package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

func restorePermanentCreatorForPromote(stagingDir, libraryDir, apiName string) error {
	existing, err := ReadCLIManifest(libraryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading existing library manifest: %w", err)
	}
	if existing.APIName != "" && apiName != "" && existing.APIName != apiName {
		return nil
	}
	if existing.Creator == nil || existing.Creator.IsZero() {
		return nil
	}

	staged, err := ReadCLIManifest(stagingDir)
	if err != nil {
		return fmt.Errorf("reading staged manifest: %w", err)
	}
	if staged.Creator == nil || staged.Creator.IsZero() || spec.SamePerson(*staged.Creator, *existing.Creator) {
		return nil
	}

	priorCreator := staged.Creator.Clean()
	priorContributors := append([]spec.Person(nil), staged.Contributors...)
	restoredCreator := existing.Creator.Clean()
	staged.Creator = &restoredCreator
	staged.Owner = existing.Owner
	if staged.Owner == "" {
		staged.Owner = restoredCreator.Handle
	}
	staged.Printer = existing.Printer
	if staged.Printer == "" {
		staged.Printer = restoredCreator.Handle
	}
	staged.PrinterName = existing.PrinterName
	if staged.PrinterName == "" {
		staged.PrinterName = restoredCreator.Name
	}
	staged.Contributors = spec.PrependContributor(existing.Contributors, priorCreator)

	if err := rewriteGeneratedAttribution(stagingDir, priorCreator, restoredCreator, priorContributors, staged.Contributors); err != nil {
		return err
	}
	if err := WriteCLIManifest(stagingDir, staged); err != nil {
		return err
	}
	return nil
}

func rewriteGeneratedAttribution(dir string, oldCreator, newCreator spec.Person, oldContributors, newContributors []spec.Person) error {
	if err := RewriteOwner(dir, copyrightToken(oldCreator), copyrightToken(newCreator)); err != nil {
		return err
	}
	replacements := map[string]string{
		renderReadmeAttribution(oldCreator, oldContributors):             renderReadmeAttribution(newCreator, newContributors),
		renderNoticeAttribution(oldCreator, oldContributors):             renderNoticeAttribution(newCreator, newContributors),
		`author: "` + yamlDoubleQuotedForManifest(oldCreator.Name) + `"`: `author: "` + yamlDoubleQuotedForManifest(newCreator.Name) + `"`,
	}
	oldCopyright := copyrightToken(oldCreator)
	newCopyright := copyrightToken(newCreator)
	for rel := range map[string]struct{}{"README.md": {}, "NOTICE": {}, "SKILL.md": {}, "LICENSE": {}} {
		path := filepath.Join(dir, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("reading %s: %w", rel, err)
		}
		updated := string(data)
		for oldText, newText := range replacements {
			if oldText == "" || oldText == newText {
				continue
			}
			updated = strings.Replace(updated, oldText, newText, 1)
		}
		updated = replaceNoPeriodCopyright(updated, oldCopyright, newCopyright)
		if updated != string(data) {
			if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
				return fmt.Errorf("writing %s: %w", rel, err)
			}
		}
	}
	return nil
}

func copyrightToken(p spec.Person) string {
	p = p.Clean()
	if p.Name != "" {
		return p.Name
	}
	return p.Handle
}

func replaceNoPeriodCopyright(content, oldOwner, newOwner string) string {
	if oldOwner == "" || newOwner == "" || oldOwner == newOwner {
		return content
	}
	escapedNew := strings.ReplaceAll(newOwner, "$", "$$")
	re := regexp.MustCompile(`(?m)^(\s*Copyright\s+\d+\s+)` + regexp.QuoteMeta(oldOwner) + `( and contributors)?$`)
	return re.ReplaceAllString(content, "${1}"+escapedNew+"${2}")
}

func renderReadmeAttribution(creator spec.Person, contributors []spec.Person) string {
	creator = creator.Clean()
	if creator.Handle == "" {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Created by [@%s](https://github.com/%s)", creator.Handle, creator.Handle)
	if creator.Name != "" {
		fmt.Fprintf(&b, " (%s)", creator.Name)
	}
	b.WriteString(".")
	if len(contributors) > 0 {
		b.WriteString("\nContributors: ")
		for i, c := range contributors {
			c = c.Clean()
			if i > 0 {
				b.WriteString(", ")
			}
			if c.Handle != "" {
				fmt.Fprintf(&b, "[@%s](https://github.com/%s)", c.Handle, c.Handle)
				if c.Name != "" {
					fmt.Fprintf(&b, " (%s)", c.Name)
				}
			} else {
				b.WriteString(c.Name)
			}
		}
		b.WriteString(".")
	}
	return b.String()
}

func renderNoticeAttribution(creator spec.Person, contributors []spec.Person) string {
	creator = creator.Clean()
	if creator.Handle == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("Created by ")
	if creator.Name != "" {
		b.WriteString(creator.Name)
		b.WriteString(" ")
	}
	fmt.Fprintf(&b, "(@%s).", creator.Handle)
	if len(contributors) > 0 {
		b.WriteString("\nContributors:")
		for _, c := range contributors {
			c = c.Clean()
			b.WriteString("\n  - ")
			if c.Name != "" {
				b.WriteString(c.Name)
			}
			if c.Handle != "" {
				if c.Name != "" {
					b.WriteString(" ")
				}
				fmt.Fprintf(&b, "(@%s)", c.Handle)
			}
		}
	}
	return b.String()
}

func yamlDoubleQuotedForManifest(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}
