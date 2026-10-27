package mcpsync

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
		present := map[string]bool{}
		needlePresent := func(needle string) bool {
			if seen, ok := present[needle]; ok {
				return seen
			}
			seen := recordedNeedlePresent(cliDir, rels, needle)
			present[needle] = seen
			return seen
		}
		for _, rel := range rels {
			src, ok := before[rel]
			if !ok {
				continue
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
	fset := token.NewFileSet()
	beforeFile, err := parser.ParseFile(fset, "before.go", beforeSrc, parser.ImportsOnly)
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
	beforeLines := strings.Split(beforeSrc, "\n")
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
		lineNo := fset.Position(imp.Pos()).Line
		if lineNo <= 0 || lineNo > len(beforeLines) {
			continue
		}
		lines = append(lines, beforeLines[lineNo-1])
	}
	return lines
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
	return ensureImport(src, path)
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
