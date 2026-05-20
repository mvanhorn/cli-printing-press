// internal/cli/projects.go
package cli

import (
	"github.com/spf13/cobra"
)

func newProjectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "Manage chatbot projects",
	}
	// Subcommands (list, show, delete) will be added in P1.
	return cmd
}
