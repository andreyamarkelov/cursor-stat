package live

import (
	"sync"
	"time"

	"github.com/cursor-stat/cursor-stat/internal/cursor"
)

const defaultRingSize = 64

// Ring is a fixed-size buffer of recent live events.
type Ring struct {
	mu     sync.RWMutex
	events []cursor.LiveEvent
	cap    int
}

// NewRing creates a ring buffer.
func NewRing(capacity int) *Ring {
	if capacity <= 0 {
		capacity = defaultRingSize
	}
	return &Ring{cap: capacity}
}

// Push appends an event, dropping oldest when full.
func (r *Ring) Push(ev cursor.LiveEvent) {
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
	if len(r.events) > r.cap {
		r.events = r.events[len(r.events)-r.cap:]
	}
}

// List returns up to n most recent events (newest last).
func (r *Ring) List(n int) []cursor.LiveEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if n <= 0 || n > len(r.events) {
		n = len(r.events)
	}
	start := len(r.events) - n
	out := make([]cursor.LiveEvent, n)
	copy(out, r.events[start:])
	return out
}

// LatestAny returns the newest event of any kind.
func (r *Ring) LatestAny() (cursor.LiveEvent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.events) == 0 {
		return cursor.LiveEvent{}, false
	}
	return r.events[len(r.events)-1], true
}

// LatestModel returns the newest beforeSubmitPrompt event with a model name.
func (r *Ring) LatestModel() (cursor.LiveEvent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for i := len(r.events) - 1; i >= 0; i-- {
		if r.events[i].Model != "" {
			return r.events[i], true
		}
	}
	return cursor.LiveEvent{}, false
}

// LatestTool returns the newest event with a tool name.
func (r *Ring) LatestTool() (cursor.LiveEvent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for i := len(r.events) - 1; i >= 0; i-- {
		if r.events[i].Tool != "" {
			return r.events[i], true
		}
	}
	return cursor.LiveEvent{}, false
}
