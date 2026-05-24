package pipeline

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	apispec "github.com/mvanhorn/cli-printing-press/v4/internal/spec"
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
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "export.go"), `package cli
func exportResource(resource string) {
	path := "/" + resource
	_ = path
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

func TestRunDogfoodOAuthScopeCoveragePassesCoveredEndpoint(t *testing.T) {
	dir, specPath := writeOAuthScopeCoverageFixture(t, `package cli
func authLogin() {
	scopes := []string{"youtube"}
	params.Set("scope", strings.Join(scopes, " "))
}
`, `openapi: 3.0.0
info:
  title: Videos API
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /videos:
    get:
      operationId: listVideos
      security:
        - OAuth2: [youtube]
      responses:
        "200":
          description: ok
  /analytics:
    get:
      operationId: listAnalytics
      responses:
        "200":
          description: ok
components:
  securitySchemes:
    OAuth2:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://example.com/auth
          tokenUrl: https://example.com/token
          scopes:
            youtube: YouTube read access
`)

	report, err := RunDogfood(dir, specPath)
	require.NoError(t, err)

	assert.Equal(t, 1, report.OAuthScopeCoverage.Checked)
	assert.Equal(t, 1, report.OAuthScopeCoverage.Covered)
	assert.Empty(t, report.OAuthScopeCoverage.Violations)
}

func TestRunDogfoodOAuthScopeCoverageFailsUncoveredEndpoint(t *testing.T) {
	dir, specPath := writeOAuthScopeCoverageFixture(t, `package cli
func authLogin() {
	scopes := []string{"youtube"}
	params.Set("scope", strings.Join(scopes, " "))
}
`, `openapi: 3.0.0
info:
  title: Videos API
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /videos:
    get:
      operationId: listVideos
      responses:
        "200":
          description: ok
  /analytics:
    get:
      operationId: listAnalytics
      security:
        - OAuth2: [yt-analytics.readonly]
      responses:
        "200":
          description: ok
components:
  securitySchemes:
    OAuth2:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://example.com/auth
          tokenUrl: https://example.com/token
          scopes:
            youtube: YouTube read access
            yt-analytics.readonly: Analytics read access
`)

	report, err := RunDogfood(dir, specPath)
	require.NoError(t, err)

	assert.Equal(t, "FAIL", report.Verdict)
	assert.Equal(t, 1, report.OAuthScopeCoverage.Checked)
	assert.Equal(t, 0, report.OAuthScopeCoverage.Covered)
	require.Len(t, report.OAuthScopeCoverage.Violations, 1)
	assert.Equal(t, "GET /analytics", report.OAuthScopeCoverage.Violations[0].Endpoint)
	assert.Equal(t, "listAnalytics", report.OAuthScopeCoverage.Violations[0].OperationID)
	assert.Equal(t, []string{"yt-analytics.readonly"}, report.OAuthScopeCoverage.Violations[0].RequiredScopes)
	assert.Contains(t, report.Issues, "OAuth scope coverage missing for 1 endpoint(s)")
}

func TestRunDogfoodOAuthScopeCoverageRequiresAllScopesInAlternative(t *testing.T) {
	specYAML := `openapi: 3.0.0
info:
  title: Videos API
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /analytics:
    get:
      operationId: listAnalytics
      security:
        - OAuth2: [youtube, yt-analytics.readonly]
      responses:
        "200":
          description: ok
components:
  securitySchemes:
    OAuth2:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://example.com/auth
          tokenUrl: https://example.com/token
          scopes:
            youtube: YouTube read access
            yt-analytics.readonly: Analytics read access
`

	t.Run("partial coverage fails", func(t *testing.T) {
		dir, specPath := writeOAuthScopeCoverageFixture(t, `package cli
func authLogin() {
	scopes := []string{"youtube"}
	params.Set("scope", strings.Join(scopes, " "))
}
`, specYAML)

		report, err := RunDogfood(dir, specPath)
		require.NoError(t, err)

		assert.Equal(t, "FAIL", report.Verdict)
		assert.Equal(t, 1, report.OAuthScopeCoverage.Checked)
		assert.Equal(t, 0, report.OAuthScopeCoverage.Covered)
		require.Len(t, report.OAuthScopeCoverage.Violations, 1)
		assert.Equal(t, []string{"youtube", "yt-analytics.readonly"}, report.OAuthScopeCoverage.Violations[0].RequiredScopes)
		assert.Equal(t, [][]string{{"youtube", "yt-analytics.readonly"}}, report.OAuthScopeCoverage.Violations[0].RequiredScopeAlternatives)
	})

	t.Run("full coverage passes", func(t *testing.T) {
		dir, specPath := writeOAuthScopeCoverageFixture(t, `package cli
func authLogin() {
	scopes := []string{"youtube", "yt-analytics.readonly"}
	params.Set("scope", strings.Join(scopes, " "))
}
`, specYAML)

		report, err := RunDogfood(dir, specPath)
		require.NoError(t, err)

		assert.Equal(t, 1, report.OAuthScopeCoverage.Checked)
		assert.Equal(t, 1, report.OAuthScopeCoverage.Covered)
		assert.Empty(t, report.OAuthScopeCoverage.Violations)
	})
}

func TestRunDogfoodOAuthScopeCoverageReadsAppendedGeneratedScope(t *testing.T) {
	dir, specPath := writeOAuthScopeCoverageFixture(t, `package cli
func authLogin() {
	scopes := []string{"youtube"}
	scopes = append(scopes, "offline")
	params.Set("scope", strings.Join(scopes, " "))
}
`, `openapi: 3.0.0
info:
  title: Videos API
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /refresh:
    get:
      operationId: refreshAccess
      security:
        - OAuth2: [offline]
      responses:
        "200":
          description: ok
components:
  securitySchemes:
    OAuth2:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://example.com/auth
          tokenUrl: https://example.com/token
          scopes:
            youtube: YouTube read access
            offline: Refresh access
`)

	report, err := RunDogfood(dir, specPath)
	require.NoError(t, err)

	assert.Equal(t, 1, report.OAuthScopeCoverage.Checked)
	assert.Equal(t, 1, report.OAuthScopeCoverage.Covered)
	assert.Empty(t, report.OAuthScopeCoverage.Violations)
}

func TestRunDogfoodOAuthScopeCoverageIgnoresUnjoinedScopesVariable(t *testing.T) {
	dir, specPath := writeOAuthScopeCoverageFixture(t, `package cli
func helper() {
	scopes := []string{"yt-analytics.readonly"}
	_ = scopes
}
`, `openapi: 3.0.0
info:
  title: Videos API
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /analytics:
    get:
      operationId: listAnalytics
      security:
        - OAuth2: [yt-analytics.readonly]
      responses:
        "200":
          description: ok
components:
  securitySchemes:
    OAuth2:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://example.com/auth
          tokenUrl: https://example.com/token
          scopes:
            yt-analytics.readonly: Analytics read access
`)

	report, err := RunDogfood(dir, specPath)
	require.NoError(t, err)

	assert.Equal(t, "FAIL", report.Verdict)
	assert.Equal(t, 1, report.OAuthScopeCoverage.Checked)
	assert.Equal(t, 0, report.OAuthScopeCoverage.Covered)
	require.Len(t, report.OAuthScopeCoverage.Violations, 1)
	assert.Equal(t, "generated auth.go declares no OAuth scopes", report.OAuthScopeCoverage.Detail)
}

func TestRunDogfoodOAuthScopeCoverageSurvivesUndefinedNonOAuthScheme(t *testing.T) {
	dir, specPath := writeOAuthScopeCoverageFixture(t, `package cli
func authLogin() {
	scopes := []string{"youtube"}
	params.Set("scope", strings.Join(scopes, " "))
}
`, `openapi: 3.0.0
info:
  title: Videos API
  version: "1.0"
servers:
  - url: https://api.example.com
paths:
  /analytics:
    get:
      operationId: listAnalytics
      security:
        - OAuth2: [yt-analytics.readonly]
          MissingAPIKey: []
      responses:
        "200":
          description: ok
components:
  securitySchemes:
    OAuth2:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://example.com/auth
          tokenUrl: https://example.com/token
          scopes:
            youtube: YouTube read access
            yt-analytics.readonly: Analytics read access
`)

	report, err := RunDogfood(dir, specPath)
	require.NoError(t, err)

	assert.Equal(t, "FAIL", report.Verdict)
	assert.Equal(t, 1, report.OAuthScopeCoverage.Checked)
	assert.Equal(t, 0, report.OAuthScopeCoverage.Covered)
	require.Len(t, report.OAuthScopeCoverage.Violations, 1)
	assert.Equal(t, "GET /analytics", report.OAuthScopeCoverage.Violations[0].Endpoint)
	assert.Equal(t, []string{"yt-analytics.readonly"}, report.OAuthScopeCoverage.Violations[0].RequiredScopes)
}

func TestLoadOpenAPISpecCollectsRootOAuthScopeRequirements(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.yaml")
	writeTestFile(t, specPath, `openapi: 3.0.0
info:
  title: Root Scoped API
  version: "1.0"
paths:
  /items:
    get:
      operationId: listItems
      responses:
        "200":
          description: ok
components:
  securitySchemes:
    OAuth2:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://example.com/auth
          tokenUrl: https://example.com/token
          scopes:
            items.read: Read items
security:
  - OAuth2: [items.read]
`)

	info, err := loadOpenAPISpec(specPath)
	require.NoError(t, err)
	require.Len(t, info.OAuthScopeRequirements, 1)
	assert.Equal(t, "GET /items", info.OAuthScopeRequirements[0].Endpoint)
	assert.Equal(t, "listItems", info.OAuthScopeRequirements[0].OperationID)
	assert.Equal(t, []string{"items.read"}, info.OAuthScopeRequirements[0].Alternatives[0].Scopes)
}

func TestLoadOpenAPISpecCombinesOAuthSchemesWithinRequirement(t *testing.T) {
	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.yaml")
	writeTestFile(t, specPath, `openapi: 3.0.0
info:
  title: Combined Scoped API
  version: "1.0"
paths:
  /items:
    get:
      operationId: listItems
      security:
        - OAuthA: [a.read]
          OAuthB: [b.read]
      responses:
        "200":
          description: ok
components:
  securitySchemes:
    OAuthA:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://example.com/auth-a
          tokenUrl: https://example.com/token-a
          scopes:
            a.read: Read A
    OAuthB:
      type: oauth2
      flows:
        authorizationCode:
          authorizationUrl: https://example.com/auth-b
          tokenUrl: https://example.com/token-b
          scopes:
            b.read: Read B
`)

	info, err := loadOpenAPISpec(specPath)
	require.NoError(t, err)
	require.Len(t, info.OAuthScopeRequirements, 1)
	require.Len(t, info.OAuthScopeRequirements[0].Alternatives, 1)
	assert.Equal(t, []string{"a.read", "b.read"}, info.OAuthScopeRequirements[0].Alternatives[0].Scopes)
}

func writeOAuthScopeCoverageFixture(t *testing.T, authGo, specYAML string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "store"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
type rootFlags struct{}
func initFlags(flags *rootFlags) { _ = flags }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "videos_get.go"), `package cli
func videosGet() {
	path := "/videos"
}
func analyticsGet() {
	path := "/analytics"
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "auth.go"), authGo)
	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
func authHeader(token string) string {
	return "Bearer " + token
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "store", "store.go"), "package store\n")

	specPath := filepath.Join(dir, "spec.yaml")
	writeTestFile(t, specPath, specYAML)
	return dir, specPath
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

// TestCheckPipelineIntegritySyncResourcesEmpty asserts that when the generator
// emits sync.go with an empty defaultSyncResources() body (the structural
// signature of an API with no bulk-list endpoint, e.g., Allrecipes), dogfood's
// pipeline check records SyncFileEmitted=true and SyncResourcesPresent=false
// and surfaces the detail string. Issue #1156.
func TestCheckPipelineIntegritySyncResourcesEmpty(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "store"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "sync.go"), `package cli

func runSync(s interface{ UpsertItems() error }) error {
	return s.UpsertItems()
}

func defaultSyncResources() []string {
	return []string{}
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "store", "store.go"), "package store\n")

	result := checkPipelineIntegrity(dir)
	assert.True(t, result.SyncFileEmitted, "sync.go was written")
	assert.False(t, result.SyncResourcesPresent, "defaultSyncResources is empty")
	assert.Contains(t, result.Detail, "defaultSyncResources empty")
}

// TestCheckPipelineIntegritySyncResourcesPopulated covers the normal case where
// the generator emits sync.go with one or more resources. Both new fields
// should be set; the empty-list detail string should not appear.
func TestCheckPipelineIntegritySyncResourcesPopulated(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "store"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "sync.go"), `package cli

func runSync(s interface{ UpsertUsers() error }) error {
	return s.UpsertUsers()
}

func defaultSyncResources() []string {
	return []string{
		"users",
	}
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "store", "store.go"), "package store\n")

	result := checkPipelineIntegrity(dir)
	assert.True(t, result.SyncFileEmitted)
	assert.True(t, result.SyncResourcesPresent)
	assert.NotContains(t, result.Detail, "defaultSyncResources empty")
}

func TestHasPopulatedSyncResources(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "empty source",
			src:  "",
			want: false,
		},
		{
			name: "helper absent (hand-rolled or fixture)",
			src:  "package cli\nfunc runSync() {}\n",
			want: true,
		},
		{
			name: "helper present with empty list",
			src: `package cli
func defaultSyncResources() []string {
	return []string{}
}
`,
			want: false,
		},
		{
			name: "helper present with one entry",
			src: `package cli
func defaultSyncResources() []string {
	return []string{
		"users",
	}
}
`,
			want: true,
		},
		{
			name: "helper present with several entries",
			src: `package cli
func defaultSyncResources() []string {
	return []string{
		"commissions",
		"customers",
		"domains",
	}
}
`,
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, hasPopulatedSyncResources(tc.src))
		})
	}
}

func TestDeriveDogfoodVerdict(t *testing.T) {
	report := &DogfoodReport{
		PathCheck:     PathCheckResult{Tested: 10, Valid: 10, Pct: 100},
		AuthCheck:     AuthCheckResult{Match: true},
		DeadFlags:     DeadCodeResult{Dead: 1},
		DeadFuncs:     DeadCodeResult{Dead: 0},
		PipelineCheck: PipelineResult{SyncCallsDomain: true, SyncResourcesPresent: true},
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

	// Issue #1156: when sync.go is emitted but defaultSyncResources is empty,
	// the sync command is a runtime no-op. Dogfood must flag this as WARN so
	// the gap surfaces at shipcheck time.
	report.PipelineCheck.SyncFileEmitted = true
	report.PipelineCheck.SyncResourcesPresent = false
	assert.Equal(t, "WARN", deriveDogfoodVerdict(report, true))
	report.PipelineCheck.SyncResourcesPresent = true
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
		{
			// Regression: examples whose first line is unindented (the result
			// of the strings.TrimSpace(`...`) idiom, which strips the leading
			// 2-space indent). Prior parser broke at the first unindented
			// line and captured nothing. New parser only breaks on canonical
			// Cobra section headers.
			name: "first example unindented (TrimSpace idiom)",
			help: "Examples:\nfood52-pp-cli articles browse food\n  food52-pp-cli articles browse life --limit 10 --json\n\nFlags:\n  --limit int\n",
			want: "food52-pp-cli articles browse food\n  food52-pp-cli articles browse life --limit 10 --json",
		},
		{
			// Trailing "Use \"...\" for more information..." line is a
			// section boundary too — match on the literal `Use "` prefix.
			name: "use-for-more trailing line",
			help: "Examples:\n  cli do --a 1\n\nUse \"cli [command] --help\" for more information about a command.\n",
			want: "cli do --a 1",
		},
		{
			// Lines that start with a word like "use" but are NOT the
			// Cobra trailing line (lowercase, no quote) are example
			// continuation, not section boundaries.
			name: "example line starting with word use",
			help: "Examples:\n  cli widget create  # use the default options\n  cli widget update\n\nFlags:\n  --opts string\n",
			want: "cli widget create  # use the default options\n  cli widget update",
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

func TestCheckCommandTree(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	// Two constructors wired via AddCommand, one unwired
	writeTestFile(t, filepath.Join(cliDir, "root.go"), `package cli
func newRootCmd() {
	rootCmd.AddCommand(newFooCmd())
	rootCmd.AddCommand(newBarCmd())
}
`)
	writeTestFile(t, filepath.Join(cliDir, "foo.go"), `package cli
func newFooCmd() { cmd := &cobra.Command{Use: "foo"} }
`)
	writeTestFile(t, filepath.Join(cliDir, "bar.go"), `package cli
func newBarCmd() { cmd := &cobra.Command{Use: "bar"} }
`)
	writeTestFile(t, filepath.Join(cliDir, "orphan.go"), `package cli
func newOrphanCmd() { cmd := &cobra.Command{Use: "orphan"} }
`)

	result := checkCommandTree(dir)
	assert.Equal(t, 3, result.Defined) // foo, bar, orphan (root excluded)
	assert.Equal(t, 2, result.Registered)
	assert.Equal(t, []string{"orphan"}, result.Unregistered)
}

// TestCheckCommandTree_BacktickUse pins that the constructor walker reads
// the Use: leaf name from a backtick raw-string literal, which authors reach
// for when the command name contains a literal double-quote. Without
// backtick support, useName silently falls back to the constructor name
// and the command surfaces with the wrong identity in CommandTreeResult.
func TestCheckCommandTree_BacktickUse(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	writeTestFile(t, filepath.Join(cliDir, "root.go"), `package cli
func newRootCmd() {
	rootCmd.AddCommand(newQueryCmd())
}
`)
	writeTestFile(t, filepath.Join(cliDir, "query.go"),
		"package cli\n"+
			"func newQueryCmd() *cobra.Command {\n"+
			"\treturn &cobra.Command{Use: `query <project> \"<sql>\"`}\n"+
			"}\n")

	result := checkCommandTree(dir)
	assert.Equal(t, 1, result.Defined)
	assert.Equal(t, 1, result.Registered)
	assert.Empty(t, result.Unregistered)
}

func TestCheckCommandTree_DeeplyNested(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	// Simulate deeply nested commands like Cal.com's organizations hierarchy.
	// All are wired via AddCommand — static analysis should find them all.
	writeTestFile(t, filepath.Join(cliDir, "root.go"), `package cli
func newRootCmd() {
	rootCmd.AddCommand(newOrganizationsCmd(&flags))
}
`)
	writeTestFile(t, filepath.Join(cliDir, "organizations.go"), `package cli
func newOrganizationsCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "organizations"}
	cmd.AddCommand(newOrgAttributesCmd(flags))
	cmd.AddCommand(newOrgRolesCmd(flags))
	cmd.AddCommand(newOrgOooCmd(flags))
	return cmd
}
`)
	writeTestFile(t, filepath.Join(cliDir, "org_attributes.go"), `package cli
func newOrgAttributesCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "attributes"}
	return cmd
}
`)
	writeTestFile(t, filepath.Join(cliDir, "org_roles.go"), `package cli
func newOrgRolesCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "roles"}
	return cmd
}
`)
	writeTestFile(t, filepath.Join(cliDir, "org_ooo.go"), `package cli
func newOrgOooCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "ooo"}
	return cmd
}
`)

	result := checkCommandTree(dir)
	// 4 non-root constructors, all wired
	assert.Equal(t, 4, result.Defined)
	assert.Equal(t, 4, result.Registered)
	assert.Empty(t, result.Unregistered)
}

func TestCheckCommandTree_IndirectWiring(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliDir, 0o755))

	// Test indirect wiring: sub := newXxxCmd(flags); cmd.AddCommand(sub)
	// This pattern is used by command_promoted.go.tmpl for multi-endpoint subresources.
	writeTestFile(t, filepath.Join(cliDir, "root.go"), `package cli
func newRootCmd() {
	rootCmd.AddCommand(newParentCmd(&flags))
}
`)
	writeTestFile(t, filepath.Join(cliDir, "parent.go"), `package cli
func newParentCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "parent"}
	{
		sub := newChildCmd(flags)
		sub.Hidden = false
		cmd.AddCommand(sub)
	}
	return cmd
}
`)
	writeTestFile(t, filepath.Join(cliDir, "child.go"), `package cli
func newChildCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "child"}
	return cmd
}
`)

	result := checkCommandTree(dir)
	assert.Equal(t, 2, result.Defined) // parent + child (root excluded)
	assert.Equal(t, 2, result.Registered)
	assert.Empty(t, result.Unregistered)
}

func TestCheckConfigConsistency(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))

	// Write site uses "AccessToken"
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "auth.go"), `package cli
func saveAuth() {
	config.Set("AccessToken", token)
}
`)
	// Read site uses "DominosToken" - a mismatch
	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
func getAuth() string {
	return config.Get("DominosToken")
}
`)

	result := checkConfigConsistency(dir)
	assert.False(t, result.Consistent)
	assert.Contains(t, result.WriteFields, "AccessToken")
	assert.Contains(t, result.ReadFields, "DominosToken")
	assert.NotEmpty(t, result.Mismatched)
}

func TestCheckConfigConsistency_Consistent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))

	// Both write and read use the same field
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "auth.go"), `package cli
func saveAuth() {
	config.Set("AccessToken", token)
}
func getAuth() string {
	return config.Get("AccessToken")
}
`)

	result := checkConfigConsistency(dir)
	assert.True(t, result.Consistent)
}

func TestCheckWorkflowCompleteness_NoManifest(t *testing.T) {
	dir := t.TempDir()
	result := checkWorkflowCompleteness(dir)
	assert.True(t, result.Skipped)
	assert.Contains(t, result.Detail, "no workflow_verify.yaml found")
}

func TestCheckWorkflowCompleteness_HappyPath(t *testing.T) {
	dir := t.TempDir()

	// Create a manifest with commands that "exist"
	// Since there's no cmd/ dir, the check will treat all as mapped (can't build binary)
	writeTestFile(t, filepath.Join(dir, "workflow_verify.yaml"), `workflows:
  - name: order flow
    steps:
      - command: auth login
        name: login
      - command: menu list
        name: browse menu
`)

	result := checkWorkflowCompleteness(dir)
	assert.False(t, result.Skipped)
	assert.Equal(t, 2, result.TotalSteps)
	// No cmd/ dir means no binary, so all steps treated as mapped
	assert.Equal(t, 2, result.MappedSteps)
	assert.Empty(t, result.UnmappedSteps)
}

func TestCheckWorkflowCompleteness_MissingCommand(t *testing.T) {
	// This test verifies parsing works correctly for a manifest with steps.
	// Without a buildable binary, the check can't actually verify commands,
	// so we test that the YAML parsing and step counting work.
	dir := t.TempDir()

	writeTestFile(t, filepath.Join(dir, "workflow_verify.yaml"), `workflows:
  - name: order flow
    steps:
      - command: cart checkout
        name: checkout
      - command: auth login
        name: login
`)

	result := checkWorkflowCompleteness(dir)
	assert.False(t, result.Skipped)
	assert.Equal(t, 2, result.TotalSteps)
}

func TestWiringCheckIntegration(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "store"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
type rootFlags struct{}
func initFlags(flags *rootFlags) { _ = flags }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
func authHeader(token string) string {
	return "Bearer " + token
}
`)
	writeTestFile(t, filepath.Join(dir, "internal", "store", "store.go"), "package store\n")

	report, err := RunDogfood(dir, "")
	require.NoError(t, err)

	// WiringCheck should be populated in the report
	assert.True(t, report.WiringCheck.ConfigConsist.Consistent)
	assert.True(t, report.WiringCheck.WorkflowComplete.Skipped)
	assert.Equal(t, 0, report.WiringCheck.CommandTree.Defined)
}

func TestCheckAuthRecognizesBasicPrefix(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "config"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
func authHeader() string { return configAuthHeader() }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "config", "config.go"), `package config
func (c *Config) AuthHeader() string {
	return "Basic " + encode(c.Username+":"+c.Password)
}
`)

	result := checkAuth(dir, apispec.AuthConfig{Type: "api_key", Format: "Basic {username}:{password}"})
	assert.True(t, result.Match)
	assert.Equal(t, "Basic ", result.GeneratedFmt)
}

func TestCheckAuthRecognizesBearerApplyAuthFormatWithTokenPlaceholder(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "config"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
func authHeader() string { return configAuthHeader() }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "config", "config.go"), `package config
func (c *Config) AuthHeader() string {
	return applyAuthFormat("Bearer {token}", map[string]string{"token": c.Token})
}
`)

	result := checkAuth(dir, apispec.AuthConfig{Type: "bearer_token", Format: "Bearer {token}"})
	assert.True(t, result.Match)
	assert.Equal(t, "Bearer ", result.GeneratedFmt)
}

func TestCheckAuthRejectsBearerApplyAuthFormatWithoutTokenPlaceholder(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "config"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
func authHeader() string { return configAuthHeader() }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "config", "config.go"), `package config
func (c *Config) AuthHeader() string {
	return applyAuthFormat("Bearer ", map[string]string{"token": c.Token})
}
`)

	result := checkAuth(dir, apispec.AuthConfig{Type: "bearer_token", Format: "Bearer "})
	assert.False(t, result.Match)
	assert.Equal(t, "Bearer ", result.GeneratedFmt)
	assert.Contains(t, result.Detail, `format literal "Bearer " does not include a token placeholder`)
}

func TestCheckAuthRejectsBearerApplyAuthFormatWithMissingPlaceholderReplacement(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "config"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
func authHeader() string { return configAuthHeader() }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "config", "config.go"), `package config
func (c *Config) AuthHeader() string {
	return applyAuthFormat("Bearer {access_token}", map[string]string{"token": c.Token})
}
`)

	result := checkAuth(dir, apispec.AuthConfig{Type: "bearer_token", Format: "Bearer {access_token}"})
	assert.False(t, result.Match)
	assert.Equal(t, "Bearer ", result.GeneratedFmt)
	assert.Contains(t, result.Detail, `includes placeholder "access_token" but generated replacements do not provide it`)
}

func TestCheckAuthPrefersTokenPreservingApplyAuthFormat(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "config"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
func authHeader() string { return configAuthHeader() }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "config", "config.go"), `package config
func previewAuthHeader(c *Config) string {
	return applyAuthFormat("Bearer ", map[string]string{"token": c.Token})
}
func (c *Config) AuthHeader() string {
	return applyAuthFormat("Bearer {token}", map[string]string{"token": c.Token})
}
`)

	result := checkAuth(dir, apispec.AuthConfig{Type: "bearer_token", Format: "Bearer {token}"})
	assert.True(t, result.Match)
	assert.Equal(t, "Bearer ", result.GeneratedFmt)
}

func TestDeriveDogfoodVerdict_WiringChecks(t *testing.T) {
	// Test that unregistered commands cause FAIL
	report := &DogfoodReport{
		PathCheck:     PathCheckResult{Tested: 10, Valid: 10, Pct: 100},
		AuthCheck:     AuthCheckResult{Match: true},
		DeadFlags:     DeadCodeResult{Dead: 0},
		DeadFuncs:     DeadCodeResult{Dead: 0},
		PipelineCheck: PipelineResult{SyncCallsDomain: true},
		WiringCheck: WiringCheckResult{
			CommandTree:      CommandTreeResult{Defined: 2, Registered: 1, Unregistered: []string{"bar"}},
			ConfigConsist:    ConfigConsistResult{Consistent: true},
			WorkflowComplete: WorkflowCompleteResult{Skipped: true},
		},
	}
	assert.Equal(t, "FAIL", deriveDogfoodVerdict(report, true))

	// Test that config inconsistency causes FAIL
	report.WiringCheck.CommandTree.Unregistered = nil
	report.WiringCheck.ConfigConsist.Consistent = false
	report.WiringCheck.ConfigConsist.Mismatched = []string{"AccessToken", "DominosToken"}
	assert.Equal(t, "FAIL", deriveDogfoodVerdict(report, true))

	// Test that unmapped workflow steps cause WARN
	report.WiringCheck.ConfigConsist.Consistent = true
	report.WiringCheck.WorkflowComplete = WorkflowCompleteResult{
		TotalSteps:    2,
		MappedSteps:   1,
		UnmappedSteps: []string{"cart checkout"},
	}
	assert.Equal(t, "WARN", deriveDogfoodVerdict(report, true))

	// Test that clean wiring passes
	report.WiringCheck.WorkflowComplete = WorkflowCompleteResult{Skipped: true}
	assert.Equal(t, "PASS", deriveDogfoodVerdict(report, true))
}

func TestDeriveDogfoodVerdict_PreservesPriority(t *testing.T) {
	report := passingDogfoodReport()
	report.AuthCheck.Match = true
	report.DeadFlags.Dead = 1
	report.ExampleCheck = ExampleCheckResult{Tested: 10, WithExamples: 4}
	assert.Equal(t, "FAIL", deriveDogfoodVerdict(report, true))

	report = passingDogfoodReport()
	report.AuthCheck.Match = true
	report.DeadFuncs.Dead = 1
	report.WiringCheck.CommandTree.Unregistered = []string{"orphaned"}
	assert.Equal(t, "WARN", deriveDogfoodVerdict(report, true))
}

func TestCheckNovelFeatures(t *testing.T) {
	t.Run("skipped when no research dir", func(t *testing.T) {
		result := checkNovelFeatures(t.TempDir(), "")
		assert.True(t, result.Skipped)
	})

	t.Run("skipped when no novel features in research", func(t *testing.T) {
		researchDir := t.TempDir()
		research := &ResearchResult{APIName: "test", NoveltyScore: 5}
		require.NoError(t, writeResearchJSON(research, researchDir))
		result := checkNovelFeatures(t.TempDir(), researchDir)
		assert.True(t, result.Skipped)
	})

	t.Run("finds matching commands", func(t *testing.T) {
		// Set up a CLI dir with a command file
		cliDir := t.TempDir()
		cliCodeDir := filepath.Join(cliDir, "internal", "cli")
		require.NoError(t, os.MkdirAll(cliCodeDir, 0o755))
		writeTestFile(t, filepath.Join(cliCodeDir, "health.go"),
			`package cli
func newHealthCmd() *cobra.Command {
	return &cobra.Command{Use: "health"}
}`)
		writeTestFile(t, filepath.Join(cliCodeDir, "triage.go"),
			`package cli
func newTriageCmd() *cobra.Command {
	return &cobra.Command{Use: "triage"}
}`)

		// Set up research with novel features
		researchDir := t.TempDir()
		research := &ResearchResult{
			APIName: "test",
			NovelFeatures: []NovelFeature{
				{Name: "Health dashboard", Command: "health"},
				{Name: "Stale triage", Command: "triage"},
			},
		}
		require.NoError(t, writeResearchJSON(research, researchDir))

		result := checkNovelFeatures(cliDir, researchDir)
		assert.False(t, result.Skipped)
		assert.Equal(t, 2, result.Planned)
		assert.Equal(t, 2, result.Found)
		assert.Empty(t, result.Missing)

		// Verify novel_features_built was written back
		updated, err := LoadResearch(researchDir)
		require.NoError(t, err)
		assert.Len(t, updated.NovelFeatures, 2, "planned list preserved")
		require.NotNil(t, updated.NovelFeaturesBuilt)
		assert.Len(t, *updated.NovelFeaturesBuilt, 2, "all built")
	})

	t.Run("syncs built features into CLI manifest", func(t *testing.T) {
		cliDir := t.TempDir()
		cliCodeDir := filepath.Join(cliDir, "internal", "cli")
		require.NoError(t, os.MkdirAll(cliCodeDir, 0o755))
		writeTestFile(t, filepath.Join(cliCodeDir, "health.go"),
			`package cli
func newHealthCmd() *cobra.Command {
	return &cobra.Command{Use: "health"}
}`)
		writeTestFile(t, filepath.Join(cliCodeDir, "stale.go"),
			`package cli
func newStaleCmd() *cobra.Command {
	return &cobra.Command{Use: "stale"}
}`)
		writeTestFile(t, filepath.Join(cliCodeDir, "bottleneck.go"),
			`package cli
func newBottleneckCmd() *cobra.Command {
	return &cobra.Command{Use: "bottleneck"}
}`)
		require.NoError(t, WriteCLIManifest(cliDir, CLIManifest{
			SchemaVersion: 1,
			APIName:       "test",
			CLIName:       "test-pp-cli",
		}))

		researchDir := t.TempDir()
		research := &ResearchResult{
			APIName: "test",
			NovelFeatures: []NovelFeature{
				{Name: "Health dashboard", Command: "health", Description: "Summarize account health.", Rationale: "Internal only."},
				{Name: "Stale triage", Command: "stale", Description: "Find stale records.", Rationale: "Internal only."},
				{Name: "Bottleneck finder", Command: "bottleneck", Description: "Locate workflow bottlenecks.", Rationale: "Internal only."},
			},
		}
		require.NoError(t, writeResearchJSON(research, researchDir))

		result := checkNovelFeatures(cliDir, researchDir)
		assert.Equal(t, 3, result.Found)

		manifest := readPublishedManifest(t, cliDir)
		require.Len(t, manifest.NovelFeatures, 3)
		assert.Equal(t, NovelFeatureManifest{
			Name:        "Health dashboard",
			Command:     "health",
			Description: "Summarize account health.",
		}, manifest.NovelFeatures[0])
		assert.Equal(t, "stale", manifest.NovelFeatures[1].Command)
		assert.Equal(t, "bottleneck", manifest.NovelFeatures[2].Command)
	})

	t.Run("detects missing commands and writes verified subset", func(t *testing.T) {
		cliDir := t.TempDir()
		cliCodeDir := filepath.Join(cliDir, "internal", "cli")
		require.NoError(t, os.MkdirAll(cliCodeDir, 0o755))
		writeTestFile(t, filepath.Join(cliCodeDir, "health.go"),
			`package cli
func newHealthCmd() *cobra.Command {
	return &cobra.Command{Use: "health"}
}`)

		researchDir := t.TempDir()
		research := &ResearchResult{
			APIName: "test",
			NovelFeatures: []NovelFeature{
				{Name: "Health dashboard", Command: "health"},
				{Name: "Stale triage", Command: "triage"},
				{Name: "Team util", Command: "team utilization"},
			},
		}
		require.NoError(t, writeResearchJSON(research, researchDir))

		result := checkNovelFeatures(cliDir, researchDir)
		assert.Equal(t, 3, result.Planned)
		assert.Equal(t, 1, result.Found)
		assert.Equal(t, []string{"triage", "team utilization"}, result.Missing)

		// Verify novel_features_built contains only the survivor
		updated, err := LoadResearch(researchDir)
		require.NoError(t, err)
		assert.Len(t, updated.NovelFeatures, 3, "planned list preserved")
		require.NotNil(t, updated.NovelFeaturesBuilt)
		require.Len(t, *updated.NovelFeaturesBuilt, 1, "only health survived")
		assert.Equal(t, "health", (*updated.NovelFeaturesBuilt)[0].Command)
	})

	t.Run("warns when advertised command depth differs from registered path", func(t *testing.T) {
		cliDir := t.TempDir()
		cliCodeDir := filepath.Join(cliDir, "internal", "cli")
		require.NoError(t, os.MkdirAll(cliCodeDir, 0o755))
		writeTestFile(t, filepath.Join(cliCodeDir, "root.go"), `package cli
func Execute() {
	rootCmd.AddCommand(newAssetsCmd())
}`)
		writeTestFile(t, filepath.Join(cliCodeDir, "assets.go"), `package cli
func newAssetsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "assets"}
	cmd.AddCommand(newAssetsGrabCmd())
	return cmd
}`)
		writeTestFile(t, filepath.Join(cliCodeDir, "assets_grab.go"), `package cli
func newAssetsGrabCmd() *cobra.Command {
	return &cobra.Command{Use: "grab"}
}`)

		researchDir := t.TempDir()
		research := &ResearchResult{
			APIName: "test",
			NovelFeatures: []NovelFeature{
				{Name: "Grab asset", Command: "grab", Example: `test-pp-cli grab "sunset"`},
			},
		}
		require.NoError(t, writeResearchJSON(research, researchDir))

		result := checkNovelFeatures(cliDir, researchDir)
		assert.Equal(t, 1, result.Planned)
		assert.Equal(t, 1, result.Found)
		assert.Empty(t, result.Missing)
		require.Len(t, result.DepthMismatches, 1)
		assert.Equal(t, NovelFeatureDepthMismatch{
			Command:    "grab",
			Advertised: "grab",
			Actual:     "assets grab",
		}, result.DepthMismatches[0])
	})

	t.Run("does not warn when variable wiring resolves the advertised root command", func(t *testing.T) {
		cliDir := t.TempDir()
		cliCodeDir := filepath.Join(cliDir, "internal", "cli")
		require.NoError(t, os.MkdirAll(cliCodeDir, 0o755))
		writeTestFile(t, filepath.Join(cliCodeDir, "root.go"), `package cli
func Execute() {
	grabCmd := rootGrabSubcmd(nil)
	rootCmd.AddCommand(grabCmd)
	rootCmd.AddCommand(newAssetsCmd())
}
func rootGrabSubcmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "grab"}
}`)
		writeTestFile(t, filepath.Join(cliCodeDir, "assets.go"), `package cli
func newAssetsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "assets"}
	cmd.AddCommand(assetsGrabSubcmd(nil))
	return cmd
}
func assetsGrabSubcmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "grab"}
}`)

		researchDir := t.TempDir()
		research := &ResearchResult{
			APIName: "test",
			NovelFeatures: []NovelFeature{
				{Name: "Grab asset", Command: "grab", Example: `test-pp-cli grab "sunset"`},
			},
		}
		require.NoError(t, writeResearchJSON(research, researchDir))

		result := checkNovelFeatures(cliDir, researchDir)
		assert.Equal(t, 1, result.Found)
		assert.Empty(t, result.Missing)
		assert.Empty(t, result.DepthMismatches)
	})

	t.Run("syncs README and SKILL to verified subset", func(t *testing.T) {
		cliDir := t.TempDir()
		cliCodeDir := filepath.Join(cliDir, "internal", "cli")
		require.NoError(t, os.MkdirAll(cliCodeDir, 0o755))
		writeTestFile(t, filepath.Join(cliCodeDir, "health.go"),
			`package cli
func newHealthCmd() *cobra.Command {
	return &cobra.Command{Use: "health"}
}`)
		writeTestFile(t, filepath.Join(cliCodeDir, "root.go"), strings.Join([]string{
			"package cli",
			"",
			"func newRootCmd() *cobra.Command {",
			"\trootCmd := &cobra.Command{",
			"\t\tUse: \"test-pp-cli\",",
			"\t\tLong: `Test CLI",
			"",
			"Highlights (not in the official API docs):",
			"  • health   planned health",
			"  • triage   planned triage",
			"",
			"Agent mode: add --agent to any command for JSON output + non-interactive mode.",
			"Health check: run 'test-pp-cli doctor' to verify auth and connectivity.",
			"See README.md or the bundled SKILL.md for recipes.`,",
			"\t}",
			"\trootCmd.AddCommand(newHealthCmd())",
			"\treturn rootCmd",
			"}",
			"",
		}, "\n"))
		writeTestFile(t, filepath.Join(cliDir, "README.md"), strings.Join([]string{
			"# Test CLI",
			"",
			"## Quick Start",
			"",
			"Run it.",
			"",
			"## Unique Features",
			"",
			"These capabilities aren't available in any other tool for this API.",
			"- **`health`** \u2014 planned health",
			"- **`triage`** \u2014 planned triage",
			"",
			"## Usage",
			"",
			"Run help.",
			"",
		}, "\n"))
		writeTestFile(t, filepath.Join(cliDir, "SKILL.md"), strings.Join([]string{
			"# Test Skill",
			"",
			"## When Not to Use This CLI",
			"",
			"No writes.",
			"",
			"## Unique Capabilities",
			"",
			"These capabilities aren't available in any other tool for this API.",
			"- **`health`** \u2014 planned health",
			"- **`triage`** \u2014 planned triage",
			"",
			"## Command Reference",
			"",
			"**items** \u2014 Items",
			"",
		}, "\n"))

		researchDir := t.TempDir()
		research := &ResearchResult{
			APIName: "test",
			NovelFeatures: []NovelFeature{
				{
					Name:         "Health dashboard",
					Command:      "health",
					Description:  "See scheduling health metrics at a glance",
					Example:      "test-pp-cli health --agent",
					WhyItMatters: "Agents can inspect health in one command",
					Group:        "Local state",
				},
				{Name: "Stale triage", Command: "triage", Description: "Find stale work", Group: "Local state"},
			},
		}
		require.NoError(t, writeResearchJSON(research, researchDir))

		var stderr string
		result := captureStderr(t, &stderr, func() NovelFeaturesCheckResult {
			return checkNovelFeatures(cliDir, researchDir)
		})
		assert.Equal(t, 1, result.Found)
		assert.Equal(t, []string{"triage"}, result.Missing)

		readmeData, err := os.ReadFile(filepath.Join(cliDir, "README.md"))
		require.NoError(t, err)
		readme := string(readmeData)
		assert.Contains(t, readme, "## Unique Features")
		assert.Contains(t, readme, "### Local state")
		assert.Contains(t, readme, "**`health`**")
		assert.Contains(t, readme, "See scheduling health metrics at a glance")
		assert.Contains(t, readme, "_Agents can inspect health in one command_")
		assert.Contains(t, readme, "test-pp-cli health --agent")
		assert.NotContains(t, readme, "triage")
		assert.Less(t, strings.Index(readme, "## Unique Features"), strings.Index(readme, "## Usage"))

		skillData, err := os.ReadFile(filepath.Join(cliDir, "SKILL.md"))
		require.NoError(t, err)
		skill := string(skillData)
		assert.Contains(t, skill, "## Unique Capabilities")
		assert.Contains(t, skill, "### Local state")
		assert.Contains(t, skill, "**`health`**")
		assert.NotContains(t, skill, "triage")
		assert.Less(t, strings.Index(skill, "## Unique Capabilities"), strings.Index(skill, "## Command Reference"))

		rootData, err := os.ReadFile(filepath.Join(cliCodeDir, "root.go"))
		require.NoError(t, err)
		root := string(rootData)
		assert.Contains(t, root, "Highlights (not in the official API docs):")
		assert.Contains(t, root, "health   See scheduling health metrics at a glance")
		assert.NotContains(t, root, "planned health")
		assert.NotContains(t, root, "triage")
		assert.Contains(t, stderr, "dogfood: synced internal/cli/root.go (Highlights) from novel_features_built")
	})

	t.Run("syncs narrative README and SKILL blocks from research", func(t *testing.T) {
		cliDir := t.TempDir()
		cliCodeDir := filepath.Join(cliDir, "internal", "cli")
		require.NoError(t, os.MkdirAll(cliCodeDir, 0o755))
		writeTestFile(t, filepath.Join(cliCodeDir, "health.go"),
			`package cli
func newHealthCmd() *cobra.Command {
	return &cobra.Command{Use: "health"}
}`)
		writeTestFile(t, filepath.Join(cliDir, "README.md"), strings.Join([]string{
			"# Test CLI",
			"",
			"## Authentication",
			"",
			"Use the old auth command.",
			"",
			"## Quick Start",
			"",
			"```bash",
			"test-pp-cli old quickstart",
			"```",
			"",
			"## Unique Features",
			"",
			"These capabilities aren't available in any other tool for this API.",
			"- **`health`** \u2014 planned health",
			"",
			"## Usage",
			"",
			"Run help.",
			"",
			"## Troubleshooting",
			"",
			"**Not found errors (exit code 3)**",
			"- Check the resource ID is correct",
			"",
			"### API-specific",
			"- **Old symptom** \u2014 Old fix",
			"",
			"## HTTP Transport",
			"",
			"Standard transport.",
			"",
		}, "\n"))
		writeTestFile(t, filepath.Join(cliDir, "SKILL.md"), strings.Join([]string{
			"# Test Skill",
			"",
			"## Unique Capabilities",
			"",
			"These capabilities aren't available in any other tool for this API.",
			"- **`health`** \u2014 planned health",
			"",
			"## Recipes",
			"",
			"### Old recipe",
			"",
			"```bash",
			"test-pp-cli old recipe",
			"```",
			"",
			"## Auth Setup",
			"",
			"Use the old auth command.",
			"",
			"Run `test-pp-cli doctor` to verify setup.",
			"",
			"## Agent Mode",
			"",
			"Use --agent.",
			"",
		}, "\n"))

		researchDir := t.TempDir()
		research := &ResearchResult{
			APIName: "test",
			NovelFeatures: []NovelFeature{
				{Name: "Health dashboard", Command: "health", Description: "See scheduling health metrics at a glance"},
			},
			Narrative: &ReadmeNarrative{
				AuthNarrative: "Use `test-pp-cli oauth-token --grant-type client_credentials` before protected calls.",
				QuickStart: []QuickStartStep{
					{Comment: "Check credentials", Command: "test-pp-cli doctor"},
					{Comment: "Inspect health", Command: "test-pp-cli health --agent"},
				},
				Troubleshoots: []TroubleshootTip{
					{Symptom: "OAuth scope rejected", Fix: "Run `test-pp-cli oauth-token --grant-type client_credentials --scope view_collection`.\n```text\n### not a section\n```"},
				},
				Recipes: []Recipe{
					{Title: "Inspect health", Command: "test-pp-cli health --agent", Explanation: "Returns the verified health summary."},
				},
			},
		}
		require.NoError(t, writeResearchJSON(research, researchDir))

		var stderr string
		result := captureStderr(t, &stderr, func() NovelFeaturesCheckResult {
			return checkNovelFeatures(cliDir, researchDir)
		})
		assert.Equal(t, 1, result.Found)

		readmeData, err := os.ReadFile(filepath.Join(cliDir, "README.md"))
		require.NoError(t, err)
		readme := string(readmeData)
		assert.Contains(t, readme, "## Authentication\n\nUse `test-pp-cli oauth-token --grant-type client_credentials` before protected calls.")
		assert.Contains(t, readme, "# Check credentials\ntest-pp-cli doctor")
		assert.Contains(t, readme, "# Inspect health\ntest-pp-cli health --agent")
		assert.Contains(t, readme, "- **OAuth scope rejected** \u2014 Run `test-pp-cli oauth-token --grant-type client_credentials --scope view_collection`.\n```text\n### not a section\n```")
		assert.NotContains(t, readme, "old quickstart")
		assert.NotContains(t, readme, "Old symptom")
		requireBefore(t, readme, "## Quick Start", "## Unique Features")
		requireBefore(t, readme, "### API-specific", "## HTTP Transport")

		skillData, err := os.ReadFile(filepath.Join(cliDir, "SKILL.md"))
		require.NoError(t, err)
		skill := string(skillData)
		assert.Contains(t, skill, "## Recipes")
		assert.Contains(t, skill, "### Inspect health")
		assert.Contains(t, skill, "test-pp-cli health --agent")
		assert.Contains(t, skill, "Returns the verified health summary.")
		assert.Contains(t, skill, "## Auth Setup\n\nUse `test-pp-cli oauth-token --grant-type client_credentials` before protected calls.")
		assert.Contains(t, skill, "Run `test-pp-cli doctor` to verify setup.")
		assert.NotContains(t, skill, "Old recipe")
		requireBefore(t, skill, "## Recipes", "## Auth Setup")

		assert.Contains(t, stderr, "dogfood: synced README.md (Quick Start) from research.json narrative")
		assert.Contains(t, stderr, "dogfood: synced README.md (Authentication) from research.json narrative")
		assert.Contains(t, stderr, "dogfood: synced README.md (Troubleshooting) from research.json narrative")
		assert.Contains(t, stderr, "dogfood: synced SKILL.md (Recipes) from research.json narrative")
		assert.Contains(t, stderr, "dogfood: synced SKILL.md (Auth Setup) from research.json narrative")

		beforeReadme := readme
		beforeSkill := skill
		result = checkNovelFeatures(cliDir, researchDir)
		assert.Equal(t, 1, result.Found)
		afterReadme, err := os.ReadFile(filepath.Join(cliDir, "README.md"))
		require.NoError(t, err)
		afterSkill, err := os.ReadFile(filepath.Join(cliDir, "SKILL.md"))
		require.NoError(t, err)
		assert.Equal(t, beforeReadme, string(afterReadme), "unchanged narrative should not rewrite README content")
		assert.Equal(t, beforeSkill, string(afterSkill), "unchanged narrative should not rewrite SKILL content")
	})

	t.Run("inserts README and SKILL sections when absent", func(t *testing.T) {
		cliDir := t.TempDir()
		cliCodeDir := filepath.Join(cliDir, "internal", "cli")
		require.NoError(t, os.MkdirAll(cliCodeDir, 0o755))
		writeTestFile(t, filepath.Join(cliCodeDir, "health.go"),
			`package cli
func newHealthCmd() *cobra.Command {
	return &cobra.Command{Use: "health"}
}`)
		writeTestFile(t, filepath.Join(cliDir, "README.md"), strings.Join([]string{
			"# Test CLI",
			"",
			"## Quick Start",
			"",
			"Run it.",
			"",
			"## Usage",
			"",
			"Run help.",
			"",
		}, "\n"))
		writeTestFile(t, filepath.Join(cliDir, "SKILL.md"), strings.Join([]string{
			"# Test Skill",
			"",
			"## Command Reference",
			"",
			"**items** \u2014 Items",
			"",
		}, "\n"))

		researchDir := t.TempDir()
		research := &ResearchResult{
			APIName: "test",
			NovelFeatures: []NovelFeature{
				{Name: "Health dashboard", Command: "health", Description: "See scheduling health metrics at a glance"},
			},
		}
		require.NoError(t, writeResearchJSON(research, researchDir))

		result := checkNovelFeatures(cliDir, researchDir)
		assert.Equal(t, 1, result.Found)

		readmeData, err := os.ReadFile(filepath.Join(cliDir, "README.md"))
		require.NoError(t, err)
		readme := string(readmeData)
		assert.Contains(t, readme, "## Unique Features")
		assert.Contains(t, readme, "**`health`**")
		assert.Less(t, strings.Index(readme, "## Unique Features"), strings.Index(readme, "## Usage"))

		skillData, err := os.ReadFile(filepath.Join(cliDir, "SKILL.md"))
		require.NoError(t, err)
		skill := string(skillData)
		assert.Contains(t, skill, "## Unique Capabilities")
		assert.Contains(t, skill, "**`health`**")
		assert.Less(t, strings.Index(skill, "## Unique Capabilities"), strings.Index(skill, "## Command Reference"))
	})

	t.Run("does not blank manifest when no features survive", func(t *testing.T) {
		cliDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "cli"), 0o755))
		existing := []NovelFeatureManifest{{
			Name:        "Existing feature",
			Command:     "existing",
			Description: "Already verified.",
		}}
		require.NoError(t, WriteCLIManifest(cliDir, CLIManifest{
			SchemaVersion: 1,
			APIName:       "test",
			CLIName:       "test-pp-cli",
			NovelFeatures: existing,
		}))

		researchDir := t.TempDir()
		research := &ResearchResult{
			APIName: "test",
			NovelFeatures: []NovelFeature{
				{Name: "Missing feature", Command: "missing", Description: "Not built."},
			},
		}
		require.NoError(t, writeResearchJSON(research, researchDir))

		result := checkNovelFeatures(cliDir, researchDir)
		assert.Equal(t, 0, result.Found)
		assert.Len(t, result.Missing, 1)

		manifest := readPublishedManifest(t, cliDir)
		assert.Equal(t, existing, manifest.NovelFeatures)
	})
}

// TestCheckNovelFeatures_BacktickUse pins that the walker matches commands
// declared with Go's backtick raw-string Use: form. Authors reach for
// backticks when the command name contains a literal double-quote (e.g.,
// `query <project> "<sql>"`), and the walker must not silently report
// those as missing.
func TestCheckNovelFeatures_BacktickUse(t *testing.T) {
	cliDir := t.TempDir()
	cliCodeDir := filepath.Join(cliDir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliCodeDir, 0o755))
	writeTestFile(t, filepath.Join(cliCodeDir, "query.go"),
		"package cli\n"+
			"func newQueryCmd() *cobra.Command {\n"+
			"\treturn &cobra.Command{Use: `query <project> \"<sql>\"`}\n"+
			"}\n")

	researchDir := t.TempDir()
	research := &ResearchResult{
		APIName: "test",
		NovelFeatures: []NovelFeature{
			{Name: "SQL query", Command: "query"},
		},
	}
	require.NoError(t, writeResearchJSON(research, researchDir))

	result := checkNovelFeatures(cliDir, researchDir)
	assert.Equal(t, 1, result.Planned)
	assert.Equal(t, 1, result.Found)
	assert.Empty(t, result.Missing)
}

func TestCheckNovelFeatures_ZeroSurvivors(t *testing.T) {
	// All planned features missing — novel_features_built should be a non-nil
	// empty slice (not omitted), so the fallback to the aspirational list
	// does NOT kick in.
	cliDir := t.TempDir()
	cliCodeDir := filepath.Join(cliDir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliCodeDir, 0o755))
	// No command files — nothing registered
	writeTestFile(t, filepath.Join(cliCodeDir, "root.go"), strings.Join([]string{
		"package cli",
		"",
		"func newRootCmd() *cobra.Command {",
		"\treturn &cobra.Command{",
		"\t\tUse: \"test-pp-cli\",",
		"\t\tLong: `Test CLI",
		"",
		"Highlights (not in the official API docs):",
		"  • health   planned health",
		"",
		"Agent mode: add --agent to any command for JSON output + non-interactive mode.",
		"Health check: run 'test-pp-cli doctor' to verify auth and connectivity.",
		"See README.md or the bundled SKILL.md for recipes.`,",
		"\t}",
		"}",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(cliDir, "README.md"), strings.Join([]string{
		"# Test CLI",
		"",
		"## Quick Start",
		"",
		"Run it.",
		"",
		"## Unique Features",
		"",
		"These capabilities aren't available in any other tool for this API.",
		"- **`health`** \u2014 planned health",
		"",
		"## Usage",
		"",
		"Run help.",
		"",
	}, "\n"))
	writeTestFile(t, filepath.Join(cliDir, "SKILL.md"), strings.Join([]string{
		"# Test Skill",
		"",
		"## Unique Capabilities",
		"",
		"These capabilities aren't available in any other tool for this API.",
		"- **`health`** \u2014 planned health",
		"",
		"## Command Reference",
		"",
		"**items** \u2014 Items",
		"",
	}, "\n"))

	researchDir := t.TempDir()
	research := &ResearchResult{
		APIName: "test",
		NovelFeatures: []NovelFeature{
			{Name: "Health", Command: "health"},
			{Name: "Triage", Command: "triage"},
		},
	}
	require.NoError(t, writeResearchJSON(research, researchDir))

	result := checkNovelFeatures(cliDir, researchDir)
	assert.Equal(t, 2, result.Planned)
	assert.Equal(t, 0, result.Found)
	assert.Len(t, result.Missing, 2)

	// Verify research.json has novel_features_built as non-nil empty
	updated, err := LoadResearch(researchDir)
	require.NoError(t, err)
	assert.Len(t, updated.NovelFeatures, 2, "planned list preserved")
	require.NotNil(t, updated.NovelFeaturesBuilt, "must be non-nil so fallback doesn't kick in")
	assert.Empty(t, *updated.NovelFeaturesBuilt, "empty — nothing survived")

	readmeData, err := os.ReadFile(filepath.Join(cliDir, "README.md"))
	require.NoError(t, err)
	assert.NotContains(t, string(readmeData), "## Unique Features")
	assert.Contains(t, string(readmeData), "## Usage")

	skillData, err := os.ReadFile(filepath.Join(cliDir, "SKILL.md"))
	require.NoError(t, err)
	assert.NotContains(t, string(skillData), "## Unique Capabilities")
	assert.Contains(t, string(skillData), "## Command Reference")

	rootData, err := os.ReadFile(filepath.Join(cliCodeDir, "root.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(rootData), "Highlights (not in the official API docs):")
	assert.NotContains(t, string(rootData), "planned health")
}

// TestCheckNovelFeatures_CrossCutting pins the cross-cutting-feature
// fallback added in #1197: planned features whose Command is a
// parenthetical marker ("(any) --dry-run", "(internal client behavior)",
// "(any read command, default behavior)") are detected against
// rootFlags / agent-authored internal packages rather than reported as
// missing. Flag-named features still report missing when the flag isn't
// declared anywhere in CLI source.
func TestCheckNovelFeatures_CrossCutting(t *testing.T) {
	// Helper: minimal CLI fixture with a single command file plus a
	// root.go that declares a few persistent flags as string literals.
	// Optional agentPkg adds an agent-authored internal/<name>/<name>.go.
	setupCLI := func(t *testing.T, agentPkg string) string {
		t.Helper()
		cliDir := t.TempDir()
		cliCodeDir := filepath.Join(cliDir, "internal", "cli")
		require.NoError(t, os.MkdirAll(cliCodeDir, 0o755))
		writeTestFile(t, filepath.Join(cliCodeDir, "health.go"),
			`package cli
func newHealthCmd() *cobra.Command {
	return &cobra.Command{Use: "health"}
}`)
		writeTestFile(t, filepath.Join(cliCodeDir, "root.go"), strings.Join([]string{
			`package cli`,
			``,
			`func newRootCmd() *cobra.Command {`,
			`	rootCmd := &cobra.Command{Use: "test-pp-cli"}`,
			`	rootCmd.PersistentFlags().Bool("dry-run", false, "preview only")`,
			`	rootCmd.PersistentFlags().String("tier", "", "service tier")`,
			`	rootCmd.AddCommand(newHealthCmd())`,
			`	return rootCmd`,
			`}`,
			``,
		}, "\n"))
		if agentPkg != "" {
			agentDir := filepath.Join(cliDir, "internal", agentPkg)
			require.NoError(t, os.MkdirAll(agentDir, 0o755))
			writeTestFile(t, filepath.Join(agentDir, "request.go"),
				"package "+agentPkg+"\n\nfunc DoRequest() {}\n")
		}
		return cliDir
	}

	t.Run("global flag declared on rootCmd resolves (any) marker", func(t *testing.T) {
		cliDir := setupCLI(t, "")
		researchDir := t.TempDir()
		require.NoError(t, writeResearchJSON(&ResearchResult{
			APIName: "test",
			NovelFeatures: []NovelFeature{
				{Name: "Dry-run mode", Command: "(any) --dry-run"},
				{Name: "Service tier", Command: "(any) --tier standard"},
			},
		}, researchDir))

		result := checkNovelFeatures(cliDir, researchDir)
		assert.Equal(t, 2, result.Planned)
		assert.Equal(t, 2, result.Found)
		assert.Empty(t, result.Missing)
	})

	t.Run("undeclared flag reports missing", func(t *testing.T) {
		cliDir := setupCLI(t, "")
		researchDir := t.TempDir()
		require.NoError(t, writeResearchJSON(&ResearchResult{
			APIName: "test",
			NovelFeatures: []NovelFeature{
				{Name: "Bogus flag", Command: "(any) --nonexistent-flag"},
			},
		}, researchDir))

		result := checkNovelFeatures(cliDir, researchDir)
		assert.Equal(t, 0, result.Found)
		assert.Equal(t, []string{"(any) --nonexistent-flag"}, result.Missing)
	})

	t.Run("internal marker resolves when an agent-authored package exists", func(t *testing.T) {
		cliDir := setupCLI(t, "dfs")
		researchDir := t.TempDir()
		require.NoError(t, writeResearchJSON(&ResearchResult{
			APIName: "test",
			NovelFeatures: []NovelFeature{
				{Name: "Adaptive client", Command: "(internal client behavior)"},
				{Name: "Config resolution", Command: "(internal config resolution)"},
			},
		}, researchDir))

		result := checkNovelFeatures(cliDir, researchDir)
		assert.Equal(t, 2, result.Found)
		assert.Empty(t, result.Missing)
	})

	t.Run("internal marker reports missing when no agent package exists", func(t *testing.T) {
		cliDir := setupCLI(t, "")
		researchDir := t.TempDir()
		require.NoError(t, writeResearchJSON(&ResearchResult{
			APIName: "test",
			NovelFeatures: []NovelFeature{
				{Name: "Adaptive client", Command: "(internal client behavior)"},
			},
		}, researchDir))

		result := checkNovelFeatures(cliDir, researchDir)
		assert.Equal(t, 0, result.Found)
		assert.Equal(t, []string{"(internal client behavior)"}, result.Missing)
	})

	t.Run("any-marker description without flag trusts the planner", func(t *testing.T) {
		cliDir := setupCLI(t, "")
		researchDir := t.TempDir()
		require.NoError(t, writeResearchJSON(&ResearchResult{
			APIName: "test",
			NovelFeatures: []NovelFeature{
				{Name: "Default behavior", Command: "(any read command, default behavior)"},
			},
		}, researchDir))

		result := checkNovelFeatures(cliDir, researchDir)
		assert.Equal(t, 1, result.Found)
		assert.Empty(t, result.Missing)
	})

	t.Run("regular commands still reported missing when unbuilt", func(t *testing.T) {
		// "sql" and "search" don't have command files and don't carry
		// cross-cutting markers, so they must still appear as missing —
		// the cross-cutting fallback must not mask genuinely-unbuilt
		// commands. The "sql --dry-run" variant pins that a real command
		// verb followed by a globally-declared flag is still reported
		// missing: the fallback must defer to the regular matcher for
		// these shapes, not absorb them just because the flag happens to
		// be quoted in internal/cli/*.go.
		cliDir := setupCLI(t, "dfs")
		researchDir := t.TempDir()
		require.NoError(t, writeResearchJSON(&ResearchResult{
			APIName: "test",
			NovelFeatures: []NovelFeature{
				{Name: "Local SQL", Command: "sql"},
				{Name: "Local search", Command: "search"},
				{Name: "SQL dry-run", Command: "sql --dry-run"},
			},
		}, researchDir))

		result := checkNovelFeatures(cliDir, researchDir)
		assert.Equal(t, 0, result.Found)
		assert.ElementsMatch(t, []string{"sql", "search", "sql --dry-run"}, result.Missing)
	})
}

// TestMatchCrossCuttingFeature covers the helper's edge cases directly:
// non-cross-cutting inputs return applied=false so the regular path
// matcher gets to decide; flag detection extracts =value suffixes and
// surrounding punctuation; case-folding is consistent.
func TestMatchCrossCuttingFeature(t *testing.T) {
	cliDir := t.TempDir()
	cliCodeDir := filepath.Join(cliDir, "internal", "cli")
	require.NoError(t, os.MkdirAll(cliCodeDir, 0o755))
	writeTestFile(t, filepath.Join(cliCodeDir, "root.go"),
		"package cli\n\nfunc init() {\n\t_ = \"dry-run\"\n\t_ = \"agent\"\n}\n")

	cases := []struct {
		name    string
		cmd     string
		matched bool
		applied bool
	}{
		{"plain command name yields applied=false", "health", false, false},
		{"space-separated path yields applied=false", "portfolio perf", false, false},
		{"command followed by flag yields applied=false", "sql --dry-run", false, false},
		{"bare flag matches declared name", "--dry-run", true, true},
		{"flag with =value suffix matches", "--dry-run=true", true, true},
		{"flag with trailing punctuation matches", "--dry-run,", true, true},
		{"uppercase paren marker normalizes", "(ANY) --dry-run", true, true},
		{"undeclared flag reports missing", "(any) --no-such-flag", false, true},
		{"unknown paren marker yields applied=false", "(observation) something", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			matched, applied := matchCrossCuttingFeature(tc.cmd, cliDir)
			assert.Equal(t, tc.applied, applied, "applied")
			assert.Equal(t, tc.matched, matched, "matched")
		})
	}
}

func captureStderr[T any](t *testing.T, captured *string, fn func() T) T {
	t.Helper()

	old := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w

	result := fn()

	require.NoError(t, w.Close())
	os.Stderr = old
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())
	*captured = string(out)
	return result
}

func requireBefore(t *testing.T, content, before, after string) {
	t.Helper()

	beforeIdx := strings.Index(content, before)
	require.NotEqual(t, -1, beforeIdx, "missing expected content %q", before)
	afterIdx := strings.Index(content, after)
	require.NotEqual(t, -1, afterIdx, "missing expected content %q", after)
	require.Less(t, beforeIdx, afterIdx, "%q should appear before %q", before, after)
}

func TestDeriveDogfoodVerdict_NovelFeatures(t *testing.T) {
	base := &DogfoodReport{
		PathCheck:     PathCheckResult{Tested: 10, Valid: 10, Pct: 100},
		AuthCheck:     AuthCheckResult{Match: true},
		DeadFlags:     DeadCodeResult{Dead: 0},
		DeadFuncs:     DeadCodeResult{Dead: 0},
		PipelineCheck: PipelineResult{SyncCallsDomain: true},
		WiringCheck: WiringCheckResult{
			CommandTree:      CommandTreeResult{Defined: 2, Registered: 2},
			ConfigConsist:    ConfigConsistResult{Consistent: true},
			WorkflowComplete: WorkflowCompleteResult{Skipped: true},
		},
	}

	// Missing novel features → WARN
	base.NovelFeaturesCheck = NovelFeaturesCheckResult{Planned: 3, Found: 1, Missing: []string{"triage", "utilization"}}
	assert.Equal(t, "WARN", deriveDogfoodVerdict(base, true))

	// Depth mismatches → WARN
	base.NovelFeaturesCheck = NovelFeaturesCheckResult{
		Planned: 1,
		Found:   1,
		DepthMismatches: []NovelFeatureDepthMismatch{{
			Command:    "grab",
			Advertised: "grab",
			Actual:     "assets grab",
		}},
	}
	assert.Equal(t, "WARN", deriveDogfoodVerdict(base, true))

	// All found → PASS
	base.NovelFeaturesCheck = NovelFeaturesCheckResult{Planned: 2, Found: 2}
	assert.Equal(t, "PASS", deriveDogfoodVerdict(base, true))

	// Skipped → PASS (no penalty)
	base.NovelFeaturesCheck = NovelFeaturesCheckResult{Skipped: true}
	assert.Equal(t, "PASS", deriveDogfoodVerdict(base, true))
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

func TestCheckNamingConsistency_CleanCLI(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "cmd.go"), `package cli

import "github.com/spf13/cobra"

var forceFlag bool

func newGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "Get a resource",
	}
	cmd.Flags().BoolVar(&forceFlag, "force", false, "skip confirmation")
	return cmd
}

func newListCmd() *cobra.Command {
	return &cobra.Command{Use: "list"}
}
`)

	result := checkNamingConsistency(dir)
	assert.Equal(t, 1, result.Checked)
	assert.Empty(t, result.Violations)
}

func TestCheckNamingConsistency_BannedVerbInfo(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "cmd.go"), `package cli

import "github.com/spf13/cobra"

func newInfoCmd() *cobra.Command {
	return &cobra.Command{Use: "info"}
}
`)

	result := checkNamingConsistency(dir)
	require.Len(t, result.Violations, 1)
	assert.Equal(t, "info", result.Violations[0].Banned)
	assert.Equal(t, "get", result.Violations[0].Preferred)
	assert.Equal(t, "verb", result.Violations[0].Category)
}

func TestCheckNamingConsistency_BannedVerbLs(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "cmd.go"), `package cli

import "github.com/spf13/cobra"

func newLsCmd() *cobra.Command {
	return &cobra.Command{Use: "ls"}
}
`)

	result := checkNamingConsistency(dir)
	require.Len(t, result.Violations, 1)
	assert.Equal(t, "ls", result.Violations[0].Banned)
	assert.Equal(t, "list", result.Violations[0].Preferred)
}

func TestCheckNamingConsistency_BannedFlagSkipConfirmations(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "cmd.go"), `package cli

import "github.com/spf13/cobra"

var skip bool

func newDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "delete"}
	cmd.Flags().BoolVar(&skip, "skip-confirmations", false, "bypass prompts")
	return cmd
}
`)

	result := checkNamingConsistency(dir)
	require.Len(t, result.Violations, 1)
	assert.Equal(t, "--skip-confirmations", result.Violations[0].Banned)
	assert.Equal(t, "--force", result.Violations[0].Preferred)
	assert.Equal(t, "flag", result.Violations[0].Category)
}

func TestCheckNamingConsistency_YesFlagIsNotBanned(t *testing.T) {
	// --yes is a long-standing Unix convention (apt, dnf, etc.) and the
	// printed CLI root template uses it today. Document that only the
	// explicitly-banned skip-confirmations variants trigger the check.
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "cmd.go"), `package cli

import "github.com/spf13/cobra"

var yesFlag bool

func newCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "purge"}
	cmd.PersistentFlags().BoolVarP(&yesFlag, "yes", "y", false, "skip prompt")
	return cmd
}
`)

	result := checkNamingConsistency(dir)
	assert.Empty(t, result.Violations, "--yes is allowed; only --skip-confirmations variants are banned")
}

// TestCheckNamingConsistency_BacktickUse pins that the verb extractor reads
// the leading identifier from a backtick raw-string Use: declaration. A
// banned verb hidden in a backtick literal must still surface as a
// violation; symmetrically, a permitted verb in the same form must not
// produce a false positive.
func TestCheckNamingConsistency_BacktickUse(t *testing.T) {
	t.Run("permitted verb in backtick Use", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
		writeTestFile(t, filepath.Join(dir, "internal", "cli", "cmd.go"),
			"package cli\n"+
				"\n"+
				"import \"github.com/spf13/cobra\"\n"+
				"\n"+
				"func newQueryCmd() *cobra.Command {\n"+
				"\treturn &cobra.Command{Use: `query <project> \"<sql>\"`}\n"+
				"}\n")

		result := checkNamingConsistency(dir)
		assert.Equal(t, 1, result.Checked)
		assert.Empty(t, result.Violations)
	})

	t.Run("banned verb in backtick Use", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
		writeTestFile(t, filepath.Join(dir, "internal", "cli", "cmd.go"),
			"package cli\n"+
				"\n"+
				"import \"github.com/spf13/cobra\"\n"+
				"\n"+
				"func newInfoCmd() *cobra.Command {\n"+
				"\treturn &cobra.Command{Use: `info <project> \"<filter>\"`}\n"+
				"}\n")

		result := checkNamingConsistency(dir)
		require.Len(t, result.Violations, 1)
		assert.Equal(t, "info", result.Violations[0].Banned)
		assert.Equal(t, "verb", result.Violations[0].Category)
	})
}

func TestCheckNamingConsistency_NoFalsePositiveOnIdentifierWithBannedSubstring(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))

	// `getInfoCached` contains `info` but is a Go identifier, not a Use: verb.
	// A body containing --format or --skip as a comment or string literal must
	// not trigger a flag violation.
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "cmd.go"), `package cli

import "github.com/spf13/cobra"

func getInfoCached() string { return "cached" }

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "mentions --skip-confirmations in docstring but does not register it",
	}
}
`)

	result := checkNamingConsistency(dir)
	assert.Empty(t, result.Violations)
}

func TestCheckNamingConsistency_MultipleViolationsSortedByFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "b_cmd.go"), `package cli
import "github.com/spf13/cobra"
func newLs() *cobra.Command { return &cobra.Command{Use: "ls"} }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "a_cmd.go"), `package cli
import "github.com/spf13/cobra"
func newInfo() *cobra.Command { return &cobra.Command{Use: "info"} }
`)

	result := checkNamingConsistency(dir)
	require.Len(t, result.Violations, 2)
	// Sorted by file path — a_cmd.go comes before b_cmd.go
	assert.Contains(t, result.Violations[0].File, "a_cmd.go")
	assert.Equal(t, "info", result.Violations[0].Banned)
	assert.Contains(t, result.Violations[1].File, "b_cmd.go")
	assert.Equal(t, "ls", result.Violations[1].Banned)
}

func TestCheckNamingConsistency_EmptyCLIDir(t *testing.T) {
	dir := t.TempDir()
	// No internal/cli directory at all — check returns empty, not panics.
	result := checkNamingConsistency(dir)
	assert.Equal(t, 0, result.Checked)
	assert.Empty(t, result.Violations)
}

func TestDeriveDogfoodVerdict_NamingViolationFails(t *testing.T) {
	report := &DogfoodReport{
		PathCheck:    PathCheckResult{Tested: 10, Pct: 100},
		AuthCheck:    AuthCheckResult{Match: true},
		ExampleCheck: ExampleCheckResult{Tested: 5, WithExamples: 5},
		PipelineCheck: PipelineResult{
			SyncCallsDomain: true, SearchCallsDomain: true, DomainTables: 1,
		},
		NamingCheck: NamingCheckResult{
			Violations: []NamingViolation{
				{File: "internal/cli/cmd.go", Banned: "info", Preferred: "get", Category: "verb"},
			},
		},
	}
	assert.Equal(t, "FAIL", deriveDogfoodVerdict(report, true))
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// --- resolveDogfoodSpec ---

func TestResolveDogfoodSpec_PrefersBundledOverCallerSpec(t *testing.T) {
	dir := t.TempDir()
	bundled := filepath.Join(dir, "spec.yaml")
	writeTestFile(t, bundled, "openapi: 3.0.0\n")

	caller := filepath.Join(t.TempDir(), "upstream.yaml")
	writeTestFile(t, caller, "openapi: 3.0.0\n")

	resolved, source, overridden := resolveDogfoodSpec(dir, caller)
	assert.Equal(t, bundled, resolved)
	assert.Equal(t, DogfoodSpecSourceBundled, source)
	assert.Equal(t, caller, overridden)
}

func TestResolveDogfoodSpec_NoOverrideWhenCallerPointsAtBundled(t *testing.T) {
	dir := t.TempDir()
	bundled := filepath.Join(dir, "spec.yaml")
	writeTestFile(t, bundled, "openapi: 3.0.0\n")

	resolved, source, overridden := resolveDogfoodSpec(dir, bundled)
	assert.Equal(t, bundled, resolved)
	assert.Equal(t, DogfoodSpecSourceBundled, source)
	assert.Empty(t, overridden, "caller path equal to bundled should not be reported as overridden")
}

func TestResolveDogfoodSpec_FallsThroughWhenNoBundled(t *testing.T) {
	dir := t.TempDir() // empty, no spec.* archived
	caller := filepath.Join(t.TempDir(), "upstream.yaml")
	writeTestFile(t, caller, "openapi: 3.0.0\n")

	resolved, source, overridden := resolveDogfoodSpec(dir, caller)
	assert.Equal(t, caller, resolved)
	assert.Equal(t, DogfoodSpecSourceCaller, source)
	assert.Empty(t, overridden)
}

func TestResolveDogfoodSpec_EmptyWhenNeitherPresent(t *testing.T) {
	resolved, source, overridden := resolveDogfoodSpec(t.TempDir(), "")
	assert.Empty(t, resolved)
	assert.Empty(t, source)
	assert.Empty(t, overridden)
}

func TestResolveDogfoodSpec_PrefersSpecJSONOverSpecYAML(t *testing.T) {
	dir := t.TempDir()
	jsonSpec := filepath.Join(dir, "spec.json")
	yamlSpec := filepath.Join(dir, "spec.yaml")
	writeTestFile(t, jsonSpec, `{"openapi":"3.0.0"}`)
	writeTestFile(t, yamlSpec, "openapi: 3.0.0\n")

	resolved, source, _ := resolveDogfoodSpec(dir, "")
	assert.Equal(t, jsonSpec, resolved, "spec.json should win when both archive formats are present (mirrors findArchivedSpec)")
	assert.Equal(t, DogfoodSpecSourceBundled, source)
}

// End-to-end: RunDogfood should score against the bundled spec when the caller
// passes a different (smaller) spec.
func TestRunDogfood_BundledSpecOverridesCallerSpec(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "store"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
type rootFlags struct{}
func initFlags(flags *rootFlags) { _ = flags }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "users_get.go"), `package cli
func usersGet() { path := "/users/{id}"; _ = path }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "projects_get.go"), `package cli
func projectsGet() { path := "/projects/{id}"; _ = path }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
func authHeader(token string) string { return "Bearer " + token }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "store", "store.go"), "package store\n")

	// Bundled spec: the CLI's own authoritative spec, two endpoints — matches
	// what the CLI actually implements.
	bundledSpec := filepath.Join(dir, "spec.json")
	writeTestFile(t, bundledSpec, `{
  "paths": {
    "/users/{id}": {},
    "/projects/{id}": {}
  },
  "components": {
    "securitySchemes": {
      "BearerAuth": { "type": "http", "scheme": "bearer" }
    }
  }
}`)

	// Caller's --spec: the upstream / partial spec with only one of the two
	// endpoints. Today (pre-fix) this drives Path Validity to 1/2.
	callerSpec := filepath.Join(t.TempDir(), "upstream.json")
	writeTestFile(t, callerSpec, `{
  "paths": {
    "/users/{id}": {}
  },
  "components": {
    "securitySchemes": {
      "BearerAuth": { "type": "http", "scheme": "bearer" }
    }
  }
}`)

	report, err := RunDogfood(dir, callerSpec)
	require.NoError(t, err)

	assert.Equal(t, bundledSpec, report.SpecPath, "RunDogfood should record the bundled path it actually loaded")
	assert.Equal(t, DogfoodSpecSourceBundled, report.SpecSource)
	assert.Equal(t, 2, report.PathCheck.Tested, "should score against the bundled 2-endpoint spec, not the 1-endpoint caller spec")
	assert.Equal(t, 2, report.PathCheck.Valid)
}

// When no spec is archived alongside the CLI, RunDogfood must still honor the
// caller's --spec — no regression for legacy or orphan CLI directories that
// pre-date publish package's spec-bundling.
func TestRunDogfood_FallsBackToCallerSpecWhenNoBundle(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "client"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "store"), 0o755))

	writeTestFile(t, filepath.Join(dir, "internal", "cli", "root.go"), `package cli
type rootFlags struct{}
func initFlags(flags *rootFlags) { _ = flags }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "cli", "users_get.go"), `package cli
func usersGet() { path := "/users/{id}"; _ = path }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
func authHeader(token string) string { return "Bearer " + token }
`)
	writeTestFile(t, filepath.Join(dir, "internal", "store", "store.go"), "package store\n")

	callerSpec := filepath.Join(t.TempDir(), "upstream.json")
	writeTestFile(t, callerSpec, `{
  "paths": { "/users/{id}": {} },
  "components": {
    "securitySchemes": {
      "BearerAuth": { "type": "http", "scheme": "bearer" }
    }
  }
}`)

	report, err := RunDogfood(dir, callerSpec)
	require.NoError(t, err)

	assert.Equal(t, callerSpec, report.SpecPath)
	assert.Equal(t, DogfoodSpecSourceCaller, report.SpecSource)
	assert.Equal(t, 1, report.PathCheck.Tested)
}

// --- checkTestPresence ---

func TestCheckTestPresence_PureLogicPackageWithoutTests(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "internal", "recipes")
	require.NoError(t, os.MkdirAll(pkg, 0o755))
	writeTestFile(t, filepath.Join(pkg, "parse.go"), `package recipes

func Parse(s string) string { return s }
func Normalize(s string) string { return s }
`)

	result := checkTestPresence(dir)
	assert.Equal(t, 1, result.Checked)
	assert.Equal(t, []string{"recipes"}, result.MissingTests)
	assert.Empty(t, result.ThinTests)
}

func TestCheckTestPresence_CommandPackageSkipped(t *testing.T) {
	dir := t.TempDir()
	// A package that uses cobra.Command is skipped — it's command wiring,
	// not pure logic. Test-presence check should leave it alone even with
	// exported functions and no _test.go.
	pkg := filepath.Join(dir, "internal", "adhocwire")
	require.NoError(t, os.MkdirAll(pkg, 0o755))
	writeTestFile(t, filepath.Join(pkg, "cmd.go"), `package adhocwire

import "github.com/spf13/cobra"

func NewCmd() *cobra.Command { return &cobra.Command{} }
`)

	result := checkTestPresence(dir)
	assert.Equal(t, 0, result.Checked)
	assert.Empty(t, result.MissingTests)
}

func TestCheckTestPresence_OnlyUnexportedFuncsSkipped(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "internal", "glue")
	require.NoError(t, os.MkdirAll(pkg, 0o755))
	// No exported funcs → no public surface to test → skipped.
	writeTestFile(t, filepath.Join(pkg, "glue.go"), `package glue

func helper() string { return "x" }
type Kind string
`)

	result := checkTestPresence(dir)
	assert.Equal(t, 0, result.Checked)
}

func TestCheckTestPresence_GeneratorEmittedPackagesSkipped(t *testing.T) {
	dir := t.TempDir()
	// Even pure-logic-shaped packages that are generator-emitted (types,
	// config, client, etc.) are NOT flagged — test seeding for them is a
	// separate template-level concern.
	for _, name := range []string{"types", "config", "client", "cache", "cliutil"} {
		pkg := filepath.Join(dir, "internal", name)
		require.NoError(t, os.MkdirAll(pkg, 0o755))
		writeTestFile(t, filepath.Join(pkg, name+".go"), `package `+name+`

func Exported() string { return "x" }
func AnotherExported() string { return "y" }
`)
	}

	result := checkTestPresence(dir)
	assert.Equal(t, 0, result.Checked)
	assert.Empty(t, result.MissingTests)
}

func TestCheckTestPresence_ThinTestsFlaggedAsWarning(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "internal", "recipes")
	require.NoError(t, os.MkdirAll(pkg, 0o755))
	writeTestFile(t, filepath.Join(pkg, "parse.go"), `package recipes

func Parse(s string) string { return s }
func Normalize(s string) string { return s }
func Validate(s string) bool { return true }
`)
	// One test function — under the 3-test threshold; should surface as a
	// thin-tests warning, not a hard missing-tests error.
	writeTestFile(t, filepath.Join(pkg, "parse_test.go"), `package recipes

import "testing"

func TestParse(t *testing.T) {
	if Parse("x") != "x" {
		t.Fail()
	}
}
`)

	result := checkTestPresence(dir)
	assert.Equal(t, 1, result.Checked)
	assert.Empty(t, result.MissingTests)
	require.Len(t, result.ThinTests, 1)
	assert.Contains(t, result.ThinTests[0], "recipes")
	assert.Contains(t, result.ThinTests[0], "1 test funcs")
}

func TestCheckTestPresence_AdequateTestsClean(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "internal", "recipes")
	require.NoError(t, os.MkdirAll(pkg, 0o755))
	writeTestFile(t, filepath.Join(pkg, "parse.go"), `package recipes

func Parse(s string) string { return s }
`)
	// 3 test functions → passes both thresholds.
	writeTestFile(t, filepath.Join(pkg, "parse_test.go"), `package recipes

import "testing"

func TestParseHappy(t *testing.T) {}
func TestParseEmpty(t *testing.T) {}
func TestParseError(t *testing.T) {}
`)

	result := checkTestPresence(dir)
	assert.Equal(t, 1, result.Checked)
	assert.Empty(t, result.MissingTests)
	assert.Empty(t, result.ThinTests)
}

func TestCheckTestPresence_MissingInternalDir(t *testing.T) {
	dir := t.TempDir()
	// No internal/ directory — result is empty, no panic.
	result := checkTestPresence(dir)
	assert.Equal(t, 0, result.Checked)
	assert.Empty(t, result.MissingTests)
	assert.Empty(t, result.ThinTests)
}

func TestCollectDogfoodIssues_IncludesMissingTests(t *testing.T) {
	report := &DogfoodReport{
		TestPresence: TestPresenceResult{
			Checked:      2,
			MissingTests: []string{"recipes", "goat"},
		},
	}
	issues := collectDogfoodIssues(report, false)
	assert.Contains(t, issues, "pure-logic packages with no tests: recipes, goat")
}

func TestCollectDogfoodIssues_IncludesNovelFeatureDepthMismatch(t *testing.T) {
	report := &DogfoodReport{
		NovelFeaturesCheck: NovelFeaturesCheckResult{
			Planned: 1,
			Found:   1,
			DepthMismatches: []NovelFeatureDepthMismatch{{
				Command:    "grab",
				Advertised: "grab",
				Actual:     "assets grab",
			}},
		},
	}

	issues := collectDogfoodIssues(report, false)
	assert.Contains(t, issues, "1 novel feature command-depth mismatches: grab advertised as grab but registered as assets grab")
}

func TestDeriveDogfoodVerdict_FailsOnMissingTests(t *testing.T) {
	// Regression test: the plan's R5 gate promises "pure-logic packages with
	// zero tests fail shipcheck." Earlier drafts of this work added the
	// issue to report.Issues but never read TestPresence.MissingTests from
	// deriveDogfoodVerdict, leaving the verdict at PASS despite the issue.
	// Without this test the asymmetry is easy to reintroduce.
	report := passingDogfoodReport()
	report.TestPresence = TestPresenceResult{
		Checked:      1,
		MissingTests: []string{"recipes"},
	}
	assert.Equal(t, "FAIL", deriveDogfoodVerdict(report, false))
}

func TestDeriveDogfoodVerdict_PassesWithOnlyThinTests(t *testing.T) {
	// ThinTests alone must not trigger FAIL — they're deferred to Phase 4.85
	// agentic review, not a hard gate.
	report := passingDogfoodReport()
	report.TestPresence = TestPresenceResult{
		Checked:   1,
		ThinTests: []string{"recipes (1 test funcs)"},
	}
	assert.Equal(t, "PASS", deriveDogfoodVerdict(report, false))
}

func TestDogfoodExampleCommandPathsFromAgentContext(t *testing.T) {
	payload := []byte(`{
		"commands": [
			{"name": "posts", "subcommands": [
				{"name": "daily-feed"},
				{"name": "launch_details"}
			]},
			{"name": "auth", "subcommands": [
				{"name": "login"}
			]},
			{"name": "completion", "subcommands": [
				{"name": "bash"}
			]},
			{"name": "feedback", "subcommands": [
				{"name": "list"}
			]},
			{"name": "profile", "subcommands": [
				{"name": "save"},
				{"name": "use"},
				{"name": "list"},
				{"name": "show"},
				{"name": "delete"}
			]},
			{"name": "sync"},
			{"name": "version"},
			{"name": "what_i_missed"}
		]
	}`)

	paths, err := dogfoodExampleCommandPathsFromAgentContext(payload)
	require.NoError(t, err)
	assert.Equal(t, [][]string{
		{"posts", "daily-feed"},
		{"posts", "launch_details"},
		{"what_i_missed"},
	}, paths)
}

// passingDogfoodReport returns a DogfoodReport populated with the minimum
// set of passing sub-check values so deriveDogfoodVerdict returns PASS by
// default. Tests compose on top of this to isolate the one field they're
// exercising without tripping an unrelated default-WARN branch (e.g.,
// PipelineCheck.SyncCallsDomain zero-value triggers WARN).
func passingDogfoodReport() *DogfoodReport {
	return &DogfoodReport{
		PipelineCheck: PipelineResult{SyncCallsDomain: true},
		WiringCheck: WiringCheckResult{
			ConfigConsist: ConfigConsistResult{Consistent: true},
		},
	}
}

func TestCollectDogfoodIssues_ThinTestsNotHardIssue(t *testing.T) {
	// ThinTests are warnings for Wave B's Phase 4.85 review, not hard
	// dogfood issues. Verify they don't leak into the issues slice.
	report := &DogfoodReport{
		TestPresence: TestPresenceResult{
			Checked:   1,
			ThinTests: []string{"recipes (1 test funcs)"},
		},
	}
	issues := collectDogfoodIssues(report, false)
	for _, i := range issues {
		assert.NotContains(t, i, "recipes")
		assert.NotContains(t, i, "thin")
	}
}

// TestDiscoverExampleCheckCommandsIgnoresStderrNoise — printed CLIs
// commonly write to stderr before agent-context succeeds (deprecation
// warnings, config-load notices, panics that race with the JSON
// write). Previously runDogfoodCmd merged stderr into stdout via
// CombinedOutput, prefixing the JSON with text and breaking
// json.Unmarshal with "invalid character 'E' looking for beginning of
// value". discoverExampleCheckCommands must read stdout only so the
// dogfood "Examples" check survives stderr leaks.
func TestDiscoverExampleCheckCommandsIgnoresStderrNoise(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}
	t.Parallel()

	script := `#!/bin/sh
# Deliberately write to stderr before the JSON write — simulates a
# printed CLI emitting a deprecation warning or config-load notice.
echo "WARN: legacy config path detected" >&2
cat <<EOF
{
  "commands": [
    {"name": "posts", "subcommands": [{"name": "list"}]},
    {"name": "auth", "subcommands": [{"name": "login"}]}
  ]
}
EOF
`
	binPath := filepath.Join(t.TempDir(), "fakebin")
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))

	paths, err := discoverExampleCheckCommands(binPath)
	require.NoError(t, err, "stderr noise must not break JSON parsing")
	// auth/login is filtered out because "auth" is in the framework
	// skip set; only the posts subtree should survive.
	assert.Equal(t, [][]string{{"posts", "list"}}, paths)
}

// TestDiscoverExampleCheckCommandsSurfacesStderrOnFailure — when the
// binary actually fails (non-zero exit), stderr carries the reason
// and the caller deserves to see it. Previously a CombinedOutput
// failure left the user with "exit status 1"; the new path uses
// exec.ExitError.Stderr to surface the underlying message.
func TestDiscoverExampleCheckCommandsSurfacesStderrOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as the fake binary; skip on Windows")
	}
	t.Parallel()

	script := `#!/bin/sh
echo "config file ~/.fakebin/config.toml: permission denied" >&2
exit 1
`
	binPath := filepath.Join(t.TempDir(), "fakebin")
	require.NoError(t, os.WriteFile(binPath, []byte(script), 0o755))

	_, err := discoverExampleCheckCommands(binPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "permission denied",
		"stderr from the failed agent-context call must surface in the error")
}
