# Technician – Agent guide

Technician is a **synthetic monitoring orchestrator**: it runs health-check probes from configurable sites, exports metrics to Prometheus and traces via OTLP, and supports performance budgets and browser (Playwright) flows.

## Repository layout

| Path | Purpose |
|------|--------|
| `cmd/` | Cobra CLI: `root`, `worker`, `probe`, `serve`, `validate` |
| `internal/config/` | Main YAML config + probe definitions; env var expansion `${VAR}` |
| `internal/probe/` | Probe implementations: HTTP, SMTP, traceroute, Playwright |
| `internal/scheduler/` | Cron-based scheduling with per-site stagger |
| `internal/metrics/` | Prometheus gauges, OTLP trace export, HAR parsing |
| `internal/artifact/` | Artifact backends: local, S3, stdout, noop |
| `internal/budget/` | YAML budgets, threshold evaluation, reporters (text, JSON, GHA) |
| `internal/exporter/` | Blackbox-exporter–compatible `/probe` handler |
| `internal/playwright/` | Go orchestrator + embedded Node.js run script |
| `examples/` | Sample `technician.yml`, `probes/`, `budgets.yml` |
| `dashboards/` | Grafana JSON dashboards |
| `prometheus/` | Prometheus config, rules, Grafana provisioning |

## Commands

- **`technician worker`** – Long-running worker: loads config + probes, runs scheduler, serves `/metrics`, `/probe`, `/health`.
- **`technician probe --name <name>`** – Run a single probe by name (for debugging).
- **`technician serve`** – Serve-only mode (metrics/health).
- **`technician validate`** – Run all probes, evaluate budgets, exit 0/1 (CI).

Flags: `--config` / `-c` (default `technician.yml`), `--site` (or `SITE_CODE`), `--verbose` / `-v`.

## Configuration

- **Main config**: `technician.yml` – `service`, `hostname`, `sites`, `metrics`, `artifacts`, `playwright`.
- **Probes**: Loaded from directory next to config: `<config_dir>/probes/`:
  - `http.yml`, `smtp.yml`, `traceroute.yml` – list of probes per type.
  - Playwright: `probes/playwright/playwright.yml` (or `probes/playwright.yml`) + script files.
- **Budgets**: Optional `budgets.yml` next to main config (used by `validate`).
- All YAML supports `${ENV_VAR}` expansion.

## Probe model

- **Interface**: `internal/probe.Prober`: `Type() config.ProbeType` and `Run(ctx, cfg, site) *Result`.
- **Types**: `http`, `smtp`, `traceroute`, `playwright`.
- **Result**: `probe.Result` – `Name`, `Type`, `Success`, `Duration`, `Error`, plus type-specific fields (e.g. HTTP timings, WebVitals, HAR, traceroute hops). `Labels` are populated from `site.Labels()`. **Browser (WebVitals)**: Core Web Vitals are LCP (≤2.5s), INP (≤200ms), CLS (≤0.1). See `docs/core-web-vitals.md`.
- **Adding a probe type**: Implement `Prober`; add `ProbeType` and config struct in `internal/config/probes.go`; add loader in `LoadProbes`; register in `cmd/worker.go` (and `validate`/serve paths if needed); record metrics in `internal/metrics/prometheus.go` (and OTLP if desired).

## Sites and deployment

- **Sites** are defined in `technician.yml` (`code`, `city`, `country`, `geohash`, `provider`). Example config has `local` (provider `docker`) for local/Docker, and `us-east-1` / `us-west-2` (provider `aws`) for US regions. `SITE_CODE` (env or `--site`) selects which site labels are emitted; unset or unknown falls back to first site.
- **Local/Docker**: Use `SITE_CODE=local`; the example config includes a `local` site so labels stay distinct. Prometheus in compose scrapes `technician:9394` (Docker service name = hostname on the compose network).
- **VPC / central observability**: Deployed workers in a VPC are scraped by central Prometheus (static or service discovery). Central Grafana uses that Prometheus as source of record. See `docs/architecture/central-prometheus-grafana.md` for scrape config, discovery, and optional edge push.
- **Edge (Workers, Lambda)**: Site/location is derived from the platform at request time (e.g. Cloudflare colo, AWS region). See `docs/proposals/site-identifiers-edge.md`.

## Conventions

- **Logging**: `log/slog`; level set by `--verbose` in root command.
- **Errors**: Return with `fmt.Errorf("context: %w", err)` for wrapping.
- **Tests**: Standard library `testing`; use httptest/mocks where appropriate; probe tests live next to implementation (e.g. `http_test.go`).
- **Go**: Module `github.com/jesseheady/technician`; keep `internal/` for non-exported code.
- **Traceroute**: Uses `mtr --json`. Parser skips leading non-JSON (find first `{`) for builds that print progress; if no JSON is found, error includes a snippet of stdout. On host, mtr often needs root (raw sockets); in Docker the process runs as root by default.

## Run and test

```bash
go run . worker --config examples/technician.yml
go test ./...
go vet ./...
```

Docker: `docker compose up` (Technician + Prometheus + Grafana). Rebuild after code changes: `docker compose build technician`.

## Documentation

- `docs/getting-started.md` – Prerequisites, init script (Mac), run worker and full stack.
- `docs/architecture/central-prometheus-grafana.md` – Central Prometheus (VPC), Grafana as source of record, local vs phone-home, edge push.
- `docs/proposals/site-identifiers-edge.md` – Site identifiers when probes run on Workers or Lambda.
- `docs/proposals/cloudflare-workers.md` – Cloudflare Workers and AWS options (Health Checks, Synthetics, Lambda).
- `docs/README.md` – Index of testing, mock production, proposals.

## CI

- `.github/workflows/canary.yml` – canary synthetic check in CI.

When editing code, preserve existing patterns: probe results feed metrics and (where applicable) OTLP; new metrics should follow the naming in `internal/metrics/prometheus.go`; budget metric names must match threshold keys in `budgets.yml`. Playwright prober exists but is not yet registered in `cmd/worker.go`; add it there (and in `validate` if desired) to run browser probes.
