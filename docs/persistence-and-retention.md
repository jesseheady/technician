# Persistence and historical data

How check history and metrics are stored, how that scales, and how to get
long-term history without adding an application database.

For resource and cost sizing, see [deployment sizing](deployment-sizing.md).

## Where data lives today

| Data | Store | Retention | Survives restart? |
|------|-------|-----------|-------------------|
| Real-time check status | In-memory ring buffer (90 results per check), persisted to JSON file every 60s. Daily backups retained 90 days. Snapshot cached 2s. | Depends on check interval: ~45 min at 30s, ~3 hours at 2min, ~7.5 hours at 5min | Yes (with Docker named volume or persistent disk). Falls back to most recent backup if main file is missing or corrupt. |
| Metrics time-series | Prometheus / AMP | Configurable (default 90 days in docker-compose, up to years) | Yes |
| Dashboards, uptime history, trends | Grafana querying Prometheus | As long as Prometheus retains the data | Yes |
| HAR files, screenshots, videos | Local disk or S3 | Configurable (`artifacts.retention`) | Yes (if S3) |
| Alert history | Alertmanager / Grafana | Depends on config | Yes |

## Storage model

All storage is bounded. No container grows without a configured limit or automatic pruning.

**Install footprint (day 0)**

| Container | Docker image | Named volume | Data at first boot |
|-----------|-------------|-------------|-------------------|
| Technician | 1.62 GB (with Playwright) / 80 MB (without) | `technician_data` | Empty `status.json` (~1 KB) |
| Prometheus | ~390 MB | `prometheus_data` | Empty TSDB WAL (~1 MB) |
| Grafana | ~1.0 GB | `grafana_data` | SQLite database (~5 MB) |
| Alertmanager | ~70 MB | `alertmanager_data` | Silence + notification log (~100 KB) |
| **Total** | **~3.1 GB** (with Playwright) | | |

**Steady-state growth (per day, 31 checks, 15s scrape interval)**

| Container | Daily growth | What grows | Pruning mechanism | Steady-state disk |
|-----------|-------------|------------|-------------------|-------------------|
| Technician | ~50 KB/day (backups) | Daily `status.json` backup | Auto-pruned at 90 days | ~4.5 MB backups + ~50 KB live |
| Technician (artifacts) | 0 MB (no video) – 500 MB/day (video on all Playwright checks) | HAR, video, screenshots | `artifacts.retention` (default 72h) | 0 – 1.5 GB |
| Prometheus | ~15–25 MB/day | TSDB blocks (371 series × 15s samples) | TSDB compaction + retention | ~1.4–2.3 GB at 90d retention |
| Grafana | < 1 MB/day | Alert state, session data | Internal SQLite vacuum | < 10 MB |
| Alertmanager | < 1 MB/day | Silence/nflog state | Internal compaction | < 5 MB |

**Maximum retention by data type**

| Data | Default retention | Configurable? | How to change |
|------|------------------|---------------|---------------|
| Check history (ring buffer) | 90 entries per check (~45 min at 30s, ~3h at 2min, ~7.5h at 5min) | No (compile-time) | Change `maxHistory` in `internal/status/store.go` |
| Status backups | 90 days | No (compile-time) | Change retention in `SaveBackup()` in `internal/status/store.go` |
| Playwright artifacts | 72h | Yes | `artifacts.retention` in `technician.yml` |
| Prometheus time-series | 90 days / 5 GB (whichever first) | Yes | `--storage.tsdb.retention.time` and `--storage.tsdb.retention.size` in `docker-compose.yml` Prometheus command |
| Grafana data | Indefinite (< 10 MB) | N/A | N/A |

**Scaling by check count (steady-state at 90-day retention)**

| Checks | Technician volume | Prometheus volume (90d) | Total disk (all containers) |
|--------|------------------|------------------------|-----------------------------|
| 10 | ~4 MB | ~600 MB | ~610 MB |
| 50 | ~18 MB | ~1.8 GB | ~1.8 GB |
| 100 | ~40 MB | ~3 GB | ~3 GB |
| 500 | ~200 MB | ~12 GB | ~12.2 GB |

*Prometheus storage scales linearly with retention — doubling retention doubles disk. Estimates assume ~12 series per check and 15s scrape interval. Technician estimates include 90 days of daily backups. Artifact storage excluded (depends on Playwright video config).*

**Recommended minimum disk by deployment**

| Deployment | Disk | Headroom for |
|------------|------|-------------|
| Dev / local Docker | 5 GB | 90d Prometheus retention, no artifacts |
| Production (no Playwright) | 5–10 GB | 90d Prometheus retention, 90d status backups |
| Production (with Playwright + video) | 10–20 GB | Artifact accumulation at 72h retention |
| 500+ checks | 20–50 GB | 90d Prometheus retention at high cardinality |

## Prometheus storage safeguards

The `docker-compose.yml` includes guards to prevent Prometheus from exhausting disk. These were added after an incident where the shared Docker VM disk filled up (from dangling images and build cache), causing Prometheus WAL writes to fail with "no space left on device" — all queries returned empty and historical data was lost.

| Flag | Value | Purpose |
|------|-------|---------|
| `--storage.tsdb.retention.time` | `90d` | Delete blocks older than 90 days |
| `--storage.tsdb.retention.size` | `5GB` | Hard cap on total block size — deletes oldest blocks when exceeded, even if younger than 90d |
| `--storage.tsdb.wal-compression` | (enabled) | Compresses WAL segments (~30–50% smaller), reducing peak disk usage |

When both time and size retention are set, Prometheus deletes data when **either** limit is breached (whichever comes first). Under normal load (31 checks, ~2 GB at 90d), the time limit applies. Under disk pressure, the size limit kicks in and evicts old blocks to stay under 5 GB.

**WAL is not bounded by `retention.size`** — the WAL holds the most recent ~2 hours of uncompacted data. For this workload (371 series at 15s scrape), the WAL stays under 200 MB. WAL compression reduces this further.

**Tuning `retention.size` for larger deployments:**

| Checks | 90d block size (est.) | Recommended `retention.size` |
|--------|----------------------|------------------------------|
| 10–50 | 0.6–1.8 GB | `5GB` (default in compose) |
| 100 | ~3 GB | `8GB` |
| 500 | ~12 GB | `25GB` |

**Alert rules:** `PrometheusStorageHigh` fires at 80% of the size cap. `PrometheusWALCorruptions` fires immediately on WAL corruption. Both require the `prometheus` self-scrape job in `prometheus.yml`.

**Docker disk management:** The Prometheus-level guards protect against TSDB growth, but the original incident was caused by Docker VM disk exhaustion from dangling images and build cache. To prevent recurrence: run `docker system prune --all --force` periodically, set Docker Desktop disk size above 20 GB, and add `docker builder prune --force` as a post-build step in CI.

## Status store scaling

The in-memory status store is persisted to a single JSON file (`status.json`) every 60 seconds. Daily backups are kept for 90 days. Snapshot results are cached in memory (2s TTL) to avoid recomputing percentiles on every page load.

**File size by check count** (at full 90-entry ring buffer per check):

| Checks | Snapshot size | 90 days of daily backups | Marshal/unmarshal |
|--------|--------------|------------------------|--------------------|
| 10 | ~40 KB | ~3.5 MB | < 1 ms |
| 100 | ~400 KB | ~35 MB | ~2–3 ms |
| 500 | ~2 MB | ~180 MB | ~10–15 ms |
| 1000 | ~4 MB | ~360 MB | ~20–30 ms |
| 5000 | ~20 MB | ~1.8 GB | ~100–150 ms |

**What to watch at scale:**

- **`Snapshot()` computation** — Sorts each check's entries for percentile calculation and averages timing data. At 1000 checks × 90 entries = 90K entries per snapshot. The 2s cache absorbs rapid page refreshes and the 10s auto-refresh, but a single computation still takes O(checks × entries). This is the first bottleneck.
- **`Save()` lock hold** — Copies all data under `RLock` before marshaling. At 1000+ checks the copy phase grows, adding latency to `Push()` calls contending for the write lock.
- **Full-file rewrite** — Every 60s save rewrites the entire file. At 20 MB+ this creates unnecessary I/O.

**Inflection points:**

| Scale | Status | Action |
|-------|--------|--------|
| < 100 checks | No concerns | JSON store is the right choice |
| 100–500 checks | Monitor marshal time in logs | Consider pre-computing percentiles on `Push()` instead of on `Snapshot()` |
| 500–1000 checks | Marshal + snapshot cost becomes measurable | Move to SQLite (see below) for append-only writes, indexed queries, no full-file rewrite |
| 1000+ checks | JSON is the wrong tool | SQLite or Prometheus API for historical data |

All backup storage fits comfortably on a minimal EBS volume at any realistic check count. S3 would only be relevant for cross-region archival or decoupling storage from the worker instance.

## Prometheus metrics cardinality

Each unique check name creates a set of Prometheus time-series (one per metric × origin label combination). At scale, this can cause cardinality explosion — degrading Prometheus query performance and memory usage.

Technician enforces a default limit of **500 unique check names** for metrics recording, set by `metrics.prometheus.max_check_cardinality` in `technician.yml`. When the limit is reached, new check names are dropped from `/metrics` and a warning is logged once.

```yaml
metrics:
  prometheus:
    max_check_cardinality: 2000
```

**What the limit affects and doesn't affect:**

| Feature | Affected by limit? |
|---------|-------------------|
| Check execution (scheduling, retries) | No — all checks run normally |
| Status page and `/api/status` | No — all results appear |
| Native webhook alerts | No — fire on all check state changes |
| Prometheus `/metrics` endpoint | **Yes** — new names beyond the limit are not recorded |
| Grafana dashboards and Prometheus alert rules | **Yes** — only checks with metrics will appear |

**Scaling strategies if you approach the limit:**

1. **Consolidate check names** — If many checks target variants of the same endpoint (e.g. per-customer health checks), group them under fewer names using labels or a shared name prefix. The status page still shows individual results.
2. **Use recording rules** — Pre-aggregate high-cardinality series in Prometheus with recording rules, then drop the raw series. This reduces storage cost without losing visibility.
3. **Increase the limit** — Raise `metrics.prometheus.max_check_cardinality`; no rebuild required. A reasonable ceiling depends on your Prometheus sizing — each check name creates up to ~33 series (across all metric types and origin labels), so 1000 names ≈ 33K series, well within a modestly sized Prometheus.
4. **Shard by worker** — Run multiple Technician workers, each responsible for a subset of checks. Each worker has its own cardinality counter, effectively multiplying the limit.
5. **Use metric relabeling** — Configure Prometheus `metric_relabel_configs` to drop series you don't need (e.g. HAR resource breakdowns for non-browser checks), freeing cardinality budget for more check names.

**Recommended limits by Prometheus sizing:**

| Prometheus RAM | Active series budget | Suggested max_check_cardinality |
|---------------|---------------------|---------------------------------|
| 1 GB | ~100K series | 500 (default) |
| 2–4 GB | ~500K series | 1000–2000 |
| 8+ GB / managed (AMP) | 1M+ series | 5000+ |

For most deployments (< 100 checks), the limit is never reached. If you're operating at 500+ checks with Prometheus metrics needed for all of them, raise `max_check_cardinality` to match your Prometheus sizing.

## Getting 30-day uptime without an application database

Prometheus **is** the time-series database. When Grafana (or the status page) needs "30-day uptime for check X", it queries:

```promql
avg_over_time(technician_check_healthy{name="example.com"}[30d])
```

This returns 0–1 (e.g. 0.997 = 99.7% uptime). Prometheus handles storage, retention, and aggregation natively. For AMP, retention is 150 days by default (no configuration needed).

## Enriching the status page with historical data

The built-in status page at `:9590/` currently shows only what's in the in-memory ring buffer (90 entries per check — the visible window depends on check interval: ~45 min at 30s, ~3 hours at 2min, ~7.5 hours at 5min). To show 30-day uptime bars like the Grafana dashboards, two approaches:

**Approach A: Query Prometheus API from the status page (deferred)**

Add a `metrics.prometheus.url` config field. The status page handler queries the Prometheus HTTP API for historical uptime and response time aggregates, then renders them server-side. No new database — Prometheus (or AMP) already has the data.

This keeps Technician stateless and avoids introducing a persistence layer. The tradeoff is that the status page requires a reachable Prometheus to show history — but if Prometheus is down, you have bigger problems.

**Approach B: Embedded SQLite for local persistence** — the persistence layer ships today ([#16](https://github.com/jesseheady/technician/issues/16)); the status-page rendering of that history is a follow-up.

Enable it with `persistence.enabled: true` (off by default); results are written to `${TECHNICIAN_DATA_DIR}/results.db`. For environments where the status page should work independently of Prometheus (standalone VPS, edge deploys, or a fully self-contained worker):

- Uses [`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) — a pure-Go SQLite implementation. No CGO, compiles into the same static binary.
- One table (`probe_results`): timestamp, name, type, success, degraded, duration, and status code, indexed on `(name, ts, success)`.
- Writes are **asynchronous write-through** from the status store's hot path — results go to a buffered channel and a single writer goroutine batches the inserts, so a slow disk never adds latency to result draining; a full buffer drops rather than blocks. Infra errors are not recorded (the target was never tested).
- Rows older than `persistence.retention` (default 30d) are pruned by a periodic sweep.
- At 30s check intervals, 10 checks over 30 days = ~864,000 rows. SQLite handles this trivially — the file stays under 100 MB.

SQLite adds ~4 MB to the binary and negligible runtime overhead. It doesn't replace Prometheus for metrics and alerting — it gives the status page its own local history so it can (once the rendering lands) show 30-day uptime bars without querying an external service.

**What you don't need:**

- **RDS / managed Postgres** — Overkill. Technician stores check results, not relational data. There are no joins, no transactions, no multi-writer contention. SQLite is the right tool if you need local persistence.
- **DynamoDB / Redis** — Same story. The data model is a simple append-only log with TTL pruning. A local file is simpler and faster than a network round-trip to a managed store.
- **S3 for check results** — S3 is already used for artifacts (HAR files, screenshots). Check result metadata belongs in a queryable store (Prometheus or SQLite), not object storage.

## Recommended path by deployment

| Deploy | Historical data source | Why |
|--------|----------------------|-----|
| **Full stack (Prom + Grafana)** | SQLite (Approach B) for the page's bounded window; Grafana for the long tail | The page needs history of its own even when Prometheus has the data, and it keeps working when Prometheus is unreachable. |
| **AMP + AMG** | Same as above | AMP exposes the standard Prometheus HTTP API, so the long tail is a Grafana query. |
| **Standalone VPS worker** | SQLite (Approach B) | Worker is self-contained. Status page works without external dependencies. |
| **Lambda / edge** | N/A | No durable disk and no long-lived status page. Metrics pushed to Prometheus/AMP; Grafana handles history. |

Approach B is the decided path for the status page in every deployment that has a disk (see [roadmap](roadmap.md#status-page-historical-data) and [#16](https://github.com/jesseheady/technician/issues/16)). Approach A is deferred to whenever the long tail needs to be rendered by the page itself rather than by Grafana.

## Scaling the status page

The built-in status page shows real-time data from this worker's ring buffer. For historical views (30-day uptime bars, multi-site matrix, incident history), use Grafana. The included dashboards provide:

- **Uptime overview** — check status matrix, uptime percentage, degraded state tracking
- **HTTP timing** — DNS, TLS, connect, TTFB breakdown over time
- **Infrastructure checks** — TCP connect/TLS, DNS query time, ICMP packet loss/RTT, gRPC health, NTP offset/stratum/RTT, TLS certificate expiry/validity, BGP prefix visibility/origin match, domain expiry countdown
- **Web Performance Vitals** — LCP, INP, CLS trends
- **HAR analysis** — resource breakdown by type
- **Budget violations** — threshold tracking over time

For a public-facing status page with extended history, use Grafana's anonymous viewer role, or have the status page query Prometheus/AMP directly (Approach A).

