# 05 — Spec: MCP Message Interceptor

## Objective

Build the MCP-aware layer that parses JSON-RPC messages into typed MCP structures, routes them to appropriate handlers, and supports augmenting the tool list with Mantismo-native tools (vault tools, added in spec 13).

This replaces the PassthroughHandler from spec 04 with an intelligent handler.

## Prerequisites

- Spec 04 (JSON-RPC Proxy) complete and passing tests

## MCP Methods to Intercept

Based on the MCP spec (2025-11-25), these are the methods Mantismo cares about:

| Method | Direction | Why We Intercept |
|--------|-----------|------------------|
| `initialize` | ToServer | Capture server capabilities, session start |
| `initialized` | ToServer (notif) | Session fully established |
| `tools/list` | FromServer (response) | Augment tool list, fingerprint tools |
| `tools/call` | ToServer | Policy evaluation, argument scanning, logging |
| `tools/call` response | FromServer | Response scanning, logging |
| `resources/list` | FromServer (response) | Logging |
| `resources/read` | ToServer | Policy evaluation, logging |
| `resources/read` response | FromServer | Response scanning |
| `sampling/createMessage` | FromServer (request) | Block/restrict (security-critical) |
| `notifications/*` | Both | Logging |

All other methods: forward unchanged (transparent proxy behavior).

## Interface Contract

### Package: `internal/interceptor`

```go
// MCPMessage represents a parsed JSON-RPC message with MCP-specific fields extracted.
type MCPMessage struct {
    Raw       json.RawMessage // Original JSON bytes
    ID        *json.RawMessage // Request/response ID (nil for notifications)
    Method    string           // MCP method (empty for responses)
    IsRequest bool             // true if this is a request (has method)
    IsNotification bool        // true if this is a notification (has method, no id)
    IsResponse bool            // true if this is a response (has result or error, no method)
    IsError   bool             // true if this is an error response
}

// ToolCallRequest represents a parsed tools/call request.
type ToolCallRequest struct {
    RequestID json.RawMessage
    ToolName  string
    Arguments json.RawMessage // Raw JSON arguments
}

// ToolCallResponse represents a parsed tools/call response.
type ToolCallResponse struct {
    RequestID json.RawMessage
    Content   json.RawMessage // The result content array
    IsError   bool
    ErrorMsg  string
}

// ToolInfo represents a tool from tools/list.
type ToolInfo struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    InputSchema json.RawMessage `json:"inputSchema"`
}

// InterceptAction represents what to do with a message.
type InterceptAction int
const (
    Forward  InterceptAction = iota // Forward to destination unchanged
    Modify                          // Forward the modified version
    Block                           // Do not forward; return error to sender
    Consume                         // Do not forward; Mantismo handles the response
)

// InterceptResult is returned by hook functions.
type InterceptResult struct {
    Action   InterceptAction
    Modified json.RawMessage // Used when Action == Modify
    Error    *JSONRPCError   // Used when Action == Block
    Response json.RawMessage // Used when Action == Consume
}

// Hooks defines callback functions for each interceptable event.
// Each hook is optional (nil = passthrough).
type Hooks struct {
    OnInitialize      func(raw json.RawMessage) InterceptResult
    OnToolsList       func(tools []ToolInfo) ([]ToolInfo, error) // Can augment the tool list
    OnToolCall        func(req ToolCallRequest) InterceptResult
    OnToolCallResponse func(resp ToolCallResponse, originalReq ToolCallRequest) InterceptResult
    OnResourceRead    func(uri string, raw json.RawMessage) InterceptResult
    OnSamplingRequest func(raw json.RawMessage) InterceptResult
    OnAnyMessage      func(msg MCPMessage, dir proxy.Direction) // For logging; cannot modify
}

// Interceptor implements proxy.MessageHandler using MCP-aware routing.
type Interceptor struct {
    hooks Hooks
    // internal: maps request IDs to their original requests for response correlation
    pendingRequests sync.Map // map[string]MCPMessage
}

// New creates a new Interceptor with the given hooks.
func New(hooks Hooks) *Interceptor

// Handle implements proxy.MessageHandler.
func (i *Interceptor) Handle(msg json.RawMessage, dir proxy.Direction) (json.RawMessage, error)
```

## Detailed Requirements

### 5.1 Message Parsing

- Parse every JSON-RPC message into MCPMessage
- Extract `method` field for requests/notifications
- Extract `id` field to correlate requests with responses
- For requests: store in `pendingRequests` map (keyed by serialized ID)
- For responses: look up the original request by ID to determine what method this is responding to

### 5.2 Request-Response Correlation

The JSON-RPC `id` field links responses to requests. This is critical because responses don't contain the `method` — we need to know that response ID 5 was a `tools/call` so we can route it to `OnToolCallResponse`.

```go
// When we see a request going ToServer:
if msg.IsRequest {
    i.pendingRequests.Store(idKey(msg.ID), msg)
}

// When we see a response coming FromServer:
if msg.IsResponse {
    if original, ok := i.pendingRequests.Load(idKey(msg.ID)); ok {
        // Now we know this response's method = original.Method
        i.pendingRequests.Delete(idKey(msg.ID))
    }
}
```

### 5.3 Tool List Augmentation

When the server responds to `tools/list`, the Interceptor:
1. Parses the tool list from the response
2. Calls `OnToolsList(tools)` which can add Mantismo-native tools (e.g., vault tools)
3. Serializes the augmented list back into the response
4. Forwards the modified response

This is how vault tools appear alongside upstream server tools without the server knowing.

### 5.4 Tool Call Routing

When a `tools/call` request comes from the host:
1. Parse tool name and arguments
2. Check if the tool name is a Mantismo-native tool (prefix: `vault_`)
3. If native: return `Consume` action — Interceptor handles the response directly (spec 13)
4. If upstream: call `OnToolCall` hook for policy evaluation (spec 09)

### 5.5 Sampling Request Blocking

`sampling/createMessage` is a request FROM the server asking the host to make an LLM completion. This is a known attack vector. The Interceptor:
1. Calls `OnSamplingRequest` hook
2. Default behavior (if no hook): **block** with error response
3. Log the attempt

### 5.6 Universal Logging Hook

`OnAnyMessage` is called for EVERY message in BOTH directions, after all other processing. It cannot modify messages — it's strictly for the logging subsystem (spec 06).

## Test Plan

### Unit Tests

1. **TestParseRequest** — Parse a `tools/call` request, verify all fields extracted
2. **TestParseResponse** — Parse a response, verify ID correlation works
3. **TestParseNotification** — Parse a notification (no ID), verify correct type
4. **TestParseErrorResponse** — Parse an error response, verify error fields
5. **TestRequestResponseCorrelation** — Send request then response with same ID, verify OnToolCallResponse receives both
6. **TestToolListAugmentation** — OnToolsList adds a tool, verify augmented response
7. **TestNativeToolRouting** — tools/call for "vault_get_profile" returns Consume action
8. **TestSamplingBlocked** — sampling/createMessage returns Block action by default
9. **TestUnknownMethodPassthrough** — Unknown method passes through unchanged
10. **TestOnAnyMessageCalledForAll** — Verify logging hook receives every message

### Integration Tests (with proxy from spec 04)

11. **TestInterceptorWithProxy** — Wire Interceptor into Proxy, send initialize + tools/list + tools/call sequence through mock server, verify all hooks fire
12. **TestAugmentedToolsVisible** — Agent host receives tools/list response with injected Mantismo tools

## Acceptance Criteria

- [ ] Interceptor correctly parses all JSON-RPC message types
- [ ] Request-response correlation tracks pending requests by ID
- [ ] Tool list augmentation injects additional tools into responses
- [ ] Native tool calls (vault_*) are consumed, not forwarded
- [ ] sampling/createMessage is blocked by default
- [ ] OnAnyMessage fires for every message regardless of type
- [ ] Unknown methods pass through unchanged (transparent proxy behavior preserved)
- [ ] All 12 tests pass
