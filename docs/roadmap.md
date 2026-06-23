# 🎼 Regente — Roadmap

> Documento vivo · revisão 2026-06-22.
> Estratégia de arquitetura em [`arquitetura-futuro.md`](arquitetura-futuro.md);
> detalhe de produto no [`../README.md`](../README.md).

## 📊 Visão geral

```
Núcleo / Control-M        ██████████████████████ 100%  ✅ pronto
Identidade visual / UI    ██████████████████████ 100%  ✅ logo, topbar, 13 temas, login vídeo, sidebars
Alerting                  ██████████████████████ 100%  ✅ multi-canal + por-regra
Serverless portátil       ████████████████████░░  90%  🟡 Knative/long-poll/WASM/NATS/k8s/AWS/GCP ✓ · e2e real ⬜
Enterprise readiness      ██████████████░░░░░░░░  62%  🟡 F1/G1/secrets/OIDC/OTel ✓ · RBAC/mTLS/SIEM ⬜
Resiliência operacional   ██████████████████████ 100%  ✅ R1–R7 ✓ + chaos/HA validado em PG real
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
✅ Tela de login com o logotipo (paleta dark blue neon)
✅ Sidebars flutuantes + janela de info do job dockada (mesmo estilo do ACTIVE JOBS)
✅ Diálogos padronizados (✕ + Cancelar/Salvar via classes compartilhadas); Control Panel (ex Control-M)
✅ Monitoring: pan travado no topo (folders alinhadas com o ACTIVE JOBS; livre pros lados e pra cima)
✅ 13 temas (Escuro · Verde Amarelo · Amarelo Ouro · Verde Mata · Azul Neon · Azul Escuro ·
   Rosa · Violeta · Vermelho · Laranja · Cinza · Bege Escuro · Marrom) com swatch de cores
✅ Configurações em sub-abas (Geral · Temas); borda neon nos diálogos
◑ Minimap de navegação (protótipo opt-in, default off) — pontos por job, clique navega, redimensionável
⬜ Janela de info do job (drawer) — deixar mais friendly: ações claras, output/log legível, layout melhor
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

## ✅ Resiliência operacional *(trilha COMPLETA — R1–R7)*

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
✅ R1 · Server supervisionado          → systemd Restart=always + Windows Service +
        livenessProbe no Knative + server/deploy/ — ENTREGUE
✅ R2 · Panic-recovery + watchdog tick → recover() no dispatch/Tick/retry + idade do
        último tick em /metrics e /livez — ENTREGUE (testes)
✅ R3 · Health real                    → /readyz (ping DB = gate; líder + tick + último daily informativos),
        distinto de /livez; readinessProbe do Knative aponta aqui — ENTREGUE (testes)
✅ R4 · Config persiste no restart      → settings no DB durável (PG externo / volume p/ SQLite) + checklist
        em docs/dr-backup.md; teste TestSettings_SurviveRestart — ENTREGUE
✅ R6 · DR/backup (G3)                  → backup ONLINE portátil (`-backup` = VACUUM INTO no SQLite, in-process)
        + scripts backup.sh/restore.sh (pg_dump p/ PG) + runbook docs/dr-backup.md (PITR via WAL/gerenciado),
        validado e2e — ENTREGUE (testes)
✅ R7 · Auto-SLO do control plane      → monitor (`-selfmon`) alerta tick parado · DB inacessível · leader
        flapping · frota de agentes ↓ pelos MESMOS canais das regras (só o líder publica); gauge
        regente_is_leader em /metrics — ENTREGUE (testes + e2e)
```

## ☁️ Serverless portátil *(sem lock-in)*

```
✅ Fase 1 · gatilho externo   → Tick() · -scheduler=external · deploy/ (Knative) ✔e2e em container
✅ Fase 2 · transporte         → Bus + HTTP long-poll (-transport=http)
✅ Fase 3 · executor WASM      → wazero, pure-Go, sandbox WASI
─────────────────────────────────────────────────────────────────
✅ R5 · NATS + hub distribuído (-bus=nats)  → fan-out web + presença + dispatch roteado ao nó dono;
        VALIDADO 2-nós real (2026-06-22): job forçado no nó sem agente → roteado e executado
🟡 OpenTelemetry (I1) — tracing OTLP/HTTP opt-in (-otel-endpoint); otelhttp + spans no scheduler  ✅
◑ Adapters de nuvem por capability  → k8s Jobs ✔ (`-caps K8S`, jobType `K8S_JOB`) · AWS Lambda ✔
        (`LAMBDA`, Invoke API + SigV4 stdlib, sem SDK) · GCP Cloud Run Jobs ✔ (`GCP_RUN`, Run Admin API v2
        + Bearer OAuth) — todos testados contra API mock (httptest); ⬜ validar em cluster/conta REAL (e2e)
⬜ Durable execution (Temporal / Restate)  (opt-in)
⬜ Postgres-como-fila (SKIP LOCKED)
```

## 🏢 Enterprise readiness

```
✅ Escala     → Postgres plugável + migrations ✔validado  ⬜ stateless · 10k+ jobs/dia
✅ HA         → leader election (advisory lock) ✔failover · hub distribuído (R5) ✔  ⬜ backup (R6)
✅ Segurança  → secrets manager + SSO/OIDC opt-in          ⬜ RBAC · mTLS agentes · SIEM
🟡 Operação   → OpenTelemetry ✔ (opt-in OTLP)  ⬜ zero-downtime · multi-ambiente · quotas
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
✅ R1/R2          → supervisor (server/deploy/) + panic-recovery/watchdog ENTREGUES (testes unit)
✅ R5 NATS        → 2 nós + NATS reais (2026-06-22): job forçado no nó SEM agente → roteado via
                    NATS ao nó dono e executado (exitCode=0), sem estrandar agente
✅ R3 /readyz   → endpoint de readiness (ping DB = gate; líder/tick/último daily informativos) + testes (2026-06-23)
✅ Chaos/HA 2-nós real (2026-06-23) → 2 nós no MESMO PG (r3test): 1 líder único · matar o líder →
                    follower assume em ~4s (advisory lock) · DB parado → /readyz 503 (gate) e o processo
                    SEGUE vivo/ticando (livez≠readyz) · DB volta → 200 sozinho (~1s) · restart do nó morto
                    (R1) → rejoin como follower, sem split-brain
✅ R6/R4 backup+restart (2026-06-23) → `-backup` (VACUUM INTO) gerou snapshot; reabrir o snapshot devolveu
                    env_label=PROD-DR (config sobrevive a backup/restore E a restart) · scripts backup.sh/
                    restore.sh + runbook docs/dr-backup.md
✅ R7 auto-SLO (2026-06-23) → tick forçado a parar (modo external, limiar 3s) → monitor disparou alerta
                    control-plane "Scheduler tick parado" (critical) persistido em /api/alerts em ~6s
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

> Reprodução R5 (sem Docker): `go install github.com/nats-io/nats-server/v2@latest`; `nats-server -p 4222`;
> 2 servers `-bus=nats -nats-url nats://localhost:4222` em portas/`-node-id` distintos; agente conectado
> SÓ no nó A; force um COMMAND no nó B (sem agente) → evento `dispatched to agent:<id>` no nó B e
> `done exitCode=0` no log do agente (rodou no nó A). Persistência do resultado cross-nó requer o MESMO
> Postgres nos 2 nós (com SQLite separado, o resultado volta pro nó do agente — esperado).

---

## 🎯 Próximos movimentos de maior valor

| ⭐ | Frente | Por quê |
|----|--------|---------|
| ✅ | ~~Validar R1/R3 em chaos/HA 2-nós real~~ | **Feito (2026-06-23):** failover ~4s · /readyz gate em DB-down · rejoin sem split-brain. |
| ✅ | ~~R6 DR/backup + R4 config no restart~~ | **Feito (2026-06-23):** `-backup` online (VACUUM INTO) + scripts + runbook (PITR) · config sobrevive backup/restart. |
| **1** | **Adapters AWS/GCP** + validar **k8s** em cluster real | Cada nuvem vira plugin; o seam por capability já existe (k8s feito). |
| **2** | **Segurança** — RBAC/ACL · mTLS agentes · audit→SIEM · H1 SSO ponta-a-ponta | Requisitos enterprise de acesso e auditoria. |
| 3 | **Qualidade** — E2E/carga/chaos/SLOs | Confiança de produção; CI já roda build/vet/test. *(R7 auto-SLO ✓ entregue)* |

---

## 🧩 Aprofundamento Control-M *(testar a fundo + aprimorar)*

> O núcleo de paridade existe, mas precisa de **bateria de testes** cobrindo os casos reais do
> Control-M e refino onde faltar. Cada item = testar TODAS as possibilidades + fechar os gaps.

```
⬜ Calendários complexos — validar que o job entra na daily exatamente quando deve: 1º dia útil do mês ·
   só segundas · 1º dia útil que NÃO é segunda · N-ésimo dia útil · regras avançadas · include/exclude ·
   feriados · meses específicos. Cobrir todas as combinações; corrigir o gating onde divergir.
⬜ Controle de recursos — testar e aprimorar: quantitative (N slots), jobs que NÃO podem concorrer
   (lock exclusivo), máximo de jobs simultâneos por host/pool, fila quando esgota, liberação correta.
⬜ Actions / On-Do do job — motor de regras configuráveis por job, em 3 dimensões:
   (a) por Nº DE TENTATIVA (escada de rerun): configurar quantos retries (ex.: 4); "2º rerun → setar
       condition <evento>", "3º → alerta", "Nº → rodar outro job / set-ok / notificar";
   (b) por RESULTADO: OK / NOTOK → ação;
   (c) por TEMPO DE EXECUÇÃO ("shouts" estilo Control-M): rodando há >30min → shout no Slack · >40min →
       alerta · >1h → abre chamado via webhook tal; cada limiar com destino/ação própria (escalonamento por duração).
   Cada regra dispara: rerun · set-ok · notificar (Slack/webhook/e-mail/PagerDuty) · rodar outro job · setar
   condition · abrir chamado. Testar + expor a config por job na UI.
⬜ Job FILE_WATCH — espera a chegada de arquivo (path/glob · polling/evento · tamanho estável) antes de
   concluir; dispara o sucessor quando o arquivo chega. Novo jobType + capability.
⬜ Forecast — testar a previsão de ≥ 1 semana à frente (quais jobs rodam por dia, sem executar); validar
   contra o gating real (calendars + deps + conditions + recursos).
⬜ Ciclo de vida na daily (Keep Active / carry-over entre diárias):
   • RUNNING persiste (REGRA) — job EM EXECUÇÃO na virada da daily NÃO some: segue na daily até terminar,
     para o tracking da execução (jamais perder a instância no rollover).
   • Keep Active — opção no job: se NÃO executou com sucesso, sobrevive N diárias (keepActive=1 → +1 diária).
   • DEFAULT — job que termina NOTOK e NÃO é tratado persiste +1 diária (carry-over automático).
   • HOLD persiste — job em HOLD atravessa as diárias enquanto estiver em hold, independente do estado
     (OK/NOTOK/WAITING).
⬜ CONFIRM — config no job: precisa de ação MANUAL (Confirm) para sair do estado e prosseguir.
   (estudar o comportamento exato do Control-M antes de fechar a semântica do backlog.)
⬜ Job tipo DATABASE — plugin com conectores (JDBC e outros) p/ rodar SQL/procedure em bancos; corpo do
   job = selecionar procedure OU escrever SQL numa telinha PL/SQL amigável (editor). Novo jobType + capability.
⬜ ViewPoint (Monitoring) — viewpoints salvos/selecionáveis: mostrar SÓ certas folders (não todas) no
   Monitoring; filtro nomeado e persistido por usuário.
⬜ Dashboards prontos (ViewPoint de dashboard) — abrir um painel por folder(s) ou do AMBIENTE INTEIRO com
   gráficos (pizza e outros) e estatísticas em TEMPO REAL: jobs em execução / hold / waiting / confirm,
   total de execuções, total de jobs, end OK / failed / waiting, NOMES dos últimos jobs executados e dos
   últimos que deram erro, métricas por folder. Layout selecionável e persistido (estilo Control-M dashboards).
⬜ Mass Update / Find & Update (Design) — alteração em MASSA nas folders abertas por regex/critério de
   campo: buscar e substituir/adicionar em N jobs. Casos: descrição vazia → preencher; adicionar action/
   evento em TODOS / selecionados / que atendem critério; buscar string e substituir em qualquer campo;
   add/remove tag/condition/upstream em lote. Find & Update completo (busca + substituição + adição) com
   preview e undo, transacional por item. (bulk básico já existe via /api/bulk e /api/design/sessions/{sid}/bulk.)
⬜ Sistema de variáveis completo (estilo Control-M %%):
   • GLOBAIS de runtime — um job ATRIBUI valor e jobs posteriores LEEM (passagem entre jobs; hoje há globais
     interpoláveis via VariableStore, falta o SET em runtime por um job).
   • LOCAIS por job — escopo só do próprio job.
   • NATIVAS/sistema — %DataAtual · %DiaAtual · %AnoAtualYYYY · %MesAtual · último dia do mês · dia útil…
   • CÁLCULO de datas com template — aritmética sobre variáveis de data (ex.: %DiaAtual+3) resolvendo p/
     data numérica, ciente de dia útil/feriado/calendar. Ex.: arquivo gerado na sexta com a data de segunda
     no nome → %DiaAtual+3 = sexta + 3 dias → display da próxima data útil.
   • Interpolação em QUALQUER campo string (command/url/path/body) + inspetor de resolução por instância.
```

## ⬜ Features avançadas *(depois do núcleo sólido)*

- Job types com schema dedicado · Multi-ambiente/multi-site · What-If/Forecast/Statistics
- MFT (FILE_TRANSFER nativo) · Archives/Retention · Import de Control-M · CLI/SDK · site de docs
- **Executores AWS** (Lambda/Batch/Glue/Step) — como adapters por capability, item tardio

## 🌟 Diferenciais — além do Control-M *(visão de produto)*

> Não é paridade — é onde o Regente **passa** o Control-M. Visão de longo prazo; entra
> depois do núcleo sólido. Organizado por tema.

### 1. Orquestração híbrida e stateful *(grande gap do Control-M)*
```
⬜ Human-in-the-loop nativo + long-running — workflows que duram dias/semanas
   (ex.: aprovação manual + retry após 3 dias)
⬜ Pausa/resume com ESTADO preservado (além de Hold)
⬜ Event-driven confiável — reage a eventos externos de forma confiável (não só polling)
```

### 2. Observabilidade e análise avançada
```
⬜ Job Neighborhood + Impact Analysis — clique no job → grafo de impacto up/downstream;
   "se esse job atrasar, quais SLAs quebram?"
⬜ Root Cause Analysis automático — sugere causas por histórico
   ("80% das falhas ocorrem quando o job X roda ao mesmo tempo")
⬜ Performance forecasting com gráficos no Monitoring
⬜ Diff de Daily — comparar a daily de HOJE vs ONTEM (ou vs qualquer order_date): jobs adicionados (+) /
   removidos (-), schedule mudou, dependência mudou, mudança de def. Aproveita o DNA Git-native (cada
   instância carrega o commit_sha + snapshot da def) → diff EXATO e barato. Forte diferencial.
⬜ Blast Radius — "se eu CANCELAR/segurar este job AGORA, qual o impacto?": N jobs downstream deixarão de
   executar · X SLAs serão violados · tempo estimado de atraso. Análise de impacto de uma AÇÃO (cancel/hold),
   não só do grafo estático. Ex.: cancelar PIX_ENVIO → "37 jobs não executam · 3 SLAs violados · atraso ~2h15".
⬜ Dry Run — simular uma daily FUTURA (ex.: 25/12/2026) SEM criar instances: quem roda · quem espera ·
   quem NUNCA dispara. (o forecast já existe; falta o modo "data futura + razões por job + sem materializar".)
⬜ Explain ("por que o job não rodou?") — motor de EXPLICAÇÃO (sem IA): WAIT_RESOURCE (qual recurso, quem
   consome, slots livres ex. 0/3) · WAIT_CONDITION (qual condition falta, job produtor, última execução/status)
   · deps não satisfeitas · fora da janela · calendar. O scheduler já computa o gating — falta expor o PORQUÊ
   por instância. Mata a pergunta nº1 de quem opera Control-M.
⬜ Event log de primeira classe (CQRS-lite — NÃO Event Sourcing puro) — evoluir o `instance_events` (já é a
   semente) para um log COMPLETO e CONFIÁVEL: emissão TRANSACIONAL (evento + mutação de estado no mesmo
   commit), sequência global, + tipos que faltam (DailyCreated, ConditionAdded/Removed…). O estado mutável
   segue como PROJEÇÃO (claim atômico intacto). Destrava replay / time-travel / forense ("estado às 08:14")
   e outbox → NATS/observabilidade, SEM reescrever o core de correção. (ES puro foi avaliado e rejeitado por
   ROI/risco: HA/DR/auditoria/histórico-de-config já cobertos por Postgres+leader · PITR · instance_events · Git.)
```

### 3. Developer Experience *(onde o Control-M perde feio)*
```
⬜ Schedule as Code completo — YAML + DSL Go/Python no repo, sincronizado com a UI
⬜ Testing framework — `regente test job.yaml` simula execução com mocks
⬜ Local development mode — `regente dev daily` roda a daily local (mock de agents/datas)
```

### 4. Enterprise & operação *(além do que já existe)*
```
⬜ Multi-environment promotion (Dev → Staging → Prod) com Git flow nativo
⬜ Policy as Code — regras obrigatórias (todo job tem SLA / retry / owner…)
⬜ Chaos engineering — botão "Inject Failure" pra testar resiliência de workflows
```

### 5. Features "wow" menores mas impactantes
```
⬜ Visual Schedule Editor com timeline (Gantt-like) da daily
⬜ Bulk Schedule Actions + templates reutilizáveis
⬜ Self-Service Portal — negócio roda jobs aprovados sem tocar no Design
⬜ Mobile-friendly alerts com ações rápidas (rerun, set-ok)
```

---

## 🏁 Fase Z — ÚLTIMO GATE

Case study técnico + post LinkedIn — **só com tudo sólido**, incluindo a trilha
Resiliência (um orquestrador que cai e não volta sozinho não vira post).
