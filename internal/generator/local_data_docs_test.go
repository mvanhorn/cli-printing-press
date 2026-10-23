package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGeneratedLocalDataDisclosure checks the README/SKILL local-data and
// remote-MCP sections and runs the doctor local-files report against a
// relocated home, then cross-compiles the Windows DACL branch.
func TestGeneratedLocalDataDisclosure(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("local-docs")
	apiSpec.MCP.Transport = []string{"stdio", "http"}
	outputDir := filepath.Join(t.TempDir(), "local-docs-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())

	readme, err := os.ReadFile(filepath.Join(outputDir, "README.md"))
	require.NoError(t, err)
	for _, want := range []string{"### Local data", "None of it is encrypted", "local-data purge", "LOCAL_DOCS_NO_LOCAL_DATA=1",
		"### Remote MCP over HTTP", "LOCAL_DOCS_MCP_HTTP_TOKEN", "--tls-cert", "LOCAL_DOCS_MCP_READ_ONLY=1"} {
		require.Contains(t, string(readme), want)
	}
	skill, err := os.ReadFile(filepath.Join(outputDir, "SKILL.md"))
	require.NoError(t, err)
	for _, want := range []string{"Local data is unencrypted", "LOCAL_DOCS_MCP_HTTP_TOKEN", "LOCAL_DOCS_MCP_READ_ONLY=1"} {
		require.Contains(t, string(skill), want)
	}
	doctor, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	require.Contains(t, string(doctor), `report["oldest_synced_at"]`)
	require.Contains(t, string(doctor), `cliutil.VerifyCredsPerms(dir)`)

	const runtimeTest = `package cli

import (
	"os"
	"path/filepath"
	"testing"

	"local-docs-pp-cli/internal/cliutil"
)

func TestDoctorLocalFilesReport(t *testing.T) {
	home := filepath.Join(t.TempDir(), "[team]")
	restore, err := cliutil.SetHomeOverride(home)
	if err != nil {
		t.Fatal(err)
	}
	defer restore()
	for _, rel := range []string{"data/data.db", "data/data-0a1b.db-wal", "data/credentials.toml", "cache/http/resources/items/k.json"} {
		path := filepath.Join(home, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("abcd"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	report, aclWarning := collectLocalFilesReport()
	if len(report.StoreFiles) != 2 || report.StoreBytes != 8 {
		t.Fatalf("store files = %v (%d bytes), want data.db and data-0a1b.db-wal", report.StoreFiles, report.StoreBytes)
	}
	if report.ResponseCacheFiles != 1 || report.ResponseCacheBytes != 4 {
		t.Fatalf("response cache = %d files / %d bytes, want 1 / 4", report.ResponseCacheFiles, report.ResponseCacheBytes)
	}
	if aclWarning != "" {
		t.Fatalf("unexpected ACL warning on a private temp dir: %s", aclWarning)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "local_files_runtime_test.go"), []byte(runtimeTest), 0o600))
	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestDoctorLocalFilesReport$", "-count=1")
	output, err := runGoCommandOutputWithEnv(t, outputDir, []string{"GOOS=windows", "GOARCH=amd64"}, "build", "./...")
	require.NoError(t, err, output)

	noAuth := minimalSpec("local-docs-noauth")
	noAuth.Auth = minimalSpec("x").Auth
	noAuth.Auth.Type = "none"
	noAuthDir := filepath.Join(t.TempDir(), "local-docs-noauth-pp-cli")
	require.NoError(t, New(noAuth, noAuthDir).Generate())
	requireGeneratedCompiles(t, noAuthDir)
}
