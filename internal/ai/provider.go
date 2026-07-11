// Package ai provides multi-provider LLM support for habctl.
//
// Provider auto-detection (first key found wins):
//   ANTHROPIC_API_KEY  → Claude Haiku
//   OPENAI_API_KEY     → GPT-4o mini
//   GEMINI_API_KEY     → Gemini 2.0 Flash (via OpenAI-compatible endpoint)
//   OLLAMA_HOST or default localhost → Ollama (free, local)
//
// Override with HABCTL_PROVIDER=anthropic|openai|gemini|ollama
// or --provider flag on the suggest command.
package ai

import (
	"context"
	"fmt"
	"os"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicopt "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// Provider identifies which LLM backend to use.
type Provider string

const (
	ProviderAnthropic Provider = "anthropic"
	ProviderOpenAI    Provider = "openai"
	ProviderGemini    Provider = "gemini"
	ProviderOllama    Provider = "ollama"
)

// ProviderInfo describes a detected provider.
type ProviderInfo struct {
	Name    Provider
	Model   string
	Display string // human-readable label shown in TUI/CLI
}

// Detect returns the active provider based on environment variables.
// The HABCTL_PROVIDER env var overrides auto-detection.
func Detect() (ProviderInfo, error) {
	override := os.Getenv("HABCTL_PROVIDER")

	check := func(p Provider) bool {
		return override == "" || Provider(override) == p
	}

	if check(ProviderAnthropic) && os.Getenv("ANTHROPIC_API_KEY") != "" {
		return ProviderInfo{ProviderAnthropic, "claude-haiku-4-5-20251001", "Claude Haiku (Anthropic)"}, nil
	}
	if check(ProviderOpenAI) && os.Getenv("OPENAI_API_KEY") != "" {
		return ProviderInfo{ProviderOpenAI, "gpt-4o-mini", "GPT-4o mini (OpenAI)"}, nil
	}
	if check(ProviderGemini) && os.Getenv("GEMINI_API_KEY") != "" {
		return ProviderInfo{ProviderGemini, "gemini-2.0-flash", "Gemini 2.0 Flash (Google)"}, nil
	}
	if check(ProviderOllama) {
		model := os.Getenv("OLLAMA_MODEL")
		if model == "" {
			model = "llama3.2"
		}
		return ProviderInfo{ProviderOllama, model, fmt.Sprintf("Ollama (%s, local)", model)}, nil
	}

	if override != "" {
		return ProviderInfo{}, fmt.Errorf("HABCTL_PROVIDER=%q gesetzt, aber kein passender API-Key gefunden", override)
	}
	return ProviderInfo{}, fmt.Errorf(
		"kein API-Key gefunden — setze ANTHROPIC_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY, oder nutze Ollama lokal",
	)
}

// callAnthropic runs a blocking Anthropic call and streams chunks to out.
func callAnthropic(info ProviderInfo, system, prompt string, out func(string)) (string, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	c := anthropic.NewClient(anthropicopt.WithAPIKey(key))

	stream := c.Messages.NewStreaming(context.Background(), anthropic.MessageNewParams{
		Model:     anthropic.Model(info.Model),
		MaxTokens: 1024,
		System:    []anthropic.TextBlockParam{{Text: system}},
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
		return "", fmt.Errorf("Anthropic: %w", err)
	}
	return full.String(), nil
}

// callOpenAICompat runs a blocking OpenAI-compatible call and streams chunks to out.
// Works for OpenAI, Gemini (via compat endpoint), Ollama, Groq, etc.
func callOpenAICompat(info ProviderInfo, system, prompt string, out func(string)) (string, error) {
	var opts []option.RequestOption

	switch info.Name {
	case ProviderOpenAI:
		opts = append(opts, option.WithAPIKey(os.Getenv("OPENAI_API_KEY")))
	case ProviderGemini:
		opts = append(opts,
			option.WithAPIKey(os.Getenv("GEMINI_API_KEY")),
			option.WithBaseURL("https://generativelanguage.googleapis.com/v1beta/openai/"),
		)
	case ProviderOllama:
		host := os.Getenv("OLLAMA_HOST")
		if host == "" {
			host = "http://localhost:11434"
		}
		opts = append(opts,
			option.WithAPIKey("ollama"),
			option.WithBaseURL(host+"/v1/"),
		)
	}

	client := openai.NewClient(opts...)

	stream := client.Chat.Completions.NewStreaming(context.Background(),
		openai.ChatCompletionNewParams{
			Model: info.Model,
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.SystemMessage(system),
				openai.UserMessage(prompt),
			},
			MaxTokens: openai.Int(1024),
		},
	)

	var full strings.Builder
	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) > 0 {
			text := chunk.Choices[0].Delta.Content
			if text != "" {
				full.WriteString(text)
				if out != nil {
					out(text)
				}
			}
		}
	}
	if err := stream.Err(); err != nil {
		return "", fmt.Errorf("%s: %w", info.Display, friendlyNetErr(err))
	}
	return full.String(), nil
}

// friendlyNetErr replaces low-level Go network errors with readable messages.
func friendlyNetErr(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "lookup") || strings.Contains(msg, "no such host"):
		return fmt.Errorf("DNS-Fehler — Domain nicht erreichbar. VPN/Proxy/Firewall prüfen oder Ollama (lokal) nutzen")
	case strings.Contains(msg, "connection refused"):
		return fmt.Errorf("Verbindung abgelehnt — API-Server nicht erreichbar")
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded"):
		return fmt.Errorf("Timeout — API-Server antwortet nicht")
	case strings.Contains(msg, "404"):
		return fmt.Errorf("404 — API-Endpunkt nicht gefunden. Gemini: Key von aistudio.google.com holen (Google-Login → 'Get API key')")
	case strings.Contains(msg, "401") || strings.Contains(msg, "Unauthorized"):
		return fmt.Errorf("API-Key ungültig — Settings (S) öffnen und Key prüfen")
	case strings.Contains(msg, "403") || strings.Contains(msg, "Forbidden"):
		return fmt.Errorf("Zugriff verweigert — API-Key hat keine Berechtigung")
	}
	return err
}

// Call dispatches to the correct provider backend.
func Call(info ProviderInfo, system, prompt string, out func(string)) (string, error) {
	switch info.Name {
	case ProviderAnthropic:
		return callAnthropic(info, system, prompt, out)
	default:
		return callOpenAICompat(info, system, prompt, out)
	}
}
