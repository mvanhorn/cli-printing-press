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
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "client", "apierror_empty_body_test.go"),
		[]byte(clientTest), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestAPIError", "-count=1")
}
