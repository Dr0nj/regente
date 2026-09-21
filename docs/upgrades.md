# Upgrade and recovery compatibility

An available release, an additive migration or a patch version number is **not**
proof of mixed-version compatibility. Use the same tested server/agent release
unless the exact combination has separate evidence. This guide does not authorize
a production rollout without a workload-specific rehearsal.

## Compatibility matrix

The current schema and supported runtime range are maintained in the
[migration runbook](integration-baseline.md#current-runtime-schema-contract).

| Source / operation | Evidence and supported procedure | Not implied |
|---|---|---|
| Empty DB → current runtime | Fresh/concurrent migration tests on SQLite and PostgreSQL | Shared-file SQLite HA |
| Frozen schema 22 fixture → current runtime | Restore legacy snapshot to an isolated target, then migrate; data assertions in the mandatory lab | Every manually modified legacy installation is safe |
| Schema 23 → current runtime | Migration tests preserve metadata but retire legacy machine secrets; [reissue credentials](agent-identity.md) | Old agents/tokens keep working |
| Schema 24 → current runtime | Stop all old nodes; migrate together; human sessions revoked, federated identities need explicit linking; [authentication upgrade](authentication.md#upgrade-from-schema-24) | Old schema-24 binaries can restart against schema 25 |
| Restart current build / same-build node drain | Restart tests and disposable same-binary drain drill | Cross-version rolling upgrade or end-to-end exactly-once effects |
| Older/future/unknown binary against current DB | Range/history checks refuse unsupported schemas; tests simulate older migration sets and future history | Historical binaries predating these checks are safe to try |
| Return after upgrade | Compatible binary only, or restore a verified pre-upgrade recovery set into a separate target | Copying a `.bak` binary reverses DB migrations |

Tests using older migration sets are schema-contract tests, not certification of
every historical release executable. No mixed-release rolling pair is currently
qualified by this matrix. Expand/contract is a design requirement, not permission
to bypass the runner's supported range or protocol changes.

## Before changing a deployment

1. Record source/target binary version, checksum, DB driver/history, service working
   directory, Git commit, configuration and agent versions. Read release-specific
   migration and identity requirements. Preserve a matching binary/UI bundle.
2. Stop new ordering/dispatch, account for in-flight jobs and external effects, and
   stop **all** control planes before a schema/identity transition. Agent reconnect
   alone does not reconcile uncertain effects. Do not promise a fixed outage time.
3. Capture and verify the [complete recovery set](dr-backup.md): DB, unpublished
   drafts, any local-only definitions and external configuration/secrets. The
   updater's online DB snapshot is not this complete, quiesced recovery set.
4. Rehearse restoration/migration on an isolated target with the intended binaries.
   Never test an older binary on the sole copy of an upgraded database. With writes
   stopped, run `-migrate-only` as the service user against the intended target,
   verify schema/history and follow the [migration runbook](integration-baseline.md#safe-schema-upgrade-and-interrupted-upgrade-recovery).
5. Start one node, check authenticated API access, schema, draft content, definitions
   and agent identities; then bring up other nodes on the same tested version.
   Reconcile in-flight work before resuming ordering. Record actual downtime.

For a rehearsed compatible single-node installation, `regente-update` downloads
and installs the bundle and restarts the service. It does not coordinate a fleet,
prove compatibility, quiesce all writers or back up draft directories. Reinstalling
through `install.sh` has the same migration constraints, not an exemption.

## Recovery is not a destructive schema downgrade

Stop every writer and preserve the failed DB/disk set for diagnosis. Restore the
verified pre-upgrade DB to a **new** target; restore the matching draft/configuration
set before starting a binary that accepts that schema. Restore the original
service working-directory/path layout as described in the DR guide. Validate in
isolation, then deliberately switch the deployment. Reconcile commits, orders and
external effects after the snapshot: they are not recovered by changing binaries.

Never delete migration history, rewrite checksums, run destructive down migrations
or reuse SQLite WAL/SHM files from a different database. A retained `.bak` file is
an artifact for a reviewed recovery plan, not a ready-to-run rollback command.

## Same-binary drain drill

`server/deploy/rolling-upgrade.sh` is a **disposable PostgreSQL same-binary drill**.
It rejects different binary bytes and requires `REGENTE_DISPOSABLE_DB=1`; never
point it at a real deployment. It owns only its spawned PIDs and temporary workspace.
It samples liveness while draining one process and checks leadership transfer.
It does not test all authenticated API routes, workload continuity, agent protocol
compatibility, draft distribution, partitions or different release versions.

For example, with an empty disposable PostgreSQL database and a local build:

```sh
REGENTE_DISPOSABLE_DB=1 REGENTE_PG_DSN="$LAB_DSN" \
  REGENTE_OLD_BIN=./regente-server REGENTE_NEW_BIN=./regente-server \
  ./server/deploy/rolling-upgrade.sh
```

Only a separate reviewed/tested compatibility matrix can justify a future
mixed-version procedure. A successful drain drill does not provide that evidence.
