package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"oauth-refresh-lock-pp-cli/internal/cliutil"
	"oauth-refresh-lock-pp-cli/internal/cliutil/testenv"
	"oauth-refresh-lock-pp-cli/internal/config"
)

func TestOAuthRefreshFileLock(t *testing.T) {
	switch os.Getenv("PP_OAUTH_REFRESH_MODE") {
	case "refresh":
		oauthChildRefresh()
	case "hold":
		oauthChildHold()
	case "crash":
		oauthChildCrash()
	default:
		t.Run("two processes spend one rotating refresh token", testTwoProcessesOneSpend)
		t.Run("early window refreshes inside 60s", testEarlyWindowRefresh)
		t.Run("token outside the window is not refreshed", testNotNearExpiry)
		t.Run("zero expiry is not treated as near expiry", testZeroExpiryNoRefresh)
		t.Run("unauthorized still refreshes a current token", testUnauthorizedRefreshesCurrent)
		t.Run("stale lock file does not block refresh", testStaleLockFile)
		t.Run("refresh waits for the lock instead of spending", testLockContention)
		t.Run("distinct profiles spend their own refresh tokens", testDistinctProfiles)
		t.Run("explicit profile shares the global refresh token", testExplicitInheritsGlobal)
		t.Run("peer refresh is adopted instead of spent", testAdoptSuccessor)
		t.Run("profile does not switch to a global login", testProfileDoesNotSwitchToGlobal)
		t.Run("diverged global token is not spent", testDivergedGlobalIsNotSpent)
		t.Run("shared peer rotation is saved to both", testSharedPeerRotationSavedToBoth)
		t.Run("env refresh token does not spend the other login", testEnvDoesNotSpendOtherLogin)
		t.Run("hard-linked credentials take one spend", testHardLinkedCredentialsOneSpend)
		t.Run("symlinked config shares one spend", testSymlinkedConfigOneSpend)
		t.Run("refused refresh body does not echo tokens", testRefreshErrorRedactsTokens)
		t.Run("crash after persist lets the waiter adopt", testCrashAfterPersist)
		t.Run("agentcookie refresh is locked on the config store", testAgentcookieOneSpend)
	}
}

func oauthChildRefresh() {
	cfg, err := config.Load(os.Getenv("PP_OAUTH_CONFIG"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if tokenURL := os.Getenv("PP_OAUTH_TOKEN_URL"); tokenURL != "" {
		cfg.TokenURL = tokenURL
	}
	if ready := os.Getenv("PP_OAUTH_READY"); ready != "" {
		if err := os.WriteFile(ready, []byte("1"), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if start := os.Getenv("PP_OAUTH_START"); start != "" {
		deadline := time.Now().Add(15 * time.Second)
		for {
			if _, err := os.Stat(start); err == nil {
				break
			}
			if time.Now().After(deadline) {
				fmt.Fprintln(os.Stderr, "start file missing")
				os.Exit(1)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	c := New(cfg, 5*time.Second, 0)
	c.NoCache = true
	if err := c.refreshAccessToken(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintln(os.Stdout, cfg.AccessToken)
	os.Exit(0)
}

func oauthChildHold() {
	path := os.Getenv("PP_OAUTH_LOCK_PATH")
	ready := os.Getenv("PP_OAUTH_READY")
	err := cliutil.WithFileLock(path, func() error {
		if err := os.WriteFile(ready, []byte("1"), 0o600); err != nil {
			return err
		}
		if start := os.Getenv("PP_OAUTH_START"); start != "" && os.Getenv("PP_OAUTH_HOLD_REWRITE") == "1" {
			deadline := time.Now().Add(15 * time.Second)
			for {
				if _, err := os.Stat(start); err == nil {
					break
				}
				if time.Now().After(deadline) {
					return fmt.Errorf("start file missing")
				}
				time.Sleep(10 * time.Millisecond)
			}
		} else {
			hold := time.Second
			if os.Getenv("PP_OAUTH_HOLD_REWRITE") == "1" {
				hold = 800 * time.Millisecond
			}
			time.Sleep(hold)
		}
		if os.Getenv("PP_OAUTH_HOLD_REWRITE") == "1" {
			expiry := time.Now().Add(time.Hour)
			if os.Getenv("PP_OAUTH_HOLD_NEAR") == "1" {
				expiry = time.Now().Add(30 * time.Second)
			}
			return cliutil.SaveCredentials(&cliutil.Credentials{
				BaseURL:      "https://api.example.com",
				AccessToken:  "access-successor",
				RefreshToken: "refresh-successor",
				TokenExpiry:  expiry,
				ClientID:     "client-id",
				ClientSecret: "client-secret-value",
			})
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func oauthChildCrash() {
	oauthRefreshPersistedHook = func() { os.Exit(75) }
	oauthChildRefresh()
}

func testTwoProcessesOneSpend(t *testing.T) {
	isolateOAuth(t)
	srv := newRotatingTokenServer(t, "refresh-old", 400*time.Millisecond)
	seedTokens(t, srv.URL+"/token", "access-old", "refresh-old", time.Now().Add(-time.Minute))
	dir := t.TempDir()
	start := filepath.Join(dir, "start")
	readyA := filepath.Join(dir, "a")
	readyB := filepath.Join(dir, "b")
	cmdA := startOAuthChild(t, "refresh", "", readyA, start, srv.URL+"/token")
	cmdB := startOAuthChild(t, "refresh", "", readyB, start, srv.URL+"/token")
	waitForFile(t, readyA)
	waitForFile(t, readyB)
	if err := os.WriteFile(start, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	outA := waitChild(t, cmdA, 15*time.Second, 0)
	outB := waitChild(t, cmdB, 15*time.Second, 0)
	if srv.calls.Load() != 1 || srv.spentOld.Load() != 1 {
		t.Fatalf("token calls=%d spent old=%d, want 1 and 1", srv.calls.Load(), srv.spentOld.Load())
	}
	if !strings.Contains(outA, "access-new") || !strings.Contains(outB, "access-new") {
		t.Fatalf("children did not both adopt the new access token\nA:%s\nB:%s", outA, outB)
	}
	follow := reloadClient(t, "", srv.URL+"/token")
	if err := follow.refreshAccessToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if srv.calls.Load() != 1 {
		t.Fatalf("follow-up refresh calls=%d, want 1", srv.calls.Load())
	}
}

func testEarlyWindowRefresh(t *testing.T) {
	isolateOAuth(t)
	var calls atomic.Int32
	var gotAuth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"access-new","refresh_token":"refresh-new","expires_in":3600}`)
			return
		}
		gotAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)
	cfg := seedTokens(t, srv.URL+"/token", "access-old", "refresh-old", time.Now().Add(30*time.Second))
	cfg.BaseURL = srv.URL
	c := New(cfg, 5*time.Second, 0)
	c.NoCache = true
	c.BaseURL = srv.URL
	c.HTTPClient = srv.Client()
	if _, err := c.Get(context.Background(), "/items", nil); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("token calls=%d, want 1", calls.Load())
	}
	if got := gotAuth.Load(); got != "Bearer access-new" {
		t.Fatalf("authorization=%v, want Bearer access-new", got)
	}
	if _, err := c.Get(context.Background(), "/items", nil); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("token calls after fresh token=%d, want 1", calls.Load())
	}
}

func testNotNearExpiry(t *testing.T) {
	assertNoProactiveRefresh(t, time.Now().Add(2*time.Minute))
}

func testZeroExpiryNoRefresh(t *testing.T) {
	assertNoProactiveRefresh(t, time.Time{})
}

func assertNoProactiveRefresh(t *testing.T, expiry time.Time) {
	t.Helper()
	isolateOAuth(t)
	var calls atomic.Int32
	var gotAuth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			calls.Add(1)
			http.Error(w, "unexpected refresh", http.StatusInternalServerError)
			return
		}
		gotAuth.Store(r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)
	cfg := seedTokens(t, srv.URL+"/token", "access-old", "refresh-old", expiry)
	cfg.BaseURL = srv.URL
	c := New(cfg, 5*time.Second, 0)
	c.NoCache = true
	c.BaseURL = srv.URL
	c.HTTPClient = srv.Client()
	if _, err := c.Get(context.Background(), "/items", nil); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatalf("token calls=%d, want 0", calls.Load())
	}
	if got := gotAuth.Load(); got != "Bearer access-old" {
		t.Fatalf("authorization=%v, want Bearer access-old", got)
	}
}

func testUnauthorizedRefreshesCurrent(t *testing.T) {
	isolateOAuth(t)
	var calls atomic.Int32
	var sawNew atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"access-new","refresh_token":"refresh-new","expires_in":3600}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer access-new" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"unauthorized"}`)
			return
		}
		sawNew.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)
	cfg := seedTokens(t, srv.URL+"/token", "access-old", "refresh-old", time.Now().Add(2*time.Minute))
	cfg.BaseURL = srv.URL
	c := New(cfg, 5*time.Second, 0)
	c.NoCache = true
	c.BaseURL = srv.URL
	c.HTTPClient = srv.Client()
	body, err := c.Get(context.Background(), "/items", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"ok":true`) {
		t.Fatalf("body=%s", body)
	}
	if calls.Load() != 1 || !sawNew.Load() {
		t.Fatalf("token calls=%d saw new=%v", calls.Load(), sawNew.Load())
	}
}

func testStaleLockFile(t *testing.T) {
	isolateOAuth(t)
	srv := newRotatingTokenServer(t, "refresh-old", 0)
	seedTokens(t, srv.URL+"/token", "access-old", "refresh-old", time.Now().Add(-time.Minute))
	creds, err := cliutil.CredentialsFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(creds+".lock", []byte("not a held lock"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := reloadClient(t, "", srv.URL+"/token")
	if err := c.refreshAccessToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	if srv.calls.Load() != 1 {
		t.Fatalf("token calls=%d, want 1", srv.calls.Load())
	}
}

func testLockContention(t *testing.T) {
	isolateOAuth(t)
	srv := newRotatingTokenServer(t, "refresh-old", 0)
	seedTokens(t, srv.URL+"/token", "access-old", "refresh-old", time.Now().Add(-time.Minute))
	creds, err := cliutil.CredentialsFilePath()
	if err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := startOAuthChild(t, "hold", "", ready, "", "", "PP_OAUTH_LOCK_PATH="+creds)
	waitForFile(t, ready)
	c := reloadClient(t, "", srv.URL+"/token")
	done := make(chan error, 1)
	go func() {
		done <- c.refreshAccessToken(context.Background())
	}()
	time.Sleep(200 * time.Millisecond)
	if srv.calls.Load() != 0 {
		t.Fatalf("token calls while lock held=%d, want 0", srv.calls.Load())
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("refresh did not wait for the lock")
	}
	waitChild(t, cmd, 5*time.Second, 0)
	if srv.calls.Load() != 1 {
		t.Fatalf("token calls=%d, want 1", srv.calls.Load())
	}
}

func testDistinctProfiles(t *testing.T) {
	home := isolateOAuth(t)
	var mu sync.Mutex
	seen := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		got := r.Form.Get("refresh_token")
		mu.Lock()
		seen[got]++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"access-%s","refresh_token":"next-%s","expires_in":3600}`, got, got)
	}))
	t.Cleanup(srv.Close)
	pathA := writeProfile(t, home, "profile-a")
	pathB := writeProfile(t, home, "profile-b")
	saveProfileTokens(t, pathA, srv.URL+"/token", "refresh-a")
	saveProfileTokens(t, pathB, srv.URL+"/token", "refresh-b")
	dir := t.TempDir()
	start := filepath.Join(dir, "start")
	readyA := filepath.Join(dir, "a")
	readyB := filepath.Join(dir, "b")
	cmdA := startOAuthChild(t, "refresh", pathA, readyA, start, srv.URL+"/token")
	cmdB := startOAuthChild(t, "refresh", pathB, readyB, start, srv.URL+"/token")
	waitForFile(t, readyA)
	waitForFile(t, readyB)
	if err := os.WriteFile(start, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitChild(t, cmdA, 15*time.Second, 0)
	waitChild(t, cmdB, 15*time.Second, 0)
	mu.Lock()
	defer mu.Unlock()
	if seen["refresh-a"] != 1 || seen["refresh-b"] != 1 || len(seen) != 2 {
		t.Fatalf("refresh spends=%v, want each profile token once", seen)
	}
}

func testExplicitInheritsGlobal(t *testing.T) {
	home := isolateOAuth(t)
	srv := newRotatingTokenServer(t, "refresh-old", 400*time.Millisecond)
	seedTokens(t, srv.URL+"/token", "access-old", "refresh-old", time.Now().Add(-time.Minute))
	explicit := writeProfile(t, home, "profile-shared")
	loaded, err := config.Load(explicit)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RefreshToken != "refresh-old" {
		t.Fatalf("explicit profile refresh=%q, want inherited refresh-old", loaded.RefreshToken)
	}
	dir := t.TempDir()
	start := filepath.Join(dir, "start")
	readyA := filepath.Join(dir, "a")
	readyB := filepath.Join(dir, "b")
	cmdA := startOAuthChild(t, "refresh", "", readyA, start, srv.URL+"/token")
	cmdB := startOAuthChild(t, "refresh", explicit, readyB, start, srv.URL+"/token")
	waitForFile(t, readyA)
	waitForFile(t, readyB)
	if err := os.WriteFile(start, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	outA := waitChild(t, cmdA, 15*time.Second, 0)
	outB := waitChild(t, cmdB, 15*time.Second, 0)
	if srv.spentOld.Load() != 1 || srv.calls.Load() != 1 {
		t.Fatalf("calls=%d spent old=%d, want 1 and 1", srv.calls.Load(), srv.spentOld.Load())
	}
	if !strings.Contains(outA, "access-new") || !strings.Contains(outB, "access-new") {
		t.Fatalf("inherited refresh was spent twice\nA:%s\nB:%s", outA, outB)
	}
}

func testAdoptSuccessor(t *testing.T) {
	home := isolateOAuth(t)
	srv := newRotatingTokenServer(t, "refresh-old", 0)
	seedTokens(t, srv.URL+"/token", "access-old", "refresh-old", time.Now().Add(-time.Minute))
	explicit := writeProfile(t, home, "profile-adopt")
	creds, err := cliutil.CredentialsFilePath()
	if err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := startOAuthChild(t, "hold", "", ready, "", "", "PP_OAUTH_LOCK_PATH="+creds, "PP_OAUTH_HOLD_REWRITE=1")
	waitForFile(t, ready)
	c := reloadClient(t, explicit, srv.URL+"/token")
	if c.Config.RefreshToken != "refresh-old" {
		t.Fatalf("refresh=%q, want refresh-old", c.Config.RefreshToken)
	}
	if err := c.refreshAccessToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitChild(t, cmd, 10*time.Second, 0)
	if srv.calls.Load() != 0 {
		t.Fatalf("token calls=%d, want 0 after adopting the peer token", srv.calls.Load())
	}
	if c.Config.AccessToken != "access-successor" || c.Config.RefreshToken != "refresh-successor" {
		t.Fatalf("adopted access=%q refresh=%q", c.Config.AccessToken, c.Config.RefreshToken)
	}
}

func testProfileDoesNotSwitchToGlobal(t *testing.T) {
	home := isolateOAuth(t)
	var mu sync.Mutex
	seen := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"ok":true}`)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		got := r.Form.Get("refresh_token")
		mu.Lock()
		seen[got]++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"access-for-%s","refresh_token":"next-%s","expires_in":3600}`, got, got)
	}))
	t.Cleanup(srv.Close)
	seedTokens(t, srv.URL+"/token", "access-global", "refresh-global", time.Now().Add(time.Hour))
	explicit := writeProfile(t, home, "profile-own")
	saveProfileTokens(t, explicit, srv.URL+"/token", "refresh-profile")
	c := reloadClient(t, explicit, srv.URL+"/token")
	if c.Config.RefreshToken != "refresh-profile" {
		t.Fatalf("refresh=%q, want refresh-profile", c.Config.RefreshToken)
	}
	if err := c.refreshAccessToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen["refresh-global"] != 0 || seen["refresh-profile"] != 1 {
		t.Fatalf("spent=%v, want only refresh-profile", seen)
	}
	if c.Config.AccessToken != "access-for-refresh-profile" {
		t.Fatalf("access=%q", c.Config.AccessToken)
	}
	global := reloadConfig(t, "", "")
	if global.RefreshToken != "refresh-global" || global.AccessToken != "access-global" {
		t.Fatalf("global access=%q refresh=%q", global.AccessToken, global.RefreshToken)
	}
	profile := reloadConfig(t, explicit, "")
	if profile.RefreshToken != "next-refresh-profile" {
		t.Fatalf("profile refresh=%q", profile.RefreshToken)
	}
}

func testDivergedGlobalIsNotSpent(t *testing.T) {
	home := isolateOAuth(t)
	var mu sync.Mutex
	seen := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		got := r.Form.Get("refresh_token")
		mu.Lock()
		seen[got]++
		mu.Unlock()
		if got == "refresh-global" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"access-for-%s","refresh_token":"next-%s","expires_in":3600}`, got, got)
	}))
	t.Cleanup(srv.Close)
	seedTokens(t, srv.URL+"/token", "access-global", "refresh-global", time.Now().Add(30*time.Second))
	explicit := writeProfile(t, home, "profile-diverged")
	saveProfileTokens(t, explicit, srv.URL+"/token", "refresh-profile")
	c := reloadClient(t, explicit, srv.URL+"/token")
	if err := c.refreshAccessToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen["refresh-global"] != 0 || seen["refresh-profile"] != 1 {
		t.Fatalf("spent=%v", seen)
	}
	global := reloadConfig(t, "", "")
	if global.RefreshToken != "refresh-global" {
		t.Fatalf("global refresh=%q", global.RefreshToken)
	}
}

func testSharedPeerRotationSavedToBoth(t *testing.T) {
	home := isolateOAuth(t)
	var mu sync.Mutex
	seen := map[string]int{}
	var globalLocked atomic.Bool
	globalPath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"ok":true}`)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		got := r.Form.Get("refresh_token")
		mu.Lock()
		seen[got]++
		mu.Unlock()
		if globalPath != "" {
			ok, err := cliutil.TryWithFileLock(globalPath, func() error { return nil })
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			globalLocked.Store(!ok)
		}
		if got != "refresh-successor" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"access-final","refresh_token":"refresh-final","expires_in":3600}`)
	}))
	t.Cleanup(srv.Close)
	seedTokens(t, srv.URL+"/token", "access-old", "refresh-old", time.Now().Add(-time.Minute))
	explicit := writeProfile(t, home, "profile-shared-rotate")
	saveProfileTokens(t, explicit, srv.URL+"/token", "refresh-old")
	var err error
	globalPath, err = cliutil.CredentialsFilePath()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	start := filepath.Join(dir, "start")
	cmd := startOAuthChild(t, "hold", "", ready, start, "", "PP_OAUTH_LOCK_PATH="+globalPath, "PP_OAUTH_HOLD_REWRITE=1", "PP_OAUTH_HOLD_NEAR=1")
	waitForFile(t, ready)
	done := make(chan error, 1)
	go func() {
		cfg, err := config.Load(explicit)
		if err != nil {
			done <- err
			return
		}
		cfg.TokenURL = srv.URL + "/token"
		c := New(cfg, 5*time.Second, 0)
		c.NoCache = true
		done <- c.refreshAccessToken(context.Background())
	}()
	time.Sleep(time.Second)
	if err := os.WriteFile(start, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("refresh did not finish")
	}
	waitChild(t, cmd, 10*time.Second, 0)
	mu.Lock()
	defer mu.Unlock()
	if !globalLocked.Load() || seen["refresh-successor"] != 1 || seen["refresh-old"] != 0 {
		t.Fatalf("locked=%v spent=%v", globalLocked.Load(), seen)
	}
	global := reloadConfig(t, "", "")
	profile := reloadConfig(t, explicit, "")
	if global.RefreshToken != "refresh-final" || profile.RefreshToken != "refresh-final" {
		t.Fatalf("global=%q profile=%q", global.RefreshToken, profile.RefreshToken)
	}
	if global.AccessToken != "access-final" || profile.AccessToken != "access-final" {
		t.Fatalf("global access=%q profile access=%q", global.AccessToken, profile.AccessToken)
	}
}

func testEnvDoesNotSpendOtherLogin(t *testing.T) {
	home := isolateOAuth(t)
	var mu sync.Mutex
	seen := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		got := r.Form.Get("refresh_token")
		mu.Lock()
		seen[got]++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"access-for-%s","refresh_token":"next-%s","expires_in":3600}`, got, got)
	}))
	t.Cleanup(srv.Close)
	seedTokens(t, srv.URL+"/token", "access-global", "refresh-global", time.Now().Add(30*time.Second))
	explicit := writeProfile(t, home, "profile-env")
	saveProfileTokens(t, explicit, srv.URL+"/token", "refresh-profile")
	t.Setenv("OAUTH_REFRESH_TOKEN", "refresh-global")
	c := reloadClient(t, explicit, srv.URL+"/token")
	if c.Config.RefreshToken != "refresh-global" {
		t.Fatalf("env refresh=%q", c.Config.RefreshToken)
	}
	if err := c.refreshAccessToken(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if seen["refresh-global"] != 0 || seen["refresh-profile"] != 1 {
		t.Fatalf("spent=%v", seen)
	}
	t.Setenv("OAUTH_REFRESH_TOKEN", "")
	global := reloadConfig(t, "", "")
	if global.RefreshToken != "refresh-global" {
		t.Fatalf("global refresh=%q", global.RefreshToken)
	}
}

func testHardLinkedCredentialsOneSpend(t *testing.T) {
	home := isolateOAuth(t)
	srv := newRotatingTokenServer(t, "refresh-old", 300*time.Millisecond)
	seedTokens(t, srv.URL+"/token", "access-old", "refresh-old", time.Now().Add(-time.Minute))
	explicit := writeProfile(t, home, "profile-link")
	globalPath, err := cliutil.CredentialsFilePath()
	if err != nil {
		t.Fatal(err)
	}
	profileCreds := filepath.Join(filepath.Dir(explicit), "data", "credentials.toml")
	if err := os.MkdirAll(filepath.Dir(profileCreds), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(globalPath, profileCreds); err != nil {
		t.Skip("hard link unavailable: " + err.Error())
	}
	dir := t.TempDir()
	start := filepath.Join(dir, "start")
	readyA := filepath.Join(dir, "a")
	readyB := filepath.Join(dir, "b")
	cmdA := startOAuthChild(t, "refresh", "", readyA, start, srv.URL+"/token")
	cmdB := startOAuthChild(t, "refresh", explicit, readyB, start, srv.URL+"/token")
	waitForFile(t, readyA)
	waitForFile(t, readyB)
	if err := os.WriteFile(start, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitChild(t, cmdA, 15*time.Second, 0)
	waitChild(t, cmdB, 15*time.Second, 0)
	if srv.calls.Load() != 1 || srv.spentOld.Load() != 1 {
		t.Fatalf("calls=%d spent=%d, want 1 and 1", srv.calls.Load(), srv.spentOld.Load())
	}
}

func testSymlinkedConfigOneSpend(t *testing.T) {
	home := isolateOAuth(t)
	srv := newRotatingTokenServer(t, "refresh-old", 300*time.Millisecond)
	explicit := writeProfile(t, home, "profile-symlink")
	saveProfileTokens(t, explicit, srv.URL+"/token", "refresh-old")
	link := filepath.Join(home, "config-link.toml")
	if err := os.Symlink(explicit, link); err != nil {
		t.Skip("symlink unavailable: " + err.Error())
	}
	dir := t.TempDir()
	start := filepath.Join(dir, "start")
	readyA := filepath.Join(dir, "a")
	readyB := filepath.Join(dir, "b")
	cmdA := startOAuthChild(t, "refresh", explicit, readyA, start, srv.URL+"/token")
	cmdB := startOAuthChild(t, "refresh", link, readyB, start, srv.URL+"/token")
	waitForFile(t, readyA)
	waitForFile(t, readyB)
	if err := os.WriteFile(start, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitChild(t, cmdA, 15*time.Second, 0)
	waitChild(t, cmdB, 15*time.Second, 0)
	if srv.calls.Load() != 1 || srv.spentOld.Load() != 1 {
		t.Fatalf("calls=%d spent=%d, want 1 and 1", srv.calls.Load(), srv.spentOld.Load())
	}
}

func testRefreshErrorRedactsTokens(t *testing.T) {
	isolateOAuth(t)
	const (
		refresh = "refresh-old-secret-value"
		dup     = "refresh-dup-secret-value"
		secret  = "client-secret-value-long"
		access  = "access-body-secret-value"
		idTok   = "id-token-secret-value"
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"revoked `+refresh+`","refresh_token":"`+refresh+`","refresh_token":"`+dup+`","client_secret":"`+secret+`","access_token":"`+access+`","id_token":"`+idTok+`"}`)
	}))
	t.Cleanup(srv.Close)
	cfg := seedTokens(t, srv.URL+"/token", "access-old", refresh, time.Now().Add(-time.Minute))
	cfg.ClientSecret = secret
	c := New(cfg, 5*time.Second, 0)
	c.HTTPClient = srv.Client()
	err := c.refreshAccessToken(context.Background())
	if err == nil {
		t.Fatal("expected refresh error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "HTTP 400") {
		t.Fatalf("error=%q, want HTTP 400", msg)
	}
	for _, secret := range []string{refresh, dup, secret, access, idTok} {
		if strings.Contains(msg, secret) {
			t.Fatalf("error contains secret %q: %s", secret, msg)
		}
	}
}

func testCrashAfterPersist(t *testing.T) {
	isolateOAuth(t)
	srv := newRotatingTokenServer(t, "refresh-old", 200*time.Millisecond)
	seedTokens(t, srv.URL+"/token", "access-old", "refresh-old", time.Now().Add(-time.Minute))
	crash := startOAuthChild(t, "crash", "", "", "", srv.URL+"/token")
	deadline := time.Now().Add(10 * time.Second)
	for srv.calls.Load() == 0 {
		if time.Now().After(deadline) {
			_ = crash.Process.Kill()
			t.Fatal("crash child did not call the token endpoint")
		}
		time.Sleep(10 * time.Millisecond)
	}
	waiter := startOAuthChild(t, "refresh", "", "", "", srv.URL+"/token")
	waitChild(t, crash, 10*time.Second, 75)
	out := waitChild(t, waiter, 10*time.Second, 0)
	if srv.calls.Load() != 1 || srv.spentOld.Load() != 1 {
		t.Fatalf("calls=%d spent=%d, want 1 and 1", srv.calls.Load(), srv.spentOld.Load())
	}
	if !strings.Contains(out, "access-new") {
		t.Fatalf("waiter output=%s", out)
	}
}

func testAgentcookieOneSpend(t *testing.T) {
	isolateOAuth(t)
	srv := newRotatingTokenServer(t, "refresh-old", 300*time.Millisecond)
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(filepath.Dir(cfg.Path), ".agentcookie-managed")
	if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.BaseURL = "https://api.example.com"
	cfg.TokenURL = srv.URL + "/token"
	if err := cfg.SaveTokens("client-id", "client-secret-value", "access-old", "refresh-old", time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	start := filepath.Join(dir, "start")
	readyA := filepath.Join(dir, "a")
	readyB := filepath.Join(dir, "b")
	cmdA := startOAuthChild(t, "refresh", "", readyA, start, srv.URL+"/token")
	cmdB := startOAuthChild(t, "refresh", "", readyB, start, srv.URL+"/token")
	waitForFile(t, readyA)
	waitForFile(t, readyB)
	if err := os.WriteFile(start, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitChild(t, cmdA, 15*time.Second, 0)
	waitChild(t, cmdB, 15*time.Second, 0)
	if srv.calls.Load() != 1 {
		t.Fatalf("token calls=%d, want 1", srv.calls.Load())
	}
	data, err := os.ReadFile(cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("refresh-new")) {
		t.Fatalf("agentcookie config was not updated: %s", data)
	}
}

type rotatingServer struct {
	URL      string
	calls    atomic.Int32
	spentOld atomic.Int32
}

func newRotatingTokenServer(t *testing.T, old string, delay time.Duration) *rotatingServer {
	t.Helper()
	srvState := &rotatingServer{}
	var mu sync.Mutex
	current := old
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"ok":true}`)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		got := r.Form.Get("refresh_token")
		srvState.calls.Add(1)
		mu.Lock()
		match := got == current
		if match && got == old {
			srvState.spentOld.Add(1)
			current = "refresh-new"
		}
		mu.Unlock()
		if !match {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		if delay > 0 {
			time.Sleep(delay)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"access-new","refresh_token":"refresh-new","expires_in":3600}`)
	}))
	t.Cleanup(httpSrv.Close)
	srvState.URL = httpSrv.URL
	return srvState
}

func isolateOAuth(t *testing.T) string {
	t.Helper()
	t.Setenv("PRINTING_PRESS_VERIFY", "")
	t.Setenv("PRINTING_PRESS_VERIFY_LIVE_HTTP", "")
	t.Setenv("PRINTING_PRESS_DOGFOOD", "")
	t.Setenv("OAUTH_REFRESH_TOKEN", "")
	restore, err := cliutil.SetHomeOverride("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restore)
	home := testenv.Isolate(t, cliutil.ConfigDir, cliutil.DataDir, cliutil.StateDir, cliutil.CacheDir)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "localapp"))
	t.Setenv("APPDATA", filepath.Join(home, "appdata"))
	return home
}

func seedTokens(t *testing.T, tokenURL, access, refresh string, expiry time.Time) *config.Config {
	t.Helper()
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	cfg.BaseURL = "https://api.example.com"
	cfg.TokenURL = tokenURL
	if err := cfg.SaveTokens("client-id", "client-secret-value", access, refresh, expiry); err != nil {
		t.Fatal(err)
	}
	return reloadConfig(t, "", tokenURL)
}

func reloadConfig(t *testing.T, path, tokenURL string) *config.Config {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if tokenURL != "" {
		cfg.TokenURL = tokenURL
	}
	return cfg
}

func reloadClient(t *testing.T, path, tokenURL string) *Client {
	t.Helper()
	cfg := reloadConfig(t, path, tokenURL)
	c := New(cfg, 5*time.Second, 0)
	c.NoCache = true
	return c
}

func writeProfile(t *testing.T, home, name string) string {
	t.Helper()
	dir := filepath.Join(home, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("base_url = \"https://api.example.com\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func saveProfileTokens(t *testing.T, path, tokenURL, refresh string) {
	t.Helper()
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.TokenURL = tokenURL
	if err := cfg.SaveTokens("client-id", "client-secret-value", "access-"+refresh, refresh, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
}

func startOAuthChild(t *testing.T, mode, configPath, ready, start, tokenURL string, extra ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestOAuthRefreshFileLock$", "-test.count=1")
	cmd.Env = append(os.Environ(),
		"PP_OAUTH_REFRESH_MODE="+mode,
		"PP_OAUTH_CONFIG="+configPath,
		"PP_OAUTH_READY="+ready,
		"PP_OAUTH_START="+start,
		"PP_OAUTH_TOKEN_URL="+tokenURL,
	)
	cmd.Env = append(cmd.Env, extra...)
	cmd.Stdout = &bytes.Buffer{}
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil && cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})
	return cmd
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func waitChild(t *testing.T, cmd *exec.Cmd, timeout time.Duration, want int) string {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		code := 0
		if err != nil {
			var exitErr *exec.ExitError
			if !errorsAs(err, &exitErr) {
				t.Fatalf("child: %v\nstderr:%s", err, stderrOf(cmd))
			}
			code = exitErr.ExitCode()
		}
		if code != want {
			t.Fatalf("child exit=%d want %d\nstderr:%s\nstdout:%s", code, want, stderrOf(cmd), stdoutOf(cmd))
		}
		return stdoutOf(cmd)
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		t.Fatalf("child timed out\nstderr:%s", stderrOf(cmd))
		return ""
	}
}

func errorsAs(err error, target **exec.ExitError) bool {
	return errors.As(err, target)
}

func stdoutOf(cmd *exec.Cmd) string {
	buf, _ := cmd.Stdout.(*bytes.Buffer)
	if buf == nil {
		return ""
	}
	return buf.String()
}

func stderrOf(cmd *exec.Cmd) string {
	buf, _ := cmd.Stderr.(*bytes.Buffer)
	if buf == nil {
		return ""
	}
	return buf.String()
}
