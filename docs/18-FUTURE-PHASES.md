# 18 — Future Phases: Desktop App + Mobile Companion

## Phase 2: Tauri Desktop App (Months 5-7)

### Why Tauri (not Electron)?

- **Size:** Tauri apps are 5-10 MB vs. Electron's 150+ MB (no bundled Chromium)
- **Performance:** Uses the OS native webview, not a separate browser process
- **Go sidecar support:** Tauri can manage the Mantismo Go binary as a sidecar process — starts it on app launch, stops on quit
- **Rust backend:** Tauri's Rust core handles system tray, native notifications, file dialogs, and auto-update — all things we need
- **Security credibility:** A security product built on Tauri looks better than one built on Electron (smaller attack surface)

### Architecture

```
┌────────────────────────────────────────────────┐
│  Tauri App                                     │
│  ├── Rust core                                 │
│  │   ├── System tray (always running)          │
│  │   ├── Native notification API               │
│  │   ├── Auto-start on login                   │
│  │   ├── Sidecar management (Go binary)        │
│  │   └── Auto-update (Tauri updater)           │
│  │                                             │
│  ├── Webview                                   │
│  │   └── React SPA (same as Phase 1 dashboard) │
│  │        ├── REST → /api/*                    │
│  │        └── WS → /api/ws/*                   │
│  │                                             │
│  └── Go sidecar (mantismo binary)              │
│       ├── Proxy engine                         │
│       ├── API server (localhost:7777)           │
│       ├── Policy engine                        │
│       ├── Vault                                │
│       └── All Phase 1 functionality            │
└────────────────────────────────────────────────┘
```

### New Features in Phase 2

**Setup Wizard (first-launch experience):**
1. Welcome screen explaining what Mantismo does (plain language, not security jargon)
2. "Which AI assistants do you use?" — checkboxes for Claude Desktop, Cursor, VS Code, etc.
3. Auto-detect installed MCP configs and offer to wrap them with Mantismo
4. Security preset selector with plain-language descriptions:
   - "Strict" (paranoid) — "Approve every action before your AI does it"
   - "Balanced" — "AI can read freely, but asks before writing or changing things"
   - "Relaxed" (permissive) — "Log everything, but don't interrupt"
5. Optional: vault setup ("Store your personal info so AI agents can use it safely")
6. Done — show dashboard with first session active

**System tray:**
- Icon shows status: green (active session), gray (idle), red (blocked action)
- Click: opens dashboard
- Right-click menu: Start/stop proxy, Quick approve/deny (last pending), Settings, Quit
- Badge count for pending approvals

**Native notifications for approvals:**
- New approval backend: `backend_tauri.go`
- Uses Tauri's notification API for OS-native notifications
- Click notification → opens approval dialog in the app
- Priority: Tauri native (5) → WebSocket (10) → Terminal (100)

**Auto-configuration:**
- Detect Claude Desktop config (`~/Library/Application Support/Claude/claude_desktop_config.json` on macOS)
- Detect Cursor config (`~/.cursor/mcp.json`)
- Offer to insert Mantismo as wrapper in existing MCP server configs
- Backup original config before modifying

**Visual policy editor:**
- Instead of editing .rego files, users see a simple UI:
  - Toggle switches: "Allow read operations" / "Require approval for writes" / "Block sampling"
  - Per-tool overrides: click a tool → "Always allow" / "Always ask" / "Always block"
  - Under the hood: generates .rego policy files from the UI state
- Everyday users never see Rego. Power users can still edit .rego directly.

**Auto-update:**
- Tauri's built-in updater checks GitHub releases
- Silent background download, prompt user to restart
- Signed updates with checksum verification

### Tauri Project Structure

```
mantismo-desktop/
├── src-tauri/
│   ├── src/
│   │   ├── main.rs              # Tauri app entry
│   │   ├── tray.rs              # System tray setup
│   │   ├── notifications.rs     # Native notification bridge
│   │   ├── autoconfig.rs        # MCP config auto-detection
│   │   └── sidecar.rs           # Go binary lifecycle management
│   ├── Cargo.toml
│   ├── tauri.conf.json          # App config, sidecar declaration
│   └── icons/
├── src/                         # React SPA (copied or symlinked from Phase 1)
│   ├── App.jsx
│   ├── pages/
│   ├── components/
│   │   ├── ApprovalPopup.jsx    # Same component, now gets native notification too
│   │   ├── SetupWizard.jsx      # NEW: first-launch wizard
│   │   └── PolicyEditor.jsx     # NEW: visual policy editor
│   └── api.js                   # Same API client (relative paths still work)
├── package.json
└── README.md
```

### Build and Distribution

- `tauri build` produces platform-specific installers:
  - macOS: `.dmg` (signed and notarized via Apple Developer account)
  - Linux: `.AppImage` and `.deb`
- Go binary is cross-compiled and bundled as a Tauri sidecar
- Auto-update: Tauri updater fetches from GitHub releases

### Estimated Work (Solo)

| Task | Effort |
|------|--------|
| Tauri scaffolding + sidecar setup | 1 week |
| System tray + native notifications | 1 week |
| Setup wizard (React) | 1 week |
| Auto-config detection | 3 days |
| Visual policy editor (React) | 1 week |
| Auto-update + signing | 3 days |
| Testing + polish | 2 weeks |
| **Total** | **~7 weeks** |

---

## Phase 3: Mobile Companion App (Months 8-10)

### Why Mobile?

The #1 user complaint about approval-based security systems is "I wasn't at my computer when the approval came in." A mobile companion ensures users can approve/deny from anywhere, and monitor agent activity on the go.

### Scope (Deliberately Narrow)

The mobile app is NOT a full Mantismo client. It's a companion:
1. **Approval notifications** — Push notification when approval needed; approve/deny from notification or app
2. **Activity monitor** — Live feed of what agents are doing (read-only)
3. **Session overview** — See active sessions, recent stats
4. **Vault quick-view** — See what's in the vault (read-only, no editing)

No proxy management, no policy editing, no vault mutations. Those stay on desktop.

### Architecture

```
┌──────────────────┐          ┌──────────────────┐          ┌──────────────────┐
│  Mobile App      │◄── push ──│  Relay Server    │◄── WS ──│  Mantismo        │
│  (React Native)  │          │  (lightweight)    │          │  Desktop/CLI     │
│                  │── HTTP ─►│                  │          │  (localhost:7777) │
└──────────────────┘          └──────────────────┘          └──────────────────┘
```

The relay server is needed because the desktop Mantismo runs on localhost (not publicly accessible). The relay:
- Receives WebSocket connections from the desktop Mantismo
- Sends push notifications to the mobile app via APNs/FCM
- Forwards approval responses back to the desktop
- Encrypted end-to-end (desktop ↔ relay ↔ mobile)
- This is the first component that requires a hosted service (small, simple)

### Alternative: Local Network Only (No Relay)

For users who don't want a cloud relay:
- Mobile app discovers Mantismo on local network via mDNS/Bonjour
- Connects directly to the API server over local WiFi
- Works only when phone and computer are on the same network
- No push notifications (must have app open)

Recommendation: Ship local-network mode first (simpler, no cloud dependency). Add relay server as an optional premium feature.

### Mobile Tech Stack

- **React Native** with Expo (fastest cross-platform development for a solo dev)
- iOS and Android from one codebase
- Push: Expo Push Notifications (wraps APNs + FCM)
- Local network: mDNS discovery + direct HTTP to Mantismo API

### Estimated Work (Solo)

| Task | Effort |
|------|--------|
| React Native + Expo setup | 3 days |
| Approval notification flow (local network) | 1 week |
| Activity monitor screen | 1 week |
| Session overview + vault quick-view | 1 week |
| Local network discovery (mDNS) | 3 days |
| Testing on iOS + Android | 1 week |
| App Store submission | 3 days |
| **Total (local-network mode)** | **~5 weeks** |
| Relay server (optional, later) | +3 weeks |

---

## Technical Debt / Improvements (Ongoing)

- **Pure-Go SQLite:** If CGo cross-compilation remains painful, migrate to `modernc.org/sqlite` with application-layer encryption
- **Policy hot-reload:** Watch policy directory, auto-reload without restart
- **Log compression:** Gzip old JSONL files to save disk space
- **HTTP+SSE transport:** Proxy remote MCP servers (not just stdio)
- **Windows support:** Cross-compile Go binary; Tauri supports Windows natively
- **Plugin system:** Third-party scanners, vault backends, approval backends
- **Context isolation:** Prevent data from one MCP server's response being used in another server's request (hard problem, high impact)
- **Vector search:** Replace keyword vault search with semantic embeddings
- **Agent marketplace:** Registry of verified MCP servers with security scores
- **Enterprise features:** SSO, org-level policies, centralized audit, per-seat licensing
