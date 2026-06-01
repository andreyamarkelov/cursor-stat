package transcripts

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cursor-stat/cursor-stat/internal/cursor"
)

func TestCollectJSONL(t *testing.T) {
	root := t.TempDir()
	transcriptDir := filepath.Join(root, "Users-me-proj", "agent-transcripts")
	if err := os.MkdirAll(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"tool_name":"Shell","timestamp":"2026-05-31T12:00:00Z","session_id":"sess-1"}`
	if err := os.WriteFile(filepath.Join(transcriptDir, "sess-1.jsonl"), []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := &Collector{ProjectsDir: root}
	events, files, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if files != 1 {
		t.Fatalf("files %d", files)
	}
	if len(events) != 1 {
		t.Fatalf("events %d", len(events))
	}
	if events[0].ToolName != "Shell" {
		t.Fatalf("tool %q", events[0].ToolName)
	}
}

func TestCollectCursorAssistantFormat(t *testing.T) {
	root := t.TempDir()
	transcriptDir := filepath.Join(root, "Users-me-proj", "agent-transcripts", "sess-uuid")
	if err := os.MkdirAll(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"role":"assistant","message":{"content":[{"type":"text","text":"hi"},{"type":"tool_use","name":"Read","input":{}},{"type":"tool_use","name":"Glob","input":{}}]}}`
	if err := os.WriteFile(filepath.Join(transcriptDir, "sess-uuid.jsonl"), []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := &Collector{ProjectsDir: root}
	events, files, err := c.Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if files != 1 {
		t.Fatalf("files %d", files)
	}
	if len(events) != 2 {
		t.Fatalf("events %d", len(events))
	}
}

func TestBreakdownCounts(t *testing.T) {
	events := []cursor.ToolEvent{
		{ToolName: "Shell", Success: true},
		{ToolName: "Shell", Success: false},
		{ToolName: "Read", Success: true},
	}
	b := Breakdown(events)
	if b.Total != 3 || b.Failures != 1 {
		t.Fatalf("total=%d failures=%d", b.Total, b.Failures)
	}
	if b.ByTool["Shell"] != 2 {
		t.Fatalf("shell count %d", b.ByTool["Shell"])
	}
}

func TestLatest(t *testing.T) {
	early := time.Date(2026, 5, 31, 10, 0, 0, 0, time.UTC)
	late := time.Date(2026, 5, 31, 11, 0, 0, 0, time.UTC)
	events := []cursor.ToolEvent{
		{At: early, ToolName: "Read"},
		{At: late, ToolName: "Shell"},
	}
	ev, ok := Latest(events)
	if !ok || ev.ToolName != "Shell" {
		t.Fatalf("latest %+v ok=%v", ev, ok)
	}
}
