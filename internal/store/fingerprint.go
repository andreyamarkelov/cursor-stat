package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DirFingerprint hashes relative paths, sizes, and mtimes under dir (for ingest skip).
func DirFingerprint(dir string) (hash string, latestMtime int64, err error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return "", 0, nil
	} else if err != nil {
		return "", 0, err
	}

	h := sha256.New()
	var entries []string
	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.Contains(path, "agent-transcripts") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		line := fmt.Sprintf("%s|%d|%d\n", rel, info.Size(), info.ModTime().UnixNano())
		entries = append(entries, line)
		if info.ModTime().UnixNano() > latestMtime {
			latestMtime = info.ModTime().UnixNano()
		}
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	sort.Strings(entries)
	for _, line := range entries {
		_, _ = h.Write([]byte(line))
	}
	return hex.EncodeToString(h.Sum(nil)), latestMtime, nil
}

// MarkIngestedFingerprint records ingest for virtual sources (directories).
func (db *DB) MarkIngestedFingerprint(sourceID, path, hash string, mtime int64) error {
	_, err := db.sql.Exec(`
		INSERT INTO ingest_sources(id, path, file_size, file_mtime, content_hash, ingested_at)
		VALUES (?, ?, 0, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			path=excluded.path,
			file_mtime=excluded.file_mtime,
			content_hash=excluded.content_hash,
			ingested_at=excluded.ingested_at
	`, sourceID, path, mtime, hash, time.Now().UnixNano())
	return err
}

// NeedsIngestFingerprint compares stored hash/mtime fingerprint.
func (db *DB) NeedsIngestFingerprint(sourceID, hash string, mtime int64) (bool, error) {
	var prevHash string
	var prevMtime int64
	err := db.sql.QueryRow(
		`SELECT content_hash, file_mtime FROM ingest_sources WHERE id = ?`, sourceID,
	).Scan(&prevHash, &prevMtime)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return true, err
	}
	return prevHash != hash || prevMtime != mtime, nil
}
