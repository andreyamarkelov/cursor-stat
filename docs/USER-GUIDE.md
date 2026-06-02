# cursor-stat — User Guide

**cursor-stat** is a terminal dashboard for [Cursor IDE](https://cursor.com). It reads data stored on your machine and shows what Cursor is doing now, plus usage stats over time. Nothing is sent to the cloud.

---

## Install and first run

```bash
go run ./cmd/cursor-stat ingest    # build local cache (do this once)
go run ./cmd/cursor-stat           # open the dashboard
```

Optional but recommended for live updates:

```bash
go run ./cmd/cursor-stat hooks install
```

Restart Cursor after installing hooks so it loads the new hook entries.

---

## Commands

| Command | What it does |
|---------|----------------|
| `cursor-stat` | Full-screen TUI (default) |
| `cursor-stat --once` | Print a JSON snapshot and exit |
| `cursor-stat ingest` | Refresh `~/.cursor-stat/stats.db` from Cursor files |
| `cursor-stat doctor` | Check paths, DB readability, cache, hooks |
| `cursor-stat export --format csv --days 30` | Export daily rollups |
| `cursor-stat hooks install` | Add live hooks to `~/.cursor/hooks.json` |

### TUI keys

| Key | Action |
|-----|--------|
| `q` | Quit |
| `r` | Force refresh |
| `i` | Run ingest (rebuild cache; UI freezes counts until done) |
| `1`–`5` | Switch tab |

---

## Where data comes from

cursor-stat reads **only local files and processes** on your machine. Nothing is uploaded. Data arrives through three paths:

```text
┌─────────────────────────────────────────────────────────────────────┐
│  CURSOR ON DISK (read-only copies; never modified)                  │
│  • Global DB — session list, titles, message counts, bubble models  │
│  • Agent transcripts — tool names from agent JSONL logs             │
│  • Workspace map — hash → project folder path                       │
└───────────────────────────────┬─────────────────────────────────────┘
                                │  ingest (press i)
                                ▼
                    ~/.cursor-stat/stats.db  (our cache)

┌─────────────────────────────────────────────────────────────────────┐
│  LIVE (while TUI is open; ~every 2 seconds)                         │
│  • Hooks → HTTP localhost:23556 (tools, models, session events)      │
│  • OS process list → is Cursor running?                               │
│  • Filesystem watcher → Cursor files changed                          │
└─────────────────────────────────────────────────────────────────────┘
```

### Cursor files we read

| What | Typical location (macOS) | Used for |
|------|--------------------------|----------|
| **Global state DB** | `~/Library/Application Support/Cursor/User/globalStorage/state.vscdb` | Chat/agent **sessions** (title, workspace, message count, created/updated times). Historical **manual model** names from bubble records (best effort). |
| **Agent transcripts** | `~/.cursor/projects/{project}/agent-transcripts/*.jsonl` | **Tool usage** (`Read`, `Shell`, `Grep`, …) parsed from agent logs. |
| **Workspace storage** | `.../User/workspaceStorage/{hash}/workspace.json` | Maps a workspace id to your **project folder path**. |
| **Hooks config** | `~/.cursor/hooks.json` | Only when you run `hooks install` — tells Cursor to forward events to cursor-stat. |

Linux uses `~/.config/Cursor/User/` instead of Application Support. Windows uses `%APPDATA%\Cursor\User\`.

### Our cache

| What | Location | Used for |
|------|----------|----------|
| **stats.db** | `~/.cursor-stat/stats.db` (override with `CURSOR_STAT_HOME`) | **Today** stats, **History** tab, stable **Tools** totals, **model pick** history. Built by `ingest` / pressing **`i`**. |

### Live path (optional)

| What | How | Used for |
|------|-----|----------|
| **Hook events** | Cursor runs `hooks/cursor-stat-hook.js` → POST to `127.0.0.1:23556` | **Last tool**, **last model**, **LIVE EVENTS** list, live tool counts merged into Tab 3. |
| **Process check** | `pgrep` / `tasklist` for Cursor | Header: **running / stopped** and PID. |
| **File watcher** | Monitors global DB dir and transcript folders | **fs_change** events in LIVE EVENTS when Cursor writes files. |

### What we do **not** read or show

- Chat **message bodies** or prompts (only counts and titles)
- Token usage, billing, or account quota
- `~/.cursor/prompt_history.json`, `ai-tracking.db`, or `~/.cursor/chats/**/store.db` (known locations, not collected today)

For a full technical inventory see [DATA-SOURCES.md](DATA-SOURCES.md).

---

## The dashboard — what everything means

The TUI refreshes about **every 2 seconds**. The **header** and **NOW** panel use live reads; **TODAY**, **History**, and stable tool totals need **ingest** (press **`i`**).

### Top bar (every tab)

| Label | Meaning | Source |
|-------|---------|--------|
| `cursor-stat` | App name | — |
| `Cursor: ● running (pid …)` | Cursor IDE process is running locally | OS process list |
| `Cursor: ○ stopped` | No Cursor process found | OS process list |
| `workspace: …` | Most recently active project folder (truncated) | Workspace map + session activity |

### Footer (every tab)

| Text | Meaning |
|------|---------|
| `q quit  r refresh  i ingest  1-5 tabs` | Keyboard shortcuts |
| `ingesting…` | Cache rebuild in progress; counts stay frozen until done |
| Red error text | Last load or ingest failed (details in message) |

---

## The five tabs (detailed)

### Tab 1 — Overview

Your “at a glance” screen.

#### NOW (live — updates ~every 2s)

| Field | Meaning | Source |
|-------|---------|--------|
| **Last tool** | Most recent agent tool name (`Read`, `Shell`, …) or `-` if none yet | Hooks (if installed) or live file read |
| **Last model** | Model from your last prompt: **Auto** or a specific model id; **(manual)** if you picked something other than Auto | Hook `beforeSubmitPrompt`, or cache after ingest |
| **Last event** | How long ago something happened, plus event kind (e.g. `postToolUse`, `beforeSubmitPrompt`) | Hooks or filesystem watcher |
| **Active chat** | Title of the top recent session and its **message count** | Global Cursor DB (live read) |

#### LIVE EVENTS (needs hooks + TUI running)

Shows the last few events from the live buffer. Each line: **time ago · event kind · detail**.

| Event kind (examples) | Detail column shows |
|------------------------|---------------------|
| `beforeSubmitPrompt` | Model name (Auto or manual) |
| `preToolUse` / `postToolUse` | Tool name |
| `postToolUseFailure` | Tool that failed |
| `sessionStart` / `sessionEnd` | Session lifecycle |
| `fs_change` | Cursor file was written (transcript or DB) |
| `watch_error` | Filesystem watcher problem (rare) |

If this section warns **No live tool stream**, run `cursor-stat hooks install` and restart Cursor.

#### TODAY (cached — press **`i`** after working)

All **today** numbers use **UTC midnight** as the day boundary.

| Field | Meaning | Source |
|-------|---------|--------|
| **Sessions** | Chat/agent sessions **created** today | Ingest → composers in `stats.db` |
| **Messages** | Sum of message counts for sessions **updated** today (activity proxy, not exact chat lines) | Ingest → composers in `stats.db` |
| **Tool calls** | Agent tool invocations today | Ingest → agent transcripts |
| **Tool failures** | Tool calls marked unsuccessful today | Ingest → agent transcripts |
| **Manual models** | Prompts today where you chose a model other than Auto | Hooks + ingest backfill |
| **Auto picks** | Prompts today where model was Auto/default | Hooks + ingest backfill |
| Sparklines (`▁▂▃…`) | Rough **7-day trend** beside Sessions/Messages (not the same metric as the number on that line — visual hint only) | Cached daily rollups |

#### MANUAL MODEL PICKS

Top manual model names **across all cached history** (not just today). Press **`i`** to backfill older picks from Cursor bubble data.

#### RECENT SESSIONS

Up to 5 sessions: **short id · title · message count · last updated**. Same session list as Tab 2, trimmed for Overview.

| Section | Source | Updates |
|---------|--------|---------|
| **NOW** | Live hooks + live file reads | ~every 2s |
| **LIVE EVENTS** | Hook ring buffer | ~every 2s |
| **TODAY** / model picks | `stats.db` cache | After `i` / ingest |
| **RECENT SESSIONS** | Live read of Cursor global DB | ~every 2s |

---

### Tab 2 — Sessions

Table of chat/agent **composers** (Cursor’s name for a chat or agent thread).

| Column | Meaning |
|--------|---------|
| **TITLE** | Session name from Cursor, or `(untitled)` if empty |
| **WORKSPACE** | Project folder (folder name only, truncated) |
| **MSGS** | Message count stored by Cursor for that session |
| **UPDATED** | How long ago the session was last modified (`2m ago`, `3h ago`, …) |
| **ID** | Short session id (first 8 characters) |

Sessions without a title, zero messages, and no recent activity are **hidden** — they are usually idle stubs, not useful rows.

**Source:** live read of Cursor’s global `state.vscdb` (copy-on-read). Press **`i`** to also persist sessions in cache for History rollups.

---

### Tab 3 — Tools

Bar chart of how often each agent **tool** was used (`Read`, `Shell`, `Grep`, `Write`, …).

| Element | Meaning |
|---------|---------|
| **Bar + number** | Count for that tool; bar length is relative to the busiest tool |
| Footer: `N tool calls (cached …)` | Totals from ingested transcripts — stable after **`i`** |
| Footer: `… (M live this session …)` | Current session tools from hooks, merged on top until you ingest |
| Footer: `(live scan …)` | No cache yet; numbers from a one-off transcript scan |

**Source:** primarily `~/.cursor/projects/.../agent-transcripts/*.jsonl` after ingest; hooks add live counts for the current session.

---

### Tab 4 — Storage

Disk usage and health of Cursor data locations and your cache.

| Field | Meaning |
|-------|---------|
| **global state.vscdb** | Size of Cursor’s main SQLite DB + `readable` / `missing` |
| **projects dir** | Total size under `~/.cursor/projects` + `exists` / `missing` |
| **cache (stats.db)** | Size of our local cache + `readable` / `missing` |
| **workspaces** | Number of workspace folders Cursor knows about |
| **transcript files** | Count of `agent-transcripts` JSONL/txt files found |
| **last ingest** | When you last ran ingest successfully (RFC3339 timestamp) |

**Source:** live directory/file stats during dashboard refresh. **`readable`** means cursor-stat could copy-open the DB; if **`missing`**, try quitting Cursor briefly and run `doctor`.

---

### Tab 5 — History

Daily totals for the **last 7 days** (UTC dates).

| Column | Meaning |
|--------|---------|
| **DATE** | Calendar day (`YYYY-MM-DD`, UTC) |
| **SESSIONS** | New sessions **created** that day |
| **MESSAGES** | Sum of composer message counts for sessions **updated** that day |
| **TOOLS** | Agent tool calls that day |
| **FAILURES** | Unsuccessful tool calls that day |

**Source:** `stats.db` daily rollups — rebuild when you press **`i`**. Empty until first ingest.

---

## Live vs cached data

Use this when you wonder why a number is zero or stale:

```text
┌─────────────────────────────────────────────────────────┐
│  LIVE (~2s refresh)                                     │
│  • Header: Cursor running, workspace                    │
│  • Overview NOW: last tool, last model, active chat     │
│  • LIVE EVENTS list                                     │
│  • Tab 2 Sessions (live DB read)                        │
│  • Tab 4 Storage sizes                                  │
│  • Tab 3: extra tool counts from hooks (this session)   │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│  CACHED (press i or run cursor-stat ingest)             │
│  • Overview TODAY + manual model history                  │
│  • Tab 3 stable tool totals                             │
│  • Tab 5 History (7 days)                               │
└─────────────────────────────────────────────────────────┘
```

**Rule of thumb:** keep the TUI open while you work for live signals; press **`i`** when you want today’s totals, history, and tool bars updated from disk.

See [Where data comes from](#where-data-comes-from) and [The dashboard — what everything means](#the-dashboard--what-everything-means) for field-level detail.

---

## Model tracking (Auto vs manual pick)

When hooks are installed, every prompt submission fires Cursor’s `beforeSubmitPrompt` hook. cursor-stat reads the `model` field:

| Cursor sends | Shown as | Counted as |
|--------------|----------|------------|
| `default`, `auto`, empty | **Auto** | Auto pick |
| Anything else (e.g. `claude-opus-4-7`, `gpt-5.5`) | Model name **(manual)** | Manual pick |

On **Overview** you’ll see:

- **Last model** — from the most recent prompt (live) or cache
- **Manual models / Auto picks** — counts for today (UTC)
- **MANUAL MODEL PICKS** — top manual models from cache

**Backfill:** `ingest` also scans Cursor bubble data for past non-`default` model names. This is best-effort — Cursor does not always store the resolved model locally.

**Limits:**

- Auto may route to a specific LLM internally; locally it still looks like **Auto**.
- Sub-variants (e.g. “high thinking”) depend on what Cursor puts in the `model` field.
- No token usage or billing — local files only.

---

## Hooks

cursor-stat listens on `127.0.0.1:23556` (override with `CURSOR_STAT_HOOK_PORT`). The hook script at `hooks/cursor-stat-hook.js` forwards Cursor hook JSON to that port.

Requirements for live data:

1. `cursor-stat hooks install` (once)
2. **TUI running** (`go run ./cmd/cursor-stat`)
3. Cursor restarted after hook install

If **LIVE EVENTS** is empty, run `cursor-stat doctor` and confirm hooks are registered.

---

## Environment variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `CURSOR_USER_DATA` | OS-specific Cursor `User` dir | Point at a non-standard Cursor install |
| `CURSOR_STAT_HOME` | `~/.cursor-stat` | Cache directory for `stats.db` |
| `CURSOR_STAT_HOOK_PORT` | `23556` | Hook HTTP port |

---

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| Empty Tab 3 (Tools) | Run `cursor-stat ingest` or press `i` |
| Tab 3 stuck at low counts while `ingesting…` | Wait for ingest to finish; UI keeps old bars until done |
| `ingesting…` never clears | Check footer for error; restart TUI after updating |
| No live events | Install hooks, restart Cursor, keep TUI open |
| Sessions mostly “(untitled)” | Normal for idle stubs; real sessions have titles or message counts |
| Manual models always 0 | Pick a non-Auto model and send a prompt with hooks + TUI running |
| `doctor` fails on global DB | Quit Cursor briefly or retry — DB may be locked |

---

## Glossary

| Term | Meaning |
|------|---------|
| **Composer** | Cursor’s name for one chat or agent session |
| **Ingest** | One pass that copies Cursor files into `~/.cursor-stat/stats.db` |
| **Auto** | Model picker left on default; Cursor chooses the model internally |
| **Manual model** | You picked a specific model (not Auto) before sending a prompt |
| **Agent transcript** | JSONL log of agent actions under `~/.cursor/projects/.../agent-transcripts/` |
| **Tool** | An agent action like `Read`, `Shell`, or `Grep` — not your keyboard shortcuts |
| **Hook** | A small script Cursor runs on events; ours forwards them to the TUI |
| **Ring buffer** | In-memory list of the last ~64 live events (not persisted until ingest) |

---

## Privacy

- cursor-stat **never writes** to Cursor’s databases.
- It only **appends** marker entries to `~/.cursor/hooks.json` when you run `hooks install`.
- Message bodies are not stored by default — only counts, titles, tool names, and model ids.
- All data stays on your machine under Cursor’s dirs and `~/.cursor-stat/`.

---

## Further reading

| Doc | Audience |
|-----|----------|
| [NOVICE-GUIDE.md](NOVICE-GUIDE.md) | New Go developers — how the code is organized |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Modules, schema, data flow |
| [DATA-SOURCES.md](DATA-SOURCES.md) | Where Cursor stores files on disk |
