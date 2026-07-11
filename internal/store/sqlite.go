package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aeon022/habctl/internal/models"
	_ "modernc.org/sqlite"
)

const dateLayout = "2006-01-02"
const tsLayout = time.RFC3339Nano

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// DefaultPath returns the canonical path to the database file.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "habctl", "habits.db"), nil
}

// Open opens (or creates) the database at path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	s := &Store{db: db}
	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) init() error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA synchronous=NORMAL;",
	}
	for _, p := range pragmas {
		if _, err := s.db.Exec(p); err != nil {
			return fmt.Errorf("pragma %q: %w", p, err)
		}
	}

	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS habits (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			name        TEXT NOT NULL UNIQUE,
			description TEXT NOT NULL DEFAULT '',
			created_at  TEXT NOT NULL
		);

		CREATE TABLE IF NOT EXISTS checkins (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			habit_id   INTEGER NOT NULL REFERENCES habits(id) ON DELETE CASCADE,
			date       TEXT NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(habit_id, date)
		);
	`)
	return err
}

// AddHabit inserts a new habit. Returns an error if the name already exists.
func (s *Store) AddHabit(name, description string) (models.Habit, error) {
	now := time.Now()
	res, err := s.db.Exec(
		`INSERT INTO habits (name, description, created_at) VALUES (?, ?, ?)`,
		name, description, now.Format(tsLayout),
	)
	if err != nil {
		return models.Habit{}, fmt.Errorf("add habit: %w", err)
	}
	id, _ := res.LastInsertId()
	return models.Habit{
		ID:          id,
		Name:        name,
		Description: description,
		CreatedAt:   now,
	}, nil
}

// DeleteHabit removes a habit and all its check-ins by name.
func (s *Store) DeleteHabit(name string) error {
	res, err := s.db.Exec(`DELETE FROM habits WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("delete habit: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("habit %q not found", name)
	}
	return nil
}

// ListHabits returns all habits ordered by creation time.
func (s *Store) ListHabits() ([]models.Habit, error) {
	rows, err := s.db.Query(`SELECT id, name, description, created_at FROM habits ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var habits []models.Habit
	for rows.Next() {
		var h models.Habit
		var createdStr string
		if err := rows.Scan(&h.ID, &h.Name, &h.Description, &createdStr); err != nil {
			return nil, err
		}
		h.CreatedAt, _ = time.Parse(tsLayout, createdStr)
		habits = append(habits, h)
	}
	return habits, rows.Err()
}

// CheckIn records a check-in for the named habit on the given date.
// Uses INSERT OR IGNORE so repeated check-ins on the same day are silently ignored.
func (s *Store) CheckIn(name string, date time.Time) error {
	// Resolve habit ID by name.
	var habitID int64
	err := s.db.QueryRow(`SELECT id FROM habits WHERE name = ?`, name).Scan(&habitID)
	if err != nil {
		return fmt.Errorf("habit %q not found", name)
	}

	dateStr := date.Format(dateLayout)
	now := time.Now()
	_, err = s.db.Exec(
		`INSERT OR IGNORE INTO checkins (habit_id, date, created_at) VALUES (?, ?, ?)`,
		habitID, dateStr, now.Format(tsLayout),
	)
	if err != nil {
		return fmt.Errorf("check-in: %w", err)
	}
	return nil
}

// GetStats returns statistics for a named habit over the last `days` days.
func (s *Store) GetStats(name string, days int) (models.HabitStats, error) {
	var h models.Habit
	var createdStr string
	err := s.db.QueryRow(
		`SELECT id, name, description, created_at FROM habits WHERE name = ?`, name,
	).Scan(&h.ID, &h.Name, &h.Description, &createdStr)
	if err != nil {
		return models.HabitStats{}, fmt.Errorf("habit %q not found", name)
	}
	h.CreatedAt, _ = time.Parse(tsLayout, createdStr)

	return s.computeStats(h, days)
}

// GetAllStats returns statistics for every habit over the last `days` days.
func (s *Store) GetAllStats(days int) ([]models.HabitStats, error) {
	habits, err := s.ListHabits()
	if err != nil {
		return nil, err
	}

	stats := make([]models.HabitStats, 0, len(habits))
	for _, h := range habits {
		st, err := s.computeStats(h, days)
		if err != nil {
			return nil, err
		}
		stats = append(stats, st)
	}
	return stats, nil
}

// computeStats calculates HabitStats for a habit.
func (s *Store) computeStats(h models.Habit, days int) (models.HabitStats, error) {
	// Fetch all check-in dates for this habit, ordered newest first.
	rows, err := s.db.Query(
		`SELECT date FROM checkins WHERE habit_id = ? ORDER BY date DESC`, h.ID,
	)
	if err != nil {
		return models.HabitStats{}, err
	}
	defer rows.Close()

	// Collect all check-in dates as a set (YYYY-MM-DD strings).
	dateSet := make(map[string]bool)
	var dates []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return models.HabitStats{}, err
		}
		if !dateSet[d] {
			dateSet[d] = true
			dates = append(dates, d)
		}
	}
	if err := rows.Err(); err != nil {
		return models.HabitStats{}, err
	}

	today := truncateToDay(time.Now())
	todayStr := today.Format(dateLayout)

	// Current streak: consecutive days ending today.
	streak := 0
	for i := 0; ; i++ {
		day := today.AddDate(0, 0, -i)
		if !dateSet[day.Format(dateLayout)] {
			break
		}
		streak++
	}

	// Longest streak: scan all dates.
	longestStreak := 0
	if len(dates) > 0 {
		cur := 1
		for i := 1; i < len(dates); i++ {
			prev, _ := time.ParseInLocation(dateLayout, dates[i-1], time.Local)
			curr, _ := time.ParseInLocation(dateLayout, dates[i], time.Local)
			if prev.AddDate(0, 0, -1).Equal(curr) {
				cur++
			} else {
				cur = 1
			}
			if cur > longestStreak {
				longestStreak = cur
			}
		}
		if cur > longestStreak {
			longestStreak = cur
		}
		if longestStreak < 1 {
			longestStreak = 1
		}
	}
	// streak can be longer than longestStreak if all dates are from current streak
	if streak > longestStreak {
		longestStreak = streak
	}

	// Total days with a check-in in the last `days` days.
	windowStart := today.AddDate(0, 0, -(days - 1))
	totalDays := 0
	for i := 0; i < days; i++ {
		day := windowStart.AddDate(0, 0, i)
		if dateSet[day.Format(dateLayout)] {
			totalDays++
		}
	}

	// Last check-in.
	var lastCheckIn *time.Time
	if len(dates) > 0 {
		t, err := time.ParseInLocation(dateLayout, dates[0], time.Local)
		if err == nil {
			lastCheckIn = &t
		}
	}

	checkedToday := dateSet[todayStr]

	return models.HabitStats{
		Habit:         h,
		Streak:        streak,
		LongestStreak: longestStreak,
		TotalDays:     totalDays,
		LastCheckIn:   lastCheckIn,
		CheckedToday:  checkedToday,
	}, nil
}

// truncateToDay returns midnight local time for the given time.
func truncateToDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
