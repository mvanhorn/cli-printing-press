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
// promote to user_config even when the generated client or config package
// reads them. XDG_* and HOME/USERPROFILE are platform conventions a user
// sets globally (or that fall back via the standard library) — they are
// not credentials or per-CLI knobs an install prompt should collect.
// PRINTING_PRESS_VERIFY / LIVE_HTTP / DOGFOOD are harness flags the
// verifier injects; they are not operator-facing MCPB inputs.
var systemEnvVarDenylist = map[string]struct{}{
	"XDG_CACHE_HOME":                  {},
	"XDG_CONFIG_HOME":                 {},
	"XDG_DATA_HOME":                   {},
	"XDG_STATE_HOME":                  {},
	"HOME":                            {},
	"USERPROFILE":                     {},
	"PRINTING_PRESS_VERIFY":           {},
	"PRINTING_PRESS_VERIFY_LIVE_HTTP": {},
	"PRINTING_PRESS_DOGFOOD":          {},
}

const configFileEnvSuffix = "_CONFIG"

// nonCredentialEnvSuffixes are per-instance, non-secret inputs that
// internal/config (and occasionally internal/client) reads via os.Getenv.
// Absence does not affect authentication, so discovered user_config
// entries stay optional and non-sensitive.
var nonCredentialEnvSuffixes = []string{
	"_USER_AGENT",
	"_BASE_URL",
	"_BASE_PATH",
	"_AUTHORIZATION_URL",
	"_DEVICE_AUTHORIZATION_URL",
	"_TOKEN_URL",
	"_AUTH_ROLE",
	"_SKIP_TLS_VERIFY",
	"_OAUTH_SCOPE",
	"_TENANT_ID",
	"_SUBDOMAIN",
	"_REGION",
}

// Only the generated HTTP client and config loader: those packages own
// per-instance request settings. Broader trees (cmd/, internal/cli/) mix
// debug flags, test helpers, and IDE shims into the same Getenv pattern.
var operatorEnvScanPackages = []string{
	filepath.Join("internal", "client"),
	filepath.Join("internal", "config"),
}

func scanClientEnvReads(dir string) ([]string, error) {
	seen := make(map[string]struct{})
	for _, rel := range operatorEnvScanPackages {
		if err := scanPackageEnvReads(filepath.Join(dir, rel), seen); err != nil {
			return nil, err
		}
	}

	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

func scanPackageEnvReads(pkgDir string, seen map[string]struct{}) error {
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading env-scan dir %s: %w", pkgDir, err)
	}

	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		path := filepath.Join(pkgDir, e.Name())
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			// One malformed file must not block the rest of the
			// install-prompt inventory; stderr names the skip so
			// publish operators can see it.
			fmt.Fprintf(os.Stderr, "warning: skipping unparseable env-scan file %s: %v\n", path, err)
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
			if isDeniedDiscoveredEnvVar(name) {
				return true
			}
			seen[name] = struct{}{}
			return true
		})
	}
	return nil
}

func isDeniedDiscoveredEnvVar(name string) bool {
	if _, deny := systemEnvVarDenylist[name]; deny {
		return true
	}
	return strings.HasSuffix(name, configFileEnvSuffix)
}

// The writer already declared spec-driven auth and endpoint-template
// keys; this pass fills gaps the spec did not name (per-instance
// BASE_URL, hand-written auth helpers) without re-reading
// .printing-press.json, which may not be on disk yet.
//
// Safe by construction:
//   - APIs with neither scanned package produce no changes.
//   - Env vars already declared in mcp_config.env are skipped.
//   - Platform-profile CLIs keep PRINTING_PRESS_CLIENT_PROFILE as the
//     credential surface. Suffix-classified per-instance reads (BASE_URL
//     and similar) and endpoint-template placeholders ({shop}) are still
//     promoted: the profile selector does not fill a store hostname.
//     Credential Getenv calls in config.go are not re-added beside the
//     profile selector.
//   - Goldens for spec-driven APIs without hand-written client code are
//     untouched for credential names because those os.Getenv calls all
//     resolve to names already in mcp_config.env (or are skipped under
//     the platform-profile rule).
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

	generated := make(map[string]struct{}, len(envReads))
	for _, name := range envReads {
		generated[name] = struct{}{}
	}
	cli.generatedEnvReads = generated
	cli = dropCollidingEndpointTemplateOverrides(cli, generated)

	platformProfiles := usesPlatformClientProfiles(dir)
	var missing []string
	for _, name := range envReads {
		if _, declared := manifest.Server.MCPConfig.Env[name]; declared {
			continue
		}
		if platformProfiles && !isPlatformProfileSafeDiscoveredEnvVar(cli, name) {
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
		if templateVar, ok := endpointTemplateVarForEnv(cli, name); ok {
			_, entry := endpointTemplateUserConfigEntry(cli, templateVar)
			manifest.UserConfig[key] = entry
			continue
		}
		entryRequired := required
		sensitive := true
		if isNonCredentialDiscoveredEnvVar(name) {
			entryRequired = false
			sensitive = false
		}
		manifest.UserConfig[key] = MCPBVar{
			Type:        mcpbVarTypeString,
			Title:       name,
			Description: discoveredEnvDescription(cli, name, entryRequired),
			Sensitive:   sensitive,
			Required:    entryRequired,
		}
	}

	out, err := marshalMCPBManifest(manifest)
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath, out, 0o644)
}

// discoveredEnvDescription mirrors envVarDescription's shape but flags the
// field as discovered from generated source so an install-page reader knows
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
	b.WriteString(" MCP server.")
	if isNonCredentialDiscoveredEnvVar(envVar) {
		b.WriteString(" Per-instance, non-secret setting; not a credential.")
	} else {
		b.WriteString(" Required by the generated client for credential refresh or hand-written auth flow.")
	}
	return b.String()
}

// Request-behavior and per-instance overrides are optional and non-sensitive
// because their absence does not affect authentication. Match by environment-
// variable suffix so classification stays independent of the generated file
// that reads the value.
func isNonCredentialDiscoveredEnvVar(name string) bool {
	for _, suffix := range nonCredentialEnvSuffixes {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// The profile selector covers credentials that are not also required
// endpoint placeholders. Suffix-classified knobs and declared endpoint
// vars (including a credential-shaped default such as {access_token})
// stay collectible; a colliding auth override is not an endpoint var
// and stays off the installer.
func isPlatformProfileSafeDiscoveredEnvVar(cli CLIManifest, name string) bool {
	if isEndpointTemplateEnvVar(cli, name) {
		return true
	}
	if isAuthOrCredentialEnvVar(cli, name) {
		return false
	}
	return isNonCredentialDiscoveredEnvVar(name)
}
