package regenmerge

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/generatedmarker"
)

// declSet maps a canonical decl name to whether it was found. Names follow
// the convention used by extractDecls below.
type declSet map[string]struct{}

func (s declSet) add(name string) {
	s[name] = struct{}{}
}

// minus returns elements of s not present in other.
func (s declSet) minus(other declSet) []string {
	var out []string
	for k := range s {
		if _, ok := other[k]; !ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// extractDecls returns the canonical decl-name set for a Go source file.
// Functions are bare names; methods are "(*RecvType).Method"; types/vars/
// consts are bare names. Type parameters are stripped from the comparison
// key (so `Get` and `Get[T any]` collide as the same name; the human-readable
// reporting layer can keep the full form).
//
// Returns an empty set on any parse error so the caller can decide how to
// surface the error without crashing the whole walk.
func extractDecls(filename string) (declSet, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", filename, err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, data, parser.SkipObjectResolution)
	if err != nil {
		// Don't crash; the caller may want to surface this and continue.
		return nil, fmt.Errorf("parsing %s: %w", filename, err)
	}

	decls := declSet{}
	for _, d := range file.Decls {
		switch decl := d.(type) {
		case *ast.FuncDecl:
			decls.add(canonicalFuncName(decl))
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					decls.add(s.Name.Name)
				case *ast.ValueSpec:
					for _, n := range s.Names {
						decls.add(n.Name)
					}
				}
			}
		}
	}
	return decls, nil
}

// receiverTypeName returns the canonical key for a method receiver:
//
//	*Foo                    → "*Foo"
//	Foo                     → "Foo"
//	*Foo[T]                 → "*Foo"   (type parameters stripped)
//	*pkg.Foo                → "*pkg.Foo"
func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return "*" + receiverTypeName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		// Likely an embedded type from another package; preserve the form.
		if id, ok := t.X.(*ast.Ident); ok {
			return id.Name + "." + t.Sel.Name
		}
		return t.Sel.Name
	case *ast.IndexExpr:
		// Generic with single type parameter.
		return receiverTypeName(t.X)
	case *ast.IndexListExpr:
		// Generic with multiple type parameters.
		return receiverTypeName(t.X)
	}
	return ""
}

// canonicalFuncName returns the qualified name used as the dedup key for
// function/method decls: bare `Name` for top-level funcs, `(*Type).Name`
// or `(Type).Name` for methods.
func canonicalFuncName(fn *ast.FuncDecl) string {
	name := fn.Name.Name
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		if recv := receiverTypeName(fn.Recv.List[0].Type); recv != "" {
			name = "(" + recv + ")." + name
		}
	}
	return name
}

func hasTemplatedMarker(filename string) bool {
	return generatedmarker.HasInFile(filename)
}

// classifyFiles walks both trees, building the combined file-path set and
// emitting one FileClassification per relative path. Decl-set extraction
// for fresh .go files is cached so the cross-file-search pass and the
// per-file comparison share a single parse per fresh file.
func classifyFiles(publishedDir, freshDir string) ([]FileClassification, error) {
	pubFiles, err := walkSourceFiles(publishedDir)
	if err != nil {
		return nil, fmt.Errorf("walking published: %w", err)
	}
	freshFiles, err := walkSourceFiles(freshDir)
	if err != nil {
		return nil, fmt.Errorf("walking fresh: %w", err)
	}
	pubSet := stringSet(pubFiles)
	freshSet := stringSet(freshFiles)

	// Cache fresh decl-sets keyed by relative path. One parse per fresh
	// file across both the global cross-file search and per-file
	// comparison.
	freshDeclCache := map[string]declSet{}
	freshGlobalDecls := declSet{}
	for _, rel := range freshFiles {
		if !strings.HasSuffix(rel, ".go") {
			continue
		}
		decls, derr := extractDecls(filepath.Join(freshDir, rel))
		if derr != nil {
			continue
		}
		freshDeclCache[rel] = decls
		for k := range decls {
			freshGlobalDecls.add(k)
		}
	}

	// Combined sorted set of all paths across both trees.
	allPaths := make(map[string]struct{}, len(pubSet)+len(freshSet))
	for p := range pubSet {
		allPaths[p] = struct{}{}
	}
	for p := range freshSet {
		allPaths[p] = struct{}{}
	}
	sorted := make([]string, 0, len(allPaths))
	for p := range allPaths {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)

	var out []FileClassification
	for _, rel := range sorted {
		fc := FileClassification{Path: rel}
		_, inPub := pubSet[rel]
		_, inFresh := freshSet[rel]
		pubPath := filepath.Join(publishedDir, rel)

		switch {
		case !inPub && inFresh:
			fc.Verdict = VerdictNewTemplateEmission
		case inPub && !inFresh:
			if hasTemplatedMarker(pubPath) {
				fc.Verdict = VerdictPublishedOnlyTemplated
			} else {
				fc.Verdict = VerdictNovel
			}
		case inPub && inFresh:
			// In both. Only .go files participate in decl-set comparison;
			// go.mod / go.sum classify as TEMPLATED-CLEAN here so Apply
			// overwrites with the merged form.
			if !strings.HasSuffix(rel, ".go") {
				fc.Verdict = VerdictTemplatedClean
				out = append(out, fc)
				continue
			}
			pubDecls, perr := extractDecls(pubPath)
			freshDecls, hasFresh := freshDeclCache[rel]
			if perr != nil || !hasFresh {
				// Parse failure on either side: don't overwrite.
				fc.Verdict = VerdictTemplatedWithAdditions
				out = append(out, fc)
				continue
			}
			freshPath := filepath.Join(freshDir, rel)
			pubMarker := hasTemplatedMarker(pubPath)
			freshMarker := hasTemplatedMarker(freshPath)
			fc = decideBothPresent(rel, pubPath, freshPath, pubDecls, freshDecls, pubMarker, freshMarker, freshGlobalDecls)
		}
		out = append(out, fc)
	}
	return out, nil
}

func stringSet(s []string) map[string]struct{} {
	out := make(map[string]struct{}, len(s))
	for _, v := range s {
		out[v] = struct{}{}
	}
	return out
}

// decideBothPresent runs the in-both branch of the classification decision
// tree.
func decideBothPresent(rel, pubPath, freshPath string, pub, fresh declSet, pubMarker, freshMarker bool, freshGlobal declSet) FileClassification {
	fc := FileClassification{Path: rel}

	pubExtras := pub.minus(fresh)
	freshExtras := fresh.minus(pub)
	templated := pubMarker || freshMarker || isStrictSubset(pub, fresh)

	if templated {
		cleanByDeclSet := len(pubExtras) == 0
		if !cleanByDeclSet {
			// Cross-file decl search: are all "extras" in fresh's global
			// decl set? If so, the generator moved them to a different
			// file — decl-set is clean.
			allMoved := true
			for _, name := range pubExtras {
				if _, ok := freshGlobal[name]; !ok {
					allMoved = false
					break
				}
			}
			cleanByDeclSet = allMoved
		}
		if cleanByDeclSet {
			// Decl-set looks clean. One more check: do pub's function
			// bodies call identifiers fresh's same-function bodies don't?
			// Catches in-place body modifications that decl-set comparison
			// misses (e.g., pub adds a call to a hand-written helper
			// inside an existing templated function).
			if drift := detectBodyDrift(pubPath, freshPath); drift != nil {
				fc.Verdict = VerdictTemplatedBodyDrift
				fc.BodyDrift = drift
				return fc
			}
			fc.Verdict = VerdictTemplatedClean
			return fc
		}
		fc.Verdict = VerdictTemplatedWithAdditions
		fc.DeclSetDelta = &DeclSetDelta{
			InPublishedNotFresh: pubExtras,
			InFreshNotPublished: freshExtras,
		}
		return fc
	}

	// Neither marker nor decl-subset: check intersection.
	intersection := false
	for k := range pub {
		if _, ok := fresh[k]; ok {
			intersection = true
			break
		}
	}
	if !intersection {
		fc.Verdict = VerdictNovelCollision
	} else {
		fc.Verdict = VerdictTemplatedWithAdditions
	}
	if len(pubExtras) > 0 || len(freshExtras) > 0 {
		fc.DeclSetDelta = &DeclSetDelta{
			InPublishedNotFresh: pubExtras,
			InFreshNotPublished: freshExtras,
		}
	}
	return fc
}

// isStrictSubset reports whether all keys of small are in big AND big has at
// least one key small doesn't (so empty-on-empty is false; equal-sets is
// false). Equal sets are handled as "subset OR-equals" by the caller via the
// templated branch's pubExtras length check.
func isStrictSubset(small, big declSet) bool {
	if len(small) == 0 && len(big) == 0 {
		return false
	}
	for k := range small {
		if _, ok := big[k]; !ok {
			return false
		}
	}
	return true
}

// walkSourceFiles returns all relative paths under root that classify as
// source files per shouldClassifyFile, with directories filtered by
// shouldWalkDir. Paths are forward-slash normalized.
func walkSourceFiles(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return rerr
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "." {
				return nil
			}
			if !shouldWalkDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldClassifyFile(rel) {
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
