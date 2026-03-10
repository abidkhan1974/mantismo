// Copyright 2026 Abid Ali Khan. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

package dashboard_test

import (
	"context"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/abidkhan1974/mantismo/internal/api"
	"github.com/abidkhan1974/mantismo/internal/dashboard"
	"github.com/abidkhan1974/mantismo/internal/logger"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAPIServer creates and starts an API server on a random free port.
func newAPIServer(t *testing.T) (*api.Server, chan api.ApprovalRequest) {
	t.Helper()
	logDir := t.TempDir()
	l, err := logger.New(logDir, "sess-dash")
	require.NoError(t, err)
	t.Cleanup(func() { l.Close() })
	approvalCh := make(chan api.ApprovalRequest, 8)
	sessions := api.NewSessionStore()
	srv := api.NewServer(api.Config{Port: 0, BindAddr: "127.0.0.1"}, api.Dependencies{
		Logger:     l,
		LogDir:     logDir,
		ApprovalCh: approvalCh,
		Sessions:   sessions,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	require.NoError(t, srv.Start(ctx))
	return srv, approvalCh
}

// TestStaticFileServing verifies index.html is served at "/" with 200 OK.
func TestStaticFileServing(t *testing.T) {
	ts := httptest.NewServer(dashboard.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/") //nolint:noctx
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), "<html")
	assert.Contains(t, string(body), "Mantismo")
}

// TestSPAFallbackRouting verifies that unknown paths return index.html (SPA routing).
func TestSPAFallbackRouting(t *testing.T) {
	ts := httptest.NewServer(dashboard.Handler())
	defer ts.Close()

	paths := []string{"/sessions", "/tools", "/logs", "/settings", "/some/unknown/path"}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			resp, err := http.Get(ts.URL + p) //nolint:noctx
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Contains(t, string(body), "<html")
		})
	}
}

// TestApprovalWebSocketFlow verifies that an approval request sent to approvalCh
// is forwarded to a connected WebSocket client.
func TestApprovalWebSocketFlow(t *testing.T) {
	srv, approvalCh := newAPIServer(t)

	conn, _, err := websocket.DefaultDialer.Dial("ws://"+srv.Addr()+"/api/ws/approvals", nil)
	require.NoError(t, err)
	defer conn.Close()

	// Allow a moment for the server to register the WS client.
	time.Sleep(50 * time.Millisecond)

	req := api.ApprovalRequest{ID: "test-1", Method: "tools/call", ToolName: "write_file"}
	approvalCh <- req

	conn.SetReadDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck
	_, msg, err := conn.ReadMessage()
	require.NoError(t, err)
	assert.Contains(t, string(msg), "test-1")
	assert.Contains(t, string(msg), "write_file")
}

// TestLogStreamWebSocket verifies that a PublishLog call reaches connected WS log clients.
func TestLogStreamWebSocket(t *testing.T) {
	srv, _ := newAPIServer(t)

	conn, _, err := websocket.DefaultDialer.Dial("ws://"+srv.Addr()+"/api/ws/logs", nil)
	require.NoError(t, err)
	defer conn.Close()

	// Allow a moment for the subscription to be registered.
	time.Sleep(50 * time.Millisecond)

	entry := logger.LogEntry{
		Timestamp:   time.Now().UTC(),
		SessionID:   "sess-dash",
		Direction:   "to_server",
		MessageType: "request",
		Method:      "tools/call",
		ToolName:    "test_tool",
		Summary:     "-> tools/call test_tool",
	}
	srv.PublishLog(entry)

	conn.SetReadDeadline(time.Now().Add(5 * time.Second)) //nolint:errcheck
	_, msg, err := conn.ReadMessage()
	require.NoError(t, err)
	assert.Contains(t, string(msg), "tools/call")
}

// TestMultipleDashboardClients verifies that multiple WS clients all receive
// the same approval request (fan-out / broadcast behavior).
func TestMultipleDashboardClients(t *testing.T) {
	srv, approvalCh := newAPIServer(t)

	conn1, _, err := websocket.DefaultDialer.Dial("ws://"+srv.Addr()+"/api/ws/approvals", nil)
	require.NoError(t, err)
	defer conn1.Close()

	conn2, _, err := websocket.DefaultDialer.Dial("ws://"+srv.Addr()+"/api/ws/approvals", nil)
	require.NoError(t, err)
	defer conn2.Close()

	// Allow both clients to register.
	time.Sleep(50 * time.Millisecond)

	req := api.ApprovalRequest{ID: "multi-1", ToolName: "delete_file"}

	// The approvalCh is read by a single goroutine per WS connection, so we
	// send one request per client.
	approvalCh <- req
	approvalCh <- req

	deadline := time.Now().Add(5 * time.Second)
	conn1.SetReadDeadline(deadline) //nolint:errcheck
	conn2.SetReadDeadline(deadline) //nolint:errcheck

	_, msg1, err := conn1.ReadMessage()
	require.NoError(t, err)
	_, msg2, err := conn2.ReadMessage()
	require.NoError(t, err)

	assert.Contains(t, string(msg1), "multi-1")
	assert.Contains(t, string(msg2), "multi-1")
}

// TestLocalhostBinding verifies the API server binds only to 127.0.0.1.
func TestLocalhostBinding(t *testing.T) {
	srv, _ := newAPIServer(t)
	addr := srv.Addr()
	assert.True(t, strings.HasPrefix(addr, "127.0.0.1:"), "server should bind to 127.0.0.1, got %s", addr)
}

// TestDashboardWithoutActiveSession verifies /api/stats and /api/logs respond
// with 200 OK even when no session is running.
func TestDashboardWithoutActiveSession(t *testing.T) {
	srv, _ := newAPIServer(t)

	resp, err := http.Get("http://" + srv.Addr() + "/api/stats") //nolint:noctx
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	resp2, err := http.Get("http://" + srv.Addr() + "/api/logs") //nolint:noctx
	require.NoError(t, err)
	defer resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
}

// TestResponsiveLayout verifies that the served index.html contains a viewport
// meta tag required for responsive layouts.
func TestResponsiveLayout(t *testing.T) {
	ts := httptest.NewServer(dashboard.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/") //nolint:noctx
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	html := string(body)

	assert.Contains(t, html, `name="viewport"`, "index.html must have a viewport meta tag")
	assert.Contains(t, html, `width=device-width`, "viewport must be device-width")
}

// TestNoHardcodedURLs walks all .jsx/.js/.ts/.tsx/.html source files in ui/src
// and asserts that none contain hardcoded "localhost" or "127.0.0.1".
func TestNoHardcodedURLs(t *testing.T) {
	// In Go tests the working directory is the package directory (internal/dashboard/).
	wd, err := os.Getwd()
	require.NoError(t, err)
	srcDir := filepath.Join(wd, "ui", "src")

	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		t.Skip("ui/src not found, skipping")
	}

	var hardcodedFiles []string
	err = filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".jsx" && ext != ".js" && ext != ".ts" && ext != ".tsx" && ext != ".html" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		s := string(content)
		if strings.Contains(s, "localhost") || strings.Contains(s, "127.0.0.1") {
			hardcodedFiles = append(hardcodedFiles, path)
		}
		return nil
	})
	require.NoError(t, err)
	assert.Empty(t, hardcodedFiles, "these source files contain hardcoded URLs: %v", hardcodedFiles)
}
