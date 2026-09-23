package pipeline

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
)

// unstampedRuntimeVersionLiteral is the fresh-print sentinel. A stamped
// library entry must not be republished with this value.
const unstampedRuntimeVersionLiteral = `"0.0.0-dev"`

var cliVersionRelPaths = []string{
	filepath.Join("internal", "cli", "root.go"),
	filepath.Join("internal", "cli", "version.go"),
}

// PreserveStampedRuntimeVersion copies the base tree's runtime version
// declaration onto dest in the same file and with the same source line.
// The public-library release ledger treats any added or removed
// `var version =` line as a version change, so relocating an identical
// value still fails that guard.
func PreserveStampedRuntimeVersion(baseDir, destDir string) error {
	if strings.TrimSpace(baseDir) == "" {
		return nil
	}
	info, err := os.Stat(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("checking runtime version base: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("runtime version base %s is not a directory", baseDir)
	}
	cliLiteral, err := preserveCLIVersionLayout(baseDir, destDir)
	if err != nil {
		return err
	}
	return preserveMCPVersionLayout(baseDir, destDir, cliLiteral)
}

func preserveCLIVersionLayout(baseDir, destDir string) (string, error) {
	type located struct {
		rel  string
		decl versionDecl
	}
	locatedFiles := make([]located, 0, len(cliVersionRelPaths))
	anyPresent := false
	var stamped string
	for _, rel := range cliVersionRelPaths {
		basePath := filepath.Join(baseDir, rel)
		_, statErr := os.Stat(basePath)
		if statErr != nil && !os.IsNotExist(statErr) {
			return "", statErr
		}
		var decl versionDecl
		if statErr == nil {
			var err error
			decl, err = parseVersionDecl(basePath)
			if err != nil {
				return "", err
			}
			if decl.present {
				anyPresent = true
				if stamped == "" {
					stamped = decl.literal
				}
			}
		}
		locatedFiles = append(locatedFiles, located{rel: rel, decl: decl})
	}
	if !anyPresent {
		return "", nil
	}
	for _, lf := range locatedFiles {
		destPath := filepath.Join(destDir, lf.rel)
		if lf.decl.present {
			if err := ensureVersionDeclLine(destPath, lf.decl.line); err != nil {
				return "", err
			}
			continue
		}
		if err := removeVersionDeclFromFile(destPath); err != nil {
			return "", err
		}
	}
	return stamped, nil
}

func preserveMCPVersionLayout(baseDir, destDir, cliLiteral string) error {
	pattern := filepath.Join(destDir, "cmd", "*"+naming.MCPSuffix, "main.go")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return err
	}
	sort.Strings(matches)
	for _, destPath := range matches {
		rel, err := filepath.Rel(destDir, destPath)
		if err != nil {
			return err
		}
		basePath := filepath.Join(baseDir, rel)
		_, statErr := os.Stat(basePath)
		switch {
		case statErr == nil:
			decl, err := parseVersionDecl(basePath)
			if err != nil {
				return err
			}
			if decl.present {
				if err := ensureVersionDeclLine(destPath, decl.line); err != nil {
					return err
				}
				continue
			}
			literal := cliLiteral
			if literal == "" {
				destDecl, err := parseVersionDecl(destPath)
				if err != nil {
					return err
				}
				if !destDecl.present {
					continue
				}
				literal = destDecl.literal
			}
			if err := inlineVersionVar(destPath, literal); err != nil {
				return err
			}
		case os.IsNotExist(statErr):
			if cliLiteral == "" || cliLiteral == unstampedRuntimeVersionLiteral {
				continue
			}
			destDecl, err := parseVersionDecl(destPath)
			if err != nil {
				return err
			}
			if !destDecl.present || destDecl.literal != unstampedRuntimeVersionLiteral {
				continue
			}
			if err := replaceVersionLiteral(destPath, cliLiteral); err != nil {
				return err
			}
		default:
			return statErr
		}
	}
	return nil
}

type versionDecl struct {
	present bool
	literal string
	line    string
	lineIdx int
}

func parseVersionDecl(path string) (versionDecl, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return versionDecl{}, nil
		}
		return versionDecl{}, err
	}
	return parseVersionDeclSource(path, data)
}

func parseVersionDeclSource(filename string, data []byte) (versionDecl, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, data, parser.SkipObjectResolution)
	if err != nil {
		return versionDecl{}, fmt.Errorf("parsing %s: %w", filename, err)
	}
	lines, _, _ := splitSourceLines(data)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR || !genDeclHasVersion(gen) {
			continue
		}
		if len(gen.Specs) != 1 {
			return versionDecl{}, fmt.Errorf("%s var version declaration is not a single string assignment", filename)
		}
		valueSpec, ok := gen.Specs[0].(*ast.ValueSpec)
		if !ok || len(valueSpec.Names) != 1 || valueSpec.Names[0].Name != "version" {
			return versionDecl{}, fmt.Errorf("%s var version declaration is not a single string assignment", filename)
		}
		if len(valueSpec.Values) != 1 {
			return versionDecl{}, fmt.Errorf("%s var version is not a string literal", filename)
		}
		lit, ok := valueSpec.Values[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return versionDecl{}, fmt.Errorf("%s var version is not a string literal", filename)
		}
		start := fset.Position(gen.Pos())
		end := fset.Position(gen.End())
		if start.Line != end.Line {
			return versionDecl{}, fmt.Errorf("%s var version declaration spans multiple lines", filename)
		}
		idx := start.Line - 1
		if idx < 0 || idx >= len(lines) {
			return versionDecl{}, fmt.Errorf("%s var version line is out of range", filename)
		}
		return versionDecl{
			present: true,
			literal: lit.Value,
			line:    lines[idx],
			lineIdx: idx,
		}, nil
	}
	return versionDecl{}, nil
}

func genDeclHasVersion(gen *ast.GenDecl) bool {
	for _, spec := range gen.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for _, name := range valueSpec.Names {
			if name.Name == "version" {
				return true
			}
		}
	}
	return false
}

func ensureVersionDeclLine(path, exactLine string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("runtime version destination missing %s", path)
		}
		return err
	}
	decl, err := parseVersionDeclSource(path, data)
	if err != nil {
		return err
	}
	lines, nl, trailing := splitSourceLines(data)
	if decl.present {
		if lines[decl.lineIdx] == exactLine {
			return nil
		}
		lines[decl.lineIdx] = exactLine
		return writeSourceIfChanged(path, joinSourceLines(lines, nl, trailing))
	}
	after, err := insertionLineAfterImports(path, data)
	if err != nil {
		return err
	}
	lines = insertVersionLine(lines, after, exactLine)
	return writeSourceIfChanged(path, joinSourceLines(lines, nl, trailing))
}

func insertionLineAfterImports(filename string, data []byte) (int, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, data, parser.SkipObjectResolution)
	if err != nil {
		return 0, fmt.Errorf("parsing %s: %w", filename, err)
	}
	line := fset.Position(file.Name.End()).Line
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if ok && gen.Tok == token.IMPORT {
			if end := fset.Position(gen.End()).Line; end > line {
				line = end
			}
		}
	}
	return line, nil
}

func insertVersionLine(lines []string, after int, exact string) []string {
	idx := min(max(after, 0), len(lines))
	var extra []string
	if idx > 0 && strings.TrimSpace(lines[idx-1]) != "" {
		extra = append(extra, "")
	}
	extra = append(extra, exact)
	if idx < len(lines) && strings.TrimSpace(lines[idx]) != "" {
		extra = append(extra, "")
	}
	return insertStrings(lines, idx, extra...)
}

func removeVersionDeclFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	decl, err := parseVersionDeclSource(path, data)
	if err != nil || !decl.present {
		return err
	}
	lines, nl, trailing := splitSourceLines(data)
	drop := map[int]struct{}{decl.lineIdx: {}}
	if decl.lineIdx > 0 && isVersionComment(lines[decl.lineIdx-1]) {
		drop[decl.lineIdx-1] = struct{}{}
	}
	kept := make([]string, 0, len(lines))
	for i, line := range lines {
		if _, ok := drop[i]; ok {
			continue
		}
		kept = append(kept, line)
	}
	return writeSourceIfChanged(path, joinSourceLines(kept, nl, trailing))
}

func isVersionComment(line string) bool {
	trim := strings.TrimSpace(line)
	return strings.HasPrefix(trim, "//") && strings.Contains(strings.ToLower(trim), "version")
}

func replaceVersionLiteral(path, literal string) error {
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
		if !ok || gen.Tok != token.VAR || len(gen.Specs) != 1 {
			continue
		}
		valueSpec, ok := gen.Specs[0].(*ast.ValueSpec)
		if !ok || len(valueSpec.Names) != 1 || valueSpec.Names[0].Name != "version" || len(valueSpec.Values) != 1 {
			continue
		}
		lit, ok := valueSpec.Values[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return fmt.Errorf("%s var version is not a string literal", path)
		}
		if lit.Value == literal {
			return nil
		}
		start := fset.Position(lit.Pos()).Offset
		end := fset.Position(lit.End()).Offset
		next := append([]byte{}, data[:start]...)
		next = append(next, literal...)
		next = append(next, data[end:]...)
		return writeSourceIfChanged(path, next)
	}
	return fmt.Errorf("%s missing var version", path)
}

func inlineVersionVar(path, literal string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	decl, err := parseVersionDeclSource(path, data)
	if err != nil {
		return err
	}
	if !decl.present {
		return nil
	}
	refs, err := packageVersionRefOffsets(path, data)
	if err != nil {
		return err
	}
	if len(refs) > 0 {
		next := append([]byte{}, data...)
		sort.Sort(sort.Reverse(sort.IntSlice(refs)))
		for _, offset := range refs {
			next = append(next[:offset], append([]byte(literal), next[offset+len("version"):]...)...)
		}
		if err := writeSourceIfChanged(path, next); err != nil {
			return err
		}
	}
	return removeVersionDeclFromFile(path)
}

func packageVersionRefOffsets(filename string, data []byte) ([]int, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, data, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", filename, err)
	}

	declNameOffsets := map[int]struct{}{}
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
			for _, ident := range valueSpec.Names {
				if ident.Name == "version" {
					declNameOffsets[fset.Position(ident.Pos()).Offset] = struct{}{}
				}
			}
		}
	}
	if len(declNameOffsets) == 0 {
		return nil, nil
	}

	var references []int
	identDeclaresName := func(ident *ast.Ident) bool {
		return ident != nil && ident.Name == "version"
	}
	fieldListDeclaresName := func(fields *ast.FieldList) bool {
		if fields == nil {
			return false
		}
		for _, field := range fields.List {
			if slices.ContainsFunc(field.Names, identDeclaresName) {
				return true
			}
		}
		return false
	}
	exprsDeclareName := func(exprs []ast.Expr) bool {
		for _, expr := range exprs {
			if ident, ok := expr.(*ast.Ident); ok && identDeclaresName(ident) {
				return true
			}
		}
		return false
	}
	addReference := func(ident *ast.Ident, shadowed bool) {
		if shadowed || !identDeclaresName(ident) {
			return
		}
		offset := fset.Position(ident.Pos()).Offset
		if _, isDeclName := declNameOffsets[offset]; isDeclName {
			return
		}
		references = append(references, offset)
	}

	var collectBlock func(*ast.BlockStmt, bool)
	var collectStmt func(ast.Stmt, bool) bool
	var collectExpr func(ast.Expr, bool)
	collectExpr = func(expr ast.Expr, shadowed bool) {
		ast.Inspect(expr, func(node ast.Node) bool {
			switch n := node.(type) {
			case nil:
				return true
			case *ast.FuncLit:
				litShadowed := shadowed || fieldListDeclaresName(n.Type.Params) || fieldListDeclaresName(n.Type.Results)
				collectBlock(n.Body, litShadowed)
				return false
			case *ast.SelectorExpr:
				collectExpr(n.X, shadowed)
				return false
			case *ast.KeyValueExpr:
				if _, keyIsIdent := n.Key.(*ast.Ident); !keyIsIdent {
					collectExpr(n.Key, shadowed)
				}
				collectExpr(n.Value, shadowed)
				return false
			case *ast.Ident:
				addReference(n, shadowed)
			}
			return true
		})
	}
	collectExprs := func(exprs []ast.Expr, shadowed bool) {
		for _, expr := range exprs {
			collectExpr(expr, shadowed)
		}
	}

	collectBlock = func(block *ast.BlockStmt, shadowed bool) {
		if block == nil {
			return
		}
		blockShadowed := shadowed
		for _, stmt := range block.List {
			if collectStmt(stmt, blockShadowed) {
				blockShadowed = true
			}
		}
	}
	collectStmt = func(stmt ast.Stmt, shadowed bool) bool {
		switch s := stmt.(type) {
		case *ast.AssignStmt:
			collectExprs(s.Rhs, shadowed)
			return s.Tok == token.DEFINE && exprsDeclareName(s.Lhs)
		case *ast.DeclStmt:
			gen, ok := s.Decl.(*ast.GenDecl)
			if !ok {
				return false
			}
			declaresName := false
			for _, spec := range gen.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				collectExprs(valueSpec.Values, shadowed)
				for _, ident := range valueSpec.Names {
					if identDeclaresName(ident) {
						declaresName = true
					}
				}
			}
			return declaresName
		case *ast.ExprStmt:
			collectExpr(s.X, shadowed)
		case *ast.ReturnStmt:
			collectExprs(s.Results, shadowed)
		case *ast.IfStmt:
			stmtShadowed := shadowed
			if s.Init != nil && collectStmt(s.Init, shadowed) {
				stmtShadowed = true
			}
			collectExpr(s.Cond, stmtShadowed)
			collectBlock(s.Body, stmtShadowed)
			if s.Else != nil {
				switch elseNode := s.Else.(type) {
				case *ast.BlockStmt:
					collectBlock(elseNode, stmtShadowed)
				case *ast.IfStmt:
					collectStmt(elseNode, stmtShadowed)
				}
			}
		case *ast.ForStmt:
			stmtShadowed := shadowed
			if s.Init != nil && collectStmt(s.Init, shadowed) {
				stmtShadowed = true
			}
			if s.Cond != nil {
				collectExpr(s.Cond, stmtShadowed)
			}
			if s.Post != nil {
				collectStmt(s.Post, stmtShadowed)
			}
			collectBlock(s.Body, stmtShadowed)
		case *ast.RangeStmt:
			collectExpr(s.X, shadowed)
			bodyShadowed := shadowed
			if s.Tok == token.DEFINE && exprsDeclareName([]ast.Expr{s.Key, s.Value}) {
				bodyShadowed = true
			}
			collectBlock(s.Body, bodyShadowed)
		case *ast.BlockStmt:
			collectBlock(s, shadowed)
		case *ast.SwitchStmt:
			stmtShadowed := shadowed
			if s.Init != nil && collectStmt(s.Init, shadowed) {
				stmtShadowed = true
			}
			if s.Tag != nil {
				collectExpr(s.Tag, stmtShadowed)
			}
			collectBlock(s.Body, stmtShadowed)
		case *ast.GoStmt:
			collectExpr(s.Call, shadowed)
		case *ast.DeferStmt:
			collectExpr(s.Call, shadowed)
		}
		return false
	}

	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			shadowed := fieldListDeclaresName(d.Recv) ||
				fieldListDeclaresName(d.Type.Params) ||
				fieldListDeclaresName(d.Type.Results)
			collectBlock(d.Body, shadowed)
		case *ast.GenDecl:
			if d.Tok == token.VAR {
				for _, spec := range d.Specs {
					valueSpec, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					collectExprs(valueSpec.Values, false)
				}
			}
		}
	}
	return references, nil
}

func splitSourceLines(data []byte) (lines []string, nl string, trailing bool) {
	nl = "\n"
	s := string(data)
	if strings.Contains(s, "\r\n") {
		nl = "\r\n"
		s = strings.ReplaceAll(s, "\r\n", "\n")
	}
	trailing = strings.HasSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return []string{}, nl, trailing
	}
	return strings.Split(s, "\n"), nl, trailing
}

func joinSourceLines(lines []string, nl string, trailing bool) []byte {
	s := strings.Join(lines, nl)
	if trailing {
		s += nl
	}
	return []byte(s)
}

func insertStrings(lines []string, at int, extra ...string) []string {
	if at < 0 {
		at = 0
	}
	if at > len(lines) {
		at = len(lines)
	}
	out := make([]string, 0, len(lines)+len(extra))
	out = append(out, lines[:at]...)
	out = append(out, extra...)
	out = append(out, lines[at:]...)
	return out
}

func writeSourceIfChanged(path string, data []byte) error {
	existing, err := os.ReadFile(path)
	if err == nil && string(existing) == string(data) {
		return nil
	}
	mode := os.FileMode(0o644)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	return os.WriteFile(path, data, mode)
}
