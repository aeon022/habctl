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

	LicenseKey    string `json:"license_key,omitempty"`
	LicenseStatus string `json:"license_status,omitempty"`
}

// defaultPolarOrgID is aeon022's Polar.sh organization — shared across the
// missionctl suite, same as postctl's.
const defaultPolarOrgID = "aa792ea4-650e-492e-a955-9b3d564e943e"

// IsPro reports whether a valid Pro/Bundle license is active on this
// machine — gates AI habit suggestions and the AI weekly review.
func IsPro() bool {
	cfg, err := Load()
	if err != nil {
		return false
	}
	return cfg.LicenseStatus == "active"
}

func PolarOrgID() string {
	if v := os.Getenv("HABCTL_POLAR_ORG_ID"); v != "" {
		return v
	}
	return defaultPolarOrgID
}

// SetLicense persists the license key/status to ~/.config/habctl/config.json.
func SetLicense(key, status string) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	cfg.LicenseKey = key
	cfg.LicenseStatus = status
	return Save(cfg)
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

// ApplyToEnv sets environment variables from cfg only when the variable is not
// already set in the process environment. Call this at startup so that shell
// env vars take precedence over saved config values.
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

// ForceApplyToEnv unconditionally overwrites environment variables from cfg.
// Call this after saving config changes so ai.Detect() immediately picks up
// the new values without needing a restart.
func ForceApplyToEnv(cfg Config) {
	set := func(key, val string) {
		if val != "" {
			os.Setenv(key, val)
		}
	}
	set("HABCTL_PROVIDER", cfg.Provider)
	set("ANTHROPIC_API_KEY", cfg.AnthropicKey)
	set("OPENAI_API_KEY", cfg.OpenAIKey)
	set("GEMINI_API_KEY", cfg.GeminiKey)
	set("OLLAMA_HOST", cfg.OllamaHost)
	set("OLLAMA_MODEL", cfg.OllamaModel)
	set("GOOGLE_CLIENT_ID", cfg.GoogleClientID)
	set("GOOGLE_CLIENT_SECRET", cfg.GoogleClientSecret)
	set("GOOGLE_REFRESH_TOKEN", cfg.GoogleRefreshToken)
}
