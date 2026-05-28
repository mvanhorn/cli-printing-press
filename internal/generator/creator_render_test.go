package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func creatorRenderSpec() *spec.APISpec {
	return &spec.APISpec{
		Name:    "acme",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Creator: spec.Person{Handle: "trevin-chow", Name: "Trevin Chow"},
		Contributors: []spec.Person{
			{Handle: "jane-doe", Name: "Jane Doe"},
			{Handle: "mvanhorn", Name: "Matt Van Horn"},
		},
		Auth: spec.AuthConfig{
			Type:    "api_key",
			Header:  "Authorization",
			Format:  "Bearer {token}",
			EnvVars: []string{"ACME_API_KEY"},
		},
		Config: spec.ConfigSpec{Format: "toml", Path: "~/.config/acme-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}
}

// End-to-end render check for the creator/contributors attribution surfaces:
// copyright header, README byline + contributors line, and NOTICE. Also
// compiles the generated module so the emitted Go is valid, not just present.
func TestGeneratedAttributionSurfaces(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "acme-pp-cli")
	gen := New(creatorRenderSpec(), outputDir)
	require.NoError(t, gen.Generate())

	year := fmt.Sprint(time.Now().Year())

	// Copyright header: display name + constant " and contributors" suffix.
	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.Contains(t, string(rootGo), fmt.Sprintf("// Copyright %s Trevin Chow and contributors.", year))

	// README byline: creator top-billed, contributors listed after.
	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readme), "Created by [@trevin-chow](https://github.com/trevin-chow) (Trevin Chow).")
	assert.Contains(t, string(readme), "Contributors: [@jane-doe](https://github.com/jane-doe) (Jane Doe), [@mvanhorn](https://github.com/mvanhorn) (Matt Van Horn).")

	// NOTICE: per-CLI creator/contributors block + machine co-author line.
	notice, err := os.ReadFile(filepath.Join(outputDir, "NOTICE"))
	require.NoError(t, err)
	noticeStr := string(notice)
	assert.Contains(t, noticeStr, fmt.Sprintf("Copyright %s Trevin Chow and contributors", year))
	assert.Contains(t, noticeStr, "Created by Trevin Chow (@trevin-chow).")
	assert.Contains(t, noticeStr, "Jane Doe (@jane-doe)")
	assert.Contains(t, noticeStr, "Matt Van Horn (@mvanhorn)")
	assert.Contains(t, noticeStr, "by Matt Van Horn and Trevin Chow.")

	requireGeneratedCompiles(t, outputDir)
}

// A contributor recorded with only a display name (no handle) renders as the
// bare name — not a broken `[@](https://github.com/)` link or `(@)`.
func TestGeneratedAttributionNameOnlyContributor(t *testing.T) {
	s := creatorRenderSpec()
	s.Contributors = []spec.Person{
		{Handle: "jane-doe", Name: "Jane Doe"},
		{Name: "Nameless Helper"}, // no handle
	}
	outputDir := filepath.Join(t.TempDir(), "acme-pp-cli")
	gen := New(s, outputDir)
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readme), "Nameless Helper")
	assert.NotContains(t, string(readme), "[@](https://github.com/)", "name-only contributor must not render a broken link")
	assert.NotContains(t, string(readme), "https://github.com/) (Nameless", "no empty-handle href")

	notice, err := os.ReadFile(filepath.Join(outputDir, "NOTICE"))
	require.NoError(t, err)
	assert.Contains(t, string(notice), "Nameless Helper")
	assert.NotContains(t, string(notice), "(@)", "name-only contributor must not render an empty (@) handle")
}

// A malicious creator name/handle is neutralized at the source: the generated
// module still compiles (a newline would have broken out of the copyright
// comment) and the README byline carries no injected markdown link.
func TestGeneratedAttributionSanitizesMaliciousAttribution(t *testing.T) {
	s := creatorRenderSpec()
	s.Creator = spec.Person{Handle: "evil) [x](http://bad", Name: "Bad\n// injected"}
	s.Contributors = nil
	outputDir := filepath.Join(t.TempDir(), "acme-pp-cli")
	gen := New(s, outputDir)
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	assert.NotContains(t, string(readme), "[x](http://bad", "markdown link injection must be neutralized")
	assert.NotContains(t, string(readme), "https://github.com/evil)", "handle must be charset-constrained")

	requireGeneratedCompiles(t, outputDir)
}

// A solo CLI (creator, no contributors) still carries the "and contributors"
// suffix in the header but renders no contributors listing.
func TestGeneratedAttributionSoloCLI(t *testing.T) {
	s := creatorRenderSpec()
	s.Contributors = nil
	outputDir := filepath.Join(t.TempDir(), "acme-pp-cli")
	gen := New(s, outputDir)
	require.NoError(t, gen.Generate())

	year := fmt.Sprint(time.Now().Year())
	rootGo, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	assert.Contains(t, string(rootGo), fmt.Sprintf("// Copyright %s Trevin Chow and contributors.", year))

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readme), "Created by [@trevin-chow](https://github.com/trevin-chow) (Trevin Chow).")
	assert.NotContains(t, string(readme), "Contributors:")
	assert.False(t, strings.Contains(string(readme), "Printed by"), "old byline must be gone")
}
