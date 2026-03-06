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
