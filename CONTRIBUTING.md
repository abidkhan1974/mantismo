# Contributing to Mantismo

Thank you for your interest in contributing. Mantismo is an early-stage
project and contributions are welcome.

## Before You Start

### Sign the CLA

All contributors must sign the [Contributor License Agreement](CLA.md)
before their pull request can be merged.

To sign, add this comment to your pull request:

> I have read the CLA document and I hereby sign the CLA.

This is a one-time requirement. You do not need to sign again for
future contributions.

### Why a CLA?

Mantismo is dual-licensed (AGPL-3.0 and commercial). The CLA ensures
the Maintainer can continue to offer both licenses and keeps the
project's IP clean — which protects the long-term sustainability of
the open-source version.

---

## How to Contribute

### Reporting bugs

Open a [GitHub issue](https://github.com/abidkhan1974/mantismo/issues).
Include:
- Mantismo version (`mantismo --version`)
- OS and architecture
- Steps to reproduce
- What you expected vs what happened

For security vulnerabilities, see [SECURITY.md](SECURITY.md) — do not
open a public issue.

### Suggesting features

Open a GitHub issue with the `enhancement` label. Describe the use
case, not just the solution.

### Submitting code

1. Fork the repository
2. Create a feature branch: `git checkout -b your-feature`
3. Make your changes
4. Run the test suite: `make test`
5. Run the linter: `make lint`
6. Open a pull request with a clear description
7. Add the CLA sign-off comment if this is your first contribution

### Code style

- Standard Go formatting (`gofmt`)
- Tests required for new functionality
- Keep commits focused — one logical change per commit

---

## Development Setup

**Prerequisites:** Go 1.22+, make

```bash
git clone https://github.com/abidkhan1974/mantismo
cd mantismo
make build        # build binary
make test         # run tests
make lint         # run linter
./bin/mantismo --version
```

---

## Questions

Open a GitHub issue or email abidkhan1974@gmail.com.
