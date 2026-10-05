package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aeon022/habctl/internal/models"
	"github.com/aeon022/habctl/internal/store"
	"github.com/mark3labs/mcp-go/mcp"
)

func dayStr(off int) string { return time.Now().AddDate(0, 0, off).Format("2006-01-02") }

// Yesterday done, today not yet: that is exactly "streak at risk" and must be
// reported as such, not lumped into "no active streak".
func TestStreakAtRiskReportsRunningStreak(t *testing.T) {
	setupTestDB(t)
	callTool(t, handleCheckHabit, map[string]any{"name": "Sport", "date": dayStr(-1)})
	callTool(t, handleCheckHabit, map[string]any{"name": "Sport", "date": dayStr(-2)})
	text := resultText(t, callTool(t, handleStreakAtRisk, nil))
	if !strings.Contains(text, "Streaks at risk (1)") || !strings.Contains(text, "streak: 2 day(s)") {
		t.Errorf("want one at-risk streak of 2 days, got:\n%s", text)
	}
	if strings.Contains(text, "Pending") {
		t.Errorf("habit with a running streak must not be listed as pending:\n%s", text)
	}
}

func TestGetHabitStatsSingleAndAll(t *testing.T) {
	setupTestDB(t)
	for _, off := range []int{0, -1, -2} {
		callTool(t, handleCheckHabit, map[string]any{"name": "Sport", "date": dayStr(off)})
	}
	one := resultText(t, callTool(t, handleGetHabitStats, map[string]any{"name": "Sport", "days": float64(10)}))
	if !strings.Contains(one, "Sport (streak: 3)") || !strings.Contains(one, "3/10 days") {
		t.Errorf("single stats:\n%s", one)
	}
	all := resultText(t, callTool(t, handleGetHabitStats, map[string]any{"days": float64(-5)})) // <=0 falls back to 30
	if !strings.Contains(all, "last 30 days") || !strings.Contains(all, "Sport") {
		t.Errorf("all stats:\n%s", all)
	}

	res, err := handleGetHabitStats(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: map[string]any{"name": "Ghost"}}})
	if err != nil || !res.IsError {
		t.Errorf("unknown habit must be an error result: %+v %v", res, err)
	}
}

func TestGetHabitStatsEmptyStore(t *testing.T) {
	setupTestDB(t)
	s, _ := store.Open(dbPathOverride, false)
	_ = s.DeleteHabit("Sport")
	_ = s.Close()
	if got := resultText(t, callTool(t, handleGetHabitStats, nil)); !strings.Contains(got, "No habits tracked yet") {
		t.Errorf("got %q", got)
	}
}

func TestFormatStatsFireFromSevenDays(t *testing.T) {
	six := formatStats(models.HabitStats{Habit: models.Habit{Name: "A"}, Streak: 6, TotalDays: 3}, 10)
	seven := formatStats(models.HabitStats{Habit: models.Habit{Name: "A"}, Streak: 7, TotalDays: 3}, 10)
	if strings.Contains(six, "🔥") || !strings.Contains(seven, "🔥") {
		t.Errorf("fire badge boundary wrong:\n%s\n%s", six, seven)
	}
}

func TestListChainsWithData(t *testing.T) {
	setupTestDB(t)
	s, _ := store.Open(dbPathOverride, false)
	_, _ = s.AddHabit("Stretch", "", "")
	if err := s.AddChain("Sport", "Stretch"); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	got := resultText(t, callTool(t, handleListChains, nil))
	if !strings.Contains(got, "Habit Chains (1)") || !strings.Contains(got, "Sport → Stretch") {
		t.Errorf("got:\n%s", got)
	}
}
