package mcpsync

import (
	"encoding/json"
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

func TestSyncPreservesCustomReadHeaderTimeout(t *testing.T) {
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

	mainPath := filepath.Join(cliDir, "cmd", "handmcp-pp-mcp", "main.go")
	mainData, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	const generated = "ReadHeaderTimeout: 10 * time.Second"
	const custom = "ReadHeaderTimeout: 45 * time.Second"
	require.Contains(t, string(mainData), generated)
	planted := strings.Replace(string(mainData), generated, custom, 1)
	require.NotEqual(t, string(mainData), planted)
	require.NoError(t, os.WriteFile(mainPath, []byte(planted), 0o644))

	_, err = Sync(cliDir, Options{})
	require.NoError(t, err)

	mainAfter, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	assert.Contains(t, string(mainAfter), custom, "a non-generated ReadHeaderTimeout must survive reprint")
	assert.NotContains(t, string(mainAfter), generated, "reprint must not restore the generated 10s header timeout")
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
		"\t\tif readOnly {\n\t\t\toptions = append(options, mcplib.WithReadOnlyHintAnnotation(true), mcplib.WithDestructiveHintAnnotation(false))\n\t\t}\n\t\tif !readOnly && isMCPLocalWrite(cmd) {",
		"\t\tlocalWrite := isMCPLocalWrite(cmd)\n\t\tif localWrite {",
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
	deliverAt := strings.Index(string(shellout), "\t\"deliver\":")
	if deliverAt < 0 || !strings.Contains(string(shellout)[deliverAt:], "\n") {
		return errPlant("shellout.go db blocklist")
	}
	shelloutSrc := string(shellout)[:deliverAt] + "\t\"db\": true,\n" + string(shellout)[deliverAt:]
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
		"\t\thttpSrv := newMCPHTTPServer(bindAddr, requireBearerAuthWithOrigins(token, allowedOrigins, inner))",
		"\t\thttpSrv := &http.Server{\n\t\t\tAddr:              bindAddr,\n\t\t\tHandler:           requireBearerAuthWithOrigins(token, allowedOrigins, inner),\n\t\t\tReadHeaderTimeout: 10 * time.Second,\n\t\t\tReadTimeout:       30 * time.Second,\n\t\t\tWriteTimeout:      30 * time.Second,\n\t\t\tIdleTimeout:       120 * time.Second,\n\t\t}",
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

func TestSyncPreservesNovelFeatureMetadata(t *testing.T) {
	t.Parallel()

	apiSpec := &spec.APISpec{
		Name:    "novelfeatures",
		Version: "0.1.0",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Learn:   spec.LearnConfig{Enabled: true, EnabledSet: true},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/novelfeatures-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}
	cliDir := filepath.Join(t.TempDir(), "novelfeatures")
	gen := generator.New(apiSpec, cliDir)
	gen.VisionSet = generator.VisionTemplateSet{MCP: true, Store: true}
	gen.NovelFeatures = []generator.NovelFeature{
		{Name: "Health dashboard", Command: "health", Description: "Summarize health.", Rationale: "Requires local joins."},
		{Name: "Stale triage", Command: "stale", Description: "Find stale items.", Rationale: "Requires workflow analysis."},
	}
	require.NoError(t, gen.Generate())
	require.NoError(t, pipeline.WriteManifestForGenerate(pipeline.GenerateManifestParams{
		APIName:   apiSpec.Name,
		OutputDir: cliDir,
		Spec:      apiSpec,
		NovelFeatures: []pipeline.NovelFeatureManifest{
			{Name: "Health dashboard", Command: "health", Description: "Summarize health."},
			{Name: "Stale triage", Command: "stale", Description: "Find stale items."},
		},
	}))

	manifest, err := pipeline.ReadCLIManifest(cliDir)
	require.NoError(t, err)
	manifest.NovelFeaturesBuilt = append([]pipeline.NovelFeatureManifest(nil), manifest.NovelFeatures...)
	require.NoError(t, pipeline.WriteCLIManifest(cliDir, manifest))

	archived := *apiSpec
	archived.Learn = spec.LearnConfig{}
	specData, err := yaml.Marshal(&archived)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "spec.yaml"), specData, 0o644))

	toolsBefore, err := os.ReadFile(filepath.Join(cliDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	require.Contains(t, string(toolsBefore), `"rationale": "Requires local joins."`)
	require.Contains(t, string(toolsBefore), `"learn_protocol": learn.RecallFirstProtocol`)

	_, err = Sync(cliDir, Options{})
	require.NoError(t, err)

	after, err := pipeline.ReadCLIManifest(cliDir)
	require.NoError(t, err)
	require.Len(t, after.NovelFeaturesBuilt, 2, "mcp-sync must not zero novel_features_built")
	assert.Equal(t, "health", after.NovelFeaturesBuilt[0].Command)
	assert.Equal(t, "stale", after.NovelFeaturesBuilt[1].Command)
	require.Len(t, after.NovelFeatures, 2, "mcp-sync must keep recorded novel_features")

	raw, err := os.ReadFile(filepath.Join(cliDir, pipeline.CLIManifestFilename))
	require.NoError(t, err)
	var rawManifest map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw, &rawManifest))
	assert.JSONEq(t, `[{"name":"Health dashboard","command":"health","description":"Summarize health."},{"name":"Stale triage","command":"stale","description":"Find stale items."}]`, string(rawManifest["novel_features_built"]))

	toolsAfter, err := os.ReadFile(filepath.Join(cliDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	toolsSrc := string(toolsAfter)
	assert.Contains(t, toolsSrc, `"rationale": "Requires local joins."`)
	assert.Contains(t, toolsSrc, `"rationale": "Requires workflow analysis."`)
	assert.Contains(t, toolsSrc, `"learn_protocol": learn.RecallFirstProtocol`)
	assert.Contains(t, toolsSrc, `internal/learn"`)
}

func TestMergeNovelFeatureRationalesFromTools(t *testing.T) {
	t.Parallel()

	cliDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "mcp"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "internal", "mcp", "tools.go"), []byte(`
		"command_mirror_capabilities": []map[string]string{
			{"name": "Health dashboard", "command": "health", "cli_command": "health", "description": "Summarize health.", "rationale": "Requires local joins.", "via": "mcp-command-mirror"},
			{"name": "Legacy dashboard", "command": "legacy", "description": "Legacy health.", "rationale": "Legacy rationale.", "via": "mcp-command-mirror"},
			{"name": "Stale triage", "command": "stale", "description": "Find stale items.", "rationale": "Requires workflow analysis.", "via": "mcp-command-mirror"},
		},
`), 0o644))

	got := mergeNovelFeatureRationalesFromTools(cliDir, []generator.NovelFeature{
		{Name: "Health dashboard", Command: "health", Description: "Summarize health."},
		{Name: "Stale triage", Command: "stale", Description: "Find stale items.", Rationale: "keep existing"},
		{Name: "Unrecorded", Command: "ghost", Description: "Not in tools.go."},
		{Name: "Legacy dashboard", Command: "legacy", Description: "Legacy health."},
	})
	require.Len(t, got, 4)
	assert.Equal(t, "Requires local joins.", got[0].Rationale)
	assert.Equal(t, "keep existing", got[1].Rationale)
	assert.Empty(t, got[2].Rationale)
	assert.Equal(t, "Legacy rationale.", got[3].Rationale)
}

func TestPreserveExistingLearnLoop(t *testing.T) {
	t.Parallel()

	cliDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "learn"), 0o755))

	parsed := &spec.APISpec{}
	preserveExistingLearnLoop(cliDir, parsed)
	assert.True(t, parsed.Learn.Enabled)

	explicitOff := &spec.APISpec{Learn: spec.LearnConfig{EnabledSet: true}}
	preserveExistingLearnLoop(cliDir, explicitOff)
	assert.False(t, explicitOff.Learn.Enabled)

	disabled := &spec.APISpec{Learn: spec.LearnConfig{Disabled: true}}
	preserveExistingLearnLoop(cliDir, disabled)
	assert.False(t, disabled.Learn.Enabled)

	missing := &spec.APISpec{}
	preserveExistingLearnLoop(t.TempDir(), missing)
	assert.False(t, missing.Learn.Enabled)
}

func TestLoadNovelFeaturesPrefersBuiltList(t *testing.T) {
	t.Parallel()

	cliDir := t.TempDir()
	require.NoError(t, pipeline.WriteCLIManifest(cliDir, pipeline.CLIManifest{
		APIName: "example",
		CLIName: "example-pp-cli",
		NovelFeatures: []pipeline.NovelFeatureManifest{
			{Name: "Planned", Command: "planned", Description: "Not verified."},
		},
		NovelFeaturesBuilt: []pipeline.NovelFeatureManifest{
			{Name: "Health dashboard", Command: "health", Description: "Summarize health."},
		},
	}))

	got := loadNovelFeatures(cliDir)
	require.Len(t, got, 1)
	assert.Equal(t, "health", got[0].Command)
}

func TestSyncPreservesAssignedHTTPTimeout(t *testing.T) {
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

	mainPath := filepath.Join(cliDir, "cmd", "handmcp-pp-mcp", "main.go")
	mainData, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	const anchor = "httpSrv := newMCPHTTPServer(bindAddr, requireBearerAuthWithOrigins(token, allowedOrigins, inner))"
	const assignment = anchor + "\n\thttpSrv.ReadTimeout = 30 * time.Second"
	planted := strings.Replace(string(mainData), anchor, assignment, 1)
	require.NotEqual(t, string(mainData), planted)
	require.NoError(t, os.WriteFile(mainPath, []byte(planted), 0o644))

	_, err = Sync(cliDir, Options{})
	require.NoError(t, err)

	mainAfter, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	assert.Contains(t, string(mainAfter), "httpSrv.ReadTimeout = 30 * time.Second")
}

func TestSyncUpgradesOldStoreSearchForMCP(t *testing.T) {
	t.Parallel()

	apiSpec := handAuthoredMCPSpec()
	list := apiSpec.Resources["operations"].Endpoints["list"]
	list.Pagination = &spec.Pagination{Type: "cursor", CursorParam: "cursor", LimitParam: "limit"}
	ops := apiSpec.Resources["operations"]
	ops.Endpoints["list"] = list
	apiSpec.Resources["operations"] = ops

	cliDir := filepath.Join(t.TempDir(), "handmcp")
	gen := generator.New(apiSpec, cliDir)
	gen.VisionSet = generator.VisionTemplateSet{MCP: true, Store: true, Search: true}
	require.NoError(t, gen.Generate())
	require.NoError(t, pipeline.WriteManifestForGenerate(pipeline.GenerateManifestParams{
		APIName:   apiSpec.Name,
		OutputDir: cliDir,
		Spec:      apiSpec,
	}))
	specData, err := yaml.Marshal(apiSpec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "spec.yaml"), specData, 0o644))

	tools, err := os.ReadFile(filepath.Join(cliDir, "internal", "mcp", "tools.go"))
	require.NoError(t, err)
	require.Contains(t, string(tools), "db.SearchContext(")

	storePath := filepath.Join(cliDir, "internal", "store", "store.go")
	store, err := os.ReadFile(storePath)
	require.NoError(t, err)
	legacy := strings.Replace(string(store),
		"func (s *Store) Search(query string, limit int, resourceTypes ...string) ([]json.RawMessage, error) {\n\treturn s.SearchContext(context.Background(), query, limit, resourceTypes...)\n}\n\nfunc (s *Store) SearchContext(ctx context.Context, query string, limit int, resourceTypes ...string) ([]json.RawMessage, error) {\n\tif ctx == nil {\n\t\tctx = context.Background()\n\t}\n",
		"func (s *Store) Search(query string, limit int, resourceTypes ...string) ([]json.RawMessage, error) {\n",
		1)
	legacy = strings.Replace(legacy, "s.db.QueryContext(ctx, q, args...)", "s.db.Query(q, args...)", 1)
	require.NotContains(t, legacy, "SearchContext")
	require.Contains(t, legacy, "s.db.Query(q, args...)")
	require.NoError(t, os.WriteFile(storePath, []byte(legacy), 0o644))

	_, err = Sync(cliDir, Options{})
	require.NoError(t, err)

	upgraded, err := os.ReadFile(storePath)
	require.NoError(t, err)
	assert.Contains(t, string(upgraded), "func (s *Store) SearchContext(")
	assert.Contains(t, string(upgraded), "s.db.QueryContext(ctx, q, args...)")
	assert.Contains(t, string(upgraded), "return s.SearchContext(context.Background(), query, limit, resourceTypes...)")

	build := exec.Command("go", "test", "-mod=mod", "-c", "-o", os.DevNull, "./internal/mcp")
	build.Dir = cliDir
	output, err := build.CombinedOutput()
	require.NoError(t, err, "synced MCP package must compile against the upgraded store: %s", output)
}

func TestMCPEntrypointSupportsHTTPAuthTests(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "pre-origin four-parameter limits",
			src: `package main
func requireBearerAuthWithLimits(expected string, next http.Handler, maxBytes int64, readTimeout time.Duration) {}
`,
			want: false,
		},
		{
			name: "origin helpers",
			src: `package main
func requireBearerAuthWithOrigins(expected string, allowed map[string]struct{}, next http.Handler) {}
func requireBearerAuthWithLimits(expected string, allowed map[string]struct{}, next http.Handler, maxBytes int64, readTimeout time.Duration) {}
`,
			want: true,
		},
		{
			name: "unparsed",
			src:  "package main\nfunc",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, mcpEntrypointSupportsHTTPAuthTests([]byte(tc.src)))
		})
	}
}

func TestPreserveHTTPAuthTestsRestoresPreOriginFile(t *testing.T) {
	t.Parallel()

	cliDir := t.TempDir()
	mainPath := filepath.Join(cliDir, "cmd", "handmcp-pp-mcp", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(mainPath), 0o755))
	oldMain := []byte(preOriginMainFixture)
	oldTest := []byte("package main\n\nfunc TestPreOriginAuth(t *testing.T) {}\n")
	require.NoError(t, os.WriteFile(mainPath, oldMain, 0o644))
	testPath := filepath.Join(filepath.Dir(mainPath), "http_auth_test.go")
	require.NoError(t, os.WriteFile(testPath, oldTest, 0o644))

	snap, err := snapshotMCPRuntime(cliDir)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(mainPath, []byte("package main\n"), 0o644))
	require.NoError(t, os.WriteFile(testPath, []byte("package main\n\nfunc TestParseHTTPOriginBypasses(t *testing.T) {}\n"), 0o644))

	require.NoError(t, restoreMCPRuntime(cliDir, snap))

	gotMain, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	assert.Equal(t, string(oldMain), string(gotMain))
	gotTest, err := os.ReadFile(testPath)
	require.NoError(t, err)
	assert.Equal(t, string(oldTest), string(gotTest))
}

func TestPreserveHTTPAuthTestsKeepsRegeneratedOriginTests(t *testing.T) {
	t.Parallel()

	cliDir := t.TempDir()
	mainPath := filepath.Join(cliDir, "cmd", "handmcp-pp-mcp", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(mainPath), 0o755))
	currentMain := []byte(originMainFixture)
	require.NoError(t, os.WriteFile(mainPath, currentMain, 0o644))
	testPath := filepath.Join(filepath.Dir(mainPath), "http_auth_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte("package main\n\nfunc TestStale(t *testing.T) {}\n"), 0o644))

	snap, err := snapshotMCPRuntime(cliDir)
	require.NoError(t, err)
	regenerated := []byte("package main\n\nfunc TestParseHTTPOriginBypasses(t *testing.T) {}\n")
	require.NoError(t, os.WriteFile(mainPath, []byte("package main\n"), 0o644))
	require.NoError(t, os.WriteFile(testPath, regenerated, 0o644))

	require.NoError(t, restoreMCPRuntime(cliDir, snap))

	gotMain, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	assert.Equal(t, string(currentMain), string(gotMain))
	gotTest, err := os.ReadFile(testPath)
	require.NoError(t, err)
	assert.Equal(t, string(regenerated), string(gotTest))
}

func TestPreserveHTTPAuthTestsDropsIncompatibleRegen(t *testing.T) {
	t.Parallel()

	cliDir := t.TempDir()
	mainPath := filepath.Join(cliDir, "cmd", "handmcp-pp-mcp", "main.go")
	require.NoError(t, os.MkdirAll(filepath.Dir(mainPath), 0o755))
	oldMain := []byte(preOriginMainFixture)
	require.NoError(t, os.WriteFile(mainPath, oldMain, 0o644))

	snap, err := snapshotMCPRuntime(cliDir)
	require.NoError(t, err)
	testPath := filepath.Join(filepath.Dir(mainPath), "http_auth_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte("package main\n\nfunc TestParseHTTPOriginBypasses(t *testing.T) {}\n"), 0o644))
	require.NoError(t, os.WriteFile(mainPath, []byte("package main\n"), 0o644))

	require.NoError(t, restoreMCPRuntime(cliDir, snap))

	gotMain, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	assert.Equal(t, string(oldMain), string(gotMain))
	_, err = os.Stat(testPath)
	assert.ErrorIs(t, err, os.ErrNotExist)
}

// TestSyncPreservesPreOriginCustomizedHTTPEntrypoint starts from an HTTP
// entrypoint shaped like a print from before origin checks: the
// four-parameter requireBearerAuthWithLimits, no origin helpers, the
// pre-change http_auth_test.go, and a hand-set ReadTimeout. mcp-sync
// regenerates the tests, then restores that entrypoint. The pair must
// still compile and pass.
func TestSyncPreservesPreOriginCustomizedHTTPEntrypoint(t *testing.T) {
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

	require.NoError(t, plantPreOriginCustomizedHTTPEntrypoint(cliDir))

	mainPath := filepath.Join(cliDir, "cmd", "handmcp-pp-mcp", "main.go")
	testPath := filepath.Join(cliDir, "cmd", "handmcp-pp-mcp", "http_auth_test.go")
	mainBefore, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	testBefore, err := os.ReadFile(testPath)
	require.NoError(t, err)
	require.False(t, mcpEntrypointSupportsHTTPAuthTests(mainBefore))
	require.Contains(t, string(mainBefore), "httpSrv.ReadTimeout = 45 * time.Second")
	require.NotContains(t, string(testBefore), "requireBearerAuthWithOrigins")

	_, err = Sync(cliDir, Options{})
	require.NoError(t, err)

	mainAfter, err := os.ReadFile(mainPath)
	require.NoError(t, err)
	testAfter, err := os.ReadFile(testPath)
	require.NoError(t, err)
	assert.Equal(t, string(mainBefore), string(mainAfter), "customized pre-origin entrypoint must be restored verbatim")
	assert.Equal(t, string(testBefore), string(testAfter), "HTTP tests must match the restored entrypoint")
	assert.NotContains(t, string(testAfter), "func TestParseHTTPOriginBypasses")
	assert.NotContains(t, string(mainAfter), "requireBearerAuthWithOrigins")
	assert.NotContains(t, string(mainAfter), "mcpHTTPTokenMinLength")

	cmd := exec.Command("go", "test", "-mod=mod", "-count=1", "-timeout", "180s", "./cmd/handmcp-pp-mcp")
	cmd.Dir = cliDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "synced pre-origin HTTP entrypoint must compile and pass its preserved tests:\n%s", output)
}

func plantPreOriginCustomizedHTTPEntrypoint(cliDir string) error {
	mainPath := filepath.Join(cliDir, "cmd", "handmcp-pp-mcp", "main.go")
	mainData, err := os.ReadFile(mainPath)
	if err != nil {
		return err
	}
	src := string(mainData)
	replacements := []struct{ old, new, what string }{
		{"\t\"net/netip\"\n", "", "net/netip import"},
		{"\t\"net/url\"\n", "", "net/url import"},
		{"\tmcpHTTPTokenMinLength   = 32\n", "", "token minimum const"},
		{
			"\t\tallowedOrigins, err := loadAllowedHTTPOrigins()\n\t\tif err != nil {\n\t\t\tfmt.Fprintf(os.Stderr, \"MCP server error: %v\\n\", err)\n\t\t\tos.Exit(1)\n\t\t}\n",
			"",
			"allowed-origin load",
		},
		{
			"\t\thttpSrv := newMCPHTTPServer(bindAddr, requireBearerAuthWithOrigins(token, allowedOrigins, inner))",
			"\t\thttpSrv := newMCPHTTPServer(bindAddr, requireBearerAuth(token, inner))\n\t\thttpSrv.ReadTimeout = 45 * time.Second",
			"HTTP server call",
		},
		{
			"\tif len(token) < mcpHTTPTokenMinLength {\n\t\treturn \"\", fmt.Errorf(\"%s must be set to a bearer token of at least %d characters before starting --transport http\", httpTokenEnvVar, mcpHTTPTokenMinLength)\n\t}",
			"\tif token == \"\" {\n\t\treturn \"\", fmt.Errorf(\"%s must be set to a non-empty bearer token before starting --transport http\", httpTokenEnvVar)\n\t}",
			"token length check",
		},
		{preOriginAuthOld, preOriginAuthNew, "bearer auth helpers"},
	}
	for _, repl := range replacements {
		if !strings.Contains(src, repl.old) {
			return errPlant("main.go " + repl.what)
		}
		src = strings.Replace(src, repl.old, repl.new, 1)
	}
	src, err = dropLineContaining(src, "const httpAllowedOriginsEnvVar = ")
	if err != nil {
		return err
	}
	const originMark = "\n// parseHTTPOrigin accepts"
	at := strings.Index(src, originMark)
	if at < 0 {
		return errPlant("main.go origin helpers")
	}
	src = strings.TrimRight(src[:at], "\n") + "\n"
	if strings.Contains(src, "requireBearerAuthWithOrigins") || strings.Contains(src, "net/netip") || strings.Contains(src, "mcpHTTPTokenMinLength") {
		return errPlant("main.go leftover origin surface")
	}
	if err := os.WriteFile(mainPath, []byte(src), 0o644); err != nil {
		return err
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "preorigin_http_auth_test.go.txt"))
	if err != nil {
		return err
	}
	if strings.Contains(string(fixture), "requireBearerAuthWithOrigins") {
		return errPlant("pre-origin HTTP auth fixture")
	}
	return os.WriteFile(filepath.Join(filepath.Dir(mainPath), "http_auth_test.go"), fixture, 0o644)
}

func dropLineContaining(src, fragment string) (string, error) {
	lines := strings.Split(src, "\n")
	kept := make([]string, 0, len(lines))
	found := false
	for _, line := range lines {
		if strings.Contains(line, fragment) {
			found = true
			continue
		}
		kept = append(kept, line)
	}
	if !found {
		return "", errPlant("main.go " + fragment)
	}
	return strings.Join(kept, "\n"), nil
}

const preOriginMainFixture = `package main

import (
	"net/http"
	"time"
)

func handTunedServer() *http.Server {
	srv := &http.Server{}
	srv.ReadTimeout = 45 * time.Second
	return srv
}

func requireBearerAuthWithLimits(expected string, next http.Handler, maxBytes int64, readTimeout time.Duration) http.Handler {
	return next
}
`

const originMainFixture = `package main

import (
	"net/http"
	"time"
)

func handTunedServer() *http.Server {
	srv := &http.Server{}
	srv.ReadTimeout = 45 * time.Second
	return srv
}

func requireBearerAuthWithOrigins(expected string, allowed map[string]struct{}, next http.Handler) http.Handler {
	return next
}

func requireBearerAuthWithLimits(expected string, allowed map[string]struct{}, next http.Handler, maxBytes int64, readTimeout time.Duration) http.Handler {
	return next
}
`

const preOriginAuthOld = "func requireBearerAuth(expected string, next http.Handler) http.Handler {\n" +
	"\treturn requireBearerAuthWithOrigins(expected, nil, next)\n" +
	"}\n" +
	"\n" +
	"func requireBearerAuthWithOrigins(expected string, allowed map[string]struct{}, next http.Handler) http.Handler {\n" +
	"\treturn requireBearerAuthWithLimits(expected, allowed, next, mcpHTTPMaxBodyBytes, mcpHTTPBodyReadDeadline)\n" +
	"}\n" +
	"\n" +
	"func requireBearerAuthWithLimits(expected string, allowed map[string]struct{}, next http.Handler, maxBytes int64, readTimeout time.Duration) http.Handler {\n" +
	"\treturn http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {\n" +
	"\t\tif !requestOriginPermitted(r, allowed) {\n" +
	"\t\t\thttp.Error(w, \"forbidden origin\", http.StatusForbidden)\n" +
	"\t\t\treturn\n" +
	"\t\t}\n" +
	"\t\tif !bearerTokenMatches(r.Header.Get(\"Authorization\"), expected) {"

const preOriginAuthNew = "func requireBearerAuth(expected string, next http.Handler) http.Handler {\n" +
	"\treturn requireBearerAuthWithLimits(expected, next, mcpHTTPMaxBodyBytes, mcpHTTPBodyReadDeadline)\n" +
	"}\n" +
	"\n" +
	"func requireBearerAuthWithLimits(expected string, next http.Handler, maxBytes int64, readTimeout time.Duration) http.Handler {\n" +
	"\treturn http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {\n" +
	"\t\tif !bearerTokenMatches(r.Header.Get(\"Authorization\"), expected) {"
