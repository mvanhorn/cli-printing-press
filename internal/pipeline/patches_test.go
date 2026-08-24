package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidatePatchRecordsZeroPatchesPass(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, ValidatePatchRecords(dir))

	require.NoError(t, EnsurePatchesDir(dir))
	require.NoError(t, ValidatePatchRecords(dir))
}

func TestValidatePatchRecordsPresentCallSitePass(t *testing.T) {
	t.Parallel()

	dir := writePatchTree(t, patchTree{
		files: map[string]string{
			"internal/store/sync.go": "package store\n\nfunc Sync() { guardAgainstErrorEnvelope(body) }\n",
		},
		records: []string{`{
  "schema_version": 2,
  "id": "drop-envelope",
  "files": ["internal/store/sync.go"],
  "call_sites": ["guardAgainstErrorEnvelope("]
}`},
	})

	require.NoError(t, ValidatePatchRecords(dir))
}

func TestValidatePatchRecordsMissingFileFails(t *testing.T) {
	t.Parallel()

	dir := writePatchTree(t, patchTree{
		records: []string{`{
  "schema_version": 2,
  "id": "drop-envelope",
  "files": ["internal/store/sync.go"]
}`},
	})

	err := ValidatePatchRecords(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `patch "drop-envelope"`)
	assert.Contains(t, err.Error(), "internal/store/sync.go")
	assert.Contains(t, err.Error(), "missing")
}

func TestValidatePatchRecordsMissingCallSiteFails(t *testing.T) {
	t.Parallel()

	dir := writePatchTree(t, patchTree{
		files: map[string]string{
			"internal/store/sync.go":     "package store\n\nfunc Sync() { fmt.Println(body) }\n",
			"internal/store/envelope.go": "package store\n\nfunc guardAgainstErrorEnvelope(body []byte) bool { return false }\n",
		},
		records: []string{`{
  "schema_version": 2,
  "id": "drop-envelope",
  "files": ["internal/store/sync.go", "internal/store/envelope.go"],
  "marker": "pp:patch drop-envelope",
  "call_sites": ["if guardAgainstErrorEnvelope("]
}`},
	})

	err := ValidatePatchRecords(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `patch "drop-envelope"`)
	assert.Contains(t, err.Error(), "if guardAgainstErrorEnvelope(")
	assert.Contains(t, err.Error(), "absent from recorded files")
}

func TestValidatePatchRecordsIgnoresNeedleInsidePatchJSON(t *testing.T) {
	t.Parallel()

	dir := writePatchTree(t, patchTree{
		files: map[string]string{
			"internal/store/sync.go": "package store\n\nfunc Sync() {}\n",
		},
		records: []string{`{
  "schema_version": 2,
  "id": "drop-envelope",
  "files": ["internal/store/sync.go"],
  "call_sites": ["guardAgainstErrorEnvelope("]
}`},
	})

	err := ValidatePatchRecords(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "absent from recorded files")
}

func TestValidatePatchRecordsIgnoresNeedleOutsideRecordedFiles(t *testing.T) {
	t.Parallel()

	dir := writePatchTree(t, patchTree{
		files: map[string]string{
			"internal/store/sync.go":  "package store\n\nfunc Sync() {}\n",
			"internal/cli/refresh.go": "package cli\n\nfunc printRefreshTokenExpiry() {}\n",
			"README.md":               "docs mention if guardAgainstErrorEnvelope(body)\n",
		},
		records: []string{`{
  "schema_version": 2,
  "id": "drop-envelope",
  "files": ["internal/store/sync.go"],
  "call_sites": ["if guardAgainstErrorEnvelope("]
}`},
	})

	err := ValidatePatchRecords(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `patch "drop-envelope"`)
	assert.Contains(t, err.Error(), "absent from recorded files")
}

func TestValidatePatchRecordsEmptyRecordFailsClosed(t *testing.T) {
	t.Parallel()

	dir := writePatchTree(t, patchTree{
		records: []string{`{
  "schema_version": 2,
  "id": "empty-claim",
  "summary": "claims a customization shipped"
}`},
	})

	err := ValidatePatchRecords(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `patch "empty-claim"`)
	assert.Contains(t, err.Error(), "declares no files or call sites")
}

func TestValidatePatchRecordsLegacyIndex(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, PatchesIndexFilename), []byte(`{
  "schema_version": 1,
  "applied_at": "2026-08-24",
  "base_run_id": "test",
  "base_printing_press_version": "4.0.0",
  "patches": [
    {"id": "legacy-guard", "files": ["missing.go"]}
  ]
}`), 0o644))

	err := ValidatePatchRecords(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `patch "legacy-guard"`)
	assert.Contains(t, err.Error(), "missing.go")
}

func TestPreservePatchRecordsCopiesSnapshotJSON(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	dst := t.TempDir()
	require.NoError(t, EnsurePatchesDir(src))
	require.NoError(t, EnsurePatchesDir(dst))
	record := []byte(`{"schema_version":2,"id":"drop-envelope","files":["internal/store/sync.go"]}` + "\n")
	require.NoError(t, os.WriteFile(filepath.Join(src, PatchesDirName, "drop-envelope.json"), record, 0o644))

	require.NoError(t, PreservePatchRecords(src, dst))

	got, err := os.ReadFile(filepath.Join(dst, PatchesDirName, "drop-envelope.json"))
	require.NoError(t, err)
	assert.Equal(t, record, got)
}

func TestLoadPatchRecordsMalformedJSONFailsClosed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, EnsurePatchesDir(dir))
	require.NoError(t, os.WriteFile(filepath.Join(dir, PatchesDirName, "broken.json"), []byte("{not-json"), 0o644))

	_, err := LoadPatchRecords(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broken.json")
}

type patchTree struct {
	files   map[string]string
	records []string
}

func writePatchTree(t *testing.T, tree patchTree) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, EnsurePatchesDir(dir))
	for rel, body := range tree.files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
	}
	for _, raw := range tree.records {
		var rec PatchRecord
		require.NoError(t, json.Unmarshal([]byte(raw), &rec))
		name := rec.ID + ".json"
		if rec.ID == "" {
			name = "unnamed.json"
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, PatchesDirName, name), []byte(raw+"\n"), 0o644))
	}
	return dir
}
