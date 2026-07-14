package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aeon022/habctl/internal/ai"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "AI weekly review — coaching briefing for the last 7 days",
	Long: `Analyses your last 7 days and generates a personal coaching briefing.

Shows: overview, top habits, struggles, recommendation, tip of the week.
In TUI: press r.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		data, err := s.GetWeeklyReview()
		if err != nil {
			return fmt.Errorf("load data: %w", err)
		}
		if len(data.Habits) == 0 {
			fmt.Fprintln(os.Stderr, "No habits yet. Use habctl add \"<name>\" to get started.")
			return nil
		}

		lime := lipgloss.NewStyle().Foreground(lipgloss.Color("#84cc16")).Bold(true)
		muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#718096"))

		info, detErr := ai.Detect()
		providerLabel := ""
		if detErr == nil {
			providerLabel = muted.Render("via " + info.Display)
		} else {
			return fmt.Errorf("no AI provider configured — %w", detErr)
		}

		fmt.Println()
		fmt.Println(lime.Render("habctl review") + "  " + providerLabel)
		fmt.Println()

		_, err = ai.Review(context.Background(), data, func(chunk string) {
			if strings.HasPrefix(chunk, "## ") {
				fmt.Print(lime.Render(strings.TrimPrefix(chunk, "## ")))
			} else {
				fmt.Print(chunk)
			}
			os.Stdout.Sync()
		})
		if err != nil {
			return fmt.Errorf("review failed: %w", err)
		}

		fmt.Println()
		fmt.Println()
		return nil
	},
}
