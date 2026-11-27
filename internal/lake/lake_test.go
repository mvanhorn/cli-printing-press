package lake

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/cartridge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLakeSyncSearchAndReadOnlySQL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"trips":[{"id":"t1","title":"Seattle to Tokyo Nonstop","note":"Delta DL167 confirmed","url":"https://delta.example/t1"}]}`))
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	cart := &cartridge.Cartridge{
		Schema:      cartridge.SchemaVersionV1,
		Name:        "delta",
		Version:     "1.0",
		Description: "Delta trips",
		Trust:       cartridge.TrustPolicy{AllowedHosts: []string{u.Hostname()}, ReadOnly: true},
		Commands: map[string]cartridge.CommandSpec{
			"trips": {
				Description: "List trips",
				Method:      "GET",
				URL:         srv.URL + "/trips",
				Extract:     "$.trips[*]",
				SyncDefault: true,
				LakeProjection: &cartridge.LakeProjection{
					Entity:  "trip",
					Key:     "id",
					Title:   "title",
					Summary: "note",
					URL:     "url",
				},
			},
		},
	}
	require.NoError(t, cart.Validate())

	dbPath := filepath.Join(t.TempDir(), "lake.db")
	lk, err := Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = lk.Close() }()

	rep, err := lk.SyncCartridge(context.Background(), cart, cartridge.RunOptions{})
	require.NoError(t, err)
	assert.Equal(t, 1, rep.RowsSynced)

	hits, err := lk.Search(context.Background(), "Tokyo", "", "", 10)
	require.NoError(t, err)
	require.Len(t, hits, 1)
	assert.Equal(t, "delta", hits[0].Source)
	assert.Equal(t, "trip", hits[0].EntityType)

	rows, err := lk.QueryReadOnly(context.Background(), "SELECT source, title FROM lake_items")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "Seattle to Tokyo Nonstop", rows[0]["title"])

	// Mutating SQL must be rejected.
	_, err = lk.QueryReadOnly(context.Background(), "DELETE FROM lake_items")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read-only")

	st, err := lk.Stats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, st.TotalRows)
}
