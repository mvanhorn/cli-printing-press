package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	catalogpkg "github.com/mvanhorn/cli-printing-press/v3/internal/catalog"
	"github.com/mvanhorn/cli-printing-press/v3/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v3/internal/pipeline"
	"github.com/spf13/cobra"
)

const (
	goCommandTimeout   = 2 * time.Minute
	binaryCheckTimeout = 15 * time.Second
)

func newPublishCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Validate and package CLIs for publishing",
		Example: `  # Validate a CLI before publishing
  printing-press publish validate --dir ~/printing-press/library/notion --json

  # Package a CLI for publishing
  printing-press publish package --dir ~/printing-press/library/notion --category productivity --target /tmp/staging --json`,
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
	var apiName string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "rename",
		Short: "Rename a staged CLI (for name collision resolution)",
		Example: `  printing-press publish rename --dir /tmp/staging/library/ai/notion --old-name notion-pp-cli --new-name notion-alt-pp-cli --json
  printing-press publish rename --dir /tmp/staging/library/ai/notion --old-name notion-pp-cli --new-name notion-2-pp-cli --api-name notion --json`,
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
			if apiName == "" {
				apiName = naming.TrimCLISuffix(oldName)
			}

			filesModified, err := pipeline.RenameCLI(dir, oldName, newName, apiName)

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
	cmd.Flags().StringVar(&apiName, "api-name", "", "Original API name (defaults to TrimCLISuffix of old-name)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}

func newPublishValidateCmd() *cobra.Command {
	var dir string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a CLI is ready for publishing",
		Example: `  printing-press publish validate --dir ~/printing-press/library/notion
  printing-press publish validate --dir ~/printing-press/library/notion --json`,
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
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "package",
		Short: "Package a CLI for publishing to the library repo",
		Example: `  # Stage into a new directory (for inspection)
  printing-press publish package --dir ~/printing-press/library/notion --category productivity --target /tmp/staging --json

  # Write directly into the publish repo (replaces old CLI, includes manuscripts)
  printing-press publish package --dir ~/printing-press/library/notion --category productivity --dest ~/printing-press/.publish-repo --json`,
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

			// Strip build/ from the staged tree. autoBundleForHost writes
			// host-platform .mcpb bundles + staged binaries there as a
			// local-dev convenience; the public library treats CI release
			// artifacts as canonical, so build/ should never reach the
			// public library through the publish path.
			if err := os.RemoveAll(filepath.Join(outCLIDir, "build")); err != nil {
				cleanupOnFailure()
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("stripping build dir: %w", err)}
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
			if runID != "" {
				result.RunID = runID
				srcMsDir := filepath.Join(msDir, runID)
				dstMsDir := filepath.Join(outCLIDir, ".manuscripts", runID)
				if err := pipeline.CopyDir(srcMsDir, dstMsDir); err != nil {
					cleanupOnFailure()
					return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("copying manuscripts: %w", err)}
				} else {
					result.ManuscriptsIncluded = true
				}
			} else {
				fmt.Fprintln(os.Stderr, "warning: no manuscripts found, packaging without them")
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

// resolveManuscripts finds the manuscripts directory and most recent run ID
// for a CLI. Tries CLI name first (new convention), then API name, then fuzzy.
func resolveManuscripts(cliName, apiName string) (msDir string, runID string) {
	if apiName == "" {
		apiName = naming.TrimCLISuffix(cliName)
	}

	msRoot := pipeline.PublishedManuscriptsRoot()

	// 1. Try CLI name (new convention: manuscripts/<cli-name>/<run>/)
	cliMsDir := filepath.Join(msRoot, cliName)
	if rid, err := findMostRecentRun(cliMsDir); err == nil && rid != "" {
		return cliMsDir, rid
	}
	// 2. Try API name (old convention: manuscripts/<api-name>/<run>/)
	apiMsDir := filepath.Join(msRoot, apiName)
	if rid, err := findMostRecentRun(apiMsDir); err == nil && rid != "" {
		return apiMsDir, rid
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
			if manifest.APIName == "" || manifest.CLIName == "" {
				result.Checks = append(result.Checks, CheckResult{Name: "manifest", Passed: false, Error: "missing required fields (api_name, cli_name)"})
				allPassed = false
			} else {
				result.Checks = append(result.Checks, CheckResult{Name: "manifest", Passed: true})
				result.CLIName = manifest.CLIName
				result.APIName = manifest.APIName
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

	// 4. go vet check
	vetCheck := runGoCheck(dir, "vet", "./...")
	if !vetCheck.Passed {
		allPassed = false
	}
	result.Checks = append(result.Checks, vetCheck)

	// 5. go build check
	buildCheck := runGoCheck(dir, "build", "./...")
	if !buildCheck.Passed {
		allPassed = false
	}
	result.Checks = append(result.Checks, buildCheck)

	// 6. --help / --version checks use a dedicated temp binary so validation
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

		// 7. --version check
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

	// 8. SKILL.md verification — fail if the agent-facing skill advertises
	// commands, flags, or arguments the shipped CLI source does not provide.
	skillCheck := checkVerifySkill(dir)
	if !skillCheck.Passed {
		allPassed = false
	}
	result.Checks = append(result.Checks, skillCheck)

	// 9. Manuscripts check (warn-only)
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
	return CheckResult{Name: "verify-skill", Passed: true}
}

func runGoCheck(dir string, args ...string) CheckResult {
	name := "go " + args[0]
	ctx, cancel := context.WithTimeout(context.Background(), goCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		errMsg := strings.TrimSpace(string(output))
		if ctx.Err() == context.DeadlineExceeded {
			errMsg = fmt.Sprintf("timed out after %s", goCommandTimeout)
		} else if errMsg == "" {
			errMsg = err.Error()
		}
		return CheckResult{Name: name, Passed: false, Error: errMsg}
	}
	return CheckResult{Name: name, Passed: true}
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

	outPath := filepath.Join(tempDir, cliName)
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
