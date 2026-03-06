# 07 — Spec: CLI Interface + Internal API Server

## Objective

Implement the internal API server (the backend for all UIs) and the CLI as its first thin client. The API server is the primary interface; the CLI calls it. This ensures the Tauri desktop app (Phase 2) can consume the same API with zero backend changes.

## Prerequisites

- Spec 04 (Proxy), 05 (Interceptor), 06 (Logging) complete

## Architecture

```
mantismo wrap -- npx server-github
       │
       ├── Starts Proxy (stdio forwarding)
       ├── Starts API Server (localhost:7777)
       │     ├── REST: /api/logs, /api/tools, /api/sessions, /api/stats, /api/policy
       │     ├── WebSocket: /api/ws/logs (live stream), /api/ws/approvals (approval UI)
       │     └── Static: / (serves React dashboard SPA)
       │
       └── CLI commands call the API:
             mantismo logs   →  GET /api/logs?since=today
             mantismo tools  →  GET /api/tools
             mantismo status →  GET /api/stats
```

## Internal API Endpoints

### REST Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/sessions` | List recent proxy sessions |
| GET | `/api/logs` | Query audit logs (supports `since`, `until`, `tool`, `method`, `decision`, `limit` query params) |
| GET | `/api/tools` | List all tools with fingerprint status |
| GET | `/api/stats` | Dashboard summary statistics |
| GET | `/api/policy` | Current policy info (preset, rules, recent decisions) |
| GET | `/api/vault/stats` | Vault metadata (entry counts by category — no actual data) |
| GET | `/api/health` | Health check (is proxy running, API server up) |
| POST | `/api/policy/check` | Dry-run: replay recent logs against current policies |
| POST | `/api/approval/{id}/respond` | Respond to a pending approval (allow/deny + grant scope) |

### WebSocket Endpoints

| Path | Description |
|------|-------------|
| `/api/ws/logs` | Real-time log stream (new entries pushed as they happen) |
| `/api/ws/approvals` | Approval requests pushed to connected clients; responses sent back |

### Static Files

| Path | Description |
|------|-------------|
| `/` | React SPA (dashboard) served from embedded static files |
| `/assets/*` | Static assets (JS, CSS) for the SPA |

## Interface Contract

### Package: `internal/api`

```go
// Server is the internal API server.
type Server struct {
    port       int
    bindAddr   string
    logger     *logger.Logger
    fingerprints *fingerprint.Store
    policy     *policy.Engine
    vault      *vault.Vault       // May be nil if vault disabled
    approvalCh chan ApprovalRequest // Receives approval requests from gateway
    sessions   *SessionStore       // Tracks active and past proxy sessions
}

// NewServer creates a new API server.
func NewServer(cfg Config, deps Dependencies) *Server

// Start begins serving HTTP and WebSocket connections. Non-blocking.
func (s *Server) Start(ctx context.Context) error

// Stop gracefully shuts down the server.
func (s *Server) Stop(ctx context.Context) error

// Dependencies groups all backend components the API exposes.
type Dependencies struct {
    Logger       *logger.Logger
    Fingerprints *fingerprint.Store
    Policy       *policy.Engine
    Vault        *vault.Vault
    ApprovalCh   chan ApprovalRequest
    Sessions     *SessionStore
}

// SessionStore tracks proxy sessions.
type SessionStore struct {
    mu       sync.RWMutex
    active   *SessionInfo
    history  []SessionInfo
}

type SessionInfo struct {
    ID          string    `json:"id"`
    StartedAt   time.Time `json:"started_at"`
    EndedAt     *time.Time `json:"ended_at,omitempty"`
    ServerCmd   string    `json:"server_command"`
    ToolCalls   int       `json:"tool_calls"`
    Blocked     int       `json:"blocked"`
    Approved    int       `json:"approved"`
}
```

### Package: `internal/api/client`

```go
// Client is a Go client for the internal API. Used by the CLI.
type Client struct {
    baseURL string
    http    *http.Client
}

// NewClient creates a client pointing at the local API server.
func NewClient(port int) *Client

// Logs queries the audit log.
func (c *Client) Logs(filter LogFilter) ([]logger.LogEntry, error)

// Tools returns the tool list with fingerprint status.
func (c *Client) Tools() ([]ToolInfo, error)

// Stats returns dashboard statistics.
func (c *Client) Stats() (*StatsResponse, error)

// Health checks if the API server is running.
func (c *Client) Health() error

// StreamLogs connects to the WebSocket and streams log entries.
func (c *Client) StreamLogs(ctx context.Context, ch chan<- logger.LogEntry) error
```

## CLI Commands

### 7.1 `mantismo wrap`

The primary command. Starts the proxy AND the API server together.

```
Usage:
  mantismo wrap [flags] -- <command> [args...]

Flags:
  --preset string     Policy preset: paranoid, balanced, permissive (default "balanced")
  --log-level string  Log level: debug, info, warn, error (default "info")
  --no-policy         Disable policy engine (logging only)
  --no-vault          Disable vault tools injection
  --port int          API server port (default 7777)
  --config string     Path to config file

Examples:
  mantismo wrap -- npx -y @modelcontextprotocol/server-github
  mantismo wrap --preset paranoid -- python my_mcp_server.py
```

Implementation:
1. Parse everything after `--` as the command + args
2. Load config
3. Generate session ID (UUID v4)
4. Initialize all backend components (Logger, Fingerprinter, Policy, Scanner, Approval Gateway)
5. Start API Server (non-blocking)
6. Initialize Proxy with Interceptor as handler
7. Run Proxy (blocks until exit)
8. On startup, print to stderr:
   ```
   [mantismo] Proxying: npx @modelcontextprotocol/server-github
   [mantismo] Dashboard: http://localhost:7777
   [mantismo] Session: a1b2c3d4 | Policy: balanced | Vault: enabled
   ```
9. On exit, print summary to stderr

**Critical:** All Mantismo output goes to stderr. Stdout is exclusively for JSON-RPC proxy traffic.

### 7.2 `mantismo logs`

Queries the API server for audit logs.

```
Usage:
  mantismo logs [flags]

Flags:
  --since string    Show logs since (e.g., "1h", "2026-03-05", "today")
  --until string    Show logs until
  --tool string     Filter by tool name
  --method string   Filter by MCP method
  --session string  Filter by session ID
  --decision string Filter by policy decision (allow, deny, approve)
  --limit int       Max entries to show (default 50)
  --json            Output raw JSON (for piping)
  --follow          Follow mode (connects to WebSocket for live stream)
  --port int        API server port (default 7777)
```

Implementation: calls `GET /api/logs` with query params. For `--follow`, connects to `WS /api/ws/logs`.

If API server isn't running, falls back to reading JSONL files directly from `~/.mantismo/logs/` (so logs work even without an active session).

### 7.3 `mantismo tools`

```
Usage:
  mantismo tools [flags]

Flags:
  --changed         Only show tools whose descriptions have changed
  --json            Output as JSON
  --port int        API server port (default 7777)
```

Implementation: calls `GET /api/tools`. Falls back to reading `~/.mantismo/fingerprints.json` directly if API server isn't running.

### 7.4 `mantismo status`

```
Usage:
  mantismo status [flags]

Flags:
  --port int    API server port (default 7777)
```

Calls `GET /api/health` and `GET /api/stats`. Shows:
```
Mantismo v0.1.0 — Eyes on every agent

API Server:        running (localhost:7777)
Active Session:    a1b2c3d4 (npx @modelcontextprotocol/server-github)
Policy preset:     balanced
Vault:             enabled (14 fields stored)
Dashboard:         http://localhost:7777

Sessions today:    3
Tool calls today:  127
Blocked today:     4
```

If API server isn't running:
```
Mantismo v0.1.0 — Eyes on every agent

API Server:        not running
Last session:      2026-03-05 14:32 (47 tool calls)

Run 'mantismo wrap -- <command>' to start a proxy session.
```

### 7.5 `mantismo policy`

```
Usage:
  mantismo policy <subcommand>

Subcommands:
  init        Generate starter policy from preset
  check       Dry-run policy against recent logs (calls POST /api/policy/check)
  list        Show loaded policy rules
  edit        Open policy file in $EDITOR
```

### 7.6 `mantismo vault`

```
Usage:
  mantismo vault <subcommand>

Subcommands:
  init        Initialize the vault (creates encrypted DB, sets passphrase)
  import      Interactive import wizard
  list        List vault entries by category
  get         Get a specific vault entry
  set         Set a vault entry
  delete      Delete a vault entry
  export      Export vault data (decrypted, for backup)
  lock        Lock the vault
```

Vault commands talk directly to the vault (not via API) since they require the passphrase.

### 7.7 `mantismo dashboard`

```
Usage:
  mantismo dashboard [flags]

Flags:
  --port int      Port (default 7777)
  --open          Open browser automatically
```

If an API server is already running (from `mantismo wrap`), this just opens the browser to `localhost:7777`. If no server is running, starts the API server in standalone mode (read-only, showing historical data from logs/fingerprints).

## Configuration File

Default `~/.mantismo/config.toml` (auto-generated on first run):

```toml
# Mantismo Configuration
log_level = "info"

[api]
port = 7777
bind_addr = "127.0.0.1"

[proxy]
# No settings yet (stdio only)

[policy]
preset = "balanced"
# policy_dir = "~/.mantismo/policies/"

[vault]
enabled = false
# db_path = "~/.mantismo/vault.db"

[dashboard]
auto_open = false    # Open browser on wrap start
```

## Test Plan

1. **TestAPIServerStartStop** — Start API server, verify health endpoint, stop cleanly
2. **TestLogsEndpoint** — Create test logs, query via API, verify filtering
3. **TestLogsWebSocket** — Connect to WS, write log entry, verify it arrives
4. **TestToolsEndpoint** — Populate fingerprints, query via API
5. **TestStatsEndpoint** — Verify aggregation of log data
6. **TestApprovalWebSocket** — Send approval request, respond via WS, verify grant
7. **TestCLILogsCallsAPI** — CLI `logs` command calls the API (with mock server)
8. **TestCLILogsFallback** — CLI `logs` reads files directly when API not running
9. **TestWrapStartsAPIServer** — `wrap` command starts both proxy and API server
10. **TestWrapOutputToStderr** — All Mantismo output goes to stderr
11. **TestConfigAutoGeneration** — First run creates default config

## Acceptance Criteria

- [ ] API server starts on `localhost:7777` when `mantismo wrap` runs
- [ ] All REST endpoints return correct data
- [ ] WebSocket endpoints stream logs and approval requests in real-time
- [ ] CLI commands call the API when server is running
- [ ] CLI commands fall back to direct file reads when server is not running
- [ ] All Mantismo output to stderr; stdout reserved for JSON-RPC
- [ ] Default config auto-generated on first run
- [ ] All 11 tests pass
