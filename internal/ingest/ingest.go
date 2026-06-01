package ingest

import (
	"context"
	"fmt"
	"time"

	"github.com/cursor-stat/cursor-stat/internal/cursor"
	"github.com/cursor-stat/cursor-stat/internal/cursor/globaldb"
	"github.com/cursor-stat/cursor-stat/internal/cursor/transcripts"
	"github.com/cursor-stat/cursor-stat/internal/cursor/workspacedb"
	"github.com/cursor-stat/cursor-stat/internal/paths"
	"github.com/cursor-stat/cursor-stat/internal/store"
)

const (
	sourceGlobalDB    = "global:vscdb"
	sourceTranscripts = "transcripts:projects"
)

// Run ingests Cursor local data into stats.db (idempotent).
func Run(ctx context.Context, db *store.DB) (cursor.IngestResult, error) {
	result := cursor.IngestResult{CompletedAt: time.Now().UTC()}

	if err := db.RepairInvalidToolTimestamps(); err != nil {
		return result, err
	}

	wsDir, err := paths.WorkspaceStorageDir()
	if err != nil {
		return result, err
	}
	wsMap, err := workspacedb.Load(wsDir)
	if err != nil {
		return result, err
	}

	globalPath, err := paths.GlobalStateDB()
	if err != nil {
		return result, err
	}

	if need, err := db.NeedsIngest(sourceGlobalDB, globalPath); err == nil && need {
		composers, err := globaldb.ReadComposers(globalPath, wsMap)
		if err != nil {
			return result, fmt.Errorf("globaldb: %w", err)
		}
		for _, c := range composers {
			if err := db.UpsertComposer(c); err != nil {
				return result, err
			}
			result.ComposersUpserted++
			ok, err := insertSessionStart(db, c)
			if err != nil {
				return result, err
			}
			if ok {
				result.EventsInserted++
			} else {
				result.EventsSkipped++
			}
		}
		choices, err := globaldb.ReadBubbleModelChoices(globalPath)
		if err != nil {
			return result, fmt.Errorf("bubble models: %w", err)
		}
		for _, c := range choices {
			ok, err := db.InsertModelChoice(c)
			if err != nil {
				return result, err
			}
			if ok {
				result.EventsInserted++
			} else {
				result.EventsSkipped++
			}
		}

		if err := db.MarkIngested(sourceGlobalDB, globalPath); err != nil {
			return result, err
		}
		result.SourcesUpdated++
	}

	projectsDir, err := paths.ProjectsDir()
	if err != nil {
		return result, err
	}
	marker := projectsDir
	fp, fpMtime, err := store.DirFingerprint(projectsDir)
	if err != nil {
		return result, err
	}
	if need, err := db.NeedsIngestFingerprint(sourceTranscripts, fp, fpMtime); err == nil && need {
		tc := &transcripts.Collector{ProjectsDir: projectsDir}
		events, _, err := tc.Collect(ctx)
		if err != nil {
			return result, fmt.Errorf("transcripts: %w", err)
		}
		if err := db.ReplaceToolEventsBySource("transcript", events); err != nil {
			return result, err
		}
		result.EventsInserted += len(events)
		if err := db.MarkIngestedFingerprint(sourceTranscripts, marker, fp, fpMtime); err != nil {
			return result, err
		}
		result.SourcesUpdated++
	}

	if err := db.RebuildDailyRollups(); err != nil {
		return result, err
	}
	_ = db.SetMeta("last_ingest_at", result.CompletedAt.Format(time.RFC3339))
	return result, nil
}

func insertSessionStart(db *store.DB, c cursor.ComposerMeta) (bool, error) {
	if c.CreatedAt.IsZero() {
		return false, nil
	}
	ok := true
	return db.InsertEvent(c.CreatedAt, c.ID, c.WorkspacePath, "session_start", "", c.Source, &ok)
}
