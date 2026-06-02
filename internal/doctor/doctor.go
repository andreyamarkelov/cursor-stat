package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cursor-stat/cursor-stat/internal/hooks"
	"github.com/cursor-stat/cursor-stat/internal/paths"
	"github.com/cursor-stat/cursor-stat/internal/store"
)

// Check is one diagnostic line.
type Check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Run executes all doctor checks.
func Run() ([]Check, error) {
	var out []Check

	user, err := paths.CursorUserData()
	if err != nil {
		out = append(out, Check{Name: "cursor_user_data", Status: "FAIL", Detail: err.Error()})
	} else if st, err := os.Stat(user); err != nil {
		out = append(out, Check{Name: "cursor_user_data", Status: "FAIL", Detail: err.Error()})
	} else if !st.IsDir() {
		out = append(out, Check{Name: "cursor_user_data", Status: "FAIL", Detail: "not a directory"})
	} else {
		out = append(out, Check{Name: "cursor_user_data", Status: "OK", Detail: user})
	}

	global, err := paths.GlobalStateDB()
	if err != nil {
		out = append(out, Check{Name: "global_state_db", Status: "FAIL", Detail: err.Error()})
	} else if _, err := os.Stat(global); err != nil {
		out = append(out, Check{Name: "global_state_db", Status: "WARN", Detail: "not found — is Cursor installed?"})
	} else {
		tmp, mkErr := os.MkdirTemp("", "cursor-stat-doctor-*")
		if mkErr != nil {
			out = append(out, Check{Name: "global_state_db", Status: "WARN", Detail: "temp dir: " + mkErr.Error()})
		} else {
			_, err := store.CopySQLiteSnapshot(global, tmp)
			_ = os.RemoveAll(tmp)
			if err != nil {
				out = append(out, Check{Name: "global_state_db", Status: "WARN", Detail: "locked or unreadable: " + err.Error()})
			} else {
				out = append(out, Check{Name: "global_state_db", Status: "OK", Detail: global})
			}
		}
	}

	statDir, err := paths.StatDataDir()
	if err != nil {
		out = append(out, Check{Name: "cache_dir", Status: "FAIL", Detail: err.Error()})
	} else {
		out = append(out, Check{Name: "cache_dir", Status: "OK", Detail: statDir})
	}

	db, err := store.OpenDefault()
	if err != nil {
		out = append(out, Check{Name: "stats_db", Status: "FAIL", Detail: err.Error()})
	} else {
		defer db.Close()
		n, _ := db.ComposerCount()
		last, _ := db.LastIngestTime()

		// New: check stats.db size
		var dbSize int64
		if p, err := paths.StatDataDir(); err == nil {
			if st, err := os.Stat(filepath.Join(p, "stats.db")); err == nil {
				dbSize = st.Size()
			}
		}

		detail := fmt.Sprintf("%d composers cached", n)
		if dbSize > 0 {
			detail += fmt.Sprintf(" (%s)", formatSize(dbSize))
		}
		if !last.IsZero() {
			detail += ", last ingest " + last.Format(time.RFC3339)
		} else {
			detail += ", run `cursor-stat ingest`"
		}
		status := "OK"
		if n == 0 {
			status = "WARN"
		}
		out = append(out, Check{Name: "stats_db", Status: status, Detail: detail})
	}

	projects, err := paths.ProjectsDir()
	if err != nil {
		out = append(out, Check{Name: "projects_dir", Status: "FAIL", Detail: err.Error()})
	} else if st, err := os.Stat(projects); err != nil {
		out = append(out, Check{Name: "projects_dir", Status: "WARN", Detail: "not found"})
	} else if !st.IsDir() {
		out = append(out, Check{Name: "projects_dir", Status: "WARN", Detail: "not a directory"})
	} else {
		out = append(out, Check{Name: "projects_dir", Status: "OK", Detail: projects})
	}

	if hooks.Installed() {
		out = append(out, Check{Name: "live_hooks", Status: "OK", Detail: "cursor-stat hook registered"})
	} else {
		out = append(out, Check{Name: "live_hooks", Status: "WARN", Detail: "run `cursor-stat hooks install` for live events"})
	}

	return out, nil
}

func formatSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
