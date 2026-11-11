# Performance and recovery operations

The performance changes address repeated definition loading, retry amplification in audit receipts, and full scheduler scans. They do not establish a production capacity rating. See the [roadmap](roadmap.md) for qualification status and the [capacity laboratory](capacity.md) for workload limits.

## Compatibility and rollout

This runtime requires schema **33**, protocol **2**, and matching server/agent artifacts. Stop admission, drain or inventory active and uncertain executions, and back up the database, workspace, signing key, collector journal and agent journals before migration. Schema 33 adds audit reservations and transactional scheduler change notifications. It inventories seven audit slots for each nonterminal attempt, including uncertain attempts. An inherited backlog can exceed the configured limit: new admission then fails closed while reserved transitions remain available. Do not delete reservations or ledger entries to reopen admission.

Upgrade the independent collector before enabling `-audit-batch-size 128`. The default remains one record for compatibility with existing collectors. Upgrade the server with `-scheduler-incremental=false` first; enable `-scheduler-incremental` only after the comparison workload passes. Both switches are startup flags. Disabling incremental scheduling restores full evaluation on schema 33; it does not downgrade the database. Rollback to a schema-32 binary requires the complete matching pre-upgrade backup and reconciliation of subsequent external effects, not just replacing the executable.

### Native systemd rollout

Follow the [backup and restore procedure](dr-backup.md) and [upgrade contract](upgrades.md)
before changing the installed service. Preserve the actual unit, environment file,
binary versions and flags with the backup. Verify the restore in an isolated cell;
an online database snapshot alone does not capture every journal or external effect.

The supplied `regente-server.service` starts the binary without arguments. These
two performance switches are CLI flags, not environment variables. For that stock
unit, a reviewed drop-in at `/etc/systemd/system/regente-server.service.d/performance.conf`
can select the initial mode:

```ini
[Service]
ExecStart=
ExecStart=/usr/local/bin/regente-server -audit-batch-size=1 -scheduler-incremental=false
```

For a customized unit, preserve all existing arguments in the replacement
`ExecStart`. The environment file remains in effect. Inspect the effective unit
with `systemctl cat regente-server`, then apply the drop-in with
`systemctl daemon-reload` and `systemctl restart regente-server` during the planned
window. A restart changes process counters; start a new measurement interval.

| Stage | Mode | Evidence before proceeding | Reversal on schema 33 |
|---|---|---|---|
| Collector upgrade | Existing server settings | Durable prefix ACK, compatible single-record requests, journal preserved | Use a compatible collector with its complete journal |
| Server/agent upgrade | Batch 1, full scheduler | Definition readiness, scoped authentication, real durable canary, receipt replay, no ledger gap | Keep the matching runtime; investigate migration separately |
| Batch export | Batch 128, full scheduler | Pending audit records drain, ACK continuity, no new audit errors | Return batch size to 1 |
| Incremental scheduler | Batch 128, incremental true | Dispatch/condition parity, bounded queue, completed ticks, reconciliation progress and canary success | Return incremental to false |

Observe each stage under the same offered load before proceeding. Record the
actual settings and change time; a successful `/health` request is insufficient.
The existing `scripts/progress-probe.py` creates a real order, so use only a
dedicated, approved canary definition and a protected token file. Pause admission
and investigate if evidence has a ledger gap, conflicting receipt, duplicate
external effect, growing backlog without recovery or stalled execution. Do not
delete audit data or reissue uncertain effects to make a check pass. Switching
flags back does not undo migration or completed external effects.

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

Reconciliation reads retained rows in the current business date, including
terminal history. At 256 rows per tick and a two-second tick, nominal pass times
are 226 seconds for 28,800 rows, 782 seconds for 100,000, 1,954 seconds for 250,000,
and 7,814 seconds for 1,000,000. These are arithmetic estimates, excluding delays
and the 30-second pause between passes, not measured recovery guarantees. Durable
notifications normally provide the faster path; lost-event recovery and restart
must still be evaluated at each density. Ordering within a selected batch does
not by itself prove global FIFO across pages. Keep these gates distinct from
ordinary dispatch throughput when deciding whether a larger tier is qualified.

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

The synthetic graph uses `schedule.keepActive=2`, recorded in the report, so
admitted waiting jobs can cross the business-date rollover. The product default
of zero intentionally expires never-started waiting jobs at the first rollover;
this fixture setting does not change that behavior.

Capacity profiles use `integration/compose.capacity.yaml`, with PostgreSQL data
on a dedicated Docker volume owned by the disposable run. The runner removes
that volume during cleanup. This differs from the short integration suite's
RAM-backed `tmpfs`; do not combine their results as one storage configuration.
The report verifies the actual data mount and records PostgreSQL's Docker
CPU/memory limits separately. A zero Docker limit means no explicit container
limit; the runner's cgroup budget does not include this separate database.

The capacity runner accepts explicit `--fast-slots` and `--slow-slots` (defaults
4 and 1, respectively). Every result records both. Use identical concurrency for
both halves of a comparison; changing slots defines a new capacity envelope and
must not overwrite a failed report. Long-job capacity includes receipt/polling
latency in addition to the three-second external command. CPU profiles cover the
first 30 seconds, including any admission stop inside that window.

## Read evidence while the laboratory runs

```sh
python3 scripts/perf_report.py .integration/RUN_ID/evidence > perf-summary.json
```

The analyzer is read-only and uses one sample at a time. It accepts a live or
copied evidence directory, stops at the file size observed when opening it, and
reports an incomplete trailing line instead of consuming a partial write. Invalid
complete records, nonfinite values and out-of-order timestamps fail analysis.
It reports counter resets, missing observations, sample gaps, CPU per elapsed
interval, peak server/collector RSS, queue ages and counters. Reconciliation age
`-1` is counted as unavailable, not a fresh reconciliation. Host steal is separate
from process CPU, and guest time is not counted twice.

No final `capacity.json` means there is no final report; it does not prove the
process is still alive. The analyzer retains the runner's reported verdict and
limitations but never grants qualification. Samples include the injected outage,
so their maxima must not be substituted for the runner's fault-window-aware
acceptance decision. Preserve the original files, binary SHA and final report.

A `--perf-day` run needs the host awake with Docker running for at least 24 hours,
plus higher tiers, recovery and verification. Screen lock is fine; suspension,
shutdown or stopping Docker interrupts the experiment. Record an interrupted run
as incomplete and restart the full measurement window when rerunning it.


## Diagnose a failed daily gate before repeating it

Keep the failed `capacity.json`, sample journal, attempt records and profiles.
A short regression can exercise the same 28,800-definition graph, a real business
rollover and paused-agent recovery without claiming a full-day qualification:

```sh
python3 scripts/capacity.py --perf-check --tier-seconds 600 --fast-slots 128 --slow-slots 64
```

This uses a disposable PostgreSQL volume with durable commits enabled. Its seed
sets rollover five minutes ahead; normal daily runs keep their twelve-hour offset.
A passing short check does not substitute for `--perf-day` or authorize higher
tiers. Use the same host resource configuration and finish other verification
work before beginning the isolated daily measurement.

Sample journals now include Linux pressure and cgroup I/O/CPU data. Bounded
latency alerts capture PostgreSQL transaction wait types/blocking PIDs, process
I/O counters and server goroutine stacks. These files omit SQL text, command
arguments and credentials. Capture errors remain explicit. The report analyzer
correlates the peak host I/O-wait interval with outbox and scheduler ages; host
I/O wait includes unrelated workloads and does not identify the responsible disk
or prove an application bottleneck. Diagnostic capture does not relax budgets.

The agent drains at most 32 deliveries per tick and acknowledges a newly persisted
entry immediately, retaining concurrency and pending-journal limits. Acceptance
still follows journal persistence, and effects still require durable start
authorization. A partial SQLite index excludes acknowledged tombstones from the
pending scan while retaining them for duplicate detection across restarts.
