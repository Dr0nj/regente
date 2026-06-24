# 🎼 Regente — Roadmap

> Documento vivo · revisão 2026-06-22.
> Estratégia de arquitetura em [`arquitetura-futuro.md`](arquitetura-futuro.md);
> detalhe de produto no [`../README.md`](../README.md).

## 📊 Visão geral

```
Núcleo / Control-M        ██████████████████████ 100%  ✅ pronto
Identidade visual / UI    ██████████████████████ 100%  ✅ logo, topbar, 13 temas, login vídeo, sidebars
Alerting                  ██████████████████████ 100%  ✅ multi-canal + por-regra
Serverless portátil       ██████████████████████ 100%  ✅ Knative/WASM/NATS/k8s ✓ · k8s e2e REAL ✓ · AWS/GCP (código + mock; conta paga fora de escopo)
Enterprise readiness      ██████████████████████ 100%  ✅ RBAC/mTLS/SIEM/OTel/SLOs · SSO+carga REAIS · zero-downtime · multi-env · quotas-HA · drift · escala 100k–1M end-to-end (write+read+UI)
Escala Control-M (100k–1M) ██████████████████████ 100%  ✅ P1 write-path 1M (17s) · P2 API paginada/contadores (51/18ms) · P3 UI ViewPoint validado AO VIVO @1M
Resiliência operacional   ██████████████████████ 100%  ✅ R1–R7 ✓ + chaos/HA validado em PG real
```

> 🏁 **Marco (2026-06-24):** **todas as trilhas estruturais em 100%**, incluindo **Escala Control-M (100k–1M/dia)
> end-to-end**: write-path materializa **1M em 17s** (P1), read-path serve **summary 51ms / page 18ms @100k**
> (P2), e a **UI por ViewPoint server-driven foi validada AO VIVO com 1.000.000 de jobs** (P3) — dashboard
> instantâneo, folder aberta em ~39ms, lista virtualizada, sem nunca baixar o dia inteiro.

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
⬜ Layout de jobs SEM dependência — hoje (como no Control-M) jobs sem conexão vão empilhando pra DIREITA na
   horizontal; ao passar de N por linha fica ruim de ver (tudo esticado pro lado). Regra: ao atingir um limiar
   de jobs soltos numa folder, QUEBRAR pra baixo em GRADE (wrap em linhas), em vez de só horizontal. N
   configurável; vale pro canvas (Monitoring + Design). Mantém os conectados no fluxo; só os soltos viram grade.
⬜ Minimap REVISTO — repensar o `NavMinimap` (hoje desenha pontos de `canvas.nodes`): comportamento/usabilidade
   com a nova grade de jobs soltos + volume alto; precisa refletir o layout em wrap, navegação clara e densidade
   legível (avaliar viewport-box arrastável, escala por densidade, on/off por contexto).
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
✅ Adapters de nuvem por capability  → k8s Jobs ✔ (`-caps K8S`, jobType `K8S_JOB`) **VALIDADO em cluster REAL**
        (kind v1.36, 2026-06-23: Job real criado → kubelet rodou busybox → succeeded/failed lidos de volta;
        prova nos pod logs) · AWS Lambda ✔ (`LAMBDA`, Invoke API + SigV4 stdlib) · GCP Cloud Run Jobs ✔
        (`GCP_RUN`, Run Admin API v2 + Bearer) — AWS/GCP com código + e2e mock (httptest); validação em conta
        paga FORA DE ESCOPO por decisão (não vamos por cartão), o mesmo seam por capability já provado real no k8s
○ Extensões futuras opt-in (não bloqueiam): Durable execution (Temporal/Restate) · Postgres-como-fila (SKIP LOCKED)
```

## 🏢 Enterprise readiness

```
🟡 Escala     → Postgres plugável + migrations ✔ · stateless (estado durável externo; só o líder agenda) ·
                 **write-path 1M/dia** (P1: lote, 1M em 17s) ✓ · **read-path paginado/filtrado** (P2: /page +
                 /summary + `team` na instance, RBAC por conjunto — 51ms/18ms @100k) ✓ — ver §Escala Control-M
                 ⬜ só falta a UI por ViewPoint server-driven (P3) p/ OPERAR 100k+ na tela
✅ HA         → leader election (advisory lock) ✔failover · hub distribuído (R5) ✔ · backup/DR (R6) ✔ ·
                 **quotas sobrevivem a failover** (RebuildResourcesFromRunning reconstrói o tracker do RUNNING)
✅ Segurança  → secrets · SSO/OIDC opt-in · RBAC/ACL (roles + por-folder) · mTLS opt-in (`-tls-client-ca`,
                 verifica cert; e2e handshake) · audit→SIEM (eventos JSON em stderr + POST `-audit-siem-url`)
                 · **SSO e2e VALIDADO com Keycloak REAL** (2026-06-23: Authorization Code ponta-a-ponta →
                 user provisionado → sessão federada autentica a API)
✅ Operação   → OpenTelemetry ✔ (opt-in OTLP) · **zero-downtime VALIDADO** (rolling-upgrade.sh: novo sobe
                 follower → drena o líder → assume em ~4s, API nunca cai) · **multi-ambiente** (deployment por
                 branch+DB+env_label; `regente_env_info` no /metrics) · **quotas** (F15 + rebuild HA) — docs/operacao.md
✅ Qualidade  → E2E HTTP ✔ · chaos/HA ✔ (chaos-ha.sh + validado) · SLOs ✔ (docs/slos.md) · **carga REAL ✔**
                 (`hey` contra o binário: /readyz 50k reqs/100conc → 11.6k req/s · /metrics 7.9k req/s · 0 erros)
                 · **reconciler de drift** (`-drift-reconcile-sec`: líder alerta pelos canais do R7 ou auto-sync)
```

## 📈 Escala Control-M (100k–1M jobs/dia)

> O Control-M roda rotineiramente 100k–1M+ jobs/dia; se a UI/engine engasga em 10k, nenhum
> cliente grande adota. O estado durável (linhas) escala trivialmente — o que precisa escalar é
> o **caminho de leitura/escrita**, que foi escrito assumindo "o dia inteiro cabe na memória e
> numa tela". Three fases:

```
✅ P1 · WRITE PATH (materialização) — VALIDADO até 1M (2026-06-23)
        Antes: O(N) round-trips (COUNT por def + INSERT autocommit por linha + evento por instance) → 8.8s/10k.
        Agora: existência set-based (1 query) + gating em memória + INSERT em LOTE por transação com prepared
        stmts (chunk de 5k, 1 commit/chunk). → 10k=182ms · 100k=1.7s · 1M=17.4s (~57k inst/s).
✅ P2 · READ PATH (API) — VALIDADO (2026-06-23). Antes: /api/instances devolvia o DIA INTEIRO num array +
        CanReadFolder POR LINHA p/ não-admin. Agora: coluna `team` denormalizada na instance (migration v4 +
        índices) → filtro server-side (folder/status/busca) + **paginação por cursor** (/api/instances/page,
        keyset estável scheduled_at,id) + **contadores agregados** (/api/instances/summary, GROUP BY) + **RBAC
        por CONJUNTO** (FilterReadableFolders uma vez, não por linha). Bench @100k: **summary 51ms · page(500)
        18ms** vs full-fetch do dia inteiro 491ms (e payload 500 linhas, não 100k). TestReadPath_Scale + 4 testes.
✅ P3 · UI por VIEWPOINT server-driven — VALIDADO AO VIVO com **1.000.000 jobs** (2026-06-24). Componente
        ScaleMonitor (toggle "ViewPoint" na topbar): dashboard do /summary (1M + por-status instantâneo) +
        lista de 200 folders (byFolder) + tabela VIRTUALIZADA por folder via /page (cursor). Nunca baixa o dia
        inteiro: abrir uma folder = 1ª página em ~39ms; DOM cai de 36.777→932 nós (legado capado + não montado
        sob o ViewPoint). O canvas legado (ReactFlow) ganhou cap de 2.000 (`LEGACY_CAP`) p/ nunca travar.
⬜ P3 · UI por ViewPoint server-driven — o front hoje BAIXA todas as instances e joga no ReactFlow sem
        virtualização. Fazer: Monitoring carrega só um working set filtrado/paginado (o ViewPoint do Control-M
        já está no backlog) + virtualização do que renderiza + dashboard/contadores pro total. Ninguém renderiza
        1M nós — o modelo é mostrar centenas, guardar milhões.
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
✅ k8s adapter REAL (2026-06-23) → cluster kind v1.36 real: adapter `K8S_JOB` criou um Job de verdade →
                    kubelet rodou busybox (pod log "hello-from-regente") → succeeded; Job de `exit 7` → failed;
                    token inválido → erro na criação. Teste: agent/k8s_integration_test.go (env-gated)
✅ H1 SSO/OIDC REAL (2026-06-23) → Keycloak 26 real (realm regente, client confidencial): Authorization Code
                    ponta-a-ponta (/oidc/login → authorize → login form → callback → troca code→token→userinfo)
                    → user 'thiago' provisionado (LoginFederated) → #token no SPA → sessão autentica a API.
                    Teste: server/internal/api/oidc_integration_test.go (env-gated)
✅ Carga REAL (2026-06-23) → `hey` contra o binário compilado (TCP real, não in-process): /readyz 50k reqs /
                    100 conc → 11.6k req/s, p99 34ms, 0 erros · /metrics 30k → 7.9k req/s, 0 erros
✅ Zero-downtime REAL (2026-06-23) → rolling-upgrade.sh contra PG real: nó novo sobe follower → READY →
                    drena o líder → novo assume em ~4s, /livez do novo NUNCA caiu (API disponível na troca)
✅ Escala write-path (2026-06-23) → RunDaily materializa em LOTE (existência set-based + insert por transação
                    com prepared stmts): **10k em 182ms · 100k em 1.7s · 1M em 17.4s** (~57k inst/s, SQLite gravando
                    instances+eventos). Antes (autocommit por linha + COUNT por def): 8.8s/10k (~15min p/ 1M).
                    Idempotente — TestScale_RunDaily10k + TestScale_BenchmarkN (env REGENTE_SCALE_N)
✅ Quotas HA (2026-06-23) → RebuildResourcesFromRunning reconstrói o tracker a partir das instances RUNNING →
                    novo líder não fura a capacidade ao retomar o dispatch — TestQuotas_RebuildFromRunning
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
| ✅ | ~~Adapters AWS/GCP~~ | **Feito (2026-06-23):** AWS Lambda (SigV4) + GCP Cloud Run, por capability, testados (httptest). |
| ✅ | ~~Segurança — RBAC/ACL · mTLS · audit→SIEM~~ | **Feito (2026-06-23):** mTLS opt-in + RBAC travado + audit→SIEM (JSON/HTTP). |
| ✅ | ~~Qualidade — E2E/carga/chaos/SLOs~~ | **Feito (2026-06-23):** E2E HTTP + smoke de carga + chaos-ha.sh + docs/slos.md. |
| ✅ | ~~e2e em infra REAL — k8s · SSO · carga~~ | **Feito (2026-06-23):** k8s em cluster kind REAL · SSO ponta-a-ponta com Keycloak REAL · carga REAL (`hey`, 11.6k req/s). |
| ✅ | ~~Enterprise — operação · quotas · drift · zero-downtime~~ | **Feito (2026-06-23):** zero-downtime validado · quotas-HA · multi-ambiente · reconciler de drift. |
| ✅ | ~~Escala P1 — write-path (materialização)~~ | **Feito (2026-06-23):** daily em lote → **1M em 17s** (~57k inst/s); era ~15min. TestScale_BenchmarkN. |
| ✅ | ~~Escala P2 — read-path (API paginada/filtrada + contadores)~~ | **Feito (2026-06-23):** /page (cursor) + /summary + `team` denormalizado + RBAC por conjunto → **51ms/18ms @100k** vs 491ms. |
| ✅ | ~~Escala P3 — UI por ViewPoint server-driven~~ | **Feito (2026-06-24):** ScaleMonitor (dashboard + folders + lista virtualizada) **validado AO VIVO @1M**; folder em ~39ms, DOM 36.777→932. |
| **1** | **Aprofundamento Control-M** — Actions/On-Do · variáveis runtime · ciclo de vida da daily | Backlog de paridade de maior impacto. |
| **2** | **Diferenciais** — Explain · Diff de Daily · Blast Radius · Dry Run | Onde o Regente passa o Control-M. |
| 3 | **Fase Z** — case study + post LinkedIn | Último gate, com tudo sólido. |

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
- **Executores AWS extras** (Batch/Glue/Step) — adapters por capability (Lambda já feito); validação em conta
  paga fora de escopo por decisão

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
