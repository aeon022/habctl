package tui

import (
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/habctl/internal/store"
)

// In Bubble Tea v2 a space press stringifies as "space", not " " — every
// `case " ":` written for v1 silently stopped matching. This drives the real
// key event through handleList and checks the check-in actually happens.
func TestSpaceKeyTogglesCheckIn(t *testing.T) {
	space := tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	if space.String() != "space" {
		t.Fatalf("v2 space key String() = %q — handlers match on \"space\"", space.String())
	}

	s, err := store.Open(filepath.Join(t.TempDir(), "habits.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.AddHabit("Sport", "", ""); err != nil {
		t.Fatal(err)
	}

	reload := func() model {
		m := newTestModel()
		m.s = s
		stats, err := s.GetAllStats(30)
		if err != nil {
			t.Fatal(err)
		}
		m.habits = stats
		return m
	}

	m := reload()
	_, cmd := m.Update(space)
	if cmd == nil {
		t.Fatal("space on a habit row returned no command — key not handled")
	}
	if msg, ok := cmd().(statusMsg); !ok || !strings.HasPrefix(string(msg), "✓ Sport") {
		t.Fatalf("first space result = %#v, want \"✓ Sport…\" status", msg)
	}
	if st, _ := s.GetStats("Sport", 30); !st.CheckedToday {
		t.Fatal("space did not record today's check-in")
	}

	m = reload() // second press undoes it
	_, cmd = m.Update(space)
	if cmd == nil {
		t.Fatal("second space returned no command")
	}
	if msg, _ := cmd().(statusMsg); !strings.Contains(string(msg), "unchecked") {
		t.Errorf("second space result = %q, want unchecked", msg)
	}
	if st, _ := s.GetStats("Sport", 30); st.CheckedToday {
		t.Error("second space did not remove the check-in")
	}
}

func TestSpaceKeyWithNoHabitsDoesNothing(t *testing.T) {
	m := newTestModel()
	if _, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}); cmd != nil {
		t.Error("space with an empty list must be a no-op")
	}
}
