# Roadmap

Work that's been planned or partially designed but not yet implemented. See also [Recently completed](#recently-completed) for items that have shipped.

## Edge deployment adapters

### AWS Lambda adapter

Technician's Go binary can run in a Lambda container image (regional Lambda), but there's no Lambda-specific packaging or infrastructure yet.

**What's needed:**

- Lambda handler that wraps the existing probe execution (reuse `internal/probe` and `internal/exporter`) behind a Lambda function URL or API Gateway trigger.
- SAM template or Terraform/CDK for provisioning: function, EventBridge schedule (replaces in-process cron), IAM role, networking (VPC placement if probing internal targets).
- Decision on invocation model: EventBridge schedule triggers Lambda per-probe, or a single invocation runs all probes and pushes results.
- Push mechanism for metrics: Prometheus can't scrape a short-lived Lambda. Options are Pushgateway, Prometheus remote-write, or an in-VPC aggregator that Prometheus scrapes. See [central-prometheus-grafana.md](architecture/central-prometheus-grafana.md).
- Lambda@Edge (Node.js/Python only) would need a separate lightweight HTTP probe adapter, not the Go binary.

### Cloudflare Workers adapter

Designed in [proposals/cloudflare-workers.md](proposals/cloudflare-workers.md). The proposal recommends "Path A" — a small JS/TS Worker that performs one HTTP probe per request and returns Prometheus text or JSON.

**What's needed:**

- Reference Worker implementation (JS/TS) under e.g. `workers/cf/` with Wrangler config.
- Same `/probe?target=&module=` contract as the existing blackbox handler so Prometheus treats Worker and Technician endpoints identically.
- Cron Trigger configuration for scheduled probes.
- Documentation for how Prometheus or an aggregator scrapes/receives Worker results.

## Metrics and persistence

### Prometheus remote-write

Add native Prometheus remote-write support to Technician, configured via `metrics.prometheus.remote_write_url` in `technician.yml`. This lets workers push metrics directly to AWS Managed Prometheus (AMP), Grafana Cloud, Thanos, or Mimir without needing a sidecar agent.

**What's needed:**

- New config field: `metrics.prometheus.remote_write_url` (and optional `remote_write_sigv4` for AMP auth).
- Remote-write client using the Prometheus remote-write protocol (protobuf over HTTP).
- Push after each probe result, or batch on a timer (e.g. every 15s).
- SigV4 signing for AMP endpoints (AWS SDK is already a dependency).

### Status page historical data

The built-in status page shows recent results from an in-memory ring buffer (90 entries per probe, ~45 min at 30s intervals), persisted to a JSON file on disk so history survives restarts and container rebuilds. For longer historical views (30-day uptime bars, etc.), two additional paths:

**Path A: Query Prometheus API** — Add `metrics.prometheus.url` config. The status page handler queries Prometheus for historical uptime and timing aggregates. No new storage, but requires Prometheus to be reachable from the worker.

**Path B: Embedded SQLite** — Use `modernc.org/sqlite` (pure Go, no CGO) for local probe result persistence. Adds ~2 MB to the binary. Stores 30 days of results in a local file (~100 MB for 10 probes). Good for standalone workers without Prometheus access.

See [deployment-sizing.md § Persistence](deployment-sizing.md#persistence-and-historical-data) for the full analysis.

### SLA reporting

Generate periodic SLA reports showing uptime, latency percentiles, and incident counts over configurable windows (30, 90, 365 days). Reports can be scoped to specific probe groups — e.g. report on "Marketing" and "Infrastructure" while omitting "Third Party" probes that aren't covered by your SLA.

**Depends on:** SQLite persistence (see [Status page historical data](#status-page-historical-data)) or Prometheus API access for historical queries.

**What's needed:**

- `technician report` CLI command with flags: `--period 30d|90d|365d`, `--groups "Marketing,Infrastructure"` (default: all), `--format html|json|csv`, `--output report.html`.
- Report data model: per-probe uptime %, p50/p95/p99 latency, incident count and total downtime, grouped by probe group.
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

### IaC templates

Terraform or CloudFormation templates for common deployment patterns:

- VPS worker (systemd unit + config)
- ECS service (task definition, service discovery, AMP scraper config)
- Lambda function (EventBridge schedule, IAM role, Pushgateway push)
- Central stack (Prometheus + Grafana on a single host)

## Near-term MVP

Features planned for the next development cycle.

### Maintenance mode

Suppress alerting and mark probes as "maintenance" on the status page during planned windows. Prevents alert fatigue during deploys or scheduled downtime.

**What's needed:**

- Per-probe `maintenance` config: either a boolean `maintenance: true` for manual toggle, or a time window `maintenance_windows` with start/end times.
- Scheduler skips webhook notifications for probes in maintenance (probes still run to track actual state).
- Status page shows maintenance badge instead of failure state.
- Prometheus label `maintenance="true"` on probe metrics during active windows so Grafana alerting rules can exclude them.
- Optional: `technician maintenance enable <probe-name> --duration 2h` CLI command for ad-hoc maintenance without config edit.

**Config shape:**

```yaml
# In probe config
- name: API Server
  url: https://api.example.com
  maintenance: false                    # manual toggle
  maintenance_windows:
    - start: "2026-03-15T02:00:00Z"    # scheduled window
      end: "2026-03-15T04:00:00Z"
      reason: "Database migration"
```

**Status page display:** Maintenance probes show a wrench/tool icon and "Scheduled Maintenance" label with the reason text, replacing the normal up/down indicator.

### WebSocket monitoring

WS/WSS probe type for real-time services. Connect, optionally send a message, assert on the response, measure connection time.

**What's needed:**

- New `WebSocketProbeConfig` struct: `url` (ws:// or wss://), `headers` (map), `send` (message to send after connect), `expect` (expected response substring or regex), `skip_tls` (bool).
- WebSocket prober using `golang.org/x/net/websocket` or `nhooyr.io/websocket` (pure Go, no CGO). Connect, optionally write `send` payload, read first message, evaluate `expect` assertion, close.
- Prometheus gauges: `technician_ws_connect_seconds`, `technician_ws_message_seconds`.
- Probe result fields: `WSConnDuration`, `WSMessageDuration`, `WSResponse`.
- Blackbox `/probe` handler: `module=websocket`.

**Config shape:**

```yaml
# config/probes/websocket.yml
- name: Live Feed
  url: wss://stream.example.com/feed
  send: '{"type":"ping"}'
  expect: '"type":"pong"'
  timeout: 10s
  schedule: "*/60 * * * * *"
```

### Structured logging for Loki

Technician already uses Go's `slog` for structured logging to stdout. Enhance the log output so it's immediately useful when consumed by Grafana Loki (or any log aggregation pipeline), giving visibility into how Technician itself is performing.

**What's needed:**

- **Health log line per probe execution** — After each probe run, emit a structured log with: probe name, type, success, duration, region, degraded flag, retry count (if retried). This gives Loki a complete record of probe execution independent of Prometheus metrics.
- **Technician self-health metrics** — Log scheduler loop timing, goroutine count, memory usage, and config reload events. Useful for diagnosing "is Technician itself healthy?" without needing a separate monitoring stack.
- **Log format config** — `logging.format` in `technician.yml`: `json` (default, Loki-native) or `text` (human-readable for local dev). `logging.level`: `debug`, `info` (default), `warn`, `error`.
- **Correlation IDs** — Each probe execution gets a trace ID logged alongside the result, linking slog output to OTLP traces when tracing is enabled.

**Config shape:**

```yaml
logging:
  format: json       # json | text
  level: info        # debug | info | warn | error
```

**Example log output (JSON):**

```json
{"time":"2026-03-11T10:00:30Z","level":"INFO","msg":"probe_complete","probe":"API Health","type":"http","success":true,"duration_ms":142,"region":"us-east-1","degraded":false,"retries":0}
{"time":"2026-03-11T10:00:30Z","level":"INFO","msg":"scheduler_tick","active_probes":12,"goroutines":28,"heap_mb":8.2}
```

### Status page redesign

Redesign the built-in status page. Reference layout based on Upptime, Cachet, and Gatus.

**Current state:** Minimal HTML template with probe rows, history bars, timing breakdown, and budget badges.

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
- **Monitor grouping with collapse** — Group probes by the existing `group` field with collapsible section headers. Header shows: group name + optional icon (left), "N/N Operational" aggregate count (right), collapse chevron. Group-level status is worst-of-group.
- **Maintenance banners** — When any probe is in maintenance mode, show a blue/gray banner with the reason text and scheduled end time. Maintenance probes show a wrench icon instead of status dot.
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

Technician's scope: **probe execution, scheduling, metrics export, status page, and performance budgets — as a single binary.** Visualization, incident management, and complex alerting belong to Grafana and dedicated tools.

### What Technician owns

- **Probe execution** — HTTP, TCP, DNS, ICMP, gRPC, NTP, TLS, SMTP, traceroute, Playwright (and planned: WebSocket).
- **Scheduling** — Built-in cron with stagger/jitter. No external scheduler needed.
- **Status page** — Built-in, no external dependencies.
- **Notifications** — Webhook-based alerting for probe state transitions, cert expiry, and budget violations with severity-based routing (warning/critical).
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

## Possible enhancements

Items here are either partially covered by Grafana dashboards, low priority, or would add complexity that isn't justified yet. Each item includes a rationale for deferral.

### Probes and protocol

- **TLS version constraints** — Min/max TLS version for HTTP and TCP probes. Low priority; rarely needed for synthetic monitoring.

- **Proxy support** — HTTP proxy configuration for probes running behind corporate proxies. Edge case for most deployments.

- **IP protocol preference for HTTP** — Force IPv4 or IPv6 for HTTP probes (TCP/DNS/ICMP already support this). Low priority.

- **Full SOA record support** — DNS probe SOA queries require `miekg/dns` for full answer parsing. Current fallback verifies domain resolution.

- **Native HTTP Basic/Bearer auth fields** — Dedicated config fields instead of raw headers. Achievable via `headers` config today; first-class fields are a convenience, not a capability gap.

- **SMTP STARTTLS and auth** — The SMTP probe currently verifies basic mail server connectivity only. Full STARTTLS negotiation and authenticated sends would add value for email infrastructure monitoring. Moderate effort.

### Observability and export

- **Full OTel metrics export** — Push probe metrics via OpenTelemetry in addition to Prometheus. Tracing is implemented; metrics export is not. Medium priority.

- **Prometheus backfill on startup** — If the local store is empty or stale, query Prometheus for recent probe metrics and reconstruct the ring buffer. Limitation: HTTP timing breakdown and assertion details aren't in the metrics, so backfilled history would be partial.

### Status page and UI

- **Latency percentile Grafana panels** — The Grafana dashboards have latency trend graphs but no dedicated P50/P90/P99 panels. Add percentile stat panels and histogram panels to the HTTP Timing and Uptime Overview dashboards.

- **Per-region latency comparison** — Side-by-side latency by region. Deferred until [central Prometheus](architecture/central-prometheus-grafana.md) is in place, at which point Grafana handles this natively via `region` label grouping.

- **Latency trend sparklines on status page** — Small inline SVG sparklines per probe row. The Grafana HTTP Timing dashboard already shows latency trends. Adding SVG sparklines to the status page is possible but adds template complexity for marginal benefit over existing history bars.

- **Tags and filtering** — Arbitrary key-value tags on probes for filtering on the status page (beyond the existing `group` field). Low priority since groups already provide the primary organization dimension.

- **Public/private visibility toggle** — Control which probes are visible on the public status page vs internal-only.

### Notifications and alerting

- **Additional notification channels** — First-class Email, Telegram, PagerDuty, OpsGenie senders. The generic webhook sender already covers any HTTP-based integration. Add first-class senders only when the generic sender proves insufficient for a specific channel's payload format.

- **Slack chatbot / agent** — Manage status pages and incidents via Slack commands. Overlaps with Grafana OnCall and existing webhook notifications. Not justified at current scale.

### Operations

- **Module path** — `github.com/monkeyWzr/technician` uses a personal GitHub account. Consider a dedicated org or project namespace.

- **Status store backup rotation** — Keep N rotated copies of `status.json` to guard against corrupt writes. Simple, no external dependencies.

- **Incident tracking** — Automatic incident creation/resolution from probe failures. Grafana Alerting provides incident-style state management (firing → resolved), and PagerDuty/Grafana OnCall/OpsGenie integrate via the generic webhook sender. Building a first-party incident system would duplicate existing tooling.

See `docs/internal/` for full feature gap analyses against specific tools.

## Recently completed

### Native webhook notifications with severity routing

Built-in webhook alerting directly from the Technician worker, independent of Prometheus/Grafana. Fires on probe state transitions (up→down, down→up), new budget violations, and TLS certificate expiry warnings, with per-probe cooldown to prevent notification floods.

- **Package**: `internal/notify/` — `Manager` with state tracking, `Sender` interface with Discord, Slack, and generic HTTP implementations.
- **Config**: `webhooks` list in `technician.yml` with `url`, `type` (discord/slack/generic), `events` (probe_down/probe_up/budget_violation/cert_expiring), `severities` (warning/critical — omit for all), and `cooldown` (default 5m).
- **Severity levels**: Events carry a severity — `probe_down` = critical, `budget_violation` = warning, `cert_expiring` = warning or critical (based on days remaining vs `warn_days`/`critical_days` thresholds). Multiple webhook entries with different `severities` filters enable routing: e.g. Slack receives all alerts, PagerDuty only receives critical.
- **Cert expiry notifications**: The `cert_expiring` event fires once when entering the warning window, escalates if it reaches critical, and resets after cert renewal. State tracking prevents duplicate notifications on every probe cycle.
- **CLI**: `technician test-webhook` sends a test notification to all configured webhooks.
- **Docs**: [alerting.md](alerting.md) covers native webhooks, Grafana alerting (recommended), and Alertmanager.

### Budget check persistence and escalation

Budget badge state is persisted alongside probe history in `status.json`, surviving container restarts. Badges use a three-state escalation model: **pass** (green) → **warn** (amber, transient violation) → **fail** (red, 3+ consecutive violations). Webhook notifications only fire when crossing the fail threshold, reducing noise from single-run spikes. The persistence format is backwards-compatible with older `status.json` files.

### Latency percentiles and timing breakdown

Status page now shows P50/P90/P95/P99 latency percentiles per probe (computed from ring buffer) and a color-coded HTTP timing breakdown bar (DNS/TLS/TTFB/transfer) with legend. Both are included in the `/api/status` JSON response.

### HTTP body assertions

HTTP probes support `assertions` for response body validation: `contains`, `not_contains`, and `regex`. Failed assertions mark the probe as failed. Config validation catches invalid types and malformed regex at load time.

### TCP probe

TCP connectivity probe with IPv4/IPv6 selection, optional TLS handshake, and send/expect pattern matching. Records connection and TLS durations separately. Config: `config/probes/tcp.yml`.

### DNS probe

DNS query probe supporting A, AAAA, MX, TXT, CNAME, NS, and SRV record types. Configurable DNS server, with assertion support for expected answer values. Uses Go standard library `net.Resolver`. Config: `config/probes/dns.yml`.

### ICMP ping probe

ICMP Echo Request probe with IPv4/IPv6 selection, configurable ping count, and packet loss/RTT statistics (min/avg/max). Falls back to unprivileged UDP mode when raw socket access is unavailable. Config: `config/probes/icmp.yml`.

### gRPC health check probe

gRPC probe using the standard health check protocol (`grpc.health.v1.Health/Check`). Supports TLS with optional certificate verification skip. Reports serving status. Config: `config/probes/grpc.yml`.

### HTTP header assertions

HTTP assertions extended with `header_contains`, `header_not_contains`, and `header_regex` types. Each requires a `header` field specifying which response header to match against.

### Follow redirects toggle

HTTP probes now support `follow_redirects: true` to follow HTTP redirects (default: false, matching previous behavior).

### Retry policy

All probe types support an optional `retry` config with `count`, `backoff` (none/linear/exponential), and `delay`. Retries only trigger on probe failure. Implemented at the scheduler level.

### Response time thresholds

All probe types support `degraded_after` — a duration threshold. When a successful probe exceeds this duration, it's flagged as degraded (`technician_probe_degraded` metric). Distinct from failure: the probe passed, but response time indicates degradation.

### NTP probe

Pure-Go NTPv4 client for querying time servers over UDP. Reports clock offset, stratum, and round-trip time. No external dependencies. Config: `config/probes/ntp.yml`. Prometheus gauges: `technician_ntp_offset_ms`, `technician_ntp_stratum`, `technician_ntp_rtt_seconds`.

### TLS certificate monitoring

Dedicated `tls` probe type for monitoring certificate expiry, chain validity, and issuer details. Connects to host:port, performs TLS handshake, and inspects the certificate chain. Config struct `TLSProbeConfig` with fields: `host` (host:port), `check_expiry` (bool, default true), `warn_days` (int, default 30), `critical_days` (int, default 7). Reports subject, issuer, SANs, expiry, days remaining, and chain validity. Prometheus gauges: `technician_tls_cert_expiry_days`, `technician_tls_cert_valid`. Config: `config/probes/tls.yml`.

### Infrastructure Probes dashboard

Grafana dashboard combining TCP, DNS, ICMP, gRPC, NTP, and TLS probe metrics in a single view. Includes per-type rows with relevant panels (connect/TLS time, query time, packet loss, health status, clock offset, certificate expiry).

### Browser concurrency limiter

`max_browsers` config field (default 2) caps concurrent Chromium instances via a channel-based semaphore. Prevents OOM when multiple Playwright probes overlap. Probes queue for a slot and fail with an infra error if their timeout expires while waiting. See [Playwright scaling](playwright-scaling.md).

### CI workflow

GitHub Actions workflow (`.github/workflows/ci.yml`) with build, test (race detector + coverage), lint, validate (with and without Playwright), and Docker build jobs. Generic CI guidance for GitLab, CircleCI, and Jenkins in [docs/ci.md](ci.md).
