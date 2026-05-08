package pipeline

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	catalogfs "github.com/mvanhorn/cli-printing-press/v4/catalog"
	"github.com/mvanhorn/cli-printing-press/v4/internal/catalog"
	"github.com/mvanhorn/cli-printing-press/v4/internal/llm"
)

// ResearchResult holds the output of the research phase.
type ResearchResult struct {
	APIName            string              `json:"api_name"`
	NoveltyScore       int                 `json:"novelty_score"` // 1-10
	Alternatives       []Alternative       `json:"alternatives"`
	Gaps               []string            `json:"gaps"`           // what alternatives miss
	Patterns           []string            `json:"patterns"`       // what alternatives do well
	Recommendation     string              `json:"recommendation"` // "proceed", "proceed-with-gaps", "skip"
	ResearchedAt       time.Time           `json:"researched_at"`
	CompetitorInsights *CompetitorInsights `json:"competitor_insights,omitempty"`
	NovelFeatures      []NovelFeature      `json:"novel_features,omitempty"`
	// NovelFeaturesBuilt is the verified subset written by dogfood. Intentionally
	// NOT omitempty: an empty [] means "dogfood ran, nothing survived" while a
	// missing field means "dogfood hasn't validated yet."
	NovelFeaturesBuilt *[]NovelFeature `json:"novel_features_built,omitempty"`
	// Narrative holds LLM-authored prose for README and SKILL.md rendering.
	// Optional: templates fall back to generic content when absent. Populated
	// during the absorb phase alongside NovelFeatures.
	Narrative *ReadmeNarrative `json:"narrative,omitempty"`
}

// NovelFeature represents a transcendence feature invented during the absorb
// phase — a capability not found in any existing tool for this API.
type NovelFeature struct {
	Name        string `json:"name"`
	Command     string `json:"command"`
	Description string `json:"description"`
	Rationale   string `json:"rationale"`
	// Aliases are optional alternate command paths that should be considered
	// "this feature was built" during dogfood's novel-feature verification.
	// Planners use this when a feature may ship under any of several command
	// names (e.g. `["auth login-chrome", "auth browser-login"]`). Empty by
	// default — the three-pass matcher still covers most natural drift.
	Aliases []string `json:"aliases,omitempty"`
	// Example is a ready-to-run invocation with realistic arguments. Rendered
	// beneath the feature's description in README and SKILL so agents can
	// copy-paste without synthesizing args. Authored by the absorb LLM pass.
	Example string `json:"example,omitempty"`
	// WhyItMatters is a one-sentence agent-facing rationale for when to reach
	// for this feature. Rendered as italic text beneath the description.
	WhyItMatters string `json:"why_it_matters,omitempty"`
	// Group is a theme name used to cluster features in README and SKILL
	// (e.g. "Local state that compounds", "Reachability mitigation"). When
	// any feature carries a Group, the Unique Features block switches from
	// a flat bullet list to grouped subsections. Empty means ungrouped.
	Group string `json:"group,omitempty"`
}

// ReadmeNarrative holds LLM-authored prose and examples used to make the
// generated README and SKILL.md feel like product documentation rather than
// scaffolding. All fields are optional; templates degrade gracefully when
// absent. Populated during the absorb phase from the brief and competitor
// research; persisted in research.json so rebuilds are reproducible.
type ReadmeNarrative struct {
	// DisplayName is the canonical prose name for the API/product. It may
	// differ from the slug used for code and binary names, e.g. "Product Hunt"
	// instead of "producthunt".
	DisplayName string `json:"display_name,omitempty"`
	// Headline is a bold one-sentence value prop rendered beneath the title
	// (e.g. "Every Notion feature, plus sync, search, and a local database").
	Headline string `json:"headline,omitempty"`
	// ValueProp is a 2–3 sentence expansion of the headline rendered as a
	// paragraph between the headline and Install.
	ValueProp string `json:"value_prop,omitempty"`
	// AuthNarrative is an API-specific authentication story (how the API
	// actually works, not just "set an env var"). Rendered in place of the
	// generic auth branch when present.
	AuthNarrative string `json:"auth_narrative,omitempty"`
	// QuickStart is a sequence of realistic command invocations forming a
	// "try this" flow. Rendered in the Quick Start section when populated,
	// replacing the generic first-resource example.
	QuickStart []QuickStartStep `json:"quickstart,omitempty"`
	// Troubleshoots are API-specific symptom/fix pairs appended to the
	// generic Troubleshooting bullets.
	Troubleshoots []TroubleshootTip `json:"troubleshoots,omitempty"`
	// WhenToUse is a 2–4 sentence narrative rendered in SKILL.md describing
	// the CLI's ideal use cases. Not rendered in README.
	WhenToUse string `json:"when_to_use,omitempty"`
	// Recipes are worked examples rendered in SKILL.md's Recipes section.
	// Each recipe is a titled command with an explanation.
	Recipes []Recipe `json:"recipes,omitempty"`
	// TriggerPhrases are natural-language phrases that should invoke this
	// CLI's skill. LLM-authored per CLI so domain verbs vary appropriately
	// (finance: "quote AAPL"; media: "play track X"). Rendered in SKILL
	// frontmatter's description field.
	TriggerPhrases []string `json:"trigger_phrases,omitempty"`
}

// QuickStartStep is a single step in a Quick Start flow. Comment is an
// optional human-readable preamble (rendered as a bash comment above the
// command) so agents understand *why* the command runs.
type QuickStartStep struct {
	Command string `json:"command"`
	Comment string `json:"comment,omitempty"`
}

// Recipe is a worked example for SKILL.md. Title is rendered as a heading,
// Command as a fenced code block, Explanation as a paragraph beneath.
type Recipe struct {
	Title       string `json:"title"`
	Command     string `json:"command"`
	Explanation string `json:"explanation,omitempty"`
}

// TroubleshootTip pairs a user-visible symptom with an actionable fix.
// Rendered as "**Symptom** — Fix" bullets in the Troubleshooting section.
type TroubleshootTip struct {
	Symptom string `json:"symptom"`
	Fix     string `json:"fix"`
}

// CompetitorAnalysis holds intelligence gathered from a single competitor repo.
type CompetitorAnalysis struct {
	RepoURL         string   `json:"repo_url"`
	CommandsFound   []string `json:"commands_found"`
	FeatureRequests []string `json:"feature_requests"`
	AbandonedPRs    []string `json:"abandoned_prs"`
	PainPoints      []string `json:"pain_points"`
	CommandCount    int      `json:"command_count"`
}

// CompetitorInsights aggregates intelligence across all analyzed competitors.
type CompetitorInsights struct {
	Analyses          []CompetitorAnalysis `json:"analyses"`
	CommandTarget     int                  `json:"command_target"`
	UnmetFeatures     []string             `json:"unmet_features"`
	PainPointsToAvoid []string             `json:"pain_points_to_avoid"`
}

// Alternative represents a known competing CLI tool.
type Alternative struct {
	Name          string `json:"name"`
	URL           string `json:"url"`
	Language      string `json:"language"`
	InstallMethod string `json:"install_method"` // brew, npm, pip, cargo, binary
	Stars         int    `json:"stars"`
	LastUpdated   string `json:"last_updated"`
	CommandCount  int    `json:"command_count"`
	HasJSON       bool   `json:"has_json_output"`
	HasAuth       bool   `json:"has_auth_support"`
}

// RunResearch executes the research phase for an API.
// It checks the catalog for known alternatives, then optionally
// queries the GitHub API for additional CLI tools.
func RunResearch(apiName, pipelineDir string) (*ResearchResult, error) {
	result := &ResearchResult{
		APIName:      apiName,
		ResearchedAt: time.Now(),
	}

	// Step 1: Check catalog for known alternatives
	catalogAlts := loadCatalogAlternatives(apiName)
	for _, alt := range catalogAlts {
		result.Alternatives = append(result.Alternatives, Alternative{
			Name:     alt.Name,
			URL:      alt.URL,
			Language: alt.Language,
		})
	}

	// Step 2: Search GitHub for "<api-name> cli" repos
	ghAlts, err := searchGitHubCLIs(apiName)
	if err != nil {
		// Non-fatal: log and continue with catalog-only results
		fmt.Fprintf(os.Stderr, "warning: GitHub search failed: %v\n", err)
	} else {
		result.Alternatives = append(result.Alternatives, ghAlts...)
	}

	// Step 3: Deduplicate by URL
	result.Alternatives = deduplicateAlts(result.Alternatives)

	// Step 4: Score novelty and produce recommendation
	result.NoveltyScore = scoreNovelty(result.Alternatives)
	result.Recommendation = recommend(result.NoveltyScore)

	// Step 5: Analyze gaps and patterns
	result.Gaps, result.Patterns = analyzeAlternatives(result.Alternatives)

	// Step 5.5: Competitor intelligence - analyze GitHub repos
	useLLM := llm.Available()
	var analyses []CompetitorAnalysis
	for _, alt := range result.Alternatives {
		owner, repo := parseGitHubURL(alt.URL)
		if owner == "" || repo == "" {
			continue
		}

		// Always fetch README and issues via API (needed for both paths)
		analysis, err := analyzeCompetitorRepo(owner, repo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: competitor analysis failed for %s/%s: %v\n", owner, repo, err)
			continue
		}

		// If LLM is available, enhance with deeper analysis
		if useLLM {
			client := &http.Client{Timeout: 15 * time.Second}
			readmeContent, readmeErr := fetchReadme(client, owner, repo)
			if readmeErr == nil {
				issueTitles := analysis.FeatureRequests
				llmAnalysis, llmErr := analyzeCompetitorRepoLLM(owner, repo, readmeContent, issueTitles)
				if llmErr != nil {
					fmt.Fprintf(os.Stderr, "warning: LLM competitor analysis failed for %s/%s, using regex: %v\n", owner, repo, llmErr)
				} else {
					// Merge LLM insights into the base analysis
					if len(llmAnalysis.CommandsFound) > len(analysis.CommandsFound) {
						analysis.CommandsFound = llmAnalysis.CommandsFound
						analysis.CommandCount = len(llmAnalysis.CommandsFound)
					}
					if len(llmAnalysis.PainPoints) > len(analysis.PainPoints) {
						analysis.PainPoints = llmAnalysis.PainPoints
					}
					if len(llmAnalysis.FeatureRequests) > len(analysis.FeatureRequests) {
						analysis.FeatureRequests = llmAnalysis.FeatureRequests
					}
				}
			}
		}

		analyses = append(analyses, *analysis)
	}
	if len(analyses) > 0 {
		insights := synthesizeInsights(analyses)
		result.CompetitorInsights = &insights
	}

	// Step 6: Write research.json
	if err := writeResearchJSON(result, pipelineDir); err != nil {
		return result, fmt.Errorf("writing research.json: %w", err)
	}

	return result, nil
}

// LoadResearch reads research.json from a pipeline directory.
func LoadResearch(pipelineDir string) (*ResearchResult, error) {
	data, err := os.ReadFile(filepath.Join(pipelineDir, "research.json"))
	if err != nil {
		return nil, err
	}
	var r ResearchResult
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// loadResearchForState reads research.json from the run directory, supporting
// both write conventions: the printing-press skill writes to RunRoot/research.json
// directly, while printing-press print writes to RunRoot/pipeline/research.json
// alongside its phase artifacts. Tries the run-root path first (the dominant
// case), falls back to pipeline-dir.
//
// The two-location lookup matters at promote/publish time, when downstream code
// needs research.json to populate the published manifest's novel_features.
// Without this fallback the publish-time read silently misses skill-flow runs
// and ships manifests with empty novel_features (cal-com retro #334 F2).
func loadResearchForState(state *PipelineState) (*ResearchResult, error) {
	if r, err := LoadResearch(state.RunRoot()); err == nil {
		return r, nil
	}
	return LoadResearch(state.PipelineDir())
}

// WriteNovelFeaturesBuilt updates research.json with the verified list of
// novel features that survived the build. The original novel_features field
// is preserved as-is (the planned list); novel_features_built records what
// actually exists in the CLI.
func WriteNovelFeaturesBuilt(pipelineDir string, built []NovelFeature) error {
	research, err := LoadResearch(pipelineDir)
	if err != nil {
		return err
	}
	research.NovelFeaturesBuilt = &built
	return writeResearchJSON(research, pipelineDir)
}

// ReadmeSource represents a credited ecosystem tool for the generated README.
type ReadmeSource struct {
	Name     string
	URL      string
	Language string
	Stars    int
}

// ParseDiscoveryPages reads a browser-sniff-report.md and extracts the URLs
// from the "Pages Visited" section. Returns nil if the file doesn't exist or
// contains no pages. Pre-rename manuscripts using the legacy "sniff-report.md"
// name are not read — see D3 in docs/plans/2026-04-18-002.
func ParseDiscoveryPages(discoveryDir string) []string {
	data, err := os.ReadFile(filepath.Join(discoveryDir, "browser-sniff-report.md"))
	if err != nil {
		return nil
	}
	var pages []string
	inSection := false
	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "# ") {
			if strings.Contains(strings.ToLower(trimmed), "pages visited") {
				inSection = true
				continue
			}
			if inSection {
				break // hit next section
			}
		}
		if inSection && strings.HasPrefix(trimmed, "- ") {
			page := strings.TrimPrefix(trimmed, "- ")
			if strings.HasPrefix(page, "http") {
				pages = append(pages, page)
			}
		}
	}
	return pages
}

// SourcesForREADME filters and sorts research alternatives into README-ready
// source credits. Alternatives with empty URLs are excluded. Results are sorted
// by stars descending.
func SourcesForREADME(r *ResearchResult) []ReadmeSource {
	if r == nil {
		return nil
	}
	var sources []ReadmeSource
	for _, alt := range r.Alternatives {
		if alt.URL == "" {
			continue
		}
		sources = append(sources, ReadmeSource{
			Name:     alt.Name,
			URL:      alt.URL,
			Language: alt.Language,
			Stars:    alt.Stars,
		})
	}
	sort.Slice(sources, func(i, j int) bool {
		return sources[i].Stars > sources[j].Stars
	})
	return sources
}

func loadCatalogAlternatives(apiName string) []catalog.KnownAlt {
	entry, err := catalog.LookupFS(catalogfs.FS, apiName)
	if err != nil {
		return nil
	}
	return entry.KnownAlternatives
}

// ghSearchResponse models the GitHub search API response.
type ghSearchResponse struct {
	Items []ghRepo `json:"items"`
}

type ghRepo struct {
	FullName    string    `json:"full_name"`
	HTMLURL     string    `json:"html_url"`
	Description string    `json:"description"`
	Language    string    `json:"language"`
	Stars       int       `json:"stargazers_count"`
	PushedAt    time.Time `json:"pushed_at"`
}

func searchGitHubCLIs(apiName string) ([]Alternative, error) {
	query := fmt.Sprintf("%s+cli+language:go+language:python+language:typescript+language:rust", apiName)
	url := fmt.Sprintf("https://api.github.com/search/repositories?q=%s&sort=stars&per_page=5", query)

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	// Use GITHUB_TOKEN if available for higher rate limits
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}

	var searchResp ghSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, err
	}

	var alts []Alternative
	for _, repo := range searchResp.Items {
		alt := Alternative{
			Name:        repo.FullName,
			URL:         repo.HTMLURL,
			Language:    strings.ToLower(repo.Language),
			Stars:       repo.Stars,
			LastUpdated: repo.PushedAt.Format("2006-01-02"),
		}
		// Infer install method from language
		switch strings.ToLower(repo.Language) {
		case "go":
			alt.InstallMethod = "binary"
		case "python":
			alt.InstallMethod = "pip"
		case "typescript", "javascript":
			alt.InstallMethod = "npm"
		case "rust":
			alt.InstallMethod = "cargo"
		default:
			alt.InstallMethod = "source"
		}
		alts = append(alts, alt)
	}
	return alts, nil
}

func deduplicateAlts(alts []Alternative) []Alternative {
	seen := make(map[string]bool)
	var unique []Alternative
	for _, a := range alts {
		key := strings.ToLower(a.URL)
		if key == "" {
			key = strings.ToLower(a.Name)
		}
		if !seen[key] {
			seen[key] = true
			unique = append(unique, a)
		}
	}
	return unique
}

func scoreNovelty(alts []Alternative) int {
	if len(alts) == 0 {
		return 10 // No alternatives - maximum novelty
	}

	// Check for high-star official CLIs
	hasOfficialCLI := false
	maxStars := 0
	for _, a := range alts {
		if a.Stars > maxStars {
			maxStars = a.Stars
		}
		if a.Stars > 5000 {
			hasOfficialCLI = true
		}
	}

	if hasOfficialCLI {
		return 2 // Official CLI exists and is popular
	}
	if maxStars > 1000 {
		return 4 // Popular community CLI exists
	}
	if maxStars > 100 {
		return 6 // Some community alternatives but not dominant
	}
	if len(alts) > 0 {
		return 7 // Alternatives exist but are small/stale
	}
	return 10
}

func recommend(noveltyScore int) string {
	switch {
	case noveltyScore <= 3:
		return "skip"
	case noveltyScore <= 6:
		return "proceed-with-gaps"
	default:
		return "proceed"
	}
}

func analyzeAlternatives(alts []Alternative) (gaps, patterns []string) {
	hasGo := false
	hasJSON := false
	hasDryRun := false

	for _, a := range alts {
		if a.Language == "go" {
			hasGo = true
		}
		if a.HasJSON {
			hasJSON = true
		}
	}

	// Our CLI always has these - identify where alternatives fall short
	if !hasGo {
		patterns = append(patterns, "no Go-based alternative exists - our binary has zero runtime deps")
	}
	if !hasJSON {
		gaps = append(gaps, "most alternatives lack --json output mode")
	}
	if !hasDryRun {
		gaps = append(gaps, "no alternative offers --dry-run mode")
	}

	// Check freshness
	staleCount := 0
	for _, a := range alts {
		if a.LastUpdated != "" {
			if t, err := time.Parse("2006-01-02", a.LastUpdated); err == nil {
				if time.Since(t) > 365*24*time.Hour {
					staleCount++
				}
			}
		}
	}
	if staleCount > 0 && len(alts) > 0 {
		gaps = append(gaps, fmt.Sprintf("%d/%d alternatives are stale (>1 year since last update)", staleCount, len(alts)))
	}

	if len(gaps) == 0 {
		gaps = append(gaps, "alternatives cover the basics")
	}
	if len(patterns) == 0 {
		patterns = append(patterns, "standard CLI patterns (list, get, create, update, delete)")
	}

	return gaps, patterns
}

func writeResearchJSON(result *ResearchResult, pipelineDir string) error {
	if err := os.MkdirAll(pipelineDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(pipelineDir, "research.json"), data, 0o644)
}

// parseGitHubURL extracts owner and repo from a GitHub URL.
// Returns empty strings if the URL is not a valid GitHub repo URL.
func parseGitHubURL(url string) (owner, repo string) {
	// Match https://github.com/owner/repo or https://github.com/owner/repo/...
	url = strings.TrimSuffix(url, "/")
	url = strings.TrimSuffix(url, ".git")
	if !strings.Contains(url, "github.com/") {
		return "", ""
	}
	parts := strings.Split(url, "github.com/")
	if len(parts) < 2 {
		return "", ""
	}
	segments := strings.SplitN(parts[1], "/", 3)
	if len(segments) < 2 {
		return "", ""
	}
	return segments[0], segments[1]
}

// ghIssue models a GitHub issue from the API.
type ghIssue struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	HTMLURL   string `json:"html_url"`
	State     string `json:"state"`
	CreatedAt string `json:"created_at"`
}

// ghPull models a GitHub pull request from the API.
type ghPull struct {
	Title    string  `json:"title"`
	HTMLURL  string  `json:"html_url"`
	MergedAt *string `json:"merged_at"`
}

// ghReadmeResponse models the GitHub README API response.
type ghReadmeResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

// analyzeCompetitorRepo gathers intelligence from a competitor's GitHub repo.
func analyzeCompetitorRepo(owner, repo string) (*CompetitorAnalysis, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	repoURL := fmt.Sprintf("https://github.com/%s/%s", owner, repo)
	analysis := &CompetitorAnalysis{
		RepoURL: repoURL,
	}

	// Fetch feature requests (issues labeled enhancement or feature-request)
	featureIssues, err := fetchIssues(client, owner, repo, "enhancement,feature-request")
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to fetch labeled issues for %s/%s: %v\n", owner, repo, err)
	}
	// If no labeled issues found, fall back to any open issues
	if len(featureIssues) == 0 {
		featureIssues, err = fetchIssues(client, owner, repo, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to fetch open issues for %s/%s: %v\n", owner, repo, err)
		}
	}
	for _, issue := range featureIssues {
		analysis.FeatureRequests = append(analysis.FeatureRequests, issue.Title)
		// Extract pain points from issue bodies mentioning common frustration signals
		if containsPainSignal(issue.Title) || containsPainSignal(issue.Body) {
			analysis.PainPoints = append(analysis.PainPoints, issue.Title)
		}
	}

	// Fetch README and parse for CLI commands
	readmeContent, err := fetchReadme(client, owner, repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to fetch README for %s/%s: %v\n", owner, repo, err)
	} else {
		analysis.CommandsFound = parseCommandsFromReadme(readmeContent)
		analysis.CommandCount = len(analysis.CommandsFound)
	}

	// Fetch abandoned PRs (closed but not merged)
	abandonedPRs, err := fetchAbandonedPRs(client, owner, repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to fetch PRs for %s/%s: %v\n", owner, repo, err)
	}
	for _, pr := range abandonedPRs {
		analysis.AbandonedPRs = append(analysis.AbandonedPRs, pr.Title)
	}

	return analysis, nil
}

// newGitHubRequest creates an http.Request with appropriate headers.
func newGitHubRequest(url string) (*http.Request, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}

func fetchIssues(client *http.Client, owner, repo, labels string) ([]ghIssue, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues?state=open&per_page=10", owner, repo)
	if labels != "" {
		url += "&labels=" + labels
	}

	req, err := newGitHubRequest(url)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body[:min(len(body), 200)]))
	}

	var issues []ghIssue
	if err := json.NewDecoder(resp.Body).Decode(&issues); err != nil {
		return nil, err
	}
	return issues, nil
}

func fetchReadme(client *http.Client, owner, repo string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/readme", owner, repo)

	req, err := newGitHubRequest(url)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var readme ghReadmeResponse
	if err := json.NewDecoder(resp.Body).Decode(&readme); err != nil {
		return "", err
	}

	if readme.Encoding != "base64" {
		return "", fmt.Errorf("unexpected encoding: %s", readme.Encoding)
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(readme.Content, "\n", ""))
	if err != nil {
		return "", fmt.Errorf("decoding base64: %w", err)
	}
	return string(decoded), nil
}

func fetchAbandonedPRs(client *http.Client, owner, repo string) ([]ghPull, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls?state=closed&per_page=5", owner, repo)

	req, err := newGitHubRequest(url)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var prs []ghPull
	if err := json.NewDecoder(resp.Body).Decode(&prs); err != nil {
		return nil, err
	}

	// Filter to only abandoned (closed but not merged)
	var abandoned []ghPull
	for _, pr := range prs {
		if pr.MergedAt == nil {
			abandoned = append(abandoned, pr)
		}
	}
	return abandoned, nil
}

// commandPattern matches CLI command patterns in READMEs.
// Looks for lines like: `toolname subcommand`, `$ toolname subcommand`, or indented command blocks.
var commandPattern = regexp.MustCompile(`(?m)^\s*(?:\$\s+)?(\w[\w-]+)\s+([\w-]+)(?:\s|$)`)

func parseCommandsFromReadme(content string) []string {
	seen := make(map[string]bool)
	var commands []string

	matches := commandPattern.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		cmd := m[2]
		// Skip common non-command words
		if isCommonWord(cmd) {
			continue
		}
		if !seen[cmd] {
			seen[cmd] = true
			commands = append(commands, cmd)
		}
	}
	return commands
}

func isCommonWord(w string) bool {
	common := map[string]bool{
		"the": true, "and": true, "for": true, "with": true, "that": true,
		"this": true, "from": true, "are": true, "was": true, "will": true,
		"can": true, "has": true, "have": true, "not": true, "but": true,
		"all": true, "your": true, "you": true, "use": true, "how": true,
		"about": true, "more": true, "when": true, "into": true,
	}
	return common[strings.ToLower(w)]
}

// containsPainSignal checks if text contains signals of user frustration.
func containsPainSignal(text string) bool {
	lower := strings.ToLower(text)
	signals := []string{
		"bug", "broken", "crash", "error", "fail", "slow",
		"confusing", "unclear", "missing", "wrong", "doesn't work",
		"not working", "can't", "cannot", "won't", "frustrat",
	}
	for _, s := range signals {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// analyzeCompetitorRepoLLM uses an LLM to deeply analyze a competitor's README
// and issue titles, extracting richer intelligence than regex alone.
func analyzeCompetitorRepoLLM(owner, repo, readmeContent string, issueTitles []string) (*CompetitorAnalysis, error) {
	prompt := BuildCompetitorPrompt(owner, repo, readmeContent, issueTitles)
	response, err := llm.Run(prompt)
	if err != nil {
		return nil, err
	}
	return parseCompetitorResponse(response, owner, repo)
}

// BuildCompetitorPrompt constructs a prompt for LLM-based competitor analysis.
func BuildCompetitorPrompt(owner, repo, readmeContent string, issueTitles []string) string {
	// Truncate README to avoid exceeding context
	if len(readmeContent) > 20000 {
		readmeContent = readmeContent[:20000]
	}

	var issueList strings.Builder
	for i, title := range issueTitles {
		if i >= 20 {
			break
		}
		fmt.Fprintf(&issueList, "- %s\n", title)
	}

	return fmt.Sprintf(`Analyze this CLI tool repository and extract intelligence.

Repository: %s/%s

README:
%s

Open Issues/Feature Requests:
%s

Respond with ONLY a JSON object (no markdown fences) in this exact format:
{
  "commands_found": ["list", "create", "get", "delete"],
  "feature_requests": ["requested feature 1", "requested feature 2"],
  "pain_points": ["pain point 1", "pain point 2"],
  "auth_method": "api_key or oauth2 or bearer_token or none",
  "output_formats": ["json", "table", "csv"],
  "install_method": "brew or npm or pip or cargo or binary"
}

Rules:
- commands_found: Extract ALL CLI subcommands mentioned in the README
- feature_requests: Summarize unmet user needs from issues
- pain_points: Identify user frustrations, bugs, UX issues
- Be specific and actionable in your analysis`, owner, repo, readmeContent, issueList.String())
}

// parseCompetitorResponse parses the LLM JSON response into a CompetitorAnalysis.
func parseCompetitorResponse(response, owner, repo string) (*CompetitorAnalysis, error) {
	// Extract JSON from response (might be wrapped in markdown fences)
	jsonStr := extractJSONObject(response)

	var parsed struct {
		CommandsFound   []string `json:"commands_found"`
		FeatureRequests []string `json:"feature_requests"`
		PainPoints      []string `json:"pain_points"`
		AuthMethod      string   `json:"auth_method"`
		OutputFormats   []string `json:"output_formats"`
		InstallMethod   string   `json:"install_method"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil, fmt.Errorf("parsing LLM response: %w", err)
	}

	return &CompetitorAnalysis{
		RepoURL:         fmt.Sprintf("https://github.com/%s/%s", owner, repo),
		CommandsFound:   parsed.CommandsFound,
		CommandCount:    len(parsed.CommandsFound),
		FeatureRequests: parsed.FeatureRequests,
		PainPoints:      parsed.PainPoints,
	}, nil
}

// extractJSONObject finds and returns the first JSON object in the string.
func extractJSONObject(s string) string {
	start := -1
	depth := 0
	for i, c := range s {
		if c == '{' {
			if depth == 0 {
				start = i
			}
			depth++
		}
		if c == '}' {
			depth--
			if depth == 0 && start >= 0 {
				return s[start : i+1]
			}
		}
	}
	return "{}"
}

// synthesizeInsights aggregates competitor analyses into actionable insights.
func synthesizeInsights(analyses []CompetitorAnalysis) CompetitorInsights {
	insights := CompetitorInsights{
		Analyses: analyses,
	}

	// Find max command count and set target to 1.2x
	maxCommands := 0
	for _, a := range analyses {
		if a.CommandCount > maxCommands {
			maxCommands = a.CommandCount
		}
	}
	insights.CommandTarget = int(math.Ceil(float64(maxCommands) * 1.2))

	// Collect all unique feature requests as unmet features
	seenFeatures := make(map[string]bool)
	for _, a := range analyses {
		for _, f := range a.FeatureRequests {
			lower := strings.ToLower(f)
			if !seenFeatures[lower] {
				seenFeatures[lower] = true
				insights.UnmetFeatures = append(insights.UnmetFeatures, f)
			}
		}
	}

	// Collect all unique pain points
	seenPains := make(map[string]bool)
	for _, a := range analyses {
		for _, p := range a.PainPoints {
			lower := strings.ToLower(p)
			if !seenPains[lower] {
				seenPains[lower] = true
				insights.PainPointsToAvoid = append(insights.PainPointsToAvoid, p)
			}
		}
	}

	return insights
}
