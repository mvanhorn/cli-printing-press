package generator

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"go/format"
	"maps"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unicode"

	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/v4/internal/mcpdesc"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/v4/internal/shellargs"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
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

// Recipe mirrors pipeline.Recipe for README/SKILL template rendering.
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
	AntiTriggers   []string
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
	Sources         []ReadmeSource   // Ecosystem tools to credit in README
	DiscoveryPages  []string         // Pages visited during browser-sniff discovery
	NovelFeatures   []NovelFeature   // Transcendence features for README/SKILL
	Narrative       *ReadmeNarrative // LLM-authored prose for README/SKILL; optional
	// CatalogEntryDescription carries curated catalog copy into durable
	// generated metadata without making compact Homebrew/SKILL surfaces depend
	// on it.
	CatalogEntryDescription string
	AsyncJobs               map[string]AsyncJobInfo // Detected async-job endpoints, keyed by "<resource>/<endpoint>"

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

	htmlSyncStubComputed bool
	htmlSyncStub         bool

	mcpParamDescriptions *mcpdesc.ParamDescriptionCompactor
}

func New(s *spec.APISpec, outputDir string) *Generator {
	s.InferEndpointTemplateVarsFromBaseURLs()
	s.PromoteGlobalPathTemplateVars()
	// Resolve the creator + contributors (the canonical attribution model),
	// preserving persisted values across regens so a regen never flips the
	// creator to whoever is running the generator. The legacy
	// Owner/OwnerName/Printer/PrinterName fields are derived from the creator
	// below (dual-write) so older skills and library tooling that still read
	// them keep working during the transition window.
	if s.Creator.IsZero() {
		switch {
		case s.Printer != "" || s.PrinterName != "":
			// Printer names the human — bridge it into the creator in full.
			s.Creator = spec.Person{Handle: s.Printer, Name: s.PrinterName}
		case s.OwnerName != "" || s.Owner != "":
			// An explicitly-set legacy owner drove the copyright header before
			// the creator model existed; preserve that for backward-compat by
			// seeding only the creator NAME. Handle is left empty so the owner
			// slug never leaks into the printer byline (it is the vendor/module
			// identity, not the human).
			name := s.OwnerName
			if name == "" {
				name = s.Owner
			}
			s.Creator = spec.Person{Name: name}
		default:
			s.Creator = resolveCreatorForExisting(outputDir, s.Name)
		}
	}
	if s.Contributors == nil {
		s.Contributors = resolveContributorsForExisting(outputDir, s.Name)
	}
	// Strip attribution-unsafe characters once at the source so every render
	// surface (copyright header, README byline, NOTICE) inherits clean values
	// rather than each escaping independently.
	s.Creator = s.Creator.Clean()
	for i := range s.Contributors {
		s.Contributors[i] = s.Contributors[i].Clean()
	}

	// Owner is the slug form (Go-module-adjacent, copyright-recoverable).
	// Derive it from the creator handle when not explicitly set (e.g. by a
	// catalog override), then always sanitize.
	if s.Owner == "" {
		s.Owner = s.Creator.Handle
	}
	s.Owner = sanitizeOwner(s.Owner)
	// OwnerName / Printer / PrinterName are prose- or handle-shaped legacy
	// fields, preserved verbatim. Empty creator-derived values are validated
	// (soft) in Generate() before any file writes.
	if s.OwnerName == "" {
		s.OwnerName = s.Creator.Name
	}
	if s.Printer == "" {
		s.Printer = s.Creator.Handle
	}
	if s.PrinterName == "" {
		s.PrinterName = s.Creator.Name
	}
	g := &Generator{
		Spec:      s,
		OutputDir: outputDir,
		templates: make(map[string]*template.Template),
	}
	g.funcs = template.FuncMap{
		"title":                              cases.Title(language.English).String,
		"lower":                              strings.ToLower,
		"upper":                              strings.ToUpper,
		"join":                               strings.Join,
		"camel":                              toCamel,
		"cmdIdent":                           commandIdent,
		"snake":                              naming.Snake,
		"pascal":                             toPascal,
		"goType":                             goType,
		"goStructType":                       goStructType,
		"goTypeForParam":                     goTypeForParam,
		"goTypeForParamRequired":             goTypeForParamRequired,
		"goStoreType":                        goStoreType,
		"cobraFlagFunc":                      cobraFlagFunc,
		"cobraFlagFuncForParam":              cobraFlagFuncForParam,
		"cobraFlagFuncForParamRequired":      cobraFlagFuncForParamRequired,
		"mcpBindingFunc":                     mcpBindingFunc,
		"recipeParamTypeString":              func(t RecipeIntentParamType) string { return string(t) },
		"defaultVal":                         defaultVal,
		"defaultValForParam":                 defaultValForParam,
		"defaultValForParamRequired":         defaultValForParamRequired,
		"hasDefault":                         paramHasDefault,
		"isConstDefault":                     paramIsConstDefault,
		"zeroVal":                            zeroVal,
		"zeroValForParam":                    zeroValForParam,
		"zeroValForParamRequired":            zeroValForParamRequired,
		"positionalArgs":                     positionalArgs,
		"configTag":                          configTag,
		"camelToJSON":                        camelToJSON,
		"columnNames":                        columnNames,
		"columnPlaceholders":                 columnPlaceholders,
		"updateSet":                          updateSet,
		"envVarField":                        envVarField,
		"envVarPlaceholder":                  naming.EnvVarPlaceholder,
		"envVarIsBuiltinField":               envVarIsBuiltinField,
		"envVarBuiltinFieldName":             envVarBuiltinFieldName,
		"resolveEnvVarField":                 resolveEnvVarField,
		"authPlacement":                      authPlacement,
		"authParameterName":                  authParameterName,
		"authCommandShort":                   authCommandShort,
		"authHarvestedEnvHint":               authHarvestedEnvHint,
		"oauth2AccessTokenAuth":              oauth2AccessTokenAuth,
		"oauth2AuthSource":                   oauth2AuthSource,
		"basicAuthEnvVars":                   basicAuthEnvVars,
		"clientCredentialsEnvVars":           clientCredentialsEnvVars,
		"deviceCodeEnvVars":                  deviceCodeEnvVars,
		"clientCredentialsScope":             clientCredentialsScope,
		"clientCredentialsScopeUsesClientID": clientCredentialsScopeUsesClientID,
		"clientCredentialsTenantEnvVar":      clientCredentialsTenantEnvVar,
		"clientCredentialsTokenURLHasTenant": clientCredentialsTokenURLHasTenant,
		"hasNonCookieAuth":                   hasNonCookieAuth,
		"authAgentEnvVars":                   authAgentEnvVars,
		"hasAuthEnvVarKind":                  hasAuthEnvVarKind,
		"isRequestAuthEnvVar":                isRequestAuthEnvVar,
		"authEnvPlaceholder":                 authEnvPlaceholder,
		"authEnvPlaceholderByName":           authEnvPlaceholderByName,
		"authEnvHintComment":                 authEnvHintComment,
		"effectiveTier":                      effectiveTier,
		"effectiveSubTier":                   effectiveSubTier,
		"add":                                func(a, b int) int { return a + b },
		"chomp":                              func(s string) string { return strings.TrimRight(s, "\r\n") },
		"staleAfterExpr":                     staleAfterExpr,
		"oneline":                            naming.OneLine,
		"composeMCPDesc":                     composeMCPDesc,
		"composeMCPSubDesc":                  composeMCPSubDesc,
		"mcpParamDesc":                       g.mcpParamDescription,
		"flagName":                           flagName,
		"paramIdent":                         paramIdent,
		"paramWireName":                      paramWireName,
		"typeFieldIdent":                     typeFieldIdent,
		"typeFieldJSONTagComment":            typeFieldJSONTagComment,
		"safeTypeName":                       safeTypeName,
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
		"exampleLine":         g.exampleLine,
		"promotedExampleLine": g.promotedExampleLine,
		"commandExampleArgs":  commandExampleArgs,
		"currentYear":         func() string { return strconv.Itoa(time.Now().Year()) },
		"copyrightHolder": func() string {
			return copyrightHolderString(g.Spec.Creator, g.Spec.OwnerName, g.Spec.Owner)
		},
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
		"isGraphQL":             isGraphQLSpec,
		"localReadIsList":       localReadIsList,
		"dataSourceStrategy":    spec.EffectiveDataSourceStrategy,
		"networkFallbackReason": networkFallbackReason,
		"exportableResources":   exportableResources,
		"backtick":              func() string { return "`" },
		"kebab":                 toKebab,
		"humanName":             naming.HumanName,
		"envPrefix":             naming.EnvPrefix,
		"mcpToolName":           naming.SnakeIdentifier,
		"lookupEndpoint": func(api *spec.APISpec, ref string) templateEndpoint {
			e, _ := lookupEndpointForTemplate(api, ref)
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
		"jsonStringParam":              isJSONStringParam,
		"jsonEnumSuggestion":           jsonEnumSuggestion,
		"bodyMap":                      bodyMap,
		"bodyMapForEndpoint":           bodyMapForEndpoint,
		"bodyVarDecls":                 bodyVarDecls,
		"bodyFlagRegs":                 bodyFlagRegs,
		"bodyRequiredChecks":           bodyRequiredChecks,
		"bodyExceedsFlagDepth":         bodyExceedsFlagDepth,
		"bodyHasStringBackedBool":      bodyHasStringBackedBool,
		"multipartBodyMaps":            multipartBodyMaps,
		"endpointUsesMultipart":        endpointUsesMultipart,
		"endpointUsesCSVArray":         endpointUsesCSVArray,
		"endpointHasQueryFlags":        endpointHasQueryFlags,
		"endpointHasRequestParams":     endpointHasRequestParams,
		"endpointHasRequiredInput":     endpointHasRequiredInput,
		"endpointIsReadCommand":        endpointIsReadCommand,
		"hasMultipartRequest":          hasMultipartRequest,
		"formBodyMaps":                 formBodyMaps,
		"endpointUsesForm":             endpointUsesForm,
		"hasFormRequest":               hasFormRequest,
		"hasBodyJSONFallback":          hasBodyJSONFallback,
		"hasMCPNestedBodyPath":         hasMCPNestedBodyPath,
		"hasMCPParamDefault":           hasMCPParamDefault,
		"publicFlagName":               publicFlagName,
		"publicFlagAliases":            publicFlagAliases,
		"flagChangedExpr":              flagChangedExpr,
		"graphqlListParams":            graphqlListParams,
		"graphqlVariableType":          graphqlVariableType,
		"mcpInputName":                 mcpInputName,
		"mcpToolInputParams":           mcpToolInputParams,
		"mcpParamBindings":             mcpParamBindings,
		"mcpGlobalTemplateInputParams": mcpGlobalTemplateInputParams,
		"mcpGlobalTemplateBindings":    mcpGlobalTemplateBindings,
		// endpointNeedsClientLimit reports whether a list endpoint needs
		// client-side truncation. True when the endpoint has a `limit`-named
		// param AND no Pagination block — the spec author asked for a
		// limit flag, but didn't declare a server-side paginator. Many
		// APIs (Firebase, file-backed JSON dumps, RSS feeds) accept a
		// `?limit=N` query param without honoring it; truncating client-
		// side means the user-facing --limit flag works regardless.
		// Surfaced by hackernews retro #350 finding F6.
		"endpointNeedsClientLimit":  endpointNeedsClientLimit,
		"endpointClientSideFilters": endpointClientSideFilters,
		"envName":                   naming.EnvPrefix,
		// endpointTemplateEnvName resolves the env-var name for a
		// {placeholder} in EndpointTemplateVars. Returns the spec-declared
		// override (e.g. ST_TENANT_ID for {tenant}) when one exists, else
		// the conventional <APINAME>_<UPPER_PLACEHOLDER>. Bound to the
		// generator's current spec; callers in templates pass just the
		// placeholder name.
		"endpointTemplateEnvName": func(placeholder string) string {
			return s.EndpointTemplateEnvName(placeholder)
		},
		// endpointTemplateDefault returns the spec-declared default value for
		// a placeholder (e.g. server-URL variables' `default:` value), or ""
		// when none. Templates branch on the empty case to skip the runtime
		// fallback path and preserve byte-compat with placeholders that have
		// no spec-level default (path-positional templates like {tenant}).
		"endpointTemplateDefault": func(placeholder string) string {
			return s.EndpointTemplateDefault(placeholder)
		},
		// Predicates the config.Load template branches on. Splitting on
		// has-default vs has-undefaulted preserves byte-compat for CLIs whose
		// placeholders are all path-positional (no defaults): without the
		// split, every prior CLI would regenerate just to emit unused helpers.
		"endpointTemplateVarsHasDefault": func(vars []string) bool {
			return endpointTemplateVarsAny(vars, s, func(v string) bool { return v != "" })
		},
		"endpointTemplateVarsHasUndefaulted": func(vars []string) bool {
			return endpointTemplateVarsAny(vars, s, func(v string) bool { return v == "" })
		},
		"safeName":                       safeSQLName,
		"resourceIDFieldOverrideEntries": resourceIDFieldOverrideEntries,
		"criticalResourceEntries":        criticalResourceEntries,
		"isBackfillColumn":               isStoreBackfillColumn,
		"hasBackfillColumns":             hasStoreBackfillColumns,
		"backfillDecl":                   storeBackfillDecl,
		"safeNameSuffix": func(name, suffix string) string {
			return safeSQLName(name + suffix)
		},
		"sqlString": sqlStringLiteral,
		"hasDomainUpsert": func(name string) bool {
			return domainUpsertMethodName(name) != "UpsertBatch"
		},
		// emitsDomainTable is the single source of truth for "this table gets
		// a writable per-resource table and Upsert<X>." Table creation,
		// typed-Upsert generation, the UpsertBatch dispatch switch, and the
		// populated-table tests must all gate on the same predicate; otherwise
		// dead tables (created but never written to) leak in for resources whose
		// names hit the framework-cobra rename. JSONOnlyFallback intentionally
		// keeps a writable per-resource table while dropping extracted columns.
		"emitsDomainTable": func(t TableDef) bool {
			return (len(t.Columns) > 3 || t.JSONOnlyFallback) && t.Name != "sync_state" && domainUpsertMethodName(t.Name) != "UpsertBatch"
		},
		"pathContainsParam": func(path, name string) bool {
			return strings.Contains(path, "{"+name+"}")
		},
		"positionalIndex": positionalIndex,
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
		"yamlDoubleQuoted": yamlDoubleQuoted,
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

func endpointTemplateVarsAny(vars []string, s *spec.APISpec, predicate func(string) bool) bool {
	for _, name := range vars {
		if predicate(s.EndpointTemplateDefault(name)) {
			return true
		}
	}
	return false
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
	HasDelete            bool // spec has DELETE endpoints → emit classifyDeleteError
	HasPathParams        bool // spec has path parameters → emit replacePathParam
	HasMultiPositional   bool // spec has endpoints with 2+ positional params → emit usageErr
	HasDataLayer         bool // CLI has a local store (sync/search) → emit provenance helpers
	HasSyncHelpers       bool // generated sync implementation calls sync-only helpers
	HasClientLimit       bool // at least one endpoint needs client-side limit truncation → emit truncateJSONArray
	HasClientFilters     bool // at least one docs-derived endpoint needs client-side response filtering
	HasEmbeddedPaged     bool // at least one GET endpoint has detected embedded paged sub-resources → emit fetchEmbeddedPagedSubresource
	HasResponseUnwrap    bool // at least one generated command can call extractResponseData
	HasMutationEndpoints bool // spec has any non-GET/HEAD endpoint → emit partial-failure helpers + --allow-partial-failure flag
	HasRequiredRoles     bool // spec has per-endpoint requires_role gates → emit persona helpers
}

// computeHelperFlags scans the spec's resources to determine which helpers are needed.
func computeHelperFlags(s *spec.APISpec) HelperFlags {
	var flags HelperFlags
	for _, r := range s.Resources {
		var scan func(spec.Resource)
		scan = func(resource spec.Resource) {
			for _, e := range resource.Endpoints {
				if strings.EqualFold(e.Method, "DELETE") {
					flags.HasDelete = true
				}
				if isMutationMethod(e.Method) {
					flags.HasMutationEndpoints = true
				}
				if strings.TrimSpace(e.RequiresRole) != "" {
					flags.HasRequiredRoles = true
				}
				if endpointNeedsClientLimit(e) {
					flags.HasClientLimit = true
				}
				if len(endpointClientSideFilters(s, e)) > 0 {
					flags.HasClientFilters = true
				}
				if len(e.EmbeddedPagedSubresources) > 0 {
					flags.HasEmbeddedPaged = true
				}
				if strings.Contains(e.Path, "{") {
					flags.HasPathParams = true
				}
				positionalCount := 0
				for _, p := range e.Params {
					if p.Positional || p.PathParam {
						flags.HasPathParams = true
					}
					if p.Positional {
						positionalCount++
					}
				}
				if positionalCount >= 2 {
					flags.HasMultiPositional = true
				}
			}
			for _, sub := range resource.SubResources {
				scan(sub)
			}
		}
		scan(r)
	}
	return flags
}

// isMutationMethod reports whether method is a mutation verb that reaches the
// partial-failure detection call sites in command_endpoint.go.tmpl. The
// template emits those call sites for every non-GET/HEAD endpoint, so the
// predicate must match the same shape — otherwise a DELETE-only CLI would
// reference undefined detectPartialFailure / partialFailureReport symbols.
// Detection itself is a no-op for DELETE bodies in practice; the cost is one
// dead function on truly-DELETE-only CLIs.
func isMutationMethod(method string) bool {
	if method == "" {
		return false
	}
	return !strings.EqualFold(method, "GET") && !strings.EqualFold(method, "HEAD")
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
	HasMultipartRequest        bool
	HasFormRequest             bool
	UseChromeImpersonation     bool
	// Populated by Generator.shouldEmitAuth() so this template gate stays in
	// sync with auth.go emission, root.go registration, and scoreAuth.
	HasAuthCommand bool
}

// configTemplateData wraps APISpec with a precomputed auth-surface flag so
// config.go.tmpl can gate token-management fields and helpers on the same
// predicate the auth-command emission and root.go registration use.
type configTemplateData struct {
	*spec.APISpec
	HasAuthCommand bool
}

// endpointTemplateData is the data passed to command_endpoint.go.tmpl for both
// top-level resource endpoints and sub-resource endpoints. EffectivePath is
// either the relative endpoint path or a full URL when the endpoint declares
// one directly or inherits a per-resource/per-endpoint BaseURL override.
type endpointTemplateData struct {
	ResourceName  string
	EffectivePath string
	EffectiveTier string
	FuncPrefix    string
	CommandPath   string
	EndpointName  string
	Endpoint      spec.Endpoint
	Resource      spec.Resource
	HasStore      bool
	IsAsync       bool
	Async         AsyncJobInfo
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
	Sources            []ReadmeSource
	DiscoveryPages     []string
	NovelFeatures      []NovelFeature
	Narrative          *ReadmeNarrative
	ProseName          string
	CompactDescription string
	SkillDescription   string
	HasDataLayer       bool
	HasAsyncJobs       bool
	HasWriteCommands   bool
	HasDelete          bool
	HasAuth            bool
	HasAutoRefresh     bool
	FreshnessCommands  []string
	TrafficAnalysis    *trafficAnalysisTemplateData
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
	CompactDescription string
	TrafficAnalysis    *trafficAnalysisTemplateData
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
		CompactDescription:    g.compactDescription(),
		SkillDescription:      g.skillDescription(),
		HasDataLayer:          g.VisionSet.Store,
		HasAsyncJobs:          len(g.AsyncJobs) > 0,
		HasWriteCommands:      hasWriteCommands(g.Spec.Resources),
		HasDelete:             computeHelperFlags(g.Spec).HasDelete,
		HasAuth:               hasAuth(g.Spec.Auth),
		HasAutoRefresh:        g.hasAutoRefresh(),
		FreshnessCommands:     g.freshnessCommandPaths(),
		TrafficAnalysis:       g.trafficAnalysisData(),
		PromotedResourceNames: g.PromotedResourceNames,
		PromotedEndpointNames: g.PromotedEndpointNames,
	}
}

func (g *Generator) compactDescription() string {
	candidates := []string{}
	if g.Narrative != nil {
		candidates = append(candidates, g.Narrative.Headline)
	}
	if g.Spec != nil {
		candidates = append(candidates, g.Spec.CLIDescription, g.Spec.Description)
	}
	for _, candidate := range candidates {
		if desc := naming.CompactDescription(candidate); desc != "" {
			return desc
		}
	}
	return fmt.Sprintf("Printing Press CLI for %s.", g.proseName())
}

// CatalogDescription returns the best generated user-facing description for
// durable catalog metadata.
func (g *Generator) CatalogDescription() string {
	candidates := []string{}
	if g.Narrative != nil {
		candidates = append(candidates, g.Narrative.Headline)
	}
	candidates = append(candidates, g.CatalogEntryDescription)
	if g.Spec != nil {
		candidates = append(candidates, g.Spec.CLIDescription)
	}
	for _, candidate := range candidates {
		if desc := naming.CatalogDescription(candidate); desc != "" {
			if !naming.HasLiteralEllipsisSuffix(desc) {
				return desc
			}
		}
	}
	if g.Spec != nil {
		if desc := naming.CompactDescription(g.Spec.Description); desc != "" {
			return desc
		}
	}
	return fmt.Sprintf("Printing Press CLI for %s.", g.proseName())
}

// CatalogDisplayName returns the best generated brand name for durable catalog
// metadata.
func (g *Generator) CatalogDisplayName() string {
	if g.Narrative != nil {
		if displayName := strings.TrimSpace(g.Narrative.DisplayName); displayName != "" {
			return displayName
		}
	}
	if g.Spec != nil {
		return strings.TrimSpace(g.Spec.EffectiveDisplayName())
	}
	return ""
}

func (g *Generator) skillDescription() string {
	switch {
	case g.Narrative != nil && strings.TrimSpace(g.Narrative.Headline) != "":
		return naming.CompactDescription(g.Narrative.Headline)
	case g.Spec != nil && strings.TrimSpace(g.Spec.CLIDescription) != "":
		return naming.CompactDescription(g.Spec.CLIDescription)
	case g.Spec != nil && strings.TrimSpace(g.Spec.Description) != "":
		return fmt.Sprintf("Printing Press CLI for %s. %s", g.proseName(), naming.CompactDescription(g.Spec.Description))
	default:
		return fmt.Sprintf("Printing Press CLI for %s.", g.proseName())
	}
}

// freshnessCommandPaths returns the command paths surfaced in README.md and
// SKILL.md freshness sections. Keep this in lockstep with
// auto_refresh.go.tmpl's readCommandResources map so the docs describe the
// exact command paths that can trigger auto-refresh at runtime.
func (g *Generator) freshnessCommandPaths() []string {
	if !g.Spec.Cache.Enabled || g.shouldEmitHTMLSyncStub() || g.profile == nil {
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
		add(prefix)
		for _, subcommand := range []string{"list", "get", "search"} {
			add(prefix + " " + subcommand)
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

func authPlacement(auth spec.AuthConfig) string {
	if strings.TrimSpace(auth.In) == "query" {
		return "query"
	}
	return "header"
}

func authParameterName(auth spec.AuthConfig) string {
	if strings.TrimSpace(auth.Header) != "" {
		return auth.Header
	}
	if authPlacement(auth) == "query" {
		return "api_key"
	}
	return "Authorization"
}

func authCommandShort(api *spec.APISpec) string {
	displayName := "this API"
	if api != nil && strings.TrimSpace(api.EffectiveDisplayName()) != "" {
		displayName = api.EffectiveDisplayName()
	}
	if api != nil && api.Auth.Optional {
		return "Manage optional authentication for " + displayName
	}
	return "Manage authentication for " + displayName
}

func authHarvestedEnvHint(auth spec.AuthConfig) string {
	switch {
	case auth.Type == "cookie" || auth.Type == "composed":
		return "populated automatically by auth login --chrome"
	case auth.EffectiveOAuth2Grant() == spec.OAuth2GrantClientCredentials && auth.TokenURL != "":
		return "populated automatically by auth login --client-id/--client-secret"
	case auth.EffectiveOAuth2Grant() == spec.OAuth2GrantDeviceCode && auth.DeviceAuthorizationURL != "" && auth.TokenURL != "":
		return "populated automatically by auth login --device-code"
	case auth.AuthorizationURL != "":
		return "populated automatically by auth login"
	default:
		return "set with auth set-token"
	}
}

func oauth2AccessTokenAuth(auth spec.AuthConfig) bool {
	if auth.Type == "oauth2" || auth.Type == spec.AuthTypeOAuth2Refresh {
		return true
	}
	if auth.Type != "bearer_token" {
		return false
	}
	switch auth.EffectiveOAuth2Grant() {
	case spec.OAuth2GrantClientCredentials, spec.OAuth2GrantDeviceCode:
		return true
	default:
		return false
	}
}

func oauth2AuthSource(auth spec.AuthConfig) string {
	if auth.Type == spec.AuthTypeOAuth2Refresh {
		return spec.AuthTypeOAuth2Refresh
	}
	return "oauth2"
}

func authFormatIsBasic(auth spec.AuthConfig) bool {
	return strings.Contains(strings.ToLower(auth.Format), "basic ")
}

func basicAuthEnvVars(auth spec.AuthConfig) []spec.AuthEnvVar {
	if !authFormatIsBasic(auth) {
		return nil
	}
	var envVars []spec.AuthEnvVar
	if len(auth.EnvVarSpecs) > 0 {
		for _, envVar := range auth.EnvVarSpecs {
			if envVar.IsRequestCredential() {
				envVars = append(envVars, envVar)
			}
		}
	} else {
		for _, name := range auth.EnvVars {
			envVars = append(envVars, spec.AuthEnvVar{
				Name:     name,
				Kind:     spec.AuthEnvVarKindPerCall,
				Required: true,
			})
		}
	}
	if len(envVars) == 0 {
		return nil
	}
	if len(envVars) == 1 {
		return envVars
	}
	return envVars[:2]
}

// hasNonCookieAuth reports whether the CLI has at least one env-var
// based credential (bearer token, API key, OAuth client credentials,
// etc.). Cookie-only CLIs (instacart, airbnb, ebay, pagliacci,
// table-reservation-goat) skip the agentcookie.toml manifest emit
// and the bus-detection block in the generated config loader.
func hasNonCookieAuth(auth spec.AuthConfig) bool {
	return auth.HasNonCookieAuth()
}

func clientCredentialsEnvVars(auth spec.AuthConfig) []spec.AuthEnvVar {
	if auth.EffectiveOAuth2Grant() != spec.OAuth2GrantClientCredentials {
		return nil
	}
	if len(auth.EnvVarSpecs) > 0 {
		var candidates []spec.AuthEnvVar
		tenantEnvVar := clientCredentialsTenantEnvVar(auth)
		for _, envVar := range auth.EnvVarSpecs {
			name := strings.TrimSpace(envVar.Name)
			if name == "" || name == tenantEnvVar || envVar.EffectiveKind() == spec.AuthEnvVarKindHarvested {
				continue
			}
			candidates = append(candidates, envVar)
		}
		clientID, clientSecret := clientCredentialsNamedPair(candidates)
		if clientID.Name != "" && clientSecret.Name != "" {
			return []spec.AuthEnvVar{clientID, clientSecret}
		}
		if len(candidates) >= 2 {
			return candidates[:2]
		}
		return nil
	}
	var envVars []spec.AuthEnvVar
	tenantEnvVar := clientCredentialsTenantEnvVar(auth)
	for _, name := range auth.EnvVars {
		if strings.TrimSpace(name) == "" || name == tenantEnvVar {
			continue
		}
		envVars = append(envVars, spec.AuthEnvVar{
			Name:      name,
			Kind:      spec.AuthEnvVarKindPerCall,
			Required:  true,
			Sensitive: !isClientIDAuthEnvVar(name),
		})
	}
	if len(envVars) < 2 {
		return nil
	}
	clientID, clientSecret := clientCredentialsNamedPair(envVars)
	if clientID.Name != "" && clientSecret.Name != "" {
		return []spec.AuthEnvVar{clientID, clientSecret}
	}
	return envVars[:2]
}

func deviceCodeEnvVars(auth spec.AuthConfig) []spec.AuthEnvVar {
	if auth.EffectiveOAuth2Grant() != spec.OAuth2GrantDeviceCode {
		return nil
	}
	if len(auth.EnvVarSpecs) > 0 {
		var candidates []spec.AuthEnvVar
		for _, envVar := range auth.EnvVarSpecs {
			if strings.TrimSpace(envVar.Name) == "" || envVar.EffectiveKind() == spec.AuthEnvVarKindHarvested {
				continue
			}
			candidates = append(candidates, envVar)
		}
		clientID, _ := clientCredentialsNamedPair(candidates)
		if clientID.Name != "" {
			return []spec.AuthEnvVar{clientID}
		}
		if len(candidates) > 0 {
			return candidates[:1]
		}
		return nil
	}
	for _, name := range auth.EnvVars {
		if strings.TrimSpace(name) == "" {
			continue
		}
		return []spec.AuthEnvVar{{
			Name:      name,
			Kind:      spec.AuthEnvVarKindAuthFlowInput,
			Required:  strings.TrimSpace(auth.DefaultClientID) == "",
			Sensitive: false,
		}}
	}
	return nil
}

func clientCredentialsNamedPair(envVars []spec.AuthEnvVar) (spec.AuthEnvVar, spec.AuthEnvVar) {
	var clientID spec.AuthEnvVar
	var clientSecret spec.AuthEnvVar
	for _, envVar := range envVars {
		placeholder := naming.EnvVarPlaceholder(envVar.Name)
		switch {
		case clientID.Name == "" && (placeholder == "client_id" || strings.HasSuffix(placeholder, "_client_id")):
			clientID = envVar
		case clientSecret.Name == "" && (placeholder == "client_secret" || strings.HasSuffix(placeholder, "_client_secret")):
			clientSecret = envVar
		}
	}
	return clientID, clientSecret
}

func clientCredentialsTenantEnvVar(auth spec.AuthConfig) string {
	if auth.EffectiveOAuth2Grant() != spec.OAuth2GrantClientCredentials {
		return ""
	}
	for _, envVar := range auth.EnvVarSpecs {
		if envVar.EffectiveKind() == spec.AuthEnvVarKindAuthFlowInput && isTenantAuthEnvVar(envVar.Name) {
			return envVar.Name
		}
	}
	if len(auth.EnvVarSpecs) == 0 || spec.AllAuthEnvVarSpecsInferred(auth.EnvVarSpecs) {
		for _, name := range auth.EnvVars {
			if isTenantAuthEnvVar(name) {
				return name
			}
		}
	}
	return ""
}

func clientCredentialsTokenURLHasTenant(auth spec.AuthConfig) bool {
	return clientCredentialsTenantEnvVar(auth) != "" && isMicrosoftEntraTokenURL(auth.TokenURL) && strings.Contains(strings.ToLower(auth.TokenURL), "/common/")
}

func clientCredentialsScope(auth spec.AuthConfig) string {
	if auth.EffectiveOAuth2Grant() != spec.OAuth2GrantClientCredentials {
		return ""
	}
	if len(auth.Scopes) > 0 {
		return strings.Join(auth.Scopes, " ")
	}
	if isMicrosoftEntraTokenURL(auth.TokenURL) {
		return "api://{client_id}/.default"
	}
	return ""
}

func clientCredentialsScopeUsesClientID(scope string) bool {
	return strings.Contains(scope, "{client_id}")
}

func isMicrosoftEntraTokenURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, "login.microsoftonline.com")
}

func isTenantAuthEnvVar(name string) bool {
	placeholder := naming.EnvVarPlaceholder(name)
	return placeholder == "tenant_id" || strings.HasSuffix(placeholder, "_tenant_id")
}

func isClientIDAuthEnvVar(name string) bool {
	placeholder := naming.EnvVarPlaceholder(name)
	return placeholder == "client_id" || strings.HasSuffix(placeholder, "_client_id")
}

func authAgentEnvVars(auth spec.AuthConfig) []spec.AuthEnvVar {
	var envVars []spec.AuthEnvVar
	seen := map[string]struct{}{}
	add := func(envVar spec.AuthEnvVar) {
		if strings.TrimSpace(envVar.Name) == "" || envVar.Kind == spec.AuthEnvVarKindHarvested {
			return
		}
		if _, ok := seen[envVar.Name]; ok {
			return
		}
		seen[envVar.Name] = struct{}{}
		envVars = append(envVars, envVar)
	}

	if len(auth.EnvVarSpecs) > 0 {
		for _, envVar := range auth.EnvVarSpecs {
			add(envVar)
		}
	} else {
		for _, name := range auth.EnvVars {
			add(spec.AuthEnvVar{
				Name:      name,
				Kind:      spec.AuthEnvVarKindPerCall,
				Required:  true,
				Sensitive: true,
			})
		}
	}
	for _, header := range auth.AdditionalHeaders {
		add(header.EnvVar)
	}
	return envVars
}

func hasAuthEnvVarKind(envVarSpecs []spec.AuthEnvVar, kind string) bool {
	for _, envVar := range envVarSpecs {
		if string(envVar.Kind) == kind {
			return true
		}
	}
	return false
}

func isRequestAuthEnvVar(envVar spec.AuthEnvVar) bool {
	return envVar.IsRequestCredential()
}

func authEnvHintComment(envVar spec.AuthEnvVar) string {
	// OneLineNormalize collapses newlines and neutralizes " and \ so the value
	// is safe to embed verbatim in the generated Go double-quoted string literal
	// that prints the hint; without it a description containing a quote or
	// backslash produces non-compiling output. OneLineNormalize (not OneLine)
	// avoids the length cap so the full description survives as a hint.
	return naming.OneLineNormalize(strings.TrimSpace(envVar.Description))
}

func authEnvPlaceholder(envVar spec.AuthEnvVar) string {
	return authEnvPlaceholderByName(envVar.Name)
}

func authEnvPlaceholderByName(envVarName string) string {
	placeholder := naming.EnvVarPlaceholder(envVarName)
	switch {
	case placeholder == "domain" || strings.HasSuffix(placeholder, "_domain") ||
		placeholder == "host" || strings.HasSuffix(placeholder, "_host"):
		envNameUpper := strings.ToUpper(strings.TrimSpace(envVarName))
		return "acme." + authEnvDomainVendor(envNameUpper, placeholder) + ".com"
	case placeholder == "region" || strings.HasSuffix(placeholder, "_region"):
		return "us-east-1"
	case placeholder == "username" || strings.HasSuffix(placeholder, "_username"):
		return "your-username"
	case placeholder == "user_agent" || strings.HasSuffix(placeholder, "_user_agent"):
		return "you@example.com (Your Tool Name)"
	case placeholder == "cookies" || strings.HasSuffix(placeholder, "_cookies"):
		return "<paste your session cookies>"
	default:
		return "your-token-here"
	}
}

func authEnvDomainVendor(envNameUpper, placeholder string) string {
	vendor := strings.TrimSuffix(strings.TrimSuffix(envNameUpper, "_DOMAIN"), "_HOST")
	if vendor == envNameUpper {
		vendor = strings.TrimSuffix(strings.TrimSuffix(placeholder, "_domain"), "_host")
	}
	vendor = strings.Trim(vendor, "_")
	if vendor == "" {
		return "example"
	}
	return strings.ToLower(strings.ReplaceAll(vendor, "_", "-"))
}

func effectiveTier(api *spec.APISpec, resource spec.Resource, endpoint spec.Endpoint) string {
	if api == nil {
		return ""
	}
	return api.EffectiveTier(resource, endpoint)
}

func effectiveSubTier(api *spec.APISpec, parent spec.Resource, subResource spec.Resource, endpoint spec.Endpoint) string {
	if api == nil {
		return ""
	}
	effectiveResource := subResource
	if effectiveResource.Tier == "" {
		effectiveResource.Tier = parent.Tier
	}
	return api.EffectiveTier(effectiveResource, endpoint)
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
	"query":       true,
	"querytext":   true,
	"q":           true,
	"filter":      true,
	"filters":     true,
	"limit":       true,
	"offset":      true,
	"from":        true,
	"size":        true,
	"cursor":      true,
	"page":        true,
	"pagesize":    true,
	"sort":        true,
	"sortby":      true,
	"orderby":     true,
	"maxrecords":  true,
	"max_records": true,
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
		APISpec:            g.Spec,
		CompactDescription: g.compactDescription(),
		TrafficAnalysis:    g.trafficAnalysisData(),
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

func (g *Generator) shouldUseChromeImpersonation() bool {
	if g == nil || g.TrafficAnalysis == nil || g.TrafficAnalysis.Reachability == nil || g.TrafficAnalysis.Reachability.ImpersonationSafe == nil {
		return true
	}
	return *g.TrafficAnalysis.Reachability.ImpersonationSafe
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
		Description: g.compactDescription(),
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
	// Reserve the learn-package directory tree only when the spec opts
	// in. Keeping these gated avoids creating empty dirs in CLIs that
	// don't ship the self-learning loop, which would otherwise show up
	// as untracked dirs in published-library trees.
	if g.Spec.Learn.Enabled {
		dirs = append(dirs,
			filepath.Join("internal", "learn"),
			filepath.Join("internal", "learn", "entities"),
			filepath.Join("internal", "learn", "lookups"),
			filepath.Join("internal", "learn", "patterns"),
		)
	}
	if g.Spec.Streaming.Enabled() {
		dirs = append(dirs, filepath.Join("internal", "wsclient"))
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
		g.resetHTMLSyncStubCache()
		plan := g.profile.ToVisionaryPlan(g.Spec.Name)
		g.VisionSet = SelectVisionTemplates(plan)
	}
	if g.profile == nil {
		g.profile = profiler.Profile(g.Spec)
		g.resetHTMLSyncStubCache()
	}
	g.VisionSet = constrainVisionTemplates(g.Spec, g.VisionSet)
	if g.Spec.Learn.Enabled && !g.VisionSet.Store {
		return fmt.Errorf("learn.enabled requires VisionSet.Store=true; the learn package depends on internal/store")
	}
	if g.renameActiveFrameworkResourceCollisions() {
		g.profile = profiler.Profile(g.Spec)
		g.resetHTMLSyncStubCache()
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
	if err := g.dedupeFlagIdentifiers(); err != nil {
		return err
	}

	g.dedupeTypeFieldIdentifiers()

	return nil
}

func (g *Generator) renderSingleFiles() error {
	singleFiles := map[string]string{
		"main.go.tmpl":                             filepath.Join("cmd", naming.CLI(g.Spec.Name), "main.go"),
		"version.go.tmpl":                          filepath.Join("internal", "cli", "version.go"),
		"helpers.go.tmpl":                          filepath.Join("internal", "cli", "helpers.go"),
		"root_test.go.tmpl":                        filepath.Join("internal", "cli", "root_test.go"),
		"doctor.go.tmpl":                           filepath.Join("internal", "cli", "doctor.go"),
		"agent_context.go.tmpl":                    filepath.Join("internal", "cli", "agent_context.go"),
		"profile.go.tmpl":                          filepath.Join("internal", "cli", "profile.go"),
		"deliver.go.tmpl":                          filepath.Join("internal", "cli", "deliver.go"),
		"feedback.go.tmpl":                         filepath.Join("internal", "cli", "feedback.go"),
		"which.go.tmpl":                            filepath.Join("internal", "cli", "which.go"),
		"which_test.go.tmpl":                       filepath.Join("internal", "cli", "which_test.go"),
		"config.go.tmpl":                           filepath.Join("internal", "config", "config.go"),
		"cache.go.tmpl":                            filepath.Join("internal", "cache", "cache.go"),
		"client.go.tmpl":                           filepath.Join("internal", "client", "client.go"),
		"client_test.go.tmpl":                      filepath.Join("internal", "client", "client_test.go"),
		"client_verify_short_circuit_test.go.tmpl": filepath.Join("internal", "client", "client_verify_short_circuit_test.go"),
		"cliutil_fanout.go.tmpl":                   filepath.Join("internal", "cliutil", "fanout.go"),
		"cliutil_text.go.tmpl":                     filepath.Join("internal", "cliutil", "text.go"),
		"cliutil_probe.go.tmpl":                    filepath.Join("internal", "cliutil", "probe.go"),
		"cliutil_ratelimit.go.tmpl":                filepath.Join("internal", "cliutil", "ratelimit.go"),
		"cliutil_verifyenv.go.tmpl":                filepath.Join("internal", "cliutil", "verifyenv.go"),
		"cliutil_extractnumber.go.tmpl":            filepath.Join("internal", "cliutil", "extractnumber.go"),
		"cliutil_extractnumber_test.go.tmpl":       filepath.Join("internal", "cliutil", "extractnumber_test.go"),
		"cliutil_jwtshape.go.tmpl":                 filepath.Join("internal", "cliutil", "jwtshape.go"),
		"cliutil_jwtshape_test.go.tmpl":            filepath.Join("internal", "cliutil", "jwtshape_test.go"),
		"cliutil_duration.go.tmpl":                 filepath.Join("internal", "cliutil", "duration.go"),
		"cliutil_duration_test.go.tmpl":            filepath.Join("internal", "cliutil", "duration_test.go"),
		"cliutil_odata_date.go.tmpl":               filepath.Join("internal", "cliutil", "odata_date.go"),
		"cliutil_odata_date_test.go.tmpl":          filepath.Join("internal", "cliutil", "odata_date_test.go"),
		"cliutil_test.go.tmpl":                     filepath.Join("internal", "cliutil", "cliutil_test.go"),
		"types.go.tmpl":                            filepath.Join("internal", "types", "types.go"),
		"golangci.yml.tmpl":                        ".golangci.yml",
		"readme.md.tmpl":                           "README.md",
		"agents.md.tmpl":                           "AGENTS.md",
		"skill.md.tmpl":                            "SKILL.md",
		"LICENSE.tmpl":                             "LICENSE",
		"NOTICE.tmpl":                              "NOTICE",
	}
	maps.Copy(singleFiles, cobratreeWalkerTemplateFiles())

	for tmplName, outPath := range singleFiles {
		if tmplName == "types.go.tmpl" && g.shouldPreserveExistingTypesFile(outPath) {
			continue
		}
		var data any
		switch tmplName {
		case "readme.md.tmpl", "agents.md.tmpl", "skill.md.tmpl", "which.go.tmpl", "which_test.go.tmpl":
			data = g.readmeData()
		case "helpers.go.tmpl":
			hFlags := computeHelperFlags(g.Spec)
			hFlags.HasDataLayer = g.VisionSet.Store
			hFlags.HasSyncHelpers = g.hasGeneratedSyncImplementation()
			hFlags.HasResponseUnwrap = g.VisionSet.Store && promotedCommandsCanUnwrapResponse(g.PromotedCommands, g.Spec.Types)
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
				HasMultipartRequest:        hasMultipartRequest(g.Spec),
				HasFormRequest:             hasFormRequest(g.Spec),
				UseChromeImpersonation:     g.shouldUseChromeImpersonation(),
				HasAuthCommand:             g.shouldEmitAuth(),
			}
		case "config.go.tmpl":
			data = &configTemplateData{
				APISpec:        g.Spec,
				HasAuthCommand: g.shouldEmitAuth(),
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

func (g *Generator) shouldPreserveExistingTypesFile(outPath string) bool {
	if g == nil || g.Spec == nil || g.Spec.SpecSource != "sniffed" {
		return false
	}
	data, err := os.ReadFile(filepath.Join(g.OutputDir, outPath))
	if err != nil {
		return false
	}
	return generatedTypesFileHasDeclarations(string(data))
}

func generatedTypesFileHasDeclarations(content string) bool {
	for line := range strings.SplitSeq(content, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "type ") {
			return true
		}
	}
	return false
}

func (g *Generator) renderOptionalSupportFiles() error {
	if g.Spec.HasHTMLExtraction() {
		if err := g.renderTemplate("html_extract.go.tmpl", filepath.Join("internal", "cli", "html_extract.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering HTML extraction helper: %w", err)
		}
	}

	if g.VisionSet.Store {
		if err := g.renderTemplate("sync_hint.go.tmpl", filepath.Join("internal", "cli", "sync_hint.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering sync hint helper: %w", err)
		}
		if err := g.renderTemplate("sync_hint_test.go.tmpl", filepath.Join("internal", "cli", "sync_hint_test.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering sync hint test: %w", err)
		}
	}

	// Emit the cliutil freshness helper only when the spec opts into cache
	// or share and the CLI has a local store. Without a store there is
	// nothing to check freshness against; without cache or share opt-in
	// there is no caller that consumes the Decision.
	if g.VisionSet.Store && ((g.Spec.Cache.Enabled && g.hasGeneratedSyncImplementation()) || g.Spec.Share.Enabled) {
		if err := g.renderTemplate("cliutil_freshness.go.tmpl", filepath.Join("internal", "cliutil", "freshness.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering cliutil freshness: %w", err)
		}
		if err := g.renderTemplate("cliutil_freshness_test.go.tmpl", filepath.Join("internal", "cliutil", "freshness_test.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering cliutil freshness test: %w", err)
		}
	}

	if hasCSVArrayRequest(g.Spec) {
		if err := g.renderTemplate("cliutil_csv.go.tmpl", filepath.Join("internal", "cliutil", "csv.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering cliutil csv: %w", err)
		}
	}

	if hasCSVResponse(g.Spec) {
		if err := g.renderTemplate("cliutil_csv_parse.go.tmpl", filepath.Join("internal", "cliutil", "csv_parse.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering cliutil csv parse: %w", err)
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
	if g.hasAutoRefresh() {
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
		if err := g.renderTemplate("auto_refresh_test.go.tmpl", filepath.Join("internal", "cli", "auto_refresh_test.go"), autoRefreshData); err != nil {
			return fmt.Errorf("rendering auto_refresh_test: %w", err)
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

	// Emit the self-learning loop (entities + normalize + match +
	// recall + teach + teach_log + preseed + lookups + patterns) only
	// when the spec opts in via Learn.Enabled. The schema migrations
	// for the supporting tables are emitted from store.go.tmpl under
	// the same gate; see internal/generator/templates/learn/doc.go.tmpl
	// for the package-level design and the schema-adaptation note.
	if g.Spec.Learn.Enabled {
		if err := g.renderLearnFiles(); err != nil {
			return err
		}
		// learnings.go ports prediction-goat's canonical Apply engine
		// (Apply/Recall/UpsertLearning/ListLearnings/ForgetLearnings +
		// LearnedHit/Applier types + NormalizeQuery/MarshalLearnings
		// helpers) into the generator's internal/store/ output. The
		// engine is a method on *store.Store, so it lives next to
		// store.go rather than under internal/learn/.
		//
		// The internal/store directory is created by renderStoreFiles()
		// later in Generate(), but the learn-emission block runs first.
		// Ensure the directory exists before writing into it. Idempotent
		// with the subsequent MkdirAll in renderStoreFiles.
		if err := os.MkdirAll(filepath.Join(g.OutputDir, "internal", "store"), 0o755); err != nil {
			return fmt.Errorf("creating store dir for learn engine: %w", err)
		}
		if err := g.renderTemplate("learnings.go.tmpl", filepath.Join("internal", "store", "learnings.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering store learnings engine: %w", err)
		}
		if err := g.renderTemplate("learnings_test.go.tmpl", filepath.Join("internal", "store", "learnings_test.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering store learnings engine test: %w", err)
		}
		// store/playbooks.go ports the playbook-row CRUD (UpsertPlaybook,
		// AppendPlaybookNotes, GetPlaybookByFamily, ListPlaybooks) into
		// the generator's internal/store/ output. Lives next to
		// learnings.go and shares the LearningSourceTaught constant + the
		// learning_playbooks table created by store.go.tmpl. Gated under
		// Learn.Enabled for the same reason as learnings.go.
		if err := g.renderTemplate("store_playbooks.go.tmpl", filepath.Join("internal", "store", "playbooks.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering store playbooks: %w", err)
		}
		if err := g.renderTemplate("store_playbooks_test.go.tmpl", filepath.Join("internal", "store", "playbooks_test.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering store playbooks test: %w", err)
		}
		// teach.go and teach_test.go are emitted into internal/cli/
		// (not the learn package) because they wire cobra commands;
		// the learn package itself stays cobra-free per the boundary
		// established in U3-U5.
		if err := g.renderTemplate("teach.go.tmpl", filepath.Join("internal", "cli", "teach.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering teach commands: %w", err)
		}
		if err := g.renderTemplate("teach_test.go.tmpl", filepath.Join("internal", "cli", "teach_test.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering teach commands test: %w", err)
		}
		// teach_playbook.go ships the standalone playbook write surface:
		// `teach-playbook` (query-family-keyed playbook + notes), `playbook list`
		// (sentinel-filtered inspection), and `playbook amend` (atomic
		// AppendPlaybookNotes self-correction). Lives in internal/cli/ alongside
		// teach.go for the same package-boundary reason — cobra wiring stays
		// out of the learn package. Registration on the root command is
		// owned by root.go.tmpl (a later unit wires it).
		if err := g.renderTemplate("teach_playbook.go.tmpl", filepath.Join("internal", "cli", "teach_playbook.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering teach-playbook commands: %w", err)
		}
		if err := g.renderTemplate("teach_playbook_test.go.tmpl", filepath.Join("internal", "cli", "teach_playbook_test.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering teach-playbook commands test: %w", err)
		}
		// internal/cli/playbooks/ ships the embed.FS scaffold for hand-
		// authored playbook content (JSON + notes files). U9 emits the
		// scaffold only — the auto-install path that walks this FS lives
		// in playbook_init.go (a later unit). MANIFEST.md keeps the
		// //go:embed *.json *.md directive matching at least one file
		// when no authored content has shipped yet, so the package
		// compiles cleanly on a fresh print.
		if err := os.MkdirAll(filepath.Join(g.OutputDir, "internal", "cli", "playbooks"), 0o755); err != nil {
			return fmt.Errorf("creating cli/playbooks dir for embed.FS scaffold: %w", err)
		}
		if err := g.renderTemplate("playbooks_embed.go.tmpl", filepath.Join("internal", "cli", "playbooks", "embed.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering cli/playbooks embed.go: %w", err)
		}
		if err := g.renderTemplate("playbooks_manifest.md.tmpl", filepath.Join("internal", "cli", "playbooks", "MANIFEST.md"), g.Spec); err != nil {
			return fmt.Errorf("rendering cli/playbooks MANIFEST.md: %w", err)
		}
		// playbook_init.go is the embed-FS auto-install path. At first
		// DB open after schema migration it walks playbooks.FS, parses
		// each <base>.json + <base>_notes.md pair, derives the query
		// family via learn.QueryFamily(learn.Normalize(example)), and
		// upserts a row per family. A sentinel row tracks SeedVersion;
		// re-seed fires on binary upgrades that bump the constant.
		// `playbook amend` writes are preserved across upgrades via an
		// anchored regex check on the `[amend YYYY-...]` marker.
		// Root.go.tmpl wires runPlaybookInitOnce in a later unit.
		if err := g.renderTemplate("playbook_init.go.tmpl", filepath.Join("internal", "cli", "playbook_init.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering playbook init: %w", err)
		}
		if err := g.renderTemplate("playbook_init_test.go.tmpl", filepath.Join("internal", "cli", "playbook_init_test.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering playbook init test: %w", err)
		}
		// learn_init.go translates spec.Learn (ticker patterns, stopwords,
		// entity-lookup seeds) into a runtime *entities.Config and seeds
		// the entity_lookups table at first start. Owns newLearnConfig()
		// (which teach.go's command constructors call) and initLearn(),
		// which root.go invokes from PersistentPreRunE under the same
		// Learn.Enabled gate.
		if err := g.renderTemplate("learn_init.go.tmpl", filepath.Join("internal", "cli", "learn_init.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering learn init: %w", err)
		}
		if err := g.renderTemplate("learn_init_test.go.tmpl", filepath.Join("internal", "cli", "learn_init_test.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering learn init test: %w", err)
		}
	}

	if g.FixtureSet != nil {
		if err := g.renderTemplate("captured_test.go.tmpl", filepath.Join("internal", "client", "client_captured_test.go"), g.FixtureSet); err != nil {
			return fmt.Errorf("rendering captured fixture tests: %w", err)
		}
	}

	// Persistent cookie jar — emitted only when the spec declares cookies
	// (cookie or composed auth). Non-cookie CLIs (api_key, bearer, oauth2,
	// session_handshake, Auth0-SPA) don't need the helper and get the same
	// byte-identical client.go they always did. The session_handshake path
	// uses sess.CookieJar() and bypasses this file entirely.
	if g.Spec.Auth.HasCookies() && g.Spec.Auth.Type != "session_handshake" {
		if err := g.renderTemplate("cookiejar.go.tmpl", filepath.Join("internal", "client", "cookiejar.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering cookie jar: %w", err)
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

	if g.Spec.Streaming.Enabled() {
		if err := g.renderTemplate("wsclient.go.tmpl", filepath.Join("internal", "wsclient", "client.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering websocket client helper: %w", err)
		}
		if err := g.renderTemplate("live_ws.go.tmpl", filepath.Join("internal", "cli", "live_ws.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering websocket live command: %w", err)
		}
	}

	return nil
}

// renderLearnFiles emits the internal/learn package and its three
// sub-packages (entities, lookups, patterns) when the spec opts into
// the self-learning loop. The matching v3 schema migrations are
// emitted from internal/generator/templates/store.go.tmpl under the
// same Learn.Enabled gate.
//
// The teach_log.go.tmpl file uses the CLI name as the state-directory
// suffix; every other learn file is data-driven from the spec's
// LearnConfig values, which the per-CLI startup wires via NewConfig
// and SeedFromConfig at first run.
func (g *Generator) renderLearnFiles() error {
	learnFiles := map[string]string{
		"learn_entities/config.go.tmpl":       filepath.Join("internal", "learn", "entities", "config.go"),
		"learn_entities/config_test.go.tmpl":  filepath.Join("internal", "learn", "entities", "config_test.go"),
		"learn_entities/extract.go.tmpl":      filepath.Join("internal", "learn", "entities", "extract.go"),
		"learn_entities/extract_test.go.tmpl": filepath.Join("internal", "learn", "entities", "extract_test.go"),

		"learn/doc.go.tmpl":                   filepath.Join("internal", "learn", "doc.go"),
		"learn/normalize.go.tmpl":             filepath.Join("internal", "learn", "normalize.go"),
		"learn/normalize_test.go.tmpl":        filepath.Join("internal", "learn", "normalize_test.go"),
		"learn/match.go.tmpl":                 filepath.Join("internal", "learn", "match.go"),
		"learn/match_test.go.tmpl":            filepath.Join("internal", "learn", "match_test.go"),
		"learn/recall.go.tmpl":                filepath.Join("internal", "learn", "recall.go"),
		"learn/recall_test.go.tmpl":           filepath.Join("internal", "learn", "recall_test.go"),
		"learn/recall_canonical_test.go.tmpl": filepath.Join("internal", "learn", "recall_canonical_test.go"),
		"learn/teach.go.tmpl":                 filepath.Join("internal", "learn", "teach.go"),
		"learn/teach_test.go.tmpl":            filepath.Join("internal", "learn", "teach_test.go"),
		"learn/teach_log.go.tmpl":             filepath.Join("internal", "learn", "teach_log.go"),
		"learn/teach_log_test.go.tmpl":        filepath.Join("internal", "learn", "teach_log_test.go"),
		"learn/preseed.go.tmpl":               filepath.Join("internal", "learn", "preseed.go"),
		"learn/preseed_test.go.tmpl":          filepath.Join("internal", "learn", "preseed_test.go"),
		"learn/playbooks.go.tmpl":             filepath.Join("internal", "learn", "playbooks.go"),
		"learn/playbooks_test.go.tmpl":        filepath.Join("internal", "learn", "playbooks_test.go"),
		"learn/promote.go.tmpl":               filepath.Join("internal", "learn", "promote.go"),
		"learn/promote_test.go.tmpl":          filepath.Join("internal", "learn", "promote_test.go"),

		"learn_lookups/store.go.tmpl":      filepath.Join("internal", "learn", "lookups", "store.go"),
		"learn_lookups/store_test.go.tmpl": filepath.Join("internal", "learn", "lookups", "store_test.go"),
		"learn_lookups/seeds.go.tmpl":      filepath.Join("internal", "learn", "lookups", "seeds.go"),
		"learn_lookups/seeds_test.go.tmpl": filepath.Join("internal", "learn", "lookups", "seeds_test.go"),

		"learn_patterns/doc.go.tmpl":          filepath.Join("internal", "learn", "patterns", "doc.go"),
		"learn_patterns/store.go.tmpl":        filepath.Join("internal", "learn", "patterns", "store.go"),
		"learn_patterns/store_test.go.tmpl":   filepath.Join("internal", "learn", "patterns", "store_test.go"),
		"learn_patterns/extract.go.tmpl":      filepath.Join("internal", "learn", "patterns", "extract.go"),
		"learn_patterns/extract_test.go.tmpl": filepath.Join("internal", "learn", "patterns", "extract_test.go"),
		"learn_patterns/apply.go.tmpl":        filepath.Join("internal", "learn", "patterns", "apply.go"),
		"learn_patterns/apply_test.go.tmpl":   filepath.Join("internal", "learn", "patterns", "apply_test.go"),
	}
	for tmplName, outPath := range learnFiles {
		if err := g.renderTemplate(tmplName, outPath, g.Spec); err != nil {
			return fmt.Errorf("rendering %s: %w", tmplName, err)
		}
	}
	return nil
}

func (g *Generator) Generate() error {
	applyLargeMCPSurfaceDefault(g.Spec, os.Stderr)
	if g.Spec.OwnerName == "" {
		// OwnerName flows into Hermes `author:` and other prose
		// surfaces. We don't hard-fail on an empty value because the
		// generator package is reused by many callers (tests, mcp-sync,
		// regen-merge) where setting it is awkward. Instead, fall back
		// to the slug-shaped Owner so emission is non-empty, and warn
		// loudly so a real-print operator catches the misconfiguration.
		// The library-wide sweep tool overrides this via its own per-CLI
		// authorship mapping, so this fallback only ever lands on fresh
		// prints by users who haven't set `git config user.name`.
		fmt.Fprintf(os.Stderr,
			"WARNING: spec.OwnerName is empty; falling back to slug-shaped Owner (%q) for `author:` field. "+
				"Set `git config user.name` (display name, e.g. \"Trevin Chow\") to populate this correctly.\n",
			g.Spec.Owner,
		)
		g.Spec.OwnerName = g.Spec.Owner
	}
	if g.Spec.Printer == "" {
		// Publish enforces this so self-owned CLIs can still use matching owner/printer slugs.
		fmt.Fprintf(os.Stderr,
			"WARNING: spec.Printer is empty; README printer attribution will be omitted. "+
				"Set `git config github.user` (your GitHub @handle) to populate this correctly before publishing.\n",
		)
	}
	// Auth.VerifyPath drives the doctor's credentials probe. Derive it before
	// HealthCheckPath so HealthCheckPath's fallback chain can pick up the
	// derived value (mirroring how an operator-set auth.verify_path already
	// flows through). Without this, specs that ship a me-shaped endpoint but
	// no explicit auth.verify_path generate a doctor that reports credentials
	// as merely "present (not verified)" instead of probing the API.
	if g.Spec.Auth.VerifyPath == "" {
		g.Spec.Auth.VerifyPath = deriveAuthVerifyPath(g.Spec)
	}
	if g.Spec.HealthCheckPath == "" {
		g.Spec.HealthCheckPath = deriveHealthCheckPath(g.Spec)
	}
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

func (g *Generator) renameActiveFrameworkResourceCollisions() bool {
	active := g.activeFrameworkCobraUseNames()
	type resourceRename struct {
		from     string
		to       string
		use      string
		resource spec.Resource
	}
	var renames []resourceRename
	for name, resource := range g.Spec.Resources {
		kebab := strings.ReplaceAll(strings.ToLower(name), "_", "-")
		if _, ok := active[kebab]; !ok {
			continue
		}
		if g.Spec.ParseTimeReservedCobraUseName(kebab) {
			continue
		}
		renames = append(renames, resourceRename{
			from:     name,
			to:       g.Spec.UniqueFrameworkCollisionResourceName(kebab),
			use:      kebab,
			resource: resource,
		})
	}
	for _, rename := range renames {
		delete(g.Spec.Resources, rename.from)
		g.Spec.Resources[rename.to] = rename.resource
		fmt.Fprintf(os.Stderr, "warning: resource %q would shadow active framework cobra command %q; renamed to %q\n", rename.from, rename.use, rename.to)
	}
	return len(renames) > 0
}

func (g *Generator) activeFrameworkCobraUseNames() map[string]struct{} {
	names := map[string]struct{}{
		"agent-context": {},
		"completion":    {},
		"doctor":        {},
		"feedback":      {},
		"help":          {},
		"profile":       {},
		"version":       {},
		"which":         {},
	}
	if g.shouldEmitAuth() {
		names["auth"] = struct{}{}
		if g.emitsTopLevelOAuthLogin() {
			names["login"] = struct{}{}
		}
	}
	if g.Spec.BearerRefresh.Enabled() {
		names["refresh-bearer"] = struct{}{}
	}
	if len(g.AsyncJobs) > 0 {
		names["jobs"] = struct{}{}
	}
	if g.VisionSet.Export {
		names["export"] = struct{}{}
	}
	if g.VisionSet.Import {
		names["import"] = struct{}{}
	}
	if g.VisionSet.Search {
		names["search"] = struct{}{}
	}
	if g.VisionSet.Sync {
		names["sync"] = struct{}{}
	}
	if g.VisionSet.Tail {
		names["tail"] = struct{}{}
	}
	if g.VisionSet.Analytics {
		names["analytics"] = struct{}{}
	}
	if g.VisionSet.Store {
		names["workflow"] = struct{}{}
	}
	if g.Spec.Share.Enabled {
		names["share"] = struct{}{}
	}
	if len(g.PromotedCommands) > 0 {
		names["api"] = struct{}{}
	}
	for _, tmpl := range g.VisionSet.Workflows {
		if name := frameworkUseNameForTemplate(tmpl); name != "" {
			names[name] = struct{}{}
		}
	}
	for _, tmpl := range g.VisionSet.Insights {
		if name := frameworkUseNameForTemplate(tmpl); name != "" {
			names[name] = struct{}{}
		}
	}
	return names
}

func frameworkUseNameForTemplate(tmpl string) string {
	ctor := commandConstructorForTemplate(tmpl)
	if ctor == "" {
		return ""
	}
	return strings.ReplaceAll(naming.Snake(ctor), "_", "-")
}

// cobratreeWalkerTemplateFiles maps each cobratree MCP-walker template to its
// output path under internal/mcp/cobratree. The walker is API-agnostic, so the
// file set is identical for every printed CLI (HTTP and device); single-source
// it here rather than repeating the manifest in each generator's file map.
func cobratreeWalkerTemplateFiles() map[string]string {
	files := make(map[string]string)
	for _, name := range []string{
		"walker.go", "classify.go", "typemap.go", "shellout.go",
		"shellout_test.go", "cli_path.go", "names.go",
	} {
		files["cobratree/"+name+".tmpl"] = filepath.Join("internal", "mcp", "cobratree", name)
	}
	return files
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
	applyLargeMCPSurfaceDefault(g.Spec, os.Stderr)
	if err := g.prepareOutput(); err != nil {
		return err
	}
	g.PromotedCommands, g.PromotedResourceNames, g.PromotedEndpointNames = buildPromotedCommandPlan(g.Spec)
	mcpFiles := map[string]string{
		// cliutil files. Deliberately asymmetric with the marker-checked
		// tools.go / handlers.go / root.go paths elsewhere in mcp-sync:
		// those files can carry hand-edits and require explicit
		// confirmation before overwrite, but cliutil is generator-
		// reserved per AGENTS.md and unconditional regen is the
		// expected contract. Without this, library CLIs whose cliutil
		// predates a helper the new MCP template uses (SanitizeErrorBody,
		// LooksLikeAuthError) fail to build after migration. See the
		// GenerateMCPSurface doc comment for the full rationale.
		"cliutil_fanout.go.tmpl":             filepath.Join("internal", "cliutil", "fanout.go"),
		"cliutil_text.go.tmpl":               filepath.Join("internal", "cliutil", "text.go"),
		"cliutil_probe.go.tmpl":              filepath.Join("internal", "cliutil", "probe.go"),
		"cliutil_ratelimit.go.tmpl":          filepath.Join("internal", "cliutil", "ratelimit.go"),
		"cliutil_verifyenv.go.tmpl":          filepath.Join("internal", "cliutil", "verifyenv.go"),
		"cliutil_extractnumber.go.tmpl":      filepath.Join("internal", "cliutil", "extractnumber.go"),
		"cliutil_extractnumber_test.go.tmpl": filepath.Join("internal", "cliutil", "extractnumber_test.go"),
		"cliutil_jwtshape.go.tmpl":           filepath.Join("internal", "cliutil", "jwtshape.go"),
		"cliutil_jwtshape_test.go.tmpl":      filepath.Join("internal", "cliutil", "jwtshape_test.go"),
	}
	maps.Copy(mcpFiles, cobratreeWalkerTemplateFiles())
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

func promotedCommandsCanUnwrapResponse(commands []PromotedCommand, types map[string]spec.TypeDef) bool {
	for _, cmd := range commands {
		if !cmd.Endpoint.UsesBinaryResponse() && endpointHasStatusDataEnvelope(cmd.Endpoint, types) {
			return true
		}
	}
	return false
}

// endpointHasStatusDataEnvelope reports whether the endpoint's response type
// looks like a {status/success, data} envelope, gating extractResponseData
// emission. The runtime helper keys on the "status" field; "success" is
// accepted here too so success-keyed envelopes still emit the helper. A type
// with data+success but no status passes detection and the helper no-ops on it
// at runtime — harmless over-emission, and emit/call stay aligned either way.
func endpointHasStatusDataEnvelope(endpoint spec.Endpoint, types map[string]spec.TypeDef) bool {
	item := strings.TrimSpace(endpoint.Response.Item)
	if item == "" {
		return false
	}
	typedef, ok := types[item]
	if !ok {
		return false
	}
	hasData := false
	hasStatusOrSuccess := false
	for _, field := range typedef.Fields {
		switch strings.ToLower(strings.TrimSpace(field.Name)) {
		case "data":
			hasData = true
		case "status", "success":
			hasStatusOrSuccess = true
		}
		if hasData && hasStatusOrSuccess {
			return true
		}
	}
	return false
}

func parentCommandShort(resourceName, parentName string, resource spec.Resource, apiDescriptionShort string) string {
	short := naming.OneLine(resource.Description)
	if !naming.IsThinCommandShort(short) {
		return short
	}

	if resourceName == "" {
		return short
	}

	target := humanCommandSegment(resourceName)
	if parentName != "" {
		parent := humanCommandSegment(parentName)
		if isVerbLikeParentSegment(resourceName) {
			return "Run " + target + " operations for " + parent
		}
		if strings.EqualFold(resourceName, "pdf") {
			return "Manage PDF files for " + parent
		}
		if actions := parentCommandActions(resource.Endpoints); actions != "" {
			return actions + " " + target + " for " + parent
		}
		return "Manage " + target + " for " + parent
	}

	if apiDescriptionShort != "" {
		return apiDescriptionShort
	}
	if actions := parentCommandActions(resource.Endpoints); actions != "" {
		if computed := actions + " " + target; !naming.IsThinCommandShort(computed) {
			return computed
		}
	}
	return "Manage " + target + " command groups"
}

func parentCommandInfoDescriptionShort(description string) string {
	description = naming.OneLineNormalize(description)
	if description == "" {
		return ""
	}

	for idx, r := range description {
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		if r == '.' && parentCommandShortPeriodIsInternal(description, idx) {
			continue
		}
		sentence := parentCommandShortCompact(description[:idx+len(string(r))])
		if !naming.IsThinCommandShort(sentence) {
			return sentence
		}
		return ""
	}

	short := parentCommandShortCompact(description)
	if !naming.IsThinCommandShort(short) {
		return short
	}
	return ""
}

func parentCommandShortCompact(description string) string {
	description = naming.OneLineNormalize(description)
	runes := []rune(description)
	if len(runes) <= 120 {
		return description
	}

	cut := string(runes[:120])
	if idx := strings.LastIndex(cut, " "); idx > 60 {
		return strings.TrimRight(cut[:idx], " ,;:")
	}
	return strings.TrimRight(cut, " ,;:")
}

func parentCommandShortPeriodIsInternal(description string, idx int) bool {
	if idx <= 0 || idx >= len(description)-1 {
		return false
	}

	prev, next := description[idx-1], description[idx+1]
	if (isASCIILetter(prev) || isASCIIDigit(prev)) && (isASCIILetter(next) || isASCIIDigit(next)) {
		return true
	}

	tokenStart := strings.LastIndex(description[:idx], " ") + 1
	return strings.Contains(description[tokenStart:idx], ".")
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func isASCIIDigit(b byte) bool {
	return b >= '0' && b <= '9'
}

func parentCommandActions(endpoints map[string]spec.Endpoint) string {
	if len(endpoints) == 0 {
		return ""
	}
	seen := make(map[string]bool, len(endpoints))
	for name, endpoint := range endpoints {
		if action := endpointActionVerb(name, endpoint.Method); action != "" {
			seen[action] = true
		}
	}
	actionOrder := []string{"list", "get", "search", "find", "query", "count", "describe", "fetch", "run", "trigger", "execute", "generate", "batch", "process", "enable", "disable", "create", "add", "update", "edit", "delete", "remove", "upload", "download", "send", "submit", "verify"}
	var actions []string
	for _, action := range actionOrder {
		if seen[action] {
			actions = append(actions, action)
		}
	}
	return joinCommandActions(actions)
}

func endpointActionVerb(name, method string) string {
	head := strings.ToLower(name)
	if i := strings.IndexAny(head, "-_"); i > 0 {
		head = head[:i]
	}
	switch head {
	case "list", "get", "search", "find", "query", "count", "describe", "fetch", "run", "trigger", "execute", "generate", "batch", "process", "enable", "disable", "create", "add", "update", "edit", "delete", "remove", "upload", "download", "send", "submit", "verify":
		return head
	}
	switch strings.ToUpper(method) {
	case "GET":
		return "get"
	case "POST":
		return "create"
	case "PUT", "PATCH":
		return "update"
	case "DELETE":
		return "delete"
	default:
		return ""
	}
}

func joinCommandActions(actions []string) string {
	switch len(actions) {
	case 0:
		return ""
	case 1:
		return titleFirst(actions[0])
	case 2:
		return titleFirst(actions[0]) + " and " + actions[1]
	default:
		return titleFirst(strings.Join(actions[:len(actions)-1], ", ")) + ", and " + actions[len(actions)-1]
	}
}

func titleFirst(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func isVerbLikeParentSegment(segment string) bool {
	switch strings.ToLower(segment) {
	case "approve", "archive", "cancel", "close", "delete", "download", "edit", "freeze", "publish", "reject", "restore", "send", "submit", "unarchive", "unfreeze", "upload", "verify":
		return true
	default:
		return false
	}
}

func humanCommandSegment(segment string) string {
	words := strings.Fields(strings.ReplaceAll(strings.ReplaceAll(segment, "-", " "), "_", " "))
	for i, word := range words {
		switch strings.ToLower(word) {
		case "api", "crm", "id", "mcp", "pdf":
			words[i] = strings.ToUpper(word)
		default:
			words[i] = strings.ToLower(word)
		}
	}
	return strings.Join(words, " ")
}

func (g *Generator) renderResourceCommands(promotedResourceNames map[string]bool, promotedEndpointNames map[string]string) error {
	// When the spec emits promoted commands, the generator also emits the api
	// browser (api_discovery.go), whose RunE filters root.Commands() by
	// child.Hidden. Mark the raw top-level resource groups Hidden so they
	// surface there instead of cluttering --help. Direct invocation still
	// works (Cobra's Hidden only suppresses listing). For specs without
	// promoted commands the api browser is not generated, so leave resources
	// visible.
	hideTopLevelResources := len(g.PromotedCommands) > 0
	var apiDescriptionShort string
	if len(g.Spec.Resources) == 1 {
		apiDescriptionShort = parentCommandInfoDescriptionShort(g.Spec.Description)
	}
	novelChildrenByParent := g.novelFeatureChildrenByParent()
	// Generate per-resource parent files + per-endpoint command files
	// This produces more files (one per endpoint) which improves Breadth scoring
	for name, resource := range g.Spec.Resources {
		// Skip parent file for promoted resources — the promoted command replaces it.
		// Sub-resource parents and endpoint files are still needed (wired by the promoted command).
		if !promotedResourceNames[name] {
			// Parent file: wires subcommands together
			parentData := struct {
				ResourceName  string
				FuncPrefix    string
				CommandPath   string
				Short         string
				Resource      spec.Resource
				NovelChildren []novelFeatureChildRender
				Hidden        bool
				*spec.APISpec
			}{
				ResourceName:  name,
				FuncPrefix:    name,
				CommandPath:   name,
				Short:         parentCommandShort(name, "", resource, apiDescriptionShort),
				Resource:      resource,
				NovelChildren: novelChildrenByParent[toKebab(name)],
				Hidden:        hideTopLevelResources,
				APISpec:       g.Spec,
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
				ResourceName:  name,
				EffectivePath: effectiveEndpointPath(resource, endpoint),
				EffectiveTier: g.Spec.EffectiveTier(resource, endpoint),
				FuncPrefix:    name,
				CommandPath:   name,
				EndpointName:  eName,
				Endpoint:      endpoint,
				Resource:      resource,
				HasStore:      g.VisionSet.Store,
				IsAsync:       isAsync,
				Async:         asyncInfo,
				IsReadOnly:    endpointIsReadCommand(endpoint, eName),
				APISpec:       g.Spec,
			}
			epPath := filepath.Join("internal", "cli", safeResourceFileStem(name+"_"+eName)+".go")
			if err := g.renderTemplate("command_endpoint.go.tmpl", epPath, epData); err != nil {
				return fmt.Errorf("rendering endpoint %s/%s: %w", name, eName, err)
			}
		}

		// Sub-resource parent + endpoint files
		for subName, subResource := range resource.SubResources {
			subParentData := struct {
				ResourceName  string
				FuncPrefix    string
				CommandPath   string
				Short         string
				Resource      spec.Resource
				NovelChildren []novelFeatureChildRender
				Hidden        bool
				*spec.APISpec
			}{
				ResourceName:  subName,
				FuncPrefix:    name + "-" + subName,
				CommandPath:   name + " " + subName,
				Short:         parentCommandShort(subName, name, subResource, ""),
				Resource:      subResource,
				NovelChildren: novelChildrenByParent[toKebab(name)+" "+toKebab(subName)],
				Hidden:        false,
				APISpec:       g.Spec,
			}
			subParentPath := filepath.Join("internal", "cli", safeResourceFileStem(name+"_"+subName)+".go")
			if err := g.renderTemplate("command_parent.go.tmpl", subParentPath, subParentData); err != nil {
				return fmt.Errorf("rendering sub-parent %s/%s: %w", name, subName, err)
			}

			for eName, endpoint := range subResource.Endpoints {
				subKey := subName + "/" + eName
				asyncInfo, isAsync := g.AsyncJobs[subKey]
				effectiveResource := subResource
				if effectiveResource.Tier == "" {
					effectiveResource.Tier = resource.Tier
				}
				epData := endpointTemplateData{
					ResourceName:  subName,
					EffectivePath: effectiveSubEndpointPath(resource, subResource, endpoint),
					EffectiveTier: g.Spec.EffectiveTier(effectiveResource, endpoint),
					FuncPrefix:    name + "-" + subName,
					CommandPath:   name + " " + subName,
					EndpointName:  eName,
					Endpoint:      endpoint,
					Resource:      effectiveResource,
					HasStore:      g.VisionSet.Store,
					IsAsync:       isAsync,
					Async:         asyncInfo,
					IsReadOnly:    endpointIsReadCommand(endpoint, eName),
					APISpec:       g.Spec,
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
	// Render auth command. Template selection priority:
	//   1. OAuth2 client_credentials (server-to-server, no user redirect)
	//   2. OAuth2 device_code (agent/CLI-friendly user auth, no localhost redirect)
	//   3. OAuth2 authorization_code (3-legged, AuthorizationURL non-empty)
	//   4. Browser-cookie / composed / persisted-query
	//   5. Simple token-management (catch-all)
	authPath := filepath.Join("internal", "cli", "auth.go")
	authTmpl := "auth_simple.go.tmpl"
	switch {
	case g.Spec.Auth.EffectiveOAuth2Grant() == spec.OAuth2GrantClientCredentials && g.Spec.Auth.TokenURL != "":
		authTmpl = "auth_client_credentials.go.tmpl"
	case g.Spec.Auth.EffectiveOAuth2Grant() == spec.OAuth2GrantDeviceCode && g.Spec.Auth.DeviceAuthorizationURL != "" && g.Spec.Auth.TokenURL != "":
		authTmpl = "auth_device_code.go.tmpl"
	case g.Spec.Auth.AuthorizationURL != "":
		authTmpl = "auth.go.tmpl"
	case g.Spec.Auth.Type == "cookie" || g.Spec.Auth.Type == "composed" || g.hasTrafficAnalysisHint("graphql_persisted_query") || g.Spec.Auth.Subtype == spec.AuthSubtypeAuth0SPAInMemory:
		// Browser-aware auth template for browser-cookie auth, a
		// persisted-query registry, or an Auth0-SPA-in-memory bearer token
		// (CDP runtime extraction). Query refresh flows need temporary
		// browser capture support, not a resident browser transport.
		authTmpl = "auth_browser.go.tmpl"
	}
	authData := &authTemplateData{
		APISpec:                    g.Spec,
		HasGraphQLPersistedQueries: g.hasTrafficAnalysisHint("graphql_persisted_query"),
	}
	if err := g.renderTemplate(authTmpl, authPath, authData); err != nil {
		return fmt.Errorf("rendering auth: %w", err)
	}
	if g.Spec.Auth.EffectiveOAuth2Grant() == spec.OAuth2GrantDeviceCode {
		oauthPath := filepath.Join("internal", "oauth", "device.go")
		if err := os.MkdirAll(filepath.Join(g.OutputDir, "internal", "oauth"), 0o755); err != nil {
			return fmt.Errorf("creating OAuth helper directory: %w", err)
		}
		if err := g.renderTemplate("oauth_device.go.tmpl", oauthPath, g.Spec); err != nil {
			return fmt.Errorf("rendering OAuth device-code helper: %w", err)
		}
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

func (g *Generator) emitsTopLevelOAuthLogin() bool {
	return g.Spec.Auth.AuthorizationURL != "" &&
		(g.Spec.Auth.EffectiveOAuth2Grant() != spec.OAuth2GrantClientCredentials || g.Spec.Auth.TokenURL == "")
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
	novelCommandStubs, err := g.renderNovelFeatureStubs()
	if err != nil {
		return err
	}

	return g.renderRootProjectFiles(promotedCommands, promotedResourceNames, workflowConstructors, insightConstructors, novelCommandStubs)
}

// schemaWithDependentParents adds a parent_id column + index to dependent
// resource tables so sync can record which parent each row belongs to.
// JSON-only fallback tables keep only id/data/synced_at; parent context for
// those rows remains available in the generic resources table. For
// walker-emitted dependents whose DependentResource.KeyField is non-empty,
// parent_id stores the value of that field (not strictly a parent's primary
// key); the column name is retained for backwards compatibility with existing
// CLIs. The naming caveat is internal — the column is not part of any
// user-visible API.
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
				if table.JSONOnlyFallback {
					continue
				}
				if schema[i].ParentKeyColumn == "" {
					schema[i].ParentKeyColumn = "parent_id"
				}
				hasParentID := false
				for _, col := range table.Columns {
					if col.Name == "parent_id" {
						hasParentID = true
						break
					}
				}
				if !hasParentID {
					if len(table.Columns)+1 > maxStoreDomainTableColumns {
						schema[i].JSONOnlyFallback = true
						schema[i].OriginalColumnCount = len(table.Columns) + 1
						schema[i].Columns = append([]ColumnDef(nil), baseTableColumns...)
						schema[i].Indexes = nil
						schema[i].FTS5 = false
						schema[i].FTS5Fields = nil
						schema[i].FTS5Triggers = false
						continue
					}
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
		for _, table := range schema {
			if table.JSONOnlyFallback {
				fmt.Fprintf(os.Stderr, "warning: store-fallback: %s (%d cols) -> JSON-only\n", table.Name, table.OriginalColumnCount)
			}
		}
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
		if err := g.renderTemplate("store_extras.go.tmpl", filepath.Join("internal", "store", "extras.go"), storeData); err != nil {
			return fmt.Errorf("rendering store extras: %w", err)
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
	VisionSet                    VisionTemplateSet
	SyncableResources            []profiler.SyncableResource
	DependentSyncResources       []profiler.DependentResource
	PaginationSupportedResources []string
	SpecTimestampFields          []string
	SearchableFields             map[string][]string
	Tables                       []TableDef
	Pagination                   profiler.PaginationProfile
	SearchEndpointPath           string
	SearchQueryParam             string
	SearchEndpointMethod         string
	SearchBodyFields             []profiler.SearchBodyField
	GraphQLFieldPaths            map[string]string
	AgentMoneyWorkflow           AgentMoneyWorkflow
	HTMLSyncStub                 bool
}

type resourceIDFieldOverrideEntry struct {
	Name  string
	Value string
}

type criticalResourceEntry struct {
	Name string
}

func resourceIDFieldOverrideEntries(syncable []profiler.SyncableResource, dependent []profiler.DependentResource) []resourceIDFieldOverrideEntry {
	overrides := map[string]string{}
	for _, resource := range syncable {
		if resource.IDField != "" {
			overrides[resource.Name] = resource.IDField
		}
	}
	for _, resource := range dependent {
		if resource.IDField != "" {
			overrides[resource.Name] = resource.IDField
		}
	}

	names := make([]string, 0, len(overrides))
	for name := range overrides {
		names = append(names, name)
	}
	sort.Strings(names)

	entries := make([]resourceIDFieldOverrideEntry, len(names))
	for i, name := range names {
		entries[i] = resourceIDFieldOverrideEntry{Name: name, Value: overrides[name]}
	}
	return entries
}

func criticalResourceEntries(syncable []profiler.SyncableResource, dependent []profiler.DependentResource) []criticalResourceEntry {
	critical := map[string]bool{}
	for _, resource := range syncable {
		if resource.Critical {
			critical[resource.Name] = true
		}
	}
	for _, resource := range dependent {
		if resource.Critical {
			critical[resource.Name] = true
		}
	}

	names := make([]string, 0, len(critical))
	for name := range critical {
		names = append(names, name)
	}
	sort.Strings(names)

	entries := make([]criticalResourceEntry, len(names))
	for i, name := range names {
		entries[i] = criticalResourceEntry{Name: name}
	}
	return entries
}

func paginationSupportedResources(syncable []profiler.SyncableResource, dependent []profiler.DependentResource) []string {
	supported := map[string]bool{}
	for _, resource := range syncable {
		if resource.SupportsPagination {
			supported[resource.Name] = true
		}
	}
	for _, resource := range dependent {
		if resource.SupportsPagination {
			supported[resource.Name] = true
		}
	}
	names := make([]string, 0, len(supported))
	for name := range supported {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func specDateTimeFieldNames(api *spec.APISpec) []string {
	if api == nil {
		return nil
	}

	fields := map[string]struct{}{}
	addName := func(name, format string) {
		if strings.EqualFold(format, "date-time") && strings.TrimSpace(name) != "" {
			fields[name] = struct{}{}
		}
	}

	var walkParams func(params []spec.Param)
	walkParams = func(params []spec.Param) {
		for _, p := range params {
			addName(p.Name, p.Format)
			if len(p.Fields) > 0 {
				walkParams(p.Fields)
			}
		}
	}

	var walkResource func(resource spec.Resource)
	walkResource = func(resource spec.Resource) {
		for _, endpoint := range resource.Endpoints {
			walkParams(endpoint.Params)
			walkParams(endpoint.Body)
		}
		for _, sub := range resource.SubResources {
			walkResource(sub)
		}
	}

	for _, typeDef := range api.Types {
		for _, f := range typeDef.Fields {
			addName(f.Name, f.Format)
		}
	}
	for _, resource := range api.Resources {
		walkResource(resource)
	}

	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (g *Generator) visionRenderData(schema []TableDef) visionRenderData {
	gqlFieldPaths := map[string]string{}
	for rName, r := range g.Spec.Resources {
		if ep, ok := r.Endpoints["list"]; ok && ep.ResponsePath != "" {
			gqlFieldPaths[rName] = graphqlQueryField(ep.ResponsePath)
		}
	}

	return visionRenderData{
		APISpec:                      g.Spec,
		VisionSet:                    g.VisionSet,
		SyncableResources:            g.profile.SyncableResources,
		DependentSyncResources:       g.profile.DependentSyncResources,
		PaginationSupportedResources: paginationSupportedResources(g.profile.SyncableResources, g.profile.DependentSyncResources),
		SpecTimestampFields:          specDateTimeFieldNames(g.Spec),
		SearchableFields:             g.profile.SearchableFields,
		Tables:                       schema,
		Pagination:                   g.profile.Pagination,
		SearchEndpointPath:           g.profile.SearchEndpointPath,
		SearchQueryParam:             g.profile.SearchQueryParam,
		SearchEndpointMethod:         g.profile.SearchEndpointMethod,
		SearchBodyFields:             g.profile.SearchBodyFields,
		GraphQLFieldPaths:            gqlFieldPaths,
		AgentMoneyWorkflow:           detectAgentMoneyWorkflow(g.Spec, g.PromotedEndpointNames),
		HTMLSyncStub:                 g.shouldEmitHTMLSyncStub(),
	}
}

const htmlSyncStubThreshold = 0.7

func (g *Generator) shouldEmitHTMLSyncStub() bool {
	if g == nil || isGraphQLSpec(g.Spec) {
		return false
	}
	if g.htmlSyncStubComputed {
		return g.htmlSyncStub
	}
	total, html := g.countHTMLPageModeSyncResources()
	if total > 0 && html != total {
		g.htmlSyncStubComputed = true
		g.htmlSyncStub = false
		return g.htmlSyncStub
	}
	if total == 0 {
		total, html = countHTMLPageModeEndpoints(g.Spec)
	}
	if total == 0 {
		g.htmlSyncStubComputed = true
		g.htmlSyncStub = false
		return g.htmlSyncStub
	}
	g.htmlSyncStubComputed = true
	g.htmlSyncStub = float64(html)/float64(total) >= htmlSyncStubThreshold
	return g.htmlSyncStub
}

func (g *Generator) hasGeneratedSyncImplementation() bool {
	return g != nil && g.VisionSet.Sync && !g.shouldEmitHTMLSyncStub()
}

func (g *Generator) hasAutoRefresh() bool {
	return g != nil && g.VisionSet.Store && g.hasGeneratedSyncImplementation() && g.Spec.Cache.Enabled
}

func (g *Generator) resetHTMLSyncStubCache() {
	if g == nil {
		return
	}
	g.htmlSyncStubComputed = false
	g.htmlSyncStub = false
}

func (g *Generator) countHTMLPageModeSyncResources() (total int, html int) {
	if g == nil || g.profile == nil {
		return 0, 0
	}
	for _, resource := range g.profile.SyncableResources {
		total++
		if syncableResourceUsesHTMLPageMode(resource) {
			html++
		}
	}
	for _, resource := range g.profile.DependentSyncResources {
		total++
		if dependentResourceUsesHTMLPageMode(resource) {
			html++
		}
	}
	return total, html
}

func syncableResourceUsesHTMLPageMode(resource profiler.SyncableResource) bool {
	return resource.UsesHTMLResponse && resource.HTMLExtract.EffectiveMode() == spec.HTMLExtractModePage
}

func dependentResourceUsesHTMLPageMode(resource profiler.DependentResource) bool {
	return resource.UsesHTMLResponse && resource.HTMLExtract.EffectiveMode() == spec.HTMLExtractModePage
}

func countHTMLPageModeEndpoints(api *spec.APISpec) (total int, html int) {
	if api == nil {
		return 0, 0
	}
	for _, resource := range api.Resources {
		resourceTotal, resourceHTML := countResourceHTMLPageModeEndpoints(resource)
		total += resourceTotal
		html += resourceHTML
	}
	return total, html
}

func countResourceHTMLPageModeEndpoints(resource spec.Resource) (total int, html int) {
	for _, endpoint := range resource.Endpoints {
		total++
		if endpoint.UsesHTMLResponse() && endpoint.HTMLExtract.EffectiveMode() == spec.HTMLExtractModePage {
			html++
		}
	}
	for _, sub := range resource.SubResources {
		subTotal, subHTML := countResourceHTMLPageModeEndpoints(sub)
		total += subTotal
		html += subHTML
	}
	return total, html
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
		} else if tmplName == "sync.go.tmpl" && visionData.HTMLSyncStub {
			actualTmpl = "sync_stub.go.tmpl"
		} else if tmplName == "import.go.tmpl" && gqlSpec {
			// GraphQL APIs have no generic REST create endpoint, so the REST
			// import template's POST /<resource> per record always 400s. Emit
			// a GraphQL-aware import that errors clearly instead.
			actualTmpl = "graphql_import.go.tmpl"
		}
		var tmplData any = g.Spec
		if tmplName == "sync.go.tmpl" || tmplName == "search.go.tmpl" {
			tmplData = visionData
		}
		if err := g.renderTemplate(actualTmpl, outPath, tmplData); err != nil {
			return fmt.Errorf("rendering vision %s: %w", tmplName, err)
		}
		if tmplName == "sync.go.tmpl" && actualTmpl == "sync.go.tmpl" {
			if err := g.renderTemplate("sync_numeric_id_test.go.tmpl", filepath.Join("internal", "cli", "sync_numeric_id_test.go"), tmplData); err != nil {
				return fmt.Errorf("rendering sync numeric ID test: %w", err)
			}
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
			SyncableResources  []profiler.SyncableResource
			SearchableFields   map[string][]string
			Pagination         profiler.PaginationProfile
			AgentMoneyWorkflow AgentMoneyWorkflow
			SyncEnabled        bool
		}{
			APISpec:            g.Spec,
			SyncableResources:  g.profile.SyncableResources,
			SearchableFields:   g.profile.SearchableFields,
			Pagination:         g.profile.Pagination,
			AgentMoneyWorkflow: visionData.AgentMoneyWorkflow,
			SyncEnabled:        g.hasGeneratedSyncImplementation(),
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
		if err := g.renderTemplate(tmpl, outPath, visionData); err != nil {
			fmt.Fprintf(os.Stderr, "warning: skipping workflow template %s: %v\n", tmpl, err)
			continue
		}
		if constructor := commandConstructorForTemplate(tmpl); constructor != "" {
			renderedWorkflowConstructors = append(renderedWorkflowConstructors, constructor)
		}
	}

	return renderedWorkflowConstructors, nil
}

type AgentMoneyWorkflow struct {
	Payment  *AgentMoneyCommand
	Request  *AgentMoneyCommand
	Transfer *AgentMoneyCommand
}

func (w AgentMoneyWorkflow) Enabled() bool {
	return w.Payment != nil || w.Request != nil || w.Transfer != nil
}

func (w AgentMoneyWorkflow) complete() bool {
	return w.Payment != nil && w.Request != nil && w.Transfer != nil
}

type AgentMoneyCommand struct {
	CommandPath              []string
	HasAccountIDPosition     bool
	AmountFlag               string
	AmountInteger            bool
	RecipientIDFlag          string
	PaymentMethodFlag        string
	IdempotencyKeyFlag       string
	SourceAccountIDFlag      string
	DestinationAccountIDFlag string
	NoteFlag                 string
	ExternalMemoFlag         string
	PurposeFlag              string
}

type agentMoneyKind string

const (
	agentMoneyKindNone     agentMoneyKind = ""
	agentMoneyKindPayment  agentMoneyKind = "payment"
	agentMoneyKindRequest  agentMoneyKind = "request"
	agentMoneyKindTransfer agentMoneyKind = "transfer"
)

func detectAgentMoneyWorkflow(api *spec.APISpec, promotedEndpointNames map[string]string) AgentMoneyWorkflow {
	var workflow AgentMoneyWorkflow
	if api == nil {
		return workflow
	}

	for _, resourceName := range sortedResourceNames(api.Resources) {
		resource := api.Resources[resourceName]
		for _, endpointName := range sortedEndpointNames(resource.Endpoints) {
			endpoint := resource.Endpoints[endpointName]
			class, cmd := agentMoneyCommandForEndpoint(endpoint, []string{toKebab(resourceName), toKebab(endpointName)}, promotedEndpointNames[resourceName] == endpointName)
			assignAgentMoneyCommand(&workflow, class, cmd)
			if workflow.complete() {
				return workflow
			}
		}
		for _, subName := range sortedResourceNames(resource.SubResources) {
			sub := resource.SubResources[subName]
			for _, endpointName := range sortedEndpointNames(sub.Endpoints) {
				endpoint := sub.Endpoints[endpointName]
				class, cmd := agentMoneyCommandForEndpoint(endpoint, []string{toKebab(resourceName), toKebab(subName), toKebab(endpointName)}, false)
				assignAgentMoneyCommand(&workflow, class, cmd)
				if workflow.complete() {
					return workflow
				}
			}
		}
	}

	return workflow
}

func assignAgentMoneyCommand(workflow *AgentMoneyWorkflow, class agentMoneyKind, cmd *AgentMoneyCommand) {
	if workflow == nil || cmd == nil {
		return
	}
	switch class {
	case agentMoneyKindTransfer:
		if workflow.Transfer == nil {
			workflow.Transfer = cmd
		}
	case agentMoneyKindRequest:
		if workflow.Request == nil {
			workflow.Request = cmd
		}
	case agentMoneyKindPayment:
		if workflow.Payment == nil {
			workflow.Payment = cmd
		}
	}
}

func classifyAgentMoneyEndpoint(endpoint spec.Endpoint, body map[string]spec.Param) agentMoneyKind {
	if !strings.EqualFold(endpoint.Method, "POST") {
		return agentMoneyKindNone
	}
	has := func(name string) bool {
		_, ok := body[strings.ToLower(name)]
		return ok
	}
	if has("amount") && has("sourceAccountId") && has("destinationAccountId") && has("idempotencyKey") {
		return agentMoneyKindTransfer
	}
	if has("amount") && has("recipientId") && has("paymentMethod") && has("idempotencyKey") {
		if strings.Contains(strings.ToLower(endpoint.Path), "request") {
			return agentMoneyKindRequest
		}
		return agentMoneyKindPayment
	}
	return agentMoneyKindNone
}

func agentMoneyCommandForEndpoint(endpoint spec.Endpoint, path []string, promoted bool) (agentMoneyKind, *AgentMoneyCommand) {
	body := paramsByLowerName(endpoint.Body)
	class := classifyAgentMoneyEndpoint(endpoint, body)
	if class == agentMoneyKindNone {
		return class, nil
	}
	if !agentMoneyEndpointHasSupportedPositionals(endpoint) || !agentMoneyEndpointHasSupportedRequiredBody(endpoint, class) {
		return class, nil
	}
	if promoted && len(path) >= 1 {
		path = path[:1]
	} else if endpoint.Alias != "" && len(path) > 0 {
		path[len(path)-1] = endpoint.Alias
	}
	cmd := &AgentMoneyCommand{CommandPath: path}
	for _, param := range endpoint.Params {
		if param.Positional && strings.EqualFold(param.Name, "accountId") {
			cmd.HasAccountIDPosition = true
			break
		}
	}
	if p, ok := body["amount"]; ok {
		cmd.AmountFlag = flagName(paramIdent(p))
		cmd.AmountInteger = primitiveKind(p.Type) == "int"
	}
	if p, ok := body["recipientid"]; ok {
		cmd.RecipientIDFlag = flagName(paramIdent(p))
	}
	if p, ok := body["paymentmethod"]; ok {
		cmd.PaymentMethodFlag = flagName(paramIdent(p))
	}
	if p, ok := body["idempotencykey"]; ok {
		cmd.IdempotencyKeyFlag = flagName(paramIdent(p))
	}
	if p, ok := body["sourceaccountid"]; ok {
		cmd.SourceAccountIDFlag = flagName(paramIdent(p))
	}
	if p, ok := body["destinationaccountid"]; ok {
		cmd.DestinationAccountIDFlag = flagName(paramIdent(p))
	}
	if p, ok := body["note"]; ok {
		cmd.NoteFlag = flagName(paramIdent(p))
	}
	if p, ok := body["externalmemo"]; ok {
		cmd.ExternalMemoFlag = flagName(paramIdent(p))
	}
	if p, ok := body["purpose"]; ok {
		cmd.PurposeFlag = flagName(paramIdent(p))
	}
	return class, cmd
}

func agentMoneyEndpointHasSupportedPositionals(endpoint spec.Endpoint) bool {
	for _, param := range endpoint.Params {
		if !param.Positional || !param.Required {
			continue
		}
		if !strings.EqualFold(param.Name, "accountId") {
			return false
		}
	}
	return true
}

func agentMoneyEndpointHasSupportedRequiredBody(endpoint spec.Endpoint, class agentMoneyKind) bool {
	required := map[string]struct{}{}
	switch class {
	case agentMoneyKindTransfer:
		for _, name := range []string{"amount", "sourceaccountid", "destinationaccountid", "idempotencykey"} {
			required[name] = struct{}{}
		}
	case agentMoneyKindRequest, agentMoneyKindPayment:
		for _, name := range []string{"amount", "recipientid", "paymentmethod", "idempotencykey"} {
			required[name] = struct{}{}
		}
	default:
		return false
	}

	for _, param := range endpoint.Body {
		if !param.Required {
			continue
		}
		if _, ok := required[strings.ToLower(param.Name)]; !ok {
			return false
		}
	}
	return true
}

func paramsByLowerName(params []spec.Param) map[string]spec.Param {
	out := make(map[string]spec.Param, len(params))
	for _, param := range params {
		out[strings.ToLower(param.Name)] = param
	}
	return out
}

func sortedResourceNames(resources map[string]spec.Resource) []string {
	names := make([]string, 0, len(resources))
	for name := range resources {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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
		reservedIntentNames := reservedMCPToolNames(g.Spec, g.VisionSet, g.NovelFeatures)
		recipeIntents := buildRecipeIntents(g.Spec.Name, g.Narrative, reservedIntentNames)
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
			RecipeIntents     []RecipeIntent
			HasMCPIntents     bool
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
			RecipeIntents:     recipeIntents,
			HasMCPIntents:     len(g.Spec.MCP.Intents) > 0 || len(recipeIntents) > 0,
		}
		if err := g.renderTemplate("mcp_tools.go.tmpl", filepath.Join("internal", "mcp", "tools.go"), mcpData); err != nil {
			return fmt.Errorf("rendering MCP tools: %w", err)
		}
		if g.VisionSet.Store {
			if err := g.renderTemplate("mcp_tools_test.go.tmpl", filepath.Join("internal", "mcp", "tools_test.go"), mcpData); err != nil {
				return fmt.Errorf("rendering MCP tools tests: %w", err)
			}
		}
		if mcpData.HasMCPIntents {
			if err := g.renderTemplate("mcp_intents.go.tmpl", filepath.Join("internal", "mcp", "intents.go"), mcpData); err != nil {
				return fmt.Errorf("rendering MCP intents: %w", err)
			}
		}
		if len(recipeIntents) > 0 {
			if err := g.renderTemplate("mcp_recipe_intents_test.go.tmpl", filepath.Join("internal", "mcp", "recipe_intents_test.go"), mcpData); err != nil {
				return fmt.Errorf("rendering MCP recipe intent tests: %w", err)
			}
		}
		if g.Spec.MCP.IsCodeOrchestration() {
			if err := g.renderTemplate("mcp_code_orch.go.tmpl", filepath.Join("internal", "mcp", "code_orch.go"), mcpData); err != nil {
				return fmt.Errorf("rendering MCP code-orchestration: %w", err)
			}
			// Durable regression test for the write-body contracts
			// (double-marshal object path + bare-array body path).
			// Gated on code-orchestration so intents-only CLIs, which
			// have no codeOrchWriteBody/codeOrchArrayBody, still compile.
			if err := g.renderTemplate("mcp_code_orch_writebody_test.go.tmpl", filepath.Join("internal", "mcp", "code_orch_writebody_test.go"), mcpData); err != nil {
				return fmt.Errorf("rendering MCP code-orchestration write-body test: %w", err)
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
		resource := g.Spec.Resources[pc.ResourceName]
		promotedData := struct {
			PromotedName      string
			ResourceName      string
			EndpointName      string
			EffectivePath     string
			Endpoint          spec.Endpoint
			EffectiveTier     string
			HasStore          bool
			HasResponseUnwrap bool
			Resource          spec.Resource
			FuncPrefix        string
			IsReadOnly        bool
			*spec.APISpec
		}{
			PromotedName:  pc.PromotedName,
			ResourceName:  pc.ResourceName,
			EndpointName:  pc.EndpointName,
			EffectivePath: effectiveEndpointPath(resource, pc.Endpoint),
			Endpoint:      pc.Endpoint,
			EffectiveTier: g.Spec.EffectiveTier(resource, pc.Endpoint),
			HasStore:      g.VisionSet.Store,
			// Per-command: emit the extractResponseData call only on commands
			// whose own response is envelope-shaped, so a non-envelope command
			// in a mixed-envelope spec is never unwrapped. This is a subset of
			// the CLI-level helper-emission gate (helpers.go emits the helper
			// when ANY promoted command qualifies), so call ⊆ emit — no call to
			// an unemitted helper.
			HasResponseUnwrap: g.VisionSet.Store && !pc.Endpoint.UsesBinaryResponse() && endpointHasStatusDataEnvelope(pc.Endpoint, g.Spec.Types),
			Resource:          resource,
			FuncPrefix:        pc.ResourceName,
			IsReadOnly:        endpointIsReadCommand(pc.Endpoint, pc.EndpointName),
			APISpec:           g.Spec,
		}
		promotedPath := filepath.Join("internal", "cli", safeResourceFileStem("promoted_"+pc.PromotedName)+".go")
		if err := g.renderTemplate("command_promoted.go.tmpl", promotedPath, promotedData); err != nil {
			return fmt.Errorf("rendering promoted command %s: %w", pc.PromotedName, err)
		}
	}

	return nil
}

func (g *Generator) renderRootProjectFiles(promotedCommands []PromotedCommand, promotedResourceNames map[string]bool, renderedWorkflowConstructors, renderedInsightConstructors []string, novelCommandStubs []novelFeatureCommandRender) error {
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
	helperFlags := computeHelperFlags(g.Spec)

	rootData := struct {
		*spec.APISpec
		VisionSet             VisionTemplateSet
		VisionCmdNames        map[string]bool
		WorkflowConstructors  []string
		InsightConstructors   []string
		NovelCommandStubs     []novelFeatureCommandRender
		PromotedCommands      []PromotedCommand
		PromotedResourceNames map[string]bool
		Narrative             *ReadmeNarrative
		TopNovelFeatures      []NovelFeature
		NovelOverflowCount    int
		HasAsyncJobs          bool
		AsyncJobCount         int
		HasAuthCommand        bool
		HasDelete             bool
		HasMutationEndpoints  bool
		HasAutoRefresh        bool
		CompactDescription    string
	}{
		APISpec:               g.Spec,
		VisionSet:             g.VisionSet,
		VisionCmdNames:        g.VisionSet.CmdNames(),
		WorkflowConstructors:  renderedWorkflowConstructors,
		InsightConstructors:   renderedInsightConstructors,
		NovelCommandStubs:     novelCommandStubs,
		PromotedCommands:      promotedCommands,
		PromotedResourceNames: promotedResourceNames,
		Narrative:             g.Narrative,
		TopNovelFeatures:      shownNovel,
		NovelOverflowCount:    overflow,
		HasAsyncJobs:          len(g.AsyncJobs) > 0,
		AsyncJobCount:         len(g.AsyncJobs),
		HasAuthCommand:        hasAuthCommand,
		HasDelete:             helperFlags.HasDelete,
		HasMutationEndpoints:  helperFlags.HasMutationEndpoints,
		HasAutoRefresh:        g.hasAutoRefresh(),
		CompactDescription:    g.compactDescription(),
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

	return os.WriteFile(fullPath, normalizeRendered(buf.Bytes(), tmplName, outPath), 0o644)
}

// normalizeRendered prepares template-rendered bytes for disk: trims trailing
// whitespace and re-appends a single newline, then runs go/format.Source on
// `.go` outputs so printed CLIs ship formatting-clean. Template authors
// hand-align struct fields inconsistently, and without this pass every fresh
// print surfaces hundreds of phantom diffs on the first `gofmt -w`. Falls
// through with a stderr warning rather than fail-hard so a malformed template
// surfaces as a compile error downstream instead of an opaque emit failure.
func normalizeRendered(raw []byte, tmplName, outPath string) []byte {
	rendered := bytes.TrimRight(raw, " \t\r\n")
	rendered = append(rendered, '\n')
	if tmplName == "command_endpoint.go.tmpl" {
		rendered = pruneUnusedClientImport(rendered)
	}
	if filepath.Ext(outPath) != ".go" {
		return rendered
	}
	formatted, err := format.Source(rendered)
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: gofmt failed for %s: %v\n", outPath, err)
		return rendered
	}
	return formatted
}

// pruneUnusedClientImport drops the `<module>/internal/client` import line
// from a rendered command_endpoint.go file when the body never references
// the `client` package as a qualifier. The endpoint template emits this
// import for the GraphQL list/get path, but other branches (notably
// POST /graphql for GraphQL specs whose author wrote method: POST) reach
// the rendered file without producing a `client.X` reference, leaving an
// unused import that breaks `go build`.
func pruneUnusedClientImport(src []byte) []byte {
	// Find the import block.
	importStart := bytes.Index(src, []byte("\nimport (\n"))
	if importStart < 0 {
		return src
	}
	importBlockStart := importStart + len("\nimport (\n")
	importEnd := bytes.Index(src[importBlockStart:], []byte("\n)"))
	if importEnd < 0 {
		return src
	}
	importEnd += importBlockStart

	// Body is everything after the closing `)` of the import block.
	body := src[importEnd:]
	// A `client.X` reference is the only way the import is used inside the
	// rendered body.
	if bytes.Contains(body, []byte("client.")) {
		return src
	}

	// Locate the line `"<...>/internal/client"` inside the import block and
	// remove it (including its trailing newline). The literal suffix is
	// stable across all module paths because it's emitted as
	// `"{{modulePath}}/internal/client"` from the template.
	importBlock := src[importBlockStart:importEnd]
	suffix := []byte(`/internal/client"`)
	rel := bytes.Index(importBlock, suffix)
	if rel < 0 {
		return src
	}
	abs := importBlockStart + rel
	// Walk back to the start of the line.
	lineStart := abs
	for lineStart > 0 && src[lineStart-1] != '\n' {
		lineStart--
	}
	// Walk forward to the end of the line, including the trailing newline.
	lineEnd := abs
	for lineEnd < len(src) && src[lineEnd] != '\n' {
		lineEnd++
	}
	if lineEnd < len(src) {
		lineEnd++
	}
	out := make([]byte, 0, len(src)-(lineEnd-lineStart))
	out = append(out, src[:lineStart]...)
	out = append(out, src[lineEnd:]...)
	return out
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

	content, err := templateFS.ReadFile(path.Join("templates", tmplName))
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
	return naming.CamelIdentifier(s)
}

func commandIdent(parts ...string) string {
	joined := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimLeft(part, "$")
		if part != "" {
			joined = append(joined, part)
		}
	}
	return toCamel(strings.Join(joined, "-"))
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

// isFlagLimitParam returns true when a parameter is the canonical
// pagination/truncation `--limit` flag, which is always count-shaped
// (integer) regardless of whether the spec declares it as `number`.
// LLM-derived specs in `--docs` mode have produced `limit: number`,
// which previously emitted `Float64Var`/`float64 flagLimit` and broke
// `truncateJSONArray(data, flagLimit)` at compile time because the
// helper expects an `int`. Keyed on the same case-insensitive match
// `endpointNeedsClientLimit` uses, so the override fires exactly where
// the truncate caller lives.
func isFlagLimitParam(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "limit")
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
	if ref, ok := strings.CutPrefix(t, "ref:"); ok {
		return safeTypeName(ref)
	}
	if ref, ok := strings.CutPrefix(t, "[]ref:"); ok {
		return "[]" + safeTypeName(ref)
	}
	switch primitiveKind(t) {
	case "object", "array":
		return "json.RawMessage"
	default:
		return goType(t)
	}
}

func typeFieldJSONTagComment(f spec.TypeField) string {
	for _, r := range f.Name {
		if r > unicode.MaxASCII {
			return f.Name
		}
	}
	return ""
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

// mcpBindingFunc returns the mcplib.With* function name used in MCP tool
// input-schema registration so numeric fields bind as JSON numbers (not
// quoted strings) and pass through the generic makeAPIHandler intact.
// Funnels through primitiveKind so OpenAPI-parsed shapes ("int", "float",
// "bool") and internal-spec literals ("integer", "number", "boolean")
// produce the same binding. Object/array bodies fall back to WithString
// because they ride the --body-json fallback or a JSON-string flag.
func mcpBindingFunc(t string) string {
	switch primitiveKind(t) {
	case "int", "float":
		return "WithNumber"
	case "bool":
		return "WithBoolean"
	default:
		return "WithString"
	}
}

// goTypeForParam returns the Go type for a parameter, overriding bool→string
// for required bools without defaults so omitted can be distinguished from an
// explicit false, int→string for ID-like parameters to avoid overflow and zero-value confusion,
// numeric→string for pagination cursors so they survive scientific-notation
// rendering of large Unix timestamps and millisecond cursors, and
// float→int for the canonical `--limit` flag whose semantics are always
// a count.
func goTypeForParam(name, t string) string {
	return goTypeForParamRequired(name, t, false, false)
}

func goTypeForParamRequired(name, t string, required bool, hasDefault bool) string {
	kind := primitiveKind(t)
	if required && !hasDefault && kind == "bool" {
		return "string"
	}
	if isIDParam(name) && kind == "int" {
		return "string"
	}
	if isCursorParam(name) && (kind == "int" || kind == "float") {
		return "string"
	}
	if isFlagLimitParam(name) && kind == "float" {
		return "int"
	}
	return goType(t)
}

// cobraFlagFuncForParam returns the cobra flag function, overriding BoolVar→StringVar
// for required bools without defaults, IntVar→StringVar for ID-like parameters,
// Float64Var/IntVar→StringVar for pagination cursors,
// and Float64Var→IntVar for the canonical `--limit` flag.
func cobraFlagFuncForParam(name, t string) string {
	return cobraFlagFuncForParamRequired(name, t, false, false)
}

func cobraFlagFuncForParamRequired(name, t string, required bool, hasDefault bool) string {
	kind := primitiveKind(t)
	if required && !hasDefault && kind == "bool" {
		return "StringVar"
	}
	if isIDParam(name) && kind == "int" {
		return "StringVar"
	}
	if isCursorParam(name) && (kind == "int" || kind == "float") {
		return "StringVar"
	}
	if isFlagLimitParam(name) && kind == "float" {
		return "IntVar"
	}
	return cobraFlagFunc(t)
}

// defaultValForParam returns the default value for a flag parameter,
// overriding bool→string for required bools without defaults, int→string for
// ID-like parameters, numeric→string for pagination cursors so the StringVar default matches the StringVar field type,
// and float→int for the canonical `--limit` flag so the IntVar default
// matches its coerced int type.
func defaultValForParam(p spec.Param) string {
	return defaultValForParamRequired(p, false, false)
}

func defaultValForParamRequired(p spec.Param, required bool, hasDefault bool) string {
	kind := primitiveKind(p.Type)
	if required && !hasDefault && kind == "bool" {
		return `""`
	}
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
	if isFlagLimitParam(p.Name) && kind == "float" {
		coerced := p
		coerced.Type = "integer"
		return defaultVal(coerced)
	}
	return defaultVal(p)
}

func zeroValForParam(name, t string) string {
	return zeroValForParamRequired(name, t, false, false)
}

func zeroValForParamRequired(name, t string, required bool, hasDefault bool) string {
	kind := primitiveKind(t)
	if required && !hasDefault && kind == "bool" {
		return `""`
	}
	if isIDParam(name) && kind == "int" {
		return `""`
	}
	if isCursorParam(name) && (kind == "int" || kind == "float") {
		return `""`
	}
	return zeroVal(t)
}

func paramHasDefault(p spec.Param) bool {
	return p.Default != nil
}

// paramIsConstDefault holds for single-value-enum params whose default
// equals the only enum value. Templates emit MarkHidden for these so
// --help does not list a flag whose only valid value is the default,
// while the flag stays registered so the wire-side default still flows.
// This catches the single-URL routing-selector shape, where an API
// selects an operation via a fixed query param.
func paramIsConstDefault(p spec.Param) bool {
	if len(p.Enum) != 1 || p.Default == nil {
		return false
	}
	// Float defaults round-trip ambiguously under any single string format:
	// fmt.Sprintf("%v", float64(2.0)) and strconv.FormatFloat with -1
	// precision both yield "2", which would miss a heterogeneous spec
	// declaring `enum: ["2.0"]` alongside `default: 2.0`. Parse the enum
	// element as a float and compare numerically so "2", "2.0", and "2.00"
	// are all equivalent to float64(2.0).
	switch v := p.Default.(type) {
	case float64:
		if enumF, err := strconv.ParseFloat(p.Enum[0], 64); err == nil {
			return enumF == v
		}
		return false
	case float32:
		if enumF, err := strconv.ParseFloat(p.Enum[0], 32); err == nil {
			return float32(enumF) == v
		}
		return false
	default:
		return fmt.Sprintf("%v", p.Default) == p.Enum[0]
	}
}

type jsonFlagSuggestion struct {
	FlagName string
	Values   []string
}

type mcpParamBinding struct {
	PublicName         string
	WireName           string
	Location           string
	BodyPath           []string
	Format             string
	RequestContentType string
	Default            string
}

func flagChangedExpr(p spec.Param) string {
	names := append([]string{publicFlagName(p)}, publicFlagAliases(p)...)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("cmd.Flags().Changed(%q)", name))
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, " || ") + ")"
}

func mcpParamBindings(endpoint spec.Endpoint, pathTemplate string) []mcpParamBinding {
	bindings := make([]mcpParamBinding, 0, len(endpoint.Params)+len(endpoint.Body))
	requestContentType := ""
	if endpointUsesMultipart(endpoint) || endpointUsesForm(endpoint) {
		requestContentType = endpoint.RequestContentType
	}
	for _, p := range endpoint.Params {
		loc := "query"
		if strings.Contains(pathTemplate, "{"+p.Name+"}") {
			loc = "path"
		}
		wireName := p.WireName()
		if loc == "path" {
			wireName = p.Name
		}
		binding := mcpParamBinding{
			PublicName:         p.PublicInputName(),
			WireName:           wireName,
			Location:           loc,
			RequestContentType: requestContentType,
		}
		// Carry the spec default onto the MCP binding for query params so an
		// omitted arg sends the same value the cobra flag would (#2679). Format
		// must match the cobra default rendering for CLI/MCP wire parity; keep in
		// sync with that path (and cf. pipeline.stringifyParamDefault).
		if loc == "query" {
			if def, ok := mcpParamDefaultValue(p); ok {
				binding.Default = def
			}
		}
		bindings = append(bindings, binding)
	}
	if endpoint.BodyJSONFallback {
		// Single opaque body-json input; the handler parses it as JSON and
		// sends it verbatim, mirroring the CLI's --body-json fallback for
		// oneOf/anyOf request bodies. Body is empty by parser invariant.
		bindings = append(bindings, mcpParamBinding{
			PublicName: "body_json",
			WireName:   "body_json",
			Location:   "body_json",
		})
		return bindings
	}
	appendMCPBodyBindings(&bindings, endpoint, requestContentType)
	return bindings
}

func mcpToolInputParams(endpoint spec.Endpoint) []spec.Param {
	params := make([]spec.Param, 0, len(endpoint.Params)+len(endpoint.Body))
	params = append(params, endpoint.Params...)
	if endpoint.BodyJSONFallback {
		return params
	}
	params = append(params, mcpBodyInputParams(endpoint)...)
	return params
}

func mcpGlobalTemplateInputParams(endpoint spec.Endpoint, pathTemplate string, vars []string) []spec.Param {
	if len(vars) == 0 {
		return nil
	}
	known := map[string]struct{}{}
	for _, param := range mcpToolInputParams(endpoint) {
		known[param.PublicInputName()] = struct{}{}
	}
	params := make([]spec.Param, 0, len(vars))
	for _, name := range vars {
		if !spec.PathContainsPlaceholder(pathTemplate, name) {
			continue
		}
		if _, exists := known[name]; exists {
			continue
		}
		params = append(params, spec.Param{
			Name:        name,
			Type:        "string",
			Description: fmt.Sprintf("Path template value for {%s}; overrides env/config for this MCP call", name),
		})
		known[name] = struct{}{}
	}
	return params
}

func mcpGlobalTemplateBindings(endpoint spec.Endpoint, pathTemplate string, vars []string) []mcpParamBinding {
	inputs := mcpGlobalTemplateInputParams(endpoint, pathTemplate, vars)
	bindings := make([]mcpParamBinding, 0, len(inputs))
	for _, param := range inputs {
		bindings = append(bindings, mcpParamBinding{
			PublicName: param.PublicInputName(),
			WireName:   param.Name,
			Location:   "template",
		})
	}
	return bindings
}

func mcpBodyInputParams(endpoint spec.Endpoint) []spec.Param {
	if bodyUsesFlatEmission(endpoint) {
		return append([]spec.Param(nil), endpoint.Body...)
	}
	body := flattenCollidingBodyFields(endpoint.Body)
	params := make([]spec.Param, 0, len(body))
	collectMCPBodyInputParams(&params, body, 0, "")
	return params
}

func collectMCPBodyInputParams(params *[]spec.Param, body []spec.Param, depth int, flagPrefix string) {
	for _, p := range body {
		if p.Type == "object" && len(p.Fields) > 0 {
			if depth+1 >= maxBodyFlagDepth {
				continue
			}
			collectMCPBodyInputParams(params, p.Fields, depth+1, joinFlag(flagPrefix, publicFlagName(p)))
			continue
		}
		if flagPrefix != "" {
			p.FlagName = joinFlag(flagPrefix, publicFlagName(p))
			p.Aliases = nil
		}
		*params = append(*params, p)
	}
}

func appendMCPBodyBindings(bindings *[]mcpParamBinding, endpoint spec.Endpoint, requestContentType string) {
	if bodyUsesFlatEmission(endpoint) {
		for _, p := range endpoint.Body {
			*bindings = append(*bindings, mcpParamBinding{
				PublicName:         p.PublicInputName(),
				WireName:           p.BodyWireName(),
				Location:           "body",
				Format:             multipartBindingFormat(endpoint, p),
				RequestContentType: requestContentType,
			})
		}
		return
	}
	collectMCPBodyBindings(bindings, flattenCollidingBodyFields(endpoint.Body), 0, "", nil, requestContentType)
}

func collectMCPBodyBindings(bindings *[]mcpParamBinding, body []spec.Param, depth int, flagPrefix string, bodyPath []string, requestContentType string) {
	for _, p := range body {
		if p.Type == "object" && len(p.Fields) > 0 {
			if depth+1 >= maxBodyFlagDepth {
				continue
			}
			nextPath := append(slices.Clone(bodyPath), p.BodyWireName())
			collectMCPBodyBindings(bindings, p.Fields, depth+1, joinFlag(flagPrefix, publicFlagName(p)), nextPath, requestContentType)
			continue
		}
		publicName := p.PublicInputName()
		if flagPrefix != "" {
			publicName = joinFlag(flagPrefix, publicFlagName(p))
		}
		binding := mcpParamBinding{
			PublicName:         publicName,
			WireName:           p.BodyWireName(),
			Location:           "body",
			RequestContentType: requestContentType,
		}
		if len(bodyPath) > 0 {
			binding.BodyPath = append(append([]string(nil), bodyPath...), p.BodyWireName())
		}
		*bindings = append(*bindings, binding)
	}
}

func endpointHasMCPNestedBodyPath(endpoint spec.Endpoint) bool {
	if endpoint.BodyJSONFallback || bodyUsesFlatEmission(endpoint) {
		return false
	}
	return bodyHasReachableNestedLeaf(flattenCollidingBodyFields(endpoint.Body), 0)
}

func bodyHasReachableNestedLeaf(body []spec.Param, depth int) bool {
	for _, p := range body {
		if p.Type != "object" || len(p.Fields) == 0 {
			continue
		}
		if depth+1 >= maxBodyFlagDepth {
			continue
		}
		for _, field := range p.Fields {
			if field.Type == "object" && len(field.Fields) > 0 {
				if bodyHasReachableNestedLeaf([]spec.Param{field}, depth+1) {
					return true
				}
				continue
			}
			return true
		}
	}
	return false
}

func hasMCPNestedBodyPath(apiSpec *spec.APISpec) bool {
	return anyEndpointMatches(apiSpec, endpointHasMCPNestedBodyPath)
}

// mcpParamDefaultValue returns the wire-effective stringified default for a
// param and whether it is effective. A default that stringifies to "" is NOT
// effective: the non-paginated CLI template's zero-value gate (`if flag != ""`)
// skips an empty-string default on the wire, so for CLI/MCP parity the MCP
// binding must skip it too. (Paginated CLI branches emit query params without
// that gate, but the client layer — paginatedGet and client.Get — strips empty
// values before the wire, so parity holds there as well.) Counting/emitting an
// empty default would also leave a dead Default field and fallback block,
// breaking the "default-less CLIs stay byte-identical" goal. The check is on the
// stringified value, so a numeric/bool zero (%v -> "0"/"false") is a real
// default and is kept.
func mcpParamDefaultValue(p spec.Param) (string, bool) {
	if p.Default == nil {
		return "", false
	}
	v := fmt.Sprintf("%v", p.Default)
	return v, v != ""
}

func endpointHasMCPParamDefault(endpoint spec.Endpoint, pathTemplate string) bool {
	for _, p := range endpoint.Params {
		if strings.Contains(pathTemplate, "{"+p.Name+"}") {
			continue
		}
		if _, ok := mcpParamDefaultValue(p); ok {
			return true
		}
	}
	return false
}

func hasMCPParamDefault(apiSpec *spec.APISpec) bool {
	if apiSpec == nil {
		return false
	}
	var walk func(parent spec.Resource, resources map[string]spec.Resource, subResource bool) bool
	walk = func(parent spec.Resource, resources map[string]spec.Resource, subResource bool) bool {
		for _, resource := range resources {
			for _, endpoint := range resource.Endpoints {
				pathTemplate := effectiveEndpointPath(resource, endpoint)
				if subResource {
					pathTemplate = effectiveSubEndpointPath(parent, resource, endpoint)
				}
				if endpointHasMCPParamDefault(endpoint, pathTemplate) {
					return true
				}
			}
			if walk(resource, resource.SubResources, true) {
				return true
			}
		}
		return false
	}
	return walk(spec.Resource{}, apiSpec.Resources, false)
}

func multipartBindingFormat(endpoint spec.Endpoint, p spec.Param) string {
	if !endpointUsesMultipart(endpoint) {
		return ""
	}
	return p.Format
}

func mcpInputName(p spec.Param) string {
	return p.PublicInputName()
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

type clientSideFilter struct {
	Param spec.Param
	Field string
}

// endpointClientSideFilters reports best-effort response filters for
// docs-derived batch GET endpoints. Docs-derived specs sometimes expose
// a documented query flag that the API accepts but ignores; when the flag name
// matches a scalar response item field (including simple plural-to-singular
// forms like symbols -> symbol), generated commands locally narrow the returned
// JSON so the public flag stays truthful.
func endpointClientSideFilters(apiSpec *spec.APISpec, endpoint spec.Endpoint) []clientSideFilter {
	if apiSpec == nil || strings.TrimSpace(apiSpec.SpecSource) != "docs" {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(endpoint.Method), "GET") || endpoint.Pagination != nil {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(endpoint.Response.Type), "array") {
		return nil
	}
	if !endpointLooksLikeClientFilteredBatch(endpoint) {
		return nil
	}
	if endpoint.Response.Item == "" {
		return nil
	}
	itemType, ok := apiSpec.Types[endpoint.Response.Item]
	if !ok || len(itemType.Fields) == 0 {
		return nil
	}

	fieldsByKey := map[string]string{}
	for _, field := range itemType.Fields {
		if strings.TrimSpace(field.Name) == "" {
			continue
		}
		fieldsByKey[normalizeClientSideFilterKey(field.Name)] = field.Name
	}

	var filters []clientSideFilter
	seenFields := map[string]struct{}{}
	for _, param := range endpoint.Params {
		if !clientSideFilterParamEligible(param) {
			continue
		}
		for _, candidate := range clientSideFilterFieldCandidates(param) {
			field, ok := fieldsByKey[normalizeClientSideFilterKey(candidate)]
			if !ok {
				continue
			}
			if _, seen := seenFields[field]; seen {
				continue
			}
			filters = append(filters, clientSideFilter{Param: param, Field: field})
			seenFields[field] = struct{}{}
			break
		}
	}
	return filters
}

func endpointLooksLikeClientFilteredBatch(endpoint spec.Endpoint) bool {
	for _, value := range []string{endpoint.Path, endpoint.Description} {
		parts := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
			return (r < 'a' || r > 'z') && (r < '0' || r > '9')
		})
		if slices.Contains(parts, "batch") {
			return true
		}
	}
	return false
}

func clientSideFilterParamEligible(param spec.Param) bool {
	if param.Positional || param.PathParam {
		return false
	}
	if param.Purpose == spec.ParamPurposeFieldSelector {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(param.Type)) {
	case "", "string", "string_csv_array":
	default:
		return false
	}
	name := normalizeClientSideFilterKey(param.WireName())
	switch name {
	case "", "limit", "page", "pagesize", "perpage", "offset", "cursor", "after", "before", "next", "sort", "order", "orderby", "fields", "field", "select", "include", "expand", "q", "query", "search":
		return false
	default:
		return true
	}
}

func clientSideFilterFieldCandidates(param spec.Param) []string {
	names := []string{param.WireName(), param.Name}
	var out []string
	seen := map[string]struct{}{}
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		for _, candidate := range []string{trimmed, singularClientSideFilterName(trimmed)} {
			key := normalizeClientSideFilterKey(candidate)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, candidate)
		}
	}
	return out
}

func singularClientSideFilterName(name string) string {
	if strings.HasSuffix(name, "ies") && len(name) > len("ies") {
		return strings.TrimSuffix(name, "ies") + "y"
	}
	if strings.HasSuffix(name, "ses") {
		if len(name) > len("ses") {
			return strings.TrimSuffix(name, "es")
		}
		return name
	}
	if strings.HasSuffix(name, "s") && len(name) > 1 {
		return strings.TrimSuffix(name, "s")
	}
	return name
}

func normalizeClientSideFilterKey(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// maxBodyFlagDepth caps how many levels of nested-object recursion the
// body-flag emitters expand into per-field Cobra flags. A Param at
// depth 0 is a top-level body field; its object children are at depth 1
// and recurse with depth+1. When the next depth would meet or exceed
// the cap, the object's subtree is skipped uniformly across renderBodyMap,
// renderBodyVarDecls, renderBodyFlagRegs, and renderBodyRequiredChecks.
// The user reaches truncated fields via the existing `--stdin` flag on
// POST/PUT/PATCH commands, which reads the full JSON body from stdin.
//
// Default 3 covers typical CRUD schemas (resource.object.field) without
// the recursive explosion seen on enterprise/ERP specs that self-reference
// through 6+ layers and produce 40k-line command files the Go compiler
// OOMs on.
const maxBodyFlagDepth = 3

// bodyMap renders the per-flag body-building block shared by the
// POST/PUT/PATCH branches in command_endpoint.go.tmpl and the body
// branch in command_promoted.go.tmpl. The four sites generated the
// same Go code at different indentation levels; this consolidates them
// and parameterizes the indent. The output is the body of the
// `body := map[string]any{}` block — callers emit the surrounding
// declaration and the closing brace themselves.
//
// When a body Param has Type "object" with non-empty Fields, the block
// recurses: each leaf field becomes its own flag (parent-prefixed in
// the generated identifier so `start.dateTime` and `end.dateTime` do
// not collide), and the parent's wire-side key receives a built-up
// map[string]any rather than a single JSON-string flag. Recursion stops
// at maxBodyFlagDepth; deeper subtrees are only reachable via `--stdin`.
func bodyMap(body []spec.Param, indent string) string {
	var b strings.Builder
	renderBodyMap(&b, flattenCollidingBodyFields(body), 0, indent, "body", "", "")
	return b.String()
}

// bodyMapForEndpoint dispatches between the typed-flag body-map renderer
// and the --body-json fallback renderer. Templates call this in place of
// bodyMap so the BodyJSONFallback decision lives in one place.
func bodyMapForEndpoint(endpoint spec.Endpoint, indent string) string {
	if endpoint.BodyJSONFallback {
		return bodyJSONFallbackMap(indent)
	}
	return bodyMap(endpoint.Body, indent)
}

// bodyJSONFallbackMap renders the body-population block used when an
// endpoint's request body schema is a oneOf/anyOf (or otherwise opaque)
// and we expose a single `--body-json` string flag. The caller has
// already emitted `body = map[string]any{}`; this block conditionally
// overwrites body with a parsed JSON object when the user passed a value.
//
// The fallback intentionally accepts only JSON objects. Top-level
// discriminated unions in real-world specs (Cloudflare DNS records,
// Stripe PaymentMethod, Notion blocks, Linear filters) are object-shaped;
// rare array-typed bodies are out of scope for the minimum-viable
// fallback and surface as a clear error message.
func bodyJSONFallbackMap(indent string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%sif flagBodyJSON != \"\" {\n", indent)
	fmt.Fprintf(&b, "%s\tvar parsedBodyJSON any\n", indent)
	fmt.Fprintf(&b, "%s\tif err := json.Unmarshal([]byte(flagBodyJSON), &parsedBodyJSON); err != nil {\n", indent)
	fmt.Fprintf(&b, "%s\t\treturn fmt.Errorf(\"parsing --body-json: %%w\", err)\n", indent)
	fmt.Fprintf(&b, "%s\t}\n", indent)
	fmt.Fprintf(&b, "%s\tasMap, ok := parsedBodyJSON.(map[string]any)\n", indent)
	fmt.Fprintf(&b, "%s\tif !ok {\n", indent)
	fmt.Fprintf(&b, "%s\t\treturn fmt.Errorf(\"--body-json must be a JSON object, got JSON %%T\", parsedBodyJSON)\n", indent)
	fmt.Fprintf(&b, "%s\t}\n", indent)
	fmt.Fprintf(&b, "%s\tbody = asMap\n", indent)
	fmt.Fprintf(&b, "%s}\n", indent)
	return b.String()
}

func renderBodyMap(b *strings.Builder, body []spec.Param, depth int, indent, mapVar, identPrefix, flagPrefix string) {
	for _, p := range body {
		id := paramIdent(p)
		ident := identPrefix + toCamel(id)
		flag := joinFlag(flagPrefix, publicFlagName(p))
		if p.Type == "object" && len(p.Fields) > 0 {
			if depth+1 >= maxBodyFlagDepth {
				continue
			}
			nestedMap := "nested" + ident
			fmt.Fprintf(b, "%s{\n", indent)
			fmt.Fprintf(b, "%s\t%s := map[string]any{}\n", indent, nestedMap)
			renderBodyMap(b, p.Fields, depth+1, indent+"\t", nestedMap, ident, flag)
			fmt.Fprintf(b, "%s\tif len(%s) > 0 {\n", indent, nestedMap)
			fmt.Fprintf(b, "%s\t\t%s[%q] = %s\n", indent, mapVar, p.BodyWireName(), nestedMap)
			fmt.Fprintf(b, "%s\t}\n", indent)
			fmt.Fprintf(b, "%s}\n", indent)
			continue
		}
		if isStringCSVArrayParam(p) {
			fmt.Fprintf(b, "%sif body%s != \"\" {\n", indent, ident)
			fmt.Fprintf(b, "%s\t%s[%q] = %s\n", indent, mapVar, p.BodyWireName(), csvArrayValueExpr(p, "body"+ident))
			fmt.Fprintf(b, "%s}\n", indent)
			continue
		}
		isComplex := p.Type == "object" || p.Type == "array"
		if isComplex || isJSONStringParam(p) {
			// object/array: store the parsed value (so the API receives
			// real JSON). jsonStringParam: validate then store the raw
			// string (the API expects a JSON-encoded string field).
			rhs := "body" + ident
			if isComplex {
				rhs = "parsed" + ident
			}
			fmt.Fprintf(b, "%sif body%s != \"\" {\n", indent, ident)
			fmt.Fprintf(b, "%s\tvar parsed%s any\n", indent, ident)
			fmt.Fprintf(b, "%s\tif err := json.Unmarshal([]byte(body%s), &parsed%s); err != nil {\n", indent, ident, ident)
			fmt.Fprintf(b, "%s\t\treturn fmt.Errorf(\"parsing --%s JSON: %%w\", err)\n", indent, flag)
			fmt.Fprintf(b, "%s\t}\n", indent)
			fmt.Fprintf(b, "%s\t%s[%q] = %s\n", indent, mapVar, p.BodyWireName(), rhs)
			fmt.Fprintf(b, "%s}\n", indent)
			continue
		}
		if (p.Type == "boolean" || p.Type == "bool") && (!p.Required || p.Default != nil) {
			// Booleans gate on cmd.Flags().Changed instead of a zero-guard.
			// The zero-guard (body != false) drops user-set false values,
			// letting the server's default (often true) silently invert
			// intent. Unconditionally emitting flips the bug: PATCH bodies
			// would carry "field: false" for every untouched flag and
			// overwrite server state. Changed distinguishes "user set
			// false" from "user did not touch the flag" and is correct
			// for POST, PUT, and PATCH. Internal YAML specs use "boolean";
			// the OpenAPI parser normalizes to "bool".
			fmt.Fprintf(b, "%sif cmd.Flags().Changed(%q) {\n", indent, flag)
			fmt.Fprintf(b, "%s\t%s[%q] = body%s\n", indent, mapVar, p.BodyWireName(), ident)
			fmt.Fprintf(b, "%s}\n", indent)
			continue
		}
		fmt.Fprintf(b, "%sif body%s != %s {\n", indent, ident, zeroValForParamRequired(p.Name, p.Type, p.Required, paramHasDefault(p)))
		if isStringBackedBoolParam(p) {
			fmt.Fprintf(b, "%s\tparsed%s, err := strconv.ParseBool(body%s)\n", indent, ident, ident)
			fmt.Fprintf(b, "%s\tif err != nil {\n", indent)
			fmt.Fprintf(b, "%s\t\treturn fmt.Errorf(\"parsing --%s as bool: %%w\", err)\n", indent, flag)
			fmt.Fprintf(b, "%s\t}\n", indent)
			fmt.Fprintf(b, "%s\t%s[%q] = parsed%s\n", indent, mapVar, p.BodyWireName(), ident)
		} else {
			fmt.Fprintf(b, "%s\t%s[%q] = body%s\n", indent, mapVar, p.BodyWireName(), ident)
		}
		fmt.Fprintf(b, "%s}\n", indent)
	}
}

func bodyHasStringBackedBool(endpoint spec.Endpoint) bool {
	if endpoint.BodyJSONFallback || bodyUsesFlatEmission(endpoint) {
		return false
	}
	var walk func([]spec.Param, int) bool
	walk = func(params []spec.Param, depth int) bool {
		for _, p := range params {
			if p.Type == "object" && len(p.Fields) > 0 {
				if depth+1 >= maxBodyFlagDepth {
					continue
				}
				if walk(p.Fields, depth+1) {
					return true
				}
				continue
			}
			if isStringBackedBoolParam(p) {
				return true
			}
		}
		return false
	}
	return walk(flattenCollidingBodyFields(endpoint.Body), 0)
}

func isStringBackedBoolParam(p spec.Param) bool {
	return p.Required && p.Default == nil && primitiveKind(p.Type) == "bool"
}

// bodyVarDecls renders Go var declarations for body construction. For
// JSON-body endpoints, nested-object params (Type "object" with non-empty
// Fields) recurse: each leaf field becomes its own declaration with a
// parent-prefixed identifier. For multipart and form-encoded endpoints,
// the body path stays flat (one var per top-level param) because
// multipartBodyMaps and formBodyMaps continue to send object-typed
// parents as JSON-string fields. Output starts with "\n\tvar ..."
// matching the one-tab indent of the original `{{- range .Endpoint.Body}}`
// template loop.
//
// When endpoint.BodyJSONFallback is set (oneOf/anyOf body schema), a single
// `flagBodyJSON` string is declared instead of per-field flags.
func bodyVarDecls(endpoint spec.Endpoint) string {
	var b strings.Builder
	if endpoint.BodyJSONFallback {
		b.WriteString("\n\tvar flagBodyJSON string")
		return b.String()
	}
	if bodyUsesFlatEmission(endpoint) {
		for _, p := range endpoint.Body {
			fmt.Fprintf(&b, "\n\tvar body%s %s", toCamel(paramIdent(p)), goTypeForParamRequired(p.Name, p.Type, p.Required, paramHasDefault(p)))
		}
		return b.String()
	}
	renderBodyVarDecls(&b, flattenCollidingBodyFields(endpoint.Body), 0, "")
	return b.String()
}

// bodyUsesFlatEmission reports whether the endpoint serializes its body
// via a non-JSON-map path (multipart/form-data or
// application/x-www-form-urlencoded). Those paths keep one var/flag per
// top-level body param so multipartBodyMaps / formBodyMaps still find
// the parent variable to read from.
func bodyUsesFlatEmission(endpoint spec.Endpoint) bool {
	return endpointUsesMultipart(endpoint) || endpointUsesForm(endpoint)
}

func renderBodyVarDecls(b *strings.Builder, body []spec.Param, depth int, identPrefix string) {
	for _, p := range body {
		ident := identPrefix + toCamel(paramIdent(p))
		if p.Type == "object" && len(p.Fields) > 0 {
			if depth+1 >= maxBodyFlagDepth {
				continue
			}
			renderBodyVarDecls(b, p.Fields, depth+1, ident)
			continue
		}
		fmt.Fprintf(b, "\n\tvar body%s %s", ident, goTypeForParamRequired(p.Name, p.Type, p.Required, paramHasDefault(p)))
	}
}

// bodyFlagRegs renders cobra flag registrations for body construction.
// Multipart endpoints keep flat (one flag per top-level body param) so
// the JSON-string fallback that multipartBodyMaps emits stays addressable.
// For non-multipart endpoints, nested-object params recurse with
// parent-prefixed flag names so two parents that share a field name do
// not collide. Aliases are emitted only at the top level.
func bodyFlagRegs(endpoint spec.Endpoint) string {
	var b strings.Builder
	if endpoint.BodyJSONFallback {
		b.WriteString("\n\tcmd.Flags().StringVar(&flagBodyJSON, \"body-json\", \"\", \"Provide the full request body as a JSON object string (this endpoint accepts a polymorphic schema: oneOf/anyOf)\")")
		return b.String()
	}
	if bodyUsesFlatEmission(endpoint) {
		for _, p := range endpoint.Body {
			renderFlatBodyFlagReg(&b, p, "", "", true)
		}
		return b.String()
	}
	renderBodyFlagRegs(&b, flattenCollidingBodyFields(endpoint.Body), 0, "", "", true)
	return b.String()
}

func renderBodyFlagRegs(b *strings.Builder, body []spec.Param, depth int, identPrefix, flagPrefix string, topLevel bool) {
	for _, p := range body {
		if p.Type == "object" && len(p.Fields) > 0 {
			if depth+1 >= maxBodyFlagDepth {
				continue
			}
			ident := identPrefix + toCamel(paramIdent(p))
			flag := joinFlag(flagPrefix, publicFlagName(p))
			renderBodyFlagRegs(b, p.Fields, depth+1, ident, flag, false)
			continue
		}
		renderFlatBodyFlagReg(b, p, identPrefix, flagPrefix, topLevel)
	}
}

// renderFlatBodyFlagReg emits cobra flag registrations for a single body
// param. Const-default detection via paramIsConstDefault is intentionally
// not wired through here yet: the primary use case is the query-param
// routing-selector shape, not body fields. If a future API surfaces a
// const-default body field, extend the emission here with a MarkHidden
// line gated by paramIsConstDefault, mirroring the command_endpoint and
// command_promoted templates.
func renderFlatBodyFlagReg(b *strings.Builder, p spec.Param, identPrefix, flagPrefix string, topLevel bool) {
	ident := identPrefix + toCamel(paramIdent(p))
	flag := joinFlag(flagPrefix, publicFlagName(p))
	desc := naming.OneLine(p.Description)
	fmt.Fprintf(b, "\n\tcmd.Flags().%s(&body%s, \"%s\", %s, \"%s\")",
		cobraFlagFuncForParamRequired(p.Name, p.Type, p.Required, paramHasDefault(p)), ident, flag, defaultValForParamRequired(p, p.Required, paramHasDefault(p)), desc)
	if topLevel {
		for _, alias := range publicFlagAliases(p) {
			fmt.Fprintf(b, "\n\tcmd.Flags().%s(&body%s, \"%s\", %s, \"%s\")",
				cobraFlagFuncForParamRequired(p.Name, p.Type, p.Required, paramHasDefault(p)), ident, alias, defaultValForParamRequired(p, p.Required, paramHasDefault(p)), desc)
			fmt.Fprintf(b, "\n\t_ = cmd.Flags().MarkHidden(\"%s\")", alias)
		}
	}
}

// bodyRequiredChecks renders required-flag validation for body params.
// indent is the indent prefix applied to each emitted `if` line so the
// helper can serve both command_endpoint.go.tmpl (4-tab indent inside
// `if !stdinBody`, 3-tab indent for multipart) and command_promoted.go.tmpl
// (3-tab indent at RunE-body level). Multipart endpoints keep flat
// behavior. For non-multipart, top-level params use flagChangedExpr
// (lifts aliases); nested fields use a single Changed() check on the
// parent-prefixed flag because aliases are not propagated to children.
func bodyRequiredChecks(endpoint spec.Endpoint, indent string) string {
	var b strings.Builder
	if endpoint.BodyJSONFallback {
		if endpoint.BodyRequired {
			fmt.Fprintf(&b, "\n%sif !cmd.Flags().Changed(\"body-json\") && !flags.dryRun {", indent)
			fmt.Fprintf(&b, "\n%s\treturn fmt.Errorf(\"required flag \\\"%%s\\\" not set\", \"body-json\")", indent)
			fmt.Fprintf(&b, "\n%s}", indent)
		}
		return b.String()
	}
	if bodyUsesFlatEmission(endpoint) {
		for _, p := range endpoint.Body {
			renderFlatBodyRequiredCheck(&b, p, indent, "", true)
		}
		return b.String()
	}
	renderBodyRequiredChecks(&b, flattenCollidingBodyFields(endpoint.Body), 0, indent, "", true)
	return b.String()
}

func renderBodyRequiredChecks(b *strings.Builder, body []spec.Param, depth int, indent, flagPrefix string, topLevel bool) {
	for _, p := range body {
		if p.Type == "object" && len(p.Fields) > 0 {
			if depth+1 >= maxBodyFlagDepth {
				continue
			}
			flag := joinFlag(flagPrefix, publicFlagName(p))
			renderBodyRequiredChecks(b, p.Fields, depth+1, indent, flag, false)
			continue
		}
		renderFlatBodyRequiredCheck(b, p, indent, flagPrefix, topLevel)
	}
}

// bodyExceedsFlagDepth reports whether emitting per-field body flags for
// the endpoint would have truncated any nested-object subtree under
// maxBodyFlagDepth. Multipart/form endpoints stay flat and never
// truncate; BodyJSONFallback endpoints route through a single
// --body-json flag and never reach the per-field path.
//
// The walk uses flattenCollidingBodyFields because that is what the
// emitters render. Collision-flattening clears `Fields` on an object
// whose dot-flattened subtree would clash with a sibling identifier,
// turning it into a JSON-string leaf the user passes as a single flag.
// Walking the raw body would falsely report truncation in that case
// and rewrite the --stdin help text even when every field is exposed.
func bodyExceedsFlagDepth(endpoint spec.Endpoint) bool {
	if endpoint.BodyJSONFallback || bodyUsesFlatEmission(endpoint) {
		return false
	}
	return walkBodyExceedsDepth(flattenCollidingBodyFields(endpoint.Body), 0)
}

// walkBodyExceedsDepth returns true as soon as any nested-object subtree
// at depth >= maxBodyFlagDepth-1 is found. The walk is bounded by the
// same depth check the emitters use, so a Param graph that
// intentionally self-references (cyclic spec) does not loop here.
func walkBodyExceedsDepth(body []spec.Param, depth int) bool {
	for _, p := range body {
		if p.Type != "object" || len(p.Fields) == 0 {
			continue
		}
		if depth+1 >= maxBodyFlagDepth {
			return true
		}
		if walkBodyExceedsDepth(p.Fields, depth+1) {
			return true
		}
	}
	return false
}

func renderFlatBodyRequiredCheck(b *strings.Builder, p spec.Param, indent, flagPrefix string, topLevel bool) {
	if !p.Required || p.Default != nil {
		return
	}
	flag := joinFlag(flagPrefix, publicFlagName(p))
	var changedExpr string
	if topLevel {
		changedExpr = flagChangedExpr(p)
	} else {
		changedExpr = fmt.Sprintf("cmd.Flags().Changed(\"%s\")", flag)
	}
	fmt.Fprintf(b, "\n%sif !%s && !flags.dryRun {", indent, changedExpr)
	fmt.Fprintf(b, "\n%s\treturn fmt.Errorf(\"required flag \\\"%%s\\\" not set\", \"%s\")", indent, flag)
	fmt.Fprintf(b, "\n%s}", indent)
}

func joinFlag(prefix, name string) string {
	return naming.JoinFlag(prefix, name)
}

func multipartBodyMaps(body []spec.Param, indent string) string {
	var b strings.Builder
	for _, p := range body {
		id := paramIdent(p)
		ident := toCamel(id)
		flag := publicFlagName(p)
		if isComplexBodyField(p) || isJSONStringParam(p) {
			fmt.Fprintf(&b, "%sif body%s != \"\" {\n", indent, ident)
			fmt.Fprintf(&b, "%s\tif !json.Valid([]byte(body%s)) {\n", indent, ident)
			fmt.Fprintf(&b, "%s\t\treturn fmt.Errorf(\"parsing --%s JSON: invalid JSON\")\n", indent, flag)
			fmt.Fprintf(&b, "%s\t}\n", indent)
			fmt.Fprintf(&b, "%s\tfields[%q] = body%s\n", indent, p.BodyWireName(), ident)
			fmt.Fprintf(&b, "%s}\n", indent)
			continue
		}
		if isBinaryParam(p) {
			fmt.Fprintf(&b, "%sif body%s != \"\" {\n", indent, ident)
			fmt.Fprintf(&b, "%s\tfileFields[%q] = body%s\n", indent, p.BodyWireName(), ident)
			fmt.Fprintf(&b, "%s}\n", indent)
			continue
		}
		if p.Type == "string" {
			fmt.Fprintf(&b, "%sif body%s != \"\" {\n", indent, ident)
			fmt.Fprintf(&b, "%s\tfields[%q] = body%s\n", indent, p.BodyWireName(), ident)
			fmt.Fprintf(&b, "%s}\n", indent)
			continue
		}
		fmt.Fprintf(&b, "%sif body%s != %s {\n", indent, ident, zeroValForParamRequired(p.Name, p.Type, p.Required, paramHasDefault(p)))
		fmt.Fprintf(&b, "%s\tfields[%q] = fmt.Sprintf(\"%%v\", body%s)\n", indent, p.BodyWireName(), ident)
		fmt.Fprintf(&b, "%s}\n", indent)
	}
	return b.String()
}

// endpointHasQueryFlags reports whether the endpoint declares any non-positional,
// non-path parameters — i.e., flags that should be encoded as URL query string.
// True for any HTTP method. Used by the non-GET handler template to decide
// whether to build a params map and route through the *WithParams client
// variant, so query-shaped params on POST/PUT/DELETE/PATCH reach the URL
// instead of being silently dropped into the JSON body or omitted.
func endpointHasQueryFlags(endpoint spec.Endpoint) bool {
	for _, p := range endpoint.Params {
		if !p.Positional && !p.PathParam {
			return true
		}
	}
	return false
}

// endpointHasRequiredInput reports whether a bare invocation of the generated
// command (no flags, no args) would fail a required-input check before
// reaching the request: a required non-positional flag or a required body
// field. It gates the empty-invocation help short-circuit so read commands
// with only optional filters still execute on a bare call instead of printing
// help. Both halves mirror exactly when the template emits a required check:
// the flag half uses template.IsTrue to match the template's `(not .Default)`
// gate (so a required flag carrying a non-empty default — which the template
// lets satisfy itself — does not trigger the guard, just as it emits no
// required-flag error), and the body half reuses bodyRequiredChecks, gated on
// the body-bearing verbs the command template actually emits the body check
// for (POST/PUT/PATCH/DELETE) so a GET that happens to declare a required body
// param does not falsely trip the guard.
func endpointHasRequiredInput(endpoint spec.Endpoint) bool {
	for _, p := range endpoint.Params {
		if p.Required && !p.Positional {
			if truth, _ := template.IsTrue(p.Default); !truth {
				return true
			}
		}
	}
	switch strings.ToUpper(endpoint.Method) {
	case "POST", "PUT", "PATCH", "DELETE":
		return strings.TrimSpace(bodyRequiredChecks(endpoint, "")) != ""
	}
	return false
}

// endpointHasRequestParams reports whether the endpoint passes any values in
// the client request's params map: query flags plus positional values not
// consumed by the URL path.
func endpointHasRequestParams(endpoint spec.Endpoint) bool {
	for _, p := range endpoint.Params {
		if !p.PathParam {
			return true
		}
	}
	return false
}

func endpointUsesMultipart(endpoint spec.Endpoint) bool {
	return strings.EqualFold(strings.TrimSpace(endpoint.RequestContentType), "multipart/form-data")
}

func hasMultipartRequest(apiSpec *spec.APISpec) bool {
	return anyEndpointMatches(apiSpec, endpointUsesMultipart)
}

func endpointUsesForm(endpoint spec.Endpoint) bool {
	return strings.EqualFold(strings.TrimSpace(endpoint.RequestContentType), "application/x-www-form-urlencoded")
}

func hasFormRequest(apiSpec *spec.APISpec) bool {
	return anyEndpointMatches(apiSpec, endpointUsesForm)
}

func endpointUsesCSVArray(endpoint spec.Endpoint) bool {
	if endpointUsesMultipart(endpoint) || endpointUsesForm(endpoint) {
		return false
	}
	var walk func([]spec.Param, int) bool
	walk = func(params []spec.Param, depth int) bool {
		if depth >= maxBodyFlagDepth {
			return false
		}
		for _, p := range params {
			if isStringCSVArrayParam(p) {
				return true
			}
			if walk(p.Fields, depth+1) {
				return true
			}
		}
		return false
	}
	return walk(endpoint.Body, 0)
}

func hasCSVArrayRequest(apiSpec *spec.APISpec) bool {
	return anyEndpointMatches(apiSpec, endpointUsesCSVArray)
}

func endpointUsesCSVResponse(endpoint spec.Endpoint) bool {
	return endpoint.UsesCSVResponse()
}

func hasCSVResponse(apiSpec *spec.APISpec) bool {
	return anyEndpointMatches(apiSpec, endpointUsesCSVResponse)
}

func endpointUsesBodyJSONFallback(endpoint spec.Endpoint) bool {
	return endpoint.BodyJSONFallback
}

func hasBodyJSONFallback(apiSpec *spec.APISpec) bool {
	return anyEndpointMatches(apiSpec, endpointUsesBodyJSONFallback)
}

func anyEndpointMatches(apiSpec *spec.APISpec, predicate func(spec.Endpoint) bool) bool {
	if apiSpec == nil {
		return false
	}
	var walk func(resources map[string]spec.Resource) bool
	walk = func(resources map[string]spec.Resource) bool {
		for _, resource := range resources {
			for _, endpoint := range resource.Endpoints {
				if predicate(endpoint) {
					return true
				}
			}
			if walk(resource.SubResources) {
				return true
			}
		}
		return false
	}
	return walk(apiSpec.Resources)
}

// findEndpointMatch returns the first endpoint for which predicate is true,
// walking resources and sub-resources depth-first. Resource and endpoint
// names are iterated in sorted order so callers that bake the returned
// endpoint's path into generated output stay deterministic across runs.
func findEndpointMatch(apiSpec *spec.APISpec, predicate func(spec.Endpoint) bool) (spec.Endpoint, bool) {
	if apiSpec == nil {
		return spec.Endpoint{}, false
	}
	var walk func(resources map[string]spec.Resource) (spec.Endpoint, bool)
	walk = func(resources map[string]spec.Resource) (spec.Endpoint, bool) {
		for _, rName := range sortedKeys(resources) {
			resource := resources[rName]
			for _, eName := range sortedKeys(resource.Endpoints) {
				endpoint := resource.Endpoints[eName]
				if predicate(endpoint) {
					return endpoint, true
				}
			}
			if e, ok := walk(resource.SubResources); ok {
				return e, ok
			}
		}
		return spec.Endpoint{}, false
	}
	return walk(apiSpec.Resources)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// formBodyMaps renders per-flag form-field assignments for endpoints that send
// application/x-www-form-urlencoded request bodies. Object/array/JSON-string
// fields are validated as JSON then sent as a single string field (matching
// the JSON-shaped struct_data convention used by reverse-engineered APIs);
// scalar fields are formatted with %v.
func formBodyMaps(body []spec.Param, indent string) string {
	var b strings.Builder
	for _, p := range body {
		id := paramIdent(p)
		ident := toCamel(id)
		flag := publicFlagName(p)
		if isComplexBodyField(p) || isJSONStringParam(p) {
			fmt.Fprintf(&b, "%sif body%s != \"\" {\n", indent, ident)
			fmt.Fprintf(&b, "%s\tif !json.Valid([]byte(body%s)) {\n", indent, ident)
			fmt.Fprintf(&b, "%s\t\treturn fmt.Errorf(\"parsing --%s JSON: invalid JSON\")\n", indent, flag)
			fmt.Fprintf(&b, "%s\t}\n", indent)
			fmt.Fprintf(&b, "%s\tfields.Set(%q, body%s)\n", indent, p.BodyWireName(), ident)
			fmt.Fprintf(&b, "%s}\n", indent)
			continue
		}
		if p.Type == "string" {
			fmt.Fprintf(&b, "%sif body%s != \"\" {\n", indent, ident)
			fmt.Fprintf(&b, "%s\tfields.Set(%q, body%s)\n", indent, p.BodyWireName(), ident)
			fmt.Fprintf(&b, "%s}\n", indent)
			continue
		}
		fmt.Fprintf(&b, "%sif body%s != %s {\n", indent, ident, zeroValForParamRequired(p.Name, p.Type, p.Required, paramHasDefault(p)))
		fmt.Fprintf(&b, "%s\tfields.Set(%q, fmt.Sprintf(\"%%v\", body%s))\n", indent, p.BodyWireName(), ident)
		fmt.Fprintf(&b, "%s}\n", indent)
	}
	return b.String()
}

func isBinaryParam(p spec.Param) bool {
	return strings.EqualFold(strings.TrimSpace(p.Format), "binary")
}

func isComplexBodyField(p spec.Param) bool {
	return p.Type == "object" || p.Type == "array"
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
			FlagName: publicFlagName(other),
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

func isStringCSVArrayParam(p spec.Param) bool {
	return strings.EqualFold(strings.TrimSpace(p.Type), "string_csv_array")
}

func csvArrayValueExpr(p spec.Param, inputExpr string) string {
	switch strings.ToLower(strings.TrimSpace(p.ItemType)) {
	case "object":
		return fmt.Sprintf("cliutil.CSVTemplateObjects(%s, %s)", inputExpr, csvItemTemplateLiteral(p.ItemTemplate))
	default:
		return fmt.Sprintf("cliutil.SplitCSV(%s)", inputExpr)
	}
}

func csvItemTemplateLiteral(v any) string {
	switch val := v.(type) {
	case nil:
		return "nil"
	case string:
		return fmt.Sprintf("%q", val)
	case bool:
		if val {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case float64:
		if val == float64(int(val)) {
			return strconv.Itoa(int(val))
		}
		return fmt.Sprintf("%g", val)
	case []any:
		parts := make([]string, 0, len(val))
		for _, item := range val {
			parts = append(parts, csvItemTemplateLiteral(item))
		}
		return "[]any{" + strings.Join(parts, ", ") + "}"
	case map[string]any:
		keys := make([]string, 0, len(val))
		for key := range val {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%q: %s", key, csvItemTemplateLiteral(val[key])))
		}
		return "map[string]any{" + strings.Join(parts, ", ") + "}"
	case map[any]any:
		converted := make(map[string]any, len(val))
		for key, item := range val {
			converted[fmt.Sprint(key)] = item
		}
		return csvItemTemplateLiteral(converted)
	default:
		return fmt.Sprintf("%q", fmt.Sprint(val))
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

// positionalIndex returns the args[] slot a positional param fills at runtime.
// Cobra populates args from CLI positionals in Positional-only declaration
// order, but Endpoint.Params interleaves query/header params alongside path
// params. The full-Params index drifts from the Positional ordinal as soon as
// a non-positional param sits before a positional one (common when an OpenAPI
// list endpoint declares query params before the path param) and the runtime
// fails with "<name> is required" no matter what positional the user passes.
// Returns -1 if name is not a positional param.
func positionalIndex(e spec.Endpoint, name string) int {
	idx := 0
	for _, p := range e.Params {
		if !p.Positional {
			continue
		}
		if p.Name == name {
			return idx
		}
		idx++
	}
	return -1
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
// build-constraint pattern (*_<GOOS>.go, *_<GOARCH>.go, *_<GOOS>_<GOARCH>.go)
// or Go's test-file pattern (*_test.go). Without this rename a file like
// scheduling_windows.go would get an implicit Windows-only build tag and be
// silently excluded on macOS/Linux builds, and a file like webhook_test.go
// would be treated as a test file and excluded from the normal package build.
//
// The reserved-name collision is handled separately at spec-parse time
// (see ReservedCLIResourceNames) because the function-name collision needs a
// hard error rather than a silent rename — `new<Name>Cmd` would clash with
// the reserved template's identically-named cobra builder.
//
// The suffix "_cmd" is never itself a GOOS, GOARCH, or "test" token, so a
// single application is sufficient.
//
// Examples:
//
//	safeResourceFileStem("scheduling_windows")     -> "scheduling_windows_cmd"
//	safeResourceFileStem("foo_linux_amd64")        -> "foo_linux_amd64_cmd"
//	safeResourceFileStem("webhook_test")           -> "webhook_test_cmd"
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
		if last == "test" {
			// Go treats *_test.go as a test file and excludes it from
			// the normal package build. Suffix "_cmd" keeps the file in
			// the normal build.
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
func composeMCPDesc(api *spec.APISpec, resource spec.Resource, endpoint spec.Endpoint, publicCount, totalCount int) string {
	authType, noAuth := api.EffectiveEndpointAuth(resource, endpoint)
	return mcpdesc.Compose(mcpdesc.Input{
		Endpoint:    endpoint,
		NoAuth:      noAuth,
		AuthType:    authType,
		PublicCount: publicCount,
		TotalCount:  totalCount,
	})
}

func composeMCPSubDesc(api *spec.APISpec, parent spec.Resource, subResource spec.Resource, endpoint spec.Endpoint, publicCount, totalCount int) string {
	authType, noAuth := api.EffectiveSubEndpointAuth(parent, subResource, endpoint)
	return mcpdesc.Compose(mcpdesc.Input{
		Endpoint:    endpoint,
		NoAuth:      noAuth,
		AuthType:    authType,
		PublicCount: publicCount,
		TotalCount:  totalCount,
	})
}

func (g *Generator) mcpParamDescription(p spec.Param) string {
	if g.mcpParamDescriptions == nil {
		g.mcpParamDescriptions = mcpdesc.NewParamDescriptionCompactor(g.Spec)
	}
	return naming.OneLine(g.mcpParamDescriptions.Description(p))
}

func exampleValue(p spec.Param) string {
	if value, ok := syntheticExampleValue(p.Name); ok {
		return value
	}

	// Enum-constrained params: the API rejects anything outside the set,
	// so prefer the first declared value over name-shape heuristics.
	// This wins over name-based branches because a hypothetical
	// `status_id: [a,b,c]` enum still requires `a`, not a UUID.
	for _, v := range p.Enum {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}

	nameLower := strings.ToLower(p.Name)

	// camelCase `*Id` carries an exclusion fence so bool/numeric params
	// ending in "id" (e.g. paid, valid) get their own branches. The fence
	// is expressed as "not numeric/boolean" rather than "is string" so
	// alternative string-shaped types (e.g., `uuid`, `guid`) still match.
	isNumericOrBool := p.Type == "boolean" || p.Type == "bool" ||
		p.Type == "integer" || p.Type == "int" ||
		p.Type == "number" || p.Type == "float"
	if nameLower == "id" ||
		strings.HasSuffix(nameLower, "_id") ||
		(strings.HasSuffix(nameLower, "id") && len(nameLower) > 2 && !isNumericOrBool) {
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
	// Reuse isNumericOrBool (defined above): a numeric- or boolean-typed
	// param must not pick up an RFC3339/date example from a "time"/"date"
	// substring in its name (epoch cursors like start_time, oldest), while
	// Format == date/date-time stays authoritative for genuinely temporal
	// string params.
	if p.Format == "date" || (!isNumericOrBool && strings.Contains(nameLower, "date")) {
		return "2026-01-15"
	}
	if p.Format == "date-time" || (!isNumericOrBool && strings.Contains(nameLower, "time")) {
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
	commandParts := append(strings.Fields(commandPath), toKebab(endpointName))
	if line, ok := g.narrativeExampleLine(commandParts, endpoint); ok {
		return line
	}
	if endpoint.Alias != "" {
		aliasParts := append(strings.Fields(commandPath), endpoint.Alias)
		if line, ok := g.narrativeExampleLine(aliasParts, endpoint); ok {
			return line
		}
	}

	var parts []string
	parts = append(parts, naming.CLI(g.Spec.Name))
	parts = append(parts, commandParts...)
	parts = append(parts, commandExampleArgParts(endpoint)...)

	return "  " + strings.Join(parts, " ")
}

func (g *Generator) promotedExampleLine(promotedName string, endpoint spec.Endpoint) string {
	if line, ok := g.narrativeExampleLine([]string{promotedName}, endpoint); ok {
		return line
	}

	parts := []string{naming.CLI(g.Spec.Name), promotedName}
	parts = append(parts, commandExampleArgParts(endpoint)...)
	return "  " + strings.Join(parts, " ")
}

func (g *Generator) narrativeExampleLine(commandParts []string, endpoint spec.Endpoint) (string, bool) {
	if g == nil || g.Narrative == nil {
		return "", false
	}

	for _, command := range g.narrativeExampleCommands() {
		segments, err := shellargs.SplitChain(command)
		if err != nil {
			continue
		}
		for _, segment := range segments {
			if segment.AfterPipe {
				continue
			}
			if narrativeCommandMatches(g.Spec.Name, segment.Text, commandParts, endpoint) {
				return "  " + strings.TrimSpace(segment.Text), true
			}
		}
	}
	return "", false
}

func (g *Generator) narrativeExampleCommands() []string {
	if g == nil || g.Narrative == nil {
		return nil
	}

	var commands []string
	for _, step := range g.Narrative.QuickStart {
		if command := strings.TrimSpace(step.Command); command != "" {
			commands = append(commands, command)
		}
	}
	for _, recipe := range g.Narrative.Recipes {
		if command := strings.TrimSpace(recipe.Command); command != "" {
			commands = append(commands, command)
		}
	}
	return commands
}

func narrativeCommandMatches(apiName, command string, commandParts []string, endpoint spec.Endpoint) bool {
	tokens, err := shellargs.Split(command)
	if err != nil || len(tokens) < 1+len(commandParts) {
		return false
	}
	if tokens[0] != naming.CLI(apiName) {
		return false
	}
	args := tokens[1:]
	commandIndex, ok := narrativeCommandStart(args)
	if !ok || len(args[commandIndex:]) < len(commandParts) {
		return false
	}
	for i, part := range commandParts {
		if args[commandIndex+i] != part {
			return false
		}
	}
	return narrativeTailMatches(args[commandIndex+len(commandParts):], endpoint)
}

func narrativeTailMatches(args []string, endpoint spec.Endpoint) bool {
	flags := narrativeEndpointFlags()
	for _, p := range endpoint.Params {
		if p.Positional {
			continue
		}
		flags[publicFlagName(p)] = !isBoolParam(p)
	}
	for _, p := range endpoint.Body {
		flags[publicFlagName(p)] = !isBoolParam(p)
	}

	positionals := 0
	expectedPositionals := endpointPositionalCount(endpoint)
	for i := 0; i < len(args); {
		if strings.HasPrefix(args[i], "--") {
			next, ok := skipNarrativeFlag(args, i, flags)
			if !ok {
				return false
			}
			i = next
			continue
		}
		if positionals >= expectedPositionals {
			return false
		}
		positionals++
		i++
	}
	return positionals == expectedPositionals
}

func endpointPositionalCount(endpoint spec.Endpoint) int {
	count := 0
	for _, p := range endpoint.Params {
		if p.Positional {
			count++
		}
	}
	return count
}

func narrativeEndpointFlags() map[string]bool {
	flags := maps.Clone(narrativeGlobalFlags)
	flags["all"] = false
	flags["body-json"] = true
	flags["stdin"] = false
	flags["wait"] = false
	flags["wait-interval"] = true
	flags["wait-timeout"] = true
	return flags
}

func skipNarrativeFlag(args []string, index int, flags map[string]bool) (int, bool) {
	arg := args[index]
	if arg == "--" {
		return 0, false
	}
	name, _, hasInlineValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
	requiresValue, ok := flags[name]
	if !ok {
		return 0, false
	}
	next := index + 1
	if requiresValue && !hasInlineValue {
		if next >= len(args) {
			return 0, false
		}
		next++
	}
	return next, true
}

func isBoolParam(p spec.Param) bool {
	switch p.Type {
	case "boolean", "bool":
		return true
	default:
		return false
	}
}

func narrativeCommandStart(args []string) (int, bool) {
	for i := 0; i < len(args); {
		arg := args[i]
		if arg == "--" {
			return 0, false
		}
		if !strings.HasPrefix(arg, "--") {
			return i, true
		}
		next, ok := skipNarrativeFlag(args, i, narrativeGlobalFlags)
		if !ok {
			return 0, false
		}
		i = next
	}
	return 0, false
}

var narrativeGlobalFlags = map[string]bool{
	"agent":                 false,
	"allow-partial-failure": false,
	"compact":               false,
	"config":                true,
	"csv":                   false,
	"data-source":           true,
	"deliver":               true,
	"dry-run":               false,
	"human-friendly":        false,
	"idempotent":            false,
	"ignore-missing":        false,
	"json":                  false,
	"max-age":               true,
	"no-cache":              false,
	"no-color":              false,
	"no-input":              false,
	"plain":                 false,
	"profile":               true,
	"quiet":                 false,
	"rate-limit":            true,
	"select":                true,
	"throttle-mode":         true,
	"timeout":               true,
	"yes":                   false,
}

func flagName(name string) string {
	return naming.FlagName(name)
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

// cacheDurationDefault is the global stale-after fallback used when the spec
// declares no stale_after, or declares one that does not parse.
const cacheDurationDefault = "6 * time.Hour"

// staleAfterExpr renders the cache stale-after duration as a Go expression for
// direct initialization. A spec literal that parses to a non-negative duration
// becomes e.g. "168 * time.Hour"; an empty, unparseable, or negative value
// falls back to the 6h default (a negative stale-after would make the cache
// permanently stale). Emitting the value directly avoids a dead "staleAfter :=
// 6 * time.Hour" initializer that is always overwritten by a ParseDuration call
// which cannot fail on a constant literal.
func staleAfterExpr(lit string) string {
	if lit == "" {
		return cacheDurationDefault
	}
	d, err := time.ParseDuration(lit)
	if err != nil || d < 0 {
		return cacheDurationDefault
	}
	return goDurationExpr(d)
}

// goDurationExpr renders a time.Duration as a readable Go expression, preferring
// the largest whole unit (hours, then minutes, then seconds) and falling back
// to a nanosecond-typed literal for sub-second or non-round values.
func goDurationExpr(d time.Duration) string {
	switch {
	case d%time.Hour == 0:
		return fmt.Sprintf("%d * time.Hour", d/time.Hour)
	case d%time.Minute == 0:
		return fmt.Sprintf("%d * time.Minute", d/time.Minute)
	case d%time.Second == 0:
		return fmt.Sprintf("%d * time.Second", d/time.Second)
	default:
		return fmt.Sprintf("time.Duration(%d)", int64(d))
	}
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
	"version":        true,
	"help":           true,
	"doctor":         true,
	"auth":           true,
	"sync":           true,
	"search":         true,
	"export":         true,
	"import":         true,
	"completion":     true,
	"refresh-bearer": true,
	"workflow":       true,
	"tail":           true,
	"analytics":      true,
}

// buildPromotedCommands scans spec resources and returns safe top-level shortcuts.
// Single-endpoint resources are promoted. GraphQL resources with a canonical
// get/list pair also promote the get endpoint so users keep a friendly
// `<cli> resource <id>` read path while list and mutation siblings remain
// available as subcommands under the promoted command.
func buildPromotedCommands(s *spec.APISpec) []PromotedCommand {
	var promoted []PromotedCommand
	usedNames := make(map[string]bool)
	graphQL := isGraphQLSpec(s)

	resourceNames := make([]string, 0, len(s.Resources))
	for name := range s.Resources {
		resourceNames = append(resourceNames, name)
	}
	sort.Strings(resourceNames)

	for _, name := range resourceNames {
		resource := s.Resources[name]

		var bestName string
		var bestEndpoint spec.Endpoint
		found := false

		if len(resource.Endpoints) > 1 {
			if graphQL {
				if ep, ok := resource.Endpoints["get"]; ok {
					if _, hasList := resource.Endpoints["list"]; !hasList {
						continue
					}
					bestName = "get"
					bestEndpoint = ep
					found = true
				}
			}
			if !found {
				continue
			}
		} else {
			// Single-endpoint resources promote the only endpoint regardless of method.
			// Without this, POST-only auth resources like `login`/`logout`/`register`
			// render as `<cli> login login --email ...`.
			for _, eName := range sortedEndpointNames(resource.Endpoints) {
				ep := resource.Endpoints[eName]
				bestName = eName
				bestEndpoint = ep
				found = true
			}
		}

		if !found {
			continue
		}
		// A body that recurses past maxBodyFlagDepth must NOT be promoted: the
		// promoted template emits no --stdin fallback, so the truncated subtree
		// would silently drop fields. Skipping promotion keeps the canonical
		// command (which has --stdin) as the reachable surface.
		if bodyExceedsFlagDepth(bestEndpoint) {
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
	if s == nil {
		return false
	}
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

func networkFallbackReason(s *spec.APISpec) string {
	if s == nil {
		return "api_unreachable"
	}
	if s.IsSynthetic() {
		return "synthetic_anchor_fallback"
	}
	u, err := url.Parse(strings.TrimSpace(s.BaseURL))
	// In Printing Press specs, .local base URLs are synthetic placeholders.
	// Real mDNS/private hosts should use a non-.local alias to avoid being
	// classified as synthetic fallback surfaces.
	if err == nil && strings.HasSuffix(strings.ToLower(u.Hostname()), ".local") {
		return "synthetic_anchor_fallback"
	}
	return "api_unreachable"
}

func localReadIsList(supportsAllPagination bool, apiSpec *spec.APISpec, endpointName string, endpoint spec.Endpoint) bool {
	if supportsAllPagination {
		return true
	}
	if endpointHasPathScope(endpoint) {
		return false
	}
	if strings.EqualFold(endpointName, "list") {
		return true
	}
	return networkFallbackReason(apiSpec) == "synthetic_anchor_fallback" && strings.EqualFold(endpoint.Response.Type, "array")
}

func endpointHasPathScope(endpoint spec.Endpoint) bool {
	// Parsed specs and hand-authored fixtures may disagree between the path
	// string and normalized Param flags; either signal means local List would
	// over-return rows across parents.
	if strings.Contains(endpoint.Path, "{") {
		return true
	}
	for _, p := range endpoint.Params {
		if p.PathParam {
			return true
		}
	}
	return false
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

// graphqlListParams returns the GraphQL list arguments this generator knows how
// to render into the query document and command variables map.
func graphqlListParams(endpoint spec.Endpoint) []spec.Param {
	params := make([]spec.Param, 0, len(endpoint.Params))
	for _, p := range endpoint.Params {
		if p.Positional || p.PathParam {
			continue
		}
		switch p.Name {
		case "first", "after", "query":
		default:
			continue
		}
		params = append(params, p)
	}
	return params
}

func graphqlVariableType(p spec.Param) string {
	var typ string
	switch primitiveKind(p.Type) {
	case "int":
		typ = "Int"
	case "float":
		typ = "Float"
	case "bool":
		typ = "Boolean"
	case "array":
		typ = "[String!]"
	default:
		typ = "String"
	}
	if p.Required || strings.EqualFold(p.Name, "first") {
		typ += "!"
	}
	return typ
}

type templateEndpoint struct {
	spec.Endpoint
	EffectivePath string
	EffectiveTier string
}

// lookupEndpointForTemplate resolves a dotted "resource.endpoint" (or
// "resource.sub_resource.endpoint") reference from the spec's resource map.
// Templates use it when rendering intent handler dispatch tables so each
// step's HTTP method and effective path are known at generate time.
func lookupEndpointForTemplate(api *spec.APISpec, ref string) (templateEndpoint, bool) {
	if api == nil {
		return templateEndpoint{}, false
	}
	parts := strings.Split(ref, ".")
	switch len(parts) {
	case 2:
		r, ok := api.Resources[parts[0]]
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
			EffectiveTier: api.EffectiveTier(r, e),
		}, true
	case 3:
		r, ok := api.Resources[parts[0]]
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
		effectiveSub := sub
		if effectiveSub.Tier == "" {
			effectiveSub.Tier = r.Tier
		}
		return templateEndpoint{
			Endpoint:      e,
			EffectivePath: effectiveSubEndpointPath(r, sub, e),
			EffectiveTier: api.EffectiveTier(effectiveSub, e),
		}, true
	default:
		return templateEndpoint{}, false
	}
}

func effectiveEndpointPath(resource spec.Resource, endpoint spec.Endpoint) string {
	return endpointPathWithBase(effectiveEndpointBaseURL(resource, endpoint), endpoint.Path)
}

func effectiveSubEndpointPath(parent spec.Resource, sub spec.Resource, endpoint spec.Endpoint) string {
	return endpointPathWithBase(effectiveSubEndpointBaseURL(parent, sub, endpoint), endpoint.Path)
}

func effectiveEndpointBaseURL(resource spec.Resource, endpoint spec.Endpoint) string {
	baseURL := endpoint.BaseURL
	if baseURL == "" {
		baseURL = resource.BaseURL
	}
	return strings.TrimRight(baseURL, "/")
}

func effectiveSubEndpointBaseURL(parent spec.Resource, sub spec.Resource, endpoint spec.Endpoint) string {
	baseURL := endpoint.BaseURL
	if baseURL == "" {
		baseURL = sub.BaseURL
	}
	if baseURL == "" {
		baseURL = parent.BaseURL
	}
	return strings.TrimRight(baseURL, "/")
}

func endpointPathWithBase(baseURL, path string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" || strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "http://") {
		return path
	}
	return baseURL + path
}
