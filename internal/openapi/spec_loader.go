package openapi

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// smallResponseErrorPrefix matches the "404: Not Found" / "500: ..." shape
// some upstreams return as a 200-body payload instead of an HTTP error. The
// small-response branch in FetchOrCacheSpec uses it to surface a clear
// rejection at the boundary rather than feeding the body to the parser.
var smallResponseErrorPrefix = regexp.MustCompile(`^\d{3}:\s`)

// LoadSpecBytes reads OpenAPI spec bytes from either a local filesystem path
// or an http(s) URL, picking the right transport from the source string.
// Callers that previously wrapped os.ReadFile gain URL support transparently;
// the local-path branch is identical to the prior os.ReadFile call.
//
// URL responses are cached under ~/.cache/printing-press/specs for 24h to
// match the generator's intake behavior; pass refresh=true to bypass the
// cache and skipCache=true to skip writing it.
func LoadSpecBytes(source string, refresh bool, skipCache bool) ([]byte, error) {
	if IsRemoteSpecSource(source) {
		return FetchOrCacheSpec(source, refresh, skipCache)
	}
	return os.ReadFile(source)
}

// FetchOrCacheSpec downloads a spec over http(s) with a 24h on-disk cache.
// Exported so packages outside internal/cli (notably internal/pipeline) can
// route URL-sourced specs through the same fetch path the generator uses,
// keeping scorer subcommands and generate in sync.
func FetchOrCacheSpec(specURL string, refresh bool, skipCache bool) ([]byte, error) {
	sum := sha256.Sum256([]byte(specURL))
	cacheKey := hex.EncodeToString(sum[:])

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("finding user home directory: %w", err)
	}

	cacheDir := filepath.Join(homeDir, ".cache", "printing-press", "specs")
	cachePath := filepath.Join(cacheDir, cacheKey+".json")

	if !refresh {
		info, err := os.Stat(cachePath)
		switch {
		case err == nil && time.Since(info.ModTime()) < 24*time.Hour:
			fmt.Fprintf(os.Stderr, "Using cached spec for %s\n", specURL)
			data, readErr := os.ReadFile(cachePath)
			if readErr != nil {
				return nil, fmt.Errorf("reading cached spec: %w", readErr)
			}
			return data, nil
		case err != nil && !os.IsNotExist(err):
			return nil, fmt.Errorf("checking cached spec: %w", err)
		}
	}

	fmt.Fprintf(os.Stderr, "Fetching spec from %s...\n", specURL)
	resp, err := http.Get(specURL) //nolint:gosec // spec URLs are operator-provided
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("unexpected response status: %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if len(data) < 256 {
		trimmed := strings.TrimSpace(string(data))
		if strings.HasPrefix(trimmed, "<") ||
			smallResponseErrorPrefix.MatchString(trimmed) {
			return nil, fmt.Errorf("spec_url %s returned a small response that does not look like an OpenAPI spec (%d bytes): %q",
				specURL, len(data), truncFifty(trimmed))
		}
	}

	if !skipCache {
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return nil, fmt.Errorf("creating cache directory: %w", err)
		}
		if err := os.WriteFile(cachePath, data, 0o644); err != nil {
			return nil, fmt.Errorf("writing cached spec: %w", err)
		}
	}

	return data, nil
}

func truncFifty(s string) string {
	if len(s) > 50 {
		return s[:50] + "..."
	}
	return s
}
