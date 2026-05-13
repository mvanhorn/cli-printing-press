package pressauth

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStatusCmd(_ *GlobalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status <domain>",
		Short: "Report whether the captured session for <domain> is still valid",
		Long: `Decodes the stored JWT carrier cookie and prints a human-readable
freshness summary, e.g. "valid until 2026-05-12T18:24:00Z (29m remaining)"
or "expired 14m ago — run press-auth refresh".

Exit codes:
  0  state is captured and the JWT is not expired
  2  no captured state for <domain>
  3  state exists but the JWT has expired and a refresh is needed`,
		Example: `  press-auth status example.com
  press-auth status example.com --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			domain := args[0]
			if domain == "" {
				return &ExitError{Code: ExitUsageError, Err: fmt.Errorf("domain argument cannot be empty")}
			}
			return notImplementedExit("status")
		},
	}

	return cmd
}
