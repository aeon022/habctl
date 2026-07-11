package cmd

import (
	"github.com/aeon022/habctl/internal/mcpserver"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start the MCP server (stdio transport)",
	Long: `Start habctl as an MCP server over stdio.
Add this to your Claude Desktop or other MCP client configuration.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return mcpserver.Serve()
	},
}
