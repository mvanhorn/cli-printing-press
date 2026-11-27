package lake

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mvanhorn/cli-printing-press/v4/internal/cartridge"
	_ "modernc.org/sqlite"
)

// Item is one normalized row in the unified local Personal Life Lake (~/.pp/lake.db).
type Item struct {
	ID         string    `json:"id"`
	Source     string    `json:"source"`
	Command    string    `json:"command"`
	EntityType string    `json:"entity_type"`
	Title      string    `json:"title"`
	Summary    string    `json:"summary,omitempty"`
	URL        string    `json:"url,omitempty"`
	Timestamp  string    `json:"timestamp,omitempty"`
	RawJSON    string    `json:"raw_json,omitempty"`
	SyncedAt   time.Time `json:"synced_at"`
}

// SyncReport summarizes one cartridge sync into the Life Lake.
type SyncReport struct {
	Source     string `json:"source"`
	Commands   int    `json:"commands"`
	RowsSynced int    `json:"rows_synced"`
	DurationMS int64  `json:"duration_ms"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

// SourceStats reports row count and freshness for one source in the lake.
type SourceStats struct {
	Source   string `json:"source"`
	Rows     int    `json:"rows"`
	LastSync string `json:"last_sync"`
}

// Stats summarizes the entire local Personal Life Lake.
type Stats struct {
	Path      string        `json:"path"`
	TotalRows int           `json:"total_rows"`
	SizeBytes int64         `json:"size_bytes"`
	Sources   []SourceStats `json:"sources"`
}

// Lake wraps the unified SQLite + FTS5 Personal Life Lake.
type Lake struct {
	path string
	db   *sql.DB
}

// DefaultPath returns ~/.pp/lake.db.
func DefaultPath() (string, error) {
	home, err := cartridge.HomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "lake.db"), nil
}

// Open opens and initializes the unified SQLite + FTS5 Life Lake.
func Open(path string) (*Lake, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("creating lake dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening lake db: %w", err)
	}
	const schema = `
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;

CREATE TABLE IF NOT EXISTS lake_items (
	id TEXT PRIMARY KEY,
	source TEXT NOT NULL,
	command TEXT NOT NULL,
	entity_type TEXT NOT NULL DEFAULT 'item',
	title TEXT NOT NULL DEFAULT '',
	summary TEXT NOT NULL DEFAULT '',
	url TEXT NOT NULL DEFAULT '',
	timestamp TEXT NOT NULL DEFAULT '',
	raw_json TEXT NOT NULL DEFAULT '{}',
	synced_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_lake_items_source ON lake_items (source, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_lake_items_entity ON lake_items (entity_type, timestamp DESC);

CREATE VIRTUAL TABLE IF NOT EXISTS lake_items_fts USING fts5(
	title,
	summary,
	source,
	entity_type,
	url,
	content='lake_items',
	content_rowid='rowid'
);

CREATE TRIGGER IF NOT EXISTS lake_items_ai AFTER INSERT ON lake_items BEGIN
	INSERT INTO lake_items_fts(rowid, title, summary, source, entity_type, url)
	VALUES (new.rowid, new.title, new.summary, new.source, new.entity_type, new.url);
END;

CREATE TRIGGER IF NOT EXISTS lake_items_ad AFTER DELETE ON lake_items BEGIN
	INSERT INTO lake_items_fts(lake_items_fts, rowid, title, summary, source, entity_type, url)
	VALUES ('delete', old.rowid, old.title, old.summary, old.source, old.entity_type, old.url);
END;

CREATE TRIGGER IF NOT EXISTS lake_items_au AFTER UPDATE ON lake_items BEGIN
	INSERT INTO lake_items_fts(lake_items_fts, rowid, title, summary, source, entity_type, url)
	VALUES ('delete', old.rowid, old.title, old.summary, old.source, old.entity_type, old.url);
	INSERT INTO lake_items_fts(rowid, title, summary, source, entity_type, url)
	VALUES (new.rowid, new.title, new.summary, new.source, new.entity_type, new.url);
END;

CREATE TABLE IF NOT EXISTS lake_sync_runs (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	source TEXT NOT NULL,
	synced_at TEXT NOT NULL,
	row_count INTEGER NOT NULL DEFAULT 0,
	duration_ms INTEGER NOT NULL DEFAULT 0,
	status TEXT NOT NULL
);
`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initializing lake schema: %w", err)
	}
	return &Lake{path: path, db: db}, nil
}

// Close closes the lake database connection.
func (l *Lake) Close() error {
	if l == nil || l.db == nil {
		return nil
	}
	return l.db.Close()
}

// SyncCartridge executes every sync_default command on cart (or the first
// read-only command if none is flagged) and upserts projected rows into the lake.
func (l *Lake) SyncCartridge(ctx context.Context, cart *cartridge.Cartridge, opts cartridge.RunOptions) (*SyncReport, error) {
	start := time.Now()
	cmdNames := selectSyncCommands(cart)
	if len(cmdNames) == 0 {
		return &SyncReport{Source: cart.Name, Status: "skipped", Error: "no read-only sync commands"}, nil
	}

	totalRows := 0
	for _, cmdName := range cmdNames {
		res, err := cartridge.Execute(ctx, cart, cmdName, opts)
		if err != nil {
			rep := &SyncReport{
				Source:     cart.Name,
				Commands:   len(cmdNames),
				RowsSynced: totalRows,
				DurationMS: time.Since(start).Milliseconds(),
				Status:     "error",
				Error:      fmt.Sprintf("%s: %v", cmdName, err),
			}
			l.recordSyncRun(rep)
			return rep, err
		}
		items := projectRows(cart.Name, cmdName, cart.Commands[cmdName].LakeProjection, res.Data)
		if err := l.UpsertItems(ctx, items); err != nil {
			return nil, err
		}
		totalRows += len(items)
	}

	rep := &SyncReport{
		Source:     cart.Name,
		Commands:   len(cmdNames),
		RowsSynced: totalRows,
		DurationMS: time.Since(start).Milliseconds(),
		Status:     "ok",
	}
	l.recordSyncRun(rep)
	return rep, nil
}

// UpsertItems writes normalized items into lake_items inside one transaction.
func (l *Lake) UpsertItems(ctx context.Context, items []Item) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO lake_items (id, source, command, entity_type, title, summary, url, timestamp, raw_json, synced_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	entity_type = excluded.entity_type,
	title = excluded.title,
	summary = excluded.summary,
	url = excluded.url,
	timestamp = excluded.timestamp,
	raw_json = excluded.raw_json,
	synced_at = excluded.synced_at
`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, item := range items {
		ts := now
		if !item.SyncedAt.IsZero() {
			ts = item.SyncedAt.UTC().Format(time.RFC3339)
		}
		if _, err := stmt.ExecContext(ctx,
			item.ID, item.Source, item.Command, item.EntityType,
			item.Title, item.Summary, item.URL, item.Timestamp, item.RawJSON, ts,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Search performs ranked FTS5 full-text search across every synced source.
func (l *Lake) Search(ctx context.Context, query, source, entityType string, limit int) ([]Item, error) {
	if limit <= 0 {
		limit = 20
	}
	q := strings.TrimSpace(query)
	var (
		rows *sql.Rows
		err  error
	)
	if q == "" {
		sqlText := `SELECT id, source, command, entity_type, title, summary, url, timestamp, synced_at FROM lake_items WHERE 1=1`
		var args []any
		if source != "" {
			sqlText += ` AND source = ?`
			args = append(args, source)
		}
		if entityType != "" {
			sqlText += ` AND entity_type = ?`
			args = append(args, entityType)
		}
		sqlText += ` ORDER BY synced_at DESC LIMIT ?`
		args = append(args, limit)
		rows, err = l.db.QueryContext(ctx, sqlText, args...)
	} else {
		ftsQuery := sanitizeFTSQuery(q)
		sqlText := `
SELECT i.id, i.source, i.command, i.entity_type, i.title, i.summary, i.url, i.timestamp, i.synced_at
FROM lake_items_fts f
JOIN lake_items i ON i.rowid = f.rowid
WHERE lake_items_fts MATCH ?`
		args := []any{ftsQuery}
		if source != "" {
			sqlText += ` AND i.source = ?`
			args = append(args, source)
		}
		if entityType != "" {
			sqlText += ` AND i.entity_type = ?`
			args = append(args, entityType)
		}
		sqlText += ` ORDER BY f.rank LIMIT ?`
		args = append(args, limit)
		rows, err = l.db.QueryContext(ctx, sqlText, args...)
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var items []Item
	for rows.Next() {
		var it Item
		var synced string
		if err := rows.Scan(&it.ID, &it.Source, &it.Command, &it.EntityType, &it.Title, &it.Summary, &it.URL, &it.Timestamp, &synced); err != nil {
			return nil, err
		}
		if parsed, err := time.Parse(time.RFC3339, synced); err == nil {
			it.SyncedAt = parsed
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

const (
	// DefaultSQLTimeout bounds read-only Life Lake SQL queries so recursive
	// CTEs or heavy joins cannot stall the CLI or single-threaded stdio MCP.
	DefaultSQLTimeout = 5 * time.Second
	// DefaultSQLMaxRows caps rows materialized in memory per lake SQL query.
	DefaultSQLMaxRows = 500
	// DefaultSQLMaxBytes caps total serialized JSON bytes per lake SQL query.
	DefaultSQLMaxBytes = 512 * 1024
)

// QueryReadOnly runs a strictly read-only, single-statement SQL query against
// the Life Lake with bounded execution time, row count, and byte budget.
func (l *Lake) QueryReadOnly(ctx context.Context, query string) ([]map[string]any, error) {
	if err := cartridge.ValidateReadOnlySQL(query); err != nil {
		return nil, fmt.Errorf("lake sql rejected: %w", err)
	}
	runCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, DefaultSQLTimeout)
		defer cancel()
	}
	roDB, err := sql.Open("sqlite", l.path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer func() { _ = roDB.Close() }()
	if _, err := roDB.ExecContext(runCtx, "PRAGMA query_only = ON"); err != nil {
		return nil, err
	}
	rows, err := roDB.QueryContext(runCtx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0)
	totalBytes := 0
	for rows.Next() {
		if len(out) >= DefaultSQLMaxRows {
			return nil, fmt.Errorf("lake sql query exceeded max row budget (%d rows); add a LIMIT or WHERE clause", DefaultSQLMaxRows)
		}
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range cols {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				row[c] = string(b)
			} else {
				row[c] = vals[i]
			}
		}
		if encoded, mErr := json.Marshal(row); mErr == nil {
			if totalBytes+len(encoded) > DefaultSQLMaxBytes && len(out) > 0 {
				return nil, fmt.Errorf("lake sql query exceeded max output budget (%d KiB); narrow columns or add LIMIT", DefaultSQLMaxBytes/1024)
			}
			totalBytes += len(encoded)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// Stats returns row counts and freshness across all sources in the Life Lake.
func (l *Lake) Stats(ctx context.Context) (*Stats, error) {
	st := &Stats{Path: l.path}
	if info, err := os.Stat(l.path); err == nil {
		st.SizeBytes = info.Size()
	}
	rows, err := l.db.QueryContext(ctx, `
SELECT source, COUNT(*) AS cnt, MAX(synced_at) AS last_sync
FROM lake_items
GROUP BY source
ORDER BY cnt DESC, source ASC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var s SourceStats
		if err := rows.Scan(&s.Source, &s.Rows, &s.LastSync); err != nil {
			return nil, err
		}
		st.TotalRows += s.Rows
		st.Sources = append(st.Sources, s)
	}
	return st, rows.Err()
}

func (l *Lake) recordSyncRun(rep *SyncReport) {
	if l == nil || l.db == nil || rep == nil {
		return
	}
	_, _ = l.db.Exec(
		`INSERT INTO lake_sync_runs (source, synced_at, row_count, duration_ms, status) VALUES (?, ?, ?, ?, ?)`,
		rep.Source, time.Now().UTC().Format(time.RFC3339), rep.RowsSynced, rep.DurationMS, rep.Status,
	)
}

func selectSyncCommands(cart *cartridge.Cartridge) []string {
	var flagged []string
	var fallback []string
	for name, cmd := range cart.Commands {
		if cart.IsMutating(name) {
			continue
		}
		hasRequired := false
		for _, p := range cmd.Params {
			if p.Required && p.Default == "" {
				hasRequired = true
				break
			}
		}
		if hasRequired {
			continue
		}
		if cmd.SyncDefault {
			flagged = append(flagged, name)
		} else {
			fallback = append(fallback, name)
		}
	}
	sort.Strings(flagged)
	if len(flagged) > 0 {
		return flagged
	}
	sort.Strings(fallback)
	if len(fallback) > 0 {
		return []string{fallback[0]}
	}
	return nil
}

func projectRows(source, command string, proj *cartridge.LakeProjection, data any) []Item {
	var rawRows []any
	switch v := data.(type) {
	case []any:
		rawRows = v
	case nil:
		return nil
	default:
		rawRows = []any{v}
	}

	now := time.Now().UTC()
	out := make([]Item, 0, len(rawRows))
	for idx, row := range rawRows {
		rawBytes, _ := json.Marshal(row)
		entity := "item"
		key := ""
		title := ""
		summary := ""
		itemURL := ""
		ts := ""

		if proj != nil {
			if proj.Entity != "" {
				entity = proj.Entity
			}
			key = cartridge.StringifyField(row, proj.Key)
			title = cartridge.StringifyField(row, proj.Title)
			summary = cartridge.StringifyField(row, proj.Summary)
			itemURL = cartridge.StringifyField(row, proj.URL)
			ts = cartridge.StringifyField(row, proj.Timestamp)
		} else {
			key = firstNonEmpty(row, "id", "objectID", "slug", "name", "url", "path")
			title = firstNonEmpty(row, "title", "name", "headline", "full_name", "displayName", "shortName")
			summary = firstNonEmpty(row, "summary", "description", "excerpt", "status", "author")
			itemURL = firstNonEmpty(row, "url", "html_url", "href", "path")
			ts = firstNonEmpty(row, "timestamp", "created_at", "updated_at", "published", "date", "modified")
		}

		if key == "" {
			sum := sha256.Sum256(fmt.Appendf(rawBytes, ":%d", idx))
			key = hex.EncodeToString(sum[:8])
		}
		if title == "" {
			title = key
		}
		out = append(out, Item{
			ID:         fmt.Sprintf("%s:%s:%s", source, command, key),
			Source:     source,
			Command:    command,
			EntityType: entity,
			Title:      title,
			Summary:    summary,
			URL:        itemURL,
			Timestamp:  ts,
			RawJSON:    string(rawBytes),
			SyncedAt:   now,
		})
	}
	return out
}

func firstNonEmpty(row any, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(cartridge.StringifyField(row, k)); v != "" {
			return v
		}
	}
	return ""
}

func sanitizeFTSQuery(q string) string {
	tokens := strings.Fields(q)
	if len(tokens) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(tokens))
	for _, t := range tokens {
		clean := strings.ReplaceAll(t, `"`, "")
		if clean != "" {
			quoted = append(quoted, `"`+clean+`"`)
		}
	}
	return strings.Join(quoted, " ")
}
