package generator

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/platform"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedDryRunSideEffects(t *testing.T) {
	hits := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"1","name":"ada"}]`))
	}))
	t.Cleanup(server.Close)

	withSearch := searchCLISpec("sidefx", server.URL, map[string]spec.Resource{
		"items": searchCLIEndpoint("GET", "/search", []spec.Param{
			{Name: "q", Type: "string", In: "query"},
		}, nil),
	}, searchCLITypes())
	binary := buildSideEffectCLI(t, withSearch)
	prefix := naming.EnvPrefix(withSearch.Name)
	_, env := sideEffectEnv(t, prefix, server.URL)

	t.Run("export dry-run does not truncate", func(t *testing.T) {
		for _, flag := range []string{"--output", "--output=", "-o"} {
			out := filepath.Join(t.TempDir(), "export.jsonl")
			sentinel := []byte("keep-me")
			require.NoError(t, os.WriteFile(out, sentinel, 0o640))
			before := hits.Load()
			args := []string{"export", "records", "--dry-run", "--json"}
			switch flag {
			case "--output":
				args = append(args, "--output", out)
			case "--output=":
				args = append(args, "--output="+out)
			case "-o":
				args = append(args, "-o", out)
			}
			code, stdout, stderr := runSideEffectCLI(t, binary, env, args...)
			require.Equal(t, 0, code, "flag %s\nstdout:\n%s\nstderr:\n%s", flag, stdout, stderr)
			assertDryRunEnvelope(t, stdout, "export records")
			require.Contains(t, stderr, "/records")
			require.Contains(t, stderr, "dry run - no request sent")
			got, err := os.ReadFile(out)
			require.NoError(t, err)
			require.Equal(t, sentinel, got)
			require.Equal(t, before, hits.Load())
			if runtime.GOOS != "windows" {
				info, err := os.Stat(out)
				require.NoError(t, err)
				require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
			}
		}

		missingDir := filepath.Join(t.TempDir(), "missing-out")
		missing := filepath.Join(missingDir, "export.jsonl")
		code, stdout, stderr := runSideEffectCLI(t, binary, env, "export", "records", "--dry-run", "--json", "--output", missing)
		require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		requireAbsent(t, missingDir)

		home := filepath.Join(t.TempDir(), "absent-home")
		nope := filepath.Join(t.TempDir(), "nope.jsonl")
		code, stdout, stderr = runSideEffectCLI(t, binary, env, "--home", home, "export", "nope", "--output", nope, "--dry-run")
		require.NotEqual(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		require.Contains(t, stdout+stderr, "unknown resource")
		requireAbsent(t, home)
		require.NoFileExists(t, nope)

		missingArg := filepath.Join(t.TempDir(), "missing-arg.jsonl")
		code, _, stderr = runSideEffectCLI(t, binary, env, "export", "--output", missingArg, "--dry-run")
		require.NotEqual(t, 0, code, stderr)
		require.NoFileExists(t, missingArg)
	})

	t.Run("export writes a private file", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "export.jsonl")
		require.NoError(t, os.WriteFile(out, []byte("old"), 0o644))
		before := hits.Load()
		code, stdout, stderr := runSideEffectCLI(t, binary, env, "export", "records", "--output", out)
		require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		got, err := os.ReadFile(out)
		require.NoError(t, err)
		require.Contains(t, string(got), "ada")
		require.NotEqual(t, "old", string(got))
		require.Equal(t, before+1, hits.Load())
		if runtime.GOOS != "windows" {
			info, err := os.Stat(out)
			require.NoError(t, err)
			require.Zero(t, info.Mode().Perm()&0o077)
		}
	})

	t.Run("search dry-run does not open the store", func(t *testing.T) {
		db := filepath.Join(t.TempDir(), "no-db-dir", "search.db")
		home := filepath.Join(t.TempDir(), "absent-home")
		before := hits.Load()
		code, stdout, stderr := runSideEffectCLI(t, binary, env, "--home", home, "search", "ada", "--dry-run", "--json", "--db", db)
		require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		require.Contains(t, stderr, "/search")
		require.Contains(t, stderr, "dry run - no request sent")
		require.Equal(t, before, hits.Load())
		requireAbsent(t, filepath.Dir(db))
		requireAbsent(t, home)

		db = filepath.Join(t.TempDir(), "local-dir", "search.db")
		code, stdout, stderr = runSideEffectCLI(t, binary, env, "search", "ada", "--data-source", "local", "--dry-run", "--json", "--db", db)
		require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assertDryRunEnvelope(t, stdout, "search")
		require.Equal(t, before, hits.Load())
		requireAbsent(t, filepath.Dir(db))
	})

	t.Run("search negative limit", func(t *testing.T) {
		for _, args := range [][]string{
			{"search", "ada", "--limit=-1", "--json"},
			{"search", "ada", "--limit=-1", "--dry-run", "--data-source", "live"},
			{"search", "ada", "--limit=-1", "--data-source", "local"},
		} {
			db := filepath.Join(t.TempDir(), "neg.db")
			before := hits.Load()
			code, stdout, stderr := runSideEffectCLI(t, binary, env, append(args, "--db", db)...)
			require.Equal(t, 2, code, "args %v\nstdout:\n%s\nstderr:\n%s", args, stdout, stderr)
			require.Contains(t, stdout+stderr, "--limit must be 0 or greater, got -1")
			require.NotContains(t, stdout+stderr, "panic")
			require.Equal(t, before, hits.Load())
			require.NoFileExists(t, db)
		}
	})

	t.Run("search limit zero and one", func(t *testing.T) {
		for _, limit := range []string{"0", "1"} {
			db := filepath.Join(t.TempDir(), "search-"+limit+".db")
			code, stdout, stderr := runSideEffectCLI(t, binary, env, "search", "ada", "--data-source", "local", "--limit", limit, "--json", "--db", db)
			require.Equal(t, 0, code, "limit %s\nstdout:\n%s\nstderr:\n%s", limit, stdout, stderr)
			require.FileExists(t, db)
		}
	})

	t.Run("sync dry-run validates resource params first", func(t *testing.T) {
		dbDir := filepath.Join(t.TempDir(), "sync-missing")
		db := filepath.Join(dbDir, "sync.db")
		home := filepath.Join(t.TempDir(), "sync-home")
		before := hits.Load()
		code, stdout, stderr := runSideEffectCLI(t, binary, env, "--home", home, "sync", "--dry-run", "--json", "--db", db)
		require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assertDryRunEnvelope(t, stdout, "sync (resource eligibility and checkpoints not checked)")
		require.Equal(t, before, hits.Load())
		requireAbsent(t, dbDir)
		requireAbsent(t, home)

		for _, dry := range []bool{false, true} {
			dbDir = filepath.Join(t.TempDir(), "bad-param")
			db = filepath.Join(dbDir, "sync.db")
			home = filepath.Join(t.TempDir(), "bad-home")
			args := []string{"--home", home, "sync", "--resource-param", "bogus:limit=1", "--db", db}
			if dry {
				args = append(args, "--dry-run", "--json")
			}
			before = hits.Load()
			code, stdout, stderr = runSideEffectCLI(t, binary, env, args...)
			require.Equal(t, 2, code, "dry %v\nstdout:\n%s\nstderr:\n%s", dry, stdout, stderr)
			require.Contains(t, stdout+stderr, "unknown resource")
			require.Equal(t, before, hits.Load())
			requireAbsent(t, dbDir)
			requireAbsent(t, home)
		}

		dbDir = filepath.Join(t.TempDir(), "good-param")
		db = filepath.Join(dbDir, "sync.db")
		code, stdout, stderr = runSideEffectCLI(t, binary, env, "sync", "--resource-param", "records:limit=1", "--dry-run", "--json", "--db", db)
		require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assertDryRunEnvelope(t, stdout, "sync (resource eligibility and checkpoints not checked)")
		requireAbsent(t, dbDir)

		dbDir = filepath.Join(t.TempDir(), "bad-since")
		db = filepath.Join(dbDir, "sync.db")
		before = hits.Load()
		code, stdout, stderr = runSideEffectCLI(t, binary, env, "sync", "--since", "nope", "--db", db)
		require.NotEqual(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		require.Contains(t, stdout+stderr, "--since")
		require.Equal(t, before, hits.Load())
		requireAbsent(t, dbDir)
	})

	t.Run("sync writes the store", func(t *testing.T) {
		db := filepath.Join(t.TempDir(), "sync.db")
		before := hits.Load()
		code, stdout, stderr := runSideEffectCLI(t, binary, env, "sync", "--db", db, "--json")
		require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		require.FileExists(t, db)
		require.Greater(t, hits.Load(), before)
	})

	t.Run("profile reads do not create the config dir", func(t *testing.T) {
		home := filepath.Join(t.TempDir(), "profile-home")
		code, stdout, stderr := runSideEffectCLI(t, binary, env, "--home", home, "profile", "show", "ghost")
		require.NotEqual(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		require.Contains(t, stdout+stderr, "not found")
		requireAbsent(t, home)

		code, stdout, stderr = runSideEffectCLI(t, binary, env, "--home", home, "--profile", "ghost", "version")
		require.NotEqual(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		require.Contains(t, stdout+stderr, "not found")
		requireAbsent(t, home)
	})

	t.Run("profile save and delete dry-run", func(t *testing.T) {
		home := filepath.Join(t.TempDir(), "save-home")
		code, stdout, stderr := runSideEffectCLI(t, binary, env, "--home", home, "profile", "save", "night", "--json", "--dry-run")
		require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assertDryRunEnvelope(t, stdout, "save profile night")
		requireAbsent(t, home)

		code, stdout, stderr = runSideEffectCLI(t, binary, env, "--home", home, "profile", "save", "night", "--dry-run")
		require.NotEqual(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		require.Contains(t, stdout+stderr, "no non-default flags")
		requireAbsent(t, home)

		code, stdout, stderr = runSideEffectCLI(t, binary, env, "--home", home, "profile", "delete", "night", "--dry-run", "--json")
		require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		assertDryRunEnvelope(t, stdout, "delete profile night (existence not checked)")
		requireAbsent(t, home)

		realHome := t.TempDir()
		code, stdout, stderr = runSideEffectCLI(t, binary, env, "--home", realHome, "profile", "save", "night", "--json", "--dry-run=false")
		require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		stored := filepath.Join(realHome, "config", "profiles.json")
		body, err := os.ReadFile(stored)
		require.NoError(t, err)
		require.NotContains(t, string(body), "dry-run")
		require.Contains(t, string(body), `"json"`)

		code, stdout, stderr = runSideEffectCLI(t, binary, env, "--home", realHome, "profile", "delete", "night")
		require.NotEqual(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		body, err = os.ReadFile(stored)
		require.NoError(t, err)
		require.Contains(t, string(body), "night")

		code, stdout, stderr = runSideEffectCLI(t, binary, env, "--home", realHome, "profile", "delete", "night", "--yes")
		require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		body, err = os.ReadFile(stored)
		require.NoError(t, err)
		require.NotContains(t, string(body), "night")
	})

	without := searchCLISpec("sidefxlocal", server.URL, map[string]spec.Resource{}, searchCLITypes())
	localBin := buildSideEffectCLI(t, without)
	_, localEnv := sideEffectEnv(t, naming.EnvPrefix(without.Name), server.URL)

	t.Run("search without endpoint", func(t *testing.T) {
		for _, extra := range [][]string{nil, {"--dry-run"}} {
			db := filepath.Join(t.TempDir(), "live.db")
			before := hits.Load()
			args := append([]string{"search", "ada", "--data-source", "live", "--db", db}, extra...)
			code, stdout, stderr := runSideEffectCLI(t, localBin, localEnv, args...)
			require.NotEqual(t, 0, code, "extra %v\nstdout:\n%s\nstderr:\n%s", extra, stdout, stderr)
			require.Contains(t, stdout+stderr, "no search endpoint")
			require.Equal(t, before, hits.Load())
			require.NoFileExists(t, db)
		}
		for _, source := range []string{"auto", "local"} {
			db := filepath.Join(t.TempDir(), source+".db")
			before := hits.Load()
			code, stdout, stderr := runSideEffectCLI(t, localBin, localEnv, "search", "ada", "--data-source", source, "--dry-run", "--json", "--db", db)
			require.Equal(t, 0, code, "source %s\nstdout:\n%s\nstderr:\n%s", source, stdout, stderr)
			assertDryRunEnvelope(t, stdout, "search")
			require.Equal(t, before, hits.Load())
			require.NoFileExists(t, db)
		}
		db := filepath.Join(t.TempDir(), "real-local.db")
		code, stdout, stderr := runSideEffectCLI(t, localBin, localEnv, "search", "ada", "--data-source", "local", "--json", "--db", db)
		require.Equal(t, 0, code, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
		require.FileExists(t, db)
	})
}

func buildSideEffectCLI(t *testing.T, apiSpec *spec.APISpec) string {
	t.Helper()
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Export: true, Store: true, Sync: true, Search: true, MCP: true}
	require.NoError(t, gen.Generate())
	requireGeneratedCompiles(t, outputDir)
	binary := platform.ExecutablePath(filepath.Join(outputDir, naming.CLI(apiSpec.Name)))
	runGoCommand(t, outputDir, "build", "-o", binary, "./cmd/"+naming.CLI(apiSpec.Name))
	require.FileExists(t, binary)
	return binary
}

func sideEffectEnv(t *testing.T, prefix, baseURL string) (string, []string) {
	t.Helper()
	home := t.TempDir()
	configHome := filepath.Join(home, ".config")
	dataHome := filepath.Join(home, ".local", "share")
	stateHome := filepath.Join(home, ".local", "state")
	cacheHome := filepath.Join(home, ".cache")
	for _, dir := range []string{configHome, dataHome, stateHome, cacheHome} {
		require.NoError(t, os.MkdirAll(dir, 0o755))
	}
	env := harnessFreeEnv(
		"HOME="+home,
		"USERPROFILE="+home,
		"XDG_CONFIG_HOME="+configHome,
		"XDG_DATA_HOME="+dataHome,
		"XDG_STATE_HOME="+stateHome,
		"XDG_CACHE_HOME="+cacheHome,
		prefix+"_CONFIG=",
		prefix+"_CONFIG_DIR=",
		prefix+"_DATA_DIR=",
		prefix+"_STATE_DIR=",
		prefix+"_CACHE_DIR=",
		prefix+"_HOME=",
		prefix+"_BASE_URL="+baseURL,
	)
	return home, env
}

func runSideEffectCLI(t *testing.T, binary string, env []string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return 0, stdout.String(), stderr.String()
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("args %v: %v\nstdout:\n%s\nstderr:\n%s", args, err, stdout.String(), stderr.String())
	}
	return exitErr.ExitCode(), stdout.String(), stderr.String()
}

func requireAbsent(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist, "path exists: %s", path)
}
