package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/aeon022/habctl/internal/ai"
	"github.com/aeon022/habctl/internal/config"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	suggestRoutine  string
	suggestGoal     string
	suggestCount    int
	suggestProvider string
)

var suggestCmd = &cobra.Command{
	Use:   "suggest",
	Short: "AI-powered habit suggestions — missionctl Bundle feature",
	Long: `Let AI suggest habits based on your goals and existing habits.

Requires an active missionctl Bundle license — https://missionctl.sh/#pricing

Examples:
  habctl suggest
  habctl suggest --routine morning
  habctl suggest --routine health
  habctl suggest --goal "mehr Struktur und weniger Bildschirmzeit"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !config.IsPro() {
			return fmt.Errorf("habctl suggest is a missionctl Bundle feature — get it at https://missionctl.sh/#pricing, then: habctl license activate <key>")
		}
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		allStats, err := s.GetAllStats(30)
		if err != nil {
			return err
		}

		var existing []string
		for _, st := range allStats {
			existing = append(existing, st.Habit.Name)
		}

		// Light/Dark pairs match habctl's own AdaptiveColor set in
		// internal/tui/tui.go (colorLime/colorMuted) — this print path had
		// drifted to the Dark-only hex, invisible on a light terminal.
		lime := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#65a30d", Dark: "#84cc16"}).Bold(true)
		muted := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#64748b", Dark: "#718096"})

		label := "Habit Suggestions"
		if suggestRoutine != "" {
			routineLabels := map[string]string{
				"morning":      "Morning Routine",
				"evening":      "Evening Routine",
				"health":       "Health",
				"learning":     "Learning & Growth",
				"productivity": "Productivity",
			}
			if l, ok := routineLabels[suggestRoutine]; ok {
				label = l
			}
		}

		// Show which provider will be used.
		info, detErr := ai.Detect()
		providerLabel := ""
		if detErr == nil {
			providerLabel = muted.Render("via " + info.Display)
		}

		fmt.Println()
		fmt.Println(lime.Render("habctl suggest") + "  " + muted.Render(label) + "  " + providerLabel)
		fmt.Println()

		req := ai.SuggestRequest{
			ExistingHabits: existing,
			Routine:        suggestRoutine,
			Goal:           suggestGoal,
			Count:          suggestCount,
		}

		raw, err := ai.SuggestWithProvider(req, ai.Provider(suggestProvider), func(chunk string) {
			fmt.Print(chunk)
			os.Stdout.Sync()
		})
		if err != nil {
			return fmt.Errorf("could not generate suggestions: %w", err)
		}

		fmt.Println()
		fmt.Println()
		printAddHints(raw, muted, lime)
		fmt.Println()
		return nil
	},
}

// printAddHints prints one ready-to-run "habctl add" line per parsed
// suggestion, carrying its Benefit/Time/Tip over via --desc — replacing the
// old generic "Add with: habctl add \"<name>\"" hint, which pointed at a
// real command but silently discarded everything except the name (the CLI
// never parsed its own AI response, unlike the TUI's suggestion-accept flow,
// which already builds the description from these same fields). Falls back
// to the old generic hint if the response didn't parse into any suggestions
// at all — a model that didn't follow the format shouldn't leave the user
// with no hint whatsoever.
func printAddHints(raw string, muted, lime lipgloss.Style) {
	suggestions := ai.ParseSuggestions(raw)
	if len(suggestions) == 0 {
		fmt.Println(muted.Render("Add with: habctl add \"<name>\""))
		return
	}
	fmt.Println(muted.Render("Add with:"))
	for _, s := range suggestions {
		desc := s.Benefit
		if s.Time != "" {
			desc = "(" + s.Time + ") " + desc
		}
		if s.Tip != "" {
			desc += " Tip: " + s.Tip
		}
		escape := func(v string) string { return strings.ReplaceAll(v, `"`, `\"`) }
		line := fmt.Sprintf("  habctl add \"%s\"", escape(s.Name))
		if desc != "" {
			line = fmt.Sprintf("  habctl add \"%s\" --desc \"%s\"", escape(s.Name), escape(desc))
		}
		fmt.Println(lime.Render(line))
	}
}

func init() {
	suggestCmd.Flags().StringVar(&suggestRoutine, "routine", "",
		"Category: morning, evening, health, learning, productivity")
	suggestCmd.Flags().StringVar(&suggestGoal, "goal", "",
		"Your goal in your own words, e.g. \"more energy in the morning\"")
	suggestCmd.Flags().IntVar(&suggestCount, "count", 6,
		"Number of suggestions")
	suggestCmd.Flags().StringVar(&suggestProvider, "provider", "",
		"AI provider: anthropic, openai, gemini, ollama (default: auto-detect)")
}
