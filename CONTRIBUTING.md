# Contributing

1. Fork, clone, and install Go 1.26+. Optionally Node.js 22+ (Playwright probes) and mtr (traceroute).
2. Run `./scripts/init-mac.sh` to set up dependencies and git hooks.
3. `cp -r examples/ config/` then `go run . worker --config config/technician.yml`.
4. Create a branch and make your change.
5. Open a PR with a clear description. CI runs build, test, lint, and budget validation.

See [docs/getting-started.md](docs/getting-started.md) for the full setup guide and [AGENTS.md](AGENTS.md) for architecture, conventions, and the probe model.

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

## Code style

- **Logging**: `log/slog`. **Errors**: `fmt.Errorf("context: %w", err)`.
- **Tests**: Standard library `testing`, `httptest` for mocks. Tests live next to implementation.
- **Packages**: Non-exported code in `internal/`. New metrics follow naming in `internal/metrics/prometheus.go`.

## Adding a probe type

1. Implement `probe.Prober` (`Type()` and `Run()`).
2. Add `ProbeType` and config struct in `internal/config/probes.go`, loader in `LoadProbes`.
3. Register in `cmd/worker.go` and record metrics in `internal/metrics/prometheus.go`.
4. Add example config in `examples/probes/` and update the probe table in [docs/getting-started.md](docs/getting-started.md).

## Issues

Include: what you expected, what happened, steps to reproduce, Go version, OS, and Docker (yes/no).
