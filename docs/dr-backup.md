# DR: database, unpublished drafts and configuration

Recovery requires a **set of artifacts**, not just the database. Use the
[upgrade compatibility matrix](upgrades.md) and [migration runbook](integration-baseline.md#safe-schema-upgrade-and-interrupted-upgrade-recovery)
before starting any restored binary. Stop every control plane and other writer
for a coherent recovery set. Account for in-flight agents and external effects.

## What must survive

| Artifact | Contains | Recovery requirement |
|---|---|---|
| SQLite snapshot / PG dump or PITR | Orders, users, credentials, settings, history, **durable draft content/revisions/receipts and legacy metadata** | Consistent DB backup and tested restoration |
| Git remote and recorded commit | Published definitions | Preserve access/commit; not unpublished edits |
| Workspace on disk | Local/offline definitions and local Git state | Preserve local-only changes; not everything was necessarily pushed |
| `sessions/`, including hidden `.git` and untracked files | Legacy drafts not yet migrated; reconstructible caches for migrated drafts | Preserve legacy clones/verified exports until shared content is verified |
| Environment/config, secret provider, keys/certificates, service unit | Configuration outside DB | Secure backup or documented reprovisioning; no secrets in public evidence |
| Matching binary/UI and manifest | Versions, checksums, working directory, paths, owner/modes | Compatible executable and original layout |

`backup.sh`, `restore.sh` and the updater's snapshot operate on the **DB only**.
They do not save/restore session directories, workspace files or external secrets.
DB backups may also contain sensitive settings: restrict permissions, encrypt/store
off-host under your policy, and test decryption/restoration.

### Draft paths are part of the recovery contract

Sessions live under `./sessions` relative to the **process working directory**,
not `-workspace` or `-db`. The standard systemd unit uses
`WorkingDirectory=/var/lib/regente`; drafts normally live in
`/var/lib/regente/sessions`. Inspect the actual unit and `design_sessions.path`;
custom deployments may use a different layout.

Schema 30 makes new/migrated [Design drafts](shared-drafts.md) durable in the DB.
Each node reconstructs its own cache; absent paths never delete shared metadata.
Boot migrates only a verified legacy clone inside that node's session root and
retains its original copy. Missing/foreign clones remain recovery-required. Restore
legacy files before opening those drafts, using the original working directory/path
layout. Do not repair paths by ad-hoc SQL; rehearse relocation separately.

| Event | What is needed |
|---|---|
| Refresh / navigation | Same authenticated draft and editing revision |
| Process restart or another node | Shared DB, compatible build and same Git source/branch; cache reconstructs |
| Container/disk loss | DB backup includes migrated/new content; external config/workspace remain separate |
| Unmigrated legacy draft | Original clone/path layout or verified export; metadata alone lacks content |

## Capture a quiesced recovery set

1. Freeze writes/publish/ordering, account for running jobs and stop every control
   plane. Record versions, Git commit, DB history, service working directory,
   session paths, owner and permissions in a private manifest.
2. Snapshot the DB as below. Never copy only a live SQLite `.db` and ignore WAL.
3. Archive legacy session/workspace directories, including hidden and untracked files.
   Explicitly record when no sessions exist; do not silently omit paths referenced
   by the DB. Retain required external configuration.
4. Verify archive contents/hashes and restore in isolation. Backup command success
   alone does not prove recoverability.

POSIX example for a **verified standard layout**, after stopping all writers:

```sh
umask 077
RECOVERY_DIR=$(mktemp -d /var/backups/regente-recovery.XXXXXX)
# Standard SQLite layout; execute the backup as the service owner.
# For PostgreSQL substitute the PG recipe below.
REGENTE_BIN=/usr/local/bin/regente-server REGENTE_DB_DRIVER=sqlite \
  REGENTE_DB=/var/lib/regente/regente.db ./server/deploy/backup.sh "$RECOVERY_DIR"
# Only after checking these are the real directories and both exist:
tar -C /var/lib/regente -czf "$RECOVERY_DIR/files.tar.gz" sessions workspace
tar -tzf "$RECOVERY_DIR/files.tar.gz"   # inspect .git and unpublished files
```

This does not automatically capture `/etc/regente` or a secret provider: use your
secure configuration-backup procedure. For multiple nodes with local sessions,
collect every relevant node's files/path ownership; do not merge blindly. An
online DB snapshot plus a directory copied while edits continue is not a coherent
recovery set. The complete-set recipe requires quiescence.

## Database-only snapshots

### SQLite

The binary uses `VACUUM INTO` for a consistent online DB snapshot. Run as the
service owner so opening DB/WAL does not change ownership:

```sh
REGENTE_BIN=/usr/local/bin/regente-server REGENTE_DB_DRIVER=sqlite \
  REGENTE_DB=/var/lib/regente/regente.db ./server/deploy/backup.sh "$RECOVERY_DIR"
```

A consistent schema-30 DB snapshot includes committed shared draft revisions.
DB consistency does not make independently copied **legacy** draft files consistent.
Periodic online DB backups supplement, not replace, complete recovery sets.

### PostgreSQL

```sh
REGENTE_DB_DRIVER=postgres REGENTE_DB="$SOURCE_DSN" \
  ./server/deploy/backup.sh "$RECOVERY_DIR"  # pg_dump -Fc
```

Use protected credentials/service configuration, not secrets in shell history.
A dump does not capture later commits. PITR needs a separately tested
base-backup/WAL-archiving procedure; align its recovery point with filesystem
artifacts. This guide promises neither zero RPO nor a fixed RTO.

Schedule DB snapshots with cron/systemd timers/Kubernetes as appropriate, and
schedule complete-set restore drills. Track retention of the **whole** set:
`REGENTE_BACKUP_KEEP` only manages DB files produced by `backup.sh`.

## Restore drill: new isolated target, never overwrite the only copy

1. Keep the source/failed deployment stopped and preserved for diagnosis. Use an
   isolated host/container with outbound jobs, Git pushes and notifications
   disabled. Allocate a new SQLite filename or a newly created empty PG database.
2. Restore DB with `restore.sh`. It refuses an existing SQLite file or WAL/SHM
   sidecar; PG restore stops on errors and does not clean existing objects. After
   a failed disposable restore, create a fresh target before retrying.

```sh
# PostgreSQL: create the empty target first; TARGET_DSN is not the source.
REGENTE_DB_DRIVER=postgres REGENTE_DB="$TARGET_DSN" \
  ./server/deploy/restore.sh /private/recovery/regente-TIMESTAMP.dump
# SQLite: the parent exists, but recovered.db and its sidecars do not.
REGENTE_DB_DRIVER=sqlite REGENTE_DB=/isolated/recovered.db \
  ./server/deploy/restore.sh /private/recovery/regente-TIMESTAMP.db
```

3. Restore required legacy/workspace filesystem archives into an **empty isolated layout**,
   recreating the original working directory and recorded paths. Inspect before
   extraction. Standard layout on the isolated host:
   `tar -C /var/lib/regente -xzf /private/recovery/files.tar.gz`.
   Restore owner/modes and configuration from the secure manifest. Never extract
   over a live deployment.
4. Run a matching compatible binary with `-migrate-only` and inspect history/range.
   Start one isolated process with `-scheduler external -server-agent=false`,
   no external tick source or agents, and controlled Git access. Check readiness,
   authenticated settings, order counts and **actual unpublished draft content
   and dirty status**, not just metadata counts.
5. Compare file checksums/expected edits. Only after passing and reconciling
   writes/effects since the snapshot deliberately switch deployment targets and
   resume agents/ordering. Migration may require users to log in again.

The mandatory Linux integration laboratory exercises actual SQLite/PG backups,
a new restored DB and restarted processes. Both the complete-set and **DB-only**
targets must preserve actual unpublished content and dirty status without the old
session cache. It also edits on node A, opens/edits on B, rejects a stale A write
and restarts A without its original session disk. See `draft_recovery` in
`baseline.json`. Legacy migration and missing-clone preservation are covered by
`TestI12DraftContracts`. These are synthetic recovery contracts, not workload-sized
RPO/RTO, shared-file SQLite HA or general partition qualification.
