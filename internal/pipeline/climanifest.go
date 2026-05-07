package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v3/catalog"
	catalogpkg "github.com/mvanhorn/cli-printing-press/v3/internal/catalog"
	"github.com/mvanhorn/cli-printing-press/v3/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v3/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/mvanhorn/cli-printing-press/v3/internal/version"
)

// CLIManifestFilename is the name of the manifest file written to each
// published CLI directory.
const CLIManifestFilename = ".printing-press.json"

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
	Owner                string            `json:"owner,omitempty"`
	SpecURL              string            `json:"spec_url,omitempty"`
	SpecPath             string            `json:"spec_path,omitempty"`
	SpecFormat           string            `json:"spec_format,omitempty"`
	SpecChecksum         string            `json:"spec_checksum,omitempty"`
	RunID                string            `json:"run_id,omitempty"`
	CatalogEntry         string            `json:"catalog_entry,omitempty"`
	Category             string            `json:"category,omitempty"`
	Description          string            `json:"description,omitempty"`
	MCPBinary            string            `json:"mcp_binary,omitempty"`
	MCPToolCount         int               `json:"mcp_tool_count,omitempty"`
	MCPPublicToolCount   int               `json:"mcp_public_tool_count,omitempty"`
	MCPReady             string            `json:"mcp_ready,omitempty"`
	APIVersion           string            `json:"api_version,omitempty"` // from the spec's info.version — provenance only, not the CLI version
	AuthType             string            `json:"auth_type,omitempty"`
	AuthEnvVars          []string          `json:"auth_env_vars,omitempty"`
	AuthEnvVarSpecs      []spec.AuthEnvVar `json:"auth_env_var_specs,omitempty"`
	EndpointTemplateVars []string          `json:"endpoint_template_vars,omitempty"`
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
	populateMCPMetadata(&m, parsed)
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

	return true, WriteCLIManifest(dir, m)
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
	m.EndpointTemplateVars = parsed.EndpointTemplateVars
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
	Owner         string                 // resolved owner attribution (manifest preserve > copyright parse > git config)
	RunID         string                 // YYYYMMDD-HHMMSS, derived from --research-dir basename when empty
	Spec          *spec.APISpec          // parsed spec for MCP metadata (nil if unavailable)
	NovelFeatures []NovelFeatureManifest // transcendence features from research (nil if unavailable)
}

// runIDPattern matches the canonical pipeline run_id shape: YYYYMMDD-HHMMSS.
// When an arbitrary path basename happens to match this pattern, treat it as
// a real run_id; otherwise fall back to empty (and warn at the call site).
var runIDPattern = regexp.MustCompile(`^\d{8}-\d{6}$`)

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

// WriteManifestForGenerate writes a .printing-press.json manifest into the
// generated CLI directory. This is the generate-command counterpart of
// writeCLIManifestForPublish (which operates on PipelineState).
func WriteManifestForGenerate(p GenerateManifestParams) error {
	m := CLIManifest{
		SchemaVersion:        1,
		GeneratedAt:          time.Now().UTC(),
		PrintingPressVersion: version.Version,
		APIName:              p.APIName,
		CLIName:              naming.CLI(p.APIName),
		RunID:                p.RunID,
		Owner:                p.Owner,
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
	if entry, err := catalogpkg.LookupFS(catalog.FS, p.APIName); err == nil {
		m.CatalogEntry = entry.Name
		m.Category = entry.Category
		m.Description = entry.Description
		// Catalog's display_name wins over spec/title fallback, while explicit
		// spec display_name / x-display-name still wins in populateMCPMetadata.
		if entry.DisplayName != "" {
			m.DisplayName = entry.DisplayName
		}
	}

	// Record the API version from the spec for provenance (not the CLI version).
	if p.Spec != nil && p.Spec.Version != "" {
		m.APIVersion = p.Spec.Version
	}

	// Populate MCP metadata from the parsed spec.
	if p.Spec != nil {
		populateMCPMetadata(&m, p.Spec)
	}
	if len(p.NovelFeatures) > 0 {
		m.NovelFeatures = p.NovelFeatures
	}

	if err := WriteCLIManifest(p.OutputDir, m); err != nil {
		return err
	}
	// Emit MCPB manifest.json next to .printing-press.json. Pass the
	// in-memory struct so we don't re-read the file we just wrote.
	return WriteMCPBManifestFromStruct(p.OutputDir, m)
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
