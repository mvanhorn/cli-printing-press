package regenmerge

import (
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

// overlayHandEditedDecls writes dest as fresh, then replaces only the decls
// whose published text differs from the original emission. Decls that still
// match the base keep the fresh rewrite so a patched file cannot freeze
// unpatched function bodies across a reprint.
func tryOverlayHandEditedDecls(pubDir, freshDir, baseDir, destDir, rel string) bool {
	if baseDir == "" || destDir == "" {
		return false
	}
	rel = filepath.ToSlash(rel)
	pubPath := filepath.Join(pubDir, filepath.FromSlash(rel))
	freshPath := filepath.Join(freshDir, filepath.FromSlash(rel))
	basePath := filepath.Join(baseDir, filepath.FromSlash(rel))
	destPath := filepath.Join(destDir, filepath.FromSlash(rel))
	if _, err := os.Stat(basePath); err != nil {
		return false
	}
	return overlayHandEditedDecls(pubPath, freshPath, basePath, destPath) == nil
}

func overlayHandEditedDecls(pubPath, freshPath, basePath, destPath string) error {
	if destPath == "" {
		return errors.New("empty overlay destination")
	}
	fromPub := handEditedDeclNames(pubPath, freshPath, basePath)
	freshData, err := os.ReadFile(freshPath)
	if err != nil {
		return fmt.Errorf("reading fresh %s: %w", freshPath, err)
	}
	if len(fromPub) == 0 {
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return err
		}
		return writeFileAtomic(destPath, freshData)
	}

	pubData, err := os.ReadFile(pubPath)
	if err != nil {
		return fmt.Errorf("reading published %s: %w", pubPath, err)
	}
	pubFile, err := decorator.NewDecorator(nil).ParseFile(pubPath, pubData, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parsing published %s: %w", pubPath, err)
	}
	freshFile, err := decorator.NewDecorator(nil).ParseFile(freshPath, freshData, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parsing fresh %s: %w", freshPath, err)
	}

	pubByName := overlayDeclMap(pubFile)
	pubSpecs := canonicalSpecTexts(pubPath)
	baseSpecs := canonicalSpecTexts(basePath)
	pubNameVals := canonicalNameValueTexts(pubPath)
	baseNameVals := canonicalNameValueTexts(basePath)
	var installed []dst.Decl
	for i, d := range freshFile.Decls {
		name := overlayDeclName(d)
		if !fromPub[name] {
			continue
		}
		repl, ok := pubByName[name]
		if !ok {
			continue
		}
		if merged := mergeGroupedIfNeeded(d, repl, pubSpecs, baseSpecs, pubNameVals, baseNameVals); merged != nil {
			freshFile.Decls[i] = merged
			installed = append(installed, merged)
			continue
		}
		freshFile.Decls[i] = repl
		installed = append(installed, repl)
	}
	addImportsUsedByDecls(freshFile, pubFile, installed)
	rewriteInstalledSelectorsToDestAliases(freshFile, pubFile, installed)
	dropUnusedImports(freshFile)

	var buf bytes.Buffer
	if err := decorator.Fprint(&buf, freshFile); err != nil {
		return fmt.Errorf("rendering overlay: %w", err)
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		formatted = buf.Bytes()
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(destPath, formatted)
}

func handEditedDeclNames(pubPath, freshPath, basePath string) map[string]bool {
	pubTexts := canonicalDeclTexts(pubPath)
	freshTexts := canonicalDeclTexts(freshPath)
	if pubTexts == nil || freshTexts == nil {
		return nil
	}
	var baseTexts map[string]string
	if basePath != "" {
		baseTexts = canonicalDeclTexts(basePath)
	}
	out := map[string]bool{}
	for name, pubText := range pubTexts {
		freshText, inFresh := freshTexts[name]
		if !inFresh || pubText == freshText {
			continue
		}
		if baseTexts != nil && baseTexts[name] != "" && pubText == baseTexts[name] {
			continue
		}
		out[name] = true
	}
	return out
}

func overlayDeclMap(file *dst.File) map[string]dst.Decl {
	out := map[string]dst.Decl{}
	if file == nil {
		return out
	}
	for _, d := range file.Decls {
		if name := overlayDeclName(d); name != "" {
			out[name] = d
		}
	}
	return out
}

func overlayDeclName(d dst.Decl) string {
	switch decl := d.(type) {
	case *dst.FuncDecl:
		name := decl.Name.Name
		if decl.Recv != nil && len(decl.Recv.List) > 0 {
			if recv := dstReceiverTypeName(decl.Recv.List[0].Type); recv != "" {
				name = "(" + recv + ")." + name
			}
		}
		return name
	case *dst.GenDecl:
		if decl.Tok == token.IMPORT {
			return ""
		}
		var names []string
		for _, spec := range decl.Specs {
			switch s := spec.(type) {
			case *dst.TypeSpec:
				if s.Name != nil {
					names = append(names, s.Name.Name)
				}
			case *dst.ValueSpec:
				for _, n := range s.Names {
					names = append(names, n.Name)
				}
			}
		}
		if len(names) == 0 {
			return ""
		}
		return decl.Tok.String() + ":" + strings.Join(names, ",")
	}
	return ""
}

func mergeGroupedIfNeeded(fresh, pub dst.Decl, pubSpecs, baseSpecs, pubNameVals, baseNameVals map[string]string) dst.Decl {
	fg, ok := fresh.(*dst.GenDecl)
	if !ok || fg.Tok == token.IMPORT || (!hasMultiNameValueSpec(fg) && len(fg.Specs) < 2) {
		return nil
	}
	pg, ok := pub.(*dst.GenDecl)
	if !ok {
		return nil
	}
	mergeGroupedGenDecl(fg, pg, pubSpecs, baseSpecs, pubNameVals, baseNameVals)
	return fg
}

func mergeGroupedGenDecl(fresh, pub *dst.GenDecl, pubSpecs, baseSpecs, pubNameVals, baseNameVals map[string]string) {
	if fresh == nil || pub == nil {
		return
	}
	pubByKey := map[string]dst.Spec{}
	for _, spec := range pub.Specs {
		if key := dstSpecKey(spec); key != "" {
			pubByKey[key] = spec
		}
	}
	for i, spec := range fresh.Specs {
		if vs, ok := spec.(*dst.ValueSpec); ok && len(vs.Names) > 1 {
			if pubSpec, ok := pubByKey[dstSpecKey(spec)].(*dst.ValueSpec); ok {
				mergeMultiNameValueSpec(vs, pubSpec, pubNameVals, baseNameVals)
			}
			continue
		}
		key := dstSpecKey(spec)
		pubSpec, ok := pubByKey[key]
		if !ok {
			continue
		}
		if baseSpecs[key] != "" && pubSpecs[key] == baseSpecs[key] {
			continue
		}
		fresh.Specs[i] = pubSpec
	}
	freshKeys := map[string]bool{}
	for _, spec := range fresh.Specs {
		if key := dstSpecKey(spec); key != "" {
			freshKeys[key] = true
		}
	}
	for _, spec := range pub.Specs {
		key := dstSpecKey(spec)
		if key == "" || freshKeys[key] {
			continue
		}
		if baseSpecs[key] != "" && pubSpecs[key] == baseSpecs[key] {
			continue
		}
		fresh.Specs = append(fresh.Specs, spec)
	}
}

func hasMultiNameValueSpec(gd *dst.GenDecl) bool {
	if gd == nil {
		return false
	}
	for _, spec := range gd.Specs {
		if vs, ok := spec.(*dst.ValueSpec); ok && len(vs.Names) > 1 {
			return true
		}
	}
	return false
}

func mergeMultiNameValueSpec(fresh, pub *dst.ValueSpec, pubNameVals, baseNameVals map[string]string) {
	if fresh == nil || pub == nil {
		return
	}
	pubVal := map[string]dst.Expr{}
	for i, n := range pub.Names {
		if i < len(pub.Values) {
			pubVal[n.Name] = pub.Values[i]
		}
	}
	for i, n := range fresh.Names {
		if i >= len(fresh.Values) {
			break
		}
		name := n.Name
		if baseNameVals[name] != "" && pubNameVals[name] == baseNameVals[name] {
			continue
		}
		if v, ok := pubVal[name]; ok {
			fresh.Values[i] = v
		}
	}
}

func dstSpecKey(spec dst.Spec) string {
	switch s := spec.(type) {
	case *dst.TypeSpec:
		if s.Name != nil {
			return s.Name.Name
		}
	case *dst.ValueSpec:
		var names []string
		for _, n := range s.Names {
			names = append(names, n.Name)
		}
		return strings.Join(names, ",")
	}
	return ""
}

func dstReceiverTypeName(expr dst.Expr) string {
	switch t := expr.(type) {
	case *dst.StarExpr:
		return "*" + dstReceiverTypeName(t.X)
	case *dst.Ident:
		return t.Name
	case *dst.IndexExpr:
		return dstReceiverTypeName(t.X)
	case *dst.IndexListExpr:
		return dstReceiverTypeName(t.X)
	}
	return ""
}

func addImportsUsedByDecls(fresh, pub *dst.File, installed []dst.Decl) {
	if fresh == nil || pub == nil || len(installed) == 0 {
		return
	}
	aliasToPath := importAliasMap(pub)
	for alias, path := range importAliasMap(fresh) {
		if _, ok := aliasToPath[alias]; !ok {
			aliasToPath[alias] = path
		}
	}
	needed := importPathsUsedByDecls(installed, aliasToPath)
	have := map[string]bool{}
	for _, path := range importPathsOf(fresh) {
		have[path] = true
	}
	bound := importAliasMap(fresh)
	var missing []dst.Spec
	for _, d := range pub.Decls {
		gd, ok := d.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		for _, spec := range gd.Specs {
			is, ok := spec.(*dst.ImportSpec)
			if !ok || is.Path == nil {
				continue
			}
			if have[is.Path.Value] || !needed[is.Path.Value] {
				continue
			}
			have[is.Path.Value] = true
			alias := importSpecAlias(is)
			if alias == "_" || alias == "." {
				missing = append(missing, is)
				continue
			}
			free := unusedImportAlias(bound, alias, is.Path.Value)
			bound[free] = is.Path.Value
			missing = append(missing, importSpecWithAlias(is, free))
		}
	}
	if len(missing) == 0 {
		return
	}
	for i, d := range fresh.Decls {
		gd, ok := d.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		gd.Specs = append(gd.Specs, missing...)
		fresh.Decls[i] = gd
		return
	}
	fresh.Decls = append([]dst.Decl{&dst.GenDecl{Tok: token.IMPORT, Specs: missing}}, fresh.Decls...)
}

func rewriteInstalledSelectorsToDestAliases(fresh, pub *dst.File, installed []dst.Decl) {
	if fresh == nil || pub == nil || len(installed) == 0 {
		return
	}
	destAliasToPath := importAliasMap(fresh)
	destPathToAlias := map[string]string{}
	for alias, path := range destAliasToPath {
		if _, ok := destPathToAlias[path]; !ok {
			destPathToAlias[path] = alias
		}
	}
	pubAliasToPath := importAliasMap(pub)
	for _, d := range installed {
		forEachPackageSelector(d, func(id *dst.Ident) {
			path := pubAliasToPath[id.Name]
			if path == "" || destAliasToPath[id.Name] == path {
				return
			}
			destAlias, ok := destPathToAlias[path]
			if !ok || destAlias == id.Name {
				return
			}
			id.Name = destAlias
		})
	}
}

func dropUnusedImports(file *dst.File) {
	if file == nil {
		return
	}
	aliasToPath := importAliasMap(file)
	var body []dst.Decl
	for _, d := range file.Decls {
		if gd, ok := d.(*dst.GenDecl); ok && gd.Tok == token.IMPORT {
			continue
		}
		body = append(body, d)
	}
	needed := importPathsUsedByDecls(body, aliasToPath)

	var decls []dst.Decl
	for _, d := range file.Decls {
		gd, ok := d.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			decls = append(decls, d)
			continue
		}
		var specs []dst.Spec
		for _, spec := range gd.Specs {
			is, ok := spec.(*dst.ImportSpec)
			if !ok || is.Path == nil {
				specs = append(specs, spec)
				continue
			}
			if alias := importSpecAlias(is); alias == "_" || alias == "." {
				specs = append(specs, spec)
				continue
			}
			if needed[is.Path.Value] {
				specs = append(specs, spec)
			}
		}
		if len(specs) == 0 {
			continue
		}
		gd.Specs = specs
		decls = append(decls, gd)
	}
	file.Decls = decls
}

func importAliasMap(file *dst.File) map[string]string {
	out := map[string]string{}
	if file == nil {
		return out
	}
	for _, d := range file.Decls {
		gd, ok := d.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		for _, spec := range gd.Specs {
			is, ok := spec.(*dst.ImportSpec)
			if !ok || is.Path == nil {
				continue
			}
			alias := importSpecAlias(is)
			if alias == "" || alias == "_" || alias == "." {
				continue
			}
			out[alias] = is.Path.Value
		}
	}
	return out
}

func importSpecAlias(is *dst.ImportSpec) string {
	if is.Name != nil {
		return is.Name.Name
	}
	return importPathDefaultAlias(is.Path.Value)
}

func importPathDefaultAlias(quotedPath string) string {
	path, err := strconv.Unquote(quotedPath)
	if err != nil {
		return ""
	}
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func unusedImportAlias(bound map[string]string, preferred, path string) string {
	if bound[preferred] == "" || bound[preferred] == path {
		return preferred
	}
	def := importPathDefaultAlias(path)
	if def != "" && (bound[def] == "" || bound[def] == path) {
		return def
	}
	if def == "" {
		def = "pkg"
	}
	for i := 2; ; i++ {
		cand := def + strconv.Itoa(i)
		if bound[cand] == "" {
			return cand
		}
	}
}

func importSpecWithAlias(is *dst.ImportSpec, alias string) *dst.ImportSpec {
	out := &dst.ImportSpec{
		Path: &dst.BasicLit{Kind: token.STRING, Value: is.Path.Value},
	}
	if alias != "" && alias != importPathDefaultAlias(is.Path.Value) {
		out.Name = &dst.Ident{Name: alias}
	}
	return out
}

func importPathsUsedByDecls(decls []dst.Decl, aliasToPath map[string]string) map[string]bool {
	used := map[string]bool{}
	for _, d := range decls {
		forEachPackageSelector(d, func(id *dst.Ident) {
			if path, ok := aliasToPath[id.Name]; ok {
				used[path] = true
			}
		})
	}
	return used
}

type bindingScopes struct {
	stack []map[string]bool
}

func (s *bindingScopes) push() {
	s.stack = append(s.stack, map[string]bool{})
}

func (s *bindingScopes) pop() {
	if len(s.stack) == 0 {
		return
	}
	s.stack = s.stack[:len(s.stack)-1]
}

func (s *bindingScopes) bind(name string) {
	if s == nil || name == "" || name == "_" || len(s.stack) == 0 {
		return
	}
	s.stack[len(s.stack)-1][name] = true
}

func (s *bindingScopes) isLocal(name string) bool {
	if s == nil {
		return false
	}
	for i := len(s.stack) - 1; i >= 0; i-- {
		if s.stack[i][name] {
			return true
		}
	}
	return false
}

func walkFieldListTypes(fl *dst.FieldList, sc *bindingScopes, visit func(*dst.Ident)) {
	if fl == nil {
		return
	}
	for _, f := range fl.List {
		walkOverlayExpr(f.Type, sc, visit)
	}
}

func bindFields(sc *bindingScopes, fl *dst.FieldList) {
	if fl == nil {
		return
	}
	for _, f := range fl.List {
		for _, n := range f.Names {
			sc.bind(n.Name)
		}
	}
}

func isPackageSelectorIdent(id *dst.Ident, sc *bindingScopes) bool {
	if id.Obj != nil && id.Obj.Kind != dst.Pkg {
		return false
	}
	return !sc.isLocal(id.Name)
}

func forEachPackageSelector(d dst.Decl, visit func(*dst.Ident)) {
	if d == nil || visit == nil {
		return
	}
	if fd, ok := d.(*dst.FuncDecl); ok {
		sc := &bindingScopes{}
		sc.push()
		if fd.Type != nil {
			walkFieldListTypes(fd.Type.TypeParams, sc, visit)
			bindFields(sc, fd.Type.TypeParams)
		}
		walkFieldListTypes(fd.Recv, sc, visit)
		if fd.Type != nil {
			walkFieldListTypes(fd.Type.Params, sc, visit)
			walkFieldListTypes(fd.Type.Results, sc, visit)
		}
		bindFields(sc, fd.Recv)
		if fd.Type != nil {
			bindFields(sc, fd.Type.Params)
			bindFields(sc, fd.Type.Results)
		}
		walkOverlayStmt(fd.Body, sc, visit)
		return
	}
	dst.Inspect(d, func(n dst.Node) bool {
		sel, ok := n.(*dst.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*dst.Ident)
		if ok && isPackageSelectorIdent(id, nil) {
			visit(id)
		}
		return true
	})
}

func walkOverlayStmt(s dst.Stmt, sc *bindingScopes, visit func(*dst.Ident)) {
	if s == nil {
		return
	}
	switch x := s.(type) {
	case *dst.BlockStmt:
		sc.push()
		for _, c := range x.List {
			walkOverlayStmt(c, sc, visit)
		}
		sc.pop()
	case *dst.AssignStmt:
		for _, e := range x.Rhs {
			walkOverlayExpr(e, sc, visit)
		}
		if x.Tok == token.DEFINE {
			for _, e := range x.Lhs {
				if id, ok := e.(*dst.Ident); ok {
					sc.bind(id.Name)
					continue
				}
				walkOverlayExpr(e, sc, visit)
			}
			return
		}
		for _, e := range x.Lhs {
			walkOverlayExpr(e, sc, visit)
		}
	case *dst.DeclStmt:
		gd, ok := x.Decl.(*dst.GenDecl)
		if !ok {
			return
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*dst.ValueSpec)
			if !ok {
				continue
			}
			for _, e := range vs.Values {
				walkOverlayExpr(e, sc, visit)
			}
			for _, n := range vs.Names {
				sc.bind(n.Name)
			}
		}
	case *dst.ExprStmt:
		walkOverlayExpr(x.X, sc, visit)
	case *dst.IfStmt:
		sc.push()
		walkOverlayStmt(x.Init, sc, visit)
		walkOverlayExpr(x.Cond, sc, visit)
		walkOverlayStmt(x.Body, sc, visit)
		walkOverlayStmt(x.Else, sc, visit)
		sc.pop()
	case *dst.RangeStmt:
		walkOverlayExpr(x.X, sc, visit)
		sc.push()
		if x.Tok == token.DEFINE {
			if id, ok := x.Key.(*dst.Ident); ok {
				sc.bind(id.Name)
			}
			if id, ok := x.Value.(*dst.Ident); ok {
				sc.bind(id.Name)
			}
		} else {
			walkOverlayExpr(x.Key, sc, visit)
			walkOverlayExpr(x.Value, sc, visit)
		}
		walkOverlayStmt(x.Body, sc, visit)
		sc.pop()
	case *dst.ForStmt:
		sc.push()
		walkOverlayStmt(x.Init, sc, visit)
		walkOverlayExpr(x.Cond, sc, visit)
		walkOverlayStmt(x.Post, sc, visit)
		walkOverlayStmt(x.Body, sc, visit)
		sc.pop()
	case *dst.ReturnStmt:
		for _, e := range x.Results {
			walkOverlayExpr(e, sc, visit)
		}
	case *dst.GoStmt:
		walkOverlayExpr(x.Call, sc, visit)
	case *dst.DeferStmt:
		walkOverlayExpr(x.Call, sc, visit)
	case *dst.SwitchStmt:
		sc.push()
		walkOverlayStmt(x.Init, sc, visit)
		walkOverlayExpr(x.Tag, sc, visit)
		walkOverlayStmt(x.Body, sc, visit)
		sc.pop()
	case *dst.TypeSwitchStmt:
		sc.push()
		walkOverlayStmt(x.Init, sc, visit)
		walkOverlayStmt(x.Assign, sc, visit)
		walkOverlayStmt(x.Body, sc, visit)
		sc.pop()
	case *dst.SelectStmt:
		walkOverlayStmt(x.Body, sc, visit)
	case *dst.CaseClause:
		for _, e := range x.List {
			walkOverlayExpr(e, sc, visit)
		}
		for _, c := range x.Body {
			walkOverlayStmt(c, sc, visit)
		}
	case *dst.CommClause:
		walkOverlayStmt(x.Comm, sc, visit)
		for _, c := range x.Body {
			walkOverlayStmt(c, sc, visit)
		}
	case *dst.LabeledStmt:
		walkOverlayStmt(x.Stmt, sc, visit)
	case *dst.SendStmt:
		walkOverlayExpr(x.Chan, sc, visit)
		walkOverlayExpr(x.Value, sc, visit)
	case *dst.IncDecStmt:
		walkOverlayExpr(x.X, sc, visit)
	}
}

func walkOverlayExpr(e dst.Expr, sc *bindingScopes, visit func(*dst.Ident)) {
	if e == nil {
		return
	}
	dst.Inspect(e, func(n dst.Node) bool {
		switch x := n.(type) {
		case *dst.FuncLit:
			sc.push()
			if x.Type != nil {
				walkFieldListTypes(x.Type.TypeParams, sc, visit)
				bindFields(sc, x.Type.TypeParams)
				walkFieldListTypes(x.Type.Params, sc, visit)
				walkFieldListTypes(x.Type.Results, sc, visit)
				bindFields(sc, x.Type.Params)
				bindFields(sc, x.Type.Results)
			}
			walkOverlayStmt(x.Body, sc, visit)
			sc.pop()
			return false
		case *dst.SelectorExpr:
			if id, ok := x.X.(*dst.Ident); ok && isPackageSelectorIdent(id, sc) {
				visit(id)
			}
		}
		return true
	})
}

func importPathsOf(file *dst.File) []string {
	var out []string
	if file == nil {
		return out
	}
	for _, d := range file.Decls {
		gd, ok := d.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		for _, spec := range gd.Specs {
			is, ok := spec.(*dst.ImportSpec)
			if !ok || is.Path == nil {
				continue
			}
			out = append(out, is.Path.Value)
		}
	}
	return out
}
