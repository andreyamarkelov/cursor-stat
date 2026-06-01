package aggregate

import (
	"context"
	"time"

	"github.com/cursor-stat/cursor-stat/internal/cursor"
	"github.com/cursor-stat/cursor-stat/internal/cursor/globaldb"
	"github.com/cursor-stat/cursor-stat/internal/live"
	"github.com/cursor-stat/cursor-stat/internal/store"
)

// Dashboard builds the TUI view from cache + live signals.
func Dashboard(ctx context.Context, db *store.DB, events *live.Ring) (cursor.Dashboard, error) {
	out := cursor.Dashboard{GeneratedAt: time.Now().UTC()}

	running, pid := live.Snapshot()
	out.Live.CursorRunning = running
	out.Live.CursorPID = pid

	if events != nil {
		out.LiveEvents = events.List(10)
		if ev, ok := events.LatestTool(); ok {
			out.Live.LastTool = ev.Tool
			out.Live.LastEventAt = ev.At
		}
		if ev, ok := events.LatestModel(); ok {
			out.Live.LastModel = ev.Model
			out.Live.LastModelManual = ev.Manual
			if out.Live.LastEventAt.IsZero() || ev.At.After(out.Live.LastEventAt) {
				out.Live.LastEventAt = ev.At
			}
			if out.Live.LastEventKind == "" {
				out.Live.LastEventKind = ev.Kind
			}
		}
		if ev, ok := events.LatestAny(); ok && out.Live.LastEventKind == "" {
			out.Live.LastEventKind = ev.Kind
			if out.Live.LastEventAt.IsZero() {
				out.Live.LastEventAt = ev.At
			}
		}
	}

	skipToolScan := false
	if db != nil {
		if n, err := db.ToolEventCount(); err == nil && n > 0 {
			skipToolScan = true
		}
	}

	snap, err := Snapshot(ctx, SnapshotOptions{SkipToolScan: skipToolScan})
	if err != nil {
		return out, err
	}
	out.Storage = snap.Storage
	if out.Live.ActiveWorkspace == "" {
		out.Live.ActiveWorkspace = snap.Live.ActiveWorkspace
	}
	if out.Live.LastTool == "" {
		out.Live.LastTool = snap.Live.LastTool
		out.Live.LastEventAt = snap.Live.LastEventAt
	}

	// Sessions tab: always from live snapshot (cache may contain old stubs).
	out.Composers = filterDisplayable(snap.Composers, 50)
	enrichLiveSession(&out.Live, out.Composers, events)

	if db == nil {
		out.Tools = snap.Tools
		return out, nil
	}

	count, err := db.ComposerCount()
	if err != nil {
		return out, err
	}
	out.CacheReady = count > 0

	if out.CacheReady {
		out.History, err = db.DailyRollups(7)
		if err != nil {
			return out, err
		}
		out.Today, err = db.TodayStats()
		if err != nil {
			return out, err
		}
		out.Tools, err = db.ToolBreakdownAll()
		if err != nil {
			return out, err
		}
		out.Models, err = db.ModelBreakdownAll()
		if err != nil {
			return out, err
		}
		if last, ok, err := db.LastModelChoice(); err != nil {
			return out, err
		} else if ok && out.Live.LastModel == "" {
			out.Live.LastModel = last.Model
			out.Live.LastModelManual = last.Manual
			if out.Live.LastEventAt.IsZero() {
				out.Live.LastEventAt = last.At
			}
		}
		out.LastIngestAt, _ = db.LastIngestTime()
	}

	if out.Tools.Total == 0 {
		out.Tools = snap.Tools
	}
	if events != nil {
		live := toolsFromRing(events)
		out.ToolsLive = live.Total
		out.Tools = mergeToolBreakdown(out.Tools, live)
	}

	return out, nil
}

func mergeToolBreakdown(base, live cursor.ToolBreakdown) cursor.ToolBreakdown {
	if live.Total == 0 {
		return base
	}
	out := base
	if out.ByTool == nil {
		out.ByTool = make(map[string]int)
	}
	for name, n := range live.ByTool {
		out.ByTool[name] += n
		out.Total += n
	}
	out.Failures += live.Failures
	return out
}

func toolsFromRing(ring *live.Ring) cursor.ToolBreakdown {
	out := cursor.ToolBreakdown{ByTool: make(map[string]int)}
	for _, ev := range ring.List(128) {
		if ev.Tool == "" {
			continue
		}
		out.Total++
		out.ByTool[ev.Tool]++
	}
	return out
}

// filterDisplayable keeps sessions you can actually act on (title, msgs, or date).
func filterDisplayable(list []cursor.ComposerMeta, limit int) []cursor.ComposerMeta {
	var out []cursor.ComposerMeta
	for _, c := range list {
		if globaldb.IsDisplayableComposer(c) {
			out = append(out, c)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func enrichLiveSession(snap *cursor.LiveSnapshot, composers []cursor.ComposerMeta, ring *live.Ring) {
	if len(composers) == 0 {
		return
	}
	top := composers[0]
	snap.ActiveSession = top.Title
	snap.ActiveSessionMsgs = top.MessageCount
	if snap.ActiveWorkspace == "" {
		snap.ActiveWorkspace = top.WorkspacePath
	}
	if ring != nil {
		if _, ok := ring.LatestAny(); ok {
			return
		}
	}
	if !top.UpdatedAt.IsZero() && time.Since(top.UpdatedAt) < 10*time.Minute {
		if snap.LastEventAt.IsZero() || top.UpdatedAt.After(snap.LastEventAt) {
			snap.LastEventAt = top.UpdatedAt
			snap.LastEventKind = "session_activity"
		}
	}
}
