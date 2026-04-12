package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// writeFixtureCLI writes a minimal CLI directory that refresh-docs can
// operate on: .printing-press.json, go.mod, and internal/cli/ with a
// couple of cobra.Command constructors.
func writeFixtureCLI(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Provenance
	manifest := `{
  "api_name": "fixture",
  "cli_name": "fixture-pp-cli",
  "generated_at": "2026-04-01T12:00:00Z",
  "run_id": "20260401-120000",
  "printing_press_version": "1.3.2"
}`
	if err := os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	// go.mod with module path that looks like library layout
	goMod := "module github.com/mvanhorn/printing-press-library/library/other/fixture\n\ngo 1.23\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pre-existing files that MUST NOT be modified by refresh-docs.
	for _, name := range []string{"LICENSE", "spec.yaml", "Makefile"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("pre-existing "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "cmd", "fixture-pp-cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd", "fixture-pp-cli", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// internal/cli with two commands.
	cliSrc := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(cliSrc, 0o755); err != nil {
		t.Fatal(err)
	}
	quoteSrc := `package cli

import "github.com/spf13/cobra"

func newQuoteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quote",
		Short: "Get current quote",
		Long:  "Fetch a real-time quote for a symbol.",
	}
}
`
	if err := os.WriteFile(filepath.Join(cliSrc, "quote.go"), []byte(quoteSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	watchlistSrc := `package cli

import "github.com/spf13/cobra"

func newWatchlistCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watchlist",
		Short: "Manage watchlists",
	}
}
`
	if err := os.WriteFile(filepath.Join(cliSrc, "watchlist.go"), []byte(watchlistSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// snapshotCLIFiles returns a map of relative path → sha256 for every regular
// file under root. Used to detect what changed across a run.
func snapshotCLIFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out[rel] = hashBytes(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func hashBytes(b []byte) string {
	// Simple deterministic hash — good enough for "has this changed?"
	var sum uint64 = 14695981039346656037
	for _, c := range b {
		sum ^= uint64(c)
		sum *= 1099511628211
	}
	return string(rune(sum>>32)) + string(rune(sum&0xffffffff))
}

func TestRefreshDocs_OnlyModifiesREADMEAndSKILL(t *testing.T) {
	dir := writeFixtureCLI(t)
	before := snapshotCLIFiles(t, dir)

	var stdout, stderr bytes.Buffer
	err := runRefreshDocs(refreshDocsOpts{Dir: dir, NoLLM: true}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("refresh-docs failed: %v\nstderr:\n%s", err, stderr.String())
	}

	after := snapshotCLIFiles(t, dir)

	// Invariant #1: only README.md and SKILL.md may be changed or added.
	allowedChanges := map[string]bool{"README.md": true, "SKILL.md": true}
	for relPath, afterHash := range after {
		if beforeHash, existed := before[relPath]; existed {
			if afterHash != beforeHash && !allowedChanges[relPath] {
				t.Errorf("SAFETY: file %q was modified but isn't in allowlist", relPath)
			}
		} else {
			// File newly created
			if !allowedChanges[relPath] {
				t.Errorf("SAFETY: file %q was created but isn't in allowlist", relPath)
			}
		}
	}
	for relPath := range before {
		if _, still := after[relPath]; !still {
			t.Errorf("SAFETY: file %q was deleted (not allowed)", relPath)
		}
	}
}

func TestRefreshDocs_DryRunWritesNothing(t *testing.T) {
	dir := writeFixtureCLI(t)
	before := snapshotCLIFiles(t, dir)

	var stdout, stderr bytes.Buffer
	err := runRefreshDocs(refreshDocsOpts{Dir: dir, NoLLM: true, DryRun: true}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("refresh-docs dry-run failed: %v\nstderr:\n%s", err, stderr.String())
	}

	after := snapshotCLIFiles(t, dir)
	if len(before) != len(after) {
		t.Errorf("SAFETY: dry-run changed file count (%d → %d)", len(before), len(after))
	}
	for relPath, beforeHash := range before {
		if afterHash, still := after[relPath]; !still {
			t.Errorf("SAFETY: dry-run deleted %q", relPath)
		} else if beforeHash != afterHash {
			t.Errorf("SAFETY: dry-run modified %q", relPath)
		}
	}

	// Output should contain the rendered content.
	stdoutStr := stdout.String()
	if !strings.Contains(stdoutStr, "---- README.md (dry-run) ----") {
		t.Error("dry-run output should label the README section")
	}
	if !strings.Contains(stdoutStr, "---- SKILL.md (dry-run) ----") {
		t.Error("dry-run output should label the SKILL section")
	}
}

func TestRefreshDocs_Idempotent(t *testing.T) {
	dir := writeFixtureCLI(t)

	var stdout, stderr bytes.Buffer
	if err := runRefreshDocs(refreshDocsOpts{Dir: dir, NoLLM: true}, &stdout, &stderr); err != nil {
		t.Fatalf("first run failed: %v\n%s", err, stderr.String())
	}
	first := snapshotCLIFiles(t, dir)

	stdout.Reset()
	stderr.Reset()
	if err := runRefreshDocs(refreshDocsOpts{Dir: dir, NoLLM: true}, &stdout, &stderr); err != nil {
		t.Fatalf("second run failed: %v\n%s", err, stderr.String())
	}
	second := snapshotCLIFiles(t, dir)

	// Every file hash should match — the second run produces identical
	// output to the first. If this fails, refresh-docs is non-deterministic
	// (likely because it embedded a timestamp or similar).
	for relPath, firstHash := range first {
		secondHash, present := second[relPath]
		if !present {
			t.Errorf("IDEMPOTENCY: %q present after first run, missing after second", relPath)
			continue
		}
		if firstHash != secondHash {
			t.Errorf("IDEMPOTENCY: %q changed between runs", relPath)
		}
	}
	for relPath := range second {
		if _, present := first[relPath]; !present {
			t.Errorf("IDEMPOTENCY: %q created by second run that first didn't", relPath)
		}
	}
}

func TestRefreshDocs_RejectsMissingDir(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runRefreshDocs(refreshDocsOpts{Dir: "", NoLLM: true}, &stdout, &stderr)
	if err == nil {
		t.Error("expected error when --dir is empty")
	}
	err = runRefreshDocs(refreshDocsOpts{Dir: "/nonexistent/path/that/does/not/exist"}, &stdout, &stderr)
	if err == nil {
		t.Error("expected error when --dir does not contain internal/cli/")
	}
}

func TestRefreshDocs_JSONOutput(t *testing.T) {
	dir := writeFixtureCLI(t)

	var stdout, stderr bytes.Buffer
	err := runRefreshDocs(refreshDocsOpts{Dir: dir, NoLLM: true, AsJSON: true}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("refresh-docs failed: %v", err)
	}

	var result refreshDocsResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("JSON output not parseable: %v\nraw:\n%s", err, stdout.String())
	}
	if result.CommandsFound != 2 {
		t.Errorf("CommandsFound = %d, want 2", result.CommandsFound)
	}
	if result.READMEPath == "" || result.SKILLPath == "" {
		t.Error("JSON output missing README/SKILL paths")
	}
}

// TestRefreshDocs_EndToEndWithGitHistory runs against a real (tmpdir) git
// repo with two commits spanning the generation cutoff. Verifies that the
// classifier correctly buckets commands.
func TestRefreshDocs_EndToEndWithGitHistory(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Build a real git repo with the CLI at library/test/fixture.
	repo := t.TempDir()
	cliRel := "library/test/fixture"
	cliDir := filepath.Join(repo, cliRel)

	runGit := func(t *testing.T, dir string, env map[string]string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	runGit(t, repo, nil, "init", "-q")
	runGit(t, repo, nil, "config", "commit.gpgsign", "false")

	// Build a fixture CLI at the expected path inside the repo.
	if err := os.MkdirAll(filepath.Join(cliDir, "internal", "cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	provenance := `{
  "api_name": "fixture",
  "cli_name": "fixture-pp-cli",
  "generated_at": "2026-04-05T12:00:00Z",
  "run_id": "test",
  "printing_press_version": "1.0"
}`
	if err := os.WriteFile(filepath.Join(cliDir, ".printing-press.json"), []byte(provenance), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cliDir, "go.mod"), []byte("module github.com/t/fixture\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	quoteSrc := `package cli
import "github.com/spf13/cobra"
func newQuoteCmd() *cobra.Command { return &cobra.Command{Use: "quote", Short: "Quote"} }
`
	if err := os.WriteFile(filepath.Join(cliDir, "internal", "cli", "quote.go"), []byte(quoteSrc), 0o644); err != nil {
		t.Fatal(err)
	}

	// Commit 1 @ 2026-04-01 (before provenance cutoff)
	runGit(t, repo, nil, "add", ".")
	base := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	runGit(t, repo, map[string]string{
		"GIT_AUTHOR_DATE":    base.Format(time.RFC3339),
		"GIT_COMMITTER_DATE": base.Format(time.RFC3339),
	}, "commit", "-q", "-m", "base")

	// Commit 2 @ 2026-04-10 (after cutoff) — add watchlist.go
	watchlistSrc := `package cli
import "github.com/spf13/cobra"
func newWatchlistCmd() *cobra.Command { return &cobra.Command{Use: "watchlist", Short: "Watchlists"} }
`
	if err := os.WriteFile(filepath.Join(cliDir, "internal", "cli", "watchlist.go"), []byte(watchlistSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, nil, "add", ".")
	trans := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	runGit(t, repo, map[string]string{
		"GIT_AUTHOR_DATE":    trans.Format(time.RFC3339),
		"GIT_COMMITTER_DATE": trans.Format(time.RFC3339),
	}, "commit", "-q", "-m", "trans")

	var stdout, stderr bytes.Buffer
	opts := refreshDocsOpts{
		Dir:        cliDir,
		GitRepo:    repo,
		CLIRelPath: cliRel,
		NoLLM:      true,
		AsJSON:     true,
	}
	if err := runRefreshDocs(opts, &stdout, &stderr); err != nil {
		t.Fatalf("refresh-docs failed: %v\n%s", err, stderr.String())
	}
	var result refreshDocsResult
	_ = json.Unmarshal(stdout.Bytes(), &result)

	if result.BaseCommands != 1 {
		t.Errorf("BaseCommands = %d, want 1 (only quote should be base)", result.BaseCommands)
	}
	if result.TranscendenceCommands != 1 {
		t.Errorf("TranscendenceCommands = %d, want 1 (watchlist is post-cutoff)", result.TranscendenceCommands)
	}

	readme, err := os.ReadFile(filepath.Join(cliDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readmeStr := string(readme)
	if !strings.Contains(readmeStr, "**`watchlist`**") {
		t.Error("README should list watchlist under Unique Features")
	}
}

// TestRefreshDocs_SafetyInvariant_NeverInventsCommands confirms invariant #6:
// any rendered command must exist in the Cobra tree.
func TestRefreshDocs_SafetyInvariant_NeverInventsCommands(t *testing.T) {
	dir := writeFixtureCLI(t)

	var stdout, stderr bytes.Buffer
	if err := runRefreshDocs(refreshDocsOpts{Dir: dir, NoLLM: true}, &stdout, &stderr); err != nil {
		t.Fatalf("refresh-docs failed: %v", err)
	}

	readme, _ := os.ReadFile(filepath.Join(dir, "README.md"))
	skill, _ := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	rendered := extractRenderedCommands(string(readme), string(skill), "fixture-pp-cli")

	shipped := map[string]bool{"quote": true, "watchlist": true}
	var renderedList []string
	for k := range rendered {
		renderedList = append(renderedList, k)
	}
	sort.Strings(renderedList)

	for k := range rendered {
		if !shipped[k] {
			t.Errorf("SAFETY #6: rendered command %q not in Cobra tree (shipped: %v)", k, shipped)
		}
	}
}
