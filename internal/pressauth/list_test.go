package pressauth

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestListEmptyDir(t *testing.T) {
	useTempHome(t)

	var out, errOut bytes.Buffer
	gf := &GlobalFlags{}
	if err := runList(&out, &errOut, gf, time.Now().UTC()); err != nil {
		t.Fatalf("runList: %v", err)
	}
	if !strings.Contains(out.String(), "no domains captured yet") {
		t.Errorf("expected empty-state message, got: %q", out.String())
	}
}

func TestListDirMissing(t *testing.T) {
	// Point PRESSAUTH_HOME at a path that doesn't exist on disk.
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	t.Setenv(stateHomeEnv, missing)

	var out, errOut bytes.Buffer
	gf := &GlobalFlags{}
	if err := runList(&out, &errOut, gf, time.Now().UTC()); err != nil {
		t.Fatalf("runList: %v", err)
	}
	if !strings.Contains(out.String(), "no domains captured yet") {
		t.Errorf("expected empty-state message for missing dir, got: %q", out.String())
	}
}

func TestListPopulatedSortsByDomain(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	now := time.Date(2026, 5, 12, 17, 54, 0, 0, time.UTC)

	domA := uniqueDomain(t)
	domB := uniqueDomain(t)
	domC := uniqueDomain(t)
	useTempHome(t, domA, domB, domC)

	stA := makeStateForStatus(domA, now.Add(-30*time.Minute), now.Add(30*time.Minute), makeJWT(t, now.Add(30*time.Minute)))
	stB := makeStateForStatus(domB, now.Add(-time.Hour), now.Add(45*time.Second), makeJWT(t, now.Add(45*time.Second)))
	stC := makeStateForStatus(domC, now.Add(-48*time.Hour), now.Add(-5*time.Minute), makeJWT(t, now.Add(-5*time.Minute)))
	for _, st := range []*State{stA, stB, stC} {
		if err := Save(st); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	var out, errOut bytes.Buffer
	gf := &GlobalFlags{}
	if err := runList(&out, &errOut, gf, now); err != nil {
		t.Fatalf("runList: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "DOMAIN") || !strings.Contains(got, "CAPTURED") {
		t.Errorf("missing table header: %q", got)
	}
	// All three rows must be present.
	for _, d := range []string{domA, domB, domC} {
		if !strings.Contains(got, d) {
			t.Errorf("missing domain %q in output: %q", d, got)
		}
	}
	// Sort check: domain indexes must be ascending.
	want := []string{domA, domB, domC}
	// Compute the lex-sorted order of the random domain names.
	for i := range want {
		for j := i + 1; j < len(want); j++ {
			if want[i] > want[j] {
				want[i], want[j] = want[j], want[i]
			}
		}
	}
	prev := -1
	for _, d := range want {
		idx := strings.Index(got, d)
		if idx < 0 {
			t.Fatalf("missing %q in output", d)
		}
		if idx <= prev {
			t.Errorf("domains out of order in output: %q", got)
		}
		prev = idx
	}

	// Per-state-kind decoration.
	if !strings.Contains(got, "valid (") {
		t.Errorf("expected valid row: %q", got)
	}
	if !strings.Contains(got, "near-expiry") {
		t.Errorf("expected near-expiry row: %q", got)
	}
	if !strings.Contains(got, "expired") {
		t.Errorf("expected expired row: %q", got)
	}
}

func TestListCorruptFileSurfacesWarning(t *testing.T) {
	home := useTempHome(t)
	// Write a deliberately malformed JSON file as a state entry.
	bad := filepath.Join(home, "broken.example.com.json")
	if err := os.WriteFile(bad, []byte("{this is not valid JSON"), 0o600); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}

	var out, errOut bytes.Buffer
	gf := &GlobalFlags{}
	if err := runList(&out, &errOut, gf, time.Now().UTC()); err != nil {
		t.Fatalf("runList: %v", err)
	}

	if !strings.Contains(out.String(), "broken.example.com") {
		t.Errorf("corrupt domain missing from table: %q", out.String())
	}
	if !strings.Contains(out.String(), "corrupt") {
		t.Errorf("expected 'corrupt' state in table: %q", out.String())
	}
	if !strings.Contains(errOut.String(), "broken.example.com") {
		t.Errorf("expected warning on stderr: %q", errOut.String())
	}
	if !strings.Contains(errOut.String(), "press-auth forget") {
		t.Errorf("expected recovery hint in warning: %q", errOut.String())
	}
}

func TestListCorruptDoesNotBlockGoodEntries(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	now := time.Date(2026, 5, 12, 17, 54, 0, 0, time.UTC)

	good := uniqueDomain(t)
	home := useTempHome(t, good)
	st := makeStateForStatus(good, now, now.Add(30*time.Minute), makeJWT(t, now.Add(30*time.Minute)))
	if err := Save(st); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// Seed a corrupt file alongside the good one.
	bad := filepath.Join(home, "broken.example.com.json")
	if err := os.WriteFile(bad, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("seed corrupt file: %v", err)
	}

	var out, errOut bytes.Buffer
	gf := &GlobalFlags{}
	if err := runList(&out, &errOut, gf, now); err != nil {
		t.Fatalf("runList: %v", err)
	}
	if !strings.Contains(out.String(), good) {
		t.Errorf("good domain missing: %q", out.String())
	}
	if !strings.Contains(out.String(), "broken.example.com") {
		t.Errorf("corrupt domain missing: %q", out.String())
	}
}

func TestListJSONOutput(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	now := time.Date(2026, 5, 12, 17, 54, 0, 0, time.UTC)
	domA := uniqueDomain(t)
	domB := uniqueDomain(t)
	useTempHome(t, domA, domB)

	stA := makeStateForStatus(domA, now, now.Add(time.Hour), makeJWT(t, now.Add(time.Hour)))
	stB := makeStateForStatus(domB, now, now.Add(-time.Hour), makeJWT(t, now.Add(-time.Hour)))
	for _, st := range []*State{stA, stB} {
		if err := Save(st); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}

	var out, errOut bytes.Buffer
	gf := &GlobalFlags{JSON: true}
	if err := runList(&out, &errOut, gf, now); err != nil {
		t.Fatalf("runList: %v", err)
	}

	var parsed []statusJSON
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatalf("unmarshal: %v\noutput=%q", err, out.String())
	}
	if len(parsed) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(parsed))
	}
	// Sort check.
	if parsed[0].Domain > parsed[1].Domain {
		t.Errorf("not sorted: %q before %q", parsed[0].Domain, parsed[1].Domain)
	}
	// Each row matches the documented shape.
	for _, row := range parsed {
		if row.Domain == "" {
			t.Errorf("row has empty domain: %+v", row)
		}
		if row.State == "" {
			t.Errorf("row has empty state: %+v", row)
		}
	}
}

func TestListJSONEmpty(t *testing.T) {
	useTempHome(t)
	var out, errOut bytes.Buffer
	gf := &GlobalFlags{JSON: true}
	if err := runList(&out, &errOut, gf, time.Now().UTC()); err != nil {
		t.Fatalf("runList: %v", err)
	}
	got := strings.TrimSpace(out.String())
	if got != "[]" {
		t.Errorf("expected '[]', got %q", got)
	}
}
