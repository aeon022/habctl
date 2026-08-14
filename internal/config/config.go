// Package config manages habctl's persistent configuration (~/.config/habctl/config.json).
// Environment variables always take precedence over config file values.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/aeon022/missionctl-core/licensing"
)

// Config holds all persisted settings.
type Config struct {
	Provider     string `json:"provider,omitempty"`
	AnthropicKey string `json:"anthropic_api_key,omitempty"`
	OpenAIKey    string `json:"openai_api_key,omitempty"`
	GeminiKey    string `json:"gemini_api_key,omitempty"`
	OllamaHost   string `json:"ollama_host,omitempty"`
	OllamaModel  string `json:"ollama_model,omitempty"`
	// Google OAuth2 credentials (Desktop app from console.cloud.google.com).
	GoogleClientID     string `json:"google_client_id,omitempty"`
	GoogleClientSecret string `json:"google_client_secret,omitempty"`
	GoogleRefreshToken string `json:"google_refresh_token,omitempty"`

	LicenseKey       string `json:"license_key,omitempty"`
	LicenseStatus    string `json:"license_status,omitempty"`
	LicenseBenefitID string `json:"license_benefit_id,omitempty"`
}

// bundleBenefitID and habctlBenefitID identify the missionctl Bundle's and
// habctl's own individual-product license-key benefits in Polar. Both
// start empty (the habctl-only product doesn't exist in Polar yet) — see
// licensing.Result.Grants: empty IDs fall back to "any active key under
// our org grants access", so this is a no-op until both are filled in once
// the individual product is created and its benefit ID is known.
const (
	bundleBenefitID = "de1be860-1dfc-43da-99a8-206fb2573f09"
	habctlBenefitID = "1e88193f-749e-4e48-bab2-5ced533c9266"
)

// IsPro reports whether a valid Pro/Bundle or habctl-only license is
// active on this machine — gates AI habit suggestions and the AI weekly
// review.
func IsPro() bool {
	cfg, err := Load()
	if err != nil {
		return false
	}
	result := licensing.Result{Status: cfg.LicenseStatus, BenefitID: cfg.LicenseBenefitID}
	return result.Grants(habctlBenefitID, bundleBenefitID)
}

func PolarOrgID() string {
	if v := os.Getenv("HABCTL_POLAR_ORG_ID"); v != "" {
		return v
	}
	return licensing.DefaultOrgID
}

// SetLicense persists the license key/status/benefit to ~/.config/habctl/config.json.
func SetLicense(key, status, benefitID string) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	cfg.LicenseKey = key
	cfg.LicenseStatus = status
	cfg.LicenseBenefitID = benefitID
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

// ApplyToEnv sets environment variables from cfg. When force is false, a
// variable already set in the process environment is left alone (used at
// startup so shell env vars take precedence over saved config values). When
// force is true, values are unconditionally overwritten (used after saving
// config changes so ai.Detect() immediately picks up the new values without
// needing a restart).
func ApplyToEnv(cfg Config, force bool) {
	set := func(key, val string) {
		if val == "" {
			return
		}
		if !force && os.Getenv(key) != "" {
			return
		}
		os.Setenv(key, val)
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
