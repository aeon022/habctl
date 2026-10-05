package ai

import (
	"os"
	"strings"
	"testing"

	"github.com/aeon022/habctl/internal/models"
	coreai "github.com/aeon022/missionctl-core/ai"
)

func TestSuggestionCacheRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if got, err := LoadLastSuggestions(); got != nil || err != nil {
		t.Fatalf("nothing cached yet must be nil,nil — got %v, %v", got, err)
	}
	want := []Suggestion{{Name: "Walk", Time: "10 min", Benefit: "energy", Tip: "after lunch"}, {Name: "Read"}}
	if err := SaveLastSuggestions(want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadLastSuggestions()
	if err != nil || len(got) != 2 || got[0] != want[0] || got[1].Name != "Read" {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
	// a second save replaces, never appends
	if err := SaveLastSuggestions(want[1:]); err != nil {
		t.Fatal(err)
	}
	if got, _ = LoadLastSuggestions(); len(got) != 1 {
		t.Errorf("cache must hold only the latest suggestions, got %+v", got)
	}
}

func TestLoadLastSuggestionsCorruptFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := SaveLastSuggestions([]Suggestion{{Name: "x"}}); err != nil {
		t.Fatal(err)
	}
	p, _ := lastSuggestionsPath()
	if err := writeFile(p, "{not json"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLastSuggestions(); err == nil {
		t.Error("corrupt cache must surface an error, not silently look empty")
	}
}

func TestSuggestionDescription(t *testing.T) {
	cases := []struct {
		s    Suggestion
		want string
	}{
		{Suggestion{Benefit: "calm"}, "calm"},
		{Suggestion{Time: "5 min", Benefit: "calm"}, "(5 min) calm"},
		{Suggestion{Time: "5 min", Benefit: "calm", Tip: "breathe"}, "(5 min) calm Tip: breathe"},
	}
	for _, c := range cases {
		if got := c.s.Description(); got != c.want {
			t.Errorf("Description() = %q, want %q", got, c.want)
		}
	}
}

func TestBuildPrompt(t *testing.T) {
	p := buildPrompt(SuggestRequest{})
	if !strings.Contains(p, "exactly 3 habits") || !strings.Contains(p, "mix of health") {
		t.Errorf("defaults wrong: %q", p)
	}
	if strings.Contains(p, "existing habits") || strings.Contains(p, "My goal") {
		t.Errorf("empty request must not mention habits/goal: %q", p)
	}

	p = buildPrompt(SuggestRequest{
		ExistingHabits:  []string{"Run", "Read"},
		CompletionRates: map[string]float64{"Run": 0.5},
		Routine:         "evening",
		Goal:            "better sleep",
		Count:           5,
	})
	for _, want := range []string{"- Run (50% last week)", "- Read\n", "No overlap", "exactly 5 habits", "evening routine", "My goal: better sleep"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
	// the rate suffix belongs only to habits that have a rate
	if strings.Contains(p, "Read (") {
		t.Errorf("habit without a rate got a rate suffix:\n%s", p)
	}
}

func TestBuildPromptGermanGoalGetsGermanDirective(t *testing.T) {
	p := buildPrompt(SuggestRequest{Goal: "mehr Bewegung im Alltag"})
	if !strings.HasPrefix(p, germanDirective) {
		t.Errorf("German goal must lead with the German directive: %q", p)
	}
	// with no goal, language is taken from the existing habit names
	p = buildPrompt(SuggestRequest{ExistingHabits: []string{"Früh aufstehen"}})
	if !strings.HasPrefix(p, germanDirective) {
		t.Errorf("umlaut habit name must trigger German directive: %q", p)
	}
}

func TestBuildReviewPrompt(t *testing.T) {
	p := buildReviewPrompt(models.WeeklyReview{
		PerfectDays: 2, WeakestDay: "Monday (10%)", StrongestDay: "Friday (90%)",
		Habits: []models.HabitWeekData{{
			Name: "Run", Icon: "🏃", DoneThisWeek: 3, DoneLast30: 12,
			CompletionPct7: 3.0 / 7, CompletionPct30: 0.4, CurrentStreak: 2,
			RecentNotes: []models.NoteEntry{{Date: "2026-10-01", Note: "knee hurt"}},
		}},
	})
	for _, want := range []string{
		"Habits analysed: 1", "Perfect days this week: 2/7", "Weakest weekday (30 days): Monday (10%)",
		"Strongest weekday (30 days): Friday (90%)", "### 🏃 Run", "Last 7 days: 3/7 (43%)",
		"Last 30 days: 12/30 (40%)", "Current streak: 2 days", "[2026-10-01] knee hurt",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("review prompt missing %q:\n%s", want, p)
		}
	}
	bare := buildReviewPrompt(models.WeeklyReview{Habits: []models.HabitWeekData{{Name: "X"}}})
	if strings.Contains(bare, "Weakest") || strings.Contains(bare, "Notes:") {
		t.Errorf("absent data must be omitted:\n%s", bare)
	}
}

func TestDetectForcedBranches(t *testing.T) {
	t.Setenv("GEMINI_MODEL", "")
	for p, wantModel := range map[Provider]string{
		ProviderAnthropic: "claude-haiku-4-5-20251001",
		ProviderOpenAI:    "gpt-4o-mini",
		ProviderGemini:    "gemini-flash-latest",
	} {
		info, err := detectForced(p)
		if err != nil || info.Name != p || info.Model != wantModel {
			t.Errorf("detectForced(%s) = %+v, %v", p, info, err)
		}
	}
	t.Setenv("GEMINI_MODEL", "gemini-pro")
	if info, _ := detectForced(ProviderGemini); info.Model != "gemini-pro" {
		t.Errorf("GEMINI_MODEL ignored: %+v", info)
	}
	if _, err := detectForced("nope"); err == nil {
		t.Error("unknown provider must error")
	}
}

func TestDetectUsesHabctlPrefixAndGoogleHook(t *testing.T) {
	for _, k := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "GOOGLE_REFRESH_TOKEN"} {
		t.Setenv(k, "")
	}
	t.Setenv("HABCTL_PROVIDER", "openai")
	if _, err := Detect(); err == nil {
		t.Error("HABCTL_PROVIDER=openai without a key must error")
	}
	t.Setenv("OPENAI_API_KEY", "sk-test")
	if info, err := Detect(); err != nil || info.Name != ProviderOpenAI {
		t.Errorf("Detect = %+v, %v", info, err)
	}

	// Gemini becomes available through a Google refresh token alone (habctl's own hook)
	t.Setenv("HABCTL_PROVIDER", "gemini")
	t.Setenv("OPENAI_API_KEY", "")
	if _, err := Detect(); err == nil {
		t.Fatal("gemini without any credential must error")
	}
	t.Setenv("GOOGLE_REFRESH_TOKEN", "rt")
	if !coreai.GeminiAvailable() {
		t.Error("GeminiAvailable hook not installed")
	}
	if info, err := Detect(); err != nil || info.Name != ProviderGemini {
		t.Errorf("refresh token alone should enable gemini: %+v, %v", info, err)
	}
	// without client id/secret the OAuth exchange is skipped (no network) → falls back to the API key
	if k := coreai.GeminiKey(); k != "" {
		t.Errorf("GeminiKey = %q, want \"\" without client credentials", k)
	}
}

func writeFile(p, s string) error { return os.WriteFile(p, []byte(s), 0o644) }
