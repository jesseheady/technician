# Documentation

| Document | Description |
|----------|-------------|
| [Getting started](getting-started.md) | Prerequisites, one-time setup (Mac init script), and how to run the worker and full stack. |
| [Alerting](alerting.md) | Native webhooks (Discord, Slack, generic), Grafana alerting (recommended), and Alertmanager. |
| [Testing and e2e](testing-and-e2e.md) | Unit tests, `go test`/vet, and end-to-end validation (validate command, metrics scrape, Docker stack, CI). |
| [Local development](mock-production.md) | Run Technician locally with full metrics/dashboards while probing mock local endpoints. |
| [Roadmap](roadmap.md) | Near-term MVP (WebSocket, structured logging, status page redesign), scope principles, future enhancements, and recently completed work. |
| [Proposals](proposals/) | Design and rollout plans. |
| [Proposal: Cloudflare Workers](proposals/cloudflare-workers.md) | Plan to support deploying check runners as Cloudflare Workers (and alignment with AWS Lambda Edge). |
| [Proposal: Site identifiers (edge)](proposals/site-identifiers-edge.md) | How to define site/location when checks run on Workers or Lambda (provider location codes, metrics labels). |
| [Central Prometheus and Grafana](architecture/central-prometheus-grafana.md) | Config for central Prometheus (private VPC) and Grafana as source of record; local/Docker vs central reporting; edge push. |
| [Core Web Vitals](core-web-vitals.md) | LCP, INP, CLS; thresholds, collection, network throttling (4G/3G/slow-3G), and device emulation. |
| [Deployment sizing](deployment-sizing.md) | Resource requirements, cost estimates, and sizing by deployment mode (VPS, Lambda, Workers, full stack). |
| [Multi-target deployment](multi-target-deployment.md) | Serve many workers from one checks directory with `check_filter` (types/groups/tags) and the `--types`/`--groups`/`--tags` flags. |
| [CI](ci.md) | GitHub Actions workflow, generic CI pipelines (GitLab, CircleCI, Jenkins), budget validation, Playwright in CI. |
| [Playwright scaling](playwright-scaling.md) | Browser check resource analysis, concurrency controls (`max_browsers`), and dedicated runner architecture (stages 1-4). |
| [Migrating between versions](migrating.md) | Breaking changes, label renames, port changes, Prometheus/Grafana cutover, and version-specific migration steps. |

See also [AGENTS.md](../AGENTS.md) for architecture and conventions.
