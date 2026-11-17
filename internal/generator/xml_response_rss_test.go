package generator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/platform"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

const rssFeedBody = `<?xml version="1.0"?><rss version="2.0"><channel><title>Example</title>` +
	`<item><title>Alpha</title><link>https://example.test/a</link></item>` +
	`<item><title>Beta</title><link>https://example.test/b</link></item>` +
	`</channel></rss>`

func TestGeneratedRSSCommandNormalizesInternalSpecXML(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	seen := map[string]http.Header{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.URL.Path] = r.Header.Clone()
		mu.Unlock()
		switch r.URL.Path {
		case "/items":
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = w.Write([]byte(rssFeedBody))
		case "/rss", "/atom", "/raw", "/guides/feed":
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = w.Write([]byte(rssFeedBody))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	apiSpec := minimalSpec("rss-feed")
	apiSpec.BaseURL = server.URL
	apiSpec.Learn.Disabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"feeds": {
			Description: "Feeds",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:         http.MethodGet,
					Path:           "/rss",
					Description:    "Read the feed",
					ResponseFormat: spec.ResponseFormatXML,
				},
			},
		},
		"atoms": {
			Description: "Atoms",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:         http.MethodGet,
					Path:           "/atom",
					Description:    "Read the atom feed",
					ResponseFormat: spec.ResponseFormatXML,
					HeaderOverrides: []spec.RequiredHeader{{
						Name:  "Accept",
						Value: "application/atom+xml",
					}},
				},
			},
		},
		"rawfeed": {
			Description: "Raw feed",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:         http.MethodGet,
					Path:           "/raw",
					Description:    "Read the feed with a JSON Accept override",
					ResponseFormat: spec.ResponseFormatXML,
					HeaderOverrides: []spec.RequiredHeader{{
						Name:  "Accept",
						Value: "application/json",
					}},
				},
			},
		},
		"items": {
			Description: "Items",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      http.MethodGet,
					Path:        "/items",
					Description: "List items",
				},
			},
		},
		"guides": {
			Description: "Guides",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      http.MethodGet,
					Path:        "/guides",
					Description: "List guides",
				},
				"feed": {
					Method:         http.MethodGet,
					Path:           "/guides/feed",
					Description:    "Guide feed",
					ResponseFormat: spec.ResponseFormatXML,
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, gen.Generate())

	feedsSrc := compactWS(generatedCommandSource(t, outputDir, "/rss"))
	require.Contains(t, feedsSrc, `"Accept": "application/xml"`)
	require.Contains(t, feedsSrc, `"X-Printing-Press-XML-Response": "true"`)

	atomsSrc := compactWS(generatedCommandSource(t, outputDir, "/atom"))
	require.Contains(t, atomsSrc, `"Accept": "application/atom+xml"`)
	require.NotContains(t, atomsSrc, `"Accept": "application/xml"`)
	require.Contains(t, atomsSrc, `"X-Printing-Press-XML-Response": "true"`)

	rawSrc := compactWS(generatedCommandSource(t, outputDir, "/raw"))
	require.Contains(t, rawSrc, `"Accept": "application/json"`)
	require.NotContains(t, rawSrc, `"Accept": "application/xml"`)
	require.Contains(t, rawSrc, `"X-Printing-Press-XML-Response": "true"`)

	itemsSrc := generatedCommandSource(t, outputDir, "/items")
	require.NotContains(t, itemsSrc, `"X-Printing-Press-XML-Response"`)
	require.NotContains(t, itemsSrc, `"Accept":`)

	guideSrc := compactWS(generatedCommandSource(t, outputDir, "/guides/feed"))
	require.Contains(t, guideSrc, `"Accept": "application/xml"`)
	require.Contains(t, guideSrc, `"X-Printing-Press-XML-Response": "true"`)

	toolsSrc := compactWS(readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go"))
	require.Contains(t, toolsSrc, `client.XMLResponseHeader: "true"`)
	require.Contains(t, toolsSrc, `"Accept": "application/atom+xml"`)

	orchSpec := *apiSpec
	orchSpec.MCP.Orchestration = "code"
	orchDir := filepath.Join(t.TempDir(), naming.CLI(orchSpec.Name))
	orchGen := New(&orchSpec, orchDir)
	orchGen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, orchGen.Generate())
	orchSrc := compactWS(readGeneratedFile(t, orchDir, "internal", "mcp", "code_orch.go"))
	require.Contains(t, orchSrc, `"X-Printing-Press-XML-Response": "true"`)
	require.Contains(t, orchSrc, `"Accept": "application/json"`)
	requireGeneratedCompiles(t, orchDir)

	requireGeneratedCompiles(t, outputDir)
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "client", "xml_request_branch_test.go"),
		[]byte(xmlRequestBranchClientTest), 0o644))
	branchOut, branchErr := runGoCommandOutput(t, outputDir, "test", "-v", "./internal/client", "-run", "^TestXMLRequestNormalizationBranches$", "-count=1")
	require.NoError(t, branchErr, branchOut)
	require.Contains(t, branchOut, "--- PASS: TestXMLRequestNormalizationBranches")

	binaryPath := platform.ExecutablePath(filepath.Join(outputDir, naming.CLI(apiSpec.Name)))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))
	env := withoutEnvPrefixes(isolatedCLIEnv(t, t.TempDir()), "PRINTING_PRESS_", "MYAPI_", "RSS_FEED_")
	env = append(env, "MYAPI_TOKEN=test-token", "RSS_FEED_BASE_URL="+server.URL)

	feedsOut, err := runGeneratedCLI(t, binaryPath, env, "feeds", "--json")
	require.NoError(t, err, feedsOut)
	require.Equal(t, []string{"Alpha", "Beta"}, itemTitles(t, feedsOut))
	requireHeader := func(path, accept string) {
		t.Helper()
		mu.Lock()
		header := seen[path]
		mu.Unlock()
		require.Equal(t, accept, header.Get("Accept"), path)
		require.Empty(t, header.Get("X-Printing-Press-XML-Response"), path)
	}
	requireHeader("/rss", "application/xml")

	atomsOut, err := runGeneratedCLI(t, binaryPath, env, "atoms", "--json")
	require.NoError(t, err, atomsOut)
	require.Equal(t, []string{"Alpha", "Beta"}, itemTitles(t, atomsOut))
	requireHeader("/atom", "application/atom+xml")

	rawOut, err := runGeneratedCLI(t, binaryPath, env, "rawfeed", "--json")
	require.NoError(t, err, rawOut)
	require.Equal(t, []string{"Alpha", "Beta"}, itemTitles(t, rawOut))
	requireHeader("/raw", "application/json")

	guideOut, err := runGeneratedCLI(t, binaryPath, env, "guides", "feed", "--json")
	require.NoError(t, err, guideOut)
	require.Equal(t, []string{"Alpha", "Beta"}, itemTitles(t, guideOut))
	requireHeader("/guides/feed", "application/xml")

	itemsOut, err := runGeneratedCLI(t, binaryPath, env, "items", "--json")
	require.Error(t, err, itemsOut)
	requireExitCode(t, err, 5)
	require.Contains(t, itemsOut, "API returned XML (application/rss+xml)")
	require.Contains(t, itemsOut, "declare response_format: xml")
	require.NotContains(t, itemsOut, "HTML document")
	require.NotContains(t, itemsOut, "HTML instead of JSON")
	requireHeader("/items", "application/json")
}

func TestGeneratedJSONClientReportsXMLInsteadOfHTML(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("xml-misdeclare")
	apiSpec.Learn.Disabled = true
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	require.Contains(t, clientSrc, "API returned XML")
	require.Contains(t, clientSrc, "declare response_format: xml")
	require.Contains(t, clientSrc, "func looksLikeXMLDocument")
	require.NotContains(t, clientSrc, "func isXMLRequest")
	require.NotContains(t, clientSrc, "XMLResponseHeader")

	requireGeneratedCompiles(t, outputDir)
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "client", "xml_misdeclare_test.go"),
		[]byte(xmlMisdeclareClientTest), 0o644))
	out, err := runGoCommandOutput(t, outputDir, "test", "-v", "./internal/client", "-run", "^TestJSONSuccessClassifiesXMLAndHTML$", "-count=1")
	require.NoError(t, err, out)
	require.Contains(t, out, "--- PASS: TestJSONSuccessClassifiesXMLAndHTML")
}

func generatedCommandSource(t *testing.T, dir, path string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "internal", "cli", "*.go"))
	require.NoError(t, err)
	needle := `"` + path + `"`
	var found []string
	for _, match := range matches {
		data, err := os.ReadFile(match)
		require.NoError(t, err)
		if strings.Contains(string(data), needle) {
			found = append(found, string(data))
		}
	}
	require.Len(t, found, 1, "command source for %s", path)
	return found[0]
}

func compactWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func withoutEnvPrefixes(env []string, prefixes ...string) []string {
	filtered := make([]string, 0, len(env))
	for _, kv := range env {
		skip := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(kv, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			filtered = append(filtered, kv)
		}
	}
	return filtered
}

func itemTitles(t *testing.T, out string) []string {
	t.Helper()
	raw := out
	if i := strings.IndexByte(out, '{'); i >= 0 {
		raw = out[i:]
	}
	var payload any
	require.NoError(t, json.Unmarshal([]byte(raw), &payload), out)
	found := findJSONKey(payload, "item")
	arr, ok := found.([]any)
	require.Truef(t, ok, "item should be an array, got %#v\n%s", found, out)
	titles := make([]string, 0, len(arr))
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		require.True(t, ok, "item element %#v", item)
		title, ok := obj["title"].(string)
		require.True(t, ok, "item title %#v", obj["title"])
		titles = append(titles, title)
	}
	return titles
}

func findJSONKey(v any, key string) any {
	switch node := v.(type) {
	case map[string]any:
		if child, ok := node[key]; ok {
			return child
		}
		for _, child := range node {
			if found := findJSONKey(child, key); found != nil {
				return found
			}
		}
	case []any:
		for _, child := range node {
			if found := findJSONKey(child, key); found != nil {
				return found
			}
		}
	}
	return nil
}

const xmlRequestBranchClientTest = `package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rss-feed-pp-cli/internal/config"
)

func TestXMLRequestNormalizationBranches(t *testing.T) {
	const rss = "<?xml version='1.0'?><rss><channel><item><title>Alpha</title></item><item><title>Beta</title></item></channel></rss>"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(XMLResponseHeader) != "" {
			t.Errorf("%s leaked onto %s", XMLResponseHeader, r.URL.Path)
		}
		switch r.URL.Path {
		case "/declared":
			if got := r.Header.Get("Accept"); got != "application/json" {
				t.Errorf("declared Accept = %q", got)
			}
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = w.Write([]byte(rss))
		case "/accept":
			if got := r.Header.Get("Accept"); got != "application/xml" {
				t.Errorf("accept Accept = %q", got)
			}
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = w.Write([]byte(rss))
		case "/rss-default":
			if got := r.Header.Get("Accept"); got != "application/json" {
				t.Errorf("default Accept = %q", got)
			}
			w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
			_, _ = w.Write([]byte(rss))
		case "/comma":
			if got := r.Header.Get("Accept"); got != "application/json, application/rss+xml" {
				t.Errorf("comma Accept = %q", got)
			}
			w.Header().Set("Content-Type", "text/xml")
			_, _ = w.Write([]byte("<note><title>Memo</title></note>"))
		case "/prolog":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("<?xml version='1.0'?><feed><title>Nope</title></feed>"))
		case "/doctype":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("<?xml version='1.0'?><!DOCTYPE rss [<!ELEMENT rss ANY>]><rss><title>Feed</title></rss>"))
		case "/missing":
			w.Header().Set("Content-Type", "application/rss+xml")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("<rss><channel><title>Missing</title></channel></rss>"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	c := New(&config.Config{BaseURL: server.URL}, time.Second, 0)
	c.NoCache = true
	ctx := context.Background()

	got, err := c.GetWithHeaders(ctx, "/accept", nil, map[string]string{"Accept": "application/xml"})
	if err != nil {
		t.Fatalf("Accept xml: %v", err)
	}
	if !strings.Contains(string(got), "Alpha") || !strings.Contains(string(got), "Beta") {
		t.Fatalf("Accept xml body = %s", got)
	}

	got, err = c.GetWithHeaders(ctx, "/declared", nil, map[string]string{
		"Accept":          "application/json",
		XMLResponseHeader: "true",
	})
	if err != nil {
		t.Fatalf("declared xml with JSON Accept: %v", err)
	}
	if !strings.Contains(string(got), "\"item\"") || !strings.Contains(string(got), "Alpha") {
		t.Fatalf("declared xml body = %s", got)
	}

	got, err = c.GetWithHeaders(ctx, "/comma", nil, map[string]string{"Accept": "application/json, application/rss+xml"})
	if err != nil {
		t.Fatalf("comma Accept: %v", err)
	}
	if !strings.Contains(string(got), "Memo") {
		t.Fatalf("comma Accept body = %s", got)
	}

	got, err = c.GetWithHeaders(ctx, "/prolog", nil, map[string]string{
		"Accept":          "application/xml",
		XMLResponseHeader: "true",
	})
	if err != nil {
		t.Fatalf("prolog on declared xml: %v", err)
	}
	if strings.Contains(string(got), "<?xml") || !strings.Contains(string(got), "Nope") {
		t.Fatalf("prolog on declared xml body = %s", got)
	}

	got, err = c.GetWithHeaders(ctx, "/doctype", nil, map[string]string{
		"Accept":          "application/xml",
		XMLResponseHeader: "true",
	})
	if err != nil {
		t.Fatalf("doctype subset on declared xml: %v", err)
	}
	if strings.Contains(string(got), "<?xml") || strings.Contains(string(got), "HTML") || !strings.Contains(string(got), "Feed") {
		t.Fatalf("doctype subset body = %s", got)
	}

	_, err = c.Get(ctx, "/rss-default", nil)
	if err == nil || !strings.Contains(err.Error(), "API returned XML (application/rss+xml; charset=utf-8)") || strings.Contains(err.Error(), "HTML") {
		t.Fatalf("undeclared rss error = %v", err)
	}

	_, err = c.Get(ctx, "/missing", nil)
	if err == nil || strings.Contains(err.Error(), "HTML error page") || strings.Contains(err.Error(), "HTML document") {
		t.Fatalf("rss 404 error = %v", err)
	}
}
`

const xmlMisdeclareClientTest = `package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xml-misdeclare-pp-cli/internal/config"
)

func TestJSONSuccessClassifiesXMLAndHTML(t *testing.T) {
	type want struct {
		path    string
		status  int
		ct      string
		body    string
		errHas  string
		errSkip string
	}
	cases := []want{
		{path: "/rss", ct: "application/rss+xml", body: "<rss><channel><title>Feed</title></channel></rss>", errHas: "API returned XML (application/rss+xml)", errSkip: "HTML document"},
		{path: "/charset", ct: "application/rss+xml; charset=utf-8", body: "<rss><channel><title>Feed</title></channel></rss>", errHas: "API returned XML (application/rss+xml; charset=utf-8)", errSkip: "HTML"},
		{path: "/textxml", ct: "text/xml", body: "<note><title>Memo</title></note>", errHas: "API returned XML (text/xml)", errSkip: "HTML document"},
		{path: "/xml", ct: "application/xml", body: "<note><title>Memo</title></note>", errHas: "declare response_format: xml", errSkip: "HTML document"},
		{path: "/prolog", body: "<?xml version='1.0'?><feed><title>Nope</title></feed>", errHas: "API returned XML (unspecified)", errSkip: "HTML document"},
		{path: "/plain", ct: "text/plain", body: "<?xml version='1.0'?><feed><title>Nope</title></feed>", errHas: "API returned XML (text/plain)", errSkip: "HTML document"},
		{path: "/bom", body: "\uFEFF<?xml version='1.0'?><feed><title>Bom</title></feed>", errHas: "API returned XML (unspecified)", errSkip: "HTML document"},
		{path: "/comment", body: "<?xml version='1.0'?><!-- note --><!DOCTYPE rss><rss><title>T</title></rss>", errHas: "API returned XML (unspecified)", errSkip: "HTML document"},
		{path: "/doctype-subset", body: "<?xml version='1.0'?><!DOCTYPE rss [<!ELEMENT rss ANY>]><rss><title>Feed</title></rss>", errHas: "API returned XML (unspecified)", errSkip: "HTML document"},
		{path: "/doctype-quote", body: "<?xml version='1.0'?><!DOCTYPE rss SYSTEM 'rss>feed.dtd'><rss><title>Quoted</title></rss>", errHas: "API returned XML (unspecified)", errSkip: "HTML document"},
		{path: "/html", ct: "text/html; charset=utf-8", body: "<!doctype html><html><head><title>Sign in</title></head><body></body></html>", errHas: "returned HTML instead of JSON", errSkip: "API returned XML"},
		{path: "/xhtml", ct: "application/xhtml+xml", body: "<?xml version='1.0'?><html><head><title>Page</title></head></html>", errHas: "returned HTML instead of JSON", errSkip: "API returned XML"},
		{path: "/xhtml-root", body: "<?xml version='1.0'?><!-- c --><html><title>Page</title></html>", errHas: "returned HTML instead of JSON", errSkip: "API returned XML"},
		{path: "/styled", ct: "text/html", body: "<?xml-stylesheet type='text/xsl' href='a.xsl'?><rss><title>Styled</title></rss>", errHas: "returned HTML instead of JSON", errSkip: "API returned XML"},
		{path: "/missing", status: http.StatusNotFound, ct: "application/rss+xml", body: "<rss><channel><title>Missing</title></channel></rss>", errHas: "HTTP 404", errSkip: "HTML error page"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, tc := range cases {
			if r.URL.Path != tc.path {
				continue
			}
			w.Header().Set("Content-Type", tc.ct)
			if tc.status != 0 {
				w.WriteHeader(tc.status)
			}
			_, _ = w.Write([]byte(tc.body))
			return
		}
		if r.URL.Path == "/json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{\"ok\":true}"))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	c := New(&config.Config{BaseURL: server.URL}, time.Second, 0)
	c.NoCache = true
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			_, err := c.Get(context.Background(), tc.path, nil)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.errHas) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.errHas)
			}
			if tc.errSkip != "" && strings.Contains(err.Error(), tc.errSkip) {
				t.Fatalf("error %q contains %q", err.Error(), tc.errSkip)
			}
		})
	}

	got, err := c.Get(context.Background(), "/json", nil)
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	if string(got) != "{\"ok\":true}" {
		t.Fatalf("json body = %s", got)
	}
}
`
