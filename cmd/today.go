package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var todayJSON bool

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

		done := 0
		for _, st := range allStats {
			if st.CheckedToday {
				done++
			}
		}
		total := len(allStats)

		if todayJSON {
			type habitToday struct {
				Name         string `json:"name"`
				CheckedToday bool   `json:"checked_today"`
				Streak       int    `json:"streak"`
			}
			data := make([]habitToday, 0, len(allStats))
			for _, st := range allStats {
				data = append(data, habitToday{
					Name:         st.Habit.Name,
					CheckedToday: st.CheckedToday,
					Streak:       st.Streak,
				})
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(map[string]any{
				"tool": "habctl", "command": "today",
				"done": done, "total": total,
				"data": data,
			})
		}

		if len(allStats) == 0 {
			fmt.Println("No habits yet. Add one with: habctl add \"<name>\"")
			return nil
		}

		// Light/Dark pairs match habctl's own AdaptiveColor set in
		// internal/tui/tui.go (colorLime/colorMuted/colorOk/colorFg) — this
		// print path had drifted to Dark-only hex values; "bold" in
		// particular was near-white (#e2e8f0), invisible on a light
		// terminal.
		lime := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#65a30d", Dark: "#84cc16"})
		muted := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#64748b", Dark: "#718096"})
		ok := lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#16a34a", Dark: "#4ade80"})
		bold := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#1e293b", Dark: "#e2e8f0"})

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

func init() {
	todayCmd.Flags().BoolVar(&todayJSON, "json", false, "Output as JSON")
}
