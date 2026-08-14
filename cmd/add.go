package cmd

import (
	"fmt"
	"strconv"

	"github.com/aeon022/habctl/internal/ai"
	"github.com/spf13/cobra"
)

var addDesc string

var addCmd = &cobra.Command{
	Use:   "add <name|N>",
	Short: "Add a new habit",
	Long: `Add a new habit to track. The name must be unique.

If N is a plain number, it's looked up in the suggestions from the most
recent "habctl suggest" instead of being used as a literal name — so
"habctl add 2" adds the second suggestion by name and description, without
having to type or copy-paste an emoji-prefixed title. --desc still
overrides the suggestion's own description if given explicitly.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, desc := resolveAddArgs(args[0], addDesc, cmd.Flags().Changed("desc"))

		s, err := openStore()
		if err != nil {
			return err
		}
		defer s.Close()

		habit, err := s.AddHabit(name, desc, "")
		if err != nil {
			return err
		}

		fmt.Printf("Added habit: %s (id: %d)\n", habit.Name, habit.ID)
		return nil
	},
}

// resolveAddArgs turns the raw <name|N> positional arg and --desc flag into
// the actual name/description to add. If arg is a plain positive number, it
// looks it up (1-based) against the most recent "habctl suggest" cache (see
// ai.SaveLastSuggestions) and, if found, uses that suggestion's own name and
// description — descChanged (cmd.Flags().Changed("desc")) lets an
// explicitly-passed --desc still override it. Falls through to treating arg
// as a literal name when there's no cache or the number is out of range,
// rather than erroring on what might be a genuinely intended (if unusual)
// numeric habit name — same as before numbered lookup existed.
func resolveAddArgs(arg, desc string, descChanged bool) (name, resolvedDesc string) {
	name, resolvedDesc = arg, desc
	n, err := strconv.Atoi(arg)
	if err != nil || n <= 0 {
		return name, resolvedDesc
	}
	suggestions, loadErr := ai.LoadLastSuggestions()
	if loadErr != nil || n > len(suggestions) {
		return name, resolvedDesc
	}
	s := suggestions[n-1]
	name = s.Name
	if !descChanged {
		resolvedDesc = s.Description()
	}
	return name, resolvedDesc
}

func init() {
	addCmd.Flags().StringVar(&addDesc, "desc", "", "Optional description for the habit")
}
