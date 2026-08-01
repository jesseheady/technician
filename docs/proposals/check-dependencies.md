# Check dependencies (`depends_on`)

Design proposal for [#234](https://github.com/jesseheady/technician/issues/234).

Issue #234 frames the choice as **Option 1** (alert-time correlation in PromQL,
no code change) vs **Option 2** (runtime dependencies via a DAG scheduler with
result handoff). This proposes a third position that lands almost all of the
Option 2 value at roughly Option 1's cost:

> **Dependencies gate, they do not sequence.**

A check declares `depends_on`. At its own tick, before running, it consults the
**last known result** of each named check. If any of them is not currently
passing, the run is skipped and reported as *blocked* — a neutral, first-class
state alongside the existing `InfraError`. No DAG, no topological sort, no
change to the `Checker` interface, no cross-goroutine orchestration.

## Why gating (not sequencing) is the right model

The motivating example — alert when TLS is healthy *but* the TLSA record
disagrees — does not actually need ordering or state handoff. It needs the TLSA
check to **not produce a misleading signal while TLS itself is broken**. A gate
on last-known state delivers exactly that.

Sequencing ("run B immediately after A completes, in the same pass") is what
forces the expensive parts of Option 2: merged schedules for parent and child,
timeout and error propagation across the boundary, cycle-aware execution
ordering, and a `Run` signature that threads a parent `Result`. Every one of
those is a new failure mode in the hot path of a monitoring tool whose entire
job is to be more reliable than the things it watches.

What gating gives up, honestly:

| Given up | Consequence | Why it's acceptable |
|---|---|---|
| Ordering guarantee | A dependent ticking on the same cron second reads the dependency's *previous* result | Check state changes on the order of minutes; the gate is a health precondition, not a transaction |
| Runtime state reuse | A TLSA check re-does its own TLS handshake rather than reusing the parent's leaf cert | One extra handshake per interval is negligible; typed state handoff would couple check types to each other permanently |
| Fan-out orchestration | No "run these 5 only after this 1 completes" batch semantics | Not requested; cron + gate covers the observed use cases |

If a concrete need for true sequencing or state handoff appears later, this
design does not block it: `depends_on` is the same config surface a sequencing
implementation would need, so it becomes a semantics upgrade, not a rewrite.

## Config surface

`depends_on` is a list of check names, matching the existing `tags` convention
(`[]string`, no scalar-or-list custom unmarshaller). All named checks must be
passing for the dependent to run — the list is an AND.

```yaml
# checks/tls.yml
- name: example-tls
  type: tls
  host: example.com:443
  schedule: "0 */6 * * *"

# checks/dns.yml
- name: example-tlsa
  type: dns
  domain: _443._tcp.example.com
  record_type: TLSA
  schedule: "*/5 * * * *"
  depends_on: [example-tls]     # don't cross-validate a cert that isn't valid
```

Multi-signal gate:

```yaml
- name: api-deep-flow
  type: playwright
  script: checkout.js
  schedule: "*/10 * * * *"
  depends_on: [api-tls, api-http-health]   # skip the expensive browser run
                                           # when the basics are already down
```

That second example is the general shape this pays off in: it stops expensive
or noisy checks from firing redundant alerts about a failure a cheaper check
already reported. It is alert-noise suppression expressed in config rather than
in Alertmanager inhibit rules.

### Semantics

1. **Gate is evaluated at the dependent's own tick**, immediately before `Run`.
2. **Pass** = the dependency's last result had `Success == true`.
3. **No result yet** = blocked. A dependent stays blocked until its dependency
   has reported at least once.
4. **Infra error on the dependency** = blocked (the target was never tested, so
   its health is unknown — consistent with how `InfraError` already suppresses
   target-level metrics).
5. **A blocked dependency's own dependents are blocked** transitively, since
   being blocked means it never produced a passing result.
6. **Per-origin.** Each worker's scheduler evaluates the gate against results it
   observed itself. A dependency failing in `us-east-1` does not gate the
   dependent in `us-west-2`. This falls out of the existing one-scheduler-per-
   origin model for free, and is the correct semantic for a multi-region prober.
7. **No staleness window in v1.** The last result is used regardless of age. A
   dependency on a 6-hour schedule legitimately gates a 5-minute check with a
   6-hour-old result. (Ceiling: if a dependency's loop dies — e.g. an invalid
   cron expression the scheduler already logs and skips — its last success
   pins open forever. Config validation catches the schedule case at load;
   `depends_on_max_age` is the upgrade path if a real gap shows up.)

## The blocked state

Blocked is threaded through the existing result pipeline as a `Skipped` flag on
`check.Result`, mirroring `InfraError` exactly. This is what makes it
first-class rather than a log line: every consumer already receives it.

```go
// internal/check/check.go
type Result struct {
    // ...
    InfraError bool   // check infrastructure failed (not the target)
    Skipped    bool   // gated by depends_on; the target was never tested
    SkipReason string // e.g. "depends_on: example-tls not passing"
}
```

| Consumer | Behavior on `Skipped` |
|---|---|
| `metrics.RecordResult` | Set `technician_check_blocked{type,name,...} = 1`; return before touching `check_healthy`, `check_duration_seconds`, `last_run_timestamp_seconds`. Clear the gauge to 0 on any normal run (same shape as the existing `checkInfraError` clear). |
| `status.Store.Push` | Do not push a ring entry (no fake history bar, no uptime distortion); set `ring.blocked` + reason so the status page renders "Blocked — waiting on example-tls" instead of a stale or failing tile. |
| `notify.Manager.HandleResult` | Return early. A blocked check must never fire `check_down`, and must not clear a real down-state either. |
| Budgets / `validate` | Blocked checks are neither pass nor fail; `validate` reports them as skipped and does not count them toward exit status. |

**Why not record `check_healthy = 0`:** it would page for a check that was never
run. **Why not record `= 1`:** it would assert health that was never observed.
Leaving the series untouched and raising a separate gauge is the only honest
option, and it matches the precedent already set for infra errors.

### Alerting integration

`technician_last_run_timestamp_seconds` is deliberately not advanced on a
blocked run — the same reasoning as infra errors. The existing
`TechnicianDataGap` rule aggregates with `max()` across all checks, so one
blocked check cannot trip it; only a total stall can, which is correct.

`CheckFailing` (`technician_check_healthy == 0`) is unaffected because the
gauge is left at its last real value. To avoid alerting on a *stale* healthy
value for a long-blocked check, add one rule:

```yaml
- alert: CheckBlockedTooLong
  expr: min_over_time(technician_check_blocked[1h]) == 1
  for: 0m
  labels:
    severity: warning
    alert_class: dependency
  annotations:
    summary: "{{ $labels.name }} blocked by its dependency for over an hour"
```

This is the one genuinely new alerting concern the feature introduces, and it
is one rule plus a promtool test.

## Validation

Validation runs at config load, after `check_filter` is applied, so it sees the
exact set this worker will run.

1. **Unknown reference** → hard error: `check "x": depends_on references unknown check "y"`.
   This is the common typo class and the one that would otherwise silently
   block a check forever.
2. **Self reference** → hard error.
3. **Cycle** → hard error, with the cycle path in the message. A cycle is not a
   deadlock under gating semantics (everything in it simply blocks forever),
   which is exactly why it must be rejected loudly rather than left to be
   discovered as a mystery outage. Iterative DFS over the name graph, ~25 lines.

Rule 1 has a deployment interaction worth calling out in docs: with
`check_filter`, a dependent and its dependency must land in the same filter
scope. Splitting them across workers is a config error, and failing at load is
the correct treatment — the alternative (run ungated) silently discards the
guarantee the operator asked for.

## Implementation

Scheduler state is a single mutex-guarded map, written in `execute` where
results are already annotated, read in `runLoop` before dispatch.

```go
// internal/scheduler/scheduler.go
type Scheduler struct {
    // ...
    lastMu  sync.RWMutex
    lastOK  map[string]bool // check name -> last run passed
}

// passesDeps reports whether every dependency has a passing last result.
// ponytail: last-known-state gate, not ordering. A dependent ticking in the
// same second as its dependency reads the previous result; that is intended.
func (s *Scheduler) passesDeps(cfg *config.CheckConfig) (string, bool) {
    s.lastMu.RLock()
    defer s.lastMu.RUnlock()
    for _, dep := range cfg.DependsOn {
        if !s.lastOK[dep] {
            return dep, false
        }
    }
    return "", true
}
```

`execute` gains a leading gate check that publishes a `Skipped` result and
returns; `lastOK[cfg.Name]` is updated from `result.Success && !result.InfraError`
at the end. Blocked runs do not update `lastOK` (they leave the previous value,
which for a never-run check is the zero value `false`).

Estimated diff: **~120 lines of implementation** across config, scheduler,
check, metrics, status, and notify, plus tests.

### Phases

**Phase 1 — the feature.**
`DependsOn []string` on `CheckConfig` + `checkYAML`; validation (unknown, self,
cycle); scheduler gate + `lastOK` map; `Result.Skipped`/`SkipReason`;
`technician_check_blocked` gauge; status-page blocked state; notify early
return. Tests: config validation table (unknown ref, self, 2- and 3-node
cycles), scheduler gate (dep passing → runs; dep failing → skipped result
published, no metric mutation; dep never ran → blocked), status store (blocked
does not push an entry), notify (blocked fires nothing).

**Phase 2 — boot warm-start (optional).**
On startup every check is unrun, so every gated check blocks until its
dependency's first result. For a dependency on a 6-hour schedule that is a
6-hour blackout after each restart. Fix: seed `lastOK` from the persisted status
store snapshot (`$TECHNICIAN_DATA_DIR/status.json`) at scheduler construction —
the last-known state is already on disk and already survives restarts. ~10
lines. Worth doing in phase 1 if the store is reachable from the scheduler
without an import cycle; otherwise ship phase 1 and document the blackout.

**Deliberately not built:** DAG scheduler, result payload handoff, `Run`
signature change, `when: failure` inversion, `depends_on_max_age`,
cross-origin dependencies. Each is an additive upgrade to this same config
surface if a concrete need appears.

## Companion edits

Per `AGENTS.md`, the feature branch carries:

- `docs/getting-started.md` — `depends_on` in the check configuration section.
- `docs/metrics.md` — `technician_check_blocked`.
- `docs/alerting.md` — blocked state vs check_down; `CheckBlockedTooLong`.
- `docs/multi-target-deployment.md` — the `check_filter` scope constraint.
- `prometheus/rules.yml` + promtool test — the new alert.
- `examples/checks/` — one worked gated pair (the TLS→TLSA example).
- `AGENTS.md` — `depends_on` in the check model section.
- `docs/roadmap.md` — move to "Recently completed" on merge; close #234.

## Recommendation

Ship this instead of either option in the issue. Option 1 (PromQL correlation)
leaves the relationship invisible in config, cannot skip work, and gets verbose
past two signals. Option 2 as scoped is a scheduler rewrite bought for a
guarantee — ordering — that the motivating use case does not require. Gating is
the smallest thing that makes check relationships real, in config, in metrics,
and on the status page.
