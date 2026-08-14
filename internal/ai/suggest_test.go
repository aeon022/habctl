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

// TestParseSuggestions pins the "###" Name:/Time:/Benefit:/Tip: block
// format both the TUI's suggestion-accept flow and the CLI's add-hints
// (cmd/suggest.go's printAddHints) depend on — a regression here silently
// breaks both into "just a bare title, no content", which is exactly the
// failure mode reported live (a model that translated the field labels
// instead of just the bracketed content would trigger this the same way).
func TestParseSuggestions(t *testing.T) {
	raw := "\n###\nName: 🏋️ Daily Core Workout\nTime: 15 min/day\nBenefit: Builds core strength.\nTip: Start with planks.\n###\n\n###\nName: 📚 Read Daily\nBenefit: Expands knowledge.\n###\n"

	got := ParseSuggestions(raw)
	if len(got) != 2 {
		t.Fatalf("ParseSuggestions() returned %d items, want 2: %+v", len(got), got)
	}
	if got[0].Name != "🏋️ Daily Core Workout" || got[0].Time != "15 min/day" ||
		got[0].Benefit != "Builds core strength." || got[0].Tip != "Start with planks." {
		t.Errorf("got[0] = %+v, fields didn't parse as expected", got[0])
	}
	// Second block has no Time:/Tip: lines at all — must not carry over
	// empty-but-present values from the first block or panic.
	if got[1].Name != "📚 Read Daily" || got[1].Time != "" || got[1].Tip != "" {
		t.Errorf("got[1] = %+v, want Time/Tip empty (not present in this block)", got[1])
	}
}

func TestParseSuggestions_EmptyAndMalformedInput(t *testing.T) {
	if got := ParseSuggestions(""); len(got) != 0 {
		t.Errorf("ParseSuggestions(\"\") = %+v, want empty", got)
	}
	// A block with no "Name:" line at all is discarded rather than
	// producing a blank-titled entry.
	if got := ParseSuggestions("###\nBenefit: orphaned, no name\n###"); len(got) != 0 {
		t.Errorf("ParseSuggestions(no Name:) = %+v, want empty", got)
	}
}
