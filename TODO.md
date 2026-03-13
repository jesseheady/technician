# Technician Performance TODO

Tracking performance improvements identified during review (2026-03-13).

## Critical

- [x] Add HTTP server timeouts (ReadTimeout, WriteTimeout, IdleTimeout, MaxHeaderBytes)
- [x] Reuse HTTP client with connection pooling instead of creating per-probe
- [x] Add gzip response compression middleware
- [x] Close results channel on scheduler shutdown to prevent goroutine leak

## High

- [ ] Make S3/artifact uploads async (non-blocking result pipeline)
- [x] Status page: use JSON delta updates instead of full HTML re-fetch every 10s
- [ ] Cache DNS resolver instead of creating per-probe
- [ ] Move stagger delay outside cron job closure
- [ ] Add gRPC connection pooling
- [ ] Add Prometheus label cardinality bounds or documentation

## Medium

- [x] Add max size limit to TCP read buffer
- [x] Buffer template output before writing to ResponseWriter
- [ ] Use circular ring buffer instead of reslicing on overflow
- [x] Cache compiled regexes for HTTP assertions
- [ ] Add ETag support for status page / API responses

## Docker & CI

- [x] Create .dockerignore
- [ ] Add Docker buildx layer caching in CI
- [x] Docker Compose: add health checks, restart policies, pin image versions
- [ ] Add security scanning to CI (Trivy, CodeQL, or similar)
