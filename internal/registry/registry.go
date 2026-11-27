package registry

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/cartridge"
)

const (
	DefaultRegistryURL    = "https://raw.githubusercontent.com/mvanhorn/printing-press-library/main/registry.json"
	DefaultReleaseBaseURL = "https://github.com/mvanhorn/printing-press-library/releases/download"
	DefaultSkillBaseURL   = "https://raw.githubusercontent.com/mvanhorn/printing-press-library/main/cli-skills"
	CacheTTL              = 6 * time.Hour
)

// Catalog mirrors printing-press-library's registry.json v2 schema.
type Catalog struct {
	SchemaVersion int     `json:"schema_version"`
	Entries       []Entry `json:"entries"`
}

// Entry represents one printed CLI in the Printing Press Library.
type Entry struct {
	Name        string       `json:"name"`
	Category    string       `json:"category"`
	API         string       `json:"api"`
	Description string       `json:"description"`
	Homepage    string       `json:"homepage,omitempty"`
	SearchTerms []string     `json:"search_terms,omitempty"`
	Path        string       `json:"path"`
	Release     *ReleaseInfo `json:"release,omitempty"`
	MCP         *MCPInfo     `json:"mcp,omitempty"`
	Printer     string       `json:"printer,omitempty"`
	PrinterName string       `json:"printer_name,omitempty"`
}

// ReleaseInfo describes the prebuilt binary release metadata for an entry.
type ReleaseInfo struct {
	CLIName      string `json:"cli_name"`
	Version      string `json:"version"`
	ReleasedAt   string `json:"released_at"`
	SourceCommit string `json:"source_commit"`
}

// MCPInfo summarizes an entry's MCP readiness and auth requirements.
type MCPInfo struct {
	Binary     string   `json:"binary"`
	Transports []string `json:"transports"`
	ToolCount  int      `json:"tool_count"`
	AuthType   string   `json:"auth_type"`
	EnvVars    []string `json:"env_vars"`
	MCPReady   string   `json:"mcp_ready"`
}

// Client manages catalog caching, search, URL fast-path routing, and zero-Go
// prebuilt binary installation.
type Client struct {
	RegistryURL    string
	ReleaseBaseURL string
	SkillBaseURL   string
	HTTPClient     *http.Client
	CachePath      string
	LocalRepoPath  string
}

// NewClient creates a registry client rooted under ~/.pp/cache/registry.json.
func NewClient() (*Client, error) {
	home, err := cartridge.HomeDir()
	if err != nil {
		return nil, err
	}
	userHome, _ := os.UserHomeDir()
	localRepo := ""
	if userHome != "" {
		candidate := filepath.Join(userHome, "printing-press-library", "registry.json")
		if _, err := os.Stat(candidate); err == nil {
			localRepo = candidate
		}
	}
	return &Client{
		RegistryURL:    DefaultRegistryURL,
		ReleaseBaseURL: DefaultReleaseBaseURL,
		SkillBaseURL:   DefaultSkillBaseURL,
		HTTPClient:     &http.Client{Timeout: 30 * time.Second},
		CachePath:      filepath.Join(home, "cache", "registry.json"),
		LocalRepoPath:  localRepo,
	}, nil
}

// Load fetches the catalog from fresh cache or GitHub raw URL, falling back to
// stale cache or a local printing-press-library clone only when offline.
func (c *Client) Load(ctx context.Context, forceRefresh bool) (*Catalog, error) {
	if !forceRefresh && c.CachePath != "" {
		if info, err := os.Stat(c.CachePath); err == nil && time.Since(info.ModTime()) < CacheTTL {
			if cat, err := readCatalogFile(c.CachePath); err == nil {
				return cat, nil
			}
		}
	}

	if c.RegistryURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.RegistryURL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.httpClient().Do(req)
		if err == nil {
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode == http.StatusOK {
				raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
				if readErr == nil {
					var cat Catalog
					if json.Unmarshal(raw, &cat) == nil && len(cat.Entries) > 0 {
						if c.CachePath != "" {
							_ = os.MkdirAll(filepath.Dir(c.CachePath), 0o700)
							_ = os.WriteFile(c.CachePath, raw, 0o600)
						}
						return &cat, nil
					}
				}
			}
		}
	}

	// Offline fallback to stale cache or local repo.
	if c.CachePath != "" {
		if cat, err := readCatalogFile(c.CachePath); err == nil {
			return cat, nil
		}
	}
	if c.LocalRepoPath != "" {
		if cat, err := readCatalogFile(c.LocalRepoPath); err == nil {
			return cat, nil
		}
	}
	return &Catalog{SchemaVersion: 2}, nil
}

// Search ranks catalog entries matching query and optional category.
func (c *Client) Search(cat *Catalog, query, category string, limit int) []Entry {
	if cat == nil {
		return nil
	}
	q := strings.ToLower(strings.TrimSpace(query))
	catFilter := strings.ToLower(strings.TrimSpace(category))

	type scored struct {
		entry Entry
		score int
	}
	var hits []scored
	for _, e := range cat.Entries {
		if catFilter != "" && strings.ToLower(e.Category) != catFilter {
			continue
		}
		if q == "" {
			hits = append(hits, scored{entry: e, score: 1})
			continue
		}
		score := scoreEntry(e, q)
		if score > 0 {
			hits = append(hits, scored{entry: e, score: score})
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].entry.Name < hits[j].entry.Name
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Entry, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.entry)
	}
	return out
}

func scoreEntry(e Entry, q string) int {
	name := strings.ToLower(e.Name)
	if name == q || name+"-pp-cli" == q {
		return 100
	}
	score := 0
	if strings.Contains(name, q) {
		score += 50
	}
	if strings.Contains(strings.ToLower(e.Homepage), q) {
		score += 35
	}
	if strings.Contains(strings.ToLower(e.Description), q) {
		score += 20
	}
	if strings.Contains(strings.ToLower(e.Category), q) {
		score += 15
	}
	for _, term := range e.SearchTerms {
		if strings.Contains(strings.ToLower(term), q) {
			score += 10
			break
		}
	}
	return score
}

// RouteDecision tells a browser/computer-use agent (Ardia, OpenCode Browse,
// Claude Code, OpenClaw, Hermes, Codex) whether to take the 50ms Printing Press
// fast path instead of opening a browser tab.
type RouteDecision struct {
	Matched          bool             `json:"matched"`
	Input            string           `json:"input"`
	Host             string           `json:"host,omitempty"`
	App              string           `json:"app,omitempty"`
	Mode             string           `json:"mode,omitempty"` // "cartridge", "prebuilt-binary", "browser-fallback"
	Installed        bool             `json:"installed"`
	Description      string           `json:"description,omitempty"`
	RecommendedCmd   string           `json:"recommended_cmd,omitempty"`
	InstallCmd       string           `json:"install_cmd,omitempty"`
	CompileCmd       string           `json:"compile_cmd,omitempty"`
	BrowserFallback  string           `json:"browser_fallback,omitempty"`
	EstimatedSavings *SavingsEstimate `json:"estimated_savings,omitempty"`
}

// SavingsEstimate gives agents concrete latency/token numbers for choosing PP over pixels.
type SavingsEstimate struct {
	BrowserMS     int `json:"browser_ms"`
	PPMS          int `json:"pp_ms"`
	BrowserTokens int `json:"browser_tokens"`
	PPTokens      int `json:"pp_tokens"`
}

// Route resolves a URL or intent string against installed cartridges, built-in
// cartridges, and the 616-app PPL registry in milliseconds.
func (c *Client) Route(ctx context.Context, input string) (*RouteDecision, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, errors.New("url or query is required")
	}

	host := extractHost(trimmed)
	fallback := fmt.Sprintf("ARDIA_BACKGROUND=1 ardia browse open %s", trimmed)
	if host == "" {
		fallback = "ARDIA_BACKGROUND=1 ardia browse open <url>"
	}

	// 1. Check installed user cartridges first.
	installed, _ := cartridge.Installed()
	for _, cart := range installed {
		if (host != "" && cart.MatchDomain(host)) || strings.EqualFold(cart.Name, trimmed) {
			return buildCartridgeDecision(trimmed, host, cart, true, fallback), nil
		}
	}

	// 2. Check embedded starter cartridges (ready immediately with zero install).
	builtins, _ := cartridge.Builtins()
	for _, cart := range builtins {
		if (host != "" && cart.MatchDomain(host)) || strings.EqualFold(cart.Name, trimmed) {
			return buildCartridgeDecision(trimmed, host, cart, true, fallback), nil
		}
	}

	// 3. Check 616+ Printing Press Library registry entries.
	cat, _ := c.Load(ctx, false)
	if cat != nil {
		if match := matchCatalogEntry(cat, trimmed, host); match != nil {
			_, isInstalled := InstalledBinaryPath(match.Name)
			recCmd := fmt.Sprintf("pp run %s --help", match.Name)
			return &RouteDecision{
				Matched:         true,
				Input:           trimmed,
				Host:            host,
				App:             match.Name,
				Mode:            "prebuilt-binary",
				Installed:       isInstalled,
				Description:     match.Description,
				RecommendedCmd:  recCmd,
				InstallCmd:      fmt.Sprintf("pp use %s", match.Name),
				BrowserFallback: fallback,
				EstimatedSavings: &SavingsEstimate{
					BrowserMS:     4500,
					PPMS:          180,
					BrowserTokens: 12000,
					PPTokens:      180,
				},
			}, nil
		}
	}

	compileTarget := trimmed
	if host == "" {
		compileTarget = "<url>"
	}
	return &RouteDecision{
		Matched:         false,
		Input:           trimmed,
		Host:            host,
		Mode:            "browser-fallback",
		Installed:       false,
		CompileCmd:      fmt.Sprintf("pp record --backend ardia %s", compileTarget),
		BrowserFallback: fallback,
	}, nil
}

func buildCartridgeDecision(input, host string, cart *cartridge.Cartridge, installed bool, fallback string) *RouteDecision {
	firstCmd := pickCommandName(cart, input)
	return &RouteDecision{
		Matched:         true,
		Input:           input,
		Host:            host,
		App:             cart.Name,
		Mode:            "cartridge",
		Installed:       installed,
		Description:     cart.Description,
		RecommendedCmd:  fmt.Sprintf("pp run %s %s --compact", cart.Name, firstCmd),
		BrowserFallback: fallback,
		EstimatedSavings: &SavingsEstimate{
			BrowserMS:     4000,
			PPMS:          150,
			BrowserTokens: 12000,
			PPTokens:      140,
		},
	}
}

func pickCommandName(cart *cartridge.Cartridge, input string) string {
	lowerInput := strings.ToLower(input)
	names := make([]string, 0, len(cart.Commands))
	for k := range cart.Commands {
		names = append(names, k)
	}
	sort.Strings(names)
	// Prefer a command name explicitly echoed in the URL path or user intent.
	for _, n := range names {
		if strings.Contains(lowerInput, strings.ToLower(n)) {
			return n
		}
	}
	for _, n := range names {
		if cart.Commands[n].SyncDefault {
			return n
		}
	}
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

func matchCatalogEntry(cat *Catalog, input, host string) *Entry {
	if host != "" {
		for i := range cat.Entries {
			e := &cat.Entries[i]
			if e.Homepage == "" {
				continue
			}
			eh := extractHost(e.Homepage)
			if eh != "" && (host == eh || strings.HasSuffix(host, "."+eh)) {
				return e
			}
		}
	}
	// Exact slug or high-confidence query match.
	lower := strings.ToLower(strings.TrimSpace(input))
	for i := range cat.Entries {
		e := &cat.Entries[i]
		if strings.ToLower(e.Name) == lower || strings.ToLower(e.Name)+"-pp-cli" == lower {
			return e
		}
	}
	if host == "" {
		// Natural language intent: match only strong hits.
		for tok := range strings.FieldsSeq(lower) {
			if len(tok) < 3 {
				continue
			}
			for i := range cat.Entries {
				e := &cat.Entries[i]
				if strings.ToLower(e.Name) == tok {
					return e
				}
			}
		}
	}
	return nil
}

func extractHost(raw string) string {
	s := strings.TrimSpace(raw)
	if !strings.Contains(s, "://") && strings.Contains(s, ".") && !strings.ContainsAny(s, " \t\n") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" {
		return ""
	}
	h := strings.ToLower(u.Hostname())
	h = strings.TrimPrefix(h, "www.")
	return h
}

// InstallResult describes what `pp use <slug>` installed.
type InstallResult struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"` // "cartridge" or "binary"
	Version   string `json:"version,omitempty"`
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SkillPath string `json:"skill_path,omitempty"`
	ZeroGo    bool   `json:"zero_go"`
}

// Install installs an app by slug with zero Go compiler required:
// embedded cartridge -> pinned immediately; otherwise downloads the prebuilt
// cross-platform binary from printing-press-library GitHub Releases and verifies
// its SHA-256 against checksums.txt.
func (c *Client) Install(ctx context.Context, slug string) (*InstallResult, error) {
	clean := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(slug)), "-pp-cli")
	if clean == "" {
		return nil, errors.New("app name is required")
	}

	// 1. Embedded starter cartridge?
	if raw, ok := cartridge.BuiltinRaw(clean); ok {
		cart, path, err := cartridge.InstallBytes(raw, "builtin:"+clean+".yaml")
		if err != nil {
			return nil, err
		}
		return &InstallResult{
			Name:    cart.Name,
			Kind:    "cartridge",
			Version: cart.Version,
			Path:    path,
			SHA256:  cart.SHA256,
			ZeroGo:  true,
		}, nil
	}

	// 2. Lookup in Printing Press Library catalog & download prebuilt binary.
	cat, err := c.Load(ctx, false)
	if err != nil {
		return nil, err
	}
	var entry *Entry
	for i := range cat.Entries {
		if strings.EqualFold(cat.Entries[i].Name, clean) {
			entry = &cat.Entries[i]
			break
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("app %q not found in catalog (try `pp search %s` or `pp record <url>`)", clean, clean)
	}

	cliName := clean + "-pp-cli"
	version := "current"
	if entry.Release != nil {
		if entry.Release.CLIName != "" {
			cliName = entry.Release.CLIName
		}
		if entry.Release.Version != "" {
			version = entry.Release.Version
		}
	}

	assetName := releaseAssetName(cliName, runtime.GOOS, runtime.GOARCH)
	tag := clean + "-current"
	baseURL := strings.TrimRight(c.ReleaseBaseURL, "/")
	checksumsURL := fmt.Sprintf("%s/%s/checksums.txt", baseURL, tag)
	assetURL := fmt.Sprintf("%s/%s/%s", baseURL, tag, assetName)

	expectedHash, err := c.fetchExpectedChecksum(ctx, checksumsURL, assetName)
	if err != nil {
		return nil, fmt.Errorf("verifying release checksums for %s: %w", clean, err)
	}

	binBytes, err := c.downloadBytes(ctx, assetURL, 100<<20)
	if err != nil {
		return nil, fmt.Errorf("downloading prebuilt binary %s: %w", assetName, err)
	}
	actualHash := sha256Hex(binBytes)
	if expectedHash != "" && !strings.EqualFold(expectedHash, actualHash) {
		return nil, fmt.Errorf("checksum mismatch for %s: expected %s, got %s", assetName, expectedHash, actualHash)
	}

	home, err := cartridge.HomeDir()
	if err != nil {
		return nil, err
	}
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		return nil, err
	}
	localName := cliName
	if runtime.GOOS == "windows" {
		localName += ".exe"
	}
	destPath := filepath.Join(binDir, localName)
	tmpPath := destPath + ".tmp"
	if err := os.WriteFile(tmpPath, binBytes, 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}

	// Best-effort focused SKILL.md download.
	skillPath := ""
	if c.SkillBaseURL != "" {
		skillURL := fmt.Sprintf("%s/pp-%s/SKILL.md", strings.TrimRight(c.SkillBaseURL, "/"), clean)
		if skillBytes, err := c.downloadBytes(ctx, skillURL, 2<<20); err == nil && len(skillBytes) > 0 {
			skillDir := filepath.Join(home, "skills", "pp-"+clean)
			if os.MkdirAll(skillDir, 0o700) == nil {
				target := filepath.Join(skillDir, "SKILL.md")
				if os.WriteFile(target, skillBytes, 0o600) == nil {
					skillPath = target
				}
			}
		}
	}

	lockPath, err := cartridge.LockfilePath()
	if err != nil {
		return nil, err
	}
	if err := cartridge.UpdateLockfile(lockPath, func(lf *cartridge.Lockfile) error {
		lf.Pin(cartridge.LockEntry{
			Kind:    "binary",
			Name:    clean,
			Version: version,
			SHA256:  actualHash,
			Path:    destPath,
			Source:  assetURL,
		})
		return nil
	}); err != nil {
		return nil, err
	}

	return &InstallResult{
		Name:      clean,
		Kind:      "binary",
		Version:   version,
		Path:      destPath,
		SHA256:    actualHash,
		SkillPath: skillPath,
		ZeroGo:    true,
	}, nil
}

// InstalledBinaryPath locates a previously installed prebuilt PPL binary under ~/.pp/bin/.
func InstalledBinaryPath(slug string) (string, bool) {
	home, err := cartridge.HomeDir()
	if err != nil {
		return "", false
	}
	clean := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(slug)), "-pp-cli")
	name := clean + "-pp-cli"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	p := filepath.Join(home, "bin", name)
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		return p, true
	}
	return "", false
}

// VerifyInstalledBinary checks the SHA-256 of an installed prebuilt binary
// against ~/.pp/lock.json, failing closed on missing pin, corrupt lockfile, or
// digest mismatch.
func VerifyInstalledBinary(slug, binPath string) error {
	lockPath, err := cartridge.LockfilePath()
	if err != nil {
		return fmt.Errorf("resolving lockfile path: %w", err)
	}
	lf, err := cartridge.LoadLockfile(lockPath)
	if err != nil {
		return fmt.Errorf("loading lockfile for binary verification: %w", err)
	}
	return lf.VerifyBinary(slug, binPath)
}

func releaseAssetName(cliName, goos, goarch string) string {
	name := fmt.Sprintf("%s-%s-%s", cliName, goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

func (c *Client) fetchExpectedChecksum(ctx context.Context, checksumsURL, assetName string) (string, error) {
	raw, err := c.downloadBytes(ctx, checksumsURL, 1<<20)
	if err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		target := strings.TrimPrefix(fields[1], "*")
		if target == assetName {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("asset %q not listed in checksums.txt", assetName)
}

func (c *Client) downloadBytes(ctx context.Context, rawURL string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "pp/2.0 (Printing Press Local Runtime)")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBytes))
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func readCatalogFile(path string) (*Catalog, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cat Catalog
	if err := json.Unmarshal(raw, &cat); err != nil {
		return nil, err
	}
	return &cat, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
