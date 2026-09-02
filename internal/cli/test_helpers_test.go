package cli

import (
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runWithCapturedStdout executes fn while capturing os.Stdout via a pipe.
//
// The pipe is drained by a background goroutine that starts before fn runs, so
// fn never blocks on a full OS pipe buffer. Draining only after fn returned
// would deadlock once fn writes more than the buffer holds (notably smaller on
// Windows), so the reader must run concurrently with the writer.
func runWithCapturedStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	origStdout := os.Stdout
	os.Stdout = w

	outCh := make(chan string, 1)
	go func() {
		defer r.Close()
		out, _ := io.ReadAll(r)
		outCh <- string(out)
	}()

	stdoutRestored := false
	restoreStdout := func() {
		if stdoutRestored {
			return
		}
		w.Close()
		os.Stdout = origStdout
		stdoutRestored = true
	}
	defer restoreStdout()

	execErr := fn()
	restoreStdout()

	out := <-outCh
	return out, execErr
}

func runWithCapturedStderr(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	origStderr := os.Stderr
	os.Stderr = w

	errCh := make(chan string, 1)
	go func() {
		defer r.Close()
		out, _ := io.ReadAll(r)
		errCh <- string(out)
	}()

	stderrRestored := false
	restoreStderr := func() {
		if stderrRestored {
			return
		}
		w.Close()
		os.Stderr = origStderr
		stderrRestored = true
	}
	defer restoreStderr()

	execErr := fn()
	restoreStderr()

	out := <-errCh
	return out, execErr
}

// runWithCapturedStdoutAndStderr captures both streams concurrently so
// progress banners and JSON payloads can be asserted independently.
func runWithCapturedStdoutAndStderr(t *testing.T, fn func() error) (stdout string, stderr string, err error) {
	t.Helper()
	outR, outW, err := os.Pipe()
	require.NoError(t, err)
	errR, errW, err := os.Pipe()
	require.NoError(t, err)

	origStdout, origStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	outCh := make(chan string, 1)
	errCh := make(chan string, 1)
	go func() {
		defer outR.Close()
		b, _ := io.ReadAll(outR)
		outCh <- string(b)
	}()
	go func() {
		defer errR.Close()
		b, _ := io.ReadAll(errR)
		errCh <- string(b)
	}()

	restored := false
	restore := func() {
		if restored {
			return
		}
		_ = outW.Close()
		_ = errW.Close()
		os.Stdout, os.Stderr = origStdout, origStderr
		restored = true
	}
	defer restore()

	execErr := fn()
	restore()
	return <-outCh, <-errCh, execErr
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	out, err := runWithCapturedStdout(t, func() error {
		fn()
		return nil
	})
	require.NoError(t, err)
	return out
}

func TestRunWithCapturedStdoutRestoresStdoutAfterPanic(t *testing.T) {
	origStdout := os.Stdout
	var didPanic bool
	var restored bool

	func() {
		defer func() {
			didPanic = recover() != nil
			restored = os.Stdout == origStdout
			os.Stdout = origStdout
		}()

		_, _ = runWithCapturedStdout(t, func() error {
			panic("boom")
		})
	}()

	require.True(t, didPanic)
	assert.True(t, restored, "stdout should be restored while unwinding a panic")
}

func TestRunWithCapturedStderrRestoresStderrAfterPanic(t *testing.T) {
	origStderr := os.Stderr
	var didPanic bool
	var restored bool

	func() {
		defer func() {
			didPanic = recover() != nil
			restored = os.Stderr == origStderr
			os.Stderr = origStderr
		}()

		_, _ = runWithCapturedStderr(t, func() error {
			panic("boom")
		})
	}()

	require.True(t, didPanic)
	assert.True(t, restored, "stderr should be restored while unwinding a panic")
}
