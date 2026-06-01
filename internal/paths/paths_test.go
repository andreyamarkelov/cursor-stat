package paths

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestCursorUserData(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CURSOR_USER_DATA", "")

	got, err := CursorUserData()
	if err != nil {
		t.Fatal(err)
	}

	var want string
	switch runtime.GOOS {
	case "darwin":
		want = filepath.Join(home, "Library", "Application Support", "Cursor", "User")
	case "linux":
		want = filepath.Join(home, ".config", "Cursor", "User")
	case "windows":
		want = filepath.Join(home, "AppData", "Roaming", "Cursor", "User")
	default:
		t.Skip("unsupported GOOS")
	}
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCursorUserDataOverride(t *testing.T) {
	t.Setenv("CURSOR_USER_DATA", "/tmp/custom-cursor-user")
	got, err := CursorUserData()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/custom-cursor-user" {
		t.Fatalf("got %q", got)
	}
}

func TestStatDataDirOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CURSOR_STAT_HOME", dir)
	got, err := StatDataDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Fatalf("got %q want %q", got, dir)
	}
}
