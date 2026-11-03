package generator

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/stretchr/testify/require"
)

func TestGeneratedOpenAPICursorPreservesDateFilters(t *testing.T) {
	api, err := openapi.Parse([]byte(`openapi: 3.0.3
info: {title: Events, version: "1.0"}
servers: [{url: https://events.example}]
paths:
  /events:
    get:
      operationId: listEvents
      parameters:
        - {name: after, in: query, schema: {type: string}}
        - {name: before, in: query, schema: {type: string}}
        - {name: cursor, in: query, schema: {type: string}}
      responses:
        "200":
          description: Page of events
          content:
            application/json:
              schema:
                type: object
                properties:
                  data: {type: array, items: {type: object, properties: {id: {type: string}}}}
                  next_cursor: {type: string}
`))
	require.NoError(t, err)
	api.Learn.Disabled = true
	outputDir, binary := buildGeneratedBinary(t, api)
	requireGeneratedCompiles(t, outputDir)

	var queries []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.Query())
		w.Header().Set("Content-Type", "application/json")
		if len(queries) == 1 {
			fmt.Fprint(w, `{"data":[{"id":"first-event"}],"next_cursor":"page-two"}`)
		} else {
			fmt.Fprint(w, `{"data":[{"id":"second-event"}],"next_cursor":""}`)
		}
	}))
	t.Cleanup(server.Close)
	testHome := t.TempDir()
	cmd := exec.Command(binary, "events", "--all", "--after", "2026-01-01", "--before", "2026-02-01", "--json")
	cmd.Env = []string{
		"HOME=" + testHome,
		"USERPROFILE=" + testHome,
		"XDG_CONFIG_HOME=" + filepath.Join(testHome, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(testHome, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(testHome, ".local", "state"),
		"XDG_CACHE_HOME=" + filepath.Join(testHome, ".cache"),
		naming.EnvPrefix(api.Name) + "_BASE_URL=" + server.URL,
	}
	stdout, err := cmd.Output()
	require.NoError(t, err)
	server.Close()
	require.Equal(t, []url.Values{
		{"after": {"2026-01-01"}, "before": {"2026-02-01"}},
		{"after": {"2026-01-01"}, "before": {"2026-02-01"}, "cursor": {"page-two"}},
	}, queries)
	require.Contains(t, string(stdout), "first-event")
	require.Contains(t, string(stdout), "second-event")
}
