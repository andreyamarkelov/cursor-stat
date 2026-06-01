package transcripts

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cursor-stat/cursor-stat/internal/cursor"
)

// Collector scans ~/.cursor/projects for agent transcript JSONL files.
type Collector struct {
	ProjectsDir string
}

func (c *Collector) Name() string { return "transcripts" }

// Collect walks transcript files and returns tool events.
func (c *Collector) Collect(ctx context.Context) ([]cursor.ToolEvent, int, error) {
	if c.ProjectsDir == "" {
		return nil, 0, nil
	}
	if _, err := os.Stat(c.ProjectsDir); os.IsNotExist(err) {
		return nil, 0, nil
	} else if err != nil {
		return nil, 0, err
	}

	var out []cursor.ToolEvent
	files := 0
	err := filepath.WalkDir(c.ProjectsDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".jsonl") && !strings.HasSuffix(name, ".txt") {
			return nil
		}
		if !strings.Contains(path, string(filepath.Separator)+"agent-transcripts"+string(filepath.Separator)) {
			return nil
		}
		files++
		events, err := parseFile(path)
		if err != nil {
			return nil // skip unreadable files
		}
		out = append(out, events...)
		return nil
	})
	return out, files, err
}

func parseFile(path string) ([]cursor.ToolEvent, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	workspace := workspaceFromPath(path)
	sessionID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	fileTime := info.ModTime()

	var events []cursor.ToolEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		lineNo++
		events = append(events, parseLine(line, sessionID, workspace, fileTime, lineNo)...)
	}
	return events, scanner.Err()
}

func parseLine(line []byte, sessionID, workspace string, fileTime time.Time, lineNo int) []cursor.ToolEvent {
	// Cursor agent-transcripts: {"role":"assistant","message":{"content":[{"type":"tool_use","name":"Read",...}]}}
	var assistant struct {
		Role    string `json:"role"`
		Message struct {
			Content []struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &assistant); err == nil && assistant.Role == "assistant" {
		var out []cursor.ToolEvent
		for i, part := range assistant.Message.Content {
			if part.Type != "tool_use" || part.Name == "" {
				continue
			}
			out = append(out, cursor.ToolEvent{
				At:        eventTime(fileTime, lineNo, i),
				SessionID: sessionID,
				ToolName:  part.Name,
				Success:   true,
				Workspace: workspace,
				Source:    "transcript",
			})
		}
		if len(out) > 0 {
			return out
		}
	}

	// Legacy flat JSONL (tests / older formats).
	var row map[string]json.RawMessage
	if err := json.Unmarshal(line, &row); err != nil {
		return nil
	}
	toolName := firstString(row, "tool_name", "toolName", "name")
	if toolName == "" {
		if typ := firstString(row, "type"); typ == "tool_use" || typ == "tool_call" {
			toolName = firstString(row, "tool", "tool_name")
		}
	}
	if toolName == "" {
		return nil
	}
	at := parseTime(firstString(row, "timestamp", "time", "createdAt"))
	if at.IsZero() {
		at = eventTime(fileTime, lineNo, 0)
	}
	errMsg := firstString(row, "error", "failure", "stderr")
	sid := firstString(row, "session_id", "sessionId", "conversation_id", "composerId")
	if sid == "" {
		sid = sessionID
	}
	return []cursor.ToolEvent{{
		At:        at,
		SessionID: sid,
		ToolName:  toolName,
		Success:   errMsg == "",
		Workspace: workspace,
		Source:    "transcript",
	}}
}

func workspaceFromPath(path string) string {
	// .../projects/Users-user-tmp-my-app/agent-transcripts/id.jsonl
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i, p := range parts {
		if p == "projects" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func firstString(row map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		raw, ok := row[k]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err == nil && s != "" {
			return s
		}
	}
	return ""
}

func eventTime(fileTime time.Time, lineNo, idx int) time.Time {
	if fileTime.IsZero() {
		return time.Now().UTC().Add(time.Duration(idx) * time.Microsecond)
	}
	return fileTime.Add(time.Duration(lineNo)*time.Second + time.Duration(idx)*time.Millisecond)
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

// Breakdown aggregates tool events.
func Breakdown(events []cursor.ToolEvent) cursor.ToolBreakdown {
	out := cursor.ToolBreakdown{ByTool: make(map[string]int)}
	for _, ev := range events {
		out.Total++
		if !ev.Success {
			out.Failures++
		}
		out.ByTool[ev.ToolName]++
	}
	return out
}

// Latest returns the most recent tool event by timestamp.
func Latest(events []cursor.ToolEvent) (cursor.ToolEvent, bool) {
	var best cursor.ToolEvent
	ok := false
	for _, ev := range events {
		if !ok || ev.At.After(best.At) {
			best = ev
			ok = true
		}
	}
	return best, ok
}
