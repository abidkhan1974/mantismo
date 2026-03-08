# 01 — Product Brief: Mantismo

## What Is Mantismo?

Mantismo is a personal MCP security proxy. It sits between AI agent hosts (Claude Desktop, Cursor, VS Code) and MCP servers (GitHub, filesystem, database tools), intercepting, inspecting, and policy-filtering every tool call.

**One-liner:** A personal firewall for your AI agents.

**Tagline:** Own your context, lease it to agents on your terms.

## Who Is It For?

**Primary user (MVP):** Privacy-conscious developers and security professionals who use AI coding agents daily and are uncomfortable with the unchecked access those agents have to their tools and data.

**Not for (MVP):** Enterprise platform teams (they have Gravitee, Traefik Hub, etc.), non-technical consumers, or users who don't use MCP-based agents.

## Core Value Propositions

1. **Visibility:** See exactly what every AI agent accesses, when, and why — via structured audit logs
2. **Control:** Allow, deny, or require approval for any tool call based on configurable policies
3. **Protection:** Detect and block credential leaks, tool poisoning (rug pulls), and prompt injection-driven exfiltration
4. **Sovereignty:** Keep personal data in a local encrypted vault; agents query it through scoped, read-only MCP tools — never raw filesystem access

## Product Principles

- **Zero-config start:** `mantismo wrap -- <any mcp server command>` must work with no setup
- **Local-first:** All data stays on the user's machine. No cloud account required. No telemetry.
- **Transparent proxy:** Neither the agent host nor the MCP server needs modification. Mantismo is invisible unless it intervenes.
- **Single static binary:** Distribute as one Go binary. No runtime dependencies (no Node, no Python, no Docker for the user).
- **Progressive disclosure:** Start with logging only. User opts into policies, then vault, then dashboard — each layer additive.

## Explicit Non-Goals (MVP)

- HTTP+SSE transport (stdio only for MVP)
- Multi-user or multi-tenant support
- Cloud/SaaS hosting
- Mobile apps
- Agent marketplace
- Enterprise SSO/SAML integration
- Windows support (Linux + macOS only for MVP; Windows in Phase 2)

## Technical Constraints

- **Language:** Go 1.26+ (static binary, zero dependencies, fast cross-compilation)
- **Encryption:** SQLCipher for vault storage (AES-256), Argon2id for key derivation
- **Policy engine:** Embedded OPA (Open Policy Agent) via Go library
- **Config format:** TOML (`~/.mantismo/config.toml`)
- **Data directory:** `~/.mantismo/` (respects `$XDG_DATA_HOME` on Linux)
- **Minimum MCP spec version:** 2025-11-25 (current stable)

## Success Criteria (Launch)

1. Wrap any stdio-based MCP server with a single CLI command
2. Log 100% of MCP tool calls to queryable JSONL files
3. Detect tool description changes (rug-pull defense) between sessions
4. Block or allow tool calls based on OPA policies with three built-in presets
5. Scan tool call arguments for leaked secrets (API keys, tokens, passwords)
6. Store personal data in a local encrypted vault accessible to agents via scoped MCP tools
7. Provide a local web dashboard showing real-time agent activity

## Competitive Context

Mantismo is NOT competing with enterprise MCP gateways (Gravitee, Traefik Hub, Kong, MintMCP, IBM ContextForge). Those are infrastructure tools for platform teams.

Mantismo is the personal equivalent: `brew install mantismo`, one config line change, and your agents are governed. No Kubernetes. No Helm charts. No cloud account.

Think: Tailscale (personal VPN) vs. Cisco (enterprise VPN). Mantismo is Tailscale for MCP security.
