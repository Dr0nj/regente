# Deploying `regente-server` — supervision (R1)

> **A critical orchestrator never runs as a loose process.** It assumes the process *will* die
> (crash, OOM, deploy, reboot) and restarts under a supervisor. These artifacts provide
> **process liveness**, not a guarantee against lost drafts or uncertain execution effects.
> Preserve the [complete recovery set](../../docs/dr-backup.md).

| File | What it is for |
|---|---|
| `regente-server.service` | A systemd unit with `Restart=always` (Linux). |
| `server.env.example` | Configuration through env vars (token, DB, GitOps…). Copy it to `/etc/regente/server.env`. |
| `install-linux.sh` | Installs the unit, the binary and the env file, then enables and starts it. |
| `install-windows.ps1` | A Scheduled Task (boot + automatic restart) on Windows. |
| `configure.sh` | Guided setup, installed as `regente-configure`. |
| `update.sh` | Upgrade in one command, installed as `regente-update`: database snapshot, latest release, restart. |
| `backup.sh` · `restore.sh` | Database-only snapshots/recovery; drafts/configuration require separate backup. |
| `chaos-ha.sh` · `rolling-upgrade.sh` | Laboratory failover / same-binary drain, not mixed-version qualification. |

## Linux (systemd)

```bash
cd server && CGO_ENABLED=0 go build -o regente-server .
# optional: build the UI so it is served alongside, single-origin (UI+API+WS on one port)
(cd ../app && VITE_REGENTE_SERVER_URL=@origin npm ci && npm run build)
sudo ./deploy/install-linux.sh          # detects ../app/dist and sets REGENTE_SPA_DIR itself
sudo regente-configure                   # guided: strong token, GitHub PAT/repo, domain
sudo systemctl restart regente-server
journalctl -u regente-server -f
```

> **The UI alongside:** if `app/dist` exists (or `SPA_DIR=` points at a built UI), the installer
> copies it to `/var/lib/regente/app` and writes `REGENTE_SPA_DIR` into `server.env` — the server
> then serves the SPA on the SAME port (the frontend resolves `@origin`, so it works behind any
> domain or tunnel without a rebuild). Without `app/dist` it installs **the API only**, with a
> warning.
>
> **No toolchain on the VPS:** use the one-liner that downloads the ready-made bundle (binary +
> UI):
> `curl -fsSL https://github.com/Dr0nj/regente/releases/latest/download/install.sh -o regente-install.sh && sudo bash regente-install.sh`.

Kill the process (`sudo systemctl kill -s SIGKILL regente-server`, or `kill -9`) and it comes back
according to the supervisor policy. Recovery of DB state and local draft files depends on
their persistent volumes; reconcile interrupted execution separately.

## Windows (Scheduled Task)

```powershell
cd server; go build -o regente-server.exe .
.\deploy\install-windows.ps1 -Token <a-strong-token> `
  -GitSource https://github.com/<owner>/<your-workspace>.git
```

> The workspace repository is **yours**, one per installation — there is no default. Leave
> `-GitSource` out and the server runs offline, keeping the definitions on local disk; point it
> at an empty repository and the first start writes the initial content and pushes it.

## Container / Kubernetes / serverless

See [`../../deploy/`](../../deploy): `Dockerfile` (distroless) + `knative-service.yaml`
(`readinessProbe`→`/readyz`, `livenessProbe`→`/livez` — a process that hangs while still listening
gets restarted) + `cronjob.yaml` (the external trigger for `-scheduler=external`).

## DR / backup (R6)

`backup.sh` and `restore.sh` cover the **database only**: SQLite (`-backup` = an online
`VACUUM INTO`) and Postgres (`pg_dump`/`pg_restore`). Restore into a new target, with all
writers stopped; restore drafts/configuration before startup. The full runbook is in
[`../../docs/dr-backup.md`](../../docs/dr-backup.md).

Before using `regente-update`, read the [compatibility matrix](../../docs/upgrades.md).
Its DB snapshot and retained `.bak` binary do not constitute a complete rollback.

```sh
REGENTE_DB_DRIVER=postgres REGENTE_DB="$DSN" ./deploy/backup.sh /backups   # schedule via cron/timer/CronJob
```

## Configuration through env vars (summary)

Every relevant flag reads an environment variable, so the unit or manifest needs no arguments:

| Env | Flag | Default |
|---|---|---|
| `REGENTE_ADDR` | `-addr` | `:8080` |
| `REGENTE_SPA_DIR` | `-spa-dir` | — (single-origin UI; the installer sets it when it finds `app/dist`) |
| `REGENTE_DB_DRIVER` | `-db-driver` | `sqlite` |
| `REGENTE_DB` | `-db` | `./regente.db` (a SQLite path or a Postgres DSN) |
| `REGENTE_WORKSPACE` | `-workspace` | `./workspace` |
| `REGENTE_TOKEN` | `-api-token` | `dev-token` |
| `REGENTE_GIT_SOURCE` | `-git-source` | — (**empty = offline**, definitions on local disk; set it to **your own** workspace repository) |
| `REGENTE_SECRET_GITHUB_TOKEN` | (secrets provider) | — (keeps the PAT out of the DB in plaintext) |
| `REGENTE_ROLE` / `REGENTE_SCHEDULER` | `-role` / `-scheduler` | `all` / `internal` |

> **Persistence across restarts (R4):** in a container, keep state **outside** the ephemeral
> filesystem — use Postgres (`REGENTE_DB_DRIVER=postgres`) or a persistent volume for
> `/var/lib/regente`. The GitHub token should come from the **secrets provider**
> (`REGENTE_SECRET_GITHUB_TOKEN`), not only from the settings DB.
