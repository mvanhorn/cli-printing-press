package main

import (
	"fmt"
	"os"
)

func main() {
	args := os.Args[1:]
	switch {
	case len(args) == 0 || args[0] == "--help":
		fmt.Println("Usage:\n  structural-fail [command]\n\nAvailable Commands:\n  broken   Broken command\n\nFlags:\n  --help   help for structural-fail")
	case args[0] == "broken":
		os.Exit(1)
	case args[0] == "version" || args[0] == "--version":
		fmt.Println("structural-fail 1.0.0")
	default:
		os.Exit(1)
	}
}
