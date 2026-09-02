package regenmerge

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generatedmarker"
)

const generatedCLINameSuffix = "-pp-cli"
const specIdentityPlaceholder = "__pp_spec_identity__"

// MarkerlessFilesWouldDrop is the --force confirmation set: snapshot files
// the merge will not preserve and whose deletion would discard operator-owned
// Go. Generator-marked files, unimplemented novel scaffolds, and spec-derived
// literal drift stay off the list so fusion-guard refresh is not treated as a
// hand-authored delete.
func MarkerlessFilesWouldDrop(snapshotDir, freshDir string, report *MergeReport, opts Options) []string {
	if report == nil {
		return nil
	}
	var dropped []string
	for _, fc := range report.Files {
		if shouldPreserveFromSnapshot(fc, snapshotDir, freshDir, opts) {
			continue
		}
		if !isMarkerlessHandAuthoredSnapshotFile(snapshotDir, freshDir, fc.Path) {
			continue
		}
		snapPath := filepath.Join(snapshotDir, fc.Path)
		freshPath := filepath.Join(freshDir, fc.Path)
		sameAsFresh, err := filesEqual(snapPath, freshPath)
		if err == nil && sameAsFresh {
			continue
		}
		if opts.BaseDir != "" {
			sameAsBase, berr := filesEqual(snapPath, filepath.Join(opts.BaseDir, fc.Path))
			if berr == nil && sameAsBase {
				continue
			}
		}
		if !wouldLoseHandAuthoredSnapshot(snapshotDir, freshDir, fc) {
			continue
		}
		dropped = append(dropped, fc.Path)
	}
	slices.Sort(dropped)
	return dropped
}

func isMarkerlessHandAuthoredSnapshotFile(snapshotDir, freshDir, rel string) bool {
	rel = filepath.ToSlash(rel)
	if !strings.HasSuffix(rel, ".go") {
		return false
	}
	snapPath := filepath.Join(snapshotDir, rel)
	if _, err := os.Stat(snapPath); err != nil {
		return false
	}
	if generatedmarker.HasInFile(snapPath) {
		return false
	}
	return !isUnimplementedNovelCommandScaffold(snapshotDir, freshDir, rel)
}

func wouldLoseHandAuthoredSnapshot(snapshotDir, freshDir string, fc FileClassification) bool {
	if snapshotHasUniqueDecls(fc) {
		return true
	}
	freshPath := filepath.Join(freshDir, fc.Path)
	if generatedmarker.HasInFile(freshPath) {
		return true
	}
	// Fresh can also be markerless (testenv, extras). Same-decl NovelOnly
	// still destroys a hand-authored function body; const/var-only spec
	// drift stays off the list.
	return snapshotHasFunctionBodyChange(filepath.Join(snapshotDir, fc.Path), freshPath)
}

func snapshotHasFunctionBodyChange(snapPath, freshPath string) bool {
	snapDecls := canonicalDeclTexts(snapPath)
	freshDecls := canonicalDeclTexts(freshPath)
	if snapDecls == nil || freshDecls == nil {
		return true
	}
	// Generated tests embed exact {{.Name}} / {{.Name}}-pp-cli in
	// identity helpers. Normalize only those helper returns so a spec
	// rename is not treated as a hand-authored delete. Exact identity
	// literals in other functions stay intact.
	if strings.HasSuffix(filepath.ToSlash(snapPath), "_test.go") {
		if snapName, snapOK := specIdentityName(snapPath); snapOK {
			if freshName, freshOK := specIdentityName(freshPath); freshOK {
				snapDecls = specNormalizedDeclTexts(snapPath, snapName)
				freshDecls = specNormalizedDeclTexts(freshPath, freshName)
			}
		}
	}
	return functionDeclTextsDiffer(snapDecls, freshDecls)
}

func functionDeclTextsDiffer(snapDecls, freshDecls map[string]string) bool {
	for name, snapText := range snapDecls {
		if strings.Contains(name, ":") {
			continue
		}
		freshText, ok := freshDecls[name]
		if !ok || snapText == freshText {
			continue
		}
		return true
	}
	return false
}

func specNormalizedDeclTexts(path, specName string) map[string]string {
	if specName == "" {
		return canonicalDeclTexts(path)
	}
	return declTextsFromFile(path, func(file *ast.File) {
		rewriteSpecIdentityStringLits(file, specName)
	})
}

func rewriteSpecIdentityStringLits(file *ast.File, specName string) {
	ident := specName + generatedCLINameSuffix
	for _, d := range file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || !isIdentityHelperFunc(fn, specName) {
			continue
		}
		lit := fn.Body.List[0].(*ast.ReturnStmt).Results[0].(*ast.BasicLit)
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			continue
		}
		switch s {
		case specName:
			lit.Value = strconv.Quote(specIdentityPlaceholder)
		case ident:
			lit.Value = strconv.Quote(specIdentityPlaceholder + generatedCLINameSuffix)
		}
	}
}

func isIdentityHelperFunc(fn *ast.FuncDecl, specName string) bool {
	if fn == nil || fn.Body == nil || len(fn.Body.List) != 1 {
		return false
	}
	ret, ok := fn.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	lit, ok := ret.Results[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return false
	}
	return s == specName || s == specName+generatedCLINameSuffix
}

func specIdentityName(path string) (string, bool) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return "", false
	}
	names := map[string]struct{}{}
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true
		}
		for _, name := range specIdentityNamesIn(s) {
			if name != "" {
				names[name] = struct{}{}
			}
		}
		return true
	})
	if len(names) != 1 {
		return "", false
	}
	for name := range names {
		return name, true
	}
	return "", false
}

func specIdentityNamesIn(s string) []string {
	var out []string
	for {
		i := strings.Index(s, generatedCLINameSuffix)
		if i < 0 {
			return out
		}
		start := i
		for start > 0 {
			r, size := utf8.DecodeLastRuneInString(s[:start])
			if !isSpecSlugRune(r) {
				break
			}
			start -= size
		}
		if start < i {
			out = append(out, s[start:i])
		}
		s = s[i+len(generatedCLINameSuffix):]
	}
}

func isSpecSlugRune(r rune) bool {
	return r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func snapshotHasUniqueDecls(fc FileClassification) bool {
	if fc.Verdict == VerdictNovelCollision {
		return true
	}
	return fc.DeclSetDelta != nil && len(fc.DeclSetDelta.InPublishedNotFresh) > 0
}

func isUnimplementedNovelCommandScaffold(snapshotDir, freshDir, rel string) bool {
	if isHandAuthoredNovelCommandScaffold(snapshotDir, freshDir, rel) {
		return false
	}
	if !strings.HasPrefix(rel, "internal/cli/") || !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
		return false
	}
	freshData, err := os.ReadFile(filepath.Join(freshDir, rel))
	if err != nil {
		return false
	}
	if hasGeneratedMarkerBytes(freshData) || !bytes.Contains(freshData, []byte(novelCommandScaffoldMarker)) {
		return false
	}
	snapshotData, err := os.ReadFile(filepath.Join(snapshotDir, rel))
	if err != nil {
		return false
	}
	return bytes.Contains(snapshotData, []byte(novelCommandScaffoldTODO))
}
