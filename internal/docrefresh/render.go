// Package docrefresh renders README.md and SKILL.md for a shipped CLI
// using the output of internal/docparse as primary input, plus optional
// narrative enrichment from research.json and manuscripts.
//
// This package intentionally does not import internal/generator or
// internal/spec — the traditional generate pipeline is spec-driven, and
// refresh-docs is shipped-CLI-driven. Sharing structs would couple the two
// paths and invite drift. Templates here are tailored to the refresh-docs
// data shape.
package docrefresh

import (
	"bytes"
	_ "embed"
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/mvanhorn/cli-printing-press/internal/docparse"
)

//go:embed templates/readme_refresh.md.tmpl
var readmeTmpl string

//go:embed templates/skill_refresh.md.tmpl
var skillTmpl string

// Data is the single input struct consumed by both the README and SKILL
// templates. All fields are optional; templates degrade gracefully when
// absent. Callers populate what they can from docparse + manuscripts;
// empty fields render as empty sections or fall through to defaults.
type Data struct {
	// CLIName is the shipped binary name (e.g., "yahoo-finance-pp-cli").
	CLIName string
	// APIName is the human-ish API slug used in the library repo
	// (e.g., "yahoo-finance").
	APIName string
	// Category is the library category (e.g., "commerce").
	Category string
	// DisplayName is the human-readable API name (e.g., "Yahoo Finance").
	DisplayName string
	// ModulePath is the Go module path from go.mod (e.g.,
	// "github.com/mvanhorn/printing-press-library/library/commerce/yahoo-finance").
	ModulePath string
	// Description is the one-line CLI description from the spec or manifest.
	Description string
	// BaseCommands are the commands present at initial generation.
	// Grouped under "Command Reference" in the README.
	BaseCommands []CommandView
	// TranscendenceCommands are commands added post-generation.
	// Grouped under "Unique Features" in the README with richer presentation.
	TranscendenceCommands []CommandView
	// Narrative is LLM-authored prose (headline, value prop, etc.).
	// Optional; nil means templates use deterministic fallback.
	Narrative *Narrative
	// Sources credit alternative implementations in the ecosystem.
	Sources []Source
	// AuthType hints the template at which auth-setup block to render.
	// Values: "api_key", "bearer_token", "oauth2", "cookie", "composed",
	// "none". Unknown values render the "none" block.
	AuthType string
	// AuthEnvVars lists env vars the CLI reads for auth (e.g.,
	// ["YAHOO_FINANCE_API_KEY"]). Empty for cookie/composed auth.
	AuthEnvVars []string
}

// CommandView is a minimal render-facing view of a classified command.
// Templates iterate over CommandView slices.
type CommandView struct {
	// Path is the full dotted/space-separated command path as it would be
	// typed (e.g., "portfolio perf", "auth login-chrome").
	Path string
	// Use is the raw Cobra Use field (may include positional args like
	// "add <name> <symbol>"). Templates may show either Path or Use.
	Use string
	// Short is the one-line description.
	Short string
	// Long is multi-line description for per-command help.
	Long string
	// Example is an optional ready-to-run invocation.
	Example string
	// WhyItMatters is agent-facing rationale. Authored by the absorb LLM
	// pass or sourced from a matching planned novel feature. Empty in
	// pure --no-llm mode with no planned counterpart.
	WhyItMatters string
	// Group is a theme name for clustering in the Unique Features section.
	// Empty when no grouping signal is available; templates render a flat
	// list in that case.
	Group string
}

// Narrative carries LLM-authored prose used to lift the README / SKILL
// from mechanical scaffolding to product copy.
type Narrative struct {
	Headline       string
	ValueProp      string
	AuthNarrative  string
	WhenToUse      string
	QuickStart     []QuickStartStep
	Recipes        []Recipe
	TriggerPhrases []string
}

// QuickStartStep is a single step in the README's Quick Start section.
type QuickStartStep struct {
	Comment string // human-readable preamble rendered as a bash comment
	Command string
}

// Recipe is a worked example rendered in SKILL.md's Recipes section.
type Recipe struct {
	Title       string
	Command     string
	Explanation string
}

// Source credits an ecosystem alternative (e.g., yfinance, yahoo-finance2)
// that inspired the CLI. Rendered in the README's Sources & Inspiration
// section.
type Source struct {
	Name     string
	URL      string
	Language string
	Stars    int
}

// FromClassifications is a convenience that splits a Classification slice
// into base and transcendence CommandViews and attaches them to an
// otherwise-populated Data. Returns a shallow copy of d with the two
// command slices filled in, so callers can chain.
//
// The input classifications are preserved in source-file order within
// each bucket (sorted by file/constructor, matching ParseCLI's order).
func FromClassifications(d Data, classified []docparse.Classification) Data {
	base := make([]CommandView, 0, len(classified))
	trans := make([]CommandView, 0, len(classified))
	for _, c := range classified {
		v := CommandView{
			Path:    commandPath(c.Command),
			Use:     c.Command.Use,
			Short:   c.Command.Short,
			Long:    c.Command.Long,
			Example: c.Command.Example,
		}
		if c.IsTranscendence {
			trans = append(trans, v)
		} else {
			base = append(base, v)
		}
	}
	d.BaseCommands = base
	d.TranscendenceCommands = trans
	return d
}

// commandPath returns a human-readable path for a Command. For the
// unwrapped case (Use contains only the leaf), returns Use directly.
// Parent-subcommand joining is TBD — the AST parser currently returns
// flat commands with SubcommandConstructors for parents; full path
// resolution happens at a higher level once we wire in the tree walker.
//
// For now, returns the first token of Use (the command name only,
// stripping positional args like "<name>").
func commandPath(c docparse.Command) string {
	tokens := strings.Fields(c.Use)
	if len(tokens) == 0 {
		return ""
	}
	return tokens[0]
}

// Render executes both templates and returns (readme, skill, error).
// Neither file is written — callers decide where to put the output.
// This separation makes the function trivial to test: assert the returned
// bytes directly.
func Render(d Data) (readme, skill []byte, err error) {
	readme, err = renderTemplate("readme", readmeTmpl, d)
	if err != nil {
		return nil, nil, fmt.Errorf("render README: %w", err)
	}
	skill, err = renderTemplate("skill", skillTmpl, d)
	if err != nil {
		return nil, nil, fmt.Errorf("render SKILL: %w", err)
	}
	return readme, skill, nil
}

func renderTemplate(name, text string, d Data) ([]byte, error) {
	t, err := template.New(name).Funcs(template.FuncMap{
		"join":   strings.Join,
		"groups": groupTranscendence,
	}).Parse(text)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, d); err != nil {
		return nil, fmt.Errorf("execute: %w", err)
	}
	return buf.Bytes(), nil
}

// groupTranscendence buckets transcendence commands by their Group field.
// Commands with no Group land in a single "Other" bucket. Buckets are
// returned in a stable order: groups sorted alphabetically, "Other" last.
// Each bucket preserves input command order so the rendered README matches
// the code's file layout.
func groupTranscendence(cmds []CommandView) []CommandGroup {
	buckets := map[string][]CommandView{}
	var order []string
	for _, c := range cmds {
		name := c.Group
		if name == "" {
			name = "Other"
		}
		if _, seen := buckets[name]; !seen {
			order = append(order, name)
		}
		buckets[name] = append(buckets[name], c)
	}
	// Stable order: alphabetical, with "Other" pushed to the end.
	sort.SliceStable(order, func(i, j int) bool {
		if order[i] == "Other" {
			return false
		}
		if order[j] == "Other" {
			return true
		}
		return order[i] < order[j]
	})
	out := make([]CommandGroup, 0, len(order))
	for _, name := range order {
		out = append(out, CommandGroup{Name: name, Commands: buckets[name]})
	}
	return out
}

// CommandGroup is a render-facing grouping of transcendence commands.
type CommandGroup struct {
	Name     string
	Commands []CommandView
}
