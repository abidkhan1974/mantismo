# Security Policy

## Reporting a Vulnerability

Mantismo is a security tool, so we take vulnerability reports seriously and respond promptly.

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, report them privately via one of these channels:

- **Email:** abidkhan1974@gmail.com
- **GitHub private advisory:** [Report a vulnerability](https://github.com/abidkhan1974/mantismo/security/advisories/new)

Include as much detail as possible:

- A description of the vulnerability and its potential impact
- Steps to reproduce or a proof-of-concept
- The version of Mantismo affected
- Any suggested mitigations (optional)

## What to Expect

| Timeframe | Action |
|-----------|--------|
| Within 48 hours | Acknowledgement of your report |
| Within 7 days | Initial assessment and severity classification |
| Within 30 days | Fix or documented mitigation for confirmed issues |
| After fix | Public disclosure coordinated with you |

We follow responsible disclosure. We will credit reporters in the release notes unless you prefer to remain anonymous.

## Scope

Issues we consider in scope:

- Credential or secret leakage through the proxy layer
- Policy bypass (allowing tool calls that should be blocked)
- Audit log tampering or suppression
- Vault encryption weaknesses
- Remote code execution via malformed MCP messages
- Privilege escalation via the local API

Issues we consider out of scope:

- Vulnerabilities in MCP servers that Mantismo wraps (report those upstream)
- Social engineering attacks
- Denial of service against the local API (no network exposure by default)

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest release | ✅ |
| Older releases | ❌ — please upgrade |
