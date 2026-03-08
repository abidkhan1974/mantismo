// Copyright 2026 Mantismo. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

// Package proxy — framing.go handles newline-delimited JSON-RPC message framing over stdio.
//
// MCP uses JSON-RPC 2.0 over stdio with newline-delimited messages.
// Each message is a single line of JSON terminated by '\n'.
package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

const (
	// MaxMessageSize is the maximum allowed size for a single JSON-RPC message (10 MB).
	MaxMessageSize = 10 * 1024 * 1024
)

// Scanner reads newline-delimited JSON-RPC messages from a reader.
// It skips empty lines and enforces MaxMessageSize.
type Scanner struct {
	sc *bufio.Scanner
}

// NewScanner creates a Scanner that reads from r.
func NewScanner(r io.Reader) *Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), MaxMessageSize)
	return &Scanner{sc: sc}
}

// Scan advances to the next non-empty JSON-RPC message.
// Returns false when the reader is exhausted or an error occurs.
func (s *Scanner) Scan() bool {
	for s.sc.Scan() {
		line := bytes.TrimSpace(s.sc.Bytes())
		if len(line) > 0 {
			return true
		}
		// skip empty/blank lines
	}
	return false
}

// Bytes returns the raw bytes of the current message (not newline-terminated).
// Only valid after a successful Scan().
func (s *Scanner) Bytes() []byte {
	return s.sc.Bytes()
}

// Err returns the first non-EOF error encountered by the scanner.
func (s *Scanner) Err() error {
	return s.sc.Err()
}

// WriteMessage writes a JSON-RPC message to w, appending a newline.
// Thread-safety is the caller's responsibility.
func WriteMessage(w io.Writer, msg json.RawMessage) error {
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("proxy: write message: %w", err)
	}
	if _, err := w.Write([]byte{'\n'}); err != nil {
		return fmt.Errorf("proxy: write newline: %w", err)
	}
	return nil
}

// Envelope holds the top-level JSON-RPC fields needed for message routing.
type Envelope struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
	Result *json.RawMessage `json:"result,omitempty"`
}

// ParseEnvelope parses the minimal envelope fields needed for message routing.
func ParseEnvelope(raw json.RawMessage) (Envelope, error) {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return Envelope{}, fmt.Errorf("proxy: parse envelope: %w", err)
	}
	return env, nil
}

// MakeErrorResponse creates a JSON-RPC 2.0 error response for the given request id.
func MakeErrorResponse(id *json.RawMessage, code int, message string) json.RawMessage {
	type errObj struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	type resp struct {
		JSONRPC string           `json:"jsonrpc"`
		ID      *json.RawMessage `json:"id"`
		Error   errObj           `json:"error"`
	}
	r := resp{
		JSONRPC: "2.0",
		ID:      id,
		Error:   errObj{Code: code, Message: message},
	}
	b, _ := json.Marshal(r)
	return json.RawMessage(b)
}
