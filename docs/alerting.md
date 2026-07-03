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
   - `technician_check_up == 0` — check is down
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
    events: [check_down, check_up, cert_expiring, budget_violation]
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
    events: [check_down, cert_expiring]
    severities: [critical]
    cooldown: 10m
```

| Field | Description |
|-------|-------------|
| `url` | Webhook endpoint URL. |
| `type` | `discord`, `slack`, or `generic`. Determines payload format. |
| `events` | Which events trigger notifications. Omit for all events. |
| `severities` | Filter by severity: `warning`, `critical`. Omit to receive all severities. |
| `cooldown` | Minimum interval between repeated notifications for the same check+event. Default `5m`. |

### Event types

| Event | Default severity | Fires when |
|-------|-----------------|------------|
| `check_down` | `critical` | A check fails 3 consecutive checks (see [debouncing](#debouncing) below). |
| `check_up` | — | A check transitions from down to up (recovery). |
| `budget_violation` | `warning` | A budget metric becomes newly violated. |
| `cert_expiring` | `warning` or `critical` | A TLS certificate enters the warn or critical expiry window. Severity escalates from warning to critical as the deadline approaches. |

### Debouncing

Native webhooks use a **consecutive-failure threshold** to prevent transient blips from triggering alerts. A `check_down` notification requires **3 consecutive failures** before it fires. A single success resets the counter. Recovery (`check_up`) fires immediately when a check that was notified as down succeeds again.

This works in conjunction with check-level retries. With the recommended retry policy (`count: 1`), a check retries once before reporting failure. Combined with the 3-failure threshold, a check must fail 6 total attempts (3 cycles × 2 attempts each) before triggering an alert — providing strong protection against transient network issues.

Infrastructure errors (DNS resolution failures, connection refused, etc.) are classified as `InfraError` and excluded from state transitions entirely — they don't increment the failure counter or trigger notifications.

### Severity routing

Events carry a severity level that enables routing to different channels:

- **`warning`** — Informational. TLS cert expiring within `warn_days` (default 30), budget violations. Good for Slack or a low-priority channel.
- **`critical`** — Actionable. Check failures, TLS cert within `critical_days` (default 7). Route to PagerDuty, OpsGenie, or an on-call channel.

Configure multiple webhook entries with different `severities` filters to fork notifications. For example, one Slack channel receives all alerts while PagerDuty only receives critical.

The `cert_expiring` event uses state tracking: it fires once when entering the warning window, then again if it escalates to critical. It does not re-fire on every check cycle. If the cert is renewed (days remaining goes above the warn threshold), the state resets so future expiry will trigger again.

### Payload formats

- **Discord**: Native embed format with color-coded status (red for critical, amber for warning, green for recovery), check details, error info, and cert expiry fields.
- **Slack**: Incoming webhook format with colored attachments and `[WARNING]`/`[CRITICAL]` severity prefix.
- **Generic**: JSON object with `type`, `severity`, `check`, `probe_type`, `message`, `details`, and `timestamp` fields.

### Limitations vs Grafana

Native webhooks are intentionally simple. They do not provide:

- Alert grouping or deduplication beyond the per-check cooldown.
- Silencing or maintenance windows.
- Escalation policies.
- Alert history or state timeline.
- Custom notification templates.

For these features, use Grafana alerting.

## 3. Prometheus Alertmanager

Alertmanager provides rule-based routing, grouping, inhibition, and silencing. Technician exposes Prometheus metrics; Prometheus evaluates alert rules (defined in `prometheus/rules.yml`) and sends firing alerts to Alertmanager, which routes them to receivers.

### Setup

Alertmanager is included in `docker-compose.yml`. Receiver configuration lives in `prometheus/alertmanager.yml`, where all notification channels ship commented out. To enable channels:

1. Copy to your local config: `cp prometheus/alertmanager.yml config/alertmanager.yml`
2. Uncomment the `*_configs` blocks you need in `config/alertmanager.yml`.
3. Set the corresponding env vars in `.env` (see `.env.example` for setup instructions).
4. Create `docker-compose.override.yml` to mount your local config:
   ```yaml
   services:
     alertmanager:
       volumes:
         - ./config/alertmanager.yml:/etc/alertmanager/alertmanager.yml.tmpl:ro
         - ./prometheus/templates:/etc/alertmanager/templates:ro
   ```
5. Restart: `docker compose restart alertmanager`

Environment variables in `config/alertmanager.yml` use `${VAR}` syntax and are substituted automatically at container start — adding new channels requires no changes to `docker-compose.yml`.

Pre-configured receiver blocks are available for Discord, Slack, Pushover, PagerDuty, OpsGenie, VictorOps, Telegram, Microsoft Teams, Email, and generic webhooks.

### Alert rules

Alert rules are defined in `prometheus/rules.yml`. Rules are organized into warn/crit pairs:

- **Warning** — degraded state, routed to chatops (Slack/Discord).
- **Critical** — outage or sustained failure, routed to a pager (PagerDuty/OpsGenie) AND chatops.

#### Anti-flap treatment

Timing-based alerts (Web Vitals, HTTP, TCP, DNS, ICMP RTT, UDP) use `avg_over_time(...[15m])` — a 15-minute rolling average that smooths transient spikes. A single bad check cannot fire an alert; the average must exceed the threshold and the `for` duration must elapse across multiple evaluation cycles.

Critical-severity performance alerts additionally require **>50% of checks** (regions) to exceed the critical threshold. A single flapping region cannot page on-call. In single-region setups this degrades gracefully to per-check behavior.

Alerts on inherently stable metrics (cert/domain expiry, packet loss percentage, NTP offset) use raw values — smoothing would mask real state changes.

| Category | Warning | Critical |
|----------|---------|----------|
| Check health (all 13 types) | Single check failing 3m; >25% error rate 5m | >50% error rate 10m |
| Web Vitals (LCP/INP/CLS) | Google "needs improvement" (15m avg) | Google "poor" threshold, >50% of checks (15m avg) |
| HTTP timing (TTFB/DNS/TLS) | Degraded (15m avg) | Functionally broken, >50% of checks (15m avg) |
| TCP (connect/TLS) | Connect >1s, TLS >1s (15m avg) | Connect >5s / TLS >3s, >50% of checks (15m avg) |
| DNS (check) | Query >500ms (15m avg) | Query >2s, >50% of checks (15m avg) |
| ICMP | Packet loss >5%, RTT >200ms | Packet loss >25%, RTT >1s (>50% of checks) |
| NTP | Offset >±100ms | Offset >±500ms |
| UDP | RTT >500ms (15m avg) | RTT >2s, >50% of checks (15m avg) |
| TLS cert expiry | <30 days | ≤7 days |
| Domain expiry | <60 days | ≤7 days |
| BGP / TLS invalid / domain gone | — | Immediate (binary failure) |
| SMTP / Traceroute / gRPC | CheckFailing (warn) | HighErrorRate (crit) |
| Prometheus storage | >80% of 5GB limit | >95% of 5GB limit |

SMTP, Traceroute, and gRPC checks emit only universal metrics (`technician_check_up`, `technician_check_duration_seconds`). They receive full check health coverage (CheckFailing, HighErrorRate, CheckInfraError) but do not have check-specific threshold alerts.

Inhibition rules prevent noise: critical alerts automatically suppress their warning counterparts for the same check, aggregate error rate warnings suppress individual check failure warnings, and invalid/gone states suppress expiry alerts (e.g. `TLSCertInvalid` suppresses `TLSCertExpiringSoon` and `TLSCertExpiryCritical`, `DomainNotRegistered` suppresses both domain expiry tiers).

#### Staleness grace period

After a data gap — process restart, connectivity loss, or host downtime — the first check results carry inflated latencies: stale DNS caches, cold TCP connections, and NTP clock drift. These spike the 15-minute rolling averages and would trip the timing alerts for ~10 minutes before the averages re-stabilize. They are artifacts, not real degradation.

Technician exposes `technician_last_run_timestamp_seconds`, which advances on every recorded (non-infra) result. It freezes both when the worker stops (nothing scraped) and when checks run but keep failing to reach their target (connectivity loss), so `technician:seconds_since_last_run` grows in every gap type. The `TechnicianDataGap` alert fires when a gap over 5 minutes occurred anywhere in the last 10 minutes (`max_over_time`), so it stays firing across the post-resume stabilization window rather than clearing the instant data returns. While it fires, an inhibit rule suppresses the whole latency/timing alert family (Web Vitals, HTTP/TCP timing, DNS, ICMP RTT + packet loss, NTP offset, UDP) across every check and region. Non-timing alerts — check down, cert/domain expiry, BGP, budgets — still fire, because a data gap does not make those false.

`TechnicianDataGap` carries `severity: none` and routes to a blackhole receiver: it reaches Alertmanager to drive inhibition but notifies nobody. Rule logic is covered by `prometheus/rules_test.yml` (`promtool test rules`).

Commented-out `TestAlertWarn` and `TestAlertCritical` rules can be uncommented to validate the full pipeline. Alternatively, `scripts/test-alerts.sh` posts test alerts directly to the Alertmanager API, bypassing Prometheus evaluation timing. See the inline comments in `prometheus/rules.yml` for step-by-step instructions.

### Route structure

Alertmanager routes alerts using a pager fan-out combined with category-based receivers:

1. **Pager fan-out** — all critical alerts go to the `pager` receiver with `continue: true`, so they also reach the matching category route below.
2. **Category routing** — alerts are routed to a receiver based on their type:

| Receiver | Alerts | Repeat interval |
|----------|--------|-----------------|
| `pager` | All critical alerts (fan-out) | 1h |
| `infrastructure` | CheckFailing, HighErrorRate, TLSCertInvalid, BGP alerts, DomainNotRegistered, PrometheusWALCorruptions | 1h |
| `expiry` | TLSCertExpiringSoon/Critical, DomainExpiringSoon/Critical | 12h |
| `performance` | All timing/vitals/resource alerts, BudgetViolation | 4h |
| `chatops` | Everything else (CheckInfraError, PrometheusStorageHigh, any new rules) | 4h |
| `blackhole` | `TechnicianDataGap` (inhibit-only; notifies nobody) | — |

Each receiver has commented integration examples in `alertmanager.yml`. A critical infrastructure alert is delivered to both `pager` and `infrastructure`. A warning performance alert goes only to `performance`.

### Notification templates

Reusable Go templates in `prometheus/templates/technician.tmpl` provide consistent message formatting across receivers:

| Template | Use with |
|----------|----------|
| `technician.title` | Any receiver — `ALERT - CheckFailing / example.com` |
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
- **CLI**: `amtool silence add alertname=CheckFailing name=myprobe --duration=2h`
- **Script**: `scripts/silence.sh "alertname=CheckFailing" 2h` — create silences by pattern and duration.
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

### Maintenance windows

Maintenance suppression lives here, in Alertmanager — Technician deliberately does **not** implement its own maintenance flag (see [#22](https://github.com/jesseheady/technician/issues/22) for the rationale). Checks keep running and recording true state during maintenance; only alerting is suppressed. Pick the mechanism by whether the window is planned or ad-hoc:

- **Planned / recurring** → a **mute time interval** (above). Define the window once and reference it from the routes you want quieted — declarative and version-controlled alongside the rest of your alerting config.
- **Ad-hoc / right now** → a **silence** (above). Use the **comment field as the maintenance note** (who, why, ticket) so the reason is visible in the Alertmanager UI:

  ```bash
  amtool silence add group=payments --duration=1h \
    --comment="maintenance: payments deploy (JIRA-1234)"
  ```

Scope a window with label matchers — `group="payments"` covers every check in that group, `name="myprobe"` a single check.

Both forms **auto-expire** — a mute interval by its time window, a silence by its `--duration`. That is the safety property: a forgotten window can't suppress real outages indefinitely the way a stuck boolean flag could. Prefer a bounded duration over an open-ended one.

## Comparison

| Feature | Native webhooks | Grafana alerting | Alertmanager |
|---------|----------------|------------------|--------------|
| Setup complexity | Minimal (config only) | Low (UI or YAML) | Moderate (YAML + containers) |
| Severity routing | Yes (`severities` filter) | Yes (notification policies) | Yes (pager fan-out) |
| Category routing | No | Yes (file or UI) | Yes (named receivers) |
| Discord support | Native | Native | Native |
| Slack support | Native | Native | Native |
| Grouping / dedup | Per-check cooldown | Full | Full |
| Inhibit rules | No | No | Yes (5 pre-configured) |
| Silencing | No | Yes (UI) | Yes (API/UI) |
| Mute timings | No | Yes (file or UI) | Yes (time intervals) |
| Notification templates | No | Yes (Go templates) | Yes (`technician.tmpl`) |
| Escalation | No | Yes | Yes |
| Alert history | No | Yes | No |
| File provisioning | N/A | Yes (`grafana-alerting.yml`) | Yes (`alertmanager.yml`) |
| Runs standalone | Yes | Needs Grafana + Prometheus | Needs Prometheus |
