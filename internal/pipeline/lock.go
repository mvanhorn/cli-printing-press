package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
)

const (
	// StaleLockThreshold is the duration after which a lock is considered stale.
	StaleLockThreshold = 30 * time.Minute

	locksDir = ".locks"
)

// LockState represents the state of a build lock for a CLI.
type LockState struct {
	Scope      string    `json:"scope"`
	Phase      string    `json:"phase"`
	PID        int       `json:"pid"`
	AcquiredAt time.Time `json:"acquired_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// LockStatusResult is the combined status returned by LockStatus.
type LockStatusResult struct {
	Held       bool       `json:"held"`
	Stale      bool       `json:"stale"`
	Phase      string     `json:"phase,omitempty"`
	Scope      string     `json:"scope,omitempty"`
	AgeSeconds float64    `json:"age_seconds,omitempty"`
	HasCLI     bool       `json:"has_cli"`
	Lock       *LockState `json:"lock,omitempty"`
}

// LocksDir returns the global locks directory path.
func LocksDir() string {
	return filepath.Join(PressHome(), locksDir)
}

// LockFilePath returns the lock file path for a given CLI name. Callers may
// pass either the slug or the binary name; both forms resolve to the same
// file. Caller is responsible for validating the name first via
// validateCLIName — LockFilePath itself does not check.
func LockFilePath(cliName string) string {
	return filepath.Join(LocksDir(), normalizeLockName(cliName)+".lock")
}

func normalizeLockName(cliName string) string {
	if naming.IsCLIDirName(cliName) {
		return cliName
	}
	return naming.CLI(cliName)
}

// validateCLIName rejects names that would escape LocksDir() once joined as
// a filename. naming.IsValidLibraryDirName covers path separators, "..",
// NUL, dotfile prefixes, and non-slug shapes. The lock helpers below call
// this at entry so a malicious or buggy --cli value can never reach
// filepath.Join with traversal characters intact.
func validateCLIName(cliName string) error {
	if !naming.IsValidLibraryDirName(cliName) {
		return fmt.Errorf("invalid cli name %q", cliName)
	}
	return nil
}

// AcquireLock attempts to acquire a build lock for the given CLI.
// It auto-reclaims stale locks. If force is true, it overrides even fresh
// locks held by a different scope.
func AcquireLock(cliName, scope string, force bool) (*LockState, error) {
	if err := validateCLIName(cliName); err != nil {
		return nil, err
	}
	lockPath := LockFilePath(cliName)

	if err := os.MkdirAll(LocksDir(), 0o755); err != nil {
		return nil, fmt.Errorf("creating locks directory: %w", err)
	}

	lock := &LockState{
		Scope:      scope,
		Phase:      "acquire",
		PID:        os.Getpid(),
		AcquiredAt: time.Now(),
		UpdatedAt:  time.Now(),
	}

	// Try atomic creation first.
	err := writeLockExclusive(lockPath, lock)
	if err == nil {
		return lock, nil
	}
	if !os.IsExist(err) {
		return nil, fmt.Errorf("acquiring lock: %w", err)
	}

	// Lock file exists — check if we can reclaim it.
	// Retry read once to tolerate a concurrent atomic rename in writeLock.
	existing, readErr := readLock(lockPath)
	if readErr != nil {
		time.Sleep(50 * time.Millisecond)
		existing, readErr = readLock(lockPath)
	}
	if readErr != nil {
		// Still can't read — file is genuinely corrupt. Remove and re-create.
		_ = os.Remove(lockPath)
		if err := writeLockExclusive(lockPath, lock); err != nil {
			return nil, fmt.Errorf("acquiring lock after removing unreadable lock: %w", err)
		}
		return lock, nil
	}

	// Same scope — re-entrant, just overwrite.
	if existing.Scope == scope {
		if err := writeLock(lockPath, lock); err != nil {
			return nil, fmt.Errorf("re-acquiring lock for same scope: %w", err)
		}
		return lock, nil
	}

	// Different scope — check staleness or force.
	if IsStale(existing) || force {
		_ = os.Remove(lockPath)
		if err := writeLockExclusive(lockPath, lock); err != nil {
			return nil, fmt.Errorf("acquiring lock after reclaim: %w", err)
		}
		return lock, nil
	}

	return nil, fmt.Errorf("lock held by scope %q (phase: %s, updated: %s ago)", existing.Scope, existing.Phase, time.Since(existing.UpdatedAt).Truncate(time.Second))
}

// UpdateLock refreshes the heartbeat and phase of an existing lock.
func UpdateLock(cliName, phase string) error {
	if err := validateCLIName(cliName); err != nil {
		return err
	}
	lockPath := LockFilePath(cliName)

	existing, err := readLock(lockPath)
	if err != nil {
		return fmt.Errorf("reading lock for update: %w", err)
	}

	existing.Phase = phase
	existing.UpdatedAt = time.Now()
	existing.PID = os.Getpid()

	return writeLock(lockPath, existing)
}

// LockStatus returns the current lock state for a CLI, including whether
// a completed CLI exists in the library. An invalid cliName returns the
// zero result (no lock held, no library CLI) — safe behavior at this
// boundary since the function has no error channel.
func LockStatus(cliName string) LockStatusResult {
	result := LockStatusResult{}

	if err := validateCLIName(cliName); err != nil {
		return result
	}

	// Check library for completed CLI (slug-keyed directory).
	slug := naming.TrimCLISuffix(cliName)
	libDir := filepath.Join(PublishedLibraryRoot(), slug)
	if info, err := os.Stat(libDir); err == nil && info.IsDir() {
		goModPath := filepath.Join(libDir, "go.mod")
		manifestPath := filepath.Join(libDir, CLIManifestFilename)
		_, goModErr := os.Stat(goModPath)
		_, manifestErr := os.Stat(manifestPath)
		result.HasCLI = goModErr == nil || manifestErr == nil
	}

	// Check lock file.
	lockPath := LockFilePath(cliName)
	lock, err := readLock(lockPath)
	if err != nil {
		return result
	}

	result.Held = true
	result.Stale = IsStale(lock)
	result.Phase = lock.Phase
	result.Scope = lock.Scope
	result.AgeSeconds = time.Since(lock.UpdatedAt).Seconds()
	result.Lock = lock

	return result
}

// ReleaseLock removes the lock file for a CLI. It is idempotent.
func ReleaseLock(cliName string) error {
	if err := validateCLIName(cliName); err != nil {
		return err
	}
	lockPath := LockFilePath(cliName)
	err := os.Remove(lockPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("releasing lock: %w", err)
	}
	return nil
}

// PromoteWorkingCLI copies a working CLI directory to the library, writes
// the CLI manifest, updates the CurrentRunPointer, and releases the lock.
// Uses a staging directory with atomic swap so the previous library copy
// survives if any step fails.
func PromoteWorkingCLI(cliName, workingDir string, state *PipelineState) error {
	if err := validateCLIName(cliName); err != nil {
		return err
	}
	if workingDir == "" {
		return fmt.Errorf("working directory is empty")
	}

	// Verify working dir has content.
	entries, err := os.ReadDir(workingDir)
	if err != nil {
		return fmt.Errorf("reading working directory: %w", err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("working directory is empty: %s", workingDir)
	}
	if err := validatePhase5GateForPromote(workingDir, state); err != nil {
		return err
	}

	slug := naming.TrimCLISuffix(cliName)
	libraryDir := filepath.Join(PublishedLibraryRoot(), slug)
	stagingDir := libraryDir + ".promoting"
	backupDir := libraryDir + ".old"

	// Ensure parent exists.
	if err := os.MkdirAll(filepath.Dir(libraryDir), 0o755); err != nil {
		return fmt.Errorf("creating library parent directory: %w", err)
	}

	// If a previous promote died after moving the live library to backup but
	// before swapping in staging, restore that backup before attempting a retry.
	if _, err := os.Stat(backupDir); err == nil {
		if _, libErr := os.Stat(libraryDir); os.IsNotExist(libErr) {
			if err := os.Rename(backupDir, libraryDir); err != nil {
				return fmt.Errorf("restoring library from backup: %w", err)
			}
		} else if libErr != nil {
			return fmt.Errorf("checking existing library directory: %w", libErr)
		}
	}

	// Clean up any leftover staging dir from a previous failed promote.
	_ = os.RemoveAll(stagingDir)

	// Copy working dir to staging.
	if err := CopyDir(workingDir, stagingDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		return fmt.Errorf("copying to staging directory: %w", err)
	}

	// Update state to reflect promotion.
	state.PublishedDir = libraryDir

	// Write CLI manifest into the staging copy.
	if err := writeCLIManifestForPublish(state, stagingDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		return fmt.Errorf("writing CLI manifest: %w", err)
	}

	// Refresh the MCPB manifest.json in the staging dir so the lock-and-promote
	// flow keeps it in sync with the post-publish CLIManifest fields.
	if err := WriteMCPBManifest(stagingDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write MCPB manifest.json: %v\n", err)
	}

	// Remove any stale backup from a prior successful swap before we create a
	// fresh backup for the current library contents.
	if _, err := os.Stat(backupDir); err == nil {
		if err := os.RemoveAll(backupDir); err != nil {
			_ = os.RemoveAll(stagingDir)
			return fmt.Errorf("removing stale backup directory: %w", err)
		}
	}

	// Atomic swap: move old library aside, move staging into place.
	if _, err := os.Stat(libraryDir); err == nil {
		if err := os.Rename(libraryDir, backupDir); err != nil {
			_ = os.RemoveAll(stagingDir)
			return fmt.Errorf("backing up existing library directory: %w", err)
		}
	} else if !os.IsNotExist(err) {
		_ = os.RemoveAll(stagingDir)
		return fmt.Errorf("checking library directory before promote: %w", err)
	}

	if err := os.Rename(stagingDir, libraryDir); err != nil {
		// Restore backup if the swap failed.
		if _, statErr := os.Stat(backupDir); statErr == nil {
			_ = os.Rename(backupDir, libraryDir)
		}
		return fmt.Errorf("promoting staging to library: %w", err)
	}

	// Swap succeeded — remove the backup.
	_ = os.RemoveAll(backupDir)

	// Update current run pointer so working_dir reflects library path.
	state.WorkingDir = libraryDir
	saveErr := state.Save()
	releaseErr := ReleaseLock(cliName)

	switch {
	case saveErr != nil && releaseErr != nil:
		return fmt.Errorf("cli promoted to %s, but state update failed: %v; lock release also failed: %w", libraryDir, saveErr, releaseErr)
	case saveErr != nil:
		return fmt.Errorf("cli promoted to %s, but state update failed: %w", libraryDir, saveErr)
	case releaseErr != nil:
		return fmt.Errorf("cli promoted to %s, but lock release failed: %w", libraryDir, releaseErr)
	default:
		return nil
	}
}

func validatePhase5GateForPromote(workingDir string, state *PipelineState) error {
	if state == nil || state.RunID == "" {
		return nil
	}

	manifest := CLIManifest{
		APIName: state.APIName,
		CLIName: naming.CLI(state.APIName),
		RunID:   state.RunID,
	}
	if existing, err := ReadCLIManifest(workingDir); err == nil {
		if existing.APIName != "" {
			manifest.APIName = existing.APIName
		}
		if existing.CLIName != "" {
			manifest.CLIName = existing.CLIName
		}
		manifest.AuthType = existing.AuthType
	}

	result := ValidatePhase5Gate(state.ProofsDir(), manifest)
	if result.Passed {
		return nil
	}
	return fmt.Errorf("phase5 gate failed: %s", result.Detail)
}

// IsStale returns true if the lock's heartbeat is too old or its owner
// process is known to have exited.
func IsStale(lock *LockState) bool {
	if time.Since(lock.UpdatedAt) > StaleLockThreshold {
		return true
	}
	return lockOwnerDead(lock)
}

func lockOwnerDead(lock *LockState) bool {
	return lock.PID > 0 && !lockOwnerAliveFunc(lock.PID)
}

var lockOwnerAliveFunc = lockOwnerAlive

func readLock(path string) (*LockState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var lock LockState
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}
	return &lock, nil
}

func writeLock(path string, lock *LockState) error {
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	// Write to a temp file in the same directory and rename for atomicity.
	// This prevents concurrent readers from seeing truncated JSON.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeLockExclusive(path string, lock *LockState) error {
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
