package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/habctl/internal/models"
	"github.com/aeon022/habctl/internal/store"
)

// ── commands ──────────────────────────────────────────────────────────────────

func loadHabits(s *store.Store, days int) tea.Cmd {
	return func() tea.Msg {
		stats, err := s.GetAllStats(days)
		if err != nil {
			return errMsg{err}
		}
		return habitsLoadedMsg(stats)
	}
}

func loadGroups(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		gs, err := s.ListGroups()
		if err != nil {
			return errMsg{err}
		}
		return groupsLoadedMsg(gs)
	}
}

func loadChains(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		cs, err := s.ListChains()
		if err != nil {
			return errMsg{err}
		}
		return chainsLoadedMsg(cs)
	}
}

func loadRecentNotes(s *store.Store, name string) tea.Cmd {
	return func() tea.Msg {
		notes, _ := s.GetRecentNotes(name, 5)
		return notesLoadedMsg{name: name, notes: notes}
	}
}

// loadHabitCheckins fetches this one habit's check-in history for the
// detail view's multi-week heatmap. GetCheckinDatesByHabit returns every
// tracked habit's dates in one query (it's built for cross-habit
// correlations, see topHabitCorrelations) — cheap enough to reuse here
// rather than adding a single-habit query just for this.
func loadHabitCheckins(s *store.Store, habitID int64, name string, weeks int) tea.Cmd {
	return func() tea.Msg {
		byHabit, err := s.GetCheckinDatesByHabit(weeks)
		if err != nil {
			return habitCheckinsLoadedMsg{name: name, dates: map[string]bool{}}
		}
		return habitCheckinsLoadedMsg{name: name, dates: byHabit[habitID]}
	}
}

func loadArchivedHabits(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		habits, err := s.ListArchivedHabits()
		if err != nil {
			return errMsg{err}
		}
		return archivedLoadedMsg(habits)
	}
}

func startBlink() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(_ time.Time) tea.Msg {
		return blinkMsg{}
	})
}

func isHabitDoneToday(habits []models.HabitStats, name string) bool {
	if name == "" {
		return false
	}
	for _, h := range habits {
		if h.Habit.Name == name {
			return h.CheckedToday
		}
	}
	return false
}

func waitForReviewChunk(ch <-chan reviewChunkResult) tea.Cmd {
	return func() tea.Msg {
		r := <-ch
		switch {
		case r.err != nil:
			return reviewErrMsg{r.err, r.gen}
		case r.done:
			return reviewDoneMsg{r.gen}
		default:
			return reviewChunkMsg{r.text, r.gen}
		}
	}
}

func waitForChunk(ch <-chan suggestChunkResult) tea.Cmd {
	return func() tea.Msg {
		r := <-ch
		if r.err != nil {
			return suggestErrMsg{r.err, r.gen}
		}
		if r.done {
			return suggestDoneMsg{r.gen}
		}
		return suggestChunkMsg{r.text, r.gen}
	}
}

// toggleHabitCheckinCmd checks or unchecks habits[idx] for today — shared by
// the "space" key (acts on the cursor row) and right-click (acts on
// whatever row was clicked, per taskctl's "quick-action shouldn't require
// selecting first" convention).
func toggleHabitCheckinCmd(s *store.Store, habits []models.HabitStats, idx int) tea.Cmd {
	if idx < 0 || idx >= len(habits) {
		return nil
	}
	h := habits[idx]
	name := h.Habit.Name
	chainTo := h.ChainTo
	chainToDone := isHabitDoneToday(habits, chainTo)
	if h.CheckedToday {
		return func() tea.Msg {
			if err := s.DeleteCheckIn(name, time.Now()); err != nil {
				return errMsg{err}
			}
			return statusMsg("✗ " + name + " unchecked")
		}
	}
	return func() tea.Msg {
		if err := s.CheckIn(name, time.Now()); err != nil {
			return errMsg{err}
		}
		stats, err := s.GetStats(name, 30)
		if err != nil {
			return statusMsg(fmt.Sprintf("✓ %s", name))
		}
		out := fmt.Sprintf("✓ %s (streak: %d)", name, stats.Streak)
		if stats.Streak >= 7 {
			out += " 🔥"
		}
		if ms := models.StreakMilestone(stats.Streak); ms != "" {
			out += "  " + ms
		}
		if chainTo != "" && !chainToDone {
			out += "  →  " + chainTo + "?"
		}
		return statusMsg(out)
	}
}

// batchArchiveCmd archives every named habit — the batch-mode ("V") version
// of the single-habit "a" archive key. Returning statusMsg reuses the
// existing reload-on-status convention (see the statusMsg case in Update),
// same as every other single-habit mutation in this file.
func batchArchiveCmd(s *store.Store, names []string) tea.Cmd {
	return func() tea.Msg {
		for _, n := range names {
			_ = s.ArchiveHabit(n)
		}
		return statusMsg(fmt.Sprintf("Archived %d habit(s)", len(names)))
	}
}

func clearAfter() tea.Cmd {
	return tea.Tick(3*time.Second, func(_ time.Time) tea.Msg {
		return clearMsgMsg{}
	})
}

func clearAfterUndo() tea.Cmd {
	return tea.Tick(undoWindow, func(_ time.Time) tea.Msg {
		return clearUndoMsg{}
	})
}

// copyToClipboardCmd copies via OSC 52 (works over SSH/tmux) and also shells
// out to pbcopy — Terminal.app ignores OSC 52 — same approach taskctl/mailctl/
// notectl/calctl use for their own "y" copy shortcuts, no clipboard
// library needed.
func copyToClipboardCmd(text string) tea.Cmd {
	return tea.Batch(tea.SetClipboard(text), func() tea.Msg {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
		return nil
	})
}
