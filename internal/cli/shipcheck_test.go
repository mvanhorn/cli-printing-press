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

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
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
	if err := os.WriteFile(filepath.Join(dir, pipeline.CLIManifestFilename), []byte(`{"api_name":"example","scorecard":{"unverified_dimensions":[]}}`+"\n"), 0o644); err != nil {
		t.Fatalf("writing fake CLI manifest: %v", err)
	}
	return dir
}

func writeHTMLSyncStubMarker(t *testing.T, dir string) {
	t.Helper()
	syncDir := filepath.Join(dir, "internal", "cli")
	if err := os.MkdirAll(syncDir, 0o755); err != nil {
		t.Fatalf("creating sync dir: %v", err)
	}
	syncSrc := `package cli

func syncStubMarker() string {
	return "sync is not implemented for this CLI; write internal/cli/sync_example.go because the generic spec-driven sync template does not fit predominantly HTML page-mode endpoints"
}
`
	if err := os.WriteFile(filepath.Join(syncDir, "sync.go"), []byte(syncSrc), 0o644); err != nil {
		t.Fatalf("writing sync stub marker: %v", err)
	}
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

func TestShipcheckCLIPathForGOOS(t *testing.T) {
	t.Parallel()

	opts := &shipcheckOpts{dir: filepath.Join("tmp", "sample-cli")}
	if got, want := shipcheckCLIPathForGOOS(opts, "windows"), filepath.Join("tmp", "sample-cli", "sample-cli.exe"); got != want {
		t.Fatalf("windows path = %q, want %q", got, want)
	}
	if got, want := shipcheckCLIPathForGOOS(opts, "linux"), filepath.Join("tmp", "sample-cli", "sample-cli"); got != want {
		t.Fatalf("linux path = %q, want %q", got, want)
	}
}

// TestShipcheckCLIPath_ManifestOverridesBasename covers the slug-keyed
// library layout: dir basename ("notion") differs from the actual binary
// name ("notion-pp-cli"), so the binary path must be resolved from
// .printing-press.json's cli_name rather than the directory's basename.
func TestShipcheckCLIPath_ManifestOverridesBasename(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "notion")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating slug dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(`{"cli_name":"notion-pp-cli"}`), 0o644); err != nil {
		t.Fatalf("writing manifest: %v", err)
	}
	opts := &shipcheckOpts{dir: dir}

	if got, want := shipcheckCLIPathForGOOS(opts, "linux"), filepath.Join(dir, "notion-pp-cli"); got != want {
		t.Fatalf("linux path = %q, want %q", got, want)
	}
	if got, want := shipcheckCLIPathForGOOS(opts, "windows"), filepath.Join(dir, "notion-pp-cli.exe"); got != want {
		t.Fatalf("windows path = %q, want %q", got, want)
	}
}

func TestShipcheckCLIPath_UsesManifestBinaryInStage(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "wt-phase-4-pp-cli")
	stagedDir := filepath.Join(dir, "build", "stage", "bin")
	if err := os.MkdirAll(stagedDir, 0o755); err != nil {
		t.Fatalf("creating staged binary dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(`{"cli_name":"notion-pp-cli"}`), 0o644); err != nil {
		t.Fatalf("writing manifest: %v", err)
	}
	manifestBinary := filepath.Join(stagedDir, "notion-pp-cli")
	if err := os.WriteFile(manifestBinary, []byte("staged binary"), 0o755); err != nil {
		t.Fatalf("writing staged binary: %v", err)
	}

	if got := shipcheckCLIPath(&shipcheckOpts{dir: dir}); got != manifestBinary {
		t.Fatalf("shipcheck binary path = %q, want manifest-named staged binary %q", got, manifestBinary)
	}
}

func TestShipcheckCLIPath_UsesExistingWorktreeBinaryWhenManifestNameDiffers(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "wt-phase-4-pp-cli")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating worktree dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(`{"cli_name":"notion-pp-cli"}`), 0o644); err != nil {
		t.Fatalf("writing manifest: %v", err)
	}
	worktreeBinary := filepath.Join(dir, filepath.Base(dir))
	if err := os.WriteFile(worktreeBinary, []byte("worktree binary"), 0o755); err != nil {
		t.Fatalf("writing worktree binary: %v", err)
	}

	opts := &shipcheckOpts{dir: dir}
	if got := shipcheckCLIPath(opts); got != worktreeBinary {
		t.Fatalf("shipcheck binary path = %q, want existing worktree binary %q", got, worktreeBinary)
	}
}

// TestShipcheckCLIPath_FallsBackToBasename covers the manifest-less case:
// when .printing-press.json is missing or unparseable, the binary name
// falls back to the directory basename (preserves legacy behavior).
func TestShipcheckCLIPath_FallsBackToBasename(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "sample-cli")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating dir: %v", err)
	}
	opts := &shipcheckOpts{dir: dir}

	if got, want := shipcheckCLIPathForGOOS(opts, "linux"), filepath.Join(dir, "sample-cli"); got != want {
		t.Fatalf("linux path = %q, want %q", got, want)
	}
	if got, want := shipcheckCLIPathForGOOS(opts, "windows"), filepath.Join(dir, "sample-cli.exe"); got != want {
		t.Fatalf("windows path = %q, want %q", got, want)
	}
}

// TestShipcheckCLIPath_FallsBackOnUnparseableManifest pins the
// unparseable-error branch of pipeline.ReadCLIBinaryName: a
// .printing-press.json that exists but contains malformed JSON must
// fall back to the directory basename rather than propagating a parse
// error or producing an empty binary name.
func TestShipcheckCLIPath_FallsBackOnUnparseableManifest(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "notion")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating slug dir: %v", err)
	}
	// Truncated JSON: opening brace, key, colon, no value, no close.
	if err := os.WriteFile(filepath.Join(dir, ".printing-press.json"), []byte(`{"cli_name":`), 0o644); err != nil {
		t.Fatalf("writing malformed manifest: %v", err)
	}
	opts := &shipcheckOpts{dir: dir}

	if got, want := shipcheckCLIPathForGOOS(opts, "linux"), filepath.Join(dir, "notion"); got != want {
		t.Fatalf("linux path = %q, want %q", got, want)
	}
	if got, want := shipcheckCLIPathForGOOS(opts, "windows"), filepath.Join(dir, "notion.exe"); got != want {
		t.Fatalf("windows path = %q, want %q", got, want)
	}
}

type shipcheckHarness struct {
	dir     string
	logFile string
}

func newShipcheckHarness(t *testing.T) shipcheckHarness {
	t.Helper()
	useShipcheckStub(t)
	// Acceptance lookup consults runstate under the press home. Keep it off
	// the real profile so a local marker cannot change these argv tests.
	t.Setenv("PRINTING_PRESS_HOME", t.TempDir())
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

	// Confirm canonical order: verify, validate-narrative, dogfood, workflow-verify, apify-audit, verify-skill, scorecard.
	wantOrder := []string{"verify", "validate-narrative", "dogfood", "workflow-verify", "apify-audit", "verify-skill", "scorecard"}
	for i, want := range wantOrder {
		// argv[0] is the stub binary path; argv[1] is the leg name.
		if len(invocations[i]) < 2 {
			t.Fatalf("invocation %d has fewer than 2 args: %v", i, invocations[i])
		}
		if invocations[i][1] != want {
			t.Errorf("invocation %d: want leg %q, got %q (full argv: %v)", i, want, invocations[i][1], invocations[i])
		}
	}

	verifyArgs := findInvocation(invocations, "verify")
	if !argvHas(verifyArgs, "--write-manifest") || !argvHas(verifyArgs, filepath.Join(h.dir, pipeline.CLIManifestFilename)) {
		t.Errorf("verify argv missing --write-manifest manifest path: %v", verifyArgs)
	}
	scorecardArgs := findInvocation(invocations, "scorecard")
	if !argvHas(scorecardArgs, "--write-manifest") || !argvHas(scorecardArgs, filepath.Join(h.dir, pipeline.CLIManifestFilename)) {
		t.Errorf("scorecard argv missing --write-manifest manifest path: %v", scorecardArgs)
	}
}

// TestShipcheck_OneLegFails: verify-skill exits 1, umbrella returns
// ExitError with code 1; all legs still ran (no fail-fast).
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

func TestShipcheck_NormalizesRelativeDirForLegs(t *testing.T) {
	h := newShipcheckHarness(t)
	t.Chdir(h.dir)

	if err := runShipcheckCmd(t, "--dir", "."); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	verifyArgs := findInvocation(readStubLog(t, h.logFile), "verify")
	for i, arg := range verifyArgs {
		if arg == "--dir" && i+1 < len(verifyArgs) {
			if verifyArgs[i+1] != h.dir {
				t.Fatalf("verify --dir = %q; want %q", verifyArgs[i+1], h.dir)
			}
			return
		}
	}
	t.Fatalf("verify argv missing --dir: %v", verifyArgs)
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
	if argvHas(verifyArgs, "--no-spec") {
		t.Errorf("spec-driven verify argv should not include --no-spec: %v", verifyArgs)
	}

	scorecardArgs := findInvocation(invocations, "scorecard")
	if !argvHas(scorecardArgs, "--spec") || !argvHas(scorecardArgs, specPath) {
		t.Errorf("scorecard argv missing --spec: %v", scorecardArgs)
	}
	if !argvHas(scorecardArgs, "--research-dir") || !argvHas(scorecardArgs, researchDir) {
		t.Errorf("scorecard argv missing --research-dir: %v", scorecardArgs)
	}

	apifyArgs := findInvocation(invocations, "apify-audit")
	if !argvHas(apifyArgs, "--research-dir") || !argvHas(apifyArgs, researchDir) {
		t.Errorf("apify-audit argv missing --research-dir: %v", apifyArgs)
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

func TestShipcheck_HTMLSyncStubUsesNoSpecForVerify(t *testing.T) {
	h := newShipcheckHarness(t)
	writeHTMLSyncStubMarker(t, h.dir)

	specPath := "/some/spec.yaml"
	if err := runShipcheckCmd(t, "--dir", h.dir, "--spec", specPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invocations := readStubLog(t, h.logFile)

	verifyArgs := findInvocation(invocations, "verify")
	if !argvHas(verifyArgs, "--no-spec") {
		t.Errorf("HTML sync-stub verify argv missing --no-spec: %v", verifyArgs)
	}
	if argvHas(verifyArgs, "--spec") || argvHas(verifyArgs, specPath) {
		t.Errorf("HTML sync-stub verify argv should not receive --spec: %v", verifyArgs)
	}
	if !argvHas(verifyArgs, "--fix") {
		t.Errorf("HTML sync-stub verify argv should preserve default --fix: %v", verifyArgs)
	}

	for _, leg := range []string{"dogfood", "scorecard"} {
		args := findInvocation(invocations, leg)
		if !argvHas(args, "--spec") || !argvHas(args, specPath) {
			t.Errorf("%s argv should still receive --spec; got %v", leg, args)
		}
		if argvHas(args, "--no-spec") {
			t.Errorf("%s argv should not receive --no-spec; got %v", leg, args)
		}
	}
}

func TestShipcheck_HTMLSyncStubWithoutSpecDoesNotPassSpecFlag(t *testing.T) {
	h := newShipcheckHarness(t)
	writeHTMLSyncStubMarker(t, h.dir)

	if err := runShipcheckCmd(t, "--dir", h.dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	verifyArgs := findInvocation(readStubLog(t, h.logFile), "verify")
	if argvHas(verifyArgs, "--no-spec") {
		t.Errorf("HTML sync-stub verify argv without --spec should not receive --no-spec: %v", verifyArgs)
	}
	if argvHas(verifyArgs, "--spec") {
		t.Errorf("HTML sync-stub verify argv without --spec should not receive --spec: %v", verifyArgs)
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

func TestShipcheck_AllowDestructivePassesToNonDogfoodLiveLegs(t *testing.T) {
	h := newShipcheckHarness(t)

	if err := runShipcheckCmd(t, "--dir", h.dir, "--allow-destructive"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	invocations := readStubLog(t, h.logFile)
	verifyArgs := findInvocation(invocations, "verify")
	if !argvHas(verifyArgs, "--allow-destructive") {
		t.Errorf("verify argv missing --allow-destructive: %v", verifyArgs)
	}
	scorecardArgs := findInvocation(invocations, "scorecard")
	if !argvHas(scorecardArgs, "--allow-destructive") {
		t.Errorf("scorecard argv missing --allow-destructive: %v", scorecardArgs)
	}
	dogfoodArgs := findInvocation(invocations, "dogfood")
	if argvHas(dogfoodArgs, "--allow-destructive") {
		t.Errorf("dogfood argv should remain out of scope for --allow-destructive forwarding: %v", dogfoodArgs)
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
	for _, leg := range []string{"dogfood", "workflow-verify", "apify-audit", "verify-skill", "scorecard"} {
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
	for _, leg := range []string{"dogfood", "verify", "workflow-verify", "apify-audit", "scorecard"} {
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

func TestShipcheck_JSONEnvelope_RedactsAPIKeyFromCommands(t *testing.T) {
	h := newShipcheckHarness(t)
	const secret = "secret-token"

	out := captureStdout(t, func() {
		if err := runShipcheckCmd(t, "--dir", h.dir, "--json", "--api-key", secret); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	var env shipcheckJSONEnvelope
	if err := json.Unmarshal([]byte(extractFinalJSONObject(t, out)), &env); err != nil {
		t.Fatalf("envelope is not valid JSON: %v", err)
	}

	var verifyCommand string
	for _, leg := range env.Legs {
		if strings.Contains(leg.Command, secret) {
			t.Fatalf("leg %s command leaked API key: %q", leg.Name, leg.Command)
		}
		if leg.Name == "verify" {
			verifyCommand = leg.Command
		}
	}
	if verifyCommand == "" {
		t.Fatal("envelope missing verify command")
	}
	if !strings.Contains(verifyCommand, "--api-key <redacted>") {
		t.Fatalf("verify command should include redacted API key flag; got %q", verifyCommand)
	}

	verifyArgs := findInvocation(readStubLog(t, h.logFile), "verify")
	if !argvHas(verifyArgs, secret) {
		t.Fatalf("verify subprocess argv should still receive the raw API key; got %v", verifyArgs)
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
		return
	}
	if failingLeg.Passed {
		t.Errorf("verify-skill leg should be passed=false")
	}
	if failingLeg.ExitCode != 1 {
		t.Errorf("verify-skill leg should have exit_code=1; got %d", failingLeg.ExitCode)
	}
}

func TestShipcheck_JSONEnvelope_DogfoodAndWorkflowVerifyFailure(t *testing.T) {
	h := newShipcheckHarness(t)
	t.Setenv("STUB_EXIT_DOGFOOD", "3")
	t.Setenv("STUB_EXIT_WORKFLOW_VERIFY", "3")

	out := captureStdout(t, func() {
		err := runShipcheckCmd(t, "--dir", h.dir, "--json")
		if err == nil {
			t.Fatal("expected non-nil error when dogfood and workflow-verify fail")
		}
		exitErr, ok := err.(*ExitError)
		if !ok {
			t.Fatalf("expected *ExitError; got %T: %v", err, err)
		}
		if exitErr.Code != ExitGenerationError {
			t.Fatalf("umbrella exit code = %d, want %d", exitErr.Code, ExitGenerationError)
		}
	})

	var env shipcheckJSONEnvelope
	if err := json.Unmarshal([]byte(extractFinalJSONObject(t, out)), &env); err != nil {
		t.Fatalf("envelope is not valid JSON: %v", err)
	}
	if env.Passed || env.Verdict != "FAIL" || env.ExitCode != ExitGenerationError {
		t.Fatalf("envelope = passed=%v verdict=%s exit=%d, want FAIL exit %d", env.Passed, env.Verdict, env.ExitCode, ExitGenerationError)
	}

	saw := map[string]bool{}
	for _, leg := range env.Legs {
		if leg.Name == "dogfood" || leg.Name == "workflow-verify" {
			saw[leg.Name] = true
			if leg.Passed || leg.Verdict != "FAIL" || leg.ExitCode != ExitGenerationError {
				t.Errorf("%s leg = passed=%v verdict=%s exit=%d, want FAIL exit %d",
					leg.Name, leg.Passed, leg.Verdict, leg.ExitCode, ExitGenerationError)
			}
		}
	}
	if !saw["dogfood"] || !saw["workflow-verify"] {
		t.Fatalf("envelope missing dogfood or workflow-verify legs: %v", saw)
	}
}

func TestShipcheck_HoldsOnUnverifiedScorecard(t *testing.T) {
	h := newShipcheckHarness(t)
	if err := os.WriteFile(filepath.Join(h.dir, pipeline.CLIManifestFilename), []byte(`{
  "api_name": "example",
  "scorecard": {
    "unverified_dimensions": ["auth_protocol", "live_api_verification"]
  }
}
`), 0o644); err != nil {
		t.Fatalf("writing manifest: %v", err)
	}

	out := captureStdout(t, func() {
		err := runShipcheckCmd(t, "--dir", h.dir)
		if err == nil {
			t.Fatal("expected shipcheck hold")
		}
		exitErr, ok := err.(*ExitError)
		if !ok {
			t.Fatalf("expected *ExitError; got %T", err)
		}
		if exitErr.Code != ExitGenerationError {
			t.Fatalf("hold exit code = %d, want %d", exitErr.Code, ExitGenerationError)
		}
	})

	if !strings.Contains(out, "HOLD") {
		t.Errorf("shipcheck output missing HOLD: %q", out)
	}
	if !strings.Contains(out, "unverified") {
		t.Errorf("shipcheck output missing unverified detail: %q", out)
	}
}

func TestShipcheck_AllowsUnscoredLiveAPIVerification(t *testing.T) {
	h := newShipcheckHarness(t)
	cliDir := filepath.Join(h.dir, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatalf("creating CLI fixture: %v", err)
	}
	const cliSource = `package cli

func widgetsPath() string { return "/widgets" }
`
	if err := os.WriteFile(filepath.Join(cliDir, "widgets.go"), []byte(cliSource), 0o644); err != nil {
		t.Fatalf("writing CLI fixture: %v", err)
	}
	specPath := filepath.Join(h.dir, "spec.yaml")
	const spec = `name: widgets
version: "1.0.0"
base_url: https://api.example.com
resources:
  widgets:
    endpoints:
      list:
        method: GET
        path: /widgets
`
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatalf("writing spec fixture: %v", err)
	}

	sc, err := pipeline.RunScorecard(h.dir, t.TempDir(), specPath, nil)
	if err != nil {
		t.Fatalf("running scorecard: %v", err)
	}
	if _, err := pipeline.PersistScorecardToManifest(filepath.Join(h.dir, pipeline.CLIManifestFilename), sc, ""); err != nil {
		t.Fatalf("persisting scorecard: %v", err)
	}

	if err := runShipcheckCmd(t, "--dir", h.dir); err != nil {
		t.Fatalf("unscored live API verification should not hold shipping: %v", err)
	}
}

func TestShipcheck_HoldsWithoutScorecardManifestEvidence(t *testing.T) {
	h := newShipcheckHarness(t)
	if err := os.Remove(filepath.Join(h.dir, pipeline.CLIManifestFilename)); err != nil {
		t.Fatalf("removing manifest: %v", err)
	}

	out := captureStdout(t, func() {
		err := runShipcheckCmd(t, "--dir", h.dir)
		if err == nil {
			t.Fatal("expected shipcheck hold")
		}
		exitErr, ok := err.(*ExitError)
		if !ok {
			t.Fatalf("expected *ExitError; got %T", err)
		}
		if exitErr.Code != ExitGenerationError {
			t.Fatalf("hold exit code = %d, want %d", exitErr.Code, ExitGenerationError)
		}
	})

	if !strings.Contains(out, "HOLD") || !strings.Contains(out, "manifest evidence") {
		t.Errorf("shipcheck output missing manifest-evidence hold: %q", out)
	}
}

func TestShipcheck_JSONEnvelope_HoldsOnUnverifiedScorecard(t *testing.T) {
	h := newShipcheckHarness(t)
	if err := os.WriteFile(filepath.Join(h.dir, pipeline.CLIManifestFilename), []byte(`{
  "api_name": "example",
  "scorecard": {"unverified_dimensions": ["auth_protocol"]}
}
`), 0o644); err != nil {
		t.Fatalf("writing manifest: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runShipcheckCmd(t, "--dir", h.dir, "--json"); err == nil {
			t.Fatal("expected shipcheck hold")
		}
	})
	var env shipcheckJSONEnvelope
	if err := json.Unmarshal([]byte(extractFinalJSONObject(t, out)), &env); err != nil {
		t.Fatalf("envelope is not valid JSON: %v", err)
	}
	if env.Passed || env.Verdict != "HOLD" || env.ExitCode != ExitGenerationError {
		t.Fatalf("unexpected hold envelope: %+v", env)
	}
	if !strings.Contains(env.Reason, "auth_protocol") {
		t.Fatalf("hold envelope missing dimension reason: %+v", env)
	}
	var scorecardLeg *shipcheckJSONLeg
	for i := range env.Legs {
		if env.Legs[i].Name == "scorecard" {
			scorecardLeg = &env.Legs[i]
			break
		}
	}
	if scorecardLeg == nil || scorecardLeg.Verdict != "HOLD" || scorecardLeg.Passed || scorecardLeg.ExitCode != ExitGenerationError {
		t.Fatalf("unexpected scorecard leg: %+v", scorecardLeg)
	}
}

func TestShipcheck_JSONEnvelopeFailureTakesPrecedenceOverHold(t *testing.T) {
	h := newShipcheckHarness(t)
	t.Setenv("STUB_EXIT_VERIFY_SKILL", "1")
	if err := os.WriteFile(filepath.Join(h.dir, pipeline.CLIManifestFilename), []byte(`{
  "api_name": "example",
  "scorecard": {"unverified_dimensions": ["auth_protocol"]}
}
`), 0o644); err != nil {
		t.Fatalf("writing manifest: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runShipcheckCmd(t, "--dir", h.dir, "--json"); err == nil {
			t.Fatal("expected shipcheck failure")
		}
	})
	var env shipcheckJSONEnvelope
	if err := json.Unmarshal([]byte(extractFinalJSONObject(t, out)), &env); err != nil {
		t.Fatalf("envelope is not valid JSON: %v", err)
	}
	if env.Passed || env.Verdict != "FAIL" || env.ExitCode != ExitGenerationError {
		t.Fatalf("genuine failure must take precedence over hold: %+v", env)
	}
	var failedLeg *shipcheckJSONLeg
	for i := range env.Legs {
		if env.Legs[i].Name == "verify-skill" {
			failedLeg = &env.Legs[i]
			break
		}
	}
	if failedLeg == nil || failedLeg.Verdict != "FAIL" || failedLeg.Passed {
		t.Fatalf("unexpected genuine failure leg: %+v", failedLeg)
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

const shipcheckAcceptanceRunID = "run-acceptance"

func shipcheckPolicyHarness(t *testing.T) shipcheckHarness {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("PRINTING_PRESS_HOME", filepath.Join(home, "printing-press"))
	logFile := filepath.Join(t.TempDir(), "stub.log")
	t.Setenv("STUB_LOG_FILE", logFile)
	return shipcheckHarness{
		dir:     fakeCLIDir(t),
		logFile: logFile,
	}
}

func writeShipcheckAcceptanceManifest(t *testing.T, dir, runID string) {
	t.Helper()
	manifest := pipeline.CLIManifest{
		APIName:  "example",
		CLIName:  "example-pp-cli",
		RunID:    runID,
		AuthType: "none",
		Scorecard: &pipeline.CLIManifestScorecard{
			UnverifiedDimensions: []string{},
		},
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshaling manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, pipeline.CLIManifestFilename), append(data, '\n'), 0o644); err != nil {
		t.Fatalf("writing manifest: %v", err)
	}
}

func shipcheckSourcePath(dir string) string {
	return filepath.Join(dir, "main.go")
}

// writeShipcheckAcceptance mints a gate-passing phase5-acceptance.json in the
// CLI's embedded proofs directory and returns the marker path plus the source
// file the fingerprint covers.
func writeShipcheckAcceptance(t *testing.T, dir string) (string, string) {
	t.Helper()
	sourcePath := shipcheckSourcePath(dir)
	if err := os.WriteFile(sourcePath, []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("writing source: %v", err)
	}
	source, err := pipeline.CaptureSourceFingerprint(dir)
	if err != nil {
		t.Fatalf("fingerprinting CLI: %v", err)
	}
	writeShipcheckAcceptanceManifest(t, dir, shipcheckAcceptanceRunID)
	marker := pipeline.Phase5GateMarker{
		SchemaVersion:     1,
		APIName:           "example",
		RunID:             shipcheckAcceptanceRunID,
		Status:            "pass",
		Level:             "full",
		MatrixSize:        1,
		TestsPassed:       1,
		SourceFingerprint: source.Digest,
		SourceFiles:       source.Files,
		AuthContext:       pipeline.Phase5AuthContext{Type: "none"},
	}
	proofsDir := filepath.Join(dir, ".manuscripts", shipcheckAcceptanceRunID, "proofs")
	if err := os.MkdirAll(proofsDir, 0o755); err != nil {
		t.Fatalf("creating proofs dir: %v", err)
	}
	markerPath := filepath.Join(proofsDir, pipeline.Phase5AcceptanceFilename)
	writeShipcheckMarkerFile(t, markerPath, marker)
	return markerPath, sourcePath
}

func writeShipcheckMarkerFile(t *testing.T, path string, marker pipeline.Phase5GateMarker) {
	t.Helper()
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		t.Fatalf("marshaling marker: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating marker dir: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("writing marker: %v", err)
	}
}

func shipcheckAcceptanceValid(t *testing.T, dir, proofsDir string) bool {
	t.Helper()
	manifest, err := pipeline.ReadCLIManifest(dir)
	if err != nil {
		t.Fatalf("reading manifest: %v", err)
	}
	result := pipeline.ValidatePhase5Gate(proofsDir, manifest, dir)
	return result.Passed
}

func runShipcheckCaptured(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runWithCapturedStderr(t, func() error {
		return runShipcheckCmd(t, args...)
	})
}

func TestShipcheck_PostAcceptanceVerifyFixPolicy(t *testing.T) {
	useShipcheckStub(t)

	t.Run("valid marker leaves tree and omits fix", func(t *testing.T) {
		h := shipcheckPolicyHarness(t)
		markerPath, sourcePath := writeShipcheckAcceptance(t, h.dir)
		beforeSource, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("reading source: %v", err)
		}
		beforeMarker, err := os.ReadFile(markerPath)
		if err != nil {
			t.Fatalf("reading marker: %v", err)
		}
		t.Setenv("STUB_VERIFY_FIX_TOUCH", sourcePath)

		stderr, err := runShipcheckCaptured(t, "--dir", h.dir)
		if err != nil {
			t.Fatalf("shipcheck after a valid acceptance marker: %v", err)
		}
		verifyArgs := findInvocation(readStubLog(t, h.logFile), "verify")
		if argvHas(verifyArgs, "--fix") {
			t.Fatalf("verify argv = %v, want no --fix while the acceptance fingerprint matches", verifyArgs)
		}
		if !strings.Contains(stderr, "verify will run without --fix") {
			t.Fatalf("stderr = %q, want read-only notice", stderr)
		}
		if strings.Contains(stderr, "invalidates the phase5 acceptance marker") {
			t.Fatalf("read-only shipcheck warned that it invalidated the marker: %s", stderr)
		}
		afterSource, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("reading source after shipcheck: %v", err)
		}
		if string(afterSource) != string(beforeSource) {
			t.Fatalf("source changed:\n%s", afterSource)
		}
		afterMarker, err := os.ReadFile(markerPath)
		if err != nil {
			t.Fatalf("reading marker after shipcheck: %v", err)
		}
		if string(afterMarker) != string(beforeMarker) {
			t.Fatalf("marker file changed:\n%s", afterMarker)
		}
		proofsDir := filepath.Dir(markerPath)
		if !shipcheckAcceptanceValid(t, h.dir, proofsDir) {
			t.Fatal("acceptance fingerprint no longer matches after read-only shipcheck")
		}
	})

	t.Run("explicit fix still fixes and warns", func(t *testing.T) {
		h := shipcheckPolicyHarness(t)
		markerPath, sourcePath := writeShipcheckAcceptance(t, h.dir)
		beforeSource, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("reading source: %v", err)
		}
		t.Setenv("STUB_VERIFY_FIX_TOUCH", sourcePath)

		stderr, err := runShipcheckCaptured(t, "--dir", h.dir, "--fix")
		if err != nil {
			t.Fatalf("shipcheck --fix: %v", err)
		}
		verifyArgs := findInvocation(readStubLog(t, h.logFile), "verify")
		if !argvHas(verifyArgs, "--fix") {
			t.Fatalf("verify argv = %v, want --fix", verifyArgs)
		}
		if !strings.Contains(stderr, "invalidates the phase5 acceptance marker at "+markerPath) {
			t.Fatalf("stderr = %q, want invalidation warning for %s", stderr, markerPath)
		}
		afterSource, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("reading source after --fix: %v", err)
		}
		if string(afterSource) == string(beforeSource) {
			t.Fatal("verify --fix did not edit source")
		}
		if shipcheckAcceptanceValid(t, h.dir, filepath.Dir(markerPath)) {
			t.Fatal("acceptance fingerprint still matches after verify --fix edited source")
		}
	})

	t.Run("no-fix=false is an explicit fix", func(t *testing.T) {
		h := shipcheckPolicyHarness(t)
		markerPath, _ := writeShipcheckAcceptance(t, h.dir)

		stderr, err := runShipcheckCaptured(t, "--dir", h.dir, "--no-fix=false")
		if err != nil {
			t.Fatalf("shipcheck --no-fix=false: %v", err)
		}
		verifyArgs := findInvocation(readStubLog(t, h.logFile), "verify")
		if !argvHas(verifyArgs, "--fix") {
			t.Fatalf("verify argv = %v, want --fix", verifyArgs)
		}
		if !strings.Contains(stderr, "invalidates the phase5 acceptance marker at "+markerPath) {
			t.Fatalf("stderr = %q, want invalidation warning", stderr)
		}
	})

	t.Run("fix=false stays read-only", func(t *testing.T) {
		h := shipcheckPolicyHarness(t)
		_, sourcePath := writeShipcheckAcceptance(t, h.dir)
		t.Setenv("STUB_VERIFY_FIX_TOUCH", sourcePath)
		before, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("reading source: %v", err)
		}

		stderr, err := runShipcheckCaptured(t, "--dir", h.dir, "--fix=false")
		if err != nil {
			t.Fatalf("shipcheck --fix=false: %v", err)
		}
		verifyArgs := findInvocation(readStubLog(t, h.logFile), "verify")
		if argvHas(verifyArgs, "--fix") {
			t.Fatalf("verify argv = %v, want no --fix", verifyArgs)
		}
		if strings.Contains(stderr, "invalidates the phase5 acceptance marker") {
			t.Fatalf("read-only --fix=false warned about invalidation: %s", stderr)
		}
		after, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("reading source: %v", err)
		}
		if string(after) != string(before) {
			t.Fatalf("source changed under --fix=false:\n%s", after)
		}
	})

	t.Run("no-fix with valid marker does not warn", func(t *testing.T) {
		h := shipcheckPolicyHarness(t)
		_, sourcePath := writeShipcheckAcceptance(t, h.dir)
		t.Setenv("STUB_VERIFY_FIX_TOUCH", sourcePath)

		stderr, err := runShipcheckCaptured(t, "--dir", h.dir, "--no-fix")
		if err != nil {
			t.Fatalf("shipcheck --no-fix: %v", err)
		}
		verifyArgs := findInvocation(readStubLog(t, h.logFile), "verify")
		if argvHas(verifyArgs, "--fix") {
			t.Fatalf("verify argv = %v, want no --fix", verifyArgs)
		}
		if strings.Contains(stderr, "invalidates the phase5 acceptance marker") {
			t.Fatalf("--no-fix warned about invalidation: %s", stderr)
		}
	})

	t.Run("fix and no-fix cannot be combined", func(t *testing.T) {
		h := shipcheckPolicyHarness(t)
		writeShipcheckAcceptance(t, h.dir)

		_, err := runShipcheckCaptured(t, "--dir", h.dir, "--fix", "--no-fix")
		if err == nil {
			t.Fatal("expected mutually exclusive flags to fail")
		}
		exitErr, ok := err.(*ExitError)
		if !ok {
			t.Fatalf("expected *ExitError; got %T: %v", err, err)
		}
		if exitErr.Code != ExitInputError {
			t.Fatalf("exit code = %d, want %d", exitErr.Code, ExitInputError)
		}
		if !strings.Contains(err.Error(), "--fix and --no-fix cannot be combined") {
			t.Fatalf("error = %v, want mutual-exclusion message", err)
		}
		if _, statErr := os.Stat(h.logFile); !os.IsNotExist(statErr) {
			invocations := readStubLog(t, h.logFile)
			if len(invocations) != 0 {
				t.Fatalf("flags rejected after legs ran: %v", invocations)
			}
		}
	})

	t.Run("no marker keeps fix", func(t *testing.T) {
		h := shipcheckPolicyHarness(t)

		stderr, err := runShipcheckCaptured(t, "--dir", h.dir)
		if err != nil {
			t.Fatalf("shipcheck without a marker: %v", err)
		}
		verifyArgs := findInvocation(readStubLog(t, h.logFile), "verify")
		if !argvHas(verifyArgs, "--fix") {
			t.Fatalf("verify argv = %v, want --fix", verifyArgs)
		}
		if strings.Contains(stderr, "invalidates the phase5 acceptance marker") || strings.Contains(stderr, "without --fix") {
			t.Fatalf("stderr = %q, want today's default with no acceptance notice", stderr)
		}
	})

	t.Run("stale marker keeps fix", func(t *testing.T) {
		h := shipcheckPolicyHarness(t)
		_, sourcePath := writeShipcheckAcceptance(t, h.dir)
		if err := os.WriteFile(sourcePath, []byte("package main\n\nfunc changed() {}\n"), 0o644); err != nil {
			t.Fatalf("editing source: %v", err)
		}

		stderr, err := runShipcheckCaptured(t, "--dir", h.dir)
		if err != nil {
			t.Fatalf("shipcheck with a stale marker: %v", err)
		}
		verifyArgs := findInvocation(readStubLog(t, h.logFile), "verify")
		if !argvHas(verifyArgs, "--fix") {
			t.Fatalf("verify argv = %v, want --fix", verifyArgs)
		}
		if strings.Contains(stderr, "invalidates the phase5 acceptance marker") || strings.Contains(stderr, "without --fix") {
			t.Fatalf("stderr = %q, want today's default for a mismatched fingerprint", stderr)
		}
	})

	t.Run("corrupt marker falls back to fix", func(t *testing.T) {
		h := shipcheckPolicyHarness(t)
		writeShipcheckAcceptanceManifest(t, h.dir, shipcheckAcceptanceRunID)
		markerPath := filepath.Join(h.dir, ".manuscripts", shipcheckAcceptanceRunID, "proofs", pipeline.Phase5AcceptanceFilename)
		if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
			t.Fatalf("creating proofs dir: %v", err)
		}
		if err := os.WriteFile(markerPath, []byte("{"), 0o644); err != nil {
			t.Fatalf("writing corrupt marker: %v", err)
		}

		stderr, err := runShipcheckCaptured(t, "--dir", h.dir)
		if err != nil {
			t.Fatalf("corrupt marker crashed shipcheck: %v", err)
		}
		verifyArgs := findInvocation(readStubLog(t, h.logFile), "verify")
		if !argvHas(verifyArgs, "--fix") {
			t.Fatalf("verify argv = %v, want --fix", verifyArgs)
		}
		if strings.Contains(stderr, "invalidates the phase5 acceptance marker") || strings.Contains(stderr, "without --fix") {
			t.Fatalf("stderr = %q, want today's default for a corrupt marker", stderr)
		}
	})

	t.Run("unreadable marker falls back to fix", func(t *testing.T) {
		h := shipcheckPolicyHarness(t)
		writeShipcheckAcceptanceManifest(t, h.dir, shipcheckAcceptanceRunID)
		// A directory at the marker path fails ReadFile on every OS, including
		// when the process is root and mode bits would not.
		markerPath := filepath.Join(h.dir, ".manuscripts", shipcheckAcceptanceRunID, "proofs", pipeline.Phase5AcceptanceFilename)
		if err := os.MkdirAll(markerPath, 0o755); err != nil {
			t.Fatalf("creating unreadable marker path: %v", err)
		}

		stderr, err := runShipcheckCaptured(t, "--dir", h.dir)
		if err != nil {
			t.Fatalf("unreadable marker crashed shipcheck: %v", err)
		}
		verifyArgs := findInvocation(readStubLog(t, h.logFile), "verify")
		if !argvHas(verifyArgs, "--fix") {
			t.Fatalf("verify argv = %v, want --fix", verifyArgs)
		}
		if strings.Contains(stderr, "invalidates the phase5 acceptance marker") || strings.Contains(stderr, "without --fix") {
			t.Fatalf("stderr = %q, want today's default for an unreadable marker", stderr)
		}
	})

	t.Run("unreadable manifest falls back to fix", func(t *testing.T) {
		h := shipcheckPolicyHarness(t)
		manifestPath := filepath.Join(h.dir, pipeline.CLIManifestFilename)
		if err := os.Remove(manifestPath); err != nil {
			t.Fatalf("removing manifest: %v", err)
		}
		if err := os.Mkdir(manifestPath, 0o755); err != nil {
			t.Fatalf("replacing manifest with a directory: %v", err)
		}

		_, err := runShipcheckCaptured(t, "--dir", h.dir)
		if err == nil {
			t.Fatal("expected scorecard hold when the manifest cannot be read")
		}
		verifyArgs := findInvocation(readStubLog(t, h.logFile), "verify")
		if !argvHas(verifyArgs, "--fix") {
			t.Fatalf("verify argv = %v, want --fix when the manifest cannot be read", verifyArgs)
		}
	})

	t.Run("failed acceptance keeps fix", func(t *testing.T) {
		h := shipcheckPolicyHarness(t)
		markerPath, _ := writeShipcheckAcceptance(t, h.dir)
		data, err := os.ReadFile(markerPath)
		if err != nil {
			t.Fatalf("reading marker: %v", err)
		}
		updated := strings.Replace(string(data), `"status": "pass"`, `"status": "fail"`, 1)
		if updated == string(data) {
			t.Fatal("marker status was not replaced")
		}
		if err := os.WriteFile(markerPath, []byte(updated), 0o644); err != nil {
			t.Fatalf("writing failed marker: %v", err)
		}

		stderr, err := runShipcheckCaptured(t, "--dir", h.dir)
		if err != nil {
			t.Fatalf("shipcheck with a failed acceptance marker: %v", err)
		}
		verifyArgs := findInvocation(readStubLog(t, h.logFile), "verify")
		if !argvHas(verifyArgs, "--fix") {
			t.Fatalf("verify argv = %v, want --fix", verifyArgs)
		}
		if strings.Contains(stderr, "without --fix") {
			t.Fatalf("stderr = %q, want today's default for a non-passing marker", stderr)
		}
	})

	t.Run("runstate marker omits fix", func(t *testing.T) {
		h := shipcheckPolicyHarness(t)
		sourcePath := shipcheckSourcePath(h.dir)
		if err := os.WriteFile(sourcePath, []byte("package main\n"), 0o644); err != nil {
			t.Fatalf("writing source: %v", err)
		}
		source, err := pipeline.CaptureSourceFingerprint(h.dir)
		if err != nil {
			t.Fatalf("fingerprinting CLI: %v", err)
		}
		writeShipcheckAcceptanceManifest(t, h.dir, shipcheckAcceptanceRunID)
		state := pipeline.NewStateWithRun("example", h.dir, shipcheckAcceptanceRunID, "shipcheck-test")
		if err := state.Save(); err != nil {
			t.Fatalf("saving runstate: %v", err)
		}
		markerPath := filepath.Join(state.ProofsDir(), pipeline.Phase5AcceptanceFilename)
		writeShipcheckMarkerFile(t, markerPath, pipeline.Phase5GateMarker{
			SchemaVersion:     1,
			APIName:           "example",
			RunID:             shipcheckAcceptanceRunID,
			Status:            "pass",
			Level:             "full",
			MatrixSize:        1,
			TestsPassed:       1,
			SourceFingerprint: source.Digest,
			SourceFiles:       source.Files,
			AuthContext:       pipeline.Phase5AuthContext{Type: "none"},
		})
		before, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("reading source: %v", err)
		}
		t.Setenv("STUB_VERIFY_FIX_TOUCH", sourcePath)

		stderr, err := runShipcheckCaptured(t, "--dir", h.dir)
		if err != nil {
			t.Fatalf("shipcheck with a runstate acceptance marker: %v", err)
		}
		verifyArgs := findInvocation(readStubLog(t, h.logFile), "verify")
		if argvHas(verifyArgs, "--fix") {
			t.Fatalf("verify argv = %v, want no --fix", verifyArgs)
		}
		if !strings.Contains(stderr, "verify will run without --fix") {
			t.Fatalf("stderr = %q, want read-only notice", stderr)
		}
		after, err := os.ReadFile(sourcePath)
		if err != nil {
			t.Fatalf("reading source: %v", err)
		}
		if string(after) != string(before) {
			t.Fatalf("source changed:\n%s", after)
		}
		if !shipcheckAcceptanceValid(t, h.dir, state.ProofsDir()) {
			t.Fatal("runstate acceptance fingerprint no longer matches")
		}
	})
}
