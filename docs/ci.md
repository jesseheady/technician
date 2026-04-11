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
| **Docker build** | Builds the container image | On main branch |

## GitHub Actions

### Ready-to-use workflow

The project includes `.github/workflows/ci.yml` which runs on push to `main` and on PRs:

- **Build** -- compiles the binary and uploads as artifact
- **Test** -- runs all tests with race detector and uploads coverage
- **Lint** -- runs `go vet`
- **Validate** -- runs `technician validate` with example config and budgets
- **Validate Playwright** -- same as validate but installs Node.js + Chromium for browser checks
- **Docker build** -- builds the container image on `main` pushes

### Budget violation annotations

When you use `--output gha`, budget violations appear as GitHub Actions annotations -- inline on the PR diff at the relevant location:

```yaml
- name: Validate checks + budgets
  run: ./technician validate --config config/technician.yml --budget config/budgets.yml --output gha
```

### Canary workflow

The separate `canary.yml` workflow runs post-deployment synthetic checks:

```yaml
# Triggered by deployment success or manual dispatch
on:
  deployment_status:
  workflow_dispatch:
    inputs:
      target_url:
        description: "URL to check"
```

It uses the pre-built Docker image with Chromium included and uploads HAR artifacts.

### Artifacts

Both workflows upload check artifacts (HAR files, videos) so you can debug failures:

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
    node-version: 22

- name: Install Playwright + Chromium
  run: |
    cd internal/playwright/scripts
    npm init -y
    npm install playwright
    npx playwright install chromium
```

**Option B: Use the Docker image** (used in `canary.yml`)

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
  image: node:22-slim
  before_script:
    - apt-get update && apt-get install -y ca-certificates
    - cd internal/playwright/scripts && npm init -y && npm install playwright && npx playwright install chromium && cd -
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
      - image: node:22-slim
    steps:
      - checkout
      - attach_workspace:
          at: .
      - run: |
          cd internal/playwright/scripts
          npm init -y && npm install playwright
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

If your CI environment can't run Chromium (no graphical dependencies, limited RAM), you can still validate all non-browser checks. Create a check config that excludes the `playwright/` directory:

```bash
# Copy only non-browser checks
mkdir -p ci-config/checks
cp config/checks/http.yml config/checks/tcp.yml config/checks/dns.yml ci-config/checks/
cp config/technician.yml ci-config/

./technician validate --config ci-config/technician.yml --budget config/budgets.yml
```

Or keep a dedicated CI config directory checked in with the right subset of checks.

## Environment variables

Check configs support `${ENV_VAR}` expansion, useful for CI secrets:

```yaml
# checks/http.yml
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
