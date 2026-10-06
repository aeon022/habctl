package store

import (
	"testing"
	"time"

	"github.com/aeon022/missionctl-core/activity"
)

func activitySandbox(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MISSIONCTL_DATA_DIR", t.TempDir())
	t.Setenv("MISSIONCTL_ACTIVITY", "")
}

func todaysEvents(t *testing.T) []activity.Event {
	t.Helper()
	evs, err := activity.Read(activity.Day(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

func TestAddAndCheckInAreLoggedOnce(t *testing.T) {
	activitySandbox(t)
	s := testStore(t)
	if _, err := s.AddHabit("Sport", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckIn("Sport", time.Now()); err != nil {
		t.Fatal(err)
	}
	// the same day again is idempotent in the store, so it must not log again
	if err := s.CheckIn("Sport", time.Now()); err != nil {
		t.Fatal(err)
	}
	evs := todaysEvents(t)
	if len(evs) != 2 {
		t.Fatalf("want added + checked, got %+v", evs)
	}
	if evs[0].Tool != "habctl" || evs[0].Action != "added" || evs[0].Title != "Sport" {
		t.Errorf("first = %+v", evs[0])
	}
	if evs[1].Action != "checked" || evs[1].Title != "Sport" {
		t.Errorf("second = %+v", evs[1])
	}
}

func TestBackfillAndNoteEditsAreNotLogged(t *testing.T) {
	activitySandbox(t)
	s := testStore(t)
	_, _ = s.AddHabit("Lesen", "", "")
	before := len(todaysEvents(t))

	if err := s.CheckIn("Lesen", time.Now().AddDate(0, 0, -3)); err != nil { // backfill
		t.Fatal(err)
	}
	if err := s.CheckInWithNote("Lesen", time.Now().AddDate(0, 0, -2), "gestern vergessen"); err != nil {
		t.Fatal(err)
	}
	if got := len(todaysEvents(t)); got != before {
		t.Errorf("backfilled days must not log: %d new events", got-before)
	}

	// a check-in with a note that creates today's row counts once...
	if err := s.CheckInWithNote("Lesen", time.Now(), "30 Seiten"); err != nil {
		t.Fatal(err)
	}
	// ...editing that note afterwards does not
	if err := s.CheckInWithNote("Lesen", time.Now(), "40 Seiten"); err != nil {
		t.Fatal(err)
	}
	if got := len(todaysEvents(t)) - before; got != 1 {
		t.Errorf("want exactly 1 checked event for today, got %d", got)
	}
}

func TestActivityOffStillDoesTheWork(t *testing.T) {
	activitySandbox(t)
	t.Setenv("MISSIONCTL_ACTIVITY", "off")
	s := testStore(t)
	if _, err := s.AddHabit("Sport", "", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckIn("Sport", time.Now()); err != nil {
		t.Fatal(err)
	}
	if evs := todaysEvents(t); len(evs) != 0 {
		t.Errorf("logging is off, got %+v", evs)
	}
	if hs, _ := s.ListHabits(); len(hs) != 1 {
		t.Error("the habit must still be created")
	}
}
