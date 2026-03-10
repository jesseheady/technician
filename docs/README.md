# Documentation

| Document | Description |
|----------|-------------|
| [Getting started](getting-started.md) | Prerequisites, one-time setup (Mac init script), and how to run the worker and full stack. |
| [Testing and e2e](testing-and-e2e.md) | Unit tests, `go test`/vet, and end-to-end validation (validate command, metrics scrape, Docker stack, CI canary). |
| [Mock production](mock-production.md) | Run Technician locally with full metrics/dashboards while probing mock local endpoints. |
| [Proposals](proposals/) | Design and rollout plans. |
| [Proposal: Cloudflare Workers](proposals/cloudflare-workers.md) | Plan to support deploying probe runners as Cloudflare Workers (and alignment with AWS Lambda Edge). |
| [Proposal: Site identifiers (edge)](proposals/site-identifiers-edge.md) | How to define site/location when probes run on Workers or Lambda (provider location codes, metrics labels). |
| [Central Prometheus and Grafana](architecture/central-prometheus-grafana.md) | Config for central Prometheus (private VPC) and Grafana as source of record; local/Docker vs central reporting; edge push. |
| [Core Web Vitals](core-web-vitals.md) | LCP, INP, CLS; thresholds and collection. |

See also [AGENTS.md](../AGENTS.md) for architecture and conventions.
