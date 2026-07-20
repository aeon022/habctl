package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aeon022/habctl/internal/store"
	"github.com/mark3labs/mcp-go/mcp"
)

// setupTestDB points openStore() at a temporary database via dbPathOverride
// and seeds one habit. suggest_habits calls a real Claude/Gemini API and is
// deliberately not smoke-tested; every other handler is pure local SQLite.
func setupTestDB(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "habits.db")
	dbPathOverride = path
	t.Cleanup(func() { dbPathOverride = "" })

	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer s.Close()

	if _, err := s.AddHabit("Sport", "20 min run", ""); err != nil {
		t.Fatalf("AddHabit: %v", err)
	}
}

func callTool(t *testing.T, handler func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error), args map[string]any) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}}
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler returned error: %v", err)
	}
	if res.IsError {
		t.Fatalf("handler returned an error result: %+v", res.Content)
	}
	return res
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", res.Content[0])
	}
	return tc.Text
}

func TestToolsAreRegisteredWithValidSchema(t *testing.T) {
	for _, tc := range []struct {
		name string
		tool mcp.Tool
	}{
		{"check_habit", toolCheckHabit()},
		{"list_habits", toolListHabits()},
		{"get_habit_stats", toolGetHabitStats()},
		{"add_habit", toolAddHabit()},
		{"delete_habit", toolDeleteHabit()},
		{"streak_at_risk", toolStreakAtRisk()},
		{"get_weekly_summary", toolWeeklySummary()},
		{"suggest_habits", toolSuggestHabits()},
		{"add_checkin_note", toolAddCheckinNote()},
		{"uncheck_habit", toolUncheckHabit()},
		{"list_chains", toolListChains()},
		{"get_weekly_review", toolGetWeeklyReview()},
	} {
		if tc.tool.Name != tc.name {
			t.Errorf("expected tool name %q, got %q", tc.name, tc.tool.Name)
		}
		if tc.tool.Description == "" {
			t.Errorf("tool %q has no description", tc.name)
		}
	}
}

func TestHandleAddAndListHabits(t *testing.T) {
	setupTestDB(t)

	callTool(t, handleAddHabit, map[string]any{"name": "Reading", "description": "30 min"})

	res := callTool(t, handleListHabits, nil)
	text := resultText(t, res)
	if !strings.Contains(text, "Sport") || !strings.Contains(text, "Reading") {
		t.Errorf("expected both habits in output, got:\n%s", text)
	}
}

func TestHandleCheckAndUncheckHabit(t *testing.T) {
	setupTestDB(t)

	res := callTool(t, handleCheckHabit, map[string]any{"name": "Sport"})
	if !strings.Contains(resultText(t, res), "streak: 1") {
		t.Errorf("expected streak of 1 after first check-in, got:\n%s", resultText(t, res))
	}

	listRes := callTool(t, handleListHabits, nil)
	if !strings.Contains(resultText(t, listRes), "✓") {
		t.Error("expected habit to show as checked today")
	}

	callTool(t, handleUncheckHabit, map[string]any{"name": "Sport"})
	listRes = callTool(t, handleListHabits, nil)
	if strings.Contains(resultText(t, listRes), "✓") {
		t.Error("expected habit to show as unchecked after uncheck_habit")
	}
}

func TestHandleDeleteHabit(t *testing.T) {
	setupTestDB(t)

	callTool(t, handleDeleteHabit, map[string]any{"name": "Sport"})

	res := callTool(t, handleListHabits, nil)
	text := resultText(t, res)
	if strings.Contains(text, "Sport") {
		t.Errorf("expected habit to be gone after delete, got:\n%s", text)
	}
}

func TestHandleStreakAtRisk(t *testing.T) {
	setupTestDB(t)

	res := callTool(t, handleStreakAtRisk, nil)
	text := resultText(t, res)
	if !strings.Contains(text, "Pending") && !strings.Contains(text, "Sport") {
		t.Errorf("expected uncompleted habit to be flagged, got:\n%s", text)
	}

	callTool(t, handleCheckHabit, map[string]any{"name": "Sport"})
	res = callTool(t, handleStreakAtRisk, nil)
	if !strings.Contains(resultText(t, res), "checked in today") {
		t.Errorf("expected all-clear message after checking in, got:\n%s", resultText(t, res))
	}
}

func TestHandleWeeklySummaryAndReview(t *testing.T) {
	setupTestDB(t)
	callTool(t, handleCheckHabit, map[string]any{"name": "Sport"})

	res := callTool(t, handleWeeklySummary, nil)
	if !strings.Contains(resultText(t, res), "Sport") {
		t.Errorf("expected habit in weekly summary, got:\n%s", resultText(t, res))
	}

	res = callTool(t, handleGetWeeklyReview, nil)
	if !strings.Contains(resultText(t, res), "Sport") {
		t.Errorf("expected habit in weekly review, got:\n%s", resultText(t, res))
	}
}

func TestHandleAddCheckinNote(t *testing.T) {
	setupTestDB(t)
	callTool(t, handleCheckHabit, map[string]any{"name": "Sport"})

	res := callTool(t, handleAddCheckinNote, map[string]any{"name": "Sport", "note": "felt great"})
	if !strings.Contains(resultText(t, res), "felt great") {
		t.Errorf("expected note confirmation, got:\n%s", resultText(t, res))
	}

	review := callTool(t, handleGetWeeklyReview, nil)
	if !strings.Contains(resultText(t, review), "felt great") {
		t.Errorf("expected note to show up in weekly review, got:\n%s", resultText(t, review))
	}
}

func TestHandleListChainsEmpty(t *testing.T) {
	setupTestDB(t)

	res := callTool(t, handleListChains, nil)
	if !strings.Contains(resultText(t, res), "No habit chains") {
		t.Errorf("expected empty-chains message, got:\n%s", resultText(t, res))
	}
}
