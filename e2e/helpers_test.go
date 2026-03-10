// Copyright 2026 Abid Ali Khan. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

package e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// binaryPath is set by TestMain.
var binaryPath string

// pythonPath is set by TestMain.
var pythonPath string

// projectRoot is set by TestMain.
var projectRoot string

func TestMain(m *testing.M) {
	// Find python3
	var err error
	pythonPath, err = exec.LookPath("python3")
	if err != nil {
		pythonPath, err = exec.LookPath("python")
		if err != nil {
			fmt.Println("python3/python not found — skipping E2E tests")
			os.Exit(0)
		}
	}

	// Find project root (where go.mod lives).
	// This file lives in <projectRoot>/e2e/helpers_test.go,
	// so project root is one level up from the e2e dir.
	wd, _ := os.Getwd()
	projectRoot = filepath.Dir(wd) // e2e/ is one level below project root

	// Build the binary.
	binaryPath = filepath.Join(os.TempDir(), "mantismo-e2e-test")
	buildCmd := exec.Command(
		"go", "build",
		"-o", binaryPath,
		"./cmd/mantismo/",
	)
	buildCmd.Dir = projectRoot
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, buildErr := buildCmd.CombinedOutput(); buildErr != nil {
		fmt.Fprintf(os.Stderr, "failed to build mantismo: %v\n%s\n", buildErr, out)
		os.Exit(1)
	}

	code := m.Run()
	_ = os.Remove(binaryPath)
	os.Exit(code)
}

// ProxyOption configures a test proxy.
type ProxyOption func(*proxyConfig)

type proxyConfig struct {
	preset    string
	policyDir string
	mockArgs  []string
	dataDir   string
}

// WithPreset sets the policy preset.
func WithPreset(preset string) ProxyOption {
	return func(c *proxyConfig) { c.preset = preset }
}

// WithPolicyDir sets a custom policy directory.
func WithPolicyDir(dir string) ProxyOption {
	return func(c *proxyConfig) { c.policyDir = dir }
}

// WithMockArgs passes extra args to the mock MCP server.
func WithMockArgs(args ...string) ProxyOption {
	return func(c *proxyConfig) { c.mockArgs = append(c.mockArgs, args...) }
}

// WithDataDir sets a specific data directory (to persist fingerprints across sessions).
func WithDataDir(dir string) ProxyOption {
	return func(c *proxyConfig) { c.dataDir = dir }
}

// safeBuffer is a goroutine-safe bytes.Buffer.
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (sb *safeBuffer) Write(p []byte) (int, error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Write(p)
}

func (sb *safeBuffer) String() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.String()
}

// TestProxy wraps a running mantismo proxy for test interaction.
type TestProxy struct {
	t       *testing.T
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	Stderr  *safeBuffer
	LogDir  string
	DataDir string
	cancel  context.CancelFunc
}

// StartProxy builds and launches the mantismo proxy wrapping the mock MCP server.
func StartProxy(t *testing.T, opts ...ProxyOption) *TestProxy {
	t.Helper()
	cfg := &proxyConfig{
		preset: "balanced",
	}
	for _, o := range opts {
		o(cfg)
	}

	// Use provided dataDir or create a temp one.
	dataDir := cfg.dataDir
	if dataDir == "" {
		dataDir = t.TempDir()
	}
	logDir := filepath.Join(dataDir, ".mantismo", "logs")

	mockServerPath := filepath.Join(projectRoot, "testdata", "mock_mcp_server.py")
	mockArgs := append([]string{mockServerPath}, cfg.mockArgs...)

	args := []string{
		"wrap",
		"--preset", cfg.preset,
		"--port", "0", // auto-assign API port
	}

	if cfg.policyDir != "" {
		args = append(args, "--policy-dir", cfg.policyDir)
	}

	args = append(args, "--")
	args = append(args, pythonPath)
	args = append(args, mockArgs...)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Env = append(os.Environ(),
		"HOME="+dataDir, // isolate data dir so ~/.mantismo goes into dataDir
		"CGO_ENABLED=0",
	)

	stdinPipe, err := cmd.StdinPipe()
	require.NoError(t, err)

	stdoutPipe, err := cmd.StdoutPipe()
	require.NoError(t, err)

	stderr := &safeBuffer{}
	cmd.Stderr = stderr

	require.NoError(t, cmd.Start())

	sc := bufio.NewScanner(stdoutPipe)
	sc.Buffer(make([]byte, 1<<20), 1<<20) // set large buffer once at creation

	tp := &TestProxy{
		t:       t,
		cmd:     cmd,
		stdin:   stdinPipe,
		scanner: sc,
		Stderr:  stderr,
		LogDir:  logDir,
		DataDir: dataDir,
		cancel:  cancel,
	}

	t.Cleanup(tp.Close)

	// Wait a short moment for the proxy to start.
	time.Sleep(300 * time.Millisecond)

	return tp
}

// SendRequest sends a JSON-RPC request and returns the response (raw JSON).
func (tp *TestProxy) SendRequest(id int, method string, params interface{}) json.RawMessage {
	tp.t.Helper()
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		msg["params"] = params
	}
	b, err := json.Marshal(msg)
	require.NoError(tp.t, err)

	_, err = fmt.Fprintf(tp.stdin, "%s\n", b)
	require.NoError(tp.t, err)

	// Read next line from stdout.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if tp.scanner.Scan() {
			return json.RawMessage(append([]byte(nil), tp.scanner.Bytes()...))
		}
		if err := tp.scanner.Err(); err != nil {
			tp.t.Fatalf("scanner error reading response to %q: %v", method, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	tp.t.Fatalf("timeout waiting for response to %q", method)
	return nil
}

// SendNotification sends a JSON-RPC notification (no ID, no response expected).
func (tp *TestProxy) SendNotification(method string, params interface{}) {
	tp.t.Helper()
	msg := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		msg["params"] = params
	}
	b, _ := json.Marshal(msg)
	_, _ = fmt.Fprintf(tp.stdin, "%s\n", b)
}

// WaitForStderr waits up to timeout for the given string to appear on stderr.
func (tp *TestProxy) WaitForStderr(contains string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(tp.Stderr.String(), contains) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// Initialize performs the MCP handshake.
func (tp *TestProxy) Initialize() json.RawMessage {
	resp := tp.SendRequest(1, "initialize", map[string]interface{}{
		"protocolVersion": "2025-11-25",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "test-client", "version": "1.0"},
	})
	tp.SendNotification("notifications/initialized", nil)
	return resp
}

// Close stops the proxy.
func (tp *TestProxy) Close() {
	_ = tp.stdin.Close()
	tp.cancel()
	_ = tp.cmd.Wait()
}
