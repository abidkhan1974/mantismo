# 11 — Spec: Approval Gateway (Multi-Backend)

## Objective

Build a human-in-the-loop approval system that works across multiple UIs: terminal (CLI users), WebSocket (dashboard users), and eventually native notifications (Tauri desktop app) and push notifications (mobile). The gateway abstracts the approval backend so adding new UIs requires zero changes to the core logic.

## Prerequisites

- Spec 07 (API Server) — WebSocket endpoint for approvals
- Spec 09 (Policy Engine) — provides "approve" decisions

## Architecture

```
Policy Engine returns "approve"
       │
       ▼
Approval Gateway
       │
       ├── Check permanent allows → approved immediately
       ├── Check session grants → approved immediately
       │
       └── No cached grant → try backends in priority order:
              │
              ├── 1. WebSocket backend (if dashboard/Tauri client connected)
              │      Push approval request to connected client
              │      Wait for response via WebSocket
              │
              ├── 2. Terminal backend (if running in terminal with /dev/tty)
              │      Print prompt to stderr
              │      Read response from /dev/tty
              │
              └── 3. Timeout (no backends available or all timed out)
                     Auto-deny after configurable period
```

The key insight: **the gateway doesn't care which UI the user responds from.** If the dashboard is open, approvals appear there as a popup. If the user is in a terminal, they get a text prompt. If both are open, the WebSocket backend takes priority (better UX). Phase 2 adds Tauri native notifications as another backend. Phase 3 adds mobile push. Same gateway, same interface.

## Interface Contract

### Package: `internal/approval`

```go
// Backend defines the interface all approval UIs must implement.
type Backend interface {
    // Name returns the backend identifier (for logging).
    Name() string

    // Available returns true if this backend can currently accept prompts.
    // e.g., WebSocket backend returns true only if a client is connected.
    Available() bool

    // Prompt displays the approval request and waits for a response.
    // Returns the user's decision or an error (including timeout).
    Prompt(ctx context.Context, req ApprovalPrompt) (ApprovalResponse, error)

    // Priority returns the backend's priority (lower = tried first).
    Priority() int
}

// ApprovalPrompt contains the information shown to the user.
type ApprovalPrompt struct {
    ID          string          `json:"id"`          // Unique prompt ID
    ToolName    string          `json:"tool_name"`
    ServerCmd   string          `json:"server_cmd"`
    Reason      string          `json:"reason"`      // From policy engine
    Arguments   string          `json:"arguments"`   // Summarized, truncated, redacted
    RiskLevel   string          `json:"risk_level"`  // "write", "changed", "sensitive"
    CreatedAt   time.Time       `json:"created_at"`
    ExpiresAt   time.Time       `json:"expires_at"`  // When auto-deny kicks in
}

// ApprovalResponse is the user's decision.
type ApprovalResponse struct {
    Decision    GrantDecision   `json:"decision"`
    GrantScope  GrantScope      `json:"grant_scope"`
}

type GrantDecision string
const (
    Approved GrantDecision = "approved"
    Denied   GrantDecision = "denied"
    Timeout  GrantDecision = "timeout"
)

type GrantScope string
const (
    ThisCallOnly GrantScope = "this_call"
    For5Minutes  GrantScope = "5_minutes"
    For30Minutes GrantScope = "30_minutes"
    ForSession   GrantScope = "session"
    Permanently  GrantScope = "permanent"
)

// Grant represents a cached approval.
type Grant struct {
    Decision    GrantDecision
    Scope       GrantScope
    ToolName    string
    ExpiresAt   *time.Time    // nil for session and permanent grants
    GrantedAt   time.Time
}

// Gateway manages approval flow across multiple backends.
type Gateway struct {
    backends    []Backend          // Sorted by priority
    timeout     time.Duration
    grants      map[string]Grant   // Active session grants
    permanents  map[string]bool    // Permanently allowed tools
    permFile    string             // Path to persistent allows file
    mu          sync.Mutex
}

// NewGateway creates an approval gateway with the given backends.
func NewGateway(timeout time.Duration, permFile string, backends ...Backend) *Gateway

// RequestApproval checks caches, then prompts via available backends.
func (g *Gateway) RequestApproval(ctx context.Context, prompt ApprovalPrompt) (Grant, error)

// PendingApprovals returns currently pending approval requests (for API).
func (g *Gateway) PendingApprovals() []ApprovalPrompt
```

### Terminal Backend (`backend_terminal.go`)

```go
type TerminalBackend struct{}

func NewTerminalBackend() *TerminalBackend
func (t *TerminalBackend) Name() string          // "terminal"
func (t *TerminalBackend) Available() bool        // true if /dev/tty exists
func (t *TerminalBackend) Prompt(...) (...)       // stderr prompt + /dev/tty input
func (t *TerminalBackend) Priority() int          // 100 (low priority)
```

Terminal prompt format:
```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚡ APPROVAL REQUIRED
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
Tool:    create_issue
Server:  npx @modelcontextprotocol/server-github
Reason:  write operation requires approval
Args:    {title: "Fix login bug", repo: "myapp"}

  [1] Allow (this call only)
  [2] Allow (5 minutes)
  [3] Allow (30 minutes)
  [4] Allow (this session)
  [5] Allow (always for this tool)
  [d] Deny

Choice (auto-deny in 55s): _
```

All I/O goes to stderr (prompt) and /dev/tty (input). Never touches stdin/stdout.

### WebSocket Backend (`backend_websocket.go`)

```go
type WebSocketBackend struct {
    clients   map[string]*websocket.Conn  // Connected dashboard/Tauri clients
    pending   map[string]chan ApprovalResponse // Waiting for response by prompt ID
    mu        sync.Mutex
}

func NewWebSocketBackend() *WebSocketBackend
func (w *WebSocketBackend) Name() string          // "websocket"
func (w *WebSocketBackend) Available() bool        // true if any client connected
func (w *WebSocketBackend) Prompt(...) (...)       // push to WS, wait for response
func (w *WebSocketBackend) Priority() int          // 10 (high priority)

// HandleConnection is called by the API server when a client connects to /api/ws/approvals
func (w *WebSocketBackend) HandleConnection(conn *websocket.Conn)
```

WebSocket message format (server → client):
```json
{
    "type": "approval_request",
    "data": {
        "id": "abc123",
        "tool_name": "create_issue",
        "server_cmd": "npx @modelcontextprotocol/server-github",
        "reason": "write operation requires approval",
        "arguments": "{title: \"Fix login bug\"}",
        "risk_level": "write",
        "expires_at": "2026-03-05T14:33:00Z"
    }
}
```

WebSocket message format (client → server):
```json
{
    "type": "approval_response",
    "data": {
        "id": "abc123",
        "decision": "approved",
        "grant_scope": "5_minutes"
    }
}
```

This is exactly what the React dashboard (and later Tauri app) will render as a popup dialog.

## Detailed Requirements

### 11.1 Backend Priority and Fallback

When an approval is needed:
1. Sort backends by priority (lowest number first)
2. For each backend, check `Available()`
3. Call `Prompt()` on the first available backend
4. If that backend returns an error (not timeout), try the next backend
5. If no backends are available, wait up to timeout then auto-deny

This means:
- If dashboard is open → approval appears as a WebSocket popup (priority 10)
- If dashboard is closed but terminal available → falls back to stderr prompt (priority 100)
- If neither available (e.g., running as daemon) → auto-deny after timeout

### 11.2 Grant Caching

Session grants are time-bounded approvals cached in memory:
- `this_call` → no cache
- `5_minutes` → cache with 5 minute expiry
- `30_minutes` → cache with 30 minute expiry
- `session` → cache for proxy lifetime (no expiry)
- `permanent` → write to `~/.mantismo/allows.json` and cache

Cached grants are checked before trying any backend.

### 11.3 Permanent Allows

- Stored in `~/.mantismo/allows.json`
- Format: `{"create_issue": true, "push_commits": true}`
- Loaded on startup
- Updated immediately when user selects "permanent"

### 11.4 Concurrent Approval Handling

Multiple tool calls may trigger approval simultaneously. The gateway:
- Assigns each approval a unique ID
- Sends all pending approvals to available backends concurrently
- WebSocket backend can show multiple pending approvals at once (the dashboard renders a queue)
- Terminal backend serializes prompts (one at a time)

### 11.5 API Integration

The API server exposes:
- `GET /api/approvals/pending` — returns currently pending approvals
- `POST /api/approvals/{id}/respond` — respond to a specific approval (alternative to WebSocket for simple HTTP clients)
- `WS /api/ws/approvals` — real-time bidirectional approval channel

This means the future Tauri app or mobile app can respond to approvals via simple HTTP POST, not just WebSocket. Maximum flexibility.

### 11.6 Proxy Integration

While waiting for approval, the proxy must not block other traffic:
```go
// In the Interceptor's OnToolCall hook:
if policyResult.Decision == policy.Approve {
    resultCh := make(chan Grant, 1)
    go func() {
        grant, _ := gateway.RequestApproval(ctx, prompt)
        resultCh <- grant
    }()
    return InterceptResult{Action: Pending, PendingCh: resultCh}
}
```

The proxy handles `Pending` actions by buffering the request and waiting on the channel. Non-pending traffic continues flowing.

## Test Plan

1. **TestTerminalApproval** — Mock /dev/tty, verify prompt appears and response works
2. **TestWebSocketApproval** — Connect WS client, send approval request, respond, verify grant
3. **TestBackendPriority** — Both backends available, verify WebSocket is tried first
4. **TestBackendFallback** — WebSocket unavailable, verify terminal is tried
5. **TestNoBackendsTimeout** — No backends available, verify auto-deny after timeout
6. **TestSessionGrant** — Approve with "5 minutes" scope, verify second call skips prompt
7. **TestSessionGrantExpiry** — Set 1s grant, wait 2s, verify prompt shown again
8. **TestPermanentAllow** — Approve permanently, verify persisted and loaded on restart
9. **TestConcurrentApprovals** — Submit 3 approvals, verify all resolve independently
10. **TestHTTPApprovalEndpoint** — Respond via POST /api/approvals/{id}/respond
11. **TestProxyFlowsDuringApproval** — Non-approval traffic flows while waiting

## Acceptance Criteria

- [ ] Approval gateway supports pluggable backends via interface
- [ ] Terminal backend works via stderr + /dev/tty
- [ ] WebSocket backend pushes approvals to connected dashboard clients
- [ ] HTTP POST endpoint provides an alternative response mechanism
- [ ] Backend priority ensures best available UI is used
- [ ] Fallback works when preferred backend is unavailable
- [ ] Grant caching (session + permanent) reduces prompt frequency
- [ ] Concurrent approvals handled correctly
- [ ] Adding a new backend (Tauri, mobile) requires only implementing the Backend interface
- [ ] All 11 tests pass
