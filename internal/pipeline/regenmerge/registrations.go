package regenmerge

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// extractLostRegistrations walks both trees' internal/cli/ directories,
// collects every AddCommand call expression (against any receiver — not just
// rootCmd), and computes the lost set per host file: calls present in
// published but missing from fresh. Lost calls whose target constructor
// name doesn't exist in the fresh tree's internal/cli/ are flagged as
// `skipped_for_missing_referent` rather than included for injection.
//
// "Host file" is any internal/cli/*.go file in published that contains at
// least one AddCommand call (root.go, plus resource-parents like
// category.go).
func extractLostRegistrations(publishedDir, freshDir string) ([]LostRegistration, error) {
	pubCLIDir := filepath.Join(publishedDir, "internal", "cli")
	freshCLIDir := filepath.Join(freshDir, "internal", "cli")

	pubCalls, hostFiles, err := collectAddCommandCalls(pubCLIDir)
	if err != nil {
		return nil, fmt.Errorf("scanning published internal/cli: %w", err)
	}
	freshCalls, _, err := collectAddCommandCalls(freshCLIDir)
	if err != nil {
		return nil, fmt.Errorf("scanning fresh internal/cli: %w", err)
	}

	// Merged-tree decl-set for referent-existence checks: fresh's
	// internal/cli/ ∪ published novel files (those Apply preserves into
	// the merged tree). Without the union, novel-command constructors get
	// falsely flagged as missing.
	freshDeclNames, err := collectDeclsFromDir(freshCLIDir, false)
	if err != nil {
		return nil, fmt.Errorf("collecting fresh internal/cli decls: %w", err)
	}
	novelDecls, err := collectDeclsFromDir(pubCLIDir, true)
	if err != nil {
		return nil, fmt.Errorf("collecting published novel decls: %w", err)
	}
	for k := range novelDecls {
		freshDeclNames[k] = struct{}{}
	}

	// Group calls per host file. Lost-set: published-calls in this file
	// that aren't anywhere in fresh's calls (across all hosts).
	freshCallSet := map[string]struct{}{}
	for _, calls := range freshCalls {
		for _, c := range calls {
			freshCallSet[c.normalized] = struct{}{}
		}
	}

	var out []LostRegistration
	sort.Strings(hostFiles)
	for _, host := range hostFiles {
		var lost, skipped []string
		for _, call := range pubCalls[host] {
			if _, present := freshCallSet[call.normalized]; present {
				continue
			}
			// Referent check.
			if call.constructorName != "" {
				if _, ok := freshDeclNames[call.constructorName]; !ok {
					skipped = append(skipped, call.source)
					continue
				}
			}
			lost = append(lost, call.source)
		}
		if len(lost) == 0 && len(skipped) == 0 {
			continue
		}
		out = append(out, LostRegistration{
			HostFile:                  filepath.ToSlash(filepath.Join("internal", "cli", filepath.Base(host))),
			Calls:                     lost,
			SkippedForMissingReferent: skipped,
		})
	}
	return out, nil
}

// addCommandCall records an AddCommand call in a file: source representation,
// normalized form for set-comparison, and the inferred constructor name (so
// referent-existence can be checked in fresh).
type addCommandCall struct {
	source          string // pretty-printed call expression
	normalized      string // identical-ish form for diffing across files
	constructorName string // e.g. "newCanonicalCmd"; empty when arg shape is unrecognized
}

// collectAddCommandCalls walks all .go files under dir and collects calls of
// the form `<recv>.AddCommand(<arg>)`. Returns:
//   - calls: map of file path → list of calls in that file
//   - hostFiles: list of files that contain at least one such call
func collectAddCommandCalls(dir string) (map[string][]addCommandCall, []string, error) {
	calls := map[string][]addCommandCall{}
	var hosts []string

	entries, err := readDirAllowMissing(dir)
	if err != nil {
		return nil, nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			// A broken file shouldn't block the whole walk, but a silent
			// skip can corrupt the lost-set: if fresh's host fails to
			// parse, all pub calls become "lost" and get re-injected on
			// top of fresh's already-emitted calls. Warn loudly to stderr
			// so the user sees the parse error and can fix it.
			fmt.Fprintf(os.Stderr, "regen-merge: warning: skipping unparseable file %s: %v\n", path, err)
			continue
		}
		var fileCalls []addCommandCall
		var inspectErr error
		ast.Inspect(file, func(n ast.Node) bool {
			ce, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := ce.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil || sel.Sel.Name != "AddCommand" {
				return true
			}
			call, err := formatCallExpr(fset, ce)
			if err != nil {
				inspectErr = err
				return false
			}
			fileCalls = append(fileCalls, call)
			return true
		})
		if inspectErr != nil {
			return nil, nil, fmt.Errorf("formatting AddCommand calls in %s: %w", path, inspectErr)
		}
		if len(fileCalls) > 0 {
			calls[path] = fileCalls
			hosts = append(hosts, path)
		}
	}
	return calls, hosts, nil
}

// formatCallExpr renders an AddCommand call expression and extracts the
// constructor name (the function called as the AddCommand argument). Returns
// an error if the printer fails so an empty addCommandCall can never enter
// the call set (where it would corrupt set-comparison via a "" key).
func formatCallExpr(fset *token.FileSet, ce *ast.CallExpr) (addCommandCall, error) {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, ce); err != nil {
		return addCommandCall{}, fmt.Errorf("printing AddCommand call: %w", err)
	}
	src := buf.String()

	// Normalize: collapse internal whitespace into single spaces. This
	// makes `rootCmd.AddCommand(newX(flags))` and the same call across
	// formatting differences match for set-comparison.
	normalized := strings.Join(strings.Fields(src), " ")

	// Infer constructor name from the first argument: usually
	// `newX(args...)` — extract `newX`.
	var ctor string
	if len(ce.Args) > 0 {
		if argCall, ok := ce.Args[0].(*ast.CallExpr); ok {
			if id, ok := argCall.Fun.(*ast.Ident); ok {
				ctor = id.Name
			}
		}
	}

	return addCommandCall{source: src, normalized: normalized, constructorName: ctor}, nil
}

// collectDeclsFromDir walks dir's .go files (non-recursive) and returns the
// union of their top-level decl names. When skipTemplated is true, files
// carrying the "Generated by CLI Printing Press" marker are excluded — used
// by the published-novel side of the referent-existence check, where only
// files Apply preserves should contribute decls. When false, every .go file
// is included — used by the fresh side.
func collectDeclsFromDir(dir string, skipTemplated bool) (declSet, error) {
	out := declSet{}
	entries, err := readDirAllowMissing(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if skipTemplated && hasTemplatedMarker(path) {
			continue
		}
		decls, err := extractDecls(path)
		if err != nil {
			continue
		}
		for k := range decls {
			out[k] = struct{}{}
		}
	}
	return out, nil
}

// readDirAllowMissing returns the directory entries; treats a missing dir as
// empty rather than error (a CLI may not have an internal/cli/ at all).
func readDirAllowMissing(dir string) ([]fs.DirEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}
