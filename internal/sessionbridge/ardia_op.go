package sessionbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultArdiaCookieTimeout = 30 * time.Second
	defaultOnePasswordTimeout = 20 * time.Second
)

type cdpCookie struct {
	Name    string  `json:"name"`
	Value   string  `json:"value"`
	Domain  string  `json:"domain"`
	Path    string  `json:"path"`
	Expires float64 `json:"expires"`
	Secure  bool    `json:"secure"`
}

// ResolveOnePassword hydrates an `op://Vault/Item/field` reference via the
// local 1Password CLI (`op read`) so Touch ID / biometrics unlock secrets per
// call with zero plaintext tokens stored on disk.
func ResolveOnePassword(ctx context.Context, ref string) (string, error) {
	trimmed := strings.TrimSpace(ref)
	if !strings.HasPrefix(trimmed, "op://") {
		return "", fmt.Errorf("invalid 1Password reference %q (must start with op://)", ref)
	}
	opBin := strings.TrimSpace(os.Getenv("PP_OP_BIN"))
	if opBin == "" {
		var err error
		opBin, err = exec.LookPath("op")
		if err != nil {
			return "", errors.New("1Password CLI (`op`) not found on PATH; install `op` and enable Touch ID integration")
		}
	}
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	if _, hasDeadline := runCtx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, defaultOnePasswordTimeout)
		defer cancel()
	}
	cmd := exec.CommandContext(runCtx, opBin, "read", "--no-newline", trimmed)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg != "" {
			return "", fmt.Errorf("1Password `op read` failed: %v (%s)", err, errMsg)
		}
		return "", fmt.Errorf("1Password `op read` failed: %w", err)
	}
	val := strings.TrimSpace(stdout.String())
	if val == "" {
		return "", fmt.Errorf("1Password reference %q resolved to an empty value", trimmed)
	}
	return val, nil
}

// LoadFromArdia queries the running Ardia Browser via its local control socket
// (`ARDIA_BACKGROUND=1 ardia browse cdp Storage.getCookies --target browser --json`)
// with zero focus steal, zero Chrome 144 popups, and strict RFC 6265 domain/path/
// scheme/expiry filtering so cookies never cross site boundaries.
func LoadFromArdia(ctx context.Context, domains []string, target *requestTarget) (string, map[string]string, string, error) {
	if strings.TrimSpace(os.Getenv("PP_NO_ARDIA_COOKIES")) == "1" {
		return "", nil, "", errors.New("ardia cookie handoff disabled via PP_NO_ARDIA_COOKIES=1")
	}
	if len(domains) == 0 && target == nil {
		return "", nil, "", errors.New("no domains or target URL supplied")
	}
	ardiaBin, err := findArdiaBinary()
	if err != nil {
		return "", nil, "", err
	}
	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	if _, hasDeadline := runCtx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, defaultArdiaCookieTimeout)
		defer cancel()
	}
	cmd := exec.CommandContext(runCtx, ardiaBin, "browse", "cdp", "Storage.getCookies", "--target", "browser", "--json")
	cmd.Env = append(os.Environ(), "ARDIA_BACKGROUND=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", nil, "", fmt.Errorf("ardia cookie lookup failed: %w", err)
	}

	cookies, err := parseArdiaCookiesJSON(stdout.Bytes())
	if err != nil {
		return "", nil, "", err
	}

	nowSec := float64(time.Now().Unix())
	type match struct {
		value   string
		pathLen int
	}
	seen := make(map[string]match)
	for _, ck := range cookies {
		if ck.Name == "" || ck.Value == "" {
			continue
		}
		if ck.Expires > 0 && ck.Expires <= nowSec {
			continue
		}
		if target != nil {
			if !cookieDomainMatchesHost(ck.Domain, target.host) {
				continue
			}
			if !cookiePathMatches(ck.Path, target.path) {
				continue
			}
			if ck.Secure && !target.isHTTPS {
				continue
			}
		} else if !matchesAnyDomain(ck.Domain, domains) {
			continue
		}
		if prev, exists := seen[ck.Name]; !exists || len(ck.Path) >= prev.pathLen {
			seen[ck.Name] = match{value: ck.Value, pathLen: len(ck.Path)}
		}
	}
	if len(seen) == 0 {
		return "", nil, "", errors.New("no matching ardia cookies found for target")
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
	return strings.Join(parts, "; "), cookieMap, "ardia-cdp", nil
}

// ResolveArdiaDomainCookies is a convenience helper used by `pp auth sync <app>`
// to extract strictly one domain's active HTTPS cookies from Ardia Browser.
func ResolveArdiaDomainCookies(ctx context.Context, domain string) (string, map[string]string, error) {
	clean := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(domain)), ".")
	if clean == "" {
		return "", nil, errors.New("domain is required")
	}
	target := &requestTarget{
		host:    clean,
		path:    "/",
		isHTTPS: true,
	}
	hdr, m, _, err := LoadFromArdia(ctx, []string{"." + clean, clean}, target)
	if err != nil {
		return "", nil, err
	}
	return hdr, m, nil
}

func findArdiaBinary() (string, error) {
	if override := strings.TrimSpace(os.Getenv("PP_ARDIA_BIN")); override != "" {
		return override, nil
	}
	if p, err := exec.LookPath("ardia"); err == nil {
		return p, nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, ".local", "bin", "ardia")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("ardia CLI not found")
}

// parseArdiaCookiesJSON decodes the first JSON object emitted by
// `ardia browse cdp Storage.getCookies --json` across wrapper shapes:
// `{"success":true,"data":{"cookies":[...]}}`,
// `{"success":true,"data":{"result":{"cookies":[...]}}}`,
// `{"result":{"cookies":[...]}}`, or `{"cookies":[...]}`.
func parseArdiaCookiesJSON(raw []byte) ([]cdpCookie, error) {
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimSpace(raw)))
	var env struct {
		Success *bool       `json:"success"`
		Error   any         `json:"error"`
		Cookies []cdpCookie `json:"cookies"`
		Result  *struct {
			Cookies []cdpCookie `json:"cookies"`
		} `json:"result"`
		Data *struct {
			Cookies []cdpCookie `json:"cookies"`
			Result  *struct {
				Cookies []cdpCookie `json:"cookies"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := dec.Decode(&env); err != nil {
		return nil, fmt.Errorf("decoding ardia cookie payload: %w", err)
	}
	if env.Success != nil && !*env.Success {
		return nil, fmt.Errorf("ardia cookie lookup error: %v", env.Error)
	}
	switch {
	case env.Data != nil && len(env.Data.Cookies) > 0:
		return env.Data.Cookies, nil
	case env.Data != nil && env.Data.Result != nil && len(env.Data.Result.Cookies) > 0:
		return env.Data.Result.Cookies, nil
	case env.Result != nil && len(env.Result.Cookies) > 0:
		return env.Result.Cookies, nil
	case len(env.Cookies) > 0:
		return env.Cookies, nil
	default:
		return nil, nil
	}
}
