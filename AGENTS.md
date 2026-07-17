# Technician – Agent guide

Technician is a **multi-region check runner** — a single Go binary that checks your infrastructure over the network. Thirteen check types, Prometheus metrics on `/metrics`, OTLP traces, performance budgets for CI, and Playwright browser flows.

## Repository layout

| Path | Purpose |
|------|--------|
| `cmd/` | Cobra CLI: `root`, `worker`, `check`, `serve`, `validate` |
| `internal/config/` | Main YAML config + check definitions; env var expansion `${VAR}` |
| `internal/check/` | Check implementations: HTTP, TCP, UDP, DNS, ICMP, gRPC, NTP, TLS, SMTP, traceroute, BGP, domain expiry, Playwright |
| `internal/scheduler/` | Cron-expression scheduling (gronx + stdlib timer loop) with per-site stagger and a once-on-startup run |
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
- **`technician check run --check <name>`** – Run a single check by name (omit `--check` to run all); for debugging.
- **`technician serve`** – Serve-only mode (metrics/health).
- **`technician validate`** – Run all checks, evaluate budgets, exit 0/1 (CI).
- **`technician test-webhook`** – Send a test notification to all configured webhooks.

Flags: `--config` / `-c` (default `technician.yml`), `--origin` (or `ORIGIN_ID`), `--log-level` (debug, info, warn, error). `worker` and `check run` also accept `--types`/`--groups`/`--tags` (comma-separated) to run a subset of checks, overriding the `check_filter` config block per dimension.

## Configuration

- **Main config**: `technician.yml` – `service`, `hostname`, `origins`, `metrics` (prometheus: listen, max_check_cardinality; otel: endpoint), `artifacts`, `playwright` (mode, server_url, max_browsers), `webhooks`, `check_filter` (types/groups/tags — load-time subset so one checks dir serves many targets; see [docs/multi-target-deployment.md](docs/multi-target-deployment.md)).
- **Checks**: Loaded from directory next to config: `<config_dir>/checks.yml` (or `<config_dir>/checks/` directory):
  - `http.yml`, `tcp.yml`, `udp.yml`, `dns.yml`, `icmp.yml`, `grpc.yml`, `ntp.yml`, `tls.yml`, `smtp.yml`, `traceroute.yml`, `bgp.yml`, `domain_expiry.yml` – list of checks per type. HTTP checks support `assertions` (body: contains, not_contains, regex; header: header_contains, header_not_contains, header_regex), `follow_redirects`, `ip_version`, `min_tls`/`max_tls`, `basic_auth`/`bearer_token` (mutually exclusive), and `proxy`. TCP checks support `min_tls`/`max_tls` (with `tls: true`). SMTP checks support `start_tls`, `skip_tls`, and `username`/`password` auth (auth requires `start_tls`). DNS checks support SOA alongside A/AAAA/MX/TXT/CNAME/NS/SRV.
  - Playwright: `checks.yml` (or `checks/playwright.yml`) + script files.
  - All check types support optional `retry` (count, backoff, delay), `degraded_after` (duration threshold), `group`, and `tags` (used by `check_filter`).
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
- **Result**: `check.Result` – `Name`, `Type`, `Success`, `Duration`, `Error`, `Degraded`, plus type-specific fields (HTTP timings/assertions, TCP conn/TLS durations, UDP RTT/response bytes, DNS answers/query time, ICMP packet loss/RTT stats, gRPC status, NTP offset/stratum/RTT, WebVitals, HAR, traceroute hops). `Labels` are populated from `origin.MetricLabels()`. **Browser (WebVitals)**: Core Web Vitals are LCP (≤2.5s), INP (≤200ms), CLS (≤0.1). See `docs/core-web-vitals.md`.
- **Adding a check type: Implement `Checker`; add `CheckType` and config struct in `internal/config/checks.go`; add loader in `LoadChecks`; register in `cmd/worker.go` (and `validate`/serve paths if needed); record metrics in `internal/metrics/prometheus.go` (and OTLP if desired).

## Sites and deployment

- **Sites** are defined in `technician.yml` (`code`, `city`, `country`, `geohash`, `platform`). Example config has `local` (platform `docker`) for local/Docker, and `us-east-1` / `us-west-2` (platform `aws`) for US regions. `ORIGIN_ID` (env or `--origin`) selects which origin labels are emitted; unset or unknown falls back to first site.
- **Local/Docker**: Use `ORIGIN_ID=local`; the example config includes a `local` site so labels stay distinct. Prometheus in compose scrapes `technician:9590` (Docker service name = hostname on the compose network).
- **VPC / central observability**: Deployed workers in a VPC are scraped by central Prometheus (static or service discovery). Central Grafana uses that Prometheus as source of record. See `docs/architecture/central-prometheus-grafana.md` for scrape config, discovery, and optional edge push.
- **Edge (Workers, Lambda)**: Site/location is derived from the platform at request time (e.g. Cloudflare colo, AWS region). See `docs/proposals/site-identifiers-edge.md`.

## Conventions

- **Logging**: `log/slog`; level set by `--log-level` in root command.
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
- `docs/getting-started.md` – Three onboarding paths (evaluate via Docker, contribute via `init.sh`, run in production), prerequisites, and check configuration.
- `docs/ci.md` – GitHub Actions workflow, generic CI pipelines, budget validation, Playwright in CI.
- `docs/playwright-scaling.md` – Browser check resource analysis, concurrency controls (`max_browsers`), dedicated runner architecture.
- `docs/deployment-sizing.md` – Resource requirements, VPS/Docker/Lambda/Workers sizing, worker-only deployment guide.
- `docs/architecture/central-prometheus-grafana.md` – Central Prometheus (VPC), Grafana as source of record, local vs phone-home, edge push.
- `docs/proposals/site-identifiers-edge.md` – Site identifiers when checks run on Workers or Lambda.
- `docs/proposals/cloudflare-workers.md` – Cloudflare Workers and AWS options (Health Checks, Synthetics, Lambda).
- `docs/README.md` – Full index of all documentation.

## CI and workflows

- `.github/workflows/ci.yml` – Build, test, lint, validate (with and without Playwright), security scan (govulncheck), Docker build. Runs on push to main and PRs. A `changes` filter job skips heavy jobs for docs-only changes. A `CI Passed` gate job aggregates all results for branch protection.
- `.github/workflows/release.yml` – Triggered on `v*` tag push. Builds binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64. Builds and pushes the multi-arch Docker image to GHCR (`ghcr.io/jesseheady/technician:<tag>` and `:latest`). Creates a GitHub Release with binaries attached. **While the repo is private, do not rely on this to publish images:** no GHCR package exists yet, so new tags must be pushed manually (build + `docker push ghcr.io/jesseheady/technician:<tag>`) until the repo goes public. **Flip the repo public before pushing the first `v*` tag.** A GHCR package inherits its visibility from the repo at creation time, so tagging while private creates a private package, and every `docker pull` of it then fails with `unauthorized` — including the one the README documents — until the package is manually flipped public in its settings. The base `docker-compose.yml` builds from source; the `docker-compose.prod.yml` overlay pulls this image for operators.
- `.github/workflows/cache-cleanup.yml` – On PR close, deletes the caches scoped to that PR's `refs/pull/<n>/merge` ref (via `gh api`) so merged/closed PRs stop holding cache space instead of waiting for the 7-day/10 GB eviction.
- `.github/workflows/trivy-ignore-audit.yml` – Weekly, reconciles `.trivyignore.yaml` deferrals: when an entry's `expired_at` is within 14 days it re-scans the image and either opens a PR to remove the entry (CVE fixed) or a re-triage issue (still vulnerable). Logic in `scripts/trivy-ignore-audit.sh`.
- `.github/release.yml` – Changelog category config for auto-generated release notes. Categories PRs by label (enhancement, bug, performance, documentation, infrastructure, dependencies). PRs labeled `skip-changelog` are excluded.
- `renovate.json` – Renovate is the dependency-update tool (Go modules, GitHub Actions, Docker base images). Auto-merges non-major updates, groups patch/minor/OTel/AWS-SDK updates, and runs lock-file maintenance. Major bumps are reviewed manually. No Dependabot version-update config — Renovate is the sole updater. The `:gitSignOff` preset adds a `Signed-off-by:` trailer to Renovate's commits so they satisfy the DCO check like any other commit — the check is not weakened or bypassed for bots. The go.mod `go` directive and the `golang` builder image are grouped ("go toolchain") so the pinned minimum Go version and the base-image Go version update together and never drift into a build break.

### Go toolchain in Docker

The builder stage sets `ENV GOTOOLCHAIN=auto`, overriding the `golang` base image's default of `local`. With `local`, a `go.mod` that requires a newer Go patch than the pinned base image hard-fails the build (`go: go.mod requires go >= X (running Y; GOTOOLCHAIN=local)`) — this bit us when a `go.mod` bump landed before the matching base-image digest bump. `auto` lets the builder fetch the exact toolchain `go.mod` pins (checksum-DB verified; `go.mod` stays the pin) instead of failing, decoupling the two so they can land in separate PRs. `local` was never a deliberate gate we set — it is the base image's own default.

## Pre-commit hook

`.githooks/pre-commit` runs on every commit: `go build`, `go vet`, `go test -race`, `govulncheck` (skipped if not installed), `gofmt` on staged Go files, and `trivy fs` secret and misconfig scans. `promtool check rules` (`prometheus/*.yml`), `docker compose config` (compose/Dockerfile), and `shellcheck` (`*.sh`, `.githooks/`) run only when those files are staged. This mirrors CI. The hook is configured by `scripts/init.sh` via `git config core.hooksPath .githooks`; new contributors must run the init script or set this manually.

`SKIP_HOOKS=1` skips the conditional checks but never the trivy scans: a secret is unrecoverable once committed, so those are unconditional. Secrets are scanned at every severity because trivy rates some real credentials below HIGH (a JWT is MEDIUM); misconfig gates on CRITICAL/HIGH only. `trivy.yml` splits them the same way.

The hooks prefer native `trivy`, `shellcheck`, `promtool`, and `amtool` on `PATH` and fall back to containers otherwise; `fs` scans and config linting only read the working tree, so no daemon is needed. `scripts/init.sh` prints platform install hints for any that are missing. With them installed, Docker is only required for `docker compose config`.

## Branch protection

Main requires four status checks: `CI Passed`, `Go vulnerability check`, `Scan Go dependencies`, `Go security scan`. Checks are strict — a PR branch must be up to date with main to merge, so main moving forces a rebase and re-run.

History is linear: merge commits are disabled repo-wide, so PRs land via squash (default) or rebase and every commit on main has one parent. Force-pushes and deletion of main are blocked. Admin bypass is enabled for the maintainer to push directly; contributors must open PRs that pass CI before merging.

Reviews and conversation resolution are deliberately not required. Neither constrains outside contributors — they have no write access and cannot merge regardless — and the maintainer can bypass both, so the status checks are the real gate. Renovate has write access but is not an admin, so it cannot bypass them: its automerge must pass the same four checks.

## Releases

- Use annotated tags: `git tag -a v0.1.0 -m "Short summary"`.
- The release workflow builds multi-platform binaries and creates a GitHub Release with auto-generated notes.
- Auto-generated notes are categorized by PR labels (configured in `.github/release.yml`).
- For the tag annotation, write a short summary. For the GitHub Release, the auto-generated notes cover PR-level detail; add a hand-written summary at the top for major releases.
- The first tagged release will be `v0.1.0`. Use semver: patch for fixes, minor for features, major for breaking changes. Stay on `v0.x.x` until stability guarantees are established.

## PR labels

Use labels on PRs for changelog categorization:

| Label | Use for |
|-------|---------|
| `enhancement` | New feature or improvement |
| `bug` | Bug fix |
| `performance` | Performance improvement |
| `documentation` | Docs changes |
| `infrastructure` | CI, build, deployment |
| `dependencies` | Dependency updates (Renovate adds this automatically) |
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

**DCO required.** Every commit must carry a `Signed-off-by:` trailer matching the
author (`git commit -s`). The DCO workflow rejects any PR commit without it; this
is the one trailer that is required (the no-Co-Authored-By rule still applies).

## Config changes vs code changes

- Config changes (`config/`, `technician.yml`, `checks/*.yml`, and the mounted `prometheus/*.yml`): `docker compose up -d --force-recreate <service>`. No rebuild needed; config is volume-mounted.
- Code changes (Go source, Dockerfile): `docker compose build technician && docker compose up`.

> **Use `--force-recreate`, not `restart`, after editing a mounted config.** The stack bind-mounts individual config *files* (not directories). Most editors — and tools like `sed -i` — save by writing a temp file and renaming it, which swaps the file's inode. On Docker Desktop (macOS), a running container keeps viewing the old inode, so `docker compose restart` (and in-place reloads like SIGHUP / `/-/reload`) can load a stale or truncated version. `up -d --force-recreate <service>` creates a new container that re-establishes the mount against the current file. Mounting directories instead of individual files would remove this footgun — tracked separately.

## Security

- `SECURITY.md` directs vulnerability reports to GitHub's private vulnerability reporting (Security tab). No public issues for security reports.
- Private vulnerability reporting is enabled on the repo.
- Dependabot **alerts** (the detection scanner — server-side, never shown in the Actions tab) are enabled; Renovate's `vulnerabilityAlerts` reads them and raises the fixes. Dependabot's own PRs are disabled: no `.github/dependabot.yml` (no version-update PRs) and security auto-fix is off. The idle "Dependabot Updates" workflow in the Actions tab is historical residue from before the Renovate migration and cannot be removed (GitHub-managed).
- **Trivy scanning** (`trivy.yml`): the filesystem scan gates on our own Go/npm deps (fails CI on any CRITICAL/HIGH). The container image scan uses `ignore-unfixed: true` + `exit-code: 1`, so it gates only on **fixable** CRITICAL/HIGH — unfixable upstream Debian/Node base CVEs can't block CI, but a fixable one forces a base-image bump (Renovate keeps the digest current). The runtime image is `node:24-slim` (Debian) because Playwright/Chromium requires glibc; a zero-CVE base is not achievable there (tracked separately for a possible split slim/browser image).
- **`.trivyignore.yaml`** – the only place fixable CVEs are deferred, and only when the fix hasn't reached our base image yet. Every entry requires `expired_at` + `statement`. Expired entries lapse and fail CI again; `trivy-ignore-audit.yml` reconciles them (PR to remove when fixed, issue to re-triage when not). Do not add blanket/undated ignores.

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
