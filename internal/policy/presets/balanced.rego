package mantismo

import future.keywords.if
import future.keywords.in

# Balanced mode: allow by default, approve writes, block sampling.
default decision := {"decision": "allow", "reason": "balanced mode: allowed by default", "rule": "default_allow"}

# Protocol handshakes always pass through.
decision := {"decision": "allow", "reason": "protocol handshake", "rule": "allow_protocol"} if {
	input.method in ["initialize", "shutdown"]
}

# Notifications always pass through.
decision := {"decision": "allow", "reason": "notifications always allowed", "rule": "allow_notifications"} if {
	startswith(input.method, "notifications/")
}

# Read-only tools: always allow.
decision := {"decision": "allow", "reason": "read-only tool", "rule": "allow_reads"} if {
	read_prefixes := ["get_", "list_", "search_", "read_", "fetch_", "show_", "describe_", "vault_get_", "vault_search_"]
	some prefix in read_prefixes
	startswith(input.tool_name, prefix)
	not input.tool_changed
}

# Write/mutate tools: require human approval.
decision := {"decision": "approve", "reason": "write operation requires approval", "rule": "approve_writes"} if {
	write_prefixes := ["create_", "update_", "delete_", "remove_", "push_", "send_", "execute_", "run_", "write_", "modify_"]
	some prefix in write_prefixes
	startswith(input.tool_name, prefix)
}

# Changed+unacknowledged tools require approval.
decision := {"decision": "approve", "reason": "tool description changed — please verify", "rule": "approve_changed"} if {
	input.tool_changed
	not input.tool_acknowledged
}

# Block sampling (LLM-to-LLM calls).
decision := {"decision": "deny", "reason": "sampling requests blocked", "rule": "block_sampling"} if {
	input.method == "sampling/createMessage"
}
