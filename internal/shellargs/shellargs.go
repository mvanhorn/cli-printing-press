package shellargs

import (
	"fmt"
	"strings"
)

// Split tokenizes the simple command examples the Printing Press emits in
// README/SKILL narrative. It preserves double-quoted and single-quoted
// tokens and backslash escapes (POSIX semantics: backslashes are literal
// inside single quotes), but intentionally does not perform shell
// expansion.
func Split(s string) ([]string, error) {
	s = joinLineContinuations(s)

	var tokens []string
	var current strings.Builder
	var quoteChar rune // 0 outside quotes; '"' or '\'' while inside.
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
		// Single quotes: everything is literal until the closing quote.
		// POSIX forbids backslash escapes inside single quotes, so the
		// '\\' branch must be skipped while quoteChar is '\''.
		if quoteChar == '\'' {
			if r == '\'' {
				quoteChar = 0
				tokenStarted = true
				continue
			}
			current.WriteRune(r)
			tokenStarted = true
			continue
		}
		if r == '\\' {
			escaped = true
			tokenStarted = true
			continue
		}
		if quoteChar == '"' {
			if r == '"' {
				quoteChar = 0
				tokenStarted = true
				continue
			}
			current.WriteRune(r)
			tokenStarted = true
			continue
		}
		switch r {
		case '"', '\'':
			quoteChar = r
			tokenStarted = true
		case '#':
			if !tokenStarted {
				// Shell line comment: '#' at the start of a word drops the
				// rest of the input. Cobra Example fields routinely append
				// trailing comments ("sync # full refresh"); without this
				// branch a downstream consumer runs the binary with the
				// comment text as positional args.
				return tokens, nil
			}
			current.WriteRune(r)
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
	if quoteChar != 0 {
		return nil, fmt.Errorf("unclosed %s quote in %q", quoteName(quoteChar), s)
	}
	if tokenStarted {
		flush()
	}
	return tokens, nil
}

func quoteName(r rune) string {
	if r == '\'' {
		return "single"
	}
	return "double"
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
