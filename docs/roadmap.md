# 🎼 Regente — Roadmap

> Documento vivo · revisão 2026-06-18 (validação de infra real destacada).
> Estratégia de arquitetura em [`arquitetura-futuro.md`](arquitetura-futuro.md);
> detalhe de produto no [`../README.md`](../README.md).

## 📊 Visão geral

```
Núcleo / Control-M     ██████████████████████  100%  ✅ pronto
Alerting               █████████████████████░   95%  ✅ + routing externo
Serverless portátil    ████████████████░░░░░░   70%  🟡 funcional ponta-a-ponta
Enterprise readiness   ███████████░░░░░░░░░░░   50%  🟡 base sólida, faltam gaps
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
✅ Routing externo  → Slack / webhook genérico (sinks globais configuráveis)
⬜ Routing por regra na UI · e-mail (SMTP) · PagerDuty Events API
```

## ☁️ Serverless portátil *(sem lock-in)*

```
✅ Fase 1 · gatilho externo   → Tick() · -scheduler=external · deploy/ (Knative)
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
✅ Escala     → Postgres plugável + migrations      ⬜ stateless · 10k+ jobs/dia · DR
✅ HA         → leader election (advisory lock)      ⬜ hub distribuído · backup
✅ Segurança  → secrets manager + SSO/OIDC opt-in    ⬜ RBAC · mTLS agentes · SIEM
⬜ Operação   → OpenTelemetry · zero-downtime · multi-ambiente · quotas
⬜ Qualidade  → E2E · carga · chaos · SLOs · reconciler de drift
```

## 🧪 Validação pendente *(codado + teste unitário; falta exercitar em infra real)*

Os itens enterprise acima estão implementados e cobertos por teste unitário, mas
**ainda não foram validados ponta-a-ponta contra infraestrutura real** — gate
antes de contar como *production-ready*:

```
⬜ F1 Postgres    → subir server com -db-driver postgres contra um PG real
⬜ G1 HA          → 2 nós no mesmo PG → 1 líder; matar líder → failover em ~5s
⬜ H1 SSO/OIDC    → fluxo completo com IdP real (Keycloak/Cognito/Google) + SPA lê #token
⬜ Secrets        → resolver github_token/webhook_secret via provider (env/-secrets-file)
⬜ SSH · agente   → host com sshd; agente instalado como serviço (systemd / Task Windows)
```

---

## 🎯 Próximos movimentos de maior valor

| ⭐ | Frente | Por quê |
|----|--------|---------|
| **1** | **Adapter NATS** + hub distribuído | destrava escala real multi-nó no caminho serverless |
| **2** | **Adapters de nuvem por capability** (AWS/GCP/k8s) | aderência + adoção; cada nuvem vira plugin, não fundação |
| **3** | **Observabilidade** (OpenTelemetry) | tracing distribuído = table stakes enterprise |
