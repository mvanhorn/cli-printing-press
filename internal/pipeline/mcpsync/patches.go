package mcpsync

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
)

var patchImportBlockOpen = regexp.MustCompile(`import \(\n`)

// regeneratedMCPToolsFile is rewritten from the spec on every sync. Recorded
// seams in that file are not spliced back over the generator output; patch
// validation fails the sync instead.
const regeneratedMCPToolsFile = "internal/mcp/tools.go"

const recordedSeamAnchorWindow = 15

type seamLine struct {
	patchID string
	rel     string
	line    string
	index   int
	needle  string
}

// snapshotRecordedFiles reads the pre-sync bytes of every safe path listed
// in a patch record. Missing files are left for validation. A directory or
// unreadable path is an error so sync stops before it rewrites the tree.
func snapshotRecordedFiles(cliDir string, records []pipeline.PatchRecord) (map[string][]byte, error) {
	out := map[string][]byte{}
	for _, rec := range records {
		for _, raw := range rec.Files {
			rel, ok := safePatchRel(raw)
			if !ok {
				continue
			}
			if _, seen := out[rel]; seen {
				continue
			}
			path := filepath.Join(cliDir, filepath.FromSlash(rel))
			data, err := os.ReadFile(path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				return nil, fmt.Errorf("reading recorded patch file %s: %w", rel, err)
			}
			out[rel] = data
		}
	}
	return out, nil
}

// preserveRecordedPatchSeams copies recorded marker and call-site lines from
// the pre-sync snapshot back into files the sync regenerated. It rewrites a
// file only when a recorded line is missing. No records means no writes.
// tools.go is left to the generator; a dropped seam there fails validation.
func preserveRecordedPatchSeams(cliDir string, records []pipeline.PatchRecord, before map[string][]byte) error {
	if len(records) == 0 {
		return nil
	}
	seams := collectRecordedSeams(cliDir, records, before)
	slices.SortFunc(seams, func(a, b seamLine) int {
		if cmp := strings.Compare(a.rel, b.rel); cmp != 0 {
			return cmp
		}
		return a.index - b.index
	})
	var failures []string
	for _, seam := range seams {
		if err := reinsertRecordedLine(cliDir, before[seam.rel], seam); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	return nil
}

func collectRecordedSeams(cliDir string, records []pipeline.PatchRecord, before map[string][]byte) []seamLine {
	var seams []seamLine
	seenLine := map[string]bool{}
	for _, rec := range records {
		id := rec.ID
		if id == "" {
			id = rec.Source
		}
		needles := patchNeedles(rec)
		var rels []string
		for _, raw := range rec.Files {
			rel, ok := safePatchRel(raw)
			if !ok || rel == regeneratedMCPToolsFile {
				continue
			}
			rels = append(rels, rel)
		}
		for _, rel := range rels {
			src, ok := before[rel]
			if !ok {
				continue
			}
			// A needle that survived in another recorded file is not
			// evidence this file still has its own line.
			present := map[string]bool{}
			needlePresent := func(needle string) bool {
				if seen, ok := present[needle]; ok {
					return seen
				}
				seen := recordedNeedlePresent(cliDir, []string{rel}, needle)
				present[needle] = seen
				return seen
			}
			for _, hit := range linesCarryingNeedles(string(src), needles, needlePresent) {
				key := rel + "\x00" + strconv.Itoa(hit.index)
				if seenLine[key] {
					continue
				}
				seenLine[key] = true
				seams = append(seams, seamLine{
					patchID: id,
					rel:     rel,
					line:    hit.line,
					index:   hit.index,
					needle:  hit.needle,
				})
			}
		}
	}
	return seams
}

type needleLine struct {
	index  int
	line   string
	needle string
}

func linesCarryingNeedles(src string, needles []string, present func(string) bool) []needleLine {
	fileLines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	var hits []needleLine
	for i, line := range fileLines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		for _, needle := range needles {
			if needle == "" || strings.Contains(needle, "\n") || !strings.Contains(line, needle) || present(needle) {
				continue
			}
			hits = append(hits, needleLine{index: i, line: line, needle: needle})
			break
		}
	}
	return hits
}

func patchNeedles(rec pipeline.PatchRecord) []string {
	var needles []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || slices.Contains(needles, s) {
			return
		}
		needles = append(needles, s)
	}
	add(rec.Marker)
	for _, s := range rec.Markers {
		add(s)
	}
	for _, s := range rec.CallSites {
		add(s)
	}
	return needles
}

func fileHasExactLine(src []byte, line string) bool {
	line = strings.TrimRight(line, "\r")
	return slices.Contains(strings.Split(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n"), line)
}

func recordedNeedlePresent(dir string, rels []string, needle string) bool {
	for _, rel := range rels {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			continue
		}
		if bytes.Contains(data, []byte(needle)) {
			return true
		}
	}
	return false
}

func reinsertRecordedLine(cliDir string, before []byte, seam seamLine) error {
	path := filepath.Join(cliDir, filepath.FromSlash(seam.rel))
	current, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("patch %q: could not preserve recorded line %q in %s: file is missing after sync", seam.patchID, strings.TrimSpace(seam.line), seam.rel)
		}
		return fmt.Errorf("patch %q: reading %s: %w", seam.patchID, seam.rel, err)
	}
	if fileHasExactLine(current, seam.line) {
		return nil
	}
	updated, err := spliceRecordedLine(before, current, seam)
	if err != nil {
		return err
	}
	if bytes.Equal(updated, current) {
		return fmt.Errorf("patch %q: could not preserve recorded line %q in %s", seam.patchID, strings.TrimSpace(seam.line), seam.rel)
	}
	if strings.HasSuffix(seam.rel, ".go") {
		if _, err := parser.ParseFile(token.NewFileSet(), seam.rel, updated, parser.SkipObjectResolution); err != nil {
			return fmt.Errorf("patch %q: could not preserve recorded line %q in %s: %w", seam.patchID, strings.TrimSpace(seam.line), seam.rel, err)
		}
	}
	if err := writeFileAtomic(path, updated); err != nil {
		return fmt.Errorf("patch %q: writing %s: %w", seam.patchID, seam.rel, err)
	}
	return nil
}

func spliceRecordedLine(before, current []byte, seam seamLine) ([]byte, error) {
	beforeNorm := strings.ReplaceAll(string(before), "\r\n", "\n")
	after := strings.ReplaceAll(string(current), "\r\n", "\n")
	beforeLines := strings.Split(beforeNorm, "\n")
	if seam.index < 0 || seam.index >= len(beforeLines) {
		return nil, fmt.Errorf("patch %q: could not preserve recorded line %q in %s", seam.patchID, strings.TrimSpace(seam.line), seam.rel)
	}
	pos, ok := anchorAfterLine(after, beforeLines, seam.index)
	if !ok {
		pos, ok = anchorBeforeLine(after, beforeLines, seam.index)
	}
	if !ok {
		return nil, fmt.Errorf("patch %q: could not preserve recorded line %q in %s", seam.patchID, strings.TrimSpace(seam.line), seam.rel)
	}
	updated := insertSourceLine(after, pos, seam.line)
	for _, importLine := range missingImportLines(beforeNorm, updated, seam.line) {
		updated = addImportLine(updated, importLine)
	}
	updated, err := reconcileImportAliases(beforeNorm, updated, seam.line)
	if err != nil {
		return nil, fmt.Errorf("patch %q: could not preserve recorded line %q in %s: %w", seam.patchID, strings.TrimSpace(seam.line), seam.rel, err)
	}
	if !strings.Contains(updated, seam.needle) {
		return nil, fmt.Errorf("patch %q: could not preserve recorded line %q in %s", seam.patchID, strings.TrimSpace(seam.line), seam.rel)
	}
	return []byte(updated), nil
}

func anchorAfterLine(after string, beforeLines []string, lineIdx int) (int, bool) {
	var window []string
	for i := lineIdx - 1; i >= 0 && len(window) < recordedSeamAnchorWindow; i-- {
		if strings.TrimSpace(beforeLines[i]) == "" && len(window) == 0 {
			continue
		}
		window = append([]string{beforeLines[i]}, window...)
		if pos, ok := uniqueBlockEnd(after, window); ok {
			return pos, true
		}
	}
	return 0, false
}

func anchorBeforeLine(after string, beforeLines []string, lineIdx int) (int, bool) {
	var window []string
	for i := lineIdx + 1; i < len(beforeLines) && len(window) < recordedSeamAnchorWindow; i++ {
		if strings.TrimSpace(beforeLines[i]) == "" && len(window) == 0 {
			continue
		}
		window = append(window, beforeLines[i])
		if pos, ok := uniqueBlockStart(after, window); ok {
			return pos, true
		}
	}
	return 0, false
}

func uniqueBlockEnd(src string, lines []string) (int, bool) {
	idx, block, ok := uniqueWholeBlock(src, lines)
	if !ok {
		return 0, false
	}
	end := idx + len(block)
	if end < len(src) && src[end] == '\n' {
		end++
	}
	return end, true
}

func uniqueBlockStart(src string, lines []string) (int, bool) {
	idx, _, ok := uniqueWholeBlock(src, lines)
	if !ok {
		return 0, false
	}
	return idx, true
}

func uniqueWholeBlock(src string, lines []string) (int, string, bool) {
	block := strings.Join(lines, "\n")
	if strings.TrimSpace(block) == "" || strings.Count(src, block) != 1 {
		return 0, "", false
	}
	idx := strings.Index(src, block)
	if idx > 0 && src[idx-1] != '\n' {
		return 0, "", false
	}
	end := idx + len(block)
	if end < len(src) && src[end] != '\n' {
		return 0, "", false
	}
	return idx, block, true
}

func insertSourceLine(src string, pos int, line string) string {
	line = strings.TrimRight(line, "\r")
	prefix := src[:pos]
	if prefix != "" && !strings.HasSuffix(prefix, "\n") {
		prefix += "\n"
	}
	return prefix + line + "\n" + src[pos:]
}

func missingImportLines(beforeSrc, afterSrc, inserted string) []string {
	beforeFile, err := parser.ParseFile(token.NewFileSet(), "before.go", beforeSrc, parser.ImportsOnly)
	if err != nil {
		return nil
	}
	afterFile, err := parser.ParseFile(token.NewFileSet(), "after.go", afterSrc, parser.ImportsOnly)
	if err != nil {
		return nil
	}
	have := map[string]bool{}
	for _, imp := range afterFile.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err == nil {
			have[path] = true
		}
	}
	var lines []string
	for _, imp := range beforeFile.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || have[path] {
			continue
		}
		name := importLocalName(imp, path)
		if !referencesImport(inserted, name) {
			continue
		}
		// A standalone `import "log"` line is a whole declaration. Only the
		// spec can be pasted into an existing import block.
		lines = append(lines, importSpecText(imp))
	}
	return lines
}

func importSpecText(imp *ast.ImportSpec) string {
	if imp.Name != nil {
		return imp.Name.Name + " " + imp.Path.Value
	}
	return imp.Path.Value
}

// Duplicate import paths are illegal, and the recorded needle names the
// call's local name, so that name replaces the generated one.
func reconcileImportAliases(beforeSrc, afterSrc, inserted string) (string, error) {
	beforeFile, err := parser.ParseFile(token.NewFileSet(), "before.go", beforeSrc, parser.ImportsOnly)
	if err != nil {
		return afterSrc, nil
	}
	updated := afterSrc
	for _, imp := range beforeFile.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		name := importLocalName(imp, path)
		if !referencesImport(inserted, name) {
			continue
		}
		var renameErr error
		updated, renameErr = renameImportIfNeeded(updated, path, name)
		if renameErr != nil {
			return "", renameErr
		}
	}
	return updated, nil
}

func renameImportIfNeeded(src, path, want string) (string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "after.go", src, parser.SkipObjectResolution)
	if err != nil {
		return src, nil
	}
	var target *ast.ImportSpec
	for _, imp := range file.Imports {
		got, err := strconv.Unquote(imp.Path.Value)
		if err != nil || got != path {
			continue
		}
		target = imp
		break
	}
	if target == nil {
		return src, nil
	}
	have := importLocalName(target, path)
	if have == want {
		return src, nil
	}
	if other, ok := conflictingImport(file, want, path); ok {
		return "", fmt.Errorf("import name %q is already used by %q", want, other)
	}
	var edits []sourceEdit
	if target.Name != nil {
		edits = append(edits, sourceEdit{
			start: fset.Position(target.Name.Pos()).Offset,
			end:   fset.Position(target.Name.End()).Offset,
			text:  want,
		})
	} else {
		edits = append(edits, sourceEdit{
			start: fset.Position(target.Path.Pos()).Offset,
			end:   fset.Position(target.Path.Pos()).Offset,
			text:  want + " ",
		})
	}
	// Locals that shadow the import are not renamed. An unresolved qualifier
	// aborts the rename so sync does not keep a file that parses and does not compile.
	selectorEdits, err := renameImportSelectors(fset, file, importRenameUses(fset, file), path, have, want)
	if err != nil {
		return "", err
	}
	edits = append(edits, selectorEdits...)
	return applySourceEdits(src, edits)
}

// Printed CLIs do not import this path, so Check does not treat a source import as a self-import.
const importRenamePkgPath = "mcpsync.local/import-rename"

// Empty packages are enough: rename only needs to know which identifier is the import.
type importRenameImporter map[string]*types.Package

func (m importRenameImporter) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if pkg := m[path]; pkg != nil {
		return pkg, nil
	}
	name := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		name = path[i+1:]
	}
	pkg := types.NewPackage(path, name)
	pkg.MarkComplete()
	m[path] = pkg
	return pkg, nil
}

func importRenameUses(fset *token.FileSet, file *ast.File) map[*ast.Ident]types.Object {
	info := &types.Info{Uses: make(map[*ast.Ident]types.Object)}
	conf := types.Config{
		Importer: importRenameImporter{},
		// A nil Error stops at the first missing export and drops later selectors from Uses.
		Error: func(error) {},
	}
	_, _ = conf.Check(importRenamePkgPath, fset, []*ast.File{file}, info)
	return info.Uses
}

func renameImportSelectors(fset *token.FileSet, file *ast.File, uses map[*ast.Ident]types.Object, path, have, want string) ([]sourceEdit, error) {
	var edits []sourceEdit
	var unresolved error
	ast.Inspect(file, func(n ast.Node) bool {
		if unresolved != nil {
			return false
		}
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Name != have {
			return true
		}
		obj := uses[id]
		if obj == nil {
			unresolved = fmt.Errorf("import %s: %s does not resolve; refusing to rename %q to %q", path, fset.Position(id.Pos()), have, want)
			return false
		}
		pkg, ok := obj.(*types.PkgName)
		if !ok || pkg.Imported().Path() != path {
			return true
		}
		edits = append(edits, sourceEdit{
			start: fset.Position(id.Pos()).Offset,
			end:   fset.Position(id.End()).Offset,
			text:  want,
		})
		return true
	})
	if unresolved != nil {
		return nil, unresolved
	}
	return edits, nil
}

func conflictingImport(file *ast.File, name, path string) (string, bool) {
	for _, imp := range file.Imports {
		got, err := strconv.Unquote(imp.Path.Value)
		if err != nil || got == path {
			continue
		}
		if importLocalName(imp, got) == name {
			return got, true
		}
	}
	return "", false
}

type sourceEdit struct {
	start int
	end   int
	text  string
}

func applySourceEdits(src string, edits []sourceEdit) (string, error) {
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	b := []byte(src)
	limit := len(b)
	for _, edit := range edits {
		if edit.start < 0 || edit.end > limit || edit.start > edit.end {
			return "", errors.New("import edit out of range")
		}
		b = append(b[:edit.start], append([]byte(edit.text), b[edit.end:]...)...)
		limit = edit.start
	}
	return string(b), nil
}

func importLocalName(imp *ast.ImportSpec, path string) string {
	if imp.Name != nil {
		return imp.Name.Name
	}
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func referencesImport(src, name string) bool {
	if name == "" || name == "_" || name == "." {
		return false
	}
	needle := name + "."
	rest := src
	for {
		i := strings.Index(rest, needle)
		if i < 0 {
			return false
		}
		if i == 0 || !isIdentByte(rest[i-1]) {
			return true
		}
		rest = rest[i+len(needle):]
	}
}

func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

func addImportLine(src, importLine string) string {
	trimmed := strings.TrimSpace(importLine)
	path := quotedImportPath(trimmed)
	if path == "" || strings.Contains(src, strconv.Quote(path)) {
		return src
	}
	indented := "\t" + trimmed + "\n"
	if loc := patchImportBlockOpen.FindStringIndex(src); loc != nil {
		return src[:loc[1]] + indented + src[loc[1]:]
	}
	return insertImportSpec(src, trimmed)
}

func insertImportSpec(src, spec string) string {
	stmt := "import " + spec + "\n"
	single := regexp.MustCompile(`(?m)^import .+\n`)
	if loc := single.FindStringIndex(src); loc != nil {
		return src[:loc[1]] + stmt + src[loc[1]:]
	}
	pkg := regexp.MustCompile(`(?m)^package \w+\n`)
	if loc := pkg.FindStringIndex(src); loc != nil {
		return src[:loc[1]] + "\n" + stmt + src[loc[1]:]
	}
	return src
}

func quotedImportPath(line string) string {
	_, rest, ok := strings.Cut(line, `"`)
	if !ok {
		return ""
	}
	path, _, ok := strings.Cut(rest, `"`)
	if !ok {
		return ""
	}
	return path
}

func safePatchRel(raw string) (string, bool) {
	rel := filepath.ToSlash(strings.TrimSpace(raw))
	rel = strings.TrimPrefix(rel, "./")
	if rel == "" || rel == "." || filepath.IsAbs(raw) || strings.HasPrefix(rel, "/") {
		return "", false
	}
	if slices.Contains(strings.Split(rel, "/"), "..") {
		return "", false
	}
	return rel, true
}
