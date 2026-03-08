# 02 — System Architecture

## High-Level Architecture

```
┌─────────────────┐     stdio      ┌─────────────────────────────────────────────┐     stdio      ┌─────────────────┐
│   Agent Host    │ ◄────────────► │              Mantismo                       │ ◄────────────► │   MCP Server    │
│ (Claude Desktop │    JSON-RPC    │                                             │    JSON-RPC    │ (e.g. GitHub,   │
│  Cursor, etc.)  │                │  ┌───────────┐  ┌───────────────┐          │                │  filesystem)    │
│                 │                │  │ Interceptor│─►│ Policy Engine │          │                │                 │
│                 │                │  └───────────┘  └───────┬───────┘          │                └─────────────────┘
│                 │                │       │                 │                  │
│                 │                │       ▼                 ▼                  │
│                 │                │  ┌─────────┐    ┌────────────┐            │
│                 │                │  │  Logger  │    │  Approval  │            │
│                 │                │  └─────────┘    │  Gateway   │            │
│                 │                │       │         └──────┬─────┘            │
│                 │                │       ▼                │                  │
│                 │                │  ┌──────────────────────────────────┐     │
│                 │                │  │       Internal API Server        │     │
│                 │                │  │     (localhost:7777/api/*)       │     │
│                 │                │  │                                  │     │
│                 │                │  │  Consumers:                     │     │
│                 │                │  │  ├── Phase 1: CLI + Web UI      │     │
│                 │                │  │  ├── Phase 2: Tauri Desktop App │     │
│                 │                │  │  └── Phase 3: Mobile App        │     │
│                 │                │  └──────────────────────────────────┘     │
│                 │                │                    │                       │
│                 │                │  ┌──────────────────────────────────┐     │
│                 │                │  │    ~/.mantismo/                  │     │
│                 │                │  │  ├── logs/                      │     │
│                 │                │  │  ├── vault.db                   │     │
│                 │                │  │  ├── fingerprints.json          │     │
│                 │                │  │  └── policies/                  │     │
│                 │                │  └──────────────────────────────────┘     │
│                 │                │                                             │
│                 │                │  ┌──────────────────────────┐             │
│                 │                │  │  Vault MCP Tools         │             │
│                 │                │  └──────────────────────────┘             │
└─────────────────┘                └─────────────────────────────────────────────┘
```

## Key Architectural Principle: API-First

**Every feature Mantismo offers is exposed through an internal REST + WebSocket API.** The CLI is a thin client that calls this API. The web dashboard is a thin client that calls this API. The future Tauri desktop app and mobile app will be thin clients that call this API.

This means:
- The Go backend is the single source of truth
- No business logic lives in the CLI, dashboard, or any future frontend
- Switching from CLI to GUI requires zero backend changes
- Multiple clients can run simultaneously (CLI and dashboard at the same time)

## Data Flow: Normal Tool Call

```
1. Agent Host sends JSON-RPC request (e.g. tools/call) via stdout
       │
       ▼
2. Mantismo PROXY reads from stdin, parses JSON-RPC envelope
       │
       ▼
3. INTERCEPTOR identifies MCP method, extracts tool name, arguments
       │
       ▼
4. POLICY ENGINE evaluates request against OPA policies
       │
       ├── ALLOW ──► Forward request to MCP server, log decision
       │
       ├── DENY ──► Return error response to agent host, log decision
       │
       └── APPROVE ──► APPROVAL GATEWAY prompts user
                           │
                           ├── User approves ──► Forward + log
                           └── User denies ──► Return error + log
       │
       ▼
5. MCP Server processes request, returns JSON-RPC response
       │
       ▼
6. Mantismo INTERCEPTOR inspects response
       │
       ├── SECRET SCANNER checks for leaked credentials in response
       │
       └── LOGGER writes complete request+response+decision to JSONL
       │
       ▼
7. Response forwarded to Agent Host
```

## Component Responsibilities

### Internal API Server (`internal/api/`)
**This is the central nervous system.** Every other component exposes its functionality through this server.

- Runs on `localhost:7777` (configurable)
- REST endpoints for queries (logs, tools, stats, vault metadata, policy info, sessions)
- WebSocket endpoints for real-time data (log streaming, approval requests)
- Serves the React SPA static files at `/`
- No authentication in Phase 1 (localhost only)
- Phase 2: adds a local auth token for Tauri IPC
- All API endpoints documented with OpenAPI for future clients

### Proxy Engine (`internal/proxy/`)
- Manages stdio subprocess lifecycle (spawn, pipe, signal, cleanup)
- Reads/writes JSON-RPC 2.0 messages bidirectionally
- Handles message framing (newline-delimited JSON on stdio)
- Passes messages to Interceptor; forwards results
- Manages graceful shutdown on SIGINT/SIGTERM
- Reports proxy status to API server (active sessions, uptime, connected server)

### Interceptor (`internal/interceptor/`)
- Parses JSON-RPC messages into typed MCP structures
- Routes messages to appropriate handlers (policy, logging, vault tools)
- Handles the tools/list augmentation (injecting vault tools into the tool list)
- Detects and routes sampling/createMessage requests

### Policy Engine (`internal/policy/`)
- Embeds OPA as a Go library (no sidecar)
- Loads .rego policy files from `~/.mantismo/policies/`
- Evaluates each tool call against loaded policies
- Returns one of: ALLOW, DENY, APPROVE (needs human approval)
- Ships three preset policy bundles: paranoid, balanced, permissive
- Exposes policy info via API (current preset, rule list, recent decisions)

### Logger (`internal/logger/`)
- Writes structured JSONL logs to `~/.mantismo/logs/`
- One file per day: `2026-03-05.jsonl`
- Rotation: configurable max age (default 30 days)
- Exposes log query + real-time stream via API

### Tool Fingerprinter (`internal/fingerprint/`)
- Hashes tool descriptions on first use, alerts on changes
- Reports changed tools via API (for dashboard/app alerts)

### Secret Scanner (`internal/scanner/`)
- Regex-based pattern matching for common secret formats
- Scans tool call arguments (outbound) and responses (inbound)

### Approval Gateway (`internal/approval/`)
- **Multi-backend design (critical for Phase 2/3 transition):**
  - `backend_terminal.go` — Phase 1: stderr prompt, /dev/tty input (CLI users)
  - `backend_websocket.go` — Phase 1: pushes approval request via WebSocket (dashboard users)
  - Phase 2: Tauri native notification backend
  - Phase 3: Push notification backend (mobile relay)
- All backends implement the same `ApprovalBackend` interface
- Gateway tries backends in priority order: WebSocket → Terminal → timeout
- This means: if the dashboard is open, approvals appear there. If not, they fall back to terminal.

### Vault Storage (`internal/vault/`)
- SQLite + SQLCipher encrypted local storage
- Exposes vault stats (not data) via API

### Vault MCP Tools (`internal/vaulttools/`)
- MCP tool handlers that read from the vault
- All tools are read-only

### CLI (`cmd/mantismo/`)
- **Thin client that calls the Internal API for most operations**
- `mantismo wrap` spawns proxy + API server, then the CLI becomes a consumer
- `mantismo logs` calls `GET /api/logs`
- `mantismo tools` calls `GET /api/tools`
- Uses `cobra` for CLI framework

### Dashboard / Web UI (`internal/dashboard/ui/`)
- React SPA served by the API server (embedded via Go embed)
- **Designed for Tauri embedding from day one:**
  - No server-side rendering; pure SPA
  - All data via REST + WebSocket calls to `/api/*`
  - Responsive layout (works in Tauri webview and standalone browser)
  - No hard-coded localhost URLs (uses relative paths only)
  - Mobile-responsive CSS (preparation for Phase 3)

## Directory Structure

```
mantismo/
├── cmd/
│   └── mantismo/
│       └── main.go                 # CLI entry point
├── internal/
│   ├── api/
│   │   ├── server.go               # HTTP + WebSocket API server
│   │   ├── server_test.go
│   │   ├── routes.go               # Route definitions
│   │   ├── handlers_logs.go
│   │   ├── handlers_tools.go
│   │   ├── handlers_sessions.go
│   │   ├── handlers_policy.go
│   │   ├── handlers_vault.go
│   │   ├── handlers_approval.go    # WebSocket approval handler
│   │   └── openapi.yaml            # API contract
│   ├── proxy/
│   │   ├── proxy.go
│   │   ├── proxy_test.go
│   │   └── framing.go
│   ├── interceptor/
│   │   ├── interceptor.go
│   │   ├── interceptor_test.go
│   │   └── types.go
│   ├── policy/
│   │   ├── engine.go
│   │   ├── engine_test.go
│   │   └── presets/
│   │       ├── paranoid.rego
│   │       ├── balanced.rego
│   │       └── permissive.rego
│   ├── logger/
│   │   ├── logger.go
│   │   └── logger_test.go
│   ├── fingerprint/
│   │   ├── fingerprint.go
│   │   └── fingerprint_test.go
│   ├── scanner/
│   │   ├── scanner.go
│   │   ├── scanner_test.go
│   │   └── patterns.go
│   ├── approval/
│   │   ├── gateway.go              # Multi-backend gateway
│   │   ├── gateway_test.go
│   │   ├── backend.go              # Backend interface
│   │   ├── backend_terminal.go     # Terminal backend
│   │   └── backend_websocket.go    # WebSocket backend
│   ├── vault/
│   │   ├── vault.go
│   │   ├── vault_test.go
│   │   └── schema.sql
│   ├── vaulttools/
│   │   ├── tools.go
│   │   └── tools_test.go
│   ├── dashboard/
│   │   └── ui/
│   │       ├── src/
│   │       └── dist/
│   └── config/
│       ├── config.go
│       └── config_test.go
├── policies/
├── testdata/
├── Makefile
├── go.mod
├── .goreleaser.yml
├── .github/workflows/
└── README.md
```

## Key Design Decisions

### Why API-first (not CLI-first)?
The CLI is the first *client*, but the API is the first *interface*. By routing everything through a local REST + WebSocket API, we guarantee that the Tauri desktop app (Phase 2) and mobile app (Phase 3) can consume the same backend with zero refactoring. The API server starts automatically when `mantismo wrap` runs. This adds ~1 week of work to Phase 1 but saves months in Phase 2.

### Why Go?
Single static binary, excellent subprocess handling, OPA Go library, trivial cross-compilation, built-in HTTP server for the API layer, and Tauri can invoke Go binaries as sidecar processes.

### Why OPA for policies?
Industry standard for policy-as-code. For Phase 2 (everyday users), the dashboard provides a visual policy editor that generates Rego under the hood — users never see the policy language directly.

### Why SQLite+SQLCipher for the vault?
Single-user, local-first, encrypted, single-file database. No server process.

### Why JSONL for logs?
Append-only, no contention, trivially greppable, streamable via WebSocket.
