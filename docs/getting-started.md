# Getting started

There are three ways in, depending on what you're trying to do. Pick one:

| I want to… | Use | Needs |
|------------|-----|-------|
| **Evaluate** — see the whole stack running fast | [Try it (Docker)](#try-it-docker) | Docker only |
| **Contribute** — build and iterate on the code | [Set up for development](#set-up-for-development) | Go (+ optional Node) |
| **Operate** — run it live in production | [Run in production](#run-in-production) | Docker or Kubernetes |

All three work on macOS, Linux, and Windows (WSL).

## Try it (Docker)

The fastest zero-to-running path. Pulls the published image and brings up
Technician alongside Prometheus and Grafana — no build, no Go toolchain.

```bash
git clone https://github.com/jesseheady/technician.git
cd technician
cp -r examples/ config/     # starter configs with placeholder targets
docker compose up
```

- **Status page**: [http://localhost:9590/](http://localhost:9590/)
- **Technician metrics**: [http://localhost:9590/metrics](http://localhost:9590/metrics)
- **Prometheus**: [http://localhost:9090](http://localhost:9090)
- **Alertmanager**: [http://localhost:9093](http://localhost:9093)
- **Grafana**: [http://localhost:3000](http://localhost:3000) (default `admin` / `admin`)

> `docker compose up` **builds Technician from local source** (the base
> `docker-compose.yml` uses `build: .`), so a fresh checkout works with no
> external image. To pull the published image instead, add the production
> overlay: `docker compose -f docker-compose.yml -f docker-compose.prod.yml up`.
> See [Run in production](#run-in-production).

## Set up for development

For contributing to Technician. This builds the binary from source so you can
iterate on Go code directly.

### Prerequisites

- **Go 1.26+** – [go.dev/dl](https://go.dev/dl/) (required)

Optional, for the full feature set:

- **Docker & Docker Compose** – to run Prometheus and Grafana alongside the worker.
- **Node.js 22+** – required for Playwright (browser) checks. Install via [nodejs.org](https://nodejs.org/), `nvm`, or your package manager.
- **mtr** – for traceroute checks. macOS: `brew install mtr`; Debian/Ubuntu/WSL: `sudo apt-get install mtr-tiny`; Fedora: `sudo dnf install mtr`. Note: mtr needs root (raw sockets) on macOS and Linux, so traceroute checks may fail unless run as root. For local dev you can leave traceroute checks out of your config.

### One-time setup

From the project root:

```bash
./scripts/init.sh
```

The script detects your platform (macOS / Linux / WSL) and prints the matching
install hints. It checks Go, Node (if present), and optional tools; installs Go
dependencies; builds the binary; and configures the git hooks. Pass `--stack` to
also start the full Docker stack. See [scripts/init.sh](../scripts/init.sh) for
exactly what it does, or [Set up without the script](#set-up-without-the-script)
to do the same steps by hand.

### Reproducing a release artifact

Releases contain reproducible artifacts. To reproduce an artifact locally, use
`scripts/build.sh` with appropriate values of GOOS and GOARCH for your target.
For example, if you'd like to reproduce `technician-linux-arm64.tar.gz` from
a release, run the script on a Linux system like this:

```shell
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 ./scripts/build.sh
sha256sum technician-linux-arm64.tar.gz
```

The resulting digest should match the one shown on the release page for the
corresponding tarball. A caveat: this is not expected to produce a matching
tarball in cross-compiling environments. The Darwin tarball can be reproduced
on a macOS system; the Linux tarball on a Linux host.

### Run the worker

```bash
cp -r examples/ config/                                  # first time only
go run . worker --config config/technician.yml
```

Or point straight at the example configs (placeholder targets):

```bash
go run . worker --config examples/technician.yml
```

- Status page: [http://localhost:9590/](http://localhost:9590/) — real-time status with collapsible groups, history bars, auto-refresh
- Status API: [http://localhost:9590/api/status](http://localhost:9590/api/status) — JSON snapshot of all check state
- Metrics: [http://localhost:9590/metrics](http://localhost:9590/metrics)
- Health: [http://localhost:9590/health](http://localhost:9590/health)
- Blackbox-style probe: [http://localhost:9590/probe?target=https://example.com&module=http_2xx](http://localhost:9590/probe?target=https://example.com&module=http_2xx)

Use `--origin <id>` to run as a specific origin (e.g. `--origin us-east-1`), and `--log-level debug` for verbose logging.

## Run in production

Technician is a long-lived worker plus an observability stack (Prometheus +
Grafana), so the production story is a **published image**, not a build. Two
supported shapes:

### Single node (Docker Compose)

Copy `docker-compose.yml`, `docker-compose.prod.yml`, `prometheus/`, and your
`config/` to the host — no source checkout or build needed. The production
overlay swaps the source build for the published `ghcr.io/jesseheady/technician`
image:

```bash
# On the target host:
export TECHNICIAN_VERSION=<tag>     # pin a release tag; defaults to :latest
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
```

Pin `TECHNICIAN_VERSION` to a released tag rather than tracking `:latest` so
deploys are reproducible. Set the external-URL env vars (`PROMETHEUS_EXTERNAL_URL`,
`ALERTMANAGER_EXTERNAL_URL`) so notification links resolve from outside the host —
see [.env.example](../.env.example).

### Kubernetes (Helm)

A chart lives in [deploy/helm/technician](../deploy/helm/technician); see its
[README](../deploy/helm/technician/README.md) and `values.yaml` for options.

For running workers across regions and scraping them into a central Prometheus in
your VPC, see [Central Prometheus and Grafana](architecture/central-prometheus-grafana.md).
For sizing (VPS, Docker, Lambda, Workers), see [deployment sizing](deployment-sizing.md).

## Configuration layout

```
examples/          # Reference configs with placeholder values (checked in)
config/            # Your local/production configs (gitignored)
```

To get started, copy the examples to `config/` and customise:

```bash
cp -r examples/ config/
# Edit config/checks.yml, config/technician.yml, etc.
```

Docker Compose mounts config and checks from `config/`. To use the examples directly, change the volume paths in `docker-compose.yml` to `./examples/`. `ORIGIN_ID` only affects metric labels (`region`, `city`, `country`); Compose sets `ORIGIN_ID=local` so the config's "local" origin is used and labels stay distinct from real regions.

## Check configuration

Checks are defined in YAML files under the checks directory (see `examples/checks/` for reference). Each check type has its own file:

| File | Check type | What it checks |
|------|-----------|----------------|
| `http.yml` | HTTP/HTTPS | Status codes, response bodies, headers, redirects, basic/bearer auth, TLS version pinning, IP version, proxy |
| `tcp.yml` | TCP | Port reachability, TLS handshake (with version pinning), banner checks |
| `dns.yml` | DNS | Record lookups (A, AAAA, MX, TXT, CNAME, NS, SOA, SRV) |
| `icmp.yml` | ICMP (ping) | Packet loss, round-trip time |
| `grpc.yml` | gRPC | Health check protocol |
| `ntp.yml` | NTP | Clock offset, stratum, round-trip time |
| `tls.yml` | TLS | Certificate expiry, chain validity, issuer/SAN details |
| `smtp.yml` | SMTP | Connectivity, STARTTLS negotiation, authentication |
| `traceroute.yml` | Traceroute | Network path hops (requires mtr) |
| `udp.yml` | UDP | Datagram send/receive, payload matching |
| `bgp.yml` | BGP | Origin AS validation, prefix hijack detection |
| `domain_expiry.yml` | Domain expiry | RDAP-based registration expiry, warn/critical thresholds |
| `websocket.yml` | WebSocket | ws/wss connect, optional message send + response assertion, connection/message timing |
| `playwright/playwright.yml` | Playwright | Browser flows, Core Web Vitals, HAR capture |

All check types support optional `retry` (count, backoff, delay) and `degraded_after` (duration threshold for degraded state). All YAML files support `${ENV_VAR}` expansion.

**Browser checks**: the `playwright` block in `technician.yml` chooses where the browser runs.

```yaml
playwright:
  mode: local        # launch Chromium in this container (default)
  max_browsers: 2    # concurrent browsers; queued beyond this
```

```yaml
playwright:
  mode: managed                        # connect to a Playwright server instead
  server_url: ws://playwright:3000/    # required when mode is managed
  max_browsers: 2
```

Managed mode runs the browser in a stock upstream Playwright image alongside the worker, so browser patching follows its own cadence and running a different engine is an image-tag change. Start it with the overlay:

```bash
docker compose -f docker-compose.yml -f docker-compose.playwright.yml up -d
```

The sidecar's image tag and the playwright version the binary embeds must match, or the client and server fail their handshake. An invalid mode, or `managed` without a `server_url`, is rejected at startup. See [Playwright scaling](playwright-scaling.md).

**Logging**: set `logging.format` (`json` for Loki-native, `text` for local dev) and `logging.level` (`debug`/`info`/`warn`/`error`) in `technician.yml`; the `--log-level` flag overrides the configured level.

On startup every check runs once immediately (after a brief per-origin stagger) before settling into its schedule, so the status page, metrics, and alert rules have data within seconds of boot rather than waiting for the first scheduled tick.

**Retry policies**: Every check should have a retry policy to absorb transient failures. The example configs include recommended defaults:

```yaml
# Fast checks (HTTP, TCP, DNS, ICMP, UDP, gRPC) — short delay
retry:
  count: 1
  backoff: none
  delay: 2s

# Slow checks (TLS, BGP, traceroute, domain expiry, Playwright) — longer delay
retry:
  count: 1
  backoff: none
  delay: 5s

# SMTP — moderate delay (mail servers can be slow to respond)
retry:
  count: 1
  backoff: none
  delay: 3s
```

Backoff options: `none` (fixed delay), `linear` (delay × attempt), `exponential` (delay × 2^attempt). For most checks, `none` with a single retry is sufficient. Use `exponential` with `count: 2` for critical endpoints where you want more aggressive retry behavior.

**Schedule recommendations**: Not all check types need the same frequency. Recommended intervals by type:

| Check type | Recommended schedule | Why |
|------------|---------------------|-----|
| HTTP, TCP, DNS, ICMP, gRPC, UDP | `*/60 * * * * *` (60s) | Core uptime — fast, lightweight checks |
| NTP | `0 */10 * * * *` (10 min) | Clock drift changes slowly |
| SMTP | `0 */15 * * * *` (15 min) | Mail servers rate-limit connections |
| BGP | `0 */15 * * * *` (15 min) | Route tables change infrequently |
| Traceroute | `0 */30 * * * *` (30 min) | Expensive (spawns mtr subprocess) |
| TLS, Domain expiry | `0 0 */6 * * *` (6 hr) | Certificates and registrations change on day/week timescales |
| Playwright | `0 */5 * * * *` (5 min) | Browser launches are heavy; balance with visibility needs |

**Degraded thresholds**: The `degraded_after` field marks a check as degraded (yellow) when its latency exceeds the threshold — even though the check itself succeeded. Thresholds should be tuned to your deployment context, since network latency varies significantly between environments.

For most check types that latency is the check's total duration, which times a single operation. ICMP is the exception: it repeats the same probe `count` times, so it compares the threshold against the **average round-trip time of one probe**. An ICMP threshold means the same thing whatever `count` you set, and the values below are per-probe RTT.

Recommended `degraded_after` by check type and deployment:

| Check type | Cloud VPS (same region) | Cloud VPS (cross-region) | Residential / Docker Desktop |
|------------|------------------------|--------------------------|------------------------------|
| ICMP | 50ms | 100ms | 200ms |
| DNS | 100ms | 200ms | 500ms |
| UDP | 100ms | 200ms | 500ms |
| NTP (dedicated servers) | 50ms | 100ms | 200ms |
| NTP (pool.ntp.org) | 100ms | 200ms | 500ms |
| TCP | 500ms | 1s | 2s |
| TCP + TLS | 1s | 2s | 3s |
| gRPC | 500ms | 1s | 2s |
| HTTP (static/CDN) | 500ms | 1s | 3s |
| HTTP (API/dynamic) | 1s | 2s | 5s |
| SMTP | 2s | 3s | 5s |
| Traceroute | 10s | 15s | 15s |
| Playwright | 5s | 8s | 10s |

Why the variation:

- **ICMP/DNS/UDP** are the most sensitive to environment. A cloud VPS in the same region as 8.8.8.8 gets 1–5ms pings; a Mac on residential WiFi gets 100–160ms. Setting 100ms as the threshold in a residential environment means the check is permanently degraded — which is noise, not signal.
- **NTP** depends on the server. Dedicated servers like `time.google.com` and `time.cloudflare.com` are anycast and fast (20–40ms). `pool.ntp.org` round-robins to volunteer servers that may be geographically distant, with p90 latencies 5–10x higher than dedicated servers.
- **TCP/gRPC** include connection setup (and TLS handshake if enabled), so thresholds should be higher than raw ICMP/DNS.
- **HTTP** varies by what's behind the URL. A static site on a CDN responds in 50–100ms; a dynamic API may take 500ms–2s legitimately.
- **SMTP** servers are often slow to respond to EHLO, especially with greylisting or rate limiting.
- **Traceroute** spawns an mtr subprocess with multiple hops and round-trips; 5–10s is normal even from fast networks.
- **Playwright** launches a full browser, loads a page, and collects Web Vitals. Baseline is 3–5s for a simple page; complex flows take longer.

If a check is permanently degraded, the threshold is too low for your environment — raise it until degraded status reflects an actual change in behavior, not the baseline.

TLS and domain expiry checks don't typically use `degraded_after` because they check certificate/registration state rather than latency. BGP checks query the RIPE RIS API, which has variable response times (100ms–4s) that aren't indicative of your network health — `degraded_after` is generally not useful for BGP.

**Groups**: Add a `group` field to organize checks on the status page into collapsible sections:

```yaml
- name: example.com
  group: Marketing
  url: https://example.com
  schedule: "*/60 * * * * *"
```

**Schedule format**: Technician uses 6-field cron expressions (seconds included), parsed by [gronx](https://pkg.go.dev/github.com/adhocore/gronx):

```
┌──────── second (0-59)
│ ┌────── minute (0-59)
│ │ ┌──── hour (0-23)
│ │ │ ┌── day of month (1-31)
│ │ │ │ ┌ month (1-12)
│ │ │ │ │ ┌ day of week (0-6)
│ │ │ │ │ │
* * * * * *
```

Examples: `*/60 * * * * *` (every 60 seconds), `0 */5 * * * *` (every 5 minutes at second 0), `0 0 * * * *` (every hour).

**Persistence**: Check history and budget check state are persisted to a JSON file at `$TECHNICIAN_DATA_DIR/status.json` (default `/var/lib/technician/status.json`). In Docker, the `technician_data` named volume ensures state survives container rebuilds.

## Recipes

### API health check with security headers

HTTP checks support body and header assertions, which together cover the "test my API status and body content" use case. This example validates the status code, response body, Content-Type, and common security headers in a single check:

```yaml
- name: api-full-check
  group: API
  url: https://api.example.com/health
  expected_status: 200
  timeout: 10s
  schedule: "*/60 * * * * *"
  degraded_after: 2s
  retry:
    count: 1
    backoff: none
    delay: 2s
  assertions:
    # Body: verify the health payload
    - type: contains
      target: '"status":"ok"'
    - type: not_contains
      target: '"error"'
    - type: regex
      target: '"version":"\d+\.\d+\.\d+"'

    # Content-Type
    - type: header_contains
      header: Content-Type
      target: application/json

    # Security headers
    - type: header_contains
      header: Strict-Transport-Security
      target: max-age=
    - type: header_contains
      header: X-Content-Type-Options
      target: nosniff
    - type: header_contains
      header: X-Frame-Options
      target: DENY
    - type: header_regex
      header: Content-Security-Policy
      target: "default-src"
```

**Assertion types reference:**

| Type | Scope | What it checks |
|------|-------|----------------|
| `contains` | Body | Body includes the target string |
| `not_contains` | Body | Body does NOT include the target string |
| `regex` | Body | Body matches the target regex |
| `header_contains` | Header | Named header includes the target string |
| `header_not_contains` | Header | Named header does NOT include the target string |
| `header_regex` | Header | Named header matches the target regex |

If any assertion fails, the check is marked as failed and the failure message identifies which assertion didn't pass.

## Run a single check (debug)

```bash
go run . check run --check "example.com" --config config/technician.yml
```

Check name must match a `name` in your check definitions under `config/checks/`.

## Validate (checks + budgets)

Runs all checks once and checks them against performance budgets. Exits 0 if all pass, 1 if any budget is violated:

```bash
go run . validate --config config/technician.yml --budget config/budgets.yml
```

Output formats: `--output text` (default), `--output json`, `--output gha` (GitHub Actions annotations).

### How budgets match a check

- `check` matches a check `name` exactly. An entry matching no check is silently inert.
- `- check: "*"` supplies **defaults**, and a named entry overrides them **per metric**. A check that is legitimately slow (domain expiry, traceroute) sets its own `duration` and keeps every other default, so adding a per-check threshold never silently drops coverage.
- Thresholds for metrics a check never emits are ignored, not failed — `lcp`/`inp`/`cls` on an HTTP check do nothing, since only browser checks report Core Web Vitals.
- Violations sourced from `"*"` are marked as inherited: `--output text` appends `inherited from "*"`, and `--output json` sets `"inherited": true`. That distinguishes a check you tuned from one still riding the defaults.

See [examples/budgets.yml](../examples/budgets.yml) for the shape.

## Set up without the script

The same steps [scripts/init.sh](../scripts/init.sh) runs, by hand:

1. Install Go 1.26+ and ensure `go` is on your `PATH`.
2. From the repo root: `go mod download` then `go build -o technician .`.
3. Copy examples to config: `cp -r examples/ config/` and customise.
4. Run: `./technician worker --config config/technician.yml`.

For traceroute checks, install `mtr` (macOS: `brew install mtr`; Debian/Ubuntu/WSL: `sudo apt-get install mtr-tiny`; Fedora: `sudo dnf install mtr`). For Playwright checks you need Node.js and the Playwright Chromium browser (see Dockerfile or run `npx playwright install chromium` in a Node environment).

## Next steps

- [Alerting](alerting.md) – native webhooks (Discord, Slack), Grafana alerting (recommended), and Alertmanager
- [CI](ci.md) – GitHub Actions workflow, generic CI pipelines, budget validation
- [Testing and end-to-end validation](testing-and-e2e.md)
- [Local development configuration](mock-production.md)
- [Playwright scaling](playwright-scaling.md) – browser check resource analysis, concurrency controls, dedicated runners
- [AGENTS.md](../AGENTS.md) – architecture and conventions
- [Central Prometheus and Grafana](architecture/central-prometheus-grafana.md) – sending deployed and edge check metrics to a central Prometheus in your VPC and using central Grafana as source of record
