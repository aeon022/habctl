package tui

import (
	"fmt"
	"image/color"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/aeon022/missionctl-core/humanize"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/habctl/internal/ai"
	"github.com/aeon022/habctl/internal/models"
	"github.com/aeon022/missionctl-core/overlay"
)

// ── view ──────────────────────────────────────────────────────────────────────

func (m model) View() tea.View {
	v := tea.NewView(m.viewContent())
	// v1's WithAltScreen()/WithMouseAllMotion() Program options are gone in
	// v2 — they are per-View fields now.
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	return v
}

func (m model) viewContent() string {
	switch m.state {
	case viewHelp:
		// "?" is only reachable from the main list (handleList), so the list
		// is always the correct background to keep visible behind the popup.
		return overlay.Center(m.renderList(), m.renderHelpPopup(), m.width, m.height, 1)
	case viewAddInput:
		return m.renderAddInput()
	case viewAddDesc:
		return m.renderAddDesc()
	case viewSuggest:
		return m.renderSuggest()
	case viewSettings:
		return m.renderSettings()
	case viewKeyInput:
		return m.renderKeyInput()
	case viewStats:
		return m.renderStats()
	case viewEditHabit:
		return m.renderEditHabit()
	case viewGroupMgr:
		return m.renderGroupMgr()
	case viewGroupNew:
		return m.renderGroupNew()
	case viewGroupPick:
		return m.renderGroupPick()
	case viewHabitDetail:
		return m.renderHabitDetail()
	case viewReview:
		return m.renderReview()
	case viewNoteInput:
		return m.renderNoteInput()
	case viewChainMgr:
		return m.renderChainMgr()
	case viewChainPick:
		return m.renderChainPick()
	case viewGeminiMenu:
		return m.renderGeminiMenu()
	case viewGeminiCID:
		return m.renderGeminiCID()
	case viewGeminiCS:
		return m.renderGeminiCS()
	case viewOAuthWait:
		return m.renderOAuthWait()
	case viewArchive:
		return m.renderArchive()
	case viewPresets:
		return m.renderPresets()
	case viewGoalInput:
		return m.renderGoalInput()
	case viewConfirm:
		return m.renderConfirm()
	case viewFilterInput:
		return m.renderList()
	case viewCommand:
		return m.renderList()
	default:
		return m.renderList()
	}
}

// ── layout helpers ────────────────────────────────────────────────────────────

// panel wraps content in the shared panel style with a width that fills the terminal.
func (m model) panel(s string) string {
	w := m.width - 2
	if w < 62 {
		w = 62
	}
	return panelStyle.Width(w).Render(s)
}

// innerWidth returns the usable text width inside the panel.
// panelStyle: border 1+1 + padding 2+2 = 6 chars overhead; -2 for panel margin.
func (m model) innerWidth() int {
	w := m.width - 8
	if w < 54 {
		w = 54
	}
	if w > 128 {
		w = 128
	}
	return w
}

// tinyBar renders a compact filled/empty progress bar of given width.
func tinyBar(done, total, width int) string {
	if total == 0 || width <= 0 {
		return styleMuted.Render(strings.Repeat("░", width))
	}
	filled := (done * width) / total
	if filled > width {
		filled = width
	}
	return styleOk.Render(strings.Repeat("█", filled)) + styleMuted.Render(strings.Repeat("░", width-filled))
}

// dynamicPanel renders a panel with a custom border color.
func (m model) dynamicPanel(s string, bc color.Color) string {
	w := m.width - 2
	if w < 62 {
		w = 62
	}
	return panelStyle.Width(w).BorderForeground(bc).Render(s)
}

// ── renderList ────────────────────────────────────────────────────────────────

func (m model) renderList() string {
	var b strings.Builder
	today := truncateDay(time.Now())

	innerW := m.innerWidth()

	// active "/" filter: input line while typing, chip afterwards
	filterLine := ""
	if m.state == viewFilterInput {
		filterLine = "  / " + m.input.View() + "\n"
	} else if m.filterQ != "" {
		filterLine = "  " + styleMuted.Render("filter: /"+m.filterQ+"  (esc clears)") + "\n"
	}

	// active ":" command palette: input line + up to 6 live-filtered matches
	cmdLine := ""
	if m.state == viewCommand {
		var cb strings.Builder
		cb.WriteString("  : " + m.input.View() + "\n")
		matches := matchPaletteCommands(m.input.Value())
		if len(matches) > 6 {
			matches = matches[:6]
		}
		for i, c := range matches {
			row := fmt.Sprintf("%-11s %s", c.name, c.desc)
			if i == m.cmdCursor {
				cb.WriteString("    " + styleOkBold.Render("▶ "+row) + "\n")
			} else {
				cb.WriteString("      " + styleMuted.Render(row) + "\n")
			}
		}
		if len(matches) == 0 {
			cb.WriteString("    " + styleMuted.Render("no matching command") + "\n")
		}
		cmdLine = cb.String()
	}

	// ── header ────────────────────────────────────────────────────────────────

	done, total := 0, len(m.habits)
	for _, h := range m.habits {
		if h.CheckedToday {
			done++
		}
	}
	bestStreak := 0
	for _, h := range m.habits {
		if h.Streak > bestStreak {
			bestStreak = h.Streak
		}
	}

	// precompute border color
	anyAtRisk := false
	for _, h := range m.habits {
		if h.Habit.FreqTarget > 0 {
			wd := int(time.Now().Weekday())
			daysLeft := 1
			if wd != 0 {
				daysLeft = 8 - wd
			}
			needed := h.Habit.FreqTarget - h.WeeklyDone
			if needed > 0 && daysLeft <= needed {
				anyAtRisk = true
				break
			}
		} else {
			if h.Streak > 0 && !h.CheckedToday {
				anyAtRisk = true
				break
			}
		}
	}
	var borderColor color.Color
	switch {
	case total > 0 && done == total:
		borderColor = ColorOk
	case anyAtRisk:
		borderColor = colorWarn
	default:
		borderColor = colorBorder
	}

	appName := sectionHeader("Habits")
	dateStr := styleMuted.Render(today.Format("Mon, 02 Jan 2006"))
	pad := innerW - lipgloss.Width(appName) - lipgloss.Width(dateStr)
	if pad < 1 {
		pad = 1
	}
	b.WriteString(appName + strings.Repeat(" ", pad) + dateStr + "\n")

	var statsLine strings.Builder
	if bestStreak > 0 {
		statsLine.WriteString(styleOkBold.Render(fmt.Sprintf("🔥 %d", bestStreak)) +
			styleMuted.Render(" days  ·  "))
	}
	if total > 0 {
		var ps lipgloss.Style
		switch {
		case done == total:
			ps = styleOkBold
		case done > 0:
			ps = styleOk
		default:
			ps = styleMuted
		}
		bar := styleMuted.Render("[") + tinyBar(done, total, 6) + styleMuted.Render("]")
		statsLine.WriteString(bar + " " + ps.Render(fmt.Sprintf("%d/%d", done, total)) +
			styleMuted.Render(fmt.Sprintf("  ·  %d habits", total)))
	}
	b.WriteString(statsLine.String() + "\n")
	if filterLine != "" {
		b.WriteString(filterLine)
	}
	if cmdLine != "" {
		b.WriteString(cmdLine)
	}
	if m.batchMode {
		b.WriteString("  " + styleLime.Render(fmt.Sprintf("select: %d", len(m.batchSelected))) +
			styleMuted.Render("  space toggle  A all  enter archive  esc cancel") + "\n")
	}
	b.WriteString("\n")

	// ── habit list ────────────────────────────────────────────────────────────

	if total == 0 {
		if m.filterQ != "" || m.state == viewFilterInput {
			b.WriteString(styleMuted.Render("No habits match the filter.") + "\n")
		} else {
			b.WriteString(styleMuted.Render("No habits yet — press n to add one.") + "\n")
		}
	} else {
		const cbW = 4   // "[✓] "
		const dotsW = 9 // 7-day dots + trailing space
		const skW = 8   // right-aligned streak column
		nameW := innerW - cbW - dotsW - skW
		if nameW < 20 {
			nameW = 20
		}

		visible, start := m.visibleHabitsWithStart(m.habitWindowHeight())
		var lastGroupID int64 = -1
		if start > 0 {
			// Seed with the group of the row just above the window so a
			// scrolled-into group doesn't re-print its header — matches
			// how scrolling past a header already behaves in this loop.
			lastGroupID = m.habits[start-1].Habit.GroupID
		}

		for localI, h := range visible {
			i := start + localI
			gid := h.Habit.GroupID
			if gid != lastGroupID {
				lastGroupID = gid
				if gid != 0 {
					g := groupByID(m.groups, gid)
					label := g.Name
					if g.Icon != "" {
						label = g.Icon + " " + g.Name
					}
					gDone, gTotal := 0, 0
					for _, hh := range m.habits {
						if hh.Habit.GroupID == gid {
							gTotal++
							if hh.CheckedToday {
								gDone++
							}
						}
					}
					minibar := styleMuted.Render("[") + tinyBar(gDone, gTotal, 4) + styleMuted.Render("]")
					counter := minibar + styleMuted.Render(fmt.Sprintf(" %d/%d", gDone, gTotal))
					ctrW := lipgloss.Width(counter)
					groupNameW := innerW - ctrW - 1
					if groupNameW < 1 {
						groupNameW = 1
					}
					glabel := lipgloss.NewStyle().Width(groupNameW).Render(
						styleGroup.Render(humanize.Truncate(label, groupNameW-1)),
					)
					b.WriteString("\n" + glabel + " " + counter + "\n")
				} else if i > 0 {
					b.WriteString("\n")
				}
			}

			selected := i == m.cursor
			hovered := !selected && i == m.hoverRow
			var atRisk bool
			if h.Habit.FreqTarget > 0 {
				wd := int(time.Now().Weekday())
				daysLeft := 1
				if wd != 0 {
					daysLeft = 8 - wd
				}
				needed := h.Habit.FreqTarget - h.WeeklyDone
				atRisk = needed > 0 && daysLeft <= needed
			} else {
				atRisk = h.Streak > 0 && !h.CheckedToday
			}

			var cb string
			var ns lipgloss.Style
			switch {
			case m.batchMode && m.batchSelected[h.Habit.Name]:
				cb = styleLime.Render("[x]") + " "
				ns = lipgloss.NewStyle().Foreground(ColorFg).Bold(true)
			case m.batchMode:
				cb = styleMuted.Render("[ ]") + " "
				ns = styleMuted
			case selected && h.CheckedToday:
				cb = styleLime.Render("[✓]") + " "
				ns = lipgloss.NewStyle().Foreground(ColorFg).Bold(true)
			case selected:
				cb = styleLime.Render("[·]") + " "
				ns = lipgloss.NewStyle().Foreground(ColorFg).Bold(true)
			case hovered && h.CheckedToday:
				cb = styleHover.Render("[✓]") + " "
				ns = styleHover
			case hovered:
				cb = styleHover.Render("[·]") + " "
				ns = styleHover
			case h.CheckedToday:
				cb = styleOk.Render("[✓]") + " "
				ns = styleOk
			case atRisk:
				cb = styleWarnBd.Render("[!]") + " "
				ns = styleWarn
			default:
				cb = styleMuted.Render("[ ]") + " "
				ns = styleMuted
			}

			// per-habit 7-day dots
			var dotsBuf strings.Builder
			for di, chkd := range h.Last7Days {
				isToday := di == 6
				if chkd {
					if isToday {
						dotsBuf.WriteString(styleOkBold.Render("●"))
					} else {
						dotsBuf.WriteString(styleOk.Render("●"))
					}
				} else {
					if isToday && atRisk {
						dotsBuf.WriteString(styleWarnBd.Render("○"))
					} else {
						dotsBuf.WriteString(styleMuted.Render("○"))
					}
				}
			}
			dotsCol := lipgloss.NewStyle().Width(dotsW).Render(dotsBuf.String())

			matchIdx := fuzzyMatchIndexes(m.filterQ, h.Habit.Name)
			var rawName string
			if h.Habit.Icon != "" {
				rawName = ns.Render(h.Habit.Icon+" ") + highlightMatches(humanize.Truncate(h.Habit.Name, nameW-4), matchIdx, ns)
			} else {
				rawName = highlightMatches(humanize.Truncate(h.Habit.Name, nameW-1), matchIdx, ns)
			}
			nameCol := lipgloss.NewStyle().Width(nameW).Render(rawName)

			var skContent string
			if h.Habit.FreqTarget > 0 {
				weekInfo := fmt.Sprintf("%d/%dW", h.WeeklyDone, h.Habit.FreqTarget)
				switch {
				case h.CheckedToday && h.Streak > 0:
					skContent = styleOkBold.Render("🔥 "+weekInfo) + styleMuted.Render(fmt.Sprintf(" %dw", h.Streak))
				case h.CheckedToday:
					skContent = styleOk.Render(weekInfo)
				case h.WeeklyDone > 0:
					skContent = styleWarn.Render(weekInfo)
				default:
					skContent = styleMuted.Render(weekInfo)
				}
			} else {
				switch {
				case h.CheckedToday && h.Streak > 0:
					skContent = styleOkBold.Render(fmt.Sprintf("🔥 %d", h.Streak))
				case atRisk:
					skContent = styleWarnBd.Render(fmt.Sprintf("🔥 %d!", h.Streak))
				case h.Streak > 0:
					skContent = styleMuted.Render(fmt.Sprintf("%d", h.Streak))
				default:
					skContent = styleMuted.Render("0")
				}
			}
			skCol := lipgloss.NewStyle().Width(skW).Align(lipgloss.Right).Render(skContent)

			b.WriteString(cb + nameCol + dotsCol + skCol + "\n")

			if !m.compact {
				const descMaxW = 58
				const subIndent = "      "
				if h.Habit.Description != "" {
					b.WriteString(subIndent + styleMuted.Render(humanize.Truncate(h.Habit.Description, descMaxW)) + "\n")
				}
				if h.TodayNote != "" {
					b.WriteString(subIndent + styleMuted.Render("📝 "+humanize.Truncate(h.TodayNote, descMaxW-3)) + "\n")
				}
				if h.ChainTo != "" {
					b.WriteString(subIndent + styleMuted.Render("→ "+h.ChainTo) + "\n")
				}
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	if m.message != "" {
		msgStyle := styleOk
		if m.isErr {
			msgStyle = styleDanger
		}
		b.WriteString(msgStyle.Render(m.message) + "\n\n")
	}

	// Pin the footer to the bottom of the panel instead of letting it
	// glue itself right under a short habit list — pad the body out to
	// the panel's full line budget first (panelStyle overhead: border
	// 1+1, padding(1,2) → 4 rows), same pattern taskctl/notectl use.
	if m.height > 0 {
		budget := m.height - 4
		for lines := strings.Count(b.String(), "\n") + 1; lines < budget; lines++ {
			b.WriteString("\n")
		}
	}

	fk := func(key, label string) string {
		return styleLime.Render(key) + styleMuted.Render(":"+label)
	}
	footer := fk("space", "✓/✗") + styleMuted.Render("  ") +
		fk("↵", "open") + styleMuted.Render("  ") +
		fk("n", "new") + styleMuted.Render("  ") +
		fk("e", "edit") + styleMuted.Render("  ") +
		fk("d", "delete") + styleMuted.Render("  ") +
		fk("y", "copy") + styleMuted.Render("  ") +
		fk("s", "AI") + styleMuted.Render("  ") +
		fk("r", "review") + styleMuted.Render("  ") +
		styleLime.Render(":") + styleMuted.Render("cmd") + styleMuted.Render("  ") + // the key IS ":" — fk() would double it into "::cmd"
		fk("?", "help") + styleMuted.Render("  ") +
		fk("q", "quit")
	b.WriteString(footer)
	return m.dynamicPanel(b.String(), borderColor)
}

// habitWindowHeight is the approximate row-count budget for the scroll
// window (visibleHabitsWithStart) — an approximation like budgetctl/
// calctl/taskctl already use elsewhere, since rows here are variable
// height (1-4 lines) and an exact line budget isn't worth the
// complexity: panel border+padding top(2) + header+stats(2) + optional
// filter chip(1) + blank(1) reserved up front, then a generous fixed
// footer reservation (blank + footer text + optional message + panel
// border+padding bottom) so the last row+description never gets clipped.
func (m model) habitWindowHeight() int {
	preamble := 4
	if m.filterQ != "" {
		preamble++
	}
	h := m.height - preamble - 6
	if h < 1 {
		h = 1
	}
	return h
}

// visibleHabitsWithStart returns the scroll-windowed slice of m.habits
// that keeps m.cursor in view, plus its start index into m.habits, so
// renderList and rowHitTest can't drift apart on which habits are shown.
func (m model) visibleHabitsWithStart(height int) ([]models.HabitStats, int) {
	if len(m.habits) == 0 {
		return nil, 0
	}
	start := 0
	end := len(m.habits)
	if end-start > height {
		mid := m.cursor - height/2
		if mid < 0 {
			mid = 0
		}
		if mid+height > end {
			mid = end - height
		}
		start = mid
		end = start + height
	}
	return m.habits[start:end], start
}

// rowHitTest returns the m.habits index at screen row y, or -1 if the
// click landed on a header, group label, description/note line, or
// outside the list. Mirrors renderList's exact line-counting: panel
// border+padding(2), header+stats(2), an optional filter chip(1), a
// blank separator(1), then per habit — an optional group-change block
// (2 lines for a new named group, 1 blank line when returning to
// "no group" past the first row), the main row(1), 0-3 optional
// description/note/chain lines when not compact, and a trailing blank(1).
// Walks the same scroll window renderList computes (visibleHabitsWithStart,
// seeded with the group of the row just above the window) so a click
// lands on the habit it visually appears to be over once scrolled.
func (m model) rowHitTest(y int) int {
	row := 2 + 2 // panel border+padding, header+stats
	if m.filterQ != "" {
		row++
	}
	row++ // blank line before the list

	visible, start := m.visibleHabitsWithStart(m.habitWindowHeight())
	var lastGroupID int64 = -1
	if start > 0 {
		lastGroupID = m.habits[start-1].Habit.GroupID
	}
	for localI, h := range visible {
		i := start + localI
		gid := h.Habit.GroupID
		if gid != lastGroupID {
			lastGroupID = gid
			if gid != 0 {
				row += 2
			} else if i > 0 {
				row++
			}
		}

		if y == row {
			return i
		}
		row++ // main row

		if !m.compact {
			if h.Habit.Description != "" {
				row++
			}
			if h.TodayNote != "" {
				row++
			}
			if h.ChainTo != "" {
				row++
			}
		}
		row++ // trailing blank
	}
	return -1
}

// ── renderAddInput ────────────────────────────────────────────────────────────

func (m model) renderAddInput() string {
	var b strings.Builder
	b.WriteString(sectionHeader("New Habit") + "\n\n")
	b.WriteString(styleMuted.Render("Tip: emoji prefix — 🏃 Running, ☕ Coffee, 📚 Reading") + "\n\n")
	b.WriteString(m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter continue · esc cancel"))
	return m.panel(b.String())
}

// ── renderAddDesc ─────────────────────────────────────────────────────────────

func (m model) renderAddDesc() string {
	var b strings.Builder
	icon := ""
	if m.addingIcon != "" {
		icon = m.addingIcon + " "
	}
	b.WriteString(sectionHeader("New Habit") + "\n\n")
	b.WriteString(styleLime.Bold(true).Render(icon+m.addingName) + "\n\n")
	b.WriteString(styleMuted.Render("Short note? (enter to skip)") + "\n\n")
	b.WriteString(m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter save · esc skip"))
	return m.panel(b.String())
}

// ── renderEditHabit ───────────────────────────────────────────────────────────

func (m model) renderEditHabit() string {
	var b strings.Builder
	b.WriteString(sectionHeader("Edit Habit") + "\n\n")

	row := func(active bool, label, value string) {
		cursor := "  "
		ls := styleMuted
		vs := styleMuted
		if active {
			cursor = styleLime.Render("▶ ")
			ls = styleFg
			vs = styleFg.Bold(true)
		}
		b.WriteString(cursor + ls.Render(label+":") + "  " + vs.Render(value) + "\n")
	}

	switch m.editCursor {
	case 0:
		b.WriteString(styleLime.Render("▶ ") + styleFg.Render("Name / Icon:") + "\n")
		b.WriteString("    " + m.input.View() + "\n\n")
		row(false, "Description", m.editDescBuf)
	case 1:
		row(false, "Name / Icon", m.editNameBuf)
		b.WriteString("\n")
		b.WriteString(styleLime.Render("▶ ") + styleFg.Render("Description:") + "\n")
		b.WriteString("    " + m.input.View() + "\n")
	default:
		row(false, "Name / Icon", m.editNameBuf)
		row(false, "Description", m.editDescBuf)
	}

	b.WriteString("\n")

	freqActive := m.editCursor == 2
	skipActive := m.editCursor == 3

	freqCursor := "  "
	freqStyle := styleMuted
	if freqActive {
		freqCursor = styleLime.Render("▶ ")
		freqStyle = styleFg
	}
	freqVal := "daily"
	if m.editFreq > 0 {
		freqVal = fmt.Sprintf("%d× per week", m.editFreq)
	}
	b.WriteString(freqCursor + freqStyle.Render("Frequency:") + "  ")
	if freqActive {
		b.WriteString(styleFg.Bold(true).Render(freqVal) + "  " + styleMuted.Render("+/- to change"))
	} else {
		b.WriteString(styleMuted.Render(freqVal))
	}
	b.WriteString("\n")

	skipCursor := "  "
	skipStyle := styleMuted
	if skipActive {
		skipCursor = styleLime.Render("▶ ")
		skipStyle = styleFg
	}
	skipVal := "no skip"
	if m.editSkip > 0 {
		skipVal = fmt.Sprintf("%d missed day%s ok", m.editSkip, func() string {
			if m.editSkip == 1 {
				return ""
			}
			return "s"
		}())
	}
	b.WriteString(skipCursor + skipStyle.Render("Skip tolerance:") + "  ")
	if skipActive {
		b.WriteString(styleFg.Bold(true).Render(skipVal) + "  " + styleMuted.Render("+/- to change"))
	} else {
		b.WriteString(styleMuted.Render(skipVal))
	}
	b.WriteString("\n")

	b.WriteString("\n" + styleMuted.Render("enter save · tab next field · +/- for numbers · esc cancel"))
	return m.panel(b.String())
}

// ── renderGroupMgr ────────────────────────────────────────────────────────────

func (m model) renderGroupMgr() string {
	var b strings.Builder
	b.WriteString(sectionHeader("Groups") + "\n\n")

	if len(m.groups) == 0 {
		b.WriteString(styleMuted.Render("No groups yet — press a to create one.") + "\n\n")
	} else {
		for i, g := range m.groups {
			cursor := "  "
			labelStyle := lipgloss.NewStyle().Foreground(ColorMuted)
			if i == m.groupCursor {
				cursor = styleLime.Render("▶ ")
				labelStyle = lipgloss.NewStyle().Foreground(ColorFg)
			}
			icon := ""
			if g.Icon != "" {
				icon = g.Icon + " "
			}
			// Count habits in group
			count := 0
			for _, h := range m.habits {
				if h.Habit.GroupID == g.ID {
					count++
				}
			}
			b.WriteString(cursor + labelStyle.Render(icon+g.Name) +
				styleMuted.Render(fmt.Sprintf("  (%d habits)", count)) + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(styleMuted.Render("a new · d delete · u undo · j/k navigate · esc back"))
	return m.panel(b.String())
}

// ── renderGroupNew ────────────────────────────────────────────────────────────

func (m model) renderGroupNew() string {
	var b strings.Builder
	b.WriteString(sectionHeader("New Group") + "\n\n")
	b.WriteString(styleMuted.Render("Tip: start with emoji — 🌅 Morning, 💻 Work, 🌙 Evening") + "\n\n")
	b.WriteString(m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter create · esc cancel"))
	return m.panel(b.String())
}

// ── renderGroupPick ───────────────────────────────────────────────────────────

func (m model) renderGroupPick() string {
	var b strings.Builder
	habitName := ""
	if m.cursor < len(m.habits) {
		h := m.habits[m.cursor].Habit
		habitName = h.Name
		if h.Icon != "" {
			habitName = h.Icon + " " + habitName
		}
	}
	b.WriteString(sectionHeader("Assign Group") + "\n")
	b.WriteString(styleMuted.Render("→ "+habitName) + "\n\n")

	options := append([]models.Group{{Name: "None (ungrouped)", Icon: "○"}}, m.groups...)
	for i, g := range options {
		cursor := "  "
		labelStyle := lipgloss.NewStyle().Foreground(ColorMuted)
		if i == m.groupCursor {
			cursor = styleLime.Render("▶ ")
			labelStyle = lipgloss.NewStyle().Foreground(ColorFg)
		}
		icon := ""
		if g.Icon != "" {
			icon = g.Icon + " "
		}
		b.WriteString(cursor + labelStyle.Render(icon+g.Name) + "\n")
	}

	b.WriteString("\n" + styleMuted.Render("enter select · j/k navigate · esc cancel"))
	return m.panel(b.String())
}

// ── renderStats ───────────────────────────────────────────────────────────────

func (m model) renderStats() string {
	cal := m.calData
	total := cal.TotalHabits
	byDate := cal.ByDate

	var b strings.Builder
	b.WriteString(sectionHeader("Stats") + "\n\n")

	if total == 0 {
		b.WriteString(styleMuted.Render("Add habits first (n), then data will appear here.") + "\n")
		b.WriteString("\n" + styleMuted.Render("esc back"))
		return m.panel(b.String())
	}

	today := truncateDay(time.Now())

	var daysTracked, perfectDays int
	var completionSum float64
	for dateStr, cnt := range byDate {
		t, _ := time.ParseInLocation("2006-01-02", dateStr, time.Local)
		if !t.After(today) {
			daysTracked++
			pct := float64(cnt) / float64(total)
			completionSum += pct
			if cnt >= total {
				perfectDays++
			}
		}
	}
	avgCompletion := 0.0
	if daysTracked > 0 {
		avgCompletion = completionSum / float64(daysTracked) * 100
	}

	bestStreak, bestName := 0, ""
	bestLongest, bestLongestName := 0, ""
	for _, h := range m.habits {
		if h.Streak > bestStreak {
			bestStreak = h.Streak
			bestName = h.Habit.Name
		}
		if h.LongestStreak > bestLongest {
			bestLongest = h.LongestStreak
			bestLongestName = h.Habit.Name
		}
	}

	numV := lipgloss.NewStyle().Foreground(ColorLime).Bold(true)
	lbl := styleMuted

	b.WriteString(styleMuted.Render("// overview") + "\n")
	b.WriteString(fmt.Sprintf("  %s %s   %s %s   %s %s\n",
		numV.Render(fmt.Sprintf("%d", daysTracked)), lbl.Render("days tracked"),
		numV.Render(fmt.Sprintf("%.0f%%", avgCompletion)), lbl.Render("avg"),
		numV.Render(fmt.Sprintf("%d", perfectDays)), lbl.Render("perfect days"),
	))
	if bestStreak > 0 {
		b.WriteString(fmt.Sprintf("  %s %s  %s\n",
			lbl.Render("🔥 current:"),
			numV.Render(fmt.Sprintf("%d days", bestStreak)),
			lbl.Render("— "+humanize.Truncate(bestName, 24)),
		))
	}
	if bestLongest > bestStreak {
		b.WriteString(fmt.Sprintf("  %s %s  %s\n",
			lbl.Render("   longest:"),
			numV.Render(fmt.Sprintf("%d days", bestLongest)),
			lbl.Render("— "+humanize.Truncate(bestLongestName, 24)),
		))
	}

	// ── heatmap ──────────────────────────────────────────────────────────────
	// As many weeks as the panel width allows, up to a full year (52) — 2
	// chars/week + a 3-char day-label gutter, same math as innerWidth uses
	// elsewhere. Narrow terminals degrade gracefully instead of wrapping.
	weeks := (m.innerWidth() - 3) / 2
	if weeks > 52 {
		weeks = 52
	}
	if weeks < 8 {
		weeks = 8
	}
	b.WriteString("\n" + styleMuted.Render(fmt.Sprintf("// contributions (%d weeks)", weeks)) + "\n")

	wd := int(today.Weekday())
	daysFromMon := (wd + 6) % 7
	thisMonday := today.AddDate(0, 0, -daysFromMon)
	startDate := thisMonday.AddDate(0, 0, -(weeks-1)*7)

	// Month label row
	var monthLine strings.Builder
	monthLine.WriteString("   ")
	lastMonth := -1
	for w := 0; w < weeks; w++ {
		day := startDate.AddDate(0, 0, w*7)
		mo := int(day.Month())
		if mo != lastMonth {
			monthLine.WriteString(styleMuted.Render(day.Format("Jan")[:1]))
			lastMonth = mo
		} else {
			monthLine.WriteString(" ")
		}
		monthLine.WriteString(" ")
	}
	b.WriteString(monthLine.String() + "\n")

	heat := heatColors
	dayLabels := []string{"m", "t", "w", "t", "f", "s", "s"}

	for d := 0; d < 7; d++ {
		var row strings.Builder
		row.WriteString(styleMuted.Render(dayLabels[d]) + " ")
		for w := 0; w < weeks; w++ {
			day := startDate.AddDate(0, 0, w*7+d)
			if day.After(today) {
				row.WriteString(heat[0].Render("░ "))
				continue
			}
			cnt := byDate[day.Format("2006-01-02")]
			level := 0
			if total > 0 && cnt > 0 {
				pct := float64(cnt) / float64(total)
				switch {
				case pct >= 1.0:
					level = 4
				case pct >= 0.5:
					level = 3
				case pct >= 0.25:
					level = 2
				default:
					level = 1
				}
			}
			cell := "░"
			if level > 0 {
				cell = "█"
			}
			row.WriteString(heat[level].Render(cell + " "))
		}
		b.WriteString(row.String() + "\n")
	}
	b.WriteString(styleMuted.Render("  ░ 0%  ") +
		heat[2].Render("█") + styleMuted.Render(" 1-49%  ") +
		heat[3].Render("█") + styleMuted.Render(" 50-99%  ") +
		heat[4].Render("█") + styleMuted.Render(" 100%") + "\n")

	// ── day of week ───────────────────────────────────────────────────────────
	b.WriteString("\n" + styleMuted.Render("// day of week") + "\n")
	var dowCount [7]int
	var dowTotal [7]int
	for dateStr, cnt := range byDate {
		t, err := time.ParseInLocation("2006-01-02", dateStr, time.Local)
		if err != nil || t.After(today) {
			continue
		}
		dow := (int(t.Weekday()) + 6) % 7
		dowTotal[dow] += total
		dowCount[dow] += cnt
	}
	dowLabels := []string{"mo", "tu", "we", "th", "fr", "sa", "su"}
	barW := 22
	for d := 0; d < 7; d++ {
		pct := 0.0
		if dowTotal[d] > 0 {
			pct = float64(dowCount[d]) / float64(dowTotal[d])
		}
		filled := int(pct * float64(barW))
		bar := styleLime.Render(strings.Repeat("█", filled)) +
			styleMuted.Render(strings.Repeat("░", barW-filled))
		b.WriteString(fmt.Sprintf("  %s %s %s\n",
			styleMuted.Render(dowLabels[d]),
			bar,
			styleMuted.Render(fmt.Sprintf("%3.0f%%", pct*100))))
	}

	// ── correlations ─────────────────────────────────────────────────────────
	if pairs := topHabitCorrelations(m.habits, m.checkinDates, 3); len(pairs) > 0 {
		b.WriteString("\n" + styleMuted.Render("// tend to happen together") + "\n")
		for _, p := range pairs {
			b.WriteString(fmt.Sprintf("  %s %s %s %s\n",
				numV.Render(fmt.Sprintf("%.0f%%", p.jaccard*100)),
				lbl.Render("of the time"),
				lbl.Render(humanize.Truncate(p.nameA, 20)+" +"),
				lbl.Render(humanize.Truncate(p.nameB, 20)),
			))
		}
	}

	b.WriteString("\n" + styleMuted.Render("esc back"))
	return m.panel(b.String())
}

// habitPair is one habit-correlation result: how often habits A and B were
// both checked on the same day, out of every day either one was.
type habitPair struct {
	nameA, nameB string
	jaccard      float64
}

// topHabitCorrelations ranks every pair of active habits by co-occurrence
// rate (Jaccard similarity of their checked-date sets) and returns the top
// n. Pairs with too little shared history to mean anything (fewer than 5
// days where at least one of the two was checked) are excluded — otherwise
// two habits each checked once, on the same day, would misleadingly show
// 100%.
func topHabitCorrelations(habits []models.HabitStats, checkinDates map[int64]map[string]bool, n int) []habitPair {
	var active []models.HabitStats
	for _, h := range habits {
		if !h.Habit.Archived && len(checkinDates[h.Habit.ID]) > 0 {
			active = append(active, h)
		}
	}
	var pairs []habitPair
	for i := 0; i < len(active); i++ {
		for j := i + 1; j < len(active); j++ {
			a, bb := checkinDates[active[i].Habit.ID], checkinDates[active[j].Habit.ID]
			shared, union := 0, 0
			for d := range a {
				union++
				if bb[d] {
					shared++
				}
			}
			for d := range bb {
				if !a[d] {
					union++
				}
			}
			if union < 5 {
				continue
			}
			pairs = append(pairs, habitPair{
				nameA:   active[i].Habit.Name,
				nameB:   active[j].Habit.Name,
				jaccard: float64(shared) / float64(union),
			})
		}
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].jaccard > pairs[j].jaccard })
	if len(pairs) > n {
		pairs = pairs[:n]
	}
	return pairs
}

// ── renderSuggest ─────────────────────────────────────────────────────────────

func (m model) renderSuggest() string {
	var b strings.Builder
	providerLabel := ""
	if m.suggestBlocked {
		// no call was made — showing a detected provider here would be misleading
	} else if info, err := ai.Detect(); err == nil {
		providerLabel = styleMuted.Render("via " + info.Display)
	} else {
		providerLabel = styleWarn.Render("no provider — S for settings")
	}

	if m.suggestMode == "chain" {
		b.WriteString(sectionHeader("Chain Suggestions") + "  " + providerLabel + "\n\n")

		if len(m.chainSuggestItems) > 0 {
			checkOff := styleMuted.Render("[ ]")
			checkOn := styleOk.Bold(true).Render("[✓]")
			indent := "       "

			for i, it := range m.chainSuggestItems {
				if i > 0 {
					b.WriteString("\n")
				}
				selected := i == m.suggestCursor
				cursor := "  "
				nameStyle := styleMuted
				if selected {
					cursor = styleLime.Render("▶ ")
					nameStyle = styleFg.Bold(true)
				}
				chk := checkOff
				if it.selected {
					chk = checkOn
				}
				b.WriteString(cursor + chk + " " + nameStyle.Render(it.from) +
					styleMuted.Render(" → ") + nameStyle.Render(it.to) + "\n")
				if it.reason != "" {
					for _, wl := range strings.Split(wordWrap(it.reason, 62), "\n") {
						if wl != "" {
							b.WriteString(indent + styleMuted.Render(wl) + "\n")
						}
					}
				}
			}

			b.WriteString("\n")
			selectedCount := 0
			for _, it := range m.chainSuggestItems {
				if it.selected {
					selectedCount++
				}
			}
			if selectedCount > 0 {
				b.WriteString(styleOk.Render(fmt.Sprintf("%d selected", selectedCount)) + "  ")
			}
			b.WriteString(styleMuted.Render("space ✓ · enter apply · a all · j/k · esc back"))
		} else if !m.suggestDone {
			b.WriteString(styleMuted.Render("Analysing habit connections…") + "\n\n")
			b.WriteString(styleLime.Render("▌"))
		} else {
			b.WriteString(styleWarn.Render("No suggestions. I need at least 2 habits.") + "\n")
			b.WriteString(styleMuted.Render("esc back"))
		}
		return m.panel(b.String())
	}

	// ── habit / decompose suggest mode ───────────────────────────────────────
	title := "Habit Suggestions"
	loadingMsg := "Generating suggestions…"
	if m.suggestMode == "decompose" {
		title = "Goal Habits"
		loadingMsg = "Analysing goal and creating habits…"
	}
	b.WriteString(sectionHeader(title) + "  " + providerLabel + "\n\n")

	blinkCursor := "▌"
	if m.blinkOn {
		blinkCursor = " "
	}

	if len(m.suggestItems) > 0 {
		checkOff := styleMuted.Render("[ ]")
		checkOn := styleOk.Bold(true).Render("[✓]")
		indent := "       "

		for i, it := range m.suggestItems {
			if i > 0 {
				b.WriteString("\n")
			}
			selected := i == m.suggestCursor
			cursor := "  "
			headerStyle := styleMuted
			if selected {
				cursor = styleLime.Render("▶ ")
				headerStyle = styleFg.Bold(true)
			}
			chk := checkOff
			if it.selected {
				chk = checkOn
			}

			b.WriteString(cursor + chk + " " + headerStyle.Render(it.header) + "\n")

			for _, d := range it.details {
				for _, wl := range strings.Split(wordWrap(d, 62), "\n") {
					if wl == "" {
						continue
					}
					b.WriteString(indent + styleMuted.Render(wl) + "\n")
				}
			}
		}

		b.WriteString("\n")
		selectedCount := 0
		for _, it := range m.suggestItems {
			if it.selected {
				selectedCount++
			}
		}
		if selectedCount > 0 {
			b.WriteString(styleOk.Render(fmt.Sprintf("%d selected", selectedCount)) + "  ")
		}
		b.WriteString(styleMuted.Render("space ✓ · enter add · a all · j/k · esc back"))
	} else if !m.suggestDone {
		b.WriteString(styleMuted.Render(loadingMsg) + "\n\n")
		b.WriteString(styleLime.Render(blinkCursor))
	} else if m.suggestBlocked {
		b.WriteString(styleWarn.Render(m.suggestText) + "\n")
		b.WriteString(styleMuted.Render("esc back"))
	} else {
		b.WriteString(styleWarn.Render("Format not recognised.") + "\n")
		b.WriteString(styleMuted.Render("s   try again") + "\n")
		b.WriteString(styleMuted.Render("n   add manually") + "\n")
		b.WriteString(styleMuted.Render("esc back"))
	}

	return m.panel(b.String())
}

// ── renderSettings ────────────────────────────────────────────────────────────

func (m model) renderSettings() string {
	var b strings.Builder
	b.WriteString(sectionHeader("AI Provider") + "\n")
	b.WriteString(styleMuted.Render("Select your AI provider and configure it.") + "\n\n")

	activeProvider := ai.Provider(os.Getenv("HABCTL_PROVIDER"))
	if activeProvider == "" {
		if info, err := ai.Detect(); err == nil {
			activeProvider = info.Name
		}
	}

	for i, p := range providers {
		selected := i == m.settingsCursor
		active := p.id == activeProvider

		cursor := "  "
		if selected {
			cursor = styleLime.Render("▶ ")
		}
		var labelStyle lipgloss.Style
		switch {
		case selected && active:
			labelStyle = lipgloss.NewStyle().Foreground(ColorLime).Bold(true)
		case selected:
			labelStyle = lipgloss.NewStyle().Foreground(ColorFg)
		case active:
			labelStyle = lipgloss.NewStyle().Foreground(ColorLime)
		default:
			labelStyle = lipgloss.NewStyle().Foreground(ColorMuted)
		}
		label := labelStyle.Width(26).Render(p.label)

		var badge string
		if p.id == ai.ProviderOllama {
			badge = styleOk.Render("● local")
		} else {
			var activeKey string
			switch p.id {
			case ai.ProviderAnthropic:
				activeKey = m.cfg.AnthropicKey
				if activeKey == "" {
					activeKey = os.Getenv(p.envKey)
				}
			case ai.ProviderOpenAI:
				activeKey = m.cfg.OpenAIKey
				if activeKey == "" {
					activeKey = os.Getenv(p.envKey)
				}
			case ai.ProviderGemini:
				activeKey = m.cfg.GeminiKey
				if activeKey == "" {
					activeKey = os.Getenv(p.envKey)
				}
			}
			oauthActive := p.id == ai.ProviderGemini && m.cfg.GoogleRefreshToken != ""
			if oauthActive {
				badge = styleOk.Render("● OAuth active")
			} else if activeKey != "" {
				suffix := activeKey
				if len(suffix) > 4 {
					suffix = "…" + suffix[len(suffix)-4:]
				}
				badge = styleOk.Render("● Key set") + styleMuted.Render(" ("+suffix+")")
			} else {
				badge = styleMuted.Render("○ no key")
			}
		}
		if active {
			badge += " " + styleLime.Render("← active")
		}
		b.WriteString(cursor + label + "  " + badge + "\n")
	}
	b.WriteString("\n" + styleMuted.Render("enter configure · j/k navigate · esc back"))
	return m.panel(b.String())
}

// ── renderKeyInput ────────────────────────────────────────────────────────────

func (m model) renderKeyInput() string {
	var b strings.Builder
	p := providers[m.settingsCursor]
	b.WriteString(sectionHeader(p.label+" setup") + "\n\n")
	switch p.id {
	case ai.ProviderGemini:
		b.WriteString(styleMuted.Render("o  open browser: aistudio.google.com") + "\n")
		b.WriteString(styleMuted.Render("   → log in with Google → 'Get API key'") + "\n\n")
	default:
		if p.keyPage != "" {
			b.WriteString(styleOk.Render("o") + styleMuted.Render("  opens "+p.keyPage) + "\n\n")
		}
	}
	b.WriteString(styleMuted.Render("API Key (Cmd+V):") + "\n")
	b.WriteString("  " + m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter save · o open browser · esc back"))
	return m.panel(b.String())
}

// ── renderGeminiMenu / CID / CS / OAuthWait ───────────────────────────────────

func (m model) renderGeminiMenu() string {
	var b strings.Builder
	b.WriteString(sectionHeader("Google Gemini") + "\n\n")
	options := []struct{ label, desc string }{
		{"Browser Login (Google Account)", "No key needed — login in browser"},
		{"API Key", "From aistudio.google.com"},
	}
	for i, o := range options {
		cursor := "  "
		ls := lipgloss.NewStyle().Foreground(ColorMuted)
		if i == m.geminiMenuCursor {
			cursor = styleLime.Render("▶ ")
			ls = lipgloss.NewStyle().Foreground(ColorFg)
		}
		b.WriteString(cursor + ls.Bold(true).Render(o.label) + "\n")
		b.WriteString("    " + styleMuted.Render(o.desc) + "\n\n")
	}
	if m.cfg.GoogleRefreshToken != "" {
		b.WriteString(styleOk.Render("● already logged in (OAuth)") + "\n\n")
	}
	b.WriteString(styleMuted.Render("enter select · j/k navigate · esc back"))
	return m.panel(b.String())
}

func (m model) renderGeminiCID() string {
	var b strings.Builder
	b.WriteString(sectionHeader("Set up Google OAuth2 Client") + "\n\n")
	b.WriteString(styleOk.Render("o") + styleMuted.Render("  opens console.cloud.google.com/apis/credentials") + "\n\n")
	b.WriteString(styleMuted.Render("1. Select project · 2. Create Credentials → OAuth 2.0 Client ID") + "\n")
	b.WriteString(styleMuted.Render("3. Type: Desktop App · 4. Copy Client ID") + "\n\n")
	b.WriteString(styleMuted.Render("Client ID:") + "\n")
	b.WriteString("  " + m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter continue · o open browser · esc back"))
	return m.panel(b.String())
}

func (m model) renderGeminiCS() string {
	var b strings.Builder
	b.WriteString(sectionHeader("Google OAuth2 Client Secret") + "\n\n")
	b.WriteString(styleMuted.Render("Client Secret from the same credentials page:") + "\n\n")
	b.WriteString(styleMuted.Render("Client Secret:") + "\n")
	b.WriteString("  " + m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter start browser login · esc back"))
	return m.panel(b.String())
}

func (m model) renderOAuthWait() string {
	var b strings.Builder
	b.WriteString(sectionHeader("Waiting for Google login…") + "\n\n")
	b.WriteString(styleMuted.Render(
		"Browser opened.\n\n"+
			"1. Log in with your Google account\n"+
			"2. Allow habctl access\n"+
			"3. Page shows \"Login successful\" → done\n\n"+
			"Timeout: 5 minutes") + "\n")
	b.WriteString("\n" + styleLime.Render("⠿ ") + styleMuted.Render("waiting…"))
	return m.panel(b.String())
}

// ── renderAdd / renderHelp ────────────────────────────────────────────────────

// renderHelp is now unused by the "?" key directly (see openHelp /
// renderHelpPopup) but kept as the content source both paths render.
func (m model) helpContent() string {
	lime := styleLime.Bold(true)
	key := lipgloss.NewStyle().Foreground(ColorLime).Width(26)
	desc := styleMuted

	row := func(k, d string) string {
		return "  " + key.Render(k) + desc.Render(d) + "\n"
	}
	section := func(title string) string {
		return "\n  " + lime.Render(title) + "\n"
	}

	var b strings.Builder
	b.WriteString(sectionHeader("Help") + "\n\n")
	b.WriteString(styleMuted.Render(
		"  Track habits every day. Build streaks. Miss a day and\n" +
			"  the streak resets — simple, honest accountability.\n",
	))
	b.WriteString(section("Navigation"))
	b.WriteString(row("j / ↓", "move down"))
	b.WriteString(row("k / ↑", "move up"))
	b.WriteString(row("/", "filter habits (esc clears)"))
	b.WriteString(row(":", "command palette — type an action by name"))
	b.WriteString(section("Habits"))
	b.WriteString(row("space", "check in / undo check-in (toggle)"))
	b.WriteString(row("enter", "open habit (detail, description, note history)"))
	b.WriteString(row("N", "add note to today's check-in"))
	b.WriteString(row("n", "new habit (optional emoji prefix)"))
	b.WriteString(row("p", "add from a curated habit template list"))
	b.WriteString(row("e", "edit habit (name, desc, frequency, skip)"))
	b.WriteString(row("a", "archive habit (history preserved)"))
	b.WriteString(row("A", "open archive (restore / delete)"))
	b.WriteString(row("d", "delete habit permanently"))
	b.WriteString(section("Groups"))
	b.WriteString(row("m", "move habit to group"))
	b.WriteString(row("G", "manage groups (add, delete)"))
	b.WriteString(section("AI & Views"))
	b.WriteString(row("s", "AI suggestions (context-aware)"))
	b.WriteString(row("g", "goal → 3 linked habits (decompose)"))
	b.WriteString(row("r", "AI weekly review — pattern coaching briefing"))
	b.WriteString(row("t", "stats — heatmap & completion"))
	b.WriteString(row("c", "manage habit chains"))
	b.WriteString(row("S", "settings — provider & API keys"))
	b.WriteString(row("v", "compact/normal toggle (hide/show descriptions)"))
	b.WriteString(row("w", "toggle 7d / 30d streak window"))
	b.WriteString(section("Status"))
	b.WriteString(row(styleOk.Render("✓  green"), "checked in today"))
	b.WriteString(row(styleWarn.Render("!  amber"), "streak at risk — check in before midnight!"))
	b.WriteString(row(styleMuted.Render("·  gray"), "not done this day"))
	b.WriteString(section("Other"))
	b.WriteString(row("?", "toggle this help screen"))
	b.WriteString(row("q / ctrl+c", "quit"))
	return b.String()
}

// openHelp sizes and populates the transient help popup (see
// renderHelpPopup/overlayCenter) so it always fits within the background
// list panel's own border — computed from the ACTUAL rendered background,
// not the terminal size, since the panel's height depends on content (few
// habits ⇒ a short panel) and a popup taller than that would spill onto or
// past the panel's own border row.
func (m model) openHelp() model {
	bg := m.renderList()
	bgLines := strings.Split(bg, "\n")

	const inset = 1 // stay clear of the background panel's border ring
	safeH := max(6, len(bgLines)-2*inset)
	popH := min(safeH, 22)
	popW := min(76, m.width-2*inset)
	if popW < 40 {
		popW = 40
	}

	// panelStyle overhead: border 1+1, padding(1,2) → 2 rows, 4 cols; -1 more
	// row reserved for the footer (scroll/close hint) below the viewport.
	vp := viewport.New(viewport.WithWidth(popW-6), viewport.WithHeight(popH-5))
	vp.SetContent(m.helpContent())

	m.helpVP = vp
	m.helpPopW = popW
	m.helpPopH = popH
	m.state = viewHelp
	return m
}

// renderHelpPopup renders the help viewport in a bordered box, meant to be
// composited over the list view via overlayCenter rather than replacing the
// whole screen — the list stays visible around it.
func (m model) renderHelpPopup() string {
	footer := "esc / ?  close"
	if m.helpVP.TotalLineCount() > m.helpVP.Height() {
		footer = fmt.Sprintf("j/k scroll (%d%%)  ·  %s", int(m.helpVP.ScrollPercent()*100), footer)
	}
	body := m.helpVP.View() + "\n" + styleMuted.Render(footer)
	return panelStyle.Width(m.helpPopW).Render(body)
}

// renderHabitHeatmap draws a month-labeled, weekday-rowed completion
// calendar for one habit — the same visual language as renderStats' global
// heatmap, just binary (done/not done) instead of 5-level completion-%
// shading, since there's only ever one habit's worth of data here. weeks is
// however many fit ind..ind+width; the caller (renderHabitDetail) derives
// that from the panel's actual width the same way renderStats does, so the
// habit detail view uses the full width the panel already reserves instead
// of leaving it empty next to a narrow fixed-width text column.
func renderHabitHeatmap(dates map[string]bool, weeks, ind int) string {
	if weeks < 1 {
		weeks = 1
	}
	today := truncateDay(time.Now())
	wd := int(today.Weekday())
	daysFromMon := (wd + 6) % 7
	thisMonday := today.AddDate(0, 0, -daysFromMon)
	startDate := thisMonday.AddDate(0, 0, -(weeks-1)*7)
	pad := strings.Repeat(" ", ind)

	var b strings.Builder

	var monthLine strings.Builder
	monthLine.WriteString(pad + "  ")
	lastMonth := -1
	for w := 0; w < weeks; w++ {
		day := startDate.AddDate(0, 0, w*7)
		mo := int(day.Month())
		if mo != lastMonth {
			monthLine.WriteString(styleMuted.Render(day.Format("Jan")[:1]))
			lastMonth = mo
		} else {
			monthLine.WriteString(" ")
		}
		monthLine.WriteString(" ")
	}
	b.WriteString(monthLine.String() + "\n")

	dayLabels := []string{"m", "t", "w", "t", "f", "s", "s"}
	for d := 0; d < 7; d++ {
		var row strings.Builder
		row.WriteString(pad + styleMuted.Render(dayLabels[d]) + " ")
		for w := 0; w < weeks; w++ {
			day := startDate.AddDate(0, 0, w*7+d)
			switch {
			case day.After(today):
				row.WriteString(heatColors[0].Render("░ "))
			case dates[day.Format("2006-01-02")]:
				row.WriteString(heatColors[4].Render("█ "))
			default:
				row.WriteString(heatColors[0].Render("░ "))
			}
		}
		b.WriteString(row.String() + "\n")
	}
	return b.String()
}

// ── renderHabitDetail ─────────────────────────────────────────────────────────

func (m model) renderHabitDetail() string {
	if len(m.habits) == 0 {
		return m.panel(styleMuted.Render("No habit selected."))
	}
	h := m.habits[m.cursor]
	habit := h.Habit

	// Prose (description, notes) stays at a comfortable reading width even
	// on a wide terminal; the heatmap below instead uses the panel's full
	// width, same as renderStats does — that's what actually fills the
	// space a narrow fixed 62-col column used to leave empty.
	maxW := min(m.innerWidth(), 76)
	ind := "  " // section indent

	var b strings.Builder

	// ── title + status ────────────────────────────────────────────────────────
	title := habit.Name
	if habit.Icon != "" {
		title = habit.Icon + "  " + habit.Name
	}
	b.WriteString(sectionHeader("Habit") + "\n")
	b.WriteString(styleLime.Bold(true).Render(title) + "\n")

	switch {
	case h.CheckedToday && h.Streak > 0:
		b.WriteString(styleOk.Render(fmt.Sprintf("✓ today  ·  🔥 %d days", h.Streak)) + "\n")
	case h.CheckedToday:
		b.WriteString(styleOk.Render("✓ done today") + "\n")
	case h.Streak > 0:
		b.WriteString(styleWarn.Render(fmt.Sprintf("not done yet  ·  🔥 %d day streak at risk", h.Streak)) + "\n")
	default:
		b.WriteString(styleMuted.Render("not done today") + "\n")
	}

	// ── description ───────────────────────────────────────────────────────────
	if habit.Description != "" {
		b.WriteString("\n")
		for _, line := range strings.Split(wordWrap(habit.Description, maxW-len(ind)), "\n") {
			if line == "" {
				b.WriteString("\n")
			} else {
				b.WriteString(ind + styleFg.Render(line) + "\n")
			}
		}
	}

	// ── today's note ──────────────────────────────────────────────────────────
	if h.TodayNote != "" {
		b.WriteString("\n")
		b.WriteString(ind + styleMuted.Render("Note today") + "\n")
		for _, line := range strings.Split(wordWrap(h.TodayNote, maxW-len(ind)), "\n") {
			if line != "" {
				b.WriteString(ind + styleFg.Render(line) + "\n")
			}
		}
	}

	// ── recent notes history ──────────────────────────────────────────────────
	if m.recentNotesFor == habit.Name && len(m.recentNotes) > 0 {
		todayStr := time.Now().Format("2006-01-02")
		var pastNotes []models.NoteEntry
		for _, n := range m.recentNotes {
			if n.Date != todayStr {
				pastNotes = append(pastNotes, n)
			}
		}
		if len(pastNotes) > 0 {
			b.WriteString("\n")
			b.WriteString(ind + styleMuted.Render("Past notes") + "\n")
			for _, n := range pastNotes {
				b.WriteString(ind + styleMuted.Render(n.Date+"  ") +
					styleFg.Render(humanize.Truncate(n.Note, maxW-len(ind)-13)) + "\n")
			}
		}
	}

	// ── history ───────────────────────────────────────────────────────────────
	// The multi-week heatmap needs this habit's check-in dates, loaded async
	// on "enter" (see loadHabitCheckins) — until it arrives, fall back to
	// the cheap 7-dot row HabitStats already carries synchronously, so
	// opening a habit never shows a blank gap while the query is in flight.
	b.WriteString("\n")
	if m.detailHeatmapFor == habit.Name {
		weeks := (m.innerWidth() - len(ind) - 2) / 2
		weeks = min(max(weeks, 8), 52)
		b.WriteString(ind + styleMuted.Render(fmt.Sprintf("Last %d weeks", weeks)) + "\n")
		b.WriteString(renderHabitHeatmap(m.detailHeatmap, weeks, len(ind)))
	} else {
		b.WriteString(ind + styleMuted.Render("Last 7 days") + "\n")
		today := truncateDay(time.Now())
		dayAbbrDE := [7]string{"Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"}
		var dayRow, dotRow strings.Builder
		for i := 0; i < 7; i++ {
			d := today.AddDate(0, 0, i-6)
			abbr := fmt.Sprintf("%-4s", dayAbbrDE[int(d.Weekday())])
			if h.Last7Days[i] {
				dayRow.WriteString(styleOk.Render(abbr))
				dotRow.WriteString(styleOkBold.Render("✓   "))
			} else if i == 6 {
				dayRow.WriteString(styleWarn.Render(abbr))
				dotRow.WriteString(styleMuted.Render("·   "))
			} else {
				dayRow.WriteString(styleMuted.Render(abbr))
				dotRow.WriteString(styleMuted.Render("·   "))
			}
		}
		b.WriteString(ind + dayRow.String() + "\n")
		b.WriteString(ind + dotRow.String() + "\n")
	}

	// ── stats ─────────────────────────────────────────────────────────────────
	b.WriteString("\n")
	num := styleLime.Bold(true)
	lbl := styleMuted
	if habit.FreqTarget > 0 {
		b.WriteString(ind + num.Render(fmt.Sprintf("%d/%d", h.WeeklyDone, habit.FreqTarget)) +
			" " + lbl.Render("this week") +
			"   " + num.Render(fmt.Sprintf("%d", h.Streak)) + " " + lbl.Render("week streak") + "\n")
		b.WriteString(ind + lbl.Render(fmt.Sprintf("📅 %d× per week", habit.FreqTarget)) + "\n")
	} else {
		b.WriteString(ind + num.Render(fmt.Sprintf("%d", h.Streak)) + " " + lbl.Render("streak") +
			"   " + num.Render(fmt.Sprintf("%d", h.LongestStreak)) + " " + lbl.Render("longest") +
			"   " + num.Render(fmt.Sprintf("%d", h.TotalDays)) + " " + lbl.Render("days/30") + "\n")
	}
	if habit.SkipAllowed > 0 {
		b.WriteString(ind + lbl.Render(fmt.Sprintf("⏭ %d skip%s allowed", habit.SkipAllowed, func() string {
			if habit.SkipAllowed == 1 {
				return ""
			}
			return "s"
		}())) + "\n")
	}
	if h.LastCheckIn != nil && !h.CheckedToday {
		b.WriteString(ind + lbl.Render("last check-in  "+h.LastCheckIn.Format("Mon, 02 Jan")) + "\n")
	}
	if !habit.CreatedAt.IsZero() {
		days := int(time.Since(habit.CreatedAt).Hours() / 24)
		b.WriteString(ind + lbl.Render(fmt.Sprintf("tracking since  %s  (%d days)", habit.CreatedAt.Format("02 Jan 2006"), days)) + "\n")
	}

	// ── chain ─────────────────────────────────────────────────────────────────
	if h.ChainTo != "" {
		b.WriteString("\n")
		b.WriteString(ind + styleMuted.Render("Next  →  ") + styleFg.Render(h.ChainTo) + "\n")
	}

	// ── footer ────────────────────────────────────────────────────────────────
	b.WriteString("\n")
	if h.CheckedToday {
		b.WriteString(styleMuted.Render("space ✓ · N note · e edit · esc back"))
	} else {
		b.WriteString(styleMuted.Render("space check in · e edit · esc back"))
	}
	return m.panel(b.String())
}

// ── renderReview ──────────────────────────────────────────────────────────────

func (m model) renderReview() string {
	var b strings.Builder
	providerLabel := ""
	if m.reviewBlocked {
		// no call was made — showing a detected provider here would be misleading
	} else if info, err := ai.Detect(); err == nil {
		providerLabel = styleMuted.Render("via " + info.Display)
	} else {
		providerLabel = styleWarn.Render("no provider — S for settings")
	}
	b.WriteString(sectionHeader("Weekly Review") + "  " + providerLabel + "\n\n")

	blinkCursor := "▌"
	if m.blinkOn {
		blinkCursor = " "
	}
	if m.reviewText != "" {
		for _, line := range strings.Split(m.reviewText, "\n") {
			if strings.HasPrefix(line, "## ") {
				b.WriteString("\n" + styleLime.Bold(true).Render(strings.TrimPrefix(line, "## ")) + "\n")
			} else {
				b.WriteString(styleMuted.Render(line) + "\n")
			}
		}
		if !m.reviewDone {
			b.WriteString(styleLime.Render(blinkCursor))
		}
	} else if !m.reviewDone {
		b.WriteString(styleMuted.Render("Analysing last week…") + "\n\n")
		b.WriteString(styleLime.Render(blinkCursor))
	}

	b.WriteString("\n\n" + styleMuted.Render("esc back · r again"))
	return m.panel(b.String())
}

// ── renderNoteInput ───────────────────────────────────────────────────────────

func (m model) renderNoteInput() string {
	var b strings.Builder
	b.WriteString(sectionHeader("Note") + styleMuted.Render(" for "+m.noteForHabit) + "\n\n")
	b.WriteString("  " + m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter save · esc cancel"))
	return m.panel(b.String())
}

// ── renderArchive ─────────────────────────────────────────────────────────────

func (m model) renderArchive() string {
	var b strings.Builder
	b.WriteString(sectionHeader("Archive") + "\n")
	b.WriteString(styleMuted.Render("Archived habits — history is preserved.") + "\n\n")

	if len(m.archivedHabits) == 0 {
		b.WriteString(styleMuted.Render("Archive is empty. a in the list to archive habits.") + "\n\n")
	} else {
		for i, h := range m.archivedHabits {
			cursor := "  "
			ns := styleMuted
			if i == m.archiveCursor {
				cursor = styleLime.Render("▶ ")
				ns = styleFg
			}
			name := h.Name
			if h.Icon != "" {
				name = h.Icon + " " + name
			}
			b.WriteString(cursor + ns.Render(name) + "\n")
			if h.Description != "" {
				b.WriteString("      " + styleMuted.Render(humanize.Truncate(h.Description, 52)) + "\n")
			}
		}
		b.WriteString("\n")
	}

	if m.message != "" {
		msgStyle := styleOk
		if m.isErr {
			msgStyle = styleDanger
		}
		b.WriteString(msgStyle.Render(m.message) + "\n\n")
	}

	b.WriteString(styleMuted.Render("r restore · d delete permanently · j/k navigate · esc back"))
	return m.panel(b.String())
}

func (m model) renderPresets() string {
	var b strings.Builder
	b.WriteString(sectionHeader("Habit Templates") + "\n")
	b.WriteString(styleMuted.Render("A curated starter list — pick one to add it instantly.") + "\n\n")

	for i, p := range habitPresets {
		cursor := "  "
		ns := styleMuted
		if i == m.presetCursor {
			cursor = styleLime.Render("▶ ")
			ns = styleFg
		}
		b.WriteString(cursor + ns.Render(p.Icon+" "+p.Name) + "\n")
		b.WriteString("      " + styleMuted.Render(p.Desc) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(styleMuted.Render("enter add · j/k navigate · esc back"))
	return m.panel(b.String())
}

// ── renderGoalInput ───────────────────────────────────────────────────────────

func (m model) renderGoalInput() string {
	var b strings.Builder
	b.WriteString(sectionHeader("Goal → 3 linked Habits") + "\n\n")
	b.WriteString(styleMuted.Render("AI suggests 3 habits that reinforce each other.") + "\n")
	b.WriteString(styleMuted.Render("Examples: more morning energy · better sleep · more productive") + "\n\n")
	b.WriteString(m.input.View() + "\n\n")
	b.WriteString(styleMuted.Render("enter send · esc back"))
	return m.panel(b.String())
}

// ── renderChainMgr ────────────────────────────────────────────────────────────

func (m model) renderChainMgr() string {
	var b strings.Builder
	b.WriteString(sectionHeader("Habit Chains") + "\n")
	b.WriteString(styleMuted.Render("After habit A, do habit B next.") + "\n\n")

	if len(m.chains) == 0 {
		b.WriteString(styleMuted.Render("No chains yet — press a to add, s for AI suggestions.") + "\n")
	} else {
		for i, ch := range m.chains {
			cursor := "  "
			style := styleMuted
			if i == m.chainCursor {
				cursor = styleLime.Render("▶ ")
				style = styleFg
			}
			b.WriteString(cursor + style.Render(ch.FromName) + styleMuted.Render(" → ") + style.Render(ch.ToName) + "\n")
		}
	}

	b.WriteString("\n" + styleMuted.Render("a add · d delete · u undo · s AI suggestions · esc back"))
	return m.panel(b.String())
}

// ── renderChainPick ───────────────────────────────────────────────────────────

func (m model) renderChainPick() string {
	var b strings.Builder
	if m.chainFromName == "" {
		b.WriteString(sectionHeader("Create Chain") + "\n")
		b.WriteString(styleMuted.Render("Step 1: Which habit comes first?") + "\n\n")
	} else {
		b.WriteString(sectionHeader("Create Chain") + "\n")
		b.WriteString(styleMuted.Render("Step 2: Which habit follows ") +
			styleLime.Render(m.chainFromName) + styleMuted.Render("?") + "\n\n")
	}

	for i, h := range m.habits {
		cursor := "  "
		style := styleMuted
		if i == m.chainPickCursor {
			cursor = styleLime.Render("▶ ")
			style = styleFg
		}
		name := h.Habit.Name
		if h.Habit.Icon != "" {
			name = h.Habit.Icon + " " + name
		}
		if name == m.chainFromName {
			style = styleMuted // can't pick the same one
		}
		b.WriteString(cursor + style.Render(name) + "\n")
	}

	b.WriteString("\n" + styleMuted.Render("enter select · esc back"))
	return m.panel(b.String())
}
