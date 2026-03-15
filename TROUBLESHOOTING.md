# Troubleshooting

## `make build` fails with `go: command not found`

Go is not in your shell's PATH. Either add it:

```bash
export PATH="$HOME/go-install/go/bin:$PATH"
```

Or build directly:

```bash
CGO_ENABLED=0 ~/go-install/go/bin/go build -o bin/mantismo ./cmd/mantismo/
```

---

## macOS: Binary killed immediately (`zsh: killed`)

Gatekeeper is rejecting a locally-built binary.

```bash
cd /path/to/mantismo
codesign --sign - bin/mantismo
```

Then run from the build directory (`bin/mantismo`), not from a copied location.

---

## macOS: `internal error in Code Signing subsystem`

Occurs when copying the binary to `/usr/local/bin` with `cp`. Use `install` instead:

```bash
sudo install -m 755 bin/mantismo /usr/local/bin/mantismo
```

---

## macOS: `spctl --assess` shows "rejected"

Same root cause as above. Fix with:

```bash
codesign --sign - /path/to/mantismo
```

---

## Claude Desktop: MCP server not connecting

1. Verify config is correct:
   ```bash
   cat ~/Library/Application\ Support/Claude/claude_desktop_config.json
   ```
2. Fully quit Claude Desktop (Cmd+Q — not just closing the window) and relaunch.
3. Check logs after relaunch:
   ```bash
   mantismo logs --since today
   ```
   You should see `→ initialize` within a few seconds of Claude Desktop starting.

---

## `mantismo logs` shows nothing

- Mantismo only captures traffic while `mantismo wrap` is running as the MCP transport.
- Confirm your `claude_desktop_config.json` uses `mantismo wrap -- <server-cmd>` as the command.
- After fixing config, fully quit and relaunch Claude Desktop.

---

## `mantismo logs` response lines show `← → OK` (missing method name)

This means you're running a binary older than v0.1.2. Rebuild:

```bash
cd /path/to/mantismo
make build
sudo install -m 755 bin/mantismo /usr/local/bin/mantismo
```

---

## `mantismo tools` shows "Not implemented yet"

Same as above — stale binary. Rebuild and reinstall.

---

## Claude Desktop rewrites `claude_desktop_config.json`

Claude Desktop may reformat the file on launch but preserves all keys. Verify with:

```bash
cat ~/Library/Application\ Support/Claude/claude_desktop_config.json
```

If the `mantismo wrap` command is still present, it's working correctly.

---

## Approval popup never appears in the dashboard

Several things must all be true for approvals to work:

1. **Preset must be `balanced` or `paranoid`** — `permissive` never triggers approvals.
   Check your config: `mantismo wrap --preset balanced -- <server-cmd>`

2. **Dashboard must be open** *before* the tool call is made. The WebSocket backend is only
   registered when a browser tab is connected. If no tab is open, the terminal backend is used
   instead (approval prompt appears in the terminal/MCP log, not the browser).

3. **Open a fresh Claude conversation** after changing config. Claude Desktop caches tool
   definitions from the old session. Start a new conversation so it re-initializes.

---

## API server port conflict: `address already in use`

If you run two `mantismo wrap` processes (e.g., during testing), the second one will fail to
bind port 7777. As of v0.2.0 mantismo detects this and reuses the existing API server.

If you hit this with an older binary, kill the first process and restart:

```bash
pkill -f "mantismo wrap"
```

---

## Write tools bypass approval (one-time grant)

When you approve a write tool call in the dashboard, it grants permission for that call only.
The next call to the same tool requires a fresh approval. This is by design — use **Grant for session**
if you want to allow repeated calls in the same session without re-approving.

---

## Claude reports it cannot access a path (e.g., `/tmp`)

macOS resolves `/tmp` to `/private/tmp` at the filesystem level. If your MCP server is configured
with `/tmp` as the allowed path, tool calls that reference `/private/tmp` may be rejected.

Fix: use `/Users/<your-username>` or an absolute path that does not resolve to a symlink.

---

## Tool reads on new/changed servers require approval

With the `balanced` preset, tool reads (`get_`, `list_`, etc.) on a *changed* server are allowed
by default (reads are safe). Only writes and calls on changed servers require approval. If you see
unexpected approvals for read tools right after changing a server version, rebuild and reinstall —
earlier builds had this rule ordering wrong.
