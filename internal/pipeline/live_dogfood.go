package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

type LiveDogfoodStatus string

const (
	LiveDogfoodStatusPass LiveDogfoodStatus = "pass"
	LiveDogfoodStatusFail LiveDogfoodStatus = "fail"
	LiveDogfoodStatusSkip LiveDogfoodStatus = "skip"
)

type LiveDogfoodTestKind string

const (
	LiveDogfoodTestHelp  LiveDogfoodTestKind = "help"
	LiveDogfoodTestHappy LiveDogfoodTestKind = "happy_path"
	LiveDogfoodTestJSON  LiveDogfoodTestKind = "json_fidelity"
	LiveDogfoodTestError LiveDogfoodTestKind = "error_path"
)

type LiveDogfoodOptions struct {
	CLIDir              string
	BinaryName          string
	Level               string
	Timeout             time.Duration
	WriteAcceptancePath string
	AuthEnv             string
}

type LiveDogfoodReport struct {
	Dir        string                  `json:"dir"`
	Binary     string                  `json:"binary"`
	Level      string                  `json:"level"`
	Verdict    string                  `json:"verdict"`
	MatrixSize int                     `json:"matrix_size"`
	Passed     int                     `json:"passed"`
	Failed     int                     `json:"failed"`
	Skipped    int                     `json:"skipped"`
	Commands   []string                `json:"commands"`
	Tests      []LiveDogfoodTestResult `json:"tests"`
	RanAt      time.Time               `json:"ran_at"`
}

type LiveDogfoodTestResult struct {
	Command      string              `json:"command"`
	Kind         LiveDogfoodTestKind `json:"kind"`
	Args         []string            `json:"args"`
	Status       LiveDogfoodStatus   `json:"status"`
	ExitCode     int                 `json:"exit_code,omitempty"`
	Reason       string              `json:"reason,omitempty"`
	OutputSample string              `json:"output_sample,omitempty"`
}

type liveDogfoodCommand struct {
	Path []string
	Help string
}

type liveDogfoodRun struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
}

func RunLiveDogfood(opts LiveDogfoodOptions) (*LiveDogfoodReport, error) {
	if strings.TrimSpace(opts.CLIDir) == "" {
		return nil, fmt.Errorf("CLIDir is required")
	}
	level, err := normalizeLiveDogfoodLevel(opts.Level)
	if err != nil {
		return nil, err
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	binaryPath, err := liveDogfoodBinaryPath(opts.CLIDir, opts.BinaryName)
	if err != nil {
		return nil, err
	}

	commands, err := discoverLiveDogfoodCommands(binaryPath)
	if err != nil {
		return nil, err
	}
	if level == "quick" {
		commands = liveDogfoodQuickCommands(commands)
	}
	if len(commands) == 0 {
		return nil, fmt.Errorf("no live dogfood command leaves discovered")
	}

	report := &LiveDogfoodReport{
		Dir:     opts.CLIDir,
		Binary:  binaryPath,
		Level:   level,
		Verdict: "PASS",
		RanAt:   time.Now().UTC(),
	}

	siblings := buildSiblingMap(commands)
	cache := newCompanionCache()

	for _, command := range commands {
		commandName := strings.Join(command.Path, " ")
		report.Commands = append(report.Commands, commandName)
		report.Tests = append(report.Tests, runLiveDogfoodCommand(binaryPath, opts.CLIDir, command, siblings, cache, timeout)...)
	}

	finalizeLiveDogfoodReport(report)
	if report.Verdict == "PASS" && opts.WriteAcceptancePath != "" {
		if err := writeLiveDogfoodAcceptance(opts, report); err != nil {
			return nil, err
		}
	}
	return report, nil
}

func liveDogfoodBinaryPath(dir, name string) (string, error) {
	if path, err := resolveBinaryPath(dir, name); err == nil {
		return path, nil
	} else if strings.TrimSpace(name) != "" {
		return "", err
	}

	cliName := findCLIName(dir)
	if cliName == "" {
		return "", fmt.Errorf("no runnable binary found in %q and no cmd/<cli-name> package to build", dir)
	}
	return buildDogfoodBinary(dir, cliName)
}

func discoverLiveDogfoodCommands(binaryPath string) ([]liveDogfoodCommand, error) {
	out, err := runStdoutOnly(binaryPath, 15*time.Second, "agent-context")
	if err != nil {
		return nil, fmt.Errorf("agent-context failed: %w", err)
	}

	var ctx dogfoodAgentContext
	if err := json.Unmarshal(out, &ctx); err != nil {
		return nil, fmt.Errorf("parsing agent-context: %w", err)
	}

	var paths [][]string
	for _, command := range ctx.Commands {
		collectLiveDogfoodCommandPaths(nil, command, &paths)
	}
	sort.Slice(paths, func(i, j int) bool {
		return strings.Join(paths[i], " ") < strings.Join(paths[j], " ")
	})

	commands := make([]liveDogfoodCommand, 0, len(paths))
	for _, path := range paths {
		commands = append(commands, liveDogfoodCommand{Path: path})
	}
	return commands, nil
}

var liveDogfoodFrameworkSkip = map[string]bool{
	"agent-context": true,
	"completion":    true,
	"help":          true,
	"version":       true,
}

// crossAPIListVerbs are leaf names that any modern API CLI may expose as a
// list-shape companion to a get-shape command. Used by U2's chained
// companion walk to source real ids from sibling list calls.
var crossAPIListVerbs = map[string]bool{
	"list": true, "all": true, "index": true,
	"query": true, "find": true, "search": true,
	"discover": true, "browse": true, "recent": true, "feed": true,
}

// cinemaListVerbs are TMDb / cinema-API specific. Kept because TMDb's
// printed CLI does not expose a plain `list` leaf — `popular`/`trending`/
// etc. are the canonical list shape on that API surface. Future additions
// here should be domain-specific; generic verbs go in crossAPIListVerbs.
var cinemaListVerbs = map[string]bool{
	"popular": true, "trending": true, "top_rated": true,
	"latest": true, "now_playing": true, "upcoming": true,
	"airing_today": true, "on_the_air": true,
}

func isCompanionLeaf(name string) bool {
	return crossAPIListVerbs[name] || cinemaListVerbs[name]
}

// resolveStatus is the outcome enum returned by resolveCommandPositionals.
type resolveStatus int

const (
	resolveOK resolveStatus = iota
	resolveSkip
)

// companionCache is run-scoped: a per-RunLiveDogfood map keyed by the full
// companion argv (NUL-joined to avoid collisions between paths and ids).
// Values are extracted ids. helps caches the companion's --help output so
// `--limit` detection runs at most once per companion.
type companionCache struct {
	results map[string]string
	helps   map[string]string
}

func newCompanionCache() *companionCache {
	return &companionCache{
		results: map[string]string{},
		helps:   map[string]string{},
	}
}

// buildSiblingMap groups commands by their joined parent path so the chain
// walker can look up sibling list-shape companions in O(1).
func buildSiblingMap(commands []liveDogfoodCommand) map[string][]liveDogfoodCommand {
	siblings := map[string][]liveDogfoodCommand{}
	for _, c := range commands {
		if len(c.Path) == 0 {
			continue
		}
		key := strings.Join(c.Path[:len(c.Path)-1], " ")
		siblings[key] = append(siblings[key], c)
	}
	return siblings
}

// findListCompanion picks the first sibling whose leaf name is in the
// companion-leaf allowlist (cross-API or cinema). Returns nil when no
// allowlisted sibling is present.
func findListCompanion(candidates []liveDogfoodCommand) *liveDogfoodCommand {
	for i := range candidates {
		path := candidates[i].Path
		if len(path) == 0 {
			continue
		}
		if isCompanionLeaf(path[len(path)-1]) {
			return &candidates[i]
		}
	}
	return nil
}

// resolveCommandPositionals walks the sibling list-shape chain to source a
// real id for each id-shape positional in command.Help's Usage line. Earlier-
// resolved ids are threaded into later list calls as positional context, so
// nested resources (projects/tasks/update <pid> <tid>) work end-to-end.
//
// Returns:
//   - (newArgs, resolveOK, "")    — placeholders successfully substituted, run happy_path with newArgs
//   - (nil, resolveSkip, reason)  — chain broke; caller must skip happy_path + json_fidelity
//   - (happyArgs, resolveOK, "")  — no positionals at all; pass-through unchanged
func resolveCommandPositionals(
	command liveDogfoodCommand,
	happyArgs []string,
	siblings map[string][]liveDogfoodCommand,
	cache *companionCache,
	binaryPath, cliDir string,
	timeout time.Duration,
) ([]string, resolveStatus, string) {
	placeholders := extractPositionalPlaceholders(liveDogfoodUsageSuffix(command.Help))
	if len(placeholders) == 0 {
		return happyArgs, resolveOK, ""
	}

	pathLen := len(command.Path)
	nPlaceholders := len(placeholders)
	if pathLen < nPlaceholders+1 {
		// More placeholders than path segments before the verb. Unusual
		// shape (top-level command with multiple positionals); skip.
		return nil, resolveSkip, fmt.Sprintf(
			"command path %v has fewer segments than placeholders (%d)", command.Path, nPlaceholders)
	}

	resolved := make([]string, 0, nPlaceholders)
	for i, name := range placeholders {
		nameLower := strings.ToLower(name)
		// id-shape covers: bare "id", snake_case "*_id", or camelCase "*id"
		// where the prefix has at least one character (len > 2).
		isIDShape := nameLower == "id" ||
			(strings.HasSuffix(nameLower, "id") && len(nameLower) > 2)
		if !isIDShape {
			return nil, resolveSkip, fmt.Sprintf(
				"non-id positional %q at depth %d", name, i)
		}

		// Sibling key for placeholder at depth i: the parent of the verb
		// at command.Path[pathLen - nPlaceholders + i]. The list companion
		// shares this parent — i.e., it's a sibling of that verb at the
		// same path depth.
		siblingKeySegments := command.Path[:pathLen-nPlaceholders+i]
		siblingKey := strings.Join(siblingKeySegments, " ")

		listCmd := findListCompanion(siblings[siblingKey])
		if listCmd == nil {
			return nil, resolveSkip, fmt.Sprintf(
				"no list companion at depth %d for %q", i, name)
		}

		// Build companion args: list path + already-resolved parent ids +
		// --json (and --limit 1 if supported).
		listArgs := append([]string{}, listCmd.Path...)
		listArgs = append(listArgs, resolved...)
		listArgs = append(listArgs, "--json")
		if companionSupportsLimit(*listCmd, cache, binaryPath, cliDir, timeout) {
			listArgs = append(listArgs, "--limit", "1")
		}

		// Cache lookup keyed by full argv (NUL-joined to avoid path/id
		// collisions).
		cacheKey := strings.Join(listArgs, "\x00")
		if id, ok := cache.results[cacheKey]; ok {
			resolved = append(resolved, id)
			continue
		}

		run := runLiveDogfoodProcess(binaryPath, cliDir, listArgs, timeout)
		if run.exitCode != 0 {
			return nil, resolveSkip, fmt.Sprintf(
				"list companion failed at depth %d: exit %d", i, run.exitCode)
		}

		id, ok := extractFirstIDFromJSON(run.stdout)
		if !ok {
			return nil, resolveSkip, fmt.Sprintf(
				"no id parseable from companion at depth %d", i)
		}

		cache.results[cacheKey] = id
		resolved = append(resolved, id)
	}

	return substitutePositionals(happyArgs, command.Path, resolved), resolveOK, ""
}

// substitutePositionals replaces the first len(resolved) non-flag args in
// happyArgs (after command.Path) with the resolved ids. The walk preserves
// flags interleaved with positionals so an example like
// `--limit 5 widgets get <id>` stays intact when the placeholder is
// substituted in. Args before command.Path are preserved untouched.
func substitutePositionals(happyArgs, commandPath []string, resolved []string) []string {
	out := make([]string, 0, len(happyArgs))
	out = append(out, happyArgs[:min(len(commandPath), len(happyArgs))]...)
	idx := 0
	for j := len(commandPath); j < len(happyArgs); j++ {
		arg := happyArgs[j]
		if !strings.HasPrefix(arg, "-") && idx < len(resolved) {
			out = append(out, resolved[idx])
			idx++
		} else {
			out = append(out, arg)
		}
	}
	return out
}

// companionSupportsLimit checks the companion's --help for a --limit flag,
// caching the result. Lazy: only invoked once per companion path because
// the chain walker calls findListCompanion before each invocation and we
// only consult --help when a companion was actually selected.
func companionSupportsLimit(companion liveDogfoodCommand, cache *companionCache, binaryPath, cliDir string, timeout time.Duration) bool {
	pathKey := strings.Join(companion.Path, " ")
	help, cached := cache.helps[pathKey]
	if !cached {
		helpArgs := append(append([]string{}, companion.Path...), "--help")
		run := runLiveDogfoodProcess(binaryPath, cliDir, helpArgs, timeout)
		if run.exitCode != 0 {
			cache.helps[pathKey] = ""
			return false
		}
		help = run.stdout + run.stderr
		cache.helps[pathKey] = help
	}
	return slices.Contains(extractFlagNames(help), "limit")
}

// extractFirstIDFromJSON walks a small set of canonical response shapes and
// returns the first id field's value as a string. Order matters — the
// REST-style paths win when both are present (some hybrid APIs emit both).
//
// Try-list (first match wins):
//  1. .results[0].id            — TMDb, common search/list
//  2. .[0].id                   — top-level array (GitHub releases, etc.)
//  3. .items[0].id              — GitHub REST issues, paginated lists
//  4. .data[0].id               — Stripe-style envelopes
//  5. .list[0].id               — long-tail
//  6. .data.<any>.nodes[0].id   — Shopify / Linear / Notion GraphQL
//  7. .data.<any>.edges[0].node.id — Relay-style (GitHub GraphQL)
//
// Numeric ids are decoded via json.Decoder.UseNumber() so very large values
// (e.g., snowflake ids > 2^53) coerce cleanly through fmt.Sprint without
// scientific notation.
func extractFirstIDFromJSON(stdout string) (string, bool) {
	dec := json.NewDecoder(strings.NewReader(stdout))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return "", false
	}

	// Path 1: .results[0].id
	if id, ok := pickIDFromArrayKey(root, "results"); ok {
		return id, true
	}
	// Path 2: top-level array .[0].id
	if id, ok := pickIDFromTopArray(root); ok {
		return id, true
	}
	// Path 3: .items[0].id
	if id, ok := pickIDFromArrayKey(root, "items"); ok {
		return id, true
	}
	// Path 4: .data[0].id (only when .data is an ARRAY — GraphQL data is an object)
	if obj, ok := root.(map[string]any); ok {
		if dataArr, ok := obj["data"].([]any); ok {
			if id, ok := firstIDFromArray(dataArr); ok {
				return id, true
			}
		}
	}
	// Path 5: .list[0].id
	if id, ok := pickIDFromArrayKey(root, "list"); ok {
		return id, true
	}
	// Path 6: .data.<any>.nodes[0].id
	if id, ok := pickIDFromGraphQLConnection(root, "nodes", false); ok {
		return id, true
	}
	// Path 7: .data.<any>.edges[0].node.id
	if id, ok := pickIDFromGraphQLConnection(root, "edges", true); ok {
		return id, true
	}
	return "", false
}

func pickIDFromArrayKey(root any, key string) (string, bool) {
	obj, ok := root.(map[string]any)
	if !ok {
		return "", false
	}
	arr, ok := obj[key].([]any)
	if !ok {
		return "", false
	}
	return firstIDFromArray(arr)
}

func pickIDFromTopArray(root any) (string, bool) {
	arr, ok := root.([]any)
	if !ok {
		return "", false
	}
	return firstIDFromArray(arr)
}

func firstIDFromArray(arr []any) (string, bool) {
	if len(arr) == 0 {
		return "", false
	}
	first, ok := arr[0].(map[string]any)
	if !ok {
		return "", false
	}
	return idValueAsString(first["id"])
}

// pickIDFromGraphQLConnection walks .data... looking for a `connectionKey`
// (`nodes` or `edges`) array within a bounded subtree. Handles two shapes:
//
//	Shape A — depth 1 under .data (Shopify, Linear, Notion):
//	  .data.<resource>.<connectionKey>[0]...
//
//	Shape B — depth 2 under .data (GitHub Relay viewer.repos.edges):
//	  .data.<wrapper>.<resource>.<connectionKey>[0]...
//
// edgeShape=true reads id from .node.id under each entry (Relay edges);
// edgeShape=false reads id directly from each entry (nodes). The walk is
// bounded to depth 2 to avoid pathological recursion on deeply nested
// responses that don't carry an id-shaped first element.
func pickIDFromGraphQLConnection(root any, connectionKey string, edgeShape bool) (string, bool) {
	obj, ok := root.(map[string]any)
	if !ok {
		return "", false
	}
	data, ok := obj["data"].(map[string]any)
	if !ok {
		return "", false
	}
	// Try depth 1 then depth 2.
	for depth := 1; depth <= 2; depth++ {
		if id, ok := walkForConnection(data, connectionKey, edgeShape, depth); ok {
			return id, true
		}
	}
	return "", false
}

// walkForConnection descends `depth` levels into nested map[string]any
// values, returning the first matching connection's id.
func walkForConnection(node map[string]any, connectionKey string, edgeShape bool, depth int) (string, bool) {
	if depth == 0 {
		arr, ok := node[connectionKey].([]any)
		if !ok || len(arr) == 0 {
			return "", false
		}
		first, ok := arr[0].(map[string]any)
		if !ok {
			return "", false
		}
		if edgeShape {
			n, ok := first["node"].(map[string]any)
			if !ok {
				return "", false
			}
			return idValueAsString(n["id"])
		}
		return idValueAsString(first["id"])
	}
	for _, child := range node {
		childObj, ok := child.(map[string]any)
		if !ok {
			continue
		}
		if id, ok := walkForConnection(childObj, connectionKey, edgeShape, depth-1); ok {
			return id, true
		}
	}
	return "", false
}

func idValueAsString(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	switch t := v.(type) {
	case string:
		if t == "" {
			return "", false
		}
		return t, true
	case json.Number:
		return t.String(), true
	case bool:
		return "", false
	default:
		return fmt.Sprint(v), true
	}
}

func collectLiveDogfoodCommandPaths(prefix []string, command dogfoodAgentCommand, paths *[][]string) {
	if command.Name == "" || liveDogfoodFrameworkSkip[command.Name] {
		return
	}

	next := append(append([]string{}, prefix...), command.Name)
	if len(command.Subcommands) == 0 {
		*paths = append(*paths, next)
		return
	}
	for _, sub := range command.Subcommands {
		collectLiveDogfoodCommandPaths(next, sub, paths)
	}
}

func runLiveDogfoodCommand(binaryPath, cliDir string, command liveDogfoodCommand, siblings map[string][]liveDogfoodCommand, cache *companionCache, timeout time.Duration) []LiveDogfoodTestResult {
	commandName := strings.Join(command.Path, " ")

	helpArgs := append(append([]string{}, command.Path...), "--help")
	helpRun := runLiveDogfoodProcess(binaryPath, cliDir, helpArgs, timeout)
	helpResult := liveDogfoodResult(commandName, LiveDogfoodTestHelp, helpArgs, helpRun)
	helpPassed := helpRun.exitCode == 0
	help := helpRun.stdout + helpRun.stderr
	if helpPassed && extractExamplesSection(help) == "" {
		helpPassed = false
		helpResult.Status = LiveDogfoodStatusFail
		helpResult.Reason = "missing Examples section"
	}
	if helpPassed {
		helpResult.Status = LiveDogfoodStatusPass
		helpResult.Reason = ""
	}

	results := []LiveDogfoodTestResult{helpResult}
	if !helpPassed {
		results = append(results,
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestHappy, "help check failed"),
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestJSON, "help check failed"),
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestError, "help check failed"),
		)
		return results
	}

	command.Help = help
	happyArgs, ok := liveDogfoodHappyArgs(command)
	if !ok {
		results = append(results,
			failedLiveDogfoodResult(commandName, LiveDogfoodTestHappy, command.Path, "missing runnable example"),
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestJSON, "missing runnable example"),
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestError, "missing runnable example"),
		)
		return results
	}

	// Chained companion walk: source real ids for each id-shape positional
	// from sibling list-shape companions. On skip, happy_path and json_fidelity
	// become Status=Skip with a reason naming the failed link in the chain.
	// error_path runs independently (its own positional gate below).
	resolvedArgs, resolveStat, resolveReason := resolveCommandPositionals(
		command, happyArgs, siblings, cache, binaryPath, cliDir, timeout)
	if resolveStat == resolveSkip {
		results = append(results,
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestHappy, resolveReason),
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestJSON, resolveReason),
		)
	} else {
		happyArgs = resolvedArgs

		happyRun := runLiveDogfoodProcess(binaryPath, cliDir, happyArgs, timeout)
		happyResult := liveDogfoodResult(commandName, LiveDogfoodTestHappy, happyArgs, happyRun)
		if happyRun.exitCode == 0 {
			happyResult.Status = LiveDogfoodStatusPass
			happyResult.Reason = ""
		}
		results = append(results, happyResult)

		if commandSupportsJSON(command.Help) {
			jsonArgs := appendJSONArg(happyArgs)
			jsonRun := runLiveDogfoodProcess(binaryPath, cliDir, jsonArgs, timeout)
			jsonResult := liveDogfoodResult(commandName, LiveDogfoodTestJSON, jsonArgs, jsonRun)
			if jsonRun.exitCode == 0 {
				if !json.Valid([]byte(jsonRun.stdout)) {
					jsonResult.Status = LiveDogfoodStatusFail
					jsonResult.Reason = "invalid JSON"
				} else {
					jsonResult.Status = LiveDogfoodStatusPass
					jsonResult.Reason = ""
				}
			}
			results = append(results, jsonResult)
		} else {
			results = append(results, skippedLiveDogfoodResult(commandName, LiveDogfoodTestJSON, "--json not supported"))
		}
	}

	if liveDogfoodCommandTakesArg(command.Help) {
		var errorArgs []string
		if commandSupportsSearch(command.Help) {
			// Search-shape: prefer the --query flag when present, otherwise
			// pass the invalid token as a positional. Append --json so a
			// well-behaved search emits parseable output we can validate.
			errorArgs = append([]string{}, command.Path...)
			if slices.Contains(extractFlagNames(command.Help), "query") {
				errorArgs = append(errorArgs, "--query", "__printing_press_invalid__")
			} else {
				errorArgs = append(errorArgs, "__printing_press_invalid__")
			}
			if commandSupportsJSON(command.Help) {
				errorArgs = appendJSONArg(errorArgs)
			}
		} else {
			errorArgs = append(append([]string{}, command.Path...), "__printing_press_invalid__")
		}

		errorRun := runLiveDogfoodProcess(binaryPath, cliDir, errorArgs, timeout)
		errorResult := liveDogfoodResult(commandName, LiveDogfoodTestError, errorArgs, errorRun)

		if commandSupportsSearch(command.Help) {
			// Search-shape strategy: exit non-zero is PASS (the API rejected
			// the token); exit 0 is PASS UNLESS --json was supplied and the
			// output isn't valid JSON. Real-world content/feed APIs return
			// recent items as a fallback for unmatched queries — non-empty
			// results are not a failure signal here. The only fail mode is
			// a search command claiming --json support but emitting non-JSON.
			suppliedJSON := commandSupportsJSON(command.Help)
			switch {
			case errorRun.exitCode != 0:
				errorResult.Status = LiveDogfoodStatusPass
				errorResult.Reason = ""
			case suppliedJSON && !json.Valid([]byte(errorRun.stdout)):
				errorResult.Status = LiveDogfoodStatusFail
				errorResult.Reason = "invalid JSON under --json"
			default:
				errorResult.Status = LiveDogfoodStatusPass
				errorResult.Reason = ""
			}
		} else {
			// Mutation/plain-get strategy preserved: non-zero exit required.
			if errorRun.exitCode != 0 {
				errorResult.Status = LiveDogfoodStatusPass
				errorResult.Reason = ""
			} else {
				errorResult.Status = LiveDogfoodStatusFail
				errorResult.Reason = "expected non-zero exit for invalid argument"
			}
		}
		results = append(results, errorResult)
	} else {
		results = append(results, skippedLiveDogfoodResult(commandName, LiveDogfoodTestError, "no positional argument"))
	}

	return results
}

// commandSupportsSearch reports whether a command behaves like a search:
// either it ships a --query flag, or its Usage suffix carries a <query>
// positional placeholder. Search-shape commands canonically return exit 0
// with empty (or fallback) results on no-match, so error_path treats them
// differently from mutating writes.
func commandSupportsSearch(help string) bool {
	if slices.Contains(extractFlagNames(help), "query") {
		return true
	}
	return slices.Contains(extractPositionalPlaceholders(liveDogfoodUsageSuffix(help)), "query")
}

func runLiveDogfoodProcess(binaryPath, cliDir string, args []string, timeout time.Duration) liveDogfoodRun {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Dir = cliDir
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = &limitedWriter{w: stdout, remaining: MaxOutputBytes}
	cmd.Stderr = &limitedWriter{w: stderr, remaining: MaxOutputBytes}

	err := cmd.Run()
	result := liveDogfoodRun{
		stdout:   stdout.String(),
		stderr:   stderr.String(),
		exitCode: 0,
		err:      err,
	}
	if ctx.Err() == context.DeadlineExceeded {
		result.exitCode = -1
		result.err = fmt.Errorf("timed out after %s", timeout)
		return result
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.exitCode = exitErr.ExitCode()
		} else {
			result.exitCode = -1
		}
	}
	return result
}

func liveDogfoodResult(command string, kind LiveDogfoodTestKind, args []string, run liveDogfoodRun) LiveDogfoodTestResult {
	result := LiveDogfoodTestResult{
		Command:      command,
		Kind:         kind,
		Args:         append([]string{}, args...),
		Status:       LiveDogfoodStatusFail,
		ExitCode:     run.exitCode,
		OutputSample: sampleOutput(run.stdout + run.stderr),
	}
	if run.exitCode != 0 {
		result.Reason = fmt.Sprintf("exit %d", run.exitCode)
	}
	if run.err != nil && result.Reason == "" {
		result.Reason = run.err.Error()
	}
	return result
}

func failedLiveDogfoodResult(command string, kind LiveDogfoodTestKind, args []string, reason string) LiveDogfoodTestResult {
	return LiveDogfoodTestResult{
		Command: command,
		Kind:    kind,
		Args:    append([]string{}, args...),
		Status:  LiveDogfoodStatusFail,
		Reason:  reason,
	}
}

func skippedLiveDogfoodResult(command string, kind LiveDogfoodTestKind, reason string) LiveDogfoodTestResult {
	return LiveDogfoodTestResult{
		Command: command,
		Kind:    kind,
		Status:  LiveDogfoodStatusSkip,
		Reason:  reason,
	}
}

func liveDogfoodHappyArgs(command liveDogfoodCommand) ([]string, bool) {
	examples := extractExamplesSection(command.Help)
	for line := range strings.SplitSeq(examples, "\n") {
		candidate := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "$"))
		if candidate == "" || strings.HasPrefix(candidate, "#") {
			continue
		}
		args, err := parseExampleArgs(candidate)
		if err == nil && len(args) > 0 && slices.Equal(args[:min(len(command.Path), len(args))], command.Path) {
			return args, true
		}
	}
	return nil, false
}

func commandSupportsJSON(help string) bool {
	return slices.Contains(extractFlagNames(help), "json")
}

func appendJSONArg(args []string) []string {
	out := append([]string{}, args...)
	for _, arg := range out {
		if arg == "--json" || strings.HasPrefix(arg, "--json=") {
			return out
		}
	}
	return append(out, "--json")
}

func liveDogfoodCommandTakesArg(help string) bool {
	usage := liveDogfoodUsageSuffix(help)
	return len(extractPositionalPlaceholders(usage)) > 0
}

func liveDogfoodUsageSuffix(help string) string {
	lines := strings.Split(help, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "Usage:" {
			continue
		}
		if i+1 < len(lines) {
			return lines[i+1]
		}
	}
	return ""
}

func finalizeLiveDogfoodReport(report *LiveDogfoodReport) {
	for _, result := range report.Tests {
		switch result.Status {
		case LiveDogfoodStatusPass:
			report.Passed++
			report.MatrixSize++
		case LiveDogfoodStatusFail:
			report.Failed++
			report.MatrixSize++
		default:
			report.Skipped++
		}
	}
	// Failed-or-empty wins: any real failure or a fully empty matrix is FAIL,
	// regardless of level. Then the quick-level PASS arm accepts skip-with-
	// reason as a non-failure (Skipped counts toward the 5-entry quorum), with
	// a MatrixSize floor of 4 to guard against pathological all-skip outcomes.
	switch {
	case report.Failed > 0 || report.MatrixSize == 0:
		report.Verdict = "FAIL"
	case report.Level == "quick" && report.Passed+report.Skipped >= 5 && report.MatrixSize >= 4:
		report.Verdict = "PASS"
	case report.Level == "quick":
		report.Verdict = "FAIL"
	}
}

func writeLiveDogfoodAcceptance(opts LiveDogfoodOptions, report *LiveDogfoodReport) error {
	manifest, err := ReadCLIManifest(opts.CLIDir)
	if err != nil {
		return fmt.Errorf("reading CLI manifest for phase5 acceptance: %w", err)
	}
	if manifest.APIName == "" {
		return fmt.Errorf("CLI manifest missing api_name; cannot write phase5 acceptance")
	}
	if manifest.RunID == "" {
		return fmt.Errorf("CLI manifest missing run_id; cannot write phase5 acceptance")
	}
	authType := manifest.AuthType
	if authType == "" {
		authType = "none"
	}

	marker := Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       manifest.APIName,
		RunID:         manifest.RunID,
		Status:        "pass",
		Level:         report.Level,
		MatrixSize:    report.MatrixSize,
		TestsPassed:   report.Passed,
		TestsFailed:   report.Failed,
		AuthContext: Phase5AuthContext{
			Type:            authType,
			APIKeyAvailable: opts.AuthEnv != "" && os.Getenv(opts.AuthEnv) != "",
		},
	}
	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling phase5 acceptance marker: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(opts.WriteAcceptancePath), 0o755); err != nil {
		return fmt.Errorf("creating phase5 acceptance directory: %w", err)
	}
	if err := os.WriteFile(opts.WriteAcceptancePath, data, 0o644); err != nil {
		return fmt.Errorf("writing phase5 acceptance marker: %w", err)
	}
	return nil
}

func liveDogfoodQuickCommands(commands []liveDogfoodCommand) []liveDogfoodCommand {
	if len(commands) <= 2 {
		return commands
	}
	return commands[:2]
}

func normalizeLiveDogfoodLevel(level string) (string, error) {
	level = strings.ToLower(strings.TrimSpace(level))
	if level == "" {
		return "full", nil
	}
	switch level {
	case "quick", "full":
		return level, nil
	default:
		return "", fmt.Errorf("invalid live dogfood level %q (expected quick or full)", level)
	}
}
