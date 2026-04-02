package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunDogfood(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "store"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
type rootFlags struct {
	jsonOutput bool
	csvOutput  bool
	stdinInput bool
	noCache    bool
	deadOnly   bool
}
func initFlags(flags *rootFlags) {
	_ = &flags.jsonOutput
	_ = &flags.csvOutput
	_ = &flags.stdinInput
	_ = &flags.noCache
	_ = &flags.deadOnly
}
func configure(flags *rootFlags) {
	if flags.noCache {
		disableCache()
	}
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "helpers.go"), `package cli
func usedHelper() {}
func deadHelper() {}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "users_list.go"), `package cli
func usersList() {
	path := "/users/123"
	flags.jsonOutput = true
	usedHelper()
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "projects_get.go"), `package cli
func projectsGet() {
	path := "/bogus"
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "sync.go"), `package cli
func runSync(s interface{ UpsertUsers() error }) error {
	return s.UpsertUsers()
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "search.go"), `package cli
func runSearch(s interface{ SearchUsers() error }) error {
	return s.SearchUsers()
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
func authHeader(token string) string {
	return "Bearer " + token
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "store", "store.go"), "package store\n"+
		"func schema() string {\n"+
		"\treturn `\n"+
		"\t\tCREATE TABLE IF NOT EXISTS users (\n"+
		"\t\t\tid TEXT PRIMARY KEY,\n"+
		"\t\t\tname TEXT NOT NULL,\n"+
		"\t\t\temail TEXT,\n"+
		"\t\t\tdata JSON NOT NULL\n"+
		"\t\t);\n"+
		"\t\tCREATE TABLE IF NOT EXISTS sync_state (\n"+
		"\t\t\tentity_type TEXT PRIMARY KEY,\n"+
		"\t\t\tlast_sync_at TEXT NOT NULL,\n"+
		"\t\t\tcursor TEXT\n"+
		"\t\t);\n"+
		"\t`\n"+
		"}\n")

	specPath := filepath.Join(dir, "spec.json")
	writeTestFile(t, specPath, `{
  "paths": {
    "/users/{id}": {},
    "/projects/{id}": {}
  },
  "components": {
    "securitySchemes": {
      "BotToken": {
        "type": "http",
        "scheme": "bearer"
      }
    }
  }
}`)

	report, err := RunDogfood(dir, specPath)
	require.NoError(t, err)

	assert.Equal(t, "FAIL", report.Verdict)
	assert.Equal(t, 2, report.PathCheck.Tested)
	assert.Equal(t, 1, report.PathCheck.Valid)
	assert.Equal(t, 50, report.PathCheck.Pct)
	assert.Equal(t, []string{"/bogus"}, report.PathCheck.Invalid)
	assert.False(t, report.AuthCheck.Match)
	assert.Equal(t, 5, report.DeadFlags.Total)
	assert.Equal(t, 3, report.DeadFlags.Dead)
	assert.Equal(t, []string{"csvOutput", "deadOnly", "stdinInput"}, report.DeadFlags.Items)
	assert.Equal(t, 2, report.DeadFuncs.Total)
	assert.Equal(t, 1, report.DeadFuncs.Dead)
	assert.Equal(t, []string{"deadHelper"}, report.DeadFuncs.Items)
	assert.True(t, report.PipelineCheck.SyncCallsDomain)
	assert.True(t, report.PipelineCheck.SearchCallsDomain)
	assert.Equal(t, 1, report.PipelineCheck.DomainTables)
	assert.Equal(t, 0, report.ExampleCheck.Tested)
	assert.True(t, report.ExampleCheck.Skipped)
	assert.Equal(t, "no CLI command directory found", report.ExampleCheck.Detail)

	loaded, err := LoadDogfoodResults(dir)
	require.NoError(t, err)
	assert.Equal(t, report.Verdict, loaded.Verdict)
}

func TestRunDogfoodAcceptsYAMLSpec(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "store"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
type rootFlags struct{}
func initFlags(flags *rootFlags) { _ = flags }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "users_get.go"), `package cli
func usersGet() {
	path := "/users/{id}"
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
func authHeader(token string) string {
	return "Bearer " + token
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "store", "store.go"), "package store\n")

	specPath := filepath.Join(dir, "spec.yaml")
	writeTestFile(t, specPath, `openapi: 3.0.0
info:
  title: Users API
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /users/{id}:
    get:
      operationId: getUser
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: ok
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
security:
  - bearerAuth: []
`)

	report, err := RunDogfood(dir, specPath)
	require.NoError(t, err)
	assert.Equal(t, 1, report.PathCheck.Tested)
	assert.Equal(t, 1, report.PathCheck.Valid)
	assert.True(t, report.AuthCheck.Match)
}

func TestCountDomainTables(t *testing.T) {
	storeSource := `
CREATE TABLE IF NOT EXISTS users (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	email TEXT,
	data JSON NOT NULL
);

CREATE TABLE IF NOT EXISTS sync_state (
	entity_type TEXT PRIMARY KEY,
	last_sync_at TEXT NOT NULL,
	cursor TEXT
);
`
	assert.Equal(t, 1, countDomainTables(storeSource))
}

func TestDeriveDogfoodVerdict(t *testing.T) {
	report := &DogfoodReport{
		PathCheck:     PathCheckResult{Tested: 10, Valid: 10, Pct: 100},
		AuthCheck:     AuthCheckResult{Match: true},
		DeadFlags:     DeadCodeResult{Dead: 1},
		DeadFuncs:     DeadCodeResult{Dead: 0},
		PipelineCheck: PipelineResult{SyncCallsDomain: true},
	}
	assert.Equal(t, "WARN", deriveDogfoodVerdict(report, true))

	report.DeadFlags.Dead = 0
	report.DeadFuncs.Dead = 1
	assert.Equal(t, "WARN", deriveDogfoodVerdict(report, true))

	report.DeadFuncs.Dead = 0
	report.PipelineCheck.SyncCallsDomain = false
	assert.Equal(t, "WARN", deriveDogfoodVerdict(report, true))

	report.PipelineCheck.SyncCallsDomain = true
	assert.Equal(t, "PASS", deriveDogfoodVerdict(report, true))

	report.ExampleCheck = ExampleCheckResult{Tested: 10, WithExamples: 4}
	assert.Equal(t, "FAIL", deriveDogfoodVerdict(report, true))

	report.ExampleCheck = ExampleCheckResult{Tested: 10, WithExamples: 5}
	assert.Equal(t, "PASS", deriveDogfoodVerdict(report, true))

	report.ExampleCheck = ExampleCheckResult{Tested: 10, WithExamples: 10, InvalidFlags: []string{"--bogus"}}
	assert.Equal(t, "WARN", deriveDogfoodVerdict(report, true))

	report.ExampleCheck = ExampleCheckResult{Skipped: true, Detail: "could not build CLI binary"}
	assert.Equal(t, "WARN", deriveDogfoodVerdict(report, true))

	report.ExampleCheck = ExampleCheckResult{Tested: 10, WithExamples: 10, ValidExamples: 10}
	assert.Equal(t, "PASS", deriveDogfoodVerdict(report, true))
}

func TestExtractExamplesSection(t *testing.T) {
	tests := []struct {
		name string
		help string
		want string
	}{
		{
			name: "standard cobra help",
			help: "Some command\n\nUsage:\n  cli users list [flags]\n\nExamples:\n  # List all users\n  cli users list --limit 10\n\nFlags:\n  --limit int   max results\n",
			want: "# List all users\n  cli users list --limit 10",
		},
		{
			name: "no examples section",
			help: "Some command\n\nUsage:\n  cli version\n\nFlags:\n  -h, --help   help\n",
			want: "",
		},
		{
			name: "examples before global flags",
			help: "Examples:\n  cli foo --bar baz\n\nGlobal Flags:\n  --config string\n",
			want: "cli foo --bar baz",
		},
		{
			name: "multi-line examples",
			help: "Examples:\n  # First example\n  cli do --a 1\n\n  # Second example\n  cli do --b 2\n\nFlags:\n  --a int\n",
			want: "# First example\n  cli do --a 1\n\n  # Second example\n  cli do --b 2",
		},
		{
			name: "empty help",
			help: "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractExamplesSection(tt.help))
		})
	}
}

func TestExtractFlagNames(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "multiple flags",
			text: "cli users list --limit 10 --format json",
			want: []string{"format", "limit"},
		},
		{
			name: "deduplication",
			text: "--flag value --flag other",
			want: []string{"flag"},
		},
		{
			name: "hyphenated flag names",
			text: "--dry-run --output-format table",
			want: []string{"dry-run", "output-format"},
		},
		{
			name: "ignores short flags",
			text: "-h --help -v --verbose",
			want: []string{"help", "verbose"},
		},
		{
			name: "no flags",
			text: "just some text with no flags",
			want: nil,
		},
		{
			name: "ignores uppercase",
			text: "--OK should not match",
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractFlagNames(tt.text))
		})
	}
}

func TestDeadFunctions_TransitiveReachability(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "helpers.go"), `package cli

func funcA() {
	funcB()
}

func funcB() {
	// only called by funcA
}

func funcC() {
	// never called by anything
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "cmd.go"), `package cli

func runCmd() {
	funcA()
}
`)

	result := checkDeadFunctions(dir)
	assert.Equal(t, 3, result.Total)
	assert.Equal(t, 1, result.Dead)
	assert.Equal(t, []string{"funcC"}, result.Items)
}

func TestDeadFunctions_ChainOfThree(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "helpers.go"), `package cli

func funcA() {
	funcB()
}

func funcB() {
	funcC()
}

func funcC() {
	// end of chain
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "cmd.go"), `package cli

func runCmd() {
	funcA()
}
`)

	result := checkDeadFunctions(dir)
	assert.Equal(t, 3, result.Total)
	assert.Equal(t, 0, result.Dead)
	assert.Empty(t, result.Items)
}

func TestDeadFunctions_GenuinelyDead(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "helpers.go"), `package cli

func funcD() {
	// defined but never called
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "cmd.go"), `package cli

func runCmd() {
	// does not call funcD
}
`)

	result := checkDeadFunctions(dir)
	assert.Equal(t, 1, result.Total)
	assert.Equal(t, 1, result.Dead)
	assert.Equal(t, []string{"funcD"}, result.Items)
}

func TestDeadFlags_FrameworkFlags(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli

type rootFlags struct {
	agent     bool
	rateLimit int
	noCache   bool
	deadOnly  bool
}

func initFlags(flags *rootFlags) {
	_ = &flags.agent
	_ = &flags.rateLimit
	_ = &flags.noCache
	_ = &flags.deadOnly
}

func (f *rootFlags) newClient() {
	client.New(cfg, f.rateLimit)
}

func execute(flags *rootFlags) {
	if flags.agent {
		enableAgent()
	}
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "export.go"), `package cli

func runExport(flags *rootFlags) {
	if flags.noCache {
		skipCache()
	}
}
`)

	result := checkDeadFlags(dir)
	assert.Equal(t, 4, result.Total)
	assert.Equal(t, 1, result.Dead)
	assert.Equal(t, []string{"deadOnly"}, result.Items)
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}
