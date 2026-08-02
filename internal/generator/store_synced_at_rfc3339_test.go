package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateStoreSyncedAtStoredAsRFC3339ForSQLite(t *testing.T) {
	t.Parallel()

	apiSpec := minimalSpec("synced-at-format")
	outputDir := filepath.Join(t.TempDir(), "synced-at-format-pp-cli")
	gen := New(apiSpec, outputDir)
	gen.VisionSet = VisionTemplateSet{Store: true}
	require.NoError(t, gen.Generate())

	testPath := filepath.Join(outputDir, "internal", "store", "synced_at_sqlite_time_test.go")
	require.NoError(t, os.WriteFile(testPath, []byte(`package store

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"
)

func TestSyncedAtIsSQLiteStrftimeParseable(t *testing.T) {
	s, err := Open(t.TempDir() + "/data.db")
	if err != nil {
		t.Fatalf("NewStore error: %v", err)
	}
	defer s.Close()

	legacy := "2026-05-17 02:23:57.241892 -0700 PDT m=+0.49"
	if _, err := s.DB().Exec(
		"INSERT INTO resources (id, resource_type, data, synced_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		"1", "items", `+"`"+`{"id":"1"}`+"`"+`, legacy, legacy,
	); err != nil {
		t.Fatalf("seed legacy format row error: %v", err)
	}

	var legacyNormalized sql.NullString
	if err := s.DB().QueryRow(
		"SELECT strftime('%Y-%m-%dT%H:%M:%SZ', synced_at) FROM resources WHERE resource_type = ? AND id = ?",
		"items", "1",
	).Scan(&legacyNormalized); err != nil {
		t.Fatalf("legacy strftime scan error: %v", err)
	}
	if legacyNormalized.Valid {
		t.Fatalf("legacy Go verbose timestamp unexpectedly parseable: %q", legacyNormalized.String)
	}

	if err := s.Upsert("items", "1", json.RawMessage(`+"`"+`{"id":"1"}`+"`"+`)); err != nil {
		t.Fatalf("Upsert error: %v", err)
	}

	var normalized sql.NullString
	if err := s.DB().QueryRow(
		"SELECT strftime('%Y-%m-%dT%H:%M:%SZ', synced_at) FROM resources WHERE resource_type = ? AND id = ?",
		"items", "1",
	).Scan(&normalized); err != nil {
		t.Fatalf("strftime scan error: %v", err)
	}
	if !normalized.Valid {
		t.Fatalf("strftime returned NULL for synced_at")
	}
	if normalized.String == "" {
		t.Fatalf("strftime returned empty timestamp")
	}

	// Round-trip sync_state.last_synced_at: SaveSyncState now writes an
	// RFC3339 string, and GetSyncState scans it back into a time.Time. Verify
	// the driver parses the Z-suffixed string into a non-zero time (the code
	// path this change introduces).
	if err := s.SaveSyncState("items", "cursor-xyz", 5); err != nil {
		t.Fatalf("SaveSyncState error: %v", err)
	}
	_, lastSynced, count, err := s.GetSyncState("items")
	if err != nil {
		t.Fatalf("GetSyncState error: %v", err)
	}
	if lastSynced.IsZero() {
		t.Fatalf("GetSyncState returned zero time — RFC3339 last_synced_at did not scan back into time.Time")
	}
	if count != 5 {
		t.Fatalf("GetSyncState count = %d, want 5", count)
	}

	watermark := time.Date(2026, 5, 17, 2, 24, 0, 0, time.UTC)
	if err := s.SaveSyncStateAt("items", "cursor-watermark", 6, watermark); err != nil {
		t.Fatalf("SaveSyncStateAt error: %v", err)
	}
	if err := s.SaveSyncProgress("items", "cursor-progress", 7); err != nil {
		t.Fatalf("SaveSyncProgress error: %v", err)
	}
	cursor, progressSyncedAt, progressCount, err := s.GetSyncState("items")
	if err != nil {
		t.Fatalf("GetSyncState after SaveSyncProgress error: %v", err)
	}
	if cursor != "cursor-progress" || progressCount != 7 {
		t.Fatalf("progress state = cursor %q, count %d; want cursor-progress, 7", cursor, progressCount)
	}
	if !progressSyncedAt.Equal(watermark) {
		t.Fatalf("SaveSyncProgress rewrote watermark to %v, want %v", progressSyncedAt, watermark)
	}

	if err := s.SaveSyncProgress("new-items", "cursor-new", 1); err != nil {
		t.Fatalf("SaveSyncProgress new resource error: %v", err)
	}
	_, newSyncedAt, _, err := s.GetSyncState("new-items")
	if err != nil {
		t.Fatalf("GetSyncState new resource error: %v", err)
	}
	if !newSyncedAt.IsZero() {
		t.Fatalf("new resource progress watermark = %v, want zero time", newSyncedAt)
	}

	if err := s.SaveSyncCursor("items", "cursor-next"); err != nil {
		t.Fatalf("SaveSyncCursor error: %v", err)
	}
	var rawLastSynced string
	if err := s.DB().QueryRow(
		"SELECT last_synced_at FROM sync_state WHERE resource_type = ?",
		"items",
	).Scan(&rawLastSynced); err != nil {
		t.Fatalf("raw last_synced_at scan error: %v", err)
	}
	if _, err := time.Parse(time.RFC3339, rawLastSynced); err != nil {
		t.Fatalf("SaveSyncCursor wrote non-RFC3339 last_synced_at %q: %v", rawLastSynced, err)
	}
	cursor, cursorSyncedAt, _, err := s.GetSyncState("items")
	if err != nil {
		t.Fatalf("GetSyncState after SaveSyncCursor error: %v", err)
	}
	if cursor != "cursor-next" {
		t.Fatalf("GetSyncState cursor = %q, want cursor-next", cursor)
	}
	if cursorSyncedAt.IsZero() {
		t.Fatalf("GetSyncState after SaveSyncCursor returned zero time")
	}
}
`), 0o644))

	runGoCommand(t, outputDir, "test", "./internal/store", "-run", "TestSyncedAtIsSQLiteStrftimeParseable", "-count=1")
}
