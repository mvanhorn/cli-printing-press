package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedReimplementationFixture writes a minimal generated-CLI directory
// layout at root: internal/cli/<file>.go for each named command, and a
// research.json at pipelineDir listing the novel features. Returns
// (cliDir, pipelineDir) for passing into checkReimplementation.
func seedReimplementationFixture(t *testing.T, files map[string]string, novel []NovelFeature) (string, string) {
	t.Helper()

	root := t.TempDir()
	cliFilesDir := filepath.Join(root, "internal", "cli")
	if err := os.MkdirAll(cliFilesDir, 0o755); err != nil {
		t.Fatalf("mkdir cli: %v", err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(cliFilesDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	pipelineDir := filepath.Join(root, "pipeline")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatalf("mkdir pipeline: %v", err)
	}
	research := ResearchResult{NovelFeatures: novel}
	data, err := json.MarshalIndent(research, "", "  ")
	if err != nil {
		t.Fatalf("marshal research: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pipelineDir, "research.json"), data, 0o644); err != nil {
		t.Fatalf("write research.json: %v", err)
	}
	return root, pipelineDir
}

// Happy path: a novel-feature command that calls the generated client
// and transforms its response passes both the kill check and the dogfood
// scan. Nothing is flagged.
func TestCheckReimplementation_CallsClient_Passes(t *testing.T) {
	files := map[string]string{
		"digest.go": `package cli

import "github.com/spf13/cobra"

func newDigestCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "digest",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := flags.newClient()
			if err != nil { return err }
			_ = c
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Digest", Command: "digest"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Skipped {
		t.Fatalf("expected non-skipped result, got Skipped=true")
	}
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if len(got.Suspicious) != 0 {
		t.Fatalf("Suspicious: want 0, got %d (%v)", len(got.Suspicious), got.Suspicious)
	}
}

func TestCheckReimplementation_DataSourceStrategyAnnotation(t *testing.T) {
	files := map[string]string{
		"today.go": `package cli

import (
	"github.com/spf13/cobra"
	"example.com/mod/internal/store"
)

// pp:data-source local
func newTodayCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "today",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := store.Open("x.db")
			return err
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Today", Command: "today"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if len(got.MissingDataSourceStrategy) != 0 {
		t.Fatalf("MissingDataSourceStrategy: want 0, got %d (%v)", len(got.MissingDataSourceStrategy), got.MissingDataSourceStrategy)
	}
}

func TestCheckReimplementation_ComputedDataSourceWithoutClient_Passes(t *testing.T) {
	files := map[string]string{
		"irmaa.go": `package cli

import "github.com/spf13/cobra"

// pp:data-source computed
func newIRMAACmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "irmaa",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("tier: standard")
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "IRMAA", Command: "irmaa"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if len(got.MissingDataSourceStrategy) != 0 {
		t.Fatalf("MissingDataSourceStrategy: want 0, got %d (%v)", len(got.MissingDataSourceStrategy), got.MissingDataSourceStrategy)
	}
	if len(got.Suspicious) != 0 {
		t.Fatalf("Suspicious: want 0, got %d (%v)", len(got.Suspicious), got.Suspicious)
	}
}

func TestCheckReimplementation_ComputedDataSourceTODOStillFlagged(t *testing.T) {
	files := map[string]string{
		"irmaa.go": `package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// pp:data-source computed
func newIRMAACmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "irmaa",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("TODO: implement novel feature %q", "irmaa")
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "IRMAA", Command: "irmaa"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if len(got.MissingDataSourceStrategy) != 0 {
		t.Fatalf("MissingDataSourceStrategy: want 0, got %d (%v)", len(got.MissingDataSourceStrategy), got.MissingDataSourceStrategy)
	}
	if len(got.Suspicious) != 1 {
		t.Fatalf("Suspicious: want 1, got %d (%v)", len(got.Suspicious), got.Suspicious)
	}
	if !strings.Contains(got.Suspicious[0].Reason, "TODO") && !strings.Contains(got.Suspicious[0].Reason, "stub") {
		t.Fatalf("Reason should mention TODO/stub, got %q", got.Suspicious[0].Reason)
	}
}

func TestCheckReimplementation_ComputedDataSourceTODOInSiblingFileStillFlagged(t *testing.T) {
	files := map[string]string{
		"irmaa.go": `package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// pp:data-source computed
func newIRMAACmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "irmaa",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("TODO: implement novel feature %q", "irmaa")
		},
	}
}
`,
		"irmaa_helpers.go": `package cli

// pp:data-source computed
func irmaaBracket() string {
	return "standard"
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "IRMAA", Command: "irmaa"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if len(got.MissingDataSourceStrategy) != 0 {
		t.Fatalf("MissingDataSourceStrategy: want 0, got %d (%v)", len(got.MissingDataSourceStrategy), got.MissingDataSourceStrategy)
	}
	if len(got.Suspicious) != 1 {
		t.Fatalf("Suspicious: want 1, got %d (%v)", len(got.Suspicious), got.Suspicious)
	}
	if !strings.Contains(got.Suspicious[0].Reason, "TODO") && !strings.Contains(got.Suspicious[0].Reason, "stub") {
		t.Fatalf("Reason should mention TODO/stub, got %q", got.Suspicious[0].Reason)
	}
}

func TestCheckReimplementation_MissingDataSourceStrategy_Flagged(t *testing.T) {
	files := map[string]string{
		"today.go": `package cli

import (
	"github.com/spf13/cobra"
	"example.com/mod/internal/store"
)

func newTodayCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "today",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := store.Open("x.db")
			return err
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Today", Command: "today"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if len(got.MissingDataSourceStrategy) != 1 {
		t.Fatalf("MissingDataSourceStrategy: want 1, got %d (%v)", len(got.MissingDataSourceStrategy), got.MissingDataSourceStrategy)
	}
	if got.MissingDataSourceStrategy[0].Command != "today" {
		t.Fatalf("Command: want today, got %q", got.MissingDataSourceStrategy[0].Command)
	}
	if !strings.Contains(got.MissingDataSourceStrategy[0].Reason, "pp:data-source") {
		t.Fatalf("Reason should mention pp:data-source: %q", got.MissingDataSourceStrategy[0].Reason)
	}
}

func TestCheckReimplementation_GeneratedCommandDoesNotNeedDataSourceStrategy(t *testing.T) {
	files := map[string]string{
		"items_list.go": `// Generated by CLI Printing Press (https://github.com/mvanhorn/cli-printing-press). DO NOT EDIT.
package cli

import "github.com/spf13/cobra"

func newItemsListCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "list",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := flags.newClient()
			if err != nil { return err }
			_ = c
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Items", Command: "items list"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if len(got.MissingDataSourceStrategy) != 0 {
		t.Fatalf("MissingDataSourceStrategy: want 0, got %d (%v)", len(got.MissingDataSourceStrategy), got.MissingDataSourceStrategy)
	}
}

// Happy path (exempt): a SQLite-derived command that calls store.Open
// but never the client is treated as a local-data command and exempted.
// This is the carve-out that keeps stale/bottleneck/health legitimate.
func TestCheckReimplementation_StoreOnly_Exempted(t *testing.T) {
	files := map[string]string{
		"bottleneck.go": `package cli

import (
	"github.com/spf13/cobra"

	"example.com/mod/internal/store"
)

func newBottleneckCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "bottleneck",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := store.Open("x.db")
			_ = db
			_ = err
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Bottleneck", Command: "bottleneck"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if got.ExemptedViaStore != 1 {
		t.Fatalf("ExemptedViaStore: want 1, got %d", got.ExemptedViaStore)
	}
	if len(got.Suspicious) != 0 {
		t.Fatalf("Suspicious: want 0, got %d", len(got.Suspicious))
	}
}

func TestCheckReimplementation_StoreHelperHop_Exempted(t *testing.T) {
	files := map[string]string{
		"helpers.go": `package cli

import "example.com/mod/internal/store"

func openStore(path string) (*store.Store, error) {
	return store.Open(path)
}
`,
		"trend.go": `package cli

import "github.com/spf13/cobra"

func newTrendCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "trend",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := openStore("x.db")
			if err != nil { return err }
			_ = db
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Trend", Command: "trend"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if got.ExemptedViaStore != 1 {
		t.Fatalf("ExemptedViaStore: want 1, got %d", got.ExemptedViaStore)
	}
	if len(got.Suspicious) != 0 {
		t.Fatalf("Suspicious: want 0, got %d", len(got.Suspicious))
	}
}

// TestCheckReimplementation_RawDatabaseSQL_Exempted covers the carve-out
// for novel commands that bypass the generated store package and operate
// on the printed CLI's local SQLite file directly through database/sql.
// Reading the same local data through a thinner surface still counts as
// a legitimate local-data signal.
func TestCheckReimplementation_RawDatabaseSQL_Exempted(t *testing.T) {
	files := map[string]string{
		"sqlquery.go": `package cli

import (
	"database/sql"

	"github.com/spf13/cobra"
)

func newSQLQueryCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "sqlquery",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := sql.Open("sqlite", "x.db")
			if err != nil {
				return err
			}
			defer db.Close()
			rows, err := db.Query("SELECT 1")
			if err != nil {
				return err
			}
			defer rows.Close()
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "SQLQuery", Command: "sqlquery"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if got.ExemptedViaStore != 1 {
		t.Fatalf("ExemptedViaStore: want 1 (raw database/sql counts as local-data signal), got %d", got.ExemptedViaStore)
	}
	if len(got.Suspicious) != 0 {
		t.Fatalf("Suspicious: want 0, got %d", len(got.Suspicious))
	}
}

// TestCheckReimplementation_DatabaseSQLImportOnly_Flagged pins the
// negative: an unrelated import of database/sql without a sql.Open call
// is not enough to claim a local-data signal. The file still gets
// flagged as a hand-rolled response.
func TestCheckReimplementation_DatabaseSQLImportOnly_Flagged(t *testing.T) {
	files := map[string]string{
		"fake.go": `package cli

import (
	"database/sql"

	"github.com/spf13/cobra"
)

var _ = sql.ErrNoRows

func newFakeCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "fake",
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.Println("hardcoded response")
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Fake", Command: "fake"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if got.ExemptedViaStore != 0 {
		t.Fatalf("ExemptedViaStore: want 0 (import alone is not enough), got %d", got.ExemptedViaStore)
	}
	if len(got.Suspicious) != 1 {
		t.Fatalf("Suspicious: want 1, got %d", len(got.Suspicious))
	}
}

func TestCheckReimplementation_FormatterInStoreFile_NotExempted(t *testing.T) {
	files := map[string]string{
		"types.go": `package cli

import (
	"strings"

	"example.com/mod/internal/store"
)

func openStore(path string) (*store.Store, error) {
	return store.Open(path)
}

func formatTitle(title string) string {
	return strings.TrimSpace(title)
}
`,
		"trend.go": `package cli

import "github.com/spf13/cobra"

func newTrendCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "trend",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = formatTitle("static")
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Trend", Command: "trend"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if got.ExemptedViaStore != 0 {
		t.Fatalf("ExemptedViaStore: want 0, got %d", got.ExemptedViaStore)
	}
	if len(got.Suspicious) != 1 {
		t.Fatalf("Suspicious: want 1, got %d", len(got.Suspicious))
	}
	if got.Suspicious[0].Command != "trend" {
		t.Fatalf("Command: want trend, got %s", got.Suspicious[0].Command)
	}
}

// Error path: a novel-feature command body that returns a constant
// string with no client calls is flagged with "hand-rolled response."
func TestCheckReimplementation_ConstantString_Flagged(t *testing.T) {
	files := map[string]string{
		"fake.go": `package cli

import "github.com/spf13/cobra"

func newFakeCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "fake",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = "OK"
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Fake", Command: "fake"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if len(got.Suspicious) != 1 {
		t.Fatalf("Suspicious: want 1, got %d", len(got.Suspicious))
	}
	f := got.Suspicious[0]
	if f.Command != "fake" {
		t.Errorf("Command: want fake, got %s", f.Command)
	}
	if !strings.Contains(f.Reason, "hand-rolled response") && !strings.Contains(f.Reason, "empty body") {
		t.Errorf("Reason should mention hand-rolled response or empty body: %q", f.Reason)
	}
}

// Error path: a novel-feature command whose handler returns only a
// hardcoded struct literal with no client/store signals is flagged.
// The check cannot know the literal matches a schema, but the absence
// of any data-source call is enough to surface it for review.
func TestCheckReimplementation_HardcodedStructLiteral_Flagged(t *testing.T) {
	files := map[string]string{
		"ghost.go": `package cli

import (
	"encoding/json"
	"os"

	"github.com/spf13/cobra"
)

func newGhostCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "ghost",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := map[string]any{"id": "42", "status": "ok"}
			return json.NewEncoder(os.Stdout).Encode(out)
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Ghost", Command: "ghost"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if len(got.Suspicious) != 1 {
		t.Fatalf("Suspicious: want 1, got %d", len(got.Suspicious))
	}
	if got.Suspicious[0].Command != "ghost" {
		t.Errorf("Command: want ghost, got %s", got.Suspicious[0].Command)
	}
}

// Edge case: a novel-feature command that calls BOTH the API client AND
// the store passes the check. The store signal is sufficient on its own,
// but a command that caches API responses locally should not be penalized.
func TestCheckReimplementation_ClientAndStore_Exempted(t *testing.T) {
	files := map[string]string{
		"sync.go": `package cli

import (
	"github.com/spf13/cobra"

	"example.com/mod/internal/store"
)

func newSyncCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "sync",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := flags.newClient()
			if err != nil { return err }
			_ = c
			db, err := store.Open("x.db")
			_ = db
			_ = err
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Sync", Command: "sync"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.ExemptedViaStore != 1 {
		t.Errorf("ExemptedViaStore: want 1, got %d", got.ExemptedViaStore)
	}
	if len(got.Suspicious) != 0 {
		t.Errorf("Suspicious: want 0, got %d", len(got.Suspicious))
	}
}

// Edge case: an empty RunE body is flagged with the distinct "empty
// body" reason. This is the classic agent-wired-but-unimplemented
// failure mode; surfacing it with its own reason makes the fix
// obvious to the reviewer.
func TestCheckReimplementation_EmptyBody_FlaggedWithDistinctReason(t *testing.T) {
	files := map[string]string{
		"stub.go": `package cli

import "github.com/spf13/cobra"

func newStubCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "stub",
		RunE: func(cmd *cobra.Command, args []string) error { return nil },
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Stub", Command: "stub"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if len(got.Suspicious) != 1 {
		t.Fatalf("Suspicious: want 1, got %d", len(got.Suspicious))
	}
	if !strings.Contains(got.Suspicious[0].Reason, "empty body") {
		t.Errorf("Reason should mention empty body: %q", got.Suspicious[0].Reason)
	}
}

// Integration: running the check on a fixture with one compliant and
// one non-compliant novel-feature command produces a report that names
// only the non-compliant one.
func TestCheckReimplementation_MixedFixture_ReportsOnlyOffender(t *testing.T) {
	files := map[string]string{
		"real.go": `package cli

import "github.com/spf13/cobra"

func newRealCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "real",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := flags.newClient()
			if err != nil { return err }
			_ = c
			return nil
		},
	}
}
`,
		"fake.go": `package cli

import "github.com/spf13/cobra"

func newFakeCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "fake",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Real", Command: "real"},
		{Name: "Fake", Command: "fake"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 2 {
		t.Fatalf("Checked: want 2, got %d", got.Checked)
	}
	if len(got.Suspicious) != 1 {
		t.Fatalf("Suspicious: want 1, got %d (%v)", len(got.Suspicious), got.Suspicious)
	}
	if got.Suspicious[0].Command != "fake" {
		t.Errorf("Offender: want fake, got %s", got.Suspicious[0].Command)
	}
}

// TestCheckReimplementation_BacktickUse pins that the file index recognizes
// commands declared with Go's backtick raw-string Use: form. Authors reach
// for backticks when the command name contains a literal double-quote
// (e.g., `query <project> "<sql>"`); without backtick support the file
// drops out of leafToFiles and the reimplementation check silently skips
// the command entirely.
func TestCheckReimplementation_BacktickUse(t *testing.T) {
	files := map[string]string{
		"query.go": "package cli\n" +
			"\n" +
			"import \"github.com/spf13/cobra\"\n" +
			"\n" +
			"func newQueryCmd(flags *rootFlags) *cobra.Command {\n" +
			"\treturn &cobra.Command{\n" +
			"\t\tUse: `query <project> \"<sql>\"`,\n" +
			"\t\tRunE: func(cmd *cobra.Command, args []string) error {\n" +
			"\t\t\tc, err := flags.newClient()\n" +
			"\t\t\tif err != nil { return err }\n" +
			"\t\t\t_ = c\n" +
			"\t\t\treturn nil\n" +
			"\t\t},\n" +
			"\t}\n" +
			"}\n",
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "SQL query", Command: "query"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Skipped {
		t.Fatalf("expected non-skipped result, got Skipped=true (backtick Use: not indexed)")
	}
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if len(got.Suspicious) != 0 {
		t.Fatalf("Suspicious: want 0, got %d (%v)", len(got.Suspicious), got.Suspicious)
	}
}

// Skip path: when research.json is missing the check returns Skipped
// rather than crashing.
func TestCheckReimplementation_NoResearchDir_Skipped(t *testing.T) {
	got := checkReimplementation(t.TempDir(), "")
	if !got.Skipped {
		t.Errorf("expected Skipped=true, got %#v", got)
	}
}

// Skip path: an empty research.json (no novel features) returns
// Skipped. Nothing planned means nothing to validate.
func TestCheckReimplementation_NoNovelFeatures_Skipped(t *testing.T) {
	root := t.TempDir()
	pipelineDir := filepath.Join(root, "pipeline")
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pipelineDir, "research.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := checkReimplementation(root, pipelineDir)
	if !got.Skipped {
		t.Errorf("expected Skipped=true, got %#v", got)
	}
}

// TestCheckReimplementation_NovelStaticReferenceMarker_Exempted is the
// regression guard for retro #301 finding F3: a novel feature that
// intentionally ships curated static data (substitution tables, holiday
// lists, currency metadata) has no API client call and no store call,
// because the data IS the feature. Before the F3 fix, dogfood flagged
// these as "hand-rolled response: no API client call, no store access"
// even when they were the kind of feature explicitly approved during
// Phase 1.5. The `// pp:novel-static-reference` marker in the file
// header opts the command out of the reimplementation check.
func TestCheckReimplementation_NovelStaticReferenceMarker_Exempted(t *testing.T) {
	files := map[string]string{
		"sub.go": `package cli

// pp:novel-static-reference
//
// Substitution lookups are a curated static-data feature; the data is
// shipped as a hardcoded table with no API or store backing.

import "github.com/spf13/cobra"

var subTable = map[string][]string{
	"buttermilk": {"milk + lemon juice", "milk + vinegar", "yogurt"},
	"eggs":       {"flax meal + water", "applesauce"},
}

func newSubCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "sub",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = subTable
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Substitution lookup", Command: "sub"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if got.ExemptedViaAnnotation != 1 {
		t.Fatalf("ExemptedViaAnnotation: want 1 (marker should exempt), got %d", got.ExemptedViaAnnotation)
	}
	if got.ExemptedViaClientDirective != 0 {
		t.Fatalf("ExemptedViaClientDirective: want 0 (static marker is distinct), got %d", got.ExemptedViaClientDirective)
	}
	if got.ExemptedViaStore != 0 {
		t.Errorf("ExemptedViaStore: want 0 (annotation is its own carve-out, not store), got %d", got.ExemptedViaStore)
	}
	if len(got.Suspicious) != 0 {
		t.Fatalf("Suspicious: want 0, got %d (%v)", len(got.Suspicious), got.Suspicious)
	}
}

func TestCheckReimplementation_ClientCallMarker(t *testing.T) {
	tests := []struct {
		name           string
		marker         string
		wantDirective  int
		wantSuspicious int
	}{
		{
			name: "marker exempts hidden client wrapper",
			marker: `// pp:client-call
//
// fetchFlights wraps the real API client through a helper shape the
// reimplementation regex cannot see.

`,
			wantDirective: 1,
		},
		{
			name:           "missing marker still flags hidden wrapper",
			wantSuspicious: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string]string{
				"flights.go": `package cli

` + tt.marker + `import "github.com/spf13/cobra"

func newFlightsCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "flights",
		RunE: func(cmd *cobra.Command, args []string) error {
			flights, err := fetchFlights(args[0])
			if err != nil { return err }
			_ = flights
			return nil
		},
	}
}
`,
			}
			cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
				{Name: "Flights", Command: "flights"},
			})

			got := checkReimplementation(cliDir, pipelineDir)
			if got.Checked != 1 {
				t.Fatalf("Checked: want 1, got %d", got.Checked)
			}
			if got.ExemptedViaClientDirective != tt.wantDirective {
				t.Fatalf("ExemptedViaClientDirective: want %d, got %d", tt.wantDirective, got.ExemptedViaClientDirective)
			}
			if got.ExemptedViaAnnotation != 0 {
				t.Fatalf("ExemptedViaAnnotation: want 0 (client-call marker is distinct), got %d", got.ExemptedViaAnnotation)
			}
			if got.ExemptedViaStore != 0 {
				t.Fatalf("ExemptedViaStore: want 0 (client-call marker is not a store signal), got %d", got.ExemptedViaStore)
			}
			if len(got.Suspicious) != tt.wantSuspicious {
				t.Fatalf("Suspicious: want %d, got %d (%v)", tt.wantSuspicious, len(got.Suspicious), got.Suspicious)
			}
			if tt.wantSuspicious > 0 && !strings.Contains(got.Suspicious[0].Reason, "no API client call") {
				t.Errorf("expected hand-rolled-response reason, got %q", got.Suspicious[0].Reason)
			}
		})
	}
}

func TestCheckReimplementation_ClientHelperHop_Passes(t *testing.T) {
	files := map[string]string{
		"novel_helpers.go": `package cli

import "example.com/mod/internal/rappi"

func fetchRestaurantListPage(city, category string) ([]rappi.RestaurantListItem, error) {
	c := rappi.NewClient()
	return c.FetchHTML(city, category)
}
`,
		"restaurants_top.go": `package cli

import "github.com/spf13/cobra"

func newRestaurantsTopCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "restaurants-top",
		RunE: func(cmd *cobra.Command, args []string) error {
			rows, err := fetchRestaurantListPage("mx", "pizza")
			if err != nil { return err }
			_ = rows
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Restaurants top", Command: "restaurants-top"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if got.ExemptedViaClientDirective != 0 {
		t.Fatalf("ExemptedViaClientDirective: want 0 (no marker needed), got %d", got.ExemptedViaClientDirective)
	}
	if len(got.Suspicious) != 0 {
		t.Fatalf("Suspicious: want 0, got %d (%v)", len(got.Suspicious), got.Suspicious)
	}
}

func TestCheckReimplementation_CommentLedStoreHelperHop_Passes(t *testing.T) {
	files := map[string]string{
		"analytics_helpers.go": `package cli

import "example.com/mod/internal/store"

func openStoreForRead() (*store.Store, error) {
	return store.Open("analytics.db")
}
`,
		"analytics.go": `// Copyright 2026 Example and contributors.
package cli

import "github.com/spf13/cobra"

// pp:data-source local
func newRevenueCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "revenue",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStoreForRead()
			if err != nil { return err }
			_ = s
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Revenue", Command: "revenue"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if got.ExemptedViaStore != 1 {
		t.Fatalf("ExemptedViaStore: want 1 (comment-led handler calls store helper), got %d", got.ExemptedViaStore)
	}
	if len(got.Suspicious) != 0 {
		t.Fatalf("Suspicious: want 0, got %d (%v)", len(got.Suspicious), got.Suspicious)
	}
}

func TestCheckReimplementation_HardcodedHelperHop_Flagged(t *testing.T) {
	files := map[string]string{
		"novel_helpers.go": `package cli

type RestaurantListItem struct {
	Name string
}

func fetchRestaurantListPage(city, category string) ([]RestaurantListItem, error) {
	return []RestaurantListItem{{Name: "Hardcoded"}}, nil
}
`,
		"restaurants_top.go": `package cli

import "github.com/spf13/cobra"

func newRestaurantsTopCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "restaurants-top",
		RunE: func(cmd *cobra.Command, args []string) error {
			rows, err := fetchRestaurantListPage("mx", "pizza")
			if err != nil { return err }
			_ = rows
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Restaurants top", Command: "restaurants-top"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if got.ExemptedViaClientDirective != 0 {
		t.Fatalf("ExemptedViaClientDirective: want 0, got %d", got.ExemptedViaClientDirective)
	}
	if len(got.Suspicious) != 1 {
		t.Fatalf("Suspicious: want 1, got %d (%v)", len(got.Suspicious), got.Suspicious)
	}
	if got.Suspicious[0].Command != "restaurants-top" {
		t.Fatalf("Command: want restaurants-top, got %s", got.Suspicious[0].Command)
	}
}

func TestCheckReimplementation_OutboundHTTPHelperHop_Passes(t *testing.T) {
	files := map[string]string{
		"novel_helpers.go": `package cli

import "net/http"

func fetchRestaurantListPage(city, category string) (*http.Response, error) {
	return http.Get("https://example.test/restaurants")
}
`,
		"restaurants_top.go": `package cli

import "github.com/spf13/cobra"

func newRestaurantsTopCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "restaurants-top",
		RunE: func(cmd *cobra.Command, args []string) error {
			resp, err := fetchRestaurantListPage("mx", "pizza")
			if err != nil { return err }
			_ = resp
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Restaurants top", Command: "restaurants-top"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if len(got.Suspicious) != 0 {
		t.Fatalf("Suspicious: want 0, got %d (%v)", len(got.Suspicious), got.Suspicious)
	}
}

func TestCheckReimplementation_TwoHopClientHelper_Flagged(t *testing.T) {
	files := map[string]string{
		"novel_helpers.go": `package cli

import "example.com/mod/internal/rappi"

func fetchRestaurantListPage(city, category string) ([]rappi.RestaurantListItem, error) {
	return requestRestaurantListPage(city, category)
}

func requestRestaurantListPage(city, category string) ([]rappi.RestaurantListItem, error) {
	c := rappi.NewClient()
	return c.FetchHTML(city, category)
}
`,
		"restaurants_top.go": `package cli

import "github.com/spf13/cobra"

func newRestaurantsTopCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "restaurants-top",
		RunE: func(cmd *cobra.Command, args []string) error {
			rows, err := fetchRestaurantListPage("mx", "pizza")
			if err != nil { return err }
			_ = rows
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Restaurants top", Command: "restaurants-top"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if len(got.Suspicious) != 1 {
		t.Fatalf("Suspicious: want 1 (deeper helper chains need pp:client-call), got %d (%v)", len(got.Suspicious), got.Suspicious)
	}
	if got.Suspicious[0].Command != "restaurants-top" {
		t.Fatalf("Command: want restaurants-top, got %s", got.Suspicious[0].Command)
	}
}

func TestCheckReimplementation_SameFileTwoHopClientHelper_Flagged(t *testing.T) {
	files := map[string]string{
		"restaurants_top.go": `package cli

import (
	"example.com/mod/internal/rappi"
	"github.com/spf13/cobra"
)

func fetchRestaurantListPage(city, category string) ([]rappi.RestaurantListItem, error) {
	return requestRestaurantListPage(city, category)
}

func requestRestaurantListPage(city, category string) ([]rappi.RestaurantListItem, error) {
	c := rappi.NewClient()
	return c.FetchHTML(city, category)
}

func newRestaurantsTopCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "restaurants-top",
		RunE: func(cmd *cobra.Command, args []string) error {
			rows, err := fetchRestaurantListPage("mx", "pizza")
			if err != nil { return err }
			_ = rows
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Restaurants top", Command: "restaurants-top"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if len(got.Suspicious) != 1 {
		t.Fatalf("Suspicious: want 1 (same-file deeper helper chains need pp:client-call), got %d (%v)", len(got.Suspicious), got.Suspicious)
	}
}

func TestCheckReimplementation_CommentedClientCallInHelper_Flagged(t *testing.T) {
	files := map[string]string{
		"novel_helpers.go": `package cli

type RestaurantListItem struct {
	Name string
}

func fetchRestaurantListPage(city, category string) ([]RestaurantListItem, error) {
	// TODO: replace the hardcoded data with http.Get("https://example.test/restaurants")
	return []RestaurantListItem{{Name: "Hardcoded"}}, nil
}
`,
		"restaurants_top.go": `package cli

import "github.com/spf13/cobra"

func newRestaurantsTopCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "restaurants-top",
		RunE: func(cmd *cobra.Command, args []string) error {
			rows, err := fetchRestaurantListPage("mx", "pizza")
			if err != nil { return err }
			_ = rows
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Restaurants top", Command: "restaurants-top"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if len(got.Suspicious) != 1 {
		t.Fatalf("Suspicious: want 1, got %d (%v)", len(got.Suspicious), got.Suspicious)
	}
}

func TestCheckReimplementation_SiblingInternalUtilityHelper_Flagged(t *testing.T) {
	files := map[string]string{
		"novel_helpers.go": `package cli

import "example.com/mod/internal/fixtures"

func fetchRestaurantListPage(city, category string) ([]fixtures.RestaurantListItem, error) {
	return fixtures.TopRestaurants(), nil
}
`,
		"restaurants_top.go": `package cli

import "github.com/spf13/cobra"

func newRestaurantsTopCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "restaurants-top",
		RunE: func(cmd *cobra.Command, args []string) error {
			rows, err := fetchRestaurantListPage("mx", "pizza")
			if err != nil { return err }
			_ = rows
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Restaurants top", Command: "restaurants-top"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if len(got.Suspicious) != 1 {
		t.Fatalf("Suspicious: want 1, got %d (%v)", len(got.Suspicious), got.Suspicious)
	}
}

func TestCheckReimplementation_DirectSiblingInternalClient_Passes(t *testing.T) {
	files := map[string]string{
		"search.go": `package cli

import (
	"example.com/mod/internal/algolia"
	"github.com/spf13/cobra"
)

func newSearchCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "search",
		RunE: func(cmd *cobra.Command, args []string) error {
			ac := algolia.New(flags.timeout)
			rows, err := ac.Search(cmd.Context(), args[0])
			if err != nil { return err }
			_ = rows
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Search", Command: "search"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if len(got.Suspicious) != 0 {
		t.Fatalf("Suspicious: want 0, got %d (%v)", len(got.Suspicious), got.Suspicious)
	}
}

func TestCheckReimplementation_RunEPriorityOverRun_Passes(t *testing.T) {
	files := map[string]string{
		"search.go": `package cli

import (
	"example.com/mod/internal/algolia"
	"github.com/spf13/cobra"
)

func newSearchCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "search",
		RunE: func(cmd *cobra.Command, args []string) error {
			ac := algolia.New(flags.timeout)
			rows, err := ac.Search(cmd.Context(), args[0])
			if err != nil { return err }
			_ = rows
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Println(` + "`" + `{"status":"cached"}` + "`" + `)
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Search", Command: "search"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if len(got.Suspicious) != 0 {
		t.Fatalf("Suspicious: want 0, got %d (%v)", len(got.Suspicious), got.Suspicious)
	}
}

func TestCheckReimplementation_IgnoresNonCobraCommandLikeLiteral_Passes(t *testing.T) {
	files := map[string]string{
		"search.go": `package cli

import (
	"example.com/mod/internal/algolia"
	"github.com/spf13/cobra"
)

type Command struct {
	Use string
	RunE func(*cobra.Command, []string) error
}

var misleadingExample = Command{
	Use: "search",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.Println(` + "`" + `{"status":"example"}` + "`" + `)
		return nil
	},
}

func newSearchCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "search",
		RunE: func(cmd *cobra.Command, args []string) error {
			ac := algolia.New(flags.timeout)
			rows, err := ac.Search(cmd.Context(), args[0])
			if err != nil { return err }
			_ = rows
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Search", Command: "search"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if len(got.Suspicious) != 0 {
		t.Fatalf("Suspicious: want 0, got %d (%v)", len(got.Suspicious), got.Suspicious)
	}
}

// TestCheckReimplementation_LearnHelperHop_Exempted confirms that a
// novel-feature handler routing its query through the generator-owned
// internal/learn package (which delegates to internal/store for the
// actual SQL work) is not flagged as reimplementation. The handler
// calls learn.Recall against the local store; the file imports both
// internal/learn (reserved generator namespace) and internal/store, so
// the existing store carve-out covers the read path - no new exemption
// class needed. Pins the contract behind reserving "learn" in
// reservedInternalPackages so callers do not regress to flagging
// learn-routed handlers.
func TestCheckReimplementation_LearnHelperHop_Exempted(t *testing.T) {
	files := map[string]string{
		"recall.go": `package cli

import (
	"github.com/spf13/cobra"

	"example.com/mod/internal/learn"
	"example.com/mod/internal/store"
)

func newRecallCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "recall",
		RunE: func(cmd *cobra.Command, args []string) error {
			db, err := store.Open("x.db")
			if err != nil { return err }
			hit, err := learn.Recall(cmd.Context(), db, args[0], learn.Opts{})
			if err != nil { return err }
			_ = hit
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Recall", Command: "recall"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if got.ExemptedViaStore != 1 {
		t.Fatalf("ExemptedViaStore: want 1 (learn helper hop reads from internal/store), got %d", got.ExemptedViaStore)
	}
	if len(got.Suspicious) != 0 {
		t.Fatalf("Suspicious: want 0, got %d (%v)", len(got.Suspicious), got.Suspicious)
	}
}

// TestCheckReimplementation_WithoutMarker_StillFlagged confirms the
// F3 fix doesn't silently exempt commands that lack the explicit
// `// pp:novel-static-reference` marker. Same shape as the test above
// but without the comment — must still be flagged.
func TestCheckReimplementation_WithoutMarker_StillFlagged(t *testing.T) {
	files := map[string]string{
		"sub.go": `package cli

import "github.com/spf13/cobra"

var subTable = map[string][]string{
	"buttermilk": {"milk + lemon juice"},
}

func newSubCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "sub",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = subTable
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Substitution lookup", Command: "sub"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if got.ExemptedViaAnnotation != 0 {
		t.Fatalf("ExemptedViaAnnotation: want 0 (no marker), got %d", got.ExemptedViaAnnotation)
	}
	if got.ExemptedViaStore != 0 {
		t.Fatalf("ExemptedViaStore: want 0 (no store signal either), got %d", got.ExemptedViaStore)
	}
	if len(got.Suspicious) != 1 {
		t.Fatalf("Suspicious: want 1, got %d", len(got.Suspicious))
	}
	if !strings.Contains(got.Suspicious[0].Reason, "no API client call") {
		t.Errorf("expected hand-rolled-response reason, got %q", got.Suspicious[0].Reason)
	}
}

// TestCheckReimplementation_LearnRecallWithClient_Passes confirms a handler
// that mixes the generator-emitted learn package (recall/teach loop) with a
// real client call passes the dogfood reimplementation check. The realistic
// shape: a "lookup" command consults learn.Recall first, falls through to the
// API client when the cache misses, and writes back via learn.Teach.
//
// This is the canonical agent-authored novel-feature shape once the
// self-learning loop ships. The check must not flag learn.Recall+learn.Teach
// as a hand-rolled response; the client call is the legitimate signal, and
// learn is generator-owned (reserved namespace in U2).
func TestCheckReimplementation_LearnRecallWithClient_Passes(t *testing.T) {
	files := map[string]string{
		"lookup.go": `package cli

import (
	"example.com/mod/internal/learn"
	"github.com/spf13/cobra"
)

func newLookupCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use: "lookup",
		RunE: func(cmd *cobra.Command, args []string) error {
			if hit := learn.Recall(args[0]); hit != "" {
				cmd.Println(hit)
				return nil
			}
			c, err := flags.newClient()
			if err != nil { return err }
			_ = c
			_ = learn.Teach(args[0], "answer")
			return nil
		},
	}
}
`,
	}
	cliDir, pipelineDir := seedReimplementationFixture(t, files, []NovelFeature{
		{Name: "Lookup", Command: "lookup"},
	})

	got := checkReimplementation(cliDir, pipelineDir)
	if got.Checked != 1 {
		t.Fatalf("Checked: want 1, got %d", got.Checked)
	}
	if len(got.Suspicious) != 0 {
		t.Fatalf("Suspicious: want 0 (learn.Recall + client call should pass), got %d (%v)", len(got.Suspicious), got.Suspicious)
	}
}
