package ai

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aeon022/habctl/internal/models"
)

const systemPromptSuggest = `You are a habit coach. Reply in English.

Output habit suggestions in EXACTLY this format — no text before, no text after.
Each habit block starts and ends with the line "###".

###
Name: [Emoji] [Habit name]
Time: [X min/day]
Benefit: [1-2 sentences of concrete benefit]
Tip: [one practical getting-started tip]
###

The app parses this format programmatically. Deviations break parsing.
Rules: Emoji directly before the name · realistic time estimate · no overlap with existing habits`

const systemPromptReview = `You are a personal habit coach. Reply in English. Be direct, concrete and encouraging.

Analyse the habit data from the last week and write a short coaching briefing.
Structure (use exactly these sections):

## Weekly Overview
1-2 sentences: what went well, what was the overall completion rate.

## Top Habits This Week
Max. 2 habits that stood out (high completion or long streak).

## Struggling Right Now
Max. 2 habits with low completion (<50%). Be honest but not discouraging.

## Recommendation
One concrete, actionable recommendation for next week. If a habit is <40%: suggest reducing frequency (e.g. from daily to 4×/week). If habits are thematically related, mention a possible habit chain ("after X → Y").

## Tip of the Week
One short, punchy habit coaching tip (1-2 sentences).

No intro, no closing remarks beyond the briefing itself. No markdown bold on the section names themselves.`

const systemPromptDecompose = `You are a habit coach. Reply in English.

The user names a goal. Suggest exactly 3 interconnected habits that mutually reinforce each other.

Output format — no text before or after:

###
Name: [Emoji] [Habit name]
Time: [X min/day]
Benefit: [how this habit concretely supports the goal]
Tip: [how it reinforces the other two habits or benefits from them]
###

Rules: Exactly 3 habits · mutually reinforcing (timing/theme) · Emoji directly before name · 3–15 min/day · no duplicates of existing habits`

const systemPromptChains = `You are a habit coach. Reply in English.

Analyse the given habits and suggest sensible habit chains.
A habit chain means: when someone completes habit A, they should do habit B directly after.

Output suggestions in EXACTLY this format — no text before or after:

###
From: [Habit name]
To: [Habit name]
Why: [1 sentence explaining why this sequence makes sense]
###

Rules:
- Only use habits from the given list (exact names)
- Prefer natural sequences (temporal, thematic, energetic)
- Max. 3 suggestions
- Do not suggest chains when habits have no sensible connection`

// ErrNoAPIKey is returned when no provider key is configured.
var ErrNoAPIKey = fmt.Errorf("no API key found — set ANTHROPIC_API_KEY, OPENAI_API_KEY or GEMINI_API_KEY")

// SuggestRequest is the input for habit suggestions.
type SuggestRequest struct {
	ExistingHabits  []string
	CompletionRates map[string]float64 // habit name → 0..1 completion rate (last 7 days)
	Routine         string             // morning, evening, health, learning, productivity, ""
	Goal            string
	Count           int // defaults to 3
}

// Suggest streams habit suggestions from the auto-detected provider.
func Suggest(ctx context.Context, req SuggestRequest, out func(chunk string)) (string, error) {
	info, err := Detect()
	if err != nil {
		return "", err
	}
	return Call(ctx, info, systemPromptSuggest, buildPrompt(req), out)
}

// SuggestBlocking is like Suggest but returns the full result without streaming.
func SuggestBlocking(req SuggestRequest) (string, error) {
	info, err := Detect()
	if err != nil {
		return "", err
	}
	return Call(context.Background(), info, systemPromptSuggest, buildPrompt(req), nil)
}

// SuggestOllama streams suggestions from a local Ollama instance.
func SuggestOllama(req SuggestRequest, out func(string)) (string, error) {
	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = "llama3.2"
	}
	info := ProviderInfo{ProviderOllama, model, "Ollama (" + model + ", local)"}
	return Call(context.Background(), info, systemPromptSuggest, buildPrompt(req), out)
}

// SuggestWithProvider runs against a specific provider (for the --provider flag).
func SuggestWithProvider(req SuggestRequest, p Provider, out func(string)) (string, error) {
	info, err := Detect()
	if err != nil && p == "" {
		return "", err
	}
	if p != "" {
		info, err = detectForced(p)
		if err != nil {
			return "", err
		}
	}
	return Call(context.Background(), info, systemPromptSuggest, buildPrompt(req), out)
}

// Review streams an AI coaching briefing based on last week's data.
func Review(ctx context.Context, data models.WeeklyReview, out func(string)) (string, error) {
	info, err := Detect()
	if err != nil {
		return "", err
	}
	return Call(ctx, info, systemPromptReview, buildReviewPrompt(data), out)
}

// DecomposeGoal takes a user goal and suggests 3 interconnected supporting habits.
func DecomposeGoal(ctx context.Context, goal string, existing []string, out func(string)) (string, error) {
	info, err := Detect()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if len(existing) > 0 {
		b.WriteString("My existing habits (no duplicates):\n")
		for _, h := range existing {
			b.WriteString("- " + h + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("My goal: " + goal + "\n")
	return Call(ctx, info, systemPromptDecompose, b.String(), out)
}

// SuggestChains streams habit-chain suggestions based on existing habits.
func SuggestChains(ctx context.Context, habits []string, out func(string)) (string, error) {
	info, err := Detect()
	if err != nil {
		return "", err
	}
	if len(habits) < 2 {
		return "", fmt.Errorf("at least 2 habits required for chain suggestions")
	}
	var b strings.Builder
	b.WriteString("My habits:\n")
	for _, h := range habits {
		b.WriteString("- " + h + "\n")
	}
	return Call(ctx, info, systemPromptChains, b.String(), out)
}

func detectForced(p Provider) (ProviderInfo, error) {
	switch p {
	case ProviderAnthropic:
		return ProviderInfo{ProviderAnthropic, "claude-haiku-4-5-20251001", "Claude Haiku (Anthropic)"}, nil
	case ProviderOpenAI:
		return ProviderInfo{ProviderOpenAI, "gpt-4o-mini", "GPT-4o mini (OpenAI)"}, nil
	case ProviderGemini:
		model := os.Getenv("GEMINI_MODEL")
		if model == "" {
			model = "gemini-flash-latest"
		}
		return ProviderInfo{ProviderGemini, model, "Gemini " + model + " (Google)"}, nil
	case ProviderOllama:
		model := "llama3.2"
		return ProviderInfo{ProviderOllama, model, "Ollama (" + model + ", local)"}, nil
	}
	return ProviderInfo{}, fmt.Errorf("unknown provider %q", p)
}

func buildPrompt(req SuggestRequest) string {
	if req.Count == 0 {
		req.Count = 3
	}

	var b strings.Builder

	if len(req.ExistingHabits) > 0 {
		b.WriteString("My existing habits:\n")
		for _, h := range req.ExistingHabits {
			rate, hasRate := req.CompletionRates[h]
			line := "- " + h
			if hasRate {
				line += fmt.Sprintf(" (%.0f%% last week)", rate*100)
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("No overlap with these habits.\n\n")
	}

	b.WriteString(fmt.Sprintf("Suggest exactly %d habits", req.Count))
	switch req.Routine {
	case "morning":
		b.WriteString(" for a morning routine (energy, clarity, good start)")
	case "evening":
		b.WriteString(" for an evening routine (wind down, good sleep)")
	case "health":
		b.WriteString(" for health (movement, nutrition, sleep, mental health)")
	case "learning":
		b.WriteString(" for learning (reading, writing, languages, new skills)")
	case "productivity":
		b.WriteString(" for productivity (focus, deep work, knowledge workers)")
	default:
		b.WriteString(" — mix of health, learning, productivity, mindfulness")
	}
	b.WriteString(".\n")

	if req.Goal != "" {
		b.WriteString(fmt.Sprintf("My goal: %s\n", req.Goal))
	}

	return b.String()
}

func buildReviewPrompt(data models.WeeklyReview) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Habits analysed: %d\n", len(data.Habits)))
	b.WriteString(fmt.Sprintf("Perfect days this week: %d/7\n", data.PerfectDays))
	if data.WeakestDay != "" {
		b.WriteString(fmt.Sprintf("Weakest weekday (30 days): %s\n", data.WeakestDay))
	}
	if data.StrongestDay != "" {
		b.WriteString(fmt.Sprintf("Strongest weekday (30 days): %s\n", data.StrongestDay))
	}
	b.WriteString("\n")

	for _, h := range data.Habits {
		name := h.Name
		if h.Icon != "" {
			name = h.Icon + " " + name
		}
		b.WriteString(fmt.Sprintf("### %s\n", name))
		b.WriteString(fmt.Sprintf("- Last 7 days: %d/7 (%.0f%%)\n", h.DoneThisWeek, h.CompletionPct7*100))
		b.WriteString(fmt.Sprintf("- Last 30 days: %d/30 (%.0f%%)\n", h.DoneLast30, h.CompletionPct30*100))
		b.WriteString(fmt.Sprintf("- Current streak: %d days\n", h.CurrentStreak))
		if len(h.RecentNotes) > 0 {
			b.WriteString("- Notes:\n")
			for _, n := range h.RecentNotes {
				b.WriteString(fmt.Sprintf("  [%s] %s\n", n.Date, n.Note))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}
