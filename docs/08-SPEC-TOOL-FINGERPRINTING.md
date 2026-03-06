# 08 — Spec: Tool Description Fingerprinting

## Objective

Detect when MCP server tool descriptions change between sessions (rug-pull defense). This protects against tool poisoning attacks where a malicious update to an MCP server alters tool descriptions to inject harmful prompts.

## Prerequisites

- Spec 05 (MCP Interceptor) complete — hooks into `OnToolsList`

## Background

Tool poisoning is a documented MCP attack vector. An attacker can modify a tool's description to include hidden prompt injection instructions. Since agents rely on tool descriptions to decide when and how to use tools, a poisoned description can cause an agent to exfiltrate data, execute harmful commands, or bypass user intent.

Mantismo detects this by fingerprinting tool descriptions on first use and alerting when they change.

## Interface Contract

### Package: `internal/fingerprint`

```go
// ToolFingerprint represents the stored hash of a tool's definition.
type ToolFingerprint struct {
    Name        string    `json:"name"`
    Hash        string    `json:"hash"`        // SHA-256 of canonical representation
    FirstSeen   time.Time `json:"first_seen"`
    LastSeen    time.Time `json:"last_seen"`
    ServerCmd   string    `json:"server_cmd"`  // The MCP server command (for context)
    Acknowledged bool    `json:"acknowledged"` // User has seen and accepted this version
}

// Store manages persisted fingerprints.
type Store struct {
    path string // ~/.mantismo/fingerprints.json
    mu   sync.Mutex
    data map[string]ToolFingerprint // keyed by tool name
}

// NewStore loads or creates the fingerprint store.
func NewStore(path string) (*Store, error)

// Check compares current tools against stored fingerprints.
// Returns: new tools, changed tools, unchanged tools.
func (s *Store) Check(tools []interceptor.ToolInfo, serverCmd string) (new, changed, unchanged []string)

// Update stores fingerprints for the given tools.
func (s *Store) Update(tools []interceptor.ToolInfo, serverCmd string) error

// Acknowledge marks a changed tool as user-acknowledged.
func (s *Store) Acknowledge(toolName string) error

// computeHash produces a canonical SHA-256 hash of a tool definition.
// Canonical form: sorted JSON of {name, description, inputSchema}.
func computeHash(tool interceptor.ToolInfo) string
```

## Detailed Requirements

### 8.1 Canonical Hashing

To ensure consistent hashes regardless of JSON key ordering:
1. Create a struct: `{name: tool.Name, description: tool.Description, schema: tool.InputSchema}`
2. Marshal to JSON with sorted keys (`json.Marshal` on a struct is deterministic in Go)
3. SHA-256 hash the resulting bytes
4. Hex-encode the hash

### 8.2 First Run Behavior

On the first `tools/list` response for a new MCP server:
1. All tools are "new"
2. Store fingerprints for all tools
3. Print to stderr: `[mantismo] Fingerprinted 12 tools from github-server (first run)`
4. No warnings — everything is baseline

### 8.3 Subsequent Run Behavior

On subsequent `tools/list` responses:
1. Compare each tool against stored fingerprint
2. For each **changed** tool, print warning to stderr:
   ```
   ⚠ TOOL CHANGED: execute_command
     Server: npx @modelcontextprotocol/server-shell
     Previous hash: a1b2c3d4...
     Current hash:  e5f6g7h8...
     Run 'mantismo tools --changed' for details
     Run 'mantismo fingerprint ack execute_command' to accept
   ```
3. For each **new** tool (not seen before), print info to stderr:
   ```
   [mantismo] New tool detected: create_branch (github-server)
   ```
4. Changed tools are logged with `policy_decision: "warning"` in audit log

### 8.4 Changed Tool Behavior

When a tool has changed and is NOT acknowledged:
- The tool still functions (not blocked by default — that's the policy engine's job)
- A warning is logged on every invocation of that tool
- The `mantismo tools --changed` command shows full diff of old vs. new description

When the user acknowledges:
- `mantismo fingerprint ack <tool_name>` updates the stored hash
- Warning stops appearing

### 8.5 Integration with Policy Engine

The fingerprint store exposes a function that the policy engine can query:

```go
// IsToolChanged returns true if the tool's fingerprint differs from stored.
func (s *Store) IsToolChanged(toolName string) bool
```

Policies can use this to block changed tools:
```rego
deny {
    input.tool_changed == true
    not input.tool_acknowledged
}
```

The `balanced` preset warns but allows. The `paranoid` preset blocks unacknowledged changed tools.

### 8.6 Storage Format

`~/.mantismo/fingerprints.json`:
```json
{
  "get_file_contents": {
    "name": "get_file_contents",
    "hash": "a1b2c3d4e5f6...",
    "first_seen": "2026-03-01T10:00:00Z",
    "last_seen": "2026-03-05T14:32:00Z",
    "server_cmd": "npx @modelcontextprotocol/server-filesystem",
    "acknowledged": true
  }
}
```

## Test Plan

1. **TestComputeHash** — Same tool produces same hash; different descriptions produce different hashes
2. **TestFirstRunFingerprints** — All tools stored on first run; Check returns all as "new"
3. **TestUnchangedTools** — Same tools on second run; Check returns all as "unchanged"
4. **TestChangedTool** — Modify one tool's description; Check returns it as "changed"
5. **TestNewToolDetection** — Add a tool that wasn't in the first run; detected as "new"
6. **TestRemovedTool** — Remove a tool; not flagged (silent; could add later)
7. **TestAcknowledge** — Acknowledge a changed tool; subsequent Check returns "unchanged"
8. **TestHashDeterminism** — Same tool definition always produces same hash across runs
9. **TestConcurrentAccess** — Multiple goroutines calling Check/Update simultaneously

## Acceptance Criteria

- [ ] Tool descriptions are hashed on first `tools/list` response
- [ ] Changed tools produce stderr warnings on subsequent sessions
- [ ] New tools are silently fingerprinted with an info message
- [ ] User can acknowledge changes via CLI
- [ ] Fingerprints persist across sessions in JSON file
- [ ] Policy engine can query tool change status
- [ ] Hash computation is deterministic and canonical
- [ ] All 9 tests pass
