// Copyright 2026 Abid Ali Khan. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

// Package approval — backend_websocket.go implements the WebSocket approval backend.
// It pushes approval requests to connected dashboard or Tauri clients.
package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
)

// wsRequest is the envelope sent to dashboard clients.
type wsRequest struct {
	Type string         `json:"type"`
	Data ApprovalPrompt `json:"data"`
}

// wsResponse is the envelope received from dashboard clients.
type wsResponse struct {
	Type string         `json:"type"`
	Data wsResponseData `json:"data"`
}

// wsResponseData carries the client's decision.
type wsResponseData struct {
	ID         string        `json:"id"`
	Decision   GrantDecision `json:"decision"`
	GrantScope GrantScope    `json:"grant_scope"`
}

// WebSocketBackend delivers approval prompts to connected WebSocket clients.
type WebSocketBackend struct {
	mu      sync.Mutex
	clients map[string]*websocket.Conn       // clientID → conn
	pending map[string]chan ApprovalResponse // promptID → response channel
}

// NewWebSocketBackend creates an empty WebSocketBackend.
func NewWebSocketBackend() *WebSocketBackend {
	return &WebSocketBackend{
		clients: make(map[string]*websocket.Conn),
		pending: make(map[string]chan ApprovalResponse),
	}
}

// Name returns the backend identifier.
func (w *WebSocketBackend) Name() string { return "websocket" }

// Priority returns 10 — WebSocket is the high-priority preferred backend.
func (w *WebSocketBackend) Priority() int { return 10 }

// Available returns true when at least one client is connected.
func (w *WebSocketBackend) Available() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.clients) > 0
}

// Prompt broadcasts the approval request to all connected clients and waits for
// the first response (or ctx cancellation).
func (w *WebSocketBackend) Prompt(ctx context.Context, req ApprovalPrompt) (ApprovalResponse, error) {
	// Register a response channel for this prompt.
	respCh := make(chan ApprovalResponse, 1)
	w.mu.Lock()
	w.pending[req.ID] = respCh

	// Snapshot the current clients so we can broadcast outside the lock.
	conns := make([]*websocket.Conn, 0, len(w.clients))
	for _, c := range w.clients {
		conns = append(conns, c)
	}
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		delete(w.pending, req.ID)
		w.mu.Unlock()
	}()

	msg, err := json.Marshal(wsRequest{Type: "approval_request", Data: req})
	if err != nil {
		return ApprovalResponse{Decision: Denied}, fmt.Errorf("marshal approval request: %w", err)
	}

	// Best-effort broadcast; ignore individual send errors.
	for _, conn := range conns {
		_ = conn.WriteMessage(websocket.TextMessage, msg)
	}

	select {
	case resp := <-respCh:
		return resp, nil
	case <-ctx.Done():
		return ApprovalResponse{Decision: Denied}, nil
	}
}

// HandleConnection is called by the API server when a client connects to
// /api/ws/approvals. It registers the connection, reads response messages in a
// loop, and deregisters on disconnect.
func (w *WebSocketBackend) HandleConnection(conn *websocket.Conn) {
	// Use the remote address as a unique client ID.
	clientID := conn.RemoteAddr().String()

	w.mu.Lock()
	w.clients[clientID] = conn
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		delete(w.clients, clientID)
		w.mu.Unlock()
		conn.Close() //nolint:errcheck
	}()

	for {
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			// Client disconnected or error — stop reading.
			return
		}

		var env wsResponse
		if err := json.Unmarshal(msgBytes, &env); err != nil {
			continue
		}
		if env.Type != "approval_response" {
			continue
		}

		w.mu.Lock()
		ch, ok := w.pending[env.Data.ID]
		w.mu.Unlock()

		if !ok {
			continue
		}

		resp := ApprovalResponse{
			Decision:   env.Data.Decision,
			GrantScope: env.Data.GrantScope,
		}

		select {
		case ch <- resp:
		default:
			// Already resolved.
		}
	}
}
