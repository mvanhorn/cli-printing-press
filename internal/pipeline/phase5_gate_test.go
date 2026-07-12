package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
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

func TestValidatePhase5Gate_QuickPassRejectsFailures(t *testing.T) {
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
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "failed tests")
}

func TestValidatePhase5Gate_QuickRejectsFailuresBeforeThreshold(t *testing.T) {
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
	assert.Contains(t, result.Detail, "failed tests")
}

func TestValidatePhase5Gate_QuickPassFiveOfFive(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "pass",
		Level:         "quick",
		MatrixSize:    5,
		TestsPassed:   5,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.True(t, result.Passed, result.Detail)
	assert.Equal(t, "pass", result.Status)
}

func TestValidatePhase5Gate_QuickPassFourOfFour(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "pass",
		Level:         "quick",
		MatrixSize:    4,
		TestsPassed:   4,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.True(t, result.Passed, result.Detail)
	assert.Equal(t, "pass", result.Status)
}

func TestValidatePhase5Gate_QuickPassCountsSkippedTowardThreshold(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none"}
	// Runner verdict: MatrixSize=4 (Passed+Failed only), Skipped=1. Passed+Skipped=5,
	// min(5, 4)=4 → quick PASS. Without TestsSkipped in the gate predicate this
	// marker would be rejected at MatrixSize=4 with TestsPassed=4 alone.
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "pass",
		Level:         "quick",
		MatrixSize:    4,
		TestsPassed:   4,
		TestsSkipped:  1,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.True(t, result.Passed, result.Detail)
	assert.Equal(t, "pass", result.Status)
}

func TestValidatePhase5Gate_QuickFailWithSkipsBelowMatrixFloor(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "pass",
		Level:         "quick",
		MatrixSize:    2,
		TestsPassed:   2,
		TestsSkipped:  2,
		TestsFailed:   0,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "matrix_size >= 4")
}

func TestValidatePhase5Gate_QuickPassWithSkipsRequiresCountedTestsPassed(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "pass",
		Level:         "quick",
		MatrixSize:    4,
		TestsPassed:   3,
		TestsSkipped:  1,
		TestsFailed:   0,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "counted tests passed")
}

func TestValidatePhase5Gate_QuickFailMatrixBelowFloor(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "pass",
		Level:         "quick",
		MatrixSize:    3,
		TestsPassed:   3,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "matrix_size >= 4")
}

func TestValidatePhase5Gate_QuickFailBelowThresholdAtMatrixFour(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "pass",
		Level:         "quick",
		MatrixSize:    4,
		TestsPassed:   3,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "counted tests passed")
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

func TestValidatePhase5Gate_FullPassAcceptsAccountedSkips(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "pass",
		Level:         "full",
		MatrixSize:    110,
		TestsPassed:   66,
		TestsSkipped:  44,
		TestsFailed:   0,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.True(t, result.Passed, result.Detail)
	assert.Equal(t, "pass", result.Status)
}

func TestValidatePhase5Gate_FullPassRejectsSilentGaps(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "pass",
		Level:         "full",
		MatrixSize:    110,
		TestsPassed:   66,
		TestsSkipped:  43,
		TestsFailed:   0,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "accounted")
}

func TestValidatePhase5Gate_ManualLevelDocumentsAcceptedValues(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "pass",
		Level:         "manual",
		MatrixSize:    2,
		TestsPassed:   2,
		TestsFailed:   0,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, `"manual"`)
	assert.Contains(t, result.Detail, "accepted: quick, full")
	assert.Contains(t, result.Detail, "dogfood --live --write-acceptance")
}

func TestValidatePhase5Gate_ReportsMultipleAcceptanceMarkerIssues(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 2,
		Status:        "maybe",
		Level:         "smoke",
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "unsupported phase5 marker schema_version 2")
	assert.Contains(t, result.Detail, "phase5 marker missing api_name")
	assert.Contains(t, result.Detail, "phase5 marker missing run_id")
	assert.Contains(t, result.Detail, `unknown phase5 gate status "maybe"`)
	assert.Contains(t, result.Detail, "accepted: pass, fail")
	assert.Contains(t, result.Detail, `unknown phase5 acceptance level "smoke"`)
	assert.Contains(t, result.Detail, "accepted: quick, full")
	assert.Contains(t, result.Detail, "missing matrix_size")
	assert.Contains(t, result.Detail, "missing tests_passed")
}

func TestValidatePhase5Gate_UnknownLevelDocumentsAcceptedValues(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "pass",
		Level:         "smoke",
		MatrixSize:    1,
		TestsPassed:   1,
		TestsFailed:   0,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, `"smoke"`)
	assert.Contains(t, result.Detail, "accepted: quick, full")
	assert.NotContains(t, result.Detail, "manual")
	assert.Contains(t, result.Detail, "dogfood --live --write-acceptance")
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
		SkipReason:    phase5SkipReasonAuthRequiredNoCredential,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "no-auth")
}

func TestValidatePhase5Gate_SyntheticNoAuthAllowsExternalCredentialSkip(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "aws-billing", CLIName: "aws-billing-pp-cli", RunID: "run-1", AuthType: "none", SpecKind: spec.KindSynthetic}
	writePhase5GateMarker(t, proofsDir, Phase5SkipFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "aws-billing",
		RunID:         "run-1",
		Status:        "skip",
		Level:         "none",
		SkipReason:    phase5SkipReasonExternalCredentialsUnavailable,
		AuthContext:   Phase5AuthContext{Type: "oauth2", APIKeyAvailable: false},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.True(t, result.Passed, result.Detail)
	assert.Equal(t, "skip", result.Status)
}

func TestValidatePhase5Gate_StandardNoAuthRejectsExternalCredentialSkip(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "public-data", CLIName: "public-data-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5SkipFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "public-data",
		RunID:         "run-1",
		Status:        "skip",
		Level:         "none",
		SkipReason:    phase5SkipReasonExternalCredentialsUnavailable,
		AuthContext:   Phase5AuthContext{Type: "none", APIKeyAvailable: false},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "no-auth")
}

func TestValidatePhase5Gate_NoAuthLANUnreachableSkipAllowed(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "sonos", CLIName: "sonos-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5SkipFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "sonos",
		RunID:         "run-1",
		Status:        "skip",
		Level:         "none",
		SkipReason:    phase5SkipReasonLANUnreachableFromHost,
		AuthContext:   Phase5AuthContext{Type: "none", LocalNetworkOnly: true},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.True(t, result.Passed, result.Detail)
	assert.Equal(t, "skip", result.Status)
}

func TestValidatePhase5Gate_NoAuthLANUnreachableRequiresLANOnlyEvidence(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "public-data", CLIName: "public-data-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5SkipFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "public-data",
		RunID:         "run-1",
		Status:        "skip",
		Level:         "none",
		SkipReason:    phase5SkipReasonLANUnreachableFromHost,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "local_network_only")
}

func TestValidatePhase5Gate_LocalDatastoreNoAuthAllowsSkipMarker(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none", SpecFormat: "sqlite"}
	writePhase5GateMarker(t, proofsDir, Phase5SkipFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "skip",
		Level:         "none",
		SkipReason:    phase5SkipReasonLocalSourceRequiresDatabase,
		AuthContext:   Phase5AuthContext{Type: "none", LocalSQLite: true},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.True(t, result.Passed, result.Detail)
	assert.Equal(t, "skip", result.Status)
}

func TestValidatePhase5Gate_LocalDatastoreNoAuthRejectsUnrecognizedSkipReason(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none", SpecFormat: "sqlite"}
	writePhase5GateMarker(t, proofsDir, Phase5SkipFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "skip",
		Level:         "none",
		SkipReason:    "operator deferred",
		AuthContext:   Phase5AuthContext{Type: "none", LocalSQLite: true},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "local datastore")
}

func TestValidatePhase5Gate_LocalDatastoreNoAuthRejectsLANUnreachableSkip(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "none", SpecFormat: "sqlite"}
	writePhase5GateMarker(t, proofsDir, Phase5SkipFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "skip",
		Level:         "none",
		SkipReason:    phase5SkipReasonLANUnreachableFromHost,
		AuthContext:   Phase5AuthContext{Type: "none", LocalNetworkOnly: true},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "local datastore")
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
		SkipReason:    phase5SkipReasonAuthRequiredNoCredential,
		AuthContext:   Phase5AuthContext{Type: "api_key", APIKeyAvailable: false},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.True(t, result.Passed, result.Detail)
	assert.Equal(t, "skip", result.Status)
}

func TestValidatePhase5Gate_CredentialAuthRejectsLANSkipReason(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "api_key"}
	writePhase5GateMarker(t, proofsDir, Phase5SkipFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "skip",
		Level:         "none",
		SkipReason:    phase5SkipReasonLANUnreachableFromHost,
		AuthContext:   Phase5AuthContext{Type: "api_key", APIKeyAvailable: false},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "not valid")
	assert.Contains(t, result.Detail, "api_key")
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
		SkipReason:    phase5SkipReasonAuthRequiredNoCredential,
		AuthContext:   Phase5AuthContext{Type: "cookie", APIKeyAvailable: false},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "browser-session")
}

func TestValidatePhase5Gate_CookieAuthNoHarnessSessionSkipAllowed(t *testing.T) {
	for _, authType := range []string{"cookie", "composed", "session_handshake"} {
		t.Run(authType, func(t *testing.T) {
			proofsDir := t.TempDir()
			manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: authType}
			writePhase5GateMarker(t, proofsDir, Phase5SkipFilename, Phase5GateMarker{
				SchemaVersion: 1,
				APIName:       "test",
				RunID:         "run-1",
				Status:        "skip",
				Level:         "none",
				SkipReason:    phase5SkipReasonCookieAuthNoHarnessSession,
				AuthContext:   Phase5AuthContext{Type: authType, BrowserSessionAvailable: false},
			})

			result := ValidatePhase5Gate(proofsDir, manifest)
			require.True(t, result.Passed, result.Detail)
			assert.Equal(t, "skip", result.Status)
		})
	}
}

func TestValidatePhase5Gate_CookieAuthRejectsWrongSkipReason(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "cookie"}
	writePhase5GateMarker(t, proofsDir, Phase5SkipFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		RunID:         "run-1",
		Status:        "skip",
		Level:         "none",
		SkipReason:    "operator deferred",
		AuthContext:   Phase5AuthContext{Type: "cookie"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "cookie-auth-no-harness-session")
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
		SkipReason:    phase5SkipReasonAuthRequiredNoCredential,
		AuthContext:   Phase5AuthContext{Type: "api_key", APIKeyAvailable: false},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "does not match")
}

func TestValidatePhase5Gate_PassMarkerRejectsEmptyIdentityWhenManifestIdentifies(t *testing.T) {
	// Stale-marker protection: when the manifest identifies the CLI, an
	// empty-identity marker would otherwise pass every future promote.
	// Reject it so cross-check enforcement degrades only for the actual
	// unidentified-manifest case.
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

func TestValidatePhase5Gate_PassMarkerAllowsEmptyIdentityWhenManifestUnidentified(t *testing.T) {
	// dogfood --write-acceptance may run for a foreign working dir with no
	// manifest and no runstate; the marker then has no identity to record
	// and the gate has no manifest identity to compare against either.
	// The marker still validates because the cross-check has nothing to
	// enforce.
	proofsDir := t.TempDir()
	manifest := CLIManifest{AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		Status:        "pass",
		Level:         "full",
		MatrixSize:    1,
		TestsPassed:   1,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.True(t, result.Passed, result.Detail)
	assert.Equal(t, "pass", result.Status)
}

func TestValidatePhase5Gate_PassMarkerCrossChecksIdentityWhenPresent(t *testing.T) {
	// When the marker does carry identity, mismatches against the manifest
	// must still be rejected — this is what prevents stale markers from a
	// prior run leaking into a fresh promote.
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "stripe", CLIName: "stripe-pp-cli", RunID: "run-1", AuthType: "none"}
	writePhase5GateMarker(t, proofsDir, Phase5AcceptanceFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "notion",
		RunID:         "run-1",
		Status:        "pass",
		Level:         "full",
		MatrixSize:    1,
		TestsPassed:   1,
		AuthContext:   Phase5AuthContext{Type: "none"},
	})

	result := ValidatePhase5Gate(proofsDir, manifest)
	require.False(t, result.Passed)
	assert.Contains(t, result.Detail, "does not match")
}

func TestValidatePhase5Gate_SkipMarkerRequiresIdentity(t *testing.T) {
	proofsDir := t.TempDir()
	manifest := CLIManifest{APIName: "test", CLIName: "test-pp-cli", RunID: "run-1", AuthType: "api_key"}
	writePhase5GateMarker(t, proofsDir, Phase5SkipFilename, Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       "test",
		Status:        "skip",
		Level:         "none",
		SkipReason:    phase5SkipReasonAuthRequiredNoCredential,
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
