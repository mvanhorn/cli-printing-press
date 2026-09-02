package mcpsync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasHandAuthoredMCPBehavior(t *testing.T) {
	t.Parallel()

	assert.True(t, hasHandAuthoredMCPBehavior([]byte("func waitForIntentPoll() {}")))
	assert.True(t, hasHandAuthoredMCPBehavior([]byte("localWrite := isMCPLocalWrite(cmd)")))
	assert.True(t, hasHandAuthoredMCPBehavior([]byte("\t\"db\":           true,\n")))
	assert.True(t, hasHandAuthoredMCPBehavior([]byte("ReadHeaderTimeout: 10 * time.Second,")))
	assert.True(t, hasHandAuthoredMCPBehavior([]byte("func isMCPExecutionReadOnly(cmd *cobra.Command) bool { return false }")))
	assert.False(t, hasHandAuthoredMCPBehavior([]byte("if !readOnly && isMCPLocalWrite(cmd) {")))
	assert.False(t, hasHandAuthoredMCPBehavior([]byte("var blockedRootFlags = map[string]bool{\"token\": true}")))
}

func TestMergeHandAuthoredIntentFileKeepsPollHelpers(t *testing.T) {
	t.Parallel()

	before := []byte(`package mcp

import (
	"context"
	"fmt"
	"time"
)

func handleWaitForOperation(ctx context.Context) error {
	_, err := waitForIntentPoll(ctx, pollAfter, operationTimeout)
	return err
}

func callIntentEndpoint(ctx context.Context) (any, error) { return nil, nil }

const (
	pollAfter        = 200 * time.Millisecond
	operationTimeout = 5 * time.Second
)

func intentDuration(d *time.Duration) time.Duration {
	if d == nil {
		return 0
	}
	return *d
}

func waitForIntentPoll(ctx context.Context, pollAfter, operationTimeout time.Duration) (any, error) {
	_ = intentDuration(&operationTimeout)
	if !intentPollComplete(nil) {
		return nil, fmt.Errorf("unfinished")
	}
	return callIntentEndpoint(ctx)
}

func intentPollComplete(resp any) bool { return resp != nil }
`)
	after := []byte(`package mcp

import (
	"context"
	"fmt"
)

func handleWaitForOperation(ctx context.Context) error {
	_, err := callIntentEndpoint(ctx)
	return err
}

func callIntentEndpoint(ctx context.Context) (any, error) { return nil, nil }
`)

	merged, err := mergeHandAuthoredIntentFile(before, after)
	require.NoError(t, err)
	src := string(merged)
	assert.Contains(t, src, "func waitForIntentPoll(")
	assert.Contains(t, src, "func intentDuration(")
	assert.Contains(t, src, "func intentPollComplete(")
	assert.Contains(t, src, "pollAfter")
	assert.Contains(t, src, "operationTimeout")
	assert.Contains(t, src, "waitForIntentPoll(ctx, pollAfter, operationTimeout)")
	assert.Contains(t, src, `"time"`)
	assert.NotContains(t, src, "_, err := callIntentEndpoint(ctx)")
}

func TestMergeHandAuthoredIntentFileNoopsWithoutMarkers(t *testing.T) {
	t.Parallel()

	before := []byte("package mcp\nfunc handleOld() {}\n")
	after := []byte("package mcp\nfunc handleNew() {}\n")
	merged, err := mergeHandAuthoredIntentFile(before, after)
	require.NoError(t, err)
	assert.Equal(t, string(after), string(merged))
}
