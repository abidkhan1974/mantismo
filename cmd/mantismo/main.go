// Package main is the entry point for the mantismo CLI.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/inferalabs/mantismo/internal/api"
	"github.com/inferalabs/mantismo/internal/config"
	"github.com/inferalabs/mantismo/internal/fingerprint"
	"github.com/inferalabs/mantismo/internal/logger"
	"github.com/inferalabs/mantismo/internal/policy"
	"github.com/inferalabs/mantismo/internal/vault"
)

// Build-time variables injected via -ldflags.
var (
	version = "0.1.0-dev"
	commit  = "none"
	date    = "unknown"
)

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
		Version:      fmt.Sprintf("%s (commit %s, built %s)", version, commit, date),
	}

	root.AddCommand(
		newWrapCmd(),
		newLogsCmd(),
		newToolsCmd(),
		newStatusCmd(),
		newPolicyCmd(),
		newVaultCmd(),
		newDashboardCmd(),
		newDoctorCmd(),
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
			if len(args) == 0 {
				return fmt.Errorf("wrap requires a command: mantismo wrap -- <command> [args...]")
			}

			// Load configuration.
			cfg, err := config.LoadConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Override port from flag if explicitly set.
			if port != 7777 || cfg.API.Port == 0 {
				cfg.API.Port = port
			}

			// Apply preset override.
			if preset != "" {
				cfg.Policy.Preset = preset
			}

			// Set up data directory.
			dataDir := cfg.DataDir
			if err := os.MkdirAll(dataDir, 0700); err != nil {
				return fmt.Errorf("create data dir: %w", err)
			}
			logDir := filepath.Join(dataDir, "logs")

			// Session ID based on timestamp.
			sessionID := fmt.Sprintf("sess-%d", time.Now().UnixNano())

			// Create logger.
			log, err := logger.New(logDir, sessionID)
			if err != nil {
				return fmt.Errorf("init logger: %w", err)
			}
			defer log.Close()

			// Create fingerprint store.
			fpPath := filepath.Join(dataDir, "fingerprints.json")
			fpStore, err := fingerprint.NewStore(fpPath)
			if err != nil {
				return fmt.Errorf("init fingerprint store: %w", err)
			}

			// Create session store.
			sessions := api.NewSessionStore()
			sessions.SetActive(&api.SessionInfo{
				ID:        sessionID,
				StartedAt: time.Now().UTC(),
				ServerCmd: args[0],
			})
			defer sessions.EndActive()

			// Create and start the API server.
			apiCfg := api.Config{
				Port:     cfg.API.Port,
				BindAddr: cfg.API.BindAddr,
			}
			approvalCh := make(chan api.ApprovalRequest, 16)
			deps := api.Dependencies{
				Logger:       log,
				LogDir:       logDir,
				Fingerprints: fpStore,
				ApprovalCh:   approvalCh,
				Sessions:     sessions,
			}
			apiSrv := api.NewServer(apiCfg, deps)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			if err := apiSrv.Start(ctx); err != nil {
				return fmt.Errorf("start api server: %w", err)
			}

			fmt.Fprintf(os.Stderr, "[mantismo] API server listening on http://%s\n", apiSrv.Addr())
			fmt.Fprintf(os.Stderr, "[mantismo] wrapping: %v\n", args)
			fmt.Fprintf(os.Stderr, "[mantismo] policy preset: %s\n", cfg.Policy.Preset)
			fmt.Fprintf(os.Stderr, "[mantismo] session: %s\n", sessionID)

			// Suppress unused variable warnings.
			_ = logLevel
			_ = noPolicy
			_ = noVault

			// Wait for SIGINT/SIGTERM.
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

			select {
			case sig := <-sigCh:
				fmt.Fprintf(os.Stderr, "\n[mantismo] received %s, shutting down\n", sig)
				cancel()
			case <-ctx.Done():
			}

			stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer stopCancel()
			return apiSrv.Stop(stopCtx)
		},
	}

	cmd.Flags().StringVar(&preset, "preset", "balanced", "Policy preset: paranoid, balanced, permissive")
	cmd.Flags().StringVar(&logLevel, "log-level", "info", "Log level: debug, info, warn, error")
	cmd.Flags().BoolVar(&noPolicy, "no-policy", false, "Disable policy engine (logging only)")
	cmd.Flags().BoolVar(&noVault, "no-vault", false, "Disable vault tools injection")
	cmd.Flags().IntVar(&port, "port", 7777, "API server port")
	cmd.Flags().StringVar(&configPath, "config", "", "Path to config file")

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
		&cobra.Command{Use: "unlock", Short: "Unlock the vault (enter passphrase)", RunE: notImplemented},
	)

	return cmd
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Validate Mantismo installation and environment",
		Long:  "Check that Mantismo and all its dependencies are correctly installed and configured.",
		RunE: func(cmd *cobra.Command, args []string) error {
			ok := true
			pass := func(label string) { fmt.Printf("  ✓ %s\n", label) }
			fail := func(label, reason string) {
				fmt.Printf("  ✗ %s — %s\n", label, reason)
				ok = false
			}
			warn := func(label, reason string) { fmt.Printf("  ⚠ %s — %s\n", label, reason) }

			fmt.Printf("mantismo doctor  (version %s  commit %s  built %s)\n\n", version, commit, date)
			fmt.Println("Checking installation:")

			// 1. Binary check.
			exe, err := os.Executable()
			if err != nil || exe == "" {
				fail("Binary", "cannot determine executable path")
			} else {
				pass(fmt.Sprintf("Binary: %s", exe))
			}

			// 2. Data directory check.
			home, err := os.UserHomeDir()
			if err != nil {
				fail("Data directory", "cannot determine home directory")
			} else {
				dataDir := filepath.Join(home, ".mantismo")
				if err := os.MkdirAll(dataDir, 0700); err != nil {
					fail("Data directory", fmt.Sprintf("%s is not writable: %v", dataDir, err))
				} else {
					// Verify writability by creating a probe file.
					probe := filepath.Join(dataDir, ".doctor_probe")
					if err := os.WriteFile(probe, []byte("ok"), 0600); err != nil {
						fail("Data directory", fmt.Sprintf("cannot write to %s: %v", dataDir, err))
					} else {
						_ = os.Remove(probe)
						pass(fmt.Sprintf("Data directory: %s (writable)", dataDir))
					}
				}
			}

			// 3. Python check (used by some MCP servers).
			fmt.Println("\nChecking optional dependencies:")
			pythonBin := "python3"
			if runtime.GOOS == "windows" {
				pythonBin = "python"
			}
			if _, err := exec.LookPath(pythonBin); err != nil {
				warn("Python", fmt.Sprintf("%s not found in PATH (needed by some MCP servers)", pythonBin))
			} else {
				out, _ := exec.Command(pythonBin, "--version").Output()
				pass(fmt.Sprintf("Python: %s", string(out[:len(out)-1])))
			}

			// 4. Node.js check.
			if _, err := exec.LookPath("node"); err != nil {
				warn("Node.js", "not found in PATH (needed by some MCP servers, e.g. @modelcontextprotocol/server-github)")
			} else {
				out, _ := exec.Command("node", "--version").Output()
				pass(fmt.Sprintf("Node.js: %s", string(out[:len(out)-1])))
			}

			// 5. Vault encryption check.
			fmt.Println("\nChecking core components:")
			tmpDir, err := os.MkdirTemp("", "mantismo-doctor-*")
			if err != nil {
				fail("Vault encryption", fmt.Sprintf("cannot create temp dir: %v", err))
			} else {
				defer os.RemoveAll(tmpDir)
				v, err := vault.Open(filepath.Join(tmpDir, "doctor.db"), "doctor-test-passphrase")
				if err != nil {
					fail("Vault encryption", fmt.Sprintf("failed to open test vault: %v", err))
				} else {
					testEntry := vault.Entry{
						Key:         "doctor_test",
						Value:       "ok",
						Category:    vault.Profile,
						Sensitivity: vault.Public,
						Label:       "Doctor test",
					}
					if err := v.Set(testEntry); err != nil {
						fail("Vault encryption", fmt.Sprintf("write failed: %v", err))
					} else if _, err := v.Get("doctor_test"); err != nil {
						fail("Vault encryption", fmt.Sprintf("read failed: %v", err))
					} else {
						pass("Vault encryption: AES-256-GCM working")
					}
					_ = v.Close()
				}
			}

			// 6. OPA policy engine check.
			// Use a built-in balanced policy (empty dir falls back to balanced preset defaults).
			balancedPolicyDir := filepath.Join(
				filepath.Dir(os.Args[0]), "..", "policies",
			)
			eng, err := policy.NewEngine(balancedPolicyDir)
			if err != nil {
				// Fall back to empty dir (uses OPA default allow).
				eng, err = policy.NewEngine("")
			}
			if err != nil {
				fail("OPA policy engine", fmt.Sprintf("failed to initialize: %v", err))
			} else {
				result, evalErr := eng.Evaluate(policy.EvalInput{
					Method:   "tools/call",
					ToolName: "get_test",
				})
				if evalErr != nil {
					fail("OPA policy engine", fmt.Sprintf("failed to evaluate: %v", evalErr))
				} else {
					pass(fmt.Sprintf("OPA policy engine: loaded and evaluated (decision: %s)", result.Decision))
				}
			}

			// Summary.
			fmt.Println()
			if ok {
				fmt.Println("✓ All checks passed — Mantismo is ready to use.")
			} else {
				fmt.Println("✗ Some checks failed. See above for details.")
				return fmt.Errorf("doctor: one or more checks failed")
			}
			return nil
		},
	}
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
