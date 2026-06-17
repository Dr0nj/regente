# Arquitetura futura — Regente serverless, portátil e sem lock-in

> Status: vivo · Última revisão: 2026-06-17
> Escopo: control plane (`server/`) + transporte de agentes + executores.
> Decisão-mãe: **"serverless portátil" (container scale-to-zero + estado/gatilho
> externalizados), não FaaS.**

Este documento é o ADR-guia da evolução do Regente em três fases. Ele registra o
**porquê**, o que **já foi aplicado neste repositório**, e o que é **design/R&D**
para as próximas iterações. Cada fase marca claramente o que é produção vs. projetado.

---

## 1. Contexto e problema

Queremos que o Regente seja:

1. **Serverless** — sem servidor para babá, escala a zero, paga-se pelo uso.
2. **Sem lock-in** — não amarrado à AWS (nem a nenhuma nuvem específica).
3. **Aderente a tecnologias novas** e atraente para a comunidade (adoção).

A tensão real: **um scheduler é, por natureza, stateful e dirigido por tempo.**
Hoje o Regente é um daemon clássico com duas peças que brigam com serverless:

- **Ticker em goroutine** (a cada 2s) materializa a daily e despacha — exige um
  processo sempre-ligado.
- **WebSocket persistente** com agentes e web — conexões de longa duração.

Todo o resto já é serverless-ready: API REST stateless, **estado no Postgres**
(dialeto plugável) e **config no Git**.

### O reframe

`serverless` ≠ FaaS. Há um espectro:

| Abordagem | Lock-in | Fit p/ scheduler |
|---|---|---|
| FaaS (Lambda, Cloud Functions) | alto | ruim (WS + jobs longos + tick) |
| **Container-serverless (Knative, Cloud Run, Fly, App Runner)** | **baixo** | **ótimo** |
| Self-host serverless (Knative em qualquer k8s) | nenhum | ótimo |

**Decisão:** ir de **container-serverless**. Mantém o binário único e o
zero-infra (maior ativo de adoção), entrega a economia/operação de serverless e
**não** acopla à AWS.

### O que NÃO fazer

- **FaaS por job** — mais lock-in, mata o single-binary, briga com o estado.
- **DynamoDB como store primário** — o lock-in mais profundo da AWS. Postgres
  serverless (Neon/Supabase) dá scale-to-zero sem amarrar a nuvem.

---

## 2. Princípios anti-lock-in

Dependa de **protocolos/padrões abertos**, não de serviços gerenciados:

| Necessidade | Padrão aberto (escolha) | Gerenciado equivalente (troca no deploy) |
|---|---|---|
| Empacotamento | OCI image | qualquer registry |
| Runtime | Knative (CNCF) | Cloud Run · App Runner · Container Apps · Fly |
| Estado | Postgres wire protocol | Neon · Supabase · Cloud SQL · RDS |
| Gatilho de tempo | cron → HTTP | Cloud Scheduler · EventBridge · Actions cron |
| Barramento de agentes | NATS (CNCF) / SSE / long-poll | NATS gerenciado |
| Identidade | OIDC | qualquer IdP |
| Logs/artefatos | API S3-compatível | R2 · MinIO · S3 · GCS |

A disciplina de **ports & adapters** que o projeto já tem (dialeto de DB
plugável, `ExecutorPort` no frontend) é estendida para `Bus` (transporte) e para
o **gatilho** (HTTP). Assim o lock-in vira escolha de *deploy*, não de *código*.

---

## 3. Arquitetura-alvo

```
            cron externo (Cloud Scheduler / k8s CronJob / Actions)
                      │  POST /api/scheduler/tick   (Fase 1)
                      ▼
   ┌────────────────────────────────┐        Postgres (wire protocol)
   │  regente-server (imagem OCI)    │◀──────  Neon/Supabase/Cloud SQL/RDS
   │  -role=all|api|scheduler        │
   │  -scheduler=internal|external   │──────▶  Git (fonte da verdade, YAML)
   │  scale-to-zero (Knative/Run/Fly)│
   └────────────────────────────────┘
                      ▲   Bus (Fase 2): WebSocket hoje; NATS/SSE amanhã
                      │   dispatch ▼   ▲ result
              ┌────────────────────────────────┐
              │  agentes / executores (Fase 3)  │
              │  COMMAND·SCRIPT·HTTP·SSH hoje    │
              │  + WASM · AWS · GCP · k8s Jobs   │  (novos = nova capability)
              └────────────────────────────────┘
```

---

## 4. Fase 1 — externalizar o gatilho de tempo  ✅ aplicado

**Objetivo:** remover a dependência do ticker sempre-ligado para permitir
scale-to-zero. É a mudança de **maior alavancagem** (≈80% do "serverless").

**Implementado neste repo:**

- `Scheduler.Tick()` (`server/internal/scheduler/scheduler.go`) — um ciclo de
  scheduling (daily-se-devida + dispatch), **idempotente** (claim atômico em
  `startInstance` + checagem de existência na daily) e **leader-guarded**.
  `Run()` agora só chama `Tick()` em loop.
- Flags em `main.go`:
  - `-scheduler=internal|external` (`REGENTE_SCHEDULER`). `internal` = ticker em
    goroutine (default, daemon clássico). `external` = sem ticker; as defs são
    carregadas no boot e o ciclo é dirigido por HTTP.
  - `-role=all|api|scheduler` (`REGENTE_ROLE`) — modular monolith: o mesmo
    binário sobe como tudo (default) ou separa API de scheduling.
- Endpoint `POST /api/scheduler/tick` (writer-gated) → chama `Tick()`.
- Artefatos de deploy em [`../deploy/`](../deploy): `Dockerfile` (distroless),
  `knative-service.yaml` (`minScale: 0`), `cronjob.yaml` (gatilho externo),
  `README.md`.

**Compatibilidade:** defaults (`all` + `internal`) = comportamento idêntico ao
anterior. Nada quebra para quem roda como daemon.

**Resultado:** com `-scheduler=external` + cron externo + Postgres gerenciado, o
control plane roda **serverless, scale-to-zero, sem lock-in**.

**Futuro próximo:** mover `autoDailyIfDue` para um gatilho separado de cadência
diária (em vez de checar a cada tick), e expor um `/api/scheduler/daily` dedicado.

---

## 5. Fase 2 — transporte de agentes plugável (BusPort)  ◑ seam aplicado, adapters projetados

**Objetivo:** o WebSocket hub é a última peça que exige persistência. Abstrair o
transporte para poder trocá-lo sem tocar no core.

**Implementado neste repo:**

- Interface `scheduler.Bus` (`server/internal/scheduler/scheduler.go`):
  ```go
  type Bus interface {
      BroadcastWeb(event string, payload interface{})
      PickAgent(capability string) *hub.Client
      GetAgent(id string) *hub.Client
  }
  ```
  O scheduler agora depende de `Bus`, não de `*hub.Hub`. `*hub.Hub` satisfaz a
  interface (default WebSocket). Esse é o **seam**: o transporte virou um
  detalhe substituível.

**Projetado (próximas iterações):**

- **Adapter NATS** (`internal/bus/nats`) — barramento CNCF, self-hostável e com
  opção gerenciada; a escolha anti-lock-in contra SQS/EventBridge. Permite
  control plane stateless: agentes assinam `regente.dispatch.<cap>`, o server
  publica; resultados voltam por `regente.result`.
- **Adapter SSE / long-poll** — para deploys 100% serverless sem broker: o
  agente faz long-poll em `GET /api/agent/dispatch` (stateless) e devolve por
  `POST /api/agent/result`. Mantém o modelo *outbound* NAT-friendly atual.
- **Hub WebSocket distribuído** — fan-out de eventos web via NATS/Redis pub-sub
  para múltiplos nós (hoje o hub é por-processo).

**Por que não já:** NATS adiciona dependência de infra e o SSE muda o protocolo
do agente — merece iteração e teste dedicados. O seam acima garante que entram
sem refatorar o scheduler.

---

## 6. Fase 3 — executores como plugins + tecnologias novas  ◑ seam existe, executores projetados

**Objetivo:** aderência a tecnologias novas e diferenciação, mantendo o núcleo
agnóstico de fornecedor.

**Seam que já existe (e é o ponto central):** o roteamento por **capability**.
O agente anuncia capacidades (`-caps COMMAND,SCRIPT,HTTP,REST`) e despacha por
`switch jobType` (`agent/main.go`); o server casa `jobType`→agente via
`PickAgent(capability)`. **Adicionar um executor novo = adicionar um `case` +
anunciar a capability.** O core não muda. Logo, "executores multi-cloud como
plugins" já está habilitado pela arquitetura atual.

**Projetado (R&D, marca claramente futuro):**

- **Executor WASM** (`wasmtime`/`extism`) — rodar lógica de job como módulo WASM
  sandboxed e portátil: "função serverless que é sua", sem shell-out. Novo
  `jobType: WASM` + capability `WASM`. Forte diferencial e muito na moda.
- **Executores de nuvem como adapters iguais** — `AWS` (Lambda/Batch/Glue/Step),
  `GCP` (Cloud Run Jobs), `k8s Jobs`. Cada um é **uma capability**, nunca a
  fundação — AWS deixa de ser base e vira plugin.
- **Durable execution (Temporal / Restate)** — trilha opcional de
  confiabilidade: substituir retry/tick/state-machine feitos à mão por um motor
  durável testado. Grande história de DX/marketing; pesado, então fica como
  opt-in, não default.
- **Postgres-como-fila** (`SKIP LOCKED`, ex. River) — fila + estado numa só
  dependência, sem Redis/SQS. Reforça o anti-lock-in.
- **OpenTelemetry** — tracing distribuído (já no roadmap de operação).

---

## 7. Status e decisões

| Item | Fase | Status |
|---|---|---|
| `Tick()` idempotente + `Run()` refatorado | 1 | ✅ aplicado |
| Flags `-scheduler`, `-role` | 1 | ✅ aplicado |
| `POST /api/scheduler/tick` | 1 | ✅ aplicado |
| Dockerfile + Knative + CronJob + deploy/README | 1 | ✅ aplicado |
| Interface `Bus` (seam de transporte) | 2 | ✅ aplicado |
| Adapter NATS | 2 | ◻ projetado |
| Adapter SSE/long-poll p/ agente | 2 | ◻ projetado |
| Hub WebSocket distribuído | 2 | ◻ projetado |
| Executor WASM | 3 | ◻ projetado |
| Adapters de nuvem (AWS/GCP/k8s) por capability | 3 | ◻ projetado (seam pronto) |
| Durable execution (Temporal/Restate) | 3 | ◻ R&D opt-in |
| Postgres-como-fila | 3 | ◻ projetado |

**Princípio de rollout:** cada fase é backward-compatible. Os defaults
preservam o daemon clássico; o serverless é opt-in por flag/deploy.

---

## 8. Referências

- Knative (scale-to-zero portátil): https://knative.dev
- NATS (barramento CNCF): https://nats.io
- Temporal (durable execution): https://temporal.io · Restate: https://restate.dev
- extism / wasmtime (WASM): https://extism.org · https://wasmtime.dev
- River (fila em Postgres): https://riverqueue.com
- Padrão ports & adapters (hexagonal): Alistair Cockburn
