# 16 — Spec: Release Packaging

## Objective

Configure cross-compilation, release automation, and distribution so users can install Mantismo with a single command.

## Distribution Targets

| Platform | Architecture | Priority |
|----------|-------------|----------|
| macOS | arm64 (Apple Silicon) | P0 |
| macOS | amd64 (Intel) | P0 |
| Linux | amd64 | P0 |
| Linux | arm64 | P1 |

Windows is explicitly out of scope for MVP.

## GoReleaser Configuration (`.goreleaser.yml`)

```yaml
version: 2

project_name: mantismo

before:
  hooks:
    - go mod tidy

builds:
  - id: mantismo
    main: ./cmd/mantismo/
    binary: mantismo
    env:
      - CGO_ENABLED=1  # Required for SQLCipher
    goos:
      - darwin
      - linux
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w
      - -X main.version={{.Version}}
      - -X main.commit={{.Commit}}
      - -X main.date={{.Date}}

archives:
  - id: default
    format: tar.gz
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"

checksum:
  name_template: "checksums.txt"

changelog:
  sort: asc
  filters:
    exclude:
      - "^docs:"
      - "^test:"
      - "^ci:"

brews:
  - repository:
      owner: abidkhan1974
      name: homebrew-tap
    homepage: "https://github.com/abidkhan1974/mantismo"
    description: "Eyes on every agent"
    license: "Apache-2.0"
    install: |
      bin.install "mantismo"
    test: |
      system "#{bin}/mantismo", "--version"
```

## Installation Methods

### Homebrew (macOS + Linux)

```bash
brew tap abidkhan1974/homebrew-tap
brew install mantismo
```

### Direct Download

```bash
# macOS Apple Silicon
curl -sSL https://github.com/abidkhan1974/mantismo/releases/latest/download/mantismo_darwin_arm64.tar.gz | tar xz
sudo mv mantismo /usr/local/bin/

# Linux amd64
curl -sSL https://github.com/abidkhan1974/mantismo/releases/latest/download/mantismo_linux_amd64.tar.gz | tar xz
sudo mv mantismo /usr/local/bin/
```

### Install Script

Create `install.sh`:
```bash
#!/bin/sh
set -e

REPO="abidkhan1974/mantismo"
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

LATEST=$(curl -sSL "https://api.github.com/repos/$REPO/releases/latest" | grep tag_name | cut -d'"' -f4)
URL="https://github.com/$REPO/releases/download/$LATEST/mantismo_${LATEST#v}_${OS}_${ARCH}.tar.gz"

echo "Installing Mantismo $LATEST for $OS/$ARCH..."
curl -sSL "$URL" | tar xz -C /tmp
sudo mv /tmp/mantismo /usr/local/bin/
echo "Mantismo installed successfully. Run 'mantismo --version' to verify."
```

Usage: `curl -sSL https://raw.githubusercontent.com/abidkhan1974/mantismo/main/install.sh | sh`

## CGo Cross-Compilation (SQLCipher)

SQLCipher requires CGo, which complicates cross-compilation. Options:

### Option A: Docker-based Cross-Compilation (Recommended)

Use `goreleaser-cross` Docker image which has cross-compilation toolchains:

```yaml
# .github/workflows/release.yml
jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: goreleaser/goreleaser-action@v5
        with:
          distribution: goreleaser-cross
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

### Option B: Pure-Go SQLite (Fallback)

If CGo cross-compilation proves too painful, switch from `go-sqlcipher` to `modernc.org/sqlite` (pure Go SQLite) with application-layer encryption. This eliminates CGo entirely but requires implementing envelope encryption manually.

Decision: start with Option A. Fall back to B if release engineering becomes a bottleneck.

## GitHub Actions Release Workflow

```yaml
# .github/workflows/release.yml
name: Release
on:
  push:
    tags:
      - 'v*'

jobs:
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: write
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
      - uses: goreleaser/goreleaser-action@v5
        with:
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

## Post-Install Verification

`mantismo doctor` command (lightweight, add to CLI):
```
$ mantismo doctor

Mantismo v0.2.0 — System Check
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

✓ Binary installed at /usr/local/bin/mantismo
✓ Data directory writable (~/.mantismo/)
✓ Python available (for mock server testing)
✓ SQLCipher encryption working
✓ OPA policy engine loaded

Ready to use. Run 'mantismo wrap -- <your mcp server>' to get started.
```

## Acceptance Criteria

- [ ] `goreleaser` produces binaries for macOS (arm64, amd64) and Linux (amd64, arm64)
- [ ] Homebrew formula installs and runs correctly
- [ ] Install script detects OS/arch and installs correct binary
- [ ] Version, commit, and build date embedded in binary via ldflags
- [ ] `mantismo doctor` validates installation
- [ ] Release workflow triggers on git tag push
- [ ] Checksums published with every release
