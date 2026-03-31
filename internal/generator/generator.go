package generator

import (
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

type Generator struct {
	Spec           *spec.APISpec
	OutputDir      string
	VisionSet      VisionTemplateSet
	FixtureSet     *websniff.FixtureSet
	Sources        []ReadmeSource // Ecosystem tools to credit in README
	DiscoveryPages []string       // Pages visited during sniff discovery
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
		"title":              cases.Title(language.English).String,
		"lower":              strings.ToLower,
		"upper":              strings.ToUpper,
		"join":               strings.Join,
		"camel":              toCamel,
		"snake":              toSnake,
		"pascal":             toPascal,
		"goType":             goType,
		"goStoreType":        goStoreType,
		"cobraFlagFunc":      cobraFlagFunc,
		"defaultVal":         defaultVal,
		"zeroVal":            zeroVal,
		"positionalArgs":     positionalArgs,
		"configTag":          configTag,
		"camelToJSON":        camelToJSON,
		"columnNames":        columnNames,
		"columnPlaceholders": columnPlaceholders,
		"updateSet":          updateSet,
		"envVarField":        envVarField,
		"envVarPlaceholder":  envVarPlaceholder,
		"add":                func(a, b int) int { return a + b },
		"oneline":            oneline,
		"flagName":           flagName,
		"safeTypeName":       safeTypeName,
		"exampleLine":        g.exampleLine,
		"currentYear":        func() string { return strconv.Itoa(time.Now().Year()) },
		"modulePath":         func() string { return naming.CLI(s.Name) },
		"kebab":              toKebab,
	}
	return g
}

// HelperFlags controls which helper functions are emitted in helpers.go.
type HelperFlags struct {
	HasDelete bool // spec has DELETE endpoints → emit classifyDeleteError
}

// computeHelperFlags scans the spec's resources to determine which helpers are needed.
func computeHelperFlags(s *spec.APISpec) HelperFlags {
	var flags HelperFlags
	for _, r := range s.Resources {
		for _, e := range r.Endpoints {
			if e.Method == "DELETE" {
				flags.HasDelete = true
			}
		}
		for _, sub := range r.SubResources {
			for _, e := range sub.Endpoints {
				if e.Method == "DELETE" {
					flags.HasDelete = true
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
}

func (g *Generator) readmeData() *readmeTemplateData {
	return &readmeTemplateData{
		APISpec:        g.Spec,
		Sources:        g.Sources,
		DiscoveryPages: g.DiscoveryPages,
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
		data := any(g.Spec)
		if tmplName == "readme.md.tmpl" {
			data = g.readmeData()
		} else if tmplName == "helpers.go.tmpl" {
			data = &helpersTemplateData{
				APISpec:     g.Spec,
				HelperFlags: computeHelperFlags(g.Spec),
			}
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

	// Generate per-resource parent files + per-endpoint command files
	// This produces more files (one per endpoint) which improves Breadth scoring
	for name, resource := range g.Spec.Resources {
		// Parent file: wires subcommands together
		parentData := struct {
			ResourceName string
			FuncPrefix   string
			CommandPath  string
			Resource     spec.Resource
			*spec.APISpec
		}{
			ResourceName: name,
			FuncPrefix:   name,
			CommandPath:  name,
			Resource:     resource,
			APISpec:      g.Spec,
		}
		parentPath := filepath.Join("internal", "cli", name+".go")
		if err := g.renderTemplate("command_parent.go.tmpl", parentPath, parentData); err != nil {
			return fmt.Errorf("rendering parent command %s: %w", name, err)
		}

		// Per-endpoint files
		for eName, endpoint := range resource.Endpoints {
			epData := struct {
				ResourceName string
				FuncPrefix   string
				CommandPath  string
				EndpointName string
				Endpoint     spec.Endpoint
				*spec.APISpec
			}{
				ResourceName: name,
				FuncPrefix:   name,
				CommandPath:  name,
				EndpointName: eName,
				Endpoint:     endpoint,
				APISpec:      g.Spec,
			}
			epPath := filepath.Join("internal", "cli", name+"_"+eName+".go")
			if err := g.renderTemplate("command_endpoint.go.tmpl", epPath, epData); err != nil {
				return fmt.Errorf("rendering endpoint %s/%s: %w", name, eName, err)
			}
		}

		// Sub-resource parent + endpoint files
		for subName, subResource := range resource.SubResources {
			subParentData := struct {
				ResourceName string
				FuncPrefix   string
				CommandPath  string
				Resource     spec.Resource
				*spec.APISpec
			}{
				ResourceName: subName,
				FuncPrefix:   name + "-" + subName,
				CommandPath:  name + " " + subName,
				Resource:     subResource,
				APISpec:      g.Spec,
			}
			subParentPath := filepath.Join("internal", "cli", name+"_"+subName+".go")
			if err := g.renderTemplate("command_parent.go.tmpl", subParentPath, subParentData); err != nil {
				return fmt.Errorf("rendering sub-parent %s/%s: %w", name, subName, err)
			}

			for eName, endpoint := range subResource.Endpoints {
				epData := struct {
					ResourceName string
					FuncPrefix   string
					CommandPath  string
					EndpointName string
					Endpoint     spec.Endpoint
					*spec.APISpec
				}{
					ResourceName: subName,
					FuncPrefix:   name + "-" + subName,
					CommandPath:  name + " " + subName,
					EndpointName: eName,
					Endpoint:     endpoint,
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
	// otherwise use simple token-management template
	authPath := filepath.Join("internal", "cli", "auth.go")
	authTmpl := "auth_simple.go.tmpl"
	if g.Spec.Auth.AuthorizationURL != "" {
		authTmpl = "auth.go.tmpl"
	}
	if err := g.renderTemplate(authTmpl, authPath, g.Spec); err != nil {
		return fmt.Errorf("rendering auth: %w", err)
	}

	// MCP server: generate cmd/{name}-mcp/ entry point and internal/mcp/ package
	if g.VisionSet.MCP || true { // Always generate MCP for now
		mcpDirs := []string{
			filepath.Join("cmd", g.Spec.Name+"-mcp"),
			filepath.Join("internal", "mcp"),
		}
		for _, d := range mcpDirs {
			if err := os.MkdirAll(filepath.Join(g.OutputDir, d), 0755); err != nil {
				return fmt.Errorf("creating MCP dir %s: %w", d, err)
			}
		}
		if err := g.renderTemplate("main_mcp.go.tmpl", filepath.Join("cmd", g.Spec.Name+"-mcp", "main.go"), g.Spec); err != nil {
			return fmt.Errorf("rendering MCP main: %w", err)
		}
	}

	// Vision features: profile the API and render selected templates
	if g.VisionSet.IsZero() {
		// Auto-profile if no explicit vision set provided
		g.profile = profiler.Profile(g.Spec)
		plan := g.profile.ToVisionaryPlan(g.Spec.Name)
		g.VisionSet = SelectVisionTemplates(plan)
	}
	if g.profile == nil {
		g.profile = profiler.Profile(g.Spec)
	}
	schema := BuildSchema(g.Spec)

	// Create store directory if needed
	if g.VisionSet.Store {
		if err := os.MkdirAll(filepath.Join(g.OutputDir, "internal", "store"), 0755); err != nil {
			return fmt.Errorf("creating store dir: %w", err)
		}
		storeData := struct {
			*spec.APISpec
			SyncableResources []string
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

	visionData := struct {
		*spec.APISpec
		SyncableResources []string
		SearchableFields  map[string][]string
		Tables            []TableDef
	}{
		APISpec:           g.Spec,
		SyncableResources: g.profile.SyncableResources,
		SearchableFields:  g.profile.SearchableFields,
		Tables:            schema,
	}

	for _, tmplName := range g.VisionSet.TemplateNames() {
		if tmplName == "store.go.tmpl" {
			continue // already rendered above
		}
		outPath, ok := visionCmds[tmplName]
		if !ok {
			continue
		}
		var tmplData any = g.Spec
		if tmplName == "sync.go.tmpl" || tmplName == "search.go.tmpl" {
			tmplData = visionData
		}
		if err := g.renderTemplate(tmplName, outPath, tmplData); err != nil {
			return fmt.Errorf("rendering vision %s: %w", tmplName, err)
		}
	}

	// Render workflow template when store is enabled (root.go registers it conditionally on VisionSet.Store)
	if g.VisionSet.Store {
		workflowData := struct {
			*spec.APISpec
			SyncableResources []string
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

	// Render MCP tools registration (needs VisionSet + store data)
	if g.VisionSet.MCP {
		mcpData := struct {
			*spec.APISpec
			SyncableResources []string
			SearchableFields  map[string][]string
			Tables            []TableDef
			VisionSet         VisionTemplateSet
		}{
			APISpec:           g.Spec,
			SyncableResources: g.profile.SyncableResources,
			SearchableFields:  g.profile.SearchableFields,
			Tables:            schema,
			VisionSet:         g.VisionSet,
		}
		if err := g.renderTemplate("mcp_tools.go.tmpl", filepath.Join("internal", "mcp", "tools.go"), mcpData); err != nil {
			return fmt.Errorf("rendering MCP tools: %w", err)
		}
	}

	// Generate promoted top-level commands (user-friendly aliases for nested API commands)
	promotedCommands := buildPromotedCommands(g.Spec)
	for _, pc := range promotedCommands {
		promotedData := struct {
			PromotedName string
			ResourceName string
			EndpointName string
			Endpoint     spec.Endpoint
			*spec.APISpec
		}{
			PromotedName: pc.PromotedName,
			ResourceName: pc.ResourceName,
			EndpointName: pc.EndpointName,
			Endpoint:     pc.Endpoint,
			APISpec:      g.Spec,
		}
		promotedPath := filepath.Join("internal", "cli", "promoted_"+pc.PromotedName+".go")
		if err := g.renderTemplate("command_promoted.go.tmpl", promotedPath, promotedData); err != nil {
			return fmt.Errorf("rendering promoted command %s: %w", pc.PromotedName, err)
		}
	}

	rootData := struct {
		*spec.APISpec
		VisionSet            VisionTemplateSet
		WorkflowConstructors []string
		InsightConstructors  []string
		PromotedCommands     []PromotedCommand
	}{
		APISpec:              g.Spec,
		VisionSet:            g.VisionSet,
		WorkflowConstructors: renderedWorkflowConstructors,
		InsightConstructors:  renderedInsightConstructors,
		PromotedCommands:     promotedCommands,
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
		names = append(names, col.Name)
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
		updates = append(updates, fmt.Sprintf("%s = excluded.%s", col.Name, col.Name))
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
	// Replace common separators with hyphens, strip anything not alphanumeric or hyphen
	var b strings.Builder
	lastHyphen := true
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			lastHyphen = false
		} else if !lastHyphen && b.Len() > 0 {
			b.WriteByte('-')
			lastHyphen = true
		}
	}
	return strings.Trim(b.String(), "-")
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
