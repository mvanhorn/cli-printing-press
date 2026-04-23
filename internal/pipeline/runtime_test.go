package pipeline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	apispec "github.com/mvanhorn/cli-printing-press/internal/spec"
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

func TestIsIntentionalStubExit(t *testing.T) {
	assert.True(t, isIntentionalStubExit(fmt.Errorf("exit 3: {\"cf_gated\":true,\"message\":\"stub command\"}")))
	assert.True(t, isIntentionalStubExit(fmt.Errorf("exit 3: {\"cf_gated\": true, \"message\":\"needs manual clearance\"}")))
	assert.False(t, isIntentionalStubExit(fmt.Errorf("exit 3: not implemented because this path is Cloudflare gated")))
	assert.False(t, isIntentionalStubExit(fmt.Errorf("exit 3: stubbed")))
	assert.False(t, isIntentionalStubExit(fmt.Errorf("exit 3: stub command")))
	assert.False(t, isIntentionalStubExit(fmt.Errorf("exit 3: resource not found")))
	assert.False(t, isIntentionalStubExit(nil))
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
