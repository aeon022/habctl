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
	Short: "KI-Wochenreview — Coaching-Briefing der letzten 7 Tage",
	Long: `Analysiert deine letzten 7 Tage und erstellt ein persönliches Coaching-Briefing.

Zeigt: Überblick, Top-Habits, Kämpfe, Empfehlung, Tipp der Woche.
Im TUI: r Taste.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		data, err := s.GetWeeklyReview()
		if err != nil {
			return fmt.Errorf("Daten laden: %w", err)
		}
		if len(data.Habits) == 0 {
			fmt.Fprintln(os.Stderr, "Noch keine Habits. habctl add \"<Name>\" zum Starten.")
			return nil
		}

		lime := lipgloss.NewStyle().Foreground(lipgloss.Color("#84cc16")).Bold(true)
		muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#718096"))

		info, detErr := ai.Detect()
		providerLabel := ""
		if detErr == nil {
			providerLabel = muted.Render("via " + info.Display)
		} else {
			return fmt.Errorf("kein KI-Provider konfiguriert — %w", detErr)
		}

		fmt.Println()
		fmt.Println(lime.Render("habctl review") + "  " + providerLabel)
		fmt.Println()

		_, err = ai.Review(context.Background(), data, func(chunk string) {
			// Render section headers in lime, rest muted
			if strings.HasPrefix(chunk, "## ") {
				fmt.Print(lime.Render(strings.TrimPrefix(chunk, "## ")))
			} else {
				fmt.Print(chunk)
			}
			os.Stdout.Sync()
		})
		if err != nil {
			return fmt.Errorf("Review fehlgeschlagen: %w", err)
		}

		fmt.Println()
		fmt.Println()
		return nil
	},
}
