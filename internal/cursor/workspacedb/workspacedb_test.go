package workspacedb

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	root := t.TempDir()
	hash := "abc123"
	dir := filepath.Join(root, hash)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ws := filepath.Join(dir, "workspace.json")
	if err := os.WriteFile(ws, []byte(`{"folder":"file:///tmp/my-project"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got[hash] != "/tmp/my-project" {
		t.Fatalf("got %q", got[hash])
	}
}
