package ai

import (
	"os"
	"strings"
	"testing"
)

// Regression test: --provider ollama (SuggestWithProvider -> detectForced)
// used to hardcode "llama3.2" regardless of OLLAMA_MODEL, unlike Detect()
// and SuggestOllama() which both already respected it — reproduced live:
// the printed status line said "via Ollama (mistral, local)" (from cmd/
// suggest.go's own separate Detect() call for display) while the actual
// request to Ollama asked for "llama3.2" and 404'd for anyone who'd only
// pulled their configured model.
func TestDetectForced_OllamaRespectsOllamaModelEnvVar(t *testing.T) {
	old := os.Getenv("OLLAMA_MODEL")
	defer os.Setenv("OLLAMA_MODEL", old)

	os.Setenv("OLLAMA_MODEL", "mistral")
	info, err := detectForced(ProviderOllama)
	if err != nil {
		t.Fatalf("detectForced(ProviderOllama) error = %v", err)
	}
	if info.Model != "mistral" {
		t.Errorf("Model = %q, want %q (OLLAMA_MODEL was set)", info.Model, "mistral")
	}
	if !strings.Contains(info.Display, "mistral") {
		t.Errorf("Display = %q, want it to mention %q", info.Display, "mistral")
	}

	os.Setenv("OLLAMA_MODEL", "")
	info, err = detectForced(ProviderOllama)
	if err != nil {
		t.Fatalf("detectForced(ProviderOllama) error = %v", err)
	}
	if info.Model != "llama3.2" {
		t.Errorf("Model = %q, want the default %q when OLLAMA_MODEL is unset", info.Model, "llama3.2")
	}
}
