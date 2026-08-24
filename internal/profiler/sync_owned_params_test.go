package profiler

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
)

// TestSyncQueryParamDefaultsExcludeSyncOwnedKeys pins which spec defaults reach
// the syncer. The syncer assigns the paging, since, sort, and date-range keys
// itself, and does so inside conditionals -- a seeded default would survive the
// branch where it deliberately withheld the key. Everything else is scoping the
// endpoint command already sends, and must be seeded so both surfaces address
// the same slice of the API.
func TestSyncQueryParamDefaultsExcludeSyncOwnedKeys(t *testing.T) {
	t.Parallel()

	endpoint := spec.Endpoint{
		Method:   "GET",
		Path:     "/tickets",
		Response: spec.ResponseDef{Type: "array"},
		Params: []spec.Param{
			{Name: "workspace-id", In: "query", Type: "string", Default: "ws-42"},
			{Name: "updated_since", In: "query", Type: "string", Default: "2020-01-01"},
			{Name: "sort", In: "query", Type: "string", Default: "updated_at:asc"},
			{Name: "dates", In: "query", Type: "string", Default: "last_30_days"},
			{Name: "cursor", In: "query", Type: "string", Default: "abc"},
			{Name: "limit", In: "query", Type: "integer", Default: 25},
			{Name: "include_closed", In: "query", Type: "boolean", Default: true},
		},
	}

	got := syncQueryParamDefaultsFromEndpoint(endpoint, syncOwnedParams{
		cursor:    "cursor",
		limit:     "limit",
		since:     "updated_since",
		sort:      "sort",
		dateRange: syncDateRangeParamNames,
	})

	assert.ElementsMatch(t, []SyncQueryParamDefault{
		{Name: "workspace-id", Value: "ws-42"},
		{Name: "include_closed", Value: "true"},
	}, got)
}

// TestSyncQueryParamDefaultsFromEndpointHonorsDetectedSort proves the sort
// exclusion is wired to the same detection the generated syncResourceSortParam
// is emitted from, rather than to a hardcoded name, so the two cannot drift.
func TestSyncQueryParamDefaultsFromEndpointHonorsDetectedSort(t *testing.T) {
	t.Parallel()

	endpoint := spec.Endpoint{
		Method:   "GET",
		Path:     "/tickets",
		Response: spec.ResponseDef{Type: "array"},
		Params: []spec.Param{
			{Name: "updated_since", In: "query", Type: "string"},
			{Name: "order_by", In: "query", Type: "string", Default: "updated_at:asc"},
		},
	}

	sortParam, sortValue := detectEndpointSyncSort(endpoint)
	assert.Equal(t, "order_by", sortParam, "an ascending last-modified sort must be detected")
	assert.Equal(t, "updated_at:asc", sortValue)

	got := syncQueryParamDefaultsFromEndpoint(endpoint, syncOwnedParams{
		since:     "updated_since",
		sort:      sortParam,
		dateRange: syncDateRangeParamNames,
	})
	assert.Empty(t, got, "the detected sort param must not be seeded under any spelling")
}
