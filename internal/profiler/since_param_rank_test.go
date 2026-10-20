package profiler

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectEndpointSinceParamRanking(t *testing.T) {
	t.Parallel()

	const eastern = "Filter from 2014-02-01 or 2014-02-01T10:00:00 (Eastern Time)"
	cases := []struct {
		name       string
		params     []spec.Param
		pagination *spec.Pagination
		types      map[string]spec.TypeDef
		response   spec.ResponseDef
		wantParam  string
		wantFormat string
	}{
		{
			name: "updated_on beats earlier start_date",
			params: []spec.Param{
				{Name: "start_date", Type: "string", Description: "Filter by start date. Examples: `>=2024-01-01`"},
				{Name: "updated_on", Type: "string", Description: "Filter by last updated date. Examples: `>=2024-01-01`"},
				{Name: "created_on", Type: "string", Description: "Filter by creation date. Examples: `>=2024-01-01`"},
				{Name: "due_date", Type: "string", Description: "Filter by due date. Examples: `>=2024-01-01`"},
			},
			wantParam:  "updated_on",
			wantFormat: "operator:>=:date",
		},
		{
			name: "updatedMin beats timeMin",
			params: []spec.Param{
				{Name: "timeMin", Type: "string", Description: "Lower bound 2011-06-03T10:00:00-07:00 or 2011-06-03T10:00:00Z"},
				{Name: "timeMax", Type: "string", Description: "Upper bound 2011-06-03T10:00:00-07:00"},
				{Name: "updatedMin", Type: "string", Description: "Lower bound for an event's last modification time (as a RFC3339 timestamp)."},
			},
			wantParam:  "updatedMin",
			wantFormat: "",
		},
		{
			name: "date after with before",
			params: []spec.Param{
				{Name: "after", Type: "string", Format: "date", In: "query", Description: "Return records after this date"},
				{Name: "before", Type: "string", Format: "date", In: "query"},
				{Name: "page_key", Type: "string", In: "query"},
			},
			wantParam:  "after",
			wantFormat: "date",
		},
		{
			name: "After Before case preserved",
			params: []spec.Param{
				{Name: "After", Type: "string", Format: "date", In: "Query"},
				{Name: "Before", Type: "string", Format: "date", In: "Query"},
			},
			wantParam:  "After",
			wantFormat: "date",
		},
		{
			name: "fromdate with todate and eastern naive clock",
			params: []spec.Param{
				{Name: "fromdate", Type: "string", Description: eastern},
				{Name: "todate", Type: "string", Description: "Through 2014-02-01"},
			},
			wantParam:  "fromdate",
			wantFormat: "local-datetime:America/New_York",
		},
		{
			name: "dateFrom dateTo",
			params: []spec.Param{
				{Name: "dateFrom", Type: "string", Format: "date"},
				{Name: "dateTo", Type: "string", Format: "date"},
			},
			wantParam:  "dateFrom",
			wantFormat: "date",
		},
		{
			name: "startDate loses to updatedMin",
			params: []spec.Param{
				{Name: "startDate", Type: "string", Format: "date"},
				{Name: "endDate", Type: "string", Format: "date"},
				{Name: "updatedMin", Type: "string"},
			},
			wantParam:  "updatedMin",
			wantFormat: "",
		},
		{
			name: "updatedMax and updated_before lose to updatedMin",
			params: []spec.Param{
				{Name: "updatedMax", Type: "string", Format: "date-time"},
				{Name: "updated_before", Type: "string", Format: "date-time"},
				{Name: "updatedMin", Type: "string", Format: "date-time"},
			},
			wantParam:  "updatedMin",
			wantFormat: "date-time",
		},
		{
			name: "createdAfter alone",
			params: []spec.Param{
				{Name: "createdAfter", Type: "string", Format: "date-time"},
			},
			wantParam:  "createdAfter",
			wantFormat: "date-time",
		},
		{
			name: "updated_on beats createdAfter",
			params: []spec.Param{
				{Name: "createdAfter", Type: "string", Format: "date-time"},
				{Name: "updated_on", Type: "string"},
			},
			wantParam:  "updated_on",
			wantFormat: "",
		},
		{
			name: "first equal rank wins",
			params: []spec.Param{
				{Name: "updated_at", Type: "string", Format: "date-time"},
				{Name: "modified_since", Type: "string", Format: "date"},
			},
			wantParam:  "updated_at",
			wantFormat: "date-time",
		},
		{
			name: "later equal rank does not replace",
			params: []spec.Param{
				{Name: "modified_since", Type: "string"},
				{Name: "updated_at", Type: "string"},
			},
			wantParam:  "modified_since",
			wantFormat: "",
		},
		{
			name: "since beats updated_fields",
			params: []spec.Param{
				{Name: "updated_fields", Type: "string"},
				{Name: "since", Type: "string", Format: "date-time"},
			},
			wantParam:  "since",
			wantFormat: "date-time",
		},
		{
			name: "integer since kept",
			params: []spec.Param{
				{Name: "since", Type: "integer"},
			},
			wantParam: "since",
		},
		{
			name: "number since kept",
			params: []spec.Param{
				{Name: "since", Type: "number"},
			},
			wantParam: "since",
		},
		{
			name: "integer updated_at kept",
			params: []spec.Param{
				{Name: "updated_at", Type: "integer"},
			},
			wantParam: "updated_at",
		},
		{
			name: "integer min_updated_ts rejected",
			params: []spec.Param{
				{Name: "min_updated_ts", Type: "integer"},
			},
		},
		{
			name: "string min_updated_ts rejected",
			params: []spec.Param{
				{Name: "min_updated_ts", Type: "string"},
			},
		},
		{
			name: "integer start_date without end rejected",
			params: []spec.Param{
				{Name: "start_date", Type: "integer", Format: "date"},
			},
		},
		{
			name: "boolean since rejected",
			params: []spec.Param{
				{Name: "since", Type: "boolean"},
			},
		},
		{
			name: "include_updated rejected",
			params: []spec.Param{
				{Name: "include_updated", Type: "boolean"},
				{Name: "include_updated", Type: "string"},
			},
		},
		{
			name: "since_id rejected",
			params: []spec.Param{
				{Name: "since_id", Type: "string"},
			},
		},
		{
			name: "upper bounds rejected",
			params: []spec.Param{
				{Name: "updatedbefore", Type: "string", Format: "date-time"},
				{Name: "updatedmax", Type: "string", Format: "date-time"},
				{Name: "updated_before", Type: "string", Format: "date-time"},
				{Name: "updatedMax", Type: "string", Format: "date-time"},
				{Name: "lastModifiedBy", Type: "string"},
				{Name: "updated_by", Type: "string"},
			},
		},
		{
			name: "drf lower bound kept",
			params: []spec.Param{
				{Name: "published_at__gte", Type: "string", Format: "date-time"},
			},
			wantParam:  "published_at__gte",
			wantFormat: "date-time",
		},
		{
			name: "drf upper bound rejected",
			params: []spec.Param{
				{Name: "updated_at__lt", Type: "string", Format: "date-time"},
			},
		},
		{
			name: "drf lte rejected",
			params: []spec.Param{
				{Name: "updated_at__lte", Type: "string", Format: "date-time"},
			},
		},
		{
			name: "non temporal drf rejected",
			params: []spec.Param{
				{Name: "num_highlights__gt", Type: "integer"},
			},
		},
		{
			name: "created_on not accepted",
			params: []spec.Param{
				{Name: "created_on", Type: "string", Format: "date"},
			},
		},
		{
			name: "from and to without date evidence",
			params: []spec.Param{
				{Name: "from", Type: "string"},
				{Name: "to", Type: "string"},
			},
		},
		{
			name: "fromdate without todate",
			params: []spec.Param{
				{Name: "fromdate", Type: "string", Format: "date"},
			},
		},
		{
			name: "fromdate todate yyyy-mm-dd description",
			params: []spec.Param{
				{Name: "fromdate", Type: "string", Description: "Format YYYY-MM-DD"},
				{Name: "todate", Type: "string"},
			},
			wantParam:  "fromdate",
			wantFormat: "date",
		},
		{
			name: "start_date and end without date evidence",
			params: []spec.Param{
				{Name: "start_date", Type: "string"},
				{Name: "end_date", Type: "string"},
			},
		},
		{
			name: "start_date with end and description date",
			params: []spec.Param{
				{Name: "start_date", Type: "string", Description: "Begin 2024-01-01"},
				{Name: "end_date", Type: "string"},
			},
			wantParam:  "start_date",
			wantFormat: "date",
		},
		{
			name: "start_date format date with end",
			params: []spec.Param{
				{Name: "start_date", Type: "string", Format: "date"},
				{Name: "end_date", Type: "string", Format: "date"},
			},
			wantParam:  "start_date",
			wantFormat: "date",
		},
		{
			name: "header after and before ignored",
			params: []spec.Param{
				{Name: "after", Type: "string", Format: "date", In: "header"},
				{Name: "before", Type: "string", Format: "date", In: "header"},
			},
		},
		{
			name: "query after does not pair with header before",
			params: []spec.Param{
				{Name: "after", Type: "string", Format: "date", In: "query"},
				{Name: "before", Type: "string", Format: "date", In: "header"},
			},
		},
		{
			name: "header location rejected",
			params: []spec.Param{
				{Name: "updatedMin", Type: "string", Format: "date-time", In: "HEADER"},
			},
		},
		{
			name: "path updated_at rejected",
			params: []spec.Param{
				{Name: "updated_at", Type: "string", Format: "date-time", In: "path", PathParam: true},
			},
		},
		{
			name: "only updated_before",
			params: []spec.Param{
				{Name: "updated_before", Type: "string", Format: "date-time"},
			},
		},
		{
			name: "no date filter",
			params: []spec.Param{
				{Name: "email", Type: "string"},
			},
		},
		{
			name: "cursor description is not a since param",
			params: []spec.Param{
				{Name: "cursor", Type: "string", Description: "Opaque token of the last modification"},
			},
		},
		{
			name: "starting_after without a date window",
			params: []spec.Param{
				{Name: "starting_after", Type: "string"},
			},
		},
		{
			name: "explicit cursor since falls through to updatedAt",
			params: []spec.Param{
				{Name: "since", Type: "string", Format: "date-time"},
				{Name: "updatedAt", Type: "string", Format: "date-time"},
			},
			pagination: &spec.Pagination{Type: "cursor", CursorParam: "since"},
			wantParam:  "updatedAt",
			wantFormat: "date-time",
		},
		{
			name: "only cursor since is not reused",
			params: []spec.Param{
				{Name: "since", Type: "string", Format: "date-time"},
			},
			pagination: &spec.Pagination{Type: "cursor", CursorParam: "since"},
		},
		{
			name: "wire name colliding with cursor is skipped",
			params: []spec.Param{
				{Name: "watermark", URLName: "since", Type: "string", Format: "date-time"},
				{Name: "updated_at", Type: "string", Format: "date"},
			},
			pagination: &spec.Pagination{Type: "cursor", CursorParam: "since"},
			wantParam:  "updated_at",
			wantFormat: "date",
		},
		{
			name: "conditions cursor is not an odata since",
			params: []spec.Param{
				{Name: "conditions", Type: "string"},
			},
			pagination: &spec.Pagination{Type: "cursor", CursorParam: "conditions"},
			response:   spec.ResponseDef{Item: "Ticket"},
			types: map[string]spec.TypeDef{
				"Ticket": {Fields: []spec.TypeField{{Name: "_info/lastUpdated", Type: "string"}}},
			},
		},
		{
			name: "odata conditions still selected",
			params: []spec.Param{
				{Name: "conditions", Type: "string"},
			},
			response: spec.ResponseDef{Item: "Ticket"},
			types: map[string]spec.TypeDef{
				"Ticket": {Fields: []spec.TypeField{{Name: "_info/lastUpdated", Type: "string"}}},
			},
			wantParam:  "conditions",
			wantFormat: "odata-conditions:_info/lastUpdated",
		},
		{
			name:       "glued updatedafter",
			params:     []spec.Param{{Name: "updatedafter", Type: "string", Format: "date-time"}},
			wantParam:  "updatedafter",
			wantFormat: "date-time",
		},
		{
			name:      "glued modified_after",
			params:    []spec.Param{{Name: "modified_after", Type: "string"}},
			wantParam: "modified_after",
		},
		{
			name:      "lastModified",
			params:    []spec.Param{{Name: "lastModified", Type: "string"}},
			wantParam: "lastModified",
		},
		{
			name:   "bool since rejected",
			params: []spec.Param{{Name: "since", Type: "bool"}},
		},
		{
			name:      "int64 since kept",
			params:    []spec.Param{{Name: "since", Type: "int64"}},
			wantParam: "since",
		},
		{
			name: "cookie body and positional filters lose to a query window",
			params: []spec.Param{
				{Name: "updated_at", Type: "string", Format: "date-time", In: "cookie"},
				{Name: "updatedAt", Type: "string", Format: "date-time", In: "body"},
				{Name: "modified_since", Type: "string", Format: "date-time", Positional: true},
				{Name: "after", Type: "string", Format: "date", In: "query"},
				{Name: "before", Type: "string", Format: "date", In: "query"},
			},
			wantParam:  "after",
			wantFormat: "date",
		},
		{
			name: "dateStart pairs with dateEnd",
			params: []spec.Param{
				{Name: "dateStart", Type: "string", Format: "date-time"},
				{Name: "dateEnd", Type: "string", Format: "date-time"},
			},
			wantParam:  "dateStart",
			wantFormat: "date-time",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, format := detectEndpointSinceParamAndFormat(spec.Endpoint{
				Params:     tc.params,
				Pagination: tc.pagination,
				Response:   tc.response,
			}, tc.types)
			assert.Equal(t, tc.wantParam, got)
			assert.Equal(t, tc.wantFormat, format)
		})
	}
}

func TestSinceValueFormatFromSpec(t *testing.T) {
	t.Parallel()

	naive := "Example 2014-02-01T10:00:00"
	cases := []struct {
		name   string
		param  spec.Param
		format string
	}{
		{name: "schema date", param: spec.Param{Format: "date"}, format: "date"},
		{name: "padded date-time", param: spec.Param{Format: " Date-Time "}, format: "date-time"},
		{
			name:   "schema date beats eastern prose",
			param:  spec.Param{Format: "date", Description: naive + " Eastern Time"},
			format: "date",
		},
		{
			name:   "operator keeps schema date-time",
			param:  spec.Param{Format: "date-time", Description: "Examples: >=2024-01-01"},
			format: "operator:>=:date-time",
		},
		{
			name:   "operator date when schema empty",
			param:  spec.Param{Description: "Examples: `>=2024-01-01`"},
			format: "operator:>=:date",
		},
		{
			name:   "timestamped operator example is not date",
			param:  spec.Param{Description: "Examples: >=2024-01-01T00:00:00"},
			format: "",
		},
		{
			name:   "rfc3339 words only",
			param:  spec.Param{Description: "Lower bound as a RFC3339 timestamp"},
			format: "",
		},
		{
			name:   "timestamp placeholder is not date",
			param:  spec.Param{Description: "Format YYYY-MM-DDTHH:mm:ssZ"},
			format: "",
		},
		{
			name:   "rfc3339 wording keeps the timestamp",
			param:  spec.Param{Description: "RFC3339 timestamp (YYYY-MM-DD)"},
			format: "",
		},
		{
			name:   "rfc3339 placeholder keeps the timestamp",
			param:  spec.Param{Description: "RFC3339 timestamp (YYYY-MM-DDTHH:mm:ssZ)"},
			format: "",
		},
		{
			name:   "iso8601 datetime wording keeps the timestamp",
			param:  spec.Param{Description: "ISO-8601 datetime (YYYY-MM-DD)"},
			format: "",
		},
		{
			name:   "yyyy-mm-dd hint",
			param:  spec.Param{Description: "Format YYYY-MM-DD"},
			format: "date",
		},
		{
			name:   "bare date",
			param:  spec.Param{Description: "Start 2014-02-01"},
			format: "date",
		},
		{
			name:   "forest is not est",
			param:  spec.Param{Description: naive + " in the forest"},
			format: "",
		},
		{
			name:   "largest is not est",
			param:  spec.Param{Description: "the largest " + naive},
			format: "",
		},
		{
			name:   "offset example blocks naive zone",
			param:  spec.Param{Description: "2011-06-03T10:00:00-07:00 Eastern Time"},
			format: "",
		},
		{
			name:   "zulu example blocks naive zone",
			param:  spec.Param{Description: "2011-06-03T10:00:00Z Eastern Time"},
			format: "",
		},
		{
			name:   "eastern before utc",
			param:  spec.Param{Description: naive + " Eastern Time (UTC-5)"},
			format: "local-datetime:America/New_York",
		},
	}
	zones := []struct {
		phrase string
		zone   string
	}{
		{"eastern time", "America/New_York"},
		{"est", "America/New_York"},
		{"edt", "America/New_York"},
		{"america/new_york", "America/New_York"},
		{"us/eastern", "America/New_York"},
		{"central time", "America/Chicago"},
		{"cst", "America/Chicago"},
		{"cdt", "America/Chicago"},
		{"america/chicago", "America/Chicago"},
		{"us/central", "America/Chicago"},
		{"mountain time", "America/Denver"},
		{"mst", "America/Denver"},
		{"mdt", "America/Denver"},
		{"america/denver", "America/Denver"},
		{"us/mountain", "America/Denver"},
		{"pacific time", "America/Los_Angeles"},
		{"pst", "America/Los_Angeles"},
		{"pdt", "America/Los_Angeles"},
		{"america/los_angeles", "America/Los_Angeles"},
		{"us/pacific", "America/Los_Angeles"},
		{"utc", "UTC"},
		{"gmt", "UTC"},
	}
	for _, zone := range zones {
		cases = append(cases, struct {
			name   string
			param  spec.Param
			format string
		}{
			name:   zone.phrase,
			param:  spec.Param{Description: naive + " " + zone.phrase},
			format: "local-datetime:" + zone.zone,
		})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.format, sinceValueFormat(tc.param))
		})
	}
}

func TestGenericStartModificationDescriptions(t *testing.T) {
	t.Parallel()

	for _, desc := range []string{
		"updated rows",
		"modified rows",
		"modification time",
		"last changed",
		"last change",
		"UPDATED rows",
	} {
		t.Run(desc, func(t *testing.T) {
			t.Parallel()
			got, format := detectEndpointSinceParamAndFormat(spec.Endpoint{
				Params: []spec.Param{{Name: "start", Type: "string", Description: desc}},
			}, nil)
			assert.Equal(t, "start", got)
			assert.Empty(t, format)
		})
	}
}

func TestDateWindowIsNotInferredCursor(t *testing.T) {
	t.Parallel()

	cursor, _ := inferPaginationParamsFromEndpoint(spec.Endpoint{Params: []spec.Param{
		{Name: "after", Type: "string", Format: "date", In: "query"},
		{Name: "before", Type: "string", Format: "date", In: "query"},
		{Name: "page_key", Type: "string", In: "query"},
	}})
	assert.Equal(t, "page_key", cursor)

	cursor, _ = inferPaginationParamsFromEndpoint(spec.Endpoint{Params: []spec.Param{
		{Name: "page_key", Type: "string", Format: "date", In: "query"},
	}})
	assert.Empty(t, cursor)

	cursor, _ = inferPaginationParamsFromEndpoint(spec.Endpoint{Params: []spec.Param{
		{Name: "page_key", Type: "string", In: "header"},
	}})
	assert.Empty(t, cursor)

	cursor, _ = inferPaginationParamsFromEndpoint(spec.Endpoint{Params: []spec.Param{
		{Name: "after", Type: "string", In: "query"},
	}})
	assert.Equal(t, "after", cursor)

	cursor, _ = inferPaginationParamsFromEndpoint(spec.Endpoint{Params: []spec.Param{
		{Name: "after", Type: "string", Format: "date", In: "query"},
		{Name: "page_token", Type: "string", In: "query"},
	}})
	assert.Equal(t, "page_token", cursor)

	cursor, _ = inferPaginationParamsFromEndpoint(spec.Endpoint{Params: []spec.Param{
		{Name: "after", Type: "string", Format: "date", In: "header"},
		{Name: "before", Type: "string", In: "header"},
	}})
	assert.Empty(t, cursor)
}

func TestProfileRanksSinceAgainstCursor(t *testing.T) {
	api := &spec.APISpec{
		Name: "rank",
		Resources: map[string]spec.Resource{
			"sales": {Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/sales",
					Response: spec.ResponseDef{Type: "array"},
					Params: []spec.Param{
						{Name: "after", Type: "string", Format: "date", In: "query"},
						{Name: "before", Type: "string", Format: "date", In: "query"},
						{Name: "page_key", Type: "string", In: "query"},
					},
				},
			}},
			"issues": {Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/issues",
					Response: spec.ResponseDef{Type: "array"},
					Params: []spec.Param{
						{Name: "start_date", Type: "string", Description: "Filter by start date. Examples: `>=2024-01-01`"},
						{Name: "updated_on", Type: "string", Description: "Filter by last updated date. Examples: `>=2024-01-01`"},
					},
				},
			}},
			"revisions": {Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/revisions",
					Response: spec.ResponseDef{Type: "array"},
					Pagination: &spec.Pagination{
						Type:        "cursor",
						CursorParam: "since",
						LimitParam:  "limit",
					},
					Params: []spec.Param{
						{Name: "since", Type: "string", Format: "date-time"},
						{Name: "updatedAt", Type: "string", Format: "date-time"},
						{Name: "limit", Type: "integer"},
					},
				},
			}},
			"cursors": {Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:   "GET",
					Path:     "/cursors",
					Response: spec.ResponseDef{Type: "array"},
					Pagination: &spec.Pagination{
						Type:        "cursor",
						CursorParam: "since",
						LimitParam:  "limit",
					},
					Params: []spec.Param{
						{Name: "since", Type: "string", Format: "date-time"},
						{Name: "limit", Type: "integer"},
					},
				},
			}},
		},
	}

	profile := Profile(api)
	byName := map[string]SyncableResource{}
	for _, resource := range profile.SyncableResources {
		byName[resource.Name] = resource
	}

	require.Contains(t, byName, "sales")
	assert.Equal(t, "after", byName["sales"].SinceParam)
	assert.Equal(t, "date", byName["sales"].SinceParamFormat)
	assert.Equal(t, "page_key", byName["sales"].PaginationCursorParam)

	require.Contains(t, byName, "issues")
	assert.Equal(t, "updated_on", byName["issues"].SinceParam)
	assert.Equal(t, "operator:>=:date", byName["issues"].SinceParamFormat)

	require.Contains(t, byName, "revisions")
	assert.Equal(t, "updatedAt", byName["revisions"].SinceParam)
	assert.Equal(t, "since", byName["revisions"].PaginationCursorParam)

	require.Contains(t, byName, "cursors")
	assert.Empty(t, byName["cursors"].SinceParam)
	assert.Equal(t, "since", byName["cursors"].PaginationCursorParam)
}
