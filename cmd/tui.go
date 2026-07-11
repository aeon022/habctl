package cmd

import (
	"github.com/aeon022/habctl/internal/tui"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Open the interactive TUI",
	Long:  `Open the habctl TUI — navigate habits, check in, add, and delete.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()
		return tui.Run(s)
	},
}
