package workspacedb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Map maps workspaceStorage hash → filesystem project path.
type Map map[string]string

// Load walks workspaceStorage and reads workspace.json files.
func Load(workspaceStorageDir string) (Map, error) {
	out := make(Map)
	entries, err := os.ReadDir(workspaceStorageDir)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}

	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		hash := ent.Name()
		wsPath := filepath.Join(workspaceStorageDir, hash, "workspace.json")
		data, err := os.ReadFile(wsPath)
		if err != nil {
			continue
		}
		var doc struct {
			Folder string `json:"folder"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			continue
		}
		path := folderToPath(doc.Folder)
		if path != "" {
			out[hash] = path
		}
	}
	return out, nil
}

func folderToPath(folder string) string {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return ""
	}
	// VS Code URI: file:///Users/me/proj
	if strings.HasPrefix(folder, "file://") {
		p := strings.TrimPrefix(folder, "file://")
		// file:///path on unix
		if strings.HasPrefix(p, "/") {
			return p
		}
		return p
	}
	return folder
}
