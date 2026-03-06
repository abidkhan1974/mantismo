# 13 — Spec: Vault MCP Tools

## Objective

Implement MCP tool handlers that expose vault data to agents through the standard MCP protocol. These tools are injected into the `tools/list` response (via spec 05's tool list augmentation) and handled internally when called (via spec 05's native tool routing).

## Prerequisites

- Spec 05 (Interceptor) — tool list augmentation and native tool routing
- Spec 12 (Vault Storage) — encrypted data store

## Tools to Implement

### 13.1 `vault_get_profile`

Returns the user's profile information, filtered by agent trust level.

**MCP Tool Definition:**
```json
{
  "name": "vault_get_profile",
  "description": "Get the user's profile information (name, email, preferences). Returns fields appropriate to your access level.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "fields": {
        "type": "array",
        "items": {"type": "string"},
        "description": "Specific fields to return (e.g., ['full_name', 'email']). Omit to get all available fields."
      }
    }
  }
}
```

**Behavior:**
1. Query vault for all entries in `profile` category
2. Filter by agent's trust level (determines max sensitivity)
3. If `fields` specified, return only matching fields
4. Return as structured content

**Response format:**
```json
{
  "content": [
    {
      "type": "text",
      "text": "{\"full_name\": \"Abid Khan\", \"email\": \"abid@inferalabs.com\", \"preferred_language\": \"English\"}"
    }
  ]
}
```

### 13.2 `vault_get_preferences`

Returns the user's preferences for a specific domain.

**MCP Tool Definition:**
```json
{
  "name": "vault_get_preferences",
  "description": "Get the user's preferences for a specific category (e.g., travel, dietary, work_style).",
  "inputSchema": {
    "type": "object",
    "properties": {
      "domain": {
        "type": "string",
        "description": "The preference domain (e.g., 'travel', 'dietary', 'work_style', 'communication')"
      }
    },
    "required": ["domain"]
  }
}
```

**Behavior:**
1. Query vault for entries in `preferences` category matching the domain prefix
2. E.g., domain "travel" → keys like "preferences.travel_seat", "preferences.travel_class"
3. Return matching entries

### 13.3 `vault_search_docs`

Semantic search (keyword-based for MVP) across stored documents.

**MCP Tool Definition:**
```json
{
  "name": "vault_search_docs",
  "description": "Search the user's stored documents and notes. Returns matching snippets, not full documents.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "query": {
        "type": "string",
        "description": "Search query (keywords)"
      },
      "limit": {
        "type": "integer",
        "description": "Maximum results to return (default 5)"
      }
    },
    "required": ["query"]
  }
}
```

**Behavior:**
1. Search vault entries in `documents` category using keyword matching
2. Return up to `limit` results with key, label, and a snippet (first 500 chars of value)
3. Do NOT return full document text — agents get snippets and must ask for specifics

### 13.4 `vault_get_masked_id`

Returns masked identifier information (for booking, verification, etc.).

**MCP Tool Definition:**
```json
{
  "name": "vault_get_masked_id",
  "description": "Get masked identifier information. Returns partial info (e.g., last 4 digits) suitable for booking references or verification. Full ID numbers require explicit user approval.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "id_type": {
        "type": "string",
        "description": "Type of identifier (e.g., 'passport', 'drivers_license', 'loyalty_number')"
      }
    },
    "required": ["id_type"]
  }
}
```

**Behavior:**
1. Query vault for entries in `identifiers` category matching id_type
2. Apply masking: return only last 4 digits/chars, expiry dates, issuing authority
3. If agent requests full (unmasked) data, require approval via the Approval Gateway

### 13.5 `vault_list_categories`

List available data categories and field counts (no data returned).

**MCP Tool Definition:**
```json
{
  "name": "vault_list_categories",
  "description": "List the data categories available in the user's vault and how many fields each contains. Use this to discover what information is available before requesting specific data.",
  "inputSchema": {
    "type": "object",
    "properties": {}
  }
}
```

**Response:**
```json
{
  "content": [
    {
      "type": "text",
      "text": "{\"categories\": {\"profile\": 5, \"preferences\": 8, \"identifiers\": 2, \"documents\": 3, \"credentials_meta\": 4}}"
    }
  ]
}
```

## Interface Contract

### Package: `internal/vaulttools`

```go
// Handler processes vault tool calls.
type Handler struct {
    vault    *vault.Vault
    approval *approval.Gateway // For critical-sensitivity access
}

// NewHandler creates a new vault tools handler.
func NewHandler(v *vault.Vault, ag *approval.Gateway) *Handler

// ToolDefinitions returns the MCP tool definitions to inject into tools/list.
func (h *Handler) ToolDefinitions() []interceptor.ToolInfo

// HandleToolCall processes a vault_* tool call and returns the MCP response.
func (h *Handler) HandleToolCall(req interceptor.ToolCallRequest) (json.RawMessage, error)

// IsVaultTool returns true if the tool name is a Mantismo vault tool.
func IsVaultTool(name string) bool
```

## Detailed Requirements

### 13.6 Tool List Injection

When the Interceptor processes a `tools/list` response:
1. If vault is enabled, call `handler.ToolDefinitions()`
2. Append vault tools to the server's tool list
3. Return the augmented list

The agent host sees vault tools alongside server tools, with no way to distinguish them.

### 13.7 Tool Call Routing

When the Interceptor sees a `tools/call` for a `vault_*` tool:
1. Do NOT forward to the upstream MCP server
2. Call `handler.HandleToolCall(req)`
3. Return the response directly to the agent host
4. Log the call through the normal logging pipeline

### 13.8 Trust Levels

For MVP, trust level is determined by the policy preset:
- Paranoid → Untrusted (public data only from vault)
- Balanced → Standard (public + standard)
- Permissive → Trusted (public + standard + sensitive masked)

Full/critical access always requires per-call approval regardless of preset.

### 13.9 Error Handling

If vault is locked (passphrase not provided this session):
```json
{
  "content": [
    {
      "type": "text",
      "text": "Vault is locked. The user needs to unlock it with: mantismo vault unlock"
    }
  ],
  "isError": true
}
```

If requested data doesn't exist:
```json
{
  "content": [
    {
      "type": "text",
      "text": "No data found for the requested category/field."
    }
  ]
}
```

## Test Plan

1. **TestToolDefinitions** — Verify all 5 vault tools are returned with correct schemas
2. **TestGetProfile** — Store profile data, call vault_get_profile, verify response
3. **TestGetProfileFiltered** — Request specific fields, verify only those returned
4. **TestGetPreferences** — Store prefs, call vault_get_preferences with domain filter
5. **TestSearchDocs** — Store documents, search by keyword, verify snippets returned
6. **TestMaskedId** — Store ID, call vault_get_masked_id, verify masking applied
7. **TestListCategories** — Verify category counts match vault contents
8. **TestSensitivityFiltering** — With "balanced" preset, critical data not returned
9. **TestCriticalDataRequiresApproval** — Requesting critical data triggers approval flow
10. **TestVaultLockedError** — Call tool without unlocking vault, verify error response
11. **TestToolListAugmentation** — End-to-end: tools/list response includes vault tools
12. **TestNativeToolRouting** — End-to-end: vault_* call handled locally, not forwarded

## Acceptance Criteria

- [ ] 5 vault tools exposed with correct MCP tool definitions
- [ ] Vault tools appear in tools/list alongside upstream server tools
- [ ] vault_* calls are handled internally (not forwarded to upstream server)
- [ ] Data filtered by sensitivity level based on agent trust
- [ ] Masked IDs show only partial data (last 4)
- [ ] Critical data access triggers approval gateway
- [ ] Locked vault returns helpful error message
- [ ] All 12 tests pass
