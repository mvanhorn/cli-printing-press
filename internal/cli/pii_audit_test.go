package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/artifacts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPIIAuditCmd_RunsAndPersistsLedger(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "data.json"), `"email": "leak@example.com"`+"\n")

	cmd := newPIIAuditCmd()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{dir})

	require.NoError(t, cmd.Execute())

	// Default exit 0 even with pending findings
	assert.Contains(t, stdout.String(), "pending finding")

	// Ledger persisted
	_, err := os.Stat(filepath.Join(dir, artifacts.PIILedgerFilename))
	require.NoError(t, err)
}

func TestPIIAuditCmd_JSON(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "data.json"), `"email": "leak@example.com"`+"\n")

	cmd := newPIIAuditCmd()
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{dir, "--json"})

	require.NoError(t, cmd.Execute())

	var findings []artifacts.PIIFinding
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &findings))
	require.Len(t, findings, 1)
	assert.Equal(t, artifacts.PIIKindEmail, findings[0].Kind)
}

func TestPIIAuditCmd_StrictExitsNonZeroOnPending(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "data.json"), `"email": "leak@example.com"`+"\n")

	cmd := newPIIAuditCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{dir, "--strict"})

	err := cmd.Execute()
	require.Error(t, err)
	var exitErr *ExitError
	require.True(t, errors.As(err, &exitErr))
	assert.Equal(t, ExitGenerationError, exitErr.Code)
}

func TestPIIAuditCmd_StrictPassesWithValidAccepts(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "data.json"), `"email": "leak@example.com"`+"\n")

	// Run once to populate ledger
	cmd := newPIIAuditCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{dir})
	require.NoError(t, cmd.Execute())

	// Mutate ledger to accept the finding with valid pre-decision fields
	mutateLedger(t, dir, func(l *artifacts.PIILedger) {
		for i := range l.Findings {
			l.Findings[i].Status = artifacts.PIIStatusAccepted
			l.Findings[i].Category = artifacts.PIICategoryDocumentationExample
			l.Findings[i].EvidenceContext = "example email used in README documentation block"
		}
	})

	// Re-run with --strict; should now pass
	cmd2 := newPIIAuditCmd()
	cmd2.SetOut(&bytes.Buffer{})
	cmd2.SetErr(&bytes.Buffer{})
	cmd2.SetArgs([]string{dir, "--strict"})
	assert.NoError(t, cmd2.Execute())
}

func TestPIIAuditCmd_StrictFailsOnAcceptMissingCategory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "data.json"), `"email": "leak@example.com"`+"\n")

	cmd := newPIIAuditCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{dir})
	require.NoError(t, cmd.Execute())

	mutateLedger(t, dir, func(l *artifacts.PIILedger) {
		for i := range l.Findings {
			l.Findings[i].Status = artifacts.PIIStatusAccepted
			// Note: category intentionally omitted
			l.Findings[i].EvidenceContext = "ctx"
		}
	})

	cmd2 := newPIIAuditCmd()
	stdout := &bytes.Buffer{}
	cmd2.SetOut(stdout)
	cmd2.SetErr(&bytes.Buffer{})
	cmd2.SetArgs([]string{dir, "--strict"})
	err := cmd2.Execute()
	require.Error(t, err)
	assert.Contains(t, stdout.String(), "pre-decision fields")
}

func TestPIIAuditCmd_AgentFieldsSurviveReRun(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "data.json"), `"email": "leak@example.com"`+"\n")

	// Run once
	cmd := newPIIAuditCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{dir})
	require.NoError(t, cmd.Execute())

	// Agent accepts the finding
	mutateLedger(t, dir, func(l *artifacts.PIILedger) {
		for i := range l.Findings {
			l.Findings[i].Status = artifacts.PIIStatusAccepted
			l.Findings[i].Category = artifacts.PIICategoryDocumentationExample
			l.Findings[i].EvidenceContext = "ctx"
		}
	})

	// Re-run; agent field should be preserved
	cmd2 := newPIIAuditCmd()
	cmd2.SetOut(&bytes.Buffer{})
	cmd2.SetErr(&bytes.Buffer{})
	cmd2.SetArgs([]string{dir})
	require.NoError(t, cmd2.Execute())

	ledger := artifacts.ReadPIILedger(dir)
	require.NotNil(t, ledger)
	require.Len(t, ledger.Findings, 1)
	assert.Equal(t, artifacts.PIIStatusAccepted, ledger.Findings[0].Status)
	assert.Equal(t, artifacts.PIICategoryDocumentationExample, ledger.Findings[0].Category)
}

func TestPIIAuditCmd_CleanDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "clean.json"), `"version": "1.2.3"`+"\n")

	cmd := newPIIAuditCmd()
	stdout := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{dir})
	require.NoError(t, cmd.Execute())

	assert.Contains(t, stdout.String(), "no findings")

	ledger := artifacts.ReadPIILedger(dir)
	require.NotNil(t, ledger)
	assert.Equal(t, 0, ledger.FindingsCountBefore)
}

func TestPIIAuditCmd_TypedExitCodesAnnotation(t *testing.T) {
	cmd := newPIIAuditCmd()
	assert.Equal(t, "0,3", cmd.Annotations["pp:typed-exit-codes"])
	// Absent mcp:read-only annotation — pii-audit writes a ledger file.
	_, hasReadOnly := cmd.Annotations["mcp:read-only"]
	assert.False(t, hasReadOnly, "pii-audit must not claim mcp:read-only; it mutates the cli-dir by writing a ledger")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}

func mutateLedger(t *testing.T, dir string, mutate func(*artifacts.PIILedger)) {
	t.Helper()
	ledger := artifacts.ReadPIILedger(dir)
	require.NotNil(t, ledger)
	mutate(ledger)
	data, err := json.MarshalIndent(ledger, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, artifacts.PIILedgerFilename), append(data, '\n'), 0644))
}
