package mantismo

import future.keywords.if
import future.keywords.in

# Paranoid mode: all tool calls require human approval by default.
# Uses else-chain so only the highest-priority rule fires (avoids OPA conflicts).

decision := {"decision": "deny", "reason": "tool description changed and not acknowledged", "rule": "block_changed"} if {
	input.tool_changed
	not input.tool_acknowledged
} else := {"decision": "deny", "reason": "sampling requests blocked in paranoid mode", "rule": "block_sampling"} if {
	input.method == "sampling/createMessage"
} else := {"decision": "allow", "reason": "protocol handshake", "rule": "allow_protocol"} if {
	input.method in ["initialize", "shutdown"]
} else := {"decision": "allow", "reason": "notifications always allowed", "rule": "allow_notifications"} if {
	startswith(input.method, "notifications/")
} else := {"decision": "allow", "reason": "tool discovery allowed", "rule": "allow_tools_list"} if {
	input.method == "tools/list"
} else := {"decision": "approve", "reason": "paranoid mode: all tool calls require approval", "rule": "default_approve"}
