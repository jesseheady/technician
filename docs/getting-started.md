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

- Status page: [http://localhost:9394/](http://localhost:9394/) — real-time probe status with collapsible groups, history bars with tooltips (UTC/local time), and auto-refresh
- Status API: [http://localhost:9394/api/status](http://localhost:9394/api/status) — JSON snapshot of all probe state
- Metrics: [http://localhost:9394/metrics](http://localhost:9394/metrics)
- Health: [http://localhost:9394/health](http://localhost:9394/health)
- Blackbox-style probe: [http://localhost:9394/probe?target=https://example.com&module=http_2xx](http://localhost:9394/probe?target=https://example.com&module=http_2xx)

Use `--site <code>` to run as a specific site (e.g. `--site us-east-1`). Use `-v` for debug logging.

## Run with Prometheus and Grafana

```bash
docker compose up
```

- **Technician**: metrics on port 9394 (inside the compose network).
- **Prometheus**: [http://localhost:9090](http://localhost:9090) – scrapes Technician, evaluates alert rules.
- **Alertmanager**: [http://localhost:9093](http://localhost:9093) – routes alerts to Slack, Discord, PagerDuty, etc. Configure receivers in `prometheus/alertmanager.yml`.
- **Grafana**: [http://localhost:3000](http://localhost:3000) – default login `admin` / `admin` (see `docker-compose.yml` for overrides).

Config and probes are mounted from `config/`. To use the examples directly, change the volume paths in `docker-compose.yml` to `./examples/`. `SITE_CODE` only affects metric labels (`site_code`, `site_city`, `site_country`); Compose uses `SITE_CODE=local` so the config's "local" site is used and labels stay distinct from real regions.

## Probe configuration

Probes are defined in YAML files under the probes directory (see `examples/probes/` for reference). Each probe type has its own file: `http.yml`, `smtp.yml`, `traceroute.yml`, and `playwright/playwright.yml`.

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
- [Testing and end-to-end validation](testing-and-e2e.md)
- [Local development configuration](mock-production.md)
- [AGENTS.md](../AGENTS.md) – architecture and conventions
- [Central Prometheus and Grafana](architecture/central-prometheus-grafana.md) – sending deployed and edge probe metrics to a central Prometheus in your VPC and using central Grafana as source of record
