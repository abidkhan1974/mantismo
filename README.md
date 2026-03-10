# Mantismo

> **A personal firewall for your AI agents.** See everything. Approve what matters. Block what doesn't belong.

![Status](https://img.shields.io/badge/status-alpha-orange)
![License](https://img.shields.io/badge/license-AGPL--3.0%20%2F%20Commercial-blue)
![Go](https://img.shields.io/badge/go-1.22+-00ADD8)

Mantismo is a transparent security proxy for MCP servers. It sits between your AI agent host (Claude Desktop, Cursor, VS Code) and your MCP servers (filesystem, GitHub, databases), intercepting and inspecting every tool call — without changing how anything works.

**Core features:**
- **Visibility** — Every tool call logged to a structured audit trail
- **Control** — Allow, deny, or require human approval via policy presets
- **Protection** — Detects credential leaks and tool definition tampering
- **Local-first** — No cloud account, no telemetry, all data stays on your machine

---

## Install

### Homebrew (macOS + Linux)

```bash
brew tap abidkhan1974/homebrew-tap
brew install mantismo
```

### Direct download (macOS + Linux)

```bash
curl -sSL https://raw.githubusercontent.com/abidkhan1974/mantismo/main/install.sh | sh
```

Auto-detects your OS and architecture, installs to `/usr/local/bin/mantismo`.

### Build from source

**Prerequisites:** Go 1.22+, make

```bash
git clone https://github.com/abidkhan1974/mantismo
cd mantismo
make build
./bin/mantismo --version
```

---

## Integrate in 30 Seconds

Mantismo wraps any stdio MCP server with a single command change. No other configuration needed.

### Claude Desktop

Edit `~/Library/Application Support/Claude/claude_desktop_config.json`:

**Before:**
```json
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/home/user"]
    }
  }
}
```

**After:**
```json
{
  "mcpServers": {
    "filesystem": {
      "command": "mantismo",
      "args": ["wrap", "--preset", "balanced", "--",
               "npx", "-y", "@modelcontextprotocol/server-filesystem", "/home/user"]
    }
  }
}
```

Restart Claude Desktop. That's it — every tool call is now being intercepted and logged.

### Cursor

Edit `~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "github": {
      "command": "mantismo",
      "args": ["wrap", "--preset", "balanced", "--",
               "npx", "-y", "@modelcontextprotocol/server-github"],
      "env": { "GITHUB_PERSONAL_ACCESS_TOKEN": "your-token" }
    }
  }
}
```

### VS Code (any MCP extension)

```json
{
  "mcp.servers": {
    "filesystem": {
      "command": "mantismo",
      "args": ["wrap", "--preset", "balanced", "--",
               "npx", "-y", "@modelcontextprotocol/server-filesystem", "/home/user"]
    }
  }
}
```

---

## Policy Presets

Choose how aggressively Mantismo intervenes:

| Preset | Behaviour |
|--------|-----------|
| `permissive` | Log everything, block nothing — pure visibility |
| `balanced` | Log everything, require approval for writes and deletions |
| `paranoid` | Require approval for every tool call |

Pass `--preset` to `mantismo wrap`, or set it globally in `~/.mantismo/config.toml`.

---

## What You'll See

Once your agent makes tool calls, check the audit log:

```bash
mantismo logs --since today
```

Each entry is a structured JSON line:

```json
{"time":"2026-03-09T14:23:01Z","event":"tool_call","server":"filesystem","tool":"read_file","args":{"path":"/home/user/notes.txt"},"decision":"allow","preset":"balanced","duration_ms":12}
{"time":"2026-03-09T14:23:04Z","event":"tool_call","server":"filesystem","tool":"write_file","args":{"path":"/home/user/notes.txt"},"decision":"pending_approval","preset":"balanced","duration_ms":0}
```

Open the dashboard for a live view:

```bash
mantismo dashboard --open
# → http://localhost:7777
```

Check tool fingerprints (detects MCP server tampering):

```bash
mantismo tools
```

Run a health check:

```bash
mantismo doctor
```

---

## Configuration

Auto-generated at `~/.mantismo/config.toml` on first run:

```toml
log_level = "info"

[api]
port = 7777
bind_addr = "127.0.0.1"

[policy]
preset = "balanced"  # paranoid | balanced | permissive

[vault]
enabled = false
```

---

## Architecture

```
Agent Host ──stdio──► Mantismo Proxy ──stdio──► MCP Server
(Claude Desktop,            │
 Cursor, VS Code)    Internal API (localhost:7777)
                      ├── CLI
                      └── Web Dashboard
```

Mantismo uses an **API-first architecture**: a Go backend exposes a REST + WebSocket API at `localhost:7777`. The CLI and web dashboard are both thin clients of this API.

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for more detail.

---

## License

Dual licensed under AGPL-3.0 and a commercial license. See [LICENSE](LICENSE) for details.

"Mantismo" is a trademark of Abid Ali Khan. See [LICENSE](LICENSE) for permitted uses.
