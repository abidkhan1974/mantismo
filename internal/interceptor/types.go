// Copyright 2026 Mantismo. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

// Package interceptor — types.go defines MCP message types, hook signatures,
// and action constants for the MCP message interceptor.
package interceptor

import (
	"encoding/json"

	"github.com/abidkhan1974/mantismo/internal/proxy"
)

// MCPMessage represents a parsed JSON-RPC 2.0 message with MCP-specific fields.
type MCPMessage struct {
	// Raw is the original JSON bytes.
	Raw json.RawMessage
	// ID is the request/response ID. Nil for notifications.
	ID *json.RawMessage
	// Method is the MCP method name. Empty for responses.
	Method string
	// IsRequest is true if the message has both a method and an id (a call).
	IsRequest bool
	// IsNotification is true if the message has a method but no id.
	IsNotification bool
	// IsResponse is true if the message has no method (has result or error).
	IsResponse bool
	// IsError is true if the message is an error response.
	IsError bool
}

// ToolCallRequest represents a parsed tools/call request.
type ToolCallRequest struct {
	// RequestID is the raw JSON-RPC id for this request.
	RequestID json.RawMessage
	// ToolName is the name of the tool being called.
	ToolName string
	// Arguments is the raw JSON arguments object.
	Arguments json.RawMessage
}

// ToolCallResponse represents a parsed tools/call response.
type ToolCallResponse struct {
	// RequestID is the raw JSON-RPC id that this response answers.
	RequestID json.RawMessage
	// Content is the raw JSON content array from the result.
	Content json.RawMessage
	// IsError is true if this is a tool error (not a JSON-RPC error).
	IsError bool
	// ErrorMsg is the error message when IsError is true.
	ErrorMsg string
}

// ToolInfo represents a single tool from a tools/list response.
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// InterceptAction represents the decision an interceptor hook makes.
type InterceptAction int

const (
	// Forward the message to its destination unchanged.
	Forward InterceptAction = iota
	// Modify: forward the modified version in InterceptResult.Modified.
	Modify
	// Block: do not forward; the proxy returns a JSON-RPC error to the sender.
	Block
	// Consume: do not forward; Mantismo will handle the response directly.
	Consume
)

// JSONRPCError is a structured JSON-RPC 2.0 error used in Block results.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Error implements the error interface so JSONRPCError can be returned as error.
func (e *JSONRPCError) Error() string {
	return e.Message
}

// InterceptResult is returned by hook functions to indicate what to do with a message.
type InterceptResult struct {
	// Action is the routing decision.
	Action InterceptAction
	// Modified holds the replacement message when Action == Modify.
	Modified json.RawMessage
	// Error is the error to return to the sender when Action == Block.
	Error *JSONRPCError
	// Response holds the Mantismo-generated response when Action == Consume.
	Response json.RawMessage
}

// Hooks defines callback functions for each interceptable MCP event.
// Each hook is optional; a nil hook means the message is forwarded unchanged.
type Hooks struct {
	// OnInitialize is called when an initialize request goes to the server.
	OnInitialize func(raw json.RawMessage) InterceptResult

	// OnToolsList is called when the server returns a tools/list response.
	// The hook may add, remove, or modify tools; returning an error causes passthrough.
	OnToolsList func(tools []ToolInfo) ([]ToolInfo, error)

	// OnToolCall is called for every upstream tools/call request.
	// Native (vault_*) tools are never passed to this hook.
	OnToolCall func(req ToolCallRequest) InterceptResult

	// OnToolCallResponse is called when the server responds to a tools/call.
	OnToolCallResponse func(resp ToolCallResponse, originalReq ToolCallRequest) InterceptResult

	// OnResourceRead is called for resources/read requests to the server.
	OnResourceRead func(uri string, raw json.RawMessage) InterceptResult

	// OnSamplingRequest is called when the server issues a sampling/createMessage
	// request to the host. If nil, the request is blocked by default.
	OnSamplingRequest func(raw json.RawMessage) InterceptResult

	// OnAnyMessage is called for every message in both directions after all
	// other processing. It is intended for logging and cannot modify messages.
	OnAnyMessage func(msg MCPMessage, dir proxy.Direction)
}
