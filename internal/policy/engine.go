// Copyright 2026 Abid Ali Khan. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

// Package policy implements the OPA-based policy evaluation engine.
// It loads Rego policies and evaluates each MCP tool call to produce
// an allow/deny/approve decision.
package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/open-policy-agent/opa/rego"
)

// Decision represents the policy engine's verdict.
type Decision string

const (
	Allow   Decision = "allow"
	Deny    Decision = "deny"
	Approve Decision = "approve" // requires human approval
)

// EvalResult contains the policy decision and metadata.
type EvalResult struct {
	Decision Decision
	Reason   string // human-readable explanation
	Rule     string // which Rego rule triggered the decision
}

// EvalInput is the data passed to OPA for evaluation.
type EvalInput struct {
	Method           string          `json:"method"`
	ToolName         string          `json:"tool_name"`
	Arguments        json.RawMessage `json:"arguments"`
	ArgumentKeys     []string        `json:"argument_keys"`
	Direction        string          `json:"direction"`
	ToolChanged      bool            `json:"tool_changed"`
	ToolAcknowledged bool            `json:"tool_acknowledged"`
	SessionID        string          `json:"session_id"`
	ServerCommand    string          `json:"server_command"`
	Timestamp        string          `json:"timestamp"`
}

// Engine wraps the OPA evaluator with a prepared query.
type Engine struct {
	mu    sync.RWMutex
	query rego.PreparedEvalQuery
}

// NewEngine loads all .rego files from policyDir (which may be empty or absent)
// and prepares the OPA evaluator. Returns an error if any policy file is invalid.
func NewEngine(policyDir string) (*Engine, error) {
	e := &Engine{}
	if err := e.Reload(policyDir); err != nil {
		return nil, err
	}
	return e, nil
}

// Evaluate runs the policy against the given input.
// Returns EvalResult with a default of Allow if no rules match.
func (e *Engine) Evaluate(input EvalInput) (EvalResult, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Convert EvalInput to a plain map so OPA can consume it.
	inputMap, err := toMap(input)
	if err != nil {
		return EvalResult{Decision: Allow}, fmt.Errorf("policy: marshal input: %w", err)
	}

	rs, err := e.query.Eval(context.Background(), rego.EvalInput(inputMap))
	if err != nil {
		return EvalResult{Decision: Allow}, fmt.Errorf("policy: evaluate: %w", err)
	}

	return parseResult(rs)
}

// Reload reloads policies from policyDir. Safe to call concurrently.
func (e *Engine) Reload(policyDir string) error {
	modules := loadModules(policyDir)

	opts := []func(*rego.Rego){
		rego.Query("data.mantismo.decision"),
	}
	for name, src := range modules {
		opts = append(opts, rego.Module(name, src))
	}

	q, err := rego.New(opts...).PrepareForEval(context.Background())
	if err != nil {
		return fmt.Errorf("policy: prepare query: %w", err)
	}

	e.mu.Lock()
	e.query = q
	e.mu.Unlock()
	return nil
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// loadModules returns a map of filename → Rego source for all .rego files in
// policyDir. Missing or empty directories are silently ignored.
func loadModules(policyDir string) map[string]string {
	modules := make(map[string]string)
	if policyDir == "" {
		return modules
	}
	entries, err := os.ReadDir(policyDir)
	if err != nil {
		return modules
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".rego") {
			continue
		}
		path := filepath.Join(policyDir, e.Name())
		src, err := os.ReadFile(path) //nolint:gosec
		if err != nil {
			continue
		}
		modules[e.Name()] = string(src)
	}
	return modules
}

// toMap serialises v to JSON and back to a plain map (the format OPA expects).
func toMap(v any) (map[string]any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// opaDecision is the shape OPA returns for data.mantismo.decision.
type opaDecision struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	Rule     string `json:"rule"`
}

// parseResult converts an OPA result set into an EvalResult.
// Falls back to Allow if the result set is empty or the value cannot be parsed.
func parseResult(rs rego.ResultSet) (EvalResult, error) {
	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		return EvalResult{Decision: Allow, Reason: "no policy matched", Rule: "default_allow"}, nil
	}

	val := rs[0].Expressions[0].Value
	b, err := json.Marshal(val)
	if err != nil {
		return EvalResult{Decision: Allow}, nil
	}

	var d opaDecision
	if err := json.Unmarshal(b, &d); err != nil {
		return EvalResult{Decision: Allow}, nil
	}

	dec := Decision(d.Decision)
	switch dec {
	case Allow, Deny, Approve:
	default:
		dec = Allow
	}

	return EvalResult{
		Decision: dec,
		Reason:   d.Reason,
		Rule:     d.Rule,
	}, nil
}
