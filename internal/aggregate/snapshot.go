package aggregate

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/cursor-stat/cursor-stat/internal/cursor"
	"github.com/cursor-stat/cursor-stat/internal/cursor/globaldb"
	"github.com/cursor-stat/cursor-stat/internal/cursor/transcripts"
	"github.com/cursor-stat/cursor-stat/internal/cursor/workspacedb"
	"github.com/cursor-stat/cursor-stat/internal/live"
	"github.com/cursor-stat/cursor-stat/internal/paths"
	"github.com/cursor-stat/cursor-stat/internal/store"
)

// SnapshotOptions controls optional snapshot work.
type SnapshotOptions struct {
	// SkipToolScan skips walking agent-transcripts (TUI uses stats.db instead).
	SkipToolScan bool
}

// Snapshot builds a local-only report from Cursor files on disk.
func Snapshot(ctx context.Context, opts ...SnapshotOptions) (cursor.Snapshot, error) {
	var opt SnapshotOptions
	if len(opts) > 0 {
		opt = opts[0]
	}
	out := cursor.Snapshot{GeneratedAt: time.Now().UTC()}

	running, pid := live.Snapshot()
	out.Live.CursorRunning = running
	out.Live.CursorPID = pid

	if _, err := paths.CursorUserData(); err != nil {
		return out, err
	}

	wsDir, err := paths.WorkspaceStorageDir()
	if err != nil {
		return out, err
	}
	out.Live.ActiveWorkspace = live.ActiveWorkspace(wsDir)

	wsMap, err := workspacedb.Load(wsDir)
	if err != nil {
		out.Sources = append(out.Sources, cursor.SourceStatus{Name: "workspacedb", OK: false, Detail: err.Error()})
	} else {
		out.Storage.WorkspaceCount = len(wsMap)
		out.Sources = append(out.Sources, cursor.SourceStatus{
			Name: "workspacedb", OK: true, Records: len(wsMap),
		})
	}

	globalPath, err := paths.GlobalStateDB()
	if err != nil {
		return out, err
	}
	if info, err := store.StatFile(globalPath); err == nil {
		out.Storage.GlobalStateDB = cursorStoreFile(info, true)
	} else if os.IsNotExist(err) {
		out.Storage.GlobalStateDB = cursorStoreFile(&store.FileInfo{Path: globalPath}, false)
	}

	composers, gErr := globaldb.ReadComposers(globalPath, wsMap)
	if gErr != nil {
		out.Sources = append(out.Sources, cursor.SourceStatus{Name: "globaldb", OK: false, Detail: gErr.Error()})
	} else {
		out.Composers = composers
		out.Sources = append(out.Sources, cursor.SourceStatus{
			Name: "globaldb", OK: true, Records: len(composers),
		})
	}

	projectsDir, err := paths.ProjectsDir()
	if err != nil {
		return out, err
	}
	if st, err := store.DirSize(projectsDir); err == nil {
		out.Storage.ProjectsDir = cursorDirStat(projectsDir, st, true)
	} else if os.IsNotExist(err) {
		out.Storage.ProjectsDir = cursorDirStat(projectsDir, 0, false)
	}

	if statDir, err := paths.StatDataDir(); err == nil {
		statsDB := filepath.Join(statDir, "stats.db")
		if info, err := store.StatFile(statsDB); err == nil {
			out.Storage.StatsDB = cursorStoreFile(info, true)
		} else if os.IsNotExist(err) {
			out.Storage.StatsDB = cursorStoreFile(&store.FileInfo{Path: statsDB}, false)
		}
	}

	if opt.SkipToolScan {
		out.Sources = append(out.Sources, cursor.SourceStatus{
			Name: "transcripts", OK: true, Detail: "skipped (cache)",
		})
	} else {
		tc := &transcripts.Collector{ProjectsDir: projectsDir}
		toolEvents, transcriptFiles, tErr := tc.Collect(ctx)
		out.Storage.TranscriptFiles = transcriptFiles
		if tErr != nil {
			out.Sources = append(out.Sources, cursor.SourceStatus{Name: "transcripts", OK: false, Detail: tErr.Error()})
		} else {
			out.Tools = transcripts.Breakdown(toolEvents)
			out.Sources = append(out.Sources, cursor.SourceStatus{
				Name: "transcripts", OK: true, Records: len(toolEvents),
			})
			if ev, ok := transcripts.Latest(toolEvents); ok {
				out.Live.LastTool = ev.ToolName
				out.Live.LastEventAt = ev.At
			}
		}
	}

	mergeToolCounts(&out)
	sortComposers(out.Composers)

	return out, nil
}

func mergeToolCounts(snap *cursor.Snapshot) {
	if len(snap.Tools.ByTool) == 0 {
		return
	}
	// Attach global tool breakdown to composers when session id unknown — skip for now.
	_ = snap
}

func sortComposers(list []cursor.ComposerMeta) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].UpdatedAt.Equal(list[j].UpdatedAt) {
			return list[i].ID < list[j].ID
		}
		return list[i].UpdatedAt.After(list[j].UpdatedAt)
	})
}

func cursorStoreFile(info *store.FileInfo, readable bool) *cursor.StoreFile {
	return &cursor.StoreFile{
		Path:      info.Path,
		SizeBytes: info.Size,
		Readable:  readable,
	}
}

func cursorDirStat(path string, size int64, exists bool) *cursor.DirStat {
	return &cursor.DirStat{
		Path:      path,
		SizeBytes: size,
		Exists:    exists,
	}
}
