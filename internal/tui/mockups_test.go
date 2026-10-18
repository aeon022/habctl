package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/aeon022/habctl/internal/models"
)

// TestRenderLandingMockups writes an ANSI screenshot of the real TUI (made-up demo data) for the landing
// page: HABCTL_MOCKUP_DIR=/dir go test ./internal/tui -run TestRenderLandingMockups
func TestRenderLandingMockups(t *testing.T) {
	dir := os.Getenv("HABCTL_MOCKUP_DIR")
	if dir == "" {
		t.Skip("set HABCTL_MOCKUP_DIR to render landing mockups")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HABCTL_DATA_DIR", "")

	now := time.Now()
	hab := func(id int64, icon, name string, streak, total int, today bool, week [7]bool) models.HabitStats {
		last := now.AddDate(0, 0, -1)
		return models.HabitStats{
			Habit:  models.Habit{ID: id, Name: name, Icon: icon, CreatedAt: now.AddDate(0, -3, 0)},
			Streak: streak, LongestStreak: streak + 4, TotalDays: total, LastCheckIn: &last,
			CheckedToday: today, Last7Days: week,
		}
	}
	all := [7]bool{true, true, true, true, true, true, true}
	most := [7]bool{true, true, false, true, true, true, true}
	some := [7]bool{false, true, true, false, true, true, false}
	habits := []models.HabitStats{
		hab(1, "🏃", "Morning run", 12, 64, true, most),
		hab(2, "📖", "Read 30 min", 21, 88, true, all),
		hab(3, "🧘", "Meditate", 5, 41, false, some),
		hab(4, "💧", "Drink 2 l water", 9, 70, true, most),
		hab(5, "✍️", "Journal", 3, 29, false, some),
		hab(6, "🥗", "Cook at home", 0, 18, false, [7]bool{false, false, true, false, false, true, false}),
	}
	m := newTestModel()
	m.width, m.height = 118, 17
	mi, _ := m.Update(tea.WindowSizeMsg{Width: 118, Height: 17})
	mi, _ = mi.(model).Update(habitsLoadedMsg(habits))
	out := mi.(model).View().Content
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "habctl.ansi"), []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
}
