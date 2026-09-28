# Capacity evidence and guarantee limits

Reviewed on 2026-09-28 against source baseline `f067e48` (v0.2.36).
This is an evidence inventory, not a new benchmark or production certification.
The [roadmap](roadmap.md) owns delivery status; the enterprise cycle still has
I05–I17 open. A documentation correction does not close those gates.

## Three different operations

- **Materialization:** create instance rows for a daily. Rows do not prove that
  agents received work, completed it or committed its external effects.
- **Query and UI:** read aggregates/pages and render a bounded visible subset of
  seeded records. This does not measure executor throughput.
- **Execution:** dispatch, agent acceptance, work, results and external effects
  under a declared workload and failure model. Daily count alone cannot qualify it.

## Historical observations and provenance

The numbers below are retained as historical reports, **not reproduced in this
review**. Recoverable narrative and fixture code do not replace the original raw
measurement, immutable run SHA and environment manifest. Those original artifacts
were not located in the reviewed tracked evidence. This is not a finding that the
numbers are false. Do not attribute these runs to the later VPS installation or
combine measurements at different cardinalities into one end-to-end result.

| Operation / report | Date and recoverable source | Profile known from the source | Missing evidence / allowed interpretation |
|---|---|---|---|
| Daily materialization: 10k in 182ms, 100k in 1.7s, 1M in 17.4s (rounded to 17s in summaries) | Roadmap P1, 2026-06-23; [TestScale_BenchmarkN](../server/internal/scheduler/enterprise_test.go) | In-process SQLite scheduler fixture, synthetic enabled COMMAND definitions, RunDaily only; no agent fleet or execution measurement | Original run SHA, raw log, hardware/OS/toolchain/storage manifest and repeat distribution unavailable. Historical row-creation timings only |
| Read path: summary 51ms, filtered page of 500 in 18ms at 100k | Roadmap P2, 2026-06-23; [TestReadPath_Scale](../server/internal/api/instances_scale_test.go), introduced in `ef2bce6` | SQLite HTTP test server; seeded rows across 100 folders and four statuses, aggregate and folder-page queries | Code-introduction commit is not a measured-run SHA. Original raw samples and host manifest unavailable. No claim of 51ms at 1M or production p99 |
| ViewPoint: folder first page ~39ms at 1M records; later sidebar: 37 rendered rows | Roadmap P3, 2026-06-24 (`e8e9e9e` implementation); UI-1 report, 2026-07-09; [seed utility](../server/tools/seed/main.go) | Seeded 1M rows / 200 folders; bounded pages and virtualized display. P3 and UI-1 are different observations | Browser/hardware, immutable capture, raw timing and exact measured-run SHA unavailable. Rendering 37 rows does not execute 1M jobs |
| Failover ~1s in the architecture report; ~4s in the older SLO narrative | Architecture milestone 2026-06-18 and historical SLO text | Reports of two nodes with PostgreSQL; timing boundaries differ or are unspecified | No common run manifest/raw trace recovered. Neither number is a guaranteed recovery time or evidence of partition safety |
| ~7.5k requests/s in the older SLO narrative | [TestLoad_ReadyzConcurrent](../server/internal/api/e2e_test.go) | 40 workers × 50 GET /readyz requests against an in-process test server | Original run/date/SHA/host metadata unavailable. Readiness-probe traffic is not job throughput or whole-API availability |

The July 2026 case studies also retain dated code/test counts and a reported
USD 5 VPS installation. Those are historical project facts, not current pricing,
a capacity profile or evidence that the above measurements ran on that machine.

## Recoverable integration evidence

The [integration baseline](integration-baseline.md) defines a small, explicit
profile: **3 orders**, at most **3 concurrent commands across 2 agents**, two
Linux/amd64 control planes, PostgreSQL 17.6 and NATS. It exercises wiring,
identity and selected restart/restore behavior; it does not qualify sustained
throughput, partitions or all uncertain-effect windows.

Versioned evidence includes [I04](evidence/i04-b6db081.json) and
[DOC-C](evidence/doc-c-94c120d.json), with tested revisions and CI references.
I04's web-event authorization is delivered; it is independent of durable job
dispatch. A green small-workload run cannot be extrapolated to 1M executions/day.

## Dispatch and HA boundaries

| Mechanism present in source | What it establishes | What it does not establish |
|---|---|---|
| [Atomic claim in startInstance](../server/internal/scheduler/scheduler.go) | Conditional UPDATE from WAITING to RUNNING; competing claimers cannot both win that same state transition | Durable agent acceptance, one execution across retries/reruns, or exactly-once external effects |
| [Machine assignment](../server/internal/scheduler/agent_assignment.go) and [identity contract](agent-identity.md) | Work/result routing bound to the assigned authenticated machine | Per-attempt fencing, durable dispatch/result ACK or safe resolution of an uncertain remote effect |
| [PostgreSQL advisory leadership](../server/internal/leader/leader.go) | One database session holds the leadership lock; internal scheduling follows local leadership state | A fixed failover deadline, immediate loss detection under partition, or fencing of already executing agents |
| [Per-tick lock](../server/internal/scheduler/ticklock.go) | Serializes protected overlapping ticks when configured | End-to-end recovery or durable execution by itself |
| Resource rebuild from RUNNING rows | Reconstructs the leader's quota accounting from persisted state | Proof of what a remote process actually completed during disconnection |

A crash between the database claim and delivery, or after a remote effect but
before its result is recorded, leaves different uncertainty windows. Neither an
atomic row update nor electing a new leader alone resolves them. There is no
universal zero-loss/zero-duplication guarantee. Recovery must reconcile durable
state with the external system; use operation-specific idempotency where supported.

Durable acceptance/results, attempt fencing, recovery and partition qualification
remain separate enterprise work (including I08–I12); capacity, soak and operational
acceptance remain subject to I16/I17. These IDs describe open gates, not capabilities
implemented by this review. See also [operations](operations.md),
[SLO objectives](slos.md) and [backup scope](dr-backup.md).

## Requirements for a new capacity or HA claim

Record the immutable server/agent SHA, date, OS/architecture, CPU/RAM/storage,
database/version/settings, topology/transports, command and fixture, warm-up and
repetitions, raw logs and observed errors. Separate created rows, query latency,
rendered rows, accepted attempts, completed commands and verified external effects.
For execution, include peak ready/s, concurrency, duration/output distributions,
retry/idempotency policy, failure injection and reconciliation results. Report
percentiles over a defined sample/window and the applicable acceptance threshold.
A proposed SLO or an unexecuted benchmark fixture is not a measured result.
