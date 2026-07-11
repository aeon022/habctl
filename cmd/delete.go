package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a habit and all its check-ins",
	Long:  `Permanently delete a habit and all associated check-in history.`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		fmt.Printf("Delete habit %q and all its check-ins? [y/N] ", name)
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(strings.ToLower(line))

		if line != "y" && line != "yes" {
			fmt.Println("Aborted.")
			return nil
		}

		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		if err := s.DeleteHabit(name); err != nil {
			return err
		}

		fmt.Printf("Deleted habit: %s\n", name)
		return nil
	},
}
