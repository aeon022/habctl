package models

import "time"

// Habit represents a tracked habit.
type Habit struct {
	ID          int64
	Name        string
	Description string
	CreatedAt   time.Time
}

// CheckIn represents a single day's check-in for a habit.
type CheckIn struct {
	ID        int64
	HabitID   int64
	HabitName string
	Date      time.Time // midnight local time (date only)
	CreatedAt time.Time
}

// HabitStats aggregates statistics for a habit over a time window.
type HabitStats struct {
	Habit         Habit
	Streak        int        // current streak (consecutive days ending today)
	LongestStreak int        // longest streak ever
	TotalDays     int        // number of days with a check-in in the last N days
	LastCheckIn   *time.Time // most recent check-in date
	CheckedToday  bool
}
