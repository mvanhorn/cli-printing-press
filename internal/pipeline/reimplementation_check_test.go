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
		"types.go": `package cli

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
