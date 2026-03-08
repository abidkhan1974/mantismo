// Copyright 2026 Mantismo. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

// Package api implements the internal REST + WebSocket API server.
// This is the central nervous system: every feature Mantismo offers is exposed
// through this server. The CLI, web dashboard, and future Tauri/mobile apps
// are all thin clients that call this API.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/abidkhan1974/mantismo/internal/dashboard"
	"github.com/abidkhan1974/mantismo/internal/fingerprint"
	"github.com/abidkhan1974/mantismo/internal/logger"
	"github.com/gorilla/websocket"
)

// ApprovalRequest represents a pending approval for the gateway.
type ApprovalRequest struct {
	ID       string          `json:"id"`
	Method   string          `json:"method"`
	ToolName string          `json:"tool_name,omitempty"`
	Args     json.RawMessage `json:"args,omitempty"`
}

// ApprovalResponse is the user's answer to an approval request.
type ApprovalResponse struct {
	ID      string `json:"id"`
	Allowed bool   `json:"allowed"`
}

// SessionInfo tracks a single proxy session.
type SessionInfo struct {
	ID        string     `json:"id"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	ServerCmd string     `json:"server_command"`
	ToolCalls int        `json:"tool_calls"`
	Blocked   int        `json:"blocked"`
	Approved  int        `json:"approved"`
}

// SessionStore tracks active and past sessions.
type SessionStore struct {
	mu      sync.RWMutex
	active  *SessionInfo
	history []SessionInfo
}

// NewSessionStore creates a new, empty SessionStore.
func NewSessionStore() *SessionStore { return &SessionStore{} }

// SetActive replaces the current active session.
func (s *SessionStore) SetActive(info *SessionInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = info
}

// EndActive moves the current active session to history and clears it.
func (s *SessionStore) EndActive() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil {
		now := time.Now().UTC()
		s.active.EndedAt = &now
		s.history = append(s.history, *s.active)
		s.active = nil
	}
}

// Active returns a copy of the current active session, or nil.
func (s *SessionStore) Active() *SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.active == nil {
		return nil
	}
	cp := *s.active
	return &cp
}

// All returns all sessions (active first, then history).
func (s *SessionStore) All() []SessionInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var all []SessionInfo
	if s.active != nil {
		all = append(all, *s.active)
	}
	all = append(all, s.history...)
	return all
}

// Config holds API server configuration.
type Config struct {
	Port     int
	BindAddr string
}

// Dependencies groups the backend components the API server exposes.
type Dependencies struct {
	Logger       *logger.Logger
	LogDir       string
	Fingerprints *fingerprint.Store
	ApprovalCh   chan ApprovalRequest
	Sessions     *SessionStore
}

// Server is the internal REST + WebSocket API server.
type Server struct {
	cfg      Config
	deps     Dependencies
	srv      *http.Server
	upgrader websocket.Upgrader

	addr string // actual bound address (set by Start)

	// Live log streaming.
	subsMu sync.Mutex
	subs   map[chan logger.LogEntry]struct{}
}

// NewServer creates a new API Server.
func NewServer(cfg Config, deps Dependencies) *Server {
	s := &Server{
		cfg:  cfg,
		deps: deps,
		subs: make(map[chan logger.LogEntry]struct{}),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
	return s
}

// PublishLog broadcasts a log entry to all live-stream WebSocket subscribers.
func (s *Server) PublishLog(entry logger.LogEntry) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- entry:
		default: // skip slow consumers
		}
	}
}

func (s *Server) subscribe() chan logger.LogEntry {
	ch := make(chan logger.LogEntry, 64)
	s.subsMu.Lock()
	s.subs[ch] = struct{}{}
	s.subsMu.Unlock()
	return ch
}

func (s *Server) unsubscribe(ch chan logger.LogEntry) {
	s.subsMu.Lock()
	delete(s.subs, ch)
	s.subsMu.Unlock()
	// Drain to unblock any waiting sender.
	for len(ch) > 0 {
		<-ch
	}
}

// Start begins serving. It binds the port and returns; the server runs in background goroutines.
// When cfg.Port == 0 the OS assigns a free port; use Addr() to discover it.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	// Dashboard SPA — must be registered before specific routes so /api/* takes precedence.
	mux.Handle("/", dashboard.Handler())
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/tools", s.handleTools)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/policy", s.handlePolicy)
	mux.HandleFunc("/api/vault/stats", s.handleVaultStats)
	mux.HandleFunc("/api/approval/", s.handleApprovalRespond)
	mux.HandleFunc("/api/ws/logs", s.handleWSLogs)
	mux.HandleFunc("/api/ws/approvals", s.handleWSApprovals)

	bindAddr := s.cfg.BindAddr
	if bindAddr == "" {
		bindAddr = "127.0.0.1"
	}
	addr := fmt.Sprintf("%s:%d", bindAddr, s.cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("api: listen %s: %w", addr, err)
	}
	s.addr = ln.Addr().String()

	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 30 * time.Second} //nolint:gosec
	go func() {
		_ = s.srv.Serve(ln)
	}()
	go func() {
		<-ctx.Done()
		_ = s.srv.Shutdown(context.Background())
	}()
	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

// Addr returns the actual bound address (e.g., "127.0.0.1:7777").
// Only valid after Start has been called.
func (s *Server) Addr() string {
	return s.addr
}

// Port returns the actual bound port number. Only valid after Start.
func (s *Server) Port() int {
	if s.addr == "" {
		return 0
	}
	_, portStr, err := net.SplitHostPort(s.addr)
	if err != nil {
		return 0
	}
	port, _ := strconv.Atoi(portStr)
	return port
}

// ── REST handlers ─────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"proxy":   s.deps.Sessions != nil && s.deps.Sessions.Active() != nil,
		"version": "0.1.0",
	})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	filter := logger.QueryFilter{
		SessionID: q.Get("session"),
		Method:    q.Get("method"),
		ToolName:  q.Get("tool"),
		Decision:  q.Get("decision"),
	}
	if lim := q.Get("limit"); lim != "" {
		if n, err := strconv.Atoi(lim); err == nil {
			filter.Limit = n
		}
	}
	if since := q.Get("since"); since != "" {
		if t, err := parseDuration(since); err == nil {
			filter.Since = &t
		}
	}
	if until := q.Get("until"); until != "" {
		if t, err := time.Parse(time.RFC3339, until); err == nil {
			filter.Until = &t
		}
	}

	dir := s.deps.LogDir
	entries, err := logger.Query(dir, filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []logger.LogEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var tools []map[string]any
	if s.deps.Fingerprints != nil {
		for name, fp := range s.deps.Fingerprints.All() {
			tools = append(tools, map[string]any{
				"name":         name,
				"hash":         fp.Hash,
				"first_seen":   fp.FirstSeen,
				"last_seen":    fp.LastSeen,
				"server_cmd":   fp.ServerCmd,
				"acknowledged": fp.Acknowledged,
				"changed":      s.deps.Fingerprints.IsToolChanged(name),
			})
		}
	}
	if tools == nil {
		tools = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, tools)
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Query today's entries for basic stats.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	entries, _ := logger.Query(s.deps.LogDir, logger.QueryFilter{Since: &today})
	toolCalls, blocked := 0, 0
	for _, e := range entries {
		if e.Method == "tools/call" && e.MessageType == "request" {
			toolCalls++
		}
		if e.PolicyDecision == "deny" {
			blocked++
		}
	}
	var activeSession *SessionInfo
	if s.deps.Sessions != nil {
		activeSession = s.deps.Sessions.Active()
	}
	sessionsCount := 0
	if s.deps.Sessions != nil {
		sessionsCount = len(s.deps.Sessions.All())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tool_calls_today": toolCalls,
		"blocked_today":    blocked,
		"sessions_today":   sessionsCount,
		"active_session":   activeSession,
	})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var sessions []SessionInfo
	if s.deps.Sessions != nil {
		sessions = s.deps.Sessions.All()
	}
	if sessions == nil {
		sessions = []SessionInfo{}
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handlePolicy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"preset": "balanced",
		"rules":  []string{},
	})
}

func (s *Server) handleVaultStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": false,
		"entries": 0,
	})
}

func (s *Server) handleApprovalRespond(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ── WebSocket handlers ────────────────────────────────────────────────────────

func (s *Server) handleWSLogs(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ch := s.subscribe()
	defer s.unsubscribe(ch)

	done := make(chan struct{})
	go func() {
		// Read from client so we detect disconnects.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				close(done)
				return
			}
		}
	}()

	for {
		select {
		case <-done:
			return
		case entry := <-ch:
			b, _ := json.Marshal(entry)
			if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
				return
			}
		}
	}
}

func (s *Server) handleWSApprovals(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	approvalCh := s.deps.ApprovalCh
	if approvalCh == nil {
		return
	}

	done := make(chan struct{})
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				close(done)
				return
			}
			var resp ApprovalResponse
			if err := json.Unmarshal(msg, &resp); err == nil {
				// Response handling will be wired to approval gateway in spec 11.
				_ = resp
			}
		}
	}()

	for {
		select {
		case <-done:
			return
		case req := <-approvalCh:
			b, _ := json.Marshal(req)
			if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
				return
			}
		}
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// parseDuration parses a duration string like "1h", "24h", an RFC3339 timestamp,
// a date-only "2006-01-02", or the keyword "today".
func parseDuration(s string) (time.Time, error) {
	// Try RFC3339 first.
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	// Try date-only.
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	// Try "today".
	if strings.ToLower(s) == "today" {
		return time.Now().UTC().Truncate(24 * time.Hour), nil
	}
	// Try Go duration (e.g., "1h", "2h30m").
	d, err := time.ParseDuration(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot parse time %q", s)
	}
	return time.Now().UTC().Add(-d), nil
}
