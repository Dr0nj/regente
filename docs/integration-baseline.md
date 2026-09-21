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

Use a disposable Linux/amd64 machine with Git, Go 1.25+, Python 3.10+, OpenSSL, Docker Engine
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
test names, timings and outcomes. GitHub CI and Release upload that directory as
`integration-evidence`, including on failure. A missing dependency, failed service,
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
| Complete draft recovery vs DB-only control, SQLite and PG | `draft_recovery` report; real backup/restore scripts, tar and restarted processes | Unpublished content and dirty status verified with same relative path layout; DB-only recovery loses missing drafts; not distributed durability |
| Same-binary PG drain | `same-binary-drain.log` | Owned processes, sampled liveness and leadership transfer; not mixed-version compatibility |
| Current schema documented correctly | `TestMigrationRunbookContract` | Compares runbook to constants and migrated DB; rejects missing/duplicate/stale current statements |
| Daily recovery, distributed drafts, uncertain effects | preserved synthetic fixtures | Broader guarantees remain reserved for I07/I10/I12 |

| Profile | Baseline support |
|---|---|
| SQLite, one runtime node, local disk/WAL | Go tests on Windows and Linux; concurrent *upgrade* contention tested; not shared-file HA |
| PostgreSQL 17.6, two Linux/amd64 nodes, NATS | Full laboratory; separate process restart; no fencing/partition guarantee |
| Server and agent from same SHA, WS transport | Full cluster run; HTTP/SSE remain covered by API/agent tests |
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
| Dispatch durable ACK | Proposed ≤5s p99, separating a 2s default tick from network/storage budget | I08/I09 must implement and measure; current transport has no durable ACK |
| Active agent credential revocation | ≤5s on both nodes; 1s revalidation and bounded DB query | I02 WS/HTTP/SSE machine identity matrix plus cross-process cluster measurement; human session revocation remains I04 |
| Reconciliation after recovery | Proposed ≤30s for the 3-order reference set, two 15s presence intervals | I10/I11; increase workload only with measured evidence |
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

Current runtime schema: **25**; supported range: **[25,25]**.

`TestMigrationRunbookContract` compares this current statement with the migration
constants and an actual fresh database. Historical fixture versions below remain
intentional. See the [compatibility matrix](upgrades.md) before choosing binaries.

## Safe schema upgrade and interrupted-upgrade recovery

1. Record the source/target binary versions and schema history. Stop old control
   planes, quiesce writes and retain definitions plus draft directories separately
   from the database. Draft content is not yet guaranteed by shared metadata.
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
