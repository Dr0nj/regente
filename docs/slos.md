# 🎯 Control plane SLOs — regente-server

> Service-level objectives for Regente's "brain", each tied to a **metric** (`/metrics`), a
> **probe** (`/livez` · `/readyz`) and an **automatic alert** (R7, `-selfmon`). This is how the
> orchestrator watches itself.

| # | SLO | Target | Signal (metric/probe) | Alert (R7) |
|---|-----|--------|-----------------------|------------|
| 1 | **API availability** | ≥ 99.9% of `GET /readyz` = 200 | `/readyz` (gate = DB reachable) | `db-unreachable` |
| 2 | **Failover time** (leader dies → new leader) | ≤ 10 s | `regente_is_leader` (changes) | `leader-flapping` (if it changes too often) |
| 3 | **Scheduling freshness** (loop alive) | tick < 90 s | `regente_scheduler_last_tick_age_seconds` | `tick-stalled` |
| 4 | **Execution capacity** (agent fleet) | no unplanned drops | `regente_agents_online` | `agents-drop` |
| 5 | **Process liveness** | automatic restart if it hangs | `/livez` + `livenessProbe` | (R1 supervisor) |
| 6 | **RPO/RTO (DR)** | RPO ≤ backup interval · RTO ≤ minutes | R6 backup (`-backup`/`pg_dump`) | runbook [`dr-backup.md`](dr-backup.md) |

## How each SLO is observed and defended

- **1. Availability** — `/readyz` only answers 200 when the DB is reachable (hard gate). The
  k8s `readinessProbe` takes the pod out of rotation on a violation; R7 raises
  `db-unreachable`.
- **2. Failover** — only the leader runs the daily and the dispatch; when it dies the advisory
  lock is released and another node takes over. **Measured at ~4 s** in the real two-node
  validation. `regente_is_leader` lets you alert on `changes()` in Prometheus (flapping) on top
  of R7's native alert.
- **3. Freshness** — every node stamps `lastTick` each cycle; the age is exposed both in the
  gauge and in `/readyz`. Past 90 s, R7 raises `tick-stalled` (jobs may not be getting
  promoted).
- **4. Capacity** — a drop in `regente_agents_online` against the baseline raises `agents-drop`.
- **5. Liveness** — `/livez` answers for as long as the process serves; the supervisor (systemd
  `Restart=always` / Windows Service / `livenessProbe`) restarts a hung process.
- **6. DR** — online backup (`-backup` = `VACUUM INTO` on SQLite; `pg_dump`/PITR on Postgres);
  restore validated by the runbook drill (`/readyz` 200 + `/api/env` after the restore).

## Continuous verification

- **Unit/integration** — `go test ./...` (covers `/readyz`, the mTLS handshake, RBAC, selfmon
  evaluation, a backup round-trip, **HTTP E2E** and a **load smoke test** at ~7.5k req/s
  locally).
- **Chaos/HA** — `server/deploy/chaos-ha.sh` automates it: two nodes on the same Postgres →
  exactly one leader → kill the leader → check the failover.
- **Real load** — for production volume (10k+ jobs/day, p99), running `hey`/`k6` against a real
  deployment is still an open quality item.
