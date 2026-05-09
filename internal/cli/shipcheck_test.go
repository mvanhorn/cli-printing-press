package cli

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// buildShipcheckStub compiles the shipcheck stub once per test run and
// returns its path. The stub mimics the printing-press leg surface
// (dogfood/verify/workflow-verify/verify-skill/scorecard) and is
// configurable via env vars: see internal/cli/testdata/shipcheck-stub/main.go.
func buildShipcheckStub(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "shipcheck-stub")
	cmd := exec.Command("go", "build", "-o", out, "./testdata/shipcheck-stub")
	if buildOut, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building shipcheck stub: %v\n%s", err, string(buildOut))
	}
	return out
}

// fakeCLIDir creates a minimal directory that satisfies validateShipcheckDir:
// a directory containing go.mod. Returned path is absolute.
func fakeCLIDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fake\n"), 0o644); err != nil {
		t.Fatalf("writing fake go.mod: %v", err)
	}
	return dir
}

// withStubBinary swaps resolveSelfBinary for the duration of a test so
// the umbrella spawns the stub instead of the real printing-press
// binary. Returns a cleanup function callers must defer.
func withStubBinary(t *testing.T, path string) func() {
	t.Helper()
	prev := resolveSelfBinary
	resolveSelfBinary = func() (string, error) { return path, nil }
	return func() { resolveSelfBinary = prev }
}

func useShipcheckStub(t *testing.T) {
	t.Helper()
	stub := buildShipcheckStub(t)
	t.Cleanup(withStubBinary(t, stub))
}

type shipcheckHarness struct {
	dir     string
	logFile string
}

func newShipcheckHarness(t *testing.T) shipcheckHarness {
	t.Helper()
	useShipcheckStub(t)
	logFile := filepath.Join(t.TempDir(), "stub.log")
	t.Setenv("STUB_LOG_FILE", logFile)
	return shipcheckHarness{
		dir:     fakeCLIDir(t),
		logFile: logFile,
	}
}

// readStubLog parses the stub's per-invocation argv log. Each line is
// tab-separated argv as the stub recorded it.
func readStubLog(t *testing.T, logPath string) [][]string {
	t.Helper()
	f, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("opening stub log: %v", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	var out [][]string
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		out = append(out, strings.Split(line, "\t"))
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading stub log: %v", err)
	}
	return out
}

// runShipcheckCmd runs newShipcheckCmd().RunE with the given args (no
// "shipcheck" prefix) and returns the resulting error. It does not
// intercept stdout/stderr — they go to the test process's own
// streams, which lets `go test -v` show what the stub printed.
func runShipcheckCmd(t *testing.T, args ...string) error {
	t.Helper()
	cmd := newShipcheckCmd()
	cmd.SetArgs(args)
	return cmd.Execute()
}

// TestShipcheck_AllLegsPass: every leg exits 0, umbrella returns nil.
// All six legs must be invoked in canonical order with correct argv.
func TestShipcheck_AllLegsPass(t *testing.T) {
	h := newShipcheckHarness(t)

	if err := runShipcheckCmd(t, "--dir", h.dir); err != nil {
		t.Fatalf("expected nil error when all legs pass; got %v", err)
	}

	invocations := readStubLog(t, h.logFile)
	if len(invocations) != len(shipcheckLegs) {
		t.Fatalf("expected %d leg invocations; got %d: %v", len(shipcheckLegs), len(invocations), invocations)
	}

	// Confirm canonical order: dogfood, verify, workflow-verify, verify-skill, validate-narrative, scorecard.
	wantOrder := []string{"dogfood", "verify", "workflow-verify", "verify-skill", "validate-narrative", "scorecard"}
	for i, want := range wantOrder {
		// argv[0] is the stub binary path; argv[1] is the leg name.
		if len(invocations[i]) < 2 {
			t.Fatalf("invocation %d has fewer than 2 args: %v", i, invocations[i])
		}
		if invocations[i][1] != want {
			t.Errorf("invocation %d: want leg %q, got %q (full argv: %v)", i, want, invocations[i][1], invocations[i])
		}
	}
}

// TestShipcheck_OneLegFails: verify-skill exits 1, umbrella returns
// ExitError with code 1; all six legs still ran (no fail-fast).
func TestShipcheck_OneLegFails(t *testing.T) {
	h := newShipcheckHarness(t)
	t.Setenv("STUB_EXIT_VERIFY_SKILL", "1")

	err := runShipcheckCmd(t, "--dir", h.dir)
	if err == nil {
		t.Fatal("expected non-nil error when verify-skill fails; got nil")
	}
	exitErr, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError; got %T: %v", err, err)
	}
	if exitErr.Code != 1 {
		t.Errorf("expected umbrella exit code 1; got %d", exitErr.Code)
	}
	if !exitErr.Silent {
		t.Error("expected Silent=true so cobra does not duplicate the error message; got Silent=false")
	}

	invocations := readStubLog(t, h.logFile)
	if len(invocations) != len(shipcheckLegs) {
		t.Errorf("expected %d invocations even when one fails (no fail-fast); got %d", len(shipcheckLegs), len(invocations))
	}
}

// TestShipcheck_MultipleFailures: dogfood exits 2, scorecard exits 1.
// Umbrella exits with the largest non-zero code (2).
func TestShipcheck_MultipleFailures(t *testing.T) {
	h := newShipcheckHarness(t)
	t.Setenv("STUB_EXIT_DOGFOOD", "2")
	t.Setenv("STUB_EXIT_SCORECARD", "1")

	err := runShipcheckCmd(t, "--dir", h.dir)
	if err == nil {
		t.Fatal("expected non-nil error when multiple legs fail")
	}
	exitErr, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError; got %T", err)
	}
	if exitErr.Code != 2 {
		t.Errorf("expected umbrella exit code 2 (max of failing leg codes); got %d", exitErr.Code)
	}
}

// TestShipcheck_DefaultArgvIncludesFixAndLiveCheck verifies that without
// any opt-out flags, verify gets --fix and scorecard gets --live-check.
// These are the recommended Phase 4 invocations.
func TestShipcheck_DefaultArgvIncludesFixAndLiveCheck(t *testing.T) {
	h := newShipcheckHarness(t)

	if err := runShipcheckCmd(t, "--dir", h.dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invocations := readStubLog(t, h.logFile)
	verifyArgs := findInvocation(invocations, "verify")
	if !argvHas(verifyArgs, "--fix") {
		t.Errorf("expected verify argv to include --fix by default; got %v", verifyArgs)
	}
	scorecardArgs := findInvocation(invocations, "scorecard")
	if !argvHas(scorecardArgs, "--live-check") {
		t.Errorf("expected scorecard argv to include --live-check by default; got %v", scorecardArgs)
	}
}

// TestShipcheck_PassesSpecAndResearchDir: when --spec and --research-dir
// are set, dogfood and scorecard receive both; verify receives --spec.
func TestShipcheck_PassesSpecAndResearchDir(t *testing.T) {
	h := newShipcheckHarness(t)

	specPath := "/some/spec.yaml"
	researchDir := "/some/research"
	if err := runShipcheckCmd(t, "--dir", h.dir, "--spec", specPath, "--research-dir", researchDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invocations := readStubLog(t, h.logFile)

	dogfoodArgs := findInvocation(invocations, "dogfood")
	if !argvHas(dogfoodArgs, "--spec") || !argvHas(dogfoodArgs, specPath) {
		t.Errorf("dogfood argv missing --spec: %v", dogfoodArgs)
	}
	if !argvHas(dogfoodArgs, "--research-dir") || !argvHas(dogfoodArgs, researchDir) {
		t.Errorf("dogfood argv missing --research-dir: %v", dogfoodArgs)
	}

	verifyArgs := findInvocation(invocations, "verify")
	if !argvHas(verifyArgs, "--spec") || !argvHas(verifyArgs, specPath) {
		t.Errorf("verify argv missing --spec: %v", verifyArgs)
	}

	scorecardArgs := findInvocation(invocations, "scorecard")
	if !argvHas(scorecardArgs, "--spec") || !argvHas(scorecardArgs, specPath) {
		t.Errorf("scorecard argv missing --spec: %v", scorecardArgs)
	}
	if !argvHas(scorecardArgs, "--research-dir") || !argvHas(scorecardArgs, researchDir) {
		t.Errorf("scorecard argv missing --research-dir: %v", scorecardArgs)
	}

	// workflow-verify, verify-skill, and validate-narrative don't take --spec or --research-dir;
	// confirm they don't get them.
	for _, leg := range []string{"workflow-verify", "verify-skill", "validate-narrative"} {
		args := findInvocation(invocations, leg)
		if argvHas(args, "--spec") {
			t.Errorf("%s should not receive --spec; got %v", leg, args)
		}
		if argvHas(args, "--research-dir") {
			t.Errorf("%s should not receive --research-dir; got %v", leg, args)
		}
	}
}

func TestShipcheck_ValidateNarrativeUsesResearchAndBuiltBinary(t *testing.T) {
	h := newShipcheckHarness(t)
	researchDir := t.TempDir()

	if err := runShipcheckCmd(t, "--dir", h.dir, "--research-dir", researchDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	args := findInvocation(readStubLog(t, h.logFile), "validate-narrative")
	wantResearch := filepath.Join(researchDir, "research.json")
	wantBinary := filepath.Join(h.dir, filepath.Base(h.dir))
	for _, want := range []string{"--strict", "--full-examples", "--research", wantResearch, "--binary", wantBinary} {
		if !argvHas(args, want) {
			t.Errorf("validate-narrative argv missing %q: %v", want, args)
		}
	}
}

// TestShipcheck_RequiresDir: missing --dir returns ExitInputError before
// any leg runs.
func TestShipcheck_RequiresDir(t *testing.T) {
	useShipcheckStub(t)
	logFile := filepath.Join(t.TempDir(), "stub.log")
	t.Setenv("STUB_LOG_FILE", logFile)

	err := runShipcheckCmd(t)
	if err == nil {
		t.Fatal("expected error for missing --dir")
	}
	exitErr, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError; got %T", err)
	}
	if exitErr.Code != ExitInputError {
		t.Errorf("expected ExitInputError; got %d", exitErr.Code)
	}

	// Stub log should be empty — no legs spawned.
	if _, err := os.Stat(logFile); !os.IsNotExist(err) {
		invocations := readStubLog(t, logFile)
		if len(invocations) != 0 {
			t.Errorf("expected 0 invocations when --dir missing; got %d", len(invocations))
		}
	}
}

// TestShipcheck_RejectsNonexistentDir: --dir pointing at a missing path
// returns ExitInputError.
func TestShipcheck_RejectsNonexistentDir(t *testing.T) {
	useShipcheckStub(t)

	err := runShipcheckCmd(t, "--dir", "/this/path/does/not/exist/anywhere")
	if err == nil {
		t.Fatal("expected error for nonexistent --dir")
	}
	exitErr, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError; got %T", err)
	}
	if exitErr.Code != ExitInputError {
		t.Errorf("expected ExitInputError; got %d", exitErr.Code)
	}
}

// TestShipcheck_RejectsDirWithoutGoMod: --dir pointing at a directory
// without go.mod returns ExitInputError. Guards against accidentally
// running shipcheck against a manuscripts dir or unrelated path.
func TestShipcheck_RejectsDirWithoutGoMod(t *testing.T) {
	useShipcheckStub(t)

	dir := t.TempDir() // empty — no go.mod
	err := runShipcheckCmd(t, "--dir", dir)
	if err == nil {
		t.Fatal("expected error for --dir without go.mod")
	}
	exitErr, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("expected *ExitError; got %T", err)
	}
	if exitErr.Code != ExitInputError {
		t.Errorf("expected ExitInputError; got %d", exitErr.Code)
	}
}

// TestShipcheck_NoFix_OmitsFixFromVerify confirms --no-fix removes
// --fix from verify's argv. Used when an operator wants a read-only
// shipcheck pass without verify mutating source files.
func TestShipcheck_NoFix_OmitsFixFromVerify(t *testing.T) {
	h := newShipcheckHarness(t)

	if err := runShipcheckCmd(t, "--dir", h.dir, "--no-fix"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	verifyArgs := findInvocation(readStubLog(t, h.logFile), "verify")
	if argvHas(verifyArgs, "--fix") {
		t.Errorf("--no-fix should omit --fix from verify argv; got %v", verifyArgs)
	}
}

// TestShipcheck_NoLiveCheck_OmitsLiveCheckFromScorecard confirms
// --no-live-check removes --live-check from scorecard's argv. Used when
// an operator wants a quick scorecard read without sampling live calls.
func TestShipcheck_NoLiveCheck_OmitsLiveCheckFromScorecard(t *testing.T) {
	h := newShipcheckHarness(t)

	if err := runShipcheckCmd(t, "--dir", h.dir, "--no-live-check"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	scorecardArgs := findInvocation(readStubLog(t, h.logFile), "scorecard")
	if argvHas(scorecardArgs, "--live-check") {
		t.Errorf("--no-live-check should omit --live-check from scorecard argv; got %v", scorecardArgs)
	}
}

// TestShipcheck_PassesAuthFlagsToVerify confirms --api-key and --env-var
// flow through to verify (and only verify — other legs do not accept them).
func TestShipcheck_PassesAuthFlagsToVerify(t *testing.T) {
	h := newShipcheckHarness(t)

	if err := runShipcheckCmd(t,
		"--dir", h.dir,
		"--api-key", "ghp_test123",
		"--env-var", "GITHUB_TOKEN",
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invocations := readStubLog(t, h.logFile)
	verifyArgs := findInvocation(invocations, "verify")
	if !argvHas(verifyArgs, "--api-key") || !argvHas(verifyArgs, "ghp_test123") {
		t.Errorf("verify argv missing --api-key: %v", verifyArgs)
	}
	if !argvHas(verifyArgs, "--env-var") || !argvHas(verifyArgs, "GITHUB_TOKEN") {
		t.Errorf("verify argv missing --env-var: %v", verifyArgs)
	}

	// Other legs must NOT receive these flags — they don't accept them.
	for _, leg := range []string{"dogfood", "workflow-verify", "verify-skill", "scorecard"} {
		args := findInvocation(invocations, leg)
		if argvHas(args, "--api-key") {
			t.Errorf("%s argv should not include --api-key; got %v", leg, args)
		}
		if argvHas(args, "--env-var") {
			t.Errorf("%s argv should not include --env-var; got %v", leg, args)
		}
	}
}

// TestShipcheck_StrictPassesToVerifySkill confirms --strict propagates
// to verify-skill (and only verify-skill — other legs don't accept it).
func TestShipcheck_StrictPassesToVerifySkill(t *testing.T) {
	h := newShipcheckHarness(t)

	if err := runShipcheckCmd(t, "--dir", h.dir, "--strict"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invocations := readStubLog(t, h.logFile)
	vsArgs := findInvocation(invocations, "verify-skill")
	if !argvHas(vsArgs, "--strict") {
		t.Errorf("verify-skill argv missing --strict: %v", vsArgs)
	}
	for _, leg := range []string{"dogfood", "verify", "workflow-verify", "scorecard"} {
		args := findInvocation(invocations, leg)
		if argvHas(args, "--strict") {
			t.Errorf("%s argv should not include --strict; got %v", leg, args)
		}
	}
}

// TestShipcheck_JSONEnvelope_AllPass: --json produces parseable JSON
// with the expected shape when every leg passes.
func TestShipcheck_JSONEnvelope_AllPass(t *testing.T) {
	h := newShipcheckHarness(t)

	out := captureStdout(t, func() {
		if err := runShipcheckCmd(t, "--dir", h.dir, "--json"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	// The output stream is mixed: stub's own stdout plus the JSON
	// envelope at end-of-run. Find the JSON envelope by locating the
	// final `}` and walking back to the matching `{`.
	envelopeJSON := extractFinalJSONObject(t, out)

	var env shipcheckJSONEnvelope
	if err := json.Unmarshal([]byte(envelopeJSON), &env); err != nil {
		t.Fatalf("envelope is not valid JSON: %v\n--- envelope ---\n%s", err, envelopeJSON)
	}
	if !env.Passed {
		t.Errorf("expected passed=true; got %+v", env)
	}
	if env.ExitCode != 0 {
		t.Errorf("expected exit_code=0; got %d", env.ExitCode)
	}
	if len(env.Legs) != len(shipcheckLegs) {
		t.Errorf("expected %d legs in envelope; got %d", len(shipcheckLegs), len(env.Legs))
	}
	for _, leg := range env.Legs {
		if !leg.Passed {
			t.Errorf("leg %s should be passed=true; got %+v", leg.Name, leg)
		}
		if leg.ExitCode != 0 {
			t.Errorf("leg %s should have exit_code=0; got %d", leg.Name, leg.ExitCode)
		}
		if leg.Command == "" {
			t.Errorf("leg %s should have non-empty command; got %+v", leg.Name, leg)
		}
		if leg.StartedAt == "" {
			t.Errorf("leg %s should have non-empty started_at; got %+v", leg.Name, leg)
		}
	}
}

// TestShipcheck_JSONEnvelope_OneFailure: --json envelope reflects a
// failing leg with passed=false at the leg and envelope level.
func TestShipcheck_JSONEnvelope_OneFailure(t *testing.T) {
	h := newShipcheckHarness(t)
	t.Setenv("STUB_EXIT_VERIFY_SKILL", "1")

	out := captureStdout(t, func() {
		err := runShipcheckCmd(t, "--dir", h.dir, "--json")
		if err == nil {
			t.Fatal("expected non-nil error when verify-skill fails")
		}
	})

	var env shipcheckJSONEnvelope
	if err := json.Unmarshal([]byte(extractFinalJSONObject(t, out)), &env); err != nil {
		t.Fatalf("envelope is not valid JSON: %v", err)
	}

	if env.Passed {
		t.Errorf("envelope.passed should be false when verify-skill failed")
	}
	if env.ExitCode != 1 {
		t.Errorf("envelope.exit_code should be 1; got %d", env.ExitCode)
	}

	var failingLeg *shipcheckJSONLeg
	for i, l := range env.Legs {
		if l.Name == "verify-skill" {
			failingLeg = &env.Legs[i]
			break
		}
	}
	if failingLeg == nil {
		t.Fatal("envelope missing verify-skill leg")
	}
	if failingLeg.Passed {
		t.Errorf("verify-skill leg should be passed=false")
	}
	if failingLeg.ExitCode != 1 {
		t.Errorf("verify-skill leg should have exit_code=1; got %d", failingLeg.ExitCode)
	}
}

// extractFinalJSONObject finds the last balanced `{...}` block in s.
// The umbrella's --json mode mixes per-leg stub output with the final
// envelope; this walks from the end back to the matching brace.
func extractFinalJSONObject(t *testing.T, s string) string {
	t.Helper()
	end := strings.LastIndex(s, "}")
	if end < 0 {
		t.Fatalf("no JSON object found in output:\n%s", s)
	}
	depth := 0
	for i := end; i >= 0; i-- {
		switch s[i] {
		case '}':
			depth++
		case '{':
			depth--
			if depth == 0 {
				return s[i : end+1]
			}
		}
	}
	t.Fatalf("could not find matching `{` for trailing `}` in output:\n%s", s)
	return ""
}

// TestShipcheckUmbrellaCode_Aggregation tests the pure exit-code aggregator.
func TestShipcheckUmbrellaCode_Aggregation(t *testing.T) {
	cases := []struct {
		name    string
		results []shipcheckLegResult
		want    int
	}{
		{
			name: "all pass",
			results: []shipcheckLegResult{
				{Name: "dogfood", ExitCode: 0},
				{Name: "verify", ExitCode: 0},
			},
			want: 0,
		},
		{
			name: "one fails with code 1",
			results: []shipcheckLegResult{
				{Name: "dogfood", ExitCode: 0},
				{Name: "verify", ExitCode: 1},
			},
			want: 1,
		},
		{
			name: "max wins across multiple failures",
			results: []shipcheckLegResult{
				{Name: "dogfood", ExitCode: 2},
				{Name: "verify", ExitCode: 1},
				{Name: "scorecard", ExitCode: 3},
			},
			want: 3,
		},
		{
			name:    "empty results",
			results: nil,
			want:    0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shipcheckUmbrellaCode(c.results); got != c.want {
				t.Errorf("shipcheckUmbrellaCode = %d; want %d", got, c.want)
			}
		})
	}
}

// findInvocation returns the argv slice (excluding the stub binary path)
// for the given leg name, or nil if not found.
func findInvocation(invocations [][]string, leg string) []string {
	for _, argv := range invocations {
		if len(argv) >= 2 && argv[1] == leg {
			return argv[1:]
		}
	}
	return nil
}

func argvHas(argv []string, needle string) bool {
	return slices.Contains(argv, needle)
}
