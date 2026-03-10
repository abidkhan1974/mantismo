// Copyright 2026 Abid Ali Khan. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

// Package vaulttools implements MCP tool handlers that expose read-only access
// to the encrypted vault. These tools are injected into tools/list responses
// and handled locally without forwarding to the upstream MCP server.
package vaulttools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/abidkhan1974/mantismo/internal/approval"
	"github.com/abidkhan1974/mantismo/internal/interceptor"
	"github.com/abidkhan1974/mantismo/internal/vault"
)

// TrustLevel controls how much vault data can be exposed to a caller.
type TrustLevel int

const (
	// Untrusted callers receive only public entries.
	Untrusted TrustLevel = iota
	// Standard callers receive public and standard entries.
	Standard
	// Trusted callers receive public, standard, and sensitive entries.
	Trusted
	// Full callers receive all entries; critical entries still need approval.
	Full
)

// Handler routes MCP tool calls that target the vault.
type Handler struct {
	vault      *vault.Vault
	approval   *approval.Gateway
	trustLevel TrustLevel
}

// NewHandler creates a new Handler with the given vault, approval gateway, and
// trust level. The vault may be nil if it has not been initialised yet.
func NewHandler(v *vault.Vault, ag *approval.Gateway, trustLevel TrustLevel) *Handler {
	return &Handler{
		vault:      v,
		approval:   ag,
		trustLevel: trustLevel,
	}
}

// IsVaultTool returns true if the tool name belongs to the vault tool set.
func IsVaultTool(name string) bool {
	return strings.HasPrefix(name, "vault_")
}

// ToolDefinitions returns the MCP tool descriptors for all five vault tools.
func (h *Handler) ToolDefinitions() []interceptor.ToolInfo {
	mustJSON := func(v interface{}) json.RawMessage {
		b, _ := json.Marshal(v)
		return b
	}

	return []interceptor.ToolInfo{
		{
			Name:        "vault_get_profile",
			Description: "Get profile information from the personal vault",
			InputSchema: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"fields": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "List of field names to retrieve (empty=all)",
					},
				},
			}),
		},
		{
			Name:        "vault_get_preferences",
			Description: "Get preferences from the personal vault",
			InputSchema: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"domain": map[string]interface{}{
						"type":        "string",
						"description": "Filter by domain prefix (e.g. 'editor', 'notifications')",
					},
				},
			}),
		},
		{
			Name:        "vault_search_docs",
			Description: "Search documents in the personal vault",
			InputSchema: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{
						"type":        "string",
						"description": "Search query",
					},
					"max_results": map[string]interface{}{
						"type":        "integer",
						"description": "Maximum number of results (default 10)",
						"default":     10,
					},
				},
				"required": []string{"query"},
			}),
		},
		{
			Name:        "vault_get_masked_id",
			Description: "Get masked identity information (SSN, passport, etc.) from the vault",
			InputSchema: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"fields": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Specific identifier fields to retrieve (empty=all)",
					},
				},
			}),
		},
		{
			Name:        "vault_list_categories",
			Description: "List vault categories and entry counts (no data exposed)",
			InputSchema: mustJSON(map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			}),
		},
	}
}

// HandleToolCall dispatches a vault tool call and returns the JSON result.
func (h *Handler) HandleToolCall(req interceptor.ToolCallRequest) (json.RawMessage, error) {
	if h.vault == nil {
		return errorResult("vault is locked; run 'mantismo vault init' to set up your vault"), nil
	}

	var args map[string]interface{}
	if len(req.Arguments) > 0 {
		if err := json.Unmarshal(req.Arguments, &args); err != nil {
			return errorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
		}
	}
	if args == nil {
		args = make(map[string]interface{})
	}

	switch req.ToolName {
	case "vault_get_profile":
		return h.handleGetProfile(args)
	case "vault_get_preferences":
		return h.handleGetPreferences(args)
	case "vault_search_docs":
		return h.handleSearchDocs(args)
	case "vault_get_masked_id":
		return h.handleGetMaskedID(args)
	case "vault_list_categories":
		return h.handleListCategories()
	default:
		return errorResult(fmt.Sprintf("unknown vault tool: %s", req.ToolName)), nil
	}
}

// maxSensForTrust maps the handler's trust level to a vault.Sensitivity ceiling.
func (h *Handler) maxSensForTrust() vault.Sensitivity {
	switch h.trustLevel {
	case Untrusted:
		return vault.Public
	case Standard:
		return vault.Standard
	case Trusted, Full:
		return vault.Sensitive
	default:
		return vault.Standard
	}
}

// handleGetProfile returns profile entries as a JSON object {key: value}.
func (h *Handler) handleGetProfile(args map[string]interface{}) (json.RawMessage, error) {
	maxSens := h.maxSensForTrust()
	cat := vault.Profile
	entries, err := h.vault.List(&cat, &maxSens)
	if err != nil {
		return errorResult(fmt.Sprintf("vault error: %v", err)), nil
	}

	// Optional field filter.
	wanted := stringSliceArg(args, "fields")
	wantedSet := make(map[string]bool, len(wanted))
	for _, f := range wanted {
		wantedSet[f] = true
	}

	result := make(map[string]string, len(entries))
	for _, e := range entries {
		if len(wantedSet) > 0 && !wantedSet[e.Key] {
			continue
		}
		result[e.Key] = e.Value
	}
	return mustMarshal(result), nil
}

// handleGetPreferences returns preferences entries as a JSON object {key: value}.
func (h *Handler) handleGetPreferences(args map[string]interface{}) (json.RawMessage, error) {
	maxSens := h.maxSensForTrust()
	cat := vault.Preferences
	entries, err := h.vault.List(&cat, &maxSens)
	if err != nil {
		return errorResult(fmt.Sprintf("vault error: %v", err)), nil
	}

	domain, _ := args["domain"].(string)

	result := make(map[string]string, len(entries))
	for _, e := range entries {
		if domain != "" && !strings.HasPrefix(e.Key, domain+".") {
			continue
		}
		result[e.Key] = e.Value
	}
	return mustMarshal(result), nil
}

// handleSearchDocs searches vault documents and returns snippets.
func (h *Handler) handleSearchDocs(args map[string]interface{}) (json.RawMessage, error) {
	query, _ := args["query"].(string)
	if query == "" {
		return errorResult("query is required"), nil
	}

	maxResults := 10
	if v, ok := args["max_results"]; ok {
		switch n := v.(type) {
		case float64:
			maxResults = int(n)
		case int:
			maxResults = n
		}
	}

	maxSens := h.maxSensForTrust()
	results, err := h.vault.Search(query, &maxSens)
	if err != nil {
		return errorResult(fmt.Sprintf("vault error: %v", err)), nil
	}

	// Filter to documents category only.
	type snippet struct {
		Key     string `json:"key"`
		Label   string `json:"label"`
		Snippet string `json:"snippet"`
	}
	var out []snippet
	for _, e := range results {
		if e.Category != vault.Documents {
			continue
		}
		val := e.Value
		if len(val) > 500 {
			val = val[:500]
		}
		out = append(out, snippet{Key: e.Key, Label: e.Label, Snippet: val})
		if len(out) >= maxResults {
			break
		}
	}
	if out == nil {
		out = []snippet{}
	}
	return mustMarshal(out), nil
}

// handleGetMaskedID returns identifier entries with values masked.
func (h *Handler) handleGetMaskedID(args map[string]interface{}) (json.RawMessage, error) {
	maxSens := vault.Sensitive // always allow up to sensitive without approval
	cat := vault.Identifiers
	entries, err := h.vault.List(&cat, &maxSens)
	if err != nil {
		return errorResult(fmt.Sprintf("vault error: %v", err)), nil
	}

	// Optional field filter.
	wanted := stringSliceArg(args, "fields")
	wantedSet := make(map[string]bool, len(wanted))
	for _, f := range wanted {
		wantedSet[f] = true
	}

	result := make(map[string]string, len(entries))
	for _, e := range entries {
		if len(wantedSet) > 0 && !wantedSet[e.Key] {
			continue
		}
		result[e.Key] = maskValue(e.Key, e.Value)
	}

	// Also handle critical entries if gateway is available and trust is Full.
	if h.trustLevel == Full && h.approval != nil {
		allCat := vault.Identifiers
		critSens := vault.Critical
		all, listErr := h.vault.List(&allCat, &critSens)
		if listErr == nil {
			for _, e := range all {
				if e.Sensitivity != vault.Critical {
					continue
				}
				if len(wantedSet) > 0 && !wantedSet[e.Key] {
					continue
				}
				promptID := uuid.New().String()
				prompt := approval.ApprovalPrompt{
					ID:        promptID,
					ToolName:  "vault_get_masked_id",
					Reason:    fmt.Sprintf("Access critical identifier: %s", e.Key),
					RiskLevel: "high",
					CreatedAt: time.Now(),
					ExpiresAt: time.Now().Add(30 * time.Second),
				}
				grant, grantErr := h.approval.RequestApproval(context.Background(), prompt)
				if grantErr == nil && grant.Decision == approval.Approved {
					result[e.Key] = maskValue(e.Key, e.Value)
				}
			}
		}
	}

	return mustMarshal(result), nil
}

// handleListCategories returns a map of category → entry count.
func (h *Handler) handleListCategories() (json.RawMessage, error) {
	stats, err := h.vault.Stats()
	if err != nil {
		return errorResult(fmt.Sprintf("vault error: %v", err)), nil
	}
	result := make(map[string]int, len(stats.ByCategory))
	for cat, cnt := range stats.ByCategory {
		if cnt > 0 {
			result[string(cat)] = cnt
		}
	}
	return mustMarshal(result), nil
}

// maskValue masks a value for safe display.
func maskValue(key, value string) string {
	lower := strings.ToLower(key)

	// SSN / social security.
	if strings.Contains(lower, "ssn") || strings.Contains(lower, "social") {
		// Show last 4 digits: "***-**-XXXX"
		cleaned := strings.ReplaceAll(value, "-", "")
		if len(cleaned) >= 4 {
			return "***-**-" + cleaned[len(cleaned)-4:]
		}
		return "***-**-****"
	}

	// Phone number.
	if strings.Contains(lower, "phone") {
		cleaned := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, value)
		if len(cleaned) >= 4 {
			return "***-***-" + cleaned[len(cleaned)-4:]
		}
		return "***-***-****"
	}

	// Email.
	if strings.Contains(lower, "email") {
		atIdx := strings.Index(value, "@")
		if atIdx > 0 {
			return string(value[0]) + "***" + "@" + value[atIdx+1:]
		}
	}

	// Generic: first char + **** + last char.
	runes := []rune(value)
	if len(runes) <= 4 {
		return "****"
	}
	return string(runes[0]) + "****" + string(runes[len(runes)-1])
}

// stringSliceArg extracts a []string from args[key] (handles JSON array of strings).
func stringSliceArg(args map[string]interface{}, key string) []string {
	v, ok := args[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// errorResult returns a JSON object with an "error" key.
func errorResult(msg string) json.RawMessage {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return b
}

// mustMarshal marshals v to JSON, panicking on error (only used for in-process structs).
func mustMarshal(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("vaulttools: marshal: %v", err))
	}
	return b
}
