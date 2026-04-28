package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v2/internal/pipeline"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func newBundleCmd() *cobra.Command {
	var output string
	var platform string
	var skipBuild bool
	var binaryPath string
	var cliSkipBuild bool
	var cliBinaryPath string

	cmd := &cobra.Command{
		Use:   "bundle <cli-dir>",
		Short: "Package a printed CLI's MCP server as a .mcpb bundle",
		Long: `Package a printed CLI's MCP server as an MCPB v0.3 bundle (.mcpb ZIP)
suitable for drag-drop install in Claude Desktop, Claude Code, MCP for
Windows, or any MCPB-aware host.

The CLI directory must contain manifest.json (emitted automatically by
` + "`printing-press generate`" + `). bundle compiles the MCP binary for the
target platform via ` + "`go build`" + ` and ZIPs it with the manifest.

Use --platform to cross-compile for a different host. Default is the
current host (e.g., darwin/arm64). Use --skip-build with --binary to
package an already-built binary instead of recompiling.

` + "`printing-press generate`" + ` runs this automatically for the host platform
on each generation; you only need to invoke ` + "`bundle`" + ` directly to
cross-compile, rebuild after manual edits, or pull a pre-built binary
from another build pipeline.`,
		Example: `  printing-press bundle ~/printing-press/library/marketing/dub
  printing-press bundle ~/printing-press/library/marketing/dub --platform linux/amd64
  printing-press bundle ./generated/notion --output /tmp/notion.mcpb
  printing-press bundle ./generated/dub --skip-build --binary ./prebuilt/dub-pp-mcp`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliDir, err := filepath.Abs(args[0])
			if err != nil {
				return fmt.Errorf("resolving cli dir: %w", err)
			}

			info, err := os.Stat(cliDir)
			if err != nil || !info.IsDir() {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("%s is not a directory", cliDir)}
			}

			manifestPath := filepath.Join(cliDir, pipeline.MCPBManifestFilename)
			manifestData, err := os.ReadFile(manifestPath)
			if err != nil {
				return &ExitError{
					Code: ExitInputError,
					Err:  fmt.Errorf("reading manifest.json (run `printing-press generate` first): %w", err),
				}
			}
			var manifest pipeline.MCPBManifest
			if err := json.Unmarshal(manifestData, &manifest); err != nil {
				return fmt.Errorf("parsing manifest.json: %w", err)
			}
			if manifest.Name == "" {
				return fmt.Errorf("manifest.name is empty; cannot determine binary name")
			}

			goos, goarch, err := resolvePlatform(platform)
			if err != nil {
				return &ExitError{Code: ExitInputError, Err: err}
			}

			if binaryPath == "" {
				binaryPath = pipeline.StagedMCPBinaryPath(cliDir, manifest.Name)
			}
			if cliBinaryPath == "" && manifest.CLIBinary != "" {
				cliBinaryPath = pipeline.StagedMCPBinaryPath(cliDir, manifest.CLIBinary)
			}
			if err := buildBundleBinaries(cliDir, manifest.Name, binaryPath, skipBuild, manifest.CLIBinary, cliBinaryPath, cliSkipBuild, goos, goarch); err != nil {
				return err
			}

			if output == "" {
				output = pipeline.DefaultBundleOutputPath(cliDir, manifest.Name, goos, goarch)
			}

			if err := pipeline.BuildMCPBBundle(pipeline.BundleParams{
				CLIDir:        cliDir,
				BinaryPath:    binaryPath,
				CLIBinaryPath: cliBinaryPath,
				OutputPath:    output,
			}); err != nil {
				return fmt.Errorf("packaging bundle: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", output)
			return nil
		},
	}

	cmd.Flags().StringVar(&output, "output", "", "Output .mcpb path (default: <dir>/build/<name>-<os>-<arch>.mcpb)")
	cmd.Flags().StringVar(&platform, "platform", "", "Target platform as <os>/<arch> (default: host)")
	cmd.Flags().BoolVar(&skipBuild, "skip-build", false, "Skip go build for the MCP binary; use the binary at --binary")
	cmd.Flags().StringVar(&binaryPath, "binary", "", "Pre-built MCP binary path (only meaningful with --skip-build)")
	cmd.Flags().BoolVar(&cliSkipBuild, "cli-skip-build", false, "Skip go build for the companion CLI binary; use the binary at --cli-binary")
	cmd.Flags().StringVar(&cliBinaryPath, "cli-binary", "", "Pre-built CLI binary path (only meaningful with --cli-skip-build)")
	return cmd
}

// autoBundleForHost packages a host-platform .mcpb after generate.
// Best-effort: skips silently for expected non-bundle states (no manifest,
// no go.sum) and warns on real failures (malformed manifest, build/zip
// errors). Users can always re-run via `printing-press bundle <dir>`.
func autoBundleForHost(cliDir string, w io.Writer) {
	manifestPath := filepath.Join(cliDir, pipeline.MCPBManifestFilename)
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		// "No manifest" means generate decided this CLI doesn't ship as
		// a bundle (no MCP binary). That's expected — silent.
		return
	}
	var manifest pipeline.MCPBManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		// A manifest that exists but doesn't parse is a real problem
		// the user should know about — corruption, partial write, manual
		// edit gone wrong. Surface it.
		fmt.Fprintf(w, "warning: skipping bundle — manifest.json is not valid JSON: %v\n", err)
		return
	}
	if manifest.Name == "" {
		fmt.Fprintf(w, "warning: skipping bundle — manifest.json has empty name\n")
		return
	}
	// Skip silently when the generated module hasn't run `go mod tidy` yet
	// (e.g. generate --validate=false). Bundling requires a buildable module;
	// otherwise the user gets a guaranteed-fail build and a confusing
	// warning. They can run `printing-press bundle <dir>` after tidying.
	if _, err := os.Stat(filepath.Join(cliDir, "go.sum")); err != nil {
		return
	}
	binaryPath := pipeline.StagedMCPBinaryPath(cliDir, manifest.Name)
	cliBinaryPath := ""
	if manifest.CLIBinary != "" {
		cliBinaryPath = pipeline.StagedMCPBinaryPath(cliDir, manifest.CLIBinary)
	}
	if err := buildBundleBinaries(cliDir, manifest.Name, binaryPath, false, manifest.CLIBinary, cliBinaryPath, false, runtime.GOOS, runtime.GOARCH); err != nil {
		fmt.Fprintf(w, "warning: %v\n", err)
		return
	}
	output := pipeline.DefaultBundleOutputPath(cliDir, manifest.Name, runtime.GOOS, runtime.GOARCH)
	if err := pipeline.BuildMCPBBundle(pipeline.BundleParams{
		CLIDir:        cliDir,
		BinaryPath:    binaryPath,
		CLIBinaryPath: cliBinaryPath,
		OutputPath:    output,
	}); err != nil {
		fmt.Fprintf(w, "warning: could not package MCPB bundle: %v\n", err)
		return
	}
	fmt.Fprintf(w, "Bundled %s\n", output)
}

// buildBundleBinaries compiles the MCP and (optionally) CLI binaries
// concurrently. Go's build cache is concurrency-safe and the two packages
// are disjoint, so running serial doubles bundle latency for no reason.
// Returns once both finish; the first error wins.
func buildBundleBinaries(cliDir, mcpName, mcpPath string, skipMCP bool, cliName, cliPath string, skipCLI bool, goos, goarch string) error {
	var g errgroup.Group
	if !skipMCP {
		g.Go(func() error {
			if err := buildMCPBBinary(cliDir, mcpName, mcpPath, goos, goarch); err != nil {
				return fmt.Errorf("building MCP binary: %w", err)
			}
			return nil
		})
	}
	if !skipCLI && cliName != "" {
		g.Go(func() error {
			if err := buildMCPBBinary(cliDir, cliName, cliPath, goos, goarch); err != nil {
				return fmt.Errorf("building CLI binary: %w", err)
			}
			return nil
		})
	}
	return g.Wait()
}

// resolvePlatform parses an optional "<os>/<arch>" string and falls back
// to the host's GOOS/GOARCH. Returns a useful error message when the
// caller passes a malformed value rather than silently defaulting.
func resolvePlatform(s string) (string, string, error) {
	if s == "" {
		return runtime.GOOS, runtime.GOARCH, nil
	}
	parts := strings.SplitN(s, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("--platform must be <os>/<arch>, got %q", s)
	}
	return parts[0], parts[1], nil
}

// buildMCPBBinary invokes `go build` on cmd/<name>/main.go inside cliDir,
// targeting the requested GOOS/GOARCH and writing to outputPath. We pass
// -trimpath and -ldflags="-s -w" to match the bundle conventions Claude
// Desktop's prebuilt examples use; users who need debug builds can
// --skip-build and pass their own binary.
func buildMCPBBinary(cliDir, mcpName, outputPath, goos, goarch string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("creating bin dir: %w", err)
	}
	pkg := "./cmd/" + mcpName
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", outputPath, pkg)
	cmd.Dir = cliDir
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("go build %s: %w\n%s", pkg, err, string(out))
	}
	return nil
}
