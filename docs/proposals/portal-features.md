# Proposal: Status page and visualization roadmap

What to add to Technician's built-in portal, what to leave to Grafana, and what to get for free from Playwright.

## Philosophy

Three tiers of visualization, each with a clear owner:

| Tier | Owner | What it shows | Examples |
|------|-------|---------------|---------|
| **Real-time operational** | Technician status page (`:9590/`) | Is it up? How fast? What failed? | Status bars, check detail modal, group view |
| **Historical trends** | Grafana dashboards | How has it performed over time? | LCP trend, uptime %, budget violation rate |
| **Deep protocol analysis** | Playwright Trace Viewer / HAR viewers | What exactly happened in this request? | Network waterfall, DOM snapshots, console logs |

The goal is to avoid building a custom waterfall renderer or timeline viewer when Playwright already ships one. Similarly, avoid reimplementing time-series dashboards when Grafana already does this.

## Tier 1: Status page enhancements

Things that belong on the built-in status page because they're real-time, operational, and don't need Grafana.

### Already built
- Per-check status bars with color coding (up/down/error)
- Check grouping by category
- Detail modal with duration, error message, timestamp
- UTC + local time tooltips
- Auto-refresh

### Proposed additions

**Check detail expansion** (low effort)
- Show last N results in the modal (not just the latest)
- Display Web Vitals inline when available (LCP, CLS, INP as colored badges)
- Show network/device labels for Playwright checks
- Link to artifacts (screenshot, video, trace) when available

**Artifact links** (low effort)
- Serve artifacts from `/artifacts/{check}/{timestamp}/` on the status page
- Screenshot thumbnail in the check detail modal
- "View trace" link that opens Playwright Trace Viewer (see Tier 3)
- "View video" link for Playwright video recordings
- Retention policy: keep last N runs per check, prune on schedule

**Budget status indicator** (low effort)
- Green/yellow/red badge per check showing budget status
- Tooltip shows which metric violated and by how much
- Data comes from the in-memory budget evaluation (already computed in worker.go)

**Incident timeline** (medium effort)
- Collapsible list of state transitions (up→down, down→up) per check
- Shows duration of each incident
- Data source: in-memory ring buffer (short term) or Prometheus query (long term)

### What NOT to build on the status page
- Time-series charts (use Grafana)
- HAR waterfall (use Playwright Trace Viewer)
- Multi-site comparison matrix (use Grafana with region filter)
- Alert management (use Alertmanager/Grafana alerting)

## Tier 2: Grafana dashboard additions

Things that are better as Grafana panels because they're historical, comparative, or need flexible time ranges.

### Already built
- Uptime overview (check status matrix, uptime %)
- HTTP timing breakdown (DNS, TLS, TTFB stacked)
- Web Performance Vitals (LCP, INP, CLS gauges and trends)
- HAR resource analysis (resource count, transfer size by type)
- Budget violations (violation rate over time, current violations table)
- Cross-dashboard navigation links

### Proposed additions

**CI run results panel** (for ci-runner proposal)
- New dashboard: "Technician: CI Runs"
- CI results pushed to Prometheus with labels: `ci_pipeline`, `ci_commit`, `ci_branch`
- Panels: budget pass/fail per commit, LCP trend across deploys, regression detection
- Annotation markers on existing dashboards showing deploy events

**Check comparison panel**
- Side-by-side comparison of the same check across network profiles
- Already partially built (Desktop vs Mobile row in performance-vitals)
- Extend to show budget threshold as a horizontal line overlay

**SLO tracking panel**
- Monthly/weekly uptime SLO (e.g., 99.9% target)
- `1 - avg_over_time(technician_check_up[30d])` as error budget burn
- Fits on uptime-overview dashboard as a new row

### What NOT to build as Grafana dashboards
- Individual check run detail (no good Grafana panel for "show me this one HAR")
- Video playback (Grafana doesn't do video)
- Screenshot comparison (need pixel-level diff, not time-series)

## Tier 3: Playwright Trace Viewer (free)

This is the biggest win. Playwright's built-in Trace Viewer (`npx playwright show-trace trace.zip`) provides sitespeed.io/WebPageTest-level detail with zero custom UI work:

- Full network waterfall with timing breakdown per request
- DOM snapshots at each action step
- Console log capture
- Source code mapping to actions
- Screenshots at each step (filmstrip)
- Call stack for each action

### What we need to do

**Capture traces** (trivial change to run.js):
```javascript
// In run.js context creation
contextOpts.tracing = true;

// Before closing context
await context.tracing.stop({ path: '/tmp/technician-traces/trace.zip' });
```

That's ~3 lines of code. Playwright does the rest.

**Serve traces** (two options):

**Option A: Static file serving (simplest)**
- Save trace.zip to the artifacts directory per check run
- Technician serves `/artifacts/{check}/{timestamp}/trace.zip`
- User downloads and runs `npx playwright show-trace trace.zip` locally
- Or opens `https://trace.playwright.dev/` and uploads the file (Playwright's hosted viewer)

**Option B: Embedded Trace Viewer (nicer UX)**
- Playwright Trace Viewer is a static web app
- Bundle it in the Docker image or serve from CDN
- Status page links directly to `trace.playwright.dev/?trace=<url>` with the artifact URL
- No local tooling needed — works in the browser

**Option B is recommended.** The Playwright team hosts `trace.playwright.dev` publicly. We just need to generate a shareable URL to the trace.zip file. For local/private deployments, the file is accessible on the local network.

### What this gives us (for free)

| Feature | sitespeed.io | WebPageTest | Playwright Trace Viewer |
|---------|-------------|-------------|------------------------|
| Network waterfall | Yes | Yes | Yes |
| Request/response headers | Yes | Yes | Yes |
| DOM snapshot per step | No | No | Yes |
| Console logs | Partial | No | Yes |
| Filmstrip (screenshots per step) | Yes | Yes | Yes |
| Action timeline | No | No | Yes |
| CPU profiling | Yes | Yes | No |
| Lighthouse scores | Yes (integrated) | Yes | No |
| Custom setup | Heavy (Docker + S3 + Graphite) | SaaS or self-hosted | Zero (3 lines of code) |

The tradeoff: no CPU profiling or Lighthouse scores. But for synthetic monitoring (not performance auditing), the Trace Viewer covers 90% of the "what happened?" question.

## Tier 3.5: HAR Viewer (if needed beyond traces)

If trace files are too heavy for every run (they include screenshots), a lighter option is to serve raw HAR files with a browser-based viewer.

**Options:**
- Link to `nicedoc.io/nicedoc-io/har-viewer` or similar open-source HAR viewer
- Embed `nicedoc/har-viewer` as a static asset in the Docker image
- Serve HAR as JSON and render with a simple waterfall component (many open-source options)

This is lower priority than traces — the Trace Viewer already shows network waterfall. HAR is useful for:
- Sharing with non-technical stakeholders (HAR is a standard format)
- Import into browser DevTools (Chrome: Network tab → Import HAR)
- Third-party analysis tools (WebPageTest HAR import, Charles Proxy)

We already capture HAR files — just need to persist and serve them rather than only parsing for metrics.

## Implementation priority

| Priority | Feature | Effort | Value |
|----------|---------|--------|-------|
| **1** | Playwright trace capture (3 lines in run.js) | Trivial | High — unlocks Trace Viewer for free |
| **2** | Artifact serving on status page (`/artifacts/`) | Low | High — makes traces/screenshots/videos accessible |
| **3** | Screenshot capture per check run | Trivial | Medium — visual record of each check |
| **4** | Budget badge on status page | Low | Medium — at-a-glance budget health |
| **5** | Web Vitals display in check modal | Low | Medium — details without leaving the page |
| **6** | CI results to Prometheus (ci-runner proposal) | Medium | High — close the loop between CI and production |
| **7** | SLO tracking Grafana panel | Low | Medium — error budget visibility |
| **8** | Incident timeline on status page | Medium | Medium — outage history |
| **9** | Visual regression (ci-runner proposal) | Medium | Medium — layout change detection |
| **10** | Embedded HAR viewer | Medium | Low — traces cover most use cases |

## What we explicitly won't build

- **Custom waterfall renderer** — Playwright Trace Viewer and browser DevTools already do this
- **Lighthouse integration** — Different tool, different purpose. Use Lighthouse CI separately if needed.
- **RUM (Real User Monitoring)** — Technician is synthetic monitoring. RUM is a separate product category.
- **Custom charting library** — Grafana handles all time-series visualization
- **Alert notification UI** — Alertmanager and Grafana alerting handle this
- **Multi-tenant SaaS features** — Technician is self-hosted, single-tenant by design
