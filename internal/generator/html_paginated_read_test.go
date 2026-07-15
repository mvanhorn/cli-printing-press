package generator

import (
	"encoding/json"
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

func TestGeneratedPaginatedReadHonorsResponseFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><a href="/posts/one">One</a></body></html>`))
	}))
	t.Cleanup(server.Close)

	apiSpec := minimalSpec("html-paginated-read")
	apiSpec.BaseURL = server.URL
	apiSpec.Resources = map[string]spec.Resource{
		"html_posts": {
			Description: "HTML posts",
			Endpoints: map[string]spec.Endpoint{
				"list": paginatedReadEndpoint(spec.ResponseFormatHTML),
				"page": {
					Method:         "GET",
					Path:           "/posts",
					Description:    "Read an HTML page",
					ResponseFormat: spec.ResponseFormatHTML,
					HTMLExtract:    &spec.HTMLExtract{Mode: spec.HTMLExtractModePage},
				},
			},
		},
		"promoted_html_posts": {
			Description: "Promoted HTML posts",
			Endpoints: map[string]spec.Endpoint{
				"list": paginatedReadEndpoint(spec.ResponseFormatHTML),
			},
		},
		"json_posts": {
			Description: "JSON posts",
			Endpoints: map[string]spec.Endpoint{
				"list": paginatedReadEndpoint(spec.ResponseFormatJSON),
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Search: true, MCP: true}
	require.NoError(t, gen.Generate())
	requireGeneratedCompiles(t, outputDir)

	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))

	baseEnv := append(os.Environ(), strings.ToUpper(strings.ReplaceAll(apiSpec.Name, "-", "_"))+"_BASE_URL="+server.URL)
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "nested live", args: []string{"html-posts", "list", "--json", "--data-source", "live"}},
		{name: "nested auto", args: []string{"html-posts", "list", "--json"}},
		{name: "promoted live", args: []string{"promoted-html-posts", "--json", "--data-source", "live"}},
		{name: "promoted auto", args: []string{"promoted-html-posts", "--json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(binaryPath, tc.args...)
			cmd.Env = baseEnv
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))
			var envelope struct {
				Results []map[string]any `json:"results"`
			}
			require.NoError(t, json.Unmarshal(out, &envelope), string(out))
			require.Len(t, envelope.Results, 1)
			require.Equal(t, "One", envelope.Results[0]["name"])
		})
	}

	for _, strategyArgs := range [][]string{{"--data-source", "live"}, nil} {
		args := append([]string{"json-posts", "--json"}, strategyArgs...)
		cmd := exec.Command(binaryPath, args...)
		cmd.Env = baseEnv
		out, err := cmd.CombinedOutput()
		require.Error(t, err, string(out))
		require.Contains(t, string(out), "returned HTML instead of JSON")
	}

	cmd := exec.Command(binaryPath, "html-posts", "list", "--json", "--all")
	cmd.Env = baseEnv
	out, err := cmd.CombinedOutput()
	require.Error(t, err, string(out))
	require.Contains(t, string(out), "--all is not supported for live HTML responses")

	cmd = exec.Command(binaryPath, "html-posts", "list", "--json", "--all", "--data-source", "local")
	cmd.Env = baseEnv
	out, err = cmd.CombinedOutput()
	require.Error(t, err, string(out))
	require.NotContains(t, string(out), "--all is not supported for live HTML responses")
	require.Contains(t, string(out), "no local data")

	storelessSpec := *apiSpec
	storelessSpec.Learn.Disabled = true
	storelessDir := filepath.Join(t.TempDir(), naming.CLI(storelessSpec.Name))
	storelessGen := New(&storelessSpec, storelessDir)
	storelessGen.VisionSet = VisionTemplateSet{MCP: true}
	require.NoError(t, storelessGen.Generate())
	requireGeneratedCompiles(t, storelessDir)
	storelessBinary := filepath.Join(storelessDir, naming.CLI(storelessSpec.Name))
	runGoCommand(t, storelessDir, "build", "-o", storelessBinary, "./cmd/"+naming.CLI(storelessSpec.Name))
	for _, args := range [][]string{
		{"html-posts", "list", "--json", "--all"},
		{"promoted-html-posts", "--json", "--all"},
	} {
		cmd = exec.Command(storelessBinary, args...)
		cmd.Env = baseEnv
		out, err = cmd.CombinedOutput()
		require.Error(t, err, string(out))
		require.Contains(t, string(out), "--all is not supported for live HTML responses")
	}

	cacheServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><a href="/posts/one">One</a></body></html>`))
	}))
	cacheEnv := append(os.Environ(), strings.ToUpper(strings.ReplaceAll(apiSpec.Name, "-", "_"))+"_BASE_URL="+cacheServer.URL)
	cacheArgs := []string{"html-posts", "list", "--json", "--home", t.TempDir()}
	cacheCmd := exec.Command(binaryPath, cacheArgs...)
	cacheCmd.Env = cacheEnv
	out, err = cacheCmd.CombinedOutput()
	require.NoError(t, err, string(out))
	cacheServer.Close()

	cacheCmd = exec.Command(binaryPath, cacheArgs...)
	cacheCmd.Env = cacheEnv
	out, err = cacheCmd.CombinedOutput()
	require.NoError(t, err, string(out))
	var cachedEnvelope struct {
		Results []map[string]any `json:"results"`
	}
	require.NoError(t, json.Unmarshal(out, &cachedEnvelope), string(out))
	require.Len(t, cachedEnvelope.Results, 1)
	require.Equal(t, "One", cachedEnvelope.Results[0]["name"])
}

func paginatedReadEndpoint(responseFormat string) spec.Endpoint {
	endpoint := spec.Endpoint{
		Method:         "GET",
		Path:           "/posts",
		Description:    "List posts",
		ResponseFormat: responseFormat,
		Response:       spec.ResponseDef{Type: "array", Item: "html_link"},
		Params: []spec.Param{
			{Name: "limit", Type: "integer", Default: 10},
			{Name: "offset", Type: "integer"},
		},
		Pagination: &spec.Pagination{
			Type:        "offset",
			CursorParam: "offset",
			LimitParam:  "limit",
		},
	}
	if responseFormat == spec.ResponseFormatHTML {
		endpoint.HTMLExtract = &spec.HTMLExtract{
			Mode:         spec.HTMLExtractModeLinks,
			LinkPrefixes: []string{"/posts"},
		}
	}
	return endpoint
}
