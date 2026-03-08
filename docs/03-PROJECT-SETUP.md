# 03 — Spec: Project Setup

## Objective

Initialize the Go project with module structure, dependencies, Makefile, CI configuration, and linting. The output of this spec is a buildable, lintable, testable (empty) Go project.

## Prerequisites

- Go 1.26+
- `golangci-lint`
- `make`

## Tasks

### 3.1 Initialize Go Module

```bash
go mod init github.com/abidkhan1974/mantismo
```

### 3.2 Create Directory Structure

Create all directories from the Architecture doc (02-ARCHITECTURE.md). Every `internal/` package should contain a placeholder `.go` file with the package declaration and a brief doc comment.

Example for `internal/proxy/proxy.go`:
```go
// Package proxy manages the stdio subprocess lifecycle and JSON-RPC message forwarding
// between the agent host and the MCP server.
package proxy
```

### 3.3 Create `cmd/mantismo/main.go`

Minimal entry point using `cobra`. Define the root command with:
- Name: `mantismo`
- Short description: "Eyes on every agent"
- Version flag: `--version` (hardcoded to `0.1.0-dev` for now; GoReleaser will inject at build time)

Define placeholder subcommands (no implementation yet):
- `wrap` — "Wrap an MCP server with Mantismo proxy"
- `logs` — "View and query audit logs"
- `tools` — "List tools seen across sessions"
- `status` — "Show Mantismo status"
- `policy` — "Manage security policies"
- `vault` — "Manage the personal data vault"
- `dashboard` — "Launch the local web dashboard"

Each subcommand should print "Not implemented yet" and exit 0.

### 3.4 Dependencies

Add the following to `go.mod`:
- `github.com/spf13/cobra` — CLI framework
- `github.com/pelletier/go-toml/v2` — TOML config parsing
- `github.com/stretchr/testify` — Test assertions (test only)

Do NOT add OPA, SQLCipher, or other heavy dependencies yet. Those are added in their respective spec documents.

### 3.5 Makefile

```makefile
.PHONY: build test lint clean

BINARY := mantismo
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0-dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

build:
	go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/mantismo/

test:
	go test -v -race -count=1 ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/

install: build
	cp bin/$(BINARY) $(GOPATH)/bin/
```

### 3.6 Linting Configuration

Create `.golangci.yml`:
```yaml
run:
  timeout: 5m

linters:
  enable:
    - errcheck
    - gosimple
    - govet
    - ineffassign
    - staticcheck
    - unused
    - gofmt
    - goimports
    - misspell
    - gosec

linters-settings:
  gosec:
    excludes:
      - G104  # Unhandled errors (we'll handle explicitly)
```

### 3.7 GitHub Actions CI

Create `.github/workflows/ci.yml`:
- Trigger: push to main, pull requests
- Jobs:
  - `lint`: Run `golangci-lint`
  - `test`: Run `make test` on Go 1.26 (ubuntu-latest and macos-latest)
  - `build`: Run `make build` on ubuntu-latest and macos-latest

### 3.8 Config Package

Implement `internal/config/config.go`:

```go
type Config struct {
    DataDir    string         `toml:"data_dir"`
    LogLevel   string         `toml:"log_level"`    // debug, info, warn, error
    Proxy      ProxyConfig    `toml:"proxy"`
    Policy     PolicyConfig   `toml:"policy"`
    Vault      VaultConfig    `toml:"vault"`
    Dashboard  DashboardConfig `toml:"dashboard"`
}

type ProxyConfig struct {
    // No fields yet; placeholder for future transport config
}

type PolicyConfig struct {
    Preset     string `toml:"preset"`      // paranoid, balanced, permissive
    PolicyDir  string `toml:"policy_dir"`  // path to custom .rego files
}

type VaultConfig struct {
    Enabled    bool   `toml:"enabled"`
    DBPath     string `toml:"db_path"`
}

type DashboardConfig struct {
    Enabled    bool   `toml:"enabled"`
    Port       int    `toml:"port"`         // default 7777
    BindAddr   string `toml:"bind_addr"`    // default 127.0.0.1
}
```

Implement `LoadConfig(path string) (*Config, error)` that:
1. Checks for config at `path` (if provided) → `~/.mantismo/config.toml` → defaults
2. Merges environment variables (`MANTISMO_DATA_DIR`, `MANTISMO_LOG_LEVEL`, etc.)
3. Returns fully populated Config with sensible defaults

Default data directory: `~/.mantismo/` (or `$XDG_DATA_HOME/mantismo/` if set on Linux).

### 3.9 README.md

Create a README with:
- Project name and one-liner
- "Work in progress" badge
- Quick start (build from source)
- Architecture overview (link to docs)
- License: Apache 2.0

## Acceptance Criteria

- [ ] `make build` produces a binary at `bin/mantismo`
- [ ] `./bin/mantismo --version` prints version
- [ ] `./bin/mantismo wrap` prints "Not implemented yet"
- [ ] `./bin/mantismo --help` lists all subcommands
- [ ] `make test` passes (including config loading tests)
- [ ] `make lint` passes with zero warnings
- [ ] Config loads from TOML file, falls back to defaults
- [ ] All `internal/` packages have placeholder files that compile
