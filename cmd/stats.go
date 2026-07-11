package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var statsDays int

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show habit statistics with progress bars",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		allStats, err := s.GetAllStats(statsDays)
		if err != nil {
			return err
		}

		if len(allStats) == 0 {
			fmt.Println("No habits yet. Add one with: habctl add \"<name>\"")
			return nil
		}

		fmt.Printf("\n  Habit Stats — last %d days\n\n", statsDays)

		for _, st := range allStats {
			streakStr := fmt.Sprintf("streak: %d", st.Streak)
			if st.Streak >= 7 {
				streakStr += " 🔥"
			}
			fmt.Printf("  %s (%s)\n", st.Habit.Name, streakStr)
			bar := progressBar(st.TotalDays, statsDays, 30)
			fmt.Printf("  %s %d/%d days\n\n", bar, st.TotalDays, statsDays)
		}

		return nil
	},
}

func progressBar(done, total, width int) string {
	if total == 0 {
		return "[" + strings.Repeat("░", width) + "]"
	}
	filled := (done * width) / total
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

func init() {
	statsCmd.Flags().IntVar(&statsDays, "days", 30, "Number of days to show stats for")
}
