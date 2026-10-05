package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aeon022/missionctl-core/licensing"
)

func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func TestLoadMissingFileIsEmptyNotError(t *testing.T) {
	isolate(t)
	c, err := Load()
	if err != nil || c != (Config{}) {
		t.Fatalf("Load = %+v, %v; want zero Config, nil", c, err)
	}
}

func TestSaveLoadRoundTripAndPermissions(t *testing.T) {
	home := isolate(t)
	want := Config{Provider: "gemini", GeminiKey: "g-secret", GoogleRefreshToken: "rt", LicenseKey: "L"}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil || got != want {
		t.Fatalf("Load = %+v, %v", got, err)
	}
	// the file holds API keys and a refresh token: owner-only
	fi, err := os.Stat(filepath.Join(home, ".config", "habctl", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("config.json mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestLoadCorruptFileReturnsError(t *testing.T) {
	home := isolate(t)
	dir := filepath.Join(home, ".config", "habctl")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "config.json"), []byte("{oops"), 0o600)
	if _, err := Load(); err == nil {
		t.Error("corrupt config must error, not silently reset (Save would then wipe the keys)")
	}
}

func TestApplyToEnvPrecedence(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "from-shell")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")
	cfg := Config{AnthropicKey: "from-file", OpenAIKey: "oa", GeminiKey: ""}

	ApplyToEnv(cfg, false)
	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "from-shell" {
		t.Errorf("startup apply overwrote the shell value: %q", got)
	}
	if got := os.Getenv("OPENAI_API_KEY"); got != "oa" {
		t.Errorf("unset var not filled from config: %q", got)
	}
	if os.Getenv("GEMINI_API_KEY") != "" {
		t.Error("empty config value must not set anything")
	}

	ApplyToEnv(cfg, true) // after saving in the TUI: config wins
	if got := os.Getenv("ANTHROPIC_API_KEY"); got != "from-file" {
		t.Errorf("force apply did not overwrite: %q", got)
	}
}

func TestSetLicenseAndIsPro(t *testing.T) {
	isolate(t)
	if IsPro() {
		t.Fatal("no license must not be Pro")
	}
	if err := SetLicense("KEY", "granted", bundleBenefitID); err != nil {
		t.Fatal(err)
	}
	if c, _ := Load(); c.LicenseKey != "KEY" || c.LicenseStatus != "granted" {
		t.Errorf("license not persisted: %+v", c)
	}
	if !IsPro() {
		t.Error("granted bundle key must be Pro")
	}
	if err := SetLicense("KEY", "granted", habctlBenefitID); err != nil || !IsPro() {
		t.Error("habctl-only key must be Pro")
	}
	if err := SetLicense("KEY", "granted", "some-other-product"); err != nil || IsPro() {
		t.Error("a key for a different product must not unlock habctl Pro")
	}
	if err := SetLicense("KEY", "revoked", bundleBenefitID); err != nil || IsPro() {
		t.Error("revoked key must not be Pro")
	}
	// SetLicense must keep unrelated settings
	_ = Save(Config{GeminiKey: "keep-me"})
	_ = SetLicense("K2", "granted", bundleBenefitID)
	if c, _ := Load(); c.GeminiKey != "keep-me" {
		t.Errorf("SetLicense dropped other settings: %+v", c)
	}
}

func TestPolarOrgIDEnvOverride(t *testing.T) {
	t.Setenv("HABCTL_POLAR_ORG_ID", "")
	if PolarOrgID() != licensing.DefaultOrgID {
		t.Error("default org id expected")
	}
	t.Setenv("HABCTL_POLAR_ORG_ID", "custom")
	if PolarOrgID() != "custom" {
		t.Error("env override ignored")
	}
}
