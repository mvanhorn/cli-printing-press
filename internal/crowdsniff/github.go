package crowdsniff

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// GitHubOptions configures the GitHub code search source.
type GitHubOptions struct {
	BaseURL       string        // defaults to "https://api.github.com"
	HTTPClient    *http.Client  // defaults to 15s timeout client
	Token         string        // defaults to os.Getenv("GITHUB_TOKEN")
	RecencyCutoff time.Duration // defaults to 180 days
}

// GitHubSource discovers API endpoints by searching GitHub code.
type GitHubSource struct {
	baseURL       string
	client        *http.Client
	token         string
	recencyCutoff time.Duration
}

// NewGitHubSource creates a new GitHub code search discovery source.
func NewGitHubSource(opts GitHubOptions) *GitHubSource {
	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	token := opts.Token
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	cutoff := opts.RecencyCutoff
	if cutoff == 0 {
		cutoff = 180 * 24 * time.Hour
	}
	return &GitHubSource{
		baseURL:       strings.TrimRight(baseURL, "/"),
		client:        client,
		token:         token,
		recencyCutoff: cutoff,
	}
}

// Discover searches GitHub code for references to the given API and returns
// discovered endpoints. If no token is configured, it returns an empty result
// immediately (graceful degradation).
func (g *GitHubSource) Discover(ctx context.Context, apiName string) (SourceResult, error) {
	if g.token == "" {
		return SourceResult{}, nil
	}

	queries := buildSearchQueries(apiName)

	var allItems []codeSearchItem
	ticker := time.NewTicker(6 * time.Second) // 10 req/min rate limit
	defer ticker.Stop()
	firstRequest := true

	for _, query := range queries {
		for page := 1; page <= 10; page++ {
			// Rate limit: wait before each request (skip the first one).
			if !firstRequest {
				select {
				case <-ctx.Done():
					return buildResult(allItems, g, ctx), ctx.Err()
				case <-ticker.C:
				}
			}
			firstRequest = false

			items, total, err := g.searchCode(ctx, query, page)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: github code search page %d: %v\n", page, err)
				break // move to next query
			}
			allItems = append(allItems, items...)

			// Stop if we've seen all results for this query.
			if page*100 >= total {
				break
			}
		}
	}

	return buildResult(allItems, g, ctx), nil
}

// buildSearchQueries returns the GitHub code search queries for the given API name.
// Domain-like names (containing ".") get exact domain queries; plain names get
// a broader fallback.
func buildSearchQueries(apiName string) []string {
	if strings.Contains(apiName, ".") {
		return []string{
			fmt.Sprintf(`"%s" language:javascript`, apiName),
			fmt.Sprintf(`"%s" language:python`, apiName),
		}
	}
	return []string{
		fmt.Sprintf(`"%s" api language:javascript`, apiName),
		fmt.Sprintf(`"%s" api language:python`, apiName),
	}
}

// searchCode performs a single code search request and returns items + total count.
func (g *GitHubSource) searchCode(ctx context.Context, query string, page int) ([]codeSearchItem, int, error) {
	u := fmt.Sprintf("%s/search/code?q=%s&per_page=100&page=%d",
		g.baseURL, url.QueryEscape(query), page)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/vnd.github.text-match+json")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("code search returned %d", resp.StatusCode)
	}

	var result codeSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, 0, err
	}
	return result.Items, result.TotalCount, nil
}

// fetchRepoPushedAt gets the pushed_at timestamp for a repo.
func (g *GitHubSource) fetchRepoPushedAt(ctx context.Context, fullName string) (time.Time, error) {
	u := fmt.Sprintf("%s/repos/%s", g.baseURL, fullName)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return time.Time{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}

	resp, err := g.client.Do(req)
	if err != nil {
		return time.Time{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("repo fetch returned %d", resp.StatusCode)
	}

	var repo repoResponse
	if err := json.NewDecoder(resp.Body).Decode(&repo); err != nil {
		return time.Time{}, err
	}

	t, err := time.Parse(time.RFC3339, repo.PushedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing pushed_at: %w", err)
	}
	return t, nil
}

// buildResult takes raw code search items, filters by repo freshness, extracts
// endpoints and base URL candidates.
func buildResult(items []codeSearchItem, g *GitHubSource, ctx context.Context) SourceResult {
	if len(items) == 0 {
		return SourceResult{}
	}

	// Collect unique repos.
	repos := make(map[string]struct{})
	for _, item := range items {
		repos[item.Repository.FullName] = struct{}{}
	}

	// Check repo freshness.
	freshRepos := make(map[string]bool)
	cutoff := time.Now().Add(-g.recencyCutoff)
	for fullName := range repos {
		pushed, err := g.fetchRepoPushedAt(ctx, fullName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: checking repo %s freshness: %v\n", fullName, err)
			continue // skip this repo, don't include its endpoints
		}
		freshRepos[fullName] = pushed.After(cutoff)
	}

	// Extract endpoints from fresh repos only and track domain frequency.
	domainCounts := make(map[string]int)
	var endpoints []DiscoveredEndpoint

	// Track which repo contributed which endpoint to count per-repo frequency.
	type endpointKey struct {
		method string
		path   string
	}
	repoEndpoints := make(map[endpointKey]map[string]struct{})

	for _, item := range items {
		repo := item.Repository.FullName
		if !freshRepos[repo] {
			continue
		}

		for _, tm := range item.TextMatches {
			extracted := extractEndpointsFromTextMatches(tm.Fragment)
			for _, ep := range extracted {
				key := endpointKey{method: ep.method, path: ep.path}
				if _, ok := repoEndpoints[key]; !ok {
					repoEndpoints[key] = make(map[string]struct{})
				}
				repoEndpoints[key][repo] = struct{}{}

				if ep.domain != "" {
					domainCounts[ep.domain]++
				}
			}
		}
	}

	// Convert to DiscoveredEndpoint, deduplicating across repos.
	seen := make(map[endpointKey]struct{})
	for key := range repoEndpoints {
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		method := key.method
		if method == "" {
			method = "GET"
		}
		endpoints = append(endpoints, DiscoveredEndpoint{
			Method:     method,
			Path:       key.path,
			SourceTier: TierCodeSearch,
			SourceName: "github-code-search",
		})
	}

	// Build base URL candidates from most frequent domain.
	var baseURLs []string
	if len(domainCounts) > 0 {
		var bestDomain string
		var bestCount int
		for domain, count := range domainCounts {
			if count > bestCount {
				bestDomain = domain
				bestCount = count
			}
		}
		if bestDomain != "" {
			baseURLs = append(baseURLs, "https://"+bestDomain)
		}
	}

	return SourceResult{
		Endpoints:         endpoints,
		BaseURLCandidates: baseURLs,
	}
}

// extractedEndpoint is a raw endpoint extracted from text match fragments.
type extractedEndpoint struct {
	method string
	path   string
	domain string
}

// urlPathPattern matches URL paths like /v1/users, /api/projects, /v2/databases/{id}
var urlPathPattern = regexp.MustCompile(`(?:https?://([a-zA-Z0-9._-]+))?(/(?:v\d+|api)/[a-zA-Z0-9/_{}:.-]+)`)

// standalonePathPattern matches standalone paths like '/v1/users' that aren't part of a full URL.
var standalonePathPattern = regexp.MustCompile(`['"](/(?:v\d+|api)/[a-zA-Z0-9/_{}:.-]+)['"]`)

// httpMethodPattern matches HTTP method hints near URL patterns.
var httpMethodPattern = regexp.MustCompile(`(?i)\b(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\b`)

// extractEndpointsFromTextMatches parses a text match fragment and extracts
// URL path patterns and associated HTTP methods.
func extractEndpointsFromTextMatches(fragment string) []extractedEndpoint {
	var results []extractedEndpoint
	seen := make(map[string]struct{})

	// Try full URL patterns first (with domain).
	for _, match := range urlPathPattern.FindAllStringSubmatch(fragment, -1) {
		domain := match[1]
		path := cleanExtractedPath(match[2])
		if path == "" {
			continue
		}

		key := path
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}

		method := inferMethod(fragment, match[0])
		results = append(results, extractedEndpoint{
			method: method,
			path:   path,
			domain: domain,
		})
	}

	// Also try standalone paths not already captured.
	for _, match := range standalonePathPattern.FindAllStringSubmatch(fragment, -1) {
		path := cleanExtractedPath(match[1])
		if path == "" {
			continue
		}

		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}

		method := inferMethod(fragment, match[0])
		results = append(results, extractedEndpoint{
			method: method,
			path:   path,
		})
	}

	return results
}

// cleanExtractedPath trims trailing punctuation and normalizes the path.
func cleanExtractedPath(path string) string {
	// Trim common trailing characters that are regex artifacts.
	path = strings.TrimRight(path, ".'\")")
	// Must start with /.
	if !strings.HasPrefix(path, "/") {
		return ""
	}
	// Filter out paths that are too short to be meaningful.
	if len(path) < 3 {
		return ""
	}
	return path
}

// inferMethod tries to find an HTTP method keyword near the URL in the fragment.
func inferMethod(fragment, urlMatch string) string {
	// Look for a method keyword in the same fragment.
	idx := strings.Index(fragment, urlMatch)
	if idx < 0 {
		return ""
	}

	// Check a window before the URL match for method keywords.
	start := max(idx-80, 0)
	window := fragment[start : idx+len(urlMatch)]

	matches := httpMethodPattern.FindAllString(window, -1)
	if len(matches) > 0 {
		return strings.ToUpper(matches[len(matches)-1])
	}
	return ""
}

// --- Response types (unexported) ---

type codeSearchResponse struct {
	TotalCount int              `json:"total_count"`
	Items      []codeSearchItem `json:"items"`
}

type codeSearchItem struct {
	Name        string         `json:"name"`
	Path        string         `json:"path"`
	HTMLURL     string         `json:"html_url"`
	Repository  codeSearchRepo `json:"repository"`
	TextMatches []textMatch    `json:"text_matches"`
}

type codeSearchRepo struct {
	FullName string `json:"full_name"`
}

type textMatch struct {
	Fragment string `json:"fragment"`
}

type repoResponse struct {
	PushedAt string `json:"pushed_at"`
}
