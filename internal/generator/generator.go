package generator

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unicode"

	"github.com/mvanhorn/cli-printing-press/v3/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/v3/internal/mcpdesc"
	"github.com/mvanhorn/cli-printing-press/v3/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v3/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

//go:embed templates
var templateFS embed.FS

// TemplateFS exposes the embedded template tree for callers outside the
// generator package (e.g. the patch subcommand that renders a subset of
// templates against already-published CLIs).
var TemplateFS = templateFS

// ReadmeSource represents a credited ecosystem tool for the README.
type ReadmeSource struct {
	Name     string
	URL      string
	Language string
	Stars    int
}

// NovelFeature represents a transcendence feature for the README and SKILL.md.
type NovelFeature struct {
	Name         string
	Command      string
	Description  string
	Rationale    string
	Example      string // ready-to-run invocation
	WhyItMatters string // one-sentence agent-facing rationale
	Group        string // theme name for grouped rendering
}

// QuickStartStep mirrors pipeline.QuickStartStep for template rendering.
type QuickStartStep struct {
	Command string
	Comment string
}

// Recipe mirrors pipeline.Recipe for SKILL.md template rendering.
type Recipe struct {
	Title       string
	Command     string
	Explanation string
}

// TroubleshootTip mirrors pipeline.TroubleshootTip for template rendering.
type TroubleshootTip struct {
	Symptom string
	Fix     string
}

// novelFeatureGroup is a template-facing bucket of novel features sharing
// a Group name. Produced by the groupNovelFeatures template helper so the
// README/SKILL templates don't have to do collection logic in-template.
type novelFeatureGroup struct {
	Name     string
	Features []NovelFeature
}

// ReadmeNarrative mirrors pipeline.ReadmeNarrative for template rendering.
// Holds LLM-authored prose that makes generated docs feel like product
// documentation rather than scaffolding. All fields are optional.
type ReadmeNarrative struct {
	DisplayName    string
	Headline       string
	ValueProp      string
	AuthNarrative  string
	QuickStart     []QuickStartStep
	Troubleshoots  []TroubleshootTip
	WhenToUse      string
	Recipes        []Recipe
	TriggerPhrases []string
}

// DomainContext holds structured domain knowledge for MCP-connected agents.
// Front-loaded at session start so agents understand the API without discovery.
type DomainContext struct {
	APIName     string            `json:"api_name"`
	Description string            `json:"description"`
	Archetype   string            `json:"archetype"`
	Resources   []ResourceSummary `json:"resources"`
	QueryTips   []string          `json:"query_tips,omitempty"`
	Playbook    []PlaybookEntry   `json:"playbook,omitempty"`
}

// ResourceSummary describes an API resource and its capabilities for agents.
type ResourceSummary struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Endpoints   []string `json:"endpoints"`
	Syncable    bool     `json:"syncable,omitempty"`
	Searchable  bool     `json:"searchable,omitempty"`
}

// PlaybookEntry is a domain-specific insight for agents.
type PlaybookEntry struct {
	Topic   string `json:"topic"`
	Insight string `json:"insight"`
}

type Generator struct {
	Spec            *spec.APISpec
	OutputDir       string
	VisionSet       VisionTemplateSet
	FixtureSet      *browsersniff.FixtureSet
	TrafficAnalysis *browsersniff.TrafficAnalysis
	Sources         []ReadmeSource          // Ecosystem tools to credit in README
	DiscoveryPages  []string                // Pages visited during browser-sniff discovery
	NovelFeatures   []NovelFeature          // Transcendence features for README/SKILL
	Narrative       *ReadmeNarrative        // LLM-authored prose for README/SKILL; optional
	AsyncJobs       map[string]AsyncJobInfo // Detected async-job endpoints, keyed by "<resource>/<endpoint>"

	// ModulePath overrides the Go module import path emitted by templates that
	// reference internal packages (`{{modulePath}}/internal/client`, etc.).
	// Defaults to `<api>-pp-cli` when empty — matches the standalone-publish
	// shape. Set explicitly when regenerating a CLI that lives under a
	// different go.mod, e.g. library checkouts where the module path is the
	// repo-prefixed full path. Read by mcp-sync from the existing go.mod.
	ModulePath string

	// Promoted-command plan, populated by Generate() before any rendering so
	// SKILL/README templates can honor leaf promotion (and not emit phantom paths
	// like `<cli> qr get-qrcode` for a resource the generator collapsed to `qr`).
	PromotedCommands      []PromotedCommand
	PromotedResourceNames map[string]bool
	PromotedEndpointNames map[string]string

	profile   *profiler.APIProfile
	funcs     template.FuncMap
	templates map[string]*template.Template
}

func New(s *spec.APISpec, outputDir string) *Generator {
	if s.Owner == "" {
		s.Owner = resolveOwnerForExisting(outputDir)
	}
	// Sanitize owner for Go module path: lowercase, no spaces/special chars
	s.Owner = strings.ToLower(s.Owner)
	s.Owner = strings.ReplaceAll(s.Owner, " ", "-")
	s.Owner = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return -1
	}, s.Owner)
	g := &Generator{
		Spec:      s,
		OutputDir: outputDir,
		templates: make(map[string]*template.Template),
	}
	g.funcs = template.FuncMap{
		"title":                 cases.Title(language.English).String,
		"lower":                 strings.ToLower,
		"upper":                 strings.ToUpper,
		"join":                  strings.Join,
		"camel":                 toCamel,
		"snake":                 naming.Snake,
		"pascal":                toPascal,
		"goType":                goType,
		"goStructType":          goStructType,
		"goTypeForParam":        goTypeForParam,
		"goStoreType":           goStoreType,
		"cobraFlagFunc":         cobraFlagFunc,
		"cobraFlagFuncForParam": cobraFlagFuncForParam,
		"defaultVal":            defaultVal,
		"defaultValForParam":    defaultValForParam,
		"zeroVal":               zeroVal,
		"zeroValForParam": func(name, t string) string {
			kind := primitiveKind(t)
			if isIDParam(name) && kind == "int" {
				return `""`
			}
			if isCursorParam(name) && (kind == "int" || kind == "float") {
				return `""`
			}
			return zeroVal(t)
		},
		"positionalArgs":         positionalArgs,
		"configTag":              configTag,
		"camelToJSON":            camelToJSON,
		"columnNames":            columnNames,
		"columnPlaceholders":     columnPlaceholders,
		"updateSet":              updateSet,
		"envVarField":            envVarField,
		"envVarPlaceholder":      naming.EnvVarPlaceholder,
		"envVarIsBuiltinField":   envVarIsBuiltinField,
		"envVarBuiltinFieldName": envVarBuiltinFieldName,
		"resolveEnvVarField":     resolveEnvVarField,
		"add":                    func(a, b int) int { return a + b },
		"oneline":                naming.OneLine,
		"composeMCPDesc":         composeMCPDesc,
		"flagName":               flagName,
		"paramIdent":             paramIdent,
		"safeTypeName":           safeTypeName,
		"hasNonScalarType": func(types map[string]spec.TypeDef) bool {
			for _, td := range types {
				for _, f := range td.Fields {
					if f.Type == "object" || f.Type == "array" {
						return true
					}
				}
			}
			return false
		},
		"exampleLine": g.exampleLine,
		"currentYear": func() string { return strconv.Itoa(time.Now().Year()) },
		"modulePath": func() string {
			if g.ModulePath != "" {
				return g.ModulePath
			}
			return naming.CLI(s.Name)
		},
		"graphqlQueryField": graphqlQueryField,
		"graphqlFieldSelection": func(typeName string, types map[string]spec.TypeDef) []string {
			return graphqlFieldSelection(typeName, types)
		},
		"isGraphQL":   isGraphQLSpec,
		"backtick":    func() string { return "`" },
		"kebab":       toKebab,
		"humanName":   naming.HumanName,
		"envPrefix":   naming.EnvPrefix,
		"mcpToolName": naming.SnakeIdentifier,
		"lookupEndpoint": func(resources map[string]spec.Resource, ref string) templateEndpoint {
			e, _ := lookupEndpointForTemplate(resources, ref)
			return e
		},
		"effectiveEndpointPath":    effectiveEndpointPath,
		"effectiveSubEndpointPath": effectiveSubEndpointPath,
		"enumLiteral": func(values []string) string {
			// Render a string slice as a Go []string literal for template embedding.
			// Example: ["asc","desc"] → `"asc", "desc"`. Returns empty string when
			// the slice is empty so callers can {{if}}-gate the block.
			if len(values) == 0 {
				return ""
			}
			parts := make([]string, len(values))
			for i, v := range values {
				parts[i] = fmt.Sprintf("%q", v)
			}
			return strings.Join(parts, ", ")
		},
		"enumDescriptionHint": func(values []string) string {
			// Appends " (one of: a, b, c)" to a flag description when the param
			// has enum constraints. Returns empty string when the slice is empty.
			if len(values) == 0 {
				return ""
			}
			return " (one of: " + strings.Join(values, ", ") + ")"
		},
		"jsonStringParam":    isJSONStringParam,
		"jsonEnumSuggestion": jsonEnumSuggestion,
		"bodyMap":            bodyMap,
		// endpointNeedsClientLimit reports whether a list endpoint needs
		// client-side truncation. True when the endpoint has a `limit`-named
		// param AND no Pagination block — the spec author asked for a
		// limit flag, but didn't declare a server-side paginator. Many
		// APIs (Firebase, file-backed JSON dumps, RSS feeds) accept a
		// `?limit=N` query param without honoring it; truncating client-
		// side means the user-facing --limit flag works regardless.
		// Surfaced by hackernews retro #350 finding F6.
		"endpointNeedsClientLimit": endpointNeedsClientLimit,
		"envName":                  naming.EnvPrefix,
		"safeName":                 safeSQLName,
		"isBackfillColumn":         isStoreBackfillColumn,
		"hasBackfillColumns":       hasStoreBackfillColumns,
		"backfillDecl":             storeBackfillDecl,
		"safeNameSuffix": func(name, suffix string) string {
			return safeSQLName(name + suffix)
		},
		"sqlString": sqlStringLiteral,
		"hasDomainUpsert": func(name string) bool {
			return domainUpsertMethodName(name) != "UpsertBatch"
		},
		// hasTypedTable is the single source of truth for "this table gets a
		// typed Upsert<X>." Table creation, typed-Upsert generation, the
		// UpsertBatch dispatch switch, and the populated-table tests must all
		// gate on the same predicate; otherwise dead tables (created but never
		// written to) leak in for resources whose names hit the framework-cobra
		// rename and end up with only id/data/synced_at columns.
		"hasTypedTable": func(t TableDef) bool {
			return len(t.Columns) > 3 && t.Name != "sync_state" && domainUpsertMethodName(t.Name) != "UpsertBatch"
		},
		"pathContainsParam": func(path, name string) bool {
			return strings.Contains(path, "{"+name+"}")
		},
		"safeJoin": func(fields []string, sep string) string {
			safe := make([]string, len(fields))
			for i, f := range fields {
				safe[i] = safeSQLName(f)
			}
			return strings.Join(safe, sep)
		},
		"goLiteral": func(v any) string {
			// A nil default — common when the spec declares a field with no
			// default value — must render as the Go keyword `nil`, not `<nil>`
			// (which is what `fmt.Sprintf("%v", nil)` produces and which the
			// Go compiler rejects). Without this branch, search.go and other
			// generated files emit invalid syntax for spec fields with missing
			// defaults.
			if v == nil {
				return "nil"
			}
			switch val := v.(type) {
			case string:
				return fmt.Sprintf("%q", val)
			case int:
				return strconv.Itoa(val)
			case float64:
				if val == float64(int(val)) {
					return strconv.Itoa(int(val))
				}
				return fmt.Sprintf("%g", val)
			case bool:
				if val {
					return "true"
				}
				return "false"
			case []string:
				parts := make([]string, len(val))
				for i, s := range val {
					parts[i] = fmt.Sprintf("%q", s)
				}
				return "[]any{" + strings.Join(parts, ", ") + "}"
			case []any:
				parts := make([]string, len(val))
				for i, item := range val {
					parts[i] = fmt.Sprintf("%q", fmt.Sprint(item))
				}
				return "[]any{" + strings.Join(parts, ", ") + "}"
			case map[string]any:
				return "map[string]any{}"
			default:
				return fmt.Sprintf("%v", v)
			}
		},
		"firstResource": func(resources map[string]spec.Resource) string {
			var names []string
			for name := range resources {
				names = append(names, name)
			}
			sort.Strings(names)
			if len(names) > 0 {
				return names[0]
			}
			return "resource"
		},
		// goRawSafe makes a string safe to embed inside a Go raw-string literal
		// (backtick-delimited). Go raw strings cannot contain backticks —
		// there's no escape — so the compiler rejects the file outright.
		// Narrative fields are LLM-authored and routinely contain backticks
		// (e.g. "the `--agent` flag"), so stripping is mandatory before
		// rendering into Short/Long. Replaces ` with ' to preserve intent.
		"goRawSafe": func(s string) string {
			return strings.ReplaceAll(s, "`", "'")
		},
		// truncate clips a string to max runes with an ellipsis. Used to
		// enforce the root --help Long size budget: LLM-authored headlines
		// and novel-feature descriptions have no inherent length ceiling,
		// and agents running <cli> --help shouldn't be punished for one
		// verbose absorb output. Counts runes (not bytes) so multi-byte
		// characters don't produce mid-codepoint truncation.
		"truncate": func(max int, s string) string {
			if max <= 0 {
				return s
			}
			runes := []rune(s)
			if len(runes) <= max {
				return s
			}
			if max <= 1 {
				return string(runes[:max])
			}
			return string(runes[:max-1]) + "…"
		},
		// yamlDoubleQuoted escapes a string for safe embedding inside a YAML
		// double-quoted scalar. Handles the three failure modes we've seen
		// from LLM-authored narrative fields: unescaped " (breaks parser),
		// unescaped \ (swallows next char), and raw newlines (terminates
		// scalar). Leaves single quotes alone — valid in double-quoted YAML.
		"yamlDoubleQuoted": func(s string) string {
			s = strings.ReplaceAll(s, `\`, `\\`)
			s = strings.ReplaceAll(s, `"`, `\"`)
			s = strings.ReplaceAll(s, "\n", `\n`)
			s = strings.ReplaceAll(s, "\r", `\r`)
			s = strings.ReplaceAll(s, "\t", `\t`)
			return s
		},
		// groupNovelFeatures clusters features by their Group field, preserving
		// first-seen order of group names. Features with empty Group land in a
		// trailing "More" bucket so nothing gets dropped. Returns nil when no
		// feature carries a Group value — callers should then render flat.
		//
		// Group matching is canonicalized (lowercase + whitespace collapsed)
		// because the absorb LLM will not produce exact-match strings — given
		// five features in "Local state that compounds" it will usually emit
		// at least one "Local State That Compounds" or "local state that
		// compounds" by drift. Without canonicalization these silently render
		// as separate groups and a reader skimming the README sees the
		// grouping as broken. We canonicalize for bucketing but render the
		// first-seen display form so the LLM's casing choice wins — it's
		// usually the more legible one.
		"groupNovelFeatures": func(features []NovelFeature) []novelFeatureGroup {
			canonGroup := func(s string) string {
				return strings.Join(strings.Fields(strings.ToLower(s)), " ")
			}
			anyGrouped := false
			for _, f := range features {
				if canonGroup(f.Group) != "" {
					anyGrouped = true
					break
				}
			}
			if !anyGrouped {
				return nil
			}
			order := []string{}                // canonical keys in first-seen order
			displayName := map[string]string{} // canonical → first-seen display form
			byGroup := map[string][]NovelFeature{}
			for _, f := range features {
				display := f.Group
				key := canonGroup(display)
				if key == "" {
					key = "more"
					display = "More"
				}
				if _, seen := byGroup[key]; !seen {
					order = append(order, key)
					displayName[key] = display
				}
				byGroup[key] = append(byGroup[key], f)
			}
			out := make([]novelFeatureGroup, 0, len(order))
			for _, key := range order {
				out = append(out, novelFeatureGroup{Name: displayName[key], Features: byGroup[key]})
			}
			return out
		},
		"whichFallbackEntries": buildWhichFallbackEntries,
		"firstCommandExample":  firstCommandExample,
	}
	return g
}

func buildWhichFallbackEntries(resources map[string]spec.Resource) []NovelFeature {
	var entries []NovelFeature
	var resNames []string
	for name := range resources {
		resNames = append(resNames, name)
	}
	sort.Strings(resNames)
	for _, rName := range resNames {
		r := resources[rName]
		appendEndpoint := func(command string, endpoint spec.Endpoint) {
			description := strings.TrimSpace(endpoint.Description)
			if description == "" {
				description = "Run " + command
			}
			entries = append(entries, NovelFeature{
				Command:     command,
				Description: description,
				Group:       rName,
			})
		}

		var endpointNames []string
		for eName := range r.Endpoints {
			endpointNames = append(endpointNames, eName)
		}
		sort.Strings(endpointNames)
		for _, eName := range endpointNames {
			appendEndpoint(rName+" "+eName, r.Endpoints[eName])
		}

		var subNames []string
		for subName := range r.SubResources {
			subNames = append(subNames, subName)
		}
		sort.Strings(subNames)
		for _, subName := range subNames {
			sub := r.SubResources[subName]
			var subEndpointNames []string
			for eName := range sub.Endpoints {
				subEndpointNames = append(subEndpointNames, eName)
			}
			sort.Strings(subEndpointNames)
			for _, eName := range subEndpointNames {
				appendEndpoint(rName+" "+subName+" "+eName, sub.Endpoints[eName])
			}
		}
	}
	return entries
}

// HelperFlags controls which helper functions are emitted in helpers.go.
type HelperFlags struct {
	HasDelete          bool // spec has DELETE endpoints → emit classifyDeleteError
	HasPathParams      bool // spec has path parameters → emit replacePathParam
	HasMultiPositional bool // spec has endpoints with 2+ positional params → emit usageErr
	HasDataLayer       bool // CLI has a local store (sync/search) → emit provenance helpers
	HasClientLimit     bool // at least one endpoint needs client-side limit truncation → emit truncateJSONArray
}

// computeHelperFlags scans the spec's resources to determine which helpers are needed.
func computeHelperFlags(s *spec.APISpec) HelperFlags {
	var flags HelperFlags
	for _, r := range s.Resources {
		for _, e := range r.Endpoints {
			if strings.EqualFold(e.Method, "DELETE") {
				flags.HasDelete = true
			}
			if endpointNeedsClientLimit(e) {
				flags.HasClientLimit = true
			}
			positionalCount := 0
			for _, p := range e.Params {
				if p.Positional {
					flags.HasPathParams = true
					positionalCount++
				}
			}
			if positionalCount >= 2 {
				flags.HasMultiPositional = true
			}
		}
		for _, sub := range r.SubResources {
			for _, e := range sub.Endpoints {
				if strings.EqualFold(e.Method, "DELETE") {
					flags.HasDelete = true
				}
				if endpointNeedsClientLimit(e) {
					flags.HasClientLimit = true
				}
				positionalCount := 0
				for _, p := range e.Params {
					if p.Positional {
						flags.HasPathParams = true
						positionalCount++
					}
				}
				if positionalCount >= 2 {
					flags.HasMultiPositional = true
				}
			}
		}
	}
	return flags
}

// helpersTemplateData wraps APISpec with flags controlling conditional helper emission.
type helpersTemplateData struct {
	*spec.APISpec
	HelperFlags
}

// doctorTemplateData wraps APISpec with a HasStore flag so the doctor
// template can gate its cache-health section. Doctor is emitted for every
// CLI — with or without a local store — so the template needs explicit
// knowledge of whether internal/store exists.
type doctorTemplateData struct {
	*spec.APISpec
	HasStore bool
}

// authTemplateData wraps APISpec with traffic-analysis generation hints that
// control optional auth subcommands.
type authTemplateData struct {
	*spec.APISpec
	HasGraphQLPersistedQueries bool
}

// clientTemplateData wraps APISpec with optional runtime data hooks used by
// the generated HTTP client.
type clientTemplateData struct {
	*spec.APISpec
	HasGraphQLPersistedQueries bool
}

// endpointTemplateData is the data passed to command_endpoint.go.tmpl
// for both top-level resource endpoints and sub-resource endpoints.
// ResourceBaseURL carries the resource's BaseURL override (or its
// inherited parent override for sub-resources); the template prepends
// it to Endpoint.Path so per-resource hosts produce absolute URLs.
type endpointTemplateData struct {
	ResourceName    string
	ResourceBaseURL string
	FuncPrefix      string
	CommandPath     string
	EndpointName    string
	Endpoint        spec.Endpoint
	HasStore        bool
	IsAsync         bool
	Async           AsyncJobInfo
	// IsReadOnly mirrors !endpointIsWriteCommand(endpoint, name). The
	// emitted command sets Annotations["mcp:read-only"] = "true" when
	// it's true so the cobratree MCP walker marks the tool with
	// readOnlyHint and hosts skip the per-call permission prompt.
	IsReadOnly bool
	*spec.APISpec
}

// readmeTemplateData wraps APISpec with additional fields for README rendering.
type readmeTemplateData struct {
	*spec.APISpec
	Sources           []ReadmeSource
	DiscoveryPages    []string
	NovelFeatures     []NovelFeature
	Narrative         *ReadmeNarrative
	ProseName         string
	HasDataLayer      bool
	HasAsyncJobs      bool
	HasWriteCommands  bool
	HasAuth           bool
	FreshnessCommands []string
	TrafficAnalysis   *trafficAnalysisTemplateData
	// PromotedResourceNames maps a resource name to true when the generator
	// collapsed that single-endpoint resource into a leaf command. Templates
	// (notably skill.md.tmpl's Command Reference) use this to emit `<cli>
	// <resource>` instead of `<cli> <resource> <endpoint>` — the operation-id
	// path doesn't exist as a registered cobra Use: declaration for promoted
	// resources, so emitting it produces SKILL.md content that the
	// unknown-command verifier rejects.
	PromotedResourceNames map[string]bool
	// PromotedEndpointNames maps a resource name to the single endpoint name
	// that was promoted (e.g. "qr" → "get-qrcode"). Currently informational —
	// templates that need to surface the underlying operation-id can read it.
	PromotedEndpointNames map[string]string
}

type generatorTemplateData struct {
	*spec.APISpec
	TrafficAnalysis *trafficAnalysisTemplateData
}

type trafficAnalysisTemplateData struct {
	TargetURL         string
	EntryCount        int
	APIEntryCount     int
	Reachability      string
	Protocols         []string
	AuthCandidates    []string
	Protections       []string
	GenerationHints   []string
	Warnings          []string
	CandidateCommands []string
}

func (g *Generator) readmeData() *readmeTemplateData {
	// The "sniffed" spec_source is the legacy provenance name for browser-captured
	// specs (produced by the browser-sniff command). Kept for compatibility; a
	// migration to "browser-sniffed" is deferred — see docs/plans/2026-04-18-002.
	if g.Spec.WebsiteURL == "" && g.Spec.SpecSource == "sniffed" && g.Spec.BaseURL != "" {
		if u, err := url.Parse(g.Spec.BaseURL); err == nil && u.Host != "" {
			g.Spec.WebsiteURL = u.Scheme + "://" + u.Host
		}
	}
	return &readmeTemplateData{
		APISpec:               g.Spec,
		Sources:               g.Sources,
		DiscoveryPages:        g.DiscoveryPages,
		NovelFeatures:         g.NovelFeatures,
		Narrative:             g.Narrative,
		ProseName:             g.proseName(),
		HasDataLayer:          g.VisionSet.Store,
		HasAsyncJobs:          len(g.AsyncJobs) > 0,
		HasWriteCommands:      hasWriteCommands(g.Spec.Resources),
		HasAuth:               hasAuth(g.Spec.Auth),
		FreshnessCommands:     g.freshnessCommandPaths(),
		TrafficAnalysis:       g.trafficAnalysisData(),
		PromotedResourceNames: g.PromotedResourceNames,
		PromotedEndpointNames: g.PromotedEndpointNames,
	}
}

// freshnessCommandPaths returns the rendered slice of "covered command paths"
// surfaced in user-facing docs (README.md and SKILL.md) for the freshness
// section. The slice contains only paths whose subcommands actually exist in
// the generated CLI — promoted single-endpoint resources emit only the bare
// `<cli> <resource>` form, multi-endpoint resources emit the bare form plus
// one entry per real endpoint name.
//
// The runtime fallback map in `internal/cli/auto_refresh.go` (rendered by
// auto_refresh.go.tmpl) keeps its `<resource> list/get/search` no-op
// variants because Cobra's argument resolution can land on any of them at
// runtime — having the map accept those forms keeps freshness lookups
// loose. Only the slice rendered into docs needs trimming, so users and
// agents don't see phantom subcommands they can't actually invoke.
func (g *Generator) freshnessCommandPaths() []string {
	if !g.Spec.Cache.Enabled || g.profile == nil {
		return nil
	}
	seen := map[string]struct{}{}
	var paths []string
	add := func(path string) {
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	cliName := naming.CLI(g.Spec.Name)
	for _, resource := range g.profile.SyncableResources {
		prefix := cliName + " " + resource.Name
		// Always emit the bare `<cli> <resource>` form. For promoted
		// single-endpoint resources Cobra resolves this to the leaf
		// command; for multi-endpoint resources it resolves to the
		// parent help. Both are real, reachable paths.
		add(prefix)

		// Promoted resources have only one underlying endpoint and it
		// is wired directly to the bare command — emitting endpoint
		// names would create phantom paths users can't invoke.
		if g.PromotedResourceNames[resource.Name] {
			continue
		}

		// For multi-endpoint resources, emit one entry per real endpoint
		// name. The endpoint map key matches the generated subcommand
		// name (e.g., a `top` endpoint becomes `<cli> stories top`).
		specResource, ok := g.Spec.Resources[resource.Name]
		if !ok {
			continue
		}
		for endpointName := range specResource.Endpoints {
			add(prefix + " " + endpointName)
		}
	}
	for _, command := range g.Spec.Cache.Commands {
		add(cliName + " " + command.Name)
	}
	sort.Strings(paths)
	return paths
}

func (g *Generator) proseName() string {
	if g.Narrative != nil && strings.TrimSpace(g.Narrative.DisplayName) != "" {
		return strings.TrimSpace(g.Narrative.DisplayName)
	}
	return naming.HumanName(g.Spec.Name)
}

func hasAuth(auth spec.AuthConfig) bool {
	return strings.TrimSpace(auth.Type) != "" && auth.Type != "none"
}

func hasWriteCommands(resources map[string]spec.Resource) bool {
	for _, resource := range resources {
		if resourceHasWriteCommand(resource) {
			return true
		}
	}
	return false
}

func resourceHasWriteCommand(resource spec.Resource) bool {
	for name, endpoint := range resource.Endpoints {
		if endpointIsWriteCommand(endpoint, name) {
			return true
		}
	}
	for _, sub := range resource.SubResources {
		if resourceHasWriteCommand(sub) {
			return true
		}
	}
	return false
}

// methodIsWrite is the verb-only fallback. Prefer endpointIsWriteCommand
// when an Endpoint is in hand.
func methodIsWrite(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}

// readOperationIDPrefixes signal a read regardless of HTTP verb. Matched
// against the leading camelCase token of the operation id, case-insensitive.
// Whole-token (not substring) matching avoids false reads on names like
// "getter" or "listenerStart" while still catching "getUser", "listOrders".
var readOperationIDPrefixes = map[string]bool{
	"get":      true,
	"list":     true,
	"search":   true,
	"find":     true,
	"query":    true,
	"count":    true,
	"describe": true,
	"fetch":    true,
}

// writeOperationIDFragments name mutations. When a read-shaped leading token
// is followed by one of these (e.g. getOrCreate, fetchAndUpdate), the
// classifier flips back to write — the leading verb was misleading.
var writeOperationIDFragments = map[string]bool{
	"create": true,
	"update": true,
	"delete": true,
	"remove": true,
	"add":    true,
	"insert": true,
	"set":    true,
	"upsert": true,
	"save":   true,
}

// readBodyParamNames are filter-shape body field names. A POST whose body
// params are entirely drawn from this set is acting as a query, not a
// mutation; mixing in any unknown name flips the endpoint back to write.
var readBodyParamNames = map[string]bool{
	"query":     true,
	"querytext": true,
	"q":         true,
	"filter":    true,
	"filters":   true,
	"limit":     true,
	"offset":    true,
	"from":      true,
	"size":      true,
	"cursor":    true,
	"page":      true,
	"pagesize":  true,
	"sort":      true,
	"sortby":    true,
	"orderby":   true,
}

// endpointIsWriteCommand returns true when the endpoint mutates external
// state. Read signals are checked in cost order: annotation, verb, name
// token, body shape. Fail-closed when none fire so unknown shapes stay
// classified as writes.
//
// opName is the map key from Resource.Endpoints (the operation id).
func endpointIsWriteCommand(endpoint spec.Endpoint, opName string) bool {
	if v, ok := endpoint.Meta["mcp:read-only"]; ok && strings.EqualFold(strings.TrimSpace(v), "true") {
		return false
	}
	if !methodIsWrite(endpoint.Method) {
		return false
	}
	tokens := camelCaseTokens(strings.TrimSpace(opName))
	if len(tokens) > 0 && readOperationIDPrefixes[strings.ToLower(tokens[0])] {
		for _, tok := range tokens[1:] {
			if writeOperationIDFragments[strings.ToLower(tok)] {
				return true
			}
		}
		return false
	}
	return !bodyIsAllFilterShape(endpoint.Body)
}

// endpointIsReadCommand is the inverse of endpointIsWriteCommand.
// Templates use this through the IsReadOnly field on their data
// structs to decide whether to emit Annotations["mcp:read-only"].
// Centralizing the negation keeps "read-only command = not a write
// command" declared in one place; a future definition shift (e.g. a
// new annotation that forces read-only regardless of verb) only needs
// one update site.
func endpointIsReadCommand(endpoint spec.Endpoint, opName string) bool {
	return !endpointIsWriteCommand(endpoint, opName)
}

// camelCaseTokens splits "getOrCreate" → ["get", "Or", "Create"] and
// "searchAll" → ["search", "All"]. Non-letter runes (digits, separators)
// stay attached to the preceding token.
func camelCaseTokens(s string) []string {
	if s == "" {
		return nil
	}
	var tokens []string
	var cur []rune
	for _, r := range s {
		if unicode.IsUpper(r) && len(cur) > 0 {
			tokens = append(tokens, string(cur))
			cur = []rune{r}
			continue
		}
		cur = append(cur, r)
	}
	if len(cur) > 0 {
		tokens = append(tokens, string(cur))
	}
	return tokens
}

// bodyIsAllFilterShape reports whether every body param's name is in
// readBodyParamNames. Returns false for empty bodies so a POST with no body
// (the fail-closed default) stays classified as a write.
func bodyIsAllFilterShape(body []spec.Param) bool {
	if len(body) == 0 {
		return false
	}
	for _, p := range body {
		if !readBodyParamNames[strings.ToLower(strings.TrimSpace(p.Name))] {
			return false
		}
	}
	return true
}

func (g *Generator) templateData() *generatorTemplateData {
	return &generatorTemplateData{
		APISpec:         g.Spec,
		TrafficAnalysis: g.trafficAnalysisData(),
	}
}

func (g *Generator) trafficAnalysisData() *trafficAnalysisTemplateData {
	if g.TrafficAnalysis == nil {
		return nil
	}

	analysis := g.TrafficAnalysis
	data := &trafficAnalysisTemplateData{
		TargetURL:     safeDisplayURL(analysis.Summary.TargetURL),
		EntryCount:    analysis.Summary.EntryCount,
		APIEntryCount: analysis.Summary.APIEntryCount,
	}
	if analysis.Reachability != nil {
		data.Reachability = fmt.Sprintf("%s (%.0f%% confidence)", analysis.Reachability.Mode, analysis.Reachability.Confidence*100)
	}

	for _, protocol := range analysis.Protocols {
		data.Protocols = appendLimited(data.Protocols, fmt.Sprintf("%s (%.0f%% confidence)", protocol.Label, protocol.Confidence*100), 8)
	}
	for _, candidate := range analysis.Auth.Candidates {
		parts := []string{candidate.Type}
		if len(candidate.HeaderNames) > 0 {
			parts = append(parts, "headers: "+strings.Join(candidate.HeaderNames, ", "))
		}
		if len(candidate.QueryNames) > 0 {
			parts = append(parts, "query: "+strings.Join(candidate.QueryNames, ", "))
		}
		if len(candidate.CookieNames) > 0 {
			parts = append(parts, "cookies: "+strings.Join(candidate.CookieNames, ", "))
		}
		data.AuthCandidates = appendLimited(data.AuthCandidates, strings.Join(parts, " — "), 8)
	}
	for _, protection := range analysis.Protections {
		data.Protections = appendLimited(data.Protections, fmt.Sprintf("%s (%.0f%% confidence)", protection.Label, protection.Confidence*100), 8)
	}
	for _, hint := range analysis.GenerationHints {
		data.GenerationHints = appendLimited(data.GenerationHints, hint, 10)
	}
	for _, warning := range analysis.Warnings {
		data.Warnings = appendLimited(data.Warnings, warning.Type+": "+warning.Message, 10)
	}
	for _, command := range analysis.CandidateCommands {
		label := command.Name
		if command.Rationale != "" {
			label += " — " + command.Rationale
		}
		data.CandidateCommands = appendLimited(data.CandidateCommands, label, 8)
	}

	return data
}

func (g *Generator) hasTrafficAnalysisHint(hint string) bool {
	if g == nil || g.TrafficAnalysis == nil {
		return false
	}
	return slices.Contains(g.TrafficAnalysis.GenerationHints, hint)
}

func appendLimited(values []string, value string, limit int) []string {
	value = strings.TrimSpace(value)
	if value == "" || len(values) >= limit {
		return values
	}
	return append(values, value)
}

func safeDisplayURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String()
}

// buildDomainContext constructs structured domain knowledge for MCP agents
// from the spec and profiler output. This is front-loaded context that prevents
// agents from wasting tokens discovering what the API is about.
func (g *Generator) buildDomainContext() DomainContext {
	ctx := DomainContext{
		APIName:     g.Spec.Name,
		Description: naming.OneLine(g.Spec.Description),
		Archetype:   string(profiler.ArchetypeGeneric),
	}

	if g.profile != nil {
		ctx.Archetype = string(g.profile.Domain.Archetype)

		// Build resource summaries with syncable/searchable annotations
		syncSet := make(map[string]bool)
		for _, sr := range g.profile.SyncableResources {
			syncSet[sr.Name] = true
		}

		for rName, r := range g.Spec.Resources {
			rs := ResourceSummary{
				Name:        rName,
				Description: naming.OneLine(r.Description),
				Syncable:    syncSet[rName],
				Searchable:  len(g.profile.SearchableFields[rName]) > 0,
			}
			for eName := range r.Endpoints {
				rs.Endpoints = append(rs.Endpoints, eName)
			}
			sort.Strings(rs.Endpoints)
			ctx.Resources = append(ctx.Resources, rs)
		}
		sort.Slice(ctx.Resources, func(i, j int) bool {
			return ctx.Resources[i].Name < ctx.Resources[j].Name
		})

		// Add query tips based on pagination profile
		if g.profile.Pagination.CursorParam != "" {
			ctx.QueryTips = append(ctx.QueryTips,
				fmt.Sprintf("Pagination uses cursor-based paging. Pass %s parameter for subsequent pages.", g.profile.Pagination.CursorParam))
		}
		if g.profile.Pagination.PageSizeParam != "" {
			ctx.QueryTips = append(ctx.QueryTips,
				fmt.Sprintf("Control page size with the %s parameter (default %d).", g.profile.Pagination.PageSizeParam, g.profile.Pagination.DefaultPageSize))
		}
		if g.profile.Pagination.SinceParam != "" {
			ctx.QueryTips = append(ctx.QueryTips,
				fmt.Sprintf("Use %s for incremental fetches (filter by modification time).", g.profile.Pagination.SinceParam))
		}
	}

	// Add playbook entries from novel features
	for _, nf := range g.NovelFeatures {
		ctx.Playbook = append(ctx.Playbook, PlaybookEntry{
			Topic:   nf.Name,
			Insight: nf.Rationale,
		})
	}

	// Add data layer tips when store is available
	if g.VisionSet.Store {
		ctx.QueryTips = append(ctx.QueryTips,
			"Use the sql tool for ad-hoc analysis on synced data. Run sync first to populate the local database.",
			"Use the search tool for full-text search across all synced resources. Faster than iterating list endpoints.",
			"Prefer sql/search over repeated API calls when the data is already synced.")
	}

	// Add archetype-specific playbook entries — domain opinions that agents
	// can't discover from the API spec alone (PostHog Rule 4: skills are human knowledge)
	if g.profile != nil {
		ctx.Playbook = append(ctx.Playbook, archetypePlaybook(g.profile.Domain.Archetype)...)
	}

	return ctx
}

// archetypePlaybook returns domain-specific insights based on API archetype.
// These are opinionated tips that prevent common agent mistakes.
func archetypePlaybook(arch profiler.DomainArchetype) []PlaybookEntry {
	switch arch {
	case profiler.ArchetypeProjectMgmt:
		return []PlaybookEntry{
			{Topic: "Finding stale work", Insight: "Use the stale command or sql query to find items not updated recently. More reliable than scanning list results manually."},
			{Topic: "Load analysis", Insight: "When analyzing team workload, filter by assignee and status. Raw counts without status filtering are misleading."},
			{Topic: "Bulk operations", Insight: "For bulk status changes, prefer update endpoints over delete+create. Most PM APIs track history on updates."},
		}
	case profiler.ArchetypeCommunication:
		return []PlaybookEntry{
			{Topic: "Message search", Insight: "Use the search tool on synced data rather than paginating through message history. Message APIs often have aggressive rate limits."},
			{Topic: "Channel health", Insight: "When analyzing channel activity, use the channel-health command or sql aggregation on synced messages. Don't iterate individual messages via API."},
		}
	case profiler.ArchetypePayments:
		return []PlaybookEntry{
			{Topic: "Financial data", Insight: "Always use read-only operations for financial queries. Never use create/update tools for payment data without explicit user confirmation."},
			{Topic: "Reconciliation", Insight: "For reconciliation tasks, sync first then use sql for cross-referencing. API pagination over financial records is slow and rate-limited."},
		}
	case profiler.ArchetypeCRM:
		return []PlaybookEntry{
			{Topic: "Contact lookup", Insight: "Use search for finding contacts by name/email. List endpoints return unsorted results and require pagination for large datasets."},
			{Topic: "Activity tracking", Insight: "When checking deal activity, sync first and query locally. CRM APIs often throttle activity-log endpoints heavily."},
		}
	case profiler.ArchetypeDeveloperPlatform:
		return []PlaybookEntry{
			{Topic: "Resource discovery", Insight: "Use list commands to discover available resources before attempting operations. Developer platform APIs often have nested resource hierarchies."},
		}
	default:
		return nil
	}
}

func (g *Generator) prepareOutput() error {
	dirs := []string{
		filepath.Join("cmd", naming.CLI(g.Spec.Name)),
		filepath.Join("internal", "cli"),
		filepath.Join("internal", "cache"),
		filepath.Join("internal", "client"),
		filepath.Join("internal", "cliutil"),
		filepath.Join("internal", "config"),
		filepath.Join("internal", "mcp", "cobratree"),
		filepath.Join("internal", "types"),
	}

	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(g.OutputDir, d), 0755); err != nil {
			return fmt.Errorf("creating dir %s: %w", d, err)
		}
	}

	// Early profiling: compute VisionSet before endpoint rendering so
	// templates can check HasStore for data source resolution.
	if g.VisionSet.IsZero() {
		g.profile = profiler.Profile(g.Spec)
		plan := g.profile.ToVisionaryPlan(g.Spec.Name)
		g.VisionSet = SelectVisionTemplates(plan)
	}
	if g.profile == nil {
		g.profile = profiler.Profile(g.Spec)
	}
	if err := g.validateFreshnessCommandCoverage(); err != nil {
		return err
	}

	// Detect async-job endpoints once per generation. Results flow into
	// per-endpoint template data (for conditional --wait emission) and into
	// the root template (for the jobs command registration).
	if g.AsyncJobs == nil {
		g.AsyncJobs = DetectAsyncJobs(g.Spec)
	}

	// Suffix any param whose Go identifier or cobra flag name would collide
	// with another param on the same endpoint or with a generator-introduced
	// reserved name (pagination's flagAll, async's flagWait*). Must run after
	// AsyncJobs detection so async endpoints reserve the wait identifiers.
	g.dedupeFlagIdentifiers()

	return nil
}

func (g *Generator) renderSingleFiles() error {
	singleFiles := map[string]string{
		"main.go.tmpl":               filepath.Join("cmd", naming.CLI(g.Spec.Name), "main.go"),
		"helpers.go.tmpl":            filepath.Join("internal", "cli", "helpers.go"),
		"doctor.go.tmpl":             filepath.Join("internal", "cli", "doctor.go"),
		"agent_context.go.tmpl":      filepath.Join("internal", "cli", "agent_context.go"),
		"profile.go.tmpl":            filepath.Join("internal", "cli", "profile.go"),
		"deliver.go.tmpl":            filepath.Join("internal", "cli", "deliver.go"),
		"feedback.go.tmpl":           filepath.Join("internal", "cli", "feedback.go"),
		"which.go.tmpl":              filepath.Join("internal", "cli", "which.go"),
		"which_test.go.tmpl":         filepath.Join("internal", "cli", "which_test.go"),
		"config.go.tmpl":             filepath.Join("internal", "config", "config.go"),
		"cache.go.tmpl":              filepath.Join("internal", "cache", "cache.go"),
		"client.go.tmpl":             filepath.Join("internal", "client", "client.go"),
		"cliutil_fanout.go.tmpl":     filepath.Join("internal", "cliutil", "fanout.go"),
		"cliutil_text.go.tmpl":       filepath.Join("internal", "cliutil", "text.go"),
		"cliutil_probe.go.tmpl":      filepath.Join("internal", "cliutil", "probe.go"),
		"cliutil_ratelimit.go.tmpl":  filepath.Join("internal", "cliutil", "ratelimit.go"),
		"cliutil_verifyenv.go.tmpl":  filepath.Join("internal", "cliutil", "verifyenv.go"),
		"cliutil_test.go.tmpl":       filepath.Join("internal", "cliutil", "cliutil_test.go"),
		"cobratree/walker.go.tmpl":   filepath.Join("internal", "mcp", "cobratree", "walker.go"),
		"cobratree/classify.go.tmpl": filepath.Join("internal", "mcp", "cobratree", "classify.go"),
		"cobratree/typemap.go.tmpl":  filepath.Join("internal", "mcp", "cobratree", "typemap.go"),
		"cobratree/shellout.go.tmpl": filepath.Join("internal", "mcp", "cobratree", "shellout.go"),
		"cobratree/cli_path.go.tmpl": filepath.Join("internal", "mcp", "cobratree", "cli_path.go"),
		"cobratree/names.go.tmpl":    filepath.Join("internal", "mcp", "cobratree", "names.go"),
		"types.go.tmpl":              filepath.Join("internal", "types", "types.go"),
		"golangci.yml.tmpl":          ".golangci.yml",
		"readme.md.tmpl":             "README.md",
		"skill.md.tmpl":              "SKILL.md",
		"LICENSE.tmpl":               "LICENSE",
		"NOTICE.tmpl":                "NOTICE",
	}

	for tmplName, outPath := range singleFiles {
		var data any
		switch tmplName {
		case "readme.md.tmpl", "skill.md.tmpl", "which.go.tmpl", "which_test.go.tmpl":
			data = g.readmeData()
		case "helpers.go.tmpl":
			hFlags := computeHelperFlags(g.Spec)
			hFlags.HasDataLayer = g.VisionSet.Store
			data = &helpersTemplateData{
				APISpec:     g.Spec,
				HelperFlags: hFlags,
			}
		case "doctor.go.tmpl":
			data = &doctorTemplateData{
				APISpec:  g.Spec,
				HasStore: g.VisionSet.Store,
			}
		case "client.go.tmpl":
			data = &clientTemplateData{
				APISpec:                    g.Spec,
				HasGraphQLPersistedQueries: g.hasTrafficAnalysisHint("graphql_persisted_query"),
			}
		case "agent_context.go.tmpl":
			data = g.templateData()
		default:
			data = g.Spec
		}
		if err := g.renderTemplate(tmplName, outPath, data); err != nil {
			return fmt.Errorf("rendering %s: %w", tmplName, err)
		}
	}

	return nil
}

func (g *Generator) renderOptionalSupportFiles() error {
	if g.Spec.HasHTMLExtraction() {
		if err := g.renderTemplate("html_extract.go.tmpl", filepath.Join("internal", "cli", "html_extract.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering HTML extraction helper: %w", err)
		}
	}

	// Emit the cliutil freshness helper only when the spec opts into cache
	// or share and the CLI has a local store. Without a store there is
	// nothing to check freshness against; without cache or share opt-in
	// there is no caller that consumes the Decision.
	if g.VisionSet.Store && (g.Spec.Cache.Enabled || g.Spec.Share.Enabled) {
		if err := g.renderTemplate("cliutil_freshness.go.tmpl", filepath.Join("internal", "cliutil", "freshness.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering cliutil freshness: %w", err)
		}
		if err := g.renderTemplate("cliutil_freshness_test.go.tmpl", filepath.Join("internal", "cliutil", "freshness_test.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering cliutil freshness test: %w", err)
		}
	}

	// Emit the cliutil proxypath helper only for proxy-envelope clients —
	// the BuildPath function is the only caller of net/url.Values in the
	// cliutil package, and there's no point shipping it (and its tests)
	// into CLIs that don't speak the proxy-envelope protocol.
	if g.Spec.ClientPattern == "proxy-envelope" {
		if err := g.renderTemplate("cliutil_proxypath.go.tmpl", filepath.Join("internal", "cliutil", "proxypath.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering cliutil proxypath: %w", err)
		}
		if err := g.renderTemplate("cliutil_proxypath_test.go.tmpl", filepath.Join("internal", "cliutil", "proxypath_test.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering cliutil proxypath test: %w", err)
		}
	}

	// Emit the auto-refresh wrapper only when cache is explicitly enabled
	// and the CLI has both a store and a sync path to call. Without sync
	// there is nothing to refresh with; without cache.enabled there is no
	// read-path hook that would call autoRefreshIfStale.
	if g.VisionSet.Store && g.VisionSet.Sync && g.Spec.Cache.Enabled {
		autoRefreshData := struct {
			*spec.APISpec
			SyncableResources []profiler.SyncableResource
			Pagination        profiler.PaginationProfile
		}{
			APISpec:           g.Spec,
			SyncableResources: g.profile.SyncableResources,
			Pagination:        g.profile.Pagination,
		}
		if err := g.renderTemplate("auto_refresh.go.tmpl", filepath.Join("internal", "cli", "auto_refresh.go"), autoRefreshData); err != nil {
			return fmt.Errorf("rendering auto_refresh: %w", err)
		}
	}

	// Emit the git-backed share package only when explicitly enabled and
	// the CLI has a local store. Share requires a SnapshotTables allowlist;
	// spec.Validate has already rejected a missing allowlist with a clear
	// error before we reach this point.
	if g.VisionSet.Store && g.Spec.Share.Enabled {
		if err := os.MkdirAll(filepath.Join(g.OutputDir, "internal", "share"), 0o755); err != nil {
			return fmt.Errorf("creating share dir: %w", err)
		}
		if err := g.renderTemplate("share.go.tmpl", filepath.Join("internal", "share", "share.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering share: %w", err)
		}
		if err := g.renderTemplate("share_test.go.tmpl", filepath.Join("internal", "share", "share_test.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering share test: %w", err)
		}
		if err := g.renderTemplate("share_commands.go.tmpl", filepath.Join("internal", "cli", "share_commands.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering share commands: %w", err)
		}
	}

	if g.FixtureSet != nil {
		if err := g.renderTemplate("captured_test.go.tmpl", filepath.Join("internal", "client", "client_captured_test.go"), g.FixtureSet); err != nil {
			return fmt.Errorf("rendering captured fixture tests: %w", err)
		}
	}

	// For GraphQL specs, emit additional client files (GraphQL transport + query constants)
	if isGraphQLSpec(g.Spec) {
		if err := g.renderTemplate("graphql_client.go.tmpl", filepath.Join("internal", "client", "graphql.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering graphql client: %w", err)
		}
		if err := g.renderTemplate("graphql_queries.go.tmpl", filepath.Join("internal", "client", "queries.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering graphql queries: %w", err)
		}
	}

	// Specs that opt into cost-based throttling (Throttling.Enabled) get
	// the ThrottleState primitives — bucket projection, cost-extension
	// parser, retry helper. Gated by spec opt-in so existing GraphQL
	// CLIs (Linear) and REST CLIs regenerate byte-identically. Lives
	// under internal/client/ alongside graphql.go because the parser is
	// graphql-shaped today; if a REST API ever exposes cost-bucket data
	// in response headers this file would extend to read those, but the
	// surface (ThrottleState, WaitForBudget, HandleThrottleError) is
	// transport-agnostic.
	if g.Spec.HasCostThrottling() {
		if err := g.renderTemplate("throttle.go.tmpl", filepath.Join("internal", "client", "throttle.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering throttle helper: %w", err)
		}
	}

	// Specs that declare per-tenant URL placeholders (e.g. Shopify's
	// "{shop}" / "{version}") get a buildURL helper that resolves the
	// {var} markers against env-backed Config.TemplateVars at request
	// time. Specs without EndpointTemplateVars skip the file so existing
	// CLIs regenerate byte-for-byte.
	if len(g.Spec.EndpointTemplateVars) > 0 {
		if err := g.renderTemplate("url.go.tmpl", filepath.Join("internal", "client", "url.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering url helper: %w", err)
		}
	}

	return nil
}

func (g *Generator) Generate() error {
	warnUnenrichedLargeMCPSurface(g.Spec, os.Stderr)
	if err := g.prepareOutput(); err != nil {
		return err
	}

	// Lifted ahead of any rendering: SKILL/README emission needs promoted-resource
	// awareness so it doesn't emit operation-id-shaped paths (`qr get-qrcode`) for
	// resources the generator collapsed to a leaf (`qr`). buildPromotedCommandPlan
	// is pure over g.Spec, so running it here is identical to running it where it
	// used to live, except the maps are now visible to readmeData() / template
	// rendering.
	g.PromotedCommands, g.PromotedResourceNames, g.PromotedEndpointNames = buildPromotedCommandPlan(g.Spec)

	if err := g.renderSingleFiles(); err != nil {
		return err
	}
	if err := g.renderOptionalSupportFiles(); err != nil {
		return err
	}

	if err := g.renderResourceCommands(g.PromotedResourceNames, g.PromotedEndpointNames); err != nil {
		return err
	}

	if err := g.renderAuthFiles(); err != nil {
		return err
	}
	if err := g.renderMCPEntrypoint(); err != nil {
		return err
	}
	return g.renderVisionAndRootFiles(g.PromotedCommands, g.PromotedResourceNames)
}

// GenerateMCPSurface rewrites the generated MCP entrypoint, tools package,
// cobratree helpers, AND the generator-reserved cliutil package without
// touching the printed CLI's command files. The cliutil package is
// included because the MCP template references helpers (SanitizeErrorBody,
// LooksLikeAuthError) that older library CLIs lack — leaving cliutil
// stale produces "undefined: cliutil.SanitizeErrorBody" build errors on
// regenerated tools.go. Per AGENTS.md, internal/cliutil is generator-
// reserved (agents must not hand-edit it), so unconditional regen here
// is intentionally asymmetric vs the marker-checked tools.go/handlers.go
// paths in mcp-sync. Spec-conditional cliutil files (freshness,
// autoRefresh) stay in renderOptionalSupportFiles so they don't get
// emitted when the spec opts out.
func (g *Generator) GenerateMCPSurface() error {
	if err := g.prepareOutput(); err != nil {
		return err
	}
	g.PromotedCommands, g.PromotedResourceNames, g.PromotedEndpointNames = buildPromotedCommandPlan(g.Spec)
	mcpFiles := map[string]string{
		"cobratree/walker.go.tmpl":   filepath.Join("internal", "mcp", "cobratree", "walker.go"),
		"cobratree/classify.go.tmpl": filepath.Join("internal", "mcp", "cobratree", "classify.go"),
		"cobratree/typemap.go.tmpl":  filepath.Join("internal", "mcp", "cobratree", "typemap.go"),
		"cobratree/shellout.go.tmpl": filepath.Join("internal", "mcp", "cobratree", "shellout.go"),
		"cobratree/cli_path.go.tmpl": filepath.Join("internal", "mcp", "cobratree", "cli_path.go"),
		"cobratree/names.go.tmpl":    filepath.Join("internal", "mcp", "cobratree", "names.go"),
		// cliutil files. Deliberately asymmetric with the marker-checked
		// tools.go / handlers.go / root.go paths elsewhere in mcp-sync:
		// those files can carry hand-edits and require explicit
		// confirmation before overwrite, but cliutil is generator-
		// reserved per AGENTS.md and unconditional regen is the
		// expected contract. Without this, library CLIs whose cliutil
		// predates a helper the new MCP template uses (SanitizeErrorBody,
		// LooksLikeAuthError) fail to build after migration. See the
		// GenerateMCPSurface doc comment for the full rationale.
		"cliutil_fanout.go.tmpl":    filepath.Join("internal", "cliutil", "fanout.go"),
		"cliutil_text.go.tmpl":      filepath.Join("internal", "cliutil", "text.go"),
		"cliutil_probe.go.tmpl":     filepath.Join("internal", "cliutil", "probe.go"),
		"cliutil_ratelimit.go.tmpl": filepath.Join("internal", "cliutil", "ratelimit.go"),
		"cliutil_verifyenv.go.tmpl": filepath.Join("internal", "cliutil", "verifyenv.go"),
	}
	for tmplName, outPath := range mcpFiles {
		if err := g.renderTemplate(tmplName, outPath, g.Spec); err != nil {
			return fmt.Errorf("rendering %s: %w", tmplName, err)
		}
	}
	if err := g.renderMCPEntrypoint(); err != nil {
		return err
	}
	return g.renderMCPToolFiles(g.schemaWithDependentParents())
}

func buildPromotedCommandPlan(apiSpec *spec.APISpec) ([]PromotedCommand, map[string]bool, map[string]string) {
	// Compute promoted commands early — needed to determine Hidden flag on parent commands
	promotedCommands := buildPromotedCommands(apiSpec)

	// Build set of resource names that have promoted commands. Promoted commands
	// replace the resource parent entirely — the promoted command wires sibling
	// endpoints and sub-resources directly. Generating the unused parent would
	// create a dead constructor (e.g., newBookingsCmd never called).
	promotedResourceNames := make(map[string]bool)
	// Map resource name → promoted endpoint name. The promoted command's RunE
	// inlines this endpoint's logic, so the standalone file is dead code.
	promotedEndpointNames := make(map[string]string)
	for _, pc := range promotedCommands {
		promotedResourceNames[pc.ResourceName] = true
		promotedEndpointNames[pc.ResourceName] = pc.EndpointName
	}

	return promotedCommands, promotedResourceNames, promotedEndpointNames
}

func (g *Generator) renderResourceCommands(promotedResourceNames map[string]bool, promotedEndpointNames map[string]string) error {
	// Generate per-resource parent files + per-endpoint command files
	// This produces more files (one per endpoint) which improves Breadth scoring
	for name, resource := range g.Spec.Resources {
		// Skip parent file for promoted resources — the promoted command replaces it.
		// Sub-resource parents and endpoint files are still needed (wired by the promoted command).
		if !promotedResourceNames[name] {
			// Parent file: wires subcommands together
			parentData := struct {
				ResourceName string
				FuncPrefix   string
				CommandPath  string
				Resource     spec.Resource
				Hidden       bool
				*spec.APISpec
			}{
				ResourceName: name,
				FuncPrefix:   name,
				CommandPath:  name,
				Resource:     resource,
				Hidden:       false,
				APISpec:      g.Spec,
			}
			parentPath := filepath.Join("internal", "cli", safeResourceFileStem(name)+".go")
			if err := g.renderTemplate("command_parent.go.tmpl", parentPath, parentData); err != nil {
				return fmt.Errorf("rendering parent command %s: %w", name, err)
			}
		}

		// Per-endpoint files
		for eName, endpoint := range resource.Endpoints {
			// Skip the promoted endpoint — its logic is inlined in the promoted command's RunE.
			if promotedEndpointNames[name] == eName {
				continue
			}
			asyncInfo, isAsync := g.AsyncJobs[name+"/"+eName]
			epData := endpointTemplateData{
				ResourceName:    name,
				ResourceBaseURL: strings.TrimRight(resource.BaseURL, "/"),
				FuncPrefix:      name,
				CommandPath:     name,
				EndpointName:    eName,
				Endpoint:        endpoint,
				HasStore:        g.VisionSet.Store,
				IsAsync:         isAsync,
				Async:           asyncInfo,
				IsReadOnly:      endpointIsReadCommand(endpoint, eName),
				APISpec:         g.Spec,
			}
			epPath := filepath.Join("internal", "cli", safeResourceFileStem(name+"_"+eName)+".go")
			if err := g.renderTemplate("command_endpoint.go.tmpl", epPath, epData); err != nil {
				return fmt.Errorf("rendering endpoint %s/%s: %w", name, eName, err)
			}
		}

		// Sub-resource parent + endpoint files
		for subName, subResource := range resource.SubResources {
			// Skip single-endpoint sub-resource parents under promoted resources.
			// The promoted command wires the endpoint directly, making the parent dead code.
			// Multi-endpoint sub-resource parents are still needed (the promoted command uses them).
			skipSubParent := promotedResourceNames[name] && len(subResource.Endpoints) == 1
			if !skipSubParent {
				subParentData := struct {
					ResourceName string
					FuncPrefix   string
					CommandPath  string
					Resource     spec.Resource
					Hidden       bool
					*spec.APISpec
				}{
					ResourceName: subName,
					FuncPrefix:   name + "-" + subName,
					CommandPath:  name + " " + subName,
					Resource:     subResource,
					Hidden:       false,
					APISpec:      g.Spec,
				}
				subParentPath := filepath.Join("internal", "cli", safeResourceFileStem(name+"_"+subName)+".go")
				if err := g.renderTemplate("command_parent.go.tmpl", subParentPath, subParentData); err != nil {
					return fmt.Errorf("rendering sub-parent %s/%s: %w", name, subName, err)
				}
			}

			// Sub-resources inherit the parent's BaseURL override; an
			// explicit sub_resource.base_url wins. Falls through to the
			// spec-level BaseURL when both are empty. Trailing slash is
			// trimmed so the template's `path := <base><endpoint.path>`
			// concat doesn't produce `https://x.com/v1//search` when the
			// override and endpoint path both carry slashes.
			subResourceBaseURL := subResource.BaseURL
			if subResourceBaseURL == "" {
				subResourceBaseURL = resource.BaseURL
			}
			subResourceBaseURL = strings.TrimRight(subResourceBaseURL, "/")
			for eName, endpoint := range subResource.Endpoints {
				subKey := subName + "/" + eName
				asyncInfo, isAsync := g.AsyncJobs[subKey]
				epData := endpointTemplateData{
					ResourceName:    subName,
					ResourceBaseURL: subResourceBaseURL,
					FuncPrefix:      name + "-" + subName,
					CommandPath:     name + " " + subName,
					EndpointName:    eName,
					Endpoint:        endpoint,
					HasStore:        g.VisionSet.Store,
					IsAsync:         isAsync,
					Async:           asyncInfo,
					IsReadOnly:      endpointIsReadCommand(endpoint, eName),
					APISpec:         g.Spec,
				}
				epPath := filepath.Join("internal", "cli", safeResourceFileStem(name+"_"+subName+"_"+eName)+".go")
				if err := g.renderTemplate("command_endpoint.go.tmpl", epPath, epData); err != nil {
					return fmt.Errorf("rendering sub-endpoint %s/%s/%s: %w", name, subName, eName, err)
				}
			}
		}
	}

	return nil
}

func (g *Generator) renderAuthFiles() error {
	// Skip auth.go entirely when the spec declares no auth surface. See
	// shouldEmitAuth for the predicate; the matching root.go template gate
	// (HasAuthCommand) and the scorecard's no-auth exemption (scoreAuth)
	// must stay in sync with this same condition.
	if !g.shouldEmitAuth() {
		return nil
	}
	// Render auth command - use full OAuth2 template when authorization URL is present,
	// browser cookie template for cookie-auth APIs, otherwise simple token-management template
	authPath := filepath.Join("internal", "cli", "auth.go")
	authTmpl := "auth_simple.go.tmpl"
	if g.Spec.Auth.AuthorizationURL != "" {
		authTmpl = "auth.go.tmpl"
	} else if g.Spec.Auth.Type == "cookie" || g.Spec.Auth.Type == "composed" || g.hasTrafficAnalysisHint("graphql_persisted_query") {
		// Select the browser-aware auth template for browser-cookie auth or a
		// persisted-query registry, even for auth.type:none. Query refresh flows
		// need temporary browser capture support, not a resident browser transport.
		authTmpl = "auth_browser.go.tmpl"
	}
	authData := &authTemplateData{
		APISpec:                    g.Spec,
		HasGraphQLPersistedQueries: g.hasTrafficAnalysisHint("graphql_persisted_query"),
	}
	if err := g.renderTemplate(authTmpl, authPath, authData); err != nil {
		return fmt.Errorf("rendering auth: %w", err)
	}

	// For session_handshake auth, emit the session manager helper alongside
	// the client. This was previously hand-patched in every CLI that used a
	// crumb/CSRF-token pattern (yahoo-finance and any future reverse-engineered
	// API with anti-CSRF on JSON endpoints). See retro issue #174 WU-2.
	if g.Spec.Auth.Type == "session_handshake" {
		sessionPath := filepath.Join("internal", "client", "session.go")
		if err := g.renderTemplate("session_handshake.go.tmpl", sessionPath, g.Spec); err != nil {
			return fmt.Errorf("rendering session manager: %w", err)
		}
	}

	return nil
}

// shouldEmitAuth reports whether the generator should emit internal/cli/auth.go
// for this spec. Auth UI is emitted when the spec describes a real auth
// surface: a non-none auth.type, an AuthorizationURL (OAuth), or a
// graphql_persisted_query traffic-analysis hint (browser-aware refresh).
//
// Public-data specs (auth.type: "none", no OAuth, no GraphQL persisted-query)
// previously shipped a dead `auth set-token / status / logout` subcommand.
// Now they ship without it, root.go skips the registration via
// HasAuthCommand, and scoreAuth exempts them from the "no auth subcommand"
// deduction. All three call sites must agree -- they call this method.
func (g *Generator) shouldEmitAuth() bool {
	return g.Spec.Auth.Type != "none" ||
		g.Spec.Auth.AuthorizationURL != "" ||
		g.hasTrafficAnalysisHint("graphql_persisted_query")
}

func (g *Generator) renderMCPEntrypoint() error {
	// MCP server: generate cmd/{name}-pp-mcp/ entry point and internal/mcp/ package
	if g.VisionSet.MCP || true { // Always generate MCP for now
		mcpDirs := []string{
			filepath.Join("cmd", naming.MCP(g.Spec.Name)),
			filepath.Join("internal", "mcp"),
		}
		for _, d := range mcpDirs {
			if err := os.MkdirAll(filepath.Join(g.OutputDir, d), 0755); err != nil {
				return fmt.Errorf("creating MCP dir %s: %w", d, err)
			}
		}
		if err := g.renderTemplate("main_mcp.go.tmpl", filepath.Join("cmd", naming.MCP(g.Spec.Name), "main.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering MCP main: %w", err)
		}
	}

	return nil
}

func (g *Generator) renderVisionAndRootFiles(promotedCommands []PromotedCommand, promotedResourceNames map[string]bool) error {
	schema := g.schemaWithDependentParents()

	if err := g.renderStoreFiles(schema); err != nil {
		return err
	}

	visionData := g.visionRenderData(schema)
	if err := g.renderVisionCommands(visionData); err != nil {
		return err
	}
	workflowConstructors, err := g.renderWorkflowFiles(visionData)
	if err != nil {
		return err
	}
	insightConstructors := g.renderInsightFiles()

	if err := g.renderMCPToolFiles(schema); err != nil {
		return err
	}
	if err := g.renderPromotedCommandFiles(promotedCommands); err != nil {
		return err
	}

	return g.renderRootProjectFiles(promotedCommands, promotedResourceNames, workflowConstructors, insightConstructors)
}

func (g *Generator) schemaWithDependentParents() []TableDef {
	schema := BuildSchema(g.Spec)

	// Add parent_id column to tables for dependent (parent-child) sync resources
	if g.profile != nil {
		depSet := make(map[string]bool)
		for _, dep := range g.profile.DependentSyncResources {
			depSet[dep.Name] = true
		}
		for i, table := range schema {
			if depSet[table.Name] {
				hasParentID := false
				for _, col := range table.Columns {
					if col.Name == "parent_id" {
						hasParentID = true
						break
					}
				}
				if !hasParentID {
					schema[i].Columns = append(schema[i].Columns, ColumnDef{
						Name: "parent_id",
						Type: "TEXT",
					})
					schema[i].Indexes = append(schema[i].Indexes, IndexDef{
						Name:      "idx_" + table.Name + "_parent_id",
						TableName: table.Name,
						Columns:   "parent_id",
					})
				}
			}
		}
	}

	return schema
}

func (g *Generator) renderStoreFiles(schema []TableDef) error {
	// Create store directory if needed
	if g.VisionSet.Store {
		if err := os.MkdirAll(filepath.Join(g.OutputDir, "internal", "store"), 0755); err != nil {
			return fmt.Errorf("creating store dir: %w", err)
		}
		storeData := struct {
			*spec.APISpec
			SyncableResources      []profiler.SyncableResource
			DependentSyncResources []profiler.DependentResource
			SearchableFields       map[string][]string
			Tables                 []TableDef
		}{
			APISpec:                g.Spec,
			SyncableResources:      g.profile.SyncableResources,
			DependentSyncResources: g.profile.DependentSyncResources,
			SearchableFields:       g.profile.SearchableFields,
			Tables:                 schema,
		}
		if err := g.renderTemplate("store.go.tmpl", filepath.Join("internal", "store", "store.go"), storeData); err != nil {
			return fmt.Errorf("rendering store: %w", err)
		}
		if err := g.renderTemplate("store_schema_version_test.go.tmpl", filepath.Join("internal", "store", "schema_version_test.go"), storeData); err != nil {
			return fmt.Errorf("rendering store schema version test: %w", err)
		}
		if err := g.renderTemplate("store_upsert_batch_test.go.tmpl", filepath.Join("internal", "store", "upsert_batch_test.go"), storeData); err != nil {
			return fmt.Errorf("rendering store upsert batch test: %w", err)
		}
	}

	return nil
}

type visionRenderData struct {
	*spec.APISpec
	SyncableResources      []profiler.SyncableResource
	DependentSyncResources []profiler.DependentResource
	SearchableFields       map[string][]string
	Tables                 []TableDef
	Pagination             profiler.PaginationProfile
	SearchEndpointPath     string
	SearchQueryParam       string
	SearchEndpointMethod   string
	SearchBodyFields       []profiler.SearchBodyField
	GraphQLFieldPaths      map[string]string
}

func (g *Generator) visionRenderData(schema []TableDef) visionRenderData {
	gqlFieldPaths := map[string]string{}
	for rName, r := range g.Spec.Resources {
		if ep, ok := r.Endpoints["list"]; ok && ep.ResponsePath != "" {
			gqlFieldPaths[rName] = graphqlQueryField(ep.ResponsePath)
		}
	}

	return visionRenderData{
		APISpec:                g.Spec,
		SyncableResources:      g.profile.SyncableResources,
		DependentSyncResources: g.profile.DependentSyncResources,
		SearchableFields:       g.profile.SearchableFields,
		Tables:                 schema,
		Pagination:             g.profile.Pagination,
		SearchEndpointPath:     g.profile.SearchEndpointPath,
		SearchQueryParam:       g.profile.SearchQueryParam,
		SearchEndpointMethod:   g.profile.SearchEndpointMethod,
		SearchBodyFields:       g.profile.SearchBodyFields,
		GraphQLFieldPaths:      gqlFieldPaths,
	}
}

func (g *Generator) renderVisionCommands(visionData visionRenderData) error {
	// Render vision CLI commands
	visionCmds := map[string]string{
		"export.go.tmpl":    filepath.Join("internal", "cli", "export.go"),
		"import.go.tmpl":    filepath.Join("internal", "cli", "import.go"),
		"search.go.tmpl":    filepath.Join("internal", "cli", "search.go"),
		"sync.go.tmpl":      filepath.Join("internal", "cli", "sync.go"),
		"tail.go.tmpl":      filepath.Join("internal", "cli", "tail.go"),
		"analytics.go.tmpl": filepath.Join("internal", "cli", "analytics.go"),
	}

	gqlSpec := isGraphQLSpec(g.Spec)
	for _, tmplName := range g.VisionSet.TemplateNames() {
		if tmplName == "store.go.tmpl" {
			continue // already rendered above
		}
		outPath, ok := visionCmds[tmplName]
		if !ok {
			continue
		}
		// For GraphQL specs, use the GraphQL sync template instead of the REST one
		actualTmpl := tmplName
		if tmplName == "sync.go.tmpl" && gqlSpec {
			actualTmpl = "graphql_sync.go.tmpl"
		}
		var tmplData any = g.Spec
		if tmplName == "sync.go.tmpl" || tmplName == "search.go.tmpl" {
			tmplData = visionData
		}
		if err := g.renderTemplate(actualTmpl, outPath, tmplData); err != nil {
			return fmt.Errorf("rendering vision %s: %w", tmplName, err)
		}
	}

	return nil
}

func (g *Generator) renderWorkflowFiles(visionData visionRenderData) ([]string, error) {
	// Render data source resolution template when store is enabled
	if g.VisionSet.Store {
		if err := g.renderTemplate("data_source.go.tmpl", filepath.Join("internal", "cli", "data_source.go"), visionData); err != nil {
			return nil, fmt.Errorf("rendering data_source: %w", err)
		}
	}

	// Render workflow template when store is enabled (root.go registers it conditionally on VisionSet.Store)
	if g.VisionSet.Store {
		workflowData := struct {
			*spec.APISpec
			SyncableResources []profiler.SyncableResource
			SearchableFields  map[string][]string
		}{
			APISpec:           g.Spec,
			SyncableResources: g.profile.SyncableResources,
			SearchableFields:  g.profile.SearchableFields,
		}
		if err := g.renderTemplate("channel_workflow.go.tmpl", filepath.Join("internal", "cli", "channel_workflow.go"), workflowData); err != nil {
			return nil, fmt.Errorf("rendering workflow: %w", err)
		}
	}

	var renderedWorkflowConstructors []string
	// Render domain-specific workflow templates
	for _, tmpl := range g.VisionSet.Workflows {
		outName := strings.TrimSuffix(filepath.Base(tmpl), ".tmpl")
		outPath := filepath.Join("internal", "cli", outName)
		if err := g.renderTemplate(tmpl, outPath, g.Spec); err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping workflow template %s: %v\n", tmpl, err)
			continue
		}
		if constructor := commandConstructorForTemplate(tmpl); constructor != "" {
			renderedWorkflowConstructors = append(renderedWorkflowConstructors, constructor)
		}
	}

	return renderedWorkflowConstructors, nil
}

func (g *Generator) renderInsightFiles() []string {
	var renderedInsightConstructors []string

	// Render insight templates
	for _, tmpl := range g.VisionSet.Insights {
		outName := strings.TrimSuffix(filepath.Base(tmpl), ".tmpl")
		outPath := filepath.Join("internal", "cli", outName)
		if err := g.renderTemplate(tmpl, outPath, g.Spec); err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping insight template %s: %v\n", tmpl, err)
			continue
		}
		if constructor := commandConstructorForTemplate(tmpl); constructor != "" {
			renderedInsightConstructors = append(renderedInsightConstructors, constructor)
		}
	}

	return renderedInsightConstructors
}

func (g *Generator) renderMCPToolFiles(schema []TableDef) error {
	// Render MCP tools registration (needs VisionSet + store data + tool counts for annotations)
	if g.VisionSet.MCP {
		mcpTotal, mcpPublic := g.Spec.CountMCPTools()
		domainCtx := g.buildDomainContext()
		mcpData := struct {
			*spec.APISpec
			SyncableResources []profiler.SyncableResource
			SearchableFields  map[string][]string
			Tables            []TableDef
			VisionSet         VisionTemplateSet
			MCPTotalCount     int
			MCPPublicCount    int
			NovelFeatures     []NovelFeature
			DomainContext     DomainContext
		}{
			APISpec:           g.Spec,
			SyncableResources: g.profile.SyncableResources,
			SearchableFields:  g.profile.SearchableFields,
			Tables:            schema,
			VisionSet:         g.VisionSet,
			MCPTotalCount:     mcpTotal,
			MCPPublicCount:    mcpPublic,
			NovelFeatures:     g.NovelFeatures,
			DomainContext:     domainCtx,
		}
		if err := g.renderTemplate("mcp_tools.go.tmpl", filepath.Join("internal", "mcp", "tools.go"), mcpData); err != nil {
			return fmt.Errorf("rendering MCP tools: %w", err)
		}
		if len(g.Spec.MCP.Intents) > 0 {
			if err := g.renderTemplate("mcp_intents.go.tmpl", filepath.Join("internal", "mcp", "intents.go"), mcpData); err != nil {
				return fmt.Errorf("rendering MCP intents: %w", err)
			}
		}
		if g.Spec.MCP.IsCodeOrchestration() {
			if err := g.renderTemplate("mcp_code_orch.go.tmpl", filepath.Join("internal", "mcp", "code_orch.go"), mcpData); err != nil {
				return fmt.Errorf("rendering MCP code-orchestration: %w", err)
			}
		}
	}

	return nil
}

func (g *Generator) renderPromotedCommandFiles(promotedCommands []PromotedCommand) error {
	// Generate api discovery command when promoted commands exist (lets users browse the raw generated surface)
	if len(promotedCommands) > 0 {
		if err := g.renderTemplate("api_discovery.go.tmpl", filepath.Join("internal", "cli", "api_discovery.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering api discovery: %w", err)
		}
	}

	// Generate promoted top-level commands (user-friendly aliases for nested API commands)
	// promotedCommands was computed earlier so promoted resources can replace their raw parents.
	for _, pc := range promotedCommands {
		// Look up the full resource to pass sibling endpoints/sub-resources.
		// Trim trailing slash on BaseURL so the promoted handler's
		// `path := <Resource.BaseURL><Endpoint.Path>` concat doesn't
		// produce `https://x.com/v1//search`.
		resource := g.Spec.Resources[pc.ResourceName]
		resource.BaseURL = strings.TrimRight(resource.BaseURL, "/")
		promotedData := struct {
			PromotedName string
			ResourceName string
			EndpointName string
			Endpoint     spec.Endpoint
			HasStore     bool
			Resource     spec.Resource
			FuncPrefix   string
			IsReadOnly   bool
			*spec.APISpec
		}{
			PromotedName: pc.PromotedName,
			ResourceName: pc.ResourceName,
			EndpointName: pc.EndpointName,
			Endpoint:     pc.Endpoint,
			HasStore:     g.VisionSet.Store,
			Resource:     resource,
			FuncPrefix:   pc.ResourceName,
			IsReadOnly:   endpointIsReadCommand(pc.Endpoint, pc.EndpointName),
			APISpec:      g.Spec,
		}
		promotedPath := filepath.Join("internal", "cli", "promoted_"+pc.PromotedName+".go")
		if err := g.renderTemplate("command_promoted.go.tmpl", promotedPath, promotedData); err != nil {
			return fmt.Errorf("rendering promoted command %s: %w", pc.PromotedName, err)
		}
	}

	return nil
}

func (g *Generator) renderRootProjectFiles(promotedCommands []PromotedCommand, promotedResourceNames map[string]bool, renderedWorkflowConstructors, renderedInsightConstructors []string) error {
	// Root --help Long surfaces ALL verified-built novel features — the
	// whole point of this change is to stop making agents do discovery
	// for novel capabilities. A count cap (earlier draft used 3) neuters
	// the thesis for CLIs with genuinely many novel features, which are
	// the CLIs that benefit most from the absorb work in the first place.
	//
	// Size is bounded two ways:
	//   1. per-line truncation via the template's truncate helper (200 runes)
	//   2. a soft cap on total feature lines rendered (MaxHighlightLines);
	//      overflow becomes a "…and N more — see README" breadcrumb so a
	//      verbose absorb output doesn't blow up --help
	const maxHighlightLines = 15 // ~3000-char description ceiling in the worst case
	shownNovel := g.NovelFeatures
	overflow := 0
	if len(shownNovel) > maxHighlightLines {
		overflow = len(shownNovel) - maxHighlightLines
		shownNovel = shownNovel[:maxHighlightLines]
	}
	// HasAuthCommand mirrors shouldEmitAuth. The template uses it to gate
	// rootCmd.AddCommand(newAuthCmd) so the root binary does not reference an
	// undefined symbol when auth.go was skipped.
	hasAuthCommand := g.shouldEmitAuth()

	rootData := struct {
		*spec.APISpec
		VisionSet             VisionTemplateSet
		VisionCmdNames        map[string]bool
		WorkflowConstructors  []string
		InsightConstructors   []string
		PromotedCommands      []PromotedCommand
		PromotedResourceNames map[string]bool
		Narrative             *ReadmeNarrative
		TopNovelFeatures      []NovelFeature
		NovelOverflowCount    int
		HasAsyncJobs          bool
		AsyncJobCount         int
		HasAuthCommand        bool
	}{
		APISpec:               g.Spec,
		VisionSet:             g.VisionSet,
		VisionCmdNames:        g.VisionSet.CmdNames(),
		WorkflowConstructors:  renderedWorkflowConstructors,
		InsightConstructors:   renderedInsightConstructors,
		PromotedCommands:      promotedCommands,
		PromotedResourceNames: promotedResourceNames,
		Narrative:             g.Narrative,
		TopNovelFeatures:      shownNovel,
		NovelOverflowCount:    overflow,
		HasAsyncJobs:          len(g.AsyncJobs) > 0,
		AsyncJobCount:         len(g.AsyncJobs),
		HasAuthCommand:        hasAuthCommand,
	}
	if err := g.renderTemplate("root.go.tmpl", filepath.Join("internal", "cli", "root.go"), rootData); err != nil {
		return fmt.Errorf("rendering root: %w", err)
	}
	if len(g.AsyncJobs) > 0 {
		jobsData := struct {
			*spec.APISpec
			AsyncJobs map[string]AsyncJobInfo
		}{
			APISpec:   g.Spec,
			AsyncJobs: g.AsyncJobs,
		}
		if err := g.renderTemplate("jobs.go.tmpl", filepath.Join("internal", "cli", "jobs.go"), jobsData); err != nil {
			return fmt.Errorf("rendering jobs: %w", err)
		}
	}
	if err := g.renderTemplate("go.mod.tmpl", "go.mod", rootData); err != nil {
		return fmt.Errorf("rendering go.mod: %w", err)
	}
	if err := g.renderTemplate("makefile.tmpl", "Makefile", rootData); err != nil {
		return fmt.Errorf("rendering Makefile: %w", err)
	}
	if err := g.renderTemplate("goreleaser.yaml.tmpl", ".goreleaser.yaml", rootData); err != nil {
		return fmt.Errorf("rendering goreleaser: %w", err)
	}

	return nil
}

func (g *Generator) validateFreshnessCommandCoverage() error {
	if !g.Spec.Cache.Enabled || len(g.Spec.Cache.Commands) == 0 {
		return nil
	}
	syncable := make(map[string]struct{}, len(g.profile.SyncableResources))
	for _, resource := range g.profile.SyncableResources {
		syncable[resource.Name] = struct{}{}
	}
	for _, command := range g.Spec.Cache.Commands {
		if _, collides := generatedFreshnessCommandNames(command.Name, syncable); collides {
			return fmt.Errorf("cache.commands[%s]: command path is already covered by generated resource freshness", command.Name)
		}
		for _, resource := range command.Resources {
			if _, ok := syncable[resource]; !ok {
				return fmt.Errorf("cache.commands[%s]: resource %q is not syncable and cannot be auto-refreshed", command.Name, resource)
			}
		}
	}
	return nil
}

func generatedFreshnessCommandNames(name string, syncable map[string]struct{}) (string, bool) {
	parts := strings.Fields(name)
	if len(parts) == 0 {
		return "", false
	}
	if _, ok := syncable[parts[0]]; !ok {
		return "", false
	}
	if len(parts) == 1 {
		return parts[0], true
	}
	if len(parts) == 2 {
		switch parts[1] {
		case "list", "get", "search":
			return strings.Join(parts, " "), true
		}
	}
	return "", false
}

func commandConstructorForTemplate(tmpl string) string {
	switch filepath.Base(tmpl) {
	case "pm_stale.go.tmpl":
		return "Stale"
	case "pm_orphans.go.tmpl":
		return "Orphans"
	case "pm_load.go.tmpl":
		return "Load"
	case "health_score.go.tmpl":
		return "Health"
	case "similar.go.tmpl":
		return "Similar"
	default:
		return ""
	}
}

func (g *Generator) renderTemplate(tmplName, outPath string, data any) error {
	tmpl, err := g.template(tmplName)
	if err != nil {
		return err
	}

	fullPath := filepath.Join(g.OutputDir, outPath)
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("executing template %s: %w", tmplName, err)
	}
	if err := validateRenderedArtifact(outPath, buf.String()); err != nil {
		return err
	}

	return os.WriteFile(fullPath, buf.Bytes(), 0o644)
}

func validateRenderedArtifact(outPath, content string) error {
	switch filepath.Base(outPath) {
	case "README.md", "SKILL.md":
	default:
		return nil
	}
	for _, marker := range []string{"<cli>-pp-cli", "~/.<cli>-pp-cli", "<CLI>_", "{{.Name}}"} {
		if strings.Contains(content, marker) {
			return fmt.Errorf("%s contains unsubstituted placeholder %q", outPath, marker)
		}
	}
	if err := scanForControlBytes(outPath, content); err != nil {
		return err
	}
	return nil
}

// scanForControlBytes rejects any rendered markdown that contains ASCII
// control bytes outside the small set legitimately used in text:
// 0x09 (tab), 0x0A (LF), 0x0D (CR). Everything else in 0x00-0x1F is
// rejected with the file path, byte offset, and a hint about the most
// likely cause.
//
// Why: research.json values flow through Go's encoding/json which honors
// JSON escape sequences like "\b" (0x08 backspace), "\f" (0x0C form
// feed), etc. When an agent author writes a regex literal as
// `"command": "...\bGo\b..."` (intending the literal characters
// `\` `b` `G` `o` `\` `b`) the JSON parser yields a string containing
// real backspace bytes, and the template engine writes those bytes
// straight into SKILL.md / README.md. The result renders as nothing in
// most viewers — silent corruption.
//
// The fix runs at render time (not at JSON parse time) so it catches
// every future class of escape mistake, not just the regex case that
// surfaced it. A targeted check on `narrative.recipes[].command`
// alone would only catch this one path.
//
// Surfaced by hackernews retro #350 finding F2.
func scanForControlBytes(outPath, content string) error {
	for i := 0; i < len(content); i++ {
		b := content[i]
		// Tab (0x09), LF (0x0A), CR (0x0D) are allowed in markdown.
		// Everything else in 0x00-0x1F is forbidden.
		if b > 0x1F || b == 0x09 || b == 0x0A || b == 0x0D {
			continue
		}
		hint := "likely cause: a JSON-parsed field (e.g. narrative.recipes[].command) contained \"\\b\", \"\\f\", or another JSON escape that became a control byte. Double-escape backslashes in regex literals: \"\\\\b\" not \"\\b\"."
		return fmt.Errorf("%s contains forbidden control byte 0x%02X at offset %d. %s", outPath, b, i, hint)
	}
	return nil
}

func (g *Generator) template(tmplName string) (*template.Template, error) {
	if tmpl, ok := g.templates[tmplName]; ok {
		return tmpl, nil
	}

	content, err := templateFS.ReadFile(filepath.Join("templates", tmplName))
	if err != nil {
		return nil, fmt.Errorf("reading template %s: %w", tmplName, err)
	}

	tmpl, err := template.New(tmplName).Funcs(g.funcs).Parse(string(content))
	if err != nil {
		return nil, fmt.Errorf("parsing template %s: %w", tmplName, err)
	}

	g.templates[tmplName] = tmpl
	return tmpl, nil
}

// Template helper functions.
//
// These run inside Go templates over parsed APISpec data, so their inputs
// have already been ASCII-folded by the openapi/graphql parsers (which
// route raw spec strings through naming.ASCIIFold at sanitizeTypeName,
// toCamelCase, etc). Treat any new caller that feeds raw spec strings
// directly into these helpers as a bug — fold first, then shape.
func toCamel(s string) string {
	// Strip characters that are invalid in Go identifiers
	s = strings.TrimLeft(s, "$")
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	result := strings.Join(parts, "")
	// Ensure starts with letter
	if len(result) > 0 && !unicode.IsLetter(rune(result[0])) {
		result = "V" + result
	}
	return result
}

func toPascal(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for i, part := range parts {
		if part == "" {
			continue
		}
		lower := strings.ToLower(part)
		parts[i] = strings.ToUpper(lower[:1]) + lower[1:]
	}
	result := strings.Join(parts, "")
	if len(result) > 0 && !unicode.IsLetter(rune(result[0])) {
		result = "V" + result
	}
	return result
}

func domainUpsertMethodName(tableName string) string {
	return "Upsert" + toPascal(tableName)
}

// isIDParam returns true if the parameter name suggests it's an identifier
// that should be typed as string regardless of the spec's declared type.
// IDs like steamid (17-digit number) overflow int64, and zero-value confusion
// makes IntVar unsuitable for identifiers.
func isIDParam(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, "id") || strings.HasSuffix(lower, "ids") ||
		strings.HasSuffix(lower, "_id") || strings.HasSuffix(lower, "_ids") ||
		lower == "steamid" || lower == "steamids"
}

// isCursorParam returns true if the parameter name suggests it's a pagination
// cursor, page, offset, or timestamp that should be typed as string regardless
// of the spec's declared numeric type. Spec'd as `number` or `integer`, these
// values cross into scientific-notation territory (~10^6 and above) when Go's
// default float formatter renders them, which upstream APIs reject as invalid
// integer cursors. String typing handles opaque tokens, numeric strings, and
// integer literals uniformly.
func isCursorParam(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "cursor", "min_cursor", "max_cursor", "next_cursor",
		"page", "page_token", "next_page_token",
		"page[cursor]", "min_time", "max_time", "offset":
		return true
	}
	return false
}

func primitiveKind(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "string":
		return "string"
	case "integer", "int":
		return "int"
	case "boolean", "bool":
		return "bool"
	case "number", "float":
		return "float"
	case "object":
		return "object"
	case "array":
		return "array"
	default:
		return "string"
	}
}

func goType(t string) string {
	switch primitiveKind(t) {
	case "string":
		return "string"
	case "int":
		return "int"
	case "bool":
		return "bool"
	case "float":
		return "float64"
	default:
		return "string"
	}
}

// goStructType returns the Go type for a struct field definition.
// Unlike goType (used for CLI flags which are always primitives),
// this maps object/array types to json.RawMessage for type fidelity.
func goStructType(t string) string {
	switch primitiveKind(t) {
	case "object", "array":
		return "json.RawMessage"
	default:
		return goType(t)
	}
}

func goStoreType(sqlType string) string {
	upper := strings.ToUpper(sqlType)
	switch {
	case strings.HasPrefix(upper, "INTEGER"):
		return "int"
	case strings.HasPrefix(upper, "REAL"):
		return "float64"
	case strings.HasPrefix(upper, "JSON"):
		return "json.RawMessage"
	case strings.HasPrefix(upper, "DATETIME"):
		return "string"
	default:
		return "string"
	}
}

func camelToJSON(s string) string {
	parts := strings.Split(strings.ToLower(s), "_")
	if len(parts) == 0 {
		return s
	}
	for i := 1; i < len(parts); i++ {
		if parts[i] == "" {
			continue
		}
		parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
	}
	return strings.Join(parts, "")
}

func columnNames(cols []ColumnDef) string {
	names := make([]string, 0, len(cols))
	for _, col := range cols {
		names = append(names, safeSQLName(col.Name))
	}
	return strings.Join(names, ", ")
}

func columnPlaceholders(cols []ColumnDef) string {
	if len(cols) == 0 {
		return ""
	}
	placeholders := make([]string, len(cols))
	for i := range cols {
		placeholders[i] = "?"
	}
	return strings.Join(placeholders, ", ")
}

func updateSet(cols []ColumnDef) string {
	var updates []string
	for _, col := range cols {
		if col.PrimaryKey {
			continue
		}
		safe := safeSQLName(col.Name)
		updates = append(updates, fmt.Sprintf("%s = excluded.%s", safe, safe))
	}
	return strings.Join(updates, ", ")
}

func isStoreBackfillColumn(col ColumnDef) bool {
	switch col.Name {
	case "id", "data", "synced_at":
		return false
	default:
		return !col.PrimaryKey
	}
}

func hasStoreBackfillColumns(table TableDef) bool {
	return slices.ContainsFunc(table.Columns, isStoreBackfillColumn)
}

func storeBackfillDecl(col ColumnDef) string {
	if strings.TrimSpace(col.Type) == "" {
		return "TEXT"
	}
	return col.Type
}

func cobraFlagFunc(t string) string {
	switch primitiveKind(t) {
	case "string":
		return "StringVar"
	case "int":
		return "IntVar"
	case "bool":
		return "BoolVar"
	case "float":
		return "Float64Var"
	default:
		return "StringVar"
	}
}

// goTypeForParam returns the Go type for a parameter, overriding int→string
// for ID-like parameters to avoid overflow and zero-value confusion, and
// numeric→string for pagination cursors so they survive scientific-notation
// rendering of large Unix timestamps and millisecond cursors.
func goTypeForParam(name, t string) string {
	kind := primitiveKind(t)
	if isIDParam(name) && kind == "int" {
		return "string"
	}
	if isCursorParam(name) && (kind == "int" || kind == "float") {
		return "string"
	}
	return goType(t)
}

// cobraFlagFuncForParam returns the cobra flag function, overriding IntVar→StringVar
// for ID-like parameters and Float64Var/IntVar→StringVar for pagination cursors.
func cobraFlagFuncForParam(name, t string) string {
	kind := primitiveKind(t)
	if isIDParam(name) && kind == "int" {
		return "StringVar"
	}
	if isCursorParam(name) && (kind == "int" || kind == "float") {
		return "StringVar"
	}
	return cobraFlagFunc(t)
}

// defaultValForParam returns the default value for a flag parameter,
// overriding int→string for ID-like parameters and numeric→string for
// pagination cursors so the StringVar default matches the StringVar field type.
func defaultValForParam(p spec.Param) string {
	kind := primitiveKind(p.Type)
	if isIDParam(p.Name) && kind == "int" {
		if p.Default != nil {
			return fmt.Sprintf("%q", fmt.Sprintf("%v", p.Default))
		}
		return `""`
	}
	if isCursorParam(p.Name) && (kind == "int" || kind == "float") {
		if p.Default != nil {
			return fmt.Sprintf("%q", fmt.Sprintf("%v", p.Default))
		}
		return `""`
	}
	return defaultVal(p)
}

type jsonFlagSuggestion struct {
	FlagName string
	Values   []string
}

// endpointNeedsClientLimit reports whether a list endpoint needs
// client-side response truncation. True when:
//   - method is GET (only read endpoints need truncation)
//   - the endpoint has a non-positional `limit` param (the user-facing
//     --limit flag exists)
//   - no Pagination block is declared (the spec author hasn't told us
//     the API actually paginates)
//
// When all three conditions hold, the generator emits a
// truncateJSONArray call after the API response returns so --limit N
// is honored even when the API ignores ?limit=N. APIs like Firebase
// and various file-backed JSON endpoints accept the query param
// without applying it server-side; the truncation is harmless when
// the API DID return only N items already (idempotent).
//
// Surfaced by hackernews retro #350 finding F6.
func endpointNeedsClientLimit(endpoint spec.Endpoint) bool {
	if !strings.EqualFold(strings.TrimSpace(endpoint.Method), "GET") {
		return false
	}
	if endpoint.Pagination != nil {
		return false
	}
	for _, p := range endpoint.Params {
		if p.Positional || p.PathParam {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(p.Name), "limit") {
			return true
		}
	}
	return false
}

// bodyMap renders the per-flag body-building block shared by the
// POST/PUT/PATCH branches in command_endpoint.go.tmpl and the body
// branch in command_promoted.go.tmpl. The four sites generated the
// same Go code at different indentation levels; this consolidates them
// and parameterizes the indent. The output is the body of the
// `body := map[string]any{}` block — callers emit the surrounding
// declaration and the closing brace themselves.
func bodyMap(body []spec.Param, indent string) string {
	var b strings.Builder
	for _, p := range body {
		id := paramIdent(p)
		ident := toCamel(id)
		flag := flagName(id)
		isComplex := p.Type == "object" || p.Type == "array"
		if isComplex || isJSONStringParam(p) {
			// object/array: store the parsed value (so the API receives
			// real JSON). jsonStringParam: validate then store the raw
			// string (the API expects a JSON-encoded string field).
			rhs := "body" + ident
			if isComplex {
				rhs = "parsed" + ident
			}
			fmt.Fprintf(&b, "%sif body%s != \"\" {\n", indent, ident)
			fmt.Fprintf(&b, "%s\tvar parsed%s any\n", indent, ident)
			fmt.Fprintf(&b, "%s\tif err := json.Unmarshal([]byte(body%s), &parsed%s); err != nil {\n", indent, ident, ident)
			fmt.Fprintf(&b, "%s\t\treturn fmt.Errorf(\"parsing --%s JSON: %%w\", err)\n", indent, flag)
			fmt.Fprintf(&b, "%s\t}\n", indent)
			fmt.Fprintf(&b, "%s\tbody[%q] = %s\n", indent, p.Name, rhs)
			fmt.Fprintf(&b, "%s}\n", indent)
			continue
		}
		fmt.Fprintf(&b, "%sif body%s != %s {\n", indent, ident, zeroVal(p.Type))
		fmt.Fprintf(&b, "%s\tbody[%q] = body%s\n", indent, p.Name, ident)
		fmt.Fprintf(&b, "%s}\n", indent)
	}
	return b.String()
}

func isJSONStringParam(p spec.Param) bool {
	if p.Type != "string" {
		return false
	}

	format := strings.ToLower(strings.TrimSpace(p.Format))
	switch format {
	case "json", "application/json":
		return true
	}

	description := strings.TrimSpace(p.Description)
	if strings.HasPrefix(description, "{") || strings.HasPrefix(description, "[") {
		return true
	}
	lowerDescription := strings.ToLower(description)
	jsonDescriptionMarkers := []string{
		"as json",
		"json:",
		"json object",
		"json array",
		"json value",
		"valid json",
		"json-encoded",
		"json encoded",
		"json-formatted",
		"json formatted",
		"serialized json",
	}
	for _, marker := range jsonDescriptionMarkers {
		if strings.Contains(lowerDescription, marker) {
			return true
		}
	}
	return false
}

func jsonEnumSuggestion(p spec.Param, params []spec.Param) *jsonFlagSuggestion {
	for _, other := range params {
		if other.Name == p.Name || other.Positional || other.Type != "string" || len(other.Enum) == 0 {
			continue
		}
		if !isRelatedJSONPresetParam(p, other) {
			continue
		}
		return &jsonFlagSuggestion{
			FlagName: flagName(other.Name),
			Values:   other.Enum,
		}
	}
	return nil
}

func isRelatedJSONPresetParam(jsonParam, enumParam spec.Param) bool {
	jsonText := strings.ToLower(jsonParam.Name + " " + jsonParam.Description)
	enumText := strings.ToLower(enumParam.Name + " " + enumParam.Description)

	if !strings.Contains(enumText, "preset") {
		return false
	}

	return hasTemporalMarker(jsonText) && hasTemporalMarker(enumText)
}

func hasTemporalMarker(s string) bool {
	for _, marker := range []string{"time", "date", "range", "window"} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

func defaultVal(p spec.Param) string {
	if p.Default != nil {
		// Coerce the default value to match the declared param type
		switch primitiveKind(p.Type) {
		case "string":
			return fmt.Sprintf("%q", fmt.Sprintf("%v", p.Default))
		case "bool":
			switch v := p.Default.(type) {
			case bool:
				return fmt.Sprintf("%t", v)
			case string:
				if v == "true" || v == "false" {
					return v
				}
			}
			return "false"
		case "int":
			switch v := p.Default.(type) {
			case float64:
				return fmt.Sprintf("%d", int(v))
			case int:
				return fmt.Sprintf("%d", v)
			}
			return "0"
		case "float":
			switch v := p.Default.(type) {
			case float64:
				return fmt.Sprintf("%f", v)
			case int:
				return fmt.Sprintf("%f", float64(v))
			}
			return "0.0"
		case "object", "array":
			data, err := json.Marshal(p.Default)
			if err != nil {
				return `""`
			}
			return fmt.Sprintf("%q", string(data))
		}
	}
	return zeroVal(p.Type)
}

func zeroVal(t string) string {
	switch primitiveKind(t) {
	case "string":
		return `""`
	case "int":
		return "0"
	case "bool":
		return "false"
	case "float":
		return "0.0"
	default:
		return `""`
	}
}

func positionalArgs(e spec.Endpoint) string {
	var args []string
	for _, p := range e.Params {
		if p.Positional {
			args = append(args, "<"+p.Name+">")
		}
	}
	if len(args) > 0 {
		return " " + strings.Join(args, " ")
	}
	return ""
}

func configTag(format string) string {
	switch format {
	case "toml":
		return "toml"
	case "yaml":
		return "yaml"
	default:
		return "json"
	}
}

func envVarField(envVar string) string {
	// STYTCH_PROJECT_ID -> ProjectID
	parts := strings.Split(strings.ToLower(envVar), "_")
	var result strings.Builder
	for _, p := range parts {
		if len(p) > 0 {
			result.WriteString(strings.ToUpper(p[:1]) + p[1:])
		}
	}
	return result.String()
}

// ReservedCLIResourceNames lives in internal/spec/spec.go (spec.ReservedCLIResourceNames)
// so the parser can consult it without importing this package and creating a cycle.

// goosTokens are the GOOS values Go's filename-based build constraints recognize.
// A file named *_<token>.go gets an implicit build tag and is silently excluded
// when the host OS doesn't match. Source of truth: `go tool dist list`.
var goosTokens = map[string]struct{}{
	"aix":       {},
	"android":   {},
	"darwin":    {},
	"dragonfly": {},
	"freebsd":   {},
	"hurd":      {},
	"illumos":   {},
	"ios":       {},
	"js":        {},
	"linux":     {},
	"nacl":      {},
	"netbsd":    {},
	"openbsd":   {},
	"plan9":     {},
	"solaris":   {},
	"wasip1":    {},
	"windows":   {},
	"zos":       {},
}

// goarchTokens are the GOARCH values Go's filename-based build constraints recognize.
var goarchTokens = map[string]struct{}{
	"386":      {},
	"amd64":    {},
	"arm":      {},
	"arm64":    {},
	"loong64":  {},
	"mips":     {},
	"mips64":   {},
	"mips64le": {},
	"mipsle":   {},
	"ppc64":    {},
	"ppc64le":  {},
	"riscv":    {},
	"riscv64":  {},
	"s390x":    {},
	"sparc64":  {},
	"wasm":     {},
}

// safeResourceFileStem returns a basename (without .go) safe to write under
// internal/cli/, suffixing "_cmd" if the bare stem matches Go's filename-based
// build-constraint pattern (*_<GOOS>.go, *_<GOARCH>.go, *_<GOOS>_<GOARCH>.go).
// Without this rename, a file like scheduling_windows.go would get an implicit
// Windows-only build tag and be silently excluded on macOS/Linux builds.
//
// The reserved-name collision is handled separately at spec-parse time
// (see ReservedCLIResourceNames) because the function-name collision needs a
// hard error rather than a silent rename — `new<Name>Cmd` would clash with
// the reserved template's identically-named cobra builder.
//
// The suffix "_cmd" is never itself a GOOS or GOARCH token, so a single
// application is sufficient.
//
// Examples:
//
//	safeResourceFileStem("scheduling_windows")     -> "scheduling_windows_cmd"
//	safeResourceFileStem("foo_linux_amd64")        -> "foo_linux_amd64_cmd"
//	safeResourceFileStem("scheduling_window_days") -> "scheduling_window_days" (no change)
//	safeResourceFileStem("feedback")               -> "feedback" (no change; rejected at parse)
func safeResourceFileStem(stem string) string {
	parts := strings.Split(stem, "_")
	if len(parts) >= 2 {
		last := parts[len(parts)-1]
		if _, isOS := goosTokens[last]; isOS {
			return stem + "_cmd"
		}
		if _, isArch := goarchTokens[last]; isArch {
			return stem + "_cmd"
		}
	}
	if len(parts) >= 3 {
		// Match the *_GOOS_GOARCH.go pattern (e.g., foo_linux_amd64.go).
		penultimate := parts[len(parts)-2]
		last := parts[len(parts)-1]
		_, osOK := goosTokens[penultimate]
		_, archOK := goarchTokens[last]
		if osOK && archOK {
			return stem + "_cmd"
		}
	}
	return stem
}

// builtinConfigTags lists the JSON/TOML tags of hardcoded Config struct fields
// in config.go.tmpl. When an env var's placeholder matches one of these, the
// env var should populate the existing field instead of creating a duplicate.
var builtinConfigTags = map[string]string{
	"access_token":  "AccessToken",
	"refresh_token": "RefreshToken",
	"client_id":     "ClientID",
	"client_secret": "ClientSecret",
	"base_url":      "BaseURL",
	"auth_header":   "AuthHeaderVal",
}

// envVarIsBuiltinField returns true if the env var's placeholder tag would
// collide with a hardcoded Config struct field tag.
func envVarIsBuiltinField(envVar string) bool {
	placeholder := naming.EnvVarPlaceholder(envVar)
	_, ok := builtinConfigTags[placeholder]
	return ok
}

// envVarBuiltinFieldName returns the Go field name of the hardcoded Config
// struct field that matches this env var's placeholder, or empty string if none.
func envVarBuiltinFieldName(envVar string) string {
	placeholder := naming.EnvVarPlaceholder(envVar)
	return builtinConfigTags[placeholder]
}

// resolveEnvVarField returns the correct Go field name for an env var,
// accounting for builtin field collisions. If the env var's placeholder
// matches a hardcoded field, returns that field name; otherwise returns
// the computed field name from envVarField.
func resolveEnvVarField(envVar string) string {
	if name := envVarBuiltinFieldName(envVar); name != "" {
		return name
	}
	return envVarField(envVar)
}

// composeMCPDesc is the template helper that wraps mcpdesc.Compose so
// the mcp_tools.go.tmpl template can build a full description from
// the parsed endpoint plus auth context. The composer in
// internal/mcpdesc shapes the action sentence + Required/Optional
// parameter lines + Returns clause; this wrapper just packages the
// arguments into the Input struct.
func composeMCPDesc(endpoint spec.Endpoint, noAuth bool, authType string, publicCount, totalCount int) string {
	return mcpdesc.Compose(mcpdesc.Input{
		Endpoint:    endpoint,
		NoAuth:      noAuth,
		AuthType:    authType,
		PublicCount: publicCount,
		TotalCount:  totalCount,
	})
}

func exampleValue(p spec.Param) string {
	nameLower := strings.ToLower(p.Name)

	if strings.HasSuffix(nameLower, "_id") || nameLower == "id" {
		return "550e8400-e29b-41d4-a716-446655440000"
	}
	if strings.Contains(nameLower, "email") {
		return "user@example.com"
	}
	if strings.Contains(nameLower, "url") || strings.Contains(nameLower, "link") {
		return "https://example.com/resource"
	}
	if strings.Contains(nameLower, "name") || strings.Contains(nameLower, "title") {
		return "example-resource"
	}
	if strings.Contains(nameLower, "date") || p.Format == "date" {
		return "2026-01-15"
	}
	if strings.Contains(nameLower, "time") || p.Format == "date-time" {
		return "2026-01-15T09:00:00Z"
	}
	if strings.Contains(nameLower, "token") || strings.Contains(nameLower, "key") {
		return "your-token-here"
	}
	if strings.Contains(nameLower, "limit") || strings.Contains(nameLower, "count") || strings.Contains(nameLower, "size") {
		if p.Type == "integer" || p.Type == "int" {
			return "50"
		}
	}
	if p.Type == "boolean" || p.Type == "bool" {
		return "true"
	}
	if p.Type == "integer" || p.Type == "int" || p.Type == "number" || p.Type == "float" {
		return "42"
	}
	return "example-value"
}

func (g *Generator) exampleLine(commandPath, endpointName string, endpoint spec.Endpoint) string {
	var parts []string
	parts = append(parts, naming.CLI(g.Spec.Name))
	parts = append(parts, strings.Fields(commandPath)...)
	parts = append(parts, endpointName)

	// Add positional arg placeholders with realistic values
	for _, p := range endpoint.Params {
		if p.Positional {
			val := exampleValue(p)
			if val == "" {
				val = "<" + p.Name + ">"
			}
			parts = append(parts, val)
		}
	}

	// Add a sample flag for POST/PUT/PATCH with realistic values
	switch endpoint.Method {
	case "POST", "PUT", "PATCH":
		for _, p := range endpoint.Body {
			if p.Required && p.Type == "string" {
				val := exampleValue(p)
				if val == "" {
					val = "value"
				}
				parts = append(parts, "--"+strings.ReplaceAll(p.Name, "_", "-"), val)
				break
			}
		}
	}

	return "  " + strings.Join(parts, " ")
}

func flagName(name string) string {
	name = strings.TrimLeft(name, "$")
	// Convert camelCase/PascalCase and separators to kebab-case.
	// "pageSize" → "page-size", "storeID" → "store-id", "per_page" → "per-page"
	var b strings.Builder
	runes := []rune(name)
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			// Non-alphanumeric → hyphen (dedup'd below)
			if b.Len() > 0 {
				b.WriteByte('-')
			}
			continue
		}
		// Insert hyphen at camelCase boundaries: lowercase→uppercase
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				b.WriteByte('-')
			} else if unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
				// Handle acronyms: "storeID" → "store-id" (not "store-i-d")
				b.WriteByte('-')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	// Collapse multiple hyphens and trim
	result := b.String()
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	return strings.Trim(result, "-")
}

func safeTypeName(name string) string {
	name = strings.TrimLeft(name, "$")
	name = strings.NewReplacer(".", "_", "/", "_", "\\", "_", "-", "_", " ", "_").Replace(name)
	var b strings.Builder
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(r)
		}
	}
	result := b.String()
	if len(result) > 0 && !unicode.IsLetter(rune(result[0])) {
		result = "T" + result
	}
	if isGoKeyword(result) {
		result = "T" + result
	}
	return result
}

// goKeywords is the set of reserved words from the Go language spec
// (https://go.dev/ref/spec#Keywords). Type names that match these refuse to
// parse as `type X struct { ... }`. Predeclared identifiers (bool, int,
// string, error, etc.) shadow rather than fail and are intentionally
// excluded; OpenAPI specs that use them as type names compile, just with
// shadowed builtins inside that file.
var goKeywords = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
}

// isGoKeyword reports whether s is a reserved word in the Go language spec.
func isGoKeyword(s string) bool {
	return goKeywords[s]
}

// toKebab converts PascalCase, camelCase, or mixed names to kebab-case.
// It also strips a leading "I" if it looks like an interface prefix (e.g., ISteamUser → steam-user).
func toKebab(s string) string {
	// Strip leading "I" when followed by an uppercase letter (interface prefix convention)
	if len(s) > 1 && s[0] == 'I' && unicode.IsUpper(rune(s[1])) {
		s = s[1:]
	}
	var result strings.Builder
	for i, r := range s {
		// Snake-case underscores convert to dashes. Lets spec keys like
		// `customer_feedback` and `slot_list_for_date` flow through to
		// user-facing cobra `Use:` strings as `customer-feedback` and
		// `slot-list-for-date` instead of preserving the snake form.
		if r == '_' {
			result.WriteByte('-')
			continue
		}
		if unicode.IsUpper(r) && i > 0 {
			prev := rune(s[i-1])
			// Insert hyphen before uppercase letter if preceded by lowercase,
			// or if preceding char is uppercase AND next char is lowercase (e.g., "APIKey" → "api-key")
			if unicode.IsLower(prev) || (unicode.IsUpper(prev) && i+1 < len(s) && unicode.IsLower(rune(s[i+1]))) {
				result.WriteByte('-')
			}
		}
		result.WriteRune(unicode.ToLower(r))
	}
	return result.String()
}

// PromotedCommand represents a top-level user-friendly command that wraps a nested API endpoint.
type PromotedCommand struct {
	PromotedName string
	ResourceName string
	Endpoint     spec.Endpoint
	EndpointName string
}

// builtinCommands lists command names that must not be used for promoted commands
// because they collide with the CLI's own built-in commands.
var builtinCommands = map[string]bool{
	"version":    true,
	"help":       true,
	"doctor":     true,
	"auth":       true,
	"sync":       true,
	"search":     true,
	"export":     true,
	"import":     true,
	"completion": true,
	"workflow":   true,
	"tail":       true,
	"analytics":  true,
}

// buildPromotedCommands scans spec resources and returns safe top-level shortcuts.
// Only single-endpoint resources are promoted. Multi-endpoint resources stay
// nested so an unknown subcommand cannot silently fall back to an arbitrary
// parent RunE action.
func buildPromotedCommands(s *spec.APISpec) []PromotedCommand {
	var promoted []PromotedCommand
	usedNames := make(map[string]bool)

	resourceNames := make([]string, 0, len(s.Resources))
	for name := range s.Resources {
		resourceNames = append(resourceNames, name)
	}
	sort.Strings(resourceNames)

	for _, name := range resourceNames {
		resource := s.Resources[name]
		if len(resource.Endpoints) > 1 {
			continue
		}

		// Single-endpoint resources promote the only endpoint regardless of method.
		// Without this, POST-only auth resources like `login`/`logout`/`register`
		// render as `<cli> login login --email ...`.
		var bestName string
		var bestEndpoint spec.Endpoint
		found := false

		for _, eName := range sortedEndpointNames(resource.Endpoints) {
			ep := resource.Endpoints[eName]
			bestName = eName
			bestEndpoint = ep
			found = true
		}

		if !found {
			continue
		}

		promotedName := toKebab(name)
		if builtinCommands[promotedName] {
			continue
		}
		if usedNames[promotedName] {
			continue
		}
		usedNames[promotedName] = true

		promoted = append(promoted, PromotedCommand{
			PromotedName: promotedName,
			ResourceName: name,
			Endpoint:     bestEndpoint,
			EndpointName: bestName,
		})
	}
	return promoted
}

func sortedEndpointNames(endpoints map[string]spec.Endpoint) []string {
	names := make([]string, 0, len(endpoints))
	for name := range endpoints {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// isGraphQLSpec returns true if the spec was produced by a GraphQL SDL parser.
// Detection heuristic: all list endpoints have path "/graphql".
func isGraphQLSpec(s *spec.APISpec) bool {
	hasListEndpoint := false
	for _, r := range s.Resources {
		for eName, ep := range r.Endpoints {
			if eName == "list" {
				hasListEndpoint = true
				if ep.Path != "/graphql" {
					return false
				}
			}
		}
	}
	return hasListEndpoint
}

// graphqlQueryField extracts the GraphQL query field name from a ResponsePath.
// For example, "data.issues.nodes" returns "issues", "data.issue" returns "issue".
// For SyncableResource.Path which is always "/graphql", return the resource name.
func graphqlQueryField(responsePath string) string {
	responsePath = strings.TrimPrefix(responsePath, "/graphql")
	if responsePath == "" || responsePath == "/graphql" {
		return ""
	}
	parts := strings.Split(responsePath, ".")
	// Strip "data" prefix
	if len(parts) > 0 && parts[0] == "data" {
		parts = parts[1:]
	}
	// Strip "nodes" suffix
	if len(parts) > 0 && parts[len(parts)-1] == "nodes" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) > 0 {
		return parts[0]
	}
	return responsePath
}

// graphqlFieldSelection returns the list of field names for a GraphQL query
// selection set, derived from the type definition in the spec.
func graphqlFieldSelection(typeName string, types map[string]spec.TypeDef) []string {
	td, ok := types[typeName]
	if !ok {
		return []string{"id"}
	}
	var fields []string
	for _, f := range td.Fields {
		name := strings.TrimSpace(f.Name)
		if name == "" {
			continue
		}
		if selection := strings.TrimSpace(f.Selection); selection != "" {
			fields = append(fields, name+" "+selection)
			continue
		}
		fields = append(fields, name)
	}
	if len(fields) == 0 {
		return []string{"id"}
	}
	return fields
}

type templateEndpoint struct {
	spec.Endpoint
	EffectivePath string
}

// lookupEndpointForTemplate resolves a dotted "resource.endpoint" (or
// "resource.sub_resource.endpoint") reference from the spec's resource map.
// Templates use it when rendering intent handler dispatch tables so each
// step's HTTP method and effective path are known at generate time.
func lookupEndpointForTemplate(resources map[string]spec.Resource, ref string) (templateEndpoint, bool) {
	parts := strings.Split(ref, ".")
	switch len(parts) {
	case 2:
		r, ok := resources[parts[0]]
		if !ok {
			return templateEndpoint{}, false
		}
		e, ok := r.Endpoints[parts[1]]
		if !ok {
			return templateEndpoint{}, false
		}
		return templateEndpoint{
			Endpoint:      e,
			EffectivePath: effectiveEndpointPath(r, e),
		}, true
	case 3:
		r, ok := resources[parts[0]]
		if !ok {
			return templateEndpoint{}, false
		}
		sub, ok := r.SubResources[parts[1]]
		if !ok {
			return templateEndpoint{}, false
		}
		e, ok := sub.Endpoints[parts[2]]
		if !ok {
			return templateEndpoint{}, false
		}
		return templateEndpoint{
			Endpoint:      e,
			EffectivePath: effectiveSubEndpointPath(r, sub, e),
		}, true
	default:
		return templateEndpoint{}, false
	}
}

func effectiveEndpointPath(resource spec.Resource, endpoint spec.Endpoint) string {
	return endpointPathWithBase(resource.BaseURL, endpoint.Path)
}

func effectiveSubEndpointPath(parent spec.Resource, sub spec.Resource, endpoint spec.Endpoint) string {
	baseURL := sub.BaseURL
	if baseURL == "" {
		baseURL = parent.BaseURL
	}
	return endpointPathWithBase(baseURL, endpoint.Path)
}

func endpointPathWithBase(baseURL, path string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return path
	}
	return baseURL + path
}
