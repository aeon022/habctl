package tui

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aeon022/habctl/internal/ai"
	"github.com/aeon022/habctl/internal/auth"
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
	colorGroup  = lipgloss.AdaptiveColor{Light: "#0ea5e9", Dark: "#38bdf8"}

	styleLime   = lipgloss.NewStyle().Foreground(colorLime)
	styleMuted  = lipgloss.NewStyle().Foreground(colorMuted)
	styleOk     = lipgloss.NewStyle().Foreground(colorOk)
	styleOkBold = lipgloss.NewStyle().Foreground(colorOk).Bold(true)
	styleWarn   = lipgloss.NewStyle().Foreground(colorWarn)
	styleWarnBd = lipgloss.NewStyle().Foreground(colorWarn).Bold(true)
	styleFg     = lipgloss.NewStyle().Foreground(colorFg)
	styleGroup  = lipgloss.NewStyle().Foreground(colorGroup).Bold(true)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2)
)

// ── provider table ────────────────────────────────────────────────────────────

type providerEntry struct {
	id      ai.Provider
	label   string
	keyPage string
	envKey  string
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
	viewAddInput      // step 1: habit name (+ optional emoji prefix)
	viewAddDesc       // step 2: description (optional)
	viewHelp
	viewSuggest
	viewSettings
	viewKeyInput
	viewStats
	viewEditHabit  // edit name/icon/description of selected habit
	viewGroupMgr   // manage groups list
	viewGroupNew   // create new group (name + icon)
	viewGroupPick  // assign a habit to a group
	viewReview     // weekly AI coaching briefing
	viewNoteInput  // add/edit note for today's check-in
	viewChainMgr   // manage habit chains
	viewChainPick  // pick target habit when creating a chain
	viewGeminiMenu
	viewGeminiCID
	viewGeminiCS
	viewOAuthWait
)

// ── messages ─────────────────────────────────────────────────────────────────

type suggestChunkResult struct {
	text string
	done bool
	err  error
	gen  int
}

type suggestChunkMsg struct {
	text string
	gen  int
}
type suggestDoneMsg struct{ gen int }
type suggestErrMsg struct {
	err error
	gen int
}

type suggestItem struct {
	name     string   // text from **...**  — used when adding to DB
	header   string   // first line without ** (name + time)
	details  []string // description and tip lines
	selected bool
}

type reviewChunkResult struct {
	text string
	done bool
	err  error
	gen  int
}
type reviewChunkMsg struct {
	text string
	gen  int
}
type reviewDoneMsg struct{ gen int }
type reviewErrMsg struct {
	err error
	gen int
}

type oauthSuccessMsg struct{ refreshToken string }
type oauthErrMsg struct{ err error }

type habitsLoadedMsg []models.HabitStats
type groupsLoadedMsg []models.Group
type chainsLoadedMsg []models.Chain
type errMsg struct{ err error }
type clearMsgMsg struct{}
type statusMsg string

// ── model ────────────────────────────────────────────────────────────────────

type model struct {
	habits   []models.HabitStats
	groups   []models.Group
	chains   []models.Chain
	cursor   int
	state    viewState
	input    textinput.Model
	s        *store.Store
	message  string
	isErr    bool
	weekView bool

	suggestText   string
	suggestDone   bool
	suggestCh     <-chan suggestChunkResult
	suggestGen    int
	suggestCancel context.CancelFunc
	suggestItems  []suggestItem
	suggestCursor int

	// weekly review (AI briefing)
	reviewText   string
	reviewDone   bool
	reviewCh     <-chan reviewChunkResult
	reviewGen    int
	reviewCancel context.CancelFunc

	settingsCursor   int
	geminiMenuCursor int
	geminiClientID   string

	// add-habit flow
	addingName string
	addingIcon string

	// edit-habit flow
	editCursor  int
	editOldName string

	// group management
	groupCursor int

	// chain management
	chainCursor     int // cursor in viewChainMgr
	chainPickCursor int // cursor in viewChainPick
	chainFromName   string // habit name selected as chain source

	// note flow: add note to today's check-in
	noteForHabit string // habit name the note belongs to

	cfg     config.Config
	calData store.CalendarData
	width   int
}

// ── entry point ──────────────────────────────────────────────────────────────

func Run(s *store.Store) error {
	ti := textinput.New()
	ti.Placeholder = "Habit name…"
	ti.CharLimit = 80

	cfg, _ := config.Load()
	m := model{s: s, input: ti, cfg: cfg}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithInput(os.Stdin), tea.WithOutput(os.Stdout))
	_, err := p.Run()
	return err
}

// ── bubbletea interface ───────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	return tea.Batch(loadHabits(m.s, 30), loadGroups(m.s), loadChains(m.s))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case habitsLoadedMsg:
		m.habits = []models.HabitStats(msg)
		if m.cursor >= len(m.habits) {
			m.cursor = max(0, len(m.habits)-1)
		}
		return m, nil

	case groupsLoadedMsg:
		m.groups = []models.Group(msg)
		return m, nil

	case chainsLoadedMsg:
		m.chains = []models.Chain(msg)
		return m, nil

	case statusMsg:
		m.message = string(msg)
		m.isErr = false
		days := 30
		if m.weekView {
			days = 7
		}
		return m, tea.Batch(loadHabits(m.s, days), loadGroups(m.s), loadChains(m.s), clearAfter())

	case errMsg:
		m.message = "✗ " + msg.err.Error()
		m.isErr = true
		return m, clearAfter()

	case clearMsgMsg:
		m.message = ""
		m.isErr = false
		return m, nil

	case suggestChunkMsg:
		if msg.gen == m.suggestGen {
			m.suggestText += msg.text
		}
		return m, waitForChunk(m.suggestCh)

	case suggestDoneMsg:
		if msg.gen == m.suggestGen {
			m.suggestDone = true
			m.suggestItems = parseSuggestions(m.suggestText)
			m.suggestCursor = 0
		}
		return m, nil

	case suggestErrMsg:
		if msg.gen == m.suggestGen {
			m.suggestDone = true
			m.suggestText += "\n\n✗ " + msg.err.Error()
		}
		return m, nil

	case reviewChunkMsg:
		if msg.gen == m.reviewGen {
			m.reviewText += msg.text
		}
		return m, waitForReviewChunk(m.reviewCh)

	case reviewDoneMsg:
		if msg.gen == m.reviewGen {
			m.reviewDone = true
		}
		return m, nil

	case reviewErrMsg:
		if msg.gen == m.reviewGen {
			m.reviewDone = true
			m.reviewText += "\n\n✗ " + msg.err.Error()
		}
		return m, nil

	case oauthSuccessMsg:
		m.cfg.GoogleRefreshToken = msg.refreshToken
		config.Save(m.cfg)
		config.ForceApplyToEnv(m.cfg)
		m.state = viewList
		m.message = "Google-Login erfolgreich — Gemini aktiv"
		return m, nil

	case oauthErrMsg:
		m.state = viewSettings
		m.message = "Login fehlgeschlagen: " + msg.err.Error()
		m.isErr = true
		return m, clearAfter()

	case tea.KeyMsg:
		switch m.state {
		case viewHelp:
			return m.handleHelp(msg)
		case viewAddInput:
			return m.handleAddInput(msg)
		case viewAddDesc:
			return m.handleAddDesc(msg)
		case viewSuggest:
			return m.handleSuggest(msg)
		case viewSettings:
			return m.handleSettings(msg)
		case viewKeyInput:
			return m.handleKeyInput(msg)
		case viewStats:
			return m.handleStats(msg)
		case viewEditHabit:
			return m.handleEditHabit(msg)
		case viewGroupMgr:
			return m.handleGroupMgr(msg)
		case viewGroupNew:
			return m.handleGroupNew(msg)
		case viewGroupPick:
			return m.handleGroupPick(msg)
		case viewReview:
			return m.handleReview(msg)
		case viewNoteInput:
			return m.handleNoteInput(msg)
		case viewChainMgr:
			return m.handleChainMgr(msg)
		case viewChainPick:
			return m.handleChainPick(msg)
		case viewGeminiMenu:
			return m.handleGeminiMenu(msg)
		case viewGeminiCID:
			return m.handleGeminiCID(msg)
		case viewGeminiCS:
			return m.handleGeminiCS(msg)
		case viewOAuthWait:
			if msg.String() == "ctrl+c" {
				return m, tea.Quit
			}
		default:
			return m.handleList(msg)
		}
	}

	if m.state == viewAddInput || m.state == viewAddDesc ||
		m.state == viewKeyInput || m.state == viewEditHabit ||
		m.state == viewGeminiCID || m.state == viewGeminiCS ||
		m.state == viewGroupNew || m.state == viewNoteInput {
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
		if m.suggestCancel != nil {
			m.suggestCancel()
		}
		m.state = viewSuggest
		m.suggestText = ""
		m.suggestDone = false
		m.suggestItems = nil
		m.suggestCursor = 0
		m.suggestGen++
		existing := make([]string, 0, len(m.habits))
		rates := make(map[string]float64, len(m.habits))
		for _, h := range m.habits {
			existing = append(existing, h.Habit.Name)
			done := 0
			for _, v := range h.Last7Days {
				if v {
					done++
				}
			}
			rates[h.Habit.Name] = float64(done) / 7.0
		}
		ch := make(chan suggestChunkResult, 64)
		m.suggestCh = ch
		gen := m.suggestGen
		ctx, cancel := context.WithCancel(context.Background())
		m.suggestCancel = cancel
		go func() {
			defer cancel()
			req := ai.SuggestRequest{ExistingHabits: existing, CompletionRates: rates, Count: 3}
			_, err := ai.Suggest(ctx, req, func(chunk string) {
				select {
				case ch <- suggestChunkResult{text: chunk, gen: gen}:
				case <-ctx.Done():
				}
			})
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				select {
				case ch <- suggestChunkResult{err: err, gen: gen}:
				case <-ctx.Done():
				}
				return
			}
			select {
			case ch <- suggestChunkResult{done: true, gen: gen}:
			case <-ctx.Done():
			}
		}()
		return m, waitForChunk(m.suggestCh)

	case "r":
		if m.reviewCancel != nil {
			m.reviewCancel()
		}
		m.state = viewReview
		m.reviewText = ""
		m.reviewDone = false
		m.reviewGen++
		rch := make(chan reviewChunkResult, 64)
		m.reviewCh = rch
		gen := m.reviewGen
		ctx, cancel := context.WithCancel(context.Background())
		m.reviewCancel = cancel
		s := m.s
		go func() {
			defer cancel()
			data, err := s.GetWeeklyReview()
			if err != nil {
				select {
				case rch <- reviewChunkResult{err: err, gen: gen}:
				case <-ctx.Done():
				}
				return
			}
			_, err = ai.Review(ctx, data, func(chunk string) {
				select {
				case rch <- reviewChunkResult{text: chunk, gen: gen}:
				case <-ctx.Done():
				}
			})
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				select {
				case rch <- reviewChunkResult{err: err, gen: gen}:
				case <-ctx.Done():
				}
				return
			}
			select {
			case rch <- reviewChunkResult{done: true, gen: gen}:
			case <-ctx.Done():
			}
		}()
		return m, waitForReviewChunk(m.reviewCh)

	case "N":
		if len(m.habits) == 0 {
			break
		}
		h := m.habits[m.cursor]
		if !h.CheckedToday {
			break // can only add note if already checked in today
		}
		m.noteForHabit = h.Habit.Name
		m.state = viewNoteInput
		m.input.Reset()
		m.input.SetValue(h.TodayNote)
		m.input.CursorEnd()
		m.input.Placeholder = "Notiz für heute…"
		m.input.CharLimit = 200
		m.input.Focus()

	case "c":
		m.state = viewChainMgr
		m.chainCursor = 0

	case "t":
		cal, err := m.s.GetCalendarData(26)
		if err == nil {
			m.calData = cal
		}
		m.state = viewStats

	case "n":
		m.state = viewAddInput
		m.input.Reset()
		m.input.Placeholder = "Habit (optional: 🏃 Morning run)"
		m.input.CharLimit = 80
		m.input.Focus()
		m.addingName = ""
		m.addingIcon = ""

	case "e":
		if len(m.habits) == 0 {
			break
		}
		h := m.habits[m.cursor].Habit
		m.editOldName = h.Name
		m.editCursor = 0
		m.state = viewEditHabit
		// pre-fill with icon + name
		combined := h.Name
		if h.Icon != "" {
			combined = h.Icon + " " + h.Name
		}
		m.input.Reset()
		m.input.SetValue(combined)
		m.input.CursorEnd()
		m.input.Placeholder = ""
		m.input.Focus()

	case "E":
		if len(m.habits) == 0 {
			break
		}
		h := m.habits[m.cursor].Habit
		m.editOldName = h.Name
		m.editCursor = 1
		m.state = viewEditHabit
		m.input.Reset()
		m.input.SetValue(h.Description)
		m.input.CursorEnd()
		m.input.Placeholder = ""
		m.input.Focus()

	case "m":
		if len(m.habits) == 0 {
			break
		}
		m.state = viewGroupPick
		m.groupCursor = 0

	case "G":
		m.state = viewGroupMgr
		m.groupCursor = 0

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
			return statusMsg("Gelöscht: " + name)
		}

	case "S":
		cfg, _ := config.Load()
		m.cfg = cfg
		config.ForceApplyToEnv(cfg)
		m.state = viewSettings
		m.settingsCursor = 0

	case "?":
		m.state = viewHelp
	}
	return m, nil
}

func (m model) handleAddInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.state = viewList
		return m, nil
	case "enter":
		raw := strings.TrimSpace(m.input.Value())
		if raw == "" {
			return m, nil
		}
		icon, name := splitIcon(raw)
		if name == "" {
			name = raw
			icon = ""
		}
		m.addingName = name
		m.addingIcon = icon
		m.state = viewAddDesc
		m.input.Reset()
		m.input.Placeholder = "// Notiz (optional, enter zum Überspringen)"
		m.input.Focus()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) handleAddDesc(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "enter":
		desc := ""
		if msg.String() == "enter" {
			desc = strings.TrimSpace(m.input.Value())
		}
		name := m.addingName
		icon := m.addingIcon
		s := m.s
		m.state = viewList
		return m, func() tea.Msg {
			if _, err := s.AddHabit(name, desc, icon); err != nil {
				return errMsg{err}
			}
			return statusMsg("+ " + name)
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) handleEditHabit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.state = viewList
		return m, nil
	case "tab":
		// switch between name+icon and description field
		val := m.input.Value()
		if m.editCursor == 0 {
			// Save the name/icon field temporarily and switch to description
			h := m.habits[m.cursor].Habit
			m.editCursor = 1
			m.input.Reset()
			m.input.SetValue(h.Description)
			m.input.CursorEnd()
		} else {
			m.editCursor = 0
			h := m.habits[m.cursor].Habit
			combined := h.Name
			if h.Icon != "" {
				combined = h.Icon + " " + h.Name
			}
			m.input.Reset()
			m.input.SetValue(combined)
			m.input.CursorEnd()
		}
		_ = val
		return m, nil
	case "enter":
		val := strings.TrimSpace(m.input.Value())
		oldName := m.editOldName
		// Fetch current habit data
		var currentHabit models.Habit
		for _, h := range m.habits {
			if h.Habit.Name == oldName {
				currentHabit = h.Habit
				break
			}
		}
		var newName, newIcon, newDesc string
		if m.editCursor == 0 {
			// editing name + icon
			newIcon, newName = splitIcon(val)
			if newName == "" {
				newName = val
				newIcon = ""
			}
			newDesc = currentHabit.Description
		} else {
			// editing description
			newName = currentHabit.Name
			newIcon = currentHabit.Icon
			newDesc = val
		}
		if newName == "" {
			return m, nil
		}
		s := m.s
		m.state = viewList
		return m, func() tea.Msg {
			if err := s.UpdateHabit(oldName, newName, newIcon, newDesc); err != nil {
				return errMsg{err}
			}
			return statusMsg("✓ " + newName + " aktualisiert")
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) handleGroupMgr(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.state = viewList
	case "j", "down":
		if m.groupCursor < len(m.groups)-1 {
			m.groupCursor++
		}
	case "k", "up":
		if m.groupCursor > 0 {
			m.groupCursor--
		}
	case "a":
		m.state = viewGroupNew
		m.input.Reset()
		m.input.Placeholder = "🌅 Morgen (emoji + Name)"
		m.input.Focus()
	case "d":
		if len(m.groups) == 0 {
			break
		}
		g := m.groups[m.groupCursor]
		s := m.s
		return m, func() tea.Msg {
			if err := s.DeleteGroup(g.ID); err != nil {
				return errMsg{err}
			}
			return statusMsg("Gruppe gelöscht: " + g.Name)
		}
	}
	return m, nil
}

func (m model) handleGroupNew(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.state = viewGroupMgr
		return m, nil
	case "enter":
		raw := strings.TrimSpace(m.input.Value())
		if raw == "" {
			return m, nil
		}
		icon, name := splitIcon(raw)
		if name == "" {
			name = raw
			icon = ""
		}
		s := m.s
		m.state = viewGroupMgr
		return m, func() tea.Msg {
			if _, err := s.AddGroup(name, icon); err != nil {
				return errMsg{err}
			}
			return statusMsg("Gruppe erstellt: " + name)
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) handleGroupPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// +1 because index 0 = "Kein" (ungrouped)
	total := len(m.groups) + 1
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.state = viewList
	case "j", "down":
		if m.groupCursor < total-1 {
			m.groupCursor++
		}
	case "k", "up":
		if m.groupCursor > 0 {
			m.groupCursor--
		}
	case "enter":
		if len(m.habits) == 0 {
			break
		}
		habitName := m.habits[m.cursor].Habit.Name
		var groupID int64
		if m.groupCursor > 0 {
			groupID = m.groups[m.groupCursor-1].ID
		}
		s := m.s
		m.state = viewList
		return m, func() tea.Msg {
			if err := s.SetHabitGroup(habitName, groupID); err != nil {
				return errMsg{err}
			}
			return statusMsg("✓ Gruppe zugewiesen")
		}
	}
	return m, nil
}

func (m model) handleReview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		if m.reviewCancel != nil {
			m.reviewCancel()
		}
		m.state = viewList
	}
	return m, nil
}

func (m model) handleNoteInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.state = viewList
		return m, nil
	case "enter":
		note := strings.TrimSpace(m.input.Value())
		name := m.noteForHabit
		s := m.s
		m.state = viewList
		return m, func() tea.Msg {
			if err := s.CheckInWithNote(name, time.Now(), note); err != nil {
				return errMsg{err}
			}
			if note != "" {
				return statusMsg("Notiz gespeichert für " + name)
			}
			return statusMsg("Notiz gelöscht für " + name)
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) handleChainMgr(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.state = viewList
	case "j", "down":
		if m.chainCursor < len(m.chains)-1 {
			m.chainCursor++
		}
	case "k", "up":
		if m.chainCursor > 0 {
			m.chainCursor--
		}
	case "a":
		if len(m.habits) < 2 {
			break
		}
		m.chainFromName = ""
		m.chainPickCursor = 0
		m.state = viewChainPick
	case "d":
		if len(m.chains) == 0 {
			break
		}
		ch := m.chains[m.chainCursor]
		s := m.s
		return m, func() tea.Msg {
			if err := s.DeleteChain(ch.ID); err != nil {
				return errMsg{err}
			}
			return statusMsg("Kette gelöscht")
		}
	case "s":
		// AI chain suggestions
		if m.suggestCancel != nil {
			m.suggestCancel()
		}
		habits := make([]string, 0, len(m.habits))
		for _, h := range m.habits {
			name := h.Habit.Name
			if h.Habit.Icon != "" {
				name = h.Habit.Icon + " " + name
			}
			habits = append(habits, name)
		}
		m.state = viewSuggest
		m.suggestText = ""
		m.suggestDone = false
		m.suggestItems = nil
		m.suggestCursor = 0
		m.suggestGen++
		rch := make(chan suggestChunkResult, 64)
		m.suggestCh = rch
		gen := m.suggestGen
		ctx, cancel := context.WithCancel(context.Background())
		m.suggestCancel = cancel
		go func() {
			defer cancel()
			_, err := ai.SuggestChains(ctx, habits, func(chunk string) {
				select {
				case rch <- suggestChunkResult{text: chunk, gen: gen}:
				case <-ctx.Done():
				}
			})
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				select {
				case rch <- suggestChunkResult{err: err, gen: gen}:
				case <-ctx.Done():
				}
				return
			}
			select {
			case rch <- suggestChunkResult{done: true, gen: gen}:
			case <-ctx.Done():
			}
		}()
		return m, waitForChunk(m.suggestCh)
	}
	return m, nil
}

func (m model) handleChainPick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.state = viewChainMgr
	case "j", "down":
		if m.chainPickCursor < len(m.habits)-1 {
			m.chainPickCursor++
		}
	case "k", "up":
		if m.chainPickCursor > 0 {
			m.chainPickCursor--
		}
	case "enter":
		if len(m.habits) == 0 {
			break
		}
		picked := m.habits[m.chainPickCursor].Habit.Name
		if m.chainFromName == "" {
			// first pick: set source habit
			m.chainFromName = picked
			m.chainPickCursor = 0
			return m, nil
		}
		// second pick: create chain
		from := m.chainFromName
		to := picked
		s := m.s
		m.state = viewChainMgr
		return m, func() tea.Msg {
			if err := s.AddChain(from, to); err != nil {
				return errMsg{err}
			}
			return statusMsg("Kette erstellt: " + from + " → " + to)
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
		if m.suggestCancel != nil {
			m.suggestCancel()
			m.suggestCancel = nil
		}
		m.state = viewList

	case "j", "down":
		if m.suggestCursor < len(m.suggestItems)-1 {
			m.suggestCursor++
		}

	case "k", "up":
		if m.suggestCursor > 0 {
			m.suggestCursor--
		}

	case " ":
		if m.suggestCursor < len(m.suggestItems) {
			m.suggestItems[m.suggestCursor].selected = !m.suggestItems[m.suggestCursor].selected
		}

	case "a":
		// Toggle all: if any are unselected, select all; else deselect all.
		anyUnselected := false
		for _, it := range m.suggestItems {
			if !it.selected {
				anyUnselected = true
				break
			}
		}
		for i := range m.suggestItems {
			m.suggestItems[i].selected = anyUnselected
		}

	case "enter":
		// Add all selected items; if none selected, add the current one.
		var toAdd []suggestItem
		for _, it := range m.suggestItems {
			if it.selected {
				toAdd = append(toAdd, it)
			}
		}
		if len(toAdd) == 0 && m.suggestCursor < len(m.suggestItems) {
			toAdd = []suggestItem{m.suggestItems[m.suggestCursor]}
		}
		if len(toAdd) == 0 {
			break
		}
		s := m.s
		items := make([]suggestItem, len(toAdd))
		copy(items, toAdd)
		m.state = viewList
		return m, func() tea.Msg {
			var added []string
			for _, it := range items {
				icon, name := splitIcon(it.name)
				if name == "" {
					name = it.name
					icon = ""
				}
				desc := ""
				for _, d := range it.details {
					if !strings.HasPrefix(d, "Tipp:") {
						desc = d
						break
					}
				}
				if _, err := s.AddHabit(name, desc, icon); err != nil {
					return errMsg{err}
				}
				added = append(added, name)
			}
			return statusMsg("+ " + strings.Join(added, ", "))
		}

	case "n":
		// Manual add with empty input
		m.state = viewAddInput
		m.input.Reset()
		m.input.Placeholder = "Habit (optional: 🏃 Morning run)"
		m.input.Focus()
	}
	return m, nil
}

func (m model) handleStats(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.state = viewList
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
		switch p.id {
		case ai.ProviderOllama:
			m.cfg.Provider = string(ai.ProviderOllama)
			config.Save(m.cfg)
			config.ForceApplyToEnv(m.cfg)
			m.state = viewList
			m.message = "Ollama aktiv (lokal)"
		case ai.ProviderGemini:
			m.state = viewGeminiMenu
			m.geminiMenuCursor = 0
		default:
			m.state = viewKeyInput
			m.input.Reset()
			m.input.Placeholder = "API-Key einfügen…"
			if k := os.Getenv(p.envKey); k != "" {
				m.input.SetValue(k)
			}
			m.input.Focus()
		}
	}
	return m, nil
}

func (m model) handleGeminiMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.state = viewSettings
	case "j", "down":
		if m.geminiMenuCursor < 1 {
			m.geminiMenuCursor++
		}
	case "k", "up":
		if m.geminiMenuCursor > 0 {
			m.geminiMenuCursor--
		}
	case "enter":
		if m.geminiMenuCursor == 1 {
			m.state = viewKeyInput
			m.input.Reset()
			m.input.Placeholder = "Gemini API-Key einfügen…"
			if k := os.Getenv("GEMINI_API_KEY"); k != "" {
				m.input.SetValue(k)
			}
			m.input.Focus()
		} else {
			if m.cfg.GoogleClientID == "" {
				m.state = viewGeminiCID
				m.input.Reset()
				m.input.Placeholder = "Client ID einfügen…"
				m.input.Focus()
			} else {
				m.state = viewOAuthWait
				return m, startOAuth(m.cfg.GoogleClientID, m.cfg.GoogleClientSecret)
			}
		}
	}
	return m, nil
}

func (m model) handleGeminiCID(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.state = viewGeminiMenu
		return m, nil
	case "o":
		go auth.OpenBrowser("https://console.cloud.google.com/apis/credentials")
		return m, nil
	case "enter":
		if v := strings.TrimSpace(m.input.Value()); v != "" {
			m.geminiClientID = v
			m.state = viewGeminiCS
			m.input.Reset()
			m.input.Placeholder = "Client Secret einfügen…"
			m.input.Focus()
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) handleGeminiCS(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.state = viewGeminiCID
		m.input.Reset()
		m.input.Placeholder = "Client ID einfügen…"
		m.input.SetValue(m.geminiClientID)
		m.input.Focus()
		return m, nil
	case "enter":
		if v := strings.TrimSpace(m.input.Value()); v != "" {
			m.cfg.GoogleClientID = m.geminiClientID
			m.cfg.GoogleClientSecret = v
			config.Save(m.cfg)
			config.ForceApplyToEnv(m.cfg)
			m.state = viewOAuthWait
			return m, startOAuth(m.cfg.GoogleClientID, m.cfg.GoogleClientSecret)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
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
				m.cfg.GoogleRefreshToken = ""
				os.Unsetenv("GOOGLE_REFRESH_TOKEN")
			}
			m.cfg.Provider = string(p.id)
			config.Save(m.cfg)
			config.ForceApplyToEnv(m.cfg)
			m.state = viewList
			m.message = p.label + " eingerichtet"
		}
		return m, nil
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
		return m.renderAddInput()
	case viewAddDesc:
		return m.renderAddDesc()
	case viewSuggest:
		return m.renderSuggest()
	case viewSettings:
		return m.renderSettings()
	case viewKeyInput:
		return m.renderKeyInput()
	case viewStats:
		return m.renderStats()
	case viewEditHabit:
		return m.renderEditHabit()
	case viewGroupMgr:
		return m.renderGroupMgr()
	case viewGroupNew:
		return m.renderGroupNew()
	case viewGroupPick:
		return m.renderGroupPick()
	case viewReview:
		return m.renderReview()
	case viewNoteInput:
		return m.renderNoteInput()
	case viewChainMgr:
		return m.renderChainMgr()
	case viewChainPick:
		return m.renderChainPick()
	case viewGeminiMenu:
		return m.renderGeminiMenu()
	case viewGeminiCID:
		return m.renderGeminiCID()
	case viewGeminiCS:
		return m.renderGeminiCS()
	case viewOAuthWait:
		return m.renderOAuthWait()
	default:
		return m.renderList()
	}
}

// ── renderList ────────────────────────────────────────────────────────────────

func (m model) renderList() string {
	var b strings.Builder
	today := truncateDay(time.Now())

	innerW := m.width - 6 // border(1+1) + padding(2+2)
	if innerW < 55 {
		innerW = 68
	}

	// ── header ────────────────────────────────────────────────────────────────

	done, total := 0, len(m.habits)
	for _, h := range m.habits {
		if h.CheckedToday {
			done++
		}
	}
	bestStreak, totalCheckIns := 0, 0
	for _, h := range m.habits {
		if h.Streak > bestStreak {
			bestStreak = h.Streak
		}
		totalCheckIns += h.TotalDays
	}

	b.WriteString(styleLime.Bold(true).Render("habctl") +
		styleMuted.Render("   @habctl $ daily") + "\n")
	b.WriteString(styleMuted.Render("// "+motivationalMsg(totalCheckIns)) + "\n\n")

	weekdayDE := [...]string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"}
	monthDE := [...]string{"", "Januar", "Februar", "März", "April", "Mai", "Juni",
		"Juli", "August", "September", "Oktober", "November", "Dezember"}
	dateLabel := fmt.Sprintf("%s, %d. %s %d",
		weekdayDE[today.Weekday()], today.Day(), monthDE[today.Month()], today.Year())
	b.WriteString(styleFg.Render(dateLabel) + "\n")

	var statsLine strings.Builder
	if bestStreak > 0 {
		statsLine.WriteString(styleOkBold.Render(fmt.Sprintf("🔥 %d", bestStreak)) +
			styleMuted.Render(" Tage  ·  "))
	}
	if total > 0 {
		var ps lipgloss.Style
		switch {
		case done == total:
			ps = styleOkBold
		case done > 0:
			ps = styleOk
		default:
			ps = styleMuted
		}
		statsLine.WriteString(ps.Render(fmt.Sprintf("%d/%d heute", done, total)) +
			styleMuted.Render(fmt.Sprintf("  ·  %d Habits", total)))
	}
	b.WriteString(statsLine.String() + "\n\n")

	// ── week calendar strip ───────────────────────────────────────────────────

	if total > 0 {
		var dayDone [7]int
		for _, h := range m.habits {
			for i, chkd := range h.Last7Days {
				if chkd {
					dayDone[i]++
				}
			}
		}

		dayAbbrDE := [7]string{"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"}
		const slotW = 5

		var rowDay, rowDate, rowBar strings.Builder
		for i := 0; i < 7; i++ {
			d := today.AddDate(0, 0, i-6)
			isToday := i == 6
			abbr := dayAbbrDE[int(d.Weekday())]
			dateNum := fmt.Sprintf("%d", d.Day())

			var dayS, dateS lipgloss.Style
			if isToday {
				dayS = styleLime.Bold(true)
				dateS = styleLime.Bold(true)
			} else {
				dayS = styleMuted
				dateS = styleMuted
			}
			rowDay.WriteString(lipgloss.NewStyle().Width(slotW).Render(dayS.Render(abbr)))
			rowDate.WriteString(lipgloss.NewStyle().Width(slotW).Render(dateS.Render(dateNum)))

			pct := 0.0
			if total > 0 {
				pct = float64(dayDone[i]) / float64(total)
			}
			var blk string
			switch {
			case pct >= 1.0:
				blk = styleOkBold.Render("███")
			case pct >= 0.67:
				blk = styleOk.Render("▓▓▓")
			case pct >= 0.33:
				blk = styleWarn.Render("▒▒▒")
			case pct > 0:
				blk = styleWarn.Render("▒░░")
			default:
				blk = styleMuted.Render("░░░")
			}
			rowBar.WriteString(lipgloss.NewStyle().Width(slotW).Render(blk))
		}

		b.WriteString("  " + rowDay.String() + "\n")
		b.WriteString("  " + rowDate.String() + "\n")
		b.WriteString("  " + rowBar.String() + "\n\n")
	}

	// ── habit list ────────────────────────────────────────────────────────────

	if total == 0 {
		b.WriteString(styleMuted.Render("Noch keine Habits. n zum Anlegen.") + "\n")
	} else {
		const cbW = 4  // "[✓] "
		const skW = 10 // right-aligned streak column
		nameW := innerW - cbW - skW
		if nameW < 20 {
			nameW = 20
		}

		var lastGroupID int64 = -1

		for i, h := range m.habits {
			gid := h.Habit.GroupID
			if gid != lastGroupID {
				lastGroupID = gid
				if gid != 0 {
					g := groupByID(m.groups, gid)
					label := g.Name
					if g.Icon != "" {
						label = g.Icon + " " + g.Name
					}
					gDone, gTotal := 0, 0
					for _, hh := range m.habits {
						if hh.Habit.GroupID == gid {
							gTotal++
							if hh.CheckedToday {
								gDone++
							}
						}
					}
					counter := fmt.Sprintf("[%d/%d] ▼", gDone, gTotal)
					ctrW := lipgloss.Width(counter)
					groupNameW := innerW - ctrW - 1
					if groupNameW < 1 {
						groupNameW = 1
					}
					glabel := lipgloss.NewStyle().Width(groupNameW).Render(
						styleGroup.Render(truncate(label, groupNameW-1)),
					)
					b.WriteString("\n" + glabel + " " + styleMuted.Render(counter) + "\n")
				} else if i > 0 {
					b.WriteString("\n")
				}
			}

			selected := i == m.cursor
			atRisk := h.Streak > 0 && !h.CheckedToday

			var cb string
			var ns lipgloss.Style
			switch {
			case selected && h.CheckedToday:
				cb = styleLime.Render("[✓]") + " "
				ns = lipgloss.NewStyle().Foreground(colorFg).Bold(true)
			case selected:
				cb = styleLime.Render("[·]") + " "
				ns = lipgloss.NewStyle().Foreground(colorFg).Bold(true)
			case h.CheckedToday:
				cb = styleOk.Render("[✓]") + " "
				ns = styleOk
			case atRisk:
				cb = styleWarnBd.Render("[!]") + " "
				ns = styleWarn
			default:
				cb = styleMuted.Render("[ ]") + " "
				ns = styleMuted
			}

			var rawName string
			if h.Habit.Icon != "" {
				rawName = h.Habit.Icon + " " + truncate(h.Habit.Name, nameW-4)
			} else {
				rawName = truncate(h.Habit.Name, nameW-1)
			}
			nameCol := lipgloss.NewStyle().Width(nameW).Render(ns.Render(rawName))

			var skContent string
			switch {
			case h.CheckedToday && h.Streak > 0:
				skContent = styleOkBold.Render(fmt.Sprintf("🔥 %d", h.Streak))
			case atRisk:
				skContent = styleWarnBd.Render(fmt.Sprintf("🔥 %d!", h.Streak))
			case h.Streak > 0:
				skContent = styleMuted.Render(fmt.Sprintf("%d", h.Streak))
			default:
				skContent = styleMuted.Render("0")
			}
			skCol := lipgloss.NewStyle().Width(skW).Align(lipgloss.Right).Render(skContent)

			b.WriteString(cb + nameCol + skCol + "\n")

			maxSubW := innerW - 4 - 3
			if h.Habit.Description != "" {
				b.WriteString("    " + styleMuted.Render("// "+truncate(h.Habit.Description, maxSubW)) + "\n")
			}
			if h.TodayNote != "" {
				b.WriteString("    " + styleMuted.Render("📝 "+truncate(h.TodayNote, maxSubW)) + "\n")
			}
			if h.ChainTo != "" {
				b.WriteString("    " + styleMuted.Render("→ "+h.ChainTo) + "\n")
			}
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

	b.WriteString(styleMuted.Render(
		"space ✓ · N notiz · n neu · e edit · m gruppe · c ketten · s KI · r review · t stats · ? help · q quit"))
	return panelStyle.Render(b.String())
}

func motivationalMsg(checkIns int) string {
	switch {
	case checkIns == 0:
		return "Fange heute an."
	case checkIns < 5:
		return "Die ersten Schritte."
	case checkIns < 15:
		return "Der Rhythmus beginnt."
	case checkIns < 30:
		return "Du bist dabei."
	case checkIns < 50:
		return "Halbzeit der ersten 50."
	case checkIns < 100:
		return "Die Daten fangen an, etwas zu bedeuten."
	case checkIns < 200:
		return "Echte Konstanz."
	case checkIns < 365:
		return "Gewohnheit ist Charakter."
	default:
		return "Ein Jahr. Unstoppbar."
	}
}

// ── renderAddInput ────────────────────────────────────────────────────────────

func (m model) renderAddInput() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Neuer Habit") + "\n\n")
	b.WriteString(styleMuted.Render("Tipp: emoji als Prefix — 🏃 Laufen, ☕ Kaffee, 📚 Lesen") + "\n\n")
	b.WriteString(m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter weiter · esc abbrechen"))
	return panelStyle.Render(b.String())
}

// ── renderAddDesc ─────────────────────────────────────────────────────────────

func (m model) renderAddDesc() string {
	var b strings.Builder
	icon := ""
	if m.addingIcon != "" {
		icon = m.addingIcon + " "
	}
	b.WriteString(styleLime.Bold(true).Render(icon+m.addingName) + "\n\n")
	b.WriteString(styleMuted.Render("Kurze Notiz? (enter zum Überspringen)") + "\n\n")
	b.WriteString(m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter speichern · esc überspringen"))
	return panelStyle.Render(b.String())
}

// ── renderEditHabit ───────────────────────────────────────────────────────────

func (m model) renderEditHabit() string {
	var b strings.Builder
	var h models.Habit
	for _, hs := range m.habits {
		if hs.Habit.Name == m.editOldName {
			h = hs.Habit
			break
		}
	}

	b.WriteString(styleLime.Bold(true).Render("Habit bearbeiten") + "\n\n")

	if m.editCursor == 0 {
		b.WriteString(styleLime.Render("▶ ") + styleFg.Render("Name / Icon") + "\n")
		b.WriteString("  " + m.input.View() + "\n\n")
		b.WriteString(styleMuted.Render("  Notiz: ") + styleMuted.Render(h.Description) + "\n")
	} else {
		b.WriteString(styleMuted.Render("  Name: ") + styleFg.Render(h.Icon+" "+h.Name) + "\n\n")
		b.WriteString(styleLime.Render("▶ ") + styleFg.Render("Notiz") + "\n")
		b.WriteString("  " + m.input.View() + "\n")
	}

	b.WriteString("\n" + styleMuted.Render("enter speichern · tab anderes Feld · esc abbrechen"))
	return panelStyle.Render(b.String())
}

// ── renderGroupMgr ────────────────────────────────────────────────────────────

func (m model) renderGroupMgr() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Gruppen") + "\n\n")

	if len(m.groups) == 0 {
		b.WriteString(styleMuted.Render("Noch keine Gruppen. a zum Anlegen.") + "\n\n")
	} else {
		for i, g := range m.groups {
			cursor := "  "
			labelStyle := lipgloss.NewStyle().Foreground(colorMuted)
			if i == m.groupCursor {
				cursor = styleLime.Render("▶ ")
				labelStyle = lipgloss.NewStyle().Foreground(colorFg)
			}
			icon := ""
			if g.Icon != "" {
				icon = g.Icon + " "
			}
			// Count habits in group
			count := 0
			for _, h := range m.habits {
				if h.Habit.GroupID == g.ID {
					count++
				}
			}
			b.WriteString(cursor + labelStyle.Render(icon+g.Name) +
				styleMuted.Render(fmt.Sprintf("  (%d habits)", count)) + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(styleMuted.Render("a neu · d löschen · j/k navigieren · esc zurück"))
	return panelStyle.Render(b.String())
}

// ── renderGroupNew ────────────────────────────────────────────────────────────

func (m model) renderGroupNew() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Neue Gruppe") + "\n\n")
	b.WriteString(styleMuted.Render("Tipp: mit Emoji starten — 🌅 Morgen, 💻 Arbeit, 🌙 Abend") + "\n\n")
	b.WriteString(m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter erstellen · esc abbrechen"))
	return panelStyle.Render(b.String())
}

// ── renderGroupPick ───────────────────────────────────────────────────────────

func (m model) renderGroupPick() string {
	var b strings.Builder
	habitName := ""
	if m.cursor < len(m.habits) {
		h := m.habits[m.cursor].Habit
		habitName = h.Name
		if h.Icon != "" {
			habitName = h.Icon + " " + habitName
		}
	}
	b.WriteString(styleLime.Bold(true).Render("Gruppe zuweisen") + "\n")
	b.WriteString(styleMuted.Render("→ "+habitName) + "\n\n")

	options := append([]models.Group{{Name: "Kein (ungrouped)", Icon: "○"}}, m.groups...)
	for i, g := range options {
		cursor := "  "
		labelStyle := lipgloss.NewStyle().Foreground(colorMuted)
		if i == m.groupCursor {
			cursor = styleLime.Render("▶ ")
			labelStyle = lipgloss.NewStyle().Foreground(colorFg)
		}
		icon := ""
		if g.Icon != "" {
			icon = g.Icon + " "
		}
		b.WriteString(cursor + labelStyle.Render(icon+g.Name) + "\n")
	}

	b.WriteString("\n" + styleMuted.Render("enter wählen · j/k navigieren · esc abbrechen"))
	return panelStyle.Render(b.String())
}

// ── renderStats ───────────────────────────────────────────────────────────────

func (m model) renderStats() string {
	cal := m.calData
	total := cal.TotalHabits
	byDate := cal.ByDate

	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Statistiken") + "\n\n")

	if total == 0 {
		b.WriteString(styleMuted.Render("Erst Habits anlegen (n), dann kommen hier Daten.") + "\n")
		b.WriteString("\n" + styleMuted.Render("esc zurück"))
		return panelStyle.Render(b.String())
	}

	today := truncateDay(time.Now())

	var daysTracked, perfectDays int
	var completionSum float64
	for dateStr, cnt := range byDate {
		t, _ := time.ParseInLocation("2006-01-02", dateStr, time.Local)
		if !t.After(today) {
			daysTracked++
			pct := float64(cnt) / float64(total)
			completionSum += pct
			if cnt >= total {
				perfectDays++
			}
		}
	}
	avgCompletion := 0.0
	if daysTracked > 0 {
		avgCompletion = completionSum / float64(daysTracked) * 100
	}

	bestStreak, bestName := 0, ""
	bestLongest, bestLongestName := 0, ""
	for _, h := range m.habits {
		if h.Streak > bestStreak {
			bestStreak = h.Streak
			bestName = h.Habit.Name
		}
		if h.LongestStreak > bestLongest {
			bestLongest = h.LongestStreak
			bestLongestName = h.Habit.Name
		}
	}

	numV := lipgloss.NewStyle().Foreground(colorLime).Bold(true)
	lbl := styleMuted

	b.WriteString(styleMuted.Render("// überblick") + "\n")
	b.WriteString(fmt.Sprintf("  %s %s   %s %s   %s %s\n",
		numV.Render(fmt.Sprintf("%d", daysTracked)), lbl.Render("days tracked"),
		numV.Render(fmt.Sprintf("%.0f%%", avgCompletion)), lbl.Render("avg"),
		numV.Render(fmt.Sprintf("%d", perfectDays)), lbl.Render("perfect days"),
	))
	if bestStreak > 0 {
		b.WriteString(fmt.Sprintf("  %s %s  %s\n",
			lbl.Render("🔥 aktuell:"),
			numV.Render(fmt.Sprintf("%d days", bestStreak)),
			lbl.Render("— "+truncate(bestName, 24)),
		))
	}
	if bestLongest > bestStreak {
		b.WriteString(fmt.Sprintf("  %s %s  %s\n",
			lbl.Render("   längster:"),
			numV.Render(fmt.Sprintf("%d days", bestLongest)),
			lbl.Render("— "+truncate(bestLongestName, 24)),
		))
	}

	// ── heatmap ──────────────────────────────────────────────────────────────
	const weeks = 26
	b.WriteString("\n" + styleMuted.Render("// contributions (26 Wochen)") + "\n")

	wd := int(today.Weekday())
	daysFromMon := (wd + 6) % 7
	thisMonday := today.AddDate(0, 0, -daysFromMon)
	startDate := thisMonday.AddDate(0, 0, -(weeks-1)*7)

	// Month label row
	var monthLine strings.Builder
	monthLine.WriteString("   ")
	lastMonth := -1
	for w := 0; w < weeks; w++ {
		day := startDate.AddDate(0, 0, w*7)
		mo := int(day.Month())
		if mo != lastMonth {
			monthLine.WriteString(styleMuted.Render(day.Format("Jan")[:1]))
			lastMonth = mo
		} else {
			monthLine.WriteString(" ")
		}
		monthLine.WriteString(" ")
	}
	b.WriteString(monthLine.String() + "\n")

	heat := [5]lipgloss.Style{
		lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#cbd5e1", Dark: "#2d3748"}),
		lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#86efac", Dark: "#276749"}),
		lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#4ade80", Dark: "#38a169"}),
		lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#22c55e", Dark: "#48bb78"}),
		lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#16a34a", Dark: "#68d391"}),
	}
	dayLabels := []string{"m", "t", "w", "t", "f", "s", "s"}

	for d := 0; d < 7; d++ {
		var row strings.Builder
		row.WriteString(styleMuted.Render(dayLabels[d]) + " ")
		for w := 0; w < weeks; w++ {
			day := startDate.AddDate(0, 0, w*7+d)
			if day.After(today) {
				row.WriteString(heat[0].Render("░ "))
				continue
			}
			cnt := byDate[day.Format("2006-01-02")]
			level := 0
			if total > 0 && cnt > 0 {
				pct := float64(cnt) / float64(total)
				switch {
				case pct >= 1.0:
					level = 4
				case pct >= 0.5:
					level = 3
				case pct >= 0.25:
					level = 2
				default:
					level = 1
				}
			}
			cell := "░"
			if level > 0 {
				cell = "█"
			}
			row.WriteString(heat[level].Render(cell+" "))
		}
		b.WriteString(row.String() + "\n")
	}
	b.WriteString(styleMuted.Render("  ░ 0%  ") +
		heat[2].Render("█") + styleMuted.Render(" 1-49%  ") +
		heat[3].Render("█") + styleMuted.Render(" 50-99%  ") +
		heat[4].Render("█") + styleMuted.Render(" 100%") + "\n")

	// ── day of week ───────────────────────────────────────────────────────────
	b.WriteString("\n" + styleMuted.Render("// wochentag") + "\n")
	var dowCount [7]int
	var dowTotal [7]int
	for dateStr, cnt := range byDate {
		t, err := time.ParseInLocation("2006-01-02", dateStr, time.Local)
		if err != nil || t.After(today) {
			continue
		}
		dow := (int(t.Weekday()) + 6) % 7
		dowTotal[dow] += total
		dowCount[dow] += cnt
	}
	dowLabels := []string{"mo", "tu", "we", "th", "fr", "sa", "su"}
	barW := 22
	for d := 0; d < 7; d++ {
		pct := 0.0
		if dowTotal[d] > 0 {
			pct = float64(dowCount[d]) / float64(dowTotal[d])
		}
		filled := int(pct * float64(barW))
		bar := styleLime.Render(strings.Repeat("█", filled)) +
			styleMuted.Render(strings.Repeat("░", barW-filled))
		b.WriteString(fmt.Sprintf("  %s %s %s\n",
			styleMuted.Render(dowLabels[d]),
			bar,
			styleMuted.Render(fmt.Sprintf("%3.0f%%", pct*100))))
	}

	b.WriteString("\n" + styleMuted.Render("esc zurück"))
	return panelStyle.Render(b.String())
}

// ── renderSuggest ─────────────────────────────────────────────────────────────

func (m model) renderSuggest() string {
	var b strings.Builder
	providerLabel := ""
	if info, err := ai.Detect(); err == nil {
		providerLabel = styleMuted.Render("via " + info.Display)
	} else {
		providerLabel = styleWarn.Render("kein Provider — S für Settings")
	}
	b.WriteString(styleLime.Bold(true).Render("Habit-Vorschläge") + "  " + providerLabel + "\n\n")

	if len(m.suggestItems) > 0 {
		checkOff := styleMuted.Render("[ ]")
		checkOn := styleOk.Bold(true).Render("[✓]")
		indent := "       " // 7 chars: "  [ ]  " width

		for i, it := range m.suggestItems {
			if i > 0 {
				b.WriteString("\n")
			}
			selected := i == m.suggestCursor
			cursor := "  "
			headerStyle := styleMuted
			if selected {
				cursor = styleLime.Render("▶ ")
				headerStyle = styleFg.Bold(true)
			}
			chk := checkOff
			if it.selected {
				chk = checkOn
			}

			b.WriteString(cursor + chk + " " + headerStyle.Render(it.header) + "\n")

			for _, d := range it.details {
				for _, wl := range strings.Split(wordWrap(d, 62), "\n") {
					if wl == "" {
						continue
					}
					b.WriteString(indent + styleMuted.Render(wl) + "\n")
				}
			}
		}

		b.WriteString("\n")
		selectedCount := 0
		for _, it := range m.suggestItems {
			if it.selected {
				selectedCount++
			}
		}
		if selectedCount > 0 {
			b.WriteString(styleOk.Render(fmt.Sprintf("%d ausgewählt", selectedCount)) + "  ")
		}
		b.WriteString(styleMuted.Render("space ✓ · enter hinzufügen · a alle · j/k · esc zurück"))
	} else if !m.suggestDone {
		// Streaming in progress — don't show raw markdown
		b.WriteString(styleMuted.Render("Generiere Vorschläge…") + "\n\n")
		b.WriteString(styleLime.Render("▌"))
	} else {
		// Done but parse returned nothing — show error
		b.WriteString(styleWarn.Render("Format nicht erkannt.") + "\n")
		b.WriteString(styleMuted.Render("s   nochmal versuchen") + "\n")
		b.WriteString(styleMuted.Render("n   manuell hinzufügen") + "\n")
		b.WriteString(styleMuted.Render("esc zurück"))
	}

	return panelStyle.Render(b.String())
}

// ── renderSettings ────────────────────────────────────────────────────────────

func (m model) renderSettings() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("KI-Provider") + "\n")
	b.WriteString(styleMuted.Render("Wähle deinen KI-Anbieter und richte ihn ein.") + "\n\n")

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

		var badge string
		if p.id == ai.ProviderOllama {
			badge = styleOk.Render("● lokal")
		} else {
			var activeKey string
			switch p.id {
			case ai.ProviderAnthropic:
				activeKey = m.cfg.AnthropicKey
				if activeKey == "" {
					activeKey = os.Getenv(p.envKey)
				}
			case ai.ProviderOpenAI:
				activeKey = m.cfg.OpenAIKey
				if activeKey == "" {
					activeKey = os.Getenv(p.envKey)
				}
			case ai.ProviderGemini:
				activeKey = m.cfg.GeminiKey
				if activeKey == "" {
					activeKey = os.Getenv(p.envKey)
				}
			}
			oauthActive := p.id == ai.ProviderGemini && m.cfg.GoogleRefreshToken != ""
			if oauthActive {
				badge = styleOk.Render("● OAuth aktiv")
			} else if activeKey != "" {
				suffix := activeKey
				if len(suffix) > 4 {
					suffix = "…" + suffix[len(suffix)-4:]
				}
				badge = styleOk.Render("● Key gesetzt") + styleMuted.Render(" ("+suffix+")")
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

// ── renderKeyInput ────────────────────────────────────────────────────────────

func (m model) renderKeyInput() string {
	var b strings.Builder
	p := providers[m.settingsCursor]
	b.WriteString(styleLime.Bold(true).Render(p.label+" einrichten") + "\n\n")
	switch p.id {
	case ai.ProviderGemini:
		b.WriteString(styleMuted.Render("o  Browser öffnen: aistudio.google.com") + "\n")
		b.WriteString(styleMuted.Render("   → Mit Google-Account einloggen → 'Get API key'") + "\n\n")
	default:
		if p.keyPage != "" {
			b.WriteString(styleOk.Render("o") + styleMuted.Render("  öffnet "+p.keyPage) + "\n\n")
		}
	}
	b.WriteString(styleMuted.Render("API-Key (Cmd+V):") + "\n")
	b.WriteString("  " + m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter speichern · o Browser öffnen · esc zurück"))
	return panelStyle.Render(b.String())
}

// ── renderGeminiMenu / CID / CS / OAuthWait ───────────────────────────────────

func (m model) renderGeminiMenu() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Google Gemini") + "\n\n")
	options := []struct{ label, desc string }{
		{"Browser Login (Google-Account)", "Kein Key nötig — Login im Browser"},
		{"API Key", "Von aistudio.google.com"},
	}
	for i, o := range options {
		cursor := "  "
		ls := lipgloss.NewStyle().Foreground(colorMuted)
		if i == m.geminiMenuCursor {
			cursor = styleLime.Render("▶ ")
			ls = lipgloss.NewStyle().Foreground(colorFg)
		}
		b.WriteString(cursor + ls.Bold(true).Render(o.label) + "\n")
		b.WriteString("    " + styleMuted.Render(o.desc) + "\n\n")
	}
	if m.cfg.GoogleRefreshToken != "" {
		b.WriteString(styleOk.Render("● bereits eingeloggt (OAuth)") + "\n\n")
	}
	b.WriteString(styleMuted.Render("enter auswählen · j/k navigieren · esc zurück"))
	return panelStyle.Render(b.String())
}

func (m model) renderGeminiCID() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Google OAuth2 Client einrichten") + "\n\n")
	b.WriteString(styleOk.Render("o") + styleMuted.Render("  öffnet console.cloud.google.com/apis/credentials") + "\n\n")
	b.WriteString(styleMuted.Render("1. Projekt wählen · 2. Create Credentials → OAuth 2.0 Client ID") + "\n")
	b.WriteString(styleMuted.Render("3. Typ: Desktop-App · 4. Client ID kopieren") + "\n\n")
	b.WriteString(styleMuted.Render("Client ID:") + "\n")
	b.WriteString("  " + m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter weiter · o Browser öffnen · esc zurück"))
	return panelStyle.Render(b.String())
}

func (m model) renderGeminiCS() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Google OAuth2 Client Secret") + "\n\n")
	b.WriteString(styleMuted.Render("Client Secret von der gleichen Credentials-Seite:") + "\n\n")
	b.WriteString(styleMuted.Render("Client Secret:") + "\n")
	b.WriteString("  " + m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter Browser-Login starten · esc zurück"))
	return panelStyle.Render(b.String())
}

func (m model) renderOAuthWait() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Warte auf Google-Login…") + "\n\n")
	b.WriteString(styleMuted.Render(
		"Browser wurde geöffnet.\n\n"+
			"1. Mit Google-Account einloggen\n"+
			"2. habctl Zugriff erlauben\n"+
			"3. Seite zeigt \"Login erfolgreich\" → fertig\n\n"+
			"Timeout: 5 Minuten") + "\n")
	b.WriteString("\n" + styleLime.Render("⠿ ") + styleMuted.Render("warte…"))
	return panelStyle.Render(b.String())
}

// ── renderAdd / renderHelp ────────────────────────────────────────────────────

func (m model) renderHelp() string {
	lime := styleLime.Bold(true)
	key := lipgloss.NewStyle().Foreground(colorLime).Width(26)
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
	b.WriteString(section("Habits"))
	b.WriteString(row("space / enter", "check in today"))
	b.WriteString(row("N", "Notiz zu heutigem Check-in hinzufügen"))
	b.WriteString(row("n", "neuen Habit anlegen (+ optionales Emoji)"))
	b.WriteString(row("e", "Name + Icon bearbeiten"))
	b.WriteString(row("E", "Beschreibung bearbeiten"))
	b.WriteString(row("d", "Habit löschen"))
	b.WriteString(section("Gruppen"))
	b.WriteString(row("m", "Habit in Gruppe verschieben"))
	b.WriteString(row("G", "Gruppen verwalten (add, delete)"))
	b.WriteString(section("Views"))
	b.WriteString(row("s", "KI-Vorschläge (kontextbewusst)"))
	b.WriteString(row("r", "KI-Wochenreview — Coaching-Briefing"))
	b.WriteString(row("t", "Statistiken — Heatmap & Completion"))
	b.WriteString(row("c", "Habit-Ketten verwalten"))
	b.WriteString(row("S", "Settings — Provider & API-Keys"))
	b.WriteString(row("w", "toggle 7d / 30d streak window"))
	b.WriteString(section("Status"))
	b.WriteString(row(styleOk.Render("✓  green"), "checked in today"))
	b.WriteString(row(styleWarn.Render("!  amber"), "streak at risk — check in before midnight!"))
	b.WriteString(row(styleMuted.Render("·  gray"), "not done this day"))
	b.WriteString(section("Other"))
	b.WriteString(row("?", "toggle this help screen"))
	b.WriteString(row("q / ctrl+c", "quit"))
	b.WriteString("\n  " + styleMuted.Render("esc / ?   close help"))
	return panelStyle.Render(b.String())
}

// ── renderReview ──────────────────────────────────────────────────────────────

func (m model) renderReview() string {
	var b strings.Builder
	providerLabel := ""
	if info, err := ai.Detect(); err == nil {
		providerLabel = styleMuted.Render("via " + info.Display)
	} else {
		providerLabel = styleWarn.Render("kein Provider — S für Settings")
	}
	b.WriteString(styleLime.Bold(true).Render("Wochenreview") + "  " + providerLabel + "\n\n")

	if m.reviewText != "" {
		for _, line := range strings.Split(m.reviewText, "\n") {
			if strings.HasPrefix(line, "## ") {
				b.WriteString("\n" + styleLime.Bold(true).Render(strings.TrimPrefix(line, "## ")) + "\n")
			} else {
				b.WriteString(styleMuted.Render(line) + "\n")
			}
		}
		if !m.reviewDone {
			b.WriteString(styleLime.Render("▌"))
		}
	} else if !m.reviewDone {
		b.WriteString(styleMuted.Render("Analysiere letzte Woche…") + "\n\n")
		b.WriteString(styleLime.Render("▌"))
	}

	b.WriteString("\n\n" + styleMuted.Render("esc zurück · r nochmal"))
	return panelStyle.Render(b.String())
}

// ── renderNoteInput ───────────────────────────────────────────────────────────

func (m model) renderNoteInput() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Notiz") + styleMuted.Render(" für "+m.noteForHabit) + "\n\n")
	b.WriteString("  " + m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter speichern · esc abbrechen"))
	return panelStyle.Render(b.String())
}

// ── renderChainMgr ────────────────────────────────────────────────────────────

func (m model) renderChainMgr() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Habit-Ketten") + "\n")
	b.WriteString(styleMuted.Render("Nach Habit A kommt direkt Habit B.") + "\n\n")

	if len(m.chains) == 0 {
		b.WriteString(styleMuted.Render("Noch keine Ketten. a anlegen, s KI-Vorschläge.") + "\n")
	} else {
		for i, ch := range m.chains {
			cursor := "  "
			style := styleMuted
			if i == m.chainCursor {
				cursor = styleLime.Render("▶ ")
				style = styleFg
			}
			b.WriteString(cursor + style.Render(ch.FromName) + styleMuted.Render(" → ") + style.Render(ch.ToName) + "\n")
		}
	}

	b.WriteString("\n" + styleMuted.Render("a anlegen · d löschen · s KI-Vorschläge · esc zurück"))
	return panelStyle.Render(b.String())
}

// ── renderChainPick ───────────────────────────────────────────────────────────

func (m model) renderChainPick() string {
	var b strings.Builder
	if m.chainFromName == "" {
		b.WriteString(styleLime.Bold(true).Render("Kette anlegen") + "\n")
		b.WriteString(styleMuted.Render("Schritt 1: Welcher Habit kommt zuerst?") + "\n\n")
	} else {
		b.WriteString(styleLime.Bold(true).Render("Kette anlegen") + "\n")
		b.WriteString(styleMuted.Render("Schritt 2: Welcher Habit folgt auf ") +
			styleLime.Render(m.chainFromName) + styleMuted.Render("?") + "\n\n")
	}

	for i, h := range m.habits {
		cursor := "  "
		style := styleMuted
		if i == m.chainPickCursor {
			cursor = styleLime.Render("▶ ")
			style = styleFg
		}
		name := h.Habit.Name
		if h.Habit.Icon != "" {
			name = h.Habit.Icon + " " + name
		}
		if name == m.chainFromName {
			style = styleMuted // can't pick the same one
		}
		b.WriteString(cursor + style.Render(name) + "\n")
	}

	b.WriteString("\n" + styleMuted.Render("enter auswählen · esc zurück"))
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

func loadGroups(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		gs, err := s.ListGroups()
		if err != nil {
			return errMsg{err}
		}
		return groupsLoadedMsg(gs)
	}
}

func loadChains(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		cs, err := s.ListChains()
		if err != nil {
			return errMsg{err}
		}
		return chainsLoadedMsg(cs)
	}
}

func waitForReviewChunk(ch <-chan reviewChunkResult) tea.Cmd {
	return func() tea.Msg {
		r := <-ch
		switch {
		case r.err != nil:
			return reviewErrMsg{r.err, r.gen}
		case r.done:
			return reviewDoneMsg{r.gen}
		default:
			return reviewChunkMsg{r.text, r.gen}
		}
	}
}

func waitForChunk(ch <-chan suggestChunkResult) tea.Cmd {
	return func() tea.Msg {
		r := <-ch
		if r.err != nil {
			return suggestErrMsg{r.err, r.gen}
		}
		if r.done {
			return suggestDoneMsg{r.gen}
		}
		return suggestChunkMsg{r.text, r.gen}
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

func wordWrap(text string, width int) string {
	var out strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if len([]rune(line)) <= width {
			out.WriteString(line + "\n")
			continue
		}
		words := strings.Fields(line)
		col := 0
		for i, w := range words {
			wl := len([]rune(w))
			if i > 0 && col+1+wl > width {
				out.WriteString("\n")
				col = 0
			} else if i > 0 {
				out.WriteString(" ")
				col++
			}
			out.WriteString(w)
			col += wl
		}
		out.WriteString("\n")
	}
	return strings.TrimRight(out.String(), "\n")
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

func truncateDay(t time.Time) time.Time {
	y, mo, d := t.Date()
	return time.Date(y, mo, d, 0, 0, 0, 0, t.Location())
}

// splitIcon extracts a leading emoji from user input.
// "🏃 Morning run" → ("🏃", "Morning run")
// "Journal" → ("", "Journal")
func splitIcon(input string) (icon, name string) {
	r := []rune(strings.TrimSpace(input))
	if len(r) == 0 {
		return "", ""
	}
	first := r[0]
	// Emoji range: misc symbols, dingbats, or supplementary multilingual plane
	if first >= 0x2600 && first <= 0x27BF || first >= 0x1F000 {
		end := 1
		// skip variation selector U+FE0F
		if end < len(r) && r[end] == 0xFE0F {
			end++
		}
		// skip zero-width joiner sequences (basic check)
		icon = string(r[:end])
		name = strings.TrimSpace(string(r[end:]))
		if name == "" {
			return "", string(r) // only an emoji → treat as name
		}
		return icon, name
	}
	return "", string(r)
}

// parseSuggestions parses the ### block format produced by the AI.
// Each block is delimited by "###" and contains Name:, Zeit:, Nutzen:, Tipp: fields.
func parseSuggestions(text string) []suggestItem {
	var items []suggestItem
	for _, block := range strings.Split(text, "###") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		var name, zeit, nutzen, tipp string
		for _, raw := range strings.Split(block, "\n") {
			line := strings.TrimSpace(raw)
			switch {
			case strings.HasPrefix(line, "Name:"):
				name = strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
			case strings.HasPrefix(line, "Zeit:"):
				zeit = strings.TrimSpace(strings.TrimPrefix(line, "Zeit:"))
			case strings.HasPrefix(line, "Nutzen:"):
				nutzen = strings.TrimSpace(strings.TrimPrefix(line, "Nutzen:"))
			case strings.HasPrefix(line, "Tipp:"):
				tipp = strings.TrimSpace(strings.TrimPrefix(line, "Tipp:"))
			}
		}
		if name == "" {
			continue
		}
		header := name
		if zeit != "" {
			header = name + "  ·  " + zeit
		}
		var details []string
		if nutzen != "" {
			details = append(details, nutzen)
		}
		if tipp != "" {
			details = append(details, "Tipp: "+tipp)
		}
		items = append(items, suggestItem{name: name, header: header, details: details})
	}
	return items
}

func groupByID(groups []models.Group, id int64) models.Group {
	for _, g := range groups {
		if g.ID == id {
			return g
		}
	}
	return models.Group{}
}

func startOAuth(clientID, clientSecret string) tea.Cmd {
	return func() tea.Msg {
		rt, err := auth.BrowserLogin(clientID, clientSecret)
		if err != nil {
			return oauthErrMsg{err}
		}
		return oauthSuccessMsg{rt}
	}
}

func openBrowser(url string) {
	auth.OpenBrowser(url)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// displayWidth returns the approximate display width of a string,
// counting emoji and CJK as 2 columns.
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if r >= 0x1F000 || (r >= 0x2600 && r <= 0x27BF) {
			w += 2
		} else {
			w += utf8.RuneLen(r) // approximation
		}
	}
	return w
}
