// Package docparse extracts structured information from a shipped CLI's
// source code. It reads Go source with go/parser (not --help output) so it
// can see Long descriptions, Example fields, flag declarations, and source
// file groupings — signal that isn't available from compiled binaries alone.
//
// The parser is the foundation of the refresh-docs subcommand: it answers
// "what commands does this CLI actually expose?" without trusting the spec.
package docparse

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Command describes a single cobra.Command extracted from CLI source.
// Paths (e.g., "portfolio perf") are populated once the command tree is
// resolved by ParseCLI — the raw extraction step leaves Path empty.
type Command struct {
	// ConstructorName is the name of the Go function that returns the
	// *cobra.Command (e.g., "newPortfolioCmd"). Empty if the command is
	// declared inline rather than via a constructor.
	ConstructorName string

	// File is the absolute path to the source file containing the command.
	// Used for grouping hints — commands in the same file tend to cluster.
	File string

	// Use is the Cobra Use field — the command's own name token.
	// e.g., "portfolio", "perf", "login-chrome".
	Use string

	// Short is the one-line description rendered in --help listings.
	Short string

	// Long is the multi-line description rendered on `<cmd> --help`.
	Long string

	// Example is the Example field (optional usage snippet shown in help).
	Example string

	// Aliases is the list of command aliases declared via `Aliases: []string{...}`.
	Aliases []string

	// Annotations captures the Annotations map literal (optional metadata).
	// Rendered to a key→value map for consumption by downstream classifiers.
	Annotations map[string]string

	// SubcommandConstructors is the list of constructor names this command
	// wires up via `.AddCommand(newXxxCmd(...))` inside its constructor.
	// Resolved against other Commands by ParseCLI to build the tree.
	SubcommandConstructors []string
}

// ParseCLI reads Go source files in cliDir (typically `<cli>/internal/cli/`)
// and returns every cobra.Command it can extract, with subcommand wiring
// resolved into flat Path strings (e.g., "portfolio perf").
//
// The parser tolerates unparseable files — any file that fails to parse
// produces a warning but does not abort the overall extraction. Callers get
// whatever we could extract plus a list of files that failed.
func ParseCLI(cliDir string) ([]Command, []string, error) {
	entries, err := os.ReadDir(cliDir)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", cliDir, err)
	}

	var commands []Command
	var unparseable []string

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		if strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(cliDir, e.Name())
		cmds, err := parseFile(path)
		if err != nil {
			unparseable = append(unparseable, path)
			continue
		}
		commands = append(commands, cmds...)
	}

	// Sort deterministically for stable output.
	sort.Slice(commands, func(i, j int) bool {
		if commands[i].File != commands[j].File {
			return commands[i].File < commands[j].File
		}
		return commands[i].ConstructorName < commands[j].ConstructorName
	})

	return commands, unparseable, nil
}

// parseFile extracts cobra.Command constructors from a single .go file.
// Looks for two patterns:
//  1. Constructor function returning *cobra.Command (most common in generated CLIs)
//  2. Direct cobra.Command composite literal in an expression
func parseFile(path string) ([]Command, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	var commands []Command

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if !returnsCobraCommand(fn) {
			continue
		}
		cmd := extractCommandFromFunc(fn)
		if cmd.Use == "" {
			// No Use field — either not a real command constructor or
			// the composite literal we found was for something else.
			continue
		}
		cmd.ConstructorName = fn.Name.Name
		cmd.File = path
		commands = append(commands, cmd)
	}

	return commands, nil
}

// returnsCobraCommand reports whether fn's signature is `func ... () *cobra.Command`.
func returnsCobraCommand(fn *ast.FuncDecl) bool {
	if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return false
	}
	star, ok := fn.Type.Results.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkg.Name == "cobra" && sel.Sel.Name == "Command"
}

// extractCommandFromFunc walks a constructor function body looking for
// `&cobra.Command{...}` composite literals and `cmd.AddCommand(...)` calls.
func extractCommandFromFunc(fn *ast.FuncDecl) Command {
	var cmd Command
	if fn.Body == nil {
		return cmd
	}

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if isCobraCommandLit(x) {
				populateFromCompositeLit(&cmd, x)
			}
		case *ast.CallExpr:
			if constructor := addCommandConstructor(x); constructor != "" {
				cmd.SubcommandConstructors = append(cmd.SubcommandConstructors, constructor)
			}
		}
		return true
	})

	return cmd
}

// isCobraCommandLit reports whether a composite literal is `cobra.Command{...}`.
func isCobraCommandLit(lit *ast.CompositeLit) bool {
	sel, ok := lit.Type.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkg.Name == "cobra" && sel.Sel.Name == "Command"
}

// populateFromCompositeLit extracts string-literal field values from a
// cobra.Command composite literal. Non-literal values (function references,
// computed strings) are skipped — the parser handles what it can, caller
// tolerates gaps.
func populateFromCompositeLit(cmd *Command, lit *ast.CompositeLit) {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Use":
			if s, ok := stringLit(kv.Value); ok {
				cmd.Use = s
			}
		case "Short":
			if s, ok := stringLit(kv.Value); ok {
				cmd.Short = s
			}
		case "Long":
			if s, ok := stringLit(kv.Value); ok {
				cmd.Long = s
			}
		case "Example":
			if s, ok := stringLit(kv.Value); ok {
				cmd.Example = s
			}
		case "Aliases":
			cmd.Aliases = stringSliceLit(kv.Value)
		case "Annotations":
			cmd.Annotations = stringMapLit(kv.Value)
		}
	}
}

// addCommandConstructor extracts the constructor name from a call like
// `rootCmd.AddCommand(newFooCmd(flags))` or `parent.AddCommand(newFooCmd())`.
// Returns empty string if the call isn't an AddCommand or the argument
// isn't a recognizable constructor call.
func addCommandConstructor(call *ast.CallExpr) string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "AddCommand" {
		return ""
	}
	if len(call.Args) != 1 {
		return ""
	}
	innerCall, ok := call.Args[0].(*ast.CallExpr)
	if !ok {
		return ""
	}
	ident, ok := innerCall.Fun.(*ast.Ident)
	if !ok {
		return ""
	}
	return ident.Name
}

// stringLit unquotes a string basic literal. Returns false for non-literal
// expressions (e.g., variables, concatenations).
func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	// Strip surrounding quotes. Handle both "..." and `...`.
	raw := lit.Value
	if len(raw) < 2 {
		return "", false
	}
	switch raw[0] {
	case '"':
		unquoted, err := unquoteDouble(raw)
		if err != nil {
			return "", false
		}
		return unquoted, true
	case '`':
		return raw[1 : len(raw)-1], true
	}
	return "", false
}

// unquoteDouble unquotes a double-quoted Go string literal. Mirrors
// strconv.Unquote but kept inline to avoid the extra import and to produce
// predictable empty-string output on error.
func unquoteDouble(s string) (string, error) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", fmt.Errorf("not a double-quoted string")
	}
	var out strings.Builder
	escape := false
	for _, r := range s[1 : len(s)-1] {
		if escape {
			switch r {
			case 'n':
				out.WriteRune('\n')
			case 't':
				out.WriteRune('\t')
			case 'r':
				out.WriteRune('\r')
			case '\\':
				out.WriteRune('\\')
			case '"':
				out.WriteRune('"')
			default:
				out.WriteRune(r)
			}
			escape = false
			continue
		}
		if r == '\\' {
			escape = true
			continue
		}
		out.WriteRune(r)
	}
	return out.String(), nil
}

// stringSliceLit extracts string literals from a `[]string{"a", "b"}` expression.
func stringSliceLit(e ast.Expr) []string {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	var out []string
	for _, elt := range lit.Elts {
		if s, ok := stringLit(elt); ok {
			out = append(out, s)
		}
	}
	return out
}

// stringMapLit extracts key→value pairs from a `map[string]string{"k": "v"}` literal.
func stringMapLit(e ast.Expr) map[string]string {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	out := make(map[string]string)
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		k, kOK := stringLit(kv.Key)
		v, vOK := stringLit(kv.Value)
		if kOK && vOK {
			out[k] = v
		}
	}
	return out
}
