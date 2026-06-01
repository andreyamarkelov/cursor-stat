package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cursor-stat/cursor-stat/internal/aggregate"
	"github.com/cursor-stat/cursor-stat/internal/cursor"
	"github.com/cursor-stat/cursor-stat/internal/ingest"
	"github.com/cursor-stat/cursor-stat/internal/live"
	"github.com/cursor-stat/cursor-stat/internal/store"
)

type tickMsg time.Time
type dataMsg struct {
	gen  uint64
	data cursor.Dashboard
}
type errMsg struct {
	gen uint64
	err error
}
type ingestDoneMsg cursor.IngestResult
type ingestErrMsg struct{ err error }

type model struct {
	ctx       context.Context
	db        *store.DB
	ring      *live.Ring
	tab       int
	width     int
	height    int
	data      cursor.Dashboard
	loading   bool
	errText   string
	refresh   time.Duration
	filter    string
	ingesting bool
	loadGen   uint64
}

// Run starts the interactive dashboard.
func Run(ctx context.Context, db *store.DB, ring *live.Ring) error {
	if ring == nil {
		ring = live.NewRing(64)
	}
	m := model{
		ctx:     ctx,
		db:      db,
		ring:    ring,
		tab:     1,
		refresh: 2 * time.Second,
		loading: true,
	}
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m model) Init() tea.Cmd {
	m.loadGen = 1
	return tea.Batch(tickCmd(m.refresh), loadCmd(m.ctx, m.db, m.ring, m.loadGen))
}

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func loadCmd(ctx context.Context, db *store.DB, ring *live.Ring, gen uint64) tea.Cmd {
	return func() tea.Msg {
		d, err := aggregate.Dashboard(ctx, db, ring)
		if err != nil {
			return errMsg{gen: gen, err: err}
		}
		return dataMsg{gen: gen, data: d}
	}
}

func ingestCmd(db *store.DB) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		res, err := ingest.Run(ctx, db)
		if err != nil {
			return ingestErrMsg{err: err}
		}
		return ingestDoneMsg(res)
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			m.loading = true
			m.loadGen++
			return m, loadCmd(m.ctx, m.db, m.ring, m.loadGen)
		case "i":
			if !m.ingesting {
				m.ingesting = true
				m.errText = ""
				return m, ingestCmd(m.db)
			}
		case "1", "2", "3":
			m.tab = int(msg.String()[0] - '0')
		case "4", "5":
			m.tab = int(msg.String()[0] - '0')
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tickMsg:
		cmds := []tea.Cmd{tickCmd(m.refresh)}
		if !m.ingesting {
			m.loadGen++
			cmds = append(cmds, loadCmd(m.ctx, m.db, m.ring, m.loadGen))
		}
		return m, tea.Batch(cmds...)
	case dataMsg:
		if msg.gen != m.loadGen || m.ingesting {
			return m, nil
		}
		m.loading = false
		m.data = msg.data
		m.errText = ""
	case ingestDoneMsg:
		m.ingesting = false
		m.loadGen++
		return m, loadCmd(m.ctx, m.db, m.ring, m.loadGen)
	case ingestErrMsg:
		m.ingesting = false
		m.errText = msg.err.Error()
	case errMsg:
		if msg.gen != m.loadGen {
			return m, nil
		}
		m.loading = false
		m.errText = msg.err.Error()
	}
	return m, nil
}

func (m model) View() string {
	if m.width < 40 {
		return "Terminal too narrow (need ≥40 columns)\n"
	}
	var b strings.Builder
	b.WriteString(renderHeader(m))
	b.WriteString("\n")
	b.WriteString(renderTabs(m))
	b.WriteString("\n")
	switch m.tab {
	case 2:
		b.WriteString(renderSessions(m))
	case 3:
		b.WriteString(renderTools(m))
	case 4:
		b.WriteString(renderStorage(m))
	case 5:
		b.WriteString(renderHistory(m))
	default:
		b.WriteString(renderOverview(m))
	}
	b.WriteString("\n")
	b.WriteString(renderFooter(m))
	return b.String()
}

func renderHeader(m model) string {
	run := dimStyle.Render("Cursor: ○ stopped")
	if m.data.Live.CursorRunning {
		run = okStyle.Render(fmt.Sprintf("Cursor: ● running (pid %d)", m.data.Live.CursorPID))
	}
	ws := truncate(m.data.Live.ActiveWorkspace, 40)
	return titleStyle.Render("cursor-stat") + " │ " + run + " │ " + dimStyle.Render("workspace: "+ws)
}

func renderTabs(m model) string {
	labels := []string{"Overview", "Sessions", "Tools", "Storage", "History"}
	var parts []string
	for i, l := range labels {
		n := i + 1
		if m.tab == n {
			parts = append(parts, titleStyle.Render(fmt.Sprintf("[%d] %s", n, l)))
		} else {
			parts = append(parts, dimStyle.Render(fmt.Sprintf("[%d] %s", n, l)))
		}
	}
	return strings.Join(parts, "  ")
}

func renderOverview(m model) string {
	today := m.data.Today
	live := m.data.Live
	var b strings.Builder

	b.WriteString(titleStyle.Render(" NOW ") + dimStyle.Render("(refreshes every ~2s)\n"))
	lastTool := live.LastTool
	if lastTool == "" {
		lastTool = dimStyle.Render("-")
	} else {
		lastTool = okStyle.Render(lastTool)
	}
	b.WriteString(fmt.Sprintf(" Last tool:     %s\n", lastTool))
	lastModel := formatLastModel(live.LastModel, live.LastModelManual)
	b.WriteString(fmt.Sprintf(" Last model:    %s\n", lastModel))
	b.WriteString(fmt.Sprintf(" Last event:    %s\n", liveEventLine(live.LastEventKind, live.LastEventAt)))
	activeChat := truncate(live.ActiveSession, 36)
	if activeChat == "" {
		activeChat = dimStyle.Render("-")
	}
	b.WriteString(fmt.Sprintf(" Active chat:   %s (%d msgs)\n", activeChat, live.ActiveSessionMsgs))

	if len(m.data.LiveEvents) > 0 {
		b.WriteString(dimStyle.Render("\n LIVE EVENTS (hooks / filesystem)\n"))
		start := len(m.data.LiveEvents) - 3
		if start < 0 {
			start = 0
		}
		for _, ev := range m.data.LiveEvents[start:] {
			b.WriteString(fmt.Sprintf("  %s  %s  %s\n", relTime(ev.At), ev.Kind, liveEventDetail(ev)))
		}
	} else {
		b.WriteString(warnStyle.Render("\n No live tool stream — run: cursor-stat hooks install\n"))
	}

	b.WriteString("\n" + dimStyle.Render(" TODAY (from cache — press i after working)\n"))
	b.WriteString(fmt.Sprintf(" Sessions: %-6d              %s\n", today.SessionsStarted, sparkline(m.data.History, func(r cursor.DailyRollup) float64 { return float64(r.AssistantMsgs) })))
	b.WriteString(fmt.Sprintf(" Messages: %-6d              %s\n", today.Messages, sparkline(m.data.History, func(r cursor.DailyRollup) float64 { return float64(r.ToolCalls) })))
	b.WriteString(fmt.Sprintf(" Tool calls: %-6d\n", today.ToolCalls))
	b.WriteString(fmt.Sprintf(" Tool failures: %-6d\n", today.ToolFailures))
	b.WriteString(fmt.Sprintf(" Manual models: %-6d (Auto picks: %d)\n", today.ManualModelPrompts, today.AutoModelPrompts))
	if m.data.Models.Manual > 0 {
		b.WriteString(dimStyle.Render("\n MANUAL MODEL PICKS (all time, press i to backfill history)\n"))
		names := modelPickLines(m.data.Models, 5)
		for _, line := range names {
			b.WriteString("  " + line + "\n")
		}
	}
	if !m.data.CacheReady {
		b.WriteString(warnStyle.Render("\n Cache empty — press i to ingest\n"))
	}
	if len(m.data.Composers) > 0 {
		b.WriteString("\n RECENT SESSIONS\n")
		limit := 5
		if limit > len(m.data.Composers) {
			limit = len(m.data.Composers)
		}
		for _, c := range m.data.Composers[:limit] {
			b.WriteString(fmt.Sprintf("  %-8s  %-24s  msgs=%d  %s\n",
				shortID(c.ID), truncate(c.Title, 24), c.MessageCount, relTime(c.UpdatedAt)))
		}
	}
	return b.String()
}

func formatLastModel(model string, manual bool) string {
	if model == "" {
		return dimStyle.Render("-")
	}
	label := cursor.NormalizeModel(model)
	if manual {
		return okStyle.Render(label + " (manual)")
	}
	return dimStyle.Render(label)
}

func liveEventDetail(ev cursor.LiveEvent) string {
	if ev.Model != "" {
		s := cursor.NormalizeModel(ev.Model)
		if ev.Manual {
			return okStyle.Render(s + " (manual)")
		}
		return s
	}
	if ev.Tool != "" {
		return ev.Tool
	}
	return ev.Detail
}

func modelPickLines(models cursor.ModelBreakdown, limit int) []string {
	type pair struct {
		name  string
		count int
	}
	var rows []pair
	for name, n := range models.ByModel {
		if name == "Auto" {
			continue
		}
		rows = append(rows, pair{name, n})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].count == rows[j].count {
			return rows[i].name < rows[j].name
		}
		return rows[i].count > rows[j].count
	})
	if limit > len(rows) {
		limit = len(rows)
	}
	out := make([]string, 0, limit)
	for _, r := range rows[:limit] {
		out = append(out, fmt.Sprintf("%-22s %d", r.name, r.count))
	}
	return out
}

func liveEventLine(kind string, at time.Time) string {
	if at.IsZero() && kind == "" {
		return dimStyle.Render("-")
	}
	when := relTime(at)
	if kind == "" {
		return when
	}
	return fmt.Sprintf("%s (%s)", when, kind)
}

func renderSessions(m model) string {
	var b strings.Builder
	b.WriteString(" TITLE                    WORKSPACE              MSGS  UPDATED     ID\n")
	for _, c := range m.data.Composers {
		title := truncate(c.Title, 24)
		if title == "" {
			title = dimStyle.Render("(untitled)")
		}
		ws := truncate(baseName(c.WorkspacePath), 22)
		if ws == "" {
			ws = dimStyle.Render("-")
		}
		b.WriteString(fmt.Sprintf(" %-24s  %-22s  %4d  %-10s  %s\n",
			title, ws, c.MessageCount, relTime(c.UpdatedAt), shortID(c.ID)))
	}
	if len(m.data.Composers) == 0 {
		b.WriteString(dimStyle.Render(" No sessions with titles or activity — press i to ingest\n"))
	}
	return b.String()
}

func renderTools(m model) string {
	var b strings.Builder
	names := make([]string, 0, len(m.data.Tools.ByTool))
	max := 0
	for name, n := range m.data.Tools.ByTool {
		names = append(names, name)
		if n > max {
			max = n
		}
	}
	sort.Strings(names)
	for _, name := range names {
		n := m.data.Tools.ByTool[name]
		bar := barChart(n, max, 20)
		b.WriteString(fmt.Sprintf(" %-12s %s %d\n", name, bar, n))
	}
	if m.data.Tools.Total == 0 {
		b.WriteString(dimStyle.Render(" No tool data yet.\n"))
		b.WriteString(dimStyle.Render(" Press i to ingest transcripts, or use agent tools with hooks + TUI running.\n"))
	} else if m.data.CacheReady {
		if m.data.ToolsLive > 0 {
			b.WriteString(dimStyle.Render(fmt.Sprintf("\n %d tool calls (%d live this session — press i to cache)\n", m.data.Tools.Total, m.data.ToolsLive)))
		} else {
			b.WriteString(dimStyle.Render(fmt.Sprintf("\n %d tool calls (cached — press i after new sessions)\n", m.data.Tools.Total)))
		}
	} else {
		b.WriteString(dimStyle.Render("\n (live scan — press i to cache for stable totals)\n"))
	}
	return b.String()
}

func renderStorage(m model) string {
	s := m.data.Storage
	var b strings.Builder
	if s.GlobalStateDB != nil {
		b.WriteString(fmt.Sprintf(" global state.vscdb: %s (%s)\n", formatBytes(s.GlobalStateDB.SizeBytes), okFail(s.GlobalStateDB.Readable)))
	}
	if s.ProjectsDir != nil {
		b.WriteString(fmt.Sprintf(" projects dir: %s (%s)\n", formatBytes(s.ProjectsDir.SizeBytes), okExist(s.ProjectsDir.Exists)))
	}
	b.WriteString(fmt.Sprintf(" workspaces: %d  transcript files: %d\n", s.WorkspaceCount, s.TranscriptFiles))
	if !m.data.LastIngestAt.IsZero() {
		b.WriteString(fmt.Sprintf(" last ingest: %s\n", m.data.LastIngestAt.Format(time.RFC3339)))
	}
	return b.String()
}

func renderHistory(m model) string {
	var b strings.Builder
	b.WriteString(" DATE        SESSIONS  MESSAGES  TOOLS  FAILURES\n")
	for _, r := range m.data.History {
		b.WriteString(fmt.Sprintf(" %-10s  %8d  %8d  %5d  %8d\n",
			r.Date, r.SessionsStarted, r.AssistantMsgs, r.ToolCalls, r.ToolFailures))
	}
	return b.String()
}

func renderFooter(m model) string {
	hint := "q quit  r refresh  i ingest  1-5 tabs"
	if m.ingesting {
		hint = " ingesting…"
	}
	if m.errText != "" {
		hint = failStyle.Render(m.errText)
	}
	return dimStyle.Render(hint)
}

func sparkline(rows []cursor.DailyRollup, val func(cursor.DailyRollup) float64) string {
	if len(rows) == 0 {
		return dimStyle.Render("▁▁▁▁▁▁▁")
	}
	max := 0.0
	for _, r := range rows {
		if v := val(r); v > max {
			max = v
		}
	}
	chars := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	for _, r := range rows {
		v := val(r)
		idx := 0
		if max > 0 {
			idx = int((v / max) * float64(len(chars)-1))
		}
		b.WriteRune(chars[idx])
	}
	return barFilled.Render(b.String())
}

func barChart(n, max, width int) string {
	if max <= 0 {
		return strings.Repeat("░", width)
	}
	filled := (n * width) / max
	if filled > width {
		filled = width
	}
	return barFilled.Render(strings.Repeat("█", filled)) + barEmpty.Render(strings.Repeat("░", width-filled))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func shortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

func baseName(p string) string {
	if p == "" {
		return ""
	}
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}

func relTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	if d < time.Minute {
		return "now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return t.Format("2006-01-02")
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func okFail(ok bool) string {
	if ok {
		return okStyle.Render("readable")
	}
	return failStyle.Render("missing")
}

func okExist(ok bool) string {
	if ok {
		return okStyle.Render("exists")
	}
	return warnStyle.Render("missing")
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	dimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	okStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	failStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	barFilled  = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	barEmpty   = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
)
