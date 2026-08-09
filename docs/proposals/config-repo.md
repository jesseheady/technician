# Proposal: Config repos — deploying Technician with private, version-controlled checks

**Status:** Proposal
**Goal:** Define the supported shape for operators who keep their check definitions, alert routing, and stack wiring in their own version-controlled repository, and identify the smallest set of changes to Technician that make that shape drift-free.

## Context

Technician already assumes configuration lives outside the product repo:

- `/config/` is gitignored, seeded by `cp -r examples/ config/` (`.gitignore`, `docs/getting-started.md`).
- `docker-compose.override.yml` is gitignored and is where local wiring currently lands.
- `LoadChecks` resolves `checks.yml`, then a `checks/` directory, **relative to the loaded `technician.yml`** (`internal/config/checks.go:389`). Config is already directory-portable.

What is *not* separated is the **stack definition**. `docker-compose.yml` bind-mounts `./config/...`, `./prometheus/...`, and `./dashboards/` as paths relative to the Technician working tree. An operator who wants private checks under version control today has three bad options: commit secrets into a fork, copy the compose file and let it silently drift from upstream, or carry the whole Go source as a submodule to get four YAML files.

This proposal names the pattern, draws the product/instance boundary, and specifies the minimal changes.

## The shape

This is the **app repo / config repo split**, the GitOps vocabulary from the Argo CD and Flux ecosystems. It also travels as *environment repo*, *deployment repo*, or *instance repo*. In Prometheus shops it is usually just "the monitoring-config repo."

The useful mental model: **Technician is a distribution; your repo is an instance of it.** The same relationship a Linux distribution has with `/etc`.

```
┌─────────────────────────────┐        ┌──────────────────────────────┐
│ technician (public)         │        │ <your>-monitoring (private)  │
│                             │        │                              │
│ image, binary, Helm chart   │──────▶ │ pins vX.Y.Z by digest        │
│ dashboards, rules, templates│ tagged │ checks/, technician.yml      │
│ compose topology            │release │ alert routing, budgets       │
│ examples/                   │        │ env/secret refs              │
└─────────────────────────────┘        └──────────────────────────────┘
```

The instance repo consumes released artifacts. It never forks, never vendors source, and never sends its check definitions upstream.

## Where the line falls

The test for which side something belongs on: **would a second operator want a different value?** If yes, it is instance.

| Product (Technician, tagged and released) | Instance (operator's private repo) |
|---|---|
| Image, binary, Helm chart | `checks/*.yml`, `technician.yml`, `budgets.yml` |
| Default dashboards, recording and alerting rules | Playwright flow scripts |
| Alertmanager template bodies (`prometheus/templates/`) | Alertmanager routing tree and receivers |
| Compose topology, service wiring, healthchecks | Version pins, hostnames, TLS, reverse proxy |
| `examples/` | Secret references (never secret values) |
| Prometheus scrape topology | Per-target `check_filter` selections |

Two entries deserve a note:

- **Alertmanager** splits across the line. Technician ships the notification *templates*; the operator owns the *routing tree and receivers*, because that encodes their on-call structure. The existing `${VAR}` substitution in the alertmanager service is already the correct seam for this.
- **Dashboards** are additive, not replaced. The instance repo mounts its own JSON alongside Technician's rather than editing shipped files, so upgrades stay clean.

## Consumption options

| Option | Mechanism | Verdict |
|---|---|---|
| **A. Published artifacts** | Instance repo pulls `ghcr.io/jesseheady/technician@sha256:…` plus a released stack bundle | **Recommended** for Compose deployments |
| **B. Helm via OCI** | Instance repo is `values.yaml` + real check files; chart pulled from an OCI registry | **Recommended** for Kubernetes |
| **C. Git submodule** | Instance repo submodules Technician at a tag | Works today with zero changes, but drags the full Go source in to obtain a handful of YAML files. Stopgap only. |
| **D. Fork with config in-tree** | Operator forks Technician, commits config alongside source | **Anti-pattern.** Destroys the ability to pull upstream cleanly. Should be named explicitly in the docs so operators do not drift into it. |

## Reference layout

### Compose flavour

```
acme-monitoring/                    # private
  .github/workflows/
    validate.yml                    # runs `technician validate` on the pinned image
    deploy.yml                      # deploys on tag
  technician/
    technician.yml                  # service config, check_filter
    checks/
      edge.yml
      internal.yml
      playwright/
        checkout_flow.js
    budgets.yml
  alertmanager/
    alertmanager.yml                # routing tree; ${VAR} placeholders for secrets
  dashboards/                       # instance-specific panels, additive
  stack/
    .env                            # TECHNICIAN_VERSION, external URLs (no secrets)
    compose.yaml                    # `include:` of the vendored bundle + overrides
  vendor/technician-stack/          # extracted release bundle, committed, replaced wholesale on upgrade
  VERSION                           # single source of the pin, bumped by Renovate
```

`vendor/technician-stack/` is committed rather than fetched at deploy time so the deployed topology is reviewable in a diff. Because it is replaced wholesale on upgrade and never hand-edited, drift is visible in code review instead of silent.

### Helm flavour

```
acme-monitoring/
  clusters/prod/
    values.yaml                     # image digest, origins, resources
    checks/                         # real .yml files, globbed into a ConfigMap
    Chart.yaml                      # dependency: technician chart from OCI, pinned
```

## Worked flow

1. Engineer opens a PR adding a check to `technician/checks/edge.yml`.
2. CI runs `technician validate` **on the exact pinned image**, against the mounted config directory. Invalid YAML, unknown check types, and bad `check_filter` values fail the PR.
3. Merge, tag.
4. Deploy job pulls the pinned digest and applies. No laptop in the path.
5. Renovate opens a PR bumping `VERSION` and the vendored bundle when a new Technician release lands. That PR is itself validated by step 2 before it can merge.

Step 2 is the central payoff. Private checks get tested against the exact version about to ship, on every change, without Technician ever seeing them.

## Minimal changes to Technician

Roughly 10% code, 90% packaging and documentation. None of these are required to *stand up* a config repo today; they are what stops it drifting.

### 1. Env-indirect the mount paths (rides on #232)

**This is mostly already scoped as [#232](https://github.com/jesseheady/technician/issues/232)**, which proposes replacing the individual file bind-mounts with directory mounts (`./config:/etc/technician:ro`) to fix inode-swap staleness on Docker Desktop. That work is the prerequisite here, and its issue already works through the Alertmanager and Grafana complications.

What this proposal adds on top is one line of indirection per mount:

```yaml
volumes:
-  - ./config:/etc/technician:ro
+  - ${TECHNICIAN_CONFIG_DIR:-./config}:/etc/technician:ro
   - technician_data:/var/lib/technician
```

Same for `${TECHNICIAN_DASHBOARDS_DIR:-./dashboards}` on Grafana. Effect: an instance repo never copies the compose file. It supplies env and points `-f` at the vendored one.

Notes carried over from #232 that matter here:

- **Alertmanager does not need the directory mount.** Its entrypoint renders `alertmanager.yml.tmpl` into `/etc/alertmanager`, so a read-only directory mount there breaks. For the config-repo case a file mount plus env indirection (`${TECHNICIAN_ALERTMANAGER_CONFIG:-./prometheus/alertmanager.yml}`) is sufficient and sidesteps the problem entirely. This also retires the gitignored `docker-compose.override.yml`, which exists today only to swap that one path.
- **Grafana provisioning** expects a fixed `datasources/`/`dashboards/`/`alerting/` layout, so those mounts stay as-is; only the dashboards content directory needs indirection.
- **A directory mount exposes everything under `config/`**, including `config/prod`, `config/dev`, `config/local`, and `config/pi`. Harmless, but #232's "confirm nothing unwanted leaks in" acceptance criterion applies.
- `config/` currently holds both `checks.yml` and `checks/`. `LoadChecks` prefers `checks.yml`, so behaviour is unchanged, but it becomes load-order-dependent rather than mount-determined and should be stated in the docs.

**Caveat to document:** Compose resolves relative paths against the project directory, so `TECHNICIAN_CONFIG_DIR` should be absolute in an instance repo, or the deploy should pass `--project-directory`.

### 2. Publish a versioned stack bundle

`release.yml` already builds tarballs and uploads release assets. Add one job that packages the deployable surface:

```
technician-stack-vX.Y.Z.tar.gz
  docker-compose.yml
  docker-compose.prod.yml
  prometheus/           # rules, scrape config, alertmanager templates, grafana provisioning
  dashboards/
  examples/
  LICENSE
```

**Build it with the same deterministic tar settings `scripts/build.sh` already uses** for the binary tarballs (`SOURCE_DATE_EPOCH`, `--sort=name`, `--owner=0 --numeric-owner`, `gzip -n`) — extend that script or add a sibling — so the stack bundle is reproducible like the rest of the release rather than shipping one reproducible and one non-reproducible artifact from the same tag. The bundle carries no Go binary, so it needs no `THIRD_PARTY_LICENSES` (the image ships its own, per #277); it includes `LICENSE` for consistency, since the compose topology and configs are Technician's own MIT-licensed files.

Effect: an instance repo obtains the full deployable topology by tag, with no submodule and no source checkout. Combined with change 1, upgrading is "extract the new bundle over `vendor/`, review the diff, merge."

### 3. Helm: accept an existing ConfigMap

`values.yaml` inlines config as a string map (`values.yaml:35`), which caps out quickly against a real check tree and forces check YAML through Helm's escaping. Add an alternative source:

```yaml
config:
  existingConfigMap: ""   # when set, .Values.config.* inline keys are ignored
```

`templates/deployment.yaml` picks the volume source conditionally. Note that the `checksum/config` pod annotation cannot hash an externally managed ConfigMap, so with `existingConfigMap` set, rollout-on-change becomes the operator's responsibility (documented, not worked around).

### 4. Publish the Helm chart to OCI

`release.yml` does not package or push the chart today. Without this, the Helm flavour of the split has no pinnable artifact and falls back to option C. `helm package` plus `helm push oci://ghcr.io/jesseheady/charts` is a small additive step and is the Kubernetes equivalent of change 2.

### 5. Documentation

A `docs/config-repo.md` guide: the pattern and its name, the boundary table, both reference layouts, the CI validate recipe, and an explicit statement of the fork anti-pattern. This is the deliverable that makes the other four legible.

## Non-goals

- **A git-sync sidecar or remote config loader inside Technician.** CI checkout plus a bind mount already solves this. Building it in would mean owning credentials, network failure modes, and reload semantics permanently.
- **A config-repo scaffolding generator.** A documented layout is sufficient; a `technician init --config-repo` command is speculative until operators ask for it.
- **Shipping a reference private repo.** The layout in this document is the reference.

## Interaction with existing features and issues

`check_filter` (`docs/multi-target-deployment.md`, [#15](https://github.com/jesseheady/technician/issues/15)) maps onto this cleanly and was arguably designed for it: one canonical `checks/` tree in the instance repo, per-target `technician.yml` files selecting subsets, every worker scraping into the same Prometheus. A config repo is the natural home for that arrangement.

| Issue | Relationship |
|---|---|
| [#232](https://github.com/jesseheady/technician/issues/232) Directory mounts | **Prerequisite** for change 1. Independently motivated (bind-mount staleness); this proposal adds env indirection on top. |
| [#246](https://github.com/jesseheady/technician/issues/246) Pull-based prod overlay (closed) | **Already shipped.** `docker-compose.prod.yml` is the building block that lets an instance repo pull rather than build. |
| [#21](https://github.com/jesseheady/technician/issues/21) IaC templates | **Adjacent, not overlapping.** #21 provisions the infrastructure; this proposal governs what configuration rides on it. An instance repo is a natural consumer of both. |
| [#15](https://github.com/jesseheady/technician/issues/15) Check filtering (closed) | Already shipped; the multi-target arrangement it enables is what a config repo organises. |

## Best practices to codify in the guide

1. One config repo per stack instance; directory-per-environment inside it. Do not branch-per-environment.
2. Pin by digest, not tag. Renovate manages the bump.
3. Run `technician validate` in instance-repo CI using the pinned image.
4. Secrets never enter the config repo. The `${VAR}` substitution seam already supports env, SOPS, or a secret manager at deploy time.
5. Deploy from a tag in CI, never from a working copy.
6. Dashboards are additive; do not edit shipped JSON.
7. Replace the vendored bundle wholesale; never hand-edit it.

## Open questions

- Should the stack bundle include `examples/`, or does that duplicate what the instance repo already has once seeded?
- Signing the stack bundle: now that #289 publishes reproducible per-tarball digests, a `sha256` on the stack bundle is the parity move and should ship with change 2, not be deferred. The open part is whether to add cosign signing on top, given the image is already published to GHCR.
- Does the Alertmanager `${VAR}` awk substitution survive an instance repo cleanly, or does the routing tree want a first-class include mechanism?
- For change 3, is `existingConfigMap` sufficient, or do operators want the chart to glob a local `checks/` directory directly (`.Files.Glob`), which keeps check files as real files but requires the chart to be local rather than pulled from OCI?
