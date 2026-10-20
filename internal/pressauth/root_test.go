package pressauth

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCmd executes the press-auth Cobra tree with the given argv slice and
// returns captured stdout, stderr, and the resulting error. Tests use this
// helper instead of shelling out to a built binary so the suite stays
// hermetic and fast.
func runCmd(args []string) (stdout, stderr string, err error) {
	cmd := NewRootCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestRootHelpListsAllSubcommands(t *testing.T) {
	stdout, _, err := runCmd([]string{"--help"})
	if err != nil {
		t.Fatalf("--help should exit cleanly, got: %v", err)
	}

	wantSubs := []string{"login", "cookies", "status", "refresh", "list", "forget"}
	for _, sub := range wantSubs {
		if !strings.Contains(stdout, sub) {
			t.Errorf("--help output missing subcommand %q; got:\n%s", sub, stdout)
		}
	}
}

func TestLoginHelpListsExpectedFlags(t *testing.T) {
	stdout, _, err := runCmd([]string{"login", "--help"})
	if err != nil {
		t.Fatalf("login --help should exit cleanly, got: %v", err)
	}

	wantFlags := []string{"--login-url", "--complete-selector", "--complete-cookie", "--include-subdomains", "--refresh-endpoint", "--jwt-carrier-cookie", "--force"}
	for _, flag := range wantFlags {
		if !strings.Contains(stdout, flag) {
			t.Errorf("login --help output missing flag %q; got:\n%s", flag, stdout)
		}
	}
	if !strings.Contains(stdout, "every page tab") {
		t.Errorf("login --help output missing %q; got:\n%s", "every page tab", stdout)
	}
	if !strings.Contains(stdout, "neither of those flags is set") {
		t.Errorf("login --help should limit the sign-out heuristic to the case with no completion flag; got:\n%s", stdout)
	}
	if strings.Count(stdout, "replaces the sign-out heuristic") < 2 {
		t.Errorf("login --help should say both completion flags replace the sign-out heuristic; got:\n%s", stdout)
	}
	for _, phrase := range []string{"host-only", "RFC 6265", "Leave the flag off"} {
		if !strings.Contains(stdout, phrase) {
			t.Errorf("login --help output missing %q; got:\n%s", phrase, stdout)
		}
	}
}

func TestIncludeSubdomainsFlagDefaultsOff(t *testing.T) {
	cmd := newLoginCmd(nil)
	flag := cmd.Flags().Lookup("include-subdomains")
	if flag == nil {
		t.Fatal("missing --include-subdomains")
	}
	if flag.DefValue != "false" {
		t.Fatalf("default = %q, want false", flag.DefValue)
	}
	if err := cmd.ParseFlags([]string{"--include-subdomains=false"}); err != nil {
		t.Fatalf("parse explicit false: %v", err)
	}
	off, err := cmd.Flags().GetBool("include-subdomains")
	if err != nil {
		t.Fatal(err)
	}
	if off {
		t.Fatal("explicit --include-subdomains=false parsed as true")
	}

	onCmd := newLoginCmd(nil)
	if err := onCmd.ParseFlags([]string{"--include-subdomains"}); err != nil {
		t.Fatalf("parse flag: %v", err)
	}
	on, err := onCmd.Flags().GetBool("include-subdomains")
	if err != nil {
		t.Fatal(err)
	}
	if !on {
		t.Fatal("bare --include-subdomains parsed as false")
	}
}

func TestNoMatchedCookiesErrorMentionsSubdomains(t *testing.T) {
	err := noMatchedCookiesError("example.com", false)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected *ExitError, got %T", err)
	}
	if exitErr.Code != ExitNoNewCookies {
		t.Errorf("Code = %d, want ExitNoNewCookies", exitErr.Code)
	}
	if !strings.Contains(err.Error(), "--include-subdomains") {
		t.Errorf("default miss should mention --include-subdomains, got: %v", err)
	}
	if strings.Contains(noMatchedCookiesError("example.com", true).Error(), "--include-subdomains") {
		t.Error("error should not repeat --include-subdomains when the flag was already set")
	}
	wide := noMatchedCookiesError("example.com", true)
	if !errors.As(wide, &exitErr) {
		t.Fatalf("expected *ExitError, got %T", wide)
	}
	if exitErr.Code != ExitNoNewCookies {
		t.Errorf("wide Code = %d, want ExitNoNewCookies", exitErr.Code)
	}
}

func TestLoginRequiresDomainArg(t *testing.T) {
	_, stderr, err := runCmd([]string{"login"})
	if err == nil {
		t.Fatal("expected error when domain arg is missing, got nil")
	}

	// Should not be an ExitError-wrapped panic; Cobra surfaces a clean usage error.
	combined := stderr + err.Error()
	if !strings.Contains(combined, "accepts 1 arg") && !strings.Contains(combined, "requires") && !strings.Contains(combined, "arg(s)") {
		t.Errorf("expected a usage error mentioning missing arg, got:\n  err: %v\n  stderr: %s", err, stderr)
	}

	// No stack trace leaked into stderr.
	if strings.Contains(stderr, "goroutine ") || strings.Contains(stderr, ".go:") {
		t.Errorf("usage error leaked a stack trace; stderr:\n%s", stderr)
	}
}

func TestCookiesMissingStateReturnsRecoveryPath(t *testing.T) {
	_, _, err := runCmd([]string{"cookies", "missing.example.com"})
	if err == nil {
		t.Fatal("expected error from cookies subcommand without captured state, got nil")
	}

	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected an *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != ExitNotCaptured {
		t.Errorf("expected ExitNotCaptured (%d), got code %d", ExitNotCaptured, exitErr.Code)
	}
	if !strings.Contains(err.Error(), "press-auth login") {
		t.Errorf("error message should name the recovery command, got: %v", err)
	}
}

func TestUnknownSubcommand(t *testing.T) {
	_, _, err := runCmd([]string{"bogus"})
	if err == nil {
		t.Fatal("expected error for unknown subcommand, got nil")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("expected 'unknown command' in error, got: %v", err)
	}
}

// Note: the prior TestSubcommandStubsReturnNotImplemented covered status,
// list, and forget while those subcommands were unimplemented stubs.
// Real behaviour for all three lands in U5 and is covered by
// status_test.go, list_test.go, and forget_test.go.

// TestLoginRequiresLoginURL exercises the U3 flag-validation path: when the
// caller passes a domain but omits --login-url, login should refuse with a
// usage-error exit code rather than launching chromedp.
func TestLoginRequiresLoginURL(t *testing.T) {
	_, _, err := runCmd([]string{"login", "example.com"})
	if err == nil {
		t.Fatal("expected error when --login-url is missing, got nil")
	}
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected an *ExitError, got %T: %v", err, err)
	}
	if exitErr.Code != ExitUsageError {
		t.Errorf("expected ExitUsageError (%d), got code %d", ExitUsageError, exitErr.Code)
	}
	if !strings.Contains(err.Error(), "--login-url") {
		t.Errorf("error should mention --login-url, got: %v", err)
	}
}

// TestLoginRejectsInsecureURL guards the http:// vs https:// branch in
// validateLoginURL. http://localhost is allowed; http://anywhere-else is
// not.
func TestLoginRejectsInsecureURL(t *testing.T) {
	_, _, err := runCmd([]string{"login", "example.com", "--login-url", "http://example.com/login"})
	if err == nil {
		t.Fatal("expected error for plain http login URL, got nil")
	}
	if !strings.Contains(err.Error(), "https") {
		t.Errorf("error should explain https requirement, got: %v", err)
	}
}

func TestGlobalFlagsRegistered(t *testing.T) {
	cmd := NewRootCmd()
	for _, name := range []string{"json", "quiet", "config"} {
		if cmd.PersistentFlags().Lookup(name) == nil {
			t.Errorf("expected persistent flag --%s on root command", name)
		}
	}
}

func TestConfigFlagOverridesStateDirectory(t *testing.T) {
	envDir := t.TempDir()
	configDir := t.TempDir()
	t.Setenv(stateHomeEnv, envDir)

	if err := os.WriteFile(filepath.Join(envDir, "env-only.example.json"), []byte(`{`), 0o600); err != nil {
		t.Fatalf("seed env state file: %v", err)
	}

	stdout, _, err := runCmd([]string{"--config", configDir, "list"})
	if err != nil {
		t.Fatalf("list with --config: %v", err)
	}
	if strings.Contains(stdout, "env-only.example") {
		t.Fatalf("--config did not override %s; stdout:\n%s", stateHomeEnv, stdout)
	}
	if !strings.Contains(stdout, listEmptyMessage) {
		t.Fatalf("expected empty config dir output, got:\n%s", stdout)
	}
}
