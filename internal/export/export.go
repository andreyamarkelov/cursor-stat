package exportdata

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/cursor-stat/cursor-stat/internal/store"
)

// WriteCSV exports daily rollups to w.
func WriteCSV(db *store.DB, days int, w io.Writer) error {
	rollups, err := db.DailyRollups(days)
	if err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"date", "sessions_started", "messages", "tool_calls", "tool_failures"}); err != nil {
		return err
	}
	for _, r := range rollups {
		if err := cw.Write([]string{
			r.Date,
			fmt.Sprintf("%d", r.SessionsStarted),
			fmt.Sprintf("%d", r.AssistantMsgs),
			fmt.Sprintf("%d", r.ToolCalls),
			fmt.Sprintf("%d", r.ToolFailures),
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// WriteJSON exports composers + rollups.
func WriteJSON(db *store.DB, days int, w io.Writer) error {
	composers, err := db.ListComposers(500)
	if err != nil {
		return err
	}
	rollups, err := db.DailyRollups(days)
	if err != nil {
		return err
	}
	last, _ := db.LastIngestTime()
	payload := map[string]any{
		"exported_at": time.Now().UTC(),
		"composers":   composers,
		"rollups":     rollups,
		"last_ingest": last,
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// WriteCSVFile writes CSV export to path.
func WriteCSVFile(db *store.DB, days int, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return WriteCSV(db, days, f)
}
