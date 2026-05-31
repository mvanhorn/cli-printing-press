package openapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLoadSpecBytes_URLAndFile verifies the URL-vs-file dispatch.
//
// Before #1001, pipeline subcommands (dogfood/verify/scorecard) called
// os.ReadFile directly on the --spec argument and rejected http(s) URLs as
// "no such file or directory" on every platform (Windows worst because the
// error message was misleading). This test pins the new helper's contract:
// http(s) sources route through the fetch path, everything else routes to
// the filesystem, and the dispatch decision is made off the source string
// — not the running platform.
func TestLoadSpecBytes_URLAndFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	specPayload := `{"openapi":"3.0.0","info":{"title":"Test","version":"1.0"},"paths":{}}` + strings.Repeat(" ", 300)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(specPayload))
	}))
	defer srv.Close()

	t.Run("URL source routes to http fetch", func(t *testing.T) {
		data, err := LoadSpecBytes(srv.URL, true, true)
		if err != nil {
			t.Fatalf("LoadSpecBytes(URL) returned error: %v", err)
		}
		if string(data) != specPayload {
			t.Fatalf("LoadSpecBytes(URL) returned unexpected body: got %d bytes, want %d", len(data), len(specPayload))
		}
	})

	t.Run("file source routes to disk read", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "spec.json")
		if err := os.WriteFile(path, []byte(specPayload), 0o644); err != nil {
			t.Fatalf("writing fixture: %v", err)
		}

		data, err := LoadSpecBytes(path, false, false)
		if err != nil {
			t.Fatalf("LoadSpecBytes(file) returned error: %v", err)
		}
		if string(data) != specPayload {
			t.Fatalf("LoadSpecBytes(file) returned unexpected body")
		}
	})

	t.Run("URL with http:// prefix is also remote", func(t *testing.T) {
		if !strings.HasPrefix(srv.URL, "http://") {
			t.Fatalf("test precondition: httptest URL should start with http://, got %s", srv.URL)
		}
		if !IsRemoteSpecSource(srv.URL) {
			t.Fatalf("IsRemoteSpecSource(%q) = false, want true", srv.URL)
		}
	})

	t.Run("Windows-shaped path is not remote", func(t *testing.T) {
		// Regression guard for #1001: a Windows-shaped filesystem path must
		// not be misclassified as remote even though it contains colons.
		path := `C:\Users\dev\spec.json`
		if IsRemoteSpecSource(path) {
			t.Fatalf("IsRemoteSpecSource(%q) = true, want false", path)
		}
	})
}

func TestLoadSpecBytes_FileMissing(t *testing.T) {
	_, err := LoadSpecBytes(filepath.Join(t.TempDir(), "does-not-exist.json"), false, false)
	if err == nil {
		t.Fatal("LoadSpecBytes for missing file should error")
	}
}

func TestFetchOrCacheSpec_Timeout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Send headers, then hang on the body — a server that accepts the
		// connection but never finishes the response.
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-release
	}))
	// close(release) must run before srv.Close() (LIFO defers) so the blocked
	// handler is unblocked before Close waits on it.
	defer srv.Close()
	defer close(release)

	done := make(chan error, 1)
	go func() {
		// Limits are passed as arguments, so no shared package state is mutated.
		_, err := fetchOrCacheSpec(srv.URL, true, true, 50*time.Millisecond, maxSpecBytes)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a timeout error for a hanging response body")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fetchOrCacheSpec did not return within 5s; fetch timeout not enforced")
	}
}

func TestFetchOrCacheSpec_SizeCap(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	body := strings.Repeat("x", 64) // well over the 16-byte cap
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	_, err := fetchOrCacheSpec(srv.URL, true, true, specFetchTimeout, 16)
	if err == nil {
		t.Fatal("expected a size-cap error for an oversized body")
	}
	if !strings.Contains(err.Error(), "size cap") {
		t.Fatalf("error should mention the size cap, got: %v", err)
	}
}

func TestLoadSpecBytes_RemoteServerError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("404 Not Found"))
	}))
	defer srv.Close()

	_, err := LoadSpecBytes(srv.URL, true, true)
	if err == nil {
		t.Fatal("LoadSpecBytes against 404 should error")
	}
	if !strings.Contains(err.Error(), "404") && !strings.Contains(err.Error(), "Not Found") {
		t.Fatalf("error should mention HTTP status, got: %v", err)
	}
}
