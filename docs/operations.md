# ⚙️ Running in production — enterprise readiness

> Operating mechanisms and their limits: coordinated upgrades, multiple environments,
> quota reconstruction and GitOps drift reconciliation. These mechanisms do not by
> themselves qualify workload availability or correctness. See the [SLOs](slos.md)
> and the [upgrade compatibility matrix](upgrades.md).

## 1. Upgrade compatibility and same-binary drain

PostgreSQL and advisory-lock leadership support multiple control-plane processes.
Draft content still depends on session directories; the database is not the entire
recovery set. Follow [DR](dr-backup.md) and [coordinated upgrades](upgrades.md).
There is no qualified mixed-release rolling pair. The following is only a
same-binary laboratory sequence, not a production rollout recipe:

```
1. Use a disposable Postgres database and the SAME tested binary on both nodes.
2. Start a second node as follower while the first holds leadership.
3. Wait for the second node to become READY (/readyz = 200).
4. Stop the first process; observe leadership transfer and sample liveness on the survivor.
5. Stop the owned test processes. Record observations, not a universal availability guarantee.
```

`/readyz` can admit a follower; a passing liveness sample does not demonstrate all
API routes, workload continuity or freedom from duplicate/lost external effects.
Additive SQL does not override the binary's schema range or identity/protocol contract.
For schema transitions, stop old nodes and follow the migration runbook; do not
start a new migrator alongside an unqualified old runtime.

[`rolling-upgrade.sh`](../server/deploy/rolling-upgrade.sh) rejects different binary
bytes and requires an explicit disposable-DB acknowledgment. Its successful result
is a same-binary drain observation, not certification of a version upgrade.

## 2. Multiple environments (Dev / Staging / Prod)

Each environment is an **independent deployment** — no magic flags inside a single process.
Isolation comes from the **workspace branch + state store + label**:

| Axis | Dev | Staging | Prod |
|------|-----|---------|------|
| Workspace (defs) | branch `dev` | branch `staging` | branch `main` |
| State store | dev PG/SQLite | staging PG | prod PG (HA) |
| `env_label` | `DEV` | `STAGING` | `PROD` |

```sh
# Prod example
regente-server -db-driver postgres -db "$PROD_DSN" \
  -git-branch main -github-repo Dr0nj/regente-workspace
# env_label is set through the UI (Settings → Environment) or seeded; it shows up in
# /api/env and /metrics
```

**Promotion** is a Git flow: a PR `dev → staging → main` in `regente-workspace` promotes the
definitions from one environment to the next (reviewable, auditable, revertible with
`git revert`).

**Per-environment observability:** `/metrics` exposes `regente_env_info{env="PROD"} 1`;
dashboards and alerts group or filter on that label, so a single Prometheus covers all three
environments without mixing series.

## 3. Quotas (resources) across a failover

Quotas (F15 — *quantitative resources*, classic enterprise style) cap how many jobs compete for a named
resource (e.g. `db=5` → at most 5 jobs using the pool at once). Two halves, with different
lifetimes:

- **Capacities are durable** — the named resources and their capacity live in the `resources`
  table, so a restart no longer zeroes the registry; the tracker reloads them at boot
  (`ResourceTracker.LoadFromDB`).
- **Usage is in memory, on the leader** — when a node takes leadership (at boot or after a
  failover), the new leader **rebuilds usage from the `RUNNING` instances** in durable state —
  each instance carries the snapshot of its definition, resources included — via
  `RebuildResourcesFromRunning`. Without it, a freshly promoted leader would start with an empty
  tracker and let capacity be exceeded. Covered by a test (`TestQuotas_RebuildFromRunning`).

## 4. Drift reconciliation (GitOps)

The repository is the **desired** state; the runtime holds a local copy. When the remote moves
ahead (a push, a merged PR), the runtime is in **drift**.

- **Auto-sync** (already there): `-git-poll-interval` runs a periodic `fetch+reset+reload` — the
  runtime converges to Git on its own. Drift also raises a `git.drift` event in the UI.
- **Operational reconciler** (`-drift-reconcile-sec`, opt-in): only the **leader** runs it; when
  it detects drift it **alerts through the same channels as R7** (Slack/webhook/email/PagerDuty)
  — not just a badge in the UI. Two modes:
  - `-drift-reconcile-mode=alert` (default): it does **not** touch the workspace, it only
    **notifies** whoever is on call. Ideal for regulated environments or `pr-required` setups,
    where an automatic reset is not allowed.
  - `-drift-reconcile-mode=sync`: it **reconciles by itself** (fetch+reset+reload). Useful when
    git-poll is off and you want periodic convergence plus an alert if convergence fails.

```sh
# Regulated prod: notify, do not reset on its own
regente-server ... -git-poll-interval 0 -drift-reconcile-sec 60 -drift-reconcile-mode alert
```

## 5. Production checklist

```
[ ] State store OUTSIDE the ephemeral container (managed PG, or a volume for SQLite) — R4/R6
[ ] Supervisor with automatic restart (systemd Restart=always / Windows Service / k8s) — R1
[ ] /livez on the livenessProbe · /readyz on the readinessProbe — R2/R3
[ ] -selfmon ON with alert channels configured (Slack/PagerDuty) — R7
[ ] DB backup plus drafts/local definitions/external configuration + complete restore drill — R6
[ ] env_label set per environment; Prometheus grouping by regente_env_info
[ ] -drift-reconcile-sec enabled (alert when regulated, sync otherwise)
[ ] Exact source/target schema and identity compatibility reviewed; coordinated upgrade rehearsed
```
