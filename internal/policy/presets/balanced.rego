package mantismo

import future.keywords.if
import future.keywords.in

# Balanced mode: allow by default, approve writes, block sampling.
# Uses else-chain so only the highest-priority rule fires (avoids OPA conflicts).

decision := {"decision": "deny", "reason": "sampling requests blocked", "rule": "block_sampling"} if {
	input.method == "sampling/createMessage"
} else := {"decision": "allow", "reason": "protocol handshake", "rule": "allow_protocol"} if {
	input.method in ["initialize", "shutdown"]
} else := {"decision": "allow", "reason": "notifications always allowed", "rule": "allow_notifications"} if {
	startswith(input.method, "notifications/")
} else := {"decision": "approve", "reason": "write operation requires approval", "rule": "approve_writes"} if {
	write_prefixes := ["create_", "update_", "delete_", "remove_", "push_", "send_", "execute_", "run_", "write_", "modify_"]
	some prefix in write_prefixes
	startswith(input.tool_name, prefix)
} else := {"decision": "approve", "reason": "tool description changed — please verify", "rule": "approve_changed"} if {
	input.tool_changed
	not input.tool_acknowledged
} else := {"decision": "allow", "reason": "read-only tool", "rule": "allow_reads"} if {
	read_prefixes := ["get_", "list_", "search_", "read_", "fetch_", "show_", "describe_", "vault_get_", "vault_search_"]
	some prefix in read_prefixes
	startswith(input.tool_name, prefix)
} else := {"decision": "allow", "reason": "balanced mode: allowed by default", "rule": "default_allow"}
