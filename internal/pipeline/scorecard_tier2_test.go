package pipeline

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestLoadOpenAPISpec_AcceptsHTTPURL is the regression guard for #1001:
// scorer subcommands rejected --spec URLs because the loader called
// os.ReadFile directly. The fix routes through openapi.LoadSpecBytes,
// which dispatches by scheme. A URL must now load successfully on every
// platform without a separate "curl to /tmp" workaround.
func TestLoadOpenAPISpec_AcceptsHTTPURL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
  "openapi": "3.0.0",
  "info": {"title": "Demo", "version": "1.0"},
  "paths": {
    "/things": {"get": {"responses": {"200": {"description": "ok"}}}}
  },
  "components": {
    "securitySchemes": {
      "bearer_auth": {"type": "http", "scheme": "bearer"}
    }
  }
}`))
	}))
	defer srv.Close()

	info, err := loadOpenAPISpec(srv.URL)
	assert.NoError(t, err)
	assert.NotNil(t, info)
	assert.Contains(t, info.Paths, "/things")
	assert.Contains(t, info.SecuritySchemes, "bearer_auth")
}

func TestScorecardTreatsDeviceBackedCLIAsNonHTTP(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pipelineDir := t.TempDir()
	writeFile(t, filepath.Join(dir, "device-spec.yaml"), `version: 1
name: desk-lamp
display_name: Desk Lamp
protocol: ble
identity:
  advertised_names: ["Desk Lamp"]
ble:
  services:
    - uuid: "180f"
      characteristics:
        - uuid: "2a19"
          properties: ["read"]
`)

	sc, err := RunScorecard(dir, pipelineDir, "", nil)
	assert.NoError(t, err)
	assert.Contains(t, sc.UnscoredDimensions, DimPathValidity)
	assert.Contains(t, sc.UnscoredDimensions, DimAuthProtocol)
}

func TestScorecardAcceptsDeviceSpecPathAsNonHTTP(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pipelineDir := t.TempDir()
	specPath := filepath.Join(t.TempDir(), "device.yaml")
	writeFile(t, specPath, `version: 1
name: desk-lamp
display_name: Desk Lamp
protocol: ble
identity:
  advertised_names: ["Desk Lamp"]
ble:
  services:
    - uuid: "180f"
      characteristics:
        - uuid: "2a19"
          properties: ["read"]
`)

	sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
	assert.NoError(t, err)
	assert.Contains(t, sc.UnscoredDimensions, DimPathValidity)
	assert.Contains(t, sc.UnscoredDimensions, DimAuthProtocol)
}

func TestIsThinMCPDescription(t *testing.T) {
	tests := []struct {
		name string
		desc string
		want bool
	}{
		{"empty is thin", "", true},
		{"whitespace is thin", "   ", true},
		{"short and few words", "Get a tag", true}, // 9 chars / 3 words → both trip
		{"both signals trip on short low-word string", "verylongidentifier verylongidentifier", true},                                        // 37 chars / 2 words → both below thresholds
		{"long enough chars passes even if few words", "verylongidentifier verylongidentifier verylongidentifier verylongidentifier", false}, // 73 chars / 4 words → length passes
		{"enough words passes even if short", "Create a new tag in the user workspace", false},                                               // 38 chars / 8 words → words passes
		{"genuinely rich passes", "Create a new tag in the workspace. Required: name. Returns id and slug.", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsThinMCPDescription(tt.desc); got != tt.want {
				t.Errorf("IsThinMCPDescription(%q) = %v, want %v", tt.desc, got, tt.want)
			}
		})
	}
}

func TestScoreMCPDescriptionQuality(t *testing.T) {
	mk := func(t *testing.T, descs []string) string {
		t.Helper()
		dir := t.TempDir()
		tools := make([]map[string]any, 0, len(descs))
		for i, d := range descs {
			tools = append(tools, map[string]any{
				"name":        "tool_" + string(rune('a'+i)),
				"description": d,
			})
		}
		manifest := map[string]any{"tools": tools}
		data, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "tools-manifest.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	mkManifest := func(t *testing.T, manifest ToolsManifest) string {
		t.Helper()
		dir := t.TempDir()
		data, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "tools-manifest.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	t.Run("missing manifest is unscored", func(t *testing.T) {
		dir := t.TempDir()
		score, scored := scoreMCPDescriptionQuality(dir)
		if scored {
			t.Errorf("expected unscored, got scored=%d", score)
		}
	})

	t.Run("empty tools list is unscored", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "tools-manifest.json"), []byte(`{"tools":[]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		score, scored := scoreMCPDescriptionQuality(dir)
		if scored {
			t.Errorf("expected unscored, got scored=%d", score)
		}
	})

	rich := "Create a new tag in the workspace. Required: name. Returns id and slug."
	thin := "Create a tag"

	cases := []struct {
		name  string
		descs []string
		want  int
	}{
		{"all rich -> 10", []string{rich, rich, rich, rich, rich}, 10},
		{"4% thin -> 9", appendN([]string{thin}, rich, 24), 9},
		{"10% thin -> 7", appendN([]string{thin}, rich, 9), 7},
		{"25% thin -> 5", []string{rich, rich, rich, thin}, 5},
		{"40% thin -> 3", []string{rich, rich, rich, thin, thin}, 3},
		{"100% thin -> 0", []string{thin, thin, thin, thin, thin}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := mk(t, c.descs)
			score, scored := scoreMCPDescriptionQuality(dir)
			if !scored || score != c.want {
				t.Errorf("score=%d scored=%v, want %d/true", score, scored, c.want)
			}
		})
	}

	t.Run("hidden endpoint mirrors are not counted", func(t *testing.T) {
		dir := mkManifest(t, ToolsManifest{
			MCP: &ManifestMCP{
				EndpointTools: "hidden",
				Orchestration: "code",
			},
			Tools: []ManifestTool{
				{Name: "demo_get", Description: "Get"},
				{Name: "demo_create", Description: "Create"},
			},
		})
		score, scored := scoreMCPDescriptionQuality(dir)
		if scored {
			t.Errorf("score=%d scored=%v, want unscored", score, scored)
		}
	})

	t.Run("visible endpoint mirrors are still counted", func(t *testing.T) {
		dir := mkManifest(t, ToolsManifest{
			MCP: &ManifestMCP{
				EndpointTools: "visible",
			},
			Tools: []ManifestTool{
				{Name: "demo_get", Description: "Get"},
				{Name: "demo_create", Description: "Create"},
			},
		})
		score, scored := scoreMCPDescriptionQuality(dir)
		if !scored || score != 0 {
			t.Errorf("score=%d scored=%v, want 0/true", score, scored)
		}
	})
}

func appendN(prefix []string, val string, n int) []string {
	out := make([]string, 0, len(prefix)+n)
	out = append(out, prefix...)
	for range n {
		out = append(out, val)
	}
	return out
}

func TestScoreDeadCode(t *testing.T) {
	t.Run("penalizes dead flags and helper functions", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/root.go", `
package cli

var flags struct {
	jsonOutput bool
	csvOutput bool
	stdinInput bool
}

func init() {
	rootCmd.Flags().BoolVar(&flags.jsonOutput, "json", false, "JSON output")
	rootCmd.Flags().BoolVar(&flags.csvOutput, "csv", false, "CSV output")
	rootCmd.Flags().BoolVar(&flags.stdinInput, "stdin", false, "Read stdin")
}
`)
		writeScorecardFixture(t, dir, "internal/cli/messages.go", `
package cli

func runMessages() {
	if flags.jsonOutput {
		println("json")
	}
}
`)
		writeScorecardFixture(t, dir, "internal/cli/helpers.go", `
package cli

func filterFields() {}

func outputCSV() {}

func boundCtx() {}
`)

		// 2 dead flags (csvOutput, stdinInput), 2 dead functions (filterFields, outputCSV)
		assert.Equal(t, 1, scoreDeadCode(dir))
	})

	t.Run("returns full score when nothing is dead", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/root.go", `
package cli

var flags struct {
	jsonOutput bool
}

func init() {
	rootCmd.Flags().BoolVar(&flags.jsonOutput, "json", false, "JSON output")
}
`)
		writeScorecardFixture(t, dir, "internal/cli/messages.go", `
package cli

func runMessages() {
	if flags.jsonOutput {
		println("json")
	}
}
`)

		assert.Equal(t, 5, scoreDeadCode(dir))
	})
}

func TestScoreDataPipelineIntegrity(t *testing.T) {
	t.Run("scores generic store methods and tables low", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/sync.go", `
package cli

import "example.com/project/internal/store"

func runSync(db *store.DB) {
	db.Upsert("messages", nil)
}
`)
		writeScorecardFixture(t, dir, "internal/cli/search.go", `
package cli

func runSearch(db interface{ Search(string) error }) {
	_ = db.Search("term")
}
`)
		writeScorecardFixture(t, dir, "internal/store/store.go", `
package store

const schema = "`+`
CREATE TABLE sync_records (
	id TEXT,
	data JSON,
	synced_at TEXT
);
`+`"
`)

		assert.Equal(t, 1, scoreDataPipelineIntegrity(dir))
	})

	t.Run("scores domain specific pipelines high", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/sync.go", `
package cli

func runSync(db interface {
	UpsertMessage(any) error
	UpsertChannel(any) error
}) {
	_ = db.UpsertMessage(nil)
	_ = db.UpsertChannel(nil)
}
`)
		writeScorecardFixture(t, dir, "internal/cli/search.go", `
package cli

func runSearch(db interface{ SearchMessages(string) error }) {
	_ = db.SearchMessages("hello")
}
`)
		writeScorecardFixture(t, dir, "internal/store/store.go", `
package store

const schema = "`+`
CREATE TABLE messages (
	id TEXT,
	channel_id TEXT,
	author_id TEXT,
	content TEXT,
	created_at TEXT,
	updated_at TEXT
);

CREATE TABLE channels (
	id TEXT,
	guild_id TEXT,
	name TEXT,
	type TEXT,
	position INTEGER,
	synced_at TEXT
);
`+`"
`)

		assert.Equal(t, 9, scoreDataPipelineIntegrity(dir))
	})
}

func TestScoreSyncCorrectness(t *testing.T) {
	t.Run("scores empty resource selection and missing state tracking low", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/sync.go", `
package cli

func defaultSyncResources() []string {
	return []string{}
}

func runSync(resource string) string {
	path := "/" + resource
	return path
}
`)

		assert.LessOrEqual(t, scoreSyncCorrectness(dir), 3)
	})

	t.Run("scores resource defaults pagination and sync state high", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/sync.go", `
package cli

func defaultSyncResources() []string {
	return []string{"channels", "messages"}
}

func runSync(store interface {
	GetSyncState(string) string
	SaveSyncState(string, string)
}) {
	path := "/guilds/{guild_id}/messages"
	cursor := store.GetSyncState("messages")
	paginatedGet(path, cursor)
	store.SaveSyncState("messages", "next")
}

func paginatedGet(path, cursor string) error {
	for {
		params := map[string]string{}
		if cursor != "" {
			params["after"] = cursor
		}
		_, nextCursor, hasMore := fetchPage(path, params)
		if !hasMore || nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	return nil
}
`)

		assert.Equal(t, 10, scoreSyncCorrectness(dir))
	})

	t.Run("scores structural pagination without canonical helper name", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/sync.go", `
package cli

func defaultSyncResources() []string {
	return []string{"channels", "messages"}
}

func runSync(store interface {
	GetSyncState(string) string
	SaveSyncState(string, string)
}, client interface {
	Get(string, map[string]string) ([]byte, error)
}) error {
	path := "/guilds/{guild_id}/messages"
	cursor := store.GetSyncState("messages")
	for {
		params := map[string]string{}
		if cursor != "" {
			params["after"] = cursor
		}
		data, err := client.Get(path, params)
		if err != nil {
			return err
		}
		nextCursor, hasMore := extractNextCursor(data)
		store.SaveSyncState("messages", nextCursor)
		if !hasMore || nextCursor == "" {
			break
		}
		cursor = nextCursor
	}
	return nil
}
`)

		assert.Equal(t, 10, scoreSyncCorrectness(dir))
	})

	t.Run("does not score a named pagination stub as real pagination", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/sync.go", `
package cli

func defaultSyncResources() []string {
	return []string{"messages"}
}

func runSync(store interface {
	GetSyncState(string) string
	SaveSyncState(string, string)
}) {
	path := "/guilds/{guild_id}/messages"
	cursor := store.GetSyncState("messages")
	paginatedGet(path, cursor)
	store.SaveSyncState("messages", "next")
}

func paginatedGet(path, cursor string) error {
	return nil
}
`)

		assert.Less(t, scoreSyncCorrectness(dir), 10)
	})
}

func TestScoreOutputModes(t *testing.T) {
	t.Run("scores structural page progress without page_fetch literal", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/root.go", `
package cli

func init() {
	rootCmd.PersistentFlags().Bool("json", false, "JSON")
	rootCmd.PersistentFlags().Bool("plain", false, "Plain")
	rootCmd.PersistentFlags().String("select", "", "Select")
	rootCmd.PersistentFlags().Bool("csv", false, "CSV")
	rootCmd.PersistentFlags().Bool("quiet", false, "Quiet")
}
`)
		writeScorecardFixture(t, dir, "internal/cli/helpers.go", `
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
)

func filterFields(data json.RawMessage, fields string) json.RawMessage {
	var v any
	_ = json.Unmarshal(data, &v)
	return data
}

func fetchEveryPage() {
	page := 0
	for {
		page++
		fmt.Fprintf(os.Stderr, "fetching page %d...\n", page)
		break
	}
}

func newTabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
}
`)

		assert.Equal(t, 10, scoreOutputModes(dir))
	})
}

func TestScorePathValidity(t *testing.T) {
	t.Run("matches short variable path declarations used by generated commands", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/links.go", `
package cli

func runLinks() string {
	path := "/links"
	return path
}
`)

		specPath := filepath.Join(dir, "spec.json")
		writeScorecardFixture(t, dir, "spec.json", `{
  "paths": {
    "/links": {}
  },
  "components": {
    "securitySchemes": {}
  }
}`)

		spec, err := loadOpenAPISpec(specPath)
		assert.NoError(t, err)
		assert.Equal(t, 10, evaluatePathValidity(dir, spec).score)
	})
}

func TestScoreSpecDimensionsPrefersEmbeddedMergedSpecJSON(t *testing.T) {
	dir := t.TempDir()
	writeScorecardFixture(t, dir, "internal/cli/merged.go", `
package cli

func runMerged() string {
	path := "/merged"
	return path
}
`)
	writeScorecardFixture(t, dir, "spec.json", `{
  "openapi": "3.0.0",
  "paths": {"/merged": {}}
}`)
	callerSpec := filepath.Join(t.TempDir(), "single.json")
	assert.NoError(t, os.WriteFile(callerSpec, []byte(`{
  "openapi": "3.0.0",
  "paths": {"/single": {}}
}`), 0o644))

	sc := &Scorecard{}
	_, err := scoreSpecDimensions(sc, dir, callerSpec)
	assert.NoError(t, err)
	assert.Equal(t, 10, sc.Steinberger.PathValidity)
	assert.NotContains(t, sc.UnscoredDimensions, DimPathValidity)
}

func TestScoreTypeFidelity(t *testing.T) {
	t.Run("scores wrong id flag types and dummy guards low", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/messages.go", `
package cli

import "strings"

var _ = strings.ReplaceAll

func init() {
	cmd := messagesCmd
	cmd.Flags().IntVar(&flagAfterID, "after-id", 0, "After")
}
`)

		assert.Equal(t, 0, scoreTypeFidelity(dir, nil))
	})

	t.Run("scores string id flags but does not treat them as full type fidelity", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/messages.go", `
package cli

func init() {
	cmd := messagesCmd
	cmd.Flags().StringVar(&flagAfterID, "after-id", "", "Snowflake ID to fetch results after the given message")
	cmd.Flags().StringVar(&flagChannelID, "channel-id", "", "Channel ID containing the messages to fetch for sync")
	cmd.Flags().StringVar(&flagGuildID, "guild-id", "", "Guild ID used to scope channel and message syncing")
}
`)

		// String-backed ID flags are valid, but by themselves they do not prove
		// positional arg handling or typed parser coverage.
		assert.Equal(t, 2, scoreTypeFidelity(dir, nil))
	})

	t.Run("scores mixed id flag typing with partial credit", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/messages.go", `
package cli

func init() {
	cmd := messagesCmd
	cmd.Flags().StringVar(&flagAfterID, "after-id", "", "Snowflake ID")
	cmd.Flags().IntVar(&flagChannelID, "channel-id", 0, "Channel ID")
}
`)

		assert.Equal(t, 1, scoreTypeFidelity(dir, nil))
	})

	t.Run("caps multiple cobra arg validator types at two points", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/events.go", `
package cli

import "github.com/spf13/cobra"

var getCmd = &cobra.Command{
	Use:  "get <event_id>",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_ = args[0]
		return nil
	},
}

var exportCmd = &cobra.Command{
	Use:  "export <resource> [id]",
	Args: cobra.MinimumNArgs(1),
}

var searchCmd = &cobra.Command{
	Use:  "search [query]",
	Args: cobra.MaximumNArgs(1),
}
`)

		assert.Equal(t, 2, scoreTypeFidelity(dir, nil))
	})

	t.Run("scores typed parser coverage plus positional command shape high", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/events_get.go", `
package cli

import "github.com/spf13/cobra"

func newEventsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:  "get <calendar_id> <event_id>",
		Args: cobra.ExactArgs(2),
	}
}
`)
		writeScorecardFixture(t, dir, "internal/graph/events.go", `
package graph

import "encoding/json"

type graphEvent struct {
	ID      string `+"`json:\"id\"`"+`
	Subject string `+"`json:\"subject\"`"+`
}

func ParseGraphEvent(data []byte) (graphEvent, error) {
	var event graphEvent
	err := json.Unmarshal(data, &event)
	return event, err
}
`)
		writeScorecardFixture(t, dir, "internal/graph/events_test.go", `
package graph

import "testing"

func TestParseGraphEvent(t *testing.T) {
	_, _ = ParseGraphEvent([]byte("{}"))
}
`)

		assert.Equal(t, 4, scoreTypeFidelity(dir, nil))
	})

	t.Run("scores typed parser coverage plus generated positional use without Args validators", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/events_get.go", `
package cli

import "github.com/spf13/cobra"

func newEventsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use: "get <event_id>",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			_ = args[0]
			return nil
		},
	}
}
`)
		writeScorecardFixture(t, dir, "internal/cli/novel_events.go", `
package cli

import "encoding/json"

type graphEvent struct {
	ID      string `+"`json:\"id\"`"+`
	Subject string `+"`json:\"subject\"`"+`
}

func parseGraphEvent(data []byte) (graphEvent, error) {
	var event graphEvent
	err := json.Unmarshal(data, &event)
	return event, err
}
`)
		writeScorecardFixture(t, dir, "internal/cli/novel_events_test.go", `
package cli

import "testing"

func TestParseGraphEvent(t *testing.T) {
	_, _ = parseGraphEvent([]byte("{}"))
}
`)

		assert.Equal(t, 4, scoreTypeFidelity(dir, &openAPISpecInfo{PositionalParamCount: 1}))
	})

	t.Run("does not score spec positionals without CLI positional handling", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/events_get.go", `
package cli

func init() {
	cmd := eventsCmd
	cmd.Flags().StringVar(&flagQuery, "query", "", "Text query")
}
`)

		assert.Equal(t, 0, scoreTypeFidelity(dir, &openAPISpecInfo{PositionalParamCount: 1}))
	})

	t.Run("does not score advertised positionals without consuming or validating args", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/events_get.go", `
package cli

import "github.com/spf13/cobra"

func newEventsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use: "get <event_id>",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
}
`)

		assert.Equal(t, 0, scoreTypeFidelity(dir, &openAPISpecInfo{PositionalParamCount: 1}))
	})

	t.Run("does not score parser-looking code without tests that mention the parser", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/events_get.go", `
package cli

import "github.com/spf13/cobra"

func newEventsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:  "get <event_id>",
		Args: cobra.ExactArgs(1),
	}
}
`)
		writeScorecardFixture(t, dir, "internal/cli/novel_events.go", `
package cli

import "encoding/json"

type graphEvent struct {
	ID string `+"`json:\"id\"`"+`
}

func parseGraphEvent(data []byte) (graphEvent, error) {
	var event graphEvent
	err := json.Unmarshal(data, &event)
	return event, err
}
`)
		writeScorecardFixture(t, dir, "internal/cli/novel_events_test.go", `
package cli

import "testing"

func TestSomethingElse(t *testing.T) {}
`)

		assert.Equal(t, 2, scoreTypeFidelity(dir, nil))
	})

	t.Run("scores generic string flags without typed parsing low", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/messages.go", `
package cli

func init() {
	cmd := messagesCmd
	cmd.Flags().StringVar(&flagQuery, "query", "", "Text query used to filter the list of messages")
	cmd.Flags().StringVar(&flagStatus, "status", "", "Status value used to filter the list of messages")
}
`)

		assert.LessOrEqual(t, scoreTypeFidelity(dir, nil), 2)
	})
}

// TestIsIDFlagName pins the kebab-case word-boundary semantics that replaced
// the bare `strings.Contains(name, "id")` check. The old check classified
// "price-paid-cents" as an ID flag because "paid" contains "id", which then
// failed the "all ID flags must be StringVar" rule on IntVar money columns.
func TestIsIDFlagName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"id", true},
		{"user-id", true},
		{"id-prefix", true},
		{"parent-id-child", true},
		{"price-paid-cents", false},
		{"validate", false},
		{"kid", false},
		{"wide", false},
		{"video", false},
		{"identity", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isIDFlagName(tc.name))
		})
	}
}

// TestScoreTypeFidelity_FlagDeclRegexBoundedToOneLine pins that consecutive
// Flags() calls stay isolated when detecting ID flag names. Before the
// [^,\n]+ fix the greedy [^,]+ spanned newlines into the next Flags() call.
func TestScoreTypeFidelity_FlagDeclRegexBoundedToOneLine(t *testing.T) {
	dir := t.TempDir()
	writeScorecardFixture(t, dir, "internal/cli/messages.go", `
package cli

func init() {
	cmd := messagesCmd
	cmd.Flags().StringVar(&flagAlphaID, "alpha-id", "", "Alpha identifier")
	cmd.Flags().StringVar(&flagBravoID, "bravo-id", "", "Bravo identifier")
	cmd.Flags().StringVar(&flagCharlieID, "charlie-id", "", "Charlie identifier")
}
`)

	assert.Equal(t, 2, scoreTypeFidelity(dir, nil))
}

// TestScoreTypeFidelity_DoesNotRewardMarkFlagRequired pins that
// MarkFlagRequired no longer earns a point. The SKILL's verify-friendly RunE
// rule forbids it (Cobra evaluates it before RunE, so --dry-run probes fail).
// Rewarding it created a direct scorer-versus-SKILL conflict — a compliant
// agent would always lose this point.
func TestScoreTypeFidelity_DoesNotRewardMarkFlagRequired(t *testing.T) {
	withRequired := t.TempDir()
	writeScorecardFixture(t, withRequired, "internal/cli/messages.go", `
package cli

func init() {
	cmd := messagesCmd
	cmd.Flags().StringVar(&flagAlpha, "alpha", "", "Alpha description with at least seven words here")
	cmd.Flags().StringVar(&flagBravo, "bravo", "", "Bravo description with at least seven words here")
	cmd.Flags().StringVar(&flagCharlie, "charlie", "", "Charlie description with at least seven words here")
	_ = cmd.MarkFlagRequired("alpha")
	_ = cmd.MarkFlagRequired("bravo")
	_ = cmd.MarkFlagRequired("charlie")
}
`)

	withoutRequired := t.TempDir()
	writeScorecardFixture(t, withoutRequired, "internal/cli/messages.go", `
package cli

func init() {
	cmd := messagesCmd
	cmd.Flags().StringVar(&flagAlpha, "alpha", "", "Alpha description with at least seven words here")
	cmd.Flags().StringVar(&flagBravo, "bravo", "", "Bravo description with at least seven words here")
	cmd.Flags().StringVar(&flagCharlie, "charlie", "", "Charlie description with at least seven words here")
}
`)

	assert.Equal(t, scoreTypeFidelity(withoutRequired, nil), scoreTypeFidelity(withRequired, nil),
		"MarkFlagRequired must not earn a scorecard point — it is forbidden by the SKILL's verify-friendly RunE rule")
}

func TestScoreSyncCorrectness_NonSyncFilename(t *testing.T) {
	t.Run("finds sync patterns in non-sync.go files", func(t *testing.T) {
		dir := t.TempDir()

		// Sync logic lives in channel_workflow.go, not sync.go
		writeScorecardFixture(t, dir, "internal/cli/channel_workflow.go", `
package cli

func defaultSyncResources() []string {
	return []string{"bookings", "event_types"}
}

func runChannelSync(store interface {
	GetSyncState(string) string
	SaveSyncState(string, string)
}) {
	path := "/v2/bookings"
	cursor := store.GetSyncState("bookings")
	paginatedGet(path, cursor)
	store.SaveSyncState("bookings", "next")
}
`)

		score := scoreSyncCorrectness(dir)
		assert.GreaterOrEqual(t, score, 7, "sync logic in non-sync.go should score high")
	})
}

func TestScoreDataPipelineIntegrity_NonSyncFilename(t *testing.T) {
	t.Run("finds upsert patterns in non-sync.go files", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/sync_cmd.go", `
package cli

import "example.com/project/internal/store"

func runSync(db *store.DB) {
	_ = db.UpsertBooking(nil)
}
`)
		writeScorecardFixture(t, dir, "internal/cli/search_cmd.go", `
package cli

func runSearch(db interface{ SearchBookings(string) error }) {
	_ = db.SearchBookings("term")
}
`)
		writeScorecardFixture(t, dir, "internal/store/store.go", `
package store

const schema = `+"`"+`
CREATE TABLE bookings (
	id TEXT,
	user_id TEXT,
	event_type_id TEXT,
	title TEXT,
	start_time TEXT,
	end_time TEXT
);
`+"`"+`
`)

		score := scoreDataPipelineIntegrity(dir)
		assert.GreaterOrEqual(t, score, 7, "domain upserts in non-sync.go should score high")
	})

	t.Run("credits generic resources SQL search", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/coin_search.go", `
package cli

import "database/sql"

func runCoinSearch(db *sql.DB, query string) error {
	rows, err := db.Query(`+"`"+`
SELECT resources.data
FROM resources
JOIN resources_fts ON resources_fts.rowid = resources.rowid
WHERE resources.resource_type = ? AND resources_fts MATCH ?
`+"`"+`, "coin", query)
	_ = rows
	return err
}
`)
		writeScorecardFixture(t, dir, "internal/store/store.go", `
package store

const schema = `+"`"+`
CREATE TABLE resources (
	id TEXT PRIMARY KEY,
	resource_type TEXT NOT NULL,
	data TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE VIRTUAL TABLE resources_fts USING fts5(resource_type, data);
`+"`"+`
`)

		score := scoreDataPipelineIntegrity(dir)
		assert.GreaterOrEqual(t, score, 7, "raw SQL search over the generic resources store should get search credit")
	})

	t.Run("credits store-backed generic resources SQL search", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/coin_search.go", `
package cli

import "example.com/project/internal/store"

func runCoinSearch(query string) error {
	db := store.Open()
	rows, err := db.Query(`+"`"+`
SELECT resources.data
FROM resources
WHERE resources.resource_type = ? AND resources.data LIKE ?
`+"`"+`, "coin", query)
	_ = rows
	return err
}
`)
		writeScorecardFixture(t, dir, "internal/store/store.go", `
package store

const schema = `+"`"+`
CREATE TABLE resources (
	id TEXT PRIMARY KEY,
	resource_type TEXT NOT NULL,
	data TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
`+"`"+`
`)

		score := scoreDataPipelineIntegrity(dir)
		assert.GreaterOrEqual(t, score, 7, "store-backed raw SQL search over resources should get search credit")
	})

	t.Run("does not credit copied generic resources SQL without execution", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/notes.go", `
package cli

const copiedQuery = `+"`"+`
SELECT resources.data
FROM resources
JOIN resources_fts ON resources_fts.rowid = resources.rowid
WHERE resources.resource_type = ? AND resources_fts MATCH ?
`+"`"+`
`)
		writeScorecardFixture(t, dir, "internal/store/store.go", `
package store

const schema = `+"`"+`
CREATE TABLE resources (
	id TEXT PRIMARY KEY,
	resource_type TEXT NOT NULL,
	data TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
`+"`"+`
`)

		score := scoreDataPipelineIntegrity(dir)
		assert.Equal(t, 3, score, "copied SQL text should not get local-store or search execution credit")
	})

	t.Run("does not combine generic SQL signals across files", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/notes.go", `
package cli

const copiedQuery = `+"`"+`
SELECT resources.data
FROM resources
WHERE resources.resource_type = ?
`+"`"+`
`)
		writeScorecardFixture(t, dir, "internal/cli/unrelated_sql.go", `
package cli

import "database/sql"

func runOther(db *sql.DB) error {
	rows, err := db.Query("SELECT id FROM accounts")
	_ = rows
	return err
}
`)
		writeScorecardFixture(t, dir, "internal/store/store.go", `
package store

const schema = `+"`"+`
CREATE TABLE resources (
	id TEXT PRIMARY KEY,
	resource_type TEXT NOT NULL,
	data TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
`+"`"+`
`)

		score := scoreDataPipelineIntegrity(dir)
		assert.Equal(t, 3, score, "generic resources SQL and unrelated SQL execution in different files should not combine")
	})

	t.Run("does not credit orphan generic resources SQL command", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli

func newRootCmd() { rootCmd.AddCommand(newLookupCmd(nil)) }
`)
		writeScorecardFixture(t, dir, "internal/cli/lookup.go", `package cli

func newLookupCmd(flags any) {}
`)
		writeScorecardFixture(t, dir, "internal/cli/coin_search.go", `
package cli

import "database/sql"

func newCoinSearchCmd(flags any) {}

func runCoinSearch(db *sql.DB, query string) error {
	rows, err := db.Query(`+"`"+`
SELECT resources.data
FROM resources
WHERE resources.resource_type = ?
`+"`"+`, "coin")
	_ = rows
	return err
}
`)
		writeScorecardFixture(t, dir, "internal/store/store.go", `
package store

const schema = `+"`"+`
CREATE TABLE resources (
	id TEXT PRIMARY KEY,
	resource_type TEXT NOT NULL,
	data TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
`+"`"+`
`)

		score := scoreDataPipelineIntegrity(dir)
		assert.Equal(t, 3, score, "unregistered generic resources SQL commands should not get search execution credit")
	})
}

func TestScoreDeadCode_FlagsPassedAsArg(t *testing.T) {
	t.Run("flags struct passed to function counts all fields as used", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/root.go", `
package cli

var flags struct {
	asJSON   bool
	csvOutput bool
	verbose  bool
}

func init() {
	rootCmd.Flags().BoolVar(&flags.asJSON, "json", false, "JSON output")
	rootCmd.Flags().BoolVar(&flags.csvOutput, "csv", false, "CSV output")
	rootCmd.Flags().BoolVar(&flags.verbose, "verbose", false, "Verbose")
}
`)
		writeScorecardFixture(t, dir, "internal/cli/messages.go", `
package cli

func runMessages() {
	printOutput(cmd, flags, data, statusCode)
}
`)

		assert.Equal(t, 5, scoreDeadCode(dir))
	})
}

func TestScoreDeadCode_IntraFileHelperCalls(t *testing.T) {
	t.Run("helpers calling other helpers are not dead", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/root.go", `
package cli
`)
		writeScorecardFixture(t, dir, "internal/cli/helpers.go", `
package cli

func formatOutput(data interface{}) string {
	return applyFormat(data)
}

func applyFormat(data interface{}) string {
	return ""
}
`)
		writeScorecardFixture(t, dir, "internal/cli/messages.go", `
package cli

func runMessages() {
	formatOutput(data)
}
`)

		assert.Equal(t, 5, scoreDeadCode(dir))
	})

	t.Run("generated helper extension points are not dead code", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli`)
		writeScorecardFixture(t, dir, "internal/cli/helpers.go", `
package cli

func applyResponsePath() {}
func emitMissingPaginationCursorWarning() {}
func emitMissingPaginationSignalWarning() {}
func emitPaginatedGetMaxPagesWarning() {}
func emitTruncationWarning() {}
func extractGraphQLConnection() {}
func extractGraphQLObject() {}
func formatCLIParamValue() {}
func nextClientSidePaginationCursor() {}
func nextFullPageOffsetCursor() {}
func paginatedGet() {}
func paginationCursorToken() {}
func replacePathParam() {}
func responsePayloadParentAtPath() {}
func writeNoop() {}
`)

		assert.Equal(t, 5, scoreDeadCode(dir))
	})
}

func TestScorecard_VerifyCalibration(t *testing.T) {
	t.Run("verify pass rate sets floor on total score", func(t *testing.T) {
		dir := t.TempDir()

		// Minimal CLI that would score low on static analysis
		writeScorecardFixture(t, dir, "internal/cli/root.go", `
package cli
`)
		writeScorecardFixture(t, dir, "internal/cli/helpers.go", `
package cli
`)
		writeScorecardFixture(t, dir, "README.md", `# Test CLI`)

		pipelineDir := t.TempDir()
		verifyReport := &VerifyReport{
			PassRate:     91.0, // PassRate is 0-100, not 0.0-1.0
			Total:        33,
			Passed:       30,
			DataPipeline: true,
			Verdict:      "PASS",
		}

		sc, err := RunScorecard(dir, pipelineDir, "", verifyReport)
		assert.NoError(t, err)
		// int(91.0) * 80 / 100 = 72 floor
		assert.GreaterOrEqual(t, sc.Steinberger.Total, 72)
		assert.Contains(t, sc.Steinberger.CalibrationNote, "verify pass rate")
	})

	t.Run("verify failure caps data pipeline dimension", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli`)
		writeScorecardFixture(t, dir, "internal/cli/sync.go", `
package cli

import "example.com/project/internal/store"

func runSync(db *store.DB) {
	_ = db.UpsertBooking(nil)
}

func defaultSyncResources() []string {
	return []string{"bookings"}
}
`)
		writeScorecardFixture(t, dir, "internal/cli/search.go", `
package cli

func runSearch(db interface{ SearchBookings(string) error }) {
	_ = db.SearchBookings("term")
}
`)
		writeScorecardFixture(t, dir, "internal/store/store.go", `
package store

const schema = `+"`"+`
CREATE TABLE bookings (
	id TEXT,
	user_id TEXT,
	event_type_id TEXT,
	title TEXT,
	start_time TEXT,
	end_time TEXT
);
`+"`"+`
`)

		pipelineDir := t.TempDir()
		verifyReport := &VerifyReport{
			PassRate:     50.0, // PassRate is 0-100
			DataPipeline: false,
			Verdict:      "FAIL",
		}

		sc, err := RunScorecard(dir, pipelineDir, "", verifyReport)
		assert.NoError(t, err)
		assert.LessOrEqual(t, sc.Steinberger.DataPipelineIntegrity, 5)
	})

	t.Run("nil verify report has no effect", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli`)
		writeScorecardFixture(t, dir, "README.md", `# Test`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, "", nil)
		assert.NoError(t, err)
		assert.Empty(t, sc.Steinberger.CalibrationNote)
	})
}

func TestRunScorecard_UnscoredSpecDimensions(t *testing.T) {
	t.Run("no spec omits path and auth dimensions from scoring", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/cli/links.go", `
package cli

func runLinks() string {
	path := "/links"
	return path
}
`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, "", nil)
		assert.NoError(t, err)
		assert.ElementsMatch(t, []string{"mcp_description_quality", "mcp_token_efficiency", "mcp_remote_transport", "mcp_tool_design", "mcp_surface_strategy", "cache_freshness", "path_validity", "auth_protocol", "data_pipeline_integrity", "sync_correctness", "type_fidelity", "live_api_verification"}, sc.UnscoredDimensions)
		assert.NotContains(t, sc.GapReport, "path_validity scored 0/10 - needs improvement")
		assert.NotContains(t, sc.GapReport, "auth_protocol scored 0/10 - needs improvement")
	})

	t.Run("graphql specs omit rest path validity from scoring", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli`)
		writeScorecardFixture(t, dir, "internal/cli/events.go", `
package cli

func runEvents() string {
	path := "/events"
	return path
}
`)
		specPath := filepath.Join(dir, "spec.yaml")
		writeScorecardFixture(t, dir, "spec.yaml", `
name: events-graphql
version: "0.1.0"
base_url: https://api.example.com
graphql_endpoint_path: /graphql
auth:
  type: none
resources:
  events:
    description: Events
    endpoints:
      list:
        method: POST
        path: /graphql
        description: List events
`)

		sc, err := RunScorecard(dir, t.TempDir(), specPath, nil)
		assert.NoError(t, err)
		assert.Contains(t, sc.UnscoredDimensions, DimPathValidity)
		assert.Zero(t, sc.Steinberger.PathValidity)
		assert.NotContains(t, sc.GapReport, "path_validity scored 0/10 - needs improvement")
	})

	t.Run("OpenAPI GraphQL specs omit rest path validity from scoring", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli`)
		writeScorecardFixture(t, dir, "internal/cli/events.go", `
package cli

func runEvents() string {
	path := "/events"
	return path
}
`)
		specPath := filepath.Join(dir, "openapi.yaml")
		writeScorecardFixture(t, dir, "openapi.yaml", `
openapi: 3.0.3
info:
  title: Events GraphQL
  version: 1.0.0
  x-graphql-endpoint: /graphql
paths:
  /graphql:
    post:
      operationId: graphql
      responses:
        "200":
          description: ok
`)

		sc, err := RunScorecard(dir, t.TempDir(), specPath, nil)
		assert.NoError(t, err)
		assert.Contains(t, sc.UnscoredDimensions, DimPathValidity)
		assert.Zero(t, sc.Steinberger.PathValidity)
		assert.NotContains(t, sc.GapReport, "path_validity scored 0/10 - needs improvement")
	})

	t.Run("no store omits store pipeline dimensions from scoring", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli
func initRoot() { rootCmd.AddCommand(newSyncCmd()) }
`)
		writeScorecardFixture(t, dir, "internal/cli/sync.go", `package cli
import "github.com/spf13/cobra"
func newSyncCmd() *cobra.Command { return &cobra.Command{Use: "sync"} }
`)
		writeScorecardFixture(t, dir, "internal/cli/snapshot.go", `package cli
import "github.com/spf13/cobra"
func newSnapshotCmd() *cobra.Command { return &cobra.Command{Use: "snapshot"} }
`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, "", nil)
		assert.NoError(t, err)
		assert.Contains(t, sc.UnscoredDimensions, DimDataPipelineIntegrity)
		assert.Contains(t, sc.UnscoredDimensions, DimSyncCorrectness)
		assert.Contains(t, sc.UnscoredDimensions, DimTypeFidelity)
		assert.NotContains(t, sc.GapReport, "data_pipeline_integrity scored 0/10 - needs improvement")
		assert.NotContains(t, sc.GapReport, "sync_correctness scored 0/10 - needs improvement")
		assert.NotContains(t, sc.GapReport, "type_fidelity scored 0/5 - needs improvement")
	})

	t.Run("hidden endpoint mirrors omit mcp description quality from scoring", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, ToolsManifestFilename, `{
  "mcp": {
    "endpoint_tools": "hidden",
    "orchestration": "code"
  },
  "tools": [
    {"name": "demo_get", "description": "Get"},
    {"name": "demo_create", "description": "Create"}
  ]
}`)

		sc, err := RunScorecard(dir, t.TempDir(), "", nil)
		assert.NoError(t, err)
		assert.Contains(t, sc.UnscoredDimensions, DimMCPDescriptionQuality)
	})

	t.Run("missing security schemes renormalizes tier2 instead of treating auth as zero", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/cli/links.go", `
package cli

func runLinks() string {
	path := "/links"
	return path
}
`)

		specWithoutAuth := filepath.Join(dir, "spec-no-auth.json")
		writeScorecardFixture(t, dir, "spec-no-auth.json", `{
  "paths": {
    "/links": {}
  },
  "components": {
    "securitySchemes": {}
  }
}`)

		specWithBearer := filepath.Join(dir, "spec-bearer.json")
		writeScorecardFixture(t, dir, "spec-bearer.json", `{
  "paths": {
    "/links": {}
  },
  "security": [
    {
      "bearerAuth": []
    }
  ],
  "components": {
    "securitySchemes": {
      "bearerAuth": {
        "type": "http",
        "scheme": "bearer"
      }
    }
  }
}`)

		pipelineNoAuth := t.TempDir()
		scNoAuth, err := RunScorecard(dir, pipelineNoAuth, specWithoutAuth, nil)
		assert.NoError(t, err)
		assert.Contains(t, scNoAuth.UnscoredDimensions, "auth_protocol")

		pipelineBearer := t.TempDir()
		scBearer, err := RunScorecard(dir, pipelineBearer, specWithBearer, nil)
		assert.NoError(t, err)
		assert.NotContains(t, scBearer.UnscoredDimensions, "auth_protocol")

		assert.Equal(t, scBearer.Steinberger.PathValidity, scNoAuth.Steinberger.PathValidity)
		assert.Equal(t, scBearer.Steinberger.AuthProtocol, 0)
		sharedTier2Raw := scBearer.Steinberger.PathValidity +
			scBearer.Steinberger.DataPipelineIntegrity +
			scBearer.Steinberger.SyncCorrectness +
			scBearer.Steinberger.TypeFidelity +
			scBearer.Steinberger.DeadCode
		noAuthMax := scorecardTierMax(scNoAuth, 60, DimLiveAPIVerification, DimPathValidity, DimAuthProtocol, DimSyncCorrectness, DimDataPipelineIntegrity, DimTypeFidelity)
		bearerMax := scorecardTierMax(scBearer, 60, DimLiveAPIVerification, DimPathValidity, DimAuthProtocol, DimSyncCorrectness, DimDataPipelineIntegrity, DimTypeFidelity)
		expectedDelta := (sharedTier2Raw * 50 / noAuthMax) - (sharedTier2Raw * 50 / bearerMax)
		assert.Equal(t, scBearer.Steinberger.Total+expectedDelta, scNoAuth.Steinberger.Total)
	})

	t.Run("unused declared security schemes leave auth unscored", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/client/client.go", `
package client

func setAuth(req interface{ Header() map[string]string }) {}
`)

		specPath := filepath.Join(dir, "spec-unused-auth.json")
		writeScorecardFixture(t, dir, "spec-unused-auth.json", `{
  "paths": {
    "/links": {
      "get": {
        "responses": {
          "200": { "description": "ok" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "bearerAuth": {
        "type": "http",
        "scheme": "bearer"
      }
    }
  }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		assert.Contains(t, sc.UnscoredDimensions, "auth_protocol")
	})

	t.Run("referenced oauth2 scheme remains scoreable", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/client/client.go", `
package client

type request struct {
	Header map[string]string
}

func setAuth(req *request, token string) {
	req.Header = map[string]string{}
	req.Header["Authorization"] = "Bearer " + token
}
`)

		specPath := filepath.Join(dir, "spec-oauth.json")
		writeScorecardFixture(t, dir, "spec-oauth.json", `{
  "paths": {
    "/links": {
      "get": {
        "security": [
          {
            "oauth": []
          }
        ],
        "responses": {
          "200": { "description": "ok" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "oauth": {
        "type": "oauth2"
      }
    }
  }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		assert.NotContains(t, sc.UnscoredDimensions, "auth_protocol")
		assert.Greater(t, sc.Steinberger.AuthProtocol, 0)
	})

	t.Run("bearer prefix in config scores auth protocol", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/client/client.go", `
package client

import "net/http"

func setAuth(req *http.Request, authHeader string) {
	req.Header.Set("Authorization", authHeader)
}
`)
		writeScorecardFixture(t, dir, "internal/config/config.go", `
package config

import "os"

type Config struct {
	CalComToken string
}

func Load() Config {
	return Config{CalComToken: os.Getenv("CAL_COM_TOKEN")}
}

func (c Config) AuthHeader() string {
	return "Bearer " + c.CalComToken
}
`)

		specPath := filepath.Join(dir, "spec-bearer-config.json")
		writeScorecardFixture(t, dir, "spec-bearer-config.json", `{
  "paths": {
    "/bookings": {
      "get": {
        "security": [
          {
            "CAL_COM_TOKEN": []
          }
        ],
        "responses": {
          "200": { "description": "ok" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "CAL_COM_TOKEN": {
        "type": "http",
        "scheme": "bearer"
      }
    }
  }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		assert.NotContains(t, sc.UnscoredDimensions, "auth_protocol")
		assert.GreaterOrEqual(t, sc.Steinberger.AuthProtocol, 7)
	})

	t.Run("rich env var specs only emission remains scoreable", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/cli/auth.go", `
package cli

func newAuthCmd() {}
`)
		writeScorecardFixture(t, dir, "internal/client/client.go", `
package client

import "net/http"

func setAuth(req *http.Request, authHeader string) {
	req.Header.Set("Authorization", authHeader)
}
`)
		writeScorecardFixture(t, dir, "internal/config/config.go", `
package config

import "os"

type Config struct {
	RichAuthApiKey string
}

func Load() Config {
	return Config{RichAuthApiKey: os.Getenv("RICH_AUTH_API_KEY")}
}

func (c Config) AuthHeader() string {
	return "Bearer " + c.RichAuthApiKey
}
`)

		specPath := filepath.Join(dir, "spec-rich-env-var-specs.json")
		writeScorecardFixture(t, dir, "spec-rich-env-var-specs.json", `{
  "paths": {
    "/items": {
      "get": {
        "security": [
          {
            "RICH_AUTH_API_KEY": []
          }
        ],
        "responses": {
          "200": { "description": "ok" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "RICH_AUTH_API_KEY": {
        "type": "http",
        "scheme": "bearer"
      }
    }
  }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		assert.NotContains(t, sc.UnscoredDimensions, "auth_protocol")
		assert.GreaterOrEqual(t, sc.Steinberger.AuthProtocol, 7)
		assert.Greater(t, sc.Steinberger.Auth, 0)
	})

	t.Run("single header auth scheme scores full runtime protocol", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/client/client.go", `
package client

import "net/http"

func setAuth(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
}
`)
		writeScorecardFixture(t, dir, "internal/config/config.go", `
package config

import "os"

func LoadToken() string {
	return os.Getenv("TOKEN")
}
`)

		specPath := filepath.Join(dir, "spec-single-bearer.json")
		writeScorecardFixture(t, dir, "spec-single-bearer.json", `{
  "security": [
    {
      "TOKEN": []
    }
  ],
  "paths": {
    "/portfolio": {
      "get": {
        "responses": {
          "200": { "description": "ok" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "TOKEN": {
        "type": "http",
        "scheme": "bearer"
      }
    }
  }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		assert.NotContains(t, sc.UnscoredDimensions, "auth_protocol")
		assert.Equal(t, 10, sc.Steinberger.AuthProtocol)
	})

	t.Run("composed header auth scores unreferenced sibling emissions", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/client/client.go", `
package client

import "net/http"

func signKalshiRequest(req *http.Request, key string, signature string, timestamp string) {
	req.Header.Set("KALSHI-ACCESS-KEY", key)
	req.Header.Set("KALSHI-ACCESS-SIGNATURE", signature)
	req.Header.Set("KALSHI-ACCESS-TIMESTAMP", timestamp)
}
`)

		specPath := filepath.Join(dir, "spec-kalshi-composed.json")
		writeScorecardFixture(t, dir, "spec-kalshi-composed.json", `{
  "security": [
    {
      "kalshiAccessKey": []
    }
  ],
  "paths": {
    "/portfolio/balance": {
      "get": {
        "responses": {
          "200": { "description": "ok" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "kalshiAccessKey": {
        "type": "apiKey",
        "in": "header",
        "name": "KALSHI-ACCESS-KEY"
      },
      "kalshiAccessSignature": {
        "type": "apiKey",
        "in": "header",
        "name": "KALSHI-ACCESS-SIGNATURE"
      },
      "kalshiAccessTimestamp": {
        "type": "apiKey",
        "in": "header",
        "name": "KALSHI-ACCESS-TIMESTAMP"
      }
    }
  }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		assert.NotContains(t, sc.UnscoredDimensions, "auth_protocol")
		assert.Equal(t, 10, sc.Steinberger.AuthProtocol)
	})

	t.Run("composed header auth penalizes missing sibling emissions", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/client/client.go", `
package client

import "net/http"

func signKalshiRequest(req *http.Request, key string, signature string) {
	req.Header.Set("KALSHI-ACCESS-KEY", key)
	req.Header.Set("KALSHI-ACCESS-SIGNATURE", signature)
}
`)

		specPath := filepath.Join(dir, "spec-kalshi-composed-missing.json")
		writeScorecardFixture(t, dir, "spec-kalshi-composed-missing.json", `{
  "security": [
    {
      "kalshiAccessKey": []
    }
  ],
  "paths": {
    "/portfolio/balance": {
      "get": {
        "responses": {
          "200": { "description": "ok" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "kalshiAccessKey": {
        "type": "apiKey",
        "in": "header",
        "name": "KALSHI-ACCESS-KEY"
      },
      "kalshiAccessSignature": {
        "type": "apiKey",
        "in": "header",
        "name": "KALSHI-ACCESS-SIGNATURE"
      },
      "kalshiAccessTimestamp": {
        "type": "apiKey",
        "in": "header",
        "name": "KALSHI-ACCESS-TIMESTAMP"
      }
    }
  }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		assert.NotContains(t, sc.UnscoredDimensions, "auth_protocol")
		assert.Less(t, sc.Steinberger.AuthProtocol, 5)
	})

	t.Run("all apiKey header auth scores every required header", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/client/client.go", `
package client

import "net/http"

func setAutotaskAuth(req *http.Request, userName string, secret string, integrationCode string) {
	req.Header.Set("UserName", userName)
	req.Header.Set("Secret", secret)
	req.Header.Set("ApiIntegrationCode", integrationCode)
}
`)

		specPath := filepath.Join(dir, "spec-autotask-composed.json")
		writeScorecardFixture(t, dir, "spec-autotask-composed.json", `{
  "security": [
    {
      "UserName": [],
      "Secret": [],
      "ApiIntegrationCode": []
    }
  ],
  "paths": {
    "/tickets": {
      "get": {
        "responses": {
          "200": { "description": "ok" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "UserName": {
        "type": "apiKey",
        "in": "header",
        "name": "UserName"
      },
      "Secret": {
        "type": "apiKey",
        "in": "header",
        "name": "Secret"
      },
      "ApiIntegrationCode": {
        "type": "apiKey",
        "in": "header",
        "name": "ApiIntegrationCode"
      }
    }
  }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		assert.NotContains(t, sc.UnscoredDimensions, "auth_protocol")
		assert.Equal(t, 10, sc.Steinberger.AuthProtocol)
	})

	t.Run("all apiKey header auth penalizes missing required header", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/client/client.go", `
package client

import "net/http"

func setAutotaskAuth(req *http.Request, userName string, integrationCode string) {
	req.Header.Set("UserName", userName)
	req.Header.Set("ApiIntegrationCode", integrationCode)
}
`)

		specPath := filepath.Join(dir, "spec-autotask-composed-missing.json")
		writeScorecardFixture(t, dir, "spec-autotask-composed-missing.json", `{
  "security": [
    {
      "UserName": [],
      "Secret": [],
      "ApiIntegrationCode": []
    }
  ],
  "paths": {
    "/tickets": {
      "get": {
        "responses": {
          "200": { "description": "ok" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "UserName": {
        "type": "apiKey",
        "in": "header",
        "name": "UserName"
      },
      "Secret": {
        "type": "apiKey",
        "in": "header",
        "name": "Secret"
      },
      "ApiIntegrationCode": {
        "type": "apiKey",
        "in": "header",
        "name": "ApiIntegrationCode"
      }
    }
  }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		assert.NotContains(t, sc.UnscoredDimensions, "auth_protocol")
		assert.Less(t, sc.Steinberger.AuthProtocol, 5)
	})

	t.Run("all apiKey query auth scores every required query parameter", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/client/client.go", `
package client

import "net/http"

func setTrelloAuth(req *http.Request, key string, token string) {
	q := req.URL.Query()
	q.Set("key", key)
	q.Set("token", token)
	req.URL.RawQuery = q.Encode()
}
`)
		writeScorecardFixture(t, dir, "internal/config/config.go", `
package config

import "os"

func Load() {
	if v := os.Getenv("TRELLO_API_KEY"); v != "" {
		cfg.TrelloApiKey = v
	}
	if v := os.Getenv("TRELLO_TOKEN"); v != "" {
		cfg.TrelloToken = v
	}
}
`)

		specPath := filepath.Join(dir, "spec-trello-query-composed.json")
		writeScorecardFixture(t, dir, "spec-trello-query-composed.json", `{
  "security": [
    {
      "APIKey": [],
      "APIToken": []
    }
  ],
  "paths": {
    "/members/me": {
      "get": {
        "responses": {
          "200": { "description": "ok" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "APIKey": {
        "type": "apiKey",
        "in": "query",
        "name": "key",
        "x-auth-env-vars": ["TRELLO_API_KEY"]
      },
      "APIToken": {
        "type": "apiKey",
        "in": "query",
        "name": "token",
        "x-auth-env-vars": ["TRELLO_TOKEN"]
      }
    }
  }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		assert.NotContains(t, sc.UnscoredDimensions, "auth_protocol")
		assert.Equal(t, 10, sc.Steinberger.AuthProtocol)
	})

	t.Run("all apiKey query auth penalizes missing required query parameter", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/client/client.go", `
package client

import "net/http"

func setTrelloAuth(req *http.Request, key string) {
	q := req.URL.Query()
	q.Set("key", key)
	req.URL.RawQuery = q.Encode()
}
`)
		writeScorecardFixture(t, dir, "internal/config/config.go", `
package config

import "os"

func Load() {
	if v := os.Getenv("TRELLO_API_KEY"); v != "" {
		cfg.TrelloApiKey = v
	}
	if v := os.Getenv("TRELLO_TOKEN"); v != "" {
		cfg.TrelloToken = v
	}
}
`)

		specPath := filepath.Join(dir, "spec-trello-query-composed-missing.json")
		writeScorecardFixture(t, dir, "spec-trello-query-composed-missing.json", `{
  "security": [
    {
      "APIKey": [],
      "APIToken": []
    }
  ],
  "paths": {
    "/members/me": {
      "get": {
        "responses": {
          "200": { "description": "ok" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "APIKey": {
        "type": "apiKey",
        "in": "query",
        "name": "key",
        "x-auth-env-vars": ["TRELLO_API_KEY"]
      },
      "APIToken": {
        "type": "apiKey",
        "in": "query",
        "name": "token",
        "x-auth-env-vars": ["TRELLO_TOKEN"]
      }
    }
  }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		assert.NotContains(t, sc.UnscoredDimensions, "auth_protocol")
		assert.Less(t, sc.Steinberger.AuthProtocol, 6)
	})

	t.Run("same-prefix standalone header scheme is not pulled into composed auth", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/client/client.go", `
package client

import "net/http"

func setAuth(req *http.Request, key string) {
	req.Header.Set("X-API-KEY", key)
}
`)

		specPath := filepath.Join(dir, "spec-same-prefix-standalone.json")
		writeScorecardFixture(t, dir, "spec-same-prefix-standalone.json", `{
  "security": [
    {
      "apiKey": []
    }
  ],
  "paths": {
    "/links": {
      "get": {
        "responses": {
          "200": { "description": "ok" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "apiKey": {
        "type": "apiKey",
        "in": "header",
        "name": "X-API-KEY"
      },
      "apiToken": {
        "type": "apiKey",
        "in": "header",
        "name": "X-API-TOKEN"
      }
    }
  }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		assert.NotContains(t, sc.UnscoredDimensions, "auth_protocol")
		assert.GreaterOrEqual(t, sc.Steinberger.AuthProtocol, 8)
	})

	t.Run("same-prefix security alternatives are not merged into composed auth", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/client/client.go", `
package client

import "net/http"

func setAuth(req *http.Request, key string) {
	req.Header.Set("X-API-KEY", key)
}
`)

		specPath := filepath.Join(dir, "spec-same-prefix-alternatives.json")
		writeScorecardFixture(t, dir, "spec-same-prefix-alternatives.json", `{
  "security": [
    {
      "apiKey": []
    },
    {
      "apiSignature": []
    }
  ],
  "paths": {
    "/links": {
      "get": {
        "responses": {
          "200": { "description": "ok" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "apiKey": {
        "type": "apiKey",
        "in": "header",
        "name": "X-API-KEY"
      },
      "apiSignature": {
        "type": "apiKey",
        "in": "header",
        "name": "X-API-SIGNATURE"
      }
    }
  }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		assert.NotContains(t, sc.UnscoredDimensions, "auth_protocol")
		assert.GreaterOrEqual(t, sc.Steinberger.AuthProtocol, 8)
	})

	t.Run("required auth alternative penalizes an unimplemented scheme", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/client/client.go", `
package client

import "net/http"

func setAuth(req *http.Request, key string) {
	req.Header.Set("X-API-Key", key)
}
`)

		specPath := filepath.Join(dir, "spec-required-oauth-and-key.json")
		writeScorecardFixture(t, dir, "spec-required-oauth-and-key.json", `{
  "paths": {
    "/links": {
      "get": {
        "security": [
          {
            "oauth": [],
            "apiKey": []
          }
        ],
        "responses": {
          "200": { "description": "ok" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "oauth": {
        "type": "oauth2"
      },
      "apiKey": {
        "type": "apiKey",
        "in": "header",
        "name": "X-API-Key"
      }
    }
  }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		assert.NotContains(t, sc.UnscoredDimensions, "auth_protocol")
		assert.Less(t, sc.Steinberger.AuthProtocol, 5)
	})

	t.Run("anonymous alternative leaves auth unscored", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/client/client.go", `
package client

type request struct {
	Header map[string]string
}

func setAuth(req *request, token string) {
	req.Header = map[string]string{}
	req.Header["Authorization"] = "Bearer " + token
}
`)

		specPath := filepath.Join(dir, "spec-optional-auth.json")
		writeScorecardFixture(t, dir, "spec-optional-auth.json", `{
  "paths": {
    "/links": {
      "get": {
        "security": [
          {},
          {
            "oauth": []
          }
        ],
        "responses": {
          "200": { "description": "ok" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "oauth": {
        "type": "oauth2"
      }
    }
  }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		assert.Contains(t, sc.UnscoredDimensions, "auth_protocol")
	})

	t.Run("alternative auth schemes use best matching option", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/client/client.go", `
package client

import "net/http"

func setAuth(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
}
`)

		specPath := filepath.Join(dir, "spec-auth-alternatives.json")
		writeScorecardFixture(t, dir, "spec-auth-alternatives.json", `{
  "paths": {
    "/links": {
      "get": {
        "security": [
          {
            "api_key": []
          },
          {
            "oauth": []
          }
        ],
        "responses": {
          "200": { "description": "ok" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "api_key": {
        "type": "apiKey",
        "in": "header",
        "name": "X-API-Key"
      },
      "oauth": {
        "type": "oauth2"
      }
    }
  }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		assert.NotContains(t, sc.UnscoredDimensions, "auth_protocol")
		assert.GreaterOrEqual(t, sc.Steinberger.AuthProtocol, 3)
	})

	t.Run("operation security override can make inherited auth unscored", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/client/client.go", `
package client

import "net/http"

func setAuth(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
}
`)

		specPath := filepath.Join(dir, "spec-root-auth-operation-anon.json")
		writeScorecardFixture(t, dir, "spec-root-auth-operation-anon.json", `{
  "security": [
    {
      "oauth": []
    }
  ],
  "paths": {
    "/links": {
      "get": {
        "security": [],
        "responses": {
          "200": { "description": "ok" }
        }
      },
      "post": {
        "security": [
          {}
        ],
        "responses": {
          "200": { "description": "ok" }
        }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "oauth": {
        "type": "oauth2"
      }
    }
  }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		assert.Contains(t, sc.UnscoredDimensions, "auth_protocol")
	})

	t.Run("invalid spec path returns an error instead of renormalizing", func(t *testing.T) {
		dir := t.TempDir()
		pipelineDir := t.TempDir()

		_, err := RunScorecard(dir, pipelineDir, filepath.Join(dir, "missing-spec.json"), nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "reading spec")
	})

	t.Run("json output keeps numeric fields for backward compatibility", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/cli/links.go", `
package cli

func runLinks() string {
	path := "/links"
	return path
}
`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, "", nil)
		assert.NoError(t, err)

		data, err := json.Marshal(sc)
		assert.NoError(t, err)
		body := string(data)
		assert.True(t, strings.Contains(body, `"path_validity":0`))
		assert.True(t, strings.Contains(body, `"auth_protocol":0`))
		assert.True(t, strings.Contains(body, `"unscored_dimensions":["mcp_description_quality","mcp_token_efficiency","mcp_remote_transport","mcp_tool_design","mcp_surface_strategy","cache_freshness","path_validity","auth_protocol","data_pipeline_integrity","sync_correctness","type_fidelity","live_api_verification"]`))
	})
}

func TestScoreCacheFreshness(t *testing.T) {
	t.Run("no store returns zero unscored", func(t *testing.T) {
		dir := t.TempDir()
		score, scored := scoreCacheFreshness(dir)
		assert.False(t, scored, "missing store.go must mark the dimension unscored")
		assert.Equal(t, 0, score)
	})

	t.Run("store only with no phase 1-3 signals scores zero", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/store/store.go", `package store

func Open() {}`)
		score, scored := scoreCacheFreshness(dir)
		assert.True(t, scored)
		assert.Equal(t, 0, score)
	})

	t.Run("schema version + doctor + auto-refresh scores 10", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/store/store.go", `package store

const StoreSchemaVersion = 1

func migrate() {
	_ = "PRAGMA user_version"
}`)
		writeScorecardFixture(t, dir, "internal/cli/doctor.go", `package cli

func collectCacheReport() {}`)
		writeScorecardFixture(t, dir, "internal/cli/auto_refresh.go", `package cli

func autoRefreshIfStale() {}`)
		writeScorecardFixture(t, dir, "internal/cliutil/freshness.go", `package cliutil

func EnsureFresh() {}`)

		score, scored := scoreCacheFreshness(dir)
		assert.True(t, scored)
		assert.Equal(t, 10, score, "all three freshness signals should top the dimension out without an unrelated share feature")
	})

	t.Run("share feature does not influence freshness", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/store/store.go", `package store

func Open() {}`)
		writeScorecardFixture(t, dir, "internal/share/share.go", `package share

func Export() {}`)
		writeScorecardFixture(t, dir, "internal/cli/share_commands.go", `package cli

func newShareCmd() {}`)

		score, scored := scoreCacheFreshness(dir)
		assert.True(t, scored)
		assert.Equal(t, 0, score, "share is a separate feature and must not award freshness points")
	})

	t.Run("schema version + doctor only scores 5", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/store/store.go", `package store

const StoreSchemaVersion = 1

func migrate() {
	_ = "PRAGMA user_version"
}`)
		writeScorecardFixture(t, dir, "internal/cli/doctor.go", `package cli

func collectCacheReport() {}`)
		score, scored := scoreCacheFreshness(dir)
		assert.True(t, scored)
		assert.Equal(t, 5, score) // 3 (schema gate) + 2 (doctor cache section)
	})

	t.Run("lookup log excludes auto-refresh from denominator", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/store/store.go", `package store

const StoreSchemaVersion = 1

func migrate() {
	_ = "PRAGMA user_version"
	_ = "CREATE TABLE lookup_log (resource_type TEXT, looked_up_at TEXT)"
}`)
		writeScorecardFixture(t, dir, "internal/cli/doctor.go", `package cli

func collectCacheReport() {}`)

		score, scored := scoreCacheFreshness(dir)
		assert.True(t, scored)
		assert.Equal(t, 10, score, "quota-aware CLIs should not be penalized for deliberately omitting auto-refresh")
	})

	t.Run("daily quota helper excludes auto-refresh from denominator", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/store/store.go", `package store

const StoreSchemaVersion = 1

func migrate() {
	_ = "PRAGMA user_version"
}`)
		writeScorecardFixture(t, dir, "internal/cli/doctor.go", `package cli

func collectCacheReport() {}`)
		writeScorecardFixture(t, dir, "internal/cliutil/quota.go", `package cliutil

const DailyQuota = 1000
`)

		score, scored := scoreCacheFreshness(dir)
		assert.True(t, scored)
		assert.Equal(t, 10, score, "per-day quota helpers should mark auto-refresh as intentionally not applicable")
	})

	t.Run("incidental day substring in quota helper does not exclude auto-refresh", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/store/store.go", `package store

const StoreSchemaVersion = 1

func migrate() {
	_ = "PRAGMA user_version"
}`)
		writeScorecardFixture(t, dir, "internal/cli/doctor.go", `package cli

func collectCacheReport() {}`)
		writeScorecardFixture(t, dir, "internal/cliutil/quota.go", `package cliutil

const TotalQuota = 1000

// Resets every Monday.
`)

		score, scored := scoreCacheFreshness(dir)
		assert.True(t, scored)
		assert.Equal(t, 5, score, "incidental day-like words must not mark a CLI as quota-aware freshness")
	})
}

func TestScoreVision_ResourceGroupedCapabilityShapes(t *testing.T) {
	dir := t.TempDir()

	writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli

func newRootCmd() {
	rootCmd.AddCommand(newCoinCmd(nil))
	rootCmd.AddCommand(newAuditCmd(nil))
}`)
	writeScorecardFixture(t, dir, "internal/cli/coin.go", `package cli

import "github.com/spf13/cobra"

func newCoinCmd(flags any) *cobra.Command {
	cmd := &cobra.Command{Use: "coin"}
	cmd.AddCommand(newBatchCmd(flags))
	return cmd
}
`)
	writeScorecardFixture(t, dir, "internal/cli/coin_batch.go", `package cli

import (
	"encoding/json"
	"example.com/project/internal/store"
	"github.com/spf13/cobra"
)

func newBatchCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "batch --list-certs", RunE: func(cmd *cobra.Command, args []string) error {
		db := store.Open()
		rows := db.ListCoins()
		return json.NewEncoder(cmd.OutOrStdout()).Encode(rows)
	}}
}
`)
	writeScorecardFixture(t, dir, "internal/cli/audit.go", `package cli

import "github.com/spf13/cobra"

func newAuditCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "audit", RunE: func(cmd *cobra.Command, args []string) error {
		first := c.Get("/coins")
		second := c.Get("/orders")
		_, _ = first, second
		return nil
	}}
}
`)
	writeScorecardFixture(t, dir, "internal/store/store.go", `package store

func Open() DB { return DB{} }

type DB struct{}

func (DB) ListCoins() []string { return nil }
`)

	score := scoreVision(dir)
	assert.GreaterOrEqual(t, score, 4, "resource-grouped export and workflow equivalents should contribute to Vision")
}

func TestIsVisionExportShapeRequiresStructuredExportWriter(t *testing.T) {
	outputOnly := `package cli

import (
	"fmt"
	"example.com/project/internal/store"
	"github.com/spf13/cobra"
)

func newListCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "list", RunE: func(cmd *cobra.Command, args []string) error {
		db := store.Open()
		fmt.Fprintln(cmd.OutOrStdout(), db.List())
		return nil
	}}
}
`
	assert.False(t, isVisionExportShape(outputOnly), "ordinary store-backed command output must not count as an export shape")
}

func TestScoreVision_IgnoresOrphanWorkflowFile(t *testing.T) {
	dir := t.TempDir()

	writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli

func newRootCmd() { rootCmd.AddCommand(newLookupCmd(nil)) }
`)
	writeScorecardFixture(t, dir, "internal/cli/lookup.go", `package cli

func newLookupCmd(flags any) {}
`)
	writeScorecardFixture(t, dir, "internal/cli/coin_workflow.go", `package cli

func newCoinWorkflowCmd(flags any) {}
`)

	score := scoreVision(dir)
	assert.Equal(t, 0, score, "unregistered workflow-shaped filenames should not inflate Vision")
}

func TestScoreDoctorDetectsHTTPClientReachability(t *testing.T) {
	t.Run("scores named http client get", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/cli/doctor.go", `package cli

import (
	"net/http"
	"time"
)

func newDoctorCmd() {}

func doctorCheckBrowserCDP(cdpURL string) error {
	httpClient := &http.Client{Timeout: 5 * time.Second}
	resp, err := httpClient.Get(cdpURL + "/json/version")
	_ = resp
	return err
}

func doctorReport() {
	_ = "auth token config version"
}
`)

		assert.Equal(t, 10, scoreDoctor(dir))
	})

	t.Run("scores inline http client get", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/cli/doctor.go", `package cli

import (
	"net/http"
	"time"
)

func newDoctorCmd() {}

func doctorCheck() {
	_, _ = (&http.Client{Timeout: 5 * time.Second}).Get("https://example.com/health")
	_ = "auth token config version"
}
`)

		assert.Equal(t, 10, scoreDoctor(dir))
	})

	t.Run("does not score exec curl as HTTP reachability", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/cli/doctor.go", `package cli

import "os/exec"

func newDoctorCmd() {}

func doctorCheck() {
	_ = exec.Command("curl", "https://example.com/health")
	_ = "auth token config version"
}
`)

		assert.Equal(t, 8, scoreDoctor(dir))
	})
}

func TestScoreDoctorLocalDatastoreRequiresRealSQLiteReachability(t *testing.T) {
	dir := t.TempDir()
	writeScorecardFixture(t, dir, ".printing-press.json", `{
  "schema_version": 1,
  "api_name": "local",
  "cli_name": "local-pp-cli",
  "auth_type": "none",
  "spec_format": "sqlite"
}`)
	writeScorecardFixture(t, dir, "internal/cli/doctor.go", `package cli

import "os"

func newDoctorCmd() {}

func doctorCheckPath(path string) error {
	_, err := os.Stat(path)
	return err
}
`)

	assert.Equal(t, 2, scoreDoctor(dir), "local-datastore doctors should not receive auth/config/reachability credit from os.Stat alone")
}

func TestScorecardLocalDatastoreManifestCreditsLocalShape(t *testing.T) {
	dir := t.TempDir()
	writeScorecardFixture(t, dir, ".printing-press.json", `{
  "schema_version": 1,
  "api_name": "chrome-history",
  "cli_name": "chrome-history-pp-cli",
  "auth_type": "none",
  "spec_format": "sqlite",
  "spec_source": "local-sqlite"
}`)
	writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli

func register() {
	rootCmd.AddCommand(newListCmd())
	rootCmd.AddCommand(newSyncCmd())
}
`)
	writeScorecardFixture(t, dir, "internal/cli/doctor.go", `package cli

import (
	"database/sql"
	"os"
)

func newDoctorCmd() {}

func checkLocalSource(path string) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	return db.Ping()
}
`)
	writeScorecardFixture(t, dir, "internal/cli/list.go", `package cli

func newListCmd() *cobra.Command {
	return &cobra.Command{Use: "list"}
}
`)
	writeScorecardFixture(t, dir, "internal/cli/sync.go", `package cli

func newSyncCmd() *cobra.Command {
	return &cobra.Command{Use: "sync"}
}
`)
	writeScorecardFixture(t, dir, "internal/source/sqlite.go", `package source

import "database/sql"

func Query(db *sql.DB) {
	rows, _ := db.Query("SELECT url FROM visits")
	_ = rows
}
`)
	writeScorecardFixture(t, dir, "internal/mcp/server.go", `package mcp

func NewServer() {
	RegisterTools("context", "search")
	_ = "Returns browsing-history results from the local SQLite database"
}
func handleSearch() {}
`)

	assert.GreaterOrEqual(t, scoreDoctor(dir), 4, "local SQLite reachability should substitute for HTTP reachability without granting unrelated credit")
	assert.Equal(t, 10, scoreLocalCache(dir), "manifest-gated local SQLite source should count as the local datastore")
	assert.GreaterOrEqual(t, scoreMCPQuality(dir), 6, "hand-authored local MCP server should not require generated tools.go")
	assert.GreaterOrEqual(t, scoreDataPipelineIntegrity(dir), 8, "SQL-backed local source should count as real data pipeline")

	sc, err := RunScorecard(dir, t.TempDir(), filepath.Join(dir, "not-openapi.sqlite"), nil)
	assert.NoError(t, err, "local-datastore manifests should bypass OpenAPI-only spec parsing")
	assert.Contains(t, sc.UnscoredDimensions, DimPathValidity)
	assert.Contains(t, sc.UnscoredDimensions, DimAuthProtocol)
	assert.Contains(t, sc.UnscoredDimensions, DimSyncCorrectness)
}

func TestScoreWorkflows(t *testing.T) {
	t.Run("counts files matching expanded prefixes", func(t *testing.T) {
		dir := t.TempDir()

		// 3 workflow files by prefix
		writeScorecardFixture(t, dir, "internal/cli/stale_tasks.go", `package cli`)
		writeScorecardFixture(t, dir, "internal/cli/agenda.go", `package cli`)
		writeScorecardFixture(t, dir, "internal/cli/conflicts.go", `package cli`)

		assert.GreaterOrEqual(t, scoreWorkflows(dir), 6) // 3 compound commands → 6
	})

	t.Run("skips infra files", func(t *testing.T) {
		dir := t.TempDir()

		// helpers.go should not count as a workflow
		writeScorecardFixture(t, dir, "internal/cli/helpers.go", `package cli`)
		writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli`)

		assert.Equal(t, 0, scoreWorkflows(dir))
	})

	t.Run("detects store-using commands structurally", func(t *testing.T) {
		dir := t.TempDir()

		// File doesn't match any prefix but imports store
		writeScorecardFixture(t, dir, "internal/cli/bookings_report.go", `
package cli

import "example.com/project/internal/store"

func runReport(db *store.DB) {}
`)
		writeScorecardFixture(t, dir, "internal/cli/availability.go", `
package cli

func runAvailability() {
	db := store.Open()
	_ = db
}
`)

		assert.GreaterOrEqual(t, scoreWorkflows(dir), 4) // 2 compound → 4
	})

	t.Run("counts commands that call package-local store helpers", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/helpers.go", `
package cli

import "example.com/project/internal/store"

func openLocalStore() (*store.Store, error) {
	return store.Open("data.db")
}

func ensureLocalStore() error {
	db, err := openLocalStore()
	if err != nil {
		return err
	}
	_ = db
	return nil
}
`)
		writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli
func newRootCmd() {
	rootCmd.AddCommand(
		newReport1Cmd(nil),
		newReport2Cmd(nil),
		newReport3Cmd(nil),
		newReport4Cmd(nil),
		newReport5Cmd(nil),
		newReport6Cmd(nil),
		newReport7Cmd(nil),
		newLookupCmd(nil),
	)
}
`)
		for i := 1; i <= 7; i++ {
			writeScorecardFixture(t, dir, filepath.Join("internal/cli", fmt.Sprintf("report_%d.go", i)), fmt.Sprintf(`
package cli

func newReport%dCmd(flags any) {}

func runReport%d() error {
	return ensureLocalStore()
}
`, i, i))
		}
		writeScorecardFixture(t, dir, "internal/cli/lookup.go", `package cli
func newLookupCmd(flags any) {}
func runLookup() error { return nil }
`)

		assert.Equal(t, 10, scoreWorkflows(dir))
	})

	t.Run("counts direct store-helper calls and excludes non-store commands", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/helpers.go", `
package cli

import "example.com/project/internal/store"

func openLocalStore() (*store.Store, error) {
	return store.Open("data.db")
}
`)
		writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli
func newRootCmd() {
	rootCmd.AddCommand(
		newReport1Cmd(nil),
		newReport2Cmd(nil),
		newReport3Cmd(nil),
		newReport4Cmd(nil),
		newLookupCmd(nil),
	)
}
`)
		for i := 1; i <= 4; i++ {
			writeScorecardFixture(t, dir, filepath.Join("internal/cli", fmt.Sprintf("report_%d.go", i)), fmt.Sprintf(`
package cli

func newReport%dCmd(flags any) {}

func runReport%d() error {
	db, err := openLocalStore()
	if err != nil {
		return err
	}
	_ = db
	return nil
}
`, i, i))
		}
		writeScorecardFixture(t, dir, "internal/cli/lookup.go", `package cli
func newLookupCmd(flags any) {}
func runLookup() error { return nil }
`)

		assert.Equal(t, 6, scoreWorkflows(dir))
	})

	t.Run("counts commands that call package-local client helpers", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/helpers.go", `
package cli

func fetchMeta(c apiClient) error {
	_, err := c.Get("/meta")
	return err
}

func runQuery(c apiClient) error {
	_, err := c.Post("/query", nil)
	return err
}
`)
		writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli
func newRootCmd() {
	rootCmd.AddCommand(
		newReport1Cmd(nil),
		newReport2Cmd(nil),
		newReport3Cmd(nil),
	)
}
`)
		for i := 1; i <= 3; i++ {
			writeScorecardFixture(t, dir, filepath.Join("internal/cli", fmt.Sprintf("report_%d.go", i)), fmt.Sprintf(`
package cli

func newReport%dCmd(flags any) {}

func runReport%d(c apiClient) error {
	if err := fetchMeta(c); err != nil {
		return err
	}
	return runQuery(c)
}
`, i, i))
		}

		assert.Equal(t, 6, scoreWorkflows(dir))
	})

	t.Run("counts package-local client helper weights greater than one", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/helpers.go", `
	package cli

	func fetchBundle(c apiClient) error {
		if _, err := c.Get("/bundle/meta"); err != nil {
			return err
		}
		_, err := c.Get("/bundle/items")
		return err
	}
	`)
		writeScorecardFixture(t, dir, "internal/cli/report.go", `
	package cli

	func runReport(c apiClient) error {
		return fetchBundle(c)
	}
	`)

		assert.Equal(t, 2, scoreWorkflows(dir))
	})

	t.Run("ignores unregistered commands that call package-local client helpers", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/pxcommon.go", `
package cli

func runQuery(c apiClient) error {
	_, err := c.Post("/query", nil)
	return err
}
`)
		writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli
func newRootCmd() {
	rootCmd.AddCommand(newLookupCmd(nil))
}
`)
		writeScorecardFixture(t, dir, "internal/cli/lookup.go", `package cli
func newLookupCmd(flags any) {}
func runLookup() error { return nil }
`)
		writeScorecardFixture(t, dir, "internal/cli/report.go", `
package cli

func newReportCmd(flags any) {}

func runReport(c apiClient) error {
	return runQuery(c)
}
`)

		assert.Equal(t, 0, scoreWorkflows(dir))
	})

	t.Run("counts package-local helpers using other generated client verbs", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/helpers.go", `
package cli

func updateThing(c apiClient) error {
	_, err := c.Put("/thing", nil)
	return err
}

func deleteThing(c apiClient) error {
	_, err := c.Delete("/thing")
	return err
}
`)
		writeScorecardFixture(t, dir, "internal/cli/report.go", `
package cli

func runReport(c apiClient) error {
	if err := updateThing(c); err != nil {
		return err
	}
	return deleteThing(c)
}
`)

		assert.Equal(t, 2, scoreWorkflows(dir))
	})

	t.Run("counts package-local helpers that call sibling internal clients", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/pxcommon.go", `
package cli

import "example.com/project/internal/phgraphql"

func fetchMeta() error {
	_, err := phgraphql.FetchMeta()
	return err
}

func runQuery() error {
	_, err := phgraphql.Post()
	return err
}
`)
		writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli
func newRootCmd() {
	rootCmd.AddCommand(newReportCmd(nil))
}
`)
		writeScorecardFixture(t, dir, "internal/cli/report.go", `
package cli

func newReportCmd(flags any) {}

func runReport() error {
	if err := fetchMeta(); err != nil {
		return err
	}
	return runQuery()
}
`)

		assert.Equal(t, 2, scoreWorkflows(dir))
	})

	t.Run("does not count commands that only call non-client helpers", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/helpers.go", `
package cli

func staticRows() []string {
	return []string{"cached"}
}
`)
		writeScorecardFixture(t, dir, "internal/cli/report.go", `
package cli

func runReport() []string {
	return staticRows()
}
`)

		assert.Equal(t, 0, scoreWorkflows(dir))
	})

	t.Run("does not double-count same-file command runners as helpers", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/report.go", `
package cli

func newReportCmd(flags any) {
	runReport(flags)
}

func runReport(c apiClient) error {
	_, err := c.Get("/report")
	return err
}
`)

		assert.Equal(t, 0, scoreWorkflows(dir))
	})

	t.Run("counts multi-API-call files", func(t *testing.T) {
		dir := t.TempDir()

		// File makes 2+ different API calls
		writeScorecardFixture(t, dir, "internal/cli/transfer.go", `
package cli

func runTransfer() {
	resp1 := c.Get("/source")
	_ = c.Post("/destination", resp1)
}
`)

		assert.GreaterOrEqual(t, scoreWorkflows(dir), 2) // 1 compound → 2
	})
}

func TestScoreInsight(t *testing.T) {
	t.Run("counts files matching expanded prefixes", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/stats.go", `package cli`)
		writeScorecardFixture(t, dir, "internal/cli/health.go", `package cli`)
		writeScorecardFixture(t, dir, "internal/cli/trends.go", `package cli`)

		assert.GreaterOrEqual(t, scoreInsight(dir), 6) // 3 found → 6
	})

	t.Run("skips infra files", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/helpers.go", `package cli`)
		writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli`)
		writeScorecardFixture(t, dir, "internal/cli/doctor.go", `package cli`)

		assert.Equal(t, 0, scoreInsight(dir))
	})

	t.Run("detects store plus aggregation structurally", func(t *testing.T) {
		dir := t.TempDir()

		// File uses store AND aggregation — should count as insight
		writeScorecardFixture(t, dir, "internal/cli/usage_report.go", `
package cli

import "example.com/project/internal/store"

func runUsageReport(db *store.DB) {
	rows := db.Query("SELECT COUNT(*) FROM bookings GROUP BY status")
	_ = rows
}
`)

		assert.GreaterOrEqual(t, scoreInsight(dir), 2) // 1 found → 2
	})

	t.Run("store without aggregation does not count", func(t *testing.T) {
		dir := t.TempDir()

		// File uses store but NO aggregation — should not count
		writeScorecardFixture(t, dir, "internal/cli/lookup.go", `
package cli

import "example.com/project/internal/store"

func runLookup(db *store.DB) {
	row := db.Query("SELECT * FROM bookings WHERE id = ?")
	_ = row
}
`)

		assert.Equal(t, 0, scoreInsight(dir))
	})

	t.Run("credits manifest novel features whose Use leaves don't match prefix list", func(t *testing.T) {
		dir := t.TempDir()

		// root.go registers each constructor so registeredCommandFiles can
		// verify the files are actually wired in.
		writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli

func register() {
	rootCmd.AddCommand(newTrajectoryCmd())
	rootCmd.AddCommand(newSnapshotCmd())
	rootCmd.AddCommand(newCalendarCmd())
	rootCmd.AddCommand(newLookalikeCmd())
}`)

		// None of these names match insightPrefixes; they would score 0 today
		// without manifest crediting.
		writeScorecardFixture(t, dir, "internal/cli/posts_trajectory.go", `package cli

func newTrajectoryCmd() *cobra.Command {
	return &cobra.Command{Use: "trajectory <slug>"}
}`)
		writeScorecardFixture(t, dir, "internal/cli/category_snapshot.go", `package cli

func newSnapshotCmd() *cobra.Command {
	return &cobra.Command{Use: "snapshot <slug>"}
}`)
		writeScorecardFixture(t, dir, "internal/cli/launches_calendar.go", `package cli

func newCalendarCmd() *cobra.Command {
	return &cobra.Command{Use: "calendar"}
}`)
		writeScorecardFixture(t, dir, "internal/cli/posts_lookalike.go", `package cli

func newLookalikeCmd() *cobra.Command {
	return &cobra.Command{Use: "lookalike <slug>"}
}`)

		// Manifest declares all four as novel features.
		writeScorecardFixture(t, dir, ".printing-press.json", `{
  "schema_version": 1,
  "api_name": "demo",
  "cli_name": "demo-pp-cli",
  "novel_features": [
    {"name": "Trajectory", "command": "posts trajectory", "description": "x"},
    {"name": "Snapshot", "command": "category snapshot", "description": "x"},
    {"name": "Calendar", "command": "launches calendar", "description": "x"},
    {"name": "Lookalike", "command": "posts lookalike", "description": "x"}
  ]
}`)

		assert.Equal(t, 8, scoreInsight(dir), "4 novel-feature commands should map to 8/10 via the found→score table")
	})

	t.Run("ignores manifest novel features whose files are not registered", func(t *testing.T) {
		dir := t.TempDir()

		// root.go registers a different command — registeredCommandFiles is
		// non-empty, so the registration filter applies and orphaned files
		// must be skipped even when they appear in novel_features.
		writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli

func register() {
	rootCmd.AddCommand(newOtherCmd())
}`)
		writeScorecardFixture(t, dir, "internal/cli/other.go", `package cli

func newOtherCmd() *cobra.Command {
	return &cobra.Command{Use: "other"}
}`)
		writeScorecardFixture(t, dir, "internal/cli/posts_trajectory.go", `package cli

func newTrajectoryCmd() *cobra.Command {
	return &cobra.Command{Use: "trajectory"}
}`)
		writeScorecardFixture(t, dir, ".printing-press.json", `{
  "cli_name": "demo-pp-cli",
  "novel_features": [{"name": "T", "command": "posts trajectory", "description": "x"}]
}`)

		assert.Equal(t, 0, scoreInsight(dir), "novel features must still pass the registeredCommandFiles gate")
	})
}

func TestEvaluateAuthProtocol_InferredAuth(t *testing.T) {
	t.Run("inferred auth is scored when Auth inferred marker present", func(t *testing.T) {
		dir := t.TempDir()

		// Config with inferred auth marker and env var
		writeScorecardFixture(t, dir, "internal/config/config.go", `package config
// Auth inferred from API description — verify the env var below is correct
func Load() {
	if v := os.Getenv("EXAMPLE_TOKEN"); v != "" {
		cfg.Token = v
	}
}
func (c *Config) AuthHeader() string {
	return "Bearer " + c.Token
}
`)
		// Client sends Authorization header
		writeScorecardFixture(t, dir, "internal/client/client.go", `package client
func (c *Client) do() {
	req.Header.Set("Authorization", authHeader)
}
`)

		// Spec with NO securitySchemes
		specPath := filepath.Join(dir, "spec.json")
		writeScorecardFixture(t, dir, "spec.json", `{
  "paths": { "/users": { "get": { "responses": { "200": { "description": "ok" } } } } },
  "components": { "securitySchemes": {} }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		// auth_protocol should be SCORED (not in UnscoredDimensions)
		assert.NotContains(t, sc.UnscoredDimensions, "auth_protocol",
			"inferred auth with marker should be scored, not unscored")
		assert.Greater(t, sc.Steinberger.AuthProtocol, 0,
			"inferred auth should score > 0")
	})

	t.Run("inferred cookie header auth is scored", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/config/config.go", `package config
// Auth inferred from API description — verify the env var below is correct
func Load() {
	if v := os.Getenv("EXAMPLE_COOKIE"); v != "" {
		cfg.Cookie = v
	}
}
`)
		writeScorecardFixture(t, dir, "internal/client/client.go", `package client
func (c *Client) do() {
	req.Header.Set("Cookie", authHeader)
}
`)

		specPath := filepath.Join(dir, "spec.json")
		writeScorecardFixture(t, dir, "spec.json", `{
  "paths": { "/users": { "get": { "responses": { "200": { "description": "ok" } } } } },
  "components": { "securitySchemes": {} }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		assert.NotContains(t, sc.UnscoredDimensions, "auth_protocol",
			"inferred cookie auth with Cookie header should be scored")
		assert.GreaterOrEqual(t, sc.Steinberger.AuthProtocol, 8)
	})

	t.Run("query-param auth without inferred marker stays unscored", func(t *testing.T) {
		dir := t.TempDir()

		// Config with env var but NO "Auth inferred" marker (query-param auth)
		writeScorecardFixture(t, dir, "internal/config/config.go", `package config
func Load() {
	if v := os.Getenv("STEAM_API_KEY"); v != "" {
		cfg.APIKey = v
	}
}
`)
		writeScorecardFixture(t, dir, "internal/client/client.go", `package client
func (c *Client) do() {
	q.Set("key", apiKey)
}
`)

		// Spec with NO securitySchemes (query-param auth was inferred by inferQueryParamAuth)
		specPath := filepath.Join(dir, "spec.json")
		writeScorecardFixture(t, dir, "spec.json", `{
  "paths": { "/users": { "get": { "responses": { "200": { "description": "ok" } } } } },
  "components": { "securitySchemes": {} }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		// auth_protocol should be UNSCORED — no marker, no securitySchemes
		assert.Contains(t, sc.UnscoredDimensions, "auth_protocol",
			"query-param auth without inferred marker should stay unscored (not penalized)")
	})

	t.Run("inferred auth with custom header X-Api-Key is scored", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/config/config.go", `package config
// Auth inferred from API description — verify the env var below is correct
func Load() {
	if v := os.Getenv("EXAMPLE_API_KEY"); v != "" {
		cfg.APIKey = v
	}
}
`)
		writeScorecardFixture(t, dir, "internal/client/client.go", `package client
func (c *Client) do() {
	req.Header.Set("X-Api-Key", apiKey)
}
`)

		specPath := filepath.Join(dir, "spec.json")
		writeScorecardFixture(t, dir, "spec.json", `{
  "paths": { "/users": { "get": { "responses": { "200": { "description": "ok" } } } } },
  "components": { "securitySchemes": {} }
}`)

		pipelineDir := t.TempDir()
		sc, err := RunScorecard(dir, pipelineDir, specPath, nil)
		assert.NoError(t, err)
		assert.NotContains(t, sc.UnscoredDimensions, "auth_protocol",
			"inferred auth with custom header should be scored")
		assert.Greater(t, sc.Steinberger.AuthProtocol, 0)
	})
}

func TestEvaluateAuthProtocol_InternalCookieAuth(t *testing.T) {
	t.Run("scores generated Cookie header for internal YAML cookie auth", func(t *testing.T) {
		sc := scoreInternalCookieAuthProtocol(t, `package client
func (c *Client) do() {
	req.Header.Set("Cookie", authHeader)
}
`)

		assert.NotContains(t, sc.UnscoredDimensions, "auth_protocol")
		assert.GreaterOrEqual(t, sc.Steinberger.AuthProtocol, 8)
	})

	t.Run("does not score Cookie mentions without header assignment", func(t *testing.T) {
		sc := scoreInternalCookieAuthProtocol(t, `package client
func (c *Client) do() {
	_ = "Cookie"
	// Cookie appears in documentation, not in an outgoing request.
}
`)

		assert.NotContains(t, sc.UnscoredDimensions, "auth_protocol")
		assert.Less(t, sc.Steinberger.AuthProtocol, 8)
	})
}

func scoreInternalCookieAuthProtocol(t *testing.T, clientContent string) *Scorecard {
	t.Helper()

	dir := t.TempDir()
	writeScorecardFixture(t, dir, "internal/config/config.go", `package config
func Load() {
	if v := os.Getenv("EXAMPLE_COOKIE"); v != "" {
		cfg.Cookie = v
	}
}
`)
	writeScorecardFixture(t, dir, "internal/client/client.go", clientContent)
	specPath := filepath.Join(dir, "spec.yaml")
	writeScorecardFixture(t, dir, "spec.yaml", `name: example
display_name: Example
description: Cookie auth scorecard fixture
base_url: https://api.example.com
auth:
  type: cookie
  header: Cookie
  env_vars:
    - EXAMPLE_COOKIE
resources:
  users:
    description: Users
    endpoints:
      list:
        method: GET
        path: /users
`)

	sc, err := RunScorecard(dir, t.TempDir(), specPath, nil)
	assert.NoError(t, err)
	return sc
}

func TestScoreAuthScheme_APIKeyHeaderUsesCaseInsensitiveHeaderAndGenericAPIKeyEnv(t *testing.T) {
	clientContent := `package client
func (c *Client) do(req *http.Request) {
	req.Header.Set("X-API-Key", cfg.APIKey)
}`
	configContent := `package config
func Load() {
	if v := os.Getenv("SETLIST_FM_API_KEY"); v != "" {
		cfg.APIKey = v
	}
}`
	scheme := openAPISecurityScheme{
		Key:        "x-api-key",
		Type:       "apikey",
		In:         "header",
		HeaderName: "x-api-key",
	}

	score, scoreable := scoreAuthScheme(clientContent, configContent, "", false, scheme)
	assert.True(t, scoreable)
	assert.Equal(t, 10, score)

	partnerOnlyConfig := `package config
func Load() {
	if v := os.Getenv("PARTNER_SERVICE_API_KEY"); v != "" {
		cfg.PartnerAPIKey = v
	}
}`
	assert.False(t, configReadsAPIKeyEnvForScheme(partnerOnlyConfig, scheme))
	partnerOnlyScore, partnerOnlyScoreable := scoreAuthScheme(clientContent, partnerOnlyConfig, "", false, scheme)
	assert.True(t, partnerOnlyScoreable)
	assert.Equal(t, 8, partnerOnlyScore)

	unrelatedScheme := openAPISecurityScheme{
		Key:        "account-id",
		Type:       "apikey",
		In:         "header",
		HeaderName: "X-Account-ID",
	}
	unrelatedScore, unrelatedScoreable := scoreAuthScheme(clientContent, configContent, "", false, unrelatedScheme)
	assert.True(t, unrelatedScoreable)
	assert.Equal(t, 0, unrelatedScore)
}

func TestConfigReadsSchemeEnvVarRequiresExactEnvLookup(t *testing.T) {
	scheme := openAPISecurityScheme{
		Key:     "APIToken",
		Type:    "apikey",
		In:      "query",
		EnvVars: []string{"KEY"},
	}

	assert.True(t, configReadsSchemeEnvVar(`package config
func Load() {
	if v := os.Getenv("KEY"); v != "" {
		cfg.Key = v
	}
}`, scheme))
	assert.True(t, configReadsSchemeEnvVar(`package config
func Load() {
	if v, ok := os.LookupEnv("KEY"); ok {
		cfg.Key = v
	}
}`, scheme))
	assert.False(t, configReadsSchemeEnvVar(`package config
// KEY is mentioned here, but not read.
func Load() {
	cfg.SomeOtherKey = "not-secret"
}`, scheme))
}

func TestRunScorecard_APIKeyHeaderUsesCaseInsensitiveHeaderAndGenericAPIKeyEnv(t *testing.T) {
	dir := t.TempDir()
	writeScorecardFixture(t, dir, "internal/client/client.go", `package client
func (c *Client) do(req *http.Request) {
	req.Header.Set("X-API-Key", cfg.APIKey)
}`)
	writeScorecardFixture(t, dir, "internal/config/config.go", `package config
func Load() {
	if v := os.Getenv("SETLIST_FM_API_KEY"); v != "" {
		cfg.APIKey = v
	}
}`)
	specPath := filepath.Join(dir, "spec.json")
	writeScorecardFixture(t, dir, "spec.json", `{
  "openapi": "3.0.3",
  "info": {"title": "Setlist", "version": "1.0.0"},
  "paths": {
    "/1.0/search/artists": {
      "get": {
        "security": [{"x-api-key": []}],
        "responses": {"200": {"description": "ok"}}
      }
    }
  },
  "components": {
    "securitySchemes": {
      "x-api-key": {"type": "apiKey", "in": "header", "name": "x-api-key"}
    }
  }
}`)

	sc, err := RunScorecard(dir, t.TempDir(), specPath, nil)
	assert.NoError(t, err)
	assert.NotContains(t, sc.UnscoredDimensions, "auth_protocol")
	assert.Equal(t, 10, sc.Steinberger.AuthProtocol)
}

func TestLoadOpenAPISpec_Swagger20SecurityDefinitions(t *testing.T) {
	t.Run("swagger 2.0 apiKey in header with Authorization maps to bearer", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "swagger.json")
		writeScorecardFixture(t, dir, "swagger.json", `{
  "swagger": "2.0",
  "paths": {
    "/api/chat": {
      "get": {
        "responses": { "200": { "description": "ok" } }
      }
    }
  },
  "securityDefinitions": {
    "api_key": {
      "type": "apiKey",
      "in": "header",
      "name": "Authorization"
    }
  },
  "security": [
    { "api_key": [] }
  ]
}`)

		info, err := loadOpenAPISpec(specPath)
		assert.NoError(t, err)
		assert.Len(t, info.SecuritySchemes, 1)
		scheme := info.SecuritySchemes["api_key"]
		assert.Equal(t, "http", scheme.Type)
		assert.Equal(t, "bearer", scheme.Scheme)
		assert.Len(t, info.SecurityRequirements, 1)
	})

	t.Run("swagger 2.0 oauth2 maps correctly", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "swagger.json")
		writeScorecardFixture(t, dir, "swagger.json", `{
  "swagger": "2.0",
  "paths": {
    "/api/data": {
      "get": {
        "responses": { "200": { "description": "ok" } }
      }
    }
  },
  "securityDefinitions": {
    "oauth": {
      "type": "oauth2",
      "flow": "accessCode"
    }
  },
  "security": [
    { "oauth": [] }
  ]
}`)

		info, err := loadOpenAPISpec(specPath)
		assert.NoError(t, err)
		assert.Len(t, info.SecuritySchemes, 1)
		scheme := info.SecuritySchemes["oauth"]
		assert.Equal(t, "oauth2", scheme.Type)
	})

	t.Run("swagger 2.0 basic auth maps to http basic", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "swagger.json")
		writeScorecardFixture(t, dir, "swagger.json", `{
  "swagger": "2.0",
  "paths": { "/api": {} },
  "securityDefinitions": {
    "basicAuth": {
      "type": "basic"
    }
  },
  "security": [
    { "basicAuth": [] }
  ]
}`)

		info, err := loadOpenAPISpec(specPath)
		assert.NoError(t, err)
		scheme := info.SecuritySchemes["basicAuth"]
		assert.Equal(t, "http", scheme.Type)
		assert.Equal(t, "basic", scheme.Scheme)
	})

	t.Run("OAS3 takes precedence over swagger 2.0 securityDefinitions", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "mixed.json")
		writeScorecardFixture(t, dir, "mixed.json", `{
  "paths": {
    "/api": {
      "get": {
        "responses": { "200": { "description": "ok" } }
      }
    }
  },
  "components": {
    "securitySchemes": {
      "bearerAuth": {
        "type": "http",
        "scheme": "bearer"
      }
    }
  },
  "securityDefinitions": {
    "api_key": {
      "type": "apiKey",
      "in": "header",
      "name": "X-API-Key"
    }
  },
  "security": [
    { "bearerAuth": [] }
  ]
}`)

		info, err := loadOpenAPISpec(specPath)
		assert.NoError(t, err)
		// OAS3 should win: only bearerAuth, not api_key.
		assert.Len(t, info.SecuritySchemes, 1)
		_, hasBearerAuth := info.SecuritySchemes["bearerAuth"]
		assert.True(t, hasBearerAuth)
		_, hasAPIKey := info.SecuritySchemes["api_key"]
		assert.False(t, hasAPIKey)
	})

	t.Run("spec with neither OAS3 nor swagger 2.0 security has empty schemes", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "bare.json")
		writeScorecardFixture(t, dir, "bare.json", `{
  "paths": {
    "/api": {}
  }
}`)

		info, err := loadOpenAPISpec(specPath)
		assert.NoError(t, err)
		assert.Empty(t, info.SecuritySchemes)
		assert.Empty(t, info.SecurityRequirements)
	})

	t.Run("swagger 2.0 apiKey without Authorization stays as apikey", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "swagger.json")
		writeScorecardFixture(t, dir, "swagger.json", `{
  "swagger": "2.0",
  "paths": { "/api": {} },
  "securityDefinitions": {
    "token": {
      "type": "apiKey",
      "in": "header",
      "name": "X-API-Token"
    }
  },
  "security": [
    { "token": [] }
  ]
}`)

		info, err := loadOpenAPISpec(specPath)
		assert.NoError(t, err)
		scheme := info.SecuritySchemes["token"]
		assert.Equal(t, "apikey", scheme.Type)
		assert.Equal(t, "header", scheme.In)
		assert.Equal(t, "X-API-Token", scheme.HeaderName)
	})
}

// TestLoadOpenAPISpec_OpenAPIYAML covers the YAML branch that lets
// scorecard --spec foo.yaml work without converting to JSON first.
// The function prefers JSON when the spec begins with `{` and falls
// back to yaml.Unmarshal otherwise so the JSON branch's error message
// stays diagnostic for malformed JSON inputs.
func TestLoadOpenAPISpec_OpenAPIYAML(t *testing.T) {
	t.Run("OpenAPI YAML produces same paths and security as JSON form", func(t *testing.T) {
		dir := t.TempDir()
		yamlPath := filepath.Join(dir, "openapi.yaml")
		writeScorecardFixture(t, dir, "openapi.yaml", `openapi: "3.0.3"
info:
  title: Test API
  version: "1.0.0"
paths:
  /users:
    get:
      operationId: listUsers
      responses:
        "200":
          description: ok
  /widgets:
    post:
      operationId: createWidget
      responses:
        "201":
          description: created
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
`)

		info, err := loadOpenAPISpec(yamlPath)
		assert.NoError(t, err)
		assert.Equal(t, []string{"/users", "/widgets"}, info.Paths)
		scheme := info.SecuritySchemes["bearerAuth"]
		assert.Equal(t, "http", scheme.Type)
		assert.Equal(t, "bearer", scheme.Scheme)
	})

	t.Run("equivalent JSON and YAML specs produce equivalent info", func(t *testing.T) {
		dir := t.TempDir()
		jsonPath := filepath.Join(dir, "spec.json")
		yamlPath := filepath.Join(dir, "spec.yaml")

		writeScorecardFixture(t, dir, "spec.json", `{
  "openapi": "3.0.3",
  "paths": {
    "/items": { "get": { "responses": { "200": { "description": "ok" } } } }
  },
  "components": {
    "securitySchemes": {
      "apiKey": { "type": "apiKey", "in": "header", "name": "X-API-Key" }
    }
  }
}`)
		writeScorecardFixture(t, dir, "spec.yaml", `openapi: "3.0.3"
paths:
  /items:
    get:
      responses:
        "200":
          description: ok
components:
  securitySchemes:
    apiKey:
      type: apiKey
      in: header
      name: X-API-Key
`)

		jsonInfo, err := loadOpenAPISpec(jsonPath)
		assert.NoError(t, err)
		yamlInfo, err := loadOpenAPISpec(yamlPath)
		assert.NoError(t, err)

		assert.Equal(t, jsonInfo.Paths, yamlInfo.Paths)
		assert.Equal(t, jsonInfo.SecuritySchemes, yamlInfo.SecuritySchemes)
	})

	t.Run("malformed YAML returns OpenAPI YAML error", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "broken.yaml")
		// Mixing tabs and spaces is a classic YAML structural failure.
		writeScorecardFixture(t, dir, "broken.yaml", "openapi: \"3.0.3\"\npaths:\n\t/users:\n  get: {}\n")

		_, err := loadOpenAPISpec(specPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "parsing OpenAPI YAML spec")
	})

	t.Run("malformed JSON keeps the JSON-specific error message", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "broken.json")
		// Trailing comma is invalid JSON; whitespace-leading `{` routes to JSON branch.
		writeScorecardFixture(t, dir, "broken.json", `{ "paths": { "/x": {} }, }`)

		_, err := loadOpenAPISpec(specPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "parsing spec JSON")
		assert.NotContains(t, err.Error(), "parsing OpenAPI YAML spec")
	})

	t.Run("internal YAML spec still routes through internal branch", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "internal.yaml")
		writeScorecardFixture(t, dir, "internal.yaml", `name: example
display_name: Example API
description: Test fixture for internal-YAML routing
base_url: https://api.example.com
auth:
  type: bearer_token
  env_vars:
    - EXAMPLE_TOKEN
resources:
  users:
    description: User accounts
    endpoints:
      list:
        method: GET
        path: /users
`)

		info, err := loadOpenAPISpec(specPath)
		assert.NoError(t, err)
		assert.NotNil(t, info, "internal-YAML branch should produce a non-nil info")
	})

	t.Run("OpenAPI path templates count positional parameters", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "path-params.json")
		writeScorecardFixture(t, dir, "path-params.json", `{
  "openapi": "3.0.3",
  "paths": {
    "/calendars/{calendar_id}/events/{event_id}": {},
    "/submissions/CIK{cik}.json": {}
  }
}`)

		info, err := loadOpenAPISpec(specPath)
		assert.NoError(t, err)
		assert.Equal(t, 3, info.PositionalParamCount)
	})

	t.Run("internal YAML positional params count from Param metadata", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "internal-positionals.yaml")
		writeScorecardFixture(t, dir, "internal-positionals.yaml", `name: example
display_name: Example API
description: Test fixture for internal positional metadata
base_url: https://api.example.com
resources:
  events:
    description: Events
    endpoints:
      get:
        method: GET
        path: /events/{event_id}
        params:
          - name: event_id
            type: string
            required: true
            positional: true
`)

		info, err := loadOpenAPISpec(specPath)
		assert.NoError(t, err)
		assert.Equal(t, 1, info.PositionalParamCount)
	})

	t.Run("leading whitespace before { still detects JSON", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "indented.json")
		writeScorecardFixture(t, dir, "indented.json", "\n\n  {\"paths\": {\"/x\": {}}}")

		info, err := loadOpenAPISpec(specPath)
		assert.NoError(t, err)
		assert.Equal(t, []string{"/x"}, info.Paths)
	})

	t.Run("empty file returns explicit error", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "empty.yaml")
		writeScorecardFixture(t, dir, "empty.yaml", "")

		_, err := loadOpenAPISpec(specPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty")
	})

	t.Run("whitespace-only file returns explicit error", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "blank.yaml")
		writeScorecardFixture(t, dir, "blank.yaml", "   \n\n\t  \n")

		_, err := loadOpenAPISpec(specPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty")
	})

	t.Run("UTF-8 BOM-prefixed JSON still detects JSON branch", func(t *testing.T) {
		dir := t.TempDir()
		specPath := filepath.Join(dir, "bom.json")
		// Editors on Windows occasionally emit a UTF-8 BOM at the head of
		// JSON files. Without the BOM strip it would route to the YAML
		// branch and the JSON-specific error message would be lost.
		bom := string([]byte{0xEF, 0xBB, 0xBF})
		writeScorecardFixture(t, dir, "bom.json", bom+`{"paths": {"/y": {}}}`)

		info, err := loadOpenAPISpec(specPath)
		assert.NoError(t, err)
		assert.Equal(t, []string{"/y"}, info.Paths)
	})
}

func writeScorecardFixture(t *testing.T, root, relPath, content string) {
	t.Helper()

	path := filepath.Join(root, relPath)
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatalf("mkdir %s: %v", relPath, err)
	}

	err = os.WriteFile(path, []byte(content), 0o644)
	if err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}

// TestScoreAuthScheme_BearerPrefixOverride pins that the AuthProtocol scorer
// accepts a non-Bearer scheme literal when openAPISecurityScheme.Prefix is
// populated by an internal-YAML spec. The relative comparison (with-Prefix
// scores higher than without) survives weight rebalancing where an absolute
// threshold would not.
func TestScoreAuthScheme_BearerPrefixOverride(t *testing.T) {
	configWithToken := `func (c *Config) AuthHeader() string { return "Token " + c.AccessToken }`
	clientStub := `req.Header.Set("Authorization", c.AuthHeader())`

	withPrefix := openAPISecurityScheme{
		Key:    "bearer_token",
		Type:   "http",
		Scheme: "bearer",
		Prefix: "Token",
	}
	withoutPrefix := withPrefix
	withoutPrefix.Prefix = ""

	scoreWith, _ := scoreAuthScheme(clientStub, configWithToken, "", false, withPrefix)
	scoreWithout, _ := scoreAuthScheme(clientStub, configWithToken, "", false, withoutPrefix)

	assert.Greater(t, scoreWith, scoreWithout,
		"AuthProtocol score with configured prefix must exceed the empty-prefix default when generated code uses the prefix literal")
}

// TestScoreAuthScheme_StructuralOAuthSurface pins that bearer-style schemes
// get credit for real OAuth machinery (refresh-token rotation in config, or a
// dedicated internal/oauth helper package) even when the literal "Bearer "
// string is absent from source — preventing the score-by-literal regression
// where a polish pass could add an unused const to lift the score.
func TestScoreAuthScheme_StructuralOAuthSurface(t *testing.T) {
	bearerScheme := openAPISecurityScheme{Key: "bearerAuth", Type: "http", Scheme: "bearer"}
	oauth2Scheme := openAPISecurityScheme{Key: "oauth2Auth", Type: "oauth2"}
	openIDScheme := openAPISecurityScheme{Key: "oidcAuth", Type: "openidconnect"}
	basicScheme := openAPISecurityScheme{Key: "basicAuth", Type: "http", Scheme: "basic"}

	clientHeaderOnly := `req.Header.Set("Authorization", token)`
	configRefresh := `type Config struct { AccessToken string; RefreshToken string }`
	configNoOAuth := `type Config struct { Token string }`

	t.Run("bearer scheme without literal credited when RefreshToken in config", func(t *testing.T) {
		score, scoreable := scoreAuthScheme(clientHeaderOnly, configRefresh, "", true, bearerScheme)
		assert.True(t, scoreable)
		assert.GreaterOrEqual(t, score, 9, "real OAuth surface should lift score above the literal-grep ceiling")
	})

	t.Run("bearer scheme without literal stays low when no structural OAuth", func(t *testing.T) {
		score, scoreable := scoreAuthScheme(clientHeaderOnly, configNoOAuth, "", false, bearerScheme)
		assert.True(t, scoreable)
		assert.Less(t, score, 7, "absent literal AND absent OAuth surface must not score as wired auth")
	})

	t.Run("oauth2 scheme without literal credited when structural OAuth present", func(t *testing.T) {
		score, scoreable := scoreAuthScheme(clientHeaderOnly, configRefresh, "", true, oauth2Scheme)
		assert.True(t, scoreable)
		assert.GreaterOrEqual(t, score, 9)
	})

	t.Run("openidconnect scheme shares the bearer-style credit path", func(t *testing.T) {
		// openidconnect lives on the same switch case as oauth2, so a future
		// split must not silently drop structural credit from this arm.
		score, scoreable := scoreAuthScheme(clientHeaderOnly, configRefresh, "", true, openIDScheme)
		assert.True(t, scoreable)
		assert.GreaterOrEqual(t, score, 9)
	})

	t.Run("basic scheme not lifted by OAuth surface", func(t *testing.T) {
		// Counter-check: structural OAuth signal must not bleed into non-bearer
		// schemes. A Basic scheme without its literal should stay at the
		// header-name credit ceiling regardless of nearby OAuth machinery.
		withOAuth, _ := scoreAuthScheme(clientHeaderOnly, configRefresh, "", true, basicScheme)
		withoutOAuth, _ := scoreAuthScheme(clientHeaderOnly, configRefresh, "", false, basicScheme)
		assert.Equal(t, withOAuth, withoutOAuth, "OAuth surface must not credit basic-scheme scoring")
	})
}

// TestHasStructuralOAuthSurface pins the helper's recognition signals:
// either a RefreshToken field in generated config.go, or a hand-written
// internal/oauth helper package on disk.
func TestHasStructuralOAuthSurface(t *testing.T) {
	t.Run("refresh-token field in config", func(t *testing.T) {
		dir := t.TempDir()
		got := hasStructuralOAuthSurface(dir, "type Config struct { RefreshToken string }")
		assert.True(t, got)
	})

	t.Run("internal/oauth package on disk", func(t *testing.T) {
		dir := t.TempDir()
		err := os.MkdirAll(filepath.Join(dir, "internal", "oauth"), 0o755)
		assert.NoError(t, err)
		assert.True(t, hasStructuralOAuthSurface(dir, "type Config struct { Token string }"))
	})

	t.Run("neither signal present", func(t *testing.T) {
		dir := t.TempDir()
		assert.False(t, hasStructuralOAuthSurface(dir, "type Config struct { Token string }"))
	})

	t.Run("internal/oauth as a regular file does not count", func(t *testing.T) {
		dir := t.TempDir()
		err := os.MkdirAll(filepath.Join(dir, "internal"), 0o755)
		assert.NoError(t, err)
		err = os.WriteFile(filepath.Join(dir, "internal", "oauth"), []byte("not a package"), 0o644)
		assert.NoError(t, err)
		assert.False(t, hasStructuralOAuthSurface(dir, "type Config struct { Token string }"))
	})

	t.Run("word-anchored RefreshToken rejects same-word neighbours", func(t *testing.T) {
		// Cosmetic identifiers that contain "RefreshToken" as a substring
		// (NoRefreshToken, RefreshTokenError) must not trip the structural
		// check — otherwise the polish pass this fix defeats just renames
		// its decoy.
		dir := t.TempDir()
		assert.False(t, hasStructuralOAuthSurface(dir, "type Config struct { NoRefreshToken bool }"))
		assert.False(t, hasStructuralOAuthSurface(dir, "type RefreshTokenError struct{}"))
	})
}

// TestScoreTerminalUX_TTYDetectionPatterns pins that the TTY-detection check
// accepts all canonical Go idioms, not just the "isatty" literal. The
// generator's own helpers.go template uses (fi.Mode() & os.ModeCharDevice),
// so a substring-only "isatty" check penalized every generated CLI by 1pt.
func TestScoreTerminalUX_TTYDetectionPatterns(t *testing.T) {
	cases := []struct {
		name       string
		helpers    string
		wantCredit bool
	}{
		{
			name: "ModeCharDevice (generator template idiom) credited",
			helpers: `package cli
import "os"
func isTerminal(f *os.File) bool {
	fi, _ := f.Stat()
	return (fi.Mode() & os.ModeCharDevice) != 0
}`,
			wantCredit: true,
		},
		{
			name: "term.IsTerminal (golang.org/x/term) credited",
			helpers: `package cli
import "golang.org/x/term"
func isTerminal() bool { return term.IsTerminal(0) }`,
			wantCredit: true,
		},
		{
			name: "isatty literal (mattn/go-isatty) credited",
			helpers: `package cli
import "github.com/mattn/go-isatty"
func isTerminal() bool { return isatty.IsTerminal(0) }`,
			wantCredit: true,
		},
		{
			name: "no TTY detection at all not credited",
			helpers: `package cli
func isTerminal() bool { return false }`,
			wantCredit: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeScorecardFixture(t, dir, "internal/cli/helpers.go", tc.helpers)
			writeScorecardFixture(t, dir, "internal/cli/root.go", "package cli")

			score := scoreTerminalUX(dir)
			if tc.wantCredit {
				assert.GreaterOrEqual(t, score, 1, "TTY-detection check should award at least 1pt")
			} else {
				assert.Equal(t, 0, score, "no TTY detection should not award the TTY-detection point")
			}
		})
	}
}

// TestScoreVision_LearnCreditIsStaticBehavioral pins the retirement of the
// internal/learn/doc.go presence sentinel. Once the learn loop is default-on,
// every printed CLI ships doc.go, so its presence proves nothing. Vision
// credit for the loop is now split into two static-behavioral quarter points:
//
//	+0.25 when root.go actually registers the learn command surface
//	      (teach + recall + learnings constructor calls, read via the same
//	      reachable-command parse the rest of the scorer uses);
//	+0.25 when the emitted seed artifact carries non-empty entity lookup
//	      seeds (the stamped lookups.SeedConfig map in learn_init.go).
//
// The base fixture is sized at 3.75 raw points (tier1 3.0 + tier2 wiring
// 0.75) so each individual +0.25 crosses the integer boundary to 4, and the
// retired doc.go sentinel alone visibly earns nothing (would cross to 4 if
// it still carried +0.5).
func TestScoreVision_LearnCreditIsStaticBehavioral(t *testing.T) {
	build := func(docSentinel, registered, seeded bool) string {
		dir := t.TempDir()
		rootRegs := "newSearchCmd(nil), newExportCmd(nil), newSyncCmd(nil)"
		if registered {
			rootRegs += ", newTeachCmd(nil, nil), newRecallCmd(nil, nil), newLearningsCmd(nil, nil)"
		}
		writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli
func newRootCmd() { rootCmd.AddCommand(`+rootRegs+`) }
`)
		writeScorecardFixture(t, dir, "internal/cli/search.go", `package cli
func newSearchCmd(flags any) {}
`)
		writeScorecardFixture(t, dir, "internal/cli/export.go", `package cli
func newExportCmd(flags any) {}
`)
		writeScorecardFixture(t, dir, "internal/store/store.go", `package store
`)
		if docSentinel {
			writeScorecardFixture(t, dir, "internal/learn/doc.go", `// Package learn owns the generated recall/teach loop.
package learn
`)
		}
		if seeded {
			writeScorecardFixture(t, dir, "internal/cli/learn_init.go", `package cli
import "example.com/project/internal/learn/lookups"
func initLearn(db any) error {
	seeds := map[string][]lookups.SeedConfig{
		"team": {
			{Canonical: "SEA", Values: []string{"Mariners", "Seattle"}},
		},
	}
	_, err := lookups.SeedFromConfig(nil, seeds)
	return err
}
`)
		}
		return dir
	}

	assert.Equal(t, 3, scoreVision(build(false, false, false)),
		"base fixture should sum to 3.75 raw points → int 3")
	assert.Equal(t, 3, scoreVision(build(true, false, false)),
		"internal/learn/doc.go presence alone must earn nothing — the sentinel is retired")
	assert.Equal(t, 4, scoreVision(build(false, true, false)),
		"registered teach+recall+learnings surface should earn +0.25 → 4.0 → int 4")
	assert.Equal(t, 4, scoreVision(build(false, false, true)),
		"non-empty entity lookup seeds should earn +0.25 → 4.0 → int 4")
	assert.Equal(t, 4, scoreVision(build(true, true, true)),
		"full learn evidence earns +0.5 combined → 4.25 → int 4")
}

func TestScoreVision_LearnLoopGoldenFixtureEarnsBehavioralCredit(t *testing.T) {
	fixture := filepath.Join("..", "..", "testdata", "golden", "expected", "generate-learn-loop-api", "learn-loop-example")
	score := scoreVision(fixture)

	assert.GreaterOrEqual(t, score, 4, "learn-enabled golden fixture registers teach/recall/learnings and stamps seeds, so it should earn the behavioral credit")
}

func TestLearnCommandSurfaceRegistered(t *testing.T) {
	write := func(t *testing.T, rootBody string) string {
		dir := t.TempDir()
		if rootBody != "" {
			writeScorecardFixture(t, dir, "internal/cli/root.go", rootBody)
		}
		return dir
	}

	t.Run("all three learn commands registered", func(t *testing.T) {
		dir := write(t, `package cli
func newRootCmd() {
	rootCmd.AddCommand(newTeachCmd(nil, nil))
	rootCmd.AddCommand(newRecallCmd(nil, nil))
	rootCmd.AddCommand(newLearningsCmd(nil, nil))
}
`)
		assert.True(t, learnCommandSurfaceRegistered(filepath.Join(dir, "internal", "cli")))
	})

	t.Run("missing learnings registration", func(t *testing.T) {
		dir := write(t, `package cli
func newRootCmd() {
	rootCmd.AddCommand(newTeachCmd(nil, nil), newRecallCmd(nil, nil))
}
`)
		assert.False(t, learnCommandSurfaceRegistered(filepath.Join(dir, "internal", "cli")),
			"a partial surface (no learnings) must not earn the registration credit")
	})

	t.Run("constructor names outside AddCommand do not count", func(t *testing.T) {
		dir := write(t, `package cli
// newTeachCmd newRecallCmd newLearningsCmd mentioned in a comment only.
func newRootCmd() {
	rootCmd.AddCommand(newSearchCmd(nil))
}
`)
		assert.False(t, learnCommandSurfaceRegistered(filepath.Join(dir, "internal", "cli")),
			"registration evidence must come from parsed AddCommand calls, not substring mentions")
	})

	t.Run("missing root.go", func(t *testing.T) {
		dir := write(t, "")
		assert.False(t, learnCommandSurfaceRegistered(filepath.Join(dir, "internal", "cli")))
	})
}

func TestLearnEntitySeedsNonEmpty(t *testing.T) {
	t.Run("stamped seed map in learn_init.go", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/cli/learn_init.go", `package cli
import "example.com/project/internal/learn/lookups"
func initLearn(db any) error {
	seeds := map[string][]lookups.SeedConfig{
		"venue": {
			{Canonical: "KALSHI", Values: []string{"kalshi"}},
		},
	}
	_, err := lookups.SeedFromConfig(nil, seeds)
	return err
}
`)
		assert.True(t, learnEntitySeedsNonEmpty(dir))
	})

	t.Run("empty-seed learn_init.go earns nothing", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/cli/learn_init.go", `package cli
func initLearn(db any) error {
	_ = db
	return nil
}
`)
		assert.False(t, learnEntitySeedsNonEmpty(dir))
	})

	t.Run("generic lookups library alone earns nothing", func(t *testing.T) {
		// The emitted lookups library always contains SeedConfig and
		// variable-reference Canonical fields; that must not count as
		// seed data.
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/learn/lookups/seeds.go", `package lookups
type SeedConfig struct {
	Canonical string
	Values    []string
}
func SeedFromConfig(db any, seedsByKind map[string][]SeedConfig) (int, error) {
	rows := make([]LookupRow, 0)
	for kind, seeds := range seedsByKind {
		for _, s := range seeds {
			rows = append(rows, LookupRow{Kind: kind, Canonical: s.Canonical, Value: s.Canonical, Source: "seeded"})
		}
	}
	_ = rows
	return 0, nil
}
`)
		assert.False(t, learnEntitySeedsNonEmpty(dir))
	})

	t.Run("commented seed shape earns nothing", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/cli/learn_init.go", `package cli
// SeedConfig example only: Canonical: "SEA"
func initLearn(db any) error {
	_ = db
	return nil
}
`)
		assert.False(t, learnEntitySeedsNonEmpty(dir))
	})

	t.Run("hand-authored seed table under lookups counts", func(t *testing.T) {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/learn/lookups/seeds.go", `package lookups
var builtinSeeds = map[string][]SeedConfig{
	"team": {
		{Canonical: "SEA", Values: []string{"Mariners"}},
	},
}
`)
		assert.True(t, learnEntitySeedsNonEmpty(dir))
	})

	t.Run("no seed artifacts at all", func(t *testing.T) {
		assert.False(t, learnEntitySeedsNonEmpty(t.TempDir()))
	})
}

// TestScorecardSourcesNeverImportOSExec pins the scorecard's pure-static
// contract: every scorecard credit is computed by scanning emitted source
// content, never by executing printed binaries. Execution proof belongs to
// verify and the acceptance print. Any scorecard source file importing
// os/exec is a contract violation, regardless of what it does with it.
func TestScorecardSourcesNeverImportOSExec(t *testing.T) {
	matches, err := filepath.Glob("scorecard*.go")
	if err != nil {
		t.Fatalf("glob scorecard sources: %v", err)
	}

	checked := 0
	fset := token.NewFileSet()
	for _, m := range matches {
		if strings.HasSuffix(m, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, m, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", m, err)
		}
		for _, imp := range parsed.Imports {
			assert.NotEqual(t, `"os/exec"`, imp.Path.Value,
				"%s must not import os/exec — the scorecard never executes binaries", m)
		}
		checked++
	}
	assert.GreaterOrEqual(t, checked, 2, "expected to check scorecard.go and scorecard_structural.go at minimum")
}

func TestScoreVision_ImportPresenceAddsHalfPoint(t *testing.T) {
	build := func(includeImport bool) string {
		dir := t.TempDir()
		writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli
func newRootCmd() { rootCmd.AddCommand(newSearchCmd(nil), newExportCmd(nil)) }
`)
		writeScorecardFixture(t, dir, "internal/cli/search.go", `package cli
func newSearchCmd(flags any) {}
`)
		writeScorecardFixture(t, dir, "internal/cli/export.go", `package cli
func newExportCmd(flags any) {}
`)
		writeScorecardFixture(t, dir, "internal/store/store.go", `package store
`)
		if includeImport {
			writeScorecardFixture(t, dir, "internal/cli/import.go", `package cli
func newImportCmd(flags any) {}
`)
		}
		return dir
	}

	assert.Equal(t, 3, scoreVision(build(false)))
	assert.Equal(t, 4, scoreVision(build(true)))
}

func TestScoreVision_MaxReachableAndPartialStaysSub10(t *testing.T) {
	build := func(includeLearn, includeFTS5 bool) string {
		dir := t.TempDir()

		learnRegs := ""
		if includeLearn {
			learnRegs = "\n\t\tnewTeachCmd(nil, nil),\n\t\tnewRecallCmd(nil, nil),\n\t\tnewLearningsCmd(nil, nil),"
		}
		writeScorecardFixture(t, dir, "internal/cli/root.go", `package cli
func newRootCmd() {
	rootCmd.AddCommand(
		newSyncCmd(nil),
		newSearchCmd(nil),
		newExportCmd(nil),
		newTailCmd(nil),
		newImportCmd(nil),
		newAnalyticsCmd(nil),`+learnRegs+`
	)
}
`)
		writeScorecardFixture(t, dir, "internal/cli/sync.go", `package cli
func newSyncCmd(flags any) {}
`)
		writeScorecardFixture(t, dir, "internal/cli/search.go", `package cli
import "example.com/project/internal/store"
func newSearchCmd(flags any) {
	_ = store.Open()
}
`)
		writeScorecardFixture(t, dir, "internal/cli/export.go", `package cli
import (
	"encoding/json"
	"example.com/project/internal/store"
)
func newExportCmd(flags any) {
	_ = json.NewEncoder(nil)
	_ = store.Open()
}
`)
		writeScorecardFixture(t, dir, "internal/cli/tail.go", `package cli
func newTailCmd(flags any) {}
`)
		writeScorecardFixture(t, dir, "internal/cli/import.go", `package cli
func newImportCmd(flags any) {}
`)
		writeScorecardFixture(t, dir, "internal/cli/analytics.go", `package cli
func newAnalyticsCmd(flags any) {
	first := c.Get("/one")
	second := c.Get("/two")
	_, _ = first, second
}
`)

		storeBody := `package store
func Open() DB { return DB{} }
type DB struct{}
const schema = `
		if includeFTS5 {
			storeBody += "`CREATE TABLE projects(id INTEGER PRIMARY KEY);\n" +
				"CREATE TABLE datasets(id INTEGER PRIMARY KEY);\n" +
				"CREATE TABLE models(id INTEGER PRIMARY KEY);\n" +
				"CREATE VIRTUAL TABLE docs_fts USING fts5(content);`"
		} else {
			storeBody += "`CREATE TABLE projects(id INTEGER PRIMARY KEY);\n" +
				"CREATE TABLE datasets(id INTEGER PRIMARY KEY);\n" +
				"CREATE TABLE models(id INTEGER PRIMARY KEY);`"
		}
		writeScorecardFixture(t, dir, "internal/store/store.go", storeBody)

		if includeLearn {
			writeScorecardFixture(t, dir, "internal/learn/doc.go", `// Package learn owns the generated recall/teach loop.
package learn
`)
			writeScorecardFixture(t, dir, "internal/cli/learn_init.go", `package cli
import "example.com/project/internal/learn/lookups"
func initLearn(db any) error {
	seeds := map[string][]lookups.SeedConfig{
		"team": {
			{Canonical: "SEA", Values: []string{"Mariners"}},
		},
	}
	_, err := lookups.SeedFromConfig(nil, seeds)
	return err
}
`)
		}
		return dir
	}

	// Before the tier2 schema+wiring cap fix, build(true,true) scored 9 and
	// build(false,false) scored 8. Both sat in the affected (3.0,3.5] band;
	// the full fixture now reaches 10, the partial reaches 9. The learn half
	// point is behavioral now: registered teach/recall/learnings (+0.25) plus
	// stamped non-empty seeds (+0.25).
	assert.Equal(t, 10, scoreVision(build(true, true)))
	assert.Equal(t, 9, scoreVision(build(false, false)))
}
