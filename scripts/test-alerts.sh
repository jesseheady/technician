#!/usr/bin/env bash
# Test the full WARN → CRIT → RESOLVED alert pipeline by posting
# directly to the Alertmanager API.  No Prometheus rules needed.
#
# Usage:
#   ./scripts/test-alerts.sh              # default: localhost:9093
#   ./scripts/test-alerts.sh host:port    # custom Alertmanager address

set -euo pipefail

AM="${1:-localhost:9093}"
URL="http://${AM}/api/v2/alerts"
NOW=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

post_alert() {
  local severity="$1" status="$2" end_time="${3:-}"
  local end_field=""
  [ -n "$end_time" ] && end_field="\"endsAt\": \"${end_time}\","

  curl -s -o /dev/null -w "%{http_code}" -X POST "$URL" \
    -H "Content-Type: application/json" \
    -d "[{
      \"labels\": {
        \"alertname\": \"TestPipeline\",
        \"name\": \"test-alert\",
        \"severity\": \"${severity}\"
      },
      \"annotations\": {
        \"summary\": \"Test alert (${severity}) - pipeline validation\",
        \"description\": \"End-to-end test of the ${severity} alert path. Safe to ignore.\"
      },
      \"startsAt\": \"${NOW}\",
      ${end_field}
      \"generatorURL\": \"http://localhost:9090/graph\"
    }]"
}

step() {
  echo ""
  echo "──────────────────────────────────────────"
  echo "  $1"
  echo "──────────────────────────────────────────"
}

wait_for_enter() {
  echo ""
  read -rp "  Press Enter when verified (or Ctrl-C to abort)... "
}

step "Step 1: Firing WARNING alert"
code=$(post_alert warning firing)
echo "  POST $URL → HTTP $code"
echo ""
echo "  Expected: Discord #chatops channel only"
wait_for_enter

step "Step 2: Escalating to CRITICAL alert"
code=$(post_alert critical firing)
echo "  POST $URL → HTTP $code"
echo ""
echo "  Expected:"
echo "    - Discord #chatops channel"
echo "    - Discord #incidents channel"
echo "    - Pushover notification (emergency priority)"
wait_for_enter

step "Step 3: Resolving both alerts"
RESOLVED=$(date -u -v+1S +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null \
  || date -u -d "+1 second" +"%Y-%m-%dT%H:%M:%SZ")
code_warn=$(post_alert warning resolved "$RESOLVED")
code_crit=$(post_alert critical resolved "$RESOLVED")
echo "  POST warning  → HTTP $code_warn"
echo "  POST critical → HTTP $code_crit"
echo ""
echo "  Expected: resolved notifications in all channels above"
wait_for_enter

step "Done! Pipeline validated."
