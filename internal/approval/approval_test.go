// Copyright 2026 Abid Ali Khan. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// ---------------------------------------------------------------------------
// mockBackend implements Backend for testing.
// ---------------------------------------------------------------------------

type mockBackend struct {
	mu        sync.Mutex
	name      string
	priority  int
	available bool
	response  ApprovalResponse
	delay     time.Duration
	callCount int
}

func (m *mockBackend) Name() string  { return m.name }
func (m *mockBackend) Priority() int { return m.priority }
func (m *mockBackend) Available() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.available
}

func (m *mockBackend) Prompt(ctx context.Context, _ ApprovalPrompt) (ApprovalResponse, error) {
	m.mu.Lock()
	m.callCount++
	delay := m.delay
	resp := m.response
	m.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return ApprovalResponse{Decision: Denied}, nil
		}
	}
	return resp, nil
}

func (m *mockBackend) getCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

// ---------------------------------------------------------------------------
// Helper to create a test prompt.
// ---------------------------------------------------------------------------

func newTestPrompt(toolName string) ApprovalPrompt {
	now := time.Now()
	return ApprovalPrompt{
		ID:        fmt.Sprintf("test-%d-%s", now.UnixNano(), toolName),
		ToolName:  toolName,
		ServerCmd: "test-server",
		Reason:    "test reason",
		Arguments: "{}",
		RiskLevel: "low",
		CreatedAt: now,
		ExpiresAt: now.Add(30 * time.Second),
	}
}

// ---------------------------------------------------------------------------
// Test 1: TestTerminalApproval
// ---------------------------------------------------------------------------

func TestTerminalApproval(t *testing.T) {
	tb := newTerminalBackendWithIO(strings.NewReader("1\n"), &strings.Builder{})
	gw := NewGateway(5*time.Second, "", tb)

	prompt := newTestPrompt("test_tool_terminal")
	grant, err := gw.RequestApproval(context.Background(), prompt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if grant.Decision != Approved {
		t.Errorf("expected Approved, got %v", grant.Decision)
	}
	if grant.Scope != ThisCallOnly {
		t.Errorf("expected ThisCallOnly, got %v", grant.Scope)
	}
}

// ---------------------------------------------------------------------------
// Test 2: TestWebSocketApproval
// ---------------------------------------------------------------------------

func TestWebSocketApproval(t *testing.T) {
	wsb := NewWebSocketBackend()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade error: %v", err)
			return
		}
		wsb.HandleConnection(conn)
	}))
	defer ts.Close()

	// Connect a client.
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil) //nolint:bodyclose
	if err != nil {
		t.Fatalf("dial error: %v", err)
	}
	defer clientConn.Close()

	// Give HandleConnection time to register the client.
	time.Sleep(20 * time.Millisecond)

	gw := NewGateway(5*time.Second, "", wsb)
	prompt := newTestPrompt("test_tool_ws")

	var grant Grant
	var gwErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		grant, gwErr = gw.RequestApproval(context.Background(), prompt)
	}()

	// The client reads the approval_request and sends back approval_response.
	_, msg, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("client read error: %v", err)
	}

	var req struct {
		Type string         `json:"type"`
		Data ApprovalPrompt `json:"data"`
	}
	if err := json.Unmarshal(msg, &req); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if req.Type != "approval_request" {
		t.Fatalf("expected approval_request, got %q", req.Type)
	}

	resp := fmt.Sprintf(`{"type":"approval_response","data":{"id":%q,"decision":"approved","grant_scope":"session"}}`,
		req.Data.ID)
	if err := clientConn.WriteMessage(websocket.TextMessage, []byte(resp)); err != nil {
		t.Fatalf("client write error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("RequestApproval timed out")
	}

	if gwErr != nil {
		t.Fatalf("gateway error: %v", gwErr)
	}
	if grant.Decision != Approved {
		t.Errorf("expected Approved, got %v", grant.Decision)
	}
}

// ---------------------------------------------------------------------------
// Test 3: TestBackendPriority
// ---------------------------------------------------------------------------

func TestBackendPriority(t *testing.T) {
	low := &mockBackend{
		name: "low-priority", priority: 100, available: true,
		response: ApprovalResponse{Decision: Approved, GrantScope: ThisCallOnly},
	}
	high := &mockBackend{
		name: "high-priority", priority: 10, available: true,
		response: ApprovalResponse{Decision: Approved, GrantScope: ThisCallOnly},
	}

	// Pass in reverse order to verify sorting.
	gw := NewGateway(5*time.Second, "", low, high)
	prompt := newTestPrompt("priority_tool")
	_, err := gw.RequestApproval(context.Background(), prompt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if high.getCallCount() != 1 {
		t.Errorf("expected high-priority backend called once, got %d", high.getCallCount())
	}
	if low.getCallCount() != 0 {
		t.Errorf("expected low-priority backend not called, got %d", low.getCallCount())
	}
}

// ---------------------------------------------------------------------------
// Test 4: TestBackendFallback
// ---------------------------------------------------------------------------

func TestBackendFallback(t *testing.T) {
	unavailable := &mockBackend{
		name: "unavailable", priority: 10, available: false,
		response: ApprovalResponse{Decision: Approved, GrantScope: ThisCallOnly},
	}
	fallback := &mockBackend{
		name: "fallback", priority: 100, available: true,
		response: ApprovalResponse{Decision: Approved, GrantScope: ThisCallOnly},
	}

	gw := NewGateway(5*time.Second, "", unavailable, fallback)
	prompt := newTestPrompt("fallback_tool")
	_, err := gw.RequestApproval(context.Background(), prompt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if unavailable.getCallCount() != 0 {
		t.Errorf("unavailable backend should not have been called")
	}
	if fallback.getCallCount() != 1 {
		t.Errorf("expected fallback backend called once, got %d", fallback.getCallCount())
	}
}

// ---------------------------------------------------------------------------
// Test 5: TestNoBackendsTimeout
// ---------------------------------------------------------------------------

func TestNoBackendsTimeout(t *testing.T) {
	gw := NewGateway(100*time.Millisecond, "")

	prompt := newTestPrompt("timeout_tool")
	start := time.Now()
	grant, err := gw.RequestApproval(context.Background(), prompt)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if grant.Decision != Timeout {
		t.Errorf("expected Timeout, got %v", grant.Decision)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("took too long: %v", elapsed)
	}
}

// ---------------------------------------------------------------------------
// Test 6: TestSessionGrant
// ---------------------------------------------------------------------------

func TestSessionGrant(t *testing.T) {
	mb := &mockBackend{
		name: "mock", priority: 10, available: true,
		response: ApprovalResponse{Decision: Approved, GrantScope: For5Minutes},
	}

	gw := NewGateway(5*time.Second, "", mb)

	prompt1 := newTestPrompt("cached_tool")
	_, err := gw.RequestApproval(context.Background(), prompt1)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}

	// Second call should hit cache, not backend.
	prompt2 := newTestPrompt("cached_tool")
	grant2, err := gw.RequestApproval(context.Background(), prompt2)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}

	if mb.getCallCount() != 1 {
		t.Errorf("expected backend called once, got %d", mb.getCallCount())
	}
	if grant2.Decision != Approved {
		t.Errorf("expected Approved from cache, got %v", grant2.Decision)
	}
}

// ---------------------------------------------------------------------------
// Test 7: TestSessionGrantExpiry
// ---------------------------------------------------------------------------

func TestSessionGrantExpiry(t *testing.T) {
	mb := &mockBackend{
		name: "mock", priority: 10, available: true,
		response: ApprovalResponse{Decision: Approved, GrantScope: ThisCallOnly},
	}

	gw := NewGateway(5*time.Second, "", mb)

	// Directly insert an expired grant into the internal map.
	past := time.Now().Add(-1 * time.Minute)
	gw.mu.Lock()
	gw.grants["expired_tool"] = Grant{
		Decision:  Approved,
		Scope:     For5Minutes,
		ToolName:  "expired_tool",
		ExpiresAt: &past,
		GrantedAt: past,
	}
	gw.mu.Unlock()

	prompt := newTestPrompt("expired_tool")
	grant, err := gw.RequestApproval(context.Background(), prompt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Backend should have been called because the cached grant was expired.
	if mb.getCallCount() != 1 {
		t.Errorf("expected backend called once (cache expired), got %d", mb.getCallCount())
	}
	if grant.Decision != Approved {
		t.Errorf("expected Approved, got %v", grant.Decision)
	}
}

// ---------------------------------------------------------------------------
// Test 8: TestPermanentAllow
// ---------------------------------------------------------------------------

func TestPermanentAllow(t *testing.T) {
	tmpDir := t.TempDir()
	permFile := tmpDir + "/perms.json"

	mb := &mockBackend{
		name: "mock", priority: 10, available: true,
		response: ApprovalResponse{Decision: Approved, GrantScope: Permanently},
	}

	gw1 := NewGateway(5*time.Second, permFile, mb)
	prompt1 := newTestPrompt("perm_tool")
	_, err := gw1.RequestApproval(context.Background(), prompt1)
	if err != nil {
		t.Fatalf("first gateway error: %v", err)
	}
	if mb.getCallCount() != 1 {
		t.Errorf("expected backend called once for first gateway, got %d", mb.getCallCount())
	}

	// Create a fresh gateway from the same permFile — no backend should be called.
	mb2 := &mockBackend{
		name: "mock2", priority: 10, available: true,
		response: ApprovalResponse{Decision: Approved, GrantScope: ThisCallOnly},
	}
	gw2 := NewGateway(5*time.Second, permFile, mb2)
	prompt2 := newTestPrompt("perm_tool")
	grant2, err := gw2.RequestApproval(context.Background(), prompt2)
	if err != nil {
		t.Fatalf("second gateway error: %v", err)
	}
	if mb2.getCallCount() != 0 {
		t.Errorf("expected backend NOT called for second gateway (permanent), got %d", mb2.getCallCount())
	}
	if grant2.Decision != Approved {
		t.Errorf("expected Approved from permanent store, got %v", grant2.Decision)
	}
}

// ---------------------------------------------------------------------------
// Test 9: TestConcurrentApprovals
// ---------------------------------------------------------------------------

func TestConcurrentApprovals(t *testing.T) {
	mb := &mockBackend{
		name: "mock", priority: 10, available: true,
		delay:    10 * time.Millisecond,
		response: ApprovalResponse{Decision: Approved, GrantScope: ThisCallOnly},
	}

	gw := NewGateway(5*time.Second, "", mb)

	var wg sync.WaitGroup
	results := make([]Grant, 3)
	errors := make([]error, 3)

	tools := []string{"tool_a", "tool_b", "tool_c"}
	for i, tool := range tools {
		wg.Add(1)
		go func(idx int, toolName string) {
			defer wg.Done()
			prompt := newTestPrompt(toolName)
			results[idx], errors[idx] = gw.RequestApproval(context.Background(), prompt)
		}(i, tool)
	}

	wg.Wait()

	for i := range tools {
		if errors[i] != nil {
			t.Errorf("tool %d error: %v", i, errors[i])
		}
		if results[i].Decision != Approved {
			t.Errorf("tool %d: expected Approved, got %v", i, results[i].Decision)
		}
	}
}

// ---------------------------------------------------------------------------
// Test 10: TestHTTPApprovalEndpoint
// ---------------------------------------------------------------------------

func TestHTTPApprovalEndpoint(t *testing.T) {
	// No backends available — the gateway will wait for HTTP resolution.
	gw := NewGateway(2*time.Second, "")

	prompt := newTestPrompt("http_tool")

	var grant Grant
	var gwErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		grant, gwErr = gw.RequestApproval(context.Background(), prompt)
	}()

	// Wait for the prompt to appear in PendingApprovals.
	var pending []ApprovalPrompt
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		pending = gw.PendingApprovals()
		if len(pending) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(pending) == 0 {
		t.Fatal("expected at least one pending approval")
	}

	// Resolve via RespondToApproval.
	err := gw.RespondToApproval(pending[0].ID, ApprovalResponse{
		Decision:   Approved,
		GrantScope: ForSession,
	})
	if err != nil {
		t.Fatalf("RespondToApproval error: %v", err)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("RequestApproval timed out after HTTP response")
	}

	if gwErr != nil {
		t.Fatalf("gateway error: %v", gwErr)
	}
	if grant.Decision != Approved {
		t.Errorf("expected Approved, got %v", grant.Decision)
	}
}

// ---------------------------------------------------------------------------
// Test 11: TestProxyFlowsDuringApproval
// ---------------------------------------------------------------------------

func TestProxyFlowsDuringApproval(t *testing.T) {
	// First backend: slow for tool_slow, fast for tool_fast.
	slowBackend := &mockBackend{
		name: "slow", priority: 10, available: true,
		delay:    200 * time.Millisecond,
		response: ApprovalResponse{Decision: Approved, GrantScope: ThisCallOnly},
	}

	gw := NewGateway(5*time.Second, "", slowBackend)

	var wg sync.WaitGroup
	results := make(map[string]Grant)
	errs := make(map[string]error)
	var mu sync.Mutex

	for _, tool := range []string{"tool_slow", "tool_fast"} {
		wg.Add(1)
		go func(toolName string) {
			defer wg.Done()
			prompt := newTestPrompt(toolName)
			g, err := gw.RequestApproval(context.Background(), prompt)
			mu.Lock()
			results[toolName] = g
			errs[toolName] = err
			mu.Unlock()
		}(tool)
	}

	wg.Wait()

	for _, tool := range []string{"tool_slow", "tool_fast"} {
		if errs[tool] != nil {
			t.Errorf("%s error: %v", tool, errs[tool])
		}
		if results[tool].Decision != Approved {
			t.Errorf("%s: expected Approved, got %v", tool, results[tool].Decision)
		}
	}

	// Both tools should have been approved independently.
	if slowBackend.getCallCount() != 2 {
		t.Errorf("expected backend called twice (once per tool), got %d", slowBackend.getCallCount())
	}
}
