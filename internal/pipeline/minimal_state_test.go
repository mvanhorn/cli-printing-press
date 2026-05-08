// Copyright 2026 trevin-chow. Licensed under Apache-2.0. See LICENSE.

package pipeline

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewMinimalState_NoRunstateLeavesRunIDEmpty exercises the back-compat
// path: a working dir with no adjacent runstate must produce a minimal state
// with RunID empty (today's behavior). Hand-built CLIs that genuinely have
// no prior runstate must keep working.
func TestNewMinimalState_NoRunstateLeavesRunIDEmpty(t *testing.T) {
	setPressTestEnv(t)

	state := NewMinimalState("hand-built-pp-cli", "/tmp/never-existed")

	assert.Equal(t, "hand-built", state.APIName, "APIName must be the trimmed CLI name")
	assert.Equal(t, "/tmp/never-existed", state.WorkingDir)
	assert.Equal(t, "/tmp/never-existed", state.OutputDir)
	assert.Empty(t, state.RunID, "RunID must stay empty when no runstate exists")
}

// TestNewMinimalState_RecoversRunIDFromRegistry exercises the U3 fix: when
// a runstate exists for this API in the scoped runstate registry, its RunID
// is borrowed so downstream state.PipelineDir() resolves to a path where
// research.json can be found. The caller's WorkingDir wins over the loaded
// state's so promotion still copies from the right source.
func TestNewMinimalState_RecoversRunIDFromRegistry(t *testing.T) {
	setPressTestEnv(t)

	// Plant a runstate as if a prior `generate` had stored one.
	prior := NewState("recovery-test", "/tmp/prior-working-dir")
	require.NotEmpty(t, prior.RunID, "NewState should mint a RunID")
	require.NoError(t, prior.Save(), "saving the prior runstate must succeed")

	// Now invoke NewMinimalState with a DIFFERENT working dir for the
	// same API (e.g., user's `lock promote` from a fresh checkout).
	state := NewMinimalState("recovery-test-pp-cli", "/tmp/different-working-dir")

	assert.Equal(t, "recovery-test", state.APIName)
	assert.Equal(t, "/tmp/different-working-dir", state.WorkingDir,
		"caller's WorkingDir must win — promotion copies from this dir")
	assert.Equal(t, "/tmp/different-working-dir", state.OutputDir,
		"caller's OutputDir must win")
	assert.Equal(t, prior.RunID, state.RunID,
		"RunID must be borrowed from the recovered runstate so PipelineDir resolves correctly")
}

// TestNewMinimalState_PipelineDirResolvesAfterRecovery proves the U3 contract:
// after recovery, state.PipelineDir() returns a real path where research.json
// would live, not the bogus RunPipelineDir("") that the pre-fix behavior
// produced.
func TestNewMinimalState_PipelineDirResolvesAfterRecovery(t *testing.T) {
	setPressTestEnv(t)

	prior := NewState("pipeline-dir-test", "/tmp/working")
	require.NoError(t, prior.Save())

	state := NewMinimalState("pipeline-dir-test-pp-cli", "/tmp/different-working")
	pipelineDir := state.PipelineDir()

	// The pre-fix path was RunPipelineDir("") which contains an empty
	// segment. Post-fix, the path must contain the recovered RunID.
	assert.Contains(t, pipelineDir, prior.RunID,
		"PipelineDir must include the recovered RunID, not be empty")
	assert.NotContains(t, pipelineDir, "//runs/pipeline",
		"PipelineDir must not contain the empty-RunID artifact `//runs/pipeline`")
}

// TestNewMinimalState_APINameMismatchDoesNotAdoptStaleRunID confirms that
// a runstate for a DIFFERENT API name does not get adopted. findRunstateStatePath
// already filters by APIName, so this should be a non-event — but the test
// guards against future regressions if the filtering is ever weakened.
func TestNewMinimalState_APINameMismatchDoesNotAdoptStaleRunID(t *testing.T) {
	setPressTestEnv(t)

	// Plant runstate for a different API.
	other := NewState("some-other-api", "/tmp/other-working")
	require.NoError(t, other.Save())

	// Create minimal state for an API with no prior runstate.
	state := NewMinimalState("brand-new-pp-cli", "/tmp/brand-new-working")

	assert.Equal(t, "brand-new", state.APIName)
	assert.Empty(t, state.RunID,
		"RunID must NOT be borrowed from a runstate with a different APIName")
}

// TestNewMinimalState_PreservesScopeFromRecoveredState confirms Scope is
// copied alongside RunID. Without Scope, downstream registry lookups (e.g.,
// findRunstateStatePath called transitively) may fail with empty-scope
// path resolution.
func TestNewMinimalState_PreservesScopeFromRecoveredState(t *testing.T) {
	setPressTestEnv(t)

	prior := NewState("scope-test", "/tmp/working")
	require.NoError(t, prior.Save())
	require.Equal(t, "test-scope", prior.Scope, "test env sets test-scope")

	state := NewMinimalState("scope-test-pp-cli", "/tmp/different")
	assert.Equal(t, "test-scope", state.Scope, "Scope must be borrowed from the recovered state")
}

func TestFindStateByWorkingDirFindsRunFromDifferentCurrentScope(t *testing.T) {
	home := setPressTestEnv(t)
	workingDir := filepath.Join(home, "work", "cross-scope-pp-cli")
	prior := NewState("cross-scope", workingDir)
	require.NoError(t, prior.Save())

	t.Setenv("PRINTING_PRESS_SCOPE", "fresh-publish-scope")

	state, err := FindStateByWorkingDir(workingDir)
	require.NoError(t, err)
	assert.Equal(t, prior.RunID, state.RunID)
	assert.Equal(t, "test-scope", state.Scope)
	assert.Equal(t,
		filepath.Join(home, ".runstate", "test-scope", "runs", prior.RunID, "pipeline"),
		state.PipelineDir(),
		"state path helpers should honor the recovered state's scope, not the current shell scope")
}
