// Package config manages habctl's persistent configuration (~/.config/habctl/config.json).
// Environment variables always take precedence over config file values.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds all persisted settings.
type Config struct {
	Provider       string `json:"provider,omitempty"`
	AnthropicKey   string `json:"anthropic_api_key,omitempty"`
	OpenAIKey      string `json:"openai_api_key,omitempty"`
	GeminiKey      string `json:"gemini_api_key,omitempty"`
	OllamaHost     string `json:"ollama_host,omitempty"`
	OllamaModel    string `json:"ollama_model,omitempty"`
	// Google OAuth2 credentials (Desktop app from console.cloud.google.com).
	GoogleClientID     string `json:"google_client_id,omitempty"`
	GoogleClientSecret string `json:"google_client_secret,omitempty"`
	GoogleRefreshToken string `json:"google_refresh_token,omitempty"`
}

func path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "habctl", "config.json"), nil
}

// Load reads the config file. Returns an empty Config (no error) if the file doesn't exist.
func Load() (Config, error) {
	p, err := path()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Save writes cfg to disk, creating the directory if needed.
func Save(cfg Config) error {
	p, err := path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// ApplyToEnv sets environment variables from cfg for keys not already present in the environment.
// Call this early in main() so that ai.Detect() picks up saved keys.
func ApplyToEnv(cfg Config) {
	setIfMissing := func(key, val string) {
		if val != "" && os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
	setIfMissing("HABCTL_PROVIDER", cfg.Provider)
	setIfMissing("ANTHROPIC_API_KEY", cfg.AnthropicKey)
	setIfMissing("OPENAI_API_KEY", cfg.OpenAIKey)
	setIfMissing("GEMINI_API_KEY", cfg.GeminiKey)
	setIfMissing("OLLAMA_HOST", cfg.OllamaHost)
	setIfMissing("OLLAMA_MODEL", cfg.OllamaModel)
	setIfMissing("GOOGLE_CLIENT_ID", cfg.GoogleClientID)
	setIfMissing("GOOGLE_CLIENT_SECRET", cfg.GoogleClientSecret)
	setIfMissing("GOOGLE_REFRESH_TOKEN", cfg.GoogleRefreshToken)
}
