package cli

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v3/internal/pipeline"
	"github.com/spf13/cobra"
)

const (
	ledgerFilename    = ".printing-press-tools-polish.json"
	ledgerStaleAfter  = 24 * time.Hour
	statusAccepted    = "accepted"
	suspiciousMaxLen  = 30
	suspiciousMinWord = 4

	// Finding kinds. These strings appear in the JSON output, the
	// ledger, and in agent-readable error messages, so changing a
	// value is a backwards-incompatible ledger format change.
	kindEmptyShort      = "empty-short"
	kindThinShort       = "thin-short"
	kindMissingReadOnly = "missing-read-only"
	kindEmptyMCPDesc    = "empty-mcp-description"
	kindThinMCPDesc     = "thin-mcp-description"

	// duplicateRationaleThreshold caps how many accepted findings may
	// share the same normalized note before the run is flagged as
	// incomplete. Hedge against bulk-accept patterns ("systemic to
	// OpenAPI specs" stamped on N findings without per-item
	// deliberation). Five identical rationales is suspicious; ten is
	// almost certainly a punt. See AGENTS.md "Deterministic Inventory
	// + Agent-Marked Ledger".
	duplicateRationaleThreshold = 5
)

// MCP description thresholds (pipeline.MCPDescMinLen, MCPDescMinWords,
// IsThinMCPDescription) live in pipeline so the scorecard applies the
// same predicate as the audit.

// FrameworkCommands mirrors cobratree/classify.go.tmpl. The runtime
// walker skips these names entirely — they're never registered as MCP
// tools — so audit findings on their Cobra Short are noise. Exported
// so the spec drift test in internal/spec can assert the cobra-Use
// reserved set is a strict superset (anything cobratree skips at MCP
// time would shadow at cobra time too).
var FrameworkCommands = map[string]bool{
	"about":         true,
	"agent-context": true,
	"api":           true,
	"auth":          true,
	"completion":    true,
	"doctor":        true,
	"feedback":      true,
	"help":          true,
	"profile":       true,
	"search":        true,
	"sql":           true,
	"version":       true,
	"which":         true,
}

// newToolsAuditCmd inspects a single printed CLI's command tree for
// MCP tool quality issues a deterministic check can catch: empty Short,
// suspiciously thin Short, and read-shaped command names that lack the
// mcp:read-only annotation. The output is a JSON list of findings the
// agent then runs through the references/tools-polish.md playbook.
//
// Deterministic only — judgment-grade questions ("is this description
// agent-grade?") belong in the polish skill, not here. Diagnostic
// exit code 0 regardless of findings.
func newToolsAuditCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "tools-audit <cli-dir>",
		Short: "Mechanically audit a printed CLI's MCP tool surface for missing annotations and thin descriptions",
		Long: `Walks <cli-dir>/internal/cli/*.go and reports per-command
findings that signal MCP tool quality issues. Detection is purely
mechanical: empty Short fields, Short text under 30 characters with
fewer than 4 words, and read-shaped command names that lack the
mcp:read-only annotation. The agent layer (references/tools-polish.md)
takes these findings and applies judgment for descriptions and
borderline classifications.

Exit 0 regardless of findings (diagnostic, not gating).`,
		Example: `  printing-press tools-audit ~/printing-press/library/dub
  printing-press tools-audit ~/printing-press/library/dub --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliDir := args[0]
			// Parse tools-manifest.json once and share it across the
			// audit, snapshot capture, and scorecard-delta gate. Each
			// of those previously called pipeline.ReadToolsManifest
			// independently — three reads on the first run.
			manifest, _ := pipeline.ReadToolsManifest(cliDir)

			findings, err := runToolsAudit(cliDir, manifest)
			if err != nil {
				return err
			}

			if asJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(findings)
			}

			previous := readPreviousLedger(cliDir)
			delta := reconcileWithLedger(previous, findings)

			if err := writeLedger(cliDir, manifest, findings, previous); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: writing ledger %s: %v\n", filepath.Join(cliDir, ledgerFilename), err)
			}
			completion := evaluateCompletion(manifest, findings, previous)
			renderToolsAuditTable(cmd.OutOrStdout(), findings, delta, completion)
			return nil
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON instead of a human-readable table")
	return cmd
}

// ToolsAuditFinding is one mechanical issue discovered in either a
// Cobra command literal or an entry in the runtime tools manifest.
// Status and Note are ledger-only; the audit phase emits findings
// without them (omitempty keeps --json clean for downstream parsing).
// The agent edits the persisted ledger to flip Status to "accepted"
// with a Note explaining why a finding is fine as-is. On re-run the
// binary preserves these fields for any finding whose identity key
// matches.
//
// Five finding kinds, two surfaces:
//   - Cobra surface (internal/cli/*.go): "empty-short", "thin-short",
//     "missing-read-only" — these check shell-out tools the runtime
//     walker registers from Cobra metadata.
//   - Manifest surface (tools-manifest.json): "empty-mcp-description",
//     "thin-mcp-description" — these check the descriptions agents
//     actually see for typed endpoint tools, where the source is the
//     OpenAPI spec rather than the Cobra Short.
type ToolsAuditFinding struct {
	Kind     string `json:"kind"`
	Command  string `json:"command"`          // tool name (manifest) or Cobra Use head (source)
	File     string `json:"file"`             // file path relative to cli-dir
	Line     int    `json:"line"`             // 1-based source line; 0 for manifest findings
	Evidence string `json:"evidence"`         // the offending text
	Status   string `json:"status,omitempty"` // "" (== pending) or "accepted"; agent writes
	Note     string `json:"note,omitempty"`   // agent-written rationale for an accept decision

	// Pre-decision fields. The agent fills these BEFORE flipping
	// Status to "accepted" on kinds where requiresPreDecisionFields
	// is true; the gate forces per-item deliberation instead of
	// pattern-matching with one rationale across many findings.
	SpecSourceMaterial string `json:"spec_source_material,omitempty"`
	TargetDescription  string `json:"target_description,omitempty"`
	GapAnalysis        string `json:"gap_analysis,omitempty"`
}

// readShapedNames is the heuristic for "this command name suggests a
// read operation." We exclude verbs already in cobratree's
// FrameworkCommands skip set (search, sql, doctor, version) — the
// runtime walker doesn't register those as MCP tools, so a missing
// read-only annotation is meaningless noise for them.
//
// tail/since/report/lint were added after dub's polish-Pass-2 surfaced
// them as commands the heuristic missed. They're consistently read-
// shaped across domains (log tail, time-windowed listing, generated
// report, static check). Do not add domain-specific verbs (funnel,
// leaderboard, journey, drift) — those mean reads in some verticals
// and writes in others; let Pass 2 catch them per CLI.
var readShapedNames = map[string]struct{}{
	"list": {}, "get": {}, "show": {}, "view": {},
	"find": {}, "describe": {}, "context": {}, "stats": {},
	"trending": {}, "trust": {}, "health": {}, "stale": {}, "orphans": {},
	"reconcile": {}, "analytics": {}, "tail": {}, "since": {},
	"report": {}, "lint": {},
}

// runToolsAudit walks two surfaces: the Cobra source under
// <cliDir>/internal/cli/*.go (shell-out tools) and the runtime tools
// manifest at <cliDir>/tools-manifest.json (typed endpoint tools).
// Findings are sorted by file then line for stable output.
func runToolsAudit(cliDir string, manifest *pipeline.ToolsManifest) ([]ToolsAuditFinding, error) {
	cobraFindings, err := auditCobraSource(cliDir)
	if err != nil {
		return nil, err
	}
	manifestFindings := auditMCPManifest(manifest)
	findings := append(cobraFindings, manifestFindings...)
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		return findings[i].Command < findings[j].Command
	})
	return findings, nil
}

func auditCobraSource(cliDir string) ([]ToolsAuditFinding, error) {
	pkgDir := filepath.Join(cliDir, "internal", "cli")
	entries, err := os.ReadDir(pkgDir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", pkgDir, err)
	}
	var findings []ToolsAuditFinding
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		full := filepath.Join(pkgDir, name)
		// Skip unparseable files — the agent can run go build separately
		// to surface syntax errors without failing the audit.
		file, err := parser.ParseFile(fset, full, nil, 0)
		if err != nil {
			continue
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || !isCobraCommandType(lit.Type) {
				return true
			}
			fields := extractCommandFields(lit)
			if fields.use == "" {
				return true
			}
			line := fset.Position(lit.Pos()).Line
			findings = append(findings, auditCommandFields(name, line, fields)...)
			return true
		})
	}
	return findings, nil
}

// auditMCPManifest flags MCP tool descriptions that fall below the
// agent-grade bar. The manifest is the source of truth for typed
// endpoint tools' descriptions; for shell-out tools, descriptions
// come from the Cobra Short and the auditCommandFields path covers
// them. Nil manifest (missing or malformed file) emits no findings.
func auditMCPManifest(m *pipeline.ToolsManifest) []ToolsAuditFinding {
	if m == nil {
		return nil
	}
	var findings []ToolsAuditFinding
	for _, t := range m.Tools {
		if t.Name == "" {
			continue
		}
		switch {
		case t.Description == "":
			findings = append(findings, ToolsAuditFinding{
				Kind: kindEmptyMCPDesc, Command: t.Name,
				File: pipeline.ToolsManifestFilename, Evidence: "(empty)",
			})
		case pipeline.IsThinMCPDescription(t.Description):
			findings = append(findings, ToolsAuditFinding{
				Kind: kindThinMCPDesc, Command: t.Name,
				File: pipeline.ToolsManifestFilename, Evidence: t.Description,
			})
		}
	}
	return findings
}

type commandFields struct {
	use         string
	short       string
	hasReadOnly bool
	hasEndpoint bool
	hasRunE     bool // true when the literal declares Run or RunE; parent groupers omit both
}

func isCobraCommandType(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return pkg.Name == "cobra" && sel.Sel.Name == "Command"
}

// extractCommandFields pulls Use/Short/Annotations out of a composite
// literal. Concatenated string literals and unresolvable expressions
// surface as the empty string — acceptable since the audit's job is to
// flag missing or thin content, not enforce that all values be string
// literals.
func extractCommandFields(lit *ast.CompositeLit) commandFields {
	var f commandFields
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Use":
			f.use = stringLit(kv.Value)
		case "Short":
			f.short = stringLit(kv.Value)
		case "Annotations":
			f.hasReadOnly, f.hasEndpoint = inspectAnnotations(kv.Value)
		case "Run", "RunE":
			f.hasRunE = true
		}
	}
	return f
}

func stringLit(e ast.Expr) string {
	bl, ok := e.(*ast.BasicLit)
	if !ok || bl.Kind != token.STRING {
		return ""
	}
	if len(bl.Value) >= 2 && (bl.Value[0] == '"' || bl.Value[0] == '`') {
		return bl.Value[1 : len(bl.Value)-1]
	}
	return bl.Value
}

func inspectAnnotations(e ast.Expr) (hasReadOnly, hasEndpoint bool) {
	lit, ok := e.(*ast.CompositeLit)
	if !ok {
		return false, false
	}
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		switch stringLit(kv.Key) {
		case "mcp:read-only":
			hasReadOnly = stringLit(kv.Value) == "true"
		case "pp:endpoint":
			hasEndpoint = stringLit(kv.Value) != ""
		}
	}
	return hasReadOnly, hasEndpoint
}

func auditCommandFields(file string, line int, f commandFields) []ToolsAuditFinding {
	cmdName := strings.Fields(f.use)
	if len(cmdName) == 0 {
		return nil
	}
	name := cmdName[0]

	// The Cobra-side checks only apply to commands that actually become
	// shell-out MCP tools at runtime. The cobratree walker skips:
	//   - pp:endpoint commands (registered as typed tools with
	//     spec-derived descriptions; the manifest audit covers those)
	//   - parent groupers (no Run/RunE means not actionable)
	//   - framework commands (auth, doctor, version, etc.) — including
	//     their entire subtree. Generated CLIs put children (e.g.
	//     `auth status`, `profile list`) in the same file as the parent,
	//     so the file basename's leading token tells us the subtree
	//     even when the child's own Use field doesn't match a framework
	//     name.
	// For all of these, the Cobra Short isn't the MCP tool description
	// the agent will see, so flagging it would be noise.
	isShellOut := !f.hasEndpoint && f.hasRunE && !FrameworkCommands[name] && !inFrameworkSubtree(file)

	var out []ToolsAuditFinding
	if isShellOut {
		switch {
		case f.short == "":
			out = append(out, ToolsAuditFinding{
				Kind: kindEmptyShort, Command: name, File: file, Line: line,
				Evidence: "(empty)",
			})
		case suspiciousShort(f.short):
			out = append(out, ToolsAuditFinding{
				Kind: kindThinShort, Command: name, File: file, Line: line,
				Evidence: f.short,
			})
		}
	}
	// missing-read-only applies only to commands that become shell-out
	// MCP tools (typed endpoint tools get classification from the spec
	// method; framework commands don't register at all).
	if isShellOut && !f.hasReadOnly && readShapedName(name) {
		out = append(out, ToolsAuditFinding{
			Kind: kindMissingReadOnly, Command: name, File: file, Line: line,
			Evidence: "name matches read heuristic; no mcp:read-only annotation",
		})
	}
	return out
}

// inFrameworkSubtree returns true when the file basename's leading
// token (split on `_`) is a framework command. Generated CLIs follow
// the convention <parent>.go or <parent>_<child>.go, so this catches
// both the parent and its subtree without needing to track AddCommand
// edges across files.
func inFrameworkSubtree(file string) bool {
	base := strings.TrimSuffix(file, ".go")
	if i := strings.IndexByte(base, '_'); i > 0 {
		base = base[:i]
	}
	return FrameworkCommands[base]
}

// suspiciousShort flags Short text that's both short (under 30 chars)
// and uses fewer than 4 words. Either dimension alone is fine: a long
// 3-word phrase is OK, and a short-but-precise instruction is OK.
// Both together is the "Search Ads" / "Subreddit Posts" anti-pattern.
func suspiciousShort(s string) bool {
	return len(s) < suspiciousMaxLen && len(strings.Fields(s)) < suspiciousMinWord
}

// readShapedName matches the head before the first hyphen against the
// readShapedNames set. Compound names like "get-foo" or "list-bar"
// classify by their leading verb.
func readShapedName(name string) bool {
	head := name
	if i := strings.IndexByte(name, '-'); i > 0 {
		head = name[:i]
	}
	_, ok := readShapedNames[head]
	return ok
}

// completionStatus carries the three load-bearing gates plus a
// resume hint. The gates distinguish a complete polish run from one
// that "looks done" via count-based pending alone. See AGENTS.md
// "Deterministic Inventory + Agent-Marked Ledger" for the rationale.
type completionStatus struct {
	IncompleteAccepts        []ToolsAuditFinding  // accepted but pre-decision fields missing
	DuplicateRationaleGroups []rationaleGroup     // accepts that share a normalized note
	ScorecardDeltaIssue      *scorecardDeltaIssue // accepted MCP-desc findings without score lift
	NextPending              *ToolsAuditFinding   // resume hint
}

// rationaleGroup is one cluster of accepted findings whose normalized
// notes match. The threshold is duplicateRationaleThreshold; groups
// below it are not surfaced (the agent gets some natural overlap).
type rationaleGroup struct {
	Rationale string              // the normalized note text
	Findings  []ToolsAuditFinding // accepted findings sharing this rationale
}

// scorecardDeltaIssue describes the scorecard-delta gate firing: the
// run accepted N thin-mcp-description findings but MCPDescriptionQuality
// did not improve. Either the accepts were unwarranted (the dimension
// would have lifted with overrides) or the dimension is mis-scored.
// Surface both numbers so the agent can debug.
type scorecardDeltaIssue struct {
	Before          int
	After           int
	AcceptedThinMCP int
}

// evaluateCompletion runs the three gates plus the resume hint
// against the current findings. The completionStatus carries gate
// failures (IncompleteAccepts, DuplicateRationaleGroups,
// ScorecardDeltaIssue) and a NextPending hint; gate-failure fields
// being empty + no pending entries means the run is complete.
func evaluateCompletion(manifest *pipeline.ToolsManifest, findings []ToolsAuditFinding, previous *ToolsAuditLedger) completionStatus {
	var c completionStatus
	for _, f := range findings {
		if f.Status != statusAccepted {
			continue
		}
		if requiresPreDecisionFields(f.Kind) && missingPreDecisionFields(f) {
			c.IncompleteAccepts = append(c.IncompleteAccepts, f)
		}
	}
	c.DuplicateRationaleGroups = detectDuplicateRationales(findings, duplicateRationaleThreshold)
	c.ScorecardDeltaIssue = checkScorecardDelta(manifest, findings, previous)
	c.NextPending = nextPendingFinding(findings, previous)
	return c
}

// requiresPreDecisionFields gates which finding kinds force the
// SpecSourceMaterial/TargetDescription/GapAnalysis trio when
// accepted. The Cobra-side kinds have legitimate uniform accept
// patterns (DO-NOT-EDIT generated files) that would be noise to
// gate; the MCP-description kinds are the ones bulk-accept with a
// generic rationale historically masked.
func requiresPreDecisionFields(kind string) bool {
	return kind == kindThinMCPDesc || kind == kindEmptyMCPDesc
}

func missingPreDecisionFields(f ToolsAuditFinding) bool {
	return strings.TrimSpace(f.SpecSourceMaterial) == "" ||
		strings.TrimSpace(f.TargetDescription) == "" ||
		strings.TrimSpace(f.GapAnalysis) == ""
}

// detectDuplicateRationales finds clusters of accepted findings whose
// normalized notes match. Normalization is lowercase + collapsed
// whitespace so "Systemic to OpenAPI specs" and "systemic  to openapi
// specs" cluster. Groups under the threshold are dropped — some
// natural overlap is fine; we're guarding against the punt pattern,
// not against any duplication.
func detectDuplicateRationales(findings []ToolsAuditFinding, threshold int) []rationaleGroup {
	if threshold <= 0 {
		return nil
	}
	clusters := make(map[string][]ToolsAuditFinding)
	for _, f := range findings {
		if f.Status != statusAccepted {
			continue
		}
		key := normalizeRationale(f.Note)
		if key == "" {
			continue
		}
		clusters[key] = append(clusters[key], f)
	}
	var groups []rationaleGroup
	for key, fs := range clusters {
		if len(fs) > threshold {
			groups = append(groups, rationaleGroup{Rationale: key, Findings: fs})
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		return len(groups[i].Findings) > len(groups[j].Findings)
	})
	return groups
}

func normalizeRationale(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// checkScorecardDelta compares ScorecardBefore (captured at run start)
// against the current MCPDescriptionQuality score. Fires only when
// there are accepted thin-mcp-description findings — accepting other
// kinds doesn't entail score movement, so skipping the gate for those
// avoids false positives.
//
// Returns nil when ScorecardBefore is absent (older ledger, or CLI
// with no manifest) or when there are no accepted thin-mcp findings.
// Both conditions mean the gate has nothing to enforce.
func checkScorecardDelta(manifest *pipeline.ToolsManifest, findings []ToolsAuditFinding, previous *ToolsAuditLedger) *scorecardDeltaIssue {
	if previous == nil || previous.ScorecardBefore == nil {
		return nil
	}
	var acceptedThinMCP int
	for _, f := range findings {
		if f.Status == statusAccepted && f.Kind == kindThinMCPDesc {
			acceptedThinMCP++
		}
	}
	if acceptedThinMCP == 0 {
		return nil
	}
	current, scored := pipeline.ScoreMCPDescriptionQualityForManifest(manifest)
	if !scored {
		return nil
	}
	if current > previous.ScorecardBefore.MCPDescriptionQuality {
		return nil
	}
	return &scorecardDeltaIssue{
		Before:          previous.ScorecardBefore.MCPDescriptionQuality,
		After:           current,
		AcceptedThinMCP: acceptedThinMCP,
	}
}

// isPending reports whether a finding still needs the agent's
// attention. Either status is unset, or status is "accepted" on a
// kind that requires pre-decision fields and the fields aren't
// populated — an accepted-but-incomplete entry, which the gates
// surface and which counts as pending for the summary line.
func isPending(f ToolsAuditFinding) bool {
	if f.Status != statusAccepted {
		return true
	}
	return requiresPreDecisionFields(f.Kind) && missingPreDecisionFields(f)
}

// nextPendingFinding returns the resume hint: the first finding that
// isPending. The previous ledger's Progress.LastProcessedFindingID is
// read as a soft hint — when present, the search starts after that
// finding so a re-invocation resumes mid-walk; if exhausted or
// missing, falls back to a head-to-tail scan.
func nextPendingFinding(findings []ToolsAuditFinding, previous *ToolsAuditLedger) *ToolsAuditFinding {
	startIdx := 0
	if previous != nil && previous.Progress != nil && previous.Progress.LastProcessedFindingID != "" {
		for i, f := range findings {
			if findingKey(f) == previous.Progress.LastProcessedFindingID {
				startIdx = i + 1
				break
			}
		}
	}
	if f := firstPending(findings, startIdx, len(findings)); f != nil {
		return f
	}
	return firstPending(findings, 0, startIdx)
}

// firstPending returns the first isPending finding in findings[lo:hi]
// or nil. Extracted so nextPendingFinding's forward + fallback scans
// share the same predicate.
func firstPending(findings []ToolsAuditFinding, lo, hi int) *ToolsAuditFinding {
	if lo < 0 {
		lo = 0
	}
	if hi > len(findings) {
		hi = len(findings)
	}
	for i := lo; i < hi; i++ {
		if isPending(findings[i]) {
			return &findings[i]
		}
	}
	return nil
}

func renderToolsAuditTable(w io.Writer, findings []ToolsAuditFinding, delta ledgerDelta, completion completionStatus) {
	var pending, accepted int
	for _, f := range findings {
		if isPending(f) {
			pending++
		} else {
			accepted++
		}
	}
	gateFired := completion.hasGateFailure()

	switch {
	case pending == 0 && !gateFired:
		if accepted > 0 {
			fmt.Fprintf(w, "tools-audit: no pending findings (%d accepted)\n", accepted)
		} else {
			fmt.Fprintln(w, "tools-audit: no findings")
		}
	case pending == 0 && gateFired:
		fmt.Fprintf(w, "tools-audit: incomplete (%d accepted, %d gate failure(s))\n", accepted, completion.gateFailureCount())
	default:
		fmt.Fprintf(w, "tools-audit: %d pending finding(s)", pending)
		if accepted > 0 {
			fmt.Fprintf(w, " (%d accepted)", accepted)
		}
		if gateFired {
			fmt.Fprintf(w, ", %d gate failure(s)", completion.gateFailureCount())
		}
		fmt.Fprintln(w)
	}

	if delta.hasPrevious {
		fmt.Fprintf(w, "since last run: %d resolved, %d new\n", len(delta.resolved), len(delta.added))
	}

	renderCompletionGates(w, completion)

	if pending > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "%-20s  %-15s  %-30s  %s\n", "KIND", "COMMAND", "FILE:LINE", "EVIDENCE")
		for _, f := range findings {
			if !isPending(f) {
				continue
			}
			loc := fmt.Sprintf("%s:%d", f.File, f.Line)
			fmt.Fprintf(w, "%-20s  %-15s  %-30s  %s\n", f.Kind, f.Command, loc, f.Evidence)
		}
	}

	if completion.NextPending != nil {
		f := completion.NextPending
		loc := fmt.Sprintf("%s:%d", f.File, f.Line)
		fmt.Fprintln(w)
		fmt.Fprintf(w, "next: %s on %s (%s)\n", f.Kind, f.Command, loc)
	}
}

// gateFailureCount returns the number of gates surfacing an issue the
// agent has to act on. NextPending is a resume hint, not a gate, and
// is excluded.
func (c completionStatus) gateFailureCount() int {
	n := 0
	if len(c.IncompleteAccepts) > 0 {
		n++
	}
	if len(c.DuplicateRationaleGroups) > 0 {
		n++
	}
	if c.ScorecardDeltaIssue != nil {
		n++
	}
	return n
}

func (c completionStatus) hasGateFailure() bool {
	return c.gateFailureCount() > 0
}

// renderCompletionGates surfaces each fired gate with the specific
// reason and the entries the agent should revisit. Silent when no
// gate fired.
func renderCompletionGates(w io.Writer, c completionStatus) {
	if !c.hasGateFailure() {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "incomplete: the run is not done yet")
	if len(c.IncompleteAccepts) > 0 {
		fmt.Fprintf(w, "  - %d accepted finding(s) missing pre-decision fields (spec_source_material, target_description, gap_analysis):\n", len(c.IncompleteAccepts))
		for _, f := range c.IncompleteAccepts {
			fmt.Fprintf(w, "      %s on %s (%s:%d)\n", f.Kind, f.Command, f.File, f.Line)
		}
	}
	for _, g := range c.DuplicateRationaleGroups {
		fmt.Fprintf(w, "  - %d accepted finding(s) share rationale %q — differentiate per item or rewrite:\n", len(g.Findings), truncate(g.Rationale, 60))
		for _, f := range g.Findings {
			fmt.Fprintf(w, "      %s on %s (%s:%d)\n", f.Kind, f.Command, f.File, f.Line)
		}
	}
	if d := c.ScorecardDeltaIssue; d != nil {
		fmt.Fprintf(w, "  - MCPDescriptionQuality unchanged (%d/10 → %d/10) but %d thin-mcp-description finding(s) accepted; either write overrides or accepts are unwarranted\n", d.Before, d.After, d.AcceptedThinMCP)
	}
}

// ToolsAuditLedger is the on-disk snapshot of the last audit run.
//
// ScorecardBefore is captured on the first audit run that finds no
// existing ledger and preserved across subsequent runs. It anchors the
// scorecard-delta gate: a polish run that "completes" with accepted
// thin-mcp-description findings but no movement in
// MCPDescriptionQuality is incomplete by definition.
//
// Progress is an optional checkpoint the polish skill writes after
// deliberating on each finding. A re-invocation after a context flush
// reads it to resume mid-walk. When absent, the resume hint is the
// first pending finding in scan order.
type ToolsAuditLedger struct {
	Timestamp       time.Time           `json:"timestamp"`
	CLIDir          string              `json:"cli_dir"`
	Findings        []ToolsAuditFinding `json:"findings"`
	ScorecardBefore *ScorecardSnapshot  `json:"scorecard_before,omitempty"`
	Progress        *PolishProgress     `json:"progress,omitempty"`
}

// ScorecardSnapshot captures the scorecard dimensions the polish
// ledger gates on, taken at run start.
type ScorecardSnapshot struct {
	MCPDescriptionQuality int       `json:"mcp_description_quality"`
	Captured              time.Time `json:"captured"`
}

// PolishProgress is the agent-written checkpoint for resume after
// context loss. The binary derives a fallback resume hint from the
// finding list when this is absent, so the field is optional.
type PolishProgress struct {
	LastProcessedFindingID string `json:"last_processed_finding_id,omitempty"`
}

type ledgerDelta struct {
	hasPrevious bool
	resolved    []ToolsAuditFinding // present in previous, absent in current
	added       []ToolsAuditFinding // present in current, absent in previous
}

// readPreviousLedger loads the ledger at <cli-dir>/<ledgerFilename>.
// Returns nil for missing, corrupt, or stale ledgers — the audit treats
// all three as "no resumable state." Stale and corrupt files are
// deleted so the next write starts clean. Read errors other than "not
// exists" silently fall back to no-ledger; the next write surfaces
// the same error to stderr.
func readPreviousLedger(cliDir string) *ToolsAuditLedger {
	path := filepath.Join(cliDir, ledgerFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var l ToolsAuditLedger
	if err := json.Unmarshal(data, &l); err != nil {
		_ = os.Remove(path)
		return nil
	}
	if time.Since(l.Timestamp) > ledgerStaleAfter {
		_ = os.Remove(path)
		return nil
	}
	return &l
}

func writeLedger(cliDir string, manifest *pipeline.ToolsManifest, findings []ToolsAuditFinding, previous *ToolsAuditLedger) error {
	ledger := ToolsAuditLedger{
		Timestamp: time.Now().UTC(),
		CLIDir:    cliDir,
		Findings:  findings,
	}
	// ScorecardBefore is sticky: captured on the first run that has no
	// existing ledger, preserved on every subsequent run. Anchoring the
	// scorecard-delta gate, so re-running must not rebase the baseline.
	if previous != nil && previous.ScorecardBefore != nil {
		ledger.ScorecardBefore = previous.ScorecardBefore
	} else {
		ledger.ScorecardBefore = captureScorecardSnapshot(manifest)
	}
	if previous != nil && previous.Progress != nil {
		ledger.Progress = previous.Progress
	}
	data, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding ledger: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(cliDir, ledgerFilename), data, 0644)
}

// captureScorecardSnapshot reads the dimensions the ledger gates on.
// Returns nil when no dimension can be scored (e.g., no manifest) so
// the gate has nothing to enforce.
func captureScorecardSnapshot(manifest *pipeline.ToolsManifest) *ScorecardSnapshot {
	score, scored := pipeline.ScoreMCPDescriptionQualityForManifest(manifest)
	if !scored {
		return nil
	}
	return &ScorecardSnapshot{
		MCPDescriptionQuality: score,
		Captured:              time.Now().UTC(),
	}
}

// reconcileWithLedger carries Status/Note from the previous ledger
// onto matching current findings (so accept decisions survive re-runs)
// and computes the resolved/added delta in a single pass. Identity is
// (file, line, kind, command, evidence); a finding whose Short was
// rewritten reads as "old resolved, new added" rather than mutated.
func reconcileWithLedger(previous *ToolsAuditLedger, current []ToolsAuditFinding) ledgerDelta {
	if previous == nil {
		return ledgerDelta{}
	}
	prev := make(map[string]ToolsAuditFinding, len(previous.Findings))
	for _, f := range previous.Findings {
		prev[findingKey(f)] = f
	}
	delta := ledgerDelta{hasPrevious: true}
	seen := make(map[string]bool, len(current))
	for i := range current {
		k := findingKey(current[i])
		seen[k] = true
		if old, ok := prev[k]; ok {
			current[i].Status = old.Status
			current[i].Note = old.Note
			current[i].SpecSourceMaterial = old.SpecSourceMaterial
			current[i].TargetDescription = old.TargetDescription
			current[i].GapAnalysis = old.GapAnalysis
		} else {
			delta.added = append(delta.added, current[i])
		}
	}
	for k, f := range prev {
		if !seen[k] {
			delta.resolved = append(delta.resolved, f)
		}
	}
	return delta
}

func findingKey(f ToolsAuditFinding) string {
	return fmt.Sprintf("%s:%d:%s:%s:%s", f.File, f.Line, f.Kind, f.Command, f.Evidence)
}
