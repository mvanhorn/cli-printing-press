// Copyright mvanhorn. Licensed under Apache-2.0. See LICENSE.

package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
)

// compilePhase5MarkerSchema compiles the schema this binary publishes via
// `cli-printing-press schema phase5-marker`.
func compilePhase5MarkerSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(phase5MarkerSchemaJSON))
	require.NoError(t, err)
	c := jsonschema.NewCompiler()
	require.NoError(t, c.AddResource("phase5-acceptance.schema.json", doc))
	sch, err := c.Compile("phase5-acceptance.schema.json")
	require.NoError(t, err)
	return sch
}

func validateMarker(t *testing.T, sch *jsonschema.Schema, marker pipeline.Phase5GateMarker) error {
	t.Helper()
	raw, err := json.Marshal(marker)
	require.NoError(t, err)
	var v any
	require.NoError(t, json.Unmarshal(raw, &v))
	return sch.Validate(v)
}

// TestPhase5Marker_WriterOutputMatchesPublishedSchema is the contract test the
// pair was missing: what the runner writes must validate against what
// `schema phase5-marker` publishes.
//
// They had drifted in both directions. The struct emitted coverage_hollow,
// hollow_features, tests_unverified and skip_reason, none of which the schema
// declared — fatal under additionalProperties:false. The schema declared
// cli_name, tests_total, completed_at and summary, which the struct never
// emits, and marked api_name/run_id required even though both carry omitempty
// and the gate (phase5_gate.go) treats them as optional.
func TestPhase5Marker_WriterOutputMatchesPublishedSchema(t *testing.T) {
	sch := compilePhase5MarkerSchema(t)

	t.Run("full pass marker with hollow coverage", func(t *testing.T) {
		marker := pipeline.Phase5GateMarker{
			SchemaVersion:     1,
			APIName:           "probe",
			RunID:             "20260806-000000-abcdef",
			Status:            "pass",
			Level:             "full",
			MatrixSize:        220,
			TestsPassed:       220,
			TestsSkipped:      258,
			TestsUnverified:   258,
			CoverageHollow:    true,
			HollowFeatures:    []string{"approvals answer"},
			SourceFingerprint: "deadbeef",
			SourceFiles:       map[string]string{"go.mod": "cafe"},
			AuthContext: pipeline.Phase5AuthContext{
				Type:            "none",
				APIKeyAvailable: true,
			},
		}
		require.NoError(t, validateMarker(t, sch, marker))
	})

	// A standalone `dogfood --dir X` outside a pipeline run has no manifest and
	// no run state, so identity is genuinely unknown. omitempty drops both
	// fields; the schema must not demand them.
	t.Run("identity unresolvable", func(t *testing.T) {
		marker := pipeline.Phase5GateMarker{
			SchemaVersion:     1,
			Status:            "pass",
			Level:             "quick",
			MatrixSize:        6,
			TestsPassed:       6,
			SourceFingerprint: "deadbeef",
		}
		require.NoError(t, validateMarker(t, sch, marker))
	})

	t.Run("fail marker carries failure summary", func(t *testing.T) {
		marker := pipeline.Phase5GateMarker{
			SchemaVersion:     1,
			APIName:           "probe",
			Status:            "fail",
			Level:             "full",
			MatrixSize:        220,
			TestsPassed:       218,
			TestsFailed:       2,
			SourceFingerprint: "deadbeef",
			FailureSummary: &pipeline.Phase5FailureSummary{
				HTTP4xx:  2,
				Commands: []string{"servicedeskapi get-articles"},
			},
		}
		require.NoError(t, validateMarker(t, sch, marker))
	})
}

// TestPhase5Marker_SchemaDeclaresNoPhantomProperties guards the other direction:
// a property the schema declares but the writer can never emit is a promise to
// consumers that nothing keeps.
func TestPhase5Marker_SchemaDeclaresNoPhantomProperties(t *testing.T) {
	var schema struct {
		Properties map[string]any `json:"properties"`
	}
	require.NoError(t, json.Unmarshal([]byte(phase5MarkerSchemaJSON), &schema))

	emittable := map[string]bool{}
	raw, err := json.Marshal(pipeline.Phase5GateMarker{
		SchemaVersion:     1,
		APIName:           "x",
		RunID:             "x",
		Status:            "pass",
		Level:             "full",
		MatrixSize:        1,
		TestsPassed:       1,
		TestsSkipped:      1,
		TestsUnverified:   1,
		TestsFailed:       1,
		CoverageHollow:    true,
		HollowFeatures:    []string{"x"},
		SkipReason:        "x",
		SourceFingerprint: "x",
		SourceFiles:       map[string]string{"a": "b"},
		AuthContext:       pipeline.Phase5AuthContext{Type: "none"},
		FailureSummary:    &pipeline.Phase5FailureSummary{HTTP4xx: 1},
	})
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	for k := range m {
		emittable[k] = true
	}

	for prop := range schema.Properties {
		require.True(t, emittable[prop],
			"schema declares %q but Phase5GateMarker can never emit it", prop)
	}
}
