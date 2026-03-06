package mantismo

import future.keywords.if

default decision := {"decision": "allow", "reason": "permissive mode: all allowed", "rule": "default_allow"}

# Only block sampling
decision := {"decision": "deny", "reason": "sampling requests always blocked", "rule": "block_sampling"} if {
	input.method == "sampling/createMessage"
}

# Warn on changed tools (still allow in permissive mode)
decision := {"decision": "allow", "reason": "tool changed but allowed in permissive mode", "rule": "allow_changed"} if {
	input.tool_changed
}
