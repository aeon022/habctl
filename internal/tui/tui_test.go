package tui

import (
	"strings"
	"testing"

	"github.com/aeon022/habctl/internal/models"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func newTestModel() model {
	return model{width: 100, height: 30, input: textinput.New()}
}

func TestMatchPaletteCommands_EmptyQueryReturnsAll(t *testing.T) {
	got := matchPaletteCommands("")
	if len(got) != len(paletteCommands) {
		t.Errorf("empty query = %d matches, want all %d", len(got), len(paletteCommands))
	}
}

func TestMatchPaletteCommands_PrefixRankedBeforeSubstring(t *testing.T) {
	// "s" is a prefix of "suggest"/"settings" and a substring of "chains".
	got := matchPaletteCommands("s")
	if len(got) == 0 {
		t.Fatal("expected matches for \"s\"")
	}
	if got[0].name[0] != 's' {
		t.Errorf("first match %q should be a prefix match (start with 's')", got[0].name)
	}
	// "chains" contains "s" but isn't a prefix match — must rank after prefix matches.
	sIdx, chainsIdx := -1, -1
	for i, c := range got {
		if c.name == "settings" {
			sIdx = i
		}
		if c.name == "chains" {
			chainsIdx = i
		}
	}
	if sIdx == -1 || chainsIdx == -1 {
		t.Fatal("expected both \"settings\" and \"chains\" in matches for \"s\"")
	}
	if sIdx > chainsIdx {
		t.Errorf("prefix match \"settings\" (idx %d) should rank before substring match \"chains\" (idx %d)", sIdx, chainsIdx)
	}
}

func TestMatchPaletteCommands_NoMatch(t *testing.T) {
	if got := matchPaletteCommands("zzz-nope"); len(got) != 0 {
		t.Errorf("expected no matches, got %d", len(got))
	}
}

func TestHandleCommandPalette_OpenFilterEsc(t *testing.T) {
	m := newTestModel()

	mi, _ := m.handleList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = mi.(model)
	if m.state != viewCommand {
		t.Fatalf("expected viewCommand after ':', got %v", m.state)
	}

	for _, r := range "arch" {
		mi, _ = m.handleCommandPalette(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mi.(model)
	}
	matches := matchPaletteCommands(m.input.Value())
	if len(matches) == 0 || matches[0].name != "archive" {
		t.Fatalf("expected \"archive\" to be the top match for \"arch\", got %+v", matches)
	}

	mi, _ = m.handleCommandPalette(tea.KeyMsg{Type: tea.KeyEsc})
	m = mi.(model)
	if m.state != viewList {
		t.Errorf("expected esc to cancel back to viewList, got %v", m.state)
	}
	if m.input.Value() != "" {
		t.Errorf("expected input cleared after esc, got %q", m.input.Value())
	}
}

func TestHandleCommandPalette_EnterDispatchesMappedKey(t *testing.T) {
	// "help" maps to the same "?" keypress handleList already handles, and
	// doesn't touch the (nil, in this test) store — safe to exercise fully.
	m := newTestModel()
	mi, _ := m.handleList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	m = mi.(model)
	for _, r := range "help" {
		mi, _ = m.handleCommandPalette(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mi.(model)
	}
	mi, _ = m.handleCommandPalette(tea.KeyMsg{Type: tea.KeyEnter})
	m = mi.(model)
	if m.state != viewHelp {
		t.Errorf("expected \"help\" command to dispatch to viewHelp, got %v", m.state)
	}
}

func TestHandleCommandPalette_ArrowNavigationStaysInBounds(t *testing.T) {
	m := newTestModel()
	m.state = viewCommand
	m.input.SetValue("s") // matches several commands
	matches := matchPaletteCommands(m.input.Value())
	if len(matches) < 2 {
		t.Fatal("test assumes at least 2 matches for \"s\"")
	}

	mi, _ := m.handleCommandPalette(tea.KeyMsg{Type: tea.KeyUp})
	m = mi.(model)
	if m.cmdCursor != 0 {
		t.Errorf("up at cursor 0 should stay at 0, got %d", m.cmdCursor)
	}

	for i := 0; i < len(matches)+3; i++ {
		mi, _ = m.handleCommandPalette(tea.KeyMsg{Type: tea.KeyDown})
		m = mi.(model)
	}
	if m.cmdCursor != len(matches)-1 {
		t.Errorf("down past the end should clamp at %d, got %d", len(matches)-1, m.cmdCursor)
	}
}

func TestFilterHabits_FuzzyMatchesName(t *testing.T) {
	habits := []models.HabitStats{
		{Habit: models.Habit{Name: "Morning Run"}},
		{Habit: models.Habit{Name: "Reading"}},
	}
	got := filterHabits(habits, "run")
	if len(got) != 1 || got[0].Habit.Name != "Morning Run" {
		t.Errorf("expected only \"Morning Run\" to match \"run\", got %+v", got)
	}
}

func TestFilterHabits_FallsBackToDescriptionSubstring(t *testing.T) {
	habits := []models.HabitStats{
		{Habit: models.Habit{Name: "Meditation", Description: "calm and focus"}},
		{Habit: models.Habit{Name: "Reading"}},
	}
	got := filterHabits(habits, "focus")
	if len(got) != 1 || got[0].Habit.Name != "Meditation" {
		t.Errorf("expected \"Meditation\" to match via description fallback, got %+v", got)
	}
}

func TestFilterHabits_EmptyQueryReturnsAll(t *testing.T) {
	habits := []models.HabitStats{{Habit: models.Habit{Name: "A"}}, {Habit: models.Habit{Name: "B"}}}
	if got := filterHabits(habits, ""); len(got) != 2 {
		t.Errorf("empty query should return all habits, got %d", len(got))
	}
}

func TestFuzzyMatchIndexes(t *testing.T) {
	idx := fuzzyMatchIndexes("run", "Morning Run")
	if len(idx) != 3 {
		t.Fatalf("expected 3 matched indexes for \"run\" in \"Morning Run\", got %v", idx)
	}
	if idx := fuzzyMatchIndexes("", "anything"); idx != nil {
		t.Errorf("empty query should return nil indexes, got %v", idx)
	}
	if idx := fuzzyMatchIndexes("zzz", "Morning Run"); idx != nil {
		t.Errorf("non-matching query should return nil indexes, got %v", idx)
	}
}

func TestHighlightMatches_PreservesBaseStyleAfterHighlight(t *testing.T) {
	// Regression test: nesting a highlighted lipgloss.Render() call inside
	// another Render() call silently drops the outer style after the first
	// highlighted character, because every Render() call ends with a full
	// SGR reset. highlightMatches must render per-character instead so the
	// base style survives past a highlighted run.
	lipgloss.SetColorProfile(termenv.ANSI256)
	base := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("189"))

	out := highlightMatches("abRuncd", []int{2, 3, 4}, base)

	// The last character ('d') must still carry the base style's bold SGR
	// attribute (1) — if the outer style got wiped by a nested reset, its
	// segment would render as plain, unstyled text (just "\x1b[0md\x1b[0m").
	dPos := strings.LastIndex(out, "d")
	if dPos == -1 {
		t.Fatalf("expected rendered output to still contain the trailing character: %q", out)
	}
	openCode := out[strings.LastIndex(out[:dPos], "\x1b["):dPos]
	if !strings.Contains(openCode, "1") {
		t.Errorf("expected base style (bold) to survive after the highlighted run, 'd' opening code = %q", openCode)
	}
}

// Overlay-compositing correctness (border-ring collision, background
// padding, oversized-popup clamping) is tested in
// missionctl-core/overlay — this package only wires openHelp/
// renderHelpPopup into that shared primitive.
