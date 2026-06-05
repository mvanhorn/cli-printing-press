package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
const reasonMutatingErrorPath = "mutating command; error_path would call live API without --dry-run"
const reasonMutatingRunnableFixture = "blocked-fixture: mutating command requires runnable example"
const reasonNoLiveSignal = "no live happy/json pass; credential-unavailable skips cannot certify acceptance"
const reasonUnavailableRunnerCredentials = "unavailable for runner credentials"
const reasonFileFixtureRequired = "file fixture required"
const reasonRequiredParamFixture = "blocked-fixture: required API parameter"
const reasonNoErrorPathProbeAnnotation = "no-error-path-probe annotation"

// dogfoodEnvVar is the env signal every live-dogfood subprocess
// inherits. Generated commands with a long-running happy path detect
// this via cliutil.IsDogfoodEnv() and curtail work (paginate once,
// honor a smaller --limit) so the matrix's per-command timeout
// doesn't kill an otherwise healthy run.
const dogfoodEnvVar = "PRINTING_PRESS_DOGFOOD"
const liveDogfoodAuthTierEnvVar = "PP_AUTH_TIER"
const liveDogfoodAuthRetryDelay = time.Second

type LiveDogfoodOptions struct {
	CLIDir              string
	BinaryName          string
	Level               string
	Timeout             time.Duration
	WriteAcceptancePath string
	AuthEnv             string
	AuthTier            string
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
	stdout          string
	stderr          string
	stdoutTruncated bool
	exitCode        int
	err             error
}

func RunLiveDogfood(opts LiveDogfoodOptions) (*LiveDogfoodReport, error) {
	if strings.TrimSpace(opts.CLIDir) == "" {
		return nil, fmt.Errorf("CLIDir is required")
	}
	if isDeviceCLIDir(opts.CLIDir) {
		// Device (BLE) CLIs cannot be auto-driven by the generic live runner:
		// their actuating commands require an explicit --live flag, a physically
		// present/awake device, and domain-specific arguments the runner cannot
		// synthesize. Report a clean "unverified" outcome (manual --live testing
		// is the real Phase 5 gate for device CLIs) instead of crashing on the
		// missing agent-context command or failing a meaningless matrix.
		return &LiveDogfoodReport{
			Dir:     opts.CLIDir,
			Level:   opts.Level,
			Verdict: "unverified-device",
			Skipped: 1,
			RanAt:   time.Now().UTC(),
			Tests: []LiveDogfoodTestResult{{
				Command: "(device CLI)",
				Status:  LiveDogfoodStatusSkip,
				Reason:  "device CLI: live dogfood requires manual --live testing against the physical device",
			}},
		}, nil
	}
	homeScope, err := scopeLiveDogfoodSubprocessHome(opts.CLIDir, opts.BinaryName)
	if err != nil {
		return nil, err
	}
	defer homeScope.release()

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
		authTier:         resolveLiveDogfoodAuthTier(opts.AuthTier),
		allowDestructive: opts.AllowDestructive,
	}

	for _, command := range commands {
		commandName := strings.Join(command.Path, " ")
		report.Commands = append(report.Commands, commandName)
		report.Tests = append(report.Tests, runLiveDogfoodCommand(command, ctx)...)
	}

	finalizeLiveDogfoodReport(report)
	// The Phase 5.6 acceptance gate's contract is "marker from the runner on
	// every outcome": pass → promote, fail → hold-path, missing → "Phase 5
	// was skipped or not recorded." Writing only on PASS forced operators to
	// hand-author the FAIL marker, which the SKILL also forbids. Write on
	// every terminal verdict; phase5_gate.go already routes status:"fail"
	// to the hold path.
	if opts.WriteAcceptancePath != "" {
		if err := writeLiveDogfoodAcceptance(opts, report); err != nil {
			return nil, err
		}
	}
	if err := homeScope.syncBack(); err != nil {
		return report, err
	}
	return report, nil
}

type liveDogfoodHomeScope struct {
	release  func()
	syncBack func() error
}

func noopLiveDogfoodHomeScope() *liveDogfoodHomeScope {
	return &liveDogfoodHomeScope{
		release:  func() {},
		syncBack: func() error { return nil },
	}
}

func scopeLiveDogfoodSubprocessHome(cliDir, binaryName string) (*liveDogfoodHomeScope, error) {
	manifest, err := ReadCLIManifest(cliDir)
	if err == nil && manifest.IsLocalDatastore() && strings.EqualFold(strings.TrimSpace(manifest.AuthType), "none") {
		return noopLiveDogfoodHomeScope(), nil
	}
	cliName := strings.TrimSpace(manifest.CLIName)
	if cliName == "" {
		cliName = strings.TrimSpace(binaryName)
	}
	if cliName == "" {
		cliName = findCLIName(cliDir)
	}
	syncConfigBack := strings.EqualFold(strings.TrimSpace(manifest.AuthType), "oauth2_refresh")
	return scopeSubprocessHomeWithCredentialMirror(cliName, syncConfigBack)
}

func scopeSubprocessHomeWithCredentialMirror(cliName string, syncConfigBack bool) (*liveDogfoodHomeScope, error) {
	homeDir, removeHome, err := newScopedConfigHome()
	if err != nil {
		return nil, err
	}
	mirrors, err := mirrorLiveDogfoodCredentialFiles(homeDir, cliName, syncConfigBack)
	if err != nil {
		removeHome()
		return nil, err
	}
	restore := installScopedSubprocessHome(homeDir)
	return &liveDogfoodHomeScope{
		release: func() {
			restore()
			removeHome()
		},
		syncBack: func() error {
			return syncLiveDogfoodCredentialMirrors(mirrors)
		},
	}, nil
}

type liveDogfoodCredentialMirror struct {
	src      string
	dst      string
	original []byte
	mode     os.FileMode
}

func mirrorLiveDogfoodCredentialFiles(scopedHome, cliName string, syncConfigBack bool) ([]liveDogfoodCredentialMirror, error) {
	cliName = strings.TrimSpace(cliName)
	if scopedHome == "" || cliName == "" {
		return nil, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, nil
	}
	paths := []struct {
		rel      string
		syncBack bool
	}{
		{rel: filepath.Join(".config", cliName, "config.toml"), syncBack: syncConfigBack},
		{rel: filepath.Join(".config", cliName, "config.json"), syncBack: syncConfigBack},
		{rel: filepath.Join(".local", "share", cliName, "cookies.json")},
	}
	var mirrors []liveDogfoodCredentialMirror
	for _, path := range paths {
		src := filepath.Join(home, path.rel)
		dst := filepath.Join(scopedHome, path.rel)
		mirror, err := copyLiveDogfoodCredentialFile(src, dst)
		if err != nil {
			return nil, err
		}
		if path.syncBack && mirror != nil {
			mirrors = append(mirrors, *mirror)
		}
	}
	return mirrors, nil
}

func syncLiveDogfoodCredentialMirrors(mirrors []liveDogfoodCredentialMirror) error {
	for _, mirror := range mirrors {
		updated, err := os.ReadFile(mirror.dst)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("reading live dogfood credential mirror %s: %w", mirror.dst, err)
		}
		if bytes.Equal(updated, mirror.original) {
			continue
		}
		if err := writeLiveDogfoodCredentialFileIfUnchanged(mirror, updated); err != nil {
			return err
		}
	}
	return nil
}

func writeLiveDogfoodCredentialFileIfUnchanged(mirror liveDogfoodCredentialMirror, updated []byte) error {
	dir := filepath.Dir(mirror.src)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating live dogfood credential mirror directory for %s: %w", mirror.src, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(mirror.src)+".dogfood-sync-*")
	if err != nil {
		return fmt.Errorf("creating live dogfood credential sync temp file for %s: %w", mirror.src, err)
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(updated); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing live dogfood credential sync temp file %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(mirror.mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("setting live dogfood credential sync temp file mode %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing live dogfood credential sync temp file %s: %w", tmpName, err)
	}

	// Best-effort compare-and-swap: generated printed CLIs do not share a file
	// lock with live dogfood, so a non-cooperating writer can still race after
	// this final read. The temp-file rename keeps the sync-back atomic and this
	// last comparison catches operator edits made before live dogfood commits
	// the rotated credential.
	current, err := os.ReadFile(mirror.src)
	if err != nil {
		return fmt.Errorf("reading operator credential file before sync-back %s: %w", mirror.src, err)
	}
	if !bytes.Equal(current, mirror.original) {
		return fmt.Errorf("refusing to sync refreshed live dogfood credentials to %s: operator config changed during dogfood", mirror.src)
	}
	if err := os.Rename(tmpName, mirror.src); err != nil {
		return fmt.Errorf("writing live dogfood credential file %s: %w", mirror.src, err)
	}
	cleanup = false
	return nil
}

func writeLiveDogfoodCredentialMirrorFile(dst string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("creating live dogfood credential mirror directory for %s: %w", dst, err)
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		return fmt.Errorf("writing live dogfood credential file %s: %w", dst, err)
	}
	return nil
}

func copyLiveDogfoodCredentialFile(src, dst string) (*liveDogfoodCredentialMirror, error) {
	in, err := os.Open(src)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening live dogfood credential file %s: %w", src, err)
	}
	defer func() { _ = in.Close() }()

	info, err := in.Stat()
	if err != nil {
		return nil, fmt.Errorf("checking live dogfood credential file %s: %w", src, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("live dogfood credential file %s is not a regular file", src)
	}

	data, err := io.ReadAll(in)
	if err != nil {
		return nil, fmt.Errorf("reading live dogfood credential file %s: %w", src, err)
	}
	mode := info.Mode().Perm()
	if err := writeLiveDogfoodCredentialMirrorFile(dst, data, mode); err != nil {
		return nil, err
	}
	return &liveDogfoodCredentialMirror{
		src:      src,
		dst:      dst,
		original: data,
		mode:     mode,
	}, nil
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
	authTier         string
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
	// pp:happy-args already supplies real positional values, so the args are
	// authoritative — skip placeholder re-resolution, which would otherwise
	// overwrite them via the list companion or skip the command when no
	// companion is reachable.
	if strings.TrimSpace(command.Annotations[happyArgsAnnotation]) != "" {
		return happyArgs, false, ""
	}
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
	mutating := liveDogfoodCommandMutates(command)
	useDryRun := mutating && commandSupportsDryRun(command.Help)

	tierSkip := liveDogfoodRequiresTierSkipReason(command.Annotations, ctx.authTier)
	if tierSkip != "" {
		results = append(results,
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestHappy, tierSkip),
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestJSON, tierSkip),
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestError, tierSkip),
		)
		if useDryRun {
			results = append(results, skippedLiveDogfoodResult(commandName, LiveDogfoodTestErrorReal, tierSkip))
		}
		return results
	}

	happyArgs, ok := liveDogfoodHappyArgs(command)
	if !ok {
		if mutating {
			results = append(results,
				skippedLiveDogfoodResult(commandName, LiveDogfoodTestHappy, reasonMutatingRunnableFixture),
				skippedLiveDogfoodResult(commandName, LiveDogfoodTestJSON, reasonMutatingRunnableFixture),
				skippedLiveDogfoodResult(commandName, LiveDogfoodTestError, reasonMutatingRunnableFixture),
			)
			return results
		}
		results = append(results,
			failedLiveDogfoodResult(commandName, LiveDogfoodTestHappy, command.Path, "missing runnable example"),
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestJSON, "missing runnable example"),
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestError, "missing runnable example"),
		)
		return results
	}

	fixtureSkip := happyPathFileFixtureSkip(happyArgs, ctx.cliDir)
	resolvedArgs, resolveSkipped, resolveReason := resolveCommandPositionals(command, happyArgs, ctx)
	switch {
	case fixtureSkip != "":
		results = append(results,
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestHappy, fixtureSkip),
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestJSON, fixtureSkip),
		)
	case resolveSkipped:
		results = append(results,
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestHappy, resolveReason),
			skippedLiveDogfoodResult(commandName, LiveDogfoodTestJSON, resolveReason),
		)
	default:
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
		} else if requiredParamReason := liveDogfoodRequiredParamFixtureReason(happyRun); requiredParamReason != "" {
			happyResult.Status = LiveDogfoodStatusSkip
			happyResult.Reason = requiredParamReason
		}
		results = append(results, happyResult)

		if happyResult.Status == LiveDogfoodStatusSkip &&
			(happyResult.Reason == reasonUnavailableRunnerCredentials || happyResult.Reason == reasonRequiredParamFixture) {
			results = append(results, skippedLiveDogfoodResult(commandName, LiveDogfoodTestJSON, happyResult.Reason))
		} else if commandSupportsJSON(command.Help) {
			jsonArgs := appendJSONArg(runArgs)
			jsonRun := runLiveDogfoodProcess(ctx.binaryPath, ctx.cliDir, jsonArgs, ctx.timeout)
			jsonResult := liveDogfoodResult(commandName, LiveDogfoodTestJSON, jsonArgs, jsonRun)
			if jsonRun.exitCode == 0 {
				if jsonRun.stdoutTruncated || !validLiveDogfoodJSONOutput(jsonRun.stdout) {
					jsonResult.Status = LiveDogfoodStatusFail
					jsonResult.Reason = liveDogfoodInvalidJSONReason(jsonRun, "invalid JSON")
				} else {
					jsonResult.Status = LiveDogfoodStatusPass
					jsonResult.Reason = ""
				}
			} else if liveDogfoodUnavailableForRunner(jsonRun) {
				jsonResult.Status = LiveDogfoodStatusSkip
				jsonResult.Reason = reasonUnavailableRunnerCredentials
			} else if requiredParamReason := liveDogfoodRequiredParamFixtureReason(jsonRun); requiredParamReason != "" {
				jsonResult.Status = LiveDogfoodStatusSkip
				jsonResult.Reason = requiredParamReason
			}
			results = append(results, jsonResult)
		} else {
			results = append(results, skippedLiveDogfoodResult(commandName, LiveDogfoodTestJSON, "--json not supported"))
		}
	}

	takesArg := liveDogfoodCommandTakesArg(command.Help)
	if takesArg && annotationIsTrueValue(command.Annotations[noErrorPathProbeAnnotation]) {
		results = append(results, skippedLiveDogfoodResult(commandName, LiveDogfoodTestError, reasonNoErrorPathProbeAnnotation))
	} else if takesArg {
		if mutating {
			// Mutating commands cannot run the error_path probe safely: the
			// __printing_press_invalid__ sentinel is sent as a real argument
			// and many APIs accept arbitrary string fields (tag names, labels,
			// notes), turning the probe into a real create/update/delete with
			// no rollback. --dry-run injection is the happy_path-only safety
			// net; for the error_path we skip outright, mirroring how
			// error_path_real already skips below.
			results = append(results, skippedLiveDogfoodResult(commandName, LiveDogfoodTestError, reasonMutatingErrorPath))
		} else {
			flagNames := extractFlagNames(command.Help)
			hasQueryFlag := slices.Contains(flagNames, "query")
			isSearch := commandSupportsSearch(command.Help)
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
				case suppliedJSON && errorRun.stdoutTruncated:
					errorResult.Status = LiveDogfoodStatusFail
					errorResult.Reason = liveDogfoodInvalidJSONReason(errorRun, "invalid JSON under --json")
				case suppliedJSON && !json.Valid([]byte(errorRun.stdout)):
					errorResult.Status = LiveDogfoodStatusFail
					errorResult.Reason = liveDogfoodInvalidJSONReason(errorRun, "invalid JSON under --json")
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
		}
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
	deadline := time.Now().Add(timeout)
	run := runLiveDogfoodProcessOnce(binaryPath, cliDir, args, timeout)
	if !liveDogfoodRetryableAuth401(run) || time.Until(deadline) <= liveDogfoodAuthRetryDelay {
		return run
	}
	time.Sleep(liveDogfoodAuthRetryDelay)
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return run
	}
	return runLiveDogfoodProcessOnce(binaryPath, cliDir, args, remaining)
}

func runLiveDogfoodProcessOnce(binaryPath, cliDir string, args []string, timeout time.Duration) liveDogfoodRun {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Dir = cliDir
	applyDefaultSubprocessEnv(cmd)
	// Strip PRINTING_PRESS_VERIFY{,_LIVE_HTTP} from the subprocess env so an
	// operator who inherited them from a parent shell, CI runner, or
	// container image cannot silently noop the destructive live path.
	// The transport-layer short-circuit is for verify mock-mode only.
	cmd.Env = filterVerifyEnv(cmd.Env)
	cmd.Env = append(cmd.Env, dogfoodEnvVar+"=1")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	stdoutCap := &limitedWriter{w: stdout, remaining: liveDogfoodMaxOutputBytes}
	stderrCap := &limitedWriter{w: stderr, remaining: MaxErrorOutputBytes}
	cmd.Stdout = stdoutCap
	cmd.Stderr = stderrCap

	err := cmd.Run()
	result := liveDogfoodRun{
		stdout:          stdout.String(),
		stderr:          stderr.String(),
		stdoutTruncated: stdoutCap.truncated,
		exitCode:        0,
		err:             err,
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

func liveDogfoodRetryableAuth401(run liveDogfoodRun) bool {
	if run.exitCode == 0 {
		return false
	}
	return liveDogfoodAuth401(run)
}

func liveDogfoodInvalidJSONReason(run liveDogfoodRun, fallback string) string {
	if run.stdoutTruncated {
		return "output exceeded capture cap"
	}
	return fallback
}

func liveDogfoodResult(command string, kind LiveDogfoodTestKind, args []string, run liveDogfoodRun) LiveDogfoodTestResult {
	result := LiveDogfoodTestResult{
		Command:      command,
		Kind:         kind,
		Args:         append([]string{}, args...),
		Status:       LiveDogfoodStatusFail,
		ExitCode:     run.exitCode,
		OutputSample: sampleOutputParts(run.stdout, run.stderr),
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
	endpointAnnotation         = "pp:endpoint"
	endpointMethodAnnotation   = "pp:method"
	endpointPathAnnotation     = "pp:path"
	mcpReadOnlyAnnotation      = "mcp:read-only"
	destructiveAuthAnnotation  = "pp:destructive-auth"
	noErrorPathProbeAnnotation = "pp:no-error-path-probe"
	requiresTierAnnotation     = "pp:requires-tier"
	liveDogfoodMaxOutputBytes  = 10 << 20
)

var liveDogfoodRequiredParamFixturePhrases = []string{
	"missing parameter",
	"missing param",
	"required parameter",
	"required param",
	"must provide parameter",
	"must provide param",
	"please provide email",
}

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

// happyPathFileFixtureSkip returns a skip reason when the parsed Example
// references a file-flag value that doesn't exist on disk relative to
// cliDir. Flag names containing "file" or "csv" trigger the check; the
// motivating cases are `--file accounts.csv` / `--csv prospects.csv` shapes
// where the example would otherwise fail with `open <path>: no such file
// or directory`, masking the signal that the command is callable.
func happyPathFileFixtureSkip(args []string, cliDir string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			continue
		}
		name := strings.TrimPrefix(a, "--")
		var value string
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			value = name[eq+1:]
			name = name[:eq]
		} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			value = args[i+1]
			i++
		}
		if !flagNameSuggestsFile(name) {
			continue
		}
		if value == "" || strings.Contains(value, "://") {
			continue
		}
		if fileExistsRelativeTo(value, cliDir) {
			continue
		}
		return fmt.Sprintf("%s: --%s %s", reasonFileFixtureRequired, name, value)
	}
	return ""
}

func flagNameSuggestsFile(name string) bool {
	n := strings.ToLower(name)
	if n == "file" || n == "csv" {
		return true
	}
	// Anchor on a separator so `--profile` (contains "file") and similar
	// non-file flags don't trigger spurious skips. Common shapes covered:
	// `--input-file`, `--output_file`, `--import-csv`, `--config-csv`.
	return strings.HasSuffix(n, "-file") || strings.HasSuffix(n, "_file") ||
		strings.HasSuffix(n, "-csv") || strings.HasSuffix(n, "_csv")
}

func fileExistsRelativeTo(p, cliDir string) bool {
	if p == "" {
		return false
	}
	if filepath.IsAbs(p) {
		_, err := os.Stat(p)
		return err == nil
	}
	candidates := []string{p}
	if cliDir != "" {
		candidates = append(candidates, filepath.Join(cliDir, p))
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return true
		}
	}
	return false
}

func liveDogfoodHappyArgs(command liveDogfoodCommand) ([]string, bool) {
	// pp:happy-args supplies real happy-path args, overriding the Example-derived
	// placeholders (e.g. "--ids example-value") that strict upstream validators
	// reject with HTTP 400. Same `;`-separated `--flag=value` / `<name>=value`
	// grammar the runtime layer uses (parseHappyArgsAnnotation), so a single
	// annotation drives both surfaces.
	if raw := strings.TrimSpace(command.Annotations[happyArgsAnnotation]); raw != "" {
		parsed := parseHappyArgsAnnotation(raw)
		args := append([]string{}, command.Path...)
		args = append(args, parsed.positionals...)
		args = append(args, parsed.flags...)
		if len(args) > len(command.Path) {
			return args, true
		}
	}
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
		liveDogfoodAuth401Output(output) ||
		strings.Contains(output, "permission denied") ||
		strings.Contains(output, "your credentials are valid but lack access")
}

func liveDogfoodRequiredParamFixtureReason(run liveDogfoodRun) string {
	if run.exitCode == 0 {
		return ""
	}
	output := strings.ToLower(run.stdout + " " + run.stderr)
	if !strings.Contains(output, "http 400") && !strings.Contains(output, "http 422") {
		return ""
	}
	if containsAnyOf(output, liveDogfoodRequiredParamFixturePhrases) {
		return reasonRequiredParamFixture
	}
	return ""
}

func resolveLiveDogfoodAuthTier(flagValue string) string {
	if tier := strings.TrimSpace(flagValue); tier != "" {
		return tier
	}
	return strings.TrimSpace(os.Getenv(liveDogfoodAuthTierEnvVar))
}

func liveDogfoodRequiresTierSkipReason(annotations map[string]string, activeTier string) string {
	requiredTier := strings.TrimSpace(annotations[requiresTierAnnotation])
	if requiredTier == "" {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(activeTier), requiredTier) {
		return ""
	}
	return fmt.Sprintf("blocked-fixture: requires auth tier %q", requiredTier)
}

func liveDogfoodAuth401(run liveDogfoodRun) bool {
	return liveDogfoodAuth401Output(strings.ToLower(run.stdout + run.stderr))
}

func liveDogfoodAuth401Output(output string) bool {
	if !strings.Contains(output, "http 401") {
		return false
	}
	return strings.Contains(output, "couldn't authenticate") ||
		strings.Contains(output, "could not authenticate") ||
		strings.Contains(output, "not authenticated")
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
	// Identity (api_name/run_id) is recorded so `lock promote`'s cross-check
	// in validatePhase5Marker can reject stale markers. Three sources, in
	// order: the working-dir manifest (most authoritative — already merged
	// catalog/spec data), the runstate for this working dir (covers the
	// pre-promote case where generate has not written the manifest yet), and
	// finally an empty fall-back so dogfood still emits a marker for foreign
	// working dirs. The marker carries empty identity only when neither
	// source exists, which is the scenario where a downstream gate has no
	// manifest identity to compare against either.
	apiName, runID, authType := resolveLiveDogfoodAcceptanceIdentity(opts.CLIDir)
	if authType == "" {
		authType = "none"
	}

	status := "pass"
	var failureSummary *Phase5FailureSummary
	if report.Verdict != "PASS" {
		status = "fail"
		failureSummary = summarizeLiveDogfoodFailures(report)
	}

	marker := Phase5GateMarker{
		SchemaVersion: 1,
		APIName:       apiName,
		RunID:         runID,
		Status:        status,
		Level:         report.Level,
		MatrixSize:    report.MatrixSize,
		TestsPassed:   report.Passed,
		TestsSkipped:  report.Skipped,
		TestsFailed:   report.Failed,
		AuthContext: Phase5AuthContext{
			Type:            authType,
			APIKeyAvailable: opts.AuthEnv != "" && os.Getenv(opts.AuthEnv) != "",
		},
		FailureSummary: failureSummary,
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

// summarizeLiveDogfoodFailures groups failed test results by category so the
// fail-marker carries a one-glance triage hint. Categories mirror the
// retro's suggested buckets: transport-error, http-4xx, http-5xx,
// exit-nonzero, output-mismatch, other. Commands lists deduplicated command
// names that contributed at least one failure.
func summarizeLiveDogfoodFailures(report *LiveDogfoodReport) *Phase5FailureSummary {
	if report == nil {
		return nil
	}
	summary := &Phase5FailureSummary{}
	seen := map[string]bool{}
	for _, t := range report.Tests {
		if t.Status != LiveDogfoodStatusFail {
			continue
		}
		switch classifyLiveDogfoodFailure(t) {
		case "transport_error":
			summary.TransportError++
		case "http_4xx":
			summary.HTTP4xx++
		case "http_5xx":
			summary.HTTP5xx++
		case "exit_nonzero":
			summary.ExitNonzero++
		case "output_mismatch":
			summary.OutputMismatch++
		default:
			summary.Other++
		}
		if t.Command != "" && !seen[t.Command] {
			seen[t.Command] = true
			summary.Commands = append(summary.Commands, t.Command)
		}
	}
	if summary.TransportError == 0 && summary.HTTP4xx == 0 && summary.HTTP5xx == 0 &&
		summary.ExitNonzero == 0 && summary.OutputMismatch == 0 && summary.Other == 0 {
		return nil
	}
	sort.Strings(summary.Commands)
	return summary
}

// classifyLiveDogfoodFailure picks the failure bucket for one test result.
// The reason string and a small slice of the captured output (already
// truncated to OutputSample) are the only signals; classification is a
// best-effort hint, not a contract.
func classifyLiveDogfoodFailure(t LiveDogfoodTestResult) string {
	hay := strings.ToLower(t.Reason + " " + t.OutputSample)
	// 4xx is checked before 5xx: a legitimate 5xx response is unlikely to
	// also mention "http 4", whereas error strings citing 400/401/403/404
	// frequently start with digit 4 and would otherwise be shadowed if 5xx
	// were checked first (e.g., a retry-count log like
	// "retried http 5 times, status http 404").
	switch {
	case strings.Contains(hay, "http 4"):
		return "http_4xx"
	case strings.Contains(hay, "http 5"):
		return "http_5xx"
	case strings.Contains(hay, "connection refused") ||
		strings.Contains(hay, "no such host") ||
		strings.Contains(hay, "timeout") ||
		strings.Contains(hay, "dial tcp"):
		return "transport_error"
	// "invalid json" / "not json" match independently so the runner's own
	// Reason strings (literal "invalid JSON" at the two emit sites) bucket
	// here even when neither Reason nor OutputSample contains the word
	// "output". The "output" + "mismatch" conjunction stays as a separate
	// match for the schema-mismatch flavor of failure.
	case strings.Contains(hay, "invalid json") || strings.Contains(hay, "not json") ||
		(strings.Contains(hay, "output") && strings.Contains(hay, "mismatch")):
		return "output_mismatch"
	case t.ExitCode != 0:
		return "exit_nonzero"
	}
	return "other"
}

// resolveLiveDogfoodAcceptanceIdentity finds the marker's api_name, run_id,
// and auth_type. Manifest on disk wins (also yields auth_type); runstate
// fills in when the manifest hasn't been written yet (the pre-promote case
// from issue #963). I/O errors other than "not found" propagate as empty
// values rather than failing the write — emitting an incomplete marker
// beats blocking dogfood, and the gate cross-check catches identity drift
// on the way to promote.
func resolveLiveDogfoodAcceptanceIdentity(cliDir string) (apiName, runID, authType string) {
	if manifest, err := ReadCLIManifest(cliDir); err == nil {
		apiName = manifest.APIName
		runID = manifest.RunID
		authType = manifest.AuthType
	}
	if apiName != "" && runID != "" {
		return apiName, runID, authType
	}
	if state, err := FindStateByWorkingDir(cliDir); err == nil {
		if apiName == "" {
			apiName = state.APIName
		}
		if runID == "" {
			runID = state.RunID
		}
	}
	return apiName, runID, authType
}

func liveDogfoodQuickCommands(commands []liveDogfoodCommand) []liveDogfoodCommand {
	const quickTarget = 6
	if len(commands) <= quickTarget {
		return commands
	}
	selected := make([]liveDogfoodCommand, 0, quickTarget)
	selectedIndex := make(map[int]bool, quickTarget)
	seenFamily := map[string]bool{}
	for i, command := range commands {
		family := liveDogfoodCommandFamily(command)
		if family != "" {
			if seenFamily[family] {
				continue
			}
			seenFamily[family] = true
		}
		selected = append(selected, command)
		selectedIndex[i] = true
		if len(selected) == quickTarget {
			return selected
		}
	}
	for i, command := range commands {
		if selectedIndex[i] {
			continue
		}
		selected = append(selected, command)
		if len(selected) == quickTarget {
			return selected
		}
	}
	return selected
}

func liveDogfoodCommandFamily(command liveDogfoodCommand) string {
	if len(command.Path) == 0 {
		return ""
	}
	return command.Path[0]
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
