package mcpsync

import (
	"go/parser"
	"go/token"
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

func TestIsGeneratedIntentHandler(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "generated mcp tool handler",
			src: `package mcp
func handleCreateOperation(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return nil, nil
}`,
			want: true,
		},
		{
			name: "hand-authored handle helper",
			src: `package mcp
func handleCustomPoll(ctx context.Context, c *client.Client, opID string) (bool, error) {
	return false, nil
}`,
			want: false,
		},
		{
			name: "handle prefix without tool signature",
			src: `package mcp
func handleOldExplicit(ctx context.Context) error { return nil }`,
			want: false,
		},
		{
			name: "non-handle helper",
			src: `package mcp
func waitForIntentPoll(ctx context.Context) error { return nil }`,
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "intents.go", tc.src, 0)
			require.NoError(t, err)
			require.NotEmpty(t, file.Decls)
			assert.Equal(t, tc.want, isGeneratedIntentHandler(file.Decls[0]))
		})
	}
}

func TestMergeHandAuthoredIntentFileDropsRemovedGeneratedHandlers(t *testing.T) {
	t.Parallel()

	before := []byte(`package mcp

import (
	"context"
	"fmt"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func handleOldExplicit(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	_, err := waitForIntentPoll(ctx, pollAfter, operationTimeout)
	return nil, err
}

func handleWaitForOperation(ctx context.Context) error {
	_, err := waitForIntentPoll(ctx, pollAfter, operationTimeout)
	return err
}

func callIntentEndpoint(ctx context.Context) (any, error) { return nil, nil }

const (
	pollAfter        = 200 * time.Millisecond
	operationTimeout = 5 * time.Second
)

func intentDuration(d *time.Duration) time.Duration { return 0 }

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

func handleNewExplicit(ctx context.Context) error {
	_, err := callIntentEndpoint(ctx)
	return err
}

func handleWaitForOperation(ctx context.Context) error {
	_, err := callIntentEndpoint(ctx)
	return err
}

func callIntentEndpoint(ctx context.Context) (any, error) { return nil, nil }
`)

	merged, err := mergeHandAuthoredIntentFile(before, after)
	require.NoError(t, err)
	src := string(merged)
	assert.Contains(t, src, "func handleNewExplicit(")
	assert.NotContains(t, src, "func handleOldExplicit(", "removed generated handlers must not be re-appended")
	assert.Contains(t, src, "func waitForIntentPoll(")
	assert.Contains(t, src, "func intentPollComplete(")
	assert.Contains(t, src, "waitForIntentPoll(ctx, pollAfter, operationTimeout)")
}

func TestMergeHandAuthoredIntentFileKeepsCustomHandleHelper(t *testing.T) {
	t.Parallel()

	before := []byte(`package mcp

import (
	"context"
	"fmt"
	"time"

	mcplib "github.com/mark3labs/mcp-go/mcp"

	"github.com/example/fixture-pp-cli/internal/client"
)

func handleCreateOperation(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	_, err := waitForIntentPoll(ctx, nil, "op")
	return nil, err
}

func handleCustomPoll(ctx context.Context, c *client.Client, opID string) (bool, error) {
	return waitForIntentPoll(ctx, c, opID)
}

func handleOldExplicit(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	_, err := waitForIntentPoll(ctx, nil, "gone")
	return nil, err
}

func waitForIntentPoll(ctx context.Context, c *client.Client, opID string) (any, error) {
	_ = operationTimeout
	return nil, fmt.Errorf("unfinished")
}

const operationTimeout = 5 * time.Second
`)
	after := []byte(`package mcp

import (
	"context"

	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func handleCreateOperation(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	return nil, nil
}
`)

	merged, err := mergeHandAuthoredIntentFile(before, after)
	require.NoError(t, err)
	src := string(merged)
	assert.Contains(t, src, "func handleCustomPoll(")
	assert.Contains(t, src, "func waitForIntentPoll(")
	assert.Contains(t, src, "func handleCreateOperation(")
	assert.Contains(t, src, `waitForIntentPoll(ctx, nil, "op")`)
	assert.NotContains(t, src, "func handleOldExplicit(")
}
