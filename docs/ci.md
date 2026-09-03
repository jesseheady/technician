# Continuous integration

How to run Technician in CI pipelines. GitHub Actions is first-class with a ready-to-use workflow, but the same patterns work on any CI platform.

## What CI checks

| Job | What it does | Required? |
|-----|-------------|-----------|
| **Build** | Compiles the Go binary | Yes |
| **Test** | `go test -race ./...` with coverage | Yes |
| **Lint** | `go vet ./...` | Yes |
| **Validate** | Runs all checks once, checks performance budgets | Recommended |
| **Validate (Playwright)** | Same as validate, but with Chromium installed for browser checks | Yes |
| **Docker build** | Builds the container image | Main pushes + PRs touching image inputs |
| **Scan container image** | Trivy scan of the built image for fixable CRITICAL/HIGH CVEs | Main pushes, weekly, + PRs touching image inputs |

## GitHub Actions

### Ready-to-use workflow

The project includes `.github/workflows/ci.yml` which runs on push to `main` and on PRs:

- **Build** -- compiles the binary and uploads as artifact
- **Test** -- runs all tests with race detector and uploads coverage
- **Lint** -- runs `go vet`
- **Validate** -- runs `technician validate` with example config and budgets
- **Validate Playwright** -- same as validate but installs Node.js + Chromium for browser checks
- **Docker build** -- builds the container image on `main` pushes and on PRs that touch image inputs (`Dockerfile`, `.dockerignore`, `internal/playwright/scripts/`), so a broken image build blocks merge via the `CI Passed` gate
- **Scan container image** -- builds and Trivy-scans the image on `main` pushes, weekly, and on PRs that touch image inputs; a fixable CRITICAL/HIGH CVE blocks merge via the `Container scan passed` gate

### Budget violation annotations

When you use `--output gha`, budget violations appear as GitHub Actions annotations -- inline on the PR diff at the relevant location:

```yaml
- name: Validate checks + budgets
  run: ./technician validate --config config/technician.yml --budget config/budgets.yml --output gha
```

### Artifacts

The CI workflow uploads check artifacts (HAR files, videos) so you can debug failures:

```yaml
- uses: actions/upload-artifact@v7
  with:
    name: check-results
    path: /tmp/technician/artifacts/
    retention-days: 7
```

### Playwright in CI

Browser checks need Node.js + Chromium. Two approaches:

**Option A: Install in the job** (used in `ci.yml`)

```yaml
- uses: actions/setup-node@v4
  with:
    node-version: 24

- name: Install Playwright + Chromium
  run: |
    cd internal/playwright/scripts
    npm ci
    npx playwright install chromium
```

**Option B: Use the pre-built Docker image** (Chromium included)

```yaml
container:
  image: ghcr.io/your-org/technician:latest
```

The Docker image includes Chromium, so no extra setup is needed.

### Resource considerations

GitHub Actions standard runners have 7 GB RAM and 2 CPUs. With `max_browsers: 2` (the default), you can run 3-4 concurrent Playwright checks comfortably. If you have more browser checks, set `max_browsers: 1` in your CI config to serialize them:

```yaml
# ci-config/technician.yml
playwright:
  max_browsers: 1  # serialize browser checks on CI runners
```

See [Playwright scaling](playwright-scaling.md) for detailed resource analysis.

## Generic CI

The `technician validate` command is the CI integration point. It works identically on any platform.

### Minimal pipeline

```bash
# 1. Build
CGO_ENABLED=0 go build -ldflags="-s -w" -o technician .

# 2. Run tests
go test -race ./...

# 3. Lint
go vet ./...

# 4. Validate checks against budgets
./technician validate \
  --config config/technician.yml \
  --budget config/budgets.yml \
  --output text
# Exit code: 0 = pass, 1 = violations found
```

### Output formats

| Format | Flag | Best for |
|--------|------|----------|
| `text` | `--output text` | Human-readable terminal output |
| `json` | `--output json` | Machine-parseable, pipe to `jq` or reporting tools |
| `gha` | `--output gha` | GitHub Actions annotations (inline on PR diffs) |

### GitLab CI example

```yaml
stages:
  - build
  - test
  - validate

build:
  stage: build
  image: golang:1.26
  script:
    - CGO_ENABLED=0 go build -ldflags="-s -w" -o technician .
  artifacts:
    paths:
      - technician

test:
  stage: test
  image: golang:1.26
  script:
    - go test -race -coverprofile=coverage.out ./...
  artifacts:
    reports:
      coverage_report:
        coverage_format: cobertura
        path: coverage.out

validate:
  stage: validate
  image: node:24-slim
  before_script:
    - apt-get update && apt-get install -y ca-certificates
    - cd internal/playwright/scripts && npm ci && npx playwright install chromium && cd -
  script:
    - ./technician validate --config examples/technician.yml --budget examples/budgets.yml --output json
  artifacts:
    paths:
      - /tmp/technician/artifacts/
    when: always
```

### CircleCI example

```yaml
version: 2.1

jobs:
  build-and-test:
    docker:
      - image: golang:1.26
    steps:
      - checkout
      - run: go test -race ./...
      - run: go vet ./...
      - run: CGO_ENABLED=0 go build -ldflags="-s -w" -o technician .
      - persist_to_workspace:
          root: .
          paths: [technician]

  validate:
    docker:
      - image: node:24-slim
    steps:
      - checkout
      - attach_workspace:
          at: .
      - run: |
          cd internal/playwright/scripts
          npm ci
          npx playwright install chromium
      - run: ./technician validate --config examples/technician.yml --budget examples/budgets.yml --output json
      - store_artifacts:
          path: /tmp/technician/artifacts/
```

### Jenkins (Declarative Pipeline)

```groovy
pipeline {
    agent any
    stages {
        stage('Build') {
            steps {
                sh 'CGO_ENABLED=0 go build -ldflags="-s -w" -o technician .'
            }
        }
        stage('Test') {
            steps {
                sh 'go test -race ./...'
                sh 'go vet ./...'
            }
        }
        stage('Validate') {
            steps {
                sh './technician validate --config examples/technician.yml --budget examples/budgets.yml --output json'
            }
        }
    }
    post {
        always {
            archiveArtifacts artifacts: '/tmp/technician/artifacts/**', allowEmptyArchive: true
        }
    }
}
```

## Validate without Playwright

If your CI environment can't run Chromium (no graphical dependencies, limited RAM), skip browser checks with `--exclude-type playwright` — no separate config needed:

```bash
./technician validate --config config/technician.yml --budget config/budgets.yml --exclude-type playwright
```

`--check-type playwright` does the inverse (browser checks only), which is how the two CI validate jobs split the work.

## Retries in validate

`technician validate` applies each check's `retry:` policy, the same way the scheduler
does in production. A check that fails and then passes on a retry counts as a pass, so a
short outage at a third party does not fail the build.

Retries cost time only when a check fails. A run in which all checks pass takes the same
time as before. A run in which a check fails waits for that check's `delay:` and then runs
it again, and validate runs checks one after another, so the delays add up. With the
example config, a run in which every non-browser check fails waits about 53 seconds more
than a run with no retries.

Set `retry: {count: 0}` on a check to switch this off for that check.

## Environment variables

Check configs support `${ENV_VAR}` expansion, useful for CI secrets:

```yaml
# checks.yml
- name: staging API
  url: ${STAGING_URL}/health
  headers:
    Authorization: "Bearer ${API_TOKEN}"
```

```yaml
# GHA
env:
  STAGING_URL: ${{ secrets.STAGING_URL }}
  API_TOKEN: ${{ secrets.API_TOKEN }}
```

## Next steps

- [Testing and e2e validation](testing-and-e2e.md) -- unit tests, integration checks, full e2e
- [Playwright scaling](playwright-scaling.md) -- resource analysis, concurrency controls, dedicated runner architecture
- [Deployment sizing](deployment-sizing.md) -- resource requirements for all deployment modes
