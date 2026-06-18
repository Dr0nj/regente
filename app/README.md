<div align="center">
  <img src="public/favicon-512.png" width="96" alt="Regente" />
  <h1>Regente — Web (frontend)</h1>
  <p><strong>Frontend do Regente, orquestrador de workflows Git-nativo inspirado em Control-M.</strong></p>
</div>

> 📦 Esta é a pasta **`app/`** do monorepo. Visão geral do projeto, arquitetura e
> instalação do **server**/**agent** estão no **[README raiz](../README.md)**.

O frontend (React + TypeScript + Vite) é o cliente HTTP/WebSocket do `regente-server`:
Monitoring (o que roda hoje) e Design (canvas drag-and-drop das definitions, com
Publish pro Git). A UX é a de quem opera Control-M (folders, hold/rerun/force,
find & update).

---

## Conceito

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

## Funcionalidades

- 🟢 **Git-nativo (GitOps)** — Publish vira commit/PR; mudanças no GitHub voltam
  pra UI via webhook + polling. Deep-links job→YAML, instance→commit.
- 🟢 **Executores locais via agente** — `COMMAND` (shell), `SCRIPT` (.sh/.bat/.ps1)
  e `HTTP`, rodando na máquina onde o agente está (Windows ou Linux). Cada job
  pode mirar um agente específico ou ser roteado por capability. Tokens **por
  agente** + agente instalável como serviço (systemd / Tarefa Agendada).
- 🟢 **SSH agentless** — `SSH` roda comando remoto direto do server (sem agente
  no alvo), com stream de saída.
- 🟢 **Retry de execution** — re-tentativa automática em falha (respeita `retries`).
- 🟢 **Observabilidade** — `/metrics` em formato Prometheus.
- 🟢 **Executor WASM** — `WASM` roda módulos WebAssembly WASI sandboxed via wazero
  (pure-Go, sem CGO).
- 🟢 **Alerting (Fase 8)** — regras configuráveis, tela de alertas (sino + badge +
  ack), toast em tempo real e routing externo (Slack/webhook).
- 🟢 **Schedule estilo Control-M** — dias da semana/mês, N-ésimo dia útil, regras
  avançadas, janelas e execução cíclica; calendars include/exclude visuais.
- 🟢 **Dependências entre jobs** com condições (on-success/failure/complete/always).
- 🟢 **Engines de paridade** — calendars, resources/quotas, conditions, variáveis
  globais (interpolação), SLA e forecast/analytics.
- 🟢 **Token do GitHub pela UI** — configurável em runtime (Settings), persistido
  server-side, sem precisar subir o server com `GITHUB_TOKEN`.

## Stack

| Camada | Tecnologia |
|---|---|
| Frontend (este repo) | React + TypeScript + Vite, [@xyflow/react](https://reactflow.dev) (canvas), ícones lucide |
| Backend | Go (`regente-server`) — GitOps + SQLite/Postgres, WebSocket hub |
| Executor | Go (`regente-agent`) — conexão outbound (WS ou HTTP long-poll), roda COMMAND/SCRIPT/HTTP/SSH/WASM |
| Fonte da verdade | Repositório GitHub (YAML em `definitions/<folder>/<id>.yaml`) |

## Rodando o frontend

Pré-requisitos: Node 18+ e o `regente-server` rodando (ver abaixo).

```bash
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

## Arquitetura

```
  ┌──────────────┐    REST + WebSocket    ┌──────────────────┐   git push/pull   ┌──────────┐
  │  Frontend     │ ─────────────────────▶ │  regente-server   │ ◀───────────────▶ │  GitHub   │
  │  (este repo)  │ ◀───────────────────── │  (Go, SQLite)     │   (fonte da       │  (YAML)   │
  └──────────────┘    instance.changed     └──────────────────┘    verdade)        └──────────┘
                                                    ▲
                                                    │ WebSocket (agente disca pra fora)
                                                    │ dispatch ▼   ▲ result
                                            ┌──────────────────┐
                                            │  regente-agent    │  roda COMMAND/SCRIPT/HTTP
                                            │  (seu PC / EC2)   │  no Windows ou Linux
                                            └──────────────────┘
```

O `regente-server` e o `regente-agent` (Go) vivem fora deste repositório. Em dev:

```bash
# server (GitOps + SQLite)
cd ../server && go run . -api-token dev-token

# agente executor (na máquina onde os comandos devem rodar)
cd ../agent  && go run . -server ws://localhost:8080/ws/agent -token dev-token \
                         -id meu-pc -caps COMMAND,SCRIPT,HTTP
```

## Estrutura (frontend)

```
src/
├── v2/                  # UI atual (Monitoring, Design, drawers, dialogs)
│   ├── V2Preview.tsx    # shell principal (topbar, canvas, modos)
│   ├── JobConfigDrawer  # edição de job (Geral/Schedule/Calendars/Action/Deps)
│   ├── ScheduleEditor   # scheduler visual estilo Control-M
│   ├── AlertsPanel.tsx  # tela de alertas (eventos + regras + canais) — Fase 8
│   └── ...
├── lib/                 # clientes de API + modelo + adapters
│   ├── server-client.ts # REST + WS
│   ├── git-api.ts       # status, token, cleanup, deep-links
│   ├── agents-api.ts    # agentes online
│   ├── alerts-api.ts    # alertas (facade dual-mode server/local)
│   └── adapters/        # ports & adapters (storage/scheduler/executor)
└── main.tsx
```

## Roadmap

> Fonte única no monorepo (evita divergência): checklist no
> **[README raiz](../README.md#-roadmap)** e a versão visual consolidada em
> **[`../docs/roadmap.md`](../docs/roadmap.md)**. A estratégia serverless
> (sem lock-in) está em **[`../docs/arquitetura-futuro.md`](../docs/arquitetura-futuro.md)**.

---

<sub>Projeto pessoal de portfólio. UI inspirada na operação do Control-M; nenhuma
relação com a BMC.</sub>
