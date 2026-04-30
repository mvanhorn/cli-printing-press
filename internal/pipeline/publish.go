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

	"github.com/mvanhorn/cli-printing-press/v3/catalog"
	catalogpkg "github.com/mvanhorn/cli-printing-press/v3/internal/catalog"
	"github.com/mvanhorn/cli-printing-press/v3/internal/graphql"
	"github.com/mvanhorn/cli-printing-press/v3/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v3/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"github.com/mvanhorn/cli-printing-press/v3/internal/version"
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
		SchemaVersion:        1,
		GeneratedAt:          time.Now().UTC(),
		PrintingPressVersion: version.Version,
		APIName:              state.APIName,
		CLIName:              naming.CLI(state.APIName),
		SpecURL:              specURL,
		SpecPath:             specPath,
		RunID:                state.RunID,
	}

	// Carry forward metadata from the generated manifest when publish-time
	// parsing is unavailable or lossy for the original spec format. NovelFeatures
	// is carried forward as a defensive fallback in case the loadResearchForState
	// lookup below misses (e.g., research.json moved or absent); when both are
	// available, research.json wins as the post-dogfood source of truth.
	if existingData, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename)); err == nil {
		var existing CLIManifest
		if json.Unmarshal(existingData, &existing) == nil {
			m.MCPBinary = existing.MCPBinary
			m.MCPToolCount = existing.MCPToolCount
			m.MCPPublicToolCount = existing.MCPPublicToolCount
			m.MCPReady = existing.MCPReady
			m.AuthType = existing.AuthType
			m.AuthEnvVars = existing.AuthEnvVars
			m.NovelFeatures = existing.NovelFeatures
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
			parsed, parseErr = openapi.Parse(data)
		case "graphql":
			parsed, parseErr = graphql.ParseSDLBytes(specFile, data)
		case "internal":
			parsed, parseErr = spec.ParseBytes(data)
		}
		if parseErr == nil {
			populateMCPMetadata(&m, parsed)
		}

		// Generate tools-manifest.json for diagnostic commands
		// (auth-doctor, mcp-audit). Non-blocking: log warning on error
		// but don't fail the publish.
		if parsed != nil {
			if tmErr := WriteToolsManifest(dir, parsed); tmErr != nil {
				fmt.Fprintf(os.Stderr, "warning: could not write tools manifest: %v\n", tmErr)
			}
		}
	}

	// Look up catalog entry by API name; empty string if not found.
	if entry, err := catalogpkg.LookupFS(catalog.FS, state.APIName); err == nil {
		m.CatalogEntry = entry.Name
		m.Category = entry.Category
		m.Description = entry.Description
	}

	// Load novel features from research.json if available, populating the
	// manifest's NovelFeatures field so publish-validate's transcendence
	// check passes without manual patching.
	//
	// Lookup order:
	//   1. loadResearchForState — checks <RunRoot>/research.json (skill-flow
	//      convention) then <state.PipelineDir>/research.json (generate-flow
	//      convention). Covers both write conventions (cal-com retro #334 F2,
	//      and food52 retro #337 F4 once #340 recovers RunID in NewMinimalState).
	//   2. Minimal-state fallback: when loadResearchForState fails AND
	//      state.RunID is empty (NewMinimalState had no registered runstate
	//      to recover RunID from), glob the scoped runstate root for any
	//      run whose research.json names this APIName and pick the most
	//      recent by mtime. Backstop for orphaned plan-driven promotes.
	//
	// Within the loaded ResearchResult, prefer NovelFeaturesBuilt
	// (dogfood-verified subset) over NovelFeatures (planned list) — if
	// dogfood ran, only the actually-shipped features should be advertised.
	// Falling back to the planned list when dogfood didn't run (or wrote
	// an empty NovelFeaturesBuilt) keeps first-publish from failing the
	// transcendence check on a CLI that genuinely shipped novel features.
	research, source := loadResearchForPromote(state)
	if research != nil {
		nfs := pickNovelFeaturesForManifest(research)
		// Override the existing-manifest carry-forward (line 221) with
		// research.json's view: post-dogfood data is the source of truth.
		// If pickNovelFeaturesForManifest returned an empty slice (no
		// Built and no planned), leave the carry-forward in place.
		if len(nfs) > 0 {
			m.NovelFeatures = novelFeaturesToManifest(nfs)
		}
		if len(m.NovelFeatures) > 0 && source != "" && source != state.PipelineDir() && source != RunRoot(state.RunID) {
			// Visibility for non-canonical sources — a one-line stderr
			// note keeps promote silent on the happy path but tells the
			// user when novel_features came from the glob fallback.
			fmt.Fprintf(os.Stderr, "publish: hydrated %d novel_features from %s\n", len(m.NovelFeatures), source)
		}
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
			filepath.Join(RunRoot(state.RunID), "research.json"),
			filepath.Join(state.PipelineDir(), "research.json"),
			state.RunID)
	}

	return WriteCLIManifest(dir, m)
}

// loadResearchForPromote returns the research.json relevant to the
// current promote/publish call, plus the path it was loaded from (used
// for non-canonical-source stderr visibility).
//
// Lookup order:
//   - Delegate to loadResearchForState first (RunRoot then PipelineDir).
//     This is the dominant path; with #340's NewMinimalState recovery,
//     even plan-driven promotes hit this branch.
//   - When loadResearchForState fails AND RunID is empty (the registry
//     recovery had no prior runstate to borrow from), glob the scoped
//     runstate root for any research.json whose APIName matches and
//     pick the most recent by mtime. Backstop for orphaned promotes.
//
// Returns (nil, "") when neither lookup finds a usable research.json.
func loadResearchForPromote(state *PipelineState) (*ResearchResult, string) {
	if r, err := loadResearchForState(state); err == nil {
		// loadResearchForState is the canonical loader; report the
		// path it tried first (run-root) when RunID is set, since the
		// caller's "is this a non-canonical source?" check compares
		// against state.PipelineDir() and RunRoot(state.RunID).
		if state.RunID != "" {
			if _, statErr := os.Stat(filepath.Join(RunRoot(state.RunID), "research.json")); statErr == nil {
				return r, RunRoot(state.RunID)
			}
			return r, state.PipelineDir()
		}
		return r, ""
	}

	if state.RunID != "" {
		// loadResearchForState already covered both canonical paths
		// for a populated state — no further fallback to try.
		return nil, ""
	}

	// Minimal-state fallback: empty RunID and the canonical loader
	// found nothing. Glob the scoped runstate root for research.json
	// files matching this APIName and pick the most recent.
	candidates, err := globResearchCandidates(ScopedRunstateRoot())
	if err != nil || len(candidates) == 0 {
		return nil, ""
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].mtime.After(candidates[j].mtime)
	})
	for _, c := range candidates {
		r, err := LoadResearch(filepath.Dir(c.path))
		if err != nil {
			continue
		}
		if state.APIName != "" && r.APIName != "" && r.APIName != state.APIName {
			continue
		}
		return r, c.path
	}
	return nil, ""
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
