# Mantismo — Project Documentation Index

**Product:** Mantismo — A Personal Firewall for AI Agents
**Date:** March 2026
**Status:** Pre-MVP

---

## How to Use These Documents with Claude Code

Each document is numbered and self-contained. Feed them to Claude Code in order. Each document produces a working, testable increment. Do NOT skip ahead — later documents assume earlier ones are complete and passing tests.

### Document Map

| # | Document | Purpose | Output | Est. Effort |
|---|----------|---------|--------|-------------|
| 01 | `01-PRODUCT-BRIEF.md` | Product vision, constraints, non-goals | Shared context (no code) | — |
| 02 | `02-ARCHITECTURE.md` | System architecture, component diagram, data flow | Shared context (no code) | — |
| 03 | `03-PROJECT-SETUP.md` | Go project scaffold, CI, linting, Makefile | Buildable empty project | Week 1 |
| 04 | `04-SPEC-JSONRPC-PROXY.md` | Core JSON-RPC 2.0 proxy engine (stdio transport) | Working transparent proxy | Weeks 1-2 |
| 05 | `05-SPEC-MCP-INTERCEPTOR.md` | MCP-aware message parsing and interception | Proxy understands MCP methods | Week 3 |
| 06 | `06-SPEC-LOGGING.md` | Structured audit logging subsystem | Every MCP call logged to JSONL | Week 3-4 |
| 07 | `07-SPEC-CLI.md` | Internal API server + CLI as thin client | API server + CLI | Week 4-5 |
| 08 | `08-SPEC-TOOL-FINGERPRINTING.md` | Tool description hashing and change detection | Rug-pull defense | Week 5-6 |
| 09 | `09-SPEC-POLICY-ENGINE.md` | OPA-based policy engine for tool call filtering | Allow/deny/approve decisions | Weeks 7-8 |
| 10 | `10-SPEC-SECRET-SCANNER.md` | Argument and response scanning for secrets | Credential leak prevention | Week 9 |
| 11 | `11-SPEC-APPROVAL-GATEWAY.md` | Multi-backend approval (terminal + WebSocket + future native) | Pluggable approval system | Week 10 |
| 12 | `12-SPEC-VAULT-STORAGE.md` | SQLite+SQLCipher encrypted personal data vault | Local encrypted store | Weeks 11-12 |
| 13 | `13-SPEC-VAULT-MCP-TOOLS.md` | Vault-native MCP tools exposed to agents | Agents query vault via MCP | Weeks 13-14 |
| 14 | `14-SPEC-DASHBOARD.md` | Tauri-ready React SPA dashboard | Browser-based UI (embeddable in Tauri) | Weeks 15-16 |
| 15 | `15-TESTING-STRATEGY.md` | Test plan, fixtures, integration test harness | Comprehensive test suite | Ongoing |
| 16 | `16-RELEASE-PACKAGING.md` | Build, cross-compile, Homebrew, install script | Distributable binary | Week 17 |

### Dependency Graph

```
01-PRODUCT-BRIEF ──┐
02-ARCHITECTURE ────┤
                    ▼
              03-PROJECT-SETUP
                    │
                    ▼
            04-JSONRPC-PROXY ◄── (core engine, everything depends on this)
                    │
                    ▼
           05-MCP-INTERCEPTOR
                 /     \
                ▼       ▼
          06-LOGGING   08-TOOL-FINGERPRINTING
               │
               ▼
            07-CLI
               │
               ▼
        09-POLICY-ENGINE
           /        \
          ▼          ▼
  10-SECRET-SCANNER  11-APPROVAL-GATEWAY
                          │
                          ▼
                   12-VAULT-STORAGE
                          │
                          ▼
                  13-VAULT-MCP-TOOLS
                          │
                          ▼
                    14-DASHBOARD
```

### Cross-Cutting Documents

- `15-TESTING-STRATEGY.md` — Referenced by every spec; defines how each component is tested
- `16-RELEASE-PACKAGING.md` — Final integration; depends on all specs being complete

### Conventions Used Across All Specs

- **Acceptance criteria** are written as testable assertions (`GIVEN / WHEN / THEN`)
- **Interface contracts** define exact function signatures, types, and error codes
- **File paths** are relative to the Go module root (`github.com/abidkhan1974/mantismo`)
- **Error handling** follows Go conventions: return `(result, error)`, never panic
- **Logging** uses `slog` (Go stdlib structured logging)
- **Configuration** uses TOML files (`~/.mantismo/config.toml`)
- **Data storage** lives in `~/.mantismo/` (XDG-aware on Linux)
