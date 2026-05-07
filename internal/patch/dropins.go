package patch

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generator"
)

// dropin describes one file the patcher creates from a template.
type dropin struct {
	templateName string // e.g. "profile.go.tmpl"
	path         string // absolute destination path
}

// planDropins returns the list of drop-in files that should be created for a
// CLI, skipping any whose resource-level collision marked them as existing
// (the patcher refuses rather than overwrites — collision handling is done
// before this is called unless Force is set).
func planDropins(dir string, skipCollisions []Collision) []dropin {
	skip := map[string]bool{}
	for _, c := range skipCollisions {
		if c.Kind == "resource" {
			skip[c.Symbol+".go"] = true
		}
	}
	cliDir := filepath.Join(dir, "internal", "cli")
	var out []dropin
	for _, name := range []string{"profile.go.tmpl", "deliver.go.tmpl", "feedback.go.tmpl"} {
		destName := strings.TrimSuffix(name, ".tmpl")
		if skip[destName] {
			continue
		}
		destPath := filepath.Join(cliDir, destName)
		if _, err := os.Stat(destPath); err == nil {
			// File exists; detectCollisions already decided whether this is
			// idempotent (matching PR #218 content) or fatal.
			continue
		}
		out = append(out, dropin{templateName: name, path: destPath})
	}
	return out
}

// renderDropin executes a template with a minimal data payload and writes
// the result. The three drop-in templates use {{.Name}}, {{.Owner}},
// {{envName .Name}}, and {{currentYear}} — no spec or module path needed.
func renderDropin(d dropin, name, owner string) error {
	src, err := fs.ReadFile(generator.TemplateFS, "templates/"+d.templateName)
	if err != nil {
		return fmt.Errorf("reading template %s: %w", d.templateName, err)
	}
	funcs := template.FuncMap{
		"envName":     envName,
		"currentYear": func() string { return fmt.Sprintf("%d", time.Now().Year()) },
		// modulePath is unused by these three templates, but include it as a
		// defensive stub in case a future template revision references it.
		"modulePath": func() string { return name + "-pp-cli" },
	}
	tmpl, err := template.New(d.templateName).Funcs(funcs).Parse(string(src))
	if err != nil {
		return fmt.Errorf("parse %s: %w", d.templateName, err)
	}
	f, err := os.Create(d.path)
	if err != nil {
		return fmt.Errorf("create %s: %w", d.path, err)
	}
	if err := tmpl.Execute(f, struct {
		Name  string
		Owner string
	}{Name: name, Owner: owner}); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// ownerRegexp matches the copyright line the generator writes to every
// printed CLI file, e.g. `// Copyright 2026 trevin-chow. Licensed under ...`
var ownerRegexp = regexp.MustCompile(`//\s*Copyright\s+\d+\s+(.+?)\.\s+Licensed`)

// extractOwner reads an existing generated file from the CLI dir and returns
// the copyright owner from its header, so re-rendered files match the
// original owner rather than the machine's git-config fallback.
func extractOwner(cliDir string) string {
	for _, rel := range []string{"internal/cli/doctor.go", "internal/cli/helpers.go"} {
		data, err := os.ReadFile(filepath.Join(cliDir, rel))
		if err != nil {
			continue
		}
		if m := ownerRegexp.FindSubmatch(data); len(m) == 2 {
			return strings.TrimSpace(string(m[1]))
		}
	}
	return ""
}

// envName matches the generator's convention for deriving environment
// variable prefixes from a CLI name: upper-case, dashes → underscores.
func envName(name string) string {
	return strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
}
