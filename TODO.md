# Technician Performance TODO

Tracking performance improvements identified during review (2026-03-13).

## Critical

- [x] Add HTTP server timeouts (ReadTimeout, WriteTimeout, IdleTimeout, MaxHeaderBytes)
- [x] Reuse HTTP client with connection pooling instead of creating per-check
- [x] Add gzip response compression middleware
- [x] Close results channel on scheduler shutdown to prevent goroutine leak

## High

- [ ] Make S3/artifact uploads async — deferred: uploads not yet wired into result pipeline
- [x] Status page: use JSON delta updates instead of full HTML re-fetch every 10s
- [x] Cache DNS resolver instead of creating per-check
- [x] Move stagger delay — reviewed; sleep is intentional for stagger spread, added clarifying comment
- [x] Add gRPC connection pooling
- [x] Add Prometheus label cardinality bounds (maxProbeCardinality=500 guard)

## Medium

- [x] Add max size limit to TCP read buffer
- [x] Buffer template output before writing to ResponseWriter
- [x] Use circular ring buffer instead of reslicing on overflow
- [x] Cache compiled regexes for HTTP assertions
- [x] Add ETag support for status page / API responses

## Docker & CI

- [x] Create .dockerignore
- [x] Add Docker buildx layer caching in CI
- [x] Docker Compose: add health checks, restart policies, pin image versions
- [x] Add security scanning to CI (govulncheck)

## New Check Types

- [x] BGP route checks (origin AS validation, prefix hijack detection, path monitoring)
- [x] Domain expiration checks (WHOIS/RDAP lookup, days-until-expiry metric + alerting)

## Status Page & Dashboards

- [x] Tab-based category navigation (Network, Web, Services, Security) with search and issues-only filter
- [ ] Prune stale checks from status store on startup (remove entries for checks no longer in config)
- [ ] Status page: show flapping/unstable indicator based on ring buffer history (e.g. frequent up/down transitions)
- [ ] Review Grafana community dashboards for inspiration on dashboard design patterns
