// Package reachability runs no-browser probes against a URL to classify
// whether a printed CLI can reach it with plain HTTP, with browser-compatible
// HTTP (Surf/uTLS Chrome fingerprint), or whether it needs a real browser
// session to capture a clearance cookie.
//
// The classification vocabulary mirrors internal/browsersniff/analysis.go's
// Reachability.Mode so both flows speak one language: a Surf-pass during
// pre-capture probing emits the same `browser_http` mode the post-capture
// analyzer would have produced.
package reachability

// Mode classifies how a URL is reachable.
type Mode string

const (
	// ModeStandardHTTP — plain stdlib HTTP works. Generated CLI ships with
	// Go's default net/http client.
	ModeStandardHTTP Mode = "standard_http"

	// ModeBrowserHTTP — bot-protection signals exist but a Chrome-shaped
	// HTTP client (Surf/uTLS) clears them without cookies. Generated CLI
	// ships with Surf transport. No Chrome attach during generation.
	ModeBrowserHTTP Mode = "browser_http"

	// ModeBrowserClearanceHTTP — neither stdlib nor Surf cleared the
	// protection. Recommendation, not a verdict: the skill should escalate
	// to a real browser-sniff capture, which will produce the authoritative
	// classification. The probe cannot tell whether the site needs a
	// clearance cookie or live page-context execution.
	ModeBrowserClearanceHTTP Mode = "browser_clearance_http"

	// ModeUnknown — probes failed for non-protection reasons (DNS, timeout,
	// connection refused, 5xx). Not enough evidence to recommend a runtime.
	ModeUnknown Mode = "unknown"
)

// Transport names a probe rung.
type Transport string

const (
	TransportStdlib     Transport = "stdlib"
	TransportSurfChrome Transport = "surf-chrome"
)

// ProbeOnly restricts the probe ladder to a single rung. Debug only — the
// skill never sets this. With ProbeOnly set, Result.Partial is true and
// the mode reflects only what the run rung proved.
type ProbeOnly string

const (
	ProbeOnlyNone   ProbeOnly = ""
	ProbeOnlyStdlib ProbeOnly = "stdlib"
	ProbeOnlySurf   ProbeOnly = "surf"
)

// Result is the probe-reachability output. Stable JSON field names so the
// printing-press skill (and any future scripts) can consume this without
// depending on Go struct ordering.
type Result struct {
	URL               string         `json:"url"`
	Mode              Mode           `json:"mode"`
	Confidence        float64        `json:"confidence"`
	BodyEvidence      []string       `json:"body_evidence,omitempty"`
	Probes            []ProbeResult  `json:"probes"`
	Recommendation    Recommendation `json:"recommendation"`
	ImpersonationSafe *bool          `json:"impersonation_safe,omitempty"`
	// Partial is true when --probe-only restricted the ladder to one rung.
	// Skill consumers should ignore Mode when Partial is true.
	Partial bool `json:"partial,omitempty"`
}

// ProbeResult is one rung of the probe ladder.
type ProbeResult struct {
	Transport    Transport `json:"transport"`
	Status       int       `json:"status,omitempty"`
	ElapsedMS    int64     `json:"elapsed_ms"`
	Evidence     []string  `json:"evidence,omitempty"`
	BodyEvidence []string  `json:"body_evidence,omitempty"`
	ContentType  string    `json:"content_type,omitempty"`
	Error        string    `json:"error,omitempty"`
}

// Recommendation tells the caller what runtime to ship and what consent
// gates remain.
type Recommendation struct {
	Runtime              Mode   `json:"runtime"`
	NeedsBrowserCapture  bool   `json:"needs_browser_capture"`
	NeedsClearanceCookie bool   `json:"needs_clearance_cookie"`
	Rationale            string `json:"rationale"`
}
