package generator

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realExitError returns a genuine *exec.ExitError (a process that started and
// exited non-zero), which cannot be constructed by hand.
func realExitError(t *testing.T) error {
	t.Helper()
	err := exec.Command("go", "this-is-not-a-go-subcommand").Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Skipf("could not obtain an *exec.ExitError on this host: %v", err)
	}
	return err
}

func TestClassifyExecError(t *testing.T) {
	// A localized OS start-failure message (Dutch WDAC block) — the exact
	// shape that slipped past the old English-substring classifier.
	localizedWDAC := &fs.PathError{Op: "fork/exec", Path: "x.exe", Err: errors.New("Dit bestand is geblokkeerd door een beleid voor toepassingsbeheer.")}
	wdacErrno := &fs.PathError{Op: "fork/exec", Path: "x.exe", Err: syscall.Errno(1260)}
	accessDenied := &fs.PathError{Op: "fork/exec", Path: "x.exe", Err: os.ErrPermission}

	cases := []struct {
		name        string
		err         error
		binExists   bool
		wantBlocked bool
	}{
		{"no error", nil, true, false},
		{"binary quarantined: gone after clean build", errors.New("anything"), false, true},
		{"start failure, localized WDAC message", localizedWDAC, true, true},
		{"start failure, WDAC errno 1260", wdacErrno, true, true},
		{"start failure, access denied", accessDenied, true, true},
		{"genuine non-zero exit (ran)", realExitError(t), true, false},
		{"ran cleanly but empty output", errors.New("demo-pp-cli --help produced no output"), true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blocked, reason := classifyExecError(tc.err, tc.binExists)
			assert.Equal(t, tc.wantBlocked, blocked)
			if blocked {
				assert.NotEmpty(t, reason, "a blocked verdict must carry an actionable reason")
			}
		})
	}

	// The WDAC errno sharpens the generic block reason.
	_, reason := classifyExecError(wdacErrno, true)
	assert.Contains(t, reason, "WDAC")
}

func TestValidationReportJSONShape(t *testing.T) {
	r := ValidationReport{
		Mode:           ModeSkipExec,
		ExecStatus:     ExecSkipped,
		Reason:         "validation-mode=skip-exec",
		ManualCommands: []string{"go build -o demo ./cmd/demo"},
	}
	b, err := json.Marshal(r)
	require.NoError(t, err)
	s := string(b)
	assert.Contains(t, s, `"mode":"skip-exec"`)
	assert.Contains(t, s, `"exec_status":"skipped"`)
	assert.Contains(t, s, `"manual_commands":["go build -o demo ./cmd/demo"]`)

	// reason and manual_commands are omitempty: a clean run carries neither.
	clean, err := json.Marshal(ValidationReport{Mode: ModeBinary, ExecStatus: ExecRan})
	require.NoError(t, err)
	assert.Equal(t, `{"mode":"binary","exec_status":"ran"}`, string(clean))
}

func TestManualValidationCommandsName(t *testing.T) {
	cmds := manualValidationCommands("/out", "/out/demo-pp-cli-validation.exe", "demo-pp-cli")
	require.NotEmpty(t, cmds)
	joined := strings.Join(cmds, "\n")
	assert.Contains(t, joined, "demo-pp-cli-validation.exe")
	assert.Contains(t, joined, "go build -o demo-pp-cli-validation.exe ./cmd/demo-pp-cli")
}
