package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withTimeout temporarily swaps remoteSpecTimeout for the duration of the test.
func withTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := remoteSpecTimeout
	remoteSpecTimeout = d
	t.Cleanup(func() { remoteSpecTimeout = prev })
}

// withMaxBytes temporarily swaps maxRemoteSpecBytes for the duration of the test.
func withMaxBytes(t *testing.T, n int64) {
	t.Helper()
	prev := maxRemoteSpecBytes
	maxRemoteSpecBytes = n
	t.Cleanup(func() { maxRemoteSpecBytes = prev })
}

func TestLoadSpec_HappyPath(t *testing.T) {
	body := "openapi: 3.0.0\ninfo:\n  title: tiny\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	got, err := loadSpec(srv.URL)
	if err != nil {
		t.Fatalf("loadSpec returned error: %v", err)
	}
	if string(got) != body {
		t.Fatalf("loadSpec returned %q, want %q", string(got), body)
	}
}

func TestLoadSpec_LocalFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "spec.yaml")
	body := []byte("base_url: https://example.com\nresources: {}\n")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	got, err := loadSpec(path)
	if err != nil {
		t.Fatalf("loadSpec returned error: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("loadSpec returned %q, want %q", string(got), string(body))
	}
}

func TestLoadSpec_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	_, err := loadSpec(srv.URL)
	if err == nil {
		t.Fatalf("loadSpec returned nil error for 500 response")
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("error %q does not mention HTTP 500", err.Error())
	}
}

// TestLoadSpec_StalledServer verifies the request times out instead of hanging
// forever when the server flushes headers but never sends the body.
func TestLoadSpec_StalledServer(t *testing.T) {
	withTimeout(t, 200*time.Millisecond)

	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Block until the test ends so the body never arrives.
		select {
		case <-released:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(released)
		srv.Close()
	})

	start := time.Now()
	_, err := loadSpec(srv.URL)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("loadSpec returned nil error for stalled server")
	}
	// Allow generous slack (~3x the configured timeout) for scheduler jitter.
	if elapsed > 3*remoteSpecTimeout+500*time.Millisecond {
		t.Fatalf("loadSpec took %v, expected to error within ~3x timeout (%v)", elapsed, remoteSpecTimeout)
	}
}

// TestLoadSpec_Oversized verifies that responses larger than maxRemoteSpecBytes
// produce an explicit oversized-spec error instead of silently truncating.
func TestLoadSpec_Oversized(t *testing.T) {
	withMaxBytes(t, 1024)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		// Stream maxRemoteSpecBytes + 1 bytes so the read sees one byte past the limit.
		buf := bytes.Repeat([]byte{'a'}, int(maxRemoteSpecBytes)+1)
		_, _ = w.Write(buf)
	}))
	t.Cleanup(srv.Close)

	_, err := loadSpec(srv.URL)
	if err == nil {
		t.Fatalf("loadSpec returned nil error for oversized response")
	}
	want := fmt.Sprintf("%d bytes", maxRemoteSpecBytes)
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error %q does not mention byte limit %q", err.Error(), want)
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error %q does not mention 'exceeds'", err.Error())
	}
}

// TestLoadSpec_AtLimit verifies a response exactly at the byte limit succeeds.
func TestLoadSpec_AtLimit(t *testing.T) {
	withMaxBytes(t, 1024)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		buf := bytes.Repeat([]byte{'a'}, int(maxRemoteSpecBytes))
		_, _ = w.Write(buf)
	}))
	t.Cleanup(srv.Close)

	got, err := loadSpec(srv.URL)
	if err != nil {
		t.Fatalf("loadSpec returned error for at-limit response: %v", err)
	}
	if int64(len(got)) != maxRemoteSpecBytes {
		t.Fatalf("loadSpec returned %d bytes, want %d", len(got), maxRemoteSpecBytes)
	}
}
