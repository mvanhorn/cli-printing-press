package main

import (
	"context"
	"fmt"
	"os"

	"github.com/mvanhorn/cli-printing-press/v4/internal/devicesniff/bleprobe"
)

func main() {
	if err := bleprobe.Execute(context.Background(), "ble-probe"); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
