package pressauth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// stateHomeEnv lets tests redirect ~/.press-auth to a temp directory without
// touching the real user home. Production callers leave this unset and the
// state directory resolves to "<UserHomeDir>/.press-auth".
const stateHomeEnv = "PRESSAUTH_HOME"

// stateDirName is the directory name under the user's home that holds the
// per-domain state files. Kept in one place so renames stay in sync.
const stateDirName = ".press-auth"

// aesKeySize is the AES-256 key length used for state encryption.
const aesKeySize = 32

// nonceSize is the GCM nonce length in bytes. AES-GCM requires a fresh
// nonce per encryption; we generate 12 random bytes via crypto/rand each
// time Save is called.
const nonceSize = 12

// ErrStateNotFound is returned by Load when no state file exists for a
// domain. Callers map this to ExitNotCaptured so the user gets a recovery
// hint pointing at `press-auth login`.
var ErrStateNotFound = errors.New("state not found")

// State is the in-memory representation of one captured login session. The
// JSON on-disk shape is fixed by stateFile below; the encrypted Cookies
// field is base64-encoded inside the file rather than carried in cleartext.
type State struct {
	Domain           string
	CapturedAt       time.Time
	Cookies          map[string]string
	RefreshEndpoint  string
	JWTCarrierCookie string
	JWTExpiry        time.Time
}

// stateFile is the wire shape of the on-disk JSON file. Field tags fix the
// JSON key names and order; both the marshaled output and any goldens depend
// on this struct exactly as written.
type stateFile struct {
	Domain           string    `json:"domain"`
	CapturedAt       time.Time `json:"captured_at"`
	CookiesEncrypted string    `json:"cookies_encrypted"`
	RefreshEndpoint  string    `json:"refresh_endpoint"`
	JWTCarrierCookie string    `json:"jwt_carrier_cookie"`
	JWTExpiry        time.Time `json:"jwt_expiry"`
}

// StateDir returns the absolute path of the press-auth state directory,
// honoring PRESSAUTH_HOME for tests so writes never leak into the real
// ~/.press-auth/. The directory is not created here; Save handles MkdirAll
// with the correct 0700 mode.
func StateDir() (string, error) {
	if override := os.Getenv(stateHomeEnv); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving user home directory: %w", err)
	}
	return filepath.Join(home, stateDirName), nil
}

// stateFilePath returns the absolute path of the state file for one
// domain. Filename is "<domain>.json". Domains are user-supplied, so the
// helper rejects any value that could escape the state directory: a real
// hostname never contains a path separator or "..", and the joined path
// must still resolve directly under the state directory.
func stateFilePath(domain string) (string, error) {
	if strings.ContainsRune(domain, '/') || strings.ContainsRune(domain, os.PathSeparator) || strings.Contains(domain, "..") {
		return "", fmt.Errorf("invalid domain %q: must not contain path separators or %q", domain, "..")
	}
	dir, err := StateDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, domain+".json")
	if filepath.Dir(path) != filepath.Clean(dir) {
		return "", fmt.Errorf("invalid domain %q: resolves outside the state directory", domain)
	}
	return path, nil
}

// Save encrypts s.Cookies with the per-domain key in the macOS keychain
// (generating one if absent), serializes the resulting stateFile to JSON,
// and writes it atomically with mode 0600. The parent directory is created
// with mode 0700 on first write.
func Save(s *State) error {
	if s == nil {
		return errors.New("nil state")
	}
	if s.Domain == "" {
		return errors.New("state has no domain")
	}

	dir, err := StateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}
	// MkdirAll is a no-op when the directory already exists, so it leaves
	// pre-existing wider modes in place. Tighten explicitly so cookie state
	// never lives in a world-readable directory.
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("tightening state directory mode: %w", err)
	}

	key, err := getOrCreateKey(s.Domain)
	if err != nil {
		return fmt.Errorf("getting encryption key: %w", err)
	}

	encoded, err := encryptCookies(key, s.Cookies)
	if err != nil {
		return fmt.Errorf("encrypting cookies: %w", err)
	}

	wire := stateFile{
		Domain:           s.Domain,
		CapturedAt:       s.CapturedAt,
		CookiesEncrypted: encoded,
		RefreshEndpoint:  s.RefreshEndpoint,
		JWTCarrierCookie: s.JWTCarrierCookie,
		JWTExpiry:        s.JWTExpiry,
	}
	data, err := json.MarshalIndent(&wire, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	path, err := stateFilePath(s.Domain)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data, 0o600)
}

// Load reads the state file for domain, fetches the per-domain key from the
// macOS keychain, and decrypts cookies into the returned State. Returns
// ErrStateNotFound when no file exists so callers can map it to the
// "not captured" recovery message.
func Load(domain string) (*State, error) {
	if domain == "" {
		return nil, errors.New("empty domain")
	}
	path, err := stateFilePath(domain)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrStateNotFound, domain)
		}
		return nil, fmt.Errorf("reading state file: %w", err)
	}

	var wire stateFile
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}

	key, err := loadKey(domain)
	if err != nil {
		return nil, fmt.Errorf("loading encryption key: %w", err)
	}
	if err := validateKeySize(domain, key); err != nil {
		// Guard before aes.NewCipher so a corrupted keychain entry yields
		// this actionable message instead of a cryptic cipher-size error.
		return nil, err
	}

	cookies, err := decryptCookies(key, wire.CookiesEncrypted)
	if err != nil {
		// Do not include the encrypted blob or any cookie bytes in the
		// error: the error message can leak into user-visible logs.
		return nil, fmt.Errorf("decrypting cookies: %w", err)
	}

	return &State{
		Domain:           wire.Domain,
		CapturedAt:       wire.CapturedAt,
		Cookies:          cookies,
		RefreshEndpoint:  wire.RefreshEndpoint,
		JWTCarrierCookie: wire.JWTCarrierCookie,
		JWTExpiry:        wire.JWTExpiry,
	}, nil
}

// Delete removes the state file and the matching keychain entry for
// domain. Both removals are best-effort idempotent: a missing file or
// keychain item is not an error so callers can use Delete as the body of
// `press-auth forget`. Other errors bubble up.
func Delete(domain string) error {
	if domain == "" {
		return errors.New("empty domain")
	}
	path, err := stateFilePath(domain)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing state file: %w", err)
	}
	if err := deleteKey(domain); err != nil {
		return fmt.Errorf("removing keychain entry: %w", err)
	}
	return nil
}

// getOrCreateKey returns the existing key for domain or generates a fresh
// AES-256 key and stores it in the keychain. A miss returned as (nil, nil)
// from loadKey is treated as "create one"; a real keychain error short-
// circuits with the underlying message.
// validateKeySize rejects a keychain entry that is not a full AES-256 key
// so the bytes never reach aes.NewCipher and surface as a cryptic cipher
// error. The message points the user at the recovery command.
func validateKeySize(domain string, key []byte) error {
	if len(key) != aesKeySize {
		return fmt.Errorf("keychain entry for %s has unexpected size %d (want %d); re-run press-auth login %s to recapture", domain, len(key), aesKeySize, domain)
	}
	return nil
}

func getOrCreateKey(domain string) ([]byte, error) {
	key, err := loadKey(domain)
	if err == nil {
		if err := validateKeySize(domain, key); err != nil {
			return nil, err
		}
		return key, nil
	}
	if !errors.Is(err, errKeyNotFound) {
		return nil, err
	}
	key = make([]byte, aesKeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generating key: %w", err)
	}
	if err := saveKey(domain, key); err != nil {
		return nil, fmt.Errorf("persisting key: %w", err)
	}
	return key, nil
}

// encryptCookies marshals cookies to JSON with sorted keys and seals the
// result with AES-256-GCM. The on-disk encoding is base64(nonce || sealed).
func encryptCookies(key []byte, cookies map[string]string) (string, error) {
	plain, err := marshalCookies(cookies)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generating nonce: %w", err)
	}
	sealed := aead.Seal(nil, nonce, plain, nil)
	out := make([]byte, 0, len(nonce)+len(sealed))
	out = append(out, nonce...)
	out = append(out, sealed...)
	return base64.StdEncoding.EncodeToString(out), nil
}

// decryptCookies reverses encryptCookies. A wrong key, tampered ciphertext,
// or short input all return a generic "ciphertext authentication failed"
// error class — never the decoded bytes.
func decryptCookies(key []byte, encoded string) (map[string]string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decoding ciphertext: %w", err)
	}
	if len(raw) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}
	nonce, sealed := raw[:nonceSize], raw[nonceSize:]
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := aead.Open(nil, nonce, sealed, nil)
	if err != nil {
		return nil, errors.New("ciphertext authentication failed")
	}
	return unmarshalCookies(plain)
}

// marshalCookies emits a JSON object with keys in lexicographic order so
// the encrypted blob is deterministic for a given cookie set + nonce. The
// stdlib json.Marshal already sorts map keys, but we go through an explicit
// pass to be defensive against future changes to that behavior.
func marshalCookies(cookies map[string]string) ([]byte, error) {
	if cookies == nil {
		return []byte("{}"), nil
	}
	names := make([]string, 0, len(cookies))
	for name := range cookies {
		names = append(names, name)
	}
	sort.Strings(names)
	ordered := make([]byte, 0, 32)
	ordered = append(ordered, '{')
	for i, name := range names {
		if i > 0 {
			ordered = append(ordered, ',')
		}
		nameBytes, err := json.Marshal(name)
		if err != nil {
			return nil, err
		}
		valueBytes, err := json.Marshal(cookies[name])
		if err != nil {
			return nil, err
		}
		ordered = append(ordered, nameBytes...)
		ordered = append(ordered, ':')
		ordered = append(ordered, valueBytes...)
	}
	ordered = append(ordered, '}')
	return ordered, nil
}

// unmarshalCookies parses the JSON cookie-bag emitted by marshalCookies.
func unmarshalCookies(data []byte) (map[string]string, error) {
	var out map[string]string
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]string{}
	}
	return out, nil
}

// writeFileAtomic writes data to path via a sibling temp file, fsyncs the
// temp file so the rename is durable, sets mode before rename so a reader
// never sees a world-readable intermediate, and renames into place. The
// rename is atomic on POSIX and effectively atomic on macOS; concurrent
// readers either see the old file, the new file, or os.IsNotExist —
// never a partial JSON write.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	// Best-effort cleanup if anything below fails before the rename.
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("setting temp file mode: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsyncing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming temp file into place: %w", err)
	}
	cleanup = false
	return nil
}
