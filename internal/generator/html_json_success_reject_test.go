package generator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedClientRejectsHTMLOnJSONSuccess(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("html-json-reject")
	apiSpec.Learn.Disabled = true
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	require.Contains(t, clientSrc, "expected JSON, API returned HTML instead of JSON",
		"JSON success path must fail loud on an HTML document")
	require.Contains(t, clientSrc, "HTMLResponseHeader")
	require.Contains(t, clientSrc, "summarizeHTMLDocument")

	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "client", "html_json_success_reject_test.go"),
		[]byte(htmlJSONSuccessRejectClientTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/client", "-run", "^TestJSONSuccessRejectsHTMLDocument$", "-count=1")
}

func TestGeneratedHTMLExtractionStillSucceedsOnHTMLDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><a href="/posts/one">One</a></body></html>`))
	}))
	t.Cleanup(server.Close)

	apiSpec := minimalSpec("html-extract-ok")
	apiSpec.BaseURL = server.URL
	apiSpec.Learn.Disabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"html_posts": {
			Description: "HTML posts",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:         http.MethodGet,
					Path:           "/posts",
					Description:    "List posts",
					ResponseFormat: spec.ResponseFormatHTML,
					HTMLExtract: &spec.HTMLExtract{
						Mode:         spec.HTMLExtractModeLinks,
						LinkPrefixes: []string{"/posts"},
					},
					Response: spec.ResponseDef{Type: "array", Item: "html_link"},
				},
				"page": {
					Method:         http.MethodGet,
					Path:           "/posts",
					Description:    "Read an HTML page",
					ResponseFormat: spec.ResponseFormatHTML,
					HTMLExtract:    &spec.HTMLExtract{Mode: spec.HTMLExtractModePage},
				},
			},
		},
		"items": {
			Description: "JSON items",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      http.MethodGet,
					Path:        "/items",
					Description: "List items",
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Search: true, MCP: true}
	require.NoError(t, gen.Generate())
	requireGeneratedCompiles(t, outputDir)

	htmlSrc := readGeneratedFile(t, outputDir, "internal", "cli", "html_posts_list.go")
	require.Contains(t, htmlSrc, `"X-Printing-Press-HTML-Response": "true"`,
		"HTML-extraction commands must opt the shared client out of the JSON HTML guard")

	itemsSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_items.go")
	require.NotContains(t, itemsSrc, `"X-Printing-Press-HTML-Response": "true"`,
		"JSON commands must not opt out of the HTML document guard")

	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "client", "html_extract_success_test.go"),
		[]byte(htmlExtractionClientTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/client", "-run", "^TestHTMLExtractionRequestAllowsHTMLDocument$", "-count=1")

	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))
	baseEnv := append(os.Environ(),
		"HOME="+t.TempDir(),
		"MYAPI_TOKEN=test-token",
		strings.ToUpper(strings.ReplaceAll(apiSpec.Name, "-", "_"))+"_BASE_URL="+server.URL,
	)

	htmlOut, err := runGeneratedCLI(t, binaryPath, baseEnv, "html-posts", "list", "--json", "--data-source", "live")
	require.NoError(t, err, htmlOut)
	var envelope struct {
		Results []map[string]any `json:"results"`
	}
	require.NoError(t, json.Unmarshal([]byte(htmlOut), &envelope), htmlOut)
	require.Len(t, envelope.Results, 1)
	require.Equal(t, "One", envelope.Results[0]["name"])

	itemsOut, err := runGeneratedCLI(t, binaryPath, baseEnv, "items", "--json", "--data-source", "live")
	require.Error(t, err, itemsOut)
	require.Contains(t, itemsOut, "returned HTML instead of JSON")
	require.NotContains(t, itemsOut, `"source": "live"`)
}

const htmlJSONSuccessRejectClientTest = `package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"html-json-reject-pp-cli/internal/config"
)

func TestJSONSuccessRejectsHTMLDocument(t *testing.T) {
	html := ` + "`" + `<!doctype html><html><head><title>Sign in</title></head><body><form>login</form></body></html>` + "`" + `
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get(HTMLResponseHeader), "true") {
			t.Fatalf("JSON request leaked %s to the server", HTMLResponseHeader)
		}
		switch r.URL.Path {
		case "/html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(html))
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	c := New(&config.Config{BaseURL: server.URL}, time.Second, 0)
	c.NoCache = true

	_, err := c.Get(context.Background(), "/html", nil)
	if err == nil {
		t.Fatal("Get(200 HTML) succeeded; want expected-JSON error")
	}
	if !strings.Contains(err.Error(), "returned HTML instead of JSON") {
		t.Fatalf("Get(200 HTML) error = %v, want HTML instead of JSON", err)
	}
	if !strings.Contains(err.Error(), "Sign in") {
		t.Fatalf("Get(200 HTML) error = %v, want document title", err)
	}

	got, err := c.Get(context.Background(), "/json", nil)
	if err != nil {
		t.Fatalf("Get(200 JSON) error = %v", err)
	}
	if string(got) != ` + "`" + `{"ok":true}` + "`" + ` {
		t.Fatalf("Get(200 JSON) = %s, want {\"ok\":true}", got)
	}
}
`

const htmlExtractionClientTest = `package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"html-extract-ok-pp-cli/internal/config"
)

func TestHTMLExtractionRequestAllowsHTMLDocument(t *testing.T) {
	html := ` + "`" + `<html><body><a href="/posts/one">One</a></body></html>` + "`" + `
	var sawHTMLHeader bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get(HTMLResponseHeader), "true") {
			t.Fatalf("HTML opt-out header leaked to the server: %q", r.Header.Get(HTMLResponseHeader))
		}
		if r.URL.Path == "/posts" {
			sawHTMLHeader = true
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	}))
	t.Cleanup(server.Close)

	c := New(&config.Config{BaseURL: server.URL}, time.Second, 0)
	c.NoCache = true

	_, err := c.Get(context.Background(), "/items", nil)
	if err == nil {
		t.Fatal("JSON Get(200 HTML) succeeded; want expected-JSON error")
	}
	if !strings.Contains(err.Error(), "returned HTML instead of JSON") {
		t.Fatalf("JSON Get(200 HTML) error = %v, want HTML instead of JSON", err)
	}

	got, err := c.GetWithHeaders(context.Background(), "/posts", nil, map[string]string{HTMLResponseHeader: "true"})
	if err != nil {
		t.Fatalf("HTML GetWithHeaders(200 HTML) error = %v", err)
	}
	if !strings.Contains(string(got), "<a href=\"/posts/one\">One</a>") {
		t.Fatalf("HTML GetWithHeaders body = %s, want raw HTML", got)
	}
	if !sawHTMLHeader {
		t.Fatal("HTML extraction request never reached the server")
	}
}
`
