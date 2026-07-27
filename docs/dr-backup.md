# 🛟 DR / Backup / Restore — regente-server (R6) + config across restarts (R4)

> How to **survive a disaster, a container restart and an HA failover without losing state or
> configuration**. Applies to both state store backends: **SQLite** (single node) and
> **Postgres** (production/HA). Scripts live in
> [`../server/deploy/backup.sh`](../server/deploy/backup.sh) and
> [`../server/deploy/restore.sh`](../server/deploy/restore.sh).

## Principle (what has to survive)

Everything that is **durable state** lives in the state store (the DB), not on the ephemeral
filesystem of the process or container:

- **runtime**: `instances`, `daily_runs`, `instance_events`, `instance_output`, `conditions`,
  `resources`, `sla_breaches`, `alert_*`, `agent_tokens`…
- **config (R4)**: the `settings` table — `env_label`, `github_token`, `webhook_secret`, alert
  credentials (Slack/webhook/SMTP/PagerDuty).
- **definitions**: YAML versioned in **Git** (the workspace) — it already has its own DR: it is
  a repository.

> So: **losing the container loses nothing**, as long as the DB lives outside it (a persistent
> volume for SQLite, an external Postgres server) and is backed up. The two sections below make
> sure of that.

---

## R4 — configuration survives a restart (checklist)

Runtime configuration is already read from the DB at boot (`settings`). For it to survive a
restart, an ephemeral container or a failover, make sure that:

- [ ] **SQLite**: the `-db` file sits on a **persistent volume**, not on the container's
  ephemeral layer. On k8s: a `PersistentVolumeClaim` mounted at the `-db` path.
- [ ] **Postgres**: `-db` points at an **external PG** (managed, or a StatefulSet with a PVC) —
  never a PG inside the same ephemeral pod.
- [ ] **Secrets**: prefer the **secrets provider** (`REGENTE_SECRET_*` / `-secrets-file`) over
  storing the PAT or webhook in the database in plaintext — that way sensitive configuration
  comes from your secret manager and the DB only carries the rest.
- [ ] **Restart smoke test**: start → set `env_label`/token → stop → start → check that it came
  back (covered by the `TestSettings_SurviveRestart` test).

---

## R6 — backup

### SQLite (single node)

An **online, consistent** snapshot taken by the binary itself (`VACUUM INTO`, pure-Go, without
stopping the server):

```sh
regente-server -db ./regente.db -backup /backups/regente-$(date +%F).db
# or through the script (with retention):
REGENTE_DB_DRIVER=sqlite REGENTE_DB=./regente.db ./server/deploy/backup.sh /backups
```

### Postgres (production / HA)

A consistent **logical** backup (custom format):

```sh
REGENTE_DB_DRIVER=postgres REGENTE_DB="postgres://u:p@host/db?sslmode=require" \
  ./server/deploy/backup.sh /backups            # runs pg_dump -Fc
```

**PITR (point-in-time recovery)** — to recover up to the second before an incident, `pg_dump`
(a snapshot) is **not enough**; enable WAL archiving in Postgres:

- **Managed PG** (Neon / Cloud SQL / RDS / Supabase): turn on *PITR / continuous backups* in the
  console — the provider archives the WAL. This is the recommended path (zero infrastructure).
- **Self-hosted PG**: `wal_level=replica`, `archive_mode=on`,
  `archive_command='... cp %p /wal-archive/%f'` plus a periodic `pg_basebackup`. Restore =
  basebackup + WAL replay up to `recovery_target_time`.

### Scheduling

- **cron**: `0 */6 * * *  REGENTE_DB_DRIVER=postgres REGENTE_DB=... /opt/regente/backup.sh /backups`
- **systemd timer**: a `.service` that calls the script plus a `.timer` with
  `OnCalendar=*-*-* 0/6:00:00`.
- **k8s CronJob**: a container with the binary (or `pg_dump`), `schedule: "0 */6 * * *"`, the
  PVC/secret mounted, and `./backup.sh` as the command.

---

## R6 — restore (drill)

> **Stop the server before restoring.** In HA, stop every node (the leader re-elects itself
> afterwards).

```sh
# Postgres:
REGENTE_DB_DRIVER=postgres REGENTE_DB="postgres://u:p@host/db" \
  ./server/deploy/restore.sh /backups/regente-20260623-030000.dump

# SQLite:
REGENTE_DB_DRIVER=sqlite REGENTE_DB=./regente.db \
  ./server/deploy/restore.sh /backups/regente-20260623-030000.db
```

Post-restore validation:

1. Start one node and check `GET /readyz` → **200** with `db.ok=true`.
2. `GET /api/env` → the expected `env_label` is back (this proves R4).
3. `GET /metrics` → `regente_instances{...}` reflects the restored order_date.
4. In HA, start the remaining nodes and confirm there is **exactly one leader** in `/readyz`.

> **Run the drill for real, periodically** — a backup that has never been restored is not a
> backup.
