package mantismo

import future.keywords.if
import future.keywords.in

default decision := {"decision": "allow", "reason": "balanced mode: allowed by default", "rule": "default_allow"}

# Read-only tools: always allow (unless the tool description has changed)
decision := {"decision": "allow", "reason": "read-only tool", "rule": "allow_reads"} if {
	read_tool_prefixes := ["get_", "list_", "search_", "read_", "fetch_", "show_", "describe_", "vault_get_", "vault_search_"]
	some prefix in read_tool_prefixes
	startswith(input.tool_name, prefix)
	not input.tool_changed
}

# Write/mutate tools: require approval (unless the tool description has changed — approve_changed takes precedence)
decision := {"decision": "approve", "reason": "write operation requires approval", "rule": "approve_writes"} if {
	write_tool_prefixes := ["create_", "update_", "delete_", "remove_", "push_", "send_", "execute_", "run_", "write_", "modify_"]
	some prefix in write_tool_prefixes
	startswith(input.tool_name, prefix)
	not input.tool_changed
}

# Warn on changed tools (allow but require approval)
decision := {"decision": "approve", "reason": "tool description changed — please verify", "rule": "approve_changed"} if {
	input.tool_changed
	not input.tool_acknowledged
}

# Block sampling
decision := {"decision": "deny", "reason": "sampling requests blocked", "rule": "block_sampling"} if {
	input.method == "sampling/createMessage"
}
