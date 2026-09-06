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

func TestSyncQueryParamDefaultsWidenOpenStatusFilter(t *testing.T) {
	t.Parallel()

	endpoint := spec.Endpoint{
		Method:   "GET",
		Path:     "/orders",
		Response: spec.ResponseDef{Type: "array"},
		Params: []spec.Param{
			{Name: "status", In: "query", Type: "string", Default: "open", Enum: []string{"open", "closed", "all"}},
			{Name: "workspace-id", In: "query", Type: "string", Default: "ws-42"},
		},
	}

	got := syncQueryParamDefaultsFromEndpoint(endpoint, syncOwnedParams{})
	assert.ElementsMatch(t, []SyncQueryParamDefault{
		{Name: "status", Value: "all"},
		{Name: "workspace-id", Value: "ws-42"},
	}, got)
}

func TestSyncQueryParamDefaultsWidenOpenStateToAny(t *testing.T) {
	t.Parallel()

	endpoint := spec.Endpoint{
		Method:   "GET",
		Path:     "/orders",
		Response: spec.ResponseDef{Type: "array"},
		Params: []spec.Param{
			{Name: "status", In: "query", Type: "string", Default: "open", Enum: []string{"open", "closed", "any"}},
		},
	}

	got := syncQueryParamDefaultsFromEndpoint(endpoint, syncOwnedParams{})
	assert.Equal(t, []SyncQueryParamDefault{{Name: "status", Value: "any"}}, got)
}

func TestSyncQueryParamDefaultsDoNotWidenActiveOrUnrelatedDefaults(t *testing.T) {
	t.Parallel()

	endpoint := spec.Endpoint{
		Method:   "GET",
		Path:     "/subscriptions",
		Response: spec.ResponseDef{Type: "array"},
		Params: []spec.Param{
			{Name: "status", In: "query", Type: "string", Default: "active", Enum: []string{"active", "canceled", "all"}},
			{Name: "sort", In: "query", Type: "string", Default: "new"},
		},
	}

	got := syncQueryParamDefaultsFromEndpoint(endpoint, syncOwnedParams{})
	assert.ElementsMatch(t, []SyncQueryParamDefault{
		{Name: "status", Value: "active"},
		{Name: "sort", Value: "new"},
	}, got)
}

func TestSyncQueryParamSeedHiddenHistoryWhenEnumHasNoAll(t *testing.T) {
	t.Parallel()

	endpoint := spec.Endpoint{
		Method:   "GET",
		Path:     "/orders",
		Response: spec.ResponseDef{Type: "array"},
		Params: []spec.Param{
			{Name: "status", In: "query", Type: "string", Default: "open", Enum: []string{"open", "closed"}},
		},
	}

	seed := syncQueryParamSeedFromEndpoint(endpoint, syncOwnedParams{})
	assert.Equal(t, []SyncQueryParamDefault{{Name: "status", Value: "open"}}, seed.Defaults)
	assert.Equal(t, []SyncQueryParamDefault{{Name: "status", Value: "open"}}, seed.HiddenHistory)
}

func TestSyncParamsOverlayWinsAndSuppressesHiddenHistory(t *testing.T) {
	t.Parallel()

	endpoint := spec.Endpoint{
		Method:   "GET",
		Path:     "/orders",
		Response: spec.ResponseDef{Type: "array"},
		Params: []spec.Param{
			{Name: "status", In: "query", Type: "string", Default: "open", Enum: []string{"open", "closed", "all"}},
		},
		SyncParams: map[string]string{"status": "open"},
	}

	seed := syncQueryParamSeedFromEndpoint(endpoint, syncOwnedParams{})
	assert.Equal(t, []SyncQueryParamDefault{{Name: "status", Value: "open"}}, seed.Defaults)
	assert.Empty(t, seed.HiddenHistory, "explicit sync_params status=open is the intended scope")
}

func TestSyncParamsAddsAllHistoryWithoutSpecDefault(t *testing.T) {
	t.Parallel()

	endpoint := spec.Endpoint{
		Method:   "GET",
		Path:     "/orders",
		Response: spec.ResponseDef{Type: "array"},
		Params: []spec.Param{
			{Name: "status", In: "query", Type: "string", Enum: []string{"open", "closed", "all"}},
		},
		SyncParams: map[string]string{"status": "all"},
	}

	got := syncQueryParamDefaultsFromEndpoint(endpoint, syncOwnedParams{limit: "limit"})
	assert.Equal(t, []SyncQueryParamDefault{{Name: "status", Value: "all"}}, got)
}
