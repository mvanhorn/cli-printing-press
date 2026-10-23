package generator

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/require"
)

// An existing world-listable data directory left by an older print must be
// narrowed to the owner on open, while a directory the operator chose keeps
// its mode.
func TestGeneratedStoreTightensOnlyItsOwnExistingDataDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	t.Parallel()

	apiSpec := minimalSpec("store-dir-perms")
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())
	requireGeneratedCompiles(t, outputDir)

	runtimeTest := strings.ReplaceAll(`package store

import (
	"os"
	"path/filepath"
	"testing"

	"MODULE/internal/cliutil"
)

func openAndClose(t *testing.T, dbPath string) {
	t.Helper()
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open(%s): %v", dbPath, err)
	}
	_ = s.Close()
}

func worldListableDir(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func dirPerm(t *testing.T, dir string) os.FileMode {
	t.Helper()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestOpenTightensOnlyOwnDataDir(t *testing.T) {
	t.Setenv("PREFIX_HOME", "")
	t.Setenv("PREFIX_DATA_DIR", "")
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	own, err := cliutil.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	openAndClose(t, filepath.Join(worldListableDir(t, own), "data.db"))
	if got := dirPerm(t, own); got != 0o700 {
		t.Fatalf("own data dir mode = %o, want 700", got)
	}

	chosen := worldListableDir(t, filepath.Join(t.TempDir(), "chosen"))
	openAndClose(t, filepath.Join(chosen, "data.db"))
	if got := dirPerm(t, chosen); got != 0o755 {
		t.Fatalf("--db parent mode = %o, want 755 left alone", got)
	}

	explicit := worldListableDir(t, filepath.Join(t.TempDir(), "explicit"))
	t.Setenv("PREFIX_DATA_DIR", explicit)
	openAndClose(t, filepath.Join(explicit, "data.db"))
	if got := dirPerm(t, explicit); got != 0o755 {
		t.Fatalf("PREFIX_DATA_DIR mode = %o, want 755 left alone", got)
	}
}
`, "PREFIX", naming.EnvPrefix(apiSpec.Name))
	runtimeTest = strings.ReplaceAll(runtimeTest, "MODULE", naming.CLI(apiSpec.Name))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "store", "data_dir_perms_test.go"), []byte(runtimeTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/store", "-run", "^TestOpenTightensOnlyOwnDataDir$", "-count=1")
}
