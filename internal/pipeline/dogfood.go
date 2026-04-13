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
	"gopkg.in/yaml.v3"
)

type DogfoodReport struct {
	Dir                string                   `json:"dir"`
	SpecPath           string                   `json:"spec_path,omitempty"`
	Verdict            string                   `json:"verdict"`
	PathCheck          PathCheckResult          `json:"path_check"`
	AuthCheck          AuthCheckResult          `json:"auth_check"`
	DeadFlags          DeadCodeResult           `json:"dead_flags"`
	DeadFuncs          DeadCodeResult           `json:"dead_functions"`
	PipelineCheck      PipelineResult           `json:"pipeline_check"`
	ExampleCheck       ExampleCheckResult       `json:"example_check"`
	WiringCheck        WiringCheckResult        `json:"wiring_check"`
	NovelFeaturesCheck NovelFeaturesCheckResult `json:"novel_features_check"`
	Issues             []string                 `json:"issues"`
}

// NovelFeaturesCheckResult tracks whether transcendence features planned
// during absorb actually survived the build as registered CLI commands.
type NovelFeaturesCheckResult struct {
	Planned int      `json:"planned"`
	Found   int      `json:"found"`
	Missing []string `json:"missing,omitempty"`
	Skipped bool     `json:"skipped,omitempty"`
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

type WiringCheckResult struct {
	CommandTree      CommandTreeResult      `json:"command_tree"`
	ConfigConsist    ConfigConsistResult    `json:"config_consistency"`
	WorkflowComplete WorkflowCompleteResult `json:"workflow_completeness"`
}

type CommandTreeResult struct {
	Defined      int      `json:"defined"`
	Registered   int      `json:"registered"`
	Unregistered []string `json:"unregistered,omitempty"`
}

type ConfigConsistResult struct {
	WriteFields []string `json:"write_fields,omitempty"`
	ReadFields  []string `json:"read_fields,omitempty"`
	Mismatched  []string `json:"mismatched,omitempty"`
	Consistent  bool     `json:"consistent"`
}

type WorkflowCompleteResult struct {
	Skipped       bool     `json:"skipped,omitempty"`
	TotalSteps    int      `json:"total_steps"`
	MappedSteps   int      `json:"mapped_steps"`
	UnmappedSteps []string `json:"unmapped_steps,omitempty"`
	Detail        string   `json:"detail"`
}

type openAPISpec struct {
	Paths []string
	Auth  apispec.AuthConfig
}

func RunDogfood(dir, specPath string, opts ...DogfoodOption) (*DogfoodReport, error) {
	cfg := dogfoodConfig{}
	for _, o := range opts {
		o(&cfg)
	}

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
	report.WiringCheck = checkWiring(dir)
	report.NovelFeaturesCheck = checkNovelFeatures(dir, cfg.researchDir)
	report.Issues = collectDogfoodIssues(report, spec != nil)
	report.Verdict = deriveDogfoodVerdict(report, spec != nil)

	if err := writeDogfoodResults(report, dir); err != nil {
		return nil, err
	}

	return report, nil
}

type dogfoodConfig struct {
	researchDir string
}

// DogfoodOption configures optional behavior for RunDogfood.
type DogfoodOption func(*dogfoodConfig)

// WithResearchDir provides the pipeline directory containing research.json
// so dogfood can validate novel features against registered commands.
func WithResearchDir(dir string) DogfoodOption {
	return func(c *dogfoodConfig) {
		c.researchDir = dir
	}
}

// checkNovelFeatures validates that transcendence features from research.json
// have corresponding registered commands in the generated CLI. It also writes
// the verified list back as novel_features_built so downstream consumers
// (README, publish) only claim what actually exists.
func checkNovelFeatures(cliDir, researchDir string) NovelFeaturesCheckResult {
	if researchDir == "" {
		return NovelFeaturesCheckResult{Skipped: true}
	}
	research, err := LoadResearch(researchDir)
	if err != nil || len(research.NovelFeatures) == 0 {
		return NovelFeaturesCheckResult{Skipped: true}
	}

	paths, leaves := collectRegisteredCommands(cliDir)

	result := NovelFeaturesCheckResult{
		Planned: len(research.NovelFeatures),
	}
	built := make([]NovelFeature, 0)
	for _, nf := range research.NovelFeatures {
		if matchNovelFeature(nf, paths, leaves) {
			result.Found++
			built = append(built, nf)
		} else {
			result.Missing = append(result.Missing, nf.Command)
		}
	}

	// Write the verified list back to research.json so the README and
	// publish steps only reference features that actually exist.
	if err := WriteNovelFeaturesBuilt(researchDir, built); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write novel_features_built: %v\n", err)
	}

	return result
}

// matchNovelFeature reports whether a planned novel feature has a
// corresponding built command. When paths are available it matches on
// full command paths (so "portfolio perf" does not collide with
// "analytics perf"); when paths are empty it falls back to leaf-only
// matching against the flat leaves set for CLIs where the tree walker
// couldn't construct a tree.
//
// Match strategies in both modes: exact match, then hyphen-prefix on
// the last segment (sibling specialization — "auth login" → "auth
// login-chrome"). Aliases run the same rules.
//
// Paths and leaves are both consulted. Path matching is preferred when
// it fires because it's more specific, but leaf matching runs as a
// fallback on every planned command — tree reconstruction only sees
// commands wired via direct `rootCmd.AddCommand(newFooCmd())` /
// `cmd.AddCommand(newFooCmd())` literals, so any CLI using variable-
// based or late-bound registration (`sub := newFooCmd(); parent.
// AddCommand(sub)`) will have a partial paths map. Treating a
// non-empty paths map as "complete" would false-negative legitimate
// built commands.
func matchNovelFeature(nf NovelFeature, paths, leaves map[string]bool) bool {
	plan := commandPath(nf.Command)
	if plan == "" {
		return false
	}
	try := func(p string) bool {
		return matchPath(p, paths) || matchLeaf(p, leaves)
	}
	if try(plan) {
		return true
	}
	for _, alias := range nf.Aliases {
		if ap := commandPath(alias); ap != "" && try(ap) {
			return true
		}
	}
	return false
}

// matchPath matches a planned path against a set of built paths:
// exact match, or sibling hyphen-prefix (same parent, leaf ↔ leaf-foo).
func matchPath(plan string, paths map[string]bool) bool {
	if paths[plan] {
		return true
	}
	parent, leaf := splitCommandPath(plan)
	if leaf == "" {
		return false
	}
	for path := range paths {
		pp, pl := splitCommandPath(path)
		if pp != parent || pl == "" {
			continue
		}
		if strings.HasPrefix(pl, leaf+"-") || strings.HasPrefix(leaf, pl+"-") {
			return true
		}
	}
	return false
}

// matchLeaf is the legacy leaf-only matcher used as a fallback when the
// built command tree can't be reconstructed. Ignores nesting.
func matchLeaf(plan string, leaves map[string]bool) bool {
	_, leaf := splitCommandPath(plan)
	if leaf == "" {
		return false
	}
	if leaves[leaf] {
		return true
	}
	for use := range leaves {
		if strings.HasPrefix(use, leaf+"-") || strings.HasPrefix(leaf, use+"-") {
			return true
		}
	}
	return false
}

// commandPath strips flag tokens from a command string and joins the
// remaining leading non-flag tokens into a space-separated path. Stops
// at the first flag because any bare word that follows is a flag value,
// not a command token — "options --moneyness otm" → "options".
func commandPath(cmd string) string {
	tokens := strings.Fields(strings.ToLower(cmd))
	path := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if strings.HasPrefix(t, "-") {
			break
		}
		path = append(path, t)
	}
	return strings.Join(path, " ")
}

// splitCommandPath returns the parent and leaf segments of a space-
// separated path. "portfolio perf" → ("portfolio", "perf"), "digest" →
// ("", "digest").
func splitCommandPath(path string) (parent, leaf string) {
	i := strings.LastIndex(path, " ")
	if i < 0 {
		return "", path
	}
	return path[:i], path[i+1:]
}

// collectRegisteredCommands reads the CLI's internal/cli/*.go files once
// and returns two views of its registered cobra commands:
//
//   - paths: full space-separated command paths (e.g. "portfolio perf",
//     "auth login-chrome"), reconstructed by walking AddCommand edges.
//     Empty when the tree walker can't identify roots or the source
//     doesn't follow the expected pattern.
//   - leaves: a flat set of every Use: name (e.g. "perf", "login-chrome")
//     regardless of nesting. Used by callers as a leaf-only fallback
//     when paths is empty.
//
// Callers prefer paths when non-empty for accuracy, and fall through to
// leaves when the tree walker produced nothing.
func collectRegisteredCommands(dir string) (paths, leaves map[string]bool) {
	cliDir := filepath.Join(dir, "internal", "cli")
	files := listGoFiles(cliDir)

	type cmdFunc struct {
		use      string
		children []string
	}
	funcs := map[string]*cmdFunc{}
	var rootFuncs []string
	leaves = map[string]bool{}

	// Root detection scans the full file because the wiring function
	// (Execute / helpers) isn't a new*Cmd constructor.
	rootAddRe := regexp.MustCompile(`rootCmd\.AddCommand\(\s*(new\w+Cmd)\b`)
	funcHeaderRe := regexp.MustCompile(`func\s+(new\w+Cmd)\s*\(`)
	useRe := regexp.MustCompile(`Use:\s*"([^"\s]+)`)
	addChildRe := regexp.MustCompile(`\.AddCommand\(\s*(new\w+Cmd)\b`)

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		src := string(data)
		for _, rm := range rootAddRe.FindAllStringSubmatch(src, -1) {
			rootFuncs = append(rootFuncs, rm[1])
		}
		for _, u := range useRe.FindAllStringSubmatch(src, -1) {
			if name := strings.Fields(u[1])[0]; name != "" {
				leaves[name] = true
			}
		}
		for _, m := range funcHeaderRe.FindAllStringSubmatchIndex(src, -1) {
			name := src[m[2]:m[3]]
			body := extractFuncBody(src, m[1])
			if body == "" {
				continue
			}
			entry := &cmdFunc{}
			if u := useRe.FindStringSubmatch(body); u != nil {
				entry.use = strings.Fields(u[1])[0]
			}
			for _, cm := range addChildRe.FindAllStringSubmatch(body, -1) {
				entry.children = append(entry.children, cm[1])
			}
			funcs[name] = entry
		}
	}

	paths = map[string]bool{}
	if len(rootFuncs) == 0 || len(funcs) == 0 {
		return paths, leaves
	}

	type qItem struct{ funcName, prefix string }
	queue := make([]qItem, 0, len(rootFuncs))
	for _, rf := range rootFuncs {
		queue = append(queue, qItem{funcName: rf})
	}
	seen := map[qItem]bool{}
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		if seen[it] {
			continue
		}
		seen[it] = true
		fn, ok := funcs[it.funcName]
		if !ok || fn.use == "" {
			continue
		}
		path := fn.use
		if it.prefix != "" {
			path = it.prefix + " " + fn.use
		}
		paths[path] = true
		for _, child := range fn.children {
			queue = append(queue, qItem{funcName: child, prefix: path})
		}
	}
	return paths, leaves
}

// extractFuncBody returns the balanced-brace body of a Go function given
// the position of its header (regex match end). Empty string if no body
// is found.
func extractFuncBody(src string, headerEnd int) string {
	i := strings.Index(src[headerEnd:], "{")
	if i < 0 {
		return ""
	}
	start := headerEnd + i + 1
	depth := 1
	end := start
	for end < len(src) && depth > 0 {
		switch src[end] {
		case '{':
			depth++
		case '}':
			depth--
		}
		end++
	}
	return src[start:end]
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
	// Try internal YAML spec format first (starts with "name:" + "resources:").
	if internal, err := tryLoadInternalYAMLSpec(specPath); err != nil {
		return nil, err
	} else if internal != nil {
		return internalSpecToDogfoodSpec(internal), nil
	}

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

	// Also read config.go — Bearer/Bot prefix may be constructed there
	// rather than in client.go (e.g., config.AuthHeader() returns "Bearer " + token).
	configData, _ := os.ReadFile(filepath.Join(dir, "internal", "config", "config.go"))

	combinedSource := string(clientData) + string(configData)
	switch {
	case strings.Contains(combinedSource, `"Bot "`):
		result.GeneratedFmt = "Bot "
	case strings.Contains(combinedSource, `"Bearer "`):
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

	// Collect external sources (everything except helpers.go) for liveness seeding.
	files := listGoFiles(filepath.Join(dir, "internal", "cli"))
	var externalSources []string
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

	// Seed live set: functions called from external (non-helpers) files.
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
		start := locs[i][1]
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
	if len(report.WiringCheck.CommandTree.Unregistered) > 0 {
		return "FAIL"
	}
	if !report.WiringCheck.ConfigConsist.Consistent && len(report.WiringCheck.ConfigConsist.Mismatched) > 0 {
		return "FAIL"
	}
	if len(report.WiringCheck.WorkflowComplete.UnmappedSteps) > 0 {
		return "WARN"
	}
	if len(report.NovelFeaturesCheck.Missing) > 0 {
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
	if len(report.WiringCheck.CommandTree.Unregistered) > 0 {
		issues = append(issues, fmt.Sprintf("%d unregistered commands: %s",
			len(report.WiringCheck.CommandTree.Unregistered),
			strings.Join(report.WiringCheck.CommandTree.Unregistered, ", ")))
	}
	if !report.WiringCheck.ConfigConsist.Consistent && len(report.WiringCheck.ConfigConsist.Mismatched) > 0 {
		issues = append(issues, fmt.Sprintf("config inconsistency: write fields %v vs read fields %v",
			report.WiringCheck.ConfigConsist.WriteFields,
			report.WiringCheck.ConfigConsist.ReadFields))
	}
	if len(report.WiringCheck.WorkflowComplete.UnmappedSteps) > 0 {
		issues = append(issues, fmt.Sprintf("%d unmapped workflow steps: %s",
			len(report.WiringCheck.WorkflowComplete.UnmappedSteps),
			strings.Join(report.WiringCheck.WorkflowComplete.UnmappedSteps, ", ")))
	}
	if len(report.NovelFeaturesCheck.Missing) > 0 {
		issues = append(issues, fmt.Sprintf("%d/%d novel features missing: %s",
			len(report.NovelFeaturesCheck.Missing),
			report.NovelFeaturesCheck.Planned,
			strings.Join(report.NovelFeaturesCheck.Missing, ", ")))
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

// checkWiring orchestrates all three wiring sub-checks.
func checkWiring(dir string) WiringCheckResult {
	return WiringCheckResult{
		CommandTree:      checkCommandTree(dir),
		ConfigConsist:    checkConfigConsistency(dir),
		WorkflowComplete: checkWorkflowCompleteness(dir),
	}
}

// checkCommandTree scans internal/cli/*.go for command constructor functions
// (func newXxxCmd) and verifies each is wired via an AddCommand call somewhere
// in the source. This is pure static analysis — no binary build or help-text
// parsing needed — and correctly handles deeply nested command hierarchies that
// help-text scraping misses.
func checkCommandTree(dir string) CommandTreeResult {
	result := CommandTreeResult{}

	cliDir := filepath.Join(dir, "internal", "cli")
	files := listGoFiles(cliDir)

	// Phase 1: Find all command constructor definitions and their Use: names.
	// A constructor is func newXxxCmd(...) — we extract both the function name
	// and the cobra Use: field (the command name users see).
	constructorRe := regexp.MustCompile(`(?m)^func\s+(new\w+Cmd)\s*\(`)
	useFieldRe := regexp.MustCompile(`(?m)Use:\s*"([^"\s]+)`)

	type cmdDef struct {
		constructor string // e.g. "newBookingsCmd"
		useName     string // e.g. "bookings"
	}

	var allDefs []cmdDef
	allSource := strings.Builder{}

	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		allSource.WriteString(content)
		allSource.WriteString("\n")

		// Find constructors in this file and pair with their Use: field
		constructors := constructorRe.FindAllStringSubmatch(content, -1)
		useFields := useFieldRe.FindAllStringSubmatch(content, -1)

		// Build a lookup of Use: names for this file
		var useNames []string
		for _, m := range useFields {
			name := strings.Fields(m[1])[0]
			if name != "" {
				useNames = append(useNames, name)
			}
		}

		for i, m := range constructors {
			funcName := m[1]
			// Skip the root command constructor — it's not added via AddCommand
			if funcName == "newRootCmd" {
				continue
			}
			useName := funcName
			if i < len(useNames) {
				useName = useNames[i]
			}
			allDefs = append(allDefs, cmdDef{constructor: funcName, useName: useName})
		}
	}

	result.Defined = len(allDefs)
	if result.Defined == 0 {
		return result
	}

	// Phase 2: Check which constructors are called from other functions.
	// A constructor is "wired" if it appears as a call (funcName + "(") outside
	// its own definition. This catches both direct AddCommand(newXxxCmd(...))
	// and indirect patterns like: sub := newXxxCmd(flags); cmd.AddCommand(sub).
	source := allSource.String()
	for _, def := range allDefs {
		// Count occurrences of "constructorName(" in all source.
		// >=2 means at least one call site beyond the func definition itself.
		if strings.Count(source, def.constructor+"(") >= 2 {
			result.Registered++
		} else {
			result.Unregistered = append(result.Unregistered, def.useName)
		}
	}
	sort.Strings(result.Unregistered)
	return result
}

// extractCommandNames extracts command names from cobra --help output.
// It looks for the "Available Commands:" section.
func extractCommandNames(helpOutput string) []string {
	lines := strings.Split(helpOutput, "\n")
	var inCommands bool
	var cmds []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "Available Commands:" {
			inCommands = true
			continue
		}
		if inCommands {
			if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
				break
			}
			parts := strings.Fields(trimmed)
			if len(parts) > 0 {
				cmds = append(cmds, parts[0])
			}
		}
	}
	return cmds
}

// checkConfigConsistency scans CLI source for token/credential write and read
// sites, then verifies they reference the same config field names.
func checkConfigConsistency(dir string) ConfigConsistResult {
	result := ConfigConsistResult{Consistent: true}

	// Collect all Go source files recursively
	var sources []string
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			sources = append(sources, path)
		}
		return nil
	})

	writePatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)SaveTokens?\s*\(`),
		regexp.MustCompile(`(?i)SetTokens?\s*\(`),
		regexp.MustCompile(`(?i)WriteTokens?\s*\(`),
		regexp.MustCompile(`(?i)config\.Set\s*\(\s*"([^"]+)"`),
		regexp.MustCompile(`(?i)viper\.Set\s*\(\s*"([^"]+)"`),
	}
	readPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)AuthHeader\s*\(`),
		regexp.MustCompile(`(?i)GetTokens?\s*\(`),
		regexp.MustCompile(`(?i)ReadTokens?\s*\(`),
		regexp.MustCompile(`(?i)config\.Get\s*\(\s*"([^"]+)"`),
		regexp.MustCompile(`(?i)viper\.Get\s*\(\s*"([^"]+)"`),
	}

	// Also look for string literals that name token fields
	fieldExtractRe := regexp.MustCompile(`"([^"]*(?i:token|credential|secret|key|auth)[^"]*)"`)

	writeFields := make(map[string]struct{})
	readFields := make(map[string]struct{})

	for _, srcPath := range sources {
		data, err := os.ReadFile(srcPath)
		if err != nil {
			continue
		}
		content := string(data)

		for _, pat := range writePatterns {
			if pat.MatchString(content) {
				// Extract field names from the same lines
				matches := pat.FindAllStringSubmatch(content, -1)
				for _, m := range matches {
					if len(m) > 1 && m[1] != "" {
						writeFields[m[1]] = struct{}{}
					}
				}
				// Also extract nearby token-related string literals
				for _, line := range strings.Split(content, "\n") {
					if pat.MatchString(line) {
						fieldMatches := fieldExtractRe.FindAllStringSubmatch(line, -1)
						for _, fm := range fieldMatches {
							writeFields[fm[1]] = struct{}{}
						}
					}
				}
			}
		}

		for _, pat := range readPatterns {
			if pat.MatchString(content) {
				matches := pat.FindAllStringSubmatch(content, -1)
				for _, m := range matches {
					if len(m) > 1 && m[1] != "" {
						readFields[m[1]] = struct{}{}
					}
				}
				for _, line := range strings.Split(content, "\n") {
					if pat.MatchString(line) {
						fieldMatches := fieldExtractRe.FindAllStringSubmatch(line, -1)
						for _, fm := range fieldMatches {
							readFields[fm[1]] = struct{}{}
						}
					}
				}
			}
		}
	}

	result.WriteFields = sortedKeys(writeFields)
	result.ReadFields = sortedKeys(readFields)

	// If both sides have fields, check overlap
	if len(writeFields) > 0 && len(readFields) > 0 {
		overlap := false
		for wf := range writeFields {
			if _, ok := readFields[wf]; ok {
				overlap = true
				break
			}
		}
		if !overlap {
			result.Consistent = false
			// Mismatched = write fields not found in read fields
			for _, wf := range result.WriteFields {
				if _, ok := readFields[wf]; !ok {
					result.Mismatched = append(result.Mismatched, wf)
				}
			}
			for _, rf := range result.ReadFields {
				if _, ok := writeFields[rf]; !ok {
					result.Mismatched = append(result.Mismatched, rf)
				}
			}
			result.Mismatched = uniqueSorted(result.Mismatched)
		}
	}

	return result
}

// workflowManifest represents the structure of workflow_verify.yaml.
type workflowManifest struct {
	Workflows []workflowDef `yaml:"workflows"`
}

type workflowDef struct {
	Name  string         `yaml:"name"`
	Steps []workflowStep `yaml:"steps"`
}

type workflowStep struct {
	Command string `yaml:"command"`
	Name    string `yaml:"name"`
}

// checkWorkflowCompleteness verifies that every step in a workflow_verify.yaml
// manifest has a corresponding registered CLI command.
func checkWorkflowCompleteness(dir string) WorkflowCompleteResult {
	manifestPath := filepath.Join(dir, "workflow_verify.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return WorkflowCompleteResult{
			Skipped: true,
			Detail:  "no workflow_verify.yaml found",
		}
	}

	var manifest workflowManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return WorkflowCompleteResult{
			Skipped: true,
			Detail:  fmt.Sprintf("failed to parse workflow_verify.yaml: %v", err),
		}
	}

	// Collect all step commands
	var stepCommands []string
	for _, wf := range manifest.Workflows {
		for _, step := range wf.Steps {
			if step.Command != "" {
				stepCommands = append(stepCommands, step.Command)
			}
		}
	}

	result := WorkflowCompleteResult{
		TotalSteps: len(stepCommands),
	}

	if len(stepCommands) == 0 {
		result.Detail = "manifest has no steps"
		return result
	}

	// Get help output to check command existence
	cliName := findCLIName(dir)
	if cliName == "" {
		result.Detail = "no CLI binary to verify against"
		result.MappedSteps = result.TotalSteps
		return result
	}

	binaryPath, err := buildDogfoodBinary(dir, cliName)
	if err != nil {
		result.Detail = fmt.Sprintf("could not build CLI binary: %v", err)
		result.MappedSteps = result.TotalSteps
		return result
	}
	defer func() { _ = os.Remove(binaryPath) }()

	helpOut, err := runDogfoodCmd(binaryPath, 15*time.Second, "--help")
	if err != nil {
		result.Detail = fmt.Sprintf("failed to run --help: %v", err)
		result.MappedSteps = result.TotalSteps
		return result
	}

	// Gather subcommand help too
	helpLower := strings.ToLower(helpOut)
	topCmds := extractCommandNames(helpOut)
	for _, topCmd := range topCmds {
		subOut, err := runDogfoodCmd(binaryPath, 15*time.Second, topCmd, "--help")
		if err == nil {
			helpLower += "\n" + strings.ToLower(subOut)
		}
	}

	for _, cmd := range stepCommands {
		// Check if all parts of the command appear in help
		cmdLower := strings.ToLower(cmd)
		parts := strings.Fields(cmdLower)
		found := true
		for _, part := range parts {
			if !strings.Contains(helpLower, part) {
				found = false
				break
			}
		}
		if found {
			result.MappedSteps++
		} else {
			result.UnmappedSteps = append(result.UnmappedSteps, cmd)
		}
	}

	result.UnmappedSteps = uniqueSorted(result.UnmappedSteps)
	result.Detail = fmt.Sprintf("%d/%d workflow steps mapped to commands", result.MappedSteps, result.TotalSteps)
	return result
}
