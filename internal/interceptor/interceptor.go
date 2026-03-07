// Package interceptor provides MCP-aware message routing and augmentation,
// sitting between the proxy transport layer and business logic (policy, logging, vault).
package interceptor

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/inferalabs/mantismo/internal/proxy"
)

// Interceptor implements proxy.MessageHandler using MCP-aware routing.
type Interceptor struct {
	hooks           Hooks
	pendingRequests sync.Map // key: string(id raw JSON) → value: MCPMessage
}

// New creates a new Interceptor with the given hooks.
// Hooks may be zero-valued; nil fields fall back to default (passthrough or block).
func New(hooks Hooks) *Interceptor {
	return &Interceptor{hooks: hooks}
}

// Handle implements proxy.MessageHandler.
// It parses the message, routes it through the appropriate hook, fires OnAnyMessage,
// and converts the InterceptResult into the (forward, error) return values the proxy expects.
func (i *Interceptor) Handle(msg json.RawMessage, dir proxy.Direction) (json.RawMessage, error) {
	parsed := parseMessage(msg)

	// Track pending ToServer requests so we can correlate their responses.
	if dir == proxy.ToServer && parsed.IsRequest {
		i.pendingRequests.Store(idKey(parsed.ID), parsed)
	}

	result := i.route(parsed, dir)

	// Fire the universal logging hook after all processing (read-only).
	if i.hooks.OnAnyMessage != nil {
		i.hooks.OnAnyMessage(parsed, dir)
	}

	switch result.Action {
	case Forward:
		return msg, nil
	case Modify:
		return result.Modified, nil
	case Block:
		if result.Error != nil {
			return nil, result.Error
		}
		return nil, fmt.Errorf("blocked by mantismo")
	case Consume:
		// Message was handled natively; do not forward to the destination.
		return nil, nil
	default:
		return msg, nil
	}
}

// ── Internal routing ─────────────────────────────────────────────────────────

func (i *Interceptor) route(msg MCPMessage, dir proxy.Direction) InterceptResult {
	switch dir {
	case proxy.ToServer:
		return i.routeToServer(msg)
	case proxy.FromServer:
		return i.routeFromServer(msg)
	}
	return InterceptResult{Action: Forward}
}

// routeToServer handles messages going from the agent host to the MCP server.
func (i *Interceptor) routeToServer(msg MCPMessage) InterceptResult {
	if !msg.IsRequest {
		// Notifications (e.g., notifications/initialized) pass through unchanged.
		return InterceptResult{Action: Forward}
	}

	switch msg.Method {
	case "initialize":
		if i.hooks.OnInitialize != nil {
			return i.hooks.OnInitialize(msg.Raw)
		}

	case "tools/call":
		return i.routeToolCall(msg)

	case "resources/read":
		if i.hooks.OnResourceRead != nil {
			var params struct {
				URI string `json:"uri"`
			}
			_ = json.Unmarshal(extractParams(msg.Raw), &params)
			return i.hooks.OnResourceRead(params.URI, msg.Raw)
		}
	}

	return InterceptResult{Action: Forward}
}

// routeFromServer handles messages going from the MCP server to the agent host.
func (i *Interceptor) routeFromServer(msg MCPMessage) InterceptResult {
	// Sampling requests from the server are blocked by default (security-critical).
	if msg.IsRequest && msg.Method == "sampling/createMessage" {
		if i.hooks.OnSamplingRequest != nil {
			return i.hooks.OnSamplingRequest(msg.Raw)
		}
		return InterceptResult{
			Action: Block,
			Error: &JSONRPCError{
				Code:    -32603,
				Message: "sampling/createMessage blocked by mantismo policy",
			},
		}
	}

	// Correlate responses back to their originating requests.
	if msg.IsResponse && msg.ID != nil {
		key := idKey(msg.ID)
		if val, ok := i.pendingRequests.Load(key); ok {
			original := val.(MCPMessage) //nolint:forcetypeassert
			i.pendingRequests.Delete(key)
			return i.routeResponse(msg, original)
		}
	}

	return InterceptResult{Action: Forward}
}

// routeResponse dispatches a server response to the appropriate hook.
func (i *Interceptor) routeResponse(resp MCPMessage, original MCPMessage) InterceptResult {
	switch original.Method {
	case "tools/list":
		return i.handleToolsListResponse(resp)

	case "tools/call":
		if i.hooks.OnToolCallResponse != nil {
			tcResp := buildToolCallResponse(resp)
			tcReq := ToolCallRequest{
				ToolName:  extractToolName(original.Raw),
				Arguments: extractToolArguments(original.Raw),
			}
			if original.ID != nil {
				tcReq.RequestID = *original.ID
			}
			return i.hooks.OnToolCallResponse(tcResp, tcReq)
		}
	}

	return InterceptResult{Action: Forward}
}

// routeToolCall handles tools/call requests.
// Native Mantismo tools (prefix "vault_") are consumed rather than forwarded.
func (i *Interceptor) routeToolCall(msg MCPMessage) InterceptResult {
	toolName := extractToolName(msg.Raw)

	// Mantismo-native tools (vault_*) are handled internally; never forwarded.
	if strings.HasPrefix(toolName, "vault_") {
		// The vault tool handler (spec 13) will send the response through its own path.
		return InterceptResult{Action: Consume}
	}

	// Upstream tool: let the policy hook decide.
	if i.hooks.OnToolCall != nil {
		req := ToolCallRequest{
			ToolName:  toolName,
			Arguments: extractToolArguments(msg.Raw),
		}
		if msg.ID != nil {
			req.RequestID = *msg.ID
		}
		return i.hooks.OnToolCall(req)
	}

	return InterceptResult{Action: Forward}
}

// handleToolsListResponse augments the tool list with Mantismo-native tools.
func (i *Interceptor) handleToolsListResponse(msg MCPMessage) InterceptResult {
	if i.hooks.OnToolsList == nil {
		return InterceptResult{Action: Forward}
	}

	tools, err := parseToolsList(msg.Raw)
	if err != nil {
		return InterceptResult{Action: Forward}
	}

	augmented, err := i.hooks.OnToolsList(tools)
	if err != nil {
		return InterceptResult{Action: Forward}
	}

	modified, err := rebuildToolsListResponse(msg.Raw, augmented)
	if err != nil {
		return InterceptResult{Action: Forward}
	}

	return InterceptResult{Action: Modify, Modified: modified}
}

// ── Message parsing helpers ──────────────────────────────────────────────────

// parseMessage parses a raw JSON-RPC 2.0 message into MCPMessage.
func parseMessage(raw json.RawMessage) MCPMessage {
	// Use a raw map so we can distinguish absent vs null id.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return MCPMessage{Raw: raw}
	}

	msg := MCPMessage{Raw: raw}

	// Extract method (string field).
	if methodRaw, ok := fields["method"]; ok {
		_ = json.Unmarshal(methodRaw, &msg.Method)
	}

	// Extract id — presence in the map distinguishes a notification (absent)
	// from a request (id present, even if null).
	if idRaw, ok := fields["id"]; ok {
		id := json.RawMessage(append([]byte(nil), idRaw...))
		msg.ID = &id
	}

	// Classify the message type.
	if msg.Method != "" {
		if msg.ID != nil {
			msg.IsRequest = true
		} else {
			msg.IsNotification = true
		}
	} else {
		msg.IsResponse = true
		_, msg.IsError = fields["error"]
	}

	return msg
}

// idKey returns the string key used to store/lookup pending requests.
func idKey(id *json.RawMessage) string {
	if id == nil {
		return ""
	}
	return string(*id)
}

// extractParams returns the raw JSON params field, or nil if absent.
func extractParams(raw json.RawMessage) json.RawMessage {
	var env struct {
		Params json.RawMessage `json:"params"`
	}
	_ = json.Unmarshal(raw, &env)
	return env.Params
}

// extractToolName pulls the tool name from a tools/call request.
func extractToolName(raw json.RawMessage) string {
	var req struct {
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	_ = json.Unmarshal(raw, &req)
	return req.Params.Name
}

// extractToolArguments pulls the arguments from a tools/call request.
func extractToolArguments(raw json.RawMessage) json.RawMessage {
	var req struct {
		Params struct {
			Arguments json.RawMessage `json:"arguments"`
		} `json:"params"`
	}
	_ = json.Unmarshal(raw, &req)
	return req.Params.Arguments
}

// buildToolCallResponse constructs a ToolCallResponse from a parsed server response.
func buildToolCallResponse(msg MCPMessage) ToolCallResponse {
	resp := ToolCallResponse{}
	if msg.ID != nil {
		resp.RequestID = *msg.ID
	}
	if msg.IsError {
		var env struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(msg.Raw, &env)
		resp.IsError = true
		resp.ErrorMsg = env.Error.Message
	} else {
		var env struct {
			Result struct {
				Content json.RawMessage `json:"content"`
			} `json:"result"`
		}
		_ = json.Unmarshal(msg.Raw, &env)
		resp.Content = env.Result.Content
	}
	return resp
}

// parseToolsList extracts the tools array from a tools/list response.
func parseToolsList(raw json.RawMessage) ([]ToolInfo, error) {
	var env struct {
		Result struct {
			Tools []ToolInfo `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	return env.Result.Tools, nil
}

// rebuildToolsListResponse replaces the tools array in a tools/list response.
func rebuildToolsListResponse(original json.RawMessage, tools []ToolInfo) (json.RawMessage, error) {
	// Parse to a generic map so we preserve all top-level fields (jsonrpc, id, etc.).
	var env map[string]json.RawMessage
	if err := json.Unmarshal(original, &env); err != nil {
		return nil, err
	}

	// Parse the existing result object.
	var result map[string]json.RawMessage
	if resultRaw, ok := env["result"]; ok && len(resultRaw) > 0 {
		if err := json.Unmarshal(resultRaw, &result); err != nil {
			result = make(map[string]json.RawMessage)
		}
	} else {
		result = make(map[string]json.RawMessage)
	}

	// Replace the tools array.
	toolsJSON, err := json.Marshal(tools)
	if err != nil {
		return nil, err
	}
	result["tools"] = toolsJSON

	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	env["result"] = resultJSON

	return json.Marshal(env)
}
