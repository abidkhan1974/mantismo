// Copyright 2026 Abid Ali Khan. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

// Package approval — backend_terminal.go implements the terminal approval backend.
// It prints prompts to stderr and reads user input from /dev/tty.
package approval

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// TerminalBackend presents approval prompts on the terminal.
type TerminalBackend struct {
	in  io.Reader
	out io.Writer
}

// NewTerminalBackend creates a TerminalBackend that reads from /dev/tty and
// writes to os.Stderr.
func NewTerminalBackend() *TerminalBackend {
	tty, err := os.Open("/dev/tty") //nolint:gosec
	if err != nil {
		// Fall back to stdin if /dev/tty is unavailable.
		return &TerminalBackend{in: os.Stdin, out: os.Stderr}
	}
	return &TerminalBackend{in: tty, out: os.Stderr}
}

// newTerminalBackendWithIO creates a TerminalBackend with injected I/O (for tests).
func newTerminalBackendWithIO(in io.Reader, out io.Writer) *TerminalBackend {
	return &TerminalBackend{in: in, out: out}
}

// Name returns the backend identifier.
func (t *TerminalBackend) Name() string { return "terminal" }

// Priority returns 100 — terminal is the low-priority fallback backend.
func (t *TerminalBackend) Priority() int { return 100 }

// Available returns true when /dev/tty exists on the system.
func (t *TerminalBackend) Available() bool {
	_, err := os.Stat("/dev/tty")
	return err == nil
}

// Prompt prints the approval request to out and reads the user's choice from in.
// It is context-cancellable: if ctx is cancelled the prompt returns Denied.
func (t *TerminalBackend) Prompt(ctx context.Context, req ApprovalPrompt) (ApprovalResponse, error) {
	secsLeft := int(time.Until(req.ExpiresAt).Seconds())
	if secsLeft < 0 {
		secsLeft = 0
	}

	border := strings.Repeat("━", 50)
	fmt.Fprintf(t.out, "%s\n", border)
	fmt.Fprintf(t.out, "⚡ APPROVAL REQUIRED\n")
	fmt.Fprintf(t.out, "%s\n", border)
	fmt.Fprintf(t.out, "Tool:    %s\n", req.ToolName)
	fmt.Fprintf(t.out, "Server:  %s\n", req.ServerCmd)
	fmt.Fprintf(t.out, "Reason:  %s\n", req.Reason)
	fmt.Fprintf(t.out, "Args:    %s\n\n", req.Arguments)
	fmt.Fprintf(t.out, "  [1] Allow (this call only)\n")
	fmt.Fprintf(t.out, "  [2] Allow (5 minutes)\n")
	fmt.Fprintf(t.out, "  [3] Allow (30 minutes)\n")
	fmt.Fprintf(t.out, "  [4] Allow (this session)\n")
	fmt.Fprintf(t.out, "  [5] Allow (always for this tool)\n")
	fmt.Fprintf(t.out, "  [d] Deny\n\n")
	fmt.Fprintf(t.out, "Choice (auto-deny in %ds): ", secsLeft)

	// Read input in a goroutine so we can race against ctx.Done().
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		scanner := bufio.NewScanner(t.in)
		if scanner.Scan() {
			ch <- result{line: strings.TrimSpace(scanner.Text())}
		} else {
			if err := scanner.Err(); err != nil {
				ch <- result{err: err}
			} else {
				ch <- result{line: ""}
			}
		}
	}()

	select {
	case <-ctx.Done():
		return ApprovalResponse{Decision: Denied}, nil
	case r := <-ch:
		if r.err != nil {
			return ApprovalResponse{Decision: Denied}, nil
		}
		return parseTerminalChoice(r.line), nil
	}
}

// parseTerminalChoice maps a raw input line to an ApprovalResponse.
func parseTerminalChoice(line string) ApprovalResponse {
	switch line {
	case "1":
		return ApprovalResponse{Decision: Approved, GrantScope: ThisCallOnly}
	case "2":
		return ApprovalResponse{Decision: Approved, GrantScope: For5Minutes}
	case "3":
		return ApprovalResponse{Decision: Approved, GrantScope: For30Minutes}
	case "4":
		return ApprovalResponse{Decision: Approved, GrantScope: ForSession}
	case "5":
		return ApprovalResponse{Decision: Approved, GrantScope: Permanently}
	default:
		return ApprovalResponse{Decision: Denied}
	}
}
