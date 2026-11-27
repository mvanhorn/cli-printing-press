package appsniff

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/mvanhorn/cli-printing-press/v4/internal/browsersniff"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsForbiddenSecretPath(t *testing.T) {
	t.Parallel()

	forbidden := []string{
		"IndexedDB/https_app.example.com_0.indexeddb.leveldb/000003.log",
		"/Users/test/Library/Application Support/App/Cookies",
		"/Users/test/Library/Application Support/App/Local Storage/leveldb/000005.ldb",
		"Session Storage/000001.ldb",
		"Keychain/login.keychain-db",
		"config/credentials.json",
		"app/.env.production",
		"certs/client.p12",
		"db/sessions.sqlite3",
	}
	for _, p := range forbidden {
		assert.True(t, IsForbiddenSecretPath(p), "expected forbidden: %s", p)
	}

	allowed := []string{
		"Service Worker/CacheStorage/abc/123_0",
		"Contents/Resources/app.asar",
		"dist/main.bundle.js",
		"assets/index.android.bundle",
	}
	for _, p := range allowed {
		assert.False(t, IsForbiddenSecretPath(p), "expected allowed: %s", p)
	}
}

func TestRedactStringAndSanitizeURL(t *testing.T) {
	t.Parallel()

	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	out := RedactString("Authorization: Bearer " + jwt + " token=xoxb-1234567890-abcdefghijkl")
	assert.NotContains(t, out, "eyJhbGci")
	assert.NotContains(t, out, "xoxb-1234567890")
	assert.Contains(t, out, browsersniff.RedactedSentinel)

	cleanURL := SanitizeURL("https://user:secretpass@api.example.com/v1/items?access_token=supersecret123&limit=20")
	assert.NotContains(t, cleanURL, "secretpass")
	assert.NotContains(t, cleanURL, "supersecret123")
	assert.Contains(t, cleanURL, "limit=20")
	assert.Contains(t, cleanURL, "api.example.com/v1/items")
}

func buildTestASAR(t *testing.T, files map[string][]byte) string {
	t.Helper()

	type node struct {
		Files  map[string]node `json:"files,omitempty"`
		Size   int             `json:"size,omitempty"`
		Offset string          `json:"offset,omitempty"`
	}

	root := node{Files: make(map[string]node)}
	var payload bytes.Buffer
	for name, data := range files {
		off := payload.Len()
		payload.Write(data)
		root.Files[name] = node{
			Size:   len(data),
			Offset: intToString(off),
		}
	}

	headerJSON, err := json.Marshal(root)
	require.NoError(t, err)
	pad := (4 - (len(headerJSON) % 4)) % 4
	paddedHeader := append(headerJSON, bytes.Repeat([]byte{0}, pad)...)

	headerPayloadSize := uint32(4 + len(paddedHeader))
	headerPickleSize := uint32(4 + headerPayloadSize)

	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint32(4))
	_ = binary.Write(&buf, binary.LittleEndian, headerPickleSize)
	_ = binary.Write(&buf, binary.LittleEndian, headerPayloadSize)
	_ = binary.Write(&buf, binary.LittleEndian, uint32(len(headerJSON)))
	buf.Write(paddedHeader)
	buf.Write(payload.Bytes())

	path := filepath.Join(t.TempDir(), "app.asar")
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o644))
	return path
}

func intToString(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestExtractASARAndDesktopDiscovery(t *testing.T) {
	t.Parallel()

	js := `
		const SHARDS = {
			pecan: "https://pecan.api.example-notes.test",
			maple: "https://maple.api.example-notes.test"
		};
		async function load(id) {
			await fetch("https://api.example-notes.test/v1/notes/" + id, {
				method: "POST",
				headers: { "authorization": "Bearer " + tok, "x-client-version": "7.867.0" },
				body: JSON.stringify({ title: "x", workspace_id: "w1" })
			});
			await axios.get("https://api.example-notes.test/v1/search");
		}
	`
	asarPath := buildTestASAR(t, map[string][]byte{
		"main.js": []byte(js),
	})

	res, err := DiscoverDesktopApp(asarPath, Options{Name: "Example Notes"})
	require.NoError(t, err)
	assert.Equal(t, TargetKindDesktop, res.TargetKind)
	assert.Equal(t, "https://api.example-notes.test", res.PrimaryBaseURL)
	assert.Equal(t, AuthBearer, res.Auth.Scheme)
	assert.GreaterOrEqual(t, len(res.Meta.Shards), 2)

	// Reserved resource name "search" must be disambiguated so spec.Validate passes.
	apiSpec, err := BuildSpec(res, Options{Name: "Example Notes"})
	require.NoError(t, err)
	assert.Equal(t, "example-notes", apiSpec.Name)
	for resName := range apiSpec.Resources {
		_, isReserved := spec.ReservedCobraUseNames[resName]
		assert.False(t, isReserved, "resource %q collides with reserved Cobra name", resName)
	}

	analysis := BuildTrafficAnalysis(res, apiSpec)
	assert.Equal(t, "1", analysis.Version)
	assert.NotEmpty(t, analysis.EndpointClusters)
	assert.NotEmpty(t, analysis.Protocols)
}

func TestParseSimpleCacheEntryAndSecretExclusion(t *testing.T) {
	t.Parallel()

	jsPayload := []byte(`fetch("https://edgeapi.example-chat.test/api/bookmarks.list", { method: "POST" }); api.call("auth.teams.list");`)

	makeSimpleCache := func(key string, body []byte) []byte {
		var b bytes.Buffer
		_ = binary.Write(&b, binary.LittleEndian, simpleCacheInitialMagic)
		_ = binary.Write(&b, binary.LittleEndian, uint32(5))
		_ = binary.Write(&b, binary.LittleEndian, uint32(len(key)))
		_ = binary.Write(&b, binary.LittleEndian, uint32(0))
		_ = binary.Write(&b, binary.LittleEndian, uint32(0))
		b.WriteString(key)
		b.Write(body)
		b.Write(simpleCacheEOFMagicLE)
		return b.Bytes()
	}

	// Plain UTF-8 entry
	plainRaw := makeSimpleCache("1/0/_dk_https://app.example-chat.test https://cdn.example-chat.test/static/main.a1b2c3d4.js", jsPayload)
	entry, err := ParseSimpleCacheEntry(plainRaw, defaultMaxFileBytes)
	require.NoError(t, err)
	assert.Contains(t, entry.Key, "https://cdn.example-chat.test/static/main.a1b2c3d4.js")
	assert.Contains(t, string(entry.Payload), "bookmarks.list")

	// Gzipped entry
	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	_, err = gw.Write(jsPayload)
	require.NoError(t, err)
	require.NoError(t, gw.Close())
	gzEntry, err := ParseSimpleCacheEntry(makeSimpleCache("https://cdn.example-chat.test/chunk.js", gzBuf.Bytes()), defaultMaxFileBytes)
	require.NoError(t, err)
	assert.Contains(t, string(gzEntry.Payload), "auth.teams.list")

	// Brotli entry
	var brBuf bytes.Buffer
	bw := brotli.NewWriter(&brBuf)
	_, err = bw.Write(jsPayload)
	require.NoError(t, err)
	require.NoError(t, bw.Close())
	brEntry, err := ParseSimpleCacheEntry(makeSimpleCache("https://cdn.example-chat.test/br.js", brBuf.Bytes()), defaultMaxFileBytes)
	require.NoError(t, err)
	assert.Contains(t, string(brEntry.Payload), "bookmarks.list")

	// Directory scan must skip forbidden Cookies/IndexedDB paths while reading CacheStorage.
	dir := t.TempDir()
	cacheSub := filepath.Join(dir, "CacheStorage", "bucket")
	secretSub := filepath.Join(dir, "IndexedDB")
	require.NoError(t, os.MkdirAll(cacheSub, 0o755))
	require.NoError(t, os.MkdirAll(secretSub, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cacheSub, "abcdef01_0"), plainRaw, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(secretSub, "secret_0"), makeSimpleCache("https://secret.example.test/leak.js", []byte(`fetch("https://secret.example.test/v1/leak")`)), 0o644))

	res, err := DiscoverCacheDir(dir, Options{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, res.Stats.FilesSkippedSecret, 1)
	assert.GreaterOrEqual(t, len(res.Endpoints), 2)
	for _, ep := range res.Endpoints {
		assert.NotContains(t, ep.Path, "leak")
	}
}

func TestParseInfoPlistXML(t *testing.T) {
	t.Parallel()

	plistXML := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDisplayName</key>
	<string>ExampleApp</string>
	<key>CFBundleIdentifier</key>
	<string>com.example.mobile</string>
	<key>CFBundleShortVersionString</key>
	<string>3.4.5</string>
	<key>CFBundleExecutable</key>
	<string>ExampleApp</string>
	<key>NSAppTransportSecurity</key>
	<dict>
		<key>NSExceptionDomains</key>
		<dict>
			<key>api.example-mobile.test</key>
			<dict><key>NSIncludesSubdomains</key><true/></dict>
		</dict>
	</dict>
</dict>
</plist>`

	info, err := ParseInfoPlist([]byte(plistXML))
	require.NoError(t, err)
	assert.Equal(t, "ExampleApp", info.AppName)
	assert.Equal(t, "com.example.mobile", info.BundleID)
	assert.Equal(t, "3.4.5", info.Version)
	assert.Equal(t, []string{"api.example-mobile.test"}, info.ATSDomains)
}

func TestReconcileWithHARAndAuditCLI(t *testing.T) {
	t.Parallel()

	res := &DiscoveryResult{
		Target:         "/fake/app.asar",
		TargetKind:     TargetKindDesktop,
		PrimaryBaseURL: "https://api.example-notes.test",
		Meta: BinaryMeta{
			AppName: "example-notes",
			Version: "7.867.0",
			Shards: []ShardRoute{
				{Name: "pecan", Host: "pecan.api.example-notes.test"},
				{Name: "maple", Host: "maple.api.example-notes.test"},
			},
		},
		Endpoints: []Endpoint{
			{Method: "GET", Path: "/v1/notes/{id}", Host: "api.example-notes.test", BaseURL: "https://api.example-notes.test", Protocol: ProtocolREST, Source: "asar:main.js", Confidence: ConfidenceLow},
			{Method: "GET", Path: "/v1/folders", Host: "api.example-notes.test", BaseURL: "https://api.example-notes.test", Protocol: ProtocolREST, Source: "asar:main.js", Confidence: ConfidenceMedium},
		},
	}

	har := `{
		"log": {
			"entries": [
				{
					"request": {
						"method": "POST",
						"url": "https://api.example-notes.test/v1/notes/550e8400-e29b-41d4-a716-446655440000?include_transcript=true",
						"headers": [{"name": "Authorization", "value": "Bearer secret-token-value"}]
					},
					"response": {"status": 200, "content": {"mimeType": "application/json", "text": "{\"ok\":true}"}}
				},
				{
					"request": {
						"method": "GET",
						"url": "https://api.example-notes.test/v1/workspaces",
						"headers": []
					},
					"response": {"status": 200, "content": {"mimeType": "application/json", "text": "[]"}}
				}
			]
		}
	}`
	harPath := filepath.Join(t.TempDir(), "capture.har")
	require.NoError(t, os.WriteFile(harPath, []byte(har), 0o644))

	require.NoError(t, ReconcileWithHAR(res, harPath, Options{}))
	require.NotNil(t, res.Reconciliation)
	assert.Equal(t, 1, res.Reconciliation.Confirmed)
	assert.Equal(t, 1, res.Reconciliation.ObservedOnly)
	assert.Equal(t, 1, res.Reconciliation.StaticOnly)

	// Verify low-confidence static GET was promoted to POST and observed auth header name recorded (never value).
	var foundNotes bool
	for _, ep := range res.Endpoints {
		if ep.Path == "/v1/notes/{id}" {
			foundNotes = true
			assert.Equal(t, "POST", ep.Method)
			assert.Equal(t, ReconcileConfirmed, ep.ReconcileState)
			assert.Contains(t, ep.ObservedAuth, "authorization")
		}
	}
	assert.True(t, foundNotes)

	// Audit a synthetic printed CLI directory. Include a saved spec YAML listing
	// /v1/folders to ensure AuditCLI only counts implemented Go source routes
	// and does not falsely match /v1/notes as a substring of /v1/notes/{id}.
	cliDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "client"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "mcp"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "spec.yaml"), []byte("path: /v1/folders\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "internal", "client", "client.go"), []byte(`
		package client
		const ClientVersion = "7.299.0"
		const shardHost = "pecan.api.example-notes.test"
		func get() { _ = "/v1/notes/{id}"; _ = "/v1/extra" }
	`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(cliDir, "internal", "mcp", "server.go"), []byte(`
		package mcp
		var codeOrchEndpoints = []struct{ Path string }{{Path: "/v1/notes/{id}"}}
	`), 0o644))

	audit, err := AuditCLI(cliDir, res)
	require.NoError(t, err)
	assert.True(t, audit.VersionDrift)
	assert.Equal(t, "7.299.0", audit.CLIVersion)
	assert.Equal(t, "7.867.0", audit.AppVersion)
	require.Len(t, audit.MissingShards, 1)
	assert.Equal(t, "maple.api.example-notes.test", audit.MissingShards[0].Host)
	assert.Equal(t, 1, audit.CoveredCount)
	assert.Equal(t, 2, audit.MissingCount)
	assert.Contains(t, audit.MCPParityGaps, "/v1/extra")
}

func TestHardeningAndReviewRegressions(t *testing.T) {
	t.Parallel()

	// 1. Nested fetch options, no fake GET on axios.delete, JWT redaction in URL paths,
	// no fabricated /graphql/<OpName> routes, and port-distinct origins.
	js := `
		async function run() {
			await fetch("https://api.vendor.test/v1/items", {
				headers: { "x-trace": "1", "authorization": "Bearer secret-tok" },
				method: "POST",
				body: JSON.stringify({ title: "hello", count: 3 })
			});
			await axios.delete("https://api.vendor.test/v1/widgets");
			await fetch("https://api.vendor.test:8443/v1/admin", { method: "GET" });
			const leak = "https://api.vendor.test/v1/reset/eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U";
			const gql = "query GetUser($id: ID!) { user(id: $id) { id } }";
		}
	`
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "app.js"), []byte(js), 0o644))
	// Symlink pointing at a forbidden .env file must be skipped.
	secretFile := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(secretFile, []byte(`fetch("https://secret.vendor.test/v1/leaked")`), 0o644))
	_ = os.Symlink(secretFile, filepath.Join(dir, "linked.js"))

	res, err := DiscoverDesktopApp(dir, Options{Name: "Vendor"})
	require.NoError(t, err)

	var methodsForWidgets []string
	var foundItemsPost, foundItemsTitle, foundAdminPort, foundRedactedReset bool
	for _, ep := range res.Endpoints {
		assert.NotContains(t, ep.Path, "eyJhbGci", "JWT must be redacted from path")
		assert.NotEqual(t, "/graphql/GetUser", ep.Path, "must not fabricate /graphql/<OpName>")
		assert.NotContains(t, ep.Path, "leaked", "symlinked secret file must be skipped")
		if ep.Path == "/v1/widgets" {
			methodsForWidgets = append(methodsForWidgets, ep.Method)
		}
		if ep.Path == "/v1/items" && ep.Method == "POST" {
			foundItemsPost = true
			for _, bk := range ep.BodyKeys {
				if bk == "title" {
					foundItemsTitle = true
				}
			}
		}
		if ep.Path == "/v1/admin" && ep.BaseURL == "https://api.vendor.test:8443" {
			foundAdminPort = true
		}
		if ep.Path == "/v1/reset/{token}" {
			foundRedactedReset = true
		}
	}
	assert.Equal(t, []string{"DELETE"}, methodsForWidgets, "axios.delete must not emit invented GET")
	assert.True(t, foundItemsPost, "nested fetch options must recover POST")
	assert.True(t, foundItemsTitle, "nested fetch options must recover body keys")
	assert.True(t, foundAdminPort, "non-default port origin must stay distinct")
	assert.True(t, foundRedactedReset, "JWT segment in path must become {token}")

	// BuildSpec must keep per-endpoint BaseURL for the :8443 port endpoint.
	apiSpec, err := BuildSpec(res, Options{Name: "Vendor", BaseURL: "https://api.vendor.test"})
	require.NoError(t, err)
	var adminEpBase string
	for _, r := range apiSpec.Resources {
		for _, ep := range r.Endpoints {
			if ep.Path == "/v1/admin" {
				adminEpBase = ep.BaseURL
			}
		}
	}
	assert.Equal(t, "https://api.vendor.test:8443", adminEpBase)

	// 2. CacheStorage stem preserves directory path across distinct bundles.
	assert.NotEqual(t,
		normalizedCacheStem("https://cdn.vendor.test/editor/main.abcdef12.js"),
		normalizedCacheStem("https://cdn.vendor.test/settings/main.123456ab.js"),
	)
	assert.Equal(t,
		normalizedCacheStem("https://cdn.vendor.test/editor/main.abcdef12.js"),
		normalizedCacheStem("https://cdn.vendor.test/editor/main.99887766.js"),
	)

	// 3. HAR reconciliation: respects origin, routes gRPC to GRPCEndpoints, enforces --host and --top.
	harRes := &DiscoveryResult{
		PrimaryBaseURL: "https://api.alpha.test",
		Endpoints: []Endpoint{
			{Method: "GET", Path: "/v1/users", Host: "api.alpha.test", BaseURL: "https://api.alpha.test", Protocol: ProtocolREST, Confidence: ConfidenceLow},
			{Method: "GET", Path: "/v1/users", Host: "api.beta.test", BaseURL: "https://api.beta.test", Protocol: ProtocolREST, Confidence: ConfidenceLow},
		},
	}
	harBody := `{
		"log": {
			"entries": [
				{"request":{"method":"POST","url":"https://api.beta.test/v1/users","headers":[]},"response":{"status":201,"content":{"mimeType":"application/json","text":"{}"}}},
				{"request":{"method":"POST","url":"https://api.beta.test/com.beta.v1.UserService/GetUser","headers":[]},"response":{"status":200,"content":{"mimeType":"application/grpc","text":""}}},
				{"request":{"method":"GET","url":"https://api.gamma.test/v1/ignored","headers":[]},"response":{"status":200,"content":{"mimeType":"application/json","text":"{}"}}}
			]
		}
	}`
	harFile := filepath.Join(t.TempDir(), "reconcile.har")
	require.NoError(t, os.WriteFile(harFile, []byte(harBody), 0o644))
	require.NoError(t, ReconcileWithHAR(harRes, harFile, Options{HostFilters: []string{"api.alpha.test", "api.beta.test"}}))

	for _, ep := range harRes.Endpoints {
		assert.NotEqual(t, ProtocolGRPC, ep.Protocol, "HAR gRPC route must not leak into REST Endpoints")
		assert.NotEqual(t, "api.gamma.test", ep.Host, "HAR route must honor --host filter")
		if ep.Host == "api.alpha.test" {
			assert.Equal(t, ReconcileStaticOnly, ep.ReconcileState, "beta HAR must not confirm alpha route")
			assert.Equal(t, "GET", ep.Method)
		}
		if ep.Host == "api.beta.test" {
			assert.Equal(t, ReconcileConfirmed, ep.ReconcileState)
			assert.Equal(t, "POST", ep.Method)
		}
	}
	require.Len(t, harRes.GRPCEndpoints, 1)
	assert.Equal(t, "/com.beta.v1.UserService/GetUser", harRes.GRPCEndpoints[0].Path)
}

func TestBinaryPlistPureGoAndExecutableContainment(t *testing.T) {
	t.Parallel()

	// Build a minimal valid bplist00 dict:
	//   obj 0: dict { 1 -> 2, 3 -> 4 }
	//   obj 1: "CFBundleDisplayName"
	//   obj 2: "BinApp"
	//   obj 3: "CFBundleExecutable"
	//   obj 4: "BinApp"
	var b bytes.Buffer
	b.WriteString(bplistMagic)
	offsets := make([]byte, 5)
	offsets[0] = byte(b.Len())
	b.Write([]byte{0xd2, 1, 3, 2, 4}) // dict of 2 entries: keys [1,3], vals [2,4]
	writeASCII := func(idx int, s string) {
		offsets[idx] = byte(b.Len())
		if len(s) < 15 {
			b.WriteByte(0x50 | byte(len(s)))
		} else {
			b.WriteByte(0x5f)
			b.WriteByte(0x10)
			b.WriteByte(byte(len(s)))
		}
		b.WriteString(s)
	}
	writeASCII(1, "CFBundleDisplayName")
	writeASCII(2, "BinApp")
	writeASCII(3, "CFBundleExecutable")
	writeASCII(4, "BinApp")

	offTable := uint64(b.Len())
	b.Write(offsets)
	// 32-byte trailer: 6 unused, offsetIntSize=1, objectRefSize=1, numObjects=5, topObject=0, offTable
	b.Write(make([]byte, 6))
	b.WriteByte(1)
	b.WriteByte(1)
	_ = binary.Write(&b, binary.BigEndian, uint64(5))
	_ = binary.Write(&b, binary.BigEndian, uint64(0))
	_ = binary.Write(&b, binary.BigEndian, offTable)

	info, err := ParseInfoPlist(b.Bytes())
	require.NoError(t, err)
	assert.Equal(t, "BinApp", info.AppName)

	// Path traversal in CFBundleExecutable must be rejected.
	appDir := t.TempDir()
	_, err = safeBundleExecutablePath(appDir, "../../.env")
	assert.Error(t, err)
	_, err = safeBundleExecutablePath(appDir, ".env")
	assert.Error(t, err)

	// Shared-reference DAG (billion-laughs style) bplist must decode in O(N) via cache
	// without exponential blowup, and cyclic references must fail cleanly.
	var dag bytes.Buffer
	dag.WriteString(bplistMagic)
	const levels = 25
	dagOffsets := make([]byte, levels+1)
	for i := range levels {
		dagOffsets[i] = byte(dag.Len())
		next := byte(i + 1)
		dag.Write([]byte{0xa2, next, next}) // array of 2 refs to next level
	}
	dagOffsets[levels] = byte(dag.Len())
	dag.Write([]byte{0x51, 'x'}) // leaf "x"
	dagOffTable := uint64(dag.Len())
	dag.Write(dagOffsets)
	dag.Write(make([]byte, 6))
	dag.WriteByte(1)
	dag.WriteByte(1)
	_ = binary.Write(&dag, binary.BigEndian, uint64(levels+1))
	_ = binary.Write(&dag, binary.BigEndian, uint64(0))
	_ = binary.Write(&dag, binary.BigEndian, dagOffTable)

	val, err := parseBinaryPlist(dag.Bytes())
	require.NoError(t, err)
	require.NotNil(t, val)

	// Cyclic bplist (obj 0 -> [0]) must return an error.
	var cyc bytes.Buffer
	cyc.WriteString(bplistMagic)
	cycOff := byte(cyc.Len())
	cyc.Write([]byte{0xa1, 0})
	cycOffTable := uint64(cyc.Len())
	cyc.WriteByte(cycOff)
	cyc.Write(make([]byte, 6))
	cyc.WriteByte(1)
	cyc.WriteByte(1)
	_ = binary.Write(&cyc, binary.BigEndian, uint64(1))
	_ = binary.Write(&cyc, binary.BigEndian, uint64(0))
	_ = binary.Write(&cyc, binary.BigEndian, cycOffTable)
	_, err = parseBinaryPlist(cyc.Bytes())
	assert.Error(t, err)
}

func TestDistinctHostExactMethodDoesNotHideOtherHostRoute(t *testing.T) {
	t.Parallel()

	js := `
		axios.post("https://api.alpha.test/v1/users");
		const other = "https://api.beta.test/v1/users";
	`
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "multi.js"), []byte(js), 0o644))
	res, err := DiscoverDesktopApp(dir, Options{Name: "Multi"})
	require.NoError(t, err)

	var gotAlphaPost, gotBetaGet bool
	for _, ep := range res.Endpoints {
		if ep.Host == "api.alpha.test" && ep.Path == "/v1/users" && ep.Method == "POST" {
			gotAlphaPost = true
		}
		if ep.Host == "api.beta.test" && ep.Path == "/v1/users" && ep.Method == "GET" {
			gotBetaGet = true
		}
	}
	assert.True(t, gotAlphaPost, "expected POST on api.alpha.test")
	assert.True(t, gotBetaGet, "expected GET on api.beta.test not to be hidden by api.alpha.test POST")
}

func TestHostRankingPenalizesNonAPISubdomains(t *testing.T) {
	t.Parallel()

	// Paired positive and negative fixtures for isNonAPISubdomain.
	penalized := []string{
		"www.brand.test",
		"docs.brand.test",
		"help.brand.test",
		"support.brand.test",
		"status.brand.test",
		"blog.brand.test",
		"cdn.brand.test",
		"media.brand.test",
		"app-media.brand.test",
		"static.brand.test",
		"assets.brand.test",
		"images.brand.test",
		"img.brand.test",
	}
	for _, host := range penalized {
		assert.True(t, isNonAPISubdomain(host), "expected non-API subdomain penalty for %s", host)
	}

	nonPenalized := []string{
		"api.brand.test",
		"client-api.brand.test",
		"app-api.brand.test",
		"brand.test",
		"gateway.brand.test",
	}
	for _, host := range nonPenalized {
		assert.False(t, isNonAPISubdomain(host), "did not expect penalty for %s", host)
	}

	// End-to-end ranking: repeated media/static URLs must lose to the first-party API host.
	js := `
		const a = "https://app-media.example-brand.test/logo.png";
		const b = "https://app-media.example-brand.test/banner.png";
		const c = "https://static.example-brand.test/bundle.js";
		const d = "https://images.example-brand.test/hero.png";
		fetch("https://client-api.example-brand.test/v1/status");
	`
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "rank.js"), []byte(js), 0o644))
	res, err := DiscoverDesktopApp(dir, Options{Name: "Example Brand"})
	require.NoError(t, err)
	assert.Equal(t, "https://client-api.example-brand.test", res.PrimaryBaseURL)
}
