package sessionbridge

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveOnePasswordViaMockOP(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell mock")
	}
	tmp := t.TempDir()
	mockOP := filepath.Join(tmp, "op")
	script := "#!/bin/sh\nif [ \"$1\" = \"read\" ] && [ \"$2\" = \"--no-newline\" ] && [ \"$3\" = \"op://Personal/Stripe/api_key\" ]; then\n  printf 'sk_live_touchid_123'\n  exit 0\nfi\nexit 1\n"
	require.NoError(t, os.WriteFile(mockOP, []byte(script), 0o755))
	t.Setenv("PP_OP_BIN", mockOP)
	t.Setenv("STRIPE_KEY", "op://Personal/Stripe/api_key")

	res, err := Resolve(AuthSpec{
		AppName:  "stripe",
		Strategy: "env_bearer",
		EnvVar:   "STRIPE_KEY",
	})
	require.NoError(t, err)
	assert.Equal(t, "Bearer sk_live_touchid_123", res.Headers["Authorization"])
	assert.Equal(t, "1password:op://Personal/Stripe/api_key", res.Source)
	assert.NotContains(t, res.Source, "sk_live_touchid_123")

	// Cookie auth with op:// must also keep Source as the op:// ref, never the secret.
	cookieRes, err := Resolve(AuthSpec{
		AppName:  "stripe",
		Strategy: "browser_cookie",
		EnvVar:   "STRIPE_KEY",
	})
	require.NoError(t, err)
	assert.Equal(t, "sk_live_touchid_123", cookieRes.Headers["Cookie"])
	assert.Equal(t, "1password:op://Personal/Stripe/api_key", cookieRes.Source)
	assert.NotContains(t, cookieRes.Source, "sk_live_touchid_123")
}

func TestLoadFromArdiaEnforcesRFC6265DomainPathSchemeAndExpiry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell mock")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("PP_INSTACART_TOKEN", "")
	t.Setenv("PP_AUTH_TOKEN", "")
	t.Setenv("AGENTCOOKIE_SIDECAR_DB", filepath.Join(tmp, "missing.db"))

	mockArdia := filepath.Join(tmp, "ardia")
	payload := `{"success":true,"data":{"result":{"cookies":[` +
		`{"name":"_instacart_session","value":"good_sess","domain":".instacart.com","path":"/","expires":4102444800,"secure":true},` +
		`{"name":"admin_tok","value":"no_path","domain":".instacart.com","path":"/admin","expires":4102444800,"secure":true},` +
		`{"name":"expired_tok","value":"old","domain":".instacart.com","path":"/","expires":1000,"secure":false},` +
		`{"name":"google_sid","value":"leak_me_not","domain":".google.com","path":"/","expires":4102444800,"secure":true}` +
		`]}}}`
	script := "#!/bin/sh\nprintf '%s\\n' '" + payload + "'\n"
	require.NoError(t, os.WriteFile(mockArdia, []byte(script), 0o755))
	t.Setenv("PP_ARDIA_BIN", mockArdia)
	t.Setenv("PP_NO_ARDIA_COOKIES", "")

	res, err := Resolve(AuthSpec{
		AppName:   "instacart",
		Strategy:  "browser_cookie",
		Domains:   []string{".instacart.com", ".google.com"},
		TargetURL: "https://www.instacart.com/store/costco",
	})
	require.NoError(t, err)
	assert.Equal(t, "_instacart_session=good_sess", res.Headers["Cookie"])
	assert.Equal(t, "ardia-cdp", res.Source)

	// Secure cookie must NOT be sent over plain HTTP.
	_, _, _, err = LoadFromArdia(context.Background(), []string{".instacart.com"}, &requestTarget{
		host:    "www.instacart.com",
		path:    "/store",
		isHTTPS: false,
	})
	require.Error(t, err)
}
