package store

import (
	"testing"
	"time"
)

func mustHabit(t *testing.T, s *Store, name string) {
	t.Helper()
	if _, err := s.AddHabit(name, "", ""); err != nil {
		t.Fatal(err)
	}
}

func mustCheck(t *testing.T, s *Store, name string, offsets ...int) {
	t.Helper()
	for _, off := range offsets {
		if err := s.CheckIn(name, day(off)); err != nil {
			t.Fatal(err)
		}
	}
}

// mondayOffset is the day offset (<= 0) of this week's Monday.
func mondayOffset() int {
	wd := int(time.Now().Weekday())
	if wd == 0 {
		wd = 7
	}
	return -(wd - 1)
}

// A daily habit that was done yesterday and the day before but not yet today
// must keep its streak — the day isn't over. Otherwise every morning shows 0
// until the check-in, and "streak at risk" could never fire.
func TestDailyStreakSurvivesUncheckedToday(t *testing.T) {
	s := testStore(t)
	mustHabit(t, s, "Run")
	mustCheck(t, s, "Run", -2, -1)
	st, err := s.GetStats("Run", 30)
	if err != nil {
		t.Fatal(err)
	}
	if st.CheckedToday {
		t.Error("CheckedToday must be false")
	}
	if st.Streak != 2 {
		t.Errorf("streak = %d, want 2 (today not over yet)", st.Streak)
	}
}

func TestDailyStreakDeadAfterMissedYesterday(t *testing.T) {
	s := testStore(t)
	mustHabit(t, s, "Run")
	mustCheck(t, s, "Run", -3, -2) // yesterday AND today missed
	st, _ := s.GetStats("Run", 30)
	if st.Streak != 0 {
		t.Errorf("streak = %d, want 0", st.Streak)
	}
}

func TestWeeklyHabitStreak(t *testing.T) {
	s := testStore(t)
	mustHabit(t, s, "Gym")
	if err := s.SetHabitFreq("Gym", 1); err != nil {
		t.Fatal(err)
	}
	mon := mondayOffset()
	// target met in each of the two previous weeks and today
	mustCheck(t, s, "Gym", mon-14, mon-7, 0)
	st, err := s.GetStats("Gym", 30)
	if err != nil {
		t.Fatal(err)
	}
	if !st.CheckedToday || st.WeeklyDone != 1 {
		t.Errorf("CheckedToday=%v WeeklyDone=%d, want true/1", st.CheckedToday, st.WeeklyDone)
	}
	if st.Streak != 3 {
		t.Errorf("streak = %d, want 3 (this week + 2 previous)", st.Streak)
	}
}

func TestWeeklyHabitInProgressWeekDoesNotBreakStreak(t *testing.T) {
	s := testStore(t)
	mustHabit(t, s, "Gym")
	if err := s.SetHabitFreq("Gym", 2); err != nil {
		t.Fatal(err)
	}
	mon := mondayOffset()
	// two full previous weeks at target 2; this week has not reached 2 yet
	mustCheck(t, s, "Gym", mon-14, mon-13, mon-7, mon-6)
	st, _ := s.GetStats("Gym", 30)
	if st.CheckedToday {
		t.Error("week target not met yet, CheckedToday must be false")
	}
	if st.Streak != 2 {
		t.Errorf("streak = %d, want 2 (previous weeks still count)", st.Streak)
	}
}

func TestWeeklyHabitMissedWeekBreaksStreak(t *testing.T) {
	s := testStore(t)
	mustHabit(t, s, "Gym")
	_ = s.SetHabitFreq("Gym", 2)
	mon := mondayOffset()
	mustCheck(t, s, "Gym", mon-14, mon-13, mon-7) // last week only 1 of 2
	st, _ := s.GetStats("Gym", 30)
	if st.Streak != 0 {
		t.Errorf("streak = %d, want 0", st.Streak)
	}
}

func TestLongestStreakAndWindowTotals(t *testing.T) {
	s := testStore(t)
	mustHabit(t, s, "Read")
	mustCheck(t, s, "Read", -20, -19, -18, -17, -5, -4, 0)
	st, _ := s.GetStats("Read", 10)
	if st.LongestStreak != 4 {
		t.Errorf("longest = %d, want 4", st.LongestStreak)
	}
	if st.TotalDays != 3 { // only -5, -4, 0 are inside the 10-day window
		t.Errorf("TotalDays = %d, want 3", st.TotalDays)
	}
	if st.LastCheckIn == nil || !st.LastCheckIn.Equal(day(0)) {
		t.Errorf("LastCheckIn = %v, want today", st.LastCheckIn)
	}
	want := [7]bool{false, true, true, false, false, false, true} // days -6..0 -> -5,-4,0
	// index i = day(i-6): -5 -> 1, -4 -> 2, 0 -> 6
	if st.Last7Days != want {
		t.Errorf("Last7Days = %v, want %v", st.Last7Days, want)
	}
}

func TestNotesRoundTrip(t *testing.T) {
	s := testStore(t)
	mustHabit(t, s, "Write")
	if err := s.CheckInWithNote("Write", day(-2), "old"); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckInWithNote("Write", day(-1), ""); err != nil { // empty note excluded
		t.Fatal(err)
	}
	if err := s.CheckInWithNote("Write", day(0), "first"); err != nil {
		t.Fatal(err)
	}
	// plain CheckIn must not wipe the note; CheckInWithNote replaces it
	mustCheck(t, s, "Write", 0)
	st, _ := s.GetStats("Write", 30)
	if st.TodayNote != "first" {
		t.Errorf("CheckIn clobbered the note: %q", st.TodayNote)
	}
	if err := s.CheckInWithNote("Write", day(0), "second"); err != nil {
		t.Fatal(err)
	}
	notes, err := s.GetRecentNotes("Write", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 2 || notes[0].Note != "second" || notes[1].Note != "old" {
		t.Errorf("notes = %+v, want [second old] newest first, empty skipped", notes)
	}
	if n, _ := s.GetRecentNotes("Write", 1); len(n) != 1 {
		t.Errorf("limit ignored: %+v", n)
	}
	if err := s.CheckInWithNote("Nope", day(0), "x"); err == nil {
		t.Error("want error for unknown habit")
	}
}

func TestGroupsOrderAndDelete(t *testing.T) {
	s := testStore(t)
	mustHabit(t, s, "A")
	mustHabit(t, s, "B")
	mustHabit(t, s, "C")
	g1, err := s.AddGroup("Morning", "☀")
	if err != nil {
		t.Fatal(err)
	}
	g2, _ := s.AddGroup("Evening", "🌙")
	if g2.SortOrder != g1.SortOrder+1 {
		t.Errorf("sort orders %d, %d", g1.SortOrder, g2.SortOrder)
	}
	_ = s.SetHabitGroup("C", g1.ID)
	_ = s.SetHabitGroup("B", g2.ID)

	habits, _ := s.ListHabits()
	var names []string
	for _, h := range habits {
		names = append(names, h.Name)
	}
	if got := names; len(got) != 3 || got[0] != "C" || got[1] != "B" || got[2] != "A" {
		t.Errorf("order = %v, want [C B A] (group sort order, ungrouped last)", got)
	}

	if err := s.DeleteGroup(g1.ID); err != nil {
		t.Fatal(err)
	}
	habits, _ = s.ListHabits()
	for _, h := range habits {
		if h.Name == "C" && h.GroupID != 0 {
			t.Errorf("habit kept dangling group_id %d after DeleteGroup", h.GroupID)
		}
	}
	if gs, _ := s.ListGroups(); len(gs) != 1 || gs[0].Name != "Evening" {
		t.Errorf("groups = %+v", gs)
	}
	if err := s.SetHabitGroup("B", 0); err != nil { // 0 = ungroup
		t.Fatal(err)
	}
	habits, _ = s.ListHabits()
	for _, h := range habits {
		if h.Name == "B" && h.GroupID != 0 {
			t.Error("SetHabitGroup(0) must ungroup")
		}
	}
}

func TestChains(t *testing.T) {
	s := testStore(t)
	mustHabit(t, s, "Wake")
	mustHabit(t, s, "Stretch")
	if err := s.AddChain("Wake", "Wake"); err == nil {
		t.Error("self chain must be rejected")
	}
	if err := s.AddChain("Wake", "Ghost"); err == nil {
		t.Error("unknown habit must be rejected")
	}
	if err := s.AddChain("Wake", "Stretch"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddChain("Wake", "Stretch"); err != nil { // duplicate ignored, not an error
		t.Fatal(err)
	}
	chains, _ := s.ListChains()
	if len(chains) != 1 || chains[0].FromName != "Wake" || chains[0].ToName != "Stretch" {
		t.Fatalf("chains = %+v", chains)
	}
	if st, _ := s.GetStats("Wake", 7); st.ChainTo != "Stretch" {
		t.Errorf("ChainTo = %q", st.ChainTo)
	}
	if err := s.DeleteChain(chains[0].ID); err != nil {
		t.Fatal(err)
	}
	if chains, _ = s.ListChains(); len(chains) != 0 {
		t.Errorf("chain not deleted: %+v", chains)
	}
}

func TestCalendarData(t *testing.T) {
	s := testStore(t)
	if cd, err := s.GetCalendarData(4); err != nil || cd.TotalHabits != 0 || len(cd.ByDate) != 0 {
		t.Fatalf("empty store: %+v %v", cd, err)
	}
	mustHabit(t, s, "A")
	mustHabit(t, s, "B")
	mustCheck(t, s, "A", 0, -1, -100)
	mustCheck(t, s, "B", 0)
	cd, err := s.GetCalendarData(4)
	if err != nil {
		t.Fatal(err)
	}
	if cd.TotalHabits != 2 {
		t.Errorf("TotalHabits = %d, want 2", cd.TotalHabits)
	}
	if cd.ByDate[day(0).Format(dateLayout)] != 2 || cd.ByDate[day(-1).Format(dateLayout)] != 1 {
		t.Errorf("ByDate = %v", cd.ByDate)
	}
	if _, old := cd.ByDate[day(-100).Format(dateLayout)]; old {
		t.Error("check-in outside the window leaked in")
	}

	by, err := s.GetCheckinDatesByHabit(4)
	if err != nil {
		t.Fatal(err)
	}
	habits, _ := s.ListHabits()
	idA := habits[0].ID
	if len(by[idA]) != 2 || !by[idA][day(-1).Format(dateLayout)] {
		t.Errorf("by habit = %v", by)
	}
}

// Archived habits are hidden everywhere — the heatmap denominator included,
// otherwise a day where every ACTIVE habit was done never reaches 100%.
func TestCalendarTotalIgnoresArchived(t *testing.T) {
	s := testStore(t)
	mustHabit(t, s, "A")
	mustHabit(t, s, "Old")
	if err := s.ArchiveHabit("Old"); err != nil {
		t.Fatal(err)
	}
	mustCheck(t, s, "A", 0)
	cd, _ := s.GetCalendarData(2)
	if cd.TotalHabits != 1 {
		t.Errorf("TotalHabits = %d, want 1 (archived excluded)", cd.TotalHabits)
	}
}

func TestGetAllStatsAndUnknownHabit(t *testing.T) {
	s := testStore(t)
	mustHabit(t, s, "A")
	mustHabit(t, s, "B")
	mustCheck(t, s, "A", 0)
	all, err := s.GetAllStats(7)
	if err != nil || len(all) != 2 {
		t.Fatalf("GetAllStats = %d, %v", len(all), err)
	}
	if all[0].Habit.Name != "A" || !all[0].CheckedToday || all[1].CheckedToday {
		t.Errorf("stats = %+v", all)
	}
	if _, err := s.GetStats("Ghost", 7); err == nil {
		t.Error("want error for unknown habit")
	}
}

func TestWeeklyReview(t *testing.T) {
	s := testStore(t)
	mustHabit(t, s, "A")
	mustHabit(t, s, "B")
	mustCheck(t, s, "A", 0, -1, -2)
	mustCheck(t, s, "B", 0, -1)
	_ = s.CheckInWithNote("A", day(0), "felt good")

	r, err := s.GetWeeklyReview()
	if err != nil {
		t.Fatal(err)
	}
	if r.PerfectDays != 2 {
		t.Errorf("PerfectDays = %d, want 2 (today and yesterday: both habits)", r.PerfectDays)
	}
	if len(r.Habits) != 2 {
		t.Fatalf("habits = %+v", r.Habits)
	}
	a := r.Habits[0]
	if a.Name != "A" || a.DoneThisWeek != 3 || a.DoneLast30 != 3 || a.CurrentStreak != 3 {
		t.Errorf("A = %+v", a)
	}
	if a.CompletionPct7 < 0.42 || a.CompletionPct7 > 0.43 { // 3/7
		t.Errorf("CompletionPct7 = %v, want 3/7", a.CompletionPct7)
	}
	if len(a.RecentNotes) != 1 || a.RecentNotes[0].Note != "felt good" {
		t.Errorf("notes = %+v", a.RecentNotes)
	}
	if r.WeakestDay == "" {
		t.Error("WeakestDay empty although data exists")
	}

	empty := testStore(t)
	if r, err := empty.GetWeeklyReview(); err != nil || len(r.Habits) != 0 || r.PerfectDays != 0 {
		t.Errorf("empty review = %+v %v", r, err)
	}
}

func TestResolveDBPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HABCTL_DATA_DIR", "")
	p, shared, err := ResolveDBPath()
	if err != nil || shared || p != home+"/.local/share/habctl/habits.db" {
		t.Errorf("default = %q shared=%v err=%v", p, shared, err)
	}
	dir := t.TempDir() + "/sync"
	t.Setenv("HABCTL_DATA_DIR", dir)
	p, shared, _ = ResolveDBPath()
	if !shared || p != dir+"/habits.db" {
		t.Errorf("override = %q shared=%v", p, shared)
	}
}
