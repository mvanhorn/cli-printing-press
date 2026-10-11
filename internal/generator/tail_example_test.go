package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/shellargs"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTailPollResourcesUsesLexicalSyncableRead(t *testing.T) {
	t.Parallel()

	api := &spec.APISpec{
		Name: "tail-pick",
		Resources: map[string]spec.Resource{
			"Invoices": tailListResource("/invoices"),
			"widgets":  tailListResource("/widgets"),
			"alpha":    tailListResource("/parents/{id}/alpha"),
			"zeta":     tailListResource("/zeta"),
		},
	}
	data := visionRenderData{
		APISpec: api,
		SyncableResources: []profiler.SyncableResource{
			{Name: "zeta", Path: "/zeta", Method: "GET"},
			{Name: "alpha", Path: "/parents/{id}/alpha", Method: "GET"},
			{Name: "invoices", Path: "/invoices", Method: "GET"},
			{Name: "widgets", Path: "/widgets", Method: "GET"},
			{Name: "pages", Path: "/pages", Method: "GET", ResponseFormat: spec.ResponseFormatHTML, UsesHTMLResponse: true},
		},
	}
	api.Resources["pages"] = tailListResource("/pages")

	assert.Equal(t, []string{"Invoices", "widgets", "zeta"}, tailPollResources(data))
	assert.Equal(t, "Invoices", tailPrimaryResource(data))
	assert.Equal(t, "resource=Invoices;--follow=false", tailHappyArgs(data))

	example := tailCommandExample(data)
	first := firstTailExampleLine(example)
	args, err := shellargs.ArgsAfterBinary(first)
	require.NoError(t, err)
	assert.Equal(t, []string{"tail", "Invoices", "--follow=false"}, args)
	assert.NotContains(t, example, "messages")
	assert.NotContains(t, example, "events")
}

func TestTailExampleWithoutPollTargetIsDiscovery(t *testing.T) {
	t.Parallel()

	data := visionRenderData{
		APISpec: &spec.APISpec{
			Name: "tail-empty",
			Resources: map[string]spec.Resource{
				"widgets": {
					Endpoints: map[string]spec.Endpoint{
						"create": {Method: "POST", Path: "/widgets"},
					},
				},
			},
		},
		SyncableResources: []profiler.SyncableResource{
			{Name: "children", Path: "/parents/{id}/children", Method: "GET"},
		},
	}
	assert.Empty(t, tailPollResources(data))
	assert.Empty(t, tailHappyArgs(data))
	first := firstTailExampleLine(tailCommandExample(data))
	args, err := shellargs.ArgsAfterBinary(first)
	require.NoError(t, err)
	assert.Equal(t, []string{"tail", "--json"}, args)
	assert.NotContains(t, tailCommandExample(data), "messages")
	assert.NotContains(t, tailCommandExample(data), "events")
}

func TestTailPollResourcesSkipsGetsThatNeedQueryArgs(t *testing.T) {
	t.Parallel()

	api := &spec.APISpec{
		Name: "tail-required",
		Resources: map[string]spec.Resource{
			"availability": tailResourceWithParams("/availability", spec.Param{
				Name: "source", In: "query", Type: "string", Required: true,
				Enum: []string{"united", "delta", "aeroplan"},
			}),
			"catalog": tailResourceWithParams("/catalog", spec.Param{
				Name: "source", In: "query", Required: true, Default: "united",
			}),
			"items": tailListResource("/items"),
			"pages": tailResourceWithParams("/pages", spec.Param{
				Name: "limit", In: "query", Required: true,
			}),
			"routes": tailResourceWithParams("/routes",
				spec.Param{Name: "origin_airport", In: "query", Required: true},
				spec.Param{Name: "destination_airport", In: "query", Required: true},
			),
			"seats": tailResourceWithParams("/availability?source=united", spec.Param{
				Name: "source", In: "query", Required: true,
			}),
			"widgets": tailResourceWithParams("/widgets", spec.Param{
				Name: "X-Request-Id", In: "header", Required: true,
			}),
		},
	}
	data := visionRenderData{
		APISpec: api,
		SyncableResources: []profiler.SyncableResource{
			{Name: "availability", Path: "/availability", Method: "GET"},
			{Name: "catalog", Path: "/catalog", Method: "GET"},
			{Name: "routes", Path: "/routes", Method: "GET"},
			{Name: "pages", Path: "/pages", Method: "GET"},
			{Name: "items", Path: "/items", Method: "GET"},
			{Name: "seats", Path: "/availability?source=united", Method: "GET"},
			{Name: "widgets", Path: "/widgets", Method: "GET"},
		},
	}

	assert.Equal(t, []string{"items", "seats", "widgets"}, tailPollResources(data))
	assert.Equal(t, "items", tailPrimaryResource(data))
	assert.Equal(t, "resource=items;--follow=false", tailHappyArgs(data))
	args, err := shellargs.ArgsAfterBinary(firstTailExampleLine(tailCommandExample(data)))
	require.NoError(t, err)
	assert.Equal(t, []string{"tail", "items", "--follow=false"}, args)

	onlyRequired := visionRenderData{
		APISpec: &spec.APISpec{
			Name: "tail-required-only",
			Resources: map[string]spec.Resource{
				"availability": api.Resources["availability"],
			},
		},
		SyncableResources: []profiler.SyncableResource{
			{Name: "availability", Path: "/availability", Method: "GET"},
		},
	}
	assert.Empty(t, tailPollResources(onlyRequired))
	assert.Empty(t, tailPrimaryResource(onlyRequired))
	assert.Empty(t, tailHappyArgs(onlyRequired))
	hiddenArgs, err := shellargs.ArgsAfterBinary(firstTailExampleLine(tailCommandExample(onlyRequired)))
	require.NoError(t, err)
	assert.Equal(t, []string{"tail", "--json"}, hiddenArgs)
}

func TestTailGetNeedsQueryArgsIgnoresAuthSuppliedKeys(t *testing.T) {
	t.Parallel()

	required := func(name, urlName string) spec.Param {
		return spec.Param{Name: name, URLName: urlName, In: "query", Required: true}
	}
	withScheme := func(resource spec.Resource, scheme string) spec.Resource {
		endpoint := resource.Endpoints["list"]
		endpoint.AuthScheme = scheme
		resource.Endpoints["list"] = endpoint
		return resource
	}
	queryKey := spec.AuthConfig{Type: "api_key", In: "query", Header: "key", Scheme: "APIKey"}
	sibling := queryKey
	sibling.AdditionalHeaders = []spec.AdditionalAuthHeader{{
		Header: "token",
		In:     "query",
		EnvVar: spec.AuthEnvVar{Name: "API_TOKEN"},
	}}
	perOperation := queryKey
	perOperation.AdditionalHeaders = []spec.AdditionalAuthHeader{{
		Header:       "token",
		In:           "query",
		Scheme:       "APIToken",
		PerOperation: true,
		EnvVar:       spec.AuthEnvVar{Name: "API_TOKEN"},
	}}
	handshake := spec.AuthConfig{
		Type:           "session_handshake",
		In:             "query",
		Header:         "api_key",
		TokenParamName: "crumb",
		TokenParamIn:   "query",
	}

	tests := []struct {
		name     string
		auth     spec.AuthConfig
		resource spec.Resource
		pattern  string
		tier     bool
		needs    bool
	}{
		{
			name:     "default query key",
			auth:     spec.AuthConfig{Type: "api_key", In: "query"},
			resource: tailResourceWithParams("/quotes", required("api_key", "")),
		},
		{
			name:     "configured query name",
			auth:     spec.AuthConfig{Type: "api_key", In: "query", Header: "key"},
			resource: tailResourceWithParams("/quotes", required("key", "")),
		},
		{
			name:     "wire name",
			auth:     spec.AuthConfig{Type: "api_key", In: "query"},
			resource: tailResourceWithParams("/quotes", required("apiKey", "api_key")),
		},
		{
			name:     "different query name still required",
			auth:     spec.AuthConfig{Type: "api_key", In: "query", Header: "key"},
			resource: tailResourceWithParams("/quotes", required("api_key", "")),
			needs:    true,
		},
		{
			name:     "header auth does not satisfy a query param",
			auth:     spec.AuthConfig{Type: "api_key", In: "header", Header: "api_key"},
			resource: tailResourceWithParams("/quotes", required("api_key", "")),
			needs:    true,
		},
		{
			name: "auth key plus another required query",
			auth: spec.AuthConfig{Type: "api_key", In: "query"},
			resource: tailResourceWithParams("/quotes",
				required("api_key", ""),
				required("source", ""),
			),
			needs: true,
		},
		{
			name:     "sibling query credential",
			auth:     sibling,
			resource: tailResourceWithParams("/quotes", required("key", ""), required("token", "")),
		},
		{
			name:     "other sole scheme omits the primary key",
			auth:     perOperation,
			resource: withScheme(tailResourceWithParams("/quotes", required("key", "")), "APIToken"),
			needs:    true,
		},
		{
			name:     "other sole scheme supplies its query credential",
			auth:     perOperation,
			resource: withScheme(tailResourceWithParams("/quotes", required("token", "")), "APIToken"),
		},
		{
			name:     "session handshake query token",
			auth:     handshake,
			resource: tailResourceWithParams("/quotes", required("crumb", "")),
		},
		{
			name:     "session handshake does not send the header name",
			auth:     handshake,
			resource: tailResourceWithParams("/quotes", required("api_key", "")),
			needs:    true,
		},
		{
			name: "session handshake header token is not a query arg",
			auth: spec.AuthConfig{
				Type:           "session_handshake",
				TokenParamName: "crumb",
				TokenParamIn:   "header",
			},
			resource: tailResourceWithParams("/quotes", required("crumb", "")),
			needs:    true,
		},
		{
			name:     "tier routing uses the primary query name",
			auth:     handshake,
			resource: tailResourceWithParams("/quotes", required("api_key", "")),
			tier:     true,
		},
		{
			name:     "tier routing does not use the session token name",
			auth:     handshake,
			resource: tailResourceWithParams("/quotes", required("crumb", "")),
			tier:     true,
			needs:    true,
		},
		{
			name:     "proxy envelope leaves auth off the inner path",
			auth:     spec.AuthConfig{Type: "api_key", In: "query"},
			resource: tailResourceWithParams("/quotes", required("api_key", "")),
			pattern:  "proxy-envelope",
			needs:    true,
		},
		{
			name:     "query key case must match",
			auth:     spec.AuthConfig{Type: "api_key", In: "query"},
			resource: tailResourceWithParams("/quotes", required("API_KEY", "")),
			needs:    true,
		},
		{
			name:     "exact query key case",
			auth:     spec.AuthConfig{Type: "api_key", In: "query", Header: "API_KEY"},
			resource: tailResourceWithParams("/quotes", required("API_KEY", "")),
		},
		{
			name:     "path query case must match",
			auth:     spec.AuthConfig{Type: "none"},
			resource: tailResourceWithParams("/quotes?API_KEY=1", required("api_key", "")),
			needs:    true,
		},
		{
			name:     "path query exact case",
			auth:     spec.AuthConfig{Type: "none"},
			resource: tailResourceWithParams("/quotes?api_key=1", required("api_key", "")),
		},
		{
			name:     "auth type none supplies nothing",
			auth:     spec.AuthConfig{Type: "none", In: "query"},
			resource: tailResourceWithParams("/quotes", required("api_key", "")),
			needs:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			api := &spec.APISpec{
				Name:          "tail-auth",
				Auth:          tt.auth,
				ClientPattern: tt.pattern,
				Resources:     map[string]spec.Resource{"quotes": tt.resource},
			}
			if tt.tier {
				api.TierRouting = spec.TierRoutingConfig{DefaultTier: "live"}
			}
			assert.Equal(t, tt.needs, tailGetNeedsQueryArgs(api, tt.resource))
		})
	}
}

func TestTailPollResourcesKeepsQueryAuthResource(t *testing.T) {
	t.Parallel()

	api := &spec.APISpec{
		Name: "tail-auth",
		Auth: spec.AuthConfig{Type: "api_key", In: "query"},
		Resources: map[string]spec.Resource{
			"quotes": tailResourceWithParams("/quotes", spec.Param{
				Name: "api_key", In: "query", Required: true,
			}),
			"availability": tailResourceWithParams("/availability", spec.Param{
				Name: "source", In: "query", Required: true,
			}),
		},
	}
	data := visionRenderData{
		APISpec: api,
		SyncableResources: []profiler.SyncableResource{
			{Name: "availability", Path: "/availability", Method: "GET"},
			{Name: "quotes", Path: "/quotes", Method: "GET"},
		},
	}
	assert.Equal(t, []string{"quotes"}, tailPollResources(data))
	assert.Equal(t, "quotes", tailPrimaryResource(data))
	assert.Equal(t, "resource=quotes;--follow=false", tailHappyArgs(data))
	args, err := shellargs.ArgsAfterBinary(firstTailExampleLine(tailCommandExample(data)))
	require.NoError(t, err)
	assert.Equal(t, []string{"tail", "quotes", "--follow=false"}, args)

	onlyKey := visionRenderData{
		APISpec: &spec.APISpec{
			Name: "tail-auth-only",
			Auth: api.Auth,
			Resources: map[string]spec.Resource{
				"quotes": api.Resources["quotes"],
			},
		},
		SyncableResources: []profiler.SyncableResource{
			{Name: "quotes", Path: "/quotes", Method: "GET"},
		},
	}
	assert.Equal(t, []string{"quotes"}, tailPollResources(onlyKey))
	assert.NotEmpty(t, tailHappyArgs(onlyKey))
}

func TestGeneratedTailExamplesUseSyncableResources(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "tail-examples",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"widgets":  tailListResource("/widgets"),
			"invoices": tailListResource("/invoices"),
			"alpha-notes": {
				Endpoints: map[string]spec.Endpoint{
					"create": {Method: "POST", Path: "/notes"},
				},
			},
			"scoped": tailListResource("/parents/{id}/children"),
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Tail: true, MCP: true}
	require.NoError(t, gen.Generate())

	tailSrc := readTailExampleFile(t, outputDir)
	assert.NotContains(t, tailSrc, "messages")
	assert.NotContains(t, tailSrc, "events")
	assert.Contains(t, tailSrc, `"pp:happy-args"] = "resource=invoices;--follow=false"`)
	assert.NotContains(t, tailSrc, "cmd.Hidden = true")
	assert.Contains(t, tailSrc, "BoolVar(&follow, \"follow\", true")
	assert.Contains(t, tailSrc, "resource name required; known resources:")
	assert.NotContains(t, tailSrc, "tail messages")

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "tail_example_behavior_test.go"), []byte(tailSyncableBehaviorTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestGeneratedTail", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedTailWithoutSyncableResourcesStaysCompilable(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "tail-empty",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"widgets": {
				Endpoints: map[string]spec.Endpoint{
					"create": {Method: "POST", Path: "/widgets", Body: []spec.Param{{Name: "name", Type: "string"}}},
				},
			},
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Tail: true, MCP: true}
	require.NoError(t, gen.Generate())

	tailSrc := readTailExampleFile(t, outputDir)
	assert.NotContains(t, tailSrc, "messages")
	assert.NotContains(t, tailSrc, "events")
	assert.NotContains(t, tailSrc, "pp:happy-args")
	assert.Contains(t, tailSrc, "cmd.Hidden = true")
	assert.Contains(t, tailSrc, "tail --json")
	assert.Contains(t, tailSrc, "this CLI exposes no tail resources")

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "tail_example_behavior_test.go"), []byte(tailEmptyBehaviorTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestGeneratedTail", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedTailExampleSkipsRequiredQueryResource(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "tail-query",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"availability": tailResourceWithParams("/availability", spec.Param{
				Name:     "source",
				In:       "query",
				Type:     "string",
				Required: true,
				Enum:     []string{"united", "delta", "aeroplan"},
			}),
			"items": tailListResource("/items"),
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Tail: true, MCP: true}
	require.NoError(t, gen.Generate())

	tailSrc := readTailExampleFile(t, outputDir)
	assert.Contains(t, tailSrc, `"pp:happy-args"] = "resource=items;--follow=false"`)
	assert.Contains(t, tailSrc, "tail items --follow=false")
	assert.NotContains(t, tailSrc, "tail availability")
	assert.NotContains(t, tailSrc, "cmd.Hidden = true")
	assert.Contains(t, tailSrc, `"availability"`)

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "tail_example_behavior_test.go"), []byte(tailRequiredQueryBehaviorTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestGeneratedTail", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedTailHidesWhenEveryGetNeedsQueryArgs(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "tail-query-only",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"availability": tailResourceWithParams("/availability", spec.Param{
				Name:     "source",
				In:       "query",
				Type:     "string",
				Required: true,
				Enum:     []string{"united", "delta", "aeroplan"},
			}),
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Tail: true, MCP: true}
	require.NoError(t, gen.Generate())

	tailSrc := readTailExampleFile(t, outputDir)
	assert.Contains(t, tailSrc, "cmd.Hidden = true")
	assert.NotContains(t, tailSrc, "pp:happy-args")
	assert.Contains(t, tailSrc, "tail --json")
	assert.NotContains(t, tailSrc, "tail availability")
	assert.Contains(t, tailSrc, `"availability"`)
	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedTailKeepsQueryAuthResource(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "tail-auth-query",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "api_key", In: "query"},
		Resources: map[string]spec.Resource{
			"quotes": tailResourceWithParams("/quotes", spec.Param{
				Name:     "api_key",
				In:       "query",
				Type:     "string",
				Required: true,
			}),
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Tail: true, MCP: true}
	require.NoError(t, gen.Generate())

	tailSrc := readTailExampleFile(t, outputDir)
	assert.NotContains(t, tailSrc, "cmd.Hidden = true")
	assert.Contains(t, tailSrc, `"pp:happy-args"] = "resource=quotes;--follow=false"`)
	assert.Contains(t, tailSrc, "tail quotes --follow=false")

	clientSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "client", "client.go"))
	require.NoError(t, err)
	assert.Contains(t, string(clientSrc), `q.Set("api_key", authHeader)`)
	requireGeneratedCompiles(t, outputDir)
}

func tailResourceWithParams(path string, params ...spec.Param) spec.Resource {
	resource := tailListResource(path)
	endpoint := resource.Endpoints["list"]
	endpoint.Params = append([]spec.Param(nil), params...)
	resource.Endpoints["list"] = endpoint
	return resource
}

func tailListResource(path string) spec.Resource {
	return spec.Resource{
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method:     "GET",
				Path:       path,
				Response:   spec.ResponseDef{Type: "array"},
				Pagination: &spec.Pagination{Type: "cursor", CursorParam: "cursor", LimitParam: "limit"},
			},
		},
	}
}

func firstTailExampleLine(example string) string {
	for line := range strings.SplitSeq(example, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	return ""
}

func readTailExampleFile(t *testing.T, outputDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "tail.go"))
	require.NoError(t, err)
	return string(data)
}

const tailRequiredQueryBehaviorTest = `package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestGeneratedTailPollsItemWithoutRequiredQuery(t *testing.T) {
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	t.Setenv("PRINTING_PRESS_VERIFY", "")

	cmd := newTailCmd(&rootFlags{})
	if cmd.Hidden {
		t.Fatal("tail must stay visible when another resource needs no query arguments")
	}
	if got := cmd.Annotations["pp:happy-args"]; got != "resource=items;--follow=false" {
		t.Fatalf("pp:happy-args = %q", got)
	}
	first := ""
	for _, line := range strings.Split(cmd.Example, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		first = line
		break
	}
	if !strings.Contains(first, "tail items --follow=false") {
		t.Fatalf("first example = %q\nfull:\n%s", first, cmd.Example)
	}
	if strings.Contains(cmd.Example, "tail availability") {
		t.Fatalf("example polls a resource that needs query arguments:\n%s", cmd.Example)
	}

	var gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, ` + "`" + `[{"id":"item-1"}]` + "`" + `)
	}))
	defer server.Close()
	t.Setenv("TAIL_QUERY_BASE_URL", server.URL)

	orig := os.Stdout
	readEnd, writeEnd, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatal(pipeErr)
	}
	os.Stdout = writeEnd
	poll := newTailCmd(&rootFlags{})
	poll.SetOut(io.Discard)
	poll.SetErr(io.Discard)
	poll.SetArgs([]string{"items", "--follow=false"})
	done := make(chan error, 1)
	go func() { done <- poll.Execute() }()
	select {
	case execErr := <-done:
		os.Stdout = orig
		_ = writeEnd.Close()
		_, _ = io.Copy(io.Discard, readEnd)
		if execErr != nil {
			t.Fatalf("tail items --follow=false: %v", execErr)
		}
	case <-time.After(5 * time.Second):
		os.Stdout = orig
		_ = writeEnd.Close()
		t.Fatal("tail items --follow=false did not return")
	}
	if gotPath != "/items" {
		t.Fatalf("path = %q, want /items", gotPath)
	}
	if gotQuery != "" {
		t.Fatalf("query = %q, want empty", gotQuery)
	}
}
`

const tailSyncableBehaviorTest = `package cli

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
)

func TestGeneratedTailPollsFirstSyncableResourceOnce(t *testing.T) {
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	t.Setenv("PRINTING_PRESS_VERIFY", "")

	cmd := newTailCmd(&rootFlags{})
	if cmd.Hidden {
		t.Fatal("tail with a syncable resource must stay visible")
	}
	if got := cmd.Annotations["pp:happy-args"]; got != "resource=invoices;--follow=false" {
		t.Fatalf("pp:happy-args = %q", got)
	}
	follow := cmd.Flags().Lookup("follow")
	if follow == nil || follow.Value.Type() != "bool" {
		t.Fatal("--follow must exist and accept --follow=false")
	}
	first := firstGeneratedTailExample(cmd.Example)
	if !strings.Contains(first, "tail invoices --follow=false") {
		t.Fatalf("first example = %q\nfull:\n%s", first, cmd.Example)
	}
	if strings.Contains(cmd.Example, "messages") || strings.Contains(cmd.Example, "events") {
		t.Fatalf("example names a placeholder resource:\n%s", cmd.Example)
	}

	missing := newTailCmd(&rootFlags{})
	missing.SetOut(io.Discard)
	missing.SetErr(io.Discard)
	err := missing.Execute()
	if err == nil || !strings.Contains(err.Error(), "invoices") || !strings.Contains(err.Error(), "widgets") || strings.Contains(err.Error(), "messages") {
		t.Fatalf("missing-resource error = %v", err)
	}

	help := newRootCmd(&rootFlags{})
	var helpBuf bytes.Buffer
	help.SetOut(&helpBuf)
	help.SetErr(&helpBuf)
	help.SetArgs([]string{"tail", "--help"})
	helpErr := help.Execute()
	if helpErr != nil && !errors.Is(helpErr, pflag.ErrHelp) {
		t.Fatalf("tail --help: %v\n%s", helpErr, helpBuf.String())
	}
	if !strings.Contains(helpBuf.String(), "tail invoices --follow=false") {
		t.Fatalf("help missing runnable example:\n%s", helpBuf.String())
	}

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, ` + "`" + `[{"id":"inv-1"}]` + "`" + `)
	}))
	defer server.Close()
	t.Setenv("TAIL_EXAMPLES_BASE_URL", server.URL)

	orig := os.Stdout
	readEnd, writeEnd, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatal(pipeErr)
	}
	os.Stdout = writeEnd
	poll := newTailCmd(&rootFlags{})
	poll.SetOut(io.Discard)
	poll.SetErr(io.Discard)
	poll.SetArgs([]string{"invoices", "--follow=false"})
	done := make(chan error, 1)
	go func() { done <- poll.Execute() }()
	select {
	case execErr := <-done:
		os.Stdout = orig
		_ = writeEnd.Close()
		_, _ = io.Copy(io.Discard, readEnd)
		if execErr != nil {
			t.Fatalf("tail invoices --follow=false: %v", execErr)
		}
	case <-time.After(5 * time.Second):
		os.Stdout = orig
		_ = writeEnd.Close()
		t.Fatal("tail invoices --follow=false did not return")
	}
	if gotPath != "/invoices" {
		t.Fatalf("path = %q, want /invoices", gotPath)
	}
}

func firstGeneratedTailExample(example string) string {
	for _, line := range strings.Split(example, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line
	}
	return ""
}
`

const tailEmptyBehaviorTest = `package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func TestGeneratedTailWithoutResourcesDiscoversAndCompiles(t *testing.T) {
	cmd := newTailCmd(&rootFlags{})
	if !cmd.Hidden {
		t.Fatal("tail with no syncable resources must be hidden")
	}
	if _, ok := cmd.Annotations["pp:happy-args"]; ok {
		t.Fatalf("pp:happy-args = %q", cmd.Annotations["pp:happy-args"])
	}
	first := ""
	for _, line := range strings.Split(cmd.Example, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		first = line
		break
	}
	if !strings.Contains(first, "tail --json") {
		t.Fatalf("first example = %q", first)
	}
	if strings.Contains(cmd.Example, "messages") || strings.Contains(cmd.Example, "events") {
		t.Fatalf("example names a placeholder resource:\n%s", cmd.Example)
	}

	missing := newTailCmd(&rootFlags{})
	missing.SetOut(io.Discard)
	missing.SetErr(io.Discard)
	err := missing.Execute()
	if err == nil || strings.Contains(err.Error(), "messages") || !strings.Contains(err.Error(), "no tail resources") {
		t.Fatalf("missing-resource error = %v", err)
	}

	root := newRootCmd(&rootFlags{})
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"tail", "--json"})
	if err := root.Execute(); err != nil && !errors.Is(err, pflag.ErrHelp) {
		t.Fatalf("tail --json: %v\n%s", err, buf.String())
	}
	out := strings.TrimSpace(buf.String())
	if !json.Valid([]byte(out)) {
		t.Fatalf("tail --json stdout is not JSON: %s", out)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode: %v\n%s", err, out)
	}
	if _, ok := payload["resources"]; !ok {
		t.Fatalf("payload = %#v", payload)
	}
}
`
