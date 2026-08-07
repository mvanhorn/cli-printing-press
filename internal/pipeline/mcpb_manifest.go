package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/mvanhorn/cli-printing-press/v4/internal/version"
)

// MCPB-bundle constants. Promoted from string literals so a typo here can't
// silently flip semantics — particularly authRequiresCredential, where a
// renamed auth type would otherwise default to "not required."
const (
	mcpbServerTypeBinary = "binary"
	mcpbVarTypeString    = "string"

	authTypeAPIKey        = "api_key"
	authTypeBearerToken   = "bearer_token"
	authTypeOAuth2        = "oauth2"
	authTypeOAuth2Refresh = "oauth2_refresh"
)

// defaultMCPBPlatforms is the set of host platforms our generated bundles
// target. Matches goreleaser's default Go cross-compile matrix.
var defaultMCPBPlatforms = []string{"darwin", "linux", "win32"}

var semverVersionRE = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)

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
// generator can fill from existing spec or manifest metadata. Authors who need
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
	out, err := marshalMCPBManifest(buildMCPBManifest(dir, m))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, MCPBManifestFilename), out, 0o644); err != nil {
		return err
	}
	if usesPlatformClientProfiles(dir) {
		return nil
	}
	// Extend the just-written manifest with env vars read by
	// internal/client/*.go that the spec-driven build didn't surface
	// (credential-flow JWT refreshers, hand-written auth helpers, etc.).
	// Runs from every writer call site so the bundle path reads a
	// reconciled manifest regardless of whether it came through lock+promote
	// or a one-off bundle build.
	return reconcileMCPBManifestFromClient(dir, m)
}

// marshalMCPBManifest serializes an MCPBManifest with the same encoder
// settings the writer uses end-to-end. SetEscapeHTML(false) so `>=1.0.0`
// stays readable instead of `>=1.0.0`.
func marshalMCPBManifest(manifest MCPBManifest) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(manifest); err != nil {
		return nil, fmt.Errorf("marshaling MCPB manifest: %w", err)
	}
	return buf.Bytes(), nil
}

func buildMCPBManifest(dir string, m CLIManifest) MCPBManifest {
	// display_name and description use opposite preservation rules:
	// canonical wins for display_name (so spec updates flow through),
	// existing wins for description (so hand-edits survive regen). Both
	// consult the existing manifest.json — load once, share the snapshot.
	existing := loadExistingMCPBManifest(dir)

	displayName := m.DisplayName
	if displayName == "" && existing != nil && existing.DisplayName != "" && existing.DisplayName != m.APIName {
		displayName = existing.DisplayName
	}
	if displayName == "" {
		displayName = m.APIName
	}
	launchEnv := buildMCPBEnv(m)
	userConfig := buildMCPBUserConfig(m)
	if usesPlatformClientProfiles(dir) {
		launchEnv = map[string]string{"PRINTING_PRESS_CLIENT_PROFILE": "${user_config.printing_press_client_profile}"}
		userConfig = map[string]MCPBVar{
			"printing_press_client_profile": {
				Type: mcpbVarTypeString, Title: "Client profile", Required: true,
				Description: "Binds the MCP server to an existing tenant-gated Printing Press client profile.",
			},
		}
	}

	return MCPBManifest{
		ManifestVersion: MCPBManifestVersion,
		Name:            m.MCPBinary,
		DisplayName:     displayName,
		// The generated on-disk manifest does not know the printed CLI's
		// release tag yet. Release packaging can stamp the bundle version
		// into the ZIP without mutating this generate-time manifest.
		Version:     bundleVersion(m),
		Description: manifestDescription(existing, m, displayName),
		Author:      MCPBAuthor{Name: "CLI Printing Press"},
		License:     "Apache-2.0",
		Server: MCPBServer{
			Type:       mcpbServerTypeBinary,
			EntryPoint: "bin/" + m.MCPBinary,
			MCPConfig: MCPBLaunchSpec{
				Command: "${__dirname}/bin/" + m.MCPBinary,
				Args:    []string{},
				Env:     launchEnv,
			},
		},
		UserConfig: userConfig,
		Compatibility: &MCPBCompat{
			ClaudeDesktop: minClaudeDesktopVersion,
			Platforms:     defaultMCPBPlatforms,
		},
	}
}

func usesPlatformClientProfiles(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "internal", "platform", "profile.go"))
	return err == nil && !info.IsDir()
}

// bundleVersion returns the best known printed CLI bundle version at generate
// time. Release packaging may still stamp the final public-library version
// into the ZIP without mutating this generate-time manifest.
func bundleVersion(m CLIManifest) string {
	if v := strings.TrimSpace(m.APIVersion); isSemverVersion(v) {
		return v
	}
	if v := strings.TrimSpace(m.PrintingPressVersion); isSemverVersion(v) {
		return v
	}
	if v := strings.TrimSpace(version.Version); isSemverVersion(v) {
		return v
	}
	return "0.0.0"
}

func isSemverVersion(v string) bool {
	return semverVersionRE.MatchString(v)
}

// manifestDescription preserves hand-edited bundle descriptions while letting
// canonical manifest descriptions refresh known generated defaults and legacy
// literal-ellipsis truncations.
func manifestDescription(existing *existingMCPBManifest, m CLIManifest, displayName string) string {
	derivedDefault := displayNameForConcat(displayName) + " API surface as MCP tools."
	priorDerivedDefault := displayName + " API surface as MCP tools."
	if existing != nil && existing.Description != "" &&
		existing.Description != derivedDefault &&
		existing.Description != priorDerivedDefault &&
		!naming.HasLiteralEllipsisSuffix(existing.Description) {
		return existing.Description
	}
	if m.Description != "" {
		return m.Description
	}
	return derivedDefault
}

// displayNameForConcat strips a trailing " API" from displayName so
// concatenating with text that already names the API doesn't read
// "Stripe API API surface as MCP tools." or "Stripe API MCP server."
// Spec authors commonly include " API" as a suffix in info.title and
// x-display-name; we let them keep that form for the manifest's
// display_name field while removing the redundancy at concat sites.
func displayNameForConcat(displayName string) string {
	return strings.TrimSuffix(displayName, " API")
}

// existingMCPBManifest is the subset of manifest.json the manifest writer
// reads when refreshing a published CLI's bundle. Single load site so the
// display_name and description preservation rules don't each re-read the
// file on every WriteMCPBManifest call.
type existingMCPBManifest struct {
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
}

// loadExistingMCPBManifest returns nil when the file is missing or
// unparseable. Callers branch on nil; non-nil means the snapshot is
// usable but each field still needs its own "is this a derived default?"
// check (see buildMCPBManifest and manifestDescription).
func loadExistingMCPBManifest(dir string) *existingMCPBManifest {
	data, err := os.ReadFile(filepath.Join(dir, MCPBManifestFilename))
	if err != nil {
		return nil
	}
	var existing existingMCPBManifest
	if err := json.Unmarshal(data, &existing); err != nil {
		return nil
	}
	return &existing
}

// buildMCPBEnv maps each declared auth env var into the launch spec's env
// block, pointing at the corresponding user_config slot. The host fills in
// the value at runtime from what the user typed (or whatever the keychain
// has cached). Empty list returns nil so the manifest stays compact.
func buildMCPBEnv(m CLIManifest) map[string]string {
	authEnvVarSpecs := mcpbUserConfigAuthEnvVars(m)
	if len(authEnvVarSpecs) == 0 && len(m.EndpointTemplateVars) == 0 {
		return nil
	}
	env := make(map[string]string, len(authEnvVarSpecs)+len(m.EndpointTemplateVars))
	for _, envVar := range authEnvVarSpecs {
		env[envVar.Name] = "${user_config." + userConfigKey(envVar.Name) + "}"
	}
	for _, templateVar := range m.EndpointTemplateVars {
		name := endpointTemplateEnvVar(m, templateVar)
		env[name] = "${user_config." + userConfigKey(name) + "}"
	}
	return env
}

// buildMCPBUserConfig translates each declared auth env var and endpoint
// template var into a user_config entry. Required-ness for auth depends on
// auth type: composed/cookie flows mean some tools work unauthenticated, so
// we keep the field optional and let the user skip it; api_key/bearer_token
// mean the API needs the credential to do anything useful, so we mark
// required. Endpoint template vars are required only when the spec offers
// no fallback default: path-positional placeholders (Shopify {shop},
// ServiceTitan {tenant}) have no spec-level default and must be supplied,
// but a server-URL variable carrying a `default:` value resolves at runtime
// without user input, so marking it Required = true alongside a Default
// presents Claude Desktop with a contradictory user_config (required field
// pre-filled with a vendor-placeholder value the user is unlikely to want).
func buildMCPBUserConfig(m CLIManifest) map[string]MCPBVar {
	authEnvVarSpecs := mcpbUserConfigAuthEnvVars(m)
	if len(authEnvVarSpecs) == 0 && len(m.EndpointTemplateVars) == 0 {
		return nil
	}
	vars := make(map[string]MCPBVar, len(authEnvVarSpecs)+len(m.EndpointTemplateVars))
	singleAuthEnvVar := len(authEnvVarSpecs) == 1
	for _, envVar := range authEnvVarSpecs {
		required := envVar.Required && !m.AuthOptional
		title, description := authUserConfigText(m, envVar, required, singleAuthEnvVar)
		vars[userConfigKey(envVar.Name)] = MCPBVar{
			Type:        mcpbVarTypeString,
			Title:       title,
			Description: description,
			Sensitive:   envVar.Sensitive,
			Required:    required,
		}
	}
	for _, templateVar := range m.EndpointTemplateVars {
		name := endpointTemplateEnvVar(m, templateVar)
		defaultValue := endpointTemplateDefault(m, templateVar)
		vars[userConfigKey(name)] = MCPBVar{
			Type:        mcpbVarTypeString,
			Title:       name,
			Description: endpointTemplateVarDescription(templateVar, name),
			Required:    defaultValue == "",
			Default:     defaultValue,
		}
	}
	return vars
}

func mcpbUserConfigAuthEnvVars(m CLIManifest) []spec.AuthEnvVar {
	envVarSpecs := (ManifestAuth{
		EnvVars:     m.AuthEnvVars,
		EnvVarSpecs: m.AuthEnvVarSpecs,
	}).EffectiveEnvVarSpecs()
	if len(m.AuthEnvVarSpecs) == 0 && len(m.AuthEnvVars) > 0 {
		required := authRequiresCredential(m.AuthType)
		for i := range envVarSpecs {
			envVarSpecs[i].Required = required
		}
	}
	if len(envVarSpecs) == 0 && len(m.AuthAdditionalHeaders) == 0 {
		return nil
	}
	filtered := make([]spec.AuthEnvVar, 0, len(envVarSpecs)+len(m.AuthAdditionalHeaders))
	seen := make(map[string]struct{}, len(envVarSpecs))
	for _, envVar := range envVarSpecs {
		if envVar.Name == "" {
			continue
		}
		envVar.Kind = envVar.EffectiveKind()
		seen[envVar.Name] = struct{}{}
		filtered = append(filtered, envVar)
	}
	// Sibling-scheme credentials (e.g. an apiKey header alongside an OAuth
	// bearer) ride the same user_config + env-forwarding path so MCP hosts
	// prompt for them at install time. Without this, composed-auth specs ship
	// install bundles that silently 401 at first request.
	for _, ah := range m.AuthAdditionalHeaders {
		ev := ah.EnvVar
		if ev.Name == "" {
			continue
		}
		if _, dup := seen[ev.Name]; dup {
			continue
		}
		ev.Kind = spec.AuthEnvVarKindPerCall
		seen[ev.Name] = struct{}{}
		filtered = append(filtered, ev)
	}
	return filtered
}

func endpointTemplateEnvVar(m CLIManifest, templateVar string) string {
	if override, ok := m.EndpointTemplateEnvOverrides[templateVar]; ok {
		if trimmed := strings.TrimSpace(override); trimmed != "" {
			return trimmed
		}
	}
	return spec.DefaultEndpointTemplateEnvName(m.APIName, templateVar)
}

// userConfigKey lowercases the env var so manifest user_config keys match
// the `${user_config.foo_bar}` substitution syntax in mcp_config.env.
func userConfigKey(envVar string) string {
	return strings.ToLower(envVar)
}

func endpointTemplateVarDescription(templateVar, envVar string) string {
	return fmt.Sprintf("Sets %s for the endpoint template variable {%s}.", envVar, templateVar)
}

func authUserConfigText(m CLIManifest, envVar spec.AuthEnvVar, required bool, singleAuthEnvVar bool) (string, string) {
	title := envVar.Name
	if singleAuthEnvVar {
		if override := strings.TrimSpace(m.AuthTitle); override != "" {
			title = override
		}
		if description := strings.TrimSpace(m.AuthDescription); description != "" {
			return title, description
		}
	}
	if description := strings.TrimSpace(envVar.Description); description != "" {
		if !required {
			return title, "Optional. " + description
		}
		return title, description
	}
	return title, envVarDescription(m, envVar, required)
}

func endpointTemplateDefault(m CLIManifest, templateVar string) string {
	if v := m.EndpointTemplateVarDefaults[templateVar]; v != "" {
		return v
	}
	if strings.EqualFold(templateVar, "api_version") {
		return m.APIVersion
	}
	return ""
}

// envVarDescription is the help text under each user_config field. The
// registration URL (when we have one) is what makes the difference between
// "fill this in" and "I don't know where to get this value."
func envVarDescription(m CLIManifest, envVar spec.AuthEnvVar, required bool) string {
	var b strings.Builder
	if !required {
		b.WriteString("Optional. ")
	}
	switch envVar.EffectiveKind() {
	case spec.AuthEnvVarKindAuthFlowInput:
		b.WriteString("Collects ")
		b.WriteString(envVar.Name)
		b.WriteString(" for the auth setup flow used by the ")
	case spec.AuthEnvVarKindHarvested:
		b.WriteString("Stores ")
		b.WriteString(envVar.Name)
		b.WriteString(" after it is harvested by the auth setup flow for the ")
	default:
		b.WriteString("Sets ")
		b.WriteString(envVar.Name)
		b.WriteString(" for the ")
	}
	if m.DisplayName != "" {
		b.WriteString(displayNameForConcat(m.DisplayName))
	} else {
		b.WriteString(m.APIName)
	}
	b.WriteString(" MCP server.")
	if m.AuthKeyURL != "" && envVar.EffectiveKind() != spec.AuthEnvVarKindHarvested {
		b.WriteString(" Get a credential from ")
		b.WriteString(m.AuthKeyURL)
		b.WriteString(".")
	}
	return b.String()
}

// authRequiresCredential decides whether a user_config field is required.
// api_key/bearer_token/oauth2/oauth2_refresh gate every API call on the credential.
// cookie/composed flows have unauth fallbacks for some tools, so we let
// the user skip and hit the parts that work without credentials.
func authRequiresCredential(authType string) bool {
	switch authType {
	case authTypeAPIKey, authTypeBearerToken, authTypeOAuth2, authTypeOAuth2Refresh:
		return true
	default:
		return false
	}
}
