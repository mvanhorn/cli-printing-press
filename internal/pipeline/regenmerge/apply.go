package regenmerge

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"golang.org/x/mod/modfile"

	"github.com/mvanhorn/cli-printing-press/v3/internal/pipeline"
)

// Apply executes a MergeReport's plan against the published CLI directory,
// using stage-and-swap-with-recovery transactional semantics.
//
// Steps:
//  1. Pre-flight: refuse non-clean git tree unless opts.Force
//  2. Stage to sibling tempdir <parent>/<basename>.regen-merge-<ts>/
//  3. Deep-copy published → tempdir (preserves novels, additions, collisions)
//  4. Overwrite TEMPLATED-CLEAN files from fresh
//  5. Copy NEW-TEMPLATE-EMISSION files from fresh
//  6. Delete PUBLISHED-ONLY-TEMPLATED files from tempdir
//  7. Write fresh's go.mod into tempdir, then run RewriteModulePath
//     (rewrites all .go imports + go.mod module line in one sweep)
//  8. Overwrite tempdir's go.mod with the merged form (published module +
//     fresh requires + smart replaces)
//  9. Apply restoration plans (referent-existence check against tempdir)
//  10. Two-step rename: <cli-dir> → bak; tempdir → <cli-dir>; remove bak
//
// On any failure pre-rename, removes tempdir; published is untouched.
// On rename-2 failure, attempts to restore from bak.
// On both renames failing, returns an error with absolute bak path so the
// user can recover manually.
func Apply(report *MergeReport, opts Options) error {
	if report == nil {
		return errors.New("nil report")
	}
	cliDir := report.CLIDir
	freshDir := report.FreshDir

	// Pre-flight: require clean git tree (unless --force).
	if !opts.Force {
		if err := assertGitClean(cliDir); err != nil {
			return err
		}
	}

	// Stage tempdir as sibling of cliDir to ensure same-FS rename.
	parent := filepath.Dir(cliDir)
	base := filepath.Base(cliDir)
	ts := time.Now().Unix()
	tempDir := filepath.Join(parent, fmt.Sprintf("%s.regen-merge-%d", base, ts))
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return fmt.Errorf("creating tempdir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }

	// Deep-copy published → tempdir.
	if err := deepCopy(cliDir, tempDir); err != nil {
		cleanup()
		return fmt.Errorf("deep-copy to tempdir: %w", err)
	}

	// Apply file-level changes from the report.
	for i := range report.Files {
		fc := &report.Files[i]
		switch fc.Verdict {
		case VerdictTemplatedClean, VerdictNewTemplateEmission:
			src := filepath.Join(freshDir, fc.Path)
			dst := filepath.Join(tempDir, fc.Path)
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				cleanup()
				return fmt.Errorf("mkdir for %s: %w", fc.Path, err)
			}
			data, err := os.ReadFile(src)
			if err != nil {
				cleanup()
				return fmt.Errorf("reading fresh %s: %w", fc.Path, err)
			}
			if err := writeFileAtomic(dst, data); err != nil {
				cleanup()
				return fmt.Errorf("writing %s: %w", fc.Path, err)
			}
			fc.Applied = true
		case VerdictPublishedOnlyTemplated:
			dst := filepath.Join(tempDir, fc.Path)
			if err := os.Remove(dst); err != nil && !errors.Is(err, fs.ErrNotExist) {
				cleanup()
				return fmt.Errorf("removing stale %s: %w", fc.Path, err)
			}
			fc.Applied = true
		}
	}

	// Module-path rewrite: go.mod from fresh first, then RewriteModulePath
	// rewrites all .go imports + go.mod module line. Then overwrite go.mod
	// with the merged form (which has the published module path).
	pubModulePath, freshModulePath, err := readModulePaths(cliDir, freshDir)
	if err != nil {
		cleanup()
		return fmt.Errorf("reading module paths: %w", err)
	}
	freshGoMod := filepath.Join(freshDir, "go.mod")
	if data, err := os.ReadFile(freshGoMod); err == nil {
		if err := writeFileAtomic(filepath.Join(tempDir, "go.mod"), data); err != nil {
			cleanup()
			return fmt.Errorf("writing fresh go.mod into tempdir: %w", err)
		}
	}
	if pubModulePath != "" && freshModulePath != "" && pubModulePath != freshModulePath {
		if err := pipeline.RewriteModulePath(tempDir, freshModulePath, pubModulePath); err != nil {
			cleanup()
			return fmt.Errorf("rewriting module path: %w", err)
		}
	}

	// Overwrite go.mod with the final merged form.
	if mergedBytes, err := renderMergedGoMod(cliDir, freshDir); err == nil && len(mergedBytes) > 0 {
		if err := writeFileAtomic(filepath.Join(tempDir, "go.mod"), mergedBytes); err != nil {
			cleanup()
			return fmt.Errorf("writing merged go.mod: %w", err)
		}
		if report.GoMod != nil {
			report.GoMod.Merged = true
		}
	}

	// Apply restoration plans.
	for i := range report.LostRegistrations {
		lr := &report.LostRegistrations[i]
		hostPath := filepath.Join(tempDir, lr.HostFile)
		if err := injectAddCommands(hostPath, lr.Calls); err != nil {
			cleanup()
			return fmt.Errorf("injecting AddCommand into %s: %w", lr.HostFile, err)
		}
		lr.Applied = true
	}

	// Two-step rename with bak-recovery.
	bakDir := filepath.Join(parent, fmt.Sprintf("%s.regen-merge-bak-%d", base, ts))
	if err := os.Rename(cliDir, bakDir); err != nil {
		cleanup()
		return fmt.Errorf("renaming original to bak: %w", err)
	}
	if err := os.Rename(tempDir, cliDir); err != nil {
		// Recovery attempt.
		if rerr := os.Rename(bakDir, cliDir); rerr != nil {
			return fmt.Errorf("UNRECOVERABLE: rename to final failed (%v) AND restore from bak failed (%v); your data is at %s",
				err, rerr, bakDir)
		}
		_ = os.RemoveAll(tempDir)
		return fmt.Errorf("rename to final failed (recovered from bak): %w", err)
	}
	if err := os.RemoveAll(bakDir); err != nil {
		// Tree is fine; bak just lingers.
		fmt.Fprintf(os.Stderr, "warning: failed to remove bak dir %s: %v\n", bakDir, err)
	}

	report.Applied = true
	return nil
}

// assertGitClean returns an error if the git tree at dir has uncommitted
// changes. Mitigates the "uncommitted edits to TEMPLATED-CLEAN files
// silently destroyed" failure mode.
func assertGitClean(dir string) error {
	cmd := exec.Command("git", "status", "--porcelain", dir)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		// If git isn't available or the dir isn't a git repo, just warn.
		return nil
	}
	if len(out) > 0 {
		return fmt.Errorf("git tree at %s has uncommitted changes; commit/stash first or pass --force:\n%s", dir, out)
	}
	return nil
}

// deepCopy recursively copies src tree to dst.
func deepCopy(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		// Skip non-regular files (sockets, symlinks treated as warning).
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeType != 0 {
			return nil // skip
		}
		return copyFile(path, target, info.Mode().Perm())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	_, err = io.Copy(out, in)
	return err
}

// readModulePaths reads the module paths from both go.mod files. Either
// or both may be empty if go.mod is missing.
func readModulePaths(pubDir, freshDir string) (string, string, error) {
	read := func(p string) (string, error) {
		data, err := os.ReadFile(filepath.Join(p, "go.mod"))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return "", nil
			}
			return "", err
		}
		mf, err := modfile.Parse("go.mod", data, nil)
		if err != nil {
			return "", err
		}
		if mf.Module == nil {
			return "", nil
		}
		return mf.Module.Mod.Path, nil
	}
	pub, err := read(pubDir)
	if err != nil {
		return "", "", err
	}
	fresh, err := read(freshDir)
	if err != nil {
		return "", "", err
	}
	return pub, fresh, nil
}

// injectAddCommands appends the given AddCommand call expressions to a
// host file just before the trailing `return ...` statement of the function
// that already contains AddCommand calls.
//
// Uses dave/dst to preserve comments and formatting on the surrounding
// code. If the host file's structure doesn't match expectations (no
// `Execute` / cobra-Command-returning function, no existing AddCommand
// calls), the function returns an error so the caller surfaces a warning.
func injectAddCommands(hostPath string, calls []string) error {
	if len(calls) == 0 {
		return nil
	}
	data, err := os.ReadFile(hostPath)
	if err != nil {
		return err
	}
	dec := decorator.NewDecorator(nil)
	file, err := dec.ParseFile(hostPath, data, 0)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", hostPath, err)
	}

	// Find the function containing AddCommand calls. Inject the new calls
	// just before that function's trailing return statement (or at the end
	// of its body if no trailing return).
	injected := false
	dst.Inspect(file, func(n dst.Node) bool {
		fn, ok := n.(*dst.FuncDecl)
		if !ok || fn.Body == nil {
			return true
		}
		// Does this function have any AddCommand call?
		if !slices.ContainsFunc(fn.Body.List, isAddCommandStmt) {
			return true
		}

		// Build new statements from source strings.
		var newStmts []dst.Stmt
		for _, src := range calls {
			stmt, perr := parseStmtViaDST(src)
			if perr != nil {
				continue
			}
			newStmts = append(newStmts, stmt)
		}

		// Insert before the last `return` statement, or at the end if no
		// trailing return.
		insertAt := len(fn.Body.List)
		for i := len(fn.Body.List) - 1; i >= 0; i-- {
			if _, isRet := fn.Body.List[i].(*dst.ReturnStmt); isRet {
				insertAt = i
				break
			}
		}
		fn.Body.List = append(fn.Body.List[:insertAt], append(newStmts, fn.Body.List[insertAt:]...)...)
		injected = true
		return false
	})

	if !injected {
		return fmt.Errorf("no function with AddCommand calls found in %s", hostPath)
	}

	var buf strings.Builder
	if err := decorator.Fprint(&buf, file); err != nil {
		return fmt.Errorf("rendering %s: %w", hostPath, err)
	}
	return writeFileAtomic(hostPath, []byte(buf.String()))
}

// isAddCommandStmt returns true if the statement is a call to
// `<recv>.AddCommand(...)`.
func isAddCommandStmt(stmt dst.Stmt) bool {
	es, ok := stmt.(*dst.ExprStmt)
	if !ok {
		return false
	}
	ce, ok := es.X.(*dst.CallExpr)
	if !ok {
		return false
	}
	sel, ok := ce.Fun.(*dst.SelectorExpr)
	if !ok || sel.Sel == nil {
		return false
	}
	return sel.Sel.Name == "AddCommand"
}

// parseStmtViaDST parses a single Go statement into a dst.Stmt via the
// decorator. Wraps the source in a minimal func and extracts the body
// statement.
func parseStmtViaDST(src string) (dst.Stmt, error) {
	wrapped := "package x\nfunc _() {\n" + src + "\n}\n"
	dec := decorator.NewDecorator(nil)
	file, err := dec.ParseFile("inject.go", []byte(wrapped), 0)
	if err != nil {
		return nil, fmt.Errorf("parsing injection: %w", err)
	}
	for _, d := range file.Decls {
		fn, ok := d.(*dst.FuncDecl)
		if !ok {
			continue
		}
		if fn.Body == nil || len(fn.Body.List) == 0 {
			continue
		}
		return fn.Body.List[0], nil
	}
	return nil, fmt.Errorf("no statement in: %s", src)
}
