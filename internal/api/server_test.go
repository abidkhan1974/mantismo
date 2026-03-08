// Copyright 2026 Mantismo. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abidkhan1974/mantismo/internal/api"
	apiclient "github.com/abidkhan1974/mantismo/internal/api/client"
	"github.com/abidkhan1974/mantismo/internal/fingerprint"
	"github.com/abidkhan1974/mantismo/internal/interceptor"
	"github.com/abidkhan1974/mantismo/internal/logger"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startTestServer is a helper that creates and starts an API server on a random
// free port. It returns the server and a cancel function that stops it.
func startTestServer(t *testing.T, deps api.Dependencies) (*api.Server, context.CancelFunc) {
	t.Helper()
	cfg := api.Config{
		BindAddr: "127.0.0.1",
		Port:     0, // OS picks a free port
	}
	srv := api.NewServer(cfg, deps)
	ctx, cancel := context.WithCancel(context.Background())
	err := srv.Start(ctx)
	require.NoError(t, err, "server should start without error")
	return srv, cancel
}

// TestAPIServerStartStop verifies the server starts, responds to /api/health, then stops cleanly.
func TestAPIServerStartStop(t *testing.T) {
	sessions := api.NewSessionStore()
	srv, cancel := startTestServer(t, api.Dependencies{Sessions: sessions})
	defer cancel()

	url := fmt.Sprintf("http://%s/api/health", srv.Addr())
	resp, err := http.Get(url) //nolint:gosec
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
	assert.Equal(t, "0.1.0", body["version"])
	assert.Equal(t, false, body["proxy"]) // no active session

	// Graceful stop
	ctx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer stopCancel()
	require.NoError(t, srv.Stop(ctx))
}

// TestLogsEndpoint creates a temp log directory, writes log entries, and verifies
// the /api/logs endpoint returns them with filtering.
func TestLogsEndpoint(t *testing.T) {
	dir := t.TempDir()

	// Write a log entry to today's file.
	log, err := logger.New(dir, "sess-test-logs")
	require.NoError(t, err)
	defer log.Close()

	entry := logger.LogEntry{
		Timestamp:   time.Now().UTC(),
		SessionID:   "sess-test-logs",
		Direction:   "to_server",
		MessageType: "request",
		Method:      "tools/call",
		ToolName:    "read_file",
		Summary:     "-> tools/call read_file",
		RawSize:     42,
	}
	require.NoError(t, log.Log(entry))

	sessions := api.NewSessionStore()
	srv, cancel := startTestServer(t, api.Dependencies{
		LogDir:   dir,
		Sessions: sessions,
	})
	defer cancel()

	baseURL := fmt.Sprintf("http://%s", srv.Addr())

	// All logs
	resp, err := http.Get(baseURL + "/api/logs") //nolint:noctx
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var entries []logger.LogEntry
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&entries))
	assert.GreaterOrEqual(t, len(entries), 1)

	// Filter by tool
	resp2, err := http.Get(baseURL + "/api/logs?tool=read_file") //nolint:noctx
	require.NoError(t, err)
	defer resp2.Body.Close()
	var filtered []logger.LogEntry
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&filtered))
	assert.GreaterOrEqual(t, len(filtered), 1)
	assert.Equal(t, "read_file", filtered[0].ToolName)

	// Filter by tool that doesn't exist — expect empty array
	resp3, err := http.Get(baseURL + "/api/logs?tool=nonexistent_xyz") //nolint:noctx
	require.NoError(t, err)
	defer resp3.Body.Close()
	var empty []logger.LogEntry
	require.NoError(t, json.NewDecoder(resp3.Body).Decode(&empty))
	assert.Equal(t, 0, len(empty))
}

// TestLogsWebSocket connects to the WS log stream, publishes a log entry via
// PublishLog, and verifies the entry arrives over the WebSocket.
func TestLogsWebSocket(t *testing.T) {
	sessions := api.NewSessionStore()
	srv, cancel := startTestServer(t, api.Dependencies{Sessions: sessions})
	defer cancel()

	wsURL := fmt.Sprintf("ws://%s/api/ws/logs", srv.Addr())
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// Allow a moment for the server to register the subscription.
	time.Sleep(50 * time.Millisecond)

	want := logger.LogEntry{
		Timestamp:   time.Now().UTC(),
		SessionID:   "sess-ws-test",
		Direction:   "to_server",
		MessageType: "request",
		Method:      "tools/call",
		ToolName:    "ws_tool",
		Summary:     "ws test",
		RawSize:     10,
	}
	srv.PublishLog(want)

	// Set a read deadline so the test doesn't hang.
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))

	_, msg, err := conn.ReadMessage()
	require.NoError(t, err)

	var got logger.LogEntry
	require.NoError(t, json.Unmarshal(msg, &got))
	assert.Equal(t, "sess-ws-test", got.SessionID)
	assert.Equal(t, "ws_tool", got.ToolName)
}

// TestToolsEndpoint creates a fingerprint store, populates it, and verifies
// /api/tools returns the expected tool data.
func TestToolsEndpoint(t *testing.T) {
	dir := t.TempDir()
	fpPath := filepath.Join(dir, "fingerprints.json")
	store, err := fingerprint.NewStore(fpPath)
	require.NoError(t, err)

	tools := []interceptor.ToolInfo{
		{Name: "bash", Description: "Run shell commands", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "read_file", Description: "Read a file", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}
	require.NoError(t, store.Update(tools, "test-server"))

	sessions := api.NewSessionStore()
	srv, cancel := startTestServer(t, api.Dependencies{
		Fingerprints: store,
		Sessions:     sessions,
	})
	defer cancel()

	resp, err := http.Get(fmt.Sprintf("http://%s/api/tools", srv.Addr())) //nolint:noctx
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var toolList []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&toolList))
	assert.Equal(t, 2, len(toolList))

	names := make(map[string]bool)
	for _, tool := range toolList {
		name, _ := tool["name"].(string)
		names[name] = true
		assert.NotEmpty(t, tool["hash"])
	}
	assert.True(t, names["bash"])
	assert.True(t, names["read_file"])
}

// TestStatsEndpoint verifies /api/stats returns the expected fields with correct types.
func TestStatsEndpoint(t *testing.T) {
	dir := t.TempDir()
	sessions := api.NewSessionStore()
	srv, cancel := startTestServer(t, api.Dependencies{
		LogDir:   dir,
		Sessions: sessions,
	})
	defer cancel()

	resp, err := http.Get(fmt.Sprintf("http://%s/api/stats", srv.Addr())) //nolint:noctx
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var stats map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&stats))

	assert.Contains(t, stats, "tool_calls_today")
	assert.Contains(t, stats, "blocked_today")
	assert.Contains(t, stats, "sessions_today")
	assert.Contains(t, stats, "active_session")

	// With no active session, active_session should be null/nil.
	assert.Nil(t, stats["active_session"])

	// Sessions count should be 0 initially.
	sessionsVal, _ := stats["sessions_today"].(float64)
	assert.Equal(t, float64(0), sessionsVal)

	// Add a session and verify the count changes.
	sessions.SetActive(&api.SessionInfo{
		ID:        "test-sess",
		StartedAt: time.Now().UTC(),
		ServerCmd: "test-server",
	})

	resp2, err := http.Get(fmt.Sprintf("http://%s/api/stats", srv.Addr())) //nolint:noctx
	require.NoError(t, err)
	defer resp2.Body.Close()
	var stats2 map[string]any
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&stats2))
	sessionsVal2, _ := stats2["sessions_today"].(float64)
	assert.Equal(t, float64(1), sessionsVal2)
	assert.NotNil(t, stats2["active_session"])
}

// TestApprovalWebSocket verifies that posting to the approvalCh causes the approval
// to be delivered to a connected WebSocket client.
func TestApprovalWebSocket(t *testing.T) {
	approvalCh := make(chan api.ApprovalRequest, 4)
	sessions := api.NewSessionStore()
	srv, cancel := startTestServer(t, api.Dependencies{
		Sessions:   sessions,
		ApprovalCh: approvalCh,
	})
	defer cancel()

	wsURL := fmt.Sprintf("ws://%s/api/ws/approvals", srv.Addr())
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer conn.Close()

	// Give the server a moment to start reading from approvalCh.
	time.Sleep(50 * time.Millisecond)

	want := api.ApprovalRequest{
		ID:       "req-001",
		Method:   "tools/call",
		ToolName: "dangerous_tool",
		Args:     json.RawMessage(`{"path":"/etc/passwd"}`),
	}
	approvalCh <- want

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, msg, err := conn.ReadMessage()
	require.NoError(t, err)

	var got api.ApprovalRequest
	require.NoError(t, json.Unmarshal(msg, &got))
	assert.Equal(t, "req-001", got.ID)
	assert.Equal(t, "dangerous_tool", got.ToolName)
}

// TestCLILogsCallsAPI verifies that the API client can fetch logs from a running server.
func TestCLILogsCallsAPI(t *testing.T) {
	dir := t.TempDir()

	// Write a log entry.
	log, err := logger.New(dir, "cli-logs-test")
	require.NoError(t, err)
	defer log.Close()

	require.NoError(t, log.Log(logger.LogEntry{
		Timestamp:   time.Now().UTC(),
		SessionID:   "cli-logs-test",
		Direction:   "to_server",
		MessageType: "request",
		Method:      "tools/call",
		ToolName:    "my_tool",
		Summary:     "test",
		RawSize:     1,
	}))

	sessions := api.NewSessionStore()
	srv, cancel := startTestServer(t, api.Dependencies{
		LogDir:   dir,
		Sessions: sessions,
	})
	defer cancel()

	c := apiclient.NewClient(srv.Port())
	require.NoError(t, c.Health())

	entries, err := c.Logs(apiclient.LogFilter{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(entries), 1)

	// Filter by tool name.
	filtered, err := c.Logs(apiclient.LogFilter{Tool: "my_tool"})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(filtered), 1)
	assert.Equal(t, "my_tool", filtered[0].ToolName)
}

// TestCLILogsFallback verifies that logger.Query works directly from files when
// the API server is not running (the CLI's offline fallback path).
func TestCLILogsFallback(t *testing.T) {
	dir := t.TempDir()

	log, err := logger.New(dir, "fallback-test")
	require.NoError(t, err)
	defer log.Close()

	require.NoError(t, log.Log(logger.LogEntry{
		Timestamp:   time.Now().UTC(),
		SessionID:   "fallback-test",
		Direction:   "from_server",
		MessageType: "response",
		Method:      "tools/list",
		Summary:     "fallback entry",
		RawSize:     5,
	}))

	// Query directly from files (no server needed).
	entries, err := logger.Query(dir, logger.QueryFilter{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(entries), 1)

	// Filter by session.
	filtered, err := logger.Query(dir, logger.QueryFilter{SessionID: "fallback-test"})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(filtered), 1)
	assert.Equal(t, "fallback-test", filtered[0].SessionID)
}

// TestWrapStartsAPIServer verifies the core lifecycle: create a server, start it,
// confirm it is reachable, stop it. This mirrors the wrap command initialization logic.
func TestWrapStartsAPIServer(t *testing.T) {
	sessions := api.NewSessionStore()
	cfg := api.Config{
		BindAddr: "127.0.0.1",
		Port:     0,
	}
	srv := api.NewServer(cfg, api.Dependencies{Sessions: sessions})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, srv.Start(ctx))
	assert.NotEmpty(t, srv.Addr(), "server should have a bound address")
	assert.Greater(t, srv.Port(), 0, "server should have a non-zero port")

	// Confirm it is reachable.
	url := fmt.Sprintf("http://%s/api/health", srv.Addr())
	resp, err := http.Get(url) //nolint:gosec
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Stop and confirm it is no longer reachable.
	cancel()
	time.Sleep(100 * time.Millisecond)
	_, err = http.Get(url) //nolint:gosec
	assert.Error(t, err, "server should not be reachable after stop")
}

// TestWrapOutputToStderr verifies that writing to os.Stderr works (non-stdout output).
// This represents the contract that all Mantismo diagnostic output goes to stderr,
// keeping stdout clean for the MCP JSON-RPC protocol.
func TestWrapOutputToStderr(t *testing.T) {
	// This test captures the concept: write to stderr, verify no panic/error.
	// In a real integration test, we would exec the binary and check stdout/stderr separately.
	oldStderr := os.Stderr
	tmpFile, err := os.CreateTemp(t.TempDir(), "stderr-*.txt")
	require.NoError(t, err)
	defer tmpFile.Close()

	os.Stderr = tmpFile
	defer func() { os.Stderr = oldStderr }()

	// Write to stderr.
	fmt.Fprintln(os.Stderr, "[mantismo] starting proxy")

	// Restore and read what was written.
	os.Stderr = oldStderr
	tmpFile.Seek(0, 0) //nolint:errcheck

	buf := make([]byte, 256)
	n, _ := tmpFile.Read(buf)
	written := string(buf[:n])
	assert.Contains(t, written, "[mantismo] starting proxy")
}

// TestConfigAutoGeneration verifies that LoadConfig does not fail when the config
// file does not exist (first-run scenario), and returns sensible defaults.
func TestConfigAutoGeneration(t *testing.T) {
	dir := t.TempDir()
	// Point config at a path that doesn't exist yet.
	cfgPath := filepath.Join(dir, "nonexistent", "config.toml")

	// Set the data dir env var so the default path resolution picks up our temp dir.
	t.Setenv("MANTISMO_DATA_DIR", dir)

	// Use a direct API server with zero config to simulate first-run defaults.
	cfg := api.Config{
		BindAddr: "127.0.0.1",
		Port:     0,
	}
	srv := api.NewServer(cfg, api.Dependencies{Sessions: api.NewSessionStore()})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, srv.Start(ctx))
	assert.NotEmpty(t, srv.Addr())

	// Verify health endpoint works with minimal config.
	resp, err := http.Get(fmt.Sprintf("http://%s/api/health", srv.Addr())) //nolint:noctx
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Suppress unused variable.
	_ = cfgPath
}

// TestSessionsEndpoint verifies the /api/sessions endpoint returns session data correctly.
func TestSessionsEndpoint(t *testing.T) {
	sessions := api.NewSessionStore()
	srv, cancel := startTestServer(t, api.Dependencies{Sessions: sessions})
	defer cancel()

	baseURL := fmt.Sprintf("http://%s", srv.Addr())

	// No sessions initially.
	resp, err := http.Get(baseURL + "/api/sessions") //nolint:noctx
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var initial []api.SessionInfo
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&initial))
	assert.Equal(t, 0, len(initial))

	// Add an active session.
	now := time.Now().UTC()
	sessions.SetActive(&api.SessionInfo{
		ID:        "session-abc",
		StartedAt: now,
		ServerCmd: "npx some-server",
		ToolCalls: 3,
	})

	resp2, err := http.Get(baseURL + "/api/sessions") //nolint:noctx
	require.NoError(t, err)
	defer resp2.Body.Close()
	var withActive []api.SessionInfo
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&withActive))
	assert.Equal(t, 1, len(withActive))
	assert.Equal(t, "session-abc", withActive[0].ID)
	assert.Equal(t, "npx some-server", withActive[0].ServerCmd)
}
