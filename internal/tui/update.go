package tui

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/habctl/internal/ai"
	"github.com/aeon022/habctl/internal/auth"
	"github.com/aeon022/habctl/internal/config"
	"github.com/aeon022/habctl/internal/models"
	"github.com/sahilm/fuzzy"
)

// ── bubbletea interface ───────────────────────────────────────────────────────

// focusReloadAfter is how stale the list must be before a window-focus event
// reloads it.
const focusReloadAfter = 5 * time.Second

func (m model) Init() tea.Cmd {
	days := 30
	if m.weekView {
		days = 7
	}
	return tea.Batch(loadHabits(m.s, days), loadGroups(m.s), loadChains(m.s))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.MouseWheelMsg:
		switch msg.Button {
		case tea.MouseWheelUp:
			if m.state == viewList && m.cursor > 0 {
				m.cursor--
			}
		case tea.MouseWheelDown:
			if m.state == viewList && m.cursor < len(m.habits)-1 {
				m.cursor++
			}
		}
		return m, nil

	case tea.MouseClickMsg:
		if m.state != viewList {
			return m, nil
		}
		switch msg.Button {
		case tea.MouseLeft:
			if i := m.rowHitTest(msg.Y); i >= 0 {
				now := time.Now()
				if i == m.lastClickRow && now.Sub(m.lastClickAt) < doubleClickWindow {
					m.cursor = i
					m.lastClickRow = -1 // consumed, so a third click starts fresh
					m.state = viewHabitDetail
					name := m.habits[i].Habit.Name
					return m, loadRecentNotes(m.s, name)
				}
				m.cursor = i
				m.lastClickRow = i
				m.lastClickAt = now
			}
		case tea.MouseRight:
			// Toggle check-in on whatever row was clicked, not the cursor
			// row — a quick-action shouldn't require selecting first.
			if i := m.rowHitTest(msg.Y); i >= 0 {
				return m, toggleHabitCheckinCmd(m.s, m.habits, i)
			}
		}
		return m, nil

	case tea.MouseMotionMsg:
		if m.state == viewList {
			m.hoverRow = m.rowHitTest(msg.Y)
		}
		return m, nil

	case tea.FocusMsg:
		// Window regained focus: data may have changed elsewhere (CLI, MCP,
		// another terminal). Reload — but only while just browsing the list,
		// so no input/confirm/palette state is ever disturbed, and not on
		// every focus flicker.
		if m.state != viewList || m.batchMode || m.confirmPrompt != "" || m.reloading ||
			time.Since(m.lastLoad) < focusReloadAfter {
			return m, nil
		}
		m.reloading = true
		days := 30
		if m.weekView {
			days = 7
		}
		return m, loadHabits(m.s, days)

	case habitsLoadedMsg:
		m.lastLoad, m.reloading = time.Now(), false
		stats := []models.HabitStats(msg)
		sort.SliceStable(stats, func(i, j int) bool {
			gi, gj := stats[i].Habit.GroupID, stats[j].Habit.GroupID
			if gi != gj {
				return false
			}
			return !stats[i].CheckedToday && stats[j].CheckedToday
		})
		if m.s != nil { // detail-panel heatmap data; one small query per load
			m.heat, _ = m.s.GetCheckinDatesByHabit(12)
		}
		m.allHabits = stats
		m.habits = filterHabits(stats, m.filterQ)
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

	case groupDeletedMsg:
		m.lastDeletedGroup = &msg.group
		m.lastDeletedGroupHabits = msg.habitNames
		m.message = "Group deleted: " + msg.group.Name + " — press u to undo"
		m.isErr = false
		days := 30
		if m.weekView {
			days = 7
		}
		return m, tea.Batch(loadHabits(m.s, days), loadGroups(m.s), clearAfterUndo())

	case chainDeletedMsg:
		m.lastDeletedChain = &msg.chain
		m.message = fmt.Sprintf("Chain deleted: %s → %s — press u to undo", msg.chain.FromName, msg.chain.ToName)
		m.isErr = false
		return m, tea.Batch(loadChains(m.s), clearAfterUndo())

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

	case clearUndoMsg:
		m.message = ""
		m.isErr = false
		m.lastDeletedGroup = nil
		m.lastDeletedGroupHabits = nil
		m.lastDeletedChain = nil
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

	case habitCheckinsLoadedMsg:
		m.detailHeatmap = msg.dates
		m.detailHeatmapFor = msg.name
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
		config.ApplyToEnv(m.cfg, true)
		m.state = viewList
		m.message = "Google login successful — Gemini active"
		return m, nil

	case oauthErrMsg:
		m.state = viewSettings
		m.message = "Login failed: " + msg.err.Error()
		m.isErr = true
		return m, clearAfter()

	case tea.KeyPressMsg:
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
		case viewConfirm:
			return m.handleConfirm(msg)
		case viewFilterInput:
			return m.handleFilterInput(msg)
		case viewCommand:
			return m.handleCommandPalette(msg)
		case viewPresets:
			return m.handlePresets(msg)
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
		m.state == viewGoalInput || m.state == viewFilterInput {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// ── key handlers ─────────────────────────────────────────────────────────────

func (m model) handleList(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.batchMode {
		switch msg.String() {
		case "esc":
			m.batchMode = false
			m.batchSelected = nil
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.habits)-1 {
				m.cursor++
			}
		case "space":
			if len(m.habits) == 0 {
				break
			}
			name := m.habits[m.cursor].Habit.Name
			if m.batchSelected[name] {
				delete(m.batchSelected, name)
			} else {
				m.batchSelected[name] = true
			}
		case "A":
			for _, h := range m.habits {
				m.batchSelected[h.Habit.Name] = true
			}
		case "enter":
			if len(m.batchSelected) == 0 {
				break
			}
			names := make([]string, 0, len(m.batchSelected))
			for n := range m.batchSelected {
				names = append(names, n)
			}
			m.batchMode = false
			m.batchSelected = nil
			return m, batchArchiveCmd(m.s, names)
		}
		return m, nil
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case ":":
		m.state = viewCommand
		m.cmdCursor = 0
		m.input.Placeholder = "command…"
		m.input.SetValue("")
		m.input.CursorEnd()
		m.input.Focus()
		return m, nil

	case "/":
		m.state = viewFilterInput
		m.input.Placeholder = "filter habits…"
		m.input.SetValue(m.filterQ)
		m.input.CursorEnd()
		m.input.Focus()
		return m, nil

	case "esc":
		if m.filterQ != "" {
			m.filterQ = ""
			m.habits = filterHabits(m.allHabits, "")
			m.cursor = 0
		}

	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// jump to the nth visible (on-screen) habit — mirrors
		// visibleHabitsWithStart/habitWindowHeight, the same window
		// rowHitTest and the main render loop already use.
		n := int(msg.String()[0] - '0')
		visible, start := m.visibleHabitsWithStart(m.habitWindowHeight())
		if n <= len(visible) {
			m.cursor = start + n - 1
		}

	case "j", "down":
		if m.cursor < len(m.habits)-1 {
			m.cursor++
		}

	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}

	case "space":
		if len(m.habits) == 0 {
			break
		}
		return m, toggleHabitCheckinCmd(m.s, m.habits, m.cursor)

	case "enter":
		if len(m.habits) == 0 {
			break
		}
		m.state = viewHabitDetail
		habit := m.habits[m.cursor].Habit
		return m, tea.Batch(loadRecentNotes(m.s, habit.Name), loadHabitCheckins(m.s, habit.ID, habit.Name, 52))

	case "w":
		m.weekView = !m.weekView
		m.saveUIState()
		days := 30
		if m.weekView {
			days = 7
		}
		return m, loadHabits(m.s, days)

	case "v":
		m.compact = !m.compact
		m.saveUIState()

	case "V":
		if len(m.habits) == 0 {
			break
		}
		m.batchMode = true
		m.batchSelected = map[string]bool{m.habits[m.cursor].Habit.Name: true}
		return m, nil

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
		if !config.IsPro() {
			m.suggestText = "AI habit suggestions is a missionctl Bundle feature — see missionctl.sh/#pricing"
			m.suggestDone = true
			m.suggestBlocked = true
			return m, nil
		}
		m.suggestText = ""
		m.suggestDone = false
		m.suggestBlocked = false
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
		if !config.IsPro() {
			m.reviewText = "AI weekly review is a missionctl Bundle feature — see missionctl.sh/#pricing"
			m.reviewDone = true
			m.reviewBlocked = true
			return m, nil
		}
		m.reviewText = ""
		m.reviewDone = false
		m.reviewBlocked = false
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
		cal, err := m.s.GetCalendarData(52)
		if err == nil {
			m.calData = cal
		}
		if dates, err := m.s.GetCheckinDatesByHabit(52); err == nil {
			m.checkinDates = dates
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

	case "p":
		m.presetCursor = 0
		m.state = viewPresets

	case "y":
		if len(m.habits) == 0 {
			break
		}
		m.message = "Copied to clipboard"
		return m, tea.Batch(copyToClipboardCmd(m.habits[m.cursor].Habit.Name), clearAfter())

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
		return m.askConfirm(
			"Delete habit "+styleWarn.Render(name)+" and all its check-ins permanently?",
			func() tea.Msg {
				if err := s.DeleteHabit(name); err != nil {
					return errMsg{err}
				}
				return statusMsg("Deleted: " + name)
			})

	case "S":
		cfg, _ := config.Load()
		m.cfg = cfg
		config.ApplyToEnv(cfg, true)
		m.state = viewSettings
		m.settingsCursor = 0

	case "?":
		m = m.openHelp()
	}
	return m, nil
}

func (m model) handleAddInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

func (m model) handleAddDesc(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

func (m model) handleEditHabit(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

func (m model) handleGroupMgr(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
	case "u":
		if m.lastDeletedGroup != nil {
			g := m.lastDeletedGroup
			habitNames := m.lastDeletedGroupHabits
			m.lastDeletedGroup = nil
			m.lastDeletedGroupHabits = nil
			m.message = ""
			s := m.s
			return m, func() tea.Msg {
				newGroup, err := s.AddGroup(g.Name, g.Icon)
				if err != nil {
					return errMsg{err}
				}
				for _, name := range habitNames {
					_ = s.SetHabitGroup(name, newGroup.ID)
				}
				return statusMsg("Group restored: " + g.Name)
			}
		}
	case "d":
		if len(m.groups) == 0 {
			break
		}
		g := m.groups[m.groupCursor]
		s := m.s
		var habitNames []string
		for _, h := range m.habits {
			if h.Habit.GroupID == g.ID {
				habitNames = append(habitNames, h.Habit.Name)
			}
		}
		return m.askConfirm(
			"Delete group "+styleWarn.Render(g.Name)+"? Habits in it are kept (ungrouped).",
			func() tea.Msg {
				if err := s.DeleteGroup(g.ID); err != nil {
					return errMsg{err}
				}
				return groupDeletedMsg{group: g, habitNames: habitNames}
			})
	}
	return m, nil
}

func (m model) handleGroupNew(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

func (m model) handleGroupPick(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

func (m model) handleHabitDetail(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "enter":
		m.state = viewList
	case "space":
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
			if ms := models.StreakMilestone(stats.Streak); ms != "" {
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

func (m model) handleReview(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

func (m model) handleNoteInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

func (m model) handleChainMgr(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
	case "u":
		if m.lastDeletedChain != nil {
			ch := m.lastDeletedChain
			m.lastDeletedChain = nil
			m.message = ""
			s := m.s
			return m, func() tea.Msg {
				if err := s.AddChain(ch.FromName, ch.ToName); err != nil {
					return errMsg{err}
				}
				return statusMsg(fmt.Sprintf("Chain restored: %s → %s", ch.FromName, ch.ToName))
			}
		}
	case "d":
		if len(m.chains) == 0 {
			break
		}
		ch := m.chains[m.chainCursor]
		s := m.s
		return m.askConfirm(
			"Delete this habit chain?",
			func() tea.Msg {
				if err := s.DeleteChain(ch.ID); err != nil {
					return errMsg{err}
				}
				return chainDeletedMsg{chain: ch}
			})
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

func (m model) handleChainPick(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

// handleFilterInput drives the "/" habit filter (filters live while typing).
func (m model) handleFilterInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.state = viewList
		m.input.Blur()
		m.filterQ = ""
		m.habits = filterHabits(m.allHabits, "")
		m.cursor = 0
		return m, nil
	case "enter":
		m.state = viewList
		m.input.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.filterQ = strings.TrimSpace(m.input.Value())
	m.habits = filterHabits(m.allHabits, m.filterQ)
	m.cursor = 0
	return m, cmd
}

func (m model) handleCommandPalette(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	closeCmd := func(mm model) model {
		mm.state = viewList
		mm.input.Blur()
		mm.input.SetValue("")
		mm.cmdCursor = 0
		return mm
	}

	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m = closeCmd(m)
		return m, nil
	case "up", "ctrl+p":
		if m.cmdCursor > 0 {
			m.cmdCursor--
		}
		return m, nil
	case "down", "ctrl+n":
		matches := matchPaletteCommands(m.input.Value())
		if m.cmdCursor < len(matches)-1 {
			m.cmdCursor++
		}
		return m, nil
	case "enter":
		matches := matchPaletteCommands(m.input.Value())
		if len(matches) == 0 {
			m = closeCmd(m)
			return m, nil
		}
		if m.cmdCursor >= len(matches) {
			m.cmdCursor = len(matches) - 1
		}
		chosen := matches[m.cmdCursor]
		m = closeCmd(m)
		return m.handleList(tea.KeyPressMsg{Text: chosen.key, Code: []rune(chosen.key)[0]})
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.cmdCursor = 0
	return m, cmd
}

// filterHabits fuzzy-matches q against habit names (ranked best-match-first,
// like fzf/k9s), falling back to a plain substring match on the description
// for habits the name fuzzy-match missed — some people search by what a
// habit is for, not just its name.
func filterHabits(habits []models.HabitStats, q string) []models.HabitStats {
	q = strings.TrimSpace(q)
	if q == "" {
		return habits
	}

	names := make([]string, len(habits))
	for i, h := range habits {
		names[i] = h.Habit.Name
	}
	matches := fuzzy.Find(q, names)

	out := make([]models.HabitStats, 0, len(matches))
	matched := make(map[int]bool, len(matches))
	for _, mt := range matches {
		out = append(out, habits[mt.Index])
		matched[mt.Index] = true
	}

	ql := strings.ToLower(q)
	for i, h := range habits {
		if matched[i] {
			continue
		}
		if strings.Contains(strings.ToLower(h.Habit.Description), ql) {
			out = append(out, h)
		}
	}
	return out
}

// fuzzyMatchIndexes returns the rune indexes within s that q fuzzy-matched,
// or nil if q is empty or doesn't match at all.
func fuzzyMatchIndexes(q, s string) []int {
	if q == "" {
		return nil
	}
	matches := fuzzy.Find(q, []string{s})
	if len(matches) == 0 {
		return nil
	}
	return matches[0].MatchedIndexes
}

// highlightMatches renders s with the rune positions in idxs (from
// fuzzyMatchIndexes) styled via a warm, underlined variant of base, and
// every other character via base itself — fzf-style match highlighting.
//
// This renders one character at a time rather than nesting a highlighted
// span inside a single outer Render() call: lipgloss's Render() ends every
// string with a full SGR reset, so an inner Render() call's reset would
// wipe out the outer style for everything after the first highlighted
// character. Per-character rendering keeps every segment self-contained
// (verified: each carries its own open+reset), at the cost of more escape
// bytes — negligible for name-length strings in a TUI.
//
// idxs are indexes into s BEFORE any truncation — callers must resolve
// indexes against the same, untruncated string used to compute them.
func highlightMatches(s string, idxs []int, base lipgloss.Style) string {
	if len(idxs) == 0 {
		return base.Render(s)
	}
	hi := base.Foreground(colorWarn).Underline(true)
	matchSet := make(map[int]bool, len(idxs))
	for _, i := range idxs {
		matchSet[i] = true
	}
	var b strings.Builder
	for i, r := range []rune(s) {
		if matchSet[i] {
			b.WriteString(hi.Render(string(r)))
		} else {
			b.WriteString(base.Render(string(r)))
		}
	}
	return b.String()
}

// askConfirm switches to the confirmation prompt; action runs only on "y"/enter.
func (m model) askConfirm(prompt string, action tea.Cmd) (tea.Model, tea.Cmd) {
	m.confirmPrompt = prompt
	m.confirmAction = action
	m.confirmReturn = m.state
	m.state = viewConfirm
	return m, nil
}

func (m model) handleConfirm(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "y", "Y", "enter":
		action := m.confirmAction
		m.state = m.confirmReturn
		m.confirmPrompt = ""
		m.confirmAction = nil
		return m, action
	default:
		m.state = m.confirmReturn
		m.confirmPrompt = ""
		m.confirmAction = nil
		return m, nil
	}
}

func (m model) renderConfirm() string {
	var b strings.Builder
	b.WriteString(sectionHeader("Confirm") + "\n\n")
	b.WriteString(styleWarn.Render("Delete?") + "\n\n")
	b.WriteString(m.confirmPrompt + "\n\n")
	b.WriteString(styleMuted.Render("y confirm · enter confirm · esc cancel"))
	return m.panel(b.String())
}

func (m model) handleArchive(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
		return m.askConfirm(
			"Delete archived habit "+styleWarn.Render(name)+" and its history permanently?",
			func() tea.Msg {
				if err := s.DeleteHabit(name); err != nil {
					return errMsg{err}
				}
				return archiveReloadMsg{"Deleted: " + name}
			})
	}
	return m, nil
}

// habitPreset is one curated, ready-to-add habit template.
type habitPreset struct {
	Icon string
	Name string
	Desc string
}

// habitPresets is a small curated starter list — common habits people
// track, for a zero-wait alternative to the AI-powered "s" suggest flow.
var habitPresets = []habitPreset{
	{"💧", "Drink water", "8 glasses a day"},
	{"🏃", "Exercise", "Any movement counts"},
	{"📖", "Read", "Even just a few pages"},
	{"🧘", "Meditate", "5-10 minutes of stillness"},
	{"😴", "Sleep 8 hours", "Consistent bedtime"},
	{"📵", "No phone before bed", "Screen off an hour before sleep"},
	{"✍️", "Journal", "A few sentences about the day"},
	{"🥗", "Eat vegetables", "At least one serving"},
	{"🚶", "Walk 10k steps", "Track with your phone or watch"},
	{"🙏", "Gratitude practice", "Note one thing you're grateful for"},
	{"🧹", "Tidy up", "10 minutes of cleaning"},
	{"💰", "Track spending", "Log today's expenses"},
}

func (m model) handlePresets(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.state = viewList
	case "j", "down":
		if m.presetCursor < len(habitPresets)-1 {
			m.presetCursor++
		}
	case "k", "up":
		if m.presetCursor > 0 {
			m.presetCursor--
		}
	case "enter":
		p := habitPresets[m.presetCursor]
		s := m.s
		m.state = viewList
		return m, func() tea.Msg {
			if _, err := s.AddHabit(p.Name, p.Desc, p.Icon); err != nil {
				return errMsg{err}
			}
			return statusMsg("+ " + p.Name)
		}
	}
	return m, nil
}

func (m model) handleGoalInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

func (m model) handleHelp(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "?", "esc", "q":
		m.state = viewList
		return m, nil
	}
	var cmd tea.Cmd
	m.helpVP, cmd = m.helpVP.Update(msg)
	return m, cmd
}

func (m model) handleSuggest(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

	case "space":
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

func (m model) handleStats(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q":
		m.state = viewList
	}
	return m, nil
}

func (m model) handleSettings(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
			config.ApplyToEnv(m.cfg, true)
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

func (m model) handleGeminiMenu(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

func (m model) handleGeminiCID(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

func (m model) handleGeminiCS(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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
			config.ApplyToEnv(m.cfg, true)
			m.state = viewOAuthWait
			return m, startOAuth(m.cfg.GoogleClientID, m.cfg.GoogleClientSecret)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) handleKeyInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.state = viewSettings
		return m, nil
	case "o":
		p := providers[m.settingsCursor]
		if p.keyPage != "" {
			go auth.OpenBrowser(p.keyPage)
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
			config.ApplyToEnv(m.cfg, true)
			m.state = viewList
			m.message = p.label + " configured"
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}
