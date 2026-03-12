# Testing and end-to-end validation

How to run unit tests, integration-style checks, and full end-to-end validation for Technician.

## Unit tests

Run all tests:

```bash
go test ./...
```

Run with verbose output and race detector:

```bash
go test -v -race ./...
```

Run tests for a single package:

```bash
go test ./internal/probe/...
go test ./internal/budget/...
go test ./internal/config/...
```

Coverage (report to stdout):

```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

Coverage HTML:

```bash
go tool cover -html=coverage.out -o coverage.html
```

## Lint and vet

```bash
go vet ./...
```

Add static analysis (optional):

```bash
go install golang.org/x/lint/golint@latest
golint ./...
```

## Test layout

| Package            | Focus |
|--------------------|--------|
| `internal/probe`   | HTTP (httptest), SMTP, traceroute, behavior |
| `internal/config`  | YAML loading, env expansion, defaults |
| `internal/budget`  | Threshold evaluation, wildcards |
| `internal/metrics` | HAR parsing, recording |
| `internal/scheduler` | Stagger, cron |
| `internal/artifact` | Store interfaces |
| `internal/exporter` | Blackbox handler |

Tests live next to code (`*_test.go`). Use `httptest.NewServer` for HTTP probes; mock or skip external dependencies (e.g. SMTP, mtr) when not available.

## End-to-end validation methods

### 1. Validate command (probes + budgets)

Runs all configured probes once and evaluates budgets. Use this in CI and for local e2e:

```bash
go run . validate --config config/technician.yml --budget config/budgets.yml
```

- **Exit 0**: All probes ran and no budget violations.
- **Exit 1**: One or more violations (or probe load/run failure).

Use a **dedicated e2e config** that points at stable or mock targets so results are deterministic (see [Mock production](mock-production.md)).

### 2. Single-probe run

Sanity-check one probe by name:

```bash
go run . probe --name "Example Website" --config config/technician.yml
```

Inspect stdout for success/failure and timing fields.

### 3. Worker + metrics scrape

1. Start the worker with a config that includes your probes:
   ```bash
   go run . worker --config config/technician.yml -v
   ```
2. Wait at least one schedule interval (e.g. 30s for `*/30 * * * * *`).
3. Curl metrics and check for non-zero counters/gauges:
   ```bash
   curl -s http://localhost:9394/metrics | grep technician_
   ```

### 4. Blackbox-style probe endpoint

Hit the `/probe` endpoint (same semantics as Prometheus blackbox_exporter):

```bash
curl -s "http://localhost:9394/probe?target=https://example.com&module=http_2xx"
```

Expect Prometheus exposition format with `technician_probe_up` and timing metrics.

### 5. Docker Compose stack

Full stack e2e:

```bash
docker compose up -d
# Wait for scrape + evaluation (e.g. 1–2 minutes)
curl -s http://localhost:9090/api/v1/query?query=technician_probe_duration_seconds | jq .
# Optional: open Grafana and confirm dashboards show data
docker compose down
```

### 6. CI canary (GitHub Actions)

The canary workflow runs `technician validate` inside the built image with a canary config and budgets. Use it as the reference e2e:

- Trigger via `workflow_dispatch` with optional `target_url`.
- Or on `deployment_status` with the deployment environment URL.

See [.github/workflows/canary.yml](../.github/workflows/canary.yml).

## Recommended e2e flow for changes

1. `go test ./...` and `go vet ./...` on every change.
2. Before merge: run `validate` with a config that uses stable or mock targets.
3. Optionally run the full Docker stack and spot-check Grafana/Prometheus.
4. Rely on the canary workflow in CI for deployment-time validation.

## Adding e2e coverage

- Add or extend probe definitions in `examples/probes/` (or a dedicated `e2e/` config dir) that hit known-good or mock endpoints.
- Add a minimal `budgets.yml` for e2e so `validate` has clear pass/fail (e.g. relaxed thresholds for mock targets).
- Document any required environment (e.g. `CANARY_URL`) in this doc or in the workflow file.
