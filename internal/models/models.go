package models

import "time"

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
	Habits      []HabitWeekData
	PerfectDays int // days this week where every habit was checked in
}
