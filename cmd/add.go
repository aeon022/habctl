package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var addDesc string

var addCmd = &cobra.Command{
	Use:   "add <name>",
	Short: "Add a new habit",
	Long:  `Add a new habit to track. The name must be unique.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		habit, err := s.AddHabit(name, addDesc)
		if err != nil {
			return err
		}

		fmt.Printf("Added habit: %s (id: %d)\n", habit.Name, habit.ID)
		return nil
	},
}

func init() {
	addCmd.Flags().StringVar(&addDesc, "desc", "", "Optional description for the habit")
}
