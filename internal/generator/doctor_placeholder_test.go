package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGeneratedDoctorSkipsOnlyLiteralPlaceholders(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("doctor-placeholder")
	apiSpec.BaseURL = "https://api.hubapi.com"
	apiSpec.Auth.VerifyPath = "/account-info/v3/details"
	outputDir := filepath.Join(t.TempDir(), "doctor-placeholder-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorSrc := readGeneratedFile(t, outputDir, "internal", "cli", "doctor.go")
	require.Contains(t, doctorSrc, "func doctorBaseURLIsPlaceholder(")
	require.Contains(t, doctorSrc, "GetWithHeadersNoCache(")
	require.Contains(t, doctorSrc, "base_url is a placeholder")
	require.NotContains(t, doctorSrc, "https://api.hubapi.com")
	require.NotContains(t, doctorSrc, "https://api.huntress.io")

	const inlineTest = `package cli

import "testing"

func TestDoctorBaseURLPlaceholderShapes(t *testing.T) {
	cases := []struct {
		base string
		want bool
	}{
		{base: "", want: false},
		{base: "https://api.hubapi.com", want: false},
		{base: "https://api.huntress.io", want: false},
		{base: "https://api.example.com", want: true},
		{base: "https://example.com/v1", want: true},
		{base: "https://{tenant}.example/api", want: true},
		{base: "https://api.vendor.com/{org}", want: true},
	}
	for _, tc := range cases {
		if got := doctorBaseURLIsPlaceholder(tc.base); got != tc.want {
			t.Errorf("doctorBaseURLIsPlaceholder(%q) = %v, want %v", tc.base, got, tc.want)
		}
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "doctor_placeholder_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestDoctorBaseURLPlaceholderShapes", "-count=1")
}
