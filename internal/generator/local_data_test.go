package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGeneratedLocalDataPurge compiles a store-backed print and runs
// emitted-code tests for the local-data purge command.
func TestGeneratedLocalDataPurge(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("local-data")
	outputDir := filepath.Join(t.TempDir(), "local-data-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, MCP: true}
	require.NoError(t, gen.Generate())
	localData, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "local_data.go"))
	require.NoError(t, err)
	require.Contains(t, string(localData), `"mcp:hidden": "true"`, "purge deletes local files and must stay off the MCP surface")
	platformSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "platform_client.go"))
	require.NoError(t, err)
	require.Contains(t, string(platformSrc), `"api", "local-data":`, "erasing local files must not need a live tenant gate")

	const runtimeTest = `package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"local-data-pp-cli/internal/cliutil"
)

func relocateLocalData(t *testing.T) string {
	t.Helper()
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	home := t.TempDir()
	restore, err := cliutil.SetHomeOverride(home)
	if err != nil {
		t.Fatalf("home override: %v", err)
	}
	t.Cleanup(restore)
	return home
}

func seedLocalFiles(t *testing.T, home string, rel ...string) []string {
	t.Helper()
	var paths []string
	for _, name := range rel {
		path := filepath.Join(home, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	return paths
}

func runLocalDataPurge(t *testing.T, flags *rootFlags, args ...string) string {
	t.Helper()
	cmd := newLocalDataCmd(flags)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(append([]string{"purge"}, args...))
	if err := cmd.Execute(); err != nil {
		t.Fatalf("purge %v: %v", args, err)
	}
	return out.String()
}

func TestLocalDataPurgeDryRunThenDelete(t *testing.T) {
	home := relocateLocalData(t)
	purged := seedLocalFiles(t, home, "data/data.db", "data/data.db-wal", "data/data-0a1b2c.db", "data/data-0a1b2c.db-shm",
		"cache/http/resources/items/k.json", "state/learn/journal-20261022.jsonl", "state/learn/journal.offset")
	kept := seedLocalFiles(t, home, "data/credentials.toml", "config/config.toml", "state/teach.log")

	out := runLocalDataPurge(t, &rootFlags{})
	if !strings.Contains(out, "Would delete 7 item(s)") {
		t.Fatalf("dry run output = %q", out)
	}
	for _, path := range purged {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("dry run removed %s", path)
		}
	}

	runLocalDataPurge(t, &rootFlags{yes: true})
	for _, path := range purged {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s survived purge --yes", path)
		}
	}
	for _, path := range kept {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("purge removed %s: %v", path, err)
		}
	}
}

func TestLocalDataPurgeThroughExecuteLeavesNothingBehind(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	purged := seedLocalFiles(t, home, "state/learn/journal-20261022.jsonl", "cache/http/resources/items/k.json")
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	for _, args := range [][]string{{"local-data", "purge", "--home", home}, {"local-data", "purge", "--yes", "--home", home}} {
		os.Args = append([]string{"local-data-pp-cli"}, args...)
		if err := Execute(); err != nil {
			t.Fatalf("Execute %v: %v", args, err)
		}
		if matches, _ := filepath.Glob(filepath.Join(home, "data", "data*.db")); len(matches) != 0 {
			t.Fatalf("%v opened the store as a side effect: %v", args, matches)
		}
	}
	for _, path := range purged {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s survived purge, or the learn journal recreated it", path)
		}
	}
	if matches, _ := filepath.Glob(filepath.Join(home, "state", "learn", "journal-*.jsonl")); len(matches) != 0 {
		t.Fatalf("journal recreated after purge: %v", matches)
	}
}

func TestLocalDataPurgeReportsUnreadableDir(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs Unix permission bits and a non-root user")
	}
	home := relocateLocalData(t)
	seedLocalFiles(t, home, "data/data.db")
	dataDir := filepath.Join(home, "data")
	if err := os.Chmod(dataDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dataDir, 0o700) })
	cmd := newLocalDataCmd(&rootFlags{yes: true})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"purge"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "inspect") {
		t.Fatalf("unreadable data dir was treated as empty: err=%v", err)
	}
}

func TestLocalDataPurgeOnlyCache(t *testing.T) {
	seeded := seedLocalFiles(t, relocateLocalData(t), "data/data.db", "cache/http/resources/items/k.json")
	store, cached := seeded[0], seeded[1]

	var payload struct {
		DryRun  bool ` + "`json:\"dry_run\"`" + `
		Targets []localDataTarget ` + "`json:\"targets\"`" + `
	}
	out := runLocalDataPurge(t, &rootFlags{yes: true, asJSON: true}, "--only", "cache")
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("decode %q: %v", out, err)
	}
	if payload.DryRun || len(payload.Targets) != 1 || payload.Targets[0].Kind != "cache" {
		t.Fatalf("payload = %+v", payload)
	}
	if _, err := os.Stat(store); err != nil {
		t.Fatalf("--only cache removed the store: %v", err)
	}
	if _, err := os.Stat(cached); !os.IsNotExist(err) {
		t.Fatalf("--only cache kept %s", cached)
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "local_data_runtime_test.go"), []byte(runtimeTest), 0o600))
	requireGeneratedCompiles(t, outputDir)
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestLocalDataPurge", "-count=1")

	minimal := filepath.Join(t.TempDir(), "local-data-min-pp-cli")
	require.NoError(t, New(minimalSpec("local-data-min"), minimal).Generate())
	requireGeneratedCompiles(t, minimal)
}
