package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v2/internal/version"
)

// MCPB-bundle constants. Promoted from string literals so a typo here can't
// silently flip semantics — particularly authRequiresCredential, where a
// renamed auth type would otherwise default to "not required."
const (
	mcpbServerTypeBinary = "binary"
	mcpbVarTypeString    = "string"

	authTypeAPIKey      = "api_key"
	authTypeBearerToken = "bearer_token"
	authTypeOAuth2      = "oauth2"
)

// defaultMCPBPlatforms is the set of host platforms our generated bundles
// target. Matches goreleaser's default Go cross-compile matrix.
var defaultMCPBPlatforms = []string{"darwin", "linux", "win32"}

// minClaudeDesktopVersion is the minimum Claude Desktop release that
// understands the MCPB bundle format we emit. 1.0.0 is the version that
// introduced MCPB support (Nov 2025); bump this if we adopt schema fields
// that older Claude Desktop releases reject. Living in one place beats
// hunting it down across goldens and templates if/when that day comes.
const minClaudeDesktopVersion = ">=1.0.0"

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

// WriteMCPBManifest emits manifest.json for a published CLI directory by
// reading .printing-press.json. Skips silently only when the CLI dir has
// no .printing-press.json or no MCP binary — every other CLI ships a
// manifest, including composed/cookie-auth ones with a "partial" MCPReady
// label. The user_config block conveys auth-required-or-optional via
// authRequiresCredential, which is enough for the host to prompt or skip.
//
// Callers that already have the CLIManifest in memory should use
// WriteMCPBManifestFromStruct to avoid the re-read.
func WriteMCPBManifest(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	if err != nil {
		return nil
	}
	var m CLIManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("parsing manifest for MCPB: %w", err)
	}
	return WriteMCPBManifestFromStruct(dir, m)
}

// WriteMCPBManifestFromStruct is the in-memory variant of WriteMCPBManifest.
// Use it when the CLIManifest was just built and writing it back to disk
// only to re-read it would be wasted work.
func WriteMCPBManifestFromStruct(dir string, m CLIManifest) error {
	if m.MCPBinary == "" {
		return nil
	}
	// SetEscapeHTML(false) so `>=1.0.0` stays readable instead of `>=1.0.0`.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(buildMCPBManifest(dir, m)); err != nil {
		return fmt.Errorf("marshaling MCPB manifest: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, MCPBManifestFilename), buf.Bytes(), 0o644)
}

func buildMCPBManifest(dir string, m CLIManifest) MCPBManifest {
	// Resolution order mirrors manifestDescription: canonical source first
	// (.printing-press.json populated by spec.display_name on modern
	// prints), then existing manifest.json for older library CLIs whose
	// codemod-baked display_name carries the right brand casing, then
	// derived from the API slug. Without the existing-manifest fallback,
	// mcp-sync regressed library CLIs from "ESPN" → "espn".
	displayName := m.DisplayName
	if displayName == "" {
		displayName = readExistingManifestDisplayName(dir, m.APIName)
	}
	if displayName == "" {
		displayName = m.APIName
	}

	return MCPBManifest{
		ManifestVersion: MCPBManifestVersion,
		Name:            m.MCPBinary,
		DisplayName:     displayName,
		// Bundle version tracks the printing-press release that produced
		// it so Claude Desktop's update detection sees a fresh value on
		// regeneration. A hardcoded "1.0.0" would defeat the host's
		// "newer bundle available" prompt.
		Version:     bundleVersion(m),
		Description: manifestDescription(dir, m, displayName),
		Author:      MCPBAuthor{Name: "CLI Printing Press"},
		License:     "Apache-2.0",
		Server: MCPBServer{
			Type:       mcpbServerTypeBinary,
			EntryPoint: "bin/" + m.MCPBinary,
			MCPConfig: MCPBLaunchSpec{
				Command: "${__dirname}/bin/" + m.MCPBinary,
				Args:    []string{},
				Env:     buildMCPBEnv(m),
			},
		},
		UserConfig: buildMCPBUserConfig(m),
		Compatibility: &MCPBCompat{
			ClaudeDesktop: minClaudeDesktopVersion,
			Platforms:     defaultMCPBPlatforms,
		},
	}
}

// bundleVersion returns a semver-shaped version for the manifest. Prefers
// the manifest's recorded printing-press version (so two bundles built
// from different generator releases differ), falls back to the linker-
// stamped version when the manifest field is empty (older runs).
func bundleVersion(m CLIManifest) string {
	if m.PrintingPressVersion != "" {
		return m.PrintingPressVersion
	}
	if version.Version != "" {
		return version.Version
	}
	return "0.0.0"
}

// manifestDescription prefers the catalog/spec description verbatim and
// only falls back to a derived sentence when nothing better is available.
// We deliberately keep this single-line — long_description is reserved for
// multi-paragraph context, which we don't synthesize from spec data today.
//
// Resolution order: the .printing-press.json description (canonical source
// when the printing-press version that produced the CLI populates it) →
// the existing manifest.json's description (preserves rich descriptions
// baked by older codemods or hand-edits when .printing-press.json lacks
// the field — e.g., library CLIs printed under v1.x predate the field) →
// derived from displayName.
func manifestDescription(dir string, m CLIManifest, displayName string) string {
	if m.Description != "" {
		return m.Description
	}
	if existing := readExistingManifestDescription(dir, displayName); existing != "" {
		return existing
	}
	return displayName + " API surface as MCP tools."
}

// readExistingManifestDescription returns the description from an existing
// manifest.json on disk, but only if it's a real description rather than
// the derived "<displayName> API surface as MCP tools." default we'd
// otherwise re-emit. Returning the derived form would defeat the
// fallback chain by treating an old derived-default as "preserved
// content" and never advancing past it.
func readExistingManifestDescription(dir, displayName string) string {
	data, err := os.ReadFile(filepath.Join(dir, MCPBManifestFilename))
	if err != nil {
		return ""
	}
	var existing struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(data, &existing); err != nil {
		return ""
	}
	derivedDefault := displayName + " API surface as MCP tools."
	if existing.Description == "" || existing.Description == derivedDefault {
		return ""
	}
	return existing.Description
}

// readExistingManifestDisplayName returns the display_name from an
// existing manifest.json if it's a real brand name rather than a derived
// fallback. The two derived forms we explicitly reject: the bare API slug
// (lowercase, e.g. "espn") that buildMCPBManifest emits when nothing
// better is known, and the title-cased slug ("Espn") that the older
// spec.EffectiveDisplayName fallback used to produce. Anything else —
// "ESPN", "Cal.com", "Company GOAT", "PokéAPI" — is real brand content
// from a hand-edit or a codemod and worth preserving across regen.
func readExistingManifestDisplayName(dir, apiSlug string) string {
	data, err := os.ReadFile(filepath.Join(dir, MCPBManifestFilename))
	if err != nil {
		return ""
	}
	var existing struct {
		DisplayName string `json:"display_name"`
	}
	if err := json.Unmarshal(data, &existing); err != nil {
		return ""
	}
	if existing.DisplayName == "" || existing.DisplayName == apiSlug {
		return ""
	}
	titleCased := titleCaseFirstRune(apiSlug)
	if existing.DisplayName == titleCased {
		return ""
	}
	return existing.DisplayName
}

// titleCaseFirstRune capitalizes the first ASCII letter of slug. Mirrors
// the older spec.EffectiveDisplayName fallback so we can detect and
// reject preserved title-cased slugs that masquerade as real brand names.
func titleCaseFirstRune(slug string) string {
	if slug == "" {
		return ""
	}
	runes := []rune(slug)
	if runes[0] >= 'a' && runes[0] <= 'z' {
		runes[0] -= 'a' - 'A'
	}
	return string(runes)
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
			Type:        mcpbVarTypeString,
			Title:       name,
			Description: envVarDescription(m, name, required),
			Sensitive:   true,
			Required:    required,
		}
	}
	return vars
}

// userConfigKey lowercases the env var so manifest user_config keys match
// the `${user_config.foo_bar}` substitution syntax in mcp_config.env.
func userConfigKey(envVar string) string {
	return strings.ToLower(envVar)
}

// envVarDescription is the help text under each user_config field. The
// registration URL (when we have one) is what makes the difference between
// "fill this in" and "I don't know where to get this value."
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
// api_key/bearer_token/oauth2 gate every API call on the credential.
// cookie/composed flows have unauth fallbacks for some tools, so we let
// the user skip and hit the parts that work without credentials.
func authRequiresCredential(authType string) bool {
	switch authType {
	case authTypeAPIKey, authTypeBearerToken, authTypeOAuth2:
		return true
	default:
		return false
	}
}
