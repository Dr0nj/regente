# ⚙️ Running in production — enterprise readiness

> How to run `regente-server` in production without losing availability or correctness:
> **zero-downtime upgrades**, **multiple environments** (Dev/Staging/Prod), **quotas** that
> survive a failover, and **GitOps drift reconciliation**. It connects to the resilience track
> (R1–R7) and to the [SLOs](slos.md).

## 1. Zero-downtime (rolling) upgrades

The control plane is **stateless** — every piece of durable state lives outside the process
(Postgres + the Git workspace) — and it uses **leader election through an advisory lock** (G1).
That makes a rolling upgrade a special case of the failover already validated in chaos/HA:

```
1. Nodes A (vN) and B (vN) behind a LB/VIP, on the SAME Postgres → A is leader, B follower.
2. Start node C on the NEW version (vN+1) → it joins as a follower (the advisory lock is taken).
3. Wait for C to become READY (/readyz = 200) — only then does the LB send it traffic.
4. Drain and stop an OLD node. If it was the leader, the lock is released and another node
   (C included) takes over in ~4s (measured in chaos/HA).
5. Repeat until the whole fleet is on vN+1.
```

**Why the API sees zero downtime:** a follower serves the API normally (`/readyz` gates on the
DB, not on leadership — R3). During the ~4s failover window **the API keeps answering** from the
followers; only daily materialization and dispatch pause briefly — and both are **idempotent**
(atomic claim + existence check), so resuming neither duplicates nor loses anything.

**Migrations:** migrations are **idempotent and versioned** (`schema_migrations`). A vN+1 node
applies whatever is missing at boot; vN and vN+1 coexist as long as the migration is additive
(rule: never drop or rename a column in the same release as the code that uses it — expand and
contract across two releases).

Automated demonstration:
[`../server/deploy/rolling-upgrade.sh`](../server/deploy/rolling-upgrade.sh) (starts two nodes on
the same PG, promotes the new one, drains the old one and measures the leadership gap).

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

Quotas (F15 — *quantitative resources*, Control-M style) cap how many jobs compete for a named
resource (e.g. `db=5` → at most 5 jobs using the pool at once). The tracker is **in memory and
lives on the leader**. When a node takes leadership (at boot or after a failover), the new leader
**rebuilds usage from the `RUNNING` instances** in durable state — each instance carries the
snapshot of its definition, resources included — via `RebuildResourcesFromRunning`. Without it, a
freshly promoted leader would start with an empty tracker and let capacity be exceeded. Covered
by a test (`TestQuotas_RebuildFromRunning`).

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
[ ] Scheduled backup (backup.sh / pg_dump) + a restore drill — R6
[ ] env_label set per environment; Prometheus grouping by regente_env_info
[ ] -drift-reconcile-sec enabled (alert when regulated, sync otherwise)
[ ] Rolling upgrade rehearsed (rolling-upgrade.sh) before the first real deployment
```
