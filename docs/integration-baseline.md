# Integration baseline and migration runbook

I00/I01 establish a reproducible engineering baseline. They do not qualify a
critical workload or close the remaining identity, durable execution, HA, draft
storage, security and capacity work. The source baseline is `b88af2a`, schema 23.
See [the migration decision](adr/001-safe-migrations.md).

Verified baseline: [CI run 34588148310](https://github.com/Dr0nj/regente/actions/runs/34588148310)
at `7d24f9ee627950e7530c43c4d74cb56daf9bff86`, clean checkout. Server, agent, web
and mandatory integration passed. The [recorded report](evidence/i00-i01-7d24f9e.json)
contains the executed test names and timings: 3 cluster jobs completed, legacy
restore preserved all 4 fixture entities, and incompatible binary startup was
rejected. Total lab time was 111.455s on that runner; this is not a capacity result.

## Run the mandatory laboratory

Use a disposable Linux/amd64 machine with Git, Go 1.26+, Python 3.10+, OpenSSL, Docker Engine
and Compose v2. Allow registry/module downloads and approximately 8 GiB RAM for
the development IdP, compiler and processes. No Python packages are required.

```sh
git clone https://github.com/Dr0nj/regente.git
cd regente
python3 scripts/integration.py
```

Each invocation owns a unique `.integration/<run-id>` directory, Compose project,
network and loopback-only ephemeral ports. PostgreSQL uses disposable tmpfs;
Keycloak imports `integration/realm.json`; NATS is a real broker. The script builds
the checked-out source, starts two control planes sharing PostgreSQL/NATS and one
COMMAND agent per node, executes the synthetic workloads, restarts a node, dumps
and restores PostgreSQL into another database, then validates the restored schema.
It always stops its own processes and removes its own containers and volumes.
Credentials and permissive redirect configuration are synthetic and **lab only**.
The IdP uses HTTPS with a freshly generated local certificate, explicitly trusted
by the test clients through `SSL_CERT_FILE`; TLS verification stays enabled.
No connection to an existing deployment is required or accepted by this script.

Images are pinned by Linux/amd64 digest: PostgreSQL 17.6 Alpine, NATS 2.11.8 Alpine,
Keycloak 26.3.3. These are reproducibility pins, not a recommended production
version policy. Update them through a reviewed baseline run. Docker Desktop on a
Windows host is not needed when using the GitHub integration job; Windows native
Go tests cover SQLite, while the full cluster profile runs on Linux/amd64.

`baseline.json` and logs live in `.integration/<run-id>/evidence/`. The report records
the exact SHA, dirty-checkout indicator, tool/platform versions, resolved images,
test names, timings and outcomes. The [full verification profile](verification.md)
uploads it within `verification-evidence` in CI. The reusable integration workflow
uploads `integration-evidence` for Release, including on failure. A missing dependency, failed service,
skipped selected test or missing required test fails the gate. Local default
`go test` may still skip optional external tests; it is **not** integration evidence.
Release publication depends on integration success for its selected source ref.
Pushes to `codex/**` run CI so the baseline can be tested before promotion to main.

For a manually managed **disposable** PostgreSQL test database, the Go database
suite accepts a URL in `REGENTE_TEST_PG_DSN`. It creates and removes a unique schema
per test. `REGENTE_REQUIRE_INTEGRATION=1` makes missing PG/OIDC settings fatal.
The full script provisions all settings itself. Kubernetes/cloud executor tests
are outside this baseline and are not claimed as executed.

## Scenario inventory and compatibility

| Scenario | Fixture / evidence | Scope |
|---|---|---|
| Fresh DB, two concurrent migrators, restart | `TestMigrationSafety`, both backends | Executed by mandatory gate |
| v22 legacy upgrade, v23 backfill once | `legacy-data.sql`, frozen `legacy-v22-*.sql` from b88af2a | Token, running job/output, partial daily, draft metadata preserved |
| Statement failure after data and DDL | `statement_failure_rolls_back_and_resumes` | Rollback and retry preserve earlier versions |
| Process killed with open transaction | `process_death_releases_transaction_and_lock` | Real child process, shared lock/transaction primitives, partial DDL and data; then restart runner |
| SQL/checksum drift, absent hash, gap/future version | `refuse_*` | Startup blocked before application services |
| Interrupted old autocommit upgrade | `legacy_partial_failure_*` | Fails closed; no automatic guessed repair |
| SQLite pre-upgrade backup/restore | `TestMigrationLegacySQLiteBackupRestore` | Independent restored file upgrades with preserved data |
| PG dump/restore | `restored-schema.log`, `legacy-restored-upgrade.log`, report | Different databases; legacy v22 restored then upgraded; current runtime and completed orders preserved |
| Real OIDC authorization-code flow | `TestIntegrationOIDC_AuthCodeFlow` | Keycloak discovery, login, callback and protected API; not I03 security qualification |
| Distributed agents and execution | cluster report, node/agent logs | Both agents visible from both nodes; jobs pinned to each node complete; repeat Order Folder does not reexecute effects |
| Shared draft recovery, SQLite and PG | `draft_recovery` report; real backup/restore scripts and separate server processes | Node A/B edit and stale-write rejection; restart without cache; DB-only restore preserves content/dirty status |
| Same-binary PG drain | `same-binary-drain.log` | Owned processes, sampled liveness and leadership transfer; not mixed-version compatibility |
| Current schema documented correctly | `TestMigrationRunbookContract` | Compares runbook to constants and migrated DB; rejects missing/duplicate/stale current statements |
| Daily partial chunk/restart, source freezing and corrupt snapshot | TestI07DailyRecoveryIntegration on SQLite/Postgres | [Daily recovery](daily-recovery.md); 5,001-order synthetic scenario, not capacity qualification |
| Isolated attempts, atomic dispatch and result, concurrent admission/cancellation, restart and lease uncertainty | TestI08AttemptIntegration on SQLite/Postgres; execution unit suite | Opt-in development [attempt laboratory](adr-i08-attempts.md); synthetic v2 client only, legacy dispatch and conditions unchanged |
| Agent journal restart, lost receipts, real runtime results and operator API/CLI | TestI09RealAgentJournalLostReceiptAndUncertainRestart and TestI10RealAgentRuntimeLostReceiptAndUncertainRestart, SQLite/Postgres; TestI10OperatorAPIAndCLIContracts | Actual agent process killed/restarted; identity, sysout, RBAC and audited decisions |
| Atomic runtime conditions, retry, resource holds, On/Do and external uncertainty | TestI10PostgresRuntimeContracts and SQLite scheduler suite | Fault-injected transactions, restart, real OpenSSH and internal HTTP; unknown effects are never automatically repeated |
| Real server SIGKILL while an agent executes | durable_process_recovery report, SQLite/Postgres | Actual server and agent binaries; result_pending journal during downtime, one non-idempotent effect, current identity and condition preserved |
| Browser execution recovery | I10 browser scenario | Actual server restart and audited resolution from Execution tab; no mocked API |
| Draft content/CAS/ownership/publication recovery | TestI12DraftContracts, SQLite/Postgres; I12 browser | Forms/CODE/bulk/Mass Update/undo/layout, concurrent writes, Git conflict, legacy migration, export/import and publication receipt |

| Profile | Baseline support |
|---|---|
| SQLite, one runtime node, local disk/WAL | Go tests on Windows and Linux; concurrent *upgrade* contention tested; not shared-file HA |
| PostgreSQL 17.6, two Linux/amd64 nodes, NATS | Full laboratory; separate process restart; no fencing/partition guarantee |
| Server and agent from same SHA, legacy WS and durable v2 | Legacy cluster plus v2 real-process recovery; HTTP/SSE covered by API/agent tests |
| Other PostgreSQL versions, mixed binaries, arm64 cluster, external IdPs | Not qualified by this baseline |
| Pre-runner binaries | Stop all nodes before first upgrade; they cannot enforce the new history checks |

## Reference workload and engineering targets

The measured CI workload is deliberately small: **3 orders in one synthetic daily**,
a burst of 3 eligible orders from one request, at most 3 concurrent commands across
2 agents, two short file operations and one 10-second command (timeouts 30/45s).
Output is a few short lines. Schedule is disabled in fixtures; explicit Order Folder
selects them, with no business window or automatic retries. The append workload
exposes duplicate effects; the replacement workload is idempotent. This profile
detects broken wiring and data preservation, **not throughput capacity**.

The engineering maintainer owns these laboratory thresholds; an operations owner
must separately approve workload-specific targets before a pilot. Numbers below
are regression budgets tied to current mechanisms or a future contract, not SLAs:

| Dimension | Laboratory target and rationale | Qualification gate |
|---|---|---|
| Migration startup | 2-minute default total deadline; configurable for measured large backfills | I01; SQLite driver busy wait may add up to its configured busy timeout |
| Presence propagation | Within 75s test deadline, allowing the current 5s announcement/15s TTL plus CI startup noise | I00 wiring test, not a low-latency SLA |
| Dispatch durable ACK | Proposed ≤5s p99, separating a 2s default tick from network/storage budget | Protocol 2 implements durable acceptance/result receipts; p99 latency is not qualified by the functional recovery fixtures |
| Active agent credential revocation | ≤5s on both nodes; 1s revalidation and bounded DB query | I02 WS/HTTP/SSE machine identity matrix plus cross-process cluster measurement; human session revocation and folder-scoped web events are delivered in I04 ([contract](web-events.md), [evidence](evidence/i04-b6db081.json)) |
| Reconciliation after recovery | Proposed ≤30s for the 3-order reference set, two 15s presence intervals | I10 functional recovery records measured fixture times; I11 HA/partition guarantees remain separate |
| Availability | Every scripted probe succeeds after startup/restart readiness; no monthly availability claim | I16 will measure sustained availability and approved outage budget |
| RPO | Zero committed-order loss for the exact stopped-node/restore snapshots in this lab | I01 checks snapshot preservation; disaster RPO depends on WAL/backup cadence |
| RTO | Restore and schema verification within the 20-minute CI job budget; report actual seconds | I16 must set a workload-sized RTO, including operator and infrastructure time |
| Soak | Proposed 30-minute developer soak, then 24h pilot rehearsal with a frozen load profile | I16; short I00 smoke is not a completed soak |

Before a pilot, the operations owner records expected daily count, peak ready/s,
maximum concurrency, duration/output distributions, business windows/timezone,
retention, topology/platforms and backup constraints. No volume is inferred from
a prospective organization. The resulting profile replaces the synthetic one;
capacity and business acceptance stay open until then.

## Current runtime schema contract

Historical DOC-A recovery extension (before shared draft storage): [CI 35639790032](https://github.com/Dr0nj/regente/actions/runs/35639790032)
at `af75c51` passed the complete-set/DB-only draft drills on SQLite and PostgreSQL,
the same-binary drain and the schema-document contract. The
[recorded evidence](evidence/doc-a-af75c51.json) is synthetic laboratory evidence,
not production or mixed-version qualification.

Current runtime schema: **31**; supported range: **[31,31]**.

`TestMigrationRunbookContract` compares this current statement with the migration
constants and an actual fresh database. Historical fixture versions below remain
intentional. See the [compatibility matrix](upgrades.md) before choosing binaries.

## Safe schema upgrade and interrupted-upgrade recovery

1. Record the source/target binary versions and schema history. Stop old control
   planes, quiesce writes and retain definitions plus draft directories separately
   from the database for unmigrated legacy drafts. Schema 30 stores verified shared
   content in the DB; missing legacy clones remain recovery-required.
2. Take and verify a backup using [the backup guide](dr-backup.md). Never copy only
   the live SQLite `.db` while ignoring WAL. For PostgreSQL, use `pg_dump -Fc` or
   the established PITR procedure. Confirm restoration on an isolated target.
3. Run the new binary with `-migrate-only` and the intended database configuration.
   Increase `-migration-timeout` only from a measured rehearsal; e.g. `10m`.
4. Verify the logged schema and range against the current runtime contract above.
   Schema-23 agent
   credentials require [explicit reissue](agent-identity.md). History is in
   `schema_migrations`; hashes and `applied`/`legacy-adopted` provenance are in
   `schema_migration_checksums`. Legacy adoption cannot prove historical SQL or
   detect every pre-existing manual schema mutation; inspect/rehearse old databases.
   Schema-24 upgrades revoke human sessions; follow the
   [identity transition](authentication.md#upgrade-from-schema-24).
5. Start the same tested binary on all control planes. A failed migration stops
   startup before the scheduler/API. With this runner, a process interruption rolls
   back the in-progress version; rerun the same binary to continue after the last
   committed version. Do not remove version rows or overwrite hashes.
6. A partial migration left by an older autocommit runner needs an explicit repair
   review or restoration of the verified pre-upgrade backup to a **new** database.
   Preserve the failed database for diagnosis. Point the matching binary at the
   restored database only after checking data, history and isolated smoke results.
   This is recovery by restore, not destructive schema downgrade.

New migrations must be additive first and preserve existing SQL. Every version
must be registered for both dialects. SQL containing embedded semicolons or
operations requiring autocommit is unsupported by this runner: design a separate
verified resumable procedure before introducing it. PostgreSQL automatically rejects
nontransactional DDL inside the transaction; the runner never retries it in autocommit.

## I11 shared resources and leadership

[HA resource contracts](ha-resources.md) are exercised by TestI11 on both supported backends and mandatory PostgreSQL leadership tests. The runner also records i11_ha with two servers, a follower-connected agent, quota changes, PostgreSQL/NATS interruption and leader SIGKILL. These are correctness proofs within the synthetic laboratory, not capacity or pilot qualification.

## I13 execution security

The [execution security profile](execution-security.md) is exercised by TestI13CertificateBindingAndActiveRevocation, TestI13RealAgentSecretsMTLSAndEgress and TestI13InternalHTTPPolicyAndSSH on both SQLite and PostgreSQL, plus secret authorization/outage and pinned-egress tests. CI and release also require installed systemd HTTP and COMMAND cells, including actual denied host egress. This is a synthetic correctness profile; cloud managers and hostile-code isolation are outside its qualification.
