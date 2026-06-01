package aggregate

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSnapshotMinimal(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "User")
	if err := os.MkdirAll(user, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CURSOR_USER_DATA", user)
	t.Setenv("HOME", root)

	snap, err := Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.GeneratedAt.IsZero() {
		t.Fatal("missing generated_at")
	}
	if len(snap.Sources) == 0 {
		t.Fatal("expected source statuses")
	}
}

func TestSnapshotWithFixtures(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)

	user := filepath.Join(root, "User")
	globalDir := filepath.Join(user, "globalStorage")
	wsRoot := filepath.Join(user, "workspaceStorage")
	if err := os.MkdirAll(filepath.Join(wsRoot, "ws1"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(wsRoot, "ws1", "workspace.json"),
		[]byte(`{"folder":"file:///tmp/demo"}`),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	createComposerDB(t, filepath.Join(globalDir, "state.vscdb"))

	transDir := filepath.Join(root, ".cursor", "projects", "demo", "agent-transcripts")
	if err := os.MkdirAll(transDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(transDir, "sess.jsonl"),
		[]byte(`{"tool_name":"Read","timestamp":"2026-05-31T12:00:00Z"}`+"\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	t.Setenv("CURSOR_USER_DATA", user)

	snap, err := Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Composers) != 1 {
		t.Fatalf("composers %d", len(snap.Composers))
	}
	if snap.Composers[0].Title != "test session" {
		t.Fatalf("title %q", snap.Composers[0].Title)
	}
	if snap.Tools.Total != 1 {
		t.Fatalf("tools total %d", snap.Tools.Total)
	}
	if snap.Storage.TranscriptFiles != 1 {
		t.Fatalf("transcript files %d", snap.Storage.TranscriptFiles)
	}
}

func createComposerDB(t *testing.T, dbPath string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value BLOB)`); err != nil {
		t.Fatal(err)
	}
	payload := `{
		"name": "test session",
		"workspaceIdentifier": "ws1",
		"createdAt": 1710000000000,
		"lastUpdatedAt": 1710003600000,
		"fullConversationHeadersOnly": [{}, {}]
	}`
	if _, err := db.Exec(
		`INSERT INTO cursorDiskKV(key, value) VALUES (?, ?)`,
		"composerData:abc-123",
		payload,
	); err != nil {
		t.Fatal(err)
	}
}
