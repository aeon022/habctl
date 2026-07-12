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
		{4, `ALTER TABLE checkins ADD COLUMN note TEXT NOT NULL DEFAULT ''`},
		{5, `CREATE TABLE IF NOT EXISTS habit_chains (
			id      INTEGER PRIMARY KEY AUTOINCREMENT,
			from_id INTEGER NOT NULL REFERENCES habits(id) ON DELETE CASCADE,
			to_id   INTEGER NOT NULL REFERENCES habits(id) ON DELETE CASCADE,
			UNIQUE(from_id, to_id)
		)`},
		{6, `ALTER TABLE habits ADD COLUMN freq_target  INTEGER NOT NULL DEFAULT 0`},
		{7, `ALTER TABLE habits ADD COLUMN skip_allowed INTEGER NOT NULL DEFAULT 0`},
		{8, `ALTER TABLE habits ADD COLUMN archived     INTEGER NOT NULL DEFAULT 0`},
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

// ArchiveHabit soft-deletes a habit (keeps history, hides from active list).
func (s *Store) ArchiveHabit(name string) error {
	res, err := s.db.Exec(`UPDATE habits SET archived = 1 WHERE name = ? AND archived = 0`, name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("habit %q not found or already archived", name)
	}
	return nil
}

// UnarchiveHabit restores an archived habit to the active list.
func (s *Store) UnarchiveHabit(name string) error {
	_, err := s.db.Exec(`UPDATE habits SET archived = 0 WHERE name = ?`, name)
	return err
}

// ListArchivedHabits returns all archived habits, newest first.
func (s *Store) ListArchivedHabits() ([]models.Habit, error) {
	rows, err := s.db.Query(`
		SELECT id, name, description, icon, COALESCE(group_id,0),
		       freq_target, skip_allowed, created_at
		FROM habits WHERE archived = 1 ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var habits []models.Habit
	for rows.Next() {
		var h models.Habit
		var ts string
		if err := rows.Scan(&h.ID, &h.Name, &h.Description, &h.Icon,
			&h.GroupID, &h.FreqTarget, &h.SkipAllowed, &ts); err != nil {
			return nil, err
		}
		h.Archived = true
		h.CreatedAt, _ = time.Parse(tsLayout, ts)
		habits = append(habits, h)
	}
	return habits, rows.Err()
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

// ListHabits returns all active (non-archived) habits ordered by group sort_order then creation time.
func (s *Store) ListHabits() ([]models.Habit, error) {
	rows, err := s.db.Query(`
		SELECT h.id, h.name, h.description, h.icon,
		       COALESCE(h.group_id, 0), h.freq_target, h.skip_allowed, h.created_at
		FROM habits h
		LEFT JOIN groups g ON h.group_id = g.id
		WHERE h.archived = 0
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
		if err := rows.Scan(&h.ID, &h.Name, &h.Description, &h.Icon,
			&h.GroupID, &h.FreqTarget, &h.SkipAllowed, &createdStr); err != nil {
			return nil, err
		}
		h.CreatedAt, _ = time.Parse(tsLayout, createdStr)
		habits = append(habits, h)
	}
	return habits, rows.Err()
}

// ── check-ins ────────────────────────────────────────────────────────────────

// CheckIn records a check-in for today. Does not overwrite an existing note.
func (s *Store) CheckIn(name string, date time.Time) error {
	var habitID int64
	if err := s.db.QueryRow(`SELECT id FROM habits WHERE name = ?`, name).Scan(&habitID); err != nil {
		return fmt.Errorf("habit %q not found", name)
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO checkins (habit_id, date, note, created_at) VALUES (?, ?, '', ?)`,
		habitID, date.Format(dateLayout), time.Now().Format(tsLayout),
	)
	return err
}

// DeleteCheckIn removes a check-in for the given habit and date (undo).
func (s *Store) DeleteCheckIn(name string, date time.Time) error {
	var habitID int64
	if err := s.db.QueryRow(`SELECT id FROM habits WHERE name = ?`, name).Scan(&habitID); err != nil {
		return fmt.Errorf("habit %q not found", name)
	}
	_, err := s.db.Exec(
		`DELETE FROM checkins WHERE habit_id = ? AND date = ?`,
		habitID, date.Format(dateLayout),
	)
	return err
}

// SetHabitFreq sets the weekly frequency target (0 = daily, N = N times per week).
func (s *Store) SetHabitFreq(name string, freq int) error {
	_, err := s.db.Exec(`UPDATE habits SET freq_target = ? WHERE name = ?`, freq, name)
	return err
}

// SetHabitSkip sets how many consecutive missed days are allowed before the streak breaks.
func (s *Store) SetHabitSkip(name string, skip int) error {
	_, err := s.db.Exec(`UPDATE habits SET skip_allowed = ? WHERE name = ?`, skip, name)
	return err
}

// CheckInWithNote records a check-in and attaches an optional coaching note.
func (s *Store) CheckInWithNote(name string, date time.Time, note string) error {
	var habitID int64
	err := s.db.QueryRow(`SELECT id FROM habits WHERE name = ?`, name).Scan(&habitID)
	if err != nil {
		return fmt.Errorf("habit %q not found", name)
	}
	dateStr := date.Format(dateLayout)
	now := time.Now()
	_, err = s.db.Exec(
		`INSERT INTO checkins (habit_id, date, note, created_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(habit_id, date) DO UPDATE SET note = excluded.note`,
		habitID, dateStr, note, now.Format(tsLayout),
	)
	return err
}

// GetRecentNotes returns up to limit non-empty notes for a habit, newest first.
func (s *Store) GetRecentNotes(habitName string, limit int) ([]models.NoteEntry, error) {
	rows, err := s.db.Query(`
		SELECT c.date, c.note FROM checkins c
		JOIN habits h ON h.id = c.habit_id
		WHERE h.name = ? AND c.note != ''
		ORDER BY c.date DESC LIMIT ?
	`, habitName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.NoteEntry
	for rows.Next() {
		var e models.NoteEntry
		if err := rows.Scan(&e.Date, &e.Note); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ── stats ────────────────────────────────────────────────────────────────────

// GetStats returns statistics for a named habit over the last `days` days.
func (s *Store) GetStats(name string, days int) (models.HabitStats, error) {
	var h models.Habit
	var createdStr string
	err := s.db.QueryRow(
		`SELECT id, name, description, icon, COALESCE(group_id,0), freq_target, skip_allowed, created_at
		 FROM habits WHERE name = ?`, name,
	).Scan(&h.ID, &h.Name, &h.Description, &h.Icon, &h.GroupID, &h.FreqTarget, &h.SkipAllowed, &createdStr)
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

	// ── monday helper ─────────────────────────────────────────────────────────
	mondayOf := func(t time.Time) time.Time {
		wd := int(t.Weekday())
		if wd == 0 {
			wd = 7
		}
		return truncateToDay(t.AddDate(0, 0, -(wd - 1)))
	}

	// ── streak ────────────────────────────────────────────────────────────────
	var streak, longestStreak, weeklyDone int
	var checkedToday bool

	if h.FreqTarget > 0 {
		// weekly habit: streak = consecutive weeks meeting target
		thisMonday := mondayOf(today)
		for d := thisMonday; !d.After(today); d = d.AddDate(0, 0, 1) {
			if dateSet[d.Format(dateLayout)] {
				weeklyDone++
			}
		}
		checkedToday = weeklyDone >= h.FreqTarget
		if checkedToday {
			streak = 1
		}
		for w := 1; ; w++ {
			wStart := thisMonday.AddDate(0, 0, -w*7)
			cnt := 0
			for d := wStart; d.Before(wStart.AddDate(0, 0, 7)); d = d.AddDate(0, 0, 1) {
				if dateSet[d.Format(dateLayout)] {
					cnt++
				}
			}
			if cnt < h.FreqTarget {
				break
			}
			streak++
		}
		longestStreak = streak // simplified: longest = current for now
	} else {
		// daily habit (with optional skip forgiveness)
		checkedToday = dateSet[todayStr]
		consecutiveMisses := 0
		for i := 0; ; i++ {
			if dateSet[today.AddDate(0, 0, -i).Format(dateLayout)] {
				streak++
				consecutiveMisses = 0
			} else {
				consecutiveMisses++
				if consecutiveMisses > h.SkipAllowed {
					break
				}
			}
		}
		// longest streak (no skip logic for simplicity)
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
	}

	// ── totals ────────────────────────────────────────────────────────────────
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

	// today's note (empty string if none or not checked in)
	var todayNote string
	s.db.QueryRow(
		`SELECT note FROM checkins WHERE habit_id = ? AND date = ?`, h.ID, todayStr,
	).Scan(&todayNote)

	// chain: find the first chained follow-up habit name
	var chainTo string
	s.db.QueryRow(`
		SELECT h2.name FROM habit_chains ch
		JOIN habits h2 ON h2.id = ch.to_id
		WHERE ch.from_id = ? LIMIT 1
	`, h.ID).Scan(&chainTo)

	return models.HabitStats{
		Habit:         h,
		Streak:        streak,
		LongestStreak: longestStreak,
		TotalDays:     totalDays,
		LastCheckIn:   lastCheckIn,
		CheckedToday:  checkedToday,
		Last7Days:     last7,
		ChainTo:       chainTo,
		TodayNote:     todayNote,
		WeeklyDone:    weeklyDone,
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

// ── chains ───────────────────────────────────────────────────────────────────

// AddChain creates a from→to habit chain.
func (s *Store) AddChain(fromName, toName string) error {
	var fromID, toID int64
	if err := s.db.QueryRow(`SELECT id FROM habits WHERE name = ?`, fromName).Scan(&fromID); err != nil {
		return fmt.Errorf("habit %q not found", fromName)
	}
	if err := s.db.QueryRow(`SELECT id FROM habits WHERE name = ?`, toName).Scan(&toID); err != nil {
		return fmt.Errorf("habit %q not found", toName)
	}
	if fromID == toID {
		return fmt.Errorf("ein Habit kann nicht auf sich selbst zeigen")
	}
	_, err := s.db.Exec(`INSERT OR IGNORE INTO habit_chains (from_id, to_id) VALUES (?, ?)`, fromID, toID)
	return err
}

// ListChains returns all chains with resolved habit names.
func (s *Store) ListChains() ([]models.Chain, error) {
	rows, err := s.db.Query(`
		SELECT ch.id, ch.from_id, ch.to_id, h1.name, h2.name
		FROM habit_chains ch
		JOIN habits h1 ON h1.id = ch.from_id
		JOIN habits h2 ON h2.id = ch.to_id
		ORDER BY h1.name, h2.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.Chain
	for rows.Next() {
		var c models.Chain
		if err := rows.Scan(&c.ID, &c.FromID, &c.ToID, &c.FromName, &c.ToName); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteChain removes a chain by ID.
func (s *Store) DeleteChain(id int64) error {
	_, err := s.db.Exec(`DELETE FROM habit_chains WHERE id = ?`, id)
	return err
}

// ── weekly review ─────────────────────────────────────────────────────────────

// GetWeeklyReview aggregates per-habit stats for the AI coaching briefing.
func (s *Store) GetWeeklyReview() (models.WeeklyReview, error) {
	habits, err := s.ListHabits()
	if err != nil {
		return models.WeeklyReview{}, err
	}
	today := truncateToDay(time.Now())

	// count perfect days this week (all habits checked in)
	total := len(habits)
	perfectDays := 0
	if total > 0 {
		for i := 0; i < 7; i++ {
			d := today.AddDate(0, 0, i-6).Format(dateLayout)
			var cnt int
			s.db.QueryRow(
				`SELECT COUNT(DISTINCT habit_id) FROM checkins WHERE date = ?`, d,
			).Scan(&cnt)
			if cnt >= total {
				perfectDays++
			}
		}
	}

	var habitData []models.HabitWeekData
	for _, h := range habits {
		st, err := s.computeStats(h, 30)
		if err != nil {
			continue
		}
		done7 := 0
		for _, v := range st.Last7Days {
			if v {
				done7++
			}
		}
		pct7 := 0.0
		if done7 > 0 {
			pct7 = float64(done7) / 7.0
		}
		pct30 := 0.0
		if st.TotalDays > 0 {
			pct30 = float64(st.TotalDays) / 30.0
		}
		notes, _ := s.GetRecentNotes(h.Name, 3)
		habitData = append(habitData, models.HabitWeekData{
			Name:            h.Name,
			Icon:            h.Icon,
			DoneThisWeek:    done7,
			DoneLast30:      st.TotalDays,
			CompletionPct7:  pct7,
			CompletionPct30: pct30,
			CurrentStreak:   st.Streak,
			RecentNotes:     notes,
		})
	}
	// ── weekday completion stats (last 30 days) ───────────────────────────────
	weakestDay, strongestDay := "", ""
	if len(habits) > 0 {
		since30 := today.AddDate(0, 0, -29).Format(dateLayout)
		var dowDone [7]int
		drows, derr := s.db.Query(`
			SELECT strftime('%w', ci.date), COUNT(DISTINCT ci.habit_id)
			FROM checkins ci
			JOIN habits h ON h.id = ci.habit_id
			WHERE ci.date >= ? AND h.archived = 0
			GROUP BY strftime('%w', ci.date)
		`, since30)
		if derr == nil {
			defer drows.Close()
			for drows.Next() {
				var dow string
				var cnt int
				if drows.Scan(&dow, &cnt) == nil && len(dow) > 0 {
					d := int(dow[0] - '0')
					if d >= 0 && d < 7 {
						dowDone[d] += cnt
					}
				}
			}
		}
		var dowTotal [7]int
		for i := 0; i < 30; i++ {
			wd := int(today.AddDate(0, 0, -i).Weekday())
			dowTotal[wd] += len(habits)
		}
		weekdayDE := [7]string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"}
		weakestPct, strongestPct := 2.0, -1.0
		weakestIdx, strongestIdx := -1, -1
		for i := 0; i < 7; i++ {
			if dowTotal[i] == 0 {
				continue
			}
			pct := float64(dowDone[i]) / float64(dowTotal[i])
			if pct < weakestPct {
				weakestPct = pct
				weakestIdx = i
			}
			if pct > strongestPct {
				strongestPct = pct
				strongestIdx = i
			}
		}
		if weakestIdx >= 0 {
			weakestDay = fmt.Sprintf("%s (%.0f%%)", weekdayDE[weakestIdx], weakestPct*100)
		}
		if strongestIdx >= 0 && strongestIdx != weakestIdx {
			strongestDay = fmt.Sprintf("%s (%.0f%%)", weekdayDE[strongestIdx], strongestPct*100)
		}
	}

	return models.WeeklyReview{
		Habits:       habitData,
		PerfectDays:  perfectDays,
		WeakestDay:   weakestDay,
		StrongestDay: strongestDay,
	}, nil
}

// ── util ─────────────────────────────────────────────────────────────────────

func truncateToDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}
