# Deploy do `regente-server` — supervisão (R1)

> **Um orquestrador crítico nunca roda como processo solto.** Ele assume que o
> processo *vai* morrer (crash, OOM, deploy, reboot) e **volta sozinho, sem perda**
> — o estado é durável (SQLite/Postgres) e a daily é idempotente. Estes artefatos
> dão a metade que faltava: **liveness do processo**.

| Arquivo | Para quê |
|---|---|
| `regente-server.service` | Unit systemd com `Restart=always` (Linux). |
| `server.env.example` | Config via env (token, DB, GitOps…). Copie para `/etc/regente/server.env`. |
| `install-linux.sh` | Instala a unit + binário + env, habilita e inicia. |
| `install-windows.ps1` | Tarefa Agendada (boot + reinício automático) no Windows. |

## Linux (systemd)

```bash
cd server && CGO_ENABLED=0 go build -o regente-server .
# opcional (V1): builda a UI pra servir junto, single-origin (UI+API+WS numa porta só)
(cd ../app && VITE_REGENTE_SERVER_URL=@origin npm ci && npm run build)
sudo ./deploy/install-linux.sh          # detecta ../app/dist e seta REGENTE_SPA_DIR sozinho
sudo $EDITOR /etc/regente/server.env      # REGENTE_TOKEN forte, REGENTE_DB, GitOps…
sudo systemctl restart regente-server
journalctl -u regente-server -f
```

> **UI junto (V1):** se `app/dist` existir (ou `SPA_DIR=` apontar pra uma UI buildada), o
> install copia pra `/var/lib/regente/app` e escreve `REGENTE_SPA_DIR` no `server.env` — o
> server serve o SPA na MESMA porta (o front resolve `@origin`, funciona atrás de qualquer
> domínio/túnel sem rebuildar). Sem `app/dist`, instala **só a API** (com aviso).
>
> **Sem toolchain no VPS (V2):** use o one-liner que baixa o bundle pronto (binário + UI):
> `curl -fsSL https://github.com/Dr0nj/regente/releases/latest/download/install.sh -o regente-install.sh && sudo bash regente-install.sh`.

Mate o processo (`sudo systemctl kill -s SIGKILL regente-server` ou `kill -9`) e
ele volta em ~5s — **sem perder estado** (instâncias, daily e eventos persistem).

## Windows (Tarefa Agendada)

```powershell
cd server; go build -o regente-server.exe .
.\deploy\install-windows.ps1 -Token <token-forte> `
  -GitSource https://github.com/Dr0nj/regente-workspace.git
```

## Container / Kubernetes / serverless

Ver [`../../deploy/`](../../deploy): `Dockerfile` (distroless) + `knative-service.yaml`
(`readinessProbe`→`/readyz`, `livenessProbe`→`/livez` — um processo travado mas escutando é
reiniciado) + `cronjob.yaml` (gatilho externo no modo `-scheduler=external`).

## DR / backup (R6)

`backup.sh` / `restore.sh` cobrem SQLite (`-backup` = VACUUM INTO online) e Postgres
(`pg_dump`/`pg_restore`). Runbook completo + PITR + checklist de config no restart (R4):
[`../../docs/dr-backup.md`](../../docs/dr-backup.md).

```sh
REGENTE_DB_DRIVER=postgres REGENTE_DB="$DSN" ./deploy/backup.sh /backups   # agende via cron/timer/CronJob
```

## Config por env (resumo)

Todas as flags relevantes leem env, então a unit/manifesto não precisa de argumentos:

| Env | Flag | Default |
|---|---|---|
| `REGENTE_ADDR` | `-addr` | `:8080` |
| `REGENTE_SPA_DIR` | `-spa-dir` | — (UI single-origin; o install seta quando acha `app/dist`) |
| `REGENTE_DB_DRIVER` | `-db-driver` | `sqlite` |
| `REGENTE_DB` | `-db` | `./regente.db` (caminho SQLite ou DSN Postgres) |
| `REGENTE_WORKSPACE` | `-workspace` | `./workspace` |
| `REGENTE_TOKEN` | `-api-token` | `dev-token` |
| `REGENTE_GIT_SOURCE` | `-git-source` | `…/regente-workspace.git` |
| `REGENTE_SECRET_GITHUB_TOKEN` | (secrets provider) | — (tira o PAT da DB em claro) |
| `REGENTE_ROLE` / `REGENTE_SCHEDULER` | `-role` / `-scheduler` | `all` / `internal` |

> **Persistência no restart (R4):** em container, ponha o estado **fora** do FS
> efêmero — use Postgres (`REGENTE_DB_DRIVER=postgres`) ou um volume persistente
> para `/var/lib/regente`. O token do GitHub deve vir do **secrets provider**
> (`REGENTE_SECRET_GITHUB_TOKEN`), não só da DB de settings.
