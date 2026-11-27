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

	require.NoError(t, ReconcileWithHAR(res, harPath))
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

	// Audit a synthetic printed CLI directory.
	cliDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "client"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(cliDir, "internal", "mcp"), 0o755))
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
