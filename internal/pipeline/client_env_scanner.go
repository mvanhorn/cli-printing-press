package pipeline

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// systemEnvVarDenylist names env vars the manifest reconciler must never
// promote to user_config even when the generated client reads them.
// XDG_* and HOME/USERPROFILE are platform conventions a user sets globally
// (or that fall back via the standard library) — they are not credentials
// or per-CLI knobs an install prompt should collect.
var systemEnvVarDenylist = map[string]struct{}{
	"XDG_CACHE_HOME":  {},
	"XDG_CONFIG_HOME": {},
	"XDG_DATA_HOME":   {},
	"XDG_STATE_HOME":  {},
	"HOME":            {},
	"USERPROFILE":     {},
}

// scanClientEnvReads parses every Go source file under <dir>/internal/client/
// and returns the deduplicated, sorted set of env var names read via
// os.Getenv("..."), excluding well-known platform-convention vars
// (systemEnvVarDenylist).
//
// The scan is intentionally restricted to internal/client/. Hand-written
// auth-refresh code in that package is the unambiguous signal the manifest
// reconciler keys off — broader scans (cmd/, internal/cli/) would pick up
// env reads that should not become MCPB user_config entries (debug flags,
// test helpers, IDE shims).
//
// Parser errors on individual files are reported via stderr so an operator
// running publish sees which file the scan skipped. The scan continues so
// one malformed file does not block reconciliation of the remaining files.
func scanClientEnvReads(dir string) ([]string, error) {
	clientDir := filepath.Join(dir, "internal", "client")
	entries, err := os.ReadDir(clientDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading client dir: %w", err)
	}

	seen := make(map[string]struct{})
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		path := filepath.Join(clientDir, e.Name())
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping unparseable client file %s: %v\n", path, err)
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Getenv" {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "os" {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			name, err := strconv.Unquote(lit.Value)
			if err != nil || name == "" {
				return true
			}
			if _, deny := systemEnvVarDenylist[name]; deny {
				return true
			}
			seen[name] = struct{}{}
			return true
		})
	}

	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// reconcileMCPBManifestFromClient extends the just-written manifest.json in
// dir with user_config entries for any os.Getenv read in internal/client/*.go
// that the manifest does not already declare. cli is the same in-memory
// CLIManifest the writer just emitted; passing it through avoids a re-read
// of .printing-press.json (which may not be on disk yet for callers that
// build the CLI manifest in memory).
//
// Safe by construction:
//   - APIs with no internal/client/ dir produce no changes.
//   - Env vars already declared in mcp_config.env are skipped.
//   - Goldens for spec-driven APIs without hand-written client code are
//     untouched because their client.go's os.Getenv calls all resolve to
//     names already in mcp_config.env.
func reconcileMCPBManifestFromClient(dir string, cli CLIManifest) error {
	manifestPath := filepath.Join(dir, MCPBManifestFilename)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading MCPB manifest: %w", err)
	}

	var manifest MCPBManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parsing MCPB manifest: %w", err)
	}

	envReads, err := scanClientEnvReads(dir)
	if err != nil {
		return err
	}
	if len(envReads) == 0 {
		return nil
	}

	var missing []string
	for _, name := range envReads {
		if _, declared := manifest.Server.MCPConfig.Env[name]; declared {
			continue
		}
		missing = append(missing, name)
	}
	if len(missing) == 0 {
		return nil
	}

	if manifest.Server.MCPConfig.Env == nil {
		manifest.Server.MCPConfig.Env = make(map[string]string, len(missing))
	}
	if manifest.UserConfig == nil {
		manifest.UserConfig = make(map[string]MCPBVar, len(missing))
	}

	required := authRequiresCredential(cli.AuthType) && !cli.AuthOptional
	for _, name := range missing {
		key := userConfigKey(name)
		manifest.Server.MCPConfig.Env[name] = "${user_config." + key + "}"
		manifest.UserConfig[key] = MCPBVar{
			Type:        mcpbVarTypeString,
			Title:       name,
			Description: discoveredEnvDescription(cli, name, required),
			Sensitive:   true,
			Required:    required,
		}
	}

	out, err := marshalMCPBManifest(manifest)
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath, out, 0o644)
}

// discoveredEnvDescription mirrors envVarDescription's shape but flags the
// field as discovered from the client source so an install-page reader knows
// why it appeared in the user_config block alongside the spec-declared keys.
func discoveredEnvDescription(m CLIManifest, envVar string, required bool) string {
	var b strings.Builder
	if !required {
		b.WriteString("Optional. ")
	}
	b.WriteString("Sets ")
	b.WriteString(envVar)
	b.WriteString(" for the ")
	if m.DisplayName != "" {
		b.WriteString(displayNameForConcat(m.DisplayName))
	} else if m.APIName != "" {
		b.WriteString(m.APIName)
	} else {
		b.WriteString("CLI")
	}
	b.WriteString(" MCP server. Required by the generated client for credential refresh or hand-written auth flow.")
	return b.String()
}
