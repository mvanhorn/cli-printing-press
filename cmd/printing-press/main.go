package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/mvanhorn/cli-printing-press/v4/internal/cli"
)

func main() {
	if isCatalogInstallerCommand(os.Args[1:]) {
		fmt.Fprintf(os.Stderr, `The "printing-press %s" command belongs to the catalog installer, not the CLI generator.

You are running the legacy generator entrypoint installed as "printing-press".
Use "cli-printing-press" for generator commands, or remove/rename the old
printing-press binary so the npm catalog installer can own this command name.

Generator install:
  go install github.com/mvanhorn/cli-printing-press/v4/cmd/cli-printing-press@latest

Catalog installer:
  npx -y @mvanhorn/printing-press %s
`, os.Args[1], os.Args[1])
		os.Exit(cli.ExitInputError)
	}

	if err := cli.ExecuteWithName(cli.LegacyBinaryName); err != nil {
		var exitErr *cli.ExitError
		if errors.As(err, &exitErr) {
			if !exitErr.Silent {
				fmt.Fprintln(os.Stderr, err.Error())
			}
			os.Exit(exitErr.Code)
		}
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(cli.ExitUnknownError)
	}
}

func isCatalogInstallerCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}

	switch args[0] {
	case "list", "search", "install":
		return true
	default:
		return false
	}
}
