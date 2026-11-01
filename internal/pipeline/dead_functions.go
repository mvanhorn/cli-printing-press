package pipeline

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type definedFunc struct {
	key  string
	file string
	dir  string
	pkg  string
	name string
}

// deadFuncLoc is one unreferenced top-level function and the file that
// defines it. Removal uses this path instead of searching source text, so a
// declaration such as `func name[T any](...)` is still removed.
type deadFuncLoc struct {
	name string
	file string
}

// scanFuncUses reports top-level functions defined in defFiles and which of
// those candidates are unreferenced from defFiles and useFiles.
//
// A use is an identifier that resolves to the function: a call, a value
// passed to another function, an assignment, or a composite-literal value.
// Calls inside methods and allowlisted helpers count. Method selectors,
// struct-field keys, and longer identifiers that only contain the name do not.
// Files that fail to parse still count a whole-word mention, so a broken use
// file cannot make a live function look dead.
func scanFuncUses(defFiles, useFiles []string) (candidates, dead []string) {
	funcs, referenced := analyzeFuncUses(defFiles, useFiles)
	return candidateNames(funcs, referenced)
}

func analyzeFuncUses(defFiles, useFiles []string) ([]definedFunc, map[string]struct{}) {
	defSet := map[string]struct{}{}
	var paths []string
	for _, p := range defFiles {
		abs := absPath(p)
		if _, ok := defSet[abs]; ok {
			continue
		}
		defSet[abs] = struct{}{}
		paths = append(paths, abs)
	}
	useSet := map[string]struct{}{}
	for _, p := range useFiles {
		abs := absPath(p)
		if _, ok := useSet[abs]; ok {
			continue
		}
		useSet[abs] = struct{}{}
		if _, ok := defSet[abs]; ok {
			continue
		}
		paths = append(paths, abs)
	}
	if len(defSet) == 0 {
		return nil, nil
	}

	fset := token.NewFileSet()
	var parsed []parsedGoFile
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		file, perr := parser.ParseFile(fset, p, src, parser.SkipObjectResolution)
		parsed = append(parsed, parsedGoFile{
			path:     p,
			dir:      filepath.Dir(p),
			src:      src,
			file:     file,
			parseErr: perr,
		})
	}

	var funcs []definedFunc
	seenKey := map[string]struct{}{}
	for i := range parsed {
		pf := &parsed[i]
		if pf.parseErr != nil || pf.file == nil {
			continue
		}
		if _, ok := defSet[pf.path]; !ok {
			continue
		}
		pf.pkg = pf.file.Name.Name
		for _, decl := range pf.file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name == nil || fn.Name.Name == "" || fn.Name.Name == "_" {
				continue
			}
			df := definedFunc{
				file: pf.path,
				dir:  pf.dir,
				pkg:  pf.pkg,
				name: fn.Name.Name,
			}
			df.key = df.dir + "\x00" + df.pkg + "\x00" + df.name
			if _, ok := seenKey[df.key]; ok {
				continue
			}
			seenKey[df.key] = struct{}{}
			funcs = append(funcs, df)
		}
	}

	referenced := map[string]struct{}{}
	byDirPkg := map[string][]*parsedGoFile{}
	var groupOrder []string
	for i := range parsed {
		pf := &parsed[i]
		if pf.parseErr != nil || pf.file == nil || pf.file.Name == nil {
			continue
		}
		pf.pkg = pf.file.Name.Name
		gk := pf.dir + "\x00" + pf.pkg
		if _, ok := byDirPkg[gk]; !ok {
			groupOrder = append(groupOrder, gk)
		}
		byDirPkg[gk] = append(byDirPkg[gk], pf)
	}
	for _, gk := range groupOrder {
		group := byDirPkg[gk]
		recordPackageUses(fset, group, funcs, referenced)
	}
	for i := range parsed {
		pf := &parsed[i]
		if pf.parseErr == nil {
			continue
		}
		if _, isUse := useSet[pf.path]; !isUse {
			if _, isDef := defSet[pf.path]; !isDef {
				continue
			}
		}
		markRawMentions(pf.src, funcs, referenced)
	}

	return funcs, referenced
}

func findDeadFuncLocs(cliDir string, extraSearchDirs ...string) []deadFuncLoc {
	defFiles := listGoFiles(cliDir)
	if len(defFiles) == 0 {
		return nil
	}
	useFiles := append(append([]string{}, defFiles...), listGoTestFiles(cliDir)...)
	for _, dir := range extraSearchDirs {
		useFiles = append(useFiles, listGoFilesRecursive(dir)...)
	}
	funcs, referenced := analyzeFuncUses(defFiles, useFiles)
	return deadFuncLocs(funcs, referenced)
}

func deadFuncLocs(funcs []definedFunc, referenced map[string]struct{}) []deadFuncLoc {
	if referenced == nil {
		referenced = map[string]struct{}{}
	}
	live := map[string]struct{}{}
	for _, fn := range funcs {
		if skipDeadFuncCandidate(fn.name) {
			continue
		}
		if _, ok := referenced[fn.key]; ok {
			live[fn.name] = struct{}{}
		}
	}
	seen := map[string]struct{}{}
	var locs []deadFuncLoc
	for _, fn := range funcs {
		if skipDeadFuncCandidate(fn.name) {
			continue
		}
		if _, ok := live[fn.name]; ok {
			continue
		}
		key := fn.file + "\x00" + fn.name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		locs = append(locs, deadFuncLoc{name: fn.name, file: fn.file})
	}
	sort.Slice(locs, func(i, j int) bool {
		if locs[i].name != locs[j].name {
			return locs[i].name < locs[j].name
		}
		return locs[i].file < locs[j].file
	})
	if len(locs) == 0 {
		return nil
	}
	return locs
}

func deadFuncNames(locs []deadFuncLoc) []string {
	seen := map[string]struct{}{}
	var names []string
	for _, loc := range locs {
		if _, ok := seen[loc.name]; ok {
			continue
		}
		seen[loc.name] = struct{}{}
		names = append(names, loc.name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil
	}
	return names
}

func candidateNames(funcs []definedFunc, referenced map[string]struct{}) (candidates, dead []string) {
	live := map[string]struct{}{}
	unused := map[string]struct{}{}
	for _, fn := range funcs {
		if skipDeadFuncCandidate(fn.name) {
			continue
		}
		if _, ok := referenced[fn.key]; ok {
			live[fn.name] = struct{}{}
			continue
		}
		unused[fn.name] = struct{}{}
	}
	for name := range live {
		delete(unused, name)
		candidates = append(candidates, name)
	}
	for name := range unused {
		candidates = append(candidates, name)
		dead = append(dead, name)
	}
	sort.Strings(candidates)
	sort.Strings(dead)
	if len(dead) == 0 {
		dead = nil
	}
	return candidates, dead
}

func skipDeadFuncCandidate(name string) bool {
	switch name {
	case "main", "init":
		return true
	}
	if strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Benchmark") {
		return true
	}
	return isAllowedDeadHelper(name)
}

type parsedGoFile struct {
	path     string
	dir      string
	pkg      string
	src      []byte
	file     *ast.File
	parseErr error
}

func recordPackageUses(fset *token.FileSet, group []*parsedGoFile, funcs []definedFunc, referenced map[string]struct{}) {
	if len(group) == 0 {
		return
	}
	files := make([]*ast.File, 0, len(group))
	for _, pf := range group {
		files = append(files, pf.file)
	}
	synth := filepath.ToSlash(group[0].dir) + "/" + group[0].pkg
	info := &types.Info{
		Uses: make(map[*ast.Ident]types.Object),
		Defs: make(map[*ast.Ident]types.Object),
	}
	conf := types.Config{
		Importer: &stubImporter{pkgs: map[string]*types.Package{}},
		Error:    func(error) {},
	}
	_, _ = conf.Check(synth, fset, files, info)

	for _, obj := range info.Uses {
		fn, ok := obj.(*types.Func)
		if !ok || fn.Pkg() == nil || fn.Pkg().Path() != synth {
			continue
		}
		if fn.Signature().Recv() != nil {
			continue
		}
		for i := range funcs {
			if funcs[i].name == fn.Name() && funcs[i].dir == group[0].dir && funcs[i].pkg == group[0].pkg {
				referenced[funcs[i].key] = struct{}{}
			}
		}
	}

	for _, pf := range group {
		recordSelectorUses(pf.file, info, funcs, referenced)
		recordDotImportUses(pf.file, info, funcs, referenced)
	}
}

func recordSelectorUses(file *ast.File, info *types.Info, funcs []definedFunc, referenced map[string]struct{}) {
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel == nil {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		switch obj := info.Uses[id].(type) {
		case *types.PkgName:
			markImport(obj.Imported().Path(), sel.Sel.Name, funcs, referenced)
		case nil:
			// Package-qualified name with no import, as in the dogfood and
			// scorecard fixtures (`cli.MCPStorePath`). A same-package selector
			// whose receiver failed to type-check is not this shape.
			if file.Name != nil && id.Name == file.Name.Name {
				return true
			}
			markPackageNamed(id.Name, sel.Sel.Name, funcs, referenced)
		}
		return true
	})
}

func recordDotImportUses(file *ast.File, info *types.Info, funcs []definedFunc, referenced map[string]struct{}) {
	var paths []string
	for _, imp := range file.Imports {
		if imp.Name == nil || imp.Name.Name != "." || imp.Path == nil {
			continue
		}
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		paths = append(paths, p)
	}
	if len(paths) == 0 {
		return
	}
	skip := map[*ast.Ident]struct{}{}
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		skip[sel.Sel] = struct{}{}
		if id, ok := sel.X.(*ast.Ident); ok {
			skip[id] = struct{}{}
		}
		return true
	})
	ast.Inspect(file, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok || id == nil || id.Name == "" || id.Name == "_" {
			return true
		}
		if _, ok := skip[id]; ok || info.Defs[id] != nil || info.Uses[id] != nil {
			return true
		}
		for _, p := range paths {
			markImport(p, id.Name, funcs, referenced)
		}
		return true
	})
}

func markImport(importPath, name string, funcs []definedFunc, referenced map[string]struct{}) {
	for i := range funcs {
		if funcs[i].name != name {
			continue
		}
		if importSelectsDir(importPath, funcs[i].dir) {
			referenced[funcs[i].key] = struct{}{}
		}
	}
}

func markPackageNamed(pkgName, name string, funcs []definedFunc, referenced map[string]struct{}) {
	for i := range funcs {
		if funcs[i].pkg == pkgName && funcs[i].name == name {
			referenced[funcs[i].key] = struct{}{}
		}
	}
}

// importSelectsDir reports whether importPath refers to the package in dir.
// The last two path elements must match, so internal/cliutil is not internal/cli.
func importSelectsDir(importPath, dir string) bool {
	importPath = path.Clean(importPath)
	parts := strings.Split(importPath, "/")
	if len(parts) < 2 {
		return false
	}
	parent := filepath.Base(filepath.Dir(dir))
	if parent == "." || parent == string(filepath.Separator) {
		return false
	}
	return parts[len(parts)-1] == filepath.Base(dir) && parts[len(parts)-2] == parent
}

func markRawMentions(src []byte, funcs []definedFunc, referenced map[string]struct{}) {
	text := string(src)
	seen := map[string]struct{}{}
	for i := range funcs {
		if _, ok := seen[funcs[i].name]; ok {
			continue
		}
		seen[funcs[i].name] = struct{}{}
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(funcs[i].name) + `\b`)
		if !re.MatchString(text) {
			continue
		}
		for j := range funcs {
			if funcs[j].name == funcs[i].name {
				referenced[funcs[j].key] = struct{}{}
			}
		}
	}
}

func absPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	return abs
}

func listGoFilesRecursive(dir string) []string {
	var files []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	sort.Strings(files)
	return files
}

type stubImporter struct {
	pkgs map[string]*types.Package
}

func (m *stubImporter) Import(path string) (*types.Package, error) {
	if path == "unsafe" {
		return types.Unsafe, nil
	}
	if pkg := m.pkgs[path]; pkg != nil {
		return pkg, nil
	}
	name := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		name = path[i+1:]
	}
	pkg := types.NewPackage(path, name)
	pkg.MarkComplete()
	m.pkgs[path] = pkg
	return pkg, nil
}
