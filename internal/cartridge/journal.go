package cartridge

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// JournalEntry records one cartridge execution outcome for drift detection and
// `pp heal` diagnostics.
type JournalEntry struct {
	ID         int64     `json:"id"`
	Cartridge  string    `json:"cartridge"`
	Command    string    `json:"command"`
	Status     string    `json:"status"` // ok, auth_error, rate_limited, drift, error
	HTTPStatus int       `json:"http_status,omitempty"`
	LatencyMS  int64     `json:"latency_ms"`
	RowCount   int       `json:"row_count"`
	ErrorText  string    `json:"error_text,omitempty"`
	RecordedAt time.Time `json:"recorded_at"`
}

// Journal persists invocation telemetry in ~/.pp/journal.sqlite.
type Journal struct {
	db *sql.DB
}

// OpenJournal opens (and initializes) the SQLite invocation journal.
func OpenJournal(path string) (*Journal, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("creating journal dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening journal db: %w", err)
	}
	const schema = `
CREATE TABLE IF NOT EXISTS invocations (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	cartridge TEXT NOT NULL,
	command TEXT NOT NULL,
	status TEXT NOT NULL,
	http_status INTEGER NOT NULL DEFAULT 0,
	latency_ms INTEGER NOT NULL DEFAULT 0,
	row_count INTEGER NOT NULL DEFAULT 0,
	error_text TEXT NOT NULL DEFAULT '',
	recorded_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_invocations_cartridge_cmd ON invocations (cartridge, command, id DESC);
`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initializing journal schema: %w", err)
	}
	return &Journal{db: db}, nil
}

// DefaultJournalPath returns ~/.pp/journal.sqlite.
func DefaultJournalPath() (string, error) {
	base, err := HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "journal.sqlite"), nil
}

// Close closes the underlying SQLite connection.
func (j *Journal) Close() error {
	if j == nil || j.db == nil {
		return nil
	}
	return j.db.Close()
}

// Record inserts one invocation entry.
func (j *Journal) Record(entry JournalEntry) error {
	if j == nil || j.db == nil {
		return nil
	}
	if entry.RecordedAt.IsZero() {
		entry.RecordedAt = time.Now().UTC()
	}
	_, err := j.db.Exec(
		`INSERT INTO invocations (cartridge, command, status, http_status, latency_ms, row_count, error_text, recorded_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.Cartridge,
		entry.Command,
		entry.Status,
		entry.HTTPStatus,
		entry.LatencyMS,
		entry.RowCount,
		entry.ErrorText,
		entry.RecordedAt.Format(time.RFC3339),
	)
	return err
}

// Recent returns the latest N journal entries for a cartridge (and optional command).
func (j *Journal) Recent(cartridgeName, command string, limit int) ([]JournalEntry, error) {
	if j == nil || j.db == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	var (
		rows *sql.Rows
		err  error
	)
	if command != "" {
		rows, err = j.db.Query(
			`SELECT id, cartridge, command, status, http_status, latency_ms, row_count, error_text, recorded_at FROM invocations WHERE cartridge = ? AND command = ? ORDER BY id DESC LIMIT ?`,
			cartridgeName, command, limit,
		)
	} else {
		rows, err = j.db.Query(
			`SELECT id, cartridge, command, status, http_status, latency_ms, row_count, error_text, recorded_at FROM invocations WHERE cartridge = ? ORDER BY id DESC LIMIT ?`,
			cartridgeName, limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []JournalEntry
	for rows.Next() {
		var e JournalEntry
		var ts string
		if err := rows.Scan(&e.ID, &e.Cartridge, &e.Command, &e.Status, &e.HTTPStatus, &e.LatencyMS, &e.RowCount, &e.ErrorText, &ts); err != nil {
			return nil, err
		}
		if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			e.RecordedAt = parsed
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountRows estimates row count for journaling and output metadata.
func CountRows(data any) int {
	switch v := data.(type) {
	case nil:
		return 0
	case []any:
		return len(v)
	case []map[string]any:
		return len(v)
	case json.RawMessage:
		if len(v) == 0 {
			return 0
		}
		return 1
	default:
		return 1
	}
}
