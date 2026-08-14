package cmd

import (
	"testing"

	"github.com/aeon022/habctl/internal/ai"
)

// TestResolveAddArgs covers the numbered-lookup feature end to end at the
// logic level (no store/DB needed): "habctl add <N>" should resolve to the
// Nth cached suggestion's own name and description, --desc should still
// override that description when passed explicitly, and anything that
// isn't a valid in-range number must fall through to being treated as a
// literal habit name — unchanged from before numbered lookup existed.
func TestResolveAddArgs(t *testing.T) {
	t.Cleanup(func() { ai.SaveLastSuggestions(nil) })

	suggestions := []ai.Suggestion{
		{Name: "🏃 Morning Run", Time: "20 min/day", Benefit: "Boosts energy.", Tip: "Start slow."},
		{Name: "📚 Evening Reading", Benefit: "Winds down the day."},
	}
	if err := ai.SaveLastSuggestions(suggestions); err != nil {
		t.Fatalf("SaveLastSuggestions() error = %v", err)
	}

	t.Run("number resolves to cached suggestion", func(t *testing.T) {
		name, desc := resolveAddArgs("1", "", false)
		if name != "🏃 Morning Run" {
			t.Errorf("name = %q, want %q", name, "🏃 Morning Run")
		}
		if desc != suggestions[0].Description() {
			t.Errorf("desc = %q, want %q", desc, suggestions[0].Description())
		}
	})

	t.Run("explicit --desc overrides the cached description", func(t *testing.T) {
		name, desc := resolveAddArgs("2", "my own description", true)
		if name != "📚 Evening Reading" {
			t.Errorf("name = %q, want %q", name, "📚 Evening Reading")
		}
		if desc != "my own description" {
			t.Errorf("desc = %q, want the explicit override %q", desc, "my own description")
		}
	})

	t.Run("out-of-range number falls back to literal name", func(t *testing.T) {
		name, desc := resolveAddArgs("99", "", false)
		if name != "99" {
			t.Errorf("name = %q, want the literal arg %q back", name, "99")
		}
		if desc != "" {
			t.Errorf("desc = %q, want empty", desc)
		}
	})

	t.Run("non-numeric arg is untouched", func(t *testing.T) {
		name, desc := resolveAddArgs("Drink Water", "stay hydrated", true)
		if name != "Drink Water" || desc != "stay hydrated" {
			t.Errorf("got (%q, %q), want the literal args passed through unchanged", name, desc)
		}
	})
}

// Zero/negative numbers aren't valid 1-based indices and must not panic on
// an empty or short cache — fall back to literal-name behavior instead.
func TestResolveAddArgs_NoCache(t *testing.T) {
	ai.SaveLastSuggestions(nil)
	t.Cleanup(func() { ai.SaveLastSuggestions(nil) })

	name, desc := resolveAddArgs("1", "", false)
	if name != "1" || desc != "" {
		t.Errorf("with no cache, got (%q, %q), want (\"1\", \"\") — literal fallback", name, desc)
	}
}
