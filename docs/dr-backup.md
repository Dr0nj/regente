# 🛟 DR / Backup / Restore — regente-server (R6) + config no restart (R4)

> Como **sobreviver a desastre, restart de container e failover HA sem perder estado nem
> config**. Vale para os dois backends do state store: **SQLite** (nó único) e **Postgres**
> (produção/HA). Scripts em [`../server/deploy/backup.sh`](../server/deploy/backup.sh) e
> [`../server/deploy/restore.sh`](../server/deploy/restore.sh).

## Princípio (o que precisa sobreviver)

Tudo que é **estado durável** vive no state store (DB), não no filesystem efêmero do
processo/container:

- **runtime**: `instances`, `daily_runs`, `instance_events`, `conditions`, `resources`,
  `sla_breaches`, `alert_*`, `agent_tokens`…
- **config (R4)**: tabela `settings` — `env_label`, `github_token`, `webhook_secret`,
  credenciais de alerta (Slack/webhook/SMTP/PagerDuty).
- **definitions**: YAML versionado no **Git** (workspace) — já tem DR próprio (é um repo).

> Logo: **perder o container não perde nada** desde que o DB esteja fora dele (volume
> persistente p/ SQLite, servidor Postgres externo) e tenha backup. As duas seções abaixo
> garantem isso.

---

## R4 — config persiste no restart (checklist)

A config de runtime já é lida do DB no boot (`settings`). Para que sobreviva a restart /
container efêmero / failover, garanta:

- [ ] **SQLite**: o arquivo `-db` está num **volume persistente** (não na camada efêmera do
  container). Em k8s: `PersistentVolumeClaim` montado no path do `-db`.
- [ ] **Postgres**: o `-db` aponta para um **PG externo** (gerenciado ou StatefulSet com PVC),
  nunca um PG dentro do mesmo pod efêmero.
- [ ] **Segredos**: preferir o **secrets provider** (`REGENTE_SECRET_*` / `-secrets-file`) a
  guardar PAT/webhook no banco em claro — assim a config sensível vem do gestor de segredos
  e o DB carrega só o resto.
- [ ] **Smoke de restart**: subir → setar `env_label`/token → derrubar → subir → conferir que
  voltou (coberto pelo teste `TestSettings_SurviveRestart`).

---

## R6 — backup

### SQLite (nó único)

Snapshot **online e consistente** via o próprio binário (`VACUUM INTO`, pure-Go, sem parar o
servidor):

```sh
regente-server -db ./regente.db -backup /backups/regente-$(date +%F).db
# ou pelo script (com retenção):
REGENTE_DB_DRIVER=sqlite REGENTE_DB=./regente.db ./server/deploy/backup.sh /backups
```

### Postgres (produção / HA)

Backup **lógico** consistente (custom format):

```sh
REGENTE_DB_DRIVER=postgres REGENTE_DB="postgres://u:p@host/db?sslmode=require" \
  ./server/deploy/backup.sh /backups            # roda pg_dump -Fc
```

**PITR (point-in-time recovery)** — para recuperar até o segundo antes do incidente, o
`pg_dump` (snapshot) **não basta**; habilite WAL archiving no Postgres:

- **PG gerenciado** (Neon / Cloud SQL / RDS / Supabase): ligue *PITR / backups contínuos* no
  painel — o provedor arquiva o WAL. É o caminho recomendado (zero infra).
- **PG self-hosted**: `wal_level=replica`, `archive_mode=on`,
  `archive_command='... cp %p /wal-archive/%f'` + um `pg_basebackup` periódico. Restore =
  basebackup + replay do WAL até `recovery_target_time`.

### Agendamento

- **cron**: `0 */6 * * *  REGENTE_DB_DRIVER=postgres REGENTE_DB=... /opt/regente/backup.sh /backups`
- **systemd timer**: um `.service` chamando o script + um `.timer` `OnCalendar=*-*-* 0/6:00:00`.
- **k8s CronJob**: container com o binário/`pg_dump`, `schedule: "0 */6 * * *"`, PVC/secret
  montados, comando = `./backup.sh`.

---

## R6 — restore (drill)

> **Pare o servidor antes de restaurar.** Em HA, pare todos os nós (o líder reassume sozinho
> depois).

```sh
# Postgres:
REGENTE_DB_DRIVER=postgres REGENTE_DB="postgres://u:p@host/db" \
  ./server/deploy/restore.sh /backups/regente-20260623-030000.dump

# SQLite:
REGENTE_DB_DRIVER=sqlite REGENTE_DB=./regente.db \
  ./server/deploy/restore.sh /backups/regente-20260623-030000.db
```

Validação pós-restore:

1. Suba 1 nó e confira `GET /readyz` → **200** com `db.ok=true`.
2. `GET /api/env` → o `env_label` esperado voltou (prova R4).
3. `GET /metrics` → `regente_instances{...}` reflete o order_date restaurado.
4. Em HA, suba os demais nós; confira que há **1 líder único** no `/readyz`.

> **Faça o drill de verdade periodicamente** — backup que nunca foi restaurado não é backup.
