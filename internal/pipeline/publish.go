package pipeline

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/catalog"
	catalogpkg "github.com/mvanhorn/cli-printing-press/v4/internal/catalog"
	"github.com/mvanhorn/cli-printing-press/v4/internal/catalogmeta"
	"github.com/mvanhorn/cli-printing-press/v4/internal/graphql"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/mvanhorn/cli-printing-press/v4/internal/version"
	"gopkg.in/yaml.v3"
)

type RunManifest struct {
	Version               int       `json:"version"`
	APIName               string    `json:"api_name"`
	RunID                 string    `json:"run_id"`
	Scope                 string    `json:"scope"`
	GitRoot               string    `json:"git_root"`
	SpecPath              string    `json:"spec_path,omitempty"`
	SpecURL               string    `json:"spec_url,omitempty"`
	WorkingDir            string    `json:"working_dir"`
	PublishedCLIDir       string    `json:"published_cli_dir,omitempty"`
	ArchivedManuscriptDir string    `json:"archived_manuscript_dir,omitempty"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

func BuildRunManifest(state *PipelineState) RunManifest {
	return RunManifest{
		Version:               1,
		APIName:               state.APIName,
		RunID:                 state.RunID,
		Scope:                 state.Scope,
		GitRoot:               repoRoot(),
		SpecPath:              state.SpecPath,
		SpecURL:               state.SpecURL,
		WorkingDir:            state.EffectiveWorkingDir(),
		PublishedCLIDir:       state.PublishedDir,
		ArchivedManuscriptDir: ArchivedManuscriptDir(state.APIName, state.RunID),
		CreatedAt:             state.StartedAt,
		UpdatedAt:             time.Now(),
	}
}

func WriteRunManifest(state *PipelineState) error {
	manifest := BuildRunManifest(state)
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling run manifest: %w", err)
	}
	if err := os.WriteFile(state.ManifestPath(), data, 0o644); err != nil {
		return fmt.Errorf("writing run manifest: %w", err)
	}
	return nil
}

func WriteArchivedManifest(state *PipelineState) error {
	manifest := BuildRunManifest(state)
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling archived manifest: %w", err)
	}
	if err := os.MkdirAll(ArchivedManuscriptDir(state.APIName, state.RunID), 0o755); err != nil {
		return fmt.Errorf("creating archived manuscript dir: %w", err)
	}
	if err := os.WriteFile(ArchivedManifestPath(state.APIName, state.RunID), data, 0o644); err != nil {
		return fmt.Errorf("writing archived manifest: %w", err)
	}
	return nil
}

func PublishWorkingCLI(state *PipelineState, targetDir string) (string, error) {
	workingDir := state.EffectiveWorkingDir()
	if workingDir == "" {
		return "", fmt.Errorf("working dir is empty")
	}

	finalDir := targetDir
	var err error
	if finalDir == "" {
		finalDir, err = ClaimOutputDir(DefaultOutputDir(state.APIName))
		if err != nil {
			return "", err
		}
	} else {
		finalDir, err = filepath.Abs(finalDir)
		if err != nil {
			return "", fmt.Errorf("resolving publish dir: %w", err)
		}
		if _, err := os.Stat(finalDir); err == nil {
			return "", fmt.Errorf("publish dir already exists: %s", finalDir)
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("checking publish dir: %w", err)
		}
	}

	if err := CopyDir(workingDir, finalDir); err != nil {
		return "", fmt.Errorf("publishing CLI: %w", err)
	}

	state.PublishedDir = finalDir

	if err := writeCLIManifestForPublish(state, finalDir); err != nil {
		return "", err
	}

	// Refresh the MCPB manifest.json for the final published location.
	// Generate already wrote one alongside .printing-press.json; rewriting
	// here picks up any provenance fields the publish step added.
	if err := WriteMCPBManifest(finalDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write MCPB manifest.json: %v\n", err)
	}

	if err := state.Save(); err != nil {
		return "", err
	}
	if err := WriteRunManifest(state); err != nil {
		return "", err
	}
	return finalDir, nil
}

func ArchiveRunArtifacts(state *PipelineState) (string, error) {
	archiveDir := ArchivedManuscriptDir(state.APIName, state.RunID)
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return "", fmt.Errorf("creating archive dir: %w", err)
	}

	type pair struct {
		src string
		dst string
	}

	pairs := []pair{
		{src: state.ResearchDir(), dst: ArchivedResearchDir(state.APIName, state.RunID)},
		{src: state.ProofsDir(), dst: ArchivedProofsDir(state.APIName, state.RunID)},
		{src: state.PipelineDir(), dst: ArchivedPipelineDir(state.APIName, state.RunID)},
		{src: state.DiscoveryDir(), dst: ArchivedDiscoveryDir(state.APIName, state.RunID)},
	}

	for _, item := range pairs {
		info, err := os.Stat(item.src)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", fmt.Errorf("stat %s: %w", item.src, err)
		}
		if !info.IsDir() {
			continue
		}
		if err := CopyDir(item.src, item.dst); err != nil {
			return "", fmt.Errorf("archiving %s: %w", item.src, err)
		}
	}

	if err := WriteArchivedManifest(state); err != nil {
		return "", err
	}
	if err := WriteRunManifest(state); err != nil {
		return "", err
	}
	return archiveDir, nil
}

func writeCLIManifestForPublish(state *PipelineState, dir string) error {
	// Normalize spec_url vs spec_path. The fullrun pipeline sets
	// state.SpecURL to the raw --spec argument (URL or file path)
	// and state.SpecPath = SpecURL for --spec runs. We need to put
	// URLs in spec_url and file paths in spec_path, not both.
	specURL, specPath := state.SpecURL, state.SpecPath
	isURL := strings.HasPrefix(specURL, "http://") || strings.HasPrefix(specURL, "https://")
	if !isURL && specURL != "" {
		// Raw --spec argument was a file path, not a URL.
		specPath = specURL
		specURL = ""
	}
	if isURL {
		// Don't duplicate a URL into spec_path.
		if specPath == specURL {
			specPath = ""
		}
	}

	m := CLIManifest{
		SchemaVersion:        CurrentCLIManifestSchemaVersion,
		GeneratedAt:          time.Now().UTC(),
		PrintingPressVersion: version.Version,
		APIName:              state.APIName,
		CLIName:              naming.CLI(state.APIName),
		SpecURL:              specURL,
		SpecPath:             sanitizeManifestSpecPath(specPath),
		RunID:                state.RunID,
	}
	var existingDescription string

	// Carry forward metadata from the generated manifest when publish-time
	// parsing is unavailable or lossy for the original spec format. NovelFeatures
	// is carried forward as a defensive fallback in case research.json is absent;
	// when both are available, research.json wins as the post-dogfood source of truth.
	if existingData, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename)); err == nil {
		var existing CLIManifest
		if json.Unmarshal(existingData, &existing) == nil {
			if state.RunID == "" && existing.RunID != "" {
				state.RunID = existing.RunID
				m.RunID = existing.RunID
			}
			if existing.DisplayName != "" {
				m.DisplayName = existing.DisplayName
			}
			if existing.Owner != "" {
				m.Owner = existing.Owner
			}
			if existing.Printer != "" {
				m.Printer = existing.Printer
			}
			if existing.PrinterName != "" {
				m.PrinterName = existing.PrinterName
			}
			if existing.CatalogEntry != "" {
				m.CatalogEntry = existing.CatalogEntry
			}
			if existing.Category != "" {
				m.Category = existing.Category
			}
			if preserveExistingDescription(existing.Description) {
				m.Description = existing.Description
				existingDescription = existing.Description
			}
			if existing.APIVersion != "" {
				m.APIVersion = existing.APIVersion
			}
			m.MCPBinary = existing.MCPBinary
			m.MCPToolCount = existing.MCPToolCount
			m.MCPPublicToolCount = existing.MCPPublicToolCount
			m.MCPReady = existing.MCPReady
			m.AuthType = existing.AuthType
			m.AuthEnvVars = existing.AuthEnvVars
			m.AuthEnvVarSpecs = existing.AuthEnvVarSpecs
			m.EndpointTemplateVars = existing.EndpointTemplateVars
			m.EndpointTemplateEnvOverrides = existing.EndpointTemplateEnvOverrides
			m.EndpointTemplateVarDefaults = existing.EndpointTemplateVarDefaults
			m.AuthKeyURL = existing.AuthKeyURL
			m.AuthTitle = existing.AuthTitle
			m.AuthDescription = existing.AuthDescription
			m.AuthOptional = existing.AuthOptional
			m.NovelFeatures = existing.NovelFeatures
		}
	}

	// Catalog metadata must be present before parsing refreshes display_name:
	// explicit spec display_name wins, but OpenAPI info.title-derived fallback
	// should not clobber curated catalog display_name.
	if entry, err := catalogpkg.LookupFS(catalog.FS, state.APIName); err == nil {
		m.CatalogEntry = entry.Name
		m.Category = entry.Category
		if m.Description == "" {
			m.Description = entry.Description
		}
		if entry.DisplayName != "" {
			m.DisplayName = entry.DisplayName
		}
	}

	// Detect spec format and compute checksum from the spec file archived
	// alongside the CLI. generate writes spec.json for JSON inputs and
	// spec.yaml for YAML inputs; --docs / --plan runs leave no archive and
	// these fields stay empty.
	if specFile, data, err := findArchivedSpec(state.EffectiveWorkingDir()); err == nil && specFile != "" {
		m.SpecFormat = detectSpecFormat(data)
		if checksum, err := specChecksum(specFile); err == nil {
			m.SpecChecksum = checksum
		}

		// Populate MCP metadata from the source spec when possible.
		// If parsing fails, keep any carried-forward values from the generated
		// manifest so non-OpenAPI CLIs do not lose MCP metadata at publish time.
		var (
			parsed   *spec.APISpec
			parseErr error
		)
		switch m.SpecFormat {
		case "openapi3":
			parsed, parseErr = openapi.ParseWithPath(data, specFile)
		case "graphql":
			parsed, parseErr = graphql.ParseSDLBytes(specFile, data)
		case "internal":
			parsed, parseErr = spec.ParseBytes(data)
		}
		if parseErr == nil {
			applyPublishCatalogMetadata(parsed, state.APIName)
			populateMCPMetadata(&m, parsed)
			if m.Description == "" {
				m.Description = naming.CompactDescription(parsed.Description)
			}
			if preserveExistingDescription(existingDescription) {
				m.Description = existingDescription
			}
		}
		if m.Description == "" {
			m.Description = archivedSpecDescription(data)
		}

		// Fall back to spec.Category for synthetic CLIs not in the embedded
		// catalog (mirrors the same fallback in WriteManifestForGenerate).
		// The catalog lookup earlier in this function only fires for
		// catalog-listed APIs; synthetic CLIs would otherwise lose the
		// spec's category at publish time and break verify-skill's
		// canonical-sections check.
		if m.Category == "" && parsed != nil && parsed.Category != "" {
			m.Category = parsed.Category
		}

		// Generate tools-manifest.json for diagnostic commands
		// (auth-doctor, mcp-audit). Non-blocking: log warning on error
		// but don't fail the publish.
		if parsed != nil {
			if tmErr := WriteToolsManifestWithDescription(dir, parsed, m.Description); tmErr != nil {
				fmt.Fprintf(os.Stderr, "warning: could not write tools manifest: %v\n", tmErr)
			}
		}
	}

	// Load novel features from research.json if available, populating the
	// manifest's NovelFeatures field so publish-validate's transcendence
	// check passes without manual patching.
	//
	// Lookup order:
	//   1. Check <RunRoot>/research.json (skill-flow convention), then
	//      <state.PipelineDir>/research.json (generate-flow convention).
	//   2. With a RunID, glob matching run IDs across scopes as a recovery
	//      path for recovered/minimal state.
	//   3. Without a RunID, glob the scoped runstate root for the most recent
	//      research.json whose APIName matches this API.
	//
	// Within the loaded ResearchResult, prefer NovelFeaturesBuilt
	// (dogfood-verified subset) over NovelFeatures (planned list) — if
	// dogfood ran, only the actually-shipped features should be advertised.
	// Falling back to the planned list when dogfood didn't run (or wrote
	// an empty NovelFeaturesBuilt) keeps first-publish from failing the
	// transcendence check on a CLI that genuinely shipped novel features.
	research, source, researchErr := loadResearchForPromote(state)
	if research != nil {
		nfs := pickNovelFeaturesForManifest(research)
		// Override the existing-manifest carry-forward (line 221) with
		// research.json's view: post-dogfood data is the source of truth.
		// If pickNovelFeaturesForManifest returned an empty slice (no
		// Built and no planned), leave the carry-forward in place.
		if len(nfs) > 0 {
			m.NovelFeatures = novelFeaturesToManifest(nfs)
		}
		if len(m.NovelFeatures) > 0 && source != "" && source != state.PipelineDir() && source != state.RunRoot() {
			// Visibility for non-canonical sources — a one-line stderr
			// note keeps promote silent on the happy path but tells the
			// user when novel_features came from the glob fallback.
			fmt.Fprintf(os.Stderr, "publish: hydrated %d novel_features from %s\n", len(m.NovelFeatures), source)
		}
	} else if researchErr != nil {
		fmt.Fprintf(os.Stderr,
			"debug: %v; skipping novel_features enrichment (state.RunID=%q)\n",
			researchErr,
			state.RunID)
	} else {
		// No research.json found by any lookup path. Emit a debug
		// breadcrumb naming the canonical paths so the silent dropout
		// from earlier versions stays observable. Promote still
		// succeeds with whatever was carried forward from the existing
		// manifest; publish validate will surface the
		// transcendence-check failure separately if relevant.
		fmt.Fprintf(os.Stderr,
			"debug: research.json not found at %s or %s; skipping novel_features enrichment "+
				"(state.RunID=%q)\n",
			filepath.Join(state.RunRoot(), "research.json"),
			filepath.Join(state.PipelineDir(), "research.json"),
			state.RunID)
	}

	return WriteCLIManifest(dir, m)
}

func archivedSpecDescription(data []byte) string {
	var probe struct {
		Description string `yaml:"description" json:"description"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return ""
	}
	return naming.CompactDescription(probe.Description)
}

func applyPublishCatalogMetadata(parsed *spec.APISpec, apiName string) {
	if parsed == nil || apiName == "" {
		return
	}
	priorName := parsed.Name
	if priorName != "" && priorName != apiName {
		catalogmeta.RebaseAuthEnvPrefix(&parsed.Auth, priorName, apiName)
	}
	parsed.Name = apiName

	entry, err := catalogpkg.LookupFS(catalog.FS, apiName)
	if err != nil {
		return
	}
	catalogmeta.ApplyRuntimeMetadata(parsed, entry)
}

// loadResearchForPromote returns the research.json relevant to the
// current promote/publish call, plus the path it was loaded from (used
// for non-canonical-source stderr visibility).
//
// Lookup order:
//   - Check the canonical run-root path, then the pipeline path.
//   - When RunID is set, glob matching run IDs across scopes.
//   - When RunID is empty, glob the scoped runstate root for any
//     research.json whose APIName matches and pick the most recent by mtime.
//
// Returns (nil, "", nil) when neither lookup finds a usable research.json.
func loadResearchForPromote(state *PipelineState) (*ResearchResult, string, error) {
	canonicalCandidates := []struct {
		path   string
		source string
	}{
		{path: filepath.Join(state.RunRoot(), "research.json"), source: state.RunRoot()},
		{path: filepath.Join(state.PipelineDir(), "research.json"), source: state.PipelineDir()},
	}
	for _, candidate := range canonicalCandidates {
		r, err := loadResearchPath(candidate.path)
		if err != nil {
			return nil, "", err
		}
		if r != nil {
			if state.RunID == "" {
				return r, "", nil
			}
			return r, candidate.source, nil
		}
	}

	if state.RunID != "" {
		return loadMatchingResearch(globResearchCandidatesForRunID(state.RunID), state.APIName)
	}

	// Minimal-state fallback: empty RunID and the canonical loader
	// found nothing. Glob the scoped runstate root for research.json
	// files matching this APIName and pick the most recent.
	candidates, err := globResearchCandidates(ScopedRunstateRoot())
	if err != nil || len(candidates) == 0 {
		return nil, "", nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].mtime.After(candidates[j].mtime)
	})
	return loadMatchingResearch(candidates, state.APIName)
}

func loadResearchPath(path string) (*ResearchResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("research.json at %s read failed: %w", path, err)
	}
	r, err := decodeResearch(data)
	if err != nil {
		return nil, fmt.Errorf("research.json at %s failed to parse: %w", path, err)
	}
	return r, nil
}

func loadMatchingResearch(candidates []researchCandidate, apiName string) (*ResearchResult, string, error) {
	var loadErr error
	for _, c := range candidates {
		r, err := loadResearchPath(c.path)
		if err != nil {
			if loadErr == nil {
				loadErr = err
			}
			continue
		}
		if r == nil {
			continue
		}
		if apiName != "" && r.APIName != "" && r.APIName != apiName {
			continue
		}
		return r, c.path, nil
	}
	if loadErr != nil {
		return nil, "", loadErr
	}
	return nil, "", nil
}

func globResearchCandidatesForRunID(runID string) []researchCandidate {
	if runID == "" {
		return nil
	}
	var out []researchCandidate
	seen := make(map[string]bool)
	add := func(path string) {
		if seen[path] {
			return
		}
		seen[path] = true
		info, err := os.Stat(path)
		if err != nil {
			return
		}
		out = append(out, researchCandidate{path: path, mtime: info.ModTime()})
	}

	add(filepath.Join(RunRoot(runID), "research.json"))
	add(filepath.Join(RunRoot(runID), "pipeline", "research.json"))

	scopeEntries, err := os.ReadDir(RunstateRoot())
	if err != nil {
		return out
	}
	for _, entry := range scopeEntries {
		if !entry.IsDir() {
			continue
		}
		runRoot := runRootForScope(entry.Name(), runID)
		add(filepath.Join(runRoot, "research.json"))
		add(filepath.Join(runRoot, "pipeline", "research.json"))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].mtime.After(out[j].mtime)
	})
	return out
}

// researchCandidate is a path + mtime for sorting glob results.
type researchCandidate struct {
	path  string
	mtime time.Time
}

// globResearchCandidates walks the scoped runstate root for research.json
// files. Looks under both `<root>/runs/*/research.json` (run-root form,
// what the skill flow writes) and `<root>/runs/*/pipeline/research.json`
// (canonical generate-pipeline form). Errors during walk are non-fatal
// (returns whatever it found so far).
func globResearchCandidates(scopedRoot string) ([]researchCandidate, error) {
	runsDir := filepath.Join(scopedRoot, "runs")
	entries, err := os.ReadDir(runsDir)
	if err != nil {
		// Missing runs directory is normal for a brand-new workspace —
		// not an error worth surfacing.
		return nil, nil
	}
	var out []researchCandidate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		runDir := filepath.Join(runsDir, entry.Name())
		// Try run-root research.json (skill-flow shape).
		for _, candidate := range []string{
			filepath.Join(runDir, "research.json"),
			filepath.Join(runDir, "pipeline", "research.json"),
		} {
			info, err := os.Stat(candidate)
			if err != nil {
				continue
			}
			out = append(out, researchCandidate{path: candidate, mtime: info.ModTime()})
		}
	}
	return out, nil
}

// pickNovelFeaturesForManifest selects the right novel-features list to
// promote into the manifest. Prefers NovelFeaturesBuilt (dogfood-verified)
// when non-nil and non-empty; falls back to NovelFeatures (planned) so
// CLIs whose dogfood didn't run still get a populated manifest.
func pickNovelFeaturesForManifest(research *ResearchResult) []NovelFeature {
	if research == nil {
		return nil
	}
	if research.NovelFeaturesBuilt != nil && len(*research.NovelFeaturesBuilt) > 0 {
		return *research.NovelFeaturesBuilt
	}
	return research.NovelFeatures
}

func CopyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}

	if err := os.MkdirAll(dst, info.Mode()); err != nil {
		return err
	}

	srcRoot, err := filepath.Abs(src)
	if err != nil {
		return fmt.Errorf("resolving source dir: %w", err)
	}

	// WalkDir (unlike Walk) does not follow directory symlinks, so the
	// callback sees them as symlink entries and we can validate them
	// without descending into potentially huge or circular targets.
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == src {
			return nil
		}

		// Drop git plumbing wherever it appears in the tree. A stray .git
		// from `git init` in a working CLI dir, or a nested submodule, would
		// otherwise be carried into the library and re-staged downstream as
		// a submodule pointer when `git add` runs in the publish repo.
		// .gitignore / .gitattributes are legitimate CLI content and stay.
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == ".gitmodules" {
			return nil
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		// d.Type() returns mode bits from Lstat, so symlinks (including
		// directory symlinks) are detected before any descent.
		if d.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			ok, err := symlinkTargetWithinRoot(srcRoot, path, link)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("symlink %s points outside source tree", path)
			}
			return os.Symlink(link, target)
		}

		if d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(target, info.Mode())
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func symlinkTargetWithinRoot(root, path, link string) (bool, error) {
	resolved := link
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(filepath.Dir(path), resolved)
	}

	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return false, fmt.Errorf("resolving symlink target for %s: %w", path, err)
	}

	rel, err := filepath.Rel(root, absResolved)
	if err != nil {
		return false, fmt.Errorf("checking symlink target for %s: %w", path, err)
	}

	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))), nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
