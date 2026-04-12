package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mvanhorn/cli-printing-press/internal/docparse"
	"github.com/mvanhorn/cli-printing-press/internal/docrefresh"
	"github.com/mvanhorn/cli-printing-press/internal/pipeline"
	"github.com/spf13/cobra"
)

// newRefreshDocsCmd registers `printing-press refresh-docs`. The subcommand
// regenerates README.md and SKILL.md from the shipped CLI's source code
// (via docparse), classified against the initial generation timestamp
// (via docparse.Classify), and rendered through docrefresh templates.
//
// Nothing outside README.md and SKILL.md is touched — enforced by design
// and validated by the integration test. Safety invariant from the plan:
// "refresh-docs MUST NOT modify any file outside README.md and SKILL.md."
func newRefreshDocsCmd() *cobra.Command {
	var (
		dir         string
		researchDir string
		gitRepo     string
		cliRelPath  string
		noLLM       bool
		dryRun      bool
		asJSON      bool
	)

	cmd := &cobra.Command{
		Use:   "refresh-docs",
		Short: "Regenerate README.md and SKILL.md for a shipped CLI",
		Long: `Refresh the README.md and SKILL.md of an already-shipped CLI without
touching its source code. Ground truth is the Cobra command tree in
the CLI's internal/cli/ source files, enriched with optional research
metadata and git/PR history.

Designed for retroactive catch-up of CLIs generated before the machine's
narrative-docs support (see docs/plans/2026-04-12-004-feat-refresh-docs
-from-cli-plan.md). Can also run after emboss/polish cycles to keep
docs in sync with evolving code.

Only README.md and SKILL.md are ever modified. All other files
(cmd/, internal/, go.mod, .printing-press.json, .manuscripts/) are
left exactly as found.`,
		Example: `  # Deterministic refresh of a local CLI, no LLM
  printing-press refresh-docs --dir ~/printing-press/library/yahoo-finance --no-llm

  # Refresh with research context and git-tracked transcendence classification
  printing-press refresh-docs \
    --dir ~/Code/printing-press-library/library/commerce/yahoo-finance \
    --research-dir ~/Code/printing-press-library/library/commerce/yahoo-finance/.manuscripts/20260411-210148 \
    --git-repo ~/Code/printing-press-library \
    --cli-relpath library/commerce/yahoo-finance

  # Preview without writing
  printing-press refresh-docs --dir ./my-cli --no-llm --dry-run`,
		RunE: func(c *cobra.Command, args []string) error {
			if dir == "" {
				return fmt.Errorf("--dir is required")
			}
			return runRefreshDocs(refreshDocsOpts{
				Dir:         dir,
				ResearchDir: researchDir,
				GitRepo:     gitRepo,
				CLIRelPath:  cliRelPath,
				NoLLM:       noLLM,
				DryRun:      dryRun,
				AsJSON:      asJSON,
			}, c.OutOrStdout(), c.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Path to the shipped CLI directory (required)")
	cmd.Flags().StringVar(&researchDir, "research-dir", "", "Path to manuscripts run directory with research.json (optional)")
	cmd.Flags().StringVar(&gitRepo, "git-repo", "", "Git repo containing the CLI (optional; enables transcendence classification by commit date)")
	cmd.Flags().StringVar(&cliRelPath, "cli-relpath", "", "Path to the CLI within --git-repo (defaults to derived from --dir and --git-repo)")
	cmd.Flags().BoolVar(&noLLM, "no-llm", false, "Skip the absorb pass; emit deterministic narrative-less output")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print proposed content to stdout without writing files")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Emit a JSON summary instead of human-readable progress")
	return cmd
}

type refreshDocsOpts struct {
	Dir         string
	ResearchDir string
	GitRepo     string
	CLIRelPath  string
	NoLLM       bool
	DryRun      bool
	AsJSON      bool
}

// refreshDocsResult captures the outcome for JSON output and for tests.
type refreshDocsResult struct {
	CommandsFound         int      `json:"commands_found"`
	BaseCommands          int      `json:"base_commands"`
	TranscendenceCommands int      `json:"transcendence_commands"`
	UnparseableFiles      []string `json:"unparseable_files,omitempty"`
	PlannedButNotBuilt    []string `json:"planned_but_not_built,omitempty"`
	READMEPath            string   `json:"readme_path,omitempty"`
	SKILLPath             string   `json:"skill_path,omitempty"`
	DryRun                bool     `json:"dry_run,omitempty"`
}

func runRefreshDocs(opts refreshDocsOpts, stdout, stderr interface{ Write([]byte) (int, error) }) error {
	// Phase 1: command discovery via AST.
	cliDir := filepath.Join(opts.Dir, "internal", "cli")
	if _, err := os.Stat(cliDir); err != nil {
		return fmt.Errorf("CLI source dir not found: %s: %w", cliDir, err)
	}
	cmds, unparseable, err := docparse.ParseCLI(cliDir)
	if err != nil {
		return fmt.Errorf("parse CLI source: %w", err)
	}
	if len(cmds) == 0 {
		return fmt.Errorf("no cobra commands found in %s", cliDir)
	}
	for _, f := range unparseable {
		fmt.Fprintf(stderr, "Warning: could not parse %s (skipping)\n", f)
	}

	// Phase 2: provenance + classification.
	prov, err := docparse.LoadProvenance(opts.Dir)
	if err != nil {
		return fmt.Errorf("load provenance: %w", err)
	}
	cliRel := opts.CLIRelPath
	if cliRel == "" && opts.GitRepo != "" {
		if rel, err := filepath.Rel(opts.GitRepo, opts.Dir); err == nil {
			cliRel = rel
		}
	}
	classified, err := docparse.Classify(cmds, docparse.ClassifyOpts{
		GitRepo:      opts.GitRepo,
		CLIRelPath:   cliRel,
		CLISourceDir: opts.Dir,
		Provenance:   prov,
	})
	if err != nil {
		return fmt.Errorf("classify commands: %w", err)
	}

	// Phase 3: load research metadata if provided. Narrative enrichment
	// beyond what the deterministic path produces is the absorb LLM pass —
	// deferred to a follow-up commit.
	var research *pipeline.ResearchResult
	if opts.ResearchDir != "" {
		r, err := pipeline.LoadResearch(opts.ResearchDir)
		if err != nil {
			fmt.Fprintf(stderr, "Warning: could not load research.json: %v\n", err)
		} else {
			research = r
		}
	}

	// Phase 4: build the render Data.
	data := buildData(opts, classified, research)

	// Phase 5: track planned-but-not-built features for audit (plan
	// invariant #7). A planned feature is matched if its command's leaf
	// appears in the shipped Cobra tree.
	plannedMissing := plannedButNotBuilt(research, cmds)
	for _, miss := range plannedMissing {
		fmt.Fprintf(stderr, "Warning: planned-but-not-built: %s\n", miss)
	}

	// Phase 6: render.
	readme, skill, err := docrefresh.Render(data)
	if err != nil {
		return fmt.Errorf("render: %w", err)
	}

	// Phase 7: enforce safety invariant #6 — every rendered command MUST
	// exist in the Cobra tree. This is the hard validation gate.
	renderedCmds := extractRenderedCommands(string(readme), string(skill), data.CLIName)
	shippedPaths := make(map[string]bool)
	for _, c := range cmds {
		// Use the first token of Use as the "shipped path" for comparison.
		if tokens := strings.Fields(c.Use); len(tokens) > 0 {
			shippedPaths[tokens[0]] = true
		}
	}
	for rc := range renderedCmds {
		if !shippedPaths[rc] {
			return fmt.Errorf("safety invariant violated: rendered command %q not present in Cobra tree", rc)
		}
	}

	result := refreshDocsResult{
		CommandsFound:         len(cmds),
		BaseCommands:          len(data.BaseCommands),
		TranscendenceCommands: len(data.TranscendenceCommands),
		UnparseableFiles:      unparseable,
		PlannedButNotBuilt:    plannedMissing,
		DryRun:                opts.DryRun,
	}

	if opts.DryRun {
		if !opts.AsJSON {
			fmt.Fprintf(stdout, "---- README.md (dry-run) ----\n%s\n", readme)
			fmt.Fprintf(stdout, "---- SKILL.md (dry-run) ----\n%s\n", skill)
		} else {
			return json.NewEncoder(stdout).Encode(result)
		}
		return nil
	}

	readmePath := filepath.Join(opts.Dir, "README.md")
	skillPath := filepath.Join(opts.Dir, "SKILL.md")
	if err := writeAtomic(readmePath, readme); err != nil {
		return fmt.Errorf("write README: %w", err)
	}
	if err := writeAtomic(skillPath, skill); err != nil {
		return fmt.Errorf("write SKILL: %w", err)
	}
	result.READMEPath = readmePath
	result.SKILLPath = skillPath

	if opts.AsJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	fmt.Fprintf(stdout, "Refreshed %d commands (%d base, %d transcendence)\n  README: %s\n  SKILL:  %s\n",
		result.CommandsFound, result.BaseCommands, result.TranscendenceCommands, readmePath, skillPath)
	return nil
}

// buildData assembles the Data struct consumed by docrefresh.Render. Pulls
// CLI identity from .printing-press.json + go.mod when available.
func buildData(opts refreshDocsOpts, classified []docparse.Classification, research *pipeline.ResearchResult) docrefresh.Data {
	d := docrefresh.Data{}

	// CLI identity from provenance.
	if prov, err := docparse.LoadProvenance(opts.Dir); err == nil && prov != nil {
		d.CLIName = prov.CLIName
		d.APIName = prov.APIName
	}
	if d.APIName == "" {
		d.APIName = filepath.Base(opts.Dir)
	}
	if d.CLIName == "" {
		d.CLIName = d.APIName + "-pp-cli"
	}

	// Module path from go.mod.
	if modPath := readGoModulePath(filepath.Join(opts.Dir, "go.mod")); modPath != "" {
		d.ModulePath = modPath
		// Category is the segment after "library/" in the module path,
		// if the module is under the library repo layout.
		d.Category = deriveCategoryFromModule(modPath)
	}

	// Human-readable display name: title-case the API name with hyphen/space
	// normalization. Falls back to the API name as-is.
	d.DisplayName = deriveDisplayName(d.APIName)

	// Attach research-derived sources. Note: research.APIName and
	// research.Recommendation are intentionally not used — APIName comes
	// from provenance (authoritative) and Recommendation isn't a
	// description. Both are read elsewhere in the pipeline.
	if research != nil {
		for _, alt := range research.Alternatives {
			d.Sources = append(d.Sources, docrefresh.Source{
				Name:     alt.Name,
				URL:      alt.URL,
				Language: alt.Language,
				Stars:    alt.Stars,
			})
		}
	}

	// Split classified commands into base / transcendence views.
	d = docrefresh.FromClassifications(d, classified)

	// Enrich transcendence commands with planned-feature rationale via
	// leaf-name lookup (path-aware matcher is a future enhancement).
	if research != nil {
		enrichWithPlannedFeatures(d.TranscendenceCommands, research.NovelFeatures)
	}

	return d
}

// enrichWithPlannedFeatures annotates each transcendence command with
// WhyItMatters and Group from a matching planned novel feature, when the
// leaf name or hyphen-prefix matches. Mutates the slice in place.
func enrichWithPlannedFeatures(cmds []docrefresh.CommandView, planned []pipeline.NovelFeature) {
	for i := range cmds {
		leaf := cmds[i].Path
		for _, p := range planned {
			plannedLeaf := firstToken(p.Command)
			if plannedLeaf == "" {
				continue
			}
			if plannedLeaf == leaf ||
				strings.HasPrefix(leaf, plannedLeaf+"-") ||
				strings.HasPrefix(plannedLeaf, leaf+"-") {
				if p.Rationale != "" {
					cmds[i].WhyItMatters = p.Rationale
				}
				// Group field doesn't exist on pipeline.NovelFeature yet
				// (it's on the enriched schema in #186); leave unset here.
				break
			}
		}
	}
}

func firstToken(s string) string {
	for _, tok := range strings.Fields(s) {
		if !strings.HasPrefix(tok, "-") {
			return tok
		}
	}
	return ""
}

// plannedButNotBuilt returns planned novel features whose leaf doesn't
// appear in any shipped command's Use field. Used for audit logging.
func plannedButNotBuilt(research *pipeline.ResearchResult, cmds []docparse.Command) []string {
	if research == nil {
		return nil
	}
	shipped := make(map[string]bool)
	for _, c := range cmds {
		if tok := firstToken(c.Use); tok != "" {
			shipped[tok] = true
		}
	}
	var missing []string
	for _, p := range research.NovelFeatures {
		leaf := firstToken(p.Command)
		if leaf == "" {
			continue
		}
		if shipped[leaf] {
			continue
		}
		// Also accept hyphen-prefix match in either direction.
		matched := false
		for s := range shipped {
			if strings.HasPrefix(s, leaf+"-") || strings.HasPrefix(leaf, s+"-") {
				matched = true
				break
			}
		}
		if !matched {
			missing = append(missing, fmt.Sprintf("%s (%s)", p.Name, p.Command))
		}
	}
	return missing
}

// extractRenderedCommands walks the rendered README + SKILL for tokens that
// our templates render as command names in list-item bullets. Returns a set
// of unique first-tokens used to verify every rendered command exists in
// the Cobra tree.
//
// Only two patterns are treated as "command references":
//  1. "- **`<cmd>`** —" (README/SKILL unique-features bullet)
//  2. "- `<cliName> <cmd>` —" (SKILL command-reference bullet)
//
// Backticks in narrative prose or code blocks are NOT validated — they can
// reference shell utilities (`which`, `export`) or flags (`--json`) that
// aren't subcommands. The safety invariant is specifically about "commands
// the CLI claims to ship," not "every backtick token."
func extractRenderedCommands(readme, skill, cliName string) map[string]bool {
	out := make(map[string]bool)

	// Pattern 1: unique-features bullets — `- **`<cmd>`** —`.
	bulletBold := regexp.MustCompile("- \\*\\*`([a-z][a-z0-9-]*)[^`]*`\\*\\* —")

	// Pattern 2: command-reference bullets — `- `<cliName> <cmd>` —`.
	cliPrefix := ""
	if cliName != "" {
		cliPrefix = regexp.QuoteMeta(cliName) + " "
	}
	bulletPlain := regexp.MustCompile("- `" + cliPrefix + "([a-z][a-z0-9-]*)[^`]*` —")

	for _, src := range []string{readme, skill} {
		for _, m := range bulletBold.FindAllStringSubmatch(src, -1) {
			if len(m) >= 2 {
				out[m[1]] = true
			}
		}
		for _, m := range bulletPlain.FindAllStringSubmatch(src, -1) {
			if len(m) >= 2 {
				out[m[1]] = true
			}
		}
	}
	return out
}

// readGoModulePath extracts the module path from a go.mod file. Returns
// empty string if the file is absent or malformed.
func readGoModulePath(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module"))
		}
	}
	return ""
}

// deriveCategoryFromModule extracts the category segment from a library
// module path like github.com/mvanhorn/printing-press-library/library/commerce/x.
// Returns empty string when the module isn't under the library layout.
func deriveCategoryFromModule(module string) string {
	idx := strings.Index(module, "/library/")
	if idx == -1 {
		return ""
	}
	rest := module[idx+len("/library/"):]
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// deriveDisplayName converts an API slug to a display name. "yahoo-finance"
// → "Yahoo Finance"; "cal-com" → "Cal Com".
func deriveDisplayName(api string) string {
	if api == "" {
		return ""
	}
	parts := strings.Split(api, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

// writeAtomic writes content to path by writing to path+".tmp" then
// renaming. Prevents partial-write corruption on interrupt.
func writeAtomic(path string, content []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
