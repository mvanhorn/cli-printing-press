package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupVerifierDirs(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "store"), 0o755))
}

func TestPathProof_DetectsHallucinatedEndpoint(t *testing.T) {
	dir := t.TempDir()
	setupVerifierDirs(t, dir)

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "users_get.go"), `package cli
func usersGet() {
	path = "/users/{id}"
	path = replacePathParam(path, "id", args[0])
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "bogus_get.go"), `package cli
func bogusGet() {
	path = "/bogus/endpoint"
}
`)

	specPath := filepath.Join(dir, "spec.json")
	writeTestFile(t, specPath, `{
  "paths": {
    "/users/{user_id}": {}
  },
  "components": { "securitySchemes": {} }
}`)

	v, err := NewVerifier(dir, specPath)
	require.NoError(t, err)

	results := v.PathProof()
	require.Len(t, results, 2)

	var validCount, invalidCount int
	for _, r := range results {
		if r.InSpec {
			validCount++
		} else {
			invalidCount++
			assert.Equal(t, "/bogus/endpoint", r.Path)
		}
	}
	assert.Equal(t, 1, validCount, "one path should be in spec")
	assert.Equal(t, 1, invalidCount, "one path should be hallucinated")
}

func TestPathProof_DetectsUnresolvedPathTemplatePlaceholders(t *testing.T) {
	dir := t.TempDir()
	setupVerifierDirs(t, dir)

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "feed.go"), `package cli
func runFeed() {
	path := "/feeds/{type}/{month}"
	_ = c.Get(path)
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "accounts.go"), `package cli
func runAccounts() {
	path := "/accounts/{account_id}/workers/{account_id}"
	path = replacePathParam(path, "account_id", args[0])
	_ = c.Get(path)
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "direct.go"), `package cli
func runDirect() {
	_ = c.Get("/direct/{id}")
}
`)

	specPath := filepath.Join(dir, "spec.yaml")
	writeTestFile(t, specPath, `openapi: 3.0.0
info:
  title: Template API
  version: "1.0"
paths:
  /feeds/{type}/{month}:
    get:
      responses:
        "200":
          description: ok
  /accounts/{account_id}/workers/{account_id}:
    get:
      responses:
        "200":
          description: ok
  /direct/{id}:
    get:
      responses:
        "200":
          description: ok`)

	v, err := NewVerifier(dir, specPath)
	require.NoError(t, err)

	results := v.PathProof()
	require.Len(t, results, 3)

	var feed, accounts, direct PathProofResult
	for _, r := range results {
		switch r.File {
		case "feed.go":
			feed = r
		case "accounts.go":
			accounts = r
		case "direct.go":
			direct = r
		}
	}
	assert.False(t, feed.Valid)
	assert.Equal(t, 3, feed.Line)
	assert.Equal(t, []string{"type", "month"}, feed.UnresolvedPlaceholders)
	assert.True(t, accounts.Valid, "one substitution covers repeated account_id placeholders")
	assert.Equal(t, 3, accounts.Line)
	assert.Empty(t, accounts.UnresolvedPlaceholders)
	assert.False(t, direct.Valid)
	assert.Equal(t, 3, direct.Line)
	assert.Equal(t, []string{"id"}, direct.UnresolvedPlaceholders)
}

func TestNewVerifierAcceptsYAMLSpec(t *testing.T) {
	dir := t.TempDir()
	setupVerifierDirs(t, dir)

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "users_get.go"), `package cli
func usersGet() {
	path = "/users/{id}"
	path = replacePathParam(path, "id", args[0])
}
`)

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

	v, err := NewVerifier(dir, specPath)
	require.NoError(t, err)

	results := v.PathProof()
	require.Len(t, results, 1)
	assert.True(t, results[0].InSpec)

	auth := v.AuthProof()
	assert.NotEqual(t, "spec not provided; auth check skipped", auth.Detail)
	assert.NotEmpty(t, auth.SpecScheme)
}

func TestPathProof_SkipsLocalCommands(t *testing.T) {
	dir := t.TempDir()
	setupVerifierDirs(t, dir)

	// These are in verifyInfraFiles and should be skipped
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "search.go"), `package cli
func runSearch() {
	query := "hello"
	_ = query
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "sync.go"), `package cli
func runSync() {
	state := "ready"
	_ = state
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "analytics.go"), `package cli
func runAnalytics() {
	count := 42
	_ = count
}
`)

	specPath := filepath.Join(dir, "spec.json")
	writeTestFile(t, specPath, `{
  "paths": { "/users/{id}": {} },
  "components": { "securitySchemes": {} }
}`)

	v, err := NewVerifier(dir, specPath)
	require.NoError(t, err)

	results := v.PathProof()
	assert.Empty(t, results, "infra/local files should be skipped, no false positives")
}

func TestFlagProof_DetectsDeadFlags(t *testing.T) {
	dir := t.TempDir()
	setupVerifierDirs(t, dir)

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
type rootFlags struct {
	jsonOutput bool
	csvOutput  bool
	dryRun     bool
}
func initFlags(flags *rootFlags) {
	_ = &flags.jsonOutput
	_ = &flags.csvOutput
	_ = &flags.dryRun
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "users_list.go"), `package cli
func usersList() {
	flags.jsonOutput = true
}
`)
	// client.go uses "dryRun" (lowercase d) to match the field name exactly
	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
type Client struct {
	dryRun bool
}
func (c *Client) Do() {
	if c.dryRun {
		return
	}
}
`)

	v, err := NewVerifier(dir, "")
	require.NoError(t, err)

	results := v.FlagProof()
	require.Len(t, results, 3)

	flagMap := make(map[string]FlagProofResult)
	for _, r := range results {
		flagMap[r.Flag] = r
	}

	assert.False(t, flagMap["jsonOutput"].DeadFlag, "jsonOutput is used in CLI")
	assert.True(t, flagMap["csvOutput"].DeadFlag, "csvOutput has no references")
	// FlagProof counts strings.Count(clientSource, field) where field="dryRun"
	assert.False(t, flagMap["dryRun"].DeadFlag, "dryRun is referenced in client.go")
}

func TestFlagProof_IndirectUsageThroughClient(t *testing.T) {
	dir := t.TempDir()
	setupVerifierDirs(t, dir)

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
type rootFlags struct {
	timeout int
}
func initFlags(flags *rootFlags) {
	_ = &flags.timeout
}
`)
	// No CLI file references flags.timeout, but client.go has "timeout" substring
	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
type Client struct {
	timeout int
}
func (c *Client) SetTimeout(t int) {
	c.timeout = t
}
`)

	v, err := NewVerifier(dir, "")
	require.NoError(t, err)

	results := v.FlagProof()
	require.Len(t, results, 1)
	assert.Equal(t, "timeout", results[0].Flag)
	assert.False(t, results[0].DeadFlag, "timeout is referenced indirectly through client.go")
	assert.Greater(t, results[0].RefCount, 0)
}

func TestPipelineProof_DetectsGhostTable(t *testing.T) {
	dir := t.TempDir()
	setupVerifierDirs(t, dir)

	writeTestFile(t, filepath.Join(dir, "internal", "store", "store.go"), "package store\n"+
		"func schema() string {\n"+
		"\treturn `\n"+
		"\t\tCREATE TABLE IF NOT EXISTS messages (\n"+
		"\t\t\tid TEXT PRIMARY KEY,\n"+
		"\t\t\tchannel_id TEXT NOT NULL,\n"+
		"\t\t\tauthor TEXT NOT NULL,\n"+
		"\t\t\tcontent TEXT NOT NULL,\n"+
		"\t\t\ttimestamp TEXT NOT NULL,\n"+
		"\t\t\tdata JSON NOT NULL\n"+
		"\t\t);\n"+
		"\t\tCREATE TABLE IF NOT EXISTS sync_state (\n"+
		"\t\t\tentity_type TEXT PRIMARY KEY,\n"+
		"\t\t\tlast_sync_at TEXT NOT NULL,\n"+
		"\t\t\tcursor TEXT\n"+
		"\t\t);\n"+
		"\t`\n"+
		"}\n")

	// sync.go calls generic Upsert, NOT UpsertMessage
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "sync.go"), `package cli
func runSync() {
	_ = Upsert("something")
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "search.go"), `package cli
func runSearch() {
	_ = SearchMessage("query")
}
`)

	v, err := NewVerifier(dir, "")
	require.NoError(t, err)

	results := v.PipelineProof()
	require.Len(t, results, 1, "sync_state is exempt, only messages should appear")

	msg := results[0]
	assert.Equal(t, "messages", msg.TableName)
	assert.True(t, msg.GhostTable, "messages has no write path (UpsertMessage not called)")
	assert.GreaterOrEqual(t, msg.Columns, 5)
}

func TestPipelineProof_DetectsOrphanFTS(t *testing.T) {
	dir := t.TempDir()
	setupVerifierDirs(t, dir)

	writeTestFile(t, filepath.Join(dir, "internal", "store", "store.go"), "package store\n"+
		"func schema() string {\n"+
		"\treturn `\n"+
		"\t\tCREATE TABLE IF NOT EXISTS messages (\n"+
		"\t\t\tid TEXT PRIMARY KEY,\n"+
		"\t\t\tchannel_id TEXT NOT NULL,\n"+
		"\t\t\tauthor TEXT NOT NULL,\n"+
		"\t\t\tcontent TEXT NOT NULL,\n"+
		"\t\t\ttimestamp TEXT NOT NULL,\n"+
		"\t\t\tdata JSON NOT NULL\n"+
		"\t\t);\n"+
		"\t\tCREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(content, content='messages');\n"+
		"\t`\n"+
		"}\n")

	// sync.go writes to messages via UpsertMessage
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "sync.go"), `package cli
func runSync() {
	_ = UpsertMessage("data")
}
`)
	// But NO command calls SearchMessage
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "list.go"), `package cli
func runList() {
	_ = GetMessage("id")
}
`)

	v, err := NewVerifier(dir, "")
	require.NoError(t, err)

	results := v.PipelineProof()
	require.Len(t, results, 1)

	msg := results[0]
	assert.Equal(t, "messages", msg.TableName)
	assert.True(t, msg.HasWrite, "UpsertMessage is called")
	assert.False(t, msg.GhostTable, "write path exists")
	assert.True(t, msg.FTSExists, "messages_fts exists")
	assert.True(t, msg.OrphanFTS, "no command calls SearchMessage")
}

func TestPipelineProof_HealthyPipeline(t *testing.T) {
	dir := t.TempDir()
	setupVerifierDirs(t, dir)

	writeTestFile(t, filepath.Join(dir, "internal", "store", "store.go"), "package store\n"+
		"func schema() string {\n"+
		"\treturn `\n"+
		"\t\tCREATE TABLE IF NOT EXISTS messages (\n"+
		"\t\t\tid TEXT PRIMARY KEY,\n"+
		"\t\t\tcontent TEXT NOT NULL,\n"+
		"\t\t\tdata JSON NOT NULL\n"+
		"\t\t);\n"+
		"\t`\n"+
		"}\n")

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "sync.go"), `package cli
func runSync() {
	_ = UpsertMessage("data")
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "search.go"), `package cli
func runSearch() {
	_ = SearchMessage("query")
}
`)

	v, err := NewVerifier(dir, "")
	require.NoError(t, err)

	results := v.PipelineProof()
	require.Len(t, results, 1)

	msg := results[0]
	assert.False(t, msg.GhostTable, "write path exists")
	assert.False(t, msg.OrphanFTS, "no FTS table, so no orphan")
}

func TestAuthProof_DetectsMismatch(t *testing.T) {
	dir := t.TempDir()
	setupVerifierDirs(t, dir)

	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
func authHeader(token string) string {
	return "Bearer " + token
}
`)

	specPath := filepath.Join(dir, "spec.json")
	writeTestFile(t, specPath, `{
  "paths": {},
  "components": {
    "securitySchemes": {
      "BotToken": {
        "type": "http",
        "scheme": "bearer"
      }
    }
  }
}`)

	v, err := NewVerifier(dir, specPath)
	require.NoError(t, err)

	result := v.AuthProof()
	assert.False(t, result.Match, "Bearer in client vs BotToken scheme should mismatch")
	assert.True(t, result.Mismatch)
	assert.Contains(t, result.GeneratedScheme, "Bearer")
}

func TestAuthProofRejectsBearerApplyAuthFormatWithoutTokenPlaceholder(t *testing.T) {
	dir := t.TempDir()
	setupVerifierDirs(t, dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "config"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
func authHeader() string { return configAuthHeader() }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "config", "config.go"), `package config
func (c *Config) AuthHeader() string {
	return applyAuthFormat("Bearer ", map[string]string{"token": c.Token})
}
`)

	specPath := filepath.Join(dir, "spec.json")
	writeTestFile(t, specPath, `{
  "paths": {},
  "components": {
    "securitySchemes": {
      "BearerAuth": {
        "type": "http",
        "scheme": "bearer"
      }
    }
  }
}`)

	v, err := NewVerifier(dir, specPath)
	require.NoError(t, err)

	result := v.AuthProof()
	assert.False(t, result.Match)
	assert.True(t, result.Mismatch)
	assert.Equal(t, "Bearer ", result.GeneratedFormat)
	assert.Contains(t, result.Detail, `format literal "Bearer " does not include a token placeholder`)
}

func TestRunVerification_FullIntegration(t *testing.T) {
	dir := t.TempDir()
	setupVerifierDirs(t, dir)

	// Hallucinated path
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "users_get.go"), `package cli
func usersGet() {
	path = "/users/{id}"
	path = replacePathParam(path, "id", args[0])
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "bogus_get.go"), `package cli
func bogusGet() {
	path = "/bogus/hallucinated"
}
`)

	// Dead flag
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
type rootFlags struct {
	deadFlag bool
}
func initFlags(flags *rootFlags) {
	_ = &flags.deadFlag
}
`)

	// Ghost table
	writeTestFile(t, filepath.Join(dir, "internal", "store", "store.go"), "package store\n"+
		"func schema() string {\n"+
		"\treturn `\n"+
		"\t\tCREATE TABLE IF NOT EXISTS messages (\n"+
		"\t\t\tid TEXT PRIMARY KEY,\n"+
		"\t\t\tchannel_id TEXT NOT NULL,\n"+
		"\t\t\tauthor TEXT NOT NULL,\n"+
		"\t\t\tcontent TEXT NOT NULL,\n"+
		"\t\t\ttimestamp TEXT NOT NULL,\n"+
		"\t\t\tdata JSON NOT NULL\n"+
		"\t\t);\n"+
		"\t`\n"+
		"}\n")

	// No UpsertMessage or SearchMessage calls
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "list.go"), `package cli
func runList() {
	_ = "nothing relevant"
}
`)

	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
func authHeader(token string) string {
	return "Bearer " + token
}
`)

	specPath := filepath.Join(dir, "spec.json")
	writeTestFile(t, specPath, `{
  "paths": {
    "/users/{user_id}": {}
  },
  "components": { "securitySchemes": {} }
}`)

	// We can't run RunVerification because it calls CompileGate (go build).
	// Instead, test the individual proofs and verdict derivation.
	v, err := NewVerifier(dir, specPath)
	require.NoError(t, err)

	paths := v.PathProof()
	flags := v.FlagProof()
	pipeline := v.PipelineProof()
	auth := v.AuthProof()

	report := &VerificationReport{
		Dir:      dir,
		SpecPath: specPath,
		Paths:    paths,
		Flags:    flags,
		Pipeline: pipeline,
		Auth:     auth,
	}

	for _, p := range paths {
		if !p.Valid {
			report.HallucinatedPaths++
		}
	}
	for _, f := range flags {
		if f.References == 0 {
			report.DeadFlags++
		}
	}
	for _, p := range pipeline {
		if !p.HasWrite {
			report.GhostTables++
		}
		if p.HasFTS && !p.HasSearch {
			report.OrphanFTS++
		}
	}
	report.AuthMismatch = auth.Mismatch

	report.Issues = collectVerificationIssues(report)
	report.Verdict = deriveVerificationVerdict(report)

	assert.Equal(t, "FAIL", report.Verdict)
	assert.Equal(t, 1, report.HallucinatedPaths)
	assert.Equal(t, 1, report.DeadFlags)
	assert.Equal(t, 1, report.GhostTables)
	assert.GreaterOrEqual(t, len(report.Issues), 3)
}

func TestRemediate_RemovesDeadFlags(t *testing.T) {
	dir := t.TempDir()
	setupVerifierDirs(t, dir)

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
type rootFlags struct {
	jsonOutput bool
	deadFlag   bool
}
func initFlags(flags *rootFlags) {
	_ = &flags.jsonOutput
	_ = &flags.deadFlag
}
`)

	report := &VerificationReport{
		Flags: []FlagProofResult{
			{Flag: "deadFlag", FlagName: "deadFlag", DeadFlag: true, References: 0},
		},
	}

	// Remediate will try compileGateCheck which will fail (no go.mod), but
	// the dead flag removal itself should happen. We check the file content
	// before the compile gate reverts it by inspecting the removeDeadFlags function directly.
	err := removeDeadFlags(dir, []string{"deadFlag"})
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(dir, "internal", "cli", "root.go"))
	require.NoError(t, err)
	content := string(data)

	assert.NotContains(t, content, "&flags.deadFlag")
	assert.NotContains(t, content, "deadFlag")
	assert.Contains(t, content, "&flags.jsonOutput", "jsonOutput should remain")

	_ = report // used to set up context
}

func TestDeriveVerificationVerdict(t *testing.T) {
	tests := []struct {
		name   string
		report *VerificationReport
		want   string
	}{
		{
			name: "hallucinated paths -> FAIL",
			report: &VerificationReport{
				HallucinatedPaths: 1,
			},
			want: "FAIL",
		},
		{
			name: "auth mismatch -> FAIL",
			report: &VerificationReport{
				AuthMismatch: true,
			},
			want: "FAIL",
		},
		{
			name: "ghost table with 5+ columns -> FAIL",
			report: &VerificationReport{
				Pipeline: []PipelineProofResult{
					{TableName: "messages", HasWrite: false, Columns: 6},
				},
			},
			want: "FAIL",
		},
		{
			name: "dead flags only -> WARN",
			report: &VerificationReport{
				DeadFlags: 2,
			},
			want: "WARN",
		},
		{
			name: "orphan FTS -> WARN",
			report: &VerificationReport{
				OrphanFTS: 1,
			},
			want: "WARN",
		},
		{
			name: "ghost table with <5 columns -> WARN",
			report: &VerificationReport{
				Pipeline: []PipelineProofResult{
					{TableName: "small", HasWrite: false, Columns: 3},
				},
			},
			want: "WARN",
		},
		{
			name:   "all clean -> PASS",
			report: &VerificationReport{},
			want:   "PASS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveVerificationVerdict(tt.report)
			assert.Equal(t, tt.want, got)
		})
	}
}
