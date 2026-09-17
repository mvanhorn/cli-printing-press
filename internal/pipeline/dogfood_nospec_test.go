package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunDogfood_NoSpecMarksPathAndAuthSkipped(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "store"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
func newRootCmd() {}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "widgets.go"), `package cli
func newWidgetsCmd() {}
func wireWidgets() { _ = newWidgetsCmd() }
func widgetsList() {
	path := "/widgets"
	_ = path
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), "package client\n")
	writeTestFile(t, filepath.Join(dir, "internal", "store", "store.go"), "package store\n")

	report, err := RunDogfood(dir, "")
	require.NoError(t, err)

	assert.Equal(t, DogfoodSpecSourceNone, report.SpecSource)
	assert.Empty(t, report.SpecPath)
	assert.True(t, report.PathCheck.Skipped)
	assert.Contains(t, report.PathCheck.Detail, "no resolvable spec")
	assert.Contains(t, report.PathCheck.Detail, "1 command(s) unvalidated")
	assert.Zero(t, report.PathCheck.Tested)
	assert.Zero(t, report.PathCheck.Valid)
	assert.True(t, report.AuthCheck.Skipped)
	assert.False(t, report.AuthCheck.Match)
	assert.Contains(t, report.AuthCheck.Detail, "spec not provided")
	assert.NotEqual(t, "FAIL", report.Verdict)

	raw, err := json.Marshal(report)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(raw, &payload))
	assert.Equal(t, "none", payload["spec_source"])
	pathCheck, ok := payload["path_check"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, pathCheck["skipped"])
	authCheck, ok := payload["auth_check"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, authCheck["skipped"])
	assert.Equal(t, false, authCheck["match"])
}

func TestRunDogfood_AllInvalidPathsStayUnskipped(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "store"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
type rootFlags struct{}
func initFlags(flags *rootFlags) { _ = flags }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "widgets_list.go"), `package cli
func widgetsList() {
	path := "/not-in-spec"
	_ = path
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
func authHeader(token string) string { return "Bearer " + token }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "store", "store.go"), "package store\n")

	specPath := filepath.Join(dir, "spec.json")
	writeTestFile(t, specPath, `{
  "openapi": "3.0.0",
  "info": {"title": "Widgets", "version": "1.0"},
  "paths": {
    "/widgets": {
      "get": {
        "operationId": "listWidgets",
        "responses": {"200": {"description": "ok"}}
      }
    }
  },
  "components": {
    "securitySchemes": {
      "BearerAuth": {"type": "http", "scheme": "bearer"}
    }
  }
}`)

	report, err := RunDogfood(dir, specPath)
	require.NoError(t, err)

	assert.Equal(t, DogfoodSpecSourceBundled, report.SpecSource)
	assert.False(t, report.PathCheck.Skipped)
	assert.Greater(t, report.PathCheck.Tested, 0)
	assert.Zero(t, report.PathCheck.Pct)
	assert.Equal(t, 0, report.PathCheck.Valid)
}

func TestDeriveDogfoodVerdict_NoSpecDoesNotFail(t *testing.T) {
	t.Parallel()

	report := passingDogfoodReport()
	report.SpecSource = DogfoodSpecSourceNone
	report.PathCheck = PathCheckResult{Skipped: true, Detail: "no resolvable spec; 2 command(s) unvalidated"}
	report.AuthCheck = AuthCheckResult{Match: false, Skipped: true, Detail: "spec not provided; auth protocol check skipped"}
	assert.Equal(t, "PASS", deriveDogfoodVerdict(report, false))
}
