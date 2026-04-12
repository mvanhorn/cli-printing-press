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

// Classify labels every Command in cmds as base or transcendence using
// git-log creation dates for each command's source file, compared against
// the provenance generatedAt cutoff.
//
// Arguments:
//   - cmds: commands extracted by ParseCLI
//   - gitRepo: root of a git repo containing the CLI (empty to skip git lookups)
//   - cliRelPath: path within the repo to the CLI (e.g., "library/commerce/yahoo-finance")
//   - provenance: from .printing-press.json; nil means "classify everything as transcendence"
//
// Behavior when git data is missing:
//   - No gitRepo → all commands get IsTranscendence=false, AddedAt zero.
//     (Safe default: caller can fall back to "render everything without
//     the base/transcendence split" in the template.)
//   - git log errors on a specific file → that file's command gets AddedAt
//     zero. The command is still returned; caller decides whether to treat
//     unknown-date as transcendence or base.
//   - No provenance → caller's choice via IsTranscendence defaulting to
//     false; see mergePolicy below.
func Classify(cmds []Command, gitRepo, cliRelPath string, provenance *Provenance) ([]Classification, error) {
	out := make([]Classification, 0, len(cmds))

	// Group commands by file to avoid running git log N times for the same file.
	fileDates := map[string]time.Time{}
	fileCommits := map[string]string{}
	seenFile := map[string]bool{}

	for _, c := range cmds {
		if !seenFile[c.File] && gitRepo != "" {
			seenFile[c.File] = true
			added, commit := firstCommitFor(gitRepo, cliRelPath, c.File)
			fileDates[c.File] = added
			fileCommits[c.File] = commit
		}
	}

	for _, c := range cmds {
		cls := Classification{Command: c}
		if gitRepo != "" {
			cls.AddedAt = fileDates[c.File]
			cls.AddedInCommit = fileCommits[c.File]
		}
		cls.IsTranscendence = isTranscendence(cls.AddedAt, provenance)
		out = append(out, cls)
	}

	return out, nil
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
// that introduced fileAbsPath into the repo at gitRepo. The CLI lives at
// cliRelPath within the repo.
//
// Returns zero values if git log fails (repo missing, file not tracked,
// binary not found, etc.). Callers get a safe fallback without crashing.
//
// Note: we intentionally do NOT use --follow. It interacts poorly with
// --diff-filter=A --reverse (silently returns no rows). Generated CLIs
// rarely rename files; losing rename tracking is an acceptable trade for
// a deterministic query. If rename tracking becomes important, a separate
// `git log --follow` query can be run and merged in.
func firstCommitFor(gitRepo, cliRelPath, fileAbsPath string) (time.Time, string) {
	fileRel, err := filepath.Rel(filepath.Join(gitRepo, cliRelPath), fileAbsPath)
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
