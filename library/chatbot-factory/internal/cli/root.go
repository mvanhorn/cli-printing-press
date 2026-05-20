// internal/cli/root.go
package cli

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"chatbot-factory-pp-cli/internal/store"
)

// version is set at build time via -ldflags "-X chatbot-factory-pp-cli/internal/cli.version=..."
var version = "dev"

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
		Use:          "chatbot-factory-pp-cli",
		Short:        "From a knowledge base to a production RAG chatbot, one command at a time.",
		SilenceUsage: true,
		Version:      version,
	}
	root.SetVersionTemplate("chatbot-factory-pp-cli {{ .Version }}\n")

	root.AddCommand(newVersionCliCmd())
	root.AddCommand(newProjectsCmd())
	root.AddCommand(newStyleCmd())
	root.AddCommand(newEnvCmd())
	root.AddCommand(newPipelineCmd())
	root.AddCommand(newMigrateCmd())

	return root
}

// Execute runs the CLI and returns any error.
func Execute() error {
	dbPath, _ := store.DefaultPath()
	_ = store.MaybeMigrateLegacy(dbPath)
	return newRootCmd().Execute()
}

func newVersionCliCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version",
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOut {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]string{
					"version": version,
					"name":    "chatbot-factory-pp-cli",
				})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "chatbot-factory-pp-cli %s\n", version)
			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit machine-readable JSON")
	return cmd
}
