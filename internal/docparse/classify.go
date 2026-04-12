package docparse

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Classification labels a Command as base (present at initial generation) or
// transcendence (added post-generation via emboss/polish/manual PRs). The
// distinction drives how the README groups commands — base commands cluster
// under "Command Reference," transcendence under "Unique Features."
type Classification struct {
	Command         Command
	IsTranscendence bool
	// AddedAt is the earliest git commit date for the command's source file.
	// Zero if the file has no git history or --git-repo wasn't set.
	AddedAt time.Time
	// AddedInCommit is the SHA of the commit that introduced the source file.
	// Empty when git history is unavailable.
	AddedInCommit string
}

// Provenance captures the subset of .printing-press.json fields that
// docparse cares about — mainly the initial generation timestamp used as
// the base-vs-transcendence cutoff.
type Provenance struct {
	APIName      string    `json:"api_name"`
	CLIName      string    `json:"cli_name"`
	GeneratedAt  time.Time `json:"generated_at"`
	RunID        string    `json:"run_id"`
	SpecSource   string    `json:"spec_source,omitempty"`
	PrintingPVer string    `json:"printing_press_version,omitempty"`
}

// LoadProvenance reads .printing-press.json from the CLI directory. Returns
// (nil, nil) if the file is absent — callers treat that as "no provenance
// known" and classify every command as transcendence (safe default: surface
// everything, don't hide anything).
func LoadProvenance(cliDir string) (*Provenance, error) {
	path := filepath.Join(cliDir, ".printing-press.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var p Provenance
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &p, nil
}

// ClassifyOpts carries path resolution inputs for Classify. cliSourceDir
// is the directory the parsed cmds' File paths are rooted at (typically
// --dir passed to refresh-docs). gitRepo + cliRelPath describe where the
// CLI lives inside the git repo — which may be a different absolute path
// than cliSourceDir when --dir points at a copy or mirror.
type ClassifyOpts struct {
	// GitRepo is the root of a git repo containing the CLI. Empty skips
	// git lookups and classifies everything as base.
	GitRepo string
	// CLIRelPath is the path within GitRepo to the CLI root
	// (e.g., "library/commerce/yahoo-finance").
	CLIRelPath string
	// CLISourceDir is the directory whose layout the parsed Command.File
	// paths are relative to. Typically the --dir flag value. May or may
	// not equal GitRepo+CLIRelPath.
	CLISourceDir string
	// Provenance is .printing-press.json contents; nil leaves everything
	// classified as base.
	Provenance *Provenance
}

// Classify labels every Command in cmds as base or transcendence using
// git-log creation dates for each command's source file, compared against
// the provenance generatedAt cutoff.
//
// Behavior when git data is missing:
//   - No GitRepo → all commands get IsTranscendence=false, AddedAt zero.
//     (Safe default: caller can fall back to "render everything without
//     the base/transcendence split" in the template.)
//   - git log errors on a specific file → that file's command gets AddedAt
//     zero. The command is still returned.
//   - No provenance → IsTranscendence=false even with git data present.
func Classify(cmds []Command, opts ClassifyOpts) ([]Classification, error) {
	out := make([]Classification, 0, len(cmds))

	fileDates := map[string]time.Time{}
	fileCommits := map[string]string{}
	seenFile := map[string]bool{}

	for _, c := range cmds {
		if !seenFile[c.File] && opts.GitRepo != "" {
			seenFile[c.File] = true
			added, commit := firstCommitFor(opts.GitRepo, opts.CLIRelPath, opts.CLISourceDir, c.File)
			fileDates[c.File] = added
			fileCommits[c.File] = commit
		}
	}

	// Detect the "single-commit library add" pattern: when every file has
	// the same AddedAt and that date is AFTER the provenance GeneratedAt,
	// git can't distinguish base from transcendence — the publish step
	// atomically added the whole CLI tree. In this case, git-signal is
	// useless; everyone would be classified as transcendence which is
	// wrong. Fall back to "treat everything as base" so the README doesn't
	// claim 44 features are novel.
	gitSignalUsable := hasClassifiableGitSpread(fileDates, opts.Provenance)

	for _, c := range cmds {
		cls := Classification{Command: c}
		if opts.GitRepo != "" {
			cls.AddedAt = fileDates[c.File]
			cls.AddedInCommit = fileCommits[c.File]
		}
		if gitSignalUsable {
			cls.IsTranscendence = isTranscendence(cls.AddedAt, opts.Provenance)
		}
		// Filename heuristic: generator emits spec-derived command files as
		// `promoted_<resource>.go`. These are always base regardless of
		// other signals. Works for CLIs generated by post-#186 machine.
		if strings.HasPrefix(filepath.Base(c.File), "promoted_") {
			cls.IsTranscendence = false
		}
		out = append(out, cls)
	}

	return out, nil
}

// hasClassifiableGitSpread reports whether the git-signal can actually
// distinguish base from transcendence. Returns false when every file has
// the same AddedAt (atomic add to the repo — a common "publish to library"
// pattern that collapses all history). Also returns false when provenance
// is missing, since the cutoff is unknown.
func hasClassifiableGitSpread(fileDates map[string]time.Time, prov *Provenance) bool {
	if prov == nil || prov.GeneratedAt.IsZero() || len(fileDates) == 0 {
		return false
	}
	var earliest, latest time.Time
	for _, t := range fileDates {
		if t.IsZero() {
			continue
		}
		if earliest.IsZero() || t.Before(earliest) {
			earliest = t
		}
		if latest.IsZero() || t.After(latest) {
			latest = t
		}
	}
	if earliest.IsZero() {
		return false
	}
	// If the spread is less than a minute, treat as atomic add.
	if latest.Sub(earliest) < time.Minute {
		return false
	}
	// The cutoff must fall within the observed range — otherwise all files
	// are either before or after the cutoff and the bucketing is degenerate.
	if prov.GeneratedAt.Before(earliest) || prov.GeneratedAt.After(latest) {
		return false
	}
	return true
}

// isTranscendence returns true when AddedAt is strictly after the provenance
// generation timestamp. Uses strict >, so a file added in the same second as
// generation counts as base (reasonable — generation itself writes files).
//
// Policy when signals are incomplete:
//   - Zero AddedAt (no git info): NOT transcendence. Safe default — avoid
//     surfacing things as novel when we can't verify.
//   - Nil provenance: NOT transcendence. Same reasoning.
//
// Callers wanting stricter auditing can re-classify based on their own
// policy using the exposed AddedAt field.
func isTranscendence(addedAt time.Time, p *Provenance) bool {
	if addedAt.IsZero() || p == nil || p.GeneratedAt.IsZero() {
		return false
	}
	return addedAt.After(p.GeneratedAt)
}

// firstCommitFor returns the author date and full SHA of the first commit
// that introduced the file at fileAbsPath into the repo at gitRepo.
//
// Path resolution: cmd.File comes from ParseCLI and is rooted at
// cliSourceDir (typically the --dir passed to refresh-docs). To find the
// file inside the git repo, we strip cliSourceDir from fileAbsPath to get
// a relative path, then prepend cliRelPath (the path within the repo).
// This decouples where the caller read the source (could be a copy) from
// where git looks for history.
//
// Returns zero values if git log fails (repo missing, file not tracked,
// binary not found, etc.). Callers get a safe fallback without crashing.
//
// Note: we intentionally do NOT use --follow. It interacts poorly with
// --diff-filter=A --reverse (silently returns no rows). Generated CLIs
// rarely rename files; losing rename tracking is an acceptable trade for
// a deterministic query. If rename tracking becomes important, a separate
// `git log --follow` query can be run and merged in.
func firstCommitFor(gitRepo, cliRelPath, cliSourceDir, fileAbsPath string) (time.Time, string) {
	source := cliSourceDir
	if source == "" {
		source = filepath.Join(gitRepo, cliRelPath)
	}
	fileRel, err := filepath.Rel(source, fileAbsPath)
	if err != nil {
		return time.Time{}, ""
	}
	pathInRepo := filepath.Join(cliRelPath, fileRel)

	cmd := exec.Command("git", "-C", gitRepo, "log",
		"--diff-filter=A", "--reverse",
		"--pretty=format:%H|%aI", "--", pathInRepo)
	out, err := cmd.Output()
	if err != nil {
		return time.Time{}, ""
	}
	line := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)[0]
	if line == "" {
		return time.Time{}, ""
	}
	parts := strings.SplitN(line, "|", 2)
	if len(parts) != 2 {
		return time.Time{}, ""
	}
	t, err := time.Parse(time.RFC3339, parts[1])
	if err != nil {
		return time.Time{}, parts[0]
	}
	return t, parts[0]
}
