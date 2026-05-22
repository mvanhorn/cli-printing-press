package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pressauth"
)

func main() {
	if err := pressauth.Execute(); err != nil {
		var exitErr *pressauth.ExitError
		if errors.As(err, &exitErr) {
			if !exitErr.Silent {
				fmt.Fprintln(os.Stderr, err.Error())
			}
			os.Exit(exitErr.Code)
		}
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(pressauth.ExitUnknownError)
	}
}
