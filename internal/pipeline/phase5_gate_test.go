package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writePhase5GateMarker(t *testing.T, proofsDir, name string, marker Phase5GateMarker) {
	t.Helper()
	require.NoError(t, os.MkdirAll(proofsDir, 0o755))
	data, err := json.MarshalIndent(marker, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(proofsDir, name), data, 0o644))
}

func TestValidatePhase5Gate_PassMarker(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "pass",
		Level:         "full",
		MatrixSize:    3,
		TestsPassed:   3,
		TestsFailed:   0,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.True(t, result.Passed, result.Detail)
	assert.Equal(t, "pass", result.Status)
	assert.Equal(t, filepath.Join(proofsDir, Phase5AcceptanceFilename), result.MarkerPath)
}

func TestValidatePhase5Gate_QuickPassAllowsOneNonBlockingMiss(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "pass",
		Level:         "quick",
		MatrixSize:    6,
		TestsPassed:   5,
		TestsFailed:   1,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.True(t, result.Passed, result.Detail)
	assert.Equal(t, "pass", result.Status)
}

func TestValidatePhase5Gate_QuickPassRequiresFiveOfSix(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "pass",
		Level:         "quick",
		MatrixSize:    6,
		TestsPassed:   4,
		TestsFailed:   2,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "5/6")
}

func TestValidatePhase5Gate_FullPassRejectsFailures(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "pass",
		Level:         "full",
		MatrixSize:    6,
		TestsPassed:   5,
		TestsFailed:   1,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "full")
}

func TestValidatePhase5Gate_NoAuthRequiresPassMarker(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5SkipFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "skip",
		Level:         "none",
		SkipReason:    "auth_required_no_credential",
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "no-auth")
}

func TestValidatePhase5Gate_APIKeyMissingSkipAllowed(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "api_key"}
	writePhase5GateMarker(t, proofsDir, Phase5SkipFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "skip",
		Level:         "none",
		SkipReason:    "auth_required_no_credential",
		AuthContext:   Phase5AuthContext{Type: "api_key", APIKeyAvailable: false},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.True(t, result.Passed, result.Detail)
	assert.Equal(t, "skip", result.Status)
}

func TestValidatePhase5Gate_CookieAuthNotSkippedByMissingAPIKey(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "cookie"}
	writePhase5GateMarker(t, proofsDir, Phase5SkipFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "skip",
		Level:         "none",
		SkipReason:    "auth_required_no_credential",
		AuthContext:   Phase5AuthContext{Type: "cookie", APIKeyAvailable: false},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "browser-session")
}

func TestValidatePhase5Gate_SkipCannotOverrideManifestAuthType(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "cookie"}
	writePhase5GateMarker(t, proofsDir, Phase5SkipFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "skip",
		Level:         "none",
		SkipReason:    "auth_required_no_credential",
		AuthContext:   Phase5AuthContext{Type: "api_key", APIKeyAvailable: false},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "does not match")
}

func TestValidatePhase5Gate_PassMarkerRequiresIdentityAndTestCount(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		Status:        "pass",
		Level:         "full",
		MatrixSize:    1,
		TestsPassed:   1,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "api_name")
}

func TestValidatePhase5Gate_SkipMarkerRequiresIdentity(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "api_key"}
	writePhase5GateMarker(t, proofsDir, Phase5SkipFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		Status:        "skip",
		Level:         "none",
		SkipReason:    "auth_required_no_credential",
		AuthContext:   Phase5AuthContext{Type: "api_key", APIKeyAvailable: false},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "run_id")
}

func TestValidatePhase5Gate_MissingMarkerFails(t *testing.T) {
	result := ValidatePhase5Gate(t.TempDir(), CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "api_key"})
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "missing")
}
