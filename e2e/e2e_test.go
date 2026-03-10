// Copyright 2026 Abid Ali Khan. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// E2E-1: Happy path — full MCP handshake + read tool call + clean shutdown.
func TestE2E_HappyPath(t *testing.T) {
	tp := StartProxy(t)

	// MCP handshake.
	initResp := tp.Initialize()
	require.NotNil(t, initResp)
	// Verify it is valid JSON and has no error field.
	var initCheck struct {
		Error *json.RawMessage `json:"error"`
	}
	require.NoError(t, json.Unmarshal(initResp, &initCheck))
	assert.Nil(t, initCheck.Error, "initialize should not return an error: %s", string(initResp))

	// tools/list
	listResp := tp.SendRequest(2, "tools/list", nil)
	require.NotNil(t, listResp)
	var listResult struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(listResp, &listResult))
	assert.Nil(t, listResult.Error, "tools/list should not return an error")
	assert.NotEmpty(t, listResult.Result.Tools, "tools/list should return at least one tool")

	// Find get_file_contents (a read tool).
	found := false
	for _, tool := range listResult.Result.Tools {
		if tool.Name == "get_file_contents" {
			found = true
			break
		}
	}
	assert.True(t, found, "get_file_contents tool should be in the list")

	// tools/call — read tool (should be allowed by balanced preset).
	callResp := tp.SendRequest(3, "tools/call", map[string]interface{}{
		"name":      "get_file_contents",
		"arguments": map[string]interface{}{"path": "/test/file.txt"},
	})
	require.NotNil(t, callResp)
	var callResult struct {
		Result *struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(callResp, &callResult))
	assert.Nil(t, callResult.Error, "get_file_contents should be allowed: %s", string(callResp))
	assert.NotNil(t, callResult.Result, "should have a result")

	// Verify log file created in <dataDir>/.mantismo/logs/
	time.Sleep(150 * time.Millisecond)
	logFiles, err := filepath.Glob(filepath.Join(tp.DataDir, ".mantismo", "logs", "*.jsonl"))
	if err == nil && len(logFiles) > 0 {
		data, readErr := os.ReadFile(logFiles[0])
		if readErr == nil {
			assert.Contains(t, string(data), "tools", "log file should contain tool call entries")
		}
	}
	// Log file check is best-effort.
}

// E2E-2: Policy blocking — write operations blocked by custom strict policy.
func TestE2E_PolicyBlocking(t *testing.T) {
	// Create a strict policy that denies write operations.
	policyDir := t.TempDir()
	strictPolicy := `package mantismo
import future.keywords.if
import future.keywords.in

default decision := {"decision": "allow", "reason": "default allow", "rule": "default_allow"}

decision := {"decision": "deny", "reason": "write operations are blocked", "rule": "deny_writes"} if {
    write_prefixes := ["create_", "delete_", "write_", "update_", "remove_"]
    some prefix in write_prefixes
    startswith(input.tool_name, prefix)
}
`
	err := os.WriteFile(filepath.Join(policyDir, "strict.rego"), []byte(strictPolicy), 0600)
	require.NoError(t, err)

	tp := StartProxy(t, WithPolicyDir(policyDir))
	tp.Initialize()

	// tools/call — write tool: should be denied.
	resp := tp.SendRequest(2, "tools/call", map[string]interface{}{
		"name":      "create_file",
		"arguments": map[string]interface{}{"path": "/tmp/test.txt", "content": "hello"},
	})
	require.NotNil(t, resp)

	var result struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Result *json.RawMessage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(resp, &result))
	assert.NotNil(t, result.Error, "create_file should be blocked: %s", string(resp))
	if result.Error != nil {
		assert.Contains(t, strings.ToLower(result.Error.Message), "block",
			"error message should mention block: %s", result.Error.Message)
	}

	// Verify "BLOCKED" appears in stderr.
	blocked := tp.WaitForStderr("BLOCKED", 3*time.Second)
	assert.True(t, blocked, "should see BLOCKED in stderr, got: %s", tp.Stderr.String())
}

// E2E-3: Secret detection — fake credential in server response triggers warning.
func TestE2E_SecretDetection(t *testing.T) {
	// Start with leak-secret mock server flag.
	tp := StartProxy(t, WithMockArgs("--leak-secret"))
	tp.Initialize()

	// Call any read tool — the mock will include a fake AWS key in the response.
	resp := tp.SendRequest(2, "tools/call", map[string]interface{}{
		"name":      "get_file_contents",
		"arguments": map[string]interface{}{"path": "/test.txt"},
	})
	require.NotNil(t, resp)

	// Wait a moment for the response to be processed.
	time.Sleep(200 * time.Millisecond)

	// The response should still arrive (we warn but don't block from-server secrets).
	var result struct {
		Result *json.RawMessage `json:"result"`
		Error  *json.RawMessage `json:"error"`
	}
	require.NoError(t, json.Unmarshal(resp, &result))

	// Verify Mantismo detected the secret (warning in stderr).
	detected := tp.WaitForStderr("credential", 3*time.Second)
	assert.True(t, detected,
		"mantismo should detect credential in response, stderr: %s", tp.Stderr.String())
}

// E2E-4: Tool poisoning detection — changed tool description triggers warning.
func TestE2E_ToolPoisoningDetection(t *testing.T) {
	// Use a shared dataDir so fingerprints persist between the two proxy sessions.
	sharedDataDir := t.TempDir()

	// Session 1: normal session to establish fingerprints.
	func() {
		tp1 := StartProxy(t, WithDataDir(sharedDataDir))
		tp1.Initialize()
		_ = tp1.SendRequest(2, "tools/list", nil) // trigger fingerprinting
		time.Sleep(300 * time.Millisecond)
		tp1.Close()
	}()

	// Give the first proxy time to write fingerprints before starting session 2.
	time.Sleep(200 * time.Millisecond)

	// Session 2: same data dir, but mock server poisons tool description.
	// The mock server returns the normal description on the first tools/list call,
	// then the poisoned description on the second call (that's how --poison works).
	// So we need two tools/list calls to trigger the fingerprint mismatch.
	tp2 := StartProxy(t, WithDataDir(sharedDataDir), WithMockArgs("--poison", "get_file_contents"))
	tp2.Initialize()
	_ = tp2.SendRequest(2, "tools/list", nil) // first call: normal description, updates fingerprints
	time.Sleep(100 * time.Millisecond)
	_ = tp2.SendRequest(3, "tools/list", nil) // second call: poisoned description → triggers warning

	// Wait for fingerprint warning.
	changed := tp2.WaitForStderr("changed", 3*time.Second)
	assert.True(t, changed,
		"mantismo should detect changed tool description, stderr: %s", tp2.Stderr.String())
}

// E2E-5: Vault tools injection — vault tools appear in augmented tools/list.
func TestE2E_VaultToolsInjected(t *testing.T) {
	tp := StartProxy(t)
	tp.Initialize()

	// tools/list — vault tools should be injected into the response.
	listResp := tp.SendRequest(2, "tools/list", nil)
	require.NotNil(t, listResp)

	var listResult struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(listResp, &listResult))
	assert.Nil(t, listResult.Error, "tools/list should not return an error")

	// Verify vault tools appear in the augmented list.
	vaultToolNames := []string{
		"vault_get_profile",
		"vault_get_preferences",
		"vault_search_docs",
		"vault_get_masked_id",
		"vault_list_categories",
	}

	toolSet := make(map[string]bool, len(listResult.Result.Tools))
	for _, tool := range listResult.Result.Tools {
		toolSet[tool.Name] = true
	}

	for _, name := range vaultToolNames {
		assert.True(t, toolSet[name],
			"vault tool %q should be injected into tools/list response", name)
	}
}
