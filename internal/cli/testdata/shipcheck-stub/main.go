// Package main is a test stub that mimics the printing-press leg surface
// (dogfood, verify, workflow-verify, verify-skill, scorecard) for shipcheck
// orchestration tests.
//
// Behavior:
//   - The first non-flag arg is the leg name (e.g., "dogfood").
//   - If STUB_LOG_FILE is set, the stub appends its full os.Args to that
//     file (one line per invocation, tab-separated) so tests can verify
//     argv pass-through.
//   - If STUB_EXIT_<LEG_UPPER> is set (e.g., STUB_EXIT_DOGFOOD=2), the
//     stub exits with that code. Otherwise it exits 0.
//   - Some output is written to stdout/stderr so tests can verify that
//     leg output streams to the umbrella's terminal.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "stub: missing leg name")
		os.Exit(99)
	}
	leg := os.Args[1]

	// Record the invocation so tests can verify argv pass-through.
	if logFile := os.Getenv("STUB_LOG_FILE"); logFile != "" {
		f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "stub: opening log %s: %v\n", logFile, err)
			os.Exit(99)
		}
		defer f.Close()
		// One invocation per line, tab-separated argv.
		if _, err := fmt.Fprintln(f, strings.Join(os.Args, "\t")); err != nil {
			fmt.Fprintf(os.Stderr, "stub: writing log: %v\n", err)
			os.Exit(99)
		}
	}

	// Print a recognizable banner so tests can verify the leg's output
	// streamed through the umbrella to the terminal.
	fmt.Fprintf(os.Stdout, "stub running leg=%s args=%v\n", leg, os.Args[2:])

	// Stand-in for verify --fix rewriting CLI source. Shipcheck tests set
	// STUB_VERIFY_FIX_TOUCH to a source file and assert the acceptance
	// fingerprint only moves when --fix was actually passed.
	if leg == "verify" {
		if touch := os.Getenv("STUB_VERIFY_FIX_TOUCH"); touch != "" && argvHasFix(os.Args[1:]) {
			f, err := os.OpenFile(touch, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				fmt.Fprintf(os.Stderr, "stub: opening %s: %v\n", touch, err)
				os.Exit(99)
			}
			if _, err := fmt.Fprintln(f, "\n// verify-fix"); err != nil {
				fmt.Fprintf(os.Stderr, "stub: writing %s: %v\n", touch, err)
				f.Close()
				os.Exit(99)
			}
			if err := f.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "stub: closing %s: %v\n", touch, err)
				os.Exit(99)
			}
		}
	}

	// Look up the configured exit code: STUB_EXIT_DOGFOOD=2, etc.
	envName := "STUB_EXIT_" + strings.ToUpper(strings.ReplaceAll(leg, "-", "_"))
	if v := os.Getenv(envName); v != "" {
		code, err := strconv.Atoi(v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "stub: invalid %s=%q: %v\n", envName, v, err)
			os.Exit(99)
		}
		if code != 0 {
			fmt.Fprintf(os.Stderr, "stub %s: forced exit %d\n", leg, code)
		}
		os.Exit(code)
	}
	os.Exit(0)
}

func argvHasFix(argv []string) bool {
	for _, arg := range argv {
		if arg == "--fix" {
			return true
		}
	}
	return false
}
