#!/usr/bin/env bash
# init-mac.sh – One-time project setup for local development on macOS.
# Usage: ./scripts/init-mac.sh [--stack]
#   --stack  Start docker compose (Technician + Prometheus + Grafana) after setup.

set -e
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
err()   { echo -e "${RED}[ERR]${NC} $*"; }

# --- Checks ---
start_stack=0
for arg in "$@"; do
  case "$arg" in
    --stack) start_stack=1 ;;
  esac
done

if ! command -v go &>/dev/null; then
  err "Go is required. Install from https://go.dev/dl/"
  exit 1
fi
info "Go: $(go version)"

if ! go list -m github.com/m0nkey/technician &>/dev/null; then
  info "Installing Go dependencies..."
  go mod download
fi

info "Building technician..."
CGO_ENABLED=0 go build -o technician .
info "Build OK: ./technician"

if command -v mtr &>/dev/null; then
  info "mtr found (traceroute checks supported)"
else
  warn "mtr not found. Traceroute checks will fail. Install with: brew install mtr"
fi

if command -v node &>/dev/null; then
  info "Node: $(node -v)"
  info "Installing Playwright + Chromium for browser checks..."
  (cd internal/playwright/scripts && npm ci && npx playwright install chromium) 2>&1 | tail -1
  info "Playwright ready"
else
  warn "Node not found. Playwright checks need Node.js (or run via Docker)."
fi

if command -v docker &>/dev/null && command -v docker compose &>/dev/null; then
  info "Docker and Docker Compose found"
else
  warn "Docker / Docker Compose not found. Use 'docker compose up' for full stack (Prometheus + Grafana)."
fi

# --- Git hooks ---
if [ -d .githooks ]; then
  git config core.hooksPath .githooks
  info "Git hooks configured (.githooks/pre-commit, .githooks/pre-push)"

  # Pre-pull images used by the hooks so the first commit/push doesn't
  # silently stall on lazy pulls. Skipped if Docker isn't running.
  if command -v docker &>/dev/null && docker info &>/dev/null; then
    info "Pre-pulling Docker images used by git hooks (one-time)..."
    for image in aquasec/trivy:latest prom/prometheus:latest prom/alertmanager:latest koalaman/shellcheck:stable; do
      if docker pull --quiet "$image" >/dev/null; then
        info "  pulled $image"
      else
        warn "  failed to pull $image"
      fi
    done
  else
    warn "Docker not running — git hooks will lazy-pull images on first commit (slow)."
  fi
fi

# --- Optional: create local dev dirs ---
mkdir -p /tmp/technician/artifacts /tmp/technician-videos 2>/dev/null || true

# --- Optional: start stack ---
if [[ "$start_stack" -eq 1 ]]; then
  if ! command -v docker &>/dev/null || ! command -v docker compose &>/dev/null; then
    err "Cannot start stack: Docker and Docker Compose are required."
    exit 1
  fi
  info "Starting Technician + Prometheus + Grafana..."
  docker compose up -d
  echo ""
  info "Technician: http://localhost:9590/metrics"
  info "Prometheus: http://localhost:9090"
  info "Grafana:    http://localhost:3000 (admin/admin if default)"
else
  echo ""
  info "Run the worker: ./technician worker --config config/technician.yml"
  info "(First time? Copy examples: cp -r examples/ config/)"
  info "Or start full stack: ./scripts/init-mac.sh --stack"
fi
