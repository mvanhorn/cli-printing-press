package crowdsniff

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	defaultRegistryBaseURL  = "https://registry.npmjs.org"
	defaultDownloadsBaseURL = "https://api.npmjs.org"
	defaultRecencyCutoff    = 180 * 24 * time.Hour // 6 months
	defaultHTTPTimeout      = 15 * time.Second
	maxTarballSize          = 10 * 1024 * 1024 // 10 MB
	maxSearchResults        = 25
	maxPackagesToProcess    = 10
	maxBulkDownloadPackages = 128
	maxReadmeSize           = 100 * 1024 // 100 KB
)

// NPMOptions configures the NPM source.
type NPMOptions struct {
	RegistryBaseURL  string
	DownloadsBaseURL string
	HTTPClient       *http.Client
	RecencyCutoff    time.Duration
}

// NPMSource discovers API endpoints by searching the npm registry,
// downloading SDK tarballs, and grepping source code for patterns.
type NPMSource struct {
	registryBaseURL  string
	downloadsBaseURL string
	httpClient       *http.Client
	recencyCutoff    time.Duration
}

// NewNPMSource creates an NPMSource with the given options.
func NewNPMSource(opts NPMOptions) *NPMSource {
	registry := opts.RegistryBaseURL
	if registry == "" {
		registry = defaultRegistryBaseURL
	}
	downloads := opts.DownloadsBaseURL
	if downloads == "" {
		downloads = defaultDownloadsBaseURL
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	cutoff := opts.RecencyCutoff
	if cutoff == 0 {
		cutoff = defaultRecencyCutoff
	}
	return &NPMSource{
		registryBaseURL:  strings.TrimRight(registry, "/"),
		downloadsBaseURL: strings.TrimRight(downloads, "/"),
		httpClient:       client,
		recencyCutoff:    cutoff,
	}
}

// npmSearchResponse represents the npm registry search API response.
type npmSearchResponse struct {
	Objects []npmSearchObject `json:"objects"`
}

type npmSearchObject struct {
	Package npmPackageInfo `json:"package"`
}

type npmPackageInfo struct {
	Name    string       `json:"name"`
	Scope   string       `json:"scope"`
	Version string       `json:"version"`
	Date    time.Time    `json:"date"`
	Links   npmLinks     `json:"links"`
	Dist    *npmDistInfo `json:"dist,omitempty"`
}

type npmLinks struct {
	NPM string `json:"npm"`
}

// npmPackageVersion represents the response from GET /<pkg>/<version>.
type npmPackageVersion struct {
	Dist npmDistInfo `json:"dist"`
}

type npmDistInfo struct {
	Tarball string `json:"tarball"`
}

// npmDownloadsResponse represents the npm downloads API response.
type npmDownloadsResponse struct {
	Downloads int    `json:"downloads"`
	Package   string `json:"package"`
}

// npmBulkDownloadsResponse maps package names to download counts.
type npmBulkDownloadsResponse map[string]*npmDownloadsResponse

// Discover searches npm for packages related to the given API name,
// downloads their source code, and greps for endpoint patterns.
func (s *NPMSource) Discover(ctx context.Context, apiName string) (SourceResult, error) {
	var result SourceResult

	// Step 1: Search the registry.
	packages, err := s.search(ctx, apiName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crowd-sniff: npm search failed: %v\n", err)
		return result, nil
	}
	if len(packages) == 0 {
		return result, nil
	}

	// Step 2: Filter by recency.
	cutoffTime := time.Now().Add(-s.recencyCutoff)
	var recent []npmPackageInfo
	for _, pkg := range packages {
		if pkg.Date.After(cutoffTime) {
			recent = append(recent, pkg)
		}
	}
	if len(recent) == 0 {
		return result, nil
	}

	// Step 3: Take top N packages.
	if len(recent) > maxPackagesToProcess {
		recent = recent[:maxPackagesToProcess]
	}

	// Step 4: Fetch download counts (non-fatal).
	downloads := s.fetchDownloads(ctx, recent)

	// Step 5: Process each package.
	apiNameLower := strings.ToLower(apiName)
	for _, pkg := range recent {
		tier := classifyPackage(pkg, apiNameLower)

		// Fetch tarball URL from package metadata.
		tarballURL, fetchErr := s.fetchTarballURL(ctx, pkg.Name, pkg.Version)
		if fetchErr != nil {
			fmt.Fprintf(os.Stderr, "crowd-sniff: failed to get tarball URL for %s: %v\n", pkg.Name, fetchErr)
			continue
		}

		// Download and extract tarball.
		endpoints, baseURLs, authPatterns, processErr := s.processPackageTarball(ctx, tarballURL, pkg.Name, tier, apiName, downloads[pkg.Name])
		if processErr != nil {
			fmt.Fprintf(os.Stderr, "crowd-sniff: failed to process %s: %v\n", pkg.Name, processErr)
			continue
		}

		result.Endpoints = append(result.Endpoints, endpoints...)
		result.BaseURLCandidates = append(result.BaseURLCandidates, baseURLs...)
		result.Auth = append(result.Auth, authPatterns...)
	}

	return result, nil
}

// search queries the npm registry search API.
func (s *NPMSource) search(ctx context.Context, query string) ([]npmPackageInfo, error) {
	u := fmt.Sprintf("%s/-/v1/search?text=%s&size=%d", s.registryBaseURL, url.QueryEscape(query), maxSearchResults)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating search request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing search: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search returned status %d", resp.StatusCode)
	}

	var searchResp npmSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("decoding search response: %w", err)
	}

	packages := make([]npmPackageInfo, 0, len(searchResp.Objects))
	for _, obj := range searchResp.Objects {
		packages = append(packages, obj.Package)
	}
	return packages, nil
}

// fetchDownloads fetches weekly download counts for packages.
// Returns a map of package name -> download count. Errors are non-fatal.
func (s *NPMSource) fetchDownloads(ctx context.Context, packages []npmPackageInfo) map[string]int {
	result := make(map[string]int)

	// Build the bulk request (up to 128 packages).
	names := make([]string, 0, len(packages))
	for _, pkg := range packages {
		if len(names) >= maxBulkDownloadPackages {
			break
		}
		names = append(names, pkg.Name)
	}

	if len(names) == 0 {
		return result
	}

	// npm bulk downloads API: GET /downloads/point/last-week/<pkg1>,<pkg2>,...
	u := fmt.Sprintf("%s/downloads/point/last-week/%s", s.downloadsBaseURL, strings.Join(names, ","))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crowd-sniff: failed to create downloads request: %v\n", err)
		return result
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "crowd-sniff: downloads request failed: %v\n", err)
		return result
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "crowd-sniff: downloads API returned status %d\n", resp.StatusCode)
		return result
	}

	var bulk npmBulkDownloadsResponse
	if err := json.NewDecoder(resp.Body).Decode(&bulk); err != nil {
		fmt.Fprintf(os.Stderr, "crowd-sniff: failed to decode downloads response: %v\n", err)
		return result
	}

	for name, data := range bulk {
		if data != nil {
			result[name] = data.Downloads
		}
	}
	return result
}

// fetchTarballURL gets the tarball download URL for a specific package version.
func (s *NPMSource) fetchTarballURL(ctx context.Context, name, version string) (string, error) {
	u := fmt.Sprintf("%s/%s/%s", s.registryBaseURL, url.PathEscape(name), url.PathEscape(version))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("creating version request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching version metadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("version metadata returned status %d", resp.StatusCode)
	}

	var version_ npmPackageVersion
	if err := json.NewDecoder(resp.Body).Decode(&version_); err != nil {
		return "", fmt.Errorf("decoding version metadata: %w", err)
	}

	if version_.Dist.Tarball == "" {
		return "", fmt.Errorf("no tarball URL in version metadata")
	}

	return version_.Dist.Tarball, nil
}

// processPackageTarball downloads a tarball, extracts it, and greps for endpoints, auth patterns, and base URLs.
func (s *NPMSource) processPackageTarball(ctx context.Context, tarballURL, pkgName, tier, apiName string, weeklyDownloads int) ([]DiscoveredEndpoint, []string, []DiscoveredAuth, error) {
	// Security: validate tarball URL is HTTPS.
	parsed, err := url.Parse(tarballURL)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("invalid tarball URL: %w", err)
	}
	if parsed.Scheme != "https" {
		return nil, nil, nil, fmt.Errorf("tarball URL must be HTTPS, got %s", parsed.Scheme)
	}

	// Download tarball.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tarballURL, nil)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("creating tarball request: %w", err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("downloading tarball: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, nil, fmt.Errorf("tarball download returned status %d", resp.StatusCode)
	}

	// Create temp directory.
	tmpDir, err := os.MkdirTemp("", "crowd-sniff-npm-*")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Extract tarball with size limit.
	if err := extractTarball(resp.Body, tmpDir); err != nil {
		return nil, nil, nil, fmt.Errorf("extracting tarball: %w", err)
	}

	// Grep extracted files for endpoint patterns and auth patterns.
	var allEndpoints []DiscoveredEndpoint
	var allBaseURLs []string
	var allContent strings.Builder

	_ = filepath.Walk(tmpDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if info.IsDir() {
			return nil
		}

		// Only grep JS/TS files.
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".js" && ext != ".ts" && ext != ".mjs" {
			return nil
		}

		// Skip declaration files and test files.
		base := filepath.Base(path)
		if strings.HasSuffix(base, ".d.ts") || strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") {
			return nil
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil // skip unreadable files
		}

		contentStr := string(content)
		endpoints, baseURLs := GrepEndpoints(contentStr, pkgName, tier)
		endpoints = EnrichWithParams(contentStr, endpoints)
		allEndpoints = append(allEndpoints, endpoints...)
		allBaseURLs = append(allBaseURLs, baseURLs...)
		allContent.WriteString(contentStr)
		allContent.WriteByte('\n')
		return nil
	})

	// Adjust tier for low-download packages (still community, but we note it).
	_ = weeklyDownloads // Used for future priority sorting; tier stays the same.

	// Extract auth patterns from the combined source content.
	authPatterns := GrepAuth(allContent.String(), tier, apiName)

	// Read README.md from the package root for key URL hints.
	// npm tarballs always have package/README.md.
	readmePath := filepath.Join(tmpDir, "package", "README.md")
	if readmeContent, readErr := readFileCapped(readmePath, maxReadmeSize); readErr == nil && len(readmeContent) > 0 {
		if keyURL := extractKeyURL(string(readmeContent), apiName); keyURL != "" {
			for i := range authPatterns {
				if authPatterns[i].KeyURLHint == "" {
					authPatterns[i].KeyURLHint = keyURL
				}
			}
		}
	}

	return allEndpoints, allBaseURLs, authPatterns, nil
}

// extractTarball extracts a gzipped tar archive to the destination directory.
// Security: rejects symlinks, hard links, path traversal, and limits total size.
func extractTarball(r io.Reader, destDir string) error {
	// Limit total bytes read.
	limited := io.LimitReader(r, maxTarballSize)

	gz, err := gzip.NewReader(limited)
	if err != nil {
		return fmt.Errorf("opening gzip reader: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("resolving dest dir: %w", err)
	}

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		// Security: reject symlinks and hard links.
		if header.Typeflag == tar.TypeSymlink || header.Typeflag == tar.TypeLink {
			continue // skip, don't error — other files may be fine
		}

		// Only process regular files and directories.
		if header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeDir {
			continue
		}

		// Security: sanitize path and prevent traversal.
		target := filepath.Join(absDestDir, filepath.Clean(header.Name))
		absTarget, err := filepath.Abs(target)
		if err != nil {
			continue
		}
		if !strings.HasPrefix(absTarget, absDestDir+string(filepath.Separator)) && absTarget != absDestDir {
			continue // path traversal attempt
		}

		if header.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(absTarget, 0o755); err != nil {
				return fmt.Errorf("creating directory %s: %w", header.Name, err)
			}
			continue
		}

		// Ensure parent directory exists.
		parentDir := filepath.Dir(absTarget)
		if err := os.MkdirAll(parentDir, 0o755); err != nil {
			return fmt.Errorf("creating parent dir for %s: %w", header.Name, err)
		}

		f, err := os.Create(absTarget)
		if err != nil {
			return fmt.Errorf("creating file %s: %w", header.Name, err)
		}

		// Copy with size limit per file (same global limit via LimitReader on outer reader).
		if _, err := io.Copy(f, tr); err != nil {
			_ = f.Close()
			return fmt.Errorf("writing file %s: %w", header.Name, err)
		}
		_ = f.Close()
	}

	return nil
}

// classifyPackage determines whether a package is an official or community SDK.
// A package is considered official if its npm scope matches the API vendor name.
func classifyPackage(pkg npmPackageInfo, apiNameLower string) string {
	scope := strings.TrimPrefix(pkg.Scope, "@")
	scope = strings.ToLower(scope)

	if scope != "" && (scope == apiNameLower ||
		strings.Contains(scope, apiNameLower) ||
		strings.Contains(apiNameLower, scope)) {
		return TierOfficialSDK
	}

	// Also check the package name itself for official-looking names.
	nameLower := strings.ToLower(pkg.Name)
	if strings.HasPrefix(nameLower, "@"+apiNameLower+"/") {
		return TierOfficialSDK
	}

	return TierCommunitySDK
}

// --- Auth pattern detection ---

var (
	// bearerHeaderPattern matches Bearer token auth in headers.
	// Examples:
	//   headers['Authorization'] = 'Bearer ' + token
	//   Authorization: `Bearer ${token}`
	//   headers.Authorization = `Bearer ${this.token}`
	//   'Authorization': 'Bearer ' + apiKey
	bearerHeaderPattern = regexp.MustCompile(`(?i)(?:headers\s*[\[.]\s*['"]?Authorization['"]?\s*[\])]?\s*=|['"]Authorization['"]\s*:)\s*['` + "`" + `]?\s*Bearer\b`)

	// bearerTemplatePattern matches template literal Bearer patterns.
	// Examples:
	//   `Bearer ${token}`
	//   `Bearer ${this.apiKey}`
	bearerTemplatePattern = regexp.MustCompile(`(?i)Bearer\s+\$\{`)

	// xApiKeyHeaderPattern matches X-Api-Key style headers.
	// Examples:
	//   headers['X-Api-Key'] = apiKey
	//   'X-Api-Key': this.key
	//   headers.set('x-api-key', key)
	xApiKeyHeaderPattern = regexp.MustCompile(`(?i)(?:headers\s*[\[.]\s*)?['"]X-Api-Key['"]\s*[\])]?\s*[:=]`)

	// queryKeyAuthPattern matches API key passed as a query parameter named "key".
	// Examples:
	//   params.key = this.apiKey
	//   query.key = apiKey
	//   { key: this.apiKey }
	//   'key': this.apiKey
	//   ?key=${apiKey}
	//   ?key=' + this.key
	queryKeyAuthPattern = regexp.MustCompile(`(?i)(?:params|query)\s*\.\s*key\s*=|(?:\bkey\b\s*:\s*(?:this\s*\.\s*)?(?:api_?[Kk]ey|key)\b)|(?:['"]key['"]\s*:\s*(?:this\s*\.\s*)?(?:api_?[Kk]ey|key))|[?&]key\s*=\s*['` + "`" + `]\s*\+?\s*\$?\{?`)

	// envVarHintPattern matches environment variable references that suggest auth.
	// Examples:
	//   process.env.STEAM_API_KEY
	//   process.env.API_KEY
	//   process.env['NOTION_API_KEY']
	envVarHintPattern = regexp.MustCompile(`process\.env\s*(?:\.\s*([A-Z][A-Z0-9_]*(?:API|KEY|TOKEN|SECRET)[A-Z0-9_]*)|\[\s*['"]([A-Z][A-Z0-9_]*(?:API|KEY|TOKEN|SECRET)[A-Z0-9_]*)['"]\s*\])`)
)

// GrepAuth scans SDK source code for authentication patterns and returns
// any detected auth configurations. Designed for high precision — it only
// reports patterns that are clearly auth-related. False negatives are
// acceptable; false positives are not.
func GrepAuth(content string, sourceTier string, apiName string) []DiscoveredAuth {
	var auths []DiscoveredAuth
	seen := make(map[string]bool) // dedup by Type+In+Header

	// Check for Bearer token auth.
	if bearerHeaderPattern.MatchString(content) || bearerTemplatePattern.MatchString(content) {
		key := "bearer_token:header:Authorization"
		if !seen[key] {
			seen[key] = true
			auths = append(auths, DiscoveredAuth{
				Type:       "bearer_token",
				Header:     "Authorization",
				In:         "header",
				Format:     "Bearer {token}",
				SourceTier: sourceTier,
			})
		}
	}

	// Check for X-Api-Key header auth.
	if xApiKeyHeaderPattern.MatchString(content) {
		key := "api_key:header:X-Api-Key"
		if !seen[key] {
			seen[key] = true
			auths = append(auths, DiscoveredAuth{
				Type:       "api_key",
				Header:     "X-Api-Key",
				In:         "header",
				SourceTier: sourceTier,
			})
		}
	}

	// Check for query param "key" auth.
	if queryKeyAuthPattern.MatchString(content) {
		key := "api_key:query:key"
		if !seen[key] {
			seen[key] = true
			auths = append(auths, DiscoveredAuth{
				Type:       "api_key",
				Header:     "key",
				In:         "query",
				SourceTier: sourceTier,
			})
		}
	}

	// Look for env var hints.
	envVarHint := extractEnvVarHint(content, apiName)

	// Apply env var hint to the first detected auth.
	if envVarHint != "" && len(auths) > 0 {
		auths[0].EnvVarHint = envVarHint
	}

	return auths
}

// extractEnvVarHint scans content for process.env references that look
// auth-related. Prefers env vars containing the API name (e.g., STEAM_API_KEY
// for api "steam") over generic matches (e.g., COOKIE_SECRET). Returns empty
// string if no API-name-relevant hint is found — the caller falls back to
// deriveEnvVar() which generates a sensible default from the API name.
func extractEnvVarHint(content string, apiName string) string {
	matches := envVarHintPattern.FindAllStringSubmatch(content, -1)
	upperAPI := strings.ToUpper(strings.ReplaceAll(apiName, ".", "_"))

	var candidates []string
	for _, m := range matches {
		var name string
		if m[1] != "" {
			name = m[1]
		} else if m[2] != "" {
			name = m[2]
		}
		if name != "" {
			candidates = append(candidates, name)
		}
	}

	// Prefer candidates containing the API name.
	for _, c := range candidates {
		if upperAPI != "" && strings.Contains(c, upperAPI) {
			return c
		}
	}

	// No API-name-relevant match — return empty to let deriveEnvVar() handle it.
	return ""
}

// --- README key URL extraction ---

var (
	// keyURLPattern matches phrases that point to an API key registration page.
	// Captures the HTTPS URL following the phrase.
	// Uses [^")\s]+ for URL capture to avoid nested .* backtracking.
	keyURLPattern = regexp.MustCompile(`(?i)(?:get\s+(?:your\s+)?(?:api\s+)?key\s+(?:at|from)|register\s+at|sign\s*up\s+at|get\s+(?:an?\s+)?api\s+key\s+(?:at|from)|create\s+(?:your\s+)?(?:api\s+)?key\s+at|developer\s+portal\s*:)\s*(https://[^")\s]+)`)

	// markdownLinkKeyURLPattern matches markdown links with key-related anchor text.
	// E.g., [Get your API key](https://example.com/keys)
	markdownLinkKeyURLPattern = regexp.MustCompile(`(?i)\[(?:[^\]]*(?:api\s*key|register|sign\s*up|developer\s+portal|get\s+(?:your\s+)?key)[^\]]*)\]\((https://[^)]+)\)`)

	// badgeURLDomains are domains used for npm badges — never useful as key URLs.
	badgeURLDomains = []string{
		"img.shields.io",
		"badge.fury.io",
		"badges.greenkeeper.io",
		"david-dm.org",
		"codecov.io",
		"coveralls.io",
		"travis-ci.org",
		"circleci.com",
		"github.com/workflows",
	}

	// knownDevPlatformPrefixes are domain prefixes for well-known developer platforms.
	knownDevPlatformPrefixes = []string{
		"developer.",
		"console.cloud.google.com",
		"console.aws.amazon.com",
		"dash.cloudflare.com",
		"portal.azure.com",
		"app.posthog.com",
	}
)

// extractKeyURL scans README content for patterns like "Get your API key at [url]"
// and returns the first HTTPS URL that either contains the API name or is on a
// known developer platform domain. Returns empty string if no suitable URL is found.
func extractKeyURL(readmeContent string, apiName string) string {
	apiNameLower := strings.ToLower(apiName)

	var candidates []string

	// Collect URLs from plain-text patterns.
	for _, m := range keyURLPattern.FindAllStringSubmatch(readmeContent, -1) {
		if len(m) > 1 {
			candidates = append(candidates, strings.TrimRight(m[1], ".,;:"))
		}
	}

	// Collect URLs from markdown link patterns.
	for _, m := range markdownLinkKeyURLPattern.FindAllStringSubmatch(readmeContent, -1) {
		if len(m) > 1 {
			candidates = append(candidates, strings.TrimRight(m[1], ".,;:"))
		}
	}

	for _, rawURL := range candidates {
		if !strings.HasPrefix(rawURL, "https://") {
			continue
		}

		parsed, err := url.Parse(rawURL)
		if err != nil {
			continue
		}
		host := strings.ToLower(parsed.Hostname())

		// Reject badge URLs.
		if isBadgeURL(host) {
			continue
		}

		// Reject GitHub repo URLs that are just the package's own repo
		// (github.com/<owner>/<repo> without /wiki, /blob, etc. pointing to docs).
		if isPackageRepoURL(host, parsed.Path) {
			continue
		}

		// Accept if the URL contains the API name (case-insensitive).
		if apiNameLower != "" && strings.Contains(strings.ToLower(rawURL), apiNameLower) {
			return rawURL
		}

		// Accept if on a known developer platform domain.
		if isKnownDevPlatform(host) {
			return rawURL
		}
	}

	return ""
}

// isBadgeURL returns true if the host is a known npm badge domain.
func isBadgeURL(host string) bool {
	for _, badge := range badgeURLDomains {
		if host == badge || strings.HasSuffix(host, "."+badge) {
			return true
		}
	}
	return false
}

// isPackageRepoURL returns true if this looks like a plain GitHub repo URL
// (github.com/<owner>/<repo>) without a deeper path that might point to
// documentation or key pages.
func isPackageRepoURL(host, path string) bool {
	if host != "github.com" {
		return false
	}
	// Strip trailing slash and count path segments.
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return false
	}
	parts := strings.Split(trimmed, "/")
	// github.com/<owner>/<repo> (2 segments) is just the repo root — reject.
	// github.com/<owner>/<repo>/blob/... or /wiki/... may contain docs — allow.
	return len(parts) <= 2
}

// isKnownDevPlatform returns true if the host matches a well-known developer platform.
func isKnownDevPlatform(host string) bool {
	for _, prefix := range knownDevPlatformPrefixes {
		if strings.HasPrefix(host, prefix) || host == prefix {
			return true
		}
	}
	return false
}

// readFileCapped reads up to maxBytes from a file. Returns the content and any error.
// If the file is larger than maxBytes, the content is truncated (no error).
func readFileCapped(path string, maxBytes int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, maxBytes)
	n, err := io.ReadFull(f, buf)
	if err == io.ErrUnexpectedEOF || err == io.EOF {
		// File was smaller than maxBytes — that's fine.
		return buf[:n], nil
	}
	if err != nil {
		return nil, err
	}
	// File was exactly maxBytes or larger — return the capped content.
	return buf[:n], nil
}
