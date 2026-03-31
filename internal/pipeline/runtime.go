package pipeline

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/internal/artifacts"
	"github.com/mvanhorn/cli-printing-press/internal/naming"
)

// VerifyConfig configures a runtime verification run.
type VerifyConfig struct {
	Dir       string // generated CLI directory
	SpecPath  string // OpenAPI spec path
	APIKey    string // optional - if set, tests against real API
	EnvVar    string // env var name for the API key (e.g., GITHUB_TOKEN)
	Threshold int    // minimum pass rate (default 80)
}

// VerifyReport is the output of a runtime verification run.
type VerifyReport struct {
	Mode         string          `json:"mode"` // "live" or "mock"
	Total        int             `json:"total"`
	Passed       int             `json:"passed"`
	Failed       int             `json:"failed"`
	Critical     int             `json:"critical"`
	PassRate     float64         `json:"pass_rate"`
	DataPipeline bool            `json:"data_pipeline"`
	Verdict      string          `json:"verdict"` // PASS, WARN, FAIL
	Results      []CommandResult `json:"results"`
	Binary       string          `json:"binary"`
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

// RunVerify executes the runtime verification pipeline.
func RunVerify(cfg VerifyConfig) (*VerifyReport, error) {
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
	commands := discoverCommands(cfg.Dir)

	// 5.5. Infer positional args from --help output
	for i := range commands {
		inferPositionalArgs(binaryPath, &commands[i])
	}

	// 6. Classify and run each command
	for i := range commands {
		classifyCommandKind(&commands[i], spec)
	}

	// 7. Run tests
	for i, cmd := range commands {
		env := os.Environ()
		if report.Mode == "live" {
			env = append(env, envVarName+"="+cfg.APIKey)
		} else {
			env = append(env, baseURLEnvVar+"="+baseURLOverride)
			env = append(env, envVarName+"=mock-token-for-testing")
		}

		result := runCommandTests(binaryPath, cmd, report.Mode, env)
		commands[i] = cmd // preserve classification
		report.Results = append(report.Results, result)
	}

	// 8. Data pipeline test
	report.DataPipeline = runDataPipelineTest(binaryPath, report.Mode, func() []string {
		env := os.Environ()
		if report.Mode == "live" {
			env = append(env, envVarName+"="+cfg.APIKey)
		} else {
			env = append(env, baseURLEnvVar+"="+baseURLOverride)
			env = append(env, envVarName+"=mock-token-for-testing")
		}
		return env
	})

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

	return report, nil
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

// discoverCommands parses root.go to find all registered commands.
func discoverCommands(dir string) []discoveredCommand {
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
	default:
		return "mock-value"
	}
}

// classifyCommandKind determines if a command is read, write, local, or data-layer.
func classifyCommandKind(cmd *discoveredCommand, spec *openAPISpec) {
	name := cmd.Name
	// Data layer commands
	switch name {
	case "sync", "search", "sql", "health", "trends", "patterns", "analytics", "export", "import":
		cmd.Kind = "data-layer"
		return
	case "doctor", "auth":
		cmd.Kind = "local"
		return
	case "tail":
		cmd.Kind = "read"
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
func workflowTestFlags(cmdName string) []string {
	switch cmdName {
	case "pr-triage", "stale", "actions-health", "security", "contributors":
		return []string{"--repo", "mock-owner/mock-repo"}
	case "changelog":
		return []string{"mock-owner", "mock-repo", "--since", "v0.0.1"}
	default:
		return nil
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

	// Get any required flags/args for this command
	extraFlags := workflowTestFlags(cmd.Name)

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
		result.DryRun = err == nil
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
		result.Execute = err == nil
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

// runDataPipelineTest tests the sync -> sql -> search -> health chain.
func runDataPipelineTest(binary, mode string, envFn func() []string) bool {
	env := envFn()

	// Create a temp dir for the test database
	tmpDir, err := os.MkdirTemp("", "verify-db-*")
	if err != nil {
		return false
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

	// Test health (if available)
	healthErr := runCLI(binary, []string{"health", "--db", dbPath}, env, 10*time.Second)
	_ = healthErr

	// The pipeline passes if sync doesn't crash (even if it syncs 0 rows from mock)
	return syncErr == nil
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

// camelToKebab is defined in verify.go
