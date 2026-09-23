package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

var lookupPromoteGitAttribution = resolvePromoteGitAttribution

func isMissingPromotePrinter(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed == "" || trimmed == "USER" || trimmed == "user"
}

func resolvePromoteGitAttribution() (handle, name string) {
	handle = firstNonEmptyCommandOutput("git", "config", "github.user")
	name = firstNonEmptyCommandOutput("git", "config", "user.name")
	return handle, name
}

func firstNonEmptyCommandOutput(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	value := strings.TrimSpace(string(out))
	if value == "" || value == "null" {
		return ""
	}
	return value
}

// backfillPromoteManifestAttribution fills printer/printer_name from git when
// they are missing, then derives creator from those fields. An existing
// creator is left untouched — it is permanent across reprints.
func backfillPromoteManifestAttribution(m *CLIManifest) {
	if m == nil {
		return
	}
	if isMissingPromotePrinter(m.Printer) || isMissingPromotePrinter(m.PrinterName) {
		handle, name := lookupPromoteGitAttribution()
		if isMissingPromotePrinter(m.Printer) && handle != "" {
			m.Printer = handle
		}
		if isMissingPromotePrinter(m.PrinterName) && name != "" {
			m.PrinterName = name
		}
	}
	if strings.TrimSpace(m.Owner) == "" && !isMissingPromotePrinter(m.Printer) {
		m.Owner = strings.TrimSpace(m.Printer)
	}
	if m.Creator == nil || m.Creator.IsZero() {
		handle := strings.TrimSpace(m.Printer)
		name := strings.TrimSpace(m.PrinterName)
		if isMissingPromotePrinter(handle) {
			handle = ""
		}
		if isMissingPromotePrinter(name) {
			name = ""
		}
		if handle != "" || name != "" {
			m.Creator = &spec.Person{Handle: handle, Name: name}
		}
	}
}

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
	stagedData, err := os.ReadFile(filepath.Join(stagingDir, CLIManifestFilename))
	if err != nil {
		return fmt.Errorf("reading staged manifest fields: %w", err)
	}
	var stagedRaw map[string]json.RawMessage
	if err := json.Unmarshal(stagedData, &stagedRaw); err != nil {
		return fmt.Errorf("parsing staged manifest fields: %w", err)
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
	if err := writeCLIManifestPreservingRaw(stagingDir, staged, stagedRaw); err != nil {
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
	if line := renderReadmeContributorsLine(contributors); line != "" {
		b.WriteString("\n")
		b.WriteString(line)
	}
	return b.String()
}

func renderReadmeContributorsLine(contributors []spec.Person) string {
	if len(contributors) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Contributors: ")
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
	if block := renderNoticeContributorsBlock(contributors); block != "" {
		b.WriteString("\n")
		b.WriteString(block)
	}
	return b.String()
}

func renderNoticeContributorsBlock(contributors []spec.Person) string {
	if len(contributors) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Contributors:")
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
	return b.String()
}

var noticeContributorItemRE = regexp.MustCompile(`^\s+- `)

// SyncContributorSurfaces rewrites README.md and NOTICE so their contributor
// sections match the manifest. The creator line and SKILL author are left
// alone. A second call with the same manifest is a no-op. Both surfaces are
// planned first and replaced via a sibling temp file. A failed write restores
// every surface this call changed, including the one that failed.
func SyncContributorSurfaces(dir string) (bool, error) {
	manifest, err := ReadCLIManifest(dir)
	if err != nil {
		return false, err
	}
	readme, notice, err := planContributorSurfacePair(dir, manifest.Contributors)
	if err != nil {
		return false, err
	}
	if err := commitContributorFiles(writeFileAtomic, "", nil, nil, false, readme, notice); err != nil {
		return false, err
	}
	return readme.changed || notice.changed, nil
}

// RecordContributor appends p to the manifest contributors and rewrites the
// README and NOTICE contributor sections to match. All three files are
// planned before any write. README and NOTICE are replaced atomically. A
// failure restores the manifest and both surfaces.
func RecordContributor(dir string, p spec.Person, front bool) (added, synced bool, err error) {
	p = p.Clean()
	if p.IsZero() {
		return false, false, nil
	}
	path := filepath.Join(dir, CLIManifestFilename)
	original, err := os.ReadFile(path)
	if err != nil {
		return false, false, fmt.Errorf("reading CLI manifest: %w", err)
	}
	planned, added, manifestChanged, err := planAppendContributor(original, p, front)
	if err != nil {
		return false, false, err
	}
	manifestBytes := original
	if manifestChanged {
		manifestBytes = planned
	}
	var manifest CLIManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return false, false, fmt.Errorf("parsing CLI manifest: %w", err)
	}
	readme, notice, err := planContributorSurfacePair(dir, manifest.Contributors)
	if err != nil {
		return false, false, err
	}
	if err := commitContributorFiles(writeFileAtomic, path, original, planned, manifestChanged, readme, notice); err != nil {
		return false, false, err
	}
	return added, readme.changed || notice.changed, nil
}

type contributorSurfaceWriter func(path string, data []byte, perm os.FileMode) error

type plannedSurface struct {
	path     string
	original []byte
	next     []byte
	mode     os.FileMode
	changed  bool
}

func planContributorSurfacePair(dir string, contributors []spec.Person) (readme, notice plannedSurface, err error) {
	readme, err = planContributorSurface(filepath.Join(dir, "README.md"), contributors, applyReadmeContributors, "README.md")
	if err != nil {
		return plannedSurface{}, plannedSurface{}, err
	}
	notice, err = planContributorSurface(filepath.Join(dir, "NOTICE"), contributors, applyNoticeContributors, "NOTICE")
	if err != nil {
		return plannedSurface{}, plannedSurface{}, err
	}
	return readme, notice, nil
}

func planContributorSurface(path string, contributors []spec.Person, apply func(string, []spec.Person) string, label string) (plannedSurface, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if len(contributors) > 0 {
				return plannedSurface{}, fmt.Errorf("%s is missing; cannot record contributors", label)
			}
			return plannedSurface{}, nil
		}
		return plannedSurface{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return plannedSurface{}, err
	}
	next := apply(string(data), contributors)
	return plannedSurface{
		path:     path,
		original: data,
		next:     []byte(next),
		mode:     info.Mode().Perm(),
		changed:  next != string(data),
	}, nil
}

func commitContributorFiles(write contributorSurfaceWriter, manifestPath string, manifestOriginal, manifestNext []byte, manifestChanged bool, surfaces ...plannedSurface) error {
	if manifestChanged {
		if err := writeFileAtomic(manifestPath, manifestNext, 0o644); err != nil {
			return fmt.Errorf("writing CLI manifest: %w", err)
		}
	}
	var written []plannedSurface
	for _, surface := range surfaces {
		if !surface.changed {
			continue
		}
		if err := write(surface.path, surface.next, surface.mode); err != nil {
			// written holds only successful writes. Restore this surface too:
			// a truncating writer may have replaced it before returning.
			if restoreErr := restoreContributorFiles(write, manifestPath, manifestOriginal, manifestChanged, append(written, surface)); restoreErr != nil {
				return fmt.Errorf("%w (also failed to restore attribution files: %v)", err, restoreErr)
			}
			return err
		}
		written = append(written, surface)
	}
	return nil
}

func restoreContributorFiles(write contributorSurfaceWriter, manifestPath string, manifestOriginal []byte, manifestChanged bool, written []plannedSurface) error {
	var restoreErr error
	for i := len(written) - 1; i >= 0; i-- {
		surface := written[i]
		if err := write(surface.path, surface.original, surface.mode); err != nil && restoreErr == nil {
			restoreErr = fmt.Errorf("restoring %s: %w", filepath.Base(surface.path), err)
		}
	}
	if manifestChanged {
		if err := writeFileAtomic(manifestPath, manifestOriginal, 0o644); err != nil && restoreErr == nil {
			restoreErr = fmt.Errorf("restoring %s: %w", CLIManifestFilename, err)
		}
	}
	return restoreErr
}

func applyReadmeContributors(content string, contributors []spec.Person) string {
	want := renderReadmeContributorsLine(contributors)
	lines, nl, trailing := splitSourceLines([]byte(content))
	idx := -1
	created := -1
	for i, line := range lines {
		trim := strings.TrimSpace(line)
		if idx < 0 && strings.HasPrefix(trim, "Contributors:") {
			idx = i
		}
		if created < 0 && strings.HasPrefix(trim, "Created by ") {
			created = i
		}
	}
	switch {
	case want == "" && idx >= 0:
		lines = append(lines[:idx], lines[idx+1:]...)
	case want != "" && idx >= 0:
		lines[idx] = want
	case want != "" && idx < 0:
		at := len(lines)
		if created >= 0 {
			at = created + 1
		} else {
			for i, line := range lines {
				if strings.HasPrefix(line, "## ") {
					at = i
					break
				}
			}
		}
		lines = insertStrings(lines, at, want)
	}
	return string(joinSourceLines(lines, nl, trailing))
}

func applyNoticeContributors(content string, contributors []spec.Person) string {
	want := renderNoticeContributorsBlock(contributors)
	lines, nl, trailing := splitSourceLines([]byte(content))
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "Contributors:" {
			start = i
			break
		}
	}
	var wantLines []string
	if want != "" {
		wantLines = strings.Split(want, "\n")
	}
	if start < 0 {
		if len(wantLines) == 0 {
			return content
		}
		at := len(lines)
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "Created by ") {
				at = i + 1
				break
			}
		}
		lines = insertStrings(lines, at, wantLines...)
		return string(joinSourceLines(lines, nl, trailing))
	}
	end := start + 1
	for end < len(lines) && noticeContributorItemRE.MatchString(lines[end]) {
		end++
	}
	kept := append([]string{}, lines[:start]...)
	kept = append(kept, wantLines...)
	kept = append(kept, lines[end:]...)
	return string(joinSourceLines(kept, nl, trailing))
}

// ContributorSurfaceIssues reports README and NOTICE contributor text that
// disagrees with the manifest. Missing files are an issue only when the
// manifest lists contributors.
func ContributorSurfaceIssues(dir string, contributors []spec.Person) []string {
	var issues []string
	wantReadme := renderReadmeContributorsLine(contributors)
	readme, err := os.ReadFile(filepath.Join(dir, "README.md"))
	switch {
	case os.IsNotExist(err):
		if len(contributors) > 0 {
			issues = append(issues, "README.md is missing contributor byline")
		}
	case err != nil:
		issues = append(issues, fmt.Sprintf("reading README.md: %v", err))
	default:
		got, found := readmeContributorsLine(string(readme))
		if (wantReadme == "" && found) || (wantReadme != "" && got != wantReadme) {
			issues = append(issues, "README contributor byline does not match manifest contributors")
		}
	}

	wantNotice := renderNoticeContributorsBlock(contributors)
	notice, err := os.ReadFile(filepath.Join(dir, "NOTICE"))
	switch {
	case os.IsNotExist(err):
		if len(contributors) > 0 {
			issues = append(issues, "NOTICE is missing contributor block")
		}
	case err != nil:
		issues = append(issues, fmt.Sprintf("reading NOTICE: %v", err))
	default:
		got, found := noticeContributorsBlock(string(notice))
		if (wantNotice == "" && found) || (wantNotice != "" && got != wantNotice) {
			issues = append(issues, "NOTICE contributor block does not match manifest contributors")
		}
	}
	return issues
}

func readmeContributorsLine(content string) (string, bool) {
	lines, _, _ := splitSourceLines([]byte(content))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "Contributors:") {
			return line, true
		}
	}
	return "", false
}

func noticeContributorsBlock(content string) (string, bool) {
	lines, _, _ := splitSourceLines([]byte(content))
	for i, line := range lines {
		if strings.TrimSpace(line) != "Contributors:" {
			continue
		}
		block := []string{line}
		for _, next := range lines[i+1:] {
			if !noticeContributorItemRE.MatchString(next) {
				break
			}
			block = append(block, next)
		}
		return strings.Join(block, "\n"), true
	}
	return "", false
}

func yamlDoubleQuotedForManifest(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}
