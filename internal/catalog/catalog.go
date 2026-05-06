package catalog

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mvanhorn/cli-printing-press/v3/internal/spec"
	"gopkg.in/yaml.v3"
)

var namePattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Public categories first, alphabetized. "other" and "example" are explicitly
// special (catch-all / test-only) and kept at the end.
var validCategories = map[string]struct{}{
	"ai":                      {},
	"auth":                    {},
	"cloud":                   {},
	"commerce":                {},
	"developer-tools":         {},
	"devices":                 {},
	"food-and-dining":         {},
	"marketing":               {},
	"media-and-entertainment": {},
	"monitoring":              {},
	"payments":                {},
	"productivity":            {},
	"project-management":      {},
	"sales-and-crm":           {},
	"social-and-messaging":    {},
	"travel":                  {},
	"other":                   {},
	"example":                 {},
}

var validSpecFormats = map[string]struct{}{
	"yaml":   {},
	"json":   {},
	"custom": {},
}

var validTiers = map[string]struct{}{
	"official":  {},
	"community": {},
}

var validSpecSources = map[string]struct{}{
	"official":  {}, // Published by API vendor (Stripe, GitHub, Discord)
	"community": {}, // Third-party maintained (apis-guru, community OpenAPI repos)
	"sniffed":   {}, // Reverse-engineered from browser traffic capture
	"docs":      {}, // Generated from documentation pages (--docs mode)
}

var validClientPatterns = map[string]struct{}{
	"rest":           {}, // Standard REST — default, no special client needed
	"proxy-envelope": {}, // All requests wrapped in a POST envelope (e.g., Postman _api/ws/proxy)
	"graphql":        {}, // GraphQL endpoint, needs query/mutation wrapper
}

var validHTTPTransports = map[string]struct{}{
	"standard":          {}, // Plain net/http, default for official APIs
	"browser-chrome":    {}, // Browser-compatible transport for web-discovered/non-official APIs
	"browser-chrome-h3": {}, // Chrome-compatible HTTP transport forced through HTTP/3
}

type KnownAlt struct {
	Name     string `yaml:"name"`
	URL      string `yaml:"url"`
	Language string `yaml:"language"`
}

// WrapperLibrary describes a community-maintained library that wraps an API
// without an official OpenAPI spec. The generator uses these as implementation
// backing when the primary source is reverse-engineered rather than spec-driven.
//
// IntegrationMode values:
//   - native: library is in Go and can be imported directly
//   - subprocess: library is in another language and must be invoked as a subprocess
//   - html-scrape: no library — the entry documents a reverse-engineering technique
type WrapperLibrary struct {
	Name            string `yaml:"name"`
	URL             string `yaml:"url"`
	Language        string `yaml:"language"`
	License         string `yaml:"license,omitempty"`
	IntegrationMode string `yaml:"integration_mode"`
	Notes           string `yaml:"notes,omitempty"`
}

type BearerRefresh struct {
	BundleURL string `yaml:"bundle_url,omitempty"`
	Pattern   string `yaml:"pattern,omitempty"`
}

func (b BearerRefresh) enabled() bool {
	return strings.TrimSpace(b.BundleURL) != "" || strings.TrimSpace(b.Pattern) != ""
}

var validIntegrationModes = map[string]struct{}{
	"native":      {},
	"subprocess":  {},
	"html-scrape": {},
}

type Entry struct {
	Name              string     `yaml:"name"`
	DisplayName       string     `yaml:"display_name"`
	Description       string     `yaml:"description"`
	Category          string     `yaml:"category"`
	SpecURL           string     `yaml:"spec_url"`
	SpecFormat        string     `yaml:"spec_format"`
	OpenAPIVersion    string     `yaml:"openapi_version"`
	Tier              string     `yaml:"tier"`
	VerifiedDate      string     `yaml:"verified_date"`
	Homepage          string     `yaml:"homepage"`
	Notes             string     `yaml:"notes"`
	Owner             string     `yaml:"owner,omitempty"`
	OwnerName         string     `yaml:"owner_name,omitempty"`
	KnownAlternatives []KnownAlt `yaml:"known_alternatives,omitempty"`
	SandboxEndpoint   string     `yaml:"sandbox_endpoint,omitempty"`
	// SpecSource describes how the spec was obtained. Empty defaults to "official".
	// Values: official, community, sniffed, docs.
	SpecSource string `yaml:"spec_source,omitempty"`
	// AuthRequired indicates whether the API needs authentication. Empty means unknown.
	AuthRequired *bool `yaml:"auth_required,omitempty"`
	// ClientPattern describes the HTTP client pattern needed. Empty defaults to "rest".
	// Values: rest, proxy-envelope, graphql.
	ClientPattern string `yaml:"client_pattern,omitempty"`
	// HTTPTransport describes the runtime HTTP transport. Empty defaults by provenance:
	// official uses standard; non-official web-discovered sources use browser-chrome.
	HTTPTransport string `yaml:"http_transport,omitempty"`
	// MCP describes generation-time MCP surface choices for catalog specs whose
	// upstream OpenAPI documents do not carry Printing Press extensions.
	MCP spec.MCPConfig `yaml:"mcp,omitempty"`
	// ProxyRoutes maps path prefixes to backend service names for proxy-envelope APIs.
	// Only relevant when ClientPattern is "proxy-envelope".
	ProxyRoutes map[string]string `yaml:"proxy_routes,omitempty"`
	// BearerRefresh describes how a printed CLI can fetch a rotating public
	// client bearer token from the source site's browser bundle.
	BearerRefresh BearerRefresh `yaml:"bearer_refresh,omitempty"`
	// WrapperLibraries lists reverse-engineered community libraries the generator
	// can use as implementation backing when no official spec exists. When this
	// list is non-empty, spec_url and spec_format are optional.
	WrapperLibraries []WrapperLibrary `yaml:"wrapper_libraries,omitempty"`
}

// IsWrapperOnly reports whether this entry represents an API reached through
// community wrapper libraries rather than an official spec.
func (e *Entry) IsWrapperOnly() bool {
	return e.SpecURL == "" && len(e.WrapperLibraries) > 0
}

func ParseEntry(data []byte) (*Entry, error) {
	var e Entry
	if err := yaml.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("parsing yaml: %w", err)
	}
	if err := e.Validate(); err != nil {
		return nil, fmt.Errorf("validation: %w", err)
	}
	return &e, nil
}

func ParseDir(dir string) ([]Entry, error) {
	return ParseFS(os.DirFS(dir))
}

// ParseFS reads all YAML catalog entries from an fs.FS (e.g., an embedded filesystem).
// It mirrors ParseDir but operates on the fs.FS interface instead of the OS filesystem.
func ParseFS(fsys fs.FS) ([]Entry, error) {
	dirEntries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("reading fs: %w", err)
	}

	sort.Slice(dirEntries, func(i, j int) bool {
		return dirEntries[i].Name() < dirEntries[j].Name()
	})

	entries := make([]Entry, 0, len(dirEntries))
	for _, de := range dirEntries {
		if de.IsDir() {
			continue
		}
		if filepath.Ext(de.Name()) != ".yaml" {
			continue
		}

		data, err := fs.ReadFile(fsys, de.Name())
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", de.Name(), err)
		}

		entry, err := ParseEntry(data)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", de.Name(), err)
		}
		entries = append(entries, *entry)
	}

	return entries, nil
}

// LookupFS finds a single catalog entry by name from an fs.FS.
// Returns an error if the entry is not found.
func LookupFS(fsys fs.FS, name string) (*Entry, error) {
	data, err := fs.ReadFile(fsys, name+".yaml")
	if err != nil {
		return nil, fmt.Errorf("catalog entry %q not found", name)
	}
	return ParseEntry(data)
}

func (e *Entry) Validate() error {
	if e.Name == "" {
		return fmt.Errorf("name is required")
	}
	if !namePattern.MatchString(e.Name) {
		return fmt.Errorf("name must be lowercase kebab-case (letters, digits, hyphens only)")
	}
	if e.DisplayName == "" {
		return fmt.Errorf("display_name is required")
	}
	if e.Description == "" {
		return fmt.Errorf("description is required")
	}
	if e.Category == "" {
		return fmt.Errorf("category is required")
	}
	if _, ok := validCategories[e.Category]; !ok {
		return fmt.Errorf("category must be one of: %s", strings.Join(PublicCategories(), ", "))
	}
	wrapperOnly := len(e.WrapperLibraries) > 0 && e.SpecURL == ""
	if !wrapperOnly {
		if e.SpecURL == "" {
			return fmt.Errorf("spec_url is required (or populate wrapper_libraries for a wrapper-only entry)")
		}
		if !strings.HasPrefix(e.SpecURL, "https://") {
			return fmt.Errorf(`spec_url must start with "https://"`)
		}
		if e.SpecFormat == "" {
			return fmt.Errorf("spec_format is required")
		}
		if _, ok := validSpecFormats[e.SpecFormat]; !ok {
			return fmt.Errorf("spec_format must be one of: %s", strings.Join(validSpecFormatNames(), ", "))
		}
	} else if e.SpecFormat != "" {
		if _, ok := validSpecFormats[e.SpecFormat]; !ok {
			return fmt.Errorf("spec_format must be one of: %s", strings.Join(validSpecFormatNames(), ", "))
		}
	}

	for i, w := range e.WrapperLibraries {
		if w.Name == "" {
			return fmt.Errorf("wrapper_libraries[%d]: name is required", i)
		}
		if w.URL == "" {
			return fmt.Errorf("wrapper_libraries[%d]: url is required", i)
		}
		if !strings.HasPrefix(w.URL, "https://") {
			return fmt.Errorf(`wrapper_libraries[%d]: url must start with "https://"`, i)
		}
		if w.Language == "" {
			return fmt.Errorf("wrapper_libraries[%d]: language is required", i)
		}
		if w.IntegrationMode == "" {
			return fmt.Errorf("wrapper_libraries[%d]: integration_mode is required", i)
		}
		if _, ok := validIntegrationModes[w.IntegrationMode]; !ok {
			return fmt.Errorf("wrapper_libraries[%d]: integration_mode must be one of: native, subprocess, html-scrape", i)
		}
	}
	if e.Tier == "" {
		return fmt.Errorf("tier is required")
	}
	if _, ok := validTiers[e.Tier]; !ok {
		return fmt.Errorf("tier must be one of: official, community")
	}

	if e.SpecSource != "" {
		if _, ok := validSpecSources[e.SpecSource]; !ok {
			return fmt.Errorf("spec_source must be one of: official, community, sniffed, docs")
		}
	}

	if e.ClientPattern != "" {
		if _, ok := validClientPatterns[e.ClientPattern]; !ok {
			return fmt.Errorf("client_pattern must be one of: rest, proxy-envelope, graphql")
		}
	}
	if e.HTTPTransport != "" {
		if _, ok := validHTTPTransports[e.HTTPTransport]; !ok {
			return fmt.Errorf("http_transport must be one of: standard, browser-chrome, browser-chrome-h3")
		}
	}
	if err := validateBearerRefresh(e.BearerRefresh); err != nil {
		return err
	}

	return nil
}

func validateBearerRefresh(cfg BearerRefresh) error {
	if !cfg.enabled() {
		return nil
	}
	if strings.TrimSpace(cfg.BundleURL) == "" {
		return fmt.Errorf("bearer_refresh.bundle_url is required when bearer_refresh is declared")
	}
	if strings.TrimSpace(cfg.Pattern) == "" {
		return fmt.Errorf("bearer_refresh.pattern is required when bearer_refresh is declared")
	}
	if !strings.HasPrefix(cfg.BundleURL, "https://") {
		return fmt.Errorf(`bearer_refresh.bundle_url must start with "https://"`)
	}
	if _, err := regexp.Compile(cfg.Pattern); err != nil {
		return fmt.Errorf("bearer_refresh.pattern is not a valid regexp: %w", err)
	}
	return nil
}

// PublicCategories returns the sorted list of user-facing categories.
// It excludes "example", which is internal-only for test fixtures.
func PublicCategories() []string {
	cats := make([]string, 0, len(validCategories))
	for c := range validCategories {
		if c != "example" {
			cats = append(cats, c)
		}
	}
	sort.Strings(cats)
	return cats
}

func validSpecFormatNames() []string {
	formats := make([]string, 0, len(validSpecFormats))
	for format := range validSpecFormats {
		formats = append(formats, format)
	}
	sort.Strings(formats)
	return formats
}

// IsPublicCategory reports whether category is allowed in user-facing workflows.
func IsPublicCategory(category string) bool {
	if category == "example" {
		return false
	}
	_, ok := validCategories[category]
	return ok
}
