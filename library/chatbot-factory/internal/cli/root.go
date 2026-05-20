// internal/cli/root.go
package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

// exitError wraps an error with a specific exit code.
type exitError struct {
	err  error
	code int
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// ExitCode returns the exit code for an error returned by Execute.
// Returns 1 for generic errors, or the code embedded by exitError.
func ExitCode(err error) int {
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	return 1
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "chatbot-factory-pp-cli",
		Short: "Chatbot Factory CLI — scaffold and manage WhatsApp/Telegram chatbot projects",
		Long: `chatbot-factory-pp-cli is the v2 CLI for the Chatbot Factory pipeline.

Pipeline commands (init, chunk, upload-rag, scaffold, env, style, deploy) will be
added in P1. This P0 release establishes the bbolt-backed project store.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(newProjectsCmd())
	root.AddCommand(newStyleCmd())
	root.AddCommand(newEnvCmd())

	return root
}

// Execute runs the CLI and returns any error.
func Execute() error {
	return newRootCmd().Execute()
}
