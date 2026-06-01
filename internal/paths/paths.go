package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// CursorUserData returns the Cursor "User" directory for the current OS.
func CursorUserData() (string, error) {
	if override := os.Getenv("CURSOR_USER_DATA"); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Cursor", "User"), nil
	case "linux":
		return filepath.Join(home, ".config", "Cursor", "User"), nil
	case "windows":
		return filepath.Join(home, "AppData", "Roaming", "Cursor", "User"), nil
	default:
		return "", fmt.Errorf("unsupported GOOS %q", runtime.GOOS)
	}
}

// CursorDotDir returns ~/.cursor (hooks, projects, chats).
func CursorDotDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cursor"), nil
}

// GlobalStateDB returns path to globalStorage/state.vscdb.
func GlobalStateDB() (string, error) {
	user, err := CursorUserData()
	if err != nil {
		return "", err
	}
	return filepath.Join(user, "globalStorage", "state.vscdb"), nil
}

// WorkspaceStorageDir returns workspaceStorage under Cursor User data.
func WorkspaceStorageDir() (string, error) {
	user, err := CursorUserData()
	if err != nil {
		return "", err
	}
	return filepath.Join(user, "workspaceStorage"), nil
}

// ProjectsDir returns ~/.cursor/projects (agent-transcripts).
func ProjectsDir() (string, error) {
	dot, err := CursorDotDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dot, "projects"), nil
}

// StatDataDir returns ~/.cursor-stat (our cache), creating it if needed.
func StatDataDir() (string, error) {
	if override := os.Getenv("CURSOR_STAT_HOME"); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".cursor-stat")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("mkdir stat home: %w", err)
	}
	return dir, nil
}
