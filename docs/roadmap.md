# 🎼 Regente — Roadmap

> Documento vivo · revisão 2026-06-22.
> Estratégia de arquitetura em [`arquitetura-futuro.md`](arquitetura-futuro.md);
> detalhe de produto no [`../README.md`](../README.md).

## 📊 Visão geral

```
Núcleo / Control-M        ██████████████████████ 100%  ✅ pronto
Identidade visual / UI    ██████████████████████ 100%  ✅ logo, topbar, 13 temas, login vídeo, sidebars
Alerting                  ██████████████████████ 100%  ✅ multi-canal + por-regra
Serverless portátil       ████████████████░░░░░░  70%  🟡 Knative/long-poll/WASM ✓ · NATS/cloud ⬜
Enterprise readiness      ████████████░░░░░░░░░░  55%  🟡 F1+G1+H3+H1-seam ✓
Resiliência operacional   ██████░░░░░░░░░░░░░░░░░  30%  🔴 estado ✓ · liveness do processo ✗
```

Legenda: ✅ pronto · 🟡 em andamento · ⬜ a fazer · ⭐ recomendado · 🔴 prioridade

---

## 🟢 Fundação — *pronta*

```
✅ GitOps (Publish · webhook · drift · deep-links · PAT via UI)
✅ Paridade Control-M (calendars · resources · conditions · vars · SLA · forecast)
✅ Daily imutável · dependências com condições · Force Order
✅ Executores: COMMAND · SCRIPT · HTTP · SSH agentless · WASM
✅ Stream stdout/stderr · retry · /metrics Prometheus
```

## 🎨 Identidade visual / UI

```
✅ Logo R próprio (branco, vetorial, transparente) — topbar, favicon e login
✅ Topbar premium flutuante — segmented ‹ DESIGN · MONITORING › com chevrons + neon por tema
✅ Cluster de ações — alertas (sino) · configurações/conta (engrenagem) · tela cheia (monitor) · avatar
✅ Tela de login com o logotipo
✅ Sidebars flutuantes (folders/jobs) com realce neon no item selecionado
✅ 13 temas (Escuro · Verde Amarelo · Amarelo Ouro · Verde Mata · Azul Neon · Azul Escuro ·
   Rosa · Violeta · Vermelho · Laranja · Cinza · Bege Escuro · Marrom) com swatch de cores
✅ Configurações em sub-abas (Geral · Temas); borda neon nos diálogos
```

## 🔔 Alerting

```
✅ Motor de regras (falha · lentidão · retries · taxa · falhas consecutivas)
✅ Tela de alertas (sino + badge + ack) · toast em tempo real · dual-mode
✅ Routing externo  → Slack · webhook · e-mail (SMTP) · PagerDuty Events API
✅ Routing por regra na UI (canais selecionáveis por regra; fallback → todos)
✅ Cooldown por (regra×job) — rajada não perde erro; só re-disparo do mesmo job agrupa
✅ Ciclo de vida — rerun/Set OK marcam o alerta como tratado; re-falha gera alerta novo
```

---

## 🔴 Resiliência operacional *(nova trilha — prioridade máxima)*

> **Contexto.** O Regente blinda *estado e correção* (estado durável externo, daily
> idempotente, claim atômico, leader election, watchdog de stuck, retry persistido,
> snapshot imutável) — matar o processo **não perde nada**. O gap é **liveness do
> processo**: o servidor é tratado como se nunca fosse morrer. Um orquestrador crítico
> tem que assumir morte e **voltar sozinho, sem perda**. Esta trilha fecha essa metade.

```
✅ Estado durável (SQLite/Postgres) · daily idempotente · claim atômico
✅ Leader election (advisory lock, failover ~1s) · watchdog stuck-running (15min)
✅ Retry persistido (attempts na DB) · daily fail-safe (adia se git sync falha)
─────────────────────────────────────────────────────────────────────────────
🔴 R1 · Server supervisionado          → systemd Restart=always + Windows Service +
        (o agente já tem; o server não)  livenessProbe no Knative + docs (default supervisionado)
🔴 R2 · Panic-recovery + watchdog tick → recover() no dispatch e no Tick; idade do
        (1 job não pode derrubar o cérebro) último tick em /metrics e /livez
🟠 R3 · Health real                    → /livez vs /readyz (ping DB · status líder · último tick/daily)
🟠 R4 · Config persiste no restart      → produção = Postgres + secrets via provider + volume p/ SQLite
🟠 R6 · DR/backup (G3)                  → runbook backup/restore · PITR no Postgres
🟠 R7 · Auto-SLO do control plane      → alerta tick parado · leader flapping · erro de DB · frota de agentes
```

## ☁️ Serverless portátil *(sem lock-in)*

```
✅ Fase 1 · gatilho externo   → Tick() · -scheduler=external · deploy/ (Knative) ✔e2e em container
✅ Fase 2 · transporte         → Bus + HTTP long-poll (-transport=http)
✅ Fase 3 · executor WASM      → wazero, pure-Go, sandbox WASI
─────────────────────────────────────────────────────────────────
🔴 R5 · NATS + hub WebSocket distribuído   (escala multi-nó; failover não estranda agentes)   ⭐
⬜ Adapters de nuvem por capability        (AWS · GCP · k8s Jobs)
⬜ Durable execution (Temporal / Restate)  (opt-in)
⬜ Postgres-como-fila (SKIP LOCKED)
```

## 🏢 Enterprise readiness

```
✅ Escala     → Postgres plugável + migrations ✔validado  ⬜ stateless · 10k+ jobs/dia
✅ HA         → leader election (advisory lock) ✔failover  ⬜ hub distribuído (R5) · backup (R6)
✅ Segurança  → secrets manager + SSO/OIDC opt-in          ⬜ RBAC · mTLS agentes · SIEM
⬜ Operação   → OpenTelemetry ⭐ · zero-downtime · multi-ambiente · quotas
⬜ Qualidade  → E2E · carga · chaos · SLOs · reconciler de drift   (CI já roda build/vet/test)
```

---

## 🧪 Validação em infra real

Gate antes de contar como *production-ready*. **F1 e G1 validados em 2026-06-18**
contra Postgres 16 real (Docker); restante pendente:

```
✅ F1 Postgres    → server -db-driver postgres contra PG 16 real: migrations v1-3,
                    seed, alerts, CRUD round-trip e leader election OK (2026-06-18)
✅ G1 HA          → 2 nós no mesmo PG → 1 líder único (sem split-brain); kill do
                    líder → failover ~1s (advisory lock auto-liberado) (2026-06-18)
⬜ R1–R3          → supervisor + liveness validados num restart/chaos real
⬜ H1 SSO/OIDC    → fluxo completo com IdP real (Keycloak/Cognito/Google) + SPA lê #token
⬜ Secrets        → resolver github_token/webhook_secret via provider (env/-secrets-file)
⬜ SSH · agente   → host com sshd; agente instalado como serviço (systemd / Task Windows)
```

> 📌 **Nota (persistência de config no restart):** verificar/garantir que **descer e subir o
> servidor** (restart, container efêmero, failover HA) **não perde as configurações de runtime**
> — GitHub token, webhook secret e label de ambiente (hoje em `settings`/SQLite). Cobrir: DB/estado
> fora do container efêmero, backup/migração da tabela `settings`, e checklist de upgrade
> zero-downtime. Liga-se a R1/R4 (supervisor + persistência) e R6 (DR/backup).

> Reprodução F1+G1: `docker run -d --name regente-pg -e POSTGRES_USER=regente
> -e POSTGRES_PASSWORD=regente -e POSTGRES_DB=regente_test -p 5432:5432 postgres:16-alpine`;
> `REGENTE_TEST_PG_DSN=postgres://regente:regente@localhost:5432/regente_test?sslmode=disable
> go test ./internal/db -run Postgres -v`; subir 2 servers (`-db-driver postgres -db <dsn>`,
> portas distintas) → só 1 loga "assumi a liderança"; matar o líder → o outro assume em ~1s.

---

## 🎯 Próximos movimentos de maior valor

| ⭐ | Frente | Por quê |
|----|--------|---------|
| **1** | **R1 — Server supervisionado** | Fecha o "caiu e ficou caído": systemd/Windows Service/livenessProbe. Barato, alto impacto. |
| **2** | **R2 — Panic-recovery + watchdog** | Um job ruim nunca pode derrubar o orquestrador. Contido e testável. |
| **3** | **R5 — Adapter NATS + hub distribuído** | Failover não estranda agentes + destrava escala real multi-nó. |
| 4 | **Observabilidade (OpenTelemetry)** | Tracing distribuído = table stakes enterprise. |
| 5 | **Adapters de nuvem por capability** (AWS/GCP/k8s) | Cada nuvem vira plugin, não fundação. |

---

## ⬜ Features avançadas *(depois do núcleo sólido)*

- Job types com schema dedicado · Multi-ambiente/multi-site · What-If/Forecast/Statistics
- MFT (FILE_TRANSFER nativo) · Archives/Retention · Import de Control-M · CLI/SDK · site de docs
- **Executores AWS** (Lambda/Batch/Glue/Step) — como adapters por capability, item tardio

## 🏁 Fase Z — ÚLTIMO GATE

Case study técnico + post LinkedIn — **só com tudo sólido**, incluindo a trilha
Resiliência (um orquestrador que cai e não volta sozinho não vira post).
