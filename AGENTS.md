# Technician – Agent guide

Technician is a **multi-region probe runner** — a single Go binary that probes your infrastructure over the network. Thirteen probe types, Prometheus metrics on `/metrics`, OTLP traces, performance budgets for CI, and Playwright browser flows.

## Repository layout

| Path | Purpose |
|------|--------|
| `cmd/` | Cobra CLI: `root`, `worker`, `probe`, `serve`, `validate` |
| `internal/config/` | Main YAML config + probe definitions; env var expansion `${VAR}` |
| `internal/probe/` | Probe implementations: HTTP, TCP, UDP, DNS, ICMP, gRPC, NTP, TLS, SMTP, traceroute, BGP, domain expiry, Playwright |
| `internal/scheduler/` | Cron-based scheduling with per-site stagger |
| `internal/metrics/` | Prometheus gauges, OTLP trace export, HAR parsing |
| `internal/artifact/` | Artifact backends: local, S3, stdout, noop |
| `internal/budget/` | YAML budgets, threshold evaluation, reporters (text, JSON, GHA) |
| `internal/exporter/` | Blackbox-exporter–compatible `/probe` handler |
| `internal/notify/` | Webhook notifications: Discord, Slack, generic HTTP |
| `internal/status/` | In-memory probe store, budget badge tracking, status page + JSON API |
| `internal/playwright/` | Go orchestrator + embedded Node.js run script |
| `config/` | Local/production configs (gitignored — copy from `examples/`) |
| `examples/` | Reference configs with placeholder values |
| `dashboards/` | Grafana JSON dashboards |
| `prometheus/` | Prometheus config, rules, Grafana provisioning |

## Commands

- **`technician worker`** – Long-running worker: loads config + probes, runs scheduler, serves `/metrics`, `/probe`, `/health`.
- **`technician probe --name <name>`** – Run a single probe by name (for debugging).
- **`technician serve`** – Serve-only mode (metrics/health).
- **`technician validate`** – Run all probes, evaluate budgets, exit 0/1 (CI).
- **`technician test-webhook`** – Send a test notification to all configured webhooks.

Flags: `--config` / `-c` (default `technician.yml`), `--site` (or `SITE_CODE`), `--verbose` / `-v`.

## Configuration

- **Main config**: `technician.yml` – `service`, `hostname`, `sites`, `metrics`, `artifacts`, `playwright` (mode, server_url, max_browsers), `webhooks`.
- **Probes**: Loaded from directory next to config: `<config_dir>/probes/`:
  - `http.yml`, `tcp.yml`, `udp.yml`, `dns.yml`, `icmp.yml`, `grpc.yml`, `ntp.yml`, `tls.yml`, `smtp.yml`, `traceroute.yml`, `bgp.yml`, `domain_expiry.yml` – list of probes per type. HTTP probes support `assertions` (body: contains, not_contains, regex; header: header_contains, header_not_contains, header_regex) and `follow_redirects`.
  - Playwright: `probes/playwright/playwright.yml` (or `probes/playwright.yml`) + script files.
  - All probe types support optional `retry` (count, backoff, delay) and `degraded_after` (duration threshold).
- **Budgets**: Optional `budgets.yml` next to main config (used by `validate`).
- **Webhooks**: Optional `webhooks` list in `technician.yml`. Each entry has `url`, `type` (discord/slack/generic), `events` (probe_down/probe_up/budget_violation/cert_expiring), `severities` (warning/critical — omit for all), and `cooldown` (default 5m). Notifications fire on state transitions, not every probe run. Events carry severity: probe_down=critical, budget_violation=warning, cert_expiring=warning or critical based on days vs thresholds. Multiple webhook entries with different `severities` filters enable routing warnings to Slack and critical to PagerDuty.
- **Config layout**: `examples/` has reference configs with placeholder values (checked in). Copy to `config/` for local/production use (gitignored). Docker Compose mounts from `config/`.
- All YAML supports `${ENV_VAR}` expansion.

## Alerting

Three strategies (see `docs/alerting.md`):

1. **Grafana alerting** (recommended) – Native contact points for Discord, Slack, PagerDuty, etc. UI for silencing, grouping, history.
2. **Native webhooks** – Direct from Technician via `webhooks` config. Simple, no external stack needed. Fires on probe state transitions, new budget violations, and TLS cert expiry warnings with per-probe cooldown. Supports severity-based routing (`severities` filter) to fork warnings and critical alerts to different channels.
3. **Prometheus Alertmanager** – Rule-based routing via `prometheus/rules.yml` and `prometheus/alertmanager.yml`. Discord requires a bridge container (`alertmanager-discord`); Slack/PagerDuty/Email work natively.

## Status page and persistence

- **Status page**: HTML at `/`, JSON API at `/api/status`. Shows probe health, history bars, latency percentiles (P50/P90/P95/P99), HTTP timing breakdown (DNS/TLS/TTFB/transfer as stacked bar), budget badges (pass/fail per metric), and overall status (operational/degraded/down).
- **Persistence**: Probe history (90-entry ring buffer per probe), down-since timestamps, and budget check state are persisted to `$TECHNICIAN_DATA_DIR/status.json` (default `/var/lib/technician/status.json`). Budget badges survive restarts.

## Probe model

- **Interface**: `internal/probe.Prober`: `Type() config.ProbeType` and `Run(ctx, cfg, site) *Result`.
- **Types**: `http`, `tcp`, `udp`, `dns`, `icmp`, `grpc`, `ntp`, `tls`, `smtp`, `traceroute`, `bgp`, `domain_expiry`, `playwright`.
- **Result**: `probe.Result` – `Name`, `Type`, `Success`, `Duration`, `Error`, `Degraded`, plus type-specific fields (HTTP timings/assertions, TCP conn/TLS durations, UDP RTT/response bytes, DNS answers/query time, ICMP packet loss/RTT stats, gRPC status, NTP offset/stratum/RTT, WebVitals, HAR, traceroute hops). `Labels` are populated from `site.Labels()`. **Browser (WebVitals)**: Core Web Vitals are LCP (≤2.5s), INP (≤200ms), CLS (≤0.1). See `docs/core-web-vitals.md`.
- **Adding a probe type**: Implement `Prober`; add `ProbeType` and config struct in `internal/config/probes.go`; add loader in `LoadProbes`; register in `cmd/worker.go` (and `validate`/serve paths if needed); record metrics in `internal/metrics/prometheus.go` (and OTLP if desired).

## Sites and deployment

- **Sites** are defined in `technician.yml` (`code`, `city`, `country`, `location_hash`, `infra_provider`). Example config has `local` (infra_provider `docker`) for local/Docker, and `us-east-1` / `us-west-2` (infra_provider `aws`) for US regions. `SITE_CODE` (env or `--site`) selects which site labels are emitted; unset or unknown falls back to first site.
- **Local/Docker**: Use `SITE_CODE=local`; the example config includes a `local` site so labels stay distinct. Prometheus in compose scrapes `technician:9590` (Docker service name = hostname on the compose network).
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
go run . worker --config config/technician.yml
go test ./...
go vet ./...
```

Docker: `docker compose up` (Technician + Prometheus + Grafana). Rebuild after code changes: `docker compose build technician`.

## Documentation

- `docs/alerting.md` – Native webhooks, Grafana alerting (recommended), Alertmanager.
- `docs/getting-started.md` – Prerequisites, init script (Mac), run worker and full stack.
- `docs/ci.md` – GitHub Actions workflow, generic CI pipelines, budget validation, Playwright in CI.
- `docs/playwright-scaling.md` – Browser probe resource analysis, concurrency controls (`max_browsers`), dedicated runner architecture.
- `docs/deployment-sizing.md` – Resource requirements, VPS/Docker/Lambda/Workers sizing, worker-only deployment guide.
- `docs/architecture/central-prometheus-grafana.md` – Central Prometheus (VPC), Grafana as source of record, local vs phone-home, edge push.
- `docs/proposals/site-identifiers-edge.md` – Site identifiers when probes run on Workers or Lambda.
- `docs/proposals/cloudflare-workers.md` – Cloudflare Workers and AWS options (Health Checks, Synthetics, Lambda).
- `docs/README.md` – Full index of all documentation.

## CI and workflows

- `.github/workflows/ci.yml` – Build, test, lint, validate (with and without Playwright), security scan (govulncheck), Docker build. Runs on push to main and PRs. Skips for docs-only changes (paths-ignore on `docs/`, `dashboards/`, `examples/`, `scripts/`, `*.md`, `LICENSE`, `.github/dependabot.yml`). A `CI Passed` gate job aggregates all results for branch protection.
- `.github/workflows/canary.yml` – Canary synthetic check post-deployment.
- `.github/workflows/release.yml` – Triggered on `v*` tag push. Builds binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64. Creates a GitHub Release with binaries attached.
- `.github/release.yml` – Changelog category config for auto-generated release notes. Categories PRs by label (enhancement, bug, performance, documentation, infrastructure, dependencies). PRs labeled `skip-changelog` are excluded.
- `.github/dependabot.yml` – Weekly dependency updates for Go modules and GitHub Actions.

## Pre-commit hook

`.githooks/pre-commit` runs on every commit: `go build`, `go vet`, `go test -race`, and `govulncheck` (skipped if not installed). This mirrors CI so issues are caught before pushing. The hook is configured automatically by `scripts/init-mac.sh` via `git config core.hooksPath .githooks`. New contributors must run the init script or set this manually.

## Branch protection

Main branch requires the `CI Passed` status check. Admin bypass is enabled for the maintainer to push directly. Contributors must open PRs that pass CI before merging.

## Releases

- Use annotated tags: `git tag -a v0.2.0 -m "Short summary"`.
- The release workflow builds multi-platform binaries and creates a GitHub Release with auto-generated notes.
- Auto-generated notes are categorized by PR labels (configured in `.github/release.yml`).
- For the tag annotation, write a short summary. For the GitHub Release, the auto-generated notes cover PR-level detail; add a hand-written summary at the top for major releases.
- First release was `v0.1.0`. Use semver: patch for fixes, minor for features, major for breaking changes. Stay on `v0.x.x` until stability guarantees are established.

## PR labels

Use labels on PRs for changelog categorization:

| Label | Use for |
|-------|---------|
| `enhancement` | New feature or improvement |
| `bug` | Bug fix |
| `performance` | Performance improvement |
| `documentation` | Docs changes |
| `infrastructure` | CI, build, deployment |
| `dependencies` | Dependency updates (Dependabot adds this automatically) |
| `skip-changelog` | Exclude from release notes |

## Commit style

Single-line commit messages. No multi-line body. No Co-Authored-By trailers.

## Config changes vs code changes

- Config changes (`config/`, `technician.yml`, `probes/*.yml`): `docker compose restart technician`. No rebuild needed; config is volume-mounted.
- Code changes (Go source, Dockerfile): `docker compose build technician && docker compose up`.

## Security

- `SECURITY.md` directs vulnerability reports to GitHub's private vulnerability reporting (Security tab). No public issues for security reports.
- Private vulnerability reporting is enabled on the repo.
- Dependabot alerts and security updates are enabled.

## Probe schedule guidance

Not all probes need the same frequency. Recommended production intervals:

| Category | Interval |
|----------|----------|
| Your own services (HTTP, gRPC) | 30s–1min |
| Infrastructure connectivity (TCP, ICMP, UDP) | 2min |
| DNS resolution | 5min |
| Third-party APIs | 5min |
| NTP | 10min |
| BGP, SMTP | 15min |
| Traceroute | 30min |
| TLS certificates, domain expiry | 6h |

When editing code, preserve existing patterns: probe results feed metrics and (where applicable) OTLP; new metrics should follow the naming in `internal/metrics/prometheus.go`; budget metric names must match threshold keys in `budgets.yml`.
