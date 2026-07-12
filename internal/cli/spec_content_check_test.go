package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRejectIfNotSpec covers issue #275 F-5's content-validity heuristic.
// A stale catalog spec_url returns "404: Not Found" or an HTML error page;
// the existing openapi.FetchOrCacheSpec status check catches HTTP-level errors, but
// any path that hands a downloaded body to readSpec without that gate (a
// user feeding a curl-saved 404 dump as --spec, a stale cache entry, an
// upstream proxy that masks status codes) bypasses it. rejectIfNotSpec
// surfaces a clear message at the boundary instead of letting the parser
// produce "validation: name is required" two layers down.
func TestRejectIfNotSpec(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		data         []byte
		wantErr      bool
		errSubstring string
	}{
		{"github raw 404 body", []byte("404: Not Found"), true, "HTTP error response"},
		{"github raw 5xx body", []byte("500: Internal Server Error\n"), true, "HTTP error response"},
		{"html error page", []byte("<html><body>Error</body></html>"), true, "HTML page"},
		{"html5 doctype", []byte("<!DOCTYPE html>\n<html>..."), true, "HTML page"},
		{"openapi yaml", []byte("openapi: 3.0.0\ninfo:\n  title: x\n"), false, ""},
		{"openapi json", []byte(`{"openapi":"3.0.0","info":{"title":"x"}}`), false, ""},
		{"graphql sdl", []byte("type Query { hello: String }\n"), false, ""},
		{"plain string with digits in middle", []byte("hello 404: world"), false, ""},
		{"empty bytes", []byte(""), false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := rejectIfNotSpec(tc.data)
			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errSubstring)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestReadSpecRejectsLocal404Dump exercises the readSpec entry point with a
// file containing a GitHub-raw 404 body. Captures the actual bug the F-5
// reporter hit: their smoke methodology curled spec URLs and saved the
// response body before invoking printing-press; stale URLs produced 14-byte
// "404: Not Found" files that previously fed straight into the parser.
func TestReadSpecRejectsLocal404Dump(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	stale := filepath.Join(dir, "stale.spec")
	require.NoError(t, os.WriteFile(stale, []byte("404: Not Found"), 0o644))

	_, err := readSpec(stale, false, true)
	require.Error(t, err, "readSpec must reject a 404-shaped body")
	assert.Contains(t, err.Error(), "HTTP error response")
}
