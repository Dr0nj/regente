# 🎼 Regente — Roadmap

> **Fonte única de status (single source of truth).** Este arquivo é a verdade sobre
> "o que está pronto / o que falta". O roadmap do [`../README.md`](../README.md) é só um
> **resumo** que aponta pra cá — se algo divergir, ESTE arquivo vence. Ao entregar algo,
> atualize AQUI (a barra da trilha + a seção "O que falta" + o changelog "Próximos
> movimentos"); o README carrega apenas destaques de 1 linha.
>
> Documento vivo · revisão **2026-07-02**.
> Estratégia de arquitetura em [`arquitetura-futuro.md`](arquitetura-futuro.md);
> detalhe de produto no [`../README.md`](../README.md).

## 📊 Visão geral

```
── Trilhas estruturais (FECHADAS) ──────────────────────────────────────────
Núcleo / Control-M         ██████████████████████ 100%  ✅ pronto
Identidade visual / UI     ██████████████████████ 100%  ✅ logo, topbar, 13 temas, login vídeo, sidebars
Alerting                   ██████████████████████ 100%  ✅ multi-canal + por-regra
Serverless portátil        ██████████████████████ 100%  ✅ Knative/WASM/NATS/k8s ✓ · AWS/GCP (código+mock)
Enterprise readiness       ██████████████████████ 100%  ✅ RBAC/mTLS/SIEM/OTel/SLOs · SSO+carga REAIS · zero-downtime · quotas-HA · drift
Escala Control-M (100k–1M) ██████████████████████ 100%  ✅ P1 write 1M/17s · P2 API 51/18ms · P3 ViewPoint @1M
Resiliência operacional    ██████████████████████ 100%  ✅ R1–R7 + chaos/HA validado em PG real

── Próximas fases ──────────────────────────────────────────────────────────
Agent-native (MCP)         █████████████████░░░  85%  🟢 servidor MCP ✅ (6 tools read + 2 write gated) · falta NL-query + writes ricos
Diferenciais               ███████████████░░░░░  75%  🟡 Explain·Diff·Blast·Dry Run·**Job Neighborhood·RCA·Event log·NL-query** ✅ · falta orquestração híbrida/stateful · DevEx · promotion · policy-as-code · "wow"
Aprofundamento Control-M   ██████████████████████ 100%  ✅ TRILHA FECHADA: daily lifecycle · Actions/On-Do (motor + UI) · daily server-side configurável · WAIT EVENT · WAIT AGENT · variáveis %% · FILE_WATCH · calendários+shift · condition vazia=on-success · **cyclic runtime** · **CONFIRM** · **DATABASE** (Postgres/MySQL/SQLite) · **SET de var em runtime + cálculo de datas %%ODATE±NB** · **janela fechada (WINDOW_CLOSED)** · baterias de teste
Refinamento UI             ██████████████████░░  88%  🟡 grade de jobs soltos ✅ · aba de agentes+ping ✅ · seletor de folder (FOLDERS) redesenhado ✅ + fix snapshot do monitoring ✅ · fix botão "Abrir"+auto-nav ✅ · fix bug "Nenhuma definition" no drag ✅ · Job Name/ID no drawer ✅ · painel Edit Job flutuante arredondado ✅ · lista de jobs da folder na sidebar ✅ · minimap revisto (jobs quadrados + viewport rect) ✅ · viewport fluido (sem sumir/pular no Run Daily/Force/refresh) ✅ · câmera consistente com a trava de pan + board sem F5 ✅ · anti-race de refresh no store ✅ · draft do Design à prova de F5 (retomada de sessão) ✅ · limpeza de ruído ✅ · falta: LEGACY_CAP virtualizado · drawer do job mais amigável
Fase Z — divulgação        ░░░░░░░░░░░░░░░░░░░░░   0%  ⬜ case study + post LinkedIn (agora com a história agent-native)
```

> 🏁 **Marco (2026-06-24):** **todas as trilhas estruturais em 100%**, incluindo **Escala Control-M (100k–1M/dia)
> end-to-end**: write-path materializa **1M em 17s** (P1), read-path serve **summary 51ms / page 18ms @100k**
> (P2), e a **UI por ViewPoint server-driven foi validada AO VIVO com 1.000.000 de jobs** (P3) — dashboard
> instantâneo, folder aberta em ~39ms, lista virtualizada, sem nunca baixar o dia inteiro.

Legenda: ✅ pronto · 🟡 em andamento · ⬜ a fazer · ⭐ recomendado · 🔴 prioridade

---

## 🎯 O que falta (lista autoritativa)

> Pergunta "o que falta?" → responde aqui. Tudo abaixo é o que **ainda não** está pronto;
> o que não aparece aqui está entregue (detalhe nas seções e no changelog "Próximos
> movimentos"). Todas as trilhas ESTRUTURAIS (núcleo · UI · alerting · resiliência ·
> serverless · enterprise · escala 100k–1M) estão **100%**.

**Aprofundamento Control-M** — ✅ **TRILHA FECHADA (2026-07-03).** cyclic runtime ·
CONFIRM · Job DATABASE (Postgres/MySQL/SQLite) · SET de var em runtime + cálculo de
datas (`%%ODATE±NB` ciente de dia útil) · janela fechada (WINDOW_CLOSED) · ViewPoints
salvos · Find & Update estendido — todos entregues com testes + validação ao vivo.
- ⬜ **Dashboards prontos** (presets do /summary na UI) — só o "wow" de apresentação
  falta; o backing store (ViewPoints) já existe. Fica como polimento da Fase Z.

**Refinamento UI:**
- ⬜ **Virtualizar a sidebar ACTIVE JOBS** (`LEGACY_CAP=2000` engana; o ViewPoint já faz 100k–1M).
- ⬜ **Drawer de info do job** mais amigável (ações claras, log legível).

**Enterprise (specs prontas — §Backlog Enterprise E1..E6):**
- ⬜ E1 timezone da daily · E2 auditoria (retenção/export/audit de settings) · E3 RBAC por
  ação operacional · E4 fila assíncrona de eventos · E5 relatório/SLO da daily · E6 importador Control-M.

**Camada agent-native (MCP)** — 85%: falta writes ricos (NL-query ✅ entregue como tool `query`).

**Diferenciais (além do Control-M)** — 75%: Explain·Diff·Blast·Dry Run·**Job Neighborhood·RCA·Event
log CQRS-lite·NL-query** ✅ (todos read-only, endpoint + MCP + testes + ao vivo); falta orquestração
híbrida/stateful · DevEx (schedule-as-code / `regente test` / `regente dev daily`) · promotion ·
policy-as-code · chaos inject · "wow" (Gantt · templates · self-service · mobile).

**🏁 Fase Z** — case study + post LinkedIn. **Último gate por definição** — só com o backlog acima onde você quer.

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
✅ Pan travado (Monitoring E Design) — entra ancorado um pouco abaixo do topo (TOP_ANCHOR=88, igual ao
   "Organizar", padrão pedido nos 2 modos). Monitoring: livre pros lados/cima, nunca abaixo do topo.
   Design (2026-06-29): BOUNDED na caixa dos jobs da folder + margem → só puxa pros lados quando os jobs
   passam da tela, com LIMITE pra não "se perder" no vazio. `panExtent`/`TOP_ANCHOR` em V2Preview.
✅ 13 temas (Escuro · Verde Amarelo · Amarelo Ouro · Verde Mata · Azul Neon · Azul Escuro ·
   Rosa · Violeta · Vermelho · Laranja · Cinza · Bege Escuro · Marrom) com swatch de cores
✅ Configurações em sub-abas (Geral · Temas); borda neon nos diálogos
◑ Minimap de navegação (protótipo opt-in, default off) — pontos por job, clique navega, redimensionável
✅ Aba Schedule do job redesenhada (2026-06-29) — `ScheduleEditor`: (1) SAÍRAM "dia útil" e "regra avançada"
   da frequência (dia útil depende de feriados/calendário de cada lugar; o Regente não adivinha) → ficam só
   daily/weekly/monthly; (2) os CALENDÁRIOS entraram na própria aba (fundiu a aba "Calendars" separada) e
   trabalham JUNTO com as regras como include/exclude, cada calendário anexado já mostra a tradução do que ele
   faz (ex.: "seg–sex, menos 3 feriados"); (3) PREVIEW = gist em linguagem natural + **CALENDÁRIO REAL** estilo
   Control-M "View Scheduling" (12 mini-meses, seletor de ano) onde os dias DESTACADOS são EXATAMENTE os que o
   job roda — vindos do backend `POST /api/schedule/preview` → `Scheduler.SchedulePreview` → `IsScheduledOn`
   (a MESMA regra da daily, fonte única; destacado=roda, apagado=não roda). 4 testes Go + endpoint validado
   ao vivo. Aba "Calendars" do drawer removida. (browser-mode sem server: mostra aviso, calendário exato exige server.)
⬜ Janela de info do job (drawer) — deixar mais friendly: ações claras, output/log legível, layout melhor
✅ Design: LISTA de labels dos jobs da folder (ENTREGUE 2026-07-01) — a aba Folders da `DesignSidebarV2`
   lista os jobs de cada folder aberta como linhas clicáveis (`JobRow`, hover destaca) que navegam/
   centralizam o canvas no nó (`onJobClick`→`focusNode`), pra localização rápida sem caçar nó por nó.
✅ Seletor de folder do Design REDESENHADO (ENTREGUE 2026-06-30) — removido o botão flutuante "open/create
   folder" (FolderOpener deletado). O botão único "FOLDERS" abre o modal (FolderManagerDialog) que agora
   CONTROLA TUDO: criar nova folder ("+ New folder", força PR no Publish via design-session), abrir/fechar
   uma OU VÁRIAS folders no canvas (multi-select via toggle "Open/Close in design" → activeFolders, badge
   OPEN + borda destacada), além de visibilidade/rename/archive/delete já existentes. Semântica de
   design-session+PR preservada (open/create routam pelo V2Preview.addFolder; close só tira da visão).
   BUG corrigido junto: o snapshot do Monitoring perdia o nome da folder quando o job era apagado do Design.
   Causa: `server-instance-store.toWeb` hardcodava `team: undefined` e o canvas recuperava a folder da
   definition VIVA (`inst.team || def.team`) — def deletada ⇒ lane "—" sem nome. Fix: a instância carrega o
   `team` congelado que o server já grava no INSERT (a API /api/instances já devolvia); apagar/mover job no
   Design não reescreve mais a daily corrente.
   ✅ PASSO 2 (2026-06-30, commit `c5e3db2`): redesign visual "Luxury Dashboard" (a partir de mock HTML do
      usuário) — fundo ultra-dark × accent do tema, títulos SERIF (novo token `--v2-font-serif` = Playfair,
      Inter+Playfair no index.html), cards de "ativo serializado" (ID REG-xxxx + LED Active/Idle + badge
      OPEN), hover que eleva, MULTI-SELEÇÃO com action bar flutuante (Abrir/Fechar/Arquivar/Excluir em lote),
      stats macro REAIS no topo (total/jobs/abertas/arquivadas). 100% theme-driven (o "dourado" = accent do
      tema; NADA hardcoded → 13 temas funcionam). Validação visual no lab do usuário.
◑ Layout de jobs — grade pros SOLTOS, fluxo pros DEPENDENTES (por folder).
   ✅ FASE 1 (ENTREGUE 2026-06-25): `layoutFolderInner` particiona conectados (dagre TB, intacto) vs soltos
      (GRADE com wrap: 10 cols, 11º→linha2/colA; alargamento cols=max(10,ceil(N/30)) após 30 linhas). Contrato
      InnerLayout inalterado. Math validada (n=12→11º em linha2/colA; n=600→20cols/30linhas). Defaults
      LAYOUT_COLUMNS=10 / LAYOUT_MAX_ROWS=30 hardcoded. (lib/layout.ts = dead code, mesmo bug, p/ limpar.)
   ✅ FASE 2 (ENTREGUE 2026-06-25): `columns`/`maxRows` configuráveis em Settings (aba Geral › Visualização,
      via localStorage + evento `regente:layout-changed` → re-layouta na hora). Threaded por LayoutConfig em
      layoutFolderInner/composeColumns/build*Canvas. Override por folder (.regente-folder.yaml) = refinamento futuro.
   ✅ FASE 3 (ENTREGUE 2026-06-25): minimap já reflete a grade (NavMinimap desenha por node.position) + botão
      "Organizar" na topbar (fitView re-enquadra; layout já auto-aplica, nós não persistem drag).
   ── (spec original abaixo) ──
   Hoje o dagre TB
   (`layoutFolderInner` no V2Preview) já posiciona DEPENDENTES certo: A na linha 1, B e C lado a lado na
   linha 2, cadeia A→B→C em 3 linhas. O problema é só com SOLTOS (sem aresta interna): o dagre joga todos no
   rank 0 → uma fila horizontal infinita. Regras (escopo POR FOLDER — a decisão de uma folder não mexe nas
   outras):
   • DEPENDENTES → mantêm o dagre TB (top-down, irmãos lado a lado). Nada muda.
   • SOLTOS → GRADE com wrap: até `columns` por linha, depois quebra pra baixo. 11º solo → linha 2 col A.
   • ALARGAMENTO: `columns` é cap SOFT; ao passar de `maxRows` linhas, cresce colunas em vez de altura →
     `effectiveCols = max(columns, ceil(qtdSoltos / maxRows))`. Não vira nem fileira única nem ribbon altíssima.
   • Layout: zona de FLUXOS (componentes conectados, dagre) em cima · GRADE de soltos embaixo (gap entre elas).
   • Parâmetros em Settings (default global + override por folder): `columns` (default 10) · `maxRows`
     (default 30 — análise: 50 vira ~6000px de scroll; 30 mantém usável e só alarga após ~300 soltos numa
     folder, raro). Range sugerido columns 4–20.
   Vale pro Monitoring e Design. Plano de implementação detalhado: ver docs/plano-layout-jobs.md.
✅ Minimap REVISTO (ENTREGUE 2026-07-01) — `NavMinimap` reescrito: desenha só os jobs em QUADRADINHOS
   (proporção do card, por `node.position`) em vez de bolinhas + RETÂNGULO do viewport (área visível, via
   `useStore` transform/width/height) refletindo a tela; escala estável top-left; clique navega. Opt-in em Settings.
⬜ Cap de 2000 do Monitoring legado é POUCO + ENGANA — `LEGACY_CAP=2000` no canvas/ACTIVE JOBS (sidebar e
   ReactFlow não-virtualizados → cap evita travar) é arbitrário e mostra "2000/2000" como se fosse o total.
   Fix: (1) VIRTUALIZAR a sidebar ACTIVE JOBS (como o `ScaleMonitor`) → mostra o dia inteiro sem travar, cap
   sobe muito/some na lista; (2) header com o TOTAL REAL do `/summary` ("2000 carregados de 1.000.000"), nunca
   número truncado disfarçado de total; (3) cap do CANVAS (ReactFlow não desenha 100k nós) vira configurável e
   bem maior, com aviso "abra o ViewPoint pra ver todos". O ViewPoint já mostra 100k–1M.
◑ Aba de AGENTES (em Settings/Config) — visão e CONTROLE da frota.
   ✅ LISTA CONSOLIDADA (ENTREGUE 2026-06-25): aba "Agentes" com a frota online + offline (last-seen) +
      CONTADOR ("N de M online"); cada agente reporta metadata no handshake (os/arch/host/versão/started);
      migration v6 enriquece a tabela `agents`; `GET /api/agents` = online (verdade do hub) + DB. CLIQUE →
      MODAL de detalhe (SO/arch, host, versão, UPTIME, conectado há, 1ª vez visto, último sinal, capabilities).
      Auto-refresh 5s. Gestão de tokens junto. `AgentsManager.tsx`. 2 testes + validado ao vivo (2 agentes
      reais → offline com last-seen ao matar). Multi-nó: online é por-nó (cross-nó via R5 presence fica p/ depois).
   ✅ PING ATIVO (ENTREGUE 2026-06-25): round-trip ping/pong pelo /ws/agent (server manda {event:ping,pingId},
      agente responde {event:pong}); `POST /api/agents/{id}/ping` → {online, ok, latencyMs, error} (timeout 5s);
      `pingRegistry` correlaciona o pong; botão "ping" por agente + "ping todos" na UI, chip com latência/timeout/
      offline. 1 teste (WS real in-process) + validado ao vivo (agente real → ok; inexistente → offline).
   ⬜ Cross-nó (multi-node R5): refletir agentes conectados em OUTROS nós (presence do bus), não só deste.

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
✅ Escala     → Postgres plugável + migrations ✔ · stateless (estado durável externo; só o líder agenda) ·
                 **write-path 1M/dia** (P1: lote, 1M em 17s) ✓ · **read-path paginado/filtrado** (P2: /page +
                 /summary + `team` na instance, RBAC por conjunto — 51ms/18ms @100k) ✓ · **UI por ViewPoint
                 server-driven** (P3: ScaleMonitor, VALIDADO AO VIVO @1M) ✓ — ver §Escala Control-M
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
        (Resta só VIRTUALIZAR a sidebar ACTIVE JOBS legada — ver "O que falta".)
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
✅ MCP agent-native (2026-06-24) → binário regente-mcp REAL dirigido por pipe JSON-RPC (como o Claude
                    Desktop faz) contra server real: initialize ecoou protocolVersion 2025-06-18; tools/list
                    devolveu as 6 read tools (writes ocultos sem -allow-writes); daily_summary e explain_job
                    relegaram a verdade do engine (runnable=false, WAIT_CONDITION BATCH_OK). 7 testes
✅ Dry Run (2026-06-24) → server REAL: workspace com 5 jobs, GET /api/daily/dryrun?date=2026-12-25 →
                    run=1 (root) · wait=1 (child, depois de root) · blocked=2 (orphan: condition NEVERSET;
                    blocked_dep: depende de day15 não-agendado) · notScheduled=1 (day15, job de dia-15), com
                    razão em cada; sem materializar nada. 3 testes
✅ Blast Radius (2026-06-24) → server REAL: workspace A→B→C + A→D(always), A travado por janela →
                    GET /api/instances/A-*/blast-radius devolveu downstream=2 (B/PAGAMENTOS d1, C/RISCO d2
                    com SLA), slaAtRisk=1, teamsAffected=2, maxDepth=2; D (aresta always) excluído. 4 testes
✅ Diff de Daily (2026-06-24) → server REAL: 2 dailies semeadas (commits aaaaaaa→bbbbbbb) → GET /api/daily/diff
                    devolveu added=[new] · removed=[gone] · unchanged=[keep] · changed=[sched (schedule
                    06:00→07:00), dep (upstream x→y)] com diff por-campo exato; counts exatos. 4 testes
✅ Explain "por que não rodou" (2026-06-24) → server REAL: job (workspace def) esperando condition
                    'FECHAMENTO_OK' + recurso db (quer 2, cap 1) → GET /explain listou os 2 bloqueios
                    (WAIT_CONDITION + WAIT_RESOURCE, runnable=false); setou a condition + subiu a capacidade
                    → runnable=true, blockers vazios. tick e Explain concordam (fonte única). 21 testes
✅ Ciclo de vida da daily / carry-over (2026-06-24) → server REAL contra DB de disco semeado: ontem
                    (2026-06-23) com 40 jobs → a virada (auto-daily) trouxe os 15 abertos (5 RUNNING + 5 NOTOK
                    + 5 HELD) para hoje com carriedFrom=2026-06-23; OK(15)+WAITING(10) ficaram; carry-over
                    rodou 1× apesar de N ticks (idempotente). Migration v5 aplicou no DB existente. 11 testes
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
| ✅ | ~~Diferenciais: Job Neighborhood · RCA · Event log · NL-query~~ | **Feito (2026-07-03):** 4 diferenciais de observabilidade, todos **read-only e aditivos** (não tocam tick/dispatch/gating). (1) **Job Neighborhood** (`scheduler/neighborhood.go`, `GET /instances/{id}/neighborhood?radius=`): BFS bidirecional (ancestrais + descendentes até 4 saltos) com status por instance e a condição da aresta nos vizinhos diretos — "quem me trava, quem eu travo". (2) **RCA** (`scheduler/rca.go`, `GET /instances/{id}/rca`): sobe a cadeia de upstreams FALHOS (NOTOK/CANCELLED) e aponta a(s) raiz(es) que falharam por conta própria + a cadeia até elas; trata alvo-é-a-própria-raiz e job-OK-sem-causa. (3) **Event log CQRS-lite** (`api/events.go`, `GET /events`): read-model cross-instance sobre `instance_events` JOIN `instances` (filtros date/kind/actor/folder/instance, cursor keyset por id, RBAC por conjunto) — timeline/auditoria do dia sem tocar o write-side. (4) **NL-query** (`api/query.go`, `POST /query {q}`): parser de intenção **determinístico** PT/EN (summary·list·count·explain, extração de folder e de job-ref por "\<job\> (não) rodou/falhou" ou "job \<X\>") → consulta estruturada reusando os mesmos filtros/RBAC, devolvendo a interpretação junto (sem chute; fora de escopo → "não entendi" com sugestões). Os 4 viram **tools MCP** (`job_neighborhood`·`root_cause`·`event_log`·`query`), read-only. UI: painéis **Causa raiz** (auto em NOTOK/bloqueado) e **Vizinhança** (opt-in, ±1/2/3) no InstanceDetailsDrawer. 20 testes Go novos + validação ao vivo headless (neighborhood ±2 · RCA · event feed+filtro · query summary/count/list/explain/unknown). |
| ✅ | ~~Aprofundamento Control-M: FECHAR a trilha (cyclic · CONFIRM · DATABASE · SET var · baterias · ViewPoints)~~ | **Feito (2026-07-03):** (1) **Cyclic runtime** — job que termina OK re-arma a MESMA instance pra rodar em `intervalMin` (`maybeCycle`): a volta vira WAITING com `scheduled_at` futuro (gate de janela mostra "próxima às HH:MM"), `attempts` reseta por volta, `cycle_runs` conta; encerra em `cyclicMaxRuns`, ao passar de `windowTo`, ou na virada da daily (WAITING-nunca-rodou não carrega). NOTOK NÃO cicla (espera operador). Validado ao vivo: volta 1 OK → re-armou pra +10min. (2) **CONFIRM** (Control-M "Wait for confirmation") — `def.confirm:true` vira gate `WAIT_CONFIRM` no `gateInstance` (fonte única): nem o tick nem o Force reivindicam até `POST /instances/{id}/confirm` (rerun re-exige; bulk `confirm`). Botão "Confirmar execução" no ExplainPanel (keyed no blocker, sem flag denormalizada). Validado ao vivo: WAIT_CONFIRM → confirm → OK. (3) **Job DATABASE** — executor no agente (`agent/database.go`) roda SQL em **Postgres/MySQL/SQLite** via drivers pure-Go (sem CGO/JDBC): SELECT renderiza linhas (maxRows), DML mostra rows-affected, auto-detecção query/exec; capability `DATABASE`, validação server (driver+dsn+sql), palette + editor de params. 4 testes contra SQLite real. (4) **SET de var em runtime** (ctmvar) — job imprime `%%SET NOME=VALOR` no output → grava global no VariableStore (auditado, teto 20/job); **cálculo de datas** `%%ODATE±N` / `±NB` (dias úteis cientes do calendar do job) em `%%` e `${var.}`. (5) **Janela fechada** — WAITING depois de `windowTo` vira gate `WINDOW_CLOSED` (não submete mais hoje). (6) **ViewPoints salvos** — filtros nomeados do Monitoring (`/api/viewpoints`, upsert por nome, shared) + Find & Update estendido (enabled/runAt em lote). Baterias de teste (calendários complexos · recursos sob concorrência/all-or-nothing · Forecast ≥1 semana) + 9 testes de feature + validação ao vivo headless. |
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
| ✅ | ~~Ciclo de vida da daily (carry-over / Keep Active)~~ | **Feito (2026-06-24):** RUNNING/HELD persistem; NOTOK +1 / keepActive N; order_date avança; migration v5; validado ao vivo. |
| ✅ | ~~Diferencial: Explain ("por que não rodou?")~~ | **Feito (2026-06-24):** gating como fonte única (tick+Explain), read-only, 21 testes, validado ao vivo. Substrato do MCP `explain_job()`. |
| ✅ | ~~Diferencial: Diff de Daily~~ | **Feito (2026-06-24):** compara 2 order_date via snapshots congelados; diff por-campo; fast-path same-commit; 4 testes; validado ao vivo. |
| ✅ | ~~Diferencial: Blast Radius~~ | **Feito (2026-06-24):** BFS reverso de deps; downstream/SLA/folders/cascata; só o raio (barato a 1M); 4 testes; validado ao vivo. |
| ✅ | ~~Diferencial: Dry Run~~ | **Feito (2026-06-24):** simula daily futura sem materializar (RUN/WAIT/BLOCKED/NOT_SCHEDULED + razão, cascata); reusa IsScheduledOn; 3 testes; validado ao vivo. |
| ✅ | ~~Camada agent-native (MCP)~~ | **Feito (2026-06-24):** servidor MCP (`server/cmd/mcp`, stdio JSON-RPC, pure-Go) expõe os 4 diferenciais + summary/busca como tools; read-only por default, writes gated; 7 testes; validado ao vivo (pipe JSON-RPC). docs/mcp.md. |
| ✅ | ~~Aprofundamento Control-M: Actions/On-Do (motor backend)~~ | **Feito (2026-06-29):** motor On/Do nas 3 dimensões (result/attempt/runtime) + 4 ações (notify/set-condition/run-job/set-ok) reusando substratos; idempotência por ledger (migration v7); 14 testes + validado ao vivo. |
| ✅ | ~~Aprofundamento Control-M: Actions/On-Do (UI por job)~~ | **Feito (2026-06-30):** aba "On/Do" no JobConfigDrawer (OnDoEditor) — regras On‹gatilho›Do‹ação› com add/remove, campos contextuais, chips de canais e tradução natural por regra. ActionRule no modelo TS + map no ServerApiAdapter; round-trip provado (TestFileStore_ActionsRoundTrip). |
| ✅ | ~~Refinamento UI: fixes de Folders/Design~~ | **Feito (2026-07-01):** (1) botão bulk da tela Folders lê **"Abrir"** quando já em Design (era sempre "Abrir no Design", redundante); ao abrir folder(s) selecionadas, o modal **fecha e cai direto no canvas** (`onOpened` callback). (2) Bug feio: arrastar o 1º job de uma folder vazia mostrava "Nenhuma definition" sobreposto ao card recém-criado — overlay usava `hasDefs` (contagem global do repo) em vez de `designDefsWithDraft` (o que o canvas realmente renderiza, incluindo o rascunho não-salvo). (3) JobConfigDrawer: campo "Label" renomeado para **"Job Name"**; campo **ID removido da UI** (segue existindo internamente — nome do YAML/chave de dependências/ledger de actions — só não precisa mais aparecer pro usuário). (4) **Todos os 4 drawers docados padronizados no flutuante arredondado** (top/right/bottom:10 + border completa + borderRadius:16 + boxShadow): `JobConfigDrawer` (Edit Job) e `InstanceDetailsDrawer` (detalhe do Monitoring) alinhados ao padrão que a `DesignSidebarV2`/`MonitoringSidebarV2` já tinham; antes colavam nas bordas (top/right/bottom:0, só borderLeft, sem raio). `ScaleMonitor` fica de fora (view full-screen `inset:0`, full-bleed de propósito). (5) Botão **"Abrir"** da action bar de Folders virou **primary em destaque** (variante `primary` no `BarBtn`: preenchido no accent, texto 12px/800, pílula, glow + lift no hover) — é a ação principal quando há folder selecionada; os demais (Arquivar/Excluir/Cancelar) seguem discretos. (6) **Job "sumido" ao criar** corrigido: com o canvas em zoom/pan, o nó recém-criado (ou os já existentes) podia nascer fora da tela e parecer sumido (não era perda de dado — a lane contava certo, era só a câmera não reenquadrar); `handleSaveDef` agora dá `fitView` ao salvar um job NOVO. (7) **Aba Folders da sidebar esquerda** (`DesignSidebarV2`) agora **lista os jobs de cada folder como linhas clicáveis** (`JobRow`, hover destaca) abaixo do nome+contagem; clicar navega/centraliza o canvas no nó (`onJobClick` → `focusNode`). Validado ao vivo (server real + drag-and-drop simulado; viewport 1.25→fit com ambos on-screen; clique no job centralizou no nó). |
| ✅ | ~~Refinamento UI: viewport fluido + minimap~~ | **Feito (2026-07-01):** causa raiz dos jobs "sumindo" no Run Daily/Force e da câmera voltando ao centro sozinha = o effect que ancora o topo dependia de `canvas.nodes` (array novo a CADA re-layout) → todo update de dado (status via WS/tick a cada 2s, Run Daily, Force) reancorava a câmera. Agora dispara só em `[viewContextKey, hasNodes]` (troca de modo/folders ou jobs aparecendo 1×); churn de dado não mexe na câmera. `handleRunDaily` sem os `fitView` concorrentes; **Force** centraliza no job forçado quando ele materializa (`pendingFocusId`, mantém zoom) em vez de reenquadrar tudo. **Minimap** reescrito: só jobs, **quadradinhos** (rect na proporção do card) em vez de círculos, + **retângulo do viewport** (via `useStore` transform/width/height) refletindo a tela. Validado ao vivo: Run Daily materializa 3 jobs on-screen; 6s de churn sem snap (viewport fixo); Force cria o 4º e centraliza nele (21px do centro); minimap 4 rects/0 circles. |
| ✅ | ~~Refinamento UI: câmera consistente com a trava + board sem F5~~ | **Feito (2026-07-01):** três raízes fechadas. (1) **Pulo pra "posição travada"** ao mexer após centralizar/Organizar/Force: `translateExtent` é estático em px de MUNDO, mas âncora/centralização posicionavam em px de TELA — em zoom<1 ou centralizando job do topo a câmera ficava FORA do extent, e o RF só clampa pan do usuário (programático passa direto) → 1º arrasto reaplicava a trava (pulo de ~120px medido). Fix: `clampTy` + `focusOnPoint` aplicam o MESMO limite do extent a TODO movimento programático (Organizar, sidebar, pendingFocus do Force, minimap), e o extent ganhou `PAN_SLACK_TOP=176` world px (cobre a âncora de 88px de tela até o minZoom 0.5, e dá folga maior de puxada pra baixo). (2) **`fitView` assíncrono do RF v12** (promise nem resolve logo após o mount): `organizeView` agora calcula o fit NA MÃO (bounds das lanes + pane via `useStoreApi`), síncrono; prop `fitView` do `<ReactFlow>` removido (corria contra a âncora); gate de entrada espera `paneReady` (dimensões via `useStore`) — cobre o mount pós-login com nodes pré-existentes. (3) **Board vazio até F5 ao abrir**: carga inicial rodava pré-login com token errado (401 sem retry) e nada re-buscava após logar. Fix: `setAuthToken` reconecta o WS → `onopen` emite `_connected` sintético → store ressincroniza instances e a UI recarrega defs; retry de 5s na carga inicial; listener de instances sem refiltro por `todayOrderDate()` do browser (zerava o board no 1º evento WS com dia cliente ≠ dia server). Validado ao vivo (90 defs/102 instances): entrada=Organizar (`ty=76`@.5); drag pós-centralização sem snap; Force ×2 câmera imóvel; login → board sem F5. |
| ✅ | ~~Daily server-side configurável + WAIT EVENT (paridade Control-M)~~ | **Feito (2026-07-02):** (1) **daily_at configurável**: `autoDailyIfDue` agora lê `settings.daily_at` (HH:MM validado; inválido loga e cai no default 00:00) a cada verificação — muda em runtime pela UI (Settings → Geral, admin) sem restart; relógio de referência = SEMPRE o do server. Novo `GET /api/daily/status` {orderDate, dailyAt, lastRunDate, lastRunAt, serverNow} = fonte do rodapé (fim do flag em localStorage em server mode; refetch em daily.started/settings.changed/_connected). Validado ao vivo: boot às 08:28 com daily_at=08:30 NÃO materializou; às 08:30:01 materializou 8 instances. (2) **Sucessor = Wait Event**: o tick cancelava o sucessor ~2s após o pai falhar (GateDepBlocked→CANCELLED) — CANCELLED é terminal, então rerun/Set OK no pai não revivia nada (fluxo de operação quebrado). Removido o auto-cancel: sucessor fica **WAITING** com pai falho/rodando/não-ordenado; rerun/Set OK destravam e o tick despacha sozinho; WAITING-nunca-rodou morre na virada (carry rules, como Control-M). UI: card mostra **WAIT EVENT** quando o WAITING é por dependência (waitEvent derivado em buildMonitoringCanvas da MESMA fonte visual das edges; "WAIT" fica pra espera de horário). Validado ao vivo com agente real: NOTOK→filho WAITING 4+ ticks (Explain BLOCKED_DEP)→Set OK→filho rodou; RUNNING 20s→filho WAIT EVENT (WAIT_DEP)→OK→filho rodou. Testes: TestTick_BlockedSuccessorWaitsAndRunsAfterSetOK + TestDailyAt_FromSettings. GOTCHA descoberto no caminho: agente Windows roda COMMAND via **powershell -Command** (não cmd) — `> NUL` falha; e o campo JSON de params da definition é **`actionConfig`** (tag json), `params` só no YAML. |
| ✅ | ~~Hardening pós-review (SQLITE_BUSY · V2Preview modular · resync total)~~ | **Feito (2026-07-01):** os 3 pontos de atenção da avaliação. (1) **SQLITE_BUSY em rajada**: pragmas iam via `Exec` numa conexão só do pool (`database/sql`) — as demais ficavam SEM `busy_timeout` e a daily perdia eventos de auditoria em rajada. Fix em `internal/db`: `sqliteDSN()` anexa `_pragma=busy_timeout(5000)/journal_mode(WAL)/foreign_keys(1)` ao DSN → o modernc/sqlite aplica em CADA conexão nova. Validado: rajada de 95 jobs, zero "database is locked", eventos todos persistidos. (2) **V2Preview.tsx desmontado** (2223→1485 linhas): `canvas-layout.ts` (constantes+builders dagre, puro), `NavMinimap.tsx`, `hooks/useCanvasCamera.ts` (panExtent+clampTy+focusOnPoint+organizeView+gate de entrada+pendingFocus — todo movimento programático clampado), `hooks/useOrchestratorData.ts` (defs+instances, bootstrap, subscribes, scheduler, WS de dados incl. `_connected`; expõe `reloadDefs`/`syncInstances`). Eventos de UI (alert.fired/changed) ficam no componente. tsc limpo; eslint = paridade exata com baseline (7 apontamentos pré-existentes que só mudaram de arquivo). (3) **Resync `_connected` ampliado**: badge de alertas, env label e `/me` se recuperam na reconexão (fecha a classe "fetch-once-e-confia-no-WS"). Regressão completa ao vivo pós-refactor: entrada=Organizar (`ty=76`@.5) · centralização anima até o clamp exato e FICA (`ty=167.2`@1.1) · drags sem pulo · Force ×2 câmera imóvel · recuperação de 401 no mount sem F5. |
| ✅ | ~~Hosting single-origin + demo pra amigos~~ | **Feito (2026-07-01):** server serve o SPA buildado na mesma porta da API+WS (flag `-spa-dir`; frontend `@origin` → `window.location.origin`, adapta a qualquer URL de túnel sem rebuild). `deploy/demo/`: Dockerfile.agent (agente sandbox em alpine descartável), `host-demo.ps1` (build + server GitOps-direct + agente Docker + Cloudflare Tunnel) e README com segurança. Validado ao vivo: app carregado de :9091 faz todas as chamadas same-origin (200); traversal barrado. |
| ✅ | ~~WAIT AGENT (azul claro) + zero churn sem agente~~ | **Feito (2026-07-02):** sem agente, o tick reivindicava (WAITING→RUNNING) e revertia a cada 2s — log/evento spam + UI piscando + jobs "sumindo" (bug report do lab .bat, que não sobe agente). Fix: disponibilidade de agente virou GATE (`GateAgent`/`WAIT_AGENT` no gateInstance = fonte única; forced também checa) — sem agente o job NEM é reivindicado (zero broadcast/evento/log; `maybeEmitNoAgent` 1×/5min). `Bus.HasAgent(agentID, capability)`: hub local + presença remota do NATS (`Distributed.findRemote`) — pré-check só no hub local starvaria dispatch multi-nó. Agente conecta → ws handler broadcast `agent.changed` + `go Tick()` (nudge) → jobs presos disparam NA HORA. UI: card **azul claro (#38bdf8) "WAIT AGENT"** derivado de `GET /api/agents` (refetch em agent.changed/_connected, SEM polling); prioridade WAIT EVENT (dep) > WAIT AGENT; badge AGENTE no Explain do drawer. Validado ao vivo: sem agente = 6s+ estável, zero `started`; conectou = dispatch <1.5s. `TestTick_NoAgentNoClaim` (WAITING estável em 4 ticks, throttle, Explain). |
| ✅ | ~~Lote enterprise 1-3 + %% + FILE_WATCH + shift de calendário~~ | **Feito (2026-07-02):** (1) **condition vazia = on-success** no server (`edgeState` case `""` movido de on-complete → on-success; era furo de segurança semântico: def YAML sem `condition:` rodava o filho com pai NOTOK; validado ao vivo — filho de YAML sem condition ficou WAITING/BLOCKED_DEP com pai NOTOK). (2) **API aceita `params` E `actionConfig`** no JSON (`decodeDefinition` em definitions+sessions; actionConfig vence se ambos; antes "params" era ignorado em silêncio e o job despachava sem comando; `TestDecodeDefinition_ParamsAlias`). (3) **mock-finish atrás de `-demo-mode`** (env `REGENTE_DEMO_MODE=1`): default OFF = sem agente, a instance REVERTE o claim pra WAITING (release de recursos + evento com throttle de 5min por instance) e o tick re-tenta quando um agente com a capability conectar; frota já alarmada pelo selfmon R7; testes usam DemoMode=true (comportamento antigo preservado). Validado: boot sem agente → 4 WAITING e 3 eventos (não dezenas); agente conectou → despachou tudo. (4) **Variáveis `%%`** (Control-M AutoEdit): `%%NAME` equivale a `${var.NAME}` (nome = letra+word-chars; dotted names só na sintaxe `${var.}`); tokens runtime em MAIÚSCULAS: **ODATE** (YYYYMMDD da ordem — lido da PRÓPRIA instance, correto em rerun/carry), ORDERDATE, RUNDATE, TIME, JOBNAME, JOBLABEL, FOLDER, INSTANCEID; resolve def.variables e globais (F18) por nome; não-resolvido fica INTACTO. Validado ao vivo: output real `ODATE=20260702 JOB=var-echo FOLDER=lote3`. 3 testes. (5) **FILE_WATCH** ponta-a-ponta: executor no agente (`agent/filewatch.go`: poll `path` a cada `intervalSec` (def 5s), opcional `stableSec` = tamanho estável por N s, timeout do job = NOTOK com motivo; capability `FILE_WATCH` no `-caps`); validação server (`FILE_WATCH.path required`); front: palette + editor de params + tipo. 4 testes de agente + validado ao vivo (RUNNING pollando → arquivo criado → OK). (6) **Shift de calendário** (Control-M roll): `schedule.shift = next-businessday \| prev-businessday` — dia nominal inelegível (feriado/exclude; sem calendar, fim de semana) rola pro dia elegível mais próximo; implementado DENTRO de `IsScheduledOn` (fonte única → daily, DryRun e SchedulePreview ganham juntos; `nominalScheduledOn`+`shiftEligible`); select no ScheduleEditor; 4 testes + validado via `/api/schedule/preview` (1/ago sáb: next→03/ago, prev→31/jul). |
| ✅ | ~~Jobs sumindo do canvas ao forçar em rajada (race de refresh)~~ | **Feito (2026-07-02, commit `0cc167e`):** bug recorrente (~6 forces seguidos → cards somem → só F5 volta). Causa raiz: `server-instance-store` disparava `GET /api/instances` COMPLETO por força E por evento WS parcial (todo broadcast `instance.changed` é parcial: só id+status) → rajada de N forças ≈ 3-4×N GETs concorrentes, cada um `cache.clear()`+repopula → resposta ANTIGA (snapshot pré-INSERTs) chegando POR ÚLTIMO apagava instances recém-criadas — e no monitoring nó do canvas = instance (`buildMonitoringCanvas`), então os cards sumiam e NADA re-disparava refresh até o F5. Três defesas no store: (1) `refresh()` **single-flight + rodada de cauda coalescida** (nunca 2 GETs em voo; gatilho durante fetch agenda UMA rodada extra DEPOIS dele = depois do commit que o originou; rajada de N eventos = ≤2 fetches e o último vê tudo; callers que aguardam — forceInstance/rerun — recebem a promise da cauda); (2) **gen guard** (`fetchGen`: resposta de fetch obsoleto descartada inteira); (3) **reconcile por MERGE**, nunca `cache.clear()` (`touchedAt`: id mutado via WS durante o fetch não é deletado/regredido pelo snapshot antigo; `tombstones`: deletado não ressuscita; marcas < startedAt podadas por fetch). Validado ao vivo (server demo-mode offline + preview): 10 forças paralelas + 2ª rajada de 6 com hold/release no meio → 16/16 cards firmes, 8 GETs coalescidos p/ ~30 eventos WS, zero console errors, sem F5. Descobertos no caminho (pendentes): ForceOrder gera id com resolução de SEGUNDO (`-FORCE-HHMMSS`, id é PK → 500 ao forçar o MESMO job 2× no mesmo segundo); cada evento WS refaz `GET /api/alerts?limit=500` sem coalesce. |
| ✅ | ~~Refinamento UI: drafts do Design à prova de F5 (retomada de sessão)~~ | **Feito (2026-07-02, commit `317ecd0`):** bug do usuário — abrir folder → Monitoring/F5 → Design voltava VAZIO; reabrir folder criava OUTRA session por cima da esquecida (trabalho invisível, "folders bugam"). Causa: o `designSessionId` vivia só em memória de módulo — o clone com o trabalho seguia vivo no server (P6: DB+disco), mas a UI perdia o PONTEIRO sem caminho de recuperação. Fix em 3 camadas: (1) **ponteiro persistido** (`localStorage regente:designSessionId` + restore no boot com claim P7; 404 limpa, erro transiente NÃO derruba); (2) **recuperação/auto-descarte** (effect pós-auth lista sessions do actor: valida posse do sid, auto-descarta LIMPAS idle >10min, SUJAS viram banner Retomar/Descartar no canvas; addFolder sem session RETOMA a suja mais recente em vez de criar por cima); (3) **server protege trabalho** (`DesignSession.Dirty()`; GC por TTL PULA sessions sujas — antes 24h idle apagava trabalho; `sweepCleanIdle(actor,10min)` no Create; `dirty` exposto no list/get via `sessionView`). `session_test.go` (TestDesignSessionDirtyProtection). Validado ao vivo: F5 retoma com o job no canvas; banner recupera órfã; abrir folder-2 por cima reusa a MESMA session (escopo f1+f2, dirty intacto); limpa idle auto-descartada. go test ./... + tsc + build ok. |
| ✅ | ~~**Aprofundamento Control-M (restante)** — cyclic · CONFIRM · DATABASE · SET var + datas · baterias · ViewPoints~~ | **Feito (2026-07-03):** TRILHA 100%. Ver a linha do topo do changelog. |
| ✅ | ~~**Diferenciais (leva 1)** — Job Neighborhood · RCA · Event log · NL-query~~ | **Feito (2026-07-03).** Ver a linha do topo do changelog. |
| **1** | **Diferenciais (leva 2)** — orquestração híbrida/stateful · DevEx (schedule-as-code · `regente test` · `regente dev daily`) · promotion · policy-as-code · "wow" (Gantt · templates) | Próxima leva; parte já dá pra alimentar a Fase Z (Gantt/templates). |
| 3 | **Refinamento UI** — grade de jobs soltos · minimap revisto · virtualizar ACTIVE JOBS (LEGACY_CAP) | Polimento que melhora a demo (alimenta a Fase Z). |
| 🏁 | **Fase Z** — case study + post LinkedIn | **ÚLTIMO gate, por definição.** Só quando o backlog acima estiver onde você quer. NÃO é o próximo passo. |

---

## 🏢 Backlog Enterprise (specs implementáveis — E1..E6, 2026-07-02)

Itens da avaliação enterprise, especificados para implementação SEM ambiguidade.
Regras gerais para TODOS os itens: (a) mudanças de schema = nova migration versionada
em `internal/db` (SQLite E Postgres); (b) settings novos via tabela `settings`
(editáveis por `PUT /api/settings`, admin-only) com fallback no default do processo;
(c) todo item entrega testes Go + validação ao vivo documentada no commit; (d) nada
quebra o modo demo/single-node — features avançadas são opt-in com default seguro.

### E1 — Timezone da daily (settings.daily_timezone)
**Contexto:** a daily roda no relógio LOCAL do server (`time.Now()` em `autoDailyIfDue`);
em produção o server costuma rodar em UTC e o negócio pensa em `America/Sao_Paulo`.
**Spec:** novo setting `daily_timezone` (string IANA, ex. `America/Sao_Paulo`; vazio =
local do server, comportamento atual). Em `scheduler.autoDailyIfDue`: carregar via
`time.LoadLocation` (cache no struct; recarregar se o setting mudar); `now :=
time.Now().In(loc)`; `today` e `dailyTime` derivam de `now` NESSA location. O
`order_date` gravado é o dia NA TIMEZONE configurada. `GET /api/daily/status` passa a
incluir `"timezone"`. UI: campo texto ao lado do "Horário da daily" em Settings→Geral
(validar com uma lista de sugestões; valor inválido → loga e cai no local).
**Aceite:** teste com `daily_timezone=America/Sao_Paulo` e clock UTC fake (injetar
`nowFn func() time.Time` no Scheduler p/ testabilidade — refactor pequeno permitido):
às 02:59Z de 03/jul não roda (ainda 23:59 de 02/jul em SP); às 03:00Z roda com
`order_date=2026-07-03`. Cuidado: `parseHHMM` continua igual; só a base muda.

### E2 — Auditoria enterprise (retenção + export + audit de settings)
**Contexto:** o event log (`instance_events`) e o audit (`audit` pkg, SIEM URL opcional)
existem, mas sem retenção nem trilha de mudanças de configuração.
**Spec (3 partes independentes):**
1. *Retenção:* setting `audit_retention_days` (int, 0 = infinito/default). GC diário
   (rodar logo após a daily, só no líder): `DELETE FROM instance_events WHERE ts <
   date('now', '-N days')` (dialect-safe via rebind; em lotes de 10k p/ não travar).
   Logar quantas linhas saíram.
2. *Audit de settings:* `PUT /api/settings` emite evento de auditoria (pkg `audit`)
   com actor + LISTA DE CHAVES alteradas e valores de→para — EXCETO chaves secretas
   (as mesmas mascaradas no GET: github_token, webhook_secret, alert_*_password/key).
3. *Export:* endpoint `GET /api/audit/export?from=&to=&format=jsonl` (admin-only,
   streaming, max 100k linhas por chamada com cursor `after_id`) devolvendo
   instance_events + audit events unificados `{ts, kind, actor, instanceId?, detail}`.
**Aceite:** testes: GC remove só o que passou do prazo; PUT settings gera evento sem
vazar segredo; export pagina com after_id estável.

### E3 — RBAC de escrita por AÇÃO OPERACIONAL (folder-scoped)
**Contexto:** definitions já checam `auth.CanWriteFolder` (ACL por folder), mas as
ações de OPERAÇÃO em instances (hold/release/cancel/rerun/set-ok/force/bulk) usam só
`requireWriterMW` (role global operator+) — um operator do time FIN consegue cancelar
job do time RISCO.
**Spec:** nos handlers de `instances.go` (hold/release/cancel/rerun/set-ok) e
`bulk.go`: após carregar a instance, resolver a folder (coluna `team` da instance;
vazio → team da definition viva; vazio → permitir) e exigir
`auth.CanWriteFolder(db, user, team)` além do writer role. `forceOrder` idem (team da
definition). Bulk: checar POR ITEM e reportar 403 por item no resultado (não abortar
o lote inteiro). Bearer token legado (api-token) segue admin (bypassa).
**Aceite:** teste API: user operator com ACL write=[FIN] consegue rerun em instance
FIN e leva 403 em instance RISCO; bulk misto retorna ok/failed por item.
**NÃO fazer:** UI de gestão de ACL nesta entrega (já existe API de ACL) — só enforcement.

### E4 — Fila assíncrona de eventos de instance (protege o hot path)
**Contexto:** `emitEvent` faz INSERT síncrono no caminho do tick/dispatch; em Postgres
sob carga extrema compete com o scheduling. (No SQLite o busy_timeout no DSN já
resolveu a rajada.)
**Spec:** `internal/scheduler/eventqueue.go`: canal buffered (cap 10_000) + goroutine
writer que agrega e grava em LOTE (INSERT multi-values, flush a cada 250ms OU 500
eventos, o que vier primeiro — mesmo padrão do insertDailyBatch). `emitEvent` vira
enqueue; se a fila estiver CHEIA, grava síncrono (degradação, nunca perde). Flush
final no shutdown (context). Métrica `regente_event_queue_depth` no /metrics.
Ordem por instance preservada (fila única FIFO).
**Aceite:** teste com 10k emits concorrentes → todas as linhas no banco, ordem por
instance preservada, zero perdas com fila cheia (modo síncrono cobre).

### E5 — Relatório/SLO da daily (o artefato que operação cobra)
**Contexto:** hoje o resultado da daily se observa olhando o board; operação de
verdade quer um resumo por dia: o que rodou, o que falhou, atraso.
**Spec:** `GET /api/daily/report?date=YYYY-MM-DD` (default hoje) → `{date, dailyAt,
startedAt, counts:{ordered, ok, notok, waiting, running, cancelled, carried},
lateStart: bool (startedAt > dailyAt+5min), failures:[{defId, team, exitCode,
finishedAt} cap 100], slaBreaches:[...via SLAEngine se disponível]}`. Fonte: 1 query
agregada em `instances` (order_date=date) + `daily_runs`. Push opcional: setting
`daily_report_channels` (csv: slack/webhook/email — REUSAR os sinks do alerting) —
enviar quando a daily "fecha" (nenhuma instance WAITING/RUNNING; checar no tick 1×/min,
flag em daily_runs `report_sent_at` p/ idempotência) OU às `daily_report_at` (HH:MM).
UI: card compacto no topo do Monitoring com ok/notok/atraso do dia (dados do endpoint).
**Aceite:** teste do agregado (seeds em vários status → counts exatos); teste de
idempotência do envio (report_sent_at); UI mostra counts do endpoint (não recalcula).

### E6 — Importador Control-M (redutor de fricção nº 1 p/ adoção)
**Contexto:** empresas que avaliarem o Regente têm CENTENAS de jobs no Control-M;
migrar na mão mata a adoção.
**Spec:** novo binário `server/cmd/importctm` (pure-Go, sem deps novas): lê o XML de
export do Control-M (`DEFTABLE`/`FOLDER`/`JOB` do ctm export/forecast) e gera um
workspace Regente (`definitions/<folder>/<job>.yaml` + `calendars/*.yaml`).
Mapeamentos v1 (documentar TODOS no README do cmd):
`SUB_APPLICATION/DATACENTER→ignorar com warning · JOBNAME→id (slug) · DESCRIPTION→label ·
PARENT_FOLDER→team · TASKTYPE Job/Command→COMMAND (CMDLINE→params.command) ·
FileWatcher→FILE_WATCH · INCOND→upstream (quando o OUTCOND correspondente é de 1 job
só; senão conditions F16 com o MESMO nome) · OUTCOND(+)→conditionsOut · TIMEFROM→runAt ·
DAYS/WEEKDAYS→frequency/daysOfWeek/daysOfMonth · DAYSCAL/WEEKSCAL→calendars include ·
CONFCAL+SHIFT→schedule.shift · SHOUT→actions notify · MAXRERUN→retries · CYCLIC→cyclic`.
Tudo que não mapear → `# TODO-import:` comentado no YAML + linha no relatório final
(`import-report.md`: N jobs ok, N parciais, N pulados e por quê). Flags: `-in export.xml
-out ./workspace [-dry-run] [-folder-filter X]`. NUNCA push — gera arquivos locais pro
usuário revisar e commitar.
**Aceite:** golden test com um XML de exemplo (fixture) cobrindo cada mapeamento;
`-dry-run` não escreve nada; relatório lista os não-mapeados.

---

## 🧩 Aprofundamento Control-M *(testar a fundo + aprimorar)*

> O núcleo de paridade existe, mas precisa de **bateria de testes** cobrindo os casos reais do
> Control-M e refino onde faltar. Cada item = testar TODAS as possibilidades + fechar os gaps.

```
⬜ Calendários complexos — validar que o job entra na daily exatamente quando deve: 1º dia útil do mês ·
   só segundas · 1º dia útil que NÃO é segunda · N-ésimo dia útil · regras avançadas · include/exclude ·
   feriados · meses específicos. Cobrir todas as combinações; corrigir o gating onde divergir.
   (Base pronta: o gating é fonte única `IsScheduledOn`; `schedule.shift` next/prev-businessday ✅ já entregue.)
⬜ Controle de recursos — testar e aprimorar: quantitative (N slots), jobs que NÃO podem concorrer
   (lock exclusivo), máximo de jobs simultâneos por host/pool, fila quando esgota, liberação correta.
✅ Actions / On-Do do job — ENTREGUE (motor 2026-06-29 + UI 2026-06-30). Regras On‹gatilho›Do‹ação› nas
   3 dimensões: (a) por Nº DE TENTATIVA (On attempt N — dispara na N-ésima falha, cobre a final) ·
   (b) por RESULTADO (On result OK/NOTOK, transição terminal) · (c) por TEMPO DE EXECUÇÃO (On runtime >N min,
   "shouts" avaliados a cada tick sobre o RUNNING, escalonáveis 30/40/60min). 4 ações reusando os substratos:
   notify (Slack/webhook/e-mail/PagerDuty) · set-condition (destrava sucessores) · run-job (Force Order) ·
   set-ok (auto-heal NOTOK→OK). Idempotente (ledger durável action_fires, migration v7). Decisor puro testável.
   server/internal/scheduler/actions.go + 14 testes + VALIDADO AO VIVO. UI: aba "On/Do" no JobConfigDrawer
   (`OnDoEditor`) — add/remove, campos contextuais por tipo, chips de canais e tradução em linguagem natural
   por regra (`ActionRule` no modelo TS + map no ServerApiAdapter; round-trip `TestFileStore_ActionsRoundTrip`).
   FALTA (opcional, não bloqueia): expor o histórico de disparos (action_fires) no drawer.
✅ Job FILE_WATCH — ENTREGUE (2026-07-02). jobType + capability `FILE_WATCH`: o agente (`agent/filewatch.go`)
   pesa `path` a cada `intervalSec` (def 5s) com estabilidade de tamanho opcional (`stableSec`) e timeout=NOTOK;
   palette + editor de params na UI; validação server (`FILE_WATCH.path required`). 4 testes de agente +
   validado ao vivo (RUNNING pollando → arquivo criado → OK → dispara o sucessor).
⬜ Forecast — testar a previsão de ≥ 1 semana à frente (quais jobs rodam por dia, sem executar); validar
   contra o gating real (calendars + deps + conditions + recursos). (Dry Run de 1 dia ✅; falta a bateria ≥1 semana.)
✅ Ciclo de vida na daily (Keep Active / carry-over entre diárias) — ENTREGUE (2026-06-24):
   • RUNNING persiste (REGRA) — job EM EXECUÇÃO na virada da daily NÃO some: segue na daily até terminar,
     para o tracking da execução (jamais perder a instância no rollover). ✓
   • Keep Active — opção no job (`schedule.keepActive`, editável no Design): se NÃO executou com sucesso,
     sobrevive N diárias. ✓
   • DEFAULT — job que termina NOTOK e NÃO é tratado persiste +1 diária (carry-over automático). ✓
   • HOLD persiste — job em HOLD atravessa as diárias enquanto estiver em hold. ✓
   Mecanismo (Control-M New Day): `scheduler.carryOver(date)` roda no topo do `RunDaily` (antes da
   existência), AVANÇANDO o order_date da ordem que sobrevive (mesmo id/status/started_at/snapshot/eventos)
   — assim tick, /page, /summary e RBAC (todos filtram order_date) a enxergam no novo dia sem mudança, e a
   ordem carregada SUBSTITUI a fresca daquele dia (sem duplicar). `carry_budget` (lazy-init, -1) bounda
   NOTOK/keepActive; `carried_from` exibe a origem (badge "↩" no ViewPoint); `carried_at` re-arma o watchdog
   de stuck-running (RUNNING carregado não é reapado no instante em que aparece). Migration v5. Idempotente.
   11 testes (regra pura + RUNNING/HELD/NOTOK/keepActive/idempotência/no-dup/watchdog). VALIDADO AO VIVO no
   binário: ontem 40→ficaram 25 (OK+WAITING), 15 abertos (RUNNING/NOTOK/HELD) migraram com carriedFrom, 1 só
   carry-over apesar de N ticks.
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
◑ Sistema de variáveis (estilo Control-M %%) — INTERPOLAÇÃO ENTREGUE (2026-07-02); falta o SET em runtime:
   ✅ Sintaxe `%%NAME` (AutoEdit) equivale a `${var.NAME}`; tokens de runtime em MAIÚSCULAS: %%ODATE (lido da
      PRÓPRIA instance, correto em rerun/carry), %%ORDERDATE, %%RUNDATE, %%TIME, %%JOBNAME, %%JOBLABEL, %%FOLDER,
      %%INSTANCEID; resolve `def.variables` e globais (F18) por nome. Interpolação em QUALQUER campo string
      (command/url/path/body); não-resolvido fica intacto. 3 testes + validado ao vivo (`ODATE=20260702`).
   ⬜ GLOBAIS de runtime (SET) — um job ATRIBUI valor e jobs posteriores LEEM (passagem entre jobs; hoje as
      globais são só interpoláveis via VariableStore, falta o SET em runtime por um job).
   ⬜ LOCAIS por job — escopo só do próprio job.
   ⬜ NATIVAS extras — último dia do mês · dia útil… (além dos tokens de runtime já entregues).
   ⬜ CÁLCULO de datas com template — aritmética sobre datas (ex.: `%DiaAtual+3`) resolvendo p/ data numérica,
      ciente de dia útil/feriado/calendar (sexta + 3 = próxima data útil). + inspetor de resolução por instância.
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
✅ Diff de Daily — ENTREGUE (2026-06-24): compara dois order_date (default hoje vs diária anterior, ou
   ?from&to&folder): adicionados (+) / removidos (-) / ALTERADOS com diff POR-CAMPO (schedule, deps, recursos,
   conditions, params, SLA…). Aproveita o DNA Git-native (instance carrega commit_sha + snapshot congelado) →
   EXATO e barato, sem reprocessar Git. Fast-path: commitA==commitB ⟹ nenhum comum mudou (pula a comparação).
   Contadores exatos, listas capadas (truncated). `GET /api/daily/diff` + `DailyDiffModal` (botão "Diff" na
   topbar). 4 testes + validado ao vivo. (`DiffDaily` é candidato natural a tool MCP `diff_daily()`.)
✅ Blast Radius — ENTREGUE (2026-06-24): "se eu CANCELAR/segurar este job AGORA, qual o impacto?". BFS no
   grafo reverso de deps a partir do alvo: jobs downstream que deixam de rodar em CASCATA · SLAs em risco ·
   folders afetadas · profundidade da cascata. Análise de uma AÇÃO (não do grafo estático): só conta sucessores
   por aresta NÃO-`always` ainda-não-rodados (WAITING/HELD); arestas `always` não propagam, jobs já rodados
   param a cascata. Visita só o RAIO → barato a 1M. `GET /api/instances/{id}/blast-radius` + painel "⚠ Impacto
   se cancelar/segurar" no drawer. 4 testes + validado ao vivo. (Candidato a tool MCP `blast_radius()`.)
✅ Dry Run — ENTREGUE (2026-06-24): simula a daily de QUALQUER data SEM materializar: RUN (raiz elegível) ·
   WAIT (depois de quais upstreams) · BLOCKED (agendado mas nunca dispara — dep não-`always` não-agendada ou
   condition que ninguém seta; cascata transitiva) · NOT_SCHEDULED (fora do calendário/frequência), com razão
   por job. Reusa `IsScheduledOn` (a MESMA decisão do RunDaily) como fonte única; recursos não entram
   (contenção de runtime). `GET /api/daily/dryrun?date=` + `DryRunModal` (botão "Dry Run" + seletor de data).
   3 testes + validado ao vivo. (Candidato a tool MCP `dry_run()`.)
✅ Explain ("por que o job não rodou?") — ENTREGUE (2026-06-24): motor de EXPLICAÇÃO (sem IA): WAIT_WINDOW ·
   WAIT_DEP / BLOCKED_DEP (qual upstream, condição, status) · WAIT_CONDITION (qual condition falta) ·
   WAIT_RESOURCE (recurso, quer/uso/capacidade). **Construído como FONTE ÚNICA do gating**: `gateInstance`
   é o avaliador que o TICK usa pra decidir E o Explain pra mostrar — nenhum gate bloqueia o dispatch sem
   aparecer no Explain, então condição nova é absorvida de graça (sem checador paralelo / manutenção dupla).
   `edgeState` (regra de aresta), `Conditions.Missing` e `Resources.Shortfalls` são read-only e fonte única
   com evalDeps/TryAcquire. `GET /api/instances/{id}/explain` → {runnable, summary, blockers[]} estruturado;
   painel "Por que (não) rodou?" no drawer. Custo O(upstreams), não O(daily) → vale a 1M. 21 testes + validado
   ao vivo. **Substrato do futuro tool MCP `explain_job()`** (camada agent-native).
⬜ Event log de primeira classe (CQRS-lite — NÃO Event Sourcing puro) — evoluir o `instance_events` (já é a
   semente) para um log COMPLETO e CONFIÁVEL: emissão TRANSACIONAL (evento + mutação de estado no mesmo
   commit), sequência global, + tipos que faltam (DailyCreated, ConditionAdded/Removed…). O estado mutável
   segue como PROJEÇÃO (claim atômico intacto). Destrava replay / time-travel / forense ("estado às 08:14")
   e outbox → NATS/observabilidade, SEM reescrever o core de correção. (ES puro foi avaliado e rejeitado por
   ROI/risco: HA/DR/auditoria/histórico-de-config já cobertos por Postgres+leader · PITR · instance_events · Git.)
⬜ Query estruturado / busca rica sobre o estado — endpoint de busca com filtro COMPOSTO (ranges, listas IN,
   múltiplos campos, agregações) além do que `/api/instances?filtros` cobre hoje. Bounded e tipado (NÃO
   SQL-sobre-HTTP). Consumidores: dashboards, integrações e a camada agent-native (tool MCP de query).
   ── DECISÃO de transporte (2026-06-24): quando este item existir, usar **`POST /api/instances/query`
   como baseline universal** + **aceitar o método HTTP `QUERY` na MESMA handler como opt-in** (progressive
   enhancement p/ CLI/integrações/MCP). `QUERY` (draft IETF httpbis, novo verbo) é SAFE+idempotente como GET,
   carrega body como POST e é CACHEÁVEL reusando o cache p/ a MESMA query (POST só semeia cache de GET/HEAD).
   Em Go NÃO depende de release de framework (diferente do .NET 10): `net/http`+chi roteiam método custom hoje
   (`r.Method("QUERY", h)`). NÃO adotar agora: os filtros atuais cabem na query string, a API é autenticada
   sem CDN no meio (ganho de cache ≈ nulo) e ecossistema (proxies/caches que entendam QUERY) é imaturo —
   seria enfeite com atrito. Adotar SÓ quando houver filtro-complexo-demais-pra-URL E/OU cache de
   intermediário em jogo. Ref: vensas.de/en/blog/http-query-method-dotnet-10.
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
