package ai

import (
	"context"
	"fmt"
	"os"
	"strings"
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

// ErrNoAPIKey is returned when no provider key is configured.
var ErrNoAPIKey = fmt.Errorf("kein API-Key gefunden — setze ANTHROPIC_API_KEY, OPENAI_API_KEY oder GEMINI_API_KEY")

// SuggestRequest is the input for habit suggestions.
type SuggestRequest struct {
	ExistingHabits []string
	Routine        string // morning, evening, health, learning, productivity, ""
	Goal           string // free-text goal
	Count          int    // defaults to 6
}

// Suggest streams habit suggestions from the auto-detected provider.
// ctx can be cancelled to abort the inflight HTTP request immediately.
// Each text chunk is passed to out as it arrives.
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

// SuggestOllama streams suggestions from a local Ollama instance, bypassing
// provider detection entirely. Used by the TUI (no API key required).
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
		b.WriteString("Meine bestehenden Habits (keine Überschneidungen):\n")
		for _, h := range req.ExistingHabits {
			b.WriteString("- " + h + "\n")
		}
		b.WriteString("\n")
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
