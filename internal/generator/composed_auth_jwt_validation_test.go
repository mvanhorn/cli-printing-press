package generator

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedComposedAuthValidatesJWTExpLocally(t *testing.T) {
	t.Parallel()

	expiredJWT := composedAuthTestJWT(t, time.Now().Add(-time.Hour))
	validJWT := composedAuthTestJWT(t, time.Now().Add(time.Hour))

	var mu sync.Mutex
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		cookieHeader := r.Header.Get("Cookie")
		callKey := "Authorization:" + authHeader + "|Cookie:" + cookieHeader
		mu.Lock()
		calls[callKey]++
		mu.Unlock()

		if authHeader != "" {
			w.WriteHeader(http.StatusTeapot)
			return
		}
		switch cookieHeader {
		case "session=" + validJWT:
			w.WriteHeader(http.StatusUnauthorized)
		case "session=opaque-session":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	apiSpec := &spec.APISpec{
		Name:    "composed-jwt-validation",
		Version: "0.1.0",
		BaseURL: server.URL,
		Auth: spec.AuthConfig{
			Type:         "composed",
			Header:       "Authorization",
			Format:       "Bearer {session}",
			CookieDomain: "example.com",
			Cookies:      []string{"session"},
		},
		Config: spec.ConfigSpec{
			Format: "toml",
			Path:   "~/.config/composed-jwt-validation-pp-cli/config.toml",
		},
		Resources: map[string]spec.Resource{
			"items": {
				Description: "Manage items",
				Endpoints: map[string]spec.Endpoint{
					"list": {Method: "GET", Path: "/items", Description: "List items"},
				},
			},
		},
	}

	outputDir := filepath.Join(t.TempDir(), "composed-jwt-validation-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	testSrc := fmt.Sprintf(`package cli

import (
	"strings"
	"testing"
)

const expiredJWT = %q
const validJWT = %q

func TestComposeAuthFromCookiesReplacesTokenPlaceholder(t *testing.T) {
	got := composeAuthFromCookies("Bearer {token}", []string{"session"}, map[string]string{"session": "captured-cookie"})
	if got != "Bearer captured-cookie" {
		t.Fatalf("composeAuthFromCookies() = %%q, want %%q", got, "Bearer captured-cookie")
	}
}

func TestValidateComposedAuthRejectsExpiredJWTBeforeProbe(t *testing.T) {
	err := validateComposedAuth("Bearer " + expiredJWT, "session=" + expiredJWT)
	if err == nil {
		t.Fatal("validateComposedAuth() error = nil, want expired JWT error")
	}
	if !strings.Contains(err.Error(), "JWT expired") {
		t.Fatalf("validateComposedAuth() error = %%q, want JWT expired", err)
	}
}

func TestValidateComposedAuthAcceptsValidJWTWithoutProbe(t *testing.T) {
	if err := validateComposedAuth("Bearer " + validJWT, "session=" + validJWT); err != nil {
		t.Fatalf("validateComposedAuth() error = %%v, want nil", err)
	}
}

func TestValidateComposedAuthFallsBackForOpaqueCredential(t *testing.T) {
	err := validateComposedAuth("Bearer opaque-session", "session=opaque-session")
	if err == nil {
		t.Fatal("validateComposedAuth() error = nil, want probe error")
	}
	if !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("validateComposedAuth() error = %%q, want HTTP 401", err)
	}
}
`, expiredJWT, validJWT)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "composed_auth_jwt_validation_test.go"), []byte(testSrc), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestValidateComposedAuth", "-count=1")

	mu.Lock()
	defer mu.Unlock()
	require.Zero(t, calls["Authorization:Bearer "+expiredJWT+"|Cookie:session="+expiredJWT], "expired JWT must fail locally even when the probe would accept it")
	require.Zero(t, calls["Authorization:|Cookie:session="+expiredJWT], "expired JWT must fail locally before any cookie probe")
	require.Zero(t, calls["Authorization:Bearer "+validJWT+"|Cookie:session="+validJWT], "valid JWT with exp must not be rejected by a hostile probe")
	require.Zero(t, calls["Authorization:|Cookie:session="+validJWT], "valid JWT with exp must not be rejected by a hostile cookie probe")
	require.Positive(t, calls["Authorization:|Cookie:session=opaque-session"], "opaque composed credentials must still use the existing cookie probe")
	require.Zero(t, calls["Authorization:Bearer opaque-session|Cookie:session=opaque-session"], "cookie-session validation must not send the bearer Authorization header")
}

func composedAuthTestJWT(t *testing.T, expiry time.Time) string {
	t.Helper()

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, `{"sub":"test","exp":%d}`, expiry.Unix()))
	signature := base64.RawURLEncoding.EncodeToString([]byte("signature"))
	return header + "." + payload + "." + signature
}
