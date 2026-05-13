package pressauth

import (
	"bytes"
	"errors"
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

	wantFlags := []string{"--login-url", "--complete-selector", "--refresh-endpoint", "--jwt-carrier-cookie", "--force"}
	for _, flag := range wantFlags {
		if !strings.Contains(stdout, flag) {
			t.Errorf("login --help output missing flag %q; got:\n%s", flag, stdout)
		}
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

func TestSubcommandStubsReturnNotImplemented(t *testing.T) {
	// login (U3, chromedp capture) and refresh (U4, lazy refresh) are no
	// longer stubs; both have their own coverage in chrome_test.go and
	// refresh_test.go. The remaining stubs land in U5.
	cases := []struct {
		name string
		args []string
	}{
		{"status", []string{"status", "example.com"}},
		{"list", []string{"list"}},
		{"forget", []string{"forget", "example.com"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := runCmd(tc.args)
			if err == nil {
				t.Fatalf("%s stub should return an error, got nil", tc.name)
			}
			if !errors.Is(err, ErrNotImplemented) {
				t.Errorf("%s stub should wrap ErrNotImplemented, got: %v", tc.name, err)
			}
		})
	}
}

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
