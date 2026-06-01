package hooks

import (
	"encoding/json"
	"strings"

	"github.com/cursor-stat/cursor-stat/internal/cursor"
)

const maxHookBody = 512 * 1024

type hookPayload struct {
	HookEventName  string `json:"hook_event_name"`
	Model          string `json:"model"`
	ToolName       string `json:"tool_name"`
	SessionID      string `json:"session_id"`
	ConversationID string `json:"conversation_id"`
	GenerationID   string `json:"generation_id"`
}

// ParseEvent decodes a Cursor hook POST body into a live event + optional model choice.
func ParseEvent(body []byte) (cursor.LiveEvent, *cursor.ModelChoiceEvent) {
	var p hookPayload
	_ = json.Unmarshal(body, &p)

	sid := p.SessionID
	if sid == "" {
		sid = p.ConversationID
	}

	ev := cursor.LiveEvent{
		Kind:    p.HookEventName,
		Tool:    p.ToolName,
		Session: sid,
		Detail:  p.HookEventName,
	}
	if p.Model != "" {
		ev.Model = p.Model
		ev.Manual = !cursor.IsAutoModel(p.Model)
	}

	if p.HookEventName != "beforeSubmitPrompt" || strings.TrimSpace(p.Model) == "" {
		return ev, nil
	}

	dedupeID := p.GenerationID
	if dedupeID == "" {
		dedupeID = p.ConversationID
	}
	return ev, &cursor.ModelChoiceEvent{
		SessionID:      dedupeID,
		ConversationID: p.ConversationID,
		GenerationID:   p.GenerationID,
		Model:          strings.TrimSpace(p.Model),
		Manual:         !cursor.IsAutoModel(p.Model),
		Source:         "hook",
	}
}
