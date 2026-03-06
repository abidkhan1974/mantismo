# 09 — Spec: Policy Engine (OPA)

## Objective

Build the policy evaluation engine that decides whether each tool call should be allowed, denied, or require human approval. Uses embedded OPA (Open Policy Agent) with Rego policies. Ships with three presets.

## Prerequisites

- Spec 05 (Interceptor) — hooks into `OnToolCall`
- Spec 08 (Fingerprinting) — provides `IsToolChanged` signal

## Interface Contract

### Package: `internal/policy`

```go
// Decision represents the policy engine's verdict.
type Decision string
const (
    Allow   Decision = "allow"
    Deny    Decision = "deny"
    Approve Decision = "approve" // Requires human approval
)

// EvalResult contains the policy decision and metadata.
type EvalResult struct {
    Decision Decision
    Reason   string   // Human-readable explanation
    Rule     string   // Which Rego rule triggered the decision
}

// EvalInput is the data passed to OPA for evaluation.
type EvalInput struct {
    Method         string          `json:"method"`          // "tools/call", "resources/read", etc.
    ToolName       string          `json:"tool_name"`       // e.g., "get_file_contents"
    Arguments      json.RawMessage `json:"arguments"`       // Raw tool arguments
    ArgumentKeys   []string        `json:"argument_keys"`   // Just the top-level keys
    Direction      string          `json:"direction"`       // "to_server" or "from_server"
    ToolChanged    bool            `json:"tool_changed"`    // From fingerprint store
    ToolAcknowledged bool          `json:"tool_acknowledged"`
    SessionID      string          `json:"session_id"`
    ServerCommand  string          `json:"server_command"`  // e.g., "npx @modelcontextprotocol/server-github"
    Timestamp      string          `json:"timestamp"`       // ISO 8601
}

// Engine wraps the OPA evaluator.
type Engine struct {
    query rego.PreparedEvalQuery
    mu    sync.RWMutex
}

// NewEngine loads policies from the given directory and prepares the OPA evaluator.
func NewEngine(policyDir string) (*Engine, error)

// Evaluate runs the policy against the given input.
func (e *Engine) Evaluate(input EvalInput) (EvalResult, error)

// Reload reloads policies from disk (for hot-reload during development).
func (e *Engine) Reload(policyDir string) error
```

## Detailed Requirements

### 9.1 OPA Embedding

- Use `github.com/open-policy-agent/opa/rego` Go library
- Load all `.rego` files from the policy directory
- Prepare a single query: `data.mantismo.decision`
- The policy must return a JSON object: `{"decision": "allow|deny|approve", "reason": "...", "rule": "..."}`

### 9.2 Policy Evaluation Flow

```
OnToolCall hook fires
       │
       ▼
Build EvalInput from request + fingerprint store
       │
       ▼
engine.Evaluate(input)
       │
       ├── Allow  → InterceptResult{Action: Forward}
       ├── Deny   → InterceptResult{Action: Block, Error: "Blocked by policy: <reason>"}
       └── Approve → InterceptResult{Action: ???} → Approval Gateway (spec 11)
```

### 9.3 Preset: Paranoid (`paranoid.rego`)

```rego
package mantismo

import future.keywords.if
import future.keywords.in

default decision := {"decision": "approve", "reason": "paranoid mode: all tool calls require approval", "rule": "default_approve"}

# Allow notifications passthrough
decision := {"decision": "allow", "reason": "notifications always allowed", "rule": "allow_notifications"} if {
    startswith(input.method, "notifications/")
}

# Allow initialize/shutdown
decision := {"decision": "allow", "reason": "protocol handshake", "rule": "allow_protocol"} if {
    input.method in ["initialize", "shutdown"]
}

# Block changed tools entirely
decision := {"decision": "deny", "reason": "tool description changed and not acknowledged", "rule": "block_changed"} if {
    input.tool_changed
    not input.tool_acknowledged
}

# Block sampling requests
decision := {"decision": "deny", "reason": "sampling requests blocked in paranoid mode", "rule": "block_sampling"} if {
    input.method == "sampling/createMessage"
}
```

### 9.4 Preset: Balanced (`balanced.rego`)

```rego
package mantismo

import future.keywords.if
import future.keywords.in

default decision := {"decision": "allow", "reason": "balanced mode: allowed by default", "rule": "default_allow"}

# Read-only tools: always allow
decision := {"decision": "allow", "reason": "read-only tool", "rule": "allow_reads"} if {
    read_tool_prefixes := ["get_", "list_", "search_", "read_", "fetch_", "show_", "describe_", "vault_get_", "vault_search_"]
    some prefix in read_tool_prefixes
    startswith(input.tool_name, prefix)
}

# Write/mutate tools: require approval
decision := {"decision": "approve", "reason": "write operation requires approval", "rule": "approve_writes"} if {
    write_tool_prefixes := ["create_", "update_", "delete_", "remove_", "push_", "send_", "execute_", "run_", "write_", "modify_"]
    some prefix in write_tool_prefixes
    startswith(input.tool_name, prefix)
}

# Warn on changed tools (allow but log)
decision := {"decision": "approve", "reason": "tool description changed — please verify", "rule": "approve_changed"} if {
    input.tool_changed
    not input.tool_acknowledged
}

# Block sampling
decision := {"decision": "deny", "reason": "sampling requests blocked", "rule": "block_sampling"} if {
    input.method == "sampling/createMessage"
}
```

### 9.5 Preset: Permissive (`permissive.rego`)

```rego
package mantismo

import future.keywords.if

default decision := {"decision": "allow", "reason": "permissive mode: all allowed", "rule": "default_allow"}

# Only block sampling
decision := {"decision": "deny", "reason": "sampling requests always blocked", "rule": "block_sampling"} if {
    input.method == "sampling/createMessage"
}

# Warn on changed tools
decision := {"decision": "allow", "reason": "tool changed but allowed in permissive mode", "rule": "allow_changed"} if {
    input.tool_changed
}
```

### 9.6 Custom Policies

Users can add custom `.rego` files to `~/.mantismo/policies/`. All files are loaded and merged. Custom rules can override preset defaults using higher-priority rule names.

Document in README:
```rego
# Example: Block all access to the shell MCP server
package mantismo

decision := {"decision": "deny", "reason": "shell access disabled", "rule": "custom_block_shell"} if {
    contains(input.server_command, "server-shell")
}
```

### 9.7 Policy Dry-Run

The `mantismo policy check` command replays recent logs:
1. Load current policies
2. Read recent log entries
3. For each tool call entry, build EvalInput and evaluate
4. Compare actual decision (from log) with what current policy would decide
5. Print differences:
   ```
   TOOL CALL                    ACTUAL    CURRENT POLICY    CHANGE
   get_file_contents            allow     allow             (same)
   delete_file                  allow     deny              ⚠ Would now be BLOCKED
   execute_command              allow     approve           ⚠ Would now require APPROVAL
   ```

## Test Plan

1. **TestParanoidBlocksAll** — In paranoid mode, tool calls default to "approve"
2. **TestParanoidAllowsProtocol** — initialize/shutdown are always allowed
3. **TestBalancedAllowsReads** — get_* tools return "allow"
4. **TestBalancedApprovesWrites** — create_* tools return "approve"
5. **TestPermissiveAllowsAll** — Everything returns "allow"
6. **TestAllPresetsBlockSampling** — sampling/createMessage returns "deny" in all presets
7. **TestChangedToolHandling** — Changed+unacknowledged tool triggers correct decision per preset
8. **TestCustomPolicyOverride** — Custom .rego file overrides preset default
9. **TestPolicyReload** — Modify policy file, call Reload, verify new policy applies
10. **TestInvalidPolicy** — Malformed .rego file returns clear error on NewEngine
11. **TestEvalPerformance** — Evaluate 1000 inputs in under 100ms (OPA should be fast)

## Acceptance Criteria

- [ ] Three presets ship with the binary (embedded via Go embed)
- [ ] `mantismo policy init --preset <name>` copies preset to user policy dir
- [ ] Every tool call is evaluated against loaded policies
- [ ] Policy decisions are logged in audit log entries
- [ ] Custom .rego files in policy dir are loaded alongside presets
- [ ] `mantismo policy check` replays logs against current policies
- [ ] OPA evaluation latency < 1ms per call
- [ ] All 11 tests pass
