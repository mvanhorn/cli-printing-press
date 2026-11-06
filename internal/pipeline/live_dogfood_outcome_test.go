package pipeline

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLiveDogfoodStructuredOutcome(t *testing.T) {
	for _, tc := range []struct {
		body string
		want bool
	}{
		{`{"verdict":"NO-GO","paid":0,"target":1}`, true},
		{`{"error":"missing --go-no-go","code":2}`, false},
		{`{"success":false}`, false},
		{`{"meta":{},"results":{"verdict":"NO-GO","paid":0}}`, true},
		{`{"meta":{},"results":{"error":"missing target"}}`, false},
		{`{"meta":{},"results":{"success":false}}`, false},
		{`{"meta":{},"results":{}}`, false},
		{`{"meta":{},"results":null}`, false},
		{`{}`, false},
		{``, false},
		{`not JSON`, false},
	} {
		require.Equal(t, tc.want, liveDogfoodStructuredOutcome(liveDogfoodRun{stdout: tc.body, exitCode: 2}), tc.body)
	}
}

func TestRunLiveDogfoodCountsDeclaredStructuredOutcome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture is a shell script")
	}
	dir, binary := writeLiveDogfoodTypedExitFixture(t)
	filename := filepath.Join(dir, binary)
	script, err := os.ReadFile(filename)
	require.NoError(t, err)
	old := "echo 'no records supplied; nothing to verify' >&2"
	require.Contains(t, string(script), old)
	require.NoError(t, os.WriteFile(filename, []byte(strings.ReplaceAll(string(script), old, `echo '{"verdict":"NO-GO","paid":0,"target":1}'`)), 0o755))
	report, err := RunLiveDogfood(LiveDogfoodOptions{CLIDir: dir, BinaryName: binary, Level: "full", Timeout: 5 * time.Second})
	require.NoError(t, err)
	for _, kind := range []LiveDogfoodTestKind{LiveDogfoodTestHappy, LiveDogfoodTestJSON} {
		row := findResultByCommandKind(report, "records verify", kind)
		require.NotNil(t, row)
		require.Equal(t, LiveDogfoodStatusPass, row.Status)
		require.Equal(t, 2, row.ExitCode)
	}
}

func TestLiveDogfoodDisabledFeatureRequiresExplicitHTTPProof(t *testing.T) {
	for _, tc := range []struct {
		text string
		code int
		want bool
	}{
		{`HTTP 404: {"message":"Reviews are not enabled for this community"}`, 3, true},
		{`HTTP 403: {"message":"Analytics is not enabled for this account"}`, 3, true},
		{`HTTP 404: {"message":"Record not found"}`, 3, false},
		{`HTTP 500: {"message":"Reviews are not enabled for this community"}`, 3, false},
		{`Reviews are not enabled for this community`, 3, false},
		{`HTTP 404: {"message":"Reviews are not enabled for this community"}`, 0, false},
	} {
		require.Equal(t, tc.want, liveDogfoodFeatureAbsentFixtureReason(liveDogfoodRun{stderr: tc.text, exitCode: tc.code}) != "", tc.text)
	}
}

func TestRunLiveDogfoodSkipsDisabledFeatureAuthExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture is a shell script")
	}
	dir, binary := writeLiveDogfoodFeatureAbsentFixture(t)
	filename := filepath.Join(dir, binary)
	script, err := os.ReadFile(filename)
	require.NoError(t, err)
	old := `echo 'HTTP 404: {"error":"feature not enabled for this workspace"}' >&2
  exit 3`
	evidence := `HTTP 403: {"message":"Analytics is not enabled for this account"}`
	require.Contains(t, string(script), old)
	require.NoError(t, os.WriteFile(filename, []byte(strings.ReplaceAll(string(script), old, "echo '"+evidence+"' >&2\n  exit 4")), 0o755))

	report, err := RunLiveDogfood(LiveDogfoodOptions{CLIDir: dir, BinaryName: binary, Level: "full", Timeout: 5 * time.Second})
	require.NoError(t, err)
	require.Equal(t, "PASS", report.Verdict, report.Tests)
	for _, kind := range []LiveDogfoodTestKind{LiveDogfoodTestHappy, LiveDogfoodTestJSON} {
		row := findResultByCommandKind(report, "workspaces list", kind)
		require.NotNil(t, row)
		require.Equal(t, LiveDogfoodStatusSkip, row.Status)
		require.Equal(t, reasonFeatureAbsentFixture, row.Reason)
		require.Equal(t, 4, row.ExitCode)
		require.Contains(t, row.OutputSample, evidence)
	}
}
