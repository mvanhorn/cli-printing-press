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
		auth := r.Header.Get("Authorization")
		mu.Lock()
		calls[auth]++
		mu.Unlock()

		switch auth {
		case "Bearer " + expiredJWT:
			w.WriteHeader(http.StatusNoContent)
		case "Bearer " + validJWT, "opaque-session":
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

func TestValidateComposedAuthRejectsExpiredJWTBeforeProbe(t *testing.T) {
	err := validateComposedAuth("Bearer " + expiredJWT)
	if err == nil {
		t.Fatal("validateComposedAuth() error = nil, want expired JWT error")
	}
	if !strings.Contains(err.Error(), "JWT expired") {
		t.Fatalf("validateComposedAuth() error = %%q, want JWT expired", err)
	}
}

func TestValidateComposedAuthAcceptsValidJWTWithoutProbe(t *testing.T) {
	if err := validateComposedAuth("Bearer " + validJWT); err != nil {
		t.Fatalf("validateComposedAuth() error = %%v, want nil", err)
	}
}

func TestValidateComposedAuthFallsBackForOpaqueCredential(t *testing.T) {
	err := validateComposedAuth("opaque-session")
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
	require.Zero(t, calls["Bearer "+expiredJWT], "expired JWT must fail locally even when the probe would accept it")
	require.Zero(t, calls["Bearer "+validJWT], "valid JWT with exp must not be rejected by a hostile probe")
	require.Positive(t, calls["opaque-session"], "opaque composed credentials must still use the existing probe")
}

func composedAuthTestJWT(t *testing.T, expiry time.Time) string {
	t.Helper()

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"sub":"test","exp":%d}`, expiry.Unix())))
	signature := base64.RawURLEncoding.EncodeToString([]byte("signature"))
	return header + "." + payload + "." + signature
}
