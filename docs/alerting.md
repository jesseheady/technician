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

### File-based provisioning

Grafana alerting resources (contact points, notification policies, mute timings) can also be managed via `prometheus/grafana-alerting.yml`, which is mounted into the Grafana container. This makes alerting config reproducible and version-controlled, matching the rest of the stack.

The file contains commented examples for:

- **Contact points**: infrastructure (PagerDuty + Slack), expiry (email), performance (Discord + Slack).
- **Notification policies**: category-based routing with per-category grouping and repeat intervals.
- **Mute timings**: maintenance windows and weekends.

Uncomment and configure after adding your integration credentials. UI changes and file-provisioned resources coexist — file-provisioned resources appear in the UI but cannot be edited there.

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
| `probe_down` | `critical` | A probe fails 3 consecutive checks (see [debouncing](#debouncing) below). |
| `probe_up` | — | A probe transitions from down to up (recovery). |
| `budget_violation` | `warning` | A budget metric becomes newly violated. |
| `cert_expiring` | `warning` or `critical` | A TLS certificate enters the warn or critical expiry window. Severity escalates from warning to critical as the deadline approaches. |

### Debouncing

Native webhooks use a **consecutive-failure threshold** to prevent transient blips from triggering alerts. A `probe_down` notification requires **3 consecutive failures** before it fires. A single success resets the counter. Recovery (`probe_up`) fires immediately when a probe that was notified as down succeeds again.

This works in conjunction with probe-level retries. With the recommended retry policy (`count: 1`), a probe retries once before reporting failure. Combined with the 3-failure threshold, a probe must fail 6 total attempts (3 cycles × 2 attempts each) before triggering an alert — providing strong protection against transient network issues.

Infrastructure errors (DNS resolution failures, connection refused, etc.) are classified as `InfraError` and excluded from state transitions entirely — they don't increment the failure counter or trigger notifications.

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
- Custom notification templates.

For these features, use Grafana alerting.

## 3. Prometheus Alertmanager

Alertmanager provides rule-based routing, grouping, inhibition, and silencing. Technician exposes Prometheus metrics; Prometheus evaluates alert rules (defined in `prometheus/rules.yml`) and sends firing alerts to Alertmanager, which routes them to receivers.

### Setup

Alertmanager is included in `docker-compose.yml`. Configure receivers in `prometheus/alertmanager.yml`. Alertmanager natively supports Slack, PagerDuty, OpsGenie, Email, Telegram, and generic webhooks.

**Discord note**: Alertmanager's Slack attachment format is not compatible with Discord's `/slack` endpoint. To use Alertmanager with Discord, you need the [alertmanager-discord](https://github.com/benjojo/alertmanager-discord) bridge container (commented out in `docker-compose.yml`). For simpler Discord delivery, use native webhooks or Grafana alerting instead.

### Alert rules

Alert rules are defined in `prometheus/rules.yml`. Rules are organized into warn/crit pairs:

- **Warning** — degraded state, routed to chatops (Slack/Discord).
- **Critical** — outage or sustained failure, routed to a pager (PagerDuty/OpsGenie) AND chatops.

#### Anti-flap treatment

Timing-based alerts (Web Vitals, HTTP, TCP, DNS, ICMP RTT, UDP) use `avg_over_time(...[15m])` — a 15-minute rolling average that smooths transient spikes. A single bad check cannot fire an alert; the average must exceed the threshold and the `for` duration must elapse across multiple evaluation cycles.

Critical-severity performance alerts additionally require **>50% of probes** (regions) to exceed the critical threshold. A single flapping region cannot page on-call. In single-region setups this degrades gracefully to per-probe behavior.

Alerts on inherently stable metrics (cert/domain expiry, packet loss percentage, NTP offset) use raw values — smoothing would mask real state changes.

| Category | Warning | Critical |
|----------|---------|----------|
| Probe health (all 13 types) | Single probe failing 3m; >25% error rate 5m | >50% error rate 10m |
| Web Vitals (LCP/INP/CLS) | Google "needs improvement" (15m avg) | Google "poor" threshold, >50% of probes (15m avg) |
| HTTP timing (TTFB/DNS/TLS) | Degraded (15m avg) | Functionally broken, >50% of probes (15m avg) |
| TCP (connect/TLS) | Connect >1s, TLS >1s (15m avg) | Connect >5s / TLS >3s, >50% of probes (15m avg) |
| DNS (probe) | Query >500ms (15m avg) | Query >2s, >50% of probes (15m avg) |
| ICMP | Packet loss >5%, RTT >200ms | Packet loss >25%, RTT >1s (>50% of probes) |
| NTP | Offset >±100ms | Offset >±500ms |
| UDP | RTT >500ms (15m avg) | RTT >2s, >50% of probes (15m avg) |
| TLS cert expiry | <30 days | ≤7 days |
| Domain expiry | <60 days | ≤7 days |
| BGP / TLS invalid / domain gone | — | Immediate (binary failure) |
| SMTP / Traceroute / gRPC | ProbeFailing (warn) | HighErrorRate (crit) |
| Prometheus storage | >80% of 5GB limit | >95% of 5GB limit |

SMTP, Traceroute, and gRPC probes emit only universal metrics (`technician_probe_up`, `technician_probe_duration_seconds`). They receive full probe health coverage (ProbeFailing, HighErrorRate, ProbeInfraError) but do not have probe-specific threshold alerts.

Inhibition rules prevent noise: critical alerts automatically suppress their warning counterparts for the same probe, aggregate error rate warnings suppress individual probe failure warnings, and invalid/gone states suppress expiry alerts (e.g. `TLSCertInvalid` suppresses `TLSCertExpiringSoon` and `TLSCertExpiryCritical`, `DomainNotRegistered` suppresses both domain expiry tiers).

A `TestAlert` rule (commented out) can be uncommented to validate the full pipeline.

### Route structure

Alertmanager routes alerts using a pager fan-out combined with category-based receivers:

1. **Pager fan-out** — all critical alerts go to the `pager` receiver with `continue: true`, so they also reach the matching category route below.
2. **Category routing** — alerts are routed to a receiver based on their type:

| Receiver | Alerts | Repeat interval |
|----------|--------|-----------------|
| `pager` | All critical alerts (fan-out) | 1h |
| `infrastructure` | ProbeFailing, HighErrorRate, TLSCertInvalid, BGP alerts, DomainNotRegistered, PrometheusWALCorruptions | 1h |
| `expiry` | TLSCertExpiringSoon/Critical, DomainExpiringSoon/Critical | 12h |
| `performance` | All timing/vitals/resource alerts, BudgetViolation | 4h |
| `chatops` | Everything else (ProbeInfraError, PrometheusStorageHigh, any new rules) | 4h |

Each receiver has commented integration examples in `alertmanager.yml`. A critical infrastructure alert is delivered to both `pager` and `infrastructure`. A warning performance alert goes only to `performance`.

### Notification templates

Reusable Go templates in `prometheus/templates/technician.tmpl` provide consistent message formatting across receivers:

| Template | Use with |
|----------|----------|
| `technician.title` | Any receiver — `[FIRING:2] ProbeFailing — example.com` |
| `technician.text` | Any receiver — lists all alerts with annotations |
| `technician.slack.title` / `.slack.text` | Slack — mrkdwn formatting with bold and quotes |
| `technician.email.subject` / `.email.html` | Email — HTML table with Alertmanager link |

Reference templates in receiver configs:

```yaml
slack_configs:
  - api_url: 'https://hooks.slack.com/services/...'
    channel: '#alerts'
    title: '{{ template "technician.slack.title" . }}'
    text: '{{ template "technician.slack.text" . }}'
```

### Silencing / acknowledgement

To suppress alerts while investigating, use Alertmanager silences:

- **UI**: `http://localhost:9093/#/silences` — create a silence with label matchers and a duration.
- **CLI**: `amtool silence add alertname=ProbeFailing name=myprobe --duration=2h`
- **API**: `POST /api/v2/silences` — useful for chatops bot integration (e.g. react-to-silence).

PagerDuty/OpsGenie ACKs stop escalation on their side but do not silence Alertmanager — create an Alertmanager silence as well to stop both. For chatops-driven silencing, see [karma](https://github.com/prymitive/karma) or build a bot that calls the Alertmanager silence API.

### Mute time intervals

Three time intervals are defined in `alertmanager.yml` for use with routes:

| Name | Window | Use case |
|------|--------|----------|
| `maintenance` | Tuesday 02:00–04:00 UTC | Silence during deploys |
| `weekends` | Saturday–Sunday all day | Skip non-critical weekend noise |
| `business-hours` | Mon–Fri 09:00–18:00 UTC | Only page during work hours |

Intervals are inert definitions until a route references them. Add `mute_time_intervals` or `active_time_intervals` to a route:

```yaml
routes:
  - matchers:
      - severity = critical
    receiver: pager
    mute_time_intervals: ['maintenance']      # silence pager during deploys
    active_time_intervals: ['business-hours'] # or only page during work hours
```

Adjust days and times to match your schedule. All times are UTC.

## Comparison

| Feature | Native webhooks | Grafana alerting | Alertmanager |
|---------|----------------|------------------|--------------|
| Setup complexity | Minimal (config only) | Low (UI or YAML) | Moderate (YAML + containers) |
| Severity routing | Yes (`severities` filter) | Yes (notification policies) | Yes (pager fan-out) |
| Category routing | No | Yes (file or UI) | Yes (named receivers) |
| Discord support | Native | Native | Requires bridge container |
| Slack support | Native | Native | Native |
| Grouping / dedup | Per-probe cooldown | Full | Full |
| Inhibit rules | No | No | Yes (4 pre-configured) |
| Silencing | No | Yes (UI) | Yes (API/UI) |
| Mute timings | No | Yes (file or UI) | Yes (time intervals) |
| Notification templates | No | Yes (Go templates) | Yes (`technician.tmpl`) |
| Escalation | No | Yes | Yes |
| Alert history | No | Yes | No |
| File provisioning | N/A | Yes (`grafana-alerting.yml`) | Yes (`alertmanager.yml`) |
| Runs standalone | Yes | Needs Grafana + Prometheus | Needs Prometheus |
