# Proposal: Deploy Technician as Cloudflare Workers (and AWS Lambda Edge)

**Status:** Proposal  
**Goal:** Support running Technician (or a subset of its capabilities) on **Cloudflare Workers** in addition to existing and planned deployments (Docker, AWS Lambda Edge).

## Context

- **Current deployment:** Technician runs as a long-lived process (Docker, VM, or future Lambda container). It uses a cron-style scheduler, subprocesses (mtr, Node/Playwright), and persistent HTTP listeners for metrics and blackbox-style `/probe`.
- **Lambda Edge:** Mentioned in README as part of deployment strategy; typically used for request-triggered, short-lived executions at the edge.
- **Cloudflare Workers:** Serverless, edge, request-triggered; no subprocesses, no long-lived scheduler, strict CPU/time limits, and a V8/JavaScript or Workers runtime (with limited native binaries).

This proposal outlines how to support **Cloudflare Workers** as an additional deployment target without replacing the existing orchestrator.

## Is Workers the right Cloudflare product?

Cloudflare has more than one way to run checks from the edge. Choose based on how much you need to own the check logic and metrics format.

| Option | What it is | Best when |
|--------|------------|------------|
| **Health Checks** | Built-in edge service: monitor hostnames/IPs, response codes, protocols, intervals. Plan limits (e.g. Pro: 10, Business: 50, Enterprise: 1000). Analytics for uptime, latency, failure reasons. | You want “HTTP up/down from the edge” with minimal custom code and are fine with Cloudflare’s UI/API and metric shape. |
| **Synthetic Monitoring (Speed / Observatory)** | Browser or network tests from selected regions; Lighthouse-style analysis; configurable frequency by plan. | You want performance/UX checks and region selection, still within Cloudflare’s product. |
| **Workers (or Workers Unbound)** | Your own code at the edge: one HTTP request → run a check → return whatever you want (e.g. Prometheus text). Cron Triggers can run it on a schedule. Unbound removes strict CPU/time limits. | You need a **custom probe contract**, **Prometheus (or other) metric format**, or **tight integration with Technician’s pipeline** (same dashboards, same scrape targets). |

**Recommendation:**  
- **Try Health Checks (and Synthetic Monitoring)** first if “HTTP probe from the edge” is enough and you can accept their limits and metric format. No Worker to build or maintain.  
- **Use Workers** when you need the same blackbox-style `/probe` contract, Prometheus exposition, or multi-region scrape targets that feed into your existing Technician/Prometheus/Grafana stack. The rest of this proposal applies to that path.

## Is Lambda (or Lambda@Edge) the right AWS product?

AWS also offers several ways to run synthetic or health checks. Choose based on whether you need AWS-native metrics only or the same Technician contract and Prometheus pipeline.

| Option | What it is | Best when |
|--------|------------|------------|
| **Route 53 Health Checks** | Simple endpoint monitoring (IP or domain) at configurable intervals. Used for DNS failover and CloudWatch integration; metrics and alarms available. | You want “is it up?” from AWS regions for failover or basic alerting and are fine with Route 53/CloudWatch metric shape. |
| **CloudWatch Synthetics (Canaries)** | Scheduled scripts (Node.js, Python, or Java) that run as Lambda functions. Can use Playwright/Puppeteer/Selenium for browser checks or hit APIs/URLs. Run as often as once per minute; metrics in `CloudWatchSynthetics` namespace; optional screenshots, X-Ray. | You want **AWS-native synthetic monitoring** (availability, latency, browser or API) with minimal custom pipeline. Very close to Technician’s model; can replace or complement Technician if you standardize on CloudWatch. |
| **Lambda (or Lambda@Edge)** | Your own code: one invocation = one probe (e.g. triggered by EventBridge schedule or HTTP). Return Prometheus text or JSON. Lambda@Edge runs at CloudFront edge (Node.js/Python); regional Lambda can run Go (custom runtime/container). | You need the **same blackbox-style `/probe` contract**, **Prometheus exposition**, or **scrape targets** that feed into your existing Technician/Prometheus/Grafana stack. |

**Recommendation:**  
- **Try Route 53 Health Checks** for simple uptime/failover from AWS.  
- **Evaluate CloudWatch Synthetics (Canaries)** if you want full synthetic monitoring (including browser) inside AWS with CloudWatch metrics and minimal glue; it overlaps heavily with what Technician does.  
- **Use Lambda (or Lambda@Edge)** when you need your exact probe API and to keep a single Prometheus/Grafana pipeline fed by both the main Technician binary and AWS-based probe endpoints. The “Lambda Edge” adapter in this proposal applies to that path.

**Site identifiers:** When probes run at the edge, the execution location is determined by the platform (e.g. Cloudflare colo, AWS region or pop), not by a config file. See [Site identifiers for edge and serverless](site-identifiers-edge.md) for how to define and expose site/location for Workers and Lambda so metrics and dashboards stay consistent.

## Constraints

| Capability        | Current Technician        | CF Workers (and Lambda@Edge) |
|------------------|---------------------------|------------------------------|
| Execution model  | Long-running, cron-driven | Request-driven, event-based  |
| Subprocesses     | mtr, Node/Playwright      | Not available                |
| Runtime          | Go binary                 | JS/TS or limited WASM         |
| Scheduler        | In-process cron           | External (Cron Triggers, etc.)|
| Metrics endpoint | /metrics + /probe         | Per-request response         |

So we cannot run the **full** Technician stack (scheduler + mtr + Playwright) inside a single Worker. We can, however, run **per-request check execution** and optional metric export.

## Proposed direction

### 1. Treat Workers (and Lambda Edge) as “check runners,” not the main orchestrator

- **Orchestrator** (scheduler, aggregation, dashboards) stays as today: Docker, VM, or Lambda **container** (long-running process).
- **Edge / Workers** run a **single probe per request**: e.g. one HTTP request hits the Worker, the Worker performs one HTTP (or DNS) probe and returns result and/or exposes metrics for that run.

This matches “synthetic checks from the edge” without requiring cron or subprocesses inside the Worker.

### 2. Two implementation paths

**Path A – Adapter service (recommended first step)**  
Keep Technician as a Go binary. Add a **tiny Worker (or Lambda) in JS/TS** that:

- Receives a request (e.g. `GET /probe?target=https://example.com&check=http`).
- Performs a single HTTP (or DNS) check using the Worker runtime (e.g. `fetch` with timing).
- Returns Prometheus text format for that one probe, or a small JSON payload.

Technician’s Prometheus or a separate aggregator can **scrape** these Worker URLs (one per check per region) and aggregate. No Go in the Worker; only lightweight edge logic.

**Path B – Go in the Worker (WASM or compile target)**  
If/when CF Workers support Go (e.g. via WASM or a Go runtime):

- Compile a minimal “single HTTP probe” Go package to the Worker target.
- Reuse `internal/check/http.go` logic (or a trimmed, dependency-free copy) so behavior matches the main binary.
- Worker receives request, runs one probe, returns metrics.

Path B is more work and depends on Go-on-Workers support; Path A is immediately feasible.

### 3. Scope for “Technician on Cloudflare Workers”

- **In scope (v1):**
  - Design and document a **request contract** for “run one HTTP (or DNS) probe and return metrics” (aligned with existing `/probe?target=&module=` where possible).
  - Implement **Path A**: a small Cloudflare Worker (JavaScript/TypeScript) that performs one HTTP probe per request and returns Prometheus exposition or JSON.
  - Optional: Cron Triggers (or external scheduler) that invoke the Worker at intervals; Technician or Prometheus scrapes the Worker’s result URL.
  - Document how this fits alongside Docker and future Lambda Edge (e.g. same contract so the same dashboards/alerts can consume metrics from either).

- **Out of scope (v1):**
  - Running scheduler, mtr, or Playwright inside Workers.
  - Full parity with all check types on Workers (SMTP, traceroute, browser) – those remain on the main Technician deployment.

### 4. Alignment with AWS Lambda Edge

- Use the **same request/response contract** for “single probe run” so that:
  - A Lambda Edge function can implement the same contract (one probe per invocation).
  - Technician’s Prometheus, Grafana, and alerting can treat “scrape from Worker URL” and “scrape from Lambda URL” the same way.
- Lambda Edge can run a **compiled Go binary** (e.g. custom runtime or Lambda container) that reuses `internal/check` and `internal/exporter` to keep behavior identical to the main binary.

### 5. Plan (high level)

| Phase | Action |
|-------|--------|
| 1 | Define and document the **single-probe API** (query params, response format: Prometheus text and/or JSON). Ensure current blackbox handler and future Workers/Lambda share this contract. |
| 2 | Implement a **reference Cloudflare Worker** (JS/TS) that performs one HTTP probe per request and returns in that format. Publish in-repo under e.g. `workers/` or `internal/worker-cf/`. |
| 3 | Document **deployment**: Wrangler config, env vars, and how to wire Cron Triggers or external scheduler to hit the Worker; how Prometheus/Technician scrape or ingest the result. |
| 4 | (Optional) Add a small **Lambda Edge (or Lambda URL) adapter** in Go that implements the same contract and can be invoked by scheduler or Prometheus. |
| 5 | Update **AGENTS.md** and **README** to describe deployment options: Docker, Lambda (container), Lambda Edge (single probe), Cloudflare Workers (single probe). |

### 6. Success criteria

- A Cloudflare Worker can be deployed that, when requested, runs one HTTP probe and returns metrics in a format compatible with Technician’s expectations.
- The same dashboards and alerts can consume data from both the main Technician binary and from Worker (and later Lambda Edge) endpoints.
- Documentation clearly states which features run where (orchestrator vs edge check runners).

### 7. Risks and mitigations

- **Behavior drift:** Edge probe (Worker/Lambda) might differ from Go probe (e.g. DNS resolution, TLS). Mitigation: document differences; consider Path B (Go on Worker) later for parity.
- **Cost and limits:** Worker CPU/time limits may restrict heavy or long-running checks. Mitigation: keep v1 to simple HTTP (and optionally DNS) with timeouts; document limits.

---

**Next steps:** Get agreement on the single-probe API contract and Path A scope; then implement the reference Cloudflare Worker and add the deployment/docs structure above.
