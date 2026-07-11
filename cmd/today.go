package cmd

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var todayCmd = &cobra.Command{
	Use:   "today",
	Short: "Show today's habit status at a glance",
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

		if len(allStats) == 0 {
			fmt.Println("No habits yet. Add one with: habctl add \"<name>\"")
			return nil
		}

		done := 0
		for _, st := range allStats {
			if st.CheckedToday {
				done++
			}
		}
		total := len(allStats)

		lime := lipgloss.NewStyle().Foreground(lipgloss.Color("#84cc16"))
		muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#718096"))
		ok := lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80"))
		bold := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e2e8f0"))

		fmt.Println()
		fmt.Printf("  %s  %s\n\n",
			lime.Bold(true).Render("Today's Habits"),
			muted.Render(fmt.Sprintf("%d / %d done", done, total)),
		)

		for _, st := range allStats {
			if st.CheckedToday {
				streakStr := ""
				if st.Streak > 1 {
					streakStr = muted.Render(fmt.Sprintf("  streak: %d", st.Streak))
					if st.Streak >= 7 {
						streakStr += " 🔥"
					}
				}
				fmt.Printf("  %s %s%s\n", ok.Render("✓"), bold.Render(st.Habit.Name), streakStr)
			} else {
				fmt.Printf("  %s %s\n", muted.Render("–"), muted.Render(st.Habit.Name))
			}
		}

		if done == total && total > 0 {
			fmt.Println()
			fmt.Println("  " + lime.Bold(true).Render("All habits done today! 🎉"))
		} else if done == 0 {
			fmt.Println()
			fmt.Println("  " + muted.Render("No check-ins yet today."))
		}

		fmt.Println()
		return nil
	},
}
