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
	Icon        string // optional emoji shown before name
	GroupID     int64  // 0 = ungrouped
	CreatedAt   time.Time
}

// CheckIn represents a single day's check-in for a habit.
type CheckIn struct {
	ID        int64
	HabitID   int64
	HabitName string
	Date      time.Time
	CreatedAt time.Time
}

// HabitStats aggregates statistics for a habit over a time window.
type HabitStats struct {
	Habit         Habit
	Streak        int
	LongestStreak int
	TotalDays     int
	LastCheckIn   *time.Time
	CheckedToday  bool
	Last7Days     [7]bool // [0] = 6 days ago, [6] = today
}
