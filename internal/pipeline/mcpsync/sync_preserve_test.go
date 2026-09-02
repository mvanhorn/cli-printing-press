package mcpsync

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/mcpoverrides"
	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const handAuthoredOverride = "List operations with the required cursor and optional status filter for agents."

// TestSyncPreservesHandAuthoredMCPBehavior verifies that mcp-sync on a
// runtime-walking tree applies mcp-descriptions.json without deleting
// hand-authored poll loops, local-write-wins annotations, blocked
// destination flags, HTTP server timeouts, or leftover test helpers.
func TestSyncPreservesHandAuthoredMCPBehavior(t *testing.T) {
	t.Parallel()

	apiSpec := handAuthoredMCPSpec()
	cliDir := filepath.Join(t.TempDir(), "handmcp")
	gen := generator.New(apiSpec, cliDir)
	gen.VisionSet = generator.VisionTemplateSet{MCP: true, Store: true}
	require.NoError(t, gen.Generate())
	require.NoError(t, pipeline.WriteManifestForGenerate(pipeline.GenerateManifestParams{
		APIName:   apiSpec.Name,
		OutputDir: cliDir,
		Spec:      apiSpec,
	}))

	specData, err := yaml.Marshal(apiSpec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "spec.yaml"), specData, 0o644))

	state, err := pipeline.InspectMCPSurface(cliDir)
	require.NoError(t, err)
	require.Equal(t, pipeline.MCPSurfaceRuntime, state.State, "fixture must already be on the runtime walker")
	require.True(t, state.Pass, "fixture must already report MCP Surface PASS")

	require.NoError(t, plantHandAuthoredMCPBehavior(cliDir))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, mcpoverrides.Filename), []byte(`{
  "descriptions": {
    "operations_list": "`+handAuthoredOverride+`"
  }
}`), 0o644))

	_, err = Sync(cliDir, Options{})
	require.NoError(t, err)

	intentsAfter, err := os.ReadFile(filepath.Join(cliDir, "internal", "mcp", "intents.go"))
	require.NoError(t, err)
	intentsSrc := string(intentsAfter)
	assert.Contains(t, intentsSrc, "func waitForIntentPoll(", "mcp-sync must keep the hand-authored intent poll loop")
	assert.Contains(t, intentsSrc, "func intentDuration(", "mcp-sync must keep the intentDuration accessor")
	assert.Contains(t, intentsSrc, "pollAfter", "mcp-sync must keep pollAfter hints")
	assert.Contains(t, intentsSrc, "operationTimeout", "mcp-sync must keep operationTimeout hints")
	assert.Contains(t, intentsSrc, `waitForIntentPoll(ctx, c, "operations.status"`, "status step must keep polling instead of a single status call")

	walkerAfter, err := os.ReadFile(filepath.Join(cliDir, "internal", "mcp", "cobratree", "walker.go"))
	require.NoError(t, err)
	walkerSrc := string(walkerAfter)
	assert.Contains(t, walkerSrc, "localWrite := isMCPLocalWrite(cmd)", "local-write must be evaluated independently of read-only")
	assert.Contains(t, walkerSrc, "if localWrite {", "local-write must win when both annotations are present")
	assert.NotContains(t, walkerSrc, "if !readOnly && isMCPLocalWrite(cmd)", "mcp-sync must not invert the local-write vs read-only rule")

	shelloutAfter, err := os.ReadFile(filepath.Join(cliDir, "internal", "mcp", "cobratree", "shellout.go"))
	require.NoError(t, err)
	assert.Regexp(t, `"db"\s*:\s*true`, string(shelloutAfter), "blocked destination flags must keep db")

	classifyAfter, err := os.ReadFile(filepath.Join(cliDir, "internal", "mcp", "cobratree", "classify.go"))
	require.NoError(t, err)
	assert.Contains(t, string(classifyAfter), "func isMCPExecutionReadOnly(", "leftover classify helper must survive so go vet stays clean")

	mainAfter, err := os.ReadFile(filepath.Join(cliDir, "cmd", "handmcp-pp-mcp", "main.go"))
	require.NoError(t, err)
	mainSrc := string(mainAfter)
	assert.Contains(t, mainSrc, "ReadHeaderTimeout:", "HTTP server must keep ReadHeaderTimeout")
	assert.Contains(t, mainSrc, "ReadTimeout:", "HTTP server must keep ReadTimeout")
	assert.Contains(t, mainSrc, "WriteTimeout:", "HTTP server must keep WriteTimeout")
	assert.Contains(t, mainSrc, "IdleTimeout:", "HTTP server must keep IdleTimeout")
	assert.Contains(t, mainSrc, `os.Setenv("HANDMCP_LEARN_SURFACE", "mcp")`, "learn-event surface pin must survive")

	toolsManifest, err := os.ReadFile(filepath.Join(cliDir, pipeline.ToolsManifestFilename))
	require.NoError(t, err)
	assert.Contains(t, string(toolsManifest), handAuthoredOverride, "mcp-descriptions.json overrides must still reach tools-manifest.json")

	vet := exec.Command("go", "vet", "-mod=mod", "./internal/mcp/...", "./cmd/handmcp-pp-mcp")
	vet.Dir = cliDir
	output, err := vet.CombinedOutput()
	require.NoError(t, err, "post-sync MCP surface must go vet cleanly: %s", output)
}

func TestSyncDropsRemovedGeneratedIntentHandler(t *testing.T) {
	t.Parallel()

	apiSpec := handAuthoredMCPSpec()
	apiSpec.MCP.Intents = append(apiSpec.MCP.Intents, spec.Intent{
		Name:        "describe_operation",
		Description: "Describe a single operation.",
		Params: []spec.IntentParam{{
			Name:        "id",
			Type:        "string",
			Required:    true,
			Description: "Operation id",
		}},
		Steps: []spec.IntentStep{
			{Endpoint: "operations.status", Bind: map[string]string{"id": "${input.id}"}, Capture: "status"},
		},
		Returns: "status",
	})
	cliDir := filepath.Join(t.TempDir(), "handmcp")
	gen := generator.New(apiSpec, cliDir)
	gen.VisionSet = generator.VisionTemplateSet{MCP: true, Store: true}
	require.NoError(t, gen.Generate())
	require.NoError(t, pipeline.WriteManifestForGenerate(pipeline.GenerateManifestParams{
		APIName:   apiSpec.Name,
		OutputDir: cliDir,
		Spec:      apiSpec,
	}))
	require.NoError(t, plantHandAuthoredMCPBehavior(cliDir))

	apiSpec.MCP.Intents = apiSpec.MCP.Intents[:1]
	specData, err := yaml.Marshal(apiSpec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "spec.yaml"), specData, 0o644))

	_, err = Sync(cliDir, Options{})
	require.NoError(t, err)

	intentsAfter, err := os.ReadFile(filepath.Join(cliDir, "internal", "mcp", "intents.go"))
	require.NoError(t, err)
	src := string(intentsAfter)
	assert.NotContains(t, src, "func handleDescribeOperation(", "removed spec intents must not leave a generated handler")
	assert.Contains(t, src, "func waitForIntentPoll(")
	assert.Contains(t, src, `waitForIntentPoll(ctx, c, "operations.status"`)
}

func handAuthoredMCPSpec() *spec.APISpec {
	return &spec.APISpec{
		Name:    "handmcp",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Learn:   spec.LearnConfig{Enabled: true, EnabledSet: true},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/handmcp-pp-cli/config.toml",
		},
		MCP: spec.MCPConfig{
			Transport: []string{"stdio", "http"},
			Intents: []spec.Intent{{
				Name:        "wait_for_operation",
				Description: "Start an operation and wait until it finishes.",
				Params: []spec.IntentParam{{
					Name:        "name",
					Type:        "string",
					Required:    true,
					Description: "Operation name",
				}},
				Steps: []spec.IntentStep{
					{Endpoint: "operations.start", Bind: map[string]string{"name": "${input.name}"}, Capture: "start"},
					{Endpoint: "operations.status", Bind: map[string]string{"id": "${start.id}"}, Capture: "status"},
				},
				Returns: "status",
			}},
		},
		Resources: map[string]spec.Resource{
			"operations": {
				Description: "Long-running operations",
				Endpoints: map[string]spec.Endpoint{
					"list":   {Method: "GET", Path: "/operations", Description: "List operations"},
					"start":  {Method: "POST", Path: "/operations", Description: "Start an operation"},
					"status": {Method: "GET", Path: "/operations/{id}", Description: "Get operation status"},
				},
			},
		},
	}
}

func plantHandAuthoredMCPBehavior(cliDir string) error {
	walkerPath := filepath.Join(cliDir, "internal", "mcp", "cobratree", "walker.go")
	walker, err := os.ReadFile(walkerPath)
	if err != nil {
		return err
	}
	walkerSrc := strings.Replace(string(walker),
		"\t\treadOnly := isMCPReadOnly(cmd)\n\t\tif readOnly {\n\t\t\toptions = append(options, mcplib.WithReadOnlyHintAnnotation(true), mcplib.WithDestructiveHintAnnotation(false))\n\t\t}\n\t\tif !readOnly && isMCPLocalWrite(cmd) {",
		"\t\treadOnly := isMCPReadOnly(cmd)\n\t\tlocalWrite := isMCPLocalWrite(cmd)\n\t\tif localWrite {",
		1)
	if walkerSrc == string(walker) {
		return errPlant("walker.go local-write rule")
	}
	if err := os.WriteFile(walkerPath, []byte(walkerSrc), 0o644); err != nil {
		return err
	}

	shelloutPath := filepath.Join(cliDir, "internal", "mcp", "cobratree", "shellout.go")
	shellout, err := os.ReadFile(shelloutPath)
	if err != nil {
		return err
	}
	shelloutSrc := strings.Replace(string(shellout),
		"\t\"deliver\":      true,\n",
		"\t\"db\":           true,\n\t\"deliver\":      true,\n",
		1)
	if shelloutSrc == string(shellout) {
		return errPlant("shellout.go db blocklist")
	}
	if err := os.WriteFile(shelloutPath, []byte(shelloutSrc), 0o644); err != nil {
		return err
	}

	classifyPath := filepath.Join(cliDir, "internal", "mcp", "cobratree", "classify.go")
	classify, err := os.ReadFile(classifyPath)
	if err != nil {
		return err
	}
	classifySrc := strings.TrimRight(string(classify), "\n") + `

func isMCPExecutionReadOnly(cmd *cobra.Command) bool {
	return isMCPReadOnly(cmd) && !isMCPLocalWrite(cmd)
}
`
	if err := os.WriteFile(classifyPath, []byte(classifySrc), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(cliDir, "internal", "mcp", "cobratree", "classify_test.go"), []byte(`package cobratree

import "testing"

func TestIsMCPExecutionReadOnly(t *testing.T) {
	if isMCPExecutionReadOnly(nil) {
		t.Fatal("nil command should not be execution-read-only")
	}
}
`), 0o644); err != nil {
		return err
	}

	mainPath := filepath.Join(cliDir, "cmd", "handmcp-pp-mcp", "main.go")
	mainData, err := os.ReadFile(mainPath)
	if err != nil {
		return err
	}
	mainSrc := strings.Replace(string(mainData),
		"\t\thttpSrv := &http.Server{\n\t\t\tAddr:    bindAddr,\n\t\t\tHandler: requireBearerAuth(token, inner),\n\t\t}",
		"\t\thttpSrv := &http.Server{\n\t\t\tAddr:              bindAddr,\n\t\t\tHandler:           requireBearerAuth(token, inner),\n\t\t\tReadHeaderTimeout: 10 * time.Second,\n\t\t\tReadTimeout:       30 * time.Second,\n\t\t\tWriteTimeout:      30 * time.Second,\n\t\t\tIdleTimeout:       120 * time.Second,\n\t\t}",
		1)
	if mainSrc == string(mainData) {
		return errPlant("main.go HTTP timeouts")
	}
	if !strings.Contains(mainSrc, `"time"`) {
		mainSrc = strings.Replace(mainSrc, "\t\"strings\"\n", "\t\"strings\"\n\t\"time\"\n", 1)
	}
	if err := os.WriteFile(mainPath, []byte(mainSrc), 0o644); err != nil {
		return err
	}

	intentsPath := filepath.Join(cliDir, "internal", "mcp", "intents.go")
	intents, err := os.ReadFile(intentsPath)
	if err != nil {
		return err
	}
	intentsSrc := string(intents)
	if !strings.Contains(intentsSrc, `"time"`) {
		intentsSrc = strings.Replace(intentsSrc, "\t\"strings\"\n", "\t\"strings\"\n\t\"time\"\n", 1)
	}
	oldCall := `		resp, err := callIntentEndpoint(ctx, c, "operations.status", params)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("step %d (operations.status) failed: %v", 2, err)), nil
		}`
	newCall := `		resp, err := waitForIntentPoll(ctx, c, "operations.status", params, pollAfter, operationTimeout)
		if err != nil {
			return mcplib.NewToolResultError(fmt.Sprintf("step %d (operations.status) failed: %v", 2, err)), nil
		}`
	if !strings.Contains(intentsSrc, oldCall) {
		return errPlant("intents.go status call")
	}
	intentsSrc = strings.Replace(intentsSrc, oldCall, newCall, 1)
	intentsSrc = strings.TrimRight(intentsSrc, "\n") + `

const (
	pollAfter         = 200 * time.Millisecond
	operationTimeout  = 5 * time.Second
)

func intentDuration(d *time.Duration) time.Duration {
	if d == nil {
		return 0
	}
	return *d
}

func waitForIntentPoll(ctx context.Context, c *client.Client, ref string, params map[string]any, pollAfter, operationTimeout time.Duration) (any, error) {
	deadline := time.Now().Add(intentDuration(&operationTimeout))
	for {
		resp, err := callIntentEndpoint(ctx, c, ref, params)
		if err != nil {
			return nil, err
		}
		if intentPollComplete(resp) {
			return resp, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("operation %s still unfinished after %s", ref, operationTimeout)
		}
		timer := time.NewTimer(pollAfter)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func intentPollComplete(resp any) bool {
	m, ok := resp.(map[string]any)
	if !ok {
		return true
	}
	status, _ := m["status"].(string)
	return status == "done" || status == "complete" || status == "succeeded"
}
`
	if err := os.WriteFile(intentsPath, []byte(intentsSrc), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cliDir, "internal", "mcp", "intents_poll_test.go"), []byte(`package mcp

import (
	"testing"
	"time"
)

func TestIntentDuration(t *testing.T) {
	if intentDuration(nil) != 0 {
		t.Fatal("nil duration should be 0")
	}
	d := 2 * time.Second
	if intentDuration(&d) != d {
		t.Fatalf("intentDuration() = %s, want %s", intentDuration(&d), d)
	}
}
`), 0o644)
}

type plantError string

func (e plantError) Error() string { return "plant hand-authored MCP behavior: " + string(e) }

func errPlant(what string) error { return plantError(what) }
