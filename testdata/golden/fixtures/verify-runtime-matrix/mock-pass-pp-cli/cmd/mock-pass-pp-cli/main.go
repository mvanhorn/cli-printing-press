package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	args := os.Args[1:]
	switch {
	case len(args) == 0 || args[0] == "--help":
		fmt.Println("Usage:\n  mock-pass [command]\n\nAvailable Commands:\n  items   List items\n\nFlags:\n  --help   help for mock-pass")
	case args[0] == "items":
		return
	case args[0] == "sync":
		return
	case args[0] == "health":
		return
	case args[0] == "sql":
		if strings.Contains(strings.Join(args[1:], " "), "sqlite_master") {
			fmt.Println("items")
			return
		}
		fmt.Println("1")
	case args[0] == "version" || args[0] == "--version":
		fmt.Println("mock-pass 1.0.0")
	default:
		os.Exit(1)
	}
}
