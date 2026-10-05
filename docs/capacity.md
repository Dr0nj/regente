# Capacity and progress

Capacity is qualified by a measured engineering profile, never by row count alone.
The mandatory CI/release gate runs real PostgreSQL, server/agent protocol 2,
durable agent journals, external COMMAND/HTTP effects and an independent TLS
audit collector. It advances through 10,000, 100,000 and 1,000,000 retained rows
only while the previous tier meets its declared budgets. A qualifying tier runs at least
five minutes. A budget breach stops new admissions and drains the admitted work,
preserving receipts/effects and the shortened failed window. The last supported tier must pass a further 30-minute developer
soak. An independent operational pilot remains pending (I17).

## Measurements

`regente_scheduler_last_tick_age_seconds` measures a **tick attempt**, including
overlap and follower calls. `regente_scheduler_completed_tick_age_seconds`
advances only after a leader completes the synchronous instance scan without
a returned error or panic. It does not prove asynchronous hooks, daily operations
or external job effects have finished. No completion is represented by -1.
`regente_scheduler_ticks_total{outcome=...}` counts attempted, completed, failed,
overlap and follower outcomes; duration is the last completed scan.

The first observed business eligibility is recorded separately from execution
capacity. New instance events carry `kind=eligible`. Readiness, planning,
durable ACK, start and finish timestamps produce the five latency stages:
ready_to_planned, planned_to_accepted, accepted_to_started, started_to_finished
and ready_to_started. Readiness latency applies to the first attempt; retry
attempts retain their own planning/ACK/start/finish measurements. Historical
eligibility is not reconstructed.

`regente_execution_stage_seconds` reports nearest-rank p95/p99 **gauges** over
at most the newest 10,000 attempts created in the last hour, with explicit
per-stage observation counts. Incomplete observations are excluded from quantiles;
pending queues, oldest outbox age and uncertain attempts remain independently
visible. Do not apply `rate` or `increase` to these quantiles/count gauges.
Check `regente_capacity_metrics_available` before interpreting missing samples.
Database connections/waits, connected agent slots, active attempts and oldest
audit/outbox age identify saturation without instance IDs in metric labels.

## Independent progress canary

Use a reviewed, non-destructive canary definition in an authorized environment.
The probe admits a real order and therefore has an operational effect:

```sh
python3 scripts/progress-probe.py --base https://scheduler.example \
  --definition approved-canary --token-file /run/secrets/canary-token --deadline 5
```

A pass requires the instance to reach OK with exactly one durable succeeded
attempt and nonzero ACK/start receipts. HTTP health and database readiness alone
cannot pass it. The lab deliberately stops both agent processes while /readyz
remains healthy, requires the canary to detect stalled progress, then resumes
them and verifies all results and external effects. A failed deadline retains
the admitted instance ID; inspect it rather than ordering an automatic duplicate.

## Reproducible profile and scope

Run `python3 scripts/capacity.py` on Linux amd64 with Go 1.26 and Docker Compose.
Only disposable loopback databases named `regente_capacity_*` can be seeded;
the seeder also requires `REGENTE_DISPOSABLE_DB=1`. Each density uses a fresh
database and about 2 KiB per retained terminal snapshot. These rows are **fixtures**,
not a claim that their jobs or effects were executed.

The profile uses one production server with a loopback network boundary,
PostgreSQL and two durable agents (four fast slots and one slow slot).
It offers density/86,400 orders per second with fourfold peaks for the first
60 seconds of each five-minute period. The mix includes short COMMAND jobs,
64 KiB output, three-second slow jobs, one real failure/retry, HTTP effects and
producer/consumer condition gates. Authenticated operator queries run concurrently.
The 30-minute soak includes a deliberate agent pause; its interval and a
30-second margin on each side are excluded only from **normal performance**
statistics. All samples, all-attempt latencies, effects and correctness checks
remain in the evidence. The load producer records its scheduled pause.

Budgets are planning-to-ACK p99 <=5 seconds, first readiness-to-start p99 <=10
seconds, oldest outbox age <=5 seconds, audit lag <=30 seconds, server RSS <=1 GiB
and zero normal admission errors. A tier exceeding any budget stops progression.
No result can justify a higher untested tier. Completion requires no missing or
duplicate lab effects, distinct execution IDs, correct retry attempts and a
verified checkpoint from the independent audit collector.

`capacity.json` and the detailed attempts/samples contain source SHA, dirty state,
CPU/memory/OS/Go/PG settings and image identity, database size, process CPU/RSS,
offered/admitted counts, sample denominators and p95/p99. Workflows preserve these
artifacts even on failure. Capacity code changes must rerun the measured gate.

This is a short synthetic rate/density qualification on one host. It does not
certify a full day at the offered rate, business workloads, HA/NATS/mTLS capacity,
monthly SLOs or a fleet upgrade. Platform/service scope is in
[authenticated releases](authenticated-releases.md); recovery procedures are in
[upgrades](upgrades.md). The measured envelope is published after the gate passes.

The audit exporter change follows a measured bottleneck: eight real records took
8.013 seconds to receive durable ACK at the origin with one record per tick.
A bounded, sequential drain of at most 100 successful ACKs per tick took 1.026
seconds in the same microbenchmark. Order, signatures, fsynced collector receipts,
backoff and checkpoints are preserved. This microbenchmark is not a job
throughput claim.

## Error budget and alert interpretation

The engineering latency objective allows at most 1% of completed observations to
exceed 5 seconds planning-to-ACK or 10 seconds first readiness-to-start in the
bounded one-hour window. `regente_execution_stage_budget_breaches` and
`regente_execution_stage_observations` are gauges with the same window/stage.
This is an observed latency fraction, not a monthly availability counter.
Use the following PromQL for sustained budget exhaustion (for example, `for: 5m`):

```promql
sum without (threshold_seconds) (
  regente_execution_stage_budget_breaches
) / clamp_min(
  regente_execution_stage_observations{stage=~"planned_to_accepted|ready_to_started"},
  1
) > 0.01
```

Alert separately on `regente_capacity_metrics_available == 0` for one minute,
leader completed-tick age >90 seconds (or -1) for two minutes,
oldest outbox age >5 seconds for one minute and audit lag >30 seconds for two
minutes. Interpret age only on a serving leader; followers and external tick
deployments have different expected cadence. An empty sample window is not a
healthy workload claim: monitor the external canary at an approved cadence and
alert on any failed deadline. Investigate outstanding/uncertain attempts as well
as the completed denominator. No latency budget suppresses an integrity incident.

The controlled soak pause must report a healthy HTTP/DB readiness check, a failed
durable canary and a successful new canary after resumption within the 30-second
engineering recovery deadline. That deadline concerns restored progress in this
process pause scenario; it is not disaster RTO for a database restore or an
independent I17 recovery exercise.

Dataset preparation applies the actual empty-database monitoring/resource/logic
backfills before seeding and populates the current frozen monitoring columns.
A preliminary fixture with empty labels triggered 10,000 legacy per-row updates:
its lag reached 81.8 seconds while ACK p99 remained 1.996 seconds, so the tier was
rejected. That is preserved as legacy-bootstrap evidence, not included in the
current-schema steady workload envelope. Legacy backfill/startup capacity is a
separate qualification; no timeout or audit budget was raised to hide this run.

The oldest first eligibility waiting before planning is exposed separately as
regente_execution_oldest_eligible_wait_seconds; retries with an existing runtime
order are excluded. It closes the censored-sample gap: a growing pre-admission
queue can breach its additional conservative 10-second maximum budget before
completed-attempt quantiles show it. A breached investigative tier stops new
offers, drains admitted work within the unchanged 180-second recovery window
and verifies receipts/effects/checkpoint. Only the last fully passing tier proceeds
to the developer soak. Samples are saved during the run and attempts on failed drains.

Profile v2 uses a real UTC business calendar with rollover approximately twelve
hours after each phase setup. The seeder configures it through audited settings
and uses the actual businessclock.BusinessDate for retained rows and frozen
snapshots. This keeps a short qualification window at constant density when UTC
midnight occurs; the clock is not frozen and no runtime order is rewritten. The
actual calendar is recorded per phase. Business rollover qualification remains
in the mandatory I06/I07 integration contracts, not this steady-load profile.
