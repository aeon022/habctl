package tui

import (
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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
