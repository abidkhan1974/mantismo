# 17 — Threat Model

## Overview

This document catalogs the threats Mantismo defends against, threats it partially mitigates, and threats that remain out of scope. This is a reference document — it informs design decisions across all specs but produces no code.

## Assets to Protect

1. **User's personal data** (vault contents: IDs, credentials metadata, documents, preferences)
2. **User's MCP server credentials** (API keys, OAuth tokens used by MCP servers)
3. **User's computing environment** (filesystem, network, installed software)
4. **User's agent session integrity** (preventing agents from being manipulated)

## Threat Actors

| Actor | Motivation | Capability |
|---|---|---|
| Malicious MCP server author | Data theft, cryptomining | Publishes poisoned tool descriptions, malicious code in server updates |
| Prompt injection attacker | Data exfiltration | Crafts content (emails, web pages, documents) that manipulates agents |
| Compromised agent runtime | Credential theft | Exploits agent vulnerability to steal tokens or data |
| Network attacker (future) | MITM, data interception | Intercepts HTTP+SSE transport (not applicable for stdio MVP) |

## Threats and Mitigations

### T1: Tool Poisoning / Rug Pull

**Attack:** MCP server updates tool descriptions to inject hidden prompt injection instructions.
**Impact:** Agent follows hidden instructions, exfiltrates data or executes harmful actions.
**Mantismo mitigation:** Tool fingerprinting (spec 08) detects description changes and alerts the user. Paranoid policy blocks changed tools entirely.
**Residual risk:** First-run tools are trusted without history. User must verify initial tool descriptions.

### T2: Credential Leakage via Tool Arguments

**Attack:** Agent accidentally includes API keys, tokens, or passwords in tool call arguments.
**Impact:** Credentials exposed to MCP server (which may log them) or visible in transit.
**Mantismo mitigation:** Secret scanner (spec 10) detects common credential patterns and blocks outbound calls containing them.
**Residual risk:** Novel credential formats not matching known patterns will pass through.

### T3: Credential Leakage via Tool Responses

**Attack:** MCP server returns data containing credentials (e.g., reading a .env file).
**Impact:** Credentials enter the agent's context window and may be included in future requests.
**Mantismo mitigation:** Secret scanner detects credentials in responses and logs warnings. Paranoid mode can redact before forwarding.
**Residual risk:** The data has already left the server; Mantismo can only prevent further spread.

### T4: Sampling Abuse

**Attack:** Malicious MCP server sends `sampling/createMessage` to make the agent perform actions on its behalf.
**Impact:** Server-controlled prompt injection via the MCP protocol itself.
**Mantismo mitigation:** All presets block sampling requests by default.
**Residual risk:** None for this vector (sampling is fully blocked).

### T5: Over-Privileged Agent Actions

**Attack:** Agent performs write/delete/execute operations the user didn't intend.
**Impact:** Data loss, unauthorized changes, system compromise.
**Mantismo mitigation:** Policy engine (spec 09) classifies tools as read/write. Balanced preset requires approval for write operations. Paranoid preset requires approval for everything.
**Residual risk:** Tools with misleading names (e.g., "get_file" that actually deletes) bypass name-based classification. Fingerprinting helps but isn't foolproof.

### T6: Cross-Server Data Exfiltration

**Attack:** Agent reads sensitive data from Server A (e.g., email) and sends it to Server B (e.g., webhook).
**Impact:** Data leakage across trust boundaries.
**Mantismo mitigation:** Partial. Audit logging captures all cross-server data flow. Future: context isolation per server (preventing data from one server's response being used in another's request). MVP does not implement context isolation.
**Residual risk:** High in MVP. The agent's context window contains data from all servers, and Mantismo cannot control what the agent does with it outside MCP.

### T7: Vault Compromise (Local)

**Attack:** Malware on the user's machine reads the vault database.
**Impact:** All personal data exposed.
**Mantismo mitigation:** SQLCipher encryption with user passphrase. Data encrypted at rest.
**Residual risk:** If malware has read access to memory while vault is open, it can extract decrypted data. Mantismo assumes the local machine is trusted at the OS level.

### T8: Approval Fatigue

**Attack:** Attacker triggers many legitimate-looking approval requests, hoping user rubber-stamps a malicious one.
**Impact:** User approves a harmful action they intended to deny.
**Mantismo mitigation:** Risk scoring (future) to highlight unusual requests. Session grants to reduce prompt frequency. Clear display of what's being approved.
**Residual risk:** Fundamental UX challenge. Users will develop click-through habits.

### T9: Supply Chain Attack on Mantismo Itself

**Attack:** Attacker compromises Mantismo binary or dependencies.
**Impact:** All proxy traffic interceptable; vault data accessible.
**Mantismo mitigation:** Open source (verifiable). Signed releases with checksums. Minimal dependencies. GoReleaser reproducible builds.
**Residual risk:** Dependency compromise (Go modules) remains a risk for any software.

### T10: Prompt Injection via Tool Responses

**Attack:** Attacker embeds prompt injection instructions in data that MCP tools return (e.g., a README file with hidden instructions).
**Impact:** Agent follows hidden instructions from the returned data.
**Mantismo mitigation:** Partial. Response scanning can detect known injection patterns. But sophisticated injections are indistinguishable from normal text.
**Residual risk:** High. This is fundamentally unsolved in the industry. Mantismo raises the bar but cannot fully prevent it.

## Threat Matrix Summary

| Threat | Severity | Mantismo Coverage | Residual Risk |
|--------|----------|---------------------|---------------|
| T1: Tool Poisoning | Critical | Strong (fingerprinting) | Low |
| T2: Cred Leak (outbound) | High | Strong (scanner) | Low |
| T3: Cred Leak (inbound) | Medium | Moderate (scan + warn) | Medium |
| T4: Sampling Abuse | Critical | Full (blocked) | None |
| T5: Over-Privileged Actions | High | Strong (policy) | Low |
| T6: Cross-Server Exfil | High | Weak (logging only) | High |
| T7: Vault Local Compromise | Medium | Strong (encryption) | Low |
| T8: Approval Fatigue | Medium | Moderate (UX design) | Medium |
| T9: Supply Chain | Critical | Moderate (OSS + signing) | Medium |
| T10: Prompt Injection | Critical | Weak (pattern match) | High |

## Out of Scope for MVP

- Network-level attacks (stdio transport has no network surface)
- Physical access attacks
- Side-channel attacks on encryption
- Browser/OS-level security
- Agent runtime sandboxing (Mantismo operates at the MCP protocol level, not the process level)
