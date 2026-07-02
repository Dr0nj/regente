<div align="center">
  <img src="app/public/logo-r.png" width="92" alt="Regente" />
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
  É um **snapshot imutável**: a folder de cada instância é congelada quando a daily
  foi schedulada — apagar ou mover o job no Design não reescreve a daily corrente.
- **Design** — onde as definitions são editadas. Pelo botão **FOLDERS** você cria,
  abre/fecha (multi-select) e gerencia *folders* — abrir uma folder monta um clone
  Git efêmero por sessão; você edita no canvas drag-and-drop e dá **Publish**
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
- 🟢 **Ciclo de vida da daily (carry-over Control-M)** — na virada, a ordem que
  ainda está aberta **não some**: RUNNING e HELD atravessam sempre; NOTOK
  não-tratado persiste +1 diária (ou N via `schedule.keepActive`); OK/CANCELLED
  encerram. A ordem **avança o order_date** mantendo id/status/histórico (não
  duplica) e exibe a origem (badge ↩). Idempotente.
- 🟢 **Explain — "por que esse job não rodou?"** (diferencial, sem IA): por instância,
  expõe o gating que o scheduler já computa — janela, dependências (qual upstream/condição),
  conditions faltando, recurso indisponível (quer/uso/capacidade). Construído como **fonte
  única**: o mesmo avaliador decide o dispatch e explica, então condição nova é absorvida sem
  manutenção dupla. `GET /api/instances/{id}/explain` + painel no drawer.
- 🟢 **Diff de Daily** (diferencial Git-native): compara duas diárias (hoje vs a anterior,
  ou `?from&to&folder`) → jobs **adicionados / removidos / alterados**, com diff **por-campo**
  (schedule, deps, recursos, def). Lê os snapshots congelados (`commit_sha` + `definition_snapshot`)
  → exato e barato, sem reprocessar Git. `GET /api/daily/diff` + modal "Diff" na topbar.
- 🟢 **Blast Radius** (diferencial): "se eu **cancelar/segurar** este job agora, qual o impacto?"
  → jobs downstream que deixam de rodar em cascata, SLAs em risco, folders afetadas e profundidade.
  Análise de uma AÇÃO (não do grafo estático): BFS no grafo de deps, só pelo raio. `GET
  /api/instances/{id}/blast-radius` + painel "⚠ Impacto" no drawer.
- 🟢 **Dry Run** (diferencial): **simula a daily de qualquer data sem materializar** — quem **roda**,
  quem **espera** (depois de quem) e quem **nunca dispara** (e por quê: fora do calendário, dependência
  que não roda, condition órfã…), com cascata transitiva. Reusa a mesma decisão de agendamento do RunDaily.
  `GET /api/daily/dryrun?date=` + modal "Dry Run" (com seletor de data).
- 🟢 **Agent-native (MCP)** — servidor [MCP](https://modelcontextprotocol.io) (`server/cmd/mcp`)
  que expõe os diferenciais como _tools_: você **opera o Regente conversando** com o Claude
  (*"o que falhou em pagamentos hoje e por quê?"*). Read-only por padrão; writes (`rerun`/`set_ok`)
  atrás de `-allow-writes` + aprovação do cliente. Pure-Go stdlib, fachada sobre a REST. Ver
  [`docs/mcp.md`](docs/mcp.md).
- 🟢 **Tela de Agentes** (Settings → Agentes) — frota **consolidada** (online + offline com last-seen) +
  contador "N de M online"; clique num agente abre um **modal de detalhe** (SO/arch, host, versão, **uptime**,
  conectado há, 1ª vez visto, último sinal, capabilities). O agente reporta a metadata no handshake; gestão
  de tokens junto. **Ping ativo** (round-trip ping/pong, latência) por agente e "ping todos". `GET /api/agents`
  · `POST /api/agents/{id}/ping`.
- 🟢 **Retry de execution** — re-tentativa automática em falha (respeita `retries`).
- 🟢 **Observabilidade** — `/metrics` em formato Prometheus.
- 🟢 **Alerting (Fase 8)** — regras configuráveis avaliadas ao fim de cada
  execução (falha / lentidão / retries / taxa de sucesso / falhas consecutivas);
  tela de alertas (sino + badge, ack individual/todos) e toast em tempo real.
  **Routing multi-canal por regra**: Slack · webhook genérico · e-mail (SMTP) ·
  PagerDuty Events API. **Cooldown por (regra×job)** — uma rajada de jobs distintos
  nunca perde alertas (só re-disparos do mesmo job são agrupados). **Ciclo de vida
  do alerta** — rerun e Set OK do job marcam o alerta como *tratado* (rerun / set ok)
  automaticamente; re-falha gera um alerta novo. Server mode (Postgres) e local.
- 🟢 **Schedule estilo Control-M** — frequência (diário / dias da semana / dias do mês),
  janelas e execução cíclica. Os **calendários** vivem na própria aba Schedule e trabalham
  junto com as regras como **include/exclude** (ex.: negar "dias úteis" + todos os dias =
  roda todo dia, exceto dia útil), com **tradução** do que cada calendário faz e um
  **preview de calendário REAL** no rodapé (estilo Control-M "View Scheduling": 12
  mini-meses + seletor de ano; os dias **destacados** são exatamente os que o job vai
  rodar, calculados pelo backend com a **mesma regra da daily** — `POST /api/schedule/preview`).
  ("Dia útil"/regra avançada saíram da frequência: dia útil depende do calendário de feriados de cada lugar.)
- 🟢 **Dependências entre jobs** com condições (on-success/failure/complete/always).
- 🟢 **Engines de paridade** — calendars, resources/quotas, conditions, variáveis
  globais (interpolação), SLA e forecast/analytics.
- 🟢 **Token do GitHub pela UI** — configurável em runtime (Settings), persistido
  server-side, sem precisar subir o server com `GITHUB_TOKEN`.
- 🟢 **Enterprise readiness** — backend **Postgres** (além de SQLite), **leader
  election** (HA do scheduler), **secrets manager** plugável, **SSO/OIDC** opt-in.
- 🟢 **Identidade visual** — logotipo **R** próprio (branco, vetorial, fundo
  transparente), topbar **premium flutuante** (cantos arredondados, segmented
  `‹ DESIGN · MONITORING ›` com chevrons e destaque **neon** que segue o tema) e cluster
  de ações à direita: **alertas** (sino), **configurações/conta** (engrenagem → menu),
  **tela cheia** (monitor) e **avatar**. Tela de **login** (paleta dark blue neon),
  **sidebars flutuantes** e **janela de info do job dockada** (mesmo estilo do ACTIVE JOBS),
  com realce **neon** no selecionado. **Diálogos padronizados** (✕ + Cancelar/Salvar via
  classes compartilhadas; "Control-M Panel" → **Control Panel**). O **pan fica travado** nos
  dois modos, entrando **ancorado** um pouco abaixo do topo; no **Design** é **limitado** à
  caixa dos jobs da folder (só puxa pros lados quando passam da tela, sem "se perder"). Há um
  **minimap de navegação** opcional (protótipo, Settings → Geral; ponto por job, clique navega).
- 🟢 **Temas** — aparência configurável em Settings → aba **Temas**. **13 temas**: **Escuro**
  (padrão), **Verde Amarelo**, **Amarelo Ouro**, **Verde Mata**, **Azul Neon**, **Azul Escuro**,
  **Rosa**, **Violeta**, **Vermelho**, **Laranja**, **Cinza** (escala), **Bege Escuro** e **Marrom**.
  Cada card mostra um **swatch** com as cores do tema.
  Aplica na hora e persiste no navegador (`localStorage`). Implementação: cada tema
  sobrescreve os design tokens `--v2-*` via `:root[data-theme="<id>"]` em
  `app/src/v2/tokens.css` (catálogo em `app/src/lib/theme.ts`). Toda a UI consome esses
  tokens — inclusive o painel **Control-M Parity**. Diálogos de config/senha/tema ganham
  **borda neon na cor do tema** (`.v2-neon-card`).

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
| `-bus`          | `hub`                         | `hub` (local) \| `nats` (hub distribuído multi-nó, R5) |

> **Produção:** rode o server **supervisionado** com restart automático — ver
> [`server/deploy/`](server/deploy) (systemd `Restart=always` / Windows Service / `livenessProbe`).
> Todas as flags acima leem variáveis de ambiente (`REGENTE_*`), então a unit/manifesto
> dispensa argumentos. **Multi-nó (R5):** `-bus=nats` faz fan-out de eventos web e roteia o
> dispatch ao nó dono do agent — o failover do líder não estranda agentes.

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
| GET    | `/api/instances/{id}/explain`         | por que (não) rodou: gating estruturado |
| GET    | `/api/instances/{id}/blast-radius`    | impacto de cancelar/segurar (downstream/SLA) |
| GET    | `/api/daily/diff?from&to&folder`      | diff entre duas diárias (+/−/alterados) |
| GET    | `/api/daily/dryrun?date`              | simula daily futura (roda/espera/nunca) |
| POST   | `/api/daily/run`                      | força a daily do dia           |
| POST   | `/api/definitions/{id}/force`         | Order Force (Control-M)        |
| POST   | `/api/scheduler/tick`                 | dispara um ciclo (cron externo, `-scheduler=external`) |
| GET    | `/api/alerts`                         | eventos de alerta (com `resolution`) |
| POST   | `/api/alerts/{id}/ack` · `/api/alerts/ack-all` | reconhece alerta(s)   |
| GET    | `/api/alerts/rules` · POST `/api/alerts/rules/{id}/toggle` | regras de alerta |
| PUT    | `/api/alerts/rules/{id}/channels` · `.../cooldown` | routing por-regra · cooldown |
| GET    | `/api/agents`                         | frota consolidada (online + offline) |
| POST   | `/api/agents/{id}/ping`               | ping ativo (round-trip, latência) |
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
| `K8S_JOB` | Cria um **Kubernetes Job** via API REST e aguarda concluir (adapter de nuvem por capability `K8S`) | `image`, `command?`, `namespace?`, `apiServer?`, `token?` |

> `SSH` (comando remoto agentless) **não** usa o agente — roda no próprio server.
> `K8S_JOB` exige um agente que anuncie `-caps K8S` (com acesso à API do cluster).

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

`VITE_REGENTE_SERVER_URL=@origin` = **same-origin**: o server Go serve o SPA na mesma
porta (`-spa-dir`), então a UI usa `window.location.origin` em runtime — é o modo usado
pra hospedar a demo atrás de um túnel (a URL pode mudar sem rebuildar o front).

Login padrão de dev: `admin` / `admin`.

### Hospedar pra outras pessoas testarem (demo com link público)

Pra convidar amigos a testar a UI (criar e executar jobs) num link https, o server serve
o SPA single-origin (`-spa-dir`) e um agente roda isolado em Docker. Passo-a-passo,
script (`host-demo.ps1`) e notas de segurança em [`deploy/demo/README.md`](deploy/demo/README.md).

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

> Versão visual e consolidada (progresso por trilha): [`docs/roadmap.md`](docs/roadmap.md).

- [x] **GitOps** — Publish, webhook, drift, deep-links, PAT seguro + token via UI
- [x] **Paridade Control-M** — calendars, resources, conditions, variáveis, SLA, forecast
- [x] **Daily imutável** — instances congeladas no momento da ordem
- [x] **Executores locais** — agente COMMAND/SCRIPT/HTTP + targeting por agente
- [x] Stream de stdout/stderr no detalhe da instance
- [x] Auth por agente (token dedicado) + instalação como serviço
- [x] SSH agentless (comando remoto sem agente no alvo)
- [x] Retry de execution + `/metrics` (Prometheus) + webhook secret por UI
- [x] **Alerting (Fase 8)** — regras, tela (sino + badge + ack), toast, **routing multi-canal por regra**
  (Slack/webhook/SMTP/PagerDuty), **cooldown por (regra×job)** (rajada não perde erro) e **ciclo de vida**
  (rerun/Set OK marcam o alerta como tratado)
- [x] **Temas** — 13 temas (Escuro padrão + Verde Amarelo/Amarelo Ouro/Verde Mata/Azul Neon/Azul Escuro/
  Rosa/Violeta/Vermelho/Laranja/Cinza/Bege Escuro/Marrom) em Settings → aba Temas, com swatch de cores;
  tokens aplicados em toda a UI (Control-M panel incluso) + borda neon nos diálogos de config

### Enterprise readiness ✅ (100% — solidez > velocidade; escala 100k–1M end-to-end validada)
- [x] **Escala — backend Postgres** (além de SQLite): state store plugável por dialeto,
  flag `-db-driver sqlite|postgres`, migrations versionadas. **Stateless** (estado durável externo; só o líder
  agenda). **Write-path escala a 1M jobs/dia** (materialização em lote → **1M em 17s**, era ~15min). **Read-path
  paginado/filtrado** (`/api/instances/page` por cursor + `/api/instances/summary` agregado + `team`
  denormalizado na instance + RBAC por conjunto → **summary 51ms / page 18ms @100k** vs 491ms baixando o dia
  inteiro). **UI por ViewPoint server-driven** (`ScaleMonitor`: dashboard + folders + lista virtualizada)
  **validada AO VIVO com 1.000.000 de jobs** — folder aberta em ~39ms, sem nunca baixar o dia inteiro.
- [x] **HA — leader election do scheduler**: só o líder materializa a daily/dispatch
  (`pg_advisory_lock` no Postgres; nó único no SQLite). **+ hub distribuído via NATS (R5, opt-in
  `-bus=nats`)** — fan-out de eventos web + dispatch roteado ao nó dono do agent. **2-nós validado em
  Postgres real (failover ~4s); DR/backup = R6 ✓.**
- [x] **Resiliência operacional COMPLETA (R1–R7)**: server **supervisionado** (`regente-server.service`
  systemd `Restart=always` + Windows Service) + **panic-recovery** no scheduler
  (um job não derruba o cérebro) + **watchdog de tick** (`/livez` + gauge em `/metrics`) +
  **readiness real** (`/readyz`: ping DB = gate; líder + tick + último daily informativos;
  `readinessProbe` do Knative aponta aqui) + **DR/backup** (`-backup` online via `VACUUM INTO` no SQLite;
  `pg_dump`/PITR no Postgres; scripts + runbook [`docs/dr-backup.md`](docs/dr-backup.md)) + **config durável**
  no restart (settings no DB) + **auto-SLO do control plane** (`-selfmon`: alerta tick parado / DB inacessível /
  leader flapping / frota de agentes ↓ pelos mesmos canais). **Chaos/HA 2-nós validado em Postgres real** (failover ~4s).
- [x] **Segurança — secrets manager** (provider plugável, tira PAT/secrets do banco em claro;
  default env+arquivo, Vault/AWS pluggável) **+ SSO/OIDC** (opt-in `-auth-mode`) **+ RBAC/ACL** (roles
  admin/operator/viewer + ACL por folder) **+ mTLS opt-in** (`-tls-client-ca`, verifica cert de cliente)
  **+ audit→SIEM** (eventos JSON em stderr + POST `-audit-siem-url`). **SSO ponta-a-ponta VALIDADO com Keycloak real**
  (Authorization Code completo → user provisionado → sessão federada autentica a API; 2026-06-23).
- [x] **Operação — tracing (OpenTelemetry)** (OTLP/HTTP opt-in, `-otel-endpoint`) **+ upgrades zero-downtime**
  (rolling via leader election — `rolling-upgrade.sh` validado: novo sobe follower → drena o líder → assume ~4s,
  API nunca cai) **+ multi-ambiente** (deployment por branch+DB+`env_label`; `regente_env_info` no /metrics)
  **+ quotas** (F15, e o tracker se reconstrói do RUNNING após failover). Runbook: [`docs/operacao.md`](docs/operacao.md).
- [x] **Adapters de nuvem por capability**: k8s Jobs (`K8S_JOB`) **VALIDADO em cluster real** (kind v1.36 — Job criado,
  kubelet rodou, succeeded/failed lidos de volta) + **AWS Lambda** (`LAMBDA`, SigV4 stdlib) + **GCP Cloud Run Jobs**
  (`GCP_RUN`) — AWS/GCP com código + e2e em API mock; o mesmo seam por capability já está provado real no k8s,
  então validação em conta paga fica fora de escopo por decisão (sem cartão).
- [x] **Qualidade**: testes **E2E HTTP** + **chaos/HA** (`chaos-ha.sh` + validado) + **SLOs** ([`docs/slos.md`](docs/slos.md))
  + **carga REAL** (`hey` contra o binário, TCP real: /readyz **11.6k req/s** com 100 conexões, p99 34ms, 0 erros)
  + **reconciler de drift** (`-drift-reconcile-sec`: só o líder; alerta o drift GitOps pelos canais do R7, ou auto-sync).

### Aprofundamento Control-M (testar a fundo + aprimorar)
- [ ] **Calendários complexos**: validar que o job entra na daily exatamente quando deve — 1º dia útil
  do mês, só segundas, 1º dia útil que NÃO é segunda, N-ésimo dia útil, regras avançadas, include/exclude,
  feriados, meses específicos. Cobrir todas as combinações; corrigir o gating onde divergir.
- [ ] **Controle de recursos**: jobs que não podem concorrer (lock exclusivo), máximo de jobs simultâneos
  por host/pool, quantitative (N slots), fila quando esgota e liberação correta.
- [x] **Actions / On-Do do job** — motor backend ✅ (2026-06-29): regras "On `<gatilho>` Do `<ação>`" por job nas
  **3 dimensões** — **(a) por nº de tentativa** (`on: attempt` — dispara na N-ésima tentativa que falhou, cobrindo a
  final; complementa o retry automático `retries`); **(b) por resultado** (`on: result` OK/NOTOK, transição terminal);
  **(c) por tempo de execução** (`on: runtime` — RUNNING há >N min, shouts escalonáveis 30/40/60min). **4 ações**
  reusando os substratos: `notify` (Slack/webhook/e-mail/PagerDuty), `set-condition` (destrava sucessores), `run-job`
  (Force Order de outro job), `set-ok` (auto-heal NOTOK→OK). Idempotente — cada regra dispara 1× por instance (ledger
  durável `action_fires`, migration v7); decisor puro testável. 14 testes + validado ao vivo no binário (`notify`→
  `/api/alerts`, `set-condition`→`/api/conditions`). **UI ✅** (2026-06-30): aba **"On/Do"** no JobConfigDrawer
  (`OnDoEditor`) — regras On‹gatilho›Do‹ação› com add/remove, campos contextuais por tipo, chips de canais e
  **tradução em linguagem natural** por regra. `ActionRule` no modelo TS + map no `ServerApiAdapter`; backend já
  fazia round-trip nativo (handler decodifica `domain.JobDefinition` → YAML). Round-trip provado
  (`TestFileStore_ActionsRoundTrip`).
- [ ] **Job FILE_WATCH**: espera a chegada de arquivo (path/glob, polling/evento, tamanho estável) antes
  de concluir e disparar o sucessor. Novo jobType + capability.
- [ ] **Forecast**: testar a previsão de ≥ 1 semana à frente (quais jobs rodam por dia) contra o gating real.
- [x] **Ciclo de vida na daily (Keep Active / carry-over)** ✅ (2026-06-24): **RUNNING na virada não some**
  (segue na daily pra tracking até terminar); `keepActive=N` (job que não rodou OK sobrevive N diárias);
  **default** NOTOK não tratado persiste +1 diária; **HOLD** atravessa as diárias enquanto em hold. A ordem
  AVANÇA o order_date (mesmo id/status/snapshot/eventos) e SUBSTITUI a fresca do dia (sem duplicar); migration
  v5 (`carry_budget`/`carried_from`/`carried_at`); badge ↩ no ViewPoint; editável no Design (`keepActive`).
  Idempotente. 11 testes + validado ao vivo no binário (40 ontem → 15 migraram, 25 ficaram, 1 carry-over).
- [ ] **CONFIRM**: job que precisa de ação manual (Confirm) para sair do estado e prosseguir (semântica Control-M).
- [ ] **Job tipo DATABASE**: plugin com conectores (JDBC e outros); corpo = procedure ou SQL numa telinha
  PL/SQL amigável (editor). Novo jobType + capability.
- [ ] **Sistema de variáveis completo** (estilo Control-M `%%`): **globais de runtime** (um job atribui,
  outro lê — passagem entre jobs), **locais por job**, **nativas/sistema** (`%DataAtual`, `%DiaAtual`,
  `%AnoAtualYYYY`, último dia do mês…) e **cálculo de datas com template** — aritmética tipo `%DiaAtual+3`
  resolvendo p/ data numérica ciente de dia útil/feriado; interpolação em qualquer campo + inspetor por instância.
- [ ] **ViewPoint (Monitoring)**: viewpoints salvos — mostrar só certas folders (não todas) no Monitoring.
- [ ] **Dashboards prontos (ViewPoint de dashboard)**: painel por folder(s) ou do ambiente inteiro com
  gráficos (pizza e outros) e estatísticas em tempo real — jobs em execução/hold/waiting/confirm, totais,
  OK/failed, nomes dos últimos executados e dos últimos com erro, métricas por folder. (Analytics base já no Control Panel.)
- [ ] **Mass Update / Find & Update (Design)**: alteração em massa nas folders abertas por regex/critério de
  campo — buscar e substituir/adicionar em N jobs (descrição vazia, add action/evento, find-replace em
  qualquer campo, tags/conditions em lote), com preview/undo. (bulk básico já existe via `/api/bulk`.)
- [ ] **Janela de info do job (drawer)**: deixar mais friendly — ações claras, output/log legível, layout.
- [x] **Layout de jobs — grade pros soltos, fluxo pros dependentes** ✅ (2026-06-25): jobs SEM dependência
  viram uma **grade** (N colunas, default 10; 11º quebra pra linha 2; ao passar de N linhas alarga colunas em vez
  de crescer pra baixo). DEPENDENTES seguem o fluxo top-down (dagre). Por folder. `columns`/`máx. linhas`
  configuráveis em **Settings › Geral › Visualização**; botão **Organizar** (re-enquadra). Vale Monitoring + Design.
- [x] **Fixes de Folders/Design (UX)** ✅ (2026-07-01): botão bulk da tela Folders mostra **"Abrir"**
  (não "Abrir no Design", redundante já que FOLDERS só existe em modo Design) e ao abrir folder(s)
  selecionadas o modal **fecha e cai direto no canvas**; corrigido bug feio de "Nenhuma definition"
  aparecer sobreposto ao 1º job arrastado numa folder vazia (overlay checava `hasDefs` global em vez
  do rascunho não-salvo já visível no canvas); JobConfigDrawer — "Label" virou **"Job Name"** e o
  campo **ID some da UI** (segue existindo internamente: nome do YAML/chave de dependências);
  **os 4 drawers docados foram padronizados no visual flutuante arredondado** (margens +
  `borderRadius:16` + boxShadow) — o Edit Job (`JobConfigDrawer`) e o detalhe do Monitoring
  (`InstanceDetailsDrawer`) colavam nas bordas sem raio; agora batem com as sidebars de palette/monitor
  (`ScaleMonitor` fica de fora por ser view full-screen); o botão **"Abrir"** da action bar virou
  **primary em destaque** (maior, preenchido no accent, pílula com glow) por ser a ação principal
  quando há folder selecionada; corrigido o **job que "sumia" ao ser criado** (com zoom/pan o nó novo
  podia nascer fora da tela — não era perda de dado, só a câmera não reenquadrava; agora dá `fitView`
  ao salvar job novo); e a **aba Folders da sidebar esquerda lista os jobs de cada folder como linhas
  clicáveis** que navegam/centralizam o canvas no nó.
- [x] **Viewport fluido do canvas + minimap revisto** ✅ (2026-07-01): a causa dos jobs "sumindo" ao
  clicar Run Daily/Force (e da câmera voltar ao centro sozinha após cada refresh) era o reancoramento
  do topo disparando a CADA update de dado (status via WS/tick a cada 2s, Run Daily, Force) — agora só
  reancora ao trocar de modo/folders ou quando os jobs aparecem pela 1ª vez; churn de dado não mexe na
  câmera. **Force** passa a centralizar no job forçado quando ele materializa (mantendo o zoom). O
  **minimap** mostra só os jobs em **quadradinhos** (proporção do card) em vez de bolinhas, e desenha o
  **retângulo do viewport** (área visível) refletindo o alinhamento da tela.
- [x] **Câmera do canvas consistente com a trava + board nunca mais vazio sem F5** ✅ (2026-07-01):
  três raízes distintas fechadas de vez. (1) **Pulo pra "posição travada"** ao mexer depois de
  centralizar/organizar/forçar: o `translateExtent` (trava de pan) é em px de **mundo** e estático,
  mas a âncora/centralização posicionavam a câmera em px de **tela** — em zoom < 1 (ou centralizando
  job do topo) a câmera ficava FORA do extent, e o ReactFlow só clampa pan do usuário (movimento
  programático passa direto) → o 1º arrasto reaplicava a trava = pulo. Agora TODO movimento
  programático clampa pelo mesmo limite (`clampTy` + `focusOnPoint`; usado por Organizar, clique na
  sidebar, Force/pendingFocus e minimap), e o extent tem folga de `PAN_SLACK_TOP=176` px de mundo
  (cobre a âncora de 88px de tela até o minZoom 0.5) — de quebra dá a "puxada pra baixo" maior.
  Centralizar job do topo agora "centraliza até onde a trava deixa" e mexer depois NÃO salta.
  (2) **`fitView` do RF v12 é assíncrono** (e o promise nem resolve chamado logo após o mount):
  `organizeView` não usa mais `fitView` — o fit é **calculado na mão** (bounds das lanes + pane via
  `useStoreApi`), síncrono e determinístico; o prop `fitView` do `<ReactFlow>` saiu (corria contra a
  âncora de entrada); o gate de entrada agora espera `paneReady` (dimensões do pane via `useStore`) —
  cobre o mount pós-login em que os nodes já existiam antes do canvas montar. (3) **Abrir o app e o
  board vir vazio até F5**: a carga inicial rodava antes do login com token errado (401 silencioso,
  sem retry) e nada re-buscava depois de logar. Agora `setAuthToken` **reconecta o WS** com o token
  novo; o `onopen` emite o evento sintético `_connected` que ressincroniza instances (store) e
  definitions (UI); a carga inicial tem **retry de 5s** até a 1ª carga boa; e o listener de
  instances não refiltra mais por `todayOrderDate()` do browser (zerava o board no 1º evento WS
  quando o dia do cliente ≠ dia do server — acesso remoto/virada de dia). Validado ao vivo (90 defs
  + 102 instances): entrada=Organizar=âncora idêntica (`ty=76`@zoom .5), drag pós-centralização sem
  snap, Force ×2 com câmera imóvel, login → board aparece sem F5.
- [x] **Hardening pós-review** ✅ (2026-07-01): os 3 pontos de atenção apontados na avaliação do projeto.
  (1) **SQLITE_BUSY em rajada resolvido de verdade** — os pragmas (`busy_timeout`/WAL/foreign_keys) eram
  aplicados via `Exec` numa conexão SÓ do pool do `database/sql`; as demais ficavam sem `busy_timeout` e a
  materialização da daily perdia eventos de auditoria ("database is locked"). Agora vão no **DSN**
  (`_pragma=`), que o modernc/sqlite executa em CADA conexão nova — validado: mesma rajada de 95 jobs,
  **zero** SQLITE_BUSY, eventos `ordered/started/submitted/finished` todos persistidos. (2) **V2Preview
  desmontado em módulos** (2223→1485 linhas): layout puro em `canvas-layout.ts` (constantes + builders +
  dagre), minimap em `NavMinimap.tsx`, câmera em `hooks/useCanvasCamera.ts` (trava + âncora + clamp +
  centralizações + gate de entrada) e bootstrap/sync de dados em `hooks/useOrchestratorData.ts` — os três
  bugs de ciclo de vida da sessão anterior moravam todos nesse arquivo; agora cada preocupação tem dono.
  (3) **Resync `_connected` cobre os "fetch-once" restantes** — badge de alertas, env label e `/me` se
  recuperam na reconexão; bônus emergente validado ao vivo: token inválido no mount → 401 → limpa token →
  reconecta com fallback → board completo SEM F5 nem login manual (em dev; no hosted o LoginForm segue
  gateando). Regressão completa verde pós-refactor: entrada=Organizar (`ty=76`@.5), centralização anima
  até o clamp exato (`ty=167.2`@1.1) e fica, drags sem pulo, Force ×2 com câmera imóvel.
- [ ] **Cap de 2000 do Monitoring legado**: `LEGACY_CAP=2000` (canvas/ACTIVE JOBS não-virtualizados) é arbitrário
  e mostra "2000/2000" como se fosse o total. Fix: **virtualizar a sidebar ACTIVE JOBS** (mostra o dia inteiro),
  header com o **total real** do `/summary` ("2000 carregados de 1.000.000"), e cap do canvas configurável/maior
  com aviso "abra o ViewPoint". (O ViewPoint já mostra 100k–1M.)

### Diferenciais — além do Control-M (visão de produto) — detalhe em [`docs/roadmap.md`](docs/roadmap.md)
Onde o Regente **passa** o Control-M (longo prazo, depois do núcleo sólido):
- **Orquestração híbrida/stateful**: human-in-the-loop + long-running (dias/semanas), pausa/resume com estado, event-driven confiável.
- **Observabilidade avançada**: impact analysis · **blast radius** (cancelar X agora → N jobs caem, Y SLAs violados, atraso estimado) · **dry run** (simular daily futura sem criar instances) · **explain** ("por que o job não rodou?" — motor sem IA: resource/condition/deps) · root-cause automático · forecasting · **diff de daily** (barato via Git-native) · **event log de primeira classe** (CQRS-lite: log transacional/replayável; não ES puro — HA/DR/audit/config já cobertos).
- **Developer experience**: schedule-as-code (YAML + DSL), `regente test`, `regente dev daily` (mock local).
- **Enterprise**: promotion Dev→Staging→Prod via Git, **policy-as-code**, chaos ("Inject Failure").
- **"Wow"**: editor visual **Gantt** da daily, bulk schedule + templates, **self-service portal**, mobile alerts com ações.

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
  `/api/agent/poll|result|output`) para control plane stateless **+ adapter NATS
  (`-bus=nats`)** — hub distribuído com fan-out de eventos web e dispatch roteado ao
  nó dono do agent (R5; **validado em 2 nós + NATS reais**).
- [x] **Fase 3 — executores como plugins**: roteamento por capability é o seam
  **+ executor WASM** (`jobType: WASM` via wazero, pure-Go/sem CGO, sandbox WASI)
  **+ adapters de nuvem** (k8s `K8S_JOB` validado em cluster real, AWS Lambda + GCP Cloud Run com mock).
  *(extensões futuras opt-in: durable execution Temporal/Restate, Postgres-como-fila)*

---

<div align="center">
<sub>Projeto pessoal de portfólio. UX inspirada na operação do Control-M; nenhuma relação com a BMC.</sub>
</div>
