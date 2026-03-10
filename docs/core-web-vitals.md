# Core Web Vitals (browser probes)

Technician’s Playwright probes collect the **Core Web Vitals** for synthetic monitoring. As of March 2024, the three official metrics are:

| Metric | Meaning | Good threshold |
|--------|--------|----------------|
| **LCP** (Largest Contentful Paint) | How fast the main content loads | ≤ 2.5 s |
| **INP** (Interaction to Next Paint) | How fast the page responds to user actions | ≤ 200 ms |
| **CLS** (Cumulative Layout Shift) | Visual stability (unexpected layout shifts) | ≤ 0.1 |

**Note:** INP replaced the former FID (First Input Delay) metric, which Google removed from Core Web Vitals in March 2024. Technician does not collect or expose FID.

## Collection

- **LCP and CLS** are collected in the browser via the [web-vitals](https://github.com/GoogleChrome/web-vitals) library (PerformanceObserver).
- **INP** requires at least one user interaction; the Playwright harness triggers a click on the page after load, then reads INP.

## Budgets and alerts

In `budgets.yml` you can set thresholds for `lcp`, `inp`, and `cls` (e.g. `lcp: 2500`, `inp: 200`, `cls: 0.1`). Prometheus rules include **HighLCP** (LCP &gt; 2500 ms) and **HighINP** (INP &gt; 200 ms).

## Dashboards

The **Technician: Web Performance Vitals** dashboard shows LCP, INP, CLS, and TTFB with the same good/warning thresholds (green ≤ good, yellow &lt; bad, red ≥ bad).
