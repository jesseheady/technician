# Technician – Agent guide

Technician is a **multi-region check runner** — a single Go binary that checks your infrastructure over the network. Fourteen check types, Prometheus metrics on `/metrics`, OTLP traces, performance budgets for CI, and Playwright browser flows.

## Repository layout

| Path | Purpose |
|------|--------|
| `cmd/` | Cobra CLI: `root`, `worker`, `check`, `serve`, `validate` |
| `internal/config/` | Main YAML config + check definitions; env var expansion `${VAR}` |
| `internal/check/` | Check implementations: HTTP, TCP, UDP, DNS, ICMP, gRPC, NTP, TLS, SMTP, traceroute, BGP, domain expiry, WebSocket, Playwright |
| `internal/scheduler/` | Cron-expression scheduling (gronx + stdlib timer loop) with per-origin stagger and a once-on-startup run |
| `internal/metrics/` | Prometheus gauges, OTLP trace + metric export, HAR parsing |
| `internal/remotewrite/` | Prometheus remote-write push client (protowire + snappy, optional SigV4) |
| `internal/artifact/` | Artifact backends: local, S3, stdout, noop |
| `internal/budget/` | YAML budgets, threshold evaluation, reporters (text, JSON, GHA) |
| `internal/exporter/` | Blackbox-exporter–compatible `/probe` handler |
| `internal/notify/` | Webhook notifications: Discord, Slack, generic HTTP |
| `internal/status/` | In-memory check store, budget badge tracking, status page + JSON API |
| `internal/history/` | Optional SQLite long-term history (async write-through, retention prune) |
| `internal/playwright/` | Go orchestrator + embedded Node.js run script |
| `config/` | Local/production configs (gitignored — copy from `examples/`) |
| `examples/` | Reference configs with placeholder values |
| `dashboards/` | Grafana JSON dashboards |
| `prometheus/` | Prometheus config, rules, Grafana provisioning |
| `deploy/` | Deployment artifacts: Helm chart (`helm/`), CloudFormation for ECS Fargate (`cloudformation/`), systemd unit (`systemd/`) |

## Commands

- **`technician worker`** – Long-running worker: loads config + checks, runs scheduler, serves `/metrics`, `/probe`, `/health`.
- **`technician check run --check <name>`** – Run a single check by name (omit `--check` to run all); for debugging.
- **`technician serve`** – Serve-only mode (metrics/health).
- **`technician validate`** – Run all checks, evaluate budgets, exit 0/1 (CI).
- **`technician test-webhook`** – Send a test notification to all configured webhooks.

Flags: `--config` / `-c` (default `technician.yml`), `--origin` (or `ORIGIN_ID`), `--log-level` (debug, info, warn, error). `worker` and `check run` also accept `--types`/`--groups`/`--tags` (comma-separated) to run a subset of checks, overriding the `check_filter` config block per dimension.

## Configuration

- **Main config**: `technician.yml` – `service`, `hostname`, `origins`, `metrics` (prometheus: listen, max_check_cardinality, remote_write_* — push `technician_*` to a Prometheus-compatible endpoint on a timer, optional SigV4 for AMP; otel: endpoint, metrics — opt-in to also push `technician_*` via OTLP), `artifacts`, `playwright` (mode: local/managed, server_url — required for managed, max_browsers), `webhooks`, `check_filter` (types/groups/tags — load-time subset so one checks dir serves many targets; see [docs/multi-target-deployment.md](docs/multi-target-deployment.md)), `logging` (format: json/text, level: debug/info/warn/error), `persistence` (enabled, retention — optional SQLite long-term history at `${TECHNICIAN_DATA_DIR}/results.db`, off by default).
- **Checks**: Loaded from directory next to config: `<config_dir>/checks.yml` (or `<config_dir>/checks/` directory):
  - `http.yml`, `tcp.yml`, `udp.yml`, `dns.yml`, `icmp.yml`, `grpc.yml`, `ntp.yml`, `tls.yml`, `smtp.yml`, `traceroute.yml`, `bgp.yml`, `domain_expiry.yml` – list of checks per type. HTTP checks support `assertions` (body: contains, not_contains, regex; header: header_contains, header_not_contains, header_regex), `follow_redirects`, `ip_version`, `min_tls`/`max_tls`, `basic_auth`/`bearer_token` (mutually exclusive), and `proxy`. TCP checks support `min_tls`/`max_tls` (with `tls: true`). SMTP checks support `start_tls`, `skip_tls`, and `username`/`password` auth (auth requires `start_tls`). DNS checks support SOA alongside A/AAAA/MX/TXT/CNAME/NS/SRV.
  - Playwright: `checks.yml` (or `checks/playwright.yml`) + script files.
  - All check types support optional `retry` (count, backoff, delay), `degraded_after` (duration threshold), `group`, and `tags` (used by `check_filter`).
- **Budgets**: Optional `budgets.yml` next to main config (used by `validate`).
- **Webhooks**: Optional `webhooks` list in `technician.yml`. Each entry has `url`, `type` (discord/slack/generic), `events` (check_down/check_up/budget_violation/cert_expiring), `severities` (warning/critical — omit for all), and `cooldown` (default 5m). Notifications fire on state transitions, not every check run. Events carry severity: check_down=critical, budget_violation=warning, cert_expiring=warning or critical based on days vs thresholds. Multiple webhook entries with different `severities` filters enable routing warnings to Slack and critical to PagerDuty.
- **Config layout**: `examples/` has reference configs with placeholder values (checked in). Copy to `config/` for local/production use (gitignored). Docker Compose mounts from `config/`.
- All YAML supports `${ENV_VAR}` and `${ENV_VAR:-default}` expansion. Without a default an unset variable keeps the literal placeholder.

## Alerting

Three strategies (see `docs/alerting.md`):

1. **Grafana alerting** (recommended) – Native contact points for Discord, Slack, PagerDuty, etc. UI for silencing, grouping, history.
2. **Native webhooks** – Direct from Technician via `webhooks` config. Simple, no external stack needed. Fires on check state transitions, new budget violations, and TLS cert expiry warnings with per-check cooldown. Supports severity-based routing (`severities` filter) to fork warnings and critical alerts to different channels.
3. **Prometheus Alertmanager** – Rule-based routing via `prometheus/rules.yml` and `prometheus/alertmanager.yml`. Discord requires a bridge container (`alertmanager-discord`); Slack/PagerDuty/Email work natively.

## Status page and persistence

- **Status page**: HTML at `/`, JSON API at `/api/status`. Shows check health, history bars, latency percentiles (P50/P90/P95/P99), HTTP timing breakdown (DNS/TLS/TTFB/transfer as stacked bar), budget badges (pass/fail per metric), and overall status (operational/degraded/down).
- **Persistence**: Check history (90-entry ring buffer per check), down-since timestamps, and budget check state are persisted to `$TECHNICIAN_DATA_DIR/status.json` (default `/var/lib/technician/status.json`). Budget badges survive restarts.

## Check model

- **Interface**: `internal/check.Checker`: `Type() config.CheckType` and `Run(ctx, cfg, origin) *Result`.
- **Types**: `http`, `tcp`, `udp`, `dns`, `icmp`, `grpc`, `ntp`, `tls`, `smtp`, `traceroute`, `bgp`, `domain_expiry`, `websocket`, `playwright`.
- **Result**: `check.Result` – `Name`, `Type`, `Success`, `Duration`, `Error`, `Degraded`, plus type-specific fields (HTTP timings/assertions, TCP conn/TLS durations, UDP RTT/response bytes, DNS answers/query time, ICMP packet loss/RTT stats, gRPC status, NTP offset/stratum/RTT, WebVitals, HAR, traceroute hops). `Labels` are populated from `origin.MetricLabels()`. **Browser (WebVitals)**: Core Web Vitals are LCP (≤2.5s), INP (≤200ms), CLS (≤0.1). See `docs/core-web-vitals.md`.
- **Adding a check type: Implement `Checker`; add `CheckType` and config struct in `internal/config/checks.go`; add loader in `LoadChecks`; register in `cmd/worker.go` (and `validate`/serve paths if needed); record metrics in `internal/metrics/prometheus.go` (and OTLP if desired); document any new metric in `docs/metrics.md`.

## Origins and deployment

- **Origins** are defined in `technician.yml` (`id`, `city`, `country`, `platform`, and freeform `labels`). Example config has `local` (platform `docker`) for local/Docker, and `us-east-1` / `us-west-2` (platform `aws`) for US regions. `ORIGIN_ID` (env or `--origin`) selects which origin labels are emitted; unset or unknown falls back to the first origin. Emitted labels are `region` (from `id`), `city`, and `country`, plus any freeform `labels` that don't collide with those.
- **Local/Docker**: Use `ORIGIN_ID=local`; the example config includes a `local` origin so labels stay distinct. Prometheus in compose scrapes `technician:9590` (Docker service name = hostname on the compose network).
- **VPC / central observability**: Deployed workers in a VPC are scraped by central Prometheus (static or service discovery). Central Grafana uses that Prometheus as source of record. See `docs/architecture/central-prometheus-grafana.md` for scrape config, discovery, and optional edge push.
- **Edge (Workers, Lambda)**: Site/location is derived from the platform at request time (e.g. Cloudflare colo, AWS region). See `docs/proposals/site-identifiers-edge.md`.

## Conventions

- **Logging**: `log/slog`. Handler and level come from the `logging` config block (`format: json` for Loki-native, else text; `level`), applied after config load via `applyLogConfig`. The `--log-level` flag overrides `logging.level`; before config loads, a text baseline honors the flag. The worker's result-drain loop emits one structured `Check result` line per execution (name, type, success, duration, region, degraded, retries, error; WARN when down/degraded), and stamps `trace_id`/`span_id` from `TraceCheckResult` when OTLP tracing is on. Self-health (goroutines, memory, FDs, CPU) comes from the standard Prometheus Go/process collectors — don't add custom metrics for it.
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

- `docs/metrics.md` – Reference for every exported metric: type, labels, meaning. Generated from the `Name`/`Help` pairs in `internal/metrics/`; update it when you add a metric.
- `docs/alerting.md` – Native webhooks, Grafana alerting (recommended), Alertmanager.
- `docs/getting-started.md` – Three onboarding paths (evaluate via Docker, contribute via `init.sh`, run in production), prerequisites, and check configuration.
- `docs/ci.md` – GitHub Actions workflow, generic CI pipelines, budget validation, Playwright in CI.
- `docs/playwright-scaling.md` – Browser check resource analysis, concurrency controls (`max_browsers`), dedicated runner architecture.
- `docs/deployment-sizing.md` – Resource requirements, VPS/Docker/Lambda/Workers sizing, worker-only deployment guide.
- `docs/persistence-and-retention.md` – Storage model, status store scaling, Prometheus cardinality, long-term history options.
- `docs/architecture/central-prometheus-grafana.md` – Central Prometheus (VPC), Grafana as source of record, local vs phone-home, edge push.
- `docs/proposals/site-identifiers-edge.md` – Site identifiers when checks run on Workers or Lambda.
- `docs/proposals/cloudflare-workers.md` – Cloudflare Workers and AWS options (Health Checks, Synthetics, Lambda).
- `docs/proposals/check-dependencies.md` – `depends_on` gating (issue #234): relationships between checks without a DAG scheduler.
- `docs/README.md` – Full index of all documentation.

## CI and workflows

- `.github/workflows/ci.yml` – Build, test, lint, validate (with and without Playwright), security scan (govulncheck), Docker build. Runs on push to main and PRs. A `changes` filter job skips heavy jobs for docs-only changes. The `Docker build` job runs on main pushes and on PRs that touch image inputs (`Dockerfile`, `.dockerignore`, `internal/playwright/scripts/`, `scripts/gen-licenses.sh` — the Dockerfile runs the last one), and feeds the `CI Passed` gate so a broken image build blocks merge. A `CI Passed` gate job aggregates all results for branch protection. The `test` job enforces a total-coverage floor (`MIN` in the `Coverage gate` step) and fails if coverage drops below it. The floor is a safety net held with headroom, not a per-PR ratchet: don't bump it on every PR, never lower it just to make a red build pass, and raise it only when coverage has climbed well clear of it. Note CI measures ~1% under local because the ICMP tests skip without raw sockets, so set the floor from the CI number, not the local one.
- `.github/workflows/release.yml` – Triggered on `v*` tag push. Builds binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64. Builds and pushes the multi-arch Docker image to GHCR (`ghcr.io/jesseheady/technician:<tag>` and `:latest`). Creates a GitHub Release with binaries attached. Each tarball bundles `LICENSE` and a freshly generated `THIRD_PARTY_LICENSES.txt` alongside the binary, so the archive carries its own attribution when redistributed; both also ship as standalone release assets. **While the repo is private, do not rely on this to publish images:** no GHCR package exists yet, so new tags must be pushed manually (build + `docker push ghcr.io/jesseheady/technician:<tag>`) until the repo goes public. **Flip the repo public before pushing the first `v*` tag.** A GHCR package inherits its visibility from the repo at creation time, so tagging while private creates a private package, and every `docker pull` of it then fails with `unauthorized` — including the one the README documents — until the package is manually flipped public in its settings. The base `docker-compose.yml` builds from source; the `docker-compose.prod.yml` overlay pulls this image for operators.
- `.github/workflows/cache-cleanup.yml` – On PR close, deletes the caches scoped to that PR's `refs/pull/<n>/merge` ref (via `gh api`) so merged/closed PRs stop holding cache space instead of waiting for the 7-day/10 GB eviction.
- `.github/workflows/trivy-ignore-audit.yml` – Weekly, reconciles `.trivyignore.yaml` deferrals: when an entry's `expired_at` is within 14 days it re-scans the image and either opens a PR to remove the entry (CVE fixed) or a re-triage issue (still vulnerable). Logic in `scripts/trivy-ignore-audit.sh`.
- `.github/release.yml` – Changelog category config for auto-generated release notes. Categories PRs by label (enhancement, bug, performance, documentation, infrastructure, dependencies). PRs labeled `skip-changelog` are excluded.
- `renovate.json` – Renovate is the dependency-update tool (Go modules, GitHub Actions, Docker base images, and the container images the git hooks run). No standard manager reads shell scripts, so the hook images are covered by a `customManagers` regex entry that matches `name:tag@sha256:` in `.githooks/`; they are grouped with the other image updates so the hooks and the stack move together. Auto-merges non-major updates after a 7-day release-age soak (vulnerability fixes skip the soak), groups patch/minor/OTel/AWS-SDK updates, and runs lock-file maintenance. The soak is a Renovate-internal `stability-days` check, invisible to GitHub, so `platformAutomerge` is off: Renovate merges its own PRs and honors the soak. Left on (the default), GitHub's native auto-merge would merge as soon as the five required checks pass — before the soak clears — silently defeating it. Major bumps are reviewed manually. No Dependabot version-update config — Renovate is the sole updater. The `:gitSignOff` preset adds a `Signed-off-by:` trailer to Renovate's commits so they satisfy the DCO check like any other commit — the check is not weakened or bypassed for bots. The go.mod `go` directive and the `golang` builder image are grouped ("go toolchain") so the pinned minimum Go version and the base-image Go version update together and never drift into a build break.

### Go toolchain in Docker

The builder stage sets `ENV GOTOOLCHAIN=auto`, overriding the `golang` base image's default of `local`. With `local`, a `go.mod` that requires a newer Go patch than the pinned base image hard-fails the build (`go: go.mod requires go >= X (running Y; GOTOOLCHAIN=local)`) — this bit us when a `go.mod` bump landed before the matching base-image digest bump. `auto` lets the builder fetch the exact toolchain `go.mod` pins (checksum-DB verified; `go.mod` stays the pin) instead of failing, decoupling the two so they can land in separate PRs. `local` was never a deliberate gate we set — it is the base image's own default.

## Pre-commit hook

`.githooks/pre-commit` runs on every commit: `go build`, `go vet`, `go test -race`, `govulncheck` (skipped if not installed), `gofmt` on staged Go files, and `trivy fs` secret and misconfig scans. `golangci-lint run ./...` (when Go files are staged), `promtool check rules` (`prometheus/*.yml`), `docker compose config` (compose/Dockerfile), and `shellcheck` (`*.sh`, `.githooks/`) run only when those files are staged. This mirrors CI — the pre-commit `golangci-lint` matches the `Lint` job so lint failures are caught before push, not in CI. The hook is configured by `scripts/init.sh` via `git config core.hooksPath .githooks`; new contributors must run the init script or set this manually.

`SKIP_HOOKS=1` skips the conditional checks but never the trivy scans: a secret is unrecoverable once committed, so those are unconditional. Secrets are scanned at every severity because trivy rates some real credentials below HIGH (a JWT is MEDIUM); misconfig gates on CRITICAL/HIGH only. `trivy.yml` splits them the same way.

The hooks prefer native `trivy`, `shellcheck`, `golangci-lint`, `promtool`, and `amtool` on `PATH` and fall back to containers otherwise. Those container fallbacks are pinned by digest, like every other image in the repo, so a hook cannot silently change behaviour or pull a compromised image between runs; the Prometheus and Alertmanager pins match `docker-compose.yml`, so the hooks validate config with the versions the stack runs.  `fs` scans and config linting only read the working tree, so no daemon is needed. `scripts/init.sh` prints platform install hints for any that are missing. With them installed, Docker is only required for `docker compose config`.

## Branch protection

Main requires five status checks: `CI Passed`, `Go vulnerability check`, `Scan Go dependencies`, `Go security scan`, `Container scan passed`. `CI Passed` and `Container scan passed` are aggregators: the first covers the build, test, lint, validate and image-build jobs, the second covers the Trivy jobs, including the image scan. Both run with `if: always()`, because the jobs they gate are skipped on most PRs and a required context that never reports leaves a PR unmergeable. Checks are strict — a PR branch must be up to date with main to merge, so main moving forces a rebase and re-run.

History is linear: merge commits are disabled repo-wide, so PRs land via squash (default) or rebase and every commit on main has one parent. Force-pushes and deletion of main are blocked. Admin bypass is enabled for the maintainer to push directly; contributors must open PRs that pass CI before merging.

Reviews and conversation resolution are deliberately not required. Neither constrains outside contributors — they have no write access and cannot merge regardless — and the maintainer can bypass both, so the status checks are the real gate. Renovate has write access but is not an admin, so it cannot bypass them: its automerge must pass the same five checks.

## Releases

- Use annotated tags: `git tag -a vX.Y.Z -m "Short summary"`.
- The release workflow builds multi-platform binaries and creates a GitHub Release with auto-generated notes.
- Auto-generated notes are categorized by PR labels (configured in `.github/release.yml`).
- For the tag annotation, write a short summary. For the GitHub Release, the auto-generated notes cover PR-level detail; add a hand-written summary at the top for major releases.
- Use semver: patch for fixes, minor for features, major for breaking changes. Stay on `v0.x.x` until stability guarantees are established.
- After the **first** publish, the GHCR package is private by default even though the repo is public. Link it to the repo and set its visibility to public, or every `docker pull` fails with an auth error.
- Bump `appVersion` in `deploy/helm/technician/Chart.yaml` and the `ImageUri` default in `deploy/cloudformation/ecs-fargate.yaml` in the release PR, before tagging. A tag freezes the tree, so a stale default stays wrong at that tag permanently.

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

- Config changes (`config/`, `technician.yml`, `checks/*.yml`, and the mounted `prometheus/*.yml`): `docker compose restart <service>`. No rebuild needed; config is volume-mounted.
- Code changes (Go source, Dockerfile): `docker compose build technician && docker compose up`.

> **Config is mounted by directory, not by file.** Most editors, and tools like `sed -i`, save by writing a temp file and renaming it, which swaps the file's inode. A container holding a single-file bind mount keeps viewing the old inode, so on Docker Desktop (macOS) an edit followed by `docker compose restart` could load a stale or truncated version. The stack now mounts the parent directories instead — `./config`, `./prometheus` — which resolve each path on open, so a plain `restart` picks up the current bytes. Grafana keeps single-file mounts because its provisioning expects specific subdirectory names (`datasources/`, `dashboards/`, `alerting/`) that the repo layout does not match; those files are read once at startup, so recreate Grafana after editing them. The worker sees the whole `config/` tree through that mount, so keep credentials in the environment with `${VAR}`, as the example configs do, and not in files under `config/`.

## Security

- `SECURITY.md` directs vulnerability reports to GitHub's private vulnerability reporting (Security tab). No public issues for security reports.
- Private vulnerability reporting is enabled on the repo.
- Dependabot **alerts** (the detection scanner — server-side, never shown in the Actions tab) are enabled; Renovate's `vulnerabilityAlerts` reads them and raises the fixes. Dependabot's own PRs are disabled: no `.github/dependabot.yml` (no version-update PRs) and security auto-fix is off. The idle "Dependabot Updates" workflow in the Actions tab is historical residue from before the Renovate migration and cannot be removed (GitHub-managed).
- **Trivy scanning** (`trivy.yml`): the filesystem scan gates on our own Go/npm deps (fails CI on any CRITICAL/HIGH). The container image scan uses `ignore-unfixed: true` + `exit-code: 1`, so it gates only on **fixable** CRITICAL/HIGH — unfixable upstream Debian/Node base CVEs can't block CI. The image scan runs on every push to main and on the weekly schedule (base-image CVEs appear over time with no commit to trigger them), and on PRs only when the image's own inputs change (`Dockerfile`, `.dockerignore`, `.trivyignore.yaml`, `internal/playwright/scripts/`, `scripts/gen-licenses.sh` — the Dockerfile runs the last one), because the build is expensive. The scan build sets `no-cache-filters: runtime` so the stage that installs Debian packages is rebuilt rather than restored from the GHA layer cache: a cache hit pins those packages to whenever the layer was first built, and the scan then reports CVEs that a fresh `apt` would already have fixed. Little else invalidates that layer, since the base digest and instructions rarely change. The builder stage still comes from cache.
- **Runtime image CVE surface**: the runtime is `node:24-slim` (Debian) because Playwright/Chromium requires glibc. npm's bundled dependency tree was ~143 of the image's node packages and the source of nearly every fixable CVE reported — CVEs unpatchable from here, since they ship inside the upstream base image. The Dockerfile therefore removes `npm`, `npx`, `corepack`, and `yarn` in the same layer that uses them; the runtime only ever spawns `node` (`internal/playwright/runner.go`). A fixable CVE is not always clearable by a base-image bump: in July 2026 the pinned digest was already the newest published `node:24-slim`, so waiting was not an option.
- **`.trivyignore.yaml`** – two sections with deliberately different rules. **`vulnerabilities`** is the only place fixable CVEs are deferred, and only when the fix hasn't reached our base image yet; every entry requires `expired_at` + `statement`. Expired entries lapse and fail CI again; `trivy-ignore-audit.yml` reconciles them (PR to remove when fixed, issue to re-triage when not). Do not add blanket/undated ignores. Currently empty — prefer removing the vulnerable component (as the Dockerfile does with npm) over suppressing it. **`misconfigurations`** records accepted architectural decisions in IaC we publish; these do **not** expire (a date on "a synthetic monitor makes outbound connections" only guarantees a pointless future CI break) and the audit workflow ignores them, since it reads `vulnerabilities` only. Every misconfig entry requires `statement` **and** `paths` — an unscoped entry suppresses the rule repo-wide, so a future template with a real mistake would pass silently. Trivy does not honor inline `trivy:ignore` comments in CloudFormation (Terraform only), so this file is the only mechanism.

## Check schedule guidance

Not all checks need the same frequency. Recommended production intervals, with the
rationale for each, live in
[docs/deployment-sizing.md § Check schedule guidance](docs/deployment-sizing.md#check-schedule-guidance).
Keep that table as the single source; this file previously carried a copy that had
already lost a column.

When editing code, preserve existing patterns: check results feed metrics and (where applicable) OTLP; new metrics should follow the naming in `internal/metrics/prometheus.go`; budget metric names must match threshold keys in `budgets.yml`.
