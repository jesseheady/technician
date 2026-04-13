#!/usr/bin/env bash
# Create an Alertmanager silence.
#
# Usage:
#   ./scripts/silence.sh                           # silence all alerts (1 hour)
#   ./scripts/silence.sh "alertname=CheckFailing"  # silence by alertname (exact)
#   ./scripts/silence.sh "name=~jesseheady.*"      # silence by label (regex)
#   ./scripts/silence.sh "severity=critical" 2h    # silence severity critical for 2 hours
#   ./scripts/silence.sh "" 4h                     # silence all for 4 hours
#
# Duration supports: Ns, Nm, Nh, Nd (default: 1h)
# Alertmanager address: set AM=host:port (default: localhost:9093)

set -euo pipefail

AM="${AM:-localhost:9093}"
PATTERN="${1:-}"
DURATION="${2:-1h}"
CREATED_BY="${USER:-operator}"

# Parse duration to seconds
parse_duration() {
  local dur="$1"
  local num="${dur%[smhd]*}"
  local unit="${dur##*[0-9]}"
  case "$unit" in
    s) echo "$num" ;;
    m) echo $((num * 60)) ;;
    h) echo $((num * 3600)) ;;
    d) echo $((num * 86400)) ;;
    *) echo >&2 "Invalid duration unit: $unit (use s, m, h, or d)"; exit 1 ;;
  esac
}

NOW=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
SECS=$(parse_duration "$DURATION")
# macOS date
END=$(date -u -v+"${SECS}S" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null \
  || date -u -d "+${SECS} seconds" +"%Y-%m-%dT%H:%M:%SZ")

# Build matcher JSON
if [ -z "$PATTERN" ]; then
  MATCHER='{"name":"alertname","value":".+","isRegex":true,"isEqual":true}'
  DESC="all alerts"
else
  LABEL="${PATTERN%%=*}"
  REST="${PATTERN#*=}"
  IS_REGEX=false
  if [[ "$REST" == "~"* ]]; then
    IS_REGEX=true
    REST="${REST#\~}"
  fi
  MATCHER=$(printf '{"name":"%s","value":"%s","isRegex":%s,"isEqual":true}' \
    "$LABEL" "$REST" "$IS_REGEX")
  DESC="$PATTERN"
fi

COMMENT="Silence ${DESC} for ${DURATION}"

CODE=$(curl -s -o /tmp/silence-response.json -w "%{http_code}" \
  -X POST "http://${AM}/api/v2/silences" \
  -H "Content-Type: application/json" \
  -d "$(printf '{
    "matchers": [%s],
    "startsAt": "%s",
    "endsAt": "%s",
    "createdBy": "%s",
    "comment": "%s"
  }' "$MATCHER" "$NOW" "$END" "$CREATED_BY" "$COMMENT")")

if [ "$CODE" = "200" ]; then
  SID=$(python3 -c "import json; print(json.load(open('/tmp/silence-response.json'))['silenceID'])")
  echo "Silenced: ${DESC}"
  echo "Duration: ${DURATION} (until ${END})"
  echo "ID:       ${SID}"
  echo ""
  echo "To expire early:"
  echo "  curl -X DELETE http://${AM}/api/v2/silence/${SID}"
else
  echo "Failed (HTTP ${CODE}):"
  cat /tmp/silence-response.json
  exit 1
fi
