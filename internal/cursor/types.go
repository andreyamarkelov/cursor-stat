package cursor

import "time"

// ComposerMeta describes one Cursor composer (agent/chat session).
type ComposerMeta struct {
	ID            string         `json:"id"`
	WorkspaceID   string         `json:"workspace_id,omitempty"`
	WorkspacePath string         `json:"workspace_path,omitempty"`
	Title         string         `json:"title,omitempty"`
	CreatedAt     time.Time      `json:"created_at,omitempty"`
	UpdatedAt     time.Time      `json:"updated_at,omitempty"`
	MessageCount  int            `json:"message_count"`
	ToolCounts    map[string]int `json:"tool_counts,omitempty"`
	Source        string         `json:"source"`
}

// ToolEvent is a normalized tool invocation.
type ToolEvent struct {
	At        time.Time `json:"at"`
	SessionID string    `json:"session_id,omitempty"`
	ToolName  string    `json:"tool_name"`
	Success   bool      `json:"success"`
	Workspace string    `json:"workspace,omitempty"`
	Source    string    `json:"source"`
}

// LiveSnapshot holds "right now" signals from local sources only.
type LiveSnapshot struct {
	CursorRunning     bool      `json:"cursor_running"`
	CursorPID         int       `json:"cursor_pid,omitempty"`
	ActiveWorkspace   string    `json:"active_workspace,omitempty"`
	ActiveSession     string    `json:"active_session,omitempty"`
	ActiveSessionMsgs int       `json:"active_session_msgs,omitempty"`
	LastEventAt       time.Time `json:"last_event_at,omitempty"`
	LastTool          string    `json:"last_tool,omitempty"`
	LastEventKind     string    `json:"last_event_kind,omitempty"`
	LastModel         string    `json:"last_model,omitempty"`
	LastModelManual   bool      `json:"last_model_manual,omitempty"`
}

// StorageSummary reports on-disk sizes for Cursor data dirs and our cache.
type StorageSummary struct {
	GlobalStateDB   *StoreFile `json:"global_state_db,omitempty"`
	ProjectsDir     *DirStat   `json:"projects_dir,omitempty"`
	StatsDB         *StoreFile `json:"stats_db,omitempty"`
	WorkspaceCount  int        `json:"workspace_count"`
	TranscriptFiles int        `json:"transcript_files"`
}

// StoreFile describes one on-disk Cursor database file.
type StoreFile struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	Readable  bool   `json:"readable"`
}

// DirStat describes a directory size snapshot.
type DirStat struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	Exists    bool   `json:"exists"`
}

// SourceStatus reports collector health for one data source.
type SourceStatus struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Detail  string `json:"detail,omitempty"`
	Records int    `json:"records,omitempty"`
}

// ToolBreakdown aggregates tool events.
type ToolBreakdown struct {
	Total    int            `json:"total"`
	Failures int            `json:"failures"`
	ByTool   map[string]int `json:"by_tool"`
}

// Snapshot is the top-level --once JSON output.
type Snapshot struct {
	GeneratedAt time.Time      `json:"generated_at"`
	Live        LiveSnapshot   `json:"live"`
	Storage     StorageSummary `json:"storage"`
	Composers   []ComposerMeta `json:"composers"`
	Tools       ToolBreakdown  `json:"tools"`
	Sources     []SourceStatus `json:"sources"`
}
