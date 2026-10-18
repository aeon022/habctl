package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/missionctl-core/theme"
)

func TestThemeKeySwitchesLiveAndSaves(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	orig := theme.Current()
	t.Cleanup(func() { _ = theme.Apply(orig) })

	m := newTestModel()
	before := styleLime.GetForeground()
	mi, _ := m.handleList(tea.KeyPressMsg{Text: "T", Code: 'T'})
	m = mi.(model)

	if theme.Current() == orig {
		t.Fatalf("T must switch to the next theme, still %q", orig)
	}
	if styleLime.GetForeground() == before {
		t.Error("cached styles must be rebuilt when the theme changes")
	}
	if m.message != "Theme: "+theme.Current() {
		t.Errorf("a short confirmation should name the theme, got %q", m.message)
	}
}

func TestOnboardingChecklistListsFirstSteps(t *testing.T) {
	if c := onboardingChecklist(); !strings.Contains(c, "n   add your first habit") || !strings.Contains(c, "T   pick a colour theme") {
		t.Errorf("checklist missing steps:\n%s", c)
	}
}
