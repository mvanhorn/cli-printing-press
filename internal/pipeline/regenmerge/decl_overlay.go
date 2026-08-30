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
			missing = append(missing, is)
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
	path, err := strconv.Unquote(is.Path.Value)
	if err != nil {
		return ""
	}
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func importPathsUsedByDecls(decls []dst.Decl, aliasToPath map[string]string) map[string]bool {
	used := map[string]bool{}
	for _, d := range decls {
		dst.Inspect(d, func(n dst.Node) bool {
			sel, ok := n.(*dst.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*dst.Ident)
			if !ok {
				return true
			}
			if path, ok := aliasToPath[id.Name]; ok {
				used[path] = true
			}
			return true
		})
	}
	return used
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
