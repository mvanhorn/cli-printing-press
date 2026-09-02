package mcpsync

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// mcpRuntimeSnapshot holds pre-sync copies of MCP files that may carry
// hand-authored behavior. Description overrides still flow through
// GenerateMCPSurface into tools.go; these snapshots are restored or
// merged afterward so a description refresh cannot delete poll loops,
// annotation rules, destination-flag blocklists, or HTTP timeouts.
type mcpRuntimeSnapshot struct {
	files map[string][]byte
}

var (
	handAuthoredDBFlag     = regexp.MustCompile(`"db"\s*:\s*true`)
	handAuthoredIntentMark = regexp.MustCompile(`\b(waitForIntentPoll|intentDuration|pollAfter|operationTimeout)\b`)
)

func snapshotMCPRuntime(cliDir string) (*mcpRuntimeSnapshot, error) {
	snap := &mcpRuntimeSnapshot{files: map[string][]byte{}}
	if err := snapshotDirGoFiles(cliDir, snap, filepath.Join("internal", "mcp", "cobratree")); err != nil {
		return nil, err
	}
	if err := snapshotExistingFile(cliDir, snap, filepath.Join("internal", "mcp", "intents.go")); err != nil {
		return nil, err
	}
	matches, err := filepath.Glob(filepath.Join(cliDir, "cmd", "*-pp-mcp", "main.go"))
	if err != nil {
		return nil, fmt.Errorf("listing MCP entrypoints: %w", err)
	}
	for _, abs := range matches {
		rel, err := filepath.Rel(cliDir, abs)
		if err != nil {
			return nil, err
		}
		if err := snapshotExistingFile(cliDir, snap, rel); err != nil {
			return nil, err
		}
	}
	return snap, nil
}

func snapshotDirGoFiles(cliDir string, snap *mcpRuntimeSnapshot, relDir string) error {
	entries, err := os.ReadDir(filepath.Join(cliDir, relDir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading %s: %w", relDir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		if err := snapshotExistingFile(cliDir, snap, filepath.Join(relDir, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func snapshotExistingFile(cliDir string, snap *mcpRuntimeSnapshot, rel string) error {
	data, err := os.ReadFile(filepath.Join(cliDir, rel))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading %s: %w", rel, err)
	}
	snap.files[filepath.ToSlash(rel)] = data
	return nil
}

func restoreMCPRuntime(cliDir string, snap *mcpRuntimeSnapshot) error {
	if snap == nil {
		return nil
	}
	for rel, before := range snap.files {
		abs := filepath.Join(cliDir, filepath.FromSlash(rel))
		if isMCPIntentFile(rel) {
			after, err := os.ReadFile(abs)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					if err := writeFileAtomic(abs, before); err != nil {
						return err
					}
					continue
				}
				return fmt.Errorf("reading regenerated %s: %w", rel, err)
			}
			merged, err := mergeHandAuthoredIntentFile(before, after)
			if err != nil {
				return fmt.Errorf("merging hand-authored intents in %s: %w", rel, err)
			}
			if err := writeFileAtomic(abs, merged); err != nil {
				return err
			}
			continue
		}
		if !hasHandAuthoredMCPBehavior(before) {
			continue
		}
		if err := writeFileAtomic(abs, before); err != nil {
			return err
		}
	}
	return nil
}

func isMCPIntentFile(rel string) bool {
	return filepath.ToSlash(rel) == "internal/mcp/intents.go"
}

func hasHandAuthoredMCPBehavior(src []byte) bool {
	if handAuthoredIntentMark.Match(src) {
		return true
	}
	if bytes.Contains(src, []byte("func isMCPExecutionReadOnly(")) {
		return true
	}
	if bytes.Contains(src, []byte("localWrite := isMCPLocalWrite")) {
		return true
	}
	if bytes.Contains(src, []byte("ReadHeaderTimeout")) {
		return true
	}
	return handAuthoredDBFlag.Match(src)
}

func mergeHandAuthoredIntentFile(before, after []byte) ([]byte, error) {
	if !hasHandAuthoredMCPBehavior(before) {
		return after, nil
	}
	fsetBefore := token.NewFileSet()
	fileBefore, err := parser.ParseFile(fsetBefore, "intents.go", before, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parsing pre-sync intents.go: %w", err)
	}
	fsetAfter := token.NewFileSet()
	fileAfter, err := parser.ParseFile(fsetAfter, "intents.go", after, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parsing regenerated intents.go: %w", err)
	}

	afterNames := map[string]bool{}
	for _, decl := range fileAfter.Decls {
		for _, name := range collectDeclNames(decl) {
			afterNames[name] = true
		}
	}

	merged := string(after)
	for _, decl := range fileBefore.Decls {
		if isImportDecl(decl) {
			continue
		}
		beforeSrc := nodeSource(fsetBefore, before, decl)
		names := collectDeclNames(decl)
		if declHasIntentMarkers(beforeSrc) && len(names) == 1 && afterNames[names[0]] {
			afterSrc, ok := declSourceByName(fsetAfter, after, fileAfter, names[0])
			if ok && afterSrc != beforeSrc {
				merged = strings.Replace(merged, afterSrc, beforeSrc, 1)
			}
			continue
		}
		allPresent := len(names) > 0
		for _, name := range names {
			if !afterNames[name] {
				allPresent = false
				break
			}
		}
		if !allPresent {
			merged = strings.TrimRight(merged, "\n") + "\n\n" + beforeSrc + "\n"
			for _, name := range names {
				afterNames[name] = true
			}
		}
	}

	for _, path := range importPaths(fileBefore) {
		merged = ensureImport(merged, path)
	}
	formatted, err := format.Source([]byte(merged))
	if err != nil {
		return nil, fmt.Errorf("formatting merged intents.go: %w", err)
	}
	return formatted, nil
}

func isImportDecl(decl ast.Decl) bool {
	gen, ok := decl.(*ast.GenDecl)
	return ok && gen.Tok == token.IMPORT
}

func collectDeclNames(decl ast.Decl) []string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		return []string{d.Name.Name}
	case *ast.GenDecl:
		var names []string
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				names = append(names, s.Name.Name)
			case *ast.ValueSpec:
				for _, name := range s.Names {
					names = append(names, name.Name)
				}
			}
		}
		return names
	}
	return nil
}

func declSourceByName(fset *token.FileSet, src []byte, file *ast.File, name string) (string, bool) {
	for _, decl := range file.Decls {
		for _, n := range collectDeclNames(decl) {
			if n == name {
				return nodeSource(fset, src, decl), true
			}
		}
	}
	return "", false
}

func nodeSource(fset *token.FileSet, src []byte, node ast.Node) string {
	start := fset.Position(node.Pos()).Offset
	end := fset.Position(node.End()).Offset
	if start < 0 || end > len(src) || start >= end {
		return ""
	}
	return string(src[start:end])
}

func declHasIntentMarkers(src string) bool {
	return handAuthoredIntentMark.MatchString(src)
}

func importPaths(file *ast.File) []string {
	var paths []string
	for _, imp := range file.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

func ensureImport(src, path string) string {
	quoted := strconv.Quote(path)
	if strings.Contains(src, quoted) {
		return src
	}
	block := regexp.MustCompile(`import \(\n`)
	if loc := block.FindStringIndex(src); loc != nil {
		return src[:loc[1]] + "\t" + quoted + "\n" + src[loc[1]:]
	}
	single := regexp.MustCompile(`(?m)^import .+\n`)
	if loc := single.FindStringIndex(src); loc != nil {
		return src[:loc[1]] + "import " + quoted + "\n" + src[loc[1]:]
	}
	pkg := regexp.MustCompile(`(?m)^package \w+\n`)
	if loc := pkg.FindStringIndex(src); loc != nil {
		return src[:loc[1]] + "\nimport " + quoted + "\n" + src[loc[1]:]
	}
	return src
}
