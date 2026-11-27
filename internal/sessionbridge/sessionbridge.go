package sessionbridge

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pressauth"
	_ "modernc.org/sqlite"
)

// AuthSpec mirrors the minimal auth fields needed to resolve session headers
// or cookies without creating an import cycle with internal/cartridge.
type AuthSpec struct {
	AppName            string
	Strategy           string
	EnvVar             string
	Header             string
	Prefix             string
	QueryParam         string
	Domains            []string
	AgentCookieAdapter string
}

// ResolvedAuth carries the resolved auth headers/query params and provenance.
type ResolvedAuth struct {
	Headers     map[string]string
	QueryParams map[string]string
	Source      string
}

// Resolve attempts to hydrate auth for an app from (in order):
//  1. Explicit environment variable (AuthSpec.EnvVar or PP_<APP>_TOKEN)
//  2. agentcookie plaintext sidecar (~/.agentcookie/cookies-plain.db)
//  3. press-auth captured session store
//  4. Local Firefox/Zen/LibreWolf unencrypted cookies.sqlite (WAL-safe copy)
func Resolve(spec AuthSpec) (*ResolvedAuth, error) {
	strategy := strings.ToLower(strings.TrimSpace(spec.Strategy))
	if strategy == "" || strategy == "none" {
		return &ResolvedAuth{Source: "none"}, nil
	}

	switch strategy {
	case "env_bearer", "env_api_key", "header":
		val, envName := lookupEnvToken(spec)
		if val == "" {
			return nil, fmt.Errorf("missing auth credential: set %s", envName)
		}
		res := &ResolvedAuth{
			Headers:     make(map[string]string),
			QueryParams: make(map[string]string),
			Source:      "env:" + envName,
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
		if prefix == "" && strategy == "env_bearer" {
			prefix = "Bearer "
		}
		res.Headers[header] = prefix + val
		return res, nil

	case "browser_cookie", "agentcookie":
		if val, envName := lookupEnvToken(spec); val != "" {
			return &ResolvedAuth{
				Headers: map[string]string{"Cookie": val},
				Source:  "env:" + envName,
			}, nil
		}
		if cookieHeader, src, err := loadFromAgentCookie(spec.Domains); err == nil && cookieHeader != "" {
			return &ResolvedAuth{
				Headers: map[string]string{"Cookie": cookieHeader},
				Source:  src,
			}, nil
		}
		for _, dom := range spec.Domains {
			clean := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(dom)), ".")
			if clean == "" {
				continue
			}
			if state, err := pressauth.Load(clean); err == nil && len(state.Cookies) > 0 {
				return &ResolvedAuth{
					Headers: map[string]string{"Cookie": joinPressAuthCookies(state.Cookies)},
					Source:  "press-auth:" + clean,
				}, nil
			}
		}
		if cookieHeader, src, err := loadFromFirefoxFamily(spec.Domains); err == nil && cookieHeader != "" {
			return &ResolvedAuth{
				Headers: map[string]string{"Cookie": cookieHeader},
				Source:  src,
			}, nil
		}
		return nil, fmt.Errorf("no logged-in session found for %v (sign in via Ardia/Chrome + agentcookie, run `press-auth login`, or set %s)",
			spec.Domains, defaultEnvVarName(spec))

	default:
		return nil, fmt.Errorf("unsupported auth strategy %q", spec.Strategy)
	}
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

func loadFromAgentCookie(domains []string) (string, string, error) {
	if len(domains) == 0 {
		return "", "", errors.New("no domains")
	}
	dbPath, err := agentCookieSidecarPath()
	if err != nil {
		return "", "", err
	}
	if _, err := os.Stat(dbPath); err != nil {
		return "", "", err
	}
	header, err := queryCookieSQLite(dbPath, domains, "cookies", "host_key", "name", "value")
	if err != nil {
		header, err = queryCookieSQLite(dbPath, domains, "cookies", "domain", "name", "value")
	}
	if err != nil || header == "" {
		return "", "", fmt.Errorf("agentcookie lookup: %w", err)
	}
	return header, "agentcookie:" + dbPath, nil
}

func loadFromFirefoxFamily(domains []string) (string, string, error) {
	if len(domains) == 0 {
		return "", "", errors.New("no domains")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
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
			header, err := queryCookieSQLite(dbPath, domains, "moz_cookies", "host", "name", "value")
			if err == nil && header != "" {
				return header, "firefox-family:" + dbPath, nil
			}
		}
	}
	return "", "", errors.New("no matching firefox-family cookies")
}

// queryCookieSQLite copies the target SQLite file (plus optional WAL/SHM) into
// an isolated temp dir so reading never locks a running browser profile.
func queryCookieSQLite(srcPath string, domains []string, table, hostCol, nameCol, valueCol string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "pp-cookie-snap-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	dstPath := filepath.Join(tmpDir, "cookies.db")
	if err := copyFile(srcPath, dstPath); err != nil {
		return "", err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = copyFile(srcPath+suffix, dstPath+suffix)
	}

	db, err := sql.Open("sqlite", dstPath+"?mode=ro")
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()

	query := fmt.Sprintf("SELECT %s, %s, %s FROM %s", hostCol, nameCol, valueCol, table)
	rows, err := db.Query(query)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()

	seen := make(map[string]string)
	for rows.Next() {
		var host, name, value string
		if err := rows.Scan(&host, &name, &value); err != nil {
			continue
		}
		if name == "" || value == "" {
			continue
		}
		if matchesAnyDomain(host, domains) {
			seen[name] = value
		}
	}
	if len(seen) == 0 {
		return "", errors.New("no cookies matched domains")
	}
	names := make([]string, 0, len(seen))
	for k := range seen {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, k := range names {
		parts = append(parts, k+"="+seen[k])
	}
	return strings.Join(parts, "; "), nil
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
