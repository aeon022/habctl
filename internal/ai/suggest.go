package ai

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aeon022/habctl/internal/models"
)

const systemPromptSuggest = `Du bist ein Habit-Coach. Antworte auf Deutsch.

Gib die Habit-Vorschläge in EXAKT diesem Format aus — kein Text davor, kein Text danach.
Jeder Habit-Block beginnt und endet mit der Zeile "###".

###
Name: [Emoji] [Habit-Name]
Zeit: [X Min/Tag]
Nutzen: [1-2 Sätze konkreter Nutzen]
Tipp: [ein praktischer Einstiegstipp]
###

Die App parst dieses Format maschinell. Abweichungen brechen das Parsing.
Regeln: Emoji direkt vor dem Namen · Zeitaufwand realistisch · keine Überschneidungen mit bestehenden Habits`

const systemPromptReview = `Du bist ein persönlicher Habit-Coach. Antworte auf Deutsch. Sei direkt, konkret und ermutigend.

Analysiere die Habit-Daten der letzten Woche und schreibe ein kurzes Coaching-Briefing.
Struktur (nutze exakt diese Abschnitte):

## Wochenüberblick
1-2 Sätze: Was lief gut, wie war die Gesamtkompletionsrate.

## Top-Habits dieser Woche
Max. 2 Habits die herausragten (hohe Completion oder langer Streak).

## Kämpft gerade
Max. 2 Habits mit niedriger Completion (<50%). Sei ehrlich aber nicht entmutigend.

## Empfehlung
Eine konkrete, umsetzbare Empfehlung für die nächste Woche. Falls ein Habit <40% hat: schlage vor, die Frequenz zu reduzieren (z.B. von täglich auf 4x/Woche). Falls Habits thematisch zusammenpassen, erwähne eine mögliche Habit-Kette ("nach X → Y").

## Tipp der Woche
Ein kurzer, prägnanter Habit-Coaching-Tipp (1-2 Sätze).

Keine Einleitung, keine Schlussworte außer dem Briefing selbst. Kein Markdown-Fettdruck in den Abschnittsnamen selbst.`

const systemPromptDecompose = `Du bist ein Habit-Coach. Antworte auf Deutsch.

Der Nutzer nennt ein Ziel. Schlage genau 3 miteinander verknüpfte Gewohnheiten vor, die sich gegenseitig stärken.

Ausgabeformat — kein Text davor/danach:

###
Name: [Emoji] [Habit-Name]
Zeit: [X Min/Tag]
Nutzen: [wie dieser Habit das Ziel konkret unterstützt]
Tipp: [wie er die anderen beiden Habits verstärkt oder von ihnen profitiert]
###

Regeln: Genau 3 Habits · gegenseitig verstärkend (zeitlich/thematisch) · Emoji direkt vor Name · 3–15 Min/Tag · keine Duplikate bestehender Habits`

const systemPromptChains = `Du bist ein Habit-Coach. Antworte auf Deutsch.

Analysiere die gegebenen Habits und schlage sinnvolle Habit-Ketten vor.
Eine Habit-Kette bedeutet: Wenn jemand Habit A erledigt, soll er direkt danach Habit B machen.

Gib die Vorschläge in EXAKT diesem Format — kein Text davor oder danach:

###
Von: [Habit-Name]
Zu: [Habit-Name]
Warum: [1 Satz Begründung warum diese Sequenz Sinn ergibt]
###

Regeln:
- Nur Habits aus der gegebenen Liste verwenden (exakte Namen)
- Natürliche Sequenzen bevorzugen (zeitlich, thematisch, energetisch)
- Max. 3 Vorschläge
- Keine Ketten vorschlagen wenn die Habits keine sinnvolle Verbindung haben`

// ErrNoAPIKey is returned when no provider key is configured.
var ErrNoAPIKey = fmt.Errorf("kein API-Key gefunden — setze ANTHROPIC_API_KEY, OPENAI_API_KEY oder GEMINI_API_KEY")

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
		b.WriteString("Meine bestehenden Habits (keine Duplikate):\n")
		for _, h := range existing {
			b.WriteString("- " + h + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("Mein Ziel: " + goal + "\n")
	return Call(ctx, info, systemPromptDecompose, b.String(), out)
}

// SuggestChains streams habit-chain suggestions based on existing habits.
func SuggestChains(ctx context.Context, habits []string, out func(string)) (string, error) {
	info, err := Detect()
	if err != nil {
		return "", err
	}
	if len(habits) < 2 {
		return "", fmt.Errorf("mindestens 2 Habits für Ketten-Vorschläge nötig")
	}
	var b strings.Builder
	b.WriteString("Meine Habits:\n")
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
	return ProviderInfo{}, fmt.Errorf("unbekannter Provider %q", p)
}

func buildPrompt(req SuggestRequest) string {
	if req.Count == 0 {
		req.Count = 3
	}

	var b strings.Builder

	if len(req.ExistingHabits) > 0 {
		b.WriteString("Meine bestehenden Habits:\n")
		for _, h := range req.ExistingHabits {
			rate, hasRate := req.CompletionRates[h]
			line := "- " + h
			if hasRate {
				line += fmt.Sprintf(" (%.0f%% letzte Woche)", rate*100)
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("Keine Überschneidungen mit diesen Habits.\n\n")
	}

	b.WriteString(fmt.Sprintf("Schlage mir genau %d Habits vor", req.Count))
	switch req.Routine {
	case "morning":
		b.WriteString(" für eine Morgenroutine (Energie, Klarheit, guter Start)")
	case "evening":
		b.WriteString(" für eine Abendroutine (Runterfahren, guter Schlaf)")
	case "health":
		b.WriteString(" für Gesundheit (Bewegung, Ernährung, Schlaf, Mental Health)")
	case "learning":
		b.WriteString(" zum Lernen (Lesen, Schreiben, Sprachen, neue Skills)")
	case "productivity":
		b.WriteString(" für Produktivität (Fokus, Deep Work, Wissensarbeiter)")
	default:
		b.WriteString(" — Mix aus Gesundheit, Lernen, Produktivität, Mindfulness")
	}
	b.WriteString(".\n")

	if req.Goal != "" {
		b.WriteString(fmt.Sprintf("Mein Ziel: %s\n", req.Goal))
	}

	return b.String()
}

func buildReviewPrompt(data models.WeeklyReview) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Habits analysiert: %d\n", len(data.Habits)))
	b.WriteString(fmt.Sprintf("Perfekte Tage diese Woche: %d/7\n", data.PerfectDays))
	if data.WeakestDay != "" {
		b.WriteString(fmt.Sprintf("Schwächster Wochentag (30 Tage): %s\n", data.WeakestDay))
	}
	if data.StrongestDay != "" {
		b.WriteString(fmt.Sprintf("Stärkster Wochentag (30 Tage): %s\n", data.StrongestDay))
	}
	b.WriteString("\n")

	for _, h := range data.Habits {
		name := h.Name
		if h.Icon != "" {
			name = h.Icon + " " + name
		}
		b.WriteString(fmt.Sprintf("### %s\n", name))
		b.WriteString(fmt.Sprintf("- Letzte 7 Tage: %d/7 (%.0f%%)\n", h.DoneThisWeek, h.CompletionPct7*100))
		b.WriteString(fmt.Sprintf("- Letzte 30 Tage: %d/30 (%.0f%%)\n", h.DoneLast30, h.CompletionPct30*100))
		b.WriteString(fmt.Sprintf("- Aktueller Streak: %d Tage\n", h.CurrentStreak))
		if len(h.RecentNotes) > 0 {
			b.WriteString("- Notizen:\n")
			for _, n := range h.RecentNotes {
				b.WriteString(fmt.Sprintf("  [%s] %s\n", n.Date, n.Note))
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}
