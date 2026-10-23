package generator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedMCPServerHardening(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("mcp-harden")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	apiSpec.Learn.Enabled = true
	apiSpec.MCP = spec.MCPConfig{Transport: []string{"stdio", "http"}}
	items := tailListResource("/items")
	items.Description = "Manage items"
	items.Endpoints["create"] = spec.Endpoint{
		Method:      "POST",
		Path:        "/items",
		Description: "Create an item",
	}
	apiSpec.Resources = map[string]spec.Resource{
		"items": items,
		"blobs": {
			Description: "Plain blobs",
			Endpoints: map[string]spec.Endpoint{
				"upload": {
					Method:             "POST",
					Path:               "/blobs",
					Description:        "Upload a plain blob",
					RequestContentType: "text/plain",
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{
		Import: true,
		Store:  true,
		Search: true,
		Tail:   true,
		MCP:    true,
	}
	gen.Narrative = &ReadmeNarrative{
		Recipes: []Recipe{{
			Title:       "Tail items slowly",
			Command:     "mcp-harden-pp-cli tail items --interval=30s",
			Explanation: "Poll items on a thirty second interval.",
		}},
	}
	require.NoError(t, gen.Generate())

	prefix := naming.EnvPrefix(apiSpec.Name)
	cliName := naming.CLI(apiSpec.Name)

	shellout := readGeneratedFile(t, outputDir, "internal", "mcp", "cobratree", "shellout.go")
	for _, name := range mcpBlockedInputFlagNames() {
		require.Contains(t, shellout, strconvQuote(name)+":", "blockedInputFlags missing %q", name)
	}
	require.Contains(t, shellout, "func mcpChildEnviron()")
	require.Contains(t, shellout, "func mcpChildEnvDropped(")
	require.Contains(t, shellout, "strings.EqualFold(key, mcpHTTPTokenEnvVar)")
	require.Contains(t, shellout, prefix+"_MCP_HTTP_TOKEN")
	require.Contains(t, shellout, "cliutil.MCPChildEnvVar")
	verifyenv := readGeneratedFile(t, outputDir, "internal", "cliutil", "verifyenv.go")
	require.Contains(t, verifyenv, prefix+"_MCP_CHILD")

	classify := readGeneratedFile(t, outputDir, "internal", "mcp", "cobratree", "classify.go")
	require.Contains(t, classify, `FlagInputSinksAnnotation = "mcp:read-flags"`)
	require.Contains(t, classify, "func flagReadSinkNames(")
	require.Contains(t, classify, "func InputFlagBlocked(")

	typemap := readGeneratedFile(t, outputDir, "internal", "mcp", "cobratree", "typemap.go")
	require.Contains(t, typemap, "flagReadSinkNames(cmd)")
	require.Contains(t, typemap, "blockedInputFlags")

	intents := readGeneratedFile(t, outputDir, "internal", "mcp", "intents.go")
	require.Contains(t, intents, "cobratree.InputFlagBlocked")

	tailSrc := readGeneratedFile(t, outputDir, "internal", "cli", "tail.go")
	require.Contains(t, tailSrc, "cliutil.IsMCPChildEnv()")
	require.Contains(t, tailSrc, "at least 1s in follow mode")
	require.Less(t, strings.Index(tailSrc, "at least 1s in follow mode"), strings.Index(tailSrc, "flags.newClient()"))

	tools := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	require.Contains(t, tools, "db.SearchContext(")
	require.Contains(t, tools, "limit_capped")
	require.Contains(t, tools, "mcplib.Max(bound.MaxItems)")
	require.Contains(t, tools, "bound.WithSQLQueryDeadline")

	storeSrc := readGeneratedFile(t, outputDir, "internal", "store", "store.go")
	require.Contains(t, storeSrc, "func (s *Store) SearchContext(")
	require.Contains(t, storeSrc, "s.db.QueryContext(ctx, q, args...)")
	require.Contains(t, storeSrc, "return s.SearchContext(context.Background(), query, limit, resourceTypes...)")

	mainSrc := readGeneratedMCPMain(t, outputDir, apiSpec.Name)
	require.Contains(t, mainSrc, "newMCPHTTPServer")
	require.Contains(t, mainSrc, "IdleTimeout:       2 * time.Minute")
	require.Contains(t, mainSrc, "ReadHeaderTimeout: 10 * time.Second")
	require.NotContains(t, mainSrc, "ReadTimeout:")
	require.NotContains(t, mainSrc, "WriteTimeout:")
	require.True(t, generatedTreeContains(t, outputDir, `"mcp:read-flags": "file"`))

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "tail_mcp_child_test.go"), []byte(tailMCPChildTest(prefix, cliName)), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "tail_input_runtime_test.go"), []byte(mcpTailInputRuntimeTest(prefix, cliName)), 0o644))

	requireGeneratedCompiles(t, outputDir)
	requireGoTestPassed(t, outputDir, []string{"./internal/cli", "-run", "^TestGeneratedTailMCPChild$", "-count=1"},
		"--- PASS: TestGeneratedTailMCPChild")
	requireGoTestPassed(t, outputDir, []string{"./internal/mcp", "-run", "^TestMCP(TailChildPollsOnce|SearchLimitIsCapped)", "-count=1"},
		"--- PASS: TestMCPTailChildPollsOnce",
		"--- PASS: TestMCPSearchLimitIsCappedAndCancellable")
	requireGoTestPassed(t, outputDir, []string{"./internal/mcp/cobratree", "-run", "^Test(Input|RunCLICommandSetsMCPChild|RunCLICommandOmitsMCPHTTPToken)", "-count=1"},
		"--- PASS: TestInputPathFlagsStayOutOfMCPSchemaAndArgv",
		"--- PASS: TestInputFlagBlockedCoversGlobalNames",
		"--- PASS: TestRunCLICommandSetsMCPChildEnv",
		"--- PASS: TestRunCLICommandOmitsMCPHTTPToken")
	requireGoTestPassed(t, outputDir, []string{"./cmd/" + naming.MCP(apiSpec.Name), "-count=1"},
		"--- PASS: TestMCPHTTPDeclaredOversizeBodyIsRefused",
		"--- PASS: TestMCPHTTPTrickledBodyTimesOut",
		"--- PASS: TestMCPHTTPIdleKeepAliveIsReaped",
		"--- PASS: TestMCPHTTPUnauthenticatedSkipsBodyRead",
		"--- PASS: TestParseHTTPOriginBypasses",
		"--- PASS: TestMCPHTTPOriginGuard",
		"--- PASS: TestMCPHTTPInitializeRejectsForeignOrigin")
}

func requireGoTestPassed(t *testing.T, dir string, args []string, want ...string) {
	t.Helper()
	output, err := runGoCommandOutput(t, dir, append([]string{"test", "-v"}, args...)...)
	require.NoError(t, err, output)
	for _, line := range want {
		require.Contains(t, output, line, output)
	}
}

func strconvQuote(s string) string {
	return `"` + s + `"`
}

func generatedTreeContains(t *testing.T, root, needle string) bool {
	t.Helper()
	found := false
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || found || !strings.HasSuffix(path, ".go") {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if strings.Contains(string(body), needle) {
			found = true
		}
		return nil
	})
	require.NoError(t, err)
	return found
}

func tailMCPChildTest(prefix, cliName string) string {
	return `package cli

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestGeneratedTailMCPChild(t *testing.T) {
	bin := buildMCPHardenCLI(t, "` + cliName + `")

	t.Run("subsecond follow is a usage error", func(t *testing.T) {
		hits := &atomic.Int32{}
		srv := tailHitServer(hits)
		defer srv.Close()
		stdout, stderr, err := runTail(t, bin, srv.URL, nil, "items", "--interval", "500ms")
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
			t.Fatalf("exit = %v, want 2; stdout %s stderr %s", err, stdout, stderr)
		}
		if !strings.Contains(stderr, "at least 1s") {
			t.Fatalf("stderr = %q", stderr)
		}
		if hits.Load() != 0 {
			t.Fatalf("hits = %d, want 0", hits.Load())
		}
	})

	t.Run("mcp child polls once", func(t *testing.T) {
		hits := &atomic.Int32{}
		srv := tailHitServer(hits)
		defer srv.Close()
		stdout, stderr, err := runTail(t, bin, srv.URL, []string{"` + prefix + `_MCP_CHILD=1"}, "items")
		if err != nil {
			t.Fatalf("tail: %v\nstdout %s\nstderr %s", err, stdout, stderr)
		}
		if hits.Load() != 1 {
			t.Fatalf("hits = %d, want 1; stdout %s", hits.Load(), stdout)
		}
	})

	t.Run("follow false ignores the floor", func(t *testing.T) {
		hits := &atomic.Int32{}
		srv := tailHitServer(hits)
		defer srv.Close()
		stdout, stderr, err := runTail(t, bin, srv.URL, nil, "items", "--follow=false", "--interval", "500ms")
		if err != nil {
			t.Fatalf("tail: %v\nstdout %s\nstderr %s", err, stdout, stderr)
		}
		if hits.Load() != 1 {
			t.Fatalf("hits = %d, want 1; stdout %s", hits.Load(), stdout)
		}
	})

	t.Run("terminal follow stays up until interrupted", func(t *testing.T) {
		hits := &atomic.Int32{}
		first := make(chan struct{})
		var once sync.Once
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits.Add(1)
			once.Do(func() { close(first) })
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, ` + "`" + `[{"id":"1"}]` + "`" + `)
		}))
		defer srv.Close()

		cmd := exec.Command(bin, "tail", "items")
		cmd.Env = tailChildEnv(t, srv.URL, nil)
		var stdout, stderr strings.Builder
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()

		select {
		case <-first:
		case err := <-done:
			t.Fatalf("tail exited before the first poll: %v\nstdout %s\nstderr %s", err, stdout.String(), stderr.String())
		case <-time.After(8 * time.Second):
			_ = cmd.Process.Kill()
			t.Fatalf("tail made no request\nstdout %s\nstderr %s", stdout.String(), stderr.String())
		}
		select {
		case err := <-done:
			t.Fatalf("follow mode exited before SIGINT: %v\nstdout %s\nstderr %s", err, stdout.String(), stderr.String())
		case <-time.After(300 * time.Millisecond):
		}
		if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
			_ = cmd.Process.Kill()
			t.Fatal(err)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("SIGINT exit: %v\nstdout %s\nstderr %s", err, stdout.String(), stderr.String())
			}
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			t.Fatal("tail ignored SIGINT")
		}
		if hits.Load() != 1 {
			t.Fatalf("hits = %d, want 1", hits.Load())
		}
	})
}

func tailHitServer(hits *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, ` + "`" + `[{"id":"1"}]` + "`" + `)
	}))
}

func runTail(t *testing.T, bin, baseURL string, extra []string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(bin, append([]string{"tail"}, args...)...)
	cmd.Env = tailChildEnv(t, baseURL, extra)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func tailChildEnv(t *testing.T, baseURL string, extra []string) []string {
	t.Helper()
	home := t.TempDir()
	drop := map[string]bool{
		"HOME": true, "USERPROFILE": true,
		"XDG_CONFIG_HOME": true, "XDG_DATA_HOME": true,
		"XDG_STATE_HOME": true, "XDG_CACHE_HOME": true,
		"PRINTING_PRESS_DOGFOOD": true, "PRINTING_PRESS_VERIFY": true,
		"` + prefix + `_MCP_CHILD": true,
		"` + prefix + `_MCP_READ_ONLY": true,
		"` + prefix + `_BASE_URL": true,
	}
	env := make([]string, 0, len(os.Environ())+8)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if drop[name] {
			continue
		}
		env = append(env, entry)
	}
	env = append(env,
		"HOME="+home,
		"USERPROFILE="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
		"` + prefix + `_BASE_URL="+baseURL,
	)
	return append(env, extra...)
}

func buildMCPHardenCLI(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	bin := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-mod=mod", "-o", bin, "./cmd/"+name)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build cli: %v\n%s", err, out)
	}
	return bin
}
`
}

func mcpTailInputRuntimeTest(prefix, cliName string) string {
	return `package mcp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func TestMCPTailChildPollsOnce(t *testing.T) {
	bin := buildCompanionCLI(t, "` + cliName + `")
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, ` + "`" + `[{"id":"1"}]` + "`" + `)
	}))
	defer srv.Close()

	t.Setenv("` + prefix + `_CLI_PATH", bin)
	t.Setenv("` + prefix + `_BASE_URL", srv.URL)
	t.Setenv("` + prefix + `_MCP_READ_ONLY", "")
	t.Setenv("` + prefix + `_MCP_CHILD", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	t.Setenv("PRINTING_PRESS_VERIFY", "")

	s := server.NewMCPServer("mcp-harden", "test")
	RegisterTools(s)
	tools := s.ListTools()

	tailTool := tools["tail"]
	if tailTool == nil {
		t.Fatal("tail tool missing")
	}
	result, err := tailTool.Handler(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{
		Name:      "tail",
		Arguments: map[string]any{"resource": "items"},
	}})
	if err != nil {
		t.Fatalf("tail handler: %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("tail tool error: %s", mcpTextContent(t, result))
	}
	if hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1; result %s", hits.Load(), mcpTextContent(t, result))
	}

	before := hits.Load()
	rejected, err := tailTool.Handler(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{
		Name:      "tail",
		Arguments: map[string]any{"resource": "items", "interval": "500ms"},
	}})
	if err != nil {
		t.Fatalf("tail handler: %v", err)
	}
	if rejected == nil || !rejected.IsError {
		t.Fatalf("tight interval was accepted: %s", mcpTextContent(t, rejected))
	}
	if text := mcpTextContent(t, rejected); !strings.Contains(text, "at least 1s") {
		t.Fatalf("tight interval error = %q", text)
	}
	if hits.Load() != before {
		t.Fatalf("tight interval sent a request: hits %d -> %d", before, hits.Load())
	}

	for _, tc := range []struct {
		tool   string
		hidden []string
		kept   []string
		reject string
	}{
		{tool: "teach", hidden: []string{"playbook-file", "playbook-notes-file"}, kept: []string{"playbook-json", "notes", "playbook-notes"}, reject: "playbook-file"},
		{tool: "teach_playbook", hidden: []string{"playbook-file", "notes-file"}, kept: []string{"playbook-json", "notes"}, reject: "notes-file"},
		{tool: "import", hidden: []string{"input"}, kept: []string{"batch-size"}, reject: "input"},
	} {
		entry := tools[tc.tool]
		if entry == nil {
			t.Fatalf("%s tool missing", tc.tool)
		}
		props := entry.Tool.InputSchema.Properties
		for _, name := range tc.hidden {
			if _, ok := props[name]; ok {
				t.Fatalf("%s schema contains %q: %#v", tc.tool, name, props)
			}
		}
		for _, name := range tc.kept {
			if _, ok := props[name]; !ok {
				t.Fatalf("%s schema missing %q: %#v", tc.tool, name, props)
			}
		}
		refused, err := entry.Handler(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{
			Arguments: map[string]any{tc.reject: "/etc/passwd"},
		}})
		if err != nil {
			t.Fatalf("%s handler: %v", tc.tool, err)
		}
		text := mcpTextContent(t, refused)
		if refused == nil || !refused.IsError || !strings.Contains(text, "unknown MCP parameter \""+tc.reject+"\"") {
			t.Fatalf("%s accepted %s: %s", tc.tool, tc.reject, text)
		}
		if strings.Contains(text, "cli ") {
			t.Fatalf("%s spawned the CLI for %s: %s", tc.tool, tc.reject, text)
		}
	}
}

func buildCompanionCLI(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	bin := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-mod=mod", "-o", bin, "./cmd/"+name)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build cli: %v\n%s", err, out)
	}
	return bin
}
`
}
