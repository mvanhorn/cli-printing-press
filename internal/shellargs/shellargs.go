package shellargs

import (
	"fmt"
	"strings"
	"unicode"
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

// ChainSegment is one runnable or pipe-skipped piece of a shell-style command.
type ChainSegment struct {
	// Text is the segment as it appeared in the source, with surrounding
	// whitespace trimmed.
	Text string
	// AfterPipe is true when this segment sat to the right of a top-level
	// pipe operator. Callers that execute commands should usually skip these
	// because their input would normally arrive over a shell pipe.
	AfterPipe bool
}

// SplitChain returns segments separated by top-level &&, ||, ;, or | operators.
// Quoted text is preserved. Segments after a top-level | carry AfterPipe=true.
func SplitChain(command string) ([]ChainSegment, error) {
	var (
		segments  []ChainSegment
		quote     rune
		escaped   bool
		afterPipe bool
		start     int
	)
	flush := func(end int) {
		if seg := strings.TrimSpace(command[start:end]); seg != "" {
			segments = append(segments, ChainSegment{Text: seg, AfterPipe: afterPipe})
		}
	}
	for i := 0; i < len(command); i++ {
		c := command[i]
		if escaped {
			escaped = false
			continue
		}
		if quote == '\'' {
			if c == '\'' {
				quote = 0
			}
			continue
		}
		if quote == '"' {
			switch c {
			case '\\':
				escaped = true
			case '"':
				quote = 0
			}
			continue
		}
		switch c {
		case '\\':
			escaped = true
		case '\'', '"':
			quote = rune(c)
		case '&':
			if i+1 < len(command) && command[i+1] == '&' {
				flush(i)
				i++
				start = i + 1
				afterPipe = false
			}
		case '|':
			if i+1 < len(command) && command[i+1] == '|' {
				flush(i)
				i++
				start = i + 1
				afterPipe = false
				continue
			}
			flush(i)
			start = i + 1
			afterPipe = true
		case ';':
			flush(i)
			start = i + 1
			afterPipe = false
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unclosed %s quote in %q", quoteName(quote), command)
	}
	flush(len(command))
	return segments, nil
}

// ArgsAfterBinary returns every token after the leading binary name.
func ArgsAfterBinary(example string) ([]string, error) {
	tokens, err := Split(example)
	if err != nil {
		return nil, err
	}
	i := 0
	for i < len(tokens) && isShellEnvAssignment(tokens[i]) {
		i++
	}
	if len(tokens)-i < 2 {
		return nil, fmt.Errorf("example has no subcommand: %q", example)
	}
	return tokens[i+1:], nil
}

func isShellEnvAssignment(tok string) bool {
	eq := strings.IndexByte(tok, '=')
	if eq <= 0 {
		return false
	}
	name := tok[:eq]
	if name[0] >= '0' && name[0] <= '9' {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return false
	}
	return true
}

// Join renders tokens as one POSIX-shell-safe command line that Split can
// recover without losing token boundaries.
func Join(tokens []string) string {
	quoted := make([]string, len(tokens))
	for i, token := range tokens {
		quoted[i] = quote(token)
	}
	return strings.Join(quoted, " ")
}

func quote(token string) string {
	if token != "" && strings.IndexFunc(token, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("_@%+=:,./-", r)
	}) == -1 {
		return token
	}
	return "'" + strings.ReplaceAll(token, "'", `'\''`) + "'"
}
