package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunWorkflowVerification_NoManifest(t *testing.T) {
	dir := t.TempDir()

	report, err := RunWorkflowVerification(dir)
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.Equal(t, WorkflowVerdictPass, report.Verdict)
	require.Len(t, report.Issues, 1)
	assert.Contains(t, report.Issues[0], "no workflow manifest found")

	// Verify report was written to disk.
	loaded, err := LoadWorkflowVerifyReport(dir)
	require.NoError(t, err)
	assert.Equal(t, report.Verdict, loaded.Verdict)
}

func TestRunWorkflowVerification_NoCliName(t *testing.T) {
	dir := t.TempDir()

	// Write a manifest but no cmd/ directory.
	writeTestFile(t, filepath.Join(dir, "workflow_verify.yaml"), `workflows:
  - name: test flow
    steps:
      - command: hello
        mode: local
`)

	report, err := RunWorkflowVerification(dir)
	require.NoError(t, err)
	require.NotNil(t, report)

	assert.Equal(t, WorkflowVerdictFail, report.Verdict)
	require.NotEmpty(t, report.Issues)
	assert.Contains(t, report.Issues[0], "no CLI command directory found")
}

func TestSubstituteVars(t *testing.T) {
	tests := []struct {
		name string
		s    string
		vars map[string]string
		want string
	}{
		{
			name: "single variable",
			s:    "stores find ${store_id}",
			vars: map[string]string{"store_id": "123"},
			want: "stores find 123",
		},
		{
			name: "no variables",
			s:    "no vars here",
			vars: map[string]string{},
			want: "no vars here",
		},
		{
			name: "multiple variables",
			s:    "${a} and ${b}",
			vars: map[string]string{"a": "1", "b": "2"},
			want: "1 and 2",
		},
		{
			name: "nil vars map",
			s:    "hello ${world}",
			vars: nil,
			want: "hello ${world}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, substituteVars(tt.s, tt.vars))
		})
	}
}

func TestExtractJSONField(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		path    string
		want    string
		wantErr bool
	}{
		{
			name: "simple field",
			json: `{"name": "John"}`,
			path: "$.name",
			want: "John",
		},
		{
			name: "nested field",
			json: `{"data": {"id": "123"}}`,
			path: "$.data.id",
			want: "123",
		},
		{
			name: "array index",
			json: `{"items": [{"code": "abc"}]}`,
			path: "$.items[0].code",
			want: "abc",
		},
		{
			name: "numeric value",
			json: `{"count": 42}`,
			path: "$.count",
			want: "42",
		},
		{
			name:    "invalid path - missing field",
			json:    `{"name": "John"}`,
			path:    "$.missing",
			wantErr: true,
		},
		{
			name:    "invalid path - not an object",
			json:    `{"name": "John"}`,
			path:    "$.name.sub",
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			json:    `not json`,
			path:    "$.name",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractJSONField([]byte(tt.json), tt.path)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestDeriveWorkflowVerdict(t *testing.T) {
	tests := []struct {
		name  string
		steps []StepResult
		want  WorkflowVerdict
	}{
		{
			name: "all pass",
			steps: []StepResult{
				{Status: StepStatusPass},
				{Status: StepStatusPass},
			},
			want: WorkflowVerdictPass,
		},
		{
			name: "one fail-cli-bug",
			steps: []StepResult{
				{Status: StepStatusPass},
				{Status: StepStatusFailCLIBug},
			},
			want: WorkflowVerdictFail,
		},
		{
			name: "first step blocked-auth no pass",
			steps: []StepResult{
				{Status: StepStatusBlockedAuth},
				{Status: StepStatusSkippedAuth},
			},
			want: WorkflowVerdictUnverified,
		},
		{
			name: "auth blocked after a pass",
			steps: []StepResult{
				{Status: StepStatusPass},
				{Status: StepStatusBlockedAuth},
			},
			want: WorkflowVerdictPass,
		},
		{
			name:  "empty steps",
			steps: []StepResult{},
			want:  WorkflowVerdictPass,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, deriveWorkflowVerdict(tt.steps))
		})
	}
}

func TestLoadWorkflowVerifyReport(t *testing.T) {
	dir := t.TempDir()

	report := &WorkflowVerifyReport{
		Dir:     dir,
		Verdict: WorkflowVerdictPass,
		Workflows: []WorkflowResult{
			{
				Name:    "main flow",
				Primary: true,
				Verdict: WorkflowVerdictPass,
				Steps: []StepResult{
					{
						Command: "users list",
						Status:  StepStatusPass,
						Output:  `{"users": []}`,
					},
				},
			},
		},
		Issues: []string{"test issue"},
	}

	data, err := json.MarshalIndent(report, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflow-verify-report.json"), data, 0o644))

	loaded, err := LoadWorkflowVerifyReport(dir)
	require.NoError(t, err)
	assert.Equal(t, report.Dir, loaded.Dir)
	assert.Equal(t, report.Verdict, loaded.Verdict)
	require.Len(t, loaded.Workflows, 1)
	assert.Equal(t, "main flow", loaded.Workflows[0].Name)
	assert.True(t, loaded.Workflows[0].Primary)
	require.Len(t, loaded.Workflows[0].Steps, 1)
	assert.Equal(t, StepStatusPass, loaded.Workflows[0].Steps[0].Status)
	assert.Equal(t, []string{"test issue"}, loaded.Issues)
}

func TestLoadWorkflowVerifyReport_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadWorkflowVerifyReport(dir)
	assert.Error(t, err)
}
