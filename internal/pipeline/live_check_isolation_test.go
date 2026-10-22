package pipeline

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	if db, ok, err := flagValue(args, "--db"); ok || err != nil {
		return db, err
	}
	if home, ok, err := flagValue(args, "--home"); ok || err != nil {
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "data", "data.db"), nil
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

func flagValue(args []string, name string) (string, bool, error) {
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			break
		}
		if args[i] == name {
			if i+1 >= len(args) {
				return "", false, fmt.Errorf("missing %s value", name)
			}
			return args[i+1], true, nil
		}
		if value, ok := strings.CutPrefix(args[i], name+"="); ok {
			if value == "" {
				return "", false, fmt.Errorf("missing %s value", name)
			}
			return value, true, nil
		}
	}
	return "", false, nil
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
		want SampleReviewStatus
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

		got, err := rewriteLiveCheckDBArgs(cliDir, scratch, []string{"list", "--db", src}, nil)
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

		got, err := rewriteLiveCheckDBArgs(cliDir, scratch, []string{"list", "--db=" + src}, nil)
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

		got, err := rewriteLiveCheckDBArgs(cliDir, scratch, args, nil)
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

		got, err := rewriteLiveCheckDBArgs(cliDir, scratch, []string{"list", "--db", missing}, nil)
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

		_, err := rewriteLiveCheckDBArgs(cliDir, scratch, []string{"list", "--db", dir}, nil)
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

		got, err := rewriteLiveCheckDBArgs(cliDir, scratch, []string{"list", "--db", filepath.Join("..", "outside.db")}, nil)
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

		_, err := rewriteLiveCheckDBArgs(cliDir, scratch, []string{"list", "--db", src}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not a regular file")
		body, readErr := os.ReadFile(src)
		require.NoError(t, readErr)
		require.Equal(t, "seed", string(body))
	})

	t.Run("empty and dangling db flags stay unchanged", func(t *testing.T) {
		t.Parallel()
		args := []string{"list", "--db=", "--db"}
		got, err := rewriteLiveCheckDBArgs(t.TempDir(), t.TempDir(), args, nil)
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
		require.NoError(t, copyLiveCheckSQLiteDir("  ", dst, nil))
		info, err := os.Stat(dst)
		require.NoError(t, err)
		require.True(t, info.IsDir())
	})

	t.Run("missing source creates the destination directory", func(t *testing.T) {
		t.Parallel()
		dst := filepath.Join(t.TempDir(), "data")
		require.NoError(t, copyLiveCheckSQLiteDir(filepath.Join(t.TempDir(), "absent"), dst, nil))
		info, err := os.Stat(dst)
		require.NoError(t, err)
		require.True(t, info.IsDir())
	})

	t.Run("file source is an error", func(t *testing.T) {
		t.Parallel()
		src := filepath.Join(t.TempDir(), "data")
		require.NoError(t, os.WriteFile(src, []byte("keep"), 0o644))
		err := copyLiveCheckSQLiteDir(src, filepath.Join(t.TempDir(), "dst"), nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not a directory")
		body, readErr := os.ReadFile(src)
		require.NoError(t, readErr)
		require.Equal(t, "keep", string(body))
	})

	t.Run("missing cli directory is an error", func(t *testing.T) {
		t.Parallel()
		err := copyLiveCheckCWDStores(filepath.Join(t.TempDir(), "missing"), t.TempDir(), nil)
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

	t.Run("absolute --home stays byte-identical across two runs", func(t *testing.T) {
		cliDir := t.TempDir()
		installLiveCheckStub(t, cliDir, built)
		home := filepath.Join(t.TempDir(), "operator-home")
		db := filepath.Join(home, "data", "data.db")
		require.NoError(t, os.MkdirAll(filepath.Dir(db), 0o755))
		require.NoError(t, os.WriteFile(db, []byte("seed-row\n"), 0o644))
		writeLiveCheckListResearch(t, cliDir, "stub list --home "+quoteLiveCheckArg(home))
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
		body, err := os.ReadFile(db)
		require.NoError(t, err)
		require.Equal(t, "seed-row\n", string(body))
	})

	t.Run("inline --home= stays byte-identical", func(t *testing.T) {
		cliDir := t.TempDir()
		installLiveCheckStub(t, cliDir, built)
		home := filepath.Join(t.TempDir(), "operator-home")
		db := filepath.Join(home, "data", "data.db")
		require.NoError(t, os.MkdirAll(filepath.Dir(db), 0o755))
		require.NoError(t, os.WriteFile(db, []byte("seed-row\n"), 0o644))
		writeLiveCheckListResearch(t, cliDir, "stub list --home="+quoteLiveCheckArg(home))
		result := RunLiveCheck(LiveCheckOptions{CLIDir: cliDir, BinaryName: "stub", Timeout: liveCheckIntegrationTimeout})
		require.False(t, result.Unable, result.Reason)
		require.Equal(t, StatusPass, result.Features[0].Status, result.Features[0].Reason)
		require.Contains(t, result.Features[0].OutputSample, "seed-row")
		body, err := os.ReadFile(db)
		require.NoError(t, err)
		require.Equal(t, "seed-row\n", string(body))
	})

	t.Run("relative --home stays byte-identical", func(t *testing.T) {
		parent := t.TempDir()
		cliDir := filepath.Join(parent, "cli")
		require.NoError(t, os.MkdirAll(cliDir, 0o755))
		installLiveCheckStub(t, cliDir, built)
		home := filepath.Join(parent, "outside")
		db := filepath.Join(home, "data", "data.db")
		require.NoError(t, os.MkdirAll(filepath.Dir(db), 0o755))
		require.NoError(t, os.WriteFile(db, []byte("seed-row\n"), 0o644))
		writeLiveCheckListResearch(t, cliDir, "stub list --home "+quoteLiveCheckArg(filepath.Join("..", "outside")))
		result := RunLiveCheck(LiveCheckOptions{CLIDir: cliDir, BinaryName: "stub", Timeout: liveCheckIntegrationTimeout})
		require.False(t, result.Unable, result.Reason)
		require.Equal(t, StatusPass, result.Features[0].Status, result.Features[0].Reason)
		require.Contains(t, result.Features[0].OutputSample, "seed-row")
		body, err := os.ReadFile(db)
		require.NoError(t, err)
		require.Equal(t, "seed-row\n", string(body))
	})

	t.Run("missing --home is not created", func(t *testing.T) {
		cliDir := t.TempDir()
		installLiveCheckStub(t, cliDir, built)
		missing := filepath.Join(t.TempDir(), "missing-home")
		writeLiveCheckListResearch(t, cliDir, "stub list --home "+quoteLiveCheckArg(missing))
		result := RunLiveCheck(LiveCheckOptions{CLIDir: cliDir, BinaryName: "stub", Timeout: liveCheckIntegrationTimeout})
		require.False(t, result.Unable, result.Reason)
		require.Equal(t, StatusPass, result.Features[0].Status, result.Features[0].Reason)
		require.Contains(t, result.Features[0].OutputSample, "before:")
		_, err := os.Stat(missing)
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("home that is a file does not run the command", func(t *testing.T) {
		cliDir := t.TempDir()
		installLiveCheckStub(t, cliDir, built)
		home := filepath.Join(t.TempDir(), "home-file")
		require.NoError(t, os.WriteFile(home, []byte("keep"), 0o644))
		marker := filepath.Join(t.TempDir(), "marker")
		t.Setenv("STUB_MARKER", marker)
		writeLiveCheckListResearch(t, cliDir, "stub list --home "+quoteLiveCheckArg(home))
		result := RunLiveCheck(LiveCheckOptions{CLIDir: cliDir, BinaryName: "stub", Timeout: liveCheckIntegrationTimeout})
		require.False(t, result.Unable, result.Reason)
		require.Equal(t, StatusFail, result.Features[0].Status)
		require.Contains(t, result.Features[0].Reason, "not a directory")
		_, err := os.Stat(marker)
		require.ErrorIs(t, err, os.ErrNotExist)
		body, err := os.ReadFile(home)
		require.NoError(t, err)
		require.Equal(t, "keep", string(body))
	})
}

func TestRewriteLiveCheckHomeArgs(t *testing.T) {
	t.Parallel()

	t.Run("copies the data directory", func(t *testing.T) {
		t.Parallel()
		cliDir := t.TempDir()
		home := filepath.Join(cliDir, "home")
		db := filepath.Join(home, "data", "data.db")
		require.NoError(t, os.MkdirAll(filepath.Dir(db), 0o755))
		require.NoError(t, os.WriteFile(db, []byte("seed"), 0o644))
		scratch := t.TempDir()

		got, err := rewriteLiveCheckHomeArgs(cliDir, scratch, []string{"list", "--home", home}, nil)
		require.NoError(t, err)
		require.NotEqual(t, home, got[2])
		body, err := os.ReadFile(filepath.Join(got[2], "data", "data.db"))
		require.NoError(t, err)
		require.Equal(t, "seed", string(body))
		unchanged, err := os.ReadFile(db)
		require.NoError(t, err)
		require.Equal(t, "seed", string(unchanged))
	})

	t.Run("inline flag", func(t *testing.T) {
		t.Parallel()
		cliDir := t.TempDir()
		home := filepath.Join(cliDir, "home")
		db := filepath.Join(home, "data", "data.db")
		require.NoError(t, os.MkdirAll(filepath.Dir(db), 0o755))
		require.NoError(t, os.WriteFile(db, []byte("seed"), 0o644))
		scratch := t.TempDir()

		got, err := rewriteLiveCheckHomeArgs(cliDir, scratch, []string{"list", "--home=" + home}, nil)
		require.NoError(t, err)
		require.Len(t, got, 2)
		rewritten := strings.TrimPrefix(got[1], "--home=")
		require.NotEqual(t, home, rewritten)
		body, err := os.ReadFile(filepath.Join(rewritten, "data", "data.db"))
		require.NoError(t, err)
		require.Equal(t, "seed", string(body))
	})

	t.Run("stops at terminator", func(t *testing.T) {
		t.Parallel()
		cliDir := t.TempDir()
		home := filepath.Join(cliDir, "home")
		require.NoError(t, os.MkdirAll(filepath.Join(home, "data"), 0o755))
		later := filepath.Join(cliDir, "later")
		require.NoError(t, os.MkdirAll(filepath.Join(later, "data"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(later, "data", "data.db"), []byte("later"), 0o644))
		scratch := t.TempDir()
		args := []string{"list", "--home", home, "--", "--home", later}

		got, err := rewriteLiveCheckHomeArgs(cliDir, scratch, args, nil)
		require.NoError(t, err)
		require.Equal(t, later, got[5])
		require.NotEqual(t, home, got[2])
		laterBody, err := os.ReadFile(filepath.Join(later, "data", "data.db"))
		require.NoError(t, err)
		require.Equal(t, "later", string(laterBody))
	})

	t.Run("missing home is redirected and not created", func(t *testing.T) {
		t.Parallel()
		cliDir := t.TempDir()
		missing := filepath.Join(cliDir, "missing-home")
		scratch := t.TempDir()

		got, err := rewriteLiveCheckHomeArgs(cliDir, scratch, []string{"list", "--home", missing}, nil)
		require.NoError(t, err)
		require.NotEqual(t, missing, got[2])
		_, err = os.Stat(missing)
		require.ErrorIs(t, err, os.ErrNotExist)
		info, err := os.Stat(filepath.Join(got[2], "data"))
		require.NoError(t, err)
		require.True(t, info.IsDir())
	})

	t.Run("file home is an error", func(t *testing.T) {
		t.Parallel()
		cliDir := t.TempDir()
		home := filepath.Join(cliDir, "home")
		require.NoError(t, os.WriteFile(home, []byte("keep"), 0o644))
		scratch := t.TempDir()

		_, err := rewriteLiveCheckHomeArgs(cliDir, scratch, []string{"list", "--home", home}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "not a directory")
		require.Contains(t, err.Error(), "isolating sample store")
		body, readErr := os.ReadFile(home)
		require.NoError(t, readErr)
		require.Equal(t, "keep", string(body))
	})

	t.Run("relative path outside the cli directory", func(t *testing.T) {
		t.Parallel()
		parent := t.TempDir()
		cliDir := filepath.Join(parent, "cli")
		require.NoError(t, os.MkdirAll(cliDir, 0o755))
		home := filepath.Join(parent, "outside")
		db := filepath.Join(home, "data", "data.db")
		require.NoError(t, os.MkdirAll(filepath.Dir(db), 0o755))
		require.NoError(t, os.WriteFile(db, []byte("outside"), 0o644))
		scratch := t.TempDir()

		got, err := rewriteLiveCheckHomeArgs(cliDir, scratch, []string{"list", "--home", filepath.Join("..", "outside")}, nil)
		require.NoError(t, err)
		require.NotEqual(t, home, got[2])
		body, err := os.ReadFile(filepath.Join(got[2], "data", "data.db"))
		require.NoError(t, err)
		require.Equal(t, "outside", string(body))
		unchanged, err := os.ReadFile(db)
		require.NoError(t, err)
		require.Equal(t, "outside", string(unchanged))
	})

	t.Run("empty and dangling home flags stay unchanged", func(t *testing.T) {
		t.Parallel()
		args := []string{"list", "--home=", "--home"}
		got, err := rewriteLiveCheckHomeArgs(t.TempDir(), t.TempDir(), args, nil)
		require.NoError(t, err)
		require.Equal(t, args, got)
	})
}

func TestSQLiteSnapshotDSN(t *testing.T) {
	t.Parallel()

	t.Run("locking read does not depend on the wal file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		for _, name := range []string{"absent.db", "empty.db", "frames.db"} {
			src := filepath.Join(dir, name)
			require.NoError(t, os.WriteFile(src, []byte("x"), 0o644))
			switch name {
			case "empty.db":
				require.NoError(t, os.WriteFile(src+"-wal", nil, 0o644))
			case "frames.db":
				require.NoError(t, os.WriteFile(src+"-wal", []byte("frame"), 0o644))
			}
			dsn := sqliteSnapshotDSN(src)
			require.Contains(t, dsn, "mode=ro")
			require.Contains(t, dsn, "busy_timeout(5000)")
			require.NotContains(t, dsn, "immutable=1")
		}
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "dir.db-wal"), 0o755))
		dirDSN := sqliteSnapshotDSN(filepath.Join(dir, "dir.db"))
		require.NotContains(t, dirDSN, "immutable=1")
	})

	t.Run("ordinary path is not percent-encoded", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join("work", "data.db")
		require.Equal(t, "file:"+filepath.ToSlash(path), sqliteFileURI(path))
		require.Equal(t, "file:C:/cli/data.db", sqliteFileURI("C:/cli/data.db"))
		require.Equal(t, "file:a&b.db", sqliteFileURI("a&b.db"))
	})

	t.Run("hash question and percent stay in the filename", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join("work", "client#1", "data%2E.db")
		dsn := sqliteSnapshotDSN(path)
		filePart, query, ok := strings.Cut(dsn, "?")
		require.True(t, ok)
		require.Equal(t, sqliteFileURI(path), filePart)
		require.Contains(t, filePart, "client%231")
		require.Contains(t, filePart, "data%252E.db")
		require.NotContains(t, filePart, "#")
		require.True(t, strings.HasPrefix(query, "mode=ro"))
		require.Contains(t, query, "busy_timeout(5000)")
		require.NotContains(t, query, "immutable=1")

		// ? is illegal in a Windows filename, so this stays a string.
		question := filepath.Join("work", "client?1", "data.db")
		qdsn := sqliteSnapshotDSN(question)
		qFile, qQuery, ok := strings.Cut(qdsn, "?")
		require.True(t, ok)
		require.Equal(t, sqliteFileURI(question), qFile)
		require.Contains(t, qFile, "client%3F1")
		require.NotContains(t, qFile, "?")
		require.True(t, strings.HasPrefix(qQuery, "mode=ro"))
	})

	t.Run("quote in the destination path is escaped", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		src := filepath.Join(dir, "data.db")
		seedLiveCheckSQLite(t, src, "seed")
		dst := filepath.Join(t.TempDir(), "o'brien", "copy.db")
		require.NoError(t, stageSQLiteFile(src, dst, nil))
		require.Equal(t, []string{"seed"}, liveCheckSQLiteValues(t, dst))
		require.Equal(t, "'o''brien'", quoteSQLiteString("o'brien"))
	})
}

func TestLiveCheckCopyBudget(t *testing.T) {
	t.Parallel()

	t.Run("nil budget is unlimited", func(t *testing.T) {
		t.Parallel()
		var budget *liveCheckCopyBudget
		require.NoError(t, budget.consume(1<<30))
	})

	t.Run("negative size is an error", func(t *testing.T) {
		t.Parallel()
		budget := &liveCheckCopyBudget{max: 4}
		err := budget.consume(-1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "negative")
		require.Equal(t, int64(0), budget.used)
	})

	t.Run("exact fit is accepted and the next byte is refused", func(t *testing.T) {
		t.Parallel()
		budget := &liveCheckCopyBudget{max: 4}
		require.NoError(t, budget.consume(4))
		require.Equal(t, int64(4), budget.used)
		err := budget.consume(1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "store copy exceeds")
		require.Equal(t, int64(4), budget.used)
	})

	t.Run("oversized file is not copied", func(t *testing.T) {
		t.Parallel()
		src := filepath.Join(t.TempDir(), "data.db")
		require.NoError(t, os.WriteFile(src, []byte("0123456789"), 0o644))
		dst := filepath.Join(t.TempDir(), "nested", "data.db")
		err := stageSQLiteFile(src, dst, &liveCheckCopyBudget{max: 4})
		require.Error(t, err)
		require.Contains(t, err.Error(), "store copy exceeds")
		unchanged, readErr := os.ReadFile(src)
		require.NoError(t, readErr)
		require.Equal(t, "0123456789", string(unchanged))
		_, statErr := os.Stat(dst)
		require.ErrorIs(t, statErr, os.ErrNotExist)
		_, statErr = os.Stat(filepath.Dir(dst))
		require.ErrorIs(t, statErr, os.ErrNotExist)
	})

	t.Run("prepare refuses an oversized store before a partial sample exists", func(t *testing.T) {
		t.Parallel()
		cliDir := t.TempDir()
		db := filepath.Join(cliDir, "data.db")
		require.NoError(t, os.WriteFile(db, []byte("0123456789"), 0o644))
		sample, err := prepareLiveCheckSampleBudget(cliDir, []string{"list"}, 4)
		require.Error(t, err)
		require.Nil(t, sample)
		require.Contains(t, err.Error(), "isolating sample store")
		require.Contains(t, err.Error(), "store copy exceeds")
		body, readErr := os.ReadFile(db)
		require.NoError(t, readErr)
		require.Equal(t, "0123456789", string(body))
	})
}

func TestStageSQLiteFileSnapshot(t *testing.T) {
	t.Parallel()

	t.Run("snapshot keeps the wal index it opened", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		src := filepath.Join(dir, "data.db")
		seedLiveCheckSQLite(t, src, "seed")
		require.NoError(t, os.RemoveAll(src+"-wal"))
		require.NoError(t, os.RemoveAll(src+"-shm"))
		dbBefore := readLiveCheckFile(t, src)

		dst := filepath.Join(t.TempDir(), "copy.db")
		require.NoError(t, stageSQLiteFile(src, dst, nil))
		require.Equal(t, dbBefore, readLiveCheckFile(t, src))
		_, err := os.Stat(src + "-wal")
		require.NoError(t, err)
		_, err = os.Stat(src + "-shm")
		require.NoError(t, err)
		require.Equal(t, []string{"seed"}, liveCheckSQLiteValues(t, dst))
		_, err = os.Stat(dst + "-wal")
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("snapshot while a writer checkpoints sees the committed generation", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		src := filepath.Join(dir, "data.db")
		db := openLiveCheckSQLite(t, src)
		_, err := db.Exec(`CREATE TABLE sample_rows (v TEXT)`)
		require.NoError(t, err)
		_, err = db.Exec(`INSERT INTO sample_rows (v) VALUES ('seed')`)
		require.NoError(t, err)
		_, err = db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
		require.NoError(t, err)

		tx, err := db.Begin()
		require.NoError(t, err)
		_, err = tx.Exec(`INSERT INTO sample_rows (v) VALUES ('later')`)
		require.NoError(t, err)
		uncommitted := filepath.Join(t.TempDir(), "uncommitted.db")
		require.NoError(t, stageSQLiteFile(src, uncommitted, nil))
		require.Equal(t, []string{"seed"}, liveCheckSQLiteValues(t, uncommitted))
		require.NoError(t, tx.Commit())

		dbBefore := readLiveCheckFile(t, src)
		walBefore := readLiveCheckFile(t, src+"-wal")
		require.NotEmpty(t, walBefore)
		dst := filepath.Join(t.TempDir(), "copy.db")
		require.NoError(t, stageSQLiteFile(src, dst, nil))
		require.Equal(t, []string{"later", "seed"}, liveCheckSQLiteValues(t, dst))
		_, err = os.Stat(dst + "-wal")
		require.ErrorIs(t, err, os.ErrNotExist)
		require.Equal(t, dbBefore, readLiveCheckFile(t, src))
		require.Equal(t, walBefore, readLiveCheckFile(t, src+"-wal"))
		require.Equal(t, []string{"later", "seed"}, liveCheckSQLiteValuesFollowingWAL(t, src))
	})

	t.Run("header that is not a database is not left behind", func(t *testing.T) {
		t.Parallel()
		src := filepath.Join(t.TempDir(), "data.db")
		body := []byte(sqliteDatabaseHeader + "not a database page")
		require.NoError(t, os.WriteFile(src, body, 0o644))
		dst := filepath.Join(t.TempDir(), "nested", "copy.db")
		err := stageSQLiteFile(src, dst, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "snapshot sqlite database")
		unchanged, readErr := os.ReadFile(src)
		require.NoError(t, readErr)
		require.Equal(t, body, unchanged)
		_, statErr := os.Stat(src + "-wal")
		require.ErrorIs(t, statErr, os.ErrNotExist)
		_, statErr = os.Stat(src + "-shm")
		require.ErrorIs(t, statErr, os.ErrNotExist)
		_, statErr = os.Stat(dst)
		require.ErrorIs(t, statErr, os.ErrNotExist)
		_, statErr = os.Stat(dst + "-wal")
		require.ErrorIs(t, statErr, os.ErrNotExist)
	})

	t.Run("non-regular sidecar of a real database is left in place", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		src := filepath.Join(dir, "data.db")
		seedLiveCheckSQLite(t, src, "seed")
		require.NoError(t, os.RemoveAll(src+"-wal"))
		require.NoError(t, os.MkdirAll(src+"-wal", 0o755))
		dst := filepath.Join(t.TempDir(), "copy.db")
		require.NoError(t, stageSQLiteFile(src, dst, nil))
		require.Equal(t, []string{"seed"}, liveCheckSQLiteValues(t, dst))
		info, err := os.Stat(src + "-wal")
		require.NoError(t, err)
		require.True(t, info.IsDir())
		_, err = os.Stat(dst + "-wal")
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("hash in the directory is not read as a uri fragment", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		decoy := filepath.Join(root, "client")
		seedLiveCheckSQLite(t, decoy, "fragment")
		realDir := filepath.Join(root, "client#1")
		require.NoError(t, os.MkdirAll(realDir, 0o755))
		src := filepath.Join(realDir, "data.db")
		seedLiveCheckSQLite(t, src, "hash-name")
		before := snapshotDir(t, root)

		dst := filepath.Join(t.TempDir(), "copy.db")
		require.NoError(t, stageSQLiteFile(src, dst, nil))
		require.Equal(t, []string{"hash-name"}, liveCheckSQLiteValues(t, dst))
		requireSQLiteFamilyUnchanged(t, before, snapshotDir(t, root))
	})

	t.Run("percent-encoded filename is not decoded to a different file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		decoy := filepath.Join(dir, "data..db")
		seedLiveCheckSQLite(t, decoy, "decoded-name")
		src := filepath.Join(dir, "data%2E.db")
		seedLiveCheckSQLite(t, src, "encoded-name")
		before := snapshotDir(t, dir)

		dst := filepath.Join(t.TempDir(), "copy.db")
		require.NoError(t, stageSQLiteFile(src, dst, nil))
		require.Equal(t, []string{"encoded-name"}, liveCheckSQLiteValues(t, dst))
		requireSQLiteFamilyUnchanged(t, before, snapshotDir(t, dir))
	})

	t.Run("space and apostrophe in the path copy that file", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		dir := filepath.Join(root, "my data", "o'brien")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		src := filepath.Join(dir, "a&b.db")
		seedLiveCheckSQLite(t, src, "literal")
		before := snapshotDir(t, root)

		dst := filepath.Join(t.TempDir(), "copy.db")
		require.NoError(t, stageSQLiteFile(src, dst, nil))
		require.Equal(t, []string{"literal"}, liveCheckSQLiteValues(t, dst))
		requireSQLiteFamilyUnchanged(t, before, snapshotDir(t, root))
	})

	t.Run("sqlite snapshot is refused when the budget is too small", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		src := filepath.Join(dir, "data.db")
		seedLiveCheckSQLite(t, src, "seed")
		before := snapshotDir(t, dir)
		dst := filepath.Join(t.TempDir(), "nested", "copy.db")
		err := stageSQLiteFile(src, dst, &liveCheckCopyBudget{max: 4})
		require.Error(t, err)
		require.Contains(t, err.Error(), "store copy exceeds")
		require.Equal(t, before, snapshotDir(t, dir))
		_, statErr := os.Stat(dst)
		require.ErrorIs(t, statErr, os.ErrNotExist)
	})
}

func TestStageSQLiteSnapshotConsistency(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"delete", "truncate"} {
		t.Run("rollback "+mode+" waits for the committing writer", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			src := filepath.Join(dir, "data.db")
			db := openLiveCheckJournal(t, src, mode)
			mustExecLiveCheck(t, db, `CREATE TABLE sample_rows (v TEXT)`)
			mustExecLiveCheck(t, db, `INSERT INTO sample_rows (v) VALUES ('old')`)
			mustExecLiveCheck(t, db, `BEGIN EXCLUSIVE`)
			mustExecLiveCheck(t, db, `UPDATE sample_rows SET v = 'new'`)
			mustExecLiveCheck(t, db, `INSERT INTO sample_rows (v) VALUES ('newer')`)

			dst := filepath.Join(t.TempDir(), "copy.db")
			done := make(chan error, 1)
			go func() {
				done <- stageSQLiteFile(src, dst, nil)
			}()
			select {
			case err := <-done:
				t.Fatalf("snapshot finished while the writer held the database: %v", err)
			case <-time.After(200 * time.Millisecond):
			}
			mustExecLiveCheck(t, db, `COMMIT`)
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(10 * time.Second):
				t.Fatal("snapshot did not finish after the writer committed")
			}
			require.Equal(t, []string{"new", "newer"}, liveCheckSQLiteValues(t, dst))
		})
	}

	for _, state := range []string{"absent", "empty", "frames"} {
		t.Run("wal "+state+" stays one generation while a writer checkpoints", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			src := filepath.Join(dir, "data.db")
			db := seedLiveCheckJournal(t, src, "wal", "seed")
			prepareLiveCheckWALState(t, src, db, state)
			if state == "frames" {
				mustExecLiveCheck(t, db, `INSERT INTO sample_rows (v) VALUES ('frame')`)
				info, err := os.Stat(src + "-wal")
				require.NoError(t, err)
				require.Greater(t, info.Size(), int64(0))
			} else {
				db = openLiveCheckJournal(t, src, "wal")
				mustExecLiveCheck(t, db, `PRAGMA wal_autocheckpoint = 0`)
			}

			var committed atomic.Int64
			stop := make(chan struct{})
			var stopOnce sync.Once
			stopWriter := func() { stopOnce.Do(func() { close(stop) }) }
			writerErr := make(chan error, 1)
			finished := make(chan struct{})
			go func() {
				writerErr <- writeLiveCheckSnapshotRows(db, stop, &committed)
				close(finished)
			}()
			defer func() {
				stopWriter()
				<-finished
			}()
			waitForLiveCheckCommits(t, &committed, 1)

			dst := filepath.Join(t.TempDir(), "copy.db")
			require.NoError(t, stageSQLiteFile(src, dst, nil))
			stopWriter()
			require.NoError(t, <-writerErr)

			got := liveCheckSQLiteValues(t, dst)
			require.Contains(t, got, "seed")
			if state == "frames" {
				require.Contains(t, got, "frame")
			}
			requireCommittedPrefix(t, got, int(committed.Load()))
			require.GreaterOrEqual(t, committed.Load(), int64(1))
		})
	}

	t.Run("quiescent snapshot does not rewrite database or wal bytes", func(t *testing.T) {
		t.Parallel()

		t.Run("rollback", func(t *testing.T) {
			t.Parallel()
			for _, mode := range []string{"delete", "truncate"} {
				dir := t.TempDir()
				src := filepath.Join(dir, "data.db")
				db := seedLiveCheckJournal(t, src, mode, "seed")
				require.NoError(t, db.Close())
				before := snapshotDir(t, dir)
				dst := filepath.Join(t.TempDir(), "copy.db")
				require.NoError(t, stageSQLiteFile(src, dst, nil))
				require.Equal(t, before, snapshotDir(t, dir))
				require.Equal(t, []string{"seed"}, liveCheckSQLiteValues(t, dst))
			}
		})

		t.Run("wal", func(t *testing.T) {
			t.Parallel()
			for _, state := range []string{"absent", "empty", "frames"} {
				dir := t.TempDir()
				src := filepath.Join(dir, "data.db")
				db := seedLiveCheckJournal(t, src, "wal", "seed")
				prepareLiveCheckWALState(t, src, db, state)
				if state == "frames" {
					mustExecLiveCheck(t, db, `INSERT INTO sample_rows (v) VALUES ('frame')`)
				}
				before := snapshotDir(t, dir)
				dst := filepath.Join(t.TempDir(), "copy.db")
				require.NoError(t, stageSQLiteFile(src, dst, nil))
				requireSQLiteFamilyUnchanged(t, before, snapshotDir(t, dir))
				_, err := os.Stat(src + "-wal")
				require.NoError(t, err)
				_, err = os.Stat(src + "-shm")
				require.NoError(t, err)
				got := liveCheckSQLiteValues(t, dst)
				require.Contains(t, got, "seed")
				if state == "frames" {
					require.Equal(t, []string{"frame", "seed"}, got)
				}
			}
		})
	})

	t.Run("read-only directory still snapshots", func(t *testing.T) {
		t.Parallel()

		t.Run("rollback", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			src := filepath.Join(dir, "data.db")
			db := seedLiveCheckJournal(t, src, "delete", "seed")
			require.NoError(t, db.Close())
			before := snapshotDir(t, dir)
			lockLiveCheckDirReadOnly(t, dir)
			dst := filepath.Join(t.TempDir(), "copy.db")
			require.NoError(t, stageSQLiteFile(src, dst, nil))
			require.Equal(t, []string{"seed"}, liveCheckSQLiteValues(t, dst))
			restoreLiveCheckDir(t, dir)
			require.Equal(t, before, snapshotDir(t, dir))
		})

		t.Run("settled wal", func(t *testing.T) {
			t.Parallel()
			for _, state := range []string{"absent", "empty"} {
				dir := t.TempDir()
				src := filepath.Join(dir, "data.db")
				db := seedLiveCheckJournal(t, src, "wal", "seed")
				prepareLiveCheckWALState(t, src, db, state)
				before := snapshotDir(t, dir)
				lockLiveCheckDirReadOnly(t, dir)
				raw, err := sql.Open("sqlite", sqliteSnapshotDSN(src))
				require.NoError(t, err)
				pingErr := raw.Ping()
				_ = raw.Close()
				require.Error(t, pingErr)
				require.True(t, snapshotOpenNeedsImmutableFallback(pingErr), pingErr)
				dst := filepath.Join(t.TempDir(), "copy.db")
				require.NoError(t, stageSQLiteFile(src, dst, nil))
				require.Equal(t, []string{"seed"}, liveCheckSQLiteValues(t, dst))
				restoreLiveCheckDir(t, dir)
				require.Equal(t, before, snapshotDir(t, dir))
			}
		})
	})

	t.Run("wal frames are not snapshotted when the index cannot be created", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		src := filepath.Join(dir, "data.db")
		db := seedLiveCheckJournal(t, src, "wal", "seed")
		mustExecLiveCheck(t, db, `PRAGMA wal_autocheckpoint = 0`)
		mustExecLiveCheck(t, db, `INSERT INTO sample_rows (v) VALUES ('frame')`)
		dbBytes := readLiveCheckFile(t, src)
		walBytes := readLiveCheckFile(t, src+"-wal")
		require.NotEmpty(t, walBytes)
		require.NoError(t, db.Close())
		// Closing the last connection checkpoints the wal away. Put the
		// pre-close bytes back so the frames exist with no process holding -shm.
		require.NoError(t, os.WriteFile(src, dbBytes, 0o644))
		require.NoError(t, os.WriteFile(src+"-wal", walBytes, 0o644))
		require.NoError(t, os.RemoveAll(src+"-shm"))
		lockLiveCheckDirReadOnly(t, dir)

		dst := filepath.Join(t.TempDir(), "nested", "copy.db")
		err := stageSQLiteFile(src, dst, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "snapshot sqlite database")
		_, statErr := os.Stat(dst)
		require.ErrorIs(t, statErr, os.ErrNotExist)
		restoreLiveCheckDir(t, dir)
		require.Equal(t, dbBytes, readLiveCheckFile(t, src))
		require.Equal(t, walBytes, readLiveCheckFile(t, src+"-wal"))
	})

	t.Run("immutable fallback does not hide a corrupt read", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		src := filepath.Join(dir, "data.db")
		db := seedLiveCheckJournal(t, src, "wal", "seed")
		prepareLiveCheckWALState(t, src, db, "absent")
		require.NoError(t, db.Close())
		body := readLiveCheckFile(t, src)
		require.Greater(t, len(body), 100)
		for i := 100; i < len(body); i++ {
			body[i] = 0
		}
		require.NoError(t, os.WriteFile(src, body, 0o644))
		lockLiveCheckDirReadOnly(t, dir)

		dst := filepath.Join(t.TempDir(), "copy.db")
		err := stageSQLiteFile(src, dst, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "snapshot sqlite database")
		msg := strings.ToLower(err.Error())
		require.True(t, strings.Contains(msg, "malformed") || strings.Contains(msg, "not a database"), err)
		_, statErr := os.Stat(dst)
		require.ErrorIs(t, statErr, os.ErrNotExist)
		restoreLiveCheckDir(t, dir)
		require.Equal(t, body, readLiveCheckFile(t, src))
	})

	t.Run("busy error is not an immutable fallback", func(t *testing.T) {
		t.Parallel()
		require.False(t, snapshotOpenNeedsImmutableFallback(nil))
		require.False(t, snapshotOpenNeedsImmutableFallback(errors.New("database is locked (5) (SQLITE_BUSY)")))
		require.False(t, snapshotOpenNeedsImmutableFallback(errors.New("file is not a database")))
		require.False(t, snapshotOpenNeedsImmutableFallback(errors.New("database disk image is malformed")))
		require.True(t, snapshotOpenNeedsImmutableFallback(errors.New("unable to open database file (14)")))
		require.True(t, snapshotOpenNeedsImmutableFallback(errors.New("attempt to write a readonly database (1544)")))
		require.False(t, snapshotWALHasNoFrames(filepath.Join(t.TempDir(), "missing.db")))

		dir := t.TempDir()
		src := filepath.Join(dir, "data.db")
		db := openLiveCheckJournal(t, src, "delete")
		mustExecLiveCheck(t, db, `CREATE TABLE sample_rows (v TEXT)`)
		mustExecLiveCheck(t, db, `INSERT INTO sample_rows (v) VALUES ('old')`)
		mustExecLiveCheck(t, db, `BEGIN EXCLUSIVE`)
		t.Cleanup(func() { _, _ = db.Exec(`ROLLBACK`) })

		ro, err := sql.Open("sqlite", sqliteFileURI(src)+"?mode=ro&_pragma=busy_timeout(50)&_pragma=mmap_size(0)")
		require.NoError(t, err)
		t.Cleanup(func() { _ = ro.Close() })
		pingErr := ro.Ping()
		require.Error(t, pingErr)
		require.True(t, snapshotErrorIsBusy(pingErr), pingErr)
		require.False(t, snapshotOpenNeedsImmutableFallback(pingErr), pingErr)

		wal := seedLiveCheckJournal(t, filepath.Join(dir, "frames.db"), "wal", "seed")
		mustExecLiveCheck(t, wal, `INSERT INTO sample_rows (v) VALUES ('frame')`)
		require.False(t, snapshotWALHasNoFrames(filepath.Join(dir, "frames.db")))
		prepareLiveCheckWALState(t, filepath.Join(dir, "empty.db"), seedLiveCheckJournal(t, filepath.Join(dir, "empty.db"), "wal", "seed"), "empty")
		require.True(t, snapshotWALHasNoFrames(filepath.Join(dir, "empty.db")))
		require.False(t, snapshotWALHasNoFrames(src))
	})
}

func TestLiveCheckDirModeBlocksCreates(t *testing.T) {
	t.Parallel()
	require.False(t, liveCheckDirModeBlocksCreates("windows"))
	require.True(t, liveCheckDirModeBlocksCreates("linux"))
	require.True(t, liveCheckDirModeBlocksCreates("darwin"))
}

func TestLiveCheckDirCreateBlocked(t *testing.T) {
	t.Parallel()

	t.Run("writable directory is not blocked and leaves no probe", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.False(t, liveCheckDirCreateBlocked(dir))
		_, err := os.Stat(filepath.Join(dir, "write-probe"))
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("mode 0555 agrees with a direct create", func(t *testing.T) {
		t.Parallel()
		if !liveCheckDirModeBlocksCreates(runtime.GOOS) {
			t.Skip("directory mode does not block file creation on Windows")
		}
		dir := t.TempDir()
		require.NoError(t, os.Chmod(dir, 0o555))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
		probe := filepath.Join(dir, "direct-probe")
		err := os.WriteFile(probe, []byte("x"), 0o644)
		if err == nil {
			require.NoError(t, os.Remove(probe))
		}
		require.Equal(t, err != nil, liveCheckDirCreateBlocked(dir))
	})
}

func openLiveCheckJournal(t *testing.T, path, mode string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(1000)&_pragma=journal_mode("+mode+")")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.Ping())
	return db
}

func seedLiveCheckJournal(t *testing.T, path, mode, value string) *sql.DB {
	t.Helper()
	db := openLiveCheckJournal(t, path, mode)
	mustExecLiveCheck(t, db, `CREATE TABLE sample_rows (v TEXT)`)
	_, err := db.Exec(`INSERT INTO sample_rows (v) VALUES (?)`, value)
	require.NoError(t, err)
	if mode == "wal" {
		mustExecLiveCheck(t, db, `PRAGMA wal_checkpoint(TRUNCATE)`)
		mustExecLiveCheck(t, db, `PRAGMA wal_autocheckpoint = 0`)
	}
	return db
}

func mustExecLiveCheck(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	_, err := db.Exec(query)
	require.NoError(t, err)
}

// prepareLiveCheckWALState leaves a checkpointed database whose wal matches
// state. The connection stays open for "frames" so the next insert is not
// checkpointed on close; "absent" and "empty" close it first.
func prepareLiveCheckWALState(t *testing.T, src string, db *sql.DB, state string) {
	t.Helper()
	switch state {
	case "absent":
		require.NoError(t, db.Close())
		require.NoError(t, os.RemoveAll(src+"-wal"))
		require.NoError(t, os.RemoveAll(src+"-shm"))
	case "empty":
		require.NoError(t, db.Close())
		require.NoError(t, os.RemoveAll(src+"-shm"))
		require.NoError(t, os.WriteFile(src+"-wal", nil, 0o644))
	case "frames":
	default:
		t.Fatalf("unknown wal state %q", state)
	}
}

func writeLiveCheckSnapshotRows(db *sql.DB, stop <-chan struct{}, committed *atomic.Int64) error {
	for {
		select {
		case <-stop:
			return nil
		default:
		}
		n := int(committed.Load()) + 1
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO sample_rows (v) VALUES (?)`, "new-"+strconv.Itoa(n)); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if _, err := db.Exec(`PRAGMA wal_checkpoint(PASSIVE)`); err != nil && !snapshotErrorIsBusy(err) {
			return err
		}
		if n%4 == 0 {
			if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil && !snapshotErrorIsBusy(err) {
				return err
			}
		}
		committed.Store(int64(n))
	}
}

func waitForLiveCheckCommits(t *testing.T, committed *atomic.Int64, n int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for committed.Load() < n && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	require.GreaterOrEqual(t, committed.Load(), n)
}

func requireCommittedPrefix(t *testing.T, values []string, committed int) {
	t.Helper()
	var got []int
	for _, value := range values {
		id, ok := strings.CutPrefix(value, "new-")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(id)
		require.NoError(t, err)
		got = append(got, n)
	}
	slices.Sort(got)
	for i, n := range got {
		require.Equal(t, i+1, n)
		require.LessOrEqual(t, n, committed)
	}
}

func lockLiveCheckDirReadOnly(t *testing.T, dir string) {
	t.Helper()
	// Windows ignores directory mode bits, so a failed create probe there does
	// not mean SQLite cannot create the wal index. A privileged Unix process
	// can also ignore the bits, so the tests run only after a create fails.
	if !liveCheckDirModeBlocksCreates(runtime.GOOS) {
		t.Skip("directory mode does not block file creation on Windows")
	}
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if liveCheckDirCreateBlocked(dir) {
		return
	}
	require.NoError(t, os.Chmod(dir, 0o755))
	t.Skip("directory mode does not block file creation")
}

func liveCheckDirModeBlocksCreates(goos string) bool {
	return goos != "windows"
}

func liveCheckDirCreateBlocked(dir string) bool {
	probe := filepath.Join(dir, "write-probe")
	if err := os.WriteFile(probe, []byte("x"), 0o644); err == nil {
		_ = os.Remove(probe)
		return false
	}
	return true
}

func restoreLiveCheckDir(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.Chmod(dir, 0o755))
}

func requireSQLiteFamilyUnchanged(t *testing.T, before, after map[string]string) {
	t.Helper()
	for name, body := range before {
		if strings.HasSuffix(name, "-shm") {
			continue
		}
		got, ok := after[name]
		require.True(t, ok, "staging removed %s", name)
		require.Equal(t, body, got, name)
	}
	for name, body := range after {
		if _, ok := before[name]; ok || strings.HasSuffix(name, "-shm") {
			continue
		}
		if strings.HasSuffix(name, "-wal") {
			require.Empty(t, body, "staging created a non-empty wal %s", name)
			continue
		}
		t.Fatalf("staging created %s", name)
	}
}

func seedLiveCheckSQLite(t *testing.T, path, value string) {
	t.Helper()
	db := openLiveCheckSQLite(t, path)
	_, err := db.Exec(`CREATE TABLE sample_rows (v TEXT)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO sample_rows (v) VALUES (?)`, value)
	require.NoError(t, err)
	_, err = db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())
}

func openLiveCheckSQLite(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func readLiveCheckFile(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return body
}

func liveCheckSQLiteValues(t *testing.T, path string) []string {
	t.Helper()
	// immutable keeps this helper from creating a wal index beside the snapshot.
	return queryLiveCheckSQLite(t, sqliteFileURI(path)+"?mode=ro&immutable=1&_pragma=mmap_size(0)")
}

func liveCheckSQLiteValuesFollowingWAL(t *testing.T, path string) []string {
	t.Helper()
	return queryLiveCheckSQLite(t, sqliteFileURI(path)+"?mode=ro&_pragma=busy_timeout(5000)&_pragma=mmap_size(0)")
}

func queryLiveCheckSQLite(t *testing.T, dsn string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	var integrity string
	require.NoError(t, db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity))
	require.Equal(t, "ok", integrity)
	rows, err := db.Query(`SELECT v FROM sample_rows ORDER BY v`)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var value string
		require.NoError(t, rows.Scan(&value))
		out = append(out, value)
	}
	require.NoError(t, rows.Err())
	return out
}
