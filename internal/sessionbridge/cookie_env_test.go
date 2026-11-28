package sessionbridge

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/pressauth"
	"github.com/stretchr/testify/assert"
)

func TestIsMetaOrHelpArgs(t *testing.T) {
	assert.True(t, IsMetaOrHelpArgs(nil))
	assert.True(t, IsMetaOrHelpArgs([]string{"--help"}))
	assert.True(t, IsMetaOrHelpArgs([]string{"orders", "-h"}))
	assert.True(t, IsMetaOrHelpArgs([]string{"auth", "status"}))
	assert.True(t, IsMetaOrHelpArgs([]string{"doctor"}))
	assert.True(t, IsMetaOrHelpArgs([]string{"version"}))
	assert.False(t, IsMetaOrHelpArgs([]string{"orders", "list", "--json"}))
	assert.False(t, IsMetaOrHelpArgs([]string{"top"}))
}

func TestResolveCookieEnvVarsSkipsMetaAndCapsLiveTimeout(t *testing.T) {
	t.Setenv("TEST_APP_COOKIES", "")
	t.Setenv("PP_NO_ARDIA_COOKIES", "")

	liveCalled := 0
	liveFn := func(ctx context.Context, _ string) (string, map[string]string, error) {
		liveCalled++
		<-ctx.Done()
		return "", nil, ctx.Err()
	}

	// 1. --help must never trigger live browser cookie lookup.
	got := ResolveCookieEnvVars(context.Background(), CookieEnvOptions{
		Homepage:    "https://www.instacart.com",
		AuthType:    "cookie",
		EnvVarNames: []string{"TEST_APP_COOKIES"},
		ChildArgs:   []string{"--help"},
		Timeout:     25 * time.Millisecond,
		LoadState:   func(string) (*pressauth.State, error) { return nil, errors.New("miss") },
		SaveState:   func(*pressauth.State) error { return nil },
		ResolveLive: liveFn,
	})
	assert.Empty(t, got)
	assert.Equal(t, 0, liveCalled)

	// 2. Non-cookie auth type must never trigger live lookup.
	got = ResolveCookieEnvVars(context.Background(), CookieEnvOptions{
		Homepage:    "https://www.instacart.com",
		AuthType:    "",
		EnvVarNames: []string{"TEST_APP_COOKIES"},
		ChildArgs:   []string{"search", "milk"},
		Timeout:     25 * time.Millisecond,
		LoadState:   func(string) (*pressauth.State, error) { return nil, errors.New("miss") },
		SaveState:   func(*pressauth.State) error { return nil },
		ResolveLive: liveFn,
	})
	assert.Empty(t, got)
	assert.Equal(t, 0, liveCalled)

	// 3. Cookie-auth command respects short timeout and falls back to stale cache if present.
	start := time.Now()
	got = ResolveCookieEnvVars(context.Background(), CookieEnvOptions{
		Homepage:    "https://www.instacart.com",
		AuthType:    "cookie",
		EnvVarNames: []string{"TEST_APP_COOKIES"},
		ChildArgs:   []string{"search", "milk"},
		Timeout:     40 * time.Millisecond,
		LoadState: func(string) (*pressauth.State, error) {
			return &pressauth.State{
				Domain:     "instacart.com",
				CapturedAt: time.Now().UTC().Add(-48 * time.Hour),
				Cookies:    map[string]string{"sid": "stale123"},
			}, nil
		},
		SaveState:   func(*pressauth.State) error { return nil },
		ResolveLive: liveFn,
	})
	assert.Less(t, time.Since(start), time.Second)
	assert.Equal(t, 1, liveCalled)
	assert.Equal(t, []string{"TEST_APP_COOKIES=sid=stale123"}, got)
}
