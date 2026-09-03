# Core Web Vitals (browser checks)

Technician’s Playwright checks collect the **Core Web Vitals** for synthetic monitoring. As of March 2024, the three official metrics are:

| Metric | Meaning | Good threshold |
|--------|--------|----------------|
| **LCP** (Largest Contentful Paint) | How fast the main content loads | ≤ 2.5 s |
| **INP** (Interaction to Next Paint) | How fast the page responds to user actions | ≤ 200 ms |
| **CLS** (Cumulative Layout Shift) | Visual stability (unexpected layout shifts) | ≤ 0.1 |

**Note:** INP replaced the former FID (First Input Delay) metric, which Google removed from Core Web Vitals in March 2024. Technician does not collect or expose FID.

## Collection

- **LCP, CLS and INP** are collected in the browser with the [web-vitals](https://github.com/GoogleChrome/web-vitals) library (PerformanceObserver).

### INP needs an interaction

INP measures the worst interaction of the page lifetime. A check receives an INP value only if its script interacts with the page. A click, a key press and a tap are interactions.

A check that only loads a page reports no INP. Technician then records no `technician_browser_inp_ms` sample, and it applies no `inp` budget threshold. This is deliberate: a value of 0 ms looks the same as a very fast interaction, and it makes the INP alerts and the INP budgets inert.

The example script `examples/playwright/example_flow.js` only loads a page. Add interactions to your own script if you want INP for that check.

## Network throttling and device emulation

Playwright checks support mobile network simulation and device emulation to measure performance under realistic mobile conditions.

### Network profiles

Set `network` in a Playwright check config to throttle via Chrome DevTools Protocol (CDP):

| Profile | Download | Upload | RTT |
|---------|----------|--------|-----|
| `4g` | 4 Mbps | 3 Mbps | 150 ms |
| `3g` | 1.5 Mbps | 750 kbps | 300 ms |
| `slow-3g` | 500 kbps | 500 kbps | 2000 ms |

### Device emulation

Set `device` to any [Playwright device descriptor](https://playwright.dev/docs/emulation#devices) (e.g. `"iPhone 14"`, `"Pixel 7"`). This applies the device's viewport, user agent, and device scale factor.

### Example config

```yaml
- name: "example.com (mobile 4g)"
  script: playwright/flow.js
  base_url: "https://example.com"
  network: "4g"
  device: "iPhone 14"
  schedule: "0 0 * * * *"
  timeout: 120s
```

### Prometheus labels

Network and device values are exposed as Prometheus labels on all `technician_browser_*` metrics. The Grafana **Web Performance Vitals** dashboard includes `$network` and `$device` template variables for filtering, plus a **Desktop vs Mobile** comparison row.

## Budgets and alerts

In `budgets.yml` you can set thresholds for `lcp`, `inp`, and `cls` (e.g. `lcp: 2500`, `inp: 200`, `cls: 0.1`). Prometheus rules include **HighLCP** (LCP &gt; 2500 ms) and **HighINP** (INP &gt; 200 ms). An `inp` threshold stays inert until the check script interacts with the page.

## Dashboards

The **Technician: Web Performance Vitals** dashboard shows LCP, INP, CLS, and TTFB with the same good/warning thresholds (green ≤ good, yellow &lt; bad, red ≥ bad).
