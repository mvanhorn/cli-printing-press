package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/catalog"
	catalogpkg "github.com/mvanhorn/cli-printing-press/internal/catalog"
	"github.com/mvanhorn/cli-printing-press/internal/naming"
	"github.com/mvanhorn/cli-printing-press/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/internal/spec"
	"github.com/mvanhorn/cli-printing-press/internal/version"
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
	// CLIName is the executable/binary name (for example "espn-pp-cli").
	// It does not track the slug-keyed library directory.
	CLIName            string                 `json:"cli_name"`
	SpecURL            string                 `json:"spec_url,omitempty"`
	SpecPath           string                 `json:"spec_path,omitempty"`
	SpecFormat         string                 `json:"spec_format,omitempty"`
	SpecChecksum       string                 `json:"spec_checksum,omitempty"`
	RunID              string                 `json:"run_id,omitempty"`
	CatalogEntry       string                 `json:"catalog_entry,omitempty"`
	Category           string                 `json:"category,omitempty"`
	Description        string                 `json:"description,omitempty"`
	MCPBinary          string                 `json:"mcp_binary,omitempty"`
	MCPToolCount       int                    `json:"mcp_tool_count,omitempty"`
	MCPPublicToolCount int                    `json:"mcp_public_tool_count,omitempty"`
	MCPReady           string                 `json:"mcp_ready,omitempty"`
	AuthType           string                 `json:"auth_type,omitempty"`
	AuthEnvVars        []string               `json:"auth_env_vars,omitempty"`
	NovelFeatures      []NovelFeatureManifest `json:"novel_features,omitempty"`
}

// NovelFeatureManifest is a compact representation of a transcendence feature
// for the CLI manifest and registry. Stripped of Rationale (which stays in
// research.json and the README).
type NovelFeatureManifest struct {
	Name        string `json:"name"`
	Command     string `json:"command"`
	Description string `json:"description"`
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

// computeMCPReady determines the MCP readiness level based on the auth type
// and the public/total tool split.
func computeMCPReady(authType string, publicTools int) string {
	switch authType {
	case "none", "api_key", "bearer_token":
		return "full"
	case "cookie", "composed":
		if publicTools > 0 {
			return "partial"
		}
		return "cli-only"
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
	m.MCPReady = computeMCPReady(parsed.Auth.Type, public)
	m.AuthType = parsed.Auth.Type
	m.AuthEnvVars = parsed.Auth.EnvVars
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
	Spec          *spec.APISpec          // parsed spec for MCP metadata (nil if unavailable)
	NovelFeatures []NovelFeatureManifest // transcendence features from research (nil if unavailable)
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
		for _, name := range []string{"spec.json", "spec.yaml", "spec.yml"} {
			specFile := filepath.Join(p.OutputDir, name)
			data, err := os.ReadFile(specFile)
			if err != nil {
				continue
			}
			if m.SpecFormat == "" {
				m.SpecFormat = detectSpecFormat(data)
			}
			if m.SpecChecksum == "" {
				cs, err := specChecksum(specFile)
				if err == nil {
					m.SpecChecksum = cs
				}
			}
			break
		}
	}

	// Look up catalog entry for category/description enrichment.
	if entry, err := catalogpkg.LookupFS(catalog.FS, p.APIName); err == nil {
		m.CatalogEntry = entry.Name
		m.Category = entry.Category
		m.Description = entry.Description
	}

	// Populate MCP metadata from the parsed spec.
	if p.Spec != nil {
		populateMCPMetadata(&m, p.Spec)
	}
	if len(p.NovelFeatures) > 0 {
		m.NovelFeatures = p.NovelFeatures
	}

	return WriteCLIManifest(p.OutputDir, m)
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
