package pressauth

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestForgetSingleDomainWithYes(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	home := useTempHome(t, domain)

	if err := Save(sampleState(domain, time.Now().UTC())); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var out bytes.Buffer
	ff := &ForgetFlags{Yes: true}
	if err := runForget(strings.NewReader(""), &out, ff, []string{domain}); err != nil {
		t.Fatalf("runForget: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, domain+".json")); !os.IsNotExist(err) {
		t.Errorf("state file still present after forget: %v", err)
	}
	if _, err := loadKey(domain); err == nil || !errors.Is(err, errKeyNotFound) {
		t.Errorf("expected keychain entry gone, got err=%v", err)
	}
	if !strings.Contains(out.String(), "forgot "+domain) {
		t.Errorf("expected 'forgot' message: %q", out.String())
	}
}

func TestForgetMissingDomainWithYes(t *testing.T) {
	useTempHome(t, "never-saved.example.invalid")
	var out bytes.Buffer
	ff := &ForgetFlags{Yes: true}
	err := runForget(strings.NewReader(""), &out, ff, []string{"never-saved.example.invalid"})
	if err != nil {
		t.Fatalf("forget on missing state should succeed, got: %v", err)
	}
	if !strings.Contains(out.String(), "nothing to forget") {
		t.Errorf("expected 'nothing to forget' message: %q", out.String())
	}
}

func TestForgetAllWithYes(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	now := time.Now().UTC()
	d1 := uniqueDomain(t)
	d2 := uniqueDomain(t)
	d3 := uniqueDomain(t)
	home := useTempHome(t, d1, d2, d3)

	for _, d := range []string{d1, d2, d3} {
		if err := Save(sampleState(d, now)); err != nil {
			t.Fatalf("Save %s: %v", d, err)
		}
	}

	var out bytes.Buffer
	ff := &ForgetFlags{All: true, Yes: true}
	if err := runForget(strings.NewReader(""), &out, ff, nil); err != nil {
		t.Fatalf("runForget: %v", err)
	}
	if !strings.Contains(out.String(), "forgot 3 of 3 domains") {
		t.Errorf("expected summary line: %q", out.String())
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			t.Errorf("state file left over: %s", e.Name())
		}
	}
}

func TestForgetAllEmptyDir(t *testing.T) {
	useTempHome(t)
	var out bytes.Buffer
	ff := &ForgetFlags{All: true, Yes: true}
	if err := runForget(strings.NewReader(""), &out, ff, nil); err != nil {
		t.Fatalf("runForget: %v", err)
	}
	if !strings.Contains(out.String(), "no domains to forget") {
		t.Errorf("expected 'no domains to forget': %q", out.String())
	}
}

func TestForgetSingleDomainNonInteractiveRefuses(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)
	if err := Save(sampleState(domain, time.Now().UTC())); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var out bytes.Buffer
	ff := &ForgetFlags{}
	// Pass nil reader to simulate "no scripted stdin". isInteractive will
	// fall back to stat-ing os.Stdin, which under `go test` is not a
	// character device.
	err := runForget(nil, &out, ff, []string{domain})
	if err == nil {
		t.Fatal("expected refusal in non-interactive mode, got nil")
	}
	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected *ExitError, got %T", err)
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("error should name --yes, got: %v", err)
	}
}

func TestForgetSingleDomainYAnswer(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	home := useTempHome(t, domain)
	if err := Save(sampleState(domain, time.Now().UTC())); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var out bytes.Buffer
	ff := &ForgetFlags{}
	if err := runForget(strings.NewReader("y\n"), &out, ff, []string{domain}); err != nil {
		t.Fatalf("runForget: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, domain+".json")); !os.IsNotExist(err) {
		t.Errorf("expected state gone, stat err: %v", err)
	}
}

func TestForgetSingleDomainNAnswer(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	home := useTempHome(t, domain)
	if err := Save(sampleState(domain, time.Now().UTC())); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var out bytes.Buffer
	ff := &ForgetFlags{}
	if err := runForget(strings.NewReader("n\n"), &out, ff, []string{domain}); err != nil {
		t.Fatalf("runForget: %v", err)
	}
	if !strings.Contains(out.String(), "cancelled") {
		t.Errorf("expected 'cancelled' message: %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(home, domain+".json")); err != nil {
		t.Errorf("expected state file preserved, stat err: %v", err)
	}
}

func TestForgetAllNonInteractiveWithoutYesRefuses(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	d1 := uniqueDomain(t)
	useTempHome(t, d1)
	if err := Save(sampleState(d1, time.Now().UTC())); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var out bytes.Buffer
	ff := &ForgetFlags{All: true}
	err := runForget(nil, &out, ff, nil)
	if err == nil {
		t.Fatal("expected refusal, got nil")
	}
	if !strings.Contains(err.Error(), "--yes") {
		t.Errorf("expected --yes in error: %v", err)
	}
}
