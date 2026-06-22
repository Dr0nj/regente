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
sudo USER=$USER ./deploy/install-linux.sh
sudo $EDITOR /etc/regente/server.env      # REGENTE_TOKEN, REGENTE_DB, GitOps…
sudo systemctl restart regente-server
journalctl -u regente-server -f
```

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
(com `readinessProbe` **e** `livenessProbe` — um processo travado mas escutando é
reiniciado) + `cronjob.yaml` (gatilho externo no modo `-scheduler=external`).

## Config por env (resumo)

Todas as flags relevantes leem env, então a unit/manifesto não precisa de argumentos:

| Env | Flag | Default |
|---|---|---|
| `REGENTE_ADDR` | `-addr` | `:8080` |
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
