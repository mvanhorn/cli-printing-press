package browsersniff

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
)

func TestNormalizeHTTPVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"HTTP/3", "h3"},
		{"http/3.0", "h3"},
		{"h3", "h3"},
		{"HTTP/2", "h2"},
		{"http/2.0", "h2"},
		{"h2", "h2"},
		{"h2c", "h2"},
		{"HTTP/1.1", "http/1.1"},
		{"http/1.1", "http/1.1"},
		{"", ""},
		{"unknown", ""},
		{"HTTP/0.9", ""},
		{"  HTTP/2  ", "h2"},
	}
	for _, c := range cases {
		got := NormalizeHTTPVersion(c.in)
		if got != c.want {
			t.Errorf("NormalizeHTTPVersion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBrowserTransportFromHARVersion_PicksMajority(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		dist map[string]int
		want string
	}{
		{
			name: "h3 majority",
			dist: map[string]int{"h3": 20, "h2": 3},
			want: spec.HTTPTransportBrowserChromeH3,
		},
		{
			name: "h2 majority",
			dist: map[string]int{"h2": 18, "h3": 2, "http/1.1": 1},
			want: spec.HTTPTransportBrowserChromeH2,
		},
		{
			name: "h1 only",
			dist: map[string]int{"http/1.1": 5},
			want: spec.HTTPTransportBrowserChrome,
		},
		{
			name: "empty falls back to no-force browser-chrome",
			dist: map[string]int{},
			want: spec.HTTPTransportBrowserChrome,
		},
		{
			name: "nil distribution falls back to no-force browser-chrome",
			dist: nil,
			want: spec.HTTPTransportBrowserChrome,
		},
		{
			name: "tie between h3 and h2 prefers h3",
			dist: map[string]int{"h3": 5, "h2": 5},
			want: spec.HTTPTransportBrowserChromeH3,
		},
		{
			name: "unrecognized keys ignored",
			dist: map[string]int{"spdy": 99},
			want: spec.HTTPTransportBrowserChrome,
		},
	}
	for _, c := range cases {
		got := browserTransportFromHARVersion(c.dist)
		if got != c.want {
			t.Errorf("%s: browserTransportFromHARVersion(%v) = %q, want %q", c.name, c.dist, got, c.want)
		}
	}
}

func TestApplyReachabilityDefaults_HARVersionDrivesTransport(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		mode string
		dist map[string]int
		want string
	}{
		{"clearance + h3", "browser_clearance_http", map[string]int{"h3": 9}, spec.HTTPTransportBrowserChromeH3},
		{"clearance + h2", "browser_clearance_http", map[string]int{"h2": 9}, spec.HTTPTransportBrowserChromeH2},
		{"clearance + h1", "browser_clearance_http", map[string]int{"http/1.1": 5}, spec.HTTPTransportBrowserChrome},
		{"browser_http + h2", "browser_http", map[string]int{"h2": 4}, spec.HTTPTransportBrowserChromeH2},
		{"empty distribution + clearance", "browser_clearance_http", nil, spec.HTTPTransportBrowserChrome},
	}
	for _, c := range cases {
		apiSpec := &spec.APISpec{
			Name:    c.name,
			BaseURL: "https://www.example.com",
			Auth:    spec.AuthConfig{Type: "none"},
			Resources: map[string]spec.Resource{
				"posts": {Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/posts"}}},
			},
		}
		analysis := &TrafficAnalysis{
			Summary: TrafficAnalysisSummary{
				TargetURL:               "https://www.example.com",
				HTTPVersionDistribution: c.dist,
			},
			Reachability: &ReachabilityAnalysis{Mode: c.mode, Confidence: 0.9},
		}
		ApplyReachabilityDefaults(apiSpec, analysis)
		if apiSpec.HTTPTransport != c.want {
			t.Errorf("%s: HTTPTransport = %q, want %q", c.name, apiSpec.HTTPTransport, c.want)
		}
	}
}

// TestApplyReachabilityDefaults_BrowserRequiredLeavesEmpty pins the
// invariant that browser_required mode does not set HTTPTransport even
// when a HAR distribution is present. browser_required means the
// operator must drive a real browser; the surf client is not used and
// emitting a transport would mislead downstream code.
func TestApplyReachabilityDefaults_BrowserRequiredLeavesEmpty(t *testing.T) {
	t.Parallel()
	apiSpec := &spec.APISpec{
		Name:    "browserrequired",
		BaseURL: "https://www.example.com",
		Auth:    spec.AuthConfig{Type: "none"},
		Resources: map[string]spec.Resource{
			"posts": {Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/posts"}}},
		},
	}
	analysis := &TrafficAnalysis{
		Summary: TrafficAnalysisSummary{
			TargetURL:               "https://www.example.com",
			HTTPVersionDistribution: map[string]int{"h3": 10},
		},
		Reachability: &ReachabilityAnalysis{Mode: "browser_required", Confidence: 0.9},
	}
	ApplyReachabilityDefaults(apiSpec, analysis)
	if apiSpec.HTTPTransport != "" {
		t.Errorf("browser_required must leave HTTPTransport empty regardless of HAR; got %q", apiSpec.HTTPTransport)
	}
}

// TestApplyReachabilityDefaults_PreservesExplicitTransport keeps the
// caller's explicit choice when HTTPTransport is already set, so an
// operator who hand-tunes the spec is not overridden by the HAR
// majority. Matches the old switch semantics.
func TestApplyReachabilityDefaults_PreservesExplicitTransport(t *testing.T) {
	t.Parallel()
	apiSpec := &spec.APISpec{
		Name:          "preserve",
		BaseURL:       "https://www.example.com",
		Auth:          spec.AuthConfig{Type: "none"},
		HTTPTransport: spec.HTTPTransportBrowserChromeH3,
		Resources: map[string]spec.Resource{
			"posts": {Endpoints: map[string]spec.Endpoint{"list": {Method: "GET", Path: "/posts"}}},
		},
	}
	analysis := &TrafficAnalysis{
		Summary: TrafficAnalysisSummary{
			TargetURL:               "https://www.example.com",
			HTTPVersionDistribution: map[string]int{"h2": 99},
		},
		Reachability: &ReachabilityAnalysis{Mode: "browser_clearance_http", Confidence: 0.9},
	}
	ApplyReachabilityDefaults(apiSpec, analysis)
	if apiSpec.HTTPTransport != spec.HTTPTransportBrowserChromeH3 {
		t.Errorf("explicit HTTPTransport must be preserved; got %q", apiSpec.HTTPTransport)
	}
}
