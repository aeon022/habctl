package tui

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

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
	colorFg     = lipgloss.AdaptiveColor{Light: "#1e293b", Dark: "#e2e8f0"}
	colorBorder = lipgloss.AdaptiveColor{Light: "#cbd5e1", Dark: "#1e1e2e"}

	styleLime   = lipgloss.NewStyle().Foreground(colorLime)
	styleMuted  = lipgloss.NewStyle().Foreground(colorMuted)
	styleOk     = lipgloss.NewStyle().Foreground(colorOk)
	styleOkBold = lipgloss.NewStyle().Foreground(colorOk).Bold(true)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2)
)

// ── view state ───────────────────────────────────────────────────────────────

type viewState int

const (
	viewList viewState = iota
	viewAddInput
	viewHelp
)

// ── messages ─────────────────────────────────────────────────────────────────

type habitsLoadedMsg []models.HabitStats
type errMsg struct{ err error }
type clearMsgMsg struct{}
type statusMsg string

// ── model ────────────────────────────────────────────────────────────────────

type model struct {
	habits  []models.HabitStats
	cursor  int
	state   viewState
	input   textinput.Model
	s       *store.Store
	message string
	isErr   bool
}

// ── entry point ──────────────────────────────────────────────────────────────

func Run(s *store.Store) error {
	ti := textinput.New()
	ti.Placeholder = "Habit name…"
	ti.CharLimit = 60

	m := model{s: s, input: ti}
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
	return loadHabits(m.s)
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
		return m, tea.Batch(loadHabits(m.s), clearAfter())

	case errMsg:
		m.message = "✗ " + msg.err.Error()
		m.isErr = true
		return m, clearAfter()

	case clearMsgMsg:
		m.message = ""
		m.isErr = false
		return m, nil

	case tea.KeyMsg:
		if m.state == viewHelp {
			switch msg.String() {
			case "?", "esc", "q", "ctrl+c":
				if msg.String() == "ctrl+c" {
					return m, tea.Quit
				}
				m.state = viewList
			}
			return m, nil
		}
		if m.state == viewAddInput {
			return m.handleAddInput(msg)
		}
		return m.handleList(msg)
	}

	if m.state == viewAddInput {
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

	case "?":
		m.state = viewHelp

	case "n":
		m.state = viewAddInput
		m.input.Reset()
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
	default:
		return m.renderList()
	}
}

func (m model) renderList() string {
	var b strings.Builder

	b.WriteString(styleLime.Bold(true).Render("habctl") + "\n\n")

	if len(m.habits) == 0 {
		b.WriteString(styleMuted.Render("No habits yet. Press n to add one.") + "\n")
	} else {
		header := fmt.Sprintf("  %-24s  %-7s  %-14s  %s", "Habit", "today", "streak", "30-day")
		b.WriteString(styleMuted.Render(header) + "\n")

		for i, h := range m.habits {
			cursor := "  "
			nameStyle := styleMuted
			if i == m.cursor {
				cursor = styleLime.Render("▶ ")
				nameStyle = lipgloss.NewStyle().Foreground(colorFg)
			}

			todayStr := styleMuted.Render("–")
			if h.CheckedToday {
				todayStr = styleOk.Render("✓")
			}

			streakNum := fmt.Sprintf("%d day", h.Streak)
			if h.Streak != 1 {
				streakNum += "s"
			}
			streakStyle := styleMuted
			if h.CheckedToday && h.Streak > 0 {
				streakStyle = styleOkBold
			}
			streakStr := streakStyle.Render(streakNum)
			if h.Streak >= 7 {
				streakStr += " 🔥"
			}

			bar := progressBar(h.TotalDays, 30, 18)
			pct := 0
			if h.TotalDays > 0 {
				pct = int(math.Round(float64(h.TotalDays) / 30.0 * 100))
			}
			barStr := styleLime.Render(bar) + styleMuted.Render(fmt.Sprintf(" %d%%", pct))

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

	b.WriteString(styleMuted.Render("space/enter check in · n new · d delete · ? help · q quit"))
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
	key := lipgloss.NewStyle().Foreground(colorLime).Width(18)
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

	b.WriteString(section("Columns"))
	b.WriteString(row("today", "✓ = done today  –  = not yet"))
	b.WriteString(row("streak", "consecutive days ending today"))
	b.WriteString(row("30-day", "█░ bar = completion rate, last 30 days"))

	b.WriteString(section("Other"))
	b.WriteString(row("?", "toggle this help screen"))
	b.WriteString(row("q / ctrl+c", "quit"))

	b.WriteString("\n  " + styleMuted.Render("esc / ?   close help"))

	return panelStyle.Render(b.String())
}

// ── commands ──────────────────────────────────────────────────────────────────

func loadHabits(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		stats, err := s.GetAllStats(30)
		if err != nil {
			return errMsg{err}
		}
		return habitsLoadedMsg(stats)
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
