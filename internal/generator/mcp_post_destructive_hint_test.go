package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGenerateMCPPostDestructiveHints(t *testing.T) {
	if testing.Short() {
		t.Skip("generated MCP annotation proof runs in the full test lane")
	}

	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "device-safety-destructive.yaml"))
	require.NoError(t, err)
	api, err := openapi.Parse(body)
	require.NoError(t, err)
	api.Learn.Enabled = true

	retire := endpointRef(t, api, "POST", "/devices/{id}/retire")
	revoke := endpointRef(t, api, "POST", "/devices/{id}/sessions/{sessionId}/revoke")
	tag := endpointRef(t, api, "POST", "/widgets/{id}/tag")
	cancel := endpointRef(t, api, "POST", "/widgets/{id}/cancel")
	label := endpointRef(t, api, "POST", "/widgets/{id}/label")
	_ = endpointRef(t, api, "POST", "/widgets/{id}/nudge")
	_ = endpointRef(t, api, "GET", "/widgets/{id}")
	_ = endpointRef(t, api, "PUT", "/widgets/{id}")
	deleteWidget := endpointRef(t, api, "DELETE", "/widgets/{id}")
	_ = endpointRef(t, api, "POST", "/widgets/search")
	queryReports := endpointRef(t, api, "POST", "/reports/query")
	require.Len(t, strings.Split(revoke, "."), 3, "revoke must be a subresource so the second mirror site is emitted; refs=%s", dumpEndpointRefs(api))

	api.MCP.Intents = []spec.Intent{
		intentNamed("retire_device", "Retire a device", retire),
		intentNamed("tag_widget", "Tag a widget", tag),
		intentNamed("cancel_widget", "Cancel a widget", cancel),
		intentNamed("label_widget", "Label a widget", label),
		intentNamed("delete_widget", "Delete a widget", deleteWidget),
		intentNamed("query_reports", "Query reports", queryReports),
		intentNamed("retire_then_tag", "Retire then tag", retire, tag),
		intentNamed("tag_then_delete", "Tag then delete", tag, deleteWidget),
		intentNamed("missing_step", "Call a missing endpoint", "widgets.missing"),
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(api.Name))
	require.NoError(t, New(api, outputDir).Generate())

	toolsSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	intentsSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "intents.go")

	endpointCases := []struct {
		method, path string
		readOnly     bool
		destructive  *bool
	}{
		{method: "POST", path: "/devices/{id}/retire", destructive: nil},
		{method: "POST", path: "/devices/{id}/sessions/{sessionId}/revoke", destructive: nil},
		{method: "POST", path: "/widgets/{id}/tag", destructive: new(false)},
		{method: "POST", path: "/widgets/{id}/cancel", destructive: new(true)},
		{method: "POST", path: "/widgets/{id}/label", destructive: nil},
		{method: "POST", path: "/widgets/{id}/nudge", destructive: nil},
		{method: "GET", path: "/widgets/{id}", readOnly: true, destructive: new(false)},
		{method: "PUT", path: "/widgets/{id}", destructive: nil},
		{method: "DELETE", path: "/widgets/{id}", destructive: new(true)},
		{method: "POST", path: "/widgets/search", readOnly: true, destructive: new(false)},
		{method: "POST", path: "/reports/query", readOnly: true, destructive: new(false)},
	}

	var runtime []hintCase
	for _, tc := range endpointCases {
		name, block := toolBlockForPath(t, toolsSrc, tc.method, tc.path)
		assertHintBlock(t, name, block, tc.readOnly, tc.destructive, true)
		runtime = append(runtime, hintCase{
			name: name, readOnly: tc.readOnly, destructive: tc.destructive, openWorld: new(true),
		})
	}
	require.NotContains(t, toolBlockForPathMust(t, toolsSrc, "POST", "/devices/{id}/retire"), "WithDestructiveHintAnnotation(false)")

	intentCases := []struct {
		name        string
		readOnly    bool
		destructive *bool
	}{
		{name: "retire_device"},
		{name: "label_widget"},
		{name: "retire_then_tag"},
		{name: "missing_step"},
		{name: "tag_widget", destructive: new(false)},
		{name: "cancel_widget", destructive: new(true)},
		{name: "delete_widget", destructive: new(true)},
		{name: "tag_then_delete", destructive: new(true)},
		{name: "query_reports", readOnly: true, destructive: new(false)},
	}
	for _, tc := range intentCases {
		block := intentToolBlock(t, intentsSrc, tc.name)
		assertHintBlock(t, tc.name, block, tc.readOnly, tc.destructive, true)
		runtime = append(runtime, hintCase{
			name: tc.name, readOnly: tc.readOnly, destructive: tc.destructive, openWorld: new(true),
		})
	}

	contextBlock := builtInToolBlock(t, toolsSrc, "context")
	assertHintBlock(t, "context", contextBlock, true, new(false), false)
	runtime = append(runtime, hintCase{name: "context", readOnly: true, destructive: new(false)})
	if strings.Contains(toolsSrc, `NewTool("sql"`) {
		assertHintBlock(t, "sql", builtInToolBlock(t, toolsSrc, "sql"), true, new(false), false)
		runtime = append(runtime, hintCase{name: "sql", readOnly: true, destructive: new(false)})
	}
	if strings.Contains(toolsSrc, `NewTool("search"`) {
		assertHintBlock(t, "search", builtInToolBlock(t, toolsSrc, "search"), true, new(false), false)
		runtime = append(runtime, hintCase{name: "search", readOnly: true, destructive: new(false)})
	}

	teachSrc := readGeneratedFile(t, outputDir, "internal", "cli", "teach.go")
	require.Contains(t, teachSrc, "IngressSurface: store.LearnEventSurface()")
	require.Contains(t, teachSrc, `json:"untrusted_data,omitempty"`)
	require.Contains(t, readGeneratedFile(t, outputDir, "internal", "cli", "teach_playbook.go"), "store.LearnEventSurface()")
	require.Contains(t, readGeneratedFile(t, outputDir, "internal", "cli", "learnings_candidates.go"), "store.LearnEventSurface()")
	require.NotContains(t, readGeneratedFile(t, outputDir, "internal", "cli", "playbook_init.go"), "LearnEventSurface")
	require.Contains(t, readGeneratedFile(t, outputDir, "cmd", naming.MCP(api.Name), "main.go"), "_LEARN_SURFACE")
	recallSrc := readGeneratedFile(t, outputDir, "internal", "learn", "recall.go")
	require.Contains(t, recallSrc, "untrusted_data,omitempty")
	require.Contains(t, recallSrc, "untrustedPlaybookTrial")
	require.Contains(t, readGeneratedFile(t, outputDir, "internal", "store", "store.go"), "const StoreSchemaVersion = 14")
	walkerSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "cobratree", "walker.go")
	require.Contains(t, walkerSrc, "WithOpenWorldHintAnnotation(true)")
	require.NotContains(t, walkerSrc, "WithOpenWorldHintAnnotation(false)")
	require.NotContains(t, walkerSrc, "WithDestructiveHintAnnotation(false), mcplib.WithOpenWorldHintAnnotation(false)")

	writeHintRuntimeTest(t, outputDir, runtime)
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp", "-run", "TestDeviceSafetyMCPAnnotations", "-count=1")
	runGoCommandRequired(t, outputDir, "test", "./internal/learn", "-run", "TestPlaybookIngressClassification|TestRecall_MCPPlaybookIsUntrustedData|TestRecall_CLIPlaybookStaysTrusted", "-count=1")
	runGoCommandRequired(t, outputDir, "test", "./internal/store", "-run", "TestUpsertPlaybook_IngressSurfaceSticks|TestLearnEvent_SurfaceDetection|TestMigrate_V8ToV9AddsCandidatesAndEvents", "-count=1")
	runGoCommandRequired(t, outputDir, "test", "./internal/mcp/cobratree", "-run", "TestMCPChildEnvironKeepsLearnSurface|TestMCPReadOnlySwitch", "-count=1")
	requireGeneratedCompiles(t, outputDir)
}

type hintCase struct {
	name        string
	readOnly    bool
	destructive *bool
	openWorld   *bool
}

func intentNamed(name, description string, endpoints ...string) spec.Intent {
	steps := make([]spec.IntentStep, len(endpoints))
	for i, endpoint := range endpoints {
		steps[i] = spec.IntentStep{Endpoint: endpoint, Capture: fmt.Sprintf("step%d", i+1)}
	}
	return spec.Intent{Name: name, Description: description, Steps: steps, Returns: "step1"}
}

func endpointRef(t *testing.T, api *spec.APISpec, method, path string) string {
	t.Helper()
	for rName, resource := range api.Resources {
		for eName, endpoint := range resource.Endpoints {
			if strings.EqualFold(endpoint.Method, method) && endpoint.Path == path {
				return rName + "." + eName
			}
		}
		for sName, sub := range resource.SubResources {
			for eName, endpoint := range sub.Endpoints {
				if strings.EqualFold(endpoint.Method, method) && endpoint.Path == path {
					return rName + "." + sName + "." + eName
				}
			}
		}
	}
	t.Fatalf("missing %s %s\n%s", method, path, dumpEndpointRefs(api))
	return ""
}

func dumpEndpointRefs(api *spec.APISpec) string {
	var b strings.Builder
	for rName, resource := range api.Resources {
		for eName, endpoint := range resource.Endpoints {
			fmt.Fprintf(&b, "%s.%s %s %s\n", rName, eName, endpoint.Method, endpoint.Path)
		}
		for sName, sub := range resource.SubResources {
			for eName, endpoint := range sub.Endpoints {
				fmt.Fprintf(&b, "%s.%s.%s %s %s\n", rName, sName, eName, endpoint.Method, endpoint.Path)
			}
		}
	}
	return b.String()
}

func toolBlockForPathMust(t *testing.T, src, method, path string) string {
	t.Helper()
	_, block := toolBlockForPath(t, src, method, path)
	return block
}

func toolBlockForPath(t *testing.T, src, method, path string) (string, string) {
	t.Helper()
	quotedPath := strconv.Quote(path)
	searchFrom := 0
	for {
		rel := strings.Index(src[searchFrom:], quotedPath)
		if rel < 0 {
			break
		}
		pathIdx := searchFrom + rel
		windowStart := max(pathIdx-120, 0)
		window := src[windowStart:pathIdx]
		if strings.Contains(window, "makeAPIHandler("+strconv.Quote(method)+",") {
			start := strings.LastIndex(src[:pathIdx], "mcplib.NewTool(")
			require.GreaterOrEqual(t, start, 0)
			name := quotedArg(src[start:], "mcplib.NewTool(")
			return name, src[start:pathIdx]
		}
		searchFrom = pathIdx + len(quotedPath)
	}
	t.Fatalf("no %s handler for %s", method, path)
	return "", ""
}

func intentToolBlock(t *testing.T, src, name string) string {
	t.Helper()
	needle := "mcplib.NewTool(" + strconv.Quote(name)
	idx := strings.Index(src, needle)
	require.GreaterOrEqual(t, idx, 0, "intent %s missing", name)
	rest := src[idx:]
	end := strings.Index(rest, "\n\t\thandle")
	require.GreaterOrEqual(t, end, 0, "intent %s has no handler", name)
	return rest[:end]
}

func builtInToolBlock(t *testing.T, src, name string) string {
	t.Helper()
	needle := "mcplib.NewTool(" + strconv.Quote(name)
	idx := strings.Index(src, needle)
	require.GreaterOrEqual(t, idx, 0, "tool %s missing", name)
	rest := src[idx:]
	end := strings.Index(rest, "\n\t\thandle")
	if end < 0 {
		end = strings.Index(rest, "\n\t\t),")
	}
	require.Greater(t, end, 0, "tool %s block", name)
	return rest[:end]
}

func quotedArg(src, prefix string) string {
	rest := strings.TrimPrefix(src, prefix)
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	value, err := strconv.Unquote(rest[:strings.Index(rest[1:], `"`)+2])
	if err != nil {
		return ""
	}
	return value
}

func assertHintBlock(t *testing.T, name, block string, readOnly bool, destructive *bool, openWorld bool) {
	t.Helper()
	hasReadOnly := strings.Contains(block, "WithReadOnlyHintAnnotation(true)")
	hasDestructiveFalse := strings.Contains(block, "WithDestructiveHintAnnotation(false)")
	hasDestructiveTrue := strings.Contains(block, "WithDestructiveHintAnnotation(true)")
	hasOpenWorld := strings.Contains(block, "WithOpenWorldHintAnnotation(true)")
	if hasReadOnly != readOnly {
		t.Fatalf("%s readOnly = %v, want %v\n%s", name, hasReadOnly, readOnly, block)
	}
	switch {
	case destructive == nil:
		if hasDestructiveFalse || hasDestructiveTrue {
			t.Fatalf("%s destructive hint set, want unset\n%s", name, block)
		}
	case *destructive:
		if !hasDestructiveTrue || hasDestructiveFalse {
			t.Fatalf("%s destructive hint = false/unset, want true\n%s", name, block)
		}
	default:
		if !hasDestructiveFalse || hasDestructiveTrue {
			t.Fatalf("%s destructive hint = true/unset, want false\n%s", name, block)
		}
	}
	if hasOpenWorld != openWorld {
		t.Fatalf("%s openWorld = %v, want %v\n%s", name, hasOpenWorld, openWorld, block)
	}
}

func writeHintRuntimeTest(t *testing.T, outputDir string, cases []hintCase) {
	t.Helper()
	var b strings.Builder
	b.WriteString(`package mcp

import (
	"testing"

	"github.com/mark3labs/mcp-go/server"
)

func TestDeviceSafetyMCPAnnotations(t *testing.T) {
	s := server.NewMCPServer("device-safety", "test")
	RegisterTools(s)
	tools := s.ListTools()

	// mcp-go NewTool defaults destructiveHint and openWorldHint to true.
	// Omitting WithDestructiveHintAnnotation keeps that possibly-destructive
	// default; an explicit false is the additive opt-in.
	check := func(name string, readOnly, destructive, openWorld bool) {
		t.Helper()
		tool, ok := tools[name]
		if !ok {
			var names []string
			for n := range tools {
				names = append(names, n)
			}
			t.Fatalf("%s missing; tools=%v", name, names)
		}
		if tool.Tool.Annotations.ReadOnlyHint == nil || *tool.Tool.Annotations.ReadOnlyHint != readOnly {
			t.Fatalf("%s readOnlyHint = %v, want %v", name, tool.Tool.Annotations.ReadOnlyHint, readOnly)
		}
		if tool.Tool.Annotations.DestructiveHint == nil || *tool.Tool.Annotations.DestructiveHint != destructive {
			t.Fatalf("%s destructiveHint = %v, want %v", name, tool.Tool.Annotations.DestructiveHint, destructive)
		}
		if tool.Tool.Annotations.OpenWorldHint == nil || *tool.Tool.Annotations.OpenWorldHint != openWorld {
			t.Fatalf("%s openWorldHint = %v, want %v", name, tool.Tool.Annotations.OpenWorldHint, openWorld)
		}
	}
`)
	for _, tc := range cases {
		fmt.Fprintf(&b, "\tcheck(%q, %t, %t, %t)\n",
			tc.name, tc.readOnly, runtimeDestructive(tc.destructive), runtimeOpenWorld(tc.openWorld))
	}
	b.WriteString(`
	for _, name := range []string{"teach", "teach_playbook", "playbook_amend", "learnings_confirm", "teach_pattern", "teach_lookup", "learnings_forget", "learnings_reject"} {
		check(name, false, true, true)
	}
	check("recall", true, false, true)
}
`)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "device_safety_hint_test.go"), []byte(b.String()), 0o644))
}

func runtimeDestructive(v *bool) bool {
	if v == nil {
		return true
	}
	return *v
}

func runtimeOpenWorld(v *bool) bool {
	if v == nil {
		return true
	}
	return *v
}
