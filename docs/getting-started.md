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

## Run the worker (minimal)

Using the example config and probes:

```bash
go run . worker --config examples/technician.yml
```

Or after building:

```bash
go build -o technician .
./technician worker --config examples/technician.yml
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
- **Prometheus**: [http://localhost:9090](http://localhost:9090) – scrapes Technician.
- **Grafana**: [http://localhost:3000](http://localhost:3000) – default login `admin` / `admin` (see `docker-compose.yml` for overrides).

Config and probes are mounted from `examples/`. To use your own, change the volume paths in `docker-compose.yml` or pass a different config at build/runtime. `SITE_CODE` only affects metric labels (`site_code`, `site_city`, `site_country`); Compose uses `SITE_CODE=local` so the example config’s “local” site is used and labels stay distinct from real regions. You can also set it from hostname (e.g. `SITE_CODE=$(hostname)`) if you want a durable per-host identifier.

## Probe configuration

Probes are defined in YAML files under the probes directory (see `examples/probes/`). Each probe type has its own file: `http.yml`, `smtp.yml`, `traceroute.yml`, and `playwright/playwright.yml`.

**Groups**: Add a `group` field to organize probes on the status page into collapsible sections:

```yaml
- name: jesseheady.com
  group: Marketing
  url: https://jesseheady.com
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

**Persistence**: Probe history is persisted to a JSON file at `$TECHNICIAN_DATA_DIR/status.json` (default `/var/lib/technician/status.json`). In Docker, the `technician_data` named volume ensures history survives container rebuilds.

## Run a single probe (debug)

```bash
go run . probe --name "Example Website" --config examples/technician.yml
```

Probe name must match a `name` in your probe definitions under `examples/probes/`.

## Validate (probes + budgets)

Runs all probes once and checks them against performance budgets. Exits 0 if all pass, 1 if any budget is violated:

```bash
go run . validate --config examples/technician.yml --budget examples/budgets.yml
```

Output formats: `--output text` (default), `--output json`, `--output gha` (GitHub Actions annotations).

## Run without the script

1. Install Go 1.25+ and ensure `go` is on your `PATH`.
2. From the repo root: `go mod download` then `go build -o technician .`.
3. Run as above: `./technician worker --config examples/technician.yml`.

On Linux you may need `mtr` for traceroute probes (`apt install mtr` or equivalent). For Playwright probes you need Node.js and the Playwright Chromium browser (see Dockerfile or run `npx playwright install chromium` in a Node environment).

## Next steps

- [Testing and end-to-end validation](testing-and-e2e.md)
- [Mock production configuration](mock-production.md)
- [AGENTS.md](../AGENTS.md) – architecture and conventions
- [Central Prometheus and Grafana](architecture/central-prometheus-grafana.md) – sending deployed and edge probe metrics to a central Prometheus in your VPC and using central Grafana as source of record
