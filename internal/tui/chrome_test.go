package tui

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/aeon022/missionctl-core/tuitest"
)

// Every non-list view shares the list's chrome: a "habctl · …" header, a
// one-line footer with an esc hint, and constant height — never wider than the
// terminal.
var secondaryViews = []struct {
	name string
	keys []string
}{
	{"add", []string{"n"}},
	{"add desc", []string{"n", "R", "enter"}},
	{"edit", []string{"e"}},
	{"detail", []string{"enter"}},
	{"groups", []string{"g"}},
	{"group new", []string{"g", "a"}},
	{"group pick", []string{"G"}},
	{"chains", []string{"c"}},
	{"chain pick", []string{"c", "a"}},
	{"stats", []string{"t"}},
	{"presets", []string{"p"}},
	{"settings", []string{"S"}},
	{"key input", []string{"S", "enter"}},
	{"suggest", []string{"s"}},
	{"review", []string{"r"}},
	{"note", []string{"N"}},
	{"archive", []string{"A"}},
	{"delete confirm", []string{"d"}},
}

func TestSecondaryViewsShareTheListsChrome(t *testing.T) {
	for _, v := range secondaryViews {
		for _, w := range []int{40, 60, 80, 100, 140, 170} {
			for _, h := range []int{24, 36} {
				m, st := flow(t, "Run", "Read", "Meditate")
				if err := st.CheckIn("Run", time.Now()); err != nil {
					t.Fatal(err)
				}
				m = reload(t, m)
				mm, _ := tuitest.Send(m, tuitest.Resize(w, h))
				want := len(strings.Split(tuitest.Text(mm), "\n")) // the list's height
				mm, _ = tuitest.Keys(mm, v.keys...)
				lines := strings.Split(tuitest.Text(mm), "\n")
				if mm.(model).state == viewList {
					t.Errorf("%s: keys did not leave the list", v.name)
					continue
				}
				if len(lines) != want {
					t.Errorf("%s %dx%d: %d lines, want constant %d", v.name, w, h, len(lines), want)
				}
				for i, l := range lines {
					if lw := lipgloss.Width(l); lw > w {
						t.Errorf("%s %dx%d: line %d is %d wide: %q", v.name, w, h, i, lw, l)
					}
				}
				if !strings.Contains(lines[0], "habctl") {
					t.Errorf("%s %dx%d: header missing: %q", v.name, w, h, lines[0])
				}
				if last := lines[len(lines)-1]; !strings.Contains(last, "esc") {
					t.Errorf("%s %dx%d: footer must show an esc hint: %q", v.name, w, h, last)
				}
			}
		}
	}
}

func TestHelpIsATitledPanelOverADimmedList(t *testing.T) {
	m, _ := flow(t, "Run")
	mm, _ := tuitest.Send(m, tuitest.Resize(100, 30))
	mm, _ = tuitest.Keys(mm, "?")
	if !strings.Contains(tuitest.Text(mm), "╭─ Help") {
		t.Errorf("help carries its title in the border:\n%s", tuitest.Text(mm))
	}
	if !strings.Contains(mm.View().Content, "\x1b[2m") {
		t.Error("the list behind the help popup must be dimmed")
	}
}
