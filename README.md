# Mantismo

> **A personal firewall for your AI agents.** Own your context, lease it to agents on your terms.

![Work in Progress](https://img.shields.io/badge/status-work%20in%20progress-orange)
![License](https://img.shields.io/badge/license-AGPL--3.0%20%2F%20Commercial-blue)
![Go](https://img.shields.io/badge/go-1.26+-00ADD8)

## What Is Mantismo?

Mantismo is a personal MCP security proxy. It sits between AI agent hosts (Claude Desktop, Cursor, VS Code) and MCP servers (GitHub, filesystem, database tools), intercepting, inspecting, and policy-filtering every tool call.

**Core features:**
- **Visibility** — See every AI agent tool call in structured audit logs
- **Control** — Allow, deny, or require approval via configurable OPA policies
- **Protection** — Detect credential leaks and tool poisoning (rug pulls)
- **Sovereignty** — Store personal data in a local encrypted vault

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

**Prerequisites:** Go 1.26+, make

```bash
git clone https://github.com/abidkhan1974/mantismo
cd mantismo
make build
./bin/mantismo --version
```

## Quick Start

```bash
# Wrap any stdio MCP server
mantismo wrap -- npx -y @modelcontextprotocol/server-github

# View audit logs
mantismo logs --since today

# Open dashboard
mantismo dashboard --open

# Check installation
mantismo doctor
```

## Architecture

Mantismo uses an **API-first architecture**: a Go backend exposes a REST + WebSocket API at `localhost:7777`. The CLI and web dashboard are both thin clients of this API.

```
Agent Host ──stdio──► Mantismo Proxy ──stdio──► MCP Server
                            │
                     Internal API (localhost:7777)
                      ├── CLI
                      └── Web Dashboard
```

For more detail, see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Configuration

Default config is auto-generated at `~/.mantismo/config.toml` on first run.

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

## License

Dual licensed under AGPL-3.0 and a commercial license. See [LICENSE](LICENSE) for details.
