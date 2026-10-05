package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/habctl/internal/models"
	"github.com/aeon022/habctl/internal/store"
)

// k builds the v2 key press the terminal would deliver for a key name.
func k(name string) tea.KeyPressMsg {
	switch name {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	}
	return tea.KeyPressMsg{Text: name, Code: []rune(name)[0]}
}

// press feeds keys through Update one by one; the model after the last key
// and the command it returned (nil if none) come back.
func press(t *testing.T, m model, keys ...string) (model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, key := range keys {
		next, c := m.Update(k(key))
		mm, ok := next.(model)
		if !ok {
			t.Fatalf("Update returned %T", next)
		}
		m, cmd = mm, c
	}
	return m, cmd
}

// flow returns a model wired to a throwaway store holding the given habits.
// HOME is isolated so UI-state / config writes never touch the real ones.
func flow(t *testing.T, names ...string) (model, *store.Store) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HABCTL_DATA_DIR", "")
	s, err := store.Open(filepath.Join(t.TempDir(), "habits.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	for _, n := range names {
		if _, err := s.AddHabit(n, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	m := newTestModel()
	m.s = s
	return reload(t, m), s
}

func reload(t *testing.T, m model) model {
	t.Helper()
	stats, err := m.s.GetAllStats(30)
	if err != nil {
		t.Fatal(err)
	}
	m.allHabits, m.habits = stats, stats
	return m
}

// run executes a command and returns the message it produces.
func run(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command, got nil")
	}
	return cmd()
}

func wantStatus(t *testing.T, msg tea.Msg, prefix string) {
	t.Helper()
	if e, ok := msg.(errMsg); ok {
		t.Fatalf("command failed: %v", e.err)
	}
	if s, ok := msg.(statusMsg); !ok || !strings.HasPrefix(string(s), prefix) {
		t.Fatalf("message = %#v, want status starting %q", msg, prefix)
	}
}

func habitByName(t *testing.T, s *store.Store, name string) (models.Habit, bool) {
	t.Helper()
	hs, err := s.ListHabits()
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hs {
		if h.Name == name {
			return h, true
		}
	}
	return models.Habit{}, false
}

func TestListCursorMovementClamps(t *testing.T) {
	m, _ := flow(t, "A", "B", "C")
	m, _ = press(t, m, "j", "j", "j", "j")
	if m.cursor != 2 {
		t.Errorf("cursor after 4×j = %d, want 2 (clamped to last)", m.cursor)
	}
	m, _ = press(t, m, "down")
	if m.cursor != 2 {
		t.Errorf("down past end moved cursor to %d", m.cursor)
	}
	m, _ = press(t, m, "k", "up", "k", "k")
	if m.cursor != 0 {
		t.Errorf("cursor after moving up = %d, want 0 (clamped)", m.cursor)
	}
	m, _ = press(t, m, "3")
	if m.cursor != 2 {
		t.Errorf("'3' should jump to the 3rd habit, cursor = %d", m.cursor)
	}
}

func TestAddHabitFlowCreatesHabitWithIcon(t *testing.T) {
	m, s := flow(t)
	m, _ = press(t, m, "n")
	if m.state != viewAddInput {
		t.Fatalf("state after n = %v, want viewAddInput", m.state)
	}
	if m, _ = press(t, m, "enter"); m.state != viewAddInput {
		t.Fatal("enter on an empty name must not advance")
	}
	m.input.SetValue("🏃 Run")
	m, _ = press(t, m, "enter")
	if m.state != viewAddDesc || m.addingName != "Run" || m.addingIcon != "🏃" {
		t.Fatalf("after name: state=%v name=%q icon=%q", m.state, m.addingName, m.addingIcon)
	}
	m.input.SetValue("5 km")
	m, cmd := press(t, m, "enter")
	if m.state != viewList {
		t.Errorf("state after finishing add = %v, want viewList", m.state)
	}
	wantStatus(t, run(t, cmd), "+ Run")
	h, ok := habitByName(t, s, "Run")
	if !ok || h.Icon != "🏃" || h.Description != "5 km" {
		t.Errorf("stored habit = %+v (found=%v)", h, ok)
	}
}

func TestAddHabitEscCancelsWithoutCreating(t *testing.T) {
	m, s := flow(t)
	m, _ = press(t, m, "n")
	m.input.SetValue("Nope")
	m, _ = press(t, m, "esc")
	if m.state != viewList {
		t.Errorf("state = %v, want viewList", m.state)
	}
	if hs, _ := s.ListHabits(); len(hs) != 0 {
		t.Errorf("esc created %d habits", len(hs))
	}
}

func TestEditHabitRenamesAndSetsWeeklyTarget(t *testing.T) {
	m, s := flow(t, "Read")
	m, _ = press(t, m, "e")
	if m.state != viewEditHabit || m.editOldName != "Read" {
		t.Fatalf("state=%v old=%q", m.state, m.editOldName)
	}
	m.input.SetValue("📚 Study")
	m, _ = press(t, m, "tab", "tab") // name → description → frequency field
	if m.editCursor != 2 || m.editNameBuf != "📚 Study" {
		t.Fatalf("editCursor=%d nameBuf=%q after two tabs", m.editCursor, m.editNameBuf)
	}
	m, _ = press(t, m, "+", "+", "-")
	if m.editFreq != 1 {
		t.Errorf("editFreq after ++- = %d, want 1", m.editFreq)
	}
	m, cmd := press(t, m, "enter")
	wantStatus(t, run(t, cmd), "✓ Study")
	if _, ok := habitByName(t, s, "Read"); ok {
		t.Error("old name still exists after rename")
	}
	h, ok := habitByName(t, s, "Study")
	if !ok || h.Icon != "📚" || h.FreqTarget != 1 {
		t.Errorf("renamed habit = %+v (found=%v)", h, ok)
	}
}

func TestEditHabitEscDiscards(t *testing.T) {
	m, s := flow(t, "Read")
	m, _ = press(t, m, "e")
	m.input.SetValue("Changed")
	m, cmd := press(t, m, "esc")
	if m.state != viewList || cmd != nil {
		t.Errorf("esc: state=%v cmd=%v", m.state, cmd != nil)
	}
	if _, ok := habitByName(t, s, "Read"); !ok {
		t.Error("esc must not rename")
	}
}

func TestArchiveAndRestore(t *testing.T) {
	m, s := flow(t, "Yoga", "Sleep")
	_, cmd := press(t, m, "a") // archive the habit under the cursor
	wantStatus(t, run(t, cmd), "Archived: ")
	if hs, _ := s.ListHabits(); len(hs) != 1 {
		t.Fatalf("active habits after archive = %d, want 1", len(hs))
	}

	m, cmd = press(t, m, "A")
	if m.state != viewArchive {
		t.Fatalf("state = %v, want viewArchive", m.state)
	}
	next, _ := m.Update(run(t, cmd)) // the archive list arrives
	m = next.(model)
	if len(m.archivedHabits) != 1 {
		t.Fatalf("archived list = %d, want 1", len(m.archivedHabits))
	}
	_, cmd = press(t, m, "r")
	if msg, ok := run(t, cmd).(archiveReloadMsg); !ok || !strings.Contains(msg.status, "restored") {
		t.Fatalf("restore message = %#v", msg)
	}
	if hs, _ := s.ListHabits(); len(hs) != 2 {
		t.Errorf("active habits after restore = %d, want 2", len(hs))
	}
}

func TestDeleteNeedsConfirmation(t *testing.T) {
	m, s := flow(t, "Doomed")

	m, cmd := press(t, m, "d")
	if m.state != viewConfirm || cmd != nil {
		t.Fatalf("d must open the confirm prompt without acting: state=%v", m.state)
	}
	m, cmd = press(t, m, "n") // anything but y/enter cancels
	if m.state != viewList || cmd != nil {
		t.Errorf("cancel: state=%v cmd=%v", m.state, cmd != nil)
	}
	if _, ok := habitByName(t, s, "Doomed"); !ok {
		t.Fatal("habit vanished although the prompt was cancelled")
	}

	m, _ = press(t, m, "d")
	m, cmd = press(t, m, "y")
	wantStatus(t, run(t, cmd), "Deleted: Doomed")
	if _, ok := habitByName(t, s, "Doomed"); ok {
		t.Error("habit still exists after confirmed delete")
	}
	if m.state != viewList {
		t.Errorf("state after confirm = %v", m.state)
	}
}

func TestNoteOnlyAfterCheckIn(t *testing.T) {
	m, s := flow(t, "Run")
	if m, _ = press(t, m, "N"); m.state != viewList {
		t.Fatal("N before a check-in must be ignored")
	}
	if err := s.CheckIn("Run", time.Now()); err != nil {
		t.Fatal(err)
	}
	m = reload(t, m)
	m, _ = press(t, m, "N")
	if m.state != viewNoteInput || m.noteForHabit != "Run" {
		t.Errorf("state=%v noteFor=%q, want viewNoteInput/Run", m.state, m.noteForHabit)
	}
}

func TestFilterNarrowsAndEscRestores(t *testing.T) {
	m, _ := flow(t, "Run", "Read", "Sleep")
	m, _ = press(t, m, "/")
	if m.state != viewFilterInput {
		t.Fatalf("state = %v", m.state)
	}
	m, _ = press(t, m, "s", "l")
	if m.filterQ != "sl" || len(m.habits) != 1 || m.habits[0].Habit.Name != "Sleep" {
		t.Fatalf("filter %q → %d habits (first %v)", m.filterQ, len(m.habits), m.habits)
	}
	m, _ = press(t, m, "enter")
	if m.state != viewList || len(m.habits) != 1 {
		t.Errorf("enter keeps the filter: state=%v habits=%d", m.state, len(m.habits))
	}
	m, _ = press(t, m, "esc") // esc in the list clears an active filter
	if m.filterQ != "" || len(m.habits) != 3 {
		t.Errorf("esc: filterQ=%q habits=%d, want cleared/3", m.filterQ, len(m.habits))
	}
}

func TestFilterEscInInputRestoresAll(t *testing.T) {
	m, _ := flow(t, "Run", "Read")
	m, _ = press(t, m, "/", "r", "u")
	m, _ = press(t, m, "esc")
	if m.state != viewList || m.filterQ != "" || len(m.habits) != 2 {
		t.Errorf("state=%v filterQ=%q habits=%d", m.state, m.filterQ, len(m.habits))
	}
}

func TestGroupCreateAndAssign(t *testing.T) {
	m, s := flow(t, "Run")
	m, _ = press(t, m, "G")
	if m.state != viewGroupMgr {
		t.Fatalf("state = %v, want viewGroupMgr", m.state)
	}
	m, _ = press(t, m, "a")
	if m.state != viewGroupNew {
		t.Fatalf("state = %v, want viewGroupNew", m.state)
	}
	m.input.SetValue("🌅 Morning")
	m, cmd := press(t, m, "enter")
	wantStatus(t, run(t, cmd), "Group created: Morning")
	if m.state != viewGroupMgr {
		t.Errorf("state after create = %v, want back in group manager", m.state)
	}
	groups, err := s.ListGroups()
	if err != nil || len(groups) != 1 || groups[0].Icon != "🌅" {
		t.Fatalf("groups = %+v, err %v", groups, err)
	}

	m.groups = groups
	m, _ = press(t, m, "esc", "m") // pick a group for the habit under the cursor
	if m.state != viewGroupPick {
		t.Fatalf("state = %v, want viewGroupPick", m.state)
	}
	m, cmd = press(t, m, "j", "enter") // index 0 is "no group", 1 the new one
	wantStatus(t, run(t, cmd), "✓ Group assigned")
	h, _ := habitByName(t, s, "Run")
	if h.GroupID != groups[0].ID {
		t.Errorf("habit GroupID = %d, want %d", h.GroupID, groups[0].ID)
	}
}

func TestChainNeedsTwoHabits(t *testing.T) {
	m, _ := flow(t, "Only")
	m, _ = press(t, m, "c")
	if m.state != viewChainMgr {
		t.Fatalf("state = %v", m.state)
	}
	if m, _ = press(t, m, "a"); m.state != viewChainMgr {
		t.Error("a chain needs two habits — adding with one must stay in the manager")
	}

	m2, _ := flow(t, "One", "Two")
	m2, _ = press(t, m2, "c", "a")
	if m2.state != viewChainPick {
		t.Errorf("with two habits a opens the picker, state = %v", m2.state)
	}
	if m2, _ = press(t, m2, "esc"); m2.state == viewChainPick {
		t.Error("esc must leave the chain picker")
	}
}

func TestStatsAndHelpViewsOpenAndClose(t *testing.T) {
	m, _ := flow(t, "Run")
	m, _ = press(t, m, "t")
	if m.state != viewStats {
		t.Fatalf("t → %v, want viewStats", m.state)
	}
	if m, _ = press(t, m, "esc"); m.state != viewList {
		t.Errorf("esc leaves stats: state = %v", m.state)
	}
	m, _ = press(t, m, "?")
	if m.state != viewHelp {
		t.Fatalf("? → %v, want viewHelp", m.state)
	}
	if m, _ = press(t, m, "?"); m.state != viewList {
		t.Errorf("? again closes help: state = %v", m.state)
	}
}

func TestToggleWindowAndCompact(t *testing.T) {
	m, _ := flow(t, "Run")
	m, cmd := press(t, m, "w")
	if !m.weekView || cmd == nil {
		t.Errorf("w: weekView=%v reload cmd=%v", m.weekView, cmd != nil)
	}
	m, _ = press(t, m, "v")
	if !m.compact {
		t.Error("v should switch to compact")
	}
	m, _ = press(t, m, "v")
	if m.compact {
		t.Error("v again should switch back")
	}
}

func TestBatchModeSelectAndExit(t *testing.T) {
	m, _ := flow(t, "A", "B")
	m, _ = press(t, m, "V")
	if !m.batchMode || !m.batchSelected["A"] {
		t.Fatalf("V: batch=%v selected=%v, want A preselected", m.batchMode, m.batchSelected)
	}
	m, _ = press(t, m, "j", "space")
	if !m.batchSelected["B"] {
		t.Errorf("space in batch mode should select the row under the cursor: %v", m.batchSelected)
	}
	m, _ = press(t, m, "esc")
	if m.batchMode || len(m.batchSelected) != 0 {
		t.Errorf("esc: batch=%v selected=%v", m.batchMode, m.batchSelected)
	}
}

// The palette replays the chosen command's hotkey into the list handler, so
// each entry must land on the view its name promises.
func TestPaletteRunsCommands(t *testing.T) {
	cases := map[string]viewState{
		"stats":    viewStats,
		"help":     viewHelp,
		"new":      viewAddInput,
		"groups":   viewGroupMgr,
		"chains":   viewChainMgr,
		"archived": viewArchive,
		"edit":     viewEditHabit,
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			m, _ := flow(t, "Run")
			m, _ = press(t, m, ":")
			if m.state != viewCommand {
				t.Fatalf("':' → %v, want viewCommand", m.state)
			}
			m.input.SetValue(name)
			m, _ = press(t, m, "enter")
			if m.state != want {
				t.Errorf(":%s → state %v, want %v", name, m.state, want)
			}
		})
	}
}

func TestPaletteEscClosesWithoutAction(t *testing.T) {
	m, _ := flow(t, "Run")
	m, _ = press(t, m, ":")
	m.input.SetValue("delete")
	m, cmd := press(t, m, "esc")
	if m.state != viewList || cmd != nil || m.input.Value() != "" {
		t.Errorf("esc: state=%v cmd=%v input=%q", m.state, cmd != nil, m.input.Value())
	}
}

func TestEnterOpensDetailAndEscReturns(t *testing.T) {
	m, _ := flow(t, "Run")
	m, cmd := press(t, m, "enter")
	if m.state != viewHabitDetail || cmd == nil {
		t.Fatalf("enter: state=%v cmd=%v", m.state, cmd != nil)
	}
	if m, _ = press(t, m, "esc"); m.state != viewList {
		t.Errorf("esc: state = %v", m.state)
	}
}
