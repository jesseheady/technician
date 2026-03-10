# Roadmap

Work that's been planned or partially designed but not yet implemented.

## Edge deployment adapters

### AWS Lambda adapter

Technician's Go binary can run in a Lambda container image (regional Lambda), but there's no Lambda-specific packaging or infrastructure yet.

**What's needed:**

- Lambda handler that wraps the existing probe execution (reuse `internal/probe` and `internal/exporter`) behind a Lambda function URL or API Gateway trigger.
- SAM template or Terraform/CDK for provisioning: function, EventBridge schedule (replaces in-process cron), IAM role, networking (VPC placement if probing internal targets).
- Decision on invocation model: EventBridge schedule triggers Lambda per-probe, or a single invocation runs all probes and pushes results.
- Push mechanism for metrics: Prometheus can't scrape a short-lived Lambda. Options are Pushgateway, Prometheus remote-write, or an in-VPC aggregator that Prometheus scrapes. See [central-prometheus-grafana.md](architecture/central-prometheus-grafana.md).
- Lambda@Edge (Node.js/Python only) would need a separate lightweight HTTP probe adapter, not the Go binary.

### Cloudflare Workers adapter

Designed in [proposals/cloudflare-workers.md](proposals/cloudflare-workers.md). The proposal recommends "Path A" — a small JS/TS Worker that performs one HTTP probe per request and returns Prometheus text or JSON.

**What's needed:**

- Reference Worker implementation (JS/TS) under e.g. `workers/cf/` with Wrangler config.
- Same `/probe?target=&module=` contract as the existing blackbox handler so Prometheus treats Worker and Technician endpoints identically.
- Cron Trigger configuration for scheduled probes.
- Documentation for how Prometheus or an aggregator scrapes/receives Worker results.

## Possible improvements

- **Module path** — `github.com/monkeyWzr/technician` uses a personal GitHub account. Consider a dedicated org or project namespace if establishing Technician as an independent project.
