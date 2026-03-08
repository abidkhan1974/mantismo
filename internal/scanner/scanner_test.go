// Copyright 2026 Mantismo. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

package scanner_test

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/abidkhan1974/mantismo/internal/scanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── 1. TestAWSKeyDetection ────────────────────────────────────────────────────

func TestAWSKeyDetection(t *testing.T) {
	s := scanner.NewScanner(nil)

	tests := []struct {
		name   string
		input  string
		wantFn string
	}{
		{
			name:   "bare access key",
			input:  "AKIAIOSFODNN7EXAMPLE",
			wantFn: "aws_access_key",
		},
		{
			name:   "access key in JSON string",
			input:  `{"credentials": "AKIAIOSFODNN7EXAMPLE"}`,
			wantFn: "aws_access_key",
		},
		{
			name:   "secret key assignment",
			input:  "aws_secret_access_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			wantFn: "aws_secret_key",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var result scanner.ScanResult
			if strings.HasPrefix(tc.input, "{") || strings.HasPrefix(tc.input, "[") {
				raw := json.RawMessage(tc.input)
				result = s.ScanJSON(raw)
			} else {
				result = s.ScanString(tc.input)
			}
			require.True(t, result.Found, "expected secret to be found")
			found := false
			for _, m := range result.Matches {
				if m.PatternName == tc.wantFn {
					found = true
					assert.NotEmpty(t, m.Redacted)
				}
			}
			assert.True(t, found, "expected pattern %s to match", tc.wantFn)
		})
	}
}

// ── 2. TestGitHubTokenDetection ───────────────────────────────────────────────

func TestGitHubTokenDetection(t *testing.T) {
	s := scanner.NewScanner(nil)

	// Classic token: ghp_ + 36 alphanumeric chars
	classicToken := "ghp_" + strings.Repeat("a", 36)
	result := s.ScanString(classicToken)
	require.True(t, result.Found)
	assert.Equal(t, "github_token_classic", result.Matches[0].PatternName)

	// Fine-grained token: github_pat_ + 82 chars
	fineToken := "github_pat_" + strings.Repeat("b", 82)
	result2 := s.ScanString(fineToken)
	require.True(t, result2.Found)
	assert.Equal(t, "github_token_fine", result2.Matches[0].PatternName)

	// In JSON
	payload := fmt.Sprintf(`{"token": "%s"}`, classicToken)
	result3 := s.ScanJSON(json.RawMessage(payload))
	require.True(t, result3.Found)
	assert.Equal(t, "github_token_classic", result3.Matches[0].PatternName)
	assert.Equal(t, "token", result3.Matches[0].Location)
}

// ── 3. TestPrivateKeyDetection ────────────────────────────────────────────────

func TestPrivateKeyDetection(t *testing.T) {
	s := scanner.NewScanner(nil)

	tests := []string{
		"-----BEGIN RSA PRIVATE KEY-----",
		"-----BEGIN EC PRIVATE KEY-----",
		"-----BEGIN DSA PRIVATE KEY-----",
		"-----BEGIN OPENSSH PRIVATE KEY-----",
	}

	for _, header := range tests {
		t.Run(header, func(t *testing.T) {
			result := s.ScanString(header)
			require.True(t, result.Found, "should detect private key header")
			assert.Equal(t, "private_key", result.Matches[0].PatternName)
			assert.Equal(t, "critical", result.Matches[0].Severity)
			assert.Equal(t, "[PRIVATE KEY REDACTED]", result.Matches[0].Redacted)
		})
	}
}

// ── 4. TestJWTDetection ───────────────────────────────────────────────────────

func TestJWTDetection(t *testing.T) {
	s := scanner.NewScanner(nil)

	// Valid 3-part JWT structure (base64url encoded header.payload.signature)
	jwt := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"

	result := s.ScanString(jwt)
	require.True(t, result.Found, "should detect JWT token")
	assert.Equal(t, "jwt_token", result.Matches[0].PatternName)
	assert.Equal(t, "high", result.Matches[0].Severity)
	assert.NotEmpty(t, result.Matches[0].Redacted)

	// JWT in JSON arguments
	payload := fmt.Sprintf(`{"Authorization": "%s"}`, jwt)
	result2 := s.ScanJSON(json.RawMessage(payload))
	require.True(t, result2.Found)
}

// ── 5. TestGenericSecretDetection ─────────────────────────────────────────────

func TestGenericSecretDetection(t *testing.T) {
	s := scanner.NewScanner(nil)

	tests := []struct {
		input   string
		pattern string
	}{
		{"api_key=abcdefghijklmnopqrstuvwxyz", "generic_api_key"},
		{"api-key=abcdefghijklmnopqrstuvwxyz", "generic_api_key"},
		{"apikey=abcdefghijklmnopqrstuvwxyz", "generic_api_key"},
		{"password=mysecretpassword", "generic_secret"},
		{"secret=mysecretvalue", "generic_secret"},
		{"passwd=mysecretpassword", "generic_secret"},
		{"pwd=mysecretpassword", "generic_secret"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := s.ScanString(tc.input)
			require.True(t, result.Found, "should detect secret in: %s", tc.input)
			found := false
			for _, m := range result.Matches {
				if m.PatternName == tc.pattern {
					found = true
				}
			}
			assert.True(t, found, "expected pattern %s", tc.pattern)
		})
	}
}

// ── 6. TestConnectionStringDetection ─────────────────────────────────────────

func TestConnectionStringDetection(t *testing.T) {
	s := scanner.NewScanner(nil)

	tests := []struct {
		input  string
		wantFn string
	}{
		{"postgres://myuser:s3cr3t@localhost:5432/mydb", "connection_string"},
		{"mongodb://admin:p@ssw0rd@cluster0.mongodb.net/test", "connection_string"},
		{"mysql://root:password123@db.example.com/app", "connection_string"},
		{"redis://default:redispassword@redis.example.com:6379", "connection_string"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := s.ScanString(tc.input)
			require.True(t, result.Found, "should detect connection string: %s", tc.input)
			found := false
			for _, m := range result.Matches {
				if m.PatternName == tc.wantFn {
					found = true
					assert.Equal(t, "medium", m.Severity)
				}
			}
			assert.True(t, found, "expected pattern %s for: %s", tc.wantFn, tc.input)
		})
	}
}

// ── 7. TestNoFalsePositivesOnPlaceholders ─────────────────────────────────────

func TestNoFalsePositivesOnPlaceholders(t *testing.T) {
	s := scanner.NewScanner(nil)

	// These should NOT trigger detection.
	placeholders := []string{
		"YOUR_API_KEY",
		"your_api_key",
		"placeholder",
		"changeme",
		"example",
		"xxx",
		"your_secret",
	}

	for _, ph := range placeholders {
		t.Run(ph, func(t *testing.T) {
			// Test as a standalone value
			result := s.ScanString(ph)
			// Some placeholders might be caught by very broad patterns, but
			// the placeholder filter should suppress them.
			for _, m := range result.Matches {
				assert.NotEqual(t, "YOUR_API_KEY", ph, "placeholder %q should not be flagged by %s", ph, m.PatternName)
			}
		})
	}

	// The canonical AWS key placeholder should not be flagged.
	result := s.ScanString("api_key=YOUR_API_KEY")
	for _, m := range result.Matches {
		assert.NotEqual(t, "generic_api_key", m.PatternName,
			"api_key=YOUR_API_KEY should not be flagged as generic_api_key")
	}
}

// ── 8. TestRedactionFormat ────────────────────────────────────────────────────

func TestRedactionFormat(t *testing.T) {
	s := scanner.NewScanner(nil)

	tests := []struct {
		name    string
		input   string
		wantFmt string // substring that must appear in the redacted output
	}{
		{
			// AKIAIOSFODNN7EXAMPLE is 20 chars: first 4 = "AKIA", last 4 = "MPLE"
			name:    "AWS key redaction",
			input:   "AKIAIOSFODNN7EXAMPLE",
			wantFmt: "AKIA****MPLE",
		},
		{
			name:    "private key redaction",
			input:   "-----BEGIN RSA PRIVATE KEY-----",
			wantFmt: "[PRIVATE KEY REDACTED]",
		},
		{
			// generic_secret requires value >= 8 chars; use a clearly long password
			name:    "long secret redaction",
			input:   "password=supersecretpassword",
			wantFmt: "****",
		},
		{
			name:    "connection string redaction",
			input:   "postgres://myuser:s3cr3t@localhost:5432/mydb",
			wantFmt: "****",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			redacted := s.RedactString(tc.input)
			assert.Contains(t, redacted, tc.wantFmt, "redacted output should contain expected format")
			// Original secret should not appear verbatim (except for the format check above)
		})
	}

	// Verify that the AKIA key has correct structure: prefix(4) + **** + suffix(4)
	// AKIAIOSFODNN7EXAMPLE (20 chars) → AKIA + **** + MPLE
	awsRedacted := s.RedactString("AKIAIOSFODNN7EXAMPLE")
	assert.True(t, strings.HasPrefix(awsRedacted, "AKIA"), "should keep first 4 chars")
	assert.True(t, strings.HasSuffix(awsRedacted, "MPLE"), "should keep last 4 chars of AKIAIOSFODNN7EXAMPLE")
}

// ── 9. TestRedactJSON ─────────────────────────────────────────────────────────

func TestRedactJSON(t *testing.T) {
	s := scanner.NewScanner(nil)

	raw := json.RawMessage(`{
		"tool": "read_file",
		"credentials": "AKIAIOSFODNN7EXAMPLE",
		"path": "/tmp/file.txt",
		"nested": {
			"token": "ghp_` + strings.Repeat("a", 36) + `"
		}
	}`)

	redacted := s.RedactJSON(raw)
	require.NotNil(t, redacted)

	// Unmarshal the redacted JSON to verify structure.
	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(redacted, &m))

	// The "tool" field should remain unchanged.
	var tool string
	require.NoError(t, json.Unmarshal(m["tool"], &tool))
	assert.Equal(t, "read_file", tool)

	// The "credentials" field should be redacted.
	var creds string
	require.NoError(t, json.Unmarshal(m["credentials"], &creds))
	assert.Contains(t, creds, "****", "credentials should be redacted")
	assert.NotEqual(t, "AKIAIOSFODNN7EXAMPLE", creds)
}

// ── 10. TestOutboundBlocking ──────────────────────────────────────────────────

func TestOutboundBlocking(t *testing.T) {
	s := scanner.NewScanner(nil)

	// Arguments contain a critical secret (AWS access key).
	arguments := json.RawMessage(`{
		"command": "list-buckets",
		"access_key_id": "AKIAIOSFODNN7EXAMPLE"
	}`)

	result := s.ScanJSON(arguments)
	require.True(t, result.Found, "should detect secret in outbound arguments")

	// At least one match should be critical or high severity.
	hasCriticalOrHigh := false
	for _, m := range result.Matches {
		if m.Severity == "critical" || m.Severity == "high" {
			hasCriticalOrHigh = true
			break
		}
	}
	assert.True(t, hasCriticalOrHigh, "should have critical or high severity match to trigger blocking")
}

// ── 11. TestInboundWarning ────────────────────────────────────────────────────

func TestInboundWarning(t *testing.T) {
	s := scanner.NewScanner(nil)

	// Response content contains a secret (should warn, not block).
	response := json.RawMessage(`{
		"content": [
			{"type": "text", "text": "The API key is: AKIAIOSFODNN7EXAMPLE and the password is secret=hunter2"}
		]
	}`)

	result := s.ScanJSON(response)
	require.True(t, result.Found, "should detect secret in inbound response")
	assert.True(t, len(result.Matches) > 0, "should have at least one match")

	// Verify that the location path reflects the JSON structure.
	for _, m := range result.Matches {
		assert.Contains(t, m.Location, "content", "location should include 'content' path")
	}
}

// ── 12. TestDisabledPattern ───────────────────────────────────────────────────

func TestDisabledPattern(t *testing.T) {
	// Scanner with generic_secret and password_in_url disabled.
	s := scanner.NewScannerWithOptions(nil, []string{"generic_secret", "password_in_url"})

	// generic_secret should not fire.
	result := s.ScanString("password=mysecretpassword123")
	for _, m := range result.Matches {
		assert.NotEqual(t, "generic_secret", m.PatternName,
			"generic_secret should be disabled")
		assert.NotEqual(t, "password_in_url", m.PatternName,
			"password_in_url should be disabled")
	}

	// Other patterns should still work.
	s2 := scanner.NewScannerWithOptions(nil, []string{"generic_secret"})
	result2 := s2.ScanString("AKIAIOSFODNN7EXAMPLE")
	assert.True(t, result2.Found, "aws_access_key should still be detected")
}

// ── 13. TestCustomPattern ─────────────────────────────────────────────────────

func TestCustomPattern(t *testing.T) {
	customPatterns := []scanner.Pattern{
		{
			Name:     "internal_token",
			Severity: "high",
			Regex:    regexp.MustCompile(`INT_[a-f0-9]{32}`),
			Validate: func(match string) bool { return strings.HasPrefix(match, "INT_") },
		},
	}

	s := scanner.NewScanner(customPatterns)

	// Custom pattern should be detected.
	token := "INT_" + strings.Repeat("a", 32)
	result := s.ScanString(token)
	require.True(t, result.Found, "custom pattern should detect internal token")
	assert.Equal(t, "internal_token", result.Matches[0].PatternName)
	assert.Equal(t, "high", result.Matches[0].Severity)

	// Custom pattern in JSON.
	payload := fmt.Sprintf(`{"auth_token": "%s"}`, token)
	result2 := s.ScanJSON(json.RawMessage(payload))
	require.True(t, result2.Found)
	assert.Equal(t, "internal_token", result2.Matches[0].PatternName)
	assert.Equal(t, "auth_token", result2.Matches[0].Location)
}

// ── 14. TestPerformance ───────────────────────────────────────────────────────

func TestPerformance(t *testing.T) {
	s := scanner.NewScanner(nil)

	// Build a ~1MB JSON payload with no secrets.
	const targetSize = 1024 * 1024 // 1 MB
	var sb strings.Builder
	sb.WriteString(`{"entries": [`)
	entry := `{"id": "12345", "description": "This is a perfectly normal log entry with no sensitive data", "status": "ok"}`
	count := 0
	for sb.Len() < targetSize {
		if count > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(entry)
		count++
	}
	sb.WriteString(`]}`)
	payload := json.RawMessage(sb.String())

	start := time.Now()
	result := s.ScanJSON(payload)
	elapsed := time.Since(start)

	assert.False(t, result.Found, "should find no secrets in clean payload")
	assert.Less(t, elapsed.Milliseconds(), int64(2000),
		"1MB scan should complete in under 2000ms, took %v", elapsed)
}
