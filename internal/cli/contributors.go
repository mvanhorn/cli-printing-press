package cli

import (
	"fmt"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/spf13/cobra"
)

func newContributorsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "contributors",
		Short: "Manage the contributors recorded in a printed CLI's manifest",
		Long: `Contributor attribution for a printed CLI.

The 'add' subcommand records a contributor in the CLI's .printing-press.json so
they are credited in the README byline, NOTICE, and the public registry. It is
the deliberate-contribution action invoked by the publish, amend, and reprint
flows — a plain regen or sync never adds a contributor.`,
	}
	cmd.AddCommand(newContributorsAddCmd())
	return cmd
}

func newContributorsAddCmd() *cobra.Command {
	var handle, name, dir string
	var front bool

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Record a contributor in the CLI manifest (idempotent)",
		Long: `Adds a contributor to the contributors[] list in .printing-press.json.

Idempotent: the entry is skipped when it is the creator or is already listed
(matched case-insensitively by handle). With neither --handle nor --name set,
the current git identity (github.user / user.name) is used.`,
		Example: `  cli-printing-press contributors add --dir ~/printing-press/library/acme
  cli-printing-press contributors add --handle jane-doe --name "Jane Doe" --dir .`,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := spec.Person{Handle: strings.TrimSpace(handle), Name: strings.TrimSpace(name)}
			if p.IsZero() {
				p = currentGitPerson()
			}
			if p.IsZero() {
				return fmt.Errorf("no contributor identity: pass --handle/--name or set git config github.user / user.name")
			}
			added, err := pipeline.AppendContributor(dir, p, front)
			if err != nil {
				return &ExitError{Code: ExitPublishError, Err: err}
			}
			if added {
				fmt.Fprintf(cmd.OutOrStdout(), "Recorded contributor %s\n", contributorLabel(p))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "No change: %s is the creator or already a contributor\n", contributorLabel(p))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&handle, "handle", "", "Contributor GitHub @handle (defaults to git config github.user)")
	cmd.Flags().StringVar(&name, "name", "", "Contributor display name (defaults to git config user.name)")
	cmd.Flags().StringVar(&dir, "dir", ".", "CLI directory containing .printing-press.json")
	cmd.Flags().BoolVar(&front, "front", false, "List this contributor first (used by the reprint flow for the reprinter)")
	return cmd
}

// currentGitPerson resolves the running user's identity through the same
// git-then-gh fallback the publish path uses, so a contributor recorded here
// matches how publish resolves attribution (and always carries a handle).
func currentGitPerson() spec.Person {
	fb := resolveCurrentPublishAttributionFallback()
	return spec.Person{Handle: strings.TrimSpace(fb.Printer), Name: strings.TrimSpace(fb.PrinterName)}
}

func contributorLabel(p spec.Person) string {
	switch {
	case p.Name != "" && p.Handle != "":
		return fmt.Sprintf("%s (@%s)", p.Name, p.Handle)
	case p.Handle != "":
		return "@" + p.Handle
	default:
		return p.Name
	}
}
