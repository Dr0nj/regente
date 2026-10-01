# Control plane SLO objectives — regente-server

Reviewed 2026-09-28. These are **engineering objectives and observation signals**,
not measured service guarantees or production acceptance. The operations owner
must approve a workload, measurement window, outage budget and recovery drill.
[Capacity evidence and limits](capacity-guarantees.md) distinguishes historical
reports from the current small [integration profile](integration-baseline.md).

| Dimension | Proposed objective / interpretation | Signal | Qualification limit |
|---|---|---|---|
| Readiness availability | Historical objective: >=99.9% successful probes over an agreed window | GET /readyz, db-unreachable alert | A successful probe is not whole-API or job availability; no measured monthly guarantee |
| Leadership transfer | Historical objective: <=10s after leader loss | regente_is_leader, leader-flapping | Measure detection plus acquisition under the declared topology; not an execution recovery deadline |
| Scheduling freshness | Alert when tick age exceeds 90s | regente_scheduler_last_tick_age_seconds, tick-stalled | Loop freshness does not establish dispatch latency or successful external effects |
| Agent fleet presence | Investigate unexpected online-agent drops | regente_agents_online, agents-drop | Presence is not throughput, concurrency capacity or durable acceptance |
| Process liveness | Supervisor restarts failed processes; configured probe may trigger restart | /livez, systemd/Windows Service/orchestrator | Restart=always restarts exited processes; it alone does not detect every live-but-hung process |
| Recovery | Set RPO/RTO from the complete backup set and measured drill | DB/WAL/backup age, restore logs, data comparison | No universal minutes-to-recover target; drafts and external configuration matter |

## Leadership and execution are different measurements

PostgreSQL leadership uses a session advisory lock. A successor can acquire it
when PostgreSQL releases the old session's lock and the successor polls. Network
failure detection and database availability affect this interval. The old ~1s
and ~4s observations lack a recovered common run manifest; they are historical
reports, not a bound. An atomic claim does not provide durable agent ACK, attempt
fencing or exactly-once external effects. See the
[mechanism matrix](capacity-guarantees.md#dispatch-and-ha-boundaries).

## Verification scope

- Go unit/API tests exercise probes, self-monitoring, backup and concurrency.
  TestLoad_ReadyzConcurrent sends 2,000 readiness requests with 40 workers to an
  in-process HTTP server. The historical ~7.5k req/s report is not a job benchmark.
- The [integration baseline](integration-baseline.md) exercises three synthetic
  orders on two agents and selected restart/restore cases with PostgreSQL/NATS.
  Its lab deadlines and preserved snapshots do not establish a production SLO.
- The legacy [chaos-ha.sh](../server/deploy/chaos-ha.sh) laboratory checks
  leadership transfer in its specific setup. It is not a partition, durable
  result or external-effect qualification suite.
- A restore drill must compare the relevant data and unpublished content in an
  isolated recovery set; /readyz and /api/env alone cannot prove recovery completeness.
  Follow [DR and backup](dr-backup.md).

Sustained capacity, soak, failure recovery and business acceptance remain subject
to the enterprise gates in the [roadmap](roadmap.md), including I16/I17.
