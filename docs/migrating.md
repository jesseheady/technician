# Migrating between versions

When Technician ships breaking changes to config fields, metric label names, or default ports, existing data in Prometheus and the local status store won't automatically follow. This guide covers what to expect and how to handle the transition cleanly.

## What breaks on a label or config rename

Prometheus indexes every time series by its full label set. If a label name changes (say `regional` becomes `region`), Prometheus treats the new label as an entirely new series. The old series stops receiving data and ages out after your configured retention period. Any dashboards, alert rules, or recording rules that reference the old label name will silently return empty results until you update them.

The local status store (`status.json`) has a similar issue. Check results carry their labels as a `map[string]string`. When Technician restarts with new label names, it reads the old snapshot and tries to extract labels by the new keys — getting empty strings instead of the actual values. This doesn't crash anything, but metrics recorded from stale results will have blank label values until fresh check runs replace them.

## Before you upgrade

1. **Check the changelog or diff** for any renamed config fields, metric labels, default ports, or Prometheus rule names.
2. **Update your config files** (`technician.yml`, `checks/*.yml`, `budgets.yml`) to match the new field names. If you're using example configs as a base, re-copy from `examples/` and re-apply your customizations.
3. **Update Prometheus rules and scrape config** — recording rules, alert rules, and scrape targets may reference renamed labels, rule names, or ports. Diff your `prometheus/rules.yml` and `prometheus/prometheus.yml` against the new versions.
4. **Update Grafana dashboards** — re-import from `dashboards/*.json` or update template variables and PromQL queries manually.

## Cutting over

### Clean-slate approach (simplest)

If you don't need historical continuity across the version boundary — for instance, if you're early in a deployment or your retention window is short — just wipe and restart:

```bash
# Stop the worker
# Delete the local status store
rm ${TECHNICIAN_DATA_DIR:-/var/lib/technician}/status.json
rm ${TECHNICIAN_DATA_DIR:-/var/lib/technician}/status.json.bak.*

# Optionally reset Prometheus if you want a fully clean slate
# (only do this if you don't care about historical data)
# docker compose down -v  # removes Prometheus volume

# Start with the new binary and updated configs
docker compose up --build
```

Old Prometheus series will stop updating and expire after your retention period (default 90 days). New series under the new labels will start populating immediately.

### Preserving historical data

If you need dashboards to show both old and new data during the transition, add temporary recording rules that alias the old label names to the new ones. For example, if `site_code` was renamed to `region`:

```yaml
# Add to prometheus/rules.yml temporarily
- record: technician_check_up_compat
  expr: |
    technician_check_up
    or
    label_replace(
      technician_check_up{site_code!=""},
      "region", "$1", "site_code", "(.*)"
    )
```

Point your dashboards at the `_compat` series during the transition window. Once the old series expire (after your retention period), remove the compat rules and point dashboards back at the original metric names.

### Port changes

If the default listen port changes, update every reference:

- `technician.yml` — `metrics.prometheus.listen`
- `prometheus.yml` — scrape target address
- `docker-compose.yml` — port mapping
- Firewall rules — inbound allowlist
- Any load balancer or service discovery config pointing at the old port

The worker won't start on the old port automatically — if your Prometheus scrape config still points at the old port, you'll see scrape failures immediately, which is a clear signal to update.

### Webhook and alert rule renames

If a Prometheus alert name changes (e.g., `ProbeDown` → `CheckFailing`), update:

- `prometheus/rules.yml` — the alert definition
- `prometheus/alertmanager.yml` — any `match` or `inhibit_rules` referencing the old alert name
- Notification templates or routing rules that key on `alertname`

In-flight alerts under the old name will auto-resolve once Prometheus evaluates the updated rules (the old alert expression no longer exists, so it stops firing).

## Version-specific migrations

### v0.2.0 → v0.3.0

**Config field renames:**
- `geohash` → `location_hash` (in site definitions)
- `provider` → `infra_provider` (in site definitions)

**Metric label renames:**
- `site_code` → `region`
- `site_city` → `city`
- `site_country` → `country`

**Prometheus rule renames:**
- Recording rule `technician:check_down_fraction` → `technician:failure_ratio`
- Recording rule `technician:check_uptime_daily` → `technician:daily_availability`
- Alert `ProbeDown` → `CheckFailing` (also changed `for:` from 5m to 3m)

**Action required:**
1. Update `technician.yml` site fields (`location_hash`, `infra_provider`).
2. Ensure `prometheus/prometheus.yml` scrape target uses port `9590`.
3. Ensure `docker-compose.yml` port mapping is `9590:9590`.
4. Replace `prometheus/rules.yml` with the new version.
5. Update `prometheus/alertmanager.yml` inhibit rules (`CheckFailing`).
6. Re-import Grafana dashboards from `dashboards/`.
7. Delete `status.json` and backups before restarting the worker.
