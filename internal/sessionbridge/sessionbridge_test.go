package sessionbridge

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestResolveEnvBearerAndAPIKey(t *testing.T) {
	t.Setenv("DEMO_TEST_TOKEN", "secret-abc")

	res, err := Resolve(AuthSpec{
		AppName:  "demo",
		Strategy: "env_bearer",
		EnvVar:   "DEMO_TEST_TOKEN",
	})
	require.NoError(t, err)
	assert.Equal(t, "Bearer secret-abc", res.Headers["Authorization"])

	req, err := http.NewRequest(http.MethodGet, "https://example.test/v1", nil)
	require.NoError(t, err)
	res.Apply(req)
	assert.Equal(t, "Bearer secret-abc", req.Header.Get("Authorization"))
}

func TestResolveAgentCookieSidecar(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	dbPath := filepath.Join(tmp, "cookies-plain.db")
	t.Setenv("AGENTCOOKIE_SIDECAR_DB", dbPath)

	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = db.Exec(`CREATE TABLE cookies (host_key TEXT, name TEXT, value TEXT);
INSERT INTO cookies VALUES ('.instacart.com', '_instacart_session', 'tok123');
INSERT INTO cookies VALUES ('other.example', 'nope', 'ignore');`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	res, err := Resolve(AuthSpec{
		AppName:  "instacart",
		Strategy: "browser_cookie",
		Domains:  []string{".instacart.com"},
	})
	require.NoError(t, err)
	assert.Equal(t, "_instacart_session=tok123", res.Headers["Cookie"])
	assert.Contains(t, res.Source, "agentcookie:")
}
