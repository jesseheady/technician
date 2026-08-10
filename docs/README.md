# Documentation

| Document | Description |
|----------|-------------|
| [Getting started](getting-started.md) | Three onboarding paths (evaluate via Docker, contribute via `init.sh`, run in production), prerequisites, and check configuration. |
| [Metrics reference](metrics.md) | Every exported metric, its type, labels, and meaning. |
| [Alerting](alerting.md) | Native webhooks (Discord, Slack, generic), Grafana alerting (recommended), and Alertmanager. |
| [Testing and e2e](testing-and-e2e.md) | Unit tests, `go test`/vet, and end-to-end validation (validate command, metrics scrape, Docker stack, CI). |
| [Local development](mock-production.md) | Run Technician locally with full metrics/dashboards while probing mock local endpoints. |
| [Roadmap](roadmap.md) | Near-term MVP (status page redesign), scope principles, future enhancements, and recently completed work. |
| [Proposals](proposals/) | Design and rollout plans. |
| [Proposal: Cloudflare Workers](proposals/cloudflare-workers.md) | Plan to support deploying check runners as Cloudflare Workers (and alignment with AWS Lambda Edge). |
| [Proposal: Site identifiers (edge)](proposals/site-identifiers-edge.md) | How to define site/location when checks run on Workers or Lambda (provider location codes, metrics labels). |
| [Proposal: Browser checker options](proposals/browser-prober-options.md) | Four alternatives compared for the browser check runtime (today: a Node.js subprocess running Playwright). |
| [Proposal: CI/CD pipeline runner mode](proposals/ci-runner.md) | Using Technician as a CI step to enforce budgets against a preview/staging URL before merge. |
| [Proposal: Check dependencies (`depends_on`)](proposals/check-dependencies.md) | Gating a check on the last known result of another, as a smaller alternative to a dependency-DAG scheduler. |
| [Proposal: Status page and visualization roadmap](proposals/portal-features.md) | What to add to the built-in portal, what to leave to Grafana, and what comes free from Playwright. |
| [Proposal: Config repos](proposals/config-repo.md) | Supported shape for deploying Technician with private, version-controlled check configs (app repo / config repo split). |
| [Central Prometheus and Grafana](architecture/central-prometheus-grafana.md) | Config for central Prometheus (private VPC) and Grafana as source of record; local/Docker vs central reporting; edge push. |
| [Core Web Vitals](core-web-vitals.md) | LCP, INP, CLS; thresholds, collection, network throttling (4G/3G/slow-3G), and device emulation. |
| [Deployment sizing](deployment-sizing.md) | Resource requirements, cost estimates, and sizing by deployment mode (VPS, Lambda, Workers, full stack). |
| [Persistence and retention](persistence-and-retention.md) | Storage model, status store scaling, Prometheus cardinality, and long-term history without an application database. |
| [Multi-target deployment](multi-target-deployment.md) | Serve many workers from one checks directory with `check_filter` (types/groups/tags) and the `--types`/`--groups`/`--tags` flags. |
| [CI](ci.md) | GitHub Actions workflow, generic CI pipelines (GitLab, CircleCI, Jenkins), budget validation, Playwright in CI. |
| [Playwright scaling](playwright-scaling.md) | Browser check resource analysis, concurrency controls (`max_browsers`), and dedicated runner architecture (stages 1-4). |
| [Migrating between versions](migrating.md) | Breaking changes, label renames, port changes, and Prometheus/Grafana cutover. |

See also [AGENTS.md](../AGENTS.md) for architecture and conventions.
