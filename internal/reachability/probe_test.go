package reachability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeFactory returns a per-transport http.Client whose RoundTripper
// dispatches to a fake response builder. Lets tests simulate
// "stdlib gets challenged, surf clears" without a real network.
func fakeFactory(stdlib, surf func(*http.Request) (*http.Response, error)) func(Transport, time.Duration) (*http.Client, error) {
	return func(t Transport, _ time.Duration) (*http.Client, error) {
		switch t {
		case TransportStdlib:
			return &http.Client{Transport: roundTripFn(stdlib)}, nil
		case TransportSurfChrome:
			return &http.Client{Transport: roundTripFn(surf)}, nil
		}
		return nil, nil
	}
}

type roundTripFn func(*http.Request) (*http.Response, error)

func (f roundTripFn) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func vercelChallenge(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 429,
		Header: http.Header{
			"X-Vercel-Mitigated": {"challenge"},
			"Content-Type":       {"text/html"},
			"Server":             {"Vercel"},
		},
		Body: respBody("<html>Vercel Security Checkpoint</html>"),
	}, nil
}

func ok200(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"text/html"}},
		Body:       respBody("<html>recipe content</html>"),
	}, nil
}

func json200(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"application/json; charset=utf-8"}},
		Body:       respBody(`{"ok":true}`),
	}, nil
}

func jsonCaptchaFields200(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"application/json; charset=utf-8"}},
		Body:       respBody(`{"hcaptcha_required":false,"turnstile_enabled":false,"captcha_provider":"hcaptcha"}`),
	}, nil
}

func xml200(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"application/xml; charset=utf-8"}},
		Body:       respBody(`<StoreModel><ok>true</ok></StoreModel>`),
	}, nil
}

func impervaHcaptchaShell(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"text/html; charset=utf-8"}},
		Body: respBody(`<html><head><script src="/_Incapsula_Resource?SWKMTFSR=1"></script></head>
			<body><iframe src="https://newassets.hcaptcha.com/captcha/v1/index.html"></iframe></body></html>`),
	}, nil
}

func cloudflareChallenge(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 403,
		Header: http.Header{
			"Cf-Mitigated": {"challenge"},
			"Cf-Ray":       {"abc123"},
			"Server":       {"cloudflare"},
			"Content-Type": {"text/html"},
		},
		Body: respBody("<html>Just a moment...</html>"),
	}, nil
}

func turnstileInterstitial(_ *http.Request) (*http.Response, error) {
	body := strings.Repeat(" ", 20*1024) + `<html><script src="https://challenges.cloudflare.com/turnstile/v0/api.js"></script><p>You will be forwarded to the requested page shortly.</p></html>`
	return &http.Response{
		StatusCode: 200,
		Header: http.Header{
			"Cf-Ray":       {"abc123"},
			"Server":       {"cloudflare"},
			"Content-Type": {"text/html"},
		},
		Body: respBody(body),
	}, nil
}

func respBody(s string) *bodyCloser {
	return &bodyCloser{r: strings.NewReader(s)}
}

type bodyCloser struct {
	r *strings.Reader
}

func (b *bodyCloser) Read(p []byte) (int, error) { return b.r.Read(p) }
func (b *bodyCloser) Close() error               { return nil }

func TestProbe_StdlibPasses(t *testing.T) {
	result, err := Probe(context.Background(), "https://example.com", Options{
		HTTPClientFactory: fakeFactory(ok200, vercelChallenge),
	})
	require.NoError(t, err)
	assert.Equal(t, ModeStandardHTTP, result.Mode)
	assert.Len(t, result.Probes, 2, "full probes compare surf against stdlib for content negotiation drift")
	assert.Equal(t, TransportStdlib, result.Probes[0].Transport)
	assert.False(t, result.Partial)
	assert.False(t, result.Recommendation.NeedsBrowserCapture)
}

func TestProbe_CleanHTML_RemainsStandardHTTP(t *testing.T) {
	result, err := Probe(context.Background(), "https://example.com", Options{
		HTTPClientFactory: fakeFactory(ok200, ok200),
	})
	require.NoError(t, err)
	assert.Equal(t, ModeStandardHTTP, result.Mode)
	assert.Empty(t, result.BodyEvidence)
	assert.Empty(t, result.Probes[0].BodyEvidence)
	assert.False(t, result.Recommendation.NeedsBrowserCapture)
}

func TestProbe_JSONAPIWithCaptchaFields_RemainsStandardHTTP(t *testing.T) {
	result, err := Probe(context.Background(), "https://api.example.com/session/check", Options{
		HTTPClientFactory: fakeFactory(jsonCaptchaFields200, jsonCaptchaFields200),
	})
	require.NoError(t, err)
	assert.Equal(t, ModeStandardHTTP, result.Mode)
	assert.Empty(t, result.BodyEvidence)
	assert.Empty(t, result.Probes[0].Evidence)
	assert.False(t, result.Recommendation.NeedsBrowserCapture)
}

func TestProbe_StdlibJSONSurfXMLMarksImpersonationUnsafe(t *testing.T) {
	result, err := Probe(context.Background(), "https://example.com/api/stores/123", Options{
		HTTPClientFactory: fakeFactory(json200, xml200),
	})
	require.NoError(t, err)
	assert.Equal(t, ModeStandardHTTP, result.Mode)
	require.NotNil(t, result.ImpersonationSafe)
	assert.False(t, *result.ImpersonationSafe)
	assert.Contains(t, result.Recommendation.Rationale, "Surf Chrome impersonation")
	assert.Equal(t, "application/json; charset=utf-8", result.Probes[0].ContentType)
	assert.Equal(t, "application/xml; charset=utf-8", result.Probes[1].ContentType)
}

func TestProbe_StdlibChallengedSurfClears_FoodS52Pattern(t *testing.T) {
	// The exact case from the bug report: Vercel passive challenge that
	// stdlib trips on but Surf with Chrome TLS clears. No clearance cookie
	// needed.
	result, err := Probe(context.Background(), "https://food52.com", Options{
		HTTPClientFactory: fakeFactory(vercelChallenge, ok200),
	})
	require.NoError(t, err)
	assert.Equal(t, ModeBrowserHTTP, result.Mode)
	assert.Len(t, result.Probes, 2, "stdlib fails, surf passes — both rungs ran")
	assert.False(t, result.Recommendation.NeedsBrowserCapture, "surf alone is enough — no Chrome attach needed")
	assert.False(t, result.Recommendation.NeedsClearanceCookie)
	assert.NotEmpty(t, result.Probes[0].Evidence, "stdlib probe should record vercel evidence")
	assert.Empty(t, result.Probes[1].Evidence, "surf probe should be clean")
}

func TestProbe_BothChallenged_RecommendsBrowserCapture(t *testing.T) {
	result, err := Probe(context.Background(), "https://example.com", Options{
		HTTPClientFactory: fakeFactory(cloudflareChallenge, cloudflareChallenge),
	})
	require.NoError(t, err)
	assert.Equal(t, ModeBrowserClearanceHTTP, result.Mode)
	assert.True(t, result.Recommendation.NeedsBrowserCapture)
	assert.True(t, result.Recommendation.NeedsClearanceCookie)
}

func TestProbe_BothTurnstileInterstitials_RecommendsBrowserCapture(t *testing.T) {
	result, err := Probe(context.Background(), "https://example.com", Options{
		HTTPClientFactory: fakeFactory(turnstileInterstitial, turnstileInterstitial),
	})
	require.NoError(t, err)
	assert.Equal(t, ModeBrowserClearanceHTTP, result.Mode)
	assert.Equal(t, 200, result.Probes[0].Status)
	assert.NotEmpty(t, result.Probes[0].Evidence)
	assert.True(t, result.Recommendation.NeedsBrowserCapture)
	assert.True(t, result.Recommendation.NeedsClearanceCookie)
}

func TestProbe_BothImpervaHcaptchaShells_RecommendsBrowserCaptureWithBodyEvidence(t *testing.T) {
	result, err := Probe(context.Background(), "https://www.example.com", Options{
		HTTPClientFactory: fakeFactory(impervaHcaptchaShell, impervaHcaptchaShell),
	})
	require.NoError(t, err)
	assert.Equal(t, ModeBrowserClearanceHTTP, result.Mode)
	assert.ElementsMatch(t, []string{"imperva", "hcaptcha"}, result.BodyEvidence)
	require.Len(t, result.Probes, 2)
	assert.ElementsMatch(t, []string{"imperva", "hcaptcha"}, result.Probes[0].BodyEvidence)
	assert.ElementsMatch(t, []string{"imperva", "hcaptcha"}, result.Probes[1].BodyEvidence)
	assert.True(t, result.Recommendation.NeedsBrowserCapture)
	assert.True(t, result.Recommendation.NeedsClearanceCookie)
}

func TestProbe_TransportFailure_Unknown(t *testing.T) {
	failTransport := func(_ *http.Request) (*http.Response, error) {
		return nil, &net500Error{}
	}
	result, err := Probe(context.Background(), "https://nope.invalid", Options{
		HTTPClientFactory: fakeFactory(failTransport, failTransport),
	})
	require.NoError(t, err)
	assert.Equal(t, ModeUnknown, result.Mode)
	assert.NotEmpty(t, result.Probes[0].Error)
}

type net500Error struct{}

func (net500Error) Error() string { return "simulated DNS/network failure" }

func TestProbe_ProbeOnlyStdlib_PartialFlag(t *testing.T) {
	result, err := Probe(context.Background(), "https://example.com", Options{
		ProbeOnly:         ProbeOnlyStdlib,
		HTTPClientFactory: fakeFactory(ok200, vercelChallenge),
	})
	require.NoError(t, err)
	assert.Equal(t, ModeStandardHTTP, result.Mode)
	assert.True(t, result.Partial, "--probe-only must mark result as partial")
	assert.Len(t, result.Probes, 1)
	assert.Equal(t, TransportStdlib, result.Probes[0].Transport)
}

func TestProbe_ProbeOnlySurf_SkipsStdlib(t *testing.T) {
	stdlibCalled := false
	stdlib := func(_ *http.Request) (*http.Response, error) {
		stdlibCalled = true
		return ok200(nil)
	}
	result, err := Probe(context.Background(), "https://example.com", Options{
		ProbeOnly:         ProbeOnlySurf,
		HTTPClientFactory: fakeFactory(stdlib, ok200),
	})
	require.NoError(t, err)
	assert.False(t, stdlibCalled, "--probe-only surf must not run stdlib probe")
	assert.Equal(t, ModeBrowserHTTP, result.Mode)
	assert.True(t, result.Partial)
}

func TestProbe_InvalidProbeOnly(t *testing.T) {
	_, err := Probe(context.Background(), "https://example.com", Options{
		ProbeOnly: ProbeOnly("nonsense"),
	})
	require.Error(t, err)
}

func TestProbe_EmptyURL(t *testing.T) {
	_, err := Probe(context.Background(), "", Options{})
	require.Error(t, err)
}

func TestProbe_RealHTTPTestServer(t *testing.T) {
	// End-to-end against a real local HTTP server, exercising the full
	// stdlib path (no factory override).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	result, err := Probe(context.Background(), srv.URL, Options{
		ProbeOnly: ProbeOnlyStdlib, // skip surf for hermetic test (no real TLS cert)
		Timeout:   5 * time.Second,
	})
	require.NoError(t, err)
	assert.Equal(t, ModeStandardHTTP, result.Mode)
	assert.Equal(t, 200, result.Probes[0].Status)
}
