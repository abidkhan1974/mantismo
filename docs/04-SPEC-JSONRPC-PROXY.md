# 04 — Spec: JSON-RPC 2.0 Proxy Engine (stdio)

## Objective

Build a transparent stdio proxy that spawns an MCP server as a subprocess and forwards JSON-RPC 2.0 messages bidirectionally between the agent host's stdin/stdout and the MCP server's stdin/stdout. At this stage, the proxy is completely transparent — it does not inspect, modify, or filter messages. That comes in 05-MCP-INTERCEPTOR.

## Context

MCP uses JSON-RPC 2.0 over stdio transport. The agent host (e.g., Claude Desktop) spawns the MCP server as a subprocess, writing JSON-RPC requests to the server's stdin and reading responses from the server's stdout. The server writes notifications and responses to stdout and reads requests from stdin.

Mantismo inserts itself by becoming the spawned process. It then spawns the real MCP server as its own child process and proxies all stdio traffic.

```
Agent Host ──stdin──► Mantismo ──stdin──► MCP Server
Agent Host ◄──stdout── Mantismo ◄──stdout── MCP Server
Agent Host ◄──stderr── Mantismo ◄──stderr── MCP Server (passthrough)
```

## JSON-RPC 2.0 Message Format

Messages are newline-delimited JSON objects. Each message is one of:

**Request:**
```json
{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": {"name": "get_file", "arguments": {"path": "/etc/hosts"}}}
```

**Response:**
```json
{"jsonrpc": "2.0", "id": 1, "result": {"content": [{"type": "text", "text": "..."}]}}
```

**Notification (no id):**
```json
{"jsonrpc": "2.0", "method": "notifications/initialized"}
```

**Error:**
```json
{"jsonrpc": "2.0", "id": 1, "error": {"code": -32600, "message": "Invalid Request"}}
```

## Interface Contract

### Package: `internal/proxy`

```go
// ProxyConfig holds the configuration for the stdio proxy.
type ProxyConfig struct {
    Command string   // The MCP server command to execute
    Args    []string // Arguments to the MCP server command
    Env     []string // Additional environment variables (optional)
    WorkDir string   // Working directory for the subprocess (optional)
}

// MessageHandler is called for each JSON-RPC message passing through the proxy.
// Direction indicates whether the message is going TO the server or FROM the server.
// The handler returns the (possibly modified) message, or an error.
// If the handler returns a nil message, the message is dropped (not forwarded).
type Direction int
const (
    ToServer   Direction = iota // Message from agent host heading to MCP server
    FromServer                  // Message from MCP server heading to agent host
)

type MessageHandler func(msg json.RawMessage, dir Direction) (json.RawMessage, error)

// Proxy manages the MCP server subprocess and message forwarding.
type Proxy struct {
    config  ProxyConfig
    handler MessageHandler
    cmd     *exec.Cmd
    // ... internal fields
}

// New creates a new Proxy instance.
func New(config ProxyConfig, handler MessageHandler) *Proxy

// Run starts the MCP server subprocess and begins proxying messages.
// It blocks until the subprocess exits or the context is cancelled.
// Returns the subprocess exit error (nil if clean exit).
func (p *Proxy) Run(ctx context.Context) error
```

## Detailed Requirements

### 4.1 Subprocess Management

- Spawn the MCP server using `exec.CommandContext` with the provided command and args
- Connect the server's stdin to a pipe that Mantismo writes to
- Connect the server's stdout to a pipe that Mantismo reads from
- Connect the server's stderr directly to Mantismo's stderr (passthrough, no interception)
- Propagate environment variables: inherit current env + any additional env from config
- Set working directory if specified in config

### 4.2 Message Framing

- Read messages from both pipes using a buffered scanner with newline delimiter
- Each line is one complete JSON-RPC message
- Maximum message size: 10 MB (configurable; protects against malformed streams)
- Handle partial reads gracefully (buffer until newline)
- Handle empty lines (skip them)

### 4.3 Bidirectional Forwarding

Two goroutines run concurrently:

**Host → Server goroutine:**
1. Read line from Mantismo's os.Stdin
2. Call `handler(msg, ToServer)`
3. If handler returns non-nil message, write to MCP server's stdin pipe
4. If handler returns nil, do not forward (message was consumed/blocked)
5. If handler returns error, write JSON-RPC error response to Mantismo's os.Stdout

**Server → Host goroutine:**
1. Read line from MCP server's stdout pipe
2. Call `handler(msg, FromServer)`
3. If handler returns non-nil message, write to Mantismo's os.Stdout
4. If handler returns nil, do not forward
5. If handler returns error, log error and continue (don't crash)

### 4.4 Shutdown Handling

- On context cancellation: send SIGTERM to subprocess, wait 5 seconds, then SIGKILL
- On subprocess exit: cancel the context, drain any remaining buffered messages, exit with subprocess's exit code
- On stdin EOF (agent host disconnected): signal subprocess to exit
- On stdout pipe close (server crashed): log error, exit with code 1

### 4.5 Error Handling

- If the MCP server command is not found: exit with clear error message to stderr
- If the MCP server exits unexpectedly: log the exit code and signal to stderr, then exit
- If a message fails to parse as JSON: forward it unchanged (don't break non-standard messages)
- Never panic; all errors are handled and logged

### 4.6 Initial Handler (Passthrough)

For this spec, the default handler is a passthrough:
```go
func PassthroughHandler(msg json.RawMessage, dir Direction) (json.RawMessage, error) {
    return msg, nil
}
```

This ensures the proxy is fully transparent until the Interceptor (spec 05) is wired in.

## Test Plan

### Unit Tests

1. **TestMessageFraming** — Write JSON messages to a pipe, verify the reader extracts them correctly (including empty lines, large messages, messages with unicode)
2. **TestPassthroughHandler** — Verify messages pass through unchanged
3. **TestNilHandlerDropsMessage** — Handler returns nil, verify message is not forwarded
4. **TestErrorHandlerReturnsJsonRpcError** — Handler returns error, verify error response is written

### Integration Tests

Use the mock MCP server (`testdata/mock_mcp_server.py` — a simple Python script that reads JSON-RPC from stdin and echoes responses):

5. **TestProxyStartStop** — Start proxy with mock server, send `initialize` request, verify response, send shutdown, verify clean exit
6. **TestProxyForwardsMessages** — Send 100 sequential requests, verify all 100 responses are received in order
7. **TestProxyConcurrentMessages** — Send requests concurrently, verify all responses match (by id)
8. **TestProxyHandlesServerCrash** — Mock server exits mid-stream, verify proxy exits cleanly with error log
9. **TestProxyHandlesStdinEOF** — Close stdin, verify proxy signals server to exit
10. **TestProxySignalForwarding** — Send SIGINT to proxy process, verify it terminates subprocess gracefully

### Mock MCP Server (`testdata/mock_mcp_server.py`)

Create a minimal Python script that:
- Reads JSON-RPC requests from stdin (line-delimited)
- For `initialize`: responds with server capabilities
- For `tools/list`: responds with a hardcoded list of test tools
- For `tools/call`: echoes back the tool name and arguments
- For `shutdown`: exits cleanly
- Accepts a `--crash-after N` flag to exit after N messages (for crash testing)
- Accepts a `--slow` flag to add 100ms delay per response (for concurrency testing)

## Acceptance Criteria

- [ ] `mantismo wrap -- python testdata/mock_mcp_server.py` starts and proxies messages
- [ ] All JSON-RPC messages pass through with zero modification
- [ ] MCP server's stderr is visible on Mantismo's stderr
- [ ] Proxy exits cleanly when subprocess exits
- [ ] Proxy forwards SIGINT/SIGTERM to subprocess
- [ ] All 10 tests pass
- [ ] No goroutine leaks (verified by `goleak` or runtime check in tests)
