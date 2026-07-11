package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aeon022/habctl/internal/models"
	"github.com/aeon022/habctl/internal/store"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Serve starts the MCP server over stdio.
func Serve() error {
	s := server.NewMCPServer("habctl", "0.1.0",
		server.WithToolCapabilities(true),
	)
	s.AddTool(toolCheckHabit(), handleCheckHabit)
	s.AddTool(toolListHabits(), handleListHabits)
	s.AddTool(toolGetHabitStats(), handleGetHabitStats)
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

	// Return streak info.
	stats, err := st.GetStats(name, 30)
	if err != nil {
		return mcp.NewToolResultText(fmt.Sprintf("Checked in: %s", name)), nil
	}

	msg := fmt.Sprintf("Checked in: %s (streak: %d day(s))", name, stats.Streak)
	if stats.Streak >= 7 {
		msg += " 🔥"
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
		return mcp.NewToolResultText("No habits tracked yet. Add one with 'habctl add'."), nil
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
		b.WriteString(fmt.Sprintf("  %s %-20s  streak: %d  last: %s\n",
			check, s.Habit.Name, s.Streak, last))
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

// ── Helpers ───────────────────────────────────────────────────────────────────

func formatStats(s models.HabitStats, days int) string {
	streakStr := fmt.Sprintf("%d", s.Streak)
	if s.Streak >= 7 {
		streakStr += " 🔥"
	}

	bar := progressBar(s.TotalDays, days, 30)
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

func openStore() (*store.Store, error) {
	path, err := store.DefaultPath()
	if err != nil {
		return nil, fmt.Errorf("resolve db path: %w", err)
	}
	return store.Open(path)
}
