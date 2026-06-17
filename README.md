<div align="center">
  <img src="app/public/favicon-512.png" width="96" alt="Regente" />
  <h1>Regente</h1>
  <p><strong>Orquestrador de workflows Git-nativo, inspirado em Control-M.</strong></p>
  <p>
    <a href="#-conceito">Conceito</a> ·
    <a href="#-capacidades">Capacidades</a> ·
    <a href="#-arquitetura">Arquitetura</a> ·
    <a href="#-instalação">Instalação</a> ·
    <a href="#-server">Server</a> ·
    <a href="#-agent">Agent</a> ·
    <a href="#-frontend">Frontend</a> ·
    <a href="#-desenvolvimento">Dev</a> ·
    <a href="#-roadmap">Roadmap</a>
  </p>
</div>

---

Regente é um orquestrador de jobs onde **o repositório Git é a fonte da verdade**: cada
caixinha na tela vira um YAML commitado, e cada YAML no GitHub vira uma caixinha. A UX é a
de quem opera Control-M (folders, monitoring do dia, hold/rerun/force, find & update), e a
arquitetura nasce **local-first** (roda nas suas máquinas via agente) com caminho pronto
para serverless AWS.

Este é o **monorepo** do projeto inteiro. Cada componente tem seu próprio README
([`server/`](server/README.md) · [`agent/`](agent/README.md) · [`app/`](app/README.md)),
mas **este README reúne tudo** — é o compilado completo da plataforma.

| Pasta | Componente | Stack |
|---|---|---|
| [`server/`](server) | **regente-server** — daemon (API REST + WebSocket hub, scheduler, GitOps) | Go |
| [`agent/`](agent) | **regente-agent** — executor que roda nas suas máquinas (COMMAND/SCRIPT/HTTP) | Go |
| [`app/`](app) | **regente-web** — frontend (Monitoring, Design canvas) | React + TypeScript + Vite |

> A fonte da verdade dos jobs (YAMLs das *definitions*) mora num repositório **separado**
> de workspace GitOps — este repo é só o **código** da plataforma.

---

## 🧭 Conceito

Dois mundos separados, estilo Control-M:

- **Monitoring** — o que está rodando hoje. Resultado da *Daily*. Só consumo:
  hold / release / cancel / set-ok / rerun / force order, audit por instance, SLA.
- **Design** — onde as definitions são editadas. Você abre uma ou mais *folders*
  (clone Git efêmero por sessão), edita no canvas drag-and-drop e dá **Publish**
  (único caminho de escrita pro GitHub).

**A Daily** roda 1×/dia à meia-noite (BRT): lê o Git, decide o que roda hoje
(schedule + calendars + dependências + conditions) e materializa *instances*
**imutáveis** — uma mudança publicada no Design durante o dia só entra na próxima
daily ou via Force Order manual.

---

## ⚡ Capacidades

- 🟢 **Git-nativo (GitOps)** — Publish vira commit/PR; mudanças no GitHub voltam
  pra UI via webhook + polling. Deep-links job→YAML, instance→commit.
- 🟢 **Executores locais via agente** — `COMMAND` (shell), `SCRIPT` (.sh/.bat/.ps1)
  e `HTTP`, rodando na máquina onde o agente está (Windows ou Linux). Cada job
  pode mirar um agente específico ou ser roteado por capability. Tokens **por
  agente** + agente instalável como serviço (systemd / Tarefa Agendada).
- 🟢 **SSH agentless** — `SSH` roda comando remoto direto do server (sem agente
  no alvo), com stream de saída.
- 🟢 **Daily imutável** — instances congeladas no momento da ordem (mudança
  publicada no dia só entra na próxima daily ou via Force).
- 🟢 **Retry de execution** — re-tentativa automática em falha (respeita `retries`).
- 🟢 **Observabilidade** — `/metrics` em formato Prometheus.
- 🟢 **Alerting (Fase 8)** — regras configuráveis avaliadas ao fim de cada
  execução (falha / lentidão / retries / taxa de sucesso / falhas consecutivas);
  eventos com severidade, tela de alertas (sino + badge de não-reconhecidos,
  ack individual/todos) e disparo em tempo real (toast). Funciona em server mode
  (Postgres) e local (localStorage).
- 🟢 **Schedule estilo Control-M** — dias da semana/mês, N-ésimo dia útil, regras
  avançadas, janelas e execução cíclica; calendars include/exclude visuais.
- 🟢 **Dependências entre jobs** com condições (on-success/failure/complete/always).
- 🟢 **Engines de paridade** — calendars, resources/quotas, conditions, variáveis
  globais (interpolação), SLA e forecast/analytics.
- 🟢 **Token do GitHub pela UI** — configurável em runtime (Settings), persistido
  server-side, sem precisar subir o server com `GITHUB_TOKEN`.
- 🟢 **Enterprise readiness** — backend **Postgres** (além de SQLite), **leader
  election** (HA do scheduler), **secrets manager** plugável, **SSO/OIDC** opt-in.

---

## 🏗 Arquitetura

```
  ┌──────────────┐    REST + WebSocket    ┌──────────────────┐   git push/pull   ┌──────────┐
  │  app/         │ ─────────────────────▶ │  server/          │ ◀───────────────▶ │  GitHub   │
  │  (React)      │ ◀───────────────────── │  (Go, SQLite/PG)  │   (fonte da       │  (YAML)   │
  └──────────────┘    instance.changed     └──────────────────┘    verdade)        └──────────┘
                                                    ▲
                                                    │ WebSocket (agente disca pra fora — NAT-friendly)
                                                    │ dispatch ▼   ▲ result
                                            ┌──────────────────┐
                                            │  agent/           │  roda COMMAND/SCRIPT/HTTP
                                            │  (seu PC / EC2)   │  no Windows ou Linux
                                            └──────────────────┘
```

- **Local-first:** o agente faz conexão **outbound** pro server (atravessa NAT/firewall),
  recebe dispatch e devolve o resultado/stream.
- **State store plugável:** SQLite (default, pure-Go, zero infra) ou Postgres (HA/escala) —
  mesma base de código, via `-db-driver`.
- **HA:** com Postgres, vários servers usam *leader election* (advisory lock) — só o líder
  materializa a daily; todos servem API.

---

## 📦 Instalação

### Opção A — binários prontos (GitHub Releases)
Cada release publica binários do **server** e do **agent** para Linux, Windows e macOS.

```bash
# Linux/macOS — agent
curl -L https://github.com/Dr0nj/regente/releases/latest/download/regente-agent_linux_amd64 -o regente-agent
chmod +x regente-agent
./regente-agent -server ws://SEU_SERVER:8080/ws/agent -token <token> -caps COMMAND,SCRIPT,HTTP
```
```powershell
# Windows — agent como serviço (Tarefa Agendada)
irm https://github.com/Dr0nj/regente/releases/latest/download/install-windows.ps1 | iex
```

### Opção B — compilar do código (precisa de Go 1.25+)
```bash
git clone https://github.com/Dr0nj/regente.git && cd regente

# server
cd server && go build -o regente-server . && ./regente-server -api-token dev-token

# agent (na máquina onde os comandos devem rodar)
cd ../agent && go build -o regente-agent . && \
  ./regente-agent -server ws://localhost:8080/ws/agent -token dev-token -caps COMMAND,SCRIPT,HTTP
```

> `go install github.com/Dr0nj/regente/agent@latest` (instalação direta via Go) fica
> disponível quando os module paths forem alinhados ao monorepo — ver issues/roadmap.

---

## 🖥 Server

> Pasta [`server/`](server) · daemon Go. Substitui o scheduler que antes rodava no browser.

**Componentes:**
- HTTP REST API (chi) em `:8080`
- WebSocket hub para web (eventos live) e agents (dispatch)
- Scheduler core rodando em goroutine (daily + tick)
- Storage: YAML em `workspace/definitions/<team>/<id>.yaml`, opcionalmente commitado via git
- State runtime: SQLite (pure-Go, sem CGO) ou Postgres (`-db-driver`)

### Build & run (dev)

```bash
cd server
go mod tidy
go build -o regente-server .
./regente-server -addr :8080 -workspace ./workspace -db ./regente.db -api-token dev-token
```

### Flags

| Flag            | Default                       | Descrição                               |
|-----------------|-------------------------------|-----------------------------------------|
| `-addr`         | `:8080`                       | HTTP listen                             |
| `-workspace`    | `./workspace`                 | Path com `definitions/`                 |
| `-db`           | `./regente.db`                | Caminho do SQLite (ou DSN do Postgres)  |
| `-db-driver`    | `sqlite`                      | `sqlite` \| `postgres`                  |
| `-tick-ms`      | `2000`                        | Scheduler tick                          |
| `-git-commit`   | `false`                       | Commita saves (workspace deve ser repo) |
| `-api-token`    | `dev-token` / `REGENTE_TOKEN` | Bearer token web + agent                |
| `-auth-mode`    | `local`                       | `local` \| `oidc` (SSO opt-in)          |

### API resumo

Todas as rotas `/api/*` exigem `Authorization: Bearer <token>`.

| Método | Path                                  | Descrição                      |
|--------|---------------------------------------|--------------------------------|
| GET    | `/health`                             | healthcheck                    |
| GET    | `/metrics`                            | métricas Prometheus            |
| GET    | `/api/definitions`                    | lista YAMLs de `definitions/`  |
| POST   | `/api/definitions`                    | cria/atualiza YAML             |
| DELETE | `/api/definitions/{team}/{id}`        | remove YAML                    |
| GET    | `/api/folders`                        | lista subdirs de `definitions/`|
| POST   | `/api/folders`                        | cria subdir                    |
| GET    | `/api/instances?date=YYYY-MM-DD`      | instances do dia               |
| POST   | `/api/instances/{id}/hold`            | Hold                           |
| POST   | `/api/instances/{id}/release`         | Release                        |
| POST   | `/api/instances/{id}/cancel`          | Cancel                         |
| POST   | `/api/instances/{id}/rerun`           | Rerun                          |
| POST   | `/api/daily/run`                      | força a daily do dia           |
| POST   | `/api/definitions/{id}/force`         | Order Force (Control-M)        |
| POST   | `/api/scheduler/tick`                 | dispara um ciclo (cron externo, `-scheduler=external`) |
| GET    | `/api/alerts`                         | eventos de alerta              |
| POST   | `/api/alerts/{id}/ack` · `/api/alerts/ack-all` | reconhece alerta(s)   |
| GET    | `/api/alerts/rules` · POST `/api/alerts/rules/{id}/toggle` | regras de alerta |
| GET    | `/api/agents`                         | agents online                  |
| GET    | `/ws/web?token=...`                   | WS para web (events)           |
| GET    | `/ws/agent?token=...&id=...&caps=...` | WS para agent (WebSocket)      |
| GET    | `/api/agent/poll?id=...&caps=...`     | dispatch via long-poll (`-transport=http`) |
| POST   | `/api/agent/result` · `/api/agent/output` | resultado + stream do agente |

---

## 🤖 Agent

> Pasta [`agent/`](agent) · executor Go. Rode **um agente em cada máquina** que deve
> executar jobs (seu laptop, um server on-prem, uma VM/EC2, etc.).

Conecta ao `regente-server` via WebSocket **outbound** — sem abrir portas na máquina
do agente (atravessa NAT/firewall). Mesmo modelo de runner do GitHub Actions / GitLab /
Control-M Agent. Executa jobs localmente, devolve o resultado e **streama stdout/stderr
em tempo real** (aparece no detalhe da instance).

### Executores

| jobType | O que faz | Params (actionConfig) |
|---|---|---|
| `COMMAND` | Comando no shell do SO (`powershell -Command` no Windows, `sh -c` no Linux) | `command`, `cwd?` |
| `SCRIPT` | Executa um script; interpretador pela extensão (`.ps1`/`.bat`/`.sh`) | `scriptPath`, `args?`, `cwd?` |
| `HTTP` | Chamada REST com validação de status | `method`, `url`, `headers?`, `body?`, `expectStatus?` |
| `WASM` | Roda um módulo WebAssembly WASI sandboxed via [wazero](https://wazero.dev) (pure-Go, sem CGO) | `wasmPath` \| `wasmUrl`, `args?`, `stdin?` |

> `SSH` (comando remoto agentless) **não** usa o agente — roda no próprio server.

**Transporte** (`-transport`): `ws` (WebSocket, default) ou `http` (long-poll
serverless-friendly — control plane stateless/scale-to-zero). Ver
[`docs/arquitetura-futuro.md`](docs/arquitetura-futuro.md).

### Build & rodar (foreground)

```bash
cd agent
go build -o regente-agent .       # (.exe no Windows)
./regente-agent \
  -server ws://SEU-SERVER:8080/ws/agent \
  -token  rgta_...        # Settings → Agentes → Criar token \
  -id     meu-host \
  -caps   COMMAND,SCRIPT,HTTP
```

Token: gere um **token por agente** na UI (Settings → Agentes → Criar token). O
`dev-token` legado também funciona em dev. Sem token válido, o handshake é negado.

### Instalar como serviço

**Linux (systemd):**
```bash
sudo SERVER=ws://host:8080/ws/agent TOKEN=rgta_xxx ID=$(hostname) \
     CAPS=COMMAND,SCRIPT,HTTP USER=$USER ./deploy/install-linux.sh
journalctl -u regente-agent -f     # logs
```

**Windows (Tarefa Agendada — roda no boot, reinicia sozinho), PowerShell como Administrador:**
```powershell
.\deploy\install-windows.ps1 -Server ws://host:8080/ws/agent -Token rgta_xxx `
                             -Id $env:COMPUTERNAME -Caps COMMAND,SCRIPT,HTTP
Get-ScheduledTask RegenteAgent     # status
```

### Dispatch: como o server escolhe o agente

- Job com **agente específico** (campo "Agente (onde roda)" no JobConfigDrawer) →
  vai direto pra ele.
- Senão → o server escolhe um agente online cuja **capability** bate com o jobType
  (`PickAgent`). Por isso `-caps` deve incluir os jobTypes que o agente aceita.

### Protocolo WebSocket

```jsonc
// server → agent
{ "event":"dispatch", "instanceId":"...", "jobType":"COMMAND", "params":{...}, "timeout":300 }
// agent → server (streaming durante a execução)
{ "event":"output", "instanceId":"...", "chunk":"linha de stdout/stderr" }
// agent → server (final)
{ "event":"result", "instanceId":"...", "exitCode":0, "output":"saída completa" }
// agent → server (a cada 30s)
{ "event":"heartbeat" }
```

---

## 🎨 Frontend

> Pasta [`app/`](app) · React + TypeScript + Vite. Cliente HTTP/WebSocket do `regente-server`.

| Camada | Tecnologia |
|---|---|
| Frontend (este repo) | React + TypeScript + Vite, [@xyflow/react](https://reactflow.dev) (canvas), ícones lucide |
| Backend | Go (`regente-server`) — GitOps + SQLite/Postgres, WebSocket hub |
| Executor | Go (`regente-agent`) — conexão outbound (WS ou HTTP long-poll), roda COMMAND/SCRIPT/HTTP/SSH/WASM |
| Fonte da verdade | Repositório GitHub (YAML em `definitions/<folder>/<id>.yaml`) |

### Rodando o frontend

Pré-requisitos: Node 18+ e o `regente-server` rodando.

```bash
cd app
npm install
cp .env.example .env        # configure VITE_REGENTE_SERVER_URL
npm run dev                 # http://localhost:5173
```

Variáveis (`.env`):

```bash
VITE_REGENTE_SERVER_URL=http://localhost:8080   # vazio = modo local (localStorage)
VITE_REGENTE_TOKEN=dev-token
```

Login padrão de dev: `admin` / `admin`.

### Estrutura (frontend)

```
src/
├── v2/                  # UI atual (Monitoring, Design, drawers, dialogs)
│   ├── V2Preview.tsx    # shell principal (topbar, canvas, modos)
│   ├── JobConfigDrawer  # edição de job (Geral/Schedule/Calendars/Action/Deps)
│   ├── ScheduleEditor   # scheduler visual estilo Control-M
│   ├── AlertsPanel.tsx  # tela de alertas (eventos + regras) — Fase 8
│   └── ...
├── lib/                 # clientes de API + modelo + adapters
│   ├── server-client.ts # REST + WS
│   ├── git-api.ts       # status, token, cleanup, deep-links
│   ├── agents-api.ts    # agentes online
│   ├── alerts-api.ts    # alertas (facade dual-mode server/local)
│   └── adapters/        # ports & adapters (storage/scheduler/executor)
└── main.tsx
```

---

## 🛠 Desenvolvimento

Três terminais — server, agent e frontend:

```bash
# 1. server
cd server && go run . -api-token dev-token

# 2. agent (terminal separado)
cd agent && go run . -server ws://localhost:8080/ws/agent -token dev-token -id meu-pc -caps COMMAND,SCRIPT,HTTP

# 3. frontend (terminal separado)
cd app && npm install && cp .env.example .env && npm run dev    # http://localhost:5173
```

Login dev: `admin` / `admin`. Testes: `go test ./...` em `server/` e `agent/`.

---

## 🗺 Roadmap

- [x] **GitOps** — Publish, webhook, drift, deep-links, PAT seguro + token via UI
- [x] **Paridade Control-M** — calendars, resources, conditions, variáveis, SLA, forecast
- [x] **Daily imutável** — instances congeladas no momento da ordem
- [x] **Executores locais** — agente COMMAND/SCRIPT/HTTP + targeting por agente
- [x] Stream de stdout/stderr no detalhe da instance
- [x] Auth por agente (token dedicado) + instalação como serviço
- [x] SSH agentless (comando remoto sem agente no alvo)
- [x] Retry de execution + `/metrics` (Prometheus) + webhook secret por UI

### Enterprise readiness (em andamento — solidez > velocidade)
- [x] **Escala — backend Postgres** (além de SQLite): state store plugável por dialeto,
  flag `-db-driver sqlite|postgres`, migrations versionadas. *(falta: DynamoDB, nós stateless, 10k+ jobs/dia)*
- [x] **HA — leader election do scheduler**: só o líder materializa a daily/dispatch
  (`pg_advisory_lock` no Postgres; nó único no SQLite). *(falta: hub WebSocket distribuído, DR/backup)*
- [x] **Segurança — secrets manager** (provider plugável, tira PAT/secrets do banco em claro;
  default env+arquivo, Vault/AWS pluggável) **+ SSO/OIDC** (Authorization Code, opt-in via `-auth-mode`;
  login local segue default). *(falta: SSO ponta-a-ponta com IdP + frontend, RBAC/ACL completo, mTLS dos agentes, audit→SIEM)*
- [ ] **Operação**: tracing (OpenTelemetry), alert routing, upgrades zero-downtime, multi-ambiente, quotas
- [ ] **Qualidade**: testes E2E + carga + chaos, SLOs
- [ ] Reconciler de drift explícito (state machine)

### Serverless portátil (sem lock-in) — ver [`docs/arquitetura-futuro.md`](docs/arquitetura-futuro.md)
Estratégia: **container scale-to-zero + estado/gatilho externalizados**, não FaaS.
A mesma imagem OCI roda em Knative/Cloud Run/Fly/App Runner; estado em Postgres
(qualquer compatível), gatilho via cron externo. Nada amarra a AWS.
- [x] **Fase 1 — gatilho externalizado**: `Tick()` idempotente, flags
  `-scheduler=internal|external` e `-role=all|api|scheduler`, endpoint
  `POST /api/scheduler/tick`, artefatos em [`deploy/`](deploy) (Dockerfile +
  Knative + CronJob). Scale-to-zero do control plane.
- [x] **Fase 2 — transporte plugável**: interface `Bus` desacopla o scheduler do
  WebSocket hub **+ transporte HTTP long-poll** (`-transport=http` no agent;
  `/api/agent/poll|result|output`) para control plane stateless. *(projetado:
  adapter NATS, hub distribuído)*
- [x] **Fase 3 — executores como plugins**: roteamento por capability é o seam
  **+ executor WASM** (`jobType: WASM` via wazero, pure-Go/sem CGO, sandbox WASI).
  *(projetado: adapters AWS/GCP/k8s por capability, durable execution opt-in,
  Postgres-como-fila)*

---

<div align="center">
<sub>Projeto pessoal de portfólio. UX inspirada na operação do Control-M; nenhuma relação com a BMC.</sub>
</div>
