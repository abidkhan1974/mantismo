package mantismo

import future.keywords.if

# Permissive mode: allow everything except sampling.
default decision := {"decision": "allow", "reason": "permissive mode: all allowed", "rule": "default_allow"}

# Block sampling (LLM-to-LLM calls) even in permissive mode.
decision := {"decision": "deny", "reason": "sampling requests always blocked", "rule": "block_sampling"} if {
	input.method == "sampling/createMessage"
}

# Allow changed tools (log but don't block).
decision := {"decision": "allow", "reason": "tool changed but allowed in permissive mode", "rule": "allow_changed"} if {
	input.tool_changed
}
