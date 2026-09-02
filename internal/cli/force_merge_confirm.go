package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline/regenmerge"
)

const forceHandAuthoredDeleteWarning = "warning: --force would delete non-generator-owned files:"

func formatForceHandAuthoredDeleteWarning(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(forceHandAuthoredDeleteWarning)
	b.WriteByte('\n')
	for _, p := range paths {
		b.WriteString("  ")
		b.WriteString(p)
		b.WriteByte('\n')
	}
	return b.String()
}

func confirmForceHandAuthoredDeletes(yes bool, paths []string, in io.Reader, out io.Writer) error {
	if len(paths) == 0 {
		return nil
	}
	fmt.Fprint(out, formatForceHandAuthoredDeleteWarning(paths))
	if yes {
		return nil
	}
	if !forceConfirmIsInteractive(in) {
		return &ExitError{
			Code: ExitInputError,
			Err:  fmt.Errorf("refusing to delete non-generator-owned files without confirmation — pass --yes to confirm"),
		}
	}
	fmt.Fprint(out, "delete the listed non-generator-owned files? [y/N]: ")
	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return &ExitError{Code: ExitUnknownError, Err: fmt.Errorf("reading confirmation: %w", err)}
	}
	answer := strings.TrimSpace(strings.ToLower(line))
	if answer == "" {
		return &ExitError{
			Code: ExitInputError,
			Err:  fmt.Errorf("refusing to delete non-generator-owned files without confirmation — pass --yes to confirm"),
		}
	}
	if answer == "y" || answer == "yes" {
		return nil
	}
	return &ExitError{Code: ExitInputError, Err: fmt.Errorf("cancelled — non-generator-owned files left intact")}
}

func forceConfirmIsInteractive(in io.Reader) bool {
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

func restoreForceSnapshot(freshDir, snapshotDir string) error {
	if err := replaceTree(freshDir, snapshotDir); err != nil {
		return fmt.Errorf("restoring tree after refused hand-authored delete: %w; snapshot preserved at %s", err, snapshotDir)
	}
	if err := os.RemoveAll(snapshotDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not remove snapshot dir %s: %v\n", snapshotDir, err)
	}
	return nil
}

func confirmOrRestoreForceDrops(snapshotDir, freshDir string, report *regenmerge.MergeReport, opts regenmerge.Options, yes bool) error {
	dropped := regenmerge.MarkerlessFilesWouldDrop(snapshotDir, freshDir, report, opts)
	if err := confirmForceHandAuthoredDeletes(yes, dropped, os.Stdin, os.Stderr); err != nil {
		if restoreErr := restoreForceSnapshot(freshDir, snapshotDir); restoreErr != nil {
			return wrapKeepingExitClass(err, restoreErr)
		}
		return err
	}
	return nil
}
