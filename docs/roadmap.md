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

## Possible improvements

- **Module path** — `github.com/monkeyWzr/technician` uses a personal GitHub account. Consider a dedicated org or project namespace if establishing Technician as an independent project.

## Recently completed

### Native webhook notifications

Built-in webhook alerting directly from the Technician worker, independent of Prometheus/Grafana. Fires on probe state transitions (up→down, down→up) and new budget violations, with per-probe cooldown to prevent notification floods.

- **Package**: `internal/notify/` — `Manager` with state tracking, `Sender` interface with Discord, Slack, and generic HTTP implementations.
- **Config**: `webhooks` list in `technician.yml` with `url`, `type` (discord/slack/generic), `events` (probe_down/probe_up/budget_violation), and `cooldown` (default 5m).
- **CLI**: `technician test-webhook` sends a test notification to all configured webhooks.
- **Docs**: [alerting.md](alerting.md) covers native webhooks, Grafana alerting (recommended), and Alertmanager.

### Budget check persistence

Budget badge state (pass/fail per metric) is persisted alongside probe history in `status.json`. Badges survive container restarts without waiting for probes to complete a full cycle.
