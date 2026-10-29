package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func plantWorkingDirRun(t *testing.T, apiName, runID, scope, workingDir string, started time.Time, current bool) *PipelineState {
	t.Helper()
	state := NewStateWithRun(apiName, workingDir, runID, scope)
	state.StartedAt = started
	if current {
		require.NoError(t, state.Save())
	} else {
		require.NoError(t, state.SaveWithoutCurrentPointer())
	}
	return state
}

func setStateFileMTime(t *testing.T, state *PipelineState, when time.Time) {
	t.Helper()
	require.NoError(t, os.Chtimes(state.StatePath(), when, when))
}

func writeCurrentPointerAt(t *testing.T, state *PipelineState, updated time.Time) {
	t.Helper()
	pointer := CurrentRunPointer{
		APIName:    state.APIName,
		RunID:      state.RunID,
		Scope:      state.Scope,
		WorkingDir: state.EffectiveWorkingDir(),
		StatePath:  state.StatePath(),
		UpdatedAt:  updated,
	}
	data, err := json.MarshalIndent(pointer, "", "  ")
	require.NoError(t, err)
	path := currentRunPointerPathForScope(WorkspaceScope(), state.APIName)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, data, 0o644))
}

func TestFindStateByWorkingDirPrefersCurrentPointerOverNewerAndOlderRuns(t *testing.T) {
	home := setPressTestEnv(t)
	workDir := filepath.Join(home, "library", "demo")
	require.NoError(t, os.MkdirAll(workDir, 0o755))

	// Lexical run ids make the oldest directory the first glob hit.
	plantWorkingDirRun(t, "demo", "20200101T000000Z-old", "test-scope", workDir, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), false)
	current := plantWorkingDirRun(t, "demo", "20220101T000000Z-mid", "test-scope", workDir, time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC), true)
	plantWorkingDirRun(t, "demo", "20261029T000000Z-new", "test-scope", workDir, time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), false)

	state, err := FindStateByWorkingDir(workDir)
	require.NoError(t, err)
	assert.Equal(t, current.RunID, state.RunID)
	assert.Equal(t, current.ProofsDir(), state.ProofsDir())
}

func TestFindStateByWorkingDirUsesNewestWhenPointerDoesNotMatch(t *testing.T) {
	home := setPressTestEnv(t)
	workDir := filepath.Join(home, "library", "demo")
	otherDir := filepath.Join(home, "library", "other")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.MkdirAll(otherDir, 0o755))

	older := plantWorkingDirRun(t, "demo", "20200101T000000Z-old", "test-scope", workDir, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), false)
	newer := plantWorkingDirRun(t, "demo", "20261029T000000Z-new", "test-scope", workDir, time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), false)
	setStateFileMTime(t, older, time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))
	setStateFileMTime(t, newer, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))
	// Same API's pointer names a different directory, so it must not win.
	plantWorkingDirRun(t, "demo", "20280101T000000Z-elsewhere", "test-scope", otherDir, time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC), true)

	state, err := FindStateByWorkingDir(workDir)
	require.NoError(t, err)
	assert.Equal(t, newer.RunID, state.RunID)
}

func TestFindStateByWorkingDirSingleMatchIgnoresOtherDirectories(t *testing.T) {
	home := setPressTestEnv(t)
	workDir := filepath.Join(home, "library", "demo")
	otherDir := filepath.Join(home, "library", "other")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.MkdirAll(otherDir, 0o755))

	only := plantWorkingDirRun(t, "demo", "20200101T000000Z-only", "test-scope", workDir, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), false)
	plantWorkingDirRun(t, "demo", "20261029T000000Z-other", "test-scope", otherDir, time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), true)

	state, err := FindStateByWorkingDir(workDir)
	require.NoError(t, err)
	assert.Equal(t, only.RunID, state.RunID)
	assert.Equal(t, workDir, state.EffectiveWorkingDir())
}

func TestFindStateByWorkingDirIgnoresOtherScopePointer(t *testing.T) {
	home := setPressTestEnv(t)
	workDir := filepath.Join(home, "library", "demo")
	require.NoError(t, os.MkdirAll(workDir, 0o755))

	plantWorkingDirRun(t, "demo", "20200101T000000Z-old", "test-scope", workDir, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), false)
	newer := plantWorkingDirRun(t, "demo", "20261029T000000Z-new", "test-scope", workDir, time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), false)
	plantWorkingDirRun(t, "demo", "20220101T000000Z-mid", "other-scope", workDir, time.Date(2022, 1, 1, 0, 0, 0, 0, time.UTC), true)

	state, err := FindStateByWorkingDir(workDir)
	require.NoError(t, err)
	assert.Equal(t, newer.RunID, state.RunID)
	assert.Equal(t, "test-scope", state.Scope)
}

func TestFindStateByWorkingDirPrefersLaterPointerWhenSeveralMatch(t *testing.T) {
	home := setPressTestEnv(t)
	workDir := filepath.Join(home, "library", "demo")
	require.NoError(t, os.MkdirAll(workDir, 0o755))

	older := plantWorkingDirRun(t, "alpha", "20200101T000000Z-old", "test-scope", workDir, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), false)
	newer := plantWorkingDirRun(t, "beta", "20261029T000000Z-new", "test-scope", workDir, time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), false)
	writeCurrentPointerAt(t, older, time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC))
	writeCurrentPointerAt(t, newer, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))

	state, err := FindStateByWorkingDir(workDir)
	require.NoError(t, err)
	assert.Equal(t, older.RunID, state.RunID)
}

func TestFindStateByWorkingDirBreaksPointerTiesByRunRecency(t *testing.T) {
	home := setPressTestEnv(t)
	workDir := filepath.Join(home, "library", "demo")
	require.NoError(t, os.MkdirAll(workDir, 0o755))

	updated := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	older := plantWorkingDirRun(t, "alpha", "20200101T000000Z-old", "test-scope", workDir, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), false)
	newer := plantWorkingDirRun(t, "beta", "20261029T000000Z-new", "test-scope", workDir, time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), false)
	writeCurrentPointerAt(t, older, updated)
	writeCurrentPointerAt(t, newer, updated)

	state, err := FindStateByWorkingDir(workDir)
	require.NoError(t, err)
	assert.Equal(t, newer.RunID, state.RunID)
}

func TestFindStateByWorkingDirOrdersNewestByStartThenMTimeThenRunID(t *testing.T) {
	t.Run("started_at beats modtime and lexical run id", func(t *testing.T) {
		home := setPressTestEnv(t)
		workDir := filepath.Join(home, "library", "demo")
		require.NoError(t, os.MkdirAll(workDir, 0o755))
		older := plantWorkingDirRun(t, "demo", "20200101T000000Z-aaa", "test-scope", workDir, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), false)
		newer := plantWorkingDirRun(t, "demo", "20261029T000000Z-zzz", "test-scope", workDir, time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), false)
		setStateFileMTime(t, older, time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))
		setStateFileMTime(t, newer, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC))

		state, err := FindStateByWorkingDir(workDir)
		require.NoError(t, err)
		assert.Equal(t, newer.RunID, state.RunID)
	})

	t.Run("modtime breaks equal started_at", func(t *testing.T) {
		home := setPressTestEnv(t)
		workDir := filepath.Join(home, "library", "demo")
		require.NoError(t, os.MkdirAll(workDir, 0o755))
		started := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		earlierFile := plantWorkingDirRun(t, "demo", "20200101T000000Z-aaa", "test-scope", workDir, started, false)
		laterFile := plantWorkingDirRun(t, "demo", "20261029T000000Z-zzz", "test-scope", workDir, started, false)
		setStateFileMTime(t, earlierFile, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC))
		setStateFileMTime(t, laterFile, time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC))

		state, err := FindStateByWorkingDir(workDir)
		require.NoError(t, err)
		assert.Equal(t, laterFile.RunID, state.RunID)
	})

	t.Run("run id breaks equal started_at and modtime", func(t *testing.T) {
		home := setPressTestEnv(t)
		workDir := filepath.Join(home, "library", "demo")
		require.NoError(t, os.MkdirAll(workDir, 0o755))
		started := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		stamp := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
		plantWorkingDirRun(t, "demo", "run-a", "test-scope", workDir, started, false)
		laterID := plantWorkingDirRun(t, "demo", "run-z", "test-scope", workDir, started, false)
		setStateFileMTime(t, laterID, stamp)
		// Re-stamp the lexical-first run after run-z so save order cannot decide.
		firstID := plantWorkingDirRun(t, "demo", "run-a", "test-scope", workDir, started, false)
		setStateFileMTime(t, firstID, stamp)

		state, err := FindStateByWorkingDir(workDir)
		require.NoError(t, err)
		assert.Equal(t, "run-z", state.RunID)
	})

	t.Run("path breaks a full tie", func(t *testing.T) {
		home := setPressTestEnv(t)
		workDir := filepath.Join(home, "library", "demo")
		require.NoError(t, os.MkdirAll(workDir, 0o755))
		started := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		stamp := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
		lower := plantWorkingDirRun(t, "demo", "same-run", "scope-a", workDir, started, false)
		higher := plantWorkingDirRun(t, "demo", "same-run", "scope-b", workDir, started, false)
		setStateFileMTime(t, lower, stamp)
		setStateFileMTime(t, higher, stamp)

		state, err := FindStateByWorkingDir(workDir)
		require.NoError(t, err)
		assert.Equal(t, "scope-b", state.Scope)
	})

	t.Run("zero started_at loses to a timestamp", func(t *testing.T) {
		home := setPressTestEnv(t)
		workDir := filepath.Join(home, "library", "demo")
		require.NoError(t, os.MkdirAll(workDir, 0o755))
		plantWorkingDirRun(t, "demo", "20200101T000000Z-aaa", "test-scope", workDir, time.Time{}, false)
		stamped := plantWorkingDirRun(t, "demo", "20261029T000000Z-zzz", "test-scope", workDir, time.Date(2024, 5, 1, 0, 0, 0, 0, time.UTC), false)

		state, err := FindStateByWorkingDir(workDir)
		require.NoError(t, err)
		assert.Equal(t, stamped.RunID, state.RunID)
	})
}

func TestFindStateByWorkingDirIgnoresUnusableStateAndPointerFiles(t *testing.T) {
	home := setPressTestEnv(t)
	workDir := filepath.Join(home, "library", "demo")
	require.NoError(t, os.MkdirAll(workDir, 0o755))

	badDir := filepath.Join(home, ".runstate", "test-scope", "runs", "000-corrupt")
	require.NoError(t, os.MkdirAll(badDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(badDir, "state.json"), []byte("not-json"), 0o644))

	newer := plantWorkingDirRun(t, "demo", "20261029T000000Z-new", "test-scope", workDir, time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC), false)
	plantWorkingDirRun(t, "demo", "20200101T000000Z-old", "test-scope", workDir, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), false)

	current := CurrentRunDir()
	require.NoError(t, os.MkdirAll(current, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(current, "broken.json"), []byte("not-json"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(current, "empty.json"), []byte("{}\n"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(current, "dir.json"), 0o755))

	state, err := FindStateByWorkingDir(workDir)
	require.NoError(t, err)
	assert.Equal(t, newer.RunID, state.RunID)
}

func TestFindStateByWorkingDirBackfillsIdentityFromPath(t *testing.T) {
	home := setPressTestEnv(t)
	workDir := filepath.Join(home, "work", "backfill")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	runDir := filepath.Join(home, ".runstate", "test-scope", "runs", "backfill-run")
	require.NoError(t, os.MkdirAll(runDir, 0o755))
	raw := fmt.Sprintf(`{"version":3,"api_name":"backfill","output_dir":%q,"working_dir":%q,"started_at":"2024-05-01T00:00:00Z","phases":{}}`+"\n", workDir, workDir)
	require.NoError(t, os.WriteFile(filepath.Join(runDir, "state.json"), []byte(raw), 0o644))

	state, err := FindStateByWorkingDir(workDir)
	require.NoError(t, err)
	assert.Equal(t, "backfill-run", state.RunID)
	assert.Equal(t, "test-scope", state.Scope)
	assert.Equal(t, workDir, state.EffectiveWorkingDir())
}

func TestFindStateByWorkingDirResolvesRelativeDir(t *testing.T) {
	setPressTestEnv(t)
	parent := t.TempDir()
	workDir := filepath.Join(parent, "cli")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	planted := plantWorkingDirRun(t, "demo", "20240101T000000Z-rel", "test-scope", workDir, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), true)

	t.Chdir(parent)
	state, err := FindStateByWorkingDir("cli")
	require.NoError(t, err)
	assert.Equal(t, planted.RunID, state.RunID)
}

func TestFindStateByWorkingDirMissing(t *testing.T) {
	setPressTestEnv(t)
	_, err := FindStateByWorkingDir(filepath.Join(t.TempDir(), "missing-cli"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no runstate entry")
}
