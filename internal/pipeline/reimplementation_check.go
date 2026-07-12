package pipeline

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ReimplementationCheckResult flags novel-feature commands whose handler
// files show no sign of calling the generated API client and no sign of
// consulting the local store. Those are the two legitimate data sources
// for a printed CLI; a novel feature that uses neither is synthesizing
// behavior - a hand-rolled response, a constant return, or an empty
// stub that pretends to do work.
//
// The check is structural, not semantic. It looks at the file that
// implements the command and asks: does any part of this file call
// through the generated client package, read from the generated store package,
// or call a package-local helper that reaches the store or API client? If none do, the
// command is flagged. Primitive client/store signals stay regex-based; helper
// discovery uses Go AST parsing so declarations and comments do not look like
// real helper calls.
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
	// ExemptedViaClientDirective is the number of commands that passed
	// via the // pp:client-call marker. This marker is a positive assertion
	// that the command reaches a real API through a wrapper the heuristic
	// cannot see. Tracked separately from static-reference annotations so
	// dogfood analytics can distinguish "real API through abstraction" from
	// "curated static data."
	ExemptedViaClientDirective int `json:"exempted_via_client_directive,omitempty"`
	// ExemptedViaComputedDataSource is the number of commands that passed
	// via // pp:data-source computed. These commands intentionally compute
	// from embedded policy/math tables instead of calling an API or store.
	ExemptedViaComputedDataSource int `json:"exempted_via_computed_data_source,omitempty"`
	// ExemptedViaLocalDataSource is the number of commands that passed
	// via // pp:data-source local without a store package signal. These
	// commands intentionally read local files or other local-only state.
	ExemptedViaLocalDataSource int `json:"exempted_via_local_data_source,omitempty"`
	// Suspicious is the list of commands whose files show no client
	// call and no store access - the candidate hand-rolled responses.
	Suspicious []ReimplementationFinding `json:"suspicious,omitempty"`
	// MissingDataSourceStrategy is the list of hand-written novel-feature
	// commands that do not declare // pp:data-source <auto|local|live|computed>.
	MissingDataSourceStrategy []ReimplementationFinding `json:"missing_data_source_strategy,omitempty"`
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

// The primitive store/client signals stay regex-based because they are broad
// file-level probes. Helper discovery uses Go ASTs so declarations and comments
// do not look like real helper calls.

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

	todoStubRe = regexp.MustCompile(`(?i)\bTODO:\s*implement\s+novel\s+feature\b`)
)

// checkReimplementation scans the files that implement built novel
// features and classifies each via classifyReimplementation's ordered
// signal rules.
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
	helperContent := map[string]string{}
	infra := map[string]bool{
		"helpers.go": true,
		"root.go":    true,
		"doctor.go":  true,
		"auth.go":    true,
	}
	useLineRe := cobraUseLeafRe
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") {
			continue
		}
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(cliFilesDir, name))
		if readErr != nil {
			continue
		}
		content := string(data)
		helperContent[name] = content
		if infra[name] {
			continue
		}
		fileContent[name] = content
		for _, m := range useLineRe.FindAllStringSubmatch(content, -1) {
			leaf := m[1]
			leafToFiles[leaf] = append(leafToFiles[leaf], name)
		}
	}

	result := ReimplementationCheckResult{}
	storeHelpers := storeHelperNames(helperContent)
	clientHelpers := clientHelperNames(helperContent)
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
		if finding, ok := dataSourceStrategyFinding(files, fileContent); ok {
			finding.Command = nf.Command
			result.MissingDataSourceStrategy = append(result.MissingDataSourceStrategy, finding)
		}
		finding, kind, ok := classifyReimplementation(leaf, files, fileContent, storeHelpers, clientHelpers)
		switch kind {
		case exemptStore:
			result.ExemptedViaStore++
			continue
		case exemptAnnotation:
			result.ExemptedViaAnnotation++
			continue
		case exemptClientDirective:
			result.ExemptedViaClientDirective++
			continue
		case exemptComputedDataSource:
			result.ExemptedViaComputedDataSource++
			continue
		case exemptLocalDataSource:
			result.ExemptedViaLocalDataSource++
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

// clientCallDirectiveRe matches the positive assertion marker for command
// files that reach a real API through a wrapper the string heuristics cannot
// see. Unlike pp:novel-static-reference, this is not a carve-out for static
// data; it says "the API call exists, but not in a shape this checker can
// verify mechanically."
var clientCallDirectiveRe = regexp.MustCompile(`(?m)^\s*//\s*pp:client-call\b`)

var dataSourceDirectiveRe = regexp.MustCompile(`(?m)^\s*//\s*pp:data-source\s+([a-zA-Z_-]+)\b`)

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
	exemptClientDirective
	exemptComputedDataSource
	exemptLocalDataSource
)

// classifyReimplementation returns the best classification across the
// set of files that implement a single command. The rules, in order:
//
//  1. If any file carries the `// pp:novel-static-reference` marker,
//     the command is exempted as an intentional static-data feature.
//     Return (_, exemptAnnotation, true).
//  2. If any file carries the `// pp:client-call` marker, the command
//     is exempted as a real API call hidden behind an abstraction.
//     Return (_, exemptClientDirective, true).
//  3. If any file declares // pp:data-source local and does not look
//     like a TODO stub, the command is accepted as intentionally local.
//  4. If any file declares // pp:data-source computed and does not look
//     like a TODO stub, the command is exempted as intentional pure
//     computation.
//  5. If any file shows a store signal, the command is exempted as a
//     local-SQLite feature. Return (_, exemptStore, true).
//  6. If any file shows a client signal, the command is fine. Return
//     (_, exemptNone, true).
//  7. Otherwise the command is suspicious. Return a ReimplementationFinding
//     naming the primary file and a reason. Return (finding, exemptNone, false).
//
// The trivial-body regex is consulted only when rule 6 fires, to pick
// between "empty stub" and "hand-rolled response" as the reason.
func classifyReimplementation(leaf string, files []string, fileContent map[string]string, storeHelpers, clientHelpers map[string]bool) (ReimplementationFinding, exemptionKind, bool) {
	hasClient := false
	hasTrivialBody := false
	hasTODOStub := false
	primaryFile := files[0]
	hasAnyTODOStub := false
	for _, f := range files {
		content, ok := fileContent[f]
		if ok && todoStubRe.MatchString(content) {
			hasAnyTODOStub = true
			break
		}
	}
	for _, f := range files {
		content, ok := fileContent[f]
		if !ok {
			continue
		}
		if novelStaticReferenceRe.MatchString(content) {
			return ReimplementationFinding{File: f}, exemptAnnotation, true
		}
		if clientCallDirectiveRe.MatchString(content) {
			return ReimplementationFinding{File: f}, exemptClientDirective, true
		}
		if computedDataSource(content) && !hasAnyTODOStub {
			return ReimplementationFinding{File: f}, exemptComputedDataSource, true
		}
		if hasStoreSignal(content) {
			return ReimplementationFinding{File: f}, exemptStore, true
		}
		if callsStoreHelper(content, storeHelpers) {
			return ReimplementationFinding{File: f}, exemptStore, true
		}
		if localDataSource(content) && !hasAnyTODOStub {
			return ReimplementationFinding{File: f}, exemptLocalDataSource, true
		}
		commandScan := scanCommandHandler(content, leaf)
		clientScanContent := content
		if commandScan.ok {
			clientScanContent = commandScan.body
		}
		if callsClientHelper(clientScanContent, clientHelpers) {
			hasClient = true
		}
		if commandScan.ok && hasBlockClientSignal(commandScan.bodyNode, commandScan.imports, true) {
			hasClient = true
		} else if !commandScan.ok && hasClientSignal(content) {
			hasClient = true
		}
		if trivialBodyRe.MatchString(content) {
			hasTrivialBody = true
		}
		if todoStubRe.MatchString(content) {
			hasTODOStub = true
			hasTrivialBody = true
		}
	}
	if hasClient {
		return ReimplementationFinding{File: primaryFile}, exemptNone, true
	}
	reason := "hand-rolled response: no API client call, no store access"
	if hasTODOStub {
		reason = "TODO stub: no implementation"
	} else if hasTrivialBody {
		reason = "empty body: no implementation"
	}
	return ReimplementationFinding{File: primaryFile, Reason: reason}, exemptNone, false
}

func dataSourceStrategyFinding(files []string, fileContent map[string]string) (ReimplementationFinding, bool) {
	var firstMissing string
	for _, f := range files {
		content, ok := fileContent[f]
		if !ok || isGeneratedPrintingPressFile(content) {
			continue
		}
		m := dataSourceDirectiveRe.FindStringSubmatch(content)
		if len(m) == 0 {
			if firstMissing == "" {
				firstMissing = f
			}
			continue
		}
		switch strings.ToLower(strings.TrimSpace(m[1])) {
		case "auto", "local", "live", "computed":
			return ReimplementationFinding{}, false
		default:
			return ReimplementationFinding{
				File:   f,
				Reason: "invalid // pp:data-source annotation: must be auto, local, live, or computed",
			}, true
		}
	}
	if firstMissing != "" {
		return ReimplementationFinding{
			File:   firstMissing,
			Reason: "missing // pp:data-source <auto|local|live|computed> annotation",
		}, true
	}
	return ReimplementationFinding{}, false
}

func computedDataSource(content string) bool {
	m := dataSourceDirectiveRe.FindStringSubmatch(content)
	return len(m) > 1 && strings.EqualFold(strings.TrimSpace(m[1]), "computed")
}

func localDataSource(content string) bool {
	m := dataSourceDirectiveRe.FindStringSubmatch(content)
	return len(m) > 1 && strings.EqualFold(strings.TrimSpace(m[1]), "local")
}

func isGeneratedPrintingPressFile(content string) bool {
	return strings.Contains(content, "Generated by CLI Printing Press")
}

func hasStoreSignal(content string) bool {
	return storeImportRe.MatchString(content) ||
		storeCallRe.MatchString(content) ||
		(rawSQLImportRe.MatchString(content) && rawSQLOpenCallRe.MatchString(content))
}

func storeHelperNames(fileContent map[string]string) map[string]bool {
	helpers := map[string]bool{}
	funcContent := map[string]string{}
	for _, content := range fileContent {
		forEachGoFuncContent(content, func(name, body string) {
			funcContent[name] = body
			if hasStoreSignal(body) || storeTypeRe.MatchString(body) {
				helpers[name] = true
			}
		})
	}
	for {
		changed := false
		for name, body := range funcContent {
			if helpers[name] {
				continue
			}
			if callsStoreHelper(body, helpers) {
				helpers[name] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return helpers
}

type clientImportKind int

const (
	generatedClientImport clientImportKind = iota + 1
	siblingClientImport
)

func clientHelperNames(fileContent map[string]string) map[string]bool {
	helpers := map[string]bool{}
	for _, content := range fileContent {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "", content, 0)
		if err != nil {
			continue
		}
		imports := clientImportAliases(file)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name == nil || fn.Recv != nil || fn.Body == nil {
				continue
			}
			// Client helper discovery is intentionally one hop: command -> helper
			// -> client. Deeper chains use the pp:client-call escape hatch.
			if hasFunctionClientSignal(fn, imports) {
				helpers[fn.Name.Name] = true
			}
		}
	}
	return helpers
}

func clientImportAliases(file *ast.File) map[string]clientImportKind {
	aliases := map[string]clientImportKind{}
	for _, spec := range file.Imports {
		importPath := strings.Trim(spec.Path.Value, "`\"")
		if importPath == "" {
			continue
		}
		internalName, isInternal := internalPackageName(importPath)
		kind := clientImportKind(0)
		switch {
		case strings.HasSuffix(importPath, "/internal/client"):
			kind = generatedClientImport
		case isInternal && !reservedInternalPackages[internalName]:
			kind = siblingClientImport
		default:
			continue
		}
		alias := importAlias(spec, importPath)
		if alias != "" {
			aliases[alias] = kind
		}
	}
	return aliases
}

func internalPackageName(importPath string) (string, bool) {
	_, rest, ok := strings.Cut(importPath, "/internal/")
	if !ok {
		return "", false
	}
	if rest == "" {
		return "", false
	}
	name, _, _ := strings.Cut(rest, "/")
	return name, name != ""
}

func importAlias(spec *ast.ImportSpec, importPath string) string {
	if spec.Name != nil {
		switch spec.Name.Name {
		case ".", "_":
			return ""
		default:
			return spec.Name.Name
		}
	}
	if idx := strings.LastIndex(importPath, "/"); idx >= 0 {
		return importPath[idx+1:]
	}
	return importPath
}

func hasFunctionClientSignal(fn *ast.FuncDecl, imports map[string]clientImportKind) bool {
	return hasBlockClientSignal(fn.Body, imports, false)
}

func hasBlockClientSignal(body *ast.BlockStmt, imports map[string]clientImportKind, allowAnySiblingSelector bool) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isPrimitiveClientCall(call.Fun) || isImportedClientCall(call.Fun, imports, allowAnySiblingSelector) || isSidecarExecCall(call) {
			found = true
			return false
		}
		return true
	})
	return found
}

func isPrimitiveClientCall(expr ast.Expr) bool {
	parts := selectorParts(expr)
	if len(parts) < 2 {
		return false
	}
	root := parts[0]
	last := parts[len(parts)-1]
	if root == "flags" && last == "newClient" {
		return true
	}
	return outboundHTTPCallRe.MatchString(strings.Join(parts, ".") + "(")
}

func isSidecarExecCall(call *ast.CallExpr) bool {
	parts := selectorParts(call.Fun)
	if len(parts) != 2 || parts[0] != "exec" {
		return false
	}
	binaryArg := 0
	switch parts[1] {
	case "Command":
		binaryArg = 0
	case "CommandContext":
		binaryArg = 1
	default:
		return false
	}
	if len(call.Args) <= binaryArg {
		return false
	}
	lit, ok := call.Args[binaryArg].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.TrimSpace(value), "-pp-mcp")
}

func isImportedClientCall(expr ast.Expr, imports map[string]clientImportKind, allowAnySiblingSelector bool) bool {
	parts := selectorParts(expr)
	if len(parts) < 2 {
		return false
	}
	kind, ok := imports[parts[0]]
	if !ok {
		return false
	}
	if kind == generatedClientImport {
		return true
	}
	if allowAnySiblingSelector {
		return true
	}
	return isSiblingClientSelector(parts[len(parts)-1])
}

func isSiblingClientSelector(name string) bool {
	switch name {
	case "NewClient", "NewRequest", "NewRequestWithContext", "Get", "Post", "Do":
		return true
	default:
		return strings.HasPrefix(name, "Fetch")
	}
}

func selectorParts(expr ast.Expr) []string {
	switch e := expr.(type) {
	case *ast.Ident:
		return []string{e.Name}
	case *ast.SelectorExpr:
		parts := selectorParts(e.X)
		if len(parts) == 0 {
			return nil
		}
		return append(parts, e.Sel.Name)
	case *ast.ParenExpr:
		return selectorParts(e.X)
	default:
		return nil
	}
}

type commandHandlerScan struct {
	body     string
	bodyNode *ast.BlockStmt
	imports  map[string]clientImportKind
	ok       bool
}

func scanCommandHandler(content, leaf string) commandHandlerScan {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", content, 0)
	if err != nil {
		return commandHandlerScan{}
	}
	imports := clientImportAliases(file)
	cobraAliases := cobraImportAliases(file)
	var scan commandHandlerScan
	ast.Inspect(file, func(n ast.Node) bool {
		if scan.ok {
			return false
		}
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		handler := commandHandlerForLeaf(lit, leaf, cobraAliases)
		if handler == nil || handler.Body == nil {
			return true
		}
		start := fset.Position(handler.Body.Pos()).Offset
		end := fset.Position(handler.Body.End()).Offset
		if start < 0 || end > len(content) || start >= end {
			return true
		}
		scan = commandHandlerScan{
			body:     content[start:end],
			bodyNode: handler.Body,
			imports:  imports,
			ok:       true,
		}
		return false
	})
	return scan
}

func commandHandlerForLeaf(lit *ast.CompositeLit, leaf string, cobraAliases map[string]bool) *ast.FuncLit {
	if !isCommandCompositeType(lit.Type, cobraAliases) {
		return nil
	}
	matchesLeaf := false
	var runHandler *ast.FuncLit
	var runEHandler *ast.FuncLit
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Use":
			matchesLeaf = useExprLeaf(kv.Value) == leaf
		case "Run":
			if fn, ok := kv.Value.(*ast.FuncLit); ok {
				runHandler = fn
			}
		case "RunE":
			if fn, ok := kv.Value.(*ast.FuncLit); ok {
				runEHandler = fn
			}
		}
	}
	if matchesLeaf {
		if runEHandler != nil {
			return runEHandler
		}
		return runHandler
	}
	return nil
}

func cobraImportAliases(file *ast.File) map[string]bool {
	aliases := map[string]bool{}
	for _, spec := range file.Imports {
		importPath := strings.Trim(spec.Path.Value, "`\"")
		if importPath != "github.com/spf13/cobra" {
			continue
		}
		if spec.Name != nil {
			if spec.Name.Name != "_" {
				aliases[spec.Name.Name] = true
			}
			continue
		}
		aliases["cobra"] = true
	}
	return aliases
}

func isCommandCompositeType(expr ast.Expr, cobraAliases map[string]bool) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name == "Command" && cobraAliases["."]
	case *ast.SelectorExpr:
		id, ok := e.X.(*ast.Ident)
		return ok && e.Sel.Name == "Command" && cobraAliases[id.Name]
	case *ast.StarExpr:
		return isCommandCompositeType(e.X, cobraAliases)
	default:
		return false
	}
}

func useExprLeaf(expr ast.Expr) string {
	lit, ok := expr.(*ast.BasicLit)
	if !ok {
		return ""
	}
	use, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	fields := strings.Fields(use)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func forEachGoFuncContent(content string, visit func(name, body string)) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", content, 0)
	if err != nil {
		return
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Recv != nil {
			continue
		}
		start := fset.Position(fn.Pos()).Offset
		end := fset.Position(fn.End()).Offset
		if start < 0 || end > len(content) || start >= end {
			continue
		}
		visit(fn.Name.Name, content[start:end])
	}
}

func callsClientHelper(content string, helpers map[string]bool) bool {
	return callsHelper(content, helpers)
}

func callsStoreHelper(content string, helpers map[string]bool) bool {
	return callsHelper(content, helpers)
}

func countWeightedHelperCallsFiltered(content string, helpers map[string]int, include func(string, string) bool) int {
	if len(helpers) == 0 {
		return 0
	}
	return countHelperCalls(content, func(name string) int {
		count := 0
		for key, weight := range helpers {
			fileName, funcName := splitHelperKey(key)
			if funcName != name {
				continue
			}
			if include != nil && !include(fileName, funcName) {
				continue
			}
			count += weight
		}
		return count
	})
}

func callsHelper(content string, helpers map[string]bool) bool {
	if len(helpers) == 0 {
		return false
	}
	return countHelperCalls(content, func(name string) int {
		if helpers[name] {
			return 1
		}
		return 0
	}) > 0
}

func countHelperCalls(content string, weight func(string) int) int {
	source := content
	file, err := parser.ParseFile(token.NewFileSet(), "", source, 0)
	if err != nil {
		trimmed := strings.TrimSpace(source)
		if strings.HasPrefix(trimmed, "{") {
			source = "package cli\nfunc _() " + trimmed
		} else {
			source = "package cli\n" + source
		}
		file, err = parser.ParseFile(token.NewFileSet(), "", source, 0)
		if err != nil {
			return 0
		}
	}
	count := 0
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if ok {
			count += weight(ident.Name)
		}
		return true
	})
	return count
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
