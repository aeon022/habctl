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
	for _, p := range []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA synchronous=NORMAL;",
	} {
		if _, err := s.db.Exec(p); err != nil {
			return fmt.Errorf("pragma: %w", err)
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
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		);
	`)
	if err != nil {
		return err
	}
	return s.runMigrations()
}

func (s *Store) runMigrations() error {
	type migration struct {
		version int
		sql     string
	}
	migrations := []migration{
		{1, `ALTER TABLE habits ADD COLUMN icon TEXT NOT NULL DEFAULT ''`},
		{2, `CREATE TABLE IF NOT EXISTS groups (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			name       TEXT NOT NULL UNIQUE,
			icon       TEXT NOT NULL DEFAULT '',
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)`},
		{3, `ALTER TABLE habits ADD COLUMN group_id INTEGER REFERENCES groups(id) ON DELETE SET NULL`},
	}
	for _, m := range migrations {
		var n int
		s.db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, m.version).Scan(&n)
		if n > 0 {
			continue
		}
		if _, err := s.db.Exec(m.sql); err != nil {
			return fmt.Errorf("migration %d: %w", m.version, err)
		}
		s.db.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, m.version)
	}
	return nil
}

// ── habits ───────────────────────────────────────────────────────────────────

// AddHabit inserts a new habit.
func (s *Store) AddHabit(name, description, icon string) (models.Habit, error) {
	now := time.Now()
	res, err := s.db.Exec(
		`INSERT INTO habits (name, description, icon, created_at) VALUES (?, ?, ?, ?)`,
		name, description, icon, now.Format(tsLayout),
	)
	if err != nil {
		return models.Habit{}, fmt.Errorf("add habit: %w", err)
	}
	id, _ := res.LastInsertId()
	return models.Habit{ID: id, Name: name, Description: description, Icon: icon, CreatedAt: now}, nil
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

// UpdateHabit updates the name, icon, and description of a habit.
func (s *Store) UpdateHabit(oldName, newName, icon, description string) error {
	_, err := s.db.Exec(
		`UPDATE habits SET name = ?, icon = ?, description = ? WHERE name = ?`,
		newName, icon, description, oldName,
	)
	return err
}

// SetHabitGroup assigns a habit to a group (groupID=0 to ungroup).
func (s *Store) SetHabitGroup(habitName string, groupID int64) error {
	var v interface{} = groupID
	if groupID == 0 {
		v = nil
	}
	_, err := s.db.Exec(`UPDATE habits SET group_id = ? WHERE name = ?`, v, habitName)
	return err
}

// ListHabits returns all habits ordered by group sort_order then creation time.
func (s *Store) ListHabits() ([]models.Habit, error) {
	rows, err := s.db.Query(`
		SELECT h.id, h.name, h.description, h.icon, COALESCE(h.group_id, 0), h.created_at
		FROM habits h
		LEFT JOIN groups g ON h.group_id = g.id
		ORDER BY COALESCE(g.sort_order, 999999), h.created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var habits []models.Habit
	for rows.Next() {
		var h models.Habit
		var createdStr string
		if err := rows.Scan(&h.ID, &h.Name, &h.Description, &h.Icon, &h.GroupID, &createdStr); err != nil {
			return nil, err
		}
		h.CreatedAt, _ = time.Parse(tsLayout, createdStr)
		habits = append(habits, h)
	}
	return habits, rows.Err()
}

// ── check-ins ────────────────────────────────────────────────────────────────

// CheckIn records a check-in for the named habit on the given date.
func (s *Store) CheckIn(name string, date time.Time) error {
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
	return err
}

// ── stats ────────────────────────────────────────────────────────────────────

// GetStats returns statistics for a named habit over the last `days` days.
func (s *Store) GetStats(name string, days int) (models.HabitStats, error) {
	var h models.Habit
	var createdStr string
	err := s.db.QueryRow(
		`SELECT id, name, description, icon, COALESCE(group_id,0), created_at FROM habits WHERE name = ?`, name,
	).Scan(&h.ID, &h.Name, &h.Description, &h.Icon, &h.GroupID, &createdStr)
	if err != nil {
		return models.HabitStats{}, fmt.Errorf("habit %q not found", name)
	}
	h.CreatedAt, _ = time.Parse(tsLayout, createdStr)
	return s.computeStats(h, days)
}

// GetAllStats returns statistics for every habit, sorted by group then creation time.
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

func (s *Store) computeStats(h models.Habit, days int) (models.HabitStats, error) {
	rows, err := s.db.Query(
		`SELECT date FROM checkins WHERE habit_id = ? ORDER BY date DESC`, h.ID,
	)
	if err != nil {
		return models.HabitStats{}, err
	}
	defer rows.Close()

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

	streak := 0
	for i := 0; ; i++ {
		if !dateSet[today.AddDate(0, 0, -i).Format(dateLayout)] {
			break
		}
		streak++
	}

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
	if streak > longestStreak {
		longestStreak = streak
	}

	windowStart := today.AddDate(0, 0, -(days - 1))
	totalDays := 0
	for i := 0; i < days; i++ {
		if dateSet[windowStart.AddDate(0, 0, i).Format(dateLayout)] {
			totalDays++
		}
	}

	var lastCheckIn *time.Time
	if len(dates) > 0 {
		t, err := time.ParseInLocation(dateLayout, dates[0], time.Local)
		if err == nil {
			lastCheckIn = &t
		}
	}

	var last7 [7]bool
	for i := 0; i < 7; i++ {
		d := today.AddDate(0, 0, i-6)
		last7[i] = dateSet[d.Format(dateLayout)]
	}

	return models.HabitStats{
		Habit:         h,
		Streak:        streak,
		LongestStreak: longestStreak,
		TotalDays:     totalDays,
		LastCheckIn:   lastCheckIn,
		CheckedToday:  dateSet[todayStr],
		Last7Days:     last7,
	}, nil
}

// ── calendar heatmap ─────────────────────────────────────────────────────────

// CalendarData holds aggregated daily completion counts for stats views.
type CalendarData struct {
	ByDate      map[string]int
	TotalHabits int
}

// GetCalendarData returns per-day completion counts for the past `weeks` weeks.
func (s *Store) GetCalendarData(weeks int) (CalendarData, error) {
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM habits`).Scan(&total); err != nil {
		return CalendarData{}, err
	}
	if total == 0 {
		return CalendarData{ByDate: map[string]int{}, TotalHabits: 0}, nil
	}
	since := truncateToDay(time.Now()).AddDate(0, 0, -weeks*7)
	rows, err := s.db.Query(`
		SELECT date, COUNT(DISTINCT habit_id)
		FROM checkins WHERE date >= ?
		GROUP BY date
	`, since.Format(dateLayout))
	if err != nil {
		return CalendarData{}, err
	}
	defer rows.Close()
	byDate := make(map[string]int)
	for rows.Next() {
		var d string
		var cnt int
		if err := rows.Scan(&d, &cnt); err != nil {
			return CalendarData{}, err
		}
		byDate[d] = cnt
	}
	return CalendarData{ByDate: byDate, TotalHabits: total}, rows.Err()
}

// ── groups ───────────────────────────────────────────────────────────────────

// AddGroup creates a new group.
func (s *Store) AddGroup(name, icon string) (models.Group, error) {
	var maxOrder int
	s.db.QueryRow(`SELECT COALESCE(MAX(sort_order),0) FROM groups`).Scan(&maxOrder)
	now := time.Now()
	res, err := s.db.Exec(
		`INSERT INTO groups (name, icon, sort_order, created_at) VALUES (?, ?, ?, ?)`,
		name, icon, maxOrder+1, now.Format(tsLayout),
	)
	if err != nil {
		return models.Group{}, fmt.Errorf("add group: %w", err)
	}
	id, _ := res.LastInsertId()
	return models.Group{ID: id, Name: name, Icon: icon, SortOrder: maxOrder + 1, CreatedAt: now}, nil
}

// ListGroups returns all groups ordered by sort_order.
func (s *Store) ListGroups() ([]models.Group, error) {
	rows, err := s.db.Query(
		`SELECT id, name, icon, sort_order, created_at FROM groups ORDER BY sort_order ASC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var gs []models.Group
	for rows.Next() {
		var g models.Group
		var ts string
		if err := rows.Scan(&g.ID, &g.Name, &g.Icon, &g.SortOrder, &ts); err != nil {
			return nil, err
		}
		g.CreatedAt, _ = time.Parse(tsLayout, ts)
		gs = append(gs, g)
	}
	return gs, rows.Err()
}

// DeleteGroup removes a group (habits in it become ungrouped).
func (s *Store) DeleteGroup(id int64) error {
	_, err := s.db.Exec(`DELETE FROM groups WHERE id = ?`, id)
	return err
}

// ── util ─────────────────────────────────────────────────────────────────────

func truncateToDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
