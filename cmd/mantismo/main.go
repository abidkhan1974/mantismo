// Package main is the entry point for the mantismo CLI.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is injected at build time via -ldflags.
var version = "0.1.0-dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "mantismo",
		Short:        "Eyes on every agent",
		Long:         "Mantismo — a personal firewall for your AI agents. Own your context, lease it to agents on your terms.",
		SilenceUsage: true,
		Version:      version,
	}

	root.AddCommand(
		newWrapCmd(),
		newLogsCmd(),
		newToolsCmd(),
		newStatusCmd(),
		newPolicyCmd(),
		newVaultCmd(),
		newDashboardCmd(),
	)

	return root
}

func newWrapCmd() *cobra.Command {
	var preset string
	var logLevel string
	var noPolicy bool
	var noVault bool
	var port int
	var configPath string

	cmd := &cobra.Command{
		Use:   "wrap -- <command> [args...]",
		Short: "Wrap an MCP server with Mantismo proxy",
		Long:  "Start the Mantismo proxy and API server, wrapping the given MCP server command.",
		Example: `  mantismo wrap -- npx -y @modelcontextprotocol/server-github
  mantismo wrap --preset paranoid -- python my_mcp_server.py`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(os.Stderr, "Not implemented yet")
			return nil
		},
	}

	cmd.Flags().StringVar(&preset, "preset", "balanced", "Policy preset: paranoid, balanced, permissive")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level: debug, info, warn, error")
	cmd.Flags().BoolVar(&noPolicy, "no-policy", false, "Disable policy engine (logging only)")
	cmd.Flags().BoolVar(&noVault, "no-vault", false, "Disable vault tools injection")
	cmd.Flags().IntVar(&port, "port", 7777, "API server port")
	cmd.Flags().StringVar(&configPath, "config", "", "Path to config file")

	// Suppress unused variable warnings
	_ = preset
	_ = logLevel
	_ = noPolicy
	_ = noVault
	_ = port
	_ = configPath

	return cmd
}

func newLogsCmd() *cobra.Command {
	var since string
	var until string
	var tool string
	var method string
	var session string
	var decision string
	var limit int
	var jsonOut bool
	var follow bool
	var port int

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View and query audit logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(os.Stderr, "Not implemented yet")
			return nil
		},
	}

	cmd.Flags().StringVar(&since, "since", "", `Show logs since (e.g., "1h", "2026-03-05", "today")`)
	cmd.Flags().StringVar(&until, "until", "", "Show logs until")
	cmd.Flags().StringVar(&tool, "tool", "", "Filter by tool name")
	cmd.Flags().StringVar(&method, "method", "", "Filter by MCP method")
	cmd.Flags().StringVar(&session, "session", "", "Filter by session ID")
	cmd.Flags().StringVar(&decision, "decision", "", "Filter by policy decision (allow, deny, approve)")
	cmd.Flags().IntVar(&limit, "limit", 50, "Max entries to show")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output raw JSON")
	cmd.Flags().BoolVar(&follow, "follow", false, "Follow mode (live stream via WebSocket)")
	cmd.Flags().IntVar(&port, "port", 7777, "API server port")

	_ = since
	_ = until
	_ = tool
	_ = method
	_ = session
	_ = decision
	_ = limit
	_ = jsonOut
	_ = follow
	_ = port

	return cmd
}

func newToolsCmd() *cobra.Command {
	var changed bool
	var jsonOut bool
	var port int

	cmd := &cobra.Command{
		Use:   "tools",
		Short: "List tools seen across sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(os.Stderr, "Not implemented yet")
			return nil
		},
	}

	cmd.Flags().BoolVar(&changed, "changed", false, "Only show tools whose descriptions have changed")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	cmd.Flags().IntVar(&port, "port", 7777, "API server port")

	_ = changed
	_ = jsonOut
	_ = port

	return cmd
}

func newStatusCmd() *cobra.Command {
	var port int

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show Mantismo status",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(os.Stderr, "Not implemented yet")
			return nil
		},
	}

	cmd.Flags().IntVar(&port, "port", 7777, "API server port")
	_ = port

	return cmd
}

func newPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage security policies",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(os.Stderr, "Not implemented yet")
			return nil
		},
	}

	cmd.AddCommand(
		&cobra.Command{Use: "init", Short: "Generate starter policy from preset", RunE: notImplemented},
		&cobra.Command{Use: "check", Short: "Dry-run policy against recent logs", RunE: notImplemented},
		&cobra.Command{Use: "list", Short: "Show loaded policy rules", RunE: notImplemented},
		&cobra.Command{Use: "edit", Short: "Open policy file in $EDITOR", RunE: notImplemented},
	)

	return cmd
}

func newVaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault",
		Short: "Manage the personal data vault",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(os.Stderr, "Not implemented yet")
			return nil
		},
	}

	cmd.AddCommand(
		&cobra.Command{Use: "init", Short: "Initialize the vault", RunE: notImplemented},
		&cobra.Command{Use: "import", Short: "Interactive import wizard", RunE: notImplemented},
		&cobra.Command{Use: "list", Short: "List vault entries by category", RunE: notImplemented},
		&cobra.Command{Use: "get", Short: "Get a specific vault entry", RunE: notImplemented},
		&cobra.Command{Use: "set", Short: "Set a vault entry", RunE: notImplemented},
		&cobra.Command{Use: "delete", Short: "Delete a vault entry", RunE: notImplemented},
		&cobra.Command{Use: "export", Short: "Export vault data (decrypted, for backup)", RunE: notImplemented},
		&cobra.Command{Use: "lock", Short: "Lock the vault", RunE: notImplemented},
	)

	return cmd
}

func newDashboardCmd() *cobra.Command {
	var port int
	var open bool

	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Launch the local web dashboard",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(os.Stderr, "Not implemented yet")
			return nil
		},
	}

	cmd.Flags().IntVar(&port, "port", 7777, "Port")
	cmd.Flags().BoolVar(&open, "open", false, "Open browser automatically")

	_ = port
	_ = open

	return cmd
}

func notImplemented(cmd *cobra.Command, args []string) error {
	fmt.Fprintln(os.Stderr, "Not implemented yet")
	return nil
}
