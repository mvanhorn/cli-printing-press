package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/mvanhorn/cli-printing-press/v4/internal/cartridge"
	"github.com/mvanhorn/cli-printing-press/v4/internal/ppcli"
)

func main() {
	if err := ppcli.Execute(); err != nil {
		if execErr, ok := errors.AsType[*cartridge.ExecError](err); ok {
			fmt.Fprintln(os.Stderr, execErr.Error())
			os.Exit(execErr.Code)
		}
		if exitErr, ok := errors.AsType[*ppcli.ExitError](err); ok {
			if !exitErr.Silent {
				fmt.Fprintln(os.Stderr, exitErr.Error())
			}
			os.Exit(exitErr.Code)
		}
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
