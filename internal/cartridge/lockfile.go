package cartridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const LockSchemaVersion = "pp-lock/v1"

// Lockfile pins SHA-256 digests and trust boundaries for every installed
// cartridge and prebuilt binary so silent upstream mutations fail closed.
type Lockfile struct {
	Schema  string               `json:"schema"`
	Entries map[string]LockEntry `json:"entries"`
}

// LockEntry records the pinned identity of one installed tool.
type LockEntry struct {
	Kind         string    `json:"kind"` // "cartridge" or "binary"
	Name         string    `json:"name"`
	Version      string    `json:"version,omitempty"`
	SHA256       string    `json:"sha256"`
	Path         string    `json:"path"`
	Source       string    `json:"source,omitempty"`
	AllowedHosts []string  `json:"allowed_hosts,omitempty"`
	PinnedAt     time.Time `json:"pinned_at"`
}

// LockfilePath returns the canonical lockfile path (~/.pp/lock.json).
func LockfilePath() (string, error) {
	base, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "lock.json"), nil
}

// LoadLockfile reads ~/.pp/lock.json, returning an empty lockfile when absent.
func LoadLockfile(path string) (*Lockfile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Lockfile{
				Schema:  LockSchemaVersion,
				Entries: make(map[string]LockEntry),
			}, nil
		}
		return nil, fmt.Errorf("reading lockfile: %w", err)
	}
	var lf Lockfile
	if err := json.Unmarshal(raw, &lf); err != nil {
		return nil, fmt.Errorf("parsing lockfile %s: %w", path, err)
	}
	if lf.Schema == "" {
		lf.Schema = LockSchemaVersion
	}
	if lf.Entries == nil {
		lf.Entries = make(map[string]LockEntry)
	}
	return &lf, nil
}

// UpdateLockfile acquires an exclusive cross-process lock around loading,
// mutating, and atomically saving ~/.pp/lock.json so concurrent installs never
// clobber each other's pins.
func UpdateLockfile(path string, fn func(*Lockfile) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating lockfile dir: %w", err)
	}
	unlock, err := acquireFileLock(path+".lock", 10*time.Second)
	if err != nil {
		return fmt.Errorf("locking %s: %w", path, err)
	}
	defer unlock()

	lf, err := LoadLockfile(path)
	if err != nil {
		return err
	}
	if err := fn(lf); err != nil {
		return err
	}
	return lf.Save(path)
}

func acquireFileLock(lockPath string, timeout time.Duration) (func(), error) {
	deadline := time.Now().Add(timeout)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		// Recover stale lockfiles left by crashed processes (>60s old).
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > 60*time.Second {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for lockfile lock %s", lockPath)
		}
		time.Sleep(15 * time.Millisecond)
	}
}

// Save atomically persists the lockfile with 0600 permissions using a unique
// temp file in the target directory.
func (l *Lockfile) Save(path string) error {
	if l.Schema == "" {
		l.Schema = LockSchemaVersion
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating lockfile dir: %w", err)
	}
	raw, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmpFile, err := os.CreateTemp(dir, ".lock-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp lockfile: %w", err)
	}
	tmp := tmpFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmp)
		}
	}()
	if err := tmpFile.Chmod(0o600); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("chmod temp lockfile: %w", err)
	}
	if _, err := tmpFile.Write(raw); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("writing temp lockfile: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("syncing temp lockfile: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("closing temp lockfile: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("committing lockfile: %w", err)
	}
	cleanup = false
	return nil
}

// Pin records or updates a tool's SHA-256 pin.
func (l *Lockfile) Pin(entry LockEntry) {
	if l.Entries == nil {
		l.Entries = make(map[string]LockEntry)
	}
	if entry.PinnedAt.IsZero() {
		entry.PinnedAt = time.Now().UTC()
	}
	l.Entries[entry.Name] = entry
}

// VerifyCartridge checks an on-disk cartridge against its lockfile pin.
// Unpinned user-local cartridges are reported as not-pinned unless strict is true.
func (l *Lockfile) VerifyCartridge(c *Cartridge, strict bool) error {
	entry, ok := l.Entries[c.Name]
	if !ok {
		if strict {
			return fmt.Errorf("cartridge %q is not pinned in lockfile (run `pp trust %s` to pin)", c.Name, c.Name)
		}
		return nil
	}
	if entry.SHA256 != "" && c.SHA256 != "" && entry.SHA256 != c.SHA256 {
		return fmt.Errorf("cartridge %q SHA-256 drift detected (locked %s, got %s) - inspect or re-pin with `pp trust %s`",
			c.Name, shortHash(entry.SHA256), shortHash(c.SHA256), c.Name)
	}
	for _, host := range c.Trust.AllowedHosts {
		if len(entry.AllowedHosts) > 0 && !slices.Contains(entry.AllowedHosts, host) {
			return fmt.Errorf("cartridge %q added unpinned host %q (locked hosts: %v)", c.Name, host, entry.AllowedHosts)
		}
	}
	return nil
}

// VerifyBinary checks an installed prebuilt binary on disk against its saved
// SHA-256 lockfile pin. Missing pins or hash mismatches fail closed.
func (l *Lockfile) VerifyBinary(slug, binPath string) error {
	clean := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(slug)), "-pp-cli")
	entry, ok := l.Entries[clean]
	if !ok || entry.Kind != "binary" {
		return fmt.Errorf("binary %q at %s is not pinned in lockfile (reinstall with `pp use %s`)", clean, binPath, clean)
	}
	if strings.TrimSpace(entry.SHA256) == "" {
		return fmt.Errorf("binary %q lockfile entry has empty SHA-256 (reinstall with `pp use %s`)", clean, clean)
	}
	raw, err := os.ReadFile(binPath)
	if err != nil {
		return fmt.Errorf("reading installed binary %s: %w", binPath, err)
	}
	sum := sha256.Sum256(raw)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(entry.SHA256, actual) {
		return fmt.Errorf("binary %q SHA-256 drift detected (locked %s, got %s) - reinstall verified binary with `pp use %s`",
			clean, shortHash(entry.SHA256), shortHash(actual), clean)
	}
	return nil
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
