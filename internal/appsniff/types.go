package appsniff

// TargetKind identifies the source artifact class analyzed by app-sniff.
type TargetKind string

const (
	TargetKindDesktop   TargetKind = "desktop"
	TargetKindCache     TargetKind = "cache"
	TargetKindAndroid   TargetKind = "android"
	TargetKindIOS       TargetKind = "ios"
	TargetKindSourceDir TargetKind = "source-dir"
)

// ProtocolKind classifies the wire protocol of a discovered endpoint.
type ProtocolKind string

const (
	ProtocolREST    ProtocolKind = "rest"
	ProtocolGraphQL ProtocolKind = "graphql"
	ProtocolRPC     ProtocolKind = "rpc"
	ProtocolGRPC    ProtocolKind = "grpc"
	ProtocolTRPC    ProtocolKind = "trpc"
)

// ConfidenceLevel buckets extraction confidence for ranking and downstream gates.
type ConfidenceLevel string

const (
	ConfidenceHigh   ConfidenceLevel = "high"
	ConfidenceMedium ConfidenceLevel = "medium"
	ConfidenceLow    ConfidenceLevel = "low"
)

// ReconcileState marks how a static endpoint matched an optional HAR capture.
type ReconcileState string

const (
	ReconcileNone         ReconcileState = ""
	ReconcileConfirmed    ReconcileState = "confirmed"
	ReconcileObservedOnly ReconcileState = "observed-only"
	ReconcileStaticOnly   ReconcileState = "static-only"
)

// AuthScheme identifies the inferred API authentication mechanism.
type AuthScheme string

const (
	AuthNone   AuthScheme = "none"
	AuthBearer AuthScheme = "bearer_token"
	AuthAPIKey AuthScheme = "api_key"
	AuthCookie AuthScheme = "cookie"
	AuthOAuth  AuthScheme = "oauth2"
	AuthBasic  AuthScheme = "basic"
)

// FrameworkKind identifies the application runtime detected inside a bundle.
type FrameworkKind string

const (
	FrameworkUnknown       FrameworkKind = "unknown"
	FrameworkElectron      FrameworkKind = "electron"
	FrameworkChromiumCache FrameworkKind = "chromium-cache"
	FrameworkNativeAndroid FrameworkKind = "native-android"
	FrameworkReactNative   FrameworkKind = "react-native"
	FrameworkHermes        FrameworkKind = "react-native-hermes"
	FrameworkFlutter       FrameworkKind = "flutter"
	FrameworkCapacitor     FrameworkKind = "capacitor"
	FrameworkNativeIOS     FrameworkKind = "native-ios"
)

// ParamIn identifies where an extracted parameter appears on the wire.
type ParamIn string

const (
	ParamInPath   ParamIn = "path"
	ParamInQuery  ParamIn = "query"
	ParamInHeader ParamIn = "header"
	ParamInBody   ParamIn = "body"
)

// EndpointParam describes a single parameter inferred from binary annotations,
// JS source patterns, or reconciled HAR traffic.
type EndpointParam struct {
	Name     string  `json:"name"`
	In       ParamIn `json:"in"`
	Type     string  `json:"type,omitempty"`
	Required bool    `json:"required,omitempty"`
}

// Endpoint is a single API route discovered by static analysis or HAR reconciliation.
type Endpoint struct {
	Method             string          `json:"method"`
	Path               string          `json:"path"`
	Host               string          `json:"host,omitempty"`
	BaseURL            string          `json:"base_url,omitempty"`
	Protocol           ProtocolKind    `json:"protocol"`
	Source             string          `json:"source"`
	Confidence         ConfidenceLevel `json:"confidence"`
	Params             []EndpointParam `json:"params,omitempty"`
	BodyKeys           []string        `json:"body_keys,omitempty"`
	RequestContentType string          `json:"request_content_type,omitempty"`
	HeaderNames        []string        `json:"header_names,omitempty"`
	ObservedAuth       []string        `json:"observed_auth,omitempty"`
	ReconcileState     ReconcileState  `json:"reconcile_state,omitempty"`
	Statuses           []int           `json:"statuses,omitempty"`
	SampleCount        int             `json:"sample_count,omitempty"`
	Notes              []string        `json:"notes,omitempty"`
}

// ShardRoute records a multi-shard or service-specific backend host discovered
// inside an application bundle.
type ShardRoute struct {
	Name   string `json:"name"`
	Host   string `json:"host"`
	Source string `json:"source,omitempty"`
}

// PinningFinding describes TLS certificate pinning or ATS protection detected
// statically in an app bundle. Detection is read-only; pinning is never bypassed.
type PinningFinding struct {
	Kind    string   `json:"kind"`
	Detail  string   `json:"detail"`
	Domains []string `json:"domains,omitempty"`
}

// AuthSignal summarizes authentication mechanisms inferred from app assets.
// Values are never recorded, only scheme, header/cookie names, and token URLs.
type AuthSignal struct {
	Scheme      AuthScheme `json:"scheme"`
	HeaderNames []string   `json:"header_names,omitempty"`
	CookieNames []string   `json:"cookie_names,omitempty"`
	TokenURLs   []string   `json:"token_urls,omitempty"`
	EnvVarHint  string     `json:"env_var_hint,omitempty"`
	Notes       []string   `json:"notes,omitempty"`
}

// BinaryMeta records non-sensitive metadata about the inspected app artifact.
type BinaryMeta struct {
	AppName        string        `json:"app_name,omitempty"`
	BundleID       string        `json:"bundle_id,omitempty"`
	PackageID      string        `json:"package_id,omitempty"`
	Version        string        `json:"version,omitempty"`
	BuildNumber    string        `json:"build_number,omitempty"`
	Platform       string        `json:"platform,omitempty"`
	Framework      FrameworkKind `json:"framework,omitempty"`
	Encrypted      bool          `json:"encrypted,omitempty"`
	EncryptionNote string        `json:"encryption_note,omitempty"`
	BaseURLs       []string      `json:"base_urls,omitempty"`
	ATSDomains     []string      `json:"ats_domains,omitempty"`
	Shards         []ShardRoute  `json:"shards,omitempty"`
}

// ReconciliationSummary records the outcome of comparing static binary endpoints
// against a captured HAR file.
type ReconciliationSummary struct {
	HARPath      string `json:"har_path"`
	Confirmed    int    `json:"confirmed"`
	ObservedOnly int    `json:"observed_only"`
	StaticOnly   int    `json:"static_only"`
}

// DiscoveryStats summarizes work performed during static extraction.
type DiscoveryStats struct {
	FilesScanned       int `json:"files_scanned"`
	FilesSkippedSecret int `json:"files_skipped_secret,omitempty"`
	DEXFilesParsed     int `json:"dex_files_parsed,omitempty"`
	RetrofitMethods    int `json:"retrofit_methods,omitempty"`
	ARSCStringsParsed  int `json:"arsc_strings_parsed,omitempty"`
	HermesStrings      int `json:"hermes_strings,omitempty"`
	RESTEndpoints      int `json:"rest_endpoints"`
	GRPCEndpoints      int `json:"grpc_endpoints"`
}

// DiscoveryResult is the canonical output of an app-sniff inspection pass.
type DiscoveryResult struct {
	Target         string                 `json:"target"`
	TargetKind     TargetKind             `json:"target_kind"`
	Meta           BinaryMeta             `json:"meta"`
	PrimaryBaseURL string                 `json:"primary_base_url,omitempty"`
	Endpoints      []Endpoint             `json:"endpoints"`
	GRPCEndpoints  []Endpoint             `json:"grpc_endpoints,omitempty"`
	Auth           AuthSignal             `json:"auth"`
	Pinning        []PinningFinding       `json:"pinning,omitempty"`
	Reconciliation *ReconciliationSummary `json:"reconciliation,omitempty"`
	Warnings       []string               `json:"warnings,omitempty"`
	Stats          DiscoveryStats         `json:"stats"`
}

// Options configures app-sniff extraction, filtering, and spec generation.
type Options struct {
	Name          string
	BaseURL       string
	HostFilters   []string
	Top           int
	IncludeGRPC   bool
	ScanCache     bool
	CacheDir      string
	HARPath       string
	MaxFileBytes  int64
	MaxTotalBytes int64
}

const (
	defaultMaxFileBytes  int64 = 32 * 1024 * 1024
	defaultMaxTotalBytes int64 = 256 * 1024 * 1024
)

func (o Options) maxFileBytes() int64 {
	if o.MaxFileBytes > 0 {
		return o.MaxFileBytes
	}
	return defaultMaxFileBytes
}

func (o Options) maxTotalBytes() int64 {
	if o.MaxTotalBytes > 0 {
		return o.MaxTotalBytes
	}
	return defaultMaxTotalBytes
}

// AuditReport compares discovered app endpoints, version, and shards against
// an existing printed CLI directory.
type AuditReport struct {
	CLIDir           string       `json:"cli_dir"`
	CLIVersion       string       `json:"cli_version,omitempty"`
	AppVersion       string       `json:"app_version,omitempty"`
	VersionDrift     bool         `json:"version_drift"`
	MissingShards    []ShardRoute `json:"missing_shards,omitempty"`
	CoveredCount     int          `json:"covered_count"`
	MissingCount     int          `json:"missing_count"`
	MissingEndpoints []Endpoint   `json:"missing_endpoints,omitempty"`
	MCPParityGaps    []string     `json:"mcp_parity_gaps,omitempty"`
	Notes            []string     `json:"notes,omitempty"`
}
