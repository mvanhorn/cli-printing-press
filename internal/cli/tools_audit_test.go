package cli

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pipeline"
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

func TestAuditMCPManifestSkipsHiddenEndpointMirrors(t *testing.T) {
	manifest := &pipeline.ToolsManifest{
		MCP: &pipeline.ManifestMCP{
			EndpointTools: "hidden",
			Orchestration: "code",
		},
		Tools: []pipeline.ManifestTool{
			{Name: "demo_get", Description: "Get"},
			{Name: "demo_create", Description: "Create"},
		},
	}

	if got := auditMCPManifest(manifest); len(got) != 0 {
		t.Fatalf("got %d findings for hidden endpoint mirrors, want 0: %#v", len(got), got)
	}
}

func TestAuditMCPManifestStillFlagsVisibleEndpointMirrors(t *testing.T) {
	manifest := &pipeline.ToolsManifest{
		MCP: &pipeline.ManifestMCP{
			EndpointTools: "visible",
		},
		Tools: []pipeline.ManifestTool{
			{Name: "demo_get", Description: "Get"},
		},
	}

	got := auditMCPManifest(manifest)
	if len(got) != 1 {
		t.Fatalf("got %d findings, want 1: %#v", len(got), got)
	}
	if got[0].Kind != kindThinMCPDesc {
		t.Fatalf("kind = %q, want %q", got[0].Kind, kindThinMCPDesc)
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

// TestAuditCommandFieldsExplicitReadOnly pins #891 at the
// auditCommandFields layer: the missing-read-only finding fires only
// when the annotation is genuinely absent. The AST-level differentiation
// between explicit-true and explicit-false collapses to the same
// hasExplicitReadOnly=true signal by the time it reaches this function,
// so a single "present" case suffices here; the explicit-false vs
// explicit-true distinction is covered by TestInspectAnnotationsExplicitReadOnlyFalse.
func TestAuditCommandFieldsExplicitReadOnly(t *testing.T) {
	cases := []struct {
		name                string
		fields              commandFields
		wantMissingReadOnly bool
	}{
		{
			name: "explicit annotation (any value) suppresses missing-read-only",
			fields: commandFields{
				use:                 "report",
				short:               "Generate a report",
				hasExplicitReadOnly: true,
				hasRunE:             true,
			},
			wantMissingReadOnly: false,
		},
		{
			name: "absent annotation fires the finding",
			fields: commandFields{
				use:                 "report",
				short:               "Generate a report",
				hasExplicitReadOnly: false,
				hasRunE:             true,
			},
			wantMissingReadOnly: true,
		},
		{
			name: "non-read-shaped names never fire regardless of annotation",
			fields: commandFields{
				use:                 "post",
				short:               "Post a message",
				hasExplicitReadOnly: false,
				hasRunE:             true,
			},
			wantMissingReadOnly: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings := auditCommandFields("internal/cli/x.go", 1, tc.fields)
			var got bool
			for _, f := range findings {
				if f.Kind == kindMissingReadOnly {
					got = true
					break
				}
			}
			if got != tc.wantMissingReadOnly {
				t.Fatalf("missing-read-only fired = %v, want %v (findings: %+v)", got, tc.wantMissingReadOnly, findings)
			}
		})
	}
}

func TestAuditCommandFieldsThinShortStillFlagsShellOutCommands(t *testing.T) {
	findings := auditCommandFields("internal/cli/cancel.go", 1, commandFields{
		use:     "cancel",
		short:   "Manage cancel",
		hasRunE: true,
	})

	var gotThin bool
	for _, f := range findings {
		if f.Kind == kindThinShort {
			gotThin = true
			break
		}
	}
	if !gotThin {
		t.Fatalf("thin-short finding missing for shell-out command with thin Short: %+v", findings)
	}
}

func TestAuditCommandFieldsParentNoSubcommandRunESkipsParentFindings(t *testing.T) {
	findings := auditCommandFields("internal/cli/reports.go", 1, commandFields{
		use:                       "report",
		short:                     "Manage reports",
		hasRunE:                   true,
		hasParentNoSubcommandRunE: true,
	})
	if len(findings) != 0 {
		t.Fatalf("parentNoSubcommandRunE parent findings = %+v, want none", findings)
	}
}

func TestAuditCommandFieldsFrameworkNamesOnlySkippedInFrameworkSubtrees(t *testing.T) {
	t.Run("nested framework-named domain command is audited", func(t *testing.T) {
		findings := auditCommandFields("items.go", 1, commandFields{
			use:     "search",
			short:   "Find",
			hasRunE: true,
		})

		var gotThin, gotMissingReadOnly bool
		for _, f := range findings {
			switch f.Kind {
			case kindThinShort:
				gotThin = true
			case kindMissingReadOnly:
				gotMissingReadOnly = true
			}
		}
		if !gotThin || !gotMissingReadOnly {
			t.Fatalf("nested search findings = %+v, want thin-short and missing-read-only", findings)
		}
	})

	t.Run("top-level framework file is skipped", func(t *testing.T) {
		findings := auditCommandFields("search.go", 1, commandFields{
			use:     "search",
			short:   "Find",
			hasRunE: true,
		})
		if len(findings) != 0 {
			t.Fatalf("top-level framework search findings = %+v, want none", findings)
		}
	})
}

func TestExtractCommandFieldsDetectsParentNoSubcommandRunE(t *testing.T) {
	src := `package x
import "github.com/spf13/cobra"
func newMintsCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "mints",
		Short: "Manage mints",
		RunE:  parentNoSubcommandRunE(flags),
	}
}`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "mints.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var lit *ast.CompositeLit
	ast.Inspect(file, func(n ast.Node) bool {
		c, ok := n.(*ast.CompositeLit)
		if !ok || !isCobraCommandType(c.Type) {
			return true
		}
		lit = c
		return false
	})
	if lit == nil {
		t.Fatalf("no cobra command literal found")
	}

	fields := extractCommandFields(lit)
	if !fields.hasRunE {
		t.Fatalf("hasRunE = false, want true")
	}
	if !fields.hasParentNoSubcommandRunE {
		t.Fatalf("hasParentNoSubcommandRunE = false, want true")
	}
}

func TestAuditCobraSourceDistinguishesSentinelFromRealRunE(t *testing.T) {
	root := t.TempDir()
	cliDir := filepath.Join(root, "internal", "cli")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatalf("mkdir cli dir: %v", err)
	}
	sentinelSrc := `package cli
import "github.com/spf13/cobra"
func newReportCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "report",
		Short: "Manage reports",
		RunE:  parentNoSubcommandRunE(flags),
	}
}`
	realRunESrc := `package cli
import "github.com/spf13/cobra"
func newReportRealCmd(flags *rootFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "report",
		Short: "Manage reports",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
}`
	if err := os.WriteFile(filepath.Join(cliDir, "report.go"), []byte(sentinelSrc), 0o644); err != nil {
		t.Fatalf("write sentinel source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cliDir, "report_real.go"), []byte(realRunESrc), 0o644); err != nil {
		t.Fatalf("write real RunE source: %v", err)
	}

	findings, err := auditCobraSource(root)
	if err != nil {
		t.Fatalf("auditCobraSource: %v", err)
	}
	var sentinelFindings, realThinShort, realMissingReadOnly int
	for _, f := range findings {
		switch f.File {
		case "report.go":
			sentinelFindings++
		case "report_real.go":
			switch f.Kind {
			case kindThinShort:
				realThinShort++
			case kindMissingReadOnly:
				realMissingReadOnly++
			}
		}
	}
	if sentinelFindings != 0 {
		t.Fatalf("sentinel parent findings = %d in %+v, want none", sentinelFindings, findings)
	}
	if realThinShort != 1 || realMissingReadOnly != 1 {
		t.Fatalf("real RunE findings thin=%d missing-read-only=%d in %+v, want 1 each", realThinShort, realMissingReadOnly, findings)
	}
}

// TestInspectAnnotationsExplicitReadOnlyFalse pins the AST-level
// helper: any value for `mcp:read-only` — including "false" — sets
// hasExplicitReadOnly. The old behavior treated "false" as absent.
func TestInspectAnnotationsExplicitReadOnlyFalse(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		wantSetRO bool
	}{
		{
			name:      "explicit true",
			src:       `package x; var _ = map[string]string{"mcp:read-only": "true"}`,
			wantSetRO: true,
		},
		{
			name:      "explicit false",
			src:       `package x; var _ = map[string]string{"mcp:read-only": "false"}`,
			wantSetRO: true,
		},
		{
			name:      "absent",
			src:       `package x; var _ = map[string]string{}`,
			wantSetRO: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "x.go", tc.src, 0)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			var lit *ast.CompositeLit
			ast.Inspect(file, func(n ast.Node) bool {
				if c, ok := n.(*ast.CompositeLit); ok {
					if _, ok := c.Type.(*ast.MapType); ok {
						lit = c
						return false
					}
				}
				return true
			})
			if lit == nil {
				t.Fatalf("no map literal found")
			}
			gotRO, _ := inspectAnnotations(lit)
			if gotRO != tc.wantSetRO {
				t.Errorf("hasExplicitReadOnly = %v, want %v", gotRO, tc.wantSetRO)
			}
		})
	}
}
