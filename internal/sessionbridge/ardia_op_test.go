package sessionbridge

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pressauth"
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

func isolateSessionTestHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(tmp, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(tmp, ".cache"))
	return tmp
}

func TestLoadFromArdiaEnforcesRFC6265DomainPathSchemeAndExpiry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell mock")
	}
	tmp := isolateSessionTestHome(t)
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

func TestResolveFreshPressAuthSkipsArdiaCDP(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell mock")
	}
	tmp := isolateSessionTestHome(t)
	t.Setenv("AGENTCOOKIE_SIDECAR_DB", filepath.Join(tmp, "missing.db"))
	t.Setenv("PP_INSTACART_TOKEN", "")
	t.Setenv("PP_AUTH_TOKEN", "")
	t.Setenv("PP_NO_ARDIA_COOKIES", "")

	sentinel := filepath.Join(tmp, "ardia-was-called")
	mockArdia := filepath.Join(tmp, "ardia")
	script := "#!/bin/sh\ntouch \"" + sentinel + "\"\nprintf '{\"success\":true,\"data\":{\"cookies\":[{\"name\":\"_instacart_session\",\"value\":\"from_ardia\",\"domain\":\".instacart.com\",\"path\":\"/\",\"expires\":4102444800,\"secure\":true}]}}\\n'\n"
	require.NoError(t, os.WriteFile(mockArdia, []byte(script), 0o755))
	t.Setenv("PP_ARDIA_BIN", mockArdia)

	var currentState *pressauth.State
	origLoad := loadPressAuthState
	loadPressAuthState = func(domain string) (*pressauth.State, error) {
		if currentState != nil && currentState.Domain == domain {
			return currentState, nil
		}
		return nil, pressauth.ErrStateNotFound
	}
	t.Cleanup(func() { loadPressAuthState = origLoad })

	// 1. Fresh (<24h and unexpired JWT) press-auth snapshot skips Ardia CDP.
	currentState = &pressauth.State{
		Domain:     "instacart.com",
		CapturedAt: time.Now().UTC(),
		JWTExpiry:  time.Now().UTC().Add(2 * time.Hour),
		Cookies:    map[string]string{"_instacart_session": "cached_fast"},
	}

	res, err := Resolve(AuthSpec{
		AppName:   "instacart",
		Strategy:  "browser_cookie",
		Domains:   []string{".instacart.com"},
		TargetURL: "https://www.instacart.com/store",
	})
	require.NoError(t, err)
	assert.Equal(t, "_instacart_session=cached_fast", res.Headers["Cookie"])
	assert.Equal(t, "press-auth:instacart.com", res.Source)
	_, statErr := os.Stat(sentinel)
	assert.True(t, os.IsNotExist(statErr), "fresh press-auth cache must skip Ardia CDP call")

	// 2. SkipPressAuthCache forces a live Ardia lookup even when cache is fresh.
	resBypass, err := Resolve(AuthSpec{
		AppName:            "instacart",
		Strategy:           "browser_cookie",
		Domains:            []string{".instacart.com"},
		TargetURL:          "https://www.instacart.com/store",
		SkipPressAuthCache: true,
	})
	require.NoError(t, err)
	assert.Equal(t, "_instacart_session=from_ardia", resBypass.Headers["Cookie"])
	assert.Equal(t, "ardia-cdp", resBypass.Source)
	assert.FileExists(t, sentinel)

	// 3. Recently captured (<24h) snapshot whose JWTExpiry is in the past must
	// NOT be reused; it must refresh via Ardia CDP.
	require.NoError(t, os.Remove(sentinel))
	currentState = &pressauth.State{
		Domain:     "instacart.com",
		CapturedAt: time.Now().UTC().Add(-10 * time.Minute),
		JWTExpiry:  time.Now().UTC().Add(-2 * time.Minute),
		Cookies:    map[string]string{"_instacart_session": "expired_jwt"},
	}
	resExpired, err := Resolve(AuthSpec{
		AppName:   "instacart",
		Strategy:  "browser_cookie",
		Domains:   []string{".instacart.com"},
		TargetURL: "https://www.instacart.com/store",
	})
	require.NoError(t, err)
	assert.Equal(t, "_instacart_session=from_ardia", resExpired.Headers["Cookie"])
	assert.Equal(t, "ardia-cdp", resExpired.Source)
	assert.FileExists(t, sentinel)

	// 4. Stale (>=24h) press-auth snapshot must also fall through to Ardia CDP.
	require.NoError(t, os.Remove(sentinel))
	currentState = &pressauth.State{
		Domain:     "instacart.com",
		CapturedAt: time.Now().UTC().Add(-48 * time.Hour),
		Cookies:    map[string]string{"_instacart_session": "stale_old"},
	}
	res2, err := Resolve(AuthSpec{
		AppName:   "instacart",
		Strategy:  "browser_cookie",
		Domains:   []string{".instacart.com"},
		TargetURL: "https://www.instacart.com/store",
	})
	require.NoError(t, err)
	assert.Equal(t, "_instacart_session=from_ardia", res2.Headers["Cookie"])
	assert.Equal(t, "ardia-cdp", res2.Source)
	assert.FileExists(t, sentinel)
}
