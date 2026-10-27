package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRenderHumanScorecardShowsNAForUnscoredDimensions — the scorer
// reports "didn't measure this" via the UnscoredDimensions slice.
// Without N/A rendering the user sees "MCP Tool Design 0/10" alongside
// real defects and reads it as a real defect, when in fact the spec
// hasn't opted into the feature being scored.
func TestRenderHumanScorecardShowsNAForUnscoredDimensions(t *testing.T) {
	t.Parallel()
	sc := &pipeline.Scorecard{
		APIName:      "test-api",
		OverallGrade: "B",
		Steinberger: pipeline.SteinerScore{
			OutputModes:           10,
			Auth:                  10,
			ErrorHandling:         10,
			TerminalUX:            9,
			README:                8,
			Doctor:                10,
			AgentNative:           10,
			MCPDescriptionQuality: 10,
			LocalCache:            10,
			Breadth:               7,
			Vision:                8,
			Workflows:             8,
			Insight:               6,
			AgentWorkflow:         9,
			Total:                 85,
		},
		UnscoredDimensions: []string{
			pipeline.DimMCPTokenEfficiency,
			pipeline.DimMCPRemoteTransport,
			pipeline.DimMCPToolDesign,
			pipeline.DimMCPSurfaceStrategy,
			pipeline.DimCacheFreshness,
			pipeline.DimAuthProtocol,
			pipeline.DimLiveAPIVerification,
		},
	}

	var buf bytes.Buffer
	renderHumanScorecard(&buf, sc)
	got := buf.String()

	for _, dim := range []string{
		"MCP Token Efficiency N/A",
		"MCP Remote Transport N/A",
		"MCP Tool Design      N/A",
		"MCP Surface Strategy N/A",
		"Cache Freshness      N/A",
		"Auth Protocol           N/A",
		"Live API Verification   N/A",
	} {
		assert.Contains(t, got, dim, "expected %q in human output", dim)
	}

	assert.NotContains(t, got, "MCP Tool Design      0/10",
		"unscored dimensions must not render as 0/10")
	assert.NotContains(t, got, "Cache Freshness      0/10",
		"unscored cache_freshness must not render as 0/10")

	// Note line lists every unscored dimension so the user knows the
	// composite denominator is reduced.
	for _, dim := range sc.UnscoredDimensions {
		assert.Contains(t, got, dim,
			"unscored-dimensions note must list %q", dim)
	}
	assert.Contains(t, got, "omitted from denominator")
	infra, domain := pipeline.ScorecardTierDenominators(sc)
	assert.Contains(t, got, fmt.Sprintf("Denominator: infrastructure %d, domain %d", infra, domain))
	assert.NotContains(t, got, "Calibration:")
}

// TestRenderHumanScorecardShowsScoreForScoredDimensions — when a
// dimension IS scored (even at 0/10), the score renders as-is. A
// genuine 0/10 — e.g., cache_freshness on a CLI that has a local
// store but hasn't declared the freshness contract — is a real
// finding the user should see, not be masked.
func TestRenderHumanScorecardShowsScoreForScoredDimensions(t *testing.T) {
	t.Parallel()
	sc := &pipeline.Scorecard{
		APIName: "test-api",
		Steinberger: pipeline.SteinerScore{
			OutputModes:    10,
			LocalCache:     10,
			CacheFreshness: 0, // scored, but at 0 — a real finding
		},
		// CacheFreshness is NOT in UnscoredDimensions: the scorer
		// found a local store and assessed the freshness contract
		// (it's just absent).
		UnscoredDimensions: nil,
	}

	var buf bytes.Buffer
	renderHumanScorecard(&buf, sc)
	got := buf.String()

	assert.Contains(t, got, "Cache Freshness      0/10",
		"scored cache_freshness=0 must render as 0/10, not N/A")
	assert.NotContains(t, got, "Cache Freshness      N/A",
		"scored dimension at 0 is not the same as unscored")
	assert.False(t, strings.Contains(got, "omitted from denominator"),
		"with no unscored dimensions, the composite-note line must not appear")
	assert.NotContains(t, got, "Calibration:")
	infra, domain := pipeline.ScorecardTierDenominators(sc)
	assert.Contains(t, got, fmt.Sprintf("Denominator: infrastructure %d, domain %d", infra, domain))
}

func TestRenderHumanScorecardPrintsCalibrationWhenPresent(t *testing.T) {
	t.Parallel()
	sc := &pipeline.Scorecard{
		APIName:      "test-api",
		OverallGrade: "A",
		Steinberger: pipeline.SteinerScore{
			Total:           80,
			Percentage:      80,
			CalibrationNote: "Score raised from 50 to 80 based on verify pass rate",
		},
	}

	var buf bytes.Buffer
	renderHumanScorecard(&buf, sc)
	got := buf.String()

	assert.Contains(t, got, "Calibration: Score raised from 50 to 80 based on verify pass rate")
	infra, domain := pipeline.ScorecardTierDenominators(sc)
	assert.Contains(t, got, fmt.Sprintf("Denominator: infrastructure %d, domain %d", infra, domain))
}

func TestScorecardJSONKeepsLiveCheckSeparateFromLiveVerification(t *testing.T) {
	t.Parallel()
	sc := &pipeline.Scorecard{
		APIName: "test-api",
		Steinberger: pipeline.SteinerScore{
			Total:      80,
			Percentage: 80,
		},
		UnscoredDimensions: []string{pipeline.DimLiveAPIVerification},
	}
	live := &pipeline.LiveCheckResult{
		Passed:   1,
		PassRate: 1.0,
		BinaryRefresh: &pipeline.LiveCheckBinaryRefresh{
			Action: "rebuilt",
		},
		Features: []pipeline.LiveFeatureResult{{
			Name:         "sample",
			Status:       pipeline.StatusPass,
			OutputSample: "Found 3 brownie recipes",
		}},
	}

	payload := map[string]any{"scorecard": sc, "live_check": live}
	data, err := json.Marshal(payload)
	assert.NoError(t, err)
	got := string(data)

	assert.Contains(t, got, `"live_check"`)
	assert.Contains(t, got, `"binary_refresh"`)
	assert.Contains(t, got, `"action":"rebuilt"`)
	assert.Contains(t, got, `"output_sample":"Found 3 brownie recipes"`)
	assert.Contains(t, got, `"unscored_dimensions":["live_api_verification"]`)
	assert.Contains(t, got, `"live_api_verification":0`)
}

func TestRenderSampleOutputProbeSkippedIsNotAPass(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	renderSampleOutputProbe(&buf, &pipeline.LiveCheckResult{
		Failed:  2,
		Skipped: 1,
		Features: []pipeline.LiveFeatureResult{{
			Name:   "list",
			Status: pipeline.StatusFail,
			Reason: "exit 3: database disk image is malformed",
		}},
	})
	got := buf.String()
	assert.Contains(t, got, "Review: skipped")
	assert.Contains(t, got, "list: exit 3: database disk image is malformed")
	assert.NotContains(t, got, "Passed:")
	assert.NotContains(t, got, "pass rate")
	assert.NotContains(t, got, "Review: pass")
}

func TestRenderSampleOutputProbePassIsNotSkipped(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	renderSampleOutputProbe(&buf, &pipeline.LiveCheckResult{
		Passed:   2,
		PassRate: 1,
	})
	got := buf.String()
	assert.Contains(t, got, "Review: pass")
	assert.Contains(t, got, "Passed: 2/2")
	assert.Contains(t, got, "pass rate")
	assert.NotContains(t, got, "Review: skipped")
}

func TestRenderSampleOutputProbeFailIsNotAPass(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	renderSampleOutputProbe(&buf, &pipeline.LiveCheckResult{
		Passed:   1,
		Failed:   1,
		PassRate: 0.5,
		Features: []pipeline.LiveFeatureResult{{
			Name:   "list",
			Status: pipeline.StatusFail,
			Reason: "empty output",
		}},
	})
	got := buf.String()
	assert.Contains(t, got, "Review: fail")
	assert.Contains(t, got, "Passed: 1/2")
	assert.Contains(t, got, "empty output")
	assert.NotContains(t, got, "Review: pass\n")
	assert.NotContains(t, got, "Review: skipped")
}

func TestRenderSampleOutputProbeUnavailableIsSkipped(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	renderSampleOutputProbe(&buf, nil)
	assert.Empty(t, buf.String())

	renderSampleOutputProbe(&buf, &pipeline.LiveCheckResult{
		Unable: true,
		Reason: "no research.json",
		BinaryRefresh: &pipeline.LiveCheckBinaryRefresh{
			Action: "no_stage",
			Reason: "no staged binary found",
		},
	})
	got := buf.String()
	assert.Contains(t, got, "Binary refresh: no_stage")
	assert.Contains(t, got, "Status: unavailable")
	assert.Contains(t, got, "no research.json")
	assert.Contains(t, got, "Review: skipped")
	assert.NotContains(t, got, "Passed:")
	assert.NotContains(t, got, "Review: pass")
}

func TestLiveCheckFatalReasonReportsFailedBinaryRefresh(t *testing.T) {
	t.Parallel()

	live := &pipeline.LiveCheckResult{
		Unable: true,
		Reason: "rebuilding staged binary: go build failed",
		BinaryRefresh: &pipeline.LiveCheckBinaryRefresh{
			Action: "failed",
		},
	}

	assert.Contains(t, liveCheckFatalReason(live), "staged binary refresh failed")
	assert.Contains(t, liveCheckFatalReason(live), "go build failed")
	assert.Empty(t, liveCheckFatalReason(&pipeline.LiveCheckResult{Unable: true, Reason: "no research.json"}))
}

const scorecardFixtureVerifyManifest = `{
  "schema_version": 1,
  "api_name": "fixture",
  "cli_name": "fixture-pp-cli",
  "verify": {
    "mode": "mock",
    "pass_rate": 100,
    "passed": 10,
    "total": 10,
    "failed": 0,
    "data_pipeline": true,
    "verdict": "PASS"
  }
}
`

func TestScorecardWriteManifestMatchesBareScore(t *testing.T) {
	dir := writeScorecardCommandFixture(t, scorecardFixtureVerifyManifest)

	bare := runScorecardJSON(t, "--dir", dir, "--json")
	withWrite := runScorecardJSON(t, "--dir", dir, "--json", "--write-manifest", filepath.Join(dir, pipeline.CLIManifestFilename))

	assert.Equal(t, bare.Steinberger, withWrite.Steinberger)
	assert.Equal(t, bare.OverallGrade, withWrite.OverallGrade)
	assert.Equal(t, bare.UnscoredDimensions, withWrite.UnscoredDimensions)
	assert.Equal(t, bare.UnverifiedDimensions, withWrite.UnverifiedDimensions)
	assert.Equal(t, bare.Steinberger.Total, bare.Steinberger.Percentage)
	assert.Less(t, bare.Steinberger.README, 10, "fixture needs a dimension below max")
	assert.GreaterOrEqual(t, bare.Steinberger.Total, 80, "persisted verify evidence raises the shared total")
	assert.Contains(t, bare.Steinberger.CalibrationNote, "verify pass rate")

	bareHuman := runScorecardHuman(t, "--dir", dir)
	writeHuman := runScorecardHuman(t, "--dir", dir, "--write-manifest", filepath.Join(dir, pipeline.CLIManifestFilename))
	assert.Equal(t, denominatorLine(t, bareHuman), denominatorLine(t, writeHuman))
	assert.Contains(t, bareHuman, "Calibration:")
	assert.Contains(t, writeHuman, bare.Steinberger.CalibrationNote)
}

func TestScorecardWriteManifestStillPersistsScoreAndBuiltFeatures(t *testing.T) {
	dir := writeScorecardCommandFixture(t, scorecardFixtureVerifyManifest)
	manifestPath := filepath.Join(dir, pipeline.CLIManifestFilename)
	researchDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(researchDir, "research.json"), []byte(`{
  "api_name": "fixture",
  "novel_features_built": [{"name": "IRMAA", "command": "irmaa", "description": "Calculate premium brackets."}]
}
`), 0o644))

	reported := runScorecardJSON(t, "--dir", dir, "--json", "--research-dir", researchDir, "--write-manifest", manifestPath)

	data, err := os.ReadFile(manifestPath)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))

	verify := raw["verify"].(map[string]any)
	assert.Equal(t, "mock", verify["mode"])
	assert.Equal(t, float64(100), verify["pass_rate"])

	scorecard := raw["scorecard"].(map[string]any)
	steinberger := scorecard["steinberger"].(map[string]any)
	assert.Equal(t, float64(reported.Steinberger.Total), steinberger["total"])
	assert.Equal(t, float64(reported.Steinberger.Percentage), steinberger["percentage"])
	assert.Equal(t, reported.OverallGrade, steinberger["grade"])

	built := raw["novel_features_built"].([]any)
	require.Len(t, built, 1)
	assert.Equal(t, "irmaa", built[0].(map[string]any)["command"])

	again := runScorecardJSON(t, "--dir", dir, "--json")
	assert.Equal(t, reported.Steinberger.Total, again.Steinberger.Total)
	assert.Equal(t, reported.Steinberger.Percentage, again.Steinberger.Percentage)
	assert.Equal(t, reported.OverallGrade, again.OverallGrade)
}

func TestScorecardIgnoresVerifyEvidenceOutsideTheTree(t *testing.T) {
	dir := writeScorecardCommandFixture(t, `{
  "schema_version": 1,
  "api_name": "fixture",
  "cli_name": "fixture-pp-cli"
}
`)
	external := filepath.Join(t.TempDir(), "other.json")
	require.NoError(t, os.WriteFile(external, []byte(scorecardFixtureVerifyManifest), 0o644))

	bare := runScorecardJSON(t, "--dir", dir, "--json")
	withWrite := runScorecardJSON(t, "--dir", dir, "--json", "--write-manifest", external)

	assert.Equal(t, bare.Steinberger, withWrite.Steinberger)
	assert.Equal(t, bare.OverallGrade, withWrite.OverallGrade)
	assert.Less(t, bare.Steinberger.Total, 80, "verify evidence on the write path must not raise the total")
	assert.Empty(t, bare.Steinberger.CalibrationNote)
	assert.Less(t, bare.Steinberger.README, 10)

	data, err := os.ReadFile(external)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	scorecard := raw["scorecard"].(map[string]any)
	steinberger := scorecard["steinberger"].(map[string]any)
	assert.Equal(t, float64(bare.Steinberger.Total), steinberger["total"])
	assert.Equal(t, bare.OverallGrade, steinberger["grade"])
	assert.Equal(t, "mock", raw["verify"].(map[string]any)["mode"])
}

func TestScorecardMalformedTreeManifestIsAnError(t *testing.T) {
	dir := writeScorecardCommandFixture(t, "not-json")
	cmd := newScorecardCmd()
	cmd.SetArgs([]string{"--dir", dir, "--json"})
	_, err := runWithCapturedStdout(t, cmd.Execute)
	require.Error(t, err)
	var exitErr *ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, ExitGenerationError, exitErr.Code)
	assert.Contains(t, err.Error(), "loading verify evidence")
}

func TestScorecardWriteManifestRejectsUnreadableTarget(t *testing.T) {
	dir := writeScorecardCommandFixture(t, `{
  "schema_version": 1,
  "api_name": "fixture",
  "cli_name": "fixture-pp-cli"
}
`)
	target := filepath.Join(t.TempDir(), "broken.json")
	require.NoError(t, os.WriteFile(target, []byte("not-json"), 0o644))

	cmd := newScorecardCmd()
	cmd.SetArgs([]string{"--dir", dir, "--write-manifest", target})
	out, err := runWithCapturedStdout(t, cmd.Execute)
	require.Error(t, err)
	var exitErr *ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, ExitGenerationError, exitErr.Code)
	assert.Contains(t, err.Error(), "writing scorecard to manifest")
	assert.NotContains(t, out, "Total:")

	data, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "not-json", string(data))
}

func TestScorecardSpecChangesDenominatorExplicitly(t *testing.T) {
	dir := writeScorecardCommandFixture(t, "")
	specPath := filepath.Join(t.TempDir(), "spec.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(`
name: widgets
version: "1.0.0"
base_url: https://api.example.com
resources:
  widgets:
    endpoints:
      list:
        method: GET
        path: /widgets
`), 0o644))

	withoutSpec := runScorecardHuman(t, "--dir", dir)
	withSpec := runScorecardHuman(t, "--dir", dir, "--spec", specPath)

	withoutDomain := denominatorLine(t, withoutSpec)
	withDomain := denominatorLine(t, withSpec)
	assert.NotEqual(t, withoutDomain, withDomain, "supplying --spec must change the printed denominator")
	assert.Contains(t, withoutSpec, "path_validity")
	assert.Contains(t, withoutSpec, "unverified")
	assert.Contains(t, withoutSpec, "Path Validity           N/A")
	assert.NotContains(t, withSpec, "Path Validity           N/A")
	assert.Regexp(t, regexp.MustCompile(`Total: \d+/100 - Grade`), withoutSpec)
	assert.Regexp(t, regexp.MustCompile(`Total: \d+/100 - Grade`), withSpec)
}

func TestShipcheckScorecardLegMatchesStandaloneTotal(t *testing.T) {
	dir := writeScorecardCommandFixture(t, scorecardFixtureVerifyManifest)
	opts := &shipcheckOpts{dir: dir, noLiveCheck: true}
	var args []string
	for _, leg := range shipcheckLegs {
		if leg.name == "scorecard" {
			args = leg.args(opts)
			break
		}
	}
	require.NotEmpty(t, args)
	assert.Contains(t, args, "--write-manifest")
	assert.NotContains(t, args, "--live-check")

	bare := runScorecardJSON(t, "--dir", dir, "--json")
	fromLeg := runScorecardJSON(t, append(args[1:], "--json")...)
	assert.Equal(t, bare.Steinberger.Total, fromLeg.Steinberger.Total)
	assert.Equal(t, bare.Steinberger.Percentage, fromLeg.Steinberger.Percentage)
	assert.Equal(t, bare.OverallGrade, fromLeg.OverallGrade)
	assert.Less(t, bare.Steinberger.README, 10)
}

func writeScorecardCommandFixture(t *testing.T, manifest string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "cli"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "internal", "cli", "root.go"), []byte("package cli\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Fixture\n"), 0o644))
	if manifest != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, pipeline.CLIManifestFilename), []byte(manifest), 0o644))
	}
	return dir
}

func runScorecardJSON(t *testing.T, args ...string) pipeline.Scorecard {
	t.Helper()
	out := runScorecardHuman(t, args...)
	var payload struct {
		Scorecard pipeline.Scorecard `json:"scorecard"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &payload))
	return payload.Scorecard
}

func runScorecardHuman(t *testing.T, args ...string) string {
	t.Helper()
	cmd := newScorecardCmd()
	cmd.SetArgs(args)
	out, err := runWithCapturedStdout(t, cmd.Execute)
	require.NoError(t, err)
	return out
}

func denominatorLine(t *testing.T, out string) string {
	t.Helper()
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, "Denominator:") {
			return strings.TrimSpace(line)
		}
	}
	t.Fatalf("scorecard output missing denominator line:\n%s", out)
	return ""
}
