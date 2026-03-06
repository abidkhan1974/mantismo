# 15 — Testing Strategy

## Overview

Every spec has its own test plan. This document defines the cross-cutting testing strategy: test infrastructure, integration test harness, mock MCP server, and CI requirements.

## Test Pyramid

```
         ┌──────────┐
         │  E2E (5)  │  Full binary, real stdio, mock MCP server
        ┌┴──────────┴┐
        │  Integ (20) │  Multiple packages wired together
       ┌┴────────────┴┐
       │  Unit (80+)   │  Single function/method, no I/O
       └──────────────┘
```

## Mock MCP Server (`testdata/mock_mcp_server.py`)

A configurable Python script that simulates an MCP server for testing.

```python
#!/usr/bin/env python3
"""Mock MCP server for Mantismo testing.

Usage:
    python mock_mcp_server.py [options]

Options:
    --tools FILE        JSON file defining tools to expose
    --crash-after N     Exit after N messages
    --slow MS           Add delay per response (milliseconds)
    --poison TOOL       Make TOOL's description change on second tools/list call
    --leak-secret       Include a fake AWS key in tool responses
"""
```

### Capabilities

The mock server supports:
1. Standard `initialize` / `initialized` handshake
2. Configurable tool list (loaded from JSON fixture)
3. Echo-back tool calls (returns tool name + args in response)
4. Simulated crashes (for resilience testing)
5. Configurable latency (for timeout testing)
6. Tool poisoning simulation (description changes mid-session)
7. Secret injection (returns fake credentials in responses)
8. Sampling requests (for testing sampling/createMessage blocking)

### Tool Fixture Format (`testdata/sample_tools.json`)

```json
[
  {
    "name": "get_file_contents",
    "description": "Read the contents of a file",
    "inputSchema": {
      "type": "object",
      "properties": {
        "path": {"type": "string", "description": "File path to read"}
      },
      "required": ["path"]
    }
  },
  {
    "name": "create_file",
    "description": "Create a new file with content",
    "inputSchema": {
      "type": "object",
      "properties": {
        "path": {"type": "string"},
        "content": {"type": "string"}
      },
      "required": ["path", "content"]
    }
  },
  {
    "name": "delete_file",
    "description": "Delete a file permanently",
    "inputSchema": {
      "type": "object",
      "properties": {
        "path": {"type": "string"}
      },
      "required": ["path"]
    }
  }
]
```

## End-to-End Test Scenarios

### E2E-1: Happy Path
1. Start `mantismo wrap -- python mock_mcp_server.py`
2. Send `initialize` request via stdin
3. Verify `initialize` response on stdout
4. Send `tools/list` request
5. Verify response includes both mock server tools and vault tools
6. Send `tools/call get_file_contents`
7. Verify response
8. Send `shutdown`
9. Verify clean exit
10. Verify audit log contains all 4 interactions

### E2E-2: Policy Blocking
1. Start with `--preset paranoid`
2. Send `tools/call create_file` (write operation)
3. Verify approval prompt appears on stderr
4. Simulate denial
5. Verify error response returned to host
6. Verify audit log shows "deny" decision

### E2E-3: Secret Detection
1. Start with balanced preset
2. Send `tools/call` with AWS key in arguments
3. Verify call is blocked
4. Verify audit log shows "redacted" flag

### E2E-4: Tool Poisoning Detection
1. Start first session, capture tools/list
2. Start second session with `--poison get_file_contents` on mock server
3. Verify warning on stderr about changed tool description
4. Verify fingerprint store updated

### E2E-5: Vault Tool Invocation
1. Initialize vault with test data
2. Start proxy with vault enabled
3. Send `tools/call vault_get_profile`
4. Verify response contains vault data (not forwarded to mock server)
5. Verify mock server never received the vault tool call

## Test Infrastructure

### Helpers (`testdata/helpers.go`)

```go
// StartProxy starts a Mantismo proxy with the mock server for testing.
func StartProxy(t *testing.T, opts ...ProxyOption) *TestProxy

// TestProxy wraps a running proxy for test interaction.
type TestProxy struct {
    Stdin    io.Writer      // Write JSON-RPC requests here
    Stdout   *bufio.Scanner // Read JSON-RPC responses here
    Stderr   *bytes.Buffer  // Capture Mantismo output
    LogDir   string         // Temp dir for logs
    DataDir  string         // Temp dir for config/vault/fingerprints
    Process  *exec.Cmd
}

// SendRequest sends a JSON-RPC request and returns the response.
func (tp *TestProxy) SendRequest(method string, params interface{}) (json.RawMessage, error)

// WaitForStderr waits for a specific string to appear on stderr (with timeout).
func (tp *TestProxy) WaitForStderr(contains string, timeout time.Duration) error

// Close stops the proxy and cleans up temp directories.
func (tp *TestProxy) Close()
```

### Temp Directory Management

All tests that write to disk use `t.TempDir()` for isolation. No tests depend on `~/.mantismo/`.

### Parallel Test Safety

- Unit tests: always `t.Parallel()`
- Integration tests: parallel where possible, serial when sharing mock server
- E2E tests: serial (they spawn full processes)

## CI Requirements

### On Every PR

1. `make lint` — zero warnings
2. `make test` — all unit + integration tests pass
3. Race detector enabled (`-race` flag)
4. Coverage report generated (`-coverprofile`)
5. Minimum coverage threshold: 70% (enforced in CI)

### On Main Branch

All of the above, plus:
6. E2E tests run
7. Cross-compilation check (build for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64)
8. Binary size check (warn if > 50MB)

### On Tag

9. GoReleaser builds release binaries
10. Homebrew formula updated
11. GitHub Release created with checksums

## Goroutine Leak Detection

Use `go.uber.org/goleak` in test teardown:

```go
func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```

This catches goroutine leaks in the proxy (which heavily uses goroutines for bidirectional forwarding).

## Benchmark Tests

For performance-sensitive code:

```go
func BenchmarkPolicyEvaluation(b *testing.B) {
    engine := setupPolicyEngine(b)
    input := sampleEvalInput()
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        engine.Evaluate(input)
    }
}

func BenchmarkSecretScanning(b *testing.B) {
    scanner := scanner.NewScanner(nil)
    payload := loadLargeJSON(b) // 1MB payload
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        scanner.ScanJSON(payload)
    }
}
```

Target benchmarks:
- Policy evaluation: < 1ms per call
- Secret scanning: < 50ms for 1MB payload
- JSON-RPC proxy overhead: < 100μs per message
