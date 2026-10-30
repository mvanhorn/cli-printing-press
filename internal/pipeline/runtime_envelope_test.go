package pipeline

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const envelopeListSpec = `openapi: 3.0.3
info:
  title: Envelope Fixture
  version: "1.0.0"
servers:
  - url: https://api.example.com
paths:
  /bounces:
    get:
      operationId: listBounces
      x-pp-resource: bounces
      parameters:
        - name: offset
          in: query
          schema:
            type: integer
            default: 0
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  TotalCount:
                    type: integer
                  hasMore:
                    type: boolean
                    default: true
                  Bounces:
                    type: array
                    items:
                      type: object
                      properties:
                        id:
                          type: integer
                        name:
                          type: string
  /activityType:
    get:
      operationId: listDeals
      x-pp-resource: deals
      parameters:
        - name: offset
          in: query
          schema:
            type: integer
            default: 0
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  success:
                    type: boolean
                    default: false
                  data:
                    type: array
                    items:
                      type: object
                      properties:
                        id:
                          type: integer
                        name:
                          type: string
  /widgets:
    get:
      operationId: listWidgets
      x-pp-resource: widgets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  success:
                    type: boolean
                  data:
                    type: object
                    properties:
                      items:
                        type: array
                        items:
                          type: object
                      pagination:
                        type: object
                        properties:
                          total:
                            type: integer
  /tags:
    get:
      operationId: listTags
      x-pp-resource: tags
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
                  properties:
                    id:
                      type: integer
                    name:
                      type: string
  /feeds:
    get:
      operationId: listFeeds
      x-pp-resource: feeds
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  items:
                    type: array
                    items:
                      type: object
                  errors:
                    type: array
                    items:
                      type: object
  /blobs:
    get:
      operationId: listBlobs
      x-pp-resource: blobs
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                additionalProperties:
                  type: string
  /account:
    get:
      operationId: getAccount
      x-pp-resource: account
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  id:
                    type: integer
                  name:
                    type: string
`

func TestStartMockServerRendersDeclaredListEnvelopes(t *testing.T) {
	t.Parallel()

	spec := loadEnvelopeSpec(t, envelopeListSpec)
	require.Equal(t, responseFixtureTopLevel, spec.NestedDataEnvelopes[responseFixtureKey{Method: http.MethodGet, Path: "/bounces"}].Kind)
	require.Equal(t, responseFixtureDataArray, spec.NestedDataEnvelopes[responseFixtureKey{Method: http.MethodGet, Path: "/activityType"}].Kind)
	require.Equal(t, responseFixtureNestedDataObject, spec.NestedDataEnvelopes[responseFixtureKey{Method: http.MethodGet, Path: "/widgets"}].Kind)
	_, hasTags := spec.NestedDataEnvelopes[responseFixtureKey{Method: http.MethodGet, Path: "/tags"}]
	_, hasFeeds := spec.NestedDataEnvelopes[responseFixtureKey{Method: http.MethodGet, Path: "/feeds"}]
	_, hasBlobs := spec.NestedDataEnvelopes[responseFixtureKey{Method: http.MethodGet, Path: "/blobs"}]
	_, hasAccount := spec.NestedDataEnvelopes[responseFixtureKey{Method: http.MethodGet, Path: "/account"}]
	assert.False(t, hasTags)
	assert.False(t, hasFeeds)
	assert.False(t, hasBlobs)
	assert.False(t, hasAccount)

	server, baseURL := startMockServer(spec)
	t.Cleanup(server.Close)

	bounces := asObject(t, fetchMock(t, baseURL, http.MethodGet, "/bounces"))
	assert.Equal(t, float64(2), bounces["TotalCount"])
	assert.Equal(t, false, bounces["hasMore"])
	assert.Len(t, asArray(t, bounces["Bounces"]), 2)

	deals := asObject(t, fetchMock(t, baseURL, http.MethodGet, "/activityType"))
	assert.Equal(t, true, deals["success"])
	assert.Len(t, asArray(t, deals["data"]), 2)

	widgets := asObject(t, fetchMock(t, baseURL, http.MethodGet, "/widgets"))
	assert.Equal(t, true, widgets["success"])
	data := asObject(t, widgets["data"])
	assert.Len(t, asArray(t, data["items"]), 2)
	assert.Equal(t, float64(2), asObject(t, data["pagination"])["total"])

	assert.Len(t, asArray(t, fetchMock(t, baseURL, http.MethodGet, "/tags")), 1)
	assert.Len(t, asArray(t, fetchMock(t, baseURL, http.MethodGet, "/feeds")), 1)
	assert.Len(t, asArray(t, fetchMock(t, baseURL, http.MethodGet, "/blobs")), 1)

	account := asObject(t, fetchMock(t, baseURL, http.MethodGet, "/account"))
	assert.Equal(t, "mock-item", account["name"])
	_, hasBounces := account["Bounces"]
	assert.False(t, hasBounces)
}

func TestStartMockServerKeepsMethodSpecificEnvelope(t *testing.T) {
	t.Parallel()

	spec := loadEnvelopeSpec(t, `openapi: 3.0.3
info:
  title: Mixed Method Envelope
  version: "1.0.0"
paths:
  /items:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
    post:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  total:
                    type: integer
                  items:
                    type: array
                    items:
                      type: object
`)
	_, getFixture := spec.NestedDataEnvelopes[responseFixtureKey{Method: http.MethodGet, Path: "/items"}]
	postFixture, postOK := spec.NestedDataEnvelopes[responseFixtureKey{Method: http.MethodPost, Path: "/items"}]
	assert.False(t, getFixture)
	require.True(t, postOK)
	assert.Equal(t, "items", postFixture.ArrayKey)

	server, baseURL := startMockServer(spec)
	t.Cleanup(server.Close)

	assert.Len(t, asArray(t, fetchMock(t, baseURL, http.MethodGet, "/items")), 1)
	posted := asObject(t, fetchMock(t, baseURL, http.MethodPost, "/items"))
	assert.Equal(t, float64(2), posted["total"])
	assert.Len(t, asArray(t, posted["items"]), 2)
}

func TestVerifyParsesDeclaredListEnvelopes(t *testing.T) {
	apiSpec, err := openapi.Parse([]byte(envelopeListSpec))
	require.NoError(t, err)
	dir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, generator.New(apiSpec, dir).Generate())
	tidy := exec.Command("go", "mod", "tidy")
	tidy.Dir = dir
	if out, err := tidy.CombinedOutput(); err != nil {
		t.Fatalf("go mod tidy failed:\n%s", out)
	}
	specPath := filepath.Join(t.TempDir(), "spec.yaml")
	writeTestFile(t, specPath, envelopeListSpec)

	report, err := RunVerify(VerifyConfig{Dir: dir, SpecPath: specPath})
	require.NoError(t, err)
	require.Equal(t, "mock", report.Mode)
	for _, name := range []string{"bounces", "deals", "widgets", "tags", "feeds", "blobs", "account"} {
		result := commandResult(t, report, name)
		assert.Truef(t, result.Help && result.DryRun && result.Execute, "%s score=%d err=%s", name, result.Score, result.Error)
		assert.Equal(t, 3, result.Score, name)
	}
	require.Truef(t, report.DataPipeline, "data pipeline: %s", report.DataPipelineDetail)

	spec := loadEnvelopeSpec(t, envelopeListSpec)
	server, baseURL := startMockServer(spec)
	t.Cleanup(server.Close)
	env := envelopeCLIEnv(t, dir, baseURL)

	bounces := asArray(t, cliResults(t, report.Binary, env, "bounces"))
	assert.Len(t, bounces, 2)
	assert.Equal(t, "mock-item-1", asObject(t, bounces[0])["name"])

	deals := asArray(t, cliResults(t, report.Binary, env, "deals"))
	assert.Len(t, deals, 2)
	assert.Equal(t, "mock-item-1", asObject(t, deals[0])["name"])

	widgets := asObject(t, cliResults(t, report.Binary, env, "widgets"))
	widgetData := asObject(t, widgets["data"])
	assert.Len(t, asArray(t, widgetData["items"]), 2)
	assert.Equal(t, float64(2), asObject(t, widgetData["pagination"])["total"])

	assert.Len(t, asArray(t, cliResults(t, report.Binary, env, "tags")), 1)
	feeds := asArray(t, cliResults(t, report.Binary, env, "feeds"))
	assert.Len(t, feeds, 1)
	_, feedIsObject := cliResults(t, report.Binary, env, "feeds").(map[string]any)
	assert.False(t, feedIsObject)
	assert.Len(t, asArray(t, cliResults(t, report.Binary, env, "blobs")), 1)

	account := asObject(t, cliResults(t, report.Binary, env, "account"))
	assert.Equal(t, "mock-item", account["name"])
}

func loadEnvelopeSpec(t *testing.T, specYAML string) *openAPISpec {
	t.Helper()
	specPath := filepath.Join(t.TempDir(), "spec.yaml")
	writeTestFile(t, specPath, specYAML)
	spec, err := loadDogfoodOpenAPISpec(specPath, "")
	require.NoError(t, err)
	return spec
}

func fetchMock(t *testing.T, baseURL, method, path string) any {
	t.Helper()
	req, err := http.NewRequest(method, baseURL+path, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	var body any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	return body
}

func asObject(t *testing.T, body any) map[string]any {
	t.Helper()
	obj, ok := body.(map[string]any)
	require.Truef(t, ok, "got %#v", body)
	return obj
}

func asArray(t *testing.T, body any) []any {
	t.Helper()
	items, ok := body.([]any)
	require.Truef(t, ok, "got %#v", body)
	return items
}

func commandResult(t *testing.T, report *VerifyReport, name string) CommandResult {
	t.Helper()
	for _, result := range report.Results {
		if result.Command == name {
			return result
		}
	}
	lines := make([]string, 0, len(report.Results))
	for _, result := range report.Results {
		lines = append(lines, result.Command)
	}
	t.Fatalf("command %q missing from verify report: %s", name, strings.Join(lines, ", "))
	return CommandResult{}
}

func envelopeCLIEnv(t *testing.T, dir, baseURL string) []string {
	t.Helper()
	home := t.TempDir()
	env := subprocessEnv()
	env = putEnv(env, "HOME", home)
	env = putEnv(env, "USERPROFILE", home)
	env = putEnv(env, "XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	env = putEnv(env, "XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	env = putEnv(env, "XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	env = putEnv(env, "XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	env = putEnv(env, verifyEnvPrefix(dir)+"_BASE_URL", baseURL)
	env = putEnv(env, "PRINTING_PRESS_VERIFY", "1")
	env = putEnv(env, "PRINTING_PRESS_VERIFY_LIVE_HTTP", "1")
	return env
}

func cliResults(t *testing.T, binary string, env []string, args ...string) any {
	t.Helper()
	cmd := exec.Command(binary, append(args, "--json")...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoErrorf(t, cmd.Run(), "stderr:\n%s\nstdout:\n%s", stderr.String(), stdout.String())
	var payload map[string]any
	require.NoErrorf(t, json.Unmarshal(stdout.Bytes(), &payload), "stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	results, ok := payload["results"]
	require.Truef(t, ok, "stdout:\n%s", stdout.String())
	return results
}
