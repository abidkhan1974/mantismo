# 12 — Spec: Vault Storage (SQLite + SQLCipher)

## Objective

Build the encrypted local data vault that stores the user's personal information. This is the backend store; the MCP tools that expose vault data to agents are in spec 13.

## Prerequisites

- Spec 07 (CLI) — `vault` subcommands scaffold

## Interface Contract

### Package: `internal/vault`

```go
// Sensitivity levels for vault fields.
type Sensitivity string
const (
    Public    Sensitivity = "public"    // Name, preferred language
    Standard  Sensitivity = "standard"  // Email, phone, preferences
    Sensitive Sensitivity = "sensitive" // ID numbers (masked), financial metadata
    Critical  Sensitivity = "critical"  // Full ID numbers, private keys, passwords
)

// Category organizes vault entries.
type Category string
const (
    Profile        Category = "profile"          // Name, email, phone, bio
    Identifiers    Category = "identifiers"      // Passport, drivers license, SSN
    Preferences    Category = "preferences"      // Travel prefs, dietary, work style
    Documents      Category = "documents"        // Stored text documents, notes
    Credentials    Category = "credentials_meta" // Metadata only (service name, username, NOT passwords)
    Financial      Category = "financial"        // Bank name, last-4 of account (NOT full numbers)
)

// Entry represents a single vault record.
type Entry struct {
    Key          string      `json:"key"`           // Unique identifier, e.g., "profile.full_name"
    Value        string      `json:"value"`         // Plaintext value (decrypted)
    Category     Category    `json:"category"`
    Sensitivity  Sensitivity `json:"sensitivity"`
    Label        string      `json:"label"`         // Human-readable label, e.g., "Full Name"
    CreatedAt    time.Time   `json:"created_at"`
    UpdatedAt    time.Time   `json:"updated_at"`
}

// Vault provides encrypted storage operations.
type Vault struct {
    db   *sql.DB
    path string
}

// Open opens (or creates) an encrypted vault database.
// passphrase is used to derive the SQLCipher encryption key via Argon2id.
func Open(path string, passphrase string) (*Vault, error)

// Close closes the vault database.
func (v *Vault) Close() error

// Set creates or updates a vault entry.
func (v *Vault) Set(entry Entry) error

// Get retrieves a vault entry by key.
func (v *Vault) Get(key string) (*Entry, error)

// Delete removes a vault entry by key.
func (v *Vault) Delete(key string) error

// List returns all entries matching the given category and/or sensitivity filter.
func (v *Vault) List(category *Category, maxSensitivity *Sensitivity) ([]Entry, error)

// Search performs keyword search across entry keys, labels, and values.
func (v *Vault) Search(query string, maxSensitivity *Sensitivity) ([]Entry, error)

// Export returns all entries (for backup).
func (v *Vault) Export() ([]Entry, error)

// Import bulk-inserts entries (for restore).
func (v *Vault) Import(entries []Entry) error

// Stats returns vault statistics.
func (v *Vault) Stats() (VaultStats, error)

type VaultStats struct {
    TotalEntries  int
    ByCategory    map[Category]int
    BySensitivity map[Sensitivity]int
    DBSizeBytes   int64
    CreatedAt     time.Time
    LastModified  time.Time
}
```

## Detailed Requirements

### 12.1 Database Setup

SQL schema (`schema.sql`):
```sql
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
```

On first open, run schema migration and store vault version in `vault_meta`.

### 12.2 Encryption

- Use `github.com/mutecomm/go-sqlcipher/v4` (CGo wrapper for SQLCipher)
- Passphrase → key derivation via SQLCipher's built-in PBKDF2 (SQLCipher 4 uses 256,000 iterations of PBKDF2-HMAC-SHA512 by default)
- Alternative: if CGo is problematic for cross-compilation, use `crawshaw.io/sqlite` with manual envelope encryption at the application layer. Document the tradeoff.
- All data encrypted at rest; decrypted only in memory during vault operations

### 12.3 Key Derivation

On `Open()`:
1. If database doesn't exist: create it, set SQLCipher key from passphrase, run schema
2. If database exists: open it, set SQLCipher key, verify by running a test query
3. If passphrase is wrong: SQLCipher returns an error on first query (not on open) — detect this and return a clear "wrong passphrase" error

### 12.4 CLI: `vault init`

Interactive wizard:
```
$ mantismo vault init

🔒 Initialize Mantismo Vault
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Enter a passphrase for your vault (min 12 characters):
> ••••••••••••••

Confirm passphrase:
> ••••••••••••••

Vault created at: ~/.mantismo/vault.db
Your vault is encrypted with AES-256. Remember your passphrase — it cannot be recovered.

Run 'mantismo vault import' to add your personal data.
```

### 12.5 CLI: `vault import`

Interactive import wizard that walks through categories:

```
$ mantismo vault import

📥 Import Personal Data
━━━━━━━━━━━━━━━━━━━━━━

Category: Profile
  Full name: Alex Chen
  Email (standard): user@example.com
  Phone (standard): [skip]

Category: Preferences
  Preferred language: English
  Travel seat preference: aisle
  Dietary restrictions: [skip]

Category: Identifiers
  Passport number (critical): [skip for now]
  Drivers license (sensitive): [skip for now]

Imported 4 entries. Run 'mantismo vault list' to review.
```

Also support non-interactive import from JSON:
```bash
mantismo vault import --file my_data.json
```

### 12.6 Sensitivity-Based Access Control

When vault data is queried by MCP tools (spec 13), the sensitivity level determines what's returned:

| Agent Trust Level | Can Access |
|---|---|
| Untrusted | Public only |
| Standard | Public + Standard |
| Trusted | Public + Standard + Sensitive (masked) |
| Full (requires approval) | All levels (critical requires per-call approval) |

Masking rules for sensitive data:
- ID numbers: show last 4 only → `***-**-1234`
- Phone: show last 4 → `***-***-5678`
- Email: mask middle → `u***@example.com`

## Test Plan

1. **TestVaultCreateAndOpen** — Create vault, close, reopen with same passphrase
2. **TestWrongPassphrase** — Open with wrong passphrase, verify clear error
3. **TestSetAndGet** — Store entry, retrieve it, verify all fields
4. **TestUpdate** — Update existing entry, verify updated_at changes
5. **TestDelete** — Delete entry, verify it's gone
6. **TestListByCategory** — Store entries in multiple categories, filter by one
7. **TestListBySensitivity** — Filter entries at or below given sensitivity
8. **TestSearch** — Search by keyword across keys, labels, and values
9. **TestExportImport** — Export all entries, create new vault, import, verify identical
10. **TestStats** — Verify stats reflect actual vault contents
11. **TestEncryptionAtRest** — Open vault DB with raw SQLite (no key), verify unreadable
12. **TestConcurrentAccess** — Read and write from multiple goroutines simultaneously

## Acceptance Criteria

- [ ] Vault database is encrypted at rest (unreadable without passphrase)
- [ ] Wrong passphrase produces clear error, not corruption
- [ ] CRUD operations work correctly for all field types
- [ ] Sensitivity-based filtering works
- [ ] Keyword search works across keys, labels, and values
- [ ] Export produces valid JSON; import restores from JSON
- [ ] CLI wizard creates vault and imports data interactively
- [ ] All 12 tests pass
