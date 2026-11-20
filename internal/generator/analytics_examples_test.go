package generator

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/shellargs"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyticsExamplePrefersFirstDefaultSyncedResource(t *testing.T) {
	t.Parallel()

	api := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"zeta": {Endpoints: map[string]spec.Endpoint{
				"list": {Method: "GET", Path: "/zeta", Response: spec.ResponseDef{Type: "array", Item: "Zeta"}},
			}},
		},
		Types: map[string]spec.TypeDef{
			"Zeta": {Fields: []spec.TypeField{{Name: "title"}}},
		},
	}
	syncable := []profiler.SyncableResource{
		{Name: "zeta", Path: "/zeta", Method: "GET"},
		{Name: "alpha", Path: "/alpha", Method: "GET"},
		{Name: "skipped", Path: "/skipped", Method: "GET", SkipDefaultSync: true},
	}

	example := analyticsExampleFor(api, syncable, nil)
	assert.Equal(t, "alpha", example.Type)
	assert.Empty(t, example.GroupBy)

	text := analyticsHelpExample("demo", example)
	assert.Contains(t, text, "demo-pp-cli analytics --type alpha")
	assert.NotContains(t, text, "--group-by")
	assert.NotContains(t, text, "--type zeta")
	assert.NotContains(t, text, "--type messages")
	assertAnalyticsHelpParses(t, "demo-pp-cli", text)
}

func TestAnalyticsExampleUsesResponseFieldsAndOmitsUnknownOnes(t *testing.T) {
	t.Parallel()

	example := analyticsExampleFor(analyticsWidgetSpec("widgets"), []profiler.SyncableResource{
		{Name: "widgets", Path: "/widgets", Method: "GET"},
	}, nil)
	assert.Equal(t, "widgets", example.Type)
	assert.Equal(t, []string{"id", "status"}, example.GroupBy)

	text := analyticsHelpExample("widgets", example)
	assert.Contains(t, text, "widgets-pp-cli analytics --type widgets --group-by id")
	assert.Contains(t, text, "widgets-pp-cli analytics --type widgets --group-by status --limit 10 --json")
	assert.NotContains(t, text, "author_id")
	assert.NotContains(t, text, "channel_id")
	assert.NotContains(t, text, "owner_id")
	assertAnalyticsHelpParses(t, "widgets-pp-cli", text)
}

func TestAnalyticsExampleGroupFieldsStayOnTheSyncMethodWhenBaseURLChangesThePath(t *testing.T) {
	t.Parallel()

	api := &spec.APISpec{
		Resources: map[string]spec.Resource{
			"widgets": {
				BaseURL: "https://widgets.example.com/v1",
				Endpoints: map[string]spec.Endpoint{
					"create": {Method: "POST", Path: "/widgets", Response: spec.ResponseDef{Item: "Created"}},
					"list":   {Method: "GET", Path: "/widgets", Response: spec.ResponseDef{Type: "array", Item: "Widget"}},
				},
			},
		},
		Types: map[string]spec.TypeDef{
			"Widget":  {Fields: []spec.TypeField{{Name: "id"}, {Name: "status"}}},
			"Created": {Fields: []spec.TypeField{{Name: "receipt"}}},
		},
	}
	withBase := []profiler.SyncableResource{{
		Name: "widgets", Path: "https://widgets.example.com/v1/widgets", Method: "GET",
	}}
	assert.Equal(t, []string{"id", "status"}, analyticsExampleFor(api, withBase, nil).GroupBy)

	original := []profiler.SyncableResource{{Name: "widgets", Path: "/widgets", Method: "GET"}}
	assert.Equal(t, []string{"id", "status"}, analyticsExampleFor(api, original, nil).GroupBy)

	miss := []profiler.SyncableResource{{Name: "widgets", Path: "/other", Method: "GET"}}
	assert.Empty(t, analyticsExampleFor(api, miss, nil).GroupBy)
	assert.NotContains(t, analyticsHelpExample("widgets", analyticsExampleFor(api, miss, nil)), "receipt")
}

func TestAnalyticsExampleRepeatsTheOnlyKnownGroupField(t *testing.T) {
	t.Parallel()

	api := analyticsWidgetSpec("onefield")
	api.Types["Widget"] = spec.TypeDef{Fields: []spec.TypeField{{Name: "id"}, {Name: "id"}, {Name: "bad field"}}}
	example := analyticsExampleFor(api, []profiler.SyncableResource{
		{Name: "widgets", Path: "/widgets", Method: "GET"},
	}, nil)
	assert.Equal(t, []string{"id"}, example.GroupBy)
	text := analyticsHelpExample("onefield", example)
	assert.Contains(t, text, "--group-by id --limit 10 --json")
	assert.NotContains(t, text, "bad field")
	assertAnalyticsHelpParses(t, "onefield-pp-cli", text)
}

func TestAnalyticsExampleUsesSkipDefaultThenDependent(t *testing.T) {
	t.Parallel()

	skipped := []profiler.SyncableResource{{Name: "drafts", Path: "/drafts", Method: "GET", SkipDefaultSync: true}}
	example := analyticsExampleFor(nil, skipped, []profiler.DependentResource{{Name: "tasks", Path: "/parents/{id}/tasks", Method: "GET"}})
	assert.Equal(t, "drafts", example.Type)

	example = analyticsExampleFor(nil, nil, []profiler.DependentResource{
		{Name: "tasks", Path: "/parents/{id}/tasks", Method: "GET"},
		{Name: "notes", Path: "/parents/{id}/notes", Method: "GET"},
	})
	assert.Equal(t, "notes", example.Type)
	assert.Empty(t, example.GroupBy)
	assertAnalyticsHelpParses(t, "child-pp-cli", analyticsHelpExample("child", example))
}

func TestAnalyticsExampleSkipsVestigialAndUnsafeNames(t *testing.T) {
	t.Parallel()

	syncable := []profiler.SyncableResource{
		{Name: "pages", Path: "/pages", Method: "GET", UsesHTMLResponse: true},
		{Name: `foo"bar`, Path: "/foo", Method: "GET"},
		{Name: "line\nbreak", Path: "/line", Method: "GET"},
		{Name: "-hidden", Path: "/hidden", Method: "GET"},
	}
	example := analyticsExampleFor(nil, syncable, nil)
	assert.Empty(t, example.Type)
	text := analyticsHelpExample("demo", example)
	assert.Contains(t, text, "demo-pp-cli analytics\n")
	assert.Contains(t, text, "demo-pp-cli analytics --json")
	assert.NotContains(t, text, "--type")
	assert.NotContains(t, text, `foo"bar`)
	assert.NotContains(t, text, "line\nbreak")
	assert.NotContains(t, text, "-hidden")
	assertAnalyticsHelpParses(t, "demo-pp-cli", text)

	assert.Equal(t, []string{"-hidden", `foo"bar`, "line\nbreak", "pages"}, analyticsCompiledResourceTypes(syncable, []profiler.DependentResource{{Name: "pages"}}))
}

func TestAnalyticsHelpExampleUsesAFallbackBinaryName(t *testing.T) {
	t.Parallel()

	text := analyticsHelpExample("  ", analyticsExample{})
	assert.Contains(t, text, "cli-pp-cli analytics")
	assert.NotContains(t, text, "--type")
	assertAnalyticsHelpParses(t, "cli-pp-cli", text)
}

func TestAnalyticsExampleIgnoresVestigialWhenAStoreResourceExists(t *testing.T) {
	t.Parallel()

	example := analyticsExampleFor(nil, []profiler.SyncableResource{
		{Name: "pages", Path: "/pages", Method: "GET", UsesHTMLResponse: true},
		{Name: "items", Path: "/items", Method: "GET"},
	}, nil)
	assert.Equal(t, "items", example.Type)
}

func TestGeneratedAnalyticsExamplesRunWithoutMessagesResource(t *testing.T) {
	t.Parallel()

	apiSpec := analyticsWidgetSpec("widgetex")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Analytics: true, MCP: true}
	require.NoError(t, gen.Generate())

	src := readGeneratedFile(t, outputDir, "internal", "cli", "analytics.go")
	require.Contains(t, src, "analytics --type widgets")
	require.Contains(t, src, "--group-by id")
	require.Contains(t, src, "--group-by status")
	require.NotContains(t, src, "--type messages")
	require.NotContains(t, src, "author_id")
	require.NotContains(t, src, "channel_id")
	require.Contains(t, src, `"widgets":`)
	require.Regexp(t, `(?s)analyticsCompiledResourceTypes\(\) map\[string\]struct\{\} \{.*?"widgets": \{\},`, src)

	runtimeTest := strings.ReplaceAll(analyticsExampleRuntimeTest, "__MODULE_PATH__", generatedModulePath(t, outputDir))
	runtimeTest = strings.ReplaceAll(runtimeTest, "__BAKED_EXAMPLE_ARGS__", generatedAnalyticsExampleArgsLiteral(t, outputDir))
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "cli", "analytics_examples_runtime_test.go"),
		[]byte(runtimeTest),
		0o644,
	))
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "^TestAnalyticsExamplesExitZero$", "-count=1")
}

func TestGeneratedAnalyticsExamplesWithoutSafeResourceName(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("unsafeex")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, Analytics: true, MCP: true}
	require.NoError(t, gen.Generate())

	// Identifier-safe spec keys keep the rest of the module compiling. The
	// profile names are what analytics help and --type accept, so quote,
	// backslash, and newline names are injected there and only that file
	// is re-emitted.
	applyAdversarialSyncHintNames(t, gen)
	schema := gen.schemaWithDependentParents()
	require.NoError(t, gen.renderTemplate("analytics.go.tmpl", filepath.Join("internal", "cli", "analytics.go"), gen.visionRenderData(schema)))

	src := readGeneratedFile(t, outputDir, "internal", "cli", "analytics.go")
	require.NotContains(t, src, "--type")
	require.NotContains(t, src, `foo"bar`)
	require.Contains(t, src, `foo\"bar`)
	require.Contains(t, src, "unsafeex-pp-cli analytics")
	_, err := parser.ParseFile(token.NewFileSet(), "analytics.go", src, parser.AllErrors)
	require.NoError(t, err)

	runtimeTest := strings.ReplaceAll(analyticsNoTypeExampleRuntimeTest, "__MODULE_PATH__", generatedModulePath(t, outputDir))
	runtimeTest = strings.ReplaceAll(runtimeTest, "__BAKED_EXAMPLE_ARGS__", generatedAnalyticsExampleArgsLiteral(t, outputDir))
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "cli", "analytics_examples_runtime_test.go"),
		[]byte(runtimeTest),
		0o644,
	))
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "^TestAnalyticsSummaryExamplesExitZero$", "-count=1")
}

func analyticsWidgetSpec(name string) *spec.APISpec {
	api := minimalSpec(name)
	api.Resources = map[string]spec.Resource{
		"widgets": {
			Description: "Manage widgets",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/widgets",
					Description: "List widgets",
					Response:    spec.ResponseDef{Type: "array", Item: "Widget"},
				},
			},
		},
	}
	api.Types = map[string]spec.TypeDef{
		"Widget": {Fields: []spec.TypeField{
			{Name: "id"},
			{Name: "status"},
			{Name: "owner_id"},
		}},
	}
	return api
}

func assertAnalyticsHelpParses(t *testing.T, bin, text string) {
	t.Helper()
	var saw bool
	for line := range strings.SplitSeq(text, "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		args, err := shellargs.ArgsAfterBinary(trim)
		require.NoError(t, err, trim)
		require.NotEmpty(t, args)
		assert.Equal(t, "analytics", args[0], trim)
		assert.NotContains(t, args, "messages")
		saw = true
		require.True(t, strings.HasPrefix(trim, bin+" "), trim)
	}
	require.True(t, saw, "help text had no commands:\n%s", text)
}

func generatedAnalyticsExampleArgsLiteral(t *testing.T, outputDir string) string {
	t.Helper()
	bin := filepath.Base(outputDir)
	out, err := runGoCommandOutput(t, outputDir, "run", "-mod=mod", "./cmd/"+bin, "analytics", "--help")
	require.NoError(t, err, out)
	return goArgRowsLiteral(analyticsHelpCommandArgs(t, out))
}

func analyticsHelpCommandArgs(t *testing.T, help string) [][]string {
	t.Helper()
	var in bool
	var cmds [][]string
	for line := range strings.SplitSeq(help, "\n") {
		if strings.TrimSpace(line) == "Examples:" {
			in = true
			continue
		}
		if !in {
			continue
		}
		if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			break
		}
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		args, err := shellargs.ArgsAfterBinary(trim)
		require.NoError(t, err, trim)
		require.NotEmpty(t, args, trim)
		require.Equal(t, "analytics", args[0], trim)
		cmds = append(cmds, args[1:])
	}
	require.NotEmpty(t, cmds, "analytics help has no examples:\n%s", help)
	return cmds
}

func goArgRowsLiteral(rows [][]string) string {
	var b strings.Builder
	b.WriteString("[][]string{\n")
	for _, row := range rows {
		b.WriteString("\t\t{")
		for i, arg := range row {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strconv.Quote(arg))
		}
		b.WriteString("},\n")
	}
	b.WriteString("\t}")
	return b.String()
}

const analyticsExampleRuntimeTest = `package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"__MODULE_PATH__/internal/cliutil"
	"__MODULE_PATH__/internal/store"
)

func TestAnalyticsExamplesExitZero(t *testing.T) {
	isolateAnalyticsExampleHome(t)
	dbPath := filepath.Join(t.TempDir(), "data.db")
	db, err := store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	examples := __BAKED_EXAMPLE_ARGS__
	if len(examples) == 0 {
		t.Fatal("analytics help has no examples")
	}
	sawType := false
	sawGroup := false
	for _, args := range examples {
		for i := 0; i < len(args); i++ {
			if args[i] == "--type" && i+1 < len(args) {
				if args[i+1] == "messages" {
					t.Fatalf("placeholder type in %v", args)
				}
				if args[i+1] != "widgets" {
					t.Fatalf("example type = %q, want widgets", args[i+1])
				}
				sawType = true
			}
			if args[i] == "--group-by" && i+1 < len(args) {
				switch args[i+1] {
				case "id", "status":
					sawGroup = true
				default:
					t.Fatalf("group-by %q is not a widget field", args[i+1])
				}
			}
		}
		stdout, stderr, err := runAnalyticsExample(t, dbPath, args...)
		if err != nil {
			t.Fatalf("empty example %v: %v\nstderr=%s\nstdout=%s", args, err, stderr, stdout)
		}
	}
	if !sawType || !sawGroup {
		t.Fatalf("examples = %v, want --type widgets and --group-by", examples)
	}

	_, _, err = runAnalyticsExample(t, dbPath, "--type", "messages")
	if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "unknown resource type \"messages\"") || !strings.Contains(err.Error(), "widgets") {
		t.Fatalf("placeholder on empty store = %v", err)
	}
	_, _, err = runAnalyticsExample(t, dbPath, "--type", "WIDGETS")
	if err == nil || ExitCode(err) != 2 {
		t.Fatalf("case-mismatched type = %v", err)
	}
	_, _, err = runAnalyticsExample(t, dbPath, "--type", "widget")
	if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "unknown resource type \"widget\"") {
		t.Fatalf("prefix type = %v", err)
	}

	db, err = store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	raw := json.RawMessage("{\"id\":\"w1\",\"status\":\"open\",\"owner_id\":\"u1\"}")
	if err := db.Upsert("widgets", "w1", raw); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seeded: %v", err)
	}
	sqlCount := analyticsExampleSQLCount(t, dbPath, "widgets")
	if sqlCount != 1 {
		t.Fatalf("SELECT COUNT(*) = %d, want 1", sqlCount)
	}
	for _, args := range examples {
		stdout, stderr, err := runAnalyticsExample(t, dbPath, args...)
		if err != nil {
			t.Fatalf("seeded example %v: %v\nstderr=%s\nstdout=%s", args, err, stderr, stdout)
		}
		if containsFlag(args, "--group-by") && !strings.Contains(stdout, "open") && !strings.Contains(stdout, "w1") {
			t.Fatalf("group-by output = %q", stdout)
		}
	}
	stdout, stderr, err := runAnalyticsExample(t, dbPath, "--type", "widgets", "--json")
	if err != nil {
		t.Fatalf("count: %v\nstderr=%s", err, stderr)
	}
	var counted map[string]any
	if err := json.Unmarshal([]byte(stdout), &counted); err != nil {
		t.Fatalf("count json: %v\n%s", err, stdout)
	}
	if counted["count"] != float64(sqlCount) {
		t.Fatalf("command count = %#v, SELECT COUNT(*) = %d", counted, sqlCount)
	}
	_, _, err = runAnalyticsExample(t, dbPath, "--type", "messages", "--group-by", "author_id")
	if err == nil || ExitCode(err) != 2 || strings.Contains(err.Error(), "group-by field") {
		t.Fatalf("placeholder with group-by = %v", err)
	}
}

func runAnalyticsExample(t *testing.T, dbPath string, args ...string) (string, string, error) {
	t.Helper()
	root := RootCmd()
	var stdout, stderr strings.Builder
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	full := append([]string{"analytics"}, args...)
	full = append(full, "--db", dbPath)
	root.SetArgs(full)
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

func analyticsExampleSQLCount(t *testing.T, dbPath, resourceType string) int {
	t.Helper()
	ro, err := store.OpenReadOnlyContext(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open read-only: %v", err)
	}
	defer ro.Close()
	var n int
	if err := ro.DB().QueryRow("SELECT COUNT(*) FROM resources WHERE resource_type = ?", resourceType).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func containsFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}
	return false
}

func isolateAnalyticsExampleHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "")
	t.Setenv(cliutil.MCPChildEnvVar, "")
}
`

const analyticsNoTypeExampleRuntimeTest = `package cli

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"__MODULE_PATH__/internal/cliutil"
	"__MODULE_PATH__/internal/store"
)

func TestAnalyticsSummaryExamplesExitZero(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "")
	t.Setenv(cliutil.MCPChildEnvVar, "")

	dbPath := filepath.Join(t.TempDir(), "data.db")
	db, err := store.OpenWithContext(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	examples := __BAKED_EXAMPLE_ARGS__
	if len(examples) == 0 {
		t.Fatal("analytics help has no examples")
	}
	for _, args := range examples {
		for _, arg := range args {
			if arg == "--type" {
				t.Fatalf("summary example passed --type: %v", args)
			}
		}
		cmd := RootCmd()
		var stdout, stderr strings.Builder
		cmd.SetOut(&stdout)
		cmd.SetErr(&stderr)
		cmd.SetArgs(append(append([]string{"analytics"}, args...), "--db", dbPath))
		if err := cmd.Execute(); err != nil {
			t.Fatalf("example %v: %v\nstderr=%s\nstdout=%s", args, err, stderr.String(), stdout.String())
		}
	}

	cmd := RootCmd()
	var stdout, stderr strings.Builder
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"analytics", "--type", "messages", "--db", dbPath})
	err = cmd.Execute()
	if err == nil || ExitCode(err) != 2 || !strings.Contains(err.Error(), "unknown resource type \"messages\"") || !strings.Contains(err.Error(), "foo\"bar") {
		t.Fatalf("placeholder type = %v\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}

	cmd = RootCmd()
	stdout.Reset()
	stderr.Reset()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"analytics", "--type", "foo\"bar", "--json", "--db", dbPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("exact compiled-in name: %v\nstderr=%s\nstdout=%s", err, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "\"count\":0") && !strings.Contains(stdout.String(), "\"count\": 0") {
		t.Fatalf("hostile type count = %s", stdout.String())
	}

	cmd = RootCmd()
	stdout.Reset()
	stderr.Reset()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"analytics", "--type", "foo\"barx", "--db", dbPath})
	err = cmd.Execute()
	if err == nil || ExitCode(err) != 2 {
		t.Fatalf("lookalike type = %v\nstdout=%s", err, stdout.String())
	}
}
`
