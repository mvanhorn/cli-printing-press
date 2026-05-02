package pipeline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	apispec "github.com/mvanhorn/cli-printing-press/v3/internal/spec"
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

func TestRunCommandTestsExecutesMockReadCommands(t *testing.T) {
	binary := buildCommandProbeBinary(t)
	cmd := discoveredCommand{Name: "items", Kind: "read"}

	result := runCommandTests(binary, cmd, "mock", os.Environ())
	assert.True(t, result.Help)
	assert.True(t, result.DryRun)
	assert.False(t, result.Execute)
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPositionalPlaceholders(tt.usage)
			assert.Equal(t, tt.want, got)
		})
	}
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
