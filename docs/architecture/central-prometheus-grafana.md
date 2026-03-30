# Central Prometheus and Grafana

**Goal:** Run probe workers locally, in Docker, or at the edge, and have **deployed** workers send metrics to a **central Prometheus** on your private AWS VPC. Your **central Grafana** (also in the VPC or otherwise secured) is the source of record for those probes. Local Prometheus + Grafana (e.g. from `docker compose up`) remain for proving probes and running tests.

## Roles

| Environment | Purpose | Metrics flow |
|-------------|--------|--------------|
| **Local / Docker** | Prove probes, run tests, develop. Optional: push to central for ad‑hoc viewing. | Local stack only, or optional push to central (see below). |
| **Central Prometheus (VPC)** | Single place where all **deployed** probe metrics are stored. | Scrapes Technician instances that are reachable; optionally receives push from edge or local. |
| **Central Grafana** | Source of record for dashboards and alerts for deployed probes. | Datasource = central Prometheus. |
| **Edge (Workers / Lambda)** | Run probes from edge; no long‑lived `/metrics` to scrape. | Push to central (Pushgateway or remote‑write) when a probe runs, or via an aggregator that Prometheus scrapes. |

## Getting metrics to central Prometheus

Prometheus is **pull** by default: it scrapes HTTP endpoints. So the shape of your config depends on where the workers run.

### 1. Workers that Prometheus can scrape (same VPC or reachable)

When Technician runs as a long‑lived process **inside your AWS footprint** (EC2, ECS, EKS, Lambda container, etc.) and exposes `:9590/metrics` on a reachable host:

- **Central Prometheus** runs in the same VPC (or a peered VPC / VPN) and scrapes those targets.
- No push needed; use normal `scrape_configs` with static targets or service discovery (e.g. DNS, EC2, ECS).

Example: Prometheus in the VPC scraping Technician by hostname or by service discovery:

```yaml
# Central Prometheus (e.g. prometheus.yml in your VPC deployment)
scrape_configs:
  - job_name: technician
    scrape_interval: 30s
    static_configs:
      # Option A: explicit list (good for a few fixed instances)
      - targets:
          - technician-us-east-1.internal:9590
          - technician-us-west-2.internal:9590
        labels:
          environment: production
      # Option B: DNS A records (e.g. technician-probes.private.local → multiple IPs)
      # - targets:
      #     - technician-probes.private.local:9590
      #   dns_sd_configs:
      #     - names: ['technician-probes.private.local']
      #       type: A
      #       port: 9590
```

Each Technician instance should be started with the appropriate `SITE_CODE` (e.g. `us-east-1`, `us-west-2`) and the same `technician.yml` (or region‑specific) so labels are consistent. See [Site identifiers for edge and serverless](../proposals/site-identifiers-edge.md).

### 2. Local or Docker workers (optional: push to central)

Your laptop or local Docker stack is not reachable from the VPC. Options:

- **Keep it local only** – Use the local Prometheus + Grafana from `docker compose up` to prove probes and run tests. No push to central.
- **Push to central (optional)** – If you want to see local or Docker runs in central Grafana occasionally:
  - Run a **Prometheus Pushgateway** (or a remote‑write receiver) in the VPC, reachable via VPN or a secure tunnel.
  - After each probe run (or on a timer), push metrics from the Technician host to the Pushgateway. Prometheus then scrapes the Pushgateway.
  - Alternatively, use **Grafana Agent** or **Alloy** with a remote‑write receiver in the VPC; Technician or a small sidecar pushes in Prometheus remote‑write format.

Technician does not push by default; you’d add a small push step (script or sidecar) or use an agent that scrapes local Technician and forwards. Documenting the exact push mechanism is out of scope here; the important point is that central Prometheus (or a Pushgateway it scrapes) can be the single sink.

### 3. Edge workers (Cloudflare Workers, Lambda)

Edge runs are request‑driven and don’t expose a long‑lived `/metrics` endpoint. Options:

- **Push per run** – When an edge probe finishes, the Worker or Lambda pushes that probe’s metrics (e.g. in Prometheus exposition or remote‑write format) to a **Pushgateway** or **remote‑write endpoint** in your VPC (reachable from the public internet over HTTPS with auth, or via a private path if Lambda runs in the VPC).
- **Aggregator** – A small service in the VPC is scraped by Prometheus. On a schedule (or on demand), it calls your edge probe endpoints, collects results, and exposes them as Prometheus metrics. Then central Prometheus only scrapes the aggregator.

In both cases, edge metrics should use the same **site identifier** convention (e.g. `infra_provider=cloudflare`, `region=IAD`) so central Grafana can filter and group like other sites. See [Site identifiers for edge and serverless](../proposals/site-identifiers-edge.md).

## Central Grafana

- **Datasource:** Configure Grafana with a Prometheus datasource pointing at your **central Prometheus** (e.g. `http://prometheus.vpc.internal:9090` or the internal URL you use).
- **Dashboards:** Use the same Technician dashboards (uptime, HTTP timing, budgets, etc.); they already use `region` and `probe` variables, so they work with local, VPC, and (if you push) edge sites.
- **Alerts:** Define alert rules in central Prometheus (or Grafana) so that deployed probes drive notifications. Your existing `prometheus/rules.yml` is a good base; deploy it with central Prometheus and adjust for your VPC targets.

## Example: minimal central Prometheus config (VPC)

```yaml
# Central Prometheus in AWS VPC – scrape only in-VPC Technician instances
global:
  scrape_interval: 30s
  evaluation_interval: 30s

rule_files:
  - rules.yml   # same Technician rules (ProbeFailing, BudgetViolation, etc.)

scrape_configs:
  - job_name: technician
    scrape_interval: 30s
    static_configs:
      - targets: ['technician-us-east-1.private:9590', 'technician-us-west-2.private:9590']
        labels:
          environment: production
  # Optional: scrape Pushgateway if you use it for edge or local push
  # - job_name: pushgateway
  #   honor_labels: true
  #   static_configs:
  #     - targets: ['pushgateway.private:9091']
```

Replace hostnames with your real VPC DNS or IPs. Use IAM, security groups, and private subnets so that only Grafana and other trusted services can talk to Prometheus.

## Summary

- **Local/Docker:** Proves probes and tests; metrics stay in the local stack unless you add an optional push to central.
- **Central Prometheus (VPC):** Scrapes all reachable Technician instances in your AWS footprint; optionally scrapes a Pushgateway or aggregator for edge/pushed metrics.
- **Central Grafana:** Single source of record for deployed probes; datasource = central Prometheus; same dashboards and alerts, keyed by `region` and infra_provider.
- **Edge:** Push to a VPC-hosted Pushgateway or remote-write endpoint, or use an in-VPC aggregator that Prometheus scrapes, so edge results appear in central Grafana with consistent site labels.

This keeps a clear split: local stack for development and validation, central Prometheus + Grafana for production reporting from deployed and edge workers.

---

## How the current setup works when deployed to a VPC

Your current repo is: **Technician** (exposes `/metrics` on 9590), **Prometheus** (scrapes that URL), **Grafana** (datasource = Prometheus, dashboards from `dashboards/`). In Docker Compose, Prometheus reaches Technician via the hostname `technician` on the compose network. Here's how that same setup maps to a VPC.

### What stays the same

- **Technician binary and config** – Same Dockerfile, same `technician.yml` and `probes/` layout. Technician still listens on `:9590` and serves `/metrics`, `/health`, `/probe`.
- **Probe definitions** – Same YAML (all 13 probe types: HTTP, TCP, UDP, DNS, ICMP, gRPC, NTP, TLS, SMTP, traceroute, BGP, domain expiry, Playwright). Ship your `config/` directory (or a production variant) into the image or mount from a config store.
- **Prometheus rules** – Same `prometheus/rules.yml` (ProbeFailing, BudgetViolation, etc.); deploy it with Prometheus in the VPC.
- **Grafana dashboards** – Same JSON in `dashboards/`; provision them in central Grafana. Variables (`region`, `probe`) already work with whatever sites you run.

### What changes in the VPC

| Piece | Local (compose) | In VPC |
|-------|------------------|--------|
| **Technician** | One container, hostname `technician`, `SITE_CODE=local`. | One or more instances (e.g. EC2, ECS), each with a **reachable hostname or IP** and **per-instance `SITE_CODE`** (e.g. `us-east-1`, `us-west-2`). Must listen on an interface Prometheus can reach (e.g. `0.0.0.0:9590`). |
| **Prometheus** | Scrapes `technician:9590` (compose DNS). | Scrapes **VPC hostnames or IPs** (e.g. `technician-us-east-1.private:9590`). Same `rule_files`; targets come from static config or service discovery. |
| **Grafana** | Same network as Prometheus; datasource `http://prometheus:9090`. | Central Grafana's datasource is **central Prometheus** (e.g. `http://prometheus.private:9090`). No need to run Grafana next to each Technician. |

### Two deployment patterns

**Pattern A – Full stack in VPC (mirror of compose)**  
Run Technician + Prometheus + Grafana together in the VPC (e.g. one EC2 or ECS task per region, or a single cluster with all three). Prometheus scrape target is the Technician service hostname in that network (e.g. `technician:9590` if you keep the same service names). Good for a self-contained "single region" or "single cluster" deployment. Each Technician instance still gets its own `SITE_CODE`.

**Pattern B – Technician only in VPC, central Prometheus + Grafana**  
Run **only Technician** per region (or per AZ). Run **one** Prometheus and **one** Grafana elsewhere in the VPC (or in a shared observability account). Prometheus scrape config lists every Technician endpoint (by private DNS or IP). This is the central reporting model: many workers, one central Prometheus and one central Grafana.

### Concrete steps (Pattern B, one Technician per region)

1. **Build and push the image** – Same Dockerfile; push to ECR (or your registry) from the repo.
2. **Run Technician in the VPC** – ECS, EC2, or similar. Example (conceptual):
   - Image: your Technician image.
   - Command: `worker --config /etc/technician/technician.yml` (same as Dockerfile CMD).
   - Env: `SITE_CODE=us-east-1` (or the region/identifier for that instance).
   - Mount or bake in `technician.yml` and `probes/` (same structure as `config/`).
   - Listen: `0.0.0.0:9590` (default) so Prometheus in the VPC can reach it.
3. **Networking** – Security group for Technician: allow **inbound TCP 9590** from the Prometheus server(s) (or from a load balancer / service discovery you use). No public port needed if Prometheus is in the same VPC or peered.
4. **Discovery** – Give each Technician instance a stable name (e.g. private DNS `technician-us-east-1.private` or ECS service discovery). Put those hostnames (and port 9590) in Prometheus `scrape_configs` as in the "Example: minimal central Prometheus config" above.
5. **Prometheus** – Run in the VPC with that scrape config and `rule_files: [rules.yml]`. Storage and retention as you normally would for Prometheus.
6. **Grafana** – Point its Prometheus datasource at central Prometheus. Import or provision the same dashboards from `dashboards/`. No change to dashboard logic; they already filter by `region` and `probe`.

### Summary

In the VPC you keep the **same** Technician app, config format, probes, rules, and dashboards. The only operational differences are: **where** Technician runs (VPC hosts), **how** Prometheus finds it (VPC hostnames/IPs in `scrape_configs`), and **one** central Grafana using that Prometheus. The current setup is already "VPC-ready"; deployment is mostly wiring discovery and `SITE_CODE` per instance.
