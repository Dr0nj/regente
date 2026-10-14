# Operational recovery and controlled pilot

I17 combines technical recovery evidence with **independent human operation and a
bounded pilot**. Automated rehearsals prepare this work; a passing CI run does not
accept a production installation. I17 remains open until the installation dossier
below is completed and accepted. Never use a critical workload as the first pilot.

## Entry dossier: complete before touching an installation

Keep a private, dated record with these fields. Use sanitized identifiers in public
evidence; never upload databases, tokens, private keys, journals containing output,
environment files or customer definitions to CI artifacts.

| Field | Required record |
|---|---|
| People | Technical owner, independent second maintainer, operational approver, incident contact |
| Installation | Hosts, OS, DB, bus, agents, collector, versions, schema and protocol, Git SHAs |
| Authorization | Disposable or installed environment, permitted disruptions, window and rollback/stop owner |
| Workload | Daily volume, peak ready/s, concurrency, duration/output distribution, dependencies, timezone/windows, retention |
| Effects | Every external destination, how to prove completion/absence, reversibility, idempotency boundaries |
| Recovery objectives | Accepted RPO and RTO, measurement start/end, allowed downtime/data loss, evidence needed to resume |
| Storage | Backup frequency, off-host custody, encryption/decryption, key/provider access, disk and retention budget |
| Pilot | Synthetic stage, bounded reversible non-critical stage, duration, success thresholds, pause and promotion criteria |

The existing I16 envelope is a nominal 10,000 orders/day rate in a 30-minute
synthetic test. The 100,000 profile failed qualification and one million was not
executed. A new workload needs its own approved targets. Do not infer a 24-hour
pilot, disaster RPO/RTO or monthly SLO from the developer soak.

## Capture the complete recovery set

1. Identify and stop all ordering, publication, editing and dispatch triggers on
   every control plane. Drain jobs where possible; explicitly inventory unresolved
   attempts/effects and their agents. Stop writers before the coherent snapshot.
2. Follow [database and draft backup](dr-backup.md). Include the DB, published Git
   objects/SHAs, offline/unpushed workspace state, legacy draft paths and actual
   unpublished content. Preserve permissions and process working directories.
3. Preserve each stable agent identity and its stopped journal, output and WAL
   state. A SQLite backup API snapshot includes committed WAL data; copying a live
   `.db` alone is insufficient. Never initialize a replacement empty journal as a
   shortcut. Inventory internal executor journals as well.
4. Preserve service configuration, compatible binaries/UI, signing keys,
   certificates, credentials and the documented way to regain external provider
   access. Test that access using synthetic credentials in the rehearsal.
5. Save the independently verified collector checkpoint and journal under separate
   custody. Record snapshot completion times, hashes, file owners/modes, source
   version, migration range, Git commit and last known accepted effects in a private
   manifest. A database dump and an old collector copy do not define current truth.

## Restore without replaying post-backup effects

1. Preserve the failed/source environment. Allocate new, isolated targets; never
   overwrite the only recovery copy. Keep outbound execution, notifications, Git
   pushes, incoming agent traffic and external schedulers blocked at the boundary.
2. Restore the matching DB/files/configuration/key set. Use a compatible binary and
   [schema procedure](integration-baseline.md#safe-schema-upgrade-and-interrupted-upgrade-recovery).
   Run `-migrate-only` before serving. Follow the current [upgrade matrix](upgrades.md);
   mixed-version rolling operation is not qualified.
3. Run `-audit-verify -audit-checkpoint /independent/checkpoint.json` with the
   restored DB and key. A locally valid old ledger may still predate an external
   effect. A checkpoint rejection is a **stop**, not a reason to discard/reset the
   collector. Obtain a newer coherent set or enter explicit incident recovery.
4. Start one isolated inspection node with `-scheduler external
   -server-agent=false`, bound to loopback, without agents or external callers.
   **These flags are not a dispatch lock:** authenticated API calls can tick or
   trigger work. Network isolation and absence of mutating triggers remain required;
   `-role api` is not an authorization boundary either.
5. Inspect authenticated settings, orders/attempts/fences, actual draft contents and
   dirty state, Git commit, output conditions and stable agent identity/journal.
   Reconcile all operations after the snapshot with receipts and external evidence.
   Preserve unknown outcomes. Restoring before an effect does not undo that effect.
6. Apply only supported, audited [execution resolution](durable-execution.md#recover-an-unconfirmed-outcome)
   after proving the operation stopped and checking its outcome. Never mark success
   by assumption, bulk replay, edit ledger rows, clear fences or delete journals.
   Some old-snapshot incidents require newer backups or an explicit recovery plan;
   no universal automatic history merge is provided.
7. Record the incident-to-inspection interval and the longer interval until safe
   resumption. Record lost accepted records and the time range they cover. Zero loss
   of a quiesced fixture is distinct from disaster RPO. Resume one bounded synthetic
   workload only after the operational owner accepts the reconciliation.

## Failure matrix and independent execution

| Scenario | Evidence required before proceeding |
|---|---|
| Control-plane loss | Stable execution identity, preserved journal/results, no duplicate effects |
| Database or bus interruption | No unauthorized dispatch/overcommit; recovery of connectivity, leadership and results |
| Audit destination outage | Durable backlog, ordered acknowledged recovery, checkpoint agreement and no missing records |
| Stale backup | Local audit may pass; newer independent checkpoint rejects restore before any resume |
| Missing signing key/provider access | Startup/access fails clearly; recovery of the correct material, never substitute identities |
| Coherent restore and upgrade | New targets, compatible versions, real drafts/configuration and reconciled effects preserved |

The mandatory laboratory already covers node loss, DB/NATS interruption and audit
outage through I10/I11/I14, and draft recovery through I12. I17 adds the integrated
stale/current snapshot rehearsal below. Installed failure drills still require
the entry dossier and authorized window.

The second maintainer must execute the procedure from the recorded artifacts and
runbook without the author operating on their behalf. Record start/end times,
commands, failed steps, help needed, preserved/lost items and the decision to stop
or resume. An AI agent, an automated job or a second process is not that maintainer.
If assistance was required, fix the procedure and repeat the affected independent
step; do not label the assisted run independent.

## Controlled pilot and exit criteria

Begin with synthetic effects isolated from real destinations. Then admit a named,
limited batch of reversible non-critical work. Parallel comparison must simulate
or isolate effects so two schedulers cannot perform the same real action. Preserve
every incident, latency sample and unresolved item, including failed attempts.

Pause on duplicate effects, unauthorized execution, missing audit/journal data,
incomplete daily materialization, wrong attempt attribution, unreconciled outcomes,
failed recovery, breached capacity/recovery budgets or unexpected destinations.
Identify the person who can stop/resume, and how they will be reached.

Accept I17 only when an independent operator reproduced recovery and compatible
upgrade, measured RPO/RTO meet the approved workload targets, the pilot meets its
frozen criteria, no P0/P1 issue blocks that profile, and product/security/operation
reviewers record the decision. Acceptance must name the exact source/release,
environment, workload and evidence. Promotion to critical use is a separate decision.

## Reproducible engineering rehearsal

The mandatory Linux integration gate calls `scripts/recovery_rehearsal.py` for
SQLite and PostgreSQL using its own disposable services and binaries. It:

- commits a local Git definition and creates a real unpublished draft;
- takes a stopped snapshot before one reversible file effect;
- executes the effect through a real durable agent and waits for its journal receipt;
- drains a real TLS collector and snapshots the completed order;
- proves the old snapshot passes local audit but fails the newer independent checkpoint;
- restores the coherent set to a new target, checks key failure and checkpoint
  success, and inspects draft, configuration, Git SHA, attempt, condition and journal;
- leaves dispatch isolated and checks that the one effect was not repeated.

Reports `i17-recovery-sqlite.json` and `i17-recovery-postgres.json` are included in
integration evidence. They record binary hashes, fixture Git SHA, partial restore
timing and explicit limits. They always leave operational acceptance pending.
The collector is a separate process on the same laboratory host, not independent
human custody or a production security boundary.

The optional standalone SQLite command requires Linux, Python, Git, OpenSSL and
matching server/agent binaries. It allocates its own `.integration` run and accepts
no existing DB, server URL or production credentials:

```sh
python3 scripts/recovery_rehearsal.py --server-bin /path/to/regente-server --agent-bin /path/to/regente-agent
```

The ephemeral CA uses process-local `SSL_CERT_FILE` on Linux. Windows does not
use that Unix trust mechanism for the Go exporter; the rehearsal refuses that
host rather than changing system trust or bypassing verification. Windows native
installation evidence remains in I15; it does not qualify this Linux recovery profile.
