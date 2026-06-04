# Contributing

1. Fork, clone, and install Go 1.26+. Optionally Node.js 22+ (Playwright checks) and mtr (traceroute).
2. Run `./scripts/init-mac.sh` to set up dependencies and git hooks.
3. `cp -r examples/ config/` then `go run . worker --config config/technician.yml`.
4. Create a branch and make your change.
5. Open a PR with a clear description. CI runs build, test, lint, and budget validation.

See [docs/getting-started.md](docs/getting-started.md) for the full setup guide and [AGENTS.md](AGENTS.md) for architecture, conventions, and the check model.

## Pre-commit checks

The init script configures a pre-commit hook (`.githooks/pre-commit`) that runs automatically on every commit:

- `go build ./...` — compilation check
- `go vet ./...` — static analysis
- `go test -race ./...` — full test suite with race detector
- `govulncheck ./...` — vulnerability scan (skipped if not installed)

These mirror the CI pipeline, so issues are caught before pushing. To install `govulncheck` locally:

```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
```

## Dependency licenses

All Go dependencies must be permissively licensed (Apache-2.0, MIT, or BSD).
CI (`.github/workflows/licenses.yml`) and the pre-push hook fail if a dependency
carries a copyleft license, so nothing extra is needed when adding a dependency.

The aggregated attribution notice (`THIRD_PARTY_LICENSES.txt`) is **generated at
build and release time** — baked into the Docker image and attached to each
GitHub release — so it always matches the shipped binary and is never committed.
To inspect it locally:

```bash
./scripts/gen-licenses.sh   # writes THIRD_PARTY_LICENSES.txt (gitignored)
```

## Code style

- **Logging**: `log/slog`. **Errors**: `fmt.Errorf("context: %w", err)`.
- **Tests**: Standard library `testing`, `httptest` for mocks. Tests live next to implementation.
- **Packages**: Non-exported code in `internal/`. New metrics follow naming in `internal/metrics/prometheus.go`.

## Adding a check type

1. Implement `check.Checker` (`Type()` and `Run()`).
2. Add `CheckType` and config struct in `internal/config/checks.go`, loader in `LoadChecks`.
3. Register in `cmd/worker.go` and record metrics in `internal/metrics/prometheus.go`.
4. Add example config in `examples/checks/` and update the check table in [docs/getting-started.md](docs/getting-started.md).

## Issues

Include: what you expected, what happened, steps to reproduce, Go version, OS, and Docker (yes/no).
