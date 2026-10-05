package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func focus(t *testing.T, m model) (model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(tea.FocusMsg{})
	return next.(model), cmd
}

func TestFocusReloadsWhenStaleAndBrowsing(t *testing.T) {
	m, _ := flow(t, "A")
	m.lastLoad = time.Now().Add(-time.Minute)
	m, cmd := focus(t, m)
	if cmd == nil || !m.reloading {
		t.Fatalf("stale list while browsing must reload: cmd=%v reloading=%v", cmd != nil, m.reloading)
	}
	// a second focus while that reload is in flight must not start another
	if _, cmd = focus(t, m); cmd != nil {
		t.Error("focus during an in-flight reload must be a no-op")
	}
}

func TestFocusDoesNotReloadWhenFreshOrBusy(t *testing.T) {
	cases := map[string]func(m *model){
		"fresh data":      func(m *model) { m.lastLoad = time.Now() },
		"add input":       func(m *model) { m.state = viewAddInput },
		"filter input":    func(m *model) { m.state = viewFilterInput },
		"palette":         func(m *model) { m.state = viewCommand },
		"note input":      func(m *model) { m.state = viewNoteInput },
		"batch mode":      func(m *model) { m.batchMode = true },
		"confirm pending": func(m *model) { m.confirmPrompt = "Delete?" },
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			m, _ := flow(t, "A")
			m.lastLoad = time.Now().Add(-time.Minute) // stale unless the case says otherwise
			set(&m)
			if _, cmd := focus(t, m); cmd != nil {
				t.Errorf("%s: focus must not reload", name)
			}
		})
	}
}

func TestLoadedMsgMarksFreshAndClearsReloading(t *testing.T) {
	m, _ := flow(t, "A")
	m.reloading = true
	next, _ := m.Update(habitsLoadedMsg(m.allHabits))
	m = next.(model)
	if m.reloading || time.Since(m.lastLoad) > time.Second {
		t.Errorf("after load: reloading=%v lastLoad=%v", m.reloading, m.lastLoad)
	}
}

func TestViewReportsFocus(t *testing.T) {
	m, _ := flow(t, "A")
	if !m.View().ReportFocus {
		t.Error("View must set ReportFocus or FocusMsg never arrives")
	}
}

func TestCopyIsBatchOfOSC52AndPbcopy(t *testing.T) {
	cmd := copyToClipboardCmd("Sport")
	if cmd == nil {
		t.Fatal("nil cmd")
	}
	// tea.Batch's cmd only wraps the children into a BatchMsg; running it
	// does not run pbcopy.
	b, ok := cmd().(tea.BatchMsg)
	if !ok || len(b) != 2 {
		t.Errorf("want a 2-command batch (SetClipboard + pbcopy), got %T len=%d", cmd(), len(b))
	}
}
