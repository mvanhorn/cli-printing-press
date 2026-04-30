package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v3/internal/pipeline"
)

func TestRequiresPreDecisionFields(t *testing.T) {
	tests := []struct {
		kind string
		want bool
	}{
		{"thin-mcp-description", true},
		{"empty-mcp-description", true},
		{"thin-short", false},
		{"empty-short", false},
		{"missing-read-only", false},
		{"unknown", false},
		{"", false},
	}
	for _, tc := range tests {
		if got := requiresPreDecisionFields(tc.kind); got != tc.want {
			t.Errorf("requiresPreDecisionFields(%q) = %v, want %v", tc.kind, got, tc.want)
		}
	}
}

func TestMissingPreDecisionFields(t *testing.T) {
	full := ToolsAuditFinding{
		SpecSourceMaterial: "summary + 3 params",
		TargetDescription:  "Create a tag with name + color",
		GapAnalysis:        "generator can compose this from spec",
	}
	tests := []struct {
		name string
		f    ToolsAuditFinding
		want bool
	}{
		{"all populated", full, false},
		{"missing spec", ToolsAuditFinding{TargetDescription: "x", GapAnalysis: "y"}, true},
		{"missing target", ToolsAuditFinding{SpecSourceMaterial: "x", GapAnalysis: "y"}, true},
		{"missing gap", ToolsAuditFinding{SpecSourceMaterial: "x", TargetDescription: "y"}, true},
		{"all empty", ToolsAuditFinding{}, true},
		{"whitespace only", ToolsAuditFinding{SpecSourceMaterial: "  ", TargetDescription: "\t", GapAnalysis: "\n"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := missingPreDecisionFields(tc.f); got != tc.want {
				t.Errorf("missingPreDecisionFields() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNormalizeRationale(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Systemic to OpenAPI specs", "systemic to openapi specs"},
		{"systemic  to  openapi   specs", "systemic to openapi specs"},
		{"\tSystemic\nto\topenapi specs ", "systemic to openapi specs"},
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range tests {
		if got := normalizeRationale(tc.in); got != tc.want {
			t.Errorf("normalizeRationale(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDetectDuplicateRationales(t *testing.T) {
	mk := func(cmd, status, note string) ToolsAuditFinding {
		return ToolsAuditFinding{
			Kind: "thin-mcp-description", Command: cmd, File: "tools-manifest.json",
			Status: status, Note: note,
		}
	}

	t.Run("no accepts returns nil", func(t *testing.T) {
		findings := []ToolsAuditFinding{
			mk("a", "", ""), mk("b", "", ""),
		}
		if got := detectDuplicateRationales(findings, 5); got != nil {
			t.Errorf("got %v, want nil", got)
		}
	})

	t.Run("under threshold returns nil", func(t *testing.T) {
		var findings []ToolsAuditFinding
		for i := range 5 {
			findings = append(findings, mk("c"+string(rune('0'+i)), statusAccepted, "same rationale"))
		}
		if got := detectDuplicateRationales(findings, 5); got != nil {
			t.Errorf("got %v, want nil — exactly threshold should not fire", got)
		}
	})

	t.Run("over threshold surfaces group", func(t *testing.T) {
		var findings []ToolsAuditFinding
		for i := range 6 {
			findings = append(findings, mk("c"+string(rune('0'+i)), statusAccepted, "Systemic to OpenAPI specs"))
		}
		groups := detectDuplicateRationales(findings, 5)
		if len(groups) != 1 {
			t.Fatalf("got %d groups, want 1", len(groups))
		}
		if groups[0].Rationale != "systemic to openapi specs" {
			t.Errorf("got rationale %q, want normalized form", groups[0].Rationale)
		}
		if len(groups[0].Findings) != 6 {
			t.Errorf("got %d findings in group, want 6", len(groups[0].Findings))
		}
	})

	t.Run("normalization clusters case and whitespace variants", func(t *testing.T) {
		findings := []ToolsAuditFinding{
			mk("a", statusAccepted, "Systemic to OpenAPI specs"),
			mk("b", statusAccepted, "systemic  to  openapi specs"),
			mk("c", statusAccepted, "SYSTEMIC TO OPENAPI SPECS"),
			mk("d", statusAccepted, "  Systemic to OpenAPI specs  "),
			mk("e", statusAccepted, "systemic to openapi specs"),
			mk("f", statusAccepted, "Systemic\tto\tOpenAPI\tspecs"),
		}
		groups := detectDuplicateRationales(findings, 5)
		if len(groups) != 1 || len(groups[0].Findings) != 6 {
			t.Errorf("expected 1 cluster of 6, got %+v", groups)
		}
	})

	t.Run("empty notes are ignored", func(t *testing.T) {
		var findings []ToolsAuditFinding
		for i := range 10 {
			findings = append(findings, mk("c"+string(rune('0'+i)), statusAccepted, ""))
		}
		if got := detectDuplicateRationales(findings, 5); got != nil {
			t.Errorf("got %v, want nil — empty notes should not cluster", got)
		}
	})

	t.Run("multiple groups sorted by size descending", func(t *testing.T) {
		var findings []ToolsAuditFinding
		for i := range 6 {
			findings = append(findings, mk("a"+string(rune('0'+i)), statusAccepted, "rationale one"))
		}
		for i := range 8 {
			findings = append(findings, mk("b"+string(rune('0'+i)), statusAccepted, "rationale two"))
		}
		groups := detectDuplicateRationales(findings, 5)
		if len(groups) != 2 {
			t.Fatalf("got %d groups, want 2", len(groups))
		}
		if len(groups[0].Findings) != 8 {
			t.Errorf("first group should be larger; got %d", len(groups[0].Findings))
		}
	})
}

func TestNextPendingFinding(t *testing.T) {
	mk := func(cmd, kind, status string, hasFields bool) ToolsAuditFinding {
		f := ToolsAuditFinding{
			Kind: kind, Command: cmd, File: "tools-manifest.json",
			Line: 0, Evidence: "evidence-" + cmd, Status: status,
		}
		if hasFields {
			f.SpecSourceMaterial = "x"
			f.TargetDescription = "y"
			f.GapAnalysis = "z"
		}
		return f
	}

	t.Run("returns first pending in scan order", func(t *testing.T) {
		findings := []ToolsAuditFinding{
			mk("a", "thin-mcp-description", statusAccepted, true),
			mk("b", "thin-mcp-description", "", false),
			mk("c", "thin-mcp-description", "", false),
		}
		got := nextPendingFinding(findings, nil)
		if got == nil || got.Command != "b" {
			t.Errorf("got %+v, want command b", got)
		}
	})

	t.Run("returns nil when fully accepted with fields", func(t *testing.T) {
		findings := []ToolsAuditFinding{
			mk("a", "thin-mcp-description", statusAccepted, true),
			mk("b", "thin-mcp-description", statusAccepted, true),
		}
		if got := nextPendingFinding(findings, nil); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})

	t.Run("treats accepted-without-fields as pending", func(t *testing.T) {
		findings := []ToolsAuditFinding{
			mk("a", "thin-mcp-description", statusAccepted, true),
			mk("b", "thin-mcp-description", statusAccepted, false), // accepted without fields
			mk("c", "thin-mcp-description", "", false),
		}
		got := nextPendingFinding(findings, nil)
		if got == nil || got.Command != "b" {
			t.Errorf("got %+v, want command b (accepted without fields counts as pending)", got)
		}
	})

	t.Run("non-mcp accepted without fields is not pending", func(t *testing.T) {
		findings := []ToolsAuditFinding{
			mk("a", "thin-short", statusAccepted, false), // accepted without fields, not gated
			mk("b", "thin-short", "", false),
		}
		got := nextPendingFinding(findings, nil)
		if got == nil || got.Command != "b" {
			t.Errorf("got %+v, want command b (a is not gated since thin-short)", got)
		}
	})

	t.Run("progress checkpoint resumes after last processed", func(t *testing.T) {
		findings := []ToolsAuditFinding{
			mk("a", "thin-mcp-description", "", false),
			mk("b", "thin-mcp-description", statusAccepted, true),
			mk("c", "thin-mcp-description", "", false),
		}
		previous := &ToolsAuditLedger{
			Progress: &PolishProgress{LastProcessedFindingID: findingKey(findings[1])},
		}
		got := nextPendingFinding(findings, previous)
		if got == nil || got.Command != "c" {
			t.Errorf("got %+v, want command c (skipped a because checkpoint was at b)", got)
		}
	})

	t.Run("progress past everything falls back to head scan", func(t *testing.T) {
		findings := []ToolsAuditFinding{
			mk("a", "thin-mcp-description", "", false),
			mk("b", "thin-mcp-description", statusAccepted, true),
		}
		previous := &ToolsAuditLedger{
			Progress: &PolishProgress{LastProcessedFindingID: findingKey(findings[1])},
		}
		got := nextPendingFinding(findings, previous)
		if got == nil || got.Command != "a" {
			t.Errorf("got %+v, want command a (fallback to head when checkpoint exhausted forward path)", got)
		}
	})

	t.Run("missing checkpoint key falls back to head scan", func(t *testing.T) {
		findings := []ToolsAuditFinding{
			mk("a", "thin-mcp-description", "", false),
		}
		previous := &ToolsAuditLedger{
			Progress: &PolishProgress{LastProcessedFindingID: "nonexistent:0:thin-mcp-description:gone:gone"},
		}
		got := nextPendingFinding(findings, previous)
		if got == nil || got.Command != "a" {
			t.Errorf("got %+v, want command a (stale checkpoint shouldn't skip pending)", got)
		}
	})
}

func TestCheckScorecardDelta(t *testing.T) {
	mkLedger := func(before int, hasSnap bool) *ToolsAuditLedger {
		l := &ToolsAuditLedger{}
		if hasSnap {
			l.ScorecardBefore = &ScorecardSnapshot{
				MCPDescriptionQuality: before,
				Captured:              time.Now(),
			}
		}
		return l
	}
	mkFinding := func(kind, status string) ToolsAuditFinding {
		return ToolsAuditFinding{Kind: kind, Status: status, File: "tools-manifest.json"}
	}

	// One thin + one rich = 50% thin = score 0 per the curve.
	thinManifest := &pipeline.ToolsManifest{Tools: []pipeline.ManifestTool{
		{Name: "a", Description: "Create x"},
		{Name: "b", Description: "this description is long enough to be over the threshold for the test"},
	}}
	// All-rich manifest scores 10/10.
	richManifest := &pipeline.ToolsManifest{Tools: []pipeline.ManifestTool{
		{Name: "a", Description: "this is a sufficiently long and rich description for the test that exceeds threshold"},
	}}

	t.Run("nil previous returns nil", func(t *testing.T) {
		got := checkScorecardDelta(thinManifest, []ToolsAuditFinding{mkFinding("thin-mcp-description", statusAccepted)}, nil)
		if got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})

	t.Run("previous without snapshot returns nil", func(t *testing.T) {
		got := checkScorecardDelta(thinManifest, []ToolsAuditFinding{mkFinding("thin-mcp-description", statusAccepted)}, mkLedger(0, false))
		if got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})

	t.Run("no thin-mcp accepts returns nil", func(t *testing.T) {
		got := checkScorecardDelta(thinManifest, []ToolsAuditFinding{mkFinding("thin-short", statusAccepted)}, mkLedger(3, true))
		if got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})

	t.Run("score lifted returns nil", func(t *testing.T) {
		got := checkScorecardDelta(richManifest, []ToolsAuditFinding{mkFinding("thin-mcp-description", statusAccepted)}, mkLedger(0, true))
		if got != nil {
			t.Errorf("got %+v, want nil (score lifted)", got)
		}
	})

	t.Run("no lift with thin accepts fires", func(t *testing.T) {
		before := mkLedger(0, true)
		got := checkScorecardDelta(thinManifest, []ToolsAuditFinding{
			mkFinding("thin-mcp-description", statusAccepted),
			mkFinding("thin-mcp-description", statusAccepted),
		}, before)
		if got == nil {
			t.Fatal("got nil, want issue")
		}
		if got.AcceptedThinMCP != 2 {
			t.Errorf("AcceptedThinMCP = %d, want 2", got.AcceptedThinMCP)
		}
		if got.Before != 0 || got.After != 0 {
			t.Errorf("got before=%d after=%d, want 0/0", got.Before, got.After)
		}
	})

	t.Run("nil manifest returns nil", func(t *testing.T) {
		got := checkScorecardDelta(nil, []ToolsAuditFinding{mkFinding("thin-mcp-description", statusAccepted)}, mkLedger(0, true))
		if got != nil {
			t.Errorf("got %+v, want nil (nil manifest, scorer unscored)", got)
		}
	})
}

func TestRenderCompletionGatesIncludesAllReasons(t *testing.T) {
	c := completionStatus{
		IncompleteAccepts: []ToolsAuditFinding{
			{Kind: "thin-mcp-description", Command: "tags_create", File: "tools-manifest.json", Line: 0},
		},
		DuplicateRationaleGroups: []rationaleGroup{
			{Rationale: "systemic to openapi specs", Findings: []ToolsAuditFinding{
				{Kind: "thin-mcp-description", Command: "a", File: "tools-manifest.json"},
				{Kind: "thin-mcp-description", Command: "b", File: "tools-manifest.json"},
			}},
		},
		ScorecardDeltaIssue: &scorecardDeltaIssue{Before: 3, After: 3, AcceptedThinMCP: 5},
	}
	var buf bytes.Buffer
	renderCompletionGates(&buf, c)
	out := buf.String()
	for _, want := range []string{
		"incomplete: the run is not done yet",
		"missing pre-decision fields",
		"tags_create",
		"share rationale",
		"systemic to openapi specs",
		"MCPDescriptionQuality unchanged",
		"3/10 → 3/10",
		"5 thin-mcp-description finding(s) accepted",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- output ---\n%s", want, out)
		}
	}
}

func TestRenderCompletionGatesSilentWhenClean(t *testing.T) {
	var buf bytes.Buffer
	renderCompletionGates(&buf, completionStatus{})
	if buf.Len() != 0 {
		t.Errorf("expected silent output, got %q", buf.String())
	}
}

func TestEvaluateCompletionAggregatesGates(t *testing.T) {
	// Manifest with one thin description -> score = 0 (50% thin -> default tier).
	manifest := &pipeline.ToolsManifest{Tools: []pipeline.ManifestTool{
		{Name: "a", Description: "Create x"},
		{Name: "b", Description: "this is a sufficiently long description for the test bench"},
	}}
	previous := &ToolsAuditLedger{
		ScorecardBefore: &ScorecardSnapshot{MCPDescriptionQuality: 0, Captured: time.Now()},
	}
	// Build findings: one accepted thin-mcp without pre-decision fields
	// (fires gate 1), six thin-mcp accepts sharing one rationale (fires
	// gate 2), and the same thin-mcp accepts naturally fire gate 3
	// (no score lift while accepting thin-mcp).
	var findings []ToolsAuditFinding
	for i := range 7 {
		f := ToolsAuditFinding{
			Kind: "thin-mcp-description", Command: "c" + string(rune('0'+i)),
			File: "tools-manifest.json", Line: 0, Evidence: "thin",
			Status: statusAccepted, Note: "Systemic to OpenAPI specs",
		}
		if i > 0 {
			// Fill fields on all but the first to isolate gate 1
			f.SpecSourceMaterial = "summary only"
			f.TargetDescription = "a richer description"
			f.GapAnalysis = "generator could compose more from spec"
		}
		findings = append(findings, f)
	}

	c := evaluateCompletion(manifest, findings, previous)

	if len(c.IncompleteAccepts) != 1 {
		t.Errorf("IncompleteAccepts = %d, want 1", len(c.IncompleteAccepts))
	}
	if len(c.DuplicateRationaleGroups) != 1 {
		t.Errorf("DuplicateRationaleGroups = %d, want 1", len(c.DuplicateRationaleGroups))
	} else if len(c.DuplicateRationaleGroups[0].Findings) != 7 {
		t.Errorf("group size = %d, want 7", len(c.DuplicateRationaleGroups[0].Findings))
	}
	if c.ScorecardDeltaIssue == nil {
		t.Error("ScorecardDeltaIssue = nil, want issue (no lift + accepts)")
	} else if c.ScorecardDeltaIssue.AcceptedThinMCP != 7 {
		t.Errorf("AcceptedThinMCP = %d, want 7", c.ScorecardDeltaIssue.AcceptedThinMCP)
	}
	// NextPending should point at the one with missing fields (c0).
	if c.NextPending == nil || c.NextPending.Command != "c0" {
		t.Errorf("NextPending = %+v, want c0", c.NextPending)
	}

	if !c.hasGateFailure() {
		t.Error("hasGateFailure = false, want true")
	}
	if got := c.gateFailureCount(); got != 3 {
		t.Errorf("gateFailureCount = %d, want 3", got)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"under limit", "short", 10, "short"},
		{"at limit", "exactly10!", 10, "exactly10!"},
		{"truncated ascii", "a long string", 5, "a lo…"},
		{"n=1 returns first rune", "abcdef", 1, "a"},
		{"n=0 returns empty", "abc", 0, ""},
		{"multibyte safe at boundary", "héllo wörld", 8, "héllo w…"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := truncate(tc.in, tc.n); got != tc.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
			}
		})
	}
}
