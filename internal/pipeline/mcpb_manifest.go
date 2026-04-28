package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MCPBManifestFilename is the file the host (Claude Desktop, Claude Code,
// MCP for Windows, future MCPB-aware clients) reads when installing a
// .mcpb bundle. Spec: https://github.com/modelcontextprotocol/mcpb
const MCPBManifestFilename = "manifest.json"

// MCPBManifestVersion pins the manifest schema version we emit. Bump when
// the upstream MCPB spec advances and we adopt newer fields.
const MCPBManifestVersion = "0.3"

// MCPBManifest is the on-disk shape of the manifest.json sitting at the
// root of an MCPB bundle ZIP. Field names and JSON tags match the upstream
// schema at https://github.com/modelcontextprotocol/mcpb/blob/main/MANIFEST.md.
// We do not exhaustively model every optional field — only what the
// generator can fill from existing spec/catalog metadata. Authors who need
// niche fields (icons, screenshots, prompts, localization) can hand-edit
// the emitted manifest.json before bundling, which lives next to the CLI
// source like .printing-press.json does.
type MCPBManifest struct {
	ManifestVersion string             `json:"manifest_version"`
	Name            string             `json:"name"`
	DisplayName     string             `json:"display_name,omitempty"`
	Version         string             `json:"version"`
	Description     string             `json:"description"`
	LongDescription string             `json:"long_description,omitempty"`
	Author          MCPBAuthor         `json:"author"`
	Repository      *MCPBRepo          `json:"repository,omitempty"`
	License         string             `json:"license,omitempty"`
	Keywords        []string           `json:"keywords,omitempty"`
	Server          MCPBServer         `json:"server"`
	UserConfig      map[string]MCPBVar `json:"user_config,omitempty"`
	Compatibility   *MCPBCompat        `json:"compatibility,omitempty"`
}

// MCPBAuthor identifies the bundle publisher. The upstream schema accepts
// either a string or this object form; the object form gives Claude Desktop
// a clickable URL on the install page.
type MCPBAuthor struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

// MCPBRepo points the host at the bundle's source for "view repository" links.
type MCPBRepo struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// MCPBServer describes how to launch the server inside the unpacked bundle.
// For our generated CLIs we always emit type "binary" — Go produces a
// pre-compiled native executable, no Node/Python runtime needed on the
// user's machine.
type MCPBServer struct {
	Type       string         `json:"type"`
	EntryPoint string         `json:"entry_point"`
	MCPConfig  MCPBLaunchSpec `json:"mcp_config"`
}

// MCPBLaunchSpec is the command/args/env triple the host substitutes at
// runtime. Use ${__dirname} for paths inside the bundle and
// ${user_config.<key>} for values the user filled in at install time.
type MCPBLaunchSpec struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

// MCPBVar is one entry in user_config — a value the host collects from the
// user during install. Sensitive fields are masked in the input UI and
// persisted to the OS keychain on hosts that support it.
type MCPBVar struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Sensitive   bool   `json:"sensitive,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Default     string `json:"default,omitempty"`
}

// MCPBCompat declares supported host versions and platforms. We default to
// claude_desktop >=1.0.0 (the version that introduced MCPB support) and the
// three desktop platforms goreleaser builds for.
type MCPBCompat struct {
	ClaudeDesktop string   `json:"claude_desktop,omitempty"`
	Platforms     []string `json:"platforms,omitempty"`
}

// WriteMCPBManifest emits manifest.json for a published CLI directory.
// Reads .printing-press.json to drive the manifest contents — every field
// the manifest needs already lives there after WriteCLIManifest runs.
//
// Skips emission silently in three cases:
//   - No .printing-press.json present (caller is using us outside a real CLI dir)
//   - MCPBinary empty (the spec didn't generate an MCP server)
//   - MCPReady == "cli-only" (composed-auth flows where MCP can't stand alone)
//
// The third case is the same gate the prior smithery.yaml emission used; we
// preserve it because cli-only readiness means a Claude Desktop user
// installing the bundle alone would get no working tools.
func WriteMCPBManifest(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	if err != nil {
		return nil
	}
	var m CLIManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("parsing manifest for MCPB: %w", err)
	}
	if m.MCPBinary == "" || m.MCPReady == "cli-only" {
		return nil
	}

	manifest := buildMCPBManifest(m)
	// SetEscapeHTML(false) keeps `>=1.0.0` readable in the compatibility
	// block instead of writing `>=1.0.0`. The manifest is consumed by
	// JSON-aware MCPB hosts, not embedded in HTML, so HTML-safe escaping
	// only adds noise.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(manifest); err != nil {
		return fmt.Errorf("marshaling MCPB manifest: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, MCPBManifestFilename), buf.Bytes(), 0o644)
}

func buildMCPBManifest(m CLIManifest) MCPBManifest {
	displayName := m.DisplayName
	if displayName == "" {
		displayName = m.APIName
	}

	manifest := MCPBManifest{
		ManifestVersion: MCPBManifestVersion,
		Name:            m.MCPBinary,
		DisplayName:     displayName,
		Version:         "1.0.0",
		Description:     manifestDescription(m, displayName),
		Author:          MCPBAuthor{Name: "CLI Printing Press"},
		License:         "Apache-2.0",
		Server: MCPBServer{
			Type:       "binary",
			EntryPoint: "bin/" + m.MCPBinary,
			MCPConfig: MCPBLaunchSpec{
				Command: "${__dirname}/bin/" + m.MCPBinary,
				Args:    []string{},
				Env:     buildMCPBEnv(m),
			},
		},
		UserConfig: buildMCPBUserConfig(m),
		Compatibility: &MCPBCompat{
			ClaudeDesktop: ">=1.0.0",
			Platforms:     []string{"darwin", "linux", "win32"},
		},
	}

	return manifest
}

// manifestDescription prefers the catalog/spec description verbatim and
// only falls back to a derived sentence when nothing better is available.
// We deliberately keep this single-line — long_description is reserved for
// multi-paragraph context, which we don't synthesize from spec data today.
func manifestDescription(m CLIManifest, displayName string) string {
	if m.Description != "" {
		return m.Description
	}
	return displayName + " API surface as MCP tools."
}

// buildMCPBEnv maps each declared auth env var into the launch spec's env
// block, pointing at the corresponding user_config slot. The host fills in
// the value at runtime from what the user typed (or whatever the keychain
// has cached). Empty list returns nil so the manifest stays compact.
func buildMCPBEnv(m CLIManifest) map[string]string {
	if len(m.AuthEnvVars) == 0 {
		return nil
	}
	env := make(map[string]string, len(m.AuthEnvVars))
	for _, name := range m.AuthEnvVars {
		env[name] = "${user_config." + userConfigKey(name) + "}"
	}
	return env
}

// buildMCPBUserConfig translates each declared auth env var into a
// user_config entry. Required-ness depends on auth type: composed/cookie
// flows mean some tools work unauthenticated, so we keep the field optional
// and let the user skip it; api_key/bearer_token mean the API needs the
// credential to do anything useful, so we mark required.
func buildMCPBUserConfig(m CLIManifest) map[string]MCPBVar {
	if len(m.AuthEnvVars) == 0 {
		return nil
	}
	required := authRequiresCredential(m.AuthType)
	vars := make(map[string]MCPBVar, len(m.AuthEnvVars))
	for _, name := range m.AuthEnvVars {
		vars[userConfigKey(name)] = MCPBVar{
			Type:        "string",
			Title:       envVarTitle(name),
			Description: envVarDescription(m, name, required),
			Sensitive:   true,
			Required:    required,
		}
	}
	return vars
}

// userConfigKey lowercases the env-var name so manifest.json's user_config
// keys match the conventional `${user_config.foo_bar}` substitution syntax.
// Hosts treat the key as a stable identifier, so we lowercase consistently.
func userConfigKey(envVar string) string {
	return strings.ToLower(envVar)
}

// envVarTitle is the field label shown in Claude Desktop's install dialog.
// We display the env-var name verbatim because that's what users will see
// elsewhere (in CLI auth setup, in error messages, in config files), and
// keeping a single name across surfaces avoids "what is FOO_TOKEN vs the
// 'Foo Token' field" confusion.
func envVarTitle(envVar string) string {
	return envVar
}

// envVarDescription writes one sentence the user will see under the input
// field. Includes the registration URL when we know it — that's the
// difference between "fill this in" and "I have no idea where to get this
// value." For optional auth types we say so explicitly.
func envVarDescription(m CLIManifest, envVar string, required bool) string {
	var b strings.Builder
	if !required {
		b.WriteString("Optional. ")
	}
	b.WriteString("Sets ")
	b.WriteString(envVar)
	b.WriteString(" for the ")
	if m.DisplayName != "" {
		b.WriteString(m.DisplayName)
	} else {
		b.WriteString(m.APIName)
	}
	b.WriteString(" MCP server.")
	if m.AuthKeyURL != "" {
		b.WriteString(" Get a credential from ")
		b.WriteString(m.AuthKeyURL)
		b.WriteString(".")
	}
	return b.String()
}

// authRequiresCredential decides whether a user_config field is required.
// api_key and bearer_token gate every API call on the credential, so
// skipping the prompt produces a nonfunctional install. cookie/composed
// flows have unauth fallbacks for some tools, so we let the user skip and
// hit the parts that work without credentials. Empty/none auth types
// produce no env vars in the first place, so the value here is moot.
func authRequiresCredential(authType string) bool {
	switch authType {
	case "api_key", "bearer_token", "oauth2":
		return true
	default:
		return false
	}
}
