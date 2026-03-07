// Package approval — backend.go defines the ApprovalBackend interface.
package approval

import (
	"context"
	"time"
)

// Backend defines the interface all approval UIs must implement.
type Backend interface {
	Name() string
	Available() bool
	Prompt(ctx context.Context, req ApprovalPrompt) (ApprovalResponse, error)
	Priority() int
}

// ApprovalPrompt contains the information shown to the user.
type ApprovalPrompt struct {
	ID        string    `json:"id"`
	ToolName  string    `json:"tool_name"`
	ServerCmd string    `json:"server_cmd"`
	Reason    string    `json:"reason"`
	Arguments string    `json:"arguments"`
	RiskLevel string    `json:"risk_level"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// ApprovalResponse is the user's decision.
type ApprovalResponse struct {
	Decision   GrantDecision `json:"decision"`
	GrantScope GrantScope    `json:"grant_scope"`
}

// GrantDecision represents the outcome of an approval prompt.
type GrantDecision string

const (
	// Approved means the user allowed the action.
	Approved GrantDecision = "approved"
	// Denied means the user denied the action.
	Denied GrantDecision = "denied"
	// Timeout means no decision was made before the deadline.
	Timeout GrantDecision = "timeout"
)

// GrantScope controls how long an approval is cached.
type GrantScope string

const (
	// ThisCallOnly grants approval for one invocation only.
	ThisCallOnly GrantScope = "this_call"
	// For5Minutes caches the approval for five minutes.
	For5Minutes GrantScope = "5_minutes"
	// For30Minutes caches the approval for thirty minutes.
	For30Minutes GrantScope = "30_minutes"
	// ForSession caches the approval for the lifetime of the process.
	ForSession GrantScope = "session"
	// Permanently persists the approval to disk.
	Permanently GrantScope = "permanent"
)

// Grant represents a cached approval.
type Grant struct {
	Decision  GrantDecision
	Scope     GrantScope
	ToolName  string
	ExpiresAt *time.Time // nil for session and permanent grants
	GrantedAt time.Time
}
