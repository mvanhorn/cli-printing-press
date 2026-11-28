package ppmcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/cartridge"
	"github.com/mvanhorn/cli-printing-press/v4/internal/lake"
	"github.com/mvanhorn/cli-printing-press/v4/internal/registry"
)

const (
	ProtocolVersion      = "2025-03-26"
	ServerName           = "printing-press"
	ServerVersion        = "2.0.0"
	defaultBinaryTimeout = 30 * time.Second
	maxBinaryOutputBytes = 512 * 1024
)

// Server exposes all 616+ Printing Press apps and the local Life Lake through
// exactly 5 lean progressive-discovery MCP tools (~650 tokens total).
type Server struct {
	Registry *registry.Client
	LakePath string
	Journal  *cartridge.Journal
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ServeStdio runs the newline-delimited JSON-RPC 2.0 MCP loop on r/w.
func (s *Server) ServeStdio(ctx context.Context, r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	enc := json.NewEncoder(w)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_ = enc.Encode(rpcResponse{
				JSONRPC: "2.0",
				Error:   &rpcError{Code: -32700, Message: "parse error"},
			})
			continue
		}
		// Notifications have no ID and receive no response.
		if req.ID == nil && strings.HasPrefix(req.Method, "notifications/") {
			continue
		}
		resp := s.Handle(ctx, req)
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// Handle dispatches one JSON-RPC request and returns its response.
func (s *Server) Handle(ctx context.Context, req rpcRequest) rpcResponse {
	switch req.Method {
	case "initialize":
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"protocolVersion": ProtocolVersion,
				"capabilities": map[string]any{
					"tools": map[string]any{"listChanged": false},
				},
				"serverInfo": map[string]any{
					"name":    ServerName,
					"version": ServerVersion,
				},
				"instructions": "Printing Press 2.0 (pp) gives fast (~150ms, no browser tab) JSON access to 616+ apps and websites, including live sports scores (espn), tech news (hackernews, digg), GitHub (github-trending), weather, local browser history (chrome-history), commerce/delivery (instacart, amazon-orders, doordash), finance (monarch-money), travel (airbnb), and productivity tools (linear, notion, gmail). Before opening a browser or guessing, call pp_route or pp_discover, then pp_run (pass via_browse=true for sites that need Google Chrome logged-in cookies). Use pp_lake_search or pp_lake_sql to search synced personal data.",
			},
		}
	case "ping":
		return rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}
	case "tools/list":
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]any{"tools": toolDefinitions()},
		}
	case "tools/call":
		return s.handleToolCall(ctx, req)
	default:
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)},
		}
	}
}

func toolDefinitions() []map[string]any {
	return []map[string]any{
		{
			"name":        "pp_route",
			"description": "Check before opening a browser tab: resolves a URL or task to a 150ms local Printing Press Cartridge or prebuilt CLI (616+ apps) with estimated token/latency savings.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"target": map[string]any{"type": "string", "description": "URL (e.g. https://www.espn.com/nba/scoreboard) or app/intent name"},
				},
				"required": []string{"target"},
			},
		},
		{
			"name":        "pp_discover",
			"description": "Search installed Cartridges, embedded starter tools, and the 616-app Printing Press Library (returns local commands, parameters, and catalog matches).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":    map[string]any{"type": "string", "description": "Keyword or app name"},
					"category": map[string]any{"type": "string", "description": "Optional category filter"},
					"limit":    map[string]any{"type": "integer", "description": "Max results (default 15)"},
				},
			},
		},
		{
			"name":        "pp_run",
			"description": "Execute a command on a local host-locked Printing Press Cartridge or verified installed prebuilt library CLI (~150ms, compact JSON). Mutating commands require confirm_mutation=true.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"app":              map[string]any{"type": "string", "description": "Cartridge or installed library CLI name (e.g. espn, hackernews, digg)"},
					"command":          map[string]any{"type": "string", "description": "Command name inside the cartridge or subcommand for a prebuilt CLI"},
					"args":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional extra CLI arguments for installed prebuilt binaries"},
					"params":           map[string]any{"type": "object", "description": "Key-value parameters for the command"},
					"compact":          map[string]any{"type": "boolean", "description": "Return high-gravity compact fields only (default true)"},
					"limit":            map[string]any{"type": "integer", "description": "Optional max rows"},
					"dry_run":          map[string]any{"type": "boolean", "description": "Preview request without executing"},
					"confirm_mutation": map[string]any{"type": "boolean", "description": "Explicitly confirm a mutating command"},
					"via_browse":       map[string]any{"type": "boolean", "description": "Execute HTTP/GraphQL cartridge inside Google Chrome via OpenCode browse (uses Chrome session without exporting cookies)"},
				},
				"required": []string{"app", "command"},
			},
		},
		{
			"name":        "pp_lake_search",
			"description": "Instant 5ms FTS5 full-text search across every app synced into your private local Personal Life Lake (~/.pp/lake.db).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":  map[string]any{"type": "string", "description": "Search keywords"},
					"source": map[string]any{"type": "string", "description": "Optional source app filter"},
					"type":   map[string]any{"type": "string", "description": "Optional entity_type filter"},
					"limit":  map[string]any{"type": "integer", "description": "Max rows (default 20)"},
				},
			},
		},
		{
			"name":        "pp_lake_sql",
			"description": "Run a strictly read-only single-statement SQLite SELECT/WITH query across your local Personal Life Lake (table: lake_items; bounded to 5s, 500 rows, 512 KiB).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"sql": map[string]any{"type": "string", "description": "Read-only single SELECT/WITH SQL query"},
				},
				"required": []string{"sql"},
			},
		},
	}
}

func (s *Server) handleToolCall(ctx context.Context, req rpcRequest) rpcResponse {
	var call struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &call); err != nil {
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32602, Message: "invalid tool call params"},
		}
	}

	var (
		payload any
		err     error
	)

	switch call.Name {
	case "pp_route":
		target := stringArg(call.Arguments, "target")
		payload, err = s.Registry.Route(ctx, target)
	case "pp_discover":
		query := stringArg(call.Arguments, "query")
		catFilter := stringArg(call.Arguments, "category")
		limit := intArg(call.Arguments, "limit", 15)
		localMatches := discoverLocalCartridges(query, catFilter, limit)
		var catalogMatches []registry.Entry
		if s.Registry != nil {
			if cat, loadErr := s.Registry.Load(ctx, false); loadErr == nil {
				catalogMatches = s.Registry.Search(cat, query, catFilter, limit)
			}
		}
		payload = map[string]any{
			"local_cartridges": localMatches,
			"catalog_matches":  catalogMatches,
		}
	case "pp_run":
		app := stringArg(call.Arguments, "app")
		cmdName := stringArg(call.Arguments, "command")
		if cart, resolveErr := cartridge.Resolve(app); resolveErr == nil {
			compact := true
			if v, ok := call.Arguments["compact"].(bool); ok {
				compact = v
			}
			dryRun, _ := call.Arguments["dry_run"].(bool)
			confirmMut, _ := call.Arguments["confirm_mutation"].(bool)
			params := map[string]string{}
			if pm, ok := call.Arguments["params"].(map[string]any); ok {
				for k, v := range pm {
					params[k] = fmt.Sprint(v)
				}
			}
			payload, err = cartridge.Execute(ctx, cart, cmdName, cartridge.RunOptions{
				Params:       params,
				Compact:      compact,
				Limit:        intArg(call.Arguments, "limit", 0),
				DryRun:       dryRun,
				ConfirmWrite: confirmMut,
				ViaBrowse:    boolArg(call.Arguments, "via_browse"),
				Journal:      s.Journal,
			})
		} else if binPath, ok := registry.InstalledBinaryPath(app); ok {
			payload, err = runVerifiedBinaryMCP(ctx, app, binPath, cmdName, call.Arguments)
		} else if s.Registry != nil && strings.TrimSpace(os.Getenv("PP_NO_AUTO_INSTALL")) != "1" {
			if hydratedPath, _, hydErr := s.Registry.EnsureInstalled(ctx, app); hydErr == nil {
				payload, err = runVerifiedBinaryMCP(ctx, app, hydratedPath, cmdName, call.Arguments)
			} else {
				err = resolveErr
			}
		} else {
			err = resolveErr
		}
	case "pp_lake_search":
		lk, openErr := s.openLake()
		if openErr != nil {
			err = openErr
		} else {
			defer func() { _ = lk.Close() }()
			payload, err = lk.Search(ctx,
				stringArg(call.Arguments, "query"),
				stringArg(call.Arguments, "source"),
				stringArg(call.Arguments, "type"),
				intArg(call.Arguments, "limit", 20),
			)
		}
	case "pp_lake_sql":
		lk, openErr := s.openLake()
		if openErr != nil {
			err = openErr
		} else {
			defer func() { _ = lk.Close() }()
			payload, err = lk.QueryReadOnly(ctx, stringArg(call.Arguments, "sql"))
		}
	default:
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32602, Message: fmt.Sprintf("unknown tool %q", call.Name)},
		}
	}

	if err != nil {
		errText := err.Error()
		if ee, ok := errors.AsType[*cartridge.ExecError](err); ok && ee.Handoff != nil {
			if rawErr, mErr := json.MarshalIndent(ee, "", "  "); mErr == nil {
				errText = string(rawErr)
			}
		}
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"isError": true,
				"content": []map[string]any{{"type": "text", "text": errText}},
			},
		}
	}

	encoded, _ := json.MarshalIndent(payload, "", "  ")
	textOut := string(encoded)
	if call.Name == "pp_run" || call.Name == "pp_lake_search" {
		textOut = cartridge.WrapUntrustedContent(textOut, call.Name, false)
	}
	return rpcResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"content": []map[string]any{{"type": "text", "text": textOut}},
		},
	}
}

type localCommandInfo struct {
	Name           string   `json:"name"`
	Description    string   `json:"description,omitempty"`
	Mutates        bool     `json:"mutates"`
	RequiredParams []string `json:"required_params,omitempty"`
	OptionalParams []string `json:"optional_params,omitempty"`
}

type localCartridgeInfo struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Category    string             `json:"category,omitempty"`
	Kind        string             `json:"kind"` // "installed" or "builtin"
	Commands    []localCommandInfo `json:"commands"`
}

func discoverLocalCartridges(query, catFilter string, limit int) []localCartridgeInfo {
	if limit <= 0 {
		limit = 15
	}
	q := strings.ToLower(strings.TrimSpace(query))
	cf := strings.ToLower(strings.TrimSpace(catFilter))

	seen := make(map[string]bool)
	var all []*cartridge.Cartridge
	if installed, err := cartridge.Installed(); err == nil {
		for _, c := range installed {
			seen[c.Name] = true
			all = append(all, c)
		}
	}
	if builtins, err := cartridge.Builtins(); err == nil {
		for _, c := range builtins {
			if !seen[c.Name] {
				all = append(all, c)
			}
		}
	}

	out := make([]localCartridgeInfo, 0)
	for _, c := range all {
		if cf != "" && !strings.EqualFold(c.Category, cf) {
			continue
		}
		if q != "" && !cartridgeMatchesQuery(c, q) {
			continue
		}
		kind := "installed"
		if strings.HasPrefix(c.Source, "builtin:") {
			kind = "builtin"
		}
		cmdNames := make([]string, 0, len(c.Commands))
		for name := range c.Commands {
			cmdNames = append(cmdNames, name)
		}
		sort.Strings(cmdNames)
		cmds := make([]localCommandInfo, 0, len(cmdNames))
		for _, name := range cmdNames {
			spec := c.Commands[name]
			var reqParams, optParams []string
			for pName, pSpec := range spec.Params {
				if pSpec.Required && pSpec.Default == "" {
					reqParams = append(reqParams, pName)
				} else {
					optParams = append(optParams, pName)
				}
			}
			sort.Strings(reqParams)
			sort.Strings(optParams)
			cmds = append(cmds, localCommandInfo{
				Name:           name,
				Description:    spec.Description,
				Mutates:        c.IsMutating(name),
				RequiredParams: reqParams,
				OptionalParams: optParams,
			})
		}
		out = append(out, localCartridgeInfo{
			Name:        c.Name,
			Description: c.Description,
			Category:    c.Category,
			Kind:        kind,
			Commands:    cmds,
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}

func cartridgeMatchesQuery(c *cartridge.Cartridge, lowerQuery string) bool {
	if strings.Contains(strings.ToLower(c.Name), lowerQuery) ||
		strings.Contains(strings.ToLower(c.Description), lowerQuery) ||
		strings.Contains(strings.ToLower(c.Category), lowerQuery) {
		return true
	}
	for _, d := range c.Domains {
		if strings.Contains(strings.ToLower(d), lowerQuery) {
			return true
		}
	}
	for cmdName, cmd := range c.Commands {
		if strings.Contains(strings.ToLower(cmdName), lowerQuery) ||
			strings.Contains(strings.ToLower(cmd.Description), lowerQuery) {
			return true
		}
	}
	return false
}

func runVerifiedBinaryMCP(ctx context.Context, app, binPath, cmdName string, arguments map[string]any) (any, error) {
	if err := registry.VerifyInstalledBinary(app, binPath); err != nil {
		return nil, err
	}
	confirmMut := boolArg(arguments, "confirm_mutation")
	argv := make([]string, 0, 8)
	if trimmed := strings.TrimSpace(cmdName); trimmed != "" {
		argv = append(argv, strings.Fields(trimmed)...)
	}
	if rawArgs, ok := arguments["args"].([]any); ok {
		for _, a := range rawArgs {
			argv = append(argv, fmt.Sprint(a))
		}
	}
	if pm, ok := arguments["params"].(map[string]any); ok {
		keys := make([]string, 0, len(pm))
		for k := range pm {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			val := pm[k]
			if b, isBool := val.(bool); isBool {
				argv = append(argv, fmt.Sprintf("--%s=%t", k, b))
			} else {
				argv = append(argv, "--"+k, fmt.Sprint(val))
			}
		}
	}
	// Reject confirmation/mutation-bypass flags from ANY argument source
	// (command, args, or params) unless confirm_mutation is explicitly true.
	if !confirmMut {
		for _, tok := range argv {
			if isConfirmationFlagToken(tok) {
				return nil, fmt.Errorf("mutating confirmation flag %q requires confirm_mutation=true", tok)
			}
		}
	}
	if boolArg(arguments, "dry_run") {
		argv = append(argv, "--dry-run")
	}
	if confirmMut {
		argv = append(argv, "--yes")
	}
	if lim := intArg(arguments, "limit", 0); lim > 0 {
		argv = append(argv, "--limit", fmt.Sprint(lim))
	}

	runCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, defaultBinaryTimeout)
		defer cancel()
	}

	proc := exec.CommandContext(runCtx, binPath, argv...)
	var stdout, stderr bytes.Buffer
	outCap := &limitedBuffer{buf: &stdout, max: maxBinaryOutputBytes}
	errCap := &limitedBuffer{buf: &stderr, max: maxBinaryOutputBytes}
	proc.Stdout = outCap
	proc.Stderr = errCap
	err := proc.Run()
	outText := strings.TrimSpace(stdout.String())
	errText := strings.TrimSpace(stderr.String())
	if err != nil {
		if errText != "" {
			return nil, fmt.Errorf("%s failed: %v (%s)", app, err, errText)
		}
		return nil, fmt.Errorf("%s failed: %v", app, err)
	}
	if outCap.truncated {
		return nil, fmt.Errorf("%s output exceeded %d KiB MCP budget; narrow the request with --limit or filters", app, maxBinaryOutputBytes/1024)
	}
	var decoded any
	if json.Unmarshal([]byte(outText), &decoded) == nil {
		return map[string]any{
			"app":  app,
			"mode": "prebuilt-binary",
			"data": decoded,
		}, nil
	}
	return map[string]any{
		"app":    app,
		"mode":   "prebuilt-binary",
		"output": outText,
	}, nil
}

func isConfirmationFlagToken(tok string) bool {
	s := strings.ToLower(strings.TrimSpace(tok))
	if s == "" || !strings.HasPrefix(s, "-") {
		return false
	}
	name, _, _ := strings.Cut(s, "=")
	switch name {
	case "--yes", "-y", "--force", "-f", "--confirm":
		return true
	}
	// Combined POSIX short flags such as -yf or -fy.
	if !strings.HasPrefix(name, "--") && len(name) > 2 && strings.ContainsAny(name[1:], "yf") {
		return true
	}
	return false
}

type limitedBuffer struct {
	buf       *bytes.Buffer
	max       int
	truncated bool
}

func (l *limitedBuffer) Write(p []byte) (int, error) {
	rem := l.max - l.buf.Len()
	if rem <= 0 {
		if len(p) > 0 {
			l.truncated = true
		}
		return len(p), nil
	}
	if len(p) > rem {
		l.truncated = true
		_, _ = l.buf.Write(p[:rem])
		return len(p), nil
	}
	return l.buf.Write(p)
}

func (s *Server) openLake() (*lake.Lake, error) {
	p := s.LakePath
	if p == "" {
		var err error
		p, err = lake.DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	return lake.Open(p)
}

func stringArg(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func boolArg(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	b, _ := m[key].(bool)
	return b
}

func intArg(m map[string]any, key string, def int) int {
	if m == nil {
		return def
	}
	v, ok := m[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return def
	}
}
