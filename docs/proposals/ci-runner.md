# Proposal: CI/CD pipeline runner mode

Use Technician as a CI pipeline step to enforce performance budgets, validate DOM structure, and capture visual artifacts as part of the build/deploy pipeline.

## Motivation

Synthetic monitoring catches production regressions after deploy. CI runner mode catches them before merge. Same probe definitions, same budget thresholds, same Playwright scripts — run in the pipeline against a preview/staging URL.

Use cases:
- **Performance gate**: fail the pipeline if LCP exceeds budget on a staging deploy
- **DOM validation**: assert that critical elements exist (nav, footer, checkout button)
- **Visual regression**: screenshot comparison against committed baselines
- **Layout shift detection**: flag CLS regressions before they ship
- **HAR analysis**: detect new third-party scripts or unexpected resource bloat
- **Lighthouse-like reporting**: generate a performance report as a pipeline artifact

## Proposed interface

### Subcommand

```bash
technician check \
  --config probes/ci.yml \
  --budgets budgets.yml \
  --base-url "https://preview-${CI_COMMIT_SHA}.example.com" \
  --artifacts ./test-results/ \
  --format json|junit|markdown
```

### Flags

| Flag | Purpose | Default |
|------|---------|---------|
| `--config` | Probe definitions (same YAML format as worker mode) | Required |
| `--budgets` | Budget thresholds file | Optional (no budget gate without it) |
| `--base-url` | Override `base_url` in all probes (for preview/staging URLs) | None (uses config value) |
| `--artifacts` | Directory for screenshots, HAR files, videos | `./technician-artifacts/` |
| `--format` | Output format: `json`, `junit` (for CI test reporters), `markdown` (for PR comments) | `json` |
| `--fail-on` | What triggers exit code 1: `budget`, `probe-failure`, `any` | `any` |
| `--screenshot` | Capture screenshot after each probe | `true` |
| `--video` | Record video of Playwright probes | `false` |
| `--compare` | Directory of baseline screenshots for visual regression | None |
| `--threshold` | Pixel diff threshold for visual comparison (0.0–1.0) | `0.01` |

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | All probes passed, all budgets met |
| 1 | One or more budget violations or probe failures |
| 2 | Infrastructure error (config parse failure, browser launch failure, etc.) |

### Example output (JSON)

```json
{
  "summary": {
    "total": 3,
    "passed": 2,
    "failed": 1,
    "duration_ms": 12450
  },
  "results": [
    {
      "name": "homepage (desktop)",
      "success": true,
      "duration_ms": 4200,
      "vitals": { "lcp": 1800, "cls": 0.02, "inp": 45 },
      "budgets": [
        { "metric": "lcp", "threshold": 2500, "actual": 1800, "passed": true },
        { "metric": "cls", "threshold": 0.1, "actual": 0.02, "passed": true }
      ],
      "artifacts": {
        "screenshot": "test-results/homepage-desktop.png",
        "har": "test-results/homepage-desktop.har"
      }
    },
    {
      "name": "homepage (mobile 3g)",
      "success": true,
      "duration_ms": 8250,
      "vitals": { "lcp": 6500, "cls": 0.05, "inp": 120 },
      "budgets": [
        { "metric": "lcp", "threshold": 6000, "actual": 6500, "passed": false }
      ],
      "artifacts": {
        "screenshot": "test-results/homepage-mobile-3g.png"
      }
    }
  ]
}
```

### JUnit output (for CI test reporters)

```xml
<testsuites name="technician" tests="3" failures="1" time="12.45">
  <testsuite name="budgets" tests="5" failures="1">
    <testcase name="homepage (desktop) / lcp" time="4.2"/>
    <testcase name="homepage (mobile 3g) / lcp" time="8.25">
      <failure message="LCP 6500ms exceeds budget 6000ms"/>
    </testcase>
  </testsuite>
</testsuites>
```

## CI platform integration

### GitHub Actions

```yaml
- name: Performance check
  run: |
    technician check \
      --config probes/ci.yml \
      --budgets budgets.yml \
      --base-url "${{ env.PREVIEW_URL }}" \
      --artifacts ./test-results/ \
      --format junit

- name: Upload results
  if: always()
  uses: actions/upload-artifact@v4
  with:
    name: performance-results
    path: ./test-results/

- name: Test report
  if: always()
  uses: dorny/test-reporter@v1
  with:
    name: Performance Budgets
    path: ./test-results/results.xml
    reporter: java-junit
```

### GitLab CI

```yaml
performance:
  stage: test
  image: technician:latest
  script:
    - technician check
        --config probes/ci.yml
        --budgets budgets.yml
        --base-url "$CI_ENVIRONMENT_URL"
        --artifacts ./test-results/
        --format junit
  artifacts:
    when: always
    paths:
      - test-results/
    reports:
      junit: test-results/results.xml
```

### Docker (any CI)

```bash
docker run --rm \
  -v $(pwd)/probes:/etc/technician/probes:ro \
  -v $(pwd)/budgets.yml:/etc/technician/budgets.yml:ro \
  -v $(pwd)/test-results:/artifacts \
  technician:latest \
  technician check \
    --config /etc/technician/probes/ci.yml \
    --budgets /etc/technician/budgets.yml \
    --base-url "https://staging.example.com" \
    --artifacts /artifacts/
```

## CI-specific probe config

A CI probe config is the same format as worker mode, but typically:
- Targets a preview/staging URL (overridden by `--base-url`)
- Runs each probe exactly once (no schedule field needed)
- May include DOM assertions in Playwright scripts

```yaml
# probes/ci.yml
- name: "homepage (desktop)"
  group: CI
  script: playwright/homepage_check.js
  timeout: 30s

- name: "homepage (mobile 4g)"
  group: CI
  script: playwright/homepage_check.js
  network: "4g"
  device: "iPhone 14"
  timeout: 60s

- name: "checkout flow"
  group: CI
  script: playwright/checkout_flow.js
  timeout: 90s
```

## Visual regression

### How it works

1. Baseline screenshots are committed to the repo (e.g., `baselines/homepage-desktop.png`)
2. CI runs `technician check --compare baselines/`
3. For each probe, a screenshot is taken after the probe script completes
4. Pixel-level diff against the baseline using Go image comparison (no external tools)
5. If diff exceeds `--threshold`, the probe is marked as failed
6. Diff image saved to artifacts (highlights changed regions)

### Baseline management

```bash
# Generate new baselines
technician check \
  --config probes/ci.yml \
  --base-url "https://staging.example.com" \
  --artifacts baselines/

# Commit baselines
git add baselines/
git commit -m "Update visual baselines"
```

### PR comment (markdown format)

When `--format markdown`, output can be piped to a PR comment via `gh pr comment`:

```bash
technician check --format markdown > results.md
gh pr comment $PR_NUMBER --body-file results.md
```

Output:

```markdown
## Performance Check Results

| Probe | LCP | CLS | INP | Budget | Screenshot |
|-------|-----|-----|-----|--------|------------|
| homepage (desktop) | 1800ms | 0.02 | 45ms | PASS | [view](test-results/homepage-desktop.png) |
| homepage (mobile 3g) | 6500ms | 0.05 | 120ms | **FAIL** (LCP) | [view](test-results/homepage-mobile-3g.png) |
```

## Implementation plan

### Phase 1: `technician check` subcommand
- Parse `--config`, `--budgets`, `--base-url`, `--artifacts` flags
- Run all probes sequentially (no scheduler, no cron)
- Evaluate budgets against results
- Output JSON to stdout, save artifacts to disk
- Exit 0/1 based on `--fail-on`
- Reuses existing probe runners (HTTP, Playwright) and budget evaluator

### Phase 2: Output formats
- JUnit XML for CI test reporters
- Markdown for PR comments
- Screenshot capture after each Playwright probe (already supported via video path)

### Phase 3: Visual regression
- Pure Go pixel diff (no ImageMagick dependency)
- Baseline comparison with configurable threshold
- Diff image generation (overlay with highlighted regions)
- `--update-baselines` flag to regenerate

### Phase 4: PR integration
- GitHub Actions action (`uses: m0nkey/technician-action@v1`)
- Auto-comment on PR with results table
- Status check integration (pass/fail on the PR)

## What already exists vs. what's new

| Component | Exists today | New for CI mode |
|-----------|-------------|-----------------|
| Probe runners (HTTP, Playwright) | Yes | Reuse as-is |
| Budget evaluator | Yes | Reuse as-is |
| Budget YAML format | Yes | Reuse as-is |
| Probe YAML format | Yes | Reuse (ignore `schedule` field) |
| Network throttling | Yes | Reuse as-is |
| Device emulation | Yes | Reuse as-is |
| Web Vitals collection | Yes | Reuse as-is |
| HAR capture | Yes | Reuse as-is |
| `check` subcommand | No | New (~200 LOC) |
| JUnit output | No | New (~100 LOC) |
| Markdown output | No | New (~100 LOC) |
| Visual regression | No | New (~300 LOC) |
| GitHub Action wrapper | No | New (separate repo) |

Most of the CI runner is wiring — the probe and budget logic is already built. The `check` subcommand is primarily a different execution model (run-once vs. scheduled) with structured output.
