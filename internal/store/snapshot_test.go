package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopySQLiteSnapshot(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "state.vscdb")
	if err := os.WriteFile(base, []byte("main-db"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base+"-wal", []byte("wal"), 0o600); err != nil {
		t.Fatal(err)
	}

	destDir := filepath.Join(dir, "copy")
	got, err := CopySQLiteSnapshot(base, destDir)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(destDir, "state.vscdb") {
		t.Fatalf("unexpected dest path: %s", got)
	}
	for _, name := range []string{"state.vscdb", "state.vscdb-wal"} {
		p := filepath.Join(destDir, name)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}

func TestCopySQLiteSnapshotMissingMain(t *testing.T) {
	dir := t.TempDir()
	_, err := CopySQLiteSnapshot(filepath.Join(dir, "nope.vscdb"), filepath.Join(dir, "copy"))
	if err == nil {
		t.Fatal("expected error")
	}
}
