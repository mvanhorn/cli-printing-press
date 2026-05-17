// Package pressauth implements the press-auth companion CLI.
//
// press-auth captures, stores, and serves browser cookies for the printed
// CLIs the Printing Press emits. It owns a controlled Chrome window for
// one-time login capture, persists encrypted state per-domain, and serves
// cookie headers to generated CLIs over stdout.
//
// This file wires the Cobra root command and registers subcommand stubs.
// Subcommand bodies live in their own files in this package and currently
// return an ErrNotImplemented sentinel until each unit lands its logic.
package pressauth

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Exit codes for structured error reporting.
//
// The lower codes (0-4) mirror the conventions used by the main
// printing-press binary so wrapping shells can interpret either tool
// consistently. The higher codes describe press-auth-specific failure
// modes (no captured state, expired JWT, refresh failure, etc.).
const (
	ExitSuccess         = 0
	ExitUsageError      = 1
	ExitNotCaptured     = 2
	ExitInvalidJWT      = 3
	ExitRefreshFailed   = 4
	ExitNoNewCookies    = 5
	ExitMissingEndpoint = 6
	ExitUnknownError    = 7
)

// ErrNotImplemented is returned by subcommand stubs until U2-U5 fill in
// their bodies. Tests assert on its presence to confirm the CLI surface is
// wired without depending on any real behaviour.
var ErrNotImplemented = errors.New("not implemented")

// ExitError wraps an error with a specific exit code.
// When Silent is true, main should exit with the code but not print the
// error message — used when structured output (--json) already contains
// the failure details.
type ExitError struct {
	Code   int
	Err    error
	Silent bool
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

// GlobalFlags holds the root-level flags shared by every subcommand.
// Subcommands read from this struct to keep flag wiring centralized.
type GlobalFlags struct {
	JSON       bool
	Quiet      bool
	ConfigPath string
}

// Execute runs the root command using os.Args. main calls this and maps
// the returned error into the process exit code.
func Execute() error {
	return NewRootCmd().Execute()
}

// NewRootCmd builds the press-auth Cobra tree. Exposed for tests so they
// can drive subcommands via cmd.SetArgs without shelling out.
func NewRootCmd() *cobra.Command {
	gf := &GlobalFlags{}

	rootCmd := &cobra.Command{
		Use:   "press-auth",
		Short: "Capture and serve browser cookies for printed CLIs",
		Long: `press-auth is the cookie-capture companion for Printing Press CLIs.

It owns a controlled Chrome window for one-time interactive login, stores
the captured session per-domain (cookie values encrypted at rest with a
keychain-held key), and serves cookie headers to generated CLIs on demand.

Typical first-time use:

    press-auth login example.com --login-url https://example.com/login

After login, every printed CLI that supports cookie or composed auth for
example.com will pull its cookie header by shelling out to:

    press-auth cookies example.com

State lives under ~/.press-auth/. macOS Keychain holds the per-domain
encryption keys. On first write you'll see a one-time "Always Allow"
prompt per domain — click Always Allow to skip future prompts.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			if gf.ConfigPath == "" {
				return nil
			}
			if err := os.Setenv(stateHomeEnv, gf.ConfigPath); err != nil {
				return &ExitError{Code: ExitUsageError, Err: fmt.Errorf("setting state directory: %w", err)}
			}
			return nil
		},
	}

	rootCmd.PersistentFlags().BoolVar(&gf.JSON, "json", false, "Emit machine-readable JSON instead of human text")
	rootCmd.PersistentFlags().BoolVar(&gf.Quiet, "quiet", false, "Suppress non-essential output")
	rootCmd.PersistentFlags().StringVar(&gf.ConfigPath, "config", "", "Override the default config/state directory (~/.press-auth)")

	rootCmd.AddCommand(newLoginCmd(gf))
	rootCmd.AddCommand(newCookiesCmd(gf))
	rootCmd.AddCommand(newStatusCmd(gf))
	rootCmd.AddCommand(newRefreshCmd(gf))
	rootCmd.AddCommand(newListCmd(gf))
	rootCmd.AddCommand(newForgetCmd(gf))

	return rootCmd
}
