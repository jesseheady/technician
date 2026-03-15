# Getting started

Get Technician running on your machine for local development and testing.

## Prerequisites

- **Go 1.25+** – [go.dev/dl](https://go.dev/dl/)
- **macOS** – For the init script; other platforms see [Run without the script](#run-without-the-script) below.

Optional (for full feature set):

- **Docker & Docker Compose** – To run Prometheus and Grafana alongside Technician.
- **Node.js 18+** – Required for Playwright (browser) probes. Install via [nodejs.org](https://nodejs.org/) or `nvm`.
- **mtr** – For traceroute probes. On macOS: `brew install mtr`. Note: mtr typically requires root (raw sockets) on macOS and Linux, so traceroute probes may fail unless you run as root. For local dev you can remove or leave traceroute probes out of your config.

## One-time setup (Mac)

From the project root:

```bash
./scripts/init-mac.sh
```

This checks Go, Node (if needed), and optional tools; installs Go dependencies; builds the binary; and optionally starts the full stack. See [scripts/init-mac.sh](../scripts/init-mac.sh) for what it does.

## Configuration layout

```
examples/          # Reference configs with placeholder values (checked in)
config/            # Your local/production configs (gitignored)
```

To get started, copy the examples to `config/` and customise:

```bash
cp -r examples/ config/
# Edit config/probes/http.yml, config/technician.yml, etc.
```

## Run the worker (minimal)

```bash
go run . worker --config config/technician.yml
```

Or with the example configs directly (placeholder targets):

```bash
go run . worker --config examples/technician.yml
```

- Status page: [http://localhost:9590/](http://localhost:9590/) — real-time probe status with collapsible groups, history bars with tooltips (UTC/local time), and auto-refresh
- Status API: [http://localhost:9590/api/status](http://localhost:9590/api/status) — JSON snapshot of all probe state
- Metrics: [http://localhost:9590/metrics](http://localhost:9590/metrics)
- Health: [http://localhost:9590/health](http://localhost:9590/health)
- Blackbox-style probe: [http://localhost:9590/probe?target=https://example.com&module=http_2xx](http://localhost:9590/probe?target=https://example.com&module=http_2xx)

Use `--site <code>` to run as a specific site (e.g. `--site us-east-1`). Use `-v` for debug logging.

## Run with Prometheus and Grafana

```bash
docker compose up
```

- **Technician**: metrics on port 9590 (inside the compose network).
- **Prometheus**: [http://localhost:9090](http://localhost:9090) – scrapes Technician, evaluates alert rules.
- **Alertmanager**: [http://localhost:9093](http://localhost:9093) – routes alerts to Slack, Discord, PagerDuty, etc. Configure receivers in `prometheus/alertmanager.yml`.
- **Grafana**: [http://localhost:3000](http://localhost:3000) – default login `admin` / `admin` (see `docker-compose.yml` for overrides).

Config and probes are mounted from `config/`. To use the examples directly, change the volume paths in `docker-compose.yml` to `./examples/`. `SITE_CODE` only affects metric labels (`region`, `city`, `country`); Compose uses `SITE_CODE=local` so the config's "local" site is used and labels stay distinct from real regions.

## Probe configuration

Probes are defined in YAML files under the probes directory (see `examples/probes/` for reference). Each probe type has its own file:

| File | Probe type | What it checks |
|------|-----------|----------------|
| `http.yml` | HTTP/HTTPS | Status codes, response bodies, headers, redirects |
| `tcp.yml` | TCP | Port reachability, TLS handshake, banner checks |
| `dns.yml` | DNS | Record lookups (A, AAAA, MX, TXT, CNAME, NS, SRV) |
| `icmp.yml` | ICMP (ping) | Packet loss, round-trip time |
| `grpc.yml` | gRPC | Health check protocol |
| `ntp.yml` | NTP | Clock offset, stratum, round-trip time |
| `tls.yml` | TLS | Certificate expiry, chain validity, issuer/SAN details |
| `smtp.yml` | SMTP | Mail server connectivity |
| `traceroute.yml` | Traceroute | Network path hops (requires mtr) |
| `udp.yml` | UDP | Datagram send/receive, payload matching |
| `bgp.yml` | BGP | Origin AS validation, prefix hijack detection |
| `domain_expiry.yml` | Domain expiry | RDAP-based registration expiry, warn/critical thresholds |
| `playwright/playwright.yml` | Playwright | Browser flows, Core Web Vitals, HAR capture |

All probe types support optional `retry` (count, backoff, delay) and `degraded_after` (duration threshold for degraded state). All YAML files support `${ENV_VAR}` expansion.

**Retry policies**: Every probe should have a retry policy to absorb transient failures. The example configs include recommended defaults:

```yaml
# Fast probes (HTTP, TCP, DNS, ICMP, UDP, gRPC) — short delay
retry:
  count: 1
  backoff: none
  delay: 2s

# Slow probes (TLS, BGP, traceroute, domain expiry, Playwright) — longer delay
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

Backoff options: `none` (fixed delay), `linear` (delay × attempt), `exponential` (delay × 2^attempt). For most probes, `none` with a single retry is sufficient. Use `exponential` with `count: 2` for critical endpoints where you want more aggressive retry behavior.

**Schedule recommendations**: Not all probe types need the same frequency. Recommended intervals by type:

| Probe type | Recommended schedule | Why |
|------------|---------------------|-----|
| HTTP, TCP, DNS, ICMP, gRPC, UDP | `*/60 * * * * *` (60s) | Core uptime — fast, lightweight checks |
| NTP | `0 */10 * * * *` (10 min) | Clock drift changes slowly |
| SMTP | `0 */15 * * * *` (15 min) | Mail servers rate-limit connections |
| BGP | `0 */15 * * * *` (15 min) | Route tables change infrequently |
| Traceroute | `0 */30 * * * *` (30 min) | Expensive (spawns mtr subprocess) |
| TLS, Domain expiry | `0 0 */6 * * *` (6 hr) | Certificates and registrations change on day/week timescales |
| Playwright | `0 */5 * * * *` (5 min) | Browser launches are heavy; balance with visibility needs |

**Groups**: Add a `group` field to organize probes on the status page into collapsible sections:

```yaml
- name: example.com
  group: Marketing
  url: https://example.com
  schedule: "*/60 * * * * *"
```

**Schedule format**: Technician uses 6-field cron expressions (seconds included) via [robfig/cron](https://pkg.go.dev/github.com/robfig/cron/v3):

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

**Persistence**: Probe history and budget check state are persisted to a JSON file at `$TECHNICIAN_DATA_DIR/status.json` (default `/var/lib/technician/status.json`). In Docker, the `technician_data` named volume ensures state survives container rebuilds.

## Recipes

### API health check with security headers

HTTP probes support body and header assertions, which together cover the "test my API status and body content" use case. This example validates the status code, response body, Content-Type, and common security headers in a single probe:

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

If any assertion fails, the probe is marked as failed and the failure message identifies which assertion didn't pass.

## Run a single probe (debug)

```bash
go run . probe --name "example.com" --config config/technician.yml
```

Probe name must match a `name` in your probe definitions under `config/probes/`.

## Validate (probes + budgets)

Runs all probes once and checks them against performance budgets. Exits 0 if all pass, 1 if any budget is violated:

```bash
go run . validate --config config/technician.yml --budget config/budgets.yml
```

Output formats: `--output text` (default), `--output json`, `--output gha` (GitHub Actions annotations).

## Run without the script

1. Install Go 1.25+ and ensure `go` is on your `PATH`.
2. From the repo root: `go mod download` then `go build -o technician .`.
3. Copy examples to config: `cp -r examples/ config/` and customise.
4. Run: `./technician worker --config config/technician.yml`.

On Linux you may need `mtr` for traceroute probes (`apt install mtr` or equivalent). For Playwright probes you need Node.js and the Playwright Chromium browser (see Dockerfile or run `npx playwright install chromium` in a Node environment).

## Next steps

- [Alerting](alerting.md) – native webhooks (Discord, Slack), Grafana alerting (recommended), and Alertmanager
- [CI](ci.md) – GitHub Actions workflow, generic CI pipelines, budget validation
- [Testing and end-to-end validation](testing-and-e2e.md)
- [Local development configuration](mock-production.md)
- [Playwright scaling](playwright-scaling.md) – browser probe resource analysis, concurrency controls, dedicated runners
- [AGENTS.md](../AGENTS.md) – architecture and conventions
- [Central Prometheus and Grafana](architecture/central-prometheus-grafana.md) – sending deployed and edge probe metrics to a central Prometheus in your VPC and using central Grafana as source of record
