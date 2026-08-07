package reachability

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderJSON(t *testing.T) {
	r := &Result{
		URL:        "https://food52.com",
		Mode:       ModeBrowserHTTP,
		Confidence: 0.85,
		BodyEvidence: []string{
			"vercel_security_checkpoint",
		},
		Probes: []ProbeResult{
			{Transport: TransportStdlib, Status: 429, ElapsedMS: 412, Evidence: []string{"x-vercel-mitigated: challenge"}, BodyEvidence: []string{"vercel_security_checkpoint"}},
			{Transport: TransportSurfChrome, Status: 200, ElapsedMS: 387},
		},
		Recommendation: Recommendation{
			Runtime:   ModeBrowserHTTP,
			Rationale: "Surf cleared",
		},
	}
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(&buf, r))

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &decoded))
	assert.Equal(t, "browser_http", decoded["mode"])
	assert.Equal(t, "https://food52.com", decoded["url"])
	assert.Equal(t, []any{"vercel_security_checkpoint"}, decoded["body_evidence"])
	probes, ok := decoded["probes"].([]any)
	require.True(t, ok)
	assert.Len(t, probes, 2)
	firstProbe := probes[0].(map[string]any)
	assert.Equal(t, []any{"vercel_security_checkpoint"}, firstProbe["body_evidence"])
}

func TestRenderTable_FullPath(t *testing.T) {
	r := &Result{
		URL:        "https://food52.com",
		Mode:       ModeBrowserHTTP,
		Confidence: 0.85,
		Probes: []ProbeResult{
			{Transport: TransportStdlib, Status: 429, ElapsedMS: 412, Evidence: []string{"x-vercel-mitigated: challenge"}},
			{Transport: TransportSurfChrome, Status: 200, ElapsedMS: 387},
		},
		Recommendation: Recommendation{Runtime: ModeBrowserHTTP, Rationale: "Surf cleared"},
	}
	var buf bytes.Buffer
	require.NoError(t, RenderTable(&buf, r))
	out := buf.String()
	assert.Contains(t, out, "Transport")
	assert.Contains(t, out, "stdlib")
	assert.Contains(t, out, "surf-chrome")
	assert.Contains(t, out, "browser_http")
	assert.Contains(t, out, "Surf cleared")
	assert.NotContains(t, out, "partial")
}

func TestRenderTable_PartialNotice(t *testing.T) {
	r := &Result{
		URL:     "https://example.com",
		Mode:    ModeStandardHTTP,
		Partial: true,
		Probes: []ProbeResult{
			{Transport: TransportStdlib, Status: 200, ElapsedMS: 100},
		},
		Recommendation: Recommendation{Runtime: ModeStandardHTTP},
	}
	var buf bytes.Buffer
	require.NoError(t, RenderTable(&buf, r))
	out := buf.String()
	assert.True(t, strings.Contains(out, "partial"), "table must surface partial flag for debug clarity")
}

func TestRenderTable_TransportError(t *testing.T) {
	r := &Result{
		URL:  "https://nope.invalid",
		Mode: ModeUnknown,
		Probes: []ProbeResult{
			{Transport: TransportStdlib, Error: "DNS lookup failed", ElapsedMS: 50},
		},
		Recommendation: Recommendation{Runtime: ModeUnknown},
	}
	var buf bytes.Buffer
	require.NoError(t, RenderTable(&buf, r))
	out := buf.String()
	assert.Contains(t, out, "DNS lookup failed")
}
