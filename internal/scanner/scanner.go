// Copyright 2026 Abid Ali Khan. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

// Package scanner provides regex-based detection of secrets and credentials
// in MCP tool call arguments (outbound) and tool responses (inbound).
package scanner

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// ScanResult describes what was found.
type ScanResult struct {
	Found   bool          // True if any secrets detected
	Matches []SecretMatch // All matches found
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

// placeholderWords are values that look like secrets but are clearly not.
var placeholderWords = []string{
	"your_api_key", "your-api-key", "xxx", "placeholder",
	"example", "changeme", "your_secret", "your_token", "your_password",
	"<api_key>", "<secret>", "<token>", "<password>",
	"your_key_here", "insert_key_here",
}

// isPlaceholder returns true if the value is clearly a placeholder.
func isPlaceholder(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	for _, ph := range placeholderWords {
		if lower == ph {
			return true
		}
	}
	// Check if it contains placeholder-like substrings after the "=" sign.
	if idx := strings.LastIndexAny(lower, "=:"); idx >= 0 {
		val := strings.Trim(lower[idx+1:], " '\"")
		for _, ph := range placeholderWords {
			if val == ph {
				return true
			}
		}
	}
	return false
}

// NewScanner creates a scanner with built-in patterns plus any custom patterns.
func NewScanner(customPatterns []Pattern) *Scanner {
	return NewScannerWithOptions(customPatterns, nil)
}

// NewScannerWithOptions creates a scanner with built-in patterns minus any disabled ones,
// plus any custom patterns. This lets callers implement the disabled_patterns config option.
func NewScannerWithOptions(customPatterns []Pattern, disabledNames []string) *Scanner {
	disabled := make(map[string]bool, len(disabledNames))
	for _, n := range disabledNames {
		disabled[n] = true
	}
	var patterns []Pattern
	for _, p := range builtinPatterns() {
		if !disabled[p.Name] {
			patterns = append(patterns, p)
		}
	}
	patterns = append(patterns, customPatterns...)
	return &Scanner{patterns: patterns}
}

// ScanString checks a string for secrets. The location hint is empty.
func (s *Scanner) ScanString(input string) ScanResult {
	return s.scanStringAtLocation(input, "")
}

// scanStringAtLocation checks a string and tags each match with location.
func (s *Scanner) scanStringAtLocation(input, location string) ScanResult {
	var result ScanResult
	for _, p := range s.patterns {
		matches := p.Regex.FindAllString(input, -1)
		for _, match := range matches {
			if isPlaceholder(match) {
				continue
			}
			if p.Validate != nil && !p.Validate(match) {
				continue
			}
			result.Found = true
			result.Matches = append(result.Matches, SecretMatch{
				PatternName: p.Name,
				Severity:    p.Severity,
				Location:    location,
				Redacted:    redact(p.Name, match),
			})
		}
	}
	return result
}

// ScanJSON recursively checks all string values in a JSON structure.
func (s *Scanner) ScanJSON(raw json.RawMessage) ScanResult {
	var result ScanResult
	s.scanJSONValue(raw, "", &result)
	return result
}

// scanJSONValue recursively traverses a JSON value and scans string leaves.
func (s *Scanner) scanJSONValue(raw json.RawMessage, location string, result *ScanResult) {
	if len(raw) == 0 {
		return
	}

	// Try as string first.
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		loc := location
		if loc == "" {
			loc = "value"
		}
		r := s.scanStringAtLocation(str, loc)
		if r.Found {
			result.Found = true
			result.Matches = append(result.Matches, r.Matches...)
		}
		return
	}

	// Try as object.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err == nil {
		for key, val := range obj {
			childLoc := key
			if location != "" {
				childLoc = location + "." + key
			}
			s.scanJSONValue(val, childLoc, result)
		}
		return
	}

	// Try as array.
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		for i, val := range arr {
			childLoc := fmt.Sprintf("[%d]", i)
			if location != "" {
				childLoc = location + childLoc
			}
			s.scanJSONValue(val, childLoc, result)
		}
		return
	}
	// Number, boolean, null — no secrets possible.
}

// RedactString replaces detected secrets with redacted versions.
func (s *Scanner) RedactString(input string) string {
	result := input
	for _, p := range s.patterns {
		result = p.Regex.ReplaceAllStringFunc(result, func(match string) string {
			if isPlaceholder(match) {
				return match
			}
			if p.Validate != nil && !p.Validate(match) {
				return match
			}
			return redact(p.Name, match)
		})
	}
	return result
}

// RedactJSON returns a copy of the JSON with detected secrets redacted.
func (s *Scanner) RedactJSON(raw json.RawMessage) json.RawMessage {
	result, _ := s.redactJSONValue(raw)
	return result
}

// redactJSONValue recursively redacts secrets in a JSON value.
// Returns the (potentially modified) value and whether any change was made.
func (s *Scanner) redactJSONValue(raw json.RawMessage) (json.RawMessage, bool) {
	if len(raw) == 0 {
		return raw, false
	}

	// Try as string.
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		redacted := s.RedactString(str)
		if redacted != str {
			b, _ := json.Marshal(redacted)
			return b, true
		}
		return raw, false
	}

	// Try as object.
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err == nil {
		changed := false
		for key, val := range obj {
			newVal, c := s.redactJSONValue(val)
			if c {
				obj[key] = newVal
				changed = true
			}
		}
		if changed {
			b, _ := json.Marshal(obj)
			return b, true
		}
		return raw, false
	}

	// Try as array.
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err == nil {
		changed := false
		for i, val := range arr {
			newVal, c := s.redactJSONValue(val)
			if c {
				arr[i] = newVal
				changed = true
			}
		}
		if changed {
			b, _ := json.Marshal(arr)
			return b, true
		}
		return raw, false
	}

	return raw, false
}

// ── Redaction helpers ─────────────────────────────────────────────────────────

// redact chooses the appropriate redaction strategy per pattern type.
func redact(patternName, secret string) string {
	switch {
	case strings.Contains(patternName, "private_key"):
		return "[PRIVATE KEY REDACTED]"
	case patternName == "connection_string" || patternName == "password_in_url":
		return redactConnectionString(secret)
	}
	return redactMiddle(secret)
}

// redactMiddle replaces the middle characters of a secret with asterisks.
// Short secrets (<= 12 chars): `****`
// Longer secrets: keep first 4 and last 4 chars: `AKIA****MPLE`
func redactMiddle(secret string) string {
	n := len(secret)
	if n <= 12 {
		return "****"
	}
	return secret[:4] + "****" + secret[n-4:]
}

// pwdInURLRe matches the password portion of a URL-encoded connection string.
var pwdInURLRe = regexp.MustCompile(`(://[^:@/]+:)[^@]+(@)`)

// redactConnectionString replaces the password portion of a connection string.
// Input:  postgres://user:s3cr3t@host/db
// Output: postgres://user:****@host/db
func redactConnectionString(s string) string {
	return pwdInURLRe.ReplaceAllString(s, "${1}****${2}")
}
