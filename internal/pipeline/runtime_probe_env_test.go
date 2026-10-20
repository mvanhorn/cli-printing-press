package pipeline

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	apispec "github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPutEnvReplacesKeyWithoutTouchingPrefix(t *testing.T) {
	in := []string{
		"PRINTING_PRESS_VERIFY=0",
		"PRINTING_PRESS_VERIFY_LIVE_HTTP=1",
		"KEEP=1",
		"PROBE_BASE_URL=https://jobs.example",
		"PROBE_BASE_URL=https://also.example",
	}
	got := putEnv(in, "PRINTING_PRESS_VERIFY", "1")
	assert.Equal(t, []string{
		"PRINTING_PRESS_VERIFY_LIVE_HTTP=1",
		"KEEP=1",
		"PROBE_BASE_URL=https://jobs.example",
		"PROBE_BASE_URL=https://also.example",
		"PRINTING_PRESS_VERIFY=1",
	}, got)
	assert.Equal(t, "PRINTING_PRESS_VERIFY=0", in[0], "putEnv must not mutate the input")

	got = putEnv(got, "PROBE_BASE_URL", "http://127.0.0.1:9")
	assert.Equal(t, []string{
		"PRINTING_PRESS_VERIFY_LIVE_HTTP=1",
		"KEEP=1",
		"PRINTING_PRESS_VERIFY=1",
		"PROBE_BASE_URL=http://127.0.0.1:9",
	}, got)

	assert.Equal(t, []string{"NOTAKV", "A=2"}, putEnv([]string{"NOTAKV", "A=1"}, "A", "2"))
	assert.Equal(t, []string{"B=3", "A=9"}, putEnv([]string{"A=1", "A=2", "B=3"}, "A", "9"))
}

func TestApplySubprocessEnvCopiesExplicitEnv(t *testing.T) {
	src := []string{"A=1", "B=2"}
	applySubprocessEnv(nil, []string{"A=1"})
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	applySubprocessEnv(cmd, src)
	src[0] = "A=mutated"
	src = append(src, "C=3")
	assert.Equal(t, []string{"A=1", "B=2"}, cmd.Env)
}

func TestApplySubprocessEnvNilUsesSubprocessSnapshot(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	applySubprocessEnv(cmd, nil)
	require.NotNil(t, cmd.Env, "nil env must not mean inherit the parent process environment")
	assert.Equal(t, envValue(subprocessEnv(), "HOME"), envValue(cmd.Env, "HOME"))
}

func TestApplySubprocessEnvEmptyDoesNotInheritAmbient(t *testing.T) {
	if os.Getenv("PP_SUBPROCESS_ENV_PROBE") == "1" {
		if _, ok := os.LookupEnv("PP_AMBIENT_SENTINEL"); ok {
			_, _ = os.Stderr.WriteString("inherited\n")
			os.Exit(1)
		}
		_, _ = os.Stdout.WriteString("clean\n")
		os.Exit(0)
	}

	t.Setenv("PP_AMBIENT_SENTINEL", "secret")
	cmd := exec.Command(os.Args[0], "-test.run=^TestApplySubprocessEnvEmptyDoesNotInheritAmbient$")
	applySubprocessEnv(cmd, []string{"PP_SUBPROCESS_ENV_PROBE=1"})
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "child output: %s", out)
	assert.Contains(t, string(out), "clean")
	assert.NotContains(t, string(out), "inherited")
}

func TestBrowserSessionProofEnvReplacesStaleVerify(t *testing.T) {
	in := []string{
		"PRINTING_PRESS_VERIFY=0",
		"PROBE_BASE_URL=http://127.0.0.1:9",
		"PROBE_TOKEN=mock-token-for-testing",
	}
	got := browserSessionProofEnv(in)
	assert.Equal(t, []string{
		"PROBE_BASE_URL=http://127.0.0.1:9",
		"PROBE_TOKEN=mock-token-for-testing",
		"PRINTING_PRESS_VERIFY=1",
	}, got)
	assert.Equal(t, "PRINTING_PRESS_VERIFY=0", in[0])

	fromNil := browserSessionProofEnv(nil)
	assert.Equal(t, "1", envValue(fromNil, "PRINTING_PRESS_VERIFY"))
	assert.Equal(t, envValue(subprocessEnv(), "HOME"), envValue(fromNil, "HOME"))

	binary := buildBrowserProofEnvBinary(t)
	passed := runBrowserSessionProofTest(binary, browserProofAuth(), []string{
		"PATH=" + os.Getenv("PATH"),
		"PRINTING_PRESS_VERIFY=0",
		"PROBE_BASE_URL=http://127.0.0.1:9",
	})
	assert.Equal(t, 3, passed.Score)
	assert.Empty(t, passed.Error)

	rejected := runBrowserSessionProofTest(binary, browserProofAuth(), []string{
		"PATH=" + os.Getenv("PATH"),
		"PRINTING_PRESS_VERIFY=1",
		"PROBE_BASE_URL=https://jobs.example",
	})
	assert.Equal(t, 0, rejected.Score)
	assert.NotEmpty(t, rejected.Error)
}

func TestInferRequiredFlagsUsesProvidedEnvAndKeepsFlagDiscovery(t *testing.T) {
	binary := buildRequiredFlagProbeBinary(t)
	logA := filepath.Join(t.TempDir(), "a.log")
	logB := filepath.Join(t.TempDir(), "b.log")
	envA := []string{
		"PP_FLAG_LOG=" + logA,
		"PRINTING_PRESS_VERIFY=1",
		"PROBE_BASE_URL=http://127.0.0.1:9",
	}
	envB := []string{
		"PP_FLAG_LOG=" + logB,
		"PROBE_BASE_URL=https://jobs.example",
	}

	flagsA := inferRequiredFlags(binary, "create", envA)
	flagsB := inferRequiredFlags(binary, "create", envB)
	want := []string{"--event", "mock-event-123", "--year", "2026"}
	assert.Equal(t, want, flagsA)
	assert.Equal(t, want, flagsB, "mock env must not change required-flag discovery")

	logTextA := readProbeLog(t, logA)
	assert.Contains(t, logTextA, "verify=1\n")
	assert.Contains(t, logTextA, "base=http://127.0.0.1:9\n")
	logTextB := readProbeLog(t, logB)
	assert.Contains(t, logTextB, "verify=\n")
	assert.Contains(t, logTextB, "base=https://jobs.example\n")
	assert.NotContains(t, logTextB, "127.0.0.1")

	assert.Nil(t, inferRequiredFlags(binary, "plain", envA))
	assert.Nil(t, inferRequiredFlags("/nonexistent/binary/path", "create", envA))
}

func TestRunVerifyMockProbesCannotReachRealHost(t *testing.T) {
	dir, specPath := writeProbeCLI(t)
	sentinel, hits := startLeakSentinel(t)
	logPath := filepath.Join(t.TempDir(), "probe.log")
	parentHome := os.Getenv("HOME")

	t.Setenv("PROBE_BASE_URL", "https://jobs.example")
	t.Setenv("PROBE_TOKEN", "real-operator-token")
	t.Setenv("PRINTING_PRESS_VERIFY", "0")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "0")
	t.Setenv("PP_PROBE_LOG", logPath)
	t.Setenv("PP_SENTINEL_URL", sentinel.URL)
	t.Setenv("PP_PROBE_MODE", "mock")

	report, err := RunVerify(VerifyConfig{Dir: dir, SpecPath: specPath})
	records := readProbeRecords(t, logPath)
	if err != nil || report == nil || report.Verdict != "PASS" || hits.Load() != 0 {
		t.Fatalf("mock verify err=%v verdict=%s hits=%d\nreport=%s\nlog=%s",
			err, verifyVerdict(report), hits.Load(), marshalReport(report), formatProbeRecords(records))
	}

	require.NotEmpty(t, records)
	var base string
	var sawDoctor, sawList, sawCreate, sawHelp, sawEvent bool
	for _, rec := range records {
		if rec.Verify != "1" || rec.LiveHTTP != "1" || rec.Token != "mock-token-for-testing" {
			t.Fatalf("probe missing mock env: %+v", rec)
		}
		if rec.Token == "real-operator-token" || strings.Contains(rec.Base, "jobs.example") || rec.Base == sentinel.URL {
			t.Fatalf("probe kept ambient target: %+v", rec)
		}
		if !loopbackHost(rec.Base) {
			t.Fatalf("probe base is not the loopback mock: %+v", rec)
		}
		if rec.Home == "" || rec.Home == parentHome {
			t.Fatalf("probe home was not scoped: %+v parent=%q", rec, parentHome)
		}
		if base == "" {
			base = rec.Base
		} else if rec.Base != base {
			t.Fatalf("probes disagreed on mock base: %q vs %q", base, rec.Base)
		}
		switch {
		case len(rec.Args) == 1 && rec.Args[0] == "doctor":
			sawDoctor = true
		case len(rec.Args) == 1 && rec.Args[0] == "list":
			sawList = true
		case len(rec.Args) == 1 && rec.Args[0] == "create":
			sawCreate = true
		}
		if containsArg(rec.Args, "--help") {
			sawHelp = true
		}
		if containsArg(rec.Args, "--event") && containsArg(rec.Args, "mock-event-123") {
			sawEvent = true
		}
	}
	assert.True(t, sawDoctor, "bare doctor probe did not run")
	assert.True(t, sawList, "bare list probe did not run")
	assert.True(t, sawCreate, "bare create probe did not run")
	assert.True(t, sawHelp, "help probe did not run")
	assert.True(t, sawEvent, "required-flag discovery did not survive the mock env")
	assert.Equal(t, int32(0), hits.Load())
}

func TestRunVerifyLiveProbesKeepLiveEnv(t *testing.T) {
	dir, specPath := writeProbeCLI(t)
	sentinel, hits := startLeakSentinel(t)
	logPath := filepath.Join(t.TempDir(), "probe.log")
	t.Setenv("PRINTING_PRESS_VERIFY", "0")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "0")
	t.Setenv("PROBE_BASE_URL", "https://jobs.example")
	t.Setenv("PROBE_TOKEN", "ambient-should-lose-to-api-key")
	t.Setenv("PP_PROBE_LOG", logPath)
	t.Setenv("PP_SENTINEL_URL", sentinel.URL)
	t.Setenv("PP_PROBE_MODE", "live")

	report, err := RunVerify(VerifyConfig{Dir: dir, SpecPath: specPath, APIKey: "live-secret"})
	records := readProbeRecords(t, logPath)
	if err != nil || report == nil || report.Verdict != "PASS" || report.Mode != "live" || hits.Load() != 0 {
		t.Fatalf("live verify err=%v mode=%s verdict=%s hits=%d\nreport=%s\nlog=%s",
			err, verifyModeOf(report), verifyVerdict(report), hits.Load(), marshalReport(report), formatProbeRecords(records))
	}

	require.NotEmpty(t, records)
	var sawBare, sawDryRun, sawDoctorJSON, sawListJSON bool
	for _, rec := range records {
		if rec.Token != "live-secret" || rec.Base != "https://jobs.example" || rec.LiveHTTP != "0" {
			t.Fatalf("live probe env drifted: %+v", rec)
		}
		if rec.Token == "mock-token-for-testing" || loopbackHost(rec.Base) {
			t.Fatalf("live probe received mock target: %+v", rec)
		}
		dryRun := containsArg(rec.Args, "--dry-run")
		doctorJSON := len(rec.Args) >= 2 && rec.Args[0] == "doctor" && rec.Args[1] == "--json"
		if dryRun || doctorJSON {
			if rec.Verify != "1" {
				t.Fatalf("live dry-run or browser proof lost VERIFY=1: %+v", rec)
			}
		} else if rec.Verify != "0" {
			t.Fatalf("live execute/probe forced verify mode: %+v", rec)
		}
		if len(rec.Args) == 1 && (rec.Args[0] == "doctor" || rec.Args[0] == "list" || rec.Args[0] == "create") {
			sawBare = true
			if rec.Verify != "0" {
				t.Fatalf("bare live probe forced verify mode: %+v", rec)
			}
		}
		if dryRun {
			sawDryRun = true
		}
		if doctorJSON {
			sawDoctorJSON = true
		}
		if len(rec.Args) >= 2 && rec.Args[0] == "list" && containsArg(rec.Args, "--json") && !dryRun {
			sawListJSON = true
		}
	}
	assert.True(t, sawBare, "live bare probe did not run")
	assert.True(t, sawDryRun, "live dry-run did not run")
	assert.True(t, sawDoctorJSON, "live browser-session proof did not run")
	assert.True(t, sawListJSON, "live read execute did not run")
	assert.Equal(t, int32(0), hits.Load())
}

func browserProofAuth() apispec.AuthConfig {
	return apispec.AuthConfig{
		RequiresBrowserSession:       true,
		BrowserSessionValidationPath: "/api/items",
	}
}

type probeRecord struct {
	Args     []string `json:"args"`
	Verify   string   `json:"verify"`
	LiveHTTP string   `json:"live_http"`
	Base     string   `json:"base"`
	Token    string   `json:"token"`
	Home     string   `json:"home"`
}

func startLeakSentinel(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/leak" {
			hits.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func writeProbeCLI(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "probe-pp-cli")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cmd", "probe-pp-cli"), 0o755))
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/probe-pp-cli\n\ngo 1.26.1\n")
	writeTestFile(t, filepath.Join(dir, "cmd", "probe-pp-cli", "main.go"), probeCLISource)
	specPath := filepath.Join(root, "spec.yaml")
	writeTestFile(t, specPath, probeSpecYAML)
	return dir, specPath
}

func readProbeRecords(t *testing.T, path string) []probeRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read probe log: %v", err)
	}
	var records []probeRecord
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec probeRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("probe log line %q: %v", line, err)
		}
		records = append(records, rec)
	}
	return records
}

func formatProbeRecords(records []probeRecord) string {
	b, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err.Error()
	}
	return string(b)
}

func marshalReport(report *VerifyReport) string {
	if report == nil {
		return "nil"
	}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err.Error()
	}
	return string(b)
}

func verifyVerdict(report *VerifyReport) string {
	if report == nil {
		return ""
	}
	return report.Verdict
}

func verifyModeOf(report *VerifyReport) string {
	if report == nil {
		return ""
	}
	return report.Mode
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func loopbackHost(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "127.0.0.1", "::1", "localhost":
		return true
	default:
		return false
	}
}

func buildBrowserProofEnvBinary(t *testing.T) string {
	t.Helper()
	return buildStdlibProbeBinary(t, `package main

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "doctor" && os.Args[2] == "--json" {
		host := ""
		if u, err := url.Parse(os.Getenv("PROBE_BASE_URL")); err == nil {
			host = strings.ToLower(u.Hostname())
		}
		loopback := host == "127.0.0.1" || host == "::1" || host == "localhost"
		if os.Getenv("PRINTING_PRESS_VERIFY") == "1" && loopback {
			fmt.Println("{\"browser_session_proof\":\"valid\",\"browser_session_proof_detail\":\"synthetic\"}")
			return
		}
	}
	fmt.Println("{\"browser_session_proof\":\"missing\",\"browser_session_proof_detail\":\"bad env\"}")
}
`)
}

func buildRequiredFlagProbeBinary(t *testing.T) string {
	t.Helper()
	return buildStdlibProbeBinary(t, `package main

import (
	"os"
)

func main() {
	if log := os.Getenv("PP_FLAG_LOG"); log != "" {
		line := "verify=" + os.Getenv("PRINTING_PRESS_VERIFY") + "\nbase=" + os.Getenv("PROBE_BASE_URL") + "\n"
		f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString(line)
			_ = f.Close()
		}
	}
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	if cmd == "plain" {
		_, _ = os.Stderr.WriteString("Error: something else\n")
		os.Exit(1)
	}
	_, _ = os.Stderr.WriteString("required flag(s) \"event\", \"year\" not set\n")
	os.Exit(1)
}
`)
}

func buildStdlibProbeBinary(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "main.go")
	writeTestFile(t, mainFile, source)
	binaryPath := filepath.Join(dir, "probe")
	cmd := exec.Command("go", "build", "-o", binaryPath, mainFile)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "building probe binary: %s", out)
	return binaryPath
}

const probeSpecYAML = `name: probe
description: probe fixture
base_url: https://jobs.example
auth:
  type: cookie
  header: Cookie
  cookie_domain: ".example.com"
  cookies:
    - session
  requires_browser_session: true
  browser_session_validation_path: /api/items
resources:
  items:
    description: Items
    endpoints:
      list:
        method: GET
        path: /items
        description: List items
`

const probeCLISource = `package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	args := os.Args[1:]
	logInvocation(args)
	if helpInvocation(args) {
		writeHelp(args)
		return
	}
	if os.Getenv("PP_PROBE_MODE") == "live" {
		if liveLeak(args) {
			contactSentinel()
			os.Exit(2)
		}
	} else if !mockEnvOK() {
		contactSentinel()
		os.Exit(2)
	}
	if doctorJSON(args) {
		if os.Getenv("PRINTING_PRESS_VERIFY") == "1" {
			_, _ = os.Stdout.WriteString("{\"browser_session_proof\":\"valid\",\"browser_session_proof_detail\":\"synthetic\"}\n")
			return
		}
		_, _ = os.Stdout.WriteString("{\"browser_session_proof\":\"missing\",\"browser_session_proof_detail\":\"verify unset\"}\n")
		return
	}
	if len(args) > 0 && args[0] == "create" && !hasFlag(args, "event") {
		_, _ = os.Stderr.WriteString("required flag(s) \"event\" not set\n")
		os.Exit(1)
	}
}

func logInvocation(args []string) {
	path := os.Getenv("PP_PROBE_LOG")
	if path == "" {
		return
	}
	rec := map[string]any{
		"args":      args,
		"verify":    os.Getenv("PRINTING_PRESS_VERIFY"),
		"live_http": os.Getenv("PRINTING_PRESS_VERIFY_LIVE_HTTP"),
		"base":      os.Getenv("PROBE_BASE_URL"),
		"token":     os.Getenv("PROBE_TOKEN"),
		"home":      os.Getenv("HOME"),
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func helpInvocation(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" || arg == "help" {
			return true
		}
	}
	return false
}

func writeHelp(args []string) {
	cmd := ""
	for _, arg := range args {
		if arg != "--help" && arg != "-h" && arg != "help" {
			cmd = arg
			break
		}
	}
	switch cmd {
	case "":
		_, _ = os.Stdout.WriteString("probe fixture\n\nUsage:\n  probe-pp-cli [command]\n\nAvailable Commands:\n  doctor  Check connectivity\n  list    List items\n  create  Create an item\n\nFlags:\n  -h, --help   help for probe-pp-cli\n")
	case "doctor":
		_, _ = os.Stdout.WriteString("Check connectivity\n\nUsage:\n  probe-pp-cli doctor [flags]\n")
	case "list":
		_, _ = os.Stdout.WriteString("List items\n\nUsage:\n  probe-pp-cli list [flags]\n")
	case "create":
		_, _ = os.Stdout.WriteString("Create an item\n\nUsage:\n  probe-pp-cli create <id> [flags]\n")
	default:
		_, _ = os.Stdout.WriteString("Usage:\n  probe-pp-cli " + cmd + " [flags]\n")
	}
}

func doctorJSON(args []string) bool {
	return len(args) >= 2 && args[0] == "doctor" && args[1] == "--json"
}

func mockEnvOK() bool {
	if os.Getenv("PRINTING_PRESS_VERIFY") != "1" || os.Getenv("PRINTING_PRESS_VERIFY_LIVE_HTTP") != "1" {
		return false
	}
	if os.Getenv("PROBE_TOKEN") != "mock-token-for-testing" {
		return false
	}
	return loopbackBase(os.Getenv("PROBE_BASE_URL"))
}

func liveLeak(args []string) bool {
	if os.Getenv("PRINTING_PRESS_VERIFY_LIVE_HTTP") == "1" || loopbackBase(os.Getenv("PROBE_BASE_URL")) {
		return true
	}
	token := os.Getenv("PROBE_TOKEN")
	if token == "mock-token-for-testing" || token != "live-secret" {
		return true
	}
	verify := os.Getenv("PRINTING_PRESS_VERIFY")
	if hasFlag(args, "dry-run") || doctorJSON(args) {
		return verify != "1"
	}
	return verify == "1"
}

func hasFlag(args []string, name string) bool {
	flag := "--" + name
	for _, arg := range args {
		if arg == flag || strings.HasPrefix(arg, flag+"=") {
			return true
		}
	}
	return false
}

func loopbackBase(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "127.0.0.1", "::1", "localhost":
		return true
	default:
		return false
	}
}

func contactSentinel() {
	raw := strings.TrimRight(os.Getenv("PP_SENTINEL_URL"), "/")
	if raw == "" {
		return
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(raw + "/leak")
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}
`
