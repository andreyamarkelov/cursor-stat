package globaldb

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/cursor-stat/cursor-stat/internal/cursor"
	"github.com/cursor-stat/cursor-stat/internal/cursor/workspacedb"
	_ "modernc.org/sqlite"
)

func TestReadComposersFromDiskKV(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.vscdb")
	createFixtureDB(t, dbPath)

	workspaces := workspacedb.Map{"ws1": "/tmp/proj"}
	got, err := ReadComposers(dbPath, workspaces)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d composers", len(got))
	}
	if got[0].Title != "test session" {
		t.Fatalf("title %q", got[0].Title)
	}
}

func TestReadComposersMergesHeaders(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.vscdb")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value BLOB)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value BLOB)`); err != nil {
		t.Fatal(err)
	}
	headers := `[{"composerId":"abc-123","name":"from headers","lastUpdatedAt":1710003600000}]`
	if _, err := db.Exec(`INSERT INTO ItemTable(key,value) VALUES(?,?)`, "composer.composerHeaders", headers); err != nil {
		t.Fatal(err)
	}
	payload := `{"name":"","fullConversationHeadersOnly":[{},{},{}],"workspaceIdentifier":"ws1"}`
	if _, err := db.Exec(`INSERT INTO cursorDiskKV(key,value) VALUES(?,?)`, "composerData:abc-123", payload); err != nil {
		t.Fatal(err)
	}
	db.Close()

	got, err := ReadComposers(dbPath, workspacedb.Map{"ws1": "/tmp/proj"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d composers", len(got))
	}
	if got[0].Title != "from headers" {
		t.Fatalf("title %q", got[0].Title)
	}
	if got[0].MessageCount != 3 {
		t.Fatalf("msgs %d", got[0].MessageCount)
	}
}

func TestIsUsefulComposerFiltersStubs(t *testing.T) {
	if IsUsefulComposer(cursor.ComposerMeta{ID: "x"}) {
		t.Fatal("empty stub should be filtered")
	}
	if IsUsefulComposer(cursor.ComposerMeta{ID: "x", WorkspacePath: "/tmp/foo"}) {
		t.Fatal("workspace-only stub should not display")
	}
	if !IsUsefulComposer(cursor.ComposerMeta{ID: "x", Title: "hello"}) {
		t.Fatal("titled session should pass")
	}
}

func createFixtureDB(t *testing.T, dbPath string) {
	t.Helper()
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
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dbPath, 0o600); err != nil {
		t.Fatal(err)
	}
}
