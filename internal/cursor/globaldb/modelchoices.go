package globaldb

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/cursor-stat/cursor-stat/internal/cursor"
	"github.com/cursor-stat/cursor-stat/internal/cursor/sqliteutil"
)

// ReadBubbleModelChoices scans bubbleId rows for resolved model names (historical backfill).
func ReadBubbleModelChoices(globalDBPath string) ([]cursor.ModelChoiceEvent, error) {
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

	hasDiskKV, _ := sqliteutil.TableExists(db, "cursorDiskKV")
	if !hasDiskKV {
		return nil, nil
	}

	rows, err := db.Query(`SELECT key, value FROM cursorDiskKV WHERE key LIKE 'bubbleId:%' AND value LIKE '%modelName%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []cursor.ModelChoiceEvent
	for rows.Next() {
		var key string
		var value []byte
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		ev, ok := parseBubbleModelChoice(key, value)
		if ok {
			out = append(out, ev)
		}
	}
	return out, rows.Err()
}

func parseBubbleModelChoice(key string, value []byte) (cursor.ModelChoiceEvent, bool) {
	var doc struct {
		ModelInfo struct {
			ModelName string `json:"modelName"`
		} `json:"modelInfo"`
		UpdatedAt json.RawMessage `json:"updatedAt"`
		CreatedAt json.RawMessage `json:"createdAt"`
	}
	if err := json.Unmarshal(value, &doc); err != nil {
		return cursor.ModelChoiceEvent{}, false
	}
	model := strings.TrimSpace(doc.ModelInfo.ModelName)
	if model == "" || cursor.IsAutoModel(model) {
		return cursor.ModelChoiceEvent{}, false
	}

	parts := strings.Split(key, ":")
	sessionID := key
	if len(parts) >= 3 {
		sessionID = parts[1] + ":" + parts[2]
	}

	at := parseTime(jsonString(doc.UpdatedAt))
	if at.IsZero() {
		at = parseTime(jsonString(doc.CreatedAt))
	}

	return cursor.ModelChoiceEvent{
		At:        at,
		SessionID: sessionID,
		Model:     model,
		Manual:    true,
		Source:    "globaldb:bubble",
	}, true
}
