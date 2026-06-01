package cursor

import "time"

// DailyRollup is one day of aggregated stats in our cache.
type DailyRollup struct {
	Date            string `json:"date"`
	SessionsStarted int    `json:"sessions_started"`
	UserMessages    int    `json:"user_messages"`
	AssistantMsgs   int    `json:"assistant_messages"`
	ToolCalls       int    `json:"tool_calls"`
	ToolFailures    int    `json:"tool_failures"`
}

// TodayStats holds rollup numbers for the current UTC day.
type TodayStats struct {
	SessionsStarted    int `json:"sessions_started"`
	Messages           int `json:"messages"`
	ToolCalls          int `json:"tool_calls"`
	ToolFailures       int `json:"tool_failures"`
	ManualModelPrompts int `json:"manual_model_prompts"`
	AutoModelPrompts   int `json:"auto_model_prompts"`
}

// Dashboard is the TUI view model (cache + live).
type Dashboard struct {
	GeneratedAt  time.Time      `json:"generated_at"`
	Live         LiveSnapshot   `json:"live"`
	Today        TodayStats     `json:"today"`
	History      []DailyRollup  `json:"history"`
	Composers    []ComposerMeta `json:"composers"`
	Tools        ToolBreakdown  `json:"tools"`
	ToolsLive    int            `json:"tools_live,omitempty"`
	Models       ModelBreakdown `json:"models"`
	Storage      StorageSummary `json:"storage"`
	CacheReady   bool           `json:"cache_ready"`
	LastIngestAt time.Time      `json:"last_ingest_at,omitempty"`
	LiveEvents   []LiveEvent    `json:"live_events,omitempty"`
}

// LiveEvent is a recent hook or filesystem signal.
type LiveEvent struct {
	At      time.Time `json:"at"`
	Kind    string    `json:"kind"`
	Detail  string    `json:"detail,omitempty"`
	Tool    string    `json:"tool,omitempty"`
	Model   string    `json:"model,omitempty"`
	Manual  bool      `json:"manual_model,omitempty"`
	Session string    `json:"session_id,omitempty"`
}

// IngestResult summarizes an ingest run.
type IngestResult struct {
	ComposersUpserted int       `json:"composers_upserted"`
	EventsInserted    int       `json:"events_inserted"`
	EventsSkipped     int       `json:"events_skipped"`
	SourcesUpdated    int       `json:"sources_updated"`
	CompletedAt       time.Time `json:"completed_at"`
}
