package generator

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

const promotedDeleteSpecName = "promoted-delete-missing"

// TestPromotedDeleteIgnoreMissing runs a generated CLI whose delete-only
// resource is promoted, against a stub that answers 404 and 500. Endpoint
// deletes already route those errors through classifyDeleteError; the
// promoted command has to do the same.
func TestPromotedDeleteIgnoreMissing(t *testing.T) {
	t.Parallel()

	fx := buildPromotedDeleteCLI(t)

	approvals := readGeneratedFile(t, fx.dir, "internal", "cli", "promoted_approvals.go")
	grants := readGeneratedFile(t, fx.dir, "internal", "cli", "promoted_grants.go")
	widgets := readGeneratedFile(t, fx.dir, "internal", "cli", "promoted_widgets.go")
	itemsDelete := readGeneratedFile(t, fx.dir, "internal", "cli", "items_delete.go")
	helpers := readGeneratedFile(t, fx.dir, "internal", "cli", "helpers.go")

	require.Contains(t, approvals, "return classifyDeleteError(cmd.OutOrStdout(), err, flags)")
	require.NotContains(t, approvals, "return classifyAPIError(cmd.OutOrStdout(), err, flags)")
	require.Contains(t, grants, "return classifyDeleteError(cmd.OutOrStdout(), err, flags)")
	require.NotContains(t, grants, "return classifyAPIError(cmd.OutOrStdout(), err, flags)")
	require.Contains(t, widgets, "return classifyAPIError(cmd.OutOrStdout(), err, flags)")
	require.NotContains(t, widgets, "classifyDeleteError")
	require.Contains(t, itemsDelete, "return classifyDeleteError(cmd.OutOrStdout(), err, flags)")
	require.Contains(t, helpers, "func classifyDeleteError(")
	require.Contains(t, helpers, "flags.ignoreMissing")

	requireGeneratedCompiles(t, fx.dir)
	runGoCommand(t, fx.dir, "build", "-o", fx.binary, "./cmd/"+naming.CLI(promotedDeleteSpecName))

	const (
		notFoundHint = "hint: resource not found. Run the 'list' command to see available items"
		deletedProse = "already deleted (no-op)\n"
	)
	notFound := func(method, path string) string {
		return method + " " + path + " returned HTTP 404: {\"error\":\"not found\"}\n" + notFoundHint
	}
	serverErr := func(method, path string) string {
		return method + " " + path + " returned HTTP 500: {\"error\":\"boom\"}"
	}

	t.Run("promoted 404 with ignore-missing", func(t *testing.T) {
		code, stdout, stderr, hits := fx.run(t, "approvals", "missing", "--ignore-missing", "--yes", "--json")
		require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assertAlreadyDeleted(t, stdout)
		assertDeleteHits(t, hits, http.MethodDelete, "/approvals/missing", 1)
		require.NotContains(t, stderr, "Error:")
	})

	t.Run("endpoint 404 with ignore-missing matches promoted", func(t *testing.T) {
		promotedCode, promotedOut, promotedErr, promotedHits := fx.run(t, "approvals", "missing", "--ignore-missing", "--yes", "--json")
		endpointCode, endpointOut, endpointErr, endpointHits := fx.run(t, "items", "delete", "missing", "--ignore-missing", "--yes", "--json")
		require.Equal(t, 0, promotedCode, "stdout:\n%s\nstderr:\n%s", promotedOut, promotedErr)
		require.Equal(t, 0, endpointCode, "stdout:\n%s\nstderr:\n%s", endpointOut, endpointErr)
		assertAlreadyDeleted(t, promotedOut)
		require.Equal(t, promotedOut, endpointOut)
		assertDeleteHits(t, promotedHits, http.MethodDelete, "/approvals/missing", 1)
		assertDeleteHits(t, endpointHits, http.MethodDelete, "/items/missing", 1)
		require.NotContains(t, endpointErr, "Error:")
	})

	t.Run("promoted 404 without ignore-missing", func(t *testing.T) {
		code, stdout, stderr, hits := fx.run(t, "approvals", "missing", "--yes", "--json")
		require.Equal(t, 3, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assertCLIError(t, stdout, 3, notFound(http.MethodDelete, "/approvals/missing"))
		assertDeleteHits(t, hits, http.MethodDelete, "/approvals/missing", 1)
		require.Contains(t, stderr, notFoundHint)
	})

	t.Run("promoted lowercase delete 404 with ignore-missing", func(t *testing.T) {
		code, stdout, stderr, hits := fx.run(t, "grants", "missing", "--ignore-missing", "--yes", "--json")
		require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assertAlreadyDeleted(t, stdout)
		assertDeleteHits(t, hits, http.MethodDelete, "/grants/missing", 1)
		require.NotContains(t, stderr, "Error:")
	})

	t.Run("promoted lowercase delete 404 without ignore-missing", func(t *testing.T) {
		code, stdout, stderr, hits := fx.run(t, "grants", "missing", "--yes", "--json")
		require.Equal(t, 3, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assertCLIError(t, stdout, 3, notFound(http.MethodDelete, "/grants/missing"))
		assertDeleteHits(t, hits, http.MethodDelete, "/grants/missing", 1)
		require.Contains(t, stderr, notFoundHint)
	})

	t.Run("endpoint 404 without ignore-missing", func(t *testing.T) {
		code, stdout, stderr, hits := fx.run(t, "items", "delete", "missing", "--yes", "--json")
		require.Equal(t, 3, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assertCLIError(t, stdout, 3, notFound(http.MethodDelete, "/items/missing"))
		assertDeleteHits(t, hits, http.MethodDelete, "/items/missing", 1)
		require.Contains(t, stderr, notFoundHint)
	})

	t.Run("promoted 500 with ignore-missing", func(t *testing.T) {
		code, stdout, stderr, hits := fx.run(t, "approvals", "boom", "--ignore-missing", "--yes", "--json")
		require.Equal(t, 5, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assertCLIError(t, stdout, 5, serverErr(http.MethodDelete, "/approvals/boom"))
		assertDeleteHits(t, hits, http.MethodDelete, "/approvals/boom", 0)
		require.NotContains(t, stdout, "already_deleted")
	})

	t.Run("endpoint 500 with ignore-missing", func(t *testing.T) {
		code, stdout, stderr, hits := fx.run(t, "items", "delete", "boom", "--ignore-missing", "--yes", "--json")
		require.Equal(t, 5, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assertCLIError(t, stdout, 5, serverErr(http.MethodDelete, "/items/boom"))
		assertDeleteHits(t, hits, http.MethodDelete, "/items/boom", 0)
		require.NotContains(t, stdout, "already_deleted")
	})

	t.Run("prose ignore-missing matches endpoint delete", func(t *testing.T) {
		promotedCode, promotedOut, promotedErr, promotedHits := fx.run(t, "approvals", "missing", "--ignore-missing", "--yes")
		endpointCode, endpointOut, endpointErr, endpointHits := fx.run(t, "items", "delete", "missing", "--ignore-missing", "--yes")
		require.Equal(t, 0, promotedCode, "stdout:\n%s\nstderr:\n%s", promotedOut, promotedErr)
		require.Equal(t, 0, endpointCode, "stdout:\n%s\nstderr:\n%s", endpointOut, endpointErr)
		require.Equal(t, deletedProse, promotedOut)
		require.Equal(t, promotedOut, endpointOut)
		assertDeleteHits(t, promotedHits, http.MethodDelete, "/approvals/missing", 1)
		assertDeleteHits(t, endpointHits, http.MethodDelete, "/items/missing", 1)
	})

	t.Run("promoted post ignores ignore-missing", func(t *testing.T) {
		withoutCode, withoutOut, withoutErr, withoutHits := fx.run(t, "widgets", "--name", "Ada", "--yes", "--json")
		withCode, withOut, withErr, withHits := fx.run(t, "widgets", "--name", "Ada", "--ignore-missing", "--yes", "--json")
		require.Equal(t, 3, withoutCode, "stdout:\n%s\nstderr:\n%s", withoutOut, withoutErr)
		require.Equal(t, 3, withCode, "stdout:\n%s\nstderr:\n%s", withOut, withErr)
		assertCLIError(t, withoutOut, 3, notFound(http.MethodPost, "/widgets"))
		require.Equal(t, withoutOut, withOut)
		require.Equal(t, withoutErr, withErr)
		assertDeleteHits(t, withoutHits, http.MethodPost, "/widgets", 1)
		assertDeleteHits(t, withHits, http.MethodPost, "/widgets", 1)
		require.NotContains(t, withOut, "already_deleted")
	})
}

type promotedDeleteHit struct {
	method string
	path   string
}

type promotedDeleteCLI struct {
	dir    string
	binary string
	env    []string
	mu     sync.Mutex
	hits   []promotedDeleteHit
}

func buildPromotedDeleteCLI(t *testing.T) *promotedDeleteCLI {
	t.Helper()

	fx := &promotedDeleteCLI{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fx.mu.Lock()
		fx.hits = append(fx.hits, promotedDeleteHit{method: r.Method, path: r.URL.Path})
		fx.mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/boom"):
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"boom"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}
	}))
	t.Cleanup(server.Close)

	apiSpec := minimalSpec(promotedDeleteSpecName)
	apiSpec.Learn.Disabled = true
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.BaseURL = server.URL
	apiSpec.Resources = map[string]spec.Resource{
		"approvals": {
			Description: "Approvals",
			Endpoints: map[string]spec.Endpoint{
				"remove": {
					Method:      "DELETE",
					Path:        "/approvals/{id}",
					Description: "Remove an approval",
					Params: []spec.Param{{
						Name: "id", Type: "string", Required: true, Positional: true, PathParam: true,
					}},
				},
			},
		},
		"grants": {
			Description: "Grants",
			Endpoints: map[string]spec.Endpoint{
				"revoke": {
					Method:      "delete",
					Path:        "/grants/{id}",
					Description: "Revoke a grant",
					Params: []spec.Param{{
						Name: "id", Type: "string", Required: true, Positional: true, PathParam: true,
					}},
				},
			},
		},
		"widgets": {
			Description: "Widgets",
			Endpoints: map[string]spec.Endpoint{
				"create": {
					Method:      "POST",
					Path:        "/widgets",
					Description: "Create a widget",
					Body:        []spec.Param{{Name: "name", Type: "string"}},
				},
			},
		},
		"items": {
			Description: "Items",
			Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/items", Description: "List items"},
				"delete": {
					Method:      "DELETE",
					Path:        "/items/{id}",
					Description: "Delete an item",
					Params: []spec.Param{{
						Name: "id", Type: "string", Required: true, Positional: true, PathParam: true,
					}},
				},
			},
		},
	}

	fx.dir = filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, fx.dir).Generate())
	fx.binary = filepath.Join(fx.dir, naming.CLI(apiSpec.Name))

	home := t.TempDir()
	fx.env = append(isolatedQueryTestEnv(home), naming.EnvPrefix(apiSpec.Name)+"_BASE_URL="+server.URL)
	return fx
}

func (fx *promotedDeleteCLI) run(t *testing.T, args ...string) (int, string, string, []promotedDeleteHit) {
	t.Helper()
	fx.mu.Lock()
	fx.hits = nil
	fx.mu.Unlock()

	cmd := exec.Command(fx.binary, args...)
	cmd.Env = fx.env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	hits := fx.snapshot()
	if err == nil {
		return 0, stdout.String(), stderr.String(), hits
	}
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	return exitErr.ExitCode(), stdout.String(), stderr.String(), hits
}

func (fx *promotedDeleteCLI) snapshot() []promotedDeleteHit {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	out := make([]promotedDeleteHit, len(fx.hits))
	copy(out, fx.hits)
	return out
}

func assertAlreadyDeleted(t *testing.T, stdout string) {
	t.Helper()
	require.Equal(t, map[string]any{
		"status": "noop",
		"reason": "already_deleted",
	}, decodeJSONObject(t, stdout))
}

func assertCLIError(t *testing.T, stdout string, code int, message string) {
	t.Helper()
	require.Equal(t, map[string]any{
		"code":  float64(code),
		"error": message,
	}, decodeJSONObject(t, stdout), "stdout:\n%s", stdout)
}

// exact is the required hit count. A non-positive exact allows the one or
// more attempts a retried 500 produces without pinning the retry budget.
func assertDeleteHits(t *testing.T, hits []promotedDeleteHit, method, path string, exact int) {
	t.Helper()
	if exact > 0 {
		require.Len(t, hits, exact, "hits: %+v", hits)
	} else {
		require.NotEmpty(t, hits, "expected %s %s", method, path)
	}
	for _, hit := range hits {
		require.Equal(t, method, hit.method, "hits: %+v", hits)
		require.Equal(t, path, hit.path, "hits: %+v", hits)
	}
}
