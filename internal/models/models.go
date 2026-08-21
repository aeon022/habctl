package models

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Group is a named category that habits can be assigned to.
type Group struct {
	ID        int64
	Name      string
	Icon      string
	SortOrder int
	CreatedAt time.Time
}

// Habit represents a tracked habit.
type Habit struct {
	ID          int64
	Name        string
	Description string
	Icon        string
	GroupID     int64
	FreqTarget  int // 0 = daily, N = N times per week
	SkipAllowed int // consecutive missed days before streak breaks (0 = none)
	Archived    bool
	CreatedAt   time.Time
}

// CheckIn records that a habit was completed on a specific date.
type CheckIn struct {
	ID        int64
	HabitID   int64
	HabitName string
	Date      time.Time
	Note      string
	CreatedAt time.Time
}

// NoteEntry is a lightweight (date, note) pair returned by GetRecentNotes.
type NoteEntry struct {
	Date string
	Note string
}

// Chain links two habits: completing FromName suggests doing ToName next.
type Chain struct {
	ID       int64
	FromID   int64
	ToID     int64
	FromName string
	ToName   string
}

// HabitStats aggregates statistics for a habit over a time window.
type HabitStats struct {
	Habit         Habit
	Streak        int
	LongestStreak int
	TotalDays     int
	LastCheckIn   *time.Time
	CheckedToday  bool   // for weekly habits: true when week's target is met
	Last7Days     [7]bool
	ChainTo       string // name of chained follow-up habit (empty if none)
	TodayNote     string // note for today's check-in (empty if none)
	WeeklyDone    int    // check-ins this week (only meaningful when FreqTarget > 0)
}

// HabitWeekData is one habit's contribution to a WeeklyReview.
type HabitWeekData struct {
	Name            string
	Icon            string
	DoneThisWeek    int
	DoneLast30      int
	CompletionPct7  float64 // 0–1
	CompletionPct30 float64 // 0–1
	CurrentStreak   int
	RecentNotes     []NoteEntry // up to 3 most recent notes with text
}

// WeeklyReview aggregates per-habit data for the AI coaching briefing.
type WeeklyReview struct {
	Habits       []HabitWeekData
	PerfectDays  int    // days this week where every habit was checked in
	WeakestDay   string // e.g. "Mittwoch (23%)" — lowest completion weekday (30d)
	StrongestDay string // e.g. "Montag (87%)" — highest completion weekday (30d)
}

// ParseDateArg parses a YYYY-MM-DD date string, defaulting to time.Now()
// when s is empty. Shared by every check-in/uncheck/note command (CLI, MCP,
// TUI) that takes an optional date flag or argument.
func ParseDateArg(s string) (time.Time, error) {
	if s == "" {
		return time.Now(), nil
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q — expected YYYY-MM-DD", s)
	}
	return t, nil
}

// StreakMilestone returns a short celebratory message for round-number
// streak lengths, or "" for any other streak. Shared by cmd/check.go, the
// MCP server, and the TUI so the three don't drift out of sync on which
// streaks are celebrated.
func StreakMilestone(streak int) string {
	switch streak {
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

// ProgressBar renders a simple block-character progress bar of the given
// width, e.g. "[████░░░░░░]". done/total may be 0; a zero total renders an
// empty bar rather than dividing by zero.
//
// Rounds with math.Round rather than truncating integer division, so e.g.
// 1/3 of a width-10 bar renders 3 filled blocks, not 3 from truncation of
// 3.33 — the difference only shows up near a .5 boundary (e.g. 1/8 of a
// width-12 bar rounds up to 2 filled blocks instead of truncating to 1).
func ProgressBar(done, total, width int) string {
	if total == 0 {
		return "[" + strings.Repeat("░", width) + "]"
	}
	filled := int(math.Round(float64(done) / float64(total) * float64(width)))
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}
