package generator

import (
	"embed"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unicode"

	"github.com/mvanhorn/cli-printing-press/internal/naming"
	"github.com/mvanhorn/cli-printing-press/internal/profiler"
	"github.com/mvanhorn/cli-printing-press/internal/spec"
	"github.com/mvanhorn/cli-printing-press/internal/websniff"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

//go:embed templates
var templateFS embed.FS

// ReadmeSource represents a credited ecosystem tool for the README.
type ReadmeSource struct {
	Name     string
	URL      string
	Language string
	Stars    int
}

// NovelFeature represents a transcendence feature for the README.
type NovelFeature struct {
	Name        string
	Command     string
	Description string
	Rationale   string
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
	Spec           *spec.APISpec
	OutputDir      string
	VisionSet      VisionTemplateSet
	FixtureSet     *websniff.FixtureSet
	Sources        []ReadmeSource // Ecosystem tools to credit in README
	DiscoveryPages []string       // Pages visited during sniff discovery
	NovelFeatures  []NovelFeature // Transcendence features for README
	profile        *profiler.APIProfile
	funcs          template.FuncMap
	templates      map[string]*template.Template
}

func New(s *spec.APISpec, outputDir string) *Generator {
	if s.Owner == "" {
		if out, err := exec.Command("git", "config", "github.user").Output(); err == nil && len(out) > 0 {
			s.Owner = strings.TrimSpace(string(out))
		} else if out, err := exec.Command("git", "config", "user.name").Output(); err == nil && len(out) > 0 {
			s.Owner = strings.TrimSpace(string(out))
		} else {
			s.Owner = "USER"
		}
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
		"snake":                 toSnake,
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
			if isIDParam(name) && t == "int" {
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
		"envVarPlaceholder":      envVarPlaceholder,
		"envVarIsBuiltinField":   envVarIsBuiltinField,
		"envVarBuiltinFieldName": envVarBuiltinFieldName,
		"resolveEnvVarField":     resolveEnvVarField,
		"add":                    func(a, b int) int { return a + b },
		"oneline":                oneline,
		"mcpDescription":         mcpDescription,
		"mcpDescriptionRich":     mcpDescriptionRich,
		"flagName":               flagName,
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
		"exampleLine":       g.exampleLine,
		"currentYear":       func() string { return strconv.Itoa(time.Now().Year()) },
		"modulePath":        func() string { return naming.CLI(s.Name) },
		"graphqlQueryField": graphqlQueryField,
		"graphqlFieldSelection": func(typeName string, types map[string]spec.TypeDef) []string {
			return graphqlFieldSelection(typeName, types)
		},
		"backtick": func() string { return "`" },
		"kebab":    toKebab,
		"humanName": func(s string) string {
			// "steam-web" → "Steam Web", "notion" → "Notion"
			return cases.Title(language.English).String(strings.ReplaceAll(s, "-", " "))
		},
		"envName":  func(s string) string { return strings.ToUpper(strings.ReplaceAll(s, "-", "_")) },
		"safeName": safeSQLName,
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
	}
	return g
}

// HelperFlags controls which helper functions are emitted in helpers.go.
type HelperFlags struct {
	HasDelete          bool // spec has DELETE endpoints → emit classifyDeleteError
	HasPathParams      bool // spec has path parameters → emit replacePathParam
	HasMultiPositional bool // spec has endpoints with 2+ positional params → emit usageErr
	HasDataLayer       bool // CLI has a local store (sync/search) → emit provenance helpers
}

// computeHelperFlags scans the spec's resources to determine which helpers are needed.
func computeHelperFlags(s *spec.APISpec) HelperFlags {
	var flags HelperFlags
	for _, r := range s.Resources {
		for _, e := range r.Endpoints {
			if strings.EqualFold(e.Method, "DELETE") {
				flags.HasDelete = true
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

// readmeTemplateData wraps APISpec with additional fields for README rendering.
type readmeTemplateData struct {
	*spec.APISpec
	Sources        []ReadmeSource
	DiscoveryPages []string
	NovelFeatures  []NovelFeature
}

func (g *Generator) readmeData() *readmeTemplateData {
	// For sniffed APIs without a website URL, derive it from the base URL domain
	if g.Spec.WebsiteURL == "" && g.Spec.SpecSource == "sniffed" && g.Spec.BaseURL != "" {
		if u, err := url.Parse(g.Spec.BaseURL); err == nil && u.Host != "" {
			g.Spec.WebsiteURL = u.Scheme + "://" + u.Host
		}
	}
	return &readmeTemplateData{
		APISpec:        g.Spec,
		Sources:        g.Sources,
		DiscoveryPages: g.DiscoveryPages,
		NovelFeatures:  g.NovelFeatures,
	}
}

// buildDomainContext constructs structured domain knowledge for MCP agents
// from the spec and profiler output. This is front-loaded context that prevents
// agents from wasting tokens discovering what the API is about.
func (g *Generator) buildDomainContext() DomainContext {
	ctx := DomainContext{
		APIName:     g.Spec.Name,
		Description: oneline(g.Spec.Description),
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
				Description: oneline(r.Description),
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

func (g *Generator) Generate() error {
	dirs := []string{
		filepath.Join("cmd", naming.CLI(g.Spec.Name)),
		filepath.Join("internal", "cli"),
		filepath.Join("internal", "cache"),
		filepath.Join("internal", "client"),
		filepath.Join("internal", "config"),
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

	// Generate single files
	singleFiles := map[string]string{
		"main.go.tmpl":      filepath.Join("cmd", naming.CLI(g.Spec.Name), "main.go"),
		"helpers.go.tmpl":   filepath.Join("internal", "cli", "helpers.go"),
		"doctor.go.tmpl":    filepath.Join("internal", "cli", "doctor.go"),
		"config.go.tmpl":    filepath.Join("internal", "config", "config.go"),
		"cache.go.tmpl":     filepath.Join("internal", "cache", "cache.go"),
		"client.go.tmpl":    filepath.Join("internal", "client", "client.go"),
		"types.go.tmpl":     filepath.Join("internal", "types", "types.go"),
		"golangci.yml.tmpl": ".golangci.yml",
		"readme.md.tmpl":    "README.md",
		"LICENSE.tmpl":      "LICENSE",
		"NOTICE.tmpl":       "NOTICE",
	}

	for tmplName, outPath := range singleFiles {
		var data any
		switch tmplName {
		case "readme.md.tmpl":
			data = g.readmeData()
		case "helpers.go.tmpl":
			hFlags := computeHelperFlags(g.Spec)
			hFlags.HasDataLayer = g.VisionSet.Store
			data = &helpersTemplateData{
				APISpec:     g.Spec,
				HelperFlags: hFlags,
			}
		default:
			data = g.Spec
		}
		if err := g.renderTemplate(tmplName, outPath, data); err != nil {
			return fmt.Errorf("rendering %s: %w", tmplName, err)
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

	// Compute promoted commands early — needed to determine Hidden flag on parent commands
	promotedCommands := buildPromotedCommands(g.Spec)
	hasPromoted := len(promotedCommands) > 0

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

	// Generate per-resource parent files + per-endpoint command files
	// This produces more files (one per endpoint) which improves Breadth scoring
	for name, resource := range g.Spec.Resources {
		// Skip parent file for promoted resources — the promoted command replaces it.
		// Sub-resource parents and endpoint files are still needed (wired by the promoted command).
		if !promotedResourceNames[name] {
			// Parent file: wires subcommands together
			// When promoted commands exist, hide resource parents from --help
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
				Hidden:       hasPromoted,
				APISpec:      g.Spec,
			}
			parentPath := filepath.Join("internal", "cli", name+".go")
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
			epData := struct {
				ResourceName string
				FuncPrefix   string
				CommandPath  string
				EndpointName string
				Endpoint     spec.Endpoint
				HasStore     bool
				*spec.APISpec
			}{
				ResourceName: name,
				FuncPrefix:   name,
				CommandPath:  name,
				EndpointName: eName,
				Endpoint:     endpoint,
				HasStore:     g.VisionSet.Store,
				APISpec:      g.Spec,
			}
			epPath := filepath.Join("internal", "cli", name+"_"+eName+".go")
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
					Hidden:       hasPromoted,
					APISpec:      g.Spec,
				}
				subParentPath := filepath.Join("internal", "cli", name+"_"+subName+".go")
				if err := g.renderTemplate("command_parent.go.tmpl", subParentPath, subParentData); err != nil {
					return fmt.Errorf("rendering sub-parent %s/%s: %w", name, subName, err)
				}
			}

			for eName, endpoint := range subResource.Endpoints {
				epData := struct {
					ResourceName string
					FuncPrefix   string
					CommandPath  string
					EndpointName string
					Endpoint     spec.Endpoint
					HasStore     bool
					*spec.APISpec
				}{
					ResourceName: subName,
					FuncPrefix:   name + "-" + subName,
					CommandPath:  name + " " + subName,
					EndpointName: eName,
					Endpoint:     endpoint,
					HasStore:     g.VisionSet.Store,
					APISpec:      g.Spec,
				}
				epPath := filepath.Join("internal", "cli", name+"_"+subName+"_"+eName+".go")
				if err := g.renderTemplate("command_endpoint.go.tmpl", epPath, epData); err != nil {
					return fmt.Errorf("rendering sub-endpoint %s/%s/%s: %w", name, subName, eName, err)
				}
			}
		}
	}

	// Always render auth command - use full OAuth2 template when authorization URL is present,
	// browser cookie template for cookie-auth APIs, otherwise simple token-management template
	authPath := filepath.Join("internal", "cli", "auth.go")
	authTmpl := "auth_simple.go.tmpl"
	if g.Spec.Auth.AuthorizationURL != "" {
		authTmpl = "auth.go.tmpl"
	} else if g.Spec.Auth.Type == "cookie" || g.Spec.Auth.Type == "composed" {
		authTmpl = "auth_browser.go.tmpl"
	}
	if err := g.renderTemplate(authTmpl, authPath, g.Spec); err != nil {
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

	// Vision features: profile already computed in early profiling above
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

	// Create store directory if needed
	if g.VisionSet.Store {
		if err := os.MkdirAll(filepath.Join(g.OutputDir, "internal", "store"), 0755); err != nil {
			return fmt.Errorf("creating store dir: %w", err)
		}
		storeData := struct {
			*spec.APISpec
			SyncableResources []profiler.SyncableResource
			SearchableFields  map[string][]string
			Tables            []TableDef
		}{
			APISpec:           g.Spec,
			SyncableResources: g.profile.SyncableResources,
			SearchableFields:  g.profile.SearchableFields,
			Tables:            schema,
		}
		if err := g.renderTemplate("store.go.tmpl", filepath.Join("internal", "store", "store.go"), storeData); err != nil {
			return fmt.Errorf("rendering store: %w", err)
		}
	}

	// Render vision CLI commands
	visionCmds := map[string]string{
		"export.go.tmpl":    filepath.Join("internal", "cli", "export.go"),
		"import.go.tmpl":    filepath.Join("internal", "cli", "import.go"),
		"search.go.tmpl":    filepath.Join("internal", "cli", "search.go"),
		"sync.go.tmpl":      filepath.Join("internal", "cli", "sync.go"),
		"tail.go.tmpl":      filepath.Join("internal", "cli", "tail.go"),
		"analytics.go.tmpl": filepath.Join("internal", "cli", "analytics.go"),
	}

	// Build GraphQL field path mapping for sync templates
	gqlFieldPaths := map[string]string{}
	for rName, r := range g.Spec.Resources {
		if ep, ok := r.Endpoints["list"]; ok && ep.ResponsePath != "" {
			gqlFieldPaths[rName] = graphqlQueryField(ep.ResponsePath)
		}
	}

	visionData := struct {
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
	}{
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

	// Render data source resolution template when store is enabled
	if g.VisionSet.Store {
		if err := g.renderTemplate("data_source.go.tmpl", filepath.Join("internal", "cli", "data_source.go"), visionData); err != nil {
			return fmt.Errorf("rendering data_source: %w", err)
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
			return fmt.Errorf("rendering workflow: %w", err)
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
	}

	// Generate api discovery command when promoted commands exist (lets users browse hidden interfaces)
	if hasPromoted {
		if err := g.renderTemplate("api_discovery.go.tmpl", filepath.Join("internal", "cli", "api_discovery.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering api discovery: %w", err)
		}
	}

	// Generate promoted top-level commands (user-friendly aliases for nested API commands)
	// promotedCommands was computed earlier (before resource rendering) for Hidden flag
	for _, pc := range promotedCommands {
		// Look up the full resource to pass sibling endpoints/sub-resources
		resource := g.Spec.Resources[pc.ResourceName]
		promotedData := struct {
			PromotedName string
			ResourceName string
			EndpointName string
			Endpoint     spec.Endpoint
			HasStore     bool
			Resource     spec.Resource
			FuncPrefix   string
			*spec.APISpec
		}{
			PromotedName: pc.PromotedName,
			ResourceName: pc.ResourceName,
			EndpointName: pc.EndpointName,
			Endpoint:     pc.Endpoint,
			HasStore:     g.VisionSet.Store,
			Resource:     resource,
			FuncPrefix:   pc.ResourceName,
			APISpec:      g.Spec,
		}
		promotedPath := filepath.Join("internal", "cli", "promoted_"+pc.PromotedName+".go")
		if err := g.renderTemplate("command_promoted.go.tmpl", promotedPath, promotedData); err != nil {
			return fmt.Errorf("rendering promoted command %s: %w", pc.PromotedName, err)
		}
	}

	rootData := struct {
		*spec.APISpec
		VisionSet             VisionTemplateSet
		VisionCmdNames        map[string]bool
		WorkflowConstructors  []string
		InsightConstructors   []string
		PromotedCommands      []PromotedCommand
		PromotedResourceNames map[string]bool
	}{
		APISpec:               g.Spec,
		VisionSet:             g.VisionSet,
		VisionCmdNames:        g.VisionSet.CmdNames(),
		WorkflowConstructors:  renderedWorkflowConstructors,
		InsightConstructors:   renderedInsightConstructors,
		PromotedCommands:      promotedCommands,
		PromotedResourceNames: promotedResourceNames,
	}
	if err := g.renderTemplate("root.go.tmpl", filepath.Join("internal", "cli", "root.go"), rootData); err != nil {
		return fmt.Errorf("rendering root: %w", err)
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
	f, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("creating %s: %w", fullPath, err)
	}
	defer func() { _ = f.Close() }()

	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("executing template %s: %w", tmplName, err)
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

// Template helper functions

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

func toSnake(s string) string {
	var result strings.Builder
	for i, r := range s {
		if unicode.IsUpper(r) && i > 0 {
			result.WriteRune('_')
		}
		result.WriteRune(unicode.ToLower(r))
	}
	return result.String()
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
	return strings.Join(parts, "")
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

func goType(t string) string {
	switch t {
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
	switch t {
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

func cobraFlagFunc(t string) string {
	switch t {
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
// for ID-like parameters to avoid overflow and zero-value confusion.
func goTypeForParam(name, t string) string {
	if isIDParam(name) && t == "int" {
		return "string"
	}
	return goType(t)
}

// cobraFlagFuncForParam returns the cobra flag function, overriding IntVar→StringVar
// for ID-like parameters.
func cobraFlagFuncForParam(name, t string) string {
	if isIDParam(name) && t == "int" {
		return "StringVar"
	}
	return cobraFlagFunc(t)
}

// defaultValForParam returns the default value for a flag parameter,
// overriding int→string for ID-like parameters.
func defaultValForParam(p spec.Param) string {
	if isIDParam(p.Name) && p.Type == "int" {
		if p.Default != nil {
			return fmt.Sprintf("%q", fmt.Sprintf("%v", p.Default))
		}
		return `""`
	}
	return defaultVal(p)
}

func defaultVal(p spec.Param) string {
	if p.Default != nil {
		// Coerce the default value to match the declared param type
		switch p.Type {
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
		}
	}
	return zeroVal(p.Type)
}

func zeroVal(t string) string {
	switch t {
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
	var result string
	for _, p := range parts {
		if len(p) > 0 {
			result += strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return result
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
	placeholder := envVarPlaceholder(envVar)
	_, ok := builtinConfigTags[placeholder]
	return ok
}

// envVarBuiltinFieldName returns the Go field name of the hardcoded Config
// struct field that matches this env var's placeholder, or empty string if none.
func envVarBuiltinFieldName(envVar string) string {
	placeholder := envVarPlaceholder(envVar)
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

func oneline(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, `"`, `'`)
	s = strings.ReplaceAll(s, "\\", "")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	s = strings.TrimSpace(s)
	if len(s) > 120 {
		cut := s[:117]
		if idx := strings.LastIndex(cut, " "); idx > 60 {
			s = cut[:idx] + "..."
		} else {
			s = cut + "..."
		}
	}
	return s
}

// mcpDescription builds an MCP tool description with optional minority-side
// auth annotation. Only annotates when the CLI has a mix of public and
// auth-required tools. The minority side gets annotated:
//   - Public is minority → append "(public)"
//   - Auth-required is minority → append auth-type-specific suffix
//   - All same status or exact tie → no annotation
func mcpDescription(desc string, noAuth bool, authType string, publicCount, totalCount int) string {
	authCount := totalCount - publicCount
	mixed := publicCount > 0 && authCount > 0

	if mixed {
		if noAuth && publicCount < authCount {
			// Public endpoints are the minority — mark them
			desc = desc + " (public)"
		} else if !noAuth && authCount < publicCount {
			// Auth-required endpoints are the minority — mark them
			switch authType {
			case "api_key":
				desc = desc + " (requires API key)"
			case "cookie", "composed":
				desc = desc + " (requires browser login)"
			case "oauth2", "bearer_token":
				desc = desc + " (requires auth)"
			default:
				desc = desc + " (requires auth)"
			}
		}
	}

	return oneline(desc)
}

// mcpDescriptionRich builds an enriched MCP tool description that includes
// the base description plus response shape hints and method context.
// This gives agents enough information to choose the right tool without
// trial-and-error. Total length is capped to prevent token bloat.
func mcpDescriptionRich(desc string, noAuth bool, authType string, publicCount, totalCount int, method, respType, respItem string) string {
	base := mcpDescription(desc, noAuth, authType, publicCount, totalCount)

	var suffix string

	// Add response shape hint
	if respType == "array" && respItem != "" {
		suffix = "Returns array of " + respItem + "."
	} else if respType == "array" {
		suffix = "Returns array."
	} else if respType == "object" && respItem != "" {
		suffix = "Returns " + respItem + "."
	}

	// Add method context for non-obvious cases
	switch method {
	case "DELETE":
		if suffix != "" {
			suffix += " Destructive."
		} else {
			suffix = "Destructive operation."
		}
	case "PATCH":
		if suffix == "" {
			suffix = "Partial update."
		}
	}

	if suffix == "" {
		return base
	}

	result := base + " " + suffix
	// Cap at 200 chars to prevent token bloat (PostHog learned this the hard way)
	if len(result) > 200 {
		result = result[:197] + "..."
	}
	return result
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
	return result
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

// buildPromotedCommands scans spec resources and returns promotable top-level commands.
// For each resource, it finds the "primary" GET endpoint (no path params, or first GET)
// and creates a promoted command with a cleaner name.
func buildPromotedCommands(s *spec.APISpec) []PromotedCommand {
	var promoted []PromotedCommand
	usedNames := make(map[string]bool)

	for name, resource := range s.Resources {
		// Find the primary GET endpoint: prefer GET without positional params, else first GET
		var bestName string
		var bestEndpoint spec.Endpoint
		found := false

		for eName, ep := range resource.Endpoints {
			if ep.Method != "GET" {
				continue
			}
			hasPositional := false
			for _, p := range ep.Params {
				if p.Positional {
					hasPositional = true
					break
				}
			}
			if !found || !hasPositional {
				bestName = eName
				bestEndpoint = ep
				found = true
				if !hasPositional {
					break // Ideal: GET without path params (list endpoint)
				}
			}
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

func envVarPlaceholder(envVar string) string {
	// STYTCH_PROJECT_ID -> project_id (the placeholder in the format string)
	parts := strings.Split(envVar, "_")
	if len(parts) <= 1 {
		return strings.ToLower(envVar)
	}
	// Skip the first part (tool name prefix) and join the rest
	var lower []string
	for _, p := range parts[1:] {
		lower = append(lower, strings.ToLower(p))
	}
	return strings.Join(lower, "_")
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
		fields = append(fields, f.Name)
	}
	if len(fields) == 0 {
		return []string{"id"}
	}
	return fields
}
