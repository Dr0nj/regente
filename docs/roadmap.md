# 🎼 Regente — Roadmap

> Documento vivo · revisão 2026-06-18 (F1 Postgres + G1 HA validados em PG real).
> Estratégia de arquitetura em [`arquitetura-futuro.md`](arquitetura-futuro.md);
> detalhe de produto no [`../README.md`](../README.md).

## 📊 Visão geral

```
Núcleo / Control-M     ██████████████████████  100%  ✅ pronto
Alerting               ██████████████████████  100%  ✅ multi-canal + por-regra
Serverless portátil    ████████████████░░░░░░   70%  🟡 funcional ponta-a-ponta
Enterprise readiness   ████████████░░░░░░░░░░   55%  🟡 F1+G1 validados em PG real
```

Legenda: ✅ pronto · 🟡 em andamento · ⬜ a fazer · ⭐ recomendado

---

## 🟢 Fundação — *pronta*

```
✅ GitOps (Publish · webhook · drift · deep-links · PAT via UI)
✅ Paridade Control-M (calendars · resources · conditions · vars · SLA · forecast)
✅ Daily imutável · dependências com condições · Force Order
✅ Executores: COMMAND · SCRIPT · HTTP · SSH agentless · WASM
✅ Stream stdout/stderr · retry · /metrics Prometheus
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

## ☁️ Serverless portátil *(sem lock-in)*

```
✅ Fase 1 · gatilho externo   → Tick() · -scheduler=external · deploy/ (Knative) ✔e2e em container
✅ Fase 2 · transporte         → Bus + HTTP long-poll (-transport=http)
✅ Fase 3 · executor WASM      → wazero, pure-Go, sandbox WASI
─────────────────────────────────────────────────────────────────
⬜ NATS + hub WebSocket distribuído        (escala multi-nó)   ⭐
⬜ Adapters de nuvem por capability        (AWS · GCP · k8s Jobs)
⬜ Durable execution (Temporal / Restate)  (opt-in)
⬜ Postgres-como-fila (SKIP LOCKED)
```

## 🏢 Enterprise readiness

```
✅ Escala     → Postgres plugável + migrations ✔validado  ⬜ stateless · 10k+ jobs/dia · DR
✅ HA         → leader election (advisory lock) ✔failover  ⬜ hub distribuído · backup
✅ Segurança  → secrets manager + SSO/OIDC opt-in          ⬜ RBAC · mTLS agentes · SIEM
⬜ Operação   → OpenTelemetry · zero-downtime · multi-ambiente · quotas
⬜ Qualidade  → E2E · carga · chaos · SLOs · reconciler de drift
```

## 🧪 Validação em infra real

Gate antes de contar como *production-ready*. **F1 e G1 validados em 2026-06-18**
contra Postgres 16 real (Docker); restante pendente:

```
✅ F1 Postgres    → server -db-driver postgres contra PG 16 real: migrations v1-3,
                    seed, alerts, CRUD round-trip e leader election OK (2026-06-18)
✅ G1 HA          → 2 nós no mesmo PG → 1 líder único (sem split-brain); kill do
                    líder → failover ~1s (advisory lock auto-liberado) (2026-06-18)
⬜ H1 SSO/OIDC    → fluxo completo com IdP real (Keycloak/Cognito/Google) + SPA lê #token
⬜ Secrets        → resolver github_token/webhook_secret via provider (env/-secrets-file)
⬜ SSH · agente   → host com sshd; agente instalado como serviço (systemd / Task Windows)
```

> Reprodução F1+G1: `docker run -d --name regente-pg -e POSTGRES_USER=regente
> -e POSTGRES_PASSWORD=regente -e POSTGRES_DB=regente_test -p 5432:5432 postgres:16-alpine`;
> `REGENTE_TEST_PG_DSN=postgres://regente:regente@localhost:5432/regente_test?sslmode=disable
> go test ./internal/db -run Postgres -v`; subir 2 servers (`-db-driver postgres -db <dsn>`,
> portas distintas) → só 1 loga "assumi a liderança"; matar o líder → o outro assume em ~1s.

---

## 🎯 Próximos movimentos de maior valor

| ⭐ | Frente | Por quê |
|----|--------|---------|
| **1** | **Adapter NATS** + hub distribuído | destrava escala real multi-nó no caminho serverless |
| **2** | **Adapters de nuvem por capability** (AWS/GCP/k8s) | aderência + adoção; cada nuvem vira plugin, não fundação |
| **3** | **Observabilidade** (OpenTelemetry) | tracing distribuído = table stakes enterprise |
