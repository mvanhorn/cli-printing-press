package pipeline

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/artifacts"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	apispec "github.com/mvanhorn/cli-printing-press/v4/internal/spec"
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
	Mode                   string                 `json:"mode"` // "live" or "mock"
	Total                  int                    `json:"total"`
	Passed                 int                    `json:"passed"`
	Failed                 int                    `json:"failed"`
	Critical               int                    `json:"critical"`
	PassRate               float64                `json:"pass_rate"`
	DataPipeline           bool                   `json:"data_pipeline"`
	DataPipelineDetail     string                 `json:"data_pipeline_detail,omitempty"` // PASS, WARN, SKIP, FAIL with context
	Freshness              FreshnessResult        `json:"freshness"`
	BrowserSessionRequired bool                   `json:"browser_session_required,omitempty"`
	BrowserSessionProof    string                 `json:"browser_session_proof,omitempty"`
	BrowserSessionDetail   string                 `json:"browser_session_detail,omitempty"`
	AuthEnvVars            []AuthEnvVarStatus     `json:"auth_env_vars,omitempty"`
	Verdict                string                 `json:"verdict"` // PASS, WARN, FAIL
	Results                []CommandResult        `json:"results"`
	PathParamProbes        []PathParamProbeResult `json:"path_param_probes,omitempty"`
	Binary                 string                 `json:"binary"`
}

type AuthEnvVarStatus struct {
	Name     string               `json:"name"`
	Kind     string               `json:"kind"`
	Required bool                 `json:"required"`
	Status   AuthEnvVarStatusCode `json:"status"`
	Detail   string               `json:"detail,omitempty"`
}

type AuthEnvVarStatusCode string

const (
	AuthEnvVarStatusOK              AuthEnvVarStatusCode = "ok"
	AuthEnvVarStatusMissingRequired AuthEnvVarStatusCode = "missing_required"
	AuthEnvVarStatusMissingInfo     AuthEnvVarStatusCode = "missing_info"
)

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
	// Keep this boundary safe for programmatic callers; CLI commands also
	// normalize earlier when they need the stable path for follow-on argv.
	absDir, err := filepath.Abs(cfg.Dir)
	if err != nil {
		return nil, fmt.Errorf("resolving CLI directory: %w", err)
	}
	cfg.Dir = absDir
	releaseHome, err := scopeSubprocessHome(findCLINames(cfg.Dir)...)
	if err != nil {
		return nil, err
	}
	defer releaseHome()
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
		loaded, err := loadDogfoodOpenAPISpec(cfg.SpecPath, "")
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

	// 5.5. Infer positional args from --help output. Pass the spec's
	// ParamDefaults so positionals named in the spec with a `default:`
	// (e.g., a real recipe slug for food52, a real symbol for a finance
	// API) win over the generic canonicalargs registry and the legacy
	// per-name switch.
	var paramDefaults map[string]string
	if spec != nil {
		paramDefaults = spec.ParamDefaults
	}
	for i := range commands {
		inferPositionalArgs(binaryPath, &commands[i], paramDefaults)
	}

	// 6. Classify and run each command
	for i := range commands {
		classifyCommandKind(&commands[i], spec)
	}

	// Collect auth env var names from the normalized spec model, then augment
	// it with env vars actually read by the generated CLI's config.go.
	authEnvVarSpecs := []apispec.AuthEnvVar{{
		Name:      envVarName,
		Kind:      apispec.AuthEnvVarKindPerCall,
		Required:  true,
		Sensitive: true,
		Inferred:  true,
	}}
	if spec != nil {
		spec.Auth.NormalizeEnvVarSpecs("")
		if len(spec.Auth.EnvVarSpecs) > 0 {
			authEnvVarSpecs = append([]apispec.AuthEnvVar(nil), spec.Auth.EnvVarSpecs...)
		}
	}
	authEnvVarNameSet := make(map[string]struct{}, len(authEnvVarSpecs))
	for _, envVar := range authEnvVarSpecs {
		authEnvVarNameSet[envVar.Name] = struct{}{}
	}
	// Read the CLI's config.go to discover what env vars it actually reads.
	// This catches cases where Claude wired a different env var name than
	// what the spec declares or the API name implies.
	if discovered := discoverCLIEnvVars(cfg.Dir); len(discovered) > 0 {
		for _, ev := range discovered {
			if _, found := authEnvVarNameSet[ev]; found {
				continue
			}
			authEnvVarNameSet[ev] = struct{}{}
			authEnvVarSpecs = append(authEnvVarSpecs, apispec.AuthEnvVar{
				Name:      ev,
				Kind:      apispec.AuthEnvVarKindPerCall,
				Required:  true,
				Sensitive: true,
				Inferred:  true,
			})
		}
	}
	authEnvVars := authEnvVarSpecNames(authEnvVarSpecs)
	requestAuthEnvVars := requestAuthEnvVarNames(authEnvVarSpecs)
	authEnvVarStatuses := summarizeAuthEnvVars(authEnvVarSpecs, cfg.APIKey, cfg.EnvVar, report.Mode)
	if shouldReportAuthEnvVarStatuses(authEnvVarStatuses) {
		report.AuthEnvVars = authEnvVarStatuses
	}
	for _, missing := range missingRequiredAuthEnvVars(authEnvVarStatuses) {
		report.Results = append(report.Results, CommandResult{
			Command: "auth-env:" + missing.Name,
			Kind:    "auth",
			Error:   missing.Detail,
		})
	}

	// EndpointTemplateVars env names live in their own bucket so the
	// --api-key overwrite path doesn't rewrite SHOPIFY_SHOP into the
	// API key, and so mock mode can inject placeholder values that
	// satisfy buildURL without leaking into authEnvVars. Live mode
	// inherits whatever the operator already exported; we don't mirror
	// the env into the subprocess again because os.Environ() above
	// already carries it.
	templateVarEnvs := discoverCLITemplateVarEnvs(cfg.Dir)

	// buildEnv constructs the environment for test subprocesses, passing
	// all auth-related env vars so auth-requiring commands can complete.
	buildEnv := func() []string {
		env := subprocessEnv()
		if report.Mode == "live" {
			for _, ev := range authEnvVars {
				if val := os.Getenv(ev); val != "" {
					env = append(env, ev+"="+val)
				}
			}
			// Also pass the explicit --api-key under request credential env var
			// names so the generated CLI finds it without rewriting setup-only
			// OAuth inputs such as client IDs and client secrets.
			if cfg.APIKey != "" {
				for _, ev := range requestAuthEnvVars {
					env = append(env, ev+"="+cfg.APIKey)
				}
			}
		} else {
			env = append(env, baseURLEnvVar+"="+baseURLOverride)
			for _, ev := range requestAuthEnvVars {
				env = append(env, ev+"=mock-token-for-testing")
			}
			// Templated URLs (e.g. /admin/api/{api_version}/graphql.json)
			// need every {var} resolved before buildURL succeeds. Without
			// injecting safe values here mock-mode requests never leave the
			// generated CLI — TemplateVarError fires first. The string
			// "mock" is opaque to the test server, which echoes whatever
			// path it receives.
			for _, ev := range templateVarEnvs {
				env = append(env, ev+"=mock")
			}
			// Defense-in-depth: every mock-mode subprocess inherits this
			// env var. Generated commands that perform visible side
			// effects (open browser tabs, send notifications) MUST check
			// cliutil.IsVerifyEnv() and short-circuit when set, so even
			// if the side-effect classifier misses a command, the
			// command itself doesn't spam the user's environment during
			// verify. Documented in skills/printing-press/SKILL.md and
			// AGENTS.md.
			env = append(env, "PRINTING_PRESS_VERIFY=1")
			// Verify owns its httptest mock-server and needs the real
			// wire path to assert against, so it opts back in to the
			// transport-layer mutating-verb gate that every other
			// consumer leaves engaged. Without this var, the gate in
			// generated Client.do() returns a synthetic envelope for
			// DELETE/POST/PUT/PATCH and the mock server never sees the
			// request — collapsing verify's pass-rate signal to zero
			// for those verbs.
			env = append(env, "PRINTING_PRESS_VERIFY_LIVE_HTTP=1")
		}
		return env
	}

	// 7. Run tests
	for i, cmd := range commands {
		env := buildEnv()
		// Mock-mode side-effect detection: if the command opens a
		// browser tab or otherwise performs a visible action that the
		// PRINTING_PRESS_VERIFY env var alone may not gate, skip its
		// Execute test to avoid spamming the user's environment during
		// verify. The DryRun and Help tests still run — those are read-
		// only by definition.
		if report.Mode == "mock" && isSideEffectCommand(binaryPath, &commands[i], cfg.Dir) {
			result := runSideEffectSafeCommandTests(binaryPath, commands[i], env)
			report.Results = append(report.Results, result)
			continue
		}
		result := runCommandTests(binaryPath, cmd, report.Mode, env)
		commands[i] = cmd // preserve classification
		report.Results = append(report.Results, result)
	}

	expectedMockRows := 0
	if report.Mode == "mock" && spec != nil && len(spec.NestedDataEnvelopes) > 0 {
		expectedMockRows = 2
	}
	if isDeviceCLIDir(cfg.Dir) {
		// Device CLIs (BLE) have no sync->sql->search data pipeline; running the
		// HTTP-shaped pipeline test invokes a non-existent `sync` command and
		// reports a false "sync crashed". Mark the dimension satisfied instead.
		report.DataPipeline = true
		report.DataPipelineDetail = "SKIP (device CLI: no sync data pipeline)"
	} else {
		report.DataPipeline, report.DataPipelineDetail = runDataPipelineTest(binaryPath, cfg.Dir, report.Mode, buildEnv, expectedMockRows)
	}
	report.Freshness = runFreshnessContractTest(cfg.Dir)
	report.PathParamProbes = runPathParamProbes(binaryPath, buildEnv(), paramDefaults)

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

	finalizeVerifyReport(report, cfg.Threshold, true)

	return report, nil
}

func authEnvVarSpecNames(envVarSpecs []apispec.AuthEnvVar) []string {
	return authEnvVarNamesMatching(envVarSpecs, func(apispec.AuthEnvVar) bool {
		return true
	})
}

func requestAuthEnvVarNames(envVarSpecs []apispec.AuthEnvVar) []string {
	return authEnvVarNamesMatching(envVarSpecs, func(envVar apispec.AuthEnvVar) bool {
		return envVar.IsRequestCredential()
	})
}

func authEnvVarNamesMatching(envVarSpecs []apispec.AuthEnvVar, matches func(apispec.AuthEnvVar) bool) []string {
	names := make([]string, 0, len(envVarSpecs))
	seen := make(map[string]struct{}, len(envVarSpecs))
	for _, envVar := range envVarSpecs {
		if !matches(envVar) {
			continue
		}
		name := strings.TrimSpace(envVar.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}

func summarizeAuthEnvVars(envVarSpecs []apispec.AuthEnvVar, apiKey, apiKeyEnvVar, mode string) []AuthEnvVarStatus {
	statuses := make([]AuthEnvVarStatus, 0, len(envVarSpecs))
	if apiKey != "" && apiKeyEnvVar == "" {
		for _, envVar := range envVarSpecs {
			name := strings.TrimSpace(envVar.Name)
			if name == "" || !envVar.IsRequestCredential() {
				continue
			}
			if apiKeyEnvVar == "" {
				apiKeyEnvVar = name
			}
			if envVar.Required {
				apiKeyEnvVar = name
				break
			}
		}
	}
	for _, envVar := range envVarSpecs {
		name := strings.TrimSpace(envVar.Name)
		if name == "" {
			continue
		}
		kind := envVar.EffectiveKind()
		status := AuthEnvVarStatus{
			Name:     name,
			Kind:     string(kind),
			Required: envVar.Required,
			Status:   AuthEnvVarStatusOK,
		}
		if os.Getenv(name) != "" || (apiKey != "" && name == apiKeyEnvVar) || mode == "mock" {
			statuses = append(statuses, status)
			continue
		}
		if envVar.IsRequestCredential() && envVar.Required {
			status.Status = AuthEnvVarStatusMissingRequired
			status.Detail = "required per-call auth env var is not set"
		} else {
			status.Status = AuthEnvVarStatusMissingInfo
			status.Detail = "auth env var is not set but does not block verification"
		}
		statuses = append(statuses, status)
	}
	return statuses
}

func shouldReportAuthEnvVarStatuses(statuses []AuthEnvVarStatus) bool {
	for _, status := range statuses {
		if status.Status != AuthEnvVarStatusOK {
			return true
		}
	}
	return false
}

func missingRequiredAuthEnvVars(statuses []AuthEnvVarStatus) []AuthEnvVarStatus {
	missing := make([]AuthEnvVarStatus, 0, len(statuses))
	for _, status := range statuses {
		if status.Status == AuthEnvVarStatusMissingRequired {
			missing = append(missing, status)
		}
	}
	return missing
}

// runSideEffectSafeCommandTests is the mock-mode counterpart to
// runCommandTests for commands the side-effect classifier flagged. It
// runs --help (read-only) and --dry-run (the command should honor it),
// then SKIPs the Execute test rather than risk launching a browser tab,
// sending a notification, etc.
//
// PRINTING_PRESS_VERIFY=1 is already set in env, so well-behaved
// generated commands short-circuit anyway. This wrapper is the
// belt-and-suspenders complement to that env-var convention.
func runSideEffectSafeCommandTests(binary string, cmd discoveredCommand, env []string) CommandResult {
	result := CommandResult{
		Command: cmd.Name,
		Kind:    cmd.Kind,
	}

	result.Help = runCLI(binary, []string{cmd.Name, "--help"}, env, 10*time.Second) == nil

	positionals, flags := sideEffectSafeInvocationInputs(cmd)

	dryArgs := append([]string{cmd.Name}, positionals...)
	dryArgs = append(dryArgs, flags...)
	dryArgs = append(dryArgs, "--dry-run")
	if err := runCLI(binary, dryArgs, env, 10*time.Second); err == nil || isIntentionalStubExit(err) {
		result.DryRun = true
	}

	// SKIP the Execute test: this command was classified as side-effecting
	// (opens a browser, dials out, etc.) and we don't want to trigger
	// that during verify even with PRINTING_PRESS_VERIFY=1 set, because
	// older generated commands may not honor the convention. Score this
	// as a pass on Execute since "we deliberately did not exercise it"
	// is not a failure of the CLI under test.
	result.Execute = true

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

// runCommandTests executes the test suite for a single command.
func runCommandTests(binary string, cmd discoveredCommand, mode string, env []string) CommandResult {
	result := CommandResult{
		Command: cmd.Name,
		Kind:    cmd.Kind,
	}

	// Test 1: --help
	helpOutput, helpErr := runCLIWithOutput(binary, []string{cmd.Name, "--help"}, env, 10*time.Second)
	result.Help = helpErr == nil
	typedCodes := typedSuccessCodes(cmd, string(helpOutput))

	// Get any required flags/args for this command.
	// First, probe the binary for cobra-declared required flags (generic, spec-agnostic).
	// Then fall back to the positional-arg map for commands that take bare positionals.
	positionals, extraFlags := commandInvocationInputs(binary, cmd)

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
		args := buildTestArgs(cmd.Name, positionals, extraFlags, "--dry-run")
		err := runCLI(binary, args, env, 10*time.Second)
		result.DryRun = err == nil || isIntentionalStubExit(err) || isDocumentedSuccessExit(err, typedCodes)
	} else {
		result.DryRun = true // skip = pass
	}

	// Test 3: Execute (only for read commands in live mode, all in mock mode)
	if cmd.Kind == "local" || cmd.Kind == "data-layer" {
		result.Execute = true // tested separately in data pipeline
	} else if mode == "live" && cmd.Kind == "write" {
		result.Execute = true // skip writes on live = pass (tested via dry-run)
	} else {
		args := buildTestArgs(cmd.Name, positionals, extraFlags, "--json")
		err := runCLI(binary, args, env, 15*time.Second)
		result.Execute = err == nil || isIntentionalStubExit(err) || isDocumentedSuccessExit(err, typedCodes)
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

	// cliutil.IsVerifyEnv() drives doctor's synthetic browser-session
	// proof short-circuit. Without PRINTING_PRESS_VERIFY=1 the probe
	// asks the CLI to validate against a real session, which a clean
	// shipcheck environment doesn't have — every cookie-auth CLI then
	// scores 0 even when the synthetic proof would have passed. Match
	// the env-augmentation buildEnv() uses for the other mock-mode
	// probes so this probe is self-contained.
	env := append(subprocessEnv(), "PRINTING_PRESS_VERIFY=1")
	output, err := runCLIWithOutput(binary, []string{"doctor", "--json"}, env, 20*time.Second)
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
func runDataPipelineTest(binary, cliDir, mode string, envFn func() []string, expectedRows int) (bool, string) {
	if strings.TrimSpace(cliDir) != "" {
		if manifest, err := ReadCLIManifest(cliDir); err == nil && manifest.IsLocalDatastore() {
			return true, "SKIP (local-datastore CLI: no network sync to verify)"
		}
		if !cliHasSyncCommand(cliDir) {
			return true, "SKIP (CLI has no sync command)"
		}
		if !cliHasLocalStore(cliDir) {
			return true, "SKIP (CLI has no local store)"
		}
	}

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
	var syncErrors []error
	syncErr := runCLI(binary, []string{"sync", "--db", dbPath, "--resources", "repos", "--full"}, env, 30*time.Second)
	if syncErr != nil {
		syncErrors = append(syncErrors, syncErr)
		syncErr = runCLI(binary, []string{"sync", "--db", dbPath, "--full"}, env, 30*time.Second)
	}
	if syncErr != nil {
		syncErrors = append(syncErrors, syncErr)
		// Sync might not accept --resources or --full; keep --db when
		// possible so downstream sql probes read the same temporary store.
		syncErr = runCLI(binary, []string{"sync", "--db", dbPath}, env, 30*time.Second)
	}
	if syncErr != nil {
		syncErrors = append(syncErrors, syncErr)
		// Sync might not accept --db either; try the bare command before
		// deciding the pipeline crashed.
		syncErr = runCLI(binary, []string{"sync", "--full"}, env, 30*time.Second)
	}
	if syncErr != nil {
		syncErrors = append(syncErrors, syncErr)
		syncErr = runCLI(binary, []string{"sync"}, env, 30*time.Second)
	}
	if syncErr != nil {
		syncErrors = append(syncErrors, syncErr)
		if allSyncAttemptsWereUnknownCommand(syncErrors) {
			return true, "WARN: no sync command — data-pipeline check skipped"
		}
		if flag, ok := firstUnknownSyncFlag(syncErrors); ok {
			return false, fmt.Sprintf("FAIL: sync rejected flag %s", flag)
		}
		return false, "FAIL: sync crashed"
	}

	// Test health (if available)
	_ = runCLI(binary, []string{"health", "--db", dbPath}, env, 10*time.Second)

	tableQuery := `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite%' AND name NOT LIKE '%_fts%' AND name != 'sync_state'`
	tablesOut, sqlErr := runCLIWithOutput(binary, []string{"sql", "--db", dbPath, tableQuery}, env, 10*time.Second)
	if sqlErr != nil {
		return true, "PASS: sync completed (sql unavailable, table validation skipped)"
	}

	// Parse table names from output (one per line, skip empty lines and header noise)
	tables := parseSQLOutput(tablesOut)
	if len(tables) == 0 {
		// No domain tables found — ambiguous (could be minimal CLI or unusual naming).
		// Don't fail the pipeline gate; report for human review.
		return true, "WARN: sync completed but no domain tables found in sqlite_master"
	}
	if mode == "mock" && strings.TrimSpace(cliDir) != "" && !cliHasSyncableResources(cliDir) {
		return true, fmt.Sprintf("PASS: %d domain tables created (mock mode; no syncable resources declared)", len(tables))
	}

	var bestShortTable string
	var bestShortCount int
	var bestPassTable string
	var bestPassCount int
	var zeroDataTable string
	for _, table := range tables {
		countQuery := fmt.Sprintf("SELECT count(*) FROM \"%s\"", table)
		countOut, countErr := runCLIWithOutput(binary, []string{"sql", "--db", dbPath, countQuery}, env, 10*time.Second)
		if countErr != nil {
			continue
		}
		count := parseCountOutput(countOut)
		if count > 0 {
			if expectedRows > 0 {
				if count >= expectedRows {
					if !isAuxiliaryPipelineTable(table, len(tables)) && count > bestPassCount {
						bestPassTable = table
						bestPassCount = count
					}
					continue
				}
				if count > bestShortCount {
					bestShortTable = table
					bestShortCount = count
				}
				continue
			}
			return true, fmt.Sprintf("PASS: %d domain tables, %s has %d rows", len(tables), table, count)
		}
		if expectedRows > 0 && zeroDataTable == "" && !isAuxiliaryPipelineTable(table, len(tables)) {
			zeroDataTable = table
		}
	}
	if bestPassTable != "" {
		return true, fmt.Sprintf("PASS: %d domain tables, %s has %d rows", len(tables), bestPassTable, bestPassCount)
	}
	if bestShortTable != "" {
		return false, fmt.Sprintf("FAIL: %s has %d rows after sync, expected at least %d (%s mode)", bestShortTable, bestShortCount, expectedRows, mode)
	}
	if zeroDataTable != "" && len(tables) > 1 {
		return false, fmt.Sprintf("FAIL: %s has 0 rows after sync, expected at least %d (%s mode)", zeroDataTable, expectedRows, mode)
	}
	return false, fmt.Sprintf("FAIL: %d domain tables created but 0 rows after sync (%s mode)", len(tables), mode)
}

func allSyncAttemptsWereUnknownCommand(errs []error) bool {
	if len(errs) == 0 {
		return false
	}
	for _, err := range errs {
		if err == nil || !isUnknownSyncCommandError(err) {
			return false
		}
	}
	return true
}

func isUnknownSyncCommandError(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unknown command \"sync\"")
}

func firstUnknownSyncFlag(errs []error) (string, bool) {
	for _, err := range errs {
		if flag, ok := unknownSyncFlag(err); ok {
			return flag, true
		}
	}
	return "", false
}

func unknownSyncFlag(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	text := strings.ToLower(err.Error())
	for _, marker := range []string{"unknown flag: ", "unknown shorthand flag: "} {
		if _, after, ok := strings.Cut(text, marker); ok {
			flag := strings.Fields(after)
			if len(flag) > 0 {
				return flag[0], true
			}
			return "", false
		}
	}
	return "", false
}

func cliHasSyncCommand(cliDir string) bool {
	return hasRegisteredCommandFileWithPrefix(filepath.Join(cliDir, "internal", "cli"), "sync")
}

func cliHasLocalStore(cliDir string) bool {
	return fileExists(filepath.Join(cliDir, "internal", "store", "store.go"))
}

func cliHasSyncableResources(cliDir string) bool {
	content := readAllGoFiles(filepath.Join(cliDir, "internal", "cli"))
	content += readAllGoFiles(filepath.Join(cliDir, "internal", "store"))
	return hasNonEmptySyncResources(content)
}

func isAuxiliaryPipelineTable(table string, totalTables int) bool {
	if totalTables <= 1 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(table)) {
	case "config", "configs", "metadata", "schema_migrations", "settings":
		return true
	default:
		return false
	}
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
		if fixture, ok := nestedDataEnvelopeForPath(spec, path); ok {
			fmt.Fprint(w, renderNestedDataEnvelopeFixture(fixture))
		} else if strings.HasSuffix(path, "s") || strings.Contains(path, "/search") {
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

func nestedDataEnvelopeForPath(spec *openAPISpec, path string) (nestedDataEnvelopeFixture, bool) {
	if spec == nil || len(spec.NestedDataEnvelopes) == 0 {
		return nestedDataEnvelopeFixture{}, false
	}
	if fixture, ok := spec.NestedDataEnvelopes[path]; ok {
		return fixture, true
	}
	for specPath, fixture := range spec.NestedDataEnvelopes {
		if pathMatchesSpec(path, compileSpecPathPatterns([]string{specPath})) {
			return fixture, true
		}
	}
	return nestedDataEnvelopeFixture{}, false
}

func renderNestedDataEnvelopeFixture(fixture nestedDataEnvelopeFixture) string {
	arrayKey := fixture.ArrayKey
	if arrayKey == "" {
		arrayKey = "items"
	}
	body := map[string]any{
		"success": true,
		"data": map[string]any{
			arrayKey: []map[string]any{
				{"id": 1, "name": "mock-item-1", "state": "open", "title": "Mock Item", "created_at": "2026-03-27T00:00:00Z", "updated_at": "2026-03-27T00:00:00Z"},
				{"id": 2, "name": "mock-item-2", "state": "open", "title": "Mock Item 2", "created_at": "2026-03-27T00:00:00Z", "updated_at": "2026-03-27T00:00:00Z"},
			},
			"pagination": map[string]any{"total": 2},
		},
	}
	data, err := json.Marshal(body)
	if err != nil {
		return `{"success":true,"data":{"items":[{"id":1,"name":"mock-item-1"},{"id":2,"name":"mock-item-2"}],"pagination":{"total":2}}}`
	}
	return string(data)
}

// templateVarReadRe matches the shape config.go.tmpl emits for each
// EndpointTemplateVars entry: `os.Getenv("X")` immediately followed by a
// `cfg.TemplateVars["..."] = v` assignment. Auth-bearing env reads land in
// named cfg fields; template-var reads land in this map. Used by both
// discoverCLIEnvVars (to exclude template names from the auth set) and
// discoverCLITemplateVarEnvs (to recover them for mock-mode injection).
var templateVarReadRe = regexp.MustCompile(`(?s)os\.Getenv\("([^"]+)"\)[^{]*\{\s*cfg\.TemplateVars\[`)

// discoverCLIEnvVars reads the CLI's config.go and extracts env var names
// from os.Getenv() calls. This discovers what the CLI actually reads, which
// may differ from what the spec declares or the API name implies.
func discoverCLIEnvVars(dir string) []string {
	configPath := filepath.Join(dir, "internal", "config", "config.go")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}
	body := string(data)

	// Endpoint template vars (Shopify's SHOPIFY_SHOP / SHOPIFY_API_VERSION
	// shape) feed Config.TemplateVars, NOT the auth header. The verifier's
	// --api-key path overwrites every discovered auth env var with the API
	// key, so leaking a template-var name into authEnvVars rewrites the
	// resolved hostname to the API key string and routes the live request
	// at the wrong URL. Match the template-var shape and exclude those
	// names from the discovered set; mock-mode injection lives separately
	// in discoverCLITemplateVarEnvs.
	templateVarNames := map[string]bool{}
	for _, m := range templateVarReadRe.FindAllStringSubmatch(body, -1) {
		templateVarNames[m[1]] = true
	}

	re := regexp.MustCompile(`os\.Getenv\("([^"]+)"\)`)
	matches := re.FindAllStringSubmatch(body, -1)
	seen := map[string]bool{}
	var envVars []string
	for _, m := range matches {
		name := m[1]
		// Skip base URL and config path env vars — only want auth-related ones
		if strings.HasSuffix(name, "_BASE_URL") || strings.HasSuffix(name, "_CONFIG") {
			continue
		}
		if templateVarNames[name] {
			continue
		}
		if !seen[name] {
			seen[name] = true
			envVars = append(envVars, name)
		}
	}
	return envVars
}

// discoverCLITemplateVarEnvs returns the env var names that feed
// Config.TemplateVars (the {placeholder} markers in BaseURL or the request
// path). Mock-mode verification needs these so buildURL doesn't fail on
// unresolved {var} markers — without injection, a templated GraphQL path
// like /admin/api/{api_version}/graphql.json returns TemplateVarError
// before the mock server ever sees a request.
func discoverCLITemplateVarEnvs(dir string) []string {
	configPath := filepath.Join(dir, "internal", "config", "config.go")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	for _, m := range templateVarReadRe.FindAllStringSubmatch(string(data), -1) {
		name := m[1]
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// camelToKebab is defined in verify.go
