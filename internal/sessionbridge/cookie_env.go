package sessionbridge

import (
	"context"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pressauth"
)

// CookieSyncFastTimeout caps opportunistic Ardia cookie extraction during
// `pp run` / MCP `pp_run` so an unresponsive browser daemon can never hang
// CLI startup or tool execution.
const CookieSyncFastTimeout = 5 * time.Second

// IsMetaOrHelpArgs reports whether child CLI arguments represent a non-API
// meta/help/auth invocation that must never trigger browser cookie extraction.
func IsMetaOrHelpArgs(args []string) bool {
	if len(args) == 0 {
		return true
	}
	for _, a := range args {
		switch strings.ToLower(strings.TrimSpace(a)) {
		case "-h", "--help", "--version", "-v":
			return true
		}
	}
	switch strings.ToLower(strings.TrimSpace(args[0])) {
	case "help", "version", "auth", "doctor", "completion", "agent-context":
		return true
	}
	return false
}

// MergePressAuthCookies updates stored cookies while preserving any existing
// press-auth refresh configuration (RefreshEndpoint, JWTCarrierCookie) and
// recomputing JWTExpiry from the newly synced carrier cookie when present.
func MergePressAuthCookies(existing *pressauth.State, domain string, cookies map[string]string) *pressauth.State {
	next := &pressauth.State{
		Domain:     domain,
		CapturedAt: time.Now().UTC(),
		Cookies:    cookies,
	}
	if existing != nil {
		next.RefreshEndpoint = existing.RefreshEndpoint
		next.JWTCarrierCookie = existing.JWTCarrierCookie
		if existing.JWTCarrierCookie != "" {
			if carrierVal, ok := cookies[existing.JWTCarrierCookie]; ok {
				if token, err := pressauth.ExtractJWT(carrierVal); err == nil {
					if claims, err := pressauth.DecodeJWT(token); err == nil {
						if exp, err := pressauth.Exp(claims); err == nil {
							next.JWTExpiry = exp
						}
					}
				}
			}
		}
	}
	return next
}

// CookieEnvOptions configures domain-scoped cookie env var resolution for
// prebuilt library binaries.
type CookieEnvOptions struct {
	Homepage    string
	AuthType    string
	EnvVarNames []string
	ChildArgs   []string
	Timeout     time.Duration
	LoadState   func(domain string) (*pressauth.State, error)
	SaveState   func(state *pressauth.State) error
	ResolveLive func(ctx context.Context, domain string) (string, map[string]string, error)
}

// ResolveCookieEnvVars returns extra `KEY=VALUE` env pairs to inject for a
// cookie-auth library binary. It skips meta/help invocations and non-cookie
// apps completely, prefers fresh (<24h) press-auth snapshots in ~1ms, and caps
// opportunistic Ardia CDP lookups at 5 seconds.
func ResolveCookieEnvVars(ctx context.Context, opts CookieEnvOptions) []string {
	if IsMetaOrHelpArgs(opts.ChildArgs) {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(opts.AuthType)) {
	case "cookie", "browser_cookie", "session":
	default:
		return nil
	}
	var missingEnvNames []string
	for _, envName := range opts.EnvVarNames {
		if strings.Contains(strings.ToUpper(envName), "COOKIE") && os.Getenv(envName) == "" {
			missingEnvNames = append(missingEnvNames, envName)
		}
	}
	if len(missingEnvNames) == 0 {
		return nil
	}
	u, err := url.Parse(strings.TrimSpace(opts.Homepage))
	if err != nil || u.Hostname() == "" {
		return nil
	}
	dom := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")

	loadFn := opts.LoadState
	if loadFn == nil {
		loadFn = pressauth.Load
	}
	saveFn := opts.SaveState
	if saveFn == nil {
		saveFn = pressauth.Save
	}
	resolveFn := opts.ResolveLive
	if resolveFn == nil {
		resolveFn = ResolveArdiaDomainCookies
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = CookieSyncFastTimeout
	}

	existingState, existingErr := loadFn(dom)
	freshCached := existingErr == nil && IsPressAuthFresh(existingState, time.Now().UTC())
	var cookieHeader string
	switch {
	case freshCached:
		cookieHeader = JoinCookies(existingState.Cookies)
	case strings.TrimSpace(os.Getenv("PP_NO_ARDIA_COOKIES")) != "1":
		syncCtx, cancel := context.WithTimeout(ctx, timeout)
		hdr, cookieMap, aerr := resolveFn(syncCtx, dom)
		cancel()
		if aerr == nil && len(cookieMap) > 0 {
			_ = saveFn(MergePressAuthCookies(existingState, dom, cookieMap))
			cookieHeader = hdr
		} else if existingState != nil && len(existingState.Cookies) > 0 {
			cookieHeader = JoinCookies(existingState.Cookies)
		}
	case existingState != nil && len(existingState.Cookies) > 0:
		cookieHeader = JoinCookies(existingState.Cookies)
	}
	if cookieHeader == "" {
		return nil
	}
	extra := make([]string, 0, len(missingEnvNames))
	for _, envName := range missingEnvNames {
		extra = append(extra, envName+"="+cookieHeader)
	}
	return extra
}
