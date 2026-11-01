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
		deadFuncs := findAllDeadFunctions(cliDir, cmdDir, filepath.Join(dir, "internal", "mcp"))
		if len(deadFuncs) == 0 {
			break
		}

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

		// For each dead function, find its file and remove it via AST
		removedByFile := map[string][]string{}
		for _, funcName := range deadFuncs {
			file, found := findFunctionFile(cliDir, funcName)
			if !found {
				continue
			}
			removedByFile[file] = append(removedByFile[file], funcName)
		}

		backups := map[string][]byte{}
		for file, funcs := range removedByFile {
			original, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			backups[file] = original

			if err := removeFunctionsFromFile(file, funcs); err != nil {
				_ = os.WriteFile(file, original, 0o644)
				return nil, fmt.Errorf("removing functions from %s: %w", filepath.Base(file), err)
			}

			result.Removed = append(result.Removed, funcs...)
		}

		// Clean up unused imports left by removal (e.g., if the dead function
		// was the only caller of a package). Run goimports before go build
		// so stale imports don't cause a false build failure.
		for file := range removedByFile {
			importsCmd := exec.Command("goimports", "-w", file)
			if importsErr := importsCmd.Run(); importsErr != nil {
				// goimports not installed — fall back to gofmt (won't fix imports
				// but at least formats). The go build safety net will catch
				// remaining import issues and restore.
				fmtCmd := exec.Command("gofmt", "-w", file)
				_ = fmtCmd.Run()
			}
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
			result.BuildVerified = false
			result.BuildError = strings.TrimSpace(string(buildOutput))
			result.Restored = append(result.Restored, deadFuncs...)
			// Remove the funcs we just tried from the removed list
			result.Removed = result.Removed[:len(result.Removed)-len(deadFuncs)]

			for file, original := range backups {
				_ = os.WriteFile(file, original, 0o644)
			}
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
	defFiles := listGoFiles(cliDir)
	if len(defFiles) == 0 {
		return nil
	}
	useFiles := append(append([]string{}, defFiles...), listGoTestFiles(cliDir)...)
	for _, dir := range extraSearchDirs {
		useFiles = append(useFiles, listGoFilesRecursive(dir)...)
	}
	_, dead := scanFuncUses(defFiles, useFiles)
	return dead
}

// findFunctionFile searches all .go files in dir for a top-level function definition.
func findFunctionFile(dir, funcName string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}

	needle := "func " + funcName + "("
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), needle) {
			return path, true
		}
	}
	return "", false
}

// removeFunctionsFromFile parses a Go file and removes the named functions,
// including their associated doc comments.
func removeFunctionsFromFile(path string, funcNames []string) error {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	nameSet := map[string]bool{}
	for _, name := range funcNames {
		nameSet[name] = true
	}

	// Collect doc comment positions for dead functions so we can prune them
	deadDocComments := map[*ast.CommentGroup]bool{}
	var filtered []ast.Decl
	for _, decl := range node.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && nameSet[fn.Name.Name] {
			// Mark the doc comment for removal
			if fn.Doc != nil {
				deadDocComments[fn.Doc] = true
			}
			continue
		}
		filtered = append(filtered, decl)
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
		return fmt.Errorf("creating %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	cfg := &printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 8}
	if err := cfg.Fprint(f, fset, node); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}
