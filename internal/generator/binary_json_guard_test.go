package generator

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

const sitemapXML = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url><loc>https://example.com/</loc></url>
</urlset>
`

func TestGeneratedBinaryAndTextReadsSkipLiveJSONGuard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml":
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(sitemapXML))
		case "/llms.txt":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("# Docs\nUse the CLI.\n"))
		case "/items":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><body><form>login</form></body></html>`))
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	apiSpec := minimalSpec("binary-json-guard")
	apiSpec.BaseURL = server.URL
	apiSpec.Learn.Disabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"page": {
			Description: "Site pages",
			Endpoints: map[string]spec.Endpoint{
				"index": {
					Method:         http.MethodGet,
					Path:           "/sitemap.xml",
					Description:    "Fetch the XML sitemap",
					ResponseFormat: spec.ResponseFormatBinary,
				},
				"robots": {
					Method:         http.MethodGet,
					Path:           "/robots.txt",
					Description:    "Fetch robots.txt",
					ResponseFormat: spec.ResponseFormatBinary,
				},
			},
		},
		"docs": {
			Description: "Plain-text docs",
			Endpoints: map[string]spec.Endpoint{
				"llms": {
					Method:         http.MethodGet,
					Path:           "/llms.txt",
					Description:    "Fetch llms.txt",
					ResponseFormat: spec.ResponseFormatText,
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

	pageIndexSrc := readGeneratedFile(t, outputDir, "internal", "cli", "page_index.go")
	require.Contains(t, pageIndexSrc, `resolveReadWithStrategyResponsePathAndJSONGuard(`,
		"store-backed binary reads must use the JSON-guard variant")
	require.Contains(t, pageIndexSrc, `false, cmd.ErrOrStderr())`,
		"binary reads must pass guardLiveJSON=false")
	require.NotContains(t, pageIndexSrc, `resolveReadWithStrategyAndResponsePath(`,
		"binary reads must not use the wrapper that hardcodes guardLiveJSON=true")
	require.NotContains(t, pageIndexSrc, `"pp:typed-exit-codes": "0,1"`,
		"binary commands must not treat generic exit 1 as designed success")

	docsSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_docs.go")
	require.Contains(t, docsSrc, `resolveReadWithStrategyResponsePathAndJSONGuard(`,
		"store-backed text reads must use the JSON-guard variant")
	require.Contains(t, docsSrc, `false, cmd.ErrOrStderr())`,
		"text reads must pass guardLiveJSON=false")

	itemsSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_items.go")
	require.Contains(t, itemsSrc, `resolveReadWithStrategyAndResponsePath(`,
		"ordinary JSON reads must keep the default live JSON guard")
	require.NotContains(t, itemsSrc, `resolveReadWithStrategyResponsePathAndJSONGuard(`,
		"ordinary JSON reads must not skip the live JSON guard")

	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))

	baseEnv := append(os.Environ(),
		"HOME="+t.TempDir(),
		"MYAPI_TOKEN=test-token",
		strings.ToUpper(strings.ReplaceAll(apiSpec.Name, "-", "_"))+"_BASE_URL="+server.URL,
	)

	sitemapOut, err := runGeneratedCLI(t, binaryPath, baseEnv, "page", "index")
	require.NoError(t, err, sitemapOut)
	require.Contains(t, sitemapOut, `<urlset`)
	require.NotContains(t, sitemapOut, "returned HTML instead of JSON")

	docsOut, err := runGeneratedCLI(t, binaryPath, baseEnv, "docs")
	require.NoError(t, err, docsOut)
	require.Contains(t, docsOut, "# Docs")
	require.NotContains(t, docsOut, "returned HTML instead of JSON")

	itemsOut, err := runGeneratedCLI(t, binaryPath, baseEnv, "items", "--json")
	require.Error(t, err, itemsOut)
	require.Contains(t, itemsOut, "returned HTML instead of JSON")
	requireExitCode(t, err, 4)

	jsonOut, err := runGeneratedCLI(t, binaryPath, baseEnv, "page", "index", "--json")
	require.Error(t, err, jsonOut)
	require.Contains(t, jsonOut, "binary response cannot be rendered as structured output")
	require.NotContains(t, jsonOut, "returned HTML instead of JSON")
	requireExitCode(t, err, 2)
}

func TestGeneratedPaginatedBinaryTextRejectsLiveAll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/feeds/sitemap.xml":
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(sitemapXML))
		case "/feeds/llms.txt":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("# Docs\nUse the CLI.\n"))
		case "/items":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"1"}]`))
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	apiSpec := minimalSpec("binary-paginated-all")
	apiSpec.BaseURL = server.URL
	apiSpec.Learn.Disabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"feeds": {
			Description: "Non-JSON feeds",
			Endpoints: map[string]spec.Endpoint{
				"sitemap": {
					Method:         http.MethodGet,
					Path:           "/feeds/sitemap.xml",
					Description:    "Fetch the XML sitemap",
					ResponseFormat: spec.ResponseFormatBinary,
					Params: []spec.Param{
						{Name: "limit", Type: "integer"},
						{Name: "after", Type: "string"},
					},
					Pagination: &spec.Pagination{
						Type:           "cursor",
						LimitParam:     "limit",
						CursorParam:    "after",
						NextCursorPath: "next_cursor",
					},
				},
				"robots": {
					Method:         http.MethodGet,
					Path:           "/feeds/robots.txt",
					Description:    "Fetch robots.txt",
					ResponseFormat: spec.ResponseFormatBinary,
				},
			},
		},
		"docs": {
			Description: "Plain-text docs",
			Endpoints: map[string]spec.Endpoint{
				"llms": {
					Method:         http.MethodGet,
					Path:           "/feeds/llms.txt",
					Description:    "Fetch llms.txt",
					ResponseFormat: spec.ResponseFormatText,
					Params: []spec.Param{
						{Name: "limit", Type: "integer"},
						{Name: "after", Type: "string"},
					},
					Pagination: &spec.Pagination{
						Type:           "cursor",
						LimitParam:     "limit",
						CursorParam:    "after",
						NextCursorPath: "next_cursor",
					},
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
					Params: []spec.Param{
						{Name: "limit", Type: "integer"},
						{Name: "after", Type: "string"},
					},
					Pagination: &spec.Pagination{
						Type:           "cursor",
						LimitParam:     "limit",
						CursorParam:    "after",
						NextCursorPath: "next_cursor",
					},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Search: true, MCP: true}
	require.NoError(t, gen.Generate())
	requireGeneratedCompiles(t, outputDir)

	feedsSrc := readGeneratedFile(t, outputDir, "internal", "cli", "feeds_sitemap.go")
	require.Contains(t, feedsSrc, `false, liveAllRejectNonJSON, cmd.ErrOrStderr())`)
	require.NotContains(t, feedsSrc, `liveAllRejectHTML`)

	docsSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_docs.go")
	require.Contains(t, docsSrc, `false, liveAllRejectNonJSON, cmd.ErrOrStderr())`)

	itemsSrc := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_items.go")
	require.Contains(t, itemsSrc, `resolvePaginatedReadWithStrategy(`)
	require.NotContains(t, itemsSrc, `liveAllRejectNonJSON`)

	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))

	baseEnv := append(os.Environ(),
		"HOME="+t.TempDir(),
		"MYAPI_TOKEN=test-token",
		strings.ToUpper(strings.ReplaceAll(apiSpec.Name, "-", "_"))+"_BASE_URL="+server.URL,
	)

	sitemapOut, err := runGeneratedCLI(t, binaryPath, baseEnv, "feeds", "sitemap")
	require.NoError(t, err, sitemapOut)
	require.Contains(t, sitemapOut, `<urlset`)
	require.NotContains(t, sitemapOut, "[]")

	allOut, err := runGeneratedCLI(t, binaryPath, baseEnv, "feeds", "sitemap", "--all")
	require.Error(t, err, allOut)
	require.Contains(t, allOut, "--all is not supported for live binary/text responses")
	require.NotContains(t, allOut, "--all is not supported for live HTML responses")
	require.NotContains(t, allOut, `"results":[]`)

	docsAllOut, err := runGeneratedCLI(t, binaryPath, baseEnv, "docs", "--all")
	require.Error(t, err, docsAllOut)
	require.Contains(t, docsAllOut, "--all is not supported for live binary/text responses")
	require.NotContains(t, docsAllOut, "--all is not supported for live HTML responses")

	localAllOut, err := runGeneratedCLI(t, binaryPath, baseEnv, "feeds", "sitemap", "--all", "--data-source", "local")
	require.Error(t, err, localAllOut)
	require.NotContains(t, localAllOut, "--all is not supported for live binary/text responses")
	require.Contains(t, localAllOut, "no local data")

	itemsOut, err := runGeneratedCLI(t, binaryPath, baseEnv, "items", "--json", "--all")
	require.NoError(t, err, itemsOut)
	require.Contains(t, itemsOut, `"id": "1"`)

	storelessSpec := *apiSpec
	storelessSpec.Learn.Disabled = true
	storelessDir := filepath.Join(t.TempDir(), naming.CLI(storelessSpec.Name))
	storelessGen := New(&storelessSpec, storelessDir)
	storelessGen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, storelessGen.Generate())
	requireGeneratedCompiles(t, storelessDir)
	storelessBinary := filepath.Join(storelessDir, naming.CLI(storelessSpec.Name))
	runGoCommand(t, storelessDir, "build", "-o", storelessBinary, "./cmd/"+naming.CLI(storelessSpec.Name))

	storelessAll, err := runGeneratedCLI(t, storelessBinary, baseEnv, "feeds", "sitemap", "--all")
	require.Error(t, err, storelessAll)
	require.Contains(t, storelessAll, "--all is not supported for live binary/text responses")
	require.NotContains(t, storelessAll, "--all is not supported for live HTML responses")
}

func runGeneratedCLI(t *testing.T, binaryPath string, env []string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func requireExitCode(t *testing.T, err error, want int) {
	t.Helper()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	require.Equal(t, want, exitErr.ExitCode())
}
