package generator

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/mvanhorn/cli-printing-press/v3/internal/naming"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// planParentCommand represents a parent command that has subcommands.
type planParentCommand struct {
	Name        string
	Description string
	SubCommands []planSubCommand
}

// planSubCommand represents a subcommand under a parent.
type planSubCommand struct {
	FuncName string // e.g., "authLogin" for pascal-casing into newAuthLoginCmd
	Name     string // leaf name, e.g., "login"
}

// planRootData is the template data for plan_root.go.tmpl.
type planRootData struct {
	CLIName          string
	Description      string
	Version          string
	Owner            string
	TopLevelCommands []PlanCommand
	ParentCommands   []planParentCommand
}

type planGoModData struct {
	Owner     string
	CLIName   string
	VisionSet struct{ Store, MCP bool }
	Config    struct{ Format string }
}

func (planGoModData) UsesBrowserHTTPTransport() bool {
	return false
}

func (planGoModData) HasHTMLExtraction() bool {
	return false
}

// GenerateFromPlan creates a CLI scaffold from a parsed plan spec.
func GenerateFromPlan(planSpec *PlanSpec, outputDir string) error {
	cliName := planSpec.CLIName
	if cliName == "" {
		return fmt.Errorf("plan has no CLI name")
	}

	owner := resolveOwnerForExisting(outputDir)

	// Create directory structure
	dirs := []string{
		filepath.Join("cmd", naming.CLI(cliName)),
		filepath.Join("internal", "cli"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(outputDir, d), 0o755); err != nil {
			return fmt.Errorf("creating dir %s: %w", d, err)
		}
	}

	// Build template FuncMap (subset of the full generator's FuncMap)
	funcs := template.FuncMap{
		"title":       cases.Title(language.English).String,
		"lower":       strings.ToLower,
		"upper":       strings.ToUpper,
		"pascal":      toPascal,
		"camel":       toCamel,
		"snake":       naming.Snake,
		"kebab":       toKebab,
		"currentYear": func() string { return strconv.Itoa(time.Now().Year()) },
		"modulePath":  func() string { return naming.CLI(cliName) },
	}

	render := func(tmplName, outPath string, data any) error {
		content, err := templateFS.ReadFile(filepath.Join("templates", tmplName))
		if err != nil {
			return fmt.Errorf("reading template %s: %w", tmplName, err)
		}
		tmpl, err := template.New(tmplName).Funcs(funcs).Parse(string(content))
		if err != nil {
			return fmt.Errorf("parsing template %s: %w", tmplName, err)
		}
		fullPath := filepath.Join(outputDir, outPath)
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

	// Partition commands into top-level and subcommands
	topLevel, parents := partitionCommands(planSpec.Commands)

	// Render main.go
	mainData := struct {
		Owner string
	}{Owner: owner}
	if err := render("main.go.tmpl", filepath.Join("cmd", naming.CLI(cliName), "main.go"), mainData); err != nil {
		return fmt.Errorf("rendering main.go: %w", err)
	}

	// Render root.go
	rootData := planRootData{
		CLIName:          cliName,
		Description:      planSpec.Description,
		Version:          "0.1.0",
		Owner:            owner,
		TopLevelCommands: topLevel,
		ParentCommands:   parents,
	}
	if err := render("plan_root.go.tmpl", filepath.Join("internal", "cli", "root.go"), rootData); err != nil {
		return fmt.Errorf("rendering root.go: %w", err)
	}

	// Render helpers.go
	helpersData := struct{ Owner string }{Owner: owner}
	if err := render("plan_helpers.go.tmpl", filepath.Join("internal", "cli", "helpers.go"), helpersData); err != nil {
		return fmt.Errorf("rendering helpers.go: %w", err)
	}

	// Render doctor.go
	doctorData := struct {
		CLIName string
		Owner   string
	}{CLIName: cliName, Owner: owner}
	if err := render("plan_doctor.go.tmpl", filepath.Join("internal", "cli", "doctor.go"), doctorData); err != nil {
		return fmt.Errorf("rendering doctor.go: %w", err)
	}

	// Render go.mod (reuse existing template with minimal data)
	goModData := planGoModData{
		Owner:   owner,
		CLIName: cliName,
		Config:  struct{ Format string }{Format: ""},
	}
	if err := render("go.mod.tmpl", "go.mod", goModData); err != nil {
		return fmt.Errorf("rendering go.mod: %w", err)
	}

	// Render golangci.yml
	if err := render("golangci.yml.tmpl", ".golangci.yml", nil); err != nil {
		return fmt.Errorf("rendering .golangci.yml: %w", err)
	}

	// Render stub command files for top-level commands
	for _, cmd := range topLevel {
		cmdData := struct {
			CommandName string
			Description string
			Owner       string
		}{
			CommandName: cmd.Leaf(),
			Description: cmd.Description,
			Owner:       owner,
		}
		outPath := filepath.Join("internal", "cli", cmd.Leaf()+".go")
		if err := render("plan_command.go.tmpl", outPath, cmdData); err != nil {
			return fmt.Errorf("rendering command %s: %w", cmd.Name, err)
		}
	}

	// Render parent commands and their subcommand stubs
	for _, parent := range parents {
		parentData := struct {
			Name        string
			Description string
			SubCommands []planSubCommand
			Owner       string
		}{
			Name:        parent.Name,
			Description: parent.Description,
			SubCommands: parent.SubCommands,
			Owner:       owner,
		}
		outPath := filepath.Join("internal", "cli", parent.Name+".go")
		if err := render("plan_parent.go.tmpl", outPath, parentData); err != nil {
			return fmt.Errorf("rendering parent command %s: %w", parent.Name, err)
		}

		// Render each subcommand as a stub
		for _, sub := range parent.SubCommands {
			cmdData := struct {
				CommandName string
				Description string
				Owner       string
			}{
				CommandName: sub.FuncName,
				Description: parent.Name + " " + sub.Name,
				Owner:       owner,
			}
			outPath := filepath.Join("internal", "cli", parent.Name+"_"+sub.Name+".go")
			if err := render("plan_command.go.tmpl", outPath, cmdData); err != nil {
				return fmt.Errorf("rendering subcommand %s %s: %w", parent.Name, sub.Name, err)
			}
		}
	}

	// Run go mod tidy to populate go.sum
	tidyCmd := exec.Command("go", "mod", "tidy")
	tidyCmd.Dir = outputDir
	tidyCmd.Stdout = os.Stderr
	tidyCmd.Stderr = os.Stderr
	if err := tidyCmd.Run(); err != nil {
		return fmt.Errorf("running go mod tidy: %w", err)
	}

	return nil
}

// partitionCommands separates plan commands into top-level commands and
// parent commands with subcommands.
func partitionCommands(commands []PlanCommand) (topLevel []PlanCommand, parents []planParentCommand) {
	// Group subcommands by parent
	parentMap := make(map[string][]PlanCommand)
	parentDescs := make(map[string]string)

	for _, cmd := range commands {
		if parent := cmd.Parent(); parent != "" {
			parentMap[parent] = append(parentMap[parent], cmd)
			if parentDescs[parent] == "" {
				parentDescs[parent] = parent + " commands"
			}
		} else {
			// Check if this command is also a parent of other commands
			topLevel = append(topLevel, cmd)
		}
	}

	// Remove top-level commands that are actually parents
	var filteredTopLevel []PlanCommand
	for _, cmd := range topLevel {
		if _, isParent := parentMap[cmd.Leaf()]; isParent {
			// Use this command's description as the parent description
			parentDescs[cmd.Leaf()] = cmd.Description
		} else {
			filteredTopLevel = append(filteredTopLevel, cmd)
		}
	}

	// Build sorted parent list
	var parentNames []string
	for name := range parentMap {
		parentNames = append(parentNames, name)
	}
	sort.Strings(parentNames)

	for _, name := range parentNames {
		subs := parentMap[name]
		var subCommands []planSubCommand
		for _, sub := range subs {
			leaf := sub.Leaf()
			subCommands = append(subCommands, planSubCommand{
				FuncName: toPascal(name) + toPascal(leaf),
				Name:     leaf,
			})
		}
		parents = append(parents, planParentCommand{
			Name:        name,
			Description: parentDescs[name],
			SubCommands: subCommands,
		})
	}

	return filteredTopLevel, parents
}

// resolveOwnerForExisting returns the owner attribution for a regeneration
// against an existing tree at outputDir. Tiered so regens preserve original
// attribution instead of silently flipping to whoever's running the
// generator:
//  1. .printing-press.json's `owner` field, if present and non-empty
//  2. parsed `// Copyright YYYY <owner>` line in internal/cli/root.go
//  3. resolveOwnerForNew() (git config / "USER")
//
// Reads .printing-press.json directly rather than calling
// pipeline.ReadCLIManifestOwner because the pipeline package already
// imports generator — adding the reverse direction would create a cycle.
func resolveOwnerForExisting(outputDir string) string {
	if owner := readManifestOwner(outputDir); owner != "" {
		return owner
	}
	if owner := parseCopyrightOwner(outputDir); owner != "" {
		return owner
	}
	return resolveOwnerForNew()
}

// readManifestOwner returns the `owner` field from
// outputDir/.printing-press.json, or "" if the file is absent, malformed,
// or the field is empty.
func readManifestOwner(outputDir string) string {
	data, err := os.ReadFile(filepath.Join(outputDir, ".printing-press.json"))
	if err != nil {
		return ""
	}
	var m struct {
		Owner string `json:"owner"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	return m.Owner
}

// resolveOwnerForNew returns the owner attribution for a brand-new project
// (no existing tree to read from). Falls through git config in priority
// order: github.user, sanitized user.name, literal "USER".
func resolveOwnerForNew() string {
	if out, err := exec.Command("git", "config", "github.user").Output(); err == nil && len(out) > 0 {
		return strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("git", "config", "user.name").Output(); err == nil && len(out) > 0 {
		return sanitizeOwner(strings.TrimSpace(string(out)))
	}
	return "USER"
}

// copyrightOwnerRe matches a Go source `// Copyright YYYY <owner>.` line.
// The owner capture matches the same characters sanitizeOwner would emit
// (lowercase letters, digits, `-`, `_`) plus uppercase to be lenient on
// hand-edited files.
var copyrightOwnerRe = regexp.MustCompile(`(?m)^//\s*Copyright\s+\d+\s+([A-Za-z0-9_-]+)\.`)

// parseCopyrightOwner reads outputDir/internal/cli/root.go (the generator's
// canonical copyright site) and returns the owner string from the
// "// Copyright YYYY <owner>." header. Returns "" on any failure.
func parseCopyrightOwner(outputDir string) string {
	data, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	if err != nil {
		return ""
	}
	if m := copyrightOwnerRe.FindSubmatch(data); m != nil {
		return string(m[1])
	}
	return ""
}

// sanitizeOwner cleans up an owner string for use in Go module paths.
func sanitizeOwner(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return -1
	}, s)
}
