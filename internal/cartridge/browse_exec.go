package cartridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const defaultBrowseExecTimeout = 30 * time.Second

// jsHostAllowedHelper mirrors Cartridge.HostAllowed in JavaScript so page.route,
// post-navigation checks, and manual fetch redirect verification enforce the
// exact same host allowlist semantics (exact host, ".example.com", "*.example.com").
const jsHostAllowedHelper = `const isHostAllowed = (host, allowed) => {
  const h = String(host || "").toLowerCase().trim();
  if (!h || !Array.isArray(allowed) || allowed.length === 0) return false;
  for (const raw of allowed) {
    const a = String(raw || "").toLowerCase().trim();
    if (!a) continue;
    if (a.startsWith(".")) {
      const apex = a.slice(1);
      if (apex && (h === apex || h.endsWith(a))) return true;
      continue;
    }
    if (a.startsWith("*.")) {
      const apex = a.slice(2);
      if (apex && (h === apex || h.endsWith("." + apex))) return true;
      continue;
    }
    if (h === a) return true;
  }
  return false;
};`

// executeViaOpenCodeBrowse runs an HTTP or GraphQL Cartridge command inside
// the user's real Google Chrome session via OpenCode `browse execute` (zero
// Keychain prompts, zero Chrome 144 remote-debugging popups, zero DOM clicking).
func executeViaOpenCodeBrowse(ctx context.Context, c *Cartridge, cmdName string, cmd CommandSpec, params map[string]string, opts RunOptions) (any, int, string, error) {
	rawURL := renderTemplate(cmd.URL, params, true)
	parsedURL, err := url.Parse(rawURL)
	if err != nil || parsedURL.Hostname() == "" {
		return nil, 0, "", &ExecError{Code: ExitUsage, Class: ClassUsage, Message: fmt.Sprintf("invalid rendered url %q", rawURL)}
	}
	if !c.HostAllowed(parsedURL.Hostname()) {
		return nil, 0, "", &ExecError{
			Code:    ExitUsage,
			Class:   ClassBlocked,
			Message: fmt.Sprintf("outbound host %q blocked by trust.allowed_hosts (%v)", parsedURL.Hostname(), c.Trust.AllowedHosts),
		}
	}

	q := parsedURL.Query()
	for k, v := range cmd.Query {
		rendered := renderTemplate(v, params, false)
		if rendered != "" {
			q.Set(k, rendered)
		}
	}
	parsedURL.RawQuery = q.Encode()

	method := strings.ToUpper(strings.TrimSpace(cmd.Method))
	if method == "" {
		if EffectiveDriver(cmd) == DriverGraphQL {
			method = "POST"
		} else {
			method = "GET"
		}
	}

	headers := map[string]string{
		"Accept": "application/json, text/plain;q=0.9, */*;q=0.1",
	}
	var bodyStr string
	if EffectiveDriver(cmd) == DriverGraphQL {
		payload := map[string]any{"variables": params}
		if cmd.GraphQLQuery != "" {
			payload["query"] = cmd.GraphQLQuery
		}
		if cmd.PersistedQuery != "" {
			payload["operationName"] = cmd.PersistedQuery
		}
		encoded, mErr := json.Marshal(payload)
		if mErr != nil {
			return nil, 0, "", mErr
		}
		bodyStr = string(encoded)
		headers["Content-Type"] = "application/json"
	} else if strings.TrimSpace(cmd.Body) != "" {
		bodyStr = renderTemplate(cmd.Body, params, false)
		headers["Content-Type"] = "application/json"
	}
	for k, v := range cmd.Headers {
		headers[k] = renderTemplate(v, params, false)
	}

	origin := parsedURL.Scheme + "://" + parsedURL.Host
	if c.Homepage != "" {
		if hu, herr := url.Parse(c.Homepage); herr == nil && hu.Hostname() != "" && c.HostAllowed(hu.Hostname()) {
			origin = hu.Scheme + "://" + hu.Host
		}
	}

	authStrategy := strings.ToLower(strings.TrimSpace(c.Auth.Strategy))
	switch authStrategy {
	case "", AuthStrategyNone, AuthStrategyBrowserCookie, AuthStrategyAgentCookie:
		// Chrome supplies session cookies internally via credentials: "include"
		// without ever placing secrets into subprocess arguments.
	case AuthStrategyEnvBearer, AuthStrategyEnvAPIKey, AuthStrategyHeader, AuthStrategyOnePassword:
		return nil, 0, "", &ExecError{
			Code:    ExitUsage,
			Class:   ClassUsage,
			Message: fmt.Sprintf("auth strategy %q is not supported with --via browse (direct credentials must not be passed through browser CLI arguments)", c.Auth.Strategy),
			Hint:    fmt.Sprintf("omit --via browse so `pp run %s %s` sends configured credentials directly over TLS", c.Name, cmdName),
		}
	default:
		return nil, 0, "", &ExecError{
			Code:    ExitUsage,
			Class:   ClassUsage,
			Message: fmt.Sprintf("unsupported auth strategy %q for --via browse", c.Auth.Strategy),
		}
	}

	if opts.DryRun {
		return map[string]any{
			"transport":     "opencode-browse-fetch",
			"origin":        origin,
			"method":        method,
			"url":           parsedURL.String(),
			"allowed_hosts": c.Trust.AllowedHosts,
			"auth_strategy": c.Auth.Strategy,
		}, 0, "opencode-browse", nil
	}

	authSource := "opencode-browse"

	browseBin, err := findOpenCodeBrowseBinary()
	if err != nil {
		return nil, 0, "", &ExecError{Code: ExitAuth, Class: ClassAuthError, Message: err.Error()}
	}

	reqDesc := map[string]any{
		"origin":        origin,
		"url":           parsedURL.String(),
		"method":        method,
		"headers":       headers,
		"body":          bodyStr,
		"allowed_hosts": c.Trust.AllowedHosts,
	}
	reqJSON, err := json.Marshal(reqDesc)
	if err != nil {
		return nil, 0, "", err
	}

	// Build a safe JS body passing the JSON descriptor as a literal parsed JSON
	// string (escaped by json.Marshal, never interpolated into JS code).
	// Enforce trust.allowed_hosts on EVERY browser request and redirect hop via
	// both page.route interception and manual fetch redirect verification.
	escapedJSON, err := json.Marshal(string(reqJSON))
	if err != nil {
		return nil, 0, "", err
	}
	jsCode := fmt.Sprintf(`
const req = JSON.parse(%s);
%s
let blockedReason = "";
try {
  if (typeof page.route === "function") {
    await page.route("**/*", async (route) => {
      try {
        const u = new URL(route.request().url());
        if ((u.protocol === "http:" || u.protocol === "https:") && !isHostAllowed(u.hostname, req.allowed_hosts)) {
          if (!blockedReason) {
            blockedReason = "outbound host " + u.hostname + " blocked by trust.allowed_hosts";
          }
          await route.abort("blockedbyclient");
          return;
        }
      } catch (_) {}
      await route.continue();
    });
  }
  await page.goto(req.origin, { waitUntil: "domcontentloaded", timeout: 15000 });
  const landedHost = new URL(page.url()).hostname;
  if (!isHostAllowed(landedHost, req.allowed_hosts)) {
    return { blocked: "redirect to disallowed host " + landedHost + " blocked by trust.allowed_hosts" };
  }
  const result = await page.evaluate(async (r) => {
    %s
    let curURL = r.url;
    let curMethod = r.method || "GET";
    let curHeaders = Object.assign({}, r.headers || {});
    let curBody = r.body;
    for (let hop = 0; hop < 5; hop++) {
      const init = {
        method: curMethod,
        credentials: "include",
        redirect: "manual",
        headers: curHeaders
      };
      if (curBody && curMethod !== "GET" && curMethod !== "HEAD") {
        init.body = curBody;
      }
      const resp = await fetch(curURL, init);
      if (resp.type === "opaqueredirect") {
        return { blocked: "redirect blocked: browser hid redirect target (opaqueredirect) so trust.allowed_hosts could not be verified" };
      }
      if (resp.status >= 300 && resp.status < 400) {
        const loc = resp.headers.get("location");
        if (!loc) {
          return { blocked: "redirect blocked: HTTP " + resp.status + " missing Location header" };
        }
        const prevHost = new URL(curURL).hostname.toLowerCase();
        const nextURL = new URL(loc, curURL);
        if (!isHostAllowed(nextURL.hostname, r.allowed_hosts)) {
          return { blocked: "redirect to disallowed host " + nextURL.hostname + " blocked by trust.allowed_hosts" };
        }
        if (nextURL.hostname.toLowerCase() !== prevHost) {
          for (const hk of Object.keys(curHeaders)) {
            if (hk.toLowerCase() === "authorization" || hk.toLowerCase() === "cookie") {
              delete curHeaders[hk];
            }
          }
        }
        if (resp.status === 301 || resp.status === 302 || resp.status === 303) {
          curMethod = "GET";
          curBody = "";
        }
        curURL = nextURL.toString();
        continue;
      }
      const text = await resp.text();
      return { status: resp.status, text };
    }
    return { blocked: "stopped after 5 redirects" };
  }, req);
  if (result && result.blocked) {
    return result;
  }
  if (blockedReason && (!result || !result.status)) {
    return { blocked: blockedReason };
  }
  return result;
} finally {
  await page.close().catch(() => {});
}
`, string(escapedJSON), jsHostAllowedHelper, jsHostAllowedHelper)

	runCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, defaultBrowseExecTimeout)
		defer cancel()
	}

	tabLabel := fmt.Sprintf("pp-exec-%d", time.Now().UnixNano())
	defer func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = exec.CommandContext(cleanCtx, browseBin, "tabs", "close", "--tab", tabLabel).Run()
	}()

	proc := exec.CommandContext(runCtx, browseBin, "execute", "--tab", tabLabel, "--code", jsCode)
	var stdout, stderr bytes.Buffer
	proc.Stdout = &stdout
	proc.Stderr = &stderr
	if err := proc.Run(); err != nil {
		errDetail := truncate(strings.TrimSpace(stderr.String()), 220)
		return nil, 0, authSource, &ExecError{
			Code:    ExitAPI,
			Class:   ClassError,
			Message: fmt.Sprintf("OpenCode browse execute failed: %v (%s)", err, errDetail),
			Hint:    "ensure Chrome is paired with OpenCode browse (`browse status` / `browse install`), or run via Ardia without --via browse",
		}
	}

	status, bodyText, blockedMsg, err := parseBrowseExecuteFetchResult(stdout.Bytes())
	if err != nil {
		return nil, 0, authSource, &ExecError{
			Code:    ExitAPI,
			Class:   ClassError,
			Message: err.Error(),
		}
	}
	if blockedMsg != "" {
		return nil, 0, authSource, &ExecError{
			Code:    ExitUsage,
			Class:   ClassBlocked,
			Message: blockedMsg,
		}
	}

	switch {
	case status == 401 || status == 403:
		return nil, status, authSource, &ExecError{
			Code:       ExitAuth,
			HTTPStatus: status,
			Class:      ClassAuthError,
			Message:    fmt.Sprintf("HTTP %d from %s via Chrome browse fetch", status, parsedURL.Hostname()),
			Hint:       "sign in to " + origin + " in Google Chrome, then retry",
			Handoff: &HandoffHint{
				Action:     "sign_in",
				URL:        origin,
				BrowserCmd: fmt.Sprintf("browse navigate %s", origin),
				Reason:     fmt.Sprintf("HTTP %d from %s - sign in inside Chrome", status, parsedURL.Hostname()),
				ResumeCmd:  fmt.Sprintf("pp run %s %s --via browse", c.Name, cmdName),
			},
		}
	case status == 429:
		return nil, status, authSource, &ExecError{
			Code:       ExitRateLimited,
			HTTPStatus: status,
			Class:      ClassRateLimited,
			Message:    fmt.Sprintf("HTTP 429 rate limited by %s", parsedURL.Hostname()),
		}
	case status >= 400:
		return nil, status, authSource, &ExecError{
			Code:       ExitAPI,
			HTTPStatus: status,
			Class:      ClassDrift,
			Message:    fmt.Sprintf("HTTP %d from %s: %s", status, parsedURL.Hostname(), truncate(bodyText, 220)),
		}
	}

	var decoded any
	if err := json.Unmarshal([]byte(bodyText), &decoded); err != nil {
		return map[string]any{"text": bodyText}, status, authSource, nil
	}
	return decoded, status, authSource, nil
}

// parseBrowseExecuteFetchResult extracts {status, text} or {blocked} from
// OpenCode browse's stdout (which may prefix metadata lines like `tab: <id>`
// before the JSON return value, or wrap it under `result`/`value`).
func parseBrowseExecuteFetchResult(raw []byte) (int, string, string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return 0, "", "", errors.New("OpenCode browse execute returned empty output")
	}
	// Locate the first JSON object in stdout (skipping any `tab: ...` header line).
	start := strings.IndexByte(trimmed, '{')
	if start < 0 {
		return 0, "", "", fmt.Errorf("unexpected browse execute output (no JSON object): %s", truncate(trimmed, 200))
	}
	dec := json.NewDecoder(strings.NewReader(trimmed[start:]))
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return 0, "", "", fmt.Errorf("parsing browse execute JSON: %w", err)
	}
	if nested, ok := doc["result"].(map[string]any); ok {
		doc = nested
	} else if nested, ok := doc["value"].(map[string]any); ok {
		doc = nested
	}
	if blocked, ok := doc["blocked"].(string); ok && strings.TrimSpace(blocked) != "" {
		return 0, "", strings.TrimSpace(blocked), nil
	}
	statusFloat, ok := doc["status"].(float64)
	if !ok {
		return 0, "", "", fmt.Errorf("browse execute response missing HTTP status field")
	}
	text, _ := doc["text"].(string)
	return int(statusFloat), text, "", nil
}

func findOpenCodeBrowseBinary() (string, error) {
	if override := strings.TrimSpace(os.Getenv("PP_BROWSE_BIN")); override != "" {
		return override, nil
	}
	if p, err := exec.LookPath("browse"); err == nil {
		return p, nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, ".local", "bin", "browse")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", errors.New("OpenCode `browse` CLI not found on PATH")
}
