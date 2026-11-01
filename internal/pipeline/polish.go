package pipeline

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// PolishResult describes what the polish pass did.
type PolishResult struct {
	Dir           string   `json:"dir"`
	DeadFunctions []string `json:"dead_functions_found"`
	Removed       []string `json:"removed"`
	BuildVerified bool     `json:"build_verified"`
	BuildError    string   `json:"build_error,omitempty"`
	Restored      []string `json:"restored,omitempty"`
	DryRun        bool     `json:"dry_run"`
}

// RemoveDeadCode finds dead functions across ALL CLI files, then AST-removes them.
// Loops until no dead functions remain (max 3 passes to catch chained dead code).
// If dryRun is true, reports what would be removed without modifying files (1 pass only).
func RemoveDeadCode(dir string, dryRun bool) (*PolishResult, error) {
	return removeDeadCode(dir, dryRun, nil)
}

// locate overrides the scan. Nil uses each dead definition's file from the
// type-checked scan, so removal does not search for the text `func name(`.
func removeDeadCode(dir string, dryRun bool, locate func(cliDir string, extraSearchDirs ...string) []deadFuncLoc) (*PolishResult, error) {
	if locate == nil {
		locate = findDeadFuncLocs
	}
	result := &PolishResult{
		Dir:    dir,
		DryRun: dryRun,
	}

	cliDir := filepath.Join(dir, "internal", "cli")
	cmdDir := filepath.Join(dir, "cmd")

	// Validate the directory is a CLI project
	if _, err := os.Stat(cliDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("%s is not a valid CLI directory (missing internal/cli/)", dir)
	}

	const maxPasses = 3
	for pass := range maxPasses {
		locs := locate(cliDir, cmdDir, filepath.Join(dir, "internal", "mcp"))
		if len(locs) == 0 {
			break
		}
		deadFuncs := deadFuncNames(locs)

		// First pass populates DeadFunctions for reporting
		if pass == 0 {
			result.DeadFunctions = deadFuncs
		} else {
			result.DeadFunctions = append(result.DeadFunctions, deadFuncs...)
		}

		if dryRun {
			// Dry-run only reports the first pass — can't predict cascading removals
			return result, nil
		}

		removedThisPass, skipped, backups, err := removeDeadLocs(locs)
		if err != nil {
			return nil, err
		}
		if len(removedThisPass) == 0 {
			// Every candidate was skipped. Another pass would report the same
			// set and a true BuildVerified would hide functions still in the tree.
			result.BuildVerified = false
			result.BuildError = unremovedDeadFuncsError(skipped)
			return result, nil
		}

		// Verify build after each pass.
		buildCmd := exec.Command("go", "build", "./...")
		buildCmd.Dir = dir
		buildOutput, buildErr := buildCmd.CombinedOutput()

		// Test binaries must compile after removal, but they must not execute:
		// generated suites can require live credentials or other runtime setup.
		if buildErr == nil {
			if out, err := compileTestBinaries(dir); err != nil {
				buildErr = err
				if len(out) == 0 {
					out = []byte(err.Error())
				}
				buildOutput = out
			}
		}

		if buildErr != nil {
			// Put the files back before recording the pass. Restored lists
			// only functions this pass actually deleted; unlocated candidates
			// were never written and must not be subtracted from Removed.
			restoreFileBackups(backups)
			result.BuildVerified = false
			result.BuildError = strings.TrimSpace(string(buildOutput))
			result.Restored = append(result.Restored, removedThisPass...)
			return result, nil
		}

		result.Removed = append(result.Removed, removedThisPass...)
		if len(skipped) > 0 {
			result.BuildVerified = false
			result.BuildError = unremovedDeadFuncsError(skipped)
			return result, nil
		}

		// goimports already ran above — no need for a separate gofmt pass
	}

	result.BuildVerified = true
	return result, nil
}

func compileTestBinaries(dir string) ([]byte, error) {
	outputDir, err := os.MkdirTemp("", "printing-press-polish-tests-*")
	if err != nil {
		return nil, fmt.Errorf("creating test build directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(outputDir) }()

	testCmd := exec.Command("go", "test", "-c", "-o", outputDir, "./...")
	testCmd.Dir = dir
	return testCmd.CombinedOutput()
}

// findAllDeadFunctions reports top-level functions defined under cliDir that
// have no identifier use in cliDir (including _test.go) or extraSearchDirs.
// Test files are uses only: their helpers are not removal candidates, but a
// function whose only callers are tests is live.
func findAllDeadFunctions(cliDir string, extraSearchDirs ...string) []string {
	return deadFuncNames(findDeadFuncLocs(cliDir, extraSearchDirs...))
}

// findFunctionFile searches .go files in dir for a top-level function
// declaration. Matching the function name, not the text `func name(`, keeps
// generic and multiline declarations visible to callers.
func findFunctionFile(dir, funcName string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil || node == nil {
			continue
		}
		for _, decl := range node.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || fn.Name == nil {
				continue
			}
			if fn.Name.Name == funcName {
				return path, true
			}
		}
	}
	return "", false
}

func removeDeadLocs(locs []deadFuncLoc) (removedThisPass, skipped []string, backups map[string][]byte, err error) {
	backups = map[string][]byte{}
	batches := groupDeadLocs(locs)
	var changed []string
	for _, batch := range batches {
		if batch.file == "" {
			skipped = append(skipped, batch.names...)
			continue
		}
		original, readErr := os.ReadFile(batch.file)
		if readErr != nil {
			skipped = append(skipped, batch.names...)
			continue
		}
		removedNames, removeErr := removeFunctionsFromFile(batch.file, batch.names)
		if removeErr != nil {
			backups[batch.file] = original
			restoreFileBackups(backups)
			return nil, nil, nil, fmt.Errorf("removing functions from %s: %w", filepath.Base(batch.file), removeErr)
		}
		if len(removedNames) == 0 {
			skipped = append(skipped, batch.names...)
			continue
		}
		backups[batch.file] = original
		changed = append(changed, batch.file)
		removedThisPass = append(removedThisPass, removedNames...)
		got := map[string]struct{}{}
		for _, name := range removedNames {
			got[name] = struct{}{}
		}
		for _, name := range batch.names {
			if _, ok := got[name]; !ok {
				skipped = append(skipped, name)
			}
		}
	}

	// Clean up unused imports left by removal (e.g., if the dead function
	// was the only caller of a package). Run goimports before go build
	// so stale imports don't cause a false build failure.
	for _, file := range changed {
		importsCmd := exec.Command("goimports", "-w", file)
		if importsErr := importsCmd.Run(); importsErr != nil {
			// goimports not installed — fall back to gofmt (won't fix imports
			// but at least formats). The go build safety net will catch
			// remaining import issues and restore.
			fmtCmd := exec.Command("gofmt", "-w", file)
			_ = fmtCmd.Run()
		}
	}
	return removedThisPass, skipped, backups, nil
}

type deadFuncBatch struct {
	file  string
	names []string
}

func groupDeadLocs(locs []deadFuncLoc) []deadFuncBatch {
	var batches []deadFuncBatch
	index := map[string]int{}
	for _, loc := range locs {
		key := loc.file
		i, ok := index[key]
		if !ok {
			index[key] = len(batches)
			batches = append(batches, deadFuncBatch{file: loc.file})
			i = index[key]
		}
		batches[i].names = append(batches[i].names, loc.name)
	}
	return batches
}

func restoreFileBackups(backups map[string][]byte) {
	for file, original := range backups {
		_ = os.WriteFile(file, original, 0o644)
	}
}

func unremovedDeadFuncsError(skipped []string) string {
	seen := map[string]struct{}{}
	var names []string
	for _, name := range skipped {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		return "dead functions were not removed"
	}
	sort.Strings(names)
	return "dead functions not removed: " + strings.Join(names, ", ")
}

// removeFunctionsFromFile parses a Go file and removes the named top-level
// functions, including their doc comments. Methods are kept: a receiver
// means the name is not the dead top-level function.
func removeFunctionsFromFile(path string, funcNames []string) ([]string, error) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	nameSet := map[string]bool{}
	for _, name := range funcNames {
		nameSet[name] = true
	}

	// Collect doc comment positions for dead functions so we can prune them
	deadDocComments := map[*ast.CommentGroup]bool{}
	var removed []string
	var filtered []ast.Decl
	for _, decl := range node.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && fn.Name != nil && nameSet[fn.Name.Name] {
			removed = append(removed, fn.Name.Name)
			if fn.Doc != nil {
				deadDocComments[fn.Doc] = true
			}
			continue
		}
		filtered = append(filtered, decl)
	}
	if len(removed) == 0 {
		return nil, nil
	}
	node.Decls = filtered

	// Prune orphaned doc comments from the comment map
	if len(deadDocComments) > 0 {
		var kept []*ast.CommentGroup
		for _, cg := range node.Comments {
			if !deadDocComments[cg] {
				kept = append(kept, cg)
			}
		}
		node.Comments = kept
	}

	// Write back
	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("creating %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	cfg := &printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(f, fset, node); err != nil {
		return nil, fmt.Errorf("writing %s: %w", path, err)
	}

	return removed, nil
}
