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

import "testing"

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
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "client", "apierror_empty_body_test.go"),
		[]byte(clientTest), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "TestAPIErrorSeparatorGuardOnEmptyBody", "-count=1")
}
