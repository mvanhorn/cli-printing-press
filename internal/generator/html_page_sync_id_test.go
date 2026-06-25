package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedHTMLPageModeSingleObjectIDSynthesis(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "html-page-ids",
		Version: "0.1.0",
		BaseURL: "https://example.test",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/html-page-ids-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"pages": {
				Description: "HTML pages",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:         "GET",
						Path:           "/pages",
						Description:    "Fetch page metadata",
						ResponseFormat: spec.ResponseFormatHTML,
						HTMLExtract:    &spec.HTMLExtract{Mode: spec.HTMLExtractModePage},
						Response:       spec.ResponseDef{Type: "object", Item: "Page"},
					},
				},
			},
			"ships": {
				Description: "HTML ship pages",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:         "GET",
						Path:           "/ships",
						Description:    "Fetch ship metadata",
						IDField:        "imo",
						ResponseFormat: spec.ResponseFormatHTML,
						HTMLExtract:    &spec.HTMLExtract{Mode: spec.HTMLExtractModePage},
						Response:       spec.ResponseDef{Type: "object", Item: "Ship"},
					},
				},
			},
			"articles": {
				Description: "JSON articles",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/articles",
						Description: "Fetch article metadata",
						Response:    spec.ResponseDef{Type: "object", Item: "Article"},
					},
				},
			},
			"widgets": {
				Description: "JSON widgets",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/widgets",
						Description: "List widgets",
						Response:    spec.ResponseDef{Type: "array", Item: "Widget"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Page": {
				Fields: []spec.TypeField{
					{Name: "title", Type: "string"},
					{Name: "canonical_url", Type: "string"},
					{Name: "image_url", Type: "string"},
					{Name: "slug", Type: "string"},
					{Name: "key", Type: "string"},
				},
			},
			"Ship": {
				Fields: []spec.TypeField{
					{Name: "imo", Type: "string"},
					{Name: "name", Type: "string"},
					{Name: "canonical_url", Type: "string"},
				},
			},
			"Article": {
				Fields: []spec.TypeField{
					{Name: "title", Type: "string"},
					{Name: "canonical_url", Type: "string"},
					{Name: "image_url", Type: "string"},
				},
			},
			"Widget": {
				Fields: []spec.TypeField{
					{Name: "id", Type: "string"},
					{Name: "name", Type: "string"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	inlineTest := `package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"` + naming.CLI(apiSpec.Name) + `/internal/store"
)

type htmlPageClient struct{}

func (htmlPageClient) Get(ctx context.Context, path string, params map[string]string) (json.RawMessage, error) {
	return json.RawMessage(` + "`" + `<!doctype html><html><head><title>Docs</title><link rel="canonical" href="https://example.test/docs/page"><meta property="og:image" content="https://example.test/image.png"></head><body>Docs</body></html>` + "`" + `), nil
}

func (htmlPageClient) RequestBaseURL() string {
	return "https://example.test"
}

func (htmlPageClient) LastContentType() string {
	return "text/html; charset=utf-8"
}

func (htmlPageClient) RateLimit() float64 {
	return 0
}

func assertStoredID(t *testing.T, db *store.Store, table, resource, id string) {
	t.Helper()

	var genericCount int
	if err := db.DB().QueryRow("SELECT COUNT(*) FROM resources WHERE resource_type = ? AND id = ?", resource, id).Scan(&genericCount); err != nil {
		t.Fatalf("query resources %s/%s: %v", resource, id, err)
	}
	if genericCount != 1 {
		t.Fatalf("generic %s rows for %q = %d, want 1", resource, id, genericCount)
	}

	var typedCount int
	if err := db.DB().QueryRow("SELECT COUNT(*) FROM "+table+" WHERE id = ?", id).Scan(&typedCount); err != nil {
		t.Fatalf("query %s %s: %v", table, id, err)
	}
	if typedCount != 1 {
		t.Fatalf("typed %s rows for %q = %d, want 1", table, id, typedCount)
	}
}

func TestSyncSingleObjectHTMLPageModeIDFallbacks(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	canonicalURL := "https://example.test/docs/page"
	res := syncResource(context.Background(), htmlPageClient{}, db, "pages", "", false, 1, false, false, nil, nil)
	if res.Err != nil {
		t.Fatalf("syncResource canonical_url fallback: %v", res.Err)
	}
	if res.Count != 1 {
		t.Fatalf("syncResource count = %d, want 1", res.Count)
	}
	assertStoredID(t, db, "pages", "pages", canonicalURL)

	pageURL := "https://example.test/docs/url-only"
	if err := upsertSingleObject(db, "pages", json.RawMessage(` + "`" + `{"title":"URL only","url":"https://example.test/docs/url-only","image_url":"https://example.test/url-only.png"}` + "`" + `)); err != nil {
		t.Fatalf("upsert url fallback: %v", err)
	}
	assertStoredID(t, db, "pages", "pages", pageURL)

	imageURL := "https://example.test/image-only.png"
	if err := upsertSingleObject(db, "pages", json.RawMessage(` + "`" + `{"title":"Image only","image_url":"https://example.test/image-only.png"}` + "`" + `)); err != nil {
		t.Fatalf("upsert image_url fallback: %v", err)
	}
	assertStoredID(t, db, "pages", "pages", imageURL)

	if err := upsertSingleObject(db, "pages", json.RawMessage(` + "`" + `{"title":"Resource fallback"}` + "`" + `)); err != nil {
		t.Fatalf("upsert resource-name fallback: %v", err)
	}
	assertStoredID(t, db, "pages", "pages", "pages")

	if err := upsertSingleObject(db, "pages", json.RawMessage(` + "`" + `{"title":"Slugged","slug":"explicit-slug","canonical_url":"https://example.test/docs/slugged"}` + "`" + `)); err != nil {
		t.Fatalf("upsert slug page: %v", err)
	}
	assertStoredID(t, db, "pages", "pages", "explicit-slug")

	if err := upsertSingleObject(db, "pages", json.RawMessage(` + "`" + `{"title":"Vendor keyed","key":"vendor-key"}` + "`" + `)); err != nil {
		t.Fatalf("upsert vendor-key fallback: %v", err)
	}
	assertStoredID(t, db, "pages", "pages", "vendor-key")

	if err := upsertSingleObject(db, "ships", json.RawMessage(` + "`" + `{"imo":"IMO-123","name":"Mutable Vessel Name","canonical_url":"https://example.test/ships/IMO-123"}` + "`" + `)); err != nil {
		t.Fatalf("upsert x-resource-id page: %v", err)
	}
	assertStoredID(t, db, "ships", "ships", "IMO-123")

	if err := upsertSingleObject(db, "articles", json.RawMessage(` + "`" + `{"title":"JSON article","canonical_url":"https://example.test/articles/a"}` + "`" + `)); err == nil {
		t.Fatalf("JSON single-object resource without an id-shaped field unexpectedly succeeded")
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_html_page_id_test.go"), []byte(inlineTest), 0o644))

	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestSyncSingleObjectHTMLPageModeIDFallbacks$", "-count=1")
}

func TestGeneratedJSONOnlySyncKeepsSingleObjectIDShape(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "json-only-sync",
		Version: "0.1.0",
		BaseURL: "https://example.test",
		Auth:    spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/json-only-sync-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"articles": {
				Description: "JSON articles",
				Endpoints: map[string]spec.Endpoint{
					"list": {
						Method:      "GET",
						Path:        "/articles",
						Description: "Fetch article metadata",
						Response:    spec.ResponseDef{Type: "object", Item: "Article"},
					},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Article": {
				Fields: []spec.TypeField{
					{Name: "id", Type: "string"},
					{Name: "title", Type: "string"},
					{Name: "canonical_url", Type: "string"},
					{Name: "image_url", Type: "string"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	syncSrc := readGeneratedFile(t, outputDir, "internal", "cli", "sync.go")
	require.Contains(t, syncSrc, "id := extractID(resource, obj)")
	require.Contains(t, syncSrc, "return db.UpsertArticles(data)")
	require.False(t, strings.Contains(syncSrc, "synthesizeHTMLPageModeID"), "JSON-only sync must not emit HTML page-mode ID fallback")
	require.False(t, strings.Contains(syncSrc, "htmlPageModeResources"), "JSON-only sync must not emit HTML page-mode resource map")
	require.False(t, strings.Contains(syncSrc, "typedSingleObjectData"), "JSON-only sync must not emit typed ID injection helper")
}
