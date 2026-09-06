package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/stretchr/testify/require"
)

func TestGeneratedMCPClientConstructorAppliesClientHooks(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("client-hooks-mcp")
	cliName := naming.CLI(apiSpec.Name)
	outputDir := filepath.Join(t.TempDir(), cliName)
	require.NoError(t, New(apiSpec, outputDir).Generate())

	rootSrc := readGeneratedFile(t, outputDir, "internal", "cli", "root.go")
	require.Contains(t, rootSrc, "func ApplyClientHooks(c *client.Client) error")
	require.Contains(t, rootSrc, "func registerClientHook(hook func(*client.Client) error)")
	require.Contains(t, rootSrc, "if err := ApplyClientHooks(c); err != nil {")
	require.NotContains(t, rootSrc, "registerClientHookFor")
	require.NotContains(t, rootSrc, "clientHookSurface")

	toolsSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	require.Contains(t, toolsSrc, "func newMCPClientFromConfig(ctx context.Context, cfg *config.Config) (*client.Client, *platform.Session, error)")
	require.Contains(t, toolsSrc, "if err := cli.ApplyClientHooks(c); err != nil {")
	require.Contains(t, toolsSrc, "session.ZeroCredentials()")
	require.Contains(t, toolsSrc, "return newMCPClientFromConfig(ctx, cfg)")
	require.NotContains(t, toolsSrc, "func newMCPClientFromConfig(cfg *config.Config) *client.Client")

	requireGeneratedCompiles(t, outputDir)
}

func TestGeneratedMCPClientRunsPreservedHooksAndStopsBeforeHTTP(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("client-hooks-mcp")
	cliName := naming.CLI(apiSpec.Name)
	outputDir := filepath.Join(t.TempDir(), cliName)
	require.NoError(t, New(apiSpec, outputDir).Generate())

	cliFixture := `package cli

import "` + cliName + `/internal/client"

func ResetClientHooksForTest() {
	clientHooks = nil
}

func RegisterClientHookForTest(hook func(*client.Client) error) {
	registerClientHook(hook)
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "cli", "client_hook_fixture.go"),
		[]byte(cliFixture),
		0o644,
	))

	mcpTest := `package mcp

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"` + cliName + `/internal/cli"
	"` + cliName + `/internal/client"
	"` + cliName + `/internal/config"
	"` + cliName + `/internal/platform"
)

func TestMCPClientAppliesPreservedClientHooks(t *testing.T) {
	cli.ResetClientHooksForTest()
	t.Cleanup(cli.ResetClientHooksForTest)

	var calls atomic.Int32
	cli.RegisterClientHookForTest(func(*client.Client) error {
		calls.Add(1)
		return nil
	})

	c, session, err := newMCPClientFromConfig(context.Background(), &config.Config{})
	if err != nil {
		t.Fatalf("newMCPClientFromConfig() error = %v", err)
	}
	if c == nil {
		t.Fatal("newMCPClientFromConfig() client is nil")
	}
	if session != nil {
		t.Fatal("newMCPClientFromConfig() unexpected platform session")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("preserved client hook calls = %d, want 1", got)
	}
}

func TestMCPClientAllowsNoClientHooks(t *testing.T) {
	cli.ResetClientHooksForTest()
	t.Cleanup(cli.ResetClientHooksForTest)

	c, _, err := newMCPClientFromConfig(context.Background(), &config.Config{})
	if err != nil {
		t.Fatalf("newMCPClientFromConfig() with no hooks error = %v", err)
	}
	if c == nil {
		t.Fatal("newMCPClientFromConfig() with no hooks client is nil")
	}
}

func TestMCPClientHookFailureStopsBeforeHTTPAndClearsCredentials(t *testing.T) {
	cli.ResetClientHooksForTest()
	t.Cleanup(cli.ResetClientHooksForTest)

	wantErr := errors.New("managed auth setup failed")
	var laterCalls atomic.Int32
	cli.RegisterClientHookForTest(func(*client.Client) error {
		return wantErr
	})
	cli.RegisterClientHookForTest(func(*client.Client) error {
		laterCalls.Add(1)
		return nil
	})

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	t.Cleanup(server.Close)

	secret := []byte("super-secret")
	bound := &platform.Session{
		ProfileName: "fixture-tenant",
		Source:      "fixture-source",
		Paths:       platform.Paths{CacheDir: t.TempDir()},
		GateOutcome: platform.GateVerified,
		Credentials: platform.ResolvedCredentials{"token": secret},
	}
	ctx := platform.ContextWithSession(context.Background(), bound)

	c, session, err := newMCPClientFromConfig(ctx, &config.Config{BaseURL: server.URL})
	if c != nil {
		_, _ = c.Get(ctx, "/probe", nil)
	}
	if c != nil || session != nil {
		t.Fatalf("failed MCP initialization returned client=%v session=%v", c != nil, session != nil)
	}
	if err == nil || !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "initializing MCP client") {
		t.Fatalf("newMCPClientFromConfig() error = %v, want wrapped %v", err, wantErr)
	}
	if laterCalls.Load() != 0 {
		t.Fatalf("hooks after failure ran %d times, want 0", laterCalls.Load())
	}
	if requests != 0 {
		t.Fatalf("outbound requests after hook failure = %d, want 0", requests)
	}
	if _, ok := bound.Credentials["token"]; ok {
		t.Fatal("bound credentials were not cleared after hook failure")
	}
	if !bytes.Equal(secret, make([]byte, len(secret))) {
		t.Fatalf("credential buffer after Zero = %q, want zeroed", secret)
	}
}
`
	require.NoError(t, os.WriteFile(
		filepath.Join(outputDir, "internal", "mcp", "client_hook_contract_test.go"),
		[]byte(mcpTest),
		0o644,
	))

	requireGeneratedCompiles(t, outputDir)
	runGoCommand(t, outputDir, "test", "./internal/mcp", "-run", "^TestMCPClient(AppliesPreservedClientHooks|AllowsNoClientHooks|HookFailureStopsBeforeHTTPAndClearsCredentials)$", "-count=1")
}
