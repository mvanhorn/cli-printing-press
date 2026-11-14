package generator

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/platform"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildDryRunCLI generates and builds a CLI with the export and search
// templates. withSearchEndpoint adds a live search endpoint so the --data-source
// local path is exercised on a CLI that also has a remote search.
func buildDryRunCLI(t *testing.T, baseURL string, withSearchEndpoint bool) string {
	t.Helper()
	resources := map[string]spec.Resource{}
	if withSearchEndpoint {
		resources["contacts"] = searchCLIEndpoint("GET", "/contacts/search", []spec.Param{
			{Name: "filter[query]", Type: "string", In: "query"},
		}, nil)
	}
	apiSpec := searchCLISpec("dryrunfx", baseURL, resources, searchCLITypes())
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

func dryRunHitServer(t *testing.T) (*atomic.Int32, string) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"1","name":"real"}]`))
	}))
	t.Cleanup(server.Close)
	return &hits, server.URL
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func requireDryRunReport(t *testing.T, stdout string) {
	t.Helper()
	var report struct {
		DryRun  bool `json:"dry_run"`
		Success bool `json:"success"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &report), stdout)
	assert.True(t, report.DryRun, stdout)
	assert.True(t, report.Success, stdout)
}

func TestGeneratedExportDryRunDoesNotTouchOutput(t *testing.T) {
	t.Parallel()

	hits, baseURL := dryRunHitServer(t)
	binary := buildDryRunCLI(t, baseURL, false)

	// --output, -o and --output=<path> all take the same path, with and without
	// a pre-existing destination.
	for _, flag := range [][]string{{"--output"}, {"-o"}, {"--output=%s"}} {
		work := t.TempDir()
		sentinel := filepath.Join(work, "export.txt")
		require.NoError(t, os.WriteFile(sentinel, []byte("SENTINEL\n"), 0o644))
		missing := filepath.Join(work, "nested", "deeper", "out.jsonl")
		dirBefore := dirNames(t, work)

		for _, target := range []string{sentinel, missing} {
			args := []string{"export", "records", "--dry-run", "--json"}
			if flag[0] == "--output=%s" {
				args = append(args, "--output="+target)
			} else {
				args = append(args, flag[0], target)
			}
			stdout, stderr, err := runSearchCLI(t, binary, args...)
			require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
			requireDryRunReport(t, stdout)
			assert.Contains(t, stderr, baseURL+"/records", "dry run must still preview the request")
			assert.NotContains(t, stderr, "Exported ")
		}

		got, err := os.ReadFile(sentinel)
		require.NoError(t, err)
		assert.Equal(t, "SENTINEL\n", string(got), "existing export destination must not be overwritten")
		info, err := os.Stat(sentinel)
		require.NoError(t, err)
		if runtime.GOOS != "windows" {
			assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "existing destination mode must not change")
		}
		assert.NoFileExists(t, missing)
		assert.NoDirExists(t, filepath.Join(work, "nested"), "dry run must not create the output directory")
		assert.Equal(t, dirBefore, dirNames(t, work))
	}
	assert.Zero(t, hits.Load(), "dry run must not dial")
}

func TestGeneratedExportDryRunStillValidates(t *testing.T) {
	t.Parallel()

	_, baseURL := dryRunHitServer(t)
	binary := buildDryRunCLI(t, baseURL, false)
	work := t.TempDir()
	out := filepath.Join(work, "out.jsonl")

	_, _, err := runSearchCLI(t, binary, "export", "nope", "--dry-run", "--output", out)
	require.Error(t, err, "unknown resource must still fail under --dry-run")
	assert.NoFileExists(t, out)
	_, _, err = runSearchCLI(t, binary, "export", "--dry-run", "--output", out)
	require.Error(t, err, "missing resource argument must still fail")
	assert.NoFileExists(t, out)
}

func TestGeneratedExportWithoutDryRunStillWritesPrivateFile(t *testing.T) {
	t.Parallel()

	hits, baseURL := dryRunHitServer(t)
	binary := buildDryRunCLI(t, baseURL, false)
	out := filepath.Join(t.TempDir(), "nested", "out.jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(out), 0o755))
	require.NoError(t, os.WriteFile(out, []byte("OLD\n"), 0o644))

	stdout, stderr, err := runSearchCLI(t, binary, "export", "records", "--output", out)
	require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.EqualValues(t, 1, hits.Load())
	got, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"1","name":"real"}`, string(got))
	info, err := os.Stat(out)
	require.NoError(t, err)
	if runtime.GOOS != "windows" {
		assert.Zero(t, info.Mode().Perm()&0o077, "export file must be private")
	}
	assert.Contains(t, stderr, "Exported 1 records")
}

func TestGeneratedExportAndSearchHelpContract(t *testing.T) {
	t.Parallel()

	_, baseURL := dryRunHitServer(t)
	binary := buildDryRunCLI(t, baseURL, false)

	stdout, stderr, err := runSearchCLI(t, binary, "export", "--help")
	require.NoError(t, err, stderr)
	for _, want := range []string{"-o, --output string", "--format string", "--limit int", "--dry-run"} {
		assert.Contains(t, stdout, want)
	}
	stdout, stderr, err = runSearchCLI(t, binary, "search", "--help")
	require.NoError(t, err, stderr)
	for _, want := range []string{"--db string", "--type string", "--limit int", "--dry-run"} {
		assert.Contains(t, stdout, want)
	}
}

func TestGeneratedSearchDryRunDoesNotOpenDatabase(t *testing.T) {
	t.Parallel()

	for _, withEndpoint := range []bool{false, true} {
		hits, baseURL := dryRunHitServer(t)
		binary := buildDryRunCLI(t, baseURL, withEndpoint)
		modes := [][]string{{"--data-source", "local"}}
		if !withEndpoint {
			modes = append(modes, nil) // auto falls to local: no search endpoint
		} else {
			modes = append(modes, nil, []string{"--data-source", "live"})
		}

		for _, mode := range modes {
			base := append([]string{"search", "sentinel", "--dry-run", "--json"}, mode...)

			// Missing DB in an existing dir and in a not-yet-existing nested dir.
			work := t.TempDir()
			for _, db := range []string{filepath.Join(work, "missing.db"), filepath.Join(work, "nested", "deeper", "missing.db")} {
				stdout, stderr, err := runSearchCLI(t, binary, append(append([]string{}, base...), "--db", db)...)
				require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
				assert.NoFileExists(t, db)
				assert.NoFileExists(t, db+"-wal")
				assert.NoFileExists(t, db+"-shm")
			}
			assert.NoDirExists(t, filepath.Join(work, "nested"))
			assert.Empty(t, dirNames(t, work), "dry run must create nothing")

			// Existing DB: bytes, mtime and sibling files stay identical. The DB
			// is created by a real (non-dry-run) local search, which must still work.
			db := filepath.Join(t.TempDir(), "existing.db")
			stdout, stderr, err := runSearchCLI(t, binary, "search", "sentinel", "--data-source", "local", "--json", "--db", db)
			require.NoError(t, err, "non-dry-run search must still work\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
			require.FileExists(t, db)
			before, err := os.ReadFile(db)
			require.NoError(t, err)
			infoBefore, err := os.Stat(db)
			require.NoError(t, err)
			namesBefore := dirNames(t, filepath.Dir(db))

			stdout, stderr, err = runSearchCLI(t, binary, append(append([]string{}, base...), "--db", db)...)
			require.NoError(t, err, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
			after, err := os.ReadFile(db)
			require.NoError(t, err)
			infoAfter, err := os.Stat(db)
			require.NoError(t, err)
			assert.Equal(t, sha256.Sum256(before), sha256.Sum256(after))
			assert.Equal(t, infoBefore.ModTime(), infoAfter.ModTime())
			assert.Equal(t, namesBefore, dirNames(t, filepath.Dir(db)))
		}
		if !withEndpoint {
			// No search endpoint: nothing may dial in any mode.
			assert.Zero(t, hits.Load())
		}
	}
}

func TestGeneratedSearchDryRunReportsPreviewForLocalSearch(t *testing.T) {
	t.Parallel()

	_, baseURL := dryRunHitServer(t)
	binary := buildDryRunCLI(t, baseURL, false)
	db := filepath.Join(t.TempDir(), "x.db")
	stdout, stderr, err := runSearchCLI(t, binary, "search", "sentinel", "--dry-run", "--json", "--db", db)
	require.NoError(t, err, stderr)
	requireDryRunReport(t, stdout)
	stdout, stderr, err = runSearchCLI(t, binary, "search", "sentinel", "--dry-run", "--db", db)
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "dry-run: would")
	assert.NoFileExists(t, db)
}

func TestGeneratedSearchNegativeLimitIsUsageError(t *testing.T) {
	t.Parallel()

	for _, withEndpoint := range []bool{false, true} {
		hits, baseURL := dryRunHitServer(t)
		binary := buildDryRunCLI(t, baseURL, withEndpoint)
		work := t.TempDir()
		db := filepath.Join(work, "nested", "neg.db")

		for _, extra := range [][]string{nil, {"--json"}, {"--dry-run"}, {"--dry-run", "--json"}, {"--data-source", "live"}} {
			args := append([]string{"search", "sentinel", "--limit", "-1", "--db", db}, extra...)
			stdout, stderr, err := runSearchCLI(t, binary, args...)
			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr, "stdout:\n%s\nstderr:\n%s", stdout, stderr)
			assert.Equal(t, 2, exitErr.ExitCode(), "stderr:\n%s", stderr)
			assert.Contains(t, stderr, "--limit must be 0 or greater, got -1")
			assert.NotContains(t, stderr, "panic")
			assert.Empty(t, stdout)
		}
		assert.Empty(t, dirNames(t, work), "negative limit must create no database or directory")
		assert.Zero(t, hits.Load(), "negative limit must not dial the API")

		// Valid boundaries keep working: 0 and positive limits do not error.
		for _, limit := range []string{"0", "1", "50"} {
			stdout, stderr, err := runSearchCLI(t, binary, "search", "sentinel", "--data-source", "local", "--limit", limit, "--json", "--db", filepath.Join(work, "ok.db"))
			require.NoError(t, err, "limit %s\nstdout:\n%s\nstderr:\n%s", limit, stdout, stderr)
			assert.NotContains(t, stderr, "panic")
		}
	}
}
