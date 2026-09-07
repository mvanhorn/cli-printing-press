package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustWrite is the table-test friendly helper for this file. Keeps each
// fixture construction compact since the audit reads multiple file paths
// per CLI.
func mustWrite(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
}

// makeMCPTools returns a tools.go body with n endpoint-tool registrations.
func makeMCPTools(n int) string {
	var b strings.Builder
	b.WriteString("package mcp\nfunc RegisterTools() {\n")
	for range n {
		b.WriteString("mcplib.NewTool(\"x\",)\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func TestRunMCPAuditNoMCPSurface(t *testing.T) {
	lib := t.TempDir()
	// CLI without cmd/*-pp-mcp — audit should report no MCP.
	require.NoError(t, os.MkdirAll(filepath.Join(lib, "bare-cli"), 0o755))

	findings, err := runMCPAudit(lib)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	assert.Equal(t, "bare-cli", findings[0].API)
	assert.False(t, findings[0].HasMCP)
	assert.Equal(t, "n/a", findings[0].Transport)
	assert.Equal(t, "n/a", findings[0].ToolDesign)
	assert.Equal(t, intentHintsNA, findings[0].IntentHints)
	assert.Contains(t, findings[0].Recommend, "mcp: block")
}

func TestRunMCPAuditStdioEndpointMirror(t *testing.T) {
	lib := t.TempDir()
	cli := filepath.Join(lib, "small-cli")
	mustWrite(t, cli, "cmd/small-cli-pp-mcp/main.go", "package main\nfunc main() { server.ServeStdio(s) }\n")
	mustWrite(t, cli, "internal/mcp/tools.go", makeMCPTools(6))

	findings, err := runMCPAudit(lib)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	f := findings[0]
	assert.True(t, f.HasMCP)
	assert.Equal(t, "stdio", f.Transport)
	assert.Equal(t, "endpoint-mirror", f.ToolDesign)
	assert.Equal(t, 6, f.EndpointCt)
	assert.Equal(t, intentHintsNA, f.IntentHints)
	assert.Contains(t, f.Recommend, "[stdio, http]", "small stdio mirror still recommends remote")
	assert.NotContains(t, f.Recommend, "intents", "6 endpoints is below intent recommendation threshold")
}

func TestRunMCPAuditLargeMirrorRecommendsCodeOrch(t *testing.T) {
	lib := t.TempDir()
	cli := filepath.Join(lib, "huge-cli")
	// Both transports + endpoint mirror with many tools.
	mustWrite(t, cli, "cmd/huge-cli-pp-mcp/main.go",
		"package main\nfunc main() { server.ServeStdio(s); server.NewStreamableHTTPServer(s) }\n")
	mustWrite(t, cli, "internal/mcp/tools.go", makeMCPTools(60))

	findings, err := runMCPAudit(lib)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "both", f.Transport)
	assert.Equal(t, "endpoint-mirror", f.ToolDesign)
	assert.Equal(t, 60, f.EndpointCt)
	assert.Contains(t, f.Recommend, "orchestration: code")
	assert.NotContains(t, f.Recommend, "[stdio, http]", "transport is already both — no remote recommendation")
}

func TestRunMCPAuditCodeOrchSurface(t *testing.T) {
	lib := t.TempDir()
	cli := filepath.Join(lib, "cloudy-cli")
	mustWrite(t, cli, "cmd/cloudy-cli-pp-mcp/main.go",
		"package main\nfunc main() { server.ServeStdio(s); server.NewStreamableHTTPServer(s) }\n")
	mustWrite(t, cli, "internal/mcp/tools.go", "package mcp\n")
	mustWrite(t, cli, "internal/mcp/code_orch.go", "package mcp\n")

	findings, err := runMCPAudit(lib)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "both", f.Transport)
	assert.Equal(t, "code-orch", f.ToolDesign)
	assert.Equal(t, "ok", f.Recommend, "fully-modernized CLI has nothing to recommend")
}

func TestRunMCPAuditGeneratedLargeDefaultSurface(t *testing.T) {
	lib := t.TempDir()
	cli := filepath.Join(lib, "auto-cli")
	apiSpec := &spec.APISpec{
		Name:      "auto",
		BaseURL:   "https://api.example.com",
		Auth:      spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{},
	}
	r := spec.Resource{Endpoints: map[string]spec.Endpoint{}}
	for i := range spec.DefaultOrchestrationThreshold + 1 {
		name := fmt.Sprintf("get_%d", i)
		r.Endpoints[name] = spec.Endpoint{Method: "GET", Path: fmt.Sprintf("/items/%d", i)}
	}
	apiSpec.Resources["items"] = r

	require.NoError(t, generator.New(apiSpec, cli).Generate())

	findings, err := runMCPAudit(lib)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "auto-cli", f.API)
	assert.Equal(t, "both", f.Transport)
	assert.Equal(t, "code-orch", f.ToolDesign)
	assert.Equal(t, "ok", f.Recommend)
}

func TestRunMCPAuditJSONRoundtrip(t *testing.T) {
	lib := t.TempDir()
	mustWrite(t, lib, "alpha/cmd/alpha-pp-mcp/main.go", "package main\nfunc main() { server.ServeStdio(s) }\n")
	mustWrite(t, lib, "alpha/internal/mcp/tools.go", makeMCPTools(3))
	mustWrite(t, lib, "beta/cmd/beta-pp-mcp/main.go", "package main\nfunc main() { server.NewStreamableHTTPServer(s); server.ServeStdio(s) }\n")
	mustWrite(t, lib, "beta/internal/mcp/tools.go", makeMCPTools(12))
	mustWrite(t, lib, "beta/internal/mcp/intents.go", makeMCPTools(4))

	cmd := newMCPAuditCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--library", lib, "--json"})
	require.NoError(t, cmd.Execute())

	var findings []MCPAuditFinding
	require.NoError(t, json.Unmarshal(out.Bytes(), &findings))
	require.Len(t, findings, 2)
	assert.Equal(t, "alpha", findings[0].API)
	assert.Equal(t, "stdio", findings[0].Transport)
	assert.Equal(t, intentHintsNA, findings[0].IntentHints)
	assert.Equal(t, "beta", findings[1].API)
	assert.Equal(t, "both", findings[1].Transport)
	assert.Equal(t, "intent", findings[1].ToolDesign)
	assert.Equal(t, 4, findings[1].IntentCt)
	assert.Equal(t, intentHintsStale, findings[1].IntentHints)
	assert.Contains(t, findings[1].Recommend, "safety annotations")
}

func makeIntentTools(n int, annotated bool, description string) string {
	var b strings.Builder
	b.WriteString("package mcp\nfunc RegisterIntents() {\n")
	for range n {
		b.WriteString("\tmcplib.NewTool(\"x\",\n")
		b.WriteString("\t\tmcplib.WithDescription(\"" + description + "\"),\n")
		if annotated {
			b.WriteString("\t\tmcplib.WithOpenWorldHintAnnotation(true),\n")
		}
		b.WriteString("\t)\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func TestRunMCPAuditIntentSurfaceStaleWithoutAnnotations(t *testing.T) {
	lib := t.TempDir()
	cli := filepath.Join(lib, "stale-cli")
	mustWrite(t, cli, "cmd/stale-cli-pp-mcp/main.go",
		"package main\nfunc main() { server.ServeStdio(s); server.NewStreamableHTTPServer(s) }\n")
	mustWrite(t, cli, "internal/mcp/tools.go", makeMCPTools(2))
	mustWrite(t, cli, "internal/mcp/intents.go", makeIntentTools(2, false, "Find the best award, then fetch bookable trip detail"))

	findings, err := runMCPAudit(lib)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "intent", f.ToolDesign)
	assert.Equal(t, intentHintsStale, f.IntentHints)
	assert.Contains(t, f.Recommend, "safety annotations")
	assert.NotContains(t, f.Recommend, "overclaim")
}

func TestRunMCPAuditIntentSurfaceAllowsExecutedStepThenClause(t *testing.T) {
	lib := t.TempDir()
	cli := filepath.Join(lib, "step-then-cli")
	mustWrite(t, cli, "cmd/step-then-cli-pp-mcp/main.go",
		"package main\nfunc main() { server.ServeStdio(s); server.NewStreamableHTTPServer(s) }\n")
	mustWrite(t, cli, "internal/mcp/tools.go", makeMCPTools(1))
	mustWrite(t, cli, "internal/mcp/intents.go", makeIntentTools(1, true, "Fetch the record, then normalize its fields"))

	findings, err := runMCPAudit(lib)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, intentHintsOK, f.IntentHints)
	assert.Equal(t, "ok", f.Recommend)
	assert.NotContains(t, f.Recommend, "overclaim")
}

func TestRunMCPAuditIntentSurfaceCurrent(t *testing.T) {
	lib := t.TempDir()
	cli := filepath.Join(lib, "current-cli")
	mustWrite(t, cli, "cmd/current-cli-pp-mcp/main.go",
		"package main\nfunc main() { server.ServeStdio(s); server.NewStreamableHTTPServer(s) }\n")
	mustWrite(t, cli, "internal/mcp/tools.go", makeMCPTools(2))
	mustWrite(t, cli, "internal/mcp/intents.go", makeIntentTools(2, true, "Search award availability"))

	findings, err := runMCPAudit(lib)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "intent", f.ToolDesign)
	assert.Equal(t, intentHintsOK, f.IntentHints)
	assert.Equal(t, "ok", f.Recommend)
}

func TestRunMCPAuditIntentSurfaceRejectsInvalidTypedDefault(t *testing.T) {
	lib := t.TempDir()
	cli := filepath.Join(lib, "bad-default-cli")
	mustWrite(t, cli, "cmd/bad-default-cli-pp-mcp/main.go",
		"package main\nfunc main() { server.ServeStdio(s); server.NewStreamableHTTPServer(s) }\n")
	mustWrite(t, cli, "internal/mcp/tools.go", makeMCPTools(1))
	mustWrite(t, cli, "internal/mcp/intents.go", `package mcp
func RegisterIntents() {
	mcplib.NewTool("search_records",
		mcplib.WithDescription("Search records"),
		mcplib.WithOpenWorldHintAnnotation(true),
	)
	input["limit"] = 12x
}
`)

	findings, err := runMCPAudit(lib)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, intentHintsStale, f.IntentHints)
	assert.Contains(t, f.Recommend, "typed defaults")
}

func TestRunMCPAuditGeneratedIntentSurfaceIsCurrent(t *testing.T) {
	lib := t.TempDir()
	cli := filepath.Join(lib, "honest-cli")
	apiSpec := &spec.APISpec{
		Name:    "honest",
		BaseURL: "https://api.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"availability": {
				Endpoints: map[string]spec.Endpoint{
					"search": {Method: "GET", Path: "/search", Description: "Fetch the record, then normalize its fields"},
					"book":   {Method: "POST", Path: "/book", Description: "Hold a trip"},
				},
			},
		},
		MCP: spec.MCPConfig{
			Intents: []spec.Intent{
				{
					Name:        "find_best_award",
					Description: "Find the best award, then fetch bookable trip detail for the top result. Defaults to business",
					Params: []spec.IntentParam{
						{Name: "origin", Type: "string", Required: true, Description: "Origin airport"},
						{Name: "cabin", Type: "string", Description: "Cabin class"},
					},
					Steps: []spec.IntentStep{
						{
							Endpoint: "availability.search",
							Bind:     map[string]string{"origin": "${input.origin}", "cabin": "${input.cabin}"},
							Capture:  "results",
						},
					},
					Returns: "results",
				},
				{
					Name:        "hold_trip",
					Description: "Hold a trip",
					Params: []spec.IntentParam{
						{Name: "id", Type: "string", Required: true, Description: "Trip id"},
					},
					Steps: []spec.IntentStep{
						{
							Endpoint: "availability.book",
							Bind:     map[string]string{"id": "${input.id}"},
							Capture:  "hold",
						},
					},
					Returns: "hold",
				},
			},
		},
	}

	require.NoError(t, generator.New(apiSpec, cli).Generate())

	intentsSrc, err := os.ReadFile(filepath.Join(cli, "internal", "mcp", "intents.go"))
	require.NoError(t, err)
	assert.Contains(t, string(intentsSrc), "Fetch the record, then normalize its fields",
		"executed endpoint step text must remain in the generated intent surface")

	findings, err := runMCPAudit(lib)
	require.NoError(t, err)
	require.Len(t, findings, 1)
	f := findings[0]
	assert.Equal(t, "intent", f.ToolDesign)
	assert.Equal(t, 2, f.IntentCt)
	assert.Equal(t, intentHintsOK, f.IntentHints)
	assert.NotContains(t, f.Recommend, "reprint:")
	assert.NotContains(t, f.Recommend, "overclaim")
}

func TestRunMCPAuditMissingLibraryErrors(t *testing.T) {
	_, err := runMCPAudit(filepath.Join(t.TempDir(), "does-not-exist"))
	assert.Error(t, err, "missing library path should surface as a clear error")
}
