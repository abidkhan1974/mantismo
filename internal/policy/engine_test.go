// Copyright 2026 Mantismo. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

package policy_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abidkhan1974/mantismo/internal/policy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── preset helpers ─────────────────────────────────────────────────────────────

// presetDir returns a temp dir containing the named preset .rego file.
func presetDir(t *testing.T, presetName string) string {
	t.Helper()
	// Locate the policies/ directory relative to the project root.
	// The test runs from internal/policy/, so we go up two levels.
	projectRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	src := filepath.Join(projectRoot, "policies", presetName+".rego")
	data, err := os.ReadFile(src)
	require.NoError(t, err, "preset file %s not found", src)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, presetName+".rego"), data, 0600))
	return dir
}

func makeInput(method, toolName string) policy.EvalInput {
	return policy.EvalInput{
		Method:    method,
		ToolName:  toolName,
		Direction: "to_server",
		SessionID: "test",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
}

// ── TestParanoidBlocksAll ─────────────────────────────────────────────────────

func TestParanoidBlocksAll(t *testing.T) {
	e, err := policy.NewEngine(presetDir(t, "paranoid"))
	require.NoError(t, err)

	result, err := e.Evaluate(makeInput("tools/call", "get_file_contents"))
	require.NoError(t, err)
	// In paranoid mode the default is "approve" (requires human confirmation)
	assert.Equal(t, policy.Approve, result.Decision)
}

// ── TestParanoidAllowsProtocol ────────────────────────────────────────────────

func TestParanoidAllowsProtocol(t *testing.T) {
	e, err := policy.NewEngine(presetDir(t, "paranoid"))
	require.NoError(t, err)

	for _, method := range []string{"initialize", "shutdown"} {
		result, err := e.Evaluate(makeInput(method, ""))
		require.NoError(t, err)
		assert.Equal(t, policy.Allow, result.Decision, "method %s should be allowed", method)
	}
}

// ── TestBalancedAllowsReads ───────────────────────────────────────────────────

func TestBalancedAllowsReads(t *testing.T) {
	e, err := policy.NewEngine(presetDir(t, "balanced"))
	require.NoError(t, err)

	readTools := []string{"get_file_contents", "list_files", "search_repos", "read_file", "fetch_data"}
	for _, tool := range readTools {
		result, err := e.Evaluate(makeInput("tools/call", tool))
		require.NoError(t, err)
		assert.Equal(t, policy.Allow, result.Decision, "read tool %s should be allowed", tool)
	}
}

// ── TestBalancedApprovesWrites ────────────────────────────────────────────────

func TestBalancedApprovesWrites(t *testing.T) {
	e, err := policy.NewEngine(presetDir(t, "balanced"))
	require.NoError(t, err)

	writeTools := []string{"create_file", "delete_file", "update_record", "execute_command", "run_script"}
	for _, tool := range writeTools {
		result, err := e.Evaluate(makeInput("tools/call", tool))
		require.NoError(t, err)
		assert.Equal(t, policy.Approve, result.Decision, "write tool %s should require approval", tool)
	}
}

// ── TestPermissiveAllowsAll ───────────────────────────────────────────────────

func TestPermissiveAllowsAll(t *testing.T) {
	e, err := policy.NewEngine(presetDir(t, "permissive"))
	require.NoError(t, err)

	tools := []string{"get_file", "create_file", "delete_everything", "execute_rm_rf"}
	for _, tool := range tools {
		result, err := e.Evaluate(makeInput("tools/call", tool))
		require.NoError(t, err)
		assert.Equal(t, policy.Allow, result.Decision, "permissive: tool %s should be allowed", tool)
	}
}

// ── TestAllPresetsBlockSampling ───────────────────────────────────────────────

func TestAllPresetsBlockSampling(t *testing.T) {
	for _, preset := range []string{"paranoid", "balanced", "permissive"} {
		preset := preset
		t.Run(preset, func(t *testing.T) {
			e, err := policy.NewEngine(presetDir(t, preset))
			require.NoError(t, err)

			result, err := e.Evaluate(makeInput("sampling/createMessage", ""))
			require.NoError(t, err)
			assert.Equal(t, policy.Deny, result.Decision, "preset %s must block sampling", preset)
		})
	}
}

// ── TestChangedToolHandling ───────────────────────────────────────────────────

func TestChangedToolHandling(t *testing.T) {
	// Paranoid: changed + unacknowledged → deny
	t.Run("paranoid_denies_changed", func(t *testing.T) {
		e, err := policy.NewEngine(presetDir(t, "paranoid"))
		require.NoError(t, err)

		inp := makeInput("tools/call", "exec")
		inp.ToolChanged = true
		inp.ToolAcknowledged = false
		result, err := e.Evaluate(inp)
		require.NoError(t, err)
		assert.Equal(t, policy.Deny, result.Decision)
	})

	// Balanced: changed + unacknowledged → approve
	t.Run("balanced_approves_changed", func(t *testing.T) {
		e, err := policy.NewEngine(presetDir(t, "balanced"))
		require.NoError(t, err)

		inp := makeInput("tools/call", "get_file_contents")
		inp.ToolChanged = true
		inp.ToolAcknowledged = false
		result, err := e.Evaluate(inp)
		require.NoError(t, err)
		assert.Equal(t, policy.Approve, result.Decision)
	})

	// Acknowledged in paranoid → still approve (default), not deny
	t.Run("paranoid_acked_tool_approves", func(t *testing.T) {
		e, err := policy.NewEngine(presetDir(t, "paranoid"))
		require.NoError(t, err)

		inp := makeInput("tools/call", "exec")
		inp.ToolChanged = true
		inp.ToolAcknowledged = true // user said it's OK
		result, err := e.Evaluate(inp)
		require.NoError(t, err)
		// block_changed does not fire (acknowledged), falls through to default (approve)
		assert.Equal(t, policy.Approve, result.Decision)
	})
}

// ── TestCustomPolicyOverride ──────────────────────────────────────────────────

func TestCustomPolicyOverride(t *testing.T) {
	dir := t.TempDir()

	// Custom policy that denies calls to a specific server
	custom := `package mantismo

import future.keywords.if

decision := {"decision": "deny", "reason": "blocked by custom rule", "rule": "custom_block"} if {
	input.tool_name == "forbidden_tool"
}

default decision := {"decision": "allow", "reason": "custom default", "rule": "custom_allow"}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "custom.rego"), []byte(custom), 0600))

	e, err := policy.NewEngine(dir)
	require.NoError(t, err)

	// The forbidden tool should be denied.
	result, err := e.Evaluate(makeInput("tools/call", "forbidden_tool"))
	require.NoError(t, err)
	assert.Equal(t, policy.Deny, result.Decision)
	assert.Equal(t, "custom_block", result.Rule)

	// Other tools should be allowed.
	result2, err := e.Evaluate(makeInput("tools/call", "other_tool"))
	require.NoError(t, err)
	assert.Equal(t, policy.Allow, result2.Decision)
}

// ── TestPolicyReload ──────────────────────────────────────────────────────────

func TestPolicyReload(t *testing.T) {
	dir := t.TempDir()
	policyFile := filepath.Join(dir, "policy.rego")

	// Initial policy: allow all.
	v1 := `package mantismo
default decision := {"decision": "allow", "reason": "v1", "rule": "v1_allow"}
`
	require.NoError(t, os.WriteFile(policyFile, []byte(v1), 0600))

	e, err := policy.NewEngine(dir)
	require.NoError(t, err)

	r1, err := e.Evaluate(makeInput("tools/call", "get_file"))
	require.NoError(t, err)
	assert.Equal(t, policy.Allow, r1.Decision)

	// New policy: deny all.
	v2 := `package mantismo
default decision := {"decision": "deny", "reason": "v2", "rule": "v2_deny"}
`
	require.NoError(t, os.WriteFile(policyFile, []byte(v2), 0600))

	require.NoError(t, e.Reload(dir))

	r2, err := e.Evaluate(makeInput("tools/call", "get_file"))
	require.NoError(t, err)
	assert.Equal(t, policy.Deny, r2.Decision)
}

// ── TestInvalidPolicy ─────────────────────────────────────────────────────────

func TestInvalidPolicy(t *testing.T) {
	dir := t.TempDir()
	bad := `package mantismo
this is not valid rego syntax @@@
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "bad.rego"), []byte(bad), 0600))

	_, err := policy.NewEngine(dir)
	require.Error(t, err, "invalid policy should return an error")
}

// ── TestEvalPerformance ───────────────────────────────────────────────────────

func TestEvalPerformance(t *testing.T) {
	e, err := policy.NewEngine(presetDir(t, "balanced"))
	require.NoError(t, err)

	const N = 1000
	start := time.Now()
	for i := 0; i < N; i++ {
		inp := makeInput("tools/call", fmt.Sprintf("get_file_%d", i))
		_, err := e.Evaluate(inp)
		require.NoError(t, err)
	}
	elapsed := time.Since(start)
	assert.Less(t, elapsed.Milliseconds(), int64(500),
		"1000 evaluations should complete in under 500ms, took %v", elapsed)
}
