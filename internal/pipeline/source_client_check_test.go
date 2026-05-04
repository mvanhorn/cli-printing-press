package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeInternal drops content at <cliDir>/internal/<relPath>, creating parents.
// relPath is the path under internal/ (e.g. "source/sec/sec.go").
func writeInternal(t *testing.T, cliDir, relPath, content string) {
	t.Helper()
	full := filepath.Join(cliDir, "internal", relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}

func TestCheckSourceClients_NoSiblingPackages(t *testing.T) {
	cliDir := t.TempDir()
	writeInternal(t, cliDir, "client/client.go", "package client\n")
	writeInternal(t, cliDir, "store/store.go", "package store\n")

	got := checkSourceClients(cliDir)
	if !got.Skipped {
		t.Errorf("Skipped = false, want true (no sibling packages)")
	}
	if len(got.Findings) != 0 {
		t.Errorf("Findings = %v, want none", got.Findings)
	}
}

func TestCheckSourceClients_NoInternalDir(t *testing.T) {
	cliDir := t.TempDir()
	got := checkSourceClients(cliDir)
	if !got.Skipped {
		t.Errorf("Skipped = false, want true (no internal/ at all)")
	}
}

func TestCheckSourceClients_Findings(t *testing.T) {
	const cliutilLimiterClient = `package sec

import (
	"net/http"
	"example.com/foo/internal/cliutil"
)

type Client struct {
	HTTP    *http.Client
	limiter *cliutil.AdaptiveLimiter
}

func (c *Client) Fetch(url string) error {
	c.limiter.Wait()
	req, _ := http.NewRequest("GET", url, nil)
	resp, err := c.HTTP.Do(req)
	if err != nil { return err }
	if resp.StatusCode == http.StatusTooManyRequests {
		c.limiter.OnRateLimit()
		return &cliutil.RateLimitError{URL: url}
	}
	c.limiter.OnSuccess()
	return nil
}
`
	const noLimiter = `package sec

import "net/http"

type Client struct{ HTTP *http.Client }

func (c *Client) Fetch(url string) error {
	req, _ := http.NewRequest("GET", url, nil)
	resp, err := c.HTTP.Do(req)
	if err != nil { return err }
	if resp.StatusCode == 429 { return nil }
	return nil
}
`
	const generatedClientHelper = `package sec

import (
	"context"

	"example.com/foo/internal/client"
)

func Fetch(ctx context.Context, c *client.Client, path string) ([]byte, error) {
	return c.Get(ctx, path, nil, nil)
}
`
	const rawHTTPWithGeneratedClientImport = `package sec

import (
	"net/http"

	"example.com/foo/internal/client"
)

var _ *client.Client

func Fetch(url string) error {
	resp, err := http.Get(url)
	if err != nil { return err }
	_ = resp
	return nil
}
`
	const generatedClientImportWithUnrelatedCGet = `package sec

import "example.com/foo/internal/client"

var _ *client.Client

type Cache struct{}

func (c *Cache) Get(key string) ([]byte, error) {
	return nil, nil
}

func Fetch(c *Cache, key string) ([]byte, error) {
	return c.Get(key)
}
`
	const limiterButNo429 = `package sec

import (
	"net/http"
)

type secLimiter struct{}
func (*secLimiter) Wait()         {}
func (*secLimiter) OnSuccess()    {}
func (*secLimiter) OnRateLimit()  {}

type Client struct {
	HTTP    *http.Client
	limiter *secLimiter
}

func (c *Client) Fetch(url string) error {
	c.limiter.Wait()
	req, _ := http.NewRequest("GET", url, nil)
	_, err := c.HTTP.Do(req)
	if err != nil { return err }
	c.limiter.OnSuccess()
	return nil
}
`
	const bareHTTP = `package recipes

import "net/http"

func Fetch(url string) error {
	resp, err := http.Get(url)
	if err != nil { return err }
	_ = resp
	return nil
}
`
	const localCopiesPass = `package sec

import (
	"net/http"
)

type adaptiveLimiter struct{}
func (*adaptiveLimiter) Wait()        {}
func (*adaptiveLimiter) OnSuccess()   {}
func (*adaptiveLimiter) OnRateLimit() {}

type RateLimitError struct{ URL string }
func (e *RateLimitError) Error() string { return "rate limited: " + e.URL }

type Client struct {
	HTTP    *http.Client
	limiter *adaptiveLimiter
}

func (c *Client) Fetch(url string) error {
	c.limiter.Wait()
	req, _ := http.NewRequestWithContext(nil, "GET", url, nil)
	resp, err := c.HTTP.Do(req)
	if err != nil { return err }
	if resp.StatusCode == http.StatusTooManyRequests {
		c.limiter.OnRateLimit()
		return &RateLimitError{URL: url}
	}
	c.limiter.OnSuccess()
	return nil
}
`
	const reservedHTTP = `package store

import "net/http"

func Sync(url string) error {
	resp, err := http.Get(url)
	if err != nil { return err }
	_ = resp
	return nil
}
`
	const testFileWithHTTP = `package sec

import (
	"net/http"
	"testing"
)

func TestFoo(t *testing.T) {
	resp, err := http.Get("http://x")
	if err != nil { t.Fatal(err) }
	_ = resp
}
`
	const compliantSec = `package sec

import (
	"net/http"
	"example.com/foo/internal/cliutil"
)

func Fetch(url string, l *cliutil.AdaptiveLimiter) error {
	l.Wait()
	resp, err := http.Get(url)
	if err != nil { return err }
	if resp.StatusCode == http.StatusTooManyRequests {
		return &cliutil.RateLimitError{URL: url}
	}
	return nil
}
`
	const ignoredFixtureFile = `package fixture

import "net/http"
func Bad(url string) error { _, err := http.Get(url); return err }
`
	const limiterFieldOnlyName = `package sec

import "net/http"

type fooLimiter struct{}

type Client struct{ HTTP *http.Client; flagLimiter fooLimiter }

func (c *Client) Fetch(url string) error {
	resp, err := http.Get(url)
	if err != nil { return err }
	_ = resp
	return nil
}
`

	cases := []struct {
		name           string
		files          map[string]string
		wantFindings   int
		wantReasonHas  string
		wantPackage    string // checked when non-empty against Findings[0]
		wantCheckedPos bool   // assert Checked > 0
	}{
		{
			name:           "passes with cliutil limiter and error",
			files:          map[string]string{"source/sec/sec.go": cliutilLimiterClient},
			wantCheckedPos: true,
		},
		{
			name:          "flags missing limiter",
			files:         map[string]string{"source/sec/sec.go": noLimiter},
			wantFindings:  1,
			wantReasonHas: "rate limiter",
			wantPackage:   "source/sec",
		},
		{
			name:           "generated client helper passes without local limiter",
			files:          map[string]string{"source/sec/sec.go": generatedClientHelper},
			wantCheckedPos: true,
		},
		{
			name:          "generated client import does not exempt raw HTTP",
			files:         map[string]string{"source/sec/sec.go": rawHTTPWithGeneratedClientImport},
			wantFindings:  1,
			wantReasonHas: "without rate limiter or typed 429",
		},
		{
			name:          "generated client import does not exempt unrelated c Get",
			files:         map[string]string{"source/sec/sec.go": generatedClientImportWithUnrelatedCGet},
			wantFindings:  1,
			wantReasonHas: "without rate limiter or typed 429",
		},
		{
			name:          "flags swallowed 429",
			files:         map[string]string{"source/sec/sec.go": limiterButNo429},
			wantFindings:  1,
			wantReasonHas: "RateLimitError",
		},
		{
			name:          "flags both missing",
			files:         map[string]string{"recipes/archive.go": bareHTTP},
			wantFindings:  1,
			wantReasonHas: "without rate limiter or typed 429",
		},
		{
			name:  "locally-defined limiter and RateLimitError both pass",
			files: map[string]string{"source/sec/sec.go": localCopiesPass},
		},
		{
			name: "ignores reserved packages",
			files: map[string]string{
				"store/store.go":        reservedHTTP,
				"source/dummy/dummy.go": "package dummy\n",
			},
		},
		{
			name:  "ignores _test.go files",
			files: map[string]string{"source/sec/sec_test.go": testFileWithHTTP},
		},
		{
			name: "ignores testdata and vendor",
			files: map[string]string{
				"source/sec/sec.go":              compliantSec,
				"source/sec/testdata/fixture.go": ignoredFixtureFile,
				"source/sec/vendor/x/x.go":       ignoredFixtureFile,
			},
		},
		{
			name:          "identifier ending in Limiter alone is not enough",
			files:         map[string]string{"source/sec/sec.go": limiterFieldOnlyName},
			wantFindings:  1,
			wantReasonHas: "rate limiter",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cliDir := t.TempDir()
			for path, content := range tc.files {
				writeInternal(t, cliDir, path, content)
			}
			got := checkSourceClients(cliDir)
			if len(got.Findings) != tc.wantFindings {
				t.Fatalf("Findings = %d, want %d: %+v", len(got.Findings), tc.wantFindings, got.Findings)
			}
			if tc.wantReasonHas != "" && !strings.Contains(got.Findings[0].Reason, tc.wantReasonHas) {
				t.Errorf("Reason = %q, want substring %q", got.Findings[0].Reason, tc.wantReasonHas)
			}
			if tc.wantPackage != "" && got.Findings[0].Package != tc.wantPackage {
				t.Errorf("Package = %q, want %q", got.Findings[0].Package, tc.wantPackage)
			}
			if tc.wantCheckedPos && got.Checked == 0 {
				t.Errorf("Checked = 0, want > 0")
			}
		})
	}
}
