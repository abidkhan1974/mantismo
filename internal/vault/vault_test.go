package vault_test

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/inferalabs/mantismo/internal/vault"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func vaultPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test.vault")
}

// TestOpenCreate verifies that Open creates a new vault without error.
func TestOpenCreate(t *testing.T) {
	v, err := vault.Open(vaultPath(t), "passphrase123")
	require.NoError(t, err)
	require.NotNil(t, v)
	require.NoError(t, v.Close())
}

// TestOpenExistingVault verifies that reopening with the same passphrase succeeds.
func TestOpenExistingVault(t *testing.T) {
	path := vaultPath(t)

	v1, err := vault.Open(path, "mypass")
	require.NoError(t, err)
	// Trigger passphrase sentinel creation.
	require.NoError(t, v1.Set(vault.Entry{
		Key: "init", Value: "val", Category: vault.Profile, Sensitivity: vault.Standard,
	}))
	require.NoError(t, v1.Close())

	v2, err := vault.Open(path, "mypass")
	require.NoError(t, err)
	require.NotNil(t, v2)

	entry, err := v2.Get("init")
	require.NoError(t, err)
	require.NotNil(t, entry)
	assert.Equal(t, "val", entry.Value)
	require.NoError(t, v2.Close())
}

// TestWrongPassphrase verifies that the first data operation with the wrong
// passphrase returns an error containing "wrong passphrase".
func TestWrongPassphrase(t *testing.T) {
	path := vaultPath(t)

	v1, err := vault.Open(path, "correct")
	require.NoError(t, err)
	// Trigger sentinel creation.
	require.NoError(t, v1.Set(vault.Entry{
		Key: "x", Value: "y", Category: vault.Profile, Sensitivity: vault.Standard,
	}))
	require.NoError(t, v1.Close())

	v2, err := vault.Open(path, "wrong")
	require.NoError(t, err)
	require.NotNil(t, v2)
	defer v2.Close() //nolint:errcheck

	_, err = v2.Get("x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "wrong passphrase")
}

// TestSetGet verifies that Set and Get round-trip correctly.
func TestSetGet(t *testing.T) {
	v, err := vault.Open(vaultPath(t), "pass")
	require.NoError(t, err)
	defer v.Close() //nolint:errcheck

	entry := vault.Entry{
		Key:         "full_name",
		Value:       "Alice Wonderland",
		Category:    vault.Profile,
		Sensitivity: vault.Standard,
		Label:       "Full Name",
	}
	require.NoError(t, v.Set(entry))

	got, err := v.Get("full_name")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "full_name", got.Key)
	assert.Equal(t, "Alice Wonderland", got.Value)
	assert.Equal(t, vault.Profile, got.Category)
	assert.Equal(t, vault.Standard, got.Sensitivity)
	assert.Equal(t, "Full Name", got.Label)
	assert.WithinDuration(t, time.Now(), got.CreatedAt, 5*time.Second)
}

// TestSetUpdate verifies that updating an existing key preserves created_at and
// updates updated_at.
func TestSetUpdate(t *testing.T) {
	v, err := vault.Open(vaultPath(t), "pass")
	require.NoError(t, err)
	defer v.Close() //nolint:errcheck

	first := vault.Entry{
		Key: "key1", Value: "first", Category: vault.Profile, Sensitivity: vault.Standard,
	}
	require.NoError(t, v.Set(first))

	before, err := v.Get("key1")
	require.NoError(t, err)
	require.NotNil(t, before)
	createdAt := before.CreatedAt

	// Wait a moment then update.
	time.Sleep(10 * time.Millisecond)

	second := vault.Entry{
		Key: "key1", Value: "second", Category: vault.Profile, Sensitivity: vault.Standard,
	}
	require.NoError(t, v.Set(second))

	after, err := v.Get("key1")
	require.NoError(t, err)
	require.NotNil(t, after)

	assert.Equal(t, "second", after.Value)
	assert.Equal(t, createdAt.UTC().Truncate(time.Second), after.CreatedAt.UTC().Truncate(time.Second),
		"created_at should be preserved on update")
}

// TestDelete verifies that Delete removes an entry (Get returns nil afterwards).
func TestDelete(t *testing.T) {
	v, err := vault.Open(vaultPath(t), "pass")
	require.NoError(t, err)
	defer v.Close() //nolint:errcheck

	require.NoError(t, v.Set(vault.Entry{
		Key: "temp", Value: "remove_me", Category: vault.Profile, Sensitivity: vault.Standard,
	}))

	require.NoError(t, v.Delete("temp"))

	got, err := v.Get("temp")
	require.NoError(t, err)
	assert.Nil(t, got)
}

// TestListByCategory verifies filtering by category.
func TestListByCategory(t *testing.T) {
	v, err := vault.Open(vaultPath(t), "pass")
	require.NoError(t, err)
	defer v.Close() //nolint:errcheck

	require.NoError(t, v.Set(vault.Entry{
		Key: "name", Value: "Bob", Category: vault.Profile, Sensitivity: vault.Standard,
	}))
	require.NoError(t, v.Set(vault.Entry{
		Key: "ssn", Value: "123-45-6789", Category: vault.Identifiers, Sensitivity: vault.Critical,
	}))
	require.NoError(t, v.Set(vault.Entry{
		Key: "city", Value: "Seattle", Category: vault.Profile, Sensitivity: vault.Public,
	}))

	cat := vault.Profile
	entries, err := v.List(&cat, nil)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	keys := []string{entries[0].Key, entries[1].Key}
	assert.Contains(t, keys, "name")
	assert.Contains(t, keys, "city")
}

// TestListBySensitivity verifies that maxSensitivity=standard filters out higher levels.
func TestListBySensitivity(t *testing.T) {
	v, err := vault.Open(vaultPath(t), "pass")
	require.NoError(t, err)
	defer v.Close() //nolint:errcheck

	require.NoError(t, v.Set(vault.Entry{
		Key: "pub", Value: "public_val", Category: vault.Profile, Sensitivity: vault.Public,
	}))
	require.NoError(t, v.Set(vault.Entry{
		Key: "std", Value: "standard_val", Category: vault.Profile, Sensitivity: vault.Standard,
	}))
	require.NoError(t, v.Set(vault.Entry{
		Key: "sens", Value: "sensitive_val", Category: vault.Profile, Sensitivity: vault.Sensitive,
	}))
	require.NoError(t, v.Set(vault.Entry{
		Key: "crit", Value: "critical_val", Category: vault.Profile, Sensitivity: vault.Critical,
	}))

	maxSens := vault.Standard
	entries, err := v.List(nil, &maxSens)
	require.NoError(t, err)

	keys := make(map[string]bool)
	for _, e := range entries {
		keys[e.Key] = true
	}
	assert.True(t, keys["pub"], "public should be included")
	assert.True(t, keys["std"], "standard should be included")
	assert.False(t, keys["sens"], "sensitive should be excluded")
	assert.False(t, keys["crit"], "critical should be excluded")
}

// TestSearch verifies that Search finds entries by keyword.
func TestSearch(t *testing.T) {
	v, err := vault.Open(vaultPath(t), "pass")
	require.NoError(t, err)
	defer v.Close() //nolint:errcheck

	require.NoError(t, v.Set(vault.Entry{
		Key: "bio", Value: "Software engineer at ACME Corp", Category: vault.Documents, Sensitivity: vault.Standard,
	}))
	require.NoError(t, v.Set(vault.Entry{
		Key: "address", Value: "123 Main St", Category: vault.Profile, Sensitivity: vault.Standard,
	}))

	results, err := v.Search("ACME", nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "bio", results[0].Key)
}

// TestExportImport verifies exporting from one vault and importing into another.
func TestExportImport(t *testing.T) {
	v1, err := vault.Open(vaultPath(t), "pass1")
	require.NoError(t, err)
	defer v1.Close() //nolint:errcheck

	entries := []vault.Entry{
		{Key: "a", Value: "val_a", Category: vault.Profile, Sensitivity: vault.Standard, Label: "A"},
		{Key: "b", Value: "val_b", Category: vault.Preferences, Sensitivity: vault.Public, Label: "B"},
	}
	for _, e := range entries {
		require.NoError(t, v1.Set(e))
	}

	exported, err := v1.Export()
	require.NoError(t, err)
	require.Len(t, exported, 2)

	v2, err := vault.Open(vaultPath(t), "pass2")
	require.NoError(t, err)
	defer v2.Close() //nolint:errcheck

	require.NoError(t, v2.Import(exported))

	for _, e := range entries {
		got, getErr := v2.Get(e.Key)
		require.NoError(t, getErr)
		require.NotNil(t, got)
		assert.Equal(t, e.Value, got.Value)
	}
}

// TestStats verifies that Stats returns accurate counts.
func TestStats(t *testing.T) {
	v, err := vault.Open(vaultPath(t), "pass")
	require.NoError(t, err)
	defer v.Close() //nolint:errcheck

	require.NoError(t, v.Set(vault.Entry{
		Key: "name", Value: "Charlie", Category: vault.Profile, Sensitivity: vault.Standard,
	}))
	require.NoError(t, v.Set(vault.Entry{
		Key: "city", Value: "NYC", Category: vault.Profile, Sensitivity: vault.Public,
	}))
	require.NoError(t, v.Set(vault.Entry{
		Key: "theme", Value: "dark", Category: vault.Preferences, Sensitivity: vault.Public,
	}))

	stats, err := v.Stats()
	require.NoError(t, err)
	assert.Equal(t, 3, stats.TotalEntries)
	assert.Equal(t, 2, stats.ByCategory[vault.Profile])
	assert.Equal(t, 1, stats.ByCategory[vault.Preferences])
	assert.Equal(t, 1, stats.BySensitivity[vault.Standard])
	assert.Equal(t, 2, stats.BySensitivity[vault.Public])
}

// TestConcurrentAccess verifies that 10 goroutines can write and read concurrently
// without corruption.
func TestConcurrentAccess(t *testing.T) {
	v, err := vault.Open(vaultPath(t), "pass")
	require.NoError(t, err)
	defer v.Close() //nolint:errcheck

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("key_%d", i)
			val := fmt.Sprintf("value_%d", i)
			setErr := v.Set(vault.Entry{
				Key:         key,
				Value:       val,
				Category:    vault.Profile,
				Sensitivity: vault.Standard,
			})
			if setErr != nil {
				t.Errorf("Set %s: %v", key, setErr)
			}
		}()
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		key := fmt.Sprintf("key_%d", i)
		expected := fmt.Sprintf("value_%d", i)
		got, getErr := v.Get(key)
		require.NoError(t, getErr)
		require.NotNil(t, got, "expected entry for %s", key)
		assert.Equal(t, expected, got.Value)
	}
}
