// Package narrativecheck validates that command strings in
// research.json's narrative command fields and command-shaped prose snippets resolve
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
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/mvanhorn/cli-printing-press/v4/internal/shellargs"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

// Section names the narrative section a command lives in. Matches the
// JSON path used by the bash recipe this package replaces, so log
// output is consistent across the two implementations.
type Section string

const (
	SectionQuickstart    Section = "quickstart"
	SectionRecipes       Section = "recipes"
	SectionHeadline      Section = "headline"
	SectionValueProp     Section = "value_prop"
	SectionAuthNarrative Section = "auth_narrative"
	SectionWhenToUse     Section = "when_to_use"
	SectionTroubleshoot  Section = "troubleshoots.fix"
)

// Status is a command's classification after the --help walk.
type Status string

const (
	StatusOK         Status = "ok"
	StatusMissing    Status = "missing"
	StatusEmptyWords Status = "empty-words"
	// StatusExampleFailed means the command path resolved, but the full
	// narrative example failed when executed under the verify environment.
	StatusExampleFailed Status = "example-failed"
	// StatusUnsupported means full-example validation could not safely run.
	StatusUnsupported Status = "unsupported"
)

const sideEffectfulExampleSkipReason = "full-example validation skipped: command is side-effectful (auth/launch/apply)"

type Result struct {
	Section Section `json:"section"`
	Command string  `json:"command"`
	// Words is the extracted subcommand path (e.g., `reports stats`)
	// after stripping the binary name and the first --flag/positional.
	// Empty when the command was a bare binary or pure-flag invocation.
	Words  string `json:"words,omitempty"`
	Status Status `json:"status"`
	Error  string `json:"error,omitempty"`
	// Notes carries structural annotations about shell pieces that were
	// excluded from validation. Each entry is shaped `<reason>: <fragment>`,
	// e.g. `pipe-skipped: jq '.items[]'` or `redirect-stripped: <
	// keywords.txt`. Empty when the recipe is plain (no pipes, no redirects).
	Notes []string `json:"notes,omitempty"`
	// StrictFailure tells JSON consumers whether this result is part of
	// Report.HasFailures. Some unsupported examples are warnings because the
	// validator intentionally skips side-effectful commands.
	StrictFailure bool `json:"strict_failure,omitempty"`
}

type Report struct {
	Walked        int      `json:"walked"`
	Missing       int      `json:"missing"`
	Empty         int      `json:"empty"`
	ExampleFailed int      `json:"example_failed,omitempty"`
	Unsupported   int      `json:"unsupported,omitempty"`
	Results       []Result `json:"results"`
	FullExamples  bool     `json:"full_examples,omitempty"`
	FrameworkOnly bool     `json:"framework_only,omitempty"`
	// ResearchEmpty is true when neither narrative.quickstart nor
	// narrative.recipes contained any entries. The LLM may have
	// omitted both sections by mistake; the caller's --strict flag
	// can decide whether that's an error.
	ResearchEmpty bool `json:"research_empty,omitempty"`
	// ResearchNotApplicable is true when the caller pointed at an
	// absent research.json, which is valid for hand-built specs that did
	// not run the Printing Press research pipeline.
	ResearchNotApplicable bool `json:"research_not_applicable,omitempty"`
}

// Options controls optional narrative validation checks.
type Options struct {
	// FullExamples validates each full narrative command, not just its
	// Cobra path. The example is run with PRINTING_PRESS_VERIFY=1 and
	// --dry-run appended when the command advertises --dry-run.
	FullExamples bool
	// FrameworkOnly validates only stable framework-command vocabulary
	// without requiring a generated CLI binary. It is intended for the
	// pre-render research.json gate, before README/SKILL examples exist.
	FrameworkOnly bool
}

// Validate parses researchPath, walks every narrative.quickstart and
// narrative.recipes command, and resolves it against the binary's
// Cobra tree by running `<binary> <words> --help`. ctx scopes every
// subprocess so callers can interrupt cleanly.
func Validate(ctx context.Context, researchPath, binaryPath string) (*Report, error) {
	return ValidateWithOptions(ctx, researchPath, binaryPath, Options{})
}

// ValidateWithOptions parses researchPath and validates every narrative
// command according to opts. The default behavior matches Validate.
func ValidateWithOptions(ctx context.Context, researchPath, binaryPath string, opts Options) (*Report, error) {
	commands, err := loadCommands(researchPath)
	if err != nil {
		return nil, err
	}
	if opts.FrameworkOnly {
		return validateFrameworkOnly(commands), nil
	}
	var templateVarAssignments []string
	if opts.FullExamples {
		templateVarAssignments = missingTemplateVarEnvAssignments(discoverTemplateVarEnv(binaryPath))
	}

	report := &Report{
		Results:       make([]Result, 0, len(commands)),
		FullExamples:  opts.FullExamples,
		ResearchEmpty: len(commands) == 0,
	}
	for _, sc := range commands {
		r := classify(ctx, binaryPath, sc.Section, sc.Command, opts, templateVarAssignments)
		switch r.Status {
		case StatusOK:
			report.Walked++
		case StatusMissing:
			report.Missing++
		case StatusEmptyWords:
			report.Empty++
		case StatusExampleFailed:
			report.ExampleFailed++
		case StatusUnsupported:
			report.Unsupported++
		}
		report.Results = append(report.Results, r)
	}
	return report, nil
}

func validateFrameworkOnly(commands []sectionCommand) *Report {
	report := &Report{
		Results:       []Result{},
		ResearchEmpty: len(commands) == 0,
		FrameworkOnly: true,
	}
	for _, sc := range commands {
		results := classifyFrameworkCommand(sc.Section, sc.Command)
		for _, r := range results {
			switch r.Status {
			case StatusOK:
				report.Walked++
			case StatusEmptyWords:
				report.Empty++
			case StatusExampleFailed:
				report.ExampleFailed++
			}
			report.Results = append(report.Results, r)
		}
	}
	return report
}

func (r Result) isStrictFailure() bool {
	switch r.Status {
	case StatusMissing, StatusEmptyWords, StatusExampleFailed:
		return true
	case StatusUnsupported:
		return r.StrictFailure
	default:
		return false
	}
}

// HasFailures reports whether the run found entries that should fail strict
// validation. Side-effectful full examples are intentionally skipped and remain
// warnings so narratives can document real auth/login/apply flows.
func (r *Report) HasFailures() bool {
	if r == nil {
		return false
	}
	if r.Missing > 0 || r.Empty > 0 || r.ExampleFailed > 0 {
		return true
	}
	if r.Unsupported == 0 {
		return false
	}
	for _, result := range r.Results {
		if result.isStrictFailure() {
			return true
		}
	}
	return false
}

type sectionCommand struct {
	Section Section
	Command string
}

func loadCommands(researchPath string) ([]sectionCommand, error) {
	data, err := os.ReadFile(researchPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("research file %s not found: %w", researchPath, err)
		}
		return nil, fmt.Errorf("reading %s: %w", researchPath, err)
	}

	// Decode just the narrative subtree we care about. Tolerates extra
	// fields in research.json (the schema is wider than narrative).
	var doc struct {
		APIName   string `json:"api_name"`
		Narrative struct {
			Headline      string `json:"headline"`
			ValueProp     string `json:"value_prop"`
			AuthNarrative string `json:"auth_narrative"`
			Quickstart    []struct {
				Command string `json:"command"`
			} `json:"quickstart"`
			Troubleshoots []struct {
				Fix string `json:"fix"`
			} `json:"troubleshoots"`
			WhenToUse string `json:"when_to_use"`
			Recipes   []struct {
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
	codeSpanCommands := []struct {
		section Section
		text    string
	}{
		{section: SectionHeadline, text: doc.Narrative.Headline},
		{section: SectionValueProp, text: doc.Narrative.ValueProp},
		{section: SectionAuthNarrative, text: doc.Narrative.AuthNarrative},
		{section: SectionWhenToUse, text: doc.Narrative.WhenToUse},
	}
	for _, tip := range doc.Narrative.Troubleshoots {
		codeSpanCommands = append(codeSpanCommands, struct {
			section Section
			text    string
		}{section: SectionTroubleshoot, text: tip.Fix})
	}
	expectedBinaries := narrativeBinaryNames(doc.APIName)
	for _, entry := range codeSpanCommands {
		for _, cmd := range extractNarrativeCodeSpanCommands(entry.text, expectedBinaries) {
			out = append(out, sectionCommand{Section: entry.section, Command: cmd})
		}
	}
	return out, nil
}

var markdownCodeSpanRE = regexp.MustCompile("`([^`\\n]+)`")

func narrativeBinaryNames(apiName string) map[string]bool {
	slug := naming.Slug(apiName)
	if slug == "" {
		return nil
	}
	return map[string]bool{
		naming.CLI(slug):       true,
		naming.LegacyCLI(slug): true,
	}
}

func extractNarrativeCodeSpanCommands(text string, expectedBinaries map[string]bool) []string {
	if len(expectedBinaries) == 0 || strings.TrimSpace(text) == "" {
		return nil
	}
	var commands []string
	for _, match := range markdownCodeSpanRE.FindAllStringSubmatch(text, -1) {
		code := strings.TrimSpace(match[1])
		tokens, err := shellargs.Split(code)
		if err != nil || len(tokens) < 2 {
			continue
		}
		if expectedBinaries[tokens[0]] && len(extractSubcommandWords(code)) > 0 {
			commands = append(commands, code)
		}
	}
	return commands
}

// classify mirrors the bash recipe's wordlist rule: drop the leading
// binary name, keep words until the first flag (starts with `-`) or
// non-identifier character. Hyphens stay because Cobra subcommands use
// them (`list-projects`).
func classify(ctx context.Context, binaryPath string, section Section, command string, opts Options, templateVarAssignments []string) Result {
	segments, err := splitShellChain(command)
	if err != nil {
		return Result{
			Section:       section,
			Command:       command,
			Status:        StatusExampleFailed,
			Error:         err.Error(),
			StrictFailure: true,
		}
	}

	// Walk segments left-to-right, emitting notes in the order they appear
	// in the original command so authors can reconstruct the recipe by
	// reading down the Notes slice.
	var notes []string
	type runnableSegment struct {
		index   int
		text    string
		cleaned string
	}
	var runnable []runnableSegment
	for i, seg := range segments {
		if seg.AfterPipe {
			notes = append(notes, "pipe-skipped: "+seg.Text)
			continue
		}
		cleaned, redirects := stripRedirects(seg.Text)
		for _, r := range redirects {
			notes = append(notes, "redirect-stripped: "+r)
		}
		runnable = append(runnable, runnableSegment{index: i, text: seg.Text, cleaned: cleaned})
	}

	finish := func(r Result) Result {
		r.Command = command
		r.Notes = append(r.Notes, notes...)
		return r
	}

	if len(runnable) == 0 {
		// Every chained segment landed on the right side of a pipe — there
		// is no runnable head. Surface this as empty-words so the author
		// notices, but keep the pipe-skipped notes for context.
		return finish(Result{
			Section:       section,
			Status:        StatusEmptyWords,
			Error:         "command has no runnable segment (every chained segment is pipe-skipped)",
			StrictFailure: true,
		})
	}

	var last Result
	var warning Result
	hasWarning := false
	for i, seg := range runnable {
		sub := classifySegment(ctx, binaryPath, section, seg.cleaned, opts, templateVarAssignments)
		if sub.Status != StatusOK {
			if len(runnable) > 1 {
				sub.Error = fmt.Sprintf("segment %d (%q): %s", i+1, seg.cleaned, sub.Error)
			}
			if !sub.isStrictFailure() {
				if !hasWarning {
					warning = sub
					hasWarning = true
				}
				continue
			}
			return finish(sub)
		}
		last = sub
	}
	if hasWarning {
		return finish(warning)
	}
	return finish(last)
}

var (
	sinceDurationPattern = regexp.MustCompile(`^\d+[dhwm]$`)
	globalFrameworkFlags = map[string]frameworkFlagSpec{
		"agent":                 {Name: "agent"},
		"allow-partial-failure": {Name: "allow-partial-failure"},
		"compact":               {Name: "compact"},
		"config":                {Name: "config", RequiresValue: true},
		"csv":                   {Name: "csv"},
		"data-source":           {Name: "data-source", RequiresValue: true},
		"deliver":               {Name: "deliver", RequiresValue: true},
		"dry-run":               {Name: "dry-run"},
		"human-friendly":        {Name: "human-friendly"},
		"idempotent":            {Name: "idempotent"},
		"ignore-missing":        {Name: "ignore-missing"},
		"json":                  {Name: "json"},
		"no-cache":              {Name: "no-cache"},
		"no-color":              {Name: "no-color"},
		"no-input":              {Name: "no-input"},
		"plain":                 {Name: "plain"},
		"profile":               {Name: "profile", RequiresValue: true},
		"quiet":                 {Name: "quiet"},
		"rate-limit":            {Name: "rate-limit", RequiresValue: true},
		"select":                {Name: "select", RequiresValue: true},
		"throttle-mode":         {Name: "throttle-mode", RequiresValue: true},
		"timeout":               {Name: "timeout", RequiresValue: true},
		"yes":                   {Name: "yes"},
	}
	frameworkCommandSpecs = map[string]frameworkCommandSpec{
		"sync": {
			Flags: []frameworkFlagSpec{
				{Name: "resources", Example: "--resources <csv>", RequiresValue: true},
				{Name: "since", Example: "--since <duration>", RequiresValue: true, Validate: validateSinceDuration},
				{Name: "full", Example: "--full"},
				{Name: "latest-only", Example: "--latest-only"},
				{Name: "max-pages", Example: "--max-pages <int>", RequiresValue: true},
				{Name: "param", Example: "--param key=value", RequiresValue: true},
				{Name: "resource-param", Example: "--resource-param resource:key=value", RequiresValue: true},
				{Name: "global-param", Example: "--global-param key=value", RequiresValue: true},
				{Name: "db", Example: "--db <path>", RequiresValue: true},
				{Name: "concurrency", Example: "--concurrency <int>", RequiresValue: true},
				{Name: "strict", Example: "--strict"},
				{Name: "path-context", Example: "--path-context key=value", RequiresValue: true},
				{Name: "dates", Example: "--dates <range>", RequiresValue: true},
			},
		},
		"search": {
			Flags: []frameworkFlagSpec{
				{Name: "type", Example: "--type <single resource>", RequiresValue: true},
				{Name: "limit", Example: "--limit <int>", RequiresValue: true},
				{Name: "db", Example: "--db <path>", RequiresValue: true},
			},
		},
		"analytics": {
			Flags: []frameworkFlagSpec{
				{Name: "type", Example: "--type <resource>", RequiresValue: true},
				{Name: "group-by", Example: "--group-by <field>", RequiresValue: true},
				{Name: "limit", Example: "--limit <int>", RequiresValue: true},
				{Name: "db", Example: "--db <path>", RequiresValue: true},
			},
		},
		"tail": {
			Flags: []frameworkFlagSpec{
				{Name: "resource", Example: "--resource <resource>", RequiresValue: true},
				{Name: "interval", Example: "--interval <duration>", RequiresValue: true},
				{Name: "since", Example: "--since <timestamp>", RequiresValue: true},
				{Name: "follow", Example: "--follow"},
			},
		},
		"doctor": {
			Flags: []frameworkFlagSpec{
				{Name: "fail-on", Example: "--fail-on <level>", RequiresValue: true},
				{Name: "refresh-bearer", Example: "--refresh-bearer"},
				{Name: "bearer-bundle-url", Example: "--bearer-bundle-url <url>", RequiresValue: true},
				{Name: "bearer-pattern", Example: "--bearer-pattern <regexp>", RequiresValue: true},
			},
		},
	}
)

type frameworkCommandSpec struct {
	Flags []frameworkFlagSpec
}

type frameworkFlagSpec struct {
	Name          string
	Example       string
	RequiresValue bool
	Validate      func(string) error
}

func classifyFrameworkCommand(section Section, command string) []Result {
	segments, err := splitShellChain(command)
	if err != nil {
		return []Result{{
			Section: section,
			Command: command,
			Status:  StatusExampleFailed,
			Error:   err.Error(),
		}}
	}

	var out []Result
	for _, seg := range segments {
		if seg.AfterPipe {
			continue
		}
		cleaned, _ := stripRedirects(seg.Text)
		tokens, err := shellargs.Split(cleaned)
		if err != nil {
			out = append(out, Result{
				Section: section,
				Command: command,
				Status:  StatusExampleFailed,
				Error:   err.Error(),
			})
			continue
		}
		if len(tokens) <= 1 {
			continue
		}
		args := tokens[1:]
		commandIndex, commandName, preCommandErr, ok := findFrameworkCommand(args)
		if !ok {
			continue
		}
		spec := frameworkCommandSpecs[commandName]
		r := Result{
			Section: section,
			Command: command,
			Words:   commandName,
			Status:  StatusOK,
		}
		if preCommandErr != nil {
			r.Status = StatusExampleFailed
			r.Error = preCommandErr.Error()
		} else if err := validateFrameworkFlags(commandName, args[commandIndex+1:], spec); err != nil {
			r.Status = StatusExampleFailed
			r.Error = err.Error()
		}
		out = append(out, r)
	}
	return out
}

func findFrameworkCommand(args []string) (int, string, error, bool) {
	var unknownPreCommandFlag string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if _, ok := frameworkCommandSpecs[arg]; ok {
			if unknownPreCommandFlag != "" {
				return i, arg, fmt.Errorf("framework command %q does not emit --%s before the command; use documented global flags before the command or documented %s flags after it", arg, unknownPreCommandFlag, arg), true
			}
			return i, arg, nil, true
		}
		if !strings.HasPrefix(arg, "--") || arg == "--" {
			return 0, "", nil, false
		}
		name, _, hasInlineValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		flag, ok := globalFrameworkFlags[name]
		if !ok {
			if unknownPreCommandFlag == "" {
				unknownPreCommandFlag = name
			}
			continue
		}
		if flag.RequiresValue && !hasInlineValue {
			i++
		}
	}
	return 0, "", nil, false
}

func validateFrameworkFlags(commandName string, args []string, spec frameworkCommandSpec) error {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") || arg == "--" {
			continue
		}
		name, value, hasInlineValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		if name == "" {
			continue
		}
		flag, ok := spec.flag(name)
		if !ok {
			flag, ok = globalFrameworkFlags[name]
		}
		if !ok {
			return fmt.Errorf("framework command %q does not emit --%s; use documented flags such as %s", commandName, name, frameworkFlagSummary(spec))
		}
		if flag.RequiresValue || flag.Validate != nil {
			if !hasInlineValue {
				if i+1 >= len(args) {
					return fmt.Errorf("framework command %q requires --%s to have a value", commandName, name)
				}
				value = args[i+1]
				i++
			}
			if flag.Validate != nil {
				if err := flag.Validate(value); err != nil {
					return fmt.Errorf("framework command %q has invalid --%s: %w", commandName, name, err)
				}
			}
		}
	}
	return nil
}

func (s frameworkCommandSpec) flag(name string) (frameworkFlagSpec, bool) {
	for _, flag := range s.Flags {
		if flag.Name == name {
			return flag, true
		}
	}
	return frameworkFlagSpec{}, false
}

func validateSinceDuration(value string) error {
	if sinceDurationPattern.MatchString(strings.TrimSpace(value)) {
		return nil
	}
	return fmt.Errorf("use a relative duration like 7d, 24h, 1w, or 30m, got %q", value)
}

func frameworkFlagSummary(spec frameworkCommandSpec) string {
	if len(spec.Flags) == 0 {
		return "the command's generated help"
	}
	parts := make([]string, 0, len(spec.Flags))
	for _, flag := range spec.Flags {
		if flag.Example != "" {
			parts = append(parts, flag.Example)
		} else {
			parts = append(parts, "--"+flag.Name)
		}
	}
	return strings.Join(parts, ", ")
}

func classifySegment(ctx context.Context, binaryPath string, section Section, command string, opts Options, templateVarAssignments []string) Result {
	words := extractSubcommandWords(command)
	r := Result{Section: section, Command: command, Words: strings.Join(words, " ")}

	if len(words) == 0 {
		r.Status = StatusEmptyWords
		r.Error = "command has no subcommand words to verify (bare binary or pure-flag invocation)"
		r.StrictFailure = true
		return r
	}

	helpArgs := append(words, "--help")
	if !opts.FullExamples {
		if err := exec.CommandContext(ctx, binaryPath, helpArgs...).Run(); err != nil {
			r.Status = StatusMissing
			r.Error = fmt.Sprintf("%s %s --help failed: %v", binaryPath, r.Words, err)
			r.StrictFailure = true
			return r
		}

		r.Status = StatusOK
		return r
	}

	helpOut, err := exec.CommandContext(ctx, binaryPath, helpArgs...).CombinedOutput()
	if err != nil {
		r.Status = StatusMissing
		r.Error = fmt.Sprintf("%s %s --help failed: %v", binaryPath, r.Words, err)
		r.StrictFailure = true
		return r
	}
	return classifyFullExample(ctx, binaryPath, command, helpOut, r, templateVarAssignments)
}

func classifyFullExample(ctx context.Context, binaryPath, command string, helpOut []byte, r Result, templateVarAssignments []string) Result {
	tokens, err := shellargs.Split(command)
	if err != nil {
		r.Status = StatusExampleFailed
		r.Error = err.Error()
		r.StrictFailure = true
		return r
	}
	if len(tokens) <= 1 {
		r.Status = StatusEmptyWords
		r.Error = "command has no arguments to execute after the binary name"
		r.StrictFailure = true
		return r
	}

	args := append([]string(nil), tokens[1:]...)
	if isSideEffectfulNarrativeExample(args) {
		r.Status = StatusUnsupported
		r.Error = sideEffectfulExampleSkipReason
		return r
	}
	if !hasEnabledBoolFlag(args, "--dry-run") {
		if !helpAdvertisesDryRun(helpOut) {
			r.Status = StatusUnsupported
			r.Error = "full-example validation skipped: command help does not advertise --dry-run"
			r.StrictFailure = true
			return r
		}
		args = append(args, "--dry-run")
	}

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	// Mirror the verify pipeline's mock-mode contract: VERIFY=1 lets
	// generated commands short-circuit visible side effects, and
	// VERIFY_LIVE_HTTP=1 opts back in to the real wire path so the
	// transport-layer mutating-verb gate doesn't collapse narrative
	// full-example assertions to a synthetic envelope.
	cmd.Env = append(os.Environ(), "PRINTING_PRESS_VERIFY=1", "PRINTING_PRESS_VERIFY_LIVE_HTTP=1")
	cmd.Env = append(cmd.Env, templateVarAssignments...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.Status = StatusExampleFailed
		r.Error = fmt.Sprintf("full example failed: %s %s: %v%s",
			binaryPath,
			strings.Join(args, " "),
			err,
			formatOutputSuffix(out),
		)
		r.StrictFailure = true
		return r
	}

	r.Status = StatusOK
	return r
}

type templateVarEnvEntry struct {
	Name  string
	Value string
}

func discoverTemplateVarEnv(binaryPath string) []templateVarEnvEntry {
	manifest, err := pipeline.ReadCLIManifest(filepath.Dir(binaryPath))
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var envs []templateVarEnvEntry
	for _, templateVar := range manifest.EndpointTemplateVars {
		templateVar = strings.TrimSpace(templateVar)
		if templateVar == "" {
			continue
		}
		envName := strings.TrimSpace(manifest.EndpointTemplateEnvOverrides[templateVar])
		if envName == "" {
			apiName := strings.TrimSpace(manifest.APIName)
			if apiName == "" {
				continue
			}
			envName = spec.DefaultEndpointTemplateEnvName(apiName, templateVar)
		}
		if seen[envName] {
			continue
		}
		seen[envName] = true
		envs = append(envs, templateVarEnvEntry{
			Name:  envName,
			Value: templateVar + "_placeholder",
		})
	}
	return envs
}

func missingTemplateVarEnvAssignments(templateVars []templateVarEnvEntry) []string {
	var env []string
	for _, templateVar := range templateVars {
		if strings.TrimSpace(os.Getenv(templateVar.Name)) != "" {
			continue
		}
		env = append(env, templateVar.Name+"="+templateVar.Value)
	}
	return env
}

func isSideEffectfulNarrativeExample(args []string) bool {
	if len(args) >= 2 && args[0] == "auth" {
		switch args[1] {
		case "set-token", "logout", "setup", "login":
			return true
		}
	}

	if hasEnabledBoolFlag(args, "--launch") {
		return true
	}
	hasApply := hasEnabledBoolFlag(args, "--apply")
	return hasApply && !hasEnabledBoolFlag(args, "--dry-run")
}

func hasEnabledBoolFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag || arg == flag+"=true" {
			return true
		}
	}
	return false
}

func helpAdvertisesDryRun(out []byte) bool {
	return strings.Contains(string(out), "--dry-run")
}

func formatOutputSuffix(out []byte) string {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return ""
	}
	const max = 500
	if len(trimmed) > max {
		trimmed = trimmed[:max] + "..."
	}
	return ": " + trimmed
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

type chainSegment = shellargs.ChainSegment

// splitShellChain walks command and returns the segments separated by
// top-level `&&`, `||`, `;`, or `|` operators. Quoted text is preserved.
// Segments after a top-level `|` carry AfterPipe=true; the validator
// reports those as `pipe-skipped` rather than executing them.
//
// Backslash-escapes and quoted-string handling mirror shellargs.Split
// so the segments are safe to feed back into shellargs.Split.
func splitShellChain(command string) ([]chainSegment, error) {
	return shellargs.SplitChain(command)
}

// stripRedirects removes top-level shell redirects (`<file`, `>file`, `>>file`)
// from a segment and returns the cleaned text plus the human-readable redirect
// fragments that were excised (e.g. `< keywords.txt`). The validator records
// the fragments as `redirect-stripped` notes so authors see what the runtime
// dropped. Redirects inside quoted strings and `2>&1`-style fd duplications
// are left alone.
func stripRedirects(segment string) (string, []string) {
	var (
		cleaned   strings.Builder
		redirects []string
		quote     rune
		escaped   bool
	)
	cleaned.Grow(len(segment))
	bytes := []byte(segment)
	i := 0
	for i < len(bytes) {
		c := bytes[i]
		if escaped {
			cleaned.WriteByte(c)
			escaped = false
			i++
			continue
		}
		if quote != 0 {
			cleaned.WriteByte(c)
			switch {
			case quote == '\'' && c == '\'':
				quote = 0
			case quote == '"' && c == '\\':
				escaped = true
			case quote == '"' && c == '"':
				quote = 0
			}
			i++
			continue
		}
		switch c {
		case '\\':
			cleaned.WriteByte(c)
			escaped = true
			i++
		case '\'', '"':
			cleaned.WriteByte(c)
			quote = rune(c)
			i++
		case '<', '>':
			// fd duplication like `2>&1` is signaled by a preceding digit AND
			// a following `&`. Bare `>&file` is `&>file` shorthand (a real
			// redirect target) and must still be stripped, so the guard only
			// fires when both signals are present.
			prevIsDigit := cleaned.Len() > 0 && cleaned.String()[cleaned.Len()-1] >= '0' && cleaned.String()[cleaned.Len()-1] <= '9'
			if c == '>' && prevIsDigit && i+1 < len(bytes) && bytes[i+1] == '&' {
				cleaned.WriteByte(c)
				i++
				continue
			}
			op := string(c)
			if c == '>' && i+1 < len(bytes) && bytes[i+1] == '>' {
				op = ">>"
				i++
			}
			i++
			for i < len(bytes) && (bytes[i] == ' ' || bytes[i] == '\t') {
				i++
			}
			fileStart := i
			fileQuote := rune(0)
			fileEsc := false
			for i < len(bytes) {
				b := bytes[i]
				if fileEsc {
					fileEsc = false
					i++
					continue
				}
				if fileQuote == '\'' {
					if b == '\'' {
						fileQuote = 0
					}
					i++
					continue
				}
				if fileQuote == '"' {
					switch b {
					case '\\':
						fileEsc = true
					case '"':
						fileQuote = 0
					}
					i++
					continue
				}
				switch b {
				case '\'', '"':
					fileQuote = rune(b)
					i++
					continue
				case '\\':
					fileEsc = true
					i++
					continue
				case ' ', '\t':
					goto fileDone
				}
				i++
			}
		fileDone:
			fragment := strings.TrimSpace(op + " " + string(bytes[fileStart:i]))
			redirects = append(redirects, fragment)
			// Skip any whitespace that follows the file token so a
			// redirect-stripped span sandwiched between two flags collapses
			// to a single space.
			for i < len(bytes) && (bytes[i] == ' ' || bytes[i] == '\t') {
				i++
			}
			trimmed := strings.TrimRight(cleaned.String(), " \t")
			cleaned.Reset()
			cleaned.WriteString(trimmed)
			cleaned.WriteByte(' ')
		default:
			cleaned.WriteByte(c)
			i++
		}
	}
	return strings.TrimSpace(cleaned.String()), redirects
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
