//go:build !windows

package pipeline

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunStdoutOnlyRetriesTextFileBusy(t *testing.T) {
	attempts := 0
	out, err := runStdoutOnlyWithRunner(2*time.Second, func(_ context.Context) ([]byte, error) {
		attempts++
		if attempts < 3 {
			return nil, &os.PathError{Op: "fork/exec", Path: "fakebin", Err: syscall.ETXTBSY}
		}
		return []byte(`{"commands":[]}`), nil
	})
	require.NoError(t, err)
	assert.Contains(t, string(out), `"commands"`)
	assert.Equal(t, 3, attempts)
}
