package sqliteutil

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/cursor-stat/cursor-stat/internal/store"
	_ "modernc.org/sqlite"
)

// OpenReadOnlySnapshot copies dbPath to a temp dir and opens it read-only.
// The caller must call cleanup when done.
func OpenReadOnlySnapshot(dbPath string) (db *sql.DB, cleanup func(), err error) {
	tmp, err := os.MkdirTemp("", "cursor-stat-sqlite-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup = func() { _ = os.RemoveAll(tmp) }

	copyPath, err := store.CopySQLiteSnapshot(dbPath, tmp)
	if err != nil {
		cleanup()
		return nil, nil, err
	}

	dsn := fmt.Sprintf("file:%s?mode=ro", copyPath)
	db, err = sql.Open("sqlite", dsn)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	if _, err := db.Exec(`PRAGMA query_only = ON`); err != nil {
		_ = db.Close()
		cleanup()
		return nil, nil, err
	}
	return db, cleanup, nil
}

// TableExists reports whether table name exists in the database.
func TableExists(db *sql.DB, table string) (bool, error) {
	var n int
	err := db.QueryRow(
		`SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name=?`,
		table,
	).Scan(&n)
	return n > 0, err
}
