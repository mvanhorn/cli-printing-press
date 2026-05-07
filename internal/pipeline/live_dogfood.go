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
	LiveDogfoodTestHelp      LiveDogfoodTestKind = "help"
	LiveDogfoodTestHappy     LiveDogfoodTestKind = "happy_path"
	LiveDogfoodTestJSON      LiveDogfoodTestKind = "json_fidelity"
	LiveDogfoodTestError     LiveDogfoodTestKind = "error_path"
	LiveDogfoodTestErrorReal LiveDogfoodTestKind = "error_path_real"
)

// reasonDestructiveAtAuth is the Skip reason emitted for endpoints that
// can invalidate the credential used by the live-dogfood runner. Reused
// across the matrix builder, the flag help text, and the test fixtures.
const reasonDestructiveAtAuth = "destructive-at-auth"
const reasonMutatingDryRunOnly = "mutating command dry-run only"
const reasonNoLiveSignal = "no live happy/json pass; credential-unavailable skips cannot certify acceptance"
const reasonUnavailableRunnerCredentials = "unavailable for runner credentials"

type LiveDogfoodOptions struct {
	CLIDir              string
	BinaryName          string
	Level               string
	Timeout             time.Duration
	WriteAcceptancePath string
	AuthEnv             string
	// AllowDestructive re-enables testing of endpoints classified as
	// destructive-at-auth. Default skips them to prevent runner-credential
	// rotation.
	AllowDestructive bool
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
	Path        []string
	Help        string
	Annotations map[string]string
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

	ctx := resolveCtx{
		binaryPath:       binaryPath,
		cliDir:           opts.CLIDir,
		siblings:         buildSiblingMap(commands),
		cache:            newCompanionCache(),
		timeout:          timeout,
		allowDestructive: opts.AllowDestructive,
	}

	for _, command := range commands {
		commandName := strings.Join(command.Path, " ")
		report.Commands = append(report.Commands, commandName)
		report.Tests = append(report.Tests, runLiveDogfoodCommand(command, ctx)...)
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

	var commands []liveDogfoodCommand
	for _, command := range ctx.Commands {
		collectLiveDogfoodCommands(nil, command, &commands)
	}
	sort.Slice(commands, func(i, j int) bool {
		return strings.Join(commands[i].Path, " ") < strings.Join(commands[j].Path, " ")
	})
	return commands, nil
}

var liveDogfoodFrameworkSkip = map[string]bool{
	"agent-context": true,
	"auth":          true,
	"completion":    true,
	"help":          true,
	"version":       true,
}

// crossAPIListVerbs are leaf names a modern API CLI may expose as a
// list-shape companion to a get-shape command.
var crossAPIListVerbs = map[string]bool{
	"list": true, "all": true, "index": true,
	"query": true, "find": true, "search": true,
	"discover": true, "browse": true, "recent": true, "feed": true,
}

// cinemaListVerbs are domain-specific list verbs for media/cinema-class APIs
// that expose `popular`/`trending`/etc. as the canonical list shape rather
// than a plain `list` leaf. Keep cross-API generic verbs in
// crossAPIListVerbs; route new media-class verbs here.
var cinemaListVerbs = map[string]bool{
	"popular": true, "trending": true, "top_rated": true,
	"latest": true, "now_playing": true, "upcoming": true,
	"airing_today": true, "on_the_air": true,
}

func isCompanionLeaf(name string) bool {
	return crossAPIListVerbs[name] || cinemaListVerbs[name]
}

// mutatingVerbs name leaves whose semantics include writes/deletes against
// the API. Used as a deny-list overlay on the search-shape heuristic so a
// command like `delete --query=...` (mass delete by filter) is not probed
// with __printing_press_invalid__ against the live API.
var mutatingVerbs = map[string]bool{
	"delete": true, "destroy": true, "remove": true,
	"create": true, "add": true, "new": true,
	"update": true, "patch": true, "edit": true,
	"set": true, "modify": true, "replace": true,
	"post": true, "put": true, "send": true, "submit": true,
	"transfer": true, "cancel": true, "freeze": true, "unfreeze": true,
}

func isMutatingLeaf(name string) bool {
	for _, token := range commandNameTokens(name) {
		if mutatingVerbs[token] {
			return true
		}
	}
	return false
}

func liveDogfoodCommandMutates(command liveDogfoodCommand) bool {
	if annotationIsTrueValue(command.Annotations[mcpReadOnlyAnnotation]) {
		return false
	}
	if method := strings.ToUpper(strings.TrimSpace(command.Annotations[endpointMethodAnnotation])); method != "" {
		return method == "POST" || method == "PUT" || method == "PATCH" || method == "DELETE"
	}
	if len(command.Path) == 0 {
		return false
	}
	return isMutatingLeaf(command.Path[len(command.Path)-1])
}

func commandNameTokens(name string) []string {
	return strings.FieldsFunc(strings.ToLower(name), func(r rune) bool {
		return r < 'a' || r > 'z'
	})
}

// companionCache is run-scoped: per-RunLiveDogfood maps keyed by the full
// companion argv (NUL-joined to avoid path/id collisions).
type companionCache struct {
	// results: NUL-joined argv → extracted id.
	results map[string]string
	// helps: companion path → cached --help output, so `--limit` detection
	// runs at most once per companion.
	helps map[string]string
}

// resolveCtx threads run-scoped state into the chained companion walk so
// individual helpers don't need to take the same five parameters.
type resolveCtx struct {
	binaryPath       string
	cliDir           string
	siblings         map[string][]liveDogfoodCommand
	cache            *companionCache
	timeout          time.Duration
	allowDestructive bool
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
//   - (newArgs, false, "")   — placeholders substituted; run happy_path with newArgs
//   - (nil, true, reason)    — chain broke; caller must skip happy_path + json_fidelity
//   - (happyArgs, false, "") — no positionals at all; pass-through unchanged
func resolveCommandPositionals(command liveDogfoodCommand, happyArgs []string, ctx resolveCtx) ([]string, bool, string) {
	placeholders := extractPositionalPlaceholders(liveDogfoodUsageSuffix(command.Help))
	if len(placeholders) == 0 {
		return happyArgs, false, ""
	}

	pathLen := len(command.Path)
	nPlaceholders := len(placeholders)
	if pathLen < nPlaceholders+1 {
		// More placeholders than path segments before the verb. Unusual
		// shape (top-level command with multiple positionals); skip.
		return nil, true, fmt.Sprintf(
			"command path %v has fewer segments than placeholders (%d)", command.Path, nPlaceholders)
	}

	resolved := make([]string, 0, nPlaceholders)
	for i, name := range placeholders {
		nameLower := strings.ToLower(name)
		// id-shape covers: bare "id", snake_case "*_id", or camelCase "*id"
		// where the prefix has at least one character (len > 2). Broader than
		// generator.go exampleValue's predicate — no spec type info is available
		// from CLI help text, so the string-type fence applied there is omitted.
		isIDShape := nameLower == "id" ||
			(strings.HasSuffix(nameLower, "id") && len(nameLower) > 2)
		if !isIDShape {
			return nil, true, fmt.Sprintf("non-id positional %q at depth %d", name, i)
		}

		// parent path of the verb that expects this placeholder.
		siblingKey := strings.Join(command.Path[:pathLen-nPlaceholders+i], " ")
		listCmd := findListCompanion(ctx.siblings[siblingKey])
		if listCmd == nil {
			return nil, true, fmt.Sprintf("no list companion at depth %d for %q", i, name)
		}

		listArgs := append([]string{}, listCmd.Path...)
		listArgs = append(listArgs, resolved...)
		listArgs = append(listArgs, "--json")
		if companionSupportsLimit(*listCmd, ctx) {
			listArgs = append(listArgs, "--limit", "1")
		}

		cacheKey := strings.Join(listArgs, "\x00") // NUL avoids path/id collisions.
		if id, ok := ctx.cache.results[cacheKey]; ok {
			if id == "" {
				// Negative-cache sentinel: this companion already failed in this
				// run. Skip immediately so sibling get-shape commands sharing
				// the same companion don't each block on the same 30s timeout.
				return nil, true, fmt.Sprintf(
					"list companion previously failed at depth %d for %q", i, name)
			}
			resolved = append(resolved, id)
			continue
		}

		run := runLiveDogfoodProcess(ctx.binaryPath, ctx.cliDir, listArgs, ctx.timeout)
		if run.exitCode != 0 {
			ctx.cache.results[cacheKey] = "" // negative-cache sentinel
			return nil, true, fmt.Sprintf(
				"list companion failed at depth %d: exit %d", i, run.exitCode)
		}

		id, ok := extractFirstIDFromJSON(run.stdout)
		if !ok {
			ctx.cache.results[cacheKey] = "" // negative-cache sentinel
			return nil, true, fmt.Sprintf(
				"no id parseable from companion at depth %d", i)
		}

		ctx.cache.results[cacheKey] = id
		resolved = append(resolved, id)
	}

	return substitutePositionals(happyArgs, command.Path, resolved), false, ""
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
func companionSupportsLimit(companion liveDogfoodCommand, ctx resolveCtx) bool {
	pathKey := strings.Join(companion.Path, " ")
	help, cached := ctx.cache.helps[pathKey]
	if !cached {
		helpArgs := append(append([]string{}, companion.Path...), "--help")
		run := runLiveDogfoodProcess(ctx.binaryPath, ctx.cliDir, helpArgs, ctx.timeout)
		if run.exitCode != 0 {
			ctx.cache.helps[pathKey] = ""
			return false
		}
		help = run.stdout + run.stderr
		ctx.cache.helps[pathKey] = help
	}
	return slices.Contains(extractFlagNames(help), "limit")
}

// extractFirstIDFromJSON tries canonical REST and GraphQL response shapes
// in order; see inline `// Path N:` comments for the priority list.
// UseNumber() preserves large numeric ids (e.g., snowflake > 2^53) through
// fmt.Sprint without scientific notation.
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

func collectLiveDogfoodCommands(prefix []string, command dogfoodAgentCommand, cmds *[]liveDogfoodCommand) {
	if command.Name == "" || liveDogfoodFrameworkSkip[command.Name] {
		return
	}

	next := append(append([]string{}, prefix...), command.Name)
	if len(command.Subcommands) == 0 {
		*cmds = append(*cmds, liveDogfoodCommand{Path: next, Annotations: command.Annotations})
		return
	}
	for _, sub := range command.Subcommands {
		collectLiveDogfoodCommands(next, sub, cmds)
	}
}

func runLiveDogfoodCommand(command liveDogfoodCommand, ctx resolveCtx) []LiveDogfoodTestResult {
	commandName := strings.Join(command.Path, " ")

	// Destructive-at-auth short-circuit: commands that rotate or revoke
	// the runner's bearer would 401-cascade every subsequent test. Skips
	// don't count toward MatrixSize (see finalizeLiveDogfoodReport).
	if !ctx.allowDestructive && isDestructiveAtAuth(command.Annotations, command.Path) {
		return []LiveDogfoodTestResult{
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestHelp, reasonDestructiveAtAuth),
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestHappy, reasonDestructiveAtAuth),
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestJSON, reasonDestructiveAtAuth),
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestError, reasonDestructiveAtAuth),
		}
	}

	helpArgs := append(append([]string{}, command.Path...), "--help")
	helpRun := runLiveDogfoodProcess(ctx.binaryPath, ctx.cliDir, helpArgs, ctx.timeout)
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

	mutating := liveDogfoodCommandMutates(command)
	useDryRun := mutating && commandSupportsDryRun(command.Help)

	resolvedArgs, resolveSkipped, resolveReason := resolveCommandPositionals(command, happyArgs, ctx)
	if resolveSkipped {
		results = append(results,
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestHappy, resolveReason),
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestJSON, resolveReason),
		)
	} else {
		happyArgs = resolvedArgs

		runArgs := happyArgs
		if useDryRun {
			runArgs = appendDryRunArg(happyArgs)
		}

		happyRun := runLiveDogfoodProcess(ctx.binaryPath, ctx.cliDir, runArgs, ctx.timeout)
		happyResult := liveDogfoodResult(commandName, LiveDogfoodTestHappy, runArgs, happyRun)
		if happyRun.exitCode == 0 {
			happyResult.Status = LiveDogfoodStatusPass
			happyResult.Reason = ""
		} else if liveDogfoodUnavailableForRunner(happyRun) {
			happyResult.Status = LiveDogfoodStatusSkip
			happyResult.Reason = reasonUnavailableRunnerCredentials
		}
		results = append(results, happyResult)

		if commandSupportsJSON(command.Help) {
			jsonArgs := appendJSONArg(runArgs)
			jsonRun := runLiveDogfoodProcess(ctx.binaryPath, ctx.cliDir, jsonArgs, ctx.timeout)
			jsonResult := liveDogfoodResult(commandName, LiveDogfoodTestJSON, jsonArgs, jsonRun)
			if jsonRun.exitCode == 0 {
				if !validLiveDogfoodJSONOutput(jsonRun.stdout) {
					jsonResult.Status = LiveDogfoodStatusFail
					jsonResult.Reason = "invalid JSON"
				} else {
					jsonResult.Status = LiveDogfoodStatusPass
					jsonResult.Reason = ""
				}
			} else if liveDogfoodUnavailableForRunner(jsonRun) {
				jsonResult.Status = LiveDogfoodStatusSkip
				jsonResult.Reason = reasonUnavailableRunnerCredentials
			}
			results = append(results, jsonResult)
		} else {
			results = append(results, skippedLiveDogfoodResult(commandName, LiveDogfoodTestJSON, "--json not supported"))
		}
	}

	if liveDogfoodCommandTakesArg(command.Help) {
		flagNames := extractFlagNames(command.Help)
		hasQueryFlag := slices.Contains(flagNames, "query")
		// Search-shape strategy is suppressed for mutating leaves so a
		// `delete --query=...` mass-delete is never probed with the
		// invalid-token sentinel against the live API.
		isSearch := commandSupportsSearch(command.Help) && !mutating
		suppliedJSON := slices.Contains(flagNames, "json")

		var errorArgs []string
		if isSearch {
			errorArgs = append([]string{}, command.Path...)
			if hasQueryFlag {
				errorArgs = append(errorArgs, "--query", "__printing_press_invalid__")
			} else {
				errorArgs = append(errorArgs, "__printing_press_invalid__")
			}
			if suppliedJSON {
				errorArgs = appendJSONArg(errorArgs)
			}
		} else {
			errorArgs = append(append([]string{}, command.Path...), "__printing_press_invalid__")
		}

		errorRun := runLiveDogfoodProcess(ctx.binaryPath, ctx.cliDir, errorArgs, ctx.timeout)
		errorResult := liveDogfoodResult(commandName, LiveDogfoodTestError, errorArgs, errorRun)

		if isSearch {
			// Real-world feed/content APIs return recent items as a fallback
			// for unmatched queries, so non-empty results under exit 0 are
			// not a failure signal. The only fail mode is invalid JSON when
			// the caller asked for --json.
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

	if useDryRun {
		if resolveSkipped {
			results = append(results, skippedLiveDogfoodResult(commandName, LiveDogfoodTestErrorReal, resolveReason))
		} else {
			results = append(results, skippedLiveDogfoodResult(commandName, LiveDogfoodTestErrorReal, reasonMutatingDryRunOnly))
		}
	}

	return results
}

// commandSupportsSearch reports whether a command behaves like a search:
// either it ships a --query flag, or its Usage suffix carries a <query>
// positional placeholder. Search-shape commands canonically return exit 0
// with empty (or fallback) results on no-match, so error_path treats them
// differently from mutating writes.
//
// Flag detection is scoped to the Flags: section so cross-references in
// Examples or Long descriptions (e.g., "see widgets list --query=foo") do
// not contaminate the heuristic.
func commandSupportsSearch(help string) bool {
	if slices.Contains(extractFlagNames(extractFlagsSection(help)), "query") {
		return true
	}
	return slices.Contains(extractPositionalPlaceholders(liveDogfoodUsageSuffix(help)), "query")
}

// extractFlagsSection returns the body of a Cobra `--help` "Flags:" or
// "Global Flags:" block — everything from the section header through the
// next blank line. Used to scope flag-name extraction so cross-reference
// strings outside the actual flag section can't trigger false positives.
func extractFlagsSection(help string) string {
	lines := strings.Split(help, "\n")
	var out []string
	inFlags := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Flags:" || trimmed == "Global Flags:" {
			inFlags = true
			continue
		}
		if inFlags {
			if trimmed == "" {
				inFlags = false
				continue
			}
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
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

const (
	endpointAnnotation        = "pp:endpoint"
	endpointMethodAnnotation  = "pp:method"
	endpointPathAnnotation    = "pp:path"
	mcpReadOnlyAnnotation     = "mcp:read-only"
	destructiveAuthAnnotation = "pp:destructive-auth"
)

// destructiveAuthTerms are case-insensitive command or endpoint tokens
// classifying a command as destructive-at-auth.
var destructiveAuthTerms = map[string]bool{
	"refresh":    true,
	"rotate":     true,
	"revoke":     true,
	"regenerate": true,
	"reset":      true,
	"cycle":      true,
}

var destructiveAuthResources = map[string]bool{
	"api-keys": true,
	"api_keys": true,
	"sessions": true,
	"tokens":   true,
}

// isDestructiveAtAuth reports whether a command can invalidate the bearer
// the live-dogfood runner is using. Reads pp:endpoint
// (authoritative for endpoint-mirror commands) and falls back to
// path-segment matching across the command path for novel commands.
// Read-only commands are exempt regardless of name.
func isDestructiveAtAuth(annotations map[string]string, commandPath []string) bool {
	if v, ok := annotations[destructiveAuthAnnotation]; ok {
		return annotationIsTrueValue(v)
	}
	if annotationIsTrueValue(annotations[mcpReadOnlyAnnotation]) {
		return false
	}
	if endpoint := annotations[endpointAnnotation]; endpoint != "" {
		if containsDestructiveAuthTerm(endpoint) {
			return true
		}
		if strings.EqualFold(strings.TrimSpace(annotations[endpointMethodAnnotation]), "DELETE") &&
			endpointTargetsAuthResource(endpoint, annotations[endpointPathAnnotation]) {
			return true
		}
		return false
	}
	return slices.ContainsFunc(commandPath, containsDestructiveAuthTerm)
}

func containsDestructiveAuthTerm(s string) bool {
	return slices.ContainsFunc(commandNameTokens(s), func(token string) bool {
		return destructiveAuthTerms[token]
	})
}

func endpointTargetsAuthResource(endpoint, path string) bool {
	for _, segment := range splitPath(path) {
		segment = strings.ToLower(strings.Trim(segment, "{}:"))
		if destructiveAuthResources[segment] {
			return true
		}
	}
	return slices.ContainsFunc(strings.Split(strings.ToLower(endpoint), "."), func(segment string) bool {
		return destructiveAuthResources[segment]
	})
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

func validLiveDogfoodJSONOutput(stdout string) bool {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return false
	}
	if json.Valid([]byte(trimmed)) {
		return true
	}
	for line := range strings.SplitSeq(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !json.Valid([]byte(line)) {
			return false
		}
	}
	return true
}

func liveDogfoodUnavailableForRunner(run liveDogfoodRun) bool {
	output := strings.ToLower(run.stdout + run.stderr)
	return strings.Contains(output, "http 403") ||
		strings.Contains(output, "permission denied") ||
		strings.Contains(output, "your credentials are valid but lack access")
}

func commandSupportsDryRun(help string) bool {
	return slices.Contains(extractFlagNames(help), "dry-run")
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

func appendDryRunArg(args []string) []string {
	out := append([]string{}, args...)
	for _, arg := range out {
		if arg == "--dry-run" || strings.HasPrefix(arg, "--dry-run=") {
			return out
		}
	}
	return append(out, "--dry-run")
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
	hasUnavailableRunnerSkip := false
	hasLiveHappyOrJSONPass := false
	for _, result := range report.Tests {
		switch result.Status {
		case LiveDogfoodStatusPass:
			report.Passed++
			report.MatrixSize++
			if (result.Kind == LiveDogfoodTestHappy || result.Kind == LiveDogfoodTestJSON) && !slices.Contains(result.Args, "--dry-run") {
				hasLiveHappyOrJSONPass = true
			}
		case LiveDogfoodStatusFail:
			report.Failed++
			report.MatrixSize++
		default:
			report.Skipped++
			if result.Reason == reasonUnavailableRunnerCredentials {
				hasUnavailableRunnerSkip = true
			}
		}
	}
	if hasUnavailableRunnerSkip && !hasLiveHappyOrJSONPass {
		report.Failed++
		report.MatrixSize++
		report.Tests = append(report.Tests, LiveDogfoodTestResult{
			Command: "live-dogfood",
			Kind:    LiveDogfoodTestHappy,
			Status:  LiveDogfoodStatusFail,
			Reason:  reasonNoLiveSignal,
		})
	}
	// Failed-or-empty wins. Skips are non-failures, but quick acceptance still
	// needs enough counted signal before it can write an acceptance marker.
	switch {
	case report.Failed > 0 || report.MatrixSize == 0:
		report.Verdict = "FAIL"
	case report.Level == "quick" && report.MatrixSize >= 4 && report.Passed+report.Skipped >= min(5, report.MatrixSize):
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
		TestsSkipped:  report.Skipped,
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
	case phase5AcceptanceLevelQuick, phase5AcceptanceLevelFull:
		return level, nil
	default:
		return "", fmt.Errorf("invalid live dogfood level %q (expected %s)", level, strings.Join(phase5AcceptedAcceptanceLevels, " or "))
	}
}
