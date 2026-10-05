package cmd

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// runCLI executes the real cobra tree against an isolated data dir and returns
// stdout. Flag variables are package globals that cobra doesn't reset between
// Execute calls, so every test passes the flags it depends on explicitly.
func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() {
		rootCmd.SetArgs(args)
		err = rootCmd.Execute()
	})
	return out, err
}

func isolateCLI(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HABCTL_DATA_DIR", t.TempDir())
	checkDate, todayJSON, statsDays, addDesc = "", false, 30, ""
}

type todayJSONOut struct {
	Done  int `json:"done"`
	Total int `json:"total"`
	Data  []struct {
		Name         string `json:"name"`
		CheckedToday bool   `json:"checked_today"`
		Streak       int    `json:"streak"`
	} `json:"data"`
}

func today(t *testing.T) todayJSONOut {
	t.Helper()
	out, err := runCLI(t, "today", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var j todayJSONOut
	if err := json.Unmarshal([]byte(out), &j); err != nil {
		t.Fatalf("today --json is not valid JSON: %v\n%s", err, out)
	}
	return j
}

func TestCLIAddCheckTodayFlow(t *testing.T) {
	isolateCLI(t)

	if out, err := runCLI(t, "today"); err != nil || !strings.Contains(out, "No habits yet") {
		t.Fatalf("empty today: %q %v", out, err)
	}
	if out, err := runCLI(t, "add", "Sport", "--desc", "20 min"); err != nil || !strings.Contains(out, "Added habit: Sport") {
		t.Fatalf("add: %q %v", out, err)
	}
	if _, err := runCLI(t, "add", "Sport"); err == nil {
		t.Error("adding a duplicate habit must fail")
	}

	if j := today(t); j.Total != 1 || j.Done != 0 || j.Data[0].CheckedToday {
		t.Fatalf("before check-in: %+v", j)
	}
	out, err := runCLI(t, "check", "Sport")
	if err != nil || !strings.Contains(out, "checked in (streak: 1 day)") {
		t.Fatalf("check: %q %v", out, err)
	}
	if j := today(t); j.Done != 1 || !j.Data[0].CheckedToday || j.Data[0].Streak != 1 {
		t.Fatalf("after check-in: %+v", j)
	}
	if _, err := runCLI(t, "check", "Ghost"); err == nil {
		t.Error("checking an unknown habit must fail")
	}
}

func TestCLICheckWithDateBuildsStreak(t *testing.T) {
	isolateCLI(t)
	runCLI(t, "add", "Read")
	y := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	if _, err := runCLI(t, "check", "Read", "--date", y); err != nil {
		t.Fatal(err)
	}
	checkDate = ""
	// yesterday only: the streak is still alive (today not over) but not "done today"
	if j := today(t); j.Data[0].Streak != 1 || j.Data[0].CheckedToday {
		t.Errorf("yesterday's check-in: %+v", j.Data)
	}
	if _, err := runCLI(t, "check", "Read", "--date", "not-a-date"); err == nil {
		t.Error("invalid --date must be rejected")
	}
}

func TestCLIStatsAndList(t *testing.T) {
	isolateCLI(t)
	if out, _ := runCLI(t, "stats"); !strings.Contains(out, "No habits yet") {
		t.Errorf("empty stats: %q", out)
	}
	runCLI(t, "add", "Yoga")
	runCLI(t, "check", "Yoga")
	out, err := runCLI(t, "stats", "--days", "7")
	if err != nil || !strings.Contains(out, "last 7 days") || !strings.Contains(out, "Yoga (streak: 1)") {
		t.Errorf("stats: %q %v", out, err)
	}
	if out, err := runCLI(t, "list"); err != nil || !strings.Contains(out, "Yoga") {
		t.Errorf("list: %q %v", out, err)
	}
}

func TestCLIDeleteRequiresConfirmation(t *testing.T) {
	isolateCLI(t)
	runCLI(t, "add", "Temp")

	answer := func(s string) {
		r, w, _ := os.Pipe()
		w.WriteString(s)
		w.Close()
		old := os.Stdin
		os.Stdin = r
		t.Cleanup(func() { os.Stdin = old })
	}

	answer("n\n")
	if out, err := runCLI(t, "delete", "Temp"); err != nil || !strings.Contains(out, "Aborted") {
		t.Fatalf("declined delete: %q %v", out, err)
	}
	if j := today(t); j.Total != 1 {
		t.Fatal("declined delete removed the habit")
	}

	answer("y\n")
	if out, err := runCLI(t, "delete", "Temp"); err != nil || !strings.Contains(out, "Deleted habit: Temp") {
		t.Fatalf("confirmed delete: %q %v", out, err)
	}
	if j := today(t); j.Total != 0 {
		t.Errorf("habit survived confirmed delete: %+v", j)
	}
}
