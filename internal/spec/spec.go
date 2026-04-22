package spec

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Valid values for APISpec.Kind. A bare string with no const was the
// established convention for sibling fields (SpecSource, ClientPattern), but
// Kind is compared in production code at multiple sites, so the constant
// prevents typos from silently falling through to the default-rest path.
const (
	KindREST      = "rest"      // default; strict path-validity against the spec
	KindSynthetic = "synthetic" // multi-source / combo CLI; dogfood + scorecard relax path-validity
)

type APISpec struct {
	Name            string              `yaml:"name" json:"name"`
	Description     string              `yaml:"description" json:"description"`
	Version         string              `yaml:"version" json:"version"`
	BaseURL         string              `yaml:"base_url" json:"base_url"`
	BasePath        string              `yaml:"base_path,omitempty" json:"base_path,omitempty"`
	Owner           string              `yaml:"owner,omitempty" json:"owner,omitempty"`                   // GitHub owner for import paths and Homebrew tap
	Kind            string              `yaml:"kind,omitempty" json:"kind,omitempty"`                     // "rest" (default) or "synthetic" — synthetic CLIs aggregate multiple sources beyond the spec; dogfood's path-validity check is relaxed accordingly
	SpecSource      string              `yaml:"spec_source,omitempty" json:"spec_source,omitempty"`       // official, community, sniffed, docs — affects generated client defaults
	ClientPattern   string              `yaml:"client_pattern,omitempty" json:"client_pattern,omitempty"` // rest (default), proxy-envelope — affects generated HTTP client
	ProxyRoutes     map[string]string   `yaml:"proxy_routes,omitempty" json:"proxy_routes,omitempty"`     // path prefix → service name for proxy-envelope routing
	WebsiteURL      string              `yaml:"website_url,omitempty" json:"website_url,omitempty"`       // product/company website (not the API base URL)
	Category        string              `yaml:"category,omitempty" json:"category,omitempty"`             // catalog category (e.g., productivity, developer-tools) — used for library install path
	Auth            AuthConfig          `yaml:"auth" json:"auth"`
	RequiredHeaders []RequiredHeader    `yaml:"required_headers,omitempty" json:"required_headers,omitempty"`
	Config          ConfigSpec          `yaml:"config" json:"config"`
	Resources       map[string]Resource `yaml:"resources" json:"resources"`
	Types           map[string]TypeDef  `yaml:"types" json:"types"`
	ExtraCommands   []ExtraCommand      `yaml:"extra_commands,omitempty" json:"extra_commands,omitempty"` // hand-written cobra commands declared so SKILL.md can document them; spec-only metadata, no code generated
	Cache           CacheConfig         `yaml:"cache,omitempty" json:"cache,omitempty"`                   // cache freshness + auto-refresh config; when enabled, generated read commands auto-refresh stale local data before serving
	Share           ShareConfig         `yaml:"share,omitempty" json:"share,omitempty"`                   // git-backed snapshot sharing config; when enabled, emits a `share` subcommand that publishes/subscribes to a git repo
}

// ExtraCommand declares a hand-written cobra command so the SKILL.md
// Command Reference can list it alongside spec-driven resources. The
// generator does not emit code for these — authors hand-write the
// command in internal/cli/. Without this declaration the SKILL.md
// template only sees .Resources and silently omits hand-written
// commands, which is the drift class that motivated this field.
type ExtraCommand struct {
	Name        string `yaml:"name" json:"name"`                     // command path, e.g. "boxscore" or "tv airing-today"
	Description string `yaml:"description" json:"description"`       // one-line description rendered after a dash
	Args        string `yaml:"args,omitempty" json:"args,omitempty"` // optional positional arg signature, e.g. "<event_id>" or "<team1> <team2>"
}

// IsSynthetic reports whether this spec declares a multi-source / combo CLI
// where hand-built commands intentionally go beyond the spec. Dogfood skips
// strict path-validity and scorecard marks path_validity as unscored.
func (s *APISpec) IsSynthetic() bool {
	return s != nil && s.Kind == KindSynthetic
}

// RequiredHeader represents a non-auth header that the API requires on most
// requests (e.g., cal-api-version, Stripe-Version, anthropic-version).
// Detected automatically from OpenAPI specs when a required header parameter
// appears on >80% of operations.
type RequiredHeader struct {
	Name  string `yaml:"name" json:"name"`
	Value string `yaml:"value" json:"value"`
}

type AuthConfig struct {
	Type             string   `yaml:"type" json:"type"` // api_key, oauth2, bearer_token, cookie, composed, session_handshake, none
	Header           string   `yaml:"header" json:"header"`
	Format           string   `yaml:"format" json:"format"`
	EnvVars          []string `yaml:"env_vars" json:"env_vars"`
	Optional         bool     `yaml:"optional,omitempty" json:"optional,omitempty"` // true when the key enhances a subset of features (e.g., USDA nutrition backfill) rather than gating core functionality; doctor treats unconfigured optional auth as INFO not FAIL and README frames the section as "Optional"
	Scheme           string   `yaml:"scheme,omitempty" json:"scheme,omitempty"`     // OpenAPI security scheme name
	In               string   `yaml:"in,omitempty" json:"in,omitempty"`             // header, query, cookie
	KeyURL           string   `yaml:"key_url,omitempty" json:"key_url,omitempty"`   // URL where users can register for an API key
	AuthorizationURL string   `yaml:"authorization_url,omitempty" json:"authorization_url,omitempty"`
	TokenURL         string   `yaml:"token_url,omitempty" json:"token_url,omitempty"`
	Scopes           []string `yaml:"scopes,omitempty" json:"scopes,omitempty"`
	CookieDomain     string   `yaml:"cookie_domain,omitempty" json:"cookie_domain,omitempty"` // domain to read browser cookies from (e.g. ".notion.so")
	Cookies          []string `yaml:"cookies,omitempty" json:"cookies,omitempty"`             // named cookies to extract for composed auth (e.g. ["customerId", "authToken"])
	Inferred         bool     `yaml:"inferred,omitempty" json:"inferred,omitempty"`           // true when auth was inferred from spec description, not declared in securitySchemes

	// Session-handshake fields. Used only when Type == "session_handshake".
	// The pattern: GET BootstrapURL to seed cookies → GET TokenURL to receive
	// an anti-CSRF token (the "crumb" on Yahoo Finance, similarly named on
	// Walmart, some streaming APIs, Facebook's internal graph) → pass that
	// token on every subsequent data request as TokenParamName in TokenParamIn.
	// The generator emits a cookie jar, disk-persisted session file, and auto-
	// invalidation on InvalidateOnStatus responses.
	BootstrapURL       string `yaml:"bootstrap_url,omitempty" json:"bootstrap_url,omitempty"`               // optional GET to seed cookies before token fetch (e.g. "https://fc.yahoo.com/")
	SessionTokenURL    string `yaml:"session_token_url,omitempty" json:"session_token_url,omitempty"`       // endpoint that returns the token (e.g. "https://query2.finance.yahoo.com/v1/test/getcrumb"); distinct from TokenURL (OAuth) to avoid conflation
	TokenFormat        string `yaml:"token_format,omitempty" json:"token_format,omitempty"`                 // "text" (raw body) or "json" (extract via TokenJSONPath); default "text"
	TokenJSONPath      string `yaml:"token_json_path,omitempty" json:"token_json_path,omitempty"`           // when TokenFormat is "json", dot-path to the token field (e.g. "data.crumb")
	TokenParamName     string `yaml:"token_param_name,omitempty" json:"token_param_name,omitempty"`         // parameter name to attach to requests (e.g. "crumb")
	TokenParamIn       string `yaml:"token_param_in,omitempty" json:"token_param_in,omitempty"`             // "query" or "header"; default "query"
	InvalidateOnStatus []int  `yaml:"invalidate_on_status,omitempty" json:"invalidate_on_status,omitempty"` // HTTP status codes that should invalidate the cached token and re-bootstrap (e.g. [401, 403])
	SessionTTLHours    int    `yaml:"session_ttl_hours,omitempty" json:"session_ttl_hours,omitempty"`       // how long to trust a cached session (default 24)
}

type ConfigSpec struct {
	Format string `yaml:"format" json:"format"` // toml, yaml
	Path   string `yaml:"path" json:"path"`
}

// CacheConfig gates the auto-refresh machinery emitted into a printed CLI.
// Opt-in — CLIs whose local store is per-user state (carts, drafts) should leave
// Enabled at its zero value so reads never silently replace the user's state
// with a snapshot from a different session.
//
// StaleAfter and RefreshTimeout are strings parsed to time.Duration at CLI
// runtime; keeping them as strings lets spec authors write "6h" or "30s" and
// preserves the yaml-level representation for round-trip tooling.
type CacheConfig struct {
	Enabled        bool              `yaml:"enabled,omitempty" json:"enabled,omitempty"`                 // master switch; when false, freshness helpers and pre-run refresh hook are not emitted
	StaleAfter     string            `yaml:"stale_after,omitempty" json:"stale_after,omitempty"`         // default duration after which any resource's last_synced_at is considered stale (e.g., "6h"). Blank means runtime default (6h).
	RefreshTimeout string            `yaml:"refresh_timeout,omitempty" json:"refresh_timeout,omitempty"` // max wall-clock the pre-run refresh may block the command (e.g., "30s"). On timeout the command serves stale data with a stderr warning. Blank means runtime default (30s).
	EnvOptOut      string            `yaml:"env_opt_out,omitempty" json:"env_opt_out,omitempty"`         // env var name that disables auto-refresh when set to "1" (e.g., LINEAR_NO_AUTO_REFRESH). Blank lets the template derive {{upper name}}_NO_AUTO_REFRESH.
	Resources      map[string]string `yaml:"resources,omitempty" json:"resources,omitempty"`             // per-resource override of stale_after (e.g., quotes: "5m", channels: "24h"). Resources not listed inherit StaleAfter.
}

// ShareConfig gates the git-backed snapshot share surface emitted into a
// printed CLI. When Enabled, the generator emits an internal/share package
// plus a `share` cobra command (publish, subscribe, export, import). Share
// is off by default because it is a multi-user feature and most CLIs are
// single-user; enabling also requires an explicit SnapshotTables allowlist
// to prevent accidental export of auth or per-user state.
type ShareConfig struct {
	Enabled        bool     `yaml:"enabled,omitempty" json:"enabled,omitempty"`                 // master switch; when false, the share package and command are not emitted
	SnapshotTables []string `yaml:"snapshot_tables,omitempty" json:"snapshot_tables,omitempty"` // explicit allowlist of SQLite tables included in the snapshot. Required when Enabled. Names matching denylisted patterns (*_cache, *_secrets, auth_*) are rejected at parse time.
	DefaultRepo    string   `yaml:"default_repo,omitempty" json:"default_repo,omitempty"`       // optional default git remote (e.g., "git@github.com:acme/linear-snapshots.git"); command-line --repo flag always wins
	DefaultBranch  string   `yaml:"default_branch,omitempty" json:"default_branch,omitempty"`   // optional default branch for push/pull; blank means "main"
}

type Resource struct {
	Description  string              `yaml:"description" json:"description"`
	Path         string              `yaml:"path,omitempty" json:"path,omitempty"`             // base path for operations shorthand (e.g., /api/items)
	Operations   []string            `yaml:"operations,omitempty" json:"operations,omitempty"` // shorthand: list, get, create, update, delete, search
	Endpoints    map[string]Endpoint `yaml:"endpoints" json:"endpoints"`
	SubResources map[string]Resource `yaml:"sub_resources,omitempty" json:"sub_resources,omitempty"`
}

type Endpoint struct {
	Method          string            `yaml:"method" json:"method"`
	Path            string            `yaml:"path" json:"path"`
	Description     string            `yaml:"description" json:"description"`
	Params          []Param           `yaml:"params" json:"params"`
	Body            []Param           `yaml:"body" json:"body"`
	Response        ResponseDef       `yaml:"response" json:"response"`
	Pagination      *Pagination       `yaml:"pagination" json:"pagination"`
	ResponsePath    string            `yaml:"response_path,omitempty" json:"response_path,omitempty"`       // path to extract data array from response (e.g., "data", "results.items")
	Meta            map[string]string `yaml:"meta,omitempty" json:"meta,omitempty"`                         // per-endpoint metadata (e.g., source_tier, source_count from crowd-sniff)
	HeaderOverrides []RequiredHeader  `yaml:"header_overrides,omitempty" json:"header_overrides,omitempty"` // per-endpoint header overrides (e.g., different api-version)
	NoAuth          bool              `yaml:"no_auth,omitempty" json:"no_auth,omitempty"`                   // true when the endpoint does not require authentication
	Alias           string            `yaml:"-" json:"-"`                                                   // computed, not from YAML
}

type Param struct {
	Name        string   `yaml:"name" json:"name"`
	Type        string   `yaml:"type" json:"type"`
	Required    bool     `yaml:"required" json:"required"`
	Positional  bool     `yaml:"positional" json:"positional"`
	PathParam   bool     `yaml:"path_param,omitempty" json:"path_param,omitempty"` // true for path params rendered as flags (e.g., pagination)
	Default     any      `yaml:"default" json:"default"`
	Description string   `yaml:"description" json:"description"`
	Fields      []Param  `yaml:"fields" json:"fields"`                     // for nested objects
	Enum        []string `yaml:"enum,omitempty" json:"enum,omitempty"`     // enum constraints for the parameter
	Format      string   `yaml:"format,omitempty" json:"format,omitempty"` // OpenAPI format hints (date-time, email, uri, etc.)
}

type ResponseDef struct {
	Type string `yaml:"type" json:"type"` // object, array
	Item string `yaml:"item" json:"item"` // type name
}

type Pagination struct {
	Type           string `yaml:"type" json:"type"`                         // cursor, offset, page_token
	LimitParam     string `yaml:"limit_param" json:"limit_param"`           // query param name for page size (limit, maxResults, pageSize)
	CursorParam    string `yaml:"cursor_param" json:"cursor_param"`         // query param name for cursor (after, pageToken, offset)
	NextCursorPath string `yaml:"next_cursor_path" json:"next_cursor_path"` // response field with next cursor (nextPageToken, cursor)
	HasMoreField   string `yaml:"has_more_field" json:"has_more_field"`     // response field indicating more pages (has_more)
}

type TypeDef struct {
	Fields []TypeField `yaml:"fields" json:"fields"`
}

type TypeField struct {
	Name string `yaml:"name" json:"name"`
	Type string `yaml:"type" json:"type"`
}

func Parse(path string) (*APISpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	return ParseBytes(data)
}

func ParseBytes(data []byte) (*APISpec, error) {
	var s APISpec
	yamlErr := yaml.Unmarshal(data, &s)
	if yamlErr != nil || len(s.Resources) == 0 {
		// Try JSON round-trip: yaml → map → json → struct.
		// This handles YAML style variations (flow arrays, Python-style
		// quoting, non-standard indentation) that can cause struct mapping
		// to silently produce empty fields even though the YAML is valid.
		var raw map[string]any
		if yaml.Unmarshal(data, &raw) == nil && len(raw) > 0 {
			if jsonBytes, err := json.Marshal(raw); err == nil {
				var fallback APISpec
				if json.Unmarshal(jsonBytes, &fallback) == nil && len(fallback.Resources) > 0 {
					s = fallback
					yamlErr = nil
				}
			}
		}
	}
	if yamlErr != nil {
		return nil, fmt.Errorf("parsing yaml: %w", yamlErr)
	}
	s.expandOperations()
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("validation: %w", err)
	}
	return &s, nil
}

// expandOperations converts operations shorthand (e.g., [list, get, create])
// into explicit Endpoint entries for each resource that has Operations set.
// Explicit endpoints take precedence over generated ones.
func (s *APISpec) expandOperations() {
	for name, r := range s.Resources {
		if len(r.Operations) == 0 || r.Path == "" {
			continue
		}
		if r.Endpoints == nil {
			r.Endpoints = make(map[string]Endpoint)
		}
		idParam := singularize(name) + "Id"
		for _, op := range r.Operations {
			// Skip if an explicit endpoint already exists with this name
			if _, exists := r.Endpoints[op]; exists {
				continue
			}
			switch op {
			case "list":
				r.Endpoints["list"] = Endpoint{
					Method:       "GET",
					Path:         r.Path,
					Description:  "List " + name,
					ResponsePath: "results",
				}
			case "get":
				r.Endpoints["get"] = Endpoint{
					Method:      "GET",
					Path:        r.Path + "/{" + idParam + "}",
					Description: "Get a " + singularize(name) + " by ID",
					Params: []Param{{
						Name:        idParam,
						Type:        "string",
						Required:    true,
						Positional:  true,
						Description: singularize(name) + " ID",
					}},
				}
			case "create":
				r.Endpoints["create"] = Endpoint{
					Method:      "POST",
					Path:        r.Path,
					Description: "Create a new " + singularize(name),
				}
			case "update":
				r.Endpoints["update"] = Endpoint{
					Method:      "PATCH",
					Path:        r.Path + "/{" + idParam + "}",
					Description: "Update a " + singularize(name),
					Params: []Param{{
						Name:        idParam,
						Type:        "string",
						Required:    true,
						Positional:  true,
						Description: singularize(name) + " ID",
					}},
				}
			case "delete":
				r.Endpoints["delete"] = Endpoint{
					Method:      "DELETE",
					Path:        r.Path + "/{" + idParam + "}",
					Description: "Delete a " + singularize(name),
					Params: []Param{{
						Name:        idParam,
						Type:        "string",
						Required:    true,
						Positional:  true,
						Description: singularize(name) + " ID",
					}},
				}
			case "search":
				r.Endpoints["search"] = Endpoint{
					Method:       "POST",
					Path:         r.Path + "/search",
					Description:  "Search " + name,
					ResponsePath: "results",
					Body: []Param{{
						Name:        "query",
						Type:        "string",
						Description: "Search query string",
					}},
				}
			}
		}
		s.Resources[name] = r
	}
}

// singularize returns a simple singular form of a plural noun.
// Handles common patterns; irregular forms use a lookup table.
func singularize(s string) string {
	irregulars := map[string]string{
		"properties": "property",
		"companies":  "company",
		"categories": "category",
		"entries":    "entry",
		"statuses":   "status",
		"addresses":  "address",
		"analyses":   "analysis",
	}
	lower := strings.ToLower(s)
	if singular, ok := irregulars[lower]; ok {
		return singular
	}
	if strings.HasSuffix(lower, "ies") {
		return lower[:len(lower)-3] + "y"
	}
	if strings.HasSuffix(lower, "ses") || strings.HasSuffix(lower, "xes") || strings.HasSuffix(lower, "zes") {
		return lower[:len(lower)-2]
	}
	if strings.HasSuffix(lower, "s") && !strings.HasSuffix(lower, "ss") {
		return lower[:len(lower)-1]
	}
	return lower
}

func (s *APISpec) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("name is required")
	}
	// Note: s.Version holds the API version from the spec (for provenance).
	// The CLI version is always hardcoded to "1.0.0" in the generated root.go
	// template — it is independent of the API version.
	// Parser fallback may supply a placeholder base_url when the source spec omits servers.
	if s.BaseURL == "" && s.BasePath == "" {
		return fmt.Errorf("base_url is required")
	}
	if len(s.Resources) == 0 {
		return fmt.Errorf("at least one resource is required")
	}
	if err := validateExtraCommands(s.ExtraCommands); err != nil {
		return err
	}
	if err := validateCacheShare(s.Cache, s.Share); err != nil {
		return err
	}
	for name, r := range s.Resources {
		if len(r.Endpoints) == 0 && len(r.SubResources) == 0 {
			return fmt.Errorf("resource %q has no endpoints", name)
		}
		for eName, e := range r.Endpoints {
			if e.Method == "" {
				return fmt.Errorf("resource %q endpoint %q: method is required", name, eName)
			}
			if e.Path == "" {
				return fmt.Errorf("resource %q endpoint %q: path is required", name, eName)
			}
		}
		for subName, sub := range r.SubResources {
			if len(sub.Endpoints) == 0 {
				return fmt.Errorf("resource %q sub-resource %q has no endpoints", name, subName)
			}
			for eName, e := range sub.Endpoints {
				if e.Method == "" {
					return fmt.Errorf("resource %q sub-resource %q endpoint %q: method is required", name, subName, eName)
				}
				if e.Path == "" {
					return fmt.Errorf("resource %q sub-resource %q endpoint %q: path is required", name, subName, eName)
				}
			}
		}
	}
	return nil
}

// extraCommandNameRe permits a single command leaf or a parent+leaf path
// like "tv airing-today". Each segment must be lowercase with hyphens,
// matching cobra's convention. Anything else (uppercase, underscores,
// spaces in a segment) would not match an actual cobra Use: declaration
// and would silently fail the verify-skill unknown-command check at the
// consumer side, so we reject early here.
var extraCommandNameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*( [a-z][a-z0-9-]*){0,2}$`)

func validateExtraCommands(cmds []ExtraCommand) error {
	seen := make(map[string]struct{}, len(cmds))
	for i, c := range cmds {
		if c.Name == "" {
			return fmt.Errorf("extra_commands[%d]: name is required", i)
		}
		if !extraCommandNameRe.MatchString(c.Name) {
			return fmt.Errorf("extra_commands[%d]: name %q must be lowercase command path (one to three segments separated by single spaces, lowercase letters, digits, and hyphens)", i, c.Name)
		}
		if c.Description == "" {
			return fmt.Errorf("extra_commands[%d] (%s): description is required", i, c.Name)
		}
		if _, dup := seen[c.Name]; dup {
			return fmt.Errorf("extra_commands[%d]: name %q appears more than once", i, c.Name)
		}
		seen[c.Name] = struct{}{}
	}
	return nil
}

// shareTableDenyRe matches table names that must never appear in a share
// snapshot: anything ending in _cache or _secrets, or starting with auth_.
// These patterns catch the tables most likely to hold bearer tokens,
// device fingerprints, or derived per-user state that should never travel
// in a shared git repo.
var shareTableDenyRe = regexp.MustCompile(`(?i)^auth_|_cache$|_secrets$`)

// shareTableNameRe enforces SQLite-compatible lowercase identifiers for
// snapshot table entries. Keeping this strict avoids surprises when the
// generator later emits SELECT/DELETE statements against these names.
var shareTableNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// durationLikeRe is a forgiving parse-time sanity check for CacheConfig
// duration strings. The strict parse happens in the generated CLI at
// runtime via time.ParseDuration; this check only rejects obviously
// malformed values so typos surface at spec load, not at end-user runtime.
var durationLikeRe = regexp.MustCompile(`^\d+(\.\d+)?(ns|us|µs|ms|s|m|h)(\d+(\.\d+)?(ns|us|µs|ms|s|m|h))*$`)

func validateCacheShare(cache CacheConfig, share ShareConfig) error {
	if cache.StaleAfter != "" && !durationLikeRe.MatchString(cache.StaleAfter) {
		return fmt.Errorf("cache.stale_after %q is not a valid Go duration", cache.StaleAfter)
	}
	if cache.RefreshTimeout != "" && !durationLikeRe.MatchString(cache.RefreshTimeout) {
		return fmt.Errorf("cache.refresh_timeout %q is not a valid Go duration", cache.RefreshTimeout)
	}
	for resource, dur := range cache.Resources {
		if resource == "" {
			return fmt.Errorf("cache.resources: resource name must not be empty")
		}
		if !durationLikeRe.MatchString(dur) {
			return fmt.Errorf("cache.resources[%s] = %q is not a valid Go duration", resource, dur)
		}
	}

	if !share.Enabled {
		if len(share.SnapshotTables) > 0 {
			return fmt.Errorf("share.snapshot_tables is set but share.enabled is false; either enable or remove")
		}
		return nil
	}
	if len(share.SnapshotTables) == 0 {
		return fmt.Errorf("share.enabled requires a non-empty share.snapshot_tables allowlist")
	}
	seen := make(map[string]struct{}, len(share.SnapshotTables))
	for i, t := range share.SnapshotTables {
		if !shareTableNameRe.MatchString(t) {
			return fmt.Errorf("share.snapshot_tables[%d]: %q must be a lowercase SQLite identifier (letters, digits, underscore)", i, t)
		}
		if shareTableDenyRe.MatchString(t) {
			return fmt.Errorf("share.snapshot_tables[%d]: %q matches the denylist (auth_*, *_cache, *_secrets) and must not be shared", i, t)
		}
		if _, dup := seen[t]; dup {
			return fmt.Errorf("share.snapshot_tables[%d]: %q appears more than once", i, t)
		}
		seen[t] = struct{}{}
	}
	return nil
}

// CountMCPTools counts total endpoints and public (NoAuth) endpoints across
// all resources and sub-resources.
func (s *APISpec) CountMCPTools() (total, public int) {
	for _, r := range s.Resources {
		for _, e := range r.Endpoints {
			total++
			if e.NoAuth {
				public++
			}
		}
		for _, sub := range r.SubResources {
			for _, e := range sub.Endpoints {
				total++
				if e.NoAuth {
					public++
				}
			}
		}
	}
	return
}
