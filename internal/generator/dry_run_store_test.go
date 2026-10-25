package generator

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// TestDryRunLeavesLearnStoreUntouched proves a learn-enabled CLI (the
// default) treats --dry-run as a preview: fresh homes gain no store and no
// learn journal, and an older store stays byte-identical. A real invocation
// still migrates, seeds, and journals.
func TestDryRunLeavesLearnStoreUntouched(t *testing.T) {
	t.Parallel()

	apiSpec := dryRunStoreSpec()
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Sync: true, MCP: true}
	require.NoError(t, gen.Generate())
	requireGeneratedCompiles(t, outputDir)

	modulePath := generatedModulePath(t, outputDir)
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "cli", "dry_run_store_runtime_test.go"),
		[]byte(strings.ReplaceAll(dryRunStoreRuntimeTest, "__MODULE_PATH__", modulePath)),
		0o644,
	))

	runGoCommand(t, outputDir, "test", "./internal/store", "-count=1", "-run",
		"TestBackfillColumns_|TestColumnNeedsBackfill|TestSnapshotTableColumnsCanceled|TestAddColumnDuplicateAndInvalid")
	runGoCommand(t, outputDir, "test", "./internal/cli", "-count=1", "-run", "TestDryRunStore_|TestAutoRefresh")

	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))
	prefix := naming.EnvPrefix(apiSpec.Name)
	cliName := naming.CLI(apiSpec.Name)

	freshHome := t.TempDir()
	stdout, stderr, code := runDryRunCLI(t, binaryPath, dryRunStoreEnv(t, freshHome, prefix),
		"--home", freshHome, "teach", "--query", "find items", "--resource-type", "items", "--resource", "item-1")
	require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	freshDB := filepath.Join(freshHome, "data", "data.db")
	freshVersion := storeMetaVersion(t, freshDB)
	require.Greater(t, freshVersion, 1)
	require.Equal(t, 1, storeCount(t, freshDB, `SELECT COUNT(*) FROM learning_playbooks WHERE query_family = '__seed_meta__'`))
	require.GreaterOrEqual(t, storeCount(t, freshDB, `SELECT COUNT(*) FROM search_learnings`), 1)
	require.NotEmpty(t, journalSegments(t, freshHome, cliName))

	// Subtests run in this test. A parallel subtest would wait for its
	// own slot while this test still holds one, and that queue kept the
	// package from finishing.
	t.Run("teach dry-run", func(t *testing.T) {
		home := t.TempDir()
		stdout, stderr, code := runDryRunCLI(t, binaryPath, dryRunStoreEnv(t, home, prefix),
			"--home", home, "teach", "--dry-run")
		require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		require.Contains(t, stdout, "dry-run")
		assertNoStoreOrJournal(t, home, cliName)
	})

	t.Run("create dry-run", func(t *testing.T) {
		home := t.TempDir()
		stdout, stderr, code := runDryRunCLI(t, binaryPath, dryRunStoreEnv(t, home, prefix),
			"--home", home, "items", "create", "--dry-run", "--name", "sample")
		require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		require.Contains(t, stdout, "dry_run")
		assertNoStoreOrJournal(t, home, cliName)
	})

	t.Run("create dry-run missing required flag", func(t *testing.T) {
		home := t.TempDir()
		_, _, code := runDryRunCLI(t, binaryPath, dryRunStoreEnv(t, home, prefix),
			"--home", home, "items", "create", "--dry-run")
		require.NotEqual(t, 0, code)
		assertNoStoreOrJournal(t, home, cliName)
	})

	t.Run("list dry-run", func(t *testing.T) {
		home := t.TempDir()
		stdout, stderr, code := runDryRunCLI(t, binaryPath, dryRunStoreEnv(t, home, prefix),
			"--home", home, "items", "list", "--dry-run")
		require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assertNoStoreOrJournal(t, home, cliName)
	})

	t.Run("dry-run equals true", func(t *testing.T) {
		home := t.TempDir()
		_, _, code := runDryRunCLI(t, binaryPath, dryRunStoreEnv(t, home, prefix),
			"--home", home, "teach", "--dry-run=true")
		require.Equal(t, 0, code)
		assertNoStoreOrJournal(t, home, cliName)
	})

	t.Run("parse failure with dry-run", func(t *testing.T) {
		home := t.TempDir()
		_, _, code := runDryRunCLI(t, binaryPath, dryRunStoreEnv(t, home, prefix),
			"--home", home, "teach", "--dry-run", "--not-a-flag")
		require.NotEqual(t, 0, code)
		assertNoStoreOrJournal(t, home, cliName)
	})

	t.Run("parse failure dry-run is query value", func(t *testing.T) {
		home := t.TempDir()
		_, stderr, code := runDryRunCLI(t, binaryPath, dryRunStoreEnv(t, home, prefix),
			"--home", home, "teach", "--query", "--dry-run", "--not-a-flag")
		require.NotEqual(t, 0, code)
		assertNoStore(t, home, cliName)
		require.NotEmpty(t, journalSegments(t, home, cliName), "stderr:\n%s", stderr)
	})

	t.Run("parse failure dry-run overridden", func(t *testing.T) {
		home := t.TempDir()
		_, stderr, code := runDryRunCLI(t, binaryPath, dryRunStoreEnv(t, home, prefix),
			"--home", home, "--dry-run=true", "--dry-run=false", "--not-a-flag")
		require.NotEqual(t, 0, code)
		assertNoStore(t, home, cliName)
		require.NotEmpty(t, journalSegments(t, home, cliName), "stderr:\n%s", stderr)
	})

	t.Run("parse failure without dry-run still journals", func(t *testing.T) {
		home := t.TempDir()
		_, stderr, code := runDryRunCLI(t, binaryPath, dryRunStoreEnv(t, home, prefix),
			"--home", home, "teach", "--not-a-flag")
		require.NotEqual(t, 0, code)
		assertNoStore(t, home, cliName)
		// Parse failure never applies --home, so the journal lands in the
		// XDG state dir rather than <home>/state.
		require.NotEmpty(t, journalSegments(t, home, cliName), "stderr:\n%s", stderr)
	})

	t.Run("dry-run equals false still writes", func(t *testing.T) {
		home := t.TempDir()
		_, _, code := runDryRunCLI(t, binaryPath, dryRunStoreEnv(t, home, prefix),
			"--home", home, "teach", "--dry-run=false")
		require.Equal(t, 2, code)
		db := filepath.Join(home, "data", "data.db")
		require.Equal(t, freshVersion, storeMetaVersion(t, db))
		require.Equal(t, 1, storeCount(t, db, `SELECT COUNT(*) FROM learning_playbooks WHERE query_family = '__seed_meta__'`))
		require.NotEmpty(t, journalSegments(t, home, cliName))
	})

	t.Run("flag value is not a preview", func(t *testing.T) {
		home := t.TempDir()
		stdout, stderr, code := runDryRunCLI(t, binaryPath, dryRunStoreEnv(t, home, prefix),
			"--home", home, "teach", "--query", "--dry-run", "--resource-type", "items", "--resource", "item-1")
		require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		db := filepath.Join(home, "data", "data.db")
		require.Equal(t, freshVersion, storeMetaVersion(t, db))
		require.Equal(t, 1, storeCount(t, db, `SELECT COUNT(*) FROM learning_playbooks WHERE query_family = '__seed_meta__'`))
		require.GreaterOrEqual(t, storeCount(t, db, `SELECT COUNT(*) FROM search_learnings`), 1)
		require.NotEmpty(t, journalSegments(t, home, cliName))
	})

	t.Run("profile dry-run", func(t *testing.T) {
		home := t.TempDir()
		env := dryRunStoreEnv(t, home, prefix)
		stdout, stderr, code := runDryRunCLI(t, binaryPath, env,
			"--home", home, "profile", "save", "preview", "--dry-run")
		require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assertNoStoreOrJournal(t, home, cliName)

		stdout, stderr, code = runDryRunCLI(t, binaryPath, env,
			"--home", home, "items", "create", "--name", "sample", "--profile", "preview")
		require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		require.Contains(t, stdout, "dry_run")
		assertNoStoreOrJournal(t, home, cliName)
	})

	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "old store teach dry-run", args: []string{"teach", "--dry-run"}},
		{name: "old store create dry-run", args: []string{"items", "create", "--dry-run", "--name", "sample"}},
		{name: "old store list dry-run", args: []string{"items", "list", "--dry-run"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			db := filepath.Join(home, "data", "data.db")
			writeOldDeleteStore(t, db)
			before := fileHashes(t, filepath.Join(home, "data"))
			args := append([]string{"--home", home}, tc.args...)
			_, _, code := runDryRunCLI(t, binaryPath, dryRunStoreEnv(t, home, prefix), args...)
			require.Equal(t, 0, code)
			require.Equal(t, before, fileHashes(t, filepath.Join(home, "data")))
			require.Equal(t, 1, storeMetaVersion(t, db))
			_, err := os.Stat(db + "-wal")
			require.ErrorIs(t, err, os.ErrNotExist)
			_, err = os.Stat(db + "-shm")
			require.ErrorIs(t, err, os.ErrNotExist)
			require.Empty(t, journalSegments(t, home, cliName))
		})
	}

	t.Run("old store migrates when not a preview", func(t *testing.T) {
		home := t.TempDir()
		db := filepath.Join(home, "data", "data.db")
		writeOldDeleteStore(t, db)
		// --json skips the bare-command help short-circuit and still fails
		// the required body flag after PreRun has migrated the store.
		stdout, stderr, code := runDryRunCLI(t, binaryPath, dryRunStoreEnv(t, home, prefix),
			"--home", home, "items", "create", "--json")
		require.NotEqual(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		require.Equal(t, freshVersion, storeMetaVersion(t, db), "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		require.Equal(t, 1, storeCount(t, db, `SELECT COUNT(*) FROM learning_playbooks WHERE query_family = '__seed_meta__'`))
		require.NotEmpty(t, journalSegments(t, home, cliName))
	})
}

func dryRunStoreSpec() *spec.APISpec {
	apiSpec := minimalSpec("dry-run-store")
	apiSpec.BaseURL = "http://127.0.0.1:1"
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Learn.Enabled = true
	apiSpec.Cache.Enabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"items": {
			Description: "Manage items",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/items",
					Description: "List items",
					Response:    spec.ResponseDef{Type: "array", Item: "Item"},
				},
				"create": {
					Method:      "POST",
					Path:        "/items",
					Description: "Create an item",
					Body:        []spec.Param{{Name: "name", Type: "string", Required: true}},
					Response:    spec.ResponseDef{Type: "object", Item: "Item"},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Item": {
			Fields: []spec.TypeField{
				{Name: "id", Type: "string"},
				{Name: "name", Type: "string"},
				{Name: "status", Type: "string"},
				{Name: "note", Type: "string"},
			},
		},
	}
	return apiSpec
}

func dryRunStoreEnv(t *testing.T, home, prefix string) []string {
	t.Helper()
	return append(frameworkDryRunEnv(t, home, prefix),
		prefix+"_NO_LEARN=",
		prefix+"_LEARN_NO_CAPTURE=",
		"PRINTING_PRESS_VERIFY=",
		"PRINTING_PRESS_DOGFOOD=",
		"PRINTING_PRESS_VERIFY_LIVE_HTTP=",
	)
}

func runDryRunCLI(t *testing.T, binaryPath string, env []string, args ...string) (string, string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("timed out: %v\nstdout:\n%s\nstderr:\n%s", args, stdout.String(), stderr.String())
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.String(), stderr.String(), exitErr.ExitCode()
	}
	t.Fatalf("run %v: %v\nstderr:\n%s", args, err, stderr.String())
	return "", "", -1
}

func assertNoStoreOrJournal(t *testing.T, home, cliName string) {
	t.Helper()
	assertNoStore(t, home, cliName)
	require.Empty(t, learnDirFiles(t, home, cliName))
}

func assertNoStore(t *testing.T, home, cliName string) {
	t.Helper()
	for _, path := range []string{
		filepath.Join(home, "data", "data.db"),
		filepath.Join(home, ".local", "share", cliName, "data.db"),
	} {
		_, err := os.Stat(path)
		require.ErrorIs(t, err, os.ErrNotExist, path)
	}
}

func journalSegments(t *testing.T, home, cliName string) []string {
	t.Helper()
	var segments []string
	for _, name := range learnDirFiles(t, home, cliName) {
		if strings.HasPrefix(name, "journal-") && strings.HasSuffix(name, ".jsonl") {
			segments = append(segments, name)
		}
	}
	return segments
}

func learnDirFiles(t *testing.T, home, cliName string) []string {
	t.Helper()
	var names []string
	for _, dir := range []string{
		filepath.Join(home, "state", "learn"),
		filepath.Join(home, ".local", "state", cliName, "learn"),
	} {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		require.NoError(t, err)
		for _, entry := range entries {
			if !entry.IsDir() {
				names = append(names, entry.Name())
			}
		}
	}
	return names
}

func writeOldDeleteStore(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	db, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = db.Exec(`PRAGMA journal_mode = DELETE`)
	require.NoError(t, err)
	_, err = db.Exec(`PRAGMA user_version = 1`)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE resources (
		id TEXT NOT NULL,
		resource_type TEXT NOT NULL,
		data JSON NOT NULL,
		synced_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (resource_type, id)
	)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		require.NoError(t, os.RemoveAll(path+suffix))
	}
}

func fileHashes(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	require.NoError(t, err)
	return out
}

func storeMetaVersion(t *testing.T, path string) int {
	t.Helper()
	db := openStoreReadOnly(t, path)
	var version int
	require.NoError(t, db.QueryRow(`PRAGMA user_version`).Scan(&version))
	return version
}

func storeCount(t *testing.T, path, query string) int {
	t.Helper()
	db := openStoreReadOnly(t, path)
	var n int
	require.NoError(t, db.QueryRow(query).Scan(&n))
	return n
}

func openStoreReadOnly(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

const dryRunStoreRuntimeTest = `package cli

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"__MODULE_PATH__/internal/cliutil"
	"__MODULE_PATH__/internal/store"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

func TestDryRunStore_ParsedFlagBeatsArgv(t *testing.T) {
	orig := preRunReached
	t.Cleanup(func() { preRunReached = orig })

	root := RootCmd()
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{name: "bare", args: []string{"teach", "--dry-run"}, want: true},
		{name: "equals true", args: []string{"--dry-run=true", "teach"}, want: true},
		{name: "equals 1", args: []string{"--dry-run=1"}, want: true},
		{name: "equals t", args: []string{"--dry-run=t"}, want: true},
		{name: "equals TRUE", args: []string{"--dry-run=TRUE"}, want: true},
		{name: "spaced true", args: []string{"--dry-run= true "}, want: true},
		{name: "equals false", args: []string{"--dry-run=false", "teach"}, want: false},
		{name: "equals FALSE", args: []string{"--dry-run=False"}, want: false},
		{name: "equals 0", args: []string{"--dry-run=0"}, want: false},
		{name: "equals f", args: []string{"--dry-run=f"}, want: false},
		{name: "equals no", args: []string{"--dry-run=no"}, want: false},
		{name: "equals yes", args: []string{"--dry-run=yes"}, want: false},
		{name: "empty value", args: []string{"--dry-run="}, want: false},
		{name: "absent", args: []string{"teach", "--query", "x"}, want: false},
		{name: "after end of flags", args: []string{"--", "--dry-run"}, want: false},
		{name: "before end of flags", args: []string{"--dry-run", "--", "x"}, want: true},
		{name: "stop before later override", args: []string{"--dry-run", "--", "--dry-run=false"}, want: true},
		{name: "value not flag", args: []string{"--query=--dry-run"}, want: false},
		{name: "query consumes dry-run", args: []string{"teach", "--query", "--dry-run", "--not-a-flag"}, want: false},
		{name: "query consumes then real flag", args: []string{"teach", "--query", "--dry-run", "--dry-run"}, want: true},
		{name: "query equals does not consume", args: []string{"teach", "--query=hello", "--dry-run"}, want: true},
		{name: "query consumes end marker", args: []string{"teach", "--query", "--", "--dry-run"}, want: true},
		{name: "last false wins", args: []string{"--dry-run=true", "--dry-run=false", "--not-a-flag"}, want: false},
		{name: "last true wins", args: []string{"--dry-run=false", "--dry-run=true", "--not-a-flag"}, want: true},
		{name: "bare then false", args: []string{"--dry-run", "--dry-run=false"}, want: false},
		{name: "false then bare", args: []string{"--dry-run=false", "--dry-run"}, want: true},
		{name: "unrecognized does not clear", args: []string{"--dry-run", "--dry-run=yes"}, want: true},
		{name: "empty does not clear", args: []string{"--dry-run", "--dry-run="}, want: true},
		{name: "unknown does not consume", args: []string{"--not-a-flag", "--dry-run"}, want: true},
		{name: "unknown command", args: []string{"nosuch", "--dry-run"}, want: true},
		{name: "bool does not consume", args: []string{"teach", "--json", "--dry-run"}, want: true},
		{name: "home consumes dry-run", args: []string{"teach", "--home", "--dry-run"}, want: false},
		{name: "home equals does not consume", args: []string{"teach", "--home=/tmp", "--dry-run"}, want: true},
		{name: "nested name consumes", args: []string{"items", "create", "--name", "--dry-run"}, want: false},
		{name: "missing value", args: []string{"teach", "--query"}, want: false},
		{name: "bad long syntax", args: []string{"---dry-run"}, want: false},
		{name: "leading equals is not a flag", args: []string{"--=dry-run"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := argsRequestDryRun(root, tc.args); got != tc.want {
				t.Fatalf("argsRequestDryRun(%q) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}

	preRunReached = false
	if !dryRunActive(&rootFlags{dryRun: true}, root) {
		t.Fatal("parsed flag must count before pre-run")
	}
	if dryRunActive(nil, root) != argsRequestDryRun(root, os.Args[1:]) {
		t.Fatal("nil flags before parse must follow argv")
	}
	preRunReached = true
	if dryRunActive(&rootFlags{}, root) {
		t.Fatal("parsed invocation without the flag must not follow argv")
	}
	if !dryRunActive(&rootFlags{dryRun: true}, root) {
		t.Fatal("parsed flag must count after pre-run")
	}
}

func TestDryRunStore_ShortFlagValues(t *testing.T) {
	root := &cobra.Command{Use: "cli"}
	root.PersistentFlags().BoolP("dry-run", "d", false, "")
	root.PersistentFlags().BoolP("json", "j", false, "")
	sub := &cobra.Command{Use: "sub"}
	sub.Flags().StringP("file", "f", "", "")
	root.AddCommand(sub)

	cases := []struct {
		name string
		args []string
		want bool
	}{
		{name: "short bare", args: []string{"-d"}, want: true},
		{name: "short equals false", args: []string{"-d=false"}, want: false},
		{name: "short equals true", args: []string{"-d=true"}, want: true},
		{name: "short last false", args: []string{"-d=true", "-d=false"}, want: false},
		{name: "short last true", args: []string{"-d=false", "-d"}, want: true},
		{name: "file consumes next", args: []string{"sub", "-f", "--dry-run"}, want: false},
		{name: "file consumes short", args: []string{"sub", "-f", "-d"}, want: false},
		{name: "file equals does not consume", args: []string{"sub", "-f=--dry-run", "--dry-run"}, want: true},
		{name: "file attached value", args: []string{"sub", "-f--dry-run"}, want: false},
		{name: "bool cluster does not consume", args: []string{"-jj", "--dry-run"}, want: true},
		{name: "bool then dry-run in cluster", args: []string{"-jd"}, want: true},
		{name: "dry-run then value in cluster", args: []string{"sub", "-df", "--dry-run=false"}, want: true},
		{name: "bool then value in cluster", args: []string{"sub", "-jf", "--dry-run"}, want: false},
		{name: "child sees persistent short", args: []string{"sub", "-d"}, want: true},
		{name: "unknown short does not consume", args: []string{"-z", "--dry-run"}, want: true},
		{name: "earlier short survives consumed token", args: []string{"-d", "sub", "-f", "--dry-run"}, want: true},
		{name: "missing short value", args: []string{"sub", "-f"}, want: false},
		{name: "short unrecognized does not clear", args: []string{"-d", "-d=yes"}, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := argsRequestDryRun(root, tc.args); got != tc.want {
				t.Fatalf("argsRequestDryRun(%q) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestDryRunStore_DryRunDoesNotConsumeLearnInit(t *testing.T) {
	home := t.TempDir()
	origArgs := os.Args
	t.Cleanup(func() {
		os.Args = origArgs
		_, _ = cliutil.SetHomeOverride("")
	})

	os.Args = []string{"cli", "--home", home, "teach", "--dry-run"}
	if err := Execute(); err != nil {
		t.Fatalf("teach --dry-run: %v", err)
	}
	dbPath := filepath.Join(home, "data", "data.db")
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("dry-run store stat: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(home, "state", "learn", "journal-*.jsonl"))
	if err != nil {
		t.Fatalf("glob journal: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("dry-run journal: %v", matches)
	}

	os.Args = []string{"cli", "--home", home, "teach", "--query", "find items", "--resource-type", "items", "--resource", "item-1"}
	if err := Execute(); err != nil {
		t.Fatalf("teach: %v", err)
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath)+"?mode=ro")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	var version, seeds, learnings int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != store.StoreSchemaVersion {
		t.Fatalf("user_version=%d want %d", version, store.StoreSchemaVersion)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM learning_playbooks WHERE query_family = '__seed_meta__'").Scan(&seeds); err != nil {
		t.Fatal(err)
	}
	if seeds != 1 {
		t.Fatalf("seed rows=%d", seeds)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM search_learnings").Scan(&learnings); err != nil {
		t.Fatal(err)
	}
	if learnings < 1 {
		t.Fatalf("learnings=%d", learnings)
	}
}
`
