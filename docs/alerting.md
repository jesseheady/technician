# Alerting

Technician supports three alerting strategies. Pick the one that fits your stack.

## 1. Grafana alerting (recommended)

Grafana is the recommended alerting backend for production. It provides:

- Native contact points for Discord, Slack, PagerDuty, Teams, Email, Telegram, OpsGenie, and more — no sidecars or bridges needed.
- Alert rule evaluation directly from Prometheus data.
- Silencing, grouping, and escalation policies through the UI.
- Alert history and state timeline.
- Notification templates with full Go template support.

### Setup

1. Open Grafana at [http://localhost:3000](http://localhost:3000) (default `admin`/`admin`).
2. Go to **Alerting > Contact points** and add your integration (Discord, Slack, etc.).
3. Go to **Alerting > Alert rules** and create rules using Technician's Prometheus metrics:
   - `technician_probe_up == 0` — probe is down
   - `technician_budget_violation == 1` — budget threshold exceeded
   - `technician_http_ttfb_seconds > 2` — high TTFB
   - `technician_browser_lcp_ms > 2500` — high LCP
4. Set notification policies to route alerts to the appropriate contact point.

Grafana evaluates rules independently of Prometheus Alertmanager, so you can use Grafana alerting with or without Alertmanager running.

## 2. Native webhooks (simple, self-contained)

Technician can send notifications directly to Discord, Slack, or any HTTP endpoint. No external alerting stack required — useful for simple setups, edge deployments, or when running Technician standalone without Grafana.

### Configuration

Add a `webhooks` section to your `technician.yml`:

```yaml
webhooks:
  # Slack channel — all events, all severities
  - url: https://hooks.slack.com/services/T.../B.../xxx
    type: slack
    events: [probe_down, probe_up, cert_expiring, budget_violation]
    cooldown: 5m

  # Discord — warnings only (cert expiry heads-up, budget alerts)
  - url: https://discord.com/api/webhooks/ID/TOKEN
    type: discord
    events: [cert_expiring, budget_violation]
    severities: [warning]
    cooldown: 5m

  # PagerDuty / OpsGenie — critical only (pages on-call)
  - url: https://your-endpoint.example.com/alerts
    type: generic
    events: [probe_down, cert_expiring]
    severities: [critical]
    cooldown: 10m
```

| Field | Description |
|-------|-------------|
| `url` | Webhook endpoint URL. |
| `type` | `discord`, `slack`, or `generic`. Determines payload format. |
| `events` | Which events trigger notifications. Omit for all events. |
| `severities` | Filter by severity: `warning`, `critical`. Omit to receive all severities. |
| `cooldown` | Minimum interval between repeated notifications for the same probe+event. Default `5m`. |

### Event types

| Event | Default severity | Fires when |
|-------|-----------------|------------|
| `probe_down` | `critical` | A probe transitions from up to down (not on every failed check). |
| `probe_up` | — | A probe transitions from down to up (recovery). |
| `budget_violation` | `warning` | A budget metric becomes newly violated. |
| `cert_expiring` | `warning` or `critical` | A TLS certificate enters the warn or critical expiry window. Severity escalates from warning to critical as the deadline approaches. |

### Severity routing

Events carry a severity level that enables routing to different channels:

- **`warning`** — Informational. TLS cert expiring within `warn_days` (default 30), budget violations. Good for Slack or a low-priority channel.
- **`critical`** — Actionable. Probe failures, TLS cert within `critical_days` (default 7). Route to PagerDuty, OpsGenie, or an on-call channel.

Configure multiple webhook entries with different `severities` filters to fork notifications. For example, one Slack channel receives all alerts while PagerDuty only receives critical.

The `cert_expiring` event uses state tracking: it fires once when entering the warning window, then again if it escalates to critical. It does not re-fire on every probe cycle. If the cert is renewed (days remaining goes above the warn threshold), the state resets so future expiry will trigger again.

### Payload formats

- **Discord**: Native embed format with color-coded status (red for critical, amber for warning, green for recovery), probe details, error info, and cert expiry fields.
- **Slack**: Incoming webhook format with colored attachments and `[WARNING]`/`[CRITICAL]` severity prefix.
- **Generic**: JSON object with `type`, `severity`, `probe`, `probe_type`, `message`, `details`, and `timestamp` fields.

### Limitations vs Grafana

Native webhooks are intentionally simple. They do not provide:

- Alert grouping or deduplication beyond the per-probe cooldown.
- Silencing or maintenance windows.
- Escalation policies.
- Alert history or state timeline.
- Rich notification templates.

For these features, use Grafana alerting.

## 3. Prometheus Alertmanager

Alertmanager provides rule-based routing, grouping, inhibition, and silencing. Technician exposes Prometheus metrics; Prometheus evaluates alert rules (defined in `prometheus/rules.yml`) and sends firing alerts to Alertmanager, which routes them to receivers.

### Setup

Alertmanager is included in `docker-compose.yml`. Configure receivers in `prometheus/alertmanager.yml`. Alertmanager natively supports Slack, PagerDuty, OpsGenie, Email, Telegram, and generic webhooks.

**Discord note**: Alertmanager's Slack attachment format is not compatible with Discord's `/slack` endpoint. To use Alertmanager with Discord, you need the [alertmanager-discord](https://github.com/benjojo/alertmanager-discord) bridge container (commented out in `docker-compose.yml`). For simpler Discord delivery, use native webhooks or Grafana alerting instead.

### Alert rules

Alert rules are defined in `prometheus/rules.yml`. A `TestAlert` rule (commented out) can be uncommented to validate the full pipeline. See the file for the full list of pre-configured rules.

## Comparison

| Feature | Native webhooks | Grafana alerting | Alertmanager |
|---------|----------------|------------------|--------------|
| Setup complexity | Minimal (config only) | Low (UI config) | Moderate (YAML + containers) |
| Severity routing | Yes (`severities` filter) | Yes (notification policies) | Yes (route matching) |
| Discord support | Native | Native | Requires bridge container |
| Slack support | Native | Native | Native |
| Grouping / dedup | Per-probe cooldown | Full | Full |
| Silencing | No | Yes (UI) | Yes (API/UI) |
| Escalation | No | Yes | Yes |
| Alert history | No | Yes | No |
| Runs standalone | Yes | Needs Grafana + Prometheus | Needs Prometheus |
