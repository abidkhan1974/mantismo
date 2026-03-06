# 14 — Spec: Web Dashboard (Tauri-Ready)

## Objective

Build a local web dashboard that provides real-time visibility into agent activity. **Designed from day one to embed in a Tauri desktop app in Phase 2** — the React SPA communicates entirely via relative API paths, uses no server-side rendering, and is mobile-responsive.

## Prerequisites

- Spec 07 (API Server) — all data comes from the API
- Spec 06 (Logging), Spec 08 (Fingerprinting), Spec 12 (Vault)

## Architecture

```
Phase 1: Browser at localhost:7777
┌─────────────────────────────────┐
│  Browser                        │
│  └── React SPA                  │
│       ├── REST calls to /api/*  │
│       └── WS to /api/ws/*      │
└─────────────────────────────────┘

Phase 2: Same SPA inside Tauri webview
┌─────────────────────────────────┐
│  Tauri App                      │
│  ├── System tray + native menu  │
│  └── Webview                    │
│       └── Same React SPA        │
│            ├── REST to /api/*   │ (Go backend runs as Tauri sidecar)
│            └── WS to /api/ws/*  │
└─────────────────────────────────┘
```

The SPA is identical in both phases. No code changes needed.

## Tauri-Ready Design Rules

These rules ensure the Phase 2 transition is a packaging task, not a rewrite:

1. **No hard-coded URLs.** All API calls use relative paths (`/api/logs`, not `http://localhost:7777/api/logs`). The SPA works regardless of host/port.
2. **No server-side rendering.** Pure client-side React with client-side routing.
3. **No cookies or server sessions.** State lives in React state or URL params.
4. **Responsive layout.** Works at desktop (1200px+), tablet (768px), and mobile (375px) widths. Phase 3 mobile app may reuse components.
5. **No browser-specific APIs** that Tauri's webview doesn't support (no `localStorage` — use React state; no `window.open` — use in-app navigation).
6. **Dark/light mode support.** Uses CSS variables and `prefers-color-scheme`. Tauri apps look better with proper dark mode.

## React SPA Pages

### Dashboard Home (`/`)

Summary view — the first thing users see.

- **Status bar:** Active session indicator (green dot + server name), or "No active session"
- **Stats cards:** Sessions today, Tool calls, Blocked, Approved, Secrets detected
- **Live activity feed:** Real-time tool call stream via WebSocket, color-coded:
  - Green: allowed
  - Red: blocked
  - Yellow: approved (pending or granted)
  - Blue: vault tool
- **Approval queue:** If any approvals are pending, show them prominently at the top with action buttons (Allow/Deny + grant scope selector)
- **Chart:** Tool calls over time (last 24h, hourly buckets) using recharts

### Sessions (`/sessions`)

- Table of recent sessions with start/end time, server command, call counts
- Click to expand: shows all tool calls in that session as a timeline
- Filter by date range

### Tools (`/tools`)

- Card grid of all known tools
- Each card: tool name, server, call count, last used, status badge
  - OK (green) — fingerprint unchanged
  - Changed (orange warning) — description changed since last session
  - Native (blue) — Mantismo vault tool
- Click card: shows tool details, call history, description diff (if changed)
- Acknowledge button for changed tools

### Logs (`/logs`)

- Searchable, filterable log table
- Filters: time range, tool name, method, policy decision (as dropdown chips)
- Real-time toggle (switches between historical query and WebSocket tail)
- Click row to expand: shows full log entry details

### Settings (`/settings`)

- Current policy preset (with description of what each does)
- Policy preset selector (paranoid / balanced / permissive) — calls API to switch
- Vault status (enabled/disabled, entry count by category)
- Log retention setting
- API server port display
- Phase 2 will add: notification preferences, auto-start on login, setup wizard

## Approval Popup Component

**This is the most important UI component** — it's what everyday users interact with most.

When the WebSocket delivers an approval request, render a modal:

```
┌─────────────────────────────────────────┐
│  ⚡ Approval Required                   │
│                                         │
│  create_issue wants to run              │
│  Server: github                         │
│                                         │
│  Arguments:                             │
│  title: "Fix login bug"                 │
│  repo: "myapp"                          │
│                                         │
│  ┌─────────────────────────────────┐    │
│  │ Allow for:                      │    │
│  │ ○ This call only               │    │
│  │ ○ 5 minutes                    │    │
│  │ ● 30 minutes                   │    │
│  │ ○ This session                 │    │
│  │ ○ Always                       │    │
│  └─────────────────────────────────┘    │
│                                         │
│  [ Deny ]              [ Allow ✓ ]     │
│                                         │
│  Auto-deny in 47s                       │
└─────────────────────────────────────────┘
```

Features:
- Countdown timer (auto-deny on expiry)
- Grant scope selector defaults to "30 minutes" (most common choice)
- Multiple pending approvals stack as a queue (badge count on icon)
- Sound/visual notification when new approval arrives (prepares for Tauri native notifications in Phase 2)

## Implementation Notes

### React SPA (`internal/dashboard/ui/`)

- Framework: React 18 with hooks
- Styling: Tailwind CSS via CDN (utility classes only, no build step needed)
- Bundler: esbuild (`esbuild src/index.jsx --bundle --outdir=dist --minify`)
- Charts: recharts
- Routing: react-router (client-side, hash-based for Tauri compatibility)
- WebSocket: native browser API
- State: React context + hooks (no Redux — app is simple enough)

### Build Process

```makefile
dashboard-ui:
	cd internal/dashboard/ui && npm install && npm run build

# Built files are committed to repo so go:embed works without Node in CI
```

### Embedding in Go

```go
//go:embed ui/dist/*
var staticFS embed.FS

// In api/server.go routes:
// Serve SPA at / with fallback to index.html for client-side routing
```

### API Communication

```javascript
// api.js — shared API client used by all components
const API_BASE = ''; // Relative path — works in browser AND Tauri

export async function fetchLogs(params) {
  const query = new URLSearchParams(params);
  const res = await fetch(`${API_BASE}/api/logs?${query}`);
  return res.json();
}

export function connectLogStream(onEntry) {
  const wsUrl = `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/api/ws/logs`;
  const ws = new WebSocket(wsUrl);
  ws.onmessage = (e) => onEntry(JSON.parse(e.data));
  return ws;
}

export function connectApprovalStream(onRequest, onSendResponse) {
  const wsUrl = `${location.protocol === 'https:' ? 'wss:' : 'ws:'}//${location.host}/api/ws/approvals`;
  const ws = new WebSocket(wsUrl);
  ws.onmessage = (e) => {
    const msg = JSON.parse(e.data);
    if (msg.type === 'approval_request') onRequest(msg.data);
  };
  onSendResponse((response) => {
    ws.send(JSON.stringify({ type: 'approval_response', data: response }));
  });
  return ws;
}
```

Note: all URLs are relative. No `http://localhost:7777` anywhere. This is what makes Tauri embedding work without changes.

## Test Plan

1. **TestStaticFileServing** — Verify `/` serves the React SPA `index.html`
2. **TestSPAFallbackRouting** — Verify `/sessions`, `/tools` all serve `index.html` (client-side routing)
3. **TestApprovalWebSocketFlow** — Connect WS, receive approval request, send response, verify gateway resolves
4. **TestLogStreamWebSocket** — Connect to log WS, write a log entry, verify it arrives
5. **TestMultipleDashboardClients** — Two WS clients connected, both receive approval requests
6. **TestLocalhostBinding** — Verify server refuses to bind to non-localhost addresses
7. **TestDashboardWithoutActiveSession** — Dashboard shows historical data when no proxy is running
8. **TestResponsiveLayout** — Render at 375px, 768px, 1200px widths (Playwright/Puppeteer)
9. **TestNoHardcodedURLs** — Grep SPA source for `localhost` or `127.0.0.1` — must find zero

## Acceptance Criteria

- [ ] Dashboard serves at `localhost:7777` with real-time data
- [ ] Approval requests appear as popup modals with grant scope selector
- [ ] Live activity feed streams via WebSocket
- [ ] All API calls use relative paths (no hard-coded URLs)
- [ ] Layout is responsive (desktop, tablet, mobile widths)
- [ ] Dark/light mode respects system preference
- [ ] Dashboard works with no active proxy session (shows historical data)
- [ ] SPA can be embedded in a Tauri webview with zero code changes
- [ ] All 9 tests pass
