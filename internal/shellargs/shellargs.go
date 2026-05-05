package shellargs

import (
	"fmt"
	"strings"
)

// Split tokenizes the simple command examples the Printing Press emits in
// README/SKILL narrative. It preserves double-quoted tokens and backslash
// escapes, but intentionally does not perform shell expansion.
func Split(s string) ([]string, error) {
	s = joinLineContinuations(s)

	var tokens []string
	var current strings.Builder
	inQuote := false
	tokenStarted := false
	escaped := false

	flush := func() {
		tokens = append(tokens, current.String())
		current.Reset()
		tokenStarted = false
	}

	for _, r := range s {
		if escaped {
			current.WriteRune(r)
			tokenStarted = true
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			tokenStarted = true
			continue
		}
		if inQuote {
			if r == '"' {
				inQuote = false
				tokenStarted = true
				continue
			}
			current.WriteRune(r)
			tokenStarted = true
			continue
		}
		switch r {
		case '"':
			inQuote = true
			tokenStarted = true
		case ' ', '\t', '\n', '\r':
			if tokenStarted {
				flush()
			}
		default:
			current.WriteRune(r)
			tokenStarted = true
		}
	}
	if escaped {
		current.WriteRune('\\')
	}
	if inQuote {
		return nil, fmt.Errorf("unclosed quote in %q", s)
	}
	if tokenStarted {
		flush()
	}
	return tokens, nil
}

func joinLineContinuations(s string) string {
	for _, newline := range []string{"\\\r\n", "\\\n"} {
		s = strings.ReplaceAll(s, newline, "")
	}
	return s
}

// ArgsAfterBinary returns every token after the leading binary name.
func ArgsAfterBinary(example string) ([]string, error) {
	tokens, err := Split(example)
	if err != nil {
		return nil, err
	}
	if len(tokens) < 2 {
		return nil, fmt.Errorf("example has no subcommand: %q", example)
	}
	return tokens[1:], nil
}
