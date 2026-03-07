// Package approval implements the multi-backend human-in-the-loop approval gateway.
// It supports terminal, WebSocket (dashboard), and future Tauri/mobile backends
// through a common Backend interface.
package approval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

// pendingEntry bundles the prompt and its response channel together.
type pendingEntry struct {
	prompt ApprovalPrompt
	ch     chan ApprovalResponse
}

// Gateway manages the approval flow across multiple backends.
type Gateway struct {
	mu       sync.Mutex
	timeout  time.Duration
	permFile string
	backends []Backend

	// grants holds cached approvals keyed by tool name.
	grants map[string]Grant

	// perms holds permanent approvals loaded from / saved to permFile.
	perms map[string]bool

	// pending holds active approval prompts awaiting a response.
	pending map[string]*pendingEntry
}

// NewGateway creates a Gateway with the given timeout, permanents file, and backends.
// Backends are sorted by Priority() ascending (lowest number = highest priority).
func NewGateway(timeout time.Duration, permFile string, backends ...Backend) *Gateway {
	sorted := make([]Backend, len(backends))
	copy(sorted, backends)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority() < sorted[j].Priority()
	})

	g := &Gateway{
		timeout:  timeout,
		permFile: permFile,
		backends: sorted,
		grants:   make(map[string]Grant),
		perms:    make(map[string]bool),
		pending:  make(map[string]*pendingEntry),
	}

	g.loadPerms()
	return g
}

// loadPerms reads the permanents file (JSON map[string]bool) into g.perms.
func (g *Gateway) loadPerms() {
	if g.permFile == "" {
		return
	}
	data, err := os.ReadFile(g.permFile) //nolint:gosec
	if err != nil {
		return // file may not exist yet
	}
	var m map[string]bool
	if err := json.Unmarshal(data, &m); err != nil {
		return
	}
	g.perms = m
}

// savePerms writes g.perms to permFile.
func (g *Gateway) savePerms() error {
	if g.permFile == "" {
		return nil
	}
	data, err := json.MarshalIndent(g.perms, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(g.permFile, data, 0o600)
}

// RequestApproval checks caches and then asks the best available backend.
// It returns a Grant representing the decision.
func (g *Gateway) RequestApproval(ctx context.Context, prompt ApprovalPrompt) (Grant, error) {
	toolKey := prompt.ToolName

	// 1. Check permanent approvals (loaded from permFile).
	g.mu.Lock()
	if g.perms[toolKey] {
		grant := Grant{
			Decision:  Approved,
			Scope:     Permanently,
			ToolName:  toolKey,
			GrantedAt: time.Now(),
		}
		g.mu.Unlock()
		return grant, nil
	}

	// 2. Check session/timed grants.
	if cached, ok := g.grants[toolKey]; ok {
		if cached.ExpiresAt == nil || cached.ExpiresAt.After(time.Now()) {
			g.mu.Unlock()
			return cached, nil
		}
		// Expired — remove it.
		delete(g.grants, toolKey)
	}

	// 3. Register a pending entry for this prompt (HTTP endpoint resolution).
	httpRespCh := make(chan ApprovalResponse, 1)
	entry := &pendingEntry{prompt: prompt, ch: httpRespCh}
	g.pending[prompt.ID] = entry
	g.mu.Unlock()

	defer func() {
		g.mu.Lock()
		delete(g.pending, prompt.ID)
		g.mu.Unlock()
	}()

	// 4. Find the first available backend (already sorted by priority).
	g.mu.Lock()
	var chosenBackend Backend
	for _, b := range g.backends {
		if b.Available() {
			chosenBackend = b
			break
		}
	}
	g.mu.Unlock()

	// Build a context that respects the gateway timeout.
	timeoutCtx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	var resp ApprovalResponse

	if chosenBackend != nil {
		// 5. Call the chosen backend; race it against HTTP channel and timeout.
		backendRespCh := make(chan ApprovalResponse, 1)
		backendErrCh := make(chan error, 1)

		go func() {
			r, err := chosenBackend.Prompt(timeoutCtx, prompt)
			if err != nil {
				backendErrCh <- err
				return
			}
			backendRespCh <- r
		}()

		select {
		case r := <-backendRespCh:
			resp = r
		case r := <-httpRespCh:
			resp = r
		case err := <-backendErrCh:
			return Grant{}, err
		case <-timeoutCtx.Done():
			resp = ApprovalResponse{Decision: Timeout}
		}
	} else {
		// No backend available — wait on HTTP channel or timeout.
		select {
		case r := <-httpRespCh:
			resp = r
		case <-timeoutCtx.Done():
			resp = ApprovalResponse{Decision: Timeout}
		}
	}

	// 6. Build the grant.
	grant := Grant{
		Decision:  resp.Decision,
		Scope:     resp.GrantScope,
		ToolName:  toolKey,
		GrantedAt: time.Now(),
	}

	// 7. Cache the grant based on scope if approved.
	if resp.Decision == Approved {
		switch resp.GrantScope {
		case For5Minutes:
			t := time.Now().Add(5 * time.Minute)
			grant.ExpiresAt = &t
			g.mu.Lock()
			g.grants[toolKey] = grant
			g.mu.Unlock()
		case For30Minutes:
			t := time.Now().Add(30 * time.Minute)
			grant.ExpiresAt = &t
			g.mu.Lock()
			g.grants[toolKey] = grant
			g.mu.Unlock()
		case ForSession:
			g.mu.Lock()
			g.grants[toolKey] = grant
			g.mu.Unlock()
		case Permanently:
			g.mu.Lock()
			g.grants[toolKey] = grant
			g.perms[toolKey] = true
			if err := g.savePerms(); err != nil {
				g.mu.Unlock()
				return grant, fmt.Errorf("save perms: %w", err)
			}
			g.mu.Unlock()
		default:
			// ThisCallOnly — no caching.
		}
	}

	return grant, nil
}

// PendingApprovals returns the currently pending approval prompts.
func (g *Gateway) PendingApprovals() []ApprovalPrompt {
	g.mu.Lock()
	defer g.mu.Unlock()

	prompts := make([]ApprovalPrompt, 0, len(g.pending))
	for _, e := range g.pending {
		prompts = append(prompts, e.prompt)
	}
	return prompts
}

// RespondToApproval resolves a pending approval by ID (called by HTTP endpoint).
func (g *Gateway) RespondToApproval(id string, resp ApprovalResponse) error {
	g.mu.Lock()
	entry, ok := g.pending[id]
	g.mu.Unlock()
	if !ok {
		return fmt.Errorf("no pending approval with id %q", id)
	}
	select {
	case entry.ch <- resp:
	default:
		return fmt.Errorf("approval %q already resolved", id)
	}
	return nil
}
