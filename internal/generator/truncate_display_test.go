package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratedTruncateCutsOnRuneBoundaries(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("truncate-runes")
	outputDir := filepath.Join(t.TempDir(), "truncate-runes-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	helpersSrc := readGeneratedFile(t, outputDir, "internal", "cli", "helpers.go")
	body := emittedTruncateBody(t, helpersSrc)
	require.Contains(t, body, "[]rune(s)", "display truncate must count runes, not bytes")
	require.NotContains(t, body, "len(s)", "byte-length width checks truncate CJK that still fits the column")
	require.False(t, containsBareIdentSlice(body, "s[:max]"),
		"byte slices of s split multi-byte runes; runes[:max] is the safe form")
	require.False(t, containsBareIdentSlice(body, "s[:max-3]"),
		"byte slices of s split multi-byte runes; runes[:max-3] is the safe form")

	const runtimeTest = `package cli

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateCJKValidUTF8AndFitsColumn(t *testing.T) {
	twenty := strings.Repeat("测", 20)
	if got := truncate(twenty, 40); got != twenty {
		t.Fatalf("20-rune CJK in a 40-wide column was truncated: %q", got)
	}

	long := "测试文字更长的输入"
	got := truncate(long, 5)
	if !utf8.ValidString(got) {
		t.Fatalf("CJK truncate produced invalid UTF-8: %q", got)
	}
	if n := utf8.RuneCountInString(got); n > 5 {
		t.Fatalf("CJK truncate returned %d runes, want at most 5: %q", n, got)
	}
	if strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("CJK truncate inserted replacement runes: %q", got)
	}
}

func TestTruncateASCIIUnchanged(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{in: "hello", max: 40, want: "hello"},
		{in: "hello", max: 5, want: "hello"},
		{in: "hello world extra", max: 10, want: "hello w..."},
		{in: "abcd", max: 3, want: "abc"},
		{in: "abcd", max: 2, want: "ab"},
		{in: "abcd", max: 1, want: "a"},
		{in: "abcd", max: 0, want: ""},
	}
	for _, tc := range cases {
		if got := truncate(tc.in, tc.max); got != tc.want {
			t.Fatalf("truncate(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "truncate_runtime_test.go"), []byte(runtimeTest), 0o600))
	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "^TestTruncate(CJKValidUTF8AndFitsColumn|ASCIIUnchanged)$", "-count=1")
}

// containsBareIdentSlice reports whether expr (e.g. "s[:max]") appears as its
// own identifier, not as a suffix of a longer name such as runes[:max].
func containsBareIdentSlice(body, expr string) bool {
	for i := 0; ; {
		idx := strings.Index(body[i:], expr)
		if idx < 0 {
			return false
		}
		abs := i + idx
		if abs == 0 || !isIdentByte(body[abs-1]) {
			return true
		}
		i = abs + len(expr)
	}
}

func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

func TestContainsBareIdentSliceDistinguishesRunesFromString(t *testing.T) {
	t.Parallel()

	cases := []struct {
		body string
		expr string
		want bool
	}{
		{body: `return s[:max]`, expr: "s[:max]", want: true},
		{body: `return s[:max-3] + "..."`, expr: "s[:max-3]", want: true},
		{body: `return string(runes[:max])`, expr: "s[:max]", want: false},
		{body: `return string(runes[:max-3]) + "..."`, expr: "s[:max-3]", want: false},
	}
	for _, tc := range cases {
		if got := containsBareIdentSlice(tc.body, tc.expr); got != tc.want {
			t.Fatalf("containsBareIdentSlice(%q, %q) = %v, want %v", tc.body, tc.expr, got, tc.want)
		}
	}
}

func emittedTruncateBody(t *testing.T, helpersSrc string) string {
	t.Helper()
	const sig = "func truncate(s string, max int) string {"
	start := strings.Index(helpersSrc, sig)
	require.NotEqual(t, -1, start, "generated helpers.go must emit truncate")
	body := helpersSrc[start:]
	if next := strings.Index(body[1:], "\nfunc "); next != -1 {
		body = body[:next+1]
	}
	return body
}
