package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/require"
)

// TestGeneratedAPIErrorOmitsSeparatorOnEmptyBody pins #2945: the emitted
// APIError.Error() must drop the trailing ": " when the response body is empty.
// It asserts inside the generated module, so it exercises the emitted Error()
// at runtime rather than the template text.
func TestGeneratedAPIErrorOmitsSeparatorOnEmptyBody(t *testing.T) {
	apiSpec := minimalSpec("apierror-empty-body")

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())

	const clientTest = `package client

import (
	"strings"
	"testing"
)

func TestAPIErrorSeparatorGuardOnEmptyBody(t *testing.T) {
	cases := []struct {
		name       string
		statusCode int
		body       string
		want       string
	}{
		{"empty body", 401, "", "GET /items returned HTTP 401"},
		{"whitespace-only body", 401, "  \n\t ", "GET /items returned HTTP 401"},
		{"non-empty body preserved", 404, "{\"error\":\"not_found\"}", "GET /items returned HTTP 404: {\"error\":\"not_found\"}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &APIError{Method: "GET", Path: "/items", StatusCode: tc.statusCode, Body: tc.body}
			if got := e.Error(); got != tc.want {
				t.Fatalf("APIError.Error() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAPIErrorHTMLBodyCollapsed(t *testing.T) {
	body := []byte(` + "`" + `<!doctype html>
<html>
<head>
<title>Tenant Missing</title>
<style>body{color:red}</style>
</head>
<body>
<h1>Tenant Missing</h1>
<script>alert("x")</script>
<p>Use the right host.</p>
</body>
</html>` + "`" + `)

	collapsed := truncateBody(body)
	if strings.Contains(collapsed, "<html") || strings.Contains(collapsed, "<script") || strings.Contains(collapsed, "body{color:red}") {
		t.Fatalf("HTML body was emitted verbatim: %q", collapsed)
	}
	if !strings.Contains(collapsed, "HTML error page") {
		t.Fatalf("collapsed body = %q, want HTML error page summary", collapsed)
	}
	if !strings.Contains(collapsed, "Tenant Missing") {
		t.Fatalf("collapsed body = %q, want title excerpt", collapsed)
	}

	errText := (&APIError{Method: "GET", Path: "/items", StatusCode: 404, Body: collapsed}).Error()
	if strings.Contains(errText, "<html") || strings.Contains(errText, "<script") || strings.Contains(errText, "alert(") {
		t.Fatalf("APIError.Error emitted raw HTML: %q", errText)
	}
}

func TestAPIErrorHTMLTitleSanitizesDecodedControls(t *testing.T) {
	body := []byte(` + "`" + `<!doctype html><html><head><title>Tenant &#27;[2J Missing</title></head><body>Denied</body></html>` + "`" + `)

	collapsed := truncateBody(body)
	if strings.ContainsRune(collapsed, '\x1b') {
		t.Fatalf("collapsed body contains decoded ESC control: %q", collapsed)
	}
	errText := (&APIError{Method: "GET", Path: "/items", StatusCode: 403, Body: collapsed}).Error()
	if strings.ContainsRune(errText, '\x1b') {
		t.Fatalf("APIError.Error contains decoded ESC control: %q", errText)
	}
	if !strings.Contains(collapsed, "Tenant [2J Missing") {
		t.Fatalf("collapsed body = %q, want sanitized title text", collapsed)
	}

	unicodeBody := []byte(` + "`" + `<!doctype html><html><head><title>Tenant &#x202E;Split&#x2028;Missing</title></head><body>Denied</body></html>` + "`" + `)
	unicodeCollapsed := truncateBody(unicodeBody)
	if strings.ContainsRune(unicodeCollapsed, '\u202e') || strings.ContainsRune(unicodeCollapsed, '\u2028') {
		t.Fatalf("collapsed body contains decoded unicode formatting controls: %q", unicodeCollapsed)
	}
	if !strings.Contains(unicodeCollapsed, "Tenant Split Missing") {
		t.Fatalf("collapsed body = %q, want unicode controls sanitized", unicodeCollapsed)
	}
}

func TestAPIErrorHTMLTitleUsesOriginalOffsets(t *testing.T) {
	body := []byte(` + "`" + `<!doctype html><html><head><title>Tenant İ Missing</title></head><body>Denied</body></html>` + "`" + `)

	collapsed := truncateBody(body)
	if !strings.Contains(collapsed, "Tenant İ Missing") {
		t.Fatalf("collapsed body = %q, want non-ASCII title preserved", collapsed)
	}
}

func TestAPIErrorHTMLTitleIgnoresScriptAndStyleBlocks(t *testing.T) {
	closedBlock := []byte(` + "`" + `<!doctype html><html><head><script><title>Script Secret</title></script><title>Real Title</title></head></html>` + "`" + `)
	closedCollapsed := truncateBody(closedBlock)
	if strings.Contains(closedCollapsed, "Script Secret") {
		t.Fatalf("closed script block leaked into title summary: %q", closedCollapsed)
	}
	if !strings.Contains(closedCollapsed, "Real Title") {
		t.Fatalf("closed script block summary = %q, want real title", closedCollapsed)
	}

	unclosedBlock := []byte(` + "`" + `<!doctype html><html><head><style><title>Style Secret</title></head><body>Denied</body></html>` + "`" + `)
	unclosedCollapsed := truncateBody(unclosedBlock)
	if strings.Contains(unclosedCollapsed, "Style Secret") || strings.Contains(unclosedCollapsed, "Denied") {
		t.Fatalf("unclosed style block leaked body content into summary: %q", unclosedCollapsed)
	}
	if !strings.Contains(unclosedCollapsed, "HTML error page") {
		t.Fatalf("unclosed style block summary = %q, want HTML summary", unclosedCollapsed)
	}
}

func TestAPIErrorHTMLTitleSkipsRepeatedBlocks(t *testing.T) {
	var body strings.Builder
	body.WriteString(` + "`" + `<!doctype html><html><head>` + "`" + `)
	for i := 0; i < 100; i++ {
		body.WriteString(` + "`" + `<script><title>Script Secret</title></script>` + "`" + `)
	}
	body.WriteString(` + "`" + `<title>Real Title</title></head></html>` + "`" + `)

	collapsed := truncateBody([]byte(body.String()))
	if strings.Contains(collapsed, "Script Secret") {
		t.Fatalf("repeated script blocks leaked into title summary: %q", collapsed)
	}
	if !strings.Contains(collapsed, "Real Title") {
		t.Fatalf("repeated script block summary = %q, want real title", collapsed)
	}
}

func TestAPIErrorHTMLTitleScanIsCapped(t *testing.T) {
	var body strings.Builder
	body.WriteString(` + "`" + `<!doctype html><html><head>` + "`" + `)
	body.WriteString(strings.Repeat("x", 5000))
	body.WriteString(` + "`" + `<title>Late Title</title></head></html>` + "`" + `)

	collapsed := truncateBody([]byte(body.String()))
	if strings.Contains(collapsed, "Late Title") {
		t.Fatalf("title beyond capped scan window leaked into summary: %q", collapsed)
	}
	if !strings.Contains(collapsed, "HTML error page") {
		t.Fatalf("capped scan summary = %q, want HTML summary", collapsed)
	}
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "client", "apierror_empty_body_test.go"),
		[]byte(clientTest), 0o644))

	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestAPIError", "-count=1")
}
