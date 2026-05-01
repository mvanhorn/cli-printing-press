// Package narrativecheck validates that command strings in
// research.json's narrative.quickstart and narrative.recipes resolve
// against a built printed-CLI binary's Cobra tree.
//
// The narrative is LLM-authored (or hand-edited) and easily drifts from
// the actual CLI surface — e.g., research.json names `<cli> stats` but
// the real shape is `<cli> reports stats`, or a command was dropped
// because its endpoint had a complex body. Without this check, broken
// commands ship to the README's Quick Start and the SKILL's recipes;
// users hit "unknown command" on their very first copy-paste.
package narrativecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"
)

// Section names the narrative section a command lives in. Matches the
// JSON path used by the bash recipe this package replaces, so log
// output is consistent across the two implementations.
type Section string

const (
	SectionQuickstart Section = "quickstart"
	SectionRecipes    Section = "recipes"
)

// Status is a command's classification after the --help walk.
type Status string

const (
	StatusOK         Status = "ok"
	StatusMissing    Status = "missing"
	StatusEmptyWords Status = "empty-words"
)

type Result struct {
	Section Section `json:"section"`
	Command string  `json:"command"`
	// Words is the extracted subcommand path (e.g., `reports stats`)
	// after stripping the binary name and the first --flag/positional.
	// Empty when the command was a bare binary or pure-flag invocation.
	Words  string `json:"words,omitempty"`
	Status Status `json:"status"`
	Error  string `json:"error,omitempty"`
}

type Report struct {
	Walked  int      `json:"walked"`
	Missing int      `json:"missing"`
	Empty   int      `json:"empty"`
	Results []Result `json:"results"`
	// ResearchEmpty is true when neither narrative.quickstart nor
	// narrative.recipes contained any entries. The LLM may have
	// omitted both sections by mistake; the caller's --strict flag
	// can decide whether that's an error.
	ResearchEmpty bool `json:"research_empty,omitempty"`
}

// Validate parses researchPath, walks every narrative.quickstart and
// narrative.recipes command, and resolves it against the binary's
// Cobra tree by running `<binary> <words> --help`. ctx scopes every
// subprocess so callers can interrupt cleanly.
func Validate(ctx context.Context, researchPath, binaryPath string) (*Report, error) {
	commands, err := loadCommands(researchPath)
	if err != nil {
		return nil, err
	}

	report := &Report{
		Results:       make([]Result, 0, len(commands)),
		ResearchEmpty: len(commands) == 0,
	}
	for _, sc := range commands {
		r := classify(ctx, binaryPath, sc.Section, sc.Command)
		switch r.Status {
		case StatusOK:
			report.Walked++
		case StatusMissing:
			report.Missing++
		case StatusEmptyWords:
			report.Empty++
		}
		report.Results = append(report.Results, r)
	}
	return report, nil
}

// HasFailures reports whether the run found any missing or empty-words
// entries. Callers gate --strict exit codes on this.
func (r *Report) HasFailures() bool {
	return r.Missing > 0 || r.Empty > 0
}

type sectionCommand struct {
	Section Section
	Command string
}

func loadCommands(researchPath string) ([]sectionCommand, error) {
	data, err := os.ReadFile(researchPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("research file %s not found; cannot validate narrative commands", researchPath)
		}
		return nil, fmt.Errorf("reading %s: %w", researchPath, err)
	}

	// Decode just the narrative subtree we care about. Tolerates extra
	// fields in research.json (the schema is wider than narrative).
	var doc struct {
		Narrative struct {
			Quickstart []struct {
				Command string `json:"command"`
			} `json:"quickstart"`
			Recipes []struct {
				Command string `json:"command"`
			} `json:"recipes"`
		} `json:"narrative"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", researchPath, err)
	}

	var out []sectionCommand
	for _, q := range doc.Narrative.Quickstart {
		if cmd := strings.TrimSpace(q.Command); cmd != "" {
			out = append(out, sectionCommand{Section: SectionQuickstart, Command: cmd})
		}
	}
	for _, r := range doc.Narrative.Recipes {
		if cmd := strings.TrimSpace(r.Command); cmd != "" {
			out = append(out, sectionCommand{Section: SectionRecipes, Command: cmd})
		}
	}
	return out, nil
}

// classify mirrors the bash recipe's wordlist rule: drop the leading
// binary name, keep words until the first flag (starts with `-`) or
// non-identifier character. Hyphens stay because Cobra subcommands use
// them (`list-projects`).
func classify(ctx context.Context, binaryPath string, section Section, command string) Result {
	words := extractSubcommandWords(command)
	r := Result{Section: section, Command: command, Words: strings.Join(words, " ")}

	if len(words) == 0 {
		r.Status = StatusEmptyWords
		r.Error = "command has no subcommand words to verify (bare binary or pure-flag invocation)"
		return r
	}

	args := append(words, "--help")
	if err := exec.CommandContext(ctx, binaryPath, args...).Run(); err != nil {
		r.Status = StatusMissing
		r.Error = fmt.Sprintf("%s %s --help failed: %v", binaryPath, r.Words, err)
		return r
	}

	r.Status = StatusOK
	return r
}

// extractSubcommandWords replicates the bash recipe's awk wordlist
// extraction so the Go and bash implementations classify identically:
//
//	for (i=2; i<=NF; i++) {
//	  if ($i ~ /^-/ || $i ~ /[^a-zA-Z0-9_-]/) break
//	  print $i
//	}
//
// Strip the first token (binary name), then keep tokens until the first
// flag or any token containing a character outside [A-Za-z0-9_-].
func extractSubcommandWords(command string) []string {
	tokens := strings.Fields(command)
	if len(tokens) <= 1 {
		return nil
	}
	var words []string
	for _, tok := range tokens[1:] {
		if strings.HasPrefix(tok, "-") || !isIdentifierToken(tok) {
			break
		}
		words = append(words, tok)
	}
	return words
}

// isIdentifierToken reports whether s contains only ASCII alphanumerics,
// underscores, and hyphens. Anything else (=, :, /, quotes, JSON-string
// arguments, etc.) signals the start of a non-subcommand token and ends
// the wordlist scan.
func isIdentifierToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}
