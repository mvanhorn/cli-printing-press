package pipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	apispec "github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunVerify_CleansTransientArtifactsButKeepsCache(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sample-cli")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "sample-cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "library", "sample-cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".cache", "go-build"), 0o755))

	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/sample-cli\n\ngo 1.26.1\n")
	writeTestFile(t, filepath.Join(dir, "cmd", "sample-cli", "main.go"), `package main
func main() {}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
func initRoot() {
	rootCmd.AddCommand(newUsersListCmd())
}
`)
	writeTestFile(t, filepath.Join(dir, "cmd", "library", "sample-cli", "main.go"), "package recursive\n")
	writeTestFile(t, filepath.Join(dir, ".DS_Store"), "finder")
	writeTestFile(t, filepath.Join(dir, ".cache", "go-build", "index"), "cache")

	report, err := RunVerify(VerifyConfig{Dir: dir})
	require.NoError(t, err)
	assert.Equal(t, "PASS", report.Verdict)
	assert.FileExists(t, report.Binary)

	assert.FileExists(t, filepath.Join(dir, "sample-cli"))
	assert.NoDirExists(t, filepath.Join(dir, "cmd", "library"))
	assert.NoFileExists(t, filepath.Join(dir, ".DS_Store"))
	assert.DirExists(t, filepath.Join(dir, ".cache"))
}

func TestRunVerify_KeepsExistingBinaryWhenRebuildFails(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sample-cli")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "sample-cli"), 0o755))

	existingBinary := filepath.Join(dir, "sample-cli")
	require.NoError(t, os.WriteFile(existingBinary, []byte("previous-build"), 0o755))
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/sample-cli\n\ngo 1.26.1\n")
	writeTestFile(t, filepath.Join(dir, "cmd", "sample-cli", "main.go"), `package main
func main() {
	this will not compile
}
`)

	report, err := RunVerify(VerifyConfig{Dir: dir})
	require.Error(t, err)
	assert.Nil(t, report)
	assert.FileExists(t, existingBinary)
}

func TestFindCLICommandDirResolvesRelativeDirFromCLIRoot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sample-cli")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "sample-cli"), 0o755))
	writeTestFile(t, filepath.Join(dir, "cmd", "sample-cli", "main.go"), `package main
func main() {}
`)

	t.Chdir(dir)

	cmdDir, err := findCLICommandDir(".")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "cmd", "sample-cli"), cmdDir)
}

func TestRunFreshnessContractTestPassesGeneratedSurface(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cliutil"), 0o755))
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "auto_refresh.go"), `package cli
var readCommandResources = map[string][]string{"demo-pp-cli items list": {"items"}}
func ensureFreshForResources() {}
func ensureFreshForCommand() {}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cliutil", "freshness.go"), `package cliutil
type FreshnessMeta struct {}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "helpers.go"), `package cli
func wrap() { meta["freshness"] = nil }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "data_source.go"), `package cli
func resolveRead() {
	switch flags.dataSource {
	case "live":
		data, err := c.Get(path, params)
		if err != nil { return }
		return data, attachFreshness(DataProvenance{Source: "live"}, flags), nil
	}
}
`)

	result := runFreshnessContractTest(dir)
	assert.Equal(t, "PASS", result.Verdict)
	assert.True(t, result.Metadata)
	assert.True(t, result.LiveBypass)
	assert.True(t, result.HelperSurface)
	assert.Greater(t, result.RegisteredPaths, 0)
}

func TestRunFreshnessContractTestSkipsWhenNotEmitted(t *testing.T) {
	result := runFreshnessContractTest(t.TempDir())
	assert.Equal(t, "SKIP", result.Verdict)
	assert.False(t, result.Enabled)
}

func TestRunBrowserSessionProofTestRequiresValidDoctorProof(t *testing.T) {
	binary := buildDoctorJSONBinary(t, `{"browser_session_proof":"missing or stale","browser_session_proof_detail":"proof not found"}`)

	result := runBrowserSessionProofTest(binary, apispec.AuthConfig{
		RequiresBrowserSession:       true,
		BrowserSessionValidationPath: "/api/items",
	})

	assert.Equal(t, 0, result.Score)
	assert.False(t, result.Execute)
	assert.Contains(t, result.Error, "proof not found")
}

func TestRunBrowserSessionProofTestPassesValidDoctorProof(t *testing.T) {
	binary := buildDoctorJSONBinary(t, `{"browser_session_proof":"valid","browser_session_proof_detail":"GET /api/items verified"}`)

	result := runBrowserSessionProofTest(binary, apispec.AuthConfig{
		RequiresBrowserSession:       true,
		BrowserSessionValidationPath: "/api/items",
	})

	assert.Equal(t, 3, result.Score)
	assert.True(t, result.Execute)
	assert.Empty(t, result.Error)
}

// TestRunBrowserSessionProofTestPropagatesVerifyEnv guards the fix for
// shipcheck FAIL-ing on cookie-auth CLIs: doctor must see
// PRINTING_PRESS_VERIFY=1 so its synthetic browser-session proof
// short-circuit fires. The stub binary returns a valid proof only when
// the env var is set; without the probe's env augmentation it would
// score 0.
func TestRunBrowserSessionProofTestPropagatesVerifyEnv(t *testing.T) {
	binary := buildVerifyEnvDoctorBinary(t)
	// Fully unset PRINTING_PRESS_VERIFY for the duration of the test
	// rather than t.Setenv(key, ""), which would leave PRINTING_PRESS_VERIFY=
	// in os.Environ() and let it shadow the probe's later "=1" entry on
	// platforms where the first env occurrence wins.
	prev, had := os.LookupEnv("PRINTING_PRESS_VERIFY")
	require.NoError(t, os.Unsetenv("PRINTING_PRESS_VERIFY"))
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("PRINTING_PRESS_VERIFY", prev)
		}
	})

	result := runBrowserSessionProofTest(binary, apispec.AuthConfig{
		RequiresBrowserSession:       true,
		BrowserSessionValidationPath: "/api/items",
	})

	assert.Equal(t, 3, result.Score)
	assert.True(t, result.Execute)
	assert.Empty(t, result.Error)
}

func TestRunCommandTestsExecutesMockReadCommands(t *testing.T) {
	binary := buildCommandProbeBinary(t)
	cmd := discoveredCommand{Name: "items", Kind: "read"}

	result := runCommandTests(binary, cmd, "mock", os.Environ())
	assert.True(t, result.Help)
	assert.True(t, result.DryRun)
	assert.False(t, result.Execute)
}

func TestRunCommandTestsUsesHappyArgsAnnotation(t *testing.T) {
	binary := buildHappyArgsProbeBinary(t)
	cmd := discoveredCommand{
		Name: "whereabouts",
		Kind: "read",
		Args: []string{"mock-value"},
		Annotations: map[string]string{
			happyArgsAnnotation: "<person>=Alice;--query=sunset",
		},
	}

	result := runCommandTests(binary, cmd, "mock", os.Environ())

	assert.True(t, result.Help)
	assert.True(t, result.DryRun)
	assert.True(t, result.Execute)
	assert.Equal(t, 3, result.Score)
}

func TestRunDataPipelineTestMockModeRequiresRows(t *testing.T) {
	t.Run("fails when sync creates tables but stores no rows", func(t *testing.T) {
		binary := buildDataPipelineProbeBinary(t, 0)

		pass, detail := runDataPipelineTest(binary, "", "mock", os.Environ, 2)

		assert.False(t, pass)
		assert.Contains(t, detail, "1 domain tables created but 0 rows after sync")
	})

	t.Run("fails when nested mock sync stores fewer rows than served", func(t *testing.T) {
		binary := buildDataPipelineProbeBinary(t, 1)

		pass, detail := runDataPipelineTest(binary, "", "mock", os.Environ, 2)

		assert.False(t, pass)
		assert.Contains(t, detail, "items has 1 rows after sync, expected at least 2")
	})

	t.Run("passes when sync stores rows", func(t *testing.T) {
		binary := buildDataPipelineProbeBinary(t, 2)

		pass, detail := runDataPipelineTest(binary, "", "mock", os.Environ, 2)

		assert.True(t, pass)
		assert.Contains(t, detail, "items has 2 rows")
	})

	t.Run("falls back when sync rejects full flag", func(t *testing.T) {
		binary := buildNoFullSyncDataPipelineProbeBinary(t, 2)

		pass, detail := runDataPipelineTest(binary, "", "mock", os.Environ, 2)

		assert.True(t, pass)
		assert.Contains(t, detail, "items has 2 rows")
	})

	t.Run("reports unknown sync flag distinctly", func(t *testing.T) {
		binary := buildUnknownFlagSyncDataPipelineProbeBinary(t)

		pass, detail := runDataPipelineTest(binary, "", "mock", os.Environ, 2)

		assert.False(t, pass)
		assert.Equal(t, "FAIL: sync rejected flag --full", detail)
	})

	t.Run("skips no-store CLIs whose sync command is not the data pipeline", func(t *testing.T) {
		dir := seedDataPipelineCLIDir(t, true)
		require.NoError(t, WriteCLIManifest(dir, CLIManifest{SpecFormat: "browser-sniff"}))
		binary := buildUnknownFlagSyncDataPipelineProbeBinary(t)

		pass, detail := runDataPipelineTest(binary, dir, "mock", os.Environ, 2)

		assert.True(t, pass)
		assert.Equal(t, "SKIP (CLI has no local store)", detail)
	})

	t.Run("passes when an auxiliary table is empty before populated data table", func(t *testing.T) {
		binary := buildAuxiliaryFirstDataPipelineProbeBinary(t, 0, 2)

		pass, detail := runDataPipelineTest(binary, "", "mock", os.Environ, 2)

		assert.True(t, pass)
		assert.Contains(t, detail, "items has 2 rows")
	})

	t.Run("passes when an auxiliary table has fewer rows before populated data table", func(t *testing.T) {
		binary := buildAuxiliaryFirstDataPipelineProbeBinary(t, 1, 2)

		pass, detail := runDataPipelineTest(binary, "", "mock", os.Environ, 2)

		assert.True(t, pass)
		assert.Contains(t, detail, "items has 2 rows")
	})

	t.Run("fails when only an auxiliary table satisfies expected rows", func(t *testing.T) {
		binary := buildAuxiliaryFirstDataPipelineProbeBinary(t, 3, 0)

		pass, detail := runDataPipelineTest(binary, "", "mock", os.Environ, 2)

		assert.False(t, pass)
		assert.Contains(t, detail, "items has 0 rows")
	})

	t.Run("warns when sync command is absent", func(t *testing.T) {
		binary := buildNoSyncDataPipelineProbeBinary(t)

		pass, detail := runDataPipelineTest(binary, "", "mock", os.Environ, 2)

		assert.True(t, pass)
		assert.Equal(t, "WARN: no sync command — data-pipeline check skipped", detail)
	})

	t.Run("fails when sync command exists but crashes", func(t *testing.T) {
		binary := buildFailingSyncDataPipelineProbeBinary(t)

		pass, detail := runDataPipelineTest(binary, "", "mock", os.Environ, 2)

		assert.False(t, pass)
		assert.Equal(t, "FAIL: sync crashed", detail)
	})
}

func TestUnknownSyncFlagIgnoresEmptyFlagName(t *testing.T) {
	flag, ok := unknownSyncFlag(errors.New("unknown flag: "))

	assert.False(t, ok)
	assert.Empty(t, flag)
}

func TestRunDataPipelineTestSkipsUnsyncableCLIs(t *testing.T) {
	t.Run("skips local-datastore manifests before sync", func(t *testing.T) {
		dir := seedDataPipelineCLIDir(t, true)
		require.NoError(t, WriteCLIManifest(dir, CLIManifest{SpecFormat: "sqlite"}))
		binary := buildNoSyncDataPipelineProbeBinary(t)

		pass, detail := runDataPipelineTest(binary, dir, "mock", os.Environ, 2)

		assert.True(t, pass)
		assert.Equal(t, "SKIP (local-datastore CLI: no network sync to verify)", detail)
	})

	t.Run("skips CLIs without sync command", func(t *testing.T) {
		dir := seedDataPipelineCLIDir(t, false)
		require.NoError(t, WriteCLIManifest(dir, CLIManifest{SpecFormat: "openapi3"}))
		binary := buildNoSyncDataPipelineProbeBinary(t)

		pass, detail := runDataPipelineTest(binary, dir, "mock", os.Environ, 2)

		assert.True(t, pass)
		assert.Equal(t, "SKIP (CLI has no sync command)", detail)
	})

	t.Run("runs normal sync CLIs", func(t *testing.T) {
		dir := seedDataPipelineCLIDir(t, true)
		require.NoError(t, WriteCLIManifest(dir, CLIManifest{SpecFormat: "openapi3"}))
		seedDataPipelineStore(t, dir, true)
		binary := buildDataPipelineProbeBinary(t, 2)

		pass, detail := runDataPipelineTest(binary, dir, "mock", os.Environ, 2)

		assert.True(t, pass)
		assert.Contains(t, detail, "items has 2 rows")
	})

	t.Run("mock mode skips row requirement for stores without syncable resources", func(t *testing.T) {
		dir := seedDataPipelineCLIDir(t, true)
		require.NoError(t, WriteCLIManifest(dir, CLIManifest{SpecFormat: "browser-sniff"}))
		seedDataPipelineStore(t, dir, false)
		binary := buildDataPipelineProbeBinary(t, 0)

		pass, detail := runDataPipelineTest(binary, dir, "mock", os.Environ, 2)

		assert.True(t, pass)
		assert.Equal(t, "PASS: 1 domain tables created (mock mode; no syncable resources declared)", detail)
	})

	t.Run("mock mode still fails zero rows when syncable resources exist", func(t *testing.T) {
		dir := seedDataPipelineCLIDir(t, true)
		require.NoError(t, WriteCLIManifest(dir, CLIManifest{SpecFormat: "openapi3"}))
		seedDataPipelineStore(t, dir, true)
		binary := buildDataPipelineProbeBinary(t, 0)

		pass, detail := runDataPipelineTest(binary, dir, "mock", os.Environ, 2)

		assert.False(t, pass)
		assert.Contains(t, detail, "1 domain tables created but 0 rows after sync")
	})
}

func TestFinalizeVerifyReportFailsRequiredDataPipeline(t *testing.T) {
	report := &VerifyReport{
		DataPipeline:       false,
		DataPipelineDetail: "FAIL: 1 domain tables created but 0 rows after sync (mock mode)",
		Results: []CommandResult{{
			Command: "items",
			Score:   3,
		}},
	}

	finalizeVerifyReport(report, 80, true)

	assert.Equal(t, "FAIL", report.Verdict)
}

func TestStartMockServerServesNestedDataEnvelopeFixtureFromSpec(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "spec.yaml")
	writeTestFile(t, specPath, `openapi: 3.0.0
info:
  title: Nested Data API
  version: "1.0"
paths:
  /users/{user_id}/items:
    get:
      operationId: listItems
      responses:
        "200":
          $ref: "#/components/responses/ListItems"
components:
  responses:
    ListItems:
      description: ok
      content:
        application/json:
          schema:
            allOf:
              - $ref: "#/components/schemas/ListEnvelope"
  schemas:
    ListEnvelope:
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
                properties:
                  id:
                    type: integer
                  name:
                    type: string
            pagination:
              type: object
`)
	spec, err := loadDogfoodOpenAPISpec(specPath, "")
	require.NoError(t, err)

	server, baseURL := startMockServer(spec)
	defer server.Close()

	resp, err := http.Get(baseURL + "/users/mock-user/items")
	require.NoError(t, err)
	defer resp.Body.Close()

	var body struct {
		Success bool `json:"success"`
		Data    struct {
			Items      []map[string]any `json:"items"`
			Pagination map[string]any   `json:"pagination"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.True(t, body.Success)
	assert.Len(t, body.Data.Items, 2)
	assert.Equal(t, float64(2), body.Data.Pagination["total"])
}

func TestStartMockServerIgnoresNestedDataEnvelopeForNonJSONResponses(t *testing.T) {
	specPath := filepath.Join(t.TempDir(), "spec.yaml")
	writeTestFile(t, specPath, `openapi: 3.0.0
info:
  title: XML Data API
  version: "1.0"
paths:
  /items:
    get:
      operationId: listItems
      responses:
        "200":
          description: ok
          content:
            application/xml:
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
`)
	spec, err := loadDogfoodOpenAPISpec(specPath, "")
	require.NoError(t, err)
	assert.Empty(t, spec.NestedDataEnvelopes)

	server, baseURL := startMockServer(spec)
	defer server.Close()

	resp, err := http.Get(baseURL + "/items")
	require.NoError(t, err)
	defer resp.Body.Close()

	var body []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Len(t, body, 1)
}

func TestDetectNestedDataEnvelopeFixturesSortsHTTPMethods(t *testing.T) {
	spec := []byte(`openapi: 3.0.0
info:
  title: Method Order API
  version: "1.0"
paths:
  /items:
    patch:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: object
                    properties:
                      results:
                        type: array
                        items:
                          type: object
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: object
                    properties:
                      items:
                        type: array
                        items:
                          type: object
`)

	fixtures := detectNestedDataEnvelopeFixtures(spec)

	require.Contains(t, fixtures, "/items")
	assert.Equal(t, "items", fixtures["/items"].ArrayKey)
}

func TestRunCommandTestsWithoutHappyArgsKeepsGenericFailure(t *testing.T) {
	binary := buildHappyArgsProbeBinary(t)
	cmd := discoveredCommand{
		Name: "whereabouts",
		Kind: "read",
		Args: []string{"mock-value"},
	}

	result := runCommandTests(binary, cmd, "mock", os.Environ())

	assert.True(t, result.Help)
	assert.False(t, result.DryRun)
	assert.False(t, result.Execute)
	assert.Equal(t, 1, result.Score)
}

func TestRunSideEffectSafeCommandTestsUsesHappyArgsWithoutNoArgProbe(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "invocations.log")
	binary := buildHappyArgsProbeBinary(t)
	cmd := discoveredCommand{
		Name: "whereabouts",
		Kind: "read",
		Args: []string{"mock-value"},
		Annotations: map[string]string{
			happyArgsAnnotation: "<person>=Alice;--query=sunset",
		},
	}
	env := append(os.Environ(), "PP_PROBE_LOG="+logPath)

	result := runSideEffectSafeCommandTests(binary, cmd, env)

	assert.True(t, result.Help)
	assert.True(t, result.DryRun)
	assert.True(t, result.Execute)
	assert.Equal(t, 3, result.Score)

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "whereabouts\n")
}

// buildVerifyEnvDoctorBinary builds a stub printed-CLI binary whose
// `doctor --json` returns a valid browser-session proof only when its
// process sees PRINTING_PRESS_VERIFY=1; otherwise it reports the proof
// as missing. Used to verify env propagation from the verify probe.
func buildVerifyEnvDoctorBinary(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.go")
	writeTestFile(t, mainFile, `package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "doctor" && os.Args[2] == "--json" {
		if os.Getenv("PRINTING_PRESS_VERIFY") == "1" {
			fmt.Println(`+"`"+`{"browser_session_proof":"valid","browser_session_proof_detail":"synthetic"}`+"`"+`)
			return
		}
		fmt.Println(`+"`"+`{"browser_session_proof":"missing","browser_session_proof_detail":"PRINTING_PRESS_VERIFY not set"}`+"`"+`)
		return
	}
	os.Exit(1)
}
`)
	binaryPath := filepath.Join(dir, "test-cli")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, mainFile)
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "building test binary: %s", string(out))
	return binaryPath
}

func buildDoctorJSONBinary(t *testing.T, payload string) string {
	t.Helper()

	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.go")
	writeTestFile(t, mainFile, `package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "doctor" && os.Args[2] == "--json" {
		fmt.Println(`+"`"+payload+"`"+`)
		return
	}
	os.Exit(1)
}
`)
	binaryPath := filepath.Join(dir, "test-cli")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, mainFile)
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "building test binary: %s", string(out))
	return binaryPath
}

func buildCommandProbeBinary(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.go")
	writeTestFile(t, mainFile, `package main

import (
	"os"
)

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "--help" || arg == "--dry-run" {
			return
		}
	}
	os.Exit(1)
}
`)
	binaryPath := filepath.Join(dir, "test-cli")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, mainFile)
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "building test binary: %s", string(out))
	return binaryPath
}

func buildHappyArgsProbeBinary(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.go")
	writeTestFile(t, mainFile, `package main

import (
	"os"
	"strings"
)

func main() {
	args := os.Args[1:]
	if logPath := os.Getenv("PP_PROBE_LOG"); logPath != "" {
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString(strings.Join(args, " ") + "\n")
			_ = f.Close()
		}
	}
	if len(args) == 2 && args[0] == "whereabouts" && args[1] == "--help" {
		return
	}
	if len(args) == 1 && args[0] == "whereabouts" {
		os.Exit(42)
	}
	if len(args) != 5 || args[0] != "whereabouts" || args[1] != "Alice" || args[2] != "--query" || args[3] != "sunset" {
		os.Exit(1)
	}
	switch args[4] {
	case "--dry-run", "--json":
		return
	default:
		os.Exit(1)
	}
}
`)
	binaryPath := filepath.Join(dir, "test-cli")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, mainFile)
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "building test binary: %s", string(out))
	return binaryPath
}

func buildDataPipelineProbeBinary(t *testing.T, rowCount int) string {
	t.Helper()

	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.go")
	writeTestFile(t, mainFile, fmt.Sprintf(`package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		os.Exit(1)
	}
	switch args[0] {
	case "sync":
		dbPath := dbArg(args[1:])
		if dbPath == "" {
			os.Exit(1)
		}
		if err := os.WriteFile(dbPath+".marker", []byte(dbPath), 0o644); err != nil {
			os.Exit(1)
		}
		return
	case "sql":
		dbPath := dbArg(args[1:])
		if dbPath == "" {
			os.Exit(1)
		}
		usedDB, err := os.ReadFile(dbPath + ".marker")
		if err != nil || string(usedDB) != dbPath {
			os.Exit(1)
		}
		query := args[len(args)-1]
		if strings.Contains(query, "sqlite_master") {
			fmt.Println("items")
			return
		}
		if strings.Contains(query, "count(*)") {
			fmt.Println(%d)
			return
		}
	}
	os.Exit(1)
}

func dbArg(args []string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--db" {
			return args[i+1]
		}
	}
	return ""
}
`, rowCount))
	binaryPath := filepath.Join(dir, "test-cli")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, mainFile)
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "building test binary: %s", string(out))
	return binaryPath
}

func buildNoFullSyncDataPipelineProbeBinary(t *testing.T, rowCount int) string {
	t.Helper()

	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.go")
	writeTestFile(t, mainFile, fmt.Sprintf(`package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		os.Exit(1)
	}
	switch args[0] {
	case "sync":
		for _, arg := range args[1:] {
			if arg == "--full" {
				fmt.Fprintln(os.Stderr, "unknown flag: --full")
				os.Exit(1)
			}
		}
		dbPath := dbArg(args[1:])
		if dbPath == "" {
			os.Exit(1)
		}
		if err := os.WriteFile(dbPath+".marker", []byte(dbPath), 0o644); err != nil {
			os.Exit(1)
		}
		return
	case "sql":
		dbPath := dbArg(args[1:])
		if dbPath == "" {
			os.Exit(1)
		}
		usedDB, err := os.ReadFile(dbPath + ".marker")
		if err != nil || string(usedDB) != dbPath {
			os.Exit(1)
		}
		query := args[len(args)-1]
		if strings.Contains(query, "sqlite_master") {
			fmt.Println("items")
			return
		}
		if strings.Contains(query, "count(*)") {
			fmt.Println(%d)
			return
		}
	}
	os.Exit(1)
}

func dbArg(args []string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--db" {
			return args[i+1]
		}
	}
	return ""
}
`, rowCount))
	binaryPath := filepath.Join(dir, "test-cli")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, mainFile)
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "building test binary: %s", string(out))
	return binaryPath
}

func buildUnknownFlagSyncDataPipelineProbeBinary(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.go")
	writeTestFile(t, mainFile, `package main

import (
	"fmt"
	"os"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "sync" {
		for _, arg := range args[1:] {
			if arg == "--full" {
				fmt.Fprintln(os.Stderr, "unknown flag: --full")
				os.Exit(1)
			}
		}
		fmt.Fprintln(os.Stderr, "sync requires --full")
		os.Exit(1)
	}
	os.Exit(1)
}
`)
	binaryPath := filepath.Join(dir, "test-cli")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, mainFile)
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "building test binary: %s", string(out))
	return binaryPath
}

func seedDataPipelineCLIDir(t *testing.T, includeSync bool) string {
	t.Helper()

	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	if includeSync {
		writeTestFile(t, filepath.Join(cliDir, "root.go"), `package cli
func newRootCmd() { rootCmd.AddCommand(newSyncCmd(nil)) }
`)
		writeTestFile(t, filepath.Join(cliDir, "sync_bluray.go"), `package cli
func newSyncCmd(flags any) {}
`)
		return dir
	}
	writeTestFile(t, filepath.Join(cliDir, "root.go"), `package cli
func newRootCmd() {}
`)
	writeTestFile(t, filepath.Join(cliDir, "items.go"), `package cli
func newItemsCmd(flags any) {}
`)
	return dir
}

func seedDataPipelineStore(t *testing.T, dir string, syncableResources bool) {
	t.Helper()

	storeDir := filepath.Join(dir, "internal", "store")
	require.NoError(t, os.MkdirAll(storeDir, 0o755))
	content := `package store

func Open() {}
`
	if syncableResources {
		content += `
func defaultSyncResources() []string {
	return []string{"items"}
}
`
	} else {
		content += `
func defaultSyncResources() []string {
	return []string{}
}
`
	}
	writeTestFile(t, filepath.Join(storeDir, "store.go"), content)
}

func buildAuxiliaryFirstDataPipelineProbeBinary(t *testing.T, settingsRows, itemRows int) string {
	t.Helper()

	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.go")
	writeTestFile(t, mainFile, fmt.Sprintf(`package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		os.Exit(1)
	}
	switch args[0] {
	case "sync":
		dbPath := dbArg(args[1:])
		if dbPath == "" {
			os.Exit(1)
		}
		if err := os.WriteFile(dbPath+".marker", []byte(dbPath), 0o644); err != nil {
			os.Exit(1)
		}
		return
	case "sql":
		dbPath := dbArg(args[1:])
		if dbPath == "" {
			os.Exit(1)
		}
		usedDB, err := os.ReadFile(dbPath + ".marker")
		if err != nil || string(usedDB) != dbPath {
			os.Exit(1)
		}
		query := args[len(args)-1]
		if strings.Contains(query, "sqlite_master") {
			fmt.Println("settings")
			fmt.Println("items")
			return
		}
		if strings.Contains(query, "count(*)") {
			if strings.Contains(query, "\"settings\"") {
				fmt.Println(%d)
				return
			}
			if strings.Contains(query, "\"items\"") {
				fmt.Println(%d)
				return
			}
			os.Exit(1)
		}
	}
	os.Exit(1)
}

func dbArg(args []string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--db" {
			return args[i+1]
		}
	}
	return ""
}
`, settingsRows, itemRows))
	binaryPath := filepath.Join(dir, "test-cli")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, mainFile)
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "building test binary: %s", string(out))
	return binaryPath
}

func buildNoSyncDataPipelineProbeBinary(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.go")
	writeTestFile(t, mainFile, `package main

import (
	"fmt"
	"os"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "sync" {
		fmt.Fprintln(os.Stderr, "Error: unknown command \"sync\" for \"test-cli\"")
		os.Exit(1)
	}
	os.Exit(1)
}
`)
	binaryPath := filepath.Join(dir, "test-cli")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, mainFile)
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "building test binary: %s", string(out))
	return binaryPath
}

func buildFailingSyncDataPipelineProbeBinary(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.go")
	writeTestFile(t, mainFile, `package main

import (
	"fmt"
	"os"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "sync" {
		fmt.Fprintln(os.Stderr, "sync failed while contacting mock API")
		os.Exit(1)
	}
	os.Exit(1)
}
`)
	binaryPath := filepath.Join(dir, "test-cli")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, mainFile)
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "building test binary: %s", string(out))
	return binaryPath
}

func TestDiscoverCommands_UsesHelpOutputWhenBinaryAvailable(t *testing.T) {
	// Create a minimal CLI directory with root.go (for fallback path).
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
func initRoot() {
	rootCmd.AddCommand(newIEconItems440Cmd())
	rootCmd.AddCommand(newPlayerCmd())
}
`)

	// Build a tiny binary that prints a fake --help with Available Commands.
	binDir := t.TempDir()
	mainFile := filepath.Join(binDir, "main.go")
	writeTestFile(t, mainFile, `package main

import "fmt"

func main() {
	fmt.Println("A test CLI")
	fmt.Println("")
	fmt.Println("Available Commands:")
	fmt.Println("  iecon-items-440  Get economy items for app 440")
	fmt.Println("  player           Get player info")
	fmt.Println("  completion       Generate completion script")
	fmt.Println("  help             Help about any command")
	fmt.Println("")
	fmt.Println("Flags:")
	fmt.Println("  -h, --help   help for test-cli")
}
`)
	binaryPath := filepath.Join(binDir, "test-cli")
	buildCmd := exec.Command("go", "build", "-o", binaryPath, mainFile)
	out, err := buildCmd.CombinedOutput()
	require.NoError(t, err, "building test binary: %s", string(out))

	commands := discoverCommands(dir, binaryPath)

	// Should use help output: iecon-items-440 (not camelToKebab's iecon-items440).
	assert.Len(t, commands, 2)
	names := make([]string, len(commands))
	for i, c := range commands {
		names[i] = c.Name
	}
	assert.Contains(t, names, "iecon-items-440")
	assert.Contains(t, names, "player")
}

func TestDiscoverCommands_FallsBackToSourceWhenBinaryMissing(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
func initRoot() {
	rootCmd.AddCommand(newUsersListCmd())
	rootCmd.AddCommand(newProjectsGetCmd())
}
`)

	// Pass a non-existent binary path — should fall back to source parsing.
	commands := discoverCommands(dir, "/nonexistent/binary")

	assert.Len(t, commands, 2)
	names := make([]string, len(commands))
	for i, c := range commands {
		names[i] = c.Name
	}
	assert.Contains(t, names, "users-list")
	assert.Contains(t, names, "projects-get")
}

func TestDiscoverCommands_FallsBackToSourceWhenBinaryPathEmpty(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
func initRoot() {
	rootCmd.AddCommand(newUsersListCmd())
}
`)

	commands := discoverCommands(dir, "")
	assert.Len(t, commands, 1)
	assert.Equal(t, "users-list", commands[0].Name)
}

func TestParseHelpCommands(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name: "standard cobra help output",
			input: `A CLI for Steam Web API

Available Commands:
  iecon-items-440  Get economy items for app 440
  player           Get player info
  completion       Generate completion script
  help             Help about any command

Flags:
  -h, --help   help for steam-pp-cli`,
			expected: []string{"iecon-items-440", "player"},
		},
		{
			name:     "empty output",
			input:    "",
			expected: nil,
		},
		{
			name: "no available commands section",
			input: `A CLI for something

Flags:
  -h, --help   help for something`,
			expected: nil,
		},
		{
			name: "single command",
			input: `Available Commands:
  users  Manage users

Flags:
  -h, --help   help`,
			expected: []string{"users"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			commands := parseHelpCommands(tt.input)
			if tt.expected == nil {
				assert.Empty(t, commands)
				return
			}
			names := make([]string, len(commands))
			for i, c := range commands {
				names[i] = c.Name
			}
			assert.Equal(t, tt.expected, names)
		})
	}
}

func TestBuildCLI_UsesCanonicalCommandDirForClaimedOutput(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sample-pp-cli-2")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "sample-pp-cli"), 0o755))

	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/sample-pp-cli\n\ngo 1.26.1\n")
	writeTestFile(t, filepath.Join(dir, "cmd", "sample-pp-cli", "main.go"), `package main
func main() {}
`)

	binaryPath, err := buildCLI(dir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "sample-pp-cli-2"), binaryPath)
	assert.FileExists(t, binaryPath)
}

func TestBuildCLIArgsUseReproducibleBuildFlags(t *testing.T) {
	args := buildCLIArgs("out-bin", "./cmd/sample-pp-cli")

	assert.Equal(t, []string{"build", "-trimpath", "-ldflags=-buildid=", "-o", "out-bin", "./cmd/sample-pp-cli"}, args)
}

// TestExtractPositionalPlaceholders covers the placeholder extractor used
// by inferPositionalArgs. The bracketed-flag-descriptor cases are the
// retro #301 F2 regression: cobra Use strings like
// `save <url> [--tags=<csv>] [--stdin]` were leaking `<csv>` as a phantom
// positional, which then violated MaximumNArgs(1) on save.
func TestExtractPositionalPlaceholders(t *testing.T) {
	tests := []struct {
		name  string
		usage string
		want  []string
	}{
		{
			name:  "single required positional",
			usage: " <url> [flags]",
			want:  []string{"url"},
		},
		{
			name:  "single optional positional",
			usage: " [id] [flags]",
			want:  []string{"id"},
		},
		{
			name:  "required plus optional positional",
			usage: " <id> [extra] [flags]",
			want:  []string{"id", "extra"},
		},
		{
			name:  "no positionals",
			usage: " [flags]",
			want:  nil,
		},
		{
			name:  "skips [command]",
			usage: " [command] [flags]",
			want:  nil,
		},
		{
			name:  "F2: bracketed flag descriptor with placeholder is not a positional",
			usage: " <url> [--tags=<csv>] [--stdin] [flags]",
			want:  []string{"url"},
		},
		{
			name:  "F2: multiple bracketed flag descriptors",
			usage: " [--name=<n>] [--limit=<int>] [flags]",
			want:  nil,
		},
		{
			name:  "F2: short-form flag descriptor",
			usage: " <q> [-v] [flags]",
			want:  []string{"q"},
		},
		{
			name:  "F2: spaced flag descriptor body",
			usage: " <id> [ --debug ] [flags]",
			want:  []string{"id"},
		},
		{
			name:  "lowercases names",
			usage: " <Region> [flags]",
			want:  []string{"region"},
		},
		{
			name:  "pipe alternative chooses first id-shaped option",
			usage: " <name|id|uuid> [flags]",
			want:  []string{"id"},
		},
		{
			name:  "pipe alternative falls back to first option",
			usage: " <query|text> [flags]",
			want:  []string{"query"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPositionalPlaceholders(tt.usage)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseHappyArgsAnnotation(t *testing.T) {
	got := parseHappyArgsAnnotation(" <person>= Alice ; --query= sunset ; --limit=10 ; invalid ; name=Bob ; --bad ")

	assert.Equal(t, []string{"Alice"}, got.positionals)
	assert.Equal(t, []string{"--query", "sunset", "--limit", "10"}, got.flags)
}

func TestMergeHappyPositionalsOverlaysInOrder(t *testing.T) {
	assert.Equal(t,
		[]string{"Alice", "mock-date"},
		mergeHappyPositionals([]string{"mock-person", "mock-date"}, []string{"Alice"}),
	)
	assert.Equal(t,
		[]string{"Alice", "2026-05-22"},
		mergeHappyPositionals([]string{"mock-person"}, []string{"Alice", "2026-05-22"}),
	)
}

func TestMergeHappyFlagsOverlaysByFlagName(t *testing.T) {
	assert.Equal(t,
		[]string{"--query", "sunset", "--limit", "10"},
		mergeHappyFlags([]string{"--query", "mock-query"}, []string{"--query", "sunset", "--limit", "10"}),
	)
}

func TestSyntheticArgValue(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"type", "collection"},
		{"entity-type", "collection"},
		{"resource", "items"},
		{"format", "json"},
		{"category", "general"},
		{"action", "list"},
		{"status", "active"},
		// Existing mappings still work
		{"query", "mock-query"},
		{"id", "12345"},
		{"region", "mock-city"},
		// Unknown falls back to mock-value
		{"unknown-placeholder", "mock-value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, syntheticArgValue(tt.name))
		})
	}
}

func TestResolvePositionalValue_SpecDefaultWins(t *testing.T) {
	defaults := map[string]string{
		"servings": "4",
		"category": "weeknight",        // overrides the per-name switch's "general"
		"slug":     "real-recipe-slug", // overrides "general"
		"airport":  "PDX",
	}
	assert.Equal(t, "4", resolvePositionalValue("servings", defaults))
	// Spec default beats canonicalargs (which has no servings entry).
	assert.Equal(t, "weeknight", resolvePositionalValue("category", defaults))
	// Spec default beats the legacy syntheticArgValue switch.
	assert.Equal(t, "real-recipe-slug", resolvePositionalValue("slug", defaults))
	// Case insensitive.
	assert.Equal(t, "PDX", resolvePositionalValue("AIRPORT", defaults))
}

func TestResolvePositionalValue_FallsThroughToCanonicalargs(t *testing.T) {
	// nil paramDefaults — the next step is canonicalargs.
	assert.Equal(t, "2026-01-01", resolvePositionalValue("since", nil))
	assert.Equal(t, "2026-12-31", resolvePositionalValue("until", nil))
	assert.Equal(t, "mock-tag", resolvePositionalValue("tag", nil))
	assert.Equal(t, "mock-vertical", resolvePositionalValue("vertical", nil))
}

func TestResolvePositionalValue_FallsThroughToLegacySwitch(t *testing.T) {
	// Names not in canonicalargs but present in syntheticArgValue's
	// per-name switch must keep returning their calibrated values.
	assert.Equal(t, "mock-query", resolvePositionalValue("query", nil))
	assert.Equal(t, "/mock/path", resolvePositionalValue("url", nil))
	assert.Equal(t, "general", resolvePositionalValue("slug", nil))
	assert.Equal(t, "12345", resolvePositionalValue("id", nil))
}

func TestResolvePositionalValue_CatchAll(t *testing.T) {
	assert.Equal(t, "mock-value", resolvePositionalValue("airport_code", nil))
	assert.Equal(t, "mock-value", resolvePositionalValue("totally-novel-name", nil))
}

// Spec defaults registered with the same canonical key (lowercase, trimmed)
// as a canonicalargs entry must still win — verifies the lookup order.
func TestResolvePositionalValue_SpecDefaultBeatsCanonicalargs(t *testing.T) {
	defaults := map[string]string{"tag": "real-tag-value"}
	assert.Equal(t, "real-tag-value", resolvePositionalValue("tag", defaults))
}

// An empty-string default in the map must NOT short-circuit; the lookup
// chain continues to canonicalargs / syntheticArgValue. Empty defaults
// signal "spec author left it blank", not "use blank".
func TestResolvePositionalValue_EmptyDefaultDoesNotMaskFallback(t *testing.T) {
	defaults := map[string]string{"tag": ""}
	assert.Equal(t, "mock-tag", resolvePositionalValue("tag", defaults))
}

func TestHelpScanIndicatesSideEffect(t *testing.T) {
	binaryPath := buildHelpScanFixture(t, `Open a recipe in your default browser.

Usage:
  fixture-pp-cli open <slug>

Examples:
  fixture-pp-cli open recipes-1`)
	cmd := &discoveredCommand{Name: "open"}
	assert.True(t, helpScanIndicatesSideEffect(binaryPath, cmd),
		"help text mentioning 'browser' should be classified as side-effecting")
}

func TestHelpScanReturnsFalseForBenignHelp(t *testing.T) {
	binaryPath := buildHelpScanFixture(t, `List recipes.

Usage:
  fixture-pp-cli list

Examples:
  fixture-pp-cli list --json`)
	cmd := &discoveredCommand{Name: "list"}
	assert.False(t, helpScanIndicatesSideEffect(binaryPath, cmd),
		"benign help text should not be classified as side-effecting")
}

func TestSourceScanIndicatesSideEffect_DetectsExecOpen(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	body := `package cli
import "os/exec"
func openHandler(url string) error {
    return exec.Command("open", url).Run()
}`
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "open.go"), []byte(body), 0o644))

	cmd := &discoveredCommand{Name: "open"}
	assert.True(t, sourceScanIndicatesSideEffect(cmd, dir))
}

func TestSourceScanIndicatesSideEffect_DetectsPkgBrowserImport(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	body := `package cli
import "github.com/pkg/browser"
func openHandler(url string) error {
    return browser.OpenURL(url)
}`
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "open.go"), []byte(body), 0o644))

	cmd := &discoveredCommand{Name: "open"}
	assert.True(t, sourceScanIndicatesSideEffect(cmd, dir))
}

func TestSourceScanIndicatesSideEffect_IgnoresBenignHandler(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))
	body := `package cli
func listHandler() error { return nil }`
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "list.go"), []byte(body), 0o644))

	cmd := &discoveredCommand{Name: "list"}
	assert.False(t, sourceScanIndicatesSideEffect(cmd, dir))
}

// buildHelpScanFixture writes a tiny shell script that prints the given
// help text on stdout for any args, builds nothing, and returns its path.
// helpScanIndicatesSideEffect only cares about CombinedOutput, so a shell
// stub is enough — no need to compile a Go binary for each fixture.
func buildHelpScanFixture(t *testing.T, helpText string) string {
	t.Helper()
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "fixture-pp-cli")
	script := "#!/bin/sh\ncat <<'EOF'\n" + helpText + "\nEOF\n"
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))
	return scriptPath
}

func TestIsIntentionalStubExit(t *testing.T) {
	assert.True(t, isIntentionalStubExit(fmt.Errorf("exit 3: {\"cf_gated\":true,\"message\":\"stub command\"}")))
	assert.True(t, isIntentionalStubExit(fmt.Errorf("exit 3: {\"cf_gated\": true, \"message\":\"needs manual clearance\"}")))
	assert.False(t, isIntentionalStubExit(fmt.Errorf("exit 3: not implemented because this path is Cloudflare gated")))
	assert.False(t, isIntentionalStubExit(fmt.Errorf("exit 3: stubbed")))
	assert.False(t, isIntentionalStubExit(fmt.Errorf("exit 3: stub command")))
	assert.False(t, isIntentionalStubExit(fmt.Errorf("exit 3: resource not found")))
	assert.False(t, isIntentionalStubExit(nil))
}

func TestParseExitCodesFromHelp(t *testing.T) {
	tests := []struct {
		name string
		help string
		want map[int]bool
		ok   bool
	}{
		{
			name: "command help block",
			help: `Resolve capabilities.

Exit codes:
  0  at least one match found
  2  no confident match - fall back to help

Flags:
  -h, --help   help for which`,
			want: map[int]bool{0: true, 2: true},
			ok:   true,
		},
		{
			name: "next section without blank",
			help: `Resolve capabilities.

Exit codes:
  0  at least one match found
  2  no confident match
Examples:
  cli which search`,
			want: map[int]bool{0: true, 2: true},
			ok:   true,
		},
		{
			name: "malformed block",
			help: `Resolve capabilities.

Exit codes:
success only`,
			want: nil,
			ok:   false,
		},
		{
			name: "no block",
			help: `Resolve capabilities.

Flags:
  -h, --help   help for which`,
			want: nil,
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseExitCodesFromHelp(tt.help)
			assert.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestParseTypedExitCodesAnnotation(t *testing.T) {
	got, ok := parseTypedExitCodesAnnotation("0,2")
	assert.True(t, ok)
	assert.Equal(t, map[int]bool{0: true, 2: true}, got)

	got, ok = parseTypedExitCodesAnnotation("0, 2, 3")
	assert.True(t, ok)
	assert.Equal(t, map[int]bool{0: true, 2: true, 3: true}, got)

	_, ok = parseTypedExitCodesAnnotation("")
	assert.False(t, ok)
	_, ok = parseTypedExitCodesAnnotation("abc")
	assert.False(t, ok)
	_, ok = parseTypedExitCodesAnnotation("-1")
	assert.False(t, ok)
}

func TestTypedSuccessCodesAnnotationWinsOverHelp(t *testing.T) {
	cmd := discoveredCommand{
		Name: "which",
		Annotations: map[string]string{
			typedExitCodesAnnotation: "0,2",
		},
	}
	help := `Exit codes:
  0  success
  3  not found`

	assert.Equal(t, map[int]bool{0: true, 2: true}, typedSuccessCodes(cmd, help))
}

func TestTypedSuccessCodesMalformedAnnotationFallsBackToHelp(t *testing.T) {
	cmd := discoveredCommand{
		Name: "which",
		Annotations: map[string]string{
			typedExitCodesAnnotation: "0,nope",
		},
	}
	help := `Exit codes:
  0  success
  2  no confident match`

	assert.Equal(t, map[int]bool{0: true, 2: true}, typedSuccessCodes(cmd, help))
}

func TestIsDocumentedSuccessExit(t *testing.T) {
	err := exec.Command("sh", "-c", "exit 2").Run()
	require.Error(t, err)

	wrapped := fmt.Errorf("exit %w: no confident match", err)
	assert.True(t, isDocumentedSuccessExit(wrapped, map[int]bool{0: true, 2: true}))
	assert.False(t, isDocumentedSuccessExit(wrapped, map[int]bool{0: true}))
	assert.False(t, isDocumentedSuccessExit(nil, map[int]bool{0: true, 2: true}))
}

func TestEnrichCommandAnnotationsFromSource(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "which.go"), `package cli

import "github.com/spf13/cobra"

func newWhichCmd() *cobra.Command {
	return &cobra.Command{
		Use: "which [query]",
		Annotations: map[string]string{"pp:typed-exit-codes": "0,2"},
	}
}
`)

	commands := enrichCommandAnnotationsFromSource(dir, []discoveredCommand{{Name: "which"}})
	require.Len(t, commands, 1)
	assert.Equal(t, "0,2", commands[0].Annotations[typedExitCodesAnnotation])
}

func TestEnrichCommandAnnotationsFromSourceReadsHappyArgsOnly(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "whereabouts.go"), `package cli

import "github.com/spf13/cobra"

func newWhereaboutsCmd() *cobra.Command {
	return &cobra.Command{
		Use: "whereabouts <person>",
		Annotations: map[string]string{"pp:happy-args": "<person>=Alice;--query=sunset"},
	}
}
`)

	commands := enrichCommandAnnotationsFromSource(dir, []discoveredCommand{{Name: "whereabouts"}})
	require.Len(t, commands, 1)
	assert.Equal(t, "<person>=Alice;--query=sunset", commands[0].Annotations[happyArgsAnnotation])
}

func TestEnrichCommandAnnotationsFromSourceReadsAssignmentForm(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "whereabouts.go"), `package cli

import "github.com/spf13/cobra"

func newWhereaboutsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "whereabouts <person>",
	}
	cmd.Annotations = map[string]string{"pp:happy-args": "<person>=Alice;--query=sunset"}
	return cmd
}
`)

	commands := enrichCommandAnnotationsFromSource(dir, []discoveredCommand{{Name: "whereabouts"}})
	require.Len(t, commands, 1)
	assert.Equal(t, "<person>=Alice;--query=sunset", commands[0].Annotations[happyArgsAnnotation])
}

func TestEnrichCommandAnnotationsFromSourceKeepsReassignedCommandAnnotations(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "whereabouts.go"), `package cli

import "github.com/spf13/cobra"

func newWhereaboutsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "first <person>",
	}
	cmd.Annotations = map[string]string{"pp:happy-args": "<person>=Alice"}
	cmd = &cobra.Command{
		Use: "second <person>",
	}
	cmd.Annotations = map[string]string{"pp:happy-args": "<person>=Bob"}
	return cmd
}
`)

	commands := enrichCommandAnnotationsFromSource(dir, []discoveredCommand{{Name: "first"}, {Name: "second"}})
	require.Len(t, commands, 2)
	assert.Equal(t, "<person>=Alice", commands[0].Annotations[happyArgsAnnotation])
	assert.Equal(t, "<person>=Bob", commands[1].Annotations[happyArgsAnnotation])
}

func TestSyntheticFlagValue(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"org", "mock-owner"},
		{"repo", "mock-owner/mock-repo"},
		{"event", "mock-event-123"},
		{"game-id", "mock-event-123"},
		{"season", "2026"},
		{"sport", "mock-league"},
		{"ticker", "MOCK"},
		{"date", "2026-04-11"},
		{"since", "2026-01-01"},
		{"limit", "10"},
		{"status", "active"},
		// Case insensitive
		{"Event", "mock-event-123"},
		{"ORG", "mock-owner"},
		// Unknown falls back to mock-value
		{"unknown-flag", "mock-value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, syntheticFlagValue(tt.name))
		})
	}
}

func TestRequiredFlagsRegex(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		expected []string
	}{
		{
			name:     "single flag",
			output:   `Error: required flag(s) "event" not set`,
			expected: []string{"event"},
		},
		{
			name:     "multiple flags",
			output:   `Error: required flag(s) "event", "year" not set`,
			expected: []string{"event", "year"},
		},
		{
			name:     "three flags",
			output:   `Error: required flag(s) "org", "repo", "branch" not set`,
			expected: []string{"org", "repo", "branch"},
		},
		{
			name:     "no required-flags error",
			output:   `Error: unknown command "foo"`,
			expected: nil,
		},
		{
			name:     "surrounded by other output",
			output:   "Usage: cli foo [flags]\n\nError: required flag(s) \"event\" not set\nRun 'cli foo --help'",
			expected: []string{"event"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := requiredFlagsRe.FindStringSubmatch(tt.output)
			if tt.expected == nil {
				assert.Nil(t, m)
				return
			}
			require.NotNil(t, m)
			nameMatches := flagNameRe.FindAllStringSubmatch(m[1], -1)
			got := make([]string, 0, len(nameMatches))
			for _, nm := range nameMatches {
				got = append(got, nm[1])
			}
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestInferRequiredFlags_UnknownBinaryReturnsNil(t *testing.T) {
	// Probing a non-existent binary should return nil cleanly, not panic or hang.
	result := inferRequiredFlags("/nonexistent/binary/path", "somecmd")
	assert.Nil(t, result)
}

func TestParseSQLOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "simple table names one per line",
			input:    "bookings\nevent_types\nschedules\n",
			expected: []string{"bookings", "event_types", "schedules"},
		},
		{
			name:     "with header and separator",
			input:    "name\n---\nbookings\nevent_types\n",
			expected: []string{"bookings", "event_types"},
		},
		{
			name:     "empty output",
			input:    "",
			expected: nil,
		},
		{
			name:     "only whitespace and empty lines",
			input:    "\n  \n\n",
			expected: nil,
		},
		{
			name:     "with box-drawing characters",
			input:    "┌────────┐\n│ name   │\n├────────┤\n│bookings│\n└────────┘\n",
			expected: []string{"bookings"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseSQLOutput([]byte(tt.input))
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseCountOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "simple count",
			input:    "42\n",
			expected: 42,
		},
		{
			name:     "count with header",
			input:    "count(*)\n---\n15\n",
			expected: 15,
		},
		{
			name:     "zero count",
			input:    "0\n",
			expected: 0,
		},
		{
			name:     "empty output",
			input:    "",
			expected: 0,
		},
		{
			name:     "non-numeric output",
			input:    "error: no such table\n",
			expected: 0,
		},
		{
			name:     "box-drawn count",
			input:    "┌──────────┐\n│ count(*) │\n├──────────┤\n│ 42       │\n└──────────┘\n",
			expected: 42,
		},
		{
			name:     "pipe-wrapped count no spaces",
			input:    "│15│\n",
			expected: 15,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseCountOutput([]byte(tt.input))
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRunStructuralVerify(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sample-cli")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "sample-cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))

	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/sample-cli\n\ngo 1.26.1\n")
	writeTestFile(t, filepath.Join(dir, "cmd", "sample-cli", "main.go"), `package main
func main() {}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
func initRoot() {
	rootCmd.AddCommand(newUsersListCmd())
}
`)

	report, err := RunVerify(VerifyConfig{Dir: dir, NoSpec: true})
	require.NoError(t, err)
	assert.Equal(t, "structural", report.Mode)
	assert.Equal(t, "PASS", report.Verdict)
	assert.FileExists(t, report.Binary)
}

func TestSummarizeAuthEnvVarsKindAware(t *testing.T) {
	t.Setenv("REQUIRED_TOKEN", "")
	t.Setenv("SETUP_SECRET", "")
	t.Setenv("SESSION_COOKIE", "")

	got := summarizeAuthEnvVars([]apispec.AuthEnvVar{
		{Name: "REQUIRED_TOKEN", Kind: apispec.AuthEnvVarKindPerCall, Required: true},
		{Name: "OPTIONAL_TOKEN", Kind: apispec.AuthEnvVarKindPerCall, Required: false},
		{Name: "SETUP_SECRET", Kind: apispec.AuthEnvVarKindAuthFlowInput, Required: false},
		{Name: "SESSION_COOKIE", Kind: apispec.AuthEnvVarKindHarvested, Required: false},
	}, "", "", "live")

	require.Len(t, got, 4)
	assert.Equal(t, AuthEnvVarStatusMissingRequired, got[0].Status)
	assert.Equal(t, []AuthEnvVarStatus{got[0]}, missingRequiredAuthEnvVars(got))
	assert.Equal(t, AuthEnvVarStatusMissingInfo, got[1].Status)
	assert.Equal(t, AuthEnvVarStatusMissingInfo, got[2].Status)
	assert.Equal(t, AuthEnvVarStatusMissingInfo, got[3].Status)

	mock := summarizeAuthEnvVars([]apispec.AuthEnvVar{
		{Name: "REQUIRED_TOKEN", Kind: apispec.AuthEnvVarKindPerCall, Required: true},
	}, "", "", "mock")
	require.Len(t, mock, 1)
	assert.Equal(t, AuthEnvVarStatusOK, mock[0].Status)
}

func TestRequestAuthEnvVarNamesOnlyIncludesPerCallCredentials(t *testing.T) {
	got := requestAuthEnvVarNames([]apispec.AuthEnvVar{
		{Name: "REQUIRED_TOKEN", Kind: apispec.AuthEnvVarKindPerCall, Required: true},
		{Name: "LEGACY_EMPTY_KIND"},
		{Name: "SETUP_CLIENT_ID", Kind: apispec.AuthEnvVarKindAuthFlowInput},
		{Name: "SESSION_COOKIE", Kind: apispec.AuthEnvVarKindHarvested},
		{Name: "REQUIRED_TOKEN", Kind: apispec.AuthEnvVarKindPerCall, Required: true},
	})

	assert.Equal(t, []string{"REQUIRED_TOKEN", "LEGACY_EMPTY_KIND"}, got)
}

func TestSummarizeAuthEnvVarsAPIKeyOnlySatisfiesConfiguredEnvVar(t *testing.T) {
	t.Setenv("PRIMARY_TOKEN", "")
	t.Setenv("SECONDARY_TOKEN", "")

	got := summarizeAuthEnvVars([]apispec.AuthEnvVar{
		{Name: "PRIMARY_TOKEN", Kind: apispec.AuthEnvVarKindPerCall, Required: true},
		{Name: "SECONDARY_TOKEN", Kind: apispec.AuthEnvVarKindPerCall, Required: true},
	}, "secret", "PRIMARY_TOKEN", "live")

	require.Len(t, got, 2)
	assert.Equal(t, AuthEnvVarStatusOK, got[0].Status)
	assert.Equal(t, AuthEnvVarStatusMissingRequired, got[1].Status)
	assert.Equal(t, []AuthEnvVarStatus{got[1]}, missingRequiredAuthEnvVars(got))
}

func TestSummarizeAuthEnvVarsApiKeyWithoutCanonicalEnvVar(t *testing.T) {
	t.Setenv("PRIMARY_TOKEN", "")

	got := summarizeAuthEnvVars([]apispec.AuthEnvVar{
		{Name: "SETUP_CLIENT_ID", Kind: apispec.AuthEnvVarKindAuthFlowInput},
		{Name: "PRIMARY_TOKEN", Kind: apispec.AuthEnvVarKindPerCall, Required: true},
		{Name: "SESSION_COOKIE", Kind: apispec.AuthEnvVarKindHarvested},
	}, "secret", "", "live")

	require.Len(t, got, 3)
	assert.Equal(t, AuthEnvVarStatusMissingInfo, got[0].Status)
	assert.Equal(t, AuthEnvVarStatusOK, got[1].Status)
	assert.Equal(t, AuthEnvVarStatusMissingInfo, got[2].Status)
	assert.Empty(t, missingRequiredAuthEnvVars(got))
}

// TestDiscoverCLIEnvVars_SkipsTemplateVarReads guards the verifier integration
// for endpoint template vars: the helper must NOT report env vars that feed
// Config.TemplateVars (Shopify's SHOPIFY_SHOP / SHOPIFY_API_VERSION shape) as
// auth env vars, because the verifier's --api-key path overwrites every
// discovered name with the API key value, which would route requests at the
// API-key string instead of the configured shop.
func TestDiscoverCLIEnvVars_SkipsTemplateVarReads(t *testing.T) {
	dir := t.TempDir()
	configDir := filepath.Join(dir, "internal", "config")
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	// Mirror the shape that config.go.tmpl emits for a spec with both auth
	// env vars and EndpointTemplateVars: auth reads land in named fields,
	// template-var reads land in Config.TemplateVars.
	configBody := `package config

import "os"

type Config struct {
	BaseURL      string
	AccessToken  string
	TemplateVars map[string]string
}

func Load() *Config {
	cfg := &Config{}
	if v := os.Getenv("SHOPIFY_ACCESS_TOKEN"); v != "" {
		cfg.AccessToken = v
	}
	if v := os.Getenv("SHOPIFY_BASE_URL"); v != "" {
		cfg.BaseURL = v
	}
	if cfg.TemplateVars == nil {
		cfg.TemplateVars = map[string]string{}
	}
	if v := os.Getenv("SHOPIFY_SHOP"); v != "" {
		cfg.TemplateVars["shop"] = v
	}
	if v := os.Getenv("SHOPIFY_API_VERSION"); v != "" {
		cfg.TemplateVars["api_version"] = v
	}
	return cfg
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "config.go"), []byte(configBody), 0o644))

	got := discoverCLIEnvVars(dir)
	assert.Equal(t, []string{"SHOPIFY_ACCESS_TOKEN"}, got,
		"discoverCLIEnvVars must report only auth env vars; template-var reads must be excluded")

	gotTemplate := discoverCLITemplateVarEnvs(dir)
	assert.ElementsMatch(t, []string{"SHOPIFY_SHOP", "SHOPIFY_API_VERSION"}, gotTemplate,
		"discoverCLITemplateVarEnvs must return the template-var env names so mock mode can inject placeholder values")
}
