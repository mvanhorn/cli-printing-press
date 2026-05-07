package generator

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
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
