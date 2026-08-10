# Roadmap

Work that's been planned or partially designed but not yet implemented. See also [Recently completed](#recently-completed) for items that have shipped.

## Edge deployment adapters

### AWS Lambda adapter [#17](https://github.com/jesseheady/technician/issues/17)

Technician's Go binary can run in a Lambda container image (regional Lambda), but there's no Lambda-specific packaging or infrastructure yet.

**What's needed:**

- Lambda handler that wraps the existing check execution (reuse `internal/check` and `internal/exporter`) behind a Lambda function URL or API Gateway trigger.
- SAM template or Terraform/CDK for provisioning: function, EventBridge schedule (replaces in-process cron), IAM role, networking (VPC placement if probing internal targets).
- Decision on invocation model: EventBridge schedule triggers Lambda per-check, or a single invocation runs all checks and pushes results.
- Push mechanism for metrics: Prometheus can't scrape a short-lived Lambda. Options are Pushgateway, Prometheus remote-write, or an in-VPC aggregator that Prometheus scrapes. See [central-prometheus-grafana.md](architecture/central-prometheus-grafana.md).
- Lambda@Edge (Node.js/Python only) would need a separate lightweight HTTP check adapter, not the Go binary.

### Cloudflare Workers adapter [#18](https://github.com/jesseheady/technician/issues/18)

Designed in [proposals/cloudflare-workers.md](proposals/cloudflare-workers.md). The proposal recommends "Path A" — a small JS/TS Worker that performs one HTTP check per request and returns Prometheus text or JSON.

**What's needed:**

- Reference Worker implementation (JS/TS) under e.g. `workers/cf/` with Wrangler config.
- Same `/probe?target=&module=` contract as the existing blackbox handler so Prometheus treats Worker and Technician endpoints identically.
- Cron Trigger configuration for scheduled checks.
- Documentation for how Prometheus or an aggregator scrapes/receives Worker results.

## Metrics and persistence

### Prometheus remote-write [#19](https://github.com/jesseheady/technician/issues/19)

Add native Prometheus remote-write support to Technician, configured via `metrics.prometheus.remote_write_url` in `technician.yml`. This lets workers push metrics directly to AWS Managed Prometheus (AMP), Grafana Cloud, Thanos, or Mimir without needing a sidecar agent.

**What's needed:**

- New config field: `metrics.prometheus.remote_write_url` (and optional `remote_write_sigv4` for AMP auth).
- Remote-write client using the Prometheus remote-write protocol (protobuf over HTTP).
- Push after each check result, or batch on a timer (e.g. every 15s).
- SigV4 signing for AMP endpoints (AWS SDK is already a dependency).

### Status page historical data

The built-in status page shows recent results from an in-memory ring buffer (90 entries per check, ~45 min at 30s intervals), persisted to a JSON file on disk so history survives restarts and container rebuilds. For longer historical views (30-day uptime bars, etc.), two additional paths:

**Path A: Query Prometheus API** — Add `metrics.prometheus.url` config. The status page handler queries Prometheus for historical uptime and timing aggregates. No new storage, but requires Prometheus to be reachable from the worker.

**Path B: Embedded SQLite (recommended for public status page)** — Use `modernc.org/sqlite` (pure Go, no CGO) for local check result persistence. Adds ~2 MB to the binary. Good for standalone workers without Prometheus access, and the key enabler for a public-facing status page with meaningful history.

**Sizing estimates (30 checks, recommended production intervals):**

| Retention | Rows | Disk | Memory |
|---|---|---|---|
| 30 days | ~674K | 70–130 MB | 2–5 MB |
| 90 days | ~2M | 200–400 MB | 2–5 MB |
| 12 months | ~8.2M | 0.8–1.6 GB | 2–5 MB |

Memory does not scale with retention. SQLite reads pages on demand; the page cache stays at 2–5 MB. At 100 checks and 12 months, disk reaches 2.5–5 GB. At 500 checks, 12–25 GB.

**Implementation:** Single `probe_results` table, covering index on `(name, timestamp, success)`, configurable retention (`persistence.retention: 90d`), periodic prune with `PRAGMA auto_vacuum=incremental`. The existing ring buffer stays for real-time rendering; SQLite is queried for historical views.

**Port separation:** A public-facing status page should not share a port with `/metrics`, `/probe`, and `/health`. These are internal operational endpoints. Options include a separate listen address (`status.listen: :8080`) or a config toggle to disable operational endpoints on the status port.

See [persistence-and-retention.md](persistence-and-retention.md) for the full analysis and [#16](https://github.com/jesseheady/technician/issues/16) for implementation tracking.

### SLA reporting [#20](https://github.com/jesseheady/technician/issues/20)

Generate periodic SLA reports showing uptime, latency percentiles, and incident counts over configurable windows (30, 90, 365 days). Reports can be scoped to specific check groups — e.g. report on "Marketing" and "Infrastructure" while omitting "Third Party" checks that aren't covered by your SLA.

**Depends on:** SQLite persistence (see [Status page historical data](#status-page-historical-data)) or Prometheus API access for historical queries.

**What's needed:**

- `technician report` CLI command with flags: `--period 30d|90d|365d`, `--groups "Marketing,Infrastructure"` (default: all), `--format html|json|csv`, `--output report.html`.
- Report data model: per-check uptime %, p50/p95/p99 latency, incident count and total downtime, grouped by check group.
- HTML template for rendered reports: clean, printable layout with uptime bars, latency sparklines, and summary table. Suitable for emailing to stakeholders or attaching to an internal SLA review.
- JSON/CSV output for programmatic consumption or import into spreadsheets.
- Optional: scheduled report generation via cron config in `technician.yml`, with delivery via webhook (post the HTML/JSON to Slack, email gateway, or S3).

**Config shape:**

```yaml
reports:
  - name: Monthly SLA
    period: 30d
    groups: [Marketing, Infrastructure]
    schedule: "0 0 1 * *"    # 1st of each month
    format: html
    deliver:
      - url: https://hooks.slack.com/services/T00/B00/xxx
      - s3: s3://reports-bucket/sla/{{.Year}}/{{.Month}}.html
```

**Aggregation queries** (SQLite path):

```sql
-- Uptime %
SELECT name, group_name,
       COUNT(CASE WHEN success = 1 THEN 1 END) * 100.0 / COUNT(*) AS uptime_pct
FROM probe_results
WHERE timestamp > datetime('now', '-30 days')
  AND group_name IN ('Marketing', 'Infrastructure')
GROUP BY name, group_name;

-- Latency percentiles
SELECT name, group_name,
       percentile(duration_ms, 50) AS p50,
       percentile(duration_ms, 95) AS p95,
       percentile(duration_ms, 99) AS p99
FROM probe_results
WHERE success = 1
  AND timestamp > datetime('now', '-30 days')
GROUP BY name, group_name;
```

### IaC templates [#21](https://github.com/jesseheady/technician/issues/21)

Terraform or CloudFormation templates for common deployment patterns:

- VPS worker (systemd unit + config)
- ECS service (task definition, service discovery, AMP scraper config)
- Lambda function (EventBridge schedule, IAM role, Pushgateway push)
- Central stack (Prometheus + Grafana on a single host)

## Near-term MVP

Features planned for the next development cycle.

### Maintenance mode [#22](https://github.com/jesseheady/technician/issues/22)

**Display-only** status-page maintenance badge. Alert suppression is **not** in scope — that stays in Alertmanager (mute time intervals for planned windows, silences for ad-hoc; see [alerting.md § Maintenance windows](alerting.md#maintenance-windows)). Technician should not be a second place to reason about "is it muted?".

When a check or group has a declared maintenance window, Technician's own status page shows a distinct **maintenance** badge instead of "down". Checks keep running and record true state; only the presentation changes.

**What's needed:**

- Declarative `maintenance_windows` (start/end) in check config, at check and group/tag granularity.
- Render-time evaluation against `now` — no metric/label/gauge and no reconciler, so no staleness lag.
- Status page renders a maintenance badge (icon + reason) in place of the up/down indicator.

**Deferred** until the status page is load-bearing (public/customer-facing, or the single pane engineers rely on); for internal use today, Alertmanager fully covers the operational need. Ad-hoc on/off (as opposed to declared windows) needs an API or admin UI, neither of which exists yet. If the same windows are also wanted for Alertmanager muting, generate the mute time interval from this config rather than declaring it twice.

**Config shape:**

```yaml
# In check config — windows drive status-page display only
- name: API Server
  url: https://api.example.com
  maintenance_windows:
    - start: "2026-03-15T02:00:00Z"
      end: "2026-03-15T04:00:00Z"
      reason: "Database migration"
```

### Status page redesign [#25](https://github.com/jesseheady/technician/issues/25)

Redesign the built-in status page. Reference layout based on Upptime, Cachet, and Gatus.

**Current state:** Minimal HTML template with check rows, history bars, timing breakdown, and budget badges.

**Reference layout:**

Each monitor is rendered as a card with three stacked elements:
1. **Header row** — Monitor name + status icon (left), overall uptime % (right, color-coded green/amber/red based on threshold)
2. **Dense uptime bar** — Horizontal strip of small colored segments, one per time bucket (day). Green = all checks passed, amber = some failures or degraded, red = majority down. Hover tooltip shows the downtime duration and error details for that segment.
3. **Response time line chart** — Always visible below the uptime bar (not collapsed). Y-axis auto-scales per monitor (important — a 600ms baseline service and a 10s baseline service need different scales). X-axis shows time labels.

Monitors are grouped under collapsible section headers: group name (left), "N/N Operational" count (right), collapse chevron.

**Target improvements:**

- **Overall status banner** — Large banner at the top: "All Systems Operational" (green), "Some Systems Degraded" (amber), or "Outage Detected" (red). Single glance tells you the story.
- **Dense uptime bars with hover tooltips** — Replace the current short history bars with a dense segmented bar where each segment represents one time bucket. Hover tooltip shows: segment date/time, uptime %, downtime duration, and error summary for that bucket. Color: green (100% up), amber (partial failures or degraded), red (majority down). Shows overall uptime % right-aligned on the header row.
- **Time window picker** — Allow users to switch the status page view between time windows: Last 24h, Last 7d, Last 30d, Last 90d. This controls both the uptime bar granularity (hourly buckets for 24h, daily for 7d/30d/90d) and the response time chart X-axis. Default: 24h for the ring buffer (no persistence needed), longer windows require [historical data](#status-page-historical-data). Implemented as simple tab/button bar above the monitor list.
- **Response time line chart** — Per-monitor line chart always visible below the uptime bar. Y-axis auto-scaled per monitor so both fast (200ms) and slow (5s) services are readable. Response time is a key golden signal — this should be prominent, not hidden behind a click. Lightweight JS using inline `<canvas>` or SVG path generation. No chart library dependency.
- **Monitor grouping with collapse** — Group checks by the existing `group` field with collapsible section headers. Header shows: group name + optional icon (left), "N/N Operational" aggregate count (right), collapse chevron. Group-level status is worst-of-group.
- **Maintenance banners** — When any check is in maintenance mode, show a blue/gray banner with the reason text and scheduled end time. Maintenance checks show a wrench icon instead of status dot.
- **Dark/light mode** — Respect `prefers-color-scheme` media query. Dark mode as default (matches Grafana). CSS-only, no JS framework needed.
- **Incident timeline** — Below the monitor list, show recent incidents (state transitions) in reverse chronological order with duration and resolution status.

**Design principles:**
- No JavaScript framework (keep the binary small, no node_modules).
- CSS variables for theming (light/dark).
- Progressive enhancement: the page works without JS (static uptime bars, no charts). Charts enhance with JS.
- All data served from existing `/api/status` endpoint (extend with time window query param: `/api/status?window=24h`).
- Response time charts are a golden signal — they should be always visible per monitor, not buried behind interactions.
- Auto-scale Y-axis per monitor. A 600ms baseline and a 10,000ms spike need different visual scales than a 50ms service.

## Scope principles

Technician's scope: **check execution, scheduling, metrics export, status page, and performance budgets — as a single binary.** Visualization, incident management, and complex alerting belong to Grafana and dedicated tools.

### What Technician owns

- **Check execution** — HTTP, TCP, UDP, DNS, ICMP, gRPC, NTP, TLS, SMTP, traceroute, BGP, domain expiry, WebSocket, Playwright.
- **Scheduling** — Built-in cron with stagger/jitter. No external scheduler needed.
- **Status page** — Built-in, no external dependencies.
- **Notifications** — Webhook-based alerting for check state transitions, cert expiry, and budget violations with severity-based routing (warning/critical).
- **Performance budgets** — Threshold-based degradation tracking with three-state escalation.
- **Prometheus metrics** — Native exposition on `/metrics`.
- **Structured logs** — slog to stdout for Loki/log aggregation. How operators observe Technician itself.

### What Technician defers to other tools

- **Incident management** — Grafana OnCall, PagerDuty, OpsGenie. Technician fires webhooks into these tools, it doesn't replace them.
- **Complex alerting rules** — Grafana Alerting handles multi-condition, multi-signal alerting with silences, routing, and escalation. Technician's notifications are simple state-transition triggers.
- **Log aggregation and analysis** — Loki. Technician emits structured logs; it doesn't store, index, or query them.
- **Dashboarding beyond provisioned set** — Grafana. The 6 provisioned dashboards are a starting point. Custom dashboards are the user's domain.
- **Domain intelligence / WHOIS** — Different problem domain. Dedicated tools (dnstwist, domain monitoring services) handle this better.
- **Admin UI / user management / SSO** — Grafana is the UI. Building a second admin interface creates maintenance burden for marginal value.
- **Full incident timelines and postmortems** — Grafana Incident, Statuspage, or dedicated incident tools. Technician can feed data into these but shouldn't replicate them.

## Companion tooling

### k6 load testing integration [#26](https://github.com/jesseheady/technician/issues/26)

k6 (Grafana's open-source load testing tool) is a natural companion to Technician. Technician answers "is my service healthy?"; k6 answers "how does it behave under load?" Running both together lets you generate traffic with k6 and watch Technician's checks reflect the degradation in real-time.

**Approach:** "Works well alongside" — no changes to the Technician Go binary. k6 remains a standalone tool; Technician provides infrastructure, dashboards, and documentation to make the pairing seamless. This mirrors how Technician already defers dashboarding to Grafana and incident management to external tools.

**What's needed:**

- **Docker Compose profile** — Add a `loadtest` profile to the existing stack (`docker compose --profile loadtest up`). Includes k6 configured to push metrics to the same Prometheus instance via `K6_PROMETHEUS_RW_SERVER_URL`. Off by default so it doesn't affect the standard monitoring stack.
- **Grafana dashboard** — Pre-built k6 dashboard (provisioned alongside the existing five) showing request rate, response time distribution, error rate, VU count, and iteration throughput. Sourced from k6's official Prometheus dashboard with adjustments to match Technician's Grafana theme.
- **Example scripts** — `examples/k6/` directory with sample load test scripts targeting the same services used in the sample check configs. Includes a basic HTTP load test, a ramping VU scenario, and a threshold-based test that mirrors Technician's performance budgets.
- **CI workflow example** — GitHub Actions job showing the "load then validate" pattern: run a k6 scenario against a staging target, then run `technician validate` to check if performance budgets still pass under load.
- **Documentation** — `docs/k6-companion.md` covering setup, the Docker Compose profile, dashboard walkthrough, and the recommended workflow for pairing load tests with synthetic monitoring.

**What this is NOT:**

- Not a new check type or k6 embedding in the Technician binary.
- Not a k6 orchestration layer — users author and run k6 scripts with the standard `k6 run` CLI.
- Not a replacement for k6 Cloud or Grafana Cloud k6 for distributed load testing.

**Cost and sizing considerations:**

k6 load generation is CPU and memory intensive, unlike Technician's lightweight checks. Sizing depends heavily on the protocol, target, and concurrency:

| Scenario | VUs | Approx. resources | Notes |
|---|---|---|---|
| HTTP baseline | 50 VUs | 1 vCPU, 512 MB | Simple GET/POST, low think time |
| HTTP sustained | 200–500 VUs | 2 vCPU, 1–2 GB | Realistic API load test |
| HTTP high concurrency | 1,000+ VUs | 4+ vCPU, 4+ GB | May need distributed execution |
| Browser (k6 browser module) | 5–10 VUs | 2+ vCPU, 2–4 GB | Each VU runs a Chromium instance |

- **Do not co-locate with Technician workers in production.** k6 load generation will saturate CPU and network, skewing Technician's check latency measurements. Run k6 on a separate host, container, or CI runner.
- **Lambda/Workers targets are not suitable** for load generation. k6 needs sustained compute; serverless invocation models don't fit.
- **Network egress costs** can be significant for high-throughput tests against cloud-hosted targets. A 500 VU test at 100 req/s for 10 minutes generates meaningful egress depending on response payload sizes.
- **Distributed execution** — For tests exceeding a single machine's capacity, k6 supports distributed mode via `k6 operator` (Kubernetes) or Grafana Cloud k6. This is outside Technician's scope but should be documented as the scaling path.
- **Recommended deployment pattern:** Dedicated `t3.medium` / `c6i.large` (or equivalent) instance for load generation, separate from the Technician worker and Prometheus/Grafana stack. For CI, use a dedicated runner with at least 2 vCPU and 2 GB RAM.

## Possible enhancements

Items here are either partially covered by Grafana dashboards, low priority, or would add complexity that isn't justified yet. Each item includes a rationale for deferral.

### Observability and export

- **Full OTel metrics export** [#33](https://github.com/jesseheady/technician/issues/33) — Push check metrics via OpenTelemetry in addition to Prometheus. Trace export is wired into the worker; metrics export is not. Medium priority.

- **Prometheus backfill on startup** [#34](https://github.com/jesseheady/technician/issues/34) — If the local store is empty or stale, query Prometheus for recent check metrics and reconstruct the ring buffer. Limitation: HTTP timing breakdown and assertion details aren't in the metrics, so backfilled history would be partial.

### Status page and UI

- **Public/private visibility toggle** [#39](https://github.com/jesseheady/technician/issues/39) — Control which checks are visible on the public status page vs internal-only.

### Notifications and alerting

- **Additional notification channels** [#40](https://github.com/jesseheady/technician/issues/40) — First-class Email, Telegram, PagerDuty, OpsGenie senders. The generic webhook sender already covers any HTTP-based integration. Add first-class senders only when the generic sender proves insufficient for a specific channel's payload format.

- **Slack chatbot / agent** — Manage status pages and incidents via Slack commands. Overlaps with Grafana OnCall and existing webhook notifications. Not justified at current scale.

### Operations

- **Incident tracking** — Automatic incident creation/resolution from check failures. Grafana Alerting provides incident-style state management (firing → resolved), and PagerDuty/Grafana OnCall/OpsGenie integrate via the generic webhook sender. Building a first-party incident system would duplicate existing tooling.

See `docs/internal/` for full feature gap analyses against specific tools.

## Recently completed

### Structured logging for Loki [#24](https://github.com/jesseheady/technician/issues/24)

`slog` output is now Loki-ready. Each check execution logs a structured `Check result` line — name, type, success, duration, region, degraded, retries, error — at INFO (WARN when down or degraded), giving a complete record independent of Prometheus. When OTLP tracing is enabled the line carries `trace_id`/`span_id` for Loki↔trace correlation. `logging.format` (`json`/`text`) and `logging.level` config landed earlier. Self-health (goroutines, memory, threads, FDs, CPU) is already exported by the standard Prometheus Go/process collectors, so no custom metrics were added. Scheduler-loop-timing and config-reload logging were dropped: per-check duration plus the runtime collectors already cover "is Technician healthy?", and there is no hot config reload to log.

### WebSocket check type [#23](https://github.com/jesseheady/technician/issues/23)

WS/WSS check type for real-time services: connect, optionally send a message, assert on the response, and record connection and message timing. Uses `golang.org/x/net/websocket` — already an indirect dependency via `golang.org/x/net`, so no new module was added. Config fields: `url` (ws:// or wss://), `send`, `expect_recv`, `skip_tls`, `headers`. Emits `technician_ws_connect_seconds` and `technician_ws_message_seconds`.

### Budget `*` thresholds are per-metric defaults

`examples/budgets.yml` documented the `*` entry as "applied to any check not listed below", but `EvaluateAll` matched it against every check and appended its thresholds alongside the named entry's. A per-check budget could therefore never loosen a global default: a check with its own `duration: 30000` was still failed by the `*` entry's `duration: 10000` on the same run, with no config able to express the intent. Slow-by-nature checks (domain expiry via RDAP, traceroute) inherited a default sized for HTTP and reported violations while passing.

Thresholds now merge per metric — `*` supplies defaults and a named entry overrides them individually. Replacing the whole entry instead would have fixed the same bug but introduced a quieter one, where adding a single per-check threshold silently drops every default the entry omits; merging keeps that coverage. Violations record whether their threshold came from `*`, surfaced as `inherited from "*"` in text output and `"inherited": true` in JSON, so a tuned check is distinguishable from one still on the defaults.

### OTLP trace export wired into the worker

`internal/metrics/otel.go` held a complete tracing implementation that nothing ever called, so `metrics.otel.endpoint` was accepted and silently ignored — spans were never emitted despite tracing being a documented feature. The worker now initializes the tracer at startup, emits a span per check result, and flushes the batcher on shutdown. Two defects surfaced while wiring it: spans were anchored at drain time (collapsing to ~0 duration, since results are traced after the check finishes) and the exporter always used TLS, so plaintext local/sidecar collectors could never receive traces. Spans now carry the check's real start and end, and the endpoint's scheme selects the transport.

### Configurable Prometheus cardinality limit

The 500-name cardinality guard was a compile-time constant, so operators running more checks than that had to fork and rebuild to get metrics for all of them. It's now `metrics.prometheus.max_check_cardinality`, defaulting to 500 when unset. The guard itself is unchanged — only its limit moved into config.

### Orphaned budget state pruning

`Store.Reconcile` already dropped persisted check history for checks removed from the config (shipped earlier), but budget state is keyed by check name alone in a separate map and survived the sweep. Orphaned badges lingered in `status.json` indefinitely and inflated the status page's budget totals. Reconcile now prunes both.

### Pull-based production compose

The base `docker-compose.yml` builds from source so a fresh checkout works with no image; a tracked `docker-compose.prod.yml` overlay swaps in the published image `ghcr.io/jesseheady/technician:${TECHNICIAN_VERSION:-latest}` for operators (`-f docker-compose.yml -f docker-compose.prod.yml`). Documented the single-node Compose and Kubernetes (Helm) production paths in getting-started.

### Cross-platform developer onboarding

Renamed `scripts/init-mac.sh` → `scripts/init.sh` with platform detection (`uname` + package-manager sniffing) so one-time setup works on macOS, Linux, and WSL. Restructured getting-started around an evaluate-vs-contribute split with a chooser table. Evaluated `mise` for runtime pinning and kept it out — Go is pinned via `go.mod` and adding a bootstrap tool would raise, not lower, the adoption barrier.

### Check filtering (multi-target deployment)

Workers can run a targeted subset of checks via a `check_filter` block in `technician.yml` (`types`, `groups`, `tags`) or the `--types`/`--groups`/`--tags` flags on `worker` and `check run`. Dimensions are AND-ed, values within a dimension OR-ed, matching is case-insensitive, and unknown types are rejected at startup. Filtering happens once at load time, so filtered-out checks are never scheduled. Added a `tags` field to check configs; `validate` respects the config filter (its `--check-type`/`--exclude-type` flags still layer on top). This lets one shared `checks/` directory serve many deployment targets — see [multi-target deployment](multi-target-deployment.md).

### Staleness grace period (post-gap alert suppression)

After a data gap — restart, connectivity loss, host downtime — the first check results carry cold-start latencies (stale DNS, cold TCP, NTP drift) that spike the 15m rolling averages and would falsely fire the timing alerts for ~10 minutes. Technician now exposes `technician_last_run_timestamp_seconds` (advances only on recorded, non-infra results, so it freezes in every gap type), a `technician:seconds_since_last_run` recording rule, and a `TechnicianDataGap` alert that fires when a >5-min gap occurred within the last 10 minutes — staying firing through the post-resume stabilization window. An Alertmanager inhibit rule suppresses the latency/timing alert family (Web Vitals, HTTP/TCP timing, DNS, ICMP RTT + packet loss, NTP, UDP) while it fires; non-timing alerts (check down, expiry, BGP, budgets) are unaffected. The alert routes to a blackhole receiver so it drives inhibition without notifying anyone. Rule logic is covered by a `promtool test rules` unit test wired into the pre-commit hook.

### Playwright temp-asset isolation (HAR race + video leak fix)

Each Playwright run now gets a unique per-run temp directory for its HAR and video output, created and removed by the Go orchestrator. Previously every run wrote its HAR to the same fixed path, so concurrent runs (`max_browsers > 1`) clobbered each other's data — corrupting the HAR metrics/OTLP output — and `video: true` files accumulated unbounded under `/tmp`. Videos are deleted with the work dir and `VideoPath` is cleared; retaining failure videos via the artifact store is tracked separately.

### HTTP proxy support

HTTP checks accept a `proxy` field (`http://host:port`) to route the request through an explicit proxy — useful for checks running behind a corporate proxy. Implemented via the transport's `Proxy` (clients are cached per proxy), with the URL validated at config load.

### Full SOA record support for DNS checks

DNS checks now query SOA records properly via `miekg/dns` (Go's `net.Resolver` can't query SOA), replacing the previous fallback that only verified the domain resolved. The answer is formatted as `mname rname serial refresh retry expire minimum`, and SOA records are read from either the answer or authority section. Other record types continue to use `net.Resolver`. Adds the `github.com/miekg/dns` dependency (~1 MB to the binary).

### SMTP STARTTLS and authentication

The SMTP check now negotiates STARTTLS (`start_tls: true`, failing if the server doesn't advertise it) and optionally authenticates over the encrypted channel (`username`/`password`, PLAIN auth). `skip_tls` allows self-signed mail servers. Config validation enforces that auth requires `start_tls` and that username/password are set together. This extends the check beyond basic connectivity for email-infrastructure monitoring.

### Latency percentile Grafana panels

Added P50/P90/P99 stat panels plus a latency-distribution histogram to the HTTP Timing (TTFB) and Uptime Overview (check duration) dashboards. Because `technician_check_duration_seconds` / `technician_http_ttfb_seconds` are gauges (not histograms), the percentiles are computed with `quantile_over_time(φ, metric[$__range])` rather than `histogram_quantile`, and the histogram panels bin the raw gauge samples.

### Scheduler dependency: robfig/cron → gronx + stdlib

Replaced the abandoned `github.com/robfig/cron/v3` (last release 2020, Renovate-flagged) with [`gronx`](https://github.com/adhocore/gronx) for cron-expression parsing plus a standard-library timer loop we own. Each check runs in its own `runLoop` goroutine that computes the next tick with `gronx.NextTick` and waits for it; runs are synchronous per check, so a slow probe can delay or skip its own next tick but never pile up overlapping runs. Shutdown now waits on a `sync.WaitGroup` covering both the schedule loops and the one-shot startup runs before closing the results channel. `robfig/cron/v3` is gone from `go.mod` entirely (direct and indirect).

### Status store backup rotation

Already shipped (issue [#41](https://github.com/jesseheady/technician/issues/41) closed as done). The status store writes dated daily backups of `status.json` with 90-day retention (`SaveBackup`/`pruneBackups` in `internal/status/store.go`), and `load()` falls back to the most recent backup when the primary file is missing or fails to parse — guarding against corrupt writes. Rotation is time-based (90 days) rather than a fixed count, achieving the same protection.

### Run all checks once on startup

The scheduler executes every check once immediately on boot (each after its existing stagger delay, in the background) before settling into the cron schedule. The status page, Prometheus metrics, and alert rules now have data within seconds of startup instead of waiting up to a full interval for the first tick — which mattered most for long-interval checks like TLS-cert and domain-expiry monitoring.

- **Implementation**: `internal/scheduler/scheduler.go` shares one `execute` path between the startup run and the cron job; `runInitial` fans out the one-shot runs. A check may run once on startup and again on its first cron tick, which is harmless for these read-only probes.

### Native HTTP Basic/Bearer auth fields

HTTP checks accept dedicated `basic_auth` (`username`/`password`) and `bearer_token` fields instead of hand-rolling an `Authorization` header. The two are mutually exclusive and validated at config load time; dedicated fields take precedence over any raw `Authorization` header.

- **Config**: `basic_auth: {username, password}` or `bearer_token` on HTTP checks (supports `${ENV_VAR}` expansion).
- **Implementation**: `internal/check/http.go` applies the credentials via `req.SetBasicAuth` / a `Bearer` header; `internal/config/checks.go` enforces mutual exclusion.

### TLS version constraints for HTTP and TCP checks

HTTP and TCP (with `tls: true`) checks accept `min_tls` and `max_tls` fields (`"1.0".."1.3"`) to pin the negotiated TLS version. Values are validated at config load time — unknown versions and an inverted min/max range are rejected.

- **Config**: `min_tls` / `max_tls` on HTTP and TCP checks (omit for the Go default range).
- **Implementation**: `config.TLSVersion` maps the strings to `crypto/tls` constants; `internal/check/http.go` and `internal/check/tcp.go` apply them to the `tls.Config`.

### IP protocol preference for HTTP checks

HTTP checks accept an `ip_version` field (`"4"`, `"6"`, or empty for either family) to force the dial address family, matching the existing TCP/UDP/ICMP checks.

- **Config**: `ip_version` on HTTP checks in the checks YAML.
- **Implementation**: `internal/check/http.go` pins the transport `DialContext` to `tcp4`/`tcp6`; clients are cached per address-family so connection pooling is preserved.

### Native webhook notifications with severity routing

Built-in webhook alerting directly from the Technician worker, independent of Prometheus/Grafana. Fires on check state transitions (up→down, down→up), new budget violations, and TLS certificate expiry warnings, with per-check cooldown to prevent notification floods.

- **Package**: `internal/notify/` — `Manager` with state tracking, `Sender` interface with Discord, Slack, and generic HTTP implementations.
- **Config**: `webhooks` list in `technician.yml` with `url`, `type` (discord/slack/generic), `events` (check_down/check_up/budget_violation/cert_expiring), `severities` (warning/critical — omit for all), and `cooldown` (default 5m).
- **Severity levels**: Events carry a severity — `check_down` = critical, `budget_violation` = warning, `cert_expiring` = warning or critical (based on days remaining vs `warn_days`/`critical_days` thresholds). Multiple webhook entries with different `severities` filters enable routing: e.g. Slack receives all alerts, PagerDuty only receives critical.
- **Cert expiry notifications**: The `cert_expiring` event fires once when entering the warning window, escalates if it reaches critical, and resets after cert renewal. State tracking prevents duplicate notifications on every check cycle.
- **CLI**: `technician test-webhook` sends a test notification to all configured webhooks.
- **Docs**: [alerting.md](alerting.md) covers native webhooks, Grafana alerting (recommended), and Alertmanager.

### Budget check persistence and escalation

Budget badge state is persisted alongside check history in `status.json`, surviving container restarts. Badges use a three-state escalation model: **pass** (green) → **warn** (amber, transient violation) → **fail** (red, 3+ consecutive violations). Webhook notifications only fire when crossing the fail threshold, reducing noise from single-run spikes. The persistence format is backwards-compatible with older `status.json` files.

### Latency percentiles and timing breakdown

Status page now shows P50/P90/P95/P99 latency percentiles per check (computed from ring buffer) and a color-coded HTTP timing breakdown bar (DNS/TLS/TTFB/transfer) with legend. Both are included in the `/api/status` JSON response.

### HTTP body assertions

HTTP checks support `assertions` for response body validation: `contains`, `not_contains`, and `regex`. Failed assertions mark the check as failed. Config validation catches invalid types and malformed regex at load time.

### TCP check

TCP connectivity check with IPv4/IPv6 selection, optional TLS handshake, and send/expect pattern matching. Records connection and TLS durations separately.

### DNS check

DNS query check supporting A, AAAA, MX, TXT, CNAME, NS, and SRV record types. Configurable DNS server, with assertion support for expected answer values. Uses Go standard library `net.Resolver`.

### ICMP ping check

ICMP Echo Request check with IPv4/IPv6 selection, configurable ping count, and packet loss/RTT statistics (min/avg/max). Falls back to unprivileged UDP mode when raw socket access is unavailable.

### gRPC health check

gRPC check using the standard health check protocol (`grpc.health.v1.Health/Check`). Supports TLS with optional certificate verification skip. Reports serving status.

### HTTP header assertions

HTTP assertions extended with `header_contains`, `header_not_contains`, and `header_regex` types. Each requires a `header` field specifying which response header to match against.

### Follow redirects toggle

HTTP checks now support `follow_redirects: true` to follow HTTP redirects (default: false, matching previous behavior).

### Retry policy

All check types support an optional `retry` config with `count`, `backoff` (none/linear/exponential), and `delay`. Retries only trigger on check failure. Implemented at the scheduler level.

### Response time thresholds

All check types support `degraded_after` — a duration threshold. When a successful check exceeds this duration, it's flagged as degraded (`technician_check_degraded` metric). Distinct from failure: the check passed, but response time indicates degradation.

### NTP check

Pure-Go NTPv4 client for querying time servers over UDP. Reports clock offset, stratum, and round-trip time. No external dependencies. Prometheus gauges: `technician_ntp_offset_ms`, `technician_ntp_stratum`, `technician_ntp_rtt_seconds`.

### TLS certificate monitoring

Dedicated `tls` check type for monitoring certificate expiry, chain validity, and issuer details. Connects to host:port, performs TLS handshake, and inspects the certificate chain. Config struct `TLSProbeConfig` with fields: `host` (host:port), `check_expiry` (bool, default true), `warn_days` (int, default 30), `critical_days` (int, default 7). Reports subject, issuer, SANs, expiry, days remaining, and chain validity. Prometheus gauges: `technician_tls_cert_expiry_days`, `technician_tls_cert_valid`.

### Infrastructure Checks dashboard

Grafana dashboard combining TCP, DNS, ICMP, gRPC, NTP, TLS, UDP, BGP, and domain expiry check metrics in a single view. Includes per-type rows with relevant panels (connect/TLS time, query time, packet loss, health status, clock offset, certificate expiry, prefix visibility, origin ASN match, domain days until expiry).

### Browser concurrency limiter

`max_browsers` config field (default 2) caps concurrent Chromium instances via a channel-based semaphore. Prevents OOM when multiple Playwright checks overlap. Checks queue for a slot and fail with an infra error if their timeout expires while waiting. See [Playwright scaling](playwright-scaling.md).

### CI workflow

GitHub Actions workflow (`.github/workflows/ci.yml`) with build, test (race detector + coverage), lint, validate (with and without Playwright), Docker build, and govulncheck security scan. `CI Passed` gate job aggregates results for branch protection. Paths-ignore skips CI for docs-only changes. Generic CI guidance for GitLab, CircleCI, and Jenkins in [docs/ci.md](ci.md).

### Release workflow

GitHub Actions workflow (`.github/workflows/release.yml`) triggered on `v*` tag push. Builds binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64. Creates a GitHub Release with binaries attached and auto-generated changelog categorized by PR labels (`.github/release.yml`).

### Pre-commit hook

`.githooks/pre-commit` runs `go build`, `go vet`, `go test -race`, and `govulncheck` (optional) on every commit. Mirrors CI locally. Configured automatically by `scripts/init.sh` via `git config core.hooksPath .githooks`.

### Automated dependency updates

Renovate (`renovate.json`) is the sole updater for Go modules, GitHub Actions, and Docker base images — auto-merging non-major updates, grouping patch/minor/OTel/AWS-SDK bumps, and running lock-file maintenance. Dependabot **alerts** stay enabled and Renovate's `vulnerabilityAlerts` raises the fixes; Dependabot's own version-update PRs are off (no `.github/dependabot.yml`). Originally shipped as a Dependabot config, since migrated.

### Branch protection

Main branch requires `CI Passed` status check. Admin bypass enabled for maintainer direct pushes. Contributors must open PRs.

### Log level flag

`--log-level` CLI flag (debug, info, warn, error) for controlling log verbosity. Default remains INFO. Part of the broader structured logging effort ([#24](https://github.com/jesseheady/technician/issues/24)), now complete — see the [#24](https://github.com/jesseheady/technician/issues/24) entry above.

### CONTRIBUTING.md and SECURITY.md

Contributing guide with setup, pre-commit hooks, code style, and PR guidelines. Security policy directing vulnerability reports to GitHub's private reporting feature.

### Mermaid architecture diagram

README architecture diagram updated from ASCII art to Mermaid with all 13 check types. Renders natively on GitHub.
