package openapi

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectPaginationSkipsDateFormattedCursors(t *testing.T) {
	t.Parallel()

	t.Run("date after is not a cursor", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, detectPagination([]spec.Param{{Name: "after", In: "query", Format: "date"}}, nil))
	})

	t.Run("date after yields to a later opaque cursor", func(t *testing.T) {
		t.Parallel()
		pag := detectPagination([]spec.Param{
			{Name: "after", In: "query", Format: "date"},
			{Name: "next_token", In: "query"},
		}, nil)
		require.NotNil(t, pag)
		assert.Equal(t, "next_token", pag.CursorParam)
		assert.Equal(t, "cursor", pag.Type)
	})

	t.Run("date after keeps pageToken", func(t *testing.T) {
		t.Parallel()
		pag := detectPagination([]spec.Param{
			{Name: "after", In: "query", Format: "date-time"},
			{Name: "pageToken", In: "query"},
		}, nil)
		require.NotNil(t, pag)
		assert.Equal(t, "pageToken", pag.CursorParam)
		assert.Equal(t, "page_token", pag.Type)
	})

	t.Run("header after is not a cursor", func(t *testing.T) {
		t.Parallel()
		pag := detectPagination([]spec.Param{
			{Name: "after", In: "header", Format: "date"},
			{Name: "cursor", In: "query"},
		}, nil)
		require.NotNil(t, pag)
		assert.Equal(t, "cursor", pag.CursorParam)
	})

	t.Run("opaque after stays a cursor", func(t *testing.T) {
		t.Parallel()
		pag := detectPagination([]spec.Param{{Name: "after", In: "query"}}, nil)
		require.NotNil(t, pag)
		assert.Equal(t, "after", pag.CursorParam)
		assert.Equal(t, "cursor", pag.Type)
	})

	t.Run("date after yields to page_key", func(t *testing.T) {
		t.Parallel()
		pag := detectPagination([]spec.Param{
			{Name: "after", In: "query", Format: "date"},
			{Name: "before", In: "query", Format: "date"},
			{Name: "page_key", In: "query"},
		}, nil)
		require.NotNil(t, pag)
		assert.Equal(t, "page_key", pag.CursorParam)
		assert.Equal(t, "cursor", pag.Type)
	})

	t.Run("opaque after wins over page_key", func(t *testing.T) {
		t.Parallel()
		pag := detectPagination([]spec.Param{
			{Name: "after", In: "query"},
			{Name: "page_key", In: "query"},
		}, nil)
		require.NotNil(t, pag)
		assert.Equal(t, "after", pag.CursorParam)
		assert.Equal(t, "cursor", pag.Type)
	})

	t.Run("header page_key is not a cursor", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, detectPagination([]spec.Param{{Name: "page_key", In: "header"}}, nil))
	})
}
