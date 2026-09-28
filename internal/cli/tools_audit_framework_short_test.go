package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

func TestGeneratedFrameworkListShortsPassToolsAudit(t *testing.T) {
	t.Parallel()

	const cliName = "shortspec"
	apiSpec := &spec.APISpec{
		Name:      cliName,
		Version:   "0.1.0",
		BaseURL:   "https://api.example.com",
		Owner:     "test-owner",
		OwnerName: "Test Author",
		Auth:      spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/shortspec-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), cliName+"-pp-cli")
	if err := generator.New(apiSpec, outputDir).Generate(); err != nil {
		t.Fatal(err)
	}

	platformSrc := readAuditFile(t, filepath.Join(outputDir, "internal", "cli", "platform_client.go"))
	teachSrc := readAuditFile(t, filepath.Join(outputDir, "internal", "cli", "teach.go"))
	profileSrc := readAuditFile(t, filepath.Join(outputDir, "internal", "cli", "profile.go"))

	const platformShort = `Short: "List client profile names from the shared printing-press clients store for shortspec-pp-cli"`
	const teachShort = `Short: "List recorded learnings from the local shortspec-pp-cli search_learnings table"`
	if !strings.Contains(platformSrc, platformShort) {
		t.Fatalf("platform list Short missing from emitted platform_client.go")
	}
	if !strings.Contains(teachSrc, teachShort) {
		t.Fatalf("learnings list Short missing from emitted teach.go")
	}
	if strings.Contains(platformSrc, `Short: "List client profiles"`) {
		t.Fatalf("emitted platform list Short is still the thin framework stub")
	}
	if strings.Contains(teachSrc, `Short: "List recorded learnings"`) {
		t.Fatalf("emitted learnings list Short is still the thin framework stub")
	}
	for _, want := range []string{
		`Short: "Show non-secret client profile configuration"`,
		`Short: "Set the cross-CLI default client profile"`,
		`Short: "Manage tenant-gated client profiles"`,
	} {
		if !strings.Contains(platformSrc, want) {
			t.Fatalf("platform sibling Short changed: %s", want)
		}
	}
	for _, want := range []string{
		`Short: "Inspect or forget the local search_learnings table"`,
		`Short: "Delete learnings matching a query (use --all to wipe every rule for that query)"`,
		`Short: "Record a query -> resource mapping for future recall (LLM-fired, silent)"`,
	} {
		if !strings.Contains(teachSrc, want) {
			t.Fatalf("teach sibling Short changed: %s", want)
		}
	}
	if !strings.Contains(profileSrc, `Short: "List saved profiles"`) {
		t.Fatalf("profile list Short changed")
	}

	assertNoFrameworkListThinShort(t, outputDir)

	handPath := filepath.Join(outputDir, "internal", "cli", "hand_notes.go")
	if err := os.WriteFile(handPath, []byte(`package cli

import "github.com/spf13/cobra"

func newHandNotesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List notes",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	var handFlagged bool
	for _, f := range auditOrFatal(t, outputDir) {
		if f.Kind == kindThinShort && f.File == "hand_notes.go" && f.Command == "list" {
			handFlagged = true
		}
	}
	if !handFlagged {
		t.Fatal("hand-authored thin Short was not flagged")
	}
	assertNoFrameworkListThinShort(t, outputDir)
}

func assertNoFrameworkListThinShort(t *testing.T, outputDir string) {
	t.Helper()
	for _, f := range auditOrFatal(t, outputDir) {
		if f.Kind != kindThinShort || f.Command != "list" {
			continue
		}
		if f.File == "platform_client.go" || f.File == "teach.go" {
			t.Fatalf("tools-audit thin-short on framework list: %+v", f)
		}
	}
}

func auditOrFatal(t *testing.T, outputDir string) []ToolsAuditFinding {
	t.Helper()
	findings, err := auditCobraSource(outputDir)
	if err != nil {
		t.Fatal(err)
	}
	return findings
}

func readAuditFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
