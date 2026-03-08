// Copyright 2026 Mantismo. All rights reserved.
// Use of this source code is governed by the AGPL-3.0 license
// or a commercial license. See LICENSE for details.

// Package vault provides an encrypted local data store for personal information.
// Values are encrypted with AES-256-GCM; the key is derived from a passphrase
// using PBKDF2-HMAC-SHA512 with 256,000 iterations.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha512"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	// Register modernc.org/sqlite driver as "sqlite".
	"golang.org/x/crypto/pbkdf2"
	_ "modernc.org/sqlite"
)

// Sensitivity classifies how sensitive a vault entry is.
type Sensitivity string

const (
	// Public entries can be shared with any caller.
	Public Sensitivity = "public"
	// Standard entries are suitable for trusted callers.
	Standard Sensitivity = "standard"
	// Sensitive entries require an elevated trust level.
	Sensitive Sensitivity = "sensitive"
	// Critical entries require explicit user approval before exposure.
	Critical Sensitivity = "critical"
)

// Category groups vault entries by their semantic meaning.
type Category string

const (
	// Profile holds personal profile information (name, address, etc.).
	Profile Category = "profile"
	// Identifiers holds official identification numbers (SSN, passport, etc.).
	Identifiers Category = "identifiers"
	// Preferences holds user preference settings.
	Preferences Category = "preferences"
	// Documents holds stored documents.
	Documents Category = "documents"
	// Credentials holds credentials metadata (not actual passwords).
	Credentials Category = "credentials_meta"
	// Financial holds financial information.
	Financial Category = "financial"
)

// Entry represents a single record in the vault.
type Entry struct {
	// Key is the unique identifier for the entry.
	Key string
	// Value is the decrypted plaintext value.
	Value string
	// Category classifies the entry semantically.
	Category Category
	// Sensitivity controls access level required.
	Sensitivity Sensitivity
	// Label is a human-readable description.
	Label string
	// CreatedAt is when the entry was first stored.
	CreatedAt time.Time
	// UpdatedAt is when the entry was last modified.
	UpdatedAt time.Time
}

// VaultStats holds aggregate statistics about the vault.
type VaultStats struct {
	// TotalEntries is the total number of entries in the vault.
	TotalEntries int
	// ByCategory maps each category to its entry count.
	ByCategory map[Category]int
	// BySensitivity maps each sensitivity level to its entry count.
	BySensitivity map[Sensitivity]int
	// DBSizeBytes is the size of the SQLite database file in bytes.
	DBSizeBytes int64
	// CreatedAt is when the vault was first created.
	CreatedAt time.Time
	// LastModified is when the vault was last modified.
	LastModified time.Time
}

// Vault is an encrypted local SQLite database storing personal data.
type Vault struct {
	mu   sync.RWMutex
	db   *sql.DB
	path string
	key  []byte // 32-byte AES-256 key derived from passphrase
}

const schema = `
CREATE TABLE IF NOT EXISTS vault_entries (
    key           TEXT PRIMARY KEY,
    value         TEXT NOT NULL,
    category      TEXT NOT NULL,
    sensitivity   TEXT NOT NULL DEFAULT 'standard',
    label         TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_category ON vault_entries(category);
CREATE INDEX IF NOT EXISTS idx_sensitivity ON vault_entries(sensitivity);
CREATE TABLE IF NOT EXISTS vault_meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
`

// Open opens (or creates) the vault at the given path with the given passphrase.
// The passphrase is not verified at open time; it is deferred to the first data
// operation via checkPassphrase.
func Open(path string, passphrase string) (*Vault, error) {
	// Ensure parent directories exist.
	if err := os.MkdirAll(filepath(path), 0o700); err != nil {
		return nil, fmt.Errorf("vault: create dirs: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("vault: open sqlite: %w", err)
	}

	// Enable WAL mode for better concurrency.
	if _, err = db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("vault: enable WAL: %w", err)
	}

	// Create schema.
	if _, err = db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("vault: create schema: %w", err)
	}

	v := &Vault{db: db, path: path}

	// Load or create the KDF salt.
	salt, err := loadOrCreateSalt(db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("vault: kdf salt: %w", err)
	}

	// Derive the AES-256 key.
	v.key = pbkdf2.Key([]byte(passphrase), salt, 256000, 32, sha512.New)

	return v, nil
}

// filepath returns the directory component of path.
func filepath(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return "."
	}
	return path[:idx]
}

// loadOrCreateSalt reads the KDF salt from vault_meta, creating one if absent.
func loadOrCreateSalt(db *sql.DB) ([]byte, error) {
	var hexSalt string
	err := db.QueryRow("SELECT value FROM vault_meta WHERE key='kdf_salt'").Scan(&hexSalt)
	if err == nil {
		// Salt already exists.
		return hex.DecodeString(hexSalt)
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("query kdf_salt: %w", err)
	}

	// Generate a new 32-byte salt.
	salt := make([]byte, 32)
	if _, err = io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}
	_, err = db.Exec("INSERT INTO vault_meta (key, value) VALUES ('kdf_salt', ?)", hex.EncodeToString(salt))
	if err != nil {
		return nil, fmt.Errorf("store kdf_salt: %w", err)
	}
	return salt, nil
}

// encryptValue encrypts plaintext with AES-256-GCM and returns base64(nonce+ciphertext).
func encryptValue(v *Vault, plaintext string) (string, error) {
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return "", fmt.Errorf("vault: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("vault: new GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("vault: generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decryptValue decodes and decrypts a base64(nonce+ciphertext) value.
func decryptValue(v *Vault, encoded string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("vault: base64 decode: %w", err)
	}
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return "", fmt.Errorf("vault: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("vault: new GCM: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("vault: ciphertext too short")
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("vault: decrypt: %w", err)
	}
	return string(plaintext), nil
}

// checkPassphrase verifies the passphrase by decrypting the kdf_check sentinel.
// On a new vault it creates the sentinel. Must be called while the Vault mu is held.
func checkPassphrase(v *Vault) error {
	var encoded string
	err := v.db.QueryRow("SELECT value FROM vault_meta WHERE key='kdf_check'").Scan(&encoded)
	if err == sql.ErrNoRows {
		// New vault — create the sentinel.
		encrypted, encErr := encryptValue(v, "mantismo_vault_ok")
		if encErr != nil {
			return encErr
		}
		_, dbErr := v.db.Exec("INSERT INTO vault_meta (key, value) VALUES ('kdf_check', ?)", encrypted)
		if dbErr != nil {
			return fmt.Errorf("vault: store kdf_check: %w", dbErr)
		}
		// Also record the vault creation time.
		now := time.Now().UTC().Format(time.RFC3339)
		_, _ = v.db.Exec("INSERT OR IGNORE INTO vault_meta (key, value) VALUES ('created_at', ?)", now)
		return nil
	}
	if err != nil {
		return fmt.Errorf("vault: query kdf_check: %w", err)
	}
	_, decErr := decryptValue(v, encoded)
	if decErr != nil {
		return fmt.Errorf("vault: wrong passphrase")
	}
	return nil
}

// Close closes the underlying database connection.
func (v *Vault) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.db.Close()
}

// Set stores an entry in the vault, encrypting its value.
// If the key already exists, the value, category, sensitivity and label are
// updated while the original created_at is preserved.
func (v *Vault) Set(entry Entry) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if err := checkPassphrase(v); err != nil {
		return err
	}

	encrypted, err := encryptValue(v, entry.Value)
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Check whether the key already exists.
	var existingCreatedAt string
	queryErr := v.db.QueryRow("SELECT created_at FROM vault_entries WHERE key=?", entry.Key).Scan(&existingCreatedAt)
	switch queryErr {
	case nil:
		// Key exists — update it, preserving created_at.
		_, err = v.db.Exec(
			"UPDATE vault_entries SET value=?, category=?, sensitivity=?, label=?, updated_at=? WHERE key=?",
			encrypted, string(entry.Category), string(entry.Sensitivity), entry.Label, now, entry.Key,
		)
	case sql.ErrNoRows:
		// New key — insert it.
		_, err = v.db.Exec(
			"INSERT INTO vault_entries (key, value, category, sensitivity, label, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
			entry.Key, encrypted, string(entry.Category), string(entry.Sensitivity), entry.Label, now, now,
		)
	default:
		return fmt.Errorf("vault: check key existence: %w", queryErr)
	}
	return err
}

// Get retrieves and decrypts an entry by key.
// Returns nil, nil if the key does not exist.
func (v *Vault) Get(key string) (*Entry, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if err := checkPassphrase(v); err != nil {
		return nil, err
	}

	var (
		encValue  string
		category  string
		sens      string
		label     string
		createdAt string
		updatedAt string
	)
	err := v.db.QueryRow(
		"SELECT value, category, sensitivity, label, created_at, updated_at FROM vault_entries WHERE key=?", key,
	).Scan(&encValue, &category, &sens, &label, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("vault: query entry: %w", err)
	}

	decrypted, err := decryptValue(v, encValue)
	if err != nil {
		return nil, err
	}

	ca, _ := time.Parse(time.RFC3339, createdAt)
	ua, _ := time.Parse(time.RFC3339, updatedAt)

	return &Entry{
		Key:         key,
		Value:       decrypted,
		Category:    Category(category),
		Sensitivity: Sensitivity(sens),
		Label:       label,
		CreatedAt:   ca,
		UpdatedAt:   ua,
	}, nil
}

// Delete removes an entry by key. It is not an error if the key does not exist.
func (v *Vault) Delete(key string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if err := checkPassphrase(v); err != nil {
		return err
	}

	_, err := v.db.Exec("DELETE FROM vault_entries WHERE key=?", key)
	return err
}

// sensitivityOrder maps a Sensitivity to a comparable integer.
func sensitivityOrder(s Sensitivity) int {
	switch s {
	case Public:
		return 0
	case Standard:
		return 1
	case Sensitive:
		return 2
	case Critical:
		return 3
	default:
		return 1
	}
}

// List returns all entries, optionally filtered by category and/or maxSensitivity.
// Entries are returned ordered by key.
func (v *Vault) List(category *Category, maxSensitivity *Sensitivity) ([]Entry, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if err := checkPassphrase(v); err != nil {
		return nil, err
	}

	return listEntries(v, category, maxSensitivity)
}

// listEntries is the internal implementation of List, called without locking.
func listEntries(v *Vault, category *Category, maxSensitivity *Sensitivity) ([]Entry, error) {
	query := "SELECT key, value, category, sensitivity, label, created_at, updated_at FROM vault_entries"
	var args []interface{}
	var conditions []string

	if category != nil {
		conditions = append(conditions, "category=?")
		args = append(args, string(*category))
	}

	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY key"

	rows, err := v.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("vault: list query: %w", err)
	}
	defer rows.Close()

	maxOrder := 3
	if maxSensitivity != nil {
		maxOrder = sensitivityOrder(*maxSensitivity)
	}

	var entries []Entry
	for rows.Next() {
		var (
			key       string
			encValue  string
			cat       string
			sens      string
			label     string
			createdAt string
			updatedAt string
		)
		if err = rows.Scan(&key, &encValue, &cat, &sens, &label, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("vault: scan row: %w", err)
		}

		if sensitivityOrder(Sensitivity(sens)) > maxOrder {
			continue
		}

		decrypted, decErr := decryptValue(v, encValue)
		if decErr != nil {
			return nil, decErr
		}

		ca, _ := time.Parse(time.RFC3339, createdAt)
		ua, _ := time.Parse(time.RFC3339, updatedAt)

		entries = append(entries, Entry{
			Key:         key,
			Value:       decrypted,
			Category:    Category(cat),
			Sensitivity: Sensitivity(sens),
			Label:       label,
			CreatedAt:   ca,
			UpdatedAt:   ua,
		})
	}
	return entries, rows.Err()
}

// Search returns entries whose key, value, or label contains query (case-insensitive).
// Optionally filtered by maxSensitivity.
func (v *Vault) Search(query string, maxSensitivity *Sensitivity) ([]Entry, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if err := checkPassphrase(v); err != nil {
		return nil, err
	}

	all, err := listEntries(v, nil, maxSensitivity)
	if err != nil {
		return nil, err
	}

	lower := strings.ToLower(query)
	var matches []Entry
	for _, e := range all {
		if strings.Contains(strings.ToLower(e.Key), lower) ||
			strings.Contains(strings.ToLower(e.Value), lower) ||
			strings.Contains(strings.ToLower(e.Label), lower) {
			matches = append(matches, e)
		}
	}
	return matches, nil
}

// Export returns all entries in decrypted form.
func (v *Vault) Export() ([]Entry, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if err := checkPassphrase(v); err != nil {
		return nil, err
	}

	return listEntries(v, nil, nil)
}

// Import stores a slice of entries by calling Set for each one.
func (v *Vault) Import(entries []Entry) error {
	// Do not hold the RW lock here; Set acquires it per-entry.
	if err := func() error {
		v.mu.RLock()
		defer v.mu.RUnlock()
		return checkPassphrase(v)
	}(); err != nil {
		return err
	}

	for _, e := range entries {
		if err := v.Set(e); err != nil {
			return err
		}
	}
	return nil
}

// Stats returns aggregate statistics about the vault.
func (v *Vault) Stats() (VaultStats, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if err := checkPassphrase(v); err != nil {
		return VaultStats{}, err
	}

	stats := VaultStats{
		ByCategory:    make(map[Category]int),
		BySensitivity: make(map[Sensitivity]int),
	}

	// Total entries.
	if err := v.db.QueryRow("SELECT COUNT(*) FROM vault_entries").Scan(&stats.TotalEntries); err != nil {
		return stats, fmt.Errorf("vault: count entries: %w", err)
	}

	// By category.
	rows, err := v.db.Query("SELECT category, COUNT(*) FROM vault_entries GROUP BY category")
	if err != nil {
		return stats, fmt.Errorf("vault: count by category: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cat string
		var cnt int
		if err = rows.Scan(&cat, &cnt); err != nil {
			return stats, err
		}
		stats.ByCategory[Category(cat)] = cnt
	}
	if err = rows.Err(); err != nil {
		return stats, err
	}

	// By sensitivity.
	rows2, err := v.db.Query("SELECT sensitivity, COUNT(*) FROM vault_entries GROUP BY sensitivity")
	if err != nil {
		return stats, fmt.Errorf("vault: count by sensitivity: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var sens string
		var cnt int
		if err = rows2.Scan(&sens, &cnt); err != nil {
			return stats, err
		}
		stats.BySensitivity[Sensitivity(sens)] = cnt
	}
	if err = rows2.Err(); err != nil {
		return stats, err
	}

	// DB file size.
	if fi, statErr := os.Stat(v.path); statErr == nil {
		stats.DBSizeBytes = fi.Size()
	}

	// Vault creation time from vault_meta.
	var createdStr string
	if err = v.db.QueryRow("SELECT value FROM vault_meta WHERE key='created_at'").Scan(&createdStr); err == nil {
		stats.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	}

	// Last modification time: max updated_at across all entries.
	var lastModStr string
	if err = v.db.QueryRow("SELECT MAX(updated_at) FROM vault_entries").Scan(&lastModStr); err == nil && lastModStr != "" {
		stats.LastModified, _ = time.Parse(time.RFC3339, lastModStr)
	}

	return stats, nil
}
