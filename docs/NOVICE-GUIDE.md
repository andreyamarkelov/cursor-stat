# cursor-stat — Novice Go Developer Training Guide

A hands-on course that walks through **the real code in this repository**, mostly line by line. By the end you should be able to explain how every package works and confidently make changes.

**Who this is for:** you know basic Go syntax (variables, `if`, `for`, structs, functions) but are new to CLIs, SQLite, terminal UIs, goroutines, and the `internal/` layout.

**How to read it:** open the named file beside this guide and follow along. Each lesson shows the real code with line numbers, then a table explaining each part. Line numbers match the source files at the time of writing; if they drift slightly, search for the function name.

> Just want to *use* the tool? Read [USER-GUIDE.md](USER-GUIDE.md) instead.

---

## Table of contents

1. [The mental model](#1-the-mental-model)
2. [Go concepts you'll meet](#2-go-concepts-youll-meet)
3. [Repository map and reading order](#3-repository-map-and-reading-order)
4. [The big picture: data flow](#4-the-big-picture-data-flow)
5. [Lesson 1 — `main.go`: the entry point](#5-lesson-1--maingo-the-entry-point)
6. [Lesson 2 — `paths.go`: finding Cursor's files](#6-lesson-2--pathsgo-finding-cursors-files)
7. [Lesson 3 — domain types](#7-lesson-3--domain-types)
8. [Lesson 4 — reading Cursor's SQLite safely](#8-lesson-4--reading-cursors-sqlite-safely)
9. [Lesson 5 — the transcripts parser](#9-lesson-5--the-transcripts-parser)
10. [Lesson 6 — the cache store (`stats.db`)](#10-lesson-6--the-cache-store-statsdb)
11. [Lesson 7 — the ingest pipeline](#11-lesson-7--the-ingest-pipeline)
12. [Lesson 8 — the `doctor` command](#12-lesson-8--the-doctor-command)
13. [Lesson 9 — hooks and model tracking](#13-lesson-9--hooks-and-model-tracking)
14. [Lesson 10 — the live ring buffer and watcher](#14-lesson-10--the-live-ring-buffer-and-watcher)
15. [Lesson 11 — `aggregate.Dashboard`](#15-lesson-11--aggregatedashboard)
16. [Lesson 12 — the Bubble Tea TUI](#16-lesson-12--the-bubble-tea-tui)
17. [Concurrency model in one page](#17-concurrency-model-in-one-page)
18. [Testing patterns](#18-testing-patterns)
19. [Glossary](#19-glossary)
20. [Exercises](#20-exercises)

---

## 1. The mental model

Cursor (the IDE) saves your chats, agent sessions, and tool logs **on your own disk**. cursor-stat reads those files and shows statistics. It has two ways of getting data:

1. **Ingest (slow, complete):** parse Cursor's files into our own small database `~/.cursor-stat/stats.db`. This gives stable history.
2. **Live (fast, partial):** receive events from Cursor *hooks* over HTTP, plus cheap signals like "is Cursor running?". This powers the "right now" panel.

A terminal UI (the **TUI**) merges both and redraws every ~2 seconds.

Picture it as a kitchen:

- **Cursor's files** are the pantry (raw ingredients).
- **ingest** is meal prep — you chop everything once and store it in labelled containers (`stats.db`).
- **live hooks** are a window into what's cooking *right now*.
- **the TUI** is the plate you serve every 2 seconds.

---

## 2. Go concepts you'll meet

You don't need to know these up front — each is explained where it appears — but here's the map:

| Concept | One-line meaning | First seen in |
|---------|------------------|---------------|
| `package main` + `func main()` | The program's starting point | Lesson 1 |
| `flag` package | Parse `--once`, `--timeout` from the command line | Lesson 1 |
| `os.Args` | Raw command-line words, e.g. `["cursor-stat","ingest"]` | Lesson 1 |
| `error` as a value | Functions return errors; you check them with `if err != nil` | everywhere |
| `defer` | "Run this when the function returns" (cleanup) | Lesson 1 |
| `context.Context` | Carries deadlines/cancellation through calls | Lesson 1 |
| `os.UserHomeDir`, `runtime.GOOS` | Find the home dir; detect the OS | Lesson 2 |
| `struct` + JSON tags | Data shapes; `json:"..."` maps fields to JSON keys | Lesson 3 |
| `database/sql` | Go's standard DB interface | Lesson 4 |
| pointer receiver `func (db *DB)` | A method that can mutate its receiver | Lesson 6 |
| `goroutine` (`go f()`) | Run `f` concurrently | Lesson 9 |
| `sync.RWMutex` | Lock to share data safely across goroutines | Lesson 9 |
| `channel` + `select` | Goroutines communicate / wait on events | Lesson 9 |
| Bubble Tea `Model/Update/View` | The Elm-style TUI pattern | Lesson 11 |
| table tests | One test function, many `{input,want}` rows | Lesson 17 |

---

## 3. Repository map and reading order

```text
cmd/
  cmd/cursor-stat/main.go      entry point: flags, subcommands, starts the TUI
internal/
  paths/paths.go               where Cursor stores files (per OS)
  cursor/
    types.go                   shared structs: ComposerMeta, ToolEvent, LiveSnapshot…
    dashboard_types.go         structs the TUI renders: Dashboard, DailyRollup…
    model.go                   model-name helpers: IsAutoModel, NormalizeModel
    sqliteutil/open.go         open a Cursor SQLite copy read-only
    globaldb/globaldb.go       read chat sessions from state.vscdb
    globaldb/modelchoices.go   read historical model names from bubbles
    transcripts/transcripts.go parse agent-transcripts/*.jsonl for tool usage
    workspacedb/workspacedb.go map workspace hash → project folder
  store/
    db.go                      our cache DB: schema + queries
    snapshot.go                copy a SQLite file safely (with -wal/-shm)
    fingerprint.go             detect "did the transcripts change?"
  ingest/ingest.go             the pipeline that fills stats.db
  hooks/
    parse.go                   decode a hook JSON payload
    server.go                  HTTP server on 127.0.0.1:23556
    install.go                 add our hook to ~/.cursor/hooks.json
  live/
    live.go                    is Cursor running? which workspace?
    ring.go                    fixed-size buffer of recent events
    watcher.go                 fsnotify file-change watcher
  aggregate/
    snapshot.go                one-shot read of Cursor files (for --once)
    dashboard.go               merge cache + live into one Dashboard
  doctor/doctor.go             health checks (diagnostics)
  export/export.go             CSV / JSON export
```

**Suggested order:** `main.go` → `paths.go` → `types.go` → `store/db.go` → `ingest.go` → `doctor/doctor.go` → the collectors (`globaldb`, `transcripts`) → `hooks` → `live` → `aggregate/dashboard.go` → `tui/model.go`. That's exactly the order of the lessons below.

---

## 4. The big picture: data flow

```text
                    ┌──────────────────────┐
                    │   Cursor on disk     │
                    │ state.vscdb, JSONL,  │
                    │ ~/.cursor/hooks.json │
                    └──────────┬───────────┘
                               │
        ingest (press i)       │        live (every ~2s)
                    ▼          │             ▼
            ┌───────────┐      │   ┌──────────────────┐
            │ stats.db  │      │   │   live.Ring      │ ◄── hooks POST :23556
            │  (cache)  │      │   │  (last 64 events)│
            └─────┬─────┘      │   └────────┬─────────┘
                  │            │            │
                  └─────┬──────┴────────────┘
                        ▼
            aggregate.Dashboard(ctx, db, ring)   ← merges both sides
                        ▼
              internal/tui  Update → View        ← redraws the screen
```

Keep this diagram in mind. Every lesson is one box in it.

---

## 5. Lesson 1 — `main.go`: the entry point

File: `cmd/cursor-stat/main.go`. This is where execution begins. Read it top to bottom.

### 5.1 Package and imports

```go
 1| package main
 2|
 3| import (
 4|     "context"
 5|     "encoding/json"
 6|     "flag"
 7|     "fmt"
 8|     "log"
 9|     "os"
10|     "path/filepath"
11|     "time"
12|
13|     "github.com/cursor-stat/cursor-stat/internal/aggregate"
14|     "github.com/cursor-stat/cursor-stat/internal/doctor"
15|     exportdata "github.com/cursor-stat/cursor-stat/internal/export"
16|     "github.com/cursor-stat/cursor-stat/internal/hooks"
17|     "github.com/cursor-stat/cursor-stat/internal/ingest"
18|     "github.com/cursor-stat/cursor-stat/internal/live"
19|     "github.com/cursor-stat/cursor-stat/internal/store"
20|     "github.com/cursor-stat/cursor-stat/internal/tui"
21| )
```

| Line(s) | Explanation |
|---------|-------------|
| 1 | `package main` is special: it tells Go this folder compiles to an **executable**, not a library. Every runnable Go program has exactly one `package main` with a `func main()`. |
| 3–11 | **Standard library** imports. `flag` parses CLI options; `log` prints fatal errors; `os` gives args and exit; `encoding/json` prints snapshots; `context`/`time` handle deadlines. |
| 13–20 | **Our own packages** under `internal/`. The `internal/` prefix is a Go rule: only code inside this module may import them, so they can't leak into other projects. |
| 15 | `exportdata "…/export"` is an **import alias**. The package is named `exportdata`; the alias just makes that explicit at the call site. |

### 5.2 `main()` — subcommand dispatch then flags

```go
23| func main() {
24|     if len(os.Args) > 1 {
25|         switch os.Args[1] {
26|         case "ingest":
27|             runIngest(os.Args[2:])
28|             return
29|         case "doctor":
30|             runDoctor()
31|             return
32|         case "export":
33|             runExport(os.Args[2:])
34|             return
35|         case "hooks":
36|             runHooks(os.Args[2:])
37|             return
38|         case "help", "-h", "--help":
39|             printUsage()
40|             return
41|         }
42|     }
43|
44|     fs := flag.NewFlagSet("cursor-stat", flag.ExitOnError)
45|     once := fs.Bool("once", false, "print JSON snapshot and exit")
46|     noTUI := fs.Bool("no-tui", false, "never start interactive UI")
47|     timeout := fs.Duration("timeout", 30*time.Second, "max collection time")
48|     _ = fs.Parse(os.Args[1:])
49|
50|     ctx, cancel := context.WithTimeout(context.Background(), *timeout)
51|     defer cancel()
52|
53|     if *once || *noTUI {
54|         runOnce(ctx)
55|         return
56|     }
57|     runInteractive()
58| }
```

| Line(s) | Explanation |
|---------|-------------|
| 24 | `os.Args` is the list of command-line words. `os.Args[0]` is the program name, so `len > 1` means the user typed something after it. |
| 25–41 | A **subcommand router**. If the first word is `ingest`/`doctor`/etc., we call that handler and `return` — the TUI never starts. `os.Args[2:]` passes the *remaining* words to the handler (its own flags). |
| 38 | One `case` can match several values. `help`, `-h`, and `--help` all print usage. |
| 44 | If no subcommand matched, we treat the args as flags for the **default** (TUI) mode. `NewFlagSet` makes an isolated parser; `ExitOnError` means "print usage and exit if a flag is wrong". |
| 45–47 | Declare three flags. `fs.Bool` returns a `*bool` (pointer); the value isn't filled in until `Parse` runs. `Duration` accepts strings like `30s` or `2m`. |
| 48 | `fs.Parse` reads the flags. `_ =` discards the returned error because `ExitOnError` already handles bad input. |
| 50–51 | Build a `context` that auto-cancels after `timeout`. `defer cancel()` guarantees we release its resources when `main` returns. **Pattern to memorise:** create context, immediately `defer cancel()`. |
| 53–57 | `--once`/`--no-tui` print a JSON snapshot (good for scripts/CI). Otherwise launch the interactive dashboard. |

> **Why two layers (subcommand, then flags)?** Subcommands like `ingest` are verbs with their own options; the bare program with `--once` is the default behaviour. Splitting them keeps `main` readable without a heavy CLI framework.

### 5.3 `runOnce` — the simplest path

```go
60| func runOnce(ctx context.Context) {
61|     snap, err := aggregate.Snapshot(ctx)
62|     if err != nil {
63|         log.Fatalf("snapshot: %v", err)
64|     }
65|     enc := json.NewEncoder(os.Stdout)
66|     enc.SetIndent("", "  ")
67|     if err := enc.Encode(snap); err != nil {
68|         log.Fatalf("encode: %v", err)
69|     }
70| }
```

| Line(s) | Explanation |
|---------|-------------|
| 61 | `aggregate.Snapshot` reads Cursor files **right now** and returns a struct (Lesson 10/`snapshot.go`). It returns two values: the data and an `error`. |
| 62–64 | The Go error convention: check `err != nil`. `log.Fatalf` prints the message and calls `os.Exit(1)`. Use it only in `main`-level code, never in libraries. |
| 65–66 | Make a JSON encoder writing to standard output, with 2-space indentation for readability. |
| 67 | `Encode` turns the struct into JSON. This is why the structs in `types.go` have `json:"..."` tags — they control the output keys. |

This is the **best function to study first** because it has no concurrency: read → encode → print.

### 5.4 `runInteractive` — wiring the live system

```go
72| func runInteractive() {
73|     ctx := context.Background()
74|     db, err := store.OpenDefault()
75|     if err != nil {
76|         log.Fatalf("store: %v", err)
77|     }
78|     defer db.Close()
79|     _ = db.RepairInvalidToolTimestamps()
80|     _ = db.RebuildDailyRollups()
81|
82|     ring := live.NewRing(64)
83|     bg, cancel := context.WithCancel(context.Background())
84|     defer cancel()
85|
86|     go func() { _ = hooks.NewServer(ring, db, hooks.DefaultPort).Start(bg) }()
87|     go func() { _ = live.NewWatcher(ring).Run(bg) }()
88|
89|     if err := tui.Run(ctx, db, ring); err != nil {
90|         log.Fatalf("tui: %v", err)
91|     }
92| }
```

| Line(s) | Explanation |
|---------|-------------|
| 73 | `context.Background()` is the "root" context — no deadline. The TUI should run as long as the user wants, so we don't time it out. |
| 74–78 | Open (or create) our cache DB. `defer db.Close()` ensures it's closed on exit. |
| 79–80 | Two cheap maintenance calls at startup: fix any bad timestamps from older versions, and recompute daily totals so the History tab is correct immediately. The `_ =` ignores errors here on purpose — they're non-fatal. |
| 82 | Create the **ring buffer** that holds the last 64 live events (Lesson 9). |
| 83–84 | A separate **cancellable** context for background goroutines. When `runInteractive` returns, `cancel()` tells them to stop. |
| 86 | Start the **hook HTTP server** in a goroutine (`go func(){...}()`). It shares the same `db` and `ring`, so incoming hook events update both the cache and the live view. |
| 87 | Start the **filesystem watcher** in another goroutine. |
| 89 | `tui.Run` **blocks** here until the user quits. The goroutines keep feeding `ring` while it runs. |

> **Key insight:** the hook server gets the *same* `db` pointer as the TUI. That's why a model choice from `beforeSubmitPrompt` is saved immediately — not only when you press `i`.

The remaining functions (`runIngest`, `runDoctor`, `runExport`, `runHooks`, `printUsage`) are small and follow the same "open DB → call a package → print" shape. Read them once; they reinforce the pattern.

---

## 6. Lesson 2 — `paths.go`: finding Cursor's files

File: `internal/paths/paths.go`. This is the easiest package — pure string building, no I/O except checking the home directory. Great for confidence.

```go
11| func CursorUserData() (string, error) {
12|     if override := os.Getenv("CURSOR_USER_DATA"); override != "" {
13|         return override, nil
14|     }
15|     home, err := os.UserHomeDir()
16|     if err != nil {
17|         return "", fmt.Errorf("home dir: %w", err)
18|     }
19|     switch runtime.GOOS {
20|     case "darwin":
21|         return filepath.Join(home, "Library", "Application Support", "Cursor", "User"), nil
22:     case "linux":
23:         return filepath.Join(home, ".config", "Cursor", "User"), nil
24:     case "windows":
25:         return filepath.Join(home, "AppData", "Roaming", "Cursor", "User"), nil
26|     default:
27|         return "", fmt.Errorf("unsupported GOOS %q", runtime.GOOS)
28|     }
29| }
```

| Line(s) | Explanation |
|---------|-------------|
| 11–13 | **Environment override first.** If `CURSOR_USER_DATA` is set, use it. Tests rely on this to point at a fake directory without touching your real Cursor data. |
| 14–17 | `os.UserHomeDir()` finds the user's home. `fmt.Errorf("…: %w", err)` **wraps** the original error; the `%w` verb lets callers unwrap it later. Always add context when returning errors. |
| 18 | `runtime.GOOS` is a constant set at build time: `"darwin"` (macOS), `"linux"`, or `"windows"`. |
| 19–24 | `filepath.Join` builds OS-correct paths (it uses `\` on Windows, `/` elsewhere). Never concatenate paths with `+`. |
| 25–27 | Defensive default: unknown OS returns an error rather than a wrong path. `%q` quotes the value in the message. |

The other functions (`GlobalStateDB`, `WorkspaceStorageDir`, `ProjectsDir`, `StatDataDir`) just build on `CursorUserData` / `CursorDotDir`. Note `StatDataDir` also **creates** the directory (`os.MkdirAll(dir, 0o700)`) because we own it; `0o700` means "owner read/write/execute only".

> **Takeaway:** centralising paths in one package means there's exactly **one** place to fix if Cursor moves a file or you add Windows support.

---

## 7. Lesson 3 — domain types

Files: `internal/cursor/types.go` and `internal/cursor/dashboard_types.go`. These define the **shapes of data** that flow between packages. No logic — just structs. Read them so later code makes sense.

```go
// internal/cursor/types.go
 6| type ComposerMeta struct {
 7|     ID            string         `json:"id"`
 8|     WorkspaceID   string         `json:"workspace_id,omitempty"`
 9|     WorkspacePath string         `json:"workspace_path,omitempty"`
10|     Title         string         `json:"title,omitempty"`
11|     CreatedAt     time.Time      `json:"created_at,omitempty"`
12|     UpdatedAt     time.Time      `json:"updated_at,omitempty"`
13|     MessageCount  int            `json:"message_count"`
14|     ToolCounts    map[string]int `json:"tool_counts,omitempty"`
15|     Source        string         `json:"source"`
16| }
```

| Element | Explanation |
|---------|-------------|
| `ComposerMeta` | One Cursor "composer" = a chat/agent session. This is our normalised view of it. |
| `` `json:"id"` `` | A **struct tag**. When we encode to JSON, the field `ID` becomes `"id"`. Tags are how `encoding/json` maps Go names ↔ JSON keys. |
| `,omitempty` | Skip the field in JSON output if it's empty/zero. Keeps snapshots tidy. |
| `map[string]int` | `ToolCounts` maps a tool name to a count. |
| `Source` | Where this record came from (e.g. `globaldb:composerHeaders`) — useful for debugging. |

Other important types in the same file:

- **`ToolEvent`** — one normalised tool call (`At`, `ToolName`, `Success`, `SessionID`, `Source`). Produced by the transcripts parser and the hook server.
- **`LiveSnapshot`** — the "right now" panel: `CursorRunning`, `ActiveWorkspace`, `LastTool`, `LastModel`, `LastModelManual`, etc.
- **`ToolBreakdown`** — `Total`, `Failures`, and `ByTool map[string]int` for the Tools tab.
- **`Snapshot`** — the top-level object printed by `--once`.

And in `dashboard_types.go`:

- **`Dashboard`** — everything the TUI needs in one struct (live + today + history + composers + tools + models).
- **`DailyRollup`** — one day's totals for the History tab.
- **`LiveEvent`** — one entry in the ring buffer (`Kind`, `Tool`, `Model`, `Manual`, `At`).
- **`ModelBreakdown`** / **`ModelChoiceEvent`** — model-tracking types (see Lesson 8).

`internal/cursor/model.go` adds two tiny helpers you'll see everywhere model handling happens:

```go
27| func IsAutoModel(model string) bool {
28|     switch strings.ToLower(strings.TrimSpace(model)) {
29|     case "", "default", "auto", "automatic":
30|         return true
31|     }
32|     return false
33| }
```

| Line(s) | Explanation |
|---------|-------------|
| 28 | Normalise first: lower-case and trim spaces so `"Default"` and `" default "` both match. |
| 29–30 | Cursor reports the Auto picker as `default`/`auto`/empty. We treat all of these as "the user did **not** manually choose a model". |
| `NormalizeModel` | (just below) returns `"Auto"` for those, or the raw model id otherwise — used for display. |

---

## 8. Lesson 4 — reading Cursor's SQLite safely

Files: `internal/store/snapshot.go` and `internal/cursor/sqliteutil/open.go`.

**The problem:** Cursor keeps `state.vscdb` open in **WAL mode** (Write-Ahead Logging). There are sibling files `state.vscdb-wal` and `state.vscdb-shm`. If we open the live file we might read half-written data or hit a lock. **The rule: never open Cursor's DB directly — copy it first.**

### 8.1 Copying the database + its WAL siblings

```go
// internal/store/snapshot.go
12| func CopySQLiteSnapshot(basePath, destDir string) (string, error) {
13|     if err := os.MkdirAll(destDir, 0o700); err != nil {
14|         return "", err
15|     }
16|
17|     files := []string{basePath, basePath + "-wal", basePath + "-shm"}
18|     for _, src := range files {
19|         if _, err := os.Stat(src); os.IsNotExist(err) {
20|             continue
21|         } else if err != nil {
22|             return "", fmt.Errorf("stat %s: %w", src, err)
23|         }
24|         dest := filepath.Join(destDir, filepath.Base(src))
25|         if err := copyFile(src, dest); err != nil {
26|             return "", fmt.Errorf("copy %s: %w", src, err)
27|         }
28|     }
29|
30|     destBase := filepath.Join(destDir, filepath.Base(basePath))
31|     if _, err := os.Stat(destBase); err != nil {
32|         return "", fmt.Errorf("snapshot missing main db: %w", err)
33|     }
34|     return destBase, nil
35| }
```

| Line(s) | Explanation |
|---------|-------------|
| 17 | We must copy **all three** files together — the `-wal` file holds recent writes not yet folded into the main DB. Copying only the main file could lose data. |
| 19–23 | `os.Stat` checks existence. `os.IsNotExist(err)` → the `-wal`/`-shm` may legitimately be absent, so we `continue`. Any *other* stat error is real, so we return it. |
| 24–27 | `filepath.Base(src)` keeps just the filename so the copy lands in `destDir`. |
| 30–33 | Sanity check: the main DB copy must exist, otherwise later queries would fail confusingly. |

### 8.2 Opening the copy read-only

```go
// internal/cursor/sqliteutil/open.go
14| func OpenReadOnlySnapshot(dbPath string) (db *sql.DB, cleanup func(), err error) {
15|     tmp, err := os.MkdirTemp("", "cursor-stat-sqlite-*")
16|     if err != nil {
17|         return nil, nil, err
18|     }
19|     cleanup = func() { _ = os.RemoveAll(tmp) }
20|
21|     copyPath, err := store.CopySQLiteSnapshot(dbPath, tmp)
22|     if err != nil {
23|         cleanup()
24|         return nil, nil, err
25|     }
26|
27|     dsn := fmt.Sprintf("file:%s?mode=ro", copyPath)
28|     db, err = sql.Open("sqlite", dsn)
29|     if err != nil {
30|         cleanup()
31|         return nil, nil, err
32|     }
33|     if _, err := db.Exec(`PRAGMA query_only = ON`); err != nil {
34|         _ = db.Close()
35|         cleanup()
36|         return nil, nil, err
37|     }
38|     return db, cleanup, nil
39| }
```

| Line(s) | Explanation |
|---------|-------------|
| 14 | **Named returns** `(db, cleanup, err)`. The function hands back a `cleanup` function so the caller can delete the temp copy when done. |
| 15 | `os.MkdirTemp` makes a unique temp dir; the `*` is replaced with random characters. Unique names avoid clashes between concurrent reads. |
| 19 | `cleanup` is a **closure** capturing `tmp`. Calling it removes the whole temp dir. |
| 21–25 | Copy into temp. On error we call `cleanup()` *before* returning so we don't leak the dir. |
| 27–28 | The **DSN** (data source name) tells the driver to open `file:...` in `mode=ro` (read-only). `sql.Open` doesn't actually connect yet — it prepares the pool. |
| 33 | `PRAGMA query_only = ON` is a belt-and-braces guarantee: even if something tried to write, SQLite refuses. |
| 38 | Return the open DB and the `cleanup`. **The caller must `defer cleanup()` and `defer db.Close()`** (see how `globaldb` uses it). |

`TableExists` below queries `sqlite_master` so collectors can feature-detect tables (Cursor's schema changes between versions).

> **Pattern:** "copy → open read-only → query → cleanup". Any time you must read a file another process owns, copy first.

---

## 9. Lesson 5 — the transcripts parser

File: `internal/cursor/transcripts/transcripts.go`. This turns Cursor's agent log files into `ToolEvent`s. It's a great example of **streaming file parsing** and **tolerant JSON decoding**.

### 9.1 Walking the directory

```go
23| func (c *Collector) Collect(ctx context.Context) ([]cursor.ToolEvent, int, error) {
24|     if c.ProjectsDir == "" {
25|         return nil, 0, nil
26|     }
27|     if _, err := os.Stat(c.ProjectsDir); os.IsNotExist(err) {
28|         return nil, 0, nil
29|     } else if err != nil {
30|         return nil, 0, err
31|     }
32|
33|     var out []cursor.ToolEvent
34|     files := 0
35|     err := filepath.WalkDir(c.ProjectsDir, func(path string, d os.DirEntry, walkErr error) error {
36|         if walkErr != nil {
37|             return walkErr
38|         }
39|         if ctx.Err() != nil {
40|             return ctx.Err()
41|         }
42|         if d.IsDir() {
43|             return nil
44|         }
45|         name := d.Name()
46:         if !strings.HasSuffix(name, ".jsonl") && !strings.HasSuffix(name, ".txt") {
47:             return nil
48:         }
49:         if !strings.Contains(path, string(filepath.Separator)+"agent-transcripts"+string(filepath.Separator)) {
50:             return nil
51:         }
52|         files++
53|         events, err := parseFile(path)
54|         if err != nil {
55|             return nil // skip unreadable files
56|         }
57|         out = append(out, events...)
58|         return nil
59|     })
60|     return out, files, err
61| }
```

| Line(s) | Explanation |
|---------|-------------|
| 23–30 | **Graceful absence.** No directory configured, or it doesn't exist → return empty, no error. Missing data is normal, not a failure. |
| 34 | `filepath.WalkDir` visits every file/dir under the root, calling our function for each. The function is a **closure** so it can append to `out`. |
| 38–40 | Honour cancellation. If the `context` was cancelled (timeout), stop walking by returning its error. |
| 41–43 | Skip directories — we only parse files. |
| 45–50 | Two filters: the file must be `.jsonl`/`.txt` **and** sit inside an `agent-transcripts/` folder. This avoids parsing unrelated files. |
| 52–55 | Parse one file. If it can't be read, we **skip it** (return `nil`) instead of aborting the whole walk — one bad file shouldn't kill ingest. |
| 56 | `append(out, events...)` — the `...` spreads the slice so we append each element. |
| 59 | Return all events, the file count, and any walk error. Returning **three values** is idiomatic Go. |

### 9.2 Streaming a file line by line

```go
63| func parseFile(path string) ([]cursor.ToolEvent, error) {
64|     info, err := os.Stat(path)
65|     if err != nil {
66|         return nil, err
67|     }
68|     f, err := os.Open(path)
69|     if err != nil {
70|         return nil, err
71|     }
72|     defer f.Close()
73|
74|     workspace := workspaceFromPath(path)
75|     sessionID := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
76|     fileTime := info.ModTime()
77|
78|     var events []cursor.ToolEvent
79|     scanner := bufio.NewScanner(f)
80|     scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
81|     lineNo := 0
82|     for scanner.Scan() {
83|         line := scanner.Bytes()
84|         if len(line) == 0 {
85|             continue
86|         }
87|         lineNo++
88|         events = append(events, parseLine(line, sessionID, workspace, fileTime, lineNo)...)
89|     }
90|     return events, scanner.Err()
91| }
```

| Line(s) | Explanation |
|---------|-------------|
| 64–67 | `os.Stat` gives file metadata; we want `ModTime()` (line 76) to assign timestamps to events that lack one. |
| 68–72 | Open the file; `defer f.Close()` runs when the function returns — even on early return. |
| 74–75 | Derive the workspace from the path and the session id from the filename (strip the extension). |
| 79–80 | A `bufio.Scanner` reads **one line at a time** — constant memory even for huge files. `Buffer(..., 1024*1024)` raises the max line size to 1 MB (transcript lines can be long). |
| 82–89 | The scan loop. Skip blank lines; track `lineNo` (used to order events that share a file). |
| 90 | `scanner.Err()` reports any read error that ended the loop. |

### 9.3 Tolerant JSON decoding (two formats)

```go
93| func parseLine(line []byte, sessionID, workspace string, fileTime time.Time, lineNo int) []cursor.ToolEvent {
94|     // Cursor agent-transcripts: {"role":"assistant","message":{"content":[{"type":"tool_use","name":"Read",...}]}}
95|     var assistant struct {
96|         Role    string `json:"role"`
97|         Message struct {
98|             Content []struct {
99|                 Type string `json:"type"`
100|                Name string `json:"name"`
101|            } `json:"content"`
102|        } `json:"message"`
103|    }
104|    if err := json.Unmarshal(line, &assistant); err == nil && assistant.Role == "assistant" {
105|        var out []cursor.ToolEvent
106|        for i, part := range assistant.Message.Content {
107|            if part.Type != "tool_use" || part.Name == "" {
108|                continue
109|            }
110|            out = append(out, cursor.ToolEvent{
111|                At:        eventTime(fileTime, lineNo, i),
112|                SessionID: sessionID,
113|                ToolName:  part.Name,
114|                Success:   true,
115|                Workspace: workspace,
116|                Source:    "transcript",
117|            })
118|        }
119|        if len(out) > 0 {
120|            return out
121|        }
122|    }
123|    // …fallback to legacy flat JSON below…
```

| Line(s) | Explanation |
|---------|-------------|
| 95–103 | An **anonymous struct** describing *only the fields we need* from Cursor's real format. You don't have to model the whole giant JSON — just the path to `content[].type` and `content[].name`. |
| 104 | `json.Unmarshal` parses the line into our struct. We proceed only if it parsed **and** it's an assistant message. |
| 106–109 | Loop the content parts; keep only `tool_use` entries with a name. `range` gives index `i` and value `part`. |
| 110–117 | Build a `ToolEvent`. `eventTime(fileTime, lineNo, i)` synthesises a sensible timestamp (file mtime + line + part offset) because these records don't carry their own time. |
| 119–121 | If we found tool uses, return them. Otherwise fall through to the legacy parser (lines 124+), which handles a simpler `{"tool_name":...}` shape used by tests and older logs. |

> **Lesson:** decode defensively. Model the minimum, skip what you don't understand, and support more than one format when a tool's files evolve.

The helpers `firstString` (try several JSON keys), `parseTime` (try RFC3339 variants), and `eventTime` (synthesise a timestamp) are small and worth reading. `Breakdown` and `Latest` at the bottom aggregate events.

---

## 10. Lesson 6 — the cache store (`stats.db`)

File: `internal/store/db.go`. This is our **own** SQLite database (separate from Cursor's). We fully control it.

### 10.1 Opening and the `DB` wrapper

```go
21| type DB struct {
22|     sql *sql.DB
23| }
24|
26| func OpenDefault() (*DB, error) {
27|     dir, err := paths.StatDataDir()
28|     if err != nil {
29|         return nil, err
30|     }
31|     return Open(filepath.Join(dir, "stats.db"))
32| }
33|
35| func Open(path string) (*DB, error) {
36|     if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
37|         return nil, err
38|     }
39|     dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)
40|     sqlDB, err := sql.Open("sqlite", dsn)
41|     if err != nil {
42|         return nil, err
43|     }
44|     db := &DB{sql: sqlDB}
45|     if err := db.Migrate(); err != nil {
46|         _ = sqlDB.Close()
47|         return nil, err
48|     }
49|     return db, nil
50| }
```

| Line(s) | Explanation |
|---------|-------------|
| 21–23 | We **wrap** the standard `*sql.DB` in our own `DB` type. The field is lowercase `sql`, so it's private — outside packages must use our methods, not raw SQL. This is encapsulation. |
| 26–32 | `OpenDefault` resolves `~/.cursor-stat/` then opens `stats.db` inside it. |
| 36 | Make sure the parent directory exists. |
| 39 | The DSN sets two pragmas: `busy_timeout(5000)` waits up to 5s if the DB is briefly locked, and `journal_mode(WAL)` gives better concurrency. |
| 44–49 | Build the wrapper and run `Migrate()`. If migration fails we close the handle and report the error (don't return a half-initialised DB). |

### 10.2 Migrations: create tables if missing

```go
60| func (db *DB) Migrate() error {
61|     stmts := []string{
62|         `CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`,
...
82|         `CREATE TABLE IF NOT EXISTS events (
83|             id INTEGER PRIMARY KEY AUTOINCREMENT,
84|             at INTEGER NOT NULL,
85|             session_id TEXT,
86|             workspace TEXT,
87|             kind TEXT NOT NULL,
88|             tool_name TEXT,
89|             success INTEGER,
90|             source TEXT NOT NULL,
91|             meta_json TEXT
92|         )`,
93|         `CREATE UNIQUE INDEX IF NOT EXISTS idx_events_dedup
94|          ON events(source, session_id, at, kind, COALESCE(tool_name, ''))`,
...
108|    }
109|    for _, s := range stmts {
110|        if _, err := db.sql.Exec(s); err != nil {
111|            return fmt.Errorf("migrate: %w", err)
112|        }
113|    }
```

| Line(s) | Explanation |
|---------|-------------|
| 61–108 | A slice of `CREATE TABLE IF NOT EXISTS` statements. Running them every startup is safe and means "the schema exists" without a separate setup step. |
| 82–92 | The **`events`** table is the heart of the cache. One row per thing-that-happened. `kind` distinguishes `tool`, `model_choice`, and `session_start`. `tool_name` doubles as the model id for model choices; `success` doubles as `1=manual / 0=auto`. Reusing columns keeps the schema tiny. |
| 93–94 | A **unique index** for de-duplication. Inserting the same `(source, session_id, at, kind, tool_name)` twice is ignored, so re-ingesting is safe (idempotent). `COALESCE(tool_name,'')` treats NULL as `''` so the index works when there's no tool name. |
| 109–112 | Execute each statement; wrap any error with context. |

### 10.3 Inserting an event (idempotent)

```go
200| func (db *DB) InsertEvent(at time.Time, sessionID, workspace, kind, toolName, source string, success *bool) (bool, error) {
201|    var succ sql.NullInt64
202|    if success != nil {
203|        if *success {
204|            succ = sql.NullInt64{Int64: 1, Valid: true}
205|        } else {
206|            succ = sql.NullInt64{Int64: 0, Valid: true}
207|        }
208|    }
209|    res, err := db.sql.Exec(`
210|        INSERT OR IGNORE INTO events(at, session_id, workspace, kind, tool_name, success, source)
211|        VALUES (?, ?, ?, ?, ?, ?, ?)
212|    `, at.UnixNano(), sessionID, workspace, kind, toolName, succ, source)
213|    if err != nil {
214|        return false, err
215|    }
216|    n, _ := res.RowsAffected()
217|    return n > 0, nil
218| }
```

| Line(s) | Explanation |
|---------|-------------|
| 200 | `success *bool` is a **pointer** so it can be `nil` ("unknown"). That's how Go expresses an optional value. |
| 201–208 | `sql.NullInt64` represents a column that may be NULL. `Valid:true` means "I have a value"; `Valid:false` (the zero value) means NULL. |
| 209–212 | `INSERT OR IGNORE` + the unique index = duplicates are silently dropped. The `?` placeholders are **parameters** — never build SQL by string concatenation (that invites injection bugs). `at.UnixNano()` stores time as an integer. |
| 216–217 | `RowsAffected()` tells us whether a row was actually inserted (`>0`) or ignored as a duplicate. We return that as a `bool` so callers can count new vs skipped. |

Other methods you'll meet: `UpsertComposer`, `RebuildDailyRollups` (recomputes the `daily_rollups` table with `GROUP BY` queries), `ToolBreakdownAll`, `ModelBreakdownAll`, `InsertModelChoice`, `ReplaceToolEventsBySource` (atomic swap in a transaction — Lesson 7), and `TodayStats`.

`internal/store/fingerprint.go` adds `DirFingerprint`: it hashes the relative path + size + mtime of every transcript file. If the hash is unchanged since last ingest, we skip re-reading them. That's how `ingest` stays fast.

---

## 11. Lesson 7 — the ingest pipeline

File: `internal/ingest/ingest.go`. This orchestrates the collectors and writes to the cache. It's the "meal prep" step.

```go
22| func Run(ctx context.Context, db *store.DB) (cursor.IngestResult, error) {
23|     result := cursor.IngestResult{CompletedAt: time.Now().UTC()}
24|
25|     if err := db.RepairInvalidToolTimestamps(); err != nil {
26|         return result, err
27|     }
28|     // …resolve workspace map + global DB path…
43|     if need, err := db.NeedsIngest(sourceGlobalDB, globalPath); err == nil && need {
44|         composers, err := globaldb.ReadComposers(globalPath, wsMap)
45|         if err != nil {
46|             return result, fmt.Errorf("globaldb: %w", err)
47|         }
48|         for _, c := range composers {
49|             if err := db.UpsertComposer(c); err != nil {
50|                 return result, err
51|             }
52|             result.ComposersUpserted++
53|             ok, err := insertSessionStart(db, c)
            // …count inserted vs skipped…
62|         }
            // …read bubble model choices, insert each…
            // …MarkIngested(sourceGlobalDB, globalPath); SourcesUpdated++…
79|     }
```

| Line(s) | Explanation |
|---------|-------------|
| 23 | Build a `result` struct we fill in as we go (counts of what we did). |
| 25–27 | Cheap repair of legacy bad rows before anything else. |
| 43 | `NeedsIngest` compares the global DB's size+mtime to what we stored last time. If unchanged, the whole block is **skipped** — that's the idempotency optimisation. |
| 44–47 | `globaldb.ReadComposers` opens a read-only copy (Lesson 4) and returns sessions. Errors are wrapped with the collector name. |
| 48–52 | Loop sessions; `UpsertComposer` inserts-or-updates each; count it. |
| 53 | Each session also produces a `session_start` event so the History tab can count "sessions started per day". |
| (in block) | We also read **historical model choices** from bubbles here, and only `MarkIngested` at the end so a crash mid-way doesn't mark the source "done". |

```go
85|     projectsDir, err := paths.ProjectsDir()
...
90|     fp, fpMtime, err := store.DirFingerprint(projectsDir)
...
94|     if need, err := db.NeedsIngestFingerprint(sourceTranscripts, fp, fpMtime); err == nil && need {
95|         tc := &transcripts.Collector{ProjectsDir: projectsDir}
96|         events, _, err := tc.Collect(ctx)
97|         if err != nil {
98|             return result, fmt.Errorf("transcripts: %w", err)
99|         }
100|        if err := db.ReplaceToolEventsBySource("transcript", events); err != nil {
101|            return result, err
102|        }
103|        result.EventsInserted += len(events)
104|        // …MarkIngestedFingerprint…
108|    }
109|
110|    if err := db.RebuildDailyRollups(); err != nil {
111|        return result, err
112:    }
113:    // …SetMeta("last_ingest_at", …)…
```

| Line(s) | Explanation |
|---------|-------------|
| 90–94 | Use the **directory fingerprint** to decide whether transcripts changed. Cheaper and more accurate than checking one file. |
| 95–99 | Run the transcripts collector (Lesson 5). |
| 100 | `ReplaceToolEventsBySource("transcript", …)` deletes old transcript tool rows and inserts the new set **inside one transaction**. This is critical: the TUI, refreshing every 2s, never sees an empty half-imported table. (Read `ReplaceToolEventsBySource` in `db.go` — it uses `db.sql.Begin()`, a prepared statement in a loop, and `tx.Commit()`.) |
| 110–112 | Recompute the daily rollups from the now-updated `events`/`composers`. |

> **Two big ideas in ingest:** (1) **skip unchanged sources** for speed; (2) **swap data atomically** so readers never see a torn state.

---

## 12. Lesson 8 — the `doctor` command

File: `internal/doctor/doctor.go`. This package provides diagnostic health checks. It's a great example of a simple, extensible feature that uses several other packages.

```go
22| func Run() ([]Check, error) {
23|     var out []Check
24|
25|     user, err := paths.CursorUserData()
...
63|     db, err := store.OpenDefault()
...
71|         // New: check stats.db size
72|         var dbSize int64
73|         if p, err := paths.StatDataDir(); err == nil {
74|             if st, err := os.Stat(filepath.Join(p, "stats.db")); err == nil {
75|                 dbSize = st.Size()
76|             }
77|         }
...
115| func formatSize(b int64) string {
```

| Line(s) | Explanation |
|---------|-------------|
| 22 | `Run()` returns a slice of `Check` structs, which `main.go` prints to the console. |
| 25 | It uses the `paths` package to find where Cursor data *should* be. |
| 63 | It opens our local `stats.db` to report how many sessions are cached. |
| 71–77 | It checks the physical file size of our cache on disk. This was originally an exercise but is now part of the tool! |
| 115 | `formatSize` is a classic helper that turns raw bytes into human-readable strings like "1.2 MB". |

> **Lesson:** Diagnostics should be "read-only" and never crash. If a check fails (e.g., `stats.db` is missing), we record the error in the `Check` result instead of returning a fatal error.

---

## 13. Lesson 9 — hooks and model tracking

Files: `internal/hooks/parse.go`, `internal/hooks/server.go`, plus `hooks/cursor-stat-hook.js`.

**Flow:** Cursor runs the small Node script on each event → the script POSTs the event JSON to `http://127.0.0.1:23556/event` → our Go server parses it → updates the ring (for live view) and, for prompt submissions, writes a `model_choice` row.

### 13.1 Parsing the payload

```go
// internal/hooks/parse.go
22| func ParseEvent(body []byte) (cursor.LiveEvent, *cursor.ModelChoiceEvent) {
23|     var p hookPayload
24|     _ = json.Unmarshal(body, &p)
25|
26|     sid := p.SessionID
27|     if sid == "" {
28|         sid = p.ConversationID
29|     }
30|
31|     ev := cursor.LiveEvent{
32|         Kind:    p.HookEventName,
33|         Tool:    p.ToolName,
34|         Session: sid,
35|         Detail:  p.HookEventName,
36|     }
37|     if p.Model != "" {
38|         ev.Model = p.Model
39|         ev.Manual = !cursor.IsAutoModel(p.Model)
40|     }
41|
42|     if p.HookEventName != "beforeSubmitPrompt" || strings.TrimSpace(p.Model) == "" {
43|         return ev, nil
44|     }
45|     // …build a ModelChoiceEvent and return it alongside ev…
```

| Line(s) | Explanation |
|---------|-------------|
| 23–24 | Decode into a private `hookPayload` struct. We ignore the error: a malformed payload yields an empty struct and we degrade gracefully. |
| 26–29 | Cursor sometimes sends `session_id`, sometimes `conversation_id`. Prefer the first, fall back to the second. |
| 31–36 | Build a `LiveEvent` for the ring (the live view) regardless of event type. |
| 37–40 | If the payload carries a `model`, record it and compute `Manual` using `IsAutoModel` (Lesson 3). |
| 42–44 | Only `beforeSubmitPrompt` with a real model becomes a persisted **model choice**. Everything else returns `nil` for the second value. Returning a **pointer** (`*ModelChoiceEvent`) lets us express "there may or may not be one". |

### 13.2 The HTTP server

```go
// internal/hooks/server.go (handleEvent)
70| func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
71|     if r.Method != http.MethodPost {
72|         http.Error(w, "method", http.StatusMethodNotAllowed)
73|         return
74|     }
75|     body, err := io.ReadAll(io.LimitReader(r.Body, maxHookBody))
76|     if err != nil {
77|         http.Error(w, "read", http.StatusBadRequest)
78|         return
79|     }
80|
81|     at := time.Now().UTC()
82|     ev, choice := ParseEvent(body)
83|     ev.At = at
84|     // …set ev.Detail to a friendly model label…
89|     if s.ring != nil {
90|         s.ring.Push(ev)
91|     }
92|     if choice != nil && s.db != nil {
93|         choice.At = at
94|         _, _ = s.db.InsertModelChoice(*choice)
95|     }
96|     w.WriteHeader(http.StatusOK)
97|     _, _ = w.Write([]byte("{}"))
98| }
```

| Line(s) | Explanation |
|---------|-------------|
| 70 | A standard Go HTTP handler: it gets a `ResponseWriter` to reply with and the `*Request`. |
| 71–74 | Only accept POST. `http.Error` writes a status code + message. |
| 75 | `io.LimitReader(r.Body, maxHookBody)` caps how much we read — a safety limit so a giant body can't exhaust memory. |
| 82 | Parse once; get both the live event and an optional model choice. |
| 89–91 | Push to the ring for the live panel (guard against `nil` in tests). |
| 92–95 | If there's a model choice and a DB, persist it. `*choice` dereferences the pointer to pass the value. We ignore the insert error — a dropped hook event must never crash Cursor. |
| 96–97 | Always reply `200 {}` quickly so Cursor isn't blocked. |

The server is created in `main.go` (`hooks.NewServer(ring, db, port)`) and started in a goroutine. `internal/hooks/install.go` writes the hook entries into `~/.cursor/hooks.json` using a **marker** (the script filename) so re-running install is safe and detectable.

---

## 14. Lesson 10 — the live ring buffer and watcher

Files: `internal/live/ring.go`, `internal/live/watcher.go`, `internal/live/live.go`.

### 14.1 A thread-safe ring buffer

```go
// internal/live/ring.go
12| type Ring struct {
13|     mu     sync.RWMutex
14|     events []cursor.LiveEvent
15|     cap    int
16| }
27| func (r *Ring) Push(ev cursor.LiveEvent) {
28|     if ev.At.IsZero() {
29|         ev.At = time.Now().UTC()
30|     }
31|     r.mu.Lock()
32|     defer r.mu.Unlock()
33|     r.events = append(r.events, ev)
34|     if len(r.events) > r.cap {
35|         r.events = r.events[len(r.events)-r.cap:]
36|     }
37| }
```

| Line(s) | Explanation |
|---------|-------------|
| 13 | A `sync.RWMutex` protects the slice. **Why?** The hook server (one goroutine) writes while the TUI (another goroutine) reads. Without a lock that's a data race. |
| 27–30 | Default the timestamp if missing. |
| 31–32 | `Lock()` for writing; `defer Unlock()` guarantees release even if something panics. |
| 33–36 | Append, then trim to the last `cap` events. This is the "ring": old events fall off the front. |

`List(n)`, `LatestAny`, `LatestTool`, and `LatestModel` use `RLock()` (a **read** lock) — many readers can hold it at once, which is fine because they don't mutate.

### 14.2 The filesystem watcher

`watcher.go` uses `fsnotify` to watch Cursor's `globalStorage` dir and every `agent-transcripts` folder. The interesting part is **debouncing**:

```go
58| debounce := time.NewTimer(0)
...
72|     if ev.Op&(fsnotify.Write|fsnotify.Create) != 0 {
73|         pending = ev.Name
74|         debounce.Reset(500 * time.Millisecond)
75|     }
...
85| case <-debounce.C:
86|     if pending != "" {
87|         w.ring.Push(cursor.LiveEvent{ At: time.Now().UTC(), Kind: "fs_change", Detail: filepath.Base(pending) })
88|         pending = ""
89|     }
```

| Line(s) | Explanation |
|---------|-------------|
| 72–74 | On a write/create, remember the file and **reset** a 500 ms timer instead of reacting immediately. |
| 85–89 | Only when the timer finally fires (500 ms of quiet) do we push one event. This collapses a burst of rapid writes into a single signal — that's **debouncing**. |
| `select` | The loop uses `select` to wait on several channels at once: `ctx.Done()` (stop), `fw.Events`, `fw.Errors`, and `debounce.C`. `select` blocks until one is ready. |

`live.go` answers "is Cursor running?" by shelling out to `pgrep` (macOS/Linux) or `tasklist` (Windows), and "which workspace?" by finding the most recently modified `workspace.json`.

---

## 15. Lesson 11 — `aggregate.Dashboard`

File: `internal/aggregate/dashboard.go`. This is the **merge point**: it combines the live ring and the cache into one `Dashboard` struct the TUI renders.

```go
14| func Dashboard(ctx context.Context, db *store.DB, events *live.Ring) (cursor.Dashboard, error) {
15|     out := cursor.Dashboard{GeneratedAt: time.Now().UTC()}
16|
17|     running, pid := live.Snapshot()
18|     out.Live.CursorRunning = running
19|     out.Live.CursorPID = pid
20|
21|     if events != nil {
22|         out.LiveEvents = events.List(10)
23|         if ev, ok := events.LatestTool(); ok { /* set LastTool */ }
            // …LatestModel sets LastModel/LastModelManual…
33|     }
34|
35|     skipToolScan := false
36|     if db != nil {
37|         if n, err := db.ToolEventCount(); err == nil && n > 0 {
38|             skipToolScan = true
39|         }
40|     }
41|
42|     snap, err := Snapshot(ctx, SnapshotOptions{SkipToolScan: skipToolScan})
```

| Line(s) | Explanation |
|---------|-------------|
| 15 | Start an empty `Dashboard` stamped with the current time. |
| 17–19 | Cheap live signals first (process check). |
| 21–33 | If we have a ring, pull the last 10 events and the latest tool/model for the "NOW" panel. |
| 35–40 | **Optimisation:** if the cache already has tool rows, set `skipToolScan` so the snapshot below won't re-walk every transcript on disk each tick. |
| 42 | `Snapshot` does a fresh read of Cursor files (session list, storage sizes), honouring `SkipToolScan`. |

```go
70|     if out.CacheReady {
71|         out.History, err = db.DailyRollups(7)
            // …
78|         out.Tools, err = db.ToolBreakdownAll()
            // …
83|         out.Models, err = db.ModelBreakdownAll()
            // …last model choice from cache…
93|     }
94|
95|     if out.Tools.Total == 0 {
96|         out.Tools = snap.Tools
97|     }
98|     if events != nil {
99|         live := toolsFromRing(events)
100|        out.ToolsLive = live.Total
101|        out.Tools = mergeToolBreakdown(out.Tools, live)
102|    }
```

| Line(s) | Explanation |
|---------|-------------|
| 70–93 | When the cache has data, read history, tool totals, model breakdown, and the last model choice from `stats.db`. |
| 95–97 | Fallback: if the cache had no tools, use whatever the live snapshot found. |
| 98–101 | **Merge live tools on top of cached tools** so Tab 3 ticks up during a session even before you re-ingest. `mergeToolBreakdown` adds the per-tool counts together. |

The helper `toolsFromRing` counts tool events currently in the ring; `mergeToolBreakdown` sums two breakdowns. Both are pure functions — easy to test.

---

## 16. Lesson 12 — the Bubble Tea TUI

File: `internal/tui/model.go`. Bubble Tea uses the **Elm architecture**: one immutable `Model`, an `Update` function that returns a *new* model for each message, and a `View` that renders the model to a string. You never draw to the screen yourself.

### 16.1 Messages and the model struct

```go
19| type tickMsg time.Time
20| type dataMsg struct {
21|     gen  uint64
22|     data cursor.Dashboard
23| }
24| type errMsg struct {
25|     gen uint64
26|     err error
27| }
28| type ingestDoneMsg cursor.IngestResult
29| type ingestErrMsg struct{ err error }
30|
31| type model struct {
32|     ctx       context.Context
33|     db        *store.DB
34|     ring      *live.Ring
35|     tab       int
36|     width     int
37|     height    int
38|     data      cursor.Dashboard
39|     loading   bool
40|     errText   string
41|     refresh   time.Duration
42|     filter    string
43|     ingesting bool
44|     loadGen   uint64
45| }
```

| Line(s) | Explanation |
|---------|-------------|
| 19–29 | **Messages** are just types. Bubble Tea delivers them to `Update`. `tickMsg` is the periodic clock; `dataMsg` carries a finished dashboard; the `ingest*` messages report ingest progress. |
| 20–23, 24–27 | `dataMsg`/`errMsg` carry a `gen` (generation number). More on this below — it prevents stale updates. |
| 31–45 | **All UI state in one struct.** `tab` is the current tab; `data` is the last dashboard; `ingesting` freezes refresh; `loadGen` is the generation counter. |

### 16.2 `Init`, commands, and `tea.Cmd`

```go
65| func (m model) Init() tea.Cmd {
66|     m.loadGen = 1
67|     return tea.Batch(tickCmd(m.refresh), loadCmd(m.ctx, m.db, m.ring, m.loadGen))
68| }
70| func tickCmd(d time.Duration) tea.Cmd {
71|     return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
72| }
74| func loadCmd(ctx context.Context, db *store.DB, ring *live.Ring, gen uint64) tea.Cmd {
75|     return func() tea.Msg {
76|         d, err := aggregate.Dashboard(ctx, db, ring)
77|         if err != nil {
78|             return errMsg{gen: gen, err: err}
79|         }
80|         return dataMsg{gen: gen, data: d}
81|     }
82| }
```

| Line(s) | Explanation |
|---------|-------------|
| 65–68 | `Init` runs once at startup. `tea.Batch` runs several commands together: start the tick timer **and** kick off the first data load. |
| 70–72 | `tea.Tick` sends a `tickMsg` after duration `d`. We re-arm it every tick to get a repeating clock. |
| 74–82 | A `tea.Cmd` is `func() tea.Msg` — Bubble Tea runs it **in a goroutine** so slow work (reading the DB) doesn't freeze the UI. When it finishes it returns a message that arrives back in `Update`. Each load is tagged with `gen`. |

### 16.3 `Update`: the heart of the loop

```go
96| func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
97|     switch msg := msg.(type) {
98|     case tea.KeyMsg:
99|         switch msg.String() {
100|        case "q", "ctrl+c":
101|            return m, tea.Quit
102|        case "r":
103|            m.loading = true
104|            m.loadGen++
105|            return m, loadCmd(m.ctx, m.db, m.ring, m.loadGen)
106|        case "i":
107|            if !m.ingesting {
108|                m.ingesting = true
109|                m.errText = ""
110|                return m, ingestCmd(m.db)
111|            }
112|        case "1", "2", "3":
113|            m.tab = int(msg.String()[0] - '0')
114|        case "4", "5":
115|            m.tab = int(msg.String()[0] - '0')
116|        }
117|    case tea.WindowSizeMsg:
118|        m.width = msg.Width
119|        m.height = msg.Height
120|    case tickMsg:
121|        cmds := []tea.Cmd{tickCmd(m.refresh)}
122|        if !m.ingesting {
123|            m.loadGen++
124|            cmds = append(cmds, loadCmd(m.ctx, m.db, m.ring, m.loadGen))
125|        }
126|        return m, tea.Batch(cmds...)
127|    case dataMsg:
128|        if msg.gen != m.loadGen || m.ingesting {
129|            return m, nil
130|        }
131|        m.loading = false
132|        m.data = msg.data
133|        m.errText = ""
134|    case ingestDoneMsg:
135|        m.ingesting = false
136|        m.loadGen++
137|        return m, loadCmd(m.ctx, m.db, m.ring, m.loadGen)
138|    case ingestErrMsg:
139|        m.ingesting = false
140|        m.errText = msg.err.Error()
141|    case errMsg:
142|        if msg.gen != m.loadGen {
143|            return m, nil
144|        }
145|        m.loading = false
146|        m.errText = msg.err.Error()
147|    }
148|    return m, nil
149| }
```

| Line(s) | Explanation |
|---------|-------------|
| 97 | `switch msg := msg.(type)` is a **type switch** — it branches on the concrete type of the message. |
| 100–101 | `q`/Ctrl-C return `tea.Quit`, a special command that ends the program. |
| 102–105 | `r` forces a reload: bump `loadGen` and issue a new `loadCmd`. |
| 106–111 | `i` starts ingest, but only if not already ingesting. We clear any old error and set `ingesting = true`. |
| 112–115 | Number keys switch tabs. `msg.String()[0] - '0'` converts the character `'3'` to the int `3`. |
| 120–126 | On each tick we always re-arm the timer; we reload **only if not ingesting** (so the screen doesn't flicker mid-ingest). |
| 127–133 | **The generation check.** A `dataMsg` is applied only if its `gen` matches the latest request and we're not ingesting. This throws away results from older, slower loads that finished late — preventing stale data from overwriting fresh data. |
| 134–137 | Ingest finished: clear the flag and reload immediately to show new numbers. |
| 138–140 | Ingest failed: clear the flag and show the error in the footer. |
| 141–146 | A failed load shows its error (subject to the same generation check). |
| 148 | `return m, nil` — return the (possibly updated) model and no follow-up command. |

> **Two patterns worth stealing:** (1) run slow work in a `tea.Cmd` goroutine and feed results back as messages; (2) tag async work with a generation number so late results can be ignored.

### 16.4 `View`: pure rendering

```go
151| func (m model) View() string {
152|     if m.width < 40 {
153|         return "Terminal too narrow (need ≥40 columns)\n"
154|     }
155|     var b strings.Builder
156|     b.WriteString(renderHeader(m))
157|     b.WriteString("\n")
158|     b.WriteString(renderTabs(m))
159|     b.WriteString("\n")
160|     switch m.tab {
161|     case 2: b.WriteString(renderSessions(m))
163|     case 3: b.WriteString(renderTools(m))
165|     case 4: b.WriteString(renderStorage(m))
167|     case 5: b.WriteString(renderHistory(m))
169|     default: b.WriteString(renderOverview(m))
171|     }
172|     b.WriteString("\n")
173|     b.WriteString(renderFooter(m))
174|     return b.String()
175| }
```

| Line(s) | Explanation |
|---------|-------------|
| 151 | `View` returns a **string** — the whole screen. Bubble Tea diffs and draws it. |
| 152–154 | Guard tiny terminals instead of crashing. |
| 155 | `strings.Builder` efficiently concatenates many pieces. |
| 156–173 | Build header, tab bar, the body for the current tab, then footer. Each `render*` is a pure function of the model — **no I/O here**, so drawing is fast and never blocks. |

The `render*` helpers (e.g. `renderOverview`, `renderTools`) use **Lip Gloss** styles (defined at the bottom of the file) for colour and a small `barChart`/`sparkline` for ASCII charts. Read `renderTools` to see how tool names are sorted before drawing — that keeps the bars stable between refreshes.

---

## 17. Concurrency model in one page

Who runs where, while the TUI is open:

```text
main goroutine ─────────────► tui.Run (blocks)
                                 │  every 2s issues loadCmd
                                 ▼
                              tea.Cmd goroutine ── aggregate.Dashboard ── reads stats.db (RO copy of Cursor DB)
goroutine #1 (hooks server) ── handleEvent ── ring.Push + db.InsertModelChoice
goroutine #2 (fs watcher)   ── debounced ──── ring.Push
```

Safety rules the code follows:

1. **Shared state is guarded.** The `ring` is the only thing multiple goroutines touch, and every access goes through its `RWMutex`.
2. **The cache DB is single-writer-ish.** Hooks write small rows; ingest does the big writes; SQLite's `busy_timeout` smooths brief overlaps.
3. **Slow work never runs in `Update` or `View`.** It runs in `tea.Cmd` goroutines and returns a message.
4. **Background goroutines stop cleanly.** They take the cancellable `bg` context; when `runInteractive` returns, `cancel()` ends them.

---

## 18. Testing patterns

Run them:

```bash
go test ./...                 # everything
go test ./internal/cursor -v  # one package, verbose
go test -run TestIsAutoModel ./internal/cursor
```

### 18.1 A table test, explained

```go
// internal/cursor/model_test.go
 5| func TestIsAutoModel(t *testing.T) {
 6|     auto := []string{"", "default", "Default", "auto", "AUTO", "automatic", "  default  "}
 7|     for _, m := range auto {
 8|         if !IsAutoModel(m) {
 9|             t.Fatalf("%q should be auto", m)
10|         }
11|     }
12|     if IsAutoModel("claude-opus-4-7") {
13|         t.Fatal("expected manual model")
14|     }
15| }
```

| Line(s) | Explanation |
|---------|-------------|
| 5 | A test is a function named `TestXxx(t *testing.T)`. |
| 6–11 | A slice of inputs that should **all** be "auto"; loop and assert. `t.Fatalf` reports a failure and stops this test. `%q` quotes the value so empty/space inputs are visible. |
| 12–14 | The negative case: a real model must not be auto. |

### 18.2 Tests never touch your real data

```go
// internal/ingest/ingest_test.go (excerpt)
17| t.Setenv("HOME", root)
18| t.Setenv("CURSOR_STAT_HOME", filepath.Join(root, ".cursor-stat"))
...
39| t.Setenv("CURSOR_USER_DATA", user)
```

| Line(s) | Explanation |
|---------|-------------|
| 17–39 | `t.Setenv` sets environment variables **only for this test** (restored afterwards). Because `paths.go` checks `CURSOR_USER_DATA`/`CURSOR_STAT_HOME` first, the test redirects everything into a temp dir. This is why the override-first design in Lesson 2 matters. |

Fixtures (tiny SQLite DBs, JSONL snippets) are created inside the `_test.go` files with `t.TempDir()`, so tests are hermetic and need no network.

---

## 19. Glossary

| Term | Meaning |
|------|---------|
| **Composer** | Cursor's name for a chat/agent session |
| **Bubble** | One message inside a composer (`bubbleId:*` rows) |
| **WAL** | SQLite Write-Ahead Log; the `-wal`/`-shm` sibling files |
| **Ingest** | One pass that updates `stats.db` from Cursor files |
| **Rollup** | Pre-computed daily totals (`daily_rollups` table) |
| **Ring buffer** | Fixed-size slice holding the last N live events |
| **Snapshot** | A fresh, live read of Cursor files (no cache) |
| **Hook** | A command Cursor runs on events; ours POSTs to our server |
| **Bubble Tea** | Go TUI framework with `Model`/`Update`/`View` |
| **Lip Gloss** | Styling library used for colours and layout |
| **`tea.Cmd`** | `func() tea.Msg` run in a goroutine; result returns to `Update` |
| **Generation counter** | `loadGen`; lets `Update` ignore stale async results |
| **Idempotent** | Running it again changes nothing new (safe to repeat) |

---

## 20. Exercises

Work top to bottom; each builds confidence.

1. **Read-only tour.** Run `go run ./cmd/cursor-stat --once | less` and find where each JSON field is set in the code. Trace `tools` back to `transcripts.Breakdown`.
2. **Improve the doctor.** In `internal/doctor/doctor.go`, add a check that warns if `stats.db` is larger than 50MB. (Hint: check `dbSize` in `doctor.go` and add a new `Check` with `Status: "WARN"` if it exceeds the limit.)
3. **New CSV column.** In `internal/export/export.go`, add a `manual_model_prompts` column. You'll need a query in `db.go` and a header change. Add a test.
4. **Sessions filter.** Add a 'Workspace' filter to the Sessions tab (Tab 2). This requires adding a `filter` string to the `model` struct in `tui/model.go` and updating `renderSessions` to skip rows that don't match.
5. **New hook field.** Capture `cwd` from the hook payload in `parse.go` and show it in the live events list.

For each: write the code, run `gofmt -l .` (should print nothing), `go vet ./...`, and `go test ./...`.

Debug without the TUI:

```bash
go run ./cmd/cursor-stat --once     # JSON snapshot
go run ./cmd/cursor-stat doctor     # health checks
go run ./cmd/cursor-stat ingest     # rebuild the cache, prints a JSON result
```

Next, read [ARCHITECTURE.md](ARCHITECTURE.md) for the module boundaries and schema, and [DATA-SOURCES.md](DATA-SOURCES.md) for exactly where Cursor stores its files.
