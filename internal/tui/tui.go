package tui

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/aeon022/habctl/internal/ai"
	"github.com/aeon022/habctl/internal/config"
	"github.com/aeon022/habctl/internal/models"
	"github.com/aeon022/habctl/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── colors ───────────────────────────────────────────────────────────────────

var (
	colorLime   = lipgloss.AdaptiveColor{Light: "#65a30d", Dark: "#84cc16"}
	colorMuted  = lipgloss.AdaptiveColor{Light: "#64748b", Dark: "#718096"}
	colorOk     = lipgloss.AdaptiveColor{Light: "#16a34a", Dark: "#4ade80"}
	colorWarn   = lipgloss.AdaptiveColor{Light: "#d97706", Dark: "#fbbf24"}
	colorFg     = lipgloss.AdaptiveColor{Light: "#1e293b", Dark: "#e2e8f0"}
	colorBorder = lipgloss.AdaptiveColor{Light: "#cbd5e1", Dark: "#1e1e2e"}

	styleLime   = lipgloss.NewStyle().Foreground(colorLime)
	styleMuted  = lipgloss.NewStyle().Foreground(colorMuted)
	styleOk     = lipgloss.NewStyle().Foreground(colorOk)
	styleOkBold = lipgloss.NewStyle().Foreground(colorOk).Bold(true)
	styleWarn   = lipgloss.NewStyle().Foreground(colorWarn)
	styleWarnBd = lipgloss.NewStyle().Foreground(colorWarn).Bold(true)
	styleFg     = lipgloss.NewStyle().Foreground(colorFg)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2)
)

// ── provider table ────────────────────────────────────────────────────────────

type providerEntry struct {
	id      ai.Provider
	label   string
	keyPage string // URL to open for API key
	envKey  string // env var that holds the key
}

var providers = []providerEntry{
	{ai.ProviderAnthropic, "Anthropic / Claude", "https://console.anthropic.com/account/keys", "ANTHROPIC_API_KEY"},
	{ai.ProviderOpenAI, "OpenAI / ChatGPT", "https://platform.openai.com/api-keys", "OPENAI_API_KEY"},
	{ai.ProviderGemini, "Google Gemini", "https://aistudio.google.com/apikey", "GEMINI_API_KEY"},
	{ai.ProviderOllama, "Ollama (lokal)", "", ""},
}

// ── view state ───────────────────────────────────────────────────────────────

type viewState int

const (
	viewList viewState = iota
	viewAddInput
	viewHelp
	viewSuggest
	viewSettings
	viewKeyInput
)

// ── messages ─────────────────────────────────────────────────────────────────

type suggestChunkResult struct {
	text string
	done bool
	err  error
}

type suggestChunkMsg string
type suggestDoneMsg struct{}
type suggestErrMsg struct{ err error }

type habitsLoadedMsg []models.HabitStats
type errMsg struct{ err error }
type clearMsgMsg struct{}
type statusMsg string

// ── model ────────────────────────────────────────────────────────────────────

type model struct {
	habits         []models.HabitStats
	cursor         int
	state          viewState
	input          textinput.Model
	s              *store.Store
	message        string
	isErr          bool
	weekView       bool
	suggestText    string
	suggestDone    bool
	suggestCh      <-chan suggestChunkResult
	settingsCursor int
	cfg            config.Config
}

// ── entry point ──────────────────────────────────────────────────────────────

func Run(s *store.Store) error {
	ti := textinput.New()
	ti.Placeholder = "Habit name…"
	ti.CharLimit = 60

	cfg, _ := config.Load()

	m := model{s: s, input: ti, cfg: cfg}
	p := tea.NewProgram(m,
		tea.WithAltScreen(),
		tea.WithInput(os.Stdin),
		tea.WithOutput(os.Stdout),
	)
	_, err := p.Run()
	return err
}

// ── bubbletea interface ───────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	return loadHabits(m.s, 30)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case habitsLoadedMsg:
		m.habits = []models.HabitStats(msg)
		if m.cursor >= len(m.habits) {
			m.cursor = max(0, len(m.habits)-1)
		}
		return m, nil

	case statusMsg:
		m.message = string(msg)
		m.isErr = false
		days := 30
		if m.weekView {
			days = 7
		}
		return m, tea.Batch(loadHabits(m.s, days), clearAfter())

	case errMsg:
		m.message = "✗ " + msg.err.Error()
		m.isErr = true
		return m, clearAfter()

	case clearMsgMsg:
		m.message = ""
		m.isErr = false
		return m, nil

	case suggestChunkMsg:
		m.suggestText += string(msg)
		return m, waitForChunk(m.suggestCh)

	case suggestDoneMsg:
		m.suggestDone = true
		return m, nil

	case suggestErrMsg:
		m.suggestDone = true
		m.suggestText += "\n\n✗ " + msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		switch m.state {
		case viewHelp:
			return m.handleHelp(msg)
		case viewAddInput:
			return m.handleAddInput(msg)
		case viewSuggest:
			return m.handleSuggest(msg)
		case viewSettings:
			return m.handleSettings(msg)
		case viewKeyInput:
			return m.handleKeyInput(msg)
		default:
			return m.handleList(msg)
		}
	}

	if m.state == viewAddInput || m.state == viewKeyInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// ── key handlers ─────────────────────────────────────────────────────────────

func (m model) handleList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "j", "down":
		if m.cursor < len(m.habits)-1 {
			m.cursor++
		}

	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}

	case " ", "enter":
		if len(m.habits) == 0 {
			break
		}
		h := m.habits[m.cursor]
		name := h.Habit.Name
		s := m.s
		return m, func() tea.Msg {
			if err := s.CheckIn(name, time.Now()); err != nil {
				return errMsg{err}
			}
			stats, err := s.GetStats(name, 30)
			if err != nil {
				return statusMsg(fmt.Sprintf("✓ %s", name))
			}
			out := fmt.Sprintf("✓ %s (streak: %d)", name, stats.Streak)
			if stats.Streak >= 7 {
				out += " 🔥"
			}
			if ms := streakMilestone(stats.Streak); ms != "" {
				out += "  " + ms
			}
			return statusMsg(out)
		}

	case "w":
		m.weekView = !m.weekView
		days := 30
		if m.weekView {
			days = 7
		}
		return m, loadHabits(m.s, days)

	case "s":
		m.state = viewSuggest
		m.suggestText = ""
		m.suggestDone = false
		var existing []string
		for _, h := range m.habits {
			existing = append(existing, h.Habit.Name)
		}
		ch := make(chan suggestChunkResult, 64)
		m.suggestCh = ch
		go func() {
			req := ai.SuggestRequest{ExistingHabits: existing, Count: 6}
			_, err := ai.Suggest(req, func(chunk string) {
				ch <- suggestChunkResult{text: chunk}
			})
			if err != nil {
				ch <- suggestChunkResult{err: err}
			}
			ch <- suggestChunkResult{done: true}
		}()
		return m, waitForChunk(m.suggestCh)

	case "S":
		cfg, _ := config.Load()
		m.cfg = cfg
		m.state = viewSettings
		m.settingsCursor = 0

	case "?":
		m.state = viewHelp

	case "n":
		m.state = viewAddInput
		m.input.Reset()
		m.input.Placeholder = "Habit name…"
		m.input.Focus()

	case "d":
		if len(m.habits) == 0 {
			break
		}
		name := m.habits[m.cursor].Habit.Name
		s := m.s
		return m, func() tea.Msg {
			if err := s.DeleteHabit(name); err != nil {
				return errMsg{err}
			}
			return statusMsg("Deleted: " + name)
		}
	}
	return m, nil
}

func (m model) handleHelp(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "?", "esc", "q":
		m.state = viewList
	}
	return m, nil
}

func (m model) handleSuggest(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.state = viewList
	case "n":
		if m.suggestDone {
			m.state = viewAddInput
			m.input.Reset()
			m.input.Placeholder = "Habit name…"
			m.input.Focus()
		}
	}
	return m, nil
}

func (m model) handleSettings(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.state = viewList

	case "j", "down":
		if m.settingsCursor < len(providers)-1 {
			m.settingsCursor++
		}

	case "k", "up":
		if m.settingsCursor > 0 {
			m.settingsCursor--
		}

	case "enter":
		p := providers[m.settingsCursor]
		if p.id == ai.ProviderOllama {
			// Ollama needs no key — just activate.
			m.cfg.Provider = string(ai.ProviderOllama)
			config.Save(m.cfg)
			config.ApplyToEnv(m.cfg)
			m.state = viewList
			m.message = "Ollama aktiv (lokal)"
		} else {
			m.state = viewKeyInput
			m.input.Reset()
			m.input.Placeholder = "API-Key einfügen…"
			// Pre-fill with existing saved key.
			existingKey := os.Getenv(p.envKey)
			if existingKey != "" {
				m.input.SetValue(existingKey)
			}
			m.input.Focus()
		}
	}
	return m, nil
}

func (m model) handleKeyInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit

	case "esc":
		m.state = viewSettings
		return m, nil

	case "o":
		p := providers[m.settingsCursor]
		if p.keyPage != "" {
			go openBrowser(p.keyPage)
		}
		return m, nil

	case "enter":
		key := strings.TrimSpace(m.input.Value())
		p := providers[m.settingsCursor]
		if key != "" {
			switch p.id {
			case ai.ProviderAnthropic:
				m.cfg.AnthropicKey = key
			case ai.ProviderOpenAI:
				m.cfg.OpenAIKey = key
			case ai.ProviderGemini:
				m.cfg.GeminiKey = key
			}
			m.cfg.Provider = string(p.id)
			config.Save(m.cfg)
			config.ApplyToEnv(m.cfg)
			m.state = viewList
			m.message = p.label + " eingerichtet"
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) handleAddInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = viewList
		return m, nil

	case "enter":
		name := strings.TrimSpace(m.input.Value())
		m.state = viewList
		if name == "" {
			return m, nil
		}
		s := m.s
		return m, func() tea.Msg {
			if _, err := s.AddHabit(name, ""); err != nil {
				return errMsg{err}
			}
			return statusMsg("+ Added: " + name)
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// ── view ──────────────────────────────────────────────────────────────────────

func (m model) View() string {
	switch m.state {
	case viewHelp:
		return m.renderHelp()
	case viewAddInput:
		return m.renderAdd()
	case viewSuggest:
		return m.renderSuggest()
	case viewSettings:
		return m.renderSettings()
	case viewKeyInput:
		return m.renderKeyInput()
	default:
		return m.renderList()
	}
}

func (m model) renderList() string {
	var b strings.Builder

	days := 30
	if m.weekView {
		days = 7
	}
	dayLabel := fmt.Sprintf("%dd", days)

	title := styleLime.Bold(true).Render("habctl")
	viewToggle := styleMuted.Render(fmt.Sprintf("[w] %s view", dayLabel))
	b.WriteString(title + "  " + viewToggle + "\n\n")

	if len(m.habits) == 0 {
		b.WriteString(styleMuted.Render("No habits yet. Press n to add one.") + "\n")
	} else {
		header := fmt.Sprintf("  %-24s  %-7s  %-14s  %s", "Habit", "today", "streak", dayLabel)
		b.WriteString(styleMuted.Render(header) + "\n")

		for i, h := range m.habits {
			atRisk := h.Streak > 0 && !h.CheckedToday

			cursor := "  "
			nameStyle := styleMuted
			if atRisk && i != m.cursor {
				nameStyle = styleWarn
			}
			if i == m.cursor {
				cursor = styleLime.Render("▶ ")
				nameStyle = styleFg
			}

			todayStr := styleMuted.Render("–")
			if h.CheckedToday {
				todayStr = styleOk.Render("✓")
			} else if atRisk {
				todayStr = styleWarn.Render("!")
			}

			streakNum := fmt.Sprintf("%d day", h.Streak)
			if h.Streak != 1 {
				streakNum += "s"
			}
			var streakStr string
			switch {
			case h.CheckedToday && h.Streak > 0:
				streakStr = styleOkBold.Render(streakNum)
			case atRisk:
				streakStr = styleWarnBd.Render(streakNum)
			default:
				streakStr = styleMuted.Render(streakNum)
			}
			if h.Streak >= 7 {
				streakStr += " 🔥"
			}

			bar := progressBar(h.TotalDays, days, 18)
			pct := 0
			if h.TotalDays > 0 {
				pct = int(math.Round(float64(h.TotalDays) / float64(days) * 100))
			}
			barStyle := styleLime
			if atRisk {
				barStyle = styleWarn
			}
			barStr := barStyle.Render(bar) + styleMuted.Render(fmt.Sprintf(" %d%%", pct))

			name := nameStyle.Render(truncate(h.Habit.Name, 24))
			b.WriteString(fmt.Sprintf("%s%-26s  %-9s  %-20s  %s\n",
				cursor, name, todayStr, streakStr, barStr))
		}
	}

	b.WriteString("\n")

	if m.message != "" {
		msgStyle := styleOk
		if m.isErr {
			msgStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		}
		b.WriteString(msgStyle.Render(m.message) + "\n\n")
	}

	b.WriteString(styleMuted.Render("space check in · n new · d delete · s KI-Vorschläge · S settings · w 7d/30d · ? help · q quit"))
	return panelStyle.Render(b.String())
}

func (m model) renderSettings() string {
	var b strings.Builder

	b.WriteString(styleLime.Bold(true).Render("KI-Provider") + "\n")
	b.WriteString(styleMuted.Render("Wähle deinen KI-Anbieter und richte ihn ein.") + "\n\n")

	// Detect active provider from env.
	activeProvider := ai.Provider(os.Getenv("HABCTL_PROVIDER"))
	if activeProvider == "" {
		if info, err := ai.Detect(); err == nil {
			activeProvider = info.Name
		}
	}

	for i, p := range providers {
		selected := i == m.settingsCursor
		active := p.id == activeProvider

		cursor := "  "
		if selected {
			cursor = styleLime.Render("▶ ")
		}

		// Label: 26 chars wide via lipgloss (ANSI-safe).
		var labelStyle lipgloss.Style
		switch {
		case selected && active:
			labelStyle = lipgloss.NewStyle().Foreground(colorLime).Bold(true)
		case selected:
			labelStyle = lipgloss.NewStyle().Foreground(colorFg)
		case active:
			labelStyle = lipgloss.NewStyle().Foreground(colorLime)
		default:
			labelStyle = lipgloss.NewStyle().Foreground(colorMuted)
		}
		label := labelStyle.Width(26).Render(p.label)

		// Status badge.
		var badge string
		if p.id == ai.ProviderOllama {
			badge = styleOk.Render("● lokal")
		} else {
			key := os.Getenv(p.envKey)
			if key != "" {
				badge = styleOk.Render("● Key gesetzt")
			} else {
				badge = styleMuted.Render("○ kein Key")
			}
		}
		if active {
			badge += " " + styleLime.Render("← aktiv")
		}

		b.WriteString(cursor + label + "  " + badge + "\n")
	}

	b.WriteString("\n" + styleMuted.Render("enter konfigurieren · j/k navigieren · esc zurück"))
	return panelStyle.Render(b.String())
}

func (m model) renderKeyInput() string {
	var b strings.Builder

	p := providers[m.settingsCursor]

	b.WriteString(styleLime.Bold(true).Render(p.label+" einrichten") + "\n\n")

	if p.keyPage != "" {
		b.WriteString(styleMuted.Render("Schritt 1 — API-Key holen:") + "\n")
		b.WriteString("  " + styleOk.Render("o") + styleMuted.Render("  öffnet "+p.keyPage) + "\n\n")
		b.WriteString(styleMuted.Render("Schritt 2 — Key einfügen (Cmd+V):") + "\n")
	} else {
		b.WriteString(styleMuted.Render("API-Key eingeben:") + "\n")
	}

	b.WriteString("  " + m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter speichern · o Browser öffnen · esc zurück"))
	return panelStyle.Render(b.String())
}

func (m model) renderSuggest() string {
	var b strings.Builder

	providerLabel := ""
	if info, err := ai.Detect(); err == nil {
		providerLabel = styleMuted.Render("via " + info.Display)
	} else {
		providerLabel = styleWarn.Render("kein Provider — S für Settings")
	}
	b.WriteString(styleLime.Bold(true).Render("Habit-Vorschläge") + "  " + providerLabel + "\n\n")

	if m.suggestText == "" {
		b.WriteString(styleMuted.Render("Generiere Vorschläge…") + "\n")
	} else {
		b.WriteString(m.suggestText)
	}

	if m.suggestDone {
		b.WriteString("\n\n" + styleMuted.Render("n neuen Habit hinzufügen · esc zurück"))
	} else {
		b.WriteString(styleLime.Render("▌"))
	}

	return panelStyle.Render(b.String())
}

func (m model) renderAdd() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("New habit") + "\n\n")
	b.WriteString(m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter save · esc cancel"))
	return panelStyle.Render(b.String())
}

func (m model) renderHelp() string {
	lime := styleLime.Bold(true)
	key := lipgloss.NewStyle().Foreground(colorLime).Width(22)
	desc := styleMuted

	row := func(k, d string) string {
		return "  " + key.Render(k) + desc.Render(d) + "\n"
	}
	section := func(title string) string {
		return "\n  " + lime.Render(title) + "\n"
	}

	var b strings.Builder

	b.WriteString(lime.Render("habctl") + styleMuted.Render(" — daily habit tracker") + "\n\n")
	b.WriteString(styleMuted.Render(
		"  Track habits every day. Build streaks. Miss a day and\n" +
			"  the streak resets — simple, honest accountability.\n",
	))

	b.WriteString(section("Navigation"))
	b.WriteString(row("j / ↓", "move down"))
	b.WriteString(row("k / ↑", "move up"))

	b.WriteString(section("Actions"))
	b.WriteString(row("space / enter", "check in today"))
	b.WriteString(row("n", "add new habit"))
	b.WriteString(row("d", "delete selected habit"))
	b.WriteString(row("s", "KI-Vorschläge (aktiver Provider)"))
	b.WriteString(row("S", "Settings — Provider & API-Keys"))

	b.WriteString(section("View"))
	b.WriteString(row("w", "toggle 7d / 30d view"))

	b.WriteString(section("Status colors"))
	b.WriteString(row(styleOk.Render("✓  green"), "checked in today"))
	b.WriteString(row(styleWarn.Render("!  amber"), "streak at risk — check in before midnight!"))
	b.WriteString(row(styleMuted.Render("–  gray"), "no active streak"))

	b.WriteString(section("Other"))
	b.WriteString(row("?", "toggle this help screen"))
	b.WriteString(row("q / ctrl+c", "quit"))

	b.WriteString("\n  " + styleMuted.Render("esc / ?   close help"))
	return panelStyle.Render(b.String())
}

// ── commands ──────────────────────────────────────────────────────────────────

func loadHabits(s *store.Store, days int) tea.Cmd {
	return func() tea.Msg {
		stats, err := s.GetAllStats(days)
		if err != nil {
			return errMsg{err}
		}
		return habitsLoadedMsg(stats)
	}
}

func waitForChunk(ch <-chan suggestChunkResult) tea.Cmd {
	return func() tea.Msg {
		r := <-ch
		if r.err != nil {
			return suggestErrMsg{r.err}
		}
		if r.done {
			return suggestDoneMsg{}
		}
		return suggestChunkMsg(r.text)
	}
}

func clearAfter() tea.Cmd {
	return tea.Tick(3*time.Second, func(_ time.Time) tea.Msg {
		return clearMsgMsg{}
	})
}

// ── util ─────────────────────────────────────────────────────────────────────

func progressBar(done, total, width int) string {
	if total == 0 {
		return strings.Repeat("░", width)
	}
	filled := int(math.Round(float64(done) / float64(total) * float64(width)))
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}

func streakMilestone(n int) string {
	switch n {
	case 7:
		return "🎯 One week!"
	case 14:
		return "💪 Two weeks!"
	case 21:
		return "🧠 21 days!"
	case 30:
		return "🏆 One month!"
	case 60:
		return "🌟 60 days!"
	case 100:
		return "🎉 100 days!!"
	}
	return ""
}

func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd, args = "open", []string{url}
	case "linux":
		cmd, args = "xdg-open", []string{url}
	default:
		cmd, args = "cmd", []string{"/c", "start", url}
	}
	exec.Command(cmd, args...).Start()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
