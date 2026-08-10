# Multi-target deployment

Run one set of check definitions across many workers, each running only the
checks that fit its role — without duplicating check YAML per deployment.

## The problem

A common pattern is several workers with different capabilities or constraints:

- A full-capability VPS that can run network + HTTP + browser checks.
- A lightweight edge worker (Cloudflare Workers, AWS Lambda) limited to HTTP.
- A dedicated, beefy host that runs only Playwright browser flows.

The workaround is a separate `config/checks/` directory per target, but that
duplicates check definitions: updating a shared check (a URL, a threshold) means
editing every copy.

## Check filtering

`check_filter` lets a single `checks/` directory serve every target. Each worker
gets its own `technician.yml` that declares which checks to run:

```yaml
# technician.yml for a network + HTTP worker
check_filter:
  types: [http, tcp, icmp, dns]
```

```yaml
# technician.yml for a browser-only runner
check_filter:
  tags: [browser]
```

Filtering happens once at load time — filtered-out checks are never scheduled,
so they cost nothing at runtime and never emit metrics from that worker.

### Semantics

- Three dimensions: `types`, `groups`, `tags`.
- A check must match **every** dimension you specify (dimensions are AND-ed) and
  **any** value within a dimension (values are OR-ed).
- A check matches a `tags` filter if it carries **any** of the requested tags.
- Omit a dimension to leave it unrestricted; omit the whole block to run
  everything.
- Matching is **case-insensitive** (`HTTP` == `http`, `Website` == `website`).
- Unknown check types are rejected at startup, so a typo (`types: [htpp]`) fails
  loudly instead of silently running nothing.

`types` are the built-in check types (`http`, `tcp`, `udp`, `dns`, `icmp`,
`grpc`, `ntp`, `tls`, `smtp`, `traceroute`, `bgp`, `domain_expiry`,
`websocket`, `playwright`). `groups` and `tags` are user-defined metadata on each check:

```yaml
- name: example.com
  type: http
  group: Website
  tags: [public, critical]
  url: https://example.com
```

### CLI overrides

`worker` and `check run` accept flags that override the config per dimension (a
non-empty flag replaces that dimension entirely):

```bash
technician worker --types http,dns
technician worker --groups Infrastructure
technician worker --tags browser
```

An explicit `technician check run --check <name>` always runs the named check,
even if `check_filter` would exclude it — the filter only shapes "run all".

### Example: three targets, one checks directory

| Deployment target | Filter | Runs |
|---|---|---|
| VPS us-east-1 | `types: [http, tcp, icmp, dns]` | Network + HTTP checks |
| Edge (HTTP-only) | `types: [http]` | HTTP checks only |
| Browser runner | `tags: [browser]` | Playwright flows on a dedicated host |

```
config/
  checks/            # one copy, shared by every target
    http.yml
    dns.yml
    playwright.yml
  us-east-1.yml      # check_filter: { types: [http, tcp, icmp, dns] }
  edge.yml           # check_filter: { types: [http] }
  browser.yml        # check_filter: { tags: [browser] }
```

Each worker runs with `--config config/<target>.yml`. All workers expose/push
metrics to the same central Prometheus; Grafana sees every check regardless of
which worker ran it, distinguished by the `region` label.

## Validation and CI

`technician validate` respects `check_filter` too, so CI can validate exactly the
subset a given target will run. Its existing `--check-type` / `--exclude-type`
flags still work and layer on top of the config filter.
