package globaldb

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestReadBubbleModelChoices(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.vscdb")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE cursorDiskKV (key TEXT PRIMARY KEY, value BLOB)`); err != nil {
		t.Fatal(err)
	}
	payload := `{
		"modelInfo":{"modelName":"claude-opus-4-7"},
		"updatedAt":1710003600000
	}`
	if _, err := db.Exec(
		`INSERT INTO cursorDiskKV(key,value) VALUES(?,?)`,
		"bubbleId:composer-1:bubble-1",
		payload,
	); err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(`INSERT INTO cursorDiskKV(key,value) VALUES(?,?)`,
		"bubbleId:composer-1:bubble-2",
		`{"modelInfo":{"modelName":"default"}}`,
	)
	db.Close()

	choices, err := ReadBubbleModelChoices(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(choices) != 1 {
		t.Fatalf("choices %d", len(choices))
	}
	if choices[0].Model != "claude-opus-4-7" || !choices[0].Manual {
		t.Fatalf("choice %+v", choices[0])
	}
	if choices[0].Source != "globaldb:bubble" {
		t.Fatalf("source %q", choices[0].Source)
	}
}

func TestReadBubbleModelChoicesMissing(t *testing.T) {
	choices, err := ReadBubbleModelChoices(filepath.Join(t.TempDir(), "missing.vscdb"))
	if err != nil {
		t.Fatal(err)
	}
	if choices != nil {
		t.Fatalf("expected nil, got %v", choices)
	}
	_ = os.WriteFile(filepath.Join(t.TempDir(), "x"), []byte("x"), 0o600)
}
