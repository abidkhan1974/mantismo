// Package logger writes structured JSONL audit logs to ~/.mantismo/logs/,
// one file per UTC day, with automatic rotation and thread-safe writes.
package logger

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// LogEntry represents a single audit log record.
type LogEntry struct {
	Timestamp      time.Time        `json:"ts"`
	SessionID      string           `json:"session_id"`
	Direction      string           `json:"dir"`      // "to_server" or "from_server"
	MessageType    string           `json:"msg_type"` // "request", "response", "notification", "error"
	Method         string           `json:"method"`   // MCP method (empty for pure responses)
	RequestID      *json.RawMessage `json:"request_id,omitempty"`
	ToolName       string           `json:"tool,omitempty"`     // for tools/call
	ResourceURI    string           `json:"resource,omitempty"` // for resources/read
	PolicyDecision string           `json:"policy,omitempty"`   // "allow", "deny", "approve"
	Redacted       bool             `json:"redacted"`
	DurationMs     *float64         `json:"duration_ms,omitempty"`
	ErrorCode      *int             `json:"error_code,omitempty"`
	ErrorMessage   string           `json:"error_msg,omitempty"`
	Summary        string           `json:"summary"`
	RawSize        int              `json:"raw_bytes"`
}

// Logger writes structured audit logs to JSONL files (one per UTC day).
type Logger struct {
	dir        string
	sessionID  string
	file       *os.File
	mu         sync.Mutex
	currentDay string // "2006-01-02"
}

// New creates a new Logger that writes to dir.
// The directory is created (0700) if it does not exist.
func New(dir string, sessionID string) (*Logger, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("logger: create log dir: %w", err)
	}
	l := &Logger{
		dir:       dir,
		sessionID: sessionID,
	}
	// Open the file for today immediately so we catch permission errors early.
	if err := l.openFile(time.Now().UTC()); err != nil {
		return nil, err
	}
	return l, nil
}

// Log writes a single LogEntry as a JSONL line. Thread-safe.
func (l *Logger) Log(entry LogEntry) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now().UTC()
	day := now.Format("2006-01-02")

	// Rotate when the UTC day has changed.
	if day != l.currentDay {
		if l.file != nil {
			_ = l.file.Close()
			l.file = nil
		}
		if err := l.openFile(now); err != nil {
			return err
		}
	}

	b, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("logger: marshal entry: %w", err)
	}
	b = append(b, '\n')

	if _, err := l.file.Write(b); err != nil {
		return fmt.Errorf("logger: write entry: %w", err)
	}
	return nil
}

// Close flushes and closes the current log file.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}

// openFile opens (or creates) the JSONL file for the given day.
// Must be called with l.mu held.
func (l *Logger) openFile(t time.Time) error {
	day := t.Format("2006-01-02")
	name := filepath.Join(l.dir, day+".jsonl")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600) //nolint:gosec
	if err != nil {
		return fmt.Errorf("logger: open %s: %w", name, err)
	}
	l.file = f
	l.currentDay = day
	return nil
}

// ── Query interface ───────────────────────────────────────────────────────────

// QueryFilter specifies which log entries to return from Query.
type QueryFilter struct {
	Since     *time.Time
	Until     *time.Time
	SessionID string
	Method    string
	ToolName  string
	Decision  string
	Limit     int // 0 = unlimited
}

// Query reads log entries from dir that match filter.
// Entries are returned in reverse-chronological order (most recent first).
func Query(dir string, filter QueryFilter) ([]LogEntry, error) {
	// List all YYYY-MM-DD.jsonl files.
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("logger: read log dir: %w", err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		day := strings.TrimSuffix(name, ".jsonl")
		if _, err := time.Parse("2006-01-02", day); err != nil {
			continue
		}

		// Filter files by date range.
		if filter.Since != nil {
			dayEnd, _ := time.Parse("2006-01-02", day)
			dayEnd = dayEnd.Add(24 * time.Hour)
			if dayEnd.Before(*filter.Since) {
				continue
			}
		}
		if filter.Until != nil {
			dayStart, _ := time.Parse("2006-01-02", day)
			if dayStart.After(*filter.Until) {
				continue
			}
		}
		files = append(files, filepath.Join(dir, name))
	}

	// Sort files in reverse order so most recent entries come first.
	sort.Sort(sort.Reverse(sort.StringSlice(files)))

	var result []LogEntry

	for _, path := range files {
		batch, err := readJSONLFile(path, filter)
		if err != nil {
			continue // skip unreadable files
		}
		result = append(result, batch...)
		if filter.Limit > 0 && len(result) >= filter.Limit {
			result = result[:filter.Limit]
			return result, nil
		}
	}

	return result, nil
}

// readJSONLFile reads and filters entries from a single JSONL file.
func readJSONLFile(path string, filter QueryFilter) ([]LogEntry, error) {
	f, err := os.Open(path) //nolint:gosec
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Collect lines in reverse (most recent last in file → prepend or reverse).
	var lines [][]byte
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20) // 1 MB per line
	for sc.Scan() {
		line := make([]byte, len(sc.Bytes()))
		copy(line, sc.Bytes())
		lines = append(lines, line)
	}

	// Reverse so most-recent entries come first.
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}

	var result []LogEntry
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var e LogEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		if !matchesFilter(e, filter) {
			continue
		}
		result = append(result, e)
	}
	return result, nil
}

// matchesFilter returns true if the entry satisfies all filter criteria.
func matchesFilter(e LogEntry, f QueryFilter) bool {
	if f.Since != nil && e.Timestamp.Before(*f.Since) {
		return false
	}
	if f.Until != nil && e.Timestamp.After(*f.Until) {
		return false
	}
	if f.SessionID != "" && e.SessionID != f.SessionID {
		return false
	}
	if f.Method != "" && e.Method != f.Method {
		return false
	}
	if f.ToolName != "" && e.ToolName != f.ToolName {
		return false
	}
	if f.Decision != "" && e.PolicyDecision != f.Decision {
		return false
	}
	return true
}

// ── Duration tracking ─────────────────────────────────────────────────────────

// RequestTracker correlates outgoing requests with their responses to compute
// round-trip durations.
type RequestTracker struct {
	pending sync.Map // map[string]time.Time
}

// NewRequestTracker creates a new RequestTracker.
func NewRequestTracker() *RequestTracker {
	return &RequestTracker{}
}

// TrackRequest records the send time for the given request ID.
func (t *RequestTracker) TrackRequest(id json.RawMessage) {
	t.pending.Store(string(id), time.Now())
}

// CompleteRequest returns the elapsed duration in milliseconds since the request
// was tracked, and removes it from the tracker. Returns nil if not tracked.
func (t *RequestTracker) CompleteRequest(id json.RawMessage) *float64 {
	key := string(id)
	val, ok := t.pending.LoadAndDelete(key)
	if !ok {
		return nil
	}
	sent := val.(time.Time) //nolint:forcetypeassert
	ms := float64(time.Since(sent).Nanoseconds()) / 1e6
	return &ms
}

// ── Summary generation ────────────────────────────────────────────────────────

// BuildSummary creates a human-readable one-liner for a log entry.
func BuildSummary(dir, msgType, method, toolName string, durationMs *float64, rawSize int, isError bool, errCode *int, errMsg string) string {
	arrow := "→"
	if dir == "from_server" {
		arrow = "←"
	}

	switch msgType {
	case "notification":
		return fmt.Sprintf("%s %s", arrow, method)
	case "request":
		if toolName != "" {
			return fmt.Sprintf("%s %s %s", arrow, method, truncate(toolName, 60))
		}
		return fmt.Sprintf("%s %s", arrow, method)
	case "response":
		if isError && errCode != nil {
			return fmt.Sprintf("%s %s → ERROR %d: %s", arrow, method, *errCode, truncate(errMsg, 60))
		}
		if durationMs != nil {
			kb := float64(rawSize) / 1024.0
			return fmt.Sprintf("%s %s → OK (%.0fms, %.1fKB)", arrow, method, *durationMs, kb)
		}
		return fmt.Sprintf("%s %s → OK", arrow, method)
	case "error":
		if errCode != nil {
			return fmt.Sprintf("%s %s → ERROR %d: %s", arrow, method, *errCode, truncate(errMsg, 60))
		}
		return fmt.Sprintf("%s %s → ERROR: %s", arrow, method, truncate(errMsg, 60))
	}
	return fmt.Sprintf("%s %s", arrow, method)
}

// truncate clips a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
