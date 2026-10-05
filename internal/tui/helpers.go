package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/aeon022/habctl/internal/ai"
	"github.com/aeon022/habctl/internal/auth"
	"github.com/aeon022/habctl/internal/models"
)

// ── util ─────────────────────────────────────────────────────────────────────

// wordWrap wraps text to width, delegating to lipgloss (already a direct
// dependency) instead of a hand-rolled word-wrap loop.
func wordWrap(text string, width int) string {
	return lipgloss.NewStyle().Width(width).Render(text)
}

func truncateDay(t time.Time) time.Time {
	y, mo, d := t.Date()
	return time.Date(y, mo, d, 0, 0, 0, 0, t.Location())
}

// splitIcon extracts a leading emoji from user input.
// "🏃 Morning run" → ("🏃", "Morning run")
// "Journal" → ("", "Journal")
func splitIcon(input string) (icon, name string) {
	r := []rune(strings.TrimSpace(input))
	if len(r) == 0 {
		return "", ""
	}
	first := r[0]
	// Emoji range: misc symbols, dingbats, or supplementary multilingual plane
	if first >= 0x2600 && first <= 0x27BF || first >= 0x1F000 {
		end := 1
		// skip variation selector U+FE0F
		if end < len(r) && r[end] == 0xFE0F {
			end++
		}
		// skip zero-width joiner sequences (basic check)
		icon = string(r[:end])
		name = strings.TrimSpace(string(r[end:]))
		if name == "" {
			return "", string(r) // only an emoji → treat as name
		}
		return icon, name
	}
	return "", string(r)
}

// parseSuggestions adapts ai.ParseSuggestions (the shared "###" Name:/Time:/
// Benefit:/Tip: block parser — see its doc comment) into this view's own
// suggestItem shape (header/details are display concerns specific to this
// list, not part of the shared format).
func parseSuggestions(text string) []suggestItem {
	var items []suggestItem
	for _, s := range ai.ParseSuggestions(text) {
		header := s.Name
		if s.Time != "" {
			header = s.Name + "  ·  " + s.Time
		}
		var details []string
		if s.Benefit != "" {
			details = append(details, s.Benefit)
		}
		if s.Tip != "" {
			details = append(details, "Tip: "+s.Tip)
		}
		items = append(items, suggestItem{name: s.Name, header: header, details: details})
	}
	return items
}

// parseChainSuggestions parses the From:/To:/Why: block format from SuggestChains.
func parseChainSuggestions(text string) []chainSuggestItem {
	var items []chainSuggestItem
	for _, block := range strings.Split(text, "###") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		var from, to, why string
		for _, raw := range strings.Split(block, "\n") {
			line := strings.TrimSpace(raw)
			switch {
			case strings.HasPrefix(line, "From:"):
				from = strings.TrimSpace(strings.TrimPrefix(line, "From:"))
			case strings.HasPrefix(line, "To:"):
				to = strings.TrimSpace(strings.TrimPrefix(line, "To:"))
			case strings.HasPrefix(line, "Why:"):
				why = strings.TrimSpace(strings.TrimPrefix(line, "Why:"))
			}
		}
		if from == "" || to == "" {
			continue
		}
		items = append(items, chainSuggestItem{from: from, to: to, reason: why})
	}
	return items
}

func groupByID(groups []models.Group, id int64) models.Group {
	for _, g := range groups {
		if g.ID == id {
			return g
		}
	}
	return models.Group{}
}

func startOAuth(clientID, clientSecret string) tea.Cmd {
	return func() tea.Msg {
		rt, err := auth.BrowserLogin(clientID, clientSecret)
		if err != nil {
			return oauthErrMsg{err}
		}
		return oauthSuccessMsg{rt}
	}
}
