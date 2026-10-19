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
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// labels, events, and Bounces are not keys unwrapSingleKeyArray strips
// (results/data/items/nodes/entries/records). Printing only those arrays
// is the parser's ResponsePath, not the runtime single-key helper.
func TestGeneratedSingleArrayRecordPrintsFullObject(t *testing.T) {
	t.Parallel()

	const specYAML = `openapi: "3.0.3"
info:
  title: Single Array Records
  version: "1.0.0"
servers:
  - url: https://api.example.com
paths:
  /servers/{id}:
    get:
      operationId: getServer
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  Name: {type: string}
                  ID: {type: integer}
                  Color: {type: string}
                  ApiTokens:
                    type: array
                    items: {type: string}
  /admins/{id}:
    get:
      operationId: getAdmin
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  type: {type: string}
                  id: {type: string}
                  name: {type: string}
                  email: {type: string}
                  team_ids:
                    type: array
                    items: {type: string}
  /messages/{id}:
    get:
      operationId: getMessage
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  id: {type: string}
                  threadId: {type: string}
                  snippet: {type: string}
                  labelIds:
                    type: array
                    items: {type: string}
  /resource-pools/{poolId}:
    get:
      operationId: getResourcePool
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  name: {type: string}
                  cpu_allocation:
                    type: object
                    properties:
                      limit: {type: integer}
                  memory_allocation:
                    type: object
                    properties:
                      limit: {type: integer}
                  resource_pools:
                    type: array
                    items:
                      type: object
                      properties:
                        id: {type: string}
  /device-commands/{commandId}:
    get:
      operationId: getDeviceCommand
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  commandId: {type: string}
                  errors:
                    type: array
                    items:
                      type: object
                      properties:
                        id: {type: string}
                        message: {type: string}
  /labels:
    get:
      operationId: listLabels
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  labels:
                    type: array
                    items:
                      type: object
                      properties:
                        id: {type: string}
                        name: {type: string}
  /events:
    get:
      operationId: listEvents
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  events:
                    type: array
                    items:
                      type: object
                      properties:
                        event_ticker: {type: string}
                  cursor: {type: string}
  /bounces:
    get:
      operationId: listBounces
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  TotalCount: {type: integer}
                  Bounces:
                    type: array
                    items:
                      type: object
                      properties:
                        id: {type: string}
  /items:
    get:
      operationId: listItems
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  items:
                    type: array
                    items:
                      type: object
                      properties:
                        sku: {type: string}
                  next_cursor: {type: string}
`

	bodies := map[string]string{
		"/servers/srv-1":         `{"Name":"prod","ID":42,"Color":"blue","ApiTokens":["tok-a","tok-b"]}`,
		"/admins/a1":             `{"type":"admin","id":"a1","name":"Ada","email":"ada@example.com","team_ids":["t1","t2"]}`,
		"/messages/m1":           `{"id":"m1","threadId":"th1","snippet":"hello","labelIds":["INBOX","UNREAD"]}`,
		"/resource-pools/pool-1": `{"name":"pool-a","cpu_allocation":{"limit":100},"memory_allocation":{"limit":200},"resource_pools":[{"id":"child"}]}`,
		"/device-commands/cmd-9": `{"commandId":"cmd-9","errors":[{"id":"e1","message":"nope"}]}`,
		"/labels":                `{"labels":[{"id":"INBOX","name":"Inbox"}]}`,
		"/events":                `{"events":[{"event_ticker":"EVT"}],"cursor":"next"}`,
		"/bounces":               `{"TotalCount":1,"Bounces":[{"id":"b1"}]}`,
		"/items":                 `{"items":[{"sku":"sku-1"}],"next_cursor":"more"}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := bodies[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	apiSpec, err := openapi.Parse([]byte(specYAML))
	require.NoError(t, err)
	apiSpec.Learn.Disabled = true
	apiSpec.BaseURL = server.URL

	for _, path := range []string{
		"/servers/{id}",
		"/admins/{id}",
		"/messages/{id}",
		"/resource-pools/{poolId}",
		"/device-commands/{commandId}",
	} {
		ep, ok := endpointByPath(apiSpec, path)
		require.Truef(t, ok, "missing %s", path)
		require.Equal(t, "object", ep.Response.Type, path)
		require.Empty(t, ep.ResponsePath, path)
	}
	for path, wantPath := range map[string]string{
		"/labels":  "labels",
		"/events":  "events",
		"/bounces": "Bounces",
		"/items":   "items",
	} {
		ep, ok := endpointByPath(apiSpec, path)
		require.Truef(t, ok, "missing %s", path)
		require.Equal(t, "array", ep.Response.Type, path)
		require.Equal(t, wantPath, ep.ResponsePath, path)
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	for _, goos := range []string{"darwin", "windows"} {
		runGoCommandWithEnv(t, outputDir, []string{"GOOS=" + goos, "CGO_ENABLED=0"}, "build", "./...")
	}

	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))

	home := t.TempDir()
	env := append(os.Environ(),
		"HOME="+home,
		"USERPROFILE="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
		naming.EnvPrefix(apiSpec.Name)+"_BASE_URL="+server.URL,
	)

	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "server record keeps ApiTokens",
			args: []string{"servers", "srv-1", "--json"},
			want: bodies["/servers/srv-1"],
		},
		{
			name: "admin record keeps team_ids",
			args: []string{"admins", "a1", "--json"},
			want: bodies["/admins/a1"],
		},
		{
			name: "message record keeps labelIds",
			args: []string{"messages", "m1", "--json"},
			want: bodies["/messages/m1"],
		},
		{
			name: "resource pool without id keeps child pools",
			args: []string{"resource-pools", "pool-1", "--json"},
			want: bodies["/resource-pools/pool-1"],
		},
		{
			name: "command id record keeps errors",
			args: []string{"device-commands", "cmd-9", "--json"},
			want: bodies["/device-commands/cmd-9"],
		},
		{
			name: "labels envelope unwraps",
			args: []string{"labels", "--json"},
			want: `[{"id":"INBOX","name":"Inbox"}]`,
		},
		{
			name: "events envelope unwraps",
			args: []string{"events", "--json"},
			want: `[{"event_ticker":"EVT"}]`,
		},
		{
			name: "bounces envelope unwraps",
			args: []string{"bounces", "--json"},
			want: `[{"id":"b1"}]`,
		},
		{
			name: "items envelope unwraps",
			args: []string{"items", "--json"},
			want: `[{"sku":"sku-1"}]`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout := runGeneratedStdout(t, binaryPath, env, tc.args...)
			require.JSONEq(t, tc.want, printedJSONPayload(t, stdout))
		})
	}
}

func endpointByPath(apiSpec *spec.APISpec, path string) (spec.Endpoint, bool) {
	if apiSpec == nil {
		return spec.Endpoint{}, false
	}
	for _, resource := range apiSpec.Resources {
		if ep, ok := endpointByPathResource(resource, path); ok {
			return ep, true
		}
	}
	return spec.Endpoint{}, false
}

func endpointByPathResource(resource spec.Resource, path string) (spec.Endpoint, bool) {
	for _, ep := range resource.Endpoints {
		if ep.Path == path {
			return ep, true
		}
	}
	for _, sub := range resource.SubResources {
		if ep, ok := endpointByPathResource(sub, path); ok {
			return ep, true
		}
	}
	return spec.Endpoint{}, false
}

func runGeneratedStdout(t *testing.T, binaryPath string, env []string, args ...string) string {
	t.Helper()
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Run(), "args: %v\nstdout:\n%s\nstderr:\n%s", args, stdout.String(), stderr.String())
	return stdout.String()
}

func printedJSONPayload(t *testing.T, stdout string) string {
	t.Helper()
	stdout = strings.TrimSpace(stdout)
	require.NotEmpty(t, stdout)
	var raw json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(stdout), &raw), stdout)
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err == nil {
		if _, hasMeta := obj["meta"]; hasMeta {
			if results, ok := obj["results"]; ok {
				return string(results)
			}
		}
	}
	return string(raw)
}
