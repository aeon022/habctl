package store

import (
	"path/filepath"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "habits.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func day(offset int) time.Time {
	return truncateToDay(time.Now()).AddDate(0, 0, offset)
}

func TestHabitCRUD(t *testing.T) {
	s := testStore(t)

	if _, err := s.AddHabit("Meditate", "10 minutes", "🧘"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddHabit("Meditate", "", ""); err == nil {
		t.Fatal("want error for duplicate habit name")
	}

	habits, err := s.ListHabits()
	if err != nil {
		t.Fatal(err)
	}
	if len(habits) != 1 || habits[0].Name != "Meditate" {
		t.Fatalf("unexpected habits: %+v", habits)
	}

	if err := s.UpdateHabit("Meditate", "Meditation", "🧘", "morning"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteHabit("Meditation"); err != nil {
		t.Fatal(err)
	}
	habits, _ = s.ListHabits()
	if len(habits) != 0 {
		t.Fatalf("habit not deleted: %+v", habits)
	}
}

func TestCheckInIdempotent(t *testing.T) {
	s := testStore(t)
	if _, err := s.AddHabit("Read", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckIn("Read", day(0)); err != nil {
		t.Fatal(err)
	}
	// second check-in on the same day must not create a second row / error
	if err := s.CheckIn("Read", day(0)); err != nil {
		t.Fatalf("duplicate check-in: %v", err)
	}
	st, err := s.GetStats("Read", 30)
	if err != nil {
		t.Fatal(err)
	}
	if st.TotalDays != 1 {
		t.Errorf("want TotalDays 1, got %d", st.TotalDays)
	}
	if !st.CheckedToday {
		t.Error("want CheckedToday true")
	}
}

func TestDailyStreak(t *testing.T) {
	s := testStore(t)
	if _, err := s.AddHabit("Run", "", ""); err != nil {
		t.Fatal(err)
	}
	// three consecutive days ending today
	for _, off := range []int{-2, -1, 0} {
		if err := s.CheckIn("Run", day(off)); err != nil {
			t.Fatal(err)
		}
	}
	st, err := s.GetStats("Run", 30)
	if err != nil {
		t.Fatal(err)
	}
	if st.Streak != 3 {
		t.Errorf("want streak 3, got %d", st.Streak)
	}
	if st.LongestStreak < 3 {
		t.Errorf("want longest >= 3, got %d", st.LongestStreak)
	}
}

func TestStreakBreaksOnMiss(t *testing.T) {
	s := testStore(t)
	if _, err := s.AddHabit("Write", "", ""); err != nil {
		t.Fatal(err)
	}
	// 5-day streak in the past, then a gap of 2 days, then today
	for _, off := range []int{-8, -7, -6, -5, -4, 0} {
		if err := s.CheckIn("Write", day(off)); err != nil {
			t.Fatal(err)
		}
	}
	st, err := s.GetStats("Write", 30)
	if err != nil {
		t.Fatal(err)
	}
	if st.Streak != 1 {
		t.Errorf("want current streak 1 (gap breaks it), got %d", st.Streak)
	}
	if st.LongestStreak != 5 {
		t.Errorf("want longest streak 5, got %d", st.LongestStreak)
	}
}

func TestStreakSkipForgiveness(t *testing.T) {
	s := testStore(t)
	if _, err := s.AddHabit("Yoga", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetHabitSkip("Yoga", 1); err != nil {
		t.Fatal(err)
	}
	// checked today and 2 days ago; yesterday missed — 1 skip allowed keeps it alive
	for _, off := range []int{-2, 0} {
		if err := s.CheckIn("Yoga", day(off)); err != nil {
			t.Fatal(err)
		}
	}
	st, err := s.GetStats("Yoga", 30)
	if err != nil {
		t.Fatal(err)
	}
	if st.Streak != 2 {
		t.Errorf("want streak 2 with skip forgiveness, got %d", st.Streak)
	}
}

func TestUncheckReducesStreak(t *testing.T) {
	s := testStore(t)
	if _, err := s.AddHabit("Stretch", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckIn("Stretch", day(0)); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteCheckIn("Stretch", day(0)); err != nil {
		t.Fatal(err)
	}
	st, err := s.GetStats("Stretch", 30)
	if err != nil {
		t.Fatal(err)
	}
	if st.Streak != 0 || st.CheckedToday {
		t.Errorf("want streak 0 / not checked, got %d / %v", st.Streak, st.CheckedToday)
	}
}

func TestArchiveHidesHabit(t *testing.T) {
	s := testStore(t)
	if _, err := s.AddHabit("Old", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.ArchiveHabit("Old"); err != nil {
		t.Fatal(err)
	}
	habits, _ := s.ListHabits()
	if len(habits) != 0 {
		t.Fatalf("archived habit still listed: %+v", habits)
	}
	archived, _ := s.ListArchivedHabits()
	if len(archived) != 1 {
		t.Fatalf("want 1 archived habit, got %d", len(archived))
	}
	if err := s.UnarchiveHabit("Old"); err != nil {
		t.Fatal(err)
	}
	habits, _ = s.ListHabits()
	if len(habits) != 1 {
		t.Fatalf("unarchived habit not listed: %+v", habits)
	}
}
