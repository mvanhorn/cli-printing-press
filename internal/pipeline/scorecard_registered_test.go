package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

const rawSQLiteInventoryCommandFixture = `package cli
import "database/sql"
func newInventoryCmd(flags any) {}
func open() (*sql.DB, error) { return sql.Open("sqlite", "inventory.db") }
`

// TestRegisteredCommandFiles_OrphanIgnored verifies that scoreWorkflows and
// scoreInsight no longer count files whose constructor is never registered in
// root.go. This prevents dead-code removal from dropping the score and ensures
// orphaned command files don't inflate it either.
func TestRegisteredCommandFiles_OrphanIgnored(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// root.go registers only `newStaleCmd`. `newDeadCmd` exists but isn't added.
	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "x"}
	rootCmd.AddCommand(newStaleCmd(nil))
	return rootCmd
}`)

	writeFile(t, filepath.Join(cliDir, "stale.go"), `package cli
import "github.com/spf13/cobra"
func newStaleCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "stale"}
}`)

	writeFile(t, filepath.Join(cliDir, "dead.go"), `package cli
import "github.com/spf13/cobra"
func newDeadCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "dead"}
}`)

	// With only stale.go registered, workflows should count 1 (not 2).
	registered := registeredCommandFiles(cliDir)
	if !registered["stale.go"] {
		t.Errorf("expected stale.go to be registered, got %v", registered)
	}
	if registered["dead.go"] {
		t.Errorf("expected dead.go to NOT be registered (orphan), got %v", registered)
	}
	if registered["root.go"] {
		t.Errorf("root.go itself should not be in registered set (it has no newXxxCmd)")
	}
}

// TestRegisteredCommandFiles_FallsOpenOnMissingRoot verifies graceful handling
// when root.go is missing or unparseable — older CLIs and partial trees must
// still score, not return zero.
func TestRegisteredCommandFiles_FallsOpenOnMissingRoot(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// No root.go at all.
	writeFile(t, filepath.Join(cliDir, "stale.go"), `package cli
func newStaleCmd() {}`)

	registered := registeredCommandFiles(cliDir)
	if len(registered) != 0 {
		t.Errorf("expected empty map when root.go is missing, got %v", registered)
	}
}

func TestRegisteredCommandFiles_FollowsParentGroupAddCommandCalls(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
func newRootCmd() {
	rootCmd.AddCommand(newAvailabilityCmd(nil))
}`)

	writeFile(t, filepath.Join(cliDir, "availability.go"), `package cli
import "github.com/spf13/cobra"
func newAvailabilityCmd(flags any) *cobra.Command {
	cmd := &cobra.Command{Use: "availability"}
	cmd.AddCommand(newAvailabilitySweepCmd(flags))
	return cmd
}`)

	writeFile(t, filepath.Join(cliDir, "availability_sweep.go"), `package cli
import "github.com/spf13/cobra"
func newAvailabilitySweepCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "sweep"}
}`)

	registered := registeredCommandFiles(cliDir)
	if !registered["availability.go"] {
		t.Errorf("expected availability.go parent to be registered, got %v", registered)
	}
	if !registered["availability_sweep.go"] {
		t.Errorf("expected child command registered through parent AddCommand to be registered, got %v", registered)
	}
}

func TestRegisteredCommandFiles_IgnoresConstructorCallsOutsideAddCommand(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
func newRootCmd() {
	rootCmd.AddCommand(newAvailabilityCmd(nil))
}`)

	writeFile(t, filepath.Join(cliDir, "availability.go"), `package cli
import "github.com/spf13/cobra"
func newAvailabilityCmd(flags any) *cobra.Command {
	_ = newAvailabilityGhostCmd(flags)
	return &cobra.Command{Use: "availability"}
}`)

	writeFile(t, filepath.Join(cliDir, "availability_ghost.go"), `package cli
import "github.com/spf13/cobra"
func newAvailabilityGhostCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "ghost"}
}`)

	registered := registeredCommandFiles(cliDir)
	if registered["availability_ghost.go"] {
		t.Errorf("expected non-AddCommand constructor call to stay unregistered, got %v", registered)
	}
}

func TestScoreInsightCountsNovelCommandsRegisteredThroughParentGroupers(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
func newRootCmd() {
	rootCmd.AddCommand(newAvailabilityCmd(nil))
}`)

	writeFile(t, filepath.Join(cliDir, "availability.go"), `package cli
import "github.com/spf13/cobra"
func newAvailabilityCmd(flags any) *cobra.Command {
	cmd := &cobra.Command{Use: "availability"}
	cmd.AddCommand(newAvailabilitySweepCmd(flags))
	cmd.AddCommand(newAvailabilityDriftCmd(flags))
	return cmd
}`)

	writeFile(t, filepath.Join(cliDir, "availability_sweep.go"), `package cli
import "github.com/spf13/cobra"
func newAvailabilitySweepCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "sweep"}
}`)

	writeFile(t, filepath.Join(cliDir, "availability_drift.go"), `package cli
import "github.com/spf13/cobra"
func newAvailabilityDriftCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "drift"}
}`)

	writeFile(t, filepath.Join(cliDir, "availability_ghost.go"), `package cli
import "github.com/spf13/cobra"
func newAvailabilityGhostCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "ghost"}
}`)

	writeFile(t, filepath.Join(dir, CLIManifestFilename), `{
  "cli_name": "demo-pp-cli",
  "novel_features": [
    {"name": "Sweep", "command": "availability sweep", "description": "x"},
    {"name": "Drift", "command": "availability drift", "description": "x"},
    {"name": "Ghost", "command": "availability ghost", "description": "x"}
  ]
}`)

	if score := scoreInsight(dir); score != 4 {
		t.Fatalf("expected two registered parent-grouper insight commands to score 4, got %d", score)
	}
}

// TestScoreWorkflows_IgnoresOrphanFile is the integration-level guard — the
// workflows dimension must not count a dead-code file just because its name
// matches a workflow prefix.
func TestScoreWorkflows_IgnoresOrphanFile(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
import "github.com/spf13/cobra"
func run() {
	rootCmd := &cobra.Command{}
	rootCmd.AddCommand(newStaleCmd(nil))
}`)
	// Registered workflow command
	writeFile(t, filepath.Join(cliDir, "stale.go"), `package cli
import "github.com/spf13/cobra"
func newStaleCmd(flags any) *cobra.Command { return &cobra.Command{} }`)
	// Orphan — filename matches workflow prefix, but not registered
	writeFile(t, filepath.Join(cliDir, "search_query.go"), `package cli
import "github.com/spf13/cobra"
func newSearchQueryCmd(flags any) *cobra.Command { return &cobra.Command{} }`)

	score := scoreWorkflows(dir)
	// Exactly one registered workflow-prefix file → score 2 (per scoreWorkflows
	// rubric: >=1 compound command → 2). The orphan search_query.go must not
	// bump this to 4.
	if score != 2 {
		t.Errorf("expected score=2 (one registered workflow), got %d — orphan likely counted", score)
	}
}

func TestScoreWorkflows_FollowsRegisteredChildCommandFiles(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
func newRootCmd() { rootCmd.AddCommand(newCoinCmd(nil)) }
`)
	writeFile(t, filepath.Join(cliDir, "coin.go"), `package cli
import "github.com/spf13/cobra"
func newCoinCmd(flags any) *cobra.Command {
	cmd := &cobra.Command{Use: "coin"}
	cmd.AddCommand(newBatchCmd(nil))
	return cmd
}
`)
	writeFile(t, filepath.Join(cliDir, "coin_batch.go"), `package cli
import (
	"example.com/project/internal/store"
	"github.com/spf13/cobra"
)
func newBatchCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "batch", RunE: func(cmd *cobra.Command, args []string) error {
		_ = store.Open
		return nil
	}}
}
`)

	if score := scoreWorkflows(dir); score != 2 {
		t.Fatalf("expected registered child workflow command file to count, got %d", score)
	}
}

func TestScoreErrorHandling_UsesReachableSiblingClientPackage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/nonrest\n\ngo 1.24\n")
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "x"}
	rootCmd.AddCommand(newExecuteCmd(nil))
	return rootCmd
}`)
	writeFile(t, filepath.Join(cliDir, "execute.go"), `package cli
import (
	"github.com/spf13/cobra"
	"example.com/nonrest/internal/odoo"
)
func newExecuteCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "execute", RunE: func(cmd *cobra.Command, args []string) error {
		return odoo.Execute()
	}}
}`)
	writeFile(t, filepath.Join(dir, "internal", "odoo", "errors.go"), `package odoo
func Execute() error { return nil }
const help = "Hint: Run doctor"
const typed = "code: auth\ncode: not_found\ncode: rate_limited\n404\n409 already exists\n429 Retry-After retry"
`)

	if score := scoreErrorHandling(dir); score != 10 {
		t.Fatalf("expected reachable sibling package to provide full error score, got %d", score)
	}
}

func TestScoreErrorHandling_UsesDeclaredPackageNameForDefaultImportAlias(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/nonrest\n\ngo 1.24\n")
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "x"}
	rootCmd.AddCommand(newExecuteCmd(nil))
	return rootCmd
}`)
	writeFile(t, filepath.Join(cliDir, "execute.go"), `package cli
import (
	"github.com/spf13/cobra"
	"example.com/nonrest/internal/jsonrpc_client"
)
func newExecuteCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "execute", RunE: func(cmd *cobra.Command, args []string) error {
		return client.Execute()
	}}
}`)
	writeFile(t, filepath.Join(dir, "internal", "jsonrpc_client", "errors.go"), `package client
func Execute() error { return nil }
const help = "Hint: Run doctor"
const typed = "code: auth\ncode: not_found\ncode: rate_limited\n404\n409 already exists\n429 Retry-After retry"
`)

	if score := scoreErrorHandling(dir); score != 10 {
		t.Fatalf("expected declared package name alias to reach sibling package, got %d", score)
	}
}

func TestScoreErrorHandling_IgnoresUnreachableDeadClientPackage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/nonrest\n\ngo 1.24\n")
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "x"}
	rootCmd.AddCommand(newExecuteCmd(nil))
	return rootCmd
}`)
	writeFile(t, filepath.Join(cliDir, "execute.go"), `package cli
import "github.com/spf13/cobra"
func newExecuteCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "execute", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
}`)
	writeFile(t, filepath.Join(dir, "internal", "client", "client.go"), `package client
const dead = "Hint: Run doctor\ncode: auth\ncode: not_found\ncode: rate_limited\n404\n409 already exists\n429 Retry-After retry"
`)

	if score := scoreErrorHandling(dir); score != 0 {
		t.Fatalf("expected unreachable dead client package to score 0, got %d", score)
	}
}

func TestScoreErrorHandling_DoesNotPairSignalsAcrossReachableFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/nonrest\n\ngo 1.24\n")
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "x"}
	rootCmd.AddCommand(newExecuteCmd(nil))
	return rootCmd
}`)
	writeFile(t, filepath.Join(cliDir, "execute.go"), `package cli
import (
	"github.com/spf13/cobra"
	"example.com/nonrest/internal/limits"
	"example.com/nonrest/internal/retry"
)
func newExecuteCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "execute", RunE: func(cmd *cobra.Command, args []string) error {
		limits.Mark()
		retry.Mark()
		return nil
	}}
}`)
	writeFile(t, filepath.Join(dir, "internal", "limits", "limits.go"), `package limits
func Mark() string { return "429" }
`)
	writeFile(t, filepath.Join(dir, "internal", "retry", "retry.go"), `package retry
func Mark() string { return "Retry-After" }
`)

	if score := scoreErrorHandling(dir); score != 0 {
		t.Fatalf("expected split rate-limit signals not to score, got %d", score)
	}
}

func TestScoreErrorHandling_DoesNotTreatDoctorCommandAsActionableHint(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "x"}
	rootCmd.AddCommand(newDoctorCmd(nil))
	return rootCmd
}`)
	writeFile(t, filepath.Join(cliDir, "doctor.go"), `package cli
import "github.com/spf13/cobra"
func newDoctorCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "doctor", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
}`)

	if score := scoreErrorHandling(dir); score != 0 {
		t.Fatalf("expected doctor command registration not to score as an actionable hint, got %d", score)
	}
}

func TestScoreErrorHandling_IgnoresOrphanCLIImportedSiblingPackage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/nonrest\n\ngo 1.24\n")
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "x"}
	rootCmd.AddCommand(newLiveCmd(nil))
	return rootCmd
}`)
	writeFile(t, filepath.Join(cliDir, "live.go"), `package cli
import "github.com/spf13/cobra"
func newLiveCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "live", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
}`)
	writeFile(t, filepath.Join(cliDir, "dead.go"), `package cli
import "example.com/nonrest/internal/odoo"
func newDeadCmd(flags any) { _ = odoo.Execute }
`)
	writeFile(t, filepath.Join(dir, "internal", "odoo", "errors.go"), `package odoo
func Execute() error { return nil }
const help = "Hint: Run doctor"
const typed = "code: auth\ncode: not_found\ncode: rate_limited\n404\n409 already exists\n429 Retry-After retry"
`)

	if score := scoreErrorHandling(dir); score != 0 {
		t.Fatalf("expected orphan command import not to inflate error score, got %d", score)
	}
}

func TestScoreErrorHandling_IgnoresDeadFileInImportedSiblingPackage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/nonrest\n\ngo 1.24\n")
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "x"}
	rootCmd.AddCommand(newExecuteCmd(nil))
	return rootCmd
}`)
	writeFile(t, filepath.Join(cliDir, "execute.go"), `package cli
import (
	"github.com/spf13/cobra"
	"example.com/nonrest/internal/odoo"
)
func newExecuteCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "execute", RunE: func(cmd *cobra.Command, args []string) error {
		return odoo.Execute()
	}}
}`)
	writeFile(t, filepath.Join(dir, "internal", "odoo", "execute.go"), `package odoo
func Execute() error { return nil }
`)
	writeFile(t, filepath.Join(dir, "internal", "odoo", "dead_errors.go"), `package odoo
const dead = "Hint: Run doctor\ncode: auth\ncode: not_found\ncode: rate_limited\n404\n409 already exists\n429 Retry-After retry"
`)

	if score := scoreErrorHandling(dir); score != 0 {
		t.Fatalf("expected dead sibling package file not to inflate error score, got %d", score)
	}
}

func TestScoreOutputModes_UsesReachableSiblingFormattingPackage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/nonrest\n\ngo 1.24\n")
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "x"}
	_ = "json"
	_ = "plain"
	_ = "select"
	_ = "csv"
	_ = "quiet"
	rootCmd.AddCommand(newFormatCmd(nil))
	return rootCmd
}`)
	writeFile(t, filepath.Join(cliDir, "format.go"), `package cli
import (
	"github.com/spf13/cobra"
	"example.com/nonrest/internal/odoo"
)
func newFormatCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "format", RunE: func(cmd *cobra.Command, args []string) error {
		return odoo.Render()
	}}
}`)
	writeFile(t, filepath.Join(dir, "internal", "odoo", "format.go"), `package odoo
import (
	"encoding/json"
	"text/tabwriter"
)
func Render() error {
	_ = tabwriter.NewWriter(nil, 0, 0, 0, ' ', 0)
	var v any
	_ = json.Unmarshal(nil, &v)
	return nil
}
func filterFields(v any) any { return v }
`)

	if score := scoreOutputModes(dir); score != 9 {
		t.Fatalf("expected reachable sibling formatting package to score 9, got %d", score)
	}
}

func TestScoreOutputModes_MergesSymbolsFromMultipleImporters(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/nonrest\n\ngo 1.24\n")
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "x"}
	_ = "json"
	_ = "plain"
	_ = "select"
	_ = "csv"
	_ = "quiet"
	rootCmd.AddCommand(newExecuteCmd(nil), newFormatCmd(nil))
	return rootCmd
}`)
	writeFile(t, filepath.Join(cliDir, "execute.go"), `package cli
import (
	"github.com/spf13/cobra"
	"example.com/nonrest/internal/odoo"
)
func newExecuteCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "execute", RunE: func(cmd *cobra.Command, args []string) error {
		odoo.Execute()
		return nil
	}}
}`)
	writeFile(t, filepath.Join(cliDir, "format.go"), `package cli
import (
	"github.com/spf13/cobra"
	"example.com/nonrest/internal/odoo"
)
func newFormatCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "format", RunE: func(cmd *cobra.Command, args []string) error {
		odoo.Render()
		return nil
	}}
}`)
	writeFile(t, filepath.Join(dir, "internal", "odoo", "execute.go"), `package odoo
func Execute() {}
`)
	writeFile(t, filepath.Join(dir, "internal", "odoo", "render.go"), `package odoo
import "text/tabwriter"
func Render() {
	_ = tabwriter.NewWriter(nil, 0, 0, 0, ' ', 0)
}
`)

	if score := scoreOutputModes(dir); score != 7 {
		t.Fatalf("expected symbols from both odoo importers to contribute to output score, got %d", score)
	}
}

func TestScoreOutputModes_ReprocessesPackageWhenLaterSymbolsArrive(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/nonrest\n\ngo 1.24\n")
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "x"}
	_ = "json"
	_ = "plain"
	_ = "select"
	_ = "csv"
	_ = "quiet"
	rootCmd.AddCommand(newBFirstCmd(nil), newZLaterCmd(nil))
	return rootCmd
}`)
	writeFile(t, filepath.Join(cliDir, "b_first.go"), `package cli
import (
	"github.com/spf13/cobra"
	"example.com/nonrest/internal/bpkg"
)
func newBFirstCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "b", RunE: func(cmd *cobra.Command, args []string) error {
		bpkg.List()
		return nil
	}}
}`)
	writeFile(t, filepath.Join(cliDir, "z_later.go"), `package cli
import (
	"github.com/spf13/cobra"
	"example.com/nonrest/internal/apkg"
)
func newZLaterCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "z", RunE: func(cmd *cobra.Command, args []string) error {
		apkg.Start()
		return nil
	}}
}`)
	writeFile(t, filepath.Join(dir, "internal", "apkg", "start.go"), `package apkg
import "example.com/nonrest/internal/bpkg"
func Start() { bpkg.Render() }
`)
	writeFile(t, filepath.Join(dir, "internal", "bpkg", "list.go"), `package bpkg
func List() {}
`)
	writeFile(t, filepath.Join(dir, "internal", "bpkg", "render.go"), `package bpkg
import "text/tabwriter"
func Render() {
	_ = tabwriter.NewWriter(nil, 0, 0, 0, ' ', 0)
}
`)

	if score := scoreOutputModes(dir); score != 7 {
		t.Fatalf("expected later bpkg.Render symbol to reprocess package and score tabwriter, got %d", score)
	}
}

func TestScoreOutputModes_UsesReachableSiblingPageProgressStructure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/nonrest\n\ngo 1.24\n")
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "x"}
	_ = "json"
	_ = "plain"
	_ = "select"
	_ = "csv"
	_ = "quiet"
	rootCmd.AddCommand(newProgressCmd(nil))
	return rootCmd
}`)
	writeFile(t, filepath.Join(cliDir, "progress.go"), `package cli
import (
	"github.com/spf13/cobra"
	"example.com/nonrest/internal/odoo"
)
func newProgressCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "progress", RunE: func(cmd *cobra.Command, args []string) error {
		odoo.Render()
		return nil
	}}
}`)
	writeFile(t, filepath.Join(dir, "internal", "odoo", "render.go"), `package odoo
import "fmt"
func Render() {
	for page := 1; page < 2; page++ {
		fmt.Printf("page %d", page)
	}
}
`)

	if score := scoreOutputModes(dir); score != 6 {
		t.Fatalf("expected reachable sibling page-progress loop to contribute to output score, got %d", score)
	}
}

func TestScoreTerminalUX_RecognizesXTermTTYDetection(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
func init() { _ = "no-color" }
`)
	writeFile(t, filepath.Join(cliDir, "helpers.go"), `package cli
import "golang.org/x/term"
func isTerminal(fd int) bool { return term.IsTerminal(fd) }
const noColor = "NO_COLOR"
`)

	if score := scoreTerminalUX(dir); score != 3 {
		t.Fatalf("expected NO_COLOR, no-color flag, and x/term TTY detection to score 3, got %d", score)
	}
}

func TestScoreTerminalUX_UsesReachableSiblingFormattingPackage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/nonrest\n\ngo 1.24\n")
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
import "github.com/spf13/cobra"
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{Use: "x"}
	_ = "no-color"
	rootCmd.AddCommand(newFormatCmd(nil))
	return rootCmd
}`)
	writeFile(t, filepath.Join(cliDir, "format.go"), `package cli
import (
	"github.com/spf13/cobra"
	"example.com/nonrest/internal/formatting"
)
func newFormatCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "format", RunE: func(cmd *cobra.Command, args []string) error {
		formatting.Render()
		return nil
	}}
}`)
	writeFile(t, filepath.Join(dir, "internal", "formatting", "format.go"), `package formatting
import (
	"text/tabwriter"
	"golang.org/x/term"
)
func Render() {
	_ = tabwriter.NewWriter(nil, 0, 0, 0, ' ', 0)
	_ = term.IsTerminal(1)
}
`)

	if score := scoreTerminalUX(dir); score != 4 {
		t.Fatalf("expected reachable sibling TTY and tabwriter signals to score 4, got %d", score)
	}
}

func TestScoreVision_RecognizesRenamedRegisteredSyncCommand(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "internal", "store"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
func newRootCmd() { rootCmd.AddCommand(newSyncCmd(nil), newSearchCmd(nil), newExportCmd(nil)) }
`)
	writeFile(t, filepath.Join(cliDir, "sync_bluray.go"), `package cli
func newSyncCmd(flags any) {}
`)
	writeFile(t, filepath.Join(cliDir, "search.go"), `package cli
func newSearchCmd(flags any) {}
`)
	writeFile(t, filepath.Join(cliDir, "export.go"), `package cli
func newExportCmd(flags any) {}
`)
	writeFile(t, filepath.Join(dir, "internal", "store", "store.go"), `package store
`)

	if score := scoreVision(dir); score != 4 {
		t.Fatalf("expected renamed registered sync command to contribute to vision score, got %d", score)
	}
}

func TestScoreMCPQuality_RecognizesRenamedRegisteredSyncCommand(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "internal", "mcp"), 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
func newRootCmd() { rootCmd.AddCommand(newSyncCmd(nil)) }
`)
	writeFile(t, filepath.Join(cliDir, "sync_bluray.go"), `package cli
func newSyncCmd(flags any) {}
`)
	writeFile(t, filepath.Join(dir, "internal", "mcp", "tools.go"), `package mcp
func RegisterTools() {
	_ = "context"
	handleContext()
	_ = "sql"
	handleSQL()
	cobratree.RegisterAll(nil)
}
func handleContext() {}
func handleSQL() {}
`)

	if score := scoreMCPQuality(dir); score != 7 {
		t.Fatalf("expected runtime mirror plus renamed sync command to score high-level sync credit, got %d", score)
	}
}

func TestScoreMCPQuality_LocalDatastoreSearchRequiresHandlerOrRegisteredCommand(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "internal", "mcp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteCLIManifest(dir, CLIManifest{
		SchemaVersion: 1,
		APIName:       "fixture",
		CLIName:       "fixture-pp-cli",
		AuthType:      "none",
		SpecFormat:    "sqlite",
	}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "internal", "mcp", "tools.go"), `package mcp
func RegisterTools() {
	_ = "context"
	handleContext()
	_ = "search"
	_ = "sync"
	handleSync()
}
func handleContext() {}
func handleSync() {}
`)

	if score := scoreMCPQuality(dir); score != 6 {
		t.Fatalf("expected bare search string not to score as a high-level MCP tool, got %d", score)
	}
}

func TestScoreWorkflows_RecognizesRawSQLiteDataLayer(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
func newRootCmd() { rootCmd.AddCommand(newInventoryCmd(nil)) }
	`)
	writeFile(t, filepath.Join(cliDir, "inventory.go"), rawSQLiteInventoryCommandFixture)

	if score := scoreWorkflows(dir); score != 2 {
		t.Fatalf("expected raw SQLite-backed command to count as workflow, got %d", score)
	}
}

func TestScoreInsight_UsesEveryCobraUseLiteralAndRawSQLiteDataLayer(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, CLIManifestFilename), `{"novel_features":[{"name":"Check watchlist","command":"watch check","description":"Check watchlist drift"}]}`)

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
func newRootCmd() { rootCmd.AddCommand(newWatchCmd(nil), newInventoryCmd(nil)) }
	`)
	writeFile(t, filepath.Join(cliDir, "watch.go"), `package cli
import "github.com/spf13/cobra"
func newWatchCmd(flags any) *cobra.Command {
	cmd := &cobra.Command{Use: "watch"}
	cmd.AddCommand(&cobra.Command{Use: "check"})
	return cmd
}
`)
	writeFile(t, filepath.Join(cliDir, "inventory.go"), rawSQLiteInventoryCommandFixture+`const q = "SELECT COUNT(*) FROM discs GROUP BY format"
`)

	if score := scoreInsight(dir); score != 4 {
		t.Fatalf("expected nested Cobra Use and raw SQLite aggregation to count as two insights, got %d", score)
	}
}

func TestScoreInsight_FollowsRegisteredChildCommandFiles(t *testing.T) {
	dir := t.TempDir()
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, CLIManifestFilename), `{"novel_features":[{"name":"Check watchlist","command":"watch check","description":"Check watchlist drift"}]}`)

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
func newRootCmd() { rootCmd.AddCommand(newWatchCmd(nil)) }
`)
	writeFile(t, filepath.Join(cliDir, "watch.go"), `package cli
import "github.com/spf13/cobra"
func newWatchCmd(flags any) *cobra.Command {
	cmd := &cobra.Command{Use: "watch"}
	cmd.AddCommand(newCheckCmd(nil))
	return cmd
}
`)
	writeFile(t, filepath.Join(cliDir, "check.go"), `package cli
import (
	"database/sql"
	"github.com/spf13/cobra"
)
func newCheckCmd(flags any) *cobra.Command {
	return &cobra.Command{Use: "check"}
}
func open() (*sql.DB, error) { return sql.Open("sqlite", "catalog.db") }
const q = "SELECT COUNT(*) FROM discs GROUP BY format"
`)

	if score := scoreInsight(dir); score != 2 {
		t.Fatalf("expected registered child command file to count as insight, got %d", score)
	}
}

func TestScoreSyncCorrectness_UsesReachableSiblingPackage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/nonrest\n\ngo 1.24\n")
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
func newRootCmd() { rootCmd.AddCommand(newSyncCmd(nil)) }
`)
	writeFile(t, filepath.Join(cliDir, "sync.go"), `package cli
import "example.com/nonrest/internal/odoo"
func newSyncCmd(flags any) { _ = odoo.Sync }
`)
	writeFile(t, filepath.Join(dir, "internal", "odoo", "sync.go"), `package odoo
var defaultSyncResources = []string{"records"}
func GetSyncState() string { return "" }
func SaveSyncState(s string) {}
func Sync() {
	cursor := "/{record_id}"
	for {
		Query(cursor)
		SaveSyncState(cursor)
		break
	}
}
func Query(cursor string) {}
`)

	if score := scoreSyncCorrectness(dir); score != 10 {
		t.Fatalf("expected reachable sibling sync implementation to score 10, got %d", score)
	}
}

func TestScoreSyncCorrectness_IgnoresOrphanCLIImportedSiblingPackage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/nonrest\n\ngo 1.24\n")
	cliDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeFile(t, filepath.Join(cliDir, "root.go"), `package cli
func newRootCmd() { rootCmd.AddCommand(newLiveCmd(nil)) }
`)
	writeFile(t, filepath.Join(cliDir, "live.go"), `package cli
func newLiveCmd(flags any) {}
`)
	writeFile(t, filepath.Join(cliDir, "dead_sync.go"), `package cli
import "example.com/nonrest/internal/odoo"
func newDeadSyncCmd(flags any) { _ = odoo.Sync }
`)
	writeFile(t, filepath.Join(dir, "internal", "odoo", "sync.go"), `package odoo
var defaultSyncResources = []string{"records"}
func GetSyncState() string { return "" }
func SaveSyncState(s string) {}
func Sync() {
	cursor := "/{record_id}"
	for {
		Query(cursor)
		SaveSyncState(cursor)
		break
	}
}
func Query(cursor string) {}
`)

	if score := scoreSyncCorrectness(dir); score != 0 {
		t.Fatalf("expected orphan sync command import not to inflate score, got %d", score)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
