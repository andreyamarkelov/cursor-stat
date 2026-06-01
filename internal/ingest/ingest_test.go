package ingest

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cursor-stat/cursor-stat/internal/store"
	_ "modernc.org/sqlite"
)

func TestRunIngest(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("CURSOR_STAT_HOME", filepath.Join(root, ".cursor-stat"))

	user := filepath.Join(root, "User")
	globalDir := filepath.Join(user, "globalStorage")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	createGlobalDB(t, filepath.Join(globalDir, "state.vscdb"))

	transDir := filepath.Join(root, ".cursor", "projects", "demo", "agent-transcripts")
	if err := os.MkdirAll(transDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(transDir, "s.jsonl"),
		[]byte(`{"tool_name":"Grep","timestamp":"2026-05-31T10:00:00Z"}`+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CURSOR_USER_DATA", user)

	db, err := store.OpenDefault()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	res, err := Run(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if res.ComposersUpserted != 1 {
		t.Fatalf("composers %d", res.ComposersUpserted)
	}
	if res.EventsInserted < 2 {
		t.Fatalf("events inserted %d", res.EventsInserted)
	}

	n, err := db.ComposerCount()
	if err != nil || n != 1 {
		t.Fatalf("composer count %d err=%v", n, err)
	}

	rollups, err := db.DailyRollups(7)
	if err != nil {
		t.Fatal(err)
	}
	if len(rollups) == 0 {
		t.Fatal("expected rollups")
	}

	res2, err := Run(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	if res2.SourcesUpdated != 0 {
		t.Fatalf("expected no source updates, got %d", res2.SourcesUpdated)
	}
}

func createGlobalDB(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value BLOB)`); err != nil {
		t.Fatal(err)
	}
	payload := `{"name":"ingest test","createdAt":1710000000000,"lastUpdatedAt":1710003600000,"fullConversationHeadersOnly":[{}]}`
	if _, err := db.Exec(`INSERT INTO cursorDiskKV(key,value) VALUES(?,?)`, "composerData:x1", payload); err != nil {
		t.Fatal(err)
	}
}

func TestEventDedup(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CURSOR_STAT_HOME", filepath.Join(root, ".cursor-stat"))
	db, err := store.OpenDefault()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ok := true
	inserted, err := db.InsertEvent(mustTime("2026-05-31T10:00:00Z"), "s1", "", "tool", "Read", "test", &ok)
	if err != nil || !inserted {
		t.Fatalf("first insert inserted=%v err=%v", inserted, err)
	}
	inserted2, err := db.InsertEvent(mustTime("2026-05-31T10:00:00Z"), "s1", "", "tool", "Read", "test", &ok)
	if err != nil || inserted2 {
		t.Fatalf("dup insert inserted=%v err=%v", inserted2, err)
	}
}

func mustTime(s string) (t time.Time) {
	t, _ = time.Parse(time.RFC3339, s)
	return t
}
