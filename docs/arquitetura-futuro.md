# Arquitetura futura — Regente serverless, portátil e sem lock-in

> Status: vivo · Última revisão: 2026-07-10
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
persistidas no PG) de forma idempotente. A 1ª rodada **descobriu** um furo: a
camada de storage fazia `exec git`, então a imagem `distroless/static` crashava
no boot (`EnsureClone`: `git` ausente) — contornado na época com Alpine+git. A
**correção definitiva** migrou `internal/storage/git.go` para **go-git** (Go
puro, sem shell-out): a imagem voltou a `gcr.io/distroless/static-debian12:nonroot`
(estática, non-root, só ca-certificates) e o boot/daily clonam/fazem fetch+reset
**sem `git` no PATH**. Cuidado tratado: o `reset --hard` do go-git apaga arquivos
untracked (diverge do git); reproduzimos o git limitando o reset à união dos
paths tracked, preservando a SQLite DB no workspace.

**Gatilho de daily dedicado (ARCH-5, ✅ aplicado 2026-07-10):** `autoDailyIfDue`
foi exposto como `Scheduler.RunDailyIfDue()` (leader-gated, idempotente) atrás de
`POST /api/scheduler/daily` — no serverless você aponta um cron de minutos em
`/scheduler/tick` (dispatch) e um cron **diário** em `/scheduler/daily`
(materialização), separando as cadências. O Tick segue chamando `autoDailyIfDue`
(backward-compat com o daemon interno).

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

**Lock-por-tick (ARCH-3, ✅ aplicado 2026-07-10):** o mecanismo projetado acima —
serializar ticks **sobrepostos** com um advisory lock de escopo curto — foi
implementado em `scheduler/ticklock.go`. Duas camadas: em-processo (`atomic.Bool`,
sempre ativa) + `pg_try_advisory_lock` cross-processo no Postgres (chave distinta
da liderança, opt-in via `EnableTickLock()`, ligado no `-scheduler=external`).
Segue sendo higiene, não corretude — o claim atômico é a rede de segurança.

---

## 5. Fase 2 — transporte de agentes plugável (BusPort)  ✅ seam + long-poll + SSE + NATS aplicados

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

**Também aplicado (depois da 1ª redação deste ADR):**

- **Transporte SSE** (ARCH-4, ✅ 2026-07-10) — `api/agent_sse.go` +
  `-transport=sse` no agente: stream `text/event-stream` com push IMEDIATO do
  dispatch (sem a janela de ~25s do long-poll), mesmo modelo outbound e mesmo
  `agentBroker`/`hub.Client`; resultados voltam pelos mesmos POSTs.
- **Adapter NATS + hub distribuído** (R5, ✅ 2026-06-22) — `-bus=nats`:
  fan-out de eventos web, presença de agentes e dispatch roteado ao nó dono do
  agente entre múltiplos nós. VALIDADO em 2 nós reais. (Sub-estimado na 1ª
  redação como "projetado"; entregue e testado.)

---

## 6. Fase 3 — executores como plugins + tecnologias novas  ✅ WASM + nuvem + OTel aplicados

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

**Também aplicado (depois da 1ª redação):**

- **Executores de nuvem como adapters iguais** (✅) — `AWS` Lambda (SigV4 stdlib)
  + Batch/Glue/Step (ADV-8), `GCP` Cloud Run Jobs, `k8s Jobs` (validado em
  cluster kind real). Cada um é **uma capability**, nunca a fundação — AWS é
  plugin, não base.
- **OpenTelemetry** (✅) — tracing OTLP/HTTP opt-in (`-otel-endpoint`), otelhttp
  + spans no scheduler.

**🚫 Decidido NÃO fazer (menção, sem pendência):**

- **Durable execution (Temporal / Restate)** — substituir retry/tick/state-machine
  por um motor durável **contradiz a decisão-mãe** deste ADR (single-binary,
  zero-infra, anti-lock-in): exigiria rodar um cluster a mais para babá. E a
  corretude que ela entrega (retomar um fluxo pós-crash sem re-executar passos) o
  Regente **já dá** por idempotência + claim atômico. Fica como referência de
  arquitetura, não como trabalho.
- **Postgres-como-fila** (`SKIP LOCKED`, ex. River) — reescreveria o dispatch (hot
  path validado a 1M) por uma alternativa ao claim atômico — que **é primo** do
  `SKIP LOCKED` e já cobre o caso. Sem ganho que pague a troca.

---

## 7. Status e decisões

| Item | Fase | Status |
|---|---|---|
| `Tick()` idempotente + `Run()` refatorado | 1 | ✅ aplicado |
| Flags `-scheduler`, `-role` | 1 | ✅ aplicado |
| `POST /api/scheduler/tick` | 1 | ✅ aplicado |
| Dockerfile + Knative + CronJob + deploy/README | 1 | ✅ aplicado · e2e em container validado (2026-06-18) |
| Imagem distroless sem `git` no PATH | 1 | ✅ storage migrada p/ go-git (Go puro); imagem voltou a `distroless/static:nonroot` |
| HA clássico — leader election advisory-lock (`G1`) | 1 | ✅ aplicado · 2-nós validado em PG real (failover ~1s, 2026-06-18) |
| HA serverless — idempotência + claim atômico | 1 | ✅ aplicado (corretude) |
| Lock-por-tick (ticks sobrepostos, ARCH-3) | 1 | ✅ aplicado (2026-07-10; higiene, não correção) |
| Gatilho de daily dedicado (`/api/scheduler/daily`, ARCH-5) | 1 | ✅ aplicado (2026-07-10) |
| Interface `Bus` (seam de transporte) | 2 | ✅ aplicado |
| Transporte HTTP long-poll (`-transport=http`) | 2 | ✅ aplicado (e2e) |
| Transporte SSE (`-transport=sse`, ARCH-4) | 2 | ✅ aplicado (2026-07-10; e2e ao vivo) |
| Adapter NATS | 2 | ✅ aplicado (R5; 2-nós real) |
| Hub WebSocket distribuído | 2 | ✅ aplicado (R5) |
| Executor WASM (wazero) | 3 | ✅ aplicado (unit + e2e) |
| Adapters de nuvem (AWS/GCP/k8s) por capability | 3 | ✅ aplicado (k8s real; AWS/GCP mock + ADV-8) |
| OpenTelemetry (tracing OTLP) | 3 | ✅ aplicado (opt-in) |
| Durable execution (Temporal/Restate) | 3 | 🚫 decidido não fazer (contradiz a decisão-mãe) |
| Postgres-como-fila (River/SKIP LOCKED) | 3 | 🚫 decidido não fazer (claim atômico já cobre) |

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
