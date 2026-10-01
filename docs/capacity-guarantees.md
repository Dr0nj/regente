# Capacity evidence and guarantee limits

Historical evidence reviewed on 2026-09-28 against `f067e48` (v0.2.36);
durable dispatch and HA boundaries updated for I11 on 2026-10-01.
This is an evidence inventory, not a new benchmark or production certification.
The [roadmap](roadmap.md) owns delivery status; the enterprise cycle still has
I12–I17 pending after the I11 validation gate. A small-workload result does not
close capacity, disaster recovery or pilot acceptance.

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
| [Durable execution](durable-execution.md), I08–I10 | Attempt identity, durable dispatch/result receipts and agent journal; uncertain effects require explicit evidence | Exactly-once effects in arbitrary external systems, automatic resolution of unknown completion or fleet capacity |
| [Machine identity](agent-identity.md) and shared protocol-2 capacity | Routing bound to authenticated assigned machines; polls on either node inform admission and agent limits | Immediate disconnection detection, a guarantee that a remote process stopped or permission to reset its journal |
| [PostgreSQL leadership and resources](ha-resources.md), I11 | Session lock ownership and persisted terms gate scheduling transactions; shared reservations belong to executionId and survive failover | A fixed failover deadline, arbitrary partition qualification or fencing of already authorized physical effects |
| [Per-tick lock](../server/internal/scheduler/ticklock.go) | Serializes protected overlapping ticks when configured | End-to-end recovery or durable execution by itself |
| Durable reservation reconciliation | Repairs missing reservations from verified runtime snapshots; retains unknown holds and rejects conflicting ownership | Proof of what a remote process completed during disconnection or unrestricted legacy HA guarantees |

A crash between the database claim and delivery, or after a remote effect but
before its result is recorded, leaves different uncertainty windows. Neither an
atomic row update nor electing a new leader alone resolves them. There is no
universal zero-loss/zero-duplication guarantee. Recovery must reconcile durable
state with the external system; use operation-specific idempotency where supported.

I11 adds a separate two-server PostgreSQL/NATS scenario with an agent attached
to the follower, shared quota 1, a follower quota update, a controlled database/NATS
interruption and leader SIGKILL. A command remains active through succession;
its execution identity and reservation are checked before allowing completion.
Two isolated command effects must occur exactly once in this scenario. Inspect
the mandatory integration report's i11_ha section at the tested SHA. This is a
correctness profile, not a sustained-throughput benchmark or a claim about every
asymmetric network partition.

Audit, load/soak, disaster recovery and operational pilot acceptance remain separate
enterprise gates I12–I17. See [operations](operations.md), [SLO objectives](slos.md)
and [backup scope](dr-backup.md).

## Requirements for a new capacity or HA claim

Record the immutable server/agent SHA, date, OS/architecture, CPU/RAM/storage,
database/version/settings, topology/transports, command and fixture, warm-up and
repetitions, raw logs and observed errors. Separate created rows, query latency,
rendered rows, accepted attempts, completed commands and verified external effects.
For execution, include peak ready/s, concurrency, duration/output distributions,
retry/idempotency policy, failure injection and reconciliation results. Report
percentiles over a defined sample/window and the applicable acceptance threshold.
A proposed SLO or an unexecuted benchmark fixture is not a measured result.
