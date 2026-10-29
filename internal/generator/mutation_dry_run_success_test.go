package generator

import (
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestGeneratedMutationDryRunSuccess runs a generated mutating command.
// A dry run that built the request reports success with no HTTP status.
// A dry run that fails validation exits non-zero with success false.
func TestGeneratedMutationDryRunSuccess(t *testing.T) {
	t.Parallel()

	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"note-1","name":"Ada"}`))
	}))
	t.Cleanup(server.Close)

	apiSpec := &spec.APISpec{
		Name:      "mutdry",
		Version:   "0.1.0",
		BaseURL:   server.URL,
		Owner:     "test-owner",
		OwnerName: "Test Author",
		Auth:      spec.AuthConfig{Type: "none"},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/mutdry-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"notes": {
				Description: "Notes",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/notes", Description: "List notes"},
					"create": {
						Method:      "POST",
						Path:        "/notes",
						Description: "Create a note",
						Params: []spec.Param{
							{Name: "visibility", Type: "string", Enum: []string{"public", "private"}, Description: "Visibility"},
						},
						Body: []spec.Param{
							{Name: "name", Type: "string", Required: true, Description: "Note name"},
						},
					},
				},
			},
		},
	}
	apiSpec.Learn.Disabled = true

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	createSrc := readGeneratedFile(t, outputDir, "internal", "cli", "notes_create.go")
	require.Contains(t, createSrc, "markCompletedDryRun(envelope)")
	require.Contains(t, createSrc, "dryRunValidationErr(cmd, flags,")
	require.NotContains(t, createSrc, `envelope["status"] = 0`)
	helpers := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	require.Contains(t, helpers, "func markCompletedDryRun(")
	require.Contains(t, helpers, "func dryRunValidationErr(")
	require.Contains(t, helpers, "`json:\"success\"`")

	bin := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", bin, "./cmd/"+naming.CLI(apiSpec.Name))
	env := mutationDryRunEnv(t, naming.EnvPrefix(apiSpec.Name), server.URL)

	t.Run("completed json", func(t *testing.T) {
		before := hits.Load()
		code, stdout, stderr := runMutationDryRun(t, bin, env, "notes", "create", "--name", "Ada", "--dry-run", "--json")
		require.Equal(t, 0, code, "stderr:\n%s\nstdout:\n%s", stderr, stdout)
		require.Equal(t, before, hits.Load(), "completed dry run must not dial")
		require.Contains(t, stderr, "(dry run - no request sent)")
		assertCompletedDryRun(t, stdout, "post")
	})

	t.Run("completed agent", func(t *testing.T) {
		before := hits.Load()
		code, stdout, stderr := runMutationDryRun(t, bin, env, "notes", "create", "--name", "Ada", "--dry-run", "--agent")
		require.Equal(t, 0, code, "stderr:\n%s\nstdout:\n%s", stderr, stdout)
		require.Equal(t, before, hits.Load(), "completed agent dry run must not dial")
		assertCompletedDryRun(t, stdout, "post")
	})

	t.Run("validation failure json", func(t *testing.T) {
		before := hits.Load()
		code, stdout, stderr := runMutationDryRun(t, bin, env, "notes", "create", "--dry-run", "--json")
		require.Equal(t, 2, code, "stderr:\n%s\nstdout:\n%s", stderr, stdout)
		require.Equal(t, before, hits.Load(), "invalid dry run must not dial")
		assertDryRunValidationFailure(t, stdout)
		require.Contains(t, stderr, `required flag "name" not set`)
	})

	t.Run("validation failure agent", func(t *testing.T) {
		code, stdout, stderr := runMutationDryRun(t, bin, env, "notes", "create", "--dry-run", "--agent")
		require.Equal(t, 2, code, "stderr:\n%s\nstdout:\n%s", stderr, stdout)
		assertDryRunValidationFailure(t, stdout)
	})

	t.Run("validation failure enum", func(t *testing.T) {
		before := hits.Load()
		code, stdout, stderr := runMutationDryRun(t, bin, env, "notes", "create", "--name", "Ada", "--visibility", "nope", "--dry-run", "--json")
		require.NotEqual(t, 0, code, "stderr:\n%s\nstdout:\n%s", stderr, stdout)
		require.Equal(t, before, hits.Load(), "invalid enum dry run must not dial")
		assertDryRunValidationFailure(t, stdout)
		require.Contains(t, stdout, "invalid value")
	})

	t.Run("validation failure human", func(t *testing.T) {
		code, stdout, stderr := runMutationDryRun(t, bin, env, "notes", "create", "--dry-run")
		require.Equal(t, 2, code, "stderr:\n%s\nstdout:\n%s", stderr, stdout)
		require.NotContains(t, stdout, `"success"`)
		require.Contains(t, stderr, `required flag "name" not set`)
	})

	t.Run("live missing input is not a dry-run envelope", func(t *testing.T) {
		code, stdout, stderr := runMutationDryRun(t, bin, env, "notes", "create", "--json")
		require.Equal(t, 2, code, "stderr:\n%s\nstdout:\n%s", stderr, stdout)
		require.NotContains(t, stdout, `"dry_run"`)
		require.NotContains(t, stdout, `"success": true`)
		require.NotContains(t, stdout, `"success":true`)
	})

	t.Run("live success keeps http status", func(t *testing.T) {
		before := hits.Load()
		code, stdout, stderr := runMutationDryRun(t, bin, env, "notes", "create", "--name", "Ada", "--json")
		require.Equal(t, 0, code, "stderr:\n%s\nstdout:\n%s", stderr, stdout)
		require.Equal(t, before+1, hits.Load(), "live create must dial")
		payload := decodeJSONObject(t, stdout)
		require.Equal(t, true, payload["success"], "stdout: %s", stdout)
		require.Equal(t, float64(http.StatusOK), payload["status"], "stdout: %s", stdout)
		_, dry := payload["dry_run"]
		require.False(t, dry, "live create must not wear dry_run: %s", stdout)
	})

	t.Run("verify noop stays unsuccessful", func(t *testing.T) {
		before := hits.Load()
		verifyEnv := append(append([]string{}, env...), "PRINTING_PRESS_VERIFY=1")
		code, stdout, stderr := runMutationDryRun(t, bin, verifyEnv, "notes", "create", "--name", "Ada", "--dry-run", "--json")
		require.Equal(t, 0, code, "stderr:\n%s\nstdout:\n%s", stderr, stdout)
		require.Equal(t, before, hits.Load(), "verify dry run must not dial")
		payload := decodeJSONObject(t, stdout)
		require.Equal(t, true, payload["dry_run"], "stdout: %s", stdout)
		require.Equal(t, true, payload["verify_noop"], "stdout: %s", stdout)
		require.Equal(t, false, payload["success"], "stdout: %s", stdout)
		_, hasStatus := payload["status"]
		require.False(t, hasStatus, "verify dry-run envelope must not carry an HTTP status: %s", stdout)
	})
}

func assertCompletedDryRun(t *testing.T, stdout, action string) {
	t.Helper()
	payload := decodeJSONObject(t, stdout)
	require.Equal(t, true, payload["dry_run"], "stdout: %s", stdout)
	require.Equal(t, true, payload["success"], "stdout: %s", stdout)
	require.Equal(t, action, payload["action"], "stdout: %s", stdout)
	_, hasStatus := payload["status"]
	require.False(t, hasStatus, "dry-run envelope must not carry an HTTP status: %s", stdout)
}

func assertDryRunValidationFailure(t *testing.T, stdout string) {
	t.Helper()
	payload := decodeJSONObject(t, stdout)
	require.Equal(t, false, payload["success"], "stdout: %s", stdout)
	require.Equal(t, true, payload["dry_run"], "stdout: %s", stdout)
	_, hasStatus := payload["status"]
	require.False(t, hasStatus, "validation dry-run envelope must not carry an HTTP status: %s", stdout)
	errText, _ := payload["error"].(string)
	require.NotEmpty(t, errText, "stdout: %s", stdout)
}

func mutationDryRunEnv(t *testing.T, prefix, baseURL string) []string {
	t.Helper()
	env := frameworkDryRunEnv(t, t.TempDir(), prefix)
	return append(withoutVerifyEnv(env), prefix+"_BASE_URL="+baseURL)
}

func withoutVerifyEnv(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "PRINTING_PRESS_VERIFY", "PRINTING_PRESS_VERIFY_LIVE_HTTP", "PRINTING_PRESS_DOGFOOD":
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func TestSpecEmitsDryRunValidation(t *testing.T) {
	t.Parallel()

	require.False(t, specEmitsDryRunValidation(nil, nil, nil))

	readOnly := minimalSpec("dry-val-read")
	assertDryRunValidation(t, readOnly, false)

	defaulted := minimalSpec("dry-val-default")
	defaulted.Resources["items"].Endpoints["list"] = spec.Endpoint{
		Method: "GET",
		Path:   "/items",
		Params: []spec.Param{{Name: "view", Type: "string", Required: true, Default: "all"}},
	}
	assertDryRunValidation(t, defaulted, false)

	getBody := minimalSpec("dry-val-get-body")
	getBody.Resources["items"] = spec.Resource{
		Description: "Items",
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method: "GET",
				Path:   "/items",
				Body:   []spec.Param{{Name: "name", Type: "string", Required: true}},
			},
			"get": {Method: "GET", Path: "/items/{id}", Description: "Get one"},
		},
	}
	assertDryRunValidation(t, getBody, false)

	optionsOnly := minimalSpec("dry-val-options")
	optionsOnly.Resources["items"] = spec.Resource{
		Endpoints: map[string]spec.Endpoint{
			"list":    {Method: "GET", Path: "/items"},
			"options": {Method: "OPTIONS", Path: "/items", Params: []spec.Param{{Name: "q", Type: "string", Required: true}}},
		},
	}
	assertDryRunValidation(t, optionsOnly, false)

	requiredFlag := minimalSpec("dry-val-flag")
	requiredFlag.Resources["items"].Endpoints["list"] = spec.Endpoint{
		Method: "GET",
		Path:   "/items",
		Params: []spec.Param{{Name: "q", Type: "string", Required: true}},
	}
	assertDryRunValidation(t, requiredFlag, true)

	enumParam := minimalSpec("dry-val-enum")
	enumParam.Resources["items"].Endpoints["list"] = spec.Endpoint{
		Method: "GET",
		Path:   "/items",
		Params: []spec.Param{{Name: "visibility", Type: "string", Enum: []string{"public", "private"}}},
	}
	assertDryRunValidation(t, enumParam, true)

	requiredBody := minimalSpec("dry-val-body")
	requiredBody.Resources["items"] = spec.Resource{
		Endpoints: map[string]spec.Endpoint{
			"list": {Method: "GET", Path: "/items"},
			"create": {
				Method: "POST",
				Path:   "/items",
				Body:   []spec.Param{{Name: "name", Type: "string", Required: true}},
			},
		},
	}
	assertDryRunValidation(t, requiredBody, true)

	unsatisfiable := minimalSpec("dry-val-enum-body")
	unsatisfiable.Resources["items"] = spec.Resource{
		Endpoints: map[string]spec.Endpoint{
			"list": {Method: "GET", Path: "/items"},
			"create": {
				Method: "POST",
				Path:   "/items",
				Body:   []spec.Param{{Name: "tone", Type: "string", EnumUnsatisfiable: true}},
			},
		},
	}
	assertDryRunValidation(t, unsatisfiable, true)

	rawBody := minimalSpec("dry-val-raw")
	rawBody.Resources["items"] = spec.Resource{
		Endpoints: map[string]spec.Endpoint{
			"upload": {
				Method:             "POST",
				Path:               "/items",
				RequestContentType: "text/plain",
				BodyRequired:       true,
			},
		},
	}
	assertDryRunValidation(t, rawBody, true)

	promotedGetBody := minimalSpec("dry-val-promoted-get")
	promotedGetBody.Resources["items"] = spec.Resource{
		Endpoints: map[string]spec.Endpoint{
			"list": {
				Method: "GET",
				Path:   "/items",
				Body:   []spec.Param{{Name: "name", Type: "string", Required: true}},
			},
		},
	}
	assertDryRunValidation(t, promotedGetBody, true)
}

func assertDryRunValidation(t *testing.T, apiSpec *spec.APISpec, want bool) {
	t.Helper()
	promoted, _, names := buildPromotedCommandPlan(apiSpec)
	require.Equal(t, want, specEmitsDryRunValidation(apiSpec, promoted, names))
}

func TestDryRunHelpersFollowCallSites(t *testing.T) {
	t.Parallel()

	readOnly := minimalSpec("dry-helper-read")
	readOnly.Learn.Disabled = true
	readOnly.Auth = spec.AuthConfig{Type: "none"}
	readDir := filepath.Join(t.TempDir(), naming.CLI(readOnly.Name))
	readGen := New(readOnly, readDir)
	readGen.VisionSet = VisionTemplateSet{Export: true, MCP: true}
	require.NoError(t, readGen.Generate())
	requireGeneratedCompiles(t, readDir)
	readHelpers := readGeneratedFile(t, readDir, "internal", "cli", "helpers.go")
	require.Contains(t, readHelpers, "func markCompletedDryRun(")
	require.NotContains(t, readHelpers, "func dryRunValidationErr(")

	stored := minimalSpec("dry-helper-store")
	stored.Learn.Disabled = true
	stored.Auth = spec.AuthConfig{Type: "none"}
	storeDir := filepath.Join(t.TempDir(), naming.CLI(stored.Name))
	storeGen := New(stored, storeDir)
	// The generated server imports internal/mcp. Store-only emission leaves
	// that package test-only, so ./... cannot compile.
	storeGen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, storeGen.Generate())
	requireGeneratedCompiles(t, storeDir)
	storeHelpers := readGeneratedFile(t, storeDir, "internal", "cli", "helpers.go")
	require.NotContains(t, storeHelpers, "func markCompletedDryRun(")
	require.NotContains(t, storeHelpers, "func dryRunValidationErr(")
}

func runMutationDryRun(t *testing.T, bin string, env []string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return 0, stdout.String(), stderr.String()
	}
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "stdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	return exitErr.ExitCode(), stdout.String(), stderr.String()
}
