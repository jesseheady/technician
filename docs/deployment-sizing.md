# Deployment sizing

Resource requirements, deployment topology, and scaling considerations.

## Context

Technician is a static Go binary (14 MB, stripped) with no database, no runtime interpreter, and no background job system. Probes run as goroutines inside a single process. The main variable is whether you include **Playwright browser probes**, which require Node.js and Chromium.

## Measured resource usage

Numbers below were measured with 30 probes active across all 13 probe types (7 HTTP, 3 TCP, 1 UDP, 4 DNS, 3 ICMP, 3 NTP, 1 TLS, 1 SMTP, 4 traceroute, 1 BGP, 1 domain expiry, 3 Playwright) on a Docker Compose stack, exporting 117 Prometheus metric lines.

### Runtime memory

| Component | RSS | Notes |
|-----------|-----|-------|
| Technician (Go process) | ~18 MB | 31 probes (13 types), status store, Prometheus registry |
| Prometheus | ~150 MB | 15-day retention, scraping one target |
| Grafana | ~304 MB | 6 provisioned dashboards, anonymous viewer |
| **Full stack total** | **~472 MB** | |

### Image / disk sizes

| Component | Size | Breakdown |
|-----------|------|-----------|
| Go binary | 15 MB | `CGO_ENABLED=0`, `-ldflags="-s -w"` |
| Docker image (with Playwright) | 1.62 GB | Chromium 602 MB + headless shell 323 MB + Node.js base + system deps |
| Docker image (without Playwright) | ~80 MB | Alpine or distroless base + Go binary + mtr + ca-certificates |
| Prometheus image | 303 MB | |
| Grafana image | 693 MB | |

### Playwright overhead

Each Playwright probe launches a separate Chromium instance via Node.js:

| Metric | Value |
|--------|-------|
| Node.js baseline RSS | ~40 MB |
| Chromium per instance | ~150–300 MB |
| HAR + video artifacts | 1–10 MB per run (disk) |
| Cold start (first probe) | ~2–3 s |
| Warm probe execution | ~3–8 s depending on page complexity |

Chromium instances are not pooled — each probe invocation launches and closes a browser. The `max_browsers` setting (default 2) caps concurrent instances via a semaphore. Probes queue for a slot and fail with an infra error if their timeout expires while waiting. See [Playwright scaling](playwright-scaling.md) for detailed resource projections and dedicated runner architecture.

### TLS probe overhead

The TLS probe is minimal: a single outbound TCP connection + TLS handshake per check, with no subprocess or external dependency. Memory overhead is negligible (~1 KB per probe run). Resource usage is comparable to a TCP probe with TLS enabled.

## Deployment topology

Technician is designed as a set of independent components. You can run everything on one box or spread across multiple hosts. Here's the full picture:

```
┌─────────────────────────────────────────────────────────────┐
│                     Your infrastructure                      │
│                                                              │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │  Technician   │  │  Technician   │  │  Technician   │     │
│  │  worker       │  │  worker       │  │  worker       │     │
│  │  us-east-1    │  │  us-west-2    │  │  eu-west-1    │     │
│  │  :9590        │  │  :9590        │  │  :9590        │     │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘      │
│         │                  │                  │               │
│         │    ┌─────────────┴─────────────┐   │               │
│         └───►│     Prometheus (central)   │◄──┘               │
│              │     scrapes all workers    │                   │
│              │     :9090                  │                   │
│              └─────────────┬─────────────┘                   │
│                            │                                 │
│              ┌─────────────▼─────────────┐                   │
│              │     Grafana (central)      │                   │
│              │     dashboards + alerts    │                   │
│              │     :3000                  │                   │
│              └───────────────────────────┘                   │
│                                                              │
│  Optional:                                                   │
│  ┌──────────────┐  ┌──────────────┐                         │
│  │  Pushgateway  │  │  Alertmanager │                        │
│  │  for edge     │  │  notifications│                        │
│  │  :9091        │  │  :9093        │                        │
│  └──────────────┘  └──────────────┘                         │
│                                                              │
│  Edge (no long-lived process):                               │
│  ┌──────────────┐  ┌──────────────┐                         │
│  │  Lambda       │  │  CF Worker    │──push──► Pushgateway   │
│  │  scheduled    │  │  HTTP probes  │                        │
│  └──────────────┘  └──────────────┘                         │
└─────────────────────────────────────────────────────────────┘
```

### Component roles

| Component | Role | Required? | Runs where |
|-----------|------|-----------|------------|
| **Technician worker** | Runs probes on a schedule, exposes `/metrics` and status page | Yes (1+ instances) | VPS, EC2, ECS, Kubernetes |
| **Prometheus** | Scrapes workers, stores time-series, evaluates alert rules | Yes (1 instance) | Central host or managed service |
| **Grafana** | Dashboards, historical views, alert notifications | Recommended (1 instance) | Central host or Grafana Cloud |
| **Alertmanager** | Routes alerts to Slack, PagerDuty, email, etc. | Optional | Alongside Prometheus |
| **Pushgateway** | Receives metrics from edge/serverless probes | Only for edge deploys | Central, reachable from edge |

### What should run together vs. separately

**Single-box deploy (development, small-scale)**

All components on one host. This is the `docker compose up` default:

- Technician worker + Prometheus + Grafana on one machine
- Fine for monitoring a handful of sites from one vantage point
- The status page at `:9590/` gives real-time view; Grafana at `:3000` gives historical

**Production deploy (multi-region)**

Separate concerns for reliability and geographic coverage:

- **Workers**: One per region/vantage point. Each runs independently with its own `SITE_CODE`. If a worker goes down, other regions keep probing. Workers are stateless — no data loss on restart (metrics are scraped by Prometheus before they're lost).
- **Prometheus + Grafana**: Central, on a single host or managed service. Should not run on the same box as a worker — if the worker's host dies, you lose your monitoring history.
- **Alertmanager**: Alongside Prometheus. Fires notifications when `ProbeFailing` or `BudgetViolation` rules trigger.

**Why workers should be separate from the central stack:**

1. **Independence** — A worker failure shouldn't take down dashboards. A Grafana restart shouldn't stop probing.
2. **Geography** — Workers need to be in the regions you're probing from. Prometheus/Grafana only need to be reachable.
3. **Resource isolation** — Workers are tiny (~18 MB). Grafana is heavy (300 MB). Different scaling profiles.

## Build artifacts and deploy flow

One repo produces multiple deployment targets. Here's what ships where and how:

```
                        ┌─────────────────────┐
                        │   technician repo    │
                        └──────────┬──────────┘
                                   │
               ┌───────────────────┼───────────────────┐
               │                   │                   │
               ▼                   ▼                   ▼
    ┌──────────────────┐ ┌──────────────────┐ ┌──────────────────┐
    │  Go binary        │ │  Docker image     │ │  JS Worker       │
    │  `go build`       │ │  `docker build`   │ │  `wrangler`      │
    │                   │ │                   │ │                   │
    │  14 MB, static    │ │  1.6 GB (full)    │ │  < 1 MB bundle   │
    │  No dependencies  │ │  80 MB  (slim)    │ │  HTTP probes only │
    └────────┬─────────┘ └────────┬─────────┘ └────────┬─────────┘
             │                    │                     │
     ┌───────┴───────┐   ┌───────┴───────┐            │
     │               │   │               │            │
     ▼               ▼   ▼               ▼            ▼
   VPS/EC2        Lambda  ECS/K8s     Lambda      CF Workers
  (systemd)    (container) (task)  (container)    (edge PoPs)
```

### What each target runs

| Target | Binary / image | Subcommand | Metrics flow | Status page |
|--------|---------------|------------|--------------|-------------|
| **VPS / EC2** | Go binary or Docker image | `technician worker` | Prometheus scrapes `:9590/metrics` | Built-in at `:9590/` |
| **ECS / Kubernetes** | Docker image | `technician worker` | Prometheus scrapes via service discovery | Built-in at `:9590/` |
| **Lambda (scheduled)** | Go binary (zip) or Docker image | `technician validate` or Lambda handler (planned) | Push to Pushgateway or remote-write | None (headless) |
| **Cloudflare Worker** | JS bundle (planned) | N/A (JS runtime) | Push to Pushgateway | None (headless) |

### How to deploy each target

**VPS (any provider)**

The simplest path. A single static binary, no Docker required.

```bash
# Build
CGO_ENABLED=0 go build -ldflags="-s -w" -o technician .

# Copy to server
scp technician yourserver:/usr/local/bin/
scp -r config/ yourserver:/etc/technician/

# Create data directory for status persistence
ssh yourserver 'mkdir -p /var/lib/technician'

# Run (or use systemd — see below)
ssh yourserver 'SITE_CODE=us-east-1 technician worker --config /etc/technician/technician.yml'
```

**What you get with just the worker (no Prometheus/Grafana):**

| Feature | Available? |
|---------|-----------|
| Status page at `:9590/` | Yes — real-time probe status, history bars, latency percentiles |
| JSON API at `/api/status` | Yes — for external integrations or a custom status page |
| Prometheus metrics at `/metrics` | Yes — ready to scrape whenever you add Prometheus |
| Native webhook alerts (Discord, Slack, generic) | Yes — fires on probe state transitions, cert expiry, and budget violations with severity routing (warn/crit to different channels) |
| Blackbox-exporter compat at `/probe` | Yes |
| Grafana dashboards | No — requires Prometheus + Grafana |
| Historical data beyond ~45 min | No — the in-memory ring buffer holds 90 entries per probe |
| Alert rules (ProbeFailing, BudgetViolation) | No — requires Prometheus |

This is a valid production deployment for teams that just need uptime monitoring with webhook alerts. The status page and `/api/status` endpoint work independently. Add Prometheus + Grafana later when you want historical trends and dashboards — the worker is already exporting metrics.

**systemd unit file:**

```ini
[Unit]
Description=Technician probe runner
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/technician worker --config /etc/technician/technician.yml
Environment=SITE_CODE=us-east-1
Restart=always
RestartSec=5
WorkingDirectory=/var/lib/technician

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/technician
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

```bash
# Install and start
sudo cp technician.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now technician
sudo journalctl -u technician -f  # tail logs
```

For Playwright probes, you'd also need Node.js and Chromium on the host — in that case, Docker is easier. For traceroute probes, install mtr (`apt install mtr-tiny`) and note that mtr requires root for raw sockets (the systemd unit runs as root by default; add `AmbientCapabilities=CAP_NET_RAW` if you add a `User=` directive). For NTP probes, ensure outbound UDP port 123 is open.

**Docker / ECS / Kubernetes**

```bash
# Build image
docker build -t technician .

# Push to registry
docker tag technician your-registry/technician:latest
docker push your-registry/technician:latest

# Deploy per region (each with its own SITE_CODE)
# ECS: one service per region, env SITE_CODE=us-east-1
# K8s: one Deployment per region, env SITE_CODE=eu-west-1
```

Each instance runs `technician worker` (the Dockerfile default). Prometheus discovers them via DNS, ECS service discovery, or Kubernetes service endpoints.

**Lambda (planned)**

The Go binary runs in Lambda as a container image or zip deploy. Two invocation models:

1. **EventBridge schedule** triggers Lambda every N minutes. Lambda runs all probes once (`technician validate`), pushes results to Pushgateway, exits.
2. **Per-probe invocation** — EventBridge triggers a separate Lambda per probe. More granular, better cold-start isolation.

Lambda can't be scraped (no long-lived process), so metrics must be **pushed**:

```
EventBridge (cron) → Lambda → run probes → push to Pushgateway → Prometheus scrapes Pushgateway
```

See [Central Prometheus and Grafana](architecture/central-prometheus-grafana.md) for push configuration.

**Cloudflare Workers (planned)**

A separate JS/TS implementation under `workers/cf/` that mirrors the `/probe?target=&module=` contract. Deployed via Wrangler:

```
Cron Trigger → Worker → HTTP probe → push result to Pushgateway
```

Workers run at Cloudflare's edge PoPs — you get geographic diversity without managing servers. The Worker is not the Go binary; it's a lightweight JS probe that speaks the same metrics format.

See [Cloudflare Workers proposal](proposals/cloudflare-workers.md) for the full design.

### Where the control center lives

The **central dashboard** is separate from all probe workers:

| Piece | What it does | Where it runs |
|-------|-------------|---------------|
| **Prometheus** | Scrapes all VPS/ECS workers. Scrapes Pushgateway for Lambda/edge results. Stores all time-series. Evaluates alert rules. | One central host — a VPS, EC2 instance, or managed Prometheus service. |
| **Grafana** | Queries Prometheus. Renders dashboards (uptime, timing, vitals, budgets). Sends alert notifications. This is the "control center." | Same host as Prometheus, or Grafana Cloud (free tier available). |
| **Pushgateway** | Receives metric pushes from Lambda and Cloudflare Workers. Prometheus scrapes it like any other target. | Same host as Prometheus (port 9091). |
| **Alertmanager** | Routes alerts from Prometheus rules to Slack, PagerDuty, email, webhooks. | Same host as Prometheus (port 9093). |

The built-in status page at each worker's `:9590/` is a **per-worker view** — it shows what that one worker sees in real time. Grafana is the aggregated view across all workers and regions. For a public-facing status page, use Grafana's anonymous viewer role or a static page that queries the Prometheus API.

### Putting it all together

A complete multi-region deployment from this repo:

```
┌─ Your repo ──────────────────────────────────────────────┐
│                                                           │
│  go build ──► binary ──► scp to VPS (us-east-1)          │
│                     └──► scp to VPS (eu-west-1)           │
│                                                           │
│  docker build ──► image ──► ECR ──► ECS (us-west-2)      │
│                         └──► Lambda (ap-southeast-1)      │
│                                                           │
│  wrangler deploy ──► CF Worker (global edge PoPs)         │
│                                                           │
│  docker compose up ──► Prometheus + Grafana (central VPS) │
│                                                           │
│  prometheus/ ──► rules.yml, scrape config (central)       │
│  dashboards/ ──► Grafana provisioned dashboards           │
└───────────────────────────────────────────────────────────┘
```

All probe results — from VPS workers, ECS tasks, Lambda functions, and Cloudflare Workers — end up in the same central Prometheus. Grafana queries that one Prometheus and shows everything on the same dashboards, filterable by `region` and probe type.

## Using managed services

Technician is designed to work with self-hosted or managed infrastructure. Here's what's configurable today and what needs work.

### What's configurable now

| Dependency | Config field | Options |
|------------|-------------|---------|
| **OTLP tracing** | `metrics.otel.endpoint` | Any OTLP-compatible collector — AWS X-Ray (via ADOT Collector), Datadog, Honeycomb, Jaeger, etc. Leave empty to disable. |
| **Artifact storage** | `artifacts.driver` | `local` (disk), `s3` (any S3-compatible store — AWS S3, MinIO, R2), or `none`. |
| **Prometheus scrape** | `metrics.prometheus.listen` | Technician exposes `/metrics` on this address. Any Prometheus-compatible scraper can collect from it. |

### AWS Managed Prometheus (AMP)

AMP uses remote-write ingestion, not pull-based scraping. Three ways to get metrics from Technician into AMP:

**Option A: AMP managed scraper (simplest for ECS/EC2)**

AMP can scrape ECS and EC2 targets natively via its managed scraper feature. Configure a scrape config in AMP that discovers Technician services by tag or service discovery. No sidecar, no code changes — Technician just exposes `/metrics` as usual.

**Option B: Sidecar agent (works anywhere)**

Run Grafana Alloy, Prometheus in agent mode, or AWS Distro for OpenTelemetry (ADOT) alongside each Technician worker. The agent scrapes local `:9590/metrics` and remote-writes to AMP.

Example with Grafana Alloy:

```hcl
prometheus.scrape "technician" {
  targets    = [{"__address__" = "localhost:9590"}]
  forward_to = [prometheus.remote_write.amp.receiver]
}

prometheus.remote_write "amp" {
  endpoint {
    url = "https://aps-workspaces.us-east-1.amazonaws.com/workspaces/ws-xxx/api/v1/remote_write"
    sigv4 { region = "us-east-1" }
  }
}
```

**Option C: Built-in remote-write (planned)**

A future enhancement would add native Prometheus remote-write to Technician itself, configured via `metrics.prometheus.remote_write_url` in `technician.yml`. This eliminates the sidecar for push-based environments (Lambda, edge, or when AMP managed scraper isn't available).

### AWS Managed Grafana (AMG)

No code changes needed. Configure AMG with a Prometheus datasource pointing at your AMP workspace, then import the dashboard JSONs from `dashboards/`. The dashboards use standard PromQL and template variables (`region`, `probe`, and for browser dashboards `network` and `device`) that work identically with AMP.

### Alerting

Technician supports two complementary alerting layers:

**Central alerting** — alerts evaluated by Prometheus or Grafana against scraped metrics. Supports grouping, silencing, escalation, and dashboards.

| Approach | How |
|----------|-----|
| **Prometheus alert rules** | Deploy `prometheus/rules.yml` to AMP or self-hosted Prometheus. Rules like `ProbeFailing` and `BudgetViolation` fire as native Prometheus alerts. |
| **Grafana alerting** | Define alert rules in AMG/Grafana that query AMP. Route to SNS, PagerDuty, Slack, etc. via Grafana contact points. |
| **Alertmanager** | Self-hosted Alertmanager receives alerts from Prometheus rules. Routes to Slack, PagerDuty, email, webhooks. |

**Native webhooks** — webhooks fired directly from the Technician worker on probe state changes (up→down, down→up) and budget violations. No dependency on the central stack being reachable.

| | Central (Prometheus/Grafana) | Native (Technician worker) |
|--|------------------------------|--------------------------|
| **Latency** | 30–60s (scrape interval + rule eval) | Immediate (fires in the probe goroutine) |
| **Deduplication** | Alertmanager groups, deduplicates, silences | Fire on state change only, with per-probe cooldown |
| **Dependency** | Requires Prometheus + Alertmanager/Grafana to be up | Zero — just an outbound HTTP POST |
| **Escalation** | Routes by severity, time-of-day, team | Not built-in (use central for complex routing) |
| **Works on Lambda/edge** | Only if metrics reach Prometheus in time | Yes — fires before the function exits |
| **Best for** | Alert management with grouping, silencing, escalation, history | "Probe down" notifications that fire regardless of central stack health |

Native webhooks are configured in `technician.yml` under `webhooks` and support Discord, Slack, and generic HTTP endpoints. See [alerting.md](alerting.md) for configuration details and comparison of all three alerting strategies.

### Dependency map

Every external dependency Technician touches, and whether it can be swapped:

| Dependency | Bundled (docker compose) | Managed alternative | Config needed |
|------------|-------------------------|--------------------|----|
| Prometheus | `prom/prometheus` container | AMP, Grafana Cloud, Thanos, Mimir | Scrape config or remote-write agent |
| Grafana | `grafana/grafana` container | AMG, Grafana Cloud (free tier) | Datasource + dashboard import |
| Alertmanager | `prom/alertmanager` container | AMG alerting, SNS, PagerDuty via Grafana | `prometheus/alertmanager.yml` receivers; or contact point config in Grafana |
| Artifact storage | Local disk (`/tmp/technician/artifacts`) | S3, R2, MinIO | `artifacts.driver: s3` + bucket/region |
| OTLP tracing | None (disabled by default) | X-Ray (ADOT), Datadog, Honeycomb | `metrics.otel.endpoint` |
| DNS/network | Host resolver | Route 53, Cloudflare DNS | N/A (uses OS resolver) |

## Persistence and historical data

### Where data lives today

| Data | Store | Retention | Survives restart? |
|------|-------|-----------|-------------------|
| Real-time probe status | In-memory ring buffer (90 results per probe), persisted to JSON file every 60s. Daily backups retained 90 days. Snapshot cached 2s. | ~45 min at 30s intervals | Yes (with Docker named volume or persistent disk). Falls back to most recent backup if main file is missing or corrupt. |
| Metrics time-series | Prometheus / AMP | Configurable (default 15 days, up to years) | Yes |
| Dashboards, uptime history, trends | Grafana querying Prometheus | As long as Prometheus retains the data | Yes |
| HAR files, screenshots, videos | Local disk or S3 | Configurable (`artifacts.retention`) | Yes (if S3) |
| Alert history | Alertmanager / Grafana | Depends on config | Yes |

### Status store scaling

The in-memory status store is persisted to a single JSON file (`status.json`) every 60 seconds. Daily backups are kept for 90 days. Snapshot results are cached in memory (2s TTL) to avoid recomputing percentiles on every page load.

**File size by probe count** (at full 90-entry ring buffer per probe):

| Probes | Snapshot size | 90 days of daily backups | Marshal/unmarshal |
|--------|--------------|------------------------|--------------------|
| 10 | ~40 KB | ~3.5 MB | < 1 ms |
| 100 | ~400 KB | ~35 MB | ~2–3 ms |
| 500 | ~2 MB | ~180 MB | ~10–15 ms |
| 1000 | ~4 MB | ~360 MB | ~20–30 ms |
| 5000 | ~20 MB | ~1.8 GB | ~100–150 ms |

**What to watch at scale:**

- **`Snapshot()` computation** — Sorts each probe's entries for percentile calculation and averages timing data. At 1000 probes × 90 entries = 90K entries per snapshot. The 2s cache absorbs rapid page refreshes and the 10s auto-refresh, but a single computation still takes O(probes × entries). This is the first bottleneck.
- **`Save()` lock hold** — Copies all data under `RLock` before marshaling. At 1000+ probes the copy phase grows, adding latency to `Push()` calls contending for the write lock.
- **Full-file rewrite** — Every 60s save rewrites the entire file. At 20 MB+ this creates unnecessary I/O.

**Inflection points:**

| Scale | Status | Action |
|-------|--------|--------|
| < 100 probes | No concerns | JSON store is the right choice |
| 100–500 probes | Monitor marshal time in logs | Consider pre-computing percentiles on `Push()` instead of on `Snapshot()` |
| 500–1000 probes | Marshal + snapshot cost becomes measurable | Move to SQLite (see below) for append-only writes, indexed queries, no full-file rewrite |
| 1000+ probes | JSON is the wrong tool | SQLite or Prometheus API for historical data |

All backup storage fits comfortably on a minimal EBS volume at any realistic probe count. S3 would only be relevant for cross-region archival or decoupling storage from the worker instance.

### Prometheus metrics cardinality

Each unique probe name creates a set of Prometheus time-series (one per metric × site label combination). At scale, this can cause cardinality explosion — degrading Prometheus query performance and memory usage.

Technician enforces a default limit of **500 unique probe names** for metrics recording. This is controlled by `maxProbeCardinality` in `internal/metrics/prometheus.go`. When the limit is reached, new probe names are silently dropped from `/metrics` and a warning is logged.

**What the limit affects and doesn't affect:**

| Feature | Affected by limit? |
|---------|-------------------|
| Probe execution (scheduling, retries) | No — all probes run normally |
| Status page and `/api/status` | No — all results appear |
| Native webhook alerts | No — fire on all probe state changes |
| Prometheus `/metrics` endpoint | **Yes** — new names beyond the limit are not recorded |
| Grafana dashboards and Prometheus alert rules | **Yes** — only probes with metrics will appear |

**Scaling strategies if you approach the limit:**

1. **Consolidate probe names** — If many probes target variants of the same endpoint (e.g. per-customer health checks), group them under fewer names using labels or a shared name prefix. The status page still shows individual results.
2. **Use recording rules** — Pre-aggregate high-cardinality series in Prometheus with recording rules, then drop the raw series. This reduces storage cost without losing visibility.
3. **Increase the limit** — Change `maxProbeCardinality` in `internal/metrics/prometheus.go`. The constant is a compile-time guard; there's no config file knob for it today. A reasonable ceiling depends on your Prometheus sizing — each probe name creates up to ~33 series (across all metric types and site labels), so 1000 names ≈ 33K series, well within a modestly sized Prometheus.
4. **Shard by worker** — Run multiple Technician workers, each responsible for a subset of probes. Each worker has its own cardinality counter, effectively multiplying the limit.
5. **Use metric relabeling** — Configure Prometheus `metric_relabel_configs` to drop series you don't need (e.g. HAR resource breakdowns for non-browser probes), freeing cardinality budget for more probe names.

**Recommended limits by Prometheus sizing:**

| Prometheus RAM | Active series budget | Suggested maxProbeCardinality |
|---------------|---------------------|-------------------------------|
| 1 GB | ~100K series | 500 (default) |
| 2–4 GB | ~500K series | 1000–2000 |
| 8+ GB / managed (AMP) | 1M+ series | 5000+ |

For most deployments (< 100 probes), the limit is never reached. If you're operating at 500+ probes with Prometheus metrics needed for all of them, consider either bumping the constant or making it a config-file option — a future enhancement tracked in TODO.md.

### Getting 30-day uptime without an application database

Prometheus **is** the time-series database. When Grafana (or the status page) needs "30-day uptime for probe X", it queries:

```promql
avg_over_time(technician_probe_up{name="example.com"}[30d])
```

This returns 0–1 (e.g. 0.997 = 99.7% uptime). Prometheus handles storage, retention, and aggregation natively. For AMP, retention is 150 days by default (no configuration needed).

### Enriching the status page with historical data

The built-in status page at `:9590/` currently shows only what's in the in-memory ring buffer (~45 minutes). To show 30-day uptime bars like the Grafana dashboards, two approaches:

**Approach A: Query Prometheus API from the status page (recommended)**

Add a `metrics.prometheus.url` config field. The status page handler queries the Prometheus HTTP API for historical uptime and response time aggregates, then renders them server-side. No new database — Prometheus (or AMP) already has the data.

This keeps Technician stateless and avoids introducing a persistence layer. The tradeoff is that the status page requires a reachable Prometheus to show history — but if Prometheus is down, you have bigger problems.

**Approach B: Embedded SQLite for local persistence**

For environments where the status page should work independently of Prometheus (standalone VPS, edge deploys, or when you want the worker to be fully self-contained):

- Use [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) — a pure-Go SQLite implementation. No CGO, no external dependencies, compiles to the same static binary.
- Store probe results in a local SQLite file (e.g. `/var/lib/technician/results.db`).
- Schema is simple: one table (`probe_results`) with timestamp, name, type, success, duration, and HTTP timing columns.
- At 30s probe intervals, 10 probes over 30 days = ~864,000 rows. SQLite handles this trivially — the file stays under 100 MB.
- Prune rows older than the configured retention on each insert (or via a periodic goroutine).

SQLite adds ~2 MB to the binary size and negligible runtime overhead. It doesn't replace Prometheus for metrics and alerting — it just gives the status page its own local history so it can render 30-day uptime bars without querying an external service.

**What you don't need:**

- **RDS / managed Postgres** — Overkill. Technician stores probe results, not relational data. There are no joins, no transactions, no multi-writer contention. SQLite is the right tool if you need local persistence.
- **DynamoDB / Redis** — Same story. The data model is a simple append-only log with TTL pruning. A local file is simpler and faster than a network round-trip to a managed store.
- **S3 for probe results** — S3 is already used for artifacts (HAR files, screenshots). Probe result metadata belongs in a queryable store (Prometheus or SQLite), not object storage.

### Recommended path by deployment

| Deploy | Historical data source | Why |
|--------|----------------------|-----|
| **Full stack (Prom + Grafana)** | Prometheus API (Approach A) | Prometheus already has the data. No new dependency. |
| **AMP + AMG** | AMP API (Approach A) | Same as above — AMP exposes the standard Prometheus HTTP API. |
| **Standalone VPS worker** | SQLite (Approach B) | Worker is self-contained. Status page works without external dependencies. |
| **Lambda / edge** | N/A | No long-lived status page. Metrics pushed to Prometheus/AMP; Grafana handles history. |

### Scaling the status page

The built-in status page shows real-time data from this worker's ring buffer. For historical views (30-day uptime bars, multi-site matrix, incident history), use Grafana. The included dashboards provide:

- **Uptime overview** — probe status matrix, uptime percentage, degraded state tracking
- **HTTP timing** — DNS, TLS, connect, TTFB breakdown over time
- **Infrastructure probes** — TCP connect/TLS, DNS query time, ICMP packet loss/RTT, gRPC health, NTP offset/stratum/RTT, TLS certificate expiry/validity, BGP prefix visibility/origin match, domain expiry countdown
- **Web Performance Vitals** — LCP, INP, CLS trends
- **HAR analysis** — resource breakdown by type
- **Budget violations** — threshold tracking over time

For a public-facing status page with extended history, use Grafana's anonymous viewer role, or have the status page query Prometheus/AMP directly (Approach A).

## Sizing by deployment mode

### Probes only, no Playwright

The lightest deployment. Just the Go binary running HTTP, TCP, DNS, ICMP, gRPC, NTP, TLS, SMTP, and/or traceroute probes.

| Resource | Minimum | Notes |
|----------|---------|-------|
| CPU | 1 vCPU (shared OK) | Probes are I/O-bound, not CPU-bound |
| RAM | 128 MB | Go binary + headroom |
| Disk | 50 MB | Binary + config + mtr |

This fits on a $2.50–5/mo VPS from any provider. Also fine as a sidecar container in ECS/Kubernetes.

### Probes with Playwright

Add browser probes for Core Web Vitals (LCP, INP, CLS) and visual testing.

| Resource | Minimum | Recommended |
|----------|---------|-------------|
| CPU | 1 vCPU | 2 vCPU |
| RAM | 512 MB | 1 GB |
| Disk | 2 GB | 3 GB (if saving video artifacts) |

The RAM floor depends on concurrency. One Playwright probe at a time: 512 MB is fine. If you schedule overlapping browser probes or have short intervals, budget 1 GB. Video recording adds disk I/O but minimal RAM.

### Full stack, single box

Everything on one host — good for a single VPS or self-contained monitoring instance.

| Resource | Minimum | Recommended |
|----------|---------|-------------|
| CPU | 1 vCPU | 2 vCPU |
| RAM | 1 GB (no Playwright) | 2 GB (with Playwright) |
| Disk | 5 GB | 10 GB (Prometheus retention + Grafana + artifacts) |

A $4–6/mo VPS (1 vCPU, 1 GB) handles this comfortably without Playwright. Grafana is the heaviest component (~300 MB RSS). If you use hosted Grafana (Grafana Cloud, etc.), drop it from the stack and save ~700 MB of image + ~300 MB of RAM.

### Full spread, multi-region

The "all in" deployment for production monitoring from multiple vantage points:

| Component | Instances | Specs each | Monthly cost estimate |
|-----------|-----------|------------|----------------------|
| Technician worker (no Playwright) | 1 per region | 1 vCPU, 256 MB, 1 GB disk | $2.50–5 |
| Technician worker (with Playwright) | 1 per region | 1 vCPU, 1 GB, 3 GB disk | $5–6 |
| Prometheus + Alertmanager | 1 central | 1 vCPU, 1 GB, 20 GB disk | $5–6 |
| Grafana | 1 central | 1 vCPU, 1 GB, 5 GB disk | $5–6 (or free on Grafana Cloud) |
| Pushgateway (if using edge) | 1 central | 1 vCPU, 256 MB | Collocate with Prometheus |

**Example: 3 regions, with Playwright, self-hosted Grafana**

| | Count | Per unit | Total |
|--|-------|----------|-------|
| Workers | 3 | ~$6/mo | $18/mo |
| Prometheus + Grafana | 1 | ~$10/mo | $10/mo |
| **Total** | | | **~$28/mo** |

**Example: 3 regions, HTTP-only probes, Grafana Cloud free tier**

| | Count | Per unit | Total |
|--|-------|----------|-------|
| Workers | 3 | ~$3/mo | $9/mo |
| Prometheus | 1 | ~$5/mo | $5/mo |
| Grafana Cloud | 1 | free | $0 |
| **Total** | | | **~$14/mo** |

### AWS Lambda

Lambda runs probes on demand (EventBridge schedule → Lambda invocation → push metrics).

| Config | Function memory | Timeout | Image |
|--------|-----------------|---------|-------|
| HTTP/SMTP only | 128 MB | 30 s | Go binary (zip deploy, ~10 MB) |
| With Playwright | 512 MB–1 GB | 60 s | Container image (Lambda container, ~1.6 GB) |

Lambda cold starts: ~100 ms for the Go binary, ~3–5 s with Playwright/Chromium. Use provisioned concurrency if cold start matters.

No `/metrics` endpoint on Lambda — push results to a Pushgateway or remote-write endpoint. See [Central Prometheus and Grafana](architecture/central-prometheus-grafana.md).

### Cloudflare Workers

JS/TS-only HTTP probes (no Go binary, no Playwright). See [Cloudflare Workers proposal](proposals/cloudflare-workers.md).

| Resource | Limit |
|----------|-------|
| CPU time | 10–50 ms per invocation (Workers free/paid) |
| RAM | 128 MB |
| Deploy size | < 1 MB (JS bundle) |

Workers run at the edge with no cold start. Suited for HTTP uptime checks from many PoPs.

## Firewall rules

| Port | Protocol | Direction | Purpose |
|------|----------|-----------|---------|
| 9590 | TCP | Inbound | Technician status page, `/metrics`, `/probe` |
| 9090 | TCP | Inbound | Prometheus UI (if running full stack) |
| 3000 | TCP | Inbound | Grafana UI (if running full stack) |
| 25 | TCP | Outbound | SMTP probes (if used) |
| 123 | UDP | Outbound | NTP probes (if used) |
| 443 | TCP | Outbound | HTTPS probes + Playwright CDN fetches |

SMTP probes need outbound port 25, which most cloud vendors firewall by default. You'll need to submit a support request to have it opened before SMTP checks will work.
