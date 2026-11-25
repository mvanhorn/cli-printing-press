package generator

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestGeneratedExitCodeContracts proves the printed CLI, not the template
// text: positional validators exit 2, a RunE error that quotes a validator
// sentence does not, and a loopback server that always returns 429 exits 7
// after a bounded number of requests.
func TestGeneratedExitCodeContracts(t *testing.T) {
	apiSpec := minimalSpec("exitcode")
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	// A second endpoint keeps `items list` a subcommand. A lone `list`
	// endpoint is promoted to `items`, which is a different argv shape.
	apiSpec.Resources["items"].Endpoints["get"] = spec.Endpoint{
		Method:      "GET",
		Path:        "/items/{id}",
		Description: "Get one item",
		Params: []spec.Param{
			{Name: "id", Type: "string", Required: true, Positional: true, PathParam: true},
		},
	}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Export: true}
	require.NoError(t, gen.Generate())

	runGoCommand(t, outputDir, "test", "./internal/cli", "-count=1",
		"-run", "^(TestPositionalArgValidatorsExit2|TestPositionalArgValidatorAcceptsMatchingArgs|TestPositionalArgRuntimeErrorKeepsExit1|TestGeneratedTreePositionalExitCodes)$")

	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	binName := "exitcode-pp-cli" + exe
	runGoCommand(t, outputDir, "build", "-o", "./"+binName, "./cmd/exitcode-pp-cli")
	bin := filepath.Join(outputDir, binName)

	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{name: "exact missing", args: []string{"profile", "show"}, want: 2},
		{name: "exact extra", args: []string{"profile", "show", "one", "two"}, want: 2},
		{name: "range missing", args: []string{"export"}, want: 2},
		{name: "range extra", args: []string{"export", "items", "out", "extra"}, want: 2},
		{name: "noargs extra", args: []string{"local-data", "purge", "extra"}, want: 2},
		{name: "runtime arg text", args: []string{"profile", "save", "a b"}, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out := runExitCodeCLI(t, bin, nil, tc.args...)
			require.Equal(t, tc.want, code, "output:\n%s", out)
		})
	}

	t.Run("persistent 429 exits 7", func(t *testing.T) {
		for _, timeout := range []string{"12s", "0"} {
			t.Run("timeout "+timeout, func(t *testing.T) {
				srv, hits := retryAfterServer(t, "1", http.StatusTooManyRequests)
				started := time.Now()
				code, out := runExitCodeCLI(t, bin, []string{"EXITCODE_BASE_URL=" + srv.URL},
					"--timeout", timeout, "--rate-limit", "0", "--no-cache", "--json", "items", "list")
				elapsed := time.Since(started)
				require.Equal(t, 7, code, "output:\n%s", out)
				require.Contains(t, out, "rate limited:")
				require.Contains(t, out, `"code":7`)
				require.Equal(t, 5, hits.Count(), "output:\n%s", out)
				require.Less(t, elapsed, 8*time.Second, "persistent 429 must stop before --timeout; output:\n%s", out)
			})
		}
	})

	t.Run("429 then 200 succeeds", func(t *testing.T) {
		var mu sync.Mutex
		var n int
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			n++
			call := n
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			if call <= 2 {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":"slow down"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`[]`))
		}))
		t.Cleanup(srv.Close)

		code, out := runExitCodeCLI(t, bin, []string{"EXITCODE_BASE_URL=" + srv.URL},
			"--timeout", "12s", "--rate-limit", "0", "--no-cache", "--json", "items", "list")
		require.Equal(t, 0, code, "output:\n%s", out)
		mu.Lock()
		defer mu.Unlock()
		require.Equal(t, 3, n, "two 429s then 200 should be three requests; output:\n%s", out)
	})

	t.Run("deadline during retry budget stays timeout", func(t *testing.T) {
		srv, hits := retryAfterServer(t, "30", http.StatusTooManyRequests)
		started := time.Now()
		code, out := runExitCodeCLI(t, bin, []string{"EXITCODE_BASE_URL=" + srv.URL},
			"--timeout", "500ms", "--rate-limit", "0", "--no-cache", "--json", "items", "list")
		elapsed := time.Since(started)
		require.Equal(t, 5, code, "output:\n%s", out)
		require.NotContains(t, out, "rate limited:")
		require.Equal(t, 1, hits.Count(), "output:\n%s", out)
		require.Less(t, elapsed, 3*time.Second)
	})
}

func retryAfterServer(t *testing.T, retryAfter string, status int) (*httptest.Server, *hitCount) {
	t.Helper()
	hits := &hitCount{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", retryAfter)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"slow down"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

type hitCount struct {
	mu sync.Mutex
	n  int
}

func (h *hitCount) Add() {
	h.mu.Lock()
	h.n++
	h.mu.Unlock()
}

func (h *hitCount) Count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.n
}

func runExitCodeCLI(t *testing.T, bin string, extra []string, args ...string) (int, string) {
	t.Helper()
	home := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"USERPROFILE="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"XDG_DATA_HOME="+filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME="+filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME="+filepath.Join(home, ".cache"),
		"PRINTING_PRESS_VERIFY=",
		"PRINTING_PRESS_DOGFOOD=",
		"PRINTING_PRESS_VERIFY_LIVE_HTTP=",
	)
	cmd.Env = append(cmd.Env, extra...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("command %v did not finish within 20s (possible unbounded 429 retry)\n%s", args, out)
	}
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr, "args %v output:\n%s", args, out)
	return exitErr.ExitCode(), string(out)
}
