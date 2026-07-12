package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aeon022/habctl/internal/ai"
	"github.com/aeon022/habctl/internal/models"
	"github.com/aeon022/habctl/internal/store"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Serve starts the MCP server over stdio.
func Serve() error {
	s := server.NewMCPServer("habctl", "0.2.0",
		server.WithToolCapabilities(true),
	)
	s.AddTool(toolCheckHabit(), handleCheckHabit)
	s.AddTool(toolListHabits(), handleListHabits)
	s.AddTool(toolGetHabitStats(), handleGetHabitStats)
	s.AddTool(toolAddHabit(), handleAddHabit)
	s.AddTool(toolDeleteHabit(), handleDeleteHabit)
	s.AddTool(toolStreakAtRisk(), handleStreakAtRisk)
	s.AddTool(toolWeeklySummary(), handleWeeklySummary)
	s.AddTool(toolSuggestHabits(), handleSuggestHabits)
	s.AddTool(toolAddCheckinNote(), handleAddCheckinNote)
	s.AddTool(toolUncheckHabit(), handleUncheckHabit)
	s.AddTool(toolListChains(), handleListChains)
	s.AddTool(toolGetWeeklyReview(), handleGetWeeklyReview)
	return server.ServeStdio(s)
}

// ── Tool definitions ──────────────────────────────────────────────────────────

func toolCheckHabit() mcp.Tool {
	return mcp.NewTool("check_habit",
		mcp.WithDescription("Check in a habit for today (or a specific date). Use this to mark a habit as done."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Habit name")),
		mcp.WithString("date", mcp.Description("Date in YYYY-MM-DD format (defaults to today)")),
	)
}

func toolListHabits() mcp.Tool {
	return mcp.NewTool("list_habits",
		mcp.WithDescription("List all habits with their current streak and today's check-in status."),
	)
}

func toolGetHabitStats() mcp.Tool {
	return mcp.NewTool("get_habit_stats",
		mcp.WithDescription("Get detailed statistics for one or all habits over a time window."),
		mcp.WithString("name", mcp.Description("Habit name (optional — omit for all habits)")),
		mcp.WithNumber("days", mcp.Description("Number of days to look back (default: 30)")),
	)
}

func toolAddHabit() mcp.Tool {
	return mcp.NewTool("add_habit",
		mcp.WithDescription("Add a new habit to track. Use this when the user wants to start tracking a new habit."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Habit name, e.g. '20 Min Laufen'")),
		mcp.WithString("description", mcp.Description("Optional description or motivation")),
	)
}

func toolDeleteHabit() mcp.Tool {
	return mcp.NewTool("delete_habit",
		mcp.WithDescription("Delete a habit and all its check-in history. This is permanent — confirm with the user before calling."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Exact habit name to delete")),
	)
}

func toolStreakAtRisk() mcp.Tool {
	return mcp.NewTool("streak_at_risk",
		mcp.WithDescription("List habits that have not been checked in today but have an active streak. Use this for end-of-day reminders or to nudge the user."),
	)
}

func toolWeeklySummary() mcp.Tool {
	return mcp.NewTool("get_weekly_summary",
		mcp.WithDescription("Get a summary of habit completion for the last 7 days. Useful for weekly briefings and reflection."),
	)
}

func toolSuggestHabits() mcp.Tool {
	return mcp.NewTool("suggest_habits",
		mcp.WithDescription("Use AI to suggest new habits based on the user's goals and existing habits. Good for onboarding or when the user asks for ideas."),
		mcp.WithString("routine", mcp.Description("Focus area: morning, evening, health, learning, productivity (optional)")),
		mcp.WithString("goal", mcp.Description("User's goal in their own words, e.g. 'mehr Energie am Morgen'")),
	)
}

func toolAddCheckinNote() mcp.Tool {
	return mcp.NewTool("add_checkin_note",
		mcp.WithDescription("Add or update a text note on today's (or a specific date's) check-in for a habit. The habit must already be checked in for that date."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Habit name")),
		mcp.WithString("note", mcp.Required(), mcp.Description("The note text (pass empty string to clear)")),
		mcp.WithString("date", mcp.Description("Date in YYYY-MM-DD format (defaults to today)")),
	)
}

func toolUncheckHabit() mcp.Tool {
	return mcp.NewTool("uncheck_habit",
		mcp.WithDescription("Remove a check-in for a habit (undo). Use this to correct accidental check-ins."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Habit name")),
		mcp.WithString("date", mcp.Description("Date in YYYY-MM-DD format (defaults to today)")),
	)
}

func toolListChains() mcp.Tool {
	return mcp.NewTool("list_chains",
		mcp.WithDescription("List all habit chains — pairs of habits where completing the first suggests doing the second immediately after."),
	)
}

func toolGetWeeklyReview() mcp.Tool {
	return mcp.NewTool("get_weekly_review",
		mcp.WithDescription("Get detailed per-habit data for the last 7 days and 30 days, including completion rates and recent notes. Use this as input for a coaching briefing."),
	)
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func handleCheckHabit(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := req.GetString("name", "")
	dateStr := req.GetString("date", "")

	if name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}

	date := time.Now()
	if dateStr != "" {
		parsed, err := time.ParseInLocation("2006-01-02", dateStr, time.Local)
		if err != nil {
			return mcp.NewToolResultError("invalid date format, expected YYYY-MM-DD"), nil
		}
		date = parsed
	}

	st, err := openStore()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer st.Close()

	if err := st.CheckIn(name, date); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	stats, err := st.GetStats(name, 30)
	if err != nil {
		return mcp.NewToolResultText(fmt.Sprintf("Checked in: %s", name)), nil
	}

	msg := fmt.Sprintf("Checked in: %s (streak: %d day(s))", name, stats.Streak)
	if stats.Streak >= 7 {
		msg += " 🔥"
	}
	if ms := streakMilestone(stats.Streak); ms != "" {
		msg += " " + ms
	}
	return mcp.NewToolResultText(msg), nil
}

func handleListHabits(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	st, err := openStore()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer st.Close()

	allStats, err := st.GetAllStats(30)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if len(allStats) == 0 {
		return mcp.NewToolResultText("No habits tracked yet. Use add_habit to get started."), nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Habits (%d):\n\n", len(allStats)))
	for _, s := range allStats {
		check := "✗"
		if s.CheckedToday {
			check = "✓"
		}
		last := "never"
		if s.LastCheckIn != nil {
			last = s.LastCheckIn.Format("2006-01-02")
		}
		streakStr := fmt.Sprintf("%d", s.Streak)
		if s.Streak >= 7 {
			streakStr += " 🔥"
		}
		b.WriteString(fmt.Sprintf("  %s %-24s  streak: %-8s  last: %s\n",
			check, s.Habit.Name, streakStr, last))
	}
	return mcp.NewToolResultText(b.String()), nil
}

func handleGetHabitStats(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := req.GetString("name", "")
	days := int(req.GetFloat("days", 30))
	if days <= 0 {
		days = 30
	}

	st, err := openStore()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer st.Close()

	var b strings.Builder

	if name != "" {
		stats, err := st.GetStats(name, days)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		b.WriteString(formatStats(stats, days))
	} else {
		allStats, err := st.GetAllStats(days)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if len(allStats) == 0 {
			return mcp.NewToolResultText("No habits tracked yet."), nil
		}
		b.WriteString(fmt.Sprintf("Habit Stats — last %d days\n\n", days))
		for _, s := range allStats {
			b.WriteString(formatStats(s, days))
			b.WriteString("\n")
		}
	}

	return mcp.NewToolResultText(b.String()), nil
}

func handleAddHabit(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := req.GetString("name", "")
	desc := req.GetString("description", "")

	if name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}

	st, err := openStore()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer st.Close()

	if _, err := st.AddHabit(name, desc, ""); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Added habit: %q — start checking in with check_habit.", name)), nil
}

func handleDeleteHabit(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := req.GetString("name", "")
	if name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}

	st, err := openStore()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer st.Close()

	if err := st.DeleteHabit(name); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Deleted habit %q and all its check-in history.", name)), nil
}

func handleStreakAtRisk(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	st, err := openStore()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer st.Close()

	allStats, err := st.GetAllStats(30)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var atRisk []models.HabitStats
	var noStreak []models.HabitStats
	for _, s := range allStats {
		if s.CheckedToday {
			continue
		}
		if s.Streak > 0 {
			atRisk = append(atRisk, s)
		} else {
			noStreak = append(noStreak, s)
		}
	}

	if len(atRisk) == 0 && len(noStreak) == 0 {
		return mcp.NewToolResultText("All habits checked in today! 🎉"), nil
	}

	var b strings.Builder
	if len(atRisk) > 0 {
		b.WriteString(fmt.Sprintf("⚠ Streaks at risk (%d):\n", len(atRisk)))
		for _, s := range atRisk {
			b.WriteString(fmt.Sprintf("  - %-24s  streak: %d day(s)\n", s.Habit.Name, s.Streak))
		}
	}
	if len(noStreak) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(fmt.Sprintf("Pending (no active streak, %d):\n", len(noStreak)))
		for _, s := range noStreak {
			b.WriteString(fmt.Sprintf("  - %s\n", s.Habit.Name))
		}
	}

	return mcp.NewToolResultText(b.String()), nil
}

func handleWeeklySummary(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	st, err := openStore()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer st.Close()

	allStats, err := st.GetAllStats(7)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if len(allStats) == 0 {
		return mcp.NewToolResultText("No habits tracked yet."), nil
	}

	var b strings.Builder
	b.WriteString("Weekly Summary — last 7 days\n\n")

	for _, s := range allStats {
		pct := 0
		if s.TotalDays > 0 {
			pct = s.TotalDays * 100 / 7
		}
		bar := progressBar(s.TotalDays, 7, 14)
		streakStr := fmt.Sprintf("%d", s.Streak)
		if s.Streak >= 7 {
			streakStr += " 🔥"
		}
		b.WriteString(fmt.Sprintf("  %-22s  %s %d/7 (%d%%)  streak: %s\n",
			s.Habit.Name, bar, s.TotalDays, pct, streakStr))
	}

	return mcp.NewToolResultText(b.String()), nil
}

func handleSuggestHabits(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	routine := req.GetString("routine", "")
	goal := req.GetString("goal", "")

	st, err := openStore()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer st.Close()

	allStats, err := st.GetAllStats(30)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	var existing []string
	for _, s := range allStats {
		existing = append(existing, s.Habit.Name)
	}

	result, err := ai.SuggestBlocking(ai.SuggestRequest{
		ExistingHabits: existing,
		Routine:        routine,
		Goal:           goal,
		Count:          6,
	})
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("AI suggestion failed: %v", err)), nil
	}

	return mcp.NewToolResultText(result), nil
}

func handleAddCheckinNote(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := req.GetString("name", "")
	note := req.GetString("note", "")
	dateStr := req.GetString("date", "")

	if name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}

	date := time.Now()
	if dateStr != "" {
		parsed, err := time.ParseInLocation("2006-01-02", dateStr, time.Local)
		if err != nil {
			return mcp.NewToolResultError("invalid date format, expected YYYY-MM-DD"), nil
		}
		date = parsed
	}

	st, err := openStore()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer st.Close()

	if err := st.CheckInWithNote(name, date, note); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if note == "" {
		return mcp.NewToolResultText(fmt.Sprintf("Note cleared for %s on %s.", name, date.Format("2006-01-02"))), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("Note saved for %s: %q", name, note)), nil
}

func handleUncheckHabit(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	name := req.GetString("name", "")
	dateStr := req.GetString("date", "")

	if name == "" {
		return mcp.NewToolResultError("name is required"), nil
	}

	date := time.Now()
	if dateStr != "" {
		parsed, err := time.ParseInLocation("2006-01-02", dateStr, time.Local)
		if err != nil {
			return mcp.NewToolResultError("invalid date format, expected YYYY-MM-DD"), nil
		}
		date = parsed
	}

	st, err := openStore()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer st.Close()

	if err := st.DeleteCheckIn(name, date); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Removed check-in for %s on %s.", name, date.Format("2006-01-02"))), nil
}

func handleListChains(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	st, err := openStore()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer st.Close()

	chains, err := st.ListChains()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if len(chains) == 0 {
		return mcp.NewToolResultText("No habit chains defined. Use the TUI (c key) to create chains."), nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Habit Chains (%d):\n\n", len(chains)))
	for _, ch := range chains {
		b.WriteString(fmt.Sprintf("  %s → %s\n", ch.FromName, ch.ToName))
	}
	return mcp.NewToolResultText(b.String()), nil
}

func handleGetWeeklyReview(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	st, err := openStore()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer st.Close()

	data, err := st.GetWeeklyReview()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	if len(data.Habits) == 0 {
		return mcp.NewToolResultText("No habits tracked yet."), nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Weekly Review — %d habits  ·  %d perfect days this week\n\n", len(data.Habits), data.PerfectDays))
	for _, h := range data.Habits {
		b.WriteString(fmt.Sprintf("%s %s\n", h.Icon, h.Name))
		b.WriteString(fmt.Sprintf("  7d: %d check-ins (%.0f%%)  |  30d: %d check-ins (%.0f%%)  |  streak: %d\n",
			h.DoneThisWeek, h.CompletionPct7*100,
			h.DoneLast30, h.CompletionPct30*100,
			h.CurrentStreak))
		for _, n := range h.RecentNotes {
			b.WriteString(fmt.Sprintf("  [%s] %s\n", n.Date, n.Note))
		}
		b.WriteString("\n")
	}
	return mcp.NewToolResultText(b.String()), nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func formatStats(s models.HabitStats, days int) string {
	streakStr := fmt.Sprintf("%d", s.Streak)
	if s.Streak >= 7 {
		streakStr += " 🔥"
	}
	bar := progressBar(s.TotalDays, days, 20)
	return fmt.Sprintf("%s (streak: %s)\n%s %d/%d days\n",
		s.Habit.Name, streakStr, bar, s.TotalDays, days)
}

func progressBar(done, total, width int) string {
	if total == 0 {
		return "[" + strings.Repeat("░", width) + "]"
	}
	filled := (done * width) / total
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

func streakMilestone(n int) string {
	switch n {
	case 7:
		return "🎯 One week!"
	case 14:
		return "💪 Two weeks!"
	case 21:
		return "🧠 21 days!"
	case 30:
		return "🏆 One month!"
	case 60:
		return "🌟 60 days!"
	case 100:
		return "🎉 100 days!!"
	}
	return ""
}

func openStore() (*store.Store, error) {
	path, err := store.DefaultPath()
	if err != nil {
		return nil, fmt.Errorf("resolve db path: %w", err)
	}
	return store.Open(path)
}
