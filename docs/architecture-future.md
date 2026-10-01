# Future architecture — serverless, portable, no lock-in

> Historical design: 2026-07-10 · Evidence/status review: 2026-09-28
> Scope: control plane (`server/`) + agent transport + executors.
> Root decision: **"portable serverless" (a scale-to-zero container with state and trigger
> externalized), not FaaS.**

This document is the guiding ADR for Regente's evolution across three phases. It records the
**why**, what has **already been applied in this repository**, and what is **design/R&D** for the
next iterations. Applied mechanisms are not equivalent to production qualification.
Current delivery status lives in the [roadmap](roadmap.md);
[capacity and HA limits](capacity-guarantees.md) delimit the historical claims.

---

## 1. Context and problem

We want Regente to be:

1. **Serverless** — no server to babysit, scales to zero, you pay for what you use.
2. **Free of lock-in** — not tied to AWS (or to any single cloud).
3. **Aligned with modern technology** and attractive to the community (adoption).

The real tension: **a scheduler is stateful and time-driven by nature.** Today Regente is a
classic daemon with two pieces that fight serverless:

- A **goroutine ticker** (every 2s) materializes the daily and dispatches — it needs an
  always-on process.
- A **persistent WebSocket** to agents and to the web — long-lived connections.

The design externalizes database state to PostgreSQL and published definitions to Git.
Schema 30 stores new/migrated Design drafts in the shared DB, with revision CAS and
reconstructible local caches. Unmigrated legacy clones still require preservation.
See [shared drafts](shared-drafts.md) and [recovery scope](dr-backup.md).

### The reframe

`serverless` ≠ FaaS. There is a spectrum:

| Approach | Lock-in | Fit for a scheduler |
|---|---|---|
| FaaS (Lambda, Cloud Functions) | high | poor (WS + long jobs + tick) |
| **Container-serverless (Knative, Cloud Run, Fly, App Runner)** | **low** | **great** |
| Self-hosted serverless (Knative on any k8s) | none | great |

**Decision:** go **container-serverless**. It keeps the single binary and the zero-infrastructure
story (the project's biggest adoption asset), delivers serverless economics and operations, and
does **not** couple us to AWS.

### What NOT to do

- **FaaS per job** — more lock-in, kills the single binary, fights the state.
- **DynamoDB as the primary store** — AWS's deepest lock-in. Serverless Postgres (Neon/Supabase)
  gives you scale-to-zero without tying you to a cloud.

---

## 2. Anti-lock-in principles

Depend on **open protocols and standards**, not on managed services:

| Need | Open standard (our choice) | Managed equivalent (swapped at deploy time) |
|---|---|---|
| Packaging | OCI image | any registry |
| Runtime | Knative (CNCF) | Cloud Run · App Runner · Container Apps · Fly |
| State | Postgres wire protocol | Neon · Supabase · Cloud SQL · RDS |
| Time trigger | cron → HTTP | Cloud Scheduler · EventBridge · Actions cron |
| Agent bus | NATS (CNCF) / SSE / long-poll | managed NATS |
| Identity | OIDC | any IdP |
| Logs/artifacts | S3-compatible API | R2 · MinIO · S3 · GCS |

The **ports & adapters** discipline the project already has (pluggable DB dialect,
`ExecutorPort` in the frontend) is extended to `Bus` (transport) and to the **trigger** (HTTP).
That turns lock-in into a *deployment* choice rather than a *code* choice.

---

## 3. Target architecture

```
            external cron (Cloud Scheduler / k8s CronJob / Actions)
                      │  POST /api/scheduler/tick   (Phase 1)
                      ▼
   ┌────────────────────────────────┐        Postgres (wire protocol)
   │  regente-server (OCI image)     │◀──────  Neon/Supabase/Cloud SQL/RDS
   │  -role=all|api|scheduler        │
   │  -scheduler=internal|external   │──────▶  Git (source of truth, YAML)
   │  scale-to-zero (Knative/Run/Fly)│
   └────────────────────────────────┘
                      ▲   Bus (Phase 2): WebSocket today; NATS/SSE tomorrow
                      │   dispatch ▼   ▲ result
              ┌────────────────────────────────┐
              │  agents / executors (Phase 3)   │
              │  COMMAND·SCRIPT·HTTP·SSH today   │
              │  + WASM · AWS · GCP · k8s Jobs   │  (a new one = a new capability)
              └────────────────────────────────┘
```

---

## 4. Phase 1 — externalize the time trigger  ✅ applied

**Goal:** remove the dependency on an always-on ticker so the service can scale to zero. This is
the **highest-leverage** change (roughly 80% of "serverless").

**Implemented in this repo:**

- `Scheduler.Tick()` (`server/internal/scheduler/scheduler.go`) — one scheduling cycle
  (daily-if-due + dispatch), **idempotent** (atomic claim in `startInstance` + an existence check
  in the daily) and **leader-guarded**. `Run()` now just calls `Tick()` in a loop.
- Flags in `main.go`:
  - `-scheduler=internal|external` (`REGENTE_SCHEDULER`). `internal` = goroutine ticker (the
    default, classic daemon). `external` = no ticker; definitions load at boot and the cycle is
    driven over HTTP.
  - `-role=all|api|scheduler` (`REGENTE_ROLE`) — modular monolith: the same binary starts as
    everything (default) or splits API from scheduling.
- Endpoint `POST /api/scheduler/tick` (writer-gated) → calls `Tick()`.
- Deployment artifacts in [`../deploy/`](../deploy): `Dockerfile` (distroless),
  `knative-service.yaml` (`minScale: 0`), `cronjob.yaml` (the external trigger), `README.md`.

**Compatibility:** the defaults (`all` + `internal`) behave exactly as before. Nothing breaks for
anyone running it as a daemon.

**Result:** with `-scheduler=external` + an external cron + managed Postgres, the control plane
runs **serverless, scale-to-zero, without lock-in**.

**Validated in a container (2026-06-18):** the image from [`../deploy`](../deploy) + Postgres in a
container → clean boot, `/health`→200, leader election, and the external trigger
`POST /api/scheduler/tick` materialized the daily (`5 instances`, persisted in PG) idempotently.
The first run **uncovered** a hole: the storage layer shelled out to `git`, so the
`distroless/static` image crashed at boot (`EnsureClone`: no `git`) — worked around at the time
with Alpine+git. The **permanent fix** migrated `internal/storage/git.go` to **go-git** (pure Go,
no shell-out): the image went back to `gcr.io/distroless/static-debian12:nonroot` (static,
non-root, only ca-certificates) and boot/daily now clone and fetch+reset **without `git` on the
PATH**. One sharp edge handled: go-git's `reset --hard` deletes untracked files (git does not);
we reproduced git's behaviour by limiting the reset to the union of tracked paths, which
preserves the SQLite DB inside the workspace.

**Dedicated daily trigger (ARCH-5, ✅ applied 2026-07-10):** `autoDailyIfDue` was exposed as
`Scheduler.RunDailyIfDue()` (leader-gated, idempotent) behind `POST /api/scheduler/daily` — in
serverless you point a minute-level cron at `/scheduler/tick` (dispatch) and a **daily** cron at
`/scheduler/daily` (materialization), splitting the cadences. Tick still calls `autoDailyIfDue`
(backward compatible with the internal daemon).

### Concurrency and HA — classic versus serverless (important)

Regente has **two HA models**, and they are not the same mechanism. Confusing them leads to
over-investing in the wrong lock.

**Classic track (daemon, `-scheduler=internal`):** N always-on nodes, one holds
`pg_try_advisory_lock` and runs the ticker; the others are *hot standbys*. This is
`G1 leader election` (`internal/leader`, `PgAdvisory`). It makes sense for on-prem/enterprise
deployments of 2–3 fixed nodes. This is a session lock, not a fixed-TTL lease:
transfer depends on PostgreSQL releasing the session and a successor polling.
It does not fence an agent or guarantee recovery of external effects.

**Serverless track (`-scheduler=external`, scale-to-zero):** the leadership advisory lock **does
not map** — there is no long-lived process to *hold* the lock, and most of the time there are
**zero** instances. The external cron fires `POST /api/scheduler/tick`, a container starts, runs
`Tick()`, and dies. Concurrent scheduling uses the following database guards;
these do not establish exactly-once execution or durable delivery:

1. `Tick()` is idempotent — concurrent or overlapping ticks converge.
2. The **atomic claim** in `startInstance` (`UPDATE … SET status='RUNNING' WHERE id=? AND
   status='WAITING'`, bail out on 0 rows) — a row-level correctness guard, independent of how
   many processes are running. It protects that state transition, not remote acceptance.
3. The existence check when materializing the daily — idempotent across ticks.

In other words: **in serverless, leader election stops being correctness and becomes an
optimization** (avoiding N nodes doing redundant scheduling). If you do need to shield against
**overlapping** ticks (a cron that fires twice, or long ticks that cross), the right mechanism is
a **per-tick lock** — `pg_try_advisory_lock` acquired at the start of `Tick()` and released at the
end, short scope — and **not** a long-lived leadership lock.

**Scope:** PostgreSQL supports both tracks; leadership and per-tick/row guards
coordinate database work. Neither track gains durable execution or partition safety
automatically. See [capacity and HA evidence](capacity-guarantees.md) for the
claim/ACK/fencing/recovery distinction and the remaining enterprise gates.

**Per-tick lock (ARCH-3, ✅ applied 2026-07-10):** the mechanism designed above — serializing
**overlapping** ticks with a short-scoped advisory lock — was implemented in
`scheduler/ticklock.go`. Two layers: in-process (`atomic.Bool`, always on) plus a cross-process
`pg_try_advisory_lock` on Postgres (a key distinct from leadership, opt-in via `EnableTickLock()`,
switched on by `-scheduler=external`). It serializes the protected tick scope;
combined with atomic claims it still does not supply durable agent acknowledgment.

---

## 5. Phase 2 — pluggable agent transport (BusPort)  ✅ seam + long-poll + SSE + NATS applied

**Goal:** the WebSocket hub is the last piece that requires persistence. Abstract the transport
and offer a stateless path for serverless deployments.

**Implemented in this repo:**

- The `scheduler.Bus` interface (`server/internal/scheduler/scheduler.go`):
  ```go
  type Bus interface {
      BroadcastWeb(event string, payload interface{})
      PickAgent(capability string) *hub.Client
      GetAgent(id string) *hub.Client
  }
  ```
  The scheduler depends on `Bus`, not on `*hub.Hub`. The transport became a replaceable detail.
- **HTTP long-poll transport** (`server/internal/api/agent_http.go` + the agent):
  - `GET /api/agent/poll?id=&caps=` — blocks for ~25s waiting for a dispatch (204 on timeout);
    `POST /api/agent/result` finishes it; `POST /api/agent/output` streams stdout/stderr.
  - The `agentBroker` registers the agent as a `hub.Client` with a `Send` channel, so it
    **reuses all the existing routing** (`startInstance` → `PickAgent` → `Send`) — zero change in
    the scheduler. A reaper removes agents that stop polling (there is no "disconnect" like on
    WS).
  - Agent: the `-transport=ws|http` flag (`REGENTE_AGENT_TRANSPORT`). `http` keeps the
    NAT-friendly outbound model with no persistent connection in the server process → the
    control plane can scale to zero.
  - **Validated end-to-end:** an agent on `-transport=http` receives dispatches (daily and
    force), executes them and returns the result and stream.

**Also applied (after this ADR was first written):**

- **SSE transport** (ARCH-4, ✅ 2026-07-10) — `api/agent_sse.go` + `-transport=sse` on the agent:
  a `text/event-stream` with IMMEDIATE dispatch push (no ~25s long-poll window), same outbound
  model and the same `agentBroker`/`hub.Client`; results come back through the same POSTs.
- **NATS adapter + distributed hub** (R5, ✅ 2026-06-22) — `-bus=nats`: fan-out of web events,
  agent presence, and dispatch routed to the node that owns the agent, across multiple nodes.
  VALIDATED on two real nodes.

---

## 6. Phase 3 — executors as plugins + modern technology  ✅ WASM + cloud + OTel applied

**Goal:** stay aligned with modern technology and differentiate, while keeping the core
vendor-agnostic.

**The seam (the central point):** routing by **capability**. The agent announces its capabilities
(`-caps`) and dispatches through a `switch jobType` (`agent/main.go`); the server matches
`jobType`→agent via `PickAgent(capability)`. **Adding a new executor = adding a `case` and
announcing the capability.** The core does not change.

**Implemented in this repo — the WASM executor:**

- `jobType: WASM` + the `WASM` capability (in the default `-caps`).
- `runWASM`/`execWASM` (`agent/main.go`) use **wazero** (a pure-Go WASM runtime, no CGO — the same
  ethos as modernc's SQLite). It runs WASI modules (`_start`) compilable from Rust/Go/TinyGo/C;
  sandboxed by construction (it only sees what you provide). Params: `wasmPath` or `wasmUrl`,
  `args`, `stdin`. It captures stdout/stderr and maps the WASI exit to the job's exitCode.
- **Tested:** unit (`agent/wasm_test.go` with an embedded WASI fixture) + end-to-end (dispatch →
  runWASM → result through the agent).

**Also applied (after the first draft):**

- **Cloud executors as ordinary adapters** (✅) — `AWS` Lambda (SigV4 on the stdlib) plus
  Batch/Glue/Step (ADV-8), `GCP` Cloud Run Jobs, and `k8s Jobs` (validated on a real kind
  cluster). Each one is **a capability**, never the foundation — AWS is a plugin, not a base.
- **OpenTelemetry** (✅) — opt-in OTLP/HTTP tracing (`-otel-endpoint`), otelhttp plus spans in the
  scheduler.

**🚫 Decided NOT to do (noted, not pending):**

- **Durable execution (Temporal / Restate)** — replacing retry/tick/state-machine with a durable
  engine **contradicts this ADR's root decision** (single binary, zero infrastructure,
  anti-lock-in): it would mean operating another service. This historical choice
  did not implement equivalent durable execution. An atomic claim cannot establish
  safe crash resumption; native ACK/fencing/recovery remains enterprise work.
- **Postgres-as-a-queue** (`SKIP LOCKED`, e.g. River) — adopting that library was
  historically declined. The 1M report measured materialization, not dispatch.
  This technology choice does not close durable dispatch or recovery requirements.

---

## 7. Status and decisions

| Item | Phase | Status |
|---|---|---|
| Idempotent `Tick()` + refactored `Run()` | 1 | ✅ applied |
| `-scheduler`, `-role` flags | 1 | ✅ applied |
| `POST /api/scheduler/tick` | 1 | ✅ applied |
| Dockerfile + Knative + CronJob + deploy/README | 1 | ✅ applied · container e2e validated (2026-06-18) |
| Distroless image without `git` on the PATH | 1 | ✅ storage migrated to go-git (pure Go); image back to `distroless/static:nonroot` |
| Classic leadership — advisory lock (`G1`) | 1 | ✅ applied; historical two-node report, 2026-06-18; no guaranteed failover bound |
| Serverless scheduling guards — idempotency + atomic claim | 1 | ✅ applied; no durable execution guarantee |
| Per-tick lock (overlapping ticks, ARCH-3) | 1 | ✅ applied (2026-07-10; hygiene, not correctness) |
| Dedicated daily trigger (`/api/scheduler/daily`, ARCH-5) | 1 | ✅ applied (2026-07-10) |
| `Bus` interface (transport seam) | 2 | ✅ applied |
| HTTP long-poll transport (`-transport=http`) | 2 | ✅ applied (e2e) |
| SSE transport (`-transport=sse`, ARCH-4) | 2 | ✅ applied (2026-07-10; live e2e) |
| NATS adapter | 2 | ✅ applied (R5; two real nodes) |
| Distributed WebSocket hub | 2 | ✅ applied (R5) |
| WASM executor (wazero) | 3 | ✅ applied (unit + e2e) |
| Cloud adapters (AWS/GCP/k8s) by capability | 3 | ✅ applied (real k8s; AWS/GCP mocked + ADV-8) |
| OpenTelemetry (OTLP tracing) | 3 | ✅ applied (opt-in) |
| Durable execution (Temporal/Restate) | 3 | 🚫 decided against (contradicts the root decision) |
| Postgres-as-a-queue (River/SKIP LOCKED) | 3 | 🚫 library adoption declined historically; durable dispatch remains separate |

**Rollout principle:** serverless is opt-in; defaults preserve the classic daemon.
Version/schema/protocol compatibility must be checked against the
[upgrade matrix](upgrades.md); it is not guaranteed by this historical design.

---

## 8. References

- Knative (portable scale-to-zero): https://knative.dev
- NATS (CNCF bus): https://nats.io
- Temporal (durable execution): https://temporal.io · Restate: https://restate.dev
- extism / wasmtime (WASM): https://extism.org · https://wasmtime.dev
- River (Postgres queue): https://riverqueue.com
- Ports & adapters (hexagonal) pattern: Alistair Cockburn
