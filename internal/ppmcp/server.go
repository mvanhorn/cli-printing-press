package ppmcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/cartridge"
	"github.com/mvanhorn/cli-printing-press/v4/internal/lake"
	"github.com/mvanhorn/cli-printing-press/v4/internal/registry"
)

const (
	ProtocolVersion = "2025-03-26"
	ServerName      = "printing-press"
	ServerVersion   = "2.0.0"
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
			"description": "Search installed Cartridges, embedded starter tools, and the 616-app Printing Press Library.",
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
			"description": "Execute a command on a local host-locked Printing Press Cartridge (~150ms, compact JSON). Mutating commands require confirm_mutation=true.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"app":              map[string]any{"type": "string", "description": "Cartridge name (e.g. espn, hackernews, weather, chrome-history)"},
					"command":          map[string]any{"type": "string", "description": "Command name inside the cartridge"},
					"params":           map[string]any{"type": "object", "description": "Key-value parameters for the command"},
					"compact":          map[string]any{"type": "boolean", "description": "Return high-gravity compact fields only (default true)"},
					"limit":            map[string]any{"type": "integer", "description": "Optional max rows"},
					"dry_run":          map[string]any{"type": "boolean", "description": "Preview request without executing"},
					"confirm_mutation": map[string]any{"type": "boolean", "description": "Explicitly confirm a mutating command"},
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
			"description": "Run a strictly read-only SQLite SELECT query across your local Personal Life Lake (table: lake_items).",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"sql": map[string]any{"type": "string", "description": "Read-only SELECT/WITH SQL query"},
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
		cat, loadErr := s.Registry.Load(ctx, false)
		if loadErr != nil {
			err = loadErr
		} else {
			payload = map[string]any{
				"catalog_matches": s.Registry.Search(cat, query, catFilter, limit),
			}
		}
	case "pp_run":
		app := stringArg(call.Arguments, "app")
		cmdName := stringArg(call.Arguments, "command")
		cart, resolveErr := cartridge.Resolve(app)
		if resolveErr != nil {
			err = resolveErr
		} else {
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
				Journal:      s.Journal,
			})
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
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]any{
				"isError": true,
				"content": []map[string]any{{"type": "text", "text": err.Error()}},
			},
		}
	}

	encoded, _ := json.MarshalIndent(payload, "", "  ")
	return rpcResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]any{
			"content": []map[string]any{{"type": "text", "text": string(encoded)}},
		},
	}
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
