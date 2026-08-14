package ai

import (
	"encoding/json"
	"os"
	"path/filepath"
)

func lastSuggestionsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "habctl", "last_suggestions.json"), nil
}

// SaveLastSuggestions caches the most recent `habctl suggest`/`habctl goal`
// result so a follow-up `habctl add <N>` (see cmd/add.go) can look one up
// by its printed number instead of requiring the full name — often
// emoji-prefixed, awkward to type or copy-paste correctly in a terminal —
// to be retyped. Overwrites whatever was cached before; there's only ever
// "the last suggestions shown", not a history.
func SaveLastSuggestions(suggestions []Suggestion) error {
	p, err := lastSuggestionsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(suggestions)
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

// LoadLastSuggestions reads back the cache SaveLastSuggestions wrote.
// Returns nil, nil (not an error) if nothing's been cached yet — callers
// should treat that the same as "not a valid number reference" and fall
// back to treating the arg as a literal habit name.
func LoadLastSuggestions() ([]Suggestion, error) {
	p, err := lastSuggestionsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var suggestions []Suggestion
	if err := json.Unmarshal(data, &suggestions); err != nil {
		return nil, err
	}
	return suggestions, nil
}
