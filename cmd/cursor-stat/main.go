package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/cursor-stat/cursor-stat/internal/aggregate"
	"github.com/cursor-stat/cursor-stat/internal/doctor"
	exportdata "github.com/cursor-stat/cursor-stat/internal/export"
	"github.com/cursor-stat/cursor-stat/internal/hooks"
	"github.com/cursor-stat/cursor-stat/internal/ingest"
	"github.com/cursor-stat/cursor-stat/internal/live"
	"github.com/cursor-stat/cursor-stat/internal/store"
	"github.com/cursor-stat/cursor-stat/internal/tui"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "ingest":
			runIngest(os.Args[2:])
			return
		case "doctor":
			runDoctor()
			return
		case "export":
			runExport(os.Args[2:])
			return
		case "hooks":
			runHooks(os.Args[2:])
			return
		case "help", "-h", "--help":
			printUsage()
			return
		}
	}

	fs := flag.NewFlagSet("cursor-stat", flag.ExitOnError)
	once := fs.Bool("once", false, "print JSON snapshot and exit")
	noTUI := fs.Bool("no-tui", false, "never start interactive UI")
	timeout := fs.Duration("timeout", 30*time.Second, "max collection time")
	_ = fs.Parse(os.Args[1:])

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	if *once || *noTUI {
		runOnce(ctx)
		return
	}
	runInteractive()
}

func runOnce(ctx context.Context) {
	snap, err := aggregate.Snapshot(ctx)
	if err != nil {
		log.Fatalf("snapshot: %v", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(snap); err != nil {
		log.Fatalf("encode: %v", err)
	}
}

func runInteractive() {
	ctx := context.Background()
	db, err := store.OpenDefault()
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer db.Close()
	_ = db.RepairInvalidToolTimestamps()
	_ = db.RebuildDailyRollups()

	ring := live.NewRing(64)
	bg, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = hooks.NewServer(ring, db, hooks.DefaultPort).Start(bg) }()
	go func() { _ = live.NewWatcher(ring).Run(bg) }()

	if err := tui.Run(ctx, db, ring); err != nil {
		log.Fatalf("tui: %v", err)
	}
}

func runIngest(args []string) {
	fs := flag.NewFlagSet("ingest", flag.ExitOnError)
	timeout := fs.Duration("timeout", 120*time.Second, "max ingest time")
	_ = fs.Parse(args)

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	db, err := store.OpenDefault()
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer db.Close()

	res, err := ingest.Run(ctx, db)
	if err != nil {
		log.Fatalf("ingest: %v", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(res)
}

func runDoctor() {
	checks, err := doctor.Run()
	if err != nil {
		log.Fatalf("doctor: %v", err)
	}
	for _, c := range checks {
		fmt.Printf("[%s] %s — %s\n", c.Status, c.Name, c.Detail)
	}
}

func runExport(args []string) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	format := fs.String("format", "csv", "csv or json")
	days := fs.Int("days", 30, "days of rollups")
	out := fs.String("o", "", "output file (default stdout)")
	_ = fs.Parse(args)

	db, err := store.OpenDefault()
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer db.Close()

	var w *os.File
	if *out == "" {
		w = os.Stdout
	} else {
		w, err = os.Create(*out)
		if err != nil {
			log.Fatalf("create: %v", err)
		}
		defer w.Close()
	}

	switch *format {
	case "json":
		err = exportdata.WriteJSON(db, *days, w)
	case "csv":
		err = exportdata.WriteCSV(db, *days, w)
	default:
		log.Fatalf("unknown format %q", *format)
	}
	if err != nil {
		log.Fatalf("export: %v", err)
	}
}

func runHooks(args []string) {
	if len(args) == 0 || args[0] == "install" {
		hookPath := resolveHookScript()
		added, err := hooks.Install(hookPath)
		if err != nil {
			log.Fatalf("install: %v", err)
		}
		fmt.Printf("hooks installed (added %d events) → %s\n", added, hookPath)
		return
	}
	log.Fatalf("usage: cursor-stat hooks install")
}

func resolveHookScript() string {
	candidates := []string{
		"hooks/cursor-stat-hook.js",
		filepath.Join("cursor-stat", "hooks", "cursor-stat-hook.js"),
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "hooks", "cursor-stat-hook.js"))
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "hooks", "cursor-stat-hook.js"))
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	log.Fatal("cursor-stat-hook.js not found — run from repo root or set path manually")
	return ""
}

func printUsage() {
	fmt.Println(`cursor-stat — local Cursor usage dashboard

Usage:
  cursor-stat              Interactive TUI (default)
  cursor-stat --once       JSON snapshot to stdout
  cursor-stat ingest       Backfill ~/.cursor-stat/stats.db
  cursor-stat doctor       Diagnostic checks
  cursor-stat export       Export rollups (--format csv|json)
  cursor-stat hooks install  Register live hook in ~/.cursor/hooks.json`)
}
