# CLAUDE.md — memória do projeto Regente

Orientação para agentes trabalhando neste repositório. Detalhe de produto está
no [`README.md`](README.md); decisões de arquitetura futura em
[`docs/arquitetura-futuro.md`](docs/arquitetura-futuro.md).

## O que é

Orquestrador de jobs estilo Control-M, **Git-native**. Monorepo:

| Pasta | Stack | Papel |
|---|---|---|
| `server/` | Go (chi + WebSocket + SQLite/Postgres) | control plane: scheduler, API, hub, GitOps |
| `agent/` | Go (single `main.go`) | executor local, conexão outbound; COMMAND/SCRIPT/HTTP/WASM |
| `app/` | React + TS + Vite | UI (Monitoring + Design); dual-mode server/localStorage |

Fonte da verdade das definitions = YAML em repo GitHub separado (`regente-workspace`).

## Comandos

```bash
# server
cd server && go build ./... && go vet ./... && go test ./...
# agent
cd agent  && go build ./... && go vet ./... && go test ./...
# app
cd app && npm ci && npm run build      # tsc -b && vite build
cd app && npx eslint <arquivos>        # lint (NÃO rode em V2Preview.tsx isolado: tem 2 lints PRÉ-EXISTENTES)
```

CI (`.github/workflows/ci.yml`): 3 jobs — server (build/vet/test), agent
(build/test), app (`npm ci` + build). Roda em push na `main` e em PRs.

## Convenções

- **Go:** sempre `gofmt -w` os arquivos tocados antes de commitar. Idioma dos
  comentários: português. Sem CGO (SQLite via modernc; WASM via wazero).
- **Commits:** Conventional Commits em português (`feat:`, `fix:`, `docs:`…).
- **Push:** o dono autorizou **commitar e dar push direto na `main`** (sem
  branch/PR), salvo se houver branch protection — aí cai em branch + PR.
- **Modelo/identidade:** nunca colocar o id do modelo em commits/PRs/código.

## Arquitetura serverless (aplicada — ver ADR)

"Serverless portátil" (container scale-to-zero + estado/gatilho externalizados),
**não FaaS**, **sem lock-in AWS**. Tudo opt-in; defaults = daemon clássico.

- **Fase 1 — gatilho externo:** `Scheduler.Tick()` idempotente; flags
  `-scheduler=internal|external` e `-role=all|api|scheduler`; `POST /api/scheduler/tick`.
  Artefatos em [`deploy/`](deploy) (Dockerfile distroless, Knative, CronJob).
- **Fase 2 — transporte plugável:** interface `scheduler.Bus` + transporte HTTP
  long-poll (`agent -transport=http`; `agentBroker` + `/api/agent/poll|result|output`).
- **Fase 3 — executores como plugins:** roteamento por capability é o seam;
  executor **WASM** (`jobType: WASM`, wazero) entregue. Adapters de nuvem/NATS/
  durable-execution ficam projetados.

## Features entregues (histórico)

- **Alerting (Fase 8):** motor de regras (`server/internal/scheduler/alerting.go`
  + `app/src/lib/alerting.ts`), tela `app/src/v2/AlertsPanel.tsx` (sino + badge),
  API `/api/alerts*`, broadcast `alert.fired`. Dual-mode (Postgres/localStorage).
  **Routing externo multi-canal:** sinks Slack (`alert_slack_webhook`), webhook
  genérico (`alert_webhook_url`), e-mail SMTP (`alert_smtp_*`, `net/smtp`) e
  PagerDuty Events v2 (`alert_pagerduty_routing_key`) — credenciais mascaradas em
  `/api/settings`. **Routing por-regra:** cada regra escolhe os canais em `channels`
  (`PUT /api/alerts/rules/{id}/channels`); `channelWanted` decide o disparo e
  regra sem canal externo cai em fallback "todos os sinks configurados" (back-compat).
  Router best-effort async em `alerting.go`; UI na aba Regras (chips por regra +
  config de sinks, admin).
- **Serverless Fases 1–3** (acima).

## Gotchas

- **npm optional deps (bug #4828):** `app/package-lock.json` precisa conter as
  entradas instaláveis dos binários nativos (`@rolldown/binding-linux-x64-gnu`,
  `lightningcss-linux-x64-gnu`, `@tailwindcss/oxide-linux-x64-gnu`). Se o `vite
  build` quebrar com "Cannot find native binding", regenere o lockfile:
  `rm -rf node_modules package-lock.json && npm install`.
- **Fixture WASM:** `agent/testdata/echo.wasm` é compilado de
  `testdata/echo/main.go` com `GOOS=wasip1 GOARCH=wasm go build`. É grande (~2.4MB,
  saída padrão do Go) mas committado para o teste ser hermético.
- **`V2Preview.tsx`** tem 2 problemas de eslint **pré-existentes** (linha do
  `_condition` e um `eslint-disable` órfão) — não são regressões; não gaste tempo.
- **Scheduler stateful:** o tick interno (modo `internal`) reload de defs no boot
  + on-save; no modo `external` as defs são carregadas no boot (`ReloadDefs`).
