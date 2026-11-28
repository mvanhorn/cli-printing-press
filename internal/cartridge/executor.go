package cartridge

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/sessionbridge"
	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

// Typed exit codes aligned with Printing Press's agent-native contract.
const (
	ExitOK          = 0
	ExitUsage       = 2
	ExitNotFound    = 3
	ExitAuth        = 4
	ExitAPI         = 5
	ExitRateLimited = 7
)

// ErrorClass is a typed categorical classification for execution failures shared
// across the cartridge runtime, CLI, and auto-heal diagnostics.
type ErrorClass string

const (
	ClassAuthError   ErrorClass = "auth_error"
	ClassRateLimited ErrorClass = "rate_limited"
	ClassDrift       ErrorClass = "drift"
	ClassBlocked     ErrorClass = "blocked"
	ClassUsage       ErrorClass = "usage"
	ClassError       ErrorClass = "error"
)

// ExecError carries a typed agent exit code, actionable hint, and optional
// human-in-the-loop handoff descriptor for login/2FA/Passkey gates.
type ExecError struct {
	Code       int          `json:"code"`
	HTTPStatus int          `json:"http_status,omitempty"`
	Class      ErrorClass   `json:"class"`
	Message    string       `json:"message"`
	Hint       string       `json:"hint,omitempty"`
	Handoff    *HandoffHint `json:"handoff,omitempty"`
}

func (e *ExecError) Error() string {
	var parts []string
	parts = append(parts, e.Message)
	if e.Hint != "" {
		parts = append(parts, fmt.Sprintf("hint: %s", e.Hint))
	}
	if e.Handoff != nil {
		parts = append(parts, fmt.Sprintf("handoff(%s): %s -> %s (then: %s)",
			e.Handoff.Action, e.Handoff.URL, e.Handoff.BrowserCmd, e.Handoff.ResumeCmd))
	}
	return strings.Join(parts, " | ")
}

// RunOptions configures a single cartridge command invocation.
type RunOptions struct {
	Params         map[string]string
	Compact        bool
	SelectFields   []string
	Limit          int
	DryRun         bool
	ConfirmWrite   bool
	StrictLock     bool
	SkipLockVerify bool
	ViaBrowse      bool
	Timeout        time.Duration
	HTTPClient     *http.Client
	Journal        *Journal
}

// RunResult is the structured execution result returned by Execute.
type RunResult struct {
	Cartridge  string `json:"cartridge"`
	Command    string `json:"command"`
	Driver     string `json:"driver"`
	DryRun     bool   `json:"dry_run,omitempty"`
	HTTPStatus int    `json:"http_status,omitempty"`
	LatencyMS  int64  `json:"latency_ms"`
	RowCount   int    `json:"row_count"`
	AuthSource string `json:"auth_source,omitempty"`
	Data       any    `json:"data"`
}

// Execute runs a named command on a validated Cartridge, enforcing host
// allowlists, mutation guards, dry-run semantics, field compaction, and journaling.
func Execute(ctx context.Context, c *Cartridge, cmdName string, opts RunOptions) (*RunResult, error) {
	if c == nil {
		return nil, &ExecError{Code: ExitUsage, Class: ClassUsage, Message: "cartridge is nil"}
	}
	if !opts.SkipLockVerify {
		if err := verifyCartridgeExecutionPin(c, opts.StrictLock); err != nil {
			return nil, &ExecError{Code: ExitAuth, Class: ClassBlocked, Message: err.Error()}
		}
	}
	cmd, ok := c.Commands[cmdName]
	if !ok {
		return nil, &ExecError{
			Code:    ExitNotFound,
			Class:   ClassUsage,
			Message: fmt.Sprintf("unknown command %q on cartridge %q", cmdName, c.Name),
			Hint:    fmt.Sprintf("run `pp info %s` to list available commands", c.Name),
		}
	}

	resolvedParams, err := resolveParams(cmd, opts.Params)
	if err != nil {
		return nil, &ExecError{Code: ExitUsage, Class: ClassUsage, Message: err.Error()}
	}

	if c.IsMutating(cmdName) && !opts.DryRun && !opts.ConfirmWrite {
		return nil, &ExecError{
			Code:    ExitUsage,
			Class:   ClassBlocked,
			Message: fmt.Sprintf("command %s.%s mutates state and requires --yes (or pass --dry-run to preview)", c.Name, cmdName),
		}
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	driver := EffectiveDriver(cmd)

	var (
		data       any
		httpStatus int
		authSource string
		execErr    error
	)

	switch driver {
	case DriverHTTP, DriverGraphQL:
		if opts.ViaBrowse {
			data, httpStatus, authSource, execErr = executeViaOpenCodeBrowse(runCtx, c, cmdName, cmd, resolvedParams, opts)
		} else {
			data, httpStatus, authSource, execErr = executeHTTP(runCtx, c, cmd, resolvedParams, opts)
		}
	case DriverLocalSQLite:
		data, execErr = executeLocalSQLite(runCtx, cmd, resolvedParams, opts)
		authSource = "local-sqlite"
	case DriverLocalFiles:
		data, execErr = executeLocalFiles(cmd, resolvedParams, opts)
		authSource = "local-files"
	case DriverOSAutomation:
		data, execErr = executeOSAutomation(runCtx, cmd, resolvedParams, opts)
		authSource = "os-automation"
	default:
		execErr = &ExecError{Code: ExitUsage, Class: ClassUsage, Message: fmt.Sprintf("unsupported driver %q", driver)}
	}

	latencyMS := time.Since(start).Milliseconds()

	if execErr != nil {
		status := string(ClassError)
		if ee, ok := errors.AsType[*ExecError](execErr); ok {
			if ee.Class != "" {
				status = string(ee.Class)
			}
			if ee.HTTPStatus != 0 {
				httpStatus = ee.HTTPStatus
			}
		}
		if opts.Journal != nil && !opts.DryRun {
			_ = opts.Journal.Record(JournalEntry{
				Cartridge:  c.Name,
				Command:    cmdName,
				Status:     status,
				HTTPStatus: httpStatus,
				LatencyMS:  latencyMS,
				ErrorText:  execErr.Error(),
			})
		}
		return nil, execErr
	}

	if !opts.DryRun {
		if cmd.Extract != "" && (driver == DriverHTTP || driver == DriverGraphQL) {
			extracted := ExtractPath(data, cmd.Extract)
			if extracted == nil && data != nil {
				driftErr := &ExecError{
					Code:       ExitAPI,
					HTTPStatus: httpStatus,
					Class:      ClassDrift,
					Message:    fmt.Sprintf("extract path %q matched nothing in response from %s.%s", cmd.Extract, c.Name, cmdName),
					Hint:       fmt.Sprintf("upstream response shape may have changed; run `pp heal %s %s`", c.Name, cmdName),
				}
				if opts.Journal != nil {
					_ = opts.Journal.Record(JournalEntry{
						Cartridge:  c.Name,
						Command:    cmdName,
						Status:     "drift",
						HTTPStatus: httpStatus,
						LatencyMS:  latencyMS,
						ErrorText:  driftErr.Error(),
					})
				}
				return nil, driftErr
			}
			data = extracted
		}

		if opts.Limit > 0 {
			if sl, ok := data.([]any); ok && len(sl) > opts.Limit {
				data = sl[:opts.Limit]
			}
		}

		fields := opts.SelectFields
		if len(fields) == 0 && opts.Compact {
			fields = cmd.CompactFields
		}
		if len(fields) > 0 {
			data = ProjectFields(data, fields)
		}
	}

	rowCount := CountRows(data)
	if opts.Journal != nil && !opts.DryRun {
		_ = opts.Journal.Record(JournalEntry{
			Cartridge:  c.Name,
			Command:    cmdName,
			Status:     "ok",
			HTTPStatus: httpStatus,
			LatencyMS:  latencyMS,
			RowCount:   rowCount,
		})
	}

	return &RunResult{
		Cartridge:  c.Name,
		Command:    cmdName,
		Driver:     driver,
		DryRun:     opts.DryRun,
		HTTPStatus: httpStatus,
		LatencyMS:  latencyMS,
		RowCount:   rowCount,
		AuthSource: authSource,
		Data:       data,
	}, nil
}

func resolveParams(cmd CommandSpec, provided map[string]string) (map[string]string, error) {
	resolved := make(map[string]string, len(cmd.Params)+len(provided))
	maps.Copy(resolved, provided)
	for name, spec := range cmd.Params {
		val := strings.TrimSpace(resolved[name])
		if val == "" && spec.Default != "" {
			val = spec.Default
		}
		resolved[name] = val
		if spec.Required && val == "" {
			return nil, fmt.Errorf("missing required parameter --param %s=<value>", name)
		}
	}
	return resolved, nil
}

func renderTemplate(tmpl string, params map[string]string, urlEscape bool) string {
	out := tmpl
	for k, v := range params {
		replacement := v
		if urlEscape {
			replacement = url.PathEscape(v)
		}
		out = strings.ReplaceAll(out, "{"+k+"}", replacement)
	}
	return out
}

func executeHTTP(ctx context.Context, c *Cartridge, cmd CommandSpec, params map[string]string, opts RunOptions) (any, int, string, error) {
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
			method = http.MethodPost
		} else {
			method = http.MethodGet
		}
	}

	var bodyBytes []byte
	contentType := ""
	if EffectiveDriver(cmd) == DriverGraphQL {
		payload := map[string]any{
			"variables": params,
		}
		if cmd.GraphQLQuery != "" {
			payload["query"] = cmd.GraphQLQuery
		}
		if cmd.PersistedQuery != "" {
			payload["operationName"] = cmd.PersistedQuery
		}
		bodyBytes, err = json.Marshal(payload)
		if err != nil {
			return nil, 0, "", err
		}
		contentType = "application/json"
	} else if strings.TrimSpace(cmd.Body) != "" {
		bodyBytes = []byte(renderTemplate(cmd.Body, params, false))
		contentType = "application/json"
	}

	if opts.DryRun {
		preview := map[string]any{
			"method":        method,
			"url":           parsedURL.String(),
			"allowed_hosts": c.Trust.AllowedHosts,
			"auth_strategy": c.Auth.Strategy,
		}
		if len(bodyBytes) > 0 {
			preview["body"] = string(bodyBytes)
		}
		return preview, 0, "dry-run", nil
	}

	var bodyReader io.Reader
	if len(bodyBytes) > 0 {
		bodyReader = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequestWithContext(ctx, method, parsedURL.String(), bodyReader)
	if err != nil {
		return nil, 0, "", err
	}
	req.Header.Set("User-Agent", "pp/2.0 (Printing Press Local Runtime)")
	req.Header.Set("Accept", "application/json, text/plain;q=0.9, */*;q=0.1")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range cmd.Headers {
		req.Header.Set(k, renderTemplate(v, params, false))
	}

	authRes, authErr := sessionbridge.Resolve(sessionbridge.AuthSpec{
		AppName:            c.Name,
		Strategy:           c.Auth.Strategy,
		EnvVar:             c.Auth.EnvVar,
		OnePasswordRef:     c.Auth.OnePasswordRef,
		Header:             c.Auth.Header,
		Prefix:             c.Auth.Prefix,
		QueryParam:         c.Auth.QueryParam,
		Domains:            c.Auth.Domains,
		TargetURL:          parsedURL.String(),
		AgentCookieAdapter: c.Auth.AgentCookieAdapter,
	})
	if authErr != nil {
		loginURL := c.Homepage
		if loginURL == "" {
			loginURL = parsedURL.Scheme + "://" + parsedURL.Hostname()
		}
		return nil, 0, "", &ExecError{
			Code:    ExitAuth,
			Class:   ClassAuthError,
			Message: authErr.Error(),
			Handoff: &HandoffHint{
				Action:     "sign_in",
				URL:        loginURL,
				BrowserCmd: fmt.Sprintf("ARDIA_BACKGROUND=1 ardia browse open %s", loginURL),
				Reason:     "active session or credential required for " + c.Name,
				ResumeCmd:  fmt.Sprintf("pp auth sync %s", c.Name),
			},
		}
	}
	authRes.Apply(req)

	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: 20 * time.Second,
			CheckRedirect: func(next *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("stopped after 5 redirects")
				}
				if !c.HostAllowed(next.URL.Hostname()) {
					return fmt.Errorf("redirect to disallowed host %q blocked by trust.allowed_hosts", next.URL.Hostname())
				}
				if len(via) > 0 && !strings.EqualFold(via[len(via)-1].URL.Hostname(), next.URL.Hostname()) {
					next.Header.Del("Cookie")
					next.Header.Del("Authorization")
				}
				return nil
			},
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, authRes.Source, &ExecError{Code: ExitAPI, Class: ClassError, Message: fmt.Sprintf("request failed: %v", err)}
	}
	defer func() { _ = resp.Body.Close() }()

	// If a cached press-auth cookie was rejected with 401/403 on a safe read-only
	// HTTP method, retry once bypassing the press-auth cache so a live Ardia /
	// browser session can recover transparently without double-mutating state.
	if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) &&
		strings.HasPrefix(authRes.Source, "press-auth:") &&
		(method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions) {
		freshAuth, freshErr := sessionbridge.Resolve(sessionbridge.AuthSpec{
			AppName:            c.Name,
			Strategy:           c.Auth.Strategy,
			EnvVar:             c.Auth.EnvVar,
			OnePasswordRef:     c.Auth.OnePasswordRef,
			Header:             c.Auth.Header,
			Prefix:             c.Auth.Prefix,
			QueryParam:         c.Auth.QueryParam,
			Domains:            c.Auth.Domains,
			TargetURL:          parsedURL.String(),
			AgentCookieAdapter: c.Auth.AgentCookieAdapter,
			SkipPressAuthCache: true,
		})
		if freshErr == nil && freshAuth != nil && freshAuth.Headers["Cookie"] != "" && freshAuth.Headers["Cookie"] != authRes.Headers["Cookie"] {
			var retryBody io.Reader
			if len(bodyBytes) > 0 {
				retryBody = bytes.NewReader(bodyBytes)
			}
			retryReq, rErr := http.NewRequestWithContext(ctx, method, parsedURL.String(), retryBody)
			if rErr == nil {
				retryReq.Header.Set("User-Agent", "pp/2.0 (Printing Press Local Runtime)")
				retryReq.Header.Set("Accept", "application/json, text/plain;q=0.9, */*;q=0.1")
				if contentType != "" {
					retryReq.Header.Set("Content-Type", contentType)
				}
				for k, v := range cmd.Headers {
					retryReq.Header.Set(k, renderTemplate(v, params, false))
				}
				freshAuth.Apply(retryReq)
				if retryResp, doErr := client.Do(retryReq); doErr == nil {
					_ = resp.Body.Close()
					resp = retryResp
					authRes = freshAuth
				}
			}
		}
	}

	const maxBodyBytes = 16 << 20
	rawResp, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, resp.StatusCode, authRes.Source, &ExecError{Code: ExitAPI, HTTPStatus: resp.StatusCode, Class: ClassError, Message: fmt.Sprintf("reading response: %v", err)}
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		loginURL := c.Homepage
		if loginURL == "" {
			loginURL = parsedURL.Scheme + "://" + parsedURL.Hostname()
		}
		return nil, resp.StatusCode, authRes.Source, &ExecError{
			Code:       ExitAuth,
			HTTPStatus: resp.StatusCode,
			Class:      ClassAuthError,
			Message:    fmt.Sprintf("HTTP %d authentication/authorization failure from %s", resp.StatusCode, parsedURL.Hostname()),
			Hint:       fmt.Sprintf("sign in via Ardia/Chrome or run `pp auth sync %s`", c.Name),
			Handoff: &HandoffHint{
				Action:     "sign_in",
				URL:        loginURL,
				BrowserCmd: fmt.Sprintf("ARDIA_BACKGROUND=1 ardia browse open %s", loginURL),
				Reason:     fmt.Sprintf("HTTP %d from %s - human sign-in, 2FA, or Passkey required", resp.StatusCode, parsedURL.Hostname()),
				ResumeCmd:  fmt.Sprintf("pp auth sync %s", c.Name),
			},
		}
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, resp.StatusCode, authRes.Source, &ExecError{
			Code:       ExitRateLimited,
			HTTPStatus: resp.StatusCode,
			Class:      ClassRateLimited,
			Message:    fmt.Sprintf("HTTP 429 rate limited by %s", parsedURL.Hostname()),
		}
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return nil, resp.StatusCode, authRes.Source, &ExecError{
			Code:       ExitNotFound,
			HTTPStatus: resp.StatusCode,
			Class:      ClassDrift,
			Message:    fmt.Sprintf("HTTP %d from %s%s", resp.StatusCode, parsedURL.Hostname(), parsedURL.Path),
			Hint:       fmt.Sprintf("endpoint may have moved; run `pp heal %s`", c.Name),
		}
	case resp.StatusCode >= 400:
		return nil, resp.StatusCode, authRes.Source, &ExecError{
			Code:       ExitAPI,
			HTTPStatus: resp.StatusCode,
			Class:      ClassDrift,
			Message:    fmt.Sprintf("HTTP %d from %s: %s", resp.StatusCode, parsedURL.Hostname(), truncate(string(rawResp), 220)),
		}
	}

	var decoded any
	if err := json.Unmarshal(rawResp, &decoded); err != nil {
		return map[string]any{"text": string(rawResp)}, resp.StatusCode, authRes.Source, nil
	}
	return decoded, resp.StatusCode, authRes.Source, nil
}

func executeLocalSQLite(ctx context.Context, cmd CommandSpec, params map[string]string, opts RunOptions) (any, error) {
	resolvedPath, err := expandLocalPath(renderTemplate(cmd.DBPath, params, false))
	if err != nil {
		return nil, &ExecError{Code: ExitNotFound, Class: ClassError, Message: err.Error()}
	}
	if opts.DryRun {
		return map[string]any{"driver": DriverLocalSQLite, "db_path": resolvedPath, "sql": cmd.SQL}, nil
	}
	if _, err := os.Stat(resolvedPath); err != nil {
		return nil, &ExecError{Code: ExitNotFound, Class: ClassError, Message: fmt.Sprintf("local database not found: %s", resolvedPath)}
	}

	// Copy DB + WAL/SHM into an isolated temp directory so we never lock a live app.
	tmpDir, err := os.MkdirTemp("", "pp-sqlite-ro-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	dst := filepath.Join(tmpDir, "snap.db")
	if err := copyRegularFile(resolvedPath, dst); err != nil {
		return nil, err
	}
	for _, s := range []string{"-wal", "-shm"} {
		_ = copyRegularFile(resolvedPath+s, dst+s)
	}

	db, err := sql.Open("sqlite", dst+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	if _, err := db.ExecContext(ctx, "PRAGMA query_only = ON"); err != nil {
		return nil, err
	}

	sqlText := strings.TrimSpace(cmd.SQL)
	if err := ValidateReadOnlySQL(sqlText); err != nil {
		return nil, &ExecError{Code: ExitUsage, Class: ClassBlocked, Message: err.Error()}
	}

	// Bind optional :param placeholders cleanly via named SQL args.
	args := make([]any, 0, len(params))
	for k, v := range params {
		args = append(args, sql.Named(k, v))
	}
	rows, err := db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, &ExecError{Code: ExitAPI, Class: ClassError, Message: fmt.Sprintf("sqlite query failed: %v", err)}
	}
	defer func() { _ = rows.Close() }()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range cols {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			if b, ok := vals[i].([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = vals[i]
			}
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if results == nil {
		results = []any{}
	}
	return results, nil
}

func executeLocalFiles(cmd CommandSpec, params map[string]string, opts RunOptions) (any, error) {
	pattern, err := expandTilde(renderTemplate(cmd.Glob, params, false))
	if err != nil {
		return nil, err
	}
	if opts.DryRun {
		return map[string]any{"driver": DriverLocalFiles, "glob": pattern, "format": cmd.Format}, nil
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, &ExecError{Code: ExitUsage, Class: ClassUsage, Message: fmt.Sprintf("invalid glob %q: %v", pattern, err)}
	}
	slices.Sort(matches)
	query := strings.ToLower(strings.TrimSpace(params["query"]))
	out := make([]any, 0, len(matches))
	for _, p := range matches {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		text := string(raw)
		if query != "" && !strings.Contains(strings.ToLower(text), query) && !strings.Contains(strings.ToLower(filepath.Base(p)), query) {
			continue
		}
		item := map[string]any{
			"path":       p,
			"name":       filepath.Base(p),
			"size_bytes": info.Size(),
			"modified":   info.ModTime().UTC().Format(time.RFC3339),
		}
		switch strings.ToLower(strings.TrimSpace(cmd.Format)) {
		case "json":
			var parsed any
			if json.Unmarshal(raw, &parsed) == nil {
				item["content"] = parsed
			} else {
				item["excerpt"] = truncate(text, 400)
			}
		case "yaml", "yml":
			var parsed any
			if yaml.Unmarshal(raw, &parsed) == nil {
				item["content"] = parsed
			} else {
				item["excerpt"] = truncate(text, 400)
			}
		default:
			item["title"] = firstHeadingOrName(text, filepath.Base(p))
			item["excerpt"] = truncate(strings.TrimSpace(text), 400)
		}
		out = append(out, item)
	}
	return out, nil
}

func executeOSAutomation(ctx context.Context, cmd CommandSpec, params map[string]string, opts RunOptions) (any, error) {
	if runtime.GOOS != "darwin" {
		return nil, &ExecError{Code: ExitUsage, Class: ClassUsage, Message: "os-automation driver requires macOS"}
	}
	// Never interpolate parameter values into script source text; pass them as
	// argv arguments (in sorted key order) and PP_PARAM_<KEY> env vars so
	// untrusted parameter values cannot break out into executable script code.
	script := cmd.OSScript
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	scriptArgs := make([]string, 0, len(keys))
	for _, k := range keys {
		scriptArgs = append(scriptArgs, params[k])
	}
	if opts.DryRun {
		return map[string]any{
			"driver": DriverOSAutomation,
			"engine": cmd.OSEngine,
			"script": script,
			"argv":   scriptArgs,
		}, nil
	}
	args := []string{"-e", script}
	if strings.EqualFold(strings.TrimSpace(cmd.OSEngine), "jxa") {
		args = []string{"-l", "JavaScript", "-e", script}
	}
	if len(scriptArgs) > 0 {
		args = append(args, "--")
		args = append(args, scriptArgs...)
	}
	c := exec.CommandContext(ctx, "osascript", args...)
	env := os.Environ()
	for _, k := range keys {
		envName := "PP_PARAM_" + strings.ToUpper(strings.ReplaceAll(k, "-", "_"))
		env = append(env, envName+"="+params[k])
	}
	c.Env = env
	out, err := c.CombinedOutput()
	if err != nil {
		return nil, &ExecError{Code: ExitAPI, Class: ClassError, Message: fmt.Sprintf("osascript failed: %v (%s)", err, truncate(string(out), 200))}
	}
	trimmed := strings.TrimSpace(string(out))
	var parsed any
	if json.Unmarshal([]byte(trimmed), &parsed) == nil {
		return parsed, nil
	}
	return map[string]any{"output": trimmed}, nil
}

func verifyCartridgeExecutionPin(c *Cartridge, strict bool) error {
	isBuiltin := strings.HasPrefix(c.Source, "builtin:")
	if isBuiltin && !strict {
		return nil
	}
	// Check whether this cartridge is installed under CartridgesDir or strict mode is requested.
	shouldVerify := strict
	if !shouldVerify && c.Source != "" && !isBuiltin {
		if dir, err := CartridgesDir(); err == nil {
			cleanDir := filepath.Clean(dir) + string(filepath.Separator)
			cleanSrc := filepath.Clean(c.Source)
			if strings.HasPrefix(cleanSrc, cleanDir) {
				shouldVerify = true
			}
		}
	}
	if !shouldVerify {
		return nil
	}
	lockPath, err := LockfilePath()
	if err != nil {
		return fmt.Errorf("resolving lockfile path: %w", err)
	}
	lf, err := LoadLockfile(lockPath)
	if err != nil {
		return fmt.Errorf("lockfile verification failed: %w", err)
	}
	return lf.VerifyCartridge(c, strict)
}

// ValidateReadOnlySQL enforces a single read-only SELECT or WITH statement after
// stripping leading whitespace, SQL comments (-- and /* */), and leading
// statement separators, and rejecting any trailing statement or PRAGMA.
func ValidateReadOnlySQL(query string) error {
	stripped := stripLeadingSQLNoise(query)
	if stripped == "" {
		return errors.New("SQL query is empty")
	}
	if hasTrailingSQLStatement(stripped) {
		return errors.New("only a single SELECT or WITH statement is allowed")
	}
	upper := strings.ToUpper(stripped)
	startsWithKeyword := func(kw string) bool {
		if !strings.HasPrefix(upper, kw) {
			return false
		}
		if len(upper) == len(kw) {
			return false
		}
		next := upper[len(kw)]
		return next == ' ' || next == '\t' || next == '\n' || next == '\r' || next == '('
	}
	if !startsWithKeyword("SELECT") && !startsWithKeyword("WITH") {
		return errors.New("only single read-only SELECT or WITH queries are allowed (PRAGMA and write statements are rejected)")
	}
	return nil
}

func stripLeadingSQLNoise(query string) string {
	for {
		query = strings.TrimLeft(query, " \t\r\n;")
		switch {
		case strings.HasPrefix(query, "--"):
			if idx := strings.IndexByte(query, '\n'); idx >= 0 {
				query = query[idx+1:]
				continue
			}
			return ""
		case strings.HasPrefix(query, "/*"):
			if idx := strings.Index(query[2:], "*/"); idx >= 0 {
				query = query[2+idx+2:]
				continue
			}
			return ""
		default:
			return query
		}
	}
}

func hasTrailingSQLStatement(query string) bool {
	inSingle := false
	inDouble := false
	inBacktick := false
	inBracket := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(query); i++ {
		ch := query[i]
		next := byte(0)
		if i+1 < len(query) {
			next = query[i+1]
		}
		switch {
		case inLineComment:
			if ch == '\n' {
				inLineComment = false
			}
		case inBlockComment:
			if ch == '*' && next == '/' {
				inBlockComment = false
				i++
			}
		case inSingle:
			if ch == '\'' {
				if next == '\'' {
					i++
					continue
				}
				inSingle = false
			}
		case inDouble:
			if ch == '"' {
				if next == '"' {
					i++
					continue
				}
				inDouble = false
			}
		case inBacktick:
			if ch == '`' {
				if next == '`' {
					i++
					continue
				}
				inBacktick = false
			}
		case inBracket:
			if ch == ']' {
				inBracket = false
			}
		default:
			switch {
			case ch == '-' && next == '-':
				inLineComment = true
				i++
			case ch == '/' && next == '*':
				inBlockComment = true
				i++
			case ch == '\'':
				inSingle = true
			case ch == '"':
				inDouble = true
			case ch == '`':
				inBacktick = true
			case ch == '[':
				inBracket = true
			case ch == ';':
				rest := stripLeadingSQLNoise(query[i+1:])
				if rest != "" {
					return true
				}
			}
		}
	}
	return false
}

func expandTilde(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if p == "~" {
			return home, nil
		}
		return filepath.Join(home, p[2:]), nil
	}
	return p, nil
}

func expandLocalPath(raw string) (string, error) {
	// Support comma-separated fallback candidate paths (e.g., Ardia -> Chrome).
	for candidate := range strings.SplitSeq(raw, "|") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		expanded, err := expandTilde(candidate)
		if err != nil {
			continue
		}
		if strings.ContainsAny(expanded, "*?[") {
			if matches, err := filepath.Glob(expanded); err == nil && len(matches) > 0 {
				return matches[0], nil
			}
			continue
		}
		if _, err := os.Stat(expanded); err == nil {
			return expanded, nil
		}
	}
	first := strings.TrimSpace(strings.Split(raw, "|")[0])
	return expandTilde(first)
}

func copyRegularFile(src, dst string) error {
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

func firstHeadingOrName(text, fallback string) string {
	for line := range strings.SplitSeq(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(trimmed, "# "); ok {
			return strings.TrimSpace(after)
		}
	}
	return fallback
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
