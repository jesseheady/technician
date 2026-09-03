# Roadmap

Work that's been planned or partially designed but not yet implemented. See also [Recently completed](#recently-completed) for items that have shipped.

Several items here link to **closed** issues. That is deliberate: the tracker holds actionable work, and aspirations are parked here instead so the issue list stays honest. A closed link means "designed, not scheduled". Reopen it with a concrete proposal if it gets prioritized.

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

### Status page historical data

The built-in status page shows recent results from an in-memory ring buffer (90 entries per check, ~45 min at 30s intervals), persisted to a JSON file on disk so history survives restarts and container rebuilds. For longer historical views (30-day uptime bars, etc.), two additional paths:

**Path A: Query Prometheus API** — Add `metrics.prometheus.url` config. The status page handler queries Prometheus for historical uptime and timing aggregates. No new storage, but requires Prometheus to be reachable from the worker. **Deferred**, likely alongside SLA reporting: it serves the long tail (a year or more) and can be added later without disturbing Path B.

**Path B: Embedded SQLite — the decided path.** Uses `modernc.org/sqlite` (pure Go, no CGO) for local check result persistence. Adds ~4 MB to the binary. **The persistence layer has shipped** ([#16](https://github.com/jesseheady/technician/issues/16), see Recently completed) — `persistence.enabled` writes results to `results.db` with retention. What remains is *rendering* that history on the status page (30-day uptime bars), which waits on the status-page redesign below and the public-exposure question (#39).

This is first-class regardless of whether the page is public. The ring buffer spans roughly 45 min at 30s intervals and ~90 min at 1 min intervals, so the page cannot answer "was this unstable last week" in *any* deployment, including full-stack ones where Prometheus holds the data. It also does two jobs at once: it sets how far back the page can look, and it keeps that history readable when Prometheus is unreachable.

**Write-through, not sync.** Results are written to SQLite in the same push that feeds the ring buffer. A Prometheus → SQLite sync was considered and rejected: its only capability beyond write-through is backfilling results the worker never recorded, which is lossy by construction (HTTP timing breakdown and assertion detail are absent from the metrics) and would leave two tiers of row quality in one table.

**Sizing estimates (30 checks, recommended production intervals):**

| Retention | Rows | Disk | Memory |
|---|---|---|---|
| 30 days | ~674K | 70–130 MB | 2–5 MB |
| 90 days | ~2M | 200–400 MB | 2–5 MB |
| 12 months | ~8.2M | 0.8–1.6 GB | 2–5 MB |

Memory does not scale with retention. SQLite reads pages on demand; the page cache stays at 2–5 MB. At 100 checks and 12 months, disk reaches 2.5–5 GB. At 500 checks, 12–25 GB.

**Implementation:** Single `probe_results` table, covering index on `(name, timestamp, success)`, configurable retention (`persistence.retention`, 30d proposed default), periodic prune with `PRAGMA auto_vacuum=incremental`. Off by default, behind a narrow internal interface (append a result, query a window) with one implementation — no config-selectable database driver. The existing ring buffer stays for real-time rendering; SQLite is queried for historical views, and `status.json` continues to hold non-series state (order, budget state, down-since).

**State the ceiling in the UI.** The page shows what window it covers and says that longer history lives in Grafana. Documenting the limit only in `docs/` is not enough, since the page is where someone forms the assumption that it is authoritative.

Edge deployments (Lambda, Workers) have no durable disk, so the page must degrade to ring-buffer-only rather than assume local history.

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

Mostly delivered. `deploy/cloudformation/ecs-fargate.yaml` covers the ECS service (task definition, Cloud Map discovery, IAM, optional ADOT sidecar for AMP) and `deploy/systemd/technician.service` covers the VPS worker. The central stack needs nothing new, because `docker-compose.yml` already is Prometheus + Alertmanager + Grafana on one host.

What remains is the Lambda template, which is blocked on the adapter above: the invocation model decides what the template looks like, so it belongs with #17.

Per-provider IaC (Hetzner, Linode, Vultr, DigitalOcean) is deliberately out of scope: they all run the Compose stack unmodified, so what differs is billing, not deployment. Kubernetes is covered by the Helm chart.

## Designed, not scheduled

Both items below have a recorded decision to defer. Neither is in a development cycle.

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
- **Grafana dashboard** — Pre-built k6 dashboard (provisioned alongside the existing dashboards) showing request rate, response time distribution, error rate, VU count, and iteration throughput. Sourced from k6's official Prometheus dashboard with adjustments to match Technician's Grafana theme.
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

### Status page and UI

- **Public/private visibility toggle** [#39](https://github.com/jesseheady/technician/issues/39) — Control which checks are visible on the public status page vs internal-only.

### Notifications and alerting

- **Additional notification channels** [#40](https://github.com/jesseheady/technician/issues/40) — First-class Email, Telegram, PagerDuty, OpsGenie senders. The generic webhook sender already covers any HTTP-based integration. Add first-class senders only when the generic sender proves insufficient for a specific channel's payload format.

- **Slack chatbot / agent** — Manage status pages and incidents via Slack commands. Overlaps with Grafana OnCall and existing webhook notifications. Not justified at current scale.

### Operations

- **Incident tracking** — Automatic incident creation/resolution from check failures. Grafana Alerting provides incident-style state management (firing → resolved), and PagerDuty/Grafana OnCall/OpsGenie integrate via the generic webhook sender. Building a first-party incident system would duplicate existing tooling.

See `docs/internal/` for full feature gap analyses against specific tools.

## Recently completed

### Config is mounted by directory [#232](https://github.com/jesseheady/technician/issues/232)

`docker-compose.yml` bind-mounted individual config *files*. Editors, and tools like `sed -i`, save by writing a temp file and renaming it, which swaps the file's inode; a container holding a single-file mount keeps reading the old one, so an edit followed by `docker compose restart` could serve stale or truncated config. This bit us live during multi-target testing, when a `prometheus.yml` edit left a truncated scrape target in place until the container was force-recreated.

The worker and Prometheus now mount `./config` and `./prometheus` as directories, which resolve each path on open. Alertmanager mounts its source directory read-only at `/etc/alertmanager/src` and renders the expanded config to `/tmp`, because its entrypoint writes the rendered file and so its config directory cannot be read-only. Grafana keeps single-file mounts: its provisioning expects `datasources/`, `dashboards/` and `alerting/` subdirectory names the repo layout does not use, and those files are read once at startup.

Verified by swapping a file's inode with `sed -i` and confirming the container saw the new bytes with no restart at all, then that a plain `restart` loaded them. The `--force-recreate` guidance in `README.md`, `AGENTS.md` and `docs/alerting.md` is replaced by `restart`.

### Git hook images are pinned by digest [#274](https://github.com/jesseheady/technician/issues/274)

`.githooks/pre-commit` and `.githooks/pre-push` ran four images by floating tag, the only images in the repo that were not pinned. A floating tag can change under us with no diff, altering hook behaviour or pulling a compromised image. All four are now pinned as `name:version@sha256:digest`, the form used everywhere else; the Prometheus and Alertmanager pins match `docker-compose.yml`, so the hooks validate config with the versions the stack runs.

Pinning alone would have made the problem worse, because no standard Renovate manager reads shell scripts and the digests would never have been updated again. A `customManagers` regex entry now tracks them, grouped with the other image updates. The pinned digests are the ones the floating tags resolved to at the time, so no hook behaviour changed.

### A broken container image blocks merge [#304](https://github.com/jesseheady/technician/issues/304)

`Scan container image` could fail while every required context passed, so a pull request that broke the image reached a green merge button. The half of this that runs `Docker build` on pull requests touching image inputs already shipped, and it feeds `CI Passed`. The scan itself was still advisory. A `Container scan passed` aggregator now gates the Trivy jobs and is a required status check on main. It runs with `if: always()`, the same way `CI Passed` does, because the image scan is skipped on most pull requests by design and a required context that never reports would leave those pull requests unmergeable.

### validate applies the retry policy [#363](https://github.com/jesseheady/technician/issues/363)

CI ran the example config against live third-party endpoints with `--fail-on-error`, so main's build status followed the availability of services we do not control. A short RIPE Stat outage failed main; the same commit passed on a re-run. `cmd/validate.go` called `checker.Run` directly and therefore ignored the `retry:` policy, although the scheduler has implemented it all along and the example BGP checks declare it. `RunWithRetry` moves from unexported to exported in `internal/scheduler`, and validate now calls it, so a check behaves the same way in CI and in production. Retries cost time only when a check fails, so a green run takes no longer than before.

Widening the `InfraError` classification, and the question of whether an unavailable dependency should fail a CI build at all, stay with [#333](https://github.com/jesseheady/technician/issues/333).

### Unit tests no longer use the network [#355](https://github.com/jesseheady/technician/issues/355)

`TestDNSCheckerTXTRecord` and six sibling tests resolved real names against `8.8.8.8`, so a slow network or a dropped UDP packet failed `go test`. Because `.githooks/pre-commit` runs `go test -race` on every commit, that flake blocked all work, including documentation-only changes, and trained contributors to bypass the hook with `SKIP_HOOKS=1`. The tests now query the in-process `miekg/dns` server the file already used for SOA coverage, so they keep real protocol coverage with no external dependency. The full suite passes with outbound network denied; `docs/testing-and-e2e.md` states the invariant and gives the command that proves it.

### INP measures the check script's own interactions [#376](https://github.com/jesseheady/technician/issues/376)

The browser runner made a `page.click('body')` before it read INP. That click starts no event handler on almost all pages, so the measured latency stayed near 0 ms. Each failure path returned 0 as well, and 0 went to the `technician_browser_inp_ms` gauge and to the budget map. A reader could not tell "no interaction" from "a very fast interaction", and `HighINP`, `HighINPCritical` and every `inp` budget threshold stayed inert. The runner also kept the first value from `onINP` instead of the last one. That is theoretically wrong, but it changed no measurement, because `web-vitals` reads the buffered event entries and sends the worst interaction in its first callback.

The forced click is gone. `onINP` now registers beside `onLCP` and `onCLS` in the one `page.evaluate` block, and it keeps the last value, which is correct by definition. INP therefore measures the interactions that the check script itself makes. A check that only loads a page emits no INP sample and applies no `inp` threshold. Existing budget files need no change: an absent metric is skipped, not failed.

### BGP check detects more-specific hijacks [#382](https://github.com/jesseheady/technician/issues/382)

The check asked RIPE Stat `network-info`, which describes the configured prefix only. The common hijack announces a longer prefix inside it. That announcement wins longest-prefix-match and never changes the origin of the covering prefix, so the check stayed green through it. The check now asks `routing-status`, which returns the origins of the prefix and the more-specifics announced inside it in one request, so this costs no extra call. A more-specific from an ASN other than `expected_origin` fails the check and clears `technician_bgp_origin_match`, which fires the existing `BGPOriginMismatch` alert. More-specifics that `expected_origin` announces itself are normal deaggregation and pass. A prefix that is announced only as more-specifics now reports as visible, which `network-info` could not express.

### BGP check flags a newly-appeared more-specific even with the expected origin [#384](https://github.com/jesseheady/technician/issues/384)

The [Softaculous/Virtualizor BGP hijack](https://www.kentik.com/blog/latest-bgp-hijack-targets-hosting-software-vendor/) (August 2026) forged the AS path for a hijacked /24 so it ended in the real operator's ASN (Hetzner, AS24940), even though a different AS injected the route. Both an origin comparison and RPKI validation showed that announcement as correct, because neither inspects the AS path beyond the origin, and Hetzner's ROA permitted the announced prefix length. The one signal the forgery could not erase: no route for that /24 existed a moment before. The BGP checker now remembers the more-specific prefixes it has seen for each check and flags one that is new since the last successful query, even when its origin matches `expected_origin`. The baseline is process-local and starts empty at worker startup, so an already-established deaggregation is never flagged, only a prefix that starts being announced while the worker is running.

### RPKI origin validation in the BGP check [#380](https://github.com/jesseheady/technician/issues/380)

After the origin comparison passes, the BGP check queries RIPE Stat for the RPKI verdict on the prefix and its origin AS. An `invalid_asn` or `invalid_length` verdict fails the check, because no ROA authorizes that announcement. This signal does not depend on `expected_origin`, so it still detects a hijack when that value is stale. A prefix with no ROA returns `unknown`, which passes. A failed RPKI query also returns `unknown`, so it cannot turn a good origin result into a failure. The verdict is exported as `technician_bgp_rpki_valid` (1 valid, 0 invalid, -1 unknown) and alerts through `BGPRPKIInvalid`. No new config field: the check needs only the `prefix` and `expected_origin` it already has.

RPKI ROV validates the origin AS and prefix length against a ROA; it does not validate the rest of the AS path. An attacker who can inject a route and forge the path can make the origin field itself say whatever the resource holder's ROA permits — a "valid" verdict is not proof of a legitimate announcement, only proof the ROA does not forbid it. See `docs/alerting.md` for the incident that demonstrated this and the operational mitigation (RFC 9319 strict ROAs).

### BGP check validates every origin AS [#378](https://github.com/jesseheady/technician/issues/378)

The check read only the first ASN that RIPE Stat returned for a prefix. A hijacker usually announces a prefix while the real operator still announces it, so RIPE Stat returns two origins. When the expected AS came first, the check reported success and `technician_bgp_origin_match` stayed at 1 through the hijack. The check now compares every announced origin with `expected_origin` and names each unexpected AS in the error. `expected_origin` and a valid CIDR `prefix` are now required at config load, because a BGP check without them reports prefix visibility only and cannot detect a hijack.

### SQLite persistence layer [#16](https://github.com/jesseheady/technician/issues/16)

Optional long-term history in an embedded SQLite DB (`modernc.org/sqlite`, pure Go) at `${TECHNICIAN_DATA_DIR}/results.db`, beyond the in-memory ring buffer. Off by default (`persistence.enabled`); `persistence.retention` defaults to 30d. Written **asynchronously write-through** from the status store's hot path — results go to a buffered channel and a single writer goroutine batches the inserts, so a slow disk never slows result draining, and a full buffer drops rather than blocks. Infra errors are skipped (the target was never tested). `status.json` keeps its non-series state. [#16](https://github.com/jesseheady/technician/issues/16) stays open for the remaining half — rendering this history on the status page (30-day uptime bars), which is coupled to the status-page redesign (#25) and the public-exposure decision (#39).

### Prometheus remote-write [#19](https://github.com/jesseheady/technician/issues/19)

`metrics.prometheus.remote_write_url` pushes the `technician_*` metrics to a Prometheus-compatible endpoint (AMP, Grafana Cloud, Mimir, Thanos) on a timer, for Lambda/Workers/locked-down VPCs that Prometheus can't scrape. The remote-write 1.0 protobuf is encoded directly with `google.golang.org/protobuf/encoding/protowire` (no `prometheus/prometheus` client) plus `golang/snappy`; AMP auth is AWS SigV4 (`remote_write_sigv4` + `remote_write_region`), other backends use `remote_write_headers`. Delivery is best-effort — the metrics are gauges, so a failed push is superseded by the next tick rather than queued, which is why there's no WAL.

### OTLP metric export [#33](https://github.com/jesseheady/technician/issues/33)

`metrics.otel.metrics: true` pushes every `technician_*` metric via OTLP, in addition to the `/metrics` endpoint, over the same collector endpoint as traces. Implemented as a registry bridge (`go.opentelemetry.io/contrib/bridges/prometheus`) rather than a second set of instruments, so OTLP stays in parity with Prometheus automatically as metrics are added. A prefix filter keeps the stream to check metrics; `go_*`/`process_*` self-health stays on `/metrics`. Opt-in — traces remain gated on the endpoint alone.

### Sustained infra-error escalation + sensitivity docs [#333](https://github.com/jesseheady/technician/issues/333)

First tranche of the detection-sensitivity review. An infra error freezes `technician_check_healthy` (the target was never tested), so `CheckFailing` can't fire for BGP/domain-expiry/Playwright checks — a prolonged blind spot could hide behind a warning. Added `SustainedInfraError` (`infra_error == 1 for: 15m` → critical), which inhibits the warning tier for the same check. Documented the down-detection sensitivity model (retry defaults off, the `for: 3m` debounce, and the cadence interaction) in `docs/alerting.md` and `docs/deployment-sizing.md`, and the retry rationale in `examples/checks.yml`. Covered by new `promtool test rules` cases. [#333](https://github.com/jesseheady/technician/issues/333) stays open for the remaining decisions (default retry, criticality tiering).

### Browser alert window matches browser cadence [#324](https://github.com/jesseheady/technician/issues/324)

The Web Vitals alerts (`HighLCP`/`HighINP`/`HighCLS` and their `*Critical` variants) averaged over `[15m]` while browser checks run every 5–10 min, so the window held 1–3 samples and a single bad render paged for the length of the lookback. Widened to `[1h]` with `for: 15m` (≥6 samples at a 10-min cadence). Documented that the anti-flap window must span several check intervals, and that the Web Vitals alerts track the absolute Google CWV thresholds — `BudgetViolation` is the budget-relative signal. Covered by new `promtool test rules` cases.

### WebSocket alerting coverage documented [#313](https://github.com/jesseheady/technician/issues/313)

`docs/alerting.md` now lists WebSocket alongside SMTP/Traceroute/gRPC as check-health-only (no shipped threshold alerts), noting its `technician_ws_connect_seconds`/`technician_ws_message_seconds` timing metrics remain available for custom rules, dashboards, or budgets. Rules and docs now agree.

### Managed Playwright mode: browser as a sidecar [#66](https://github.com/jesseheady/technician/issues/66)

`playwright.mode: managed` connects to a Playwright server over `server_url` instead of launching Chromium in the worker. This is Stage 3 in [Playwright scaling](playwright-scaling.md), and it settles how Technician provides a browser: the server is the **stock upstream Playwright image** driven by a command, the same way Prometheus and Grafana are in the base stack, so nothing here forks or patches Playwright.

The decisive argument was getting out of the business of shipping a browser rather than relocating it. Baking Chromium in means owning its build, system dependencies, and patch cadence permanently; a sidecar makes the browser a standard upstream artifact patched on its own cadence, and turns running a different engine into an image-tag change instead of an image rebuild. CVE posture becomes "monitor the sidecar" rather than "our artifact ships known CVEs".

Everything downstream of the browser handle is unchanged — device emulation, network throttling, HAR capture, and Web Vitals all operate on the context and page, not on how the browser started — so managed mode reuses the whole existing probe path. `mode: local` remains the default, `max_browsers` still bounds concurrent sessions, and an invalid mode or a managed mode without a `server_url` is rejected at startup rather than failing once per browser check. Ships with a Compose overlay and an optional Helm sidecar; with the sidecar enabled `shareProcessNamespace` is dropped, since Chromium children belong to the sidecar's PID 1.

The npm client is pinned exactly rather than to a caret range: end-to-end testing caught a shipped handshake failure where `npm ci` resolved a client newer than the sidecar and Playwright rejected the connection with `428 Precondition Required`. Renovate now groups the npm client and the sidecar image so they can never be bumped apart. Slimming the worker image, which no longer needs a browser in managed mode, is tracked in [#213](https://github.com/jesseheady/technician/issues/213).

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

README architecture diagram updated from ASCII art to Mermaid with all check types. Renders natively on GitHub.
