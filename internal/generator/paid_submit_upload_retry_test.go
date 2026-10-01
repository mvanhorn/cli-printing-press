package generator

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/openapi"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

// paidSubmitUploadSpec models a generation API: a JSON submit that starts
// billable work and returns a job ID, a status read, free file uploads
// (multipart and raw), a file-carrying POST that is NOT an upload (billed
// transcription), an upload that the spec explicitly opts out of replay,
// and a POST search whose body carries cursor-named numeric and boolean
// fields.
const paidSubmitUploadSpec = `openapi: "3.0.3"
info:
  title: Paid Media
  version: "1.0.0"
servers:
  - url: https://api.example.com/v1
security:
  - ApiKeyAuth: []
components:
  securitySchemes:
    ApiKeyAuth:
      type: apiKey
      in: header
      name: X-API-Key
  schemas:
    RenderJob:
      type: object
      properties:
        job_id: {type: string}
        status: {type: string}
paths:
  /renders:
    post:
      tags: [renders]
      operationId: submitRender
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                prompt: {type: string}
      responses:
        "202":
          description: Accepted
          content:
            application/json:
              schema: {$ref: "#/components/schemas/RenderJob"}
  /renders/{id}:
    get:
      tags: [renders]
      operationId: getRender
      parameters:
        - {name: id, in: path, required: true, schema: {type: string}}
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema: {$ref: "#/components/schemas/RenderJob"}
  /media/upload:
    post:
      tags: [media]
      operationId: uploadMedia
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: object
              properties:
                file: {type: string, format: binary}
      responses:
        "200": {description: OK}
  /media/upload/binary:
    post:
      tags: [media]
      operationId: uploadMediaBinary
      requestBody:
        content:
          application/octet-stream:
            schema: {type: string, format: binary}
      responses:
        "200": {description: OK}
  /transcriptions:
    post:
      tags: [transcriptions]
      operationId: createTranscription
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: object
              properties:
                file: {type: string, format: binary}
      responses:
        "200": {description: OK}
  /avatars/upload:
    post:
      tags: [avatars]
      operationId: uploadAvatar
      x-pp-replay-safe: false
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: object
              properties:
                file: {type: string, format: binary}
      responses:
        "200": {description: OK}
  /billings/search:
    post:
      tags: [billings]
      operationId: searchBillings
      x-pp-mutation: false
      parameters:
        - {name: since, in: query, schema: {type: number}}
        - {name: include_deleted, in: query, schema: {type: boolean}}
      requestBody:
        content:
          application/json:
            schema:
              type: object
              required: [strict]
              properties:
                page: {type: integer, default: 1}
                page_size: {type: integer}
                offset: {type: integer}
                cursor: {type: integer}
                min_time: {type: number}
                archived: {type: boolean}
                strict: {type: boolean}
      responses:
        "200": {description: OK}
`

func TestEndpointReplaySafeClassification(t *testing.T) {
	t.Parallel()

	upload := spec.Endpoint{
		Method:             "POST",
		Path:               "/media/upload",
		RequestContentType: "multipart/form-data",
		Body:               []spec.Param{{Name: "file", Type: "string", Format: "binary"}},
	}
	rawUpload := spec.Endpoint{Method: "POST", Path: "/v1/uploads", RequestContentType: "application/octet-stream"}
	billedFile := spec.Endpoint{
		Method:             "POST",
		Path:               "/transcriptions",
		RequestContentType: "multipart/form-data",
		Body:               []spec.Param{{Name: "file", Type: "string", Format: "binary"}},
	}
	jsonSubmit := spec.Endpoint{Method: "POST", Path: "/predictions/upload-model", Body: []spec.Param{{Name: "prompt", Type: "string"}}}
	uploaderRead := spec.Endpoint{Method: "GET", Path: "/uploads"}
	optedOut := upload
	optedOut.ReplaySafe = new(false)
	optedIn := jsonSubmit
	optedIn.ReplaySafe = new(true)

	cases := map[string]struct {
		endpoint spec.Endpoint
		want     string
	}{
		"multipart upload replays":            {upload, "true"},
		"raw upload replays":                  {rawUpload, "true"},
		"billed file POST keeps default":      {billedFile, ""},
		"JSON submit keeps default":           {jsonSubmit, ""},
		"GET keeps default":                   {uploaderRead, ""},
		"explicit opt-out wins over upload":   {optedOut, "false"},
		"explicit opt-in wins for JSON POSTs": {optedIn, "true"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.want, endpointReplaySafe(tc.endpoint))
		})
	}
}

func TestAsyncStatusRecoveryPrefixFollowsIDBinding(t *testing.T) {
	t.Parallel()

	build := func(idParam spec.Param, extra ...spec.Param) *spec.APISpec {
		return &spec.APISpec{Resources: map[string]spec.Resource{
			"renders": {Endpoints: map[string]spec.Endpoint{
				"submit": {Method: "POST", Path: "/renders"},
				"get":    {Method: "GET", Path: "/renders/{id}", Params: append([]spec.Param{idParam}, extra...)},
			}},
		}}
	}
	positional := spec.Param{Name: "id", Type: "string", Positional: true, PathParam: true, Required: true}
	flagged := spec.Param{Name: "id", Type: "string", PathParam: true, Default: "latest"}

	require.Equal(t, "renders get ", asyncStatusRecoveryPrefix(build(positional), "renders", "get"))
	require.Equal(t, "renders get --id ", asyncStatusRecoveryPrefix(build(flagged), "renders", "get"),
		"a defaulted ID is a flag; appending a positional would fetch the default job")
	require.Equal(t, "jobs get ", asyncStatusRecoveryPrefix(build(flagged, spec.Param{Name: "region", Type: "string", Positional: true, Required: true}), "renders", "get"),
		"an extra required positional cannot be filled from the job ID alone")
	require.Equal(t, "jobs get ", asyncStatusRecoveryPrefix(build(spec.Param{Name: "task", Type: "string", Positional: true}), "renders", "get"))
	require.Equal(t, "jobs get ", asyncStatusRecoveryPrefix(build(positional, spec.Param{Name: "workspace", Type: "string", Required: true}), "renders", "get"),
		"a required non-ID flag cannot be filled from the job ID alone")
	require.Equal(t, "renders get ", asyncStatusRecoveryPrefix(build(positional, spec.Param{Name: "workspace", Type: "string", Required: true, Default: "main"}), "renders", "get"),
		"a required flag with a default is filled by the generated command")
	require.Equal(t, "jobs get ", asyncStatusRecoveryPrefix(build(positional, spec.Param{Name: "region", Type: "string", Positional: true, Default: "us"}), "renders", "get"),
		"another positional, even defaulted, would shift which argument the job ID binds to")
}

func TestParserReadsReplaySafeExtension(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(paidSubmitUploadSpec))
	require.NoError(t, err)
	var found bool
	for _, resource := range apiSpec.Resources {
		for _, endpoint := range resource.Endpoints {
			if endpoint.Path != "/avatars/upload" {
				continue
			}
			found = true
			value, set := endpoint.ReplaySafeOverride()
			require.True(t, set, "x-pp-replay-safe must be parsed")
			require.False(t, value)
		}
	}
	require.True(t, found)
}

// TestGeneratedPaidSubmitUploadRetryAndRecovery compiles a printed CLI and
// exercises the emitted client and command behavior against httptest:
// paid submits never replay, uploads retry with a size-scaled deadline,
// a submitted job whose wait fails exits 8 with the job ID and recovery
// command, and body/query scalars keep their declared JSON types.
func TestGeneratedPaidSubmitUploadRetryAndRecovery(t *testing.T) {
	t.Parallel()

	apiSpec, err := openapi.Parse([]byte(paidSubmitUploadSpec))
	require.NoError(t, err)
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	gen := New(apiSpec, outputDir)
	require.NoError(t, gen.Generate())

	var info AsyncJobInfo
	var ok bool
	for key, candidate := range gen.AsyncJobs {
		if strings.HasPrefix(key, "renders/") {
			info, ok = candidate, true
		}
	}
	require.True(t, ok, "submit endpoint must be detected as async: %v", gen.AsyncJobs)
	require.Equal(t, "renders get", info.StatusCommand)

	uploadSrc := readGeneratedCLIFileContaining(t, outputDir, `"pp:path": "/media/upload"`)
	require.Contains(t, uploadSrc, `"X-Printing-Press-Replay-Safe": "true"`)
	rawUploadSrc := readGeneratedCLIFileContaining(t, outputDir, `"pp:path": "/media/upload/binary"`)
	require.Contains(t, rawUploadSrc, `"X-Printing-Press-Replay-Safe": "true"`)
	avatarSrc := readGeneratedCLIFileContaining(t, outputDir, `"pp:path": "/avatars/upload"`)
	require.Contains(t, avatarSrc, `"X-Printing-Press-Replay-Safe": "false"`)
	transcriptionSrc := readGeneratedCLIFileContaining(t, outputDir, `"pp:path": "/transcriptions"`)
	require.NotContains(t, transcriptionSrc, "X-Printing-Press-Replay-Safe")
	submitSrc := readGeneratedCLIFileContaining(t, outputDir, `"pp:path": "/renders"`)
	require.NotContains(t, submitSrc, "X-Printing-Press-Replay-Safe")
	require.Contains(t, submitSrc, `asyncJobPendingErr(cmd, flags, asyncJobID, "paid-media-pp-cli renders get "+asyncJobID, werr)`)

	mcpSrc := readGeneratedFile(t, outputDir, "internal", "mcp", "tools.go")
	require.Contains(t, mcpSrc, `client.ReplaySafeHeader: "true"`)
	require.Contains(t, mcpSrc, `client.ReplaySafeHeader: "false"`)

	billingsSrc := readGeneratedCLIFileContaining(t, outputDir, `"pp:path": "/billings/search"`)
	for _, want := range []string{
		"var bodyPage int",
		"var bodyPageSize int",
		"var bodyOffset int",
		"var bodyCursor int",
		"var bodyMinTime float64",
		"var bodyArchived bool",
		"var bodyStrict string",
		"var flagSince float64",
		"var flagIncludeDeleted bool",
		`cmd.Flags().IntVar(&bodyPage, "page", 1,`,
		`cmd.Flags().IntVar(&bodyCursor, "cursor", 0,`,
		`cmd.Flags().Float64Var(&bodyMinTime, "min-time", 0.0,`,
	} {
		require.Contains(t, billingsSrc, want)
	}

	requireGeneratedCompiles(t, outputDir)

	const clientRuntimeTest = `package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"paid-media-pp-cli/internal/config"
)

type paidRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn paidRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func paidResponse(req *http.Request, status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(` + "`{\"ok\":true}`" + `)),
		Request:    req,
	}
}

func newPaidClient(t *testing.T, transport http.RoundTripper) *Client {
	t.Helper()
	c := New(&config.Config{BaseURL: "https://api.example.invalid", Path: filepath.Join(t.TempDir(), "config.toml")}, time.Second, 0)
	c.NoCache = true
	c.HTTPClient = &http.Client{Transport: transport, Timeout: 5 * time.Second}
	return c
}

func TestPaid_SubmitNeverReplaysAfterAmbiguousFailure(t *testing.T) {
	for name, fail := range map[string]func(*http.Request) (*http.Response, error){
		"gateway timeout": func(req *http.Request) (*http.Response, error) { return paidResponse(req, http.StatusGatewayTimeout), nil },
		"dropped connection": func(*http.Request) (*http.Response, error) { return nil, errors.New("connection reset by peer") },
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			c := newPaidClient(t, paidRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				return fail(req)
			}))
			if _, _, err := c.Post(context.Background(), "/renders", map[string]string{"prompt": "x"}); err == nil {
				t.Fatal("Post() error = nil, want failure")
			}
			if calls != 1 {
				t.Fatalf("paid submit sent %d times, want exactly 1", calls)
			}
		})
	}
}

func TestPaid_ReplaySafeFalseBlocksVerbDefault(t *testing.T) {
	calls := 0
	c := newPaidClient(t, paidRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return paidResponse(req, http.StatusBadGateway), nil
	}))
	_, _, err := c.PutWithHeaders(context.Background(), "/jobs/1", map[string]string{"a": "b"}, map[string]string{ReplaySafeHeader: "false"})
	if err == nil || calls != 1 {
		t.Fatalf("PUT with replay-safe=false = calls %d err %v; want one failed attempt", calls, err)
	}
}

func TestPaid_ReplaySafeFalseWinsOverReadIntent(t *testing.T) {
	calls := 0
	c := newPaidClient(t, paidRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("connection reset by peer")
	}))
	_, _, err := c.PostQueryWithParamsAndHeaders(context.Background(), "/search", nil, map[string]string{"q": "x"}, map[string]string{ReplaySafeHeader: "false"})
	if err == nil || calls != 1 {
		t.Fatalf("read-intent POST with replay-safe=false = calls %d err %v; want one attempt", calls, err)
	}
}

func TestPaid_UploadRetriesTransientFailures(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "in.png")
	if err := os.WriteFile(file, []byte("png-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, fail := range map[string]func(*http.Request) (*http.Response, error){
		"socket stall": func(*http.Request) (*http.Response, error) { return nil, errors.New("read tcp: read: operation timed out") },
		"server error": func(req *http.Request) (*http.Response, error) { return paidResponse(req, http.StatusBadGateway), nil },
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			var bodies []string
			c := newPaidClient(t, paidRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if req.Header.Get(ReplaySafeHeader) != "" {
					t.Errorf("internal %s header leaked onto the wire", ReplaySafeHeader)
				}
				b, _ := io.ReadAll(req.Body)
				bodies = append(bodies, string(b))
				if calls == 1 {
					return fail(req)
				}
				return paidResponse(req, http.StatusOK), nil
			}))
			_, status, err := c.PostMultipartWithParamsAndHeaders(context.Background(), "/media/upload", nil, nil, map[string]string{"file": file}, map[string]string{ReplaySafeHeader: "true"})
			if err != nil || status != http.StatusOK || calls != 2 {
				t.Fatalf("upload = calls %d status %d err %v; want retry then 200", calls, status, err)
			}
			if !strings.Contains(bodies[1], "png-bytes") {
				t.Fatalf("retried upload body lost the file: %q", bodies[1])
			}
		})
	}
}

func TestPaid_UploadDoesNotRetryClientErrors(t *testing.T) {
	calls := 0
	c := newPaidClient(t, paidRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return paidResponse(req, http.StatusBadRequest), nil
	}))
	_, _, err := c.SendRaw(context.Background(), "POST", "/media/upload/binary", nil, []byte("x"), "application/octet-stream", map[string]string{ReplaySafeHeader: "true"})
	if err == nil || calls != 1 {
		t.Fatalf("upload 400 = calls %d err %v; want one attempt", calls, err)
	}
}

func TestPaid_UploadDeadlineScalesWithSize(t *testing.T) {
	if got := uploadTransferAllowance(false, 10<<20); got != 0 {
		t.Fatalf("JSON allowance = %s, want 0", got)
	}
	if got := uploadTransferAllowance(true, 1); got != time.Second {
		t.Fatalf("1-byte allowance = %s, want 1s", got)
	}
	if got := uploadTransferAllowance(true, 3<<20); got != 24*time.Second {
		t.Fatalf("3 MiB allowance = %s, want 24s at 128 KiB/s", got)
	}

	// A 1s --timeout would cut off a slow multi-megabyte upload; the
	// allowance keeps it alive without mutating the shared client.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		time.Sleep(1500 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(` + "`{\"ok\":true}`" + `))
	}))
	defer slow.Close()
	c := New(&config.Config{BaseURL: slow.URL, Path: filepath.Join(t.TempDir(), "config.toml")}, time.Second, 0)
	c.NoCache = true
	shared := c.HTTPClient
	payload := make([]byte, 3<<20)
	if _, _, err := c.SendRaw(context.Background(), "POST", "/media/upload/binary", nil, payload, "application/octet-stream", map[string]string{ReplaySafeHeader: "true"}); err != nil {
		t.Fatalf("slow 3 MiB upload failed under a 1s timeout: %v", err)
	}
	if shared.Timeout != time.Second {
		t.Fatalf("shared client timeout mutated to %s", shared.Timeout)
	}
	// The same slow response on a JSON request still honors --timeout.
	if _, _, err := c.Post(context.Background(), "/renders", map[string]string{"prompt": "x"}); err == nil {
		t.Fatal("JSON POST outlived --timeout; allowance must apply only to file uploads")
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "client", "paid_submit_upload_test.go"), []byte(clientRuntimeTest), 0o644))
	runGoCommand(t, outputDir, "test", "./internal/client", "-run", "^TestPaid_", "-count=1")

	// Command-level: a submitted job whose wait fails exits 8 and prints
	// the job ID and recovery command; the body carries JSON numbers.
	binaryPath := filepath.Join(outputDir, naming.CLI(apiSpec.Name))
	runGoCommand(t, outputDir, "build", "-o", binaryPath, "./cmd/"+naming.CLI(apiSpec.Name))

	server := newPaidSubmitTestServer(t)
	env := append(os.Environ(),
		"HOME="+t.TempDir(),
		"XDG_STATE_HOME="+t.TempDir(),
		"XDG_CONFIG_HOME="+t.TempDir(),
		"PAID_MEDIA_API_KEY=test-key",
		"PAID_MEDIA_BASE_URL="+server.URL,
	)

	out, err := runGeneratedCLI(t, binaryPath, env, "renders", "submit", "--prompt", "cat", "--wait", "--wait-timeout", "4s", "--wait-interval", "200ms", "--json")
	requireExitCode(t, err, 8)
	require.Contains(t, out, "job_123")
	require.Contains(t, out, "paid-media-pp-cli renders get job_123")
	require.Contains(t, out, `"recovery_command"`)
	require.Equal(t, 1, server.submits(), "a failed wait must not resubmit the paid job")
	require.GreaterOrEqual(t, server.polls(), 2, "transient poll failures must not abandon the wait; output:\n%s", out)

	out, err = runGeneratedCLI(t, binaryPath, env, "billings", "--strict", "true", "--page-size", "10", "--cursor", "1700000000", "--archived", "--json")
	require.NoError(t, err, out)
	body := server.lastSearchBody()
	for _, want := range []string{`"page":1`, `"page_size":10`, `"cursor":1700000000`, `"archived":true`, `"strict":true`} {
		require.Contains(t, body, want, "search body %s", body)
	}
	require.NotContains(t, body, `"page":"1"`)
}

type paidSubmitTestServer struct {
	*httptest.Server
	mu         sync.Mutex
	submitN    int
	pollN      int
	searchBody string
}

// newPaidSubmitTestServer accepts one render submit, then fails or stalls
// every status poll (503, then non-terminal) so --wait never finishes.
func newPaidSubmitTestServer(t *testing.T) *paidSubmitTestServer {
	t.Helper()
	s := &paidSubmitTestServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/renders":
			s.submitN++
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"job_id":"job_123","status":"queued"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/renders/job_123":
			s.pollN++
			if s.pollN == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":"busy"}`))
				return
			}
			_, _ = w.Write([]byte(`{"job_id":"job_123","status":"processing"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/billings/search":
			b, _ := io.ReadAll(r.Body)
			s.searchBody = string(b)
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *paidSubmitTestServer) submits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.submitN
}

func (s *paidSubmitTestServer) polls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pollN
}

func (s *paidSubmitTestServer) lastSearchBody() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.ReplaceAll(s.searchBody, " ", "")
}
