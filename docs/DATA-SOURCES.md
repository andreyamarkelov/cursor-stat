# cursor-stat — Cursor Data Sources

This document inventories **every known local data location** Cursor uses, what **cursor-stat** can extract, and caveats. All paths are on the **machine running the Cursor UI** (even for SSH remote projects).

> Sources: Cursor/VS Code storage conventions and community reverse-engineering ([cursaves](https://github.com/Callum-Ward/cursaves), [vibe-replay](https://vibe-replay.com/blog/cursor-local-storage/)).

---

## 1. Path cheat sheet

### macOS

| Purpose | Path |
|---------|------|
| User data (VS Code layout) | `~/Library/Application Support/Cursor/User/` |
| Global state DB | `.../globalStorage/state.vscdb` |
| Per-workspace DBs | `.../workspaceStorage/{hash}/state.vscdb` |
| Workspace path map | `.../workspaceStorage/{hash}/workspace.json` |
| Local file history | `.../History/` |
| Dot dir (hooks, projects) | `~/.cursor/` |
| Agent transcripts | `~/.cursor/projects/{sanitized-path}/agent-transcripts/` |
| Chat store DBs | `~/.cursor/chats/**/store.db` |
| Prompt history | `~/.cursor/prompt_history.json` |
| AI code tracking | `~/.cursor/ai-tracking/ai-code-tracking.db` |
| Hooks config | `~/.cursor/hooks.json` |

### Linux

Replace Application Support with `~/.config/Cursor/User/`.

### Windows

Replace with `%APPDATA%\Cursor\User\`.

---

## 2. SQLite: `state.vscdb`

**Format:** SQLite 3, often **WAL mode** (companion `-wal`, `-shm` files).

**Tables:**

| Table | Role |
|-------|------|
| `ItemTable` | Generic key/value (VS Code legacy) |
| `cursorDiskKV` | Cursor-specific key/value (bulk of chat data) |

### 2.1 Important keys (global DB)

| Key pattern | Content | cursor-stat use |
|-------------|---------|-----------------|
| `composer.composerHeaders` | Central session index (Cursor 3.0+) | Session list, workspace linkage |
| `composerData:{uuid}` | JSON metadata: title, timestamps, mode, bubble header list | Counts, titles, activity times |
| `bubbleId:{...}` | JSON message bubbles (user/assistant/tool) | Message counts; optional preview |
| `agentKv:*` | Agent-related KV blobs | Experimental: agent state size |
| `checkpointId:*` | Checkpoint metadata | Compaction / checkpoint frequency |
| `messageRequestContext:*` | Request context snapshots | Size/count only (avoid storing bodies) |

### 2.2 Important keys (workspace DB)

| Key pattern | Content | cursor-stat use |
|-------------|---------|-----------------|
| `composer.composerData` | Legacy workspace-scoped composers | Pre-3.0 installs |
| `workbench.panel.aichat.view.aichat.chatdata` | Legacy chat JSON | Fallback session discovery |
| `workspace.json` (file, not DB) | `{ "folder": "file:///..." }` | Map hash → project path |

### 2.3 Cursor 3.0 migration note

Before 3.0, composer lists lived in **workspace** DBs. After 3.0, **`composer.composerHeaders`** in the **global** DB is authoritative, tagged with `workspaceIdentifier`.

**cursor-stat strategy:** Read global index first; fall back to workspace DBs for older data.

### 2.4 Safe read procedure

```text
NEVER open live DB write mode.
Copy: state.vscdb + state.vscdb-wal + state.vscdb-shm → temp dir
Open temp copy read-only.
If copy/open fails, return the error to the caller or surface it in `doctor`.
```

Current implementation note: `store.CopySQLiteSnapshot` copies the main DB and any `-wal`/`-shm` siblings, then `sqliteutil.OpenReadOnlySnapshot` opens the copy read-only.

---

## 3. Agent transcripts (JSONL / text)

**Path:** `~/.cursor/projects/{sanitized-path}/agent-transcripts/{composerId}.jsonl` (or `.txt`)

| Field (typical) | Use |
|-----------------|-----|
| Timestamp | Event timeline |
| Tool name | Tool breakdown stats |
| Success / error | Failure rate |
| Session / composer id | Correlate with SQLite |

**Properties:**

- Append-only logs; good for **streaming ingest**
- May exist when SQLite row is compressed or migrated
- Cursor may not load UI from these alone — we treat as **analytics source**, not source of truth for message text

---

## 4. Chat store databases (`store.db`)

**Path:** `~/.cursor/chats/{...}/store.db`

| Aspect | Detail |
|--------|--------|
| Count | Can be 100+ files on active machines |
| Size | Hundreds of MB total possible |
| Role | Alternate/composer session storage stack |

**cursor-stat use:** Enumerate count + total size; optional deep parse if global DB missing entries.

---

## 5. Hooks (`~/.cursor/hooks.json`)

Cursor invokes external commands on lifecycle events. Payload (stdin JSON) includes:

| Field | Example | cursor-stat use |
|-------|---------|-----------------|
| `hook_event_name` | `preToolUse`, `beforeSubmitPrompt`, `stop` | Live state |
| `model` | `default`, `claude-opus-4-7` | **Model tracking** on `beforeSubmitPrompt` |
| `conversation_id` / `generation_id` | UUID | Session dedup for model events |
| `cwd` / `workspace_roots` | project path | Workspace |
| `tool_name` | `Shell`, `Read` | Tool stats (live ring) |
| `status` | `error` on stop | Error counting |

**Optional live mode:** `cursor-stat hooks install` adds a marker hook that POSTs to `127.0.0.1:23556/event`.

**Not in hooks:** Full chat history, token counts, billing. Model field reflects picker value (`default` = Auto), not internal routing.

---

## 6. `prompt_history.json`

**Path:** `~/.cursor/prompt_history.json`

User prompt history (if enabled). Useful for:

- Prompts per day count
- **Privacy:** treat as sensitive; off by default in exports

---

## 7. AI code tracking DB

**Path:** `~/.cursor/ai-tracking/ai-code-tracking.db`

Reported to track AI-generated code attribution. Schema is not officially documented.

**cursor-stat today:** not parsed; listed in doctor/storage for size only.

---

## 8. Checkpoints & commits

**Path (example):** `~/Library/Application Support/Cursor/User/globalStorage/anysphere.cursor-commits/checkpoints/`

Checkpoint diffs for agent runs. Useful for:

- Storage growth metrics
- Checkpoint count per day

Not required for basic usage stats.

---

## 9. Process & window signals (OS)

Not files — but valuable for **“now”**:

| Signal | Method |
|--------|--------|
| Cursor running | Match process name `Cursor` / `cursor.exe` |
| PID | For correlation with hook `cursor_pid` |
| Foreground app | Optional; platform-specific |

---

## 10. What is NOT stored locally (usually)

| Data | Note |
|------|------|
| Authoritative billing / quota | Account server-side |
| Guaranteed model name per message | Hook `model` on `beforeSubmitPrompt`; bubble `modelInfo.modelName` (inconsistent) |
| Distinguish Auto vs manual pick | `default`/`auto` vs other model ids (best effort) |
| Cross-device sync | Local DB is this machine only |

UI should label model sections **“best effort / if present locally”**. See [USER-GUIDE.md](USER-GUIDE.md#model-tracking-auto-vs-manual-pick).

---

## 11. Extraction matrix (summary)

| Source | Live | Stats | History | Difficulty |
|--------|------|-------|---------|------------|
| global `state.vscdb` | ◐ (mtime) | ● | ● | High (size, schema) |
| workspace `state.vscdb` | ○ | ● | ● | Medium |
| agent-transcripts | ● | ● | ● | Low |
| hooks (optional) | ● | ● | ◐ | Low |
| `store.db` tree | ○ | ○ (known source, not collected) | ○ | Medium |
| `prompt_history.json` | ○ | ○ (known source, not collected) | ○ | Low |
| ai-tracking.db | ○ | ○ (known source, not collected) | ○ | Unknown |
| OS process | ● | ○ | ○ | Low |

Legend: ● full ◐ partial ○ minimal

---

## 12. Privacy defaults for cursor-stat

| Data | Default in cache | Flag to enable content |
|------|------------------|------------------------|
| Message counts | Yes | — |
| Session titles | Yes | — |
| Tool names | Yes | — |
| Message body text | **No** | `--include-content` |
| Prompt history | **No** | `--include-prompts` |
| File paths in exports | Basename only | `--full-paths` |

---

## 13. Fixture strategy for tests

Never commit real chat content. Generate fixtures:

1. Empty SQLite with correct table DDL
2. Insert synthetic `composerData:` / `bubbleId:` keys with lorem text
3. Small JSONL with fake tool events

Store under `testdata/`.
