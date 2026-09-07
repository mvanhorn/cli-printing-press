package generator

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestBearerAuthStatusAndDoctorReportJWTExpiry(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("bearer-jwt-expiry")
	apiSpec.Auth = spec.AuthConfig{
		Type:    "bearer_token",
		Header:  "Authorization",
		Format:  "Bearer {token}",
		EnvVars: []string{"BEARER_JWT_EXPIRY_TOKEN"},
	}

	outputDir := filepath.Join(t.TempDir(), "bearer-jwt-expiry-pp-cli")
	require.NoError(t, New(apiSpec, outputDir).Generate())
	requireGeneratedCompiles(t, outputDir)

	authSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "auth.go"))
	require.NoError(t, err)
	auth := string(authSrc)
	require.Contains(t, auth, `out["token_expires"] = expiresAt`)
	require.Contains(t, auth, `out["token_expired"] = expired`)
	require.Contains(t, auth, "Token expires:")

	doctorSrc, err := os.ReadFile(filepath.Join(outputDir, "internal", "cli", "doctor.go"))
	require.NoError(t, err)
	doctor := string(doctorSrc)
	require.Contains(t, doctor, `report["auth"] = "ERROR token expired at " + expiresAt`)
	require.Contains(t, doctor, `strings.HasPrefix(s, "ERROR")`)

	expiredJWT := testJWT(t, time.Now().Add(-2*time.Hour))
	validJWT := testJWT(t, time.Now().Add(3*time.Hour))
	testSrc := fmt.Sprintf(`package cli

import (
	"strings"
	"testing"
	"time"
)

const expiredJWT = %q
const validJWT = %q

func TestJWTExpiryOpaqueToken(t *testing.T) {
	if _, ok := jwtExpiry("opaque-session-token"); ok {
		t.Fatal("opaque token must not report expiry")
	}
	if _, ok := jwtExpiry("Bearer opaque-session-token"); ok {
		t.Fatal("opaque bearer header must not report expiry")
	}
}

func TestJWTExpiryExpiredToken(t *testing.T) {
	expiry, ok := jwtExpiry("Bearer " + expiredJWT)
	if !ok {
		t.Fatal("expired JWT must decode exp")
	}
	if time.Now().UTC().Before(expiry) {
		t.Fatalf("expiry %%s should be in the past", expiry)
	}
	_, _, expired, ok := jwtCredentialExpiry("Bearer " + expiredJWT)
	if !ok || !expired {
		t.Fatalf("jwtCredentialExpiry expired=%%v ok=%%v, want expired", expired, ok)
	}
}

func TestJWTExpiryValidToken(t *testing.T) {
	expiry, ok := jwtExpiry(validJWT)
	if !ok {
		t.Fatal("valid JWT must decode exp")
	}
	if !time.Now().UTC().Before(expiry) {
		t.Fatalf("expiry %%s should be in the future", expiry)
	}
	expiresAt, line, expired, ok := jwtCredentialExpiry(validJWT)
	if !ok || expired {
		t.Fatalf("jwtCredentialExpiry expired=%%v ok=%%v, want not expired", expired, ok)
	}
	if expiresAt == "" || !strings.Contains(line, "in ") {
		t.Fatalf("line = %%q expiresAt = %%q", line, expiresAt)
	}
}

func TestJWTExpiryMissingExp(t *testing.T) {
	header := %q
	payload := %q
	sig := %q
	token := header + "." + payload + "." + sig
	if _, ok := jwtExpiry(token); ok {
		t.Fatal("JWT without exp must not report expiry")
	}
}
`, expiredJWT, validJWT,
		base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`)),
		base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"test"}`)),
		base64.RawURLEncoding.EncodeToString([]byte("sig")),
	)
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "jwt_expiry_runtime_test.go"), []byte(testSrc), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/cli", "-run", "TestJWTExpiry", "-count=1")
}

func testJWT(t *testing.T, expiry time.Time) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, `{"sub":"test","exp":%d}`, expiry.Unix()))
	signature := base64.RawURLEncoding.EncodeToString([]byte("signature"))
	return header + "." + payload + "." + signature
}
