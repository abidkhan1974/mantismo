// Package proxy manages the stdio subprocess lifecycle and JSON-RPC message forwarding
// between the agent host and the MCP server.
//
// Architecture:
//
//	Agent Host stdin  ──► Proxy ──► MCP Server stdin
//	Agent Host stdout ◄── Proxy ◄── MCP Server stdout
//	Agent Host stderr ◄── Proxy ◄── MCP Server stderr (passthrough)
package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Direction indicates whether a message is heading to the MCP server or coming from it.
type Direction int

const (
	// ToServer means the message originated from the agent host and is heading to the MCP server.
	ToServer Direction = iota
	// FromServer means the message originated from the MCP server and is heading to the agent host.
	FromServer
)

// MessageHandler is called for each JSON-RPC message that passes through the proxy.
// It may return a modified message, nil (to drop the message), or an error.
// If it returns an error and dir == ToServer, the proxy writes a JSON-RPC error
// response back to the agent host. If dir == FromServer, errors are logged and the
// original message is forwarded.
type MessageHandler func(msg json.RawMessage, dir Direction) (json.RawMessage, error)

// PassthroughHandler forwards every message unchanged. Used when no interceptor is wired.
func PassthroughHandler(msg json.RawMessage, _ Direction) (json.RawMessage, error) {
	return msg, nil
}

// Config holds the configuration for the stdio proxy.
type Config struct {
	// Command is the MCP server executable.
	Command string
	// Args are the arguments passed to Command.
	Args []string
	// Env holds additional environment variables (KEY=VALUE format).
	// The current process environment is always inherited.
	Env []string
	// WorkDir is the working directory for the subprocess. Defaults to current dir.
	WorkDir string
}

// Proxy manages the MCP server subprocess and bidirectional message forwarding.
type Proxy struct {
	config  Config
	handler MessageHandler
	// stdin is the writer connected to the proxy's own os.Stdin (agent host → us)
	stdin io.Reader
	// stdout is the writer connected to the proxy's own os.Stdout (us → agent host)
	stdout io.Writer
}

// New creates a new Proxy with the given config and message handler.
// If handler is nil, PassthroughHandler is used.
func New(config Config, handler MessageHandler) *Proxy {
	if handler == nil {
		handler = PassthroughHandler
	}
	return &Proxy{
		config:  config,
		handler: handler,
		stdin:   os.Stdin,
		stdout:  os.Stdout,
	}
}

// NewWithIO creates a proxy with custom stdin/stdout (used in tests).
func NewWithIO(config Config, handler MessageHandler, stdin io.Reader, stdout io.Writer) *Proxy {
	if handler == nil {
		handler = PassthroughHandler
	}
	return &Proxy{
		config:  config,
		handler: handler,
		stdin:   stdin,
		stdout:  stdout,
	}
}

// Run starts the MCP server subprocess and begins proxying messages.
// It blocks until the subprocess exits or ctx is cancelled.
// Returns nil on clean exit, or an error describing what went wrong.
func (p *Proxy) Run(ctx context.Context) error {
	// Validate command
	if p.config.Command == "" {
		return fmt.Errorf("proxy: no command specified")
	}

	// Build the command
	cmd := exec.CommandContext(ctx, p.config.Command, p.config.Args...)
	cmd.Env = append(os.Environ(), p.config.Env...)
	if p.config.WorkDir != "" {
		cmd.Dir = p.config.WorkDir
	}
	// Passthrough stderr from MCP server to our stderr
	cmd.Stderr = os.Stderr

	// Pipes for bidirectional I/O with the subprocess
	serverStdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("proxy: create stdin pipe: %w", err)
	}
	serverStdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("proxy: create stdout pipe: %w", err)
	}

	// Start the subprocess
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("proxy: start %q: %w", p.config.Command, err)
	}

	// Create a cancellable context tied to subprocess lifetime
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	// ── Host → Server goroutine ─────────────────────────────────────────────
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer serverStdin.Close()

		sc := NewScanner(p.stdin)
		for sc.Scan() {
			if runCtx.Err() != nil {
				return
			}
			raw := json.RawMessage(append([]byte(nil), sc.Bytes()...))

			out, herr := p.handler(raw, ToServer)
			if herr != nil {
				// Handler error: write JSON-RPC error back to agent host
				env, _ := ParseEnvelope(raw)
				errResp := MakeErrorResponse(env.ID, -32603, herr.Error())
				_ = p.writeToHost(errResp)
				continue
			}
			if out == nil {
				continue // handler dropped the message
			}
			if werr := WriteMessage(serverStdin, out); werr != nil {
				if !isClosedPipe(werr) {
					errCh <- fmt.Errorf("proxy: write to server: %w", werr)
				}
				return
			}
		}
		if err := sc.Err(); err != nil && !isClosedPipe(err) {
			errCh <- fmt.Errorf("proxy: read from host stdin: %w", err)
		}
	}()

	// ── Server → Host goroutine ─────────────────────────────────────────────
	wg.Add(1)
	go func() {
		defer wg.Done()

		sc := NewScanner(serverStdout)
		for sc.Scan() {
			if runCtx.Err() != nil {
				return
			}
			raw := json.RawMessage(append([]byte(nil), sc.Bytes()...))

			out, herr := p.handler(raw, FromServer)
			if herr != nil {
				// From-server handler errors: log and drop the message.
				// Dropping (not forwarding) is intentional — the handler
				// signalled that this content must not reach the host
				// (e.g., sampling/createMessage blocked by policy).
				fmt.Fprintf(os.Stderr, "[proxy] handler error (from server): %v\n", herr)
				continue
			}
			if out == nil {
				continue
			}
			if werr := p.writeToHost(out); werr != nil {
				if !isClosedPipe(werr) {
					errCh <- fmt.Errorf("proxy: write to host stdout: %w", werr)
				}
				return
			}
		}
		if err := sc.Err(); err != nil && !isClosedPipe(err) {
			errCh <- fmt.Errorf("proxy: read from server stdout: %w", err)
		}
	}()

	// ── Wait for subprocess to finish ───────────────────────────────────────
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	var exitErr error
	select {
	case exitErr = <-waitDone:
		cancel() // stop goroutines

	case <-ctx.Done():
		// Context cancelled: signal subprocess to terminate gracefully
		if cmd.Process != nil {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			// Give it 5 seconds before SIGKILL
			timer := time.NewTimer(5 * time.Second)
			select {
			case exitErr = <-waitDone:
				timer.Stop()
			case <-timer.C:
				_ = cmd.Process.Kill()
				exitErr = <-waitDone
			}
		}
		cancel()
	}

	// Wait for goroutines to drain
	wg.Wait()

	// Drain error channel
	close(errCh)
	for e := range errCh {
		if exitErr == nil {
			exitErr = e
		}
	}

	return exitErr
}

// writeToHost writes a message to the agent host (our stdout). Thread-safe.
func (p *Proxy) writeToHost(msg json.RawMessage) error {
	return WriteMessage(p.stdout, msg)
}

// isClosedPipe reports whether err is a broken/closed pipe error, which is
// expected during normal shutdown and should not be treated as fatal.
func isClosedPipe(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) {
		return true
	}
	if errors.Is(err, os.ErrClosed) {
		return true
	}
	// syscall.EPIPE / syscall.ECONNRESET
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.EPIPE || errno == syscall.ECONNRESET
	}
	return false
}
