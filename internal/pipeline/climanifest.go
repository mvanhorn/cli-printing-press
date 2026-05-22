package pipeline

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/catalog"
	catalogpkg "github.com/mvanhorn/cli-printing-press/v4/internal/catalog"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/mvanhorn/cli-printing-press/v4/internal/version"
)

// CLIManifestFilename is the name of the manifest file written to each
// published CLI directory.
const CLIManifestFilename = ".printing-press.json"

// CurrentCLIManifestSchemaVersion is the public-library provenance contract.
const CurrentCLIManifestSchemaVersion = 1

// PatchesIndexFilename is the customizations index written next to
// CLIManifestFilename for every printed CLI. The public library's Verify
// CI requires it on every fresh-print publish; agents append entries to
// patches[] as they apply in-session customizations to the generated code.
const PatchesIndexFilename = ".printing-press-patches.json"

// CurrentPatchesIndexSchemaVersion is the schema version stamped into
// newly emitted patches indexes. Matches the shape documented in
// internal/generator/templates/agents.md.tmpl.
const CurrentPatchesIndexSchemaVersion = 1

// PatchesIndex is the customizations index emitted alongside CLIManifest
// for every generated CLI. The Patches field is intentionally
// []json.RawMessage so an empty index serializes to "patches: []" rather
// than "patches: null", and agents authoring entries control the per-patch
// JSON shape directly.
type PatchesIndex struct {
	SchemaVersion            int               `json:"schema_version"`
	AppliedAt                string            `json:"applied_at"` // YYYY-MM-DD
	BaseRunID                string            `json:"base_run_id"`
	BasePrintingPressVersion string            `json:"base_printing_press_version"`
	Patches                  []json.RawMessage `json:"patches"`
}

// CLIManifest captures provenance metadata for a generated CLI.
// It is written to the root of each published CLI directory so the
// folder is self-describing even in isolation.
type CLIManifest struct {
	SchemaVersion        int       `json:"schema_version"`
	GeneratedAt          time.Time `json:"generated_at"`
	PrintingPressVersion string    `json:"printing_press_version"`
	// APIName is the canonical API identity (for example "espn" or "notion").
	// It is not the executable name, and for collision-renamed published copies
	// it may differ from the package directory key.
	APIName string `json:"api_name"`
	// DisplayName is the human-readable brand name used by user-facing
	// surfaces that don't want a kebab-case slug — Claude Desktop's
	// connector list, the MCPB manifest's display_name field, the MCP
	// server's protocol-level name. Sourced from the spec's display_name
	// (if set) or a matching catalog entry, with a title-cased fallback.
	DisplayName string `json:"display_name,omitempty"`
	// CLIName is the executable/binary name (for example "espn-pp-cli").
	// It does not track the slug-keyed library directory.
	CLIName string `json:"cli_name"`
	// Owner is the attribution recorded in generated copyright headers
	// (for example "hiten-shah"). Persisted here so subsequent regens
	// preserve attribution regardless of who's running the generator.
	Owner string `json:"owner,omitempty"`
	// Printer is the original printer's GitHub handle, preserved across regens.
	Printer string `json:"printer,omitempty"`
	// PrinterName is the optional display name rendered beside the printer handle.
	PrinterName        string            `json:"printer_name,omitempty"`
	SpecURL            string            `json:"spec_url,omitempty"`
	SpecPath           string            `json:"spec_path,omitempty"`
	SpecFormat         string            `json:"spec_format,omitempty"`
	SpecChecksum       string            `json:"spec_checksum,omitempty"`
	RunID              string            `json:"run_id,omitempty"`
	CatalogEntry       string            `json:"catalog_entry,omitempty"`
	Category           string            `json:"category,omitempty"`
	Description        string            `json:"description,omitempty"`
	MCPBinary          string            `json:"mcp_binary,omitempty"`
	MCPToolCount       int               `json:"mcp_tool_count,omitempty"`
	MCPPublicToolCount int               `json:"mcp_public_tool_count,omitempty"`
	MCPReady           string            `json:"mcp_ready,omitempty"`
	APIVersion         string            `json:"api_version,omitempty"` // from the spec's info.version — provenance only, not the CLI version
	AuthType           string            `json:"auth_type,omitempty"`
	AuthEnvVars        []string          `json:"auth_env_vars,omitempty"`
	AuthEnvVarSpecs    []spec.AuthEnvVar `json:"auth_env_var_specs,omitempty"`
	// AuthAdditionalHeaders mirrors AuthConfig.AdditionalHeaders so the MCPB
	// manifest's user_config block prompts for sibling-scheme per-call
	// credentials (e.g. an apiKey header alongside an OAuth bearer). Without
	// this field, agents installing the printed CLI via Claude Desktop never
	// see the second credential prompt and every request returns 401.
	AuthAdditionalHeaders        []spec.AdditionalAuthHeader `json:"auth_additional_headers,omitempty"`
	EndpointTemplateVars         []string                    `json:"endpoint_template_vars,omitempty"`
	EndpointTemplateEnvOverrides map[string]string           `json:"endpoint_template_env_overrides,omitempty"`
	// EndpointTemplateVarDefaults mirrors APISpec.EndpointTemplateVarDefaults
	// so a regenerating run, the MCPB manifest's user_config default fill,
	// and the public-library republish path all see the same fallback values
	// the parser captured from the spec. Empty for path-positional templates
	// (x-tenant-env-var style) since those have no spec-level default.
	EndpointTemplateVarDefaults map[string]string `json:"endpoint_template_var_defaults,omitempty"`
	// AuthKeyURL is the page where users register for an API key. Used by
	// downstream emitters (MCPB manifest user_config descriptions, doctor
	// hints) to point users at the right credential source.
	AuthKeyURL string `json:"auth_key_url,omitempty"`
	// AuthTitle and AuthDescription customize the install/config prompt for
	// the auth credential when the spec's service identity differs from the
	// wrapped API identity.
	AuthTitle       string `json:"auth_title,omitempty"`
	AuthDescription string `json:"auth_description,omitempty"`
	// AuthOptional is true when the credential gates a subset of features
	// (e.g., USDA nutrition backfill on recipe-goat) rather than every
	// API call. Drives the MCPB user_config Required field so opt-in
	// keys don't surface as mandatory in install dialogs.
	AuthOptional  bool                   `json:"auth_optional,omitempty"`
	NovelFeatures []NovelFeatureManifest `json:"novel_features,omitempty"`
}

// NovelFeatureManifest is a compact representation of a transcendence feature
// for the CLI manifest and registry. Stripped of Rationale (which stays in
// research.json and the README).
type NovelFeatureManifest struct {
	Name        string `json:"name"`
	Command     string `json:"command"`
	Description string `json:"description"`
}

// ReadCLIBinaryName reads .printing-press.json from dir and returns the
// cli_name field. Returns empty string when the file is missing or
// unparseable so callers can fall back to convention. Used by the MCPB
// bundle builder, which can't store the CLI binary name in manifest.json
// (Claude Desktop's MCPB v0.3 validator rejects unknown top-level keys).
func ReadCLIBinaryName(dir string) string {
	m, err := ReadCLIManifest(dir)
	if err != nil {
		return ""
	}
	return m.CLIName
}

// ReadCLIManifest decodes dir/.printing-press.json.
func ReadCLIManifest(dir string) (CLIManifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	if err != nil {
		return CLIManifest{}, err
	}
	var m CLIManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return CLIManifest{}, err
	}
	return m, nil
}

// RefreshCLIManifestFromSpec rereads dir/.printing-press.json, overlays the
// spec-derived fields from parsed (via populateMCPMetadata), and writes
// the result back. Used by mcp-sync to keep provenance in sync with
// spec.yaml — without this, spec.yaml updates to auth.key_url,
// auth.optional, auth.env_vars, and similar fields never reach
// downstream emitters (manifest.json, doctor, scorecard) because those
// read .printing-press.json, not the spec directly.
//
// Generate-time fields (spec_url, spec_path, spec_checksum,
// generated_at, printing_press_version, schema_version, novel_features,
// catalog_entry, category, cli_name, api_name, api_version, description)
// are preserved as-is. Only the spec-driven MCP/auth/display fields
// are refreshed.
//
// Returns nil silently when .printing-press.json is missing — callers
// generating from scratch don't need a provenance-refresh step.
func RefreshCLIManifestFromSpec(dir string, parsed *spec.APISpec) error {
	if parsed == nil {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading CLI manifest for refresh: %w", err)
	}
	var m CLIManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("parsing CLI manifest for refresh: %w", err)
	}
	existingDescription := m.Description
	populateMCPMetadata(&m, parsed)
	if preserveExistingDescription(existingDescription) {
		m.Description = existingDescription
	}
	return WriteCLIManifest(dir, m)
}

// WriteCLIManifest marshals m as indented JSON and writes it to
// dir/.printing-press.json.
func WriteCLIManifest(dir string, m CLIManifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling CLI manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, CLIManifestFilename), data, 0o644); err != nil {
		return fmt.Errorf("writing CLI manifest: %w", err)
	}
	return nil
}

// WritePatchesIndex emits .printing-press-patches.json into the generated
// CLI directory. The library's Verify CI rejects fresh-print publishes
// without this file; emitting an empty index here removes the friction
// for every future publish without affecting CLIs that already populated
// the file via in-session customization.
//
// Preserves any existing file unchanged so agent-applied patch entries
// survive regen (parallel to how resolveOwnerForExisting preserves
// printer/owner metadata across regenerate runs). The os.Stat probe is
// the simplest reliable preservation signal — the patches index has no
// "Generated by" header to detect ownership from content.
func WritePatchesIndex(dir, runID, pressVersion string) error {
	path := filepath.Join(dir, PatchesIndexFilename)
	if _, err := os.Stat(path); err == nil {
		return nil // preserve existing — agent-authored content survives regen
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking patches index: %w", err)
	}
	idx := PatchesIndex{
		SchemaVersion:            CurrentPatchesIndexSchemaVersion,
		AppliedAt:                time.Now().UTC().Format("2006-01-02"),
		BaseRunID:                runID,
		BasePrintingPressVersion: pressVersion,
		Patches:                  []json.RawMessage{},
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling patches index: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing patches index: %w", err)
	}
	return nil
}

func novelFeaturesToManifest(features []NovelFeature) []NovelFeatureManifest {
	built := make([]NovelFeatureManifest, 0, len(features))
	for _, nf := range features {
		built = append(built, NovelFeatureManifest{
			Name:        nf.Name,
			Command:     nf.Command,
			Description: nf.Description,
		})
	}
	return built
}

// SyncCLIManifestNovelFeatures records dogfood-verified novel features in the
// generated CLI manifest. Empty verified sets intentionally leave the manifest
// untouched so a failed or incomplete dogfood pass cannot erase prior metadata.
func SyncCLIManifestNovelFeatures(dir string, features []NovelFeature) (bool, error) {
	if len(features) == 0 {
		return false, nil
	}

	manifestPath := filepath.Join(dir, CLIManifestFilename)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("reading CLI manifest: %w", err)
	}

	var m CLIManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return false, fmt.Errorf("parsing CLI manifest: %w", err)
	}
	updated := novelFeaturesToManifest(features)
	if reflect.DeepEqual(m.NovelFeatures, updated) {
		return false, nil
	}
	m.NovelFeatures = updated

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return false, fmt.Errorf("parsing CLI manifest for raw update: %w", err)
	}
	if raw == nil {
		return false, fmt.Errorf("parsing CLI manifest for raw update: expected JSON object")
	}
	known, err := marshalCLIManifestFields(m)
	if err != nil {
		return false, err
	}
	maps.Copy(raw, known)
	rendered, err := marshalCLIManifestObject(raw)
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(manifestPath, rendered, 0o644); err != nil {
		return false, fmt.Errorf("writing CLI manifest: %w", err)
	}

	return true, nil
}

func marshalCLIManifestFields(m CLIManifest) (map[string]json.RawMessage, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshaling CLI manifest fields: %w", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing CLI manifest fields: %w", err)
	}
	return raw, nil
}

func marshalCLIManifestObject(raw map[string]json.RawMessage) ([]byte, error) {
	keys := orderedCLIManifestKeys(raw)
	var b strings.Builder
	b.WriteString("{\n")
	for i, key := range keys {
		name, err := json.Marshal(key)
		if err != nil {
			return nil, fmt.Errorf("marshaling CLI manifest key: %w", err)
		}
		value, err := formatRawJSONValue(raw[key])
		if err != nil {
			return nil, fmt.Errorf("formatting CLI manifest field %q: %w", key, err)
		}
		b.WriteString("  ")
		b.WriteString(string(name))
		b.WriteString(": ")
		b.WriteString(value)
		if i < len(keys)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString("}\n")
	return []byte(b.String()), nil
}

func orderedCLIManifestKeys(raw map[string]json.RawMessage) []string {
	known := []string{
		"schema_version",
		"generated_at",
		"printing_press_version",
		"api_name",
		"display_name",
		"cli_name",
		"owner",
		"printer",
		"printer_name",
		"spec_url",
		"spec_path",
		"spec_format",
		"spec_checksum",
		"run_id",
		"catalog_entry",
		"category",
		"description",
		"mcp_binary",
		"mcp_tool_count",
		"mcp_public_tool_count",
		"mcp_ready",
		"api_version",
		"auth_type",
		"auth_env_vars",
		"auth_env_var_specs",
		"endpoint_template_vars",
		"endpoint_template_env_overrides",
		"auth_key_url",
		"auth_title",
		"auth_description",
		"auth_optional",
		"novel_features",
	}

	keys := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, key := range known {
		if _, ok := raw[key]; ok {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	var unknown []string
	for key := range raw {
		if !seen[key] {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	return append(keys, unknown...)
}

func formatRawJSONValue(raw json.RawMessage) (string, error) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return "", err
	}
	lines := strings.Split(buf.String(), "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = "  " + lines[i]
	}
	return strings.Join(lines, "\n"), nil
}

// findArchivedSpec looks for a spec file archived alongside a generated CLI.
// generate archives the source spec as spec.json (for JSON inputs) or
// spec.yaml (for YAML inputs); older runs occasionally used spec.yml. Returns
// the first match's path and contents, or an empty path with nil error when
// no archive is present.
func findArchivedSpec(dir string) (string, []byte, error) {
	for _, name := range []string{"spec.json", "spec.yaml", "spec.yml"} {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err == nil {
			return path, data, nil
		}
		if !os.IsNotExist(err) {
			return "", nil, fmt.Errorf("reading %s: %w", path, err)
		}
	}
	return "", nil, nil
}

// specChecksum computes a SHA-256 checksum of the file at path.
// Returns "sha256:<hex>" on success, or an empty string if the file
// does not exist.
func specChecksum(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading spec for checksum: %w", err)
	}
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:]), nil
}

// computeMCPReady determines the MCP readiness label for scorecard /
// SKILL prose. It does NOT gate manifest emission — that decision lives
// in WriteMCPBManifestFromStruct and is purely "do we have an MCP binary
// to ship?". The label exists to set user expectations: full = every
// tool works without per-tool auth setup; partial = some tools work
// without credentials, others need auth provided through the companion
// CLI's flow (composed, cookie).
func computeMCPReady(authType string) string {
	switch authType {
	case "cookie", "composed":
		return "partial"
	default:
		return "full"
	}
}

func populateMCPMetadata(m *CLIManifest, parsed *spec.APISpec) {
	if parsed == nil {
		return
	}
	total, public := parsed.CountMCPTools()
	m.MCPBinary = naming.MCP(parsed.Name)
	m.MCPToolCount = total
	m.MCPPublicToolCount = public
	m.MCPReady = computeMCPReady(parsed.Auth.Type)
	m.AuthType = parsed.Auth.Type
	envVarSpecs := manifestAuthEnvVarSpecs(parsed)
	m.AuthEnvVars = manifestAuthEnvVarNames(parsed, envVarSpecs)
	if !spec.AllAuthEnvVarSpecsInferred(envVarSpecs) {
		m.AuthEnvVarSpecs = envVarSpecs
	}
	m.AuthAdditionalHeaders = parsed.Auth.AdditionalHeaders
	m.EndpointTemplateVars = parsed.EndpointTemplateVars
	m.EndpointTemplateEnvOverrides = parsed.EndpointTemplateEnvOverrides
	m.EndpointTemplateVarDefaults = parsed.EndpointTemplateVarDefaults
	m.AuthKeyURL = parsed.Auth.KeyURL
	m.AuthTitle = parsed.Auth.Title
	m.AuthDescription = parsed.Auth.Description
	m.AuthOptional = parsed.Auth.Optional
	// DisplayName precedence: explicit spec field > catalog-set existing
	// value > spec/title-derived fallback > slug-derived fallback.
	// OpenAPI info.title is useful as a fallback, but it is not explicit
	// enough to clobber a curated catalog value.
	if parsed.DisplayName != "" && !parsed.DisplayNameDerivedFromTitle {
		m.DisplayName = parsed.DisplayName
	} else if m.DisplayName == "" && parsed.DisplayName != "" {
		m.DisplayName = parsed.DisplayName
	} else if m.DisplayName == "" {
		m.DisplayName = parsed.EffectiveDisplayName()
	}
	// CLIDescription overrides existing m.Description so the spec's
	// CLI-shaped copy ships in manifest.json instead of the API-shaped
	// catalog default.
	if parsed.CLIDescription != "" {
		m.Description = parsed.CLIDescription
	}
}

func manifestAuthEnvVarNames(parsed *spec.APISpec, envVarSpecs []spec.AuthEnvVar) []string {
	if parsed == nil {
		return nil
	}
	if len(envVarSpecs) > 0 {
		return authEnvVarSpecNames(envVarSpecs)
	}
	seen := make(map[string]struct{})
	var names []string
	add := func(envVars []string) {
		for _, name := range envVars {
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			names = append(names, name)
		}
	}
	add(parsed.Auth.EnvVars)
	tierNames := make([]string, 0, len(parsed.TierRouting.Tiers))
	for name := range parsed.TierRouting.Tiers {
		tierNames = append(tierNames, name)
	}
	sort.Strings(tierNames)
	for _, name := range tierNames {
		add(parsed.TierRouting.Tiers[name].Auth.EnvVars)
	}
	return names
}

func manifestAuthEnvVarSpecs(parsed *spec.APISpec) []spec.AuthEnvVar {
	if parsed == nil {
		return nil
	}
	seen := make(map[string]int)
	var specs []spec.AuthEnvVar
	add := func(envVarSpecs []spec.AuthEnvVar) {
		for _, envVar := range envVarSpecs {
			if envVar.Name == "" {
				continue
			}
			if idx, ok := seen[envVar.Name]; ok {
				specs[idx] = envVar
				continue
			}
			seen[envVar.Name] = len(specs)
			specs = append(specs, envVar)
		}
	}

	parsed.Auth.NormalizeEnvVarSpecs("")
	add(parsed.Auth.EnvVarSpecs)

	tierNames := make([]string, 0, len(parsed.TierRouting.Tiers))
	for name := range parsed.TierRouting.Tiers {
		tierNames = append(tierNames, name)
	}
	sort.Strings(tierNames)
	for _, name := range tierNames {
		tier := parsed.TierRouting.Tiers[name]
		tier.Auth.NormalizeEnvVarSpecs("")
		parsed.TierRouting.Tiers[name] = tier
		add(tier.Auth.EnvVarSpecs)
	}
	return specs
}

// GenerateManifestParams holds the information available at generate time
// for writing a CLI manifest. Unlike PublishWorkingCLI (which has full
// PipelineState), the standalone generate command only knows the spec
// sources and output directory.
type GenerateManifestParams struct {
	APIName       string
	SpecSrcs      []string // --spec args (URLs or file paths)
	SpecURL       string   // --spec-url: explicit provenance URL (when --spec is a local downloaded file)
	DocsURL       string   // --docs URL, if used
	OutputDir     string
	Description   string                 // best generated user-facing catalog description
	DisplayName   string                 // best generated user-facing catalog display name
	Owner         string                 // resolved owner attribution (manifest preserve > copyright parse > git config)
	Printer       string                 // resolved printer @handle (manifest preserve > git config github.user > empty)
	PrinterName   string                 // resolved printer display name (manifest preserve > git config user.name > empty)
	RunID         string                 // YYYYMMDD-HHMMSS, derived from --research-dir basename when empty
	Spec          *spec.APISpec          // parsed spec for MCP metadata (nil if unavailable)
	NovelFeatures []NovelFeatureManifest // transcendence features from research (nil if unavailable)
}

// runIDPattern matches the canonical pipeline run_id shape: YYYYMMDD-HHMMSS.
// When an arbitrary path basename happens to match this pattern, treat it as
// a real run_id; otherwise fall back to empty (and warn at the call site).
var runIDPattern = regexp.MustCompile(`^\d{8}-\d{6}$`)

// runIDTimeFormat is the canonical YYYYMMDD-HHMMSS layout matched by
// runIDPattern. Kept as a const so the format and pattern can't drift.
const runIDTimeFormat = "20060102-150405"

// DeriveRunIDFromResearchDir extracts a canonical run_id from a research-dir
// path, or returns "" when no valid run_id can be derived. The standalone
// generate command does not load a PipelineState, so it cannot reach
// state.RunID directly; the basename of --research-dir is the only structured
// signal available without a state-loading refactor.
func DeriveRunIDFromResearchDir(researchDir string) string {
	if researchDir == "" {
		return ""
	}
	base := filepath.Base(researchDir)
	if runIDPattern.MatchString(base) {
		return base
	}
	return ""
}

// LoadAPINameFromResearchDir reads `<researchDir>/state.json` and returns the
// recorded api_name slug, or "" when the file is absent, unreadable, malformed,
// or has no api_name. The generate command uses this as an implicit --name
// override so a spec whose `info.title` derives to something different from
// the user's intended slug (e.g. "Canvas LMS API" vs `canvas`) still produces
// the slug-keyed cmd/ directory the rest of the pipeline expects. Explicit
// --name wins over this; an absent or unreadable state.json silently yields
// to the title-derived default.
func LoadAPINameFromResearchDir(researchDir string) string {
	if researchDir == "" {
		return ""
	}
	statePath := filepath.Join(researchDir, "state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		return ""
	}
	var probe struct {
		APIName string `json:"api_name"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return ""
	}
	return strings.TrimSpace(probe.APIName)
}

// WriteManifestForGenerate writes a .printing-press.json manifest into the
// generated CLI directory. This is the generate-command counterpart of
// writeCLIManifestForPublish (which operates on PipelineState).
//
// An empty p.RunID is auto-filled with a fresh timestamp so the emitted
// manifest satisfies publish-validate's required-run_id contract. Phase 5
// dogfood acceptance still needs the original research-dir-derived run_id,
// and the root.go --research-dir warning informs phase5 callers of that gap.
func WriteManifestForGenerate(p GenerateManifestParams) error {
	now := time.Now().UTC()
	runID := p.RunID
	if runID == "" {
		runID = now.Format(runIDTimeFormat)
	}
	var existingDescription string
	if existing, err := ReadCLIManifest(p.OutputDir); err == nil {
		existingDescription = existing.Description
	}
	m := CLIManifest{
		SchemaVersion:        CurrentCLIManifestSchemaVersion,
		GeneratedAt:          now,
		PrintingPressVersion: version.Version,
		APIName:              p.APIName,
		CLIName:              naming.CLI(p.APIName),
		RunID:                runID,
		Owner:                p.Owner,
		Printer:              p.Printer,
		PrinterName:          p.PrinterName,
	}

	// Populate spec_url / spec_path from the first spec source.
	if p.DocsURL != "" {
		m.SpecURL = p.DocsURL
		m.SpecFormat = "docs"
	} else if len(p.SpecSrcs) > 0 {
		src := p.SpecSrcs[0]
		if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
			m.SpecURL = src
		} else {
			m.SpecPath = src
			// Compute checksum and format from the actual input spec file.
			if data, err := os.ReadFile(src); err == nil {
				m.SpecFormat = detectSpecFormat(data)
				h := sha256.Sum256(data)
				m.SpecChecksum = "sha256:" + hex.EncodeToString(h[:])
			}
		}
	}

	// Explicit --spec-url overrides: when the user passed a local file that was
	// downloaded from a URL, record the original URL for reproducibility.
	if p.SpecURL != "" {
		m.SpecURL = p.SpecURL
	}

	// Fallback: detect format and checksum from any spec file cached in the output dir.
	if m.SpecFormat == "" || m.SpecChecksum == "" {
		if specFile, data, err := findArchivedSpec(p.OutputDir); err == nil && specFile != "" {
			if m.SpecFormat == "" {
				m.SpecFormat = detectSpecFormat(data)
			}
			if m.SpecChecksum == "" {
				if cs, err := specChecksum(specFile); err == nil {
					m.SpecChecksum = cs
				}
			}
		}
	}

	// Look up catalog entry for category/description/display-name enrichment.
	if entry := lookupCatalogEntryForGenerate(p.APIName, m.SpecURL); entry != nil {
		m.CatalogEntry = entry.Name
		m.Category = entry.Category
		m.Description = entry.Description
		// Catalog's display_name wins over spec/title fallback, while explicit
		// spec display_name / x-display-name still wins in populateMCPMetadata.
		if entry.DisplayName != "" {
			m.DisplayName = entry.DisplayName
		}
	}
	// Fall back to spec.Category for synthetic CLIs that aren't in the
	// embedded catalog. Without this, manifest.Category stays empty even
	// when the spec sets `category: travel`, and verify-skill's canonical-
	// sections check then expects the install URL to use "other" — putting
	// the rendered SKILL (which read category from the spec via the
	// template's .Category) and the manifest-derived expected SKILL out of
	// sync. The README/SKILL templates already resolve category through the
	// spec; the manifest writer was the lone holdout.
	if m.Category == "" && p.Spec != nil && p.Spec.Category != "" {
		m.Category = p.Spec.Category
	}

	// Record the API version from the spec for provenance (not the CLI version).
	if p.Spec != nil && p.Spec.Version != "" {
		m.APIVersion = p.Spec.Version
	}

	// Populate MCP metadata from the parsed spec.
	if p.Spec != nil {
		populateMCPMetadata(&m, p.Spec)
	}
	if displayName := strings.TrimSpace(p.DisplayName); displayName != "" {
		m.DisplayName = displayName
	}
	if description := strings.TrimSpace(p.Description); description != "" {
		m.Description = description
	}
	// A durable manifest description may be hand-edited after generation.
	// Operators can delete or replace the field when they want changed spec
	// prose to become canonical on a later generate run.
	if preserveExistingDescription(existingDescription) {
		m.Description = existingDescription
	}
	if len(p.NovelFeatures) > 0 {
		m.NovelFeatures = p.NovelFeatures
	}

	if err := WriteCLIManifest(p.OutputDir, m); err != nil {
		return err
	}
	// Emit the customizations index alongside .printing-press.json. The
	// library's Verify CI requires every fresh-print publish to ship one;
	// preserve-on-regen keeps agent-applied patch entries from being
	// clobbered by a later generate --force.
	if err := WritePatchesIndex(p.OutputDir, runID, version.Version); err != nil {
		return err
	}
	// Emit MCPB manifest.json next to .printing-press.json. Pass the
	// in-memory struct so we don't re-read the file we just wrote.
	return WriteMCPBManifestFromStruct(p.OutputDir, m)
}

func preserveExistingDescription(description string) bool {
	description = strings.TrimSpace(description)
	return description != "" && !naming.HasLiteralEllipsisSuffix(description)
}

func lookupCatalogEntryForGenerate(apiName, specURL string) *catalogpkg.Entry {
	if entry, err := catalogpkg.LookupFS(catalog.FS, apiName); err == nil {
		return entry
	}
	if specURL == "" {
		return nil
	}
	entries, err := catalogpkg.ParseFS(catalog.FS)
	if err != nil {
		return nil
	}
	for i := range entries {
		if entries[i].SpecURL == specURL {
			return &entries[i]
		}
	}
	return nil
}

// detectSpecFormat examines the raw spec bytes and returns a format
// string: "openapi3", "graphql", or "internal".
func detectSpecFormat(data []byte) string {
	if openapi.IsOpenAPI(data) {
		return "openapi3"
	}
	if openapi.IsGraphQLSDL(data) {
		return "graphql"
	}
	return "internal"
}
