package cursor

import (
	"strings"
	"time"
)

// ModelChoiceEvent is a prompt submission with a model selection.
type ModelChoiceEvent struct {
	At             time.Time `json:"at"`
	SessionID      string    `json:"session_id,omitempty"`
	ConversationID string    `json:"conversation_id,omitempty"`
	GenerationID   string    `json:"generation_id,omitempty"`
	Model          string    `json:"model"`
	Manual         bool      `json:"manual"`
	Source         string    `json:"source"`
}

// ModelBreakdown aggregates model choice events.
type ModelBreakdown struct {
	Total   int            `json:"total"`
	Manual  int            `json:"manual"`
	ByModel map[string]int `json:"by_model"`
}

// IsAutoModel reports whether Cursor sent the Auto/default model picker value.
func IsAutoModel(model string) bool {
	switch strings.ToLower(strings.TrimSpace(model)) {
	case "", "default", "auto", "automatic":
		return true
	}
	return false
}

// NormalizeModel returns a display label for a model id.
func NormalizeModel(model string) string {
	m := strings.TrimSpace(model)
	if m == "" {
		return "unknown"
	}
	if IsAutoModel(m) {
		return "Auto"
	}
	return m
}
