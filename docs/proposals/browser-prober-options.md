# Proposal: Browser checker implementation options

> **Status:** partially superseded. [#66](https://github.com/jesseheady/technician/issues/66) settled *where the browser runs*: `mode: managed` connects to a stock upstream Playwright server rather than launching Chromium in the worker image. This document still stands for the separate question of *which client library drives it* (Playwright Node vs rod vs chromedp), since the Node subprocess architecture below is unchanged in either mode.

Evaluate alternatives for Technician's browser check runtime. The current implementation spawns a Node.js subprocess running Playwright. This proposal compares four paths forward.

## Current architecture

```
Go orchestrator
  └─ exec.CommandContext("node", "run.js", configJSON)
       └─ Playwright (Node.js)
            └─ Chromium (CDP)
```

**What run.js does today:**
1. Launches Chromium via Playwright
2. Creates browser context with HAR recording, device emulation, optional video
3. Applies network throttling via CDP session (`Network.emulateNetworkConditions`)
4. Loads and executes a user-provided JS check script (`require(scriptPath)`)
5. Collects Web Vitals (TTFB, FCP, LCP, CLS, INP) via `web-vitals` library import
6. Parses HAR, outputs structured JSON to stdout
7. Go checker deserializes JSON into `check.Result`

**Current costs:**
- Docker image: +~180 MB for Node.js base, +npm install step, +`npx playwright install` (~600 MB Chromium)
- Per-check: ~40 MB RSS for Node.js process + ~150–300 MB for Chromium
- Cold start: ~2–3s (Node.js boot + Chromium launch)
- Check scripts must be JavaScript files

## Options

### Option A: Keep Node.js Playwright (status quo)

No changes. Continue spawning `node run.js` with JSON config.

**Pros:**
- Already working and tested
- Playwright has the most mature browser automation API (auto-waiting, selectors, network interception)
- HAR recording is built-in (`recordHar` context option)
- Video recording is built-in (`recordVideo` context option)
- User check scripts are JS files — easy to write, widely understood
- `web-vitals` library import works natively in the browser context
- Multi-browser support available (Firefox, WebKit) if needed later

**Cons:**
- Node.js runtime adds ~180 MB to Docker image and ~40 MB RSS per invocation
- Subprocess spawning adds latency and complexity (JSON serialization, stderr parsing)
- Two languages in the codebase (Go + JS)
- npm dependency management (package.json, node_modules, version pinning)
- `npx playwright install` downloads browsers separately from the Go build

**Best for:** Teams that need user-authored JS check scripts or want to minimize implementation risk. Playwright has the broadest browser API of the options listed.

### Option B: chromedp (pure Go, CDP direct)

Replace Node.js Playwright with chromedp. Browser checks become Go code that speaks CDP directly to Chromium.

```
Go orchestrator
  └─ chromedp.Run(ctx, actions...)
       └─ Chromium (CDP)
```

**Pros:**
- Pure Go — no Node.js, no subprocess, no JSON bridge
- Docker image drops ~180 MB (no Node.js base layer)
- Checks run as goroutines, not subprocesses
- Mature: 12.8k stars, actively maintained, 6.9k dependents
- Full CDP access: network throttling, device emulation, JS evaluation all native
- Single binary deployment

**Cons:**
- Lower-level API than Playwright — no auto-waiting, manual selector strategies
- HAR capture requires manual assembly from CDP `Network` domain events (`RequestWillBeSent`, `ResponseReceived`, `LoadingFinished`)
- No built-in video recording (would need `Page.startScreencast` + ffmpeg, or screenshots-to-video)
- Check scripts become Go code, not user-authored JS files (less accessible to non-Go developers)
- Known issues: WaitReady race conditions, fixed-size event buffer can deadlock under high concurrency, single event loop blocks slow handlers
- Web Vitals collection requires `chromedp.Evaluate()` with injected JS (same approach, just different API)

**Migration effort:**

| Current feature | chromedp equivalent | Difficulty |
|---|---|---|
| Browser launch | `chromedp.NewExecAllocator` | Trivial |
| Device emulation | `chromedp.Emulate(device)` | Trivial |
| Network throttling | `cdproto/network.EmulateNetworkConditions` | Trivial |
| Navigate + wait | `chromedp.Navigate` + `chromedp.WaitReady` | Easy |
| JS evaluation (Web Vitals) | `chromedp.Evaluate` | Easy |
| HAR recording | Manual CDP event listeners → HAR struct | Medium (200–300 LOC) |
| Video recording | `Page.startScreencast` → ffmpeg or skip | Hard (or drop feature) |
| User JS check scripts | Must rewrite as Go functions | Breaking change |

**Best for:** Teams that want a single Go binary, don't need user-authored JS check scripts, and can accept building HAR capture manually.

### Option C: rod (pure Go, CDP with higher-level API)

Replace Node.js Playwright with rod. Similar to chromedp but with a friendlier API, browser pool management, and automatic browser downloading.

```
Go orchestrator
  └─ rod.New().MustConnect()
       └─ Chromium (CDP)
```

**Pros:**
- Pure Go — same benefits as chromedp (no Node.js, single binary)
- Higher-level API than chromedp: automatic element waiting, cleaner method chaining
- Built-in browser pool (`rod.NewBrowserPool`) for concurrent checks
- `rod/lib/launcher` handles Chromium download and versioning automatically
- HAR capture documented as a supported feature
- Better concurrency architecture than chromedp (decode-on-demand, no single event loop)
- 6.8k stars, 57 contributors

**Cons:**
- Latest release July 2024 — 8+ months old (worth monitoring maintenance pace)
- Bot detection issues reported (#1208) — may affect checks that hit bot-protected sites
- Same "check scripts must be Go" limitation as chromedp
- No built-in video recording
- Smaller ecosystem than chromedp (fewer third-party examples, Stack Overflow answers)
- Pre-v1.0

**Migration effort:**

| Current feature | rod equivalent | Difficulty |
|---|---|---|
| Browser launch | `rod.New().MustConnect()` | Trivial |
| Device emulation | CDP `Emulation` domain via `page.MustEval` or proto | Easy |
| Network throttling | `proto.NetworkEmulateNetworkConditions` | Trivial |
| Navigate + wait | `page.Navigate(url).WaitLoad()` | Trivial |
| JS evaluation (Web Vitals) | `page.MustEval` | Easy |
| HAR recording | Built-in HAR support | Easy |
| Video recording | Not built-in | Hard (or drop feature) |
| User JS check scripts | Must rewrite as Go functions | Breaking change |

**Best for:** Teams that want pure Go with a friendlier API than chromedp, need browser pooling, and value rod's concurrency model for running many checks.

### Option D: playwright-go (Go bindings, still requires Node.js)

Community-maintained Go bindings that talk to the Playwright server via RPC.

```
Go orchestrator
  └─ playwright-go (Go API)
       └─ Playwright server (embedded Node.js, ~50 MB)
            └─ Chromium (CDP)
```

**Pros:**
- Go API surface — write checks in Go, not JS
- API-compatible with Node.js Playwright (auto-waiting, selectors, HAR, video — all built-in)
- Multi-browser support (Chromium, Firefox, WebKit)
- Latest release February 2026

**Cons:**
- **Still ships Node.js** — embeds ~50 MB Node.js runtime. Does not eliminate the Node.js dependency.
- Community-maintained, **actively seeking new maintainers** (sustainability risk)
- Incomplete test coverage
- Parallel execution with multiple browser engines can thrash small systems
- Adds an RPC layer between Go and the browser (stdio-based)

**Best for:** Teams that want a Go API but need Playwright's full feature set (HAR, video, multi-browser) and accept the Node.js dependency.

## Feature matrix

| Feature | A: Node.js Playwright | B: chromedp | C: rod | D: playwright-go |
|---|---|---|---|---|
| **Pure Go (no Node.js)** | No | Yes | Yes | No |
| **Docker image delta** | Baseline | -180 MB | -180 MB | ~-130 MB |
| **Network throttling** | Built-in | CDP native | CDP native | Built-in |
| **Device emulation** | Built-in | CDP native | CDP native | Built-in |
| **Web Vitals (JS eval)** | Native | `Evaluate()` | `MustEval()` | `Evaluate()` |
| **HAR recording** | Built-in | Manual (~250 LOC) | Documented | Built-in |
| **Video recording** | Built-in | Manual (hard) | Manual (hard) | Built-in |
| **User JS check scripts** | Yes | No (Go only) | No (Go only) | No (Go only) |
| **Browser pooling** | Manual | Manual | Built-in | Manual |
| **Auto-waiting** | Yes | No | Yes | Yes |
| **Maintenance risk** | Low (Microsoft) | Low (active) | Medium (release pace) | High (seeking maintainers) |
| **Migration effort** | None | Medium | Medium | Medium |

## Recommendation

**Short term: Stay with Option A (Node.js Playwright).** It works, covers all features, and the JS check script model gives users the most control over what the browser does. The Node.js overhead (~40 MB RSS, ~180 MB image) is manageable at current scale.

**Medium term: Evaluate Option C (rod) as a parallel checker.** Add a `rod`-based `BrowserChecker` alongside the existing `PlaywrightChecker`. This lets us:
- Compare resource usage and reliability side-by-side
- Validate HAR capture, Web Vitals collection, and network throttling in production
- Offer a "slim" Docker image without Node.js for deployments that only need Go-defined checks
- Keep JS check support via the existing Playwright path for users who need it

**Not recommended: Option D (playwright-go).** It doesn't solve the core problem (Node.js dependency) and adds maintenance risk.

## If we proceed with rod (Option C)

### Phase 1: Core checker
- New `internal/check/browser.go` with `BrowserChecker` struct
- `rod.New().MustConnect()` lifecycle with context timeout
- Navigate to URL, wait for load
- CDP network throttling via `proto.NetworkEmulateNetworkConditions`
- Device emulation via `proto.EmulationSetDeviceMetricsOverride` + `proto.NetworkSetUserAgentOverride`
- JS-injected Web Vitals collection (same `web-vitals` import approach, via `page.MustEval`)

### Phase 2: HAR capture
- Intercept CDP `Network` domain events
- Build HAR 1.2 JSON from request/response pairs
- Wire into existing `HARData` struct and metrics

### Phase 3: Check definition model
- Define browser checks in Go as functions matching `func(page *rod.Page, ctx ProbeContext) error`
- Built-in checks: page load + vitals, multi-step navigation, form submission
- YAML config references built-in check names (not script file paths)

### Phase 4: Slim Docker image
- Multi-stage build: `FROM chromedp/headless-shell` (~300 MB) instead of Node.js + Playwright
- Or use `rod/lib/launcher` to download Chromium at container startup
- Feature flag: `--browser-engine=rod` vs `--browser-engine=playwright`

### What we'd lose (vs Playwright)
- User-authored JS check scripts (must define checks in Go or accept a limited DSL)
- Built-in video recording (would need to implement or drop)
- Multi-browser support (rod is Chromium-only)
- Playwright's selector engine (`:text()`, `:has()`, etc.)

### What we'd gain
- ~180 MB smaller Docker image
- ~40 MB less RSS per check run
- No subprocess spawning overhead
- Single language codebase
- Browser pool for concurrent checks
- Faster cold starts (no Node.js boot)
