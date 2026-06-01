package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cursor-stat/cursor-stat/internal/paths"
)

const Marker = "cursor-stat-hook.js"

var hookEvents = []string{
	"sessionStart", "sessionEnd", "beforeSubmitPrompt",
	"preToolUse", "postToolUse", "postToolUseFailure",
	"subagentStart", "subagentStop", "preCompact", "afterAgentThought", "stop",
}

// Install merges cursor-stat hook entries into ~/.cursor/hooks.json (append-only).
func Install(hookScriptPath string) (added int, err error) {
	dot, err := paths.CursorDotDir()
	if err != nil {
		return 0, err
	}
	hooksPath := filepath.Join(dot, "hooks.json")

	settings := map[string]any{"version": 1, "hooks": map[string]any{}}
	if data, err := os.ReadFile(hooksPath); err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		settings["hooks"] = hooks
	}

	cmd := fmt.Sprintf("node %q", hookScriptPath)
	for _, ev := range hookEvents {
		arr, _ := hooks[ev].([]any)
		found := false
		for _, item := range arr {
			m, _ := item.(map[string]any)
			if c, _ := m["command"].(string); strings.Contains(c, Marker) {
				found = true
				m["command"] = cmd
				break
			}
		}
		if found {
			continue
		}
		arr = append(arr, map[string]any{"command": cmd})
		hooks[ev] = arr
		added++
	}

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(dot, 0o700); err != nil {
		return 0, err
	}
	return added, os.WriteFile(hooksPath, append(out, '\n'), 0o600)
}

// Installed reports whether our hook marker is present.
func Installed() bool {
	dot, err := paths.CursorDotDir()
	if err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(dot, "hooks.json"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), Marker)
}
