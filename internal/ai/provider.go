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
	"reflect"
	"strings"

	"github.com/aeon022/habctl/internal/auth"
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
		model := os.Getenv("GEMINI_MODEL")
		if model == "" {
			model = "gemini-1.5-flash"
		}
		return ProviderInfo{ProviderGemini, model, "Gemini " + model + " (Google)"}, nil
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
		apiKey := geminiKey()
		opts = append(opts,
			option.WithAPIKey(apiKey),
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

// geminiKey returns the best available credential for the Gemini API.
// If a Google OAuth2 refresh token is configured it exchanges it for an access
// token (which the Gemini endpoint accepts as a Bearer token). Falls back to
// the plain GEMINI_API_KEY when no OAuth credentials are present.
func geminiKey() string {
	rt := os.Getenv("GOOGLE_REFRESH_TOKEN")
	clientID := os.Getenv("GOOGLE_CLIENT_ID")
	clientSecret := os.Getenv("GOOGLE_CLIENT_SECRET")
	if rt != "" && clientID != "" && clientSecret != "" {
		if tok, err := auth.GetAccessToken(clientID, clientSecret, rt); err == nil {
			return tok
		}
	}
	return os.Getenv("GEMINI_API_KEY")
}

// friendlyNetErr replaces low-level Go network errors with readable messages.
//
// The openai-go SDK stores the HTTP status in apierror.Error.StatusCode, but
// that type is in an internal package. When Gemini returns a 429 the SDK's
// JSON unmarshal fails (Google uses "code":int, OpenAI expects "code":string),
// so the error text may not contain "429" at all. We therefore check both the
// error string AND the StatusCode field via reflection.
func friendlyNetErr(err error) error {
	if code, ok := httpStatusCode(err); ok {
		return friendlyForStatus(code, err)
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "lookup") || strings.Contains(msg, "no such host"):
		return fmt.Errorf("DNS-Fehler — Domain nicht erreichbar. VPN/Proxy/Firewall prüfen oder Ollama (lokal) nutzen")
	case strings.Contains(msg, "connection refused"):
		return fmt.Errorf("Verbindung abgelehnt — API-Server nicht erreichbar")
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded"):
		return fmt.Errorf("Timeout — API-Server antwortet nicht")
	case strings.Contains(msg, "429") || strings.Contains(msg, "Too Many Requests") ||
		strings.Contains(msg, "RESOURCE_EXHAUSTED") || strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "quota"):
		return friendlyForStatus(429, err)
	case strings.Contains(msg, "404"):
		return fmt.Errorf("404 — API-Endpunkt nicht gefunden. Gemini: Key von aistudio.google.com holen (Google-Login → 'Get API key')")
	case strings.Contains(msg, "401") || strings.Contains(msg, "Unauthorized"):
		return fmt.Errorf("API-Key ungültig — Settings (S) öffnen und Key prüfen")
	case strings.Contains(msg, "403") || strings.Contains(msg, "Forbidden"):
		return fmt.Errorf("Zugriff verweigert — API-Key hat keine Berechtigung")
	}
	return err
}

// httpStatusCode extracts the HTTP status code from an error via reflection.
// The openai-go SDK stores it in apierror.Error.StatusCode (internal package).
func httpStatusCode(err error) (int, bool) {
	if err == nil {
		return 0, false
	}
	v := reflect.ValueOf(err)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if f := v.FieldByName("StatusCode"); f.IsValid() && f.Kind() == reflect.Int {
		if code := int(f.Int()); code >= 400 {
			return code, true
		}
	}
	return 0, false
}

func friendlyForStatus(code int, orig error) error {
	switch code {
	case 429:
		return fmt.Errorf("429 Rate Limit / Quota — Gemini Free Tier: kurz warten oder Modell wechseln. " +
			"Quota prüfen: aistudio.google.com/u/0/quota")
	case 404:
		return fmt.Errorf("404 — API-Endpunkt nicht gefunden. Gemini: Key von aistudio.google.com holen")
	case 401:
		return fmt.Errorf("401 — API-Key ungültig. Settings (S) öffnen und Key neu eingeben")
	case 403:
		return fmt.Errorf("403 — Zugriff verweigert. API-Key hat keine Berechtigung für dieses Modell")
	}
	return orig
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
