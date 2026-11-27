package cartridge

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func isolateEnv(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, ".cache"))
	t.Setenv("PP_HOME", filepath.Join(tmp, ".pp"))
	return tmp
}

func TestBuiltinsParseAndValidate(t *testing.T) {
	builtins, err := Builtins()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(builtins), 6)

	names := make([]string, 0, len(builtins))
	for _, b := range builtins {
		names = append(names, b.Name)
		assert.NotEmpty(t, b.SHA256)
		assert.NoError(t, b.Validate())
	}
	assert.Contains(t, names, "espn")
	assert.Contains(t, names, "hackernews")
	assert.Contains(t, names, "chrome-history")
	assert.Contains(t, names, "local-notes")
}

func TestValidateRejectsWildcardAndDisallowedHosts(t *testing.T) {
	badWildcard := []byte(`
schema: pp-cartridge/v1
name: evil
version: "1.0"
description: bad
trust:
  allowed_hosts: ["*"]
commands:
  get:
    url: https://example.com/api
`)
	_, err := Parse(badWildcard, "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wildcard")

	badHost := []byte(`
schema: pp-cartridge/v1
name: mismatch
version: "1.0"
description: bad
trust:
  allowed_hosts: ["api.allowed.test"]
commands:
  get:
    url: https://evil.test/steal
`)
	_, err = Parse(badHost, "test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in trust.allowed_hosts")
}

func TestExecuteHTTPExtractCompactDryRunAndMutationGuard(t *testing.T) {
	isolateEnv(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/items":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"events":[{"id":"1","name":"Lakers vs Celtics","status":{"short":"Final"},"noise":"ignore"},{"id":"2","name":"Warriors vs Suns","status":{"short":"Q4"},"noise":"ignore"}]}`))
		case "/v1/delete":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"deleted":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	c := &Cartridge{
		Schema:      SchemaVersionV1,
		Name:        "demo",
		Version:     "1.0.0",
		Description: "Demo API",
		Trust: TrustPolicy{
			AllowedHosts: []string{u.Hostname()},
			Mutates:      []string{"remove"},
		},
		Auth: AuthConfig{Strategy: AuthStrategyNone},
		Commands: map[string]CommandSpec{
			"list": {
				Description:   "List items",
				Method:        "GET",
				URL:           srv.URL + "/v1/items",
				Extract:       "$.events[*]",
				CompactFields: []string{"id", "name", "status=status.short"},
			},
			"remove": {
				Description: "Delete item",
				Method:      "POST",
				URL:         srv.URL + "/v1/delete",
				Mutates:     true,
			},
		},
	}
	require.NoError(t, c.Validate())

	// Compact read + limit.
	res, err := Execute(context.Background(), c, "list", RunOptions{Compact: true, Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, 1, res.RowCount)
	rows, ok := res.Data.([]any)
	require.True(t, ok)
	first, ok := rows[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "1", first["id"])
	assert.Equal(t, "Final", first["status"])
	assert.NotContains(t, first, "noise")

	// Mutation blocked without --yes.
	_, err = Execute(context.Background(), c, "remove", RunOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--yes")

	// Dry-run succeeds on mutating command without hitting server.
	dry, err := Execute(context.Background(), c, "remove", RunOptions{DryRun: true})
	require.NoError(t, err)
	assert.True(t, dry.DryRun)

	// Confirmed write succeeds.
	okRes, err := Execute(context.Background(), c, "remove", RunOptions{ConfirmWrite: true})
	require.NoError(t, err)
	assert.Equal(t, 200, okRes.HTTPStatus)
}

func TestExecuteLocalSQLiteAndLocalFiles(t *testing.T) {
	tmp := isolateEnv(t)

	// Build a synthetic SQLite DB.
	dbPath := filepath.Join(tmp, "sample.db")
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY, title TEXT); INSERT INTO notes (title) VALUES ('Printing Press 2.0'), ('Ardia Fast Path');`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	// Build a local markdown file.
	mdPath := filepath.Join(tmp, "idea.md")
	require.NoError(t, os.WriteFile(mdPath, []byte("# Skip Pixels\nUse cartridges for repeat work.\n"), 0o600))

	c := &Cartridge{
		Schema:      SchemaVersionV1,
		Name:        "local-test",
		Version:     "1.0",
		Description: "Local drivers",
		Trust:       TrustPolicy{ReadOnly: true},
		Commands: map[string]CommandSpec{
			"db": {
				Description: "Read notes DB",
				Driver:      DriverLocalSQLite,
				DBPath:      dbPath,
				SQL:         "SELECT id, title FROM notes ORDER BY id ASC",
			},
			"db-write-attempt": {
				Description: "Should be blocked",
				Driver:      DriverLocalSQLite,
				DBPath:      dbPath,
				SQL:         "DELETE FROM notes",
			},
			"files": {
				Description: "Scan markdown",
				Driver:      DriverLocalFiles,
				Glob:        filepath.Join(tmp, "*.md"),
				Format:      "markdown",
			},
		},
	}
	require.NoError(t, c.Validate())

	res, err := Execute(context.Background(), c, "db", RunOptions{})
	require.NoError(t, err)
	assert.Equal(t, 2, res.RowCount)

	_, err = Execute(context.Background(), c, "db-write-attempt", RunOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read-only")

	fRes, err := Execute(context.Background(), c, "files", RunOptions{Params: map[string]string{"query": "cartridges"}})
	require.NoError(t, err)
	assert.Equal(t, 1, fRes.RowCount)
}

func TestLockfileDetectsDrift(t *testing.T) {
	isolateEnv(t)

	raw, ok := BuiltinRaw("espn")
	require.True(t, ok)

	cart, path, err := InstallBytes(raw, "builtin:espn.yaml")
	require.NoError(t, err)

	lockPath, err := LockfilePath()
	require.NoError(t, err)
	lf, err := LoadLockfile(lockPath)
	require.NoError(t, err)
	require.NoError(t, lf.VerifyCartridge(cart, true))

	// Tamper with installed cartridge on disk.
	tampered := append(raw, []byte("\n# tampered\n")...)
	require.NoError(t, os.WriteFile(path, tampered, 0o600))
	reloaded, err := LoadFile(path)
	require.NoError(t, err)

	err = lf.VerifyCartridge(reloaded, true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SHA-256 drift")
}

func TestExecuteOmitsUnsetOptionalQueryParams(t *testing.T) {
	var seenRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenRawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"events":[{"id":"1"}]}`))
	}))
	defer srv.Close()

	parsed, err := url.Parse(srv.URL)
	require.NoError(t, err)
	raw := []byte("schema: pp-cartridge/v1\n" +
		"name: opt-query\n" +
		"version: \"1.0.0\"\n" +
		"description: Test optional query params\n" +
		"trust:\n" +
		"  allowed_hosts:\n" +
		"    - " + parsed.Hostname() + "\n" +
		"  read_only: true\n" +
		"commands:\n" +
		"  list:\n" +
		"    driver: http\n" +
		"    method: GET\n" +
		"    url: " + srv.URL + "/events\n" +
		"    params:\n" +
		"      dates:\n" +
		"        type: string\n" +
		"    query:\n" +
		"      dates: \"{dates}\"\n" +
		"    extract: \"$.events[*]\"\n")

	c, err := Parse(raw, "opt-query.yaml")
	require.NoError(t, err)

	res, err := Execute(context.Background(), c, "list", RunOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, res.RowCount)
	assert.Empty(t, seenRawQuery, "unset optional query param must not be sent as literal placeholder")
}
