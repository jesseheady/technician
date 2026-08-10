#!/usr/bin/env bash
# init.sh – One-time project setup for local development.
# Works on macOS, Linux, and Windows (WSL). Detects the platform and prints
# the matching install hints; the build itself is identical everywhere.
# Usage: ./scripts/init.sh [--stack]
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

# --- Platform detection ---
# PLATFORM drives only the human-readable install hints below; the build steps
# are identical across macOS, Linux, and WSL.
PLATFORM="unknown"
MTR_HINT="install mtr with your package manager"
TRIVY_HINT="see https://trivy.dev/latest/getting-started/installation/"
SHELLCHECK_HINT="install shellcheck with your package manager"
GOLANGCI_HINT="see https://golangci-lint.run/welcome/install/"
case "$(uname -s)" in
  Darwin)
    PLATFORM="macOS"
    MTR_HINT="brew install mtr"
    TRIVY_HINT="brew install trivy"
    SHELLCHECK_HINT="brew install shellcheck"
    GOLANGCI_HINT="brew install golangci-lint"
    ;;
  Linux)
    if grep -qiE "(microsoft|wsl)" /proc/version 2>/dev/null; then
      PLATFORM="WSL"
    else
      PLATFORM="Linux"
    fi
    if command -v apt-get &>/dev/null; then
      MTR_HINT="sudo apt-get install mtr-tiny"
      SHELLCHECK_HINT="sudo apt-get install shellcheck"
    elif command -v dnf &>/dev/null; then
      MTR_HINT="sudo dnf install mtr"
      SHELLCHECK_HINT="sudo dnf install ShellCheck"
    elif command -v pacman &>/dev/null; then
      MTR_HINT="sudo pacman -S mtr"
      SHELLCHECK_HINT="sudo pacman -S shellcheck"
    fi
    ;;
esac
info "Platform: $PLATFORM"

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

if ! go list -m github.com/jesseheady/technician &>/dev/null; then
  info "Installing Go dependencies..."
  go mod download
fi

info "Building technician..."
CGO_ENABLED=0 go build -o technician .
info "Build OK: ./technician"

if command -v mtr &>/dev/null; then
  info "mtr found (traceroute checks supported)"
else
  warn "mtr not found. Traceroute checks will fail. Install with: $MTR_HINT"
fi

if command -v trivy &>/dev/null; then
  info "trivy found (hook scans run natively, no Docker needed)"
else
  warn "trivy not found. Hook scans will fall back to Docker. Install with: $TRIVY_HINT"
fi

if command -v shellcheck &>/dev/null; then
  info "shellcheck found"
else
  warn "shellcheck not found. Staging shell scripts will fall back to Docker. Install with: $SHELLCHECK_HINT"
fi

if command -v golangci-lint &>/dev/null; then
  info "golangci-lint found"
else
  warn "golangci-lint not found. Lint hook will fall back to Docker. Install with: $GOLANGCI_HINT"
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
    # Native binaries make the matching container images dead weight.
    hook_images="prom/prometheus:latest prom/alertmanager:latest"
    command -v trivy &>/dev/null || hook_images="aquasec/trivy:latest $hook_images"
    command -v shellcheck &>/dev/null || hook_images="koalaman/shellcheck:stable $hook_images"
    command -v golangci-lint &>/dev/null || hook_images="golangci/golangci-lint:v2.12.2 $hook_images"
    for image in $hook_images; do
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
  info "Or start full stack: ./scripts/init.sh --stack"
fi
