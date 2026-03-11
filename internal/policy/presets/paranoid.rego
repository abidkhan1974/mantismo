package mantismo

import future.keywords.if
import future.keywords.in

# Paranoid mode: all tool calls require human approval by default.
default decision := {"decision": "approve", "reason": "paranoid mode: all tool calls require approval", "rule": "default_approve"}

# Protocol handshakes always pass through.
decision := {"decision": "allow", "reason": "protocol handshake", "rule": "allow_protocol"} if {
	input.method in ["initialize", "shutdown"]
}

# Notifications always pass through.
decision := {"decision": "allow", "reason": "notifications always allowed", "rule": "allow_notifications"} if {
	startswith(input.method, "notifications/")
}

# Tool-list fetches always pass through (read-only metadata).
decision := {"decision": "allow", "reason": "tool discovery allowed", "rule": "allow_tools_list"} if {
	input.method == "tools/list"
}

# Block tools whose description has changed and not been acknowledged.
decision := {"decision": "deny", "reason": "tool description changed and not acknowledged", "rule": "block_changed"} if {
	input.tool_changed
	not input.tool_acknowledged
}

# Block sampling (LLM-to-LLM calls) entirely.
decision := {"decision": "deny", "reason": "sampling requests blocked in paranoid mode", "rule": "block_sampling"} if {
	input.method == "sampling/createMessage"
}
