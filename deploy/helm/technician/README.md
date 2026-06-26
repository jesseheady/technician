# Technician Helm chart

Deploys Technician on Kubernetes following the project's core model: **one
worker per vantage point**. Each entry in `origins` renders its own
`Deployment` (pinned to a single replica) and `Service`, with a distinct
`ORIGIN_ID`.

## Why no replica pool

Technician scales by **geography, not throughput**. Every metric series is
labeled by origin, and the scheduler staggers checks by a hash of
`(check, origin)`. Running identical replicas inside one origin would:

- produce colliding `{check, origin}` series (duplicate/flapping samples in
  Prometheus),
- multiply load on the targets you monitor by the replica count,
- fire simultaneously rather than spreading out (same-origin stagger is
  identical).

So this chart deliberately offers **no `replicaCount` and no HPA**. Add
capacity by adding origins.

Splitting a single origin's check load across workers (e.g. moving heavy
Playwright checks onto a dedicated worker) requires **probe filtering**, which
is not yet implemented — track
[#15](https://github.com/jesseheady/technician/issues/15). Until then, every
worker in an origin runs the full check set.

## Install

```bash
helm install technician deploy/helm/technician \
  --set image.tag=<version> \
  --values my-values.yaml
```

> The default `image.repository` (`ghcr.io/jesseheady/technician`) is a private
> package. To run this chart yourself, override `image.repository` with your own
> build and supply `imagePullSecrets` for the registry it lives in.

## Multi-origin example

```yaml
origins:
  - id: us-east-1
  - id: eu-west-1
    nodeSelector:
      topology.kubernetes.io/region: eu-west-1

config:
  technician.yml: |
    service: technician
    metrics:
      listen: "0.0.0.0:9590"
  checks.yml: |
    # your checks
```

## Browser (Playwright) checks

Set `playwright.enabled=true` to turn on `shareProcessNamespace` (so PID 1
reaps exited Chromium children) and apply the heavier resource profile. See
`docs/playwright-scaling.md`. A dedicated browser-worker topology is tracked in
[#133](https://github.com/jesseheady/technician/issues/133).

## Scraping

- **prometheus-operator:** set `podMonitor.enabled=true` and `podMonitor.labels`
  to match your `podMonitorSelector`. Series get an `origin` label.
- **otherwise:** point Prometheus at the per-origin `Service`s.

## Values

See [values.yaml](values.yaml) for the full list and inline documentation.
