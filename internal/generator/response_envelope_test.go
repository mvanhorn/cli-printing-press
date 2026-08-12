package generator

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/graphql"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedClientResponseEnvelopeUnwrapIsOptIn(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name              string
		responseEnvelope  string
		wantHelper        bool
		wantInnerResponse bool
	}{
		{name: "enabled", responseEnvelope: "result", wantHelper: true, wantInnerResponse: true},
		{name: "disabled", wantHelper: false, wantInnerResponse: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			apiSpec := minimalSpec("response-envelope-" + tc.name)
			apiSpec.Auth = spec.AuthConfig{Type: "none"}
			apiSpec.ResponseEnvelopeKey = tc.responseEnvelope
			outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
			require.NoError(t, New(apiSpec, outputDir).Generate())

			clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
			if tc.wantHelper {
				assert.Contains(t, clientSrc, "func unwrapResponseEnvelope(")
				assert.Contains(t, clientSrc, "unwrapResponseEnvelope(sanitizeJSONResponse(respBody))")
			} else {
				assert.NotContains(t, clientSrc, "func unwrapResponseEnvelope(")
				assert.NotContains(t, clientSrc, "unwrapResponseEnvelope(")
			}

			modulePath := naming.CLI(apiSpec.Name)
			behaviorTest := fmt.Sprintf(`package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	%q
)

func TestConfiguredResponseEnvelopeShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`+"`"+`{"result":{"items":[{"id":"one"}]}}`+"`"+`))
	}))
	defer server.Close()

	c := New(&config.Config{BaseURL: server.URL}, time.Second, 0)
	c.NoCache = true
	data, err := c.Get(context.Background(), "/items", nil)
	if err != nil {
		t.Fatalf("Get: %%v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode response: %%v; body=%%s", err, data)
	}
	_, hasResult := got["result"]
	_, hasItems := got["items"]
	if hasResult != %t || hasItems != %t {
		t.Fatalf("response keys = %%v, want result=%%t items=%%t; body=%%s", got, %t, %t, data)
	}
}

func TestConfiguredResponseEnvelopeRequiresSingleKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`+"`"+`{"result":{"items":[{"id":"one"}]},"meta":{"request_id":"req-1"}}`+"`"+`))
	}))
	defer server.Close()

	c := New(&config.Config{BaseURL: server.URL}, time.Second, 0)
	c.NoCache = true
	data, err := c.Get(context.Background(), "/items", nil)
	if err != nil {
		t.Fatalf("Get: %%v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode response: %%v; body=%%s", err, data)
	}
	if _, ok := got["result"]; !ok {
		t.Fatalf("result wrapper was removed from a multi-key object: %%s", data)
	}
	if _, ok := got["meta"]; !ok {
		t.Fatalf("metadata was removed from a multi-key object: %%s", data)
	}
}

`, modulePath+"/internal/config", !tc.wantInnerResponse, tc.wantInnerResponse, !tc.wantInnerResponse, tc.wantInnerResponse)
			require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "response_envelope_test.go"), []byte(behaviorTest), 0o644))
			runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestConfiguredResponseEnvelope", "-count=1")
		})
	}
}

func TestGeneratedGraphQLDoesNotApplyRESTResponseUnwrap(t *testing.T) {
	t.Parallel()

	gqlSpec, err := graphql.ParseSDL(filepath.Join("..", "..", "testdata", "graphql", "test.graphql"))
	require.NoError(t, err)
	require.True(t, isGraphQLSpec(gqlSpec), "fixture must be recognized as GraphQL")
	gqlSpec.ResponseEnvelopeKey = "data"

	outputDir := filepath.Join(t.TempDir(), naming.CLI(gqlSpec.Name))
	require.NoError(t, New(gqlSpec, outputDir).Generate())

	clientSrc := readGeneratedFile(t, outputDir, "internal", "client", "client.go")
	assert.NotContains(t, clientSrc, "func unwrapResponseEnvelope(")
	assert.NotContains(t, clientSrc, "unwrapResponseEnvelope(")
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedSyncUsesNormalizedResponseEnvelope(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a generated binary; runs in the full generated-test CI lane")
	}
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("pageToken") == "page-2" {
			_, _ = w.Write([]byte(`{"result":{"services":[{"id":"two","name":"worker"}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"result":{"services":[{"id":"one","name":"web"}],"nextPageToken":"page-2"}}`))
	}))
	t.Cleanup(server.Close)

	apiSpec := minimalSpec("response-envelope-sync")
	apiSpec.BaseURL = server.URL
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.ResponseEnvelopeKey = "result"
	apiSpec.Resources = map[string]spec.Resource{
		"services": {
			Description: "Manage services",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/services",
					Description: "List services",
					IDField:     "id",
					Pagination: &spec.Pagination{
						Type:           "cursor",
						CursorParam:    "pageToken",
						NextCursorPath: "nextPageToken",
					},
					Response: spec.ResponseDef{Type: "array", Item: "Service"},
				},
			},
		},
	}

	slug := naming.CLI(apiSpec.Name)
	outputDir := filepath.Join(t.TempDir(), slug)
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true}
	require.NoError(t, gen.Generate())

	binaryPath := filepath.Join(outputDir, slug)
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+slug)
	dbPath := filepath.Join(t.TempDir(), "sync.db")
	cmd := exec.Command(binaryPath, "--json", "sync", "--resources", "services", "--db", dbPath)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	assert.Contains(t, string(out), `"resource":"services"`)
	assert.Contains(t, string(out), `"total":2`, "sync should see both pages after client envelope normalization")
	assert.NotContains(t, string(out), "nextPageToken", "consumed cursor metadata should not leak into sync output")
}
