package tui

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
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
	{ai.ProviderOllama, "Ollama (local)", "", ""},
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
	viewHabitDetail // full habit detail / expand view
	viewReview      // weekly AI coaching briefing
	viewNoteInput   // add/edit note for today's check-in
	viewChainMgr    // manage habit chains
	viewChainPick   // pick target habit when creating a chain
	viewGeminiMenu
	viewGeminiCID
	viewGeminiCS
	viewOAuthWait
	viewArchive   // archived habits list
	viewGoalInput // goal → 3 decomposed habits
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

type chainSuggestItem struct {
	from     string
	to       string
	reason   string
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
type blinkMsg struct{}
type notesLoadedMsg struct {
	name  string
	notes []models.NoteEntry
}
type archivedLoadedMsg []models.Habit
type archiveReloadMsg struct{ status string }

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
	height   int

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

	// extended edit-habit fields (buffered across tab switches)
	editNameBuf string
	editDescBuf string
	editFreq    int
	editSkip    int

	// suggest mode: "habit" (default), "chain", or "decompose"
	suggestMode       string
	chainSuggestItems []chainSuggestItem

	// UI state
	compact bool // hide descriptions/notes in list view
	blinkOn bool // streaming cursor blink state

	// detail-view: loaded per-habit recent notes
	recentNotes    []models.NoteEntry
	recentNotesFor string

	// archive view
	archivedHabits []models.Habit
	archiveCursor  int

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
		m.height = msg.Height
		return m, nil

	case habitsLoadedMsg:
		stats := []models.HabitStats(msg)
		sort.SliceStable(stats, func(i, j int) bool {
			gi, gj := stats[i].Habit.GroupID, stats[j].Habit.GroupID
			if gi != gj {
				return false
			}
			return !stats[i].CheckedToday && stats[j].CheckedToday
		})
		m.habits = stats
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
			if m.suggestMode == "chain" {
				m.chainSuggestItems = parseChainSuggestions(m.suggestText)
			} else {
				m.suggestItems = parseSuggestions(m.suggestText)
			}
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

	case blinkMsg:
		m.blinkOn = !m.blinkOn
		if (m.state == viewSuggest && !m.suggestDone) || (m.state == viewReview && !m.reviewDone) {
			return m, startBlink()
		}
		return m, nil

	case notesLoadedMsg:
		m.recentNotes = msg.notes
		m.recentNotesFor = msg.name
		return m, nil

	case archivedLoadedMsg:
		m.archivedHabits = []models.Habit(msg)
		return m, nil

	case archiveReloadMsg:
		m.message = msg.status
		m.isErr = false
		days := 30
		if m.weekView {
			days = 7
		}
		return m, tea.Batch(loadHabits(m.s, days), loadArchivedHabits(m.s), clearAfter())

	case oauthSuccessMsg:
		m.cfg.GoogleRefreshToken = msg.refreshToken
		config.Save(m.cfg)
		config.ForceApplyToEnv(m.cfg)
		m.state = viewList
		m.message = "Google login successful — Gemini active"
		return m, nil

	case oauthErrMsg:
		m.state = viewSettings
		m.message = "Login failed: " + msg.err.Error()
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
		case viewHabitDetail:
			return m.handleHabitDetail(msg)
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
		case viewArchive:
			return m.handleArchive(msg)
		case viewGoalInput:
			return m.handleGoalInput(msg)
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
		m.state == viewGroupNew || m.state == viewNoteInput ||
		m.state == viewGoalInput {
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

	case " ":
		if len(m.habits) == 0 {
			break
		}
		h := m.habits[m.cursor]
		name := h.Habit.Name
		chainTo := h.ChainTo
		chainToDone := isHabitDoneToday(m.habits, chainTo)
		s := m.s
		if h.CheckedToday {
			return m, func() tea.Msg {
				if err := s.DeleteCheckIn(name, time.Now()); err != nil {
					return errMsg{err}
				}
				return statusMsg("✗ " + name + " unchecked")
			}
		}
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
			if chainTo != "" && !chainToDone {
				out += "  →  " + chainTo + "?"
			}
			return statusMsg(out)
		}

	case "enter":
		if len(m.habits) == 0 {
			break
		}
		m.state = viewHabitDetail
		name := m.habits[m.cursor].Habit.Name
		return m, loadRecentNotes(m.s, name)

	case "w":
		m.weekView = !m.weekView
		days := 30
		if m.weekView {
			days = 7
		}
		return m, loadHabits(m.s, days)

	case "v":
		m.compact = !m.compact

	case "a":
		if len(m.habits) == 0 {
			break
		}
		name := m.habits[m.cursor].Habit.Name
		s := m.s
		return m, func() tea.Msg {
			if err := s.ArchiveHabit(name); err != nil {
				return errMsg{err}
			}
			return statusMsg("Archived: " + name)
		}

	case "A":
		m.state = viewArchive
		m.archiveCursor = 0
		return m, loadArchivedHabits(m.s)

	case "g":
		m.state = viewGoalInput
		m.input.Reset()
		m.input.Placeholder = "e.g. more morning energy, better sleep…"
		m.input.CharLimit = 120
		m.input.Focus()

	case "s":
		if m.suggestCancel != nil {
			m.suggestCancel()
		}
		m.state = viewSuggest
		m.suggestText = ""
		m.suggestDone = false
		m.suggestItems = nil
		m.chainSuggestItems = nil
		m.suggestCursor = 0
		m.suggestGen++
		m.suggestMode = "habit"
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
		return m, tea.Batch(waitForChunk(m.suggestCh), startBlink())

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
		return m, tea.Batch(waitForReviewChunk(m.reviewCh), startBlink())

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
		m.input.Placeholder = "Note for today…"
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
		m.editDescBuf = h.Description
		m.editFreq = h.FreqTarget
		m.editSkip = h.SkipAllowed
		m.state = viewEditHabit
		combined := h.Name
		if h.Icon != "" {
			combined = h.Icon + " " + h.Name
		}
		m.editNameBuf = combined
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
		combined := h.Name
		if h.Icon != "" {
			combined = h.Icon + " " + h.Name
		}
		m.editNameBuf = combined
		m.editDescBuf = h.Description
		m.editFreq = h.FreqTarget
		m.editSkip = h.SkipAllowed
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
			return statusMsg("Deleted: " + name)
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
		m.input.Placeholder = "// Note (optional, enter to skip)"
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
		switch m.editCursor {
		case 0:
			m.editNameBuf = strings.TrimSpace(m.input.Value())
			m.editCursor = 1
			m.input.Reset()
			m.input.SetValue(m.editDescBuf)
			m.input.CursorEnd()
		case 1:
			m.editDescBuf = strings.TrimSpace(m.input.Value())
			m.editCursor = 2
		case 2:
			m.editCursor = 3
		case 3:
			m.editCursor = 0
			m.input.Reset()
			m.input.SetValue(m.editNameBuf)
			m.input.CursorEnd()
		}
		m.input.Focus()
		return m, nil

	case "+", "=":
		if m.editCursor == 2 {
			m.editFreq++
		} else if m.editCursor == 3 {
			m.editSkip++
		}
		return m, nil

	case "-":
		if m.editCursor == 2 && m.editFreq > 0 {
			m.editFreq--
		} else if m.editCursor == 3 && m.editSkip > 0 {
			m.editSkip--
		}
		return m, nil

	case "enter":
		if m.editCursor == 0 {
			m.editNameBuf = strings.TrimSpace(m.input.Value())
		} else if m.editCursor == 1 {
			m.editDescBuf = strings.TrimSpace(m.input.Value())
		}
		icon, name := splitIcon(m.editNameBuf)
		if name == "" {
			name = m.editNameBuf
			icon = ""
		}
		if name == "" {
			return m, nil
		}
		oldName := m.editOldName
		desc := m.editDescBuf
		freq := m.editFreq
		skip := m.editSkip
		s := m.s
		m.state = viewList
		return m, func() tea.Msg {
			if err := s.UpdateHabit(oldName, name, icon, desc); err != nil {
				return errMsg{err}
			}
			if err := s.SetHabitFreq(name, freq); err != nil {
				return errMsg{err}
			}
			if err := s.SetHabitSkip(name, skip); err != nil {
				return errMsg{err}
			}
			return statusMsg("✓ " + name + " updated")
		}
	}
	if m.editCursor <= 1 {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
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
		m.input.Placeholder = "🌅 Morning (emoji + name)"
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
			return statusMsg("Group deleted: " + g.Name)
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
			return statusMsg("Group created: " + name)
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
			return statusMsg("✓ Group assigned")
		}
	}
	return m, nil
}

func (m model) handleHabitDetail(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "enter":
		m.state = viewList
	case " ":
		if len(m.habits) == 0 {
			break
		}
		h := m.habits[m.cursor]
		name := h.Habit.Name
		chainTo := h.ChainTo
		chainToDone := isHabitDoneToday(m.habits, chainTo)
		s := m.s
		m.state = viewList
		if h.CheckedToday {
			return m, func() tea.Msg {
				if err := s.DeleteCheckIn(name, time.Now()); err != nil {
					return errMsg{err}
				}
				return statusMsg("✗ " + name + " unchecked")
			}
		}
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
			if chainTo != "" && !chainToDone {
				out += "  →  " + chainTo + "?"
			}
			return statusMsg(out)
		}
	case "N":
		if len(m.habits) == 0 {
			break
		}
		h := m.habits[m.cursor]
		if !h.CheckedToday {
			break
		}
		m.noteForHabit = h.Habit.Name
		m.state = viewNoteInput
		m.input.Reset()
		m.input.SetValue(h.TodayNote)
		m.input.CursorEnd()
		m.input.Placeholder = "Note for today…"
		m.input.CharLimit = 200
		m.input.Focus()
	case "e":
		if len(m.habits) == 0 {
			break
		}
		h := m.habits[m.cursor].Habit
		m.editOldName = h.Name
		m.editCursor = 0
		m.editDescBuf = h.Description
		m.editFreq = h.FreqTarget
		m.editSkip = h.SkipAllowed
		combined := h.Name
		if h.Icon != "" {
			combined = h.Icon + " " + h.Name
		}
		m.editNameBuf = combined
		m.state = viewEditHabit
		m.input.Reset()
		m.input.SetValue(combined)
		m.input.CursorEnd()
		m.input.Placeholder = ""
		m.input.Focus()
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
				return statusMsg("Note saved for " + name)
			}
			return statusMsg("Note cleared for " + name)
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
			return statusMsg("Chain deleted")
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
		m.chainSuggestItems = nil
		m.suggestCursor = 0
		m.suggestGen++
		m.suggestMode = "chain"
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
			return statusMsg("Chain created: " + from + " → " + to)
		}
	}
	return m, nil
}

func (m model) handleArchive(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.state = viewList
	case "j", "down":
		if m.archiveCursor < len(m.archivedHabits)-1 {
			m.archiveCursor++
		}
	case "k", "up":
		if m.archiveCursor > 0 {
			m.archiveCursor--
		}
	case "r":
		if len(m.archivedHabits) == 0 {
			break
		}
		name := m.archivedHabits[m.archiveCursor].Name
		s := m.s
		return m, func() tea.Msg {
			if err := s.UnarchiveHabit(name); err != nil {
				return errMsg{err}
			}
			return archiveReloadMsg{"✓ " + name + " restored"}
		}
	case "d":
		if len(m.archivedHabits) == 0 {
			break
		}
		name := m.archivedHabits[m.archiveCursor].Name
		s := m.s
		return m, func() tea.Msg {
			if err := s.DeleteHabit(name); err != nil {
				return errMsg{err}
			}
			return archiveReloadMsg{"Deleted: " + name}
		}
	}
	return m, nil
}

func (m model) handleGoalInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.state = viewList
		return m, nil
	case "enter":
		goal := strings.TrimSpace(m.input.Value())
		if goal == "" {
			return m, nil
		}
		if m.suggestCancel != nil {
			m.suggestCancel()
		}
		m.state = viewSuggest
		m.suggestText = ""
		m.suggestDone = false
		m.suggestItems = nil
		m.chainSuggestItems = nil
		m.suggestCursor = 0
		m.suggestGen++
		m.suggestMode = "decompose"
		existing := make([]string, 0, len(m.habits))
		for _, h := range m.habits {
			existing = append(existing, h.Habit.Name)
		}
		ch := make(chan suggestChunkResult, 64)
		m.suggestCh = ch
		gen := m.suggestGen
		ctx, cancel := context.WithCancel(context.Background())
		m.suggestCancel = cancel
		goalCopy := goal
		go func() {
			defer cancel()
			_, err := ai.DecomposeGoal(ctx, goalCopy, existing, func(chunk string) {
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
		return m, tea.Batch(waitForChunk(m.suggestCh), startBlink())
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
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
		if m.suggestMode == "chain" {
			if m.suggestCursor < len(m.chainSuggestItems)-1 {
				m.suggestCursor++
			}
		} else {
			if m.suggestCursor < len(m.suggestItems)-1 {
				m.suggestCursor++
			}
		}

	case "k", "up":
		if m.suggestCursor > 0 {
			m.suggestCursor--
		}

	case " ":
		if m.suggestMode == "chain" {
			if m.suggestCursor < len(m.chainSuggestItems) {
				m.chainSuggestItems[m.suggestCursor].selected = !m.chainSuggestItems[m.suggestCursor].selected
			}
		} else {
			if m.suggestCursor < len(m.suggestItems) {
				m.suggestItems[m.suggestCursor].selected = !m.suggestItems[m.suggestCursor].selected
			}
		}

	case "a":
		if m.suggestMode == "chain" {
			anyUnselected := false
			for _, it := range m.chainSuggestItems {
				if !it.selected {
					anyUnselected = true
					break
				}
			}
			for i := range m.chainSuggestItems {
				m.chainSuggestItems[i].selected = anyUnselected
			}
		} else {
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
		}

	case "enter":
		if m.suggestMode == "chain" {
			var toApply []chainSuggestItem
			for _, it := range m.chainSuggestItems {
				if it.selected {
					toApply = append(toApply, it)
				}
			}
			if len(toApply) == 0 && m.suggestCursor < len(m.chainSuggestItems) {
				toApply = []chainSuggestItem{m.chainSuggestItems[m.suggestCursor]}
			}
			if len(toApply) == 0 {
				break
			}
			s := m.s
			items := make([]chainSuggestItem, len(toApply))
			copy(items, toApply)
			m.state = viewChainMgr
			return m, func() tea.Msg {
				var created []string
				for _, it := range items {
					if err := s.AddChain(it.from, it.to); err != nil {
						return errMsg{err}
					}
					created = append(created, it.from+" → "+it.to)
				}
				return statusMsg("Chains created: " + strings.Join(created, ", "))
			}
		}
		// habit suggest mode
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
					if !strings.HasPrefix(d, "Tip:") {
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
		if m.suggestMode != "chain" {
			m.state = viewAddInput
			m.input.Reset()
			m.input.Placeholder = "Habit (optional: 🏃 Morning run)"
			m.input.Focus()
		}
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
			m.message = "Ollama active (local)"
		case ai.ProviderGemini:
			m.state = viewGeminiMenu
			m.geminiMenuCursor = 0
		default:
			m.state = viewKeyInput
			m.input.Reset()
			m.input.Placeholder = "Paste API key…"
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
			m.input.Placeholder = "Paste Gemini API key…"
			if k := os.Getenv("GEMINI_API_KEY"); k != "" {
				m.input.SetValue(k)
			}
			m.input.Focus()
		} else {
			if m.cfg.GoogleClientID == "" {
				m.state = viewGeminiCID
				m.input.Reset()
				m.input.Placeholder = "Paste Client ID…"
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
			m.input.Placeholder = "Paste Client Secret…"
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
		m.input.Placeholder = "Paste Client ID…"
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
			m.message = p.label + " configured"
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
	case viewHabitDetail:
		return m.renderHabitDetail()
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
	case viewArchive:
		return m.renderArchive()
	case viewGoalInput:
		return m.renderGoalInput()
	default:
		return m.renderList()
	}
}

// ── layout helpers ────────────────────────────────────────────────────────────

// panel wraps content in the shared panel style with a width that fills the terminal.
func (m model) panel(s string) string {
	w := m.width - 2
	if w < 62 {
		w = 62
	}
	return panelStyle.Width(w).Render(s)
}

// innerWidth returns the usable text width inside the panel.
// panelStyle: border 1+1 + padding 2+2 = 6 chars overhead; -2 for panel margin.
func (m model) innerWidth() int {
	w := m.width - 8
	if w < 54 {
		w = 54
	}
	if w > 128 {
		w = 128
	}
	return w
}

// tinyBar renders a compact filled/empty progress bar of given width.
func tinyBar(done, total, width int) string {
	if total == 0 || width <= 0 {
		return styleMuted.Render(strings.Repeat("░", width))
	}
	filled := (done * width) / total
	if filled > width {
		filled = width
	}
	return styleOk.Render(strings.Repeat("█", filled)) + styleMuted.Render(strings.Repeat("░", width-filled))
}

// dynamicPanel renders a panel with a custom border color.
func (m model) dynamicPanel(s string, bc lipgloss.AdaptiveColor) string {
	w := m.width - 2
	if w < 62 {
		w = 62
	}
	return panelStyle.Width(w).BorderForeground(bc).Render(s)
}

// ── renderList ────────────────────────────────────────────────────────────────

func (m model) renderList() string {
	var b strings.Builder
	today := truncateDay(time.Now())

	innerW := m.innerWidth()

	// ── header ────────────────────────────────────────────────────────────────

	done, total := 0, len(m.habits)
	for _, h := range m.habits {
		if h.CheckedToday {
			done++
		}
	}
	bestStreak := 0
	for _, h := range m.habits {
		if h.Streak > bestStreak {
			bestStreak = h.Streak
		}
	}

	// precompute border color
	anyAtRisk := false
	for _, h := range m.habits {
		if h.Habit.FreqTarget > 0 {
			wd := int(time.Now().Weekday())
			daysLeft := 1
			if wd != 0 {
				daysLeft = 8 - wd
			}
			needed := h.Habit.FreqTarget - h.WeeklyDone
			if needed > 0 && daysLeft <= needed {
				anyAtRisk = true
				break
			}
		} else {
			if h.Streak > 0 && !h.CheckedToday {
				anyAtRisk = true
				break
			}
		}
	}
	var borderColor lipgloss.AdaptiveColor
	switch {
	case total > 0 && done == total:
		borderColor = colorOk
	case anyAtRisk:
		borderColor = colorWarn
	default:
		borderColor = colorBorder
	}

	appName := styleLime.Bold(true).Render("habctl")
	dateStr := styleMuted.Render(today.Format("Mon, 02 Jan 2006"))
	pad := innerW - lipgloss.Width(appName) - lipgloss.Width(dateStr)
	if pad < 1 {
		pad = 1
	}
	b.WriteString(appName + strings.Repeat(" ", pad) + dateStr + "\n")

	var statsLine strings.Builder
	if bestStreak > 0 {
		statsLine.WriteString(styleOkBold.Render(fmt.Sprintf("🔥 %d", bestStreak)) +
			styleMuted.Render(" days  ·  "))
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
		bar := styleMuted.Render("[") + tinyBar(done, total, 6) + styleMuted.Render("]")
		statsLine.WriteString(bar + " " + ps.Render(fmt.Sprintf("%d/%d", done, total)) +
			styleMuted.Render(fmt.Sprintf("  ·  %d habits", total)))
	}
	b.WriteString(statsLine.String() + "\n\n")

	// ── habit list ────────────────────────────────────────────────────────────

	if total == 0 {
		b.WriteString(styleMuted.Render("No habits yet. n to add one.") + "\n")
	} else {
		const cbW = 4   // "[✓] "
		const dotsW = 9 // 7-day dots + trailing space
		const skW = 8   // right-aligned streak column
		nameW := innerW - cbW - dotsW - skW
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
					minibar := styleMuted.Render("[") + tinyBar(gDone, gTotal, 4) + styleMuted.Render("]")
					counter := minibar + styleMuted.Render(fmt.Sprintf(" %d/%d", gDone, gTotal))
					ctrW := lipgloss.Width(counter)
					groupNameW := innerW - ctrW - 1
					if groupNameW < 1 {
						groupNameW = 1
					}
					glabel := lipgloss.NewStyle().Width(groupNameW).Render(
						styleGroup.Render(truncate(label, groupNameW-1)),
					)
					b.WriteString("\n" + glabel + " " + counter + "\n")
				} else if i > 0 {
					b.WriteString("\n")
				}
			}

			selected := i == m.cursor
			var atRisk bool
			if h.Habit.FreqTarget > 0 {
				wd := int(time.Now().Weekday())
				daysLeft := 1
				if wd != 0 {
					daysLeft = 8 - wd
				}
				needed := h.Habit.FreqTarget - h.WeeklyDone
				atRisk = needed > 0 && daysLeft <= needed
			} else {
				atRisk = h.Streak > 0 && !h.CheckedToday
			}

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

			// per-habit 7-day dots
			var dotsBuf strings.Builder
			for di, chkd := range h.Last7Days {
				isToday := di == 6
				if chkd {
					if isToday {
						dotsBuf.WriteString(styleOkBold.Render("●"))
					} else {
						dotsBuf.WriteString(styleOk.Render("●"))
					}
				} else {
					if isToday && atRisk {
						dotsBuf.WriteString(styleWarnBd.Render("○"))
					} else {
						dotsBuf.WriteString(styleMuted.Render("○"))
					}
				}
			}
			dotsCol := lipgloss.NewStyle().Width(dotsW).Render(dotsBuf.String())

			var rawName string
			if h.Habit.Icon != "" {
				rawName = h.Habit.Icon + " " + truncate(h.Habit.Name, nameW-4)
			} else {
				rawName = truncate(h.Habit.Name, nameW-1)
			}
			nameCol := lipgloss.NewStyle().Width(nameW).Render(ns.Render(rawName))

			var skContent string
			if h.Habit.FreqTarget > 0 {
				weekInfo := fmt.Sprintf("%d/%dW", h.WeeklyDone, h.Habit.FreqTarget)
				switch {
				case h.CheckedToday && h.Streak > 0:
					skContent = styleOkBold.Render("🔥 "+weekInfo) + styleMuted.Render(fmt.Sprintf(" %dw", h.Streak))
				case h.CheckedToday:
					skContent = styleOk.Render(weekInfo)
				case h.WeeklyDone > 0:
					skContent = styleWarn.Render(weekInfo)
				default:
					skContent = styleMuted.Render(weekInfo)
				}
			} else {
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
			}
			skCol := lipgloss.NewStyle().Width(skW).Align(lipgloss.Right).Render(skContent)

			b.WriteString(cb + nameCol + dotsCol + skCol + "\n")

			if !m.compact {
				const descMaxW = 58
				const subIndent = "      "
				if h.Habit.Description != "" {
					b.WriteString(subIndent + styleMuted.Render(truncate(h.Habit.Description, descMaxW)) + "\n")
				}
				if h.TodayNote != "" {
					b.WriteString(subIndent + styleMuted.Render("📝 "+truncate(h.TodayNote, descMaxW-3)) + "\n")
				}
				if h.ChainTo != "" {
					b.WriteString(subIndent + styleMuted.Render("→ "+h.ChainTo) + "\n")
				}
			}
			b.WriteString("\n")
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

	fk := func(key, label string) string {
		return styleMuted.Render("[") + styleLime.Render(key) + styleMuted.Render("] "+label)
	}
	footer := fk("space", "✓/✗") + styleMuted.Render("  ") +
		fk("↵", "open") + styleMuted.Render("  ") +
		fk("n", "new") + styleMuted.Render("  ") +
		fk("e", "edit") + styleMuted.Render("  ") +
		fk("s", "AI") + styleMuted.Render("  ") +
		fk("r", "review") + styleMuted.Render("  ") +
		fk("?", "help") + styleMuted.Render("  ") +
		fk("q", "quit")
	b.WriteString(footer)
	return m.dynamicPanel(b.String(), borderColor)
}


// ── renderAddInput ────────────────────────────────────────────────────────────

func (m model) renderAddInput() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("New Habit") + "\n\n")
	b.WriteString(styleMuted.Render("Tip: emoji prefix — 🏃 Running, ☕ Coffee, 📚 Reading") + "\n\n")
	b.WriteString(m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter continue · esc cancel"))
	return m.panel(b.String())
}

// ── renderAddDesc ─────────────────────────────────────────────────────────────

func (m model) renderAddDesc() string {
	var b strings.Builder
	icon := ""
	if m.addingIcon != "" {
		icon = m.addingIcon + " "
	}
	b.WriteString(styleLime.Bold(true).Render(icon+m.addingName) + "\n\n")
	b.WriteString(styleMuted.Render("Short note? (enter to skip)") + "\n\n")
	b.WriteString(m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter save · esc skip"))
	return m.panel(b.String())
}

// ── renderEditHabit ───────────────────────────────────────────────────────────

func (m model) renderEditHabit() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Edit Habit") + "\n\n")

	row := func(active bool, label, value string) {
		cursor := "  "
		ls := styleMuted
		vs := styleMuted
		if active {
			cursor = styleLime.Render("▶ ")
			ls = styleFg
			vs = styleFg.Bold(true)
		}
		b.WriteString(cursor + ls.Render(label+":") + "  " + vs.Render(value) + "\n")
	}

	switch m.editCursor {
	case 0:
		b.WriteString(styleLime.Render("▶ ") + styleFg.Render("Name / Icon:") + "\n")
		b.WriteString("    " + m.input.View() + "\n\n")
		row(false, "Description", m.editDescBuf)
	case 1:
		row(false, "Name / Icon", m.editNameBuf)
		b.WriteString("\n")
		b.WriteString(styleLime.Render("▶ ") + styleFg.Render("Description:") + "\n")
		b.WriteString("    " + m.input.View() + "\n")
	default:
		row(false, "Name / Icon", m.editNameBuf)
		row(false, "Description", m.editDescBuf)
	}

	b.WriteString("\n")

	freqActive := m.editCursor == 2
	skipActive := m.editCursor == 3

	freqCursor := "  "
	freqStyle := styleMuted
	if freqActive {
		freqCursor = styleLime.Render("▶ ")
		freqStyle = styleFg
	}
	freqVal := "daily"
	if m.editFreq > 0 {
		freqVal = fmt.Sprintf("%d× per week", m.editFreq)
	}
	b.WriteString(freqCursor + freqStyle.Render("Frequency:") + "  ")
	if freqActive {
		b.WriteString(styleFg.Bold(true).Render(freqVal) + "  " + styleMuted.Render("+/- to change"))
	} else {
		b.WriteString(styleMuted.Render(freqVal))
	}
	b.WriteString("\n")

	skipCursor := "  "
	skipStyle := styleMuted
	if skipActive {
		skipCursor = styleLime.Render("▶ ")
		skipStyle = styleFg
	}
	skipVal := "no skip"
	if m.editSkip > 0 {
		skipVal = fmt.Sprintf("%d missed day%s ok", m.editSkip, func() string {
			if m.editSkip == 1 {
				return ""
			}
			return "s"
		}())
	}
	b.WriteString(skipCursor + skipStyle.Render("Skip tolerance:") + "  ")
	if skipActive {
		b.WriteString(styleFg.Bold(true).Render(skipVal) + "  " + styleMuted.Render("+/- to change"))
	} else {
		b.WriteString(styleMuted.Render(skipVal))
	}
	b.WriteString("\n")

	b.WriteString("\n" + styleMuted.Render("enter save · tab next field · +/- for numbers · esc cancel"))
	return m.panel(b.String())
}

// ── renderGroupMgr ────────────────────────────────────────────────────────────

func (m model) renderGroupMgr() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Groups") + "\n\n")

	if len(m.groups) == 0 {
		b.WriteString(styleMuted.Render("No groups yet. a to create one.") + "\n\n")
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

	b.WriteString(styleMuted.Render("a new · d delete · j/k navigate · esc back"))
	return m.panel(b.String())
}

// ── renderGroupNew ────────────────────────────────────────────────────────────

func (m model) renderGroupNew() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("New Group") + "\n\n")
	b.WriteString(styleMuted.Render("Tip: start with emoji — 🌅 Morning, 💻 Work, 🌙 Evening") + "\n\n")
	b.WriteString(m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter create · esc cancel"))
	return m.panel(b.String())
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
	b.WriteString(styleLime.Bold(true).Render("Assign Group") + "\n")
	b.WriteString(styleMuted.Render("→ "+habitName) + "\n\n")

	options := append([]models.Group{{Name: "None (ungrouped)", Icon: "○"}}, m.groups...)
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

	b.WriteString("\n" + styleMuted.Render("enter select · j/k navigate · esc cancel"))
	return m.panel(b.String())
}

// ── renderStats ───────────────────────────────────────────────────────────────

func (m model) renderStats() string {
	cal := m.calData
	total := cal.TotalHabits
	byDate := cal.ByDate

	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Stats") + "\n\n")

	if total == 0 {
		b.WriteString(styleMuted.Render("Add habits first (n), then data will appear here.") + "\n")
		b.WriteString("\n" + styleMuted.Render("esc back"))
		return m.panel(b.String())
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

	b.WriteString(styleMuted.Render("// overview") + "\n")
	b.WriteString(fmt.Sprintf("  %s %s   %s %s   %s %s\n",
		numV.Render(fmt.Sprintf("%d", daysTracked)), lbl.Render("days tracked"),
		numV.Render(fmt.Sprintf("%.0f%%", avgCompletion)), lbl.Render("avg"),
		numV.Render(fmt.Sprintf("%d", perfectDays)), lbl.Render("perfect days"),
	))
	if bestStreak > 0 {
		b.WriteString(fmt.Sprintf("  %s %s  %s\n",
			lbl.Render("🔥 current:"),
			numV.Render(fmt.Sprintf("%d days", bestStreak)),
			lbl.Render("— "+truncate(bestName, 24)),
		))
	}
	if bestLongest > bestStreak {
		b.WriteString(fmt.Sprintf("  %s %s  %s\n",
			lbl.Render("   longest:"),
			numV.Render(fmt.Sprintf("%d days", bestLongest)),
			lbl.Render("— "+truncate(bestLongestName, 24)),
		))
	}

	// ── heatmap ──────────────────────────────────────────────────────────────
	const weeks = 26
	b.WriteString("\n" + styleMuted.Render("// contributions (26 weeks)") + "\n")

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
	b.WriteString("\n" + styleMuted.Render("// day of week") + "\n")
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

	b.WriteString("\n" + styleMuted.Render("esc back"))
	return m.panel(b.String())
}

// ── renderSuggest ─────────────────────────────────────────────────────────────

func (m model) renderSuggest() string {
	var b strings.Builder
	providerLabel := ""
	if info, err := ai.Detect(); err == nil {
		providerLabel = styleMuted.Render("via " + info.Display)
	} else {
		providerLabel = styleWarn.Render("no provider — S for settings")
	}

	if m.suggestMode == "chain" {
		b.WriteString(styleLime.Bold(true).Render("Chain Suggestions") + "  " + providerLabel + "\n\n")

		if len(m.chainSuggestItems) > 0 {
			checkOff := styleMuted.Render("[ ]")
			checkOn := styleOk.Bold(true).Render("[✓]")
			indent := "       "

			for i, it := range m.chainSuggestItems {
				if i > 0 {
					b.WriteString("\n")
				}
				selected := i == m.suggestCursor
				cursor := "  "
				nameStyle := styleMuted
				if selected {
					cursor = styleLime.Render("▶ ")
					nameStyle = styleFg.Bold(true)
				}
				chk := checkOff
				if it.selected {
					chk = checkOn
				}
				b.WriteString(cursor + chk + " " + nameStyle.Render(it.from) +
					styleMuted.Render(" → ") + nameStyle.Render(it.to) + "\n")
				if it.reason != "" {
					for _, wl := range strings.Split(wordWrap(it.reason, 62), "\n") {
						if wl != "" {
							b.WriteString(indent + styleMuted.Render(wl) + "\n")
						}
					}
				}
			}

			b.WriteString("\n")
			selectedCount := 0
			for _, it := range m.chainSuggestItems {
				if it.selected {
					selectedCount++
				}
			}
			if selectedCount > 0 {
				b.WriteString(styleOk.Render(fmt.Sprintf("%d selected", selectedCount)) + "  ")
			}
			b.WriteString(styleMuted.Render("space ✓ · enter apply · a all · j/k · esc back"))
		} else if !m.suggestDone {
			b.WriteString(styleMuted.Render("Analysing habit connections…") + "\n\n")
			b.WriteString(styleLime.Render("▌"))
		} else {
			b.WriteString(styleWarn.Render("No suggestions. I need at least 2 habits.") + "\n")
			b.WriteString(styleMuted.Render("esc back"))
		}
		return m.panel(b.String())
	}

	// ── habit / decompose suggest mode ───────────────────────────────────────
	title := "Habit Suggestions"
	loadingMsg := "Generating suggestions…"
	if m.suggestMode == "decompose" {
		title = "Goal Habits"
		loadingMsg = "Analysing goal and creating habits…"
	}
	b.WriteString(styleLime.Bold(true).Render(title) + "  " + providerLabel + "\n\n")

	blinkCursor := "▌"
	if m.blinkOn {
		blinkCursor = " "
	}

	if len(m.suggestItems) > 0 {
		checkOff := styleMuted.Render("[ ]")
		checkOn := styleOk.Bold(true).Render("[✓]")
		indent := "       "

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
			b.WriteString(styleOk.Render(fmt.Sprintf("%d selected", selectedCount)) + "  ")
		}
		b.WriteString(styleMuted.Render("space ✓ · enter add · a all · j/k · esc back"))
	} else if !m.suggestDone {
		b.WriteString(styleMuted.Render(loadingMsg) + "\n\n")
		b.WriteString(styleLime.Render(blinkCursor))
	} else {
		b.WriteString(styleWarn.Render("Format not recognised.") + "\n")
		b.WriteString(styleMuted.Render("s   try again") + "\n")
		b.WriteString(styleMuted.Render("n   add manually") + "\n")
		b.WriteString(styleMuted.Render("esc back"))
	}

	return m.panel(b.String())
}

// ── renderSettings ────────────────────────────────────────────────────────────

func (m model) renderSettings() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("AI Provider") + "\n")
	b.WriteString(styleMuted.Render("Select your AI provider and configure it.") + "\n\n")

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
			badge = styleOk.Render("● local")
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
				badge = styleOk.Render("● OAuth active")
			} else if activeKey != "" {
				suffix := activeKey
				if len(suffix) > 4 {
					suffix = "…" + suffix[len(suffix)-4:]
				}
				badge = styleOk.Render("● Key set") + styleMuted.Render(" ("+suffix+")")
			} else {
				badge = styleMuted.Render("○ no key")
			}
		}
		if active {
			badge += " " + styleLime.Render("← active")
		}
		b.WriteString(cursor + label + "  " + badge + "\n")
	}
	b.WriteString("\n" + styleMuted.Render("enter configure · j/k navigate · esc back"))
	return m.panel(b.String())
}

// ── renderKeyInput ────────────────────────────────────────────────────────────

func (m model) renderKeyInput() string {
	var b strings.Builder
	p := providers[m.settingsCursor]
	b.WriteString(styleLime.Bold(true).Render(p.label+" setup") + "\n\n")
	switch p.id {
	case ai.ProviderGemini:
		b.WriteString(styleMuted.Render("o  open browser: aistudio.google.com") + "\n")
		b.WriteString(styleMuted.Render("   → log in with Google → 'Get API key'") + "\n\n")
	default:
		if p.keyPage != "" {
			b.WriteString(styleOk.Render("o") + styleMuted.Render("  opens "+p.keyPage) + "\n\n")
		}
	}
	b.WriteString(styleMuted.Render("API Key (Cmd+V):") + "\n")
	b.WriteString("  " + m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter save · o open browser · esc back"))
	return m.panel(b.String())
}

// ── renderGeminiMenu / CID / CS / OAuthWait ───────────────────────────────────

func (m model) renderGeminiMenu() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Google Gemini") + "\n\n")
	options := []struct{ label, desc string }{
		{"Browser Login (Google Account)", "No key needed — login in browser"},
		{"API Key", "From aistudio.google.com"},
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
		b.WriteString(styleOk.Render("● already logged in (OAuth)") + "\n\n")
	}
	b.WriteString(styleMuted.Render("enter select · j/k navigate · esc back"))
	return m.panel(b.String())
}

func (m model) renderGeminiCID() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Set up Google OAuth2 Client") + "\n\n")
	b.WriteString(styleOk.Render("o") + styleMuted.Render("  opens console.cloud.google.com/apis/credentials") + "\n\n")
	b.WriteString(styleMuted.Render("1. Select project · 2. Create Credentials → OAuth 2.0 Client ID") + "\n")
	b.WriteString(styleMuted.Render("3. Type: Desktop App · 4. Copy Client ID") + "\n\n")
	b.WriteString(styleMuted.Render("Client ID:") + "\n")
	b.WriteString("  " + m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter continue · o open browser · esc back"))
	return m.panel(b.String())
}

func (m model) renderGeminiCS() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Google OAuth2 Client Secret") + "\n\n")
	b.WriteString(styleMuted.Render("Client Secret from the same credentials page:") + "\n\n")
	b.WriteString(styleMuted.Render("Client Secret:") + "\n")
	b.WriteString("  " + m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter start browser login · esc back"))
	return m.panel(b.String())
}

func (m model) renderOAuthWait() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Waiting for Google login…") + "\n\n")
	b.WriteString(styleMuted.Render(
		"Browser opened.\n\n"+
			"1. Log in with your Google account\n"+
			"2. Allow habctl access\n"+
			"3. Page shows \"Login successful\" → done\n\n"+
			"Timeout: 5 minutes") + "\n")
	b.WriteString("\n" + styleLime.Render("⠿ ") + styleMuted.Render("waiting…"))
	return m.panel(b.String())
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
	b.WriteString(row("space", "check in / undo check-in (toggle)"))
	b.WriteString(row("enter", "open habit (detail, description, note history)"))
	b.WriteString(row("N", "add note to today's check-in"))
	b.WriteString(row("n", "new habit (optional emoji prefix)"))
	b.WriteString(row("e", "edit habit (name, desc, frequency, skip)"))
	b.WriteString(row("a", "archive habit (history preserved)"))
	b.WriteString(row("A", "open archive (restore / delete)"))
	b.WriteString(row("d", "delete habit permanently"))
	b.WriteString(section("Groups"))
	b.WriteString(row("m", "move habit to group"))
	b.WriteString(row("G", "manage groups (add, delete)"))
	b.WriteString(section("AI & Views"))
	b.WriteString(row("s", "AI suggestions (context-aware)"))
	b.WriteString(row("g", "goal → 3 linked habits (decompose)"))
	b.WriteString(row("r", "AI weekly review — pattern coaching briefing"))
	b.WriteString(row("t", "stats — heatmap & completion"))
	b.WriteString(row("c", "manage habit chains"))
	b.WriteString(row("S", "settings — provider & API keys"))
	b.WriteString(row("v", "compact/normal toggle (hide/show descriptions)"))
	b.WriteString(row("w", "toggle 7d / 30d streak window"))
	b.WriteString(section("Status"))
	b.WriteString(row(styleOk.Render("✓  green"), "checked in today"))
	b.WriteString(row(styleWarn.Render("!  amber"), "streak at risk — check in before midnight!"))
	b.WriteString(row(styleMuted.Render("·  gray"), "not done this day"))
	b.WriteString(section("Other"))
	b.WriteString(row("?", "toggle this help screen"))
	b.WriteString(row("q / ctrl+c", "quit"))
	b.WriteString("\n  " + styleMuted.Render("esc / ?  close help"))
	return m.panel(b.String())
}

// ── renderHabitDetail ─────────────────────────────────────────────────────────

func (m model) renderHabitDetail() string {
	if len(m.habits) == 0 {
		return m.panel(styleMuted.Render("No habit selected."))
	}
	h := m.habits[m.cursor]
	habit := h.Habit

	const maxW = 62 // max content width — keeps text readable, not edge-to-edge
	ind := "  "     // section indent

	var b strings.Builder

	// ── title + status ────────────────────────────────────────────────────────
	title := habit.Name
	if habit.Icon != "" {
		title = habit.Icon + "  " + habit.Name
	}
	b.WriteString(styleLime.Bold(true).Render(title) + "\n")

	switch {
	case h.CheckedToday && h.Streak > 0:
		b.WriteString(styleOk.Render(fmt.Sprintf("✓ today  ·  🔥 %d days", h.Streak)) + "\n")
	case h.CheckedToday:
		b.WriteString(styleOk.Render("✓ done today") + "\n")
	case h.Streak > 0:
		b.WriteString(styleWarn.Render(fmt.Sprintf("not done yet  ·  🔥 %d day streak at risk", h.Streak)) + "\n")
	default:
		b.WriteString(styleMuted.Render("not done today") + "\n")
	}

	// ── description ───────────────────────────────────────────────────────────
	if habit.Description != "" {
		b.WriteString("\n")
		for _, line := range strings.Split(wordWrap(habit.Description, maxW-len(ind)), "\n") {
			if line == "" {
				b.WriteString("\n")
			} else {
				b.WriteString(ind + styleFg.Render(line) + "\n")
			}
		}
	}

	// ── today's note ──────────────────────────────────────────────────────────
	if h.TodayNote != "" {
		b.WriteString("\n")
		b.WriteString(ind + styleMuted.Render("Note today") + "\n")
		for _, line := range strings.Split(wordWrap(h.TodayNote, maxW-len(ind)), "\n") {
			if line != "" {
				b.WriteString(ind + styleFg.Render(line) + "\n")
			}
		}
	}

	// ── recent notes history ──────────────────────────────────────────────────
	if m.recentNotesFor == habit.Name && len(m.recentNotes) > 0 {
		todayStr := time.Now().Format("2006-01-02")
		var pastNotes []models.NoteEntry
		for _, n := range m.recentNotes {
			if n.Date != todayStr {
				pastNotes = append(pastNotes, n)
			}
		}
		if len(pastNotes) > 0 {
			b.WriteString("\n")
			b.WriteString(ind + styleMuted.Render("Past notes") + "\n")
			for _, n := range pastNotes {
				b.WriteString(ind + styleMuted.Render(n.Date+"  ") +
					styleFg.Render(truncate(n.Note, maxW-len(ind)-13)) + "\n")
			}
		}
	}

	// ── 7-day history ─────────────────────────────────────────────────────────
	b.WriteString("\n")
	b.WriteString(ind + styleMuted.Render("Last 7 days") + "\n")
	today := truncateDay(time.Now())
	dayAbbrDE := [7]string{"Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"}
	var dayRow, dotRow strings.Builder
	for i := 0; i < 7; i++ {
		d := today.AddDate(0, 0, i-6)
		abbr := fmt.Sprintf("%-4s", dayAbbrDE[int(d.Weekday())])
		if h.Last7Days[i] {
			dayRow.WriteString(styleOk.Render(abbr))
			dotRow.WriteString(styleOkBold.Render("✓   "))
		} else if i == 6 {
			dayRow.WriteString(styleWarn.Render(abbr))
			dotRow.WriteString(styleMuted.Render("·   "))
		} else {
			dayRow.WriteString(styleMuted.Render(abbr))
			dotRow.WriteString(styleMuted.Render("·   "))
		}
	}
	b.WriteString(ind + dayRow.String() + "\n")
	b.WriteString(ind + dotRow.String() + "\n")

	// ── stats ─────────────────────────────────────────────────────────────────
	b.WriteString("\n")
	num := styleLime.Bold(true)
	lbl := styleMuted
	if habit.FreqTarget > 0 {
		b.WriteString(ind + num.Render(fmt.Sprintf("%d/%d", h.WeeklyDone, habit.FreqTarget)) +
			" " + lbl.Render("this week") +
			"   " + num.Render(fmt.Sprintf("%d", h.Streak)) + " " + lbl.Render("week streak") + "\n")
		b.WriteString(ind + lbl.Render(fmt.Sprintf("📅 %d× per week", habit.FreqTarget)) + "\n")
	} else {
		b.WriteString(ind + num.Render(fmt.Sprintf("%d", h.Streak)) + " " + lbl.Render("streak") +
			"   " + num.Render(fmt.Sprintf("%d", h.LongestStreak)) + " " + lbl.Render("longest") +
			"   " + num.Render(fmt.Sprintf("%d", h.TotalDays)) + " " + lbl.Render("days/30") + "\n")
	}
	if habit.SkipAllowed > 0 {
		b.WriteString(ind + lbl.Render(fmt.Sprintf("⏭ %d skip%s allowed", habit.SkipAllowed, func() string {
			if habit.SkipAllowed == 1 {
				return ""
			}
			return "s"
		}())) + "\n")
	}

	// ── chain ─────────────────────────────────────────────────────────────────
	if h.ChainTo != "" {
		b.WriteString("\n")
		b.WriteString(ind + styleMuted.Render("Next  →  ") + styleFg.Render(h.ChainTo) + "\n")
	}

	// ── footer ────────────────────────────────────────────────────────────────
	b.WriteString("\n")
	if h.CheckedToday {
		b.WriteString(styleMuted.Render("space ✓ · N note · e edit · esc back"))
	} else {
		b.WriteString(styleMuted.Render("space check in · e edit · esc back"))
	}
	return m.panel(b.String())
}

// ── renderReview ──────────────────────────────────────────────────────────────

func (m model) renderReview() string {
	var b strings.Builder
	providerLabel := ""
	if info, err := ai.Detect(); err == nil {
		providerLabel = styleMuted.Render("via " + info.Display)
	} else {
		providerLabel = styleWarn.Render("no provider — S for settings")
	}
	b.WriteString(styleLime.Bold(true).Render("Weekly Review") + "  " + providerLabel + "\n\n")

	blinkCursor := "▌"
	if m.blinkOn {
		blinkCursor = " "
	}
	if m.reviewText != "" {
		for _, line := range strings.Split(m.reviewText, "\n") {
			if strings.HasPrefix(line, "## ") {
				b.WriteString("\n" + styleLime.Bold(true).Render(strings.TrimPrefix(line, "## ")) + "\n")
			} else {
				b.WriteString(styleMuted.Render(line) + "\n")
			}
		}
		if !m.reviewDone {
			b.WriteString(styleLime.Render(blinkCursor))
		}
	} else if !m.reviewDone {
		b.WriteString(styleMuted.Render("Analysing last week…") + "\n\n")
		b.WriteString(styleLime.Render(blinkCursor))
	}

	b.WriteString("\n\n" + styleMuted.Render("esc back · r again"))
	return m.panel(b.String())
}

// ── renderNoteInput ───────────────────────────────────────────────────────────

func (m model) renderNoteInput() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Note") + styleMuted.Render(" for "+m.noteForHabit) + "\n\n")
	b.WriteString("  " + m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter save · esc cancel"))
	return m.panel(b.String())
}

// ── renderArchive ─────────────────────────────────────────────────────────────

func (m model) renderArchive() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Archive") + "\n")
	b.WriteString(styleMuted.Render("Archived habits — history is preserved.") + "\n\n")

	if len(m.archivedHabits) == 0 {
		b.WriteString(styleMuted.Render("Archive is empty. a in the list to archive habits.") + "\n\n")
	} else {
		for i, h := range m.archivedHabits {
			cursor := "  "
			ns := styleMuted
			if i == m.archiveCursor {
				cursor = styleLime.Render("▶ ")
				ns = styleFg
			}
			name := h.Name
			if h.Icon != "" {
				name = h.Icon + " " + name
			}
			b.WriteString(cursor + ns.Render(name) + "\n")
			if h.Description != "" {
				b.WriteString("      " + styleMuted.Render(truncate(h.Description, 52)) + "\n")
			}
		}
		b.WriteString("\n")
	}

	if m.message != "" {
		msgStyle := styleOk
		if m.isErr {
			msgStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
		}
		b.WriteString(msgStyle.Render(m.message) + "\n\n")
	}

	b.WriteString(styleMuted.Render("r restore · d delete permanently · j/k navigate · esc back"))
	return m.panel(b.String())
}

// ── renderGoalInput ───────────────────────────────────────────────────────────

func (m model) renderGoalInput() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Goal → 3 linked Habits") + "\n\n")
	b.WriteString(styleMuted.Render("AI suggests 3 habits that reinforce each other.") + "\n")
	b.WriteString(styleMuted.Render("Examples: more morning energy · better sleep · more productive") + "\n\n")
	b.WriteString(m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter send · esc back"))
	return m.panel(b.String())
}

// ── renderChainMgr ────────────────────────────────────────────────────────────

func (m model) renderChainMgr() string {
	var b strings.Builder
	b.WriteString(styleLime.Bold(true).Render("Habit Chains") + "\n")
	b.WriteString(styleMuted.Render("After habit A, do habit B next.") + "\n\n")

	if len(m.chains) == 0 {
		b.WriteString(styleMuted.Render("No chains yet. a to add, s for AI suggestions.") + "\n")
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

	b.WriteString("\n" + styleMuted.Render("a add · d delete · s AI suggestions · esc back"))
	return m.panel(b.String())
}

// ── renderChainPick ───────────────────────────────────────────────────────────

func (m model) renderChainPick() string {
	var b strings.Builder
	if m.chainFromName == "" {
		b.WriteString(styleLime.Bold(true).Render("Create Chain") + "\n")
		b.WriteString(styleMuted.Render("Step 1: Which habit comes first?") + "\n\n")
	} else {
		b.WriteString(styleLime.Bold(true).Render("Create Chain") + "\n")
		b.WriteString(styleMuted.Render("Step 2: Which habit follows ") +
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

	b.WriteString("\n" + styleMuted.Render("enter select · esc back"))
	return m.panel(b.String())
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

func loadRecentNotes(s *store.Store, name string) tea.Cmd {
	return func() tea.Msg {
		notes, _ := s.GetRecentNotes(name, 5)
		return notesLoadedMsg{name: name, notes: notes}
	}
}

func loadArchivedHabits(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		habits, err := s.ListArchivedHabits()
		if err != nil {
			return errMsg{err}
		}
		return archivedLoadedMsg(habits)
	}
}

func startBlink() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(_ time.Time) tea.Msg {
		return blinkMsg{}
	})
}

func isHabitDoneToday(habits []models.HabitStats, name string) bool {
	if name == "" {
		return false
	}
	for _, h := range habits {
		if h.Habit.Name == name {
			return h.CheckedToday
		}
	}
	return false
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
// Each block is delimited by "###" and contains Name:, Time:, Benefit:, Tip: fields.
func parseSuggestions(text string) []suggestItem {
	var items []suggestItem
	for _, block := range strings.Split(text, "###") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		var name, timeStr, benefit, tip string
		for _, raw := range strings.Split(block, "\n") {
			line := strings.TrimSpace(raw)
			switch {
			case strings.HasPrefix(line, "Name:"):
				name = strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
			case strings.HasPrefix(line, "Time:"):
				timeStr = strings.TrimSpace(strings.TrimPrefix(line, "Time:"))
			case strings.HasPrefix(line, "Benefit:"):
				benefit = strings.TrimSpace(strings.TrimPrefix(line, "Benefit:"))
			case strings.HasPrefix(line, "Tip:"):
				tip = strings.TrimSpace(strings.TrimPrefix(line, "Tip:"))
			}
		}
		if name == "" {
			continue
		}
		header := name
		if timeStr != "" {
			header = name + "  ·  " + timeStr
		}
		var details []string
		if benefit != "" {
			details = append(details, benefit)
		}
		if tip != "" {
			details = append(details, "Tip: "+tip)
		}
		items = append(items, suggestItem{name: name, header: header, details: details})
	}
	return items
}

// parseChainSuggestions parses the From:/To:/Why: block format from SuggestChains.
func parseChainSuggestions(text string) []chainSuggestItem {
	var items []chainSuggestItem
	for _, block := range strings.Split(text, "###") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		var from, to, why string
		for _, raw := range strings.Split(block, "\n") {
			line := strings.TrimSpace(raw)
			switch {
			case strings.HasPrefix(line, "From:"):
				from = strings.TrimSpace(strings.TrimPrefix(line, "From:"))
			case strings.HasPrefix(line, "To:"):
				to = strings.TrimSpace(strings.TrimPrefix(line, "To:"))
			case strings.HasPrefix(line, "Why:"):
				why = strings.TrimSpace(strings.TrimPrefix(line, "Why:"))
			}
		}
		if from == "" || to == "" {
			continue
		}
		items = append(items, chainSuggestItem{from: from, to: to, reason: why})
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
