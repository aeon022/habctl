package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/aeon022/missionctl-core/tuitest"
)

// tour visits every view reachable from the list and leaves each with esc.
// Commands (AI calls, DB writes) returned by Update are never executed.
var tour = []string{
	"?", "esc", // help
	"n", "esc", // add: name
	"n", "R", "enter", "esc", // add: description
	"e", "esc", // edit
	"enter", "esc", // detail
	"g", "a", "esc", "esc", // groups, new group
	"G", "esc", // group pick
	"c", "esc", // chains
	"t", "esc", // stats
	"p", "esc", // presets
	"S", "esc", // settings
	"s", "esc", // AI suggestions
	"r", "esc", // review
	"N", "esc", // note (first habit is pre-checked in the test setup)
	"A", "esc", // archive
	"c", "a", "esc", "esc", // chain pick
	"S", "enter", "esc", "esc", // settings → key input
	"m", "esc",
	"/", "x", "esc", // filter
	":", "esc", // palette
	"V", "esc", // batch
	"d", "esc", // delete confirm
	"w", "w", "v", "v", "j", "k", "space", "space",
}

func TestSmokeTour(t *testing.T) {
	for _, size := range [][2]int{{100, 30}, {60, 15}} {
		m, st := flow(t, "Run", "Read", "Meditate")
		if err := st.CheckIn("Run", time.Now()); err != nil { // so "N" (note) is reachable
			t.Fatal(err)
		}
		m = reload(t, m)
		tuitest.SmokeSize(t, m, size[0], size[1], tour...)

		// the tour must end back on the list, not stuck in a sub-view
		end, _ := tuitest.Send(m, tuitest.Resize(size[0], size[1]))
		end, _ = tuitest.Keys(end, tour...)
		if got := end.(model).state; got != viewList {
			t.Errorf("%dx%d: tour ended in view %d, want list", size[0], size[1], got)
		}
	}
}

func TestSmokeEmptyData(t *testing.T) {
	m, _ := flow(t) // no habits at all
	tuitest.SmokeSize(t, m, 100, 30, "?", "esc", "n", "esc", "g", "esc", "c", "esc", "t", "esc", "S", "esc", "r", "esc", "enter", "space", "d", "esc", "V", "esc", "j", "k")
	tuitest.SmokeSize(t, m, 60, 15, "?", "esc", "t", "esc", "enter")
}

func TestListFooterNeverWraps(t *testing.T) {
	for _, w := range []int{60, 80, 100} {
		m, _ := flow(t, "Run")
		m.width, m.height = w, 30
		foot := lastNonEmptyLine(tuitest.Text(m))
		if lipgloss.Width(foot) > m.innerWidth() {
			t.Errorf("width %d: footer is %d cells wide, inner width is %d: %q", w, lipgloss.Width(foot), m.innerWidth(), foot)
		}
	}
}

func lastNonEmptyLine(s string) string {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.Trim(lines[i], " │╭╮╰╯─"); l != "" {
			return l
		}
	}
	return ""
}
