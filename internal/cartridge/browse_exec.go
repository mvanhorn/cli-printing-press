package cartridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const defaultBrowseExecTimeout = 30 * time.Second

// jsHostAllowedHelper mirrors Cartridge.HostAllowed in JavaScript so page.route,
// post-navigation checks, and manual fetch redirect verification enforce the
// exact same host allowlist semantics (exact host, ".example.com", "*.example.com").
// Uses pure string/regex URL parsing because OpenCode browse's relay sandbox
// does not expose the WHATWG URL constructor globally.
const jsHostAllowedHelper = `const parseHTTPURLParts = (raw) => {
  const m = String(raw || "").trim().match(/^([a-zA-Z][a-zA-Z0-9+.-]*):\/\/(?:[^\/@]+@)?(\[[^\]]+\]|[^\/:?#]+)/);
  if (!m) return { protocol: "", hostname: "" };
  return { protocol: m[1].toLowerCase() + ":", hostname: m[2].toLowerCase() };
};
const isHostAllowed = (host, allowed) => {
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
	homepageAllowed := false
	if c.Homepage != "" {
		if hu, herr := url.Parse(c.Homepage); herr == nil && hu.Hostname() != "" && c.HostAllowed(hu.Hostname()) {
			origin = hu.Scheme + "://" + hu.Host
			homepageAllowed = true
		}
	}
	navURL := origin
	if !homepageAllowed && method == "GET" {
		// API-only hosts (e.g. site.api.espn.com) often block bare "/" with a WAF
		// 403 while allowing the actual GET endpoint URL.
		navURL = parsedURL.String()
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

	tmpDir, err := createPrivateBrowseTempDir()
	if err != nil {
		return nil, 0, authSource, err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()
	bodyFilePath := filepath.Join(tmpDir, "body.txt")

	bufKey := fmt.Sprintf("__pp_fetch_%d", time.Now().UnixNano())
	reqDesc := map[string]any{
		"origin":        origin,
		"nav_url":       navURL,
		"url":           parsedURL.String(),
		"method":        method,
		"headers":       headers,
		"body":          bodyStr,
		"allowed_hosts": c.Trust.AllowedHosts,
		"body_file":     bodyFilePath,
		"buf_key":       bufKey,
	}
	reqJSON, err := json.Marshal(reqDesc)
	if err != nil {
		return nil, 0, "", err
	}

	// Build a safe JS body passing the JSON descriptor as a literal parsed JSON
	// string (escaped by json.Marshal, never interpolated into JS code).
	// Enforce trust.allowed_hosts via tab allowedOrigins (Chrome extension level),
	// post-navigation host verification, and manual fetch redirect verification.
	// Chunk responses >48KB across page.evaluate and spill to Go's private 0700
	// body_file so browse's 100KB bridge and stdout caps never truncate JSON.
	escapedJSON, err := json.Marshal(string(reqJSON))
	if err != nil {
		return nil, 0, "", err
	}
	jsCode := fmt.Sprintf(`const req = JSON.parse(%s);
%s
await page.goto(req.nav_url || req.origin, { waitUntil: "domcontentloaded", timeout: 15000 });
const landed = parseHTTPURLParts(page.url());
if (!isHostAllowed(landed.hostname, req.allowed_hosts)) {
  return { blocked: "redirect to disallowed host " + landed.hostname + " blocked by trust.allowed_hosts" };
}
const CHUNK = 48000;
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
    if (text.length > 48000 && r.buf_key) {
      globalThis[r.buf_key] = text;
      return { status: resp.status, total: text.length, chunk: text.slice(0, 48000) };
    }
    return { status: resp.status, text };
  }
  return { blocked: "stopped after 5 redirects" };
}, req);
if (result && !result.blocked && typeof result.total === "number" && result.total > 48000) {
  let fullText = result.chunk || "";
  try {
    for (let off = fullText.length; off < result.total; off += CHUNK) {
      const part = await page.evaluate((a) => {
        const s = globalThis[a.k] || "";
        return s.slice(a.o, a.o + a.n);
      }, { k: req.buf_key, o: off, n: CHUNK });
      fullText += part || "";
    }
  } finally {
    await page.evaluate((k) => { delete globalThis[k]; }, req.buf_key).catch(() => {});
  }
  await files.write(req.body_file, fullText);
  return { status: result.status, body_file: true };
}
return result;`, string(escapedJSON), jsHostAllowedHelper, jsHostAllowedHelper)

	runCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, defaultBrowseExecTimeout)
		defer cancel()
	}

	tabLabel := fmt.Sprintf("pp-exec-%d", time.Now().UnixNano())
	newTabArgs, err := json.Marshal(map[string]any{
		"action":         "new",
		"id":             tabLabel,
		"allowedOrigins": browseAllowedOrigins(c.Trust.AllowedHosts),
	})
	if err != nil {
		return nil, 0, authSource, err
	}
	openProc := exec.CommandContext(runCtx, browseBin, "tabs", "--json", string(newTabArgs))
	if openOut, err := openProc.CombinedOutput(); err != nil {
		errDetail := truncate(strings.TrimSpace(string(openOut)), 220)
		return nil, 0, authSource, &ExecError{
			Code:    ExitAPI,
			Class:   ClassError,
			Message: fmt.Sprintf("OpenCode browse failed to open background tab: %v (%s)", err, errDetail),
			Hint:    "ensure Google Chrome is running and paired with OpenCode browse (`browse status` / `browse install`), or run via Ardia without --via browse",
		}
	}
	defer func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		closeArgs, _ := json.Marshal(map[string]any{"action": "close", "tab": tabLabel})
		_ = exec.CommandContext(cleanCtx, browseBin, "tabs", "--json", string(closeArgs)).Run()
	}()

	execArgs, err := json.Marshal(map[string]any{
		"tab":  tabLabel,
		"code": jsCode,
	})
	if err != nil {
		return nil, 0, authSource, err
	}
	proc := exec.CommandContext(runCtx, browseBin, "execute", "--json", string(execArgs))
	var stdout, stderr bytes.Buffer
	proc.Stdout = &stdout
	proc.Stderr = &stderr
	if err := proc.Run(); err != nil {
		errDetail := strings.TrimSpace(stderr.String())
		if errDetail == "" {
			errDetail = strings.TrimSpace(stdout.String())
		}
		return nil, 0, authSource, &ExecError{
			Code:    ExitAPI,
			Class:   ClassError,
			Message: fmt.Sprintf("OpenCode browse execute failed: %v (%s)", err, truncate(errDetail, 260)),
			Hint:    "ensure Google Chrome is running and paired with OpenCode browse (`browse status` / `browse install`), or run via Ardia without --via browse",
		}
	}

	status, bodyText, usedBodyFile, blockedMsg, err := parseBrowseExecuteFetchResult(stdout.Bytes())
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
	if usedBodyFile {
		rawFile, rErr := readBoundedRegularFile(bodyFilePath, 16<<20)
		if rErr != nil {
			return nil, 0, authSource, &ExecError{
				Code:    ExitAPI,
				Class:   ClassError,
				Message: fmt.Sprintf("reading spilled browse response body: %v", rErr),
			}
		}
		bodyText = string(rawFile)
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

// parseBrowseExecuteFetchResult extracts {status, text, body_file} or {blocked}
// from OpenCode browse's stdout (which may prefix metadata lines like `tab: <id>`
// before the JSON return value, or wrap it under `result`/`value`).
func parseBrowseExecuteFetchResult(raw []byte) (int, string, bool, string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return 0, "", false, "", errors.New("OpenCode browse execute returned empty output")
	}
	// Locate the first JSON object in stdout (skipping any `tab: ...` header line).
	start := strings.IndexByte(trimmed, '{')
	if start < 0 {
		return 0, "", false, "", fmt.Errorf("unexpected browse execute output (no JSON object): %s", truncate(trimmed, 200))
	}
	dec := json.NewDecoder(strings.NewReader(trimmed[start:]))
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return 0, "", false, "", fmt.Errorf("parsing browse execute JSON: %w", err)
	}
	if nested, ok := doc["result"].(map[string]any); ok {
		doc = nested
	} else if nested, ok := doc["value"].(map[string]any); ok {
		doc = nested
	}
	if blocked, ok := doc["blocked"].(string); ok && strings.TrimSpace(blocked) != "" {
		return 0, "", false, strings.TrimSpace(blocked), nil
	}
	statusFloat, ok := doc["status"].(float64)
	if !ok {
		return 0, "", false, "", fmt.Errorf("browse execute response missing HTTP status field")
	}
	usedBodyFile := false
	switch v := doc["body_file"].(type) {
	case bool:
		usedBodyFile = v
	case string:
		usedBodyFile = strings.TrimSpace(v) != ""
	}
	text, _ := doc["text"].(string)
	return int(statusFloat), text, usedBodyFile, "", nil
}

// createPrivateBrowseTempDir creates a 0700 temp directory inside /tmp (on
// Unix, resolved via EvalSymlinks to /private/tmp on macOS) so OpenCode browse's
// `files.write` accepts it as a private subdirectory within its allowed roots.
func createPrivateBrowseTempDir() (string, error) {
	base := ""
	if runtime.GOOS != "windows" {
		if info, err := os.Stat("/tmp"); err == nil && info.IsDir() {
			base = "/tmp"
		}
	}
	dir, err := os.MkdirTemp(base, "pp-browse-*")
	if err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved, nil
	}
	return dir, nil
}

func readBoundedRegularFile(path string, maxBytes int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("expected regular file at %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, maxBytes))
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

// browseAllowedOrigins converts Cartridge trust.allowed_hosts patterns into the
// host/wildcard list expected by OpenCode `browse tabs --allowedOrigins`.
func browseAllowedOrigins(allowed []string) []string {
	seen := make(map[string]bool, len(allowed)*2)
	out := make([]string, 0, len(allowed)*2)
	add := func(v string) {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	for _, raw := range allowed {
		a := strings.ToLower(strings.TrimSpace(raw))
		if a == "" {
			continue
		}
		if apex, ok := strings.CutPrefix(a, "."); ok && apex != "" {
			add(apex)
			add("*." + apex)
			continue
		}
		if apex, ok := strings.CutPrefix(a, "*."); ok && apex != "" {
			add(apex)
			add("*." + apex)
			continue
		}
		add(a)
	}
	return out
}
