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
