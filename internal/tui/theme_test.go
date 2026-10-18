package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/missionctl-core/theme"
)

func TestThemeKeyOpensPickerWithLivePreviewAndSaves(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	orig := theme.Current()
	t.Cleanup(func() { _ = theme.Apply(orig) })

	m := newTestModel()
	before := styleLime.GetForeground()
	mi, _ := m.handleList(tea.KeyPressMsg{Text: "T", Code: 'T'})
	m = mi.(model)
	if !m.themePicker.IsOpen() || !strings.Contains(m.View().Content, "Colour theme") {
		t.Fatal("T opens the theme popup")
	}
	mi, _ = m.Update(tea.KeyPressMsg{Text: "j", Code: 'j'})
	m = mi.(model)
	if theme.Current() == orig || styleLime.GetForeground() == before {
		t.Error("moving in the popup previews the theme live and rebuilds the cached styles")
	}
	mi, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = mi.(model)
	if m.themePicker.IsOpen() || m.message != "Theme: "+theme.Current() {
		t.Errorf("enter closes the popup and confirms the theme, got %q", m.message)
	}
}

func TestOnboardingChecklistListsFirstSteps(t *testing.T) {
	if c := onboardingChecklist(); !strings.Contains(c, "n   add your first habit") || !strings.Contains(c, "T   pick a colour theme") {
		t.Errorf("checklist missing steps:\n%s", c)
	}
}

func TestSettingsThemeKeyCyclesAndShowsTheme(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	orig := theme.Current()
	t.Cleanup(func() { _ = theme.Apply(orig) })

	m := newTestModel()
	m.state = viewSettings
	mi, _ := m.handleSettings(tea.KeyPressMsg{Text: "t", Code: 't'})
	if theme.Current() == orig {
		t.Fatalf("t in settings must switch the theme, still %q", orig)
	}
	if out := mi.(model).renderSettings(); !strings.Contains(out, theme.Current()) {
		t.Errorf("the settings screen should name the active theme:\n%s", out)
	}
}

func TestTallPanelScrollsInsteadOfBeingCutOff(t *testing.T) {
	m := newTestModel()
	m.width, m.height = 80, 12
	m.state = viewSettings
	var b strings.Builder
	b.WriteString("habctl · Test\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "line %02d\n", i)
	}
	b.WriteString("esc back\n")
	body := b.String()

	if out := m.panel(body); strings.Contains(out, "line 40") || !strings.Contains(out, "scroll") {
		t.Fatalf("the first page must hint at scrolling and not show the end:\n%s", out)
	}
	mi, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	m = mi.(model)
	m.panelOff = 1000 // far past the end: clamped to the last page
	if out := m.panel(body); !strings.Contains(out, "line 40") {
		t.Errorf("scrolled to the end, the last line must be visible:\n%s", out)
	}
	m.state = viewList
	mi, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if mi.(model).panelOff != 1000 {
		t.Error("the list view is not a scrolling panel and must not touch panelOff")
	}
}
