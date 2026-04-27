package pipeline

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v2/catalog"
	catalogpkg "github.com/mvanhorn/cli-printing-press/v2/internal/catalog"
	"github.com/mvanhorn/cli-printing-press/v2/internal/graphql"
	"github.com/mvanhorn/cli-printing-press/v2/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v2/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v2/internal/spec"
	"github.com/mvanhorn/cli-printing-press/v2/internal/version"

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

	// Generate smithery.yaml for MCP marketplace listing if applicable.
	if err := writeSmitheryYAML(finalDir); err != nil {
		// Non-blocking: log warning but don't fail the publish.
		fmt.Fprintf(os.Stderr, "warning: could not write smithery.yaml: %v\n", err)
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
	// parsing is unavailable or lossy for the original spec format.
	if existingData, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename)); err == nil {
		var existing CLIManifest
		if json.Unmarshal(existingData, &existing) == nil {
			m.MCPBinary = existing.MCPBinary
			m.MCPToolCount = existing.MCPToolCount
			m.MCPPublicToolCount = existing.MCPPublicToolCount
			m.MCPReady = existing.MCPReady
			m.AuthType = existing.AuthType
			m.AuthEnvVars = existing.AuthEnvVars
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

		// Generate tools-manifest.json for the mega MCP server.
		// Non-blocking: log warning on error but don't fail the publish.
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

	// Load novel features from research.json if available. When research.json
	// can't be located (typically because state.RunID is empty in a fallback
	// scenario, or the run's pipeline dir was GCed), emit a debug breadcrumb
	// to stderr so the silent dropout from earlier versions is no longer
	// silent. The breadcrumb names the path that was attempted; the caller
	// (lock promote, publish) shows it but does not treat it as an error.
	pipelineDir := state.PipelineDir()
	if research, err := LoadResearch(pipelineDir); err == nil && research.NovelFeaturesBuilt != nil {
		for _, nf := range *research.NovelFeaturesBuilt {
			m.NovelFeatures = append(m.NovelFeatures, NovelFeatureManifest{
				Name:        nf.Name,
				Command:     nf.Command,
				Description: nf.Description,
			})
		}
	} else {
		fmt.Fprintf(os.Stderr,
			"debug: research.json not found at %s; skipping novel_features enrichment "+
				"(state.RunID=%q)\n",
			filepath.Join(pipelineDir, "research.json"), state.RunID)
	}

	return WriteCLIManifest(dir, m)
}

// smitheryConfig is the marketplace metadata schema for Smithery.
type smitheryConfig struct {
	Name         string                    `yaml:"name"`
	Description  string                    `yaml:"description"`
	StartCommand smitheryStartCommand      `yaml:"startCommand"`
	Env          map[string]smitheryEnvVar `yaml:"env,omitempty"`
}

type smitheryStartCommand struct {
	Command string `yaml:"command"`
}

type smitheryEnvVar struct {
	Description string `yaml:"description"`
	Required    bool   `yaml:"required"`
}

// writeSmitheryYAML generates a smithery.yaml marketplace metadata file
// alongside the CLI manifest. Reads .printing-press.json from dir to get
// MCP metadata. Skips writing if MCPReady is "cli-only" or if no MCP
// metadata is present.
func writeSmitheryYAML(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	if err != nil {
		return nil // no manifest, nothing to do
	}
	var m CLIManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("parsing manifest for smithery: %w", err)
	}
	if m.MCPBinary == "" || m.MCPReady == "cli-only" {
		return nil // no MCP or cli-only — skip
	}

	desc := m.Description
	if desc == "" {
		desc = m.APIName + " API"
	}

	cfg := smitheryConfig{
		Name:        m.MCPBinary,
		Description: desc,
		StartCommand: smitheryStartCommand{
			Command: "go run ./cmd/" + m.MCPBinary,
		},
	}

	if len(m.AuthEnvVars) > 0 {
		cfg.Env = make(map[string]smitheryEnvVar)
		isCookieAuth := m.AuthType == "cookie" || m.AuthType == "composed"
		for _, envVar := range m.AuthEnvVars {
			if isCookieAuth {
				cfg.Env[envVar] = smitheryEnvVar{
					Description: "Required for authenticated endpoints only — some tools work without credentials",
					Required:    false,
				}
			} else {
				cfg.Env[envVar] = smitheryEnvVar{
					Description: m.APIName + " API credential",
					Required:    true,
				}
			}
		}
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling smithery.yaml: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "smithery.yaml"), out, 0o644)
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
