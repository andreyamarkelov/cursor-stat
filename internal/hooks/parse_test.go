package hooks

import "testing"

func TestParseEventBeforeSubmitPrompt(t *testing.T) {
	body := []byte(`{
		"hook_event_name":"beforeSubmitPrompt",
		"model":"claude-opus-4-7",
		"conversation_id":"conv-1",
		"generation_id":"gen-1",
		"prompt":"hello"
	}`)
	ev, choice := ParseEvent(body)
	if ev.Kind != "beforeSubmitPrompt" {
		t.Fatalf("kind %q", ev.Kind)
	}
	if !ev.Manual || ev.Model != "claude-opus-4-7" {
		t.Fatalf("ev model %+v manual=%v", ev.Model, ev.Manual)
	}
	if choice == nil {
		t.Fatal("expected model choice")
	}
	if !choice.Manual || choice.Model != "claude-opus-4-7" || choice.SessionID != "gen-1" {
		t.Fatalf("choice %+v", choice)
	}
}

func TestParseEventAutoModel(t *testing.T) {
	body := []byte(`{"hook_event_name":"beforeSubmitPrompt","model":"default","generation_id":"g2"}`)
	ev, choice := ParseEvent(body)
	if ev.Manual {
		t.Fatal("auto should not be manual")
	}
	if choice == nil || choice.Manual {
		t.Fatalf("choice %+v", choice)
	}
}

func TestParseEventPostToolUse(t *testing.T) {
	body := []byte(`{"hook_event_name":"postToolUse","tool_name":"Read"}`)
	ev, choice := ParseEvent(body)
	if ev.Tool != "Read" || choice != nil {
		t.Fatalf("ev=%+v choice=%+v", ev, choice)
	}
}
