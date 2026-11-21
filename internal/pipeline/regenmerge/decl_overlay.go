package regenmerge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"go/format"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
)

// overlayHandEditedDecls writes dest as fresh, then overlays published decls
// that are hand-authored: published-only additions, and (when base exists)
// shared decls that no longer match the original emission. Shared function
// bodies stay on fresh when there is no base so a reprint cannot freeze
// unpatched templated implementations.
func tryOverlayHandEditedDecls(pubDir, freshDir, baseDir, destDir, rel string) (bool, error) {
	if destDir == "" {
		return false, nil
	}
	rel = filepath.ToSlash(rel)
	pubPath := filepath.Join(pubDir, filepath.FromSlash(rel))
	freshPath := filepath.Join(freshDir, filepath.FromSlash(rel))
	destPath := filepath.Join(destDir, filepath.FromSlash(rel))
	if !shouldOverlayOntoFresh(rel, pubPath, freshPath) {
		return false, nil
	}
	basePath := ""
	if baseDir != "" {
		candidate := filepath.Join(baseDir, filepath.FromSlash(rel))
		if _, err := os.Stat(candidate); err == nil {
			basePath = candidate
		}
	}
	if declKindConflict(pubPath, freshPath, basePath) {
		if len(planBothSidesFuncs(pubPath, freshPath, basePath)) > 0 {
			// Kind conflicts abort the whole overlay. Falling through would
			// copy the published file and put the published function back.
			return false, &bothSidesUnsafeError{msg: "refusing to apply: a shared function changed on both sides, but a const/var/type kind change in the same file blocks installing it\nthe published file was not used, because that would keep the published function body"}
		}
		return false, nil
	}
	err := overlayHandEditedDecls(pubPath, freshPath, basePath, destPath)
	var conflict *bothSidesConflictError
	if errors.As(err, &conflict) {
		return true, conflict
	}
	if err != nil {
		return false, nil
	}
	return true, nil
}

func shouldOverlayOntoFresh(rel, pubPath, freshPath string) bool {
	if _, ok := novelOnlyEditableHookPaths[filepath.ToSlash(rel)]; ok {
		return false
	}
	for _, path := range []string{pubPath, freshPath} {
		data, err := os.ReadFile(path)
		if err == nil && hasGeneratedMarkerBytes(data) {
			return true
		}
	}
	return false
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
	plans := map[string]bothSidesFuncPlan{}
	var conflictList []DeclConflict
	for _, plan := range planBothSidesFuncs(pubPath, freshPath, basePath) {
		plans[plan.name] = plan
	}
	var installed []dst.Decl
	var pendingImports []dst.Spec
	// Chosen aliases stay here until the loop finishes so a later function
	// cannot reuse one for a different package. The file's decl list is
	// unchanged until then.
	reservedImports := importAliasMap(freshFile)
	for i, d := range freshFile.Decls {
		name := overlayDeclName(d)
		if !fromPub[name] {
			continue
		}
		if plan, ok := plans[name]; ok {
			if plan.isConflict {
				conflictList = append(conflictList, plan.conflict)
				continue
			}
			decl, err := mergedDeclFromText(plan.merged)
			if err != nil || overlayDeclName(decl) != name {
				c := DeclConflict{Name: name, Fresh: clipDeclSnippet(plan.merged)}
				conflictList = append(conflictList, c)
				continue
			}
			specs, err := bindStableImportSelectors(decl, freshFile, pubFile, basePath, reservedImports)
			if err != nil {
				c := DeclConflict{Name: name, Fresh: clipDeclSnippet(plan.merged)}
				conflictList = append(conflictList, c)
				continue
			}
			freshFile.Decls[i] = decl
			pendingImports = append(pendingImports, specs...)
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
	freshByName := overlayDeclMap(freshFile)
	for _, d := range pubFile.Decls {
		name := overlayDeclName(d)
		if !fromPub[name] {
			continue
		}
		if _, exists := freshByName[name]; exists {
			continue
		}
		freshFile.Decls = append(freshFile.Decls, d)
		installed = append(installed, d)
		freshByName[name] = d
	}
	installImportSpecs(freshFile, pendingImports)
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
	if err := writeFileAtomic(destPath, formatted); err != nil {
		return err
	}
	return conflictError(conflictList)
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
		if inFresh && pubText == freshText {
			continue
		}
		if !inFresh {
			if baseTexts != nil && baseTexts[name] != "" && pubText == baseTexts[name] {
				continue
			}
			out[name] = true
			continue
		}
		if baseTexts != nil && baseTexts[name] != "" && pubText == baseTexts[name] {
			continue
		}
		if baseTexts == nil && isOverlayFuncDeclName(name) {
			continue
		}
		out[name] = true
	}
	return out
}

func isOverlayFuncDeclName(name string) bool {
	return !strings.HasPrefix(name, "const:") &&
		!strings.HasPrefix(name, "var:") &&
		!strings.HasPrefix(name, "type:")
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
	var body []dst.Decl
	for _, d := range file.Decls {
		if gd, ok := d.(*dst.GenDecl); ok && gd.Tok == token.IMPORT {
			continue
		}
		body = append(body, d)
	}
	usedAlias := map[string]bool{}
	for _, d := range body {
		forEachPackageSelector(d, func(id *dst.Ident) {
			usedAlias[id.Name] = true
		})
	}

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
			alias := importSpecAlias(is)
			if alias == "_" || alias == "." || usedAlias[alias] {
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

// stableImportIdent is the same Go identifier for an import path on every
// side of a merge, so one local alias cannot stand for two packages.
func stableImportIdent(quotedPath string) string {
	sum := sha256.Sum256([]byte(quotedPath))
	return "ppimp" + hex.EncodeToString(sum[:6])
}

func loadImportAliases(path string) map[string]string {
	file, err := parseOverlayFile(path)
	if err != nil {
		return nil
	}
	return importAliasMap(file)
}

func fileUsesDotImport(path string) bool {
	file, err := parseOverlayFile(path)
	if err != nil {
		return false
	}
	for _, d := range file.Decls {
		gd, ok := d.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		for _, spec := range gd.Specs {
			is, ok := spec.(*dst.ImportSpec)
			if ok && is.Name != nil && is.Name.Name == "." {
				return true
			}
		}
	}
	return false
}

func parseOverlayFile(path string) (*dst.File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decorator.NewDecorator(nil).ParseFile(path, data, parser.ParseComments)
}

func qualifyFuncSelectors(funcText string, aliasToPath map[string]string) (string, error) {
	if len(aliasToPath) == 0 {
		return funcText, nil
	}
	decl, err := mergedDeclFromText(funcText)
	if err != nil {
		return "", err
	}
	changed := false
	forEachPackageSelector(decl, func(id *dst.Ident) {
		quoted, ok := aliasToPath[id.Name]
		if !ok {
			return
		}
		id.Name = stableImportIdent(quoted)
		changed = true
	})
	if !changed {
		return funcText, nil
	}
	return renderDSTDecl(decl)
}

func renderDSTDecl(decl dst.Decl) (string, error) {
	file := &dst.File{Name: dst.NewIdent("mergeddecl"), Decls: []dst.Decl{decl}}
	var buf bytes.Buffer
	if err := decorator.Fprint(&buf, file); err != nil {
		return "", err
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		formatted = buf.Bytes()
	}
	text := string(formatted)
	if nl := strings.IndexByte(text, '\n'); nl >= 0 {
		text = text[nl+1:]
	}
	return strings.TrimSpace(text), nil
}

// bindStableImportSelectors rewrites merge idents back to the destination
// file's aliases and adds any import the merged function still needs.
func bindStableImportSelectors(decl dst.Decl, dest, pub *dst.File, basePath string, reserved map[string]string) ([]dst.Spec, error) {
	if decl == nil || dest == nil {
		return nil, nil
	}
	known := map[string]string{}
	remember := func(file *dst.File) {
		for _, quoted := range importAliasMap(file) {
			known[stableImportIdent(quoted)] = quoted
		}
	}
	remember(dest)
	remember(pub)
	// Copy so a failed bind does not reserve names the file never installs.
	destAlias := cloneAliasMap(reserved)
	pubAlias := importAliasMap(pub)
	var baseAlias map[string]string
	if basePath != "" {
		if base, err := parseOverlayFile(basePath); err == nil {
			remember(base)
			baseAlias = importAliasMap(base)
		}
	}
	// One alias per package has to be legal at every call to that package.
	// A local that is not in scope at any of those calls does not rule it out.
	topLevel := fileTopLevelNames(dest)
	type selectorUse struct {
		id      *dst.Ident
		blocked map[string]bool
	}
	uses := map[string][]selectorUse{}
	unknown := false
	forEachPackageSelectorScoped(decl, func(id *dst.Ident, sc *bindingScopes) {
		quoted, ok := known[id.Name]
		if !ok {
			if strings.HasPrefix(id.Name, "ppimp") {
				unknown = true
			}
			return
		}
		blocked := map[string]bool{}
		if sc != nil {
			for _, frame := range sc.stack {
				for name := range frame {
					blocked[name] = true
				}
			}
		}
		for name := range topLevel {
			blocked[name] = true
		}
		uses[quoted] = append(uses[quoted], selectorUse{id: id, blocked: blocked})
	})
	if unknown {
		return nil, fmt.Errorf("merged function uses an unresolved import alias")
	}
	var missing []string
	chosen := map[string]string{}
	paths := make([]string, 0, len(uses))
	for quoted := range uses {
		paths = append(paths, quoted)
	}
	sort.Strings(paths)
	for _, quoted := range paths {
		blocked := map[string]bool{}
		for _, use := range uses[quoted] {
			for name := range use.blocked {
				blocked[name] = true
			}
		}
		alias := chooseImportAlias(quoted, destAlias, pubAlias, baseAlias, blocked)
		if destAlias[alias] != quoted {
			destAlias[alias] = quoted
			missing = append(missing, quoted)
			chosen[quoted] = alias
		}
		for _, use := range uses[quoted] {
			use.id.Name = alias
		}
	}
	maps.Copy(reserved, destAlias)
	if len(missing) == 0 {
		return nil, nil
	}
	sort.Strings(missing)
	specs := make([]dst.Spec, 0, len(missing))
	for _, quoted := range missing {
		specs = append(specs, importSpecWithAlias(&dst.ImportSpec{
			Path: &dst.BasicLit{Kind: token.STRING, Value: quoted},
		}, chosen[quoted]))
	}
	return specs, nil
}

func cloneAliasMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}

// installImportSpecs adds imports after declaration replacement. Doing it
// inside that loop would shift the indexes the loop still assigns.
func installImportSpecs(file *dst.File, specs []dst.Spec) {
	if file == nil || len(specs) == 0 {
		return
	}
	seen := map[string]bool{}
	var uniq []dst.Spec
	for _, spec := range specs {
		is, ok := spec.(*dst.ImportSpec)
		if !ok || is.Path == nil {
			continue
		}
		key := importSpecAlias(is) + " " + is.Path.Value
		if seen[key] {
			continue
		}
		seen[key] = true
		uniq = append(uniq, spec)
	}
	if len(uniq) == 0 {
		return
	}
	for _, d := range file.Decls {
		gd, ok := d.(*dst.GenDecl)
		if !ok || gd.Tok != token.IMPORT {
			continue
		}
		gd.Specs = append(gd.Specs, uniq...)
		return
	}
	file.Decls = append([]dst.Decl{&dst.GenDecl{Tok: token.IMPORT, Specs: uniq}}, file.Decls...)
}

func chooseImportAlias(quoted string, destAlias, pubAlias, baseAlias map[string]string, blocked map[string]bool) string {
	var existing []string
	for alias, path := range destAlias {
		if path == quoted && !blocked[alias] {
			existing = append(existing, alias)
		}
	}
	sort.Strings(existing)
	if len(existing) > 0 {
		return existing[0]
	}
	for _, alias := range append(aliasesForPath(pubAlias, quoted), aliasesForPath(baseAlias, quoted)...) {
		if alias == "" || alias == "_" || alias == "." || blocked[alias] {
			continue
		}
		if bound, ok := destAlias[alias]; !ok || bound == quoted {
			return alias
		}
	}
	return unusedImportAliasAvoiding(destAlias, importPathDefaultAlias(quoted), quoted, blocked)
}

func aliasesForPath(aliasToPath map[string]string, quoted string) []string {
	var out []string
	for alias, path := range aliasToPath {
		if path == quoted {
			out = append(out, alias)
		}
	}
	sort.Strings(out)
	return out
}

func unusedImportAliasAvoiding(bound map[string]string, preferred, path string, blocked map[string]bool) string {
	if preferred != "" && !blocked[preferred] && (bound[preferred] == "" || bound[preferred] == path) {
		return preferred
	}
	def := importPathDefaultAlias(path)
	if def != "" && !blocked[def] && (bound[def] == "" || bound[def] == path) {
		return def
	}
	if def == "" {
		def = "pkg"
	}
	for i := 2; ; i++ {
		cand := def + strconv.Itoa(i)
		if blocked[cand] || bound[cand] != "" {
			continue
		}
		return cand
	}
}

func fileTopLevelNames(file *dst.File) map[string]bool {
	out := map[string]bool{}
	if file == nil {
		return out
	}
	for _, d := range file.Decls {
		switch x := d.(type) {
		case *dst.FuncDecl:
			if x.Recv == nil && x.Name != nil && x.Name.Name != "_" {
				out[x.Name.Name] = true
			}
		case *dst.GenDecl:
			for _, spec := range x.Specs {
				switch s := spec.(type) {
				case *dst.ValueSpec:
					for _, n := range s.Names {
						if n.Name != "_" {
							out[n.Name] = true
						}
					}
				case *dst.TypeSpec:
					if s.Name != nil && s.Name.Name != "_" {
						out[s.Name.Name] = true
					}
				}
			}
		}
	}
	return out
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
	importPath, err := strconv.Unquote(quotedPath)
	if err != nil {
		return ""
	}
	return assumedImportName(importPath)
}

func assumedImportName(importPath string) string {
	base := path.Base(importPath)
	if strings.HasPrefix(base, "v") {
		if _, err := strconv.Atoi(base[1:]); err == nil {
			dir := path.Dir(importPath)
			if dir != "." {
				base = path.Base(dir)
			}
		}
	}
	base = strings.TrimPrefix(base, "go-")
	if i := strings.IndexFunc(base, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	}); i >= 0 {
		base = base[:i]
	}
	return base
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

func walkFieldListTypes(fl *dst.FieldList, sc *bindingScopes, visit func(*dst.Ident, *bindingScopes)) {
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
	return !sc.isLocal(id.Name)
}

func forEachPackageSelector(d dst.Decl, visit func(*dst.Ident)) {
	if visit == nil {
		return
	}
	forEachPackageSelectorScoped(d, func(id *dst.Ident, _ *bindingScopes) {
		visit(id)
	})
}

func forEachPackageSelectorScoped(d dst.Decl, visit func(*dst.Ident, *bindingScopes)) {
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
			visit(id, nil)
		}
		return true
	})
}

func walkOverlayStmt(s dst.Stmt, sc *bindingScopes, visit func(*dst.Ident, *bindingScopes)) {
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
			switch s := spec.(type) {
			case *dst.ValueSpec:
				walkOverlayExpr(s.Type, sc, visit)
				for _, e := range s.Values {
					walkOverlayExpr(e, sc, visit)
				}
				for _, n := range s.Names {
					sc.bind(n.Name)
				}
			case *dst.TypeSpec:
				// The type name is in scope in its own definition, so a
				// field like pkgos.FileInfo must not be rewritten to that name.
				if s.Name != nil {
					sc.bind(s.Name.Name)
				}
				walkFieldListTypes(s.TypeParams, sc, visit)
				walkOverlayExpr(s.Type, sc, visit)
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
		walkTypeSwitchAssignRHS(x.Assign, sc, visit)
		for _, cc := range typeSwitchCaseClauses(x.Body) {
			for _, e := range cc.List {
				walkOverlayExpr(e, sc, visit)
			}
		}
		bindTypeSwitchAssign(x.Assign, sc)
		for _, cc := range typeSwitchCaseClauses(x.Body) {
			sc.push()
			for _, c := range cc.Body {
				walkOverlayStmt(c, sc, visit)
			}
			sc.pop()
		}
		sc.pop()
	case *dst.SelectStmt:
		walkOverlayStmt(x.Body, sc, visit)
	case *dst.CaseClause:
		sc.push()
		for _, e := range x.List {
			walkOverlayExpr(e, sc, visit)
		}
		for _, c := range x.Body {
			walkOverlayStmt(c, sc, visit)
		}
		sc.pop()
	case *dst.CommClause:
		sc.push()
		walkOverlayStmt(x.Comm, sc, visit)
		for _, c := range x.Body {
			walkOverlayStmt(c, sc, visit)
		}
		sc.pop()
	case *dst.LabeledStmt:
		walkOverlayStmt(x.Stmt, sc, visit)
	case *dst.SendStmt:
		walkOverlayExpr(x.Chan, sc, visit)
		walkOverlayExpr(x.Value, sc, visit)
	case *dst.IncDecStmt:
		walkOverlayExpr(x.X, sc, visit)
	}
}

func walkTypeSwitchAssignRHS(s dst.Stmt, sc *bindingScopes, visit func(*dst.Ident, *bindingScopes)) {
	switch x := s.(type) {
	case *dst.AssignStmt:
		for _, e := range x.Rhs {
			walkOverlayExpr(e, sc, visit)
		}
	case *dst.ExprStmt:
		walkOverlayExpr(x.X, sc, visit)
	default:
		walkOverlayStmt(s, sc, visit)
	}
}

func bindTypeSwitchAssign(s dst.Stmt, sc *bindingScopes) {
	as, ok := s.(*dst.AssignStmt)
	if !ok || as.Tok != token.DEFINE {
		return
	}
	for _, e := range as.Lhs {
		if id, ok := e.(*dst.Ident); ok {
			sc.bind(id.Name)
		}
	}
}

func typeSwitchCaseClauses(body *dst.BlockStmt) []*dst.CaseClause {
	if body == nil {
		return nil
	}
	var out []*dst.CaseClause
	for _, s := range body.List {
		if cc, ok := s.(*dst.CaseClause); ok {
			out = append(out, cc)
		}
	}
	return out
}

func walkOverlayExpr(e dst.Expr, sc *bindingScopes, visit func(*dst.Ident, *bindingScopes)) {
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
				visit(id, sc)
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
