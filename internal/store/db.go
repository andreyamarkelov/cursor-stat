package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/cursor-stat/cursor-stat/internal/cursor"
	"github.com/cursor-stat/cursor-stat/internal/paths"
	_ "modernc.org/sqlite"
)

const schemaVersion = 1

// DB wraps our stats cache (~/.cursor-stat/stats.db).
type DB struct {
	sql *sql.DB
}

// OpenDefault opens or creates ~/.cursor-stat/stats.db.
func OpenDefault() (*DB, error) {
	dir, err := paths.StatDataDir()
	if err != nil {
		return nil, err
	}
	return Open(filepath.Join(dir, "stats.db"))
}

// Open opens a stats database at path.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db := &DB{sql: sqlDB}
	if err := db.Migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}

// Close closes the database.
func (db *DB) Close() error {
	if db == nil || db.sql == nil {
		return nil
	}
	return db.sql.Close()
}

// Migrate applies schema migrations.
func (db *DB) Migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS ingest_sources (
			id TEXT PRIMARY KEY,
			path TEXT NOT NULL,
			file_size INTEGER,
			file_mtime INTEGER NOT NULL,
			content_hash TEXT,
			ingested_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS composers (
			id TEXT PRIMARY KEY,
			workspace_id TEXT,
			workspace_path TEXT,
			title TEXT,
			created_at INTEGER,
			updated_at INTEGER,
			message_count INTEGER DEFAULT 0,
			source TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			at INTEGER NOT NULL,
			session_id TEXT,
			workspace TEXT,
			kind TEXT NOT NULL,
			tool_name TEXT,
			success INTEGER,
			source TEXT NOT NULL,
			meta_json TEXT
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_events_dedup
		 ON events(source, session_id, at, kind, COALESCE(tool_name, ''))`,
		`CREATE INDEX IF NOT EXISTS idx_events_at ON events(at)`,
		`CREATE TABLE IF NOT EXISTS daily_rollups (
			date TEXT PRIMARY KEY,
			sessions_started INTEGER DEFAULT 0,
			user_messages INTEGER DEFAULT 0,
			assistant_messages INTEGER DEFAULT 0,
			tool_calls INTEGER DEFAULT 0,
			tool_failures INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS meta (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	}
	for _, s := range stmts {
		if _, err := db.sql.Exec(s); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	var v int
	err := db.sql.QueryRow(`SELECT version FROM schema_version LIMIT 1`).Scan(&v)
	if err == sql.ErrNoRows {
		_, err = db.sql.Exec(`INSERT INTO schema_version(version) VALUES (?)`, schemaVersion)
	}
	return err
}

// NeedsIngest returns true when source file changed since last ingest (size + mtime).
func (db *DB) NeedsIngest(sourceID, path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	var prevSize int64
	var prevMtime int64
	err = db.sql.QueryRow(
		`SELECT file_size, file_mtime FROM ingest_sources WHERE id = ?`, sourceID,
	).Scan(&prevSize, &prevMtime)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	return prevSize != info.Size() || prevMtime != info.ModTime().UnixNano(), nil
}

// MarkIngested records a successful source ingest.
func (db *DB) MarkIngested(sourceID, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	hash := ""
	if info.Size() <= 10*1024*1024 {
		hash, _ = fileHash(path)
	}
	_, err = db.sql.Exec(`
		INSERT INTO ingest_sources(id, path, file_size, file_mtime, content_hash, ingested_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			path=excluded.path,
			file_size=excluded.file_size,
			file_mtime=excluded.file_mtime,
			content_hash=excluded.content_hash,
			ingested_at=excluded.ingested_at
	`, sourceID, path, info.Size(), info.ModTime().UnixNano(), hash, time.Now().UnixNano())
	return err
}

// SetMeta stores a key/value meta string.
func (db *DB) SetMeta(key, value string) error {
	_, err := db.sql.Exec(`
		INSERT INTO meta(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value
	`, key, value)
	return err
}

// GetMeta reads meta value.
func (db *DB) GetMeta(key string) (string, error) {
	var v string
	err := db.sql.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	return v, err
}

// UpsertComposer inserts or updates composer metadata.
func (db *DB) UpsertComposer(c cursor.ComposerMeta) error {
	_, err := db.sql.Exec(`
		INSERT INTO composers(id, workspace_id, workspace_path, title, created_at, updated_at, message_count, source)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			workspace_id=excluded.workspace_id,
			workspace_path=excluded.workspace_path,
			title=excluded.title,
			created_at=excluded.created_at,
			updated_at=excluded.updated_at,
			message_count=excluded.message_count,
			source=excluded.source
	`, c.ID, c.WorkspaceID, c.WorkspacePath, c.Title,
		timeToUnix(c.CreatedAt), timeToUnix(c.UpdatedAt), c.MessageCount, c.Source)
	return err
}

// InsertEvent inserts an event if not duplicate.
func (db *DB) InsertEvent(at time.Time, sessionID, workspace, kind, toolName, source string, success *bool) (bool, error) {
	var succ sql.NullInt64
	if success != nil {
		if *success {
			succ = sql.NullInt64{Int64: 1, Valid: true}
		} else {
			succ = sql.NullInt64{Int64: 0, Valid: true}
		}
	}
	res, err := db.sql.Exec(`
		INSERT OR IGNORE INTO events(at, session_id, workspace, kind, tool_name, success, source)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, at.UnixNano(), sessionID, workspace, kind, toolName, succ, source)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RebuildDailyRollups recomputes daily_rollups from events + composers.
func (db *DB) RebuildDailyRollups() error {
	if _, err := db.sql.Exec(`DELETE FROM daily_rollups`); err != nil {
		return err
	}

	// Tool events by day
	if _, err := db.sql.Exec(`
		INSERT INTO daily_rollups(date, tool_calls, tool_failures)
		SELECT strftime('%Y-%m-%d', at / 1000000000, 'unixepoch') AS d,
		       COUNT(*),
		       SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END)
		FROM events WHERE kind = 'tool'
		GROUP BY d
		ON CONFLICT(date) DO UPDATE SET
			tool_calls=excluded.tool_calls,
			tool_failures=excluded.tool_failures
	`); err != nil {
		return err
	}

	// Session starts from composers created_at
	if _, err := db.sql.Exec(`
		INSERT INTO daily_rollups(date, sessions_started)
		SELECT strftime('%Y-%m-%d', created_at / 1000000000, 'unixepoch') AS d,
		       COUNT(*)
		FROM composers WHERE created_at > 0
		GROUP BY d
		ON CONFLICT(date) DO UPDATE SET
			sessions_started=excluded.sessions_started
	`); err != nil {
		return err
	}

	// Message counts as assistant_msgs proxy
	if _, err := db.sql.Exec(`
		INSERT INTO daily_rollups(date, assistant_messages)
		SELECT strftime('%Y-%m-%d', updated_at / 1000000000, 'unixepoch') AS d,
		       SUM(message_count)
		FROM composers WHERE updated_at > 0
		GROUP BY d
		ON CONFLICT(date) DO UPDATE SET
			assistant_messages=excluded.assistant_messages
	`); err != nil {
		return err
	}
	return nil
}

// ListComposers returns composers sorted by updated_at desc.
func (db *DB) ListComposers(limit int) ([]cursor.ComposerMeta, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := db.sql.Query(`
		SELECT id, workspace_id, workspace_path, title, created_at, updated_at, message_count, source
		FROM composers ORDER BY updated_at DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []cursor.ComposerMeta
	for rows.Next() {
		var c cursor.ComposerMeta
		var created, updated int64
		if err := rows.Scan(&c.ID, &c.WorkspaceID, &c.WorkspacePath, &c.Title,
			&created, &updated, &c.MessageCount, &c.Source); err != nil {
			return nil, err
		}
		c.CreatedAt = unixToTime(created)
		c.UpdatedAt = unixToTime(updated)
		out = append(out, c)
	}
	return out, rows.Err()
}

// DailyRollups returns last n days of rollups ascending by date.
func (db *DB) DailyRollups(days int) ([]cursor.DailyRollup, error) {
	if days <= 0 {
		days = 7
	}
	since := time.Now().UTC().AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	rows, err := db.sql.Query(`
		SELECT date, sessions_started, user_messages, assistant_messages, tool_calls, tool_failures
		FROM daily_rollups WHERE date >= ? ORDER BY date ASC
	`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []cursor.DailyRollup
	for rows.Next() {
		var r cursor.DailyRollup
		if err := rows.Scan(&r.Date, &r.SessionsStarted, &r.UserMessages,
			&r.AssistantMsgs, &r.ToolCalls, &r.ToolFailures); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TodayStats returns stats for current UTC day.
func (db *DB) TodayStats() (cursor.TodayStats, error) {
	today := time.Now().UTC().Format("2006-01-02")
	var r cursor.TodayStats
	err := db.sql.QueryRow(`
		SELECT sessions_started, assistant_messages, tool_calls, tool_failures
		FROM daily_rollups WHERE date = ?
	`, today).Scan(&r.SessionsStarted, &r.Messages, &r.ToolCalls, &r.ToolFailures)
	if err != nil && err != sql.ErrNoRows {
		return r, err
	}
	if err := db.fillTodayModelStats(&r); err != nil {
		return r, err
	}
	return r, nil
}

func (db *DB) fillTodayModelStats(r *cursor.TodayStats) error {
	start := time.Now().UTC().Truncate(24 * time.Hour).UnixNano()
	var manual, auto sql.NullInt64
	err := db.sql.QueryRow(`
		SELECT
			SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END),
			SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END)
		FROM events WHERE kind = 'model_choice' AND at >= ?
	`, start).Scan(&manual, &auto)
	if err != nil {
		return err
	}
	if manual.Valid {
		r.ManualModelPrompts = int(manual.Int64)
	}
	if auto.Valid {
		r.AutoModelPrompts = int(auto.Int64)
	}
	return nil
}

// InsertModelChoice stores a model picker / hook model event.
func (db *DB) InsertModelChoice(ev cursor.ModelChoiceEvent) (bool, error) {
	sid := ev.SessionID
	if sid == "" {
		sid = ev.ConversationID
	}
	if sid == "" {
		sid = ev.GenerationID
	}
	manual := ev.Manual
	return db.InsertEvent(ev.At, sid, "", "model_choice", ev.Model, ev.Source, &manual)
}

// ModelBreakdownAll aggregates cached model choice events.
func (db *DB) ModelBreakdownAll() (cursor.ModelBreakdown, error) {
	out := cursor.ModelBreakdown{ByModel: make(map[string]int)}
	rows, err := db.sql.Query(`
		SELECT tool_name, success FROM events WHERE kind = 'model_choice'
	`)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var manual sql.NullInt64
		if err := rows.Scan(&name, &manual); err != nil {
			return out, err
		}
		out.Total++
		if manual.Valid && manual.Int64 == 1 {
			out.Manual++
		}
		label := cursor.NormalizeModel(name)
		out.ByModel[label]++
	}
	return out, rows.Err()
}

// LastModelChoice returns the most recent model choice event.
func (db *DB) LastModelChoice() (cursor.ModelChoiceEvent, bool, error) {
	var ev cursor.ModelChoiceEvent
	var at int64
	var manual sql.NullInt64
	err := db.sql.QueryRow(`
		SELECT at, session_id, tool_name, success, source
		FROM events WHERE kind = 'model_choice'
		ORDER BY at DESC LIMIT 1
	`).Scan(&at, &ev.SessionID, &ev.Model, &manual, &ev.Source)
	if err == sql.ErrNoRows {
		return ev, false, nil
	}
	if err != nil {
		return ev, false, err
	}
	ev.At = unixToTime(at)
	if manual.Valid {
		ev.Manual = manual.Int64 == 1
	}
	return ev, true, nil
}

// ToolBreakdownSince aggregates tool events since unix nano.
func (db *DB) ToolBreakdownSince(since time.Time) (cursor.ToolBreakdown, error) {
	return db.toolBreakdown(`WHERE kind = 'tool' AND at >= ?`, since.UnixNano())
}

// ToolBreakdownAll aggregates all cached tool events.
func (db *DB) ToolBreakdownAll() (cursor.ToolBreakdown, error) {
	return db.toolBreakdown(`WHERE kind = 'tool'`, nil)
}

func (db *DB) toolBreakdown(where string, arg any) (cursor.ToolBreakdown, error) {
	out := cursor.ToolBreakdown{ByTool: make(map[string]int)}
	query := `SELECT tool_name, success FROM events ` + where
	var rows *sql.Rows
	var err error
	if arg != nil {
		rows, err = db.sql.Query(query, arg)
	} else {
		rows, err = db.sql.Query(query)
	}
	if err != nil {
		return out, err
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var succ sql.NullInt64
		if err := rows.Scan(&name, &succ); err != nil {
			return out, err
		}
		out.Total++
		if succ.Valid && succ.Int64 == 0 {
			out.Failures++
		}
		if name != "" {
			out.ByTool[name]++
		}
	}
	return out, rows.Err()
}

// ToolEventCount returns cached tool event rows.
func (db *DB) ToolEventCount() (int, error) {
	var n int
	err := db.sql.QueryRow(`SELECT COUNT(*) FROM events WHERE kind = 'tool'`).Scan(&n)
	return n, err
}

// DeleteToolEventsBySource removes tool events for a source (before re-ingest).
func (db *DB) DeleteToolEventsBySource(source string) error {
	_, err := db.sql.Exec(`DELETE FROM events WHERE kind = 'tool' AND source = ?`, source)
	return err
}

// ReplaceToolEventsBySource atomically replaces tool rows for one source.
func (db *DB) ReplaceToolEventsBySource(source string, events []cursor.ToolEvent) error {
	tx, err := db.sql.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM events WHERE kind = 'tool' AND source = ?`, source); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO events(at, session_id, workspace, kind, tool_name, success, source)
		VALUES (?, ?, ?, 'tool', ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, ev := range events {
		var succ sql.NullInt64
		if ev.Success {
			succ = sql.NullInt64{Int64: 1, Valid: true}
		} else {
			succ = sql.NullInt64{Int64: 0, Valid: true}
		}
		src := ev.Source
		if src == "" {
			src = source
		}
		if _, err := stmt.Exec(
			ev.At.UnixNano(), ev.SessionID, ev.Workspace, ev.ToolName, succ, src,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// RepairInvalidToolTimestamps fixes tool rows with pre-2020 timestamps (legacy zero time).
func (db *DB) RepairInvalidToolTimestamps() error {
	cutoff := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano()
	now := time.Now().UTC().UnixNano()
	_, err := db.sql.Exec(`UPDATE events SET at = ? WHERE kind = 'tool' AND at < ?`, now, cutoff)
	return err
}

// ComposerCount returns number of cached composers.
func (db *DB) ComposerCount() (int, error) {
	var n int
	err := db.sql.QueryRow(`SELECT COUNT(*) FROM composers`).Scan(&n)
	return n, err
}

// LastIngestTime returns the latest ingest_sources timestamp.
func (db *DB) LastIngestTime() (time.Time, error) {
	var ns int64
	err := db.sql.QueryRow(`SELECT MAX(ingested_at) FROM ingest_sources`).Scan(&ns)
	if err == sql.ErrNoRows || ns == 0 {
		return time.Time{}, nil
	}
	return unixNanoToTime(ns), err
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func timeToUnix(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}

func unixToTime(ns int64) time.Time {
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns).UTC()
}

func unixNanoToTime(ns int64) time.Time {
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}
