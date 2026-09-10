package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkflowVerifyExitAfterReport(t *testing.T) {
	assert.Equal(t, "0,3", newWorkflowVerifyCmd().Annotations["pp:typed-exit-codes"])
	for _, fail := range []bool{false, true} {
		for _, asJSON := range []bool{false, true} {
			t.Run(fmt.Sprintf("fail=%t/json=%t", fail, asJSON), func(t *testing.T) {
				dir := t.TempDir()
				if fail {
					require.NoError(t, os.WriteFile(filepath.Join(dir, "workflow_verify.yaml"), []byte("workflows: []\n"), 0644))
				}
				cmd := newWorkflowVerifyCmd()
				args := []string{"--dir", dir}
				if asJSON {
					args = append(args, "--json")
				}
				cmd.SetArgs(args)
				out, err := runWithCapturedStdout(t, cmd.Execute)
				verdict := pipeline.WorkflowVerdictPass
				if fail {
					verdict = pipeline.WorkflowVerdictFail
					var exit *ExitError
					require.ErrorAs(t, err, &exit)
					assert.Equal(t, ExitGenerationError, exit.Code)
				} else {
					require.NoError(t, err)
				}
				assert.Contains(t, out, string(verdict))
				if asJSON {
					var report pipeline.WorkflowVerifyReport
					require.NoError(t, json.Unmarshal([]byte(out), &report))
					assert.Equal(t, verdict, report.Verdict)
				}
			})
		}
	}
}

func TestWorkflowVerifyUnverifiedRemainsSuccessful(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\n\ngo 1.26.6\n"), 0644))
	target := filepath.Join(dir, "cmd", "fixture-pp-cli")
	require.NoError(t, os.MkdirAll(target, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "main.go"), []byte("package main\nimport (\"fmt\";\"os\")\nfunc main() { fmt.Println(\"401 Unauthorized\"); os.Exit(1) }\n"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "workflow_verify.yaml"), []byte("workflows:\n- name: auth\n  primary: true\n  steps:\n  - command: list\n    mode: live\n    auth_required: true\n"), 0644))
	cmd := newWorkflowVerifyCmd()
	cmd.SetArgs([]string{"--dir", dir, "--json"})
	out, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err, out)
	var report pipeline.WorkflowVerifyReport
	require.NoError(t, json.Unmarshal([]byte(out), &report))
	assert.Equal(t, pipeline.WorkflowVerdictUnverified, report.Verdict)
}
