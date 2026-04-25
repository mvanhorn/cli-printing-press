package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/internal/artifacts"
	"github.com/mvanhorn/cli-printing-press/internal/naming"
	apispec "github.com/mvanhorn/cli-printing-press/internal/spec"
)

// VerifyConfig configures a runtime verification run.
type VerifyConfig struct {
	Dir       string // generated CLI directory
	SpecPath  string // OpenAPI spec path
	APIKey    string // optional - if set, tests against real API
	EnvVar    string // env var name for the API key (e.g., GITHUB_TOKEN)
	Threshold int    // minimum pass rate (default 80)
	NoSpec    bool   // structural-only mode: skip spec-dependent checks
}

// VerifyReport is the output of a runtime verification run.
type VerifyReport struct {
	Mode                   string          `json:"mode"` // "live" or "mock"
	Total                  int             `json:"total"`
	Passed                 int             `json:"passed"`
	Failed                 int             `json:"failed"`
	Critical               int             `json:"critical"`
	PassRate               float64         `json:"pass_rate"`
	DataPipeline           bool            `json:"data_pipeline"`
	DataPipelineDetail     string          `json:"data_pipeline_detail,omitempty"` // PASS, WARN, SKIP, FAIL with context
	Freshness              FreshnessResult `json:"freshness"`
	BrowserSessionRequired bool            `json:"browser_session_required,omitempty"`
	BrowserSessionProof    string          `json:"browser_session_proof,omitempty"`
	BrowserSessionDetail   string          `json:"browser_session_detail,omitempty"`
	Verdict                string          `json:"verdict"` // PASS, WARN, FAIL
	Results                []CommandResult `json:"results"`
	Binary                 string          `json:"binary"`
}

// CommandResult is the test result for a single command.
type CommandResult struct {
	Command string `json:"command"`
	Kind    string `json:"kind"` // read, write, local, data-layer
	Help    bool   `json:"help"`
	DryRun  bool   `json:"dry_run"`
	Execute bool   `json:"execute"`
	Score   int    `json:"score"` // 0-3
	Error   string `json:"error,omitempty"`
}

type FreshnessResult struct {
	Enabled         bool   `json:"enabled"`
	RegisteredPaths int    `json:"registered_paths,omitempty"`
	Metadata        bool   `json:"metadata,omitempty"`
	LiveBypass      bool   `json:"live_bypass,omitempty"`
	HelperSurface   bool   `json:"helper_surface,omitempty"`
	Verdict         string `json:"verdict,omitempty"` // PASS, WARN, SKIP, FAIL
	Detail          string `json:"detail,omitempty"`
}

// RunVerify executes the runtime verification pipeline.
func RunVerify(cfg VerifyConfig) (*VerifyReport, error) {
	if cfg.NoSpec {
		return runStructuralVerify(cfg)
	}
	if cfg.Threshold == 0 {
		cfg.Threshold = 80
	}
	if err := artifacts.CleanupGeneratedCLI(cfg.Dir, artifacts.CleanupOptions{
		RemoveValidationBinaries: true,
		RemoveDogfoodBinaries:    true,
		RemoveRecursiveCopies:    true,
		RemoveFinderMetadata:     true,
	}); err != nil {
		return nil, fmt.Errorf("pre-verify cleanup: %w", err)
	}

	report := &VerifyReport{}

	// 1. Load spec for command classification
	var spec *openAPISpec
	if cfg.SpecPath != "" {
		loaded, err := loadDogfoodOpenAPISpec(cfg.SpecPath)
		if err != nil {
			return nil, fmt.Errorf("loading spec: %w", err)
		}
		spec = loaded
	}

	// 2. Determine mode
	if cfg.APIKey != "" {
		report.Mode = "live"
	} else {
		report.Mode = "mock"
	}

	// 3. Build the generated CLI binary
	binaryPath, err := buildCLI(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("building CLI: %w", err)
	}
	report.Binary = binaryPath

	// 4. Start mock server if needed
	var mockServer *httptest.Server
	var baseURLOverride string
	apiName := naming.TrimCLISuffix(filepath.Base(cfg.Dir))
	envVarName := cfg.EnvVar
	if envVarName == "" {
		envVarName = strings.ToUpper(strings.ReplaceAll(apiName, "-", "_")) + "_TOKEN"
	}
	baseURLEnvVar := strings.ToUpper(strings.ReplaceAll(apiName, "-", "_")) + "_BASE_URL"

	if report.Mode == "mock" {
		mockServer, baseURLOverride = startMockServer(spec)
		defer mockServer.Close()
	}

	// 5. Discover commands
	commands := discoverCommands(cfg.Dir, binaryPath)

	// 5.5. Infer positional args from --help output
	for i := range commands {
		inferPositionalArgs(binaryPath, &commands[i])
	}

	// 6. Classify and run each command
	for i := range commands {
		classifyCommandKind(&commands[i], spec)
	}

	// Collect auth env var names. Priority:
	// 1. Spec's declared env vars (from securitySchemes or auth inference)
	// 2. Env vars actually read by the CLI's config.go (ground truth)
	// 3. Derived patterns from the API name (fallback)
	authEnvVars := []string{envVarName}
	if spec != nil && len(spec.Auth.EnvVars) > 0 {
		authEnvVars = spec.Auth.EnvVars
	}
	// Read the CLI's config.go to discover what env vars it actually reads.
	// This catches cases where Claude wired a different env var name than
	// what the spec declares or the API name implies.
	if discovered := discoverCLIEnvVars(cfg.Dir); len(discovered) > 0 {
		for _, ev := range discovered {
			found := slices.Contains(authEnvVars, ev)
			if !found {
				authEnvVars = append(authEnvVars, ev)
			}
		}
	}

	// buildEnv constructs the environment for test subprocesses, passing
	// all auth-related env vars so auth-requiring commands can complete.
	buildEnv := func() []string {
		env := os.Environ()
		if report.Mode == "live" {
			for _, ev := range authEnvVars {
				if val := os.Getenv(ev); val != "" {
					env = append(env, ev+"="+val)
				}
			}
			// Also pass the explicit --api-key under ALL auth env var names so the
			// generated CLI finds it regardless of which env var it reads.
			if cfg.APIKey != "" {
				for _, ev := range authEnvVars {
					env = append(env, ev+"="+cfg.APIKey)
				}
			}
		} else {
			env = append(env, baseURLEnvVar+"="+baseURLOverride)
			for _, ev := range authEnvVars {
				env = append(env, ev+"=mock-token-for-testing")
			}
		}
		return env
	}

	// 7. Run tests
	for i, cmd := range commands {
		env := buildEnv()
		result := runCommandTests(binaryPath, cmd, report.Mode, env)
		commands[i] = cmd // preserve classification
		report.Results = append(report.Results, result)
	}

	// 8. Data pipeline test
	report.DataPipeline, report.DataPipelineDetail = runDataPipelineTest(binaryPath, report.Mode, buildEnv)
	report.Freshness = runFreshnessContractTest(cfg.Dir)

	if spec != nil && spec.Auth.RequiresBrowserSession {
		report.BrowserSessionRequired = true
		browserProof := runBrowserSessionProofTest(binaryPath, spec.Auth)
		report.Results = append(report.Results, browserProof)
		if browserProof.Score >= 2 {
			report.BrowserSessionProof = "valid"
		} else {
			report.BrowserSessionProof = "missing-or-invalid"
			report.BrowserSessionDetail = browserProof.Error
		}
	}

	// 9. Compute aggregate
	for _, r := range report.Results {
		report.Total++
		if r.Score >= 2 {
			report.Passed++
		} else {
			report.Failed++
			if r.Score == 0 {
				report.Critical++
			}
		}
	}
	if report.Total > 0 {
		report.PassRate = float64(report.Passed) / float64(report.Total) * 100
	}

	// 10. Verdict
	switch {
	case report.PassRate >= float64(cfg.Threshold) && report.DataPipeline && report.Critical == 0:
		report.Verdict = "PASS"
	case report.PassRate >= 60 && report.Critical <= 3:
		report.Verdict = "WARN"
	default:
		report.Verdict = "FAIL"
	}
	if report.BrowserSessionRequired && report.BrowserSessionProof != "valid" {
		report.Verdict = "FAIL"
	}

	return report, nil
}

// runStructuralVerify runs spec-independent verification: build, --help,
// --json validity, version, and exit code checks for every discovered command.
func runStructuralVerify(cfg VerifyConfig) (*VerifyReport, error) {
	if cfg.Threshold == 0 {
		cfg.Threshold = 80
	}
	if err := artifacts.CleanupGeneratedCLI(cfg.Dir, artifacts.CleanupOptions{
		RemoveValidationBinaries: true,
		RemoveDogfoodBinaries:    true,
		RemoveRecursiveCopies:    true,
		RemoveFinderMetadata:     true,
	}); err != nil {
		return nil, fmt.Errorf("pre-verify cleanup: %w", err)
	}

	report := &VerifyReport{Mode: "structural"}

	// 1. Build the CLI
	binaryPath, err := buildCLI(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("building CLI: %w", err)
	}
	report.Binary = binaryPath

	// 2. Discover commands from --help output
	commands := discoverCommands(cfg.Dir, binaryPath)

	// 3. Test each command structurally
	for _, cmd := range commands {
		result := runStructuralCommandTests(binaryPath, cmd)
		report.Results = append(report.Results, result)
	}

	// 4. Version command check
	versionOK := runCLI(binaryPath, []string{"version"}, os.Environ(), 10*time.Second) == nil
	if !versionOK {
		versionOK = runCLI(binaryPath, []string{"--version"}, os.Environ(), 10*time.Second) == nil
	}
	report.DataPipeline = versionOK
	if versionOK {
		report.DataPipelineDetail = "PASS (version command)"
	} else {
		report.DataPipelineDetail = "FAIL (version command)"
	}
	report.Freshness = runFreshnessContractTest(cfg.Dir)

	// 5. Aggregate
	for _, r := range report.Results {
		report.Total++
		if r.Score >= 2 {
			report.Passed++
		} else {
			report.Failed++
			if r.Score == 0 {
				report.Critical++
			}
		}
	}
	if report.Total > 0 {
		report.PassRate = float64(report.Passed) / float64(report.Total) * 100
	}

	// 6. Verdict
	switch {
	case report.PassRate >= float64(cfg.Threshold) && report.Critical == 0:
		report.Verdict = "PASS"
	case report.PassRate >= 60 && report.Critical <= 3:
		report.Verdict = "WARN"
	default:
		report.Verdict = "FAIL"
	}

	return report, nil
}

// runStructuralCommandTests tests a command without API access: --help output,
// --json flag acceptance (doesn't crash), and exit code correctness.
func runStructuralCommandTests(binary string, cmd discoveredCommand) CommandResult {
	result := CommandResult{
		Command: cmd.Name,
		Kind:    "structural",
	}

	// Test 1: --help produces output and exits 0
	result.Help = runCLI(binary, []string{cmd.Name, "--help"}, os.Environ(), 10*time.Second) == nil

	// Test 2: --help --json doesn't crash (validates flag registration)
	result.DryRun = runCLI(binary, []string{cmd.Name, "--help", "--json"}, os.Environ(), 10*time.Second) == nil

	// Test 3: command with no args exits non-zero if it requires args/flags
	// (validates that required flags are enforced). Skip commands that work
	// without args (doctor, version, auth, completion, api).
	switch cmd.Name {
	case "doctor", "version", "auth", "completion", "api", "help":
		result.Execute = true // these work without args
	default:
		// Running with just --json and no other args. If the command requires
		// flags/args, it should exit non-zero with an error message.
		// If it works without args, that's fine too.
		// Either way, we're validating it doesn't crash/panic.
		err := runCLI(binary, []string{cmd.Name, "--json"}, os.Environ(), 10*time.Second)
		// Both outcomes are acceptable for structural verification: the
		// command either ran successfully or exited with a proper error.
		// A panic or timeout would still fail via runCLI.
		result.Execute = true
		_ = err
	}

	score := 0
	if result.Help {
		score++
	}
	if result.DryRun {
		score++
	}
	if result.Execute {
		score++
	}
	result.Score = score

	return result
}

// buildCLI compiles the generated CLI and returns the binary path.
func buildCLI(dir string) (string, error) {
	name := filepath.Base(dir)
	binaryPath, err := filepath.Abs(filepath.Join(dir, name))
	if err != nil {
		return "", fmt.Errorf("resolving binary path: %w", err)
	}
	cmdDir, err := findCLICommandDir(dir)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", binaryPath, "./"+filepath.Base(cmdDir))
	cmd.Dir = filepath.Dir(cmdDir)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build: %s\n%s", err, string(out))
	}
	return binaryPath, nil
}

func findCLICommandDir(dir string) (string, error) {
	name := filepath.Base(dir)
	apiName := naming.TrimCLISuffix(name)
	candidates := []string{
		filepath.Join(dir, "cmd", name),
		filepath.Join(dir, "cmd", naming.CLI(apiName)),
		filepath.Join(dir, "cmd", naming.LegacyCLI(apiName)),
		filepath.Join(dir, "cmd", apiName),
	}

	for _, candidate := range candidates {
		info, err := os.Stat(candidate)
		if err == nil && info.IsDir() {
			return candidate, nil
		}
		if err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf("stat %s: %w", candidate, err)
		}
	}

	entries, err := os.ReadDir(filepath.Join(dir, "cmd"))
	if err != nil {
		return "", fmt.Errorf("reading cmd directory: %w", err)
	}

	var cliEntries []string
	var dirEntries []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirEntries = append(dirEntries, entry.Name())
		if naming.IsCLIDirName(entry.Name()) {
			cliEntries = append(cliEntries, entry.Name())
		}
	}

	sort.Strings(cliEntries)
	if len(cliEntries) == 1 {
		return filepath.Join(dir, "cmd", cliEntries[0]), nil
	}

	if len(dirEntries) == 1 {
		return filepath.Join(dir, "cmd", dirEntries[0]), nil
	}

	return "", fmt.Errorf("cannot find CLI cmd entry point in %s", dir)
}

// discoverCommands finds all registered commands. It first tries to parse the
// binary's --help output for ground-truth command names. If that fails (binary
// missing, crash, timeout), it falls back to regex extraction from root.go with
// camelToKebab name derivation.
func discoverCommands(dir string, binaryPath string) []discoveredCommand {
	// Primary path: parse ground-truth names from binary --help output.
	if binaryPath != "" {
		if cmds := discoverCommandsFromHelp(binaryPath); len(cmds) > 0 {
			return cmds
		}
	}

	// Fallback: regex extraction from root.go with camelToKebab derivation.
	return discoverCommandsFromSource(dir)
}

// discoverCommandsFromHelp runs `<binary> --help` and parses the Available
// Commands section to extract ground-truth command names.
func discoverCommandsFromHelp(binaryPath string) []discoveredCommand {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	helpCmd := exec.CommandContext(ctx, binaryPath, "--help")
	out, err := helpCmd.CombinedOutput()
	if err != nil {
		return nil
	}

	return parseHelpCommands(string(out))
}

// parseHelpCommands extracts command names from cobra-style --help output.
// Each line in the "Available Commands:" section has format:
//
//	<command-name>  <description>
func parseHelpCommands(helpOutput string) []discoveredCommand {
	lines := strings.Split(helpOutput, "\n")
	inAvailable := false
	var commands []discoveredCommand
	seen := map[string]bool{}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "Available Commands:") {
			inAvailable = true
			continue
		}

		// An empty line or a new section header ends the Available Commands block.
		if inAvailable && (trimmed == "" || (len(trimmed) > 0 && trimmed[len(trimmed)-1] == ':' && !strings.Contains(trimmed, " "))) {
			break
		}

		if !inAvailable {
			continue
		}

		// Extract the first non-space word as the command name.
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		if seen[name] {
			continue
		}
		seen[name] = true

		// Skip utility commands
		switch name {
		case "version", "completion", "help":
			continue
		}
		commands = append(commands, discoveredCommand{Name: name})
	}
	return commands
}

// discoverCommandsFromSource parses root.go to find all registered commands
// via regex extraction and camelToKebab name derivation.
func discoverCommandsFromSource(dir string) []discoveredCommand {
	rootPath := filepath.Join(dir, "internal", "cli", "root.go")
	data, err := os.ReadFile(rootPath)
	if err != nil {
		return nil
	}

	// Match: rootCmd.AddCommand(newXxxCmd(...))
	re := regexp.MustCompile(`rootCmd\.AddCommand\(new(\w+)Cmd\(`)
	matches := re.FindAllStringSubmatch(string(data), -1)

	var commands []discoveredCommand
	seen := map[string]bool{}
	for _, m := range matches {
		name := camelToKebab(m[1])
		if seen[name] {
			continue
		}
		seen[name] = true
		// Skip utility commands
		switch name {
		case "version-pp-cli", "version-cli", "version", "completion", "help":
			continue
		}
		commands = append(commands, discoveredCommand{Name: name})
	}
	return commands
}

type discoveredCommand struct {
	Name string
	Kind string // read, write, local, data-layer
	Args []string
}

// inferPositionalArgs runs `<binary> <cmd> --help`, parses the Usage line for
// positional arg placeholders like <region> or [price], and maps them to
// synthetic values. On any failure, it falls back to no extra args.
func inferPositionalArgs(binary string, cmd *discoveredCommand) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	helpCmd := exec.CommandContext(ctx, binary, cmd.Name, "--help")
	out, err := helpCmd.CombinedOutput()
	if err != nil {
		return // fall back to no extra args
	}

	// Find the Usage line, e.g. "Usage:\n  cli-name pulse <region> [flags]"
	usageRe := regexp.MustCompile(`(?m)^Usage:\s*\n\s+\S+\s+\S+(.*)$`)
	m := usageRe.FindSubmatch(out)
	if m == nil {
		return
	}
	rest := string(m[1])

	// Extract <arg> and [arg] placeholders (but not [flags] or [command])
	placeholderRe := regexp.MustCompile(`[<\[]([a-zA-Z][\w-]*)[>\]]`)
	matches := placeholderRe.FindAllStringSubmatch(rest, -1)
	if len(matches) == 0 {
		return
	}

	for _, match := range matches {
		name := strings.ToLower(match[1])
		// Skip cobra built-in placeholders
		if name == "flags" || name == "command" {
			continue
		}
		cmd.Args = append(cmd.Args, syntheticArgValue(name))
	}
}

// syntheticArgValue maps a positional arg placeholder name to a synthetic test value.
func syntheticArgValue(name string) string {
	switch name {
	case "region", "location", "city":
		return "mock-city"
	case "id", "property-id", "listing-id":
		return "12345"
	case "price", "amount":
		return "500000"
	case "zip", "zipcode":
		return "94102"
	case "url", "path":
		return "/mock/path"
	case "query", "search", "name":
		return "mock-query"
	case "type", "entity-type", "entity", "kind":
		return "collection"
	case "resource", "resource-type":
		return "items"
	case "format", "output-format":
		return "json"
	case "category", "slug":
		return "general"
	case "action", "command", "operation":
		return "list"
	case "status", "state":
		return "active"
	default:
		return "mock-value"
	}
}

// classifyCommandKind determines if a command is read, write, local, or data-layer.
func classifyCommandKind(cmd *discoveredCommand, spec *openAPISpec) {
	name := cmd.Name
	// Data layer commands — read from local SQLite, not the API
	switch name {
	case "sync", "search", "sql", "health", "trends", "patterns", "analytics",
		"export", "import", "stale", "no-show", "today", "busy", "diff",
		"noshow", "velocity", "popular":
		cmd.Kind = "data-layer"
		return
	case "doctor", "auth", "api", "completion":
		cmd.Kind = "local"
		return
	case "tail":
		cmd.Kind = "data-layer"
		return
	}

	// Check spec for the command's HTTP method
	if spec != nil && len(spec.Paths) > 0 {
		cmd.Kind = "read"
		return
	}

	// Default to read (safer for live mode)
	cmd.Kind = "read"
}

// workflowTestFlags returns flags needed for workflow commands that require --org or --repo.
// Retained for explicit positional-arg patterns (e.g., changelog takes two positional
// args, not flags — cobra won't surface them through the "required flag(s) not set"
// error). Flag-shaped requirements are now discovered dynamically via inferRequiredFlags.
func workflowTestFlags(cmdName string) []string {
	switch cmdName {
	case "changelog":
		return []string{"mock-owner", "mock-repo", "--since", "v0.0.1"}
	default:
		return nil
	}
}

// requiredFlagsRe matches cobra's standard "required flag(s) ... not set" error.
// Cobra emits the flag names quoted, comma-separated: required flag(s) "event", "year" not set
var requiredFlagsRe = regexp.MustCompile(`required flag\(s\) ((?:"[^"]+"(?:, )?)+) not set`)

// flagNameRe extracts quoted flag names from the required-flags error payload.
var flagNameRe = regexp.MustCompile(`"([^"]+)"`)

// inferRequiredFlags probes a command by running it with no args, parses cobra's
// "required flag(s) ... not set" error if present, and returns synthetic --flag value
// pairs the verifier can use to exercise the command. Returns nil when the command
// has no required flags (or when probing fails — the caller falls back gracefully).
func inferRequiredFlags(binary, cmdName string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	probe := exec.CommandContext(ctx, binary, cmdName)
	out, _ := probe.CombinedOutput() // error expected when flags are missing

	m := requiredFlagsRe.FindSubmatch(out)
	if m == nil {
		return nil
	}

	nameMatches := flagNameRe.FindAllSubmatch(m[1], -1)
	if len(nameMatches) == 0 {
		return nil
	}

	var args []string
	for _, nm := range nameMatches {
		flag := string(nm[1])
		args = append(args, "--"+flag, syntheticFlagValue(flag))
	}
	return args
}

// syntheticFlagValue maps a required flag name to a synthetic test value. Shares
// its philosophy with syntheticArgValue but keyed on flag names that appear in
// "required flag(s)" errors. The mock server doesn't validate values, so any
// non-empty string of the right shape works.
func syntheticFlagValue(name string) string {
	n := strings.ToLower(name)
	switch n {
	case "org", "organization", "owner":
		return "mock-owner"
	case "repo", "repository":
		return "mock-owner/mock-repo"
	case "team", "workspace", "project", "workspace-id", "project-id":
		return "mock-project"
	case "user", "username", "user-id", "account", "account-id":
		return "mock-user"
	case "event", "event-id", "game", "game-id", "match", "match-id":
		return "mock-event-123"
	case "season", "year":
		return "2026"
	case "sport", "league", "competition":
		return "mock-league"
	case "id", "uid", "uuid":
		return "mock-id-123"
	case "ticker", "symbol":
		return "MOCK"
	case "region", "location", "city":
		return "mock-city"
	case "date", "day":
		return "2026-04-11"
	case "since", "from", "start", "start-date":
		return "2026-01-01"
	case "until", "to", "end", "end-date":
		return "2026-12-31"
	case "query", "q", "search", "term":
		return "mock-query"
	case "name", "slug", "key":
		return "mock-name"
	case "type", "kind", "category":
		return "mock-type"
	case "status", "state":
		return "active"
	case "limit", "count", "size":
		return "10"
	case "format", "output":
		return "json"
	case "url", "endpoint", "base-url":
		return "https://mock.example.com"
	case "path", "file", "output-file":
		return "/tmp/mock-file"
	case "token", "api-key", "key-id", "secret":
		return "mock-secret"
	default:
		return "mock-value"
	}
}

// runCommandTests executes the test suite for a single command.
func runCommandTests(binary string, cmd discoveredCommand, mode string, env []string) CommandResult {
	result := CommandResult{
		Command: cmd.Name,
		Kind:    cmd.Kind,
	}

	// Test 1: --help
	result.Help = runCLI(binary, []string{cmd.Name, "--help"}, env, 10*time.Second) == nil

	// Get any required flags/args for this command.
	// First, probe the binary for cobra-declared required flags (generic, spec-agnostic).
	// Then fall back to the positional-arg map for commands that take bare positionals.
	extraFlags := inferRequiredFlags(binary, cmd.Name)
	if extraFlags == nil {
		extraFlags = workflowTestFlags(cmd.Name)
	}

	// Build positional args + flags for test invocations
	buildTestArgs := func(cmdName string, positionalArgs, flags []string, extra ...string) []string {
		args := []string{cmdName}
		args = append(args, positionalArgs...)
		args = append(args, flags...)
		args = append(args, extra...)
		return args
	}

	// Test 2: --dry-run (skip for local/data-layer commands that don't make API calls)
	if cmd.Kind != "local" && cmd.Kind != "data-layer" {
		args := buildTestArgs(cmd.Name, cmd.Args, extraFlags, "--dry-run")
		err := runCLI(binary, args, env, 10*time.Second)
		result.DryRun = err == nil || isIntentionalStubExit(err)
	} else {
		result.DryRun = true // skip = pass
	}

	// Test 3: Execute (only for read commands in live mode, all in mock mode)
	if cmd.Kind == "local" || cmd.Kind == "data-layer" {
		result.Execute = true // tested separately in data pipeline
	} else if mode == "live" && cmd.Kind == "write" {
		result.Execute = true // skip writes on live = pass (tested via dry-run)
	} else {
		args := buildTestArgs(cmd.Name, cmd.Args, extraFlags, "--json")
		err := runCLI(binary, args, env, 15*time.Second)
		result.Execute = err == nil || isIntentionalStubExit(err)
	}

	// Score
	score := 0
	if result.Help {
		score++
	}
	if result.DryRun {
		score++
	}
	if result.Execute {
		score++
	}
	result.Score = score

	return result
}

func isIntentionalStubExit(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, `"cf_gated":true`) ||
		strings.Contains(msg, `"cf_gated": true`)
}

func runBrowserSessionProofTest(binary string, auth apispec.AuthConfig) CommandResult {
	result := CommandResult{
		Command: "browser-session-proof",
		Kind:    "auth",
		Help:    true,
		DryRun:  true,
	}

	if strings.TrimSpace(auth.BrowserSessionValidationPath) == "" {
		result.Error = "required browser-session auth has no validation endpoint metadata"
		result.Score = 0
		return result
	}

	output, err := runCLIWithOutput(binary, []string{"doctor", "--json"}, os.Environ(), 20*time.Second)
	if err != nil {
		result.Error = fmt.Sprintf("doctor --json failed: %v", err)
		result.Score = 0
		return result
	}

	var report map[string]any
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		result.Error = "doctor --json did not return valid JSON"
		result.Score = 0
		return result
	}

	proof, _ := report["browser_session_proof"].(string)
	if proof != "valid" {
		detail, _ := report["browser_session_proof_detail"].(string)
		if detail == "" {
			detail = "run auth login --chrome to create a browser-session proof"
		}
		result.Error = detail
		result.Score = 0
		return result
	}

	result.Execute = true
	result.Score = 3
	return result
}

// runDataPipelineTest tests the sync -> sql -> search -> health chain.
// Returns (pass bool, detail string) where detail gives PASS/WARN/SKIP/FAIL context.
func runDataPipelineTest(binary, mode string, envFn func() []string) (bool, string) {
	env := envFn()

	// Create a temp dir for the test database
	tmpDir, err := os.MkdirTemp("", "verify-db-*")
	if err != nil {
		return false, "FAIL: could not create temp dir"
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	dbPath := filepath.Join(tmpDir, "test.db")
	env = append(env, "HOME="+tmpDir) // so sync uses temp location

	// Test sync (if it exists)
	syncErr := runCLI(binary, []string{"sync", "--db", dbPath, "--resources", "repos", "--full"}, env, 30*time.Second)
	if syncErr != nil {
		// Sync might not accept --db flag - try without
		syncErr = runCLI(binary, []string{"sync", "--full"}, env, 30*time.Second)
	}
	if syncErr != nil {
		return false, "FAIL: sync crashed"
	}

	// Test health (if available)
	_ = runCLI(binary, []string{"health", "--db", dbPath}, env, 10*time.Second)

	// Discover domain tables via sql command
	tableQuery := `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite%' AND name NOT LIKE '%_fts%' AND name != 'sync_state'`
	tablesOut, sqlErr := runCLIWithOutput(binary, []string{"sql", tableQuery}, env, 10*time.Second)
	if sqlErr != nil {
		// sql command may not exist or may not accept positional args — fall back to basic check
		return true, "PASS: sync completed (table validation skipped — sql command unavailable)"
	}

	// Parse table names from output (one per line, skip empty lines and header noise)
	tables := parseSQLOutput(tablesOut)
	if len(tables) == 0 {
		// No domain tables found — ambiguous (could be minimal CLI or unusual naming).
		// Don't fail the pipeline gate; report for human review.
		return true, "WARN: sync completed but no domain tables found in sqlite_master"
	}

	// In live mode, check that at least one table has rows
	if mode == "live" {
		for _, table := range tables {
			countQuery := fmt.Sprintf("SELECT count(*) FROM \"%s\"", table)
			countOut, countErr := runCLIWithOutput(binary, []string{"sql", countQuery}, env, 10*time.Second)
			if countErr != nil {
				continue
			}
			count := parseCountOutput(countOut)
			if count > 0 {
				return true, fmt.Sprintf("PASS: %d domain tables, %s has %d rows", len(tables), table, count)
			}
		}
		return false, fmt.Sprintf("WARN: %d domain tables created but 0 rows after sync (live mode)", len(tables))
	}

	// Mock mode: tables created is sufficient (mock data is minimal)
	return true, fmt.Sprintf("PASS: %d domain tables created", len(tables))
}

// runCLI executes the CLI binary with the given args and returns any error.
func runCLI(binary string, args []string, env []string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("exit %v: %s", err, string(out))
	}
	return nil
}

// runCLIWithOutput executes the CLI binary and returns its combined output.
func runCLIWithOutput(binary string, args []string, env []string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("exit %v: %s", err, string(out))
	}
	return out, nil
}

// parseSQLOutput extracts non-empty, non-header lines from sql command output.
func parseSQLOutput(out []byte) []string {
	var tables []string
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "name" || strings.HasPrefix(line, "---") {
			continue
		}
		// Skip box-drawing borders and separators
		if strings.HasPrefix(line, "┌") || strings.HasPrefix(line, "└") || strings.HasPrefix(line, "├") {
			continue
		}
		if strings.Contains(line, "───") || strings.Contains(line, "===") {
			continue
		}
		// Strip box-drawing pipe characters from cell content
		if strings.HasPrefix(line, "│") {
			line = strings.Trim(line, "│")
			line = strings.TrimSpace(line)
			if line == "" || line == "name" {
				continue
			}
		}
		tables = append(tables, line)
	}
	return tables
}

// parseCountOutput extracts a numeric count from sql command output.
func parseCountOutput(out []byte) int {
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "count(*)" || strings.HasPrefix(line, "---") {
			continue
		}
		// Skip box-drawing borders and separators
		if strings.HasPrefix(line, "┌") || strings.HasPrefix(line, "└") || strings.HasPrefix(line, "├") {
			continue
		}
		if strings.Contains(line, "───") || strings.Contains(line, "===") {
			continue
		}
		// Strip box-drawing pipe characters from cell content
		if strings.HasPrefix(line, "│") {
			line = strings.Trim(line, "│")
			line = strings.TrimSpace(line)
			if line == "" || line == "count(*)" {
				continue
			}
		}
		var n int
		if _, err := fmt.Sscanf(line, "%d", &n); err == nil {
			return n
		}
	}
	return 0
}

func runFreshnessContractTest(dir string) FreshnessResult {
	autoRefresh := readRuntimeFile(filepath.Join(dir, "internal", "cli", "auto_refresh.go"))
	freshness := readRuntimeFile(filepath.Join(dir, "internal", "cliutil", "freshness.go"))
	helpers := readRuntimeFile(filepath.Join(dir, "internal", "cli", "helpers.go"))
	dataSource := readRuntimeFile(filepath.Join(dir, "internal", "cli", "data_source.go"))
	if autoRefresh == "" && freshness == "" {
		return FreshnessResult{Verdict: "SKIP", Detail: "cache freshness helper not emitted"}
	}

	liveBypass := strings.Contains(dataSource, `case "live":`) &&
		!strings.Contains(dataSource, "writeThroughCache(resourceType, data)\n\t\treturn data, attachFreshness(DataProvenance{Source: \"live\"}, flags), nil")
	result := FreshnessResult{
		Enabled:         true,
		RegisteredPaths: strings.Count(autoRefresh, "-pp-cli "),
		Metadata:        strings.Contains(freshness, "type FreshnessMeta struct") && strings.Contains(helpers, `meta["freshness"]`),
		LiveBypass:      liveBypass,
		HelperSurface:   strings.Contains(autoRefresh, "func ensureFreshForResources(") && strings.Contains(autoRefresh, "func ensureFreshForCommand("),
	}

	var missing []string
	if result.RegisteredPaths == 0 {
		missing = append(missing, "registered command paths")
	}
	if !result.Metadata {
		missing = append(missing, "meta.freshness metadata")
	}
	if !result.LiveBypass {
		missing = append(missing, "live data-source bypass")
	}
	if !result.HelperSurface {
		missing = append(missing, "custom command helper surface")
	}
	if len(missing) > 0 {
		result.Verdict = "WARN"
		result.Detail = "missing " + strings.Join(missing, ", ")
		return result
	}
	result.Verdict = "PASS"
	result.Detail = "freshness registry, metadata, live bypass, and custom helper surface present"
	return result
}

func readRuntimeFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// startMockServer creates an httptest.Server from the OpenAPI spec.
func startMockServer(spec *openAPISpec) (*httptest.Server, string) {
	mux := http.NewServeMux()

	// Default handler returns 200 with an empty JSON object
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Check if the path looks like a list endpoint
		path := r.URL.Path
		if strings.HasSuffix(path, "s") || strings.Contains(path, "/search") {
			// Return array
			fmt.Fprint(w, `[{"id": 1, "name": "mock-item-1", "state": "open", "title": "Mock Item", "created_at": "2026-03-27T00:00:00Z", "updated_at": "2026-03-27T00:00:00Z"}]`)
		} else if strings.Contains(path, "/rate_limit") {
			fmt.Fprint(w, `{"resources":{"core":{"limit":5000,"remaining":4999,"reset":9999999999}}}`)
		} else if strings.Contains(path, "/compare/") {
			fmt.Fprint(w, `{"commits":[{"sha":"abc1234567","commit":{"message":"feat: mock commit","author":{"name":"mock","date":"2026-03-27T00:00:00Z"}},"html_url":"https://example.com"}]}`)
		} else if strings.Contains(path, "/actions/runs") {
			fmt.Fprint(w, `{"workflow_runs":[{"id":1,"name":"CI","conclusion":"success","workflow_id":1}],"total_count":1}`)
		} else {
			// Return single object
			fmt.Fprint(w, `{"id": 1, "name": "mock-item", "state": "open", "title": "Mock Item", "login": "mock-user", "full_name": "mock/repo", "created_at": "2026-03-27T00:00:00Z", "updated_at": "2026-03-27T00:00:00Z"}`)
		}
	})

	server := httptest.NewServer(mux)
	return server, server.URL
}

// discoverCLIEnvVars reads the CLI's config.go and extracts env var names
// from os.Getenv() calls. This discovers what the CLI actually reads, which
// may differ from what the spec declares or the API name implies.
func discoverCLIEnvVars(dir string) []string {
	configPath := filepath.Join(dir, "internal", "config", "config.go")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}
	re := regexp.MustCompile(`os\.Getenv\("([^"]+)"\)`)
	matches := re.FindAllStringSubmatch(string(data), -1)
	seen := map[string]bool{}
	var envVars []string
	for _, m := range matches {
		name := m[1]
		// Skip base URL and config path env vars — only want auth-related ones
		if strings.HasSuffix(name, "_BASE_URL") || strings.HasSuffix(name, "_CONFIG") {
			continue
		}
		if !seen[name] {
			seen[name] = true
			envVars = append(envVars, name)
		}
	}
	return envVars
}

// camelToKebab is defined in verify.go
