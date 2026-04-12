package docparse

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadProvenance_Present(t *testing.T) {
	dir := t.TempDir()
	manifest := `{
  "api_name": "yahoo-finance",
  "cli_name": "yahoo-finance-pp-cli",
  "generated_at": "2026-04-01T12:00:00Z",
  "run_id": "20260401-120000",
  "printing_press_version": "1.2.1"
}`
	if err := os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	p, err := LoadProvenance(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p == nil {
		t.Fatal("expected non-nil provenance")
	}
	if p.APIName != "yahoo-finance" {
		t.Errorf("APIName = %q", p.APIName)
	}
	want, _ := time.Parse(time.RFC3339, "2026-04-01T12:00:00Z")
	if !p.GeneratedAt.Equal(want) {
		t.Errorf("GeneratedAt = %v, want %v", p.GeneratedAt, want)
	}
}

func TestLoadProvenance_Absent(t *testing.T) {
	p, err := LoadProvenance(t.TempDir())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if p != nil {
		t.Error("expected nil provenance when file absent")
	}
}

func TestClassify_NoGitRepo(t *testing.T) {
	cmds := []Command{
		{Use: "quote", File: "/tmp/quote.go"},
		{Use: "portfolio", File: "/tmp/portfolio.go"},
	}
	classified, err := Classify(cmds, ClassifyOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if len(classified) != 2 {
		t.Fatalf("want 2, got %d", len(classified))
	}
	for _, c := range classified {
		if c.IsTranscendence {
			t.Errorf("%s: expected IsTranscendence=false with no git/provenance", c.Command.Use)
		}
		if !c.AddedAt.IsZero() {
			t.Errorf("%s: expected zero AddedAt with no git repo", c.Command.Use)
		}
	}
}

// initGitRepo builds a tiny real repo with two commits: one at baseline (base
// commands), one later (transcendence). Tests use this for end-to-end classification.
func initGitRepo(t *testing.T) (repo string, baseTime, transTime time.Time) {
	t.Helper()
	repo = t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "commit.gpgsign", "false")

	baseTime = time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	transTime = time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)

	// Commit 1: base file
	cliDir := filepath.Join(repo, "library", "commerce", "yahoo-finance", "internal", "cli")
	if err := os.MkdirAll(cliDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cliDir, "quote.go"), []byte("package cli\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "library/commerce/yahoo-finance/internal/cli/quote.go")
	cmd := exec.Command("git", "-C", repo, "commit", "-q", "-m", "base",
		"--date", baseTime.Format(time.RFC3339))
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
		"GIT_AUTHOR_DATE="+baseTime.Format(time.RFC3339),
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com",
		"GIT_COMMITTER_DATE="+baseTime.Format(time.RFC3339),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit base: %v\n%s", err, out)
	}

	// Commit 2: transcendence file
	if err := os.WriteFile(filepath.Join(cliDir, "watchlist.go"), []byte("package cli\n"), 0644); err != nil {
		t.Fatal(err)
	}
	run("add", "library/commerce/yahoo-finance/internal/cli/watchlist.go")
	cmd = exec.Command("git", "-C", repo, "commit", "-q", "-m", "trans")
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=t@t.com",
		"GIT_AUTHOR_DATE="+transTime.Format(time.RFC3339),
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=t@t.com",
		"GIT_COMMITTER_DATE="+transTime.Format(time.RFC3339),
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit trans: %v\n%s", err, out)
	}

	return repo, baseTime, transTime
}

func TestClassify_BaseVsTranscendence(t *testing.T) {
	repo, baseTime, transTime := initGitRepo(t)
	cliPath := filepath.Join(repo, "library", "commerce", "yahoo-finance")
	cliRelPath := "library/commerce/yahoo-finance"

	// Provenance generatedAt is between the two commits.
	gen := baseTime.Add(1 * time.Hour)
	prov := &Provenance{GeneratedAt: gen}

	cmds := []Command{
		{Use: "quote", File: filepath.Join(cliPath, "internal/cli/quote.go")},
		{Use: "watchlist", File: filepath.Join(cliPath, "internal/cli/watchlist.go")},
	}

	classified, err := Classify(cmds, ClassifyOpts{GitRepo: repo, CLIRelPath: cliRelPath, CLISourceDir: cliPath, Provenance: prov})
	if err != nil {
		t.Fatal(err)
	}

	var quoteCls, watchlistCls *Classification
	for i := range classified {
		switch classified[i].Command.Use {
		case "quote":
			quoteCls = &classified[i]
		case "watchlist":
			watchlistCls = &classified[i]
		}
	}

	if quoteCls == nil || watchlistCls == nil {
		t.Fatal("missing classification results")
	}
	if quoteCls.IsTranscendence {
		t.Error("quote should be base (added before provenance cutoff)")
	}
	if !watchlistCls.IsTranscendence {
		t.Errorf("watchlist should be transcendence (added %v, after cutoff %v)", watchlistCls.AddedAt, gen)
	}
	if !quoteCls.AddedAt.Equal(baseTime) {
		t.Errorf("quote AddedAt = %v, want %v", quoteCls.AddedAt, baseTime)
	}
	if !watchlistCls.AddedAt.Equal(transTime) {
		t.Errorf("watchlist AddedAt = %v, want %v", watchlistCls.AddedAt, transTime)
	}
}

func TestClassify_NilProvenanceMeansNoTranscendence(t *testing.T) {
	repo, _, _ := initGitRepo(t)
	cliPath := filepath.Join(repo, "library", "commerce", "yahoo-finance")

	cmds := []Command{
		{Use: "watchlist", File: filepath.Join(cliPath, "internal/cli/watchlist.go")},
	}
	classified, err := Classify(cmds, ClassifyOpts{GitRepo: repo, CLIRelPath: "library/commerce/yahoo-finance", CLISourceDir: cliPath})
	if err != nil {
		t.Fatal(err)
	}
	if classified[0].IsTranscendence {
		t.Error("with nil provenance, no command should be marked transcendence")
	}
	// But we should still have AddedAt populated from git.
	if classified[0].AddedAt.IsZero() {
		t.Error("AddedAt should be populated even without provenance")
	}
}
