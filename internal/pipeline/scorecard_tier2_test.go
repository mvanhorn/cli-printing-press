package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

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
`)

		assert.Equal(t, 10, scoreSyncCorrectness(dir))
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

		assert.Equal(t, 0, scoreTypeFidelity(dir))
	})

	t.Run("scores string id flags required markers and clear descriptions high", func(t *testing.T) {
		dir := t.TempDir()

		writeScorecardFixture(t, dir, "internal/cli/messages.go", `
package cli

func init() {
	cmd := messagesCmd
	cmd.Flags().StringVar(&flagAfterID, "after-id", "", "Snowflake ID to fetch results after the given message")
	cmd.Flags().StringVar(&flagChannelID, "channel-id", "", "Channel ID containing the messages to fetch for sync")
	cmd.Flags().StringVar(&flagGuildID, "guild-id", "", "Guild ID used to scope channel and message syncing")
	_ = cmd.MarkFlagRequired("after-id")
	_ = cmd.MarkFlagRequired("channel-id")
	_ = cmd.MarkFlagRequired("guild-id")
}
`)

		assert.GreaterOrEqual(t, scoreTypeFidelity(dir), 4)
	})
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
		assert.ElementsMatch(t, []string{"mcp_token_efficiency", "mcp_remote_transport", "mcp_tool_design", "mcp_surface_strategy", "cache_freshness", "path_validity", "auth_protocol", "live_api_verification"}, sc.UnscoredDimensions)
		assert.NotContains(t, sc.GapReport, "path_validity scored 0/10 - needs improvement")
		assert.NotContains(t, sc.GapReport, "auth_protocol scored 0/10 - needs improvement")
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
		expectedDelta := (sharedTier2Raw * 50 / 40) - (sharedTier2Raw * 50 / 50)
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
		assert.True(t, strings.Contains(body, `"unscored_dimensions":["mcp_token_efficiency","mcp_remote_transport","mcp_tool_design","mcp_surface_strategy","cache_freshness","path_validity","auth_protocol","live_api_verification"]`))
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

	t.Run("schema version + doctor + auto-refresh + share scores 10", func(t *testing.T) {
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
		writeScorecardFixture(t, dir, "internal/share/share.go", `package share

func Export() {}`)
		writeScorecardFixture(t, dir, "internal/cli/share_commands.go", `package cli

func newShareCmd() {}`)

		score, scored := scoreCacheFreshness(dir)
		assert.True(t, scored)
		assert.Equal(t, 10, score)
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
