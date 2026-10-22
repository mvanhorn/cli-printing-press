package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// TestGeneratedProfileDeliverAndMCPArgs proves three printed-CLI contracts
// against a generated module:
//   - a saved run profile applies like a typed flag, and a typed flag wins
//   - --deliver captures agent-context, version, and export, including a
//     profile deliver, while profile save does not deliver its confirmation
//   - MCP endpoint tools reject undeclared arguments instead of forwarding them
func TestGeneratedProfileDeliverAndMCPArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("generated CLI compile tests run in the full generated-test CI lane")
	}

	apiSpec := profileDeliverMCPSpec("reprokit")
	outputDir := filepath.Join(t.TempDir(), "reprokit-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true, Export: true, MCP: true}
	require.NoError(t, gen.Generate())

	configSrc := readGeneratedFile(t, outputDir, "internal", "config", "config.go")
	require.Contains(t, configSrc, `"REPROKIT_BASE_URL"`)
	require.Contains(t, configSrc, "MYAPI_TOKEN")

	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "profile_deliver_contract_test.go"), []byte(profileDeliverContractTest), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "mcp", "undeclared_mcp_args_test.go"), []byte(undeclaredMCPArgsTest), 0o644))

	output, err := runGoCommandOutputWithEnv(t, outputDir, []string{
		"PRINTING_PRESS_VERIFY=",
		"PRINTING_PRESS_VERIFY_LIVE_HTTP=",
		"PRINTING_PRESS_DOGFOOD=",
	}, "test", "-count=1", "-v", "./internal/cli", "./internal/mcp", "-run", "^(TestApplyProfileOverlay|TestProfileDeliverContract|TestUndeclaredMCPArgs)$")
	require.NoError(t, err, output)
	for _, name := range []string{
		"--- PASS: TestApplyProfileOverlay",
		"--- PASS: TestProfileDeliverContract",
		"--- PASS: TestUndeclaredMCPArgs",
	} {
		require.Contains(t, output, name, "generated-module test did not pass:\n%s", output)
	}
}

func profileDeliverMCPSpec(name string) *spec.APISpec {
	apiSpec := minimalSpec(name)
	apiSpec.Learn.Disabled = true
	apiSpec.Resources = map[string]spec.Resource{
		"devices": {
			Description: "Devices",
			Endpoints: map[string]spec.Endpoint{
				"list": {
					Method:      "GET",
					Path:        "/v2/devices",
					Description: "List devices",
					Params: []spec.Param{{
						Name: "pageSize", Type: "integer", Description: "Page size",
					}},
					Response: spec.ResponseDef{Type: "array", Item: "Device"},
				},
				"get": {
					Method:      "GET",
					Path:        "/v2/devices/{id}",
					Description: "Get a device",
					Params: []spec.Param{{
						Name: "id", Type: "string", Required: true, Positional: true,
					}},
					Response: spec.ResponseDef{Type: "object", Item: "Device"},
				},
				"retire": {
					Method:      "POST",
					Path:        "/v2/devices/{id}/retire",
					Description: "Retire a device",
					Params: []spec.Param{{
						Name: "id", Type: "string", Required: true, Positional: true,
					}},
					Body: []spec.Param{
						{Name: "reason", Type: "string", Description: "Why the device is retired"},
						{Name: "dry_run", Type: "boolean", Description: "Preview the retire"},
						{Name: "tone", Type: "string", Description: "Ambiguous suggestion neighbor"},
						{Name: "bone", Type: "string", Description: "Ambiguous suggestion neighbor"},
					},
					Response: spec.ResponseDef{Type: "object", Item: "RetireResult"},
				},
			},
			SubResources: map[string]spec.Resource{
				"notes": {
					Description: "Device notes",
					Endpoints: map[string]spec.Endpoint{
						"list": {
							Method:      "GET",
							Path:        "/v2/devices/{id}/notes",
							Description: "List notes",
							Params: []spec.Param{{
								Name: "id", Type: "string", Required: true, Positional: true,
							}},
							Response: spec.ResponseDef{Type: "array", Item: "Note"},
						},
					},
				},
			},
		},
	}
	apiSpec.Types = map[string]spec.TypeDef{
		"Device": {Fields: []spec.TypeField{
			{Name: "id", Type: "string"},
			{Name: "systemName", Type: "string"},
			{Name: "serial", Type: "string"},
		}},
		"RetireResult": {Fields: []spec.TypeField{
			{Name: "ok", Type: "boolean"},
		}},
		"Note": {Fields: []spec.TypeField{
			{Name: "id", Type: "string"},
		}},
	}
	return apiSpec
}

const profileDeliverContractTest = `package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

type capturedRequest struct {
	method string
	path   string
	query  string
	body   string
}

func TestApplyProfileOverlay(t *testing.T) {
	t.Run("marks changed and sets the value", func(t *testing.T) {
		cmd := &cobra.Command{Use: "list"}
		var timeout time.Duration
		cmd.Flags().DurationVar(&timeout, "timeout", 60*time.Second, "")
		if err := cmd.ParseFlags(nil); err != nil {
			t.Fatal(err)
		}
		if err := ApplyProfileToFlags(cmd, &Profile{Values: map[string]string{"timeout": "5s"}}); err != nil {
			t.Fatal(err)
		}
		if !cmd.Flags().Changed("timeout") {
			t.Fatal("profile timeout must mark the flag changed, as if it were typed")
		}
		if timeout != 5*time.Second {
			t.Fatalf("timeout = %s, want 5s", timeout)
		}
	})

	t.Run("typed flag wins", func(t *testing.T) {
		cmd := &cobra.Command{Use: "list"}
		var timeout time.Duration
		cmd.Flags().DurationVar(&timeout, "timeout", 60*time.Second, "")
		if err := cmd.ParseFlags([]string{"--timeout", "30s"}); err != nil {
			t.Fatal(err)
		}
		if err := ApplyProfileToFlags(cmd, &Profile{Values: map[string]string{"timeout": "5s"}}); err != nil {
			t.Fatal(err)
		}
		if timeout != 30*time.Second {
			t.Fatalf("typed timeout = %s, want 30s", timeout)
		}
	})

	t.Run("typed output mode blocks profile json and agent", func(t *testing.T) {
		cmd := &cobra.Command{Use: "list"}
		var asJSON, csv, agent bool
		cmd.Flags().BoolVar(&asJSON, "json", false, "")
		cmd.Flags().BoolVar(&csv, "csv", false, "")
		cmd.Flags().BoolVar(&agent, "agent", false, "")
		if err := cmd.ParseFlags([]string{"--csv"}); err != nil {
			t.Fatal(err)
		}
		if err := ApplyProfileToFlags(cmd, &Profile{Values: map[string]string{"json": "true", "agent": "true", "compact": "true"}}); err != nil {
			t.Fatal(err)
		}
		if asJSON {
			t.Fatal("profile --json applied over a typed --csv")
		}
		if agent {
			t.Fatal("profile --agent applied over a typed output mode")
		}
		if !csv {
			t.Fatal("typed --csv was cleared")
		}
	})

	t.Run("typed agent blocks profile csv", func(t *testing.T) {
		cmd := &cobra.Command{Use: "list"}
		var asJSON, csv, agent bool
		cmd.Flags().BoolVar(&asJSON, "json", false, "")
		cmd.Flags().BoolVar(&csv, "csv", false, "")
		cmd.Flags().BoolVar(&agent, "agent", false, "")
		if err := cmd.ParseFlags([]string{"--agent"}); err != nil {
			t.Fatal(err)
		}
		if err := ApplyProfileToFlags(cmd, &Profile{Values: map[string]string{"csv": "true", "json": "true"}}); err != nil {
			t.Fatal(err)
		}
		if csv || asJSON {
			t.Fatal("profile output flags applied over a typed --agent")
		}
		if !agent {
			t.Fatal("typed --agent was cleared")
		}
	})

	t.Run("invalid value errors", func(t *testing.T) {
		cmd := &cobra.Command{Use: "list"}
		var timeout time.Duration
		cmd.Flags().DurationVar(&timeout, "timeout", 60*time.Second, "")
		if err := cmd.ParseFlags(nil); err != nil {
			t.Fatal(err)
		}
		err := ApplyProfileToFlags(cmd, &Profile{Values: map[string]string{"timeout": "nope"}})
		if err == nil {
			t.Fatal("expected invalid timeout to error")
		}
		if !strings.Contains(err.Error(), "timeout") || !strings.Contains(err.Error(), "nope") {
			t.Fatalf("error = %v, want it to name timeout and nope", err)
		}
	})
}

func TestProfileDeliverContract(t *testing.T) {
	isolateCLIHome(t)
	t.Setenv("MYAPI_TOKEN", "test-token")
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")

	var mu sync.Mutex
	var got []capturedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, capturedRequest{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, body: string(body)})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/devices":
			_, _ = w.Write([]byte(` + "`" + `[{"id":"dev-1","systemName":"alpha","serial":"ser-1"}]` + "`" + `))
		case r.Method == http.MethodPost && r.URL.Path == "/v2/devices/7/retire":
			_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("REPROKIT_BASE_URL", srv.URL)

	take := func() []capturedRequest {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		out := append([]capturedRequest(nil), got...)
		got = nil
		return out
	}

	t.Run("sanity retire help exposes dry-run", func(t *testing.T) {
		stdout, stderr, err := runCLI(t, "devices", "retire", "--help")
		if err != nil {
			t.Fatalf("help: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
		}
		if !strings.Contains(stdout, "--dry-run") {
			t.Fatalf("retire help missing --dry-run:\n%s", stdout)
		}
	})

	t.Run("profile dry-run matches typed dry-run", func(t *testing.T) {
		stdout, stderr, err := runCLI(t, "profile", "save", "safe", "--dry-run")
		if err != nil {
			t.Fatalf("save: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
		}
		take()
		stdout, stderr, err = runCLI(t, "devices", "retire", "7", "--profile", "safe", "--no-cache")
		reqs := take()
		if err != nil {
			t.Fatalf("profile retire: %v\nstdout:\n%s\nstderr:\n%s\nreqs:%v", err, stdout, stderr, reqs)
		}
		if len(reqs) != 1 {
			t.Fatalf("profile retire requests = %#v, want one POST", reqs)
		}
		profileRaw := reqs[0].body
		profileBody := decodeBody(t, profileRaw)
		if profileBody["dry_run"] != true {
			t.Fatalf("profile body = %s, want dry_run true", profileRaw)
		}
		if reqs[0].method != http.MethodPost || reqs[0].path != "/v2/devices/7/retire" {
			t.Fatalf("profile request = %s %s", reqs[0].method, reqs[0].path)
		}

		take()
		stdout, stderr, err = runCLI(t, "devices", "retire", "7", "--dry-run", "--no-cache")
		reqs = take()
		if err != nil {
			t.Fatalf("typed retire: %v\nstdout:\n%s\nstderr:\n%s\nreqs:%v", err, stdout, stderr, reqs)
		}
		if len(reqs) != 1 {
			t.Fatalf("typed retire requests = %#v, want one POST", reqs)
		}
		if reqs[0].body != profileRaw {
			t.Fatalf("profile body %s != typed body %s", profileRaw, reqs[0].body)
		}

		take()
		stdout, stderr, err = runCLI(t, "devices", "retire", "7", "--no-cache")
		reqs = take()
		if err != nil {
			t.Fatalf("plain retire: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
		}
		if len(reqs) != 1 {
			t.Fatalf("plain retire requests = %#v", reqs)
		}
		plain := decodeBody(t, reqs[0].body)
		if _, ok := plain["dry_run"]; ok {
			t.Fatalf("plain retire sent dry_run: %s", reqs[0].body)
		}
	})

	t.Run("typed csv beats profile json", func(t *testing.T) {
		mustCLI(t, "profile", "save", "asjson", "--json")
		stdout, stderr, err := runCLI(t, "devices", "list", "--csv", "--data-source", "live", "--no-cache", "--profile", "asjson")
		if err != nil {
			t.Fatalf("csv+profile json: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
		}
		if strings.Contains(stdout, "\"source\"") || !strings.Contains(stdout, "ser-1") {
			t.Fatalf("typed --csv lost to profile --json:\n%s", stdout)
		}
		bare, _, err := runCLI(t, "devices", "list", "--csv", "--data-source", "live", "--no-cache")
		if err != nil {
			t.Fatal(err)
		}
		if stdout != bare {
			t.Fatalf("profile changed csv output\n got %q\nwant %q", stdout, bare)
		}
		asJSON, _, err := runCLI(t, "devices", "list", "--json", "--data-source", "live", "--no-cache")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(asJSON, "\"source\"") || !strings.Contains(asJSON, "ser-1") {
			t.Fatalf("plain --json output changed:\n%s", asJSON)
		}
	})

	t.Run("typed json beats profile compact", func(t *testing.T) {
		mustCLI(t, "profile", "save", "tight", "--compact")
		compacted, stderr, err := runCLI(t, "devices", "list", "--json", "--compact", "--data-source", "live", "--no-cache")
		if err != nil {
			t.Fatalf("compact control: %v\n%s\n%s", err, compacted, stderr)
		}
		if strings.Contains(compacted, "\"serial\"") {
			t.Fatalf("compact control still has serial, so the override test cannot see a drop:\n%s", compacted)
		}
		stdout, stderr, err := runCLI(t, "devices", "list", "--json", "--data-source", "live", "--no-cache", "--profile", "tight")
		if err != nil {
			t.Fatalf("json+profile compact: %v\n%s\n%s", err, stdout, stderr)
		}
		if !strings.Contains(stdout, "\"serial\"") {
			t.Fatalf("profile --compact overrode typed --json:\n%s", stdout)
		}
	})

	t.Run("typed agent beats profile csv", func(t *testing.T) {
		mustCLI(t, "profile", "save", "ascsv", "--csv")
		stdout, stderr, err := runCLI(t, "devices", "list", "--agent", "--data-source", "live", "--no-cache", "--profile", "ascsv")
		if err != nil {
			t.Fatalf("agent+profile csv: %v\n%s\n%s", err, stdout, stderr)
		}
		if !strings.Contains(stdout, "\"source\"") || strings.Contains(stdout, "id,systemName") || !strings.Contains(stdout, "alpha") {
			t.Fatalf("profile --csv overrode typed --agent:\n%s", stdout)
		}
	})

	t.Run("typed csv beats profile agent", func(t *testing.T) {
		mustCLI(t, "profile", "save", "agentmode", "--agent")
		stdout, stderr, err := runCLI(t, "devices", "list", "--csv", "--data-source", "live", "--no-cache", "--profile", "agentmode")
		if err != nil {
			t.Fatalf("csv+profile agent: %v\n%s\n%s", err, stdout, stderr)
		}
		if strings.Contains(stdout, "\"source\"") || !strings.Contains(stdout, "ser-1") {
			t.Fatalf("profile --agent forced JSON over typed --csv:\n%s", stdout)
		}
	})

	t.Run("profile save does not recapture applied flags", func(t *testing.T) {
		stdout, stderr, err := runCLI(t, "profile", "save", "derived", "--profile", "safe", "--json")
		if err != nil {
			t.Fatalf("save derived: %v\n%s\n%s", err, stdout, stderr)
		}
		shown, stderr, err := runCLI(t, "profile", "show", "derived")
		if err != nil {
			t.Fatalf("show derived: %v\n%s\n%s", err, shown, stderr)
		}
		values := profileValues(t, shown)
		if _, ok := values["dry-run"]; ok {
			t.Fatalf("profile save recaptured applied dry-run: %s", shown)
		}
		if values["json"] != "true" {
			t.Fatalf("profile save dropped the typed --json: %s", shown)
		}
		stdout, stderr, err = runCLI(t, "profile", "save", "empty", "--profile", "safe")
		if err == nil {
			t.Fatalf("profile save of only applied flags succeeded:\n%s", stdout)
		}
		if !strings.Contains(stdout+stderr, "no non-default flags") {
			t.Fatalf("empty overlay error = %v\n%s\n%s", err, stdout, stderr)
		}
		stdout, stderr, err = runCLI(t, "profile", "save", "typed", "--dry-run")
		if err != nil {
			t.Fatalf("save typed: %v\n%s\n%s", err, stdout, stderr)
		}
		shown, stderr, err = runCLI(t, "profile", "show", "typed")
		if err != nil {
			t.Fatal(err)
		}
		if profileValues(t, shown)["dry-run"] != "true" {
			t.Fatalf("user-typed --dry-run was not saved: %s", shown)
		}
	})

	t.Run("bad profile timeout names the flag", func(t *testing.T) {
		dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "reprokit-pp-cli")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		payload := []byte(` + "`" + `{"profiles":{"bad":{"name":"bad","values":{"timeout":"nope"}}}}` + "`" + `)
		if err := os.WriteFile(filepath.Join(dir, "profiles.json"), payload, 0o600); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, err := runCLI(t, "version", "--profile", "bad")
		if err == nil {
			t.Fatalf("bad timeout profile succeeded:\n%s", stdout)
		}
		combined := stdout + stderr
		if !strings.Contains(combined, "timeout") || !strings.Contains(combined, "nope") {
			t.Fatalf("error does not name timeout and nope:\n%s\n%v", combined, err)
		}
		if err := os.Remove(filepath.Join(dir, "profiles.json")); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("deliver captures stdout writers", func(t *testing.T) {
		for _, args := range [][]string{
			{"agent-context"},
			{"version"},
			{"export", "devices", "--no-cache"},
		} {
			bare, stderr, err := runCLI(t, args...)
			if err != nil {
				t.Fatalf("%v: %v\n%s\n%s", args, err, bare, stderr)
			}
			if strings.TrimSpace(bare) == "" {
				t.Fatalf("%v produced empty stdout", args)
			}
			path := filepath.Join(t.TempDir(), "out.txt")
			withDeliver := append(append([]string{}, args...), "--deliver", "file:"+path)
			stdout, stderr, err := runCLI(t, withDeliver...)
			if err != nil {
				t.Fatalf("deliver %v: %v\n%s\n%s", args, err, stdout, stderr)
			}
			file, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("deliver %v did not write %s: %v\nstdout:\n%s", args, path, err, stdout)
			}
			if stdout != bare {
				t.Fatalf("deliver %v changed stdout\n got %q\nwant %q", args, stdout, bare)
			}
			if string(file) != stdout {
				t.Fatalf("deliver %v file != stdout\nfile %q\nstdout %q", args, file, stdout)
			}
		}
	})

	t.Run("deliver stdout does not create a file", func(t *testing.T) {
		dir := t.TempDir()
		wd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := os.Chdir(wd); err != nil {
				t.Errorf("restore working directory: %v", err)
			}
		}()
		stdout, stderr, err := runCLI(t, "version", "--deliver", "stdout")
		if err != nil {
			t.Fatalf("deliver stdout: %v\n%s\n%s", err, stdout, stderr)
		}
		if !strings.Contains(stdout, "reprokit-pp-cli") || !strings.Contains(stdout, "0.0.0-dev") {
			t.Fatalf("version stdout = %q", stdout)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.Name())
			}
			t.Fatalf("deliver stdout created %v in %s", names, dir)
		}
	})

	t.Run("profile deliver applies and profile save does not", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "saved.txt")
		stdout, stderr, err := runCLI(t, "profile", "save", "todisk", "--deliver", "file:"+path)
		if err != nil {
			t.Fatalf("save todisk: %v\n%s\n%s", err, stdout, stderr)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			body, _ := os.ReadFile(path)
			t.Fatalf("profile save delivered its confirmation to %s: %q", path, body)
		}
		if !strings.Contains(stdout, "saved profile") {
			t.Fatalf("save confirmation = %q", stdout)
		}
		stdout, stderr, err = runCLI(t, "devices", "list", "--json", "--data-source", "live", "--no-cache", "--profile", "todisk")
		if err != nil {
			t.Fatalf("list profile deliver: %v\n%s\n%s", err, stdout, stderr)
		}
		file, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("profile --deliver was ignored: %v\nstdout:\n%s", err, stdout)
		}
		if string(file) != stdout || !strings.Contains(string(file), "ser-1") {
			t.Fatalf("profile deliver file mismatch\nfile %q\nstdout %q", file, stdout)
		}

		direct := filepath.Join(t.TempDir(), "direct.txt")
		stdout, stderr, err = runCLI(t, "devices", "list", "--json", "--data-source", "live", "--no-cache", "--deliver", "file:"+direct)
		if err != nil {
			t.Fatalf("direct deliver: %v\n%s\n%s", err, stdout, stderr)
		}
		file, err = os.ReadFile(direct)
		if err != nil {
			t.Fatal(err)
		}
		if string(file) != stdout || !strings.Contains(string(file), "ser-1") {
			t.Fatalf("direct deliver mismatch\nfile %q\nstdout %q", file, stdout)
		}
	})
}

func isolateCLIHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	for _, key := range []string{"HOME", "USERPROFILE"} {
		t.Setenv(key, home)
	}
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		dir := filepath.Join(home, key)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(key, dir)
	}
}

func mustCLI(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr, err := runCLI(t, args...)
	if err != nil {
		t.Fatalf("%v: %v\nstdout:\n%s\nstderr:\n%s", args, err, stdout, stderr)
	}
	return stdout
}

func runCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout, oldStderr := os.Stdout, os.Stderr
	oldArgs := os.Args
	os.Stdout, os.Stderr = stdoutW, stderrW
	os.Args = append([]string{"reprokit-pp-cli"}, args...)

	var stdout, stderr bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&stdout, stdoutR)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&stderr, stderrR)
	}()

	closed := false
	defer func() {
		if !closed {
			_ = stdoutW.Close()
			_ = stderrW.Close()
			wg.Wait()
		}
		_ = stdoutR.Close()
		_ = stderrR.Close()
		os.Stdout, os.Stderr = oldStdout, oldStderr
		os.Args = oldArgs
	}()

	execErr := Execute()
	_ = stdoutW.Close()
	_ = stderrW.Close()
	closed = true
	wg.Wait()
	return stdout.String(), stderr.String(), execErr
}

func decodeBody(t *testing.T, raw string) map[string]any {
	t.Helper()
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(raw), &body); err != nil {
		t.Fatalf("body %q: %v", raw, err)
	}
	if body == nil {
		return map[string]any{}
	}
	return body
}

func profileValues(t *testing.T, stdout string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(stdout))
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("profile json %q: %v", stdout, err)
	}
	raw, _ := doc["values"].(map[string]any)
	if raw == nil {
		t.Fatalf("profile json missing values: %s", stdout)
	}
	return raw
}
`

const undeclaredMCPArgsTest = `package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type capturedRequest struct {
	method string
	path   string
	query  string
	body   string
}

func TestUndeclaredMCPArgs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		dir := filepath.Join(home, key)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(key, dir)
	}
	t.Setenv("MYAPI_TOKEN", "test-token")
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")

	var mu sync.Mutex
	var got []capturedRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, capturedRequest{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, body: string(body)})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v2/devices" {
			_, _ = w.Write([]byte("[]"))
			return
		}
		_, _ = w.Write([]byte(` + "`" + `{"ok":true}` + "`" + `))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("REPROKIT_BASE_URL", srv.URL)

	take := func() []capturedRequest {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		out := append([]capturedRequest(nil), got...)
		got = nil
		return out
	}

	s := server.NewMCPServer("test", "0.0.0")
	RegisterTools(s)
	tools := s.ListTools()
	retire := tools["devices_retire"]
	list := tools["devices_list"]
	notes := tools["devices_notes_list"]
	if retire == nil || list == nil || notes == nil {
		names := make([]string, 0, len(tools))
		for name := range tools {
			names = append(names, name)
		}
		sort.Strings(names)
		t.Fatalf("missing endpoint tools, have %s", strings.Join(names, ", "))
	}

	t.Run("schema rejects additional properties", func(t *testing.T) {
		for _, tool := range []*server.ServerTool{retire, list, notes} {
			got, ok := tool.Tool.InputSchema.AdditionalProperties.(bool)
			if !ok || got {
				t.Fatalf("%s additionalProperties = %#v, want false", tool.Tool.Name, tool.Tool.InputSchema.AdditionalProperties)
			}
		}
	})

	call := func(t *testing.T, tool *server.ServerTool, args map[string]any) (*mcplib.CallToolResult, []capturedRequest) {
		t.Helper()
		take()
		result, err := tool.Handler(context.Background(), mcplib.CallToolRequest{Params: mcplib.CallToolParams{
			Name:      tool.Tool.Name,
			Arguments: args,
		}})
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if result == nil {
			t.Fatal("nil result")
		}
		return result, take()
	}
	textOf := func(t *testing.T, result *mcplib.CallToolResult) string {
		t.Helper()
		var b strings.Builder
		for _, item := range result.Content {
			text, ok := item.(mcplib.TextContent)
			if !ok {
				continue
			}
			b.WriteString(text.Text)
		}
		if b.Len() == 0 {
			raw, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			t.Fatalf("tool result had no text content: %s", raw)
		}
		return b.String()
	}

	t.Run("declared args are forwarded unchanged", func(t *testing.T) {
		result, reqs := call(t, retire, map[string]any{"id": float64(7), "dry_run": true, "reason": "because"})
		if result.IsError {
			t.Fatalf("declared call errored: %s", textOf(t, result))
		}
		if len(reqs) != 1 || reqs[0].method != http.MethodPost || reqs[0].path != "/v2/devices/7/retire" {
			t.Fatalf("request = %#v", reqs)
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(reqs[0].body), &body); err != nil {
			t.Fatalf("body %s: %v", reqs[0].body, err)
		}
		if body["dry_run"] != true || body["reason"] != "because" || len(body) != 2 {
			t.Fatalf("body = %s", reqs[0].body)
		}

		result, reqs = call(t, retire, map[string]any{"id": float64(7)})
		if result.IsError {
			t.Fatalf("id-only call errored: %s", textOf(t, result))
		}
		if len(reqs) != 1 {
			t.Fatalf("requests = %#v", reqs)
		}
		if strings.Contains(reqs[0].body, "dry_run") {
			t.Fatalf("id-only body contained dry_run: %s", reqs[0].body)
		}

		result, reqs = call(t, list, map[string]any{"pageSize": float64(25)})
		if result.IsError {
			t.Fatalf("list errored: %s", textOf(t, result))
		}
		if len(reqs) != 1 || reqs[0].method != http.MethodGet || reqs[0].path != "/v2/devices" || reqs[0].query != "pageSize=25" {
			t.Fatalf("list request = %#v", reqs)
		}
	})

	t.Run("unknown args are rejected before http", func(t *testing.T) {
		cases := []struct {
			name string
			args map[string]any
			want []string
			hint string
			noHint bool
		}{
			{name: "folded hyphen", args: map[string]any{"id": float64(7), "dry-run": true}, want: []string{"dry-run", "dry_run"}},
			{name: "separator stripped", args: map[string]any{"page-size": float64(25)}, want: []string{"page-size", "pageSize"}},
			{name: "edit distance", args: map[string]any{"id": float64(7), "dry_rn": true}, want: []string{"dry_rn", "dry_run"}},
			{name: "distance two", args: map[string]any{"id": float64(7), "dx_run": true}, want: []string{"dx_run", "dry_run"}},
			{name: "no suggestion", args: map[string]any{"id": float64(7), "nope": true}, want: []string{"nope"}, noHint: true},
			{name: "short name", args: map[string]any{"id": float64(7), "x": true}, want: []string{"\"x\""}, noHint: true},
			{name: "ambiguous", args: map[string]any{"id": float64(7), "cone": true}, want: []string{"cone"}, noHint: true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				tool := retire
				if _, ok := tc.args["page-size"]; ok {
					tool = list
				}
				result, reqs := call(t, tool, tc.args)
				if !result.IsError {
					t.Fatalf("got success, requests=%#v result=%s", reqs, textOf(t, result))
				}
				if len(reqs) != 0 {
					t.Fatalf("unknown arg still produced %d requests: %#v", len(reqs), reqs)
				}
				text := textOf(t, result)
				for _, piece := range tc.want {
					if !strings.Contains(text, piece) {
						t.Fatalf("result missing %q: %s", piece, text)
					}
				}
				if tc.noHint && strings.Contains(text, "did you mean") {
					t.Fatalf("unexpected suggestion: %s", text)
				}
				if !tc.noHint && !strings.Contains(text, "did you mean") {
					t.Fatalf("missing suggestion: %s", text)
				}
			})
		}

		result, reqs := call(t, retire, map[string]any{"id": float64(7), "zzz": true, "aaa": true})
		if !result.IsError || len(reqs) != 0 {
			t.Fatalf("multi unknown: err=%v reqs=%#v result=%s", result.IsError, reqs, textOf(t, result))
		}
		text := textOf(t, result)
		aaa := strings.Index(text, "aaa")
		zzz := strings.Index(text, "zzz")
		if aaa < 0 || zzz < 0 || aaa > zzz {
			t.Fatalf("unknown names not both present in order: %s", text)
		}
	})

	t.Run("reserved args are not forwarded", func(t *testing.T) {
		result, reqs := call(t, retire, map[string]any{
			"id":           float64(7),
			"dry_run":      true,
			"cursor":       "abc",
			"body_json":    "{\"x\":1}",
			"body_base64":  "eA==",
			"content_type": "text/plain",
		})
		if result.IsError {
			t.Fatalf("reserved args rejected: %s", textOf(t, result))
		}
		if len(reqs) != 1 {
			t.Fatalf("requests = %#v", reqs)
		}
		for _, forbidden := range []string{"cursor", "body_json", "body_base64", "content_type"} {
			if strings.Contains(reqs[0].body, forbidden) || strings.Contains(reqs[0].query, forbidden) {
				t.Fatalf("reserved arg %s was forwarded: body=%s query=%s", forbidden, reqs[0].body, reqs[0].query)
			}
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(reqs[0].body), &body); err != nil {
			t.Fatalf("body %s: %v", reqs[0].body, err)
		}
		if body["dry_run"] != true || len(body) != 1 {
			t.Fatalf("body = %s, want only dry_run", reqs[0].body)
		}
	})
}
`
