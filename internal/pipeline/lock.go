package pipeline

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/artifacts"
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
	_, err := promoteWorkingCLI(cliName, workingDir, state)
	return err
}

func promoteWorkingCLI(cliName, workingDir string, state *PipelineState) (*PromoteResult, error) {
	if err := validateCLIName(cliName); err != nil {
		return nil, err
	}
	if workingDir == "" {
		return nil, fmt.Errorf("working directory is empty")
	}

	// Verify working dir has content.
	entries, err := os.ReadDir(workingDir)
	if err != nil {
		return nil, fmt.Errorf("reading working directory: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("working directory is empty: %s", workingDir)
	}
	if err := validatePhase5GateForPromote(workingDir, state); err != nil {
		return nil, err
	}
	if err := validatePIIGateForPromote(workingDir, state); err != nil {
		return nil, err
	}

	slug := naming.TrimCLISuffix(cliName)
	libraryDir := filepath.Join(PublishedLibraryRoot(), slug)
	stagingDir := libraryDir + ".promoting"
	backupDir := libraryDir + ".old"

	// Ensure parent exists.
	if err := os.MkdirAll(filepath.Dir(libraryDir), 0o755); err != nil {
		return nil, fmt.Errorf("creating library parent directory: %w", err)
	}
	sameTarget, err := sameDirectory(workingDir, libraryDir)
	if err != nil {
		return nil, err
	}
	if !sameTarget {
		if err := refreshPromoteArtifactsKeepingPatchedManifests(workingDir, cliName, libraryDir); err != nil {
			return nil, fmt.Errorf("refreshing staged artifacts: %w", err)
		}
	}

	// If a previous promote died after moving the live library to backup but
	// before swapping in staging, restore that backup before attempting a retry.
	if _, err := os.Stat(backupDir); err == nil {
		if _, libErr := os.Stat(libraryDir); os.IsNotExist(libErr) {
			if err := os.Rename(backupDir, libraryDir); err != nil {
				return nil, fmt.Errorf("restoring library from backup: %w", err)
			}
		} else if libErr != nil {
			return nil, fmt.Errorf("checking existing library directory: %w", libErr)
		}
	}

	// Clean up any leftover staging dir from a previous failed promote.
	_ = os.RemoveAll(stagingDir)

	// Copy working dir to staging.
	if err := CopyDir(workingDir, stagingDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		return failPromoteBeforeSwap(cliName, sameTarget, fmt.Errorf("copying to staging directory: %w", err))
	}
	if sameTarget {
		if err := refreshPromoteArtifactsKeepingPatchedManifests(stagingDir, cliName, libraryDir); err != nil {
			_ = os.RemoveAll(stagingDir)
			return failPromoteBeforeSwap(cliName, sameTarget, fmt.Errorf("refreshing staged artifacts: %w", err))
		}
	}

	// The library may contain hand-authored patch records that were created
	// after the working copy was generated. Keep those records in the atomic
	// replacement while letting the staged copy win on filename conflicts.
	preservedPatches, err := preserveLibraryOnlyPatches(libraryDir, stagingDir)
	if err != nil {
		_ = os.RemoveAll(stagingDir)
		return failPromoteBeforeSwap(cliName, sameTarget, fmt.Errorf("preserving library-only patches: %w", err))
	}
	if err := preserveLibraryReleaseLedger(libraryDir, stagingDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		return failPromoteBeforeSwap(cliName, sameTarget, fmt.Errorf("preserving library release ledger: %w", err))
	}
	if err := PreserveStampedRuntimeVersion(libraryDir, stagingDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		return failPromoteBeforeSwap(cliName, sameTarget, fmt.Errorf("preserving runtime version layout: %w", err))
	}

	// Staging is the tree that enters the library: working copy plus any
	// library-only records just unioned in. Validating workingDir alone would
	// miss a generate --force that dropped recorded files while the library
	// still held the ledger.
	if err := ValidatePatchRecords(stagingDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		return failPromoteBeforeSwap(cliName, sameTarget, fmt.Errorf("recorded patches no longer match the tree being promoted: %w", err))
	}

	// Phase 5 writes acceptance markers to the runstate, but the published
	// copy is the path downstream consumers see — embed them before the swap.
	if err := stageRunstateManuscripts(stagingDir, state); err != nil {
		_ = os.RemoveAll(stagingDir)
		return failPromoteBeforeSwap(cliName, sameTarget, fmt.Errorf("staging runstate manuscripts: %w", err))
	}

	// Snapshot after the patch gate and before spec regeneration. writeCLIManifest
	// replaces tools-manifest.json and EnsureMCPBManifest replaces manifest.json;
	// a recorded hand-fix that just passed the gate has to be put back or the
	// library copy ships the regenerated file.
	patchedManifests, err := snapshotPatchedMCPManifests(stagingDir)
	if err != nil {
		_ = os.RemoveAll(stagingDir)
		return failPromoteBeforeSwap(cliName, sameTarget, fmt.Errorf("reading patched MCP manifests: %w", err))
	}

	// Update state to reflect promotion.
	state.PublishedDir = libraryDir

	// Write CLI manifest into the staging copy.
	if err := writeCLIManifestForPublish(state, stagingDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		return failPromoteBeforeSwap(cliName, sameTarget, fmt.Errorf("writing CLI manifest: %w", err))
	}
	if err := restorePermanentCreatorForPromote(stagingDir, libraryDir, state.APIName); err != nil {
		_ = os.RemoveAll(stagingDir)
		return failPromoteBeforeSwap(cliName, sameTarget, fmt.Errorf("restoring permanent creator: %w", err))
	}

	// Refresh the MCPB manifest.json in the staging dir so the lock-and-promote
	// flow keeps it in sync with the post-publish CLIManifest fields. The
	// writer also reconciles against env reads in internal/client/ so the
	// staged bundle includes any user_config fields the spec did not surface.
	// Errors abort the promote rather than warn-and-continue — a reconcile
	// failure here means the published bundle would ship missing user_config
	// fields, which is the exact bug class this writer chain exists to prevent.
	if err := EnsureMCPBManifest(stagingDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		return failPromoteBeforeSwap(cliName, sameTarget, fmt.Errorf("writing MCPB manifest to staging: %w", err))
	}
	if err := restorePatchedMCPManifests(stagingDir, patchedManifests); err != nil {
		_ = os.RemoveAll(stagingDir)
		return failPromoteBeforeSwap(cliName, sameTarget, fmt.Errorf("restoring patched MCP manifests: %w", err))
	}
	if _, err := syncPromoteBundle(stagingDir, cliName); err != nil {
		_ = os.RemoveAll(stagingDir)
		return failPromoteBeforeSwap(cliName, sameTarget, fmt.Errorf("syncing MCPB bundle in staging: %w", err))
	}
	// The gate above ran on the pre-regeneration tree. Check the tree that
	// is about to be swapped in so a later writer cannot drop a recorded
	// call site and still promote.
	if err := ValidatePatchRecords(stagingDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		return failPromoteBeforeSwap(cliName, sameTarget, fmt.Errorf("recorded patches no longer match the tree being promoted: %w", err))
	}

	// Remove any stale backup from a prior successful swap before we create a
	// fresh backup for the current library contents.
	if _, err := os.Stat(backupDir); err == nil {
		if err := os.RemoveAll(backupDir); err != nil {
			_ = os.RemoveAll(stagingDir)
			return failPromoteBeforeSwap(cliName, sameTarget, fmt.Errorf("removing stale backup directory: %w", err))
		}
	}

	// Atomic swap: move old library aside, move staging into place.
	if _, err := os.Stat(libraryDir); err == nil {
		if err := os.Rename(libraryDir, backupDir); err != nil {
			_ = os.RemoveAll(stagingDir)
			return failPromoteBeforeSwap(cliName, sameTarget, fmt.Errorf("backing up existing library directory: %w", err))
		}
	} else if !os.IsNotExist(err) {
		_ = os.RemoveAll(stagingDir)
		return failPromoteBeforeSwap(cliName, sameTarget, fmt.Errorf("checking library directory before promote: %w", err))
	}

	if err := os.Rename(stagingDir, libraryDir); err != nil {
		// Restore backup if the swap failed.
		if _, statErr := os.Stat(backupDir); statErr == nil {
			_ = os.Rename(backupDir, libraryDir)
		}
		return failPromoteBeforeSwap(cliName, sameTarget, fmt.Errorf("promoting staging to library: %w", err))
	}

	if sameTarget {
		if err := moveGitMetadataIntoPromotedLibrary(backupDir, libraryDir); err != nil {
			_ = os.RemoveAll(libraryDir)
			if _, statErr := os.Stat(backupDir); statErr == nil {
				_ = os.Rename(backupDir, libraryDir)
			}
			return failPromoteBeforeSwap(cliName, sameTarget, err)
		}
	}

	// Swap succeeded — remove the backup.
	_ = os.RemoveAll(backupDir)

	return finishPromote(cliName, libraryDir, state, preservedPatches)
}

// refreshPromoteArtifactsKeepingPatchedManifests runs the promote artifact
// refresh, then puts back manifest.json when a recorded patch claims it.
// The refresh rewrites that file and packs it into the .mcpb before the
// patch gate runs; without the restore, the gate fails on a hand-fix that
// was present when promote started. libraryDir is consulted only for patch
// records that dir does not have yet: those records are copied in after
// this refresh, so a hand-fix they describe has to be snapshotted now.
func refreshPromoteArtifactsKeepingPatchedManifests(dir, cliName, libraryDir string) error {
	extra, err := libraryOnlyPatchRecords(libraryDir, dir)
	if err != nil {
		return err
	}
	snaps, err := snapshotPatchedMCPManifestsFrom(dir, extra)
	if err != nil {
		return err
	}
	if _, err := refreshPromoteArtifacts(dir, cliName); err != nil {
		if restoreErr := restorePatchedMCPManifests(dir, snaps); restoreErr != nil {
			return fmt.Errorf("%w; restoring patched MCP manifests: %v", err, restoreErr)
		}
		return err
	}
	if err := restorePatchedMCPManifests(dir, snaps); err != nil {
		return err
	}
	if _, ok := snaps[MCPBManifestFilename]; !ok {
		return nil
	}
	if _, err := syncPromoteBundle(dir, cliName); err != nil {
		return fmt.Errorf("repacking MCPB bundle after restoring patched manifest: %w", err)
	}
	return nil
}

// snapshotPatchedMCPManifests copies manifest.json and tools-manifest.json
// when a patch record lists them. Other patched paths are left to the
// patch gate; these two are the files promote regenerates from the spec.
func snapshotPatchedMCPManifests(dir string) (map[string][]byte, error) {
	return snapshotPatchedMCPManifestsFrom(dir, nil)
}

// snapshotPatchedMCPManifestsFrom is snapshotPatchedMCPManifests with extra
// claims. File bytes still come from dir, not from the record's source tree.
func snapshotPatchedMCPManifestsFrom(dir string, extra []PatchRecord) (map[string][]byte, error) {
	records, err := LoadPatchRecords(dir)
	if err != nil {
		return nil, err
	}
	records = append(records, extra...)
	claimed := make(map[string]struct{})
	for _, rec := range records {
		for _, raw := range rec.Files {
			rel, pathErr := safePatchRelPath(raw)
			if pathErr != nil || !isRegeneratedMCPManifest(rel) {
				continue
			}
			claimed[rel] = struct{}{}
		}
	}
	if len(claimed) == 0 {
		return nil, nil
	}
	snaps := make(map[string][]byte, len(claimed))
	for name := range claimed {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		snaps[name] = data
	}
	return snaps, nil
}

func isRegeneratedMCPManifest(rel string) bool {
	return rel == MCPBManifestFilename || rel == ToolsManifestFilename
}

func restorePatchedMCPManifests(dir string, snaps map[string][]byte) error {
	for name, data := range snaps {
		path := filepath.Join(dir, name)
		if err := writeFileAtomic(path, data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
	}
	return nil
}

func sameDirectory(a, b string) (bool, error) {
	aInfo, err := os.Stat(a)
	if err != nil {
		return false, fmt.Errorf("checking working directory: %w", err)
	}
	bInfo, err := os.Stat(b)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("checking library directory before promote: %w", err)
	}
	return os.SameFile(aInfo, bInfo), nil
}

func moveGitMetadataIntoPromotedLibrary(backupDir, libraryDir string) error {
	src := filepath.Join(backupDir, ".git")
	dst := filepath.Join(libraryDir, ".git")
	if _, err := os.Lstat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("checking backup git metadata: %w", err)
	}
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("staged git metadata unexpectedly exists: %s", dst)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking staged git metadata: %w", err)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("moving git metadata into promoted library: %w", err)
	}
	return nil
}

func failPromoteBeforeSwap(cliName string, releaseLock bool, promoteErr error) (*PromoteResult, error) {
	if !releaseLock {
		return nil, promoteErr
	}
	if releaseErr := ReleaseLock(cliName); releaseErr != nil {
		return nil, fmt.Errorf("%w; lock release failed: %v", promoteErr, releaseErr)
	}
	return nil, promoteErr
}

func finishPromote(cliName, libraryDir string, state *PipelineState, preservedPatches []string) (*PromoteResult, error) {
	state.WorkingDir = libraryDir
	saveErr := state.Save()
	releaseErr := ReleaseLock(cliName)

	switch {
	case saveErr != nil && releaseErr != nil:
		return nil, fmt.Errorf("cli promoted to %s, but state update failed: %v; lock release also failed: %w", libraryDir, saveErr, releaseErr)
	case saveErr != nil:
		return nil, fmt.Errorf("cli promoted to %s, but state update failed: %w", libraryDir, saveErr)
	case releaseErr != nil:
		return nil, fmt.Errorf("cli promoted to %s, but lock release failed: %w", libraryDir, releaseErr)
	default:
		return &PromoteResult{PreservedPatches: preservedPatches}, nil
	}
}

// PromoteResult contains operator-visible details from a successful promote.
type PromoteResult struct {
	PreservedPatches []string
}

// PromoteWorkingCLIWithResult is the reporting form of PromoteWorkingCLI used
// by the lock CLI. The existing error-only function remains for callers that
// do not need promote details.
func PromoteWorkingCLIWithResult(cliName, workingDir string, state *PipelineState) (*PromoteResult, error) {
	return promoteWorkingCLI(cliName, workingDir, state)
}

// libraryOnlyPatchRecords returns patch files that preserveLibraryOnlyPatches
// will copy from libraryDir into workingDir. The working tree can already
// contain the edited manifest those records describe; the refresh that runs
// before the copy has to see the records or it erases that edit.
func libraryOnlyPatchRecords(libraryDir, workingDir string) ([]PatchRecord, error) {
	if libraryDir == "" {
		return nil, nil
	}
	workingPatches := filepath.Join(workingDir, PatchesDirName)
	info, err := os.Stat(workingPatches)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err == nil && !info.IsDir() {
		// preserveLibraryOnlyPatches reports this path. Reading the library
		// records here would replace that error with a parse failure.
		return nil, nil
	}

	libraryPatches := filepath.Join(libraryDir, PatchesDirName)
	entries, err := os.ReadDir(libraryPatches)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var records []PatchRecord
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == PatchesGitKeepName || name == PatchesMetadataFilename {
			continue
		}
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		if _, err := os.Lstat(filepath.Join(workingPatches, name)); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		rel := filepath.ToSlash(filepath.Join(PatchesDirName, name))
		rec, err := loadPatchRecordFile(filepath.Join(libraryPatches, name), rel)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	return records, nil
}

func preserveLibraryOnlyPatches(libraryDir, stagingDir string) ([]string, error) {
	libraryPatches := filepath.Join(libraryDir, PatchesDirName)
	entries, err := os.ReadDir(libraryPatches)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	stagingPatches := filepath.Join(stagingDir, PatchesDirName)
	if info, err := os.Stat(stagingPatches); err == nil {
		if !info.IsDir() {
			return nil, fmt.Errorf("staged patches path is not a directory: %s", stagingPatches)
		}
	} else if os.IsNotExist(err) {
		if err := os.MkdirAll(stagingPatches, 0o755); err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}

	preserved := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Name() == PatchesGitKeepName || entry.Name() == PatchesMetadataFilename {
			continue
		}
		if _, err := os.Lstat(filepath.Join(stagingPatches, entry.Name())); os.IsNotExist(err) {
			preserved = append(preserved, entry.Name())
		} else if err != nil {
			return nil, err
		}
	}
	sort.Strings(preserved)

	if err := copyDirFiltered(libraryPatches, stagingPatches, func(path string, _ fs.FileInfo) bool {
		rel, err := filepath.Rel(libraryPatches, path)
		if err != nil {
			return false
		}
		_, err = os.Lstat(filepath.Join(stagingPatches, rel))
		return err == nil
	}); err != nil {
		return nil, err
	}
	return preserved, nil
}

// preserveLibraryReleaseLedger copies the public-library release ledger from
// the tree being replaced onto the staged tree. Promote swaps the working
// copy in wholesale; these two files are owned by the library's post-merge
// release workflow and must survive that swap byte-identical.
func preserveLibraryReleaseLedger(libraryDir, stagingDir string) error {
	info, err := os.Stat(libraryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	for _, name := range []string{CLIChangelogFilename, CLIReleaseManifestFilename} {
		src := filepath.Join(libraryDir, name)
		srcInfo, err := os.Lstat(src)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("reading library %s: %w", name, err)
		}
		if !srcInfo.Mode().IsRegular() {
			return fmt.Errorf("library %s is not a regular file", name)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("reading library %s: %w", name, err)
		}
		dst := filepath.Join(stagingDir, name)
		if err := os.WriteFile(dst, data, srcInfo.Mode().Perm()); err != nil {
			return fmt.Errorf("preserving library %s: %w", name, err)
		}
	}
	return nil
}

// A pre-existing subtree in the staging copy wins so artifacts that generate
// or polish wrote directly into the working dir are never overwritten by an
// older runstate snapshot. No-op when state has no RunID — the
// NewMinimalState path for plan-driven CLIs has no runstate to stage from.
func stageRunstateManuscripts(stagingDir string, state *PipelineState) error {
	if state == nil || state.RunID == "" {
		return nil
	}
	// The leaf component of each source path becomes the subdir name in
	// staging (proofs/research/discovery). This pairs with the directory
	// shape established by paths.go's RunProofsDir, RunResearchDir, and
	// RunDiscoveryDir helpers; if those rename their leaf names, the
	// destination layout follows automatically.
	sources := []string{
		state.ProofsDir(),
		state.ResearchDir(),
		state.DiscoveryDir(),
	}
	dstRoot := filepath.Join(stagingDir, ".manuscripts", state.RunID)
	researchJSON := filepath.Join(state.RunRoot(), "research.json")
	if info, err := os.Stat(researchJSON); err == nil && !info.IsDir() {
		target := filepath.Join(dstRoot, "research.json")
		if _, statErr := os.Stat(target); statErr == nil {
			// The working copy already has the publish-visible artifact.
		} else if os.IsNotExist(statErr) {
			if err := copyFile(researchJSON, target, info.Mode()); err != nil {
				return fmt.Errorf("copying runstate research.json: %w", err)
			}
		} else {
			return fmt.Errorf("checking %s in staging: %w", target, statErr)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspecting runstate research.json: %w", err)
	}
	// Scan every directory before copying any of them. A pre-existing
	// destination is left alone, but its source still contributes skipped
	// file identities so a research/ hard link of a discovery capture is
	// not copied under the other name.
	opts := PublishableManuscriptCopyOptions{}
	var skipped skippedFiles
	type stagedSource struct {
		src  string
		name string
	}
	var existing []stagedSource
	for _, src := range sources {
		name := filepath.Base(src)
		info, err := os.Stat(src)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("inspecting runstate %s: %w", name, err)
		}
		if !info.IsDir() {
			continue
		}
		part, err := skippedPublishableContent(src, opts)
		if err != nil {
			return fmt.Errorf("copying runstate %s: %w", name, err)
		}
		skipped.merge(part)
		existing = append(existing, stagedSource{src: src, name: name})
	}
	for _, item := range existing {
		target := filepath.Join(dstRoot, item.name)
		if _, statErr := os.Stat(target); statErr == nil {
			continue
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf("checking %s in staging: %w", target, statErr)
		}
		if err := copyPublishableManuscriptDir(item.src, target, opts, skipped); err != nil {
			return fmt.Errorf("copying runstate %s: %w", item.name, err)
		}
	}
	return nil
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
		manifest.SpecKind = existing.SpecKind
	}

	candidates := Phase5ProofsDirCandidates(workingDir, manifest, state.ProofsDir())
	var firstFailure Phase5GateValidation
	foundDir := false
	for _, proofsDir := range candidates {
		info, err := os.Stat(proofsDir)
		if err != nil || !info.IsDir() {
			continue
		}
		foundDir = true
		result := ValidatePhase5Gate(proofsDir, manifest, workingDir)
		if result.Passed {
			return nil
		}
		if firstFailure.Detail == "" {
			firstFailure = result
		}
	}
	if !foundDir {
		preferred := FirstExistingPhase5ProofsDir(candidates)
		return phase5PromoteGateError(ValidatePhase5Gate(preferred, manifest, workingDir))
	}
	return phase5PromoteGateError(firstFailure)
}

func phase5PromoteGateError(result Phase5GateValidation) error {
	if result.MarkerPath != "" {
		return fmt.Errorf("phase5 gate failed: %s (marker: %s)", result.Detail, result.MarkerPath)
	}
	return fmt.Errorf("phase5 gate failed: %s", result.Detail)
}

// validatePIIGateForPromote runs the PII audit against the working
// directory and refuses promote when pending findings or enforcement-
// primitive failures remain. The audit's ledger is refreshed in place
// (every call rewrites the ledger with the current timestamp and
// FindingsCountBefore baseline) so agent-written accepts from a prior
// polish run carry forward and new findings since polish surface as
// pending. The error message points operators at the ledger file and
// the pii-polish playbook.
func validatePIIGateForPromote(workingDir string, state *PipelineState) error {
	opts := piiAuditOptionsForPromote(state)
	result, err := artifacts.RunPIIAuditWithOptions(workingDir, opts)
	if err != nil {
		return fmt.Errorf("PII gate failed (scan error): %w", err)
	}
	pending := artifacts.PIIPendingCount(result.Findings)
	if pending == 0 && !result.Completion.HasGateFailure() {
		return nil
	}

	var parts []string
	if pending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending PII finding(s)", pending))
	}
	if result.Completion.HasGateFailure() {
		parts = append(parts, fmt.Sprintf("%d gate failure(s)", result.Completion.GateFailureCount()))
	}
	ledgerPath := filepath.Join(workingDir, artifacts.PIILedgerFilename)
	msg := fmt.Sprintf("PII gate failed: %s\n", strings.Join(parts, ", "))
	if pending > 0 {
		msg += "pending findings:\n" + artifacts.FormatPIIFindings(result.Findings) + "\n"
	}
	if result.Completion.HasGateFailure() {
		msg += "gate failures:\n" + artifacts.FormatPIIGateFailures(result.Completion) + "\n"
	}
	msg += fmt.Sprintf("ledger: %s\n", ledgerPath)
	msg += "scope: phase-1 detectors (order-id, card-last-4, email, phone, ZIP+4, postal-address); ASINs and standalone names are a future detector class.\n"
	command := "cli-printing-press pii-audit <dir>"
	if opts.ManuscriptsDir != "" {
		command += " --manuscripts-dir " + shellQuote(opts.ManuscriptsDir)
	}
	msg += fmt.Sprintf("run `%s`", command)
	msg += " and follow skills/printing-press-polish/references/pii-polish.md"
	return fmt.Errorf("%s", msg)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func piiAuditOptionsForPromote(state *PipelineState) artifacts.PIIAuditOptions {
	if state == nil || state.RunID == "" {
		return artifacts.PIIAuditOptions{}
	}
	runRoot := state.RunRoot()
	info, err := os.Stat(runRoot)
	if err != nil || !info.IsDir() {
		return artifacts.PIIAuditOptions{}
	}
	return artifacts.PIIAuditOptions{ManuscriptsDir: runRoot}
}

// IsStale reports whether the lock heartbeat is older than StaleLockThreshold.
// Owner PID liveness is ignored: acquire/update are discrete CLI processes
// that exit immediately, so a dead PID is the normal live-lock state.
func IsStale(lock *LockState) bool {
	return time.Since(lock.UpdatedAt) > StaleLockThreshold
}

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
