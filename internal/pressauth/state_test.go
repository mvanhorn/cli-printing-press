package pressauth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// uniqueDomain returns a random per-test domain so concurrent runs (or
// rerun-after-fail loops) never collide on the real keychain. The "-test"
// suffix makes orphaned entries trivial to spot in Keychain Access if
// cleanup ever fails to run.
func uniqueDomain(t *testing.T) string {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("randomizing test domain: %v", err)
	}
	return "press-auth-test-" + hex.EncodeToString(b[:]) + ".example.invalid"
}

// useTempHome redirects PRESSAUTH_HOME to a fresh temp dir for the rest of
// the test and arranges keychain cleanup for any per-test domains the
// caller passes in. Production state directory is never touched.
func useTempHome(t *testing.T, domains ...string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(stateHomeEnv, dir)
	for _, d := range domains {
		domain := d
		t.Cleanup(func() {
			if runtime.GOOS == "darwin" {
				_ = deleteKey(domain)
			}
		})
	}
	return dir
}

// sampleState returns a State with three cookies, useful for round-trips.
// Cookie values are obviously fake so a leaked log line is recognisable.
func sampleState(domain string, capturedAt time.Time) *State {
	return &State{
		Domain:     domain,
		CapturedAt: capturedAt,
		Cookies: map[string]string{
			"sessionid":    "redacted-value-1",
			"csrftoken":    "redacted-value-2",
			"guestsession": "redacted-value-3",
		},
		RefreshEndpoint:  "/account/token",
		JWTCarrierCookie: "guestsession",
		JWTExpiry:        capturedAt.Add(30 * time.Minute),
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Save/Load require macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	now := time.Date(2026, 5, 12, 17, 54, 0, 0, time.UTC)
	original := sampleState(domain, now)

	if err := Save(original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(domain)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if loaded.Domain != original.Domain {
		t.Errorf("Domain mismatch: got %q want %q", loaded.Domain, original.Domain)
	}
	if !loaded.CapturedAt.Equal(original.CapturedAt) {
		t.Errorf("CapturedAt mismatch: got %v want %v", loaded.CapturedAt, original.CapturedAt)
	}
	if !loaded.JWTExpiry.Equal(original.JWTExpiry) {
		t.Errorf("JWTExpiry mismatch: got %v want %v", loaded.JWTExpiry, original.JWTExpiry)
	}
	if loaded.RefreshEndpoint != original.RefreshEndpoint {
		t.Errorf("RefreshEndpoint mismatch: got %q want %q", loaded.RefreshEndpoint, original.RefreshEndpoint)
	}
	if loaded.JWTCarrierCookie != original.JWTCarrierCookie {
		t.Errorf("JWTCarrierCookie mismatch: got %q want %q", loaded.JWTCarrierCookie, original.JWTCarrierCookie)
	}
	if len(loaded.Cookies) != len(original.Cookies) {
		t.Fatalf("cookie count mismatch: got %d want %d", len(loaded.Cookies), len(original.Cookies))
	}
	for name, want := range original.Cookies {
		if loaded.Cookies[name] != want {
			// Do not log the actual values: cookies are sensitive.
			t.Errorf("cookie %q did not round-trip", name)
		}
	}
}

func TestLoadWithWrongKeyFailsCleanly(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	if err := Save(sampleState(domain, time.Now().UTC())); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Replace the keychain key with 32 random bytes; the saved ciphertext
	// can no longer be decrypted.
	wrongKey := make([]byte, aesKeySize)
	if _, err := rand.Read(wrongKey); err != nil {
		t.Fatalf("randomizing wrong key: %v", err)
	}
	if err := saveKey(domain, wrongKey); err != nil {
		t.Fatalf("saveKey wrong: %v", err)
	}

	loaded, err := Load(domain)
	if err == nil {
		t.Fatalf("Load with wrong key should error, got state: %+v", loaded)
	}
	if !strings.Contains(err.Error(), "decrypt") && !strings.Contains(err.Error(), "authentication") {
		t.Errorf("expected decrypt/auth error, got: %v", err)
	}
	// Decrypted cookie bytes must never reach the error message.
	for _, leak := range []string{"redacted-value-1", "redacted-value-2", "redacted-value-3"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("error leaked cookie value %q: %v", leak, err)
		}
	}
}

func TestLoadWrongKeySizeFailsCleanly(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS keychain")
	}
	domain := uniqueDomain(t)
	useTempHome(t, domain)

	if err := Save(sampleState(domain, time.Now().UTC())); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Simulate a corrupted keychain entry: a short key reaches Load.
	// Without the guard this surfaces as a cryptic aes.NewCipher error;
	// with it, Load reports the actionable size mismatch.
	if err := saveKey(domain, make([]byte, 16)); err != nil {
		t.Fatalf("saveKey short: %v", err)
	}

	_, err := Load(domain)
	if err == nil {
		t.Fatalf("Load with wrong-size key should error")
	}
	if !strings.Contains(err.Error(), "unexpected size") {
		t.Errorf("expected size-mismatch error, got: %v", err)
	}
}

func TestStateFilePathRejectsTraversal(t *testing.T) {
	dir := useTempHome(t)
	clean := filepath.Clean(dir)

	for _, bad := range []string{"../escape", "..", "a/b", "a/../../b", "foo/", "/abs"} {
		if _, err := stateFilePath(bad); err == nil {
			t.Errorf("stateFilePath(%q) = nil error, want rejection", bad)
		}
	}

	for _, good := range []string{"example.com", "app.example.co.uk", "127.0.0.1", "tenant.example.invalid"} {
		path, err := stateFilePath(good)
		if err != nil {
			t.Errorf("stateFilePath(%q) errored: %v", good, err)
			continue
		}
		if filepath.Dir(path) != clean {
			t.Errorf("stateFilePath(%q) = %q, not directly under %q", good, path, clean)
		}
	}
}

func TestSaveFileAndDirModes(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Save requires macOS keychain")
	}
	domain := uniqueDomain(t)
	home := useTempHome(t, domain)

	if err := Save(sampleState(domain, time.Now().UTC())); err != nil {
		t.Fatalf("Save: %v", err)
	}

	dirInfo, err := os.Stat(home)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("state dir mode = %o, want 0700", got)
	}

	fileInfo, err := os.Stat(filepath.Join(home, domain+".json"))
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Errorf("state file mode = %o, want 0600", got)
	}
}

func TestSaveIsAtomic(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Save requires macOS keychain")
	}
	domain := uniqueDomain(t)
	home := useTempHome(t, domain)
	path := filepath.Join(home, domain+".json")

	// Seed an initial state so the reader has something to observe while
	// Save races with it.
	if err := Save(sampleState(domain, time.Now().UTC())); err != nil {
		t.Fatalf("initial Save: %v", err)
	}

	var stop atomic.Bool
	var partialReads atomic.Int64
	var reads atomic.Int64

	var wg sync.WaitGroup
	wg.Go(func() {
		for !stop.Load() {
			data, err := os.ReadFile(path)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				continue
			}
			reads.Add(1)
			// A partial write would surface as JSON that does not parse.
			var probe map[string]any
			if jsonErr := json.Unmarshal(data, &probe); jsonErr != nil {
				partialReads.Add(1)
			}
		}
	})

	for i := range 50 {
		st := sampleState(domain, time.Now().UTC().Add(time.Duration(i)*time.Second))
		if err := Save(st); err != nil {
			stop.Store(true)
			wg.Wait()
			t.Fatalf("Save iter %d: %v", i, err)
		}
	}

	stop.Store(true)
	wg.Wait()

	if reads.Load() == 0 {
		t.Fatal("reader never observed the state file — test setup bug")
	}
	if partialReads.Load() != 0 {
		t.Errorf("reader saw %d partial JSON writes (expected 0)", partialReads.Load())
	}
}

func TestStateFileJSONShape(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Save requires macOS keychain")
	}
	domain := uniqueDomain(t)
	home := useTempHome(t, domain)

	captured := time.Date(2026, 5, 12, 17, 54, 0, 0, time.UTC)
	if err := Save(sampleState(domain, captured)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(home, domain+".json"))
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}

	// Field set, including no extras, in the documented order.
	wantOrder := []string{
		"domain",
		"captured_at",
		"cookies_encrypted",
		"refresh_endpoint",
		"jwt_carrier_cookie",
		"jwt_expiry",
	}
	got := string(data)
	prev := -1
	for _, key := range wantOrder {
		needle := `"` + key + `":`
		idx := strings.Index(got, needle)
		if idx < 0 {
			t.Errorf("state JSON missing field %q\n%s", key, got)
			continue
		}
		if idx <= prev {
			t.Errorf("state JSON field %q appears out of order\n%s", key, got)
		}
		prev = idx
	}

	// Reject any unexpected top-level keys.
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(data, &bag); err != nil {
		t.Fatalf("parse state JSON: %v", err)
	}
	if len(bag) != len(wantOrder) {
		gotKeys := make([]string, 0, len(bag))
		for k := range bag {
			gotKeys = append(gotKeys, k)
		}
		t.Errorf("unexpected state JSON keys: got %v want %v", gotKeys, wantOrder)
	}

	// Cleartext cookie names/values must not appear in the file.
	for _, secret := range []string{"redacted-value-1", "redacted-value-2", "redacted-value-3"} {
		if strings.Contains(got, secret) {
			t.Errorf("state file contained cleartext cookie value %q", secret)
		}
	}
}

func TestDeleteRemovesStateAndKey(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Delete requires macOS keychain")
	}
	domain := uniqueDomain(t)
	home := useTempHome(t, domain)

	if err := Save(sampleState(domain, time.Now().UTC())); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := Delete(domain); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := os.Stat(filepath.Join(home, domain+".json")); !os.IsNotExist(err) {
		t.Errorf("expected state file to be gone, stat err: %v", err)
	}

	if _, err := loadKey(domain); err == nil || !errors.Is(err, errKeyNotFound) {
		t.Errorf("expected errKeyNotFound after Delete, got: %v", err)
	}

	if _, err := Load(domain); !errors.Is(err, ErrStateNotFound) {
		t.Errorf("expected ErrStateNotFound after Delete, got: %v", err)
	}

	// Delete on already-missing state is idempotent.
	if err := Delete(domain); err != nil {
		t.Errorf("Delete on missing state should succeed, got: %v", err)
	}
}

func TestLoadMissingStateReturnsSentinel(t *testing.T) {
	useTempHome(t)
	_, err := Load("never-saved.example.invalid")
	if !errors.Is(err, ErrStateNotFound) {
		t.Errorf("expected ErrStateNotFound, got: %v", err)
	}
}

func TestStateDirHonorsEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(stateHomeEnv, dir)
	got, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	if got != dir {
		t.Errorf("StateDir = %q, want %q (PRESSAUTH_HOME override)", got, dir)
	}
}

func TestStateDirDefaultsToUserHome(t *testing.T) {
	t.Setenv(stateHomeEnv, "")
	got, err := StateDir()
	if err != nil {
		t.Fatalf("StateDir: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("user home unavailable: %v", err)
	}
	want := filepath.Join(home, stateDirName)
	if got != want {
		t.Errorf("StateDir = %q, want %q", got, want)
	}
}

func TestSaveDoesNotWriteToRealHome(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Save requires macOS keychain")
	}
	domain := uniqueDomain(t)
	home := useTempHome(t, domain)

	if err := Save(sampleState(domain, time.Now().UTC())); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// State file landed in the temp dir, not the real ~/.press-auth/.
	if _, err := os.Stat(filepath.Join(home, domain+".json")); err != nil {
		t.Errorf("state file missing from temp home: %v", err)
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("user home unavailable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(userHome, stateDirName, domain+".json")); !os.IsNotExist(err) {
		t.Errorf("Save leaked into real ~/.press-auth/: %v", err)
	}
}

// TestFixtureDecrypts loads the golden state file in testdata and verifies
// that decryptCookies produces the expected plaintext cookies when given
// the fixture's known key. This guards against accidental ciphertext-format
// changes (nonce layout, base64 alphabet, GCM tag length) that the
// round-trip test would miss because it shares both Save and Load.
func TestFixtureDecrypts(t *testing.T) {
	const fixtureRel = "../../testdata/pressauth/fixtures/example.com.json"
	data, err := os.ReadFile(fixtureRel)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var wire stateFile
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if wire.Domain != "example.com" {
		t.Errorf("fixture domain = %q, want example.com", wire.Domain)
	}

	key := []byte("press-auth-fixture-key-32-bytes!")
	if len(key) != aesKeySize {
		t.Fatalf("fixture key size = %d, want %d", len(key), aesKeySize)
	}
	cookies, err := decryptCookies(key, wire.CookiesEncrypted)
	if err != nil {
		t.Fatalf("decrypt fixture: %v", err)
	}
	want := map[string]string{
		"sessionid":    "fixture-session-1",
		"csrftoken":    "fixture-csrf-2",
		"guestsession": "fixture-jwt-3",
	}
	if len(cookies) != len(want) {
		t.Fatalf("cookie count: got %d want %d", len(cookies), len(want))
	}
	for k, v := range want {
		if cookies[k] != v {
			t.Errorf("cookie %q did not match fixture", k)
		}
	}
}

func TestKeychainStubOnNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-darwin stub only fires on Linux/Windows")
	}
	for name, call := range map[string]func() error{
		"loadKey":   func() error { _, err := loadKey("example.com"); return err },
		"saveKey":   func() error { return saveKey("example.com", make([]byte, aesKeySize)) },
		"deleteKey": func() error { return deleteKey("example.com") },
	} {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil {
				t.Fatalf("%s should error on non-darwin, got nil", name)
			}
			if !strings.Contains(err.Error(), "requires macOS") {
				t.Errorf("%s error should mention macOS, got: %v", name, err)
			}
		})
	}
}
