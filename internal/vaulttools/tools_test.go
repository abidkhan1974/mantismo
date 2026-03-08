package vaulttools_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/abidkhan1974/mantismo/internal/interceptor"
	"github.com/abidkhan1974/mantismo/internal/vault"
	"github.com/abidkhan1974/mantismo/internal/vaulttools"
)

// createTestVault opens a new vault in a temp dir and populates it with test data.
func createTestVault(t *testing.T) *vault.Vault {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.vault")
	v, err := vault.Open(path, "testpass")
	require.NoError(t, err)
	t.Cleanup(func() { _ = v.Close() })

	entries := []vault.Entry{
		// Profile.
		{Key: "name", Value: "Jane Doe", Category: vault.Profile, Sensitivity: vault.Standard, Label: "Name"},
		{Key: "city", Value: "NYC", Category: vault.Profile, Sensitivity: vault.Public, Label: "City"},
		// Identifiers.
		{Key: "ssn", Value: "123-45-6789", Category: vault.Identifiers, Sensitivity: vault.Critical, Label: "SSN"},
		{Key: "phone", Value: "555-123-4567", Category: vault.Identifiers, Sensitivity: vault.Standard, Label: "Phone"},
		// Preferences.
		{Key: "editor.theme", Value: "dark", Category: vault.Preferences, Sensitivity: vault.Public, Label: "Editor Theme"},
		{Key: "notifications.email", Value: "true", Category: vault.Preferences, Sensitivity: vault.Public, Label: "Email Notifications"},
		// Documents.
		{Key: "resume", Value: "Software engineer with 5 years of Go experience", Category: vault.Documents, Sensitivity: vault.Standard, Label: "Resume"},
	}
	for _, e := range entries {
		require.NoError(t, v.Set(e))
	}
	return v
}

// newHandler creates a handler with Trusted level and no approval gateway.
func newHandler(v *vault.Vault) *vaulttools.Handler {
	return vaulttools.NewHandler(v, nil, vaulttools.Trusted)
}

// call is a helper to make a tool call request and decode the JSON result.
func call(t *testing.T, h *vaulttools.Handler, toolName string, args interface{}) map[string]interface{} {
	t.Helper()
	var raw json.RawMessage
	if args != nil {
		b, err := json.Marshal(args)
		require.NoError(t, err)
		raw = json.RawMessage(b)
	} else {
		raw = json.RawMessage(`{}`)
	}
	result, err := h.HandleToolCall(interceptor.ToolCallRequest{
		ToolName:  toolName,
		Arguments: raw,
	})
	require.NoError(t, err)
	var out map[string]interface{}
	require.NoError(t, json.Unmarshal(result, &out))
	return out
}

// callArray decodes the result as a JSON array.
func callArray(t *testing.T, h *vaulttools.Handler, toolName string, args interface{}) []interface{} {
	t.Helper()
	var raw json.RawMessage
	if args != nil {
		b, err := json.Marshal(args)
		require.NoError(t, err)
		raw = json.RawMessage(b)
	} else {
		raw = json.RawMessage(`{}`)
	}
	result, err := h.HandleToolCall(interceptor.ToolCallRequest{
		ToolName:  toolName,
		Arguments: raw,
	})
	require.NoError(t, err)
	var out []interface{}
	require.NoError(t, json.Unmarshal(result, &out))
	return out
}

// TestIsVaultTool verifies the IsVaultTool predicate.
func TestIsVaultTool(t *testing.T) {
	assert.True(t, vaulttools.IsVaultTool("vault_get_profile"))
	assert.True(t, vaulttools.IsVaultTool("vault_search_docs"))
	assert.False(t, vaulttools.IsVaultTool("get_file"))
	assert.False(t, vaulttools.IsVaultTool("read_file"))
}

// TestToolDefinitions verifies that exactly 5 tools are defined with the expected names.
func TestToolDefinitions(t *testing.T) {
	h := vaulttools.NewHandler(nil, nil, vaulttools.Standard)
	defs := h.ToolDefinitions()
	require.Len(t, defs, 5)

	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Name] = true
		assert.NotEmpty(t, d.Description)
		assert.NotEmpty(t, d.InputSchema)
	}
	assert.True(t, names["vault_get_profile"])
	assert.True(t, names["vault_get_preferences"])
	assert.True(t, names["vault_search_docs"])
	assert.True(t, names["vault_get_masked_id"])
	assert.True(t, names["vault_list_categories"])
}

// TestHandleVaultGetProfile verifies that vault_get_profile returns profile entries.
func TestHandleVaultGetProfile(t *testing.T) {
	v := createTestVault(t)
	h := newHandler(v)

	out := call(t, h, "vault_get_profile", nil)
	require.NotNil(t, out)
	assert.Equal(t, "Jane Doe", out["name"])
	assert.Equal(t, "NYC", out["city"])
}

// TestHandleVaultGetProfileFields verifies field-level filtering.
func TestHandleVaultGetProfileFields(t *testing.T) {
	v := createTestVault(t)
	h := newHandler(v)

	out := call(t, h, "vault_get_profile", map[string]interface{}{
		"fields": []string{"name"},
	})
	require.NotNil(t, out)
	assert.Equal(t, "Jane Doe", out["name"])
	_, hasCity := out["city"]
	assert.False(t, hasCity, "city should not be returned when only 'name' is requested")
}

// TestHandleVaultGetPreferences verifies that vault_get_preferences returns all preferences.
func TestHandleVaultGetPreferences(t *testing.T) {
	v := createTestVault(t)
	h := newHandler(v)

	out := call(t, h, "vault_get_preferences", nil)
	require.NotNil(t, out)
	assert.Equal(t, "dark", out["editor.theme"])
	assert.Equal(t, "true", out["notifications.email"])
}

// TestHandleVaultGetPreferencesDomain verifies domain-prefix filtering.
func TestHandleVaultGetPreferencesDomain(t *testing.T) {
	v := createTestVault(t)
	h := newHandler(v)

	out := call(t, h, "vault_get_preferences", map[string]interface{}{
		"domain": "editor",
	})
	require.NotNil(t, out)
	assert.Equal(t, "dark", out["editor.theme"])
	_, hasNotif := out["notifications.email"]
	assert.False(t, hasNotif, "notifications.email should not be returned for domain 'editor'")
}

// TestHandleVaultSearchDocs verifies search returns document snippets.
func TestHandleVaultSearchDocs(t *testing.T) {
	v := createTestVault(t)
	h := newHandler(v)

	arr := callArray(t, h, "vault_search_docs", map[string]interface{}{
		"query": "Go experience",
	})
	require.NotEmpty(t, arr)

	first, ok := arr[0].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "resume", first["key"])
	snippet, _ := first["snippet"].(string)
	assert.LessOrEqual(t, len(snippet), 500)
	assert.Contains(t, snippet, "Go experience")
}

// TestHandleVaultSearchDocsNoResults verifies empty array when no matches.
func TestHandleVaultSearchDocsNoResults(t *testing.T) {
	v := createTestVault(t)
	h := newHandler(v)

	arr := callArray(t, h, "vault_search_docs", map[string]interface{}{
		"query": "xyzzy_no_match_abc",
	})
	assert.Empty(t, arr)
}

// TestHandleVaultGetMaskedId verifies that standard identifiers are returned masked.
func TestHandleVaultGetMaskedId(t *testing.T) {
	v := createTestVault(t)
	// Use Standard trust so critical (SSN) is not exposed.
	h := vaulttools.NewHandler(v, nil, vaulttools.Standard)

	out := call(t, h, "vault_get_masked_id", nil)
	require.NotNil(t, out)

	// Phone (standard sensitivity) should be present and masked.
	phone, hasPhone := out["phone"].(string)
	require.True(t, hasPhone, "phone should be present for Standard trust")
	// Last 4 digits of "555-123-4567" are "4567"
	assert.Equal(t, "***-***-4567", phone, "phone should be masked showing last 4 digits")

	// SSN is critical — should NOT be present with Standard trust.
	_, hasSSN := out["ssn"]
	assert.False(t, hasSSN, "ssn (critical) should not be returned with Standard trust")
}

// TestHandleVaultListCategories verifies that category counts are returned.
func TestHandleVaultListCategories(t *testing.T) {
	v := createTestVault(t)
	h := newHandler(v)

	out := call(t, h, "vault_list_categories", nil)
	require.NotNil(t, out)

	// We set: 2 profile, 2 identifiers, 2 preferences, 1 document.
	profileCount, _ := out["profile"].(float64)
	assert.Equal(t, float64(2), profileCount, "expected 2 profile entries")

	prefsCount, _ := out["preferences"].(float64)
	assert.Equal(t, float64(2), prefsCount, "expected 2 preferences entries")

	docsCount, _ := out["documents"].(float64)
	assert.Equal(t, float64(1), docsCount, "expected 1 documents entry")
}

// TestLockedVault verifies that a nil vault returns an appropriate error message.
func TestLockedVault(t *testing.T) {
	h := vaulttools.NewHandler(nil, nil, vaulttools.Standard)

	result, err := h.HandleToolCall(interceptor.ToolCallRequest{
		ToolName:  "vault_get_profile",
		Arguments: json.RawMessage(`{}`),
	})
	require.NoError(t, err)

	var out map[string]interface{}
	require.NoError(t, json.Unmarshal(result, &out))
	errMsg, _ := out["error"].(string)
	assert.Contains(t, errMsg, "vault is locked")
}

// TestUnknownVaultTool verifies that unknown tool names return an error.
func TestUnknownVaultTool(t *testing.T) {
	v := createTestVault(t)
	h := newHandler(v)

	result, err := h.HandleToolCall(interceptor.ToolCallRequest{
		ToolName:  "vault_does_not_exist",
		Arguments: json.RawMessage(`{}`),
	})
	require.NoError(t, err)

	var out map[string]interface{}
	require.NoError(t, json.Unmarshal(result, &out))
	errMsg, _ := out["error"].(string)
	assert.Contains(t, errMsg, "unknown vault tool")
}
