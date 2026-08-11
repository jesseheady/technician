# Metrics reference

Every metric Technician exports, served in Prometheus text format at
`metrics.prometheus.listen` (default `:9590/metrics`).

All metrics carry the labels `name` (the check name), plus `region`, `city`, and
`country` from the active origin. Extra labels are noted per group. Any custom
`labels` on an origin are added too, unless they collide with a built-in.

Check names are capped by `metrics.prometheus.max_check_cardinality`; checks past
the limit are dropped rather than silently blowing up Prometheus.

**OTLP export.** Setting `metrics.otel.metrics: true` (with `metrics.otel.endpoint`)
also pushes every `technician_*` metric below to an OTLP collector, in addition to
the `/metrics` endpoint — for OTel-native backends or AMP-over-OTLP without a
Prometheus scrape. It bridges this same registry, so the OTLP stream stays in
parity automatically; the `go_*`/`process_*` self-health collectors are exported
on `/metrics` only. Labels become OTLP attributes unchanged.

**Remote write.** Setting `metrics.prometheus.remote_write_url` pushes the same
`technician_*` metrics to a Prometheus-compatible endpoint (AMP, Grafana Cloud,
Mimir, Thanos) on a timer (`remote_write_interval`, default 15s) — for Lambda,
Workers, or locked-down VPCs that Prometheus can't scrape. AMP auth is AWS SigV4
(`remote_write_sigv4` + `remote_write_region`); other backends use
`remote_write_headers`. Delivery is best-effort: since these are gauges, a failed
push is retried by the next tick rather than queued.

## Universal

Emitted for every check type. SMTP, traceroute, and gRPC checks emit only these.

Extra label: `type` (the check type) on all but `technician_budget_violation`,
which carries `metric` (the budget metric key) instead.

| Metric | Type | Description |
|---|---|---|
| `technician_check_healthy` | Gauge | 1 if the target responded successfully, 0 if the check failed |
| `technician_check_duration_seconds` | Gauge | End-to-end check execution time in seconds |
| `technician_check_degraded` | Gauge | Latency exceeds the `degraded_after` threshold (1=degraded, 0=ok). Total duration for most types; average per-probe RTT for ICMP |
| `technician_check_infra_error` | Gauge | 1 if the check's own infrastructure failed (not the target) |
| `technician_budget_violation` | Gauge | Whether a performance budget is violated (1=violated, 0=ok) |

## HTTP

| Metric | Type | Description |
|---|---|---|
| `technician_http_response_status` | Gauge | Status code returned by the HTTP target |
| `technician_http_response_bytes` | Gauge | HTTP response body size in bytes |
| `technician_http_dns_seconds` | Gauge | DNS lookup duration in seconds |
| `technician_http_connect_seconds` | Gauge | TCP connect duration in seconds |
| `technician_http_tls_seconds` | Gauge | TLS handshake duration in seconds |
| `technician_http_ttfb_seconds` | Gauge | Time to first byte in seconds |
| `technician_http_transfer_seconds` | Gauge | Response transfer duration in seconds |

## TCP and UDP

| Metric | Type | Description |
|---|---|---|
| `technician_tcp_connect_seconds` | Gauge | TCP connection duration in seconds |
| `technician_tcp_tls_seconds` | Gauge | TCP TLS handshake duration in seconds |
| `technician_udp_rtt_seconds` | Gauge | UDP round-trip time in seconds (send to response) |
| `technician_udp_response_bytes` | Gauge | UDP response size in bytes |
| `technician_ws_connect_seconds` | Gauge | WebSocket connection/handshake duration in seconds |
| `technician_ws_message_seconds` | Gauge | WebSocket send-to-response round-trip duration in seconds |

## DNS and ICMP

| Metric | Type | Description |
|---|---|---|
| `technician_dns_query_seconds` | Gauge | DNS query duration in seconds |
| `technician_icmp_avg_rtt_seconds` | Gauge | ICMP average round-trip time in seconds |
| `technician_icmp_packet_loss_percent` | Gauge | ICMP packet loss percentage |

## NTP

| Metric | Type | Description |
|---|---|---|
| `technician_ntp_offset_ms` | Gauge | Clock offset in milliseconds (positive = local ahead of server) |
| `technician_ntp_rtt_seconds` | Gauge | Round-trip time in seconds |
| `technician_ntp_stratum` | Gauge | Server stratum level (1 = primary reference) |

## TLS and domain expiry

| Metric | Type | Description |
|---|---|---|
| `technician_tls_cert_expiry_days` | Gauge | Days until the TLS certificate expires |
| `technician_tls_cert_valid` | Gauge | Whether the certificate chain is valid (1=valid, 0=invalid) |
| `technician_domain_expiry_days` | Gauge | Days until domain registration expires |
| `technician_domain_registered` | Gauge | Whether the domain is currently registered (1=registered, 0=not found) |

## BGP

| Metric | Type | Description |
|---|---|---|
| `technician_bgp_prefix_visible` | Gauge | Whether the prefix is visible in the global routing table (1=visible) |
| `technician_bgp_origin_asn` | Gauge | Observed origin AS number for the prefix |
| `technician_bgp_origin_match` | Gauge | Whether the origin ASN matches the expected value (1=match) |

## Browser (Playwright)

Extra labels: `network` and `device` (the emulated profile).
See [core-web-vitals.md](core-web-vitals.md) for thresholds.

| Metric | Type | Description |
|---|---|---|
| `technician_browser_lcp_ms` | Gauge | Largest Contentful Paint in milliseconds |
| `technician_browser_inp_ms` | Gauge | Interaction to Next Paint in milliseconds (good ≤200ms) |
| `technician_browser_cls` | Gauge | Cumulative Layout Shift score |
| `technician_browser_fcp_ms` | Gauge | First Contentful Paint in milliseconds |
| `technician_browser_ttfb_ms` | Gauge | Time to First Byte in milliseconds |
| `technician_browser_dom_complete_ms` | Gauge | DOM complete time in milliseconds |
| `technician_browser_resource_count` | Gauge | Total resource count |
| `technician_browser_total_transfer_bytes` | Gauge | Total transfer size in bytes |

## HAR

Extra label: `resource_type`.

| Metric | Type | Description |
|---|---|---|
| `technician_har_resource_bytes` | Gauge | Resource size in bytes by resource type |
| `technician_har_resource_duration_ms` | Gauge | Resource duration in milliseconds by resource type |

## Worker internals

| Metric | Type | Description |
|---|---|---|
| `technician_last_run_timestamp_seconds` | Gauge | Unix timestamp of the most recent recorded result (excludes infra errors) |
| `technician_status_store_write_errors_total` | Counter | Status store persistence write failures since process start |

## Runtime / self-health

Technician's own health is exported by the standard Prometheus Go and process
collectors (registered automatically), so no custom metrics duplicate them:

| Metric | Type | Description |
|---|---|---|
| `go_goroutines` | Gauge | Goroutines currently running |
| `go_memstats_alloc_bytes` | Gauge | Heap bytes in use |
| `go_threads` | Gauge | OS threads created |
| `process_resident_memory_bytes` | Gauge | Resident set size (RSS) |
| `process_cpu_seconds_total` | Counter | Total CPU time |
| `process_open_fds` | Gauge | Open file descriptors |

Per-check execution is also logged as a structured `Check result` line (name,
type, success, duration, region, degraded, retries, error) for Loki ingestion;
when OTLP tracing is enabled it carries `trace_id`/`span_id` so logs link to
their traces.
