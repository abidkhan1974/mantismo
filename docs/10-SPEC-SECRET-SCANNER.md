# 10 — Spec: Secret Scanner

## Objective

Scan outbound tool call arguments and inbound tool responses for accidentally leaked secrets (API keys, tokens, passwords, private keys). Block outbound leaks; warn and redact inbound leaks.

## Prerequisites

- Spec 05 (Interceptor) — hooks into `OnToolCall` (arguments) and `OnToolCallResponse` (responses)

## Interface Contract

### Package: `internal/scanner`

```go
// ScanResult describes what was found.
type ScanResult struct {
    Found    bool           // True if any secrets detected
    Matches  []SecretMatch  // All matches found
}

// SecretMatch describes a single detected secret.
type SecretMatch struct {
    PatternName string // e.g., "aws_access_key", "github_token"
    Severity    string // "critical", "high", "medium"
    Location    string // "arguments.api_key", "response.content[0].text"
    Redacted    string // The matched value with middle chars replaced: "AKIA****EXAMPLE"
}

// Scanner checks strings for secret patterns.
type Scanner struct {
    patterns []Pattern
}

// Pattern defines a secret detection rule.
type Pattern struct {
    Name     string
    Severity string
    Regex    *regexp.Regexp
    Validate func(match string) bool // Optional secondary validation (reduces false positives)
}

// NewScanner creates a scanner with built-in patterns plus any custom patterns.
func NewScanner(customPatterns []Pattern) *Scanner

// ScanString checks a string for secrets.
func (s *Scanner) ScanString(input string) ScanResult

// ScanJSON recursively checks all string values in a JSON structure.
func (s *Scanner) ScanJSON(raw json.RawMessage) ScanResult

// RedactString replaces detected secrets with redacted versions.
func (s *Scanner) RedactString(input string) string

// RedactJSON returns a copy of the JSON with detected secrets redacted.
func (s *Scanner) RedactJSON(raw json.RawMessage) json.RawMessage
```

## Built-in Patterns

### Critical Severity

| Pattern Name | Regex | Validation |
|---|---|---|
| `aws_access_key` | `AKIA[0-9A-Z]{16}` | Length check |
| `aws_secret_key` | `(?i)aws_secret_access_key\s*[:=]\s*[A-Za-z0-9/+=]{40}` | — |
| `private_key` | `-----BEGIN (RSA\|EC\|DSA\|OPENSSH) PRIVATE KEY-----` | — |
| `github_token_fine` | `github_pat_[a-zA-Z0-9_]{82}` | Prefix check |
| `github_token_classic` | `ghp_[a-zA-Z0-9]{36}` | Prefix check |

### High Severity

| Pattern Name | Regex | Validation |
|---|---|---|
| `generic_api_key` | `(?i)(api[_-]?key\|apikey)\s*[:=]\s*['"]?[a-zA-Z0-9_\-]{20,}` | Min length 20 |
| `generic_secret` | `(?i)(secret\|password\|passwd\|pwd)\s*[:=]\s*['"]?[^\s'"]{8,}` | Min length 8 |
| `jwt_token` | `eyJ[a-zA-Z0-9_-]+\.eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+` | 3-part structure |
| `bearer_token` | `(?i)bearer\s+[a-zA-Z0-9_\-.]+` | — |
| `slack_token` | `xox[bporas]-[a-zA-Z0-9-]+` | Prefix check |
| `stripe_key` | `(sk\|pk)_(test\|live)_[a-zA-Z0-9]{24,}` | Prefix check |

### Medium Severity

| Pattern Name | Regex | Validation |
|---|---|---|
| `password_in_url` | `://[^:]+:[^@]+@` | Contains :// and @ |
| `connection_string` | `(?i)(mongodb\|postgres\|mysql\|redis)://[^\s]+` | Protocol check |
| `base64_private_key` | `(?i)(private.?key\|secret)\s*[:=]\s*[A-Za-z0-9+/]{40,}={0,2}` | Base64 validity |

## Detailed Requirements

### 10.1 Outbound Scanning (Arguments)

Hook into `OnToolCall`:
1. Extract `arguments` from the tool call request
2. Run `ScanJSON(arguments)`
3. If critical or high severity match found:
   - Return `InterceptResult{Action: Block}` with error: "Blocked: potential secret detected in arguments (<pattern_name>)"
   - Log the blocked call with `redacted: true`
4. If medium severity match found:
   - Log a warning but allow the call
   - Set `redacted: true` in log entry

### 10.2 Inbound Scanning (Responses)

Hook into `OnToolCallResponse`:
1. Extract `content` from the tool response
2. Run `ScanJSON(content)`
3. If any match found:
   - **Do NOT block** (the data has already left the server; blocking just hides it from the agent)
   - Log a warning with severity and pattern name
   - Set `redacted: true` in log entry
   - Optionally: in `paranoid` mode, redact the matched values before forwarding to host

### 10.3 Redaction Format

Replace middle characters of detected secrets:
- Short secrets (<= 12 chars): `****`
- Longer secrets: keep first 4 and last 4 chars: `AKIA****MPLE`
- Private keys: `[PRIVATE KEY REDACTED]`
- Connection strings: replace password portion only: `postgres://user:****@host/db`

### 10.4 False Positive Reduction

- Validation functions provide secondary checks (e.g., AWS keys must be exactly 20 chars after AKIA prefix)
- Ignore values that are clearly placeholders: "YOUR_API_KEY", "xxx", "placeholder", "example"
- Ignore values inside common non-sensitive fields: "description", "help_text", "error_message"
- User can disable specific patterns in config:
  ```toml
  [scanner]
  disabled_patterns = ["generic_secret", "password_in_url"]
  ```

### 10.5 Custom Patterns

Users can add patterns in config:
```toml
[[scanner.custom_patterns]]
name = "internal_token"
severity = "high"
regex = "INT_[a-f0-9]{32}"
```

## Test Plan

1. **TestAWSKeyDetection** — Detect AKIA keys in various positions within JSON
2. **TestGitHubTokenDetection** — Detect ghp_ and github_pat_ tokens
3. **TestPrivateKeyDetection** — Detect PEM-encoded private keys
4. **TestJWTDetection** — Detect JWT tokens in arguments
5. **TestGenericSecretDetection** — Detect password= and api_key= patterns
6. **TestConnectionStringDetection** — Detect postgres:// and mongodb:// URIs with passwords
7. **TestNoFalsePositivesOnPlaceholders** — "YOUR_API_KEY" is not flagged
8. **TestRedactionFormat** — Verify redacted output format for various secret types
9. **TestRedactJSON** — Verify JSON structure preserved with secrets redacted
10. **TestOutboundBlocking** — Tool call with secret in arguments is blocked
11. **TestInboundWarning** — Tool response with secret is warned but not blocked
12. **TestDisabledPattern** — Disabled pattern does not trigger
13. **TestCustomPattern** — Custom regex pattern detects matches
14. **TestPerformance** — Scan 1MB JSON payload in under 50ms

## Acceptance Criteria

- [ ] Outbound tool calls with critical/high severity secrets are blocked
- [ ] Inbound responses with secrets are warned and logged
- [ ] 13 built-in patterns cover common secret formats
- [ ] Redaction preserves enough context for debugging without exposing secrets
- [ ] False positive rate is low (placeholders, common field names excluded)
- [ ] Users can disable patterns and add custom patterns
- [ ] Scanning is fast enough to not add perceptible latency
- [ ] All 14 tests pass
