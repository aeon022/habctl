package ai

import (
	"context"
	"fmt"
	"os"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
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

// ErrNoAPIKey is returned when ANTHROPIC_API_KEY is not set.
var ErrNoAPIKey = fmt.Errorf("ANTHROPIC_API_KEY nicht gesetzt")

func newClient() (*anthropic.Client, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		return nil, ErrNoAPIKey
	}
	c := anthropic.NewClient(option.WithAPIKey(key))
	return &c, nil
}

// SuggestRequest is the input for habit suggestions.
type SuggestRequest struct {
	ExistingHabits []string // current habits for context
	Routine        string   // "morning", "evening", "health", "learning", "productivity", ""
	Goal           string   // free-text goal, e.g. "mehr Struktur in den Tag"
	Count          int      // how many to suggest (default 6)
}

// Suggest calls Claude and streams habit suggestions to out.
// Returns the full text when done.
func Suggest(req SuggestRequest, out func(chunk string)) (string, error) {
	c, err := newClient()
	if err != nil {
		return "", err
	}

	if req.Count == 0 {
		req.Count = 6
	}

	prompt := buildPrompt(req)

	stream := c.Messages.NewStreaming(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5,
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: systemPromptSuggest}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(prompt))},
	})

	var full strings.Builder
	for stream.Next() {
		event := stream.Current()
		if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" {
			text := event.Delta.Text
			if text != "" {
				full.WriteString(text)
				if out != nil {
					out(text)
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return "", fmt.Errorf("Claude: %w", err)
	}
	return full.String(), nil
}

// SuggestBlocking is like Suggest but returns the full result without streaming.
func SuggestBlocking(req SuggestRequest) (string, error) {
	c, err := newClient()
	if err != nil {
		return "", err
	}

	if req.Count == 0 {
		req.Count = 6
	}

	prompt := buildPrompt(req)

	msg, err := c.Messages.New(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5,
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: systemPromptSuggest}},
		Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(prompt))},
	})
	if err != nil {
		return "", fmt.Errorf("Claude: %w", err)
	}
	if len(msg.Content) == 0 {
		return "", fmt.Errorf("leere Antwort von Claude")
	}
	return msg.Content[0].Text, nil
}

func buildPrompt(req SuggestRequest) string {
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
