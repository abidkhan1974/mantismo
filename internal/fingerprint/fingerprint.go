// Package fingerprint detects changes to MCP tool descriptions between sessions
// (rug-pull defense). It hashes each tool's name, description, and inputSchema
// and alerts when they change from the stored baseline.
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/inferalabs/mantismo/internal/interceptor"
)

// ToolFingerprint holds the stored hash and metadata for a single tool.
type ToolFingerprint struct {
	Name         string    `json:"name"`
	Hash         string    `json:"hash"` // hex-encoded SHA-256
	FirstSeen    time.Time `json:"first_seen"`
	LastSeen     time.Time `json:"last_seen"`
	ServerCmd    string    `json:"server_cmd"`   // MCP server command for context
	Acknowledged bool      `json:"acknowledged"` // user accepted this hash
}

// Store manages persisted tool fingerprints backed by a JSON file.
type Store struct {
	path string
	mu   sync.Mutex
	data map[string]ToolFingerprint // keyed by tool name
}

// NewStore loads or creates the fingerprint store at path.
func NewStore(path string) (*Store, error) {
	s := &Store{
		path: path,
		data: make(map[string]ToolFingerprint),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// Check compares current tools against stored fingerprints.
// Returns slices of new, changed, and unchanged tool names.
// It does NOT persist the results; call Update to save them.
func (s *Store) Check(tools []interceptor.ToolInfo, serverCmd string) (newTools, changed, unchanged []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, tool := range tools {
		hash := computeHash(tool)
		stored, exists := s.data[tool.Name]

		switch {
		case !exists:
			newTools = append(newTools, tool.Name)
		case stored.Hash != hash:
			changed = append(changed, tool.Name)
		default:
			unchanged = append(unchanged, tool.Name)
		}
	}
	return
}

// Update stores fingerprints for all given tools (upsert).
// Preserves FirstSeen for existing tools; sets it on new ones.
func (s *Store) Update(tools []interceptor.ToolInfo, serverCmd string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	for _, tool := range tools {
		hash := computeHash(tool)
		existing, exists := s.data[tool.Name]
		fp := ToolFingerprint{
			Name:      tool.Name,
			Hash:      hash,
			LastSeen:  now,
			ServerCmd: serverCmd,
		}
		if exists {
			fp.FirstSeen = existing.FirstSeen
			// Only keep acknowledged if the hash hasn't changed.
			if existing.Hash == hash {
				fp.Acknowledged = existing.Acknowledged
			}
		} else {
			fp.FirstSeen = now
		}
		s.data[tool.Name] = fp
	}
	return s.save()
}

// Acknowledge marks a tool as user-acknowledged for its current hash.
func (s *Store) Acknowledge(toolName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fp, ok := s.data[toolName]
	if !ok {
		return fmt.Errorf("fingerprint: tool %q not found", toolName)
	}
	fp.Acknowledged = true
	s.data[toolName] = fp
	return s.save()
}

// IsToolChanged returns true if the stored hash for toolName is marked as
// unacknowledged relative to the current fingerprint (i.e., the tool changed
// and the user has not yet accepted the new description).
func (s *Store) IsToolChanged(toolName string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	fp, ok := s.data[toolName]
	if !ok {
		return false
	}
	return !fp.Acknowledged
}

// All returns a snapshot of all stored fingerprints.
func (s *Store) All() map[string]ToolFingerprint {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]ToolFingerprint, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// computeHash produces a canonical, deterministic SHA-256 hash of a tool definition.
// Uses struct-based marshaling so key order is always alphabetical.
func computeHash(tool interceptor.ToolInfo) string {
	canonical := struct {
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"inputSchema"`
		Name        string          `json:"name"`
	}{
		Description: tool.Description,
		InputSchema: tool.InputSchema,
		Name:        tool.Name,
	}
	b, _ := json.Marshal(canonical)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// load reads the JSON file. Missing file is not an error (first run).
func (s *Store) load() error {
	data, err := os.ReadFile(s.path) //nolint:gosec
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("fingerprint: read %s: %w", s.path, err)
	}
	return json.Unmarshal(data, &s.data)
}

// save writes the current data map to the JSON file atomically (write + rename).
func (s *Store) save() error {
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("fingerprint: marshal: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return fmt.Errorf("fingerprint: write tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("fingerprint: rename: %w", err)
	}
	return nil
}
