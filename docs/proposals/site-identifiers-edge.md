# Site identifiers for edge and serverless

**Status:** Proposal  
**Goal:** Define how we identify "site" (check execution location) when probes run on Cloudflare Workers, AWS Lambda, or Lambda@Edge, so metrics and dashboards stay consistent with the current fixed-infrastructure model.

## Current model

Today, **sites** are defined in config (`technician.yml`):

```yaml
sites:
  - code: us-east-1
    city: N. Virginia
    country: US
    location_hash: dqcjq
    infra_provider: aws
  - code: us-west-2
    city: Oregon
    country: US
    location_hash: c20y
    infra_provider: aws
```

- One **site** per long-lived instance (VM, Docker, future Lambda container). The instance is started with `SITE_CODE=us-east-1` (or `--site us-east-1`).
- Metrics use `region`, `city`, `country` from `Site.Labels()`. The "site" is fixed for the lifetime of the process.

With **edge Workers** or **Lambda** (per-request execution), there is no single config-driven site: the **location is determined by the platform at request time**. We need a clear, provider-aware way to express that location so it can be used as a region identifier in metrics and dashboards.

## How providers expose location

### Cloudflare Workers

- **Source:** `request.cf.colo` (when the Worker runs on Cloudflare’s network).
- **Format:** Three-letter **IATA airport code** (e.g. `SFO`, `IAD`, `ORD`, `DFW`).
- **Docs:** [Request – Cloudflare Object](https://developers.cloudflare.com/workers/runtime-apis/request/), [Spans and attributes](https://developers.cloudflare.com/workers/observability/traces/spans-and-attributes/) (`cloudflare.colo` in tracing).
- **Note:** You don’t choose which colo runs a request; routing is by network. So the "site" is **discovered at request time**, not configured.

### AWS Lambda (regional)

- **Source:** `AWS_REGION` environment variable (set by Lambda).
- **Format:** AWS region id (e.g. `us-east-1`, `eu-west-1`, `ap-northeast-1`).
- **Note:** You deploy the function to a region, so the "site" is fixed per deployment. To run from multiple regions you deploy one function per region (or multiple stacks) and each has its own `AWS_REGION`.

### AWS Lambda@Edge

- **Source:** Not in the event body. You can use the **`x-amz-cf-pop`** response header (CloudFront point-of-presence) after the request, or infer from **CloudWatch Logs region** (logs are written in the region that received the request).
- **Format:** Pop code like `IAD89-P1` (airport prefix + suffix). Airport prefix (e.g. `IAD`, `LHR`) is the common shorthand; it maps to geographic location.
- **Docs:** [Lambda@Edge event structure](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/lambda-event-structure.html), [CloudFront headers](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/adding-cloudfront-headers.html); troubleshooting often references `x-amz-cf-pop` for "where did this run."

## Proposed approach: provider + location code

Keep **static sites** for fixed infrastructure (current behavior). For **edge/serverless**, derive site from the provider and the location identifier the platform gives:

| Provider        | Location source        | Suggested site identifier format | Example   |
|-----------------|------------------------|-----------------------------------|-----------|
| (config)        | `sites[].code`         | `code` as-is                      | `us-east-1` |
| Cloudflare      | `request.cf.colo`      | `cf:<colo>` or just `colo`       | `cf:IAD`    |
| AWS Lambda      | `AWS_REGION`           | `aws:<region>` or just `region`  | `aws:us-east-1` |
| AWS Lambda@Edge | `x-amz-cf-pop` (prefix)| `aws-edge:<pop>` or pop prefix   | `aws-edge:IAD`  |

**Recommendations:**

1. **Standardize on a composite label** in metrics (and optionally in config later):
   - `infra_provider` – `technician` | `cloudflare` | `aws` (and maybe `aws-edge` to distinguish Edge from regional Lambda).
   - `region` – For static: config `code`. For Cloudflare: colo (IATA). For AWS regional: `AWS_REGION`. For Lambda@Edge: pop prefix or full pop code.
   So edge probes would set e.g. `infra_provider=cloudflare`, `region=IAD`; no need for a pre-defined `sites` entry for every colo.

2. **Optional mapping layer:** If we want human-friendly names in dashboards (city, country) for edge sites, we can:
   - Maintain a small **lookup table** (e.g. IATA → city/country, AWS region → city/country) used only for display, or
   - Allow **optional** site definitions in config that match infra_provider + location (e.g. `infra_provider: cloudflare`, `code: IAD`) and fill in city/country for labels when present; otherwise use raw `region`.

3. **Uniqueness:** For aggregation and alerting, `(infra_provider, region)` should uniquely identify the execution location. So `region` alone is enough if we always set `infra_provider`; or we can encode provider in the code (e.g. `cf:IAD`) and keep a single `region` label.

4. **Contract for Worker/Lambda adapters:** When implementing the blackbox-style probe endpoint in a Worker or Lambda, the adapter must:
   - **Read** the execution location from the platform (e.g. `request.cf.colo`, `process.env.AWS_REGION`, or response header for Edge).
   - **Emit** metrics (e.g. Prometheus exposition) with `region` (and optionally `infra_provider`) set to that value, so scrapers and Grafana see a consistent notion of "site" across Technician binary, Cloudflare, and AWS.

## Summary

- **Cloudflare:** Use `request.cf.colo` (IATA code). No need to pre-define every colo in config; use it directly as `region` with `infra_provider=cloudflare`.
- **AWS Lambda (regional):** Use `AWS_REGION`; one deployment per region if you want multiple "sites." Set `region` (and `infra_provider=aws`) from the env.
- **Lambda@Edge:** Use pop code (e.g. from `x-amz-cf-pop`) or its airport prefix; set `region` (and `infra_provider=aws-edge` or `aws`) so each edge location is distinguishable.
- **Static (current):** Keep `sites` in config and `SITE_CODE`; no change for existing deployments.

This keeps site identifiers **clear and provider-declared**: we use the location identifiers that AWS and Cloudflare already expose, and we standardize how they appear in our metrics and docs.
