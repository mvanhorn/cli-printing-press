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
	require.Contains(t, doctorSrc, "doctorBaseURLIsPlaceholder(cfg.BaseURL)")
	require.NotContains(t, doctorSrc, "ResolvedBaseURL(")
	require.Contains(t, doctorSrc, "GetWithHeadersNoCache(")
	require.Contains(t, doctorSrc, "base_url is a placeholder")
	require.Contains(t, doctorSrc, `not configured (base_url is a placeholder)`)
	require.Contains(t, doctorSrc, `skipped (base_url is a placeholder)`)
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

func TestDoctorPlaceholderFailOn(t *testing.T) {
	if err := doctorExitForFailOn("error", map[string]any{"api": "not configured (base_url is a placeholder)"}); err == nil {
		t.Fatal("placeholder base URL did not trip --fail-on=error")
	}
	if err := doctorExitForFailOn("warn", map[string]any{"api": "not configured (base_url is a placeholder)"}); err == nil {
		t.Fatal("placeholder base URL did not trip --fail-on=warn")
	}
	if err := doctorExitForFailOn("error", map[string]any{"credentials": "skipped (base_url is a placeholder)"}); err != nil {
		t.Fatalf("placeholder credential skip tripped --fail-on=error: %v", err)
	}
	if err := doctorExitForFailOn("warn", map[string]any{"credentials": "skipped (base_url is a placeholder)"}); err == nil {
		t.Fatal("placeholder credential skip did not trip --fail-on=warn")
	}
	if err := doctorExitForFailOn("error", map[string]any{"auth": "optional — not configured"}); err != nil {
		t.Fatalf("optional auth tripped --fail-on=error: %v", err)
	}
	if err := doctorExitForFailOn("warn", map[string]any{"auth": "optional — not configured"}); err != nil {
		t.Fatalf("optional auth tripped --fail-on=warn: %v", err)
	}
	if err := doctorExitForFailOn("error", map[string]any{"api": "not configured (set base_url in config file)"}); err != nil {
		t.Fatalf("unset base URL tripped --fail-on=error: %v", err)
	}
	if err := doctorExitForFailOn("error", map[string]any{"auth": "inferred (not configured)"}); err != nil {
		t.Fatalf("inferred auth tripped --fail-on=error: %v", err)
	}
	if err := doctorExitForFailOn("error", map[string]any{"credentials": "skipped (API unreachable)"}); err == nil {
		t.Fatal("unreachable credential skip did not trip --fail-on=error")
	}
	if err := doctorExitForFailOn("error", map[string]any{"credentials": "present, not verified"}); err != nil {
		t.Fatalf("not verified tripped --fail-on=error: %v", err)
	}
}
`
	testPath := filepath.Join(outputDir, "internal", "cli", "doctor_placeholder_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(inlineTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestDoctorBaseURLPlaceholderShapes|TestDoctorPlaceholderFailOn", "-count=1")
}

func TestGeneratedDoctorProbesResolvedTemplateBaseURL(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("doctor-template-base")
	apiSpec.BaseURL = "https://{shop}"
	apiSpec.EndpointTemplateVars = []string{"shop"}
	apiSpec.Auth.VerifyPath = "/shop.json"
	outputDir := filepath.Join(t.TempDir(), "doctor-template-base-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())

	doctorSrc := readGeneratedFile(t, outputDir, "internal", "cli", "doctor.go")
	require.Contains(t, doctorSrc, "client.ResolvedBaseURL(cfg.BaseURL, cfg.TemplateVars)")
	require.Contains(t, doctorSrc, "doctorBaseURLIsPlaceholder(doctorBase)")
	require.NotContains(t, doctorSrc, "doctorBaseURLIsPlaceholder(cfg.BaseURL)")
	require.Contains(t, doctorSrc, "GetWithHeadersNoCache(")

	urlSrc := readGeneratedFile(t, outputDir, "internal", "client", "url.go")
	require.Contains(t, urlSrc, "func ResolvedBaseURL(")

	const inlineTest = `package cli

import (
	"testing"

	"doctor-template-base-pp-cli/internal/client"
)

func TestDoctorResolvedTemplateRoot(t *testing.T) {
	if doctorBaseURLIsPlaceholder(client.ResolvedBaseURL("https://{shop}", map[string]string{"shop": "api.vendor.test"})) {
		t.Fatal("resolved template root was treated as a placeholder")
	}
	if !doctorBaseURLIsPlaceholder(client.ResolvedBaseURL("https://{shop}", nil)) {
		t.Fatal("unresolved template root was not treated as a placeholder")
	}
	if !doctorBaseURLIsPlaceholder(client.ResolvedBaseURL("https://{shop}", map[string]string{"shop": "api.example.com"})) {
		t.Fatal("resolved example host was probed")
	}
	if doctorBaseURLIsPlaceholder(client.ResolvedBaseURL("https://api.hubapi.com", map[string]string{"shop": "ignored.example"})) {
		t.Fatal("real root was treated as a placeholder")
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "doctor_template_base_test.go"), []byte(inlineTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "TestDoctorResolvedTemplateRoot", "-count=1")
}
