# Arquitetura futura — Regente serverless, portátil e sem lock-in

> Status: vivo · Última revisão: 2026-06-18
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

**Validado em container (2026-06-18):** imagem de [`../deploy`](../deploy) +
Postgres em container → boot OK, `/health`→200, leader election, e o gatilho
externo `POST /api/scheduler/tick` materializou a daily (`5 instances`,
persistidas no PG) de forma idempotente. A validação **descobriu e corrigiu** um
furo: a camada de storage faz `exec git`, então a imagem `distroless/static`
crashava no boot (`EnsureClone`: `git` ausente). Final stage agora é Alpine com
`git`+`ca-certificates`, non-root (~55 MB). Alternativa distroless = git-sync +
`-git-source ""` (defs do disco) — ver `deploy/README.md`.

**Futuro próximo:** mover `autoDailyIfDue` para um gatilho separado de cadência
diária (em vez de checar a cada tick), e expor um `/api/scheduler/daily` dedicado.

### Concorrência e HA — clássico vs. serverless (importante)

Há **dois modelos de HA** no Regente, e eles não são o mesmo mecanismo. Confundir
os dois leva a super-investir no lock errado.

**Trilho clássico (daemon, `-scheduler=internal`):** N nós sempre-ligados, um
segura `pg_try_advisory_lock` e roda o ticker; os outros são *hot standbys*. É o
`G1 leader election` (`internal/leader`, `PgAdvisory`). Faz sentido para deploy
on-prem / enterprise de 2-3 nós fixos. Failover ≈ TTL do lock quando o líder cai.

**Trilho serverless (`-scheduler=external`, scale-to-zero):** o advisory-lock de
liderança **não mapeia** — não há processo de longa duração para *segurar* o lock,
e na maior parte do tempo há **zero** instâncias. O cron externo dispara
`POST /api/scheduler/tick`, um container sobe, roda `Tick()`, morre. A correção
contra **execução dupla** não vem de liderança, vem de **idempotência + claim
atômico**, que já estão no código:

1. `Tick()` idempotente — ticks concorrentes/sobrepostos convergem.
2. **Claim atômico** em `startInstance` (`UPDATE … SET status='RUNNING' WHERE
   id=? AND status='WAITING'`, bail se 0 linhas) — guard de correção nível-linha,
   independe de quantas instâncias rodam. É a rede de segurança real.
3. Checagem de existência na materialização da daily — idempotente entre ticks.

Ou seja: **no serverless, leader election deixa de ser correção e vira otimização**
(evitar N nós fazendo scheduling redundante). Se for preciso blindar ticks
**sobrepostos** (cron que dispara 2× ou ticks longos que se cruzam), o mecanismo
certo é um **lock por-tick** — `pg_try_advisory_lock` adquirido no início do
`Tick()` e solto no fim, escopo curto — e **não** um lock de liderança de longa
duração. (Projetado; hoje a idempotência + claim já garantem correção, então é
opt-in para economia, não para corretude.)

**Resumo:** `F1 Postgres` é fundação dos dois trilhos. `G1 advisory-lock` é HA do
trilho **clássico**; o serverless ganha HA "de graça" (réplicas stateless + PG
gerenciado durável + idempotência + claim atômico).

---

## 5. Fase 2 — transporte de agentes plugável (BusPort)  ✅ seam + long-poll aplicados

**Objetivo:** o WebSocket hub é a última peça que exige persistência. Abstrair o
transporte e oferecer uma via stateless para deploys serverless.

**Implementado neste repo:**

- Interface `scheduler.Bus` (`server/internal/scheduler/scheduler.go`):
  ```go
  type Bus interface {
      BroadcastWeb(event string, payload interface{})
      PickAgent(capability string) *hub.Client
      GetAgent(id string) *hub.Client
  }
  ```
  O scheduler depende de `Bus`, não de `*hub.Hub`. Transporte virou detalhe
  substituível.
- **Transporte HTTP long-poll** (`server/internal/api/agent_http.go` + agent):
  - `GET /api/agent/poll?id=&caps=` — bloqueia ~25s esperando dispatch (204 no
    timeout); `POST /api/agent/result` finaliza; `POST /api/agent/output`
    streama stdout/stderr.
  - O `agentBroker` registra o agente como `hub.Client` com canal `Send`, então
    **reaproveita todo o roteamento existente** (`startInstance` → `PickAgent` →
    `Send`) — zero mudança no scheduler. Reaper remove agentes que param de
    pollar (não há "disconnect" como no WS).
  - Agent: flag `-transport=ws|http` (`REGENTE_AGENT_TRANSPORT`). `http` mantém o
    modelo outbound NAT-friendly, sem conexão persistente no processo do server
    → control plane pode escalar a zero.
  - **Validado e2e:** agent em `-transport=http` recebe dispatch (daily + force),
    executa e devolve resultado/stream.

**Projetado (próximas iterações):**

- **Adapter NATS** (`internal/bus/nats`) — barramento CNCF para escala multi-nó:
  agentes assinam `regente.dispatch.<cap>`; resultados em `regente.result`.
- **Hub WebSocket distribuído** — fan-out de eventos web via NATS/Redis pub-sub
  para múltiplos nós (hoje o hub é por-processo).
- **SSE** como alternativa ao long-poll para push de menor latência.

---

## 6. Fase 3 — executores como plugins + tecnologias novas  ◑ executor WASM aplicado

**Objetivo:** aderência a tecnologias novas e diferenciação, mantendo o núcleo
agnóstico de fornecedor.

**Seam (ponto central):** o roteamento por **capability**. O agente anuncia
capacidades (`-caps`) e despacha por `switch jobType` (`agent/main.go`); o server
casa `jobType`→agente via `PickAgent(capability)`. **Adicionar um executor novo
= adicionar um `case` + anunciar a capability.** O core não muda.

**Implementado neste repo — executor WASM:**

- `jobType: WASM` + capability `WASM` (no default de `-caps`).
- `runWASM`/`execWASM` (`agent/main.go`) usam **wazero** (runtime WASM pure-Go,
  sem CGO — mesmo ethos do SQLite modernc). Roda módulos WASI (`_start`)
  compiláveis de Rust/Go/TinyGo/C; sandbox por construção (só enxerga o que for
  provido). Params: `wasmPath` ou `wasmUrl`, `args`, `stdin`. Captura
  stdout/stderr e mapeia o exit WASI → exitCode do job.
- **Testado:** unit (`agent/wasm_test.go` com fixture WASI embarcado) + e2e
  (dispatch → runWASM → result via agent).

**Projetado (R&D, marca claramente futuro):**

- **Executores de nuvem como adapters iguais** — `AWS` (Lambda/Batch/Glue/Step),
  `GCP` (Cloud Run Jobs), `k8s Jobs`. Cada um é **uma capability**, nunca a
  fundação — AWS deixa de ser base e vira plugin.
- **Durable execution (Temporal / Restate)** — trilha opcional: substituir
  retry/tick/state-machine por um motor durável testado. Opt-in, não default.
- **Postgres-como-fila** (`SKIP LOCKED`, ex. River) — fila + estado numa só
  dependência, sem Redis/SQS.
- **OpenTelemetry** — tracing distribuído (já no roadmap de operação).

---

## 7. Status e decisões

| Item | Fase | Status |
|---|---|---|
| `Tick()` idempotente + `Run()` refatorado | 1 | ✅ aplicado |
| Flags `-scheduler`, `-role` | 1 | ✅ aplicado |
| `POST /api/scheduler/tick` | 1 | ✅ aplicado |
| Dockerfile + Knative + CronJob + deploy/README | 1 | ✅ aplicado · e2e em container validado (2026-06-18) |
| Imagem precisa de `git` no PATH (GitOps faz `exec git`) | 1 | ✅ corrigido — Alpine+git non-root (era distroless, crashava no boot) |
| HA clássico — leader election advisory-lock (`G1`) | 1 | ✅ aplicado · 2-nós validado em PG real (failover ~1s, 2026-06-18) |
| HA serverless — idempotência + claim atômico | 1 | ✅ aplicado (corretude); lock-por-tick ◻ projetado |
| Interface `Bus` (seam de transporte) | 2 | ✅ aplicado |
| Transporte HTTP long-poll (`-transport=http`) | 2 | ✅ aplicado (e2e) |
| Adapter NATS | 2 | ◻ projetado |
| Hub WebSocket distribuído | 2 | ◻ projetado |
| Executor WASM (wazero) | 3 | ✅ aplicado (unit + e2e) |
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
