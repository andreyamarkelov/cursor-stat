package globaldb

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/cursor-stat/cursor-stat/internal/cursor"
	"github.com/cursor-stat/cursor-stat/internal/cursor/sqliteutil"
	"github.com/cursor-stat/cursor-stat/internal/cursor/workspacedb"
)

// ReadComposers reads composer metadata from global state.vscdb.
// Prefers composer.composerHeaders (Cursor 3.0+ index) and enriches with composerData blobs.
func ReadComposers(globalDBPath string, workspaces workspacedb.Map) ([]cursor.ComposerMeta, error) {
	if _, err := os.Stat(globalDBPath); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	db, cleanup, err := sqliteutil.OpenReadOnlySnapshot(globalDBPath)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	defer db.Close()

	hasItem, _ := sqliteutil.TableExists(db, "ItemTable")
	hasDiskKV, _ := sqliteutil.TableExists(db, "cursorDiskKV")

	var headers []cursor.ComposerMeta
	if hasItem {
		headers, err = readComposerHeaders(db, workspaces)
		if err != nil {
			return nil, err
		}
	}

	byID := map[string]cursor.ComposerMeta{}
	if hasDiskKV {
		disk, err := readFromDiskKV(db, workspaces)
		if err != nil {
			return nil, err
		}
		for _, c := range disk {
			byID[c.ID] = mergeComposer(byID[c.ID], c)
		}
	}
	if hasItem {
		legacy, err := readComposerDataItemTable(db, workspaces)
		if err != nil {
			return nil, err
		}
		for _, c := range legacy {
			byID[c.ID] = mergeComposer(byID[c.ID], c)
		}
	}

	var out []cursor.ComposerMeta
	if len(headers) > 0 {
		seen := map[string]bool{}
		for _, h := range headers {
			merged := mergeComposer(h, byID[h.ID])
			merged.Source = "globaldb:composerHeaders"
			if isUsefulComposer(merged) {
				out = append(out, merged)
			}
			seen[h.ID] = true
		}
		// Include rich composerData entries not listed in headers (older data).
		for id, c := range byID {
			if seen[id] || !isUsefulComposer(c) {
				continue
			}
			out = append(out, c)
		}
	} else {
		for _, c := range byID {
			if isUsefulComposer(c) {
				out = append(out, c)
			}
		}
	}

	sortComposers(out)
	return out, nil
}

func readFromDiskKV(db *sql.DB, workspaces workspacedb.Map) ([]cursor.ComposerMeta, error) {
	rows, err := db.Query(`SELECT key, value FROM cursorDiskKV WHERE key LIKE 'composerData:%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []cursor.ComposerMeta
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		id := strings.TrimPrefix(key, "composerData:")
		meta, ok := parseComposerJSON(id, value, workspaces)
		if !ok {
			continue
		}
		meta.Source = "globaldb:cursorDiskKV"
		out = append(out, meta)
	}
	return out, rows.Err()
}

func readComposerDataItemTable(db *sql.DB, workspaces workspacedb.Map) ([]cursor.ComposerMeta, error) {
	rows, err := db.Query(`SELECT key, value FROM ItemTable WHERE key LIKE 'composerData:%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []cursor.ComposerMeta
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		id := strings.TrimPrefix(key, "composerData:")
		meta, ok := parseComposerJSON(id, value, workspaces)
		if !ok {
			continue
		}
		meta.Source = "globaldb:ItemTable"
		out = append(out, meta)
	}
	return out, rows.Err()
}

func readComposerHeaders(db *sql.DB, workspaces workspacedb.Map) ([]cursor.ComposerMeta, error) {
	var raw []byte
	err := db.QueryRow(
		`SELECT value FROM ItemTable WHERE key = 'composer.composerHeaders'`,
	).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var headers []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &headers); err != nil {
		var wrapped struct {
			Composers []map[string]json.RawMessage `json:"composers"`
		}
		if err2 := json.Unmarshal(raw, &wrapped); err2 != nil {
			return nil, nil
		}
		headers = wrapped.Composers
	}

	var out []cursor.ComposerMeta
	for _, h := range headers {
		meta := parseHeaderEntry(h, workspaces)
		if meta.ID != "" {
			out = append(out, meta)
		}
	}
	return out, nil
}

func parseHeaderEntry(h map[string]json.RawMessage, workspaces workspacedb.Map) cursor.ComposerMeta {
	meta := cursor.ComposerMeta{Source: "globaldb:composerHeaders"}
	if v, ok := h["composerId"]; ok {
		meta.ID = jsonString(v)
	}
	if meta.ID == "" {
		if v, ok := h["id"]; ok {
			meta.ID = jsonString(v)
		}
	}
	if v, ok := h["name"]; ok {
		meta.Title = jsonString(v)
	} else if v, ok := h["subtitle"]; ok {
		meta.Title = jsonString(v)
	}
	if v, ok := h["workspaceIdentifier"]; ok {
		meta.WorkspaceID = jsonString(v)
		meta.WorkspacePath = resolveWorkspacePath(meta.WorkspaceID, v, workspaces)
	}
	if v, ok := h["createdAt"]; ok {
		meta.CreatedAt = parseTime(jsonString(v))
	}
	if v, ok := h["lastUpdatedAt"]; ok {
		meta.UpdatedAt = parseTime(jsonString(v))
	} else if v, ok := h["updatedAt"]; ok {
		meta.UpdatedAt = parseTime(jsonString(v))
	} else if v, ok := h["lastUpdated"]; ok {
		meta.UpdatedAt = parseTime(jsonString(v))
	}
	return meta
}

func parseComposerJSON(id string, value []byte, workspaces workspacedb.Map) (cursor.ComposerMeta, bool) {
	if id == "" {
		return cursor.ComposerMeta{}, false
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(value, &doc); err != nil {
		return cursor.ComposerMeta{}, false
	}

	meta := cursor.ComposerMeta{ID: id}
	if v, ok := doc["name"]; ok {
		meta.Title = jsonString(v)
	} else if v, ok := doc["composerName"]; ok {
		meta.Title = jsonString(v)
	} else if v, ok := doc["subtitle"]; ok {
		meta.Title = jsonString(v)
	}
	if v, ok := doc["workspaceIdentifier"]; ok {
		meta.WorkspaceID = jsonString(v)
		meta.WorkspacePath = resolveWorkspacePath(meta.WorkspaceID, v, workspaces)
	} else if v, ok := doc["workspaceId"]; ok {
		meta.WorkspaceID = jsonString(v)
		meta.WorkspacePath = workspaces[meta.WorkspaceID]
	}

	if v, ok := doc["createdAt"]; ok {
		meta.CreatedAt = parseTime(jsonString(v))
	}
	if v, ok := doc["lastUpdatedAt"]; ok {
		meta.UpdatedAt = parseTime(jsonString(v))
	} else if v, ok := doc["updatedAt"]; ok {
		meta.UpdatedAt = parseTime(jsonString(v))
	}

	if headers, ok := doc["fullConversationHeadersOnly"]; ok {
		var list []json.RawMessage
		if json.Unmarshal(headers, &list) == nil {
			meta.MessageCount = len(list)
		}
	} else if v, ok := doc["conversationMap"]; ok {
		var m map[string]json.RawMessage
		if json.Unmarshal(v, &m) == nil {
			meta.MessageCount = len(m)
		}
	}

	return meta, true
}

func resolveWorkspacePath(id string, raw json.RawMessage, workspaces workspacedb.Map) string {
	if p := workspaces[id]; p != "" {
		return p
	}
	if p := workspacePathFromIdentifier(raw); p != "" {
		return p
	}
	return workspacePathFromRawID(id)
}

func mergeComposer(base, extra cursor.ComposerMeta) cursor.ComposerMeta {
	if base.ID == "" {
		base.ID = extra.ID
	}
	if base.Title == "" {
		base.Title = extra.Title
	}
	if base.WorkspaceID == "" {
		base.WorkspaceID = extra.WorkspaceID
	}
	if base.WorkspacePath == "" {
		base.WorkspacePath = extra.WorkspacePath
	}
	if base.CreatedAt.IsZero() {
		base.CreatedAt = extra.CreatedAt
	}
	if extra.UpdatedAt.After(base.UpdatedAt) {
		base.UpdatedAt = extra.UpdatedAt
	}
	if extra.MessageCount > base.MessageCount {
		base.MessageCount = extra.MessageCount
	}
	if base.Source == "" {
		base.Source = extra.Source
	}
	return base
}

// IsUsefulComposer reports whether a session is worth storing during ingest.
func IsUsefulComposer(c cursor.ComposerMeta) bool {
	return isUsefulComposer(c)
}

// IsDisplayableComposer reports whether a session is worth showing in the UI.
func IsDisplayableComposer(c cursor.ComposerMeta) bool {
	if strings.TrimSpace(c.Title) != "" {
		return true
	}
	if c.MessageCount > 0 {
		return true
	}
	if !c.UpdatedAt.IsZero() {
		return true
	}
	return false
}

func isUsefulComposer(c cursor.ComposerMeta) bool {
	return IsDisplayableComposer(c)
}

func sortComposers(list []cursor.ComposerMeta) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].UpdatedAt.Equal(list[j].UpdatedAt) {
			return list[i].ID < list[j].ID
		}
		return list[i].UpdatedAt.After(list[j].UpdatedAt)
	})
}

func workspacePathFromRawID(id string) string {
	return workspacePathFromIdentifier(json.RawMessage(id))
}

func workspacePathFromIdentifier(raw json.RawMessage) string {
	var obj struct {
		ID  string `json:"id"`
		URI struct {
			FSPath string `json:"fsPath"`
			Path   string `json:"path"`
		} `json:"uri"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return ""
	}
	if obj.URI.FSPath != "" {
		return obj.URI.FSPath
	}
	if obj.URI.Path != "" {
		return obj.URI.Path
	}
	return ""
}

func jsonString(raw json.RawMessage) string {
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String()
	}
	return strings.Trim(string(raw), `"`)
}

func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	var ms int64
	if _, err := fmt.Sscan(s, &ms); err == nil {
		if ms > 1_000_000_000_000 {
			return time.UnixMilli(ms)
		}
		return time.Unix(ms, 0)
	}
	return time.Time{}
}
