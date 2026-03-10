# Mock production configuration

Run Technician locally with **full feature output** (metrics, traces, dashboards) while probing **mock local endpoints** instead of production. Use this for development, demos, and stable e2e validation.

## Goals

- Technician, Prometheus, and Grafana run locally (or in Docker).
- All probe types that support it target local mock services so results are deterministic and safe.
- Metrics and traces are produced as in production; you can open Grafana and see real dashboards.

## Overview

| Component        | Role |
|-----------------|------|
| **Technician**  | Worker with config that points probes at mock endpoints. |
| **Prometheus**  | Scrapes Technician and evaluates rules. |
| **Grafana**     | Dashboards backed by Prometheus. |
| **Mock targets**| Local HTTP (and optionally SMTP) servers that respond in a known way. |

## 1. Mock HTTP target

Run a minimal HTTP server that returns 200 and optional delay (to exercise TTFB/duration):

```bash
# One-liner: listen on 8080, respond 200
python3 -c "
from http.server import HTTPServer, BaseHTTPRequestHandler
class H(BaseHTTPRequestHandler):
    def do_GET(self): self.send_response(200); self.end_headers(); self.wfile.write(b'ok')
    def log_message(self, *a): pass
HTTPServer(('0.0.0.0', 8080), H).serve_forever()
"
```

Or use a small Go/Node server; keep it running in a terminal.

## 2. Mock production config and probes

Create a config set used only for local “mock production” runs.

**Option A – Use the provided mock config (recommended)**

The repo includes a ready-made mock config under `examples/mock-production/`:

- `technician.yml` – single site `local`, metrics and local artifacts.
- `probes/http.yml` – one HTTP probe to `http://localhost:8080/`.
- `probes/smtp.yml` and `probes/traceroute.yml` – empty; add entries if you run mock SMTP or want traceroute.
- `budgets.yml` – relaxed thresholds so `validate` passes.

Run the worker with:

```bash
go run . worker --config examples/mock-production/technician.yml
```

Start a mock HTTP server on port 8080 first (see [Mock HTTP target](#1-mock-http-target) above).

**Option B – Override with env vars**

Keep using `examples/technician.yml` and `examples/probes/`, but add env var expansion and a mock URL:

```yaml
# In examples/probes/http.yml (or a copy)
- name: Mock Website
  url: ${MOCK_HTTP_URL}
  expected_status: 200
  schedule: "*/30 * * * * *"
```

Then run:

```bash
export MOCK_HTTP_URL=http://localhost:8080/
go run . worker --config examples/technician.yml
```

## 3. Run the stack with mock targets

1. **Start the mock HTTP server** (terminal 1):
   ```bash
   python3 -c "from http.server import HTTPServer, BaseHTTPRequestHandler; ..."  # or your server
   ```

2. **Start Technician + Prometheus + Grafana** (terminal 2):
   ```bash
   # Use mock config; ensure compose mounts the mock config dir
   docker compose -f docker-compose.yml up
   ```
   If using a separate mock config dir, either:
   - Copy it into `examples/` and point compose at it, or
   - Run Technician outside Docker with the mock config and only start Prometheus + Grafana in Docker (and set Prometheus scrape target to host’s Technician port).

   **Simple approach**: Run Technician on the host with mock config so it can reach `localhost:8080`:
   ```bash
   go run . worker --config examples/mock-production/technician.yml
   ```
   In another terminal, run only Prometheus + Grafana (e.g. temporarily edit `docker-compose.yml` to remove the `technician` service and set Prometheus to scrape `host.docker.internal:9394` or your host IP).

   **All-in-Docker**: Run the mock HTTP server as another service in `docker-compose.yml` (e.g. image `python:3-slim` with a one-line server), and point probe URLs at that service name (e.g. `http://mock-http:8080/`). Mount mock config and probes into the Technician container.

3. **Verify**
   - [http://localhost:9394/metrics](http://localhost:9394/metrics) – should show `technician_*` metrics updating.
   - [http://localhost:9090](http://localhost:9090) – Prometheus → query `technician_probe_success`.
   - [http://localhost:3000](http://localhost:3000) – Grafana dashboards should show data for the mock probes.

## 4. Validate against mock config

Use the same mock config and budgets for e2e:

```bash
# Start mock HTTP server first
go run . validate --config examples/mock-production/technician.yml --budget examples/mock-production/budgets.yml
```

Expect exit 0 when thresholds are relaxed and the mock server is up.

## 5. Optional: mock SMTP

For SMTP probes without hitting real mail servers:

- Run a local SMTP server that accepts connections (e.g. [inbucket](https://www.inbucket.org/), or a minimal `smtpd` in Python). Point `probes/smtp.yml` at `localhost:25` (or the chosen port).
- Or leave SMTP probes out of the mock config so only HTTP (and optionally traceroute) run.

## 6. Summary

| Step | Action |
|------|--------|
| 1 | Run mock HTTP (and optionally SMTP) server(s) on localhost. |
| 2 | Create a mock config dir with `technician.yml` and probes targeting those URLs/hosts. |
| 3 | Run Technician with that config; run Prometheus + Grafana (Docker or host) so they scrape Technician. |
| 4 | Use the same config + budgets for `validate` to get reproducible e2e. |

This gives you a **mock production** setup: full feature output (metrics, traces, dashboards) against local, controllable endpoints.
