package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/artifacts"
	catalogpkg "github.com/mvanhorn/cli-printing-press/v4/internal/catalog"
	"github.com/mvanhorn/cli-printing-press/v4/internal/govulncheck"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/mvanhorn/cli-printing-press/v4/internal/platform"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/spf13/cobra"
)

const (
	goCommandTimeout   = 2 * time.Minute
	vulnCheckTimeout   = 5 * time.Minute
	binaryCheckTimeout = 15 * time.Second
)

func newPublishCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Validate and package CLIs for publishing",
		Example: `  # Validate a CLI before publishing
  cli-printing-press publish validate --dir ~/printing-press/library/notion --json

  # Package a CLI for publishing
  cli-printing-press publish package --dir ~/printing-press/library/notion --category productivity --target /tmp/staging --json`,
	}

	cmd.AddCommand(newPublishValidateCmd())
	cmd.AddCommand(newPublishPackageCmd())
	cmd.AddCommand(newPublishRenameCmd())

	return cmd
}

// CheckResult represents a single validation check.
type CheckResult struct {
	Name    string `json:"name"`
	Passed  bool   `json:"passed"`
	Error   string `json:"error,omitempty"`
	Warning string `json:"warning,omitempty"`
}

// ValidateResult is the JSON output of publish validate.
type ValidateResult struct {
	Passed     bool          `json:"passed"`
	CLIName    string        `json:"cli_name"`
	APIName    string        `json:"api_name"`
	HelpOutput string        `json:"help_output,omitempty"`
	Checks     []CheckResult `json:"checks"`
}

// PackageResult is the JSON output of publish package.
type PackageResult struct {
	StagedDir           string `json:"staged_dir"`
	CLIName             string `json:"cli_name"`
	APIName             string `json:"api_name"`
	Category            string `json:"category"`
	ModulePath          string `json:"module_path,omitempty"`
	ManuscriptsIncluded bool   `json:"manuscripts_included"`
	RunID               string `json:"run_id,omitempty"`
}

// RenameResult is the JSON output of publish rename.
type RenameResult struct {
	Success       bool   `json:"success"`
	OldName       string `json:"old_name"`
	NewName       string `json:"new_name"`
	NewDir        string `json:"new_dir"`
	FilesModified int    `json:"files_modified"`
	Error         string `json:"error,omitempty"`
}

func newPublishRenameCmd() *cobra.Command {
	var dir string
	var oldName string
	var newName string
	var legacyAPIName string
	var asJSON bool

	cmd := &cobra.Command{
		Use:     "rename",
		Short:   "Rename a staged CLI (for name collision resolution)",
		Example: `  cli-printing-press publish rename --dir /tmp/staging/library/ai/notion --old-name notion-pp-cli --new-name notion-alt-pp-cli --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dir == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--dir is required")}
			}
			if oldName == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--old-name is required")}
			}
			if newName == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--new-name is required")}
			}
			filesModified, err := pipeline.RenameCLI(dir, oldName, newName, legacyAPIName)

			if asJSON {
				result := RenameResult{
					OldName:       oldName,
					NewName:       newName,
					FilesModified: filesModified,
				}
				if err != nil {
					result.Error = err.Error()
				} else {
					result.Success = true
					result.NewDir = filepath.Join(filepath.Dir(dir), naming.LibraryDirName(newName))
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if encErr := enc.Encode(result); encErr != nil {
					return encErr
				}
				if err != nil {
					return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("rename failed"), Silent: true}
				}
				return nil
			}

			if err != nil {
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("rename failed: %w", err)}
			}
			newDir := filepath.Join(filepath.Dir(dir), naming.LibraryDirName(newName))
			fmt.Fprintf(os.Stderr, "Renamed %s → %s (%d files modified)\n", oldName, newName, filesModified)
			fmt.Fprintf(os.Stderr, "  New directory: %s\n", newDir)
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "Staged CLI directory to rename (required)")
	cmd.Flags().StringVar(&oldName, "old-name", "", "Current CLI name (required)")
	cmd.Flags().StringVar(&newName, "new-name", "", "New CLI name (required)")
	cmd.Flags().StringVar(&legacyAPIName, "api-name", "", "Deprecated no-op; api_name now follows --new-name")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}

func newPublishValidateCmd() *cobra.Command {
	var dir string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a CLI is ready for publishing",
		Example: `  cli-printing-press publish validate --dir ~/printing-press/library/notion
  cli-printing-press publish validate --dir ~/printing-press/library/notion --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dir == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--dir is required")}
			}

			result := runValidation(dir)

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if encErr := enc.Encode(result); encErr != nil {
					return encErr
				}
				if !result.Passed {
					return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("validation failed"), Silent: true}
				}
				return nil
			}

			// Human-readable output
			for _, c := range result.Checks {
				status := "PASS"
				if c.Warning != "" && c.Passed {
					status = "WARN"
				}
				if !c.Passed {
					status = "FAIL"
				}
				fmt.Fprintf(os.Stderr, "  %-20s %s", c.Name, status)
				if c.Error != "" {
					fmt.Fprintf(os.Stderr, "  %s", c.Error)
				}
				if c.Warning != "" {
					fmt.Fprintf(os.Stderr, "  %s", c.Warning)
				}
				fmt.Fprintln(os.Stderr)
			}

			if !result.Passed {
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("validation failed")}
			}
			fmt.Fprintln(os.Stderr, "\nAll checks passed.")
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "CLI directory to validate (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}

func newPublishPackageCmd() *cobra.Command {
	var dir string
	var category string
	var target string
	var dest string
	var modulePath string
	var allowMirrorDeletions bool
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "package",
		Short: "Package a CLI for publishing to the library repo",
		Example: `  # Stage into a new directory (for inspection)
  cli-printing-press publish package --dir ~/printing-press/library/notion --category productivity --target /tmp/staging --json

  # Write directly into the publish repo (replaces old CLI, includes manuscripts)
  cli-printing-press publish package --dir ~/printing-press/library/notion --category productivity --dest ~/printing-press/.publish-repo --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if dir == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--dir is required")}
			}
			if category == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--category is required")}
			}
			if strings.Contains(category, "/") || strings.Contains(category, "\\") || strings.Contains(category, "..") {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--category must be a simple slug (no path separators or '..')")}
			}
			if !catalogpkg.IsPublicCategory(category) {
				return &ExitError{
					Code: ExitInputError,
					Err:  fmt.Errorf("--category must be one of: %s", strings.Join(catalogpkg.PublicCategories(), ", ")),
				}
			}
			if target == "" && dest == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--target or --dest is required")}
			}
			if target != "" && dest != "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--target and --dest are mutually exclusive")}
			}
			if allowMirrorDeletions && dest == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--allow-mirror-deletions requires --dest (the divergence guard runs only in --dest mode)")}
			}

			// Cheap existence checks before expensive validation
			if target != "" {
				if _, err := os.Stat(target); err == nil {
					return &ExitError{Code: ExitInputError, Err: fmt.Errorf("target directory already exists: %s", target)}
				}
			}
			if dest != "" {
				if info, err := os.Stat(dest); err != nil || !info.IsDir() {
					return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--dest directory does not exist: %s", dest)}
				}
			}

			// Re-validate before packaging
			vResult := runValidation(dir)
			if !vResult.Passed {
				if asJSON {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					if encErr := enc.Encode(vResult); encErr != nil {
						return encErr
					}
				}
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("validation failed, cannot package")}
			}

			// Determine CLI name from manifest or directory name
			cliName := vResult.CLIName
			if cliName == "" {
				cliName = filepath.Base(dir)
			}

			// Determine directory name: use API slug, not CLI name.
			dirName := vResult.APIName
			if dirName == "" {
				dirName = naming.TrimCLISuffix(cliName)
			}
			if dirName == "" {
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("cannot determine API name for directory key")}
			}

			// Choose output mode: --dest writes directly, --target stages
			var outCLIDir string
			var rootDir string // root to clean up on failure
			// stashedDirs holds old CLI dirs moved aside in --dest mode.
			// Restored on failure, deleted on success.
			var stashedDirs []stashedDir
			if dest != "" {
				rootDir = dest
				outCLIDir = filepath.Join(dest, "library", category, dirName)

				// Scoped to this CLI dir; other categories belong to
				// category-migration intent, where the operator has
				// already accepted that the old location goes away.
				if !allowMirrorDeletions {
					if err := checkMirrorDivergence(outCLIDir, dir); err != nil {
						return err
					}
				}

				// Move existing CLI dirs aside (don't delete yet — restore on failure)
				var err error
				stashedDirs, err = stashExistingCLI(dest, dirName)
				if err != nil {
					return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("stashing old CLI: %w", err)}
				}
			} else {
				rootDir = target
				outCLIDir = filepath.Join(target, "library", category, dirName)
			}

			// Verify the resolved path is actually under rootDir (defense in depth)
			absRoot, _ := filepath.Abs(rootDir)
			absOut, _ := filepath.Abs(outCLIDir)
			if !strings.HasPrefix(absOut, absRoot+string(filepath.Separator)) {
				restoreStashedDirs(stashedDirs)
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("resolved output path %s escapes root directory %s", absOut, absRoot)}
			}

			cleanupOnFailure := func() {
				if dest != "" {
					_ = os.RemoveAll(outCLIDir)
					restoreStashedDirs(stashedDirs)
				} else {
					_ = os.RemoveAll(target)
				}
			}

			if err := os.MkdirAll(filepath.Dir(outCLIDir), 0o755); err != nil {
				cleanupOnFailure()
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("creating output dir: %w", err)}
			}

			// Copy CLI source
			if err := pipeline.CopyDir(dir, outCLIDir); err != nil {
				cleanupOnFailure()
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("copying CLI: %w", err)}
			}
			if err := backfillPackagedManifestAttribution(outCLIDir); err != nil {
				cleanupOnFailure()
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("backfilling printer attribution: %w", err)}
			}
			if err := normalizePackagedPublishMetadata(outCLIDir, category); err != nil {
				cleanupOnFailure()
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("normalizing publish metadata: %w", err)}
			}

			// Strip build/ from the staged tree. autoBundleForHost writes
			// host-platform .mcpb bundles + staged binaries there as a
			// local-dev convenience; the public library treats CI release
			// artifacts as canonical, so build/ should never reach the
			// public library through the publish path.
			if err := os.RemoveAll(filepath.Join(outCLIDir, "build")); err != nil {
				cleanupOnFailure()
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("stripping build dir: %w", err)}
			}
			// CopyDir preserves the source CLI tree, but publish package owns
			// the bundled manuscript set. Strip any embedded copy first, then
			// re-add manuscripts through the publishable filter below.
			if err := os.RemoveAll(filepath.Join(outCLIDir, ".manuscripts")); err != nil {
				cleanupOnFailure()
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("stripping staged manuscripts: %w", err)}
			}

			// Strip root-level binaries that local `go build ./cmd/...` (or
			// `make build` / `make build-mcp` without `-o bin/...`) drops at
			// the CLI dir root. The skill's downstream `cp -r ... PUBLISH_REPO_DIR`
			// has a parallel `rm -f` line that historically missed
			// `<api-slug>-pp-mcp`; doing the strip here on the staged copy
			// makes the publish path binary-free regardless of which downstream
			// step (skill or other tooling) the operator runs next.
			for _, name := range stagedBinaryNames(cliName, dirName) {
				if err := os.Remove(filepath.Join(outCLIDir, name)); err != nil && !os.IsNotExist(err) {
					cleanupOnFailure()
					return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("stripping staged binary %s: %w", name, err)}
				}
			}
			testBinaries, err := filepath.Glob(filepath.Join(outCLIDir, "*.test"))
			if err != nil {
				cleanupOnFailure()
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("finding staged test binaries: %w", err)}
			}
			for _, path := range testBinaries {
				if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
					cleanupOnFailure()
					return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("stripping staged test binary %s: %w", filepath.Base(path), err)}
				}
			}
			for _, name := range stagedShipcheckReportNames() {
				if err := os.Remove(filepath.Join(outCLIDir, name)); err != nil && !os.IsNotExist(err) {
					cleanupOnFailure()
					return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("stripping staged shipcheck report %s: %w", name, err)}
				}
			}

			// Rewrite go.mod module path if --module-path is set
			if modulePath != "" {
				oldModPath := cliName // generated CLIs use bare CLI name as module path
				if err := pipeline.RewriteModulePath(outCLIDir, oldModPath, modulePath); err != nil {
					cleanupOnFailure()
					return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("rewriting module path: %w", err)}
				}
			}

			// Resolve and copy manuscripts
			result := PackageResult{
				StagedDir:  outCLIDir,
				CLIName:    cliName,
				APIName:    vResult.APIName,
				Category:   category,
				ModulePath: modulePath,
			}

			msDir, runID := resolveManuscripts(cliName, vResult.APIName)
			if runID == "" {
				embeddedMsDir := filepath.Join(dir, ".manuscripts")
				if embeddedRunID, err := findMostRecentRun(embeddedMsDir); err == nil && embeddedRunID != "" {
					msDir = embeddedMsDir
					runID = embeddedRunID
				}
			}
			if runID != "" {
				result.RunID = runID
				srcMsDir := filepath.Join(msDir, runID)
				dstMsDir := filepath.Join(outCLIDir, ".manuscripts", runID)
				if err := pipeline.CopyPublishableManuscriptDir(srcMsDir, dstMsDir); err != nil {
					cleanupOnFailure()
					return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("copying manuscripts: %w", err)}
				} else {
					result.ManuscriptsIncluded = true
				}
			} else {
				fmt.Fprintln(os.Stderr, "warning: no manuscripts found, packaging without them")
			}

			cookieNames, err := stagedPackageCookieNames(outCLIDir)
			if err != nil {
				cleanupOnFailure()
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("reading staged package cookie auth metadata: %w", err)}
			}
			findings, err := artifacts.FindPackageSecrets(outCLIDir, cookieNames)
			if err != nil {
				cleanupOnFailure()
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("scanning staged package for secret tokens: %w", err)}
			}

			piiResult, piiErr := artifacts.RunPIIAudit(outCLIDir)
			if piiErr != nil {
				cleanupOnFailure()
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("scanning staged package for PII: %w", piiErr)}
			}

			if scanErr := formatCombinedScanError(findings, piiResult.Findings, piiResult.Completion); scanErr != nil {
				cleanupOnFailure()
				return &ExitError{Code: ExitPublishError, Err: scanErr}
			}

			// Success — remove stashed old CLI dirs
			removeStashedDirs(stashedDirs)

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			fmt.Fprintf(os.Stderr, "Packaged %s at %s\n", cliName, outCLIDir)
			if result.ManuscriptsIncluded {
				fmt.Fprintf(os.Stderr, "  Manuscripts: %s (run %s)\n", ".manuscripts/"+runID, runID)
			} else {
				fmt.Fprintln(os.Stderr, "  Manuscripts: not included (none found)")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&dir, "dir", "", "CLI directory to package (required)")
	cmd.Flags().StringVar(&category, "category", "", "Category for the CLI (required)")
	cmd.Flags().StringVar(&target, "target", "", "Staging directory to create (mutually exclusive with --dest)")
	cmd.Flags().StringVar(&dest, "dest", "", "Publish repo to write into directly (mutually exclusive with --target)")
	cmd.Flags().StringVar(&modulePath, "module-path", "", "Go module path to set (e.g., github.com/org/repo/library/category/cli-name)")
	cmd.Flags().BoolVar(&allowMirrorDeletions, "allow-mirror-deletions", false, "Allow the overlay to delete mirror files that have no source counterpart (use only after manual reconciliation)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}

// stashedDir records an old CLI directory that was moved aside during --dest mode.
type stashedDir struct {
	original string // where it was (e.g., library/productivity/notion-pp-cli)
	stashed  string // where it was moved to (e.g., library/productivity/notion-pp-cli.old-XXXXX)
}

// stashExistingCLI moves any existing version of a CLI aside (rename, not delete).
// This handles category changes — if a CLI moved from one category to another,
// all old dirs across categories are stashed. On success the caller removes
// the stashed dirs; on failure the caller restores them.
func stashExistingCLI(repoDir, cliName string) ([]stashedDir, error) {
	libDir := filepath.Join(repoDir, "library")
	entries, err := os.ReadDir(libDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var stashed []stashedDir
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		candidate := filepath.Join(libDir, e.Name(), cliName)
		if _, statErr := os.Stat(candidate); statErr == nil {
			tmpName := candidate + ".old"
			if err := os.Rename(candidate, tmpName); err != nil {
				// Restore any already-stashed dirs before returning
				restoreStashedDirs(stashed)
				return nil, fmt.Errorf("stashing %s: %w", candidate, err)
			}
			stashed = append(stashed, stashedDir{original: candidate, stashed: tmpName})
		}
	}
	return stashed, nil
}

// restoreStashedDirs moves stashed dirs back to their original locations.
func restoreStashedDirs(dirs []stashedDir) {
	for _, d := range dirs {
		_ = os.Rename(d.stashed, d.original)
	}
}

// removeStashedDirs permanently deletes stashed dirs after a successful package.
func removeStashedDirs(dirs []stashedDir) {
	for _, d := range dirs {
		_ = os.RemoveAll(d.stashed)
	}
}

// mirrorDivergenceExampleLimit caps how many would-be-deleted paths the
// divergence error names inline before falling back to a count.
const mirrorDivergenceExampleLimit = 10

// checkMirrorDivergence returns an ExitInputError naming the first
// findings if any file under mirrorCLIDir is not present in sourceCLIDir.
// A non-existent mirrorCLIDir is treated as no divergence.
func checkMirrorDivergence(mirrorCLIDir, sourceCLIDir string) error {
	mirrorOnly, err := listMirrorOnlyFiles(mirrorCLIDir, sourceCLIDir)
	if err != nil {
		return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("scanning mirror for divergence: %w", err)}
	}
	if len(mirrorOnly) == 0 {
		return nil
	}

	var msg strings.Builder
	noun := "files"
	if len(mirrorOnly) == 1 {
		noun = "file"
	}
	fmt.Fprintf(&msg, "mirror has %d %s not present in source library (likely a direct community PR or independent edit). Publishing now would delete this content. Reconcile manually, then re-run with --allow-mirror-deletions to override.\n", len(mirrorOnly), noun)
	limit := min(len(mirrorOnly), mirrorDivergenceExampleLimit)
	for _, p := range mirrorOnly[:limit] {
		fmt.Fprintf(&msg, "  %s\n", p)
	}
	if len(mirrorOnly) > limit {
		fmt.Fprintf(&msg, "  ... and %d more\n", len(mirrorOnly)-limit)
	}

	return &ExitError{Code: ExitInputError, Err: errors.New(strings.TrimRight(msg.String(), "\n"))}
}

// listMirrorOnlyFiles returns slash-separated relative paths under
// mirrorCLIDir whose corresponding files do not exist under sourceCLIDir.
// The top-level .manuscripts/ and build/ directories are skipped because
// the publish flow manages those outputs separately: .manuscripts/ is
// repopulated per run, and build/ is stripped after the source copy. A
// non-existent mirrorCLIDir is treated as no divergence.
func listMirrorOnlyFiles(mirrorCLIDir, sourceCLIDir string) ([]string, error) {
	var mirrorOnly []string
	err := filepath.WalkDir(mirrorCLIDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == mirrorCLIDir && os.IsNotExist(walkErr) {
				return fs.SkipAll
			}
			return fmt.Errorf("%s: %w", path, walkErr)
		}
		if path == mirrorCLIDir {
			return nil
		}

		rel, err := filepath.Rel(mirrorCLIDir, path)
		if err != nil {
			return err
		}

		if d.IsDir() {
			switch rel {
			case ".manuscripts", "build":
				return fs.SkipDir
			}
			return nil
		}

		if _, statErr := os.Lstat(filepath.Join(sourceCLIDir, rel)); statErr != nil {
			if os.IsNotExist(statErr) {
				mirrorOnly = append(mirrorOnly, filepath.ToSlash(rel))
				return nil
			}
			return statErr
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return mirrorOnly, nil
}

// resolveManuscripts finds the manuscripts directory and most recent run ID
// for a CLI. Tries API name first (SKILL convention), then CLI name (legacy
// binary convention), then fuzzy resolve.
func resolveManuscripts(cliName, apiName string) (msDir string, runID string) {
	if apiName == "" {
		apiName = naming.TrimCLISuffix(cliName)
	}

	msRoot := pipeline.PublishedManuscriptsRoot()

	// 1. Try API name (SKILL convention: manuscripts/<api-slug>/<run>/)
	apiMsDir := filepath.Join(msRoot, apiName)
	if rid, err := findMostRecentRun(apiMsDir); err == nil && rid != "" {
		return apiMsDir, rid
	}
	// 2. Try CLI name (legacy binary convention: manuscripts/<cli-name>/<run>/)
	cliMsDir := filepath.Join(msRoot, cliName)
	if rid, err := findMostRecentRun(cliMsDir); err == nil && rid != "" {
		return cliMsDir, rid
	}
	// 3. Fuzzy resolve (strip suffixes, prefix match)
	return resolveManuscriptDir(msRoot, apiName)
}

func runValidation(dir string) ValidateResult {
	result := ValidateResult{}
	allPassed := true

	// 1. Manifest check
	manifestPath := filepath.Join(dir, pipeline.CLIManifestFilename)
	var manifest pipeline.CLIManifest
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		result.Checks = append(result.Checks, CheckResult{Name: "manifest", Passed: false, Error: "missing .printing-press.json"})
		allPassed = false
	} else {
		if err := json.Unmarshal(data, &manifest); err != nil {
			result.Checks = append(result.Checks, CheckResult{Name: "manifest", Passed: false, Error: fmt.Sprintf("invalid JSON: %v", err)})
			allPassed = false
		} else {
			result.CLIName = manifest.CLIName
			result.APIName = manifest.APIName
			if issues := validatePublishManifestContract(dir, manifest); len(issues) > 0 {
				result.Checks = append(result.Checks, CheckResult{Name: "manifest", Passed: false, Error: strings.Join(issues, "; ")})
				allPassed = false
			} else {
				result.Checks = append(result.Checks, CheckResult{Name: "manifest", Passed: true})
			}
		}
	}

	// 2. Transcendence check. Published CLIs are expected to carry verified
	// novel features from the absorb phase; a bare endpoint wrapper should not
	// pass publish validation as shippable.
	if manifest.APIName == "" || manifest.CLIName == "" {
		result.Checks = append(result.Checks, CheckResult{Name: "transcendence", Passed: false, Error: "manifest unavailable"})
		allPassed = false
	} else if len(manifest.NovelFeatures) == 0 {
		result.Checks = append(result.Checks, CheckResult{Name: "transcendence", Passed: false, Error: "no novel features recorded; run the absorb/transcend phase and dogfood before publishing"})
		allPassed = false
	} else {
		result.Checks = append(result.Checks, CheckResult{Name: "transcendence", Passed: true})
	}

	phase5Check := checkPhase5Gate(dir, manifest)
	if !phase5Check.Passed {
		allPassed = false
	}
	result.Checks = append(result.Checks, phase5Check)

	cliName := result.CLIName
	if cliName == "" {
		cliName = filepath.Base(dir)
	}

	restoreBuildArtifacts := snapshotFiles(buildArtifactCandidates(dir, cliName))
	defer restoreBuildArtifacts()

	// 3. go mod tidy check — snapshot files, run tidy, compare, restore
	tidyCheck := checkGoModTidy(dir)
	if !tidyCheck.Passed {
		allPassed = false
	}
	result.Checks = append(result.Checks, tidyCheck)

	// 4. govulncheck catches reachable vulnerable code in this one CLI module
	// before publish. Keep this scoped to dir; the public library may contain
	// stale historical CLIs, and whole-library vulnerability enforcement
	// belongs in changed-module CI or a report-only scheduled sweep.
	vulnCheck := runGoVulnCheck(dir)
	if !vulnCheck.Passed {
		allPassed = false
	}
	result.Checks = append(result.Checks, vulnCheck)

	// 5. go vet check
	vetCheck := runGoCheck(dir, "vet", "./...")
	if !vetCheck.Passed {
		allPassed = false
	}
	result.Checks = append(result.Checks, vetCheck)

	// 6. go build check
	buildCheck := runGoCheck(dir, "build", "./...")
	if !buildCheck.Passed {
		allPassed = false
	}
	result.Checks = append(result.Checks, buildCheck)

	// 7. --help / --version checks use a dedicated temp binary so validation
	// exercises current source without depending on or mutating source-tree artifacts.
	binaryPath, cleanupBinary, err := buildValidationBinary(dir, cliName)
	if cleanupBinary != nil {
		defer cleanupBinary()
	}
	if err != nil {
		result.Checks = append(result.Checks, CheckResult{Name: "--help", Passed: false, Error: "built binary not found"})
		result.Checks = append(result.Checks, CheckResult{Name: "--version", Passed: false, Error: "built binary not found"})
		allPassed = false
	} else {
		helpCtx, helpCancel := context.WithTimeout(context.Background(), binaryCheckTimeout)
		defer helpCancel()
		helpCmd := exec.CommandContext(helpCtx, binaryPath, "--help")
		helpCmd.Dir = dir
		helpOut, helpErr := helpCmd.CombinedOutput()
		if helpErr != nil {
			errMsg := fmt.Sprintf("exit error: %v", helpErr)
			if helpCtx.Err() == context.DeadlineExceeded {
				errMsg = fmt.Sprintf("timed out after %s", binaryCheckTimeout)
			}
			result.Checks = append(result.Checks, CheckResult{Name: "--help", Passed: false, Error: errMsg})
			allPassed = false
		} else {
			result.Checks = append(result.Checks, CheckResult{Name: "--help", Passed: true})
			result.HelpOutput = string(helpOut)
		}

		// 8. --version check
		verCtx, verCancel := context.WithTimeout(context.Background(), binaryCheckTimeout)
		defer verCancel()
		versionCmd := exec.CommandContext(verCtx, binaryPath, "--version")
		versionCmd.Dir = dir
		if _, vErr := versionCmd.CombinedOutput(); vErr != nil {
			errMsg := fmt.Sprintf("exit error: %v", vErr)
			if verCtx.Err() == context.DeadlineExceeded {
				errMsg = fmt.Sprintf("timed out after %s", binaryCheckTimeout)
			}
			result.Checks = append(result.Checks, CheckResult{Name: "--version", Passed: false, Error: errMsg})
			allPassed = false
		} else {
			result.Checks = append(result.Checks, CheckResult{Name: "--version", Passed: true})
		}
	}

	// 9. SKILL.md verification — fail if the agent-facing skill advertises
	// commands, flags, or arguments the shipped CLI source does not provide.
	skillCheck := checkVerifySkill(dir)
	if !skillCheck.Passed {
		allPassed = false
	}
	result.Checks = append(result.Checks, skillCheck)

	// 10. Manuscripts check (warn-only)
	// Try CLI name first (new convention), then API name, then fuzzy resolve
	apiName := result.APIName
	if apiName == "" {
		apiName = naming.TrimCLISuffix(cliName)
	}
	msRoot := pipeline.PublishedManuscriptsRoot()
	msDir := filepath.Join(msRoot, cliName)
	if _, err := os.Stat(msDir); os.IsNotExist(err) {
		msDir = filepath.Join(msRoot, apiName)
	}
	if _, err := os.Stat(msDir); os.IsNotExist(err) {
		msDir, _ = resolveManuscriptDir(msRoot, apiName)
	}
	if _, err := os.Stat(msDir); os.IsNotExist(err) {
		result.Checks = append(result.Checks, CheckResult{Name: "manuscripts", Passed: true, Warning: "no manuscripts found"})
	} else {
		runID, err := findMostRecentRun(msDir)
		if err != nil || runID == "" {
			result.Checks = append(result.Checks, CheckResult{Name: "manuscripts", Passed: true, Warning: "manuscripts directory exists but no runs found"})
		} else {
			result.Checks = append(result.Checks, CheckResult{Name: "manuscripts", Passed: true})
		}
	}

	result.Passed = allPassed
	return result
}

func validatePublishManifestContract(dir string, manifest pipeline.CLIManifest) []string {
	var issues []string
	manifest = manifestWithPublishAttributionFallbacks(manifest)
	if manifest.SchemaVersion != pipeline.CurrentCLIManifestSchemaVersion {
		issues = append(issues, fmt.Sprintf("schema_version must be %d (found %d)", pipeline.CurrentCLIManifestSchemaVersion, manifest.SchemaVersion))
	}

	var creatorHandle, creatorName string
	if manifest.Creator != nil {
		creatorHandle = strings.TrimSpace(manifest.Creator.Handle)
		creatorName = strings.TrimSpace(manifest.Creator.Name)
	}

	var missing []string
	required := []struct {
		name  string
		value string
	}{
		{name: "api_name", value: manifest.APIName},
		{name: "cli_name", value: manifest.CLIName},
		{name: "run_id", value: manifest.RunID},
		{name: "printing_press_version", value: manifest.PrintingPressVersion},
		{name: "creator.handle", value: creatorHandle},
		{name: "creator.name", value: creatorName},
		{name: "printer", value: manifest.Printer},
		{name: "printer_name", value: manifest.PrinterName},
	}
	for _, field := range required {
		if strings.TrimSpace(field.value) == "" {
			missing = append(missing, field.name)
		}
	}
	if len(missing) > 0 {
		issues = append(issues, "missing required manifest fields: "+strings.Join(missing, ", "))
	}
	if isPublishPrinterSentinel(creatorHandle) {
		issues = append(issues, fmt.Sprintf("creator.handle must not be the literal sentinel %q", creatorHandle))
	}
	if isPublishPrinterSentinel(manifest.Printer) {
		issues = append(issues, fmt.Sprintf("printer must not be the literal sentinel %q", manifest.Printer))
	}
	if issue := validateGitHubPrinterExists(manifest.Printer); issue != "" {
		issues = append(issues, issue)
	}

	if manifestAdvertisesMCP(manifest) {
		for _, filename := range []string{pipeline.MCPBManifestFilename, pipeline.ToolsManifestFilename} {
			path := filepath.Join(dir, filename)
			if info, err := os.Stat(path); err != nil || info.IsDir() {
				issues = append(issues, fmt.Sprintf("MCP package metadata missing %s", filename))
			}
		}
	}

	return issues
}

func manifestWithPublishAttributionFallbacks(manifest pipeline.CLIManifest) pipeline.CLIManifest {
	// Fill the legacy printer fields from a present creator first, so a
	// creator-only manifest (manual edit or future creator-primary state)
	// validates without needing a git identity (e.g. on CI).
	if manifest.Creator != nil && !manifest.Creator.IsZero() {
		if strings.TrimSpace(manifest.Printer) == "" {
			manifest.Printer = manifest.Creator.Handle
		}
		if strings.TrimSpace(manifest.PrinterName) == "" {
			manifest.PrinterName = manifest.Creator.Name
		}
	}
	if strings.TrimSpace(manifest.Printer) == "" || strings.TrimSpace(manifest.PrinterName) == "" {
		fallback := resolvePublishAttributionFallback(manifest)
		if strings.TrimSpace(manifest.Printer) == "" && fallback.Printer != "" {
			manifest.Printer = fallback.Printer
		}
		if strings.TrimSpace(manifest.PrinterName) == "" && fallback.PrinterName != "" {
			manifest.PrinterName = fallback.PrinterName
		}
	}
	// Backfill the creator from the (now-resolved) legacy fields so a manifest
	// generated before the creator model — or one whose attribution was only
	// resolved at publish time — still carries a creator for validation and the
	// public registry.
	if manifest.Creator == nil || manifest.Creator.IsZero() {
		if h, n := strings.TrimSpace(manifest.Printer), strings.TrimSpace(manifest.PrinterName); h != "" || n != "" {
			manifest.Creator = &spec.Person{Handle: h, Name: n}
		}
	}
	return manifest
}

type publishAttributionFallback struct {
	Printer     string
	PrinterName string
}

func resolvePublishAttributionFallback(manifest pipeline.CLIManifest) publishAttributionFallback {
	printer := strings.TrimSpace(manifest.Printer)
	printerName := strings.TrimSpace(manifest.PrinterName)
	if printer != "" && printerName == "" {
		return publishAttributionFallback{PrinterName: resolveGitHubUserName(printer)}
	}
	if printer == "" && printerName == "" {
		return resolveCurrentPublishAttributionFallback()
	}
	return publishAttributionFallback{}
}

func resolveCurrentPublishAttributionFallback() publishAttributionFallback {
	if fallback := resolveGitPublishAttributionFallback(); fallback.complete() {
		return fallback
	}
	if fallback := resolveGhPublishAttributionFallback(); fallback.complete() {
		return fallback
	}
	return publishAttributionFallback{}
}

func (fallback publishAttributionFallback) complete() bool {
	return strings.TrimSpace(fallback.Printer) != "" && strings.TrimSpace(fallback.PrinterName) != ""
}

func resolveGitPublishAttributionFallback() publishAttributionFallback {
	return publishAttributionFallback{
		Printer:     firstNonNullCommandOutput([]string{"git", "config", "github.user"}),
		PrinterName: firstNonNullCommandOutput([]string{"git", "config", "user.name"}),
	}
}

func resolveGhPublishAttributionFallback() publishAttributionFallback {
	var user struct {
		Login string `json:"login"`
		Name  string `json:"name"`
	}
	if !readGhAPIJSON("user", &user) {
		return publishAttributionFallback{}
	}
	return publishAttributionFallback{Printer: strings.TrimSpace(user.Login), PrinterName: strings.TrimSpace(user.Name)}
}

func resolveGitHubUserName(printer string) string {
	var user struct {
		Name string `json:"name"`
	}
	if !readGhAPIJSON("users/"+url.PathEscape(strings.TrimSpace(printer)), &user) {
		return ""
	}
	return strings.TrimSpace(user.Name)
}

func readGhAPIJSON(path string, target any) bool {
	out, err := exec.Command("gh", "api", path).Output()
	if err != nil {
		return false
	}
	return json.Unmarshal(out, target) == nil
}

func firstNonNullCommandOutput(commands ...[]string) string {
	for _, args := range commands {
		if len(args) == 0 {
			continue
		}
		out, err := exec.Command(args[0], args[1:]...).Output()
		if err != nil {
			continue
		}
		value := strings.TrimSpace(string(out))
		if value != "" && value != "null" {
			return value
		}
	}
	return ""
}

func validateGitHubPrinterExists(printer string) string {
	printer = strings.TrimSpace(printer)
	if printer == "" || isPublishPrinterSentinel(printer) {
		return ""
	}
	out, err := exec.Command("gh", "api", "users/"+url.PathEscape(printer), "--jq", ".login").CombinedOutput()
	if err == nil {
		return ""
	}
	output := string(out)
	if strings.Contains(output, "404") || strings.Contains(strings.ToLower(output), "not found") {
		return fmt.Sprintf("printer %q does not resolve to a GitHub user", printer)
	}
	detail := strings.Join(strings.Fields(output), " ")
	if detail == "" {
		detail = err.Error()
	}
	return fmt.Sprintf("could not verify printer %q with gh: %s", printer, detail)
}

func backfillPackagedManifestAttribution(dir string) error {
	manifestPath := filepath.Join(dir, pipeline.CLIManifestFilename)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var manifest pipeline.CLIManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	fallback := resolvePublishAttributionFallback(manifest)
	needsPrinter := strings.TrimSpace(manifest.Printer) == ""
	needsPrinterName := strings.TrimSpace(manifest.PrinterName) == ""
	if needsPrinter && fallback.Printer == "" {
		return fmt.Errorf("printer attribution is missing and no fallback could be resolved")
	}
	if needsPrinterName && fallback.PrinterName == "" {
		return fmt.Errorf("printer_name attribution is missing and no fallback could be resolved")
	}
	changed := false
	if needsPrinter && fallback.Printer != "" {
		encoded, err := json.Marshal(fallback.Printer)
		if err != nil {
			return err
		}
		raw["printer"] = encoded
		changed = true
	}
	if needsPrinterName && fallback.PrinterName != "" {
		encoded, err := json.Marshal(fallback.PrinterName)
		if err != nil {
			return err
		}
		raw["printer_name"] = encoded
		changed = true
	}
	if !changed {
		return nil
	}
	updated, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	updated = append(updated, '\n')
	info, err := os.Stat(manifestPath)
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath, updated, info.Mode())
}

func normalizePackagedPublishMetadata(dir, category string) error {
	manifestPath := filepath.Join(dir, pipeline.CLIManifestFilename)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	var manifest pipeline.CLIManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}

	if strings.TrimSpace(manifest.Category) != category {
		encoded, err := json.Marshal(category)
		if err != nil {
			return err
		}
		raw["category"] = encoded
		updated, err := json.MarshalIndent(raw, "", "  ")
		if err != nil {
			return err
		}
		updated = append(updated, '\n')
		info, err := os.Stat(manifestPath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(manifestPath, updated, info.Mode()); err != nil {
			return err
		}
	}

	return pipeline.EnsurePatchesDir(dir)
}

func isPublishPrinterSentinel(printer string) bool {
	return printer == "USER" || printer == "user"
}

func manifestAdvertisesMCP(manifest pipeline.CLIManifest) bool {
	return strings.TrimSpace(manifest.MCPBinary) != "" || manifest.MCPReady != "" || manifest.MCPToolCount > 0 || manifest.MCPPublicToolCount > 0
}

func checkPhase5Gate(dir string, manifest pipeline.CLIManifest) CheckResult {
	if manifest.APIName == "" || manifest.CLIName == "" {
		return CheckResult{Name: "phase5", Passed: false, Error: "manifest unavailable"}
	}
	if manifest.RunID == "" {
		return CheckResult{Name: "phase5", Passed: false, Error: "manifest missing run_id; cannot locate Phase 5 gate proof"}
	}

	proofsDir := phase5ProofsDir(dir, manifest)
	result := pipeline.ValidatePhase5Gate(proofsDir, manifest)
	if !result.Passed {
		return CheckResult{Name: "phase5", Passed: false, Error: result.Detail}
	}
	return CheckResult{Name: "phase5", Passed: true}
}

func phase5ProofsDir(dir string, manifest pipeline.CLIManifest) string {
	runID := manifest.RunID
	candidates := []string{
		filepath.Join(dir, ".manuscripts", runID, "proofs"),
	}
	msRoot := pipeline.PublishedManuscriptsRoot()
	if manifest.CLIName != "" {
		candidates = append(candidates, filepath.Join(msRoot, manifest.CLIName, runID, "proofs"))
	}
	if manifest.APIName != "" {
		candidates = append(candidates, filepath.Join(msRoot, manifest.APIName, runID, "proofs"))
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return candidates[0]
}

func checkVerifySkill(dir string) CheckResult {
	run, err := runVerifySkillScript(dir, nil, false, false)
	if err != nil {
		errMsg := strings.TrimSpace(run.Stdout + "\n" + run.Stderr)
		if errMsg == "" {
			errMsg = err.Error()
		}
		return CheckResult{Name: "verify-skill", Passed: false, Error: errMsg}
	}
	if run.ExitCode != 0 {
		return CheckResult{Name: "verify-skill", Passed: false, Error: strings.TrimSpace(run.Stdout + "\n" + run.Stderr)}
	}
	finding, hasFinding, _, cErr := runCanonicalSectionsCheck(dir)
	if cErr != nil {
		return CheckResult{Name: "verify-skill", Passed: false, Error: cErr.Error()}
	}
	if hasFinding {
		return CheckResult{Name: "verify-skill", Passed: false, Error: fmt.Sprintf("[%s] %s: %s", finding.Check, finding.Command, finding.Detail)}
	}
	return CheckResult{Name: "verify-skill", Passed: true}
}

func runGoCheck(dir string, args ...string) CheckResult {
	return runGoCommandCheck(dir, "go "+args[0], goCommandTimeout, args...)
}

func runGoCommandCheck(dir, name string, timeout time.Duration, args ...string) CheckResult {
	return runGoCommandCheckWithEnv(dir, name, timeout, nil, args...)
}

func runGoCommandCheckWithEnv(dir, name string, timeout time.Duration, env []string, args ...string) CheckResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = envWithOverrides(os.Environ(), env)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		errMsg := strings.TrimSpace(string(output))
		if ctx.Err() == context.DeadlineExceeded {
			errMsg = fmt.Sprintf("timed out after %s", timeout)
		} else if errMsg == "" {
			errMsg = err.Error()
		}
		return CheckResult{Name: name, Passed: false, Error: errMsg}
	}
	return CheckResult{Name: name, Passed: true}
}

func envWithOverrides(base, overrides []string) []string {
	overrideKeys := make(map[string]struct{}, len(overrides))
	for _, entry := range overrides {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			overrideKeys[key] = struct{}{}
		}
	}

	env := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			if _, exists := overrideKeys[key]; exists {
				continue
			}
		}
		env = append(env, entry)
	}
	return append(env, overrides...)
}

func runGoVulnCheck(dir string) CheckResult {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return CheckResult{Name: govulncheck.Name, Passed: false, Error: "go.mod not found"}
	}
	env := govulncheck.ToolchainEnv(dir)
	return runGoCommandCheckWithEnv(dir, govulncheck.Name, vulnCheckTimeout, env, govulncheck.GoRunArgs("./...")...)
}

func checkGoModTidy(dir string) CheckResult {
	modPath := filepath.Join(dir, "go.mod")
	sumPath := filepath.Join(dir, "go.sum")

	// Snapshot current content
	origMod, modErr := os.ReadFile(modPath)
	origSum, _ := os.ReadFile(sumPath) // go.sum may not exist yet

	if modErr != nil {
		return CheckResult{Name: "go mod tidy", Passed: false, Error: "go.mod not found"}
	}

	// Run go mod tidy with timeout
	ctx, cancel := context.WithTimeout(context.Background(), goCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "mod", "tidy")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Restore originals before returning
		_ = os.WriteFile(modPath, origMod, 0o644)
		if origSum != nil {
			_ = os.WriteFile(sumPath, origSum, 0o644)
		}
		errMsg := strings.TrimSpace(string(output))
		if errMsg == "" {
			errMsg = err.Error()
		}
		return CheckResult{Name: "go mod tidy", Passed: false, Error: errMsg}
	}

	// Compare with originals
	newMod, _ := os.ReadFile(modPath)
	newSum, _ := os.ReadFile(sumPath)

	modChanged := string(origMod) != string(newMod)
	sumChanged := string(origSum) != string(newSum)

	// Always restore originals (validation should be non-destructive)
	_ = os.WriteFile(modPath, origMod, 0o644)
	if origSum != nil {
		_ = os.WriteFile(sumPath, origSum, 0o644)
	} else {
		// go.sum didn't exist before; if tidy created it, remove it
		if sumChanged {
			_ = os.Remove(sumPath)
		}
	}

	if modChanged || sumChanged {
		return CheckResult{Name: "go mod tidy", Passed: false, Error: "go.mod or go.sum is not tidy"}
	}
	return CheckResult{Name: "go mod tidy", Passed: true}
}

func buildValidationBinary(dir, cliName string) (path string, cleanup func(), err error) {
	tempDir, err := os.MkdirTemp(dir, ".publish-validate-*")
	if err != nil {
		return "", nil, err
	}

	cleanup = func() {
		_ = os.RemoveAll(tempDir)
	}

	outPath := platform.ExecutablePath(filepath.Join(tempDir, cliName))
	if err := buildBinaryAtPath(dir, outPath, "./cmd/"+cliName); err == nil {
		return outPath, cleanup, nil
	}
	if err := buildBinaryAtPath(dir, outPath, "."); err == nil {
		return outPath, cleanup, nil
	}

	cleanup()
	return "", nil, fmt.Errorf("building validation binary")
}

func buildBinaryAtPath(dir, outPath, pkg string) error {
	ctx, cancel := context.WithTimeout(context.Background(), goCommandTimeout)
	defer cancel()
	buildCmd := exec.CommandContext(ctx, "go", "build", "-o", outPath, pkg)
	buildCmd.Dir = dir
	return buildCmd.Run()
}

func buildArtifactCandidates(dir, cliName string) []string {
	return []string{
		filepath.Join(dir, cliName),
		filepath.Join(dir, "cmd", cliName, cliName),
	}
}

// stagedBinaryNames returns the root-level filenames that local builds
// (`go build ./cmd/...`, `make build`, `make build-mcp` without `-o`)
// drop alongside the source tree. The Makefile template emits
// `<api-slug>-pp-cli` and `<api-slug>-pp-mcp` peers, and verifier paths
// may also leave a bare `<api-slug>` artifact. The list is intentionally
// a small, named superset rather than a glob so the strip step never
// removes a tracked source file by accident.
func stagedBinaryNames(cliName, apiSlug string) []string {
	seen := map[string]struct{}{}
	var names []string
	add := func(n string) {
		if n == "" {
			return
		}
		if _, ok := seen[n]; ok {
			return
		}
		seen[n] = struct{}{}
		names = append(names, n)
	}
	add(apiSlug)
	add(cliName)
	if apiSlug != "" {
		add(apiSlug + "-pp-cli")
		add(apiSlug + "-pp-mcp")
	}
	return names
}

func stagedShipcheckReportNames() []string {
	return []string{"dogfood-results.json", "workflow-verify-report.json"}
}

type fileSnapshot struct {
	path    string
	exists  bool
	mode    os.FileMode
	content []byte
}

func snapshotFiles(paths []string) func() {
	snapshots := make([]fileSnapshot, 0, len(paths))
	for _, path := range paths {
		snap := fileSnapshot{path: path}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			content, readErr := os.ReadFile(path)
			if readErr == nil {
				snap.exists = true
				snap.mode = info.Mode()
				snap.content = content
			}
		}
		snapshots = append(snapshots, snap)
	}

	return func() {
		for _, snap := range snapshots {
			if snap.exists {
				if err := os.WriteFile(snap.path, snap.content, snap.mode); err == nil {
					_ = os.Chmod(snap.path, snap.mode)
				}
				continue
			}
			_ = os.Remove(snap.path)
		}
	}
}

func findMostRecentRun(msAPIDir string) (string, error) {
	entries, err := os.ReadDir(msAPIDir)
	if err != nil {
		return "", err
	}

	var runs []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			// Only include runs that actually contain files
			if hasContent(filepath.Join(msAPIDir, e.Name())) {
				runs = append(runs, e.Name())
			}
		}
	}

	if len(runs) == 0 {
		return "", nil
	}

	// Lexicographic sort (run-ids are timestamp-prefixed)
	sort.Strings(runs)
	return runs[len(runs)-1], nil
}

// resolveManuscriptDir attempts to find the manuscripts directory for an API
// when the exact apiName doesn't match a directory. The generator and the skill
// may use different slugs (e.g., "steam-web" vs "steam"). This function tries:
//  1. Strip common suffixes: -web, -api, -service
//  2. Scan directories for prefix matches (e.g., "steam" is a prefix of "steam-web")
//
// Returns the resolved directory path and the run ID, or empty strings if not found.
func resolveManuscriptDir(msRoot, apiName string) (string, string) {
	// Try stripping common suffixes
	suffixes := []string{"-web", "-api", "-service", "-public", "-v2", "-v3"}
	for _, suffix := range suffixes {
		candidate := strings.TrimSuffix(apiName, suffix)
		if candidate != apiName {
			dir := filepath.Join(msRoot, candidate)
			if runID, err := findMostRecentRun(dir); err == nil && runID != "" {
				return dir, runID
			}
		}
	}

	// Scan directories for prefix/substring matches
	entries, err := os.ReadDir(msRoot)
	if err != nil {
		return "", ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		// Check if either is a prefix of the other WITH a hyphen boundary.
		// "steam" matches "steam-web" (prefix + hyphen) but NOT "steamgames" (no hyphen).
		if (strings.HasPrefix(apiName, name+"-") || apiName == name) ||
			(strings.HasPrefix(name, apiName+"-") || name == apiName) {
			dir := filepath.Join(msRoot, name)
			if runID, err := findMostRecentRun(dir); err == nil && runID != "" {
				return dir, runID
			}
		}
	}

	return "", ""
}

// hasContent checks if a directory contains at least one non-directory entry,
// recursively. Returns false for empty directories or directories containing
// only empty subdirectories.
func hasContent(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			return true
		}
		if hasContent(filepath.Join(dir, e.Name())) {
			return true
		}
	}
	return false
}

func stagedPackageCookieNames(dir string) ([]string, error) {
	manifest, err := pipeline.ReadToolsManifest(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(manifest.Auth.Type)) {
	case "cookie", "composed":
		return manifest.Auth.Cookies, nil
	default:
		return nil, nil
	}
}

// formatCombinedScanError composes the publish-time error message from
// both scanners. Sections appear in fixed order: vendor-prefix tokens,
// then PII pending findings, then PII gate failures. Returns nil when
// nothing to report so callers can branch on the error directly.
func formatCombinedScanError(
	secretFindings []artifacts.VendorPrefixSecretFinding,
	piiFindings []artifacts.PIIFinding,
	piiCompletion artifacts.PIICompletionStatus,
) error {
	var sections []string

	if len(secretFindings) > 0 {
		sections = append(sections,
			"vendor-prefix tokens detected in staged package:\n"+
				artifacts.FormatVendorPrefixSecretFindings(secretFindings))
	}
	if artifacts.PIIPendingCount(piiFindings) > 0 {
		sections = append(sections,
			"customer PII detected in staged package:\n"+
				artifacts.FormatPIIFindings(piiFindings))
	}
	if piiCompletion.HasGateFailure() {
		sections = append(sections,
			"PII gate failures:\n"+
				artifacts.FormatPIIGateFailures(piiCompletion))
	}
	if len(sections) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(sections, "\n\n"))
}
