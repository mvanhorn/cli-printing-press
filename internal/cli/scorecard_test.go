package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/stretchr/testify/assert"
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
	assert.Contains(t, got, `"output_sample":"Found 3 brownie recipes"`)
	assert.Contains(t, got, `"unscored_dimensions":["live_api_verification"]`)
	assert.Contains(t, got, `"live_api_verification":0`)
}
