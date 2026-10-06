package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestSlotsEmptyPartialFull(t *testing.T) {
	for _, c := range []struct {
		done, total int
		want        string
	}{
		{0, 6, "○○○○○○"}, // an empty day is six open slots, not a gray block
		{2, 6, "●●○○○○"},
		{6, 6, "●●●●●●"},
		{0, 0, ""},       // no habits: nothing to draw
		{9, 6, "●●●●●●"}, // more done than total clamps
		{-1, 3, "○○○"},
	} {
		if got := ansi.Strip(slots(c.done, c.total)); got != c.want {
			t.Errorf("slots(%d,%d) = %q, want %q", c.done, c.total, got, c.want)
		}
	}
}

func TestSlotsFallBackToABarForManyHabits(t *testing.T) {
	got := slots(5, 30)
	if w := lipgloss.Width(got); w != 12 {
		t.Errorf("30 habits must use a 12-cell bar, got width %d: %q", w, ansi.Strip(got))
	}
	if strings.ContainsAny(ansi.Strip(got), "●○") {
		t.Errorf("the fallback is a ui.Bar, not slots: %q", ansi.Strip(got))
	}
}

func TestListHeaderShowsSlotsAtZeroAndPartial(t *testing.T) {
	m, st := flow(t, "Run", "Read", "Meditate")
	m.width, m.height = 100, 28
	head := ansi.Strip(m.listHeader())
	if !strings.Contains(head, "○○○ 0/3 today · 3 habits") {
		t.Errorf("0/3 header: %q", head)
	}
	if strings.Contains(head, "░") {
		t.Errorf("no gray-block track in the header: %q", head)
	}
	if err := st.CheckIn("Run", time.Now()); err != nil {
		t.Fatal(err)
	}
	m = reload(t, m)
	if head := ansi.Strip(m.listHeader()); !strings.Contains(head, "●○○ 1/3 today") {
		t.Errorf("1/3 header: %q", head)
	}
}
