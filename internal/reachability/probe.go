package reachability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/enetx/surf"
	"github.com/mvanhorn/cli-printing-press/v3/internal/version"
)

const (
	defaultTimeout = 15 * time.Second
	bodyReadCap    = 4 * 1024 // bytes scanned for protection markers
)

// Options configures Probe.
type Options struct {
	// Timeout per individual probe rung. Default 15s.
	Timeout time.Duration

	// ProbeOnly restricts the ladder to a single rung. Debug only — leave
	// empty for normal use. When set, Result.Partial is true.
	ProbeOnly ProbeOnly

	// UserAgent overrides the stdlib probe's User-Agent. Default is
	// "printing-press/<version> (probe-reachability)". Surf probes always
	// use the Chrome UA from the impersonation profile and ignore this.
	UserAgent string

	// HTTPClientFactory is a test seam. When non-nil, both probe rungs
	// resolve their *http.Client through this function instead of building
	// a real stdlib or Surf client. Production callers leave it nil.
	HTTPClientFactory func(Transport, time.Duration) (*http.Client, error)
}

// Probe runs the reachability ladder against url and returns a Result.
// Returns a non-nil error only on input-validation failure (empty URL,
// invalid ProbeOnly value); transport errors are reported per-rung in
// Result.Probes and folded into Mode classification.
func Probe(ctx context.Context, url string, opts Options) (*Result, error) {
	if strings.TrimSpace(url) == "" {
		return nil, errors.New("url is required")
	}
	switch opts.ProbeOnly {
	case ProbeOnlyNone, ProbeOnlyStdlib, ProbeOnlySurf:
	default:
		return nil, fmt.Errorf("invalid probe-only value: %q", opts.ProbeOnly)
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	if opts.UserAgent == "" {
		opts.UserAgent = "printing-press/" + version.Get() + " (probe-reachability)"
	}

	result := &Result{URL: url, Probes: []ProbeResult{}}
	partial := opts.ProbeOnly != ProbeOnlyNone

	runStdlib := opts.ProbeOnly == ProbeOnlyNone || opts.ProbeOnly == ProbeOnlyStdlib
	runSurf := opts.ProbeOnly == ProbeOnlyNone || opts.ProbeOnly == ProbeOnlySurf

	if runStdlib {
		pr := runRung(ctx, url, TransportStdlib, opts)
		result.Probes = append(result.Probes, pr)
		if pr.Status > 0 && len(pr.Evidence) == 0 && statusIsClear(pr.Status) {
			result.Mode = ModeStandardHTTP
			result.Confidence = 0.95
			result.Recommendation = Recommendation{
				Runtime:   ModeStandardHTTP,
				Rationale: "Plain stdlib HTTP returned a non-error response; no special transport needed.",
			}
			result.Partial = partial
			return result, nil
		}
	}

	if runSurf {
		pr := runRung(ctx, url, TransportSurfChrome, opts)
		result.Probes = append(result.Probes, pr)
		if pr.Status > 0 && len(pr.Evidence) == 0 && statusIsClear(pr.Status) {
			result.Mode = ModeBrowserHTTP
			result.Confidence = 0.85
			result.Recommendation = Recommendation{
				Runtime:   ModeBrowserHTTP,
				Rationale: "Surf with Chrome TLS fingerprint cleared the protection that blocked plain stdlib HTTP.",
			}
			result.Partial = partial
			return result, nil
		}
	}

	classifyFailure(result, partial)
	return result, nil
}

// statusIsClear is the status-only half of isClear. Used after evidence
// has already been ruled empty by the caller.
func statusIsClear(status int) bool {
	return isClear(status, nil)
}

// classifyFailure mutates result to set Mode/Confidence/Recommendation when
// no rung returned a clear pass.
func classifyFailure(result *Result, partial bool) {
	result.Partial = partial

	hasProtection := false
	allTransportErrors := true
	for _, pr := range result.Probes {
		if len(pr.Evidence) > 0 {
			hasProtection = true
		}
		if pr.Status > 0 {
			allTransportErrors = false
		}
	}

	switch {
	case allTransportErrors:
		result.Mode = ModeUnknown
		result.Confidence = 0
		result.Recommendation = Recommendation{
			Runtime:   ModeUnknown,
			Rationale: "All probes failed at the transport layer (DNS, timeout, connection). Cannot recommend a runtime without reaching the URL.",
		}
	case hasProtection:
		// Stdlib + Surf both saw bot-protection signals. The probe cannot
		// distinguish "needs clearance cookie" from "needs page-context
		// execution" — both produce identical bare-HTTP signatures.
		// Recommend escalation to a real browser-sniff capture, which
		// will produce the authoritative classification.
		result.Mode = ModeBrowserClearanceHTTP
		result.Confidence = 0.6
		result.Recommendation = Recommendation{
			Runtime:              ModeBrowserClearanceHTTP,
			NeedsBrowserCapture:  true,
			NeedsClearanceCookie: true,
			Rationale:            "Stdlib and Surf both received bot-protection signals. A browser-sniff capture is needed to determine whether a clearance cookie alone suffices or whether live page-context execution is required.",
		}
	default:
		// 5xx on both rungs, or other non-clear, non-protected responses.
		result.Mode = ModeUnknown
		result.Confidence = 0.3
		result.Recommendation = Recommendation{
			Runtime:   ModeUnknown,
			Rationale: "Probes returned non-clear, non-protected responses (e.g., 5xx). Site may be down or behind an unrecognized gate.",
		}
	}
}

// runRung executes one probe rung and returns its ProbeResult.
func runRung(ctx context.Context, url string, transport Transport, opts Options) ProbeResult {
	pr := ProbeResult{Transport: transport}
	start := time.Now()
	defer func() {
		pr.ElapsedMS = time.Since(start).Milliseconds()
	}()

	client, err := buildClient(transport, opts)
	if err != nil {
		pr.Error = fmt.Sprintf("building %s client: %v", transport, err)
		return pr
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		pr.Error = fmt.Sprintf("building request: %v", err)
		return pr
	}
	if transport == TransportStdlib {
		req.Header.Set("User-Agent", opts.UserAgent)
		req.Header.Set("Accept", "text/html,application/json;q=0.9,*/*;q=0.8")
	}

	resp, err := client.Do(req)
	if err != nil {
		pr.Error = fmt.Sprintf("request: %v", err)
		pr.ElapsedMS = time.Since(start).Milliseconds()
		return pr
	}
	defer func() { _ = resp.Body.Close() }()

	pr.Status = resp.StatusCode
	pr.ContentType = resp.Header.Get("Content-Type")

	body, _ := io.ReadAll(io.LimitReader(resp.Body, bodyReadCap))
	protections := classifyResponse(resp.StatusCode, resp.Header, string(body))
	pr.Evidence = protectionsToEvidence(protections)
	pr.ElapsedMS = time.Since(start).Milliseconds()
	return pr
}

// buildClient constructs the *http.Client for one probe rung. Tests
// override via opts.HTTPClientFactory; production callers get the real
// stdlib client or Surf with Chrome impersonation.
func buildClient(transport Transport, opts Options) (*http.Client, error) {
	if opts.HTTPClientFactory != nil {
		return opts.HTTPClientFactory(transport, opts.Timeout)
	}
	switch transport {
	case TransportStdlib:
		return &http.Client{Timeout: opts.Timeout}, nil
	case TransportSurfChrome:
		client, err := surf.NewClient().Builder().Impersonate().Chrome().Timeout(opts.Timeout).Build().Result()
		if err != nil {
			return nil, fmt.Errorf("build surf client: %w", err)
		}
		std := client.Std()
		std.Timeout = opts.Timeout
		return std, nil
	default:
		return nil, fmt.Errorf("unknown transport: %q", transport)
	}
}
