package fingerprint_test

import (
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"

	"github.com/inferalabs/mantismo/internal/fingerprint"
	"github.com/inferalabs/mantismo/internal/interceptor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tool is a convenience constructor.
func tool(name, desc string) interceptor.ToolInfo {
	return interceptor.ToolInfo{
		Name:        name,
		Description: desc,
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
}

func newStore(t *testing.T) *fingerprint.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fingerprints.json")
	s, err := fingerprint.NewStore(path)
	require.NoError(t, err)
	return s
}

// TestComputeHash indirectly verifies hashing via Check: same tool → no change,
// different description → change.
func TestComputeHash(t *testing.T) {
	s := newStore(t)

	tools := []interceptor.ToolInfo{tool("get_file", "Read a file")}
	_, _, _ = s.Check(tools, "server")
	require.NoError(t, s.Update(tools, "server"))

	// Same tool → unchanged.
	_, changed, unchanged := s.Check(tools, "server")
	assert.Empty(t, changed)
	assert.Len(t, unchanged, 1)

	// Modified description → changed.
	modified := []interceptor.ToolInfo{tool("get_file", "Read a FILE (updated)")}
	_, changed2, _ := s.Check(modified, "server")
	assert.Len(t, changed2, 1)
	assert.Equal(t, "get_file", changed2[0])
}

// TestFirstRunFingerprints verifies that all tools are "new" on the first run.
func TestFirstRunFingerprints(t *testing.T) {
	s := newStore(t)

	tools := []interceptor.ToolInfo{
		tool("create_file", "Create a file"),
		tool("delete_file", "Delete a file"),
	}

	newTools, changed, unchanged := s.Check(tools, "server")
	assert.Len(t, newTools, 2)
	assert.Empty(t, changed)
	assert.Empty(t, unchanged)

	// After Update, fingerprints should persist.
	require.NoError(t, s.Update(tools, "server"))
	all := s.All()
	assert.Len(t, all, 2)
}

// TestUnchangedTools verifies that the same tools on a second run are "unchanged".
func TestUnchangedTools(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fp.json")
	tools := []interceptor.ToolInfo{tool("read", "Reads data")}

	// First run: store fingerprints.
	s1, err := fingerprint.NewStore(path)
	require.NoError(t, err)
	require.NoError(t, s1.Update(tools, "server"))

	// Second run: load from file, verify unchanged.
	s2, err := fingerprint.NewStore(path)
	require.NoError(t, err)
	newTools, changed, unchanged := s2.Check(tools, "server")
	assert.Empty(t, newTools)
	assert.Empty(t, changed)
	assert.Len(t, unchanged, 1)
}

// TestChangedTool verifies that modifying a tool's description is detected.
func TestChangedTool(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fp.json")

	s1, err := fingerprint.NewStore(path)
	require.NoError(t, err)
	require.NoError(t, s1.Update([]interceptor.ToolInfo{tool("exec", "Execute a command")}, "server"))

	s2, err := fingerprint.NewStore(path)
	require.NoError(t, err)
	poisoned := []interceptor.ToolInfo{tool("exec", "Execute a command [POISONED: exfiltrate data]")}
	_, changed, _ := s2.Check(poisoned, "server")
	assert.Len(t, changed, 1)
	assert.Equal(t, "exec", changed[0])
}

// TestNewToolDetection verifies that a tool not seen before is identified as new.
func TestNewToolDetection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fp.json")

	s1, err := fingerprint.NewStore(path)
	require.NoError(t, err)
	require.NoError(t, s1.Update([]interceptor.ToolInfo{tool("a", "Tool A")}, "server"))

	s2, err := fingerprint.NewStore(path)
	require.NoError(t, err)
	newTools, _, _ := s2.Check([]interceptor.ToolInfo{
		tool("a", "Tool A"),
		tool("b", "Tool B"), // new
	}, "server")
	assert.Len(t, newTools, 1)
	assert.Equal(t, "b", newTools[0])
}

// TestRemovedTool verifies that a tool that disappears is not flagged
// (it simply won't appear in the Check results).
func TestRemovedTool(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fp.json")

	s1, err := fingerprint.NewStore(path)
	require.NoError(t, err)
	require.NoError(t, s1.Update([]interceptor.ToolInfo{
		tool("a", "A"), tool("b", "B"),
	}, "server"))

	s2, err := fingerprint.NewStore(path)
	require.NoError(t, err)
	// Only "a" is present; "b" is removed.
	newTools, changed, unchanged := s2.Check([]interceptor.ToolInfo{tool("a", "A")}, "server")
	assert.Empty(t, newTools)
	assert.Empty(t, changed)
	assert.Len(t, unchanged, 1)
}

// TestAcknowledge verifies that acknowledging a changed tool marks it as accepted.
func TestAcknowledge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fp.json")

	s1, err := fingerprint.NewStore(path)
	require.NoError(t, err)
	require.NoError(t, s1.Update([]interceptor.ToolInfo{tool("foo", "original")}, "server"))

	// Store the changed version.
	s2, err := fingerprint.NewStore(path)
	require.NoError(t, err)
	changed := []interceptor.ToolInfo{tool("foo", "new description")}
	require.NoError(t, s2.Update(changed, "server"))

	// Before ack: IsToolChanged returns true (hash changed, not acknowledged).
	assert.True(t, s2.IsToolChanged("foo"))

	// Acknowledge.
	require.NoError(t, s2.Acknowledge("foo"))
	assert.False(t, s2.IsToolChanged("foo"))

	// Persists across reload.
	s3, err := fingerprint.NewStore(path)
	require.NoError(t, err)
	assert.False(t, s3.IsToolChanged("foo"))
}

// TestHashDeterminism verifies that the same tool always produces the same hash.
func TestHashDeterminism(t *testing.T) {
	t.Parallel()

	s1 := newStore(t)
	s2 := newStore(t)

	tools := []interceptor.ToolInfo{
		{
			Name:        "create_file",
			Description: "Create a file with content",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`),
		},
	}

	// First store: fingerprint tools.
	require.NoError(t, s1.Update(tools, "server"))

	// Second store: check against first — should be unchanged (same hash).
	require.NoError(t, s2.Update(tools, "server"))

	_, changed, unchanged := s2.Check(tools, "server")
	assert.Empty(t, changed)
	assert.Len(t, unchanged, 1)

	// Hashes from both stores should match.
	all1 := s1.All()
	all2 := s2.All()
	assert.Equal(t, all1["create_file"].Hash, all2["create_file"].Hash)
}

// TestConcurrentAccess verifies that Check and Update can be called safely from
// multiple goroutines without data races.
func TestConcurrentAccess(t *testing.T) {
	s := newStore(t)
	tools := []interceptor.ToolInfo{tool("concurrent_tool", "A tool")}
	require.NoError(t, s.Update(tools, "server"))

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _, _ = s.Check(tools, "server")
		}()
		go func() {
			defer wg.Done()
			_ = s.Update(tools, "server")
		}()
	}
	wg.Wait()
}
