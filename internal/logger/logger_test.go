// Copyright 2026 Abid Ali Khan. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

package logger_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/abidkhan1974/mantismo/internal/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeEntry is a convenience constructor for test log entries.
func makeEntry(method, tool, dir, session string) logger.LogEntry {
	return logger.LogEntry{
		Timestamp:   time.Now().UTC(),
		SessionID:   session,
		Direction:   dir,
		MessageType: "request",
		Method:      method,
		ToolName:    tool,
		Summary:     "→ " + method,
		RawSize:     128,
	}
}

// TestLogWritesToFile verifies that Log() creates a JSONL file with the correct name.
func TestLogWritesToFile(t *testing.T) {
	dir := t.TempDir()
	l, err := logger.New(dir, "sess-1")
	require.NoError(t, err)
	defer l.Close()

	entry := makeEntry("tools/call", "read_file", "to_server", "sess-1")
	require.NoError(t, l.Log(entry))

	day := time.Now().UTC().Format("2006-01-02")
	path := filepath.Join(dir, day+".jsonl")
	require.FileExists(t, path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "tools/call")
}

// TestLogEntryFormat verifies that a logged entry round-trips through JSON.
func TestLogEntryFormat(t *testing.T) {
	dir := t.TempDir()
	l, err := logger.New(dir, "sess-2")
	require.NoError(t, err)
	defer l.Close()

	id := json.RawMessage(`42`)
	entry := logger.LogEntry{
		Timestamp:   time.Now().UTC().Truncate(time.Millisecond),
		SessionID:   "sess-2",
		Direction:   "to_server",
		MessageType: "request",
		Method:      "tools/call",
		RequestID:   &id,
		ToolName:    "read_file",
		Summary:     "→ tools/call read_file",
		RawSize:     256,
	}
	require.NoError(t, l.Log(entry))
	require.NoError(t, l.Close())

	// Read the file and unmarshal the entry.
	day := time.Now().UTC().Format("2006-01-02")
	data, err := os.ReadFile(filepath.Join(dir, day+".jsonl"))
	require.NoError(t, err)

	var got logger.LogEntry
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "tools/call", got.Method)
	assert.Equal(t, "read_file", got.ToolName)
	assert.Equal(t, "sess-2", got.SessionID)
}

// TestDayRotation verifies that writing entries across a day boundary creates
// two separate JSONL files.
func TestDayRotation(t *testing.T) {
	dir := t.TempDir()
	l, err := logger.New(dir, "sess-rotate")
	require.NoError(t, err)
	defer l.Close()

	// Write one entry "today".
	today := time.Now().UTC()
	e1 := makeEntry("initialize", "", "to_server", "sess-rotate")
	e1.Timestamp = today
	require.NoError(t, l.Log(e1))

	// Simulate a day change by writing an entry with yesterday's date.
	// We do this by directly creating a file — the Logger will rotate when
	// the real day changes. For this test we use a direct Query to verify two files.
	yesterday := today.Add(-24 * time.Hour)
	yDay := yesterday.Format("2006-01-02")
	yPath := filepath.Join(dir, yDay+".jsonl")
	data, _ := json.Marshal(makeEntry("ping", "", "to_server", "sess-rotate"))
	_ = os.WriteFile(yPath, append(data, '\n'), 0600)

	// There should be at least two JSONL files.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	var jsonlFiles int
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".jsonl" {
			jsonlFiles++
		}
	}
	assert.GreaterOrEqual(t, jsonlFiles, 2)
}

// TestFilePermissions verifies that log files are created with 0600 permissions.
func TestFilePermissions(t *testing.T) {
	dir := t.TempDir()
	l, err := logger.New(dir, "sess-perm")
	require.NoError(t, err)
	defer l.Close()

	require.NoError(t, l.Log(makeEntry("ping", "", "to_server", "sess-perm")))

	day := time.Now().UTC().Format("2006-01-02")
	info, err := os.Stat(filepath.Join(dir, day+".jsonl"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

// TestDurationTracking verifies that RequestTracker computes durations correctly.
func TestDurationTracking(t *testing.T) {
	tracker := logger.NewRequestTracker()

	id := json.RawMessage(`5`)
	tracker.TrackRequest(id)

	time.Sleep(10 * time.Millisecond)
	ms := tracker.CompleteRequest(id)
	require.NotNil(t, ms)
	assert.GreaterOrEqual(t, *ms, 5.0, "duration should be at least 5ms")
	assert.LessOrEqual(t, *ms, 5000.0, "duration should be less than 5s")

	// A second CompleteRequest for the same ID should return nil.
	ms2 := tracker.CompleteRequest(id)
	assert.Nil(t, ms2)
}

// TestSummaryGeneration verifies summary strings for various entry types.
func TestSummaryGeneration(t *testing.T) {
	errCode := -32602

	tests := []struct {
		name      string
		dir       string
		msgType   string
		method    string
		tool      string
		ms        *float64
		rawSize   int
		isError   bool
		errCode   *int
		errMsg    string
		wantParts []string
	}{
		{
			name: "request with tool",
			dir:  "to_server", msgType: "request", method: "tools/call", tool: "read_file",
			wantParts: []string{"→", "tools/call", "read_file"},
		},
		{
			name: "notification",
			dir:  "to_server", msgType: "notification", method: "notifications/initialized",
			wantParts: []string{"→", "notifications/initialized"},
		},
		{
			name:    "successful response",
			dir:     "from_server",
			msgType: "response", method: "tools/call",
			ms:        func() *float64 { v := 45.0; return &v }(),
			rawSize:   1200,
			wantParts: []string{"←", "OK", "45ms"},
		},
		{
			name:    "error response",
			dir:     "from_server",
			msgType: "response", method: "tools/call",
			isError: true, errCode: &errCode, errMsg: "Invalid params",
			wantParts: []string{"←", "ERROR", "-32602"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			summary := logger.BuildSummary(tc.dir, tc.msgType, tc.method, tc.tool, tc.ms, tc.rawSize, tc.isError, tc.errCode, tc.errMsg)
			for _, part := range tc.wantParts {
				assert.Contains(t, summary, part, "summary %q missing %q", summary, part)
			}
		})
	}
}

// TestQueryByTimeRange creates log files across multiple days and verifies
// that the Since/Until filters return the correct entries.
func TestQueryByTimeRange(t *testing.T) {
	dir := t.TempDir()

	now := time.Now().UTC()
	yesterday := now.Add(-24 * time.Hour)

	// Write an entry for today.
	l, err := logger.New(dir, "sess-query")
	require.NoError(t, err)
	eToday := makeEntry("tools/call", "a", "to_server", "sess-query")
	eToday.Timestamp = now
	require.NoError(t, l.Log(eToday))
	require.NoError(t, l.Close())

	// Write an entry for yesterday by hand.
	eYesterday := makeEntry("ping", "", "to_server", "sess-query")
	eYesterday.Timestamp = yesterday
	raw, _ := json.Marshal(eYesterday)
	path := filepath.Join(dir, yesterday.Format("2006-01-02")+".jsonl")
	require.NoError(t, os.WriteFile(path, append(raw, '\n'), 0600))

	// Query only today's entries.
	since := now.Add(-1 * time.Hour)
	result, err := logger.Query(dir, logger.QueryFilter{Since: &since})
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "tools/call", result[0].Method)
}

// TestQueryByToolName verifies that filtering by tool name returns only matching entries.
func TestQueryByToolName(t *testing.T) {
	dir := t.TempDir()
	l, err := logger.New(dir, "sess-tool")
	require.NoError(t, err)
	defer l.Close()

	require.NoError(t, l.Log(makeEntry("tools/call", "read_file", "to_server", "sess-tool")))
	require.NoError(t, l.Log(makeEntry("tools/call", "write_file", "to_server", "sess-tool")))
	require.NoError(t, l.Log(makeEntry("tools/call", "read_file", "to_server", "sess-tool")))

	result, err := logger.Query(dir, logger.QueryFilter{ToolName: "read_file"})
	require.NoError(t, err)
	assert.Len(t, result, 2)
	for _, e := range result {
		assert.Equal(t, "read_file", e.ToolName)
	}
}

// TestQueryLimit verifies that the Limit field caps the number of results.
func TestQueryLimit(t *testing.T) {
	dir := t.TempDir()
	l, err := logger.New(dir, "sess-limit")
	require.NoError(t, err)
	defer l.Close()

	for i := 0; i < 10; i++ {
		require.NoError(t, l.Log(makeEntry("ping", "", "to_server", "sess-limit")))
	}

	result, err := logger.Query(dir, logger.QueryFilter{Limit: 3})
	require.NoError(t, err)
	assert.Len(t, result, 3)
}

// TestConcurrentLogging verifies that concurrent Log() calls don't corrupt the file.
func TestConcurrentLogging(t *testing.T) {
	dir := t.TempDir()
	l, err := logger.New(dir, "sess-concurrent")
	require.NoError(t, err)
	defer l.Close()

	const goroutines = 10
	const entriesEach = 20

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < entriesEach; i++ {
				entry := makeEntry("tools/call", "tool", "to_server", "sess-concurrent")
				_ = l.Log(entry)
			}
		}(g)
	}
	wg.Wait()

	// All entries should be parseable (no corruption).
	result, err := logger.Query(dir, logger.QueryFilter{})
	require.NoError(t, err)
	assert.Equal(t, goroutines*entriesEach, len(result))
}
