package main

import (
	"fmt"
	"os"
)

func main() {
	args := os.Args[1:]
	switch {
	case len(args) == 0 || args[0] == "--help":
		fmt.Println("Usage:\n  structural-pass [command]\n\nAvailable Commands:\n  items   List items\n\nFlags:\n  --help   help for structural-pass")
	case args[0] == "items":
		return
	case args[0] == "version" || args[0] == "--version":
		fmt.Println("structural-pass 1.0.0")
	default:
		os.Exit(1)
	}
}
