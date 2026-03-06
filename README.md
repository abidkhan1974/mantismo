# Mantismo

> **A personal firewall for your AI agents.** Own your context, lease it to agents on your terms.

![Work in Progress](https://img.shields.io/badge/status-work%20in%20progress-orange)
![License](https://img.shields.io/badge/license-Apache%202.0-blue)
![Go](https://img.shields.io/badge/go-1.22+-00ADD8)

## What Is Mantismo?

Mantismo is a personal MCP security proxy. It sits between AI agent hosts (Claude Desktop, Cursor, VS Code) and MCP servers (GitHub, filesystem, database tools), intercepting, inspecting, and policy-filtering every tool call.

**Core features:**
- **Visibility** — See every AI agent tool call in structured audit logs
- **Control** — Allow, deny, or require approval via configurable OPA policies
- **Protection** — Detect credential leaks and tool poisoning (rug pulls)
- **Sovereignty** — Store personal data in a local encrypted vault

## Quick Start (Build from Source)

**Prerequisites:** Go 1.22+, make

```bash
git clone https://github.com/inferalabs/mantismo
cd mantismo
make build

# Wrap any stdio MCP server
./bin/mantismo wrap -- npx -y @modelcontextprotocol/server-github

# View audit logs
./bin/mantismo logs --since today

# Open dashboard
./bin/mantismo dashboard --open
```

## Architecture

Mantismo uses an **API-first architecture**: a Go backend exposes a REST + WebSocket API at `localhost:7777`. The CLI and web dashboard are thin clients of this API, enabling a future Tauri desktop app with zero backend changes.

```
Agent Host ──stdio──► Mantismo Proxy ──stdio──► MCP Server
                            │
                     Internal API (localhost:7777)
                      ├── CLI (cobra)
                      ├── Web Dashboard (React)
                      └── Future: Tauri Desktop App
```

For the full architecture, see [docs/02-ARCHITECTURE.md](docs/02-ARCHITECTURE.md).

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

## Documentation

- [Product Brief](docs/01-PRODUCT-BRIEF.md)
- [Architecture](docs/02-ARCHITECTURE.md)
- [Testing Strategy](docs/15-TESTING-STRATEGY.md)

## License

Apache 2.0 — see [LICENSE](LICENSE).
