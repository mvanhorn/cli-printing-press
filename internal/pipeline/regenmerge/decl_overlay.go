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
	for i, d := range freshFile.Decls {
		name := overlayDeclName(d)
		if !fromPub[name] {
			continue
		}
		repl, ok := pubByName[name]
		if !ok {
			continue
		}
		freshFile.Decls[i] = repl
	}
	addMissingImports(freshFile, pubFile)

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
		return decl.Tok.String() + ":" + joinOverlayNames(names)
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

func joinOverlayNames(names []string) string {
	out := names[0]
	for i := 1; i < len(names); i++ {
		out += "," + names[i]
	}
	return out
}

func addMissingImports(fresh, pub *dst.File) {
	if fresh == nil || pub == nil {
		return
	}
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
			if have[is.Path.Value] {
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
