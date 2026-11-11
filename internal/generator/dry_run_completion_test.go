package generator

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// Exercise the emitted binary, including root hooks, not just template text.
func TestGeneratedProfileAndSyncDryRunSideEffects(t *testing.T) {
	hits, baseURL := dryRunHitServer(t)
	apiSpec := searchCLISpec("completionfx", baseURL, map[string]spec.Resource{}, searchCLITypes())
	apiSpec.Resources["comuni"] = apiSpec.Resources["records"]
	delete(apiSpec.Resources, "records")
	_, binary := buildSearchCLI(t, apiSpec)
	modes := [][]string{nil, {"--json"}, {"--agent"}, {"--quiet"}, {"--yes", "--quiet"}, {"--yes", "--agent"}, {"--csv"}, {"--plain"}, {"--compact"}}
	for _, mode := range modes {
		t.Run("profile/"+joinDryRunMode(mode), func(t *testing.T) {
			for _, state := range []string{"existing", "missing", "nested"} {
				t.Run(state, func(t *testing.T) {
					root := t.TempDir()
					home := filepath.Join(root, "home")
					if state == "nested" {
						home = filepath.Join(home, "nested", "deeper")
					}
					path := filepath.Join(home, "config", "profiles.json")
					sentinel := []byte("{\"profiles\":{\"sentinel\":{\"name\":\"sentinel\",\"values\":{\"quiet\":\"false\"}}}}\n")
					if state == "existing" {
						require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
						require.NoError(t, os.WriteFile(path, sentinel, 0o640))
					}
					for _, verb := range []string{"save", "delete"} {
						args := []string{"profile", verb, "sentinel", "--home", home, "--dry-run"}
						if verb == "save" && len(mode) == 0 {
							args = append(args, "--compact")
						}
						args = append(args, mode...)
						out, stderr, err := runSearchCLI(t, binary, args...)
						require.NoError(t, err, "%v stdout=%s stderr=%s", args, out, stderr)
						if containsDryRunMode(mode, "--json") || containsDryRunMode(mode, "--agent") {
							requireDryRunReport(t, out)
						}
						if state == "existing" {
							got, err := os.ReadFile(path)
							require.NoError(t, err)
							assert.Equal(t, sentinel, got, "%s must preserve profile bytes", verb)
							info, err := os.Stat(path)
							require.NoError(t, err)
							if runtime.GOOS != "windows" {
								assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
							}
							assert.NoFileExists(t, path+".tmp")
						} else {
							assert.NoDirExists(t, home, "preview must not create profile directories")
						}
					}
				})
			}
		})
	}
	t.Run("sync", func(t *testing.T) {
		for _, mode := range modes {
			for _, state := range []string{"existing", "missing", "nested"} {
				t.Run(joinDryRunMode(mode)+"/"+state, func(t *testing.T) {
					root := t.TempDir()
					db := filepath.Join(root, "preview.db")
					if state == "nested" {
						db = filepath.Join(root, "nested", "deeper", "preview.db")
					}
					// Invalid SQLite bytes prove no store open/migration was attempted.
					sentinel := []byte("NOT A DATABASE; KEEP EXACT BYTES\n")
					if state == "existing" {
						require.NoError(t, os.WriteFile(db, sentinel, 0o640))
					}
					before := dirNames(t, root)
					args := []string{"sync", "--resources", "comuni", "--db", db, "--home", filepath.Join(root, "home"), "--dry-run", "--full", "--latest-only"}
					args = append(args, mode...)
					out, stderr, err := runSearchCLI(t, binary, args...)
					require.NoError(t, err, "%v stdout=%s stderr=%s", args, out, stderr)
					if containsDryRunMode(mode, "--json") || containsDryRunMode(mode, "--agent") {
						requireDryRunReport(t, out)
					}
					assert.Equal(t, before, dirNames(t, root), "preview must create no paths")
					if state == "existing" {
						got, err := os.ReadFile(db)
						require.NoError(t, err)
						assert.Equal(t, sentinel, got)
					} else {
						assert.NoFileExists(t, db)
					}
					for _, suffix := range []string{"-wal", "-shm", "-journal"} {
						assert.NoFileExists(t, db+suffix)
					}
				})
			}
		}
	})
	t.Run("validation", func(t *testing.T) {
		for _, args := range [][]string{
			{"profile", "save", "bad/name", "--compact"},
			{"profile", "save", "sentinel"},
			{"profile", "save", "sentinel", "--token", "not-a-secret"},
			{"sync", "--since", "not-a-duration"},
			{"sync", "--param", "invalid"},
			{"sync", "--resource-param", "unknown.limit=1"},
		} {
			home := filepath.Join(t.TempDir(), "absent", "home")
			args = append(args, "--dry-run", "--home", home)
			_, _, err := runSearchCLI(t, binary, args...)
			require.Error(t, err, "%v", args)
			assert.NoDirExists(t, home)
		}
	})
	assert.Zero(t, hits.Load(), "previews/validation must never send requests")
	t.Run("real-profile", func(t *testing.T) {
		home := filepath.Join(t.TempDir(), "home")
		path := filepath.Join(home, "config", "profiles.json")
		_, stderr, err := runSearchCLI(t, binary, "profile", "save", "real", "--home", home, "--dry-run=false")
		require.NoError(t, err, stderr)
		require.FileExists(t, path)
		before, err := os.ReadFile(path)
		require.NoError(t, err)
		_, _, err = runSearchCLI(t, binary, "profile", "delete", "real", "--home", home)
		require.Error(t, err, "real delete still requires confirmation")
		after, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, before, after)
		_, stderr, err = runSearchCLI(t, binary, "profile", "delete", "real", "--yes", "--home", home)
		require.NoError(t, err, stderr)
		after, err = os.ReadFile(path)
		require.NoError(t, err)
		assert.NotContains(t, string(after), "\"real\"")
	})
	t.Run("existing-sqlite", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "legacy.db")
		db, err := sql.Open("sqlite", path)
		require.NoError(t, err)
		_, err = db.Exec("CREATE TABLE sentinel (value TEXT); INSERT INTO sentinel VALUES ('keep')")
		require.NoError(t, err)
		require.NoError(t, db.Close())
		before, err := os.ReadFile(path)
		require.NoError(t, err)
		info, err := os.Stat(path)
		require.NoError(t, err)
		names := dirNames(t, root)
		out, stderr, err := runSearchCLI(t, binary, "sync", "--resources", "comuni", "--dry-run", "--agent", "--db", path)
		require.NoError(t, err, stderr)
		requireDryRunReport(t, out)
		after, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, before, after, "dry-run must not migrate existing SQLite")
		afterInfo, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, info.ModTime(), afterInfo.ModTime())
		assert.Equal(t, names, dirNames(t, root))
	})
	t.Run("real-sync", func(t *testing.T) {
		db := filepath.Join(t.TempDir(), "real.db")
		_, stderr, err := runSearchCLI(t, binary, "sync", "--resources", "comuni", "--db", db)
		require.NoError(t, err, stderr)
		require.FileExists(t, db)
		assert.Positive(t, hits.Load(), "real sync must still fetch")
	})
}

func containsDryRunMode(mode []string, want string) bool {
	for _, s := range mode {
		if s == want {
			return true
		}
	}
	return false
}

func joinDryRunMode(mode []string) string {
	if len(mode) == 0 {
		return "human"
	}
	return mode[len(mode)-1]
}
