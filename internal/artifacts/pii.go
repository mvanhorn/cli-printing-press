package artifacts

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/piiplaceholders"
)

// PII gate implementation following the Deterministic Inventory +
// Agent-Marked Ledger pattern (docs/PATTERNS.md). Peer to secrets.go.

const (
	PIILedgerFilename   = ".printing-press-pii-polish.json"
	PIILedgerStaleAfter = 24 * time.Hour
	PIIStatusAccepted   = "accepted"

	// Caps for the punt-pattern gates. The 5 cluster cap matches
	// tools-audit; the 10 baseline floor for all-accepted-no-fixes is
	// large enough that small genuinely-clean runs (which legitimately
	// accept everything) don't trip it.
	PIIDuplicateRationaleThreshold = 5
	PIIAllAcceptedNoFixesThreshold = 10
)

// Finding kinds. These appear in the JSON output and the ledger;
// changing a value is a backward-incompatible ledger format change.
const (
	PIIKindCardLast4     = "card-last-4"
	PIIKindOrderID       = "order-id"
	PIIKindASIN          = "asin"
	PIIKindEmail         = "email"
	PIIKindPhoneUS       = "phone-us"
	PIIKindZipPlus4      = "zip-plus-4"
	PIIKindPostalAddress = "postal-address"
)

// Categories the agent picks from when accepting a finding. The closed
// enum forces the agent to name the shape of non-PII; freeform reasoning
// goes in Note.
const (
	PIICategoryAttribution          = "attribution"
	PIICategoryPlaceName            = "place_name"
	PIICategoryCorporateName        = "corporate_name"
	PIICategoryDocumentationExample = "documentation_example"
	PIICategoryAPIProviderData      = "api_provider_data"
	PIICategorySyntheticPlaceholder = "synthetic_placeholder"
	PIICategoryOther                = "other"
)

// validPIICategories is the closed set the gate validates against.
var validPIICategories = map[string]bool{
	PIICategoryAttribution:          true,
	PIICategoryPlaceName:            true,
	PIICategoryCorporateName:        true,
	PIICategoryDocumentationExample: true,
	PIICategoryAPIProviderData:      true,
	PIICategorySyntheticPlaceholder: true,
	PIICategoryOther:                true,
}

// PIIFinding is one mechanical detection. Status/Note/Category/
// EvidenceContext are agent-written ledger fields preserved across
// re-runs when the identity key (file, line, kind, normalized span)
// matches.
type PIIFinding struct {
	Kind        string `json:"kind"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Column      int    `json:"column"`
	MatchedSpan string `json:"matched_span"`

	Status          string `json:"status,omitempty"`
	Note            string `json:"note,omitempty"`
	Category        string `json:"category,omitempty"`
	EvidenceContext string `json:"evidence_context,omitempty"`
}

type piiDetector struct {
	kind    string
	pattern *regexp.Regexp
}

var piiDetectors = []piiDetector{
	{
		kind: PIIKindCardLast4,
		// Requires a context token within 5 characters of a 4-digit
		// run. The alphabetic context tokens carry a leading word
		// boundary so "discard 1234" doesn't match as "card 1234";
		// mask shapes (xxxx, ****) are non-word so they can't carry
		// \b but their length and shape are unambiguous.
		pattern: regexp.MustCompile(`(?i)(?:\b(?:card|visa|mastercard|amex|ending in|last\s+4)|x{4,}|\*{4,})[\s:.\-]{0,5}\d{4}`),
	},
	{
		kind: PIIKindOrderID,
		// Physical and digital order IDs observed in browser-sniff
		// captures. The canonical synthetic placeholders are filtered
		// after matching.
		pattern: piiplaceholders.OrderIDPattern(),
	},
	{
		kind: PIIKindASIN,
		// ASIN-shaped product IDs from browser-sniff captures. The
		// canonical B0EXAMPLE* placeholders are filtered after matching.
		pattern: piiplaceholders.ASINPattern(),
	},
	{
		kind: PIIKindEmail,
		// Standard email shape with a TLD of 2+ chars. Word boundaries
		// guard against capturing surrounding punctuation.
		pattern: regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`),
	},
	{
		kind: PIIKindPhoneUS,
		// US-shaped 10-digit phone with optional +1 prefix and
		// parens/separators. NANP requires the area code and the
		// exchange code to each start with 2-9 — leading 0 or 1 is
		// not a real US phone. That constraint filters out 10-digit
		// product UPCs (`0190074442`) and coordinate-shaped numerics
		// (`106.0512973`) without rejecting any real phone shape.
		// Catches "(415) 555-0123", "415-555-0123", "+1 415 555 0123".
		pattern: regexp.MustCompile(`\b(?:\+?1[\s.\-]?)?\(?[2-9]\d{2}\)?[\s.\-]?[2-9]\d{2}[\s.\-]?\d{4}\b`),
	},
	{
		kind: PIIKindZipPlus4,
		// Standard ZIP+4. Common false positive: API request IDs and
		// batch identifiers with the same shape. Agent layer judges.
		pattern: regexp.MustCompile(`\b\d{5}\-\d{4}\b`),
	},
	{
		kind: PIIKindPostalAddress,
		// Street-number + 1-4 name words (Title-Case OR ALL-CAPS) +
		// street-suffix (case-insensitive). The Title-Case alternative
		// catches real API responses (`1234 Main Street` is the default
		// shape from Amazon/Shopify/Stripe/FedEx). Name words must
		// start with an uppercase letter — this is the guard against
		// conversational prose like "2 surfaces a clean way" where
		// the words are all lowercase and would not match the leading
		// `[A-Z]`. Fully-lowercase real addresses are still missed; if
		// captures surface them, expand with explicit handling.
		pattern: regexp.MustCompile(`\b\d+\s+[A-Z][a-zA-Z]+(?:\s+[A-Z][a-zA-Z]+){0,3}\s+(?i:ST|STREET|AVE|AVENUE|RD|ROAD|BLVD|BOULEVARD|DR|DRIVE|LN|LANE|CT|COURT|PL|PLACE|WAY)\b`),
	},
}

// Scoped to the capture-to-publish leak path: manuscripts, test
// fixtures, README, and *_test.go files. Non-test Go source and
// module metadata are excluded by default.
var highRiskFileGlobs = []string{
	"*.json",
	"*.yaml",
	"*.yml",
	"*.md",
	"*_test.go",
}

var highRiskDirGlobs = []string{
	".manuscripts",
	"testdata",
}

// ToolsPolishLedgerFilename is the tools-audit polish ledger basename.
// Exported here (not in cli) to break the cli → artifacts cycle that
// would otherwise force tools_audit.go to import its own package.
const ToolsPolishLedgerFilename = ".printing-press-tools-polish.json"

// Polish-skill ledgers contain matched_span of prior findings; without
// the exclusion, the next audit recursively flags its own state.
var excludedFiles = map[string]bool{
	"tools-manifest.json":     true,
	PIILedgerFilename:         true,
	ToolsPolishLedgerFilename: true,
}

// rootVendorSpecFiles are the CLI-root basenames the generator embeds
// as vendor source — the OpenAPI/internal spec the operator passed to
// `--spec`. Vendor-published `example:` values (emails, phones,
// addresses) are documentation, not customer PII, so a Stripe/Zendesk/
// GitHub spec doesn't false-fail every promote. Exemption is depth-1
// only; a spec.yaml nested under .manuscripts/ or testdata/ is captured
// content and stays in scope unless content-detection identifies it as
// an archived vendor spec (see manuscriptsDir + looksLikeVendorAPISpec).
// Mirrors findArchivedSpec()'s candidate set in
// internal/pipeline/climanifest.go.
var rootVendorSpecFiles = map[string]bool{
	"spec.json": true,
	"spec.yaml": true,
	"spec.yml":  true,
}

// manuscriptsDir is the path component that scopes the content-detected
// vendor-spec exemption. Vendor specs archived under any depth inside
// .manuscripts/ — research/, discovery/, or freeform subdirs — are
// reproducible from the upstream URL and contain documentation `example:`
// values, not customer PII. Limiting the content exemption to manuscripts
// keeps the bypass off non-archive paths (committed docs/, generated
// internal/, etc.) so a hand-edited file at docs/api.yaml still scans.
const manuscriptsDir = ".manuscripts"

// vendorSpecMarkers are the OpenAPI/Swagger version-marker patterns
// looksLikeVendorAPISpec probes for in a file's head bytes. The two-form
// shape (JSON quoted-key vs YAML unquoted-key) is required because vendor
// docs ship both formats. Both forms anchor the key to the document
// start: JSON requires `openapi`/`swagger` as the first key after the
// opening brace; YAML requires it as the first content line after any
// allowed lead-in (YAML directive `%...`, document marker `---`,
// comments, blank lines). Without these anchors, a non-spec file with
// PII in earlier keys could bypass scanning when it happened to also
// contain an `openapi`/`swagger` marker deeper in the payload — a
// response envelope, captured config blob, or research-notes YAML that
// listed real values before the version field. Version constraints
// (2.x or 3.x) avoid matching freeform mentions like "openapi: future".
var vendorSpecMarkers = []*regexp.Regexp{
	// JSON: {"openapi": "3.x.x" — openapi must be the first key.
	regexp.MustCompile(`\A\s*\{\s*"openapi"\s*:\s*"[23]\.`),
	regexp.MustCompile(`\A\s*\{\s*"swagger"\s*:\s*"2\.`),
	// YAML: optional %directives, `---` document marker, comments, and
	// blank lines may precede the marker, but no other content key can
	// appear before `openapi`/`swagger` at column 0.
	regexp.MustCompile(`\A(?:(?:%[^\n]*|---[ \t]*|[ \t]*#[^\n]*|[ \t]*)\n)*openapi\s*:\s*['"]?[23]\.`),
	regexp.MustCompile(`\A(?:(?:%[^\n]*|---[ \t]*|[ \t]*#[^\n]*|[ \t]*)\n)*swagger\s*:\s*['"]?2\.`),
}

// looksLikeVendorAPISpec reports whether the first few KB of a file
// match an OpenAPI 2.x/3.x or Swagger 2.0 root-document marker. Content
// detection beats filename heuristics because vendors ship spec source
// under arbitrary basenames (`apps/calendars.json`, `pushpress-v3.yaml`)
// that no glob would reliably cover. Restricted at the call site to
// files under .manuscripts/ so non-archive paths cannot bypass scanning
// by embedding a stray version marker.
func looksLikeVendorAPISpec(probe []byte) bool {
	for _, re := range vendorSpecMarkers {
		if re.Match(probe) {
			return true
		}
	}
	return false
}

// isUnderManuscripts reports whether the slash-separated relative path
// lives anywhere inside a .manuscripts/ subtree.
func isUnderManuscripts(relSlash string) bool {
	return slices.Contains(strings.Split(relSlash, "/"), manuscriptsDir)
}

// skippedDirs are subtree names the walker never descends into at the
// top level. Scoping to depth-1 is deliberate — `.git` and friends as
// direct children of the cli-dir are infrastructure; the same names
// nested inside `.manuscripts/` or `testdata/` are captured content
// and must be scanned.
var skippedDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"build":        true,
}

func isSyntheticPIIPlaceholder(kind, matched string) bool {
	switch kind {
	case PIIKindOrderID:
		return piiplaceholders.IsSyntheticOrderID(matched)
	case PIIKindASIN:
		return piiplaceholders.IsSyntheticASIN(matched)
	case PIIKindPostalAddress:
		return piiplaceholders.IsSyntheticPostalAddress(matched)
	default:
		return false
	}
}

// FindPII walks root, applies the file-scoping rules, and returns all
// PII-shape matches. Ordering is stable (file, line, column, kind) so
// the JSON output and ledger reconcile cleanly across runs.
//
// Per-file scan errors (unreadable file, permission denied) are logged
// to stderr and skipped — a single bad file does not abort the gate.
func FindPII(root string) ([]PIIFinding, error) {
	var findings []PIIFinding
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path == root {
				return nil
			}
			// Skip only at depth-1 from root.
			parent := filepath.Dir(path)
			if parent == root && skippedDirs[entry.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relSlash := filepath.ToSlash(rel)
		if !isHighRiskFile(relSlash) {
			return nil
		}
		fileFindings, err := scanPIIFile(root, path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: pii-audit skipping %s: %v\n", relSlash, err)
			return nil
		}
		findings = append(findings, fileFindings...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		if findings[i].Column != findings[j].Column {
			return findings[i].Column < findings[j].Column
		}
		return findings[i].Kind < findings[j].Kind
	})
	return findings, nil
}

// isHighRiskFile reports whether the relative path (forward-slash) is
// in the scan scope. A file qualifies when its basename matches one
// of the file globs OR when any path component matches one of the
// directory globs. excludedFiles overrides matches.
func isHighRiskFile(relSlash string) bool {
	base := filepath.Base(relSlash)
	if excludedFiles[base] {
		return false
	}
	if !strings.Contains(relSlash, "/") && rootVendorSpecFiles[base] {
		return false
	}
	parts := strings.Split(relSlash, "/")
	for _, dir := range highRiskDirGlobs {
		if slices.Contains(parts, dir) {
			return true
		}
	}
	for _, pattern := range highRiskFileGlobs {
		match, err := filepath.Match(pattern, base)
		if err == nil && match {
			return true
		}
	}
	// README is always in scope (any case, with or without extension
	// handled by *.md above; this catches bare README files).
	if strings.EqualFold(base, "README") {
		return true
	}
	return false
}

// scanPIIFile runs all detectors against a single file line-by-line.
// Binary files (null-byte probe) are skipped. Returns findings keyed
// to the file's path relative to root with forward-slash separators.
func scanPIIFile(root, path string) ([]PIIFinding, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	reader := bufio.NewReaderSize(file, 8192)
	probe, err := reader.Peek(8192)
	if err != nil && err != io.EOF && err != bufio.ErrBufferFull {
		return nil, err
	}
	if bytes.Contains(probe, []byte{0}) {
		return nil, nil
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		return nil, err
	}
	relSlash := filepath.ToSlash(rel)

	// Vendor-published OpenAPI/Swagger source archived under .manuscripts/
	// carries documentation `example:` values, not customer PII. Mirrors
	// the depth-1 rootVendorSpecFiles exemption for the case where the
	// spec source lives nested in research/, discovery/, or freeform
	// archive subdirs and the basename isn't `spec.{json,yaml,yml}`.
	if isUnderManuscripts(relSlash) && looksLikeVendorAPISpec(probe) {
		return nil, nil
	}

	var findings []PIIFinding
	lineNumber := 0
	for {
		line, readErr := reader.ReadString('\n')
		if readErr != nil && readErr != io.EOF {
			return nil, readErr
		}
		if line == "" && readErr == io.EOF {
			break
		}
		lineNumber++
		for _, det := range piiDetectors {
			for _, match := range det.pattern.FindAllStringIndex(line, -1) {
				matchedSpan := line[match[0]:match[1]]
				if isSyntheticPIIPlaceholder(det.kind, matchedSpan) {
					continue
				}
				findings = append(findings, PIIFinding{
					Kind:        det.kind,
					File:        relSlash,
					Line:        lineNumber,
					Column:      match[0] + 1, // 1-based
					MatchedSpan: matchedSpan,
				})
			}
		}
		if readErr == io.EOF {
			break
		}
	}
	return findings, nil
}

// PIIFindingID returns a stable 12-hex-char identifier for the finding.
// The hash composition is (file, line, kind, normalized matched span)
// — normalization collapses internal whitespace and lowercases so
// cosmetic formatting churn doesn't invalidate prior accepts. Line
// changes still force a fresh ID by design.
func PIIFindingID(f PIIFinding) string {
	key := piiFindingKey(f)
	sum := sha1.Sum([]byte(key))
	return hex.EncodeToString(sum[:6])
}

// piiFindingKey is the input to the SHA-1 hash. Kept stable across the
// codebase via this single helper.
func piiFindingKey(f PIIFinding) string {
	return fmt.Sprintf("%s\x00%d\x00%s\x00%s", f.File, f.Line, f.Kind, normalizePIISpan(f.MatchedSpan))
}

// normalizePIISpan lowercases and collapses internal whitespace.
// Tolerates cosmetic edits to the matched span without invalidating
// the finding ID — the actual character sequence (modulo whitespace
// and case) is what identifies the match.
func normalizePIISpan(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// PIILedger is the on-disk snapshot of the last PII audit run.
//
// FindingsCountBefore is sticky: captured on the first audit run that
// finds no existing ledger and preserved across subsequent runs. It
// anchors the all-accepted-no-fixes gate.
//
// Progress is an optional checkpoint the agent writes after walking
// each finding. A re-invocation after a context flush reads it to
// resume mid-walk. When absent, the resume hint is the first pending
// finding in scan order.
type PIILedger struct {
	Timestamp           time.Time    `json:"timestamp"`
	CLIDir              string       `json:"cli_dir"`
	Findings            []PIIFinding `json:"findings"`
	FindingsCountBefore int          `json:"findings_count_before"`
	Progress            *PIIProgress `json:"progress,omitempty"`
}

// PIIProgress is the agent-written resume checkpoint.
type PIIProgress struct {
	LastProcessedFindingID string `json:"last_processed_finding_id,omitempty"`
}

type PIILedgerDelta struct {
	HasPrevious bool
	Resolved    []PIIFinding // present in previous, absent in current (fixed in source)
	Added       []PIIFinding // present in current, absent in previous
}

// ReadPIILedger loads the ledger at <cliDir>/<PIILedgerFilename>.
// Returns nil for missing files. Corrupt files are deleted (the data
// is unrecoverable). Stale ledgers are returned with their content
// intact — auto-deletion on staleness silently destroyed accumulated
// agent accepts, so the caller checks IsStalePIILedger to emit a
// warning rather than erasing the ledger.
func ReadPIILedger(cliDir string) *PIILedger {
	path := filepath.Join(cliDir, PIILedgerFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var l PIILedger
	if err := json.Unmarshal(data, &l); err != nil {
		_ = os.Remove(path)
		return nil
	}
	return &l
}

// IsStalePIILedger reports whether a ledger's timestamp is older than
// the staleness window. Callers (audit, gates) decide what to do —
// typically warn but continue using the ledger so agent state is not
// lost on a slow-moving workflow.
func IsStalePIILedger(l *PIILedger) bool {
	if l == nil {
		return false
	}
	return time.Since(l.Timestamp) > PIILedgerStaleAfter
}

// WritePIILedger serializes the ledger and writes it atomically via
// temp file + rename. A crash mid-write leaves the previous ledger
// intact instead of producing a partial file that ReadPIILedger
// would silently delete, losing accumulated agent state.
func WritePIILedger(cliDir string, ledger *PIILedger) error {
	data, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding PII ledger: %w", err)
	}
	data = append(data, '\n')

	finalPath := filepath.Join(cliDir, PIILedgerFilename)
	tmpFile, err := os.CreateTemp(cliDir, PIILedgerFilename+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp PII ledger: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("writing temp PII ledger: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("syncing temp PII ledger: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("closing temp PII ledger: %w", err)
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming PII ledger: %w", err)
	}
	return nil
}

// ReconcilePIILedger carries Status/Note/Category/EvidenceContext from
// the previous ledger onto matching current findings and computes the
// resolved/added delta in a single pass. Identity is the piiFindingKey
// (file + line + kind + normalized-span); a finding whose matched
// span was rewritten in source reads as "old resolved, new added"
// rather than mutated.
//
// Mutates `current` in place to backfill agent fields.
func ReconcilePIILedger(previous *PIILedger, current []PIIFinding) PIILedgerDelta {
	if previous == nil {
		return PIILedgerDelta{}
	}
	prev := make(map[string]PIIFinding, len(previous.Findings))
	for _, f := range previous.Findings {
		prev[piiFindingKey(f)] = f
	}
	delta := PIILedgerDelta{HasPrevious: true}
	seen := make(map[string]bool, len(current))
	for i := range current {
		k := piiFindingKey(current[i])
		seen[k] = true
		if old, ok := prev[k]; ok {
			current[i].Status = old.Status
			current[i].Note = old.Note
			current[i].Category = old.Category
			current[i].EvidenceContext = old.EvidenceContext
		} else {
			delta.Added = append(delta.Added, current[i])
		}
	}
	for k, f := range prev {
		if !seen[k] {
			delta.Resolved = append(delta.Resolved, f)
		}
	}
	return delta
}

// Gate fields all empty + no pending findings means the run is complete.
type PIICompletionStatus struct {
	IncompleteAccepts        []PIIFinding         // accepts that fail missingPIIAcceptFields — see that helper for the full predicate
	DuplicateRationaleGroups []PIIRationaleGroup  // accepts sharing a normalized note
	AllAcceptedNoFixes       *PIIAllAcceptedIssue // every finding accepted, zero fixes from baseline
	NextPending              *PIIFinding          // resume hint
}

type PIIRationaleGroup struct {
	Rationale string
	Findings  []PIIFinding
}

type PIIAllAcceptedIssue struct {
	Baseline int
	Current  int
	Accepted int
}

func EvaluatePIICompletion(findings []PIIFinding, previous *PIILedger) PIICompletionStatus {
	var c PIICompletionStatus
	for _, f := range findings {
		if f.Status != PIIStatusAccepted {
			continue
		}
		if missingPIIAcceptFields(f) {
			c.IncompleteAccepts = append(c.IncompleteAccepts, f)
		}
	}
	c.DuplicateRationaleGroups = detectPIIDuplicateRationales(findings, PIIDuplicateRationaleThreshold)
	c.AllAcceptedNoFixes = checkPIIAllAcceptedNoFixes(findings, previous)
	c.NextPending = nextPIIPendingFinding(findings, previous)
	return c
}

// missingPIIAcceptFields returns true when an accepted finding is
// missing the required pre-decision fields. Either field can fail:
// empty category, invalid category, or empty evidence_context.
// Note (free-form) is optional unless category is "other".
func missingPIIAcceptFields(f PIIFinding) bool {
	if strings.TrimSpace(f.Category) == "" {
		return true
	}
	if !validPIICategories[f.Category] {
		return true
	}
	if strings.TrimSpace(f.EvidenceContext) == "" {
		return true
	}
	if f.Category == PIICategoryOther && strings.TrimSpace(f.Note) == "" {
		return true
	}
	return false
}

// detectPIIDuplicateRationales clusters accepts by (category, normalized
// note). The category split prevents legitimate overlap (5 accepts as
// place_name is fine; 6 with identical free-form note in other is the
// punt pattern). Returns groups exceeding threshold.
func detectPIIDuplicateRationales(findings []PIIFinding, threshold int) []PIIRationaleGroup {
	if threshold <= 0 {
		return nil
	}
	type rationaleKey struct{ category, note string }
	clusters := make(map[rationaleKey][]PIIFinding)
	for _, f := range findings {
		if f.Status != PIIStatusAccepted {
			continue
		}
		note := normalizePIISpan(f.Note)
		if note == "" {
			continue
		}
		clusters[rationaleKey{f.Category, note}] = append(clusters[rationaleKey{f.Category, note}], f)
	}
	var groups []PIIRationaleGroup
	for _, fs := range clusters {
		if len(fs) > threshold {
			groups = append(groups, PIIRationaleGroup{
				Rationale: fs[0].Note,
				Findings:  fs,
			})
		}
	}
	sort.Slice(groups, func(i, j int) bool {
		return len(groups[i].Findings) > len(groups[j].Findings)
	})
	return groups
}

// checkPIIAllAcceptedNoFixes fires when the agent accepted every
// finding without doing any source-level work, at a scale that
// indicates a punt. Trigger condition:
//   - Current finding count is at or above the threshold (large
//     batches deserve scrutiny; small batches can legitimately be
//     accepted wholesale).
//   - Every finding is accepted (zero pending).
//   - Either no prior baseline exists, or the baseline matches the
//     current count (no findings were removed in source).
//
// This formulation closes the incremental-add bypass: previously, when
// new findings appeared after the sticky baseline, the gate compared
// current vs baseline (now unequal) and never fired. Now the test is
// "did source-level fixes occur" — measured as current count below
// baseline — which is the actual punt signal.
func checkPIIAllAcceptedNoFixes(findings []PIIFinding, previous *PIILedger) *PIIAllAcceptedIssue {
	current := len(findings)
	if current < PIIAllAcceptedNoFixesThreshold {
		return nil
	}
	accepted := 0
	for _, f := range findings {
		if f.Status == PIIStatusAccepted {
			accepted++
		}
	}
	if accepted != current {
		return nil
	}
	baseline := 0
	if previous != nil {
		baseline = previous.FindingsCountBefore
	}
	// If baseline exists and current is below it, the agent removed
	// findings in source (legitimate work). Gate does not fire.
	if baseline > 0 && current < baseline {
		return nil
	}
	return &PIIAllAcceptedIssue{
		Baseline: baseline,
		Current:  current,
		Accepted: accepted,
	}
}

// isPIIPending reports whether a finding still needs agent attention.
// Status unset → pending. Status accepted but missing pre-decision
// fields → pending (incomplete accept).
func isPIIPending(f PIIFinding) bool {
	if f.Status != PIIStatusAccepted {
		return true
	}
	return missingPIIAcceptFields(f)
}

// nextPIIPendingFinding returns the resume hint. The previous ledger's
// Progress.LastProcessedFindingID is read as a soft hint: when present,
// the search starts after that finding. Falls back to head-scan when
// exhausted or absent.
func nextPIIPendingFinding(findings []PIIFinding, previous *PIILedger) *PIIFinding {
	startIdx := 0
	if previous != nil && previous.Progress != nil && previous.Progress.LastProcessedFindingID != "" {
		for i, f := range findings {
			if PIIFindingID(f) == previous.Progress.LastProcessedFindingID {
				startIdx = i + 1
				break
			}
		}
	}
	if f := firstPIIPending(findings, startIdx, len(findings)); f != nil {
		return f
	}
	return firstPIIPending(findings, 0, startIdx)
}

func firstPIIPending(findings []PIIFinding, lo, hi int) *PIIFinding {
	for i := lo; i < hi; i++ {
		if isPIIPending(findings[i]) {
			f := findings[i]
			return &f
		}
	}
	return nil
}

// GateFailureCount returns the number of gates surfacing an issue the
// agent must act on. NextPending is a resume hint, not a gate, and is
// excluded.
func (c PIICompletionStatus) GateFailureCount() int {
	n := 0
	if len(c.IncompleteAccepts) > 0 {
		n++
	}
	if len(c.DuplicateRationaleGroups) > 0 {
		n++
	}
	if c.AllAcceptedNoFixes != nil {
		n++
	}
	return n
}

// HasGateFailure is the boolean form of GateFailureCount. Both are used:
// HasGateFailure for branching, GateFailureCount for the integer in
// error messages.
func (c PIICompletionStatus) HasGateFailure() bool {
	return c.GateFailureCount() > 0
}

type PIIAuditResult struct {
	Findings   []PIIFinding
	Delta      PIILedgerDelta
	Completion PIICompletionStatus
}

// RunPIIAudit performs a full audit cycle against dir: scan with all
// detectors, reconcile with prior ledger (carrying agent state forward),
// write the new ledger, and evaluate enforcement primitives. Shared by
// the pii-audit subcommand (non-JSON path) and the promote/publish gates.
//
// The ledger write is best-effort — if it fails (read-only directory,
// disk full), the audit result is still returned and a warning is
// logged to stderr. The gate decision uses the in-memory result.
func RunPIIAudit(dir string) (PIIAuditResult, error) {
	return runPIIAudit(dir, true)
}

// ScanPII performs the audit without writing the ledger. The pii-audit
// CLI's --json path uses this so a read-only probe doesn't have the
// side effect of touching the filesystem.
func ScanPII(dir string) (PIIAuditResult, error) {
	return runPIIAudit(dir, false)
}

func runPIIAudit(dir string, persist bool) (PIIAuditResult, error) {
	findings, err := FindPII(dir)
	if err != nil {
		return PIIAuditResult{}, fmt.Errorf("scanning %s for PII: %w", dir, err)
	}
	previous := ReadPIILedger(dir)
	delta := ReconcilePIILedger(previous, findings)

	if persist {
		ledger := &PIILedger{
			Timestamp: time.Now().UTC(),
			CLIDir:    dir,
			Findings:  findings,
		}
		if previous != nil {
			ledger.FindingsCountBefore = previous.FindingsCountBefore
		} else {
			ledger.FindingsCountBefore = len(findings)
		}
		if previous != nil && previous.Progress != nil {
			ledger.Progress = previous.Progress
		}
		if writeErr := WritePIILedger(dir, ledger); writeErr != nil {
			fmt.Fprintf(os.Stderr, "warning: writing PII ledger: %v\n", writeErr)
		}
	}

	return PIIAuditResult{
		Findings:   findings,
		Delta:      delta,
		Completion: EvaluatePIICompletion(findings, previous),
	}, nil
}

func PIIPendingCount(findings []PIIFinding) int {
	n := 0
	for _, f := range findings {
		if isPIIPending(f) {
			n++
		}
	}
	return n
}

func FormatPIIFindings(findings []PIIFinding) string {
	var lines []string
	for _, f := range findings {
		if !isPIIPending(f) {
			continue
		}
		lines = append(lines, fmt.Sprintf("  %s:%d %s [%s] %s",
			f.File, f.Line, f.Kind, PIIFindingID(f), truncatePIIMatch(f.MatchedSpan, 60)))
	}
	return strings.Join(lines, "\n")
}

func FormatPIIGateFailures(c PIICompletionStatus) string {
	if !c.HasGateFailure() {
		return ""
	}
	var lines []string
	if len(c.IncompleteAccepts) > 0 {
		lines = append(lines, fmt.Sprintf("  %d accepted finding(s) with missing or invalid pre-decision fields (category, evidence_context, or note when category=other):",
			len(c.IncompleteAccepts)))
		for _, f := range c.IncompleteAccepts {
			lines = append(lines, fmt.Sprintf("    %s:%d %s [%s]",
				f.File, f.Line, f.Kind, PIIFindingID(f)))
		}
	}
	for _, g := range c.DuplicateRationaleGroups {
		lines = append(lines, fmt.Sprintf("  %d accepted finding(s) share rationale %q — differentiate per item:",
			len(g.Findings), truncatePIIMatch(g.Rationale, 60)))
		for _, f := range g.Findings {
			lines = append(lines, fmt.Sprintf("    %s:%d %s [%s]",
				f.File, f.Line, f.Kind, PIIFindingID(f)))
		}
	}
	if d := c.AllAcceptedNoFixes; d != nil {
		lines = append(lines, fmt.Sprintf("  all %d finding(s) accepted with zero source fixes from baseline — agent stamped accept without fixing real PII",
			d.Accepted))
	}
	return strings.Join(lines, "\n")
}

// truncatePIIMatch walks runes (not bytes) so multibyte characters in
// matched spans (unicode email domains, addresses with diacritics) are
// preserved at the boundary. Mirrors internal/cli/mcp_audit.go's
// truncate; the package boundary blocks direct reuse.
func truncatePIIMatch(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 1 {
		return string(runes[:n])
	}
	return string(runes[:n-1]) + "…"
}
