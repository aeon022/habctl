package cmd

import (
	"fmt"
	"os"

	"github.com/aeon022/habctl/internal/store"
	"github.com/aeon022/missionctl-core/doctor"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check database health",
	Run: func(cmd *cobra.Command, args []string) {
		var checks []doctor.Check
		if path, shared, err := store.ResolveDBPath(); err != nil {
			checks = append(checks, doctor.Check{Label: "Database", OK: false, Detail: fmt.Sprintf("resolving path: %v", err)})
		} else {
			checks = append(checks, doctor.CheckSQLite("Database", path, "habits"))
			checks = append(checks, doctor.CheckDataDir("Data directory", path, shared))
		}
		if !doctor.PrintReport(checks) {
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
