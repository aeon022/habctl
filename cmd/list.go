package cmd

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var listFormat string

var (
	styleChecked = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))  // bright green
	styleMuted   = lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // dim gray
	styleHeader  = lipgloss.NewStyle().Bold(true).Underline(true)
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all habits with today's status",
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

		switch listFormat {
		case "diary":
			fmt.Println("## Habits")
			for _, st := range allStats {
				if st.CheckedToday {
					if st.Streak > 1 {
						fmt.Printf("- [x] %s (streak: %d)\n", st.Habit.Name, st.Streak)
					} else {
						fmt.Printf("- [x] %s\n", st.Habit.Name)
					}
				} else {
					fmt.Printf("- [ ] %s\n", st.Habit.Name)
				}
			}

		default:
			fmt.Println()
			fmt.Println("  " + styleHeader.Render("Habits"))
			fmt.Println()

			for _, st := range allStats {
				mark := styleMuted.Render("✗ today")
				nameStyle := styleMuted
				if st.CheckedToday {
					mark = styleChecked.Render("✓ today")
					nameStyle = lipgloss.NewStyle()
				}

				streakStr := fmt.Sprintf("streak: %d day", st.Streak)
				if st.Streak != 1 {
					streakStr += "s"
				}
				if st.Streak >= 7 {
					streakStr += " 🔥"
				}

				lastStr := "never"
				if st.LastCheckIn != nil {
					daysDiff := int(time.Since(*st.LastCheckIn).Hours() / 24)
					switch daysDiff {
					case 0:
						lastStr = "today"
					case 1:
						lastStr = "yesterday"
					default:
						lastStr = fmt.Sprintf("%d days ago", daysDiff)
					}
				}

				fmt.Printf("  %-20s  %s   %-22s  last: %s\n",
					nameStyle.Render(st.Habit.Name),
					mark,
					streakStr,
					lastStr,
				)
			}
			fmt.Println()
		}

		return nil
	},
}

func init() {
	listCmd.Flags().StringVar(&listFormat, "format", "table", "Output format: table or diary")
}
