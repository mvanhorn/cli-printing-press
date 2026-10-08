package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateProofFileName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "markdown", value: "undo-lifecycle.md"},
		{name: "text", value: "undo-lifecycle.TXT"},
		{name: "empty", value: "", wantErr: true},
		{name: "parent traversal", value: "../undo.md", wantErr: true},
		{name: "embedded traversal", value: "a..b.md", wantErr: true},
		{name: "subdirectory", value: "proofs/undo.md", wantErr: true},
		{name: "windows separator", value: `proofs\undo.md`, wantErr: true},
		{name: "absolute", value: "/tmp/undo.md", wantErr: true},
		{name: "hidden", value: ".undo.md", wantErr: true},
		{name: "json report", value: "undo.json", wantErr: true},
		{name: "surrounding whitespace", value: " undo.md", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateProofFileName(tt.value)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestFinalizeLiveDogfoodCoverageProofCoverage(t *testing.T) {
	const proofName = "undo-lifecycle.md"
	const proofBody = "# undo lifecycle\n\n$ cli apply plan.json --yes  (exit 0)\n$ cli undo batch-1 --yes  (exit 0)\nsandbox: scratch folder, cleaned up\n"

	dryRunPass := []LiveDogfoodTestResult{
		{Command: "undo", Kind: LiveDogfoodTestHelp, Status: LiveDogfoodStatusPass},
		{Command: "undo", Kind: LiveDogfoodTestHappy, Status: LiveDogfoodStatusPass, Args: []string{"undo", "batch-1", "--dry-run"}},
	}
	annotated := func(value string) []liveDogfoodCommand {
		return []liveDogfoodCommand{{Path: []string{"undo"}, Annotations: map[string]string{verifiedByProofAnnotation: value}}}
	}

	tests := []struct {
		name       string
		commands   []liveDogfoodCommand
		tests      []LiveDogfoodTestResult
		proofFile  string
		proofBody  string
		noProofDir bool
		wantProof  bool
	}{
		{
			name:      "annotated with proof and dry-run pass is covered",
			commands:  annotated(proofName),
			tests:     dryRunPass,
			proofFile: proofName,
			proofBody: proofBody,
			wantProof: true,
		},
		{
			name:     "dry_run_json pass also proves the command still works",
			commands: annotated(proofName),
			tests: []LiveDogfoodTestResult{
				{Command: "undo", Kind: LiveDogfoodTestHelp, Status: LiveDogfoodStatusPass},
				{Command: "undo", Kind: LiveDogfoodTestDryRunJSON, Status: LiveDogfoodStatusPass},
			},
			proofFile: proofName,
			proofBody: proofBody,
			wantProof: true,
		},
		{
			name:     "annotation without proof file stays hollow",
			commands: annotated(proofName),
			tests:    dryRunPass,
		},
		{
			name:      "proof that never mentions the command stays hollow",
			commands:  annotated(proofName),
			tests:     dryRunPass,
			proofFile: proofName,
			proofBody: "ran some things, all good\n",
		},
		{
			name:      "empty proof stays hollow",
			commands:  annotated(proofName),
			tests:     dryRunPass,
			proofFile: proofName,
			proofBody: "  \n",
		},
		{
			name:      "unannotated command with proof file present stays hollow",
			commands:  []liveDogfoodCommand{{Path: []string{"undo"}}},
			tests:     dryRunPass,
			proofFile: proofName,
			proofBody: proofBody,
		},
		{
			name:      "path traversal annotation stays hollow",
			commands:  annotated("../" + proofName),
			tests:     dryRunPass,
			proofFile: proofName,
			proofBody: proofBody,
		},
		{
			name:       "no known proofs directory stays hollow",
			commands:   annotated(proofName),
			tests:      dryRunPass,
			proofFile:  proofName,
			proofBody:  proofBody,
			noProofDir: true,
		},
		{
			name:     "no passing dry-run check stays hollow",
			commands: annotated(proofName),
			tests: []LiveDogfoodTestResult{
				{Command: "undo", Kind: LiveDogfoodTestHelp, Status: LiveDogfoodStatusPass},
				{Command: "undo", Kind: LiveDogfoodTestHappy, Status: LiveDogfoodStatusFail, Args: []string{"undo", "batch-1", "--dry-run"}},
			},
			proofFile: proofName,
			proofBody: proofBody,
		},
		{
			name:     "failed help stays hollow",
			commands: annotated(proofName),
			tests: []LiveDogfoodTestResult{
				{Command: "undo", Kind: LiveDogfoodTestHelp, Status: LiveDogfoodStatusFail},
				{Command: "undo", Kind: LiveDogfoodTestHappy, Status: LiveDogfoodStatusPass, Args: []string{"undo", "batch-1", "--dry-run"}},
			},
			proofFile: proofName,
			proofBody: proofBody,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			researchDir := t.TempDir()
			require.NoError(t, writeResearchJSON(&ResearchResult{
				NovelFeatures: []NovelFeature{{Name: "Undo", Command: "undo"}},
			}, researchDir))
			root := t.TempDir()
			proofsDir := filepath.Join(root, "proofs")
			require.NoError(t, os.MkdirAll(proofsDir, 0o755))
			if tt.proofFile != "" {
				require.NoError(t, os.WriteFile(filepath.Join(proofsDir, tt.proofFile), []byte(tt.proofBody), 0o644))
				// A traversal value must not reach a proof outside the dir.
				require.NoError(t, os.WriteFile(filepath.Join(root, tt.proofFile), []byte(tt.proofBody), 0o644))
			}
			if tt.noProofDir {
				proofsDir = ""
			}

			report := &LiveDogfoodReport{Commands: []string{"undo"}, Tests: tt.tests}
			finalizeLiveDogfoodCoverage(report, researchDir, liveDogfoodProofContext{commands: tt.commands, proofsDir: proofsDir})

			if tt.wantProof {
				assert.False(t, report.CoverageHollow)
				assert.Empty(t, report.HollowFeatures)
				assert.Equal(t, []ProofCoveredFeature{{Command: "undo", Proof: proofName}}, report.ProofCoveredFeatures)
				return
			}
			assert.True(t, report.CoverageHollow)
			assert.Equal(t, []string{"undo"}, report.HollowFeatures)
			assert.Empty(t, report.ProofCoveredFeatures)
		})
	}
}

func TestFinalizeLiveDogfoodCoveragePrefersLivePassOverProof(t *testing.T) {
	researchDir := t.TempDir()
	require.NoError(t, writeResearchJSON(&ResearchResult{
		NovelFeatures: []NovelFeature{{Name: "Undo", Command: "undo"}},
	}, researchDir))
	proofsDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proofsDir, "undo.md"), []byte("undo ran\n"), 0o644))

	report := &LiveDogfoodReport{
		Commands: []string{"undo"},
		Tests: []LiveDogfoodTestResult{
			{Command: "undo", Kind: LiveDogfoodTestHelp, Status: LiveDogfoodStatusPass},
			{Command: "undo", Kind: LiveDogfoodTestHappy, Status: LiveDogfoodStatusPass, Args: []string{"undo"}},
		},
	}
	finalizeLiveDogfoodCoverage(report, researchDir, liveDogfoodProofContext{
		commands:  []liveDogfoodCommand{{Path: []string{"undo"}, Annotations: map[string]string{verifiedByProofAnnotation: "undo.md"}}},
		proofsDir: proofsDir,
	})
	assert.False(t, report.CoverageHollow)
	assert.Empty(t, report.ProofCoveredFeatures, "a live pass is recorded as a live pass, not a proof exception")
}

func phase5ProofTestMarker() Phase5GateMarker {
	return Phase5GateMarker{
		SchemaVersion: 1,
		Status:        "pass",
		Level:         "full",
		MatrixSize:    10,
		TestsPassed:   10,
	}
}

func writePhase5ProofTestMarker(t *testing.T, dir string, marker Phase5GateMarker) {
	t.Helper()
	data, err := json.Marshal(marker)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, Phase5AcceptanceFilename), data, 0o644))
}

func TestValidatePhase5GateProofCoveredFeatures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mutate      func(*Phase5GateMarker)
		writeProof  bool
		wantPassed  bool
		wantInError string
	}{
		{
			name: "proof-covered feature with proof beside the marker passes",
			mutate: func(m *Phase5GateMarker) {
				m.ProofCoveredFeatures = []ProofCoveredFeature{{Command: "undo", Proof: "undo-lifecycle.md"}}
			},
			writeProof: true,
			wantPassed: true,
		},
		{
			name: "proof file that did not travel with the marker fails",
			mutate: func(m *Phase5GateMarker) {
				m.ProofCoveredFeatures = []ProofCoveredFeature{{Command: "undo", Proof: "undo-lifecycle.md"}}
			},
			wantInError: "phase5 proof for \"undo\"",
		},
		{
			name: "traversal proof name fails",
			mutate: func(m *Phase5GateMarker) {
				m.ProofCoveredFeatures = []ProofCoveredFeature{{Command: "undo", Proof: "../undo-lifecycle.md"}}
			},
			writeProof:  true,
			wantInError: "bare filename",
		},
		{
			name: "hollow coverage still fails even with proof-covered features",
			mutate: func(m *Phase5GateMarker) {
				m.CoverageHollow = true
				m.HollowFeatures = []string{"apply"}
				m.ProofCoveredFeatures = []ProofCoveredFeature{{Command: "undo", Proof: "undo-lifecycle.md"}}
			},
			writeProof:  true,
			wantInError: "hollow coverage for: apply",
		},
		{
			name: "a feature cannot be both hollow and proof-covered",
			mutate: func(m *Phase5GateMarker) {
				m.HollowFeatures = []string{"undo"}
				m.ProofCoveredFeatures = []ProofCoveredFeature{{Command: "undo", Proof: "undo-lifecycle.md"}}
			},
			writeProof:  true,
			wantInError: "both hollow and proof-covered",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			marker := phase5ProofTestMarker()
			tt.mutate(&marker)
			writePhase5ProofTestMarker(t, dir, marker)
			if tt.writeProof {
				require.NoError(t, os.WriteFile(filepath.Join(dir, "undo-lifecycle.md"), []byte("$ cli undo batch-1 --yes (exit 0)\n"), 0o644))
			}
			result := ValidatePhase5Gate(dir, CLIManifest{})
			assert.Equal(t, tt.wantPassed, result.Passed, result.Detail)
			if tt.wantInError != "" {
				assert.Contains(t, result.Detail, tt.wantInError)
			}
		})
	}
}

func TestCopyProofFilesMirrorsReferencedProofs(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "proofs")
	require.NoError(t, os.WriteFile(filepath.Join(src, "undo-lifecycle.md"), []byte("undo ran\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(src, "unrelated.md"), []byte("other\n"), 0o644))

	require.NoError(t, copyProofFiles(src, dst, []ProofCoveredFeature{{Command: "undo", Proof: "undo-lifecycle.md"}}))
	got, err := os.ReadFile(filepath.Join(dst, "undo-lifecycle.md"))
	require.NoError(t, err)
	assert.Equal(t, "undo ran\n", string(got))
	_, err = os.Stat(filepath.Join(dst, "unrelated.md"))
	assert.True(t, os.IsNotExist(err))

	assert.Error(t, copyProofFiles(src, dst, []ProofCoveredFeature{{Command: "undo", Proof: "../escape.md"}}))
}

func TestCopyProofFilesSkipsSymlinkedSameFile(t *testing.T) {
	t.Parallel()

	runstateProofs := t.TempDir()
	proof := filepath.Join(runstateProofs, "undo-lifecycle.md")
	require.NoError(t, os.WriteFile(proof, []byte("$ cli undo batch-1 --yes (exit 0)\n"), 0o644))
	acceptanceDir := filepath.Join(t.TempDir(), "proofs-link")
	require.NoError(t, os.Symlink(runstateProofs, acceptanceDir))

	require.NoError(t, copyProofFiles(acceptanceDir, runstateProofs, []ProofCoveredFeature{{Command: "undo", Proof: "undo-lifecycle.md"}}))
	got, err := os.ReadFile(proof)
	require.NoError(t, err)
	assert.Equal(t, "$ cli undo batch-1 --yes (exit 0)\n", string(got), "copying a file onto itself must not truncate the proof")
}

func TestReadProofFileRejectsUnpublishableSize(t *testing.T) {
	t.Parallel()

	assert.False(t, proofFileTooLarge(publishableManuscriptMaxCaptureBytes-1))
	assert.True(t, proofFileTooLarge(publishableManuscriptMaxCaptureBytes))

	// A sparse file at the limit is rejected from its size alone, before
	// anything is read.
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "huge.md"))
	require.NoError(t, err)
	require.NoError(t, f.Truncate(publishableManuscriptMaxCaptureBytes))
	require.NoError(t, f.Close())
	_, err = readProofFile(dir, "huge.md")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be under")
}

func TestPublishableManuscriptCopyCarriesProofFilesWithMarker(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "proofs")
	marker := phase5ProofTestMarker()
	marker.ProofCoveredFeatures = []ProofCoveredFeature{{Command: "undo", Proof: "undo-lifecycle.md"}}
	writePhase5ProofTestMarker(t, src, marker)
	require.NoError(t, os.WriteFile(filepath.Join(src, "undo-lifecycle.md"), []byte("$ cli undo batch-1 --yes (exit 0)\n"), 0o644))

	// lock promote and publish package copy the proofs dir with this helper;
	// the proof must survive so the gate still passes at the destination.
	require.NoError(t, CopyPublishableManuscriptDir(src, dst))
	result := ValidatePhase5Gate(dst, CLIManifest{})
	assert.True(t, result.Passed, result.Detail)
}

func TestRunLiveDogfoodWritesProofCoveredMarkerThatPassesGate(t *testing.T) {
	dir := t.TempDir()
	binaryName := "fixture-pp-cli"
	writeTestManifestForLiveDogfood(t, dir)
	researchDir := t.TempDir()
	require.NoError(t, writeResearchJSON(&ResearchResult{
		NovelFeatures: []NovelFeature{{Name: "Undo", Command: "undo"}},
	}, researchDir))
	proofsDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(proofsDir, "undo-lifecycle.md"),
		[]byte("Sandbox folder created.\n$ fixture-pp-cli undo batch-1 --yes  (exit 0)\nSandbox removed.\n"), 0o644))

	// undo previews only for a batch that exists, so without --dry-run the
	// live matrix can never pass it; the dry-run probe still proves it runs.
	script := `set -u
if [ "$1" = "agent-context" ]; then
  cat <<'JSON'
{"commands":[{"name":"undo","annotations":{"pp:happy-args":"batch-id=batch-1","pp:verified-by-proof":"undo-lifecycle.md"}}]}
JSON
  exit 0
fi
if [ "${2:-}" = "--help" ]; then
  cat <<'HELP'
Undo a batch.

Usage:
  fixture-pp-cli undo <batch-id> [flags]

Examples:
  fixture-pp-cli undo batch-1

Flags:
      --yes   Apply the undo

Global Flags:
      --dry-run   Show request without sending
      --json      Output as JSON
HELP
  exit 0
fi
for a in "$@"; do
  if [ "$a" = "--dry-run" ]; then echo '{"dry_run":true,"action":"undo"}'; exit 0; fi
done
echo 'batch not found' >&2
exit 2
`
	writeStubBinary(t, dir, binaryName, script)

	acceptance := filepath.Join(proofsDir, Phase5AcceptanceFilename)
	report, err := RunLiveDogfood(LiveDogfoodOptions{
		CLIDir:              dir,
		BinaryName:          binaryName,
		Level:               "full",
		Timeout:             2 * time.Second,
		ResearchDir:         researchDir,
		WriteAcceptancePath: acceptance,
	})
	require.NoError(t, err)
	assert.False(t, report.CoverageHollow, report.HollowFeatures)
	assert.Equal(t, []ProofCoveredFeature{{Command: "undo", Proof: "undo-lifecycle.md"}}, report.ProofCoveredFeatures)

	data, err := os.ReadFile(acceptance)
	require.NoError(t, err)
	var marker Phase5GateMarker
	require.NoError(t, json.Unmarshal(data, &marker))
	assert.Equal(t, report.ProofCoveredFeatures, marker.ProofCoveredFeatures)
	assert.False(t, marker.CoverageHollow)

	result := ValidatePhase5Gate(proofsDir, CLIManifest{})
	assert.True(t, result.Passed, result.Detail)
}
