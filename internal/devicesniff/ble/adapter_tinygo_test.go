//go:build !ble_replay_only && (darwin || linux || windows)

package ble

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRunWithContextReturnsResult(t *testing.T) {
	t.Parallel()

	got, err := runWithContext(context.Background(), func() (int, error) {
		return 42, nil
	})
	assert.NoError(t, err)
	assert.Equal(t, 42, got)
}

func TestRunWithContextPropagatesError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")
	got, err := runWithContext(context.Background(), func() (int, error) {
		return 0, sentinel
	})
	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, 0, got)
}

func TestRunWithContextCancelledBeforeCompletion(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := runWithContext(ctx, func() (int, error) {
		// Still running when the already-cancelled context wins the select.
		time.Sleep(20 * time.Millisecond)
		return 7, nil
	})
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, 0, got)
}

func TestMapLiveError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		wantNil  bool
		wantKind AdapterErrorKind
	}{
		{name: "nil passes through", err: nil, wantNil: true},
		{name: "context cancelled passes through verbatim", err: context.Canceled},
		{name: "deadline exceeded passes through verbatim", err: context.DeadlineExceeded},
		{name: "permission denied", err: errors.New("Permission denied by host"), wantKind: AdapterErrorPermissionDenied},
		{name: "device not found", err: errors.New("no such device"), wantKind: AdapterErrorDeviceNotFound},
		{name: "disconnected", err: errors.New("peer disconnected"), wantKind: AdapterErrorDisconnected},
		{name: "unrecognized falls back to unsupported", err: errors.New("gatt error 0x0e"), wantKind: AdapterErrorUnsupported},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mapLiveError(tc.err)
			if tc.wantNil {
				assert.NoError(t, got)
				return
			}
			if errors.Is(tc.err, context.Canceled) || errors.Is(tc.err, context.DeadlineExceeded) {
				assert.Equal(t, tc.err, got)
				return
			}
			var ae *AdapterError
			assert.True(t, errors.As(got, &ae), "expected *AdapterError, got %T", got)
			assert.Equal(t, tc.wantKind, ae.Kind)
		})
	}
}
