package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var checkDate string

var checkCmd = &cobra.Command{
	Use:   "check <name>",
	Short: "Check in a habit for today",
	Long:  `Mark a habit as done for today (or a specific date).`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		date := time.Now()
		if checkDate != "" {
			parsed, err := time.ParseInLocation("2006-01-02", checkDate, time.Local)
			if err != nil {
				return fmt.Errorf("invalid date %q — expected YYYY-MM-DD", checkDate)
			}
			date = parsed
		}

		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		if err := s.CheckIn(name, date); err != nil {
			return err
		}

		// Fetch updated stats to show streak.
		stats, err := s.GetStats(name, 30)
		if err != nil {
			fmt.Printf("✓ %s — checked in\n", name)
			return nil
		}

		streakStr := fmt.Sprintf("%d day", stats.Streak)
		if stats.Streak != 1 {
			streakStr += "s"
		}
		msg := fmt.Sprintf("✓ %s — checked in (streak: %s)", name, streakStr)
		if stats.Streak >= 7 {
			msg += " 🔥"
		}
		fmt.Println(msg)
		return nil
	},
}

func init() {
	checkCmd.Flags().StringVar(&checkDate, "date", "", "Date to check in for (YYYY-MM-DD, default: today)")
}
