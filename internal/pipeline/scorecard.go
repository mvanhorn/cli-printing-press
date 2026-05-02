package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	apispec "github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"gopkg.in/yaml.v3"
)

// infraCoreFiles are CLI infrastructure files excluded from workflow/insight scoring.
// These contain shared helpers and framework code, not individual commands.
var infraCoreFiles = map[string]bool{
	"helpers.go": true, "root.go": true, "doctor.go": true, "auth.go": true,
}

// infraAllFiles extends infraCoreFiles with vision/data-layer commands that are
// scored by their own dedicated dimensions (vision, sync_correctness, etc.)
// and should not be double-counted by breadth or sampled as generic commands.
var infraAllFiles = map[string]bool{
	"helpers.go": true, "root.go": true, "doctor.go": true, "auth.go": true,
	"export.go": true, "import.go": true, "search.go": true, "sync.go": true,
	"tail.go": true, "analytics.go": true,
}

// Scorecard holds the auto-scored evaluation of a generated CLI against the Steinberger bar.
type Scorecard struct {
	APIName            string       `json:"api_name"`
	Steinberger        SteinerScore `json:"steinberger"`
	CompetitorScores   []CompScore  `json:"competitor_scores"`
	OverallGrade       string       `json:"overall_grade"`
	GapReport          []string     `json:"gap_report"`
	UnscoredDimensions []string     `json:"unscored_dimensions,omitempty"`

	verifyCalibrationFloor   int
	browserSessionUnverified bool
}

// SteinerScore breaks down the Steinberger bar into 11 dimensions, each 0-10.
type SteinerScore struct {
	OutputModes           int `json:"output_modes"`             // 0-10
	Auth                  int `json:"auth"`                     // 0-10
	ErrorHandling         int `json:"error_handling"`           // 0-10
	TerminalUX            int `json:"terminal_ux"`              // 0-10
	README                int `json:"readme"`                   // 0-10
	Doctor                int `json:"doctor"`                   // 0-10
	AgentNative           int `json:"agent_native"`             // 0-10
	MCPQuality            int `json:"mcp_quality"`              // 0-10
	MCPDescriptionQuality int `json:"mcp_description_quality"`  // 0-10; unscored when no tools-manifest.json. Penalizes thin per-tool descriptions (the same threshold as `printing-press tools-audit` thin-mcp-description).
	MCPTokenEff           int `json:"mcp_token_efficiency"`     // 0-10; unscored when no MCP surface
	MCPRemoteTransport    int `json:"mcp_remote_transport"`     // 0-10; unscored when no MCP surface. Rewards remote-capable servers per Anthropic's 2026-04 MCP guidance.
	MCPToolDesign         int `json:"mcp_tool_design"`          // 0-10; unscored when no MCP surface or endpoint count below toolDesignMinEndpoints. Rewards intent-grouped tools vs. endpoint mirrors.
	MCPSurfaceStrategy    int `json:"mcp_surface_strategy"`     // 0-10; unscored unless the endpoint surface exceeds surfaceStrategyLargeThreshold or code-orchestration is explicitly used. Penalizes endpoint-mirror at scale.
	LocalCache            int `json:"local_cache"`              // 0-10
	CacheFreshness        int `json:"cache_freshness"`          // 0-10; unscored when the CLI has no local store
	Breadth               int `json:"breadth"`                  // 0-10: how many commands (penalizes empty CLIs)
	Vision                int `json:"vision"`                   // 0-10
	Workflows             int `json:"workflows"`                // 0-10
	Insight               int `json:"insight"`                  // 0-10
	AgentWorkflow         int `json:"agent_workflow_readiness"` // 0-10: HeyGen-derived - async jobs, profiles, deliver, feedback
	// Tier 2: Domain Correctness (semantic checks)
	PathValidity          int    `json:"path_validity"`           // 0-10
	AuthProtocol          int    `json:"auth_protocol"`           // 0-10
	DataPipelineIntegrity int    `json:"data_pipeline_integrity"` // 0-10
	SyncCorrectness       int    `json:"sync_correctness"`        // 0-10
	TypeFidelity          int    `json:"type_fidelity"`           // 0-5
	DeadCode              int    `json:"dead_code"`               // 0-5
	LiveAPIVerification   int    `json:"live_api_verification"`   // 0-10; unscored when verify ran in mock/structural mode or was skipped
	Total                 int    `json:"total"`                   // 0-100 (weighted: 50% infrastructure + 50% domain)
	Percentage            int    `json:"percentage"`              // 0-100
	CalibrationNote       string `json:"calibration_note,omitempty"`
}

// Dimension identifiers used by recordOptionalScore, scorecardTierMax,
// IsDimensionUnscored, and renderers (renderHumanScorecard,
// writeScorecardMD). Any dimension that can land in
// Scorecard.UnscoredDimensions has a constant here so a typo at any
// call site fails the compile rather than silently returning false
// from IsDimensionUnscored. The string values match the JSON struct
// tags above and must stay in lockstep.
const (
	DimMCPDescriptionQuality = "mcp_description_quality"
	DimMCPTokenEfficiency    = "mcp_token_efficiency"
	DimMCPRemoteTransport    = "mcp_remote_transport"
	DimMCPToolDesign         = "mcp_tool_design"
	DimMCPSurfaceStrategy    = "mcp_surface_strategy"
	DimCacheFreshness        = "cache_freshness"
	DimPathValidity          = "path_validity"
	DimAuthProtocol          = "auth_protocol"
	DimLiveAPIVerification   = "live_api_verification"
)

// CompScore compares our score against a competitor on a single dimension.
type CompScore struct {
	Name       string `json:"name"`
	OurScore   int    `json:"our_score"`
	TheirScore int    `json:"their_score"`
	WeWin      bool   `json:"we_win"`
}

// RunScorecard evaluates generated CLI files and produces a scorecard.
// If verifyReport is non-nil, verify results calibrate the final score.
func RunScorecard(outputDir, pipelineDir, specPath string, verifyReport *VerifyReport) (*Scorecard, error) {
	sc := &Scorecard{APIName: filepath.Base(outputDir)}

	if err := scoreScorecardDimensions(sc, outputDir, specPath, verifyReport); err != nil {
		return nil, err
	}
	finalizeScorecard(sc, outputDir, pipelineDir, verifyReport)

	if err := writeScorecardArtifacts(sc, pipelineDir); err != nil {
		return sc, err
	}

	return sc, nil
}

func scoreScorecardDimensions(sc *Scorecard, outputDir, specPath string, verifyReport *VerifyReport) error {
	scoreInfrastructureDimensions(sc, outputDir)
	if err := scoreSpecDimensions(sc, outputDir, specPath); err != nil {
		return err
	}
	scoreDomainDimensions(sc, outputDir, verifyReport)
	return nil
}

func scoreInfrastructureDimensions(sc *Scorecard, outputDir string) {
	sc.Steinberger.OutputModes = scoreOutputModes(outputDir)
	sc.Steinberger.Auth = scoreAuth(outputDir)
	sc.Steinberger.ErrorHandling = scoreErrorHandling(outputDir)
	sc.Steinberger.TerminalUX = scoreTerminalUX(outputDir)
	sc.Steinberger.README = scoreREADME(outputDir)
	sc.Steinberger.Doctor = scoreDoctor(outputDir)
	sc.Steinberger.AgentNative = scoreAgentNative(outputDir)
	sc.Steinberger.MCPQuality = scoreMCPQuality(outputDir)
	mcpDescScore, mcpDescScored := scoreMCPDescriptionQuality(outputDir)
	recordOptionalScore(sc, &sc.Steinberger.MCPDescriptionQuality, DimMCPDescriptionQuality, mcpDescScore, mcpDescScored)
	mcpTokenScore, mcpTokenScored := scoreMCPTokenEfficiency(outputDir)
	recordOptionalScore(sc, &sc.Steinberger.MCPTokenEff, DimMCPTokenEfficiency, mcpTokenScore, mcpTokenScored)
	remoteScore, remoteScored := scoreMCPRemoteTransport(outputDir)
	recordOptionalScore(sc, &sc.Steinberger.MCPRemoteTransport, DimMCPRemoteTransport, remoteScore, remoteScored)
	toolDesignScore, toolDesignScored := scoreMCPToolDesign(outputDir)
	recordOptionalScore(sc, &sc.Steinberger.MCPToolDesign, DimMCPToolDesign, toolDesignScore, toolDesignScored)
	strategyScore, strategyScored := scoreMCPSurfaceStrategy(outputDir)
	recordOptionalScore(sc, &sc.Steinberger.MCPSurfaceStrategy, DimMCPSurfaceStrategy, strategyScore, strategyScored)
	sc.Steinberger.LocalCache = scoreLocalCache(outputDir)
	cacheFreshnessScore, cacheFreshnessScored := scoreCacheFreshness(outputDir)
	recordOptionalScore(sc, &sc.Steinberger.CacheFreshness, DimCacheFreshness, cacheFreshnessScore, cacheFreshnessScored)
	sc.Steinberger.Breadth = scoreBreadth(outputDir)
	sc.Steinberger.Vision = scoreVision(outputDir)
	sc.Steinberger.Workflows = scoreWorkflows(outputDir)
	sc.Steinberger.Insight = scoreInsight(outputDir)
	sc.Steinberger.AgentWorkflow = scoreAgentWorkflow(outputDir)
}

func recordOptionalScore(sc *Scorecard, target *int, dimension string, score int, scored bool) {
	if scored {
		*target = score
		return
	}
	sc.UnscoredDimensions = append(sc.UnscoredDimensions, dimension)
}

func scoreSpecDimensions(sc *Scorecard, outputDir, specPath string) error {
	if specPath == "" {
		// No spec: mark spec-dependent dimensions as unscored.
		sc.UnscoredDimensions = append(sc.UnscoredDimensions, DimPathValidity, DimAuthProtocol)
		return nil
	}

	spec, err := loadOpenAPISpec(specPath)
	if err != nil {
		return err
	}

	if spec.IsSynthetic() {
		// Hand-built commands intentionally go beyond the spec; path-validity
		// is not applicable. Mark unscored so the tier-2 denominator excludes
		// it rather than awarding a 10-point cushion the CLI didn't earn.
		sc.UnscoredDimensions = append(sc.UnscoredDimensions, DimPathValidity)
	} else {
		pathValidity := evaluatePathValidity(outputDir, spec)
		sc.Steinberger.PathValidity = pathValidity.score
		if !pathValidity.scored {
			sc.UnscoredDimensions = append(sc.UnscoredDimensions, DimPathValidity)
		}
	}

	authProtocol := evaluateAuthProtocol(outputDir, spec)
	sc.Steinberger.AuthProtocol = authProtocol.score
	if !authProtocol.scored {
		sc.UnscoredDimensions = append(sc.UnscoredDimensions, DimAuthProtocol)
	}
	return nil
}

func scoreDomainDimensions(sc *Scorecard, outputDir string, verifyReport *VerifyReport) {
	sc.Steinberger.DataPipelineIntegrity = scoreDataPipelineIntegrity(outputDir)
	sc.Steinberger.SyncCorrectness = scoreSyncCorrectness(outputDir)
	sc.Steinberger.TypeFidelity = scoreTypeFidelity(outputDir)
	sc.Steinberger.DeadCode = scoreDeadCode(outputDir)

	// LiveAPIVerification is scored only when verify ran in live mode (real
	// API, not a mock server and not structural-only). Mock-backed verify
	// passes and live verify passes look identical in dogfood output; making
	// this a distinct dimension in the scorecard lets reviewers tell them
	// apart at a glance and gives selfimprove a targeted fix plan when a
	// shipped CLI has never been exercised against the real API.
	if liveScore, scored := scoreLiveAPIVerification(verifyReport); scored {
		sc.Steinberger.LiveAPIVerification = liveScore
	} else {
		sc.UnscoredDimensions = append(sc.UnscoredDimensions, DimLiveAPIVerification)
	}
}

func finalizeScorecard(sc *Scorecard, outputDir, pipelineDir string, verifyReport *VerifyReport) {
	browserSessionUnverified := verifyReport != nil && verifyReport.BrowserSessionRequired && verifyReport.BrowserSessionProof != "valid"
	sc.browserSessionUnverified = browserSessionUnverified
	if browserSessionUnverified {
		if sc.Steinberger.Auth > 5 {
			sc.Steinberger.Auth = 5
		}
		if sc.Steinberger.AuthProtocol > 5 {
			sc.Steinberger.AuthProtocol = 5
		}
	}

	// Apply verify caps to dimensions BEFORE tier calculation so Total stays consistent
	if verifyReport != nil {
		if !verifyReport.DataPipeline && sc.Steinberger.DataPipelineIntegrity > 5 {
			sc.Steinberger.DataPipelineIntegrity = 5
		}
	}

	if verifyReport != nil {
		verifyScore := int(verifyReport.PassRate)
		sc.verifyCalibrationFloor = (verifyScore * 80) / 100 // 91% verify -> 72 floor
	}
	recomputeScorecardTotals(sc)
	applyScorecardCalibration(sc)

	// Grade
	sc.OverallGrade = computeGrade(sc.Steinberger.Percentage)

	// Gap report for dimensions below 5
	sc.GapReport = buildGapReport(sc.Steinberger, sc.UnscoredDimensions)

	// MCP tool split from manifest (informational, does not affect score)
	if manifest, err := loadCLIManifestForScorecard(outputDir); err == nil && manifest.MCPBinary != "" {
		authCount := manifest.MCPToolCount - manifest.MCPPublicToolCount
		sc.GapReport = append(sc.GapReport,
			fmt.Sprintf("MCP: %d tools (%d public, %d auth-required) — readiness: %s",
				manifest.MCPToolCount, manifest.MCPPublicToolCount, authCount, manifest.MCPReady))
	}

	// Competitor comparison from research.json
	sc.CompetitorScores = buildCompetitorScores(sc.Steinberger.Total, pipelineDir)
}

func writeScorecardArtifacts(sc *Scorecard, pipelineDir string) error {
	if err := writeScorecardMD(sc, pipelineDir); err != nil {
		return fmt.Errorf("writing scorecard.md: %w", err)
	}
	if err := writeScorecardJSON(sc, pipelineDir); err != nil {
		return fmt.Errorf("writing scorecard.json: %w", err)
	}
	return nil
}

func (sc *Scorecard) IsDimensionUnscored(name string) bool {
	return slices.Contains(sc.UnscoredDimensions, name)
}

func scoreOutputModes(dir string) int {
	rootContent := readFileContent(filepath.Join(dir, "internal", "cli", "root.go"))
	helpersContent := readFileContent(filepath.Join(dir, "internal", "cli", "helpers.go"))
	score := 0
	// Presence tier (max 5)
	if strings.Contains(rootContent, `"json"`) {
		score += 1
	}
	if strings.Contains(rootContent, `"plain"`) {
		score += 1
	}
	if strings.Contains(rootContent, `"select"`) {
		score += 1
	}
	if strings.Contains(rootContent, `"csv"`) {
		score += 1
	}
	if strings.Contains(rootContent, `"quiet"`) {
		score += 1
	}
	// Quality tier: field-aware select (real JSON parsing, not string ops)
	if strings.Contains(helpersContent, "filterFields") && strings.Contains(helpersContent, "json.Unmarshal") {
		score += 2
	}
	// Quality tier: pagination progress events
	if strings.Contains(helpersContent, "page_fetch") || strings.Contains(helpersContent, "ndjson") {
		score += 1
	}
	// Quality tier: tabwriter for aligned output
	if strings.Contains(helpersContent, "tabwriter") {
		score += 2
	}
	if score > 10 {
		score = 10
	}
	return score
}

func scoreAuth(dir string) int {
	configContent := readFileContent(filepath.Join(dir, "internal", "config", "config.go"))
	authContent := readFileContent(filepath.Join(dir, "internal", "cli", "auth.go"))
	clientContent := readFileContent(filepath.Join(dir, "internal", "client", "client.go"))

	// No-auth exemption: when the generator decided this spec requires no
	// auth surface, internal/cli/auth.go is not emitted. (Specs declaring
	// auth.type: none with no AuthorizationURL and no graphql_persisted_query
	// hint take this path; see Generator.shouldEmitAuth.) Penalizing such
	// CLIs for "missing auth subcommand" or "no env vars" is wrong --
	// they correctly reflect a no-auth spec. Award full credit and let the
	// auth-bearing dimensions on auth-bearing CLIs speak for themselves.
	//
	// Note: readFileContent returns "" for both "file does not exist" and
	// "file exists but is empty." We rely on the generator never emitting an
	// empty auth.go -- every auth template (auth.go.tmpl, auth_simple.go.tmpl,
	// auth_browser.go.tmpl) produces a non-empty file with at least the cobra
	// command stub. If a future code path emits an empty auth.go as a
	// placeholder, this exemption would fire incorrectly; the safer signal
	// is os.Stat for file existence, but the empty-content check matches
	// today's behavior with one fewer syscall.
	if authContent == "" {
		return 10
	}

	score := 0
	// Presence: at least one env var
	if strings.Count(configContent, "os.Getenv") >= 1 {
		score += 2
	}
	// Presence: auth file exists (we already returned above when it doesn't)
	score += 1
	// Quality: secure config file permissions (0o600 or 0600)
	if strings.Contains(configContent, "0o600") || strings.Contains(configContent, "0600") || strings.Contains(configContent, "0o700") || strings.Contains(configContent, "0700") {
		score += 2
	}
	// Quality: token masking in output (showing partial token)
	if strings.Contains(clientContent, "mask") || strings.Contains(clientContent, "***") || strings.Contains(clientContent, "last 4") || (strings.Contains(clientContent, "Authorization") && strings.Contains(clientContent, "[:")) {
		score += 2
	}
	// Quality: multiple auth methods (env var + config + flag)
	authSources := 0
	if strings.Contains(configContent, "os.Getenv") {
		authSources++
	}
	if strings.Contains(configContent, "ReadFile") || strings.Contains(configContent, "Load") {
		authSources++
	}
	if authSources >= 2 {
		score += 1
	}
	// TODO: Replace this free grant with real OAuth2 scoring when the generator
	// can produce OAuth2 browser flows from spec authorizationCode grants.
	// Auto-award 2 points so the ceiling is 10/10 for what's currently possible.
	score += 2
	if score > 10 {
		score = 10
	}
	return score
}

func scoreErrorHandling(dir string) int {
	helpersContent := readFileContent(filepath.Join(dir, "internal", "cli", "helpers.go"))
	clientContent := readFileContent(filepath.Join(dir, "internal", "client", "client.go"))
	score := 0
	// Presence: error hints
	if strings.Contains(helpersContent, "hint:") || strings.Contains(helpersContent, "Hint:") {
		score += 1
	}
	// Presence: at least 3 distinct exit codes
	exitCount := strings.Count(helpersContent, "code:")
	if exitCount >= 3 {
		score += 2
	} else if exitCount >= 1 {
		score += 1
	}
	// Quality: rate limit handling (429 + retry)
	if strings.Contains(clientContent, "429") && (strings.Contains(clientContent, "Retry-After") || strings.Contains(clientContent, "backoff") || strings.Contains(clientContent, "retry")) {
		score += 2
	}
	// Quality: idempotency (409 = already exists = success)
	if strings.Contains(helpersContent, "409") && strings.Contains(helpersContent, "already exists") {
		score += 2
	}
	// Quality: 404 with specific exit code
	if strings.Contains(helpersContent, "404") {
		score += 1
	}
	// Excellence: actionable suggestions in errors (not just codes)
	if (strings.Contains(helpersContent, "Run") || strings.Contains(helpersContent, "try")) && strings.Contains(helpersContent, "doctor") {
		score += 2
	}
	if score > 10 {
		score = 10
	}
	return score
}

func scoreTerminalUX(dir string) int {
	helpersContent := readFileContent(filepath.Join(dir, "internal", "cli", "helpers.go"))
	rootContent := readFileContent(filepath.Join(dir, "internal", "cli", "root.go"))
	score := 0
	// Presence: NO_COLOR support
	if strings.Contains(helpersContent, "NO_COLOR") {
		score += 1
	}
	// Presence: TTY detection
	if strings.Contains(helpersContent, "isatty") {
		score += 1
	}
	// Presence: no-color flag
	if strings.Contains(rootContent, "no-color") {
		score += 1
	}
	// Quality: tabwriter for aligned columns
	if strings.Contains(helpersContent, "tabwriter") {
		score += 2
	}
	// Quality: help text descriptions are meaningful (not just verb names)
	cmdFiles := sampleCommandFiles(dir, 5)
	goodDescs := 0
	for _, content := range cmdFiles {
		if hasQualityDescription(content) {
			goodDescs++
		}
	}
	if goodDescs >= 4 {
		score += 2
	} else if goodDescs >= 2 {
		score += 1
	}
	// Quality: example values are realistic (not abc123 or bare "value")
	goodExamples := 0
	for _, content := range cmdFiles {
		if !hasPlaceholderValues(content) {
			goodExamples++
		}
	}
	if goodExamples >= 4 {
		score += 3
	} else if goodExamples >= 2 {
		score += 1
	}
	if score > 10 {
		score = 10
	}
	return score
}

func scoreREADME(dir string) int {
	content := readFileContent(filepath.Join(dir, "README.md"))
	score := 0
	// Presence: key sections exist (1pt each, max 4)
	// Each entry can have aliases (e.g., "Doctor" and "Health Check" mean the same thing)
	for _, aliases := range [][]string{{"Quick Start"}, {"Agent Usage"}, {"Doctor", "Health Check"}, {"Troubleshooting"}} {
		for _, section := range aliases {
			if strings.Contains(content, section) {
				score++
				break
			}
		}
	}
	// Quality: Quick Start has no obvious placeholder/template values.
	// "your-key-here" in an export line is a legitimate auth setup example,
	// not a sign of unfinished boilerplate. Only penalize generic resource
	// placeholders like "abc123" or unresolved template markers like "USER/tap".
	qsIdx := strings.Index(content, "Quick Start")
	if qsIdx >= 0 {
		qsSection := content[qsIdx:min(qsIdx+500, len(content))]
		if !strings.Contains(qsSection, "USER/tap") && !strings.Contains(qsSection, "abc123") {
			score += 2
		}
	}
	// Quality: has Cookbook or Recipes with 3+ code blocks
	if strings.Contains(content, "Cookbook") || strings.Contains(content, "Recipes") {
		codeBlocks := strings.Count(content, "```")
		if codeBlocks >= 6 { // 3+ examples = 6+ backtick pairs
			score += 2
		} else {
			score += 1
		}
	}
	// Quality: README describes the API in human terms (not raw spec text)
	lines := strings.SplitN(content, "\n", 5)
	if len(lines) >= 3 {
		header := strings.Join(lines[:3], " ")
		if !strings.Contains(header, "Preview of") && !strings.Contains(header, "specification") && len(header) > 20 {
			score += 2
		}
	}
	if score > 10 {
		score = 10
	}
	return score
}

func scoreDoctor(dir string) int {
	content := readFileContent(filepath.Join(dir, "internal", "cli", "doctor.go"))
	if content == "" {
		return 0
	}
	score := 0
	// Presence: doctor command exists
	score += 2
	// Quality: checks auth/token validity
	if strings.Contains(content, "auth") || strings.Contains(content, "token") || strings.Contains(content, "Token") {
		score += 2
	}
	// Quality: checks API connectivity (makes an HTTP request)
	if hasDoctorHTTPReachability(content) {
		score += 2
	}
	// Quality: checks config file
	if strings.Contains(content, "config") || strings.Contains(content, "Config") {
		score += 2
	}
	// Excellence: checks version or API compatibility
	if strings.Contains(content, "version") || strings.Contains(content, "Version") {
		score += 2
	}
	if score > 10 {
		score = 10
	}
	return score
}

func hasDoctorHTTPReachability(content string) bool {
	if strings.Contains(content, "http.Get") ||
		strings.Contains(content, "http.Head") ||
		strings.Contains(content, "http.Post") ||
		strings.Contains(content, "http.NewRequest") {
		return true
	}
	clientCallRe := regexp.MustCompile(`\b[A-Za-z_]\w*(?:Client|HTTPClient)?\.(?:Get|Head|Post|Put|Patch|Delete|Do)\s*\(`)
	inlineClientCallRe := regexp.MustCompile(`\(&http\.Client\s*\{[^}]*\}\)\.(?:Get|Head|Post|Put|Patch|Delete|Do)\s*\(`)
	return clientCallRe.MatchString(content) || inlineClientCallRe.MatchString(content)
}

func scoreAgentNative(dir string) int {
	rootContent := readFileContent(filepath.Join(dir, "internal", "cli", "root.go"))
	helpersContent := readFileContent(filepath.Join(dir, "internal", "cli", "helpers.go"))
	score := 0
	// Presence: core agent flags (1pt each, max 5)
	if strings.Contains(rootContent, `"json"`) {
		score++
	}
	if strings.Contains(rootContent, `"select"`) {
		score++
	}
	if strings.Contains(rootContent, "dry-run") {
		score++
	}
	if strings.Contains(rootContent, "stdin") {
		score++
	}
	if strings.Contains(rootContent, `"yes"`) {
		score++
	}
	// Quality: non-interactive (no prompts in command files)
	cmdFiles := sampleCommandFiles(dir, 5)
	hasPrompts := false
	for _, content := range cmdFiles {
		if strings.Contains(content, "bufio.NewScanner(os.Stdin)") || strings.Contains(content, "Prompt") || strings.Contains(content, "ReadLine") {
			hasPrompts = true
			break
		}
	}
	if !hasPrompts && len(cmdFiles) > 0 {
		score++
	}
	// Quality: typed exit codes (5+ distinct)
	exitCount := strings.Count(helpersContent, "code:")
	if exitCount >= 5 {
		score += 2
	} else if exitCount >= 3 {
		score++
	}
	// Excellence: --stdin examples in command files (at least 3 commands show stdin usage)
	stdinExamples := 0
	for _, content := range cmdFiles {
		if strings.Contains(content, "--stdin") && strings.Contains(content, "Example") {
			stdinExamples++
		}
	}
	// Also check all command files for stdin examples, not just sample
	allCmdFiles := sampleCommandFiles(dir, 0) // 0 = all
	for _, content := range allCmdFiles {
		if strings.Contains(content, "--stdin") && strings.Contains(content, "echo") {
			stdinExamples++
		}
	}
	if stdinExamples >= 3 {
		score++
	}
	// Token efficiency: --agent meta-flag
	if strings.Contains(rootContent, `"agent"`) && strings.Contains(rootContent, "PersistentPreRun") {
		score++
	}
	// Token efficiency: --compact strips verbose fields on single objects (blocklist approach)
	if strings.Contains(helpersContent, "compactObjectFields") || strings.Contains(helpersContent, "stripVerboseFields") {
		score++
	}
	// Token efficiency: analytics commands have --limit flag
	staleContent := readFileContent(filepath.Join(dir, "internal", "cli", "pm_stale.go"))
	loadContent := readFileContent(filepath.Join(dir, "internal", "cli", "pm_load.go"))
	if (strings.Contains(staleContent, `"limit"`) || staleContent == "") && (strings.Contains(loadContent, `"limit"`) || loadContent == "") {
		score++
	}
	// Token efficiency: store has ResolveByName for name-or-ID resolution
	storeContent := readFileContent(filepath.Join(dir, "internal", "store", "store.go"))
	if strings.Contains(storeContent, "ResolveByName") || strings.Contains(storeContent, "IsUUID") {
		score++
	}
	if score > 10 {
		score = 10
	}
	return score
}

func scoreMCPQuality(dir string) int {
	mcpContent := readFileContent(filepath.Join(dir, "internal", "mcp", "tools.go"))
	if mcpContent == "" {
		return 0 // No MCP server generated
	}

	score := 0

	// Presence: MCP tools.go exists and has RegisterTools
	if strings.Contains(mcpContent, "RegisterTools") {
		score += 2
	}

	// Context tool: has rich context/about tool with domain knowledge
	if strings.Contains(mcpContent, `"context"`) || strings.Contains(mcpContent, "handleContext") {
		score += 2
	}

	// High-level tools: sql, search, sync exposed to MCP (not just CLI)
	highlevelCount := 0
	hasRuntimeMirror := strings.Contains(mcpContent, "cobratree.RegisterAll")
	if strings.Contains(mcpContent, `"sql"`) && strings.Contains(mcpContent, "handleSQL") {
		highlevelCount++
	}
	if strings.Contains(mcpContent, `"search"`) && strings.Contains(mcpContent, "handleSearch") {
		highlevelCount++
	}
	if (strings.Contains(mcpContent, `"sync"`) && strings.Contains(mcpContent, "handleSync")) ||
		(hasRuntimeMirror && fileExists(filepath.Join(dir, "internal", "cli", "sync.go"))) {
		highlevelCount++
	}
	if highlevelCount >= 2 {
		score += 2
	} else if highlevelCount >= 1 {
		score++
	}

	// Description quality: response shape hints (Returns array, Returns object)
	returnHints := strings.Count(mcpContent, "Returns array") + strings.Count(mcpContent, "Returns ")
	if returnHints >= 3 {
		score += 2
	} else if returnHints >= 1 {
		score++
	}

	// No empty descriptions
	emptyDescs := strings.Count(mcpContent, `Description("")`)
	if emptyDescs == 0 {
		score++
	}

	// Description richness: tool descriptions reference other tools or provide usage guidance
	if strings.Contains(mcpContent, "Requires sync") || strings.Contains(mcpContent, "Call this first") {
		score++
	}

	if score > 10 {
		score = 10
	}
	return score
}

// MCPDescMinLen and MCPDescMinWords are the agent-grade-description
// thresholds used by both `printing-press tools-audit` and the
// scorecard's mcp_description_quality dimension. Defined here so a
// single edit keeps both surfaces in lockstep — internal/cli imports
// these for its thin-mcp-description check.
const (
	MCPDescMinLen   = 60
	MCPDescMinWords = 8
)

// IsThinMCPDescription reports whether a description trips the
// agent-grade floor: empty or both shorter than MCPDescMinLen AND
// fewer words than MCPDescMinWords. Shared between the audit (cli
// package) and the scorer so both apply identical semantics.
func IsThinMCPDescription(desc string) bool {
	d := strings.TrimSpace(desc)
	if d == "" {
		return true
	}
	return len(d) < MCPDescMinLen && len(strings.Fields(d)) < MCPDescMinWords
}

// ScoreMCPDescriptionQualityForManifest scores an already-parsed
// manifest. Callers that read tools-manifest.json for other reasons
// in the same code path (e.g., printing-press tools-audit, which
// emits findings from the manifest) call this variant to avoid
// re-parsing.
func ScoreMCPDescriptionQualityForManifest(m *ToolsManifest) (score int, scored bool) {
	if m == nil || len(m.Tools) == 0 {
		return 0, false
	}
	thin := 0
	for _, t := range m.Tools {
		if IsThinMCPDescription(t.Description) {
			thin++
		}
	}
	pct := float64(thin) / float64(len(m.Tools))
	switch {
	case pct == 0:
		return 10, true
	case pct < 0.05:
		return 9, true
	case pct < 0.15:
		return 7, true
	case pct < 0.30:
		return 5, true
	case pct < 0.50:
		return 3, true
	default:
		return 0, true
	}
}

// scoreMCPDescriptionQuality measures how many of a CLI's typed MCP
// tools carry agent-grade descriptions vs. terse spec-derived
// summaries. Reads tools-manifest.json (the source of truth for typed
// endpoint tools' descriptions at runtime) and counts entries whose
// description trips IsThinMCPDescription — the same predicate
// `printing-press tools-audit` uses for thin-mcp-description findings.
//
// Unscored when no manifest exists (legacy CLIs predating the
// manifest schema) or when the manifest has no tools.
//
// Score curve favors low thin-percentage steeply; the goal is to
// reward CLIs that have done the work to provide rich descriptions
// rather than to give partial credit to ones that haven't. CLIs whose
// descriptions are 50%+ thin score 0 — there's no signal to credit
// when most of the surface is unusable to an agent.
func scoreMCPDescriptionQuality(dir string) (score int, scored bool) {
	m, err := ReadToolsManifest(dir)
	if err != nil {
		return 0, false
	}
	return ScoreMCPDescriptionQualityForManifest(m)
}

func scoreLocalCache(dir string) int {
	clientContent := readFileContent(filepath.Join(dir, "internal", "client", "client.go"))
	score := 0
	// Presence: GET response caching
	if strings.Contains(clientContent, "readCache") || strings.Contains(clientContent, "writeCache") || strings.Contains(clientContent, "cacheDir") {
		score += 2
	}
	// Presence: --no-cache bypass
	if strings.Contains(clientContent, "no-cache") || strings.Contains(clientContent, "NoCache") {
		score += 1
	}
	// Quality: cache has TTL (time-based expiry)
	if strings.Contains(clientContent, "time.Duration") || strings.Contains(clientContent, "ModTime") || strings.Contains(clientContent, "TTL") || strings.Contains(clientContent, "ttl") {
		score += 2
	}
	// Quality: XDG or standard cache directory
	if strings.Contains(clientContent, ".cache") || strings.Contains(clientContent, "XDG_CACHE_HOME") || strings.Contains(clientContent, "UserCacheDir") {
		score += 2
	}
	// Excellence: SQLite or embedded DB
	for _, name := range []string{"internal/cache/cache.go", "internal/store/store.go"} {
		content := readFileContent(filepath.Join(dir, name))
		if strings.Contains(content, "sqlite") || strings.Contains(content, "bolt") || strings.Contains(content, "badger") {
			score += 3
			break
		}
	}
	if score > 10 {
		score = 10
	}
	return score
}

// scoreCacheFreshness rewards the discrawl-inspired Phase 1-3 capabilities.
// Returns (score, true) when the CLI has a local store; (0, false) otherwise
// so the dimension is marked N/A and excluded from the tier1 denominator.
// Each sub-capability is worth 2-5 points; the total is capped at 10.
//
// The signals are string-matched against well-known template artifacts
// rather than imported symbols because the scorer must not depend on
// the generated package paths — every printed CLI has a distinct module
// path.
func scoreCacheFreshness(dir string) (int, bool) {
	storePath := filepath.Join(dir, "internal", "store", "store.go")
	storeContent := readFileContent(storePath)
	if storeContent == "" {
		return 0, false
	}
	score := 0

	// (a) Schema-version gate: the StoreSchemaVersion constant + PRAGMA
	// user_version read/write fail-fast against schema drift. Worth 3 pts
	// because without it, binary upgrades silently corrupt reads.
	if strings.Contains(storeContent, "StoreSchemaVersion") && strings.Contains(storeContent, "user_version") {
		score += 3
	}

	// (b) Doctor cache section: collectCacheReport + renderCacheReport
	// emit the agent-consumable freshness surface.
	doctorPath := filepath.Join(dir, "internal", "cli", "doctor.go")
	doctorContent := readFileContent(doctorPath)
	if strings.Contains(doctorContent, "collectCacheReport") {
		score += 2
	}

	// (c) Auto-refresh wired: the PersistentPreRunE hook invokes
	// autoRefreshIfStale, and the cliutil freshness helper exists. Worth
	// 5 pts because this is the user's headline ask — a CLI that ships
	// schema gating + doctor cache + auto-refresh has perfect freshness
	// behavior and should top the dimension out at 10/10.
	autoRefreshPath := filepath.Join(dir, "internal", "cli", "auto_refresh.go")
	autoRefreshContent := readFileContent(autoRefreshPath)
	freshnessPath := filepath.Join(dir, "internal", "cliutil", "freshness.go")
	freshnessContent := readFileContent(freshnessPath)
	if strings.Contains(autoRefreshContent, "autoRefreshIfStale") && strings.Contains(freshnessContent, "EnsureFresh") {
		score += 5
	}

	if score > 10 {
		score = 10
	}
	return score, true
}

// scoreLiveAPIVerification returns a 0-10 score reflecting whether verify
// ran against the real API and how many of its checks passed. It returns
// (0, false) in every case where the signal is absent or untrustworthy:
// no verify report threaded in, verify ran against a mock, or verify ran
// in structural-only mode. A scored result always means the CLI has at
// least been exercised against its real backend.
//
// PassRate is already 0-100 (e.g., 91.0 for 91%), matching the existing
// calibration path in RunScorecard. The 95% cap at 10 mirrors the
// established convention that near-perfect is perfect for grading.
func scoreLiveAPIVerification(verifyReport *VerifyReport) (int, bool) {
	if verifyReport == nil {
		return 0, false
	}
	if verifyReport.Mode != "live" {
		return 0, false
	}
	if verifyReport.PassRate >= 95 {
		return 10, true
	}
	// Linear scale below the cap: 0% → 0, 10% → 1, ..., 94% → 9.
	score := min(max(int(verifyReport.PassRate/10), 0), 10)
	return score, true
}

func scoreLiveAPIVerificationFromLiveCheck(live *LiveCheckResult) (int, bool) {
	if live == nil || live.Unable || live.Checked() == 0 {
		return 0, false
	}
	switch {
	case live.Passed >= 3:
		return 10, true
	case live.Passed >= 1:
		return 5, true
	default:
		return 0, true
	}
}

func ApplyLiveCheckToScorecard(sc *Scorecard, live *LiveCheckResult) {
	if sc == nil {
		return
	}
	score, scored := scoreLiveAPIVerificationFromLiveCheck(live)
	if !scored {
		return
	}
	sc.Steinberger.LiveAPIVerification = score
	sc.UnscoredDimensions = removeUnscoredDimension(sc.UnscoredDimensions, DimLiveAPIVerification)
	recomputeScorecardTotals(sc)
	applyScorecardCalibration(sc)
	sc.OverallGrade = computeGrade(sc.Steinberger.Percentage)
	sc.GapReport = buildGapReport(sc.Steinberger, sc.UnscoredDimensions)
}

func applyScorecardCalibration(sc *Scorecard) {
	if sc == nil {
		return
	}
	var notes []string
	if sc.verifyCalibrationFloor > 0 && sc.Steinberger.Total < sc.verifyCalibrationFloor {
		originalTotal := sc.Steinberger.Total
		sc.Steinberger.Total = sc.verifyCalibrationFloor
		notes = append(notes, fmt.Sprintf(
			"Score raised from %d to %d based on verify pass rate",
			originalTotal, sc.verifyCalibrationFloor))
	}
	if sc.browserSessionUnverified && sc.Steinberger.Total > 69 {
		sc.Steinberger.Total = 69
		notes = append(notes, "Score capped because required browser-session auth was not verified")
	}
	sc.Steinberger.Percentage = sc.Steinberger.Total
	sc.Steinberger.CalibrationNote = strings.Join(notes, "; ")
}

func removeUnscoredDimension(dimensions []string, name string) []string {
	out := dimensions[:0]
	for _, dimension := range dimensions {
		if dimension != name {
			out = append(out, dimension)
		}
	}
	return out
}

func recomputeScorecardTotals(sc *Scorecard) {
	tier1Raw := sumScorecardDimensions(
		sc.Steinberger.OutputModes,
		sc.Steinberger.Auth,
		sc.Steinberger.ErrorHandling,
		sc.Steinberger.TerminalUX,
		sc.Steinberger.README,
		sc.Steinberger.Doctor,
		sc.Steinberger.AgentNative,
		sc.Steinberger.MCPQuality,
		sc.Steinberger.MCPDescriptionQuality,
		sc.Steinberger.MCPTokenEff,
		sc.Steinberger.MCPRemoteTransport,
		sc.Steinberger.MCPToolDesign,
		sc.Steinberger.MCPSurfaceStrategy,
		sc.Steinberger.LocalCache,
		sc.Steinberger.CacheFreshness,
		sc.Steinberger.Breadth,
		sc.Steinberger.Vision,
		sc.Steinberger.Workflows,
		sc.Steinberger.Insight,
		sc.Steinberger.AgentWorkflow,
	)

	tier1Max := scorecardTierMax(sc, 200, DimMCPDescriptionQuality, DimMCPTokenEfficiency, DimCacheFreshness, DimMCPRemoteTransport, DimMCPToolDesign, DimMCPSurfaceStrategy)
	tier1Normalized := 0
	if tier1Max > 0 {
		tier1Normalized = (tier1Raw * 50) / tier1Max
	}

	tier2Raw := sumScorecardDimensions(
		sc.Steinberger.PathValidity,
		sc.Steinberger.AuthProtocol,
		sc.Steinberger.DataPipelineIntegrity,
		sc.Steinberger.SyncCorrectness,
		sc.Steinberger.TypeFidelity,
		sc.Steinberger.DeadCode,
		sc.Steinberger.LiveAPIVerification,
	)

	tier2Max := scorecardTierMax(sc, 60, DimLiveAPIVerification, DimPathValidity, DimAuthProtocol)
	tier2Normalized := 0
	if tier2Max > 0 {
		tier2Normalized = (tier2Raw * 50) / tier2Max
	}
	sc.Steinberger.Total = tier1Normalized + tier2Normalized
	sc.Steinberger.Percentage = sc.Steinberger.Total
}

func sumScorecardDimensions(scores ...int) (total int) {
	for _, score := range scores {
		total += score
	}
	return total
}

func scorecardTierMax(sc *Scorecard, base int, optionalDimensions ...string) int {
	max := base
	for _, name := range optionalDimensions {
		if sc.IsDimensionUnscored(name) {
			max -= 10
		}
	}
	return max
}

func scoreBreadth(dir string) int {
	cliDir := filepath.Join(dir, "internal", "cli")
	entries, err := os.ReadDir(cliDir)
	if err != nil {
		return 0
	}
	commandFiles := 0
	lazyDescs := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		if infraAllFiles[e.Name()] {
			continue
		}
		commandFiles++
		// Check for lazy 1-word descriptions
		content := readFileContent(filepath.Join(cliDir, e.Name()))
		if hasLazyDescription(content) {
			lazyDescs++
		}
	}

	var score int
	switch {
	case commandFiles >= 60:
		score = 8
	case commandFiles >= 41:
		score = 7
	case commandFiles >= 21:
		score = 5
	case commandFiles >= 11:
		score = 4
	case commandFiles >= 5:
		score = 2
	default:
		return 0
	}
	// Penalty: if more than 50% of commands have lazy 1-word descriptions
	if commandFiles > 0 && lazyDescs*2 > commandFiles {
		score -= 2
	}
	// Bonus: if descriptions are mostly quality (< 20% lazy)
	if commandFiles > 0 && lazyDescs*5 < commandFiles {
		score += 2
	}
	if score > 10 {
		score = 10
	}
	if score < 0 {
		score = 0
	}
	return score
}

func scoreVision(dir string) int {
	cliDir := filepath.Join(dir, "internal", "cli")

	// Tier 1: Feature Presence (0-5 points)
	tier1 := 0.0
	if fileExists(filepath.Join(cliDir, "export.go")) {
		tier1 += 1.0
	}
	if fileExists(filepath.Join(dir, "internal", "store", "store.go")) {
		tier1 += 1.0
	}
	if fileExists(filepath.Join(cliDir, "search.go")) {
		tier1 += 1.0
	}
	if fileExists(filepath.Join(cliDir, "sync.go")) {
		tier1 += 0.5
	}
	if fileExists(filepath.Join(cliDir, "tail.go")) {
		tier1 += 0.5
	}
	if fileExists(filepath.Join(cliDir, "import.go")) {
		tier1 += 0.5
	}
	// Workflow or compound command files
	entries, err := os.ReadDir(cliDir)
	if err == nil {
		for _, e := range entries {
			name := e.Name()
			if strings.Contains(name, "_workflow") || strings.Contains(name, "_compound") {
				if strings.HasSuffix(name, ".go") {
					tier1 += 0.5
					break
				}
			}
		}
	}
	if tier1 > 5 {
		tier1 = 5
	}

	// Tier 2: Feature Intelligence (0-5 points)
	tier2 := 0.0

	// Schema depth (0-1.5): check if store.go has domain-specific tables
	storePath := filepath.Join(dir, "internal", "store", "store.go")
	if fileExists(storePath) {
		storeContent := readFileContent(storePath)
		tableCount := strings.Count(storeContent, "CREATE TABLE")
		syncStateCount := strings.Count(storeContent, "sync_state")
		domainTables := tableCount
		if syncStateCount > 0 {
			domainTables-- // Don't count sync_state as a domain table
		}
		if domainTables >= 3 {
			tier2 += 1.5
		} else if domainTables >= 2 {
			tier2 += 1.0
		} else if domainTables >= 1 {
			tier2 += 0.5
		}
	}

	// Wiring check (0-1.5): are vision commands registered in root.go?
	rootPath := filepath.Join(cliDir, "root.go")
	if fileExists(rootPath) {
		rootContent := readFileContent(rootPath)
		visionFuncs := []string{"newSyncCmd", "newSearchCmd", "newExportCmd", "newTailCmd", "newImportCmd", "newAnalyticsCmd"}
		wired := 0
		for _, fn := range visionFuncs {
			if strings.Contains(rootContent, fn) {
				wired++
			}
		}
		tier2 += float64(wired) * 0.25
		if tier2 > 3.0 { // cap wiring contribution
			tier2 = 3.0
		}
	}

	// FTS5 check (0-1.0): does the store have full-text search?
	if fileExists(storePath) {
		storeContent := readFileContent(storePath)
		if strings.Contains(storeContent, "fts5") || strings.Contains(storeContent, "FTS5") {
			tier2 += 1.0
		}
	}

	// Search uses store (0-0.5): does search.go reference the store package?
	searchPath := filepath.Join(cliDir, "search.go")
	if fileExists(searchPath) {
		searchContent := readFileContent(searchPath)
		if strings.Contains(searchContent, "store.") || strings.Contains(searchContent, "/store") {
			tier2 += 0.5
		}
	}

	if tier2 > 5 {
		tier2 = 5
	}

	score := min(int(tier1+tier2), 10)
	return score
}

// cobraUseLeafRe extracts the first whitespace-delimited token from a Cobra
// `Use: "..."` literal — the leaf command name (e.g., `"trajectory <slug>"`
// → `trajectory`).
var cobraUseLeafRe = regexp.MustCompile(`Use:\s*"([^"\s]+)`)

// manifestNovelFeatureLeaves returns the leaves of every novel_features[].command
// in dir/.printing-press.json. Returns nil when the manifest is missing,
// unparseable, or carries no novel features.
func manifestNovelFeatureLeaves(dir string) map[string]bool {
	data, err := os.ReadFile(filepath.Join(dir, CLIManifestFilename))
	if err != nil {
		return nil
	}
	var m CLIManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	if len(m.NovelFeatures) == 0 {
		return nil
	}
	leaves := make(map[string]bool, len(m.NovelFeatures))
	for _, nf := range m.NovelFeatures {
		_, leaf := splitCommandPath(commandPath(nf.Command))
		if leaf != "" {
			leaves[leaf] = true
		}
	}
	return leaves
}

// registeredCommandFiles returns the set of cli/*.go filenames whose command
// constructor is referenced by root.go. Files without a registered constructor
// should not inflate workflow/insight scores even if they match prefix or
// behavioral heuristics — they're orphans, dead code, or half-built commands
// that the user cannot actually invoke.
//
// Returns an empty map if root.go is missing or parsing yields no matches so
// callers can fall open to the prior heuristic behavior (older or partial CLI
// trees where the registration graph isn't parseable).
func registeredCommandFiles(cliDir string) map[string]bool {
	rootContent := readFileContent(filepath.Join(cliDir, "root.go"))
	if rootContent == "" {
		return map[string]bool{}
	}

	// Match every `newXxxCmd(` invocation — but not definitions. root.go may
	// contain helper function declarations (e.g. `func newRootCmd()`) that we
	// must not count as registrations. Strip `func Name(` declaration heads
	// before scanning so only call-sites contribute to the ctor set.
	funcDeclRe := regexp.MustCompile(`(?m)^func\s+\w+\s*\(`)
	scanContent := funcDeclRe.ReplaceAllString(rootContent, "")

	ctorRe := regexp.MustCompile(`\bnew([A-Z][A-Za-z0-9_]*)Cmd\s*\(`)
	matches := ctorRe.FindAllStringSubmatch(scanContent, -1)
	if len(matches) == 0 {
		return map[string]bool{}
	}
	ctors := make(map[string]bool, len(matches))
	for _, m := range matches {
		ctors["new"+m[1]+"Cmd"] = true
	}

	// Walk cli/*.go and map each file to the constructor it defines. Use a
	// regexp for the declaration site to avoid depending on go/parser for one
	// lookup (keeps the scorer dependency-free, which matters because it runs
	// against third-party generated trees).
	defRe := regexp.MustCompile(`^func\s+(new[A-Z][A-Za-z0-9_]*Cmd)\s*\(`)
	entries, err := os.ReadDir(cliDir)
	if err != nil {
		return map[string]bool{}
	}
	result := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		content := readFileContent(filepath.Join(cliDir, e.Name()))
		for line := range strings.SplitSeq(content, "\n") {
			sm := defRe.FindStringSubmatch(line)
			if sm == nil {
				continue
			}
			if ctors[sm[1]] {
				result[e.Name()] = true
				break
			}
		}
	}
	return result
}

func scoreWorkflows(dir string) int {
	cliDir := filepath.Join(dir, "internal", "cli")
	entries, err := os.ReadDir(cliDir)
	if err != nil {
		return 0
	}

	// Build the set of files whose command constructor is actually registered in
	// root.go. Files that define constructors never added to the command tree —
	// whether orphaned, dead, or pending — should not inflate the score. This
	// also prevents dead-code removal from dropping the score: a file whose
	// constructor isn't registered isn't counted in the first place.
	registeredFiles := registeredCommandFiles(cliDir)

	// Some prefixes overlap with insightPrefixes intentionally — per Steinberger,
	// analytics/insights ARE compound commands (the visionary research plan lists
	// "analytics" alongside "backup" and "moderate" as workflow examples). A command
	// like stats.go correctly scores in both dimensions.
	workflowPrefixes := []string{"stale", "orphan", "triage", "load", "overdue", "standup", "deps", "workflow",
		"agenda", "free", "conflicts", "unconfirmed", "stats", "trends", "health",
		"reconcile", "revenue", "archive", "search", "sync", "busy", "export",
		"noshow", "reassign", "clone"}

	compoundCommands := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		if infraCoreFiles[e.Name()] {
			continue
		}

		// If root.go registration is discoverable, require the file to define a
		// registered constructor. Falls open when no registrations are found at
		// all (older CLIs or partial builds) so we don't zero out scores
		// unexpectedly.
		if len(registeredFiles) > 0 && !registeredFiles[e.Name()] {
			continue
		}

		name := strings.ToLower(e.Name())

		// Detect workflow commands by filename pattern
		isWorkflowFile := false
		for _, prefix := range workflowPrefixes {
			if strings.HasPrefix(name, prefix) {
				isWorkflowFile = true
				break
			}
		}
		if isWorkflowFile {
			compoundCommands++
			continue
		}

		content := readFileContent(filepath.Join(cliDir, e.Name()))

		// A command that uses the store is a workflow command (it uses the data layer)
		if strings.Contains(content, "/store") || strings.Contains(content, "store.Open") || strings.Contains(content, "store.New") {
			compoundCommands++
			continue
		}

		// Count files that make 2+ API calls (total occurrences, not unique methods).
		// A command calling c.Get 3 times is a compound workflow even if it never uses POST.
		apiCallRe := regexp.MustCompile(`c\.(Get|Post|Put|Delete|Patch)\s*\(`)
		apiCalls := len(apiCallRe.FindAllString(content, -1))
		if strings.Contains(content, "store.") {
			apiCalls++
		}
		if apiCalls >= 2 {
			compoundCommands++
		}
	}

	switch {
	case compoundCommands >= 7:
		return 10
	case compoundCommands >= 5:
		return 8
	case compoundCommands >= 3:
		return 6
	case compoundCommands >= 2:
		return 4
	case compoundCommands >= 1:
		return 2
	default:
		return 0
	}
}

func scoreInsight(dir string) int {
	cliDir := filepath.Join(dir, "internal", "cli")
	entries, err := os.ReadDir(cliDir)
	if err != nil {
		return 0
	}

	registeredFiles := registeredCommandFiles(cliDir)

	insightPrefixes := []string{"health", "similar", "bottleneck", "trends", "patterns", "forecast",
		"stats", "conflicts", "stale", "analytics", "busiest", "velocity",
		"utilization", "coverage", "gaps", "noshow"}

	// novelLeaves credits files whose Cobra Use matches an agent-declared
	// transcendence command. The prefix list above can't enumerate every
	// reasonable insight verb across APIs.
	novelLeaves := manifestNovelFeatureLeaves(dir)

	found := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		if infraCoreFiles[e.Name()] {
			continue
		}
		if len(registeredFiles) > 0 && !registeredFiles[e.Name()] {
			continue
		}
		name := strings.ToLower(e.Name())

		// Signal 1: filename prefix match
		prefixMatch := false
		for _, prefix := range insightPrefixes {
			if strings.HasPrefix(name, prefix) {
				prefixMatch = true
				break
			}
		}
		if prefixMatch {
			found++
			continue
		}

		content := readFileContent(filepath.Join(cliDir, e.Name()))
		if content == "" {
			continue
		}

		// Signal 2: file declares a Cobra Use: matching an agent-declared novel feature.
		if len(novelLeaves) > 0 {
			if m := cobraUseLeafRe.FindStringSubmatch(content); m != nil && novelLeaves[strings.ToLower(m[1])] {
				found++
				continue
			}
		}

		// Signal 3: store + SQL aggregation
		usesStore := strings.Contains(content, "/store") || strings.Contains(content, "store.Open") || strings.Contains(content, "store.New")
		rateRe := regexp.MustCompile(`\brate\b|\bRate\b`)
		hasSQLAgg := strings.Contains(content, "COUNT(") || strings.Contains(content, "SUM(") ||
			strings.Contains(content, "GROUP BY") || strings.Contains(content, "AVG(") ||
			rateRe.MatchString(content)
		if usesStore && hasSQLAgg {
			found++
			continue
		}

		// Signal 4: behavioral — command produces derived/aggregated output.
		// Detects Go-level aggregation: sorting, percentage calculations, comparisons,
		// summary statistics. Requires multi-source input (2+ API calls or store usage)
		// to avoid counting simple pass-through commands.
		apiCallRe := regexp.MustCompile(`c\.(Get|Post|Put|Delete|Patch)\s*\(`)
		apiCallCount := len(apiCallRe.FindAllString(content, -1))
		hasMultiSource := apiCallCount >= 2 || usesStore

		hasGoAgg := strings.Contains(content, "sort.Slice") ||
			strings.Contains(content, "sort.Sort") ||
			strings.Contains(content, "* 100") ||
			strings.Contains(content, "/ total") ||
			strings.Contains(content, "/ float64(") ||
			strings.Contains(content, `fmt.Sprintf("%.`) ||
			strings.Contains(content, "percentage") ||
			strings.Contains(content, "Percentage") ||
			strings.Contains(content, "completion") ||
			strings.Contains(content, "Completion")

		if hasMultiSource && hasGoAgg {
			found++
		}
	}

	switch {
	case found >= 6:
		return 10
	case found >= 5:
		return 9
	case found >= 4:
		return 8
	case found >= 3:
		return 6
	case found >= 2:
		return 4
	case found >= 1:
		return 2
	default:
		return 0
	}
}

// scoreAgentWorkflow measures how well the generated CLI supports the
// HeyGen-derived agent-workflow pattern: async jobs with --wait, named
// profiles, routed delivery, and an in-band feedback channel. Each
// capability is worth 2-3 points; a fully-equipped CLI scores 10.
//
// Profiles, deliver, and feedback are always applicable. The async
// sub-score is only earned when the generator detected at least one
// async endpoint (jobs.go is emitted); for purely synchronous specs
// the missing async capability is not a penalty because there is
// nothing to submit-then-poll.
func scoreAgentWorkflow(dir string) int {
	cliDir := filepath.Join(dir, "internal", "cli")

	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(cliDir, name))
		return err == nil
	}

	hasJobs := exists("jobs.go")
	hasProfile := exists("profile.go")
	hasDeliver := exists("deliver.go")
	hasFeedback := exists("feedback.go")

	score := 0
	if hasProfile {
		score += 3
	}
	if hasDeliver {
		score += 3
	}
	if hasFeedback {
		score += 2
	}
	if hasJobs {
		score += 2
	} else {
		// Async is spec-driven. Reward 1 for not needing it when the
		// other three agent-workflow capabilities are present - the CLI
		// is correctly shaped for its synchronous API.
		if hasProfile && hasDeliver && hasFeedback {
			score += 1
		}
	}
	if score > 10 {
		score = 10
	}
	return score
}

type openAPISecurityScheme struct {
	Key        string
	Type       string
	Scheme     string
	In         string
	HeaderName string
}

type securityRequirementSet struct {
	Alternatives    [][]string
	AllowsAnonymous bool
}

type openAPISpecInfo struct {
	Paths                []string
	SecuritySchemes      map[string]openAPISecurityScheme
	SecurityRequirements []securityRequirementSet
	Kind                 string // see apispec.KindREST / apispec.KindSynthetic
}

func (s *openAPISpecInfo) IsSynthetic() bool {
	return s != nil && s.Kind == apispec.KindSynthetic
}

func loadOpenAPISpec(specPath string) (*openAPISpecInfo, error) {
	if specPath == "" {
		return nil, nil
	}

	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, fmt.Errorf("reading spec: %w", err)
	}

	// Detect internal YAML spec format and convert to openAPISpecInfo.
	if isInternalYAMLSpec(data) {
		internal, err := apispec.ParseBytes(data)
		if err != nil {
			return nil, fmt.Errorf("parsing internal YAML spec: %w", err)
		}
		return internalSpecToOpenAPISpecInfo(internal), nil
	}

	// Strip a UTF-8 BOM if present so editors that emit one (Windows
	// Notepad, some VS Code locales) don't fail content sniffing or
	// json.Unmarshal, which both reject leading BOM bytes.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})

	// Distinguish OpenAPI JSON from OpenAPI YAML by content sniffing the
	// first non-whitespace byte. JSON objects start with `{`; YAML mappings
	// start with letters/quotes. yaml.Unmarshal would accept either format,
	// but separate branches preserve format-specific error messages.
	var raw map[string]any
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("spec file %s is empty", specPath)
	}
	if trimmed[0] == '{' {
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("parsing spec JSON: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("parsing OpenAPI YAML spec: %w", err)
		}
	}

	info := &openAPISpecInfo{
		SecuritySchemes: make(map[string]openAPISecurityScheme),
	}
	if paths, ok := raw["paths"].(map[string]any); ok {
		for path := range paths {
			info.Paths = append(info.Paths, path)
		}
		slices.Sort(info.Paths)
	}

	if components, ok := raw["components"].(map[string]any); ok {
		if securitySchemes, ok := components["securitySchemes"].(map[string]any); ok {
			for schemeName, value := range securitySchemes {
				scheme := openAPISecurityScheme{Key: schemeName}
				if fields, ok := value.(map[string]any); ok {
					scheme.Type = strings.ToLower(asString(fields["type"]))
					scheme.Scheme = strings.ToLower(asString(fields["scheme"]))
					scheme.In = strings.ToLower(asString(fields["in"]))
					scheme.HeaderName = asString(fields["name"])
				}
				info.SecuritySchemes[schemeName] = scheme
			}
		}
	}

	// Swagger 2.0 fallback: check top-level securityDefinitions when OAS3
	// components.securitySchemes is empty or missing.
	if len(info.SecuritySchemes) == 0 {
		if securityDefs, ok := raw["securityDefinitions"].(map[string]any); ok {
			for schemeName, value := range securityDefs {
				scheme := openAPISecurityScheme{Key: schemeName}
				if fields, ok := value.(map[string]any); ok {
					swType := strings.ToLower(asString(fields["type"]))
					swIn := strings.ToLower(asString(fields["in"]))
					swName := asString(fields["name"])

					switch swType {
					case "oauth2":
						scheme.Type = "oauth2"
					case "apikey":
						scheme.Type = "apikey"
						scheme.In = swIn
						scheme.HeaderName = swName
						// Detect Bearer-style API key in header with Authorization name.
						if swIn == "header" && strings.EqualFold(swName, "Authorization") {
							scheme.Type = "http"
							scheme.Scheme = "bearer"
						}
					case "basic":
						scheme.Type = "http"
						scheme.Scheme = "basic"
					}
				}
				info.SecuritySchemes[schemeName] = scheme
			}
		}
	}

	rootSecurity, rootHasSecurity := parseSecurityRequirementSet(raw["security"])
	foundOperation := false
	if paths, ok := raw["paths"].(map[string]any); ok {
		for _, pathValue := range paths {
			pathItem, ok := pathValue.(map[string]any)
			if !ok {
				continue
			}
			for method, operationValue := range pathItem {
				if !isHTTPMethod(method) {
					continue
				}
				foundOperation = true
				operation, ok := operationValue.(map[string]any)
				if !ok {
					continue
				}
				if requirementSet, ok := parseSecurityRequirementSet(operation["security"]); ok {
					info.SecurityRequirements = append(info.SecurityRequirements, requirementSet)
					continue
				}
				if rootHasSecurity {
					info.SecurityRequirements = append(info.SecurityRequirements, rootSecurity)
				}
			}
		}
	}
	if !foundOperation && rootHasSecurity {
		info.SecurityRequirements = append(info.SecurityRequirements, rootSecurity)
	}

	for _, requirementSet := range info.SecurityRequirements {
		for _, alternative := range requirementSet.Alternatives {
			for _, name := range alternative {
				if _, ok := info.SecuritySchemes[name]; !ok {
					return nil, fmt.Errorf("spec references undefined security scheme %q", name)
				}
			}
		}
	}

	return info, nil
}

type dimensionScore struct {
	score  int
	scored bool
}

func evaluatePathValidity(dir string, spec *openAPISpecInfo) dimensionScore {
	if spec == nil {
		return dimensionScore{}
	}
	if len(spec.Paths) == 0 {
		return dimensionScore{}
	}

	pathRe := regexp.MustCompile(`\bpath\s*(?::=|=|:)\s*"([^"]+)"`)
	cmdFiles := sampleCommandFiles(dir, 0) // scan all files, not a sample — avoids bias toward early-alphabet wrapper commands
	if len(cmdFiles) == 0 {
		return dimensionScore{scored: true}
	}

	total := 0
	matches := 0
	for _, content := range cmdFiles {
		found := pathRe.FindAllStringSubmatch(content, -1)
		for _, match := range found {
			if len(match) < 2 {
				continue
			}
			total++
			if specPathExists(spec.Paths, match[1]) {
				matches++
			}
		}
	}

	if total == 0 {
		return dimensionScore{scored: true}
	}
	return dimensionScore{
		score:  (matches * 10) / total,
		scored: true,
	}
}

func evaluateAuthProtocol(dir string, spec *openAPISpecInfo) dimensionScore {
	if spec == nil {
		return dimensionScore{}
	}
	clientContent := readFileContent(filepath.Join(dir, "internal", "client", "client.go"))
	configContent := readFileContent(filepath.Join(dir, "internal", "config", "config.go"))

	if len(spec.SecurityRequirements) == 0 {
		// No securitySchemes in spec. Check if auth was inferred from description
		// text (marked with "Auth inferred" comment in generated config.go).
		// Do NOT match on env var names alone — inferQueryParamAuth also produces
		// _API_KEY env vars for query-param auth, and scoring those as inferred
		// header auth would penalize correct query-param implementations.
		if !strings.Contains(configContent, "Auth inferred") {
			return dimensionScore{} // no inferred auth marker → skip scoring
		}
		// Inferred auth — score based on what the CLI actually has
		score := 1 // annotated as inferred (user knows to verify)
		if strings.Contains(configContent, "os.Getenv(") {
			score += 4 // env var support present
		}
		if strings.Contains(clientContent, "Authorization") || strings.Contains(clientContent, "X-Api-Key") || strings.Contains(clientContent, "X-Auth-Token") || strings.Contains(clientContent, "X-Access-Token") {
			score += 3 // client sends auth header (standard or custom)
		}
		// Query-param auth (e.g., TMDb ?api_key=, Google Maps ?key=):
		// the client adds the API key to the URL query string instead of a header.
		if strings.Contains(clientContent, `q.Set("api_key"`) ||
			strings.Contains(clientContent, `q.Set("key"`) ||
			strings.Contains(clientContent, `q.Set("apikey"`) ||
			strings.Contains(clientContent, `q.Set("apiKey"`) ||
			strings.Contains(clientContent, `params["api_key"]`) ||
			strings.Contains(clientContent, `params["apikey"]`) {
			score += 3 // client sends auth via query param
		}
		return dimensionScore{scored: true, score: score}
	}
	authContent := readFileContent(filepath.Join(dir, "internal", "cli", "auth.go"))
	if clientContent == "" {
		return dimensionScore{scored: true}
	}

	totalScore := 0
	scoredSets := 0
	for _, requirementSet := range spec.SecurityRequirements {
		if requirementSet.AllowsAnonymous {
			continue
		}

		bestScore := -1
		scoreable := false
		for _, alternative := range requirementSet.Alternatives {
			score, ok := scoreAuthAlternative(clientContent, configContent, authContent, spec.SecuritySchemes, alternative)
			if !ok {
				continue
			}
			scoreable = true
			if score > bestScore {
				bestScore = score
			}
		}
		if !scoreable {
			continue
		}

		totalScore += bestScore
		scoredSets++
	}
	if scoredSets == 0 {
		return dimensionScore{}
	}
	return dimensionScore{
		score:  totalScore / scoredSets,
		scored: true,
	}
}

func parseSecurityRequirementSet(value any) (securityRequirementSet, bool) {
	requirements, ok := value.([]any)
	if !ok {
		return securityRequirementSet{}, false
	}

	set := securityRequirementSet{}
	if len(requirements) == 0 {
		set.AllowsAnonymous = true
		return set, true
	}

	seenAlternatives := make(map[string]struct{})
	for _, requirement := range requirements {
		names, ok := requirement.(map[string]any)
		if !ok {
			continue
		}
		if len(names) == 0 {
			set.AllowsAnonymous = true
			continue
		}

		var alternative []string
		for name := range names {
			if strings.TrimSpace(name) == "" {
				continue
			}
			alternative = append(alternative, name)
		}
		if len(alternative) == 0 {
			set.AllowsAnonymous = true
			continue
		}
		slices.Sort(alternative)
		key := strings.Join(alternative, "\x00")
		if _, ok := seenAlternatives[key]; ok {
			continue
		}
		seenAlternatives[key] = struct{}{}
		set.Alternatives = append(set.Alternatives, alternative)
	}

	return set, true
}

func scoreAuthAlternative(clientContent, configContent, authContent string, schemes map[string]openAPISecurityScheme, alternative []string) (int, bool) {
	if len(alternative) == 0 {
		return 0, false
	}

	total := 0
	scoreableSchemes := 0
	for _, key := range alternative {
		scheme, ok := schemes[key]
		if !ok {
			continue
		}
		score, scoreable := scoreAuthScheme(clientContent, configContent, authContent, scheme)
		if !scoreable {
			continue
		}
		total += score
		scoreableSchemes++
	}
	if scoreableSchemes == 0 {
		return 0, false
	}
	return total / scoreableSchemes, true
}

func scoreAuthScheme(clientContent, configContent, authContent string, scheme openAPISecurityScheme) (int, bool) {
	nameLower := strings.ToLower(scheme.Key)
	headerName := "Authorization"
	authHeaderMatched := false
	headerNameMatched := false
	queryMatched := false
	envMatched := false
	scoreable := false

	if strings.EqualFold(scheme.Type, "apikey") && scheme.In == "header" && strings.TrimSpace(scheme.HeaderName) != "" {
		headerName = scheme.HeaderName
	}

	switch {
	case strings.Contains(nameLower, "bot"):
		scoreable = true
		if authPrefixLiteralPresent("Bot", clientContent, configContent, authContent) {
			authHeaderMatched = true
		}
	case strings.Contains(nameLower, "bearer") || (scheme.Type == "http" && scheme.Scheme == "bearer"):
		scoreable = true
		if authPrefixLiteralPresent("Bearer", clientContent, configContent, authContent) {
			authHeaderMatched = true
		}
	case strings.Contains(nameLower, "basic") || (scheme.Type == "http" && scheme.Scheme == "basic"):
		scoreable = true
		if authPrefixLiteralPresent("Basic", clientContent, configContent, authContent) {
			authHeaderMatched = true
		}
	case strings.EqualFold(scheme.Type, "apikey"):
		scoreable = true
		// For apiKey schemes, the header value format varies (Bearer, Bot, custom).
		// Credit authHeaderMatched if the client sets the correct header name,
		// since that proves the auth plumbing is wired correctly regardless of format.
		if scheme.In == "header" && headerName != "" {
			if strings.Contains(clientContent, `Header.Set("`+headerName+`"`) ||
				strings.Contains(clientContent, `Header.Add("`+headerName+`"`) {
				authHeaderMatched = true
			}
		}
	case strings.EqualFold(scheme.Type, "oauth2"), strings.EqualFold(scheme.Type, "openidconnect"):
		scoreable = true
		if authPrefixLiteralPresent("Bearer", clientContent, configContent, authContent) {
			authHeaderMatched = true
		}
	}
	if !scoreable {
		return 0, false
	}

	if strings.Contains(clientContent, `Header.Set("`+headerName+`"`) ||
		strings.Contains(clientContent, `Header.Add("`+headerName+`"`) {
		headerNameMatched = true
	}

	if scheme.In == "query" && (strings.Contains(clientContent, ".Query()") || strings.Contains(clientContent, "url.Values") || strings.Contains(clientContent, "RawQuery")) {
		queryMatched = true
	}

	envNeedle := sanitizeEnvName(scheme.Key)
	if envNeedle != "" && strings.Contains(strings.ToUpper(configContent), envNeedle) {
		envMatched = true
	}
	// Browser cookie auth (composed or cookie type) uses Chrome cookie extraction
	// instead of env vars. Credit envMatched if the auth code has cookie tooling.
	if !envMatched && (strings.Contains(authContent, "detectCookieTool") ||
		strings.Contains(authContent, "--chrome") ||
		strings.Contains(configContent, "chrome-composed") ||
		strings.Contains(configContent, `"browser"`)) {
		envMatched = true
	}

	score := 0
	if authHeaderMatched {
		score += 3
	}
	if headerNameMatched {
		score += 3
	}
	if queryMatched {
		score += 2
	}
	if envMatched {
		score += 2
	}
	if score > 10 {
		score = 10
	}
	return score, true
}

func authPrefixLiteralPresent(prefix string, contents ...string) bool {
	doubleQuoted := `"` + prefix + ` "`
	rawQuoted := "`" + prefix + " `"
	for _, content := range contents {
		if strings.Contains(content, doubleQuoted) || strings.Contains(content, rawQuoted) {
			return true
		}
	}
	return false
}

func isHTTPMethod(method string) bool {
	switch strings.ToLower(method) {
	case "get", "put", "post", "delete", "options", "head", "patch", "trace":
		return true
	default:
		return false
	}
}

func scoreDataPipelineIntegrity(dir string) int {
	score := 0
	cliDir := filepath.Join(dir, "internal", "cli")
	allCLIContent := readAllGoFiles(cliDir)
	storeContent := readFileContent(filepath.Join(dir, "internal", "store", "store.go"))

	if allCLIContent != "" && (strings.Contains(allCLIContent, "/store") || strings.Contains(allCLIContent, "store.")) {
		score++
	}

	domainUpsertRe := regexp.MustCompile(`\.Upsert[A-Z]\w*\(`)
	genericUpsertRe := regexp.MustCompile(`\.Upsert\(`)
	if domainUpsertRe.MatchString(allCLIContent) {
		score += 3
	} else if genericUpsertRe.MatchString(allCLIContent) {
		score += 0
	}

	domainSearchRe := regexp.MustCompile(`\.Search[A-Z]\w*\(`)
	genericSearchRe := regexp.MustCompile(`\.Search\(`)
	if domainSearchRe.MatchString(allCLIContent) {
		score += 3
	} else if genericSearchRe.MatchString(allCLIContent) {
		score += 0
	}

	score += scoreDomainTables(storeContent)
	if score > 10 {
		score = 10
	}
	return score
}

func scoreSyncCorrectness(dir string) int {
	cliDir := filepath.Join(dir, "internal", "cli")
	content := readAllGoFiles(cliDir)
	if content == "" {
		return 0
	}

	score := 0
	if hasNonEmptySyncResources(content) {
		score += 2
	}
	if strings.Contains(content, "GetSyncState") || strings.Contains(content, "sync_state") {
		score += 2
	}
	if strings.Contains(content, "SaveSyncState") {
		score++
	}
	if strings.Contains(content, "paginatedGet") || strings.Contains(content, "hasNextPage") || strings.Contains(content, "endCursor") || strings.Contains(content, "cursor") {
		score += 2
	}
	// URL path parameters only count when other sync signals are present,
	// otherwise any CLI with parameterized routes gets free sync credit.
	hasParamPaths := strings.Contains(content, "/{")
	if score > 0 && hasParamPaths {
		score += 3
	}
	// When the API has no parameterized list endpoints, the path-params bonus
	// is N/A. Rescale the max from 10 to 7 so flat APIs aren't penalized for
	// not having hierarchical resources.
	max := 10
	if !hasParamPaths {
		max = 7
	}
	if score > max {
		score = max
	}
	// Rescale to 0-10 range
	return score * 10 / max
}

func scoreTypeFidelity(dir string) int {
	score := 0
	cmdFiles := sampleCommandFiles(dir, 10)
	if len(cmdFiles) == 0 {
		return 0
	}

	flagDeclRe := regexp.MustCompile(`Flags\(\)\.(StringVar|IntVar|StringVarP|IntVarP)\(&[^,]+,\s*"([^"]+)"(?:,\s*[^,]+){1,2},\s*"([^"]*)"`)
	requiredRe := regexp.MustCompile(`MarkFlagRequired\("([^"]+)"\)`)

	totalIDFlags := 0
	stringIDFlags := 0
	requiredCount := 0
	descWordCount := 0
	descCount := 0

	for _, content := range cmdFiles {
		for _, match := range flagDeclRe.FindAllStringSubmatch(content, -1) {
			name := strings.ToLower(match[2])
			if strings.Contains(name, "id") {
				totalIDFlags++
				if strings.HasPrefix(match[1], "StringVar") {
					stringIDFlags++
				}
			}
			descWordCount += len(strings.Fields(match[3]))
			descCount++
		}
		requiredCount += len(requiredRe.FindAllStringSubmatch(content, -1))
	}

	if totalIDFlags == 0 || stringIDFlags == totalIDFlags {
		score += 2
	}
	if requiredCount >= 3 {
		score++
	}
	if descCount > 0 && descWordCount/descCount > 5 {
		score++
	}

	var allCLIBuilder strings.Builder
	for _, content := range sampleCommandFiles(dir, 0) {
		allCLIBuilder.WriteString(content)
	}
	allCLIBuilder.WriteString(readFileContent(filepath.Join(dir, "internal", "cli", "helpers.go")))
	allCLIBuilder.WriteString(readFileContent(filepath.Join(dir, "internal", "cli", "root.go")))
	allCLI := allCLIBuilder.String()
	if !strings.Contains(allCLI, "var _ = strings.ReplaceAll") && !strings.Contains(allCLI, "var _ = fmt.Sprintf") {
		score++
	}

	if score > 5 {
		score = 5
	}
	return score
}

func scoreDeadCode(dir string) int {
	deadFlags := 0
	deadFunctions := 0
	cliDir := filepath.Join(dir, "internal", "cli")
	rootContent := readFileContent(filepath.Join(cliDir, "root.go"))
	helpersContent := readFileContent(filepath.Join(cliDir, "helpers.go"))
	if rootContent == "" && helpersContent == "" {
		return 0
	}

	flagRe := regexp.MustCompile(`&flags\.(\w+)`)
	flagNames := uniqueMatches(flagRe, rootContent)
	otherCLI := readOtherGoFiles(cliDir, map[string]bool{"root.go": true})

	// If the flags struct is passed as a function argument, all fields are reachable
	flagsPassedRe := regexp.MustCompile(`\bflags[,)]`)
	flagsPassedAsArg := flagsPassedRe.MatchString(otherCLI)
	if !flagsPassedAsArg {
		for _, name := range flagNames {
			if !strings.Contains(otherCLI, "flags."+name) {
				deadFlags++
			}
		}
	}

	funcRe := regexp.MustCompile(`(?m)^func\s+([A-Za-z_]\w*)\s*\(`)
	funcNames := uniqueMatches(funcRe, helpersContent)
	otherHelpers := readOtherGoFiles(cliDir, map[string]bool{"helpers.go": true})
	// Check both other files AND helpers.go itself for intra-file calls.
	// Use Count >= 2 because the definition itself contributes 1 occurrence of name+"(".
	allContent := helpersContent + "\n" + otherHelpers
	for _, name := range funcNames {
		if strings.Count(allContent, name+"(") < 2 {
			deadFunctions++
		}
	}

	score := 5 - (deadFlags + deadFunctions)
	if score < 0 {
		return 0
	}
	return score
}

// sampleCommandFiles reads up to n command files from internal/cli/.
// If n <= 0, reads all command files.
func sampleCommandFiles(dir string, n int) []string {
	cliDir := filepath.Join(dir, "internal", "cli")
	entries, err := os.ReadDir(cliDir)
	if err != nil {
		return nil
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		if infraAllFiles[e.Name()] {
			continue
		}
		content := readFileContent(filepath.Join(cliDir, e.Name()))
		if content != "" {
			files = append(files, content)
		}
		if n > 0 && len(files) >= n {
			break
		}
	}
	return files
}

func specPathExists(specPaths []string, actual string) bool {
	for _, candidate := range specPaths {
		if matchSpecPath(candidate, actual) || matchSpecPath(actual, candidate) {
			return true
		}
	}
	return false
}

func matchSpecPath(pattern, actual string) bool {
	patternParts := splitPath(pattern)
	actualParts := splitPath(actual)
	if len(patternParts) != len(actualParts) {
		return false
	}
	for i := range patternParts {
		part := patternParts[i]
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			continue
		}
		if part != actualParts[i] {
			return false
		}
	}
	return true
}

func splitPath(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

func sanitizeEnvName(name string) string {
	name = strings.ToUpper(name)
	var b strings.Builder
	lastUnderscore := false
	for _, r := range name {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func scoreDomainTables(storeContent string) int {
	if storeContent == "" {
		return 0
	}
	createTableRe := regexp.MustCompile(`(?is)CREATE TABLE[^()]*\((.*?)\)`)
	columnTables := 0
	for _, match := range createTableRe.FindAllStringSubmatch(storeContent, -1) {
		columnCount := 0
		for line := range strings.SplitSeq(match[1], "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "--") {
				continue
			}
			upper := strings.ToUpper(line)
			if strings.HasPrefix(upper, "PRIMARY KEY") || strings.HasPrefix(upper, "FOREIGN KEY") || strings.HasPrefix(upper, "UNIQUE") || strings.HasPrefix(upper, "CONSTRAINT") {
				continue
			}
			columnCount++
		}
		if columnCount >= 5 {
			columnTables++
		}
	}
	if columnTables > 0 {
		return 3
	}
	return 0
}

func hasNonEmptySyncResources(content string) bool {
	if !strings.Contains(content, "defaultSyncResources") && !strings.Contains(content, "syncResources") {
		return false
	}
	// Look for non-empty []string{...} literals (sync resource lists)
	listRe := regexp.MustCompile(`\[\]string\{([^}]*)\}`)
	for _, match := range listRe.FindAllStringSubmatch(content, -1) {
		items := strings.TrimSpace(match[1])
		if items != "" {
			return true
		}
	}
	// If defaultSyncResources is called but its definition isn't in the content,
	// assume it's non-empty (defined in a different package/file).
	// If the definition IS here, the listRe above already checked all []string{} literals.
	defRe := regexp.MustCompile(`func\s+defaultSyncResources\s*\(`)
	if strings.Contains(content, "defaultSyncResources()") && !defRe.MatchString(content) {
		return true
	}
	return false
}

func uniqueMatches(re *regexp.Regexp, content string) []string {
	seen := map[string]bool{}
	var out []string
	for _, match := range re.FindAllStringSubmatch(content, -1) {
		if len(match) < 2 || seen[match[1]] {
			continue
		}
		seen[match[1]] = true
		out = append(out, match[1])
	}
	return out
}

// readAllGoFiles concatenates the content of all .go files in dir.
func readAllGoFiles(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		b.WriteString(readFileContent(filepath.Join(dir, entry.Name())))
		b.WriteByte('\n')
	}
	return b.String()
}

func readOtherGoFiles(dir string, skip map[string]bool) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || skip[entry.Name()] {
			continue
		}
		b.WriteString(readFileContent(filepath.Join(dir, entry.Name())))
		b.WriteByte('\n')
	}
	return b.String()
}

func asString(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case fmt.Stringer:
		return value.String()
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64)
	default:
		return ""
	}
}

// hasPlaceholderValues checks if file content contains common placeholder values
// that indicate unpolished examples.
func hasPlaceholderValues(content string) bool {
	placeholders := []string{"abc123", `"value"`, "my-resource", "your-key-here", "USER/tap"}
	for _, p := range placeholders {
		if strings.Contains(content, p) {
			return true
		}
	}
	return false
}

// hasQualityDescription checks if a command file has a meaningful Short description.
// Returns true if the description is multi-word and doesn't just repeat the verb.
func hasQualityDescription(content string) bool {
	idx := strings.Index(content, "Short:")
	if idx < 0 {
		return false
	}
	// Extract the Short value (between quotes)
	rest := content[idx:]
	q1 := strings.Index(rest, `"`)
	if q1 < 0 {
		return false
	}
	q2 := strings.Index(rest[q1+1:], `"`)
	if q2 < 0 {
		return false
	}
	desc := rest[q1+1 : q1+1+q2]
	// Minimum quality: multi-word and non-trivial length.
	// Actual description quality (informative vs boilerplate) is handled by
	// the skill instruction during Phase 3 polish, not by this scorer.
	return len(desc) > 10 && strings.Contains(desc, " ")
}

// hasLazyDescription checks if a command has a 1-word or very short description.
func hasLazyDescription(content string) bool {
	idx := strings.Index(content, "Short:")
	if idx < 0 {
		return false
	}
	rest := content[idx:]
	q1 := strings.Index(rest, `"`)
	if q1 < 0 {
		return false
	}
	q2 := strings.Index(rest[q1+1:], `"`)
	if q2 < 0 {
		return false
	}
	desc := rest[q1+1 : q1+1+q2]
	words := strings.Fields(desc)
	return len(words) <= 2
}

func readFileContent(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func computeGrade(percentage int) string {
	switch {
	case percentage >= 80:
		return "A"
	case percentage >= 65:
		return "B"
	case percentage >= 50:
		return "C"
	case percentage >= 35:
		return "D"
	default:
		return "F"
	}
}

// loadCLIManifestForScorecard reads .printing-press.json from the CLI directory.
// Returns an empty manifest (not error) if the file does not exist, so callers
// can check MCPBinary != "" to decide whether to show MCP info.
func loadCLIManifestForScorecard(outputDir string) (CLIManifest, error) {
	data, err := os.ReadFile(filepath.Join(outputDir, CLIManifestFilename))
	if err != nil {
		return CLIManifest{}, err
	}
	var m CLIManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return CLIManifest{}, err
	}
	return m, nil
}

func buildGapReport(s SteinerScore, unscored []string) []string {
	var gaps []string
	unscoredSet := make(map[string]struct{}, len(unscored))
	for _, name := range unscored {
		unscoredSet[name] = struct{}{}
	}
	dimensions := []struct {
		name  string
		score int
	}{
		{"output_modes", s.OutputModes},
		{"auth", s.Auth},
		{"error_handling", s.ErrorHandling},
		{"terminal_ux", s.TerminalUX},
		{"readme", s.README},
		{"doctor", s.Doctor},
		{"agent_native", s.AgentNative},
		{"mcp_quality", s.MCPQuality},
		{DimMCPDescriptionQuality, s.MCPDescriptionQuality},
		{DimMCPTokenEfficiency, s.MCPTokenEff},
		{"local_cache", s.LocalCache},
		{"breadth", s.Breadth},
		{"vision", s.Vision},
		{"workflows", s.Workflows},
		{"insight", s.Insight},
		{DimPathValidity, s.PathValidity},
		{DimAuthProtocol, s.AuthProtocol},
		{"data_pipeline_integrity", s.DataPipelineIntegrity},
		{"sync_correctness", s.SyncCorrectness},
		{"type_fidelity", s.TypeFidelity},
		{"dead_code", s.DeadCode},
	}
	for _, d := range dimensions {
		if _, skip := unscoredSet[d.name]; skip {
			continue
		}
		max := 10
		if d.name == "type_fidelity" || d.name == "dead_code" {
			max = 5
		}
		if d.score < max/2 {
			gaps = append(gaps, fmt.Sprintf("%s scored %d/%d - needs improvement", d.name, d.score, max))
		}
	}
	return gaps
}

func buildCompetitorScores(ourTotal int, artifactDir string) []CompScore {
	research, err := loadResearchForArtifactsDir(artifactDir)
	if err != nil {
		return nil
	}
	var scores []CompScore
	for _, alt := range research.Alternatives {
		theirScore := estimateCompetitorTotal(alt)
		scores = append(scores, CompScore{
			Name:       alt.Name,
			OurScore:   ourTotal,
			TheirScore: theirScore,
			WeWin:      ourTotal > theirScore,
		})
	}
	return scores
}

func loadResearchForArtifactsDir(artifactDir string) (*ResearchResult, error) {
	parent := filepath.Dir(artifactDir)
	var candidates []string
	switch filepath.Base(artifactDir) {
	case "research":
		candidates = []string{artifactDir, filepath.Join(parent, "pipeline")}
	case "proofs", "pipeline":
		candidates = []string{filepath.Join(parent, "research"), artifactDir, filepath.Join(parent, "pipeline")}
	default:
		candidates = []string{artifactDir, filepath.Join(artifactDir, "research"), filepath.Join(artifactDir, "pipeline")}
	}

	var lastErr error
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}

		research, err := LoadResearch(candidate)
		if err == nil {
			return research, nil
		}
		lastErr = err
	}

	if lastErr == nil {
		lastErr = os.ErrNotExist
	}
	return nil, lastErr
}

func estimateCompetitorTotal(alt Alternative) int {
	score := 0
	if alt.HasJSON {
		score += 6 // output_modes partial credit
	}
	if alt.HasAuth {
		score += 5 // auth partial credit
	}
	// Assume basic error handling and terminal UX
	score += 3
	score += 3
	// README and doctor are unknowns - give partial credit
	score += 4
	score += 2
	// Agent native: partial if they have JSON
	if alt.HasJSON {
		score += 3
	}
	return score
}

func writeScorecardMD(sc *Scorecard, pipelineDir string) error {
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		return err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Scorecard: %s\n\n", sc.APIName)
	fmt.Fprintf(&b, "**Overall Grade: %s** (%d%%)\n\n", sc.OverallGrade, sc.Steinberger.Percentage)
	if len(sc.UnscoredDimensions) > 0 {
		fmt.Fprintf(&b, "Unscored dimensions omitted from the total denominator: %s\n\n", strings.Join(sc.UnscoredDimensions, ", "))
	}

	// Steinberger dimensions table
	b.WriteString("## Quality Dimensions\n\n")
	b.WriteString("| Dimension | Score |\n")
	b.WriteString("|-----------|-------|\n")
	s := sc.Steinberger
	dimensions := []struct {
		name    string
		nameKey string
		score   int
	}{
		{"Output Modes", "output_modes", s.OutputModes},
		{"Auth", "auth", s.Auth},
		{"Error Handling", "error_handling", s.ErrorHandling},
		{"Terminal UX", "terminal_ux", s.TerminalUX},
		{"README", "readme", s.README},
		{"Doctor", "doctor", s.Doctor},
		{"Agent Native", "agent_native", s.AgentNative},
		{"MCP Quality", "mcp_quality", s.MCPQuality},
		{"MCP Description Quality", DimMCPDescriptionQuality, s.MCPDescriptionQuality},
		{"MCP Token Efficiency", DimMCPTokenEfficiency, s.MCPTokenEff},
		{"Local Cache", "local_cache", s.LocalCache},
		{"Breadth", "breadth", s.Breadth},
		{"Vision", "vision", s.Vision},
		{"Workflows", "workflows", s.Workflows},
		{"Insight", "insight", s.Insight},
		{"Path Validity", DimPathValidity, s.PathValidity},
		{"Auth Protocol", DimAuthProtocol, s.AuthProtocol},
		{"Data Pipeline Integrity", "data_pipeline_integrity", s.DataPipelineIntegrity},
		{"Sync Correctness", "sync_correctness", s.SyncCorrectness},
	}
	for _, d := range dimensions {
		if sc.IsDimensionUnscored(d.nameKey) {
			fmt.Fprintf(&b, "| %s | N/A |\n", d.name)
			continue
		}
		bar := strings.Repeat("#", d.score) + strings.Repeat(".", 10-d.score)
		fmt.Fprintf(&b, "| %s | %d/10 %s |\n", d.name, d.score, bar)
	}
	typeDimensions := []struct {
		name  string
		score int
	}{
		{"Type Fidelity", s.TypeFidelity},
		{"Dead Code", s.DeadCode},
	}
	for _, d := range typeDimensions {
		bar := strings.Repeat("#", d.score) + strings.Repeat(".", 5-d.score)
		fmt.Fprintf(&b, "| %s | %d/5 %s |\n", d.name, d.score, bar)
	}
	fmt.Fprintf(&b, "| **Total** | **%d/100** |\n\n", s.Total)

	// Competitor comparison
	if len(sc.CompetitorScores) > 0 {
		b.WriteString("## Competitor Comparison\n\n")
		b.WriteString("| Competitor | Ours | Theirs | Winner |\n")
		b.WriteString("|------------|------|--------|--------|\n")
		for _, cs := range sc.CompetitorScores {
			winner := "Them"
			if cs.WeWin {
				winner = "Us"
			}
			fmt.Fprintf(&b, "| %s | %d | %d | %s |\n", cs.Name, cs.OurScore, cs.TheirScore, winner)
		}
		b.WriteString("\n")
	}

	// Gap report
	if len(sc.GapReport) > 0 {
		b.WriteString("## Gaps\n\n")
		for _, g := range sc.GapReport {
			fmt.Fprintf(&b, "- %s\n", g)
		}
		b.WriteString("\n")
	}

	return os.WriteFile(filepath.Join(pipelineDir, "scorecard.md"), []byte(b.String()), 0o644)
}

func writeScorecardJSON(sc *Scorecard, pipelineDir string) error {
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(pipelineDir, "scorecard.json"), data, 0o644)
}

// LoadScorecard reads a scorecard from a pipeline directory's scorecard.json.
func LoadScorecard(pipelineDir string) (*Scorecard, error) {
	data, err := os.ReadFile(filepath.Join(pipelineDir, "scorecard.json"))
	if err != nil {
		return nil, err
	}
	var sc Scorecard
	if err := json.Unmarshal(data, &sc); err != nil {
		return nil, err
	}
	return &sc, nil
}
