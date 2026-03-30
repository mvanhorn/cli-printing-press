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

	catalogpkg "github.com/mvanhorn/cli-printing-press/internal/catalog"
	"github.com/mvanhorn/cli-printing-press/internal/naming"
	"github.com/mvanhorn/cli-printing-press/internal/pipeline"
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
  printing-press publish validate --dir ~/printing-press/library/notion-pp-cli --json

  # Package a CLI for publishing
  printing-press publish package --dir ~/printing-press/library/notion-pp-cli --category productivity --target /tmp/staging --json`,
	}

	cmd.AddCommand(newPublishValidateCmd())
	cmd.AddCommand(newPublishPackageCmd())

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

func newPublishValidateCmd() *cobra.Command {
	var dir string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate a CLI is ready for publishing",
		Example: `  printing-press publish validate --dir ~/printing-press/library/notion-pp-cli
  printing-press publish validate --dir ~/printing-press/library/notion-pp-cli --json`,
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
	var modulePath string
	var asJSON bool

	cmd := &cobra.Command{
		Use:     "package",
		Short:   "Package a CLI for publishing to the library repo",
		Example: `  printing-press publish package --dir ~/printing-press/library/notion-pp-cli --category productivity --target /tmp/staging --json`,
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
			if target == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--target is required")}
			}

			// Check target doesn't exist (before expensive validation)
			if _, err := os.Stat(target); err == nil {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("target directory already exists: %s", target)}
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

			// Build staging structure: target/library/<category>/<cli-name>/
			stagingCLIDir := filepath.Join(target, "library", category, cliName)

			// Verify the resolved path is actually under target (defense in depth)
			absTarget, _ := filepath.Abs(target)
			absStaging, _ := filepath.Abs(stagingCLIDir)
			if !strings.HasPrefix(absStaging, absTarget+string(filepath.Separator)) {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("resolved staging path %s escapes target directory %s", absStaging, absTarget)}
			}

			cleanupTarget := func() {
				_ = os.RemoveAll(target)
			}

			if err := os.MkdirAll(filepath.Dir(stagingCLIDir), 0o755); err != nil {
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("creating staging dir: %w", err)}
			}

			// Copy CLI source
			if err := pipeline.CopyDir(dir, stagingCLIDir); err != nil {
				cleanupTarget()
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("copying CLI: %w", err)}
			}

			// Rewrite go.mod module path if --module-path is set
			if modulePath != "" {
				oldModPath := cliName // generated CLIs use bare CLI name as module path
				if err := pipeline.RewriteModulePath(stagingCLIDir, oldModPath, modulePath); err != nil {
					cleanupTarget()
					return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("rewriting module path: %w", err)}
				}
			}

			// Resolve and copy manuscripts
			result := PackageResult{
				StagedDir:  stagingCLIDir,
				CLIName:    cliName,
				APIName:    vResult.APIName,
				Category:   category,
				ModulePath: modulePath,
			}

			apiName := vResult.APIName
			if apiName == "" {
				apiName = naming.TrimCLISuffix(cliName)
			}

			msRoot := pipeline.PublishedManuscriptsRoot()
			msAPIDir := filepath.Join(msRoot, apiName)
			runID, err := findMostRecentRun(msAPIDir)
			if err == nil && runID != "" {
				result.RunID = runID
				srcMsDir := filepath.Join(msAPIDir, runID)
				dstMsDir := filepath.Join(stagingCLIDir, ".manuscripts", runID)
				if err := pipeline.CopyDir(srcMsDir, dstMsDir); err != nil {
					cleanupTarget()
					return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("copying manuscripts: %w", err)}
				} else {
					result.ManuscriptsIncluded = true
				}
			} else {
				fmt.Fprintln(os.Stderr, "warning: no manuscripts found, packaging without them")
			}

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			fmt.Fprintf(os.Stderr, "Packaged %s at %s\n", cliName, stagingCLIDir)
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
	cmd.Flags().StringVar(&target, "target", "", "Staging directory to create (required)")
	cmd.Flags().StringVar(&modulePath, "module-path", "", "Go module path to set (e.g., github.com/org/repo/library/category/cli-name)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}

func runValidation(dir string) ValidateResult {
	result := ValidateResult{}
	allPassed := true

	// 1. Manifest check
	manifestPath := filepath.Join(dir, pipeline.CLIManifestFilename)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		result.Checks = append(result.Checks, CheckResult{Name: "manifest", Passed: false, Error: "missing .printing-press.json"})
		allPassed = false
	} else {
		var m pipeline.CLIManifest
		if err := json.Unmarshal(data, &m); err != nil {
			result.Checks = append(result.Checks, CheckResult{Name: "manifest", Passed: false, Error: fmt.Sprintf("invalid JSON: %v", err)})
			allPassed = false
		} else {
			if m.APIName == "" || m.CLIName == "" {
				result.Checks = append(result.Checks, CheckResult{Name: "manifest", Passed: false, Error: "missing required fields (api_name, cli_name)"})
				allPassed = false
			} else {
				result.Checks = append(result.Checks, CheckResult{Name: "manifest", Passed: true})
				result.CLIName = m.CLIName
				result.APIName = m.APIName
			}
		}
	}

	cliName := result.CLIName
	if cliName == "" {
		cliName = filepath.Base(dir)
	}

	restoreBuildArtifacts := snapshotFiles(buildArtifactCandidates(dir, cliName))
	defer restoreBuildArtifacts()

	// 2. go mod tidy check — snapshot files, run tidy, compare, restore
	tidyCheck := checkGoModTidy(dir)
	if !tidyCheck.Passed {
		allPassed = false
	}
	result.Checks = append(result.Checks, tidyCheck)

	// 3. go vet check
	vetCheck := runGoCheck(dir, "vet", "./...")
	if !vetCheck.Passed {
		allPassed = false
	}
	result.Checks = append(result.Checks, vetCheck)

	// 4. go build check
	buildCheck := runGoCheck(dir, "build", "./...")
	if !buildCheck.Passed {
		allPassed = false
	}
	result.Checks = append(result.Checks, buildCheck)

	// 5. --help / --version checks use a dedicated temp binary so validation
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

		// 6. --version check
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

	// 7. Manuscripts check (warn-only)
	apiName := result.APIName
	if apiName == "" {
		apiName = naming.TrimCLISuffix(cliName)
	}
	msDir := filepath.Join(pipeline.PublishedManuscriptsRoot(), apiName)
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
