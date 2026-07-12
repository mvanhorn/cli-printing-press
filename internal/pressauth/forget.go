package pressauth

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// ForgetFlags holds the forget subcommand's flag values.
type ForgetFlags struct {
	All bool
	Yes bool
}

func newForgetCmd(_ *GlobalFlags) *cobra.Command {
	ff := &ForgetFlags{}

	cmd := &cobra.Command{
		Use:   "forget [domain]",
		Short: "Remove captured state and keychain entries for a domain",
		Long: `Deletes the on-disk state file for <domain> and removes the matching
keychain entry that held its encryption key.

Use --all to forget every captured domain at once; --all requires --yes
unless run from a TTY, where it will prompt for interactive confirmation.

forget is idempotent: forgetting a domain that has no state succeeds and
prints a one-line "nothing to forget" notice.`,
		Example: `  press-auth forget example.com
  press-auth forget --all --yes`,
		Args: func(cmd *cobra.Command, args []string) error {
			if ff.All {
				if len(args) > 0 {
					return fmt.Errorf("--all cannot be combined with a domain argument")
				}
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runForget(cmd.InOrStdin(), cmd.OutOrStdout(), ff, args)
		},
	}

	cmd.Flags().BoolVar(&ff.All, "all", false, "Forget every captured domain")
	cmd.Flags().BoolVar(&ff.Yes, "yes", false, "Skip the interactive confirmation prompt (required with --all in non-TTY contexts)")

	return cmd
}

// runForget is the testable body. stdin is passed through so callers can
// inject scripted input via cmd.SetIn(strings.NewReader("y\n")); when nil,
// we treat it as a non-interactive context (no prompt possible).
func runForget(in io.Reader, out io.Writer, ff *ForgetFlags, args []string) error {
	if ff.All {
		return forgetAll(in, out, ff)
	}
	if len(args) == 0 || args[0] == "" {
		return &ExitError{Code: ExitUsageError, Err: fmt.Errorf("domain argument cannot be empty")}
	}
	return forgetOne(in, out, ff, args[0])
}

// forgetOne handles the single-domain path. Confirmation is required
// unless --yes is set; non-interactive contexts (no TTY, no scripted
// stdin) refuse and point the user at --yes.
func forgetOne(in io.Reader, out io.Writer, ff *ForgetFlags, domain string) error {
	if !ff.Yes {
		ok, err := confirmDelete(in, out, domain)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintf(out, "cancelled — %s left intact\n", domain)
			return nil
		}
	}

	existed, delErr := deleteIfPresent(domain)
	if delErr != nil {
		return &ExitError{Code: ExitUnknownError, Err: delErr}
	}
	if !existed {
		fmt.Fprintf(out, "nothing to forget for %s\n", domain)
		return nil
	}
	fmt.Fprintf(out, "forgot %s\n", domain)
	return nil
}

// forgetAll enumerates the state dir and applies the single-domain logic
// to each. --yes is mandatory in non-interactive contexts.
func forgetAll(in io.Reader, out io.Writer, ff *ForgetFlags) error {
	domains, err := listStateFiles()
	if err != nil {
		return &ExitError{Code: ExitUnknownError, Err: err}
	}
	if len(domains) == 0 {
		fmt.Fprintln(out, "no domains to forget")
		return nil
	}

	if !ff.Yes && !isInteractive(in) {
		return &ExitError{
			Code: ExitUsageError,
			Err:  fmt.Errorf("cannot prompt in non-interactive mode — use --yes to confirm"),
		}
	}

	var forgotten int
	for _, domain := range domains {
		if !ff.Yes {
			ok, confErr := confirmDelete(in, out, domain)
			if confErr != nil {
				return confErr
			}
			if !ok {
				fmt.Fprintf(out, "skipped %s\n", domain)
				continue
			}
		}
		existed, delErr := deleteIfPresent(domain)
		if delErr != nil {
			fmt.Fprintf(out, "error forgetting %s: %v\n", domain, delErr)
			continue
		}
		if existed {
			forgotten++
		}
	}
	fmt.Fprintf(out, "forgot %d of %d domains\n", forgotten, len(domains))
	return nil
}

// deleteIfPresent removes the state for domain and reports whether a file
// was actually deleted. The Delete helper is already idempotent, so we
// stat first to distinguish "existed and gone" from "never existed" for
// the user-facing message.
func deleteIfPresent(domain string) (bool, error) {
	path, err := stateFilePath(domain)
	if err != nil {
		return false, err
	}
	existed := true
	if _, statErr := os.Stat(path); statErr != nil {
		if os.IsNotExist(statErr) {
			existed = false
		} else {
			return false, statErr
		}
	}
	if err := Delete(domain); err != nil {
		// Delete is idempotent for missing files but bubbles real errors.
		// The keychain entry may have been written without a state file;
		// treat that as a real error worth surfacing.
		if !existed && isKeychainUnsupported(err) {
			return false, nil
		}
		if !existed && errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return existed, err
	}
	return existed, nil
}

// confirmDelete reads a y/N answer from stdin. In non-interactive contexts
// (stdin is nil, not a terminal, and has no buffered input), it returns
// an ExitError telling the user to pass --yes.
func confirmDelete(in io.Reader, out io.Writer, domain string) (bool, error) {
	if !isInteractive(in) {
		return false, &ExitError{
			Code: ExitUsageError,
			Err:  fmt.Errorf("refusing to delete %s without confirmation in non-interactive mode — pass --yes to confirm", domain),
		}
	}
	fmt.Fprintf(out, "forget state for %s? this removes the state file and macOS keychain entry [y/N]: ", domain)
	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, &ExitError{Code: ExitUnknownError, Err: fmt.Errorf("reading confirmation: %w", err)}
	}
	answer := strings.TrimSpace(strings.ToLower(line))
	return answer == "y" || answer == "yes", nil
}

// isInteractive reports whether prompting is possible. Three cases:
//  1. nil reader -> non-interactive. Production callers always pass
//     cmd.InOrStdin() so nil only appears in tests that want to assert
//     the no-input-source refusal path.
//  2. Explicit non-stdin reader (cmd.SetIn or a strings.Reader in a
//     test) -> interactive. We can drain it.
//  3. Reader is os.Stdin -> stat it and check ModeCharDevice (TTY).
//
// We avoid pulling in golang.org/x/term by stat-ing os.Stdin and reading
// the ModeCharDevice bit directly, matching the design note.
func isInteractive(in io.Reader) bool {
	if in == nil {
		return false
	}
	if in != os.Stdin {
		return true
	}
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
