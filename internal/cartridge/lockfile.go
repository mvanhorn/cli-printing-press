package cartridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

// Save atomically persists the lockfile with 0600 permissions.
func (l *Lockfile) Save(path string) error {
	if l.Schema == "" {
		l.Schema = LockSchemaVersion
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating lockfile dir: %w", err)
	}
	raw, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("writing temp lockfile: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("committing lockfile: %w", err)
	}
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
		return fmt.Errorf("cartridge %q SHA-256 drift detected (locked %s, got %s) — inspect with `pp diff %s` or re-pin with `pp trust %s`",
			c.Name, shortHash(entry.SHA256), shortHash(c.SHA256), c.Name, c.Name)
	}
	for _, host := range c.Trust.AllowedHosts {
		if len(entry.AllowedHosts) > 0 && !slices.Contains(entry.AllowedHosts, host) {
			return fmt.Errorf("cartridge %q added unpinned host %q (locked hosts: %v)", c.Name, host, entry.AllowedHosts)
		}
	}
	return nil
}

func shortHash(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}
