package pipeline

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ReimplementationCheckResult flags novel-feature commands whose handler
// files show no sign of calling the generated API client and no sign of
// consulting the local store. Those are the two legitimate data sources
// for a printed CLI; a novel feature that uses neither is synthesizing
// behavior - a hand-rolled response, a constant return, or an empty
// stub that pretends to do work.
//
// The check is structural, not semantic. It does not parse Go or try to
// understand what the function returns. It looks at the file that
// implements the command and asks: does any part of this file call
// through the generated client package, or read from the generated
// store package? If neither, the command is flagged.
//
// SQLite-derived commands (stale, bottleneck, health, reconcile) pass
// this check because their files call `store.Open` and consult the
// store package. That is correctly a local-data command, not a
// hand-rolled response.
//
// Raw `database/sql` access against the local SQLite file is also a
// legitimate local-data signal: a file that imports `database/sql`
// AND calls `sql.Open`/`sql.OpenDB` is reading the same data the
// store package wraps through a thinner surface.
type ReimplementationCheckResult struct {
	// Checked is the number of built novel-feature commands inspected.
	Checked int `json:"checked"`
	// ExemptedViaStore is the number of commands that passed the check
	// by consulting the local store package (SQLite-derived features).
	ExemptedViaStore int `json:"exempted_via_store"`
	// ExemptedViaAnnotation is the number of commands that passed the
	// check via the // pp:novel-static-reference marker (curated
	// static-data features like substitution tables, holiday lists).
	// Tracked separately from ExemptedViaStore so analytics over
	// dogfood-results.json can distinguish the two carve-out classes
	// even though they share the same ship/no-ship decision.
	ExemptedViaAnnotation int `json:"exempted_via_annotation,omitempty"`
	// Suspicious is the list of commands whose files show no client
	// call and no store access - the candidate hand-rolled responses.
	Suspicious []ReimplementationFinding `json:"suspicious,omitempty"`
	// Skipped is true when the check could not run (no research dir, no
	// novel features, no matchable files).
	Skipped bool `json:"skipped,omitempty"`
}

// ReimplementationFinding names a single suspicious command and gives
// the reviewer enough context to act: the command as planned, the file
// that implements it, and the specific reason it was flagged.
type ReimplementationFinding struct {
	Command string `json:"command"`
	File    string `json:"file"`
	Reason  string `json:"reason"`
}

// These signals are intentionally string-match and regex based. AST
// parsing would be more precise but adds scope and dependency weight
// this check does not need at v1. If false-positive pressure grows,
// we upgrade to AST in a follow-up.

var (
	// storeImportRe catches the generated store package import in any
	// printed CLI: `"<module>/internal/store"`. The module prefix varies
	// per CLI, so we anchor on the shared trailing path segment.
	storeImportRe = regexp.MustCompile(`"[^"]*/internal/store"`)

	// storeCallRe catches direct calls into the store package - the most
	// common shape is `store.Open(...)`. Agent-authored commands that
	// read sync'd data consistently use this entry point.
	storeCallRe = regexp.MustCompile(`\bstore\.[A-Z]\w*\s*\(`)

	// storeTypeRe catches helpers that accept or return the generated
	// store type even if the actual store call happens through another
	// helper.
	storeTypeRe = regexp.MustCompile(`\b\*?store\.Store\b`)

	// rawSQLImportRe catches the standard library database/sql import.
	// The import alone is not a strong signal — it can be present for
	// unrelated reasons — so hasStoreSignal pairs it with rawSQLOpenCallRe.
	rawSQLImportRe = regexp.MustCompile(`"database/sql"`)

	// rawSQLOpenCallRe catches the canonical sql.Open / sql.OpenDB
	// entry points. Method calls like db.Query are intentionally NOT
	// matched: names like Query, QueryRow, Exec collide with too many
	// other receivers (HTTP clients, cobra commands) to be a clean
	// signal on their own.
	rawSQLOpenCallRe = regexp.MustCompile(`\bsql\.(Open|OpenDB)\s*\(`)

	// clientImportRe catches the generated client package import:
	// `"<module>/internal/client"`. Not every client call requires this
	// (the command can go through `flags.newClient`), but its presence
	// is a reliable positive signal.
	clientImportRe = regexp.MustCompile(`"[^"]*/internal/client"`)

	// clientCallRe catches the canonical API-call entry points used by
	// generated endpoint commands and by well-behaved novel features:
	// `flags.newClient()` and direct `http.Get/Post/Do` calls. Commands
	// that build their own raw http.Request also land here.
	clientCallRe = regexp.MustCompile(`\b(flags\.newClient\s*\(|http\.(Get|Post|NewRequest|Do)\s*\(|c\.Do\s*\(|c\.Get\s*\(|c\.Post\s*\()`)

	// siblingInternalImportRe catches any import of a package under
	// `internal/<name>`. Go's RE2 has no negative lookahead, so the
	// regex captures all matches and the surrounding code filters out
	// the generator-reserved set (see hasSiblingInternalImport).
	//
	// Why we care: any package alongside the generated `client`,
	// `store`, `cliutil`, etc. is almost certainly a hand-built API
	// client (think `internal/algolia` for a CLI that fronts both a
	// primary and a secondary API). Calls into such packages are
	// legitimate API access; the pre-existing regex set didn't
	// recognize them, so dogfood was producing false-positive
	// reimplementation findings on every multi-source CLI.
	//
	// False positives from this signal (a non-client utility package
	// mistakenly recognized as a client) are strictly less bad than
	// the false negatives we get without it (a real Algolia client
	// flagged as reimplementation).
	//
	// Surfaced by hackernews retro #350 finding F4.
	siblingInternalImportRe = regexp.MustCompile(`"[^"]*/internal/([a-z][a-z0-9_]*)"`)

	// trivialBodyRe catches the classic empty-stub shape used when an
	// agent wires a Cobra command but never implements it:
	//
	//   RunE: func(cmd *cobra.Command, args []string) error { return nil }
	//
	// with optional whitespace variations. If the command's handler body
	// is only this, no other signal is going to save it.
	trivialBodyRe = regexp.MustCompile(`RunE:\s*func\s*\(\s*cmd\s*\*cobra\.Command\s*,\s*args\s*\[\]string\s*\)\s*error\s*\{\s*return\s+nil\s*\}`)
)

// checkReimplementation scans the files that implement built novel
// features and classifies each. A command whose file calls the store
// package is exempt. A command whose file calls the client is fine. A
// command whose file does neither - or whose handler is a trivial stub
// - is flagged for review.
//
// When researchDir is empty or research.json has no novel features the
// check returns Skipped. This mirrors the behavior of checkNovelFeatures:
// if there is nothing planned, there is nothing to validate.
func checkReimplementation(cliDir, researchDir string) ReimplementationCheckResult {
	if researchDir == "" {
		return ReimplementationCheckResult{Skipped: true}
	}
	research, err := LoadResearch(researchDir)
	if err != nil || len(research.NovelFeatures) == 0 {
		return ReimplementationCheckResult{Skipped: true}
	}

	cliFilesDir := filepath.Join(cliDir, "internal", "cli")
	entries, err := os.ReadDir(cliFilesDir)
	if err != nil {
		return ReimplementationCheckResult{Skipped: true}
	}

	// Build a quick index: leaf command name -> candidate file paths.
	// A file is a candidate for a command if it contains `Use: "<leaf>"`.
	// We only index non-infrastructure, non-test source files.
	leafToFiles := map[string][]string{}
	fileContent := map[string]string{}
	infra := map[string]bool{
		"helpers.go": true,
		"root.go":    true,
		"doctor.go":  true,
		"auth.go":    true,
	}
	useLineRe := regexp.MustCompile(`Use:\s*"([^"\s]+)`)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		if infra[name] {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(cliFilesDir, name))
		if readErr != nil {
			continue
		}
		content := string(data)
		fileContent[name] = content
		for _, m := range useLineRe.FindAllStringSubmatch(content, -1) {
			leaf := m[1]
			leafToFiles[leaf] = append(leafToFiles[leaf], name)
		}
	}

	result := ReimplementationCheckResult{}
	storeHelpers := storeHelperNames(fileContent)
	for _, nf := range research.NovelFeatures {
		leaf := lastPathSegment(commandPath(nf.Command))
		if leaf == "" {
			continue
		}
		files := leafToFiles[leaf]
		if len(files) == 0 {
			// No file owns this command leaf. checkNovelFeatures already
			// reports this as Missing; no double-count here.
			continue
		}
		// When a leaf maps to multiple files (rare), inspect all of them
		// and take the most favorable classification - any single file
		// with the right signals vindicates the command.
		result.Checked++
		finding, kind, ok := classifyReimplementation(files, fileContent, storeHelpers)
		switch kind {
		case exemptStore:
			result.ExemptedViaStore++
			continue
		case exemptAnnotation:
			result.ExemptedViaAnnotation++
			continue
		}
		if !ok {
			finding.Command = nf.Command
			result.Suspicious = append(result.Suspicious, finding)
		}
	}

	if result.Checked == 0 {
		result.Skipped = true
	}

	return result
}

// novelStaticReferenceRe matches the per-command opt-out marker
// documented in AGENTS.md. A line of the form
//
//	// pp:novel-static-reference
//
// (any leading whitespace, optional " " before the directive) anywhere
// in a command's source file declares that the command intentionally
// ships curated static data — substitution tables, holiday lists,
// currency metadata, conversion factors — rather than calling an API
// or reading from the local store. The reimplementation check honors
// the marker and exempts the command, treating it on the same footing
// as the existing store/client carve-outs.
//
// Added for retro #301 finding F3.
var novelStaticReferenceRe = regexp.MustCompile(`(?m)^\s*//\s*pp:novel-static-reference\b`)

// exemptionKind labels which carve-out vindicated a command, so the
// caller can route the bump to the right counter on
// ReimplementationCheckResult. exemptNone covers both "passes via
// client signal" (ok=true, kind=exemptNone) and "is suspicious"
// (ok=false, kind=exemptNone) — the kind only carries meaning when
// the result is exempt.
type exemptionKind int

const (
	exemptNone exemptionKind = iota
	exemptStore
	exemptAnnotation
)

// classifyReimplementation returns the best classification across the
// set of files that implement a single command. The rules, in order:
//
//  1. If any file carries the `// pp:novel-static-reference` marker,
//     the command is exempted as an intentional static-data feature.
//     Return (_, exemptAnnotation, true).
//  2. If any file shows a store signal, the command is exempted as a
//     local-SQLite feature. Return (_, exemptStore, true).
//  3. If any file shows a client signal, the command is fine. Return
//     (_, exemptNone, true).
//  4. Otherwise the command is suspicious. Return a ReimplementationFinding
//     naming the primary file and a reason. Return (finding, exemptNone, false).
//
// The trivial-body regex is consulted only when rule 4 fires, to pick
// between "empty stub" and "hand-rolled response" as the reason.
func classifyReimplementation(files []string, fileContent map[string]string, storeHelpers map[string]bool) (ReimplementationFinding, exemptionKind, bool) {
	hasClient := false
	hasTrivialBody := false
	primaryFile := files[0]
	for _, f := range files {
		content, ok := fileContent[f]
		if !ok {
			continue
		}
		if novelStaticReferenceRe.MatchString(content) {
			return ReimplementationFinding{File: f}, exemptAnnotation, true
		}
		if hasStoreSignal(content) {
			return ReimplementationFinding{File: f}, exemptStore, true
		}
		if callsStoreHelper(content, storeHelpers) {
			return ReimplementationFinding{File: f}, exemptStore, true
		}
		if hasClientSignal(content) {
			hasClient = true
		}
		if trivialBodyRe.MatchString(content) {
			hasTrivialBody = true
		}
	}
	if hasClient {
		return ReimplementationFinding{File: primaryFile}, exemptNone, true
	}
	reason := "hand-rolled response: no API client call, no store access"
	if hasTrivialBody {
		reason = "empty body: no implementation"
	}
	return ReimplementationFinding{File: primaryFile, Reason: reason}, exemptNone, false
}

func hasStoreSignal(content string) bool {
	return storeImportRe.MatchString(content) ||
		storeCallRe.MatchString(content) ||
		(rawSQLImportRe.MatchString(content) && rawSQLOpenCallRe.MatchString(content))
}

func storeHelperNames(fileContent map[string]string) map[string]bool {
	helpers := map[string]bool{}
	for _, content := range fileContent {
		if !hasStoreSignal(content) {
			continue
		}
		collectStoreHelpers(content, helpers)
	}
	return helpers
}

func collectStoreHelpers(content string, helpers map[string]bool) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", content, 0)
	if err != nil {
		return
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil {
			continue
		}
		start := fset.Position(fn.Pos()).Offset
		end := fset.Position(fn.End()).Offset
		if start < 0 || end > len(content) || start >= end {
			continue
		}
		funcText := content[start:end]
		if storeCallRe.MatchString(funcText) || storeTypeRe.MatchString(funcText) {
			helpers[fn.Name.Name] = true
		}
	}
}

func callsStoreHelper(content string, helpers map[string]bool) bool {
	for name := range helpers {
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*\(`).MatchString(content) {
			return true
		}
	}
	return false
}

func hasClientSignal(content string) bool {
	return clientImportRe.MatchString(content) ||
		clientCallRe.MatchString(content) ||
		hasSiblingInternalImport(content)
}

// hasSiblingInternalImport reports whether the file imports a non-reserved
// `internal/<name>` package — the signal for a hand-built secondary
// client. The regex matches all internal imports; we filter the
// reserved set in code because Go's RE2 has no negative lookahead.
func hasSiblingInternalImport(content string) bool {
	matches := siblingInternalImportRe.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		if !reservedInternalPackages[m[1]] {
			return true
		}
	}
	return false
}

func lastPathSegment(path string) string {
	_, leaf := splitCommandPath(path)
	if leaf != "" {
		return leaf
	}
	return path
}
