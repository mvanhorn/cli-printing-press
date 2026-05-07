// Package authdoctor scans the local printing-press library and reports
// the env-var status of every installed printed CLI. It is the data layer
// for the `printing-press auth doctor` command.
//
// The scanner reads each API's tools-manifest.json and classifies each
// declared env var as ok, suspicious, not-set, or no-auth. No network
// calls. No writes to disk.
package authdoctor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
)

// Scan inspects every installed printed CLI under the published library
// root and returns Findings ordered by API slug. A missing library
// directory is not an error; it returns an empty slice. Unreadable
// manifest files produce StatusUnknown findings rather than failing the
// whole scan.
//
// The environment is read via os.Getenv.
func Scan() ([]Finding, error) {
	return ScanAt(pipeline.PublishedLibraryRoot(), os.Getenv)
}

// ScanAt is the testable form of Scan. It accepts an explicit library
// root directory and an env lookup function so tests can seed a
// synthetic library and environment.
func ScanAt(libraryRoot string, env getEnv) ([]Finding, error) {
	dirEntries, err := os.ReadDir(libraryRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Finding{}, nil
		}
		return nil, fmt.Errorf("reading library root %q: %w", libraryRoot, err)
	}

	var findings []Finding
	for _, de := range dirEntries {
		if !de.IsDir() {
			continue
		}
		slug := de.Name()
		manifestPath := filepath.Join(libraryRoot, slug, pipeline.ToolsManifestFilename)

		manifest, loadErr := loadManifest(manifestPath)
		switch {
		case loadErr != nil && errors.Is(loadErr, os.ErrNotExist):
			// Directory exists but no manifest file. Skip — this is a
			// CLI generated before the manifest convention landed.
			continue
		case loadErr != nil:
			findings = append(findings, Finding{
				API:    slug,
				Status: StatusUnknown,
				Reason: fmt.Sprintf("manifest parse error: %v", loadErr),
			})
			continue
		}

		findings = append(findings, Classify(slug, manifest, env)...)
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].API != findings[j].API {
			return findings[i].API < findings[j].API
		}
		return findings[i].EnvVar < findings[j].EnvVar
	})

	return findings, nil
}

// loadManifest reads and parses a tools-manifest.json file.
func loadManifest(path string) (*pipeline.ToolsManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m pipeline.ToolsManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return &m, nil
}
