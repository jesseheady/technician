#!/usr/bin/env bash
# maintenance.sh — create an Alertmanager silence during stack warmup.
# Called automatically by the warmup-silence service in docker-compose.
# Can also be run manually: ./scripts/maintenance.sh [-d MINUTES]
set -euo pipefail

ALERTMANAGER_URL="${ALERTMANAGER_URL:-http://localhost:9093}"
DURATION="${SILENCE_DURATION:-20}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -d|--duration) DURATION="$2"; shift 2 ;;
    -u|--url) ALERTMANAGER_URL="$2"; shift 2 ;;
    *) shift ;;
  esac
done

# Wait for Alertmanager to become ready.
echo "waiting for alertmanager at ${ALERTMANAGER_URL} ..."
retries=60
until curl -sf "${ALERTMANAGER_URL}/-/healthy" >/dev/null 2>&1; do
  retries=$((retries - 1))
  if [[ $retries -le 0 ]]; then
    echo "error: alertmanager not reachable" >&2
    exit 1
  fi
  sleep 1
done

now=$(date -u +"%Y-%m-%dT%H:%M:%S.000Z")
# GNU date (Linux) and BSD date (macOS) differ — try both.
ends_at=$(date -u -v+"${DURATION}M" +"%Y-%m-%dT%H:%M:%S.000Z" 2>/dev/null \
  || date -u -d "+${DURATION} minutes" +"%Y-%m-%dT%H:%M:%S.000Z")

response=$(curl -sf -X POST \
  "${ALERTMANAGER_URL}/api/v2/silences" \
  -H "Content-Type: application/json" \
  -d "{
    \"matchers\": [{
      \"name\": \"alertname\",
      \"value\": \".+\",
      \"isRegex\": true,
      \"isEqual\": true
    }],
    \"startsAt\": \"${now}\",
    \"endsAt\": \"${ends_at}\",
    \"createdBy\": \"maintenance.sh\",
    \"comment\": \"Stack restart — silencing alerts during ${DURATION}m warmup\"
  }")

silence_id=$(echo "${response}" | grep -o '"silenceID":"[^"]*"' | cut -d'"' -f4)
echo "silence created: ${silence_id} (expires in ${DURATION}m)"
