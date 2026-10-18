package generator

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectAsyncJobs(t *testing.T) {
	tests := []struct {
		name       string
		spec       *spec.APISpec
		wantKey    string
		wantStatus string
		wantJobID  string
		wantCount  int
	}{
		{
			name: "POST + job_id response + sibling get: detected",
			spec: &spec.APISpec{
				Types: map[string]spec.TypeDef{
					"VideoJob": {Fields: []spec.TypeField{
						{Name: "job_id", Type: "string"},
						{Name: "status", Type: "string"},
					}},
				},
				Resources: map[string]spec.Resource{
					"videos": {
						Endpoints: map[string]spec.Endpoint{
							"create": {Method: "POST", Path: "/videos", Response: spec.ResponseDef{Type: "object", Item: "VideoJob"}},
							"get":    {Method: "GET", Path: "/videos/{id}", Response: spec.ResponseDef{Type: "object", Item: "VideoJob"}},
						},
					},
				},
			},
			wantKey:    "videos/create",
			wantStatus: "get",
			wantJobID:  "job_id",
			wantCount:  1,
		},
		{
			name: "POST + task_id response + _status sibling: detected",
			spec: &spec.APISpec{
				Types: map[string]spec.TypeDef{
					"TaskResp": {Fields: []spec.TypeField{{Name: "task_id", Type: "string"}}},
				},
				Resources: map[string]spec.Resource{
					"renders": {
						Endpoints: map[string]spec.Endpoint{
							"submit":        {Method: "POST", Path: "/renders", Response: spec.ResponseDef{Type: "object", Item: "TaskResp"}},
							"render_status": {Method: "GET", Path: "/renders/{id}/status"},
						},
					},
				},
			},
			wantKey:    "renders/submit",
			wantStatus: "render_status",
			wantJobID:  "task_id",
			wantCount:  1,
		},
		{
			name: "cross-resource: jobs resource holds status endpoint",
			spec: &spec.APISpec{
				Types: map[string]spec.TypeDef{
					"OpResp": {Fields: []spec.TypeField{{Name: "operation_id", Type: "string"}}},
				},
				Resources: map[string]spec.Resource{
					"transfers": {
						Endpoints: map[string]spec.Endpoint{
							"create": {Method: "POST", Path: "/transfers", Response: spec.ResponseDef{Type: "object", Item: "OpResp"}},
						},
					},
					"operations": {
						Endpoints: map[string]spec.Endpoint{
							"get": {Method: "GET", Path: "/operations/{id}"},
						},
					},
				},
			},
			wantKey:    "transfers/create",
			wantStatus: "get",
			wantJobID:  "operation_id",
			wantCount:  1,
		},
		{
			name: "POST with id-field but no sibling: NOT detected (status is load-bearing)",
			spec: &spec.APISpec{
				Types: map[string]spec.TypeDef{
					"R": {Fields: []spec.TypeField{{Name: "job_id", Type: "string"}}},
				},
				Resources: map[string]spec.Resource{
					"things": {
						Endpoints: map[string]spec.Endpoint{
							"create": {Method: "POST", Path: "/things", Response: spec.ResponseDef{Type: "object", Item: "R"}},
						},
					},
				},
			},
			wantCount: 0,
		},
		{
			name: "GET with job_id-field and sibling: detected (field+sibling both fire)",
			spec: &spec.APISpec{
				Types: map[string]spec.TypeDef{
					"R": {Fields: []spec.TypeField{{Name: "job_id", Type: "string"}}},
				},
				Resources: map[string]spec.Resource{
					"things": {
						Endpoints: map[string]spec.Endpoint{
							"list": {Method: "GET", Path: "/things", Response: spec.ResponseDef{Type: "array", Item: "R"}},
							"get":  {Method: "GET", Path: "/things/{id}"},
						},
					},
				},
			},
			wantKey:    "things/list",
			wantStatus: "get",
			wantJobID:  "job_id",
			wantCount:  1,
		},
		{
			name: "plain CRUD POST without id-shaped field: NOT detected",
			spec: &spec.APISpec{
				Types: map[string]spec.TypeDef{
					"User": {Fields: []spec.TypeField{
						{Name: "id", Type: "string"},
						{Name: "name", Type: "string"},
					}},
				},
				Resources: map[string]spec.Resource{
					"users": {
						Endpoints: map[string]spec.Endpoint{
							"create": {Method: "POST", Path: "/users", Response: spec.ResponseDef{Type: "object", Item: "User"}},
							"get":    {Method: "GET", Path: "/users/{id}"},
						},
					},
				},
			},
			wantCount: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectAsyncJobs(tc.spec)
			assert.Len(t, got, tc.wantCount, "detection count")
			if tc.wantKey != "" {
				info, ok := got[tc.wantKey]
				assert.True(t, ok, "expected detection at %q", tc.wantKey)
				assert.Equal(t, tc.wantStatus, info.StatusEndpoint, "status endpoint")
				assert.Equal(t, tc.wantJobID, info.JobIDField, "job id field")
			}
		})
	}
}

func TestDetectAsyncJobs_NilSpec(t *testing.T) {
	assert.Empty(t, DetectAsyncJobs(nil))
}

func TestResponseJobIDField_VariousNames(t *testing.T) {
	cases := []struct {
		field  string
		expect bool
	}{
		{"job_id", true},
		{"jobId", true},
		{"task_id", true},
		{"operation_id", true},
		{"request_id", true},
		{"async_id", true},
		{"run_id", true},
		{"batch_id", true},
		{"id", false},
		{"user_id", false},
		{"jobs", false},
	}
	for _, c := range cases {
		s := &spec.APISpec{
			Types: map[string]spec.TypeDef{
				"T": {Fields: []spec.TypeField{{Name: c.field, Type: "string"}}},
			},
		}
		ep := spec.Endpoint{Response: spec.ResponseDef{Item: "T"}}
		got := responseJobIDField(s, ep)
		if c.expect {
			assert.Equal(t, c.field, got, "should match: %s", c.field)
		} else {
			assert.Empty(t, got, "should not match: %s", c.field)
		}
	}
}

// statusSiblingStabilityRuns is large enough that Go's per-range map
// randomization fails a test that still returns the first qualifying sibling.
const statusSiblingStabilityRuns = 100

func TestDetectAsyncJobsStatusSiblingStable(t *testing.T) {
	t.Parallel()

	t.Run("cross-resource", func(t *testing.T) {
		t.Parallel()
		s := ambiguousAsyncStatusSpec()
		want := AsyncJobInfo{
			ResourceName:         "events",
			EndpointName:         "get",
			JobIDField:           "task_id",
			StatusResource:       "job-tasks",
			StatusEndpoint:       "get",
			StatusPath:           "/job-tasks/{id}",
			StatusCommand:        "job-tasks get",
			StatusRecoveryPrefix: "job-tasks get ",
			TerminalField:        "status",
			TerminalValues:       terminalValueDefaults,
		}
		for i := range statusSiblingStabilityRuns {
			got := DetectAsyncJobs(s)
			require.Lenf(t, got, 1, "run %d", i)
			require.Equalf(t, want, got["events/get"], "run %d", i)
		}
	})

	t.Run("same-resource", func(t *testing.T) {
		t.Parallel()
		s := ambiguousSameResourceStatusSpec()
		want := AsyncJobInfo{
			ResourceName:         "renders",
			EndpointName:         "submit",
			JobIDField:           "task_id",
			StatusResource:       "renders",
			StatusEndpoint:       "submit_status",
			StatusPath:           "/renders/{id}/status",
			StatusCommand:        "renders submit-status",
			StatusRecoveryPrefix: "renders submit-status ",
			TerminalField:        "status",
			TerminalValues:       terminalValueDefaults,
		}
		for i := range statusSiblingStabilityRuns {
			got := DetectAsyncJobs(s)
			require.Lenf(t, got, 1, "run %d", i)
			require.Equalf(t, want, got["renders/submit"], "run %d", i)
		}
	})
}

func TestFindStatusSiblingRanking(t *testing.T) {
	t.Parallel()

	get := func(path string) spec.Endpoint {
		return spec.Endpoint{Method: "GET", Path: path}
	}
	cases := []struct {
		name     string
		spec     *spec.APISpec
		resource string
		endpoint string
		wantRes  string
		wantEP   string
	}{
		{
			name: "exact resource name beats lexical order and a better probe",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"events":        {Endpoints: map[string]spec.Endpoint{"create": {Method: "POST", Path: "/events"}}},
				"aaa-jobs":      {Endpoints: map[string]spec.Endpoint{"get": get("/aaa-jobs/{id}")}},
				"events-status": {Endpoints: map[string]spec.Endpoint{"fetch-status": get("/events/{id}/status")}},
			}},
			resource: "events", endpoint: "create",
			wantRes: "events-status", wantEP: "fetch-status",
		},
		{
			name: "marker prefix is an exact resource name",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"events":        {Endpoints: map[string]spec.Endpoint{"create": {Method: "POST", Path: "/events"}}},
				"aaa-jobs":      {Endpoints: map[string]spec.Endpoint{"get": get("/aaa-jobs/{id}")}},
				"status-events": {Endpoints: map[string]spec.Endpoint{"get": get("/status/events/{id}")}},
			}},
			resource: "events", endpoint: "create",
			wantRes: "status-events", wantEP: "get",
		},
		{
			name: "exact full name beats exact singular stem",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"events":        {Endpoints: map[string]spec.Endpoint{"create": {Method: "POST", Path: "/events"}}},
				"event-jobs":    {Endpoints: map[string]spec.Endpoint{"get": get("/event-jobs/{id}")}},
				"events-status": {Endpoints: map[string]spec.Endpoint{"get": get("/events/{id}/status")}},
			}},
			resource: "events", endpoint: "create",
			wantRes: "events-status", wantEP: "get",
		},
		{
			name: "exact singular stem beats a prefixed full-name resource",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"events":           {Endpoints: map[string]spec.Endpoint{"create": {Method: "POST", Path: "/events"}}},
				"aaa-events-tasks": {Endpoints: map[string]spec.Endpoint{"get": get("/aaa-events-tasks/{id}")}},
				"event-jobs":       {Endpoints: map[string]spec.Endpoint{"get": get("/event-jobs/{id}")}},
			}},
			resource: "events", endpoint: "create",
			wantRes: "event-jobs", wantEP: "get",
		},
		{
			name: "full-name token beats an unrelated lexical winner",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"events":            {Endpoints: map[string]spec.Endpoint{"create": {Method: "POST", Path: "/events"}}},
				"aaa-jobs":          {Endpoints: map[string]spec.Endpoint{"get": get("/aaa-jobs/{id}")}},
				"team-events-tasks": {Endpoints: map[string]spec.Endpoint{"get": get("/team-events-tasks/{id}")}},
			}},
			resource: "events", endpoint: "create",
			wantRes: "team-events-tasks", wantEP: "get",
		},
		{
			name: "unrelated resources tie-break lexically",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"events":     {Endpoints: map[string]spec.Endpoint{"create": {Method: "POST", Path: "/events"}}},
				"sync-tasks": {Endpoints: map[string]spec.Endpoint{"get": get("/sync-tasks/{id}")}},
				"job-tasks":  {Endpoints: map[string]spec.Endpoint{"get": get("/job-tasks/{id}")}},
			}},
			resource: "events", endpoint: "create",
			wantRes: "job-tasks", wantEP: "get",
		},
		{
			name: "exact get beats a lexically earlier substring probe",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"events": {Endpoints: map[string]spec.Endpoint{"create": {Method: "POST", Path: "/events"}}},
				"job-tasks": {Endpoints: map[string]spec.Endpoint{
					"fetch-status": get("/job-tasks/{id}/status"),
					"get":          get("/job-tasks/{id}"),
				}},
			}},
			resource: "events", endpoint: "create",
			wantRes: "job-tasks", wantEP: "get",
		},
		{
			name: "substring probe order beats lexical endpoint names",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"events": {Endpoints: map[string]spec.Endpoint{"create": {Method: "POST", Path: "/events"}}},
				"jobs": {Endpoints: map[string]spec.Endpoint{
					"aaa-show":     get("/jobs/{id}/show"),
					"fetch-status": get("/jobs/{id}/status"),
				}},
			}},
			resource: "events", endpoint: "create",
			wantRes: "jobs", wantEP: "fetch-status",
		},
		{
			name: "equal substring probes tie-break on endpoint name",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"events": {Endpoints: map[string]spec.Endpoint{"create": {Method: "POST", Path: "/events"}}},
				"jobs": {Endpoints: map[string]spec.Endpoint{
					"get-task": get("/jobs/{id}/task"),
					"get-job":  get("/jobs/{id}/job"),
				}},
			}},
			resource: "events", endpoint: "create",
			wantRes: "jobs", wantEP: "get-job",
		},
		{
			name: "same-resource get beats a named sibling and a cross-resource candidate",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"renders": {Endpoints: map[string]spec.Endpoint{
					"create":        {Method: "POST", Path: "/renders"},
					"get":           get("/renders/{id}"),
					"create_status": get("/renders/{id}/status"),
				}},
				"events-status": {Endpoints: map[string]spec.Endpoint{"get": get("/events-status/{id}")}},
			}},
			resource: "renders", endpoint: "create",
			wantRes: "renders", wantEP: "get",
		},
		{
			name: "same-resource retrieve beats an exact cross-resource name",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"events": {Endpoints: map[string]spec.Endpoint{
					"create":   {Method: "POST", Path: "/events"},
					"retrieve": get("/events/{id}"),
				}},
				"events-status": {Endpoints: map[string]spec.Endpoint{"get": get("/events-status/{id}")}},
			}},
			resource: "events", endpoint: "create",
			wantRes: "events", wantEP: "retrieve",
		},
		{
			name: "same-resource status beats a named sibling when get is the submitter",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"renders": {Endpoints: map[string]spec.Endpoint{
					"get":        get("/renders/{id}"),
					"status":     get("/renders/{id}/status"),
					"get_status": get("/renders/{id}/named"),
				}},
			}},
			resource: "renders", endpoint: "get",
			wantRes: "renders", wantEP: "status",
		},
		{
			name: "same-resource compound name beats a generic status name",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"renders": {Endpoints: map[string]spec.Endpoint{
					"submit_render":      {Method: "POST", Path: "/renders"},
					"submit_render_poll": get("/renders/{id}/poll"),
					"aaa_status":         get("/renders/{id}/status"),
				}},
			}},
			resource: "renders", endpoint: "submit_render",
			wantRes: "renders", wantEP: "submit_render_poll",
		},
		{
			name: "same-resource hyphenated compound name is separator-bounded",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"renders": {Endpoints: map[string]spec.Endpoint{
					"submit-render":      {Method: "POST", Path: "/renders"},
					"submit-render-poll": get("/renders/{id}/poll"),
					"aaa_status":         get("/renders/{id}/status"),
				}},
			}},
			resource: "renders", endpoint: "submit-render",
			wantRes: "renders", wantEP: "submit-render-poll",
		},
		{
			name: "same-resource partial compound does not count as a name match",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"renders": {Endpoints: map[string]spec.Endpoint{
					"submit_render":        {Method: "POST", Path: "/renders"},
					"submit_renderer_poll": get("/renders/{id}/poll"),
					"aaa_status":           get("/renders/{id}/status"),
				}},
			}},
			resource: "renders", endpoint: "submit_render",
			wantRes: "renders", wantEP: "aaa_status",
		},
		{
			name: "cross-resource compound name beats an unrelated better probe",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"order_events": {Endpoints: map[string]spec.Endpoint{"create": {Method: "POST", Path: "/order-events"}}},
				"aaa-jobs":     {Endpoints: map[string]spec.Endpoint{"get": get("/aaa-jobs/{id}")}},
				"backup-order-events-tasks": {Endpoints: map[string]spec.Endpoint{
					"fetch-status": get("/backup/{id}/status"),
				}},
			}},
			resource: "order_events", endpoint: "create",
			wantRes: "backup-order-events-tasks", wantEP: "fetch-status",
		},
		{
			name: "same-resource token match beats a generic status name",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"renders": {Endpoints: map[string]spec.Endpoint{
					"submit":      {Method: "POST", Path: "/renders"},
					"submit_poll": get("/renders/{id}/poll"),
					"zzz_status":  get("/renders/{id}/zzz"),
					"aaa_poll":    get("/renders/{id}/aaa"),
				}},
			}},
			resource: "renders", endpoint: "submit",
			wantRes: "renders", wantEP: "submit_poll",
		},
		{
			name: "same-resource status beats poll when neither names the submitter",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"renders": {Endpoints: map[string]spec.Endpoint{
					"submit":     {Method: "POST", Path: "/renders"},
					"zzz_status": get("/renders/{id}/zzz"),
					"aaa_poll":   get("/renders/{id}/aaa"),
				}},
			}},
			resource: "renders", endpoint: "submit",
			wantRes: "renders", wantEP: "zzz_status",
		},
		{
			name: "same-resource status names tie-break lexically",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"renders": {Endpoints: map[string]spec.Endpoint{
					"submit":     {Method: "POST", Path: "/renders"},
					"mmm_status": get("/renders/{id}/mmm"),
					"aaa_status": get("/renders/{id}/aaa"),
				}},
			}},
			resource: "renders", endpoint: "submit",
			wantRes: "renders", wantEP: "aaa_status",
		},
		{
			name: "single cross-resource sibling is unchanged",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"transfers":  {Endpoints: map[string]spec.Endpoint{"create": {Method: "POST", Path: "/transfers"}}},
				"operations": {Endpoints: map[string]spec.Endpoint{"get": get("/operations/{id}")}},
			}},
			resource: "transfers", endpoint: "create",
			wantRes: "operations", wantEP: "get",
		},
		{
			name: "no qualifying sibling",
			spec: &spec.APISpec{Resources: map[string]spec.Resource{
				"things":  {Endpoints: map[string]spec.Endpoint{"create": {Method: "POST", Path: "/things"}}},
				"widgets": {Endpoints: map[string]spec.Endpoint{"list": get("/widgets")}},
			}},
			resource: "things", endpoint: "create",
			wantRes: "", wantEP: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for i := range statusSiblingStabilityRuns {
				gotRes, gotEP := findStatusSibling(tc.spec, tc.resource, tc.endpoint)
				require.Equalf(t, tc.wantRes, gotRes, "run %d", i)
				require.Equalf(t, tc.wantEP, gotEP, "run %d", i)
			}
		})
	}
}

func TestGenerateAmbiguousAsyncStatusOutputStable(t *testing.T) {
	t.Parallel()

	const runs = 12
	var baseline map[string][]byte
	for i := range runs {
		dir := filepath.Join(t.TempDir(), "cli")
		require.NoError(t, New(ambiguousAsyncStatusSpec(), dir).Generate())
		body := readGeneratedFile(t, dir, "internal", "cli", "events_get.go")
		require.Contains(t, body, `StatusResource: "job-tasks"`)
		require.Contains(t, body, `StatusEndpoint: "get"`)
		require.Contains(t, body, `"/job-tasks/{id}"`)
		require.Contains(t, body, `"events-pp-cli job-tasks get "`)
		require.NotContains(t, body, "sync-tasks")
		require.NotContains(t, body, "fetch-status")
		require.NotContains(t, body, "aaa-items")
		tree := snapshotGeneratedTree(t, dir)
		if i == 0 {
			baseline = tree
			continue
		}
		requireIdenticalTrees(t, baseline, tree)
	}
}

func ambiguousAsyncStatusSpec() *spec.APISpec {
	s := minimalSpec("events")
	s.Types = map[string]spec.TypeDef{
		"Job": {Fields: []spec.TypeField{{Name: "task_id", Type: "string"}}},
	}
	s.Resources = map[string]spec.Resource{
		"events": {
			Description: "Events",
			Endpoints: map[string]spec.Endpoint{
				"get": {
					Method:      "GET",
					Path:        "/events/{id}",
					Description: "Fetch an event",
					Response:    spec.ResponseDef{Type: "object", Item: "Job"},
				},
				"list": {Method: "GET", Path: "/events", Description: "List events"},
			},
		},
		"aaa-items": {
			Description: "Items",
			Endpoints: map[string]spec.Endpoint{
				"get": {Method: "GET", Path: "/items/{id}", Description: "Get an item"},
			},
		},
		"job-tasks": {
			Description: "Job tasks",
			Endpoints: map[string]spec.Endpoint{
				"fetch-status": {Method: "GET", Path: "/job-tasks/{id}/status", Description: "Fetch task status"},
				"get": {
					Method:      "GET",
					Path:        "/job-tasks/{id}",
					Description: "Get a job task",
					Params: []spec.Param{{
						Name: "id", Type: "string", Required: true, Positional: true, PathParam: true,
					}},
				},
			},
		},
		"sync-tasks": {
			Description: "Sync tasks",
			Endpoints: map[string]spec.Endpoint{
				"get": {Method: "GET", Path: "/sync-tasks/{id}", Description: "Get a sync task"},
			},
		},
	}
	return s
}

func ambiguousSameResourceStatusSpec() *spec.APISpec {
	s := minimalSpec("renders")
	s.Types = map[string]spec.TypeDef{
		"Job": {Fields: []spec.TypeField{{Name: "task_id", Type: "string"}}},
	}
	s.Resources = map[string]spec.Resource{
		"renders": {
			Endpoints: map[string]spec.Endpoint{
				"submit": {
					Method:   "POST",
					Path:     "/renders",
					Response: spec.ResponseDef{Type: "object", Item: "Job"},
				},
				"aaa_poll":   {Method: "GET", Path: "/renders/{id}/poll"},
				"zzz_status": {Method: "GET", Path: "/renders/{id}/zzz"},
				"submit_status": {
					Method: "GET",
					Path:   "/renders/{id}/status",
					Params: []spec.Param{{
						Name: "id", Type: "string", Required: true, Positional: true, PathParam: true,
					}},
				},
			},
		},
		"aaa-jobs": {
			Endpoints: map[string]spec.Endpoint{
				"get": {Method: "GET", Path: "/aaa-jobs/{id}"},
			},
		},
	}
	return s
}

func snapshotGeneratedTree(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	tree := map[string][]byte{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		tree[rel] = body
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, tree)
	return tree
}

func requireIdenticalTrees(t *testing.T, want, got map[string][]byte) {
	t.Helper()
	var diffs []string
	seen := make(map[string]struct{}, len(want))
	for path, body := range want {
		seen[path] = struct{}{}
		other, ok := got[path]
		if !ok {
			diffs = append(diffs, path+" missing")
			continue
		}
		if !bytes.Equal(body, other) {
			diffs = append(diffs, path)
		}
	}
	for path := range got {
		if _, ok := seen[path]; !ok {
			diffs = append(diffs, path+" extra")
		}
	}
	sort.Strings(diffs)
	require.Empty(t, diffs)
}
