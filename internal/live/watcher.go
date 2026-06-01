package live

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cursor-stat/cursor-stat/internal/cursor"
	"github.com/cursor-stat/cursor-stat/internal/paths"
	"github.com/fsnotify/fsnotify"
)

// Watcher watches Cursor files and pushes live events to a ring buffer.
type Watcher struct {
	ring *Ring
}

// NewWatcher creates a filesystem watcher.
func NewWatcher(ring *Ring) *Watcher {
	return &Watcher{ring: ring}
}

// Run blocks until ctx is cancelled.
func (w *Watcher) Run(ctx context.Context) error {
	if w.ring == nil {
		w.ring = NewRing(defaultRingSize)
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fw.Close()

	addPath := func(p string) {
		if p == "" {
			return
		}
		_ = fw.Add(p)
	}

	if global, err := paths.GlobalStateDB(); err == nil {
		addPath(filepath.Dir(global))
	}
	if projects, err := paths.ProjectsDir(); err == nil {
		_ = filepath.WalkDir(projects, func(path string, d os.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, "agent-transcripts") {
				addPath(path)
			}
			return nil
		})
	}

	debounce := time.NewTimer(0)
	if !debounce.Stop() {
		<-debounce.C
	}
	pending := ""

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-fw.Events:
			if !ok {
				return nil
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				pending = ev.Name
				debounce.Reset(500 * time.Millisecond)
			}
		case err, ok := <-fw.Errors:
			if !ok {
				return nil
			}
			w.ring.Push(cursor.LiveEvent{
				At:     time.Now().UTC(),
				Kind:   "watch_error",
				Detail: err.Error(),
			})
		case <-debounce.C:
			if pending != "" {
				w.ring.Push(cursor.LiveEvent{
					At:     time.Now().UTC(),
					Kind:   "fs_change",
					Detail: filepath.Base(pending),
				})
				pending = ""
			}
		}
	}
}
