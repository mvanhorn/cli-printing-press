package spec

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
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

const (
	HTTPTransportStandard        = "standard"          // default for official API clients
	HTTPTransportBrowserChrome   = "browser-chrome"    // Chrome-impersonated transport for browser-facing web surfaces
	HTTPTransportBrowserChromeH3 = "browser-chrome-h3" // Chrome-impersonated transport forced through HTTP/3 for stricter bot screens
)

const (
	ResponseFormatJSON = "json"
	ResponseFormatHTML = "html"
)

const (
	TierAuthTypeNone        = "none"
	TierAuthTypeAPIKey      = "api_key"
	TierAuthTypeBearerToken = "bearer_token"

	TierAuthPlacementHeader = "header"
	TierAuthPlacementQuery  = "query"
)

const (
	HTMLExtractModePage         = "page"
	HTMLExtractModeLinks        = "links"
	HTMLExtractModeEmbeddedJSON = "embedded-json"
)

// DefaultEmbeddedJSONScriptSelector is the script-tag selector used when
// `html_extract.mode: embedded-json` is set without an explicit
// `script_selector`. Targets Next.js's pages-router `<script id="__NEXT_DATA__">`
// block — the most common shape and the one the food52 retro surfaced.
// Other SSR frameworks declare different selectors:
//   - Nuxt:        script#__NUXT__
//   - Remix:       script:contains("window.__remixContext") (use selector
//     with type or id when available)
//   - Astro:       site-specific; declare per spec
const DefaultEmbeddedJSONScriptSelector = "script#__NEXT_DATA__"

type APISpec struct {
	Name string `yaml:"name" json:"name"`
	// DisplayName is the human-readable brand name used in user-facing
	// surfaces that aren't a kebab-case slug — Claude Desktop's connector
	// list, MCPB manifest display_name, the MCP server's protocol-level
	// name in `server.NewMCPServer(...)`. Authors can set it explicitly
	// (e.g. "Company GOAT", "Cal.com", "PokéAPI") to preserve unusual
	// capitalization or punctuation; when empty the generator title-cases
	// Name as a fallback. The generate command also fills this from a
	// matching catalog entry's display_name when available.
	DisplayName string `yaml:"display_name,omitempty" json:"display_name,omitempty"`
	// DisplayNameDerivedFromTitle marks OpenAPI parser fallbacks from
	// info.title. Catalog enrichment may replace that fallback, but must not
	// replace explicit display_name / x-display-name values.
	DisplayNameDerivedFromTitle bool `yaml:"-" json:"-"`
	// Description describes the API itself ("REST API for ordering pizza").
	// It flows into generated docs and SKILL.md but is intentionally NOT used
	// as the printed CLI's --help text; that's CLIDescription's job.
	Description string `yaml:"description" json:"description"`
	// CLIDescription, when set, becomes the printed CLI's root cobra command
	// `Short:` text. Spec authors should phrase it as what the CLI does
	// ("Order Seattle pizza from the terminal"), not what the API is. When
	// blank the generator falls back to the research narrative's headline,
	// then to a generic "Manage <api> resources via the <api> API". Adding
	// this field eliminates a recurring manual rewrite step that the main
	// skill used to instruct Claude to perform after every generation.
	CLIDescription string `yaml:"cli_description,omitempty" json:"cli_description,omitempty"`
	Version        string `yaml:"version" json:"version"`
	BaseURL        string `yaml:"base_url" json:"base_url"`
	BasePath       string `yaml:"base_path,omitempty" json:"base_path,omitempty"`
	// GraphQLEndpointPath is the path appended to BaseURL for GraphQL POSTs.
	// REST specs leave it empty; GraphQL specs default it to "/graphql" but
	// can override (e.g., Shopify's "/admin/api/{version}/graphql.json").
	// The split exists because some GraphQL APIs put the endpoint behind a
	// per-tenant subdomain or version segment, and the old single-BaseURL
	// model couldn't represent that without hardcoding "/graphql" in the
	// generated client.
	GraphQLEndpointPath string `yaml:"graphql_endpoint_path,omitempty" json:"graphql_endpoint_path,omitempty"`
	// EndpointTemplateVars lists placeholder names embedded in BaseURL or
	// GraphQLEndpointPath as {var} (e.g., ["shop", "version"]). The
	// generator emits per-variable env-var lookups in the printed CLI's
	// config so users can resolve them at runtime. PR-1 carries this field
	// as plumbing only; PR-2 wires the runtime substitution.
	EndpointTemplateVars []string            `yaml:"endpoint_template_vars,omitempty" json:"endpoint_template_vars,omitempty"`
	Owner                string              `yaml:"owner,omitempty" json:"owner,omitempty"`                   // GitHub owner for import paths and Homebrew tap
	OwnerName            string              `yaml:"owner_name,omitempty" json:"owner_name,omitempty"`         // Display name (e.g. "Trevin Chow") for prose surfaces — Hermes author:, README byline. Distinct from Owner (slug) which drives module paths and copyright headers.
	Kind                 string              `yaml:"kind,omitempty" json:"kind,omitempty"`                     // "rest" (default) or "synthetic" — synthetic CLIs aggregate multiple sources beyond the spec; dogfood's path-validity check is relaxed accordingly
	SpecSource           string              `yaml:"spec_source,omitempty" json:"spec_source,omitempty"`       // official, community, sniffed, docs — affects generated client defaults
	ClientPattern        string              `yaml:"client_pattern,omitempty" json:"client_pattern,omitempty"` // rest (default), proxy-envelope — affects generated HTTP client
	HTTPTransport        string              `yaml:"http_transport,omitempty" json:"http_transport,omitempty"` // standard (default for official APIs), browser-chrome, or browser-chrome-h3
	ProxyRoutes          map[string]string   `yaml:"proxy_routes,omitempty" json:"proxy_routes,omitempty"`     // path prefix → service name for proxy-envelope routing
	BearerRefresh        BearerRefreshConfig `yaml:"bearer_refresh,omitempty" json:"bearer_refresh,omitzero"`  // live-source metadata for rotating public client bearer tokens
	WebsiteURL           string              `yaml:"website_url,omitempty" json:"website_url,omitempty"`       // product/company website (not the API base URL)
	Category             string              `yaml:"category,omitempty" json:"category,omitempty"`             // catalog category (e.g., productivity, developer-tools) — used for library install path
	Auth                 AuthConfig          `yaml:"auth" json:"auth"`
	TierRouting          TierRoutingConfig   `yaml:"tier_routing,omitempty" json:"tier_routing,omitzero"`
	RequiredHeaders      []RequiredHeader    `yaml:"required_headers,omitempty" json:"required_headers,omitempty"`
	Config               ConfigSpec          `yaml:"config" json:"config"`
	Resources            map[string]Resource `yaml:"resources" json:"resources"`
	Types                map[string]TypeDef  `yaml:"types" json:"types"`
	ExtraCommands        []ExtraCommand      `yaml:"extra_commands,omitempty" json:"extra_commands,omitempty"` // hand-written cobra commands declared so SKILL.md can document them; spec-only metadata, no code generated
	Cache                CacheConfig         `yaml:"cache,omitempty" json:"cache"`                             // cache freshness + auto-refresh config; when enabled, generated read commands auto-refresh stale local data before serving
	Share                ShareConfig         `yaml:"share,omitempty" json:"share"`                             // git-backed snapshot sharing config; when enabled, emits a `share` subcommand that publishes/subscribes to a git repo
	MCP                  MCPConfig           `yaml:"mcp,omitempty" json:"mcp"`                                 // MCP server generation config; when unset, the emitted MCP binary is stdio-only (today's default). Opting into http adds a --transport/--addr flag surface so the same binary can serve cloud-hosted agents.
	Throttling           ThrottlingConfig    `yaml:"throttling,omitempty" json:"throttling"`                   // cost-based throttling config; when Enabled with a recognized Shape, the generator emits a ThrottleState (generic harness) plus a per-Shape parser that reads the API's cost bucket. Only the "shopify" Shape ships in v1.
}

type TierRoutingConfig struct {
	DefaultTier string                `yaml:"default_tier,omitempty" json:"default_tier,omitempty"`
	Tiers       map[string]TierConfig `yaml:"tiers,omitempty" json:"tiers,omitempty"`
}

type TierConfig struct {
	BaseURL            string     `yaml:"base_url,omitempty" json:"base_url,omitempty"`
	Auth               AuthConfig `yaml:"auth,omitempty" json:"auth,omitzero"`
	AllowCrossHostAuth bool       `yaml:"allow_cross_host_auth,omitempty" json:"allow_cross_host_auth,omitempty"`
}

func (s *APISpec) HasTierRouting() bool {
	if s == nil {
		return false
	}
	return s.TierRouting.DefaultTier != "" || len(s.TierRouting.Tiers) > 0
}

func (s *APISpec) EffectiveTier(resource Resource, endpoint Endpoint) string {
	name, _, ok := s.EffectiveTierConfig(resource, endpoint)
	if !ok {
		return ""
	}
	return name
}

func (s *APISpec) EffectiveTierConfig(resource Resource, endpoint Endpoint) (string, TierConfig, bool) {
	if s == nil || !s.HasTierRouting() {
		return "", TierConfig{}, false
	}
	tierName := strings.TrimSpace(endpoint.Tier)
	if tierName == "" {
		tierName = strings.TrimSpace(resource.Tier)
	}
	if tierName == "" {
		tierName = strings.TrimSpace(s.TierRouting.DefaultTier)
	}
	if tierName == "" {
		return "", TierConfig{}, false
	}
	tier, ok := s.TierRouting.Tiers[tierName]
	return tierName, tier, ok
}

func (s *APISpec) EffectiveEndpointAuth(resource Resource, endpoint Endpoint) (authType string, noAuth bool) {
	if endpoint.NoAuth {
		return TierAuthTypeNone, true
	}
	authType = strings.TrimSpace(s.Auth.Type)
	if _, tier, ok := s.EffectiveTierConfig(resource, endpoint); ok {
		authType = normalizeTierAuthType(tier.Auth.Type)
	}
	if authType == TierAuthTypeNone {
		return TierAuthTypeNone, true
	}
	return authType, false
}

func (s *APISpec) EffectiveSubEndpointAuth(parent Resource, subResource Resource, endpoint Endpoint) (authType string, noAuth bool) {
	effectiveSub := subResource
	if effectiveSub.Tier == "" {
		effectiveSub.Tier = parent.Tier
	}
	return s.EffectiveEndpointAuth(effectiveSub, endpoint)
}

// ThrottleShape names the API-specific cost-bucket parser the generator
// wires into the GraphQL client. The generic harness (bucket math, retry,
// --throttle-mode flag) is shape-agnostic; only the parser that reads the
// API's response into a ThrottleStatus differs per shape, because every
// API surfaces its calculated cost in a different place. Adding a new
// shape means: (1) add a constant here, (2) extend validateThrottling to
// accept it, (3) add the parser block to graphql_client.go.tmpl gated on
// `eq .Throttling.Shape "<name>"`. No core code changes.
type ThrottleShape string

const (
	// ThrottleShapeShopify reads `extensions.cost.throttleStatus.{maximumAvailable,
	// currentlyAvailable,restoreRate}` from each GraphQL response. This is the
	// only shape supported in v1; GitHub's queryable `rateLimit` field and
	// Datadog's header-based cost limits will need their own shapes (and the
	// GitHub case will need a query-rewrite layer, since rateLimit is a schema
	// field rather than a response extension).
	ThrottleShapeShopify ThrottleShape = "shopify"
)

// ThrottlingConfig opts a printed CLI into the cost-based throttling
// primitives. Enabled turns the surface on (--throttle-mode flag,
// ThrottleState, budget projection, retry helper); default off so existing
// CLIs regenerate byte-identically when this field is unset. Shape selects
// the per-API parser and is required when Enabled is true; see ThrottleShape
// for the valid values and how to add new ones.
//
// Authors opt in by writing `throttling: { enabled: true, shape: shopify }`.
type ThrottlingConfig struct {
	Enabled bool          `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Shape   ThrottleShape `yaml:"shape,omitempty" json:"shape,omitempty"`
}

// validateThrottling ensures Shape is set and recognized when Enabled is
// true. Off case returns nil so unrelated specs aren't penalized for never
// opting in.
func validateThrottling(c ThrottlingConfig) error {
	if !c.Enabled {
		return nil
	}
	switch c.Shape {
	case ThrottleShapeShopify:
		return nil
	case "":
		return fmt.Errorf("throttling.shape is required when throttling.enabled is true (valid: %q)", ThrottleShapeShopify)
	default:
		return fmt.Errorf("throttling.shape %q is not recognized (valid: %q)", c.Shape, ThrottleShapeShopify)
	}
}

// HasCostThrottling reports whether the spec opts into cost-based throttling
// primitives. Used by the generator to gate emission of throttle.go and the
// related conditional blocks in client.go / graphql_client.go / root.go.
// Specs without this flag regenerate byte-identical to the pre-PR-3 output.
func (s *APISpec) HasCostThrottling() bool {
	return s != nil && s.Throttling.Enabled
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

// EffectiveDisplayName returns the human-readable brand name for this CLI.
// Explicit DisplayName wins (preserves "Company GOAT", "Cal.com", "PokéAPI"
// shape); otherwise we title-case Name. Used by the MCP server's protocol
// name, the MCPB manifest, and any surface that wants a friendly identity
// instead of the kebab-case slug.
func (s *APISpec) EffectiveDisplayName() string {
	if s == nil {
		return ""
	}
	if strings.TrimSpace(s.DisplayName) != "" {
		return s.DisplayName
	}
	return naming.HumanName(s.Name)
}

func (s *APISpec) EffectiveHTTPTransport() string {
	if s == nil {
		return HTTPTransportStandard
	}
	switch s.HTTPTransport {
	case HTTPTransportStandard, HTTPTransportBrowserChrome, HTTPTransportBrowserChromeH3:
		return s.HTTPTransport
	}
	switch s.SpecSource {
	case "community", "sniffed":
		return HTTPTransportBrowserChrome
	default:
		return HTTPTransportStandard
	}
}

func (s *APISpec) UsesBrowserHTTPTransport() bool {
	switch s.EffectiveHTTPTransport() {
	case HTTPTransportBrowserChrome, HTTPTransportBrowserChromeH3:
		return true
	default:
		return false
	}
}

func (s *APISpec) UsesBrowserHTTP3Transport() bool {
	return s.EffectiveHTTPTransport() == HTTPTransportBrowserChromeH3
}

func (s *APISpec) UsesBrowserManagedUserAgent() bool {
	switch s.EffectiveHTTPTransport() {
	case HTTPTransportBrowserChrome, HTTPTransportBrowserChromeH3:
		return true
	default:
		return false
	}
}

func (s *APISpec) HasHTMLExtraction() bool {
	if s == nil {
		return false
	}
	for _, resource := range s.Resources {
		if resourceHasHTMLExtraction(resource) {
			return true
		}
	}
	return false
}

func resourceHasHTMLExtraction(resource Resource) bool {
	for _, endpoint := range resource.Endpoints {
		if endpoint.UsesHTMLResponse() {
			return true
		}
	}
	for _, sub := range resource.SubResources {
		if resourceHasHTMLExtraction(sub) {
			return true
		}
	}
	return false
}

// HasHTMLExtractMode reports whether any endpoint in the spec declares
// html_extract with the given effective mode. Used by the html_extract
// template to gate per-mode helpers: a CLI that uses only
// HTMLExtractModeEmbeddedJSON does not need the page-mode DOM walkers
// or links-mode anchor parsing, and vice versa.
//
// `mode` should be one of the HTMLExtractMode* constants. Modes that
// don't appear in any endpoint return false; modes are matched by their
// effective value (so an unset Mode counts as page).
func (s *APISpec) HasHTMLExtractMode(mode string) bool {
	if s == nil {
		return false
	}
	target := strings.ToLower(strings.TrimSpace(mode))
	if target == "" {
		return false
	}
	for _, resource := range s.Resources {
		if resourceHasHTMLExtractMode(resource, target) {
			return true
		}
	}
	return false
}

func resourceHasHTMLExtractMode(resource Resource, mode string) bool {
	for _, endpoint := range resource.Endpoints {
		if !endpoint.UsesHTMLResponse() {
			continue
		}
		if strings.ToLower(endpoint.HTMLExtract.EffectiveMode()) == mode {
			return true
		}
	}
	for _, sub := range resource.SubResources {
		if resourceHasHTMLExtractMode(sub, mode) {
			return true
		}
	}
	return false
}

// RequiredHeader represents a non-auth header that the API requires on most
// requests (e.g., cal-api-version, Stripe-Version, anthropic-version).
// Detected automatically from OpenAPI specs when a required header parameter
// appears on >80% of operations.
type RequiredHeader struct {
	Name  string `yaml:"name" json:"name"`
	Value string `yaml:"value" json:"value"`
}

type BearerRefreshConfig struct {
	BundleURL string `yaml:"bundle_url,omitempty" json:"bundle_url,omitempty"`
	Pattern   string `yaml:"pattern,omitempty" json:"pattern,omitempty"`
}

func (c BearerRefreshConfig) Enabled() bool {
	return strings.TrimSpace(c.BundleURL) != "" || strings.TrimSpace(c.Pattern) != ""
}

type AuthConfig struct {
	Type             string       `yaml:"type" json:"type"` // api_key, oauth2, bearer_token, cookie, composed, session_handshake, none
	Header           string       `yaml:"header" json:"header"`
	Format           string       `yaml:"format" json:"format"`
	EnvVars          []string     `yaml:"env_vars" json:"env_vars"`
	EnvVarSpecs      []AuthEnvVar `yaml:"env_var_specs,omitempty" json:"env_var_specs,omitempty"`
	Optional         bool         `yaml:"optional,omitempty" json:"optional,omitempty"` // true when the key enhances a subset of features (e.g., USDA nutrition backfill) rather than gating core functionality; doctor treats unconfigured optional auth as INFO not FAIL and README frames the section as "Optional"
	Scheme           string       `yaml:"scheme,omitempty" json:"scheme,omitempty"`     // OpenAPI security scheme name
	In               string       `yaml:"in,omitempty" json:"in,omitempty"`             // header, query, cookie
	KeyURL           string       `yaml:"key_url,omitempty" json:"key_url,omitempty"`   // URL where users can register for an API key
	Title            string       `yaml:"title,omitempty" json:"title,omitempty"`       // user-facing credential field title for install/config surfaces
	Description      string       `yaml:"description,omitempty" json:"description,omitempty"`
	AuthorizationURL string       `yaml:"authorization_url,omitempty" json:"authorization_url,omitempty"`
	TokenURL         string       `yaml:"token_url,omitempty" json:"token_url,omitempty"`
	Scopes           []string     `yaml:"scopes,omitempty" json:"scopes,omitempty"`
	CookieDomain     string       `yaml:"cookie_domain,omitempty" json:"cookie_domain,omitempty"` // domain to read browser cookies from (e.g. ".notion.so")
	Cookies          []string     `yaml:"cookies,omitempty" json:"cookies,omitempty"`             // named cookies to extract for composed auth (e.g. ["customerId", "authToken"])
	Inferred         bool         `yaml:"inferred,omitempty" json:"inferred,omitempty"`           // true when auth was inferred from spec description, not declared in securitySchemes

	// VerifyPath is an optional path appended to base_url that the doctor
	// command probes to validate credentials. Set this to a known-good
	// authenticated GET endpoint that returns 2xx for any valid token (e.g.
	// "/me?fields=id" for Meta, "/v1/account" for Stripe, "/user" for GitHub,
	// "/users/@me" for Discord). When empty, doctor falls back to probing
	// the bare base URL and classifies 401/403 as "inconclusive" rather than
	// "invalid", because many versioned API roots return 401 regardless of
	// token validity (the path isn't a routed endpoint, but the gateway
	// still demands credentials in a meaningful context).
	VerifyPath string `yaml:"verify_path,omitempty" json:"verify_path,omitempty"`

	// Browser-session verification fields. Used when a website-facing CLI
	// depends on browser-derived cookies or clearance state for its required
	// happy path. The generator emits validation and proof handling, and the
	// shipcheck pipeline treats a missing proof as a blocker.
	RequiresBrowserSession         bool   `yaml:"requires_browser_session,omitempty" json:"requires_browser_session,omitempty"`
	BrowserSessionReason           string `yaml:"browser_session_reason,omitempty" json:"browser_session_reason,omitempty"`
	BrowserSessionValidationPath   string `yaml:"browser_session_validation_path,omitempty" json:"browser_session_validation_path,omitempty"`
	BrowserSessionValidationMethod string `yaml:"browser_session_validation_method,omitempty" json:"browser_session_validation_method,omitempty"`

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

	// OAuth2Grant selects the OAuth2 sub-flow when Type=="oauth2". Defaults
	// to authorization_code; ignored for non-oauth2 types. Read via
	// EffectiveOAuth2Grant() so the default lives in one place.
	OAuth2Grant string `yaml:"oauth2_grant,omitempty" json:"oauth2_grant,omitempty"`
}

type AuthEnvVar struct {
	Name        string         `yaml:"name" json:"name"`
	Kind        AuthEnvVarKind `yaml:"kind,omitempty" json:"kind,omitempty"`
	Required    bool           `yaml:"required" json:"required"`
	Sensitive   bool           `yaml:"sensitive" json:"sensitive"` // orthogonal to Kind; drives redaction policy
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Inferred    bool           `yaml:"inferred,omitempty" json:"inferred,omitempty"`
}

type AuthEnvVarKind string

const (
	AuthEnvVarKindPerCall       AuthEnvVarKind = "per_call"
	AuthEnvVarKindAuthFlowInput AuthEnvVarKind = "auth_flow_input"
	AuthEnvVarKindHarvested     AuthEnvVarKind = "harvested"
)

// EffectiveKind treats legacy empty kinds as per-call credentials.
func (v AuthEnvVar) EffectiveKind() AuthEnvVarKind {
	if v.Kind == "" {
		return AuthEnvVarKindPerCall
	}
	return v.Kind
}

// IsRequestCredential reports whether this env var can satisfy request auth.
func (v AuthEnvVar) IsRequestCredential() bool {
	return v.EffectiveKind() == AuthEnvVarKindPerCall
}

func (k AuthEnvVarKind) SensitivePlaceholder() string {
	switch k {
	case AuthEnvVarKindPerCall:
		return "Set to your API credential."
	case AuthEnvVarKindAuthFlowInput:
		return "Set during initial auth setup."
	case AuthEnvVarKindHarvested:
		return "Populated automatically by auth login."
	default:
		return ""
	}
}

func (v AuthEnvVar) MarkdownDescription() string {
	if v.Sensitive {
		return v.Kind.SensitivePlaceholder()
	}
	description := strings.ReplaceAll(v.Description, "|", `\|`)
	description = strings.ReplaceAll(description, "\r\n", " ")
	description = strings.ReplaceAll(description, "\n", " ")
	return strings.ReplaceAll(description, "\r", " ")
}

// CanonicalEnvVar returns the deterministic canonical entry for human-prose surfaces.
func (c *AuthConfig) CanonicalEnvVar() *AuthEnvVar {
	if c == nil {
		return nil
	}
	c.NormalizeEnvVarSpecs("")
	for i := range c.EnvVarSpecs {
		if c.EnvVarSpecs[i].IsRequestCredential() && c.EnvVarSpecs[i].Required {
			return &c.EnvVarSpecs[i]
		}
	}
	if len(c.EnvVarSpecs) > 0 {
		return &c.EnvVarSpecs[0]
	}
	return nil
}

// IsAuthEnvVarORCase reports whether all EnvVarSpecs are non-required per_call vars.
// In this shape, no single var is the canonical credential; the runtime tries each
// in turn and returns the first non-empty value. Returns false when EnvVarSpecs has
// fewer than 2 entries, any entry is Required, or any entry is not Kind=per_call.
func (c *AuthConfig) IsAuthEnvVarORCase() bool {
	if c == nil || len(c.EnvVarSpecs) < 2 {
		return false
	}
	for _, ev := range c.EnvVarSpecs {
		if !ev.IsRequestCredential() || ev.Required {
			return false
		}
	}
	return true
}

func (c *AuthConfig) NormalizeEnvVarSpecs(context string) {
	if c == nil {
		return
	}
	if len(c.EnvVarSpecs) > 0 {
		canonicalNames := make([]string, 0, len(c.EnvVarSpecs))
		canonical := true
		for _, envVar := range c.EnvVarSpecs {
			name := strings.TrimSpace(envVar.Name)
			if name == "" {
				continue
			}
			if envVar.Name != name || envVar.Kind == "" {
				canonical = false
				break
			}
			canonicalNames = append(canonicalNames, name)
		}
		if canonical {
			envVarNames := make([]string, 0, len(c.EnvVars))
			for _, name := range c.EnvVars {
				if name = strings.TrimSpace(name); name != "" {
					envVarNames = append(envVarNames, name)
				}
			}
			if sameStringSlice(envVarNames, canonicalNames) {
				return
			}
		}
	}
	if len(c.EnvVarSpecs) == 0 {
		if len(c.EnvVars) == 0 {
			return
		}
		c.EnvVarSpecs = make([]AuthEnvVar, 0, len(c.EnvVars))
		for _, name := range c.EnvVars {
			if name = strings.TrimSpace(name); name != "" {
				c.EnvVarSpecs = append(c.EnvVarSpecs, AuthEnvVar{
					Name:      name,
					Kind:      AuthEnvVarKindPerCall,
					Required:  true,
					Sensitive: true,
					Inferred:  true,
				})
			}
		}
		return
	}

	specNames := make([]string, 0, len(c.EnvVarSpecs))
	for i := range c.EnvVarSpecs {
		c.EnvVarSpecs[i].Name = strings.TrimSpace(c.EnvVarSpecs[i].Name)
		if c.EnvVarSpecs[i].Name == "" {
			continue
		}
		if c.EnvVarSpecs[i].Kind == "" {
			c.EnvVarSpecs[i].Kind = AuthEnvVarKindPerCall
		}
		specNames = append(specNames, c.EnvVarSpecs[i].Name)
	}
	if len(c.EnvVars) > 0 && !sameStringSlice(c.EnvVars, specNames) && !AllAuthEnvVarSpecsInferred(c.EnvVarSpecs) {
		if context == "" {
			context = "auth"
		}
		fmt.Fprintf(os.Stderr, "warning: %s env_vars disagree with env_var_specs; using env_var_specs\n", context)
		c.EnvVars = specNames
		return
	}
	if len(c.EnvVars) == 0 || sameStringSlice(c.EnvVars, specNames) {
		c.EnvVars = specNames
	}
}

func AllAuthEnvVarSpecsInferred(envVarSpecs []AuthEnvVar) bool {
	if len(envVarSpecs) == 0 {
		return false
	}
	for _, envVar := range envVarSpecs {
		if !envVar.Inferred {
			return false
		}
	}
	return true
}

func sameStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}

// OAuth2GrantAuthorizationCode is the 3-legged user-OAuth flow (browser
// redirect, callback server, code exchange at TokenURL).
const OAuth2GrantAuthorizationCode = "authorization_code"

// OAuth2GrantClientCredentials is the 2-legged server-to-server flow used
// by M2M APIs (Auth0 Management, Microsoft Graph daemon apps): POST to
// TokenURL with form-encoded client_id/client_secret, no user redirect.
const OAuth2GrantClientCredentials = "client_credentials"

// EffectiveOAuth2Grant returns the configured OAuth2 grant type, defaulting
// to OAuth2GrantAuthorizationCode when unset.
func (c AuthConfig) EffectiveOAuth2Grant() string {
	if strings.TrimSpace(c.OAuth2Grant) == "" {
		return OAuth2GrantAuthorizationCode
	}
	return c.OAuth2Grant
}

// validateOAuth2Grant ensures OAuth2Grant is empty or one of the supported
// values. Empty is accepted (treated as the default). Cross-checking against
// AuthConfig.Type is intentionally skipped: the field is ignored for
// non-oauth2 types, matching how SessionTTLHours and similar fields behave.
func validateOAuth2Grant(c AuthConfig) error {
	switch c.OAuth2Grant {
	case "", OAuth2GrantAuthorizationCode, OAuth2GrantClientCredentials:
		return nil
	default:
		return fmt.Errorf("auth.oauth2_grant %q is not recognized (valid: %q, %q)",
			c.OAuth2Grant, OAuth2GrantAuthorizationCode, OAuth2GrantClientCredentials)
	}
}

// validateSessionHandshake enforces fail-fast on session_handshake auth specs
// that would otherwise emit silently-broken Go code: missing token_param_name
// produces q.Set("", token); a SessionTokenURL is required to bootstrap; and
// token_param_in is byte-compared in the template, so spec authors who write
// "Header" or "QUERY" silently route to the wrong attachment path.
func validateSessionHandshake(c AuthConfig) error {
	if c.Type != "session_handshake" {
		return nil
	}
	if c.SessionTokenURL == "" {
		return fmt.Errorf("auth.session_token_url is required when auth.type is %q", c.Type)
	}
	if c.TokenParamName == "" {
		return fmt.Errorf("auth.token_param_name is required when auth.type is %q", c.Type)
	}
	switch c.TokenParamIn {
	case "", "header", "query":
		return nil
	default:
		return fmt.Errorf("auth.token_param_in %q is not recognized (valid: %q, %q)", c.TokenParamIn, "header", "query")
	}
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
	Commands       []CacheCommand    `yaml:"commands,omitempty" json:"commands,omitempty"`               // optional custom command-path coverage for hand-authored store-backed reads. Generated resource commands are covered automatically.
}

// CacheCommand declares that a hand-authored command path reads one or more
// syncable resources and should participate in the generated freshness hook.
type CacheCommand struct {
	Name      string   `yaml:"name" json:"name"`           // lowercase cobra command path, without the binary name (e.g., "today" or "insights stale")
	Resources []string `yaml:"resources" json:"resources"` // resource names to refresh before serving the command
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

// MCPConfig declares how the generated MCP server binary is shaped. When empty
// the generator emits today's behavior: a stdio-only server via server.ServeStdio.
// Opting http into Transport adds a --transport flag (stdio|http) and, for http,
// an --addr flag so the same binary can also serve an HTTP streamable transport.
//
// Rationale: stdio-only servers can only reach clients that share a filesystem
// and can spawn a subprocess. Cloud-hosted agents (hosted Claude Code sessions,
// Managed Agents, web clients) cannot, so they need a remote transport option.
// Declaring transports in the spec rather than inferring at generate time keeps
// the decision visible and reviewable in the published CLI's source spec.
//
// Allowed Transport values: "stdio", "http". An empty list is treated as
// ["stdio"] for backward compatibility. Unknown values are rejected at spec
// load; this prevents silent drift when new transports are introduced.
type MCPConfig struct {
	Transport              []string `yaml:"transport,omitempty" json:"transport,omitempty"`                             // allowed transports the generated binary compiles support for; empty == [stdio]. Runtime transport is chosen via the --transport flag and PP_MCP_TRANSPORT env.
	Addr                   string   `yaml:"addr,omitempty" json:"addr,omitempty"`                                       // default bind address for the http transport (e.g., ":7777"). Blank means runtime default (":7777"). Ignored unless http is in Transport.
	Intents                []Intent `yaml:"intents,omitempty" json:"intents,omitempty"`                                 // higher-level MCP tools that compose multiple endpoint calls. The agent sees one intent tool; the generator emits a handler that fans out to the declared endpoints sequentially. Anti-pattern to fight: one-tool-per-endpoint mirrors that force agents to stitch primitives.
	EndpointTools          string   `yaml:"endpoint_tools,omitempty" json:"endpoint_tools,omitempty"`                   // "visible" (default) keeps the per-endpoint MCP tools; "hidden" suppresses them so only intents + generator-emitted tools appear. Use "hidden" when intents fully cover the surface and raw endpoints would be noise.
	Orchestration          string   `yaml:"orchestration,omitempty" json:"orchestration,omitempty"`                     // "endpoint-mirror" (default), "intent", or "code". Code-orchestration emits a thin <api>_search + <api>_execute pair covering the full surface in ~1K tokens; used for very large APIs where even intent-grouped tools would overflow context. Mutually exclusive with endpoint-mirror at emission time.
	OrchestrationThreshold int      `yaml:"orchestration_threshold,omitempty" json:"orchestration_threshold,omitempty"` // endpoint count above which the generator warns that code-orchestration would be a better default. Zero means use the built-in default (50).
}

// Intent declares an MCP tool that composes multiple endpoint calls into a
// single agent-facing operation. The generator emits one handler per intent
// that resolves bindings, calls each endpoint in order against the CLI's
// existing HTTP client, and returns the captured value named by Returns.
//
// Binding syntax — each value in a step's Bind map is a string expression:
//   - `${input.<name>}`    resolves to the MCP request's input parameter
//   - `${<capture>.<field>}` resolves to a field of a previous step's captured JSON response
//   - anything else is used as a string literal
//
// Type coercion: all bound values are rendered as strings at runtime; JSON
// bodies for POST/PUT/PATCH are built from the resolved map. The intent
// surface intentionally does not support array indexing, conditional
// branching, or looping in v1 — those escapes belong in U3's code-orchestration
// pattern, not here.
type Intent struct {
	Name        string        `yaml:"name" json:"name"`                           // MCP tool name; snake_case, unique within the spec
	Description string        `yaml:"description" json:"description"`             // agent-facing description; should name the *intent*, not the endpoints
	Params      []IntentParam `yaml:"params,omitempty" json:"params,omitempty"`   // input parameters the intent tool exposes to MCP callers
	Steps       []IntentStep  `yaml:"steps" json:"steps"`                         // ordered list of endpoint calls; at least one required
	Returns     string        `yaml:"returns,omitempty" json:"returns,omitempty"` // capture name whose value is returned to the caller; defaults to the last step's capture when blank
}

// IntentParam mirrors a narrow slice of the endpoint Param type. Kept small by
// design: intents are compositions, so parameter shapes should be simple
// string/int/bool inputs that bind into step calls, not full nested bodies.
type IntentParam struct {
	Name        string `yaml:"name" json:"name"`
	Type        string `yaml:"type" json:"type"` // one of: string, integer, boolean
	Required    bool   `yaml:"required,omitempty" json:"required,omitempty"`
	Description string `yaml:"description" json:"description"`
}

// IntentStep declares one endpoint call inside an intent. Endpoint references
// are `resource.endpoint` or `resource.sub_resource.endpoint`; the validator
// confirms the path resolves against APISpec.Resources at spec load.
type IntentStep struct {
	Endpoint string            `yaml:"endpoint" json:"endpoint"`                   // dotted path into the spec's resources, e.g., "messages.get_thread"
	Bind     map[string]string `yaml:"bind,omitempty" json:"bind,omitempty"`       // map of endpoint param name -> binding expression
	Capture  string            `yaml:"capture,omitempty" json:"capture,omitempty"` // name to bind this step's response under for subsequent steps / returns; must be unique within the intent
}

// EffectiveTransports returns the transports the generated binary should
// support, defaulting to stdio when none are declared. Using this helper from
// templates and the generator avoids sprinkling the default in two places.
func (m MCPConfig) EffectiveTransports() []string {
	if len(m.Transport) == 0 {
		return []string{"stdio"}
	}
	return m.Transport
}

// HasTransport reports whether t is among the effective transports for this
// MCPConfig. Case-insensitive on the comparison since spec authors may write
// "HTTP" and the generator normalizes to lowercase at validation time.
func (m MCPConfig) HasTransport(t string) bool {
	for _, v := range m.EffectiveTransports() {
		if strings.EqualFold(v, t) {
			return true
		}
	}
	return false
}

type Resource struct {
	Description string   `yaml:"description" json:"description"`
	Path        string   `yaml:"path,omitempty" json:"path,omitempty"`             // base path for operations shorthand (e.g., /api/items)
	Operations  []string `yaml:"operations,omitempty" json:"operations,omitempty"` // shorthand: list, get, create, update, delete, search
	// BaseURL overrides the spec-level BaseURL for this resource's
	// endpoints. Fixed at generation time. Incompatible with the
	// proxy-envelope client pattern, which POSTs every request to a
	// single URL.
	BaseURL      string              `yaml:"base_url,omitempty" json:"base_url,omitempty"`
	Tier         string              `yaml:"tier,omitempty" json:"tier,omitempty"`
	Endpoints    map[string]Endpoint `yaml:"endpoints" json:"endpoints"`
	SubResources map[string]Resource `yaml:"sub_resources,omitempty" json:"sub_resources,omitempty"`
}

// HasResourceBaseURLOverride reports whether any resource (top-level or
// nested sub-resource) declares a BaseURL override. Used by the client
// template to gate the absolute-URL detection branch — specs that don't
// opt in regenerate byte-identically.
func (s *APISpec) HasResourceBaseURLOverride() bool {
	if s == nil {
		return false
	}
	for _, resource := range s.Resources {
		if resourceHasBaseURLOverride(resource) {
			return true
		}
	}
	return false
}

func resourceHasBaseURLOverride(resource Resource) bool {
	if resource.BaseURL != "" {
		return true
	}
	for _, sub := range resource.SubResources {
		if resourceHasBaseURLOverride(sub) {
			return true
		}
	}
	return false
}

type Endpoint struct {
	Method          string            `yaml:"method" json:"method"`
	Path            string            `yaml:"path" json:"path"`
	Description     string            `yaml:"description" json:"description"`
	Params          []Param           `yaml:"params" json:"params"`
	Body            []Param           `yaml:"body" json:"body"`
	Response        ResponseDef       `yaml:"response" json:"response"`
	ResponseFormat  string            `yaml:"response_format,omitempty" json:"response_format,omitempty"` // json (default) or html
	HTMLExtract     *HTMLExtract      `yaml:"html_extract,omitempty" json:"html_extract,omitempty"`       // extraction options when response_format is html
	Pagination      *Pagination       `yaml:"pagination" json:"pagination"`
	ResponsePath    string            `yaml:"response_path,omitempty" json:"response_path,omitempty"`       // path to extract data array from response (e.g., "data", "results.items")
	Meta            map[string]string `yaml:"meta,omitempty" json:"meta,omitempty"`                         // per-endpoint metadata (e.g., source_tier, source_count from crowd-sniff)
	HeaderOverrides []RequiredHeader  `yaml:"header_overrides,omitempty" json:"header_overrides,omitempty"` // per-endpoint header overrides (e.g., different api-version)
	NoAuth          bool              `yaml:"no_auth,omitempty" json:"no_auth,omitempty"`                   // true when the endpoint does not require authentication
	Tier            string            `yaml:"tier,omitempty" json:"tier,omitempty"`
	// IDField is the resolved primary-key field name for items returned by this
	// endpoint, populated either by a path-item-level `x-resource-id` extension
	// or, for OpenAPI specs, by walking the response schema (id → name → first
	// required scalar). Empty when no key could be resolved; templates fall back
	// to runtime list scanning. Internal YAML specs may set this directly.
	IDField string `yaml:"id_field,omitempty" json:"id_field,omitempty"`
	// Critical flags this endpoint's resource as essential to a sync run. When
	// true, a per-resource failure is treated as a hard failure even under the
	// new (non-strict) exit-code policy. Populated from the path-item-level
	// `x-critical` extension on OpenAPI specs; defaults to false.
	Critical bool   `yaml:"critical,omitempty" json:"critical,omitempty"`
	Alias    string `yaml:"-" json:"-"` // computed, not from YAML
}

func (e Endpoint) EffectiveResponseFormat() string {
	if strings.TrimSpace(e.ResponseFormat) == "" {
		return ResponseFormatJSON
	}
	return e.ResponseFormat
}

func (e Endpoint) UsesHTMLResponse() bool {
	return e.EffectiveResponseFormat() == ResponseFormatHTML
}

type HTMLExtract struct {
	Mode         string   `yaml:"mode,omitempty" json:"mode,omitempty"`                   // page (default), links, or embedded-json
	LinkPrefixes []string `yaml:"link_prefixes,omitempty" json:"link_prefixes,omitempty"` // URL path prefixes to keep when extracting links (mode: links)
	Limit        int      `yaml:"limit,omitempty" json:"limit,omitempty"`                 // max links to return; defaults at runtime (mode: links)
	// ScriptSelector identifies the <script> tag containing serialized
	// page state when mode is embedded-json. Defaults to
	// DefaultEmbeddedJSONScriptSelector ("script#__NEXT_DATA__") when
	// empty — the most common Next.js pages-router shape. Other SSR
	// frameworks declare per-site selectors (Nuxt: "script#__NUXT__",
	// etc.). Selector grammar is the simple "tag" / "tag#id" form
	// supported by the runtime extractor; expand later if needed.
	ScriptSelector string `yaml:"script_selector,omitempty" json:"script_selector,omitempty"`
	// JSONPath is a dot-notation walk into the parsed JSON inside the
	// matched script tag (mode: embedded-json). For Next.js the typical
	// value is "props.pageProps.<route-data>"; for Nuxt "data.<route>".
	// Empty path returns the entire parsed JSON. Missing intermediate
	// keys yield a typed-empty result rather than an error.
	JSONPath string `yaml:"json_path,omitempty" json:"json_path,omitempty"`
}

func (h *HTMLExtract) EffectiveMode() string {
	if h == nil || strings.TrimSpace(h.Mode) == "" {
		return HTMLExtractModePage
	}
	return h.Mode
}

// EffectiveScriptSelector returns the configured script selector, or the
// default Next.js pages-router selector when unset. Only meaningful when
// EffectiveMode() == HTMLExtractModeEmbeddedJSON.
func (h *HTMLExtract) EffectiveScriptSelector() string {
	if h == nil || strings.TrimSpace(h.ScriptSelector) == "" {
		return DefaultEmbeddedJSONScriptSelector
	}
	return h.ScriptSelector
}

type Param struct {
	Name        string   `yaml:"name" json:"name"`
	FlagName    string   `yaml:"flag_name,omitempty" json:"flag_name,omitempty"`
	Aliases     []string `yaml:"aliases,omitempty" json:"aliases,omitempty"`
	Type        string   `yaml:"type" json:"type"`
	Required    bool     `yaml:"required" json:"required"`
	Positional  bool     `yaml:"positional" json:"positional"`
	PathParam   bool     `yaml:"path_param,omitempty" json:"path_param,omitempty"` // true for path params rendered as flags (e.g., pagination)
	Default     any      `yaml:"default" json:"default"`
	Description string   `yaml:"description" json:"description"`
	Fields      []Param  `yaml:"fields" json:"fields"`                     // for nested objects
	Enum        []string `yaml:"enum,omitempty" json:"enum,omitempty"`     // enum constraints for the parameter
	Format      string   `yaml:"format,omitempty" json:"format,omitempty"` // OpenAPI format hints (date-time, email, uri, etc.)
	// IdentName, when set, overrides Name for Go identifier and CLI flag
	// derivation (camel/flagName). Name remains the wire-side parameter name
	// used in URLs, JSON keys, and path substitution. Populated by the
	// generator's flag-collision dedup pass when two params on the same
	// endpoint would otherwise produce identical Go identifiers or CLI flag
	// names — for example Twilio's StartTime/StartTime>/StartTime< all
	// collapsing to "StartTime" through camelization. Most params leave this
	// empty and template helpers fall back to Name.
	IdentName string `yaml:"-" json:"-"`
	// FlagNameSet is true when the spec explicitly contained flag_name.
	// It lets validation distinguish an omitted public name from invalid
	// `flag_name: ""` while still allowing overlays to clear FlagName.
	FlagNameSet bool `yaml:"-" json:"-"`
}

func (p Param) PublicInputName() string {
	if p.FlagName != "" {
		return p.FlagName
	}
	if p.IdentName != "" {
		return publicInputNameFromIdent(p.IdentName)
	}
	return p.Name
}

func publicInputNameFromIdent(name string) string {
	name = strings.TrimLeft(name, "$")
	var b strings.Builder
	runes := []rune(name)
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			if b.Len() > 0 {
				b.WriteByte('-')
			}
			continue
		}
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				b.WriteByte('-')
			} else if unicode.IsUpper(prev) && i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
				b.WriteByte('-')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	result := b.String()
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	return strings.Trim(result, "-")
}

func (p *Param) UnmarshalYAML(value *yaml.Node) error {
	type paramAlias Param
	var out paramAlias
	if err := value.Decode(&out); err != nil {
		return err
	}
	*p = Param(out)
	p.FlagNameSet = yamlMappingHasKey(value, "flag_name")
	return nil
}

func (p *Param) UnmarshalJSON(data []byte) error {
	type paramAlias Param
	var out paramAlias
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*p = Param(out)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err == nil {
		_, p.FlagNameSet = raw["flag_name"]
	}
	return nil
}

func yamlMappingHasKey(value *yaml.Node, key string) bool {
	if value == nil || value.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(value.Content); i += 2 {
		if value.Content[i].Value == key {
			return true
		}
	}
	return false
}

type ResponseDef struct {
	Type          string                 `yaml:"type" json:"type"` // object, array
	Item          string                 `yaml:"item" json:"item"` // type name
	Discriminator *ResponseDiscriminator `yaml:"discriminator,omitempty" json:"discriminator,omitempty"`
}

type ResponseDiscriminator struct {
	Field   string            `yaml:"field" json:"field"`
	Mapping map[string]string `yaml:"mapping,omitempty" json:"mapping,omitempty"` // discriminator value -> schema/resource name
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
	Name string   `yaml:"name" json:"name"`
	Type string   `yaml:"type" json:"type"`
	Enum []string `yaml:"enum,omitempty" json:"enum,omitempty"`
	// Selection is an optional GraphQL sub-selection rendered when this field
	// is used in a generated GraphQL query. It lets wrapper specs keep the Go
	// field simple (for example, totalPriceSet as json.RawMessage) while still
	// issuing valid nested GraphQL selections.
	Selection string `yaml:"selection,omitempty" json:"selection,omitempty"`
	// IdentName overrides Name for Go-identifier derivation when two field
	// names in the same struct sanitize to the same identifier through
	// camel-casing. Wire-side serialization always reads Name.
	IdentName string `yaml:"-" json:"-"`
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
	s.enrichPathParams()
	if err := s.validateReservedNames(); err != nil {
		return nil, err
	}
	if err := s.validateFrameworkCobraCollisions(); err != nil {
		return nil, err
	}
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("validation: %w", err)
	}
	return &s, nil
}

// ReservedCLIResourceNames is the set of resource names that would collide with
// reserved single-file templates emitted into the printed CLI's internal/cli/
// directory. Two collisions occur if a spec uses one of these as a resource
// name: the resource template's <name>.go overwrites the reserved file (losing
// helpers like FeedbackEndpointConfigured() from feedback.go), AND the
// resource's `new<Name>Cmd` cobra-builder shadows the reserved template's
// same-named function, breaking the build with a redeclaration error.
//
// Renaming the file alone is not enough; the function-name collision still
// breaks the build. Reject at parse time and ask the author to rename the
// resource (e.g., `feedback` → `customer_feedback`, `auth` → `accounts`).
//
// The contract is intentionally stable: removing an entry is allowed only when
// the corresponding reserved template is also removed from the generator.
var ReservedCLIResourceNames = map[string]struct{}{
	"agent_context":    {},
	"api_discovery":    {},
	"auth":             {},
	"auto_refresh":     {},
	"cache":            {},
	"channel_workflow": {},
	"client":           {},
	"data_source":      {},
	"deliver":          {},
	"doctor":           {},
	"export":           {},
	"feedback":         {},
	"helpers":          {},
	"html_extract":     {},
	"import":           {},
	"profile":          {},
	"refresh_bearer":   {},
	"root":             {},
	"search":           {},
	"share_commands":   {},
	"sync":             {},
	"tail":             {},
	"types":            {},
	"which":            {},
	"workflow":         {},
}

// ReservedCobraUseNames is the set of cobra command names registered at
// the top level of every printed CLI's root cobra tree. A spec resource
// whose name maps to one of these would shadow the framework command at
// runtime. Distinct from ReservedCLIResourceNames above: that set
// protects template-file overwrites (snake_case); this set protects
// cobra-Use shadowing (kebab-case). Hand-maintained; drift invariants
// enforced by tests in reserved_drift_test.go.
var ReservedCobraUseNames = map[string]struct{}{
	"about":          {},
	"agent-context":  {},
	"analytics":      {},
	"api":            {},
	"auth":           {},
	"completion":     {},
	"doctor":         {},
	"export":         {},
	"feedback":       {},
	"health":         {},
	"help":           {},
	"import":         {},
	"jobs":           {},
	"load":           {},
	"orphans":        {},
	"profile":        {},
	"refresh-bearer": {},
	"search":         {},
	"share":          {},
	"similar":        {},
	"sql":            {},
	"stale":          {},
	"sync":           {},
	"tail":           {},
	"version":        {},
	"which":          {},
	"workflow":       {},
}

// validateReservedNames rejects specs whose top-level resource names would
// collide with reserved Printing Press templates. Sub-resource names are not
// checked because they emit under a parent prefix (`<parent>_<sub>.go`,
// `new<Parent><Sub>Cmd`) that does not collide with single-file templates.
func (s *APISpec) validateReservedNames() error {
	for name := range s.Resources {
		if _, reserved := ReservedCLIResourceNames[name]; reserved {
			return fmt.Errorf("resource name %q collides with a reserved Printing Press template (would overwrite internal/cli/%s.go and produce a duplicate `new%sCmd` function). Rename the resource — e.g. %q",
				name, name, snakeToPascal(name), name+"_resource")
		}
	}
	return nil
}

// validateFrameworkCobraCollisions rejects specs whose top-level resource
// names would shadow a framework cobra subcommand at runtime. Sub-resources
// are exempt — they register under a parent prefix and never reach the
// root.
func (s *APISpec) validateFrameworkCobraCollisions() error {
	for name := range s.Resources {
		kebab := snakeToKebab(name)
		if _, reserved := ReservedCobraUseNames[kebab]; reserved {
			suggestion := name + "_resource"
			if s.Name != "" {
				suggestion = snakeToKebab(s.Name) + "_" + name
			}
			return fmt.Errorf("resource name %q would shadow framework cobra command %q at runtime (every printed CLI registers `<cli> %s` as a built-in). Rename the resource — e.g. %q",
				name, kebab, kebab, suggestion)
		}
	}
	return nil
}

func snakeToKebab(s string) string {
	return strings.ReplaceAll(strings.ToLower(s), "_", "-")
}

// snakeToPascal converts a snake_case identifier to PascalCase so error
// messages name the same Go function the generator would emit. Mirrors
// generator.toCamel for snake_case input — kept here so the spec package
// has no import-cycle dependency on the generator. Empty input → empty
// output.
func snakeToPascal(s string) string {
	if s == "" {
		return s
	}
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// pathParamRe matches `{name}` placeholders in a path template. Names are
// alphanumeric/underscore — the conservative subset every parser observed in
// the wild uses. Anchoring on `{` and `}` keeps it from over-matching JSON
// fragments accidentally embedded in path strings.
var pathParamRe = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

var orGroupTokenRe = regexp.MustCompile(`\b[A-Z][A-Z0-9_]*\b`)

// enrichPathParams walks every resource and sub-resource endpoint and ensures
// each `{paramName}` placeholder in the endpoint path is represented in
// Endpoint.Params with Positional: true, Required: true. The expandOperations
// path already populates these for shorthand-generated endpoints; explicit
// `endpoints:` blocks in the YAML do not, so without this step the generator
// emits a literal-placeholder URL with no positional-arg parsing — every
// path-templated request returns 404 at runtime.
//
// Existing Params are never modified. If a placeholder name already appears
// in Endpoint.Params or Endpoint.Body, the placeholder is left alone — the
// author is presumed to have declared it intentionally (with their own type,
// description, or default).
//
// Order is preserved: placeholders are appended in the order they appear in
// the path so generated cobra `Args: cobra.ExactArgs(N)` sites and the
// matching `replacePathParam(...args[i])` calls line up.
func (s *APISpec) enrichPathParams() {
	for resourceName, r := range s.Resources {
		s.enrichResourcePathParams(&r)
		s.Resources[resourceName] = r
	}
}

func (s *APISpec) enrichResourcePathParams(r *Resource) {
	if r.Endpoints != nil {
		for endpointName, e := range r.Endpoints {
			enrichEndpointPathParams(&e)
			r.Endpoints[endpointName] = e
		}
	}
	for subName, sub := range r.SubResources {
		s.enrichResourcePathParams(&sub)
		r.SubResources[subName] = sub
	}
}

func enrichEndpointPathParams(e *Endpoint) {
	if e.Path == "" {
		return
	}
	matches := pathParamRe.FindAllStringSubmatch(e.Path, -1)
	if len(matches) == 0 {
		return
	}
	// Build a set of names already declared so we never duplicate or overwrite
	// an author-provided Param/Body entry.
	declared := make(map[string]struct{}, len(e.Params)+len(e.Body))
	for _, p := range e.Params {
		declared[p.Name] = struct{}{}
	}
	for _, p := range e.Body {
		declared[p.Name] = struct{}{}
	}
	// Track which placeholders we've already appended in this pass so a
	// repeated placeholder (rare but valid) doesn't add the param twice.
	seen := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		name := m[1]
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		if _, exists := declared[name]; exists {
			// The path template wins over how the author declared the param.
			// A placeholder like {cik} in /submissions/CIK{cik}.json is a path
			// substitution regardless of whether the author wrote location:query
			// or omitted location entirely. Promote the existing param so URL
			// substitution and MCP positionalParams emission see it as such.
			// Use PathParam=true (not Positional=true) to preserve the author's
			// CLI-rendering intent — a param can be a flag that also fills a
			// path slot (e.g. pagination, dates).
			for i := range e.Params {
				if e.Params[i].Name == name {
					e.Params[i].PathParam = true
					e.Params[i].Required = true
					break
				}
			}
			continue
		}
		e.Params = append(e.Params, Param{
			Name:        name,
			Type:        "string",
			Required:    true,
			Positional:  true,
			Description: name,
		})
	}
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
		singularName := singularize(name)
		idParam := singularName + "Id"
		idPath := r.Path + "/{" + idParam + "}"
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
					Path:        idPath,
					Description: "Get a " + singularName + " by ID",
					Params:      operationIDParams(idParam, singularName),
				}
			case "create":
				r.Endpoints["create"] = Endpoint{
					Method:      "POST",
					Path:        r.Path,
					Description: "Create a new " + singularName,
				}
			case "update":
				r.Endpoints["update"] = Endpoint{
					Method:      "PATCH",
					Path:        idPath,
					Description: "Update a " + singularName,
					Params:      operationIDParams(idParam, singularName),
				}
			case "delete":
				r.Endpoints["delete"] = Endpoint{
					Method:      "DELETE",
					Path:        idPath,
					Description: "Delete a " + singularName,
					Params:      operationIDParams(idParam, singularName),
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

func operationIDParams(idParam, singularName string) []Param {
	return []Param{{
		Name:        idParam,
		Type:        "string",
		Required:    true,
		Positional:  true,
		Description: singularName + " ID",
	}}
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
	s.NormalizeAuthEnvVarSpecs()
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
	switch s.HTTPTransport {
	case "", HTTPTransportStandard, HTTPTransportBrowserChrome, HTTPTransportBrowserChromeH3:
	default:
		return fmt.Errorf("http_transport must be one of: standard, browser-chrome, browser-chrome-h3")
	}
	if err := validateExtraCommands(s.ExtraCommands); err != nil {
		return err
	}
	if err := validateCacheShare(s.Cache, s.Share, s.Resources); err != nil {
		return err
	}
	if err := validateMCP(s.MCP, s.Resources); err != nil {
		return err
	}
	if err := validateThrottling(s.Throttling); err != nil {
		return err
	}
	if err := validateBearerRefresh(s); err != nil {
		return err
	}
	if err := validateOAuth2Grant(s.Auth); err != nil {
		return err
	}
	if err := validateSessionHandshake(s.Auth); err != nil {
		return err
	}
	if err := validateAuthEnvVarSpecs("auth", s.Auth); err != nil {
		return err
	}
	if err := validateTierRouting(s); err != nil {
		return err
	}
	if s.ClientPattern == "proxy-envelope" && s.HasResourceBaseURLOverride() {
		return fmt.Errorf("resource base_url overrides are incompatible with client_pattern=proxy-envelope; the proxy POSTs every request to the spec-level BaseURL, so per-resource overrides would be silently ignored")
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
			if err := validateEndpointPublicParamNames(e); err != nil {
				return fmt.Errorf("resource %q endpoint %q: %w", name, eName, err)
			}
			if err := validateEndpointResponseFormat(e); err != nil {
				return fmt.Errorf("resource %q endpoint %q: %w", name, eName, err)
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
				if err := validateEndpointPublicParamNames(e); err != nil {
					return fmt.Errorf("resource %q sub-resource %q endpoint %q: %w", name, subName, eName, err)
				}
				if err := validateEndpointResponseFormat(e); err != nil {
					return fmt.Errorf("resource %q sub-resource %q endpoint %q: %w", name, subName, eName, err)
				}
			}
		}
	}
	return nil
}

func (s *APISpec) NormalizeAuthEnvVarSpecs() {
	if s == nil {
		return
	}
	s.Auth.NormalizeEnvVarSpecs("auth")
	if !s.HasTierRouting() {
		return
	}
	for name, tier := range s.TierRouting.Tiers {
		tier.Auth.NormalizeEnvVarSpecs(fmt.Sprintf("tier_routing.tiers.%s.auth", name))
		s.TierRouting.Tiers[name] = tier
	}
}

var publicParamNameRe = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

func validateEndpointPublicParamNames(endpoint Endpoint) error {
	if err := validatePublicParamNameList("params", endpoint.Params); err != nil {
		return err
	}
	if err := validatePublicParamNameList("body", endpoint.Body); err != nil {
		return err
	}
	return nil
}

func validatePublicParamNameList(context string, params []Param) error {
	seen := map[string]string{}
	for i, p := range params {
		label := fmt.Sprintf("%s[%d] (%s)", context, i, p.Name)
		if p.FlagNameSet && p.FlagName == "" {
			return fmt.Errorf("%s: flag_name must not be empty", label)
		}
		if p.FlagName != "" {
			if !publicParamNameRe.MatchString(p.FlagName) {
				return fmt.Errorf("%s: flag_name %q must be lowercase kebab-case", label, p.FlagName)
			}
			if previous, ok := seen[p.FlagName]; ok {
				return fmt.Errorf("%s: flag_name %q collides with %s", label, p.FlagName, previous)
			}
			seen[p.FlagName] = label + " flag_name"
		}
		publicName := p.FlagName
		if publicName == "" && publicParamNameRe.MatchString(p.Name) {
			publicName = p.Name
		}
		for ai, alias := range p.Aliases {
			aliasLabel := fmt.Sprintf("%s aliases[%d]", label, ai)
			if alias == "" {
				return fmt.Errorf("%s: alias must not be empty", aliasLabel)
			}
			if !publicParamNameRe.MatchString(alias) {
				return fmt.Errorf("%s: alias %q must be lowercase kebab-case", aliasLabel, alias)
			}
			if publicName != "" && alias == publicName {
				return fmt.Errorf("%s: alias %q duplicates its public name", aliasLabel, alias)
			}
			if previous, ok := seen[alias]; ok {
				return fmt.Errorf("%s: alias %q collides with %s", aliasLabel, alias, previous)
			}
			seen[alias] = aliasLabel
		}
	}
	return nil
}

func validateAuthEnvVarSpecs(context string, auth AuthConfig) error {
	seen := map[string]struct{}{}
	for i, envVar := range auth.EnvVarSpecs {
		name := strings.TrimSpace(envVar.Name)
		if name == "" {
			return fmt.Errorf("%s.env_var_specs[%d].name is required", context, i)
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("%s.env_var_specs contains duplicate name %q", context, name)
		}
		seen[name] = struct{}{}
		switch envVar.Kind {
		case "", AuthEnvVarKindPerCall, AuthEnvVarKindAuthFlowInput, AuthEnvVarKindHarvested:
		default:
			return fmt.Errorf("%s.env_var_specs[%d].kind %q is not recognized (valid: %q, %q, %q)",
				context, i, envVar.Kind, AuthEnvVarKindPerCall, AuthEnvVarKindAuthFlowInput, AuthEnvVarKindHarvested)
		}
	}
	if example, ok := independentAuthORGroupsExample(auth.EnvVarSpecs); ok {
		return fmt.Errorf("%s: detected 2+ independent OR-groups in EnvVarSpecs (e.g., %s). The current model encodes OR-group membership via per-var Required=false + description text and supports at most one OR-group per auth block; multi-OR-group specs are not supported. Either consolidate to a single OR-group (mark all non-required entries as members of one group via cross-referencing descriptions), or require all credentials (Required=true)", context, example)
	}
	return nil
}

func independentAuthORGroupsExample(envVarSpecs []AuthEnvVar) (string, bool) {
	names := make(map[string]struct{}, len(envVarSpecs))
	for _, envVar := range envVarSpecs {
		name := strings.TrimSpace(envVar.Name)
		if name != "" {
			names[name] = struct{}{}
		}
	}

	members := make([]AuthEnvVar, 0, len(envVarSpecs))
	for _, envVar := range envVarSpecs {
		name := strings.TrimSpace(envVar.Name)
		if name == "" || !envVar.IsRequestCredential() || envVar.Required || !strings.Contains(envVar.Description, " OR ") {
			continue
		}
		referencesSibling := false
		for _, token := range orGroupTokenRe.FindAllString(envVar.Description, -1) {
			if token == name {
				continue
			}
			if _, ok := names[token]; ok {
				referencesSibling = true
				break
			}
		}
		if !referencesSibling {
			continue
		}
		members = append(members, envVar)
	}
	if len(members) < 2 {
		return "", false
	}

	parent := make(map[string]string, len(members))
	for _, member := range members {
		parent[member.Name] = member.Name
	}
	var find func(string) string
	find = func(name string) string {
		if parent[name] != name {
			parent[name] = find(parent[name])
		}
		return parent[name]
	}
	union := func(a, b string) {
		rootA, rootB := find(a), find(b)
		if rootA != rootB {
			parent[rootB] = rootA
		}
	}

	for _, member := range members {
		for _, token := range orGroupTokenRe.FindAllString(member.Description, -1) {
			if token == member.Name {
				continue
			}
			if _, inGroup := parent[token]; inGroup {
				union(member.Name, token)
			}
		}
	}

	groups := map[string][]string{}
	order := make([]string, 0, len(members))
	for _, member := range members {
		root := find(member.Name)
		if _, ok := groups[root]; !ok {
			order = append(order, root)
		}
		groups[root] = append(groups[root], member.Name)
	}
	if len(groups) < 2 {
		return "", false
	}

	parts := make([]string, 0, len(order))
	for _, root := range order {
		parts = append(parts, strings.Join(groups[root], " OR "))
	}
	return strings.Join(parts, "; "), true
}

func validateBearerRefresh(s *APISpec) error {
	cfg := s.BearerRefresh
	if !cfg.Enabled() {
		return nil
	}
	if s.Auth.Type != "bearer_token" {
		return fmt.Errorf(`bearer_refresh requires auth.type "bearer_token"`)
	}
	if s.Auth.OAuth2Grant == OAuth2GrantClientCredentials {
		return fmt.Errorf("bearer_refresh is incompatible with auth.oauth2_grant %q", OAuth2GrantClientCredentials)
	}
	if s.HasTierRouting() {
		return fmt.Errorf("bearer_refresh is incompatible with tier_routing auth")
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

func validateTierRouting(s *APISpec) error {
	if s == nil || !s.HasTierRouting() {
		return nil
	}
	if s.ClientPattern == "proxy-envelope" {
		return fmt.Errorf("tier_routing is incompatible with client_pattern=proxy-envelope; tier routing needs per-request base URL and auth selection")
	}
	if len(s.TierRouting.Tiers) == 0 {
		return fmt.Errorf("tier_routing.tiers is required when tier_routing is declared")
	}
	if s.TierRouting.DefaultTier != "" {
		if _, ok := s.TierRouting.Tiers[s.TierRouting.DefaultTier]; !ok {
			return fmt.Errorf("tier_routing.default_tier %q references unknown tier", s.TierRouting.DefaultTier)
		}
	}
	anyTierBaseURL := false
	for name, tier := range s.TierRouting.Tiers {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("tier_routing.tiers contains an empty tier name")
		}
		if strings.TrimSpace(tier.BaseURL) != "" {
			anyTierBaseURL = true
		}
		if err := validateAuthEnvVarSpecs(fmt.Sprintf("tier_routing.tiers.%s.auth", name), tier.Auth); err != nil {
			return err
		}
		if err := validateTier(name, tier, s.BaseURL); err != nil {
			return err
		}
	}
	if anyTierBaseURL && s.HasResourceBaseURLOverride() {
		return fmt.Errorf("resource base_url overrides are incompatible with tier_routing tier base_url overrides; choose one routing source")
	}
	for name, resource := range s.Resources {
		if err := validateTierRoutingResource(s, name, resource, "", ""); err != nil {
			return err
		}
	}
	return nil
}

func validateTier(name string, tier TierConfig, specBaseURL string) error {
	authType := normalizeTierAuthType(tier.Auth.Type)
	switch authType {
	case TierAuthTypeNone, TierAuthTypeAPIKey, TierAuthTypeBearerToken:
	default:
		return fmt.Errorf("tier_routing tier %q uses unsupported auth type %q; supported tier auth types are none, api_key, bearer_token", name, tier.Auth.Type)
	}
	if !tierAuthRequiresCredential(tier.Auth) {
		return nil
	}
	if len(tier.Auth.EnvVars) == 0 && len(tier.Auth.EnvVarSpecs) == 0 {
		return fmt.Errorf("tier_routing tier %q auth.env_vars or auth.env_var_specs is required for %s auth", name, authType)
	}
	if err := validateTierAuthPlacement(name, tier.Auth); err != nil {
		return err
	}
	if err := validateTierAuthFormat(name, tier.Auth); err != nil {
		return err
	}
	if err := validateCredentialTierBaseURL(name, tier, specBaseURL); err != nil {
		return err
	}
	return nil
}

func normalizeTierAuthType(authType string) string {
	authType = strings.TrimSpace(authType)
	if authType == "" {
		return TierAuthTypeNone
	}
	return authType
}

func tierAuthRequiresCredential(auth AuthConfig) bool {
	switch normalizeTierAuthType(auth.Type) {
	case TierAuthTypeAPIKey, TierAuthTypeBearerToken:
		return true
	default:
		return false
	}
}

func validateTierAuthPlacement(name string, auth AuthConfig) error {
	placement := strings.TrimSpace(auth.In)
	switch placement {
	case "", TierAuthPlacementHeader, TierAuthPlacementQuery:
	default:
		return fmt.Errorf("tier_routing tier %q auth.in must be header or query", name)
	}
	if placement == TierAuthPlacementQuery && strings.TrimSpace(auth.Header) == "" {
		return fmt.Errorf("tier_routing tier %q auth.header is required as the query parameter name", name)
	}
	if normalizeTierAuthType(auth.Type) == TierAuthTypeAPIKey && strings.TrimSpace(auth.Header) == "" {
		return fmt.Errorf("tier_routing tier %q auth.header is required for api_key auth", name)
	}
	return nil
}

var authFormatPlaceholderRe = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func validateTierAuthFormat(name string, auth AuthConfig) error {
	if strings.TrimSpace(auth.Format) == "" {
		return nil
	}
	allowed := map[string]struct{}{
		"token":        {},
		"access_token": {},
	}
	for _, envVar := range auth.EnvVars {
		allowed[envVar] = struct{}{}
		allowed[naming.EnvVarPlaceholder(envVar)] = struct{}{}
	}
	for _, match := range authFormatPlaceholderRe.FindAllStringSubmatch(auth.Format, -1) {
		if _, ok := allowed[match[1]]; !ok {
			return fmt.Errorf("tier_routing tier %q auth.format references undeclared placeholder %q", name, match[1])
		}
	}
	return nil
}

func validateCredentialTierBaseURL(name string, tier TierConfig, specBaseURL string) error {
	if strings.TrimSpace(tier.BaseURL) == "" {
		return nil
	}
	parsed, err := url.Parse(tier.BaseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("tier_routing tier %q base_url must be an absolute URL", name)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("tier_routing tier %q base_url must use https when carrying credentials", name)
	}
	host := normalizeURLHost(parsed.Hostname())
	if unsafe := unsafeCredentialHostReason(host); unsafe != "" {
		return fmt.Errorf("tier_routing tier %q base_url host %q is %s and cannot receive generated credentials", name, host, unsafe)
	}
	specHost := hostnameFromURL(specBaseURL)
	if specHost == "" || sameHostFamily(specHost, host) || tier.AllowCrossHostAuth {
		return nil
	}
	return fmt.Errorf("tier_routing tier %q base_url host %q is cross-host from spec base_url host %q; set allow_cross_host_auth after review", name, host, specHost)
}

func hostnameFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return normalizeURLHost(parsed.Hostname())
}

func normalizeURLHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func unsafeCredentialHostReason(host string) string {
	switch {
	case host == "":
		return "empty"
	case host == "localhost" || strings.HasSuffix(host, ".localhost"):
		return "loopback"
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return ""
	}
	switch {
	case addr.IsLoopback():
		return "loopback"
	case addr.IsPrivate():
		return "private"
	case addr.IsLinkLocalUnicast():
		return "link-local"
	case addr.IsUnspecified():
		return "unspecified"
	default:
		return ""
	}
}

func sameHostFamily(specHost, tierHost string) bool {
	specHost = normalizeURLHost(specHost)
	tierHost = normalizeURLHost(tierHost)
	return tierHost == specHost || strings.HasSuffix(tierHost, "."+specHost)
}

func validateTierRoutingResource(s *APISpec, resourcePath string, resource Resource, inheritedTier, inheritedBaseURL string) error {
	resourceTier := strings.TrimSpace(resource.Tier)
	if resourceTier == "" {
		resourceTier = inheritedTier
	}
	resourceBaseURL := strings.TrimSpace(resource.BaseURL)
	if resourceBaseURL == "" {
		resourceBaseURL = inheritedBaseURL
	}
	if resourceTier != "" {
		if _, ok := s.TierRouting.Tiers[resourceTier]; !ok {
			return fmt.Errorf("resource %q references unknown tier %q", resourcePath, resourceTier)
		}
	}
	effectiveResource := resource
	effectiveResource.Tier = resourceTier
	for endpointName, endpoint := range resource.Endpoints {
		tierName, tier, ok := s.EffectiveTierConfig(effectiveResource, endpoint)
		if tierName == "" {
			continue
		}
		if !ok {
			return fmt.Errorf("resource %q endpoint %q references unknown tier %q", resourcePath, endpointName, tierName)
		}
		if endpoint.NoAuth && tierAuthRequiresCredential(tier.Auth) {
			return fmt.Errorf("resource %q endpoint %q declares no_auth but tier %q requires credentials", resourcePath, endpointName, tierName)
		}
		if tierAuthRequiresCredential(tier.Auth) && strings.TrimSpace(tier.BaseURL) == "" && resourceBaseURL != "" {
			resourceTier := tier
			resourceTier.BaseURL = resourceBaseURL
			if err := validateCredentialTierBaseURL(tierName, resourceTier, s.BaseURL); err != nil {
				return fmt.Errorf("resource %q endpoint %q: %w", resourcePath, endpointName, err)
			}
		}
	}
	for subName, sub := range resource.SubResources {
		if err := validateTierRoutingResource(s, resourcePath+"."+subName, sub, resourceTier, resourceBaseURL); err != nil {
			return err
		}
	}
	return nil
}

func validateEndpointResponseFormat(e Endpoint) error {
	switch e.ResponseFormat {
	case "", ResponseFormatJSON, ResponseFormatHTML:
	default:
		return fmt.Errorf("response_format must be one of: json, html")
	}
	if !e.UsesHTMLResponse() {
		return nil
	}
	switch strings.ToUpper(strings.TrimSpace(e.Method)) {
	case "GET", "HEAD":
	default:
		return fmt.Errorf("html response_format is only supported for GET/HEAD endpoints")
	}
	if e.HTMLExtract == nil {
		return nil
	}
	switch e.HTMLExtract.Mode {
	case "", HTMLExtractModePage, HTMLExtractModeLinks, HTMLExtractModeEmbeddedJSON:
	default:
		return fmt.Errorf("html_extract.mode must be one of: page, links, embedded-json")
	}
	if e.HTMLExtract.Limit < 0 {
		return fmt.Errorf("html_extract.limit must be >= 0")
	}
	// embedded-json-specific validation: script_selector defaults to
	// Next.js's __NEXT_DATA__ when empty, so it's not strictly required;
	// json_path is also optional (empty path returns the entire parsed
	// JSON). Both have defaults so embedded-json validates with no extra
	// fields set. Reject explicit empty json_path strings that contain
	// only whitespace as a sanity check; trim happens at use time.
	if e.HTMLExtract.Mode == HTMLExtractModeEmbeddedJSON {
		if strings.TrimSpace(e.HTMLExtract.ScriptSelector) == "" && e.HTMLExtract.ScriptSelector != "" {
			return fmt.Errorf("html_extract.script_selector cannot be whitespace-only")
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

func validateCacheShare(cache CacheConfig, share ShareConfig, resources map[string]Resource) error {
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
	if !cache.Enabled && len(cache.Commands) > 0 {
		return fmt.Errorf("cache.commands is set but cache.enabled is false; either enable cache or remove")
	}
	seenCommands := make(map[string]struct{}, len(cache.Commands))
	for i, command := range cache.Commands {
		if command.Name == "" {
			return fmt.Errorf("cache.commands[%d]: name is required", i)
		}
		if !extraCommandNameRe.MatchString(command.Name) {
			return fmt.Errorf("cache.commands[%d]: name %q must be lowercase command path (one to three segments separated by single spaces, lowercase letters, digits, and hyphens)", i, command.Name)
		}
		if _, dup := seenCommands[command.Name]; dup {
			return fmt.Errorf("cache.commands[%d]: name %q appears more than once", i, command.Name)
		}
		seenCommands[command.Name] = struct{}{}
		if len(command.Resources) == 0 {
			return fmt.Errorf("cache.commands[%d] (%s): resources must not be empty", i, command.Name)
		}
		seenResources := make(map[string]struct{}, len(command.Resources))
		for j, resource := range command.Resources {
			if resource == "" {
				return fmt.Errorf("cache.commands[%d].resources[%d]: resource name must not be empty", i, j)
			}
			if _, ok := resources[resource]; !ok {
				return fmt.Errorf("cache.commands[%d].resources[%d]: resource %q is not declared in resources", i, j, resource)
			}
			if _, dup := seenResources[resource]; dup {
				return fmt.Errorf("cache.commands[%d].resources[%d]: resource %q appears more than once", i, j, resource)
			}
			seenResources[resource] = struct{}{}
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

// allowedMCPTransports is the canonical set of transports a printed CLI may
// declare. Kept explicit (rather than computed from a broader registry) so a
// typo like "htpp" is caught at spec load with a clear error message naming
// the valid options, not silently carried through to a build failure in the
// template.
var allowedMCPTransports = map[string]struct{}{
	"stdio": {},
	"http":  {},
}

// addrLikeRe accepts a ":port" or "host:port" form for the optional MCP http
// bind address. Intentionally loose — the Go net package parses and reports a
// better runtime error; this is a spec-load sanity check to reject obvious
// typos (e.g., "7777" with no colon) early.
var addrLikeRe = regexp.MustCompile(`^[A-Za-z0-9.\-_]*:[0-9]+$`)

// validateMCP enforces the Transport allowlist and normalizes the Addr shape.
// An empty Transport is valid (default stdio); non-empty lists must contain
// only entries from allowedMCPTransports, with no duplicates.
func validateMCP(m MCPConfig, resources map[string]Resource) error {
	seen := make(map[string]struct{}, len(m.Transport))
	for i, t := range m.Transport {
		normalized := strings.ToLower(strings.TrimSpace(t))
		if normalized == "" {
			return fmt.Errorf("mcp.transport[%d]: value must not be empty", i)
		}
		if _, ok := allowedMCPTransports[normalized]; !ok {
			return fmt.Errorf("mcp.transport[%d]: %q is not a supported transport (allowed: stdio, http)", i, t)
		}
		if _, dup := seen[normalized]; dup {
			return fmt.Errorf("mcp.transport[%d]: %q appears more than once", i, t)
		}
		seen[normalized] = struct{}{}
	}
	if m.Addr != "" {
		if _, httpEnabled := seen["http"]; !httpEnabled {
			return fmt.Errorf("mcp.addr is set but mcp.transport does not include http; either add http or remove addr")
		}
		if !addrLikeRe.MatchString(m.Addr) {
			return fmt.Errorf("mcp.addr %q is not a valid bind address (expect \":port\" or \"host:port\")", m.Addr)
		}
	}
	if m.EndpointTools != "" && m.EndpointTools != "visible" && m.EndpointTools != "hidden" {
		return fmt.Errorf("mcp.endpoint_tools: %q must be \"visible\" or \"hidden\"", m.EndpointTools)
	}
	switch m.Orchestration {
	case "", "endpoint-mirror", "intent", "code":
	default:
		return fmt.Errorf("mcp.orchestration: %q must be one of endpoint-mirror, intent, code", m.Orchestration)
	}
	if m.OrchestrationThreshold < 0 {
		return fmt.Errorf("mcp.orchestration_threshold: %d must be non-negative", m.OrchestrationThreshold)
	}
	return validateIntents(m.Intents, resources)
}

// DefaultOrchestrationThreshold is the endpoint-count above which the
// generator recommends (but does not require) code-orchestration. At 50+
// endpoints, even intent-grouped tools tend to overflow an agent's usable
// context; code-orchestration covers the full surface in a pair of tools.
const DefaultOrchestrationThreshold = 50

// EffectiveOrchestrationThreshold returns the resolved threshold, applying
// the built-in default when the spec leaves it unset.
func (m MCPConfig) EffectiveOrchestrationThreshold() int {
	if m.OrchestrationThreshold <= 0 {
		return DefaultOrchestrationThreshold
	}
	return m.OrchestrationThreshold
}

// IsCodeOrchestration reports whether this MCP config opts into the
// code-orchestration thin surface. Templates branch on this to emit only
// <api>_search + <api>_execute instead of the endpoint-mirror.
func (m MCPConfig) IsCodeOrchestration() bool {
	return m.Orchestration == "code"
}

// intentNameRe enforces snake_case for MCP intent tool names so they line up
// with the snake_case convention used for endpoint-mirror tool names.
var intentNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// allowedIntentParamTypes matches the narrow type set IntentParam supports.
// Intents compose endpoints; complex shapes belong in the endpoint bodies.
var allowedIntentParamTypes = map[string]struct{}{
	"string": {}, "integer": {}, "boolean": {},
}

// bindingExprRe matches either ${input.<name>} or ${<capture>.<field>} where
// the path after the dot may contain additional dot-separated segments. The
// generator's runtime resolver walks the path on a map[string]any, so deep
// paths are supported even though the validator only peeks at the first
// segment to verify the reference target exists.
var bindingExprRe = regexp.MustCompile(`^\$\{([a-z][a-z0-9_]*)(\.[a-zA-Z0-9_]+)+\}$`)

func validateIntents(intents []Intent, resources map[string]Resource) error {
	seenNames := make(map[string]struct{}, len(intents))
	for i, intent := range intents {
		if intent.Name == "" {
			return fmt.Errorf("mcp.intents[%d]: name is required", i)
		}
		if !intentNameRe.MatchString(intent.Name) {
			return fmt.Errorf("mcp.intents[%d]: name %q must be snake_case (lowercase letters, digits, underscore)", i, intent.Name)
		}
		if _, dup := seenNames[intent.Name]; dup {
			return fmt.Errorf("mcp.intents[%d]: name %q appears more than once", i, intent.Name)
		}
		seenNames[intent.Name] = struct{}{}
		if intent.Description == "" {
			return fmt.Errorf("mcp.intents[%d] (%s): description is required", i, intent.Name)
		}
		if len(intent.Steps) == 0 {
			return fmt.Errorf("mcp.intents[%d] (%s): at least one step is required", i, intent.Name)
		}
		inputNames := make(map[string]struct{}, len(intent.Params))
		for pi, p := range intent.Params {
			if p.Name == "" {
				return fmt.Errorf("mcp.intents[%d] (%s): params[%d].name is required", i, intent.Name, pi)
			}
			if _, ok := allowedIntentParamTypes[p.Type]; !ok {
				return fmt.Errorf("mcp.intents[%d] (%s): params[%d] (%s): type %q must be one of string, integer, boolean", i, intent.Name, pi, p.Name, p.Type)
			}
			if _, dup := inputNames[p.Name]; dup {
				return fmt.Errorf("mcp.intents[%d] (%s): params[%d]: name %q appears more than once", i, intent.Name, pi, p.Name)
			}
			inputNames[p.Name] = struct{}{}
		}
		captures := make(map[string]struct{}, len(intent.Steps))
		for si, step := range intent.Steps {
			if step.Endpoint == "" {
				return fmt.Errorf("mcp.intents[%d] (%s): steps[%d].endpoint is required", i, intent.Name, si)
			}
			if _, ok := lookupEndpoint(resources, step.Endpoint); !ok {
				return fmt.Errorf("mcp.intents[%d] (%s): steps[%d].endpoint %q does not resolve against the spec's resources", i, intent.Name, si, step.Endpoint)
			}
			for paramName, expr := range step.Bind {
				if paramName == "" {
					return fmt.Errorf("mcp.intents[%d] (%s): steps[%d].bind: param name must not be empty", i, intent.Name, si)
				}
				if strings.HasPrefix(expr, "${") {
					m := bindingExprRe.FindStringSubmatch(expr)
					if m == nil {
						return fmt.Errorf("mcp.intents[%d] (%s): steps[%d].bind[%s]: %q is not a valid binding (expect ${input.<name>} or ${capture.<field>})", i, intent.Name, si, paramName, expr)
					}
					root := m[1]
					if root == "input" {
						fieldPath := strings.TrimPrefix(m[2], ".")
						firstSeg := strings.SplitN(fieldPath, ".", 2)[0]
						if _, ok := inputNames[firstSeg]; !ok {
							return fmt.Errorf("mcp.intents[%d] (%s): steps[%d].bind[%s]: %q references undeclared input %q", i, intent.Name, si, paramName, expr, firstSeg)
						}
					} else if _, ok := captures[root]; !ok {
						return fmt.Errorf("mcp.intents[%d] (%s): steps[%d].bind[%s]: %q references undeclared capture %q (captures must be defined in a prior step)", i, intent.Name, si, paramName, expr, root)
					}
				}
			}
			if step.Capture != "" {
				if step.Capture == "input" {
					return fmt.Errorf("mcp.intents[%d] (%s): steps[%d].capture: %q is reserved for intent inputs", i, intent.Name, si, step.Capture)
				}
				if _, dup := captures[step.Capture]; dup {
					return fmt.Errorf("mcp.intents[%d] (%s): steps[%d].capture %q appears more than once", i, intent.Name, si, step.Capture)
				}
				captures[step.Capture] = struct{}{}
			}
		}
		if intent.Returns != "" {
			if _, ok := captures[intent.Returns]; !ok {
				return fmt.Errorf("mcp.intents[%d] (%s): returns %q does not match any step capture", i, intent.Name, intent.Returns)
			}
		}
	}
	return nil
}

// lookupEndpoint resolves a dotted endpoint reference (`resource.endpoint` or
// `resource.sub_resource.endpoint`) against the spec's resource map. Returns
// the endpoint and whether it was found. The generator uses the same lookup
// to emit the right HTTP method and path at intent-handler emission time.
func lookupEndpoint(resources map[string]Resource, ref string) (Endpoint, bool) {
	parts := strings.Split(ref, ".")
	switch len(parts) {
	case 2:
		r, ok := resources[parts[0]]
		if !ok {
			return Endpoint{}, false
		}
		e, ok := r.Endpoints[parts[1]]
		return e, ok
	case 3:
		r, ok := resources[parts[0]]
		if !ok {
			return Endpoint{}, false
		}
		sub, ok := r.SubResources[parts[1]]
		if !ok {
			return Endpoint{}, false
		}
		e, ok := sub.Endpoints[parts[2]]
		return e, ok
	default:
		return Endpoint{}, false
	}
}

// CountMCPTools counts total endpoints and public (NoAuth) endpoints across
// all resources and sub-resources.
func (s *APISpec) CountMCPTools() (total, public int) {
	for _, r := range s.Resources {
		for _, e := range r.Endpoints {
			total++
			if _, noAuth := s.EffectiveEndpointAuth(r, e); noAuth {
				public++
			}
		}
		for _, sub := range r.SubResources {
			for _, e := range sub.Endpoints {
				total++
				if _, noAuth := s.EffectiveSubEndpointAuth(r, sub, e); noAuth {
					public++
				}
			}
		}
	}
	return
}
