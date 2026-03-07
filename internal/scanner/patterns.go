// Package scanner — patterns.go defines built-in secret detection regex patterns.
package scanner

import (
	"regexp"
	"strings"
)

// builtinPatterns returns the 13 built-in secret detection patterns.
func builtinPatterns() []Pattern {
	return []Pattern{
		// ── Critical ─────────────────────────────────────────────────────────────

		{
			Name:     "aws_access_key",
			Severity: "critical",
			Regex:    regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
			Validate: func(match string) bool { return len(match) == 20 },
		},
		{
			Name:     "aws_secret_key",
			Severity: "critical",
			Regex:    regexp.MustCompile(`(?i)aws_secret_access_key\s*[:=]\s*[A-Za-z0-9/+=]{40}`),
		},
		{
			Name:     "private_key",
			Severity: "critical",
			Regex:    regexp.MustCompile(`-----BEGIN (RSA|EC|DSA|OPENSSH) PRIVATE KEY-----`),
		},
		{
			Name:     "github_token_fine",
			Severity: "critical",
			Regex:    regexp.MustCompile(`github_pat_[a-zA-Z0-9_]{82}`),
			Validate: func(match string) bool { return strings.HasPrefix(match, "github_pat_") },
		},
		{
			Name:     "github_token_classic",
			Severity: "critical",
			Regex:    regexp.MustCompile(`ghp_[a-zA-Z0-9]{36}`),
			Validate: func(match string) bool { return strings.HasPrefix(match, "ghp_") },
		},

		// ── High ─────────────────────────────────────────────────────────────────

		{
			Name:     "generic_api_key",
			Severity: "high",
			Regex:    regexp.MustCompile(`(?i)(api[_-]?key|apikey)\s*[:=]\s*['"]?[a-zA-Z0-9_\-]{20,}`),
			Validate: func(match string) bool { return len(match) >= 20 },
		},
		{
			Name:     "generic_secret",
			Severity: "high",
			Regex:    regexp.MustCompile(`(?i)(secret|password|passwd|pwd)\s*[:=]\s*['"]?[^\s'"]{8,}`),
			Validate: func(match string) bool { return len(match) >= 8 },
		},
		{
			Name:     "jwt_token",
			Severity: "high",
			Regex:    regexp.MustCompile(`eyJ[a-zA-Z0-9_-]+\.eyJ[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+`),
			Validate: func(match string) bool {
				return len(strings.Split(match, ".")) == 3
			},
		},
		{
			Name:     "bearer_token",
			Severity: "high",
			Regex:    regexp.MustCompile(`(?i)bearer\s+[a-zA-Z0-9_\-.]+`),
		},
		{
			Name:     "slack_token",
			Severity: "high",
			Regex:    regexp.MustCompile(`xox[bporas]-[a-zA-Z0-9-]+`),
			Validate: func(match string) bool { return len(match) > 5 },
		},
		{
			Name:     "stripe_key",
			Severity: "high",
			Regex:    regexp.MustCompile(`(sk|pk)_(test|live)_[a-zA-Z0-9]{24,}`),
			Validate: func(match string) bool { return len(match) > 10 },
		},

		// ── Medium ───────────────────────────────────────────────────────────────

		{
			Name:     "password_in_url",
			Severity: "medium",
			Regex:    regexp.MustCompile(`://[^:@\s]+:[^@\s]+@`),
			Validate: func(match string) bool {
				return strings.Contains(match, "://") && strings.Contains(match, "@")
			},
		},
		{
			Name:     "connection_string",
			Severity: "medium",
			Regex:    regexp.MustCompile(`(?i)(mongodb|postgres|mysql|redis)://[^\s]+`),
			Validate: func(match string) bool {
				return strings.Contains(match, "://")
			},
		},
		{
			Name:     "base64_private_key",
			Severity: "medium",
			Regex:    regexp.MustCompile(`(?i)(private.?key|secret)\s*[:=]\s*[A-Za-z0-9+/]{40,}={0,2}`),
			Validate: func(match string) bool { return len(match) >= 40 },
		},
	}
}
