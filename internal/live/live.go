package live

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/cursor-stat/cursor-stat/internal/cursor/workspacedb"
)

var cursorProcessNames = map[string][]string{
	"darwin":  {"Cursor"},
	"linux":   {"cursor", "Cursor"},
	"windows": {"Cursor.exe"},
}

// Snapshot detects whether Cursor is running locally.
func Snapshot() (running bool, pid int) {
	names, ok := cursorProcessNames[runtime.GOOS]
	if !ok {
		return false, 0
	}
	for _, name := range names {
		if p, ok := findPID(name); ok {
			return true, p
		}
	}
	return false, 0
}

func findPID(processName string) (int, bool) {
	switch runtime.GOOS {
	case "darwin", "linux":
		return pgrep(processName)
	case "windows":
		return tasklistPID(processName)
	default:
		return 0, false
	}
}

func pgrep(name string) (int, bool) {
	out, err := exec.Command("pgrep", "-x", name).Output()
	if err != nil {
		return 0, false
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return 0, false
	}
	first := strings.Split(line, "\n")[0]
	pid, err := strconv.Atoi(first)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func tasklistPID(name string) (int, bool) {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+name, "/FO", "CSV", "/NH").Output()
	if err != nil {
		return 0, false
	}
	line := strings.TrimSpace(string(out))
	if line == "" || strings.Contains(line, "No tasks") {
		return 0, false
	}
	// "Cursor.exe","1234",...
	parts := strings.Split(line, ",")
	if len(parts) < 2 {
		return 0, false
	}
	pidStr := strings.Trim(parts[1], `" `)
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

// ActiveWorkspace returns the project path for the most recently touched workspace.json.
func ActiveWorkspace(workspaceStorageDir string) string {
	wsMap, err := workspacedb.Load(workspaceStorageDir)
	if err != nil {
		return ""
	}
	var bestPath string
	var bestTime int64
	for hash := range wsMap {
		wsJSON := filepath.Join(workspaceStorageDir, hash, "workspace.json")
		info, err := os.Stat(wsJSON)
		if err != nil {
			continue
		}
		if info.ModTime().UnixNano() > bestTime {
			bestTime = info.ModTime().UnixNano()
			bestPath = wsMap[hash]
		}
	}
	return bestPath
}
