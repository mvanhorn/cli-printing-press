package generator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestPartialFailureEmissionFlagsGraphQLPromotedMutation(t *testing.T) {
	t.Parallel()

	apiSpec := graphqlFailureSpec("gql-flags", "https://api.graphql-flags.example", false)
	promoted, _, names := buildPromotedCommandPlan(apiSpec)
	require.Equal(t, "start", names["checkout"])

	hasSupport, hasTypedErr := partialFailureEmissionFlags(apiSpec, promoted, names, false)
	require.True(t, hasSupport, "promoted GraphQL mutations need the detector")
	require.True(t, hasTypedErr, "promoted GraphQL mutations call partialFailureErr")

	rest := minimalSpec("rest-promoted-post")
	rest.Resources = map[string]spec.Resource{
		"checkout": {
			Description: "Checkout",
			Endpoints: map[string]spec.Endpoint{
				"start": {
					Method:      "POST",
					Path:        "/checkout",
					Description: "Start checkout",
					Body:        []spec.Param{{Name: "cartId", Type: "string", Required: true}},
				},
			},
		},
	}
	promoted, _, names = buildPromotedCommandPlan(rest)
	require.Equal(t, "start", names["checkout"])
	hasSupport, hasTypedErr = partialFailureEmissionFlags(rest, promoted, names, false)
	require.False(t, hasSupport)
	require.False(t, hasTypedErr)
}

func TestGraphQLPromotedMutationEmitsFailureDetector(t *testing.T) {
	t.Parallel()

	apiSpec := graphqlFailureSpec("gql-emit", "https://api.graphql-emit.example", false)
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	helpers := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	require.Contains(t, helpers, "detectGraphQLFailure(top)")
	failure := readGeneratedFile(t, outputDir, "internal", "cli", "graphql_failure.go")
	require.Contains(t, failure, "func detectGraphQLFailure(")
	require.Contains(t, failure, "__pp_verify_synthetic__")
	require.NotContains(t, failure, "viewSection")

	promoted := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_checkout.go")
	require.Contains(t, promoted, "detectPartialFailure(data)")
	require.Contains(t, promoted, "partialFailureErr(")
	require.Contains(t, promoted, "statusCode")

	root := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	require.Contains(t, root, "allow-partial-failure")
	_, err := os.Stat(filepath.Join(outputDir, "internal", "cli", "checkout_start.go"))
	require.True(t, os.IsNotExist(err), "promoted endpoint must not also emit a standalone command file")
	requireGeneratedCompiles(t, outputDir)
}

func TestGraphQLMutationWithStoreCompiles(t *testing.T) {
	t.Parallel()

	apiSpec := graphqlFailureSpec("gql-store", "https://api.graphql-store.example", true)
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, MCP: true}
	require.NoError(t, gen.Generate())

	promoted := readGeneratedFile(t, outputDir, "internal", "cli", "promoted_checkout.go")
	require.Contains(t, promoted, "writeMutationResponseToStore(")
	require.Contains(t, promoted, "partialFailureErr(")
	require.Contains(t, readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go"), "detectGraphQLFailure(top)")
	requireGeneratedCompiles(t, outputDir)
}

func TestGraphQLMutationFailuresExitNonZero(t *testing.T) {
	t.Parallel()

	box := &scriptedJSON{}
	srv := httptest.NewServer(box)
	t.Cleanup(srv.Close)

	apiSpec := graphqlFailureSpec("gqlfail", srv.URL, true)
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	helpers := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	require.Contains(t, helpers, "detectGraphQLFailure(top)")
	require.Contains(t, readGeneratedFile(t, outputDir, "internal", "cli", "notes_create.go"), "detectPartialFailure(data)")
	require.Contains(t, readGeneratedFile(t, outputDir, "internal", "cli", "promoted_checkout.go"), "partialFailureErr(")

	bin := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", bin, "./cmd/"+naming.CLI(apiSpec.Name))

	home := t.TempDir()
	run := func(t *testing.T, body string, args ...string) (int, map[string]any, string) {
		t.Helper()
		box.set(body)
		before := box.hits.Load()
		code, out := runPrintedCLI(t, bin, printedCLIEnv(home), args...)
		require.Greater(t, box.hits.Load(), before, "command did not reach the API:\n%s", out)
		return code, decodeCLIObject(t, out), out
	}

	t.Run("success", func(t *testing.T) {
		code, env, out := run(t, `{"data":{"id":"1"}}`, "--json", "notes", "create", "--title", "t")
		require.Equal(t, 0, code, out)
		require.Equal(t, true, env["success"])
		_, ok := env["partial_failure"]
		require.False(t, ok, out)
	})

	t.Run("errors array", func(t *testing.T) {
		code, env, out := run(t, `{"errors":[{"message":"boom"}]}`, "--json", "notes", "create", "--title", "t")
		require.Equal(t, 6, code, out)
		require.Equal(t, false, env["success"])
		require.Equal(t, "errors", partialField(t, env))
		require.Equal(t, "boom", partialMessage(t, env))
	})

	t.Run("joined messages", func(t *testing.T) {
		code, env, out := run(t, `{"errors":[{"message":"boom"},{"message":" second "}]}`, "--json", "notes", "create", "--title", "t")
		require.Equal(t, 6, code, out)
		require.Equal(t, false, env["success"])
		require.Equal(t, "boom; second", partialMessage(t, env))
	})

	t.Run("errors without messages", func(t *testing.T) {
		code, env, out := run(t, `{"errors":[{},{"extensions":{"code":"X"}}]}`, "--json", "notes", "create", "--title", "t")
		require.Equal(t, 6, code, out)
		require.Equal(t, "2 GraphQL error(s)", partialMessage(t, env))
	})

	t.Run("blank message", func(t *testing.T) {
		code, env, out := run(t, `{"errors":[{"message":"  "}]}`, "--json", "notes", "create", "--title", "t")
		require.Equal(t, 6, code, out)
		require.Equal(t, "1 GraphQL error(s)", partialMessage(t, env))
	})

	t.Run("empty errors", func(t *testing.T) {
		code, env, out := run(t, `{"errors":[]}`, "--json", "notes", "create", "--title", "t")
		require.Equal(t, 0, code, out)
		require.Equal(t, true, env["success"])
	})

	t.Run("errors not an array", func(t *testing.T) {
		code, env, out := run(t, `{"errors":"nope"}`, "--json", "notes", "create", "--title", "t")
		require.Equal(t, 0, code, out)
		require.Equal(t, true, env["success"])
	})

	t.Run("error typename", func(t *testing.T) {
		body := `{"data":{"initializeCheckout":{"__typename":"CheckoutCheckoutErrorsCheckoutRedirectError","message":"redirect"}}}`
		code, env, out := run(t, body, "--json", "notes", "create", "--title", "t")
		require.Equal(t, 6, code, out)
		require.Equal(t, false, env["success"])
		require.Equal(t, "data.initializeCheckout.__typename", partialField(t, env))
		require.Equal(t, "CheckoutCheckoutErrorsCheckoutRedirectError: redirect", partialMessage(t, env))
	})

	t.Run("error response typename", func(t *testing.T) {
		body := `{"data":{"updateCheckout":{"__typename":"CheckoutCheckoutErrorResponse","errors":[{"__typename":"CheckoutFieldError","code":"BAD","message":"bad"}]}}}`
		code, env, out := run(t, body, "--json", "notes", "create", "--title", "t")
		require.Equal(t, 6, code, out)
		require.Equal(t, false, env["success"])
		require.Equal(t, "data.updateCheckout.__typename", partialField(t, env))
		require.Equal(t, "CheckoutCheckoutErrorResponse: CheckoutFieldError: BAD: bad", partialMessage(t, env))
	})

	t.Run("typename ending in Error", func(t *testing.T) {
		body := `{"data":{"thing":{"__typename":"NotAnError"}}}`
		code, env, out := run(t, body, "--json", "notes", "create", "--title", "t")
		require.Equal(t, 6, code, out)
		require.Equal(t, "data.thing.__typename", partialField(t, env))
	})

	t.Run("success typename", func(t *testing.T) {
		code, env, out := run(t, `{"data":{"thing":{"__typename":"CheckoutOk"}}}`, "--json", "notes", "create", "--title", "t")
		require.Equal(t, 0, code, out)
		require.Equal(t, true, env["success"])
	})

	t.Run("user errors plural", func(t *testing.T) {
		code, env, out := run(t, `{"data":{"thing":{"__typename":"UserErrors"}}}`, "--json", "notes", "create", "--title", "t")
		require.Equal(t, 0, code, out)
		require.Equal(t, true, env["success"])
	})

	t.Run("list element typename ignored", func(t *testing.T) {
		code, env, out := run(t, `{"data":{"items":[{"__typename":"ItemError"}]}}`, "--json", "notes", "create", "--title", "t")
		require.Equal(t, 0, code, out)
		require.Equal(t, true, env["success"])
	})

	t.Run("sorted data keys", func(t *testing.T) {
		body := `{"data":{"zeta":{"__typename":"ZetaError"},"alpha":{"__typename":"AlphaError"}}}`
		code, env, out := run(t, body, "--json", "notes", "create", "--title", "t")
		require.Equal(t, 6, code, out)
		require.Equal(t, "data.alpha.__typename", partialField(t, env))
	})

	t.Run("partial failure field wins", func(t *testing.T) {
		body := `{"partialFailureError":{"code":3,"message":"batch failed"},"errors":[{"message":"gql"}]}`
		code, env, out := run(t, body, "--json", "notes", "create", "--title", "t")
		require.Equal(t, 6, code, out)
		require.Equal(t, false, env["success"])
		require.Equal(t, "partialFailureError", partialField(t, env))
		require.Equal(t, "batch failed", partialMessage(t, env))
	})

	t.Run("empty partial failure still sees graphql errors", func(t *testing.T) {
		body := `{"partialFailureError":{},"errors":[{"message":"gql"}]}`
		code, env, out := run(t, body, "--json", "notes", "create", "--title", "t")
		require.Equal(t, 6, code, out)
		require.Equal(t, "errors", partialField(t, env))
		require.Equal(t, "gql", partialMessage(t, env))
	})

	t.Run("verify synthetic", func(t *testing.T) {
		body := `{"__pp_verify_synthetic__":true,"errors":[{"message":"should ignore"}],"data":{"thing":{"__typename":"ThingError"}}}`
		code, env, out := run(t, body, "--json", "notes", "create", "--title", "t")
		require.Equal(t, 0, code, out)
		require.Equal(t, true, env["verify_noop"])
		require.Equal(t, false, env["success"])
		_, ok := env["partial_failure"]
		require.False(t, ok, out)
	})

	t.Run("allow partial failure", func(t *testing.T) {
		code, env, out := run(t, `{"errors":[{"message":"boom"}]}`, "--allow-partial-failure", "--json", "notes", "create", "--title", "t")
		require.Equal(t, 0, code, out)
		require.Equal(t, true, env["success"])
		require.Equal(t, "errors", partialField(t, env))
		require.Contains(t, out, "warning: partial failure detected in notes response: boom")
	})

	t.Run("quiet json", func(t *testing.T) {
		box.set(`{"errors":[{"message":"boom"}]}`)
		before := box.hits.Load()
		code, out := runPrintedCLI(t, bin, printedCLIEnv(home), "--json", "--quiet", "notes", "create", "--title", "t")
		require.Greater(t, box.hits.Load(), before, out)
		require.Equal(t, 6, code, out)
		require.NotContains(t, out, "{")
		require.Contains(t, out, "warning: partial failure detected in notes response: boom")
	})

	t.Run("promoted errors", func(t *testing.T) {
		code, env, out := run(t, `{"errors":[{"message":"checkout failed"}]}`, "--json", "checkout", "--cart-id", "c1")
		require.Equal(t, 6, code, out)
		require.Equal(t, false, env["success"])
		require.Equal(t, "errors", partialField(t, env))
		require.Equal(t, "checkout failed", partialMessage(t, env))
	})

	t.Run("promoted typename", func(t *testing.T) {
		body := `{"data":{"initializeCheckout":{"__typename":"CheckoutCheckoutErrorResponse"}}}`
		code, env, out := run(t, body, "--json", "checkout", "--cart-id", "c1")
		require.Equal(t, 6, code, out)
		require.Equal(t, false, env["success"])
		require.Equal(t, "data.initializeCheckout.__typename", partialField(t, env))
	})

	t.Run("promoted success", func(t *testing.T) {
		box.set(`{"data":{"started":true}}`)
		before := box.hits.Load()
		code, out := runPrintedCLI(t, bin, printedCLIEnv(home), "--json", "checkout", "--cart-id", "c1")
		require.Greater(t, box.hits.Load(), before, out)
		require.Equal(t, 0, code, out)
		require.NotContains(t, out, "partial_failure")
		require.Contains(t, out, `"started"`)
	})

	t.Run("promoted allow", func(t *testing.T) {
		code, env, out := run(t, `{"errors":[{"message":"checkout failed"}]}`, "--allow-partial-failure", "--json", "checkout", "--cart-id", "c1")
		require.Equal(t, 0, code, out)
		require.Equal(t, true, env["success"])
		require.Equal(t, "errors", partialField(t, env))
	})

	t.Run("promoted synthetic", func(t *testing.T) {
		box.set(`{"__pp_verify_synthetic__":true,"errors":[{"message":"should ignore"}]}`)
		before := box.hits.Load()
		code, out := runPrintedCLI(t, bin, printedCLIEnv(home), "--json", "checkout", "--cart-id", "c1")
		require.Greater(t, box.hits.Load(), before, out)
		require.Equal(t, 0, code, out)
		require.NotContains(t, out, "partial_failure")
		require.NotContains(t, out, `"success"`)
	})

	const failureBody = `{"errors":[{"message":"boom"}],"id":"note-1","secret":"drop-me","comments":"long comment","attachments":["file-a"]}`
	const successBody = `{"id":"note-1","secret":"drop-me","comments":"long comment","attachments":["file-a"]}`
	selected := map[string]any{"id": "note-1"}
	compacted := map[string]any{"id": "note-1", "secret": "drop-me"}
	failureCompacted := map[string]any{
		"errors": []any{map[string]any{"message": "boom"}},
		"id":     "note-1",
		"secret": "drop-me",
	}
	commands := []struct {
		name string
		args []string
	}{
		{name: "notes", args: []string{"notes", "create", "--title", "t"}},
		{name: "checkout", args: []string{"checkout", "--cart-id", "c1"}},
	}
	for _, cmd := range commands {
		t.Run(cmd.name+" filters", func(t *testing.T) {
			code, env, out := run(t, failureBody, append([]string{"--json"}, cmd.args...)...)
			require.Equal(t, 6, code, out)
			require.Equal(t, false, env["success"])
			require.Contains(t, responseObject(t, env), "secret")
			require.Contains(t, responseObject(t, env), "comments")

			code, failure, out := run(t, failureBody, append([]string{"--json", "--select", "id"}, cmd.args...)...)
			require.Equal(t, 6, code, out)
			require.Equal(t, false, failure["success"])
			require.Equal(t, "errors", partialField(t, failure))
			code, success, out := run(t, successBody, append([]string{"--json", "--select", "id"}, cmd.args...)...)
			require.Equal(t, 0, code, out)
			require.Equal(t, selected, responseObject(t, success), out)
			require.Equal(t, responseObject(t, success), responseObject(t, failure))

			code, failure, out = run(t, failureBody, append([]string{"--json", "--compact"}, cmd.args...)...)
			require.Equal(t, 6, code, out)
			require.Equal(t, failureCompacted, responseObject(t, failure), out)
			code, success, out = run(t, successBody, append([]string{"--json", "--compact"}, cmd.args...)...)
			require.Equal(t, 0, code, out)
			require.Equal(t, compacted, responseObject(t, success), out)

			code, failure, out = run(t, failureBody, append([]string{"--json", "--select", "id", "--compact"}, cmd.args...)...)
			require.Equal(t, 6, code, out)
			require.Equal(t, selected, responseObject(t, failure), out)

			code, failure, out = run(t, failureBody, append([]string{"--agent", "--select", "id"}, cmd.args...)...)
			require.Equal(t, 6, code, out)
			_, hasData := failure["data"]
			require.False(t, hasData, out)
			require.Equal(t, selected, responseObject(t, failure), out)

			code, failure, out = run(t, failureBody, append([]string{"--allow-partial-failure", "--json", "--select", "id"}, cmd.args...)...)
			require.Equal(t, 0, code, out)
			require.Equal(t, true, failure["success"])
			require.Equal(t, "errors", partialField(t, failure))
			require.Equal(t, selected, responseObject(t, failure), out)
			require.Contains(t, out, "warning: partial failure detected")

			code, failure, out = run(t, failureBody, append([]string{"--allow-partial-failure", "--json", "--compact"}, cmd.args...)...)
			require.Equal(t, 0, code, out)
			require.Equal(t, true, failure["success"])
			require.Equal(t, failureCompacted, responseObject(t, failure), out)

			code, _, out = run(t, failureBody, append([]string{"--json", "--select", "not-a-field"}, cmd.args...)...)
			require.Equal(t, 6, code, out)
			code, _, out = run(t, failureBody, append([]string{"--allow-partial-failure", "--json", "--select", "not-a-field"}, cmd.args...)...)
			require.Equal(t, 2, code, out)
			require.Contains(t, out, "matched no fields")
		})
	}
}

func TestRESTCommandsIgnoreGraphQLFailureShapes(t *testing.T) {
	t.Parallel()

	box := &scriptedJSON{}
	srv := httptest.NewServer(box)
	t.Cleanup(srv.Close)

	apiSpec := restMutationSpec("restfail", srv.URL)
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	helpers := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	require.NotContains(t, helpers, "detectGraphQLFailure")
	require.Contains(t, helpers, "partialFailureFields := []string{\"partialFailureError\"}")
	_, err := os.Stat(filepath.Join(outputDir, "internal", "cli", "graphql_failure.go"))
	require.True(t, os.IsNotExist(err))
	require.Contains(t, readGeneratedFile(t, outputDir, "internal", "cli", "items_create.go"), "detectPartialFailure(data)")

	bin := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", bin, "./cmd/"+naming.CLI(apiSpec.Name))
	home := t.TempDir()

	box.set(`{"errors":[{"message":"boom"}],"data":{"thing":{"__typename":"ThingError"}}}`)
	code, out := runPrintedCLI(t, bin, printedCLIEnv(home), "--json", "items", "create", "--title", "t")
	require.Equal(t, 0, code, out)
	env := decodeCLIObject(t, out)
	require.Equal(t, true, env["success"])
	_, ok := env["partial_failure"]
	require.False(t, ok, out)

	box.set(`{"partialFailureError":{"code":3,"message":"batch failed"}}`)
	code, out = runPrintedCLI(t, bin, printedCLIEnv(home), "--json", "items", "create", "--title", "t")
	require.Equal(t, 6, code, out)
	env = decodeCLIObject(t, out)
	require.Equal(t, false, env["success"])
	require.Equal(t, "partialFailureError", partialField(t, env))
}

func graphqlFailureSpec(name, baseURL string, withCreate bool) *spec.APISpec {
	notes := map[string]spec.Endpoint{
		"list": {Method: "POST", Path: "/graphql", Description: "List notes"},
	}
	if withCreate {
		notes["create"] = spec.Endpoint{
			Method:      "POST",
			Path:        "/graphql",
			Description: "Create an issue",
			Body:        []spec.Param{{Name: "title", Type: "string", Required: true, Description: "Title"}},
		}
	}
	s := &spec.APISpec{
		Name:      name,
		Version:   "0.1.0",
		BaseURL:   baseURL,
		Owner:     "test-owner",
		OwnerName: "Test Author",
		Auth:      spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"notes": {Description: "Notes", Endpoints: notes},
			"checkout": {
				Description: "Checkout",
				Endpoints: map[string]spec.Endpoint{
					"start": {
						Method:      "POST",
						Path:        "/graphql",
						Description: "Start checkout",
						Body:        []spec.Param{{Name: "cartId", Type: "string", Required: true, Description: "Cart"}},
					},
				},
			},
		},
	}
	s.Learn.Disabled = true
	return s
}

func restMutationSpec(name, baseURL string) *spec.APISpec {
	s := &spec.APISpec{
		Name:      name,
		Version:   "0.1.0",
		BaseURL:   baseURL,
		Owner:     "test-owner",
		OwnerName: "Test Author",
		Auth:      spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
					"create": {
						Method:      "POST",
						Path:        "/items",
						Description: "Create an item",
						Body:        []spec.Param{{Name: "title", Type: "string", Required: true, Description: "Title"}},
					},
				},
			},
		},
	}
	s.Learn.Disabled = true
	return s
}

type scriptedJSON struct {
	mu   sync.Mutex
	body []byte
	hits atomic.Int32
}

func (s *scriptedJSON) set(body string) {
	s.mu.Lock()
	s.body = []byte(body)
	s.mu.Unlock()
}

func (s *scriptedJSON) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.hits.Add(1)
	s.mu.Lock()
	body := append([]byte(nil), s.body...)
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func printedCLIEnv(home string) []string {
	return replaceEnv(harnessFreeEnv(),
		"HOME="+home,
		"USERPROFILE="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
	)
}

func replaceEnv(base []string, overrides ...string) []string {
	ov := make(map[string]string, len(overrides))
	for _, item := range overrides {
		k, v, _ := strings.Cut(item, "=")
		ov[k] = v
	}
	out := make([]string, 0, len(base)+len(overrides))
	seen := make(map[string]bool, len(base))
	for _, item := range base {
		k, _, _ := strings.Cut(item, "=")
		if _, ok := ov[k]; ok || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, item)
	}
	for k, v := range ov {
		out = append(out, k+"="+v)
	}
	return out
}

func decodeCLIObject(t *testing.T, out string) map[string]any {
	t.Helper()
	i := strings.Index(out, "{")
	require.GreaterOrEqual(t, i, 0, "no json object in output:\n%s", out)
	dec := json.NewDecoder(strings.NewReader(out[i:]))
	var m map[string]any
	require.NoError(t, dec.Decode(&m), "output:\n%s", out)
	return m
}

func responseObject(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	if _, ok := env["action"]; ok {
		for _, key := range []string{"results", "data"} {
			raw, exists := env[key]
			if !exists {
				continue
			}
			payload, ok := raw.(map[string]any)
			require.True(t, ok, "%s type %T in %#v", key, raw, env)
			return payload
		}
		t.Fatalf("failure envelope missing data/results: %#v", env)
	}
	if raw, ok := env["results"].(map[string]any); ok {
		return raw
	}
	return env
}

func partialField(t *testing.T, env map[string]any) string {
	t.Helper()
	pf, ok := env["partial_failure"].(map[string]any)
	require.True(t, ok, "partial_failure missing: %#v", env)
	field, _ := pf["field"].(string)
	return field
}

func partialMessage(t *testing.T, env map[string]any) string {
	t.Helper()
	pf, ok := env["partial_failure"].(map[string]any)
	require.True(t, ok, "partial_failure missing: %#v", env)
	msg, _ := pf["message"].(string)
	return msg
}
