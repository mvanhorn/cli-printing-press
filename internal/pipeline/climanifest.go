package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mvanhorn/cli-printing-press/internal/openapi"
)

// CLIManifestFilename is the name of the manifest file written to each
// published CLI directory.
const CLIManifestFilename = ".printing-press.json"

// CLIManifest captures provenance metadata for a generated CLI.
// It is written to the root of each published CLI directory so the
// folder is self-describing even in isolation.
type CLIManifest struct {
	SchemaVersion       int       `json:"schema_version"`
	GeneratedAt         time.Time `json:"generated_at"`
	PrintingPressVersion string   `json:"printing_press_version"`
	APIName             string    `json:"api_name"`
	CLIName             string    `json:"cli_name"`
	SpecURL             string    `json:"spec_url,omitempty"`
	SpecPath            string    `json:"spec_path,omitempty"`
	SpecFormat          string    `json:"spec_format,omitempty"`
	SpecChecksum        string    `json:"spec_checksum,omitempty"`
	RunID               string    `json:"run_id,omitempty"`
	CatalogEntry        string    `json:"catalog_entry,omitempty"`
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
