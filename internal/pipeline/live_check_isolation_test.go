package pipeline

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/platform"
	"github.com/stretchr/testify/require"
)

func quoteLiveCheckArg(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func liveCheckStubPath(dir string) string {
	return platform.ExecutablePath(filepath.Join(dir, "stub"))
}

const liveCheckStoreStubSource = `package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "agent-context" {
		fmt.Println(` + "`" + `{"commands":[]}` + "`" + `)
		return
	}
	if marker := os.Getenv("STUB_MARKER"); marker != "" {
		if err := os.WriteFile(marker, []byte("ran\n"), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "marker: %v\n", err)
			os.Exit(1)
		}
	}
	db, err := storePath(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "store: %v\n", err)
		os.Exit(1)
	}
	before, err := os.ReadFile(db)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", db, err)
		os.Exit(1)
	}
	fmt.Printf("before:%s\n", strings.TrimRight(string(before), "\n"))
	f, err := os.OpenFile(db, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open %s: %v\n", db, err)
		os.Exit(1)
	}
	if _, err := f.WriteString("mutated\n"); err != nil {
		_ = f.Close()
		fmt.Fprintf(os.Stderr, "write %s: %v\n", db, err)
		os.Exit(1)
	}
	if err := f.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "close %s: %v\n", db, err)
		os.Exit(1)
	}
	if os.Getenv("STUB_FAIL") == "1" {
		fmt.Fprintln(os.Stderr, "broken output")
		os.Exit(1)
	}
}

func storePath(args []string) (string, error) {
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			break
		}
		if args[i] == "--db" {
			if i+1 >= len(args) {
				return "", fmt.Errorf("missing --db value")
			}
			return args[i+1], nil
		}
		if value, ok := strings.CutPrefix(args[i], "--db="); ok {
			if value == "" {
				return "", fmt.Errorf("missing --db value")
			}
			return value, nil
		}
	}
	if os.Getenv("STUB_STORE") == "xdg" {
		cli := os.Getenv("STUB_CLI")
		if cli == "" {
			cli = "widget-pp-cli"
		}
		data := os.Getenv("XDG_DATA_HOME")
		if data == "" {
			return "", fmt.Errorf("XDG_DATA_HOME is unset")
		}
		return filepath.Join(data, cli, "data.db"), nil
	}
	return "data.db", nil
}
`

func buildLiveCheckStoreStub(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/livecheckstub\n\ngo 1.23\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(liveCheckStoreStubSource), 0o644))
	bin := liveCheckStubPath(dir)
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "go build stub: %s", out)
	return bin
}

func installLiveCheckStub(t *testing.T, cliDir, built string) {
	t.Helper()
	body, err := os.ReadFile(built)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(liveCheckStubPath(cliDir), body, 0o755))
}

func writeLiveCheckListResearch(t *testing.T, cliDir, example string, extra ...NovelFeature) {
	t.Helper()
	features := []NovelFeature{{
		Name:    "List",
		Command: "list",
		Example: example,
	}}
	features = append(features, extra...)
	writeTestResearchJSON(t, cliDir, features)
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	slices.Sort(names)
	return names
}

func snapshotDir(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(body)
		return nil
	})
	require.NoError(t, err)
	return out
}

func TestSampleReviewStatus(t *testing.T) {
	t.Parallel()

	var nilResult *LiveCheckResult
	require.Equal(t, SampleReviewSkipped, nilResult.SampleReviewStatus())

	cases := []struct {
		name string
		live LiveCheckResult
		want string
	}{
		{name: "unable", live: LiveCheckResult{Unable: true, Passed: 3}, want: SampleReviewSkipped},
		{name: "no samples", live: LiveCheckResult{}, want: SampleReviewSkipped},
		{name: "all skipped", live: LiveCheckResult{Skipped: 4}, want: SampleReviewSkipped},
		{name: "all failed", live: LiveCheckResult{Failed: 3}, want: SampleReviewSkipped},
		{name: "pass", live: LiveCheckResult{Passed: 2}, want: SampleReviewPass},
		{name: "mixed", live: LiveCheckResult{Passed: 1, Failed: 1}, want: SampleReviewFail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, tc.live.SampleReviewStatus())
			if tc.want != SampleReviewPass {
				require.NotEqual(t, SampleReviewPass, tc.live.SampleReviewStatus())
			}
		})
	}
}

func TestRewriteLiveCheckDBArgs(t *testing.T) {
	t.Parallel()

	t.Run("copies file and sidecar", func(t *testing.T) {
		t.Parallel()
		cliDir := t.TempDir()
		src := filepath.Join(cliDir, "store.db")
		require.NoError(t, os.WriteFile(src, []byte("seed"), 0o644))
		require.NoError(t, os.WriteFile(src+"-wal", []byte("wal"), 0o644))
		scratch := t.TempDir()

		got, err := rewriteLiveCheckDBArgs(cliDir, scratch, []string{"list", "--db", src})
		require.NoError(t, err)
		require.NotEqual(t, src, got[2])
		body, err := os.ReadFile(got[2])
		require.NoError(t, err)
		require.Equal(t, "seed", string(body))
		wal, err := os.ReadFile(got[2] + "-wal")
		require.NoError(t, err)
		require.Equal(t, "wal", string(wal))
		unchanged, err := os.ReadFile(src)
		require.NoError(t, err)
		require.Equal(t, "seed", string(unchanged))
		walUnchanged, err := os.ReadFile(src + "-wal")
		require.NoError(t, err)
		require.Equal(t, "wal", string(walUnchanged))
	})

	t.Run("inline flag", func(t *testing.T) {
		t.Parallel()
		cliDir := t.TempDir()
		src := filepath.Join(cliDir, "store.db")
		require.NoError(t, os.WriteFile(src, []byte("seed"), 0o644))
		scratch := t.TempDir()

		got, err := rewriteLiveCheckDBArgs(cliDir, scratch, []string{"list", "--db=" + src})
		require.NoError(t, err)
		require.Len(t, got, 2)
		require.NotContains(t, got[1], src)
		body, err := os.ReadFile(strings.TrimPrefix(got[1], "--db="))
		require.NoError(t, err)
		require.Equal(t, "seed", string(body))
		unchanged, err := os.ReadFile(src)
		require.NoError(t, err)
		require.Equal(t, "seed", string(unchanged))
	})

	t.Run("stops at terminator", func(t *testing.T) {
		t.Parallel()
		cliDir := t.TempDir()
		src := filepath.Join(cliDir, "store.db")
		require.NoError(t, os.WriteFile(src, []byte("seed"), 0o644))
		later := filepath.Join(cliDir, "later.db")
		require.NoError(t, os.WriteFile(later, []byte("later"), 0o644))
		scratch := t.TempDir()
		args := []string{"list", "--db", src, "--", "--db", later}

		got, err := rewriteLiveCheckDBArgs(cliDir, scratch, args)
		require.NoError(t, err)
		require.Equal(t, later, got[5])
		require.NotEqual(t, src, got[2])
		laterBody, err := os.ReadFile(later)
		require.NoError(t, err)
		require.Equal(t, "later", string(laterBody))
	})

	t.Run("missing file is redirected and not created", func(t *testing.T) {
		t.Parallel()
		cliDir := t.TempDir()
		missing := filepath.Join(cliDir, "missing.db")
		scratch := t.TempDir()

		got, err := rewriteLiveCheckDBArgs(cliDir, scratch, []string{"list", "--db", missing})
		require.NoError(t, err)
		require.NotEqual(t, missing, got[2])
		_, err = os.Stat(missing)
		require.ErrorIs(t, err, os.ErrNotExist)
		_, err = os.Stat(got[2])
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("directory is an error", func(t *testing.T) {
		t.Parallel()
		cliDir := t.TempDir()
		dir := filepath.Join(cliDir, "store.db")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		scratch := t.TempDir()

		_, err := rewriteLiveCheckDBArgs(cliDir, scratch, []string{"list", "--db", dir})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not a regular file")
		info, statErr := os.Stat(dir)
		require.NoError(t, statErr)
		require.True(t, info.IsDir())
	})

	t.Run("relative path outside the cli directory", func(t *testing.T) {
		t.Parallel()
		parent := t.TempDir()
		cliDir := filepath.Join(parent, "cli")
		require.NoError(t, os.MkdirAll(cliDir, 0o755))
		outside := filepath.Join(parent, "outside.db")
		require.NoError(t, os.WriteFile(outside, []byte("outside"), 0o644))
		scratch := t.TempDir()

		got, err := rewriteLiveCheckDBArgs(cliDir, scratch, []string{"list", "--db", filepath.Join("..", "outside.db")})
		require.NoError(t, err)
		require.NotEqual(t, outside, got[2])
		body, err := os.ReadFile(got[2])
		require.NoError(t, err)
		require.Equal(t, "outside", string(body))
		unchanged, err := os.ReadFile(outside)
		require.NoError(t, err)
		require.Equal(t, "outside", string(unchanged))
	})

	t.Run("non-regular sidecar", func(t *testing.T) {
		t.Parallel()
		cliDir := t.TempDir()
		src := filepath.Join(cliDir, "store.db")
		require.NoError(t, os.WriteFile(src, []byte("seed"), 0o644))
		require.NoError(t, os.MkdirAll(src+"-wal", 0o755))
		scratch := t.TempDir()

		_, err := rewriteLiveCheckDBArgs(cliDir, scratch, []string{"list", "--db", src})
		require.Error(t, err)
		require.Contains(t, err.Error(), "not a regular file")
		body, readErr := os.ReadFile(src)
		require.NoError(t, readErr)
		require.Equal(t, "seed", string(body))
	})

	t.Run("empty and dangling db flags stay unchanged", func(t *testing.T) {
		t.Parallel()
		args := []string{"list", "--db=", "--db"}
		got, err := rewriteLiveCheckDBArgs(t.TempDir(), t.TempDir(), args)
		require.NoError(t, err)
		require.Equal(t, args, got)
	})
}

func TestLiveCheckDefaultStoreNames(t *testing.T) {
	t.Parallel()
	require.True(t, isLiveCheckDefaultStoreName("data.db"))
	require.True(t, isLiveCheckDefaultStoreName("data-abc.db"))
	require.True(t, isLiveCheckDefaultStoreName("data.db-wal"))
	require.True(t, isLiveCheckDefaultStoreName("data-abc.db-journal"))
	require.False(t, isLiveCheckDefaultStoreName("notes.db"))
	require.False(t, isLiveCheckDefaultStoreName("database.db"))
	require.False(t, isLiveCheckDefaultStoreName("data.db.bak"))
}

func TestCopyLiveCheckSQLiteDir(t *testing.T) {
	t.Parallel()

	t.Run("blank source creates the destination directory", func(t *testing.T) {
		t.Parallel()
		dst := filepath.Join(t.TempDir(), "data")
		require.NoError(t, copyLiveCheckSQLiteDir("  ", dst))
		info, err := os.Stat(dst)
		require.NoError(t, err)
		require.True(t, info.IsDir())
	})

	t.Run("missing source creates the destination directory", func(t *testing.T) {
		t.Parallel()
		dst := filepath.Join(t.TempDir(), "data")
		require.NoError(t, copyLiveCheckSQLiteDir(filepath.Join(t.TempDir(), "absent"), dst))
		info, err := os.Stat(dst)
		require.NoError(t, err)
		require.True(t, info.IsDir())
	})

	t.Run("file source is an error", func(t *testing.T) {
		t.Parallel()
		src := filepath.Join(t.TempDir(), "data")
		require.NoError(t, os.WriteFile(src, []byte("keep"), 0o644))
		err := copyLiveCheckSQLiteDir(src, filepath.Join(t.TempDir(), "dst"))
		require.Error(t, err)
		require.Contains(t, err.Error(), "not a directory")
		body, readErr := os.ReadFile(src)
		require.NoError(t, readErr)
		require.Equal(t, "keep", string(body))
	})

	t.Run("missing cli directory is an error", func(t *testing.T) {
		t.Parallel()
		err := copyLiveCheckCWDStores(filepath.Join(t.TempDir(), "missing"), t.TempDir())
		require.Error(t, err)
		require.Contains(t, err.Error(), "isolating sample store")
	})
}

func TestLiveCheckSampleProbeDoesNotMutateStore(t *testing.T) {
	built := buildLiveCheckStoreStub(t)

	t.Run("cwd store stays byte-identical across two runs", func(t *testing.T) {
		cliDir := t.TempDir()
		installLiveCheckStub(t, cliDir, built)
		require.NoError(t, os.WriteFile(filepath.Join(cliDir, "data.db"), []byte("seed-row\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(cliDir, "data.db-wal"), []byte("wal-bytes"), 0o644))
		writeLiveCheckListResearch(t, cliDir, "stub list", NovelFeature{
			Name:    "ListAgain",
			Command: "list",
			Example: "stub list",
		})
		opts := LiveCheckOptions{CLIDir: cliDir, BinaryName: "stub", Timeout: liveCheckIntegrationTimeout, Concurrency: 2}
		namesBefore := dirNames(t, cliDir)
		dbBefore, err := os.ReadFile(filepath.Join(cliDir, "data.db"))
		require.NoError(t, err)
		walBefore, err := os.ReadFile(filepath.Join(cliDir, "data.db-wal"))
		require.NoError(t, err)

		run := func() *LiveCheckResult {
			t.Helper()
			result := RunLiveCheck(opts)
			require.False(t, result.Unable, result.Reason)
			require.Equal(t, SampleReviewPass, result.SampleReviewStatus())
			require.Equal(t, 2, result.Passed)
			for _, feature := range result.Features {
				require.Equal(t, StatusPass, feature.Status, feature.Reason)
				require.Contains(t, feature.OutputSample, "seed-row")
				require.NotContains(t, feature.OutputSample, "mutated")
			}
			return result
		}
		first := run()
		second := run()
		require.Equal(t, first.Features[0].OutputSample, second.Features[0].OutputSample)
		require.Equal(t, first.Features[1].OutputSample, second.Features[1].OutputSample)
		require.Equal(t, namesBefore, dirNames(t, cliDir))
		dbAfter, err := os.ReadFile(filepath.Join(cliDir, "data.db"))
		require.NoError(t, err)
		require.Equal(t, dbBefore, dbAfter)
		walAfter, err := os.ReadFile(filepath.Join(cliDir, "data.db-wal"))
		require.NoError(t, err)
		require.Equal(t, walBefore, walAfter)
	})

	t.Run("absolute --db stays byte-identical", func(t *testing.T) {
		cliDir := t.TempDir()
		installLiveCheckStub(t, cliDir, built)
		db := filepath.Join(t.TempDir(), "store.db")
		require.NoError(t, os.WriteFile(db, []byte("seed-row\n"), 0o644))
		writeLiveCheckListResearch(t, cliDir, "stub list --db "+quoteLiveCheckArg(db))
		opts := LiveCheckOptions{CLIDir: cliDir, BinaryName: "stub", Timeout: liveCheckIntegrationTimeout}

		run := func() *LiveCheckResult {
			t.Helper()
			result := RunLiveCheck(opts)
			require.False(t, result.Unable, result.Reason)
			require.Equal(t, StatusPass, result.Features[0].Status, result.Features[0].Reason)
			require.Contains(t, result.Features[0].OutputSample, "seed-row")
			require.NotContains(t, result.Features[0].OutputSample, "mutated")
			return result
		}
		first := run()
		second := run()
		require.Equal(t, first.Features[0].OutputSample, second.Features[0].OutputSample)
		body, err := os.ReadFile(db)
		require.NoError(t, err)
		require.Equal(t, "seed-row\n", string(body))
	})

	t.Run("inline --db= stays byte-identical", func(t *testing.T) {
		cliDir := t.TempDir()
		installLiveCheckStub(t, cliDir, built)
		db := filepath.Join(t.TempDir(), "store.db")
		require.NoError(t, os.WriteFile(db, []byte("seed-row\n"), 0o644))
		writeLiveCheckListResearch(t, cliDir, "stub list --db="+quoteLiveCheckArg(db))
		result := RunLiveCheck(LiveCheckOptions{CLIDir: cliDir, BinaryName: "stub", Timeout: liveCheckIntegrationTimeout})
		require.False(t, result.Unable, result.Reason)
		require.Equal(t, StatusPass, result.Features[0].Status, result.Features[0].Reason)
		require.Contains(t, result.Features[0].OutputSample, "seed-row")
		body, err := os.ReadFile(db)
		require.NoError(t, err)
		require.Equal(t, "seed-row\n", string(body))
	})

	t.Run("relative --db stays byte-identical", func(t *testing.T) {
		cliDir := t.TempDir()
		installLiveCheckStub(t, cliDir, built)
		require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "nested"), 0o755))
		db := filepath.Join(cliDir, "nested", "store.db")
		require.NoError(t, os.WriteFile(db, []byte("seed-row\n"), 0o644))
		writeLiveCheckListResearch(t, cliDir, "stub list --db "+quoteLiveCheckArg(filepath.Join("nested", "store.db")))
		result := RunLiveCheck(LiveCheckOptions{CLIDir: cliDir, BinaryName: "stub", Timeout: liveCheckIntegrationTimeout})
		require.False(t, result.Unable, result.Reason)
		require.Equal(t, StatusPass, result.Features[0].Status, result.Features[0].Reason)
		require.Contains(t, result.Features[0].OutputSample, "seed-row")
		body, err := os.ReadFile(db)
		require.NoError(t, err)
		require.Equal(t, "seed-row\n", string(body))
	})

	t.Run("missing --db is not created", func(t *testing.T) {
		cliDir := t.TempDir()
		installLiveCheckStub(t, cliDir, built)
		missing := filepath.Join(t.TempDir(), "missing.db")
		writeLiveCheckListResearch(t, cliDir, "stub list --db "+quoteLiveCheckArg(missing))
		result := RunLiveCheck(LiveCheckOptions{CLIDir: cliDir, BinaryName: "stub", Timeout: liveCheckIntegrationTimeout})
		require.False(t, result.Unable, result.Reason)
		require.Equal(t, StatusPass, result.Features[0].Status, result.Features[0].Reason)
		require.Contains(t, result.Features[0].OutputSample, "before:")
		_, err := os.Stat(missing)
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("db directory does not run the command", func(t *testing.T) {
		cliDir := t.TempDir()
		installLiveCheckStub(t, cliDir, built)
		db := filepath.Join(t.TempDir(), "store.db")
		require.NoError(t, os.MkdirAll(db, 0o755))
		marker := filepath.Join(t.TempDir(), "marker")
		t.Setenv("STUB_MARKER", marker)
		writeLiveCheckListResearch(t, cliDir, "stub list --db "+quoteLiveCheckArg(db))
		result := RunLiveCheck(LiveCheckOptions{CLIDir: cliDir, BinaryName: "stub", Timeout: liveCheckIntegrationTimeout})
		require.False(t, result.Unable, result.Reason)
		require.Equal(t, StatusFail, result.Features[0].Status)
		require.Contains(t, result.Features[0].Reason, "not a regular file")
		_, err := os.Stat(marker)
		require.ErrorIs(t, err, os.ErrNotExist)
		info, err := os.Stat(db)
		require.NoError(t, err)
		require.True(t, info.IsDir())
	})

	t.Run("cwd data.db directory does not run the command", func(t *testing.T) {
		cliDir := t.TempDir()
		installLiveCheckStub(t, cliDir, built)
		db := filepath.Join(cliDir, "data.db")
		require.NoError(t, os.MkdirAll(db, 0o755))
		marker := filepath.Join(t.TempDir(), "marker")
		t.Setenv("STUB_MARKER", marker)
		writeLiveCheckListResearch(t, cliDir, "stub list")
		result := RunLiveCheck(LiveCheckOptions{CLIDir: cliDir, BinaryName: "stub", Timeout: liveCheckIntegrationTimeout})
		require.False(t, result.Unable, result.Reason)
		require.Equal(t, StatusFail, result.Features[0].Status)
		require.Contains(t, result.Features[0].Reason, "not a regular file")
		_, err := os.Stat(marker)
		require.ErrorIs(t, err, os.ErrNotExist)
		info, err := os.Stat(db)
		require.NoError(t, err)
		require.True(t, info.IsDir())
	})

	t.Run("unrelated db directory does not fail the probe", func(t *testing.T) {
		cliDir := t.TempDir()
		installLiveCheckStub(t, cliDir, built)
		require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "notes.db"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(cliDir, "data.db"), []byte("seed-row\n"), 0o644))
		writeLiveCheckListResearch(t, cliDir, "stub list")
		result := RunLiveCheck(LiveCheckOptions{CLIDir: cliDir, BinaryName: "stub", Timeout: liveCheckIntegrationTimeout})
		require.False(t, result.Unable, result.Reason)
		require.Equal(t, StatusPass, result.Features[0].Status, result.Features[0].Reason)
		body, err := os.ReadFile(filepath.Join(cliDir, "data.db"))
		require.NoError(t, err)
		require.Equal(t, "seed-row\n", string(body))
		info, err := os.Stat(filepath.Join(cliDir, "notes.db"))
		require.NoError(t, err)
		require.True(t, info.IsDir())
	})

	t.Run("operator store is copied and not written back", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
		t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
		t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
		xdg := filepath.Join(home, "xdg")
		t.Setenv("XDG_DATA_HOME", xdg)
		t.Setenv("STUB_STORE", "xdg")
		t.Setenv("STUB_CLI", "widget-pp-cli")

		storeDir := filepath.Join(xdg, "widget-pp-cli")
		require.NoError(t, os.MkdirAll(storeDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(storeDir, "data.db"), []byte("seed-row\n"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(storeDir, "data.db-wal"), []byte("wal-bytes"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(storeDir, "credentials.toml"), []byte("do-not-touch"), 0o644))
		before := snapshotDir(t, xdg)

		cliDir := t.TempDir()
		installLiveCheckStub(t, cliDir, built)
		require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "cmd", "widget-pp-cli"), 0o755))
		writeLiveCheckListResearch(t, cliDir, "stub list")
		opts := LiveCheckOptions{CLIDir: cliDir, BinaryName: "stub", Timeout: liveCheckIntegrationTimeout}

		run := func() *LiveCheckResult {
			t.Helper()
			result := RunLiveCheck(opts)
			require.False(t, result.Unable, result.Reason)
			require.Equal(t, SampleReviewPass, result.SampleReviewStatus())
			require.Equal(t, StatusPass, result.Features[0].Status, result.Features[0].Reason)
			require.Contains(t, result.Features[0].OutputSample, "seed-row")
			require.NotContains(t, result.Features[0].OutputSample, "mutated")
			return result
		}
		first := run()
		second := run()
		require.Equal(t, first.Features[0].OutputSample, second.Features[0].OutputSample)
		require.Equal(t, before, snapshotDir(t, xdg))
	})

	t.Run("operator data path that is a file does not run the command", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		xdg := filepath.Join(home, "xdg")
		require.NoError(t, os.MkdirAll(xdg, 0o755))
		store := filepath.Join(xdg, "widget-pp-cli")
		require.NoError(t, os.WriteFile(store, []byte("keep"), 0o644))
		t.Setenv("XDG_DATA_HOME", xdg)
		t.Setenv("STUB_STORE", "xdg")
		t.Setenv("STUB_CLI", "widget-pp-cli")
		marker := filepath.Join(t.TempDir(), "marker")
		t.Setenv("STUB_MARKER", marker)

		cliDir := t.TempDir()
		installLiveCheckStub(t, cliDir, built)
		require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "cmd", "widget-pp-cli"), 0o755))
		writeLiveCheckListResearch(t, cliDir, "stub list")
		result := RunLiveCheck(LiveCheckOptions{CLIDir: cliDir, BinaryName: "stub", Timeout: liveCheckIntegrationTimeout})
		require.False(t, result.Unable, result.Reason)
		require.Equal(t, StatusFail, result.Features[0].Status)
		require.Contains(t, result.Features[0].Reason, "not a directory")
		_, err := os.Stat(marker)
		require.ErrorIs(t, err, os.ErrNotExist)
		body, err := os.ReadFile(store)
		require.NoError(t, err)
		require.Equal(t, "keep", string(body))
	})

	t.Run("failed sample does not mutate the store and review is skipped", func(t *testing.T) {
		cliDir := t.TempDir()
		installLiveCheckStub(t, cliDir, built)
		db := filepath.Join(cliDir, "data.db")
		require.NoError(t, os.WriteFile(db, []byte("seed-row\n"), 0o644))
		t.Setenv("STUB_FAIL", "1")
		writeLiveCheckListResearch(t, cliDir, "stub list")
		opts := LiveCheckOptions{CLIDir: cliDir, BinaryName: "stub", Timeout: liveCheckIntegrationTimeout}

		run := func() *LiveCheckResult {
			t.Helper()
			result := RunLiveCheck(opts)
			require.False(t, result.Unable, result.Reason)
			require.Equal(t, 0, result.Passed)
			require.Equal(t, 1, result.Failed)
			require.Equal(t, SampleReviewSkipped, result.SampleReviewStatus())
			require.NotEqual(t, SampleReviewPass, result.SampleReviewStatus())
			require.Equal(t, StatusFail, result.Features[0].Status)
			require.Contains(t, result.Features[0].Reason, "broken output")
			body, err := json.Marshal(result)
			require.NoError(t, err)
			require.Contains(t, string(body), `"review_status":"skipped"`)
			require.NotContains(t, string(body), `"review_status":"pass"`)
			return result
		}
		first := run()
		second := run()
		require.Equal(t, first.Features[0].Reason, second.Features[0].Reason)
		body, err := os.ReadFile(db)
		require.NoError(t, err)
		require.Equal(t, "seed-row\n", string(body))
	})

	t.Run("skipped mutating sample is not a pass", func(t *testing.T) {
		cliDir := t.TempDir()
		installLiveCheckStub(t, cliDir, built)
		db := filepath.Join(cliDir, "data.db")
		require.NoError(t, os.WriteFile(db, []byte("seed-row\n"), 0o644))
		marker := filepath.Join(t.TempDir(), "marker")
		t.Setenv("STUB_MARKER", marker)
		writeTestResearchJSON(t, cliDir, []NovelFeature{{
			Name:    "Delete",
			Command: "delete",
			Example: "stub delete",
		}})
		result := RunLiveCheck(LiveCheckOptions{CLIDir: cliDir, BinaryName: "stub", Timeout: liveCheckIntegrationTimeout})
		require.False(t, result.Unable, result.Reason)
		require.Equal(t, 0, result.Passed)
		require.Equal(t, 1, result.Skipped)
		require.Equal(t, StatusSkip, result.Features[0].Status)
		require.Equal(t, SampleReviewSkipped, result.SampleReviewStatus())
		body, err := json.Marshal(result)
		require.NoError(t, err)
		require.Contains(t, string(body), `"review_status":"skipped"`)
		require.NotContains(t, string(body), `"review_status":"pass"`)
		stored, err := os.ReadFile(db)
		require.NoError(t, err)
		require.Equal(t, "seed-row\n", string(stored))
		_, err = os.Stat(marker)
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}
