package interceptor_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/inferalabs/mantismo/internal/interceptor"
	"github.com/inferalabs/mantismo/internal/proxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func callHandle(t *testing.T, i *interceptor.Interceptor, msg json.RawMessage, dir proxy.Direction) (json.RawMessage, error) {
	t.Helper()
	return i.Handle(msg, dir)
}

// ── Unit Tests ─────────────────────────────────────────────────────────────────

// TestParseRequest verifies that a tools/call request is correctly parsed and
// delivered to the OnToolCall hook with the right tool name and arguments.
func TestParseRequest(t *testing.T) {
	t.Parallel()

	var captured interceptor.ToolCallRequest
	i := interceptor.New(interceptor.Hooks{
		OnToolCall: func(req interceptor.ToolCallRequest) interceptor.InterceptResult {
			captured = req
			return interceptor.InterceptResult{Action: interceptor.Forward}
		},
	})

	raw := json.RawMessage(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/etc/hosts"}}}`)
	out, err := callHandle(t, i, raw, proxy.ToServer)
	require.NoError(t, err)
	assert.JSONEq(t, string(raw), string(out), "forwarded message should be unchanged")

	assert.Equal(t, "read_file", captured.ToolName)
	assert.JSONEq(t, `{"path":"/etc/hosts"}`, string(captured.Arguments))
	assert.Equal(t, "7", string(captured.RequestID))
}

// TestParseResponse verifies that a tools/call response is correctly correlated
// back to its request by ID and delivered to OnToolCallResponse.
func TestParseResponse(t *testing.T) {
	t.Parallel()

	var capturedResp interceptor.ToolCallResponse
	var capturedReq interceptor.ToolCallRequest

	i := interceptor.New(interceptor.Hooks{
		OnToolCall: func(req interceptor.ToolCallRequest) interceptor.InterceptResult {
			return interceptor.InterceptResult{Action: interceptor.Forward}
		},
		OnToolCallResponse: func(resp interceptor.ToolCallResponse, req interceptor.ToolCallRequest) interceptor.InterceptResult {
			capturedResp = resp
			capturedReq = req
			return interceptor.InterceptResult{Action: interceptor.Forward}
		},
	})

	// Send request (ToServer) so it is tracked.
	req := json.RawMessage(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"my_tool","arguments":{}}}`)
	_, err := callHandle(t, i, req, proxy.ToServer)
	require.NoError(t, err)

	// Send matching response (FromServer).
	resp := json.RawMessage(`{"jsonrpc":"2.0","id":5,"result":{"content":[{"type":"text","text":"ok"}],"isError":false}}`)
	out, err := callHandle(t, i, resp, proxy.FromServer)
	require.NoError(t, err)
	assert.NotNil(t, out, "response should be forwarded")

	assert.Equal(t, "5", string(capturedResp.RequestID))
	assert.False(t, capturedResp.IsError)
	assert.Equal(t, "my_tool", capturedReq.ToolName)
}

// TestParseNotification verifies that a notification (no id) is forwarded
// unchanged and the OnAnyMessage hook still fires.
func TestParseNotification(t *testing.T) {
	t.Parallel()

	anyCount := 0
	i := interceptor.New(interceptor.Hooks{
		OnAnyMessage: func(_ interceptor.MCPMessage, _ proxy.Direction) {
			anyCount++
		},
	})

	notif := json.RawMessage(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	out, err := callHandle(t, i, notif, proxy.ToServer)
	require.NoError(t, err)
	assert.JSONEq(t, string(notif), string(out))
	assert.Equal(t, 1, anyCount)
}

// TestParseErrorResponse verifies that an error response is correctly routed to
// OnToolCallResponse with IsError == true.
func TestParseErrorResponse(t *testing.T) {
	t.Parallel()

	var capturedResp interceptor.ToolCallResponse

	i := interceptor.New(interceptor.Hooks{
		OnToolCall: func(req interceptor.ToolCallRequest) interceptor.InterceptResult {
			return interceptor.InterceptResult{Action: interceptor.Forward}
		},
		OnToolCallResponse: func(resp interceptor.ToolCallResponse, _ interceptor.ToolCallRequest) interceptor.InterceptResult {
			capturedResp = resp
			return interceptor.InterceptResult{Action: interceptor.Forward}
		},
	})

	req := json.RawMessage(`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"bad_tool","arguments":{}}}`)
	_, err := callHandle(t, i, req, proxy.ToServer)
	require.NoError(t, err)

	errResp := json.RawMessage(`{"jsonrpc":"2.0","id":9,"error":{"code":-32601,"message":"Method not found"}}`)
	out, err := callHandle(t, i, errResp, proxy.FromServer)
	require.NoError(t, err)
	assert.NotNil(t, out)

	assert.True(t, capturedResp.IsError)
	assert.Contains(t, capturedResp.ErrorMsg, "Method not found")
}

// TestRequestResponseCorrelation verifies that sending a request then a response
// with the same JSON-RPC id fires OnToolCallResponse with both pieces of data.
func TestRequestResponseCorrelation(t *testing.T) {
	t.Parallel()

	fired := false

	i := interceptor.New(interceptor.Hooks{
		OnToolCall: func(req interceptor.ToolCallRequest) interceptor.InterceptResult {
			return interceptor.InterceptResult{Action: interceptor.Forward}
		},
		OnToolCallResponse: func(resp interceptor.ToolCallResponse, orig interceptor.ToolCallRequest) interceptor.InterceptResult {
			fired = true
			assert.Equal(t, "42", string(resp.RequestID), "response id should match")
			assert.Equal(t, "read_file", orig.ToolName, "original tool name should be preserved")
			assert.False(t, resp.IsError)
			return interceptor.InterceptResult{Action: interceptor.Forward}
		},
	})

	req := json.RawMessage(`{"jsonrpc":"2.0","id":42,"method":"tools/call","params":{"name":"read_file","arguments":{"path":"/tmp"}}}`)
	_, err := callHandle(t, i, req, proxy.ToServer)
	require.NoError(t, err)

	// Response with a DIFFERENT id must NOT fire the hook.
	wrongResp := json.RawMessage(`{"jsonrpc":"2.0","id":99,"result":{"content":[],"isError":false}}`)
	_, err = callHandle(t, i, wrongResp, proxy.FromServer)
	require.NoError(t, err)
	assert.False(t, fired, "wrong id should not fire correlation hook")

	// Response with the CORRECT id must fire the hook.
	goodResp := json.RawMessage(`{"jsonrpc":"2.0","id":42,"result":{"content":[{"type":"text","text":"hello"}],"isError":false}}`)
	_, err = callHandle(t, i, goodResp, proxy.FromServer)
	require.NoError(t, err)
	assert.True(t, fired, "matching id should fire correlation hook")
}

// TestToolListAugmentation verifies that OnToolsList can inject tools into the
// tools/list response and the modified response is what the host receives.
func TestToolListAugmentation(t *testing.T) {
	t.Parallel()

	mantismoTool := interceptor.ToolInfo{
		Name:        "vault_get_profile",
		Description: "Retrieve a profile from the Mantismo vault",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"}},"required":["key"]}`),
	}

	i := interceptor.New(interceptor.Hooks{
		OnToolsList: func(tools []interceptor.ToolInfo) ([]interceptor.ToolInfo, error) {
			return append(tools, mantismoTool), nil
		},
	})

	// Prime the request tracker.
	req := json.RawMessage(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	_, err := callHandle(t, i, req, proxy.ToServer)
	require.NoError(t, err)

	// Server response.
	resp := json.RawMessage(`{"jsonrpc":"2.0","id":3,"result":{"tools":[{"name":"server_tool","description":"A server tool","inputSchema":{"type":"object"}}]}}`)
	out, err := callHandle(t, i, resp, proxy.FromServer)
	require.NoError(t, err)
	require.NotNil(t, out)

	var result struct {
		Result struct {
			Tools []interceptor.ToolInfo `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(out, &result))
	require.Len(t, result.Result.Tools, 2, "should have original + injected tool")
	assert.Equal(t, "server_tool", result.Result.Tools[0].Name)
	assert.Equal(t, "vault_get_profile", result.Result.Tools[1].Name)
}

// TestNativeToolRouting verifies that tools/call for "vault_*" tools is consumed
// (not forwarded to the MCP server) and the OnToolCall hook is NOT called.
func TestNativeToolRouting(t *testing.T) {
	t.Parallel()

	hookFired := false
	i := interceptor.New(interceptor.Hooks{
		OnToolCall: func(req interceptor.ToolCallRequest) interceptor.InterceptResult {
			hookFired = true // must never fire for native tools
			return interceptor.InterceptResult{Action: interceptor.Forward}
		},
	})

	raw := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"vault_get_profile","arguments":{"key":"github"}}}`)
	out, err := callHandle(t, i, raw, proxy.ToServer)
	require.NoError(t, err)
	assert.Nil(t, out, "native tool call must NOT be forwarded to the server")
	assert.False(t, hookFired, "OnToolCall must not fire for native vault_* tools")
}

// TestSamplingBlocked verifies that sampling/createMessage from the server is
// blocked by default (no OnSamplingRequest hook), and the message is dropped.
func TestSamplingBlocked(t *testing.T) {
	t.Parallel()

	i := interceptor.New(interceptor.Hooks{})

	raw := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"sampling/createMessage","params":{"messages":[]}}`)
	out, err := callHandle(t, i, raw, proxy.FromServer)

	assert.Nil(t, out, "sampling request must not be forwarded to host")
	require.Error(t, err, "a block action must return an error")
	assert.Contains(t, err.Error(), "blocked")
}

// TestUnknownMethodPassthrough verifies that methods not known to the interceptor
// are forwarded unchanged (transparent proxy behaviour preserved).
func TestUnknownMethodPassthrough(t *testing.T) {
	t.Parallel()

	i := interceptor.New(interceptor.Hooks{})

	raw := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"custom/method","params":{}}`)
	out, err := callHandle(t, i, raw, proxy.ToServer)
	require.NoError(t, err)
	assert.JSONEq(t, string(raw), string(out))
}

// TestOnAnyMessageCalledForAll verifies that the logging hook fires for every
// message in both directions, regardless of type or method.
func TestOnAnyMessageCalledForAll(t *testing.T) {
	t.Parallel()

	var received []interceptor.MCPMessage

	i := interceptor.New(interceptor.Hooks{
		OnAnyMessage: func(msg interceptor.MCPMessage, _ proxy.Direction) {
			received = append(received, msg)
		},
	})

	messages := []struct {
		raw json.RawMessage
		dir proxy.Direction
	}{
		{json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`), proxy.ToServer},
		{json.RawMessage(`{"jsonrpc":"2.0","method":"notifications/initialized"}`), proxy.ToServer},
		{json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-11-25","capabilities":{},"serverInfo":{"name":"t","version":"0"}}}`), proxy.FromServer},
		{json.RawMessage(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`), proxy.ToServer},
		{json.RawMessage(`{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}`), proxy.FromServer},
	}

	for _, m := range messages {
		_, _ = callHandle(t, i, m.raw, m.dir)
	}

	assert.Len(t, received, len(messages), "OnAnyMessage must fire for every message")
}

// ── Integration Tests ──────────────────────────────────────────────────────────

// mockServerPath returns the path to the mock MCP server.
func mockServerPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	abs, err := filepath.Abs(filepath.Join(root, "testdata", "mock_mcp_server.py"))
	require.NoError(t, err)
	return abs
}

// python3 returns the path to python3, or skips the test.
func python3(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3", "python"} {
		dirs := append(filepath.SplitList(os.Getenv("PATH")), "/usr/local/bin", "/usr/bin", "/bin")
		for _, d := range dirs {
			p := filepath.Join(d, name)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	t.Skip("python3 not found")
	return ""
}

// sendLine writes a newline-terminated message to w.
func sendLine(t *testing.T, w io.Writer, msg string) {
	t.Helper()
	_, err := fmt.Fprintln(w, msg)
	require.NoError(t, err)
}

// readLine reads one line from sc and returns the raw bytes.
func readLine(t *testing.T, sc *bufio.Scanner) json.RawMessage {
	t.Helper()
	require.True(t, sc.Scan(), "expected a message from proxy")
	return json.RawMessage(append([]byte(nil), sc.Bytes()...))
}

// TestInterceptorWithProxy wires the Interceptor into the Proxy and sends a
// real initialize → tools/list → tools/call sequence through the mock server,
// verifying that all hooks fire.
func TestInterceptorWithProxy(t *testing.T) {
	py := python3(t)
	script := mockServerPath(t)

	initFired, toolListFired, toolCallFired := false, false, false
	anyCount := 0

	ic := interceptor.New(interceptor.Hooks{
		OnInitialize: func(_ json.RawMessage) interceptor.InterceptResult {
			initFired = true
			return interceptor.InterceptResult{Action: interceptor.Forward}
		},
		OnToolsList: func(tools []interceptor.ToolInfo) ([]interceptor.ToolInfo, error) {
			toolListFired = true
			return tools, nil
		},
		OnToolCall: func(req interceptor.ToolCallRequest) interceptor.InterceptResult {
			toolCallFired = true
			assert.Equal(t, "get_file_contents", req.ToolName)
			return interceptor.InterceptResult{Action: interceptor.Forward}
		},
		OnAnyMessage: func(_ interceptor.MCPMessage, _ proxy.Direction) {
			anyCount++
		},
	})

	hostInR, hostInW := io.Pipe()
	hostOutR, hostOutW := io.Pipe()
	defer hostOutR.Close()

	cfg := proxy.Config{Command: py, Args: []string{script}}
	p := proxy.NewWithIO(cfg, ic.Handle, hostInR, hostOutW)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	go func() { _ = p.Run(ctx) }()

	sc := bufio.NewScanner(hostOutR)

	// initialize
	sendLine(t, hostInW, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}`)
	readLine(t, sc)

	// notifications/initialized (no response)
	sendLine(t, hostInW, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	// tools/list
	sendLine(t, hostInW, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	readLine(t, sc)

	// tools/call
	sendLine(t, hostInW, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"get_file_contents","arguments":{"path":"/test"}}}`)
	readLine(t, sc)

	hostInW.Close()

	assert.True(t, initFired, "OnInitialize must fire")
	assert.True(t, toolListFired, "OnToolsList must fire")
	assert.True(t, toolCallFired, "OnToolCall must fire")
	assert.Greater(t, anyCount, 0, "OnAnyMessage must fire at least once")
}

// TestAugmentedToolsVisible verifies end-to-end that the agent host receives a
// tools/list response containing tools injected by the OnToolsList hook.
func TestAugmentedToolsVisible(t *testing.T) {
	py := python3(t)
	script := mockServerPath(t)

	injected := interceptor.ToolInfo{
		Name:        "vault_get_profile",
		Description: "Retrieve a stored profile",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"}},"required":["key"]}`),
	}

	ic := interceptor.New(interceptor.Hooks{
		OnToolsList: func(tools []interceptor.ToolInfo) ([]interceptor.ToolInfo, error) {
			return append(tools, injected), nil
		},
	})

	hostInR, hostInW := io.Pipe()
	hostOutR, hostOutW := io.Pipe()
	defer hostOutR.Close()

	cfg := proxy.Config{Command: py, Args: []string{script}}
	p := proxy.NewWithIO(cfg, ic.Handle, hostInR, hostOutW)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	go func() { _ = p.Run(ctx) }()

	sc := bufio.NewScanner(hostOutR)

	// initialize
	sendLine(t, hostInW, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}`)
	readLine(t, sc) // discard initialize response

	// tools/list — interceptor augments the response
	sendLine(t, hostInW, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	toolsRaw := readLine(t, sc)

	hostInW.Close()

	var result struct {
		Result struct {
			Tools []interceptor.ToolInfo `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(toolsRaw, &result))

	names := make([]string, len(result.Result.Tools))
	for idx, tool := range result.Result.Tools {
		names[idx] = tool.Name
	}

	assert.Contains(t, names, "vault_get_profile", "injected tool must be visible to agent host")
	assert.Greater(t, len(names), 1, "server's original tools must also be present")
}
