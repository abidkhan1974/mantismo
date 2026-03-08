# Mantismo Architecture

Mantismo is a transparent proxy that sits between your AI agent host and your MCP servers. Neither side needs modification — Mantismo is invisible unless it intervenes.

## How It Works

```
Agent Host ──stdio──► Mantismo Proxy ──stdio──► MCP Server
(Claude Desktop,            │
 Cursor, VS Code)    Internal API (localhost:7777)
                      ├── CLI
                      ├── Web Dashboard
                      └── Future clients
```

Every MCP tool call passes through Mantismo, which:

1. **Inspects** the request — extracts the tool name and arguments
2. **Evaluates** it against your policy rules (allow / deny / require approval)
3. **Logs** the decision to a structured audit log
4. **Scans** for secrets in both the request and response
5. **Forwards** (or blocks) the request, then logs the result

## Key Properties

- **Local-first** — all data stays on your machine, no cloud account required
- **API-first** — the CLI and web dashboard are both thin clients of the same local REST + WebSocket API
- **Single binary** — one Go binary, no runtime dependencies
- **Transparent** — wraps any stdio MCP server with a single command

## Data Storage

All data lives in `~/.mantismo/`:

| Path | Contents |
|------|----------|
| `config.toml` | Configuration |
| `logs/YYYY-MM-DD.jsonl` | Structured audit logs (one file per day) |
| `vault.db` | Encrypted personal data vault (optional) |
| `fingerprints.json` | Tool description hashes (rug-pull detection) |
| `policies/` | OPA policy files |

## Policy Presets

| Preset | Behaviour |
|--------|-----------|
| `permissive` | Log everything, block nothing |
| `balanced` | Log everything, require approval for writes and deletions |
| `paranoid` | Require approval for every tool call |

## License

Dual licensed under AGPL-3.0 and a commercial license. See [LICENSE](../LICENSE).
