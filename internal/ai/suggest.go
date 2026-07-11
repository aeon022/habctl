package ai

import (
	"fmt"
	"os"
	"strings"
)

const systemPromptSuggest = `Du bist ein Habit-Coach, der Menschen hilft, bessere Gewohnheiten aufzubauen.

Antworte auf Deutsch. Sei konkret und direkt. Keine langen Einleitungen.

Beim Vorschlagen von Habits:
- Konkret und umsetzbar (nicht "mehr Sport" sondern "20 Min Laufen")
- Gib den Zeitaufwand an (z.B. "2 Min", "20 Min", "täglich")
- Erkläre kurz WARUM der Habit wertvoll ist (1 Satz)
- Mix aus schnellen Wins (2-5 Min) und bedeutsamen Habits
- Vermeide Überschneidungen mit bestehenden Habits
- Format: **Habit-Name** (Zeit) — Warum es wichtig ist`

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
// Each text chunk is passed to out as it arrives.
// Returns the full response and the provider used.
func Suggest(req SuggestRequest, out func(chunk string)) (string, error) {
	info, err := Detect()
	if err != nil {
		return "", err
	}
	return Call(info, systemPromptSuggest, buildPrompt(req), out)
}

// SuggestBlocking is like Suggest but returns the full result without streaming.
func SuggestBlocking(req SuggestRequest) (string, error) {
	info, err := Detect()
	if err != nil {
		return "", err
	}
	return Call(info, systemPromptSuggest, buildPrompt(req), nil)
}

// SuggestOllama streams suggestions from a local Ollama instance, bypassing
// provider detection entirely. Used by the TUI (no API key required).
func SuggestOllama(req SuggestRequest, out func(string)) (string, error) {
	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = "llama3.2"
	}
	info := ProviderInfo{ProviderOllama, model, "Ollama (" + model + ", local)"}
	return Call(info, systemPromptSuggest, buildPrompt(req), out)
}

// SuggestWithProvider runs against a specific provider (for the --provider flag).
func SuggestWithProvider(req SuggestRequest, p Provider, out func(string)) (string, error) {
	info, err := Detect()
	if err != nil && p == "" {
		return "", err
	}
	if p != "" {
		// Re-detect but constrain to requested provider.
		info, err = detectForced(p)
		if err != nil {
			return "", err
		}
	}
	return Call(info, systemPromptSuggest, buildPrompt(req), out)
}

func detectForced(p Provider) (ProviderInfo, error) {
	switch p {
	case ProviderAnthropic:
		return ProviderInfo{ProviderAnthropic, "claude-haiku-4-5-20251001", "Claude Haiku (Anthropic)"}, nil
	case ProviderOpenAI:
		return ProviderInfo{ProviderOpenAI, "gpt-4o-mini", "GPT-4o mini (OpenAI)"}, nil
	case ProviderGemini:
		return ProviderInfo{ProviderGemini, "gemini-2.0-flash", "Gemini 2.0 Flash (Google)"}, nil
	case ProviderOllama:
		model := "llama3.2"
		return ProviderInfo{ProviderOllama, model, "Ollama (" + model + ", local)"}, nil
	}
	return ProviderInfo{}, fmt.Errorf("unbekannter Provider %q", p)
}

func buildPrompt(req SuggestRequest) string {
	if req.Count == 0 {
		req.Count = 6
	}

	var b strings.Builder

	if len(req.ExistingHabits) > 0 {
		b.WriteString("Meine aktuellen Habits:\n")
		for _, h := range req.ExistingHabits {
			b.WriteString("- " + h + "\n")
		}
		b.WriteString("\n")
	}

	switch req.Routine {
	case "morning":
		b.WriteString(fmt.Sprintf(
			"Schlage mir %d Habits für eine starke Morgenroutine vor. "+
				"Fokus: Energie, Klarheit, guter Start in den Tag. "+
				"Zeitfenster: 5–60 Minuten gesamt.\n", req.Count))
	case "evening":
		b.WriteString(fmt.Sprintf(
			"Schlage mir %d Habits für eine Abendroutine vor. "+
				"Fokus: Runterfahren, Vorbereitung auf den nächsten Tag, guter Schlaf.\n", req.Count))
	case "health":
		b.WriteString(fmt.Sprintf(
			"Schlage mir %d Habits rund um Gesundheit vor. "+
				"Mix aus Bewegung, Ernährung, Schlaf, mentaler Gesundheit.\n", req.Count))
	case "learning":
		b.WriteString(fmt.Sprintf(
			"Schlage mir %d Habits zum Lernen und persönlicher Entwicklung vor. "+
				"Lesen, Schreiben, Sprachen, neue Skills.\n", req.Count))
	case "productivity":
		b.WriteString(fmt.Sprintf(
			"Schlage mir %d Produktivitäts-Habits vor. "+
				"Fokus, Deep Work, System-Gewohnheiten für Entwickler/Wissensarbeiter.\n", req.Count))
	default:
		b.WriteString(fmt.Sprintf(
			"Schlage mir %d Habits vor — einen guten Mix aus verschiedenen Lebensbereichen "+
				"(Gesundheit, Lernen, Produktivität, Mindfulness).\n", req.Count))
	}

	if req.Goal != "" {
		b.WriteString(fmt.Sprintf("\nMein Ziel: %s\n", req.Goal))
	}

	return b.String()
}
