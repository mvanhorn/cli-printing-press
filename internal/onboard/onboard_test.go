package onboard

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/cartridge"
	"github.com/mvanhorn/cli-printing-press/v4/internal/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func TestScanLocalDomainsAndMatchCatalog(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("PP_HOME", filepath.Join(tmp, ".pp"))

	// Create Chromium Cookies DB with ONLY plaintext host_key + expires_utc
	// (no encrypted_value column at all, proving we never select secrets).
	cookiesPath := filepath.Join(tmp, "Cookies")
	cdb, err := sql.Open("sqlite", cookiesPath)
	require.NoError(t, err)
	_, err = cdb.Exec(`CREATE TABLE cookies (host_key TEXT, expires_utc INTEGER);
INSERT INTO cookies VALUES ('.instacart.com', 0);
INSERT INTO cookies VALUES ('.instacart.com', 0);
INSERT INTO cookies VALUES ('.monarchmoney.com', 0);
INSERT INTO cookies VALUES ('.expired.example', 100);`)
	require.NoError(t, err)
	require.NoError(t, cdb.Close())

	historyPath := filepath.Join(tmp, "History")
	hdb, err := sql.Open("sqlite", historyPath)
	require.NoError(t, err)
	_, err = hdb.Exec(`CREATE TABLE urls (url TEXT, visit_count INTEGER);
INSERT INTO urls VALUES ('https://www.instacart.com/store/costco/front', 42);
INSERT INTO urls VALUES ('https://www.airbnb.com/rooms/123', 15);
INSERT INTO urls VALUES ('https://www.espn.com/nba/scoreboard', 8);`)
	require.NoError(t, err)
	require.NoError(t, hdb.Close())

	signals, err := ScanLocalDomains([]BrowserSource{
		{Name: "ardia", Engine: EngineChromium, CookiesPath: cookiesPath, HistoryPath: historyPath},
	})
	require.NoError(t, err)
	require.NotEmpty(t, signals)

	cat := &registry.Catalog{
		SchemaVersion: 2,
		Entries: []registry.Entry{
			{Name: "instacart", Category: "commerce", Homepage: "https://www.instacart.com", Description: "Instacart CLI"},
			{Name: "monarch-money", Category: "payments", Homepage: "https://www.monarch.com", Description: "Monarch Money CLI"},
			{Name: "airbnb", Category: "travel", Homepage: "https://www.airbnb.com", Description: "Airbnb CLI"},
		},
	}
	builtins, err := cartridge.Builtins()
	require.NoError(t, err)

	recs := MatchCatalog(signals, cat, nil, builtins, 10)
	require.Len(t, recs, 4)
	// Active-session domains must rank ahead of history-only domains.
	assert.Equal(t, "instacart", recs[0].App)
	assert.True(t, recs[0].HasSession)
	assert.Equal(t, 42, recs[0].VisitCount)
	assert.Equal(t, "monarch-money", recs[1].App)
	assert.True(t, recs[1].HasSession)
	assert.Equal(t, "airbnb", recs[2].App)
	assert.False(t, recs[2].HasSession)
	assert.Equal(t, "espn", recs[3].App)
	assert.Equal(t, "cartridge", recs[3].Kind)
	assert.True(t, recs[3].Installed)
}
