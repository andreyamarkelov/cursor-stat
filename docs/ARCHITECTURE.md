# cursor-stat — Architecture

Contributor reference for modules, schema, and data flow. **Users** should read [USER-GUIDE.md](USER-GUIDE.md). **New Go developers** should read [NOVICE-GUIDE.md](NOVICE-GUIDE.md) first.

## 1. System context

```text
┌──────────────────────────────────────────────────────────────────┐
│                        User's machine                            │
│                                                                  │
│  ┌─────────────┐    reads (RO)     ┌──────────────────────────┐  │
│  │ Cursor IDE  │ ────────────────► │ Cursor local data        │  │
│  │             │    hooks (opt)    │ • state.vscdb (+wal)     │  │
│  └─────────────┘ ────────────────► │ • workspaceStorage       │  │
│                                    │ • agent-transcripts      │  │
│                                    │ • hooks.json             │  │
│                                    └────────────┬─────────────┘  │
│                                                 │                │
│                                    ┌────────────▼─────────────┐  │
│                                    │      cursor-stat         │  │
│                                    │  ┌────────┐ ┌─────────┐  │  │
│                                    │  │Collect │►│  Store  │  │  │
│                                    │  └───┬────┘ └────┬────┘  │  │
│                                    │      │           │       │  │
│                                    │  ┌───▼───────────▼────┐  │  │
│                                    │  │    Aggregate       │  │  │
│                                    │  └───┬───────────────┬──┘  │  │
│                                    │      │               │     │  │
│                                    │  ┌───▼────┐    ┌─────▼──┐  │  │
│                                    │  │  Live  │    │  TUI   │  │  │
│                                    │  └────────┘    └────────┘  │  │
│                                    └────────────┬───────────────┘  │
│                                                 │ writes           │
│                                    ┌────────────▼─────────────┐  │
│                                    │ ~/.cursor-stat/          │  │
│                                    │ • stats.db (our cache)   │  │
│                                    └──────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
```

**cursor-stat never writes to Cursor’s directories** except an optional, marker-based hook entry in `~/.cursor/hooks.json` when the user runs `hooks install`.

---

## 2. Layered design

| Layer | Package | Responsibility |
|-------|---------|----------------|
| **Entry** | `cmd/cursor-stat` | Flags, subcommands, wiring |
| **Presentation** | `internal/tui` | Bubble Tea models, keybindings, render |
| **Application** | `internal/aggregate`, `internal/live` | Combine sources into view models |
| **Domain** | `internal/cursor/*` | Parse Cursor formats → typed structs |
| **Persistence** | `internal/store`, `internal/ingest` | Our SQLite cache, migrations |
| **Infrastructure** | `internal/paths` | OS paths, WAL-safe DB open |

Dependencies flow **downward only** (TUI → aggregate → store/cursor → paths). No layer imports TUI.

---

## 3. Core domain types

These structs are the **contract** between collectors and the rest of the app. Keep them in `internal/cursor/types.go`.

```go
// ComposerMeta — one Cursor "composer" (agent/chat session).
type ComposerMeta struct {
    ID              string
    WorkspaceID     string    // hash under workspaceStorage
    WorkspacePath   string    // resolved from workspace.json
    Title           string    // user-visible name if present
    CreatedAt       time.Time
    UpdatedAt       time.Time
    MessageCount    int
    ToolCounts      map[string]int
    Source          string
}

// ToolEvent — normalized tool invocation.
type ToolEvent struct {
    At          time.Time
    SessionID   string
    ToolName    string
    Success     bool
    Workspace   string
    Source      string // "transcript", "hook", "bubble"
}

// LiveSnapshot — "right now" panel.
type LiveSnapshot struct {
    CursorRunning     bool
    CursorPID         int
    ActiveWorkspace   string
    ActiveSession     string
    ActiveSessionMsgs int
    LastEventAt       time.Time
    LastTool          string
    LastEventKind     string
    LastModel         string
    LastModelManual   bool
}

// DailyRollup — one row in historical sparklines.
type DailyRollup struct {
    Date            string // YYYY-MM-DD, UTC
    SessionsStarted int
    UserMessages    int
    AssistantMsgs   int
    ToolCalls       int
    ToolFailures    int
}

// StorageSummary — on-disk sizes for Cursor data and our cache.
type StorageSummary struct {
    GlobalStateDB   *StoreFile
    ProjectsDir     *DirStat
    StatsDB         *StoreFile
    WorkspaceCount  int
    TranscriptFiles int
}

// StoreFile — one SQLite database file info.
type StoreFile struct {
    Path      string
    SizeBytes int64
    Readable  bool
}

// DirStat — directory size summary.
type DirStat struct {
    Path      string
    SizeBytes int64
    Exists    bool
}
```

---

## 4. Collectors (read Cursor data)

The code does not require a shared collector interface. Each package exposes the narrow function or struct it needs (`globaldb.ReadComposers`, `globaldb.ReadBubbleModelChoices`, `transcripts.Collector.Collect`, `workspacedb.Load`), and `internal/ingest/ingest.go` orchestrates them.

### 4.1 `globaldb` — global `state.vscdb`

- **Path:** `{CursorUserData}/globalStorage/state.vscdb`
- **Tables:** `ItemTable` (key/value), `cursorDiskKV` (key/value)
- **Keys of interest:**
  - `composer.composerHeaders` (Cursor 3.0+ central index)
  - `composerData:{uuid}` — session metadata JSON
  - `bubbleId:{...}` — individual messages (count only by default)

**WAL-safe read pattern:**

```text
1. paths.GlobalStateDB() → absolute path
2. sqliteutil.OpenReadOnlySnapshot(path) → copies vscdb+wal+shm to os.TempDir
3. Query with read-only connection
4. defer os.RemoveAll(tempCopy)
```

### 4.2 `workspacedb` — per-workspace `state.vscdb`

- **Path:** `{CursorUserData}/workspaceStorage/{hash}/`
- **Files:** `state.vscdb`, `workspace.json`
- **Purpose:** Map hash → folder path; legacy per-workspace composer lists (pre-3.0)

### 4.3 `transcripts` — agent JSONL / txt logs

- **Path:** `~/.cursor/projects/{sanitized-path}/agent-transcripts/*`
- **Purpose:** Tool names, timestamps, errors; often easier to parse than giant KV blobs
- **Strategy:** Stream line-by-line; do not load whole file into memory

### 4.4 Known but not currently collected: `chats` — legacy/alternate `store.db`

- **Path:** `~/.cursor/chats/**/store.db`
- **Purpose:** Supplement session list when global index incomplete
- **Status:** Documented as a possible future fallback; no active collector reads this today.

### 4.5 Known but not currently collected: `aitracking` — `ai-code-tracking.db`

- **Path:** `~/.cursor/ai-tracking/ai-code-tracking.db` (if exists)
- **Purpose:** AI attribution / acceptance stats (schema TBD in implementation)
- **Status:** Documented as a possible future source; no active collector reads this today.

### 4.6 `hooks` — live event tap (optional)

- Node script `hooks/cursor-stat-hook.js` POSTs hook JSON to `127.0.0.1:23556/event`
- `internal/hooks/server.go` parses payload, pushes to `live.Ring`, persists model choices to `stats.db`
- **`beforeSubmitPrompt`** — `model` field → `events` row (`kind=model_choice`); `default`/`auto` = Auto
- **Tool hooks** — `preToolUse` / `postToolUse` → ring only (live Overview / Tab 3 merge)

See [USER-GUIDE.md](USER-GUIDE.md#model-tracking-auto-vs-manual-pick) for user-facing behavior.

---

## 5. Store (our cache)

**Path:** `~/.cursor-stat/stats.db`

### 5.1 Why a separate database?

| Reason | Explanation |
|--------|-------------|
| Performance | Pre-aggregated rollups; TUI reads small queries |
| Stability | Cursor schema changes don’t break dashboards overnight |
| History | We record ingest timestamps even if Cursor deletes old chats |
| Privacy control | Store hashes/counts; omit bodies unless user opts in |

### 5.2 Schema (v1)

```sql
-- migrations/001_initial.sql

CREATE TABLE IF NOT EXISTS schema_version (
    version INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS ingest_sources (
    id           TEXT PRIMARY KEY,  -- e.g. "global:vscdb"
    path         TEXT NOT NULL,
    file_size    INTEGER,
    file_mtime   INTEGER NOT NULL,  -- unix nano
    content_hash TEXT,              -- sha256 of copied snapshot
    ingested_at  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS composers (
    id              TEXT PRIMARY KEY,
    workspace_id    TEXT,
    workspace_path  TEXT,
    title           TEXT,
    created_at      INTEGER,
    updated_at      INTEGER,
    message_count   INTEGER DEFAULT 0,
    source          TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    at          INTEGER NOT NULL,
    session_id  TEXT,
    workspace   TEXT,
    kind        TEXT NOT NULL,  -- tool, model_choice, session_start, …
    tool_name   TEXT,           -- tool id OR model id for model_choice
    success     INTEGER,        -- tool: 1=ok; model_choice: 1=manual, 0=auto
    source      TEXT NOT NULL,
    meta_json   TEXT            -- optional small JSON, no large bodies
);

CREATE INDEX IF NOT EXISTS idx_events_at ON events(at);
CREATE INDEX IF NOT EXISTS idx_events_session ON events(session_id);

CREATE TABLE IF NOT EXISTS daily_rollups (
    date              TEXT PRIMARY KEY, -- YYYY-MM-DD UTC
    sessions_started  INTEGER DEFAULT 0,
    user_messages     INTEGER DEFAULT 0,
    assistant_msgs    INTEGER DEFAULT 0,
    tool_calls        INTEGER DEFAULT 0,
    tool_failures     INTEGER DEFAULT 0
);
```

### 5.3 Ingest pipeline

```text
ingest.Run(ctx)
  │
  ├─► For each registered Collector
  │     ├─ stat source file(s)
  │     ├─ skip if mtime+size unchanged (ingest_sources)
  │     └─ else Collect → merge rows (transactions)
  │
  ├─► aggregate.RebuildDailyRollups(from, to)
  │
  └─► commit
```

**Idempotency:** Events insert with `(source, session_id, at, kind, tool_name)` uniqueness where possible to avoid duplicates on re-ingest.

---

## 6. Aggregate layer

Pure functions + SQL queries; **no terminal code**.

| Function | Output |
|----------|--------|
| `Snapshot(ctx, opts)` | Live read of Cursor files (`--once`) |
| `Dashboard(ctx, db, ring)` | TUI view model: cache + live merge |
| `ToolBreakdownAll` / `ModelBreakdownAll` | SQL aggregates on `events` |

Unit-testable with temp SQLite files in `*_test.go`.

---

## 7. Live layer

```text
live.Watcher
  ├─ process.Poll() every 2s — is Cursor running?
  ├─ fsnotify on:
  │    • global state.vscdb-wal
  │    • active workspace transcript dir
  └─ optional hook HTTP :PORT/event
```

`live.Snapshot()` returns `LiveSnapshot` without blocking TUI &gt;100ms (timeouts on DB reads).

---

## 8. TUI layer (Bubble Tea)

Single file: `internal/tui/model.go` — one `model` struct holds tab index, `cursor.Dashboard` data, ingest flag, and refresh generation counter.

```text
Init → tick (2s) + loadCmd → aggregate.Dashboard → dataMsg → View
Keys: q quit, r refresh, i ingest, 1-5 tabs
```

During **ingest**, ticks do not reload the dashboard (bars stay stable until ingest completes).

Long I/O runs in `tea.Cmd` goroutines; `View` never touches SQLite or walks transcripts.

---

## 9. Paths {#paths}

Centralize in `internal/paths/paths.go`:

| Function | Returns |
|----------|---------|
| `CursorUserData()` | `~/Library/Application Support/Cursor/User` (macOS) |
| `CursorDotDir()` | `~/.cursor` |
| `GlobalStateDB()` | `{UserData}/globalStorage/state.vscdb` |
| `WorkspaceStorageDir()` | `{UserData}/workspaceStorage` |
| `ProjectsDir()` | `~/.cursor/projects` |
| `StatDataDir()` | `~/.cursor-stat` |

Override via env:

- `CURSOR_STAT_HOME` — our cache/config root
- `CURSOR_USER_DATA` — force Cursor User path (tests)

---

## 10. Concurrency & performance

| Rule | Rationale |
|------|-----------|
| Cache writes go through `store.DB` | SQLite WAL mode plus a 5s busy timeout smooths brief hook/ingest overlap |
| Cursor DB reads use snapshots | `CopySQLiteSnapshot` copies `state.vscdb` + WAL siblings, then opens the copy read-only |
| TUI load work is async | Bubble Tea `tea.Cmd` runs dashboard loads outside `Update`/`View`; `loadGen` ignores stale results |
| Stream JSONL | O(1) memory per file |
| Debounce fsnotify | 500ms coalesce before re-query |

---

## 11. Error handling philosophy

1. **Missing optional sources are empty results** — absent Cursor dirs or transcript folders return no records rather than failing the whole command.
2. **Corrupt/unreadable transcript files are skipped** — one bad JSONL file does not stop ingest.
3. **Fatal exits stay at CLI boundaries** — package code returns errors; `cmd/cursor-stat/main.go` decides whether to print and exit.
4. **Hook errors are non-fatal** — a bad or duplicate hook event should not block Cursor.

---

## 12. Testing strategy

| Level | What |
|-------|------|
| Unit | JSON parsers, path resolution, rollup math |
| Integration | `testdata/minimal/global/state.vscdb` fixture |
| Golden | `--once` JSON output compared to `testdata/golden/*.json` |
| Manual | Run against real Cursor install; compare session count to UI |

---

## 13. Security & privacy

- Bind hook HTTP to `127.0.0.1` only
- Do not include `$HOME` full paths in exported JSON by default (hash or basename)
- `--include-content` flag required to store message snippets
- Document data locations in README for corporate compliance reviews

---

## 14. Module dependency graph (implemented)

```text
cmd/cursor-stat
    → internal/tui
        → internal/aggregate
        → internal/live (+ internal/hooks server)
        → internal/store
        → internal/ingest
    → internal/doctor
    → internal/export
    → internal/cursor/*
    → internal/paths
```

`hooks/cursor-stat-hook.js` — Node hook script POSTing to `127.0.0.1:23556/event`.

### Live + cache paths

```text
Cursor hook OR fsnotify
    → live.Ring (64 events)
    → hooks/server also writes model_choice → stats.db
    → aggregate.Dashboard
    → tui.View

ingest.Run
    → globaldb + transcripts + bubble models
    → store.DB (stats.db), ReplaceToolEventsBySource (atomic)
    → RebuildDailyRollups

TUI tick (2s, skip while ingesting)
    → aggregate.Dashboard (cache + live merge; skip transcript walk if cache has tools)
```
