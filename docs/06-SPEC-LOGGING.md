# 06 — Spec: Structured Audit Logging

## Objective

Build the audit logging subsystem that records every MCP message to JSONL files with structured metadata. This is the foundation for the CLI `logs` command, the dashboard, and forensic analysis.

## Prerequisites

- Spec 05 (MCP Interceptor) complete — Logger hooks into `OnAnyMessage`

## Interface Contract

### Package: `internal/logger`

```go
// LogEntry represents a single audit log record.
type LogEntry struct {
    Timestamp    time.Time       `json:"ts"`
    SessionID    string          `json:"session_id"`    // Unique per proxy invocation
    Direction    string          `json:"dir"`           // "to_server" or "from_server"
    MessageType  string          `json:"msg_type"`      // "request", "response", "notification", "error"
    Method       string          `json:"method"`        // MCP method (empty for responses)
    RequestID    *json.RawMessage `json:"request_id,omitempty"`
    ToolName     string          `json:"tool,omitempty"`     // For tools/call only
    ResourceURI  string          `json:"resource,omitempty"` // For resources/read only
    PolicyDecision string        `json:"policy,omitempty"`   // "allow", "deny", "approve", "" (if no policy eval)
    Redacted     bool            `json:"redacted"`           // True if secrets were redacted
    DurationMs   *float64        `json:"duration_ms,omitempty"` // Response time (for responses only)
    ErrorCode    *int            `json:"error_code,omitempty"`
    ErrorMessage string          `json:"error_msg,omitempty"`
    Summary      string          `json:"summary"`      // Human-readable one-liner
    RawSize      int             `json:"raw_bytes"`     // Size of original message in bytes
}

// Logger writes structured audit logs to JSONL files.
type Logger struct {
    dir       string       // Log directory (e.g., ~/.mantismo/logs/)
    sessionID string
    file      *os.File     // Current day's log file
    mu        sync.Mutex   // Protects file writes
    currentDay string      // "2026-03-05" — triggers rotation
}

// New creates a new Logger.
func New(dir string, sessionID string) (*Logger, error)

// Log writes a single LogEntry. Thread-safe.
func (l *Logger) Log(entry LogEntry) error

// Close flushes and closes the current log file.
func (l *Logger) Close() error

// Query reads log entries matching the given filter.
func Query(dir string, filter QueryFilter) ([]LogEntry, error)

// QueryFilter specifies which log entries to return.
type QueryFilter struct {
    Since      *time.Time // Only entries after this time
    Until      *time.Time // Only entries before this time
    SessionID  string     // Filter by session
    Method     string     // Filter by MCP method
    ToolName   string     // Filter by tool name
    Decision   string     // Filter by policy decision
    Limit      int        // Max entries to return (0 = unlimited)
}

// SessionFromLogs returns a helper for correlating requests with responses
// and computing durations.
type RequestTracker struct {
    pending sync.Map // map[string]time.Time — tracks request send times by ID
}

func NewRequestTracker() *RequestTracker
func (t *RequestTracker) TrackRequest(id json.RawMessage)
func (t *RequestTracker) CompleteRequest(id json.RawMessage) *float64 // returns duration_ms or nil
```

## Detailed Requirements

### 6.1 File Management

- Log directory: configurable, default `~/.mantismo/logs/`
- File naming: `YYYY-MM-DD.jsonl` (one file per UTC day)
- Rotation: when the UTC date changes, close current file and open new one
- Append-only: never modify or truncate existing log files
- File permissions: 0600 (owner read/write only)
- Create directory if it doesn't exist (with 0700 permissions)

### 6.2 Entry Generation

The Logger hooks into the Interceptor's `OnAnyMessage`:

```go
interceptor.Hooks{
    OnAnyMessage: func(msg MCPMessage, dir Direction) {
        entry := buildLogEntry(msg, dir, sessionID, tracker)
        logger.Log(entry)
    },
}
```

`buildLogEntry` populates:
- `Timestamp`: `time.Now().UTC()`
- `SessionID`: generated once per proxy invocation (UUID v4)
- `Direction`: from `dir` parameter
- `MessageType`: from `msg.IsRequest`, `msg.IsNotification`, etc.
- `Method`: from `msg.Method` (for requests) or correlated original request (for responses)
- `ToolName`: parsed from `params.name` if method is `tools/call`
- `Summary`: human-readable summary, e.g., "tools/call get_file_contents → 200 OK (45ms)"
- `RawSize`: `len(msg.Raw)`

### 6.3 Duration Tracking

- When a request is seen going ToServer, record its ID and timestamp in RequestTracker
- When the corresponding response comes FromServer, compute duration = now - request_time
- Attach duration_ms to the response's LogEntry

### 6.4 Summary Generation

Generate a concise one-liner for each entry:
- Request: `"→ tools/call get_file_contents {path: /etc/hosts}"`
- Response: `"← tools/call get_file_contents → OK (45ms, 1.2KB)"`
- Error: `"← tools/call get_file_contents → ERROR -32602: Invalid params"`
- Notification: `"→ notifications/initialized"`
- Blocked: `"✕ tools/call delete_file DENIED by policy"`

Argument summaries should be truncated to 80 chars max.

### 6.5 Sensitive Data Handling in Logs

- Do NOT log raw message bodies by default (they may contain secrets)
- Log only: method, tool name, argument keys (not values), response status, size
- Optionally log full bodies when `log_level` is `debug` (with a warning in config docs)

### 6.6 Query Interface

`Query()` reads JSONL files from the log directory, applies filters, and returns matching entries. Implementation:
- List files matching `YYYY-MM-DD.jsonl` pattern
- Filter files by date range (Since/Until)
- Read matching files line by line, parse each into LogEntry
- Apply remaining filters (session, method, tool, decision)
- Return up to Limit entries (most recent first)

## Test Plan

1. **TestLogWritesToFile** — Log an entry, verify JSONL file created with correct name
2. **TestLogEntryFormat** — Verify a logged entry round-trips through JSON marshal/unmarshal
3. **TestDayRotation** — Log entries across a day boundary, verify two files created
4. **TestFilePermissions** — Verify log files are created with 0600 permissions
5. **TestDurationTracking** — Track request, complete it, verify duration_ms populated
6. **TestSummaryGeneration** — Verify summary strings for request, response, error, notification
7. **TestQueryByTimeRange** — Create logs across multiple days, query with Since/Until
8. **TestQueryByToolName** — Filter logs by tool name
9. **TestQueryLimit** — Verify Limit caps results
10. **TestConcurrentLogging** — Log from 10 goroutines simultaneously, verify no corruption

## Acceptance Criteria

- [ ] Every MCP message produces a JSONL log entry
- [ ] Log files are date-partitioned and append-only
- [ ] Duration tracking correlates requests with responses
- [ ] Human-readable summaries are generated for all message types
- [ ] Raw message bodies are NOT logged by default
- [ ] Query interface filters by time, session, method, tool, and decision
- [ ] Concurrent writes are safe (no interleaving or corruption)
- [ ] All 10 tests pass
