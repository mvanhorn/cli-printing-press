package generator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
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
	Auth      struct{ Type, Subtype string }
	Streaming planStreamingData
}

func (planGoModData) UsesBrowserHTTPTransport() bool {
	return false
}

func (planGoModData) HasHTMLExtraction() bool {
	return false
}

type planStreamingData struct{}

func (planStreamingData) Enabled() bool {
	return false
}

// GenerateFromPlan creates a CLI scaffold from a parsed plan spec.
func GenerateFromPlan(planSpec *PlanSpec, outputDir string) error {
	cliName := planSpec.CLIName
	if cliName == "" {
		return fmt.Errorf("plan has no CLI name")
	}

	owner := resolveOwnerForExisting(outputDir)
	// Creator drives the copyright header (display name + " and contributors");
	// owner stays the slug for module path / Homebrew tap. The plan scaffold has
	// no api_name to lineage-check against, so pass "" (no cross-lineage gate).
	creator := resolveCreatorForExisting(outputDir, "")

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
		"title":           cases.Title(language.English).String,
		"lower":           strings.ToLower,
		"upper":           strings.ToUpper,
		"pascal":          toPascal,
		"camel":           toCamel,
		"snake":           naming.Snake,
		"kebab":           toKebab,
		"currentYear":     func() string { return strconv.Itoa(time.Now().Year()) },
		"copyrightHolder": func() string { return copyrightHolderString(creator, "", owner) },
		"modulePath":      func() string { return naming.CLI(cliName) },
		// Stub: plan-generated scaffolds never declare auth env vars. The full
		// generator's hasNonCookieAuth (which inspects the real spec.AuthConfig)
		// is registered separately on its own FuncMap.
		"hasNonCookieAuth": func(any) bool { return false },
	}

	render := func(tmplName, outPath string, data any) error {
		content, err := templateFS.ReadFile(path.Join("templates", tmplName))
		if err != nil {
			return fmt.Errorf("reading template %s: %w", tmplName, err)
		}
		tmpl, err := template.New(tmplName).Funcs(funcs).Parse(string(content))
		if err != nil {
			return fmt.Errorf("parsing template %s: %w", tmplName, err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return fmt.Errorf("executing template %s: %w", tmplName, err)
		}
		fullPath := filepath.Join(outputDir, outPath)
		return os.WriteFile(fullPath, normalizeRendered(buf.Bytes(), tmplName, outPath), 0o644)
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

	// Pin golang.org/x/net to a patched release when it resolved transitively
	// below the safe version (see ensureSafeXNet).
	if err := ensureSafeXNet(outputDir); err != nil {
		return err
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

// readManifestField returns the trimmed string value at key from
// outputDir/.printing-press.json, or "" when the file is absent,
// malformed, the key is missing, or the value is empty/whitespace.
//
// Reads the manifest directly rather than calling
// pipeline.ReadCLIManifest because the pipeline package already
// imports generator — adding the reverse direction would create a
// cycle.
func readManifestField(outputDir, key string) string {
	data, err := os.ReadFile(filepath.Join(outputDir, ".printing-press.json"))
	if err != nil {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	s, _ := m[key].(string)
	return strings.TrimSpace(s)
}

// readManifestOwner returns the `owner` slug from the manifest.
func readManifestOwner(outputDir string) string {
	return readManifestField(outputDir, "owner")
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

// resolveOwnerNameForExisting returns the human-readable owner display
// name for a regen against an existing tree. Tiered:
//  1. .printing-press.json's `owner_name` field, if present and non-empty
//  2. resolveOwnerNameForNew() (raw `git config user.name`)
//
// Distinct from resolveOwnerForExisting, which returns a slug-shaped string
// for module paths and copyright headers. OwnerName flows into prose
// surfaces (Hermes author:, README byline) and must not be sanitized.
func resolveOwnerNameForExisting(outputDir string) string {
	if name := readManifestOwnerName(outputDir); name != "" {
		return name
	}
	return resolveOwnerNameForNew()
}

// readManifestOwnerName returns the `owner_name` display-name field
// from the manifest.
func readManifestOwnerName(outputDir string) string {
	return readManifestField(outputDir, "owner_name")
}

// resolveOwnerNameForNew returns the raw `git config user.name` for a fresh
// print. Returns "" when the value is unset — the caller is responsible for
// erroring on that case so the empty value never reaches a published
// SKILL.md or README. No sanitization (display-name shape preserved); no
// fallback to "USER" (would publish an obviously-wrong author).
func resolveOwnerNameForNew() string {
	out, err := exec.Command("git", "config", "user.name").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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

// resolvePrinterForExisting preserves the original printer before consulting git config.
func resolvePrinterForExisting(outputDir string) string {
	if p := readManifestPrinter(outputDir); p != "" {
		return p
	}
	return resolvePrinterForNew()
}

// readManifestPrinter returns the `printer` @handle from the manifest.
func readManifestPrinter(outputDir string) string {
	return readManifestField(outputDir, "printer")
}

// resolvePrinterForNew returns the printer @handle for a brand-new print.
// Tries `git config github.user`, then `gh api user --jq .login` so a
// logged-in `gh` covers machines without `git config github.user`. Returns
// "" instead of a sentinel when both are unset.
func resolvePrinterForNew() string {
	if out, err := exec.Command("git", "config", "github.user").Output(); err == nil {
		if v := strings.TrimSpace(string(out)); v != "" {
			return v
		}
	}
	if out, err := exec.Command("gh", "api", "user", "--jq", ".login").Output(); err == nil {
		// jq emits the literal string "null" when .login is absent or JSON null;
		// filter that explicitly so it doesn't survive into the printer field
		// and re-fail publish-validate the way an empty printer would.
		if v := strings.TrimSpace(string(out)); v != "" && v != "null" {
			return v
		}
	}
	return ""
}

// resolvePrinterNameForExisting preserves the printer display name on regen.
func resolvePrinterNameForExisting(outputDir string) string {
	if name := readManifestPrinterName(outputDir); name != "" {
		return name
	}
	return resolvePrinterNameForNew()
}

// readManifestPrinterName returns the manifest printer display-name field.
func readManifestPrinterName(outputDir string) string {
	return readManifestField(outputDir, "printer_name")
}

// resolvePrinterNameForNew returns raw git user.name for a fresh print.
func resolvePrinterNameForNew() string {
	out, err := exec.Command("git", "config", "user.name").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// copyrightCreatorRe matches the current copyright header
// `// Copyright YYYY <display name> and contributors.` and captures the
// display name. It is tried before copyrightOwnerRe (the legacy slug form) so
// a regen against a current-format tree recovers the prose name, while
// pre-transition trees still resolve via the slug. The two patterns are
// mirrored in internal/pipeline/regenmerge/owner.go and must stay aligned.
var copyrightCreatorRe = regexp.MustCompile(`(?m)^//\s*Copyright\s+\d+\s+(.+?) and contributors\.`)

// manifestAttribution captures every attribution field the resolver consults,
// read in a single pass so resolveCreatorForExisting + resolveContributorsForExisting
// open .printing-press.json once each instead of once per field.
type manifestAttribution struct {
	APIName      string        `json:"api_name"`
	Creator      spec.Person   `json:"creator"`
	Contributors []spec.Person `json:"contributors"`
	Printer      string        `json:"printer"`
	PrinterName  string        `json:"printer_name"`
	Owner        string        `json:"owner"`
	OwnerName    string        `json:"owner_name"`
}

// crossLineage reports whether an existing manifest belongs to a different API
// than the one being generated. When true, the existing tree's attribution
// must not be inherited — otherwise generating API B into a directory that
// still holds API A's manifest would silently stamp B with A's creator.
func crossLineage(existingAPIName, wantAPIName string) bool {
	return existingAPIName != "" && wantAPIName != "" && existingAPIName != wantAPIName
}

// readManifestAttribution reads and decodes the attribution fields from
// outputDir/.printing-press.json in one pass, returning the zero value when the
// file is absent or malformed.
func readManifestAttribution(outputDir string) manifestAttribution {
	var a manifestAttribution
	if data, err := os.ReadFile(filepath.Join(outputDir, ".printing-press.json")); err == nil {
		_ = json.Unmarshal(data, &a)
	}
	return a
}

// resolveCreatorForExisting returns the creator for a regen against an existing
// tree, preferring persisted attribution over re-derivation so a regen never
// silently flips the creator to whoever is running the generator:
//  1. manifest `creator` object (backfilling only an empty handle)
//  2. manifest legacy fields (printer/printer_name, then owner/owner_name)
//  3. copyright-header parse (manifest-less legacy trees)
//  4. resolveCreatorForNew() (git config)
func resolveCreatorForExisting(outputDir, apiName string) spec.Person {
	a := readManifestAttribution(outputDir)
	// A manifest (and copyright header) from a different API must not seed this
	// generation's creator; fall straight through to git resolution.
	if crossLineage(a.APIName, apiName) {
		return resolveCreatorForNew()
	}
	switch {
	case !a.Creator.IsZero():
		c := a.Creator
		// A persisted creator with a name but no handle (printed before
		// github.user resolved) would otherwise lock in an unpublishable
		// record: publish-validate rejects an empty handle and a plain regen
		// never re-derives it. Backfill only the missing handle, preferring the
		// same-lineage legacy printer field, then git config / gh. A populated
		// handle is never touched, so manifest-as-authority still holds.
		if c.Handle == "" {
			if a.Printer != "" {
				c.Handle = a.Printer
			} else if h := resolvePrinterForNew(); h != "" {
				c.Handle = h
			}
		}
		return c
	case a.Printer != "":
		return spec.Person{Handle: a.Printer, Name: a.PrinterName}
	case a.Owner != "":
		return spec.Person{Handle: a.Owner, Name: a.OwnerName}
	}
	if c := parseCopyrightCreator(outputDir); !c.IsZero() {
		return c
	}
	return resolveCreatorForNew()
}

// parseCopyrightCreator recovers the creator from a generated tree's copyright
// header when no manifest is present. The current header carries the display
// name (returned as Name); a legacy header carries the slug (returned as
// Handle). Returns the zero Person on any failure.
func parseCopyrightCreator(outputDir string) spec.Person {
	data, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "root.go"))
	if err != nil {
		return spec.Person{}
	}
	if m := copyrightCreatorRe.FindSubmatch(data); m != nil {
		return spec.Person{Name: string(m[1])}
	}
	if m := copyrightOwnerRe.FindSubmatch(data); m != nil {
		return spec.Person{Handle: string(m[1])}
	}
	return spec.Person{}
}

// resolveCreatorForNew resolves a fresh creator from git config: the handle
// mirrors printer resolution (github.user, then `gh api user`), the name is
// the raw git user.name. Empty values are tolerated here and surfaced as a
// soft warning in Generate().
func resolveCreatorForNew() spec.Person {
	return spec.Person{
		Handle: resolvePrinterForNew(),
		Name:   resolvePrinterNameForNew(),
	}
}

// resolveContributorsForExisting returns the persisted contributors from the
// manifest. The resolver never derives or appends contributors — accrual is a
// deliberate contribution-flow action (publish/amend/reprint), and plain regen
// preserves the existing list.
func resolveContributorsForExisting(outputDir, apiName string) []spec.Person {
	a := readManifestAttribution(outputDir)
	if crossLineage(a.APIName, apiName) {
		return nil
	}
	return a.Contributors
}

// copyrightHolderString builds the copyright-header holder: the creator
// display name (falling back to a prose owner name, then the owner slug),
// always followed by " and contributors" so the header is a constant shape
// regardless of contributor count. Shared by the full generator and the
// plan-scaffold func maps.
// yamlDoubleQuoted escapes a string for safe embedding inside a YAML
// double-quoted scalar. Handles the three failure modes we've seen from
// LLM-authored narrative fields: unescaped " (breaks parser), unescaped \
// (swallows next char), and raw newlines (terminates scalar). Leaves single
// quotes alone — valid in double-quoted YAML. Shared by the HTTP and device
// generator func maps so the .goreleaser.yaml homebrew description renders
// identically from both.
func yamlDoubleQuoted(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

func endsSentence(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	switch s[len(s)-1] {
	case '.', '!', '?':
		return true
	default:
		return false
	}
}

func copyrightHolderString(creator spec.Person, ownerName, ownerSlug string) string {
	holder := creator.Name
	if holder == "" {
		holder = ownerName
	}
	if holder == "" {
		holder = ownerSlug
	}
	if holder == "" {
		return "and contributors"
	}
	return holder + " and contributors"
}
