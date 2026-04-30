package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/mvanhorn/cli-printing-press/v3/internal/pipeline"
	"github.com/spf13/cobra"
)

func newLockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock",
		Short: "Manage build locks for parallel safety",
		Example: `  # Acquire a lock for a CLI build
  printing-press lock acquire --cli notion-pp-cli --scope my-workspace

  # Check lock status
  printing-press lock status --cli notion-pp-cli --json

  # Update heartbeat
  printing-press lock update --cli notion-pp-cli --phase build

  # Release a lock
  printing-press lock release --cli notion-pp-cli

  # Promote working dir to library
  printing-press lock promote --cli notion-pp-cli --dir /path/to/working`,
	}

	cmd.AddCommand(newLockAcquireCmd())
	cmd.AddCommand(newLockUpdateCmd())
	cmd.AddCommand(newLockStatusCmd())
	cmd.AddCommand(newLockReleaseCmd())
	cmd.AddCommand(newLockPromoteCmd())

	return cmd
}

func newLockAcquireCmd() *cobra.Command {
	var cliName string
	var scope string
	var force bool

	cmd := &cobra.Command{
		Use:   "acquire",
		Short: "Acquire a build lock for a CLI",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cliName == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--cli is required")}
			}
			if scope == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--scope is required")}
			}

			lock, err := pipeline.AcquireLock(cliName, scope, force)
			if err != nil {
				// Check if it's a "lock held" error — return structured JSON + non-zero exit.
				status := pipeline.LockStatus(cliName)
				result := map[string]any{
					"acquired": false,
					"blocked":  true,
					"error":    err.Error(),
					"status":   status,
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				_ = enc.Encode(result)
				return &ExitError{Code: ExitInputError, Err: err, Silent: true}
			}

			result := map[string]any{
				"acquired":    true,
				"blocked":     false,
				"cli":         cliName,
				"scope":       lock.Scope,
				"lock_file":   pipeline.LockFilePath(cliName),
				"acquired_at": lock.AcquiredAt,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		},
	}

	cmd.Flags().StringVar(&cliName, "cli", "", "CLI name (required)")
	cmd.Flags().StringVar(&scope, "scope", "", "Workspace scope (required)")
	cmd.Flags().BoolVar(&force, "force", false, "Override fresh locks from other scopes")

	return cmd
}

func newLockUpdateCmd() *cobra.Command {
	var cliName string
	var phase string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update lock heartbeat and phase",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cliName == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--cli is required")}
			}
			if phase == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--phase is required")}
			}

			if err := pipeline.UpdateLock(cliName, phase); err != nil {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("updating lock: %w", err)}
			}

			result := map[string]any{
				"updated": true,
				"cli":     cliName,
				"phase":   phase,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		},
	}

	cmd.Flags().StringVar(&cliName, "cli", "", "CLI name (required)")
	cmd.Flags().StringVar(&phase, "phase", "", "Current build phase (required)")

	return cmd
}

func newLockStatusCmd() *cobra.Command {
	var cliName string
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check lock status for a CLI",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cliName == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--cli is required")}
			}

			status := pipeline.LockStatus(cliName)

			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(status)
			}

			if !status.Held {
				if status.HasCLI {
					fmt.Fprintf(os.Stderr, "No active lock for %s (completed CLI exists in library)\n", cliName)
				} else {
					fmt.Fprintf(os.Stderr, "No active lock for %s\n", cliName)
				}
				return nil
			}

			staleStr := ""
			if status.Stale {
				staleStr = " (STALE)"
			}
			fmt.Fprintf(os.Stderr, "Lock held for %s%s\n", cliName, staleStr)
			fmt.Fprintf(os.Stderr, "  Scope: %s\n", status.Scope)
			fmt.Fprintf(os.Stderr, "  Phase: %s\n", status.Phase)
			fmt.Fprintf(os.Stderr, "  Age:   %.0fs\n", status.AgeSeconds)

			return nil
		},
	}

	cmd.Flags().StringVar(&cliName, "cli", "", "CLI name (required)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")

	return cmd
}

func newLockReleaseCmd() *cobra.Command {
	var cliName string

	cmd := &cobra.Command{
		Use:   "release",
		Short: "Release a build lock",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cliName == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--cli is required")}
			}

			if err := pipeline.ReleaseLock(cliName); err != nil {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("releasing lock: %w", err)}
			}

			result := map[string]any{
				"released": true,
				"cli":      cliName,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		},
	}

	cmd.Flags().StringVar(&cliName, "cli", "", "CLI name (required)")

	return cmd
}

func newLockPromoteCmd() *cobra.Command {
	var cliName string
	var dir string

	cmd := &cobra.Command{
		Use:   "promote",
		Short: "Promote a working CLI to the library",
		Long:  "Copies the working CLI to the library, writes the CLI manifest, updates the run pointer, and releases the lock.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cliName == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--cli is required")}
			}
			if dir == "" {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--dir is required")}
			}

			// Verify the working directory exists.
			info, err := os.Stat(dir)
			if err != nil {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("working directory not found: %w", err)}
			}
			if !info.IsDir() {
				return &ExitError{Code: ExitInputError, Err: fmt.Errorf("--dir must be a directory")}
			}

			// Try to find state by working dir. For plan-driven CLIs that
			// skipped generate, no runstate entry exists - fall back to a
			// minimal state so promote still works.
			state, err := pipeline.FindStateByWorkingDir(dir)
			if err != nil {
				state = pipeline.NewMinimalState(cliName, dir)
			}

			if err := pipeline.PromoteWorkingCLI(cliName, dir, state); err != nil {
				return &ExitError{Code: ExitGenerationError, Err: fmt.Errorf("promoting CLI: %w", err)}
			}

			result := map[string]any{
				"promoted":    true,
				"cli":         cliName,
				"library_dir": state.PublishedDir,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		},
	}

	cmd.Flags().StringVar(&cliName, "cli", "", "CLI name (required)")
	cmd.Flags().StringVar(&dir, "dir", "", "Working CLI directory to promote (required)")

	return cmd
}
