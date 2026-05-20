// internal/cli/style.go
package cli

import (
	"github.com/spf13/cobra"
)

func newStyleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "style",
		Short: "Apply and list chatbot style templates",
	}
	// Subcommands (list, apply) will be added in P1.
	return cmd
}
