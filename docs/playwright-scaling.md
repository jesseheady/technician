# Playwright scaling

Resource analysis, concurrency controls, and architecture for scaling browser probes.

## Current resource profile

Numbers measured with 3 Playwright probes (desktop, mobile 4G, mobile 3G) at 5-minute intervals on a Docker Compose stack.

### Per-instance costs

| Resource | Value |
|----------|-------|
| Node.js baseline RSS | ~40 MB |
| Chromium per instance | 150-300 MB (depends on page complexity) |
| HAR per run | 1-10 MB (disk) |
| Video per run | 1-10 MB (disk, when enabled) |
| Cold start (first probe) | ~2-3 s |
| Warm probe execution | 3-8 s |
| Web Vitals collection | up to 3 s per metric |

### Docker image impact

| | With Playwright | Without Playwright |
|--|-----------------|-------------------|
| Docker image size | 1.62 GB | ~80 MB |
| Chromium | 602 MB | - |
| Headless shell | 323 MB | - |
| Node.js + system deps | ~700 MB | - |
| Go binary | 15 MB | 15 MB |

The image size is fixed regardless of probe count -- Chromium is installed once.

## Scaling by probe count

Each Playwright probe invocation launches a fresh Chromium instance. There is no browser pooling -- `run.js` calls `chromium.launch()` every time, and the Go process spawns a new `node` subprocess per run.

The `max_browsers` setting in `technician.yml` caps concurrent Chromium instances using a channel-based semaphore. Additional probes queue until a slot opens or their timeout expires.

```yaml
playwright:
  mode: local
  max_browsers: 2  # default; increase for dedicated runner hosts
```

### Projected resource usage

| Scenario | Peak concurrent browsers | RAM ceiling | Disk (artifacts/day) |
|----------|-------------------------|-------------|---------------------|
| 3 probes @ 5 min (current) | 1 | ~350 MB | ~2 GB |
| 10 probes @ 5 min | 1-2 | ~600 MB | ~7 GB |
| 10 probes @ 1 min | 2-4 | ~1.2 GB | ~35 GB |
| 20 probes @ 1 min | 4-8 | ~2.4 GB | ~70 GB |
| CI validate (all at once) | N simultaneous | ~300 MB x N | negligible |

### The three cost drivers

**1. RAM -- concurrent Chromium instances.** Each browser is 150-300 MB depending on page complexity. The `max_browsers` semaphore prevents unbounded stacking. If a probe can't acquire a slot within its timeout, it fails with an infra error rather than OOM-killing the host.

**2. Disk -- HAR + video accumulation.** With `video: true` and full HAR recording, each run produces 2-20 MB of artifacts. At 1-minute intervals across 10 probes that's 20-200 MB/hour. The `artifacts.retention` setting (default 72h) controls cleanup. Without video, roughly half.

**3. CPU -- Chromium rendering.** Each instance uses 1-2 CPU cores during page load. 4+ concurrent browsers will saturate a 2-vCPU host. Complex flows with interactions, screenshots, and Web Vitals collection extend the CPU-busy window.

### What doesn't change

- **Docker image size** stays ~1.62 GB regardless of probe count
- **Go process RSS** stays ~18 MB -- it just shells out to Node
- **Network** is negligible unless testing bandwidth-heavy pages with video recording

## Concurrency control

The `max_browsers` config field controls how many Chromium instances can run simultaneously across all Playwright probes on a single worker. This is enforced by a channel-based semaphore in `PlaywrightProber`.

When all slots are occupied:
- New probes wait for a slot to open
- If the probe's timeout expires while waiting, it returns an infra error: `timed out waiting for browser slot (N/N in use)`
- The probe is logged as a warning so you can tune `max_browsers` or adjust schedules

### Recommended settings

| Environment | `max_browsers` | Why |
|-------------|---------------|-----|
| VPS, 1 vCPU, 1 GB | 1 | Serializes all browser probes; prevents OOM |
| VPS, 2 vCPU, 2 GB | 2 (default) | Two concurrent browsers fit comfortably |
| Dedicated runner, 4 vCPU, 4 GB | 4 | One browser per core |
| CI (GitHub Actions, 7 GB) | 3-4 | Balance parallelism with shared runner limits |
| CI (2 GB runner) | 1 | Serialize to avoid OOM |

### Tuning

If you see `timed out waiting for browser slot` in logs:
1. **Increase `max_browsers`** if the host has RAM headroom
2. **Spread schedules** -- stagger cron expressions so probes don't all fire at the same second
3. **Increase probe timeout** -- give more time to wait for a slot
4. **Move browser probes to a dedicated runner** (see below)

## CI usage

### Resource limits by CI platform

| CI runner | RAM | Comfortable `max_browsers` |
|-----------|-----|---------------------------|
| GitHub Actions (standard) | 7 GB, 2 CPU | 3-4 |
| GitHub Actions (large) | 16-64 GB, 4-16 CPU | 6-10 |
| GitLab CI (shared) | 4 GB | 2 |
| CircleCI (medium) | 4 GB | 2 |
| Jenkins (varies) | host-dependent | host-dependent |
| 2 GB runner (any) | 2 GB | 1 |

### GitHub Actions

The `technician validate` command runs all probes once and evaluates performance budgets. Use it in CI to catch regressions:

```yaml
- name: Validate probes + budgets
  run: technician validate --config config/technician.yml --budget config/budgets.yml --output gha
```

The `--output gha` flag emits GitHub Actions annotations for budget violations, which appear inline on the PR diff.

For Playwright probes in CI, either:
- Use the pre-built Docker image (includes Chromium): `container: ghcr.io/your-org/technician:latest`
- Or install Playwright in the job: `npx playwright install --with-deps chromium`

See `.github/workflows/ci.yml` for a complete example.

### Generic CI

For any CI platform, the pattern is the same:

```bash
# Build
go build -o technician .

# Run all probes and check budgets
./technician validate --config config/technician.yml --budget config/budgets.yml --output json

# Exit code: 0 = pass, 1 = violations
```

Output formats:
- `text` -- human-readable summary
- `json` -- machine-parseable results (pipe to `jq` or your reporting tool)
- `gha` -- GitHub Actions annotations

## Architecture: dedicated Playwright runners

As Playwright probe count grows, you'll want to separate browser probes from lightweight probes (HTTP, TCP, DNS, etc.). Here's the progression:

### Stage 1: Single worker (current)

All probes on one host. `max_browsers` prevents OOM.

```
┌─────────────────────────┐
│  Technician worker       │
│  HTTP, TCP, DNS, ...     │
│  Playwright (max: 2)     │
│  :9394                   │
└─────────────────────────┘
```

### Stage 2: Dedicated browser worker

Split into two workers at the same site. One runs lightweight probes, the other runs only Playwright probes with more resources.

```
┌─────────────────────────┐  ┌─────────────────────────┐
│  Worker (lightweight)    │  │  Worker (browser)        │
│  HTTP, TCP, DNS, ICMP,   │  │  Playwright only         │
│  NTP, SMTP, gRPC         │  │  max_browsers: 4         │
│  :9394                   │  │  :9395                   │
│  256 MB, 1 vCPU          │  │  4 GB, 4 vCPU            │
└─────────────────────────┘  └─────────────────────────┘
         │                             │
         └──────────┬──────────────────┘
                    ▼
             Prometheus (scrapes both)
```

This already works today with separate config directories:

```bash
# Lightweight worker (no Playwright probes in its config)
technician worker --config /etc/technician/light/technician.yml --site us-east-1

# Browser worker (only Playwright probes in its config)
technician worker --config /etc/technician/browser/technician.yml --site us-east-1
```

Prometheus scrapes both on different ports. Grafana sees all metrics with the same `site_code` label.

### Stage 3: Remote browser service (future)

For teams running many browser probes across regions, a dedicated Playwright server that workers connect to over the network:

```
┌──────────────┐       ┌──────────────────────┐
│  Worker       │──────►│  Playwright server     │
│  us-east-1    │  gRPC │  (pool of browsers)    │
│  :9394        │       │  max_browsers: 10       │
└──────────────┘       │  4 vCPU, 8 GB           │
                       └──────────────────────┘
┌──────────────┐
│  Worker       │──────► same server or regional
│  eu-west-1    │
└──────────────┘
```

The config already has `playwright.mode` and `playwright.server_url` fields for this:

```yaml
playwright:
  mode: managed         # "local" (default) or "managed"
  server_url: "http://playwright-server:3000"
  max_browsers: 10
```

In `managed` mode, the worker sends probe configs to the remote server instead of launching local Chromium instances. The server maintains a browser pool and handles concurrency internally. This decouples browser resource usage from the probe worker entirely.

**When to move to managed mode:**
- 10+ Playwright probes per region
- Multiple workers sharing the same browser pool
- Browser probes are the bottleneck and you want to scale them independently
- You want to use Playwright's built-in server (`npx playwright run-server`) or a custom gRPC service

### Stage 4: Ephemeral browsers (future)

For cloud-native deployments, each browser probe spawns an ephemeral container:

```
Worker → API call → Cloud Run / ECS task → run browser → return result → container dies
```

This scales to hundreds of concurrent browsers with no long-running infrastructure. The trade-off is cold-start latency (~3-5s for Chromium in a container).

## Recommendations by scale

| Playwright probes | Architecture | `max_browsers` | Host specs |
|-------------------|-------------|---------------|------------|
| 1-5 | Single worker (Stage 1) | 2 | 2 vCPU, 1-2 GB |
| 5-15 | Dedicated browser worker (Stage 2) | 4 | 4 vCPU, 4 GB (browser worker) |
| 15-50 | Remote browser service (Stage 3) | 10+ | 8 vCPU, 8 GB (browser server) |
| 50+ | Ephemeral browsers (Stage 4) | unlimited | Cloud auto-scaling |

## Browser reuse (planned optimization)

Currently each probe invocation launches and closes a fresh Chromium instance. A future optimization would maintain a browser pool:

1. Launch N Chromium instances at startup
2. Each probe gets a fresh `BrowserContext` (isolated cookies, storage) from the pool
3. Contexts are destroyed after each probe; browsers persist
4. Eliminates the 2-3s cold-start per probe

This reduces per-probe overhead from ~40 MB (Node.js) + 150-300 MB (Chromium launch) to just a new context (~10-20 MB). It's the logical next step before moving to managed mode.
