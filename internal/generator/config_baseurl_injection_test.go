package generator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigBaseURLIsEscapedNoInjection guards against Go code injection through
// a spec-controlled base_url. base_url is attacker-influenced for domain-import
// (recovered from an untrusted HAR / fetched domain), and Config.Load's BaseURL
// is built into the generated binary; an unescaped value could break out of the
// string literal and execute during build/validation. The template must emit it
// via %q so a malicious value stays a single, inert string literal.
func TestConfigBaseURLIsEscapedNoInjection(t *testing.T) {
	t.Parallel()

	const payload = `https://x", AuthHeaderVal: func() string { panic("INJECTED") }(), //`
	apiSpec := minimalSpec("pwn")
	apiSpec.BaseURL = payload

	outputDir := filepath.Join(t.TempDir(), "pwn-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	out := string(src)

	// The value must appear exactly as a Go-quoted literal (%q output), so the
	// embedded quote is escaped and cannot close the literal.
	assert.Contains(t, out, strconv.Quote(payload),
		"base_url must be emitted as an escaped Go string literal")

	// Parse the file and assert the BaseURL field's literal round-trips to the
	// exact payload — definitive proof there was no breakout into extra code.
	file, perr := parser.ParseFile(token.NewFileSet(), "config.go", out, parser.AllErrors)
	require.NoError(t, perr, "generated config.go must parse as valid Go")

	var got string
	var found bool
	ast.Inspect(file, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "BaseURL" {
			if lit, ok := kv.Value.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if v, err := strconv.Unquote(lit.Value); err == nil {
					got, found = v, true
				}
			}
		}
		return true
	})
	require.True(t, found, "BaseURL string literal not found in generated config.go")
	assert.Equal(t, payload, got, "BaseURL literal must round-trip to the exact spec value (no injection)")
}

// TestConfigAuthFieldsEscapedNoInjection guards the other spec-controlled
// strings emitted into config.go (auth format + env var name): a malicious value
// must be emitted as an escaped Go literal, not break out into code.
func TestConfigAuthFieldsEscapedNoInjection(t *testing.T) {
	t.Parallel()

	const fmtPayload = `Bearer {token}", Evil: func() string { panic("INJ") }(), "`
	apiSpec := minimalSpec("pwn2")
	apiSpec.Auth.Format = fmtPayload // attacker-controlled auth format

	outputDir := filepath.Join(t.TempDir(), "pwn2-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	src, err := os.ReadFile(filepath.Join(outputDir, "internal", "config", "config.go"))
	require.NoError(t, err)
	out := string(src)

	// The auth format must appear only as a properly-escaped Go literal (the
	// applyAuthFormat(...) argument), and the file must still be valid Go.
	assert.Contains(t, out, strconv.Quote(fmtPayload), "auth format must be %q-escaped")
	_, perr := parser.ParseFile(token.NewFileSet(), "config.go", out, parser.AllErrors)
	assert.NoError(t, perr, "generated config.go must parse as valid Go")
}
