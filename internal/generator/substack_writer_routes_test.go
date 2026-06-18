package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateSubstackGlobalWriterRoutes(t *testing.T) {
	t.Parallel()

	apiSpec := substackWriterRoutesSpec()
	outputDir := filepath.Join(t.TempDir(), "substack-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	assert.Contains(t, rootSrc, `templateVarPublicationId string`)
	assert.Contains(t, rootSrc, `"publication-id"`)

	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	assert.Contains(t, configSrc, `os.Getenv("SUBSTACK_PUBLICATION_ID")`)
	assert.Contains(t, configSrc, `cfg.TemplateVars["publication_id"] = v`)

	draftsSrc := readGeneratedFile(t, outputDir, "internal", "cli", "drafts_create.go")
	assert.Contains(t, draftsSrc, `"pp:path": "https://substack.com/api/v1/drafts?publication_id={publication_id}"`)
	assert.Contains(t, draftsSrc, `resolveSubstackPublicationIDTemplate(cmd.Context(), c, flags)`)
	assert.NotContains(t, draftsSrc, `trevinsays.substack.com`)

	imagesSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_images.go")
	assert.Contains(t, imagesSrc, `"pp:path": "https://substack.com/api/v1/image"`)
	assert.Contains(t, imagesSrc, `path := "https://substack.com/api/v1/image"`)
	assert.NotContains(t, imagesSrc, `{publication}.substack.com/api/v1/image`)

	helpersSrc := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	assert.Contains(t, helpersSrc, `func resolveSubstackPublicationIDTemplate(ctx context.Context, c *client.Client, flags *rootFlags) error`)
	assert.Contains(t, helpersSrc, `c.Get(ctx, "/user/profile/self", nil)`)
	assert.Contains(t, helpersSrc, `dryRunPublicationIDPlaceholder`)
	assert.Contains(t, helpersSrc, `"<resolved-at-runtime>"`)
	assert.Contains(t, helpersSrc, `dryRunPublicationIDResolvedMarker`)
	assert.Contains(t, helpersSrc, `"__pp_substack_publication_id_dry_run_resolved"`)
	assert.Contains(t, helpersSrc, `dec := json.NewDecoder(bytes.NewReader(raw))`)
	assert.Contains(t, helpersSrc, `dec.UseNumber()`)
	assert.Contains(t, helpersSrc, `case json.Number:`)

	writeSubstackWriterGeneratedRuntimeTest(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/cli")
}

func writeSubstackWriterGeneratedRuntimeTest(t *testing.T, outputDir string) {
	t.Helper()
	content := `package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestDraftCreateDryRunUsesExplicitPublicationID(t *testing.T) {
	t.Setenv("SUBSTACK_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--publication", "trevinsays",
		"--publication-id", "7019888",
		"drafts", "create",
		"--title", "CLI verification dry-run",
		"--body", "Verification only.",
		"--dry-run",
		"--agent",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("execute dry-run: %v; stderr=%s", err, stderr.String())
	}
	var envelope struct {
		Path string ` + "`json:\"path\"`" + `
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &envelope); err != nil {
		t.Fatalf("parse stdout JSON %q: %v", stdout.String(), err)
	}
	if got, want := envelope.Path, "https://substack.com/api/v1/drafts?publication_id=7019888"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestDraftCreateDryRunWithoutPublicationIDDoesNotLookupLiveProfile(t *testing.T) {
	t.Setenv("SUBSTACK_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("SUBSTACK_BASE_URL", "http://127.0.0.1:1")

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--publication", "trevinsays",
		"drafts", "create",
		"--title", "CLI verification dry-run",
		"--body", "Verification only.",
		"--dry-run",
		"--agent",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("execute dry-run without publication id should not perform live lookup: %v; stderr=%s", err, stderr.String())
	}
	var envelope struct {
		Path string ` + "`json:\"path\"`" + `
	}
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &envelope); err != nil {
		t.Fatalf("parse stdout JSON %q: %v", stdout.String(), err)
	}
	if got, want := envelope.Path, "https://substack.com/api/v1/drafts?publication_id=<resolved-at-runtime>"; got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestDraftCreateDryRunRejectsExplicitPlaceholderPublicationID(t *testing.T) {
	t.Setenv("SUBSTACK_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--publication", "trevinsays",
		"--publication-id", "<resolved-at-runtime>",
		"drafts", "create",
		"--title", "CLI verification dry-run",
		"--body", "Verification only.",
		"--dry-run",
		"--agent",
	})

	err := root.Execute()
	if err == nil {
		t.Fatalf("explicit placeholder publication id should be rejected")
	}
	if got := err.Error(); !bytes.Contains([]byte(got), []byte(` + "`invalid publication_id \"<resolved-at-runtime>\"`" + `)) {
		t.Fatalf("error = %q, want invalid publication_id", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want no request envelope", stdout.String())
	}
}

func TestPublicationIDResolverDryRunPlaceholderIsIdempotent(t *testing.T) {
	t.Setenv("SUBSTACK_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	flags := &rootFlags{dryRun: true}
	c, err := flags.newClient()
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	if err := resolveSubstackPublicationIDTemplate(t.Context(), c, flags); err != nil {
		t.Fatalf("first resolveSubstackPublicationIDTemplate: %v", err)
	}
	if err := resolveSubstackPublicationIDTemplate(t.Context(), c, flags); err != nil {
		t.Fatalf("second resolveSubstackPublicationIDTemplate: %v", err)
	}
	if got, want := c.Config.TemplateVars["publication_id"], "<resolved-at-runtime>"; got != want {
		t.Fatalf("publication_id = %q, want %q", got, want)
	}
}

func TestPublicationIDResolverLooksUpProfileWhenNotDryRun(t *testing.T) {
	t.Setenv("SUBSTACK_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	var sawProfile bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user/profile/self" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		sawProfile = true
		_, _ = w.Write([]byte(` + "`" + `{"primaryPublication":{"id":9007199254740993,"subdomain":"trevinsays"}}` + "`" + `))
	}))
	defer server.Close()
	t.Setenv("SUBSTACK_BASE_URL", server.URL)

	flags := &rootFlags{}
	c, err := flags.newClient()
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	c.Config.TemplateVars["publication"] = "trevinsays"
	if err := resolveSubstackPublicationIDTemplate(t.Context(), c, flags); err != nil {
		t.Fatalf("resolveSubstackPublicationIDTemplate: %v", err)
	}
	if !sawProfile {
		t.Fatalf("profile endpoint was not queried")
	}
	if got, want := c.Config.TemplateVars["publication_id"], "9007199254740993"; got != want {
		t.Fatalf("publication_id = %q, want %q", got, want)
	}
}

func TestImagesDryRunUsesGlobalUploadEndpoint(t *testing.T) {
	t.Setenv("SUBSTACK_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))

	root := RootCmd()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{
		"--publication", "trevinsays",
		"images",
		"--image", "data:image/png;base64,AAAA",
		"--dry-run",
		"--agent",
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("execute image dry-run: %v; stderr=%s", err, stderr.String())
	}
	if bytes.Contains(stdout.Bytes(), []byte("trevinsays.substack.com")) || bytes.Contains(stdout.Bytes(), []byte("{publication}")) {
		t.Fatalf("stdout = %q, should not use publication-host image endpoint", stdout.String())
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "substack_writer_routes_test.go"), []byte(content), 0o644))
}

func substackWriterRoutesSpec() *spec.APISpec {
	return &spec.APISpec{
		Name:                         "substack",
		DisplayName:                  "Substack",
		Version:                      "0.1.0",
		BaseURL:                      "https://{publication}.substack.com/api/v1",
		EndpointTemplateVars:         []string{"publication", "publication_id"},
		EndpointTemplateEnvOverrides: map[string]string{"publication": "SUBSTACK_PUBLICATION", "publication_id": "SUBSTACK_PUBLICATION_ID"},
		GlobalPathTemplateVars:       []string{"publication", "publication_id"},
		EndpointTemplateVarDefaults:  map[string]string{},
		Auth:                         spec.AuthConfig{Type: "cookie", EnvVars: []string{"SUBSTACK_COOKIE"}},
		Config:                       spec.ConfigSpec{Format: "toml", Path: "~/.config/substack-pp-cli/config.toml"},
		Resources: map[string]spec.Resource{
			"drafts": {
				Description: "Manage drafts",
				Endpoints: map[string]spec.Endpoint{
					"create": {
						Method:      "POST",
						Path:        "https://substack.com/api/v1/drafts?publication_id={publication_id}",
						Description: "Create a draft",
						Body: []spec.Param{
							{Name: "title", Type: "string", Required: true},
							{Name: "body", Type: "string", Required: true},
						},
					},
					"list": {
						Method:      "GET",
						Path:        "/drafts",
						Description: "List drafts",
					},
				},
			},
			"images": {
				Description: "Upload images",
				Endpoints: map[string]spec.Endpoint{
					"upload": {
						Method:      "POST",
						Path:        "https://substack.com/api/v1/image",
						Description: "Upload an image",
						Body:        []spec.Param{{Name: "image", Type: "string", Required: true}},
					},
				},
			},
		},
	}
}
