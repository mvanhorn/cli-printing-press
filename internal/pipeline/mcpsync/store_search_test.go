package mcpsync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpgradeStoreSearchContextAddsDeadlineQuery(t *testing.T) {
	t.Parallel()

	src := []byte(`package store

import (
	"database/sql"
)

func (s *Store) Search(query string, limit int, resourceTypes ...string) ([]json.RawMessage, error) {
	// } comment brace must not end the function
	q := ` + "`" + `select {not-a-group}` + "`" + `
	note := "brace } inside"
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return nil, rows.Err()
}
`)
	updated, err := upgradeStoreSearchContext(src)
	require.NoError(t, err)
	text := string(updated)
	assert.Contains(t, text, "func (s *Store) Search(query string, limit int, resourceTypes ...string) ([]json.RawMessage, error) {\n\treturn s.SearchContext(context.Background(), query, limit, resourceTypes...)\n}")
	assert.Contains(t, text, "func (s *Store) SearchContext(ctx context.Context, query string, limit int, resourceTypes ...string)")
	assert.Contains(t, text, "s.db.QueryContext(ctx, q, args...)")
	assert.Contains(t, text, "if ctx == nil {")
	assert.NotContains(t, text, "s.db.Query(")
	assert.Contains(t, text, "\t\"context\"\n")

	again, err := upgradeStoreSearchContext(updated)
	require.NoError(t, err)
	assert.Equal(t, updated, again)
}

func TestUpgradeStoreSearchContextRejectsChangedSignature(t *testing.T) {
	t.Parallel()

	src := []byte(`package store

func (s *Store) Search(query string, limit int) ([]json.RawMessage, error) {
	return nil, nil
}
`)
	_, err := upgradeStoreSearchContext(src)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not the generated shape")
}
