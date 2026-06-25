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

// CLIReleaseManifestFilename is the public-library release ledger manifest.
// The generator writes a skeleton only; printing-press-library assigns the
// actual per-CLI release version after the publish PR merges.
const CLIReleaseManifestFilename = ".printing-press-release.json"

// CLIChangelogFilename is the public-library per-CLI changelog.
const CLIChangelogFilename = "CHANGELOG.md"

// CurrentCLIManifestSchemaVersion is the public-library provenance contract.
const CurrentCLIManifestSchemaVersion = 1

// PatchesIndexFilename is the legacy single-array customizations file. It is
// superseded by PatchesDirName (one file per patch) because the single array
// conflicts on every concurrent same-CLI PR (mvanhorn/cli-printing-press#2496).
// Retained here because older published CLIs still ship it and the public
// library tolerates it on PRs, normalizing it to the directory post-merge.
const PatchesIndexFilename = ".printing-press-patches.json"

// PatchesDirName is the per-patch customizations directory written for every
// fresh print. Each <id>.json is one self-contained patch object; a .gitkeep
// keeps the directory present at zero patches. Two PRs adding different patches
// write different files, so they never conflict. Shape is documented in
// internal/generator/templates/agents.md.tmpl and the public library AGENTS.md.
const PatchesDirName = ".printing-press-patches"

// PatchesGitKeepName is the placeholder that keeps an empty PatchesDirName
// tracked by git (which does not track empty directories).
const PatchesGitKeepName = ".gitkeep"

// CurrentPatchesIndexSchemaVersion is the schema version stamped into per-patch
// files authored against the directory layout. Matches the shape documented in
// internal/generator/templates/agents.md.tmpl.
const CurrentPatchesIndexSchemaVersion = 2

// PatchesIndex is the legacy single-array customizations shape, retained for
// reading CLIs that still ship PatchesIndexFilename. Fresh prints now emit the
// PatchesDirName directory instead (see EnsurePatchesDir). The Patches field is
// []json.RawMessage so an empty legacy index serializes to "patches: []" rather
// than "patches: null".
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
	// Creator is the permanent original author (handle + display name),
	// preserved across regens regardless of who runs the generator. Source
	// of truth for every attribution surface.
	Creator *spec.Person `json:"creator,omitempty"`
	// Contributors accrue as others improve the CLI (reprinter first).
	// Preserved on plain regen/sync; appended only by deliberate
	// contribution flows (publish/amend/reprint by a non-creator).
	Contributors []spec.Person `json:"contributors,omitempty"`
	// Owner/Printer/PrinterName are legacy attribution fields, dual-written
	// from Creator during the transition window so older skills/tooling that
	// read them keep working. A future major release removes them.
	Owner string `json:"owner,omitempty"` // legacy: derived from Creator.Handle
	// Printer is the original printer's GitHub handle, preserved across regens.
	Printer string `json:"printer,omitempty"` // legacy: derived from Creator.Handle
	// PrinterName is the optional display name rendered beside the printer handle.
	PrinterName        string            `json:"printer_name,omitempty"` // legacy: derived from Creator.Name
	SpecURL            string            `json:"spec_url,omitempty"`
	SpecPath           string            `json:"spec_path,omitempty"`
	SpecFormat         string            `json:"spec_format,omitempty"`
	SpecKind           string            `json:"spec_kind,omitempty"`
	SpecSource         string            `json:"spec_source,omitempty"`
	SpecChecksum       string            `json:"spec_checksum,omitempty"`
	RunID              string            `json:"run_id,omitempty"`
	CatalogEntry       string            `json:"catalog_entry,omitempty"`
	Category           string            `json:"category,omitempty"`
	Regions            []string          `json:"regions,omitempty"`
	APILanguage        string            `json:"api_language,omitempty"`
	Description        string            `json:"description,omitempty"`
	MCPBinary          string            `json:"mcp_binary,omitempty"`
	MCPToolCount       int               `json:"mcp_tool_count,omitempty"`
	MCPPublicToolCount int               `json:"mcp_public_tool_count,omitempty"`
	MCPReady           string            `json:"mcp_ready,omitempty"`
	APIVersion         string            `json:"api_version,omitempty"` // from the spec's info.version — provenance only, not the CLI version
	AuthType           string            `json:"auth_type,omitempty"`
	AuthPreference     string            `json:"auth_preference,omitempty"`
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
	AuthOptional               bool                        `json:"auth_optional,omitempty"`
	ReviewedSecretSuppressions []ReviewedSecretSuppression `json:"reviewed_secret_suppressions,omitempty"`
	NovelFeatures              []NovelFeatureManifest      `json:"novel_features,omitempty"`
}

type ReviewedSecretSuppression struct {
	Path        string `json:"path"`
	Line        int    `json:"line"`
	Kind        string `json:"kind"`
	Fingerprint string `json:"fingerprint"`
	Reason      string `json:"reason"`
}

// CLIReleaseManifest is the skeleton shape consumed by the public library's
// release-ledger workflow. Version fields are intentionally blank at print
// time: the library owns final release accounting to avoid PR-time conflicts.
type CLIReleaseManifest struct {
	SchemaVersion        int      `json:"schema_version"`
	Slug                 string   `json:"slug"`
	CLIName              string   `json:"cli_name,omitempty"`
	Version              string   `json:"version"`
	ReleasedAt           string   `json:"released_at"`
	SourceCommit         string   `json:"source_commit"`
	PrintingPressVersion string   `json:"printing_press_version,omitempty"`
	RunID                string   `json:"run_id,omitempty"`
	Changes              []string `json:"changes,omitempty"`
}

// IsLocalDatastore reports whether the manifest describes a local-datastore
// CLI rather than an HTTP API wrapper. These CLIs read operator-local stores
// such as SQLite databases and should not be scored or dogfooded through
// HTTP-only assumptions.
func (m CLIManifest) IsLocalDatastore() bool {
	format := strings.ToLower(strings.TrimSpace(m.SpecFormat))
	source := strings.ToLower(strings.TrimSpace(m.SpecSource))
	switch format {
	case "sqlite", "local-sqlite":
		return true
	}
	return strings.Contains(source, "local") && strings.Contains(source, "sqlite")
}

// IsSyntheticSpec reports whether the manifest came from a spec marked
// `kind: synthetic`, which relaxes gates that assume HTTP API reachability.
func (m CLIManifest) IsSyntheticSpec() bool {
	return strings.EqualFold(strings.TrimSpace(m.SpecKind), spec.KindSynthetic)
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
// dir/.printing-press.json. It preserves existing release-ledger files because
// the public library workflow owns updating them after merge.
func WriteCLIManifest(dir string, m CLIManifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling CLI manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, CLIManifestFilename), data, 0o644); err != nil {
		return fmt.Errorf("writing CLI manifest: %w", err)
	}
	if err := WriteReleaseLedgerSkeleton(dir, m); err != nil {
		return err
	}
	return nil
}

func WriteReviewedSecretSuppressions(dir string, suppressions []ReviewedSecretSuppression) error {
	path := filepath.Join(dir, CLIManifestFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading CLI manifest: %w", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parsing CLI manifest: %w", err)
	}
	if len(suppressions) == 0 {
		delete(raw, "reviewed_secret_suppressions")
	} else {
		encoded, err := json.Marshal(suppressions)
		if err != nil {
			return fmt.Errorf("encoding reviewed secret suppressions: %w", err)
		}
		raw["reviewed_secret_suppressions"] = encoded
	}
	out, err := marshalCLIManifestObject(raw)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, out, 0o644); err != nil {
		return fmt.Errorf("writing CLI manifest: %w", err)
	}
	return nil
}

// WriteReleaseLedgerSkeleton preserves public-library release ledger files.
// Fresh prints intentionally do not create .printing-press-release.json:
// the public library registry validator treats a present release object as
// populated release metadata and rejects blank version/stamp fields.
func WriteReleaseLedgerSkeleton(dir string, m CLIManifest) error {
	releasePath := filepath.Join(dir, CLIReleaseManifestFilename)
	if _, err := os.Stat(releasePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking CLI release manifest skeleton: %w", err)
	}

	changelogPath := filepath.Join(dir, CLIChangelogFilename)
	if _, err := os.Stat(changelogPath); errors.Is(err, os.ErrNotExist) {
		data := []byte("# Changelog\n\nThis file is maintained by printing-press-library release automation. Do not hand-edit release sections in normal PRs.\n\n")
		if err := os.WriteFile(changelogPath, data, 0o644); err != nil {
			return fmt.Errorf("writing CLI changelog skeleton: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("checking CLI changelog skeleton: %w", err)
	}
	return nil
}

// AppendContributor adds p to the manifest's contributors[] in dir, returning
// whether a write happened. It is the deliberate-contribution counterpart to
// the resolver's preserve-on-regen behavior: only the publish/amend/reprint
// flows call it, never a plain regen.
//
// The append is idempotent and skips self-attribution: p is dropped when it is
// the creator or already a contributor (matched case-insensitively by handle).
// With front=true the contributor is prepended (used by the reprint flow so
// the reprinter is listed first); otherwise appended. All other manifest fields
// — including unknown/future keys — are preserved verbatim via the raw map.
func AppendContributor(dir string, p spec.Person, front bool) (bool, error) {
	p = p.Clean()
	if p.IsZero() {
		return false, nil
	}
	path := filepath.Join(dir, CLIManifestFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("reading CLI manifest: %w", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return false, fmt.Errorf("parsing CLI manifest: %w", err)
	}

	var creator spec.Person
	if rc, ok := raw["creator"]; ok {
		if err := json.Unmarshal(rc, &creator); err != nil {
			return false, fmt.Errorf("parsing creator: %w", err)
		}
	}
	if spec.SamePerson(p, creator) {
		return false, nil
	}

	var contributors []spec.Person
	if rc, ok := raw["contributors"]; ok {
		if err := json.Unmarshal(rc, &contributors); err != nil {
			return false, fmt.Errorf("parsing contributors: %w", err)
		}
	}
	for _, c := range contributors {
		if spec.SamePerson(p, c) {
			return false, nil
		}
	}

	if front {
		contributors = append([]spec.Person{p}, contributors...)
	} else {
		contributors = append(contributors, p)
	}
	enc, err := json.Marshal(contributors)
	if err != nil {
		return false, fmt.Errorf("encoding contributors: %w", err)
	}
	raw["contributors"] = enc

	out, err := marshalCLIManifestObject(raw)
	if err != nil {
		return false, err
	}
	if err := writeFileAtomic(path, out, 0o644); err != nil {
		return false, fmt.Errorf("writing CLI manifest: %w", err)
	}
	return true, nil
}

// writeFileAtomic writes data to a sibling temp file and renames it over path,
// so an interrupted write can't truncate the manifest (the provenance source of
// truth) and leave it unparseable for the next regen.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// EnsurePatchesDir creates the empty per-patch customizations directory
// (PatchesDirName containing a .gitkeep) into the generated CLI directory. The
// library's Verify CI requires fresh-print publishes to ship a patches index;
// emitting the empty directory here removes the friction for every future
// publish without affecting CLIs that already recorded customizations.
//
// It is a no-op when the CLI already has either the directory or the legacy
// PatchesIndexFilename, so agent-applied customizations survive regen (parallel
// to how resolveOwnerForExisting preserves printer/owner metadata across
// regenerate runs). A legacy file is left in place; the public library converts
// it to the directory post-merge via its normalize-patches workflow.
func EnsurePatchesDir(dir string) error {
	patchesDir := filepath.Join(dir, PatchesDirName)
	if _, err := os.Stat(patchesDir); err == nil {
		return nil // directory already present — preserve agent-authored content
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking patches dir: %w", err)
	}
	if _, err := os.Stat(filepath.Join(dir, PatchesIndexFilename)); err == nil {
		return nil // legacy single-array file present — preserve; library normalizes it post-merge
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking legacy patches index: %w", err)
	}
	// os.Mkdir (not MkdirAll) so a nonexistent parent CLI dir surfaces as an
	// error rather than being silently created.
	if err := os.Mkdir(patchesDir, 0o755); err != nil {
		return fmt.Errorf("creating patches dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(patchesDir, PatchesGitKeepName), nil, 0o644); err != nil {
		return fmt.Errorf("writing patches .gitkeep: %w", err)
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
		"creator",
		"contributors",
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
		"auth_preference",
		"auth_env_vars",
		"auth_env_var_specs",
		"endpoint_template_vars",
		"endpoint_template_env_overrides",
		"auth_key_url",
		"auth_title",
		"auth_description",
		"auth_optional",
		"reviewed_secret_suppressions",
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
	m.SpecKind = parsed.Kind
	total, public := parsed.CountMCPTools()
	mcpName := m.APIName
	if mcpName == "" {
		mcpName = parsed.Name
	}
	m.MCPBinary = naming.MCP(mcpName)
	m.MCPToolCount = total
	m.MCPPublicToolCount = public
	m.MCPReady = computeMCPReady(parsed.Auth.Type)
	m.AuthType = parsed.Auth.Type
	m.AuthPreference = strings.TrimSpace(parsed.Auth.Scheme)
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
	if len(parsed.Regions) > 0 {
		m.Regions = append([]string(nil), parsed.Regions...)
	}
	if parsed.APILanguage != "" {
		m.APILanguage = parsed.APILanguage
	}
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
	APIName        string
	SpecSrcs       []string // --spec args (URLs or file paths)
	SpecURL        string   // --spec-url: explicit provenance URL (when --spec is a local downloaded file)
	DocsURL        string   // --docs URL, if used
	OutputDir      string
	Description    string                 // best generated user-facing catalog description
	DisplayName    string                 // best generated user-facing catalog display name
	Creator        spec.Person            // resolved creator (manifest preserve > legacy fields > git config)
	Contributors   []spec.Person          // resolved contributors, preserved from the existing manifest
	Owner          string                 // legacy, derived from Creator.Handle (dual-write)
	Printer        string                 // legacy, derived from Creator.Handle (dual-write)
	PrinterName    string                 // legacy, derived from Creator.Name (dual-write)
	RunID          string                 // from --research-dir/state.json when available, legacy basename fallback otherwise
	Spec           *spec.APISpec          // parsed spec for MCP metadata (nil if unavailable)
	AuthPreference string                 // resolved OpenAPI securityScheme preference selected for this generate
	NovelFeatures  []NovelFeatureManifest // transcendence features from research (nil if unavailable)
}

// runIDPattern matches legacy and skill-allocated pipeline run_id basenames.
// State files are the source of truth for current runs; this is only a legacy
// fallback for older run directories that predate state-backed generate.
var runIDPattern = regexp.MustCompile(`^\d{8}-\d{6}(?:-[A-Za-z0-9._-]+)?$`)

// runIDTimeFormat is the legacy timestamp-only layout used when generate has
// no state-backed run_id. Kept as a const so fallback formatting stays stable.
const runIDTimeFormat = "20060102-150405"

// DeriveRunIDFromResearchDir extracts a canonical run_id from a research-dir
// path, or returns "" when no valid run_id can be derived. Prefer
// ResolveRunIDFromResearchDir for current generate flows so state.json wins.
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

type generateResearchState struct {
	APIName string `json:"api_name"`
	RunID   string `json:"run_id"`
}

func loadGenerateResearchState(researchDir string) (generateResearchState, bool) {
	if researchDir == "" {
		return generateResearchState{}, false
	}
	statePath := filepath.Join(researchDir, "state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		return generateResearchState{}, false
	}
	var state generateResearchState
	if err := json.Unmarshal(data, &state); err != nil {
		return generateResearchState{}, false
	}
	state.APIName = strings.TrimSpace(state.APIName)
	state.RunID = strings.TrimSpace(state.RunID)
	return state, state.APIName != "" || state.RunID != ""
}

// ResolveRunIDFromResearchDir reads the run_id recorded by Run Initialization
// in `<researchDir>/state.json`, falling back to the path basename only for
// legacy runs that predate state-backed generate.
func ResolveRunIDFromResearchDir(researchDir string) string {
	if state, ok := loadGenerateResearchState(researchDir); ok && state.RunID != "" {
		return state.RunID
	}
	return DeriveRunIDFromResearchDir(researchDir)
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
	state, ok := loadGenerateResearchState(researchDir)
	if !ok {
		return ""
	}
	return state.APIName
}

// WriteManifestForGenerate writes a .printing-press.json manifest into the
// generated CLI directory. This is the generate-command counterpart of
// writeCLIManifestForPublish (which operates on PipelineState).
//
// An empty p.RunID is auto-filled with a fresh timestamp so the emitted
// manifest satisfies publish-validate's required-run_id contract. Phase 5
// dogfood acceptance still needs the original run_id from state.json or the
// legacy research-dir basename, and the root.go --research-dir warning
// informs phase5 callers of that gap.
func WriteManifestForGenerate(p GenerateManifestParams) error {
	now := time.Now().UTC()
	runID := p.RunID
	if runID == "" {
		runID = now.Format(runIDTimeFormat)
	}
	existing, existingRaw, hasExisting := readExistingManifestForGenerate(p.OutputDir)
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
	// Creator is the canonical attribution; Owner/Printer/PrinterName above are
	// the legacy dual-write derived from it. Stored as a pointer so an empty
	// creator omits the key (and lets the same-lineage raw merge preserve a
	// persisted one).
	if !p.Creator.IsZero() {
		creator := p.Creator
		m.Creator = &creator
	}
	m.Contributors = p.Contributors

	// Populate spec_url / spec_path from the first spec source.
	if p.DocsURL != "" {
		m.SpecURL = p.DocsURL
		m.SpecFormat = "docs"
	} else if len(p.SpecSrcs) > 0 {
		src := p.SpecSrcs[0]
		if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
			m.SpecURL = src
		} else {
			m.SpecPath = sanitizeManifestSpecPath(src)
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
		m.Regions = append([]string(nil), entry.Regions...)
		m.APILanguage = entry.APILanguage
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
	if p.Spec != nil && p.Spec.IsLocalSQLiteSource() {
		m.SpecFormat = spec.SourceLocalSQLite
	}

	// Populate MCP metadata from the parsed spec.
	if p.Spec != nil {
		populateMCPMetadata(&m, p.Spec)
	}
	if authPreference := strings.TrimSpace(p.AuthPreference); authPreference != "" {
		m.AuthPreference = authPreference
	}
	if displayName := strings.TrimSpace(p.DisplayName); displayName != "" {
		m.DisplayName = displayName
	}
	if description := strings.TrimSpace(p.Description); description != "" {
		m.Description = description
	}
	preserveExisting := hasExisting && sameGenerateManifestLineage(existing, m)
	// A durable manifest description may be hand-edited after generation.
	// Operators can delete or replace the field when they want changed spec
	// prose to become canonical on a later generate run.
	if preserveExisting && preserveExistingDescription(existing.Description) {
		m.Description = existing.Description
	}
	if len(p.NovelFeatures) > 0 {
		m.NovelFeatures = p.NovelFeatures
	} else if p.NovelFeatures != nil {
		m.NovelFeatures = []NovelFeatureManifest{}
	} else if preserveExisting && len(existing.NovelFeatures) > 0 {
		m.NovelFeatures = existing.NovelFeatures
	}

	if preserveExisting && m.Category == "" && strings.TrimSpace(existing.Category) != "" {
		m.Category = existing.Category
	}
	if preserveExisting && p.RunID == "" && strings.TrimSpace(existing.RunID) != "" {
		m.RunID = existing.RunID
	}
	if preserveExisting {
		if p.Owner == "" && strings.TrimSpace(existing.Owner) != "" {
			m.Owner = existing.Owner
		}
		if p.Printer == "" && strings.TrimSpace(existing.Printer) != "" {
			m.Printer = existing.Printer
		}
		if p.PrinterName == "" && strings.TrimSpace(existing.PrinterName) != "" {
			m.PrinterName = existing.PrinterName
		}
		// Creator is permanent: preserve the persisted one when this run did
		// not carry it. Contributors are preserved unless explicitly cleared
		// (a non-nil empty slice, handled via clearFields below).
		if p.Creator.IsZero() && existing.Creator != nil && !existing.Creator.IsZero() {
			m.Creator = existing.Creator
		}
		if p.Contributors == nil && len(existing.Contributors) > 0 {
			m.Contributors = existing.Contributors
		}
	} else {
		existingRaw = nil
	}

	clearFields := map[string]struct{}{}
	if preserveExisting && p.NovelFeatures != nil && len(p.NovelFeatures) == 0 {
		clearFields["novel_features"] = struct{}{}
	}
	// A non-nil empty contributors slice is the explicit-clear signal: force
	// the key out of the same-lineage raw merge (an omitempty empty slice
	// would otherwise leave the persisted list in place).
	if preserveExisting && p.Contributors != nil && len(p.Contributors) == 0 {
		clearFields["contributors"] = struct{}{}
	}
	if preserveExisting && strings.TrimSpace(m.AuthPreference) == "" {
		clearFields["auth_preference"] = struct{}{}
	}
	if preserveExisting {
		if m.SpecURL != "" && m.SpecPath == "" {
			clearFields["spec_path"] = struct{}{}
		}
		if m.SpecPath != "" && m.SpecURL == "" {
			clearFields["spec_url"] = struct{}{}
		}
	}

	if err := writeCLIManifestForGenerate(p.OutputDir, m, existingRaw, clearFields); err != nil {
		return err
	}
	// Emit the customizations directory alongside .printing-press.json. The
	// library's Verify CI requires every fresh-print publish to ship a patches
	// index; preserve-on-regen keeps agent-applied patch entries from being
	// clobbered by a later generate --force.
	if err := EnsurePatchesDir(p.OutputDir); err != nil {
		return err
	}
	// Emit agentcookie.toml so the user's agentcookie source can ship
	// per-CLI auth tokens to the sink at tier "explicit-manifest". Skips
	// cookie-only CLIs and authors with the manual-override marker.
	if err := WriteAgentcookieManifest(p); err != nil {
		return err
	}
	// Emit MCPB manifest.json next to .printing-press.json. Pass the
	// in-memory struct so we don't re-read the file we just wrote.
	return WriteMCPBManifestFromStruct(p.OutputDir, m)
}

func readExistingManifestForGenerate(dir string) (CLIManifest, map[string]json.RawMessage, bool) {
	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	if err != nil {
		return CLIManifest{}, nil, false
	}
	var m CLIManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return CLIManifest{}, nil, false
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		raw = nil
	}
	return m, raw, true
}

func writeCLIManifestForGenerate(dir string, m CLIManifest, existingRaw map[string]json.RawMessage, clearFields map[string]struct{}) error {
	if len(existingRaw) == 0 {
		return WriteCLIManifest(dir, m)
	}
	generatedFields, err := marshalCLIManifestFields(m)
	if err != nil {
		return err
	}
	merged := maps.Clone(existingRaw)
	for key := range clearFields {
		delete(merged, key)
	}
	maps.Copy(merged, generatedFields)
	data, err := marshalCLIManifestObject(merged)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, CLIManifestFilename), data, 0o644); err != nil {
		return fmt.Errorf("writing CLI manifest: %w", err)
	}
	return nil
}

func sameGenerateManifestLineage(existing, generated CLIManifest) bool {
	if existing.APIName == "" || generated.APIName == "" || existing.APIName != generated.APIName {
		return false
	}
	if existing.SpecChecksum != "" && generated.SpecChecksum != "" {
		return existing.SpecChecksum == generated.SpecChecksum
	}
	if (existing.SpecURL != "" || existing.SpecPath != "") && (generated.SpecURL != "" || generated.SpecPath != "") {
		if existing.SpecURL != "" || generated.SpecURL != "" {
			return existing.SpecURL == generated.SpecURL
		}
		return existing.SpecPath == generated.SpecPath
	}
	return true
}

func preserveExistingDescription(description string) bool {
	description = strings.TrimSpace(description)
	return description != "" && !naming.HasLiteralEllipsisSuffix(description)
}

// sanitizeManifestSpecPath reduces a local spec file path to its basename so the
// published manifest never leaks the printer's filesystem layout. Only http(s)
// URLs pass through unchanged — a file:// URL embeds the same local path we are
// trying to keep out of the published manifest, so it is basenamed too.
func sanitizeManifestSpecPath(specPath string) string {
	if specPath == "" {
		return ""
	}
	if strings.HasPrefix(specPath, "http://") || strings.HasPrefix(specPath, "https://") {
		return specPath
	}
	return filepath.Base(specPath)
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
