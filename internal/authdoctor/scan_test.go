package authdoctor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
)

func writeManifest(t *testing.T, dir string, m pipeline.ToolsManifest) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	path := filepath.Join(dir, pipeline.ToolsManifestFilename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestScanAtMissingRoot(t *testing.T) {
	findings, err := ScanAt(filepath.Join(t.TempDir(), "nonexistent"), envFrom(nil))
	if err != nil {
		t.Fatalf("missing root should not error, got %v", err)
	}
	if len(findings) != 0 {
		t.Errorf("want empty findings for missing root, got %d", len(findings))
	}
}

func TestScanAtFullMatrix(t *testing.T) {
	root := t.TempDir()

	writeManifest(t, filepath.Join(root, "hubspot"), pipeline.ToolsManifest{
		APIName: "HubSpot",
		Auth: pipeline.ManifestAuth{
			Type:    "api_key",
			EnvVars: []string{"HUBSPOT_ACCESS_TOKEN"},
		},
	})
	writeManifest(t, filepath.Join(root, "espn"), pipeline.ToolsManifest{
		APIName: "ESPN",
		Auth: pipeline.ManifestAuth{
			Type:    "api_key",
			EnvVars: []string{"ESPN_KEY"},
		},
	})
	writeManifest(t, filepath.Join(root, "dub"), pipeline.ToolsManifest{
		APIName: "Dub",
		Auth: pipeline.ManifestAuth{
			Type:    "bearer_token",
			EnvVars: []string{"DUB_TOKEN"},
		},
	})
	writeManifest(t, filepath.Join(root, "hackernews"), pipeline.ToolsManifest{
		APIName: "Hacker News",
		Auth:    pipeline.ManifestAuth{Type: "none"},
	})

	env := envFrom(map[string]string{
		"HUBSPOT_ACCESS_TOKEN": "pat-xxxxxxxxxxxxxxxxxxxx",
		// ESPN_KEY unset -> not_set
		"DUB_TOKEN": "abc", // too short -> suspicious
		// hackernews has no auth -> no_auth
	})

	findings, err := ScanAt(root, env)
	if err != nil {
		t.Fatalf("ScanAt: %v", err)
	}

	// Results sorted by API slug.
	if len(findings) != 4 {
		t.Fatalf("want 4 findings, got %d: %+v", len(findings), findings)
	}

	// dub first alphabetically
	if findings[0].API != "dub" || findings[0].Status != StatusSuspicious {
		t.Errorf("findings[0] = %+v, want dub/suspicious", findings[0])
	}
	if findings[1].API != "espn" || findings[1].Status != StatusNotSet {
		t.Errorf("findings[1] = %+v, want espn/not_set", findings[1])
	}
	if findings[2].API != "hackernews" || findings[2].Status != StatusNoAuth {
		t.Errorf("findings[2] = %+v, want hackernews/no_auth", findings[2])
	}
	if findings[3].API != "hubspot" || findings[3].Status != StatusOK {
		t.Errorf("findings[3] = %+v, want hubspot/ok", findings[3])
	}

	s := Summarize(findings)
	if s.OK != 1 || s.Suspicious != 1 || s.NotSet != 1 || s.NoAuth != 1 {
		t.Errorf("summary = %+v, want 1/1/1/1", s)
	}
}

func TestScanAtSkipsDirectoryWithoutManifest(t *testing.T) {
	root := t.TempDir()

	// Legacy CLI directory with no manifest file.
	if err := os.MkdirAll(filepath.Join(root, "legacy-cli"), 0o755); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}
	// A real CLI with a manifest.
	writeManifest(t, filepath.Join(root, "hubspot"), pipeline.ToolsManifest{
		APIName: "HubSpot",
		Auth:    pipeline.ManifestAuth{Type: "none"},
	})

	findings, err := ScanAt(root, envFrom(nil))
	if err != nil {
		t.Fatalf("ScanAt: %v", err)
	}
	if len(findings) != 1 || findings[0].API != "hubspot" {
		t.Errorf("legacy dir should be skipped silently; findings=%+v", findings)
	}
}

func TestScanAtCorruptManifestReportedAsUnknown(t *testing.T) {
	root := t.TempDir()

	// Well-formed manifest for one CLI.
	writeManifest(t, filepath.Join(root, "hubspot"), pipeline.ToolsManifest{
		APIName: "HubSpot",
		Auth:    pipeline.ManifestAuth{Type: "none"},
	})

	// Corrupt manifest for another.
	corruptDir := filepath.Join(root, "broken")
	if err := os.MkdirAll(corruptDir, 0o755); err != nil {
		t.Fatalf("mkdir broken: %v", err)
	}
	if err := os.WriteFile(filepath.Join(corruptDir, pipeline.ToolsManifestFilename), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	findings, err := ScanAt(root, envFrom(nil))
	if err != nil {
		t.Fatalf("corrupt manifest should not fail whole scan: %v", err)
	}
	if len(findings) != 2 {
		t.Fatalf("want 2 findings (one per CLI), got %d", len(findings))
	}
	// Sorted: broken before hubspot.
	if findings[0].API != "broken" || findings[0].Status != StatusUnknown {
		t.Errorf("findings[0] = %+v, want broken/unknown", findings[0])
	}
	if findings[0].Reason == "" {
		t.Error("corrupt manifest finding should carry a reason")
	}
	if findings[1].API != "hubspot" || findings[1].Status != StatusNoAuth {
		t.Errorf("findings[1] = %+v, want hubspot/no_auth", findings[1])
	}
}

func TestScanAtIgnoresNonDirEntries(t *testing.T) {
	root := t.TempDir()
	// Stray file in library root should be ignored.
	if err := os.WriteFile(filepath.Join(root, ".DS_Store"), []byte("noise"), 0o644); err != nil {
		t.Fatalf("write stray: %v", err)
	}
	writeManifest(t, filepath.Join(root, "hubspot"), pipeline.ToolsManifest{
		APIName: "HubSpot",
		Auth:    pipeline.ManifestAuth{Type: "none"},
	})

	findings, err := ScanAt(root, envFrom(nil))
	if err != nil {
		t.Fatalf("ScanAt: %v", err)
	}
	if len(findings) != 1 {
		t.Errorf("want 1 finding (stray file ignored), got %d", len(findings))
	}
}
