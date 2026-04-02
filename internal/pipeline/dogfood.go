package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/internal/naming"
	openapiparser "github.com/mvanhorn/cli-printing-press/internal/openapi"
	apispec "github.com/mvanhorn/cli-printing-press/internal/spec"
)

type DogfoodReport struct {
	Dir           string             `json:"dir"`
	SpecPath      string             `json:"spec_path,omitempty"`
	Verdict       string             `json:"verdict"`
	PathCheck     PathCheckResult    `json:"path_check"`
	AuthCheck     AuthCheckResult    `json:"auth_check"`
	DeadFlags     DeadCodeResult     `json:"dead_flags"`
	DeadFuncs     DeadCodeResult     `json:"dead_functions"`
	PipelineCheck PipelineResult     `json:"pipeline_check"`
	ExampleCheck  ExampleCheckResult `json:"example_check"`
	Issues        []string           `json:"issues"`
}

type PathCheckResult struct {
	Tested  int      `json:"tested"`
	Valid   int      `json:"valid"`
	Invalid []string `json:"invalid,omitempty"`
	Pct     int      `json:"valid_pct"`
}

type AuthCheckResult struct {
	SpecScheme   string `json:"spec_scheme"`
	GeneratedFmt string `json:"generated_format"`
	Match        bool   `json:"match"`
	Detail       string `json:"detail"`
}

type DeadCodeResult struct {
	Total int      `json:"total"`
	Dead  int      `json:"dead"`
	Items []string `json:"items,omitempty"`
}

type PipelineResult struct {
	SyncCallsDomain   bool   `json:"sync_calls_domain"`
	SearchCallsDomain bool   `json:"search_calls_domain"`
	DomainTables      int    `json:"domain_tables"`
	Detail            string `json:"detail"`
}

type ExampleCheckResult struct {
	Tested        int      `json:"tested"`
	WithExamples  int      `json:"with_examples"`
	ValidExamples int      `json:"valid_examples"`
	InvalidFlags  []string `json:"invalid_flags,omitempty"`
	Missing       []string `json:"missing,omitempty"`
	Skipped       bool     `json:"skipped,omitempty"`
	Detail        string   `json:"detail"`
}

type openAPISpec struct {
	Paths []string
	Auth  apispec.AuthConfig
}

func RunDogfood(dir, specPath string) (*DogfoodReport, error) {
	report := &DogfoodReport{
		Dir:      dir,
		SpecPath: specPath,
		Verdict:  "PASS",
	}

	var spec *openAPISpec
	if specPath != "" {
		loaded, err := loadDogfoodOpenAPISpec(specPath)
		if err != nil {
			return nil, err
		}
		spec = loaded

		report.PathCheck = checkPaths(dir, spec.Paths)
		report.AuthCheck = checkAuth(dir, spec.Auth)
	} else {
		report.AuthCheck = AuthCheckResult{
			Match:  true,
			Detail: "spec not provided; auth protocol check skipped",
		}
	}

	report.DeadFlags = checkDeadFlags(dir)
	report.DeadFuncs = checkDeadFunctions(dir)
	report.PipelineCheck = checkPipelineIntegrity(dir)
	report.ExampleCheck = checkExamples(dir)
	report.Issues = collectDogfoodIssues(report, spec != nil)
	report.Verdict = deriveDogfoodVerdict(report, spec != nil)

	if err := writeDogfoodResults(report, dir); err != nil {
		return nil, err
	}

	return report, nil
}

func LoadDogfoodResults(dir string) (*DogfoodReport, error) {
	data, err := os.ReadFile(filepath.Join(dir, "dogfood-results.json"))
	if err != nil {
		return nil, err
	}

	var report DogfoodReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, err
	}
	return &report, nil
}

func writeDogfoodResults(report *DogfoodReport, dir string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "dogfood-results.json"), data, 0o644)
}

func loadDogfoodOpenAPISpec(specPath string) (*openAPISpec, error) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("reading spec: %w", err)
	}

	parsed, parseErr := openapiparser.ParseLenient(data)
	if parseErr == nil {
		return &openAPISpec{
			Paths: collectDogfoodSpecPaths(parsed.Resources),
			Auth:  parsed.Auth,
		}, nil
	}

	summary, err := loadOpenAPISpec(specPath)
	if err != nil {
		return nil, parseErr
	}

	return &openAPISpec{
		Paths: summary.Paths,
		Auth:  deriveDogfoodAuth(summary),
	}, nil
}

func collectDogfoodSpecPaths(resources map[string]apispec.Resource) []string {
	var paths []string
	for _, resource := range resources {
		collectDogfoodResourcePaths(resource, &paths)
	}
	return uniqueSorted(paths)
}

func collectDogfoodResourcePaths(resource apispec.Resource, paths *[]string) {
	for _, endpoint := range resource.Endpoints {
		if strings.TrimSpace(endpoint.Path) != "" {
			*paths = append(*paths, endpoint.Path)
		}
	}
	for _, subresource := range resource.SubResources {
		collectDogfoodResourcePaths(subresource, paths)
	}
}

func deriveDogfoodAuth(spec *openAPISpecInfo) apispec.AuthConfig {
	if spec == nil {
		return apispec.AuthConfig{Type: "none"}
	}

	candidateKeys := referencedDogfoodSecurityKeys(spec.SecurityRequirements)
	if len(candidateKeys) == 0 {
		for key := range spec.SecuritySchemes {
			candidateKeys = append(candidateKeys, key)
		}
		sort.Strings(candidateKeys)
	}

	for _, key := range candidateKeys {
		scheme, ok := spec.SecuritySchemes[key]
		if !ok {
			continue
		}
		if auth, ok := dogfoodAuthConfigForScheme(scheme); ok {
			return auth
		}
	}

	return apispec.AuthConfig{Type: "none"}
}

func referencedDogfoodSecurityKeys(requirements []securityRequirementSet) []string {
	seen := make(map[string]struct{})
	var keys []string
	for _, requirementSet := range requirements {
		for _, alternative := range requirementSet.Alternatives {
			for _, key := range alternative {
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				keys = append(keys, key)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

func dogfoodAuthConfigForScheme(scheme openAPISecurityScheme) (apispec.AuthConfig, bool) {
	nameLower := strings.ToLower(scheme.Key)
	auth := apispec.AuthConfig{
		Type:   "none",
		Scheme: scheme.Key,
	}

	switch {
	case strings.Contains(nameLower, "bot"):
		auth.Type = "api_key"
		auth.Header = "Authorization"
		auth.Format = "Bot {bot_token}"
		return auth, true
	case scheme.Type == "http" && scheme.Scheme == "bearer":
		auth.Type = "bearer_token"
		auth.Header = "Authorization"
		return auth, true
	case scheme.Type == "http" && scheme.Scheme == "basic":
		auth.Type = "api_key"
		auth.Header = "Authorization"
		auth.Format = "Basic {username}:{password}"
		return auth, true
	case scheme.Type == "apikey":
		auth.Type = "api_key"
		auth.In = scheme.In
		auth.Header = strings.TrimSpace(scheme.HeaderName)
		if auth.Header == "" {
			auth.Header = "Authorization"
		}
		if strings.EqualFold(auth.Header, "Authorization") && strings.Contains(nameLower, "bot") {
			auth.Format = "Bot {bot_token}"
		}
		return auth, true
	case scheme.Type == "oauth2" || scheme.Type == "openidconnect":
		auth.Type = "bearer_token"
		auth.Header = "Authorization"
		return auth, true
	default:
		return apispec.AuthConfig{}, false
	}
}

func checkPaths(dir string, paths []string) PathCheckResult {
	result := PathCheckResult{}
	if len(paths) == 0 {
		return result
	}

	cliDir := filepath.Join(dir, "internal", "cli")
	files := listGoFiles(cliDir)
	var commandFiles []string
	for _, file := range files {
		base := filepath.Base(file)
		switch base {
		case "root.go", "helpers.go", "doctor.go", "auth.go", "dogfood.go", "scorecard.go", "vision.go":
			continue
		default:
			commandFiles = append(commandFiles, file)
		}
	}
	sort.Strings(commandFiles)
	if len(commandFiles) > 10 {
		commandFiles = commandFiles[:10]
	}

	specPatterns := compileSpecPathPatterns(paths)
	pathAssignmentRe := regexp.MustCompile(`(?m)\bpath\s*(?::=|=)\s*"([^"]+)"`)

	for _, file := range commandFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		matches := pathAssignmentRe.FindAllStringSubmatch(string(content), -1)
		for _, match := range matches {
			path := match[1]
			result.Tested++
			if pathMatchesSpec(path, specPatterns) {
				result.Valid++
				continue
			}
			result.Invalid = append(result.Invalid, path)
		}
	}

	if result.Tested > 0 {
		result.Pct = (result.Valid * 100) / result.Tested
	}
	result.Invalid = uniqueSorted(result.Invalid)
	return result
}

func checkAuth(dir string, auth apispec.AuthConfig) AuthCheckResult {
	result := AuthCheckResult{
		Match:  true,
		Detail: "no recognized auth scheme in spec",
	}

	expectedPrefix := ""
	switch {
	case strings.Contains(strings.ToLower(auth.Format), "bot "):
		result.SpecScheme = `bot token format (expects "Bot " prefix)`
		expectedPrefix = "Bot "
	case strings.EqualFold(auth.Type, "bearer_token"):
		result.SpecScheme = `bearer token format (expects "Bearer " prefix)`
		expectedPrefix = "Bearer "
	case strings.Contains(strings.ToLower(auth.Format), "basic "):
		result.SpecScheme = `basic auth format (expects "Basic " prefix)`
		expectedPrefix = "Basic "
	}

	clientData, err := os.ReadFile(filepath.Join(dir, "internal", "client", "client.go"))
	if err != nil {
		result.Match = false
		result.Detail = fmt.Sprintf("failed to read client.go: %v", err)
		return result
	}

	clientSource := string(clientData)
	switch {
	case strings.Contains(clientSource, `"Bot "`):
		result.GeneratedFmt = "Bot "
	case strings.Contains(clientSource, `"Bearer "`):
		result.GeneratedFmt = "Bearer "
	default:
		result.GeneratedFmt = "unknown"
	}

	if expectedPrefix == "" {
		result.Detail = "spec not provided or no bot/bearer scheme detected"
		return result
	}

	result.Match = result.GeneratedFmt == expectedPrefix
	if result.Match {
		result.Detail = fmt.Sprintf(`spec and generated client both use %q`, strings.TrimSpace(expectedPrefix))
	} else {
		result.Detail = fmt.Sprintf(`spec expects %q but generated client uses %q`, strings.TrimSpace(expectedPrefix), strings.TrimSpace(result.GeneratedFmt))
	}
	return result
}

func checkDeadFlags(dir string) DeadCodeResult {
	rootData, err := os.ReadFile(filepath.Join(dir, "internal", "cli", "root.go"))
	if err != nil {
		return DeadCodeResult{}
	}

	fieldRe := regexp.MustCompile(`&flags\.(\w+)`)
	matches := fieldRe.FindAllStringSubmatch(string(rootData), -1)
	if len(matches) == 0 {
		return DeadCodeResult{}
	}

	fields := make(map[string]struct{})
	for _, match := range matches {
		fields[match[1]] = struct{}{}
	}

	// Build a version of root.go with declaration lines removed so only
	// reads (e.g. `if flags.agent {`, `c.NoCache = f.noCache`) remain.
	declLineRe := regexp.MustCompile(`(?m)^.*&flags\..*$`)
	rootUsageOnly := declLineRe.ReplaceAllString(string(rootData), "")

	files := listGoFiles(filepath.Join(dir, "internal", "cli"))
	var otherSources []string
	for _, file := range files {
		if filepath.Base(file) == "root.go" {
			otherSources = append(otherSources, rootUsageOnly)
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		otherSources = append(otherSources, string(data))
	}

	result := DeadCodeResult{Total: len(fields)}
	for _, field := range sortedKeys(fields) {
		// Search for both `flags.<field>` (used in Execute) and `f.<field>` or
		// any other struct accessor (used in method receivers like newClient).
		needle := "flags." + field
		receiverNeedle := "." + field
		if containsAny(otherSources, needle) {
			continue
		}
		// Check for method-receiver access patterns (e.g., f.noCache, f.timeout)
		if containsAny(otherSources, receiverNeedle) {
			continue
		}
		result.Dead++
		result.Items = append(result.Items, field)
	}
	return result
}

func checkDeadFunctions(dir string) DeadCodeResult {
	helpersPath := filepath.Join(dir, "internal", "cli", "helpers.go")
	data, err := os.ReadFile(helpersPath)
	if err != nil {
		return DeadCodeResult{}
	}

	funcRe := regexp.MustCompile(`(?m)^func\s+([A-Za-z_]\w*)\s*\(`)
	matches := funcRe.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		return DeadCodeResult{}
	}

	names := make(map[string]struct{})
	for _, match := range matches {
		names[match[1]] = struct{}{}
	}

	// Include helpers.go in the search but strip function definition lines
	// so that a function's own `func name(` line doesn't count as a call.
	// This catches intra-file calls like bold() calling colorEnabled().
	defLineRe := regexp.MustCompile(`(?m)^func\s+[A-Za-z_]\w*\s*\(.*$`)
	helpersUsageOnly := defLineRe.ReplaceAllString(string(data), "")

	files := listGoFiles(filepath.Join(dir, "internal", "cli"))
	var otherSources []string
	for _, file := range files {
		if filepath.Base(file) == "helpers.go" {
			otherSources = append(otherSources, helpersUsageOnly)
			continue
		}
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		otherSources = append(otherSources, string(content))
	}

	// Build external call set: functions called from non-helpers files.
	// Also build intra-helpers call map for transitive reachability.
	externalSources := make([]string, 0, len(otherSources))
	for _, file := range files {
		if filepath.Base(file) == "helpers.go" {
			continue
		}
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		externalSources = append(externalSources, string(content))
	}

	liveSet := make(map[string]bool)
	for _, name := range sortedKeys(names) {
		callRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*\(`)
		for _, source := range externalSources {
			if callRe.MatchString(source) {
				liveSet[name] = true
				break
			}
		}
	}

	// Build intra-helpers call map: for each helper function, which other
	// helper functions does its body call?
	helperBodies := extractFunctionBodies(string(data))
	callsMap := make(map[string][]string)
	for _, caller := range sortedKeys(names) {
		body, ok := helperBodies[caller]
		if !ok {
			continue
		}
		for _, callee := range sortedKeys(names) {
			if callee == caller {
				continue
			}
			callRe := regexp.MustCompile(`\b` + regexp.QuoteMeta(callee) + `\s*\(`)
			if callRe.MatchString(body) {
				callsMap[caller] = append(callsMap[caller], callee)
			}
		}
	}

	// Iterative expansion: mark transitively reachable helpers as live.
	for i := 0; i < 50; i++ {
		changed := false
		for _, fn := range sortedKeys(names) {
			if !liveSet[fn] {
				continue
			}
			for _, callee := range callsMap[fn] {
				if !liveSet[callee] {
					liveSet[callee] = true
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}

	result := DeadCodeResult{Total: len(names)}
	for _, name := range sortedKeys(names) {
		if liveSet[name] {
			continue
		}
		result.Dead++
		result.Items = append(result.Items, name)
	}
	return result
}

// extractFunctionBodies returns a map from function name to its body text
// (everything between its func line and the next top-level func line).
func extractFunctionBodies(source string) map[string]string {
	bodies := make(map[string]string)
	funcLineRe := regexp.MustCompile(`(?m)^func\s+([A-Za-z_]\w*)\s*\(`)
	locs := funcLineRe.FindAllStringIndex(source, -1)
	nameMatches := funcLineRe.FindAllStringSubmatch(source, -1)
	for i, match := range nameMatches {
		name := match[1]
		start := locs[i][1] // end of func line match
		var end int
		if i+1 < len(locs) {
			end = locs[i+1][0]
		} else {
			end = len(source)
		}
		bodies[name] = source[start:end]
	}
	return bodies
}

func checkPipelineIntegrity(dir string) PipelineResult {
	result := PipelineResult{
		Detail: "sync/search/store files not found",
	}

	syncData, _ := os.ReadFile(filepath.Join(dir, "internal", "cli", "sync.go"))
	searchData, _ := os.ReadFile(filepath.Join(dir, "internal", "cli", "search.go"))
	storeData, _ := os.ReadFile(filepath.Join(dir, "internal", "store", "store.go"))

	syncSource := string(syncData)
	searchSource := string(searchData)
	storeSource := string(storeData)

	domainUpsertRe := regexp.MustCompile(`\.Upsert[A-Z]\w*\s*\(`)
	domainSearchRe := regexp.MustCompile(`\.Search[A-Z]\w*\s*\(`)

	result.SyncCallsDomain = domainUpsertRe.MatchString(syncSource)
	result.SearchCallsDomain = domainSearchRe.MatchString(searchSource)
	result.DomainTables = countDomainTables(storeSource)

	var parts []string
	switch {
	case result.SyncCallsDomain:
		parts = append(parts, "sync uses domain-specific Upsert methods")
	case strings.Contains(syncSource, ".Upsert("):
		parts = append(parts, "sync uses generic Upsert only")
	default:
		parts = append(parts, "sync Upsert calls not found")
	}

	switch {
	case result.SearchCallsDomain:
		parts = append(parts, "search uses domain-specific Search methods")
	case strings.Contains(searchSource, ".Search("):
		parts = append(parts, "search uses generic Search only")
	default:
		parts = append(parts, "search methods not found")
	}

	if storeSource != "" {
		parts = append(parts, fmt.Sprintf("%d domain tables found", result.DomainTables))
	}

	result.Detail = strings.Join(parts, "; ")
	return result
}

func deriveDogfoodVerdict(report *DogfoodReport, hasSpec bool) string {
	if hasSpec && report.PathCheck.Tested > 0 && report.PathCheck.Pct < 70 {
		return "FAIL"
	}
	if hasSpec && !report.AuthCheck.Match {
		return "FAIL"
	}
	if report.DeadFlags.Dead >= 3 {
		return "FAIL"
	}
	if report.ExampleCheck.Tested > 0 && (report.ExampleCheck.WithExamples*100/report.ExampleCheck.Tested) < 50 {
		return "FAIL"
	}
	if report.DeadFlags.Dead >= 1 && report.DeadFlags.Dead <= 2 {
		return "WARN"
	}
	if report.DeadFuncs.Dead >= 1 {
		return "WARN"
	}
	if !report.PipelineCheck.SyncCallsDomain {
		return "WARN"
	}
	if len(report.ExampleCheck.InvalidFlags) > 0 {
		return "WARN"
	}
	if report.ExampleCheck.Skipped {
		return "WARN"
	}
	return "PASS"
}

func collectDogfoodIssues(report *DogfoodReport, hasSpec bool) []string {
	var issues []string
	if hasSpec && report.PathCheck.Tested > 0 && report.PathCheck.Pct < 70 {
		issues = append(issues, fmt.Sprintf("%d%% path validity against spec", report.PathCheck.Pct))
	}
	if hasSpec && !report.AuthCheck.Match {
		issues = append(issues, "auth protocol mismatch")
	}
	if report.DeadFlags.Dead >= 3 {
		issues = append(issues, fmt.Sprintf("%d dead flags found", report.DeadFlags.Dead))
	} else if report.DeadFlags.Dead > 0 {
		issues = append(issues, fmt.Sprintf("%d dead flags found", report.DeadFlags.Dead))
	}
	if report.DeadFuncs.Dead > 0 {
		issues = append(issues, fmt.Sprintf("%d dead helper functions found", report.DeadFuncs.Dead))
	}
	if !report.PipelineCheck.SyncCallsDomain {
		issues = append(issues, "sync uses generic Upsert only")
	}
	if report.ExampleCheck.Tested > 0 && (report.ExampleCheck.WithExamples*100/report.ExampleCheck.Tested) < 50 {
		issues = append(issues, fmt.Sprintf("%d%% example coverage", report.ExampleCheck.WithExamples*100/report.ExampleCheck.Tested))
	}
	if len(report.ExampleCheck.InvalidFlags) > 0 {
		issues = append(issues, fmt.Sprintf("%d invalid flags in examples", len(report.ExampleCheck.InvalidFlags)))
	}
	if report.ExampleCheck.Skipped {
		issues = append(issues, fmt.Sprintf("example check skipped: %s", report.ExampleCheck.Detail))
	}
	return issues
}

func checkExamples(dir string) ExampleCheckResult {
	result := ExampleCheckResult{}

	cliName := findCLIName(dir)
	if cliName == "" {
		result.Skipped = true
		result.Detail = "no CLI command directory found"
		return result
	}

	binaryPath, err := buildDogfoodBinary(dir, cliName)
	if err != nil {
		result.Skipped = true
		result.Detail = fmt.Sprintf("could not build CLI binary: %v", err)
		return result
	}
	defer func() { _ = os.Remove(binaryPath) }()

	// Get global flags from root --help
	globalOut, err := runDogfoodCmd(binaryPath, 15*time.Second, "--help")
	if err != nil {
		result.Skipped = true
		result.Detail = fmt.Sprintf("failed to run --help: %v", err)
		return result
	}
	globalFlags := extractFlagNames(globalOut)

	// List command files (same filtering as PathCheck)
	cliDir := filepath.Join(dir, "internal", "cli")
	files := listGoFiles(cliDir)
	var endpointFiles []string
	for _, file := range files {
		base := filepath.Base(file)
		switch base {
		case "root.go", "helpers.go", "doctor.go", "auth.go", "dogfood.go", "scorecard.go", "vision.go":
			continue
		}
		// Only include endpoint commands (those with RunE)
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		if !strings.Contains(string(content), "RunE:") {
			continue
		}
		endpointFiles = append(endpointFiles, file)
	}
	sort.Strings(endpointFiles)
	if len(endpointFiles) > 10 {
		endpointFiles = sampleEvenly(endpointFiles, 10)
	}

	for _, file := range endpointFiles {
		base := strings.TrimSuffix(filepath.Base(file), ".go")
		parts := strings.Split(base, "_")

		result.Tested++
		cmdLabel := strings.Join(parts, " ")

		cmdArgs := append(parts, "--help")
		cmdOut, err := runDogfoodCmd(binaryPath, 15*time.Second, cmdArgs...)
		if err != nil {
			result.Missing = append(result.Missing, cmdLabel)
			continue
		}

		examples := extractExamplesSection(cmdOut)
		if examples == "" {
			result.Missing = append(result.Missing, cmdLabel)
			continue
		}

		result.WithExamples++

		// Extract flags used in examples
		exampleFlags := extractFlagNames(examples)
		// Extract all valid flags from command help + global flags
		cmdFlags := extractFlagNames(cmdOut)
		allValidFlags := make(map[string]struct{})
		for _, f := range cmdFlags {
			allValidFlags[f] = struct{}{}
		}
		for _, f := range globalFlags {
			allValidFlags[f] = struct{}{}
		}

		valid := true
		for _, f := range exampleFlags {
			if _, ok := allValidFlags[f]; !ok {
				result.InvalidFlags = append(result.InvalidFlags, "--"+f)
				valid = false
			}
		}
		if valid {
			result.ValidExamples++
		}
	}

	result.InvalidFlags = uniqueSorted(result.InvalidFlags)
	result.Missing = uniqueSorted(result.Missing)

	if result.Tested == 0 {
		result.Detail = "no endpoint commands found to test"
	} else {
		result.Detail = fmt.Sprintf("%d/%d commands have examples", result.WithExamples, result.Tested)
		if len(result.InvalidFlags) > 0 {
			result.Detail += fmt.Sprintf(" (%d invalid flags: %s)", len(result.InvalidFlags), strings.Join(result.InvalidFlags, ", "))
		}
	}

	return result
}

func findCLIName(dir string) string {
	cmdDir := filepath.Join(dir, "cmd")
	entries, err := os.ReadDir(cmdDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() && naming.IsCLIDirName(entry.Name()) {
			return entry.Name()
		}
	}
	return ""
}

func buildDogfoodBinary(dir, cliName string) (string, error) {
	buildPath, err := filepath.Abs(filepath.Join(dir, cliName+"-dogfood"))
	if err != nil {
		return "", fmt.Errorf("resolving dogfood binary path: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", buildPath, "./cmd/"+cliName)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("timed out after 2m")
		}
		return "", err
	}
	return buildPath, nil
}

func runDogfoodCmd(binary string, timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	out, err := cmd.CombinedOutput()
	if err != nil && ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("timed out after %s", timeout)
	}
	// --help often returns exit 0, but accept output regardless
	if len(out) > 0 {
		return string(out), nil
	}
	return "", err
}

func extractExamplesSection(helpOutput string) string {
	lines := strings.Split(helpOutput, "\n")
	var inExamples bool
	var examples []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Examples:" {
			inExamples = true
			continue
		}
		if inExamples {
			// Section headers in Cobra help are non-indented and non-empty
			if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
				break
			}
			examples = append(examples, line)
		}
	}
	return strings.TrimSpace(strings.Join(examples, "\n"))
}

func extractFlagNames(text string) []string {
	re := regexp.MustCompile(`--([a-z][-a-z0-9]*)`)
	matches := re.FindAllStringSubmatch(text, -1)
	seen := make(map[string]struct{})
	var flags []string
	for _, match := range matches {
		name := match[1]
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			flags = append(flags, name)
		}
	}
	sort.Strings(flags)
	return flags
}

func sampleEvenly(items []string, n int) []string {
	if len(items) <= n {
		return items
	}
	step := float64(len(items)) / float64(n)
	result := make([]string, n)
	for i := 0; i < n; i++ {
		idx := int(float64(i) * step)
		result[i] = items[idx]
	}
	return result
}

func compileSpecPathPatterns(paths []string) []*regexp.Regexp {
	paramRe := regexp.MustCompile(`\\\{[^/]+\\\}`)
	var patterns []*regexp.Regexp
	for _, path := range paths {
		quoted := regexp.QuoteMeta(path)
		regex := "^" + paramRe.ReplaceAllString(quoted, `[^/]+`) + "$"
		patterns = append(patterns, regexp.MustCompile(regex))
	}
	return patterns
}

func pathMatchesSpec(path string, patterns []*regexp.Regexp) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(path) {
			return true
		}
	}
	return false
}

func listGoFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	sort.Strings(files)
	return files
}

func countDomainTables(storeSource string) int {
	if storeSource == "" {
		return 0
	}

	tableRe := regexp.MustCompile(`(?is)CREATE TABLE IF NOT EXISTS\s+\w+\s*\((.*?)\);`)
	matches := tableRe.FindAllStringSubmatch(storeSource, -1)
	count := 0
	for _, match := range matches {
		columns := 0
		for _, line := range strings.Split(match[1], "\n") {
			line = strings.TrimSpace(strings.TrimSuffix(line, ","))
			if line == "" {
				continue
			}
			upper := strings.ToUpper(line)
			if strings.HasPrefix(upper, "PRIMARY KEY") || strings.HasPrefix(upper, "FOREIGN KEY") || strings.HasPrefix(upper, "UNIQUE") || strings.HasPrefix(upper, "CONSTRAINT") || strings.HasPrefix(upper, "CHECK") {
				continue
			}
			columns++
		}
		if columns > 3 {
			count++
		}
	}
	return count
}

func uniqueSorted(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(items))
	for _, item := range items {
		set[item] = struct{}{}
	}
	return sortedKeys(set)
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func containsAny(sources []string, needle string) bool {
	for _, source := range sources {
		if strings.Contains(source, needle) {
			return true
		}
	}
	return false
}
