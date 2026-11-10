# Performance and recovery operations

The performance changes address repeated definition loading, retry amplification in audit receipts, and full scheduler scans. They do not establish a production capacity rating. See the [roadmap](roadmap.md) for qualification status and the [capacity laboratory](capacity.md) for workload limits.

## Compatibility and rollout

This runtime requires schema **33**, protocol **2**, and matching server/agent artifacts. Stop admission, drain or inventory active and uncertain executions, and back up the database, workspace, signing key, collector journal and agent journals before migration. Schema 33 adds audit reservations and transactional scheduler change notifications. It inventories seven audit slots for each nonterminal attempt, including uncertain attempts. An inherited backlog can exceed the configured limit: new admission then fails closed while reserved transitions remain available. Do not delete reservations or ledger entries to reopen admission.

Upgrade the independent collector before enabling `-audit-batch-size 128`. The default remains one record for compatibility with existing collectors. Upgrade the server with `-scheduler-incremental=false` first; enable `-scheduler-incremental` only after the comparison workload passes. Both switches are startup flags. Disabling incremental scheduling restores full evaluation on schema 33; it does not downgrade the database. Rollback to a schema-32 binary requires the complete matching pre-upgrade backup and reconciliation of subsequent external effects, not just replacing the executable.

## Definitions

The live workspace owns one immutable normalized snapshot. API readers receive isolated copies; WebSocket handshakes hash the authorized view without rebuilding or copying the graph. Force Order looks up one definition through the snapshot's ID index before checking folder write permission; it does not copy the entire workspace for authorization. Full-list copies reuse type layouts and preserve nested value types and isolation. Concurrent cold readers share a build. A cancelled HTTP reader does not cancel that shared work. Known file/Git mutations invalidate before and after changing the workspace and serialize against publication. A Git sync with unchanged HEAD and tracked files retains the hot snapshot; untracked files remain preserved. External changes are checked by content every 15 seconds, including same-size and same-mtime edits. The build compares the content it actually parsed with the surrounding digests; an unstable build is rejected.

Invalid definitions make the snapshot unavailable. `/readyz` reports this, and a WebSocket connection receives 503 with `Retry-After` before upgrade. Correct the workspace and wait for reconciliation. External tools publishing multiple files must use coordinated or atomic publication; independent filesystem writes are not a multi-file transaction. Old snapshots are retained only while existing readers use them.

## Audit pressure and receipts

Admission reserves seven named slots for dispatch, acceptance, start, terminal outcome, cancellation request/delivery and recovery. Consumption and release occur in the same transaction as the transition and signed evidence. A failed commit restores the slot. Every mandatory payload is limited to 64 KiB; reserved payload bytes are a conservative budget, not allocated RAM or total database size.

Protocol-2 ACK, result and output routes use their existing durable execution identity, fence and checksums. Successful retries return the persisted receipt without new HTTP intent/result records. Authentication and authorization denials remain audited. This does not change the auditing policy for other mutation routes. Heartbeat lease/contact updates and output byte/sequence counters are operational telemetry; execution state, identity, reason and result checksum changes still require mandatory evidence. A no-op order lock is not a business mutation.

Batch export accepts at most 128 records and 1 MiB per request. The collector validates continuity and replay identity, writes and syncs the journal, then acknowledges a contiguous prefix. The exporter advances only that prefix. Lost responses can resend records safely. Partial writes or sync failures stop the collector until its journal is recovered; never discard the journal to hide a gap. Multiple exporters may resend the same prefix; each collector journal still requires a single owning process.

## Incremental scheduling

The scheduler keeps a deduplicated candidate queue, condition consumer indexes and due-time heap. Database triggers coalesce changes entering or leaving WAITING/RUNNING in `scheduler_changes`; terminal history inserts do not occupy the notification queue. Version-checked consumption avoids deleting a newer notification. Each tick reads at most 256 notifications and a 256-row reconciliation page. Candidate evaluation yields after 100 ms and requeues unfinished work. Claims, fencing, leadership and frozen instance snapshots retain their existing transaction rules.

A full keyset pass recovers lost notifications; the next pass starts 30 seconds after completion. Agent/resource blockers retry after two seconds, running candidates after one minute. At a two-second tick, scanning 28,800 rows takes roughly 113 ticks before evaluation delays; the reconciliation interval is not a 30-second recovery guarantee. The validated-plan cache holds at most 1,024 entries. Condition-pool reads and existing running/resource maintenance remain; incremental mode does not eliminate every database scan.

## Capture comparable evidence

Profiling is disabled by default. Use `-profiling-addr 127.0.0.1:6060` or `REGENTE_PROFILING_ADDR=127.0.0.1:6060`; only numeric loopback addresses are accepted. The listener is separate from the public router. It has no HTTP authentication: access it through a protected host or SSH tunnel. Profiles can contain private paths and workload data; store them with restricted permissions.

```sh
umask 077
curl --fail 'http://127.0.0.1:6060/debug/pprof/profile?seconds=30' -o cpu.pprof
curl --fail 'http://127.0.0.1:6060/debug/pprof/heap' -o heap.pprof
go tool pprof -top ./regente-server cpu.pprof
vmstat 1 30
```

CPU and trace durations are bounded to 1–60 seconds. Heap, allocations, goroutine, thread, block and mutex profiles are available; block/mutex sampling is not enabled automatically. Capture the same workload, hardware, binary SHA, flags, database density and reader rate before and after. Report process CPU separately from host steal time. A successful health request is not proof of execution progress.

`/metrics` includes `regente_perf_stage_seconds` histograms for definition digest/read/normalization/build, workspace view, audit persistence/export, and scheduler selection/evaluation. Fixed-label counters report cache hits/builds/failures, receipt replays, rejected admission, exported records, evaluated candidates and reconciliation pages. Additional gauges expose definition readiness, reserved records/payload bytes, incremental pending/tracked/plans and reconciliation age. Existing audit lag/backlog, execution latency and completed-tick metrics remain necessary. `/readyz` distinguishes attempted ticks from completed progress; scheduler staleness is informational for followers and externally triggered scheduling.

## Local qualification

```sh
# Contract/regression suite; PostgreSQL cases require REGENTE_TEST_PG_DSN.
go test ./server/... ./agent/...
REGENTE_TEST_INCREMENTAL=1 go test ./server/internal/scheduler -count=1
# Linux/amd64, real PostgreSQL, agents and independent TLS collector:
python3 scripts/capacity.py --perf-compare
python3 scripts/capacity.py --perf-day
```

`--perf-compare` runs the same 28,800-definition workload with full scheduling/single-record export and then incremental scheduling/batch export. Both use schema 33, the definition cache and audit reservations; this is not an old-binary comparison. It records process profiles, collector RSS and container CPU/memory limits. It does not qualify a daily capacity.

The last command requests a real 24-hour run with business-date rollover before higher tiers (28,800, 100,000, 250,000 and 1,000,000 orders/day offered rates), then a recovery soak at the last supported rate. CPU/heap profiles are saved with owner-only permissions; samples retain host CPU jiffies (including steal) separately from process CPU. A failed daily gate blocks higher tiers. Retained terminal rows are fixtures; actual admitted attempts, verified effects and distinct executed graph definitions are counted separately. The daily gate requires all 28,800 graph definitions to execute. The synthetic graph contains 900 components with fan-out and short chains using permanent conditions, mixed COMMAND/HTTP jobs, output and retries. It is not a qualification of the operator's production topology. `--perf` without `--perf-day` is a short engineering comparison and cannot close the daily gate.

Require bounded queues, correct receipts/effects, acceptable CPU/RSS and dispatch/reader latency, fault recovery and the complete versioned verification gates before deployment. Preserve failures and skipped gates in the report. Short quiescent SQLite benchmarks do not establish PostgreSQL throughput, VPS CPU reduction, a daily SLO, or one-million-per-day capacity.

The capacity runner accepts explicit `--fast-slots` and `--slow-slots` (defaults
4 and 1, respectively). Every result records both. Use identical concurrency for
both halves of a comparison; changing slots defines a new capacity envelope and
must not overwrite a failed report. Long-job capacity includes receipt/polling
latency in addition to the three-second external command. CPU profiles cover the
first 30 seconds, including any admission stop inside that window.
