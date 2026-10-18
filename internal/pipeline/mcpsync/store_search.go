package mcpsync

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

const generatedStoreSearchHeader = "func (s *Store) Search(query string, limit int, resourceTypes ...string) ([]json.RawMessage, error)"

// ensureStoreSearchContext adds Store.SearchContext to an older store.go.
// mcp-sync regenerates tools.go, which calls SearchContext, and leaves the
// store package untouched. The CLI Search method keeps its signature.
func ensureStoreSearchContext(cliDir string) error {
	storePath := filepath.Join(cliDir, "internal", "store", "store.go")
	src, err := os.ReadFile(storePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading store.go: %w", err)
	}
	updated, err := upgradeStoreSearchContext(src)
	if err != nil {
		if !mcpToolsCallSearchContext(cliDir) {
			return nil
		}
		return err
	}
	if bytes.Equal(updated, src) {
		if mcpToolsCallSearchContext(cliDir) && !bytes.Contains(updated, []byte("func (s *Store) SearchContext(")) {
			return fmt.Errorf("mcp-sync: tools.go calls Store.SearchContext but internal/store/store.go has no Search method to upgrade")
		}
		return nil
	}
	if err := writeFileAtomic(storePath, updated); err != nil {
		return err
	}
	return nil
}

func mcpToolsCallSearchContext(cliDir string) bool {
	tools, err := os.ReadFile(filepath.Join(cliDir, "internal", "mcp", "tools.go"))
	if err != nil {
		return false
	}
	return bytes.Contains(tools, []byte("SearchContext("))
}

func upgradeStoreSearchContext(src []byte) ([]byte, error) {
	if bytes.Contains(src, []byte("func (s *Store) SearchContext(")) {
		return src, nil
	}
	start := bytes.Index(src, []byte("func (s *Store) Search("))
	if start < 0 {
		return src, nil
	}
	relBrace := bytes.IndexByte(src[start:], '{')
	if relBrace < 0 {
		return nil, fmt.Errorf("mcp-sync: store.go Search is missing a body")
	}
	header := strings.TrimSpace(string(src[start : start+relBrace]))
	if header != generatedStoreSearchHeader {
		return nil, fmt.Errorf("mcp-sync: store.go Search signature is %q, not the generated shape", header)
	}
	end, err := goFuncEnd(src, start+relBrace)
	if err != nil {
		return nil, err
	}
	fn := string(src[start:end])
	fn = strings.Replace(fn,
		"func (s *Store) Search(",
		"func (s *Store) SearchContext(ctx context.Context, ",
		1)
	fn = strings.Replace(fn,
		") ([]json.RawMessage, error) {",
		") ([]json.RawMessage, error) {\n\tif ctx == nil {\n\t\tctx = context.Background()\n\t}",
		1)
	fn = strings.ReplaceAll(fn, "s.db.Query(", "s.db.QueryContext(ctx, ")
	wrapper := "func (s *Store) Search(query string, limit int, resourceTypes ...string) ([]json.RawMessage, error) {\n\treturn s.SearchContext(context.Background(), query, limit, resourceTypes...)\n}\n\n"
	out := make([]byte, 0, len(src)+len(wrapper)+len(fn))
	out = append(out, src[:start]...)
	out = append(out, wrapper...)
	out = append(out, fn...)
	out = append(out, src[end:]...)
	out = ensureContextImport(out)
	if _, err := parser.ParseFile(token.NewFileSet(), "store.go", out, parser.SkipObjectResolution); err != nil {
		return nil, fmt.Errorf("mcp-sync: upgraded store.go does not parse: %w", err)
	}
	return out, nil
}

func ensureContextImport(src []byte) []byte {
	if bytes.Contains(src, []byte("\n\t\"context\"\n")) || bytes.Contains(src, []byte("\n\"context\"\n")) {
		return src
	}
	importAt := bytes.Index(src, []byte("import ("))
	if importAt < 0 {
		return src
	}
	nl := bytes.IndexByte(src[importAt:], '\n')
	if nl < 0 {
		return src
	}
	at := importAt + nl + 1
	out := make([]byte, 0, len(src)+len("\t\"context\"\n"))
	out = append(out, src[:at]...)
	out = append(out, "\t\"context\"\n"...)
	out = append(out, src[at:]...)
	return out
}

// goFuncEnd returns the offset just after the function's closing brace.
func goFuncEnd(src []byte, openBrace int) (int, error) {
	depth := 0
	i := openBrace
	for i < len(src) {
		switch src[i] {
		case '"', '\'', '`':
			next, err := skipGoString(src, i)
			if err != nil {
				return 0, err
			}
			i = next
			continue
		case '/':
			if i+1 < len(src) && (src[i+1] == '/' || src[i+1] == '*') {
				i = skipGoComment(src, i)
				continue
			}
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1, nil
			}
		}
		i++
	}
	return 0, fmt.Errorf("mcp-sync: store.go Search body is not closed")
}

func skipGoString(src []byte, i int) (int, error) {
	quote := src[i]
	i++
	if quote == '`' {
		for i < len(src) && src[i] != '`' {
			i++
		}
		if i >= len(src) {
			return 0, fmt.Errorf("mcp-sync: unclosed raw string in store.go Search")
		}
		return i + 1, nil
	}
	for i < len(src) {
		if src[i] == '\\' {
			i += 2
			continue
		}
		if src[i] == quote {
			return i + 1, nil
		}
		if src[i] == '\n' {
			return 0, fmt.Errorf("mcp-sync: unclosed string in store.go Search")
		}
		i++
	}
	return 0, fmt.Errorf("mcp-sync: unclosed string in store.go Search")
}

func skipGoComment(src []byte, i int) int {
	if src[i+1] == '/' {
		for i < len(src) && src[i] != '\n' {
			i++
		}
		return i
	}
	i += 2
	for i+1 < len(src) && (src[i] != '*' || src[i+1] != '/') {
		i++
	}
	if i+1 < len(src) {
		return i + 2
	}
	return len(src)
}
