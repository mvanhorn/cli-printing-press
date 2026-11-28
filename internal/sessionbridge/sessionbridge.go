package sessionbridge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pressauth"
	_ "modernc.org/sqlite"
)

// PressAuthFreshTTL is how long an encrypted press-auth session snapshot is
// reused before falling back to a full Ardia CDP cookie lookup.
const PressAuthFreshTTL = 24 * time.Hour

// loadPressAuthState wraps pressauth.Load so unit tests can verify cache TTL
// behavior hermetically without touching the macOS Keychain.
var loadPressAuthState = pressauth.Load

// AuthSpec mirrors the minimal auth fields needed to resolve session headers
// or cookies without creating an import cycle with internal/cartridge.
type AuthSpec struct {
	AppName            string
	Strategy           string
	EnvVar             string
	OnePasswordRef     string
	Header             string
	Prefix             string
	QueryParam         string
	Domains            []string
	TargetURL          string
	AgentCookieAdapter string
	SkipPressAuthCache bool
}

// IsPressAuthFresh reports whether a cached press-auth snapshot is usable
// without hitting the live browser: it must have cookies, must not be past
// its known JWTExpiry, and must have been captured within PressAuthFreshTTL.
func IsPressAuthFresh(state *pressauth.State, now time.Time) bool {
	if state == nil || len(state.Cookies) == 0 {
		return false
	}
	if !state.JWTExpiry.IsZero() && !state.JWTExpiry.After(now) {
		return false
	}
	return now.Sub(state.CapturedAt) < PressAuthFreshTTL
}

// ResolvedAuth carries the resolved auth headers/query params and provenance.
type ResolvedAuth struct {
	Headers     map[string]string
	QueryParams map[string]string
	Source      string
}

type requestTarget struct {
	host    string
	path    string
	isHTTPS bool
}

func parseTargetURL(raw string) *requestTarget {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	u, err := url.Parse(trimmed)
	if err != nil || u.Hostname() == "" {
		return nil
	}
	p := u.Path
	if p == "" {
		p = "/"
	}
	return &requestTarget{
		host:    strings.ToLower(u.Hostname()),
		path:    p,
		isHTTPS: strings.EqualFold(u.Scheme, "https"),
	}
}

// Resolve attempts to hydrate auth for an app from (in order):
//  1. Explicit environment variable (AuthSpec.EnvVar or PP_<APP>_TOKEN)
//  2. agentcookie plaintext sidecar (~/.agentcookie/cookies-plain.db)
//  3. press-auth captured session store
//  4. Local Firefox/Zen/LibreWolf unencrypted cookies.sqlite (WAL-safe copy)
//
// When TargetURL is populated, cookie selection is strictly scoped to that
// request's host, path, and scheme so multi-host cartridges cannot leak one
// site's session cookies to another site.
func Resolve(spec AuthSpec) (*ResolvedAuth, error) {
	strategy := strings.ToLower(strings.TrimSpace(spec.Strategy))
	if strategy == "" || strategy == "none" {
		return &ResolvedAuth{Source: "none"}, nil
	}

	switch strategy {
	case "env_bearer", "env_api_key", "header", "op_secret":
		var (
			val    string
			source string
		)
		if strings.HasPrefix(strings.TrimSpace(spec.OnePasswordRef), "op://") {
			resolved, opErr := ResolveOnePassword(context.Background(), spec.OnePasswordRef)
			if opErr != nil {
				return nil, opErr
			}
			val = resolved
			source = "1password:" + strings.TrimSpace(spec.OnePasswordRef)
		} else {
			envVal, envName := lookupEnvToken(spec)
			if envVal == "" {
				return nil, fmt.Errorf("missing auth credential: set %s (or configure op_ref: op://Vault/Item/field for 1Password Touch ID)", envName)
			}
			if strings.HasPrefix(envVal, "op://") {
				resolved, opErr := ResolveOnePassword(context.Background(), envVal)
				if opErr != nil {
					return nil, opErr
				}
				val = resolved
				source = "1password:" + envVal
			} else {
				val = envVal
				source = "env:" + envName
			}
		}
		res := &ResolvedAuth{
			Headers:     make(map[string]string),
			QueryParams: make(map[string]string),
			Source:      source,
		}
		if spec.QueryParam != "" {
			res.QueryParams[spec.QueryParam] = val
			return res, nil
		}
		header := spec.Header
		if header == "" {
			header = "Authorization"
		}
		prefix := spec.Prefix
		if prefix == "" && (strategy == "env_bearer" || strategy == "op_secret") {
			prefix = "Bearer "
		}
		res.Headers[header] = prefix + val
		return res, nil

	case "browser_cookie", "agentcookie":
		if val, envName := lookupEnvToken(spec); val != "" {
			source := "env:" + envName
			if strings.HasPrefix(val, "op://") {
				opRef := val
				resolved, opErr := ResolveOnePassword(context.Background(), opRef)
				if opErr != nil {
					return nil, opErr
				}
				val = resolved
				source = "1password:" + opRef
			}
			return &ResolvedAuth{
				Headers: map[string]string{"Cookie": val},
				Source:  source,
			}, nil
		}
		target := parseTargetURL(spec.TargetURL)
		scopedDomains := filterDomainsForTarget(spec.Domains, target)
		if target != nil && len(scopedDomains) == 0 {
			// Request host is outside declared auth domains; send no site cookies.
			return &ResolvedAuth{Source: "none"}, nil
		}

		if cookieHeader, _, src, err := loadFromAgentCookie(scopedDomains, target); err == nil && cookieHeader != "" {
			return &ResolvedAuth{
				Headers: map[string]string{"Cookie": cookieHeader},
				Source:  src,
			}, nil
		}
		// Reuse fresh (<24h and unexpired JWT) press-auth cache before hitting Ardia
		// CDP so repeated calls stay ~1ms instead of dumping all browser cookies.
		var stalePressAuth *ResolvedAuth
		now := time.Now().UTC()
		if !spec.SkipPressAuthCache {
			for _, dom := range scopedDomains {
				clean := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(dom)), ".")
				if clean == "" {
					continue
				}
				if target != nil && !cookieDomainMatchesHost(dom, target.host) {
					continue
				}
				if state, err := loadPressAuthState(clean); err == nil && len(state.Cookies) > 0 {
					candidate := &ResolvedAuth{
						Headers: map[string]string{"Cookie": joinPressAuthCookies(state.Cookies)},
						Source:  "press-auth:" + clean,
					}
					if IsPressAuthFresh(state, now) {
						return candidate, nil
					}
					// Only keep as fallback if not known-expired via JWTExpiry.
					if stalePressAuth == nil && (state.JWTExpiry.IsZero() || state.JWTExpiry.After(now)) {
						stalePressAuth = candidate
					}
				}
			}
		}
		if cookieHeader, _, src, err := LoadFromArdia(context.Background(), scopedDomains, target); err == nil && cookieHeader != "" {
			return &ResolvedAuth{
				Headers: map[string]string{"Cookie": cookieHeader},
				Source:  src,
			}, nil
		}
		if stalePressAuth != nil {
			return stalePressAuth, nil
		}
		if cookieHeader, _, src, err := loadFromFirefoxFamily(scopedDomains, target); err == nil && cookieHeader != "" {
			return &ResolvedAuth{
				Headers: map[string]string{"Cookie": cookieHeader},
				Source:  src,
			}, nil
		}
		return nil, fmt.Errorf("no logged-in session found for %v (sign in via Ardia/Chrome + agentcookie, run `pp auth sync %s`, or set %s)",
			scopedDomains, spec.AppName, defaultEnvVarName(spec))

	default:
		return nil, fmt.Errorf("unsupported auth strategy %q", spec.Strategy)
	}
}

func filterDomainsForTarget(domains []string, target *requestTarget) []string {
	if target == nil {
		return domains
	}
	if len(domains) == 0 {
		return []string{target.host}
	}
	var out []string
	for _, d := range domains {
		if cookieDomainMatchesHost(d, target.host) {
			out = append(out, d)
		}
	}
	return out
}

// cookieDomainMatchesHost implements RFC 6265 domain matching:
// a cookie with a leading dot (".example.com") matches "example.com" and its
// subdomains; a host-only cookie ("api.example.com") matches only exact host.
func cookieDomainMatchesHost(cookieHost, reqHost string) bool {
	req := strings.ToLower(strings.TrimSpace(reqHost))
	raw := strings.ToLower(strings.TrimSpace(cookieHost))
	if req == "" || raw == "" {
		return false
	}
	if apex, hasDot := strings.CutPrefix(raw, "."); hasDot {
		if apex == "" {
			return false
		}
		return req == apex || strings.HasSuffix(req, "."+apex)
	}
	return req == raw
}

// cookiePathMatches implements RFC 6265 path matching.
func cookiePathMatches(cookiePath, reqPath string) bool {
	cp := strings.TrimSpace(cookiePath)
	if cp == "" || cp == "/" {
		return true
	}
	rp := strings.TrimSpace(reqPath)
	if rp == "" {
		rp = "/"
	}
	if rp == cp {
		return true
	}
	if strings.HasPrefix(rp, cp) {
		if strings.HasSuffix(cp, "/") || rp[len(cp)] == '/' {
			return true
		}
	}
	return false
}

// Apply writes resolved headers and query parameters onto an outgoing HTTP request.
func (r *ResolvedAuth) Apply(req *http.Request) {
	if r == nil || req == nil {
		return
	}
	for k, v := range r.Headers {
		if req.Header.Get(k) == "" {
			req.Header.Set(k, v)
		}
	}
	if len(r.QueryParams) > 0 {
		q := req.URL.Query()
		for k, v := range r.QueryParams {
			if !q.Has(k) {
				q.Set(k, v)
			}
		}
		req.URL.RawQuery = q.Encode()
	}
}

func lookupEnvToken(spec AuthSpec) (string, string) {
	if spec.EnvVar != "" {
		if v := strings.TrimSpace(os.Getenv(spec.EnvVar)); v != "" {
			return v, spec.EnvVar
		}
		return "", spec.EnvVar
	}
	fallback := defaultEnvVarName(spec)
	if v := strings.TrimSpace(os.Getenv(fallback)); v != "" {
		return v, fallback
	}
	return "", fallback
}

func defaultEnvVarName(spec AuthSpec) string {
	if spec.EnvVar != "" {
		return spec.EnvVar
	}
	norm := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(spec.AppName), "-", "_"))
	if norm == "" {
		return "PP_AUTH_TOKEN"
	}
	return "PP_" + norm + "_TOKEN"
}

// JoinCookies formats a cookie map into a deterministic HTTP Cookie header.
func JoinCookies(cookies map[string]string) string {
	return joinPressAuthCookies(cookies)
}

func joinPressAuthCookies(cookies map[string]string) string {
	names := make([]string, 0, len(cookies))
	for name := range cookies {
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+"="+cookies[name])
	}
	return strings.Join(parts, "; ")
}

func agentCookieSidecarPath() (string, error) {
	if custom := strings.TrimSpace(os.Getenv("AGENTCOOKIE_SIDECAR_DB")); custom != "" {
		return custom, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".agentcookie", "cookies-plain.db"), nil
}

// ResolveAgentCookieDomainCookies extracts strictly one domain's active HTTPS
// cookies from the local agentcookie sidecar database (zero Keychain prompts).
func ResolveAgentCookieDomainCookies(domain string) (string, map[string]string, error) {
	clean := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if clean == "" {
		return "", nil, errors.New("domain is required")
	}
	target := &requestTarget{host: clean, path: "/", isHTTPS: true}
	hdr, m, _, err := loadFromAgentCookie([]string{"." + clean, clean}, target)
	if err != nil {
		return "", nil, err
	}
	return hdr, m, nil
}

// ResolveFirefoxDomainCookies extracts strictly one domain's active HTTPS
// cookies from local Firefox, Zen, or LibreWolf unencrypted cookies.sqlite.
func ResolveFirefoxDomainCookies(domain string) (string, map[string]string, error) {
	clean := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if clean == "" {
		return "", nil, errors.New("domain is required")
	}
	target := &requestTarget{host: clean, path: "/", isHTTPS: true}
	hdr, m, _, err := loadFromFirefoxFamily([]string{"." + clean, clean}, target)
	if err != nil {
		return "", nil, err
	}
	return hdr, m, nil
}

func loadFromAgentCookie(domains []string, target *requestTarget) (string, map[string]string, string, error) {
	if len(domains) == 0 && target == nil {
		return "", nil, "", errors.New("no domains")
	}
	dbPath, err := agentCookieSidecarPath()
	if err != nil {
		return "", nil, "", err
	}
	if _, err := os.Stat(dbPath); err != nil {
		return "", nil, "", err
	}
	header, cookieMap, err := queryCookieSQLite(dbPath, domains, target, "cookies", "host_key", "name", "value", "path", "is_secure")
	if err != nil {
		header, cookieMap, err = queryCookieSQLite(dbPath, domains, target, "cookies", "domain", "name", "value", "path", "is_secure")
	}
	if err != nil || header == "" {
		return "", nil, "", fmt.Errorf("agentcookie lookup: %w", err)
	}
	return header, cookieMap, "agentcookie:" + dbPath, nil
}

func loadFromFirefoxFamily(domains []string, target *requestTarget) (string, map[string]string, string, error) {
	if len(domains) == 0 && target == nil {
		return "", nil, "", errors.New("no domains")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", nil, "", err
	}
	var roots []string
	switch runtime.GOOS {
	case "darwin":
		appSup := filepath.Join(home, "Library", "Application Support")
		roots = []string{
			filepath.Join(appSup, "zen", "Profiles"),
			filepath.Join(appSup, "Firefox", "Profiles"),
			filepath.Join(appSup, "LibreWolf", "Profiles"),
		}
	case "linux":
		roots = []string{
			filepath.Join(home, ".zen"),
			filepath.Join(home, ".mozilla", "firefox"),
			filepath.Join(home, ".librewolf"),
		}
	}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dbPath := filepath.Join(root, e.Name(), "cookies.sqlite")
			if _, err := os.Stat(dbPath); err != nil {
				continue
			}
			header, cookieMap, err := queryCookieSQLite(dbPath, domains, target, "moz_cookies", "host", "name", "value", "path", "isSecure")
			if err == nil && header != "" {
				return header, cookieMap, "firefox-family:" + dbPath, nil
			}
		}
	}
	return "", nil, "", errors.New("no matching firefox-family cookies")
}

// queryCookieSQLite copies the target SQLite file (plus optional WAL/SHM) into
// an isolated temp dir so reading never locks a running browser profile.
func queryCookieSQLite(srcPath string, domains []string, target *requestTarget, table, hostCol, nameCol, valueCol, pathCol, secureCol string) (string, map[string]string, error) {
	tmpDir, err := os.MkdirTemp("", "pp-cookie-snap-*")
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	dstPath := filepath.Join(tmpDir, "cookies.db")
	if err := copyFile(srcPath, dstPath); err != nil {
		return "", nil, err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = copyFile(srcPath+suffix, dstPath+suffix)
	}

	db, err := sql.Open("sqlite", dstPath+"?mode=ro")
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = db.Close() }()

	cols := tableColumns(db, table)
	hasPath := cols[strings.ToLower(pathCol)]
	hasSecure := cols[strings.ToLower(secureCol)]

	selectCols := []string{hostCol, nameCol, valueCol}
	if hasPath {
		selectCols = append(selectCols, pathCol)
	} else {
		selectCols = append(selectCols, "'/'")
	}
	if hasSecure {
		selectCols = append(selectCols, secureCol)
	} else {
		selectCols = append(selectCols, "0")
	}

	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(selectCols, ", "), table)
	rows, err := db.Query(query)
	if err != nil {
		return "", nil, err
	}
	defer func() { _ = rows.Close() }()

	type cookieMatch struct {
		value   string
		pathLen int
	}
	seen := make(map[string]cookieMatch)
	for rows.Next() {
		var host, name, value, cpath string
		var isSecure int
		if err := rows.Scan(&host, &name, &value, &cpath, &isSecure); err != nil {
			continue
		}
		if name == "" || value == "" {
			continue
		}
		if target != nil {
			if !cookieDomainMatchesHost(host, target.host) {
				continue
			}
			if !cookiePathMatches(cpath, target.path) {
				continue
			}
			if isSecure != 0 && !target.isHTTPS {
				continue
			}
		} else if !matchesAnyDomain(host, domains) {
			continue
		}
		// More specific path wins when duplicate cookie names exist.
		if prev, exists := seen[name]; !exists || len(cpath) >= prev.pathLen {
			seen[name] = cookieMatch{value: value, pathLen: len(cpath)}
		}
	}
	if len(seen) == 0 {
		return "", nil, errors.New("no cookies matched target")
	}
	names := make([]string, 0, len(seen))
	cookieMap := make(map[string]string, len(seen))
	for k, v := range seen {
		names = append(names, k)
		cookieMap[k] = v.value
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, k := range names {
		parts = append(parts, k+"="+cookieMap[k])
	}
	return strings.Join(parts, "; "), cookieMap, nil
}

func tableColumns(db *sql.DB, table string) map[string]bool {
	out := make(map[string]bool)
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return out
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt any
		var pk int
		if rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk) == nil {
			out[strings.ToLower(name)] = true
		}
	}
	return out
}

func matchesAnyDomain(host string, domains []string) bool {
	h := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(host)), ".")
	for _, d := range domains {
		target := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(d)), ".")
		if target == "" {
			continue
		}
		if h == target || strings.HasSuffix(h, "."+target) || strings.HasSuffix(target, "."+h) {
			return true
		}
	}
	return false
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
