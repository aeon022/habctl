package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/tuitest"
)

func sized(t *testing.T, w, h int, names ...string) model {
	t.Helper()
	m, _ := flow(t, names...)
	mm, _ := tuitest.Send(m, tuitest.Resize(w, h))
	return mm.(model)
}

func TestLayoutHeaderAndFooterFitEveryWidth(t *testing.T) {
	for _, w := range []int{60, 80, 100, 140} {
		m := sized(t, w, 28, "Run", "Read", "Meditate")
		lines := strings.Split(tuitest.Text(m), "\n")
		if got := len(lines); got != 28 {
			t.Errorf("width %d: frame is %d lines, want exactly the terminal height 28", w, got)
		}
		for i, l := range lines {
			if lipgloss.Width(l) > w {
				t.Errorf("width %d line %d is %d cells: %q", w, i, lipgloss.Width(l), l)
			}
		}
		if !strings.Contains(lines[0], "habctl") || !strings.Contains(lines[1], "──") {
			t.Errorf("width %d: header + rule expected on the first two lines:\n%s\n%s", w, lines[0], lines[1])
		}
		if foot := strings.TrimSpace(lines[len(lines)-1]); !strings.Contains(foot, "q quit") && w >= 80 {
			t.Errorf("width %d: footer should keep '?'/'q': %q", w, foot)
		}
	}
}

func TestNoOuterFrameAndPanelsOnlyWhenWide(t *testing.T) {
	narrow := tuitest.Text(sized(t, 110, 28, "Run", "Read"))
	if strings.ContainsAny(narrow, "╭╮╰╯│") {
		t.Errorf("below %d columns the list has no frame at all:\n%s", wideMin, narrow)
	}
	wide := tuitest.Text(sized(t, 140, 30, "Run", "Read"))
	for _, want := range []string{"╭─ Habits", "╭─ Detail", "Last 12 weeks", "Streak"} {
		if !strings.Contains(wide, want) {
			t.Errorf("wide layout missing %q:\n%s", want, wide)
		}
	}
	if strings.Contains(narrow, "Last 12 weeks") {
		t.Error("the detail panel must not appear below 120 columns")
	}
}

func TestSelectedRowCarriesAccentBar(t *testing.T) {
	m := sized(t, 100, 28, "Run", "Read")
	var marked []string
	for _, l := range strings.Split(tuitest.Text(m), "\n") {
		if strings.HasPrefix(l, "▌") {
			marked = append(marked, l)
		}
	}
	if len(marked) != 1 || !strings.Contains(marked[0], "Run") {
		t.Errorf("exactly the selected row (Run) carries ▌, got %q", marked)
	}
	mm, _ := tuitest.Keys(m, "j")
	var after []string
	for _, l := range strings.Split(tuitest.Text(mm), "\n") {
		if strings.HasPrefix(l, "▌") {
			after = append(after, l)
		}
	}
	if len(after) != 1 || !strings.Contains(after[0], "Read") {
		t.Errorf("after j the bar moves to Read, got %q", after)
	}
}

func TestMouseClickSelectsRowInNewGeometry(t *testing.T) {
	for _, w := range []int{100, 140} { // narrow list and wide panel layout
		m := sized(t, w, 30, "Run", "Read", "Meditate")
		// find the screen row of "Meditate" in the rendered text, click there
		y := -1
		for i, l := range strings.Split(tuitest.Text(m), "\n") {
			if strings.Contains(l, "Meditate") && !strings.Contains(l, "Detail") && i > 2 {
				y = i
				break
			}
		}
		if y < 0 {
			t.Fatalf("width %d: Meditate row not found", w)
		}
		if got := m.rowHitTest(y); got != 2 {
			t.Fatalf("width %d: rowHitTest(%d) = %d, want 2 (Meditate)", w, y, got)
		}
		mm, _ := tuitest.Send(m, tuitest.Click(8, y))
		if got := mm.(model).cursor; got != 2 {
			t.Errorf("width %d: click on the Meditate row selected %d, want 2", w, got)
		}
		if got := m.rowHitTest(0); got != -1 {
			t.Errorf("width %d: header click must hit nothing, got %d", w, got)
		}
	}
}

func TestEmptyListUsesEmptyState(t *testing.T) {
	m := sized(t, 100, 24)
	text := tuitest.Text(m)
	if !strings.Contains(text, "No habits yet") || !strings.Contains(text, "press n to add one") {
		t.Errorf("empty state missing:\n%s", text)
	}
	if strings.Contains(text, "habits ·") {
		t.Error("header must not show counts for an empty list")
	}
}

func TestBordersNoneDropsPanelFrameButKeepsTitles(t *testing.T) {
	t.Setenv("MISSIONCTL_BORDERS", "none")
	wide := tuitest.Text(sized(t, 140, 30, "Run"))
	if strings.ContainsAny(wide, "╭╮╰╯│") || !strings.Contains(wide, " Habits ") || !strings.Contains(wide, " Detail ") {
		t.Errorf("borders none: titles as dividers, no frame:\n%s", wide)
	}
}
