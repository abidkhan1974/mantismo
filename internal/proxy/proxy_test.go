package proxy_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/inferalabs/mantismo/internal/proxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockServerPath returns the absolute path to the mock MCP server script.
func mockServerPath(t *testing.T) string {
	t.Helper()
	// Walk up from the test file's directory to find testdata/
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller failed")
	dir := filepath.Dir(file)
	// internal/proxy → project root
	root := filepath.Join(dir, "..", "..")
	p := filepath.Join(root, "testdata", "mock_mcp_server.py")
	abs, err := filepath.Abs(p)
	require.NoError(t, err)
	return abs
}

// python3 returns the python3 executable path (skip if not found).
func python3(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"python3", "python"} {
		if path, err := findExec(name); err == nil {
			return path
		}
	}
	t.Skip("python3 not found — skipping integration test")
	return ""
}

func findExec(name string) (string, error) {
	dirs := filepath.SplitList(os.Getenv("PATH"))
	dirs = append(dirs, "/usr/local/bin", "/usr/bin", "/bin")
	for _, d := range dirs {
		p := filepath.Join(d, name)
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s not found", name)
}

// sendRequest writes a JSON-RPC request to w and reads one response from r.
func sendRequest(t *testing.T, w io.Writer, r *bufio.Scanner, id int, method string, params interface{}) json.RawMessage {
	t.Helper()
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		req["params"] = params
	}
	b, err := json.Marshal(req)
	require.NoError(t, err)
	_, err = fmt.Fprintf(w, "%s\n", b)
	require.NoError(t, err)

	require.True(t, r.Scan(), "expected response line")
	return json.RawMessage(r.Bytes())
}

// ── Unit Tests ──────────────────────────────────────────────────────────────

// TestMessageFraming verifies that Scanner correctly reads newline-delimited
// JSON-RPC messages, skipping blank lines, and handles large + unicode messages.
func TestMessageFraming(t *testing.T) {
	t.Parallel()

	messages := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"result":{"content":"こんにちは世界"}}`,
	}

	// Mix in some blank lines
	var buf bytes.Buffer
	for i, m := range messages {
		buf.WriteString(m)
		buf.WriteByte('\n')
		if i == 1 {
			buf.WriteByte('\n') // blank line between messages 2 and 3
		}
	}

	sc := proxy.NewScanner(&buf)
	var got []string
	for sc.Scan() {
		got = append(got, string(sc.Bytes()))
	}
	require.NoError(t, sc.Err())
	assert.Equal(t, messages, got)
}

// TestPassthroughHandler verifies that the passthrough handler returns messages unchanged.
func TestPassthroughHandler(t *testing.T) {
	t.Parallel()

	msg := json.RawMessage(`{"jsonrpc":"2.0","id":1,"method":"tools/call"}`)
	out, err := proxy.PassthroughHandler(msg, proxy.ToServer)
	require.NoError(t, err)
	assert.JSONEq(t, string(msg), string(out))
}

// TestNilHandlerDropsMessage verifies that returning nil from a handler drops the message.
func TestNilHandlerDropsMessage(t *testing.T) {
	t.Parallel()

	py := python3(t)
	script := mockServerPath(t)

	// Handler that drops all ToServer messages
	dropHandler := func(msg json.RawMessage, dir proxy.Direction) (json.RawMessage, error) {
		if dir == proxy.ToServer {
			return nil, nil // drop
		}
		return msg, nil
	}

	hostIn, hostInW := io.Pipe()
	hostOutR, hostOut := io.Pipe()

	cfg := proxy.Config{Command: py, Args: []string{script}}
	p := proxy.NewWithIO(cfg, dropHandler, hostIn, hostOut)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	// Write a message that should be dropped (never reaches mock server)
	_, _ = fmt.Fprintf(hostInW, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n")

	// Close stdin — proxy should exit cleanly since no messages flow to server
	hostInW.Close()
	hostOutR.Close()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("timeout waiting for proxy to exit")
	}
}

// TestErrorHandlerReturnsJsonRpcError verifies that when a ToServer handler
// returns an error, a JSON-RPC error response is written back to the host.
func TestErrorHandlerReturnsJsonRpcError(t *testing.T) {
	t.Parallel()

	py := python3(t)
	script := mockServerPath(t)

	// Handler that errors on all ToServer messages
	errHandler := func(msg json.RawMessage, dir proxy.Direction) (json.RawMessage, error) {
		if dir == proxy.ToServer {
			return nil, fmt.Errorf("blocked by test handler")
		}
		return msg, nil
	}

	hostInR, hostInW := io.Pipe()
	hostOutR, hostOutW := io.Pipe()

	cfg := proxy.Config{Command: py, Args: []string{script}}
	p := proxy.NewWithIO(cfg, errHandler, hostInR, hostOutW)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()

	// Write a message that will be blocked
	_, err := fmt.Fprintf(hostInW, `{"jsonrpc":"2.0","id":42,"method":"tools/call"}`+"\n")
	require.NoError(t, err)

	// Read the error response
	sc := bufio.NewScanner(hostOutR)
	require.True(t, sc.Scan(), "expected error response from proxy")

	var resp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(sc.Bytes(), &resp))
	assert.Equal(t, 42, resp.ID)
	assert.NotZero(t, resp.Error.Code)
	assert.Contains(t, resp.Error.Message, "blocked by test handler")

	hostInW.Close()
	hostOutR.Close()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("timeout waiting for proxy to exit after error test")
	}
}

// ── Integration Tests ────────────────────────────────────────────────────────

// runProxy starts a proxy over custom pipes, calls setup, then returns pipes for interaction.
func runProxy(t *testing.T, handler proxy.MessageHandler, args ...string) (
	hostWriter io.WriteCloser,
	hostReader *bufio.Scanner,
	waitDone <-chan error,
	cancel context.CancelFunc,
) {
	t.Helper()
	py := python3(t)
	script := mockServerPath(t)

	hostInR, hostInW := io.Pipe()
	hostOutR, hostOutW := io.Pipe()

	allArgs := append([]string{script}, args...)
	cfg := proxy.Config{Command: py, Args: allArgs}
	p := proxy.NewWithIO(cfg, handler, hostInR, hostOutW)

	ctx, ctxCancel := context.WithTimeout(context.Background(), 15*time.Second)
	doneCh := make(chan error, 1)
	go func() { doneCh <- p.Run(ctx) }()

	t.Cleanup(func() {
		ctxCancel()
		hostInW.Close()
		hostOutR.Close()
	})

	return hostInW, bufio.NewScanner(hostOutR), doneCh, ctxCancel
}

// doInitialize sends the MCP initialize handshake and verifies the response.
func doInitialize(t *testing.T, w io.Writer, r *bufio.Scanner) {
	t.Helper()
	resp := sendRequest(t, w, r, 1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test", "version": "0.1"},
	})

	var result struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(resp, &result))
	assert.Equal(t, "2025-11-25", result.Result.ProtocolVersion)
}

// TestProxyStartStop starts the proxy with the mock server, sends initialize,
// then shuts it down and verifies clean exit.
func TestProxyStartStop(t *testing.T) {
	w, r, done, cancel := runProxy(t, nil)

	doInitialize(t, w, r)

	// Send shutdown
	resp := sendRequest(t, w, r, 2, "shutdown", nil)
	var env proxy.Envelope
	require.NoError(t, json.Unmarshal(resp, &env))
	assert.Nil(t, env.Error)

	// Close stdin → proxy should exit
	w.Close()
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("proxy did not exit after shutdown")
	}
}

// TestProxyForwardsMessages sends 20 sequential requests and verifies all responses.
func TestProxyForwardsMessages(t *testing.T) {
	w, r, _, _ := runProxy(t, nil)

	doInitialize(t, w, r)

	const N = 20
	for i := 2; i <= N+1; i++ {
		resp := sendRequest(t, w, r, i, "tools/call", map[string]interface{}{
			"name":      "get_file_contents",
			"arguments": map[string]string{"path": fmt.Sprintf("/file%d", i)},
		})

		var result struct {
			ID     int `json:"id"`
			Result struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"result"`
		}
		require.NoError(t, json.Unmarshal(resp, &result), "response %d failed to parse", i)
		assert.Equal(t, i, result.ID, "wrong id in response %d", i)
		assert.NotEmpty(t, result.Result.Content, "empty content in response %d", i)
	}
}

// TestProxyConcurrentMessages sends requests concurrently and verifies all responses by ID.
func TestProxyConcurrentMessages(t *testing.T) {
	py := python3(t)
	script := mockServerPath(t)

	hostInR, hostInW := io.Pipe()
	hostOutR, hostOutW := io.Pipe()

	cfg := proxy.Config{Command: py, Args: []string{script, "--slow", "10"}}
	p := proxy.NewWithIO(cfg, nil, hostInR, hostOutW)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	go func() { _ = p.Run(ctx) }()

	// First do the initialize synchronously to establish the session
	initReq := `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}` + "\n"
	_, _ = fmt.Fprint(hostInW, initReq)

	// Read the initialize response
	sc := bufio.NewScanner(hostOutR)
	require.True(t, sc.Scan(), "expected initialize response")

	const N = 10
	responses := make(chan json.RawMessage, N)

	// Start a reader goroutine
	go func() {
		for i := 0; i < N; i++ {
			if !sc.Scan() {
				break
			}
			responses <- json.RawMessage(append([]byte(nil), sc.Bytes()...))
		}
		close(responses)
	}()

	// Send N concurrent requests
	for i := 1; i <= N; i++ {
		req := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":"get_file_contents","arguments":{"path":"/file%d"}}}`, i, i)
		_, err := fmt.Fprintln(hostInW, req)
		require.NoError(t, err)
	}

	// Collect all responses and verify IDs
	seenIDs := make(map[int]bool)
	deadline := time.After(25 * time.Second)
	for i := 0; i < N; i++ {
		select {
		case resp := <-responses:
			var env proxy.Envelope
			require.NoError(t, json.Unmarshal(resp, &env))
			var id int
			require.NoError(t, json.Unmarshal(*env.ID, &id))
			assert.False(t, seenIDs[id], "duplicate response id %d", id)
			seenIDs[id] = true
		case <-deadline:
			t.Fatalf("timeout: only got %d/%d responses", len(seenIDs), N)
		}
	}
	assert.Len(t, seenIDs, N)

	hostInW.Close()
	hostOutR.Close()
}

// TestProxyHandlesServerCrash verifies that when the mock server exits mid-stream,
// the proxy exits cleanly (possibly with an error, but not a panic or hang).
func TestProxyHandlesServerCrash(t *testing.T) {
	// --crash-after 1: server processes message 1 normally, then crashes when
	// message 2 arrives (message_count=2 > crash_after=1).
	w, r, done, _ := runProxy(t, nil, "--crash-after", "1")

	// Drain server responses in a background goroutine so the proxy's
	// Server→Host goroutine never blocks on a full pipe write.
	go func() {
		for r.Scan() {
		}
	}()

	// Send initialize (message 1) — server processes it and sends a response.
	_, _ = fmt.Fprint(w, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0.1"}}}`+"\n")
	// Send a second message — this triggers the crash.
	_, _ = fmt.Fprint(w, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`+"\n")
	// Close host stdin so the Host→Server goroutine unblocks via EOF and can
	// exit after the server crash (blocking Read cannot be cancelled by context).
	w.Close()

	// Proxy should exit after the server crashes — any exit code is acceptable.
	select {
	case <-done:
		// crash is expected; exit error is fine
	case <-time.After(10 * time.Second):
		t.Fatal("proxy hung after server crash")
	}
}

// TestProxyHandlesStdinEOF closes the host stdin and verifies the proxy exits cleanly.
func TestProxyHandlesStdinEOF(t *testing.T) {
	w, _, done, _ := runProxy(t, nil)

	// Close host stdin immediately
	w.Close()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("proxy hung after stdin EOF")
	}
}

// TestMakeErrorResponse verifies the MakeErrorResponse helper produces valid JSON-RPC.
func TestMakeErrorResponse(t *testing.T) {
	t.Parallel()

	idRaw := json.RawMessage(`42`)
	resp := proxy.MakeErrorResponse(&idRaw, -32601, "Method not found")

	var parsed struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Error   struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(resp, &parsed))
	assert.Equal(t, "2.0", parsed.JSONRPC)
	assert.Equal(t, 42, parsed.ID)
	assert.Equal(t, -32601, parsed.Error.Code)
	assert.Equal(t, "Method not found", parsed.Error.Message)
}

// TestParseEnvelope verifies that ParseEnvelope correctly extracts routing fields.
func TestParseEnvelope(t *testing.T) {
	t.Parallel()

	t.Run("request", func(t *testing.T) {
		t.Parallel()
		raw := json.RawMessage(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{}}`)
		env, err := proxy.ParseEnvelope(raw)
		require.NoError(t, err)
		assert.Equal(t, "tools/call", env.Method)
		require.NotNil(t, env.ID)
		assert.Equal(t, "7", string(*env.ID))
	})

	t.Run("notification", func(t *testing.T) {
		t.Parallel()
		raw := json.RawMessage(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
		env, err := proxy.ParseEnvelope(raw)
		require.NoError(t, err)
		assert.Equal(t, "notifications/initialized", env.Method)
		assert.Nil(t, env.ID)
	})

	t.Run("response", func(t *testing.T) {
		t.Parallel()
		raw := json.RawMessage(`{"jsonrpc":"2.0","id":3,"result":{"tools":[]}}`)
		env, err := proxy.ParseEnvelope(raw)
		require.NoError(t, err)
		assert.Empty(t, env.Method)
		require.NotNil(t, env.ID)
	})

	t.Run("invalid_json", func(t *testing.T) {
		t.Parallel()
		_, err := proxy.ParseEnvelope(json.RawMessage(`{not valid json`))
		assert.Error(t, err)
	})
}

// TestWriteMessage verifies that WriteMessage appends a newline.
func TestWriteMessage(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	msg := json.RawMessage(`{"jsonrpc":"2.0","id":1}`)
	require.NoError(t, proxy.WriteMessage(&buf, msg))
	assert.Equal(t, string(msg)+"\n", buf.String())
}

// TestLargeMessageFraming verifies that large messages (up to MaxMessageSize) work.
func TestLargeMessageFraming(t *testing.T) {
	t.Parallel()

	// Build a 1MB payload
	payload := strings.Repeat("x", 1*1024*1024)
	msg := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"result":{"data":"%s"}}`, payload)

	var buf bytes.Buffer
	buf.WriteString(msg)
	buf.WriteByte('\n')

	sc := proxy.NewScanner(&buf)
	require.True(t, sc.Scan())
	assert.Equal(t, msg, string(sc.Bytes()))
	assert.NoError(t, sc.Err())
}
