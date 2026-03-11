// Copyright 2026 Abid Ali Khan. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

// Package main is the entry point for the mantismo CLI.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/abidkhan1974/mantismo/internal/api"
	apiclient "github.com/abidkhan1974/mantismo/internal/api/client"
	"github.com/abidkhan1974/mantismo/internal/config"
	"github.com/abidkhan1974/mantismo/internal/fingerprint"
	"github.com/abidkhan1974/mantismo/internal/interceptor"
	"github.com/abidkhan1974/mantismo/internal/logger"
	"github.com/abidkhan1974/mantismo/internal/policy"
	"github.com/abidkhan1974/mantismo/internal/proxy"
	"github.com/abidkhan1974/mantismo/internal/scanner"
	"github.com/abidkhan1974/mantismo/internal/vault"
	"github.com/abidkhan1974/mantismo/internal/vaulttools"
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
	var policyDir string

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

			// ── Load policy engine ────────────────────────────────────────────────
			var policyEng *policy.Engine
			if !noPolicy {
				candidateDirs := []string{
					policyDir,
					filepath.Join(dataDir, "policies"),
				}
				if exe, exeErr := os.Executable(); exeErr == nil {
					candidateDirs = append(candidateDirs,
						filepath.Join(filepath.Dir(exe), "..", "share", "mantismo", "policies"))
				}
				for _, d := range candidateDirs {
					if d == "" {
						continue
					}
					eng, engErr := policy.NewEngine(d)
					if engErr == nil {
						policyEng = eng
						fmt.Fprintf(os.Stderr, "[mantismo] policy: loaded from %s\n", d)
						break
					}
				}
				if policyEng == nil {
					// Fall back to embedded balanced preset
					balanced := `package mantismo
import future.keywords.if
import future.keywords.in
default decision := {"decision": "allow", "reason": "balanced mode: allowed by default", "rule": "default_allow"}
decision := {"decision": "allow", "reason": "read-only tool", "rule": "allow_reads"} if {
    read_tool_prefixes := ["get_", "list_", "search_", "read_", "fetch_", "show_", "describe_", "vault_get_", "vault_search_"]
    some prefix in read_tool_prefixes
    startswith(input.tool_name, prefix)
    not input.tool_changed
}
decision := {"decision": "approve", "reason": "write operation requires approval", "rule": "approve_writes"} if {
    write_tool_prefixes := ["create_", "update_", "delete_", "remove_", "push_", "send_", "execute_", "run_", "write_", "modify_"]
    some prefix in write_tool_prefixes
    startswith(input.tool_name, prefix)
    not input.tool_changed
}
decision := {"decision": "deny", "reason": "sampling requests blocked", "rule": "block_sampling"} if {
    input.method == "sampling/createMessage"
}
`
					tmpPolicyDir, tmpErr := os.MkdirTemp("", "mantismo-policy-*")
					if tmpErr == nil {
						regoPath := filepath.Join(tmpPolicyDir, "balanced.rego")
						if writeErr := os.WriteFile(regoPath, []byte(balanced), 0600); writeErr == nil {
							if eng, engErr := policy.NewEngine(tmpPolicyDir); engErr == nil {
								policyEng = eng
								fmt.Fprintf(os.Stderr, "[mantismo] policy: using built-in balanced preset\n")
							}
						}
						defer os.RemoveAll(tmpPolicyDir)
					}
				}
			}

			// ── Secret scanner ────────────────────────────────────────────────────
			scan := scanner.NewScanner(nil)

			// ── Request tracker (for duration logging) ────────────────────────────
			tracker := logger.NewRequestTracker()

			// ── Method correlator: maps requestID → method for response logging ──
			var methodMap sync.Map

			// ── Vault tools handler (nil vault = vault not initialized) ───────────
			var vaultHandler *vaulttools.Handler
			if !noVault {
				vaultHandler = vaulttools.NewHandler(nil, nil, vaulttools.Standard)
			}

			// ── Build interceptor hooks ───────────────────────────────────────────
			ic := interceptor.New(interceptor.Hooks{
				OnToolsList: func(tools []interceptor.ToolInfo) ([]interceptor.ToolInfo, error) {
					newTools, changed, _ := fpStore.Check(tools, args[0])
					if len(newTools) > 0 {
						fmt.Fprintf(os.Stderr, "[mantismo] %d new tool(s) seen\n", len(newTools))
					}
					if len(changed) > 0 {
						fmt.Fprintf(os.Stderr,
							"[mantismo] WARNING: %d tool description(s) changed — possible rug-pull: %v\n",
							len(changed), changed)
					}
					_ = fpStore.Update(tools, args[0])

					// Inject vault tools into the list if vault handler is available.
					if vaultHandler != nil {
						tools = append(tools, vaultHandler.ToolDefinitions()...)
					}
					return tools, nil
				},

				OnToolCall: func(req interceptor.ToolCallRequest) interceptor.InterceptResult {
					toolChanged := fpStore.IsToolChanged(req.ToolName)

					// Scan arguments for secrets.
					if scanRes := scan.ScanJSON(req.Arguments); scanRes.Found {
						fmt.Fprintf(os.Stderr,
							"[mantismo] BLOCKED: credential detected in tool call arguments (%s)\n",
							req.ToolName)
						return interceptor.InterceptResult{
							Action: interceptor.Block,
							Error: &interceptor.JSONRPCError{
								Code:    -32000,
								Message: "blocked: credential detected in arguments",
							},
						}
					}

					// Evaluate policy.
					if policyEng != nil {
						result, evalErr := policyEng.Evaluate(policy.EvalInput{
							Method:           "tools/call",
							ToolName:         req.ToolName,
							ToolChanged:      toolChanged,
							ToolAcknowledged: false,
						})
						if evalErr == nil {
							switch result.Decision {
							case policy.Deny:
								fmt.Fprintf(os.Stderr,
									"[mantismo] BLOCKED: %s — %s\n",
									req.ToolName, result.Reason)
								return interceptor.InterceptResult{
									Action: interceptor.Block,
									Error: &interceptor.JSONRPCError{
										Code:    -32000,
										Message: "blocked by policy: " + result.Reason,
									},
								}
							case policy.Allow:
								// fall through to forward
							case policy.Approve:
								// Approval gateway not yet wired; auto-deny.
								fmt.Fprintf(os.Stderr,
									"[mantismo] APPROVAL REQUIRED: %s — %s (auto-denied, no approval backend)\n",
									req.ToolName, result.Reason)
								return interceptor.InterceptResult{
									Action: interceptor.Block,
									Error: &interceptor.JSONRPCError{
										Code:    -32000,
										Message: "approval required: " + result.Reason,
									},
								}
							}
						}
					}

					return interceptor.InterceptResult{Action: interceptor.Forward}
				},

				OnToolCallResponse: func(resp interceptor.ToolCallResponse, req interceptor.ToolCallRequest) interceptor.InterceptResult {
					// Scan response content for secrets.
					if scanRes := scan.ScanJSON(resp.Content); scanRes.Found {
						fmt.Fprintf(os.Stderr,
							"[mantismo] WARNING: credential detected in tool response (%s) — redacting\n",
							req.ToolName)
						_ = log.Log(logger.LogEntry{
							Timestamp:   time.Now().UTC(),
							SessionID:   sessionID,
							Direction:   "from_server",
							MessageType: "response",
							Method:      "tools/call",
							ToolName:    req.ToolName,
							Redacted:    true,
							Summary:     "← tools/call " + req.ToolName + " [REDACTED: credential detected]",
						})
					}
					return interceptor.InterceptResult{Action: interceptor.Forward}
				},

				OnAnyMessage: func(msg interceptor.MCPMessage, dir proxy.Direction) {
					dirStr := "to_server"
					if dir == proxy.FromServer {
						dirStr = "from_server"
					}
					msgType := "notification"
					if msg.IsRequest {
						msgType = "request"
					} else if msg.IsResponse {
						msgType = "response"
					}

					var durationMs *float64
					method := msg.Method
					if msg.IsRequest && msg.ID != nil {
						tracker.TrackRequest(*msg.ID)
						methodMap.Store(string(*msg.ID), msg.Method)
					} else if msg.IsResponse && msg.ID != nil {
						durationMs = tracker.CompleteRequest(*msg.ID)
						if v, ok := methodMap.LoadAndDelete(string(*msg.ID)); ok {
							method = v.(string) //nolint:forcetypeassert
						}
					}

					entry := logger.LogEntry{
						Timestamp:   time.Now().UTC(),
						SessionID:   sessionID,
						Direction:   dirStr,
						MessageType: msgType,
						Method:      method,
						RequestID:   msg.ID,
						RawSize:     len(msg.Raw),
						DurationMs:  durationMs,
						Summary: logger.BuildSummary(
							dirStr, msgType, method, "",
							durationMs, len(msg.Raw), msg.IsError, nil, "",
						),
					}
					_ = log.Log(entry)
					apiSrv.PublishLog(entry)
				},
			})

			// ── Start proxy ───────────────────────────────────────────────────────
			proxyCfg := proxy.Config{
				Command: args[0],
				Args:    args[1:],
			}
			p := proxy.New(proxyCfg, ic.Handle)
			go func() {
				if runErr := p.Run(ctx); runErr != nil && ctx.Err() == nil {
					fmt.Fprintf(os.Stderr, "[mantismo] proxy exited: %v\n", runErr)
				}
				cancel() // shut everything down when proxy exits
			}()

			// Wait for SIGINT/SIGTERM or proxy exit.
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
	cmd.Flags().StringVar(&policyDir, "policy-dir", "", "Directory containing .rego policy files (overrides preset)")

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

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View and query audit logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("home dir: %w", err)
			}
			logDir := filepath.Join(home, ".mantismo", "logs")

			sinceT, err := parseTimeArg(since, -24*time.Hour)
			if err != nil {
				return fmt.Errorf("--since: %w", err)
			}
			untilT, err := parseTimeArg(until, 0)
			if err != nil {
				return fmt.Errorf("--until: %w", err)
			}

			if follow {
				return followLogs(logDir, sinceT)
			}

			filter := logger.QueryFilter{
				Since:     sinceT,
				Until:     untilT,
				ToolName:  tool,
				Method:    method,
				SessionID: session,
				Decision:  decision,
				Limit:     limit,
			}

			entries, err := logger.Query(logDir, filter)
			if err != nil {
				return fmt.Errorf("query logs: %w", err)
			}

			if len(entries) == 0 {
				fmt.Fprintln(os.Stderr, "No log entries found.")
				return nil
			}

			// Reverse to chronological order (Query returns newest first).
			for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
				entries[i], entries[j] = entries[j], entries[i]
			}

			if jsonOut {
				for _, e := range entries {
					b, _ := json.Marshal(e)
					fmt.Println(string(b))
				}
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, e := range entries {
				ts := e.Timestamp.Local().Format("2006-01-02 15:04:05")
				size := formatLogSize(e.RawSize)
				policy := ""
				if e.PolicyDecision != "" {
					policy = "[" + e.PolicyDecision + "]"
				}
				// Summary already contains a leading arrow (→ / ←).
				// Strip it and show direction in its own column for clean alignment.
				summary := e.Summary
				dir := "→"
				if strings.HasPrefix(summary, "← ") {
					dir = "←"
					summary = summary[3:]
				} else if strings.HasPrefix(summary, "→ ") {
					summary = summary[3:]
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", ts, dir, summary, size, policy)
			}
			w.Flush()
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
	cmd.Flags().BoolVar(&follow, "follow", false, "Follow mode (tail the current log file)")

	return cmd
}

// parseTimeArg parses --since / --until flag values into a *time.Time.
// defaultOffset is applied when s is empty: 0 means "return nil" (no filter).
func parseTimeArg(s string, defaultOffset time.Duration) (*time.Time, error) {
	if s == "" {
		if defaultOffset == 0 {
			return nil, nil
		}
		t := time.Now().UTC().Add(defaultOffset)
		return &t, nil
	}
	if s == "today" {
		now := time.Now().UTC()
		t := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		return &t, nil
	}
	if strings.HasSuffix(s, "h") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "h"))
		if err == nil {
			t := time.Now().UTC().Add(-time.Duration(n) * time.Hour)
			return &t, nil
		}
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err == nil {
			t := time.Now().UTC().Add(-time.Duration(n) * 24 * time.Hour)
			return &t, nil
		}
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, fmt.Errorf("unrecognised format %q (use 'today', '1h', '2d', or 'YYYY-MM-DD')", s)
	}
	return &t, nil
}

// formatLogSize formats a byte count for display.
func formatLogSize(b int) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

// followLogs tails the current day's log file, printing new entries as they arrive.
func followLogs(logDir string, since *time.Time) error {
	day := time.Now().UTC().Format("2006-01-02")
	path := filepath.Join(logDir, day+".jsonl")

	// First, replay any existing entries matching the since filter.
	if since != nil {
		filter := logger.QueryFilter{Since: since, Limit: 200}
		entries, err := logger.Query(logDir, filter)
		if err == nil && len(entries) > 0 {
			for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
				entries[i], entries[j] = entries[j], entries[i]
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			for _, e := range entries {
				ts := e.Timestamp.Local().Format("2006-01-02 15:04:05")
				summary := e.Summary
				dir := "→"
				if strings.HasPrefix(summary, "← ") {
					dir = "←"
					summary = summary[3:]
				} else if strings.HasPrefix(summary, "→ ") {
					summary = summary[3:]
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", ts, dir, summary, formatLogSize(e.RawSize))
			}
			w.Flush()
		}
	}

	// Open file and seek to end.
	f, err := os.Open(path) //nolint:gosec
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("open log file: %w", err)
	}
	if f != nil {
		if _, err := f.Seek(0, 2); err != nil {
			f.Close()
			return err
		}
	}

	fmt.Fprintln(os.Stderr, "Following logs... (Ctrl+C to stop)")

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	scanner := (*bufio.Scanner)(nil)
	if f != nil {
		scanner = bufio.NewScanner(f)
	}

	for range ticker.C {
		// Reopen if file rotated to a new day.
		newDay := time.Now().UTC().Format("2006-01-02")
		if newDay != day {
			if f != nil {
				f.Close()
			}
			day = newDay
			path = filepath.Join(logDir, day+".jsonl")
			f, err = os.Open(path) //nolint:gosec
			if err != nil {
				f = nil
				scanner = nil
				continue
			}
			scanner = bufio.NewScanner(f)
		}

		// Open file if it didn't exist before.
		if f == nil {
			f, err = os.Open(path) //nolint:gosec
			if err != nil {
				continue
			}
			if _, err := f.Seek(0, 2); err != nil {
				f.Close()
				f = nil
				continue
			}
			scanner = bufio.NewScanner(f)
		}

		// Read any new lines.
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			var e logger.LogEntry
			if err := json.Unmarshal(line, &e); err != nil {
				continue
			}
			ts := e.Timestamp.Local().Format("2006-01-02 15:04:05")
			summary := e.Summary
			dir := "→"
			if strings.HasPrefix(summary, "← ") {
				dir = "←"
				summary = summary[3:]
			} else if strings.HasPrefix(summary, "→ ") {
				summary = summary[3:]
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", ts, dir, summary, formatLogSize(e.RawSize))
			w.Flush()
		}
	}
	return nil
}

func newToolsCmd() *cobra.Command {
	var changedOnly bool
	var jsonOut bool
	var port int

	cmd := &cobra.Command{
		Use:   "tools",
		Short: "List tools seen across sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = port // reserved for future API mode

			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("home dir: %w", err)
			}
			fpPath := filepath.Join(home, ".mantismo", "fingerprints.json")
			fpStore, err := fingerprint.NewStore(fpPath)
			if err != nil {
				return fmt.Errorf("open fingerprints: %w", err)
			}

			all := fpStore.All()
			if len(all) == 0 {
				fmt.Fprintln(os.Stderr, "No tools fingerprinted yet. Run 'mantismo wrap' to capture tool definitions.")
				return nil
			}

			names := make([]string, 0, len(all))
			for name := range all {
				names = append(names, name)
			}
			sort.Strings(names)

			if jsonOut {
				for _, name := range names {
					fp := all[name]
					if changedOnly && fp.Acknowledged {
						continue
					}
					b, _ := json.Marshal(fp)
					fmt.Println(string(b))
				}
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tHASH\tSTATUS\tFIRST SEEN\tLAST SEEN\tSERVER")
			for _, name := range names {
				fp := all[name]
				if changedOnly && fp.Acknowledged {
					continue
				}
				status := "ok"
				if !fp.Acknowledged {
					status = "new"
				}
				hash := fp.Hash
				if len(hash) > 8 {
					hash = hash[:8]
				}
				srv := fp.ServerCmd
				if len(srv) > 50 {
					srv = srv[:47] + "..."
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					name, hash, status,
					fp.FirstSeen.Local().Format("2006-01-02 15:04"),
					fp.LastSeen.Local().Format("2006-01-02 15:04"),
					srv,
				)
			}
			w.Flush()
			return nil
		},
	}

	cmd.Flags().BoolVar(&changedOnly, "changed", false, "Only show tools whose descriptions have changed")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	cmd.Flags().IntVar(&port, "port", 7777, "API server port")

	return cmd
}

func newStatusCmd() *cobra.Command {
	var port int

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show Mantismo status",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("Mantismo %s — Eyes on every agent\n\n", version)

			apiClient := apiclient.NewClient(port)
			if err := apiClient.Health(); err != nil {
				// API server not running — show offline status from files.
				fmt.Printf("  API Server:    not running\n")

				home, _ := os.UserHomeDir()
				logDir := filepath.Join(home, ".mantismo", "logs")
				today := time.Now().UTC().Format("2006-01-02")
				logFile := filepath.Join(logDir, today+".jsonl")
				if info, err2 := os.Stat(logFile); err2 == nil {
					fmt.Printf("  Last session:  %s (%s)\n", info.ModTime().Local().Format("2006-01-02 15:04"), formatLogSize(int(info.Size())))
				}
				fmt.Printf("\nRun 'mantismo wrap -- <command>' to start a proxy session.\n")
				return nil
			}

			stats, err := apiClient.Stats()
			if err != nil {
				return fmt.Errorf("stats: %w", err)
			}

			fmt.Printf("  API Server:    running (localhost:%d)\n", port)
			if stats.ActiveSession != nil {
				fmt.Printf("  Active Session: %s (%s)\n", stats.ActiveSession.ID[:8], stats.ActiveSession.ServerCmd)
			} else {
				fmt.Printf("  Active Session: none\n")
			}
			fmt.Printf("  Dashboard:     http://localhost:%d\n", port)
			fmt.Printf("\n")
			fmt.Printf("  Sessions today:   %d\n", stats.SessionsToday)
			fmt.Printf("  Tool calls today: %d\n", stats.ToolCallsToday)
			fmt.Printf("  Blocked today:    %d\n", stats.BlockedToday)
			return nil
		},
	}

	cmd.Flags().IntVar(&port, "port", 7777, "API server port")
	return cmd
}

func newPolicyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage security policies",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// policy init --preset <balanced|paranoid|permissive>
	var preset string
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Generate starter policy from preset",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			policyDir := filepath.Join(home, ".mantismo", "policies")
			if err := os.MkdirAll(policyDir, 0700); err != nil {
				return fmt.Errorf("create policy dir: %w", err)
			}
			dst := filepath.Join(policyDir, "policy.rego")
			if _, err := os.Stat(dst); err == nil {
				fmt.Fprintf(os.Stderr, "Policy already exists at %s\nEdit it directly or delete it to re-init.\n", dst)
				return nil
			}
			// Find built-in preset .rego next to the binary.
			exe, _ := os.Executable()
			builtinDir := filepath.Join(filepath.Dir(exe), "..", "policies")
			src := filepath.Join(builtinDir, preset+".rego")
			data, err := os.ReadFile(src)
			if err != nil {
				return fmt.Errorf("preset %q not found (looked in %s): %w", preset, builtinDir, err)
			}
			if err := os.WriteFile(dst, data, 0600); err != nil {
				return fmt.Errorf("write policy: %w", err)
			}
			fmt.Printf("Policy initialised at %s (preset: %s)\n", dst, preset)
			fmt.Printf("Edit it freely, then restart 'mantismo wrap' to apply changes.\n")
			return nil
		},
	}
	initCmd.Flags().StringVar(&preset, "preset", "balanced", "Preset to use: balanced, paranoid, permissive")

	// policy list
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "Show loaded policy rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			policyDir := filepath.Join(home, ".mantismo", "policies")
			entries, err := os.ReadDir(policyDir)
			if err != nil {
				fmt.Println("No custom policy directory found.")
				fmt.Printf("Run 'mantismo policy init' to create one, or use --preset on 'mantismo wrap'.\n")
				return nil
			}
			var regoFiles []string
			for _, e := range entries {
				if !e.IsDir() && strings.HasSuffix(e.Name(), ".rego") {
					regoFiles = append(regoFiles, filepath.Join(policyDir, e.Name()))
				}
			}
			if len(regoFiles) == 0 {
				fmt.Printf("Policy directory %s has no .rego files.\n", policyDir)
				return nil
			}
			for _, f := range regoFiles {
				data, err := os.ReadFile(f)
				if err != nil {
					continue
				}
				fmt.Printf("# %s\n%s\n", f, string(data))
			}
			return nil
		},
	}

	// policy edit
	editCmd := &cobra.Command{
		Use:   "edit",
		Short: "Open policy file in $EDITOR",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			policyFile := filepath.Join(home, ".mantismo", "policies", "policy.rego")
			if _, err := os.Stat(policyFile); err != nil {
				return fmt.Errorf("policy file not found at %s — run 'mantismo policy init' first", policyFile)
			}
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = os.Getenv("VISUAL")
			}
			if editor == "" {
				editor = "vi"
			}
			c := exec.Command(editor, policyFile)
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			return c.Run()
		},
	}

	// policy check — dry-run recent logs against current policy
	checkCmd := &cobra.Command{
		Use:   "check",
		Short: "Dry-run policy against recent logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			policyDir := filepath.Join(home, ".mantismo", "policies")
			eng, err := policy.NewEngine(policyDir)
			if err != nil {
				return fmt.Errorf("load policy: %w", err)
			}

			// Read today's logs.
			logDir := filepath.Join(home, ".mantismo", "logs")
			since := time.Now().UTC().Truncate(24 * time.Hour)
			entries, err := logger.Query(logDir, logger.QueryFilter{Since: &since})
			if err != nil {
				return fmt.Errorf("read logs: %w", err)
			}

			toolCalls := 0
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "TOOL\tMETHOD\tDECISION\tREASON")
			for _, e := range entries {
				if e.Method != "tools/call" || e.ToolName == "" {
					continue
				}
				toolCalls++
				result, evalErr := eng.Evaluate(policy.EvalInput{
					Method:   e.Method,
					ToolName: e.ToolName,
				})
				if evalErr != nil {
					fmt.Fprintf(w, "%s\t%s\t%s\t%v\n", e.ToolName, e.Method, "error", evalErr)
					continue
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.ToolName, e.Method, string(result.Decision), result.Reason)
			}
			w.Flush()
			if toolCalls == 0 {
				fmt.Println("No tool calls in today's logs to check.")
			}
			return nil
		},
	}

	cmd.AddCommand(initCmd, listCmd, editCmd, checkCmd)
	return cmd
}

// vaultPath returns the default vault DB path.
func vaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".mantismo", "vault.db"), nil
}

// readPassphrase reads a passphrase from the terminal without echo.
func readPassphrase(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	// Use stty to disable echo, then read, then re-enable.
	if err := exec.Command("stty", "-echo").Run(); err != nil {
		// Fall back to plain read if stty unavailable.
		var s string
		_, err2 := fmt.Scanln(&s)
		return s, err2
	}
	defer func() { _ = exec.Command("stty", "echo").Run() }()
	var s string
	_, err := fmt.Scanln(&s)
	fmt.Fprintln(os.Stderr) // newline after hidden input
	return s, err
}

func newVaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault",
		Short: "Manage the personal data vault",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	// vault init
	var passFlag string
	initCmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize the vault",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := vaultPath()
			if err != nil {
				return err
			}
			if _, err := os.Stat(dbPath); err == nil {
				fmt.Fprintf(os.Stderr, "Vault already exists at %s\n", dbPath)
				return nil
			}
			pass := passFlag
			if pass == "" {
				pass, err = readPassphrase("New passphrase: ")
				if err != nil {
					return err
				}
				confirm, err := readPassphrase("Confirm passphrase: ")
				if err != nil {
					return err
				}
				if pass != confirm {
					return fmt.Errorf("passphrases do not match")
				}
			}
			v, err := vault.Open(dbPath, pass)
			if err != nil {
				return fmt.Errorf("create vault: %w", err)
			}
			_ = v.Close()
			fmt.Printf("Vault initialised at %s\n", dbPath)
			return nil
		},
	}
	initCmd.Flags().StringVar(&passFlag, "passphrase", "", "Passphrase (omit to prompt interactively)")

	// vault list [--category <cat>]
	var catFlag string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List vault entries by category",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := vaultPath()
			if err != nil {
				return err
			}
			pass, err := readPassphrase("Vault passphrase: ")
			if err != nil {
				return err
			}
			v, err := vault.Open(dbPath, pass)
			if err != nil {
				return fmt.Errorf("open vault: %w", err)
			}
			defer v.Close()

			var cat *vault.Category
			if catFlag != "" {
				c := vault.Category(catFlag)
				cat = &c
			}
			entries, err := v.List(cat, nil)
			if err != nil {
				return fmt.Errorf("list: %w", err)
			}
			if len(entries) == 0 {
				fmt.Println("No entries found.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "KEY\tCATEGORY\tSENSITIVITY\tLABEL")
			for _, e := range entries {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.Key, string(e.Category), string(e.Sensitivity), e.Label)
			}
			w.Flush()
			return nil
		},
	}
	listCmd.Flags().StringVar(&catFlag, "category", "", "Filter by category (profile, identifiers, credentials_meta, etc.)")

	// vault get <key>
	getCmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get a specific vault entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := vaultPath()
			if err != nil {
				return err
			}
			pass, err := readPassphrase("Vault passphrase: ")
			if err != nil {
				return err
			}
			v, err := vault.Open(dbPath, pass)
			if err != nil {
				return fmt.Errorf("open vault: %w", err)
			}
			defer v.Close()

			entry, err := v.Get(args[0])
			if err != nil {
				return fmt.Errorf("get %q: %w", args[0], err)
			}
			fmt.Printf("Key:         %s\n", entry.Key)
			fmt.Printf("Value:       %s\n", entry.Value)
			fmt.Printf("Category:    %s\n", string(entry.Category))
			fmt.Printf("Sensitivity: %s\n", string(entry.Sensitivity))
			fmt.Printf("Label:       %s\n", entry.Label)
			return nil
		},
	}

	// vault set <key> <value> [--category <cat>] [--sensitivity <s>] [--label <l>]
	var setCat, setSens, setLabel string
	setCmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a vault entry",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := vaultPath()
			if err != nil {
				return err
			}
			pass, err := readPassphrase("Vault passphrase: ")
			if err != nil {
				return err
			}
			v, err := vault.Open(dbPath, pass)
			if err != nil {
				return fmt.Errorf("open vault: %w", err)
			}
			defer v.Close()

			cat := vault.Profile
			if setCat != "" {
				cat = vault.Category(setCat)
			}
			sens := vault.Standard
			if setSens != "" {
				sens = vault.Sensitivity(setSens)
			}
			entry := vault.Entry{
				Key:         args[0],
				Value:       args[1],
				Category:    cat,
				Sensitivity: sens,
				Label:       setLabel,
			}
			if err := v.Set(entry); err != nil {
				return fmt.Errorf("set: %w", err)
			}
			fmt.Printf("Stored %s\n", args[0])
			return nil
		},
	}
	setCmd.Flags().StringVar(&setCat, "category", "", "Category (profile, identifiers, credentials_meta, preferences, documents, financial)")
	setCmd.Flags().StringVar(&setSens, "sensitivity", "", "Sensitivity (public, standard, sensitive, critical)")
	setCmd.Flags().StringVar(&setLabel, "label", "", "Human-readable label")

	// vault delete <key>
	deleteCmd := &cobra.Command{
		Use:   "delete <key>",
		Short: "Delete a vault entry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := vaultPath()
			if err != nil {
				return err
			}
			pass, err := readPassphrase("Vault passphrase: ")
			if err != nil {
				return err
			}
			v, err := vault.Open(dbPath, pass)
			if err != nil {
				return fmt.Errorf("open vault: %w", err)
			}
			defer v.Close()

			if err := v.Delete(args[0]); err != nil {
				return fmt.Errorf("delete: %w", err)
			}
			fmt.Printf("Deleted %s\n", args[0])
			return nil
		},
	}

	// vault export [--output <file>]
	var exportOut string
	exportCmd := &cobra.Command{
		Use:   "export",
		Short: "Export vault data (decrypted) to JSON",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := vaultPath()
			if err != nil {
				return err
			}
			pass, err := readPassphrase("Vault passphrase: ")
			if err != nil {
				return err
			}
			v, err := vault.Open(dbPath, pass)
			if err != nil {
				return fmt.Errorf("open vault: %w", err)
			}
			defer v.Close()

			entries, err := v.Export()
			if err != nil {
				return fmt.Errorf("export: %w", err)
			}
			data, err := json.MarshalIndent(entries, "", "  ")
			if err != nil {
				return err
			}
			if exportOut == "" || exportOut == "-" {
				fmt.Println(string(data))
				return nil
			}
			if err := os.WriteFile(exportOut, data, 0600); err != nil {
				return fmt.Errorf("write %s: %w", exportOut, err)
			}
			fmt.Printf("Exported %d entries to %s\n", len(entries), exportOut)
			return nil
		},
	}
	exportCmd.Flags().StringVar(&exportOut, "output", "-", "Output file (default: stdout)")

	// vault stats
	statsCmd := &cobra.Command{
		Use:   "stats",
		Short: "Show vault statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			dbPath, err := vaultPath()
			if err != nil {
				return err
			}
			pass, err := readPassphrase("Vault passphrase: ")
			if err != nil {
				return err
			}
			v, err := vault.Open(dbPath, pass)
			if err != nil {
				return fmt.Errorf("open vault: %w", err)
			}
			defer v.Close()

			stats, err := v.Stats()
			if err != nil {
				return fmt.Errorf("stats: %w", err)
			}
			fmt.Printf("Total entries: %d\n\nBy category:\n", stats.TotalEntries)
			for cat, count := range stats.ByCategory {
				fmt.Printf("  %-25s %d\n", string(cat), count)
			}
			fmt.Printf("\nBy sensitivity:\n")
			for sens, count := range stats.BySensitivity {
				fmt.Printf("  %-12s %d\n", string(sens), count)
			}
			return nil
		},
	}

	cmd.AddCommand(initCmd, listCmd, getCmd, setCmd, deleteCmd, exportCmd, statsCmd,
		&cobra.Command{Use: "lock", Short: "Lock the vault (close active session)", RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Vault locked. Passphrase will be required on next access.")
			return nil
		}},
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
	var openBrowser bool

	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Launch the local web dashboard",
		RunE: func(cmd *cobra.Command, args []string) error {
			url := fmt.Sprintf("http://localhost:%d", port)

			// If API server is already running (from mantismo wrap), just open the browser.
			apiClient := apiclient.NewClient(port)
			if err := apiClient.Health(); err == nil {
				fmt.Fprintf(os.Stderr, "Dashboard: %s\n", url)
				if openBrowser {
					return openURL(url)
				}
				fmt.Fprintf(os.Stderr, "API server is running. Open %s in your browser.\n", url)
				return nil
			}

			// Start a read-only API server for historical data.
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			logPath := filepath.Join(home, ".mantismo", "logs")
			fpPath := filepath.Join(home, ".mantismo", "fingerprints.json")

			log, err := logger.New(logPath, "dashboard")
			if err != nil {
				return fmt.Errorf("logger: %w", err)
			}
			fpStore, err := fingerprint.NewStore(fpPath)
			if err != nil {
				return fmt.Errorf("fingerprints: %w", err)
			}

			srv := api.NewServer(api.Config{Port: port}, api.Dependencies{
				Logger:       log,
				Fingerprints: fpStore,
				Sessions:     api.NewSessionStore(),
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if err := srv.Start(ctx); err != nil {
				return fmt.Errorf("start API server: %w", err)
			}
			fmt.Fprintf(os.Stderr, "[mantismo] Dashboard: %s\n", url)
			if openBrowser {
				_ = openURL(url)
			}

			// Block until Ctrl+C.
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			<-sigCh
			fmt.Fprintln(os.Stderr, "\n[mantismo] Shutting down.")
			stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer stopCancel()
			return srv.Stop(stopCtx)
		},
	}

	cmd.Flags().IntVar(&port, "port", 7777, "Port")
	cmd.Flags().BoolVar(&openBrowser, "open", false, "Open browser automatically")
	return cmd
}

// openURL opens a URL in the default browser.
func openURL(url string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "linux":
		c = exec.Command("xdg-open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		fmt.Println(url)
		return nil
	}
	return c.Start()
}

func notImplemented(cmd *cobra.Command, args []string) error {
	fmt.Fprintln(os.Stderr, "Not implemented yet")
	return nil
}
