package cmd

import (
	"fmt"
	"os"

	"github.com/aeon022/habctl/internal/ai"
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
	Short: "AI-powered habit suggestions",
	Long: `Let Claude suggest habits based on your goals and existing habits.

Examples:
  habctl suggest
  habctl suggest --routine morning
  habctl suggest --routine health
  habctl suggest --goal "mehr Struktur und weniger Bildschirmzeit"`,
	RunE: func(cmd *cobra.Command, args []string) error {
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

		lime := lipgloss.NewStyle().Foreground(lipgloss.Color("#84cc16")).Bold(true)
		muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#718096"))

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

		_, err = ai.SuggestWithProvider(req, ai.Provider(suggestProvider), func(chunk string) {
			fmt.Print(chunk)
			os.Stdout.Sync()
		})
		if err != nil {
			return fmt.Errorf("could not generate suggestions: %w", err)
		}

		fmt.Println()
		fmt.Println()
		fmt.Println(muted.Render("Add with: habctl add \"<name>\""))
		fmt.Println()
		return nil
	},
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
