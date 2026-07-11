package cmd

import (
	"fmt"
	"os"

	"github.com/aeon022/habctl/internal/store"
	"github.com/aeon022/habctl/internal/tui"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "habctl",
	Short: "Habit tracker for the missionctl suite",
	Long: `habctl — terminal-first habit tracker.

Track daily habits, view streaks, and build consistency.
Use subcommands to manage habits from the CLI.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()
		return tui.Run(s)
	},
}

// Execute is the entry point called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(
		addCmd,
		checkCmd,
		listCmd,
		statsCmd,
		deleteCmd,
		mcpCmd,
		tuiCmd,
		todayCmd,
		remindCmd,
		suggestCmd,
	)
}

// openStore is a helper used by all commands.
func openStore() (*store.Store, error) {
	path, err := store.DefaultPath()
	if err != nil {
		return nil, fmt.Errorf("resolve db path: %w", err)
	}
	return store.Open(path)
}
