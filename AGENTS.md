# Technician – Agent guide

Technician is a **multi-region check runner** — a single Go binary that checks your infrastructure over the network. Thirteen check types, Prometheus metrics on `/metrics`, OTLP traces, performance budgets for CI, and Playwright browser flows.

## Repository layout

| Path | Purpose |
|------|--------|
| `cmd/` | Cobra CLI: `root`, `worker`, `check`, `serve`, `validate` |
| `internal/config/` | Main YAML config + check definitions; env var expansion `${VAR}` |
| `internal/check/` | Check implementations: HTTP, TCP, UDP, DNS, ICMP, gRPC, NTP, TLS, SMTP, traceroute, BGP, domain expiry, Playwright |
| `internal/scheduler/` | Cron-based scheduling with per-site stagger |
| `internal/metrics/` | Prometheus gauges, OTLP trace export, HAR parsing |
| `internal/artifact/` | Artifact backends: local, S3, stdout, noop |
| `internal/budget/` | YAML budgets, threshold evaluation, reporters (text, JSON, GHA) |
| `internal/exporter/` | Blackbox-exporter–compatible `/probe` handler |
| `internal/notify/` | Webhook notifications: Discord, Slack, generic HTTP |
| `internal/status/` | In-memory check store, budget badge tracking, status page + JSON API |
| `internal/playwright/` | Go orchestrator + embedded Node.js run script |
| `config/` | Local/production configs (gitignored — copy from `examples/`) |
| `examples/` | Reference configs with placeholder values |
| `dashboards/` | Grafana JSON dashboards |
| `prometheus/` | Prometheus config, rules, Grafana provisioning |

## Commands

- **`technician worker`** – Long-running worker: loads config + checks, runs scheduler, serves `/metrics`, `/probe`, `/health`.
- **`technician check --name <name>`** – Run a single check by name (for debugging).
- **`technician serve`** – Serve-only mode (metrics/health).
- **`technician validate`** – Run all checks, evaluate budgets, exit 0/1 (CI).
- **`technician test-webhook`** – Send a test notification to all configured webhooks.

Flags: `--config` / `-c` (default `technician.yml`), `--site` (or `SITE_CODE`), `--verbose` / `-v`.

## Configuration

- **Main config**: `technician.yml` – `service`, `hostname`, `sites`, `metrics`, `artifacts`, `playwright` (mode, server_url, max_browsers), `webhooks`.
- **Checks**: Loaded from directory next to config: `<config_dir>/checks/`:
  - `http.yml`, `tcp.yml`, `udp.yml`, `dns.yml`, `icmp.yml`, `grpc.yml`, `ntp.yml`, `tls.yml`, `smtp.yml`, `traceroute.yml`, `bgp.yml`, `domain_expiry.yml` – list of checks per type. HTTP checks support `assertions` (body: contains, not_contains, regex; header: header_contains, header_not_contains, header_regex) and `follow_redirects`.
  - Playwright: `checks/playwright/playwright.yml` (or `checks/playwright.yml`) + script files.
  - All check types support optional `retry` (count, backoff, delay) and `degraded_after` (duration threshold).
- **Budgets**: Optional `budgets.yml` next to main config (used by `validate`).
- **Webhooks**: Optional `webhooks` list in `technician.yml`. Each entry has `url`, `type` (discord/slack/generic), `events` (check_down/check_up/budget_violation/cert_expiring), `severities` (warning/critical — omit for all), and `cooldown` (default 5m). Notifications fire on state transitions, not every check run. Events carry severity: check_down=critical, budget_violation=warning, cert_expiring=warning or critical based on days vs thresholds. Multiple webhook entries with different `severities` filters enable routing warnings to Slack and critical to PagerDuty.
- **Config layout**: `examples/` has reference configs with placeholder values (checked in). Copy to `config/` for local/production use (gitignored). Docker Compose mounts from `config/`.
- All YAML supports `${ENV_VAR}` expansion.

## Alerting

Three strategies (see `docs/alerting.md`):

1. **Grafana alerting** (recommended) – Native contact points for Discord, Slack, PagerDuty, etc. UI for silencing, grouping, history.
2. **Native webhooks** – Direct from Technician via `webhooks` config. Simple, no external stack needed. Fires on check state transitions, new budget violations, and TLS cert expiry warnings with per-check cooldown. Supports severity-based routing (`severities` filter) to fork warnings and critical alerts to different channels.
3. **Prometheus Alertmanager** – Rule-based routing via `prometheus/rules.yml` and `prometheus/alertmanager.yml`. Discord requires a bridge container (`alertmanager-discord`); Slack/PagerDuty/Email work natively.

## Status page and persistence

- **Status page**: HTML at `/`, JSON API at `/api/status`. Shows check health, history bars, latency percentiles (P50/P90/P95/P99), HTTP timing breakdown (DNS/TLS/TTFB/transfer as stacked bar), budget badges (pass/fail per metric), and overall status (operational/degraded/down).
- **Persistence**: Check history (90-entry ring buffer per check), down-since timestamps, and budget check state are persisted to `$TECHNICIAN_DATA_DIR/status.json` (default `/var/lib/technician/status.json`). Budget badges survive restarts.

## Check model

- **Interface**: `internal/check.Checker`: `Type() config.CheckType` and `Run(ctx, cfg, site) *Result`.
- **Types**: `http`, `tcp`, `udp`, `dns`, `icmp`, `grpc`, `ntp`, `tls`, `smtp`, `traceroute`, `bgp`, `domain_expiry`, `playwright`.
- **Result**: `check.Result` – `Name`, `Type`, `Success`, `Duration`, `Error`, `Degraded`, plus type-specific fields (HTTP timings/assertions, TCP conn/TLS durations, UDP RTT/response bytes, DNS answers/query time, ICMP packet loss/RTT stats, gRPC status, NTP offset/stratum/RTT, WebVitals, HAR, traceroute hops). `Labels` are populated from `site.Labels()`. **Browser (WebVitals)**: Core Web Vitals are LCP (≤2.5s), INP (≤200ms), CLS (≤0.1). See `docs/core-web-vitals.md`.
- **Adding a check type: Implement `Checker`; add `CheckType` and config struct in `internal/config/checks.go`; add loader in `LoadChecks`; register in `cmd/worker.go` (and `validate`/serve paths if needed); record metrics in `internal/metrics/prometheus.go` (and OTLP if desired).

## Sites and deployment

- **Sites** are defined in `technician.yml` (`code`, `city`, `country`, `location_hash`, `infra_provider`). Example config has `local` (infra_provider `docker`) for local/Docker, and `us-east-1` / `us-west-2` (infra_provider `aws`) for US regions. `SITE_CODE` (env or `--site`) selects which site labels are emitted; unset or unknown falls back to first site.
- **Local/Docker**: Use `SITE_CODE=local`; the example config includes a `local` site so labels stay distinct. Prometheus in compose scrapes `technician:9590` (Docker service name = hostname on the compose network).
- **VPC / central observability**: Deployed workers in a VPC are scraped by central Prometheus (static or service discovery). Central Grafana uses that Prometheus as source of record. See `docs/architecture/central-prometheus-grafana.md` for scrape config, discovery, and optional edge push.
- **Edge (Workers, Lambda)**: Site/location is derived from the platform at request time (e.g. Cloudflare colo, AWS region). See `docs/proposals/site-identifiers-edge.md`.

## Conventions

- **Logging**: `log/slog`; level set by `--verbose` in root command.
- **Errors**: Return with `fmt.Errorf("context: %w", err)` for wrapping.
- **Tests**: Standard library `testing`; use httptest/mocks where appropriate; check tests live next to implementation (e.g. `http_test.go`).
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
- `docs/playwright-scaling.md` – Browser check resource analysis, concurrency controls (`max_browsers`), dedicated runner architecture.
- `docs/deployment-sizing.md` – Resource requirements, VPS/Docker/Lambda/Workers sizing, worker-only deployment guide.
- `docs/architecture/central-prometheus-grafana.md` – Central Prometheus (VPC), Grafana as source of record, local vs phone-home, edge push.
- `docs/proposals/site-identifiers-edge.md` – Site identifiers when checks run on Workers or Lambda.
- `docs/proposals/cloudflare-workers.md` – Cloudflare Workers and AWS options (Health Checks, Synthetics, Lambda).
- `docs/README.md` – Full index of all documentation.

## CI and workflows

- `.github/workflows/ci.yml` – Build, test, lint, validate (with and without Playwright), security scan (govulncheck), Docker build. Runs on push to main and PRs. Skips for docs-only changes (paths-ignore on `docs/`, `dashboards/`, `examples/`, `scripts/`, `*.md`, `LICENSE`, `.github/dependabot.yml`). A `CI Passed` gate job aggregates all results for branch protection.
- `.github/workflows/canary.yml` – Canary synthetic check post-deployment.
- `.github/workflows/release.yml` – Triggered on `v*` tag push. Builds binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64. Builds and pushes multi-arch Docker image to GHCR (`ghcr.io/<repo>/technician:<tag>` and `:latest`). Creates a GitHub Release with binaries attached.
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

## Tracking completed work

When a GitHub issue, roadmap item, or todo is completed:

1. Close the GitHub issue.
2. Move the item from its current roadmap section to "Recently completed" in `docs/roadmap.md` with a short summary of what shipped.
3. If the item was on a todo list, check it off or remove it.

All three locations (issues, roadmap, todos) must stay in sync. The "Recently completed" section in the roadmap feeds into release notes when tagging a new version.

## Companion edits

Every code change should include a review of related docs, tests, and examples in the same branch and PR. Before marking a feature branch as ready:

- **Docs** — Check `docs/deployment-sizing.md`, `docs/getting-started.md`, `docs/ci.md`, `README.md`, and architecture docs for any claims affected by the change (binary sizes, image sizes, check counts, resource estimates, version numbers, diagrams).
- **Tests** — Add or update tests that cover the changed behavior.
- **Examples** — Update `examples/` configs if the change adds, removes, or renames config fields.
- **Internal docs** — Update `docs/internal/` comparison docs if the change affects feature parity, resource footprint, or cost estimates.
- **AGENTS.md** — Update this file if the change affects workflows, CI, deployment, or contributor-facing processes.

Do not push docs-only or test-only changes directly to main. Bundle them with the feature branch so the PR captures the full scope of the change.

## Commit style

Single-line commit messages. No multi-line body. No Co-Authored-By trailers.

## Config changes vs code changes

- Config changes (`config/`, `technician.yml`, `checks/*.yml`): `docker compose restart technician`. No rebuild needed; config is volume-mounted.
- Code changes (Go source, Dockerfile): `docker compose build technician && docker compose up`.

## Security

- `SECURITY.md` directs vulnerability reports to GitHub's private vulnerability reporting (Security tab). No public issues for security reports.
- Private vulnerability reporting is enabled on the repo.
- Dependabot alerts and security updates are enabled.

## Check schedule guidance

Not all checks need the same frequency. Recommended production intervals:

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

When editing code, preserve existing patterns: check results feed metrics and (where applicable) OTLP; new metrics should follow the naming in `internal/metrics/prometheus.go`; budget metric names must match threshold keys in `budgets.yml`.
