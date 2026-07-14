// Package ai provides multi-provider LLM support for habctl.
//
// Provider auto-detection (first key found wins):
//   ANTHROPIC_API_KEY  → Claude Haiku
//   OPENAI_API_KEY     → GPT-4o mini
//   GEMINI_API_KEY or GOOGLE_REFRESH_TOKEN → Gemini 2.0 Flash (via OpenAI-compatible endpoint)
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

// Call dispatches to the correct provider backend.
// ctx is used to cancel inflight requests; pass context.Background() for fire-and-forget callers.
func Call(ctx context.Context, info ProviderInfo, system, prompt string, out func(string)) (string, error) {
	switch info.Name {
	case ProviderAnthropic:
		return callAnthropic(ctx, info, system, prompt, out)
	default:
		return callOpenAICompat(ctx, info, system, prompt, out)
	}
}

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
	if check(ProviderGemini) && (os.Getenv("GEMINI_API_KEY") != "" || os.Getenv("GOOGLE_REFRESH_TOKEN") != "") {
		model := os.Getenv("GEMINI_MODEL")
		if model == "" {
			model = "gemini-flash-latest"
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
		return ProviderInfo{}, fmt.Errorf("HABCTL_PROVIDER=%q set but no matching API key found", override)
	}
	return ProviderInfo{}, fmt.Errorf(
		"no API key found — set ANTHROPIC_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY, or use Ollama locally",
	)
}

// callAnthropic runs a blocking Anthropic call and streams chunks to out.
func callAnthropic(ctx context.Context, info ProviderInfo, system, prompt string, out func(string)) (string, error) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	c := anthropic.NewClient(anthropicopt.WithAPIKey(key))

	stream := c.Messages.NewStreaming(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(info.Model),
		MaxTokens: 8192,
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
func callOpenAICompat(ctx context.Context, info ProviderInfo, system, prompt string, out func(string)) (string, error) {
	var opts []option.RequestOption

	switch info.Name {
	case ProviderOpenAI:
		opts = append(opts, option.WithAPIKey(os.Getenv("OPENAI_API_KEY")))
	case ProviderGemini:
		apiKey := geminiKey()
		opts = append(opts,
			option.WithAPIKey(apiKey),
			option.WithBaseURL("https://generativelanguage.googleapis.com/v1beta/openai/"),
			option.WithMaxRetries(0), // Gemini free tier: no automatic retries — each retry burns quota
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

	stream := client.Chat.Completions.NewStreaming(ctx,
		openai.ChatCompletionNewParams{
			Model: info.Model,
			Messages: []openai.ChatCompletionMessageParamUnion{
				openai.SystemMessage(system),
				openai.UserMessage(prompt),
			},
			MaxTokens: openai.Int(8192),
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
		return fmt.Errorf("DNS error — domain unreachable. Check VPN/proxy/firewall or use Ollama (local)")
	case strings.Contains(msg, "connection refused"):
		return fmt.Errorf("connection refused — API server not reachable")
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded"):
		return fmt.Errorf("timeout — API server not responding")
	case strings.Contains(msg, "429") || strings.Contains(msg, "Too Many Requests") ||
		strings.Contains(msg, "RESOURCE_EXHAUSTED") || strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "quota") || strings.Contains(msg, "Quota"):
		return friendlyForStatus(429, err)
	case strings.Contains(msg, "404"):
		return fmt.Errorf("404 — model not found. Set GEMINI_MODEL=gemini-2.0-flash or try a different model name")
	case strings.Contains(msg, "401") || strings.Contains(msg, "Unauthorized"):
		return fmt.Errorf("API key invalid — open Settings (S) and check the key")
	case strings.Contains(msg, "403") || strings.Contains(msg, "Forbidden"):
		return fmt.Errorf("access denied — API key has no permission for this resource")
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
		raw := orig.Error()
		if strings.Contains(raw, "PerDay") || strings.Contains(raw, "perDay") ||
			strings.Contains(raw, "daily") || strings.Contains(raw, "Daily") {
			return fmt.Errorf("429 daily limit exhausted (1500 req/day free tier). " +
				"Reset: daily at 00:00 PST. " +
				"Fix: create a new Google project (fresh quota) " +
				"or S → switch provider (Anthropic/OpenAI).")
		}
		return fmt.Errorf("429 per-minute limit (15 req/min free tier) — wait 60 s. " +
			"A new key in the same project won't help: quota is per project.")
	case 404:
		return fmt.Errorf("404 — model not available. Set GEMINI_MODEL=gemini-flash-latest or try a different model name")
	case 401:
		return fmt.Errorf("401 — API key invalid. Open Settings (S) and re-enter the key")
	case 403:
		return fmt.Errorf("403 — access denied. API key has no permission for this model")
	}
	return orig
}

