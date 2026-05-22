package pipeline

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const internalImportDelimiter = "/internal/"

type scorecardReachablePackage struct {
	dir     string
	symbols map[string]bool
}

func hasSyncPaginationStructureInFiles(paths []string) bool {
	for _, file := range goASTFiles(paths) {
		var found bool
		ast.Inspect(file, func(n ast.Node) bool {
			if found {
				return false
			}
			loop, ok := n.(*ast.ForStmt)
			if !ok || loop.Body == nil {
				return true
			}
			found = loopLooksLikePaginatedFetch(loop.Body)
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

func goASTFiles(paths []string) []*ast.File {
	parsed := make([]*ast.File, 0, len(paths))
	for _, path := range paths {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			continue
		}
		parsed = append(parsed, file)
	}
	return parsed
}

func loopLooksLikePaginatedFetch(body *ast.BlockStmt) bool {
	var hasFetchCall, hasCursorSignal, hasStateSave, hasExit bool
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			if selectorName(node.Fun, "Get", "GetWithHeaders", "Do", "Execute", "Query") || strings.Contains(strings.ToLower(callName(node.Fun)), "fetch") {
				hasFetchCall = true
			}
			if selectorName(node.Fun, "SaveSyncState", "SaveSyncCursor") {
				hasStateSave = true
			}
		case *ast.Ident:
			if paginationName(node.Name) {
				hasCursorSignal = true
			}
		case *ast.SelectorExpr:
			if paginationName(node.Sel.Name) {
				hasCursorSignal = true
			}
		case *ast.BasicLit:
			if node.Kind == token.STRING && paginationLiteral(node.Value) {
				hasCursorSignal = true
			}
		case *ast.BranchStmt:
			if node.Tok == token.BREAK {
				hasExit = true
			}
		case *ast.ReturnStmt:
			hasExit = true
		}
		return true
	})
	return hasFetchCall && hasCursorSignal && (hasStateSave || hasExit)
}

func hasPageProgressStructureInFiles(paths []string) bool {
	for _, file := range goASTFiles(paths) {
		var found bool
		ast.Inspect(file, func(n ast.Node) bool {
			if found {
				return false
			}
			loop, ok := n.(*ast.ForStmt)
			if !ok || loop.Body == nil {
				return true
			}
			found = loopPrintsPageProgress(loop.Body)
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

func scorecardReachableInternalContents(dir string) []string {
	return scorecardContentsFromFiles(scorecardReachableInternalFiles(dir))
}

func scorecardContentsFromFiles(files []string) []string {
	contents := make([]string, 0, len(files))
	for _, path := range files {
		contents = append(contents, readFileContent(path))
	}
	return contents
}

func scorecardReachableInternalFiles(dir string) []string {
	var filePaths []string
	includedFiles := map[string]bool{}
	inQueue := map[string]bool{}
	pendingSymbols := map[string]map[string]bool{}
	cliDir := filepath.Join(dir, "internal", "cli")
	queue := []scorecardReachablePackage{{dir: cliDir}}
	inQueue[cliDir] = true
	modulePath := readModulePath(dir)

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		pkgDir := current.dir
		inQueue[pkgDir] = false

		files := scorecardPackageSeedFiles(dir, pkgDir, pendingSymbols[pkgDir])
		depsByDir := map[string]map[string]bool{}
		for _, path := range files {
			if !includedFiles[path] {
				includedFiles[path] = true
				filePaths = append(filePaths, path)
			}

			for _, dep := range internalImportDeps(dir, modulePath, path) {
				mergeSymbols(depsByDir, dep.dir, dep.symbols)
			}
		}

		depDirs := make([]string, 0, len(depsByDir))
		for depDir := range depsByDir {
			depDirs = append(depDirs, depDir)
		}
		slices.Sort(depDirs)
		for _, depDir := range depDirs {
			if mergeSymbols(pendingSymbols, depDir, depsByDir[depDir]) && !inQueue[depDir] {
				inQueue[depDir] = true
				queue = append(queue, scorecardReachablePackage{dir: depDir})
			}
		}
	}

	return filePaths
}

func mergeSymbols(target map[string]map[string]bool, dir string, symbols map[string]bool) bool {
	if target[dir] == nil {
		target[dir] = map[string]bool{}
	}
	changed := false
	for symbol := range symbols {
		if !target[dir][symbol] {
			target[dir][symbol] = true
			changed = true
		}
	}
	return changed
}

func scorecardPackageSeedFiles(rootDir, pkgDir string, symbols map[string]bool) []string {
	files := listGoFiles(pkgDir)
	if pkgDir != filepath.Join(rootDir, "internal", "cli") {
		return scorecardFilesForSymbols(files, symbols)
	}

	registeredFiles := registeredCommandFiles(pkgDir)
	if len(registeredFiles) == 0 {
		return files
	}

	filtered := make([]string, 0, len(files))
	for _, path := range files {
		name := filepath.Base(path)
		if infraCoreFiles[name] || registeredFiles[name] {
			filtered = append(filtered, path)
		}
	}
	return filtered
}

func scorecardFilesForSymbols(files []string, symbols map[string]bool) []string {
	if len(symbols) == 0 {
		return files
	}

	wanted := map[string]bool{}
	for symbol := range symbols {
		wanted[symbol] = true
	}

	parsedFiles := map[string]*ast.File{}
	fset := token.NewFileSet()
	for _, path := range files {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err == nil {
			parsedFiles[path] = file
		}
	}

	included := map[string]bool{}
	var selected []string
	for {
		changed := false
		for _, path := range files {
			if included[path] {
				continue
			}
			file := parsedFiles[path]
			if file == nil || !fileDefinesAny(file, wanted) {
				continue
			}
			included[path] = true
			selected = append(selected, path)
			changed = true
			for symbol := range samePackageCallNames(file) {
				wanted[symbol] = true
			}
		}
		if !changed {
			break
		}
	}
	return selected
}

func fileDefinesAny(file *ast.File, symbols map[string]bool) bool {
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if symbols[d.Name.Name] {
				return true
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					if symbols[s.Name.Name] {
						return true
					}
				case *ast.ValueSpec:
					for _, name := range s.Names {
						if symbols[name.Name] {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

func samePackageCallNames(file *ast.File) map[string]bool {
	calls := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok {
			calls[ident.Name] = true
		}
		return true
	})
	return calls
}

func containsAnyInAny(contents []string, needles ...string) bool {
	for _, content := range contents {
		for _, needle := range needles {
			if strings.Contains(content, needle) {
				return true
			}
		}
	}
	return false
}

func containsAllInAny(contents []string, needles ...string) bool {
	for _, content := range contents {
		if slices.ContainsFunc(needles, func(needle string) bool {
			return !strings.Contains(content, needle)
		}) {
			continue
		}
		return true
	}
	return false
}

func countAcross(contents []string, needle string) int {
	total := 0
	for _, content := range contents {
		total += strings.Count(content, needle)
	}
	return total
}

func readModulePath(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if modulePath, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(modulePath)
		}
	}
	return ""
}

func internalImportDeps(rootDir, modulePath, goFile string) []scorecardReachablePackage {
	parsed, err := parser.ParseFile(token.NewFileSet(), goFile, nil, 0)
	if err != nil {
		return nil
	}

	aliases := map[string]string{}
	for _, spec := range parsed.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		internalPath := internalImportPath(modulePath, importPath)
		if internalPath == "" {
			continue
		}
		dir := filepath.Join(rootDir, "internal", filepath.FromSlash(internalPath))
		alias := defaultImportAlias(dir, internalPath)
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		if alias == "_" || alias == "." {
			continue
		}
		aliases[alias] = dir
	}
	if len(aliases) == 0 {
		return nil
	}

	symbolsByDir := map[string]map[string]bool{}
	ast.Inspect(parsed, func(n ast.Node) bool {
		selector, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := selector.X.(*ast.Ident)
		if !ok {
			return true
		}
		dir := aliases[ident.Name]
		if dir == "" {
			return true
		}
		if symbolsByDir[dir] == nil {
			symbolsByDir[dir] = map[string]bool{}
		}
		symbolsByDir[dir][selector.Sel.Name] = true
		return true
	})

	deps := make([]scorecardReachablePackage, 0, len(symbolsByDir))
	for dir, symbols := range symbolsByDir {
		deps = append(deps, scorecardReachablePackage{dir: dir, symbols: symbols})
	}
	return deps
}

func defaultImportAlias(pkgDir, internalPath string) string {
	for _, path := range listGoFiles(pkgDir) {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.PackageClauseOnly)
		if err == nil && file.Name != nil {
			return file.Name.Name
		}
	}
	return filepath.Base(filepath.FromSlash(internalPath))
}

func internalImportPath(modulePath, importPath string) string {
	if modulePath != "" {
		prefix := modulePath + internalImportDelimiter
		if internalPath, ok := strings.CutPrefix(importPath, prefix); ok {
			return internalPath
		}
		return ""
	}
	if _, internalPath, ok := strings.Cut(importPath, internalImportDelimiter); ok {
		return internalPath
	}
	return ""
}

func hasRegisteredCommandFileWithPrefix(cliDir, prefix string) bool {
	registeredFiles := registeredCommandFiles(cliDir)
	entries, err := os.ReadDir(cliDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if len(registeredFiles) > 0 && !registeredFiles[name] {
			continue
		}
		if strings.HasPrefix(strings.ToLower(name), prefix) {
			return true
		}
	}
	return false
}

func loopPrintsPageProgress(body *ast.BlockStmt) bool {
	var found bool
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !selectorName(call.Fun, "Printf", "Fprintf", "Fprintln", "Println") {
			return true
		}
		found = slices.ContainsFunc(call.Args, exprMentionsPage)
		return !found
	})
	return found
}

func selectorName(expr ast.Expr, names ...string) bool {
	got := callName(expr)
	return slices.Contains(names, got)
}

func callName(expr ast.Expr) string {
	switch fn := expr.(type) {
	case *ast.SelectorExpr:
		return fn.Sel.Name
	case *ast.Ident:
		return fn.Name
	default:
		return ""
	}
}

func exprMentionsPage(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return false
		}
		v, err := strconv.Unquote(e.Value)
		if err != nil {
			v = e.Value
		}
		return strings.Contains(strings.ToLower(v), "page")
	case *ast.Ident:
		return strings.Contains(strings.ToLower(e.Name), "page")
	case *ast.SelectorExpr:
		return strings.Contains(strings.ToLower(e.Sel.Name), "page")
	default:
		return false
	}
}

func paginationName(name string) bool {
	name = strings.ToLower(name)
	return strings.Contains(name, "cursor") ||
		strings.Contains(name, "nextpage") ||
		strings.Contains(name, "pagetoken") ||
		strings.Contains(name, "hasmore") ||
		strings.Contains(name, "hasnext")
}

func paginationLiteral(value string) bool {
	v, err := strconv.Unquote(value)
	if err != nil {
		v = value
	}
	v = strings.ToLower(v)
	return strings.Contains(v, "cursor") || strings.Contains(v, "next_page") || strings.Contains(v, "nextpage") || strings.Contains(v, "page_token") || strings.Contains(v, "pagetoken")
}

func codeOrchDelegatedFromRegisterTools(dir string) bool {
	toolsPath := filepath.Join(dir, "internal", "mcp", "tools.go")
	file, err := parser.ParseFile(token.NewFileSet(), toolsPath, nil, 0)
	if err != nil {
		return false
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Name == nil || fn.Name.Name != "RegisterTools" {
			continue
		}
		var delegates bool
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && selectorName(call.Fun, "RegisterCodeOrchestrationTools") {
				delegates = true
				return false
			}
			return true
		})
		return delegates
	}
	return false
}
