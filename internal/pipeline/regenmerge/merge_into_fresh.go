package regenmerge

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
)

// regenmergeGeneratorOwnedDirs lists internal/<name>/ subtrees the generator
// owns end-to-end. Files inside these directories that don't appear in the
// classifier's report (typically non-Go files like text fixtures) are NOT
// swept from snapshot into fresh during MergeIntoFreshTree — fresh's
// emission is authoritative for everything under these roots. Canonical
// source for the regen pipeline; the parallel list in internal/cli/root.go
// (alongside the dead in-memory preserve helpers) tracks the same set and
// will be removed when those helpers are deleted.
var regenmergeGeneratorOwnedDirs = map[string]struct{}{
	"cli":     {},
	"cliutil": {},
	"mcp":     {},
	"cache":   {},
	"client":  {},
	"config":  {},
	"share":   {},
	"store":   {},
	"types":   {},
}

// MergeIntoFreshTree merges hand-edits from a snapshot directory into a
// freshly-emitted CLI tree. Companion to Apply for the regen-from-spec
// workflow: where Apply runs stage-and-swap with the published tree as the
// destination, MergeIntoFreshTree mutates the freshly-generated tree
// in-place using the snapshot as the recovery path.
//
// Steps in order:
//  1. Per-file verdict switch — copy preserve-worthy files from snapshot
//     into fresh; no-op on TEMPLATED-CLEAN / NEW-TEMPLATE-EMISSION;
//     leave PUBLISHED-ONLY-TEMPLATED files alone (fresh didn't emit them).
//  2. Re-inject lost AddCommand calls into fresh-derived host files.
//  3. Merge go.mod requires/replaces from snapshot into fresh's go.mod via
//     renderMergedGoMod (preserves hand-added deps for novel packages).
//  4. Sweep snapshot for non-classified files (README.md, Makefile, etc.)
//     under non-generator-owned directories and copy any that don't exist
//     in fresh.
//
// Symlinks at any preserve path or sweep path are refused — the caller is
// expected to have validated the snapshot/fresh directory shape upstream.
//
// When opts.NovelOnly is true, only NOVEL and NOVEL-COLLISION verdicts are
// preserved; TEMPLATED-WITH-ADDITIONS, TEMPLATED-BODY-DRIFT, and
// TEMPLATED-VALUE-DRIFT files are left as fresh emitted them unless the file
// is a generated editable hook whose whole purpose is to carry agent-authored
// additions. Lost AddCommand re-injection is skipped. The non-classified file
// sweep and go.mod merge still run because both are spec-orthogonal — non-Go
// files and go.mod require additions are valid preservation targets even when
// the fresh spec differs from the snapshot's.
func MergeIntoFreshTree(snapshotDir, freshDir string, report *MergeReport, opts Options) error {
	if report == nil {
		return errors.New("nil report")
	}
	if _, err := os.Stat(snapshotDir); err != nil {
		return fmt.Errorf("snapshot dir %s: %w", snapshotDir, err)
	}
	if _, err := os.Stat(freshDir); err != nil {
		return fmt.Errorf("fresh dir %s: %w", freshDir, err)
	}

	for i := range report.Files {
		fc := &report.Files[i]
		switch fc.Verdict {
		case VerdictTemplatedClean, VerdictNewTemplateEmission, VerdictPublishedOnlyTemplated:
			// fresh's emission is authoritative; nothing to copy from snapshot.
		case VerdictNovel, VerdictNovelCollision:
			if err := copyPreserveFile(snapshotDir, freshDir, fc.Path); err != nil {
				return err
			}
			fc.Applied = true
		case VerdictTemplatedWithAdditions, VerdictTemplatedBodyDrift, VerdictTemplatedValueDrift:
			if opts.NovelOnly && !preserveTemplatedDriftInNovelOnly(fc.Path) {
				continue
			}
			if err := copyPreserveFile(snapshotDir, freshDir, fc.Path); err != nil {
				return err
			}
			fc.Applied = true
		default:
			return fmt.Errorf("unhandled verdict %q for %s", fc.Verdict, fc.Path)
		}
	}

	if !opts.NovelOnly {
		for i := range report.LostRegistrations {
			lr := &report.LostRegistrations[i]
			if len(lr.Calls) == 0 {
				continue
			}
			hostPath := filepath.Join(freshDir, lr.HostFile)
			if err := injectAddCommands(hostPath, lr.Calls, lr.EnclosingFunc); err != nil {
				return fmt.Errorf("re-injecting AddCommand into %s: %w", lr.HostFile, err)
			}
			lr.Applied = true
		}
	}

	if err := pruneFreshDeclCollisions(freshDir, report); err != nil {
		return fmt.Errorf("pruning fresh declaration collisions: %w", err)
	}
	if err := preserveRuntimeVersionLayout(snapshotDir, freshDir, report); err != nil {
		return fmt.Errorf("preserving runtime version layout: %w", err)
	}

	if report.GoMod != nil {
		merged, err := renderMergedGoModWithModulePaths(snapshotDir, freshDir)
		switch {
		case err == nil:
			if writeErr := writeFileAtomic(filepath.Join(freshDir, "go.mod"), merged.Bytes); writeErr != nil {
				return fmt.Errorf("writing merged go.mod: %w", writeErr)
			}
			if merged.PublishedModulePath != "" && merged.FreshModulePath != "" && merged.PublishedModulePath != merged.FreshModulePath {
				if err := pipeline.RewriteModulePathReferences(freshDir, merged.FreshModulePath, merged.PublishedModulePath); err != nil {
					return fmt.Errorf("rewriting module path references: %w", err)
				}
			}
			report.GoMod.Merged = true
		case errors.Is(err, fs.ErrNotExist):
			// Either tree lacks a go.mod; leave fresh's emission alone.
		default:
			return fmt.Errorf("rendering merged go.mod: %w", err)
		}
	}

	if err := sweepNonClassifiedFiles(snapshotDir, freshDir); err != nil {
		return fmt.Errorf("sweeping non-classified snapshot files: %w", err)
	}

	report.Applied = true
	return nil
}

// preserveRuntimeVersionLayout keeps release-ledger-owned version surfaces
// stable across template splits. Older published CLIs stored the CLI version
// variable in internal/cli/root.go; newer templates split it into
// internal/cli/version.go. Reprints must keep the published layout and value
// so the library's release-ledger guard does not see a runtime-version change
// in the publish PR.
func preserveRuntimeVersionLayout(snapshotDir, freshDir string, report *MergeReport) error {
	if report == nil {
		return nil
	}
	if err := preserveLegacyCLIRootVersionLayout(snapshotDir, freshDir, report); err != nil {
		return err
	}
	return preserveMCPMainVersionValues(snapshotDir, freshDir, report)
}

func preserveLegacyCLIRootVersionLayout(snapshotDir, freshDir string, report *MergeReport) error {
	rootRel := filepath.ToSlash(filepath.Join("internal", "cli", "root.go"))
	versionRel := filepath.ToSlash(filepath.Join("internal", "cli", "version.go"))
	if fileVerdict(report, versionRel) != VerdictNewTemplateEmission {
		return nil
	}

	snapshotRoot := filepath.Join(snapshotDir, rootRel)
	freshRoot := filepath.Join(freshDir, rootRel)
	freshVersion := filepath.Join(freshDir, versionRel)
	versionLiteral, ok, err := readStringVarLiteral(snapshotRoot, "version")
	if err != nil || !ok {
		return err
	}
	snapshotRootDecls, err := extractDecls(snapshotRoot)
	if err != nil {
		return err
	}
	if _, hasCurrentVersionCmd := snapshotRootDecls["newVersionCmd"]; !hasCurrentVersionCmd {
		if _, hasLegacyVersionCmd := snapshotRootDecls["newVersionCliCmd"]; !hasLegacyVersionCmd {
			return nil
		}
	}
	if _, err := os.Stat(freshVersion); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}

	rootDecls, err := extractDecls(freshRoot)
	if err != nil {
		return err
	}
	if _, hasVersion := rootDecls["version"]; hasVersion {
		if err := replaceStringVarLiteral(freshRoot, "version", versionLiteral); err != nil {
			return err
		}
	} else if err := insertStringVarAfterImports(freshRoot, "version", versionLiteral); err != nil {
		return err
	}

	rootDecls, err = extractDecls(freshRoot)
	if err != nil {
		return err
	}
	if _, hasVersionCmd := rootDecls["newVersionCmd"]; !hasVersionCmd {
		fn, err := extractFuncSource(freshVersion, "newVersionCmd")
		if err != nil {
			return err
		}
		if fn != "" {
			if err := appendGoSource(freshRoot, fn); err != nil {
				return err
			}
		}
	}
	if err := os.Remove(freshVersion); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func preserveMCPMainVersionValues(snapshotDir, freshDir string, report *MergeReport) error {
	for _, fc := range report.Files {
		rel := filepath.ToSlash(fc.Path)
		if !strings.HasPrefix(rel, "cmd/") || !strings.HasSuffix(rel, "-pp-mcp/main.go") {
			continue
		}
		literal, ok, err := readStringVarLiteral(filepath.Join(snapshotDir, rel), "version")
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		freshPath := filepath.Join(freshDir, rel)
		if _, err := os.Stat(freshPath); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return err
		}
		if err := replaceStringVarLiteral(freshPath, "version", literal); err != nil {
			return err
		}
	}
	return nil
}

func fileVerdict(report *MergeReport, rel string) Verdict {
	for _, fc := range report.Files {
		if filepath.ToSlash(fc.Path) == rel {
			return fc.Verdict
		}
	}
	return ""
}

func readStringVarLiteral(path, name string) (string, bool, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("parsing %s: %w", path, err)
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, ident := range valueSpec.Names {
				if ident.Name != name || i >= len(valueSpec.Values) {
					continue
				}
				lit, ok := valueSpec.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return "", false, nil
				}
				return lit.Value, true, nil
			}
		}
	}
	return "", false, nil
}

func replaceStringVarLiteral(path, name, literal string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, data, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, ident := range valueSpec.Names {
				if ident.Name != name || i >= len(valueSpec.Values) {
					continue
				}
				lit, ok := valueSpec.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return fmt.Errorf("%s var %s is not a string literal", path, name)
				}
				start := fset.Position(lit.Pos()).Offset
				end := fset.Position(lit.End()).Offset
				next := append([]byte{}, data[:start]...)
				next = append(next, literal...)
				next = append(next, data[end:]...)
				return writeFileAtomic(path, next)
			}
		}
	}
	return fmt.Errorf("%s missing var %s", path, name)
}

func insertStringVarAfterImports(path, name, literal string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, data, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	insertAt := -1
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if ok && gen.Tok == token.IMPORT {
			insertAt = fset.Position(gen.End()).Offset
		}
	}
	if insertAt < 0 {
		insertAt = fset.Position(file.Name.End()).Offset
	}
	block := []byte("\n\n// " + name + " is the printed CLI's version, overridable at build time via ldflags.\nvar " + name + " = " + literal + "\n")
	next := append([]byte{}, data[:insertAt]...)
	next = append(next, block...)
	next = append(next, data[insertAt:]...)
	return writeFileAtomic(path, next)
}

func extractFuncSource(path, name string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, data, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return "", fmt.Errorf("parsing %s: %w", path, err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name {
			continue
		}
		startPos := fn.Pos()
		if fn.Doc != nil {
			startPos = fn.Doc.Pos()
		}
		start := fset.Position(startPos).Offset
		end := fset.Position(fn.End()).Offset
		return strings.TrimSpace(string(data[start:end])), nil
	}
	return "", nil
}

var trailingWhitespaceRE = regexp.MustCompile(`\s*\z`)

func appendGoSource(path, src string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	next := trailingWhitespaceRE.ReplaceAll(data, nil)
	next = append(next, []byte("\n\n"+strings.TrimSpace(src)+"\n")...)
	return writeFileAtomic(path, next)
}

// pruneFreshDeclCollisions removes declarations from fresh-owned Go files when
// a preserved snapshot file in the same package directory already defines the
// same top-level name. This handles template splits such as moving `var
// version` from root.go to version.go while root.go is preserved for real
// hand edits.
func pruneFreshDeclCollisions(freshDir string, report *MergeReport) error {
	preservedByDir := map[string]declSet{}
	freshOwnedByDir := map[string][]string{}
	for _, fc := range report.Files {
		if !strings.HasSuffix(fc.Path, ".go") {
			continue
		}
		dir := filepath.Dir(filepath.ToSlash(fc.Path))
		switch {
		case fc.Applied:
			decls, err := extractDecls(filepath.Join(freshDir, fc.Path))
			if err != nil {
				return err
			}
			if preservedByDir[dir] == nil {
				preservedByDir[dir] = declSet{}
			}
			for name := range decls {
				preservedByDir[dir].add(name)
			}
		case fc.Verdict == VerdictTemplatedClean || fc.Verdict == VerdictNewTemplateEmission:
			freshOwnedByDir[dir] = append(freshOwnedByDir[dir], fc.Path)
		}
	}
	for dir, preserved := range preservedByDir {
		if len(preserved) == 0 {
			continue
		}
		for _, rel := range freshOwnedByDir[dir] {
			if err := pruneDeclsFromGoFile(filepath.Join(freshDir, rel), preserved); err != nil {
				return err
			}
		}
	}
	return nil
}

func pruneDeclsFromGoFile(path string, collisions declSet) error {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	changed := false
	var kept []ast.Decl
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if _, collides := collisions[canonicalFuncName(d)]; collides {
				changed = true
				continue
			}
		case *ast.GenDecl:
			if d.Tok == token.IMPORT {
				kept = append(kept, decl)
				continue
			}
			if d.Tok == token.CONST && constDeclHasPruneRisk(d, collisions) {
				break
			}
			specs := d.Specs[:0]
			for _, spec := range d.Specs {
				next, specChanged := pruneCollidingSpec(spec, collisions)
				if specChanged {
					changed = true
				}
				if next == nil {
					continue
				}
				specs = append(specs, next)
			}
			if len(specs) == 0 {
				continue
			}
			d.Specs = specs
		}
		kept = append(kept, decl)
	}
	if !changed {
		return nil
	}
	file.Decls = kept
	pruneUnusedImports(file)
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, file); err != nil {
		return fmt.Errorf("printing %s: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("statting %s: %w", path, err)
	}
	if err := writeFileAtomic(path, buf.Bytes()); err != nil {
		return err
	}
	return os.Chmod(path, info.Mode().Perm())
}

func constDeclHasPruneRisk(decl *ast.GenDecl, collisions declSet) bool {
	collides := false
	hasImplicitValues := false
	hasIota := false
	for _, spec := range decl.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		if len(valueSpec.Values) == 0 {
			hasImplicitValues = true
		}
		if len(valueSpec.Values) > 0 && exprListUsesIota(valueSpec.Values) {
			hasIota = true
		}
		if valueSpecCollides(valueSpec, collisions) {
			collides = true
		}
	}
	return collides && (hasImplicitValues || hasIota)
}

func exprListUsesIota(exprs []ast.Expr) bool {
	for _, expr := range exprs {
		found := false
		ast.Inspect(expr, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok && ident.Name == "iota" {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

func valueSpecCollides(spec *ast.ValueSpec, collisions declSet) bool {
	for _, name := range spec.Names {
		if _, ok := collisions[name.Name]; ok {
			return true
		}
	}
	return false
}

func pruneUnusedImports(file *ast.File) {
	used := selectorPackageNames(file)
	var decls []ast.Decl
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			decls = append(decls, decl)
			continue
		}
		specs := gen.Specs[:0]
		for _, spec := range gen.Specs {
			importSpec, ok := spec.(*ast.ImportSpec)
			if !ok || importIsUsed(importSpec, used) {
				specs = append(specs, spec)
			}
		}
		if len(specs) == 0 {
			continue
		}
		gen.Specs = specs
		decls = append(decls, gen)
	}
	file.Decls = decls
}

func selectorPackageNames(file *ast.File) map[string]struct{} {
	used := map[string]struct{}{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if ok && gen.Tok == token.IMPORT {
			continue
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if ident, ok := sel.X.(*ast.Ident); ok {
				used[ident.Name] = struct{}{}
			}
			return true
		})
	}
	return used
}

func importIsUsed(spec *ast.ImportSpec, used map[string]struct{}) bool {
	name := importLocalName(spec)
	if name == "" || name == "_" || name == "." {
		return true
	}
	_, ok := used[name]
	return ok
}

func importLocalName(spec *ast.ImportSpec) string {
	if spec.Name != nil {
		return spec.Name.Name
	}
	importPath, err := strconv.Unquote(spec.Path.Value)
	if err != nil || importPath == "" {
		return ""
	}
	return path.Base(importPath)
}

func pruneCollidingSpec(spec ast.Spec, collisions declSet) (ast.Spec, bool) {
	switch s := spec.(type) {
	case *ast.TypeSpec:
		if _, ok := collisions[s.Name.Name]; ok {
			return nil, true
		}
	case *ast.ValueSpec:
		colliding := make([]bool, len(s.Names))
		collisionCount := 0
		for i, name := range s.Names {
			if _, ok := collisions[name.Name]; ok {
				colliding[i] = true
				collisionCount++
			}
		}
		switch {
		case collisionCount == 0:
			return spec, false
		case collisionCount == len(s.Names):
			return nil, true
		case len(s.Values) != 0 && len(s.Values) != len(s.Names):
			return spec, false
		}
		origNames := append([]*ast.Ident(nil), s.Names...)
		origValues := append([]ast.Expr(nil), s.Values...)
		names := s.Names[:0]
		var values []ast.Expr
		if len(origValues) > 0 {
			values = s.Values[:0]
		}
		for i, name := range origNames {
			if colliding[i] {
				continue
			}
			names = append(names, name)
			if len(origValues) > 0 {
				values = append(values, origValues[i])
			}
		}
		s.Names = names
		if len(origValues) > 0 {
			s.Values = values
		}
		return spec, true
	}
	return spec, false
}

func preserveTemplatedDriftInNovelOnly(rel string) bool {
	_, ok := novelOnlyEditableHookPaths[filepath.ToSlash(rel)]
	return ok
}

// novelOnlyEditableHookPaths lists generator-emitted files whose intended
// purpose is to carry agent-authored edits. Add future editable hooks here
// when they need NovelOnly regen to preserve templated drift.
var novelOnlyEditableHookPaths = map[string]struct{}{
	"internal/store/extras.go": {},
}

// copyPreserveFile copies snapshot/rel → fresh/rel, refusing symlinks and
// creating parent dirs as needed.
func copyPreserveFile(snapshotDir, freshDir, rel string) error {
	src := filepath.Join(snapshotDir, rel)
	dst := filepath.Join(freshDir, rel)

	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("statting snapshot file %s: %w", rel, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to preserve symlinked snapshot file: %s", rel)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("creating parent for %s: %w", rel, err)
	}
	if err := copyFileAtomic(src, dst, info.Mode().Perm()); err != nil {
		return fmt.Errorf("writing preserved %s: %w", rel, err)
	}
	if err := os.Chmod(dst, info.Mode().Perm()); err != nil {
		return fmt.Errorf("preserving mode for %s: %w", rel, err)
	}
	if err := os.Chtimes(dst, info.ModTime(), info.ModTime()); err != nil {
		return fmt.Errorf("preserving mtime for %s: %w", rel, err)
	}
	return nil
}

func copyFileAtomic(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening source: %w", err)
	}
	defer func() { _ = in.Close() }()

	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("creating temporary destination: %w", err)
	}
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copying bytes: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("closing temporary destination: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return fmt.Errorf("replacing destination: %w", err)
	}
	removeTmp = false
	return nil
}

// sweepNonClassifiedFiles walks the snapshot for files that the classifier
// did not see (non-Go, non-module files like README.md, Makefile,
// .printing-press.json) and copies any that don't exist in fresh AND don't
// live under a generator-owned directory. Symlinks are refused.
func sweepNonClassifiedFiles(snapshotDir, freshDir string) error {
	return filepath.WalkDir(snapshotDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(snapshotDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if d.IsDir() {
			if isManuscriptsPath(relSlash) {
				return nil
			}
			if !shouldWalkDir(d.Name()) {
				return filepath.SkipDir
			}
			if isGeneratorOwnedInternalDir(relSlash) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isManuscriptsPath(relSlash) && !shouldWalkDir(filepath.Base(filepath.Dir(path))) {
			return nil
		}
		if shouldClassifyFile(relSlash) {
			return nil
		}
		dst := filepath.Join(freshDir, rel)
		if _, err := os.Stat(dst); err == nil {
			// fresh already emitted at this path; fresh wins.
			return nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("statting fresh path %s: %w", relSlash, err)
		}
		if err := copyPreserveFile(snapshotDir, freshDir, rel); err != nil {
			return fmt.Errorf("sweeping snapshot file %s: %w", relSlash, err)
		}
		return nil
	})
}

func isManuscriptsPath(relSlash string) bool {
	return relSlash == ".manuscripts" || strings.HasPrefix(relSlash, ".manuscripts/")
}

// isGeneratorOwnedInternalDir reports whether relSlash names a directory
// under internal/ that the generator owns end-to-end. Used by the sweep to
// avoid copying random non-Go content into a directory the generator
// regenerates from scratch each run.
func isGeneratorOwnedInternalDir(relSlash string) bool {
	const prefix = "internal/"
	rest, ok := strings.CutPrefix(relSlash, prefix)
	if !ok {
		return false
	}
	first, _, _ := strings.Cut(rest, "/")
	if first == "" {
		return false
	}
	_, owned := regenmergeGeneratorOwnedDirs[first]
	return owned
}
