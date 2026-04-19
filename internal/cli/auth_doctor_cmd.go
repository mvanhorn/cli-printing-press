package cli

import (
	"fmt"

	"github.com/mvanhorn/cli-printing-press/internal/authdoctor"
	"github.com/spf13/cobra"
)

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Inspect auth state across installed printed CLIs",
		Long: `Auth tooling for the local printing-press library.

Subcommands inspect env-var status for every installed printed CLI
under ~/printing-press/library/ using each CLI's tools-manifest.json.`,
	}
	cmd.AddCommand(newAuthDoctorCmd())
	return cmd
}

func newAuthDoctorCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Report env-var status for every installed printed CLI",
		Long: `Scans ~/printing-press/library/<api>/tools-manifest.json for every
installed printed CLI and reports whether its declared env vars are set,
unset, or suspicious. Fingerprints show the first four characters of each
set value (never the full token).

Exit 0 even when findings include 'not set' or 'suspicious' — this command
is diagnostic, not gating. Exit 5 only if the scan itself fails.`,
		Example: `  printing-press auth doctor
  printing-press auth doctor --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			findings, err := authdoctor.Scan()
			if err != nil {
				return &ExitError{Code: ExitPublishError, Err: fmt.Errorf("scanning library: %w", err)}
			}

			if asJSON {
				return authdoctor.RenderJSON(cmd.OutOrStdout(), findings)
			}
			return authdoctor.RenderTable(cmd.OutOrStdout(), findings)
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}
