package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var remindCmd = &cobra.Command{
	Use:   "remind",
	Short: "Send a macOS notification for unchecked habits",
	Long: `Check which habits haven't been checked in today and send a
macOS notification. Ideal as a launchd job at 20:00.`,
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

		var pending []string
		for _, st := range allStats {
			if !st.CheckedToday {
				pending = append(pending, st.Habit.Name)
			}
		}

		if len(pending) == 0 {
			fmt.Println("All habits checked in today — nothing to remind.")
			return nil
		}

		title := fmt.Sprintf("%d habit(s) left today", len(pending))
		body := strings.Join(pending, ", ")

		script := fmt.Sprintf(`display notification %q with title %q`,
			body, title)
		out, err := exec.Command("osascript", "-e", script).CombinedOutput()
		if err != nil {
			// Not on macOS or osascript not available — fall back to terminal output.
			fmt.Printf("Reminder: %s — %s\n", title, body)
			if len(out) > 0 {
				fmt.Printf("osascript: %s\n", strings.TrimSpace(string(out)))
			}
			return nil
		}

		fmt.Printf("Notified: %s\n", body)
		return nil
	},
}
