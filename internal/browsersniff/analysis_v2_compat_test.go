package browsersniff

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProtectionObservation_NotesV2Compat covers the food52-reprint repro
// from issue #474: prior traffic-analysis files emit `notes: "..."` (a single
// string) where v3 expects `notes: ["..."]` (a slice).
func TestProtectionObservation_NotesV2Compat(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		json string
		want []string
	}{
		{
			name: "v2 single string",
			json: `{"label":"vercel_bot_mitigation","confidence":1.0,"notes":"x-vercel-mitigated: challenge"}`,
			want: []string{"x-vercel-mitigated: challenge"},
		},
		{
			name: "v3 slice",
			json: `{"label":"vercel_bot_mitigation","confidence":1.0,"notes":["a","b"]}`,
			want: []string{"a", "b"},
		},
		{
			name: "missing notes",
			json: `{"label":"x","confidence":0.5}`,
			want: nil,
		},
		{
			name: "empty string notes — treated as no notes",
			json: `{"label":"x","confidence":0.5,"notes":""}`,
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p ProtectionObservation
			require.NoError(t, json.Unmarshal([]byte(tc.json), &p))
			assert.Equal(t, tc.want, p.Notes)

			// Round-trip MarshalJSON → UnmarshalJSON: should remain the v3 shape.
			rt, err := json.Marshal(&p)
			require.NoError(t, err)
			var p2 ProtectionObservation
			require.NoError(t, json.Unmarshal(rt, &p2))
			assert.Equal(t, p.Notes, p2.Notes)
		})
	}
}

// TestRequestSequence_NotesV2Compat mirrors the protection-observation case
// for the second Notes []string site.
func TestRequestSequence_NotesV2Compat(t *testing.T) {
	t.Parallel()

	var rs RequestSequence
	require.NoError(t, json.Unmarshal([]byte(`{"label":"x","confidence":1.0,"notes":"single legacy note"}`), &rs))
	assert.Equal(t, []string{"single legacy note"}, rs.Notes)

	var rs2 RequestSequence
	require.NoError(t, json.Unmarshal([]byte(`{"label":"x","confidence":1.0,"notes":["a","b","c"]}`), &rs2))
	assert.Equal(t, []string{"a", "b", "c"}, rs2.Notes)
}

// TestAnalysisWarning_StringV2Compat covers the v2-shape `warnings: [<str>]`
// where v3 expects `warnings: [{type, message, confidence, evidence}]`.
func TestAnalysisWarning_StringV2Compat(t *testing.T) {
	t.Parallel()

	t.Run("v2 string materializes to scope_note", func(t *testing.T) {
		var w AnalysisWarning
		require.NoError(t, json.Unmarshal([]byte(`"Hotline /hotline returns only siteSettings"`), &w))
		assert.Equal(t, "scope_note", w.Type)
		assert.Equal(t, "Hotline /hotline returns only siteSettings", w.Message)
		assert.Equal(t, 1.0, w.Confidence)
		require.Len(t, w.Evidence, 1)
		assert.Equal(t, EvidenceRefStringSentinel, w.Evidence[0].EntryIndex)
		assert.Equal(t, "Hotline /hotline returns only siteSettings", w.Evidence[0].Reason)
	})

	t.Run("v3 object preserved as-is", func(t *testing.T) {
		var w AnalysisWarning
		require.NoError(t, json.Unmarshal([]byte(`{"type":"weak_schema_evidence","message":"only one fixture","confidence":0.6}`), &w))
		assert.Equal(t, "weak_schema_evidence", w.Type)
		assert.Equal(t, "only one fixture", w.Message)
		assert.Equal(t, 0.6, w.Confidence)
		assert.Empty(t, w.Evidence)
	})

	t.Run("slice of mixed shapes round-trips", func(t *testing.T) {
		input := `[{"type":"a","message":"obj","confidence":0.5},"legacy string"]`
		var ws []AnalysisWarning
		require.NoError(t, json.Unmarshal([]byte(input), &ws))
		require.Len(t, ws, 2)
		assert.Equal(t, "a", ws[0].Type)
		assert.Equal(t, "scope_note", ws[1].Type)
		assert.Equal(t, "legacy string", ws[1].Message)
	})
}

// TestAuthAnalysis_CandidateTypesV2Compat covers the v2-shape
// `auth.candidate_types: ["api_key", "none"]` materializing into v3's
// `auth.candidates: [{type:..., confidence:1.0}]`.
func TestAuthAnalysis_CandidateTypesV2Compat(t *testing.T) {
	t.Parallel()

	t.Run("v2 candidate_types materializes to candidates", func(t *testing.T) {
		var a AuthAnalysis
		require.NoError(t, json.Unmarshal([]byte(`{"candidate_types":["none"]}`), &a))
		require.Len(t, a.Candidates, 1)
		assert.Equal(t, "none", a.Candidates[0].Type)
		assert.Equal(t, 1.0, a.Candidates[0].Confidence)
	})

	t.Run("v3 candidates wins when both shapes present", func(t *testing.T) {
		input := `{"candidates":[{"type":"api_key","confidence":0.9}],"candidate_types":["fallback_should_not_show"]}`
		var a AuthAnalysis
		require.NoError(t, json.Unmarshal([]byte(input), &a))
		require.Len(t, a.Candidates, 1)
		assert.Equal(t, "api_key", a.Candidates[0].Type)
		assert.Equal(t, 0.9, a.Candidates[0].Confidence)
	})

	t.Run("missing both fields is empty", func(t *testing.T) {
		var a AuthAnalysis
		require.NoError(t, json.Unmarshal([]byte(`{}`), &a))
		assert.Empty(t, a.Candidates)
	})
}

// TestTrafficAnalysis_GenerationHintsMapCompat covers `generation_hints` shape
// drift: v2 emitted an object map (key→bool); v3 emits a flat sorted slice
// derived from a fixed vocabulary (deriveGenerationHints).
func TestTrafficAnalysis_GenerationHintsMapCompat(t *testing.T) {
	t.Parallel()

	t.Run("v2 map flattens to sorted slice of true keys", func(t *testing.T) {
		input := `{"version":"1","summary":{},"protocols":[],"auth":{},"endpoint_clusters":[],"generation_hints":{"requires_browser_compatible_http":true,"browser_http_transport":true,"client_pattern":false}}`
		var ta TrafficAnalysis
		require.NoError(t, json.Unmarshal([]byte(input), &ta))
		assert.Equal(t, []string{"browser_http_transport", "requires_browser_compatible_http"}, ta.GenerationHints)
	})

	t.Run("v3 slice preserved as-is", func(t *testing.T) {
		input := `{"version":"1","summary":{},"protocols":[],"auth":{},"endpoint_clusters":[],"generation_hints":["browser_http_transport"]}`
		var ta TrafficAnalysis
		require.NoError(t, json.Unmarshal([]byte(input), &ta))
		assert.Equal(t, []string{"browser_http_transport"}, ta.GenerationHints)
	})
}

// TestTrafficAnalysis_VersionNormalization confirms that v2's "1.0" loads
// alongside v3's "1" without forcing the consumer to relax the version check.
func TestTrafficAnalysis_VersionNormalization(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"1":   "1",
		"1.0": "1", // v2 binaries emitted "1.0"; normalize on load
	}
	for in, want := range cases {
		t.Run("version "+in, func(t *testing.T) {
			input := `{"version":"` + in + `","summary":{},"protocols":[],"auth":{},"endpoint_clusters":[]}`
			var ta TrafficAnalysis
			require.NoError(t, json.Unmarshal([]byte(input), &ta))
			assert.Equal(t, want, ta.Version)
		})
	}
}

// TestReadTrafficAnalysis_V2ShapeFile is the end-to-end test: write a fully
// v2-shape file to disk, ReadTrafficAnalysis loads it without error and
// presents the v3 in-memory shape. This is the failure mode that made the
// food52 reprint patch its prior traffic-analysis.json by hand five times.
func TestReadTrafficAnalysis_V2ShapeFile(t *testing.T) {
	t.Parallel()

	v2Body := `{
		"version": "1.0",
		"summary": {"target_url": "https://example.com"},
		"protocols": [
			{"label": "ssr_embedded_data", "confidence": 1.0, "notes": "legacy notes string on protocol — should be ignored"}
		],
		"auth": {
			"candidate_types": ["none"]
		},
		"protections": [
			{"label": "vercel_bot_mitigation", "confidence": 1.0, "notes": "x-vercel-mitigated: challenge"}
		],
		"endpoint_clusters": [],
		"generation_hints": {
			"browser_http_transport": true,
			"requires_runtime_key_discovery": true,
			"deprecated_hint": false
		},
		"warnings": [
			"Hotline returns only siteSettings",
			"Shop deferred to v2"
		]
	}`

	dir := t.TempDir()
	fp := filepath.Join(dir, "traffic-analysis.json")
	require.NoError(t, os.WriteFile(fp, []byte(v2Body), 0o600))

	analysis, err := ReadTrafficAnalysis(fp)
	require.NoError(t, err)
	require.NotNil(t, analysis)

	assert.Equal(t, "1", analysis.Version, "v2 1.0 should normalize to 1")
	require.Len(t, analysis.Protections, 1)
	assert.Equal(t, []string{"x-vercel-mitigated: challenge"}, analysis.Protections[0].Notes)
	require.Len(t, analysis.Auth.Candidates, 1)
	assert.Equal(t, "none", analysis.Auth.Candidates[0].Type)
	assert.Equal(t, []string{"browser_http_transport", "requires_runtime_key_discovery"}, analysis.GenerationHints)
	require.Len(t, analysis.Warnings, 2)
	for _, w := range analysis.Warnings {
		assert.Equal(t, "scope_note", w.Type)
		assert.NotEmpty(t, w.Message)
	}
}

// TestReadTrafficAnalysis_VersionRejection confirms the version gate still
// rejects unknown versions — the compat layer normalizes "1.0" but doesn't
// silently accept "2" or "9".
func TestReadTrafficAnalysis_VersionRejection(t *testing.T) {
	t.Parallel()

	cases := []string{"2", "9", "0.5", "draft"}
	for _, v := range cases {
		t.Run("version "+v, func(t *testing.T) {
			body := `{"version":"` + v + `","summary":{},"protocols":[],"auth":{},"endpoint_clusters":[]}`
			dir := t.TempDir()
			fp := filepath.Join(dir, "ta.json")
			require.NoError(t, os.WriteFile(fp, []byte(body), 0o600))

			_, err := ReadTrafficAnalysis(fp)
			require.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), "unsupported traffic analysis version"),
				"want version-rejection error, got: %v", err)
		})
	}
}
